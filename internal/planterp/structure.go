package planterp

import (
	"strconv"

	"github.com/ogen-go/schemacompiler/internal/planwalk"
	"github.com/ogen-go/schemacompiler/plan"
)

// objectStructure checks an [plan.ObjectStructurePredicate]: the validation plan's own
// account of `properties`, `patternProperties` and `additionalProperties` (design §4.1).
//
// It shares [interp.objectAgainst] with [interp.object], which reads the same shape off
// the [plan.ObjectRepresentation]. The two descriptions are emitted together and describe
// the same object; only where they are read from differs.
func (in *interp) objectStructure(e plan.ObjectStructurePredicate, value any, f frame) (Verdict, error) {
	shape, err := structureShapeOf(e, f)
	if err != nil {
		return Verdict{}, err
	}
	obj, ok := value.(map[string]any)
	if !ok {
		// The kind assertion beside this predicate rejects a non-object; a guard that
		// cannot apply contributes nothing (design §3.1).
		return accepted(), nil
	}
	return in.objectAgainst(shape, obj, f)
}

func structureShapeOf(e plan.ObjectStructurePredicate, f frame) (objectShape, error) {
	for _, pc := range e.Properties {
		if pc.Plan.Representation == nil {
			return objectShape{}, internalf("property check %q at %s has no plan",
				pc.Name, instanceLocation(f))
		}
	}
	for i, pc := range e.Patterns {
		if pc.Plan.Representation == nil {
			return objectShape{}, internalf("pattern check %d (%q) at %s has no plan",
				i, pc.Pattern, instanceLocation(f))
		}
	}

	shape := objectShape{declared: make(map[string]struct{}, len(e.Properties))}
	for c := range planwalk.Children(planwalk.PredicateNode(e)) {
		switch c.Edge.Kind {
		case planwalk.EdgeField:
			shape.fields = append(shape.fields, objectField{
				name:     c.Edge.Name,
				plan:     c.Plan,
				presence: c.Edge.Presence,
				nullable: c.Edge.Nullable,
			})
			shape.declared[c.Edge.Name] = struct{}{}
		case planwalk.EdgePatternRule:
			shape.patterns = append(shape.patterns, patternRule{pattern: c.Edge.Name, plan: c.Plan})
		case planwalk.EdgeAdditional:
			additional := c.Plan
			shape.additional = &additional
		default:
			return objectShape{}, internalf("unhandled object structure child edge %s", c.Edge.Kind)
		}
	}
	return shape, nil
}

// arrayStructure checks an [plan.ArrayStructurePredicate], the validation plan's account
// of `prefixItems` and `items`.
func (in *interp) arrayStructure(e plan.ArrayStructurePredicate, value any, f frame) (Verdict, error) {
	items, ok := value.([]any)
	if !ok {
		return accepted(), nil
	}

	for i, item := range items {
		var slot *plan.CompilationPlan
		if i < len(e.Prefix) {
			slot = &e.Prefix[i]
			if slot.Representation == nil {
				return Verdict{}, internalf("prefix check %d at %s has no plan", i, instanceLocation(f))
			}
		} else {
			slot = e.Rest
		}
		at := f.descend(strconv.Itoa(i))
		if slot == nil {
			// A nil Rest is `items: false`: nothing past the prefix is admitted.
			return rejected(at, "items", "element is past the tuple prefix"), nil
		}
		v, err := in.plan(*slot, item, at)
		if err != nil {
			return Verdict{}, err
		}
		if !v.Accepted {
			return rejectedBy(f, "array item", strconv.Itoa(i), v.Reason), nil
		}
	}
	return accepted(), nil
}
