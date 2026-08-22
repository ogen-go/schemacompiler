package planterp_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/planterp"
	"github.com/ogen-go/schemacompiler/plan"
)

// TestAssertVsGuard pins design §3.1's two readings of Applicability (issue #115). The
// two predicates below differ only in Assert, and a number is outside both guards: the
// guard excuses it, the assertion rejects it.
//
// The representation is Any throughout, so the verdicts come from the validation plan
// alone — which is the property design §4.1 makes the plan's contract.
func TestAssertVsGuard(t *testing.T) {
	planWith := func(gp plan.GuardedPredicate) plan.CompilationPlan {
		return plan.CompilationPlan{
			Representation: plan.AnyRepresentation{},
			Validation:     plan.ValidationPlan{Predicates: []plan.GuardedPredicate{gp}},
			Dispatch:       plan.NoDispatch{},
			Resolution:     plan.FullyResolved{},
		}
	}

	tests := []struct {
		name      string
		predicate plan.GuardedPredicate
		value     any
		accepted  bool
	}{
		{
			name:      "guard excuses another kind",
			predicate: plan.GuardedPredicate{Applicability: plan.SetString, Expression: plan.MinLengthPredicate{Value: 3}},
			value:     float64(1),
			accepted:  true,
		},
		{
			name:      "guard still applies to its own kind",
			predicate: plan.GuardedPredicate{Applicability: plan.SetString, Expression: plan.MinLengthPredicate{Value: 3}},
			value:     "ab",
			accepted:  false,
		},
		{
			name:      "assertion rejects another kind",
			predicate: plan.GuardedPredicate{Applicability: plan.SetString, Assert: true},
			value:     float64(1),
			accepted:  false,
		},
		{
			name:      "assertion admits its own kind",
			predicate: plan.GuardedPredicate{Applicability: plan.SetString, Assert: true},
			value:     "ab",
			accepted:  true,
		},
		{
			name: "an assertion carrying an expression checks both",
			predicate: plan.GuardedPredicate{
				Applicability: plan.SetString,
				Assert:        true,
				Expression:    plan.MinLengthPredicate{Value: 3},
			},
			value:    "ab",
			accepted: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := planterp.Interpret(planWith(tt.predicate), tt.value)
			require.NoError(t, err)
			require.Equal(t, tt.accepted, v.Accepted, "reason: %v", v.Reason)
		})
	}
}
