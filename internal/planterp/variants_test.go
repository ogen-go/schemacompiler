package planterp_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/planterp"
	"github.com/ogen-go/schemacompiler/internal/planwalk"
	"github.com/ogen-go/schemacompiler/plan"
)

// instances covers one value of every JSON kind, so a variant is driven through the
// interpreter for each kind its guard might select.
var instances = []any{nil, true, 1.0, "s", []any{1.0}, map[string]any{"a": 1.0}}

// TestEveryVariantIsHandled drives one zero value of every plan interface variant
// through the interpreter and asserts none of them reaches an "unhandled variant"
// default. A variant added to plan and to planwalk but not here fails loudly rather than
// being silently accepted, which would make the differential harness report success for
// plans it does not understand.
func TestEveryVariantIsHandled(t *testing.T) {
	t.Run("DispatchPlan", func(t *testing.T) {
		for _, d := range planwalk.AllDispatchPlans() {
			runVariant(t, plan.CompilationPlan{Dispatch: d}, d)
		}
	})
	t.Run("PredicateExpr", func(t *testing.T) {
		for _, e := range planwalk.AllPredicateExprs() {
			p := plan.CompilationPlan{Validation: plan.ValidationPlan{
				Predicates: []plan.GuardedPredicate{{Applicability: plan.SetAny, Expression: e}},
			}}
			runVariant(t, p, e)
		}
	})
	t.Run("ResolutionPlan", func(t *testing.T) {
		for _, r := range planwalk.AllResolutionPlans() {
			runVariant(t, plan.CompilationPlan{Resolution: r}, r)
		}
	})
}

func runVariant(t *testing.T, p plan.CompilationPlan, variant any) {
	t.Helper()

	t.Run(fmt.Sprintf("%T", variant), func(t *testing.T) {
		for _, value := range instances {
			_, err := planterp.Interpret(p, value)
			if err != nil {
				require.NotContains(t, err.Error(), "unhandled",
					"variant %T reached an unhandled-variant default", variant)
			}
		}
	})
}

// TestRepresentationsAreIgnored is the structural half of design §4.1 (issue #115): the
// interpreter decides from the checks and the dispatch, so a plan carrying nothing but a
// representation accepts every instance — including [plan.NeverRepresentation], which as
// storage holds nothing and as a check says nothing.
//
// [interp.accept] is handed a `checks`, which has no representation field, so this is a
// property of the types rather than a rule the code follows. The test is here to fail if
// that is ever widened back.
func TestRepresentationsAreIgnored(t *testing.T) {
	for _, r := range planwalk.AllRepresentations() {
		t.Run(fmt.Sprintf("%T", r), func(t *testing.T) {
			for _, value := range instances {
				v, err := planterp.Interpret(plan.CompilationPlan{Representation: r}, value)
				require.NoError(t, err)
				require.True(t, v.Accepted,
					"a representation must not decide acceptance; reason: %v", v.Reason)
			}
		})
	}
}

// TestUnknownValueType keeps the interpreter from guessing at a Go value that is not a
// decoded JSON document.
func TestUnknownValueType(t *testing.T) {
	_, err := planterp.Interpret(plan.CompilationPlan{Representation: &plan.AnyRepresentation{}}, int32(1))
	require.ErrorContains(t, err, "not a decoded JSON value")
}
