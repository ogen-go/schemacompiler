package planner_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/internal/planner"
	"github.com/ogen-go/schemacompiler/plan"
)

// TestEveryPredicateDetailIsLowered pins issue #64 from the side that can actually be
// pinned. The recovery mapPredicate takes for a detail it does not recognize is sound now
// — the constraint is not enforced, so the plan is Unsupported and says so — but the
// recovery is not the goal. The goal is never reaching it.
//
// A detail added to ir and to [ir.AllPredicateDetails] but not to mapPredicate fails here,
// which is the whole point: the previous default kept such a plan at DirectGoType with a
// warning, so a backend generated a type that accepts values the schema rejects.
func TestEveryPredicateDetailIsLowered(t *testing.T) {
	for _, d := range ir.AllPredicateDetails() {
		t.Run(fmt.Sprintf("%T", d), func(t *testing.T) {
			got := planner.Build(ir.All{Operands: []ir.Expr{
				ir.Predicate{Guard: plan.SetAny, Detail: d},
			}}, nil)

			for _, diag := range got.Diagnostics {
				require.NotContains(t, diag.Message, "unrecognized predicate detail",
					"%T reached mapPredicate's unhandled-detail default", d)
			}
			require.NotEqual(t, plan.Unsupported, got.Plan.Capability,
				"%T must lower to something", d)
		})
	}
}

// TestDroppedKeywordStaysGeneratable keeps the fix above from swallowing the one detail
// that is *meant* to carry a missing constraint: an invalid keyword value leaves the plan
// generatable and declares the gap (issues #74, #84), which is a different case from a
// detail the compiler does not understand.
func TestDroppedKeywordStaysGeneratable(t *testing.T) {
	got := planner.Build(ir.All{Operands: []ir.Expr{
		ir.Kinds{Set: plan.SetString},
		ir.Predicate{Guard: plan.SetString, Detail: ir.DroppedKeywordDetail{Keyword: "maxLength"}},
	}}, nil)

	require.Equal(t, plan.DirectGoType, got.Plan.Capability)
	require.False(t, hasKind(got.Diagnostics, plan.DiagnosticUnsupported))
}
