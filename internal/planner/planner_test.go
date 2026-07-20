package planner_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/frontend"
	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/internal/planner"
	"github.com/ogen-go/schemacompiler/plan"
)

func ptr[T any](v T) *T { return &v }

func TestBuild_DirectGoType(t *testing.T) {
	// {"type": "string"}
	e := ir.All{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString}}}

	got := planner.Build(e, nil)

	require.Equal(t, plan.PrimitiveRepresentation{Kind: plan.KindString}, got.Plan.Representation)
	require.True(t, got.Plan.Validation.Empty())
	require.Equal(t, plan.DirectGoType, got.Plan.Capability)
	require.Equal(t, plan.ExactPureRepresentation, got.Exactness)
	require.Empty(t, got.Diagnostics)
}

func TestBuild_GoTypeWithValidation(t *testing.T) {
	// {"type": "string", "minLength": 3}
	e := ir.All{Operands: []ir.Expr{
		ir.Kinds{Set: plan.SetString},
		ir.Predicate{Guard: plan.SetString, Detail: ir.MinLengthDetail{Value: 3}},
	}}

	got := planner.Build(e, nil)

	require.Equal(t, plan.PrimitiveRepresentation{Kind: plan.KindString}, got.Plan.Representation)
	require.Equal(t, plan.GoTypeWithValidation, got.Plan.Capability)
	require.Equal(t, plan.ExactWithValidation, got.Exactness)
	require.Len(t, got.Plan.Validation.Predicates, 1)
	require.Equal(t, plan.MinLengthPredicate{Value: 3}, got.Plan.Validation.Predicates[0].Expression)
	require.Equal(t, plan.SetString, got.Plan.Validation.Predicates[0].Applicability)
}

func TestBuild_BarePredicateWidensToAny(t *testing.T) {
	// {"minLength": 3}: no type restriction, so every non-string value is also accepted
	// (design §3, §20.3). Representation must widen to Any, not narrow to string.
	e := ir.All{Operands: []ir.Expr{
		ir.Predicate{Guard: plan.SetString, Detail: ir.MinLengthDetail{Value: 3}},
	}}

	got := planner.Build(e, nil)

	require.Equal(t, plan.AnyRepresentation{}, got.Plan.Representation)
	require.Equal(t, plan.GoTypeWithValidation, got.Plan.Capability)
	require.Equal(t, plan.SoundOverApproximation, got.Exactness)
}

func TestBuild_StaticDispatch_KindDisjointOneOf(t *testing.T) {
	// {"oneOf": [{"type": "string"}, {"type": "number"}]}
	e := ir.All{Operands: []ir.Expr{
		ir.ExactlyOne{Operands: []ir.Expr{
			ir.All{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString}}},
			ir.All{Operands: []ir.Expr{ir.Kinds{Set: plan.SetNumber}}},
		}},
	}}

	got := planner.Build(e, nil)

	require.Equal(t, plan.StaticDispatch, got.Plan.Capability)
	disp, ok := got.Plan.Dispatch.(plan.KindDispatch)
	require.True(t, ok, "expected KindDispatch, got %T", got.Plan.Dispatch)
	require.Len(t, disp.Cases, 2)
	require.Contains(t, disp.Cases, plan.KindString)
	require.Contains(t, disp.Cases, plan.KindNumber)
	require.Empty(t, got.Diagnostics)
}

func TestBuild_PredicateDispatch_OverlappingOneOf(t *testing.T) {
	// {"oneOf": [{"type": "string", "pattern": "^a"}, {"type": "string", "minLength": 5}]}
	branch := func(detail ir.PredicateDetail) ir.Expr {
		return ir.All{Operands: []ir.Expr{
			ir.Kinds{Set: plan.SetString},
			ir.Predicate{Guard: plan.SetString, Detail: detail},
		}}
	}
	e := ir.All{Operands: []ir.Expr{
		ir.ExactlyOne{Operands: []ir.Expr{
			branch(ir.PatternDetail{Regex: "^a"}),
			branch(ir.MinLengthDetail{Value: 5}),
		}},
	}}

	got := planner.Build(e, nil)

	require.Equal(t, plan.PredicateDispatch, got.Plan.Capability)
	disp, ok := got.Plan.Dispatch.(plan.PredicateCountDispatch)
	require.True(t, ok, "expected PredicateCountDispatch, got %T", got.Plan.Dispatch)
	require.Equal(t, 1, disp.Minimum)
	require.Equal(t, 1, disp.Maximum)
	require.Len(t, disp.Branches, 2)
	require.NotEmpty(t, got.Diagnostics)
	require.Equal(t, plan.SeverityWarning, got.Diagnostics[0].Severity)
}

