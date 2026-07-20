package norm

import (
	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/plan"
)

// normalizeAll simplifies an All node: flatten nested All (design §15,
// associativity), distribute a lone ExactlyOne/AnyOf sibling over the rest
// (design §15.4-15.5, §17.3-17.4), fold Kinds operands, drop guard-vacuous
// predicates, drop duplicates/Any, and collapse subsumed operands (design
// §15.1, §15.2).
func normalizeAll(e ir.All, st *state) ir.Expr {
	// Flatten nested All *without* normalizing children first: a lone
	// ExactlyOne/AnyOf sibling must still be recognizable here, before
	// normalizing it in isolation could collapse it on its own (e.g. via
	// disjointness) and hide the distribution opportunity (design §15.4-
	// 15.5, §17.3 — push common constraints in before simplifying away the
	// combinator that would receive them).
	var raw []ir.Expr
	for _, o := range e.Operands {
		raw = flattenAllInto(raw, o)
	}

	if out, ok := distributeAll(raw, st); ok {
		return normalize(out, st)
	}

	var flat []ir.Expr
	for _, o := range raw {
		flat = flattenAllInto(flat, normalize(o, st))
	}

	flat, kinds, hasKinds, isNever := foldKindsAll(flat)
	if isNever {
		return ir.Never{}
	}

	flat = dropGuardVacuousPredicates(flat, kinds, hasKinds)

	for _, o := range flat {
		if _, ok := o.(ir.Never); ok {
			// All(..., Never, ...) -> Never (design §15.1).
			return ir.Never{}
		}
	}

	flat = dropAny(flat)    // Any is All's identity element.
	flat = dedupExprs(flat) // All(A, A) -> A (design §15.1).
	flat = removeSubsumedAll(flat)

	switch len(flat) {
	case 0:
		return ir.Any{} // All() -> Any (design §15.1).
	case 1:
		return flat[0]
	default:
		return ir.All{Operands: flat}
	}
}

// normalizeAnyOf simplifies an AnyOf node: flatten nested AnyOf, drop Never
// operands, dedup, and collapse subsumed operands (design §15.1, §15.2).
func normalizeAnyOf(e ir.AnyOf, st *state) ir.Expr {
	var flat []ir.Expr
	for _, o := range e.Operands {
		flat = flattenAnyOfInto(flat, normalize(o, st))
	}

	flat = filterNotNever(flat) // AnyOf(..., Never, ...) -> remove Never.
	flat = dedupExprs(flat)     // AnyOf(A, A) -> A.
	flat = removeSubsumedAnyOf(flat)

	switch len(flat) {
	case 0:
		return ir.Never{} // AnyOf() -> Never (design §15.1).
	case 1:
		return flat[0]
	default:
		return ir.AnyOf{Operands: flat}
	}
}

// normalizeExactlyOne simplifies an ExactlyOne node. ExactlyOne is NOT
// associative under flattening (design §17.1: nested oneOf(anyOf(...), ...)
// is not the same as flattening every operand into one exact-one group), so
// nested ExactlyOne operands are normalized but never merged into this one.
func normalizeExactlyOne(e ir.ExactlyOne, st *state) ir.Expr {
	flat := make([]ir.Expr, 0, len(e.Operands))
	for _, o := range e.Operands {
		flat = append(flat, normalize(o, st))
	}

	flat = filterNotNever(flat) // ExactlyOne(..., Never, ...) -> remove Never.

	// Generalized idempotence (design §15.1): ExactlyOne(A, A) -> Never
	// because every value satisfying A satisfies two branches. For any
	// larger group, a value in a duplicated branch always contributes >=2
	// matches, so it can never satisfy exactly-one; exclude it and recurse
	// on what remains: ExactlyOne(A, A, C, ...) = All(Not(A), ExactlyOne(C, ...)).
	if dup, rest, ok := extractDuplicate(flat); ok {
		return normalize(ir.All{Operands: []ir.Expr{
			ir.Not{Operand: dup},
			ir.ExactlyOne{Operands: rest},
		}}, st)
	}

	switch len(flat) {
	case 0:
		return ir.Never{} // ExactlyOne() -> Never (design §15.1).
	case 1:
		return flat[0]
	}

	// Subsumption (design §15.2): A ⊆ B => ExactlyOne(A, B) = All(B, Not(A)).
	// Only proven for the pairwise (n=2) case; larger groups with a
	// subsuming pair are left as-is (conservative, still sound).
	if len(flat) == 2 {
		a, b := flat[0], flat[1]
		switch {
		case subsumes(a, b):
			return normalize(ir.All{Operands: []ir.Expr{b, ir.Not{Operand: a}}}, st)
		case subsumes(b, a):
			return normalize(ir.All{Operands: []ir.Expr{a, ir.Not{Operand: b}}}, st)
		}
	}

	// Disjointness -> union (design §15.3): rewriting unlocks static kind
	// dispatch downstream instead of a residual match-count check.
	if allPairwiseDisjoint(flat) {
		return normalize(ir.AnyOf{Operands: flat}, st)
	}

	return ir.ExactlyOne{Operands: flat}
}

