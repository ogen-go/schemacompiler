package planwalk_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/planwalk"
	"github.com/ogen-go/schemacompiler/plan"
)

// TestMeaningfulKindsIsExhaustive fails when a [plan.PredicateExpr] variant is added
// without deciding which kinds it reads. An unlisted variant is skipped by
// [planwalk.OverbroadGuards] rather than checked, so the guard that would have caught the
// next issue #32 would quietly stop covering it.
func TestMeaningfulKindsIsExhaustive(t *testing.T) {
	for _, e := range planwalk.AllPredicateExprs() {
		_, known := planwalk.MeaningfulKinds(e)
		require.Truef(t, known, "%T has no meaningful kind set", e)
	}
}

func TestOverbroadGuards(t *testing.T) {
	guarded := func(k plan.KindSet) plan.CompilationPlan {
		return plan.CompilationPlan{Validation: plan.ValidationPlan{
			Predicates: []plan.GuardedPredicate{{
				Applicability: k,
				Expression:    &plan.MinLengthPredicate{Value: 1},
			}},
		}}
	}
	require.Empty(t, planwalk.OverbroadGuards(guarded(plan.SetString)))

	found := planwalk.OverbroadGuards(guarded(plan.SetString | plan.SetNumber))
	require.Len(t, found, 1)
	require.Equal(t, plan.SetString, found[0].Meaning)
}
