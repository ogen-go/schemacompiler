// nodetree_test.go checks internal/nodetree against internal/planterp over the same suite
// instances. planterp is the reference: it is differentially tested against the suite
// itself (differential_test.go), so agreeing with it is what a faster implementation has
// to earn. A disagreement is a nodetree bug until proven otherwise.
package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/go-faster/errors"
	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/nodetree"
	"github.com/ogen-go/schemacompiler/internal/planterp"
	"github.com/ogen-go/schemacompiler/plan"
)

func TestNodetreeAgreesWithPlanterp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping suite walk in -short mode")
	}
	files := suiteFiles(t)

	var schemas, compiled, instances, agreed int
	unsupported := map[string]int{}
	var findings []string

	for _, file := range files {
		rel, err := filepath.Rel(suiteRoot, file)
		require.NoError(t, err)
		rel = filepath.ToSlash(rel)

		data, err := os.ReadFile(file) //nolint:gosec // test-only, path from a controlled walk
		require.NoError(t, err)
		var cases []diffCase
		if json.Unmarshal(data, &cases) != nil {
			continue
		}
		if _, ok := outOfDialect[rel]; ok {
			continue
		}

		for _, c := range cases {
			if len(c.Schema) == 0 {
				continue
			}
			res, err := compileQuietly(c.Schema)
			if err != nil {
				continue
			}
			if res.Capability == plan.Unsupported || res.Capability >= plan.EvaluationStateValidation {
				continue
			}
			schemas++

			v, err := nodetree.Compile(res.Plan)
			if err != nil {
				if !errors.Is(err, nodetree.ErrUnsupported) {
					t.Errorf("%s (%q): nodetree.Compile: %v", rel, c.Description, err)
				}
				unsupported[errors.Unwrap(err).Error()]++
				continue
			}
			compiled++

			for _, inst := range c.Tests {
				value, err := decodeInstance(inst.Data)
				if err != nil {
					continue
				}
				want, err := planterp.Interpret(res.Plan, value)
				if err != nil {
					continue // planterp's own guards are differential_test.go's business.
				}
				instances++

				got := v.IsValid(inst.Data)

				// IterErrors is a second traversal, hand-written per node. It has to
				// agree with the fast path exactly: an instance is invalid if and only
				// if at least one error is reported, or one of the two is lying.
				reported := v.Validate(inst.Data)
				if (reported == nil) != got {
					t.Errorf("%s (%q/%q): IsValid=%v but Validate=%v",
						rel, c.Description, inst.Description, got, reported)
				}

				if got == want.Accepted {
					agreed++
					continue
				}
				if len(findings) < 25 {
					findings = append(findings, fmt.Sprintf(
						"%s :: %s :: %s\n    schema=%s\n    data=%s\n    planterp=%v nodetree=%v",
						rel, c.Description, inst.Description, c.Schema, inst.Data, want.Accepted, got))
				}
			}
		}
	}

	t.Logf("schemas in band %d, nodetree compiled %d, instances %d, agreed %d", schemas, compiled, instances, agreed)
	if len(unsupported) > 0 {
		t.Logf("not compiled by nodetree:")
		keys := make([]string, 0, len(unsupported))
		for k := range unsupported {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return unsupported[keys[i]] > unsupported[keys[j]] })
		for _, k := range keys {
			t.Logf("  %-60s %d", k, unsupported[k])
		}
	}
	for _, f := range findings {
		t.Errorf("nodetree/planterp disagreement: %s", f)
	}
}
