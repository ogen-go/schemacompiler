package norm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/plan"
)

// TestNormalizeAll_EmptyKindIntersection covers the rule that an All whose operands share
// no kind is uninhabited (design §15.5), including the Literal case foldKindsAll cannot
// see because its kind is implied by the value rather than declared by a Kinds sibling.
func TestNormalizeAll_EmptyKindIntersection(t *testing.T) {
	str := ir.Kinds{Set: plan.SetString}
	num := ir.Kinds{Set: plan.SetNumber}

	tests := []struct {
		name  string
		expr  ir.Expr
		never bool
	}{
		{name: "declared kinds disjoint", expr: ir.All{Operands: []ir.Expr{str, num}}, never: true},
		{name: "literal excluded by kinds", expr: ir.All{Operands: []ir.Expr{str, ir.Literal{Value: 1.0}}}, never: true},
		{name: "literal admitted by kinds", expr: ir.All{Operands: []ir.Expr{str, ir.Literal{Value: "a"}}}},
		{
			name:  "literals of different kinds",
			expr:  ir.All{Operands: []ir.Expr{ir.Literal{Value: "a"}, ir.Literal{Value: 1.0}}},
			never: true,
		},
		{
			name: "shape restricts no kind",
			expr: ir.All{Operands: []ir.Expr{str, ir.Shape{Detail: ir.ObjectShape{}}}},
		},
		{
			name: "one dead branch drops out of the union",
			expr: ir.All{Operands: []ir.Expr{str, ir.AnyOf{Operands: []ir.Expr{
				ir.Literal{Value: "a"},
				ir.Literal{Value: 1.0},
			}}}},
		},
		{
			name: "every branch dead makes the whole expression never",
			expr: ir.All{Operands: []ir.Expr{str, ir.AnyOf{Operands: []ir.Expr{
				ir.Literal{Value: 1.0},
				ir.Literal{Value: 2.0},
			}}}},
			never: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Normalize(tt.expr, 100)
			if tt.never {
				require.IsType(t, ir.Never{}, got)
				return
			}
			require.NotEqual(t, ir.Expr(ir.Never{}), got)
		})
	}
}

// TestNormalizeAll_DeadBranchDropped pins design §16.2's worked example: pushing the
// outer kind restriction into each branch removes the branch it excludes, leaving the
// single survivor rather than a Never alongside it.
func TestNormalizeAll_DeadBranchDropped(t *testing.T) {
	got := Normalize(ir.All{Operands: []ir.Expr{
		ir.Kinds{Set: plan.SetString},
		ir.AnyOf{Operands: []ir.Expr{ir.Literal{Value: "a"}, ir.Literal{Value: 1.0}}},
	}}, 100)
	want := ir.Expr(ir.All{Operands: []ir.Expr{
		ir.Kinds{Set: plan.SetString},
		ir.Literal{Value: "a"},
	}})
	require.Truef(t, exprEqual(want, got), "got %#v", got)
}
