package planner_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/internal/planner"
	"github.com/ogen-go/schemacompiler/plan"
)

type guardedFormat struct {
	name  string
	guard plan.KindSet
}

func formatExpr(set plan.KindSet, formats ...guardedFormat) ir.Expr {
	operands := []ir.Expr{ir.Kinds{Set: set}}
	for _, f := range formats {
		operands = append(operands, ir.Predicate{Guard: f.guard, Detail: ir.FormatDetail{Format: f.name}})
	}
	return ir.All{Operands: operands}
}

func TestBuild_FormatOnRepresentation(t *testing.T) {
	for _, tt := range []struct {
		name  string
		guard plan.KindSet
		set   plan.KindSet
		kind  plan.JSONKind
	}{
		{"date-time", plan.SetString, plan.SetString, plan.KindString},
		{"uuid", plan.SetString, plan.SetString, plan.KindString},
		{"email", plan.SetString, plan.SetString, plan.KindString},
		{"phone-number", plan.SetString, plan.SetString, plan.KindString},
		{"int32", plan.SetNumber, plan.SetNumber, plan.KindNumber},
		{"double", plan.SetNumber, plan.SetNumber, plan.KindNumber},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := planner.Build(formatExpr(tt.set, guardedFormat{tt.name, tt.guard}), nil)

			require.Equal(t, plan.PrimitiveRepresentation{
				Kind:   tt.kind,
				Format: tt.name,
			}, got.Plan.Representation)

			// The residual assertion survives onto the representation: a backend may
			// ignore Format and still validate.
			require.Len(t, got.Plan.Validation.Predicates, 1)
			require.Equal(t, plan.FormatPredicate{Format: tt.name}, got.Plan.Validation.Predicates[0].Expression)
			require.Empty(t, got.Diagnostics)
		})
	}
}

func TestBuild_FormatVacuousForKind(t *testing.T) {
	for _, tt := range []struct {
		name   string
		format guardedFormat
		set    plan.KindSet
		kind   plan.JSONKind
	}{
		// {"type": "boolean", "format": "uuid"}: the guard never fires.
		{"boolean", guardedFormat{"uuid", plan.SetString}, plan.SetBoolean, plan.KindBoolean},
		// {"type": "number", "format": "uuid"}: a string format must not type a number.
		{"number", guardedFormat{"uuid", plan.SetString}, plan.SetNumber, plan.KindNumber},
		// {"type": "string", "format": "int32"}: nor a numeric format a string.
		{"string", guardedFormat{"int32", plan.SetNumber}, plan.SetString, plan.KindString},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := planner.Build(formatExpr(tt.set, tt.format), nil)

			require.Equal(t, plan.PrimitiveRepresentation{Kind: tt.kind}, got.Plan.Representation)
			require.True(t, got.Plan.Validation.Empty())
			require.Empty(t, got.Diagnostics)
		})
	}
}

func TestBuild_FormatWithoutTypeStaysValidationOnly(t *testing.T) {
	// {"format": "uuid"}: every non-string value is still accepted, so the
	// representation widens to Any and cannot carry the format.
	got := planner.Build(ir.All{Operands: []ir.Expr{
		ir.Predicate{Guard: plan.SetString, Detail: ir.FormatDetail{Format: "uuid"}},
	}}, nil)

	require.Equal(t, plan.AnyRepresentation{}, got.Plan.Representation)
	require.Len(t, got.Plan.Validation.Predicates, 1)
	require.Equal(t, plan.SetString, got.Plan.Validation.Predicates[0].Applicability)
}

func TestBuild_ConflictingFormatsCarryNone(t *testing.T) {
	forward := planner.Build(formatExpr(plan.SetString,
		guardedFormat{"date-time", plan.SetString}, guardedFormat{"uuid", plan.SetString}), nil)
	reverse := planner.Build(formatExpr(plan.SetString,
		guardedFormat{"uuid", plan.SetString}, guardedFormat{"date-time", plan.SetString}), nil)

	require.Equal(t, forward.Plan.Representation, reverse.Plan.Representation)
	require.Equal(t, plan.PrimitiveRepresentation{Kind: plan.KindString}, forward.Plan.Representation)

	for _, got := range []planner.Result{forward, reverse} {
		require.Len(t, got.Plan.Validation.Predicates, 2)
		require.Len(t, got.Diagnostics, 1)
		require.Equal(t, plan.SeverityInfo, got.Diagnostics[0].Severity)
		require.NotEmpty(t, got.Diagnostics[0].Pointer)
		require.Contains(t, got.Diagnostics[0].Message, "conflicting formats (date-time, uuid)")
	}
}
