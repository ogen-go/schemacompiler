package ir

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/frontend"
	"github.com/ogen-go/schemacompiler/plan"
)

func ptr[T any](v T) *T { return &v }

func TestCompile_Boolean(t *testing.T) {
	require.Equal(t, Any{}, Compile(&frontend.Node{Always: ptr(true)}))
	require.Equal(t, Never{}, Compile(&frontend.Node{Always: ptr(false)}))
}

func TestCompile_UnguardedPredicate(t *testing.T) {
	// {"minLength": 5} must NOT assert a string type: it is a guarded predicate that
	// accepts every non-string value (design §3, core invariant 1).
	n := &frontend.Node{MinLength: ptr(uint64(5))}
	got := Compile(n)

	want := All{Operands: []Expr{
		Predicate{Guard: plan.SetString, Detail: MinLengthDetail{Value: 5}},
	}}
	require.Equal(t, want, got)
	require.Equal(t, plan.SetAny, got.Kinds(), "guarded predicate accepts every kind")
}

func TestCompile_PropertiesWithoutType(t *testing.T) {
	// `properties` alone must not imply an object type assertion.
	n := &frontend.Node{
		Properties: []frontend.NamedSchema{
			{Name: "name", Schema: &frontend.Node{HasType: true, Types: frontend.KindString}},
		},
	}
	got := Compile(n)

	want := All{Operands: []Expr{
		Shape{Detail: ObjectShape{
			Properties: []PropertyExpr{
				{Name: "name", Schema: All{Operands: []Expr{
					Kinds{Set: plan.SetString, Numeric: plan.AnyNumber},
				}}},
			},
		}},
	}}
	require.Equal(t, want, got)
	require.Equal(t, plan.SetAny, got.Kinds(), "bare shape accepts every kind")
}

func TestCompile_Type(t *testing.T) {
	cases := []struct {
		name string
		node *frontend.Node
		want Kinds
	}{
		{
			"string",
			&frontend.Node{HasType: true, Types: frontend.KindString},
			Kinds{Set: plan.SetString, Numeric: plan.AnyNumber},
		},
		{
			"integer",
			&frontend.Node{HasType: true, Types: frontend.KindNumber, IntegerType: true},
			Kinds{Set: plan.SetNumber, Numeric: plan.IntegerOnly},
		},
		{
			"array of types",
			&frontend.Node{HasType: true, Types: frontend.KindString | frontend.KindNumber},
			Kinds{Set: plan.SetString | plan.SetNumber, Numeric: plan.AnyNumber},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Compile(tc.node)
			require.Equal(t, All{Operands: []Expr{tc.want}}, got)
		})
	}
}

func TestCompile_ConstEnum(t *testing.T) {
	constNode := &frontend.Node{Const: &frontend.Value{Decoded: "x"}}
	require.Equal(t,
		All{Operands: []Expr{Literal{Value: "x"}}},
		Compile(constNode))

	enumNode := &frontend.Node{Enum: []frontend.Value{{Decoded: "a"}, {Decoded: "b"}}}
	require.Equal(t,
		All{Operands: []Expr{AnyOf{Operands: []Expr{
			Literal{Value: "a"}, Literal{Value: "b"},
		}}}},
		Compile(enumNode))
}

func TestCompile_AllOfAnyOfOneOfNot(t *testing.T) {
	sub := func() *frontend.Node { return &frontend.Node{HasType: true, Types: frontend.KindString} }
	subExpr := All{Operands: []Expr{Kinds{Set: plan.SetString, Numeric: plan.AnyNumber}}}

	t.Run("allOf", func(t *testing.T) {
		got := Compile(&frontend.Node{AllOf: []*frontend.Node{sub(), sub()}})
		require.Equal(t, All{Operands: []Expr{All{Operands: []Expr{subExpr, subExpr}}}}, got)
	})
	t.Run("anyOf", func(t *testing.T) {
		got := Compile(&frontend.Node{AnyOf: []*frontend.Node{sub(), sub()}})
		require.Equal(t, All{Operands: []Expr{AnyOf{Operands: []Expr{subExpr, subExpr}}}}, got)
	})
	t.Run("oneOf stays ExactlyOne, never flattened", func(t *testing.T) {
		got := Compile(&frontend.Node{OneOf: []*frontend.Node{sub(), sub()}})
		require.Equal(t, All{Operands: []Expr{ExactlyOne{Operands: []Expr{subExpr, subExpr}}}}, got)
		require.IsType(t, ExactlyOne{}, got.(All).Operands[0])
	})
	t.Run("not", func(t *testing.T) {
		got := Compile(&frontend.Node{Not: sub()})
		require.Equal(t, All{Operands: []Expr{Not{Operand: subExpr}}}, got)
	})
}

