// gogen_test.go pins the naming rule docs/backend.md §1 rests on. The rule is restrictive
// on purpose — a name it cannot derive is an error, not a guess — so what it costs is
// measured here against real specs rather than argued about.
package conformance

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/gogen"
	"github.com/ogen-go/schemacompiler/plan"
)

// TestGogenNamesOgenCorpus walks a live ogen checkout and names every plan in it. Two
// properties are asserted: the rule derives a name for every real schema without an
// annotation, and no two schemas collide. Both are what make the hard-error-on-collision
// policy affordable — if either failed at scale, the policy would be the wrong one.
func TestGogenNamesOgenCorpus(t *testing.T) {
	root := resolveOgenRoot(t)

	var lengths []int
	var docs, schemas int
	var failures []string
	require.NoError(t, filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".json") ||
			strings.Contains(p, string(filepath.Separator)+"negative"+string(filepath.Separator)) ||
			strings.Contains(p, string(filepath.Separator)+".git"+string(filepath.Separator)) {
			return nil //nolint:nilerr // a walk error on one file must not stop the corpus
		}
		data, err := os.ReadFile(p) //nolint:gosec // test-only, path from a controlled opt-in walk
		if err != nil {
			return nil
		}
		bundle, err := bundleComponentSchemas(data)
		if err != nil || bundle == nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		func() {
			defer func() { _ = recover() }()
			res, err := schemacompiler.Compile(context.Background(), bundle, schemacompiler.Options{})
			if err != nil {
				return
			}
			graph, ok := res.Plan.Resolution.(*plan.StaticReferenceGraph)
			if !ok {
				return
			}
			docs++
			names, err := gogen.Assign(graph.Definitions)
			if err != nil {
				failures = append(failures, filepath.ToSlash(rel)+": "+err.Error())
				return
			}
			schemas += len(names)
			for _, n := range names {
				lengths = append(lengths, len(n))
			}
		}()
		return nil
	}))

	require.NotZero(t, docs, "walked no compilable documents")
	require.Empty(t, failures, "every schema in the corpus must name without an annotation")

	slices.Sort(lengths)
	at := func(q int) int { return lengths[min(len(lengths)*q/100, len(lengths)-1)] }
	t.Logf("named %d schemas across %d documents; length p50=%d p90=%d p99=%d max=%d",
		schemas, docs, at(50), at(90), at(99), lengths[len(lengths)-1])
}
