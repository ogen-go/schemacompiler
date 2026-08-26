package planwalk_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/planwalk"
	"github.com/ogen-go/schemacompiler/plan"
)

func TestNilPlanSlots(t *testing.T) {
	stated := plan.CompilationPlan{Representation: &plan.AnyRepresentation{}}

	for _, tt := range []struct {
		name string
		plan plan.CompilationPlan
		want string
	}{
		{
			name: "object additional stated",
			plan: plan.CompilationPlan{Representation: &plan.ObjectRepresentation{Additional: &stated}},
		},
		{
			name: "object additional absent",
			plan: plan.CompilationPlan{Representation: &plan.ObjectRepresentation{}},
			want: "*plan.ObjectRepresentation.Additional",
		},
		{
			name: "array rest absent",
			plan: plan.CompilationPlan{Representation: &plan.ArrayRepresentation{}},
			want: "*plan.ArrayRepresentation.Rest.Plan.Representation",
		},
		{
			name: "object structure additional absent",
			plan: plan.CompilationPlan{Validation: plan.ValidationPlan{
				Predicates: []plan.GuardedPredicate{{Expression: &plan.ObjectStructurePredicate{}}},
			}},
			want: "*plan.ObjectStructurePredicate.Additional",
		},
		{
			name: "array structure rest absent",
			plan: plan.CompilationPlan{Validation: plan.ValidationPlan{
				Predicates: []plan.GuardedPredicate{{Expression: &plan.ArrayStructurePredicate{}}},
			}},
			want: "*plan.ArrayStructurePredicate.Rest",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := planwalk.NilPlanSlots(tt.plan)
			if tt.want == "" {
				require.Empty(t, got)
				return
			}
			require.Len(t, got, 1)
			require.Equal(t, tt.want, got[0].String())
		})
	}
}

func TestUndispatchedUnions(t *testing.T) {
	union := &plan.UnionRepresentation{Alternatives: []plan.Representation{
		&plan.PrimitiveRepresentation{Kind: plan.KindString},
		&plan.PrimitiveRepresentation{Kind: plan.KindNumber},
	}}

	require.Empty(t, planwalk.UndispatchedUnions(plan.CompilationPlan{
		Representation: union,
		Dispatch: &plan.KindDispatch{Cases: map[plan.JSONKind]plan.CompilationPlan{
			plan.KindString: {Representation: &plan.PrimitiveRepresentation{Kind: plan.KindString}},
		}},
	}))

	require.Len(t, planwalk.UndispatchedUnions(plan.CompilationPlan{
		Representation: union,
		Dispatch:       &plan.NoDispatch{},
	}), 1)
}

func TestContractViolationsAggregates(t *testing.T) {
	require.Empty(t, planwalk.ContractViolations(plan.CompilationPlan{
		Representation: &plan.AnyRepresentation{},
	}))
	require.Len(t, planwalk.ContractViolations(plan.CompilationPlan{
		Representation: &plan.ObjectRepresentation{},
		Validation: plan.ValidationPlan{Predicates: []plan.GuardedPredicate{
			{Applicability: plan.SetAny, Expression: &plan.MinLengthPredicate{Value: 1}},
		}},
	}), 2)
}
