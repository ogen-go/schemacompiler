package dump_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/dump"
	"github.com/ogen-go/schemacompiler/internal/planwalk"
	"github.com/ogen-go/schemacompiler/plan"
)

// TestDumpHandlesEveryVariant ties dump's rendering switches to the canonical variant
// lists: a variant dump has no case for falls into a "<unknown ...>" arm, which this
// turns into a failure instead of silently wrong output.
func TestDumpHandlesEveryVariant(t *testing.T) {
	render := func(t *testing.T, p plan.CompilationPlan) {
		t.Helper()

		var buf bytes.Buffer
		dump.Plan(&buf, p)
		require.NotContains(t, buf.String(), "<unknown")

		buf.Reset()
		dump.PlanDOT(&buf, p, nil)
		require.NotContains(t, buf.String(), "<unknown")
	}

	for _, r := range planwalk.AllRepresentations() {
		t.Run(name(r), func(t *testing.T) { render(t, plan.CompilationPlan{Representation: r}) })
	}
	for _, d := range planwalk.AllDispatchPlans() {
		t.Run(name(d), func(t *testing.T) { render(t, plan.CompilationPlan{Dispatch: d}) })
	}
	for _, e := range planwalk.AllPredicateExprs() {
		t.Run(name(e), func(t *testing.T) {
			render(t, plan.CompilationPlan{Validation: plan.ValidationPlan{
				Predicates: []plan.GuardedPredicate{{Expression: e}},
			}})
		})
	}
	for _, r := range planwalk.AllResolutionPlans() {
		t.Run(name(r), func(t *testing.T) { render(t, plan.CompilationPlan{Resolution: r}) })
	}
}

func name(v any) string {
	return strings.TrimPrefix(fmt.Sprintf("%T", v), "plan.")
}
