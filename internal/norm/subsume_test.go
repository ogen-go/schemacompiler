package norm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/plan"
)

func TestSubsumes(t *testing.T) {
	str := ir.Kinds{Set: plan.SetString}
	strOrNum := ir.Kinds{Set: plan.SetString | plan.SetNumber}
	minLen5 := ir.Predicate{Guard: plan.SetString, Detail: ir.MinLengthDetail{Value: 5}}
	minLen10 := ir.Predicate{Guard: plan.SetString, Detail: ir.MinLengthDetail{Value: 10}}
	strMinLen5 := ir.All{Operands: []ir.Expr{str, minLen5}}

	cases := []struct {
		name string
		a, b ir.Expr
		want bool
	}{
		{"syntactic equality", str, str, true},
		{"Never subsumes anything", ir.Never{}, ir.Kinds{Set: plan.SetNumber}, true},
		{"anything is subsumed by Any", ir.Kinds{Set: plan.SetNumber}, ir.Any{}, true},
		{"kind subset", str, strOrNum, true},
		{"kind superset is not subsumed", strOrNum, str, false},
		{
			"IntegerOnly ⊆ AnyNumber",
			ir.Kinds{Set: plan.SetNumber, Numeric: plan.IntegerOnly},
			ir.Kinds{Set: plan.SetNumber, Numeric: plan.AnyNumber},
			true,
		},
		{
			"AnyNumber ⊄ IntegerOnly",
			ir.Kinds{Set: plan.SetNumber, Numeric: plan.AnyNumber},
			ir.Kinds{Set: plan.SetNumber, Numeric: plan.IntegerOnly},
			false,
		},
		{
			"IntegerOnly ⊄ NonIntegerOnly",
			ir.Kinds{Set: plan.SetNumber, Numeric: plan.IntegerOnly},
			ir.Kinds{Set: plan.SetNumber, Numeric: plan.NonIntegerOnly},
			false,
		},
		{"minLength(10) ⊆ minLength(5): tighter bound", minLen10, minLen5, true},
		{"minLength(5) ⊄ minLength(10)", minLen5, minLen10, false},
		{
			"conjunct lemma: All(string, minLength5) ⊆ string",
			strMinLen5, str, true,
		},
		{
			"disjunct lemma: string ⊆ AnyOf(string, number)",
			str,
			ir.AnyOf{Operands: []ir.Expr{str, ir.Kinds{Set: plan.SetNumber}}},
			true,
		},
		{
			"regex-language inclusion is NOT attempted: conservative false",
			ir.Predicate{Guard: plan.SetString, Detail: ir.PatternDetail{Regex: "^a"}},
			ir.Predicate{Guard: plan.SetString, Detail: ir.PatternDetail{Regex: "^[a-z]"}},
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, subsumes(tc.a, tc.b))
		})
	}
}

// TestNormalize_Subsumption_DesignSect15_2 covers the two worked examples
// from design §15.2.
func TestNormalize_Subsumption_DesignSect15_2(t *testing.T) {
	t.Run("oneOf[string, string+minLength5] -> string with length<5 (left factored)", func(t *testing.T) {
		str := ir.Kinds{Set: plan.SetString}
		strMinLen5 := ir.All{Operands: []ir.Expr{
			ir.Kinds{Set: plan.SetString},
			ir.Predicate{Guard: plan.SetString, Detail: ir.MinLengthDetail{Value: 5}},
		}}
		in := ir.ExactlyOne{Operands: []ir.Expr{str, strMinLen5}}

		// ExactlyOne(A,B) with B⊆A normalizes to All(A, Not(B)): "string, and
		// not (string with length>=5)" i.e. string with length<5. Not
		// elimination is not attempted (design §11.8), so Not(B) is left
		// as-is — sound, just not further simplified into a plain bound.
		want := ir.All{Operands: []ir.Expr{str, ir.Not{Operand: strMinLen5}}}
		require.Equal(t, want, Normalize(in, 100))
	})

	t.Run("oneOf[number, integer] -> non-integral number", func(t *testing.T) {
		num := ir.Kinds{Set: plan.SetNumber, Numeric: plan.AnyNumber}
		integer := ir.Kinds{Set: plan.SetNumber, Numeric: plan.IntegerOnly}
		in := ir.ExactlyOne{Operands: []ir.Expr{num, integer}}

		want := ir.All{Operands: []ir.Expr{num, ir.Not{Operand: integer}}}
		require.Equal(t, want, Normalize(in, 100))
	})
}
