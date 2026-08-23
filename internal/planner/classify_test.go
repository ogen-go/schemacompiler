package planner

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/plan"
)

type unknownDispatch struct{ plan.DispatchPlan }

type unknownRepresentation struct{ plan.Representation }

func TestClassifyUnknownVariantsAreUnsupported(t *testing.T) {
	tests := []struct {
		name string
		rep  plan.Representation
		disp plan.DispatchPlan
		want plan.CapabilityLevel
	}{
		{
			name: "unknown dispatch",
			rep:  &plan.PrimitiveRepresentation{Kind: plan.KindString},
			disp: unknownDispatch{},
			want: plan.Unsupported,
		},
		{
			name: "unknown representation",
			rep:  unknownRepresentation{},
			disp: &plan.NoDispatch{},
			want: plan.Unsupported,
		},
		{
			name: "nil dispatch",
			rep:  &plan.PrimitiveRepresentation{Kind: plan.KindString},
			disp: nil,
			want: plan.Unsupported,
		},
		{
			name: "known dispatch and representation",
			rep:  &plan.PrimitiveRepresentation{Kind: plan.KindString},
			disp: &plan.NoDispatch{},
			want: plan.DirectGoType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classify(tt.rep, plan.ValidationPlan{}, tt.disp, &plan.FullyResolved{})
			require.Equal(t, tt.want, got)
		})
	}
}

func TestClassifyUnknownDispatchOutranksValidation(t *testing.T) {
	val := plan.ValidationPlan{Predicates: []plan.GuardedPredicate{{
		Expression: &plan.MinLengthPredicate{Value: 1},
	}}}
	require.False(t, val.Empty())

	require.Equal(t, plan.Unsupported,
		classify(&plan.PrimitiveRepresentation{Kind: plan.KindString}, val, unknownDispatch{}, &plan.FullyResolved{}))
	require.Equal(t, plan.GoTypeWithValidation,
		classify(&plan.PrimitiveRepresentation{Kind: plan.KindString}, val, &plan.NoDispatch{}, &plan.FullyResolved{}))
}

func TestClassifyCoversEveryDispatchVariant(t *testing.T) {
	for _, tt := range []struct {
		disp plan.DispatchPlan
		want plan.CapabilityLevel
	}{
		{&plan.NoDispatch{}, plan.DirectGoType},
		{&plan.KindDispatch{}, plan.StaticDispatch},
		{&plan.LiteralDispatch{}, plan.StaticDispatch},
		{&plan.PropertyDispatch{}, plan.StaticDispatch},
		{&plan.PresenceDispatch{}, plan.StaticDispatch},
		{&plan.PredicateCountDispatch{}, plan.PredicateDispatch},
	} {
		got := classify(&plan.PrimitiveRepresentation{Kind: plan.KindString}, plan.ValidationPlan{}, tt.disp, &plan.FullyResolved{})
		require.Equalf(t, tt.want, got, "%T", tt.disp)
	}
}
