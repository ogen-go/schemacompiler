// literalvalue_test.go pins the Go dynamic types a [plan.LiteralCase.Value] may arrive as.
// The doc on LiteralCase enumerates them instead of claiming a canonicalization the
// planner does not perform (issue #152), and an enumeration only stays true if a test
// fails when it stops being.
package schemacompiler_test

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/internal/planwalk"
	"github.com/ogen-go/schemacompiler/plan"
)

// documentedLiteralValueTypes is the set [plan.LiteralCase] documents, narrowed to the
// spellings this platform can produce: int64 stands in for int only where int is 32 bits.
func documentedLiteralValueTypes() []string {
	types := []string{"<nil>", "bool", "string", "int", "uint64", "float64", "[]interface {}", "map[string]interface {}"}
	if strconv.IntSize == 32 {
		types = append(types, "int64")
	}
	sort.Strings(types)
	return types
}

// literalValueTypes collects the %T of every literal value a backend can reach in p: the
// cases of [plan.LiteralDispatch] and of [plan.PropertyDispatch], which are the only
// places a decoded JSON literal crosses the plan boundary — [plan.CompilationPlan]'s
// Default and Examples are raw bytes.
func literalValueTypes(p plan.CompilationPlan, into map[string]int) {
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
			into[fmt.Sprintf("%T", c.Value)]++
		}
		return acc, planwalk.Descend
	})
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestLiteralValueGoTypes drives a literal of every JSON kind, plus the numeric edges
// where the decoded spelling changes, through the planner and asserts the resulting set of
// Go types is exactly what [plan.LiteralCase] documents.
func TestLiteralValueGoTypes(t *testing.T) {
	schemas := []string{
		`{"const":null}`,
		`{"const":true}`,
		`{"enum":[true,false]}`,
		`{"const":"x"}`,
		`{"type":"integer","enum":[1,2,3]}`,
		`{"const":-9223372036854775808}`,
		`{"const":9007199254740993}`,
		`{"const":18446744073709551615}`,
		`{"const":123456789012345678901234567890}`,
		`{"type":"number","enum":[1.5]}`,
		`{"const":1e3}`,
		`{"const":1.0}`,
		`{"enum":[[1,2],{"a":1}]}`,
		`{"oneOf":[{"properties":{"t":{"const":1}},"required":["t"]},` +
			`{"properties":{"t":{"const":2}},"required":["t"]}]}`,
		`{"oneOf":[{"properties":{"t":{"const":"a"}},"required":["t"]},` +
			`{"properties":{"t":{"const":"b"}},"required":["t"]}]}`,
	}

	got := map[string]int{}
	for _, s := range schemas {
		res, err := schemacompiler.Compile(context.Background(), []byte(s), schemacompiler.Options{})
		require.NoErrorf(t, err, "compile %s", s)
		literalValueTypes(res.Plan, got)
	}
	require.Equal(t, documentedLiteralValueTypes(), sortedKeys(got))
}

// TestLiteralCaseRawIsAuthoritative pins the reason the doc sends a consumer to Raw first:
// it is the source text, so an integer past float64's precision survives while Value has
// already lost it.
func TestLiteralCaseRawIsAuthoritative(t *testing.T) {
	res, err := schemacompiler.Compile(context.Background(),
		[]byte(`{"const":123456789012345678901234567890}`), schemacompiler.Options{})
	require.NoError(t, err)

	var cases []plan.LiteralCase
	planwalk.Fold(res.Plan, struct{}{}, func(acc struct{}, n planwalk.Node) (struct{}, planwalk.Action) {
		if d, ok := n.Dispatch.(*plan.LiteralDispatch); ok && n.Kind == planwalk.NodeDispatch {
			cases = append(cases, d.Cases...)
		}
		return acc, planwalk.Descend
	})
	require.Len(t, cases, 1)
	require.Equal(t, "123456789012345678901234567890", string(cases[0].Raw))
	require.IsType(t, float64(0), cases[0].Value)
}
