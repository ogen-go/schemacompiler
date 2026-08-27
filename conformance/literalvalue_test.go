// literalvalue_test.go measures, over real specs, the Go dynamic types a
// [plan.LiteralCase.Value] arrives as. The hand-written half of the enumeration lives in
// the repo root (literalvalue_test.go); this half is what says the enumeration covers what
// documents in the wild actually contain (issue #152).
package conformance

import (
	"fmt"
	"slices"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/planwalk"
	"github.com/ogen-go/schemacompiler/plan"
)

// documentedLiteralValueTypes is the set [plan.LiteralCase] documents, every spelling of
// it: the corpus is only asked not to leave the set, so the 32-bit-only int64 stays in.
var documentedLiteralValueTypes = []string{
	"<nil>", "bool", "string", "int", "int64", "uint64", "float64",
	"[]interface {}", "map[string]interface {}",
}

// TestLiteralValueGoTypesOgenCorpus asserts no literal in the corpus reaches a backend as
// a Go type [plan.LiteralCase] does not name, and reports the distribution.
func TestLiteralValueGoTypesOgenCorpus(t *testing.T) {
	counts := map[string]int{}
	var noRaw int
	eachOgenDocument(t, func(_ string, defs map[plan.SchemaID]plan.CompilationPlan) {
		for _, p := range defs {
			planwalk.Fold(p, struct{}{}, func(acc struct{}, n planwalk.Node) (struct{}, planwalk.Action) {
				if n.Kind != planwalk.NodeDispatch {
					return acc, planwalk.Descend
				}
				var cases []plan.LiteralCase
				switch d := n.Dispatch.(type) {
				case *plan.LiteralDispatch:
					cases = d.Cases
				case *plan.PropertyDispatch:
					cases = d.Cases
				}
				for _, c := range cases {
					counts[fmt.Sprintf("%T", c.Value)]++
					if len(c.Raw) == 0 {
						noRaw++
					}
				}
				return acc, planwalk.Descend
			})
		}
	})

	require.NotZero(t, len(counts), "corpus produced no literals")
	observed := make([]string, 0, len(counts))
	for k, n := range counts {
		observed = append(observed, k)
		t.Logf("%-24s %d", k, n)
	}
	sort.Strings(observed)
	for _, got := range observed {
		require.Truef(t, slices.Contains(documentedLiteralValueTypes, got),
			"literal value arrived as undocumented Go type %s", got)
	}
	t.Logf("cases with no Raw: %d", noRaw)
}
