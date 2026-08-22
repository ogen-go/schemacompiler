package planner_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/internal/planner"
	"github.com/ogen-go/schemacompiler/plan"
)

// constrainedString is {"type":"string","minLength":1}.
func constrainedString() ir.Expr {
	return ir.All{Operands: []ir.Expr{
		ir.Kinds{Set: plan.SetString},
		ir.Predicate{Guard: plan.SetString, Detail: ir.MinLengthDetail{Value: 1}},
	}}
}

// TestBuild_SubSchemaPlansSurvive pins issue #68: every applicator slot carries the whole
// plan of the sub-schema written there, so the sub-schema's residual validation reaches
// the value it constrains instead of being costed and dropped.
func TestBuild_SubSchemaPlansSurvive(t *testing.T) {
	tests := []struct {
		name string
		expr ir.Expr
		sub  func(t *testing.T, p plan.CompilationPlan) plan.CompilationPlan
	}{
		{
			name: "property",
			expr: ir.All{Operands: []ir.Expr{
				ir.Kinds{Set: plan.SetObject},
				ir.Shape{Detail: ir.ObjectShape{
					Properties: []ir.PropertyExpr{{Name: "a", Schema: constrainedString()}},
				}},
			}},
			sub: func(t *testing.T, p plan.CompilationPlan) plan.CompilationPlan {
				obj, ok := p.Representation.(plan.ObjectRepresentation)
				require.True(t, ok, "got %T", p.Representation)
				return plannerField(t, obj, "a").Plan
			},
		},
		{
			name: "additionalProperties",
			expr: ir.All{Operands: []ir.Expr{
				ir.Kinds{Set: plan.SetObject},
				ir.Shape{Detail: ir.ObjectShape{AdditionalProperties: constrainedString()}},
			}},
			sub: func(t *testing.T, p plan.CompilationPlan) plan.CompilationPlan {
				obj, ok := p.Representation.(plan.ObjectRepresentation)
				require.True(t, ok, "got %T", p.Representation)
				require.NotNil(t, obj.Additional)
				return *obj.Additional
			},
		},
		{
			name: "patternProperties",
			expr: ir.All{Operands: []ir.Expr{
				ir.Kinds{Set: plan.SetObject},
				ir.Shape{Detail: ir.ObjectShape{
					PatternProperties: []ir.PatternPropertyExpr{{Pattern: "^x", Schema: constrainedString()}},
				}},
			}},
			sub: func(t *testing.T, p plan.CompilationPlan) plan.CompilationPlan {
				obj, ok := p.Representation.(plan.ObjectRepresentation)
				require.True(t, ok, "got %T", p.Representation)
				require.Len(t, obj.PatternRules, 1)
				return obj.PatternRules[0].Plan
			},
		},
		{
			name: "prefixItems",
			expr: ir.All{Operands: []ir.Expr{
				ir.Kinds{Set: plan.SetArray},
				ir.Shape{Detail: ir.ArrayShape{PrefixItems: []ir.ItemExpr{{Schema: constrainedString()}}}},
			}},
			sub: func(t *testing.T, p plan.CompilationPlan) plan.CompilationPlan {
				arr, ok := p.Representation.(plan.ArrayRepresentation)
				require.True(t, ok, "got %T", p.Representation)
				require.Len(t, arr.Prefix, 1)
				return arr.Prefix[0].Plan
			},
		},
		{
			name: "items",
			expr: ir.All{Operands: []ir.Expr{
				ir.Kinds{Set: plan.SetArray},
				ir.Shape{Detail: ir.ArrayShape{Items: ir.ItemExpr{Schema: constrainedString()}}},
			}},
			sub: func(t *testing.T, p plan.CompilationPlan) plan.CompilationPlan {
				arr, ok := p.Representation.(plan.ArrayRepresentation)
				require.True(t, ok, "got %T", p.Representation)
				return arr.Rest.Plan
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := planner.Build(tt.expr, nil)
			require.Equal(t, plan.GoTypeWithValidation, got.Plan.Capability,
				"the sub-schema's cost must still be rolled up")

			sub := tt.sub(t, got.Plan)
			require.Equal(t, plan.PrimitiveRepresentation{Kind: plan.KindString}, sub.Representation)
			require.Len(t, checks(sub), 1)
			require.Equal(t, plan.MinLengthPredicate{Value: 1}, checks(sub)[0].Expression)
			require.Equal(t, plan.SetString, checks(sub)[0].Applicability)
		})
	}
}

// TestBuild_SubSchemaDispatchSurvives is the dispatch half of #68: a property whose
// sub-schema needs a runtime kind check keeps that check on the field.
func TestBuild_SubSchemaDispatchSurvives(t *testing.T) {
	// {"type":"object","properties":{"a":{"type":["string","integer","null"]}}}
	e := ir.All{Operands: []ir.Expr{
		ir.Kinds{Set: plan.SetObject},
		ir.Shape{Detail: ir.ObjectShape{Properties: []ir.PropertyExpr{{
			Name:   "a",
			Schema: ir.All{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString | plan.SetNumber | plan.SetNull}}},
		}}}},
	}}

	got := planner.Build(e, nil)

	obj, ok := got.Plan.Representation.(plan.ObjectRepresentation)
	require.True(t, ok, "got %T", got.Plan.Representation)
	field := plannerField(t, obj, "a")
	require.True(t, field.Nullable, "null is carried by the field, not by its plan")

	disp, ok := field.Plan.Dispatch.(plan.KindDispatch)
	require.True(t, ok, "got %T", field.Plan.Dispatch)
	require.Len(t, disp.Cases, 2)
	require.Contains(t, disp.Cases, plan.KindString)
	require.Contains(t, disp.Cases, plan.KindNumber)
	require.Equal(t, plan.StaticDispatch, got.Plan.Capability)
}
