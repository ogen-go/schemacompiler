package planner

import "github.com/ogen-go/schemacompiler/plan"

// anyPlan is the trivial plan for Any (schema `true`): every JSON value is accepted,
// nothing to validate, dispatch, or resolve.
func anyPlan() plan.CompilationPlan {
	return plan.CompilationPlan{
		Representation: plan.AnyRepresentation{},
		Dispatch:       plan.NoDispatch{},
		Resolution:     plan.FullyResolved{},
		Capability:     plan.DirectGoType,
	}
}

// neverPlanAt is the trivial plan for Never (schema `false` or an unsatisfiable
// intersection): the empty type is itself an exact, if uninhabited, representation.
func (b *builder) neverPlanAt(_ string) plan.CompilationPlan {
	return plan.CompilationPlan{
		Representation: plan.NeverRepresentation{},
		Dispatch:       plan.NoDispatch{},
		Resolution:     plan.FullyResolved{},
		Capability:     plan.DirectGoType,
	}
}

// kindBit returns the singleton KindSet bit for k.
func kindBit(k plan.JSONKind) plan.KindSet { return 1 << plan.KindSet(k) }

// splitKinds returns every singleton kind present in k, in JSONKind order.
func splitKinds(k plan.KindSet) []plan.JSONKind {
	var out []plan.JSONKind
	for kind := plan.KindNull; kind <= plan.KindObject; kind++ {
		if k.Has(kind) {
			out = append(out, kind)
		}
	}
	return out
}

// maxCapability returns the higher (less capable) of a and b on the capability ladder
// (design §22: composite capability is the max over its parts).
func maxCapability(a, b plan.CapabilityLevel) plan.CapabilityLevel {
	if b > a {
		return b
	}
	return a
}

// mergeResolution combines several ResolutionPlans into their least-capable common form
// (FullyResolved < StaticReferenceGraph < DynamicReferenceGraph), merging any static
// definitions and dynamic anchors along the way.
func mergeResolution(parts ...plan.ResolutionPlan) plan.ResolutionPlan {
	var (
		dyn        bool
		static     bool
		statics    map[plan.SchemaID]plan.CompilationPlan
		dynAnchors map[string][]plan.SchemaID
	)
	for _, p := range parts {
		switch v := p.(type) {
		case nil, plan.FullyResolved:
		case plan.StaticReferenceGraph:
			static = true
			for k, val := range v.Definitions {
				if statics == nil {
					statics = make(map[plan.SchemaID]plan.CompilationPlan)
				}
				statics[k] = val
			}
		case plan.DynamicReferenceGraph:
			dyn = true
			for k, val := range v.StaticDefinitions {
				if statics == nil {
					statics = make(map[plan.SchemaID]plan.CompilationPlan)
				}
				statics[k] = val
			}
			for k, val := range v.DynamicAnchors {
				if dynAnchors == nil {
					dynAnchors = make(map[string][]plan.SchemaID)
				}
				dynAnchors[k] = append(dynAnchors[k], val...)
			}
		}
	}
	switch {
	case dyn:
		return plan.DynamicReferenceGraph{StaticDefinitions: statics, DynamicAnchors: dynAnchors}
	case static:
		return plan.StaticReferenceGraph{Definitions: statics}
	default:
		return plan.FullyResolved{}
	}
}

// mergePlans folds an auxiliary plan's validation, capability, and resolution into a
// base plan, keeping the base's representation and dispatch (used for the best-effort
// `$ref` + sibling-constraint case, where the ref supplies the representation and the
// remaining local constraints only add residual validation).
func mergePlans(base, extra plan.CompilationPlan) plan.CompilationPlan {
	base.Validation.Predicates = append(base.Validation.Predicates, extra.Validation.Predicates...)
	base.Capability = maxCapability(base.Capability, extra.Capability)
	base.Resolution = mergeResolution(base.Resolution, extra.Resolution)
	return base
}

// literalKind returns the JSON kind of a decoded literal value (json.Unmarshal form).
func literalKind(v any) plan.JSONKind {
	switch v.(type) {
	case nil:
		return plan.KindNull
	case bool:
		return plan.KindBoolean
	case float64, int, int64:
		return plan.KindNumber
	case string:
		return plan.KindString
	case []any:
		return plan.KindArray
	case map[string]any:
		return plan.KindObject
	default:
		return plan.KindObject
	}
}
