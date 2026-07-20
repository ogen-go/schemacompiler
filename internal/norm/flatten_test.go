package norm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/plan"
)

func TestNormalize_FlattenAll(t *testing.T) {
	minLen3 := ir.Predicate{Guard: plan.SetString, Detail: ir.MinLengthDetail{Value: 3}}
	maxLen10 := ir.Predicate{Guard: plan.SetString, Detail: ir.MaxLengthDetail{Value: 10}}

	in := ir.All{Operands: []ir.Expr{
		ir.All{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString}, minLen3}},
		maxLen10,
	}}
	want := ir.All{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString}, minLen3, maxLen10}}
	require.Equal(t, want, Normalize(in, 100))
}

func TestNormalize_FlattenAnyOf(t *testing.T) {
	in := ir.AnyOf{Operands: []ir.Expr{
		ir.AnyOf{Operands: []ir.Expr{ir.Literal{Value: "a"}, ir.Literal{Value: "b"}}},
		ir.Literal{Value: "c"},
	}}
	want := ir.AnyOf{Operands: []ir.Expr{ir.Literal{Value: "a"}, ir.Literal{Value: "b"}, ir.Literal{Value: "c"}}}
	require.Equal(t, want, Normalize(in, 100))
}

func TestNormalize_ExactlyOneNotFlattened(t *testing.T) {
	// design §15: ExactlyOne is NOT associative like All/AnyOf. A nested
	// ExactlyOne that cannot itself be simplified away must stay nested.
	patStr := func(re string) ir.Expr {
		return ir.All{Operands: []ir.Expr{
			ir.Kinds{Set: plan.SetString},
			ir.Predicate{Guard: plan.SetString, Detail: ir.PatternDetail{Regex: re}},
		}}
	}
	inner := ir.ExactlyOne{Operands: []ir.Expr{patStr("^a"), patStr("^b")}}
	outer := ir.ExactlyOne{Operands: []ir.Expr{inner, patStr("^c")}}

	got := Normalize(outer, 100)
	want := ir.ExactlyOne{Operands: []ir.Expr{inner, patStr("^c")}}
	require.Equal(t, want, got, "nested ExactlyOne must not merge into the outer one")
}

func TestNormalize_Identities(t *testing.T) {
	cases := []struct {
		name string
		in   ir.Expr
		want ir.Expr
	}{
		{"All() -> Any", ir.All{}, ir.Any{}},
		{"AnyOf() -> Never", ir.AnyOf{}, ir.Never{}},
		{"ExactlyOne() -> Never", ir.ExactlyOne{}, ir.Never{}},
		{"All(A) -> A", ir.All{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString}}}, ir.Kinds{Set: plan.SetString}},
		{"AnyOf(A) -> A", ir.AnyOf{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString}}}, ir.Kinds{Set: plan.SetString}},
		{"ExactlyOne(A) -> A", ir.ExactlyOne{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString}}}, ir.Kinds{Set: plan.SetString}},
		{
			"All(..., Never, ...) -> Never",
			ir.All{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString}, ir.Never{}}},
			ir.Never{},
		},
		{
			"AnyOf(..., Never, ...) -> remove Never",
			ir.AnyOf{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString}, ir.Never{}}},
			ir.Kinds{Set: plan.SetString},
		},
		{
			"ExactlyOne(..., Never, ...) -> remove Never",
			ir.ExactlyOne{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString}, ir.Never{}}},
			ir.Kinds{Set: plan.SetString},
		},
		{
			"All drops Any operand",
			ir.All{Operands: []ir.Expr{ir.Any{}, ir.Kinds{Set: plan.SetString}}},
			ir.Kinds{Set: plan.SetString},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, Normalize(tc.in, 100))
		})
	}
}

func TestNormalize_Idempotence(t *testing.T) {
	cases := []struct {
		name string
		a    ir.Expr
	}{
		{"All(A, A) -> A", ir.All{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString}, ir.Kinds{Set: plan.SetString}}}},
		{"AnyOf(A, A) -> A", ir.AnyOf{Operands: []ir.Expr{ir.Literal{Value: "x"}, ir.Literal{Value: "x"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Normalize(tc.a, 100)
			require.NotEqual(t, tc.a, got)
		})
	}
}

func TestNormalize_ExactlyOneDuplicate(t *testing.T) {
	// design §15.1: ExactlyOne(A, A) -> Never, because every value satisfying
	// A satisfies two branches, so exactly-one can never hold.
	a := ir.Kinds{Set: plan.SetString}
	got := Normalize(ir.ExactlyOne{Operands: []ir.Expr{a, a}}, 100)
	require.Equal(t, ir.Never{}, got)
}

func TestNormalize_ExactlyOneDuplicateGeneralized(t *testing.T) {
	// ExactlyOne(A, A, C) generalizes to All(Not(A), ExactlyOne(C)) = All(Not(A), C):
	// any value in the duplicated branch A always yields >=2 matches and can
	// never satisfy exactly-one, so it is excluded and C alone decides the rest.
	a := ir.Kinds{Set: plan.SetString}
	c := ir.Kinds{Set: plan.SetNumber}
	got := Normalize(ir.ExactlyOne{Operands: []ir.Expr{a, a, c}}, 100)
	// foldKindsAll (design §15, kind intersection) canonicalizes the merged
	// Kinds operand to the front of an All.
	want := ir.All{Operands: []ir.Expr{c, ir.Not{Operand: a}}}
	require.Equal(t, want, got)
}

func TestNormalize_KindIntersection(t *testing.T) {
	cases := []struct {
		name string
		in   ir.Expr
		want ir.Expr
	}{
		{
			"intersect kind sets",
			ir.All{Operands: []ir.Expr{
				ir.Kinds{Set: plan.SetString | plan.SetNumber},
				ir.Kinds{Set: plan.SetNumber | plan.SetBoolean},
			}},
			ir.Kinds{Set: plan.SetNumber},
		},
		{
			"AnyNumber ∩ IntegerOnly = IntegerOnly",
			ir.All{Operands: []ir.Expr{
				ir.Kinds{Set: plan.SetNumber, Numeric: plan.AnyNumber},
				ir.Kinds{Set: plan.SetNumber, Numeric: plan.IntegerOnly},
			}},
			ir.Kinds{Set: plan.SetNumber, Numeric: plan.IntegerOnly},
		},
		{
			"IntegerOnly ∩ NonIntegerOnly = empty -> Never",
			ir.All{Operands: []ir.Expr{
				ir.Kinds{Set: plan.SetNumber, Numeric: plan.IntegerOnly},
				ir.Kinds{Set: plan.SetNumber, Numeric: plan.NonIntegerOnly},
			}},
			ir.Never{},
		},
		{
			"disjoint kind sets -> Never",
			ir.All{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString}, ir.Kinds{Set: plan.SetNumber}}},
			ir.Never{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, Normalize(tc.in, 100))
		})
	}
}

func TestNormalize_GuardElimination(t *testing.T) {
	// design §3: inside All(Kinds{K}, Predicate{Guard}), a guard disjoint
	// from K is vacuous (always passes) and can be dropped.
	minLen := ir.Predicate{Guard: plan.SetString, Detail: ir.MinLengthDetail{Value: 5}}
	in := ir.All{Operands: []ir.Expr{ir.Kinds{Set: plan.SetNumber}, minLen}}
	require.Equal(t, ir.Kinds{Set: plan.SetNumber}, Normalize(in, 100))

	// K ⊆ Guard: the guard is redundant but sound to keep; the predicate must
	// not be dropped.
	kept := ir.All{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString}, minLen}}
	require.Equal(t, kept, Normalize(kept, 100))
}
