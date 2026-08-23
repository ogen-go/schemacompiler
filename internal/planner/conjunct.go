package planner

import "github.com/ogen-go/schemacompiler/plan"

// withResidualConjuncts folds everything a conjunction holds besides its first `$ref` into
// the plan built from that ref.
//
// `allOf` is an unordered intersection (design §11.5), so no member may be dropped for
// being second — and a widened representation is only sound while residual validation
// rejects what it lets through (design §24). A reference contributes a name rather than a
// structure, so the remaining conjuncts cannot be intersected into it the way sibling
// shapes are merged into one another (design §12.3, §15.2): they survive instead as a
// whole-instance obligation over the sub-plan they build on their own, which is what
// [plan.ShapePredicate] carries. Conjuncts that need nothing but residual predicates are
// merged flat, since a wrapper would only nest the same checks (issue #78).
func (b *builder) withResidualConjuncts(k plan.KindSet, base plan.CompilationPlan, rest components, path string) plan.CompilationPlan {
	if rest.empty() {
		return base
	}
	sub := b.buildConjunction(k, rest, path)
	if _, never := sub.Representation.(*plan.NeverRepresentation); never {
		return b.neverPlanAt(path)
	}
	if validationOnly(sub) {
		return mergePlans(base, sub)
	}
	b.diag(path, plan.DiagnosticAdvisory, plan.SeverityInfo,
		"$ref conjoined with further constraints: enforced against the instance, not merged into the referenced type")
	base.Validation.Predicates = append(base.Validation.Predicates, plan.GuardedPredicate{
		Applicability: plan.SetAny,
		Expression:    &plan.ShapePredicate{Schema: sub},
	})
	base.Capability = maxCapability(base.Capability, sub.Capability)
	base.Resolution = mergeResolution(base.Resolution, sub.Resolution)
	return base
}

// empty reports whether c carries no structural contribution of its own. A bare kind
// restriction counts as none: the kinds are already folded into the caller's aggregate
// and reach the plan through it.
func (c components) empty() bool {
	return len(c.refs) == 0 && len(c.shapes) == 0 && len(c.predicates) == 0 &&
		c.literal == nil && len(c.combinators) == 0 && len(c.nots) == 0
}

// validationOnly reports whether p constrains instances through its residual predicates
// alone, so [mergePlans] carries all of it and its representation and dispatch are not
// worth preserving.
func validationOnly(p plan.CompilationPlan) bool {
	if _, isAny := p.Representation.(*plan.AnyRepresentation); !isAny {
		return false
	}
	_, noDispatch := p.Dispatch.(*plan.NoDispatch)
	return noDispatch
}