func TestBuild_EvaluationStateValidation_UnevaluatedProperties(t *testing.T) {
	// {"type": "object", "unevaluatedProperties": false}
	e := ir.All{Operands: []ir.Expr{
		ir.Kinds{Set: plan.SetObject},
		ir.Shape{Detail: ir.ObjectShape{UnevaluatedProperties: ir.Never{}}},
	}}

	got := planner.Build(e, nil)

	require.Equal(t, plan.EvaluationStateValidation, got.Plan.Capability)
	require.Equal(t, plan.UnsupportedConversion, got.Exactness)
	require.NotEmpty(t, got.Diagnostics)
	require.Equal(t, plan.SeverityError, got.Diagnostics[0].Severity)
}

func TestBuild_DynamicSchemaResolution(t *testing.T) {
	// {"$dynamicRef": "#node"}
	e := ir.All{Operands: []ir.Expr{ir.DynamicRef{Anchor: "node"}}}

	got := planner.Build(e, nil)

	require.Equal(t, plan.AnyRepresentation{}, got.Plan.Representation)
	require.Equal(t, plan.DynamicSchemaResolution, got.Plan.Capability)
	require.IsType(t, plan.DynamicReferenceGraph{}, got.Plan.Resolution)
	require.Equal(t, plan.UnsupportedConversion, got.Exactness)
	require.NotEmpty(t, got.Diagnostics)
	require.Equal(t, plan.SeverityError, got.Diagnostics[0].Severity)
}

func TestBuild_Unsupported_UnguardedRecursion(t *testing.T) {
	// A pure allOf/$ref cycle with no object/array descent: `A: allOf: [{$ref: "#/$defs/B"}]`,
	// `B: allOf: [{$ref: "#/$defs/A"}]` never crosses an instance-descent edge, so the
	// reference graph classifies it Unguarded (design §19).
	doc := `{
		"$defs": {
			"A": {"allOf": [{"$ref": "#/$defs/B"}]},
			"B": {"allOf": [{"$ref": "#/$defs/A"}]}
		},
		"$ref": "#/$defs/A"
	}`
	s, err := frontend.Load(context.Background(), []byte(doc), "")
	require.NoError(t, err)

	e := ir.Compile(s.Root)
	got := planner.Build(e, s.Registry)

	require.Equal(t, plan.Unsupported, got.Plan.Capability)
	require.NotEmpty(t, got.Diagnostics)
	found := false
	for _, d := range got.Diagnostics {
		if d.Severity == plan.SeverityError {
			found = true
		}
	}
	require.True(t, found, "expected an error diagnostic for unguarded recursion")
}

func TestBuild_GuardedRecursion(t *testing.T) {
	// Node = null | { value: number, next: Node }: guarded recursion through an object
	// property (an instance-descent edge), so a recursive Go type is representable.
	doc := `{
		"$defs": {
			"Node": {
				"type": "object",
				"properties": {
					"value": {"type": "number"},
					"next": {"$ref": "#/$defs/Node"}
				}
			}
		},
		"$ref": "#/$defs/Node"
	}`
	s, err := frontend.Load(context.Background(), []byte(doc), "")
	require.NoError(t, err)

	e := ir.Compile(s.Root)
	got := planner.Build(e, s.Registry)

	require.NotEqual(t, plan.Unsupported, got.Plan.Capability)
	require.NotEqual(t, plan.DynamicSchemaResolution, got.Plan.Capability)
}

