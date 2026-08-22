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
	if _, ok := res.(plan.DynamicReferenceGraph); ok {
		return plan.DynamicSchemaResolution
	}

	switch disp.(type) {
	case plan.NoDispatch:
	case plan.PredicateCountDispatch:
		return plan.PredicateDispatch
	case plan.KindDispatch, plan.LiteralDispatch, plan.PropertyDispatch, plan.PresenceDispatch:
		return plan.StaticDispatch
	default:
		return plan.Unsupported
	}

	if !onlyKindAssertions(val) {
		return plan.GoTypeWithValidation
	}

	switch rep.(type) {
	case plan.AnyRepresentation, plan.NeverRepresentation, plan.PrimitiveRepresentation,
		plan.ObjectRepresentation, plan.ArrayRepresentation, plan.UnionRepresentation,
		plan.RecursiveRepresentation, plan.ReferenceRepresentation:
		return plan.DirectGoType
	default:
		return plan.Unsupported
	}
}

// onlyKindAssertions reports whether every check in val is a bare kind assertion.
//
// Under design §4.1 the validation plan is total, so it is never empty once a `type` is
// stated and the old "no residual validation" test would make [plan.DirectGoType]
// unreachable. §22 amends it: DirectGoType means every check is discharged by the chosen
// representation, and a kind assertion is exactly that — the representation was built for
// that kind, so a value the Go type can hold has already satisfied it. Anything with an
// Expression is a real runtime check and is not discharged.
func onlyKindAssertions(val plan.ValidationPlan) bool {
	for _, p := range val.Predicates {
		if p.Expression != nil {
			return false
		}
	}
	return true
}

// exactnessOf derives the top-level Exactness from a finished plan's capability and
// whether it carries residual validation (design §24, §25).
//
// Exactness is a property of the accepted set of the whole plan — representation ∧
// dispatch ∧ validation — never of the representation alone. §24's contract is the
// biconditional x ⊨ S ⟺ x ∈ ⟦G(S)⟧ ∧ V(S,x), so a representation wider than the schema
// is exact whenever the residual validator closes the gap. That is what
// [plan.ExactWithValidation] means, and it is the only thing distinguishing it from
// [plan.ExactPureRepresentation]: `string` admits "ab" for `{"type":"string","minLength":3}`
// exactly as `any` does for the bare `{"minLength":3}`, and the kind-guarded MinLength
// rejects it in both (design §3, issue #95).
//
// [plan.SoundOverApproximation] is therefore reserved for a plan whose accepted set is a
// strict superset of the schema's *after* validation runs, with the plan's own machinery
// bounding the excess — today only an asserted discriminator, which trusts a declared tag
// instead of proving the branches disjoint while every branch still validates. When
// nothing bounds the excess the rung is [plan.DeclaredIncomplete] (issue #84).
//
// g reports the gaps the plan's own structure does not show. A cost-only classification
// never demotes exactness: [plan.PredicateDispatch] means match-counting or a residual
// negation is expensive to run, not that it is approximate — the lowering contract on
// [plan.PredicateCountDispatch] is exact, and [withResidualNegation] only emits a
// negation over an exactly modeled operand.
func exactnessOf(p plan.CompilationPlan, g gaps) plan.Exactness {
	if p.Capability >= plan.EvaluationStateValidation {
		return plan.UnsupportedConversion
	}
	if _, never := p.Representation.(plan.NeverRepresentation); never {
		return plan.ExactPureRepresentation
	}
	if g.dropped {
		return plan.DeclaredIncomplete
	}
	if g.asserted {
		return plan.SoundOverApproximation
	}
	if onlyKindAssertions(p.Validation) && p.Capability == plan.DirectGoType {
		return plan.ExactPureRepresentation
	}
	return plan.ExactWithValidation
}
