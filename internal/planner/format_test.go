package planner_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/internal/planner"
	"github.com/ogen-go/schemacompiler/plan"
)

func formatExpr(set plan.KindSet, formats ...string) ir.Expr {
	operands := []ir.Expr{ir.Kinds{Set: set}}
	for _, f := range formats {
		operands = append(operands, ir.Predicate{
			Guard:  plan.SetString | plan.SetNumber,
			Detail: ir.FormatDetail{Format: f},
		})
	}
	return ir.All{Operands: operands}
}

func TestBuild_FormatOnRepresentation(t *testing.T) {
	for _, tt := range []struct {
		name  string
		set   plan.KindSet
		kind  plan.JSONKind
		class plan.FormatClass
	}{
		{"date-time", plan.SetString, plan.KindString, plan.FormatRepresentational},
		{"uuid", plan.SetString, plan.KindString, plan.FormatRepresentational},
		{"email", plan.SetString, plan.KindString, plan.FormatValidationOnly},
		{"phone-number", plan.SetString, plan.KindString, plan.FormatUnrecognized},
		{"int32", plan.SetNumber, plan.KindNumber, plan.FormatRepresentational},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := planner.Build(formatExpr(tt.set, tt.name), nil)

			require.Equal(t, plan.PrimitiveRepresentation{
				Kind:   tt.kind,
				Format: plan.Format{Name: tt.name, Class: tt.class},
			}, got.Plan.Representation)

			// The residual assertion survives a representational format: a backend may
			// ignore Format and still validate.
			require.Len(t, got.Plan.Validation.Predicates, 1)
			require.Equal(t, plan.FormatPredicate{Format: tt.name}, got.Plan.Validation.Predicates[0].Expression)
			require.Empty(t, got.Diagnostics)
		})
	}
}

func TestBuild_FormatVacuousForKind(t *testing.T) {
	// {"type": "boolean", "format": "uuid"}: the guard never fires.
	got := planner.Build(formatExpr(plan.SetBoolean, "uuid"), nil)

	require.Equal(t, plan.PrimitiveRepresentation{Kind: plan.KindBoolean}, got.Plan.Representation)
	require.True(t, got.Plan.Validation.Empty())
}

func TestBuild_FormatWithoutTypeStaysValidationOnly(t *testing.T) {
	// {"format": "uuid"}: every non-string value is still accepted, so the
	// representation widens to Any and cannot carry the format.
	got := planner.Build(ir.All{Operands: []ir.Expr{
		ir.Predicate{Guard: plan.SetString | plan.SetNumber, Detail: ir.FormatDetail{Format: "uuid"}},
	}}, nil)

	require.Equal(t, plan.AnyRepresentation{}, got.Plan.Representation)
	require.Len(t, got.Plan.Validation.Predicates, 1)
}

func TestBuild_MultipleFormatsPickFirst(t *testing.T) {
	got := planner.Build(formatExpr(plan.SetString, "date-time", "uuid"), nil)

	require.Equal(t, plan.PrimitiveRepresentation{
		Kind:   plan.KindString,
		Format: plan.Format{Name: "date-time", Class: plan.FormatRepresentational},
	}, got.Plan.Representation)
	require.Len(t, got.Plan.Validation.Predicates, 2)
	require.Len(t, got.Diagnostics, 1)
	require.Equal(t, plan.SeverityInfo, got.Diagnostics[0].Severity)
	require.Contains(t, got.Diagnostics[0].Message, "multiple formats")
}
