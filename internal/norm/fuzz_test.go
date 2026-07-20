package norm

import (
	"testing"

	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/plan"
)

// buildExpr deterministically builds an ir.Expr from fuzzer-supplied bytes: a
// small opcode-driven recursive descent over a fixed leaf palette, bounded in
// depth and branching so the fuzzer explores structure rather than blowing
// the stack.
func buildExpr(data []byte, depth int) (ir.Expr, []byte) {
	if len(data) == 0 {
		return ir.Any{}, data
	}
	op := data[0]
	data = data[1:]

	leaf := func(n byte) ir.Expr {
		switch n % 6 {
		case 0:
			return ir.Any{}
		case 1:
			return ir.Never{}
		case 2:
			return ir.Kinds{Set: plan.SetString}
		case 3:
			return ir.Kinds{Set: plan.SetNumber}
		case 4:
			return ir.Literal{Value: "a"}
		default:
			return ir.Predicate{Guard: plan.SetString, Detail: ir.MinLengthDetail{Value: uint64(n)}}
		}
	}

	if depth <= 0 || len(data) == 0 {
		return leaf(op), data
	}

	var a, b ir.Expr
	a, data = buildExpr(data, depth-1)
	b, data = buildExpr(data, depth-1)

	switch op % 5 {
	case 0:
		return ir.All{Operands: []ir.Expr{a, b}}, data
	case 1:
		return ir.AnyOf{Operands: []ir.Expr{a, b}}, data
	case 2:
		return ir.ExactlyOne{Operands: []ir.Expr{a, b}}, data
	case 3:
		return ir.Not{Operand: a}, data
	default:
		return leaf(op), data
	}
}

func FuzzNormalize(f *testing.F) {
	for _, e := range exprSamples() {
		f.Add(exprSeed(e))
	}
	f.Add([]byte{0, 1, 2, 3, 4})

	f.Fuzz(func(t *testing.T, data []byte) {
		e, _ := buildExpr(data, 5)

		// A generous budget relative to the bounded input size (depth<=5):
		// budget only bounds combinator-distribution steps, and a fixed
		// small budget can legitimately make Normalize non-idempotent (a
		// second call gets a fresh budget and may finish work the first
		// call left undone) — that is expected term-rewriting behavior, not
		// a bug, so idempotence is only checked once headroom is ample.
		const budget = 4096
		got := Normalize(e, budget)

		if got.Kinds()&^e.Kinds() != 0 {
			t.Fatalf("normalized Kinds() %v is not a subset of original %v (expr=%#v)", got.Kinds(), e.Kinds(), e)
		}

		again := Normalize(got, budget)
		if !exprEqual(got, again) {
			t.Fatalf("Normalize is not idempotent:\n first=%#v\nsecond=%#v", got, again)
		}
	})
}

// exprSeed produces a byte seed that isn't necessarily round-trippable back
// to e; it only needs to give the fuzzer varied starting corpus entries.
func exprSeed(e ir.Expr) []byte {
	k := e.Kinds()
	return []byte{byte(k), 1, 2, 3}
}
