package gogen_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/gogen"
	"github.com/ogen-go/schemacompiler/internal/planwalk"
	"github.com/ogen-go/schemacompiler/plan"
)

func str() gogen.GoType  { return &gogen.Primitive{Kind: gogen.PrimitiveString} }
func num() gogen.GoType  { return &gogen.Primitive{Kind: gogen.PrimitiveFloat} }
func i64() gogen.GoType  { return &gogen.Primitive{Kind: gogen.PrimitiveInt} }
func anyT() gogen.GoType { return &gogen.Any{} }

func TestKinds(t *testing.T) {
	node := &gogen.Named{ID: "/$defs/Node", Name: "Node"}
	node.Underlying = &gogen.Struct{Fields: []gogen.Field{
		{Name: "Child", JSON: "child", Type: &gogen.Presence{Elem: &gogen.Pointer{Elem: node}, Optional: true}},
	}}

	for _, tt := range []struct {
		name string
		t    gogen.GoType
		want plan.KindSet
	}{
		{"string", str(), plan.SetString},
		{"int", i64(), plan.SetNumber},
		{"null", &gogen.Primitive{Kind: gogen.PrimitiveNull}, plan.SetNull},
		{"slice", &gogen.Slice{Elem: str()}, plan.SetArray},
		{"map", &gogen.Map{Elem: str()}, plan.SetObject},
		{"struct", &gogen.Struct{}, plan.SetObject},
		{"any", anyT(), plan.SetAny},
		{"never", &gogen.Never{}, 0},
		{"optional adds no kind", &gogen.Presence{Elem: str(), Optional: true}, plan.SetString},
		{"nullable adds null", &gogen.Presence{Elem: str(), Nullable: true}, plan.SetString | plan.SetNull},
		{"sum unions its variants", &gogen.Interface{Variants: []gogen.GoType{str(), num()}}, plan.SetString | plan.SetNumber},
		{"pointer is its element", &gogen.Pointer{Elem: str()}, plan.SetString},
		// The walk must terminate on the cycle the recursion pass left behind.
		{"recursive named", node, plan.SetObject},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, gogen.Kinds(tt.t))
		})
	}
}

func TestClassify(t *testing.T) {
	guard := func(k plan.KindSet, e plan.PredicateExpr) plan.GuardedPredicate {
		return plan.GuardedPredicate{Applicability: k, Expression: e}
	}
	object := func(fields ...gogen.Field) *gogen.Struct {
		return &gogen.Struct{Fields: fields}
	}
	required := func(name string, t gogen.GoType) gogen.Field {
		return gogen.Field{Name: "F", JSON: name, Type: t}
	}
	optional := func(name string, t gogen.GoType) gogen.Field {
		return gogen.Field{Name: "F", JSON: name, Type: &gogen.Presence{Elem: t, Optional: true}}
	}

	for _, tt := range []struct {
		name string
		t    gogen.GoType
		gp   plan.GuardedPredicate
		want gogen.Disposition
	}{
		// A guard the type never enters has nothing to say about it.
		{"minLength on a number", i64(), guard(plan.SetString, &plan.MinLengthPredicate{Value: 2}), gogen.Discharged},
		{"minimum on a string", str(), guard(plan.SetNumber, &plan.MinimumPredicate{}), gogen.Discharged},

		// An assertion is spent when the type cannot hold anything outside the guard.
		{
			"kind assertion the type makes", str(),
			plan.GuardedPredicate{Applicability: plan.SetString, Assert: true},
			gogen.Discharged,
		},
		{
			"kind assertion the type does not make", anyT(),
			plan.GuardedPredicate{Applicability: plan.SetString, Assert: true},
			gogen.Delegate,
		},

		// Values the type keeps structurally.
		{"minLength on a string", str(), guard(plan.SetString, &plan.MinLengthPredicate{Value: 2}), gogen.Inline},
		{"format on a string", str(), guard(plan.SetString, &plan.FormatPredicate{Format: "uuid"}), gogen.Inline},
		{"pattern on a string", str(), guard(plan.SetString, &plan.PatternPredicate{Regex: "^a"}), gogen.Inline},
		{"minimum on a number", num(), guard(plan.SetNumber, &plan.MinimumPredicate{}), gogen.Inline},
		{
			"maxItems on a slice", &gogen.Slice{Elem: str()},
			guard(plan.SetArray, &plan.MaxItemsPredicate{Value: 3}), gogen.Inline,
		},
		{
			"maxItems on a tuple", &gogen.Tuple{Elems: []gogen.GoType{str()}},
			guard(plan.SetArray, &plan.MaxItemsPredicate{Value: 3}), gogen.Inline,
		},
		{
			"minProperties on a map", &gogen.Map{Elem: str()},
			guard(plan.SetObject, &plan.MinPropertiesPredicate{Value: 1}), gogen.Inline,
		},

		// Raw storage keeps everything and exposes nothing.
		{"minLength on any", anyT(), guard(plan.SetString, &plan.MinLengthPredicate{Value: 2}), gogen.Delegate},
		{"minimum on any", anyT(), guard(plan.SetNumber, &plan.MinimumPredicate{}), gogen.Delegate},
		{"required on any", anyT(), guard(plan.SetObject, &plan.RequiredPredicate{Properties: []string{"a"}}), gogen.Delegate},

		// A sum is only as good as the variants the guard can reach.
		{
			"string check over a typed sum", &gogen.Interface{Variants: []gogen.GoType{str(), num()}},
			guard(plan.SetString, &plan.MinLengthPredicate{Value: 2}), gogen.Inline,
		},
		{
			"string check over a sum with a raw variant", &gogen.Interface{Variants: []gogen.GoType{anyT(), num()}},
			guard(plan.SetString, &plan.MinLengthPredicate{Value: 2}), gogen.Delegate,
		},
		{
			"number check ignores the raw string variant", &gogen.Interface{Variants: []gogen.GoType{num(), str()}},
			guard(plan.SetNumber, &plan.MinimumPredicate{}), gogen.Inline,
		},

		// `required` over a field that cannot record absence is the type's own claim.
		{
			"required, field not optional", object(required("a", str())),
			guard(plan.SetObject, &plan.RequiredPredicate{Properties: []string{"a"}}), gogen.Discharged,
		},
		{
			"required, field optional", object(optional("a", str())),
			guard(plan.SetObject, &plan.RequiredPredicate{Properties: []string{"a"}}), gogen.Inline,
		},
		{
			"required, no such field", object(required("b", str())),
			guard(plan.SetObject, &plan.RequiredPredicate{Properties: []string{"a"}}), gogen.Inline,
		},

		// An integer type states its own numeric domain.
		{
			"integer domain on int", i64(),
			guard(plan.SetNumber, &plan.NumericDomainPredicate{Domain: plan.IntegerOnly}), gogen.Discharged,
		},
		{
			"integer domain on float", num(),
			guard(plan.SetNumber, &plan.NumericDomainPredicate{Domain: plan.IntegerOnly}), gogen.Inline,
		},

		// Uniqueness compares whole values, so nothing beneath may be raw.
		{
			"uniqueItems over typed elements", &gogen.Slice{Elem: str()},
			guard(plan.SetArray, &plan.UniqueItemsPredicate{}), gogen.Inline,
		},
		{
			"uniqueItems over raw elements", &gogen.Slice{Elem: anyT()},
			guard(plan.SetArray, &plan.UniqueItemsPredicate{}), gogen.Delegate,
		},
		{
			"uniqueItems over a struct hiding a raw slot", &gogen.Slice{Elem: &gogen.Struct{Additional: anyT()}},
			guard(plan.SetArray, &plan.UniqueItemsPredicate{}), gogen.Delegate,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, gogen.Classify(tt.t, tt.gp))
		})
	}
}

