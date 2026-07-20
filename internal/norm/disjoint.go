package norm

import (
	"reflect"

	"github.com/ogen-go/schemacompiler/internal/ir"
)

// disjoint reports whether no value can satisfy both a and b, proven only
// via JSON-kind disjointness and literal-value distinctness (design §15.3).
// This is a sufficient, not necessary, proof — return false (assume they may
// overlap) when unsure; that is always safe, it just means a rule that
// depends on disjointness is skipped.
func disjoint(a, b ir.Expr) bool {
	if a.Kinds()&b.Kinds() == 0 {
		return true
	}
	if la, ok := a.(ir.Literal); ok {
		if lb, ok := b.(ir.Literal); ok {
			return !literalEqual(la.Value, lb.Value)
		}
	}
	// enum desugars to AnyOf(Literal...); a value satisfies the AnyOf if it
	// satisfies some operand, so it is disjoint from b if every operand is.
	if aa, ok := a.(ir.AnyOf); ok {
		for _, o := range aa.Operands {
			if !disjoint(o, b) {
				return false
			}
		}
		return true
	}
	if bb, ok := b.(ir.AnyOf); ok {
		for _, o := range bb.Operands {
			if !disjoint(a, o) {
				return false
			}
		}
		return true
	}
	return false
}

// allPairwiseDisjoint reports whether every pair of exprs is disjoint
// (design §15.3): the sufficient condition for rewriting an ExactlyOne into
// a plain AnyOf.
func allPairwiseDisjoint(exprs []ir.Expr) bool {
	if len(exprs) < 2 {
		return false
	}
	for i := 0; i < len(exprs); i++ {
		for j := i + 1; j < len(exprs); j++ {
			if !disjoint(exprs[i], exprs[j]) {
				return false
			}
		}
	}
	return true
}

// literalEqual compares two decoded JSON literal values, treating any
// numeric representation (float64/int/int64) as comparable by value so that
// e.g. const 1 and const 1.0 are recognized as the same JSON number.
func literalEqual(a, b any) bool {
	if af, ok := asFloat(a); ok {
		if bf, ok := asFloat(b); ok {
			return af == bf
		}
	}
	return reflect.DeepEqual(a, b)
}

func asFloat(v any) (float64, bool) {
	switch v := v.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	}
	return 0, false
}
