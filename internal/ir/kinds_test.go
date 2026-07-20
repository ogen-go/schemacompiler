package ir

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/plan"
)

func TestNot_Kinds(t *testing.T) {
	cases := []struct {
		name string
		expr Not
		want plan.KindSet
	}{
		{
			"pure kind restriction complements exactly",
			Not{Operand: Kinds{Set: plan.SetString, Numeric: plan.AnyNumber}},
			plan.SetAny &^ plan.SetString,
		},
		{
			"wrapped in single-operand All still exact",
			Not{Operand: All{Operands: []Expr{Kinds{Set: plan.SetNumber, Numeric: plan.AnyNumber}}}},
			plan.SetAny &^ plan.SetNumber,
		},
		{
			"integer-only numeric refinement is NOT pure: conservative",
			Not{Operand: Kinds{Set: plan.SetNumber, Numeric: plan.IntegerOnly}},
			plan.SetAny,
		},
		{
			"residual predicate is not a kind restriction: conservative",
			Not{Operand: Predicate{Guard: plan.SetString, Detail: MinLengthDetail{Value: 5}}},
			plan.SetAny,
		},
		{
			"Any operand complements to empty",
			Not{Operand: Any{}},
			0,
		},
		{
			"Never operand complements to SetAny",
			Not{Operand: Never{}},
			plan.SetAny,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.expr.Kinds())
		})
	}
}