func TestBuild_ThreeStatePresenceAndNullable(t *testing.T) {
	// {"type": "object", "properties": {
	//   "a": {"type": ["string", "null"]},
	//   "b": {"type": "string"}
	// }, "required": ["a"]}
	//
	// "a" is required + nullable -> Nullable[T] territory; "b" is optional + non-null.
	e := ir.All{Operands: []ir.Expr{
		ir.Kinds{Set: plan.SetObject},
		ir.Shape{Detail: ir.ObjectShape{
			Properties: []ir.PropertyExpr{
				{Name: "a", Schema: ir.All{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString | plan.SetNull}}}},
				{Name: "b", Schema: ir.All{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString}}}},
			},
		}},
		ir.Predicate{Guard: plan.SetObject, Detail: ir.RequiredDetail{Properties: []string{"a"}}},
	}}

	got := planner.Build(e, nil)

	obj, ok := got.Plan.Representation.(plan.ObjectRepresentation)
	require.True(t, ok, "expected ObjectRepresentation, got %T", got.Plan.Representation)

	a := obj.Fields["a"]
	require.Equal(t, plan.PresenceRequired, a.Presence)
	require.True(t, a.Nullable)
	require.Equal(t, plan.PrimitiveRepresentation{Kind: plan.KindString}, a.Representation)

	b := obj.Fields["b"]
	require.Equal(t, plan.PresenceOptional, b.Presence)
	require.False(t, b.Nullable)
	require.Equal(t, plan.PrimitiveRepresentation{Kind: plan.KindString}, b.Representation)
}

func TestBuild_TaggedUnionPropertyDispatch(t *testing.T) {
	// {"oneOf": [
	//   {"type":"object","properties":{"kind":{"const":"circle"}, ...},"required":["kind"]},
	//   {"type":"object","properties":{"kind":{"const":"rectangle"}, ...},"required":["kind"]}
	// ]}
	branch := func(tag string) ir.Expr {
		return ir.All{Operands: []ir.Expr{
			ir.Kinds{Set: plan.SetObject},
			ir.Shape{Detail: ir.ObjectShape{
				Properties: []ir.PropertyExpr{
					{Name: "kind", Schema: ir.Literal{Value: tag}},
				},
			}},
			ir.Predicate{Guard: plan.SetObject, Detail: ir.RequiredDetail{Properties: []string{"kind"}}},
		}}
	}
	e := ir.All{Operands: []ir.Expr{
		ir.ExactlyOne{Operands: []ir.Expr{
			branch("circle"),
			branch("rectangle"),
		}},
	}}

	got := planner.Build(e, nil)

	require.Equal(t, plan.StaticDispatch, got.Plan.Capability)
	disp, ok := got.Plan.Dispatch.(plan.PropertyDispatch)
	require.True(t, ok, "expected PropertyDispatch, got %T", got.Plan.Dispatch)
	require.Equal(t, "kind", disp.Property)
	require.Len(t, disp.Cases, 2)
	values := []any{disp.Cases[0].Value, disp.Cases[1].Value}
	require.ElementsMatch(t, []any{"circle", "rectangle"}, values)
}

func TestBuild_PresenceDispatch_DependentSchemas(t *testing.T) {
	// dependentSchemas desugars to AnyOf(Not(Has(p)), All(Has(p), C(S))) (design §12.7).
	has := ir.Predicate{Guard: plan.SetObject, Detail: ir.RequiredDetail{Properties: []string{"credit_card"}}}
	sub := ir.All{Operands: []ir.Expr{
		ir.Kinds{Set: plan.SetObject},
		ir.Predicate{Guard: plan.SetObject, Detail: ir.RequiredDetail{Properties: []string{"billing_address"}}},
	}}
	e := ir.All{Operands: []ir.Expr{
		ir.AnyOf{Operands: []ir.Expr{
			ir.Not{Operand: has},
			ir.All{Operands: []ir.Expr{has, sub}},
		}},
	}}

	got := planner.Build(e, nil)

	disp, ok := got.Plan.Dispatch.(plan.PresenceDispatch)
	require.True(t, ok, "expected PresenceDispatch, got %T", got.Plan.Dispatch)
	require.Equal(t, "credit_card", disp.Property)
	require.Equal(t, plan.StaticDispatch, got.Plan.Capability)
}

