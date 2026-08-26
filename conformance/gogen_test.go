// gogen_test.go pins the backend against real specs: the naming rule docs/backend.md §1
// rests on, and the lowering §7 does. Both are restrictive on purpose — a name the rule
// cannot derive is an error, not a guess — so what that costs is measured here rather than
// argued about.
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

// eachOgenDocument compiles every schema document in a live ogen checkout and hands its
// definitions to visit. A document that does not parse, compile or resolve statically is
// skipped: the corpus is someone else's repository, and a file it cannot read is not a
// statement about the backend.
func eachOgenDocument(t *testing.T, visit func(rel string, defs map[plan.SchemaID]plan.CompilationPlan)) {
	t.Helper()
	root := resolveOgenRoot(t)

	var docs int
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
			visit(filepath.ToSlash(rel), graph.Definitions)
		}()
		return nil
	}))
	require.NotZero(t, docs, "walked no compilable documents")
}

// TestGogenNamesOgenCorpus asserts the rule derives a name for every real schema without
// an annotation, and that no two schemas collide. Both are what make the
// hard-error-on-collision policy affordable — if either failed at scale, the policy would
// be the wrong one.
func TestGogenNamesOgenCorpus(t *testing.T) {
	var lengths []int
	var docs, schemas int
	var failures []string
	eachOgenDocument(t, func(rel string, defs map[plan.SchemaID]plan.CompilationPlan) {
		docs++
		names, err := gogen.Assign(defs)
		if err != nil {
			failures = append(failures, rel+": "+err.Error())
			return
		}
		schemas += len(names)
		for _, n := range names {
			lengths = append(lengths, len(n))
		}
	})

	require.Empty(t, failures, "every schema in the corpus must name without an annotation")

	slices.Sort(lengths)
	at := func(q int) int { return lengths[min(len(lengths)*q/100, len(lengths)-1)] }
	t.Logf("named %d schemas across %d documents; length p50=%d p90=%d p99=%d max=%d",
		schemas, docs, at(50), at(90), at(99), lengths[len(lengths)-1])
}

// TestGogenLowersOgenCorpus asserts that lowering never fails for a structural reason, and
// reports how many types the recursion pass had to break.
//
// The one failure the corpus does produce is a name: GitHub spells its reaction counts `+1`
// and `-1`, which drop to a leading digit and cannot be a Go identifier. That is the case
// [gogen.NameExtension] exists for, so the assertion is not "nothing fails" but "nothing
// fails for a reason the author cannot fix by naming it" — a shape we could not lower at
// all would say something quite different about the backend.
func TestGogenLowersOgenCorpus(t *testing.T) {
	var types, recursive int
	var unnameable []string
	eachOgenDocument(t, func(rel string, defs map[plan.SchemaID]plan.CompilationPlan) {
		lowered, err := gogen.Lower(defs)
		if err != nil {
			require.Contains(t, err.Error(), gogen.NameExtension,
				"%s: lowering failed for something other than a name", rel)
			unnameable = append(unnameable, rel)
			return
		}
		types += len(lowered)
		for _, n := range lowered {
			if n.Recursive {
				recursive++
			}
		}
	})

	require.NotZero(t, types)
	t.Logf("lowered %d types, %d recursive (%.2f%%); %d documents need a name: %s",
		types, recursive, float64(recursive)*100/float64(types), len(unnameable), strings.Join(unnameable, ", "))
}

// TestGogenLowersReproducibly lowers each document twice in one process. Map iteration is
// randomized per run, so an order leak in naming, in the component walk or in the pointer
// rewrite shows up as a difference here.
func TestGogenLowersReproducibly(t *testing.T) {
	eachOgenDocument(t, func(rel string, defs map[plan.SchemaID]plan.CompilationPlan) {
		first, err := gogen.Lower(defs)
		if err != nil {
			return
		}
		second, err := gogen.Lower(defs)
		require.NoError(t, err, rel)
		require.Len(t, second, len(first), rel)
		for i := range first {
			require.Equal(t, first[i].ID, second[i].ID, rel)
			require.Equal(t, first[i].Name, second[i].Name, rel)
			require.Equal(t, first[i].Recursive, second[i].Recursive, "%s: %s", rel, first[i].Name)
		}
	})
}
