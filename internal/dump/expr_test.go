package dump_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/dump"
	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/plan"
)

func TestExpr(t *testing.T) {
	e := ir.All{Operands: []ir.Expr{
		ir.Kinds{Set: plan.SetString},
		ir.Predicate{Guard: plan.SetString, Detail: ir.MinLengthDetail{Value: 3}},
		ir.Shape{Detail: ir.ObjectShape{
			Properties: []ir.PropertyExpr{
				{Name: "id", Schema: ir.Kinds{Set: plan.SetString}},
			},
			AdditionalProperties: ir.Never{},
		}},
		ir.AnyOf{Operands: []ir.Expr{
			ir.Ref{Target: "urn:def", TargetKinds: plan.SetString, KindsKnown: true},
			ir.DynamicRef{Anchor: "node"},
		}},
		ir.ExactlyOne{Operands: []ir.Expr{ir.Any{}, ir.Never{}}},
		ir.Not{Operand: ir.Kinds{Set: plan.SetNumber}},
		ir.Annotated{Expr: ir.Any{}},
		ir.Literal{Value: "x"},
	}}

	var out strings.Builder
	dump.Expr(&out, e)
	got := out.String()

	for _, want := range []string{
		"All",
		"Kinds {string}",
		"Predicate guard={string}",
		"MinLength 3",
		"ObjectShape",
		`property "id"`,
		"additionalProperties",
		"Never",
		"AnyOf",
		`Ref target="urn:def" kinds={string}`,
		`DynamicRef anchor="node"`,
		"ExactlyOne",
		"Any",
		"Not",
		"Kinds {number}",
		"Annotated",
		`Literal "x"`,
	} {
		require.Contains(t, got, want)
	}
}

func TestExpr_ArrayShapeAndContains(t *testing.T) {
	e := ir.Shape{Detail: ir.ArrayShape{
		PrefixItems:      []ir.Expr{ir.Kinds{Set: plan.SetString}},
		Items:            ir.Kinds{Set: plan.SetNumber},
		UnevaluatedItems: ir.Never{},
	}}

	var out strings.Builder
	dump.Expr(&out, e)
	got := out.String()

	require.Contains(t, got, "ArrayShape")
	require.Contains(t, got, "prefixItems[0]")
	require.Contains(t, got, "items")
	require.Contains(t, got, "unevaluatedItems")

	minVal := uint64(1)
	contains := ir.Predicate{Guard: plan.SetArray, Detail: ir.ContainsDetail{
		Schema: ir.Kinds{Set: plan.SetString},
		Min:    &minVal,
	}}
	out.Reset()
	dump.Expr(&out, contains)
	got = out.String()
	require.Contains(t, got, "Contains min=1 max=-")
}
