package planwalk_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/planwalk"
	"github.com/ogen-go/schemacompiler/plan"
)

func TestRepresentationHandlesEveryVariant(t *testing.T) {
	for _, r := range planwalk.AllRepresentations() {
		t.Run(typeName(r), func(t *testing.T) {
			var seen []plan.Representation
			require.NotPanics(t, func() {
				planwalk.Representation(r, func(v plan.Representation) { seen = append(seen, v) })
			})
			require.Equal(t, []plan.Representation{r}, seen)
		})
	}
}

func TestDispatchHandlesEveryVariant(t *testing.T) {
	for _, d := range planwalk.AllDispatchPlans() {
		t.Run(typeName(d), func(t *testing.T) {
			require.NotPanics(t, func() {
				planwalk.Dispatch(d, func(plan.CompilationPlan) {})
			})
		})
	}
}

func TestPredicateHandlesEveryVariant(t *testing.T) {
	for _, e := range planwalk.AllPredicateExprs() {
		t.Run(typeName(e), func(t *testing.T) {
			require.NotPanics(t, func() {
				planwalk.Predicate(e, func(plan.CompilationPlan) {})
			})
		})
	}
}

type unknownRepresentation struct{ plan.Representation }

type unknownDispatch struct{ plan.DispatchPlan }

type unknownPredicate struct{ plan.PredicateExpr }

func TestUnknownVariantPanics(t *testing.T) {
	require.PanicsWithValue(t,
		"planwalk: unhandled plan.Representation variant planwalk_test.unknownRepresentation",
		func() { planwalk.Representation(unknownRepresentation{}, func(plan.Representation) {}) })

	require.PanicsWithValue(t,
		"planwalk: unhandled plan.DispatchPlan variant planwalk_test.unknownDispatch",
		func() { planwalk.Dispatch(unknownDispatch{}, func(plan.CompilationPlan) {}) })

	require.PanicsWithValue(t,
		"planwalk: unhandled plan.PredicateExpr variant planwalk_test.unknownPredicate",
		func() { planwalk.Predicate(unknownPredicate{}, func(plan.CompilationPlan) {}) })
}

func TestNilVisitsNothing(t *testing.T) {
	require.NotPanics(t, func() {
		planwalk.Representation(nil, func(plan.Representation) { t.Fatal("visited") })
		planwalk.Dispatch(nil, func(plan.CompilationPlan) { t.Fatal("visited") })
		planwalk.Predicate(nil, func(plan.CompilationPlan) { t.Fatal("visited") })
	})
}

// TestPlanReachesEveryNestingSite pins that a reference is found in every position the
// plan structure can hide one, which is what the capability roll-up depends on.
func TestPlanReachesEveryNestingSite(t *testing.T) {
	ref := func(name string) plan.Representation { return &plan.ReferenceRepresentation{Name: name} }
	leaf := func(name string) plan.CompilationPlan {
		return plan.CompilationPlan{Representation: ref(name)}
	}

	p := plan.CompilationPlan{
		Representation: &plan.ObjectRepresentation{
			Fields: []plan.FieldRepresentation{
				{Name: "f", Plan: plan.CompilationPlan{Representation: ref("field")}},
			},
			Additional: &plan.CompilationPlan{Representation: ref("additional")},
			PatternRules: []plan.PatternFieldRepresentation{
				{Pattern: "^x", Plan: plan.CompilationPlan{Representation: ref("pattern")}},
			},
		},
		Dispatch: &plan.KindDispatch{Cases: map[plan.JSONKind]plan.CompilationPlan{
			plan.KindObject: {
				Representation: &plan.ArrayRepresentation{
					Prefix: []plan.ItemRepresentation{{Plan: plan.CompilationPlan{Representation: ref("prefix")}}},
					Rest:   plan.ItemRepresentation{Plan: plan.CompilationPlan{Representation: ref("rest")}},
				},
				Dispatch: &plan.PresenceDispatch{
					Present: leaf("present"),
					Absent:  leaf("absent"),
				},
			},
		}},
		Validation: plan.ValidationPlan{Predicates: []plan.GuardedPredicate{
			{Expression: &plan.ContainsCountPredicate{Schema: plan.CompilationPlan{
				Representation: &plan.UnionRepresentation{Alternatives: []plan.Representation{ref("alternative")}},
			}}},
			{Expression: &plan.PropertyNamesPredicate{Schema: plan.CompilationPlan{
				Representation: &plan.UnionRepresentation{Alternatives: []plan.Representation{ref("body")}},
			}}},
		}},
	}

	var names []string
	planwalk.Plan(p, func(r plan.Representation) {
		if v, ok := r.(*plan.ReferenceRepresentation); ok {
			names = append(names, v.Name)
		}
	})
	require.ElementsMatch(t, []string{
		"field", "additional", "pattern",
		"prefix", "rest", "present", "absent",
		"alternative", "body",
	}, names)
}

func typeName(v any) string {
	return fmt.Sprintf("%T", v)
}
