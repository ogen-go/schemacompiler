package norm

import (
	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/plan"
)

// subsumes reports whether every value satisfying a also satisfies b (a ⊆ b),
// proven only for the tractable v1 cases (design §15.2):
//
//   - syntactic equality;
//   - a == Never, or b == Any;
//   - Kinds subset, including NumericDomain compatibility;
//   - same-guard predicate bound tightening (minLength/maxLength,
//     minimum/maximum/exclusive variants, minItems/maxItems,
//     minProperties/maxProperties);
//   - the generic lattice lemmas All(x, ...) ⊆ y (some conjunct already
//     proves it), x ⊆ All(y1, ...) (x proves it against every conjunct),
//     and x ⊆ AnyOf(..., y, ...) (x fits inside one disjunct).
//
// Regex-language inclusion, full numeric interval algebra, and property-name
// language analysis are NOT attempted (design §23 lists these as optional/
// advanced); subsumes returns false rather than guessing, which is always
// safe here — it just means a rule that depends on the proof is skipped.
func subsumes(a, b ir.Expr) bool {
	if exprEqual(a, b) {
		return true
	}
	if _, ok := a.(ir.Never); ok {
		return true
	}
	if _, ok := b.(ir.Any); ok {
		return true
	}

	if ak, ok := asKinds(a); ok {
		if bk, ok := asKinds(b); ok {
			return kindsSubsume(ak, bk)
		}
	}

	if pa, ok := a.(ir.Predicate); ok {
		if pb, ok := b.(ir.Predicate); ok && pa.Guard == pb.Guard && predicateDetailSubsumes(pa.Detail, pb.Detail) {
			return true
		}
	}

	if allA, ok := a.(ir.All); ok {
		for _, o := range allA.Operands {
			if subsumes(o, b) {
				return true
			}
		}
	}
	if allB, ok := b.(ir.All); ok {
		all := true
		for _, y := range allB.Operands {
			if !subsumes(a, y) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	if anyB, ok := b.(ir.AnyOf); ok {
		for _, o := range anyB.Operands {
			if subsumes(a, o) {
				return true
			}
		}
	}
	return false
}

// asKinds reports the effective Kinds of e when e is exactly a kind
// restriction and nothing finer: Kinds itself, Any, Never, or a
// single-operand All wrapping one of those.
func asKinds(e ir.Expr) (ir.Kinds, bool) {
	switch e := e.(type) {
	case ir.Kinds:
		return e, true
	case ir.Any:
		return ir.Kinds{Set: plan.SetAny}, true
	case ir.Never:
		return ir.Kinds{Set: 0}, true
	case ir.All:
		if len(e.Operands) == 1 {
			return asKinds(e.Operands[0])
		}
	}
	return ir.Kinds{}, false
}

func kindsSubsume(a, b ir.Kinds) bool {
	if a.Set&^b.Set != 0 {
		return false // a accepts a kind b does not: not a subset.
	}
	if a.Set&plan.SetNumber == 0 {
		return true // no number values in play: numeric domain is irrelevant.
	}
	return numericSubsumes(a.Numeric, b.Numeric)
}

func numericSubsumes(a, b plan.NumericDomain) bool {
	return a == b || b == plan.AnyNumber
}

// predicateDetailSubsumes proves same-guard bound tightening for a small,
// tractable list of scalar-bound predicate details: a's bound is at least as
// strict as b's, so every value passing a also passes b.
func predicateDetailSubsumes(a, b ir.PredicateDetail) bool {
	switch a := a.(type) {
	case ir.MinLengthDetail:
		if b, ok := b.(ir.MinLengthDetail); ok {
			return a.Value >= b.Value
		}
	case ir.MaxLengthDetail:
		if b, ok := b.(ir.MaxLengthDetail); ok {
			return a.Value <= b.Value
		}
	case ir.MinimumDetail:
		if b, ok := b.(ir.MinimumDetail); ok {
			return a.Value >= b.Value
		}
	case ir.MaximumDetail:
		if b, ok := b.(ir.MaximumDetail); ok {
			return a.Value <= b.Value
		}
	case ir.ExclusiveMinimumDetail:
		if b, ok := b.(ir.ExclusiveMinimumDetail); ok {
			return a.Value >= b.Value
		}
	case ir.ExclusiveMaximumDetail:
		if b, ok := b.(ir.ExclusiveMaximumDetail); ok {
			return a.Value <= b.Value
		}
	case ir.MinItemsDetail:
		if b, ok := b.(ir.MinItemsDetail); ok {
			return a.Value >= b.Value
		}
	case ir.MaxItemsDetail:
		if b, ok := b.(ir.MaxItemsDetail); ok {
			return a.Value <= b.Value
		}
	case ir.MinPropertiesDetail:
		if b, ok := b.(ir.MinPropertiesDetail); ok {
			return a.Value >= b.Value
		}
	case ir.MaxPropertiesDetail:
		if b, ok := b.(ir.MaxPropertiesDetail); ok {
			return a.Value <= b.Value
		}
	}
	return false
}