// TestRawEvaluationAlwaysDelegates pins the identity docs/backend.md §10 rests on: the four
// predicates no Go type preserves are exactly [plan.RawEvaluation]'s members. Reading them
// off a fully typed value must not help, or the two ends of the ladder mean different
// things.
func TestRawEvaluationAlwaysDelegates(t *testing.T) {
	sub := plan.CompilationPlan{Representation: &plan.PrimitiveRepresentation{Kind: plan.KindString}}
	for _, e := range []plan.PredicateExpr{
		&plan.NegationPredicate{Schema: sub},
		&plan.ShapePredicate{Schema: sub},
		&plan.ContainsCountPredicate{Schema: sub, Min: 1},
		&plan.PropertyNamesPredicate{Schema: sub},
	} {
		for _, target := range []gogen.GoType{str(), &gogen.Slice{Elem: str()}, &gogen.Map{Elem: str()}} {
			require.Equalf(t, gogen.Delegate,
				gogen.Classify(target, plan.GuardedPredicate{Applicability: plan.SetAny, Expression: e}),
				"%T over %T", e, target)
		}
	}
}

// TestClassifyIsExhaustive drives every predicate variant through the table. The default
// case panics rather than guessing, so a variant added to `plan` and not classified here
// fails loudly instead of being silently inlined or silently delegated.
func TestClassifyIsExhaustive(t *testing.T) {
	targets := []gogen.GoType{
		str(), num(), i64(), anyT(), &gogen.Never{},
		&gogen.Slice{Elem: str()}, &gogen.Map{Elem: str()},
		&gogen.Struct{Fields: []gogen.Field{{Name: "F", JSON: "a", Type: str()}}},
		&gogen.Tuple{Elems: []gogen.GoType{str()}},
		&gogen.Interface{Variants: []gogen.GoType{str(), num()}},
		&gogen.Presence{Elem: str(), Optional: true, Nullable: true},
		&gogen.Pointer{Elem: str()},
	}
	for _, e := range planwalk.AllPredicateExprs() {
		for _, target := range targets {
			require.NotPanicsf(t, func() {
				gogen.Classify(target, plan.GuardedPredicate{Applicability: plan.SetAny, Expression: e})
			}, "%T over %T", e, target)
		}
	}
	require.NotPanics(t, func() {
		gogen.Classify(str(), plan.GuardedPredicate{Applicability: plan.SetAny})
	}, "a bare kind guard carries no expression")
}