func TestBuild_ObjectRepresentation_AdditionalPropertiesFalse(t *testing.T) {
	// {"type":"object","properties":{"a":{"type":"string"}},"additionalProperties":false}
	e := ir.All{Operands: []ir.Expr{
		ir.Kinds{Set: plan.SetObject},
		ir.Shape{Detail: ir.ObjectShape{
			Properties: []ir.PropertyExpr{
				{Name: "a", Schema: ir.All{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString}}}},
			},
			AdditionalProperties: ir.Never{},
		}},
	}}

	got := planner.Build(e, nil)

	obj, ok := got.Plan.Representation.(plan.ObjectRepresentation)
	require.True(t, ok)
	require.Equal(t, plan.NeverRepresentation{}, obj.Additional)
}

func TestBuild_ArrayRepresentation_PrefixAndRest(t *testing.T) {
	// {"type":"array","prefixItems":[{"type":"string"}],"items":{"type":"number"}}
	e := ir.All{Operands: []ir.Expr{
		ir.Kinds{Set: plan.SetArray},
		ir.Shape{Detail: ir.ArrayShape{
			PrefixItems: []ir.Expr{ir.All{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString}}}},
			Items:       ir.All{Operands: []ir.Expr{ir.Kinds{Set: plan.SetNumber}}},
		}},
	}}

	got := planner.Build(e, nil)

	arr, ok := got.Plan.Representation.(plan.ArrayRepresentation)
	require.True(t, ok)
	require.Len(t, arr.Prefix, 1)
	require.Equal(t, plan.PrimitiveRepresentation{Kind: plan.KindString}, arr.Prefix[0])
	require.Equal(t, plan.PrimitiveRepresentation{Kind: plan.KindNumber}, arr.Rest)
}

func TestBuild_Never(t *testing.T) {
	got := planner.Build(ir.Never{}, nil)
	require.Equal(t, plan.NeverRepresentation{}, got.Plan.Representation)
	require.Equal(t, plan.DirectGoType, got.Plan.Capability)
	require.Equal(t, plan.ExactPureRepresentation, got.Exactness)
}

func TestBuild_Any(t *testing.T) {
	got := planner.Build(ir.Any{}, nil)
	require.Equal(t, plan.AnyRepresentation{}, got.Plan.Representation)
	require.Equal(t, plan.DirectGoType, got.Plan.Capability)
	require.Equal(t, plan.ExactPureRepresentation, got.Exactness)
}

func TestBuild_Literal(t *testing.T) {
	e := ir.Literal{Value: "circle"}
	got := planner.Build(e, nil)
	require.Equal(t, plan.StaticDispatch, got.Plan.Capability)
	disp, ok := got.Plan.Dispatch.(plan.LiteralDispatch)
	require.True(t, ok)
	require.Len(t, disp.Cases, 1)
	require.Equal(t, "circle", disp.Cases[0].Value)
}

func TestBuild_ContainsCount_PredicateDispatchWarning(t *testing.T) {
	// {"type":"array","contains":{"type":"string"},"minContains":2}
	e := ir.All{Operands: []ir.Expr{
		ir.Kinds{Set: plan.SetArray},
		ir.Predicate{Guard: plan.SetArray, Detail: ir.ContainsDetail{
			Schema: ir.All{Operands: []ir.Expr{ir.Kinds{Set: plan.SetString}}},
			Min:    ptr(uint64(2)),
		}},
	}}

	got := planner.Build(e, nil)

	require.Equal(t, plan.PredicateDispatch, got.Plan.Capability)
	require.NotEmpty(t, got.Diagnostics)
	found := false
	for _, d := range got.Diagnostics {
		if d.Severity == plan.SeverityWarning {
			found = true
		}
	}
	require.True(t, found)
	require.Len(t, got.Plan.Validation.Predicates, 1)
	cc, ok := got.Plan.Validation.Predicates[0].Expression.(plan.ContainsCountPredicate)
	require.True(t, ok)
	require.Equal(t, uint64(2), cc.Min)
}
