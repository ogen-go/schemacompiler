package schemacompiler

import "github.com/ogen-go/schemacompiler/plan"

// rollUpCapabilities raises every plan's capability to at least the capability of the
// plans it references, transitively (design §22: "the capability of an object is at least
// the maximum capability of ... local resolution").
//
// A `$ref` leaf is planned as DirectGoType because the planner only knows the reference's
// identity, not its target's cost; the target's plan is assembled here, so this is the
// first point where the roll-up can happen. Without it a component referencing an
// Unsupported one advertises DirectGoType, and a generator gating per plan
// (docs/integration.md §6) emits a type pointing at one it must refuse.
func rollUpCapabilities(plans map[plan.SchemaID]plan.CompilationPlan) {
	deps := make(map[plan.SchemaID][]plan.SchemaID, len(plans))
	for id, p := range plans {
		var refs []plan.SchemaID
		planReferences(p, func(target plan.SchemaID) {
			if _, ok := plans[target]; ok && target != id {
				refs = append(refs, target)
			}
		})
		deps[id] = refs
	}

	// Capability only ever rises and is bounded by Unsupported, so the fixed point
	// terminates even when references form a cycle (design §19).
	for changed := true; changed; {
		changed = false
		for id, p := range plans {
			level := p.Capability
			for _, dep := range deps[id] {
				level = maxCapability(level, plans[dep].Capability)
			}
			if level != p.Capability {
				p.Capability = level
				plans[id] = p
				changed = true
			}
		}
	}
}

// planReferences reports every [plan.ReferenceRepresentation] name reachable within p,
// including the ones nested in dispatch branches.
func planReferences(p plan.CompilationPlan, visit func(plan.SchemaID)) {
	representationReferences(p.Representation, visit)
	switch d := p.Dispatch.(type) {
	case plan.KindDispatch:
		for _, branch := range d.Cases {
			planReferences(branch, visit)
		}
	case plan.LiteralDispatch:
		for _, c := range d.Cases {
			planReferences(c.Plan, visit)
		}
	case plan.PropertyDispatch:
		for _, c := range d.Cases {
			planReferences(c.Plan, visit)
		}
	case plan.PresenceDispatch:
		planReferences(d.Present, visit)
		planReferences(d.Absent, visit)
	case plan.PredicateCountDispatch:
		for _, branch := range d.Branches {
			planReferences(branch, visit)
		}
	}
}

func representationReferences(r plan.Representation, visit func(plan.SchemaID)) {
	switch r := r.(type) {
	case plan.ReferenceRepresentation:
		visit(plan.SchemaID(r.Name))
	case plan.RecursiveRepresentation:
		representationReferences(r.Body, visit)
	case plan.ObjectRepresentation:
		for _, f := range r.Fields {
			representationReferences(f.Representation, visit)
		}
		representationReferences(r.Additional, visit)
		for _, pr := range r.PatternRules {
			representationReferences(pr.Representation, visit)
		}
	case plan.ArrayRepresentation:
		for _, item := range r.Prefix {
			representationReferences(item, visit)
		}
		representationReferences(r.Rest, visit)
	case plan.UnionRepresentation:
		for _, alt := range r.Alternatives {
			representationReferences(alt, visit)
		}
	}
}
