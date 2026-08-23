package planner

import "github.com/ogen-go/schemacompiler/plan"

// classify implements design §22: the capability of a plan is the maximum of what its
// resolution, dispatch, validation, and representation each require. Callers that build
// composite plans (objects, arrays, unions) additionally roll up the capability of every
// part via maxCapability before calling classify on the local contribution, so the
// overall result is never lower than any nested part (design §22's recursive rule).
//
// Both variant switches are whitelists whose unknown arm returns [plan.Unsupported]:
// design §24 forbids under-approximating cost, so a variant this function has not been
// taught about must fail toward "too expensive to lower" rather than toward a plain Go
// type that would silently drop the machinery the plan needs.
func classify(rep plan.Representation, val plan.ValidationPlan, disp plan.DispatchPlan, res plan.ResolutionPlan) plan.CapabilityLevel {
	if _, ok := res.(*plan.DynamicReferenceGraph); ok {
		return plan.DynamicSchemaResolution
	}

	switch disp.(type) {
	case *plan.NoDispatch:
	case *plan.PredicateCountDispatch:
		return plan.PredicateDispatch
	case *plan.KindDispatch, *plan.LiteralDispatch, *plan.PropertyDispatch, *plan.PresenceDispatch:
		return plan.StaticDispatch
	default:
		return plan.Unsupported
	}

	if len(plan.ResidualChecks(rep, val)) > 0 {
		return plan.GoTypeWithValidation
	}

	switch rep.(type) {
	case *plan.AnyRepresentation, *plan.NeverRepresentation, *plan.PrimitiveRepresentation,
		*plan.ObjectRepresentation, *plan.ArrayRepresentation, *plan.UnionRepresentation,
		*plan.RecursiveRepresentation, *plan.ReferenceRepresentation:
		return plan.DirectGoType
	default:
		return plan.Unsupported
	}
}

// exactlyModeled reports whether p's accepted set is its schema's own, rather than a
// superset of it (design §24). It is the one place the compiler judges its own fidelity,
// and it is deliberately not reported: §25.1 retired the Exactness ladder because whether
// the *generated program* reproduces the schema depends on the lowering — integer widths,
// retained unknown properties, the regex engine — none of which the compiler chooses.
//
// What survives is this internal boolean, because negation needs it. `not S` inverts the
// approximation polarity of S (§8.2), so the standard over-approximating fallback would
// turn into an under-approximation of the negation and reject valid instances. Only an
// exactly modeled operand may be negated ([withResidualNegation]) or trusted through a
// reference ([refTrusted]).
//
// A wider representation is not itself inexact: §24's contract is the biconditional
// x ⊨ S ⟺ x ∈ ⟦G(S)⟧ ∧ V(S,x), so `string` for `{"type":"string","minLength":3}` is exact
// once the kind-guarded MinLength runs (issue #95). Nor is cost: [plan.PredicateDispatch]
// means match-counting is expensive, not approximate.
//
// g reports the gaps p's own structure does not show, and both are disqualifying: an
// asserted discriminator may route a mis-tagged instance to a branch that accepts it, and
// a dropped constraint is closed by nothing at all.
func exactlyModeled(p plan.CompilationPlan, g gaps) bool {
	if p.Capability >= plan.EvaluationStateValidation {
		return false
	}
	if _, never := p.Representation.(*plan.NeverRepresentation); never {
		return true
	}
	return !g.dropped && !g.asserted
}