func flattenAllInto(dst []ir.Expr, e ir.Expr) []ir.Expr {
	if a, ok := e.(ir.All); ok {
		for _, o := range a.Operands {
			dst = flattenAllInto(dst, o)
		}
		return dst
	}
	return append(dst, e)
}

func flattenAnyOfInto(dst []ir.Expr, e ir.Expr) []ir.Expr {
	if a, ok := e.(ir.AnyOf); ok {
		for _, o := range a.Operands {
			dst = flattenAnyOfInto(dst, o)
		}
		return dst
	}
	return append(dst, e)
}

// foldKindsAll intersects every Kinds operand in an All into one (design
// §15, kind intersection): the accepted set narrows to the intersection of
// their kind sets, and their NumericDomain refinements combine (AnyNumber
// acts as identity; IntegerOnly ∩ NonIntegerOnly is a contradiction, which
// drops the Number kind from the intersection rather than the whole All).
// isNever reports the resulting set was empty.
func foldKindsAll(operands []ir.Expr) (result []ir.Expr, kinds ir.Kinds, hasKinds, isNever bool) {
	set := plan.SetAny
	numeric := plan.AnyNumber
	numericSet := false
	found := false
	var rest []ir.Expr

	for _, o := range operands {
		k, ok := o.(ir.Kinds)
		if !ok {
			rest = append(rest, o)
			continue
		}
		found = true
		set &= k.Set
		if nd, ok := combineNumeric(numericSet, numeric, k.Numeric); ok {
			numeric = nd
			numericSet = true
		} else {
			// IntegerOnly ∩ NonIntegerOnly = ∅: no number value can satisfy
			// both, so the Number kind itself is excluded from the fold.
			set &^= plan.SetNumber
		}
	}
	if !found {
		return operands, ir.Kinds{}, false, false
	}
	if set == 0 {
		return nil, ir.Kinds{}, false, true
	}

	merged := ir.Kinds{Set: set, Numeric: numeric}
	result = make([]ir.Expr, 0, len(rest)+1)
	result = append(result, merged)
	result = append(result, rest...)
	return result, merged, true, false
}

// combineNumeric intersects two NumericDomain refinements. AnyNumber is the
// identity; equal domains are unchanged; IntegerOnly and NonIntegerOnly
// contradict (ok=false).
func combineNumeric(set bool, cur, next plan.NumericDomain) (plan.NumericDomain, bool) {
	if !set {
		return next, true
	}
	if cur == next {
		return cur, true
	}
	if cur == plan.AnyNumber {
		return next, true
	}
	if next == plan.AnyNumber {
		return cur, true
	}
	return cur, false
}

