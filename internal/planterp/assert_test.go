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

// TestNumericDomainFromValidation pins the integrality half of `type: "integer"`
// (issue #115). The representation is Any, so a rejection here proves the check reached
// the validation plan rather than staying in the Go shape.
func TestNumericDomainFromValidation(t *testing.T) {
	planFor := func(d plan.NumericDomain) plan.CompilationPlan {
		return plan.CompilationPlan{
			Representation: plan.AnyRepresentation{},
			Validation: plan.ValidationPlan{Predicates: []plan.GuardedPredicate{
				{Applicability: plan.SetNumber, Assert: true},
				{Applicability: plan.SetNumber, Expression: plan.NumericDomainPredicate{Domain: d}},
			}},
			Dispatch:   plan.NoDispatch{},
			Resolution: plan.FullyResolved{},
		}
	}

	tests := []struct {
		name     string
		domain   plan.NumericDomain
		value    any
		accepted bool
	}{
		{name: "integer admits an integer", domain: plan.IntegerOnly, value: float64(2), accepted: true},
		{name: "integer rejects a fraction", domain: plan.IntegerOnly, value: 1.5, accepted: false},
		{name: "non-integer rejects an integer", domain: plan.NonIntegerOnly, value: float64(2), accepted: false},
		{name: "non-integer admits a fraction", domain: plan.NonIntegerOnly, value: 1.5, accepted: true},
		{name: "the kind assertion still rejects a string", domain: plan.IntegerOnly, value: "2", accepted: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := planterp.Interpret(planFor(tt.domain), tt.value)
			require.NoError(t, err)
			require.Equal(t, tt.accepted, v.Accepted, "reason: %v", v.Reason)
		})
	}
}
