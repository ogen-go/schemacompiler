package planner

import (
	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/plan"
)

// buildAll is the entry point for an ir.All (or a bare node wrapped as a one-element
// All): it flattens the sibling contributions and routes to the right sub-builder
// (design §21.1, §22).
func (b *builder) buildAll(all *ir.All, path string) plan.CompilationPlan {
	k := ir.Expr(all).Kinds()
	if k == 0 {
		return b.neverPlanAt(path)
	}
	c := flattenAll(all.Operands)
	if c.never {
		return b.neverPlanAt(path)
	}

	// Negation is lifted out of the conjunction and re-attached to the finished plan
	// (design §11.8): no representation can express it, so it must survive as a residual
	// predicate rather than be dropped into an accept-more type (design §24).
	nots := c.nots
	c.nots = nil
	return b.withResidualNegation(b.buildConjunction(k, c, path), nots, path)
}

// withResidualNegation attaches the `Not` operands lifted out of a conjunction to the
// plan built from the rest of it, but only where doing so is sound.
//
// Negation inverts the approximation polarity of its operand (issue #82): `not S` accepts
// exactly what S rejects, so a nested plan that over-approximates S — this compiler's
// standard safe fallback everywhere else — turns into an under-approximation of `not S`,
// rejecting valid instances. That is the one direction §24 never permits, so the nested
// plan's own fidelity decides ([exactlyModeled]):
//
//   - exact (with or without a residual validator): emit the [plan.NegationPredicate].
//     Evaluating it means running a whole sub-schema at runtime and inverting it, which is
//     the [plan.PredicateDispatch] tier for the same reason [plan.ContainsCountPredicate]
//     is (internal/planner/validation.go, docs/implementation.md v1 scope).
//   - anything else: drop the negation. Removing a conjunct only ever accepts a superset,
//     which is a legitimate over-approximation, so the plan stays representable — but
//     nothing left in it rejects the extra values, so the drop is reported as
//     [plan.DiagnosticUnenforced] rather than [plan.DiagnosticAssumed] (issue #84).
func (b *builder) withResidualNegation(p plan.CompilationPlan, nots []ir.Not, path string) plan.CompilationPlan {
	if len(nots) == 0 {
		return p
	}
	emitted, dropped := false, false
	for _, n := range nots {
		before := b.gaps
		sub := b.build(n.Operand, path+"/negated")
		if !exactlyModeled(sub, b.since(before)) || !b.negatable(sub, n.Operand) {
			dropped = true
			continue
		}
		p.Validation.Predicates = append(p.Validation.Predicates, plan.GuardedPredicate{
			Applicability: plan.SetAny,
			Expression:    &plan.NegationPredicate{Schema: sub},
		})
		p.Resolution = mergeResolution(p.Resolution, sub.Resolution)
		emitted = true
	}
	if dropped {
		// Widening the outer plan is sound, but it no longer reproduces the accepted set,
		// and nothing downstream can tell from the plan alone that a conjunct is missing.
		b.dropped = true
		b.diag(path, plan.DiagnosticUnenforced, plan.SeverityWarning,
			"negated sub-schema is not exactly modeled; the negation is dropped and the plan accepts more")
	}
	if emitted {
		b.diag(path, plan.DiagnosticCost, plan.SeverityWarning,
			"residual negation requires runtime sub-schema validation")
		p.Capability = maxCapability(p.Capability, plan.PredicateDispatch)
	}
	return p
}

// negatable reports whether p can be trusted to accept exactly what its schema accepts,
// which is stricter than [exactlyModeled] and is what negating it requires.
//
// The gap [exactlyModeled] leaves is references: [buildRef] plans one from its identity
// alone, so p reports the fidelity of everything *except* its reference leaves. Each of
// those is resolved and judged on its target here (see [refsTrusted]).
//
// Object and array representations used to be excluded outright and no longer are: they
// carry the whole sub-plan of a field or an item (#68), keep the shape when there is no
// sibling `type` (#72), and scope `additionalProperties`/`items` to the names and indexes
// their own schema object declared (#94), so their fidelity is now what it says.
func (b *builder) negatable(p plan.CompilationPlan, operand ir.Expr) bool {
	if vacuous(p, operand) {
		return false
	}
	return b.refsTrusted(p, nil)
}

// vacuous reports whether p accepts every instance while operand does not, which proves
// the sub-builder dropped operand constraints however exact p claims to be — an
// unrecognized keyword (#64), say. Negating such a plan would reject every instance.
//
// An operand that really is unconstrained is excluded: `not {}` is legitimately Never, and
// that is the one case where a plan accepting everything is the honest answer.
func vacuous(p plan.CompilationPlan, operand ir.Expr) bool {
	if _, isAny := p.Representation.(*plan.AnyRepresentation); !isAny {
		return false
	}
	if !p.Validation.Empty() {
		return false
	}
	if _, noDispatch := p.Dispatch.(*plan.NoDispatch); !noDispatch {
		return false
	}
	_, operandIsAny := operand.(*ir.Any)
	return !operandIsAny
}

// buildConjunction routes the negation-free remainder of an [ir.All] to the right
// sub-builder (design §21.1, §22).
func (b *builder) buildConjunction(k plan.KindSet, c components, path string) plan.CompilationPlan {
	switch {
	case len(c.refs) > 0:
		// A bare `$ref` (or `$dynamicRef`) is the reference's own plan; anything else in
		// the conjunction becomes a residual obligation over it (issue #78).
		base := b.build(c.refs[0], path)
		rest := c
		rest.refs = c.refs[1:]
		return b.withResidualConjuncts(k, base, rest, path)

	case len(c.combinators) >= 1:
		primary := c.combinators[0]
		rest := c
		rest.combinators = append([]ir.Expr{}, c.combinators[1:]...)
		return b.buildUnionWithContext(k, primary, rest, path)

	default:
		return b.buildKindRestricted(k, c, path)
	}
}
