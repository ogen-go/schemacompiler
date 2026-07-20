package norm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/plan"
)

// TestNormalize_TypeArrayRemovesBranch_DesignSect16_2 is the design §16.2
// worked example: an outer type array intersected with oneOf prunes the
// branch that becomes impossible once the type is pushed in.
func TestNormalize_TypeArrayRemovesBranch_DesignSect16_2(t *testing.T) {
	in := ir.All{Operands: []ir.Expr{
		ir.Kinds{Set: plan.SetString | plan.SetNumber},
		ir.ExactlyOne{Operands: []ir.Expr{
			ir.Kinds{Set: plan.SetString},
			ir.Kinds{Set: plan.SetBoolean},
		}},
	}}
	want := ir.Kinds{Set: plan.SetString}
	require.Equal(t, want, Normalize(in, 100))
}

// TestNormalize_SiblingOneOfAnyOf_DesignSect17_3 is the design §17.3 worked
// example: sibling oneOf/anyOf combine as
//
//	ExactlyOne(A, B) ∩ (C ∪ D) = ExactlyOne(A∩(C∪D), B∩(C∪D))
//
// Here A/B are kind-disjoint (string/number) and C/D are guarded by the
// opposite kind each, so pushing (C∪D) into each branch reduces every guard
// to vacuously-true within that branch, collapsing the whole expression down
// to a plain kind union — still exactly equivalent to the original.
func TestNormalize_SiblingOneOfAnyOf_DesignSect17_3(t *testing.T) {
	c := ir.Predicate{Guard: plan.SetString, Detail: ir.MinLengthDetail{Value: 3}}
	d := ir.Predicate{Guard: plan.SetNumber, Detail: ir.MinimumDetail{Value: 0}}

	in := ir.All{Operands: []ir.Expr{
		ir.ExactlyOne{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString}, ir.Kinds{Set: plan.SetNumber}}},
		ir.AnyOf{Operands: []ir.Expr{c, d}},
	}}
	want := ir.AnyOf{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString}, ir.Kinds{Set: plan.SetNumber}}}
	require.Equal(t, want, Normalize(in, 100))
}

// TestNormalize_MultipleExactlyOneKeptFactored covers design §17.6: multiple
// independent ExactlyOne groups are not Cartesian-expanded.
func TestNormalize_MultipleExactlyOneKeptFactored(t *testing.T) {
	a := ir.All{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString}, ir.Predicate{Guard: plan.SetString, Detail: ir.PatternDetail{Regex: "^a"}}}}
	b := ir.All{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString}, ir.Predicate{Guard: plan.SetString, Detail: ir.PatternDetail{Regex: "^b"}}}}
	c := ir.All{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString}, ir.Predicate{Guard: plan.SetString, Detail: ir.PatternDetail{Regex: "^c"}}}}
	d := ir.All{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString}, ir.Predicate{Guard: plan.SetString, Detail: ir.PatternDetail{Regex: "^d"}}}}

	eo1 := ir.ExactlyOne{Operands: []ir.Expr{a, b}}
	eo2 := ir.ExactlyOne{Operands: []ir.Expr{c, d}}
	in := ir.All{Operands: []ir.Expr{eo1, eo2}}

	got := Normalize(in, 100)
	want := ir.All{Operands: []ir.Expr{eo1, eo2}}
	require.Equal(t, want, got, "two independent ExactlyOne groups must stay factored, not Cartesian-expanded")
}

// TestNormalize_BudgetStopsDistribution checks that a budget of 0 leaves the
// expression sound but un-distributed (the ExactlyOne/AnyOf branch is never
// pushed into its sibling), rather than looping or corrupting it. The
// disjointness rewrite still fires since it does not consume budget.
func TestNormalize_BudgetStopsDistribution(t *testing.T) {
	in := ir.All{Operands: []ir.Expr{
		ir.Kinds{Set: plan.SetString | plan.SetNumber},
		ir.ExactlyOne{Operands: []ir.Expr{
			ir.Kinds{Set: plan.SetString},
			ir.Kinds{Set: plan.SetBoolean},
		}},
	}}
	got := Normalize(in, 0)
	want := ir.All{Operands: []ir.Expr{
		ir.Kinds{Set: plan.SetString | plan.SetNumber},
		ir.AnyOf{Operands: []ir.Expr{
			ir.Kinds{Set: plan.SetString},
			ir.Kinds{Set: plan.SetBoolean},
		}},
	}}
	require.Equal(t, want, got)
}
