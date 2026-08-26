package planner_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/internal/planner"
	"github.com/ogen-go/schemacompiler/plan"
)

// TestBuild_NestedPlanPredicatesCostTheSame pins issue #80. Three predicates carry a whole
// nested [plan.CompilationPlan] that a backend has to run at validation time — once per
// array element, once per property key, once over the whole instance — and none of them is
// something a plain Go type plus field validators can express.
//
// They must therefore price identically: [plan.RawEvaluation] and a
// [plan.DiagnosticCost] naming the work. `propertyNames` used to do neither, so a backend
// consulting only the capability gate saw an ordinary validated object and had nowhere to
// put the per-key check (design §24 forbids under-approximating the cost).
func TestBuild_NestedPlanPredicatesCostTheSame(t *testing.T) {
	for _, tt := range []struct {
		name string
		expr ir.Expr
	}{
		{
			name: "contains",
			expr: &ir.All{Operands: []ir.Expr{
				&ir.Kinds{Set: plan.SetArray},
				&ir.Predicate{Guard: plan.SetArray, Detail: &ir.ContainsDetail{Schema: constrainedString()}},
			}},
		},
		{
			name: "propertyNames",
			expr: &ir.All{Operands: []ir.Expr{
				&ir.Kinds{Set: plan.SetObject},
				&ir.Predicate{Guard: plan.SetObject, Detail: &ir.PropertyNamesDetail{Schema: constrainedString()}},
			}},
		},
		{
			name: "not",
			expr: &ir.All{Operands: []ir.Expr{&ir.Not{Operand: constrainedString()}}},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := planner.Build(tt.expr, nil)

			require.Equal(t, plan.RawEvaluation, got.Plan.Capability,
				"a nested plan run at validation time is RawEvaluation work")
			require.True(t, hasKind(got.Diagnostics, plan.DiagnosticCost),
				"the cost must be named, not left to the capability alone: %+v", got.Diagnostics)
			require.False(t, hasKind(got.Diagnostics, plan.DiagnosticUnenforced),
				"the check is kept, so nothing is unenforced")
		})
	}
}

// TestBuild_PropertyNamesKeepsItsSubPlan keeps the floor from being satisfied by dropping
// the check: the nested plan must still reach the predicate, constraints and all.
func TestBuild_PropertyNamesKeepsItsSubPlan(t *testing.T) {
	got := planner.Build(&ir.All{Operands: []ir.Expr{
		&ir.Kinds{Set: plan.SetObject},
		&ir.Predicate{Guard: plan.SetObject, Detail: &ir.PropertyNamesDetail{Schema: constrainedString()}},
	}}, nil)

	var found *plan.PropertyNamesPredicate
	for _, gp := range got.Plan.Validation.Predicates {
		if p, ok := gp.Expression.(*plan.PropertyNamesPredicate); ok {
			found = p
		}
	}
	require.NotNil(t, found, "predicates: %+v", got.Plan.Validation.Predicates)
	require.Len(t, found.Schema.ResidualChecks(), 1)
	require.Equal(t, &plan.MinLengthPredicate{Value: 1}, found.Schema.ResidualChecks()[0].Expression)

	require.NotEmpty(t, got.Requirements.RawEvaluation,
		"the keys it sees include names the representation has no field for")
}
