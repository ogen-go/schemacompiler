package planner_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/internal/planner"
	"github.com/ogen-go/schemacompiler/plan"
)

func stringProperty(name string) ir.PropertyExpr {
	return ir.PropertyExpr{Name: name, Schema: &ir.All{Operands: []ir.Expr{&ir.Kinds{Set: plan.SetString}}}}
}

func objectOf(properties ...ir.PropertyExpr) ir.Expr {
	return &ir.All{Operands: []ir.Expr{
		&ir.Kinds{Set: plan.SetObject},
		&ir.Shape{Detail: &ir.ObjectShape{Properties: properties}},
	}}
}

func fieldNames(t *testing.T, p plan.CompilationPlan) []string {
	t.Helper()
	obj, ok := p.Representation.(*plan.ObjectRepresentation)
	require.True(t, ok, "got %T", p.Representation)
	names := make([]string, 0, len(obj.Fields))
	for _, f := range obj.Fields {
		names = append(names, f.Name)
	}
	return names
}

// TestBuild_FieldsKeepSourceOrder pins issue #89: plan.ObjectRepresentation.Fields is
// the source order of `properties`, not an alphabetical or map-iteration order.
func TestBuild_FieldsKeepSourceOrder(t *testing.T) {
	tests := []struct {
		name string
		expr ir.Expr
		want []string
	}{
		{
			name: "declaration order, not alphabetical",
			expr: objectOf(stringProperty("zeta"), stringProperty("alpha"), stringProperty("mid")),
			want: []string{"zeta", "alpha", "mid"},
		},
		{
			name: "allOf branches keep first-declaration order",
			expr: &ir.All{Operands: []ir.Expr{
				&ir.Kinds{Set: plan.SetObject},
				&ir.Shape{Detail: &ir.ObjectShape{Properties: []ir.PropertyExpr{
					stringProperty("zeta"), stringProperty("alpha"),
				}}},
				&ir.Shape{Detail: &ir.ObjectShape{Properties: []ir.PropertyExpr{
					stringProperty("alpha"), stringProperty("mid"),
				}}},
			}},
			want: []string{"zeta", "alpha", "mid"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, fieldNames(t, planner.Build(tt.expr, nil).Plan))
		})
	}
}

// TestBuild_FieldOrderIsStable runs one schema repeatedly: a keyed Fields gave a
// different order per run, so a backend generated different code from the same input.
func TestBuild_FieldOrderIsStable(t *testing.T) {
	expr := objectOf(stringProperty("zeta"), stringProperty("alpha"), stringProperty("mid"))
	want := fieldNames(t, planner.Build(expr, nil).Plan)
	for range 32 {
		require.Equal(t, want, fieldNames(t, planner.Build(expr, nil).Plan))
	}
}
