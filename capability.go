package schemacompiler

import (
	"github.com/ogen-go/schemacompiler/internal/planwalk"
	"github.com/ogen-go/schemacompiler/plan"
)

// rollUpCapabilities raises every plan's capability to at least the capability of the
// plans it references, transitively (design §22: "the capability of an object is at least
// the maximum capability of ... local resolution"), and reports the references that name
// no plan at all.
//
// A `$ref` leaf is planned as DirectGoType because the planner only knows the reference's
// identity, not its target's cost; the target's plan is assembled here, so this is the
// first point where the roll-up can happen. Without it a component referencing an
// Unsupported one advertises DirectGoType, and a generator gating per plan
// (docs/integration.md §6) emits a type pointing at one it must refuse.
//
// A reference the map cannot answer is a dangling `$ref`: [internal/ir] keys those by the
// raw reference string, since there is no target pointer to key them by. Nothing can be
// generated for such a name, so the referring plan becomes Unsupported rather than keeping
// its optimistic level (design §24 forbids under-approximating the cost).
func rollUpCapabilities(plans map[plan.SchemaID]plan.CompilationPlan, positions map[plan.SchemaID]plan.Position) []plan.Diagnostic {
	var diags []plan.Diagnostic
	deps := make(map[plan.SchemaID][]plan.SchemaID, len(plans))
	for id, p := range plans {
		var refs []plan.SchemaID
		planReferences(p, func(target plan.SchemaID) {
			switch {
			case target == id:
				// A plan cannot raise its own level.
			case hasPlan(plans, target):
				refs = append(refs, target)
			default:
				p.Capability = plan.Unsupported
				plans[id] = p
				diags = append(diags, plan.Diagnostic{
					Pointer:  string(id),
					Position: positions[id],
					Kind:     plan.DiagnosticUnsupported,
					Severity: plan.SeverityError,
					Message:  "reference " + string(target) + " resolves to no compiled schema",
				})
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
	return diags
}

func hasPlan(plans map[plan.SchemaID]plan.CompilationPlan, id plan.SchemaID) bool {
	_, ok := plans[id]
	return ok
}

// planReferences reports every [plan.ReferenceRepresentation] name reachable within p:
// in its representation, in its dispatch branches, and in the nested plans a residual
// predicate carries.
func planReferences(p plan.CompilationPlan, visit func(plan.SchemaID)) {
	planwalk.Plan(p, func(r plan.Representation) {
		if ref, ok := r.(*plan.ReferenceRepresentation); ok {
			visit(plan.SchemaID(ref.Name))
		}
	})
}