func TestCompile_IfThenElse(t *testing.T) {
	ifN := &frontend.Node{HasType: true, Types: frontend.KindString}
	thenN := &frontend.Node{MinLength: ptr(uint64(1))}
	elseN := &frontend.Node{HasType: true, Types: frontend.KindNumber}

	got := Compile(&frontend.Node{If: ifN, Then: thenN, Else: elseN})

	p := Compile(ifN)
	tExpr := Compile(thenN)
	eExpr := Compile(elseN)
	want := All{Operands: []Expr{
		AnyOf{Operands: []Expr{
			All{Operands: []Expr{p, tExpr}},
			All{Operands: []Expr{Not{Operand: p}, eExpr}},
		}},
	}}
	require.Equal(t, want, got)
}

func TestCompile_IfThenElse_DefaultsToTrue(t *testing.T) {
	// Absent `then`/`else` behave as the empty schema `true` (Any).
	ifN := &frontend.Node{HasType: true, Types: frontend.KindString}
	got := Compile(&frontend.Node{If: ifN})

	p := Compile(ifN)
	want := All{Operands: []Expr{
		AnyOf{Operands: []Expr{
			All{Operands: []Expr{p, Any{}}},
			All{Operands: []Expr{Not{Operand: p}, Any{}}},
		}},
	}}
	require.Equal(t, want, got)
}

func TestCompile_NumericFamily(t *testing.T) {
	n := &frontend.Node{
		Minimum:          ptr(1.0),
		Maximum:          ptr(2.0),
		ExclusiveMinimum: ptr(3.0),
		ExclusiveMaximum: ptr(4.0),
		MultipleOf:       ptr(5.0),
	}
	got := Compile(n).(All)
	require.Len(t, got.Operands, 5)
	for _, o := range got.Operands {
		p, ok := o.(Predicate)
		require.True(t, ok)
		require.Equal(t, plan.SetNumber, p.Guard)
	}
}

func TestCompile_StringFamily(t *testing.T) {
	n := &frontend.Node{
		MinLength: ptr(uint64(1)),
		MaxLength: ptr(uint64(2)),
		Pattern:   ptr("^a"),
		Format:    "date-time",
	}
	got := Compile(n).(All)
	require.Equal(t, []Expr{
		Predicate{Guard: plan.SetString, Detail: MinLengthDetail{Value: 1}},
		Predicate{Guard: plan.SetString, Detail: MaxLengthDetail{Value: 2}},
		Predicate{Guard: plan.SetString, Detail: PatternDetail{Regex: "^a"}},
		Predicate{Guard: plan.SetString, Detail: FormatDetail{Format: "date-time"}},
	}, got.Operands)
}

func TestCompile_ArrayFamily(t *testing.T) {
	item := &frontend.Node{HasType: true, Types: frontend.KindNumber}
	n := &frontend.Node{
		PrefixItems: []*frontend.Node{{HasType: true, Types: frontend.KindString}},
		Items:       item,
		MinItems:    ptr(uint64(1)),
		MaxItems:    ptr(uint64(2)),
		UniqueItems: true,
		Contains:    item,
		MinContains: ptr(uint64(1)),
	}
	got := Compile(n).(All)
	require.Len(t, got.Operands, 5)

	shape, ok := got.Operands[0].(Shape)
	require.True(t, ok)
	arrShape, ok := shape.Detail.(ArrayShape)
	require.True(t, ok)
	require.Len(t, arrShape.PrefixItems, 1)
	require.Equal(t, Compile(item), arrShape.Items)

	require.Equal(t, Predicate{Guard: plan.SetArray, Detail: MinItemsDetail{Value: 1}}, got.Operands[1])
	require.Equal(t, Predicate{Guard: plan.SetArray, Detail: MaxItemsDetail{Value: 2}}, got.Operands[2])
	require.Equal(t, Predicate{Guard: plan.SetArray, Detail: UniqueItemsDetail{}}, got.Operands[3])

	containsPred, ok := got.Operands[4].(Predicate)
	require.True(t, ok)
	require.Equal(t, plan.SetArray, containsPred.Guard)
	containsDetail, ok := containsPred.Detail.(ContainsDetail)
	require.True(t, ok)
	require.Equal(t, Compile(item), containsDetail.Schema)
	require.Equal(t, ptr(uint64(1)), containsDetail.Min)
}