// dropGuardVacuousPredicates removes a Predicate operand whose Guard is
// disjoint from the enclosing All's folded Kinds: every value the All can
// accept has a kind outside the guard, so the predicate always passes
// vacuously and can be dropped (design §3, guard elimination). The opposite
// case — Kinds ⊆ Guard, meaning the guard is redundant — needs no rewrite:
// the predicate already always applies, it is simply left in place.
func dropGuardVacuousPredicates(operands []ir.Expr, kinds ir.Kinds, hasKinds bool) []ir.Expr {
	if !hasKinds {
		return operands
	}
	out := make([]ir.Expr, 0, len(operands))
	for _, o := range operands {
		if p, ok := o.(ir.Predicate); ok && kinds.Set&p.Guard == 0 {
			continue // vacuous under this All's kind restriction: drop it.
		}
		out = append(out, o)
	}
	return out
}

func dedupExprs(operands []ir.Expr) []ir.Expr {
	out := make([]ir.Expr, 0, len(operands))
	for _, o := range operands {
		dup := false
		for _, kept := range out {
			if exprEqual(o, kept) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, o)
		}
	}
	return out
}

func dropAny(operands []ir.Expr) []ir.Expr {
	out := make([]ir.Expr, 0, len(operands))
	for _, o := range operands {
		if _, ok := o.(ir.Any); ok {
			continue
		}
		out = append(out, o)
	}
	return out
}

func filterNotNever(operands []ir.Expr) []ir.Expr {
	out := make([]ir.Expr, 0, len(operands))
	for _, o := range operands {
		if _, ok := o.(ir.Never); ok {
			continue
		}
		out = append(out, o)
	}
	return out
}

// removeSubsumedAll drops the operand of a subsuming pair that adds nothing:
// if a ⊆ b, All(..., a, ..., b, ...) = All(..., a, ...) since a already
// implies b (design §15.2).
func removeSubsumedAll(operands []ir.Expr) []ir.Expr {
	drop := make([]bool, len(operands))
	for i := 0; i < len(operands); i++ {
		if drop[i] {
			continue
		}
		for j := i + 1; j < len(operands); j++ {
			if drop[j] {
				continue
			}
			iSubJ := subsumes(operands[i], operands[j])
			jSubI := subsumes(operands[j], operands[i])
			switch {
			case iSubJ:
				drop[j] = true
			case jSubI:
				drop[i] = true
			}
			if drop[i] {
				break
			}
		}
	}
	out := make([]ir.Expr, 0, len(operands))
	for i, o := range operands {
		if !drop[i] {
			out = append(out, o)
		}
	}
	return out
}

// removeSubsumedAnyOf drops the operand of a subsuming pair that adds
// nothing: if a ⊆ b, AnyOf(..., a, ..., b, ...) = AnyOf(..., b, ...) since b
// already covers a (design §15.2).
func removeSubsumedAnyOf(operands []ir.Expr) []ir.Expr {
	drop := make([]bool, len(operands))
	for i := 0; i < len(operands); i++ {
		if drop[i] {
			continue
		}
		for j := i + 1; j < len(operands); j++ {
			if drop[j] {
				continue
			}
			iSubJ := subsumes(operands[i], operands[j])
			jSubI := subsumes(operands[j], operands[i])
			switch {
			case iSubJ:
				drop[i] = true
			case jSubI:
				drop[j] = true
			}
			if drop[i] {
				break
			}
		}
	}
	out := make([]ir.Expr, 0, len(operands))
	for i, o := range operands {
		if !drop[i] {
			out = append(out, o)
		}
	}
	return out
}

// extractDuplicate finds a value that occurs at least twice (by exprEqual)
// and returns it plus every operand not equal to it.
func extractDuplicate(operands []ir.Expr) (dup ir.Expr, rest []ir.Expr, ok bool) {
	for i := range operands {
		count := 0
		for j := range operands {
			if exprEqual(operands[i], operands[j]) {
				count++
			}
		}
		if count >= 2 {
			rest := make([]ir.Expr, 0, len(operands))
			for _, o := range operands {
				if !exprEqual(o, operands[i]) {
					rest = append(rest, o)
				}
			}
			return operands[i], rest, true
		}
	}
	return nil, nil, false
}
