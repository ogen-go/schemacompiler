package norm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/plan"
)

// exprSamples is a small corpus of Expr trees exercising every rule family;
// reused by the fixpoint test and as fuzz seeds.
func exprSamples() []ir.Expr {
	str := ir.Kinds{Set: plan.SetString}
	num := ir.Kinds{Set: plan.SetNumber}
	minLen5 := ir.Predicate{Guard: plan.SetString, Detail: ir.MinLengthDetail{Value: 5}}
	minimum0 := ir.Predicate{Guard: plan.SetNumber, Detail: ir.MinimumDetail{Value: 0}}

	return []ir.Expr{
		ir.Any{},
		ir.Never{},
		str,
		ir.Literal{Value: "a"},
		ir.AnyOf{Operands: []ir.Expr{ir.Literal{Value: "a"}, ir.Literal{Value: "b"}}},
		ir.All{Operands: []ir.Expr{str, minLen5}},
		ir.ExactlyOne{Operands: []ir.Expr{str, num}},
		ir.ExactlyOne{Operands: []ir.Expr{str, ir.All{Operands: []ir.Expr{str, minLen5}}}},
		ir.All{Operands: []ir.Expr{
			ir.Kinds{Set: plan.SetString | plan.SetNumber},
			ir.ExactlyOne{Operands: []ir.Expr{str, ir.Kinds{Set: plan.SetBoolean}}},
		}},
		ir.All{Operands: []ir.Expr{
			ir.ExactlyOne{Operands: []ir.Expr{str, num}},
			ir.AnyOf{Operands: []ir.Expr{minLen5, minimum0}},
		}},
		ir.Not{Operand: ir.Not{Operand: str}},
		ir.Shape{Detail: ir.ObjectShape{
			Properties: []ir.PropertyExpr{{Name: "x", Schema: ir.All{Operands: []ir.Expr{str, str}}}},
		}},
	}
}

func TestNormalize_Fixpoint(t *testing.T) {
	for i, e := range exprSamples() {
		once := Normalize(e, 100)
		twice := Normalize(once, 100)
		require.Truef(t, exprEqual(once, twice), "sample %d: Normalize is not idempotent:\n once=%#v\ntwice=%#v", i, once, twice)
	}
}

func TestNormalize_PreservesKinds(t *testing.T) {
	// Normalization must never report a kind as possible that the original
	// expression's Kinds() ruled out: Kinds() is always a sound
	// over-approximation, and every rule here only simplifies, so the
	// normalized bound can only stay the same or get tighter, never widen
	// beyond the original (e.g. collapsing Not(Not(string)) makes the bound
	// exact where it was previously a conservative SetAny — that is a
	// tightening, so still a subset).
	for i, e := range exprSamples() {
		got := Normalize(e, 100)
		require.Zerof(t, got.Kinds()&^e.Kinds(), "sample %d: normalized Kinds() %v is not a subset of original %v", i, got.Kinds(), e.Kinds())
	}
}