func TestCompile_ObjectFamily(t *testing.T) {
	n := &frontend.Node{
		Required:      []string{"a", "b"},
		MinProperties: ptr(uint64(1)),
		MaxProperties: ptr(uint64(2)),
		DependentRequired: []frontend.DependentRequired{
			{Property: "a", Requires: []string{"b"}},
		},
		PropertyNames: &frontend.Node{MinLength: ptr(uint64(1))},
	}
	got := Compile(n).(All)
	require.Equal(t, []Expr{
		Predicate{Guard: plan.SetObject, Detail: RequiredDetail{Properties: []string{"a", "b"}}},
		Predicate{Guard: plan.SetObject, Detail: MinPropertiesDetail{Value: 1}},
		Predicate{Guard: plan.SetObject, Detail: MaxPropertiesDetail{Value: 2}},
		Predicate{Guard: plan.SetObject, Detail: DependentRequiredDetail{Entries: []DependentRequiredEntry{
			{Property: "a", Requires: []string{"b"}},
		}}},
		Predicate{Guard: plan.SetObject, Detail: PropertyNamesDetail{Schema: Compile(n.PropertyNames)}},
	}, got.Operands)
}

func TestCompile_DependentSchemas(t *testing.T) {
	sub := &frontend.Node{HasType: true, Types: frontend.KindString}
	n := &frontend.Node{
		DependentSchemas: []frontend.NamedSchema{{Name: "a", Schema: sub}},
	}
	got := Compile(n).(All)
	require.Len(t, got.Operands, 1)

	has := Predicate{Guard: plan.SetObject, Detail: RequiredDetail{Properties: []string{"a"}}}
	want := AnyOf{Operands: []Expr{
		Not{Operand: has},
		All{Operands: []Expr{has, Compile(sub)}},
	}}
	require.Equal(t, want, got.Operands[0])
}

func TestCompile_Ref(t *testing.T) {
	t.Run("unresolved", func(t *testing.T) {
		got := Compile(&frontend.Node{Ref: "#/$defs/foo"})
		require.Equal(t, All{Operands: []Expr{Ref{Target: "#/$defs/foo"}}}, got)
	})
	t.Run("resolved uses target pointer", func(t *testing.T) {
		target := &frontend.Node{Pointer: "#/$defs/foo"}
		got := Compile(&frontend.Node{Ref: "#/$defs/foo", Resolved: target})
		require.Equal(t, All{Operands: []Expr{Ref{Target: "#/$defs/foo"}}}, got)
	})
	t.Run("dynamicRef", func(t *testing.T) {
		got := Compile(&frontend.Node{DynamicRef: "#node"})
		require.Equal(t, All{Operands: []Expr{DynamicRef{Anchor: "#node"}}}, got)
	})
}

func TestCompile_UnevaluatedAndAdditional(t *testing.T) {
	additional := &frontend.Node{Always: ptr(false)}
	unevaluated := &frontend.Node{Always: ptr(true)}
	n := &frontend.Node{
		AdditionalProperties:  additional,
		UnevaluatedProperties: unevaluated,
	}
	got := Compile(n).(All)
	require.Len(t, got.Operands, 1)
	shape := got.Operands[0].(Shape).Detail.(ObjectShape)
	require.Equal(t, Never{}, shape.AdditionalProperties)
	require.Equal(t, Any{}, shape.UnevaluatedProperties)
}
