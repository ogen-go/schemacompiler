package planner

import (
	"github.com/ogen-go/schemacompiler/plan"
)

// objectStructure restates an [plan.ObjectRepresentation] as the check design §4.1 puts
// in the validation plan. The two are emitted together and describe the same object:
// the representation says where a property is stored, this says what may be there.
//
// The sub-plans are shared rather than rebuilt — a [plan.CompilationPlan] is a shallow
// struct, so the second reference costs a header and not a copy of the subtree.
func objectStructure(rep plan.ObjectRepresentation) plan.GuardedPredicate {
	e := plan.ObjectStructurePredicate{
		Properties: make([]plan.PropertyCheck, 0, len(rep.Fields)),
		Additional: rep.Additional,
	}
	for _, f := range rep.Fields {
		e.Properties = append(e.Properties, plan.PropertyCheck{
			Name:     f.Name,
			Plan:     f.Plan,
			Presence: f.Presence,
			Nullable: f.Nullable,
		})
	}
	for _, r := range rep.PatternRules {
		e.Patterns = append(e.Patterns, plan.PatternCheck{Pattern: r.Pattern, Plan: r.Plan})
	}
	return plan.GuardedPredicate{Applicability: plan.SetObject, Expression: &e}
}

// arrayStructure is [objectStructure] for an [plan.ArrayRepresentation]. A Rest slot with
// no representation means nothing is admitted past the tuple prefix, which the predicate
// spells as a nil Rest.
func arrayStructure(rep plan.ArrayRepresentation) plan.GuardedPredicate {
	e := plan.ArrayStructurePredicate{Prefix: make([]plan.CompilationPlan, 0, len(rep.Prefix))}
	for _, p := range rep.Prefix {
		e.Prefix = append(e.Prefix, p.Plan)
	}
	if rep.Rest.Plan.Representation != nil {
		rest := rep.Rest.Plan
		e.Rest = &rest
	}
	return plan.GuardedPredicate{Applicability: plan.SetArray, Expression: &e}
}
