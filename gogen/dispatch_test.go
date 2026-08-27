package gogen_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/gogen"
	"github.com/ogen-go/schemacompiler/internal/planwalk"
	"github.com/ogen-go/schemacompiler/plan"
)

func TestClassifyDispatch(t *testing.T) {
	enum := &gogen.Enum{Elem: &gogen.Primitive{Kind: gogen.PrimitiveString}}
	sum := &gogen.Interface{Variants: []gogen.GoType{&gogen.Primitive{Kind: gogen.PrimitiveString}}}
	literal := &plan.LiteralDispatch{Cases: []plan.LiteralCase{{Value: "a"}}}

	for _, tt := range []struct {
		name string
		typ  gogen.GoType
		disp plan.DispatchPlan
		want gogen.DispatchCheck
	}{
		{"nil", sum, nil, gogen.DispatchCheck{}},
		{"none", sum, &plan.NoDispatch{}, gogen.DispatchCheck{}},
		{"folded literal", enum, literal, gogen.DispatchCheck{Kind: gogen.DispatchLiteral, Disposition: gogen.Discharged}},
		{
			"folded literal under a name", &gogen.Named{Underlying: enum}, literal,
			gogen.DispatchCheck{Kind: gogen.DispatchLiteral, Disposition: gogen.Discharged},
		},
		{"unfolded literal", sum, literal, gogen.DispatchCheck{Kind: gogen.DispatchLiteral, Disposition: gogen.Inline}},
		{"empty literal", sum, &plan.LiteralDispatch{}, gogen.DispatchCheck{Kind: gogen.DispatchLiteral, Disposition: gogen.Discharged}},
		{"kind", sum, &plan.KindDispatch{}, gogen.DispatchCheck{Kind: gogen.DispatchJSONKind, Disposition: gogen.Inline}},
		{"property", sum, &plan.PropertyDispatch{}, gogen.DispatchCheck{Kind: gogen.DispatchProperty, Disposition: gogen.Inline}},
		{"presence", sum, &plan.PresenceDispatch{}, gogen.DispatchCheck{Kind: gogen.DispatchPresence, Disposition: gogen.Inline}},
		{
			"predicate count", sum, &plan.PredicateCountDispatch{},
			gogen.DispatchCheck{Kind: gogen.DispatchPredicateCount, Disposition: gogen.Delegate},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, gogen.ClassifyDispatch(tt.typ, tt.disp))
		})
	}
}

// TestClassifyDispatchIsExhaustive ties a new [plan.DispatchPlan] variant to the switch
// that must learn it. Defaulting silently is the failure issue #155 is about: the zero
// [gogen.DispatchCheck] reads as "nothing to select", which is the one answer a backend
// cannot recover from later.
func TestClassifyDispatchIsExhaustive(t *testing.T) {
	for _, d := range planwalk.AllDispatchPlans() {
		require.NotPanicsf(t, func() {
			gogen.ClassifyDispatch(&gogen.Any{}, d)
		}, "%T", d)
	}
}

func TestDispatchKindString(t *testing.T) {
	require.Equal(t, "predicate-count", gogen.DispatchPredicateCount.String())
	require.Equal(t, "dispatch(?)", gogen.DispatchKind(200).String())
}
