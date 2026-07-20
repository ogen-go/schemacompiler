package planner

import "github.com/ogen-go/schemacompiler/plan"

// classify implements design §22: the capability of a plan is the maximum of what its
// resolution, dispatch, validation, and representation each require. Callers that build
// composite plans (objects, arrays, unions) additionally roll up the capability of every
// part via maxCapability before calling classify on the local contribution, so the
// overall result is never lower than any nested part (design §22's recursive rule).
func classify(rep plan.Representation, val plan.ValidationPlan, disp plan.DispatchPlan, res plan.ResolutionPlan) plan.CapabilityLevel {
	if _, ok := res.(plan.DynamicReferenceGraph); ok {
		return plan.DynamicSchemaResolution
	}

	switch d := disp.(type) {
	case plan.PredicateCountDispatch:
		_ = d
		return plan.PredicateDispatch
	case plan.KindDispatch, plan.LiteralDispatch, plan.PropertyDispatch, plan.PresenceDispatch:
		return plan.StaticDispatch
	}

	if !val.Empty() {
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

// exactnessOf derives the top-level Exactness from a finished plan's capability and
// whether it carries residual validation (design §24, §25).
func exactnessOf(p plan.CompilationPlan) plan.Exactness {
	switch p.Capability {
	case plan.EvaluationStateValidation, plan.DynamicSchemaResolution, plan.Unsupported:
		return plan.UnsupportedConversion
	}
	if _, never := p.Representation.(plan.NeverRepresentation); never {
		return plan.ExactPureRepresentation
	}
	if _, isAny := p.Representation.(plan.AnyRepresentation); isAny && !p.Validation.Empty() {
		// A schema with no representable type restriction plus residual predicates:
		// the Any representation is a strict over-approximation of the accepted set.
		return plan.SoundOverApproximation
	}
	if p.Validation.Empty() && p.Capability == plan.DirectGoType {
		return plan.ExactPureRepresentation
	}
	if p.Capability == plan.PredicateDispatch {
		return plan.SoundOverApproximation
	}
	return plan.ExactWithValidation
}
