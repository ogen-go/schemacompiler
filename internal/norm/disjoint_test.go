package norm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/plan"
)

func TestDisjoint(t *testing.T) {
	cases := []struct {
		name string
		a, b ir.Expr
		want bool
	}{
		{
			"kind-disjoint",
			ir.Kinds{Set: plan.SetString},
			ir.Kinds{Set: plan.SetNumber},
			true,
		},
		{
			"overlapping kinds are not disjoint",
			ir.Kinds{Set: plan.SetString | plan.SetNumber},
			ir.Kinds{Set: plan.SetNumber},
			false,
		},
		{
			"distinct literals, same kind",
			ir.Literal{Value: "a"},
			ir.Literal{Value: "b"},
			true,
		},
		{
			"equal literals are not disjoint",
			ir.Literal{Value: "a"},
			ir.Literal{Value: "a"},
			false,
		},
		{
			"numeric literal cross-type equality (1 vs 1.0)",
			ir.Literal{Value: float64(1)},
			ir.Literal{Value: int64(1)},
			false,
		},
		{
			"literal vs enum: disjoint from every option",
			ir.Literal{Value: "z"},
			ir.AnyOf{Operands: []ir.Expr{ir.Literal{Value: "a"}, ir.Literal{Value: "b"}}},
			true,
		},
		{
			"literal vs enum: matches one option",
			ir.Literal{Value: "a"},
			ir.AnyOf{Operands: []ir.Expr{ir.Literal{Value: "a"}, ir.Literal{Value: "b"}}},
			false,
		},
		{
			"same-kind predicates with no proof: conservative false",
			ir.Predicate{Guard: plan.SetString, Detail: ir.PatternDetail{Regex: "^a"}},
			ir.Predicate{Guard: plan.SetString, Detail: ir.PatternDetail{Regex: "^b"}},
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, disjoint(tc.a, tc.b))
		})
	}
}

// TestNormalize_DisjointOneOf_DesignSect15_3 is the design §15.3 worked
// example: oneOf[string, number] normalizes to a plain union once branches
// are proven kind-disjoint.
func TestNormalize_DisjointOneOf_DesignSect15_3(t *testing.T) {
	in := ir.ExactlyOne{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString}, ir.Kinds{Set: plan.SetNumber}}}
	want := ir.AnyOf{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString}, ir.Kinds{Set: plan.SetNumber}}}
	require.Equal(t, want, Normalize(in, 100))
}
