package planner_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/internal/planner"
	"github.com/ogen-go/schemacompiler/plan"
)

// TestBuild_ObjectRestatesItsShape pins that an object plan states its shape twice — once
// as storage and once as a check (design §4.1, issue #115) — and that the restatement is
// discharged by the representation, so the plan stays DirectGoType.
func TestBuild_ObjectRestatesItsShape(t *testing.T) {
	got := planner.Build(&ir.All{Operands: []ir.Expr{
		&ir.Kinds{Set: plan.SetObject},
		&ir.Shape{Detail: &ir.ObjectShape{
			Properties: []ir.PropertyExpr{{Name: "a", Schema: &ir.Kinds{Set: plan.SetString}}},
		}},
	}}, nil).Plan

	rep, ok := got.Representation.(*plan.ObjectRepresentation)
	require.True(t, ok, "got %T", got.Representation)
	require.Len(t, rep.Fields, 1)

	var check *plan.ObjectStructurePredicate
	for _, gp := range got.Validation.Predicates {
		if e, ok := gp.Expression.(*plan.ObjectStructurePredicate); ok {
			check = e
			require.Equal(t, plan.SetObject, gp.Applicability)
		}
	}
	require.Len(t, check.Properties, 1, "the object plan must carry its shape as a check")
	require.Equal(t, rep.Fields[0].Name, check.Properties[0].Name)
	require.Equal(t, rep.Fields[0].Presence, check.Properties[0].Presence)
	require.Same(t, rep.Additional, check.Additional, "the sub-plans are shared, not copied")

	require.Empty(t, got.ResidualChecks(), "the representation discharges its own restatement")
	require.Equal(t, plan.DirectGoType, got.Capability)
}

// TestBuild_ArrayRestatesItsShape is [TestBuild_ObjectRestatesItsShape] for a tuple.
func TestBuild_ArrayRestatesItsShape(t *testing.T) {
	got := planner.Build(&ir.All{Operands: []ir.Expr{
		&ir.Kinds{Set: plan.SetArray},
		&ir.Shape{Detail: &ir.ArrayShape{PrefixItems: []ir.ItemExpr{{Schema: &ir.Kinds{Set: plan.SetString}}}}},
	}}, nil).Plan

	rep, ok := got.Representation.(*plan.ArrayRepresentation)
	require.True(t, ok, "got %T", got.Representation)

	var check *plan.ArrayStructurePredicate
	var found bool
	for _, gp := range got.Validation.Predicates {
		if e, ok := gp.Expression.(*plan.ArrayStructurePredicate); ok {
			check, found = e, true
			require.Equal(t, plan.SetArray, gp.Applicability)
		}
	}
	require.True(t, found, "the array plan must carry its shape as a check")
	require.Len(t, check.Prefix, len(rep.Prefix))
	require.NotNil(t, check.Rest, "items absent admits any trailing element")

	require.Empty(t, got.ResidualChecks())
	require.Equal(t, plan.DirectGoType, got.Capability)
}

// TestBuild_NeverRejectsFromValidation pins that an unsatisfiable schema says so in the
// validation plan rather than only in a Go type nothing can be stored in: an assertion
// over the empty kind set, which no instance's kind is in.
func TestBuild_NeverRejectsFromValidation(t *testing.T) {
	got := planner.Build(&ir.Never{}, nil).Plan

	require.Equal(t, &plan.NeverRepresentation{}, got.Representation)
	require.Equal(t, []plan.GuardedPredicate{{Applicability: 0, Assert: true}}, got.Validation.Predicates)
	require.Empty(t, got.ResidualChecks())
}

// TestBuild_RefRestatesItsTarget pins that a `$ref` plan names its target in the
// validation plan as well as in the representation (design §4.1, issue #115), and that
// the restatement is discharged so the reference stays DirectGoType.
func TestBuild_RefRestatesItsTarget(t *testing.T) {
	got := planner.Build(&ir.Ref{Target: "#/$defs/A"}, nil).Plan

	require.Equal(t, &plan.ReferenceRepresentation{Name: "#/$defs/A"}, got.Representation)
	require.Equal(t, []plan.GuardedPredicate{{
		Applicability: plan.SetAny,
		Expression:    &plan.ReferencePredicate{Name: "#/$defs/A"},
	}}, got.Validation.Predicates)
	require.Empty(t, got.ResidualChecks())
	require.Equal(t, plan.DirectGoType, got.Capability)
}
