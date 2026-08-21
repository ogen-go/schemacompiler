package planner_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/internal/planner"
	"github.com/ogen-go/schemacompiler/plan"
)

// objectShapeExpr is {"properties":{"a":{"type":"string","minLength":1}},"additionalProperties":false}.
func objectShapeExpr() ir.Expr {
	return ir.Shape{Detail: ir.ObjectShape{
		Properties:           []ir.PropertyExpr{{Name: "a", Schema: constrainedString()}},
		AdditionalProperties: ir.Never{},
	}}
}

// arrayShapeExpr is {"items":{"type":"string","minLength":1}}.
func arrayShapeExpr() ir.Expr {
	return ir.Shape{Detail: ir.ArrayShape{Items: ir.ItemExpr{Schema: constrainedString()}}}
}

// TestBuild_ShapeWithoutTypeIsKindGuarded pins issue #72. A shape keyword written without
// a sibling `type` asserts nothing about the instance's kind (design §3), so the
// representation stays Any — but the shape itself must survive, guarded on its own kind.
func TestBuild_ShapeWithoutTypeIsKindGuarded(t *testing.T) {
	tests := []struct {
		name  string
		expr  ir.Expr
		guard plan.KindSet
	}{
		{name: "object", expr: objectShapeExpr(), guard: plan.SetObject},
		{name: "array", expr: arrayShapeExpr(), guard: plan.SetArray},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := planner.Build(tt.expr, nil).Plan

			require.IsType(t, plan.AnyRepresentation{}, got.Representation,
				"a shape keyword must not assert its own type")
			require.Len(t, got.Validation.Predicates, 1)
			gp := got.Validation.Predicates[0]
			require.Equal(t, tt.guard, gp.Applicability)
			require.IsType(t, plan.ShapePredicate{}, gp.Expression)
		})
	}
}

// TestBuild_ShapeAgreesWithTypedSpelling pins the other half of issue #72: the guarded
// sub-plan is the plan a sibling `type` would have produced, so the two spellings differ
// only in the enclosing representation and never in the constraint.
func TestBuild_ShapeAgreesWithTypedSpelling(t *testing.T) {
	tests := []struct {
		name  string
		shape ir.Expr
		kinds plan.KindSet
	}{
		{name: "object", shape: objectShapeExpr(), kinds: plan.SetObject},
		{name: "array", shape: arrayShapeExpr(), kinds: plan.SetArray},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			untyped := planner.Build(tt.shape, nil).Plan
			typed := planner.Build(ir.All{Operands: []ir.Expr{
				ir.Kinds{Set: tt.kinds},
				tt.shape,
			}}, nil).Plan

			require.Len(t, untyped.Validation.Predicates, 1)
			shape, ok := untyped.Validation.Predicates[0].Expression.(plan.ShapePredicate)
			require.True(t, ok)
			require.Equal(t, typed, shape.Schema)
		})
	}
}

// TestBuild_PatternPropertiesIntersectDeclaredFields pins design §12.3's
// `constraintsFor(name)`: a property `properties` declares and a pattern matches must
// satisfy both, so the field's own plan carries the pattern's constraint too.
func TestBuild_PatternPropertiesIntersectDeclaredFields(t *testing.T) {
	got := planner.Build(ir.All{Operands: []ir.Expr{
		ir.Kinds{Set: plan.SetObject},
		ir.Shape{Detail: ir.ObjectShape{
			Properties: []ir.PropertyExpr{
				{Name: "foo", Schema: ir.Kinds{Set: plan.SetArray}},
				{Name: "bar", Schema: ir.Kinds{Set: plan.SetArray}},
			},
			PatternProperties: []ir.PatternPropertyExpr{{
				Pattern: "f.o",
				Schema:  ir.Predicate{Guard: plan.SetArray, Detail: ir.MinItemsDetail{Value: 2}},
			}},
		}},
	}}, nil).Plan

	obj, ok := got.Representation.(plan.ObjectRepresentation)
	require.True(t, ok, "got %T", got.Representation)
	require.Equal(t,
		plan.ValidationPlan{Predicates: []plan.GuardedPredicate{{
			Applicability: plan.SetArray,
			Expression:    plan.MinItemsPredicate{Value: 2},
		}}},
		plannerField(t, obj, "foo").Plan.Validation,
		"a matching pattern must be intersected into the declared field")
	require.True(t, plannerField(t, obj, "bar").Plan.Validation.Empty(),
		"a non-matching pattern must not reach the field")
}
