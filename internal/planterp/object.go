package planterp

import (
	"strconv"

	"github.com/ogen-go/schemacompiler/plan"
)

// objectShape is an [plan.ObjectStructurePredicate] as the instance-directed pass needs it:
// the declared fields in plan order, the plan covering everything else, and the pattern
// rules in plan order. It is assembled from the predicate's [planwalk.Edge]-labeled
// children rather than from its fields.
//
// declared is built once per object rather than per property: the uncovered-property scan
// below asks "is this name a field?" for every property of the instance, and a linear scan
// of fields per property would make that quadratic (issue #89).
type objectShape struct {
	fields     []objectField
	declared   map[string]struct{}
	additional *plan.CompilationPlan
	patterns   []patternRule
}

type objectField struct {
	name     string
	plan     plan.CompilationPlan
	presence plan.PresenceMode
	nullable bool
}

type patternRule struct {
	pattern string
	plan    plan.CompilationPlan
}

// objectAgainst checks obj against shape: every declared property against its own plan,
// then every remaining property against the pattern rules and Additional.
func (in *interp) objectAgainst(shape objectShape, obj map[string]any, f frame) (Verdict, error) {
	for _, field := range shape.fields {
		v, err := in.field(field, obj, f)
		if err != nil || !v.Accepted {
			return v, err
		}
	}

	for name, pv := range obj {
		if _, declared := shape.declared[name]; declared {
			continue
		}
		matched, v, err := in.patternRules(shape.patterns, name, pv, f)
		if err != nil || !v.Accepted {
			return v, err
		}
		if matched {
			continue
		}
		if shape.additional == nil {
			// A nil Additional means additional properties are not representable as a
			// field (plan.ObjectRepresentation's contract). That is a statement about
			// storage, not about validity, so it cannot reject on its own.
			continue
		}
		v, err = in.plan(*shape.additional, pv, f.descend(name))
		if err != nil {
			return Verdict{}, err
		}
		if !v.Accepted {
			return rejectedBy(f, "additionalProperties", strconv.Quote(name), v.Reason), nil
		}
	}
	return accepted(), nil
}

func (in *interp) field(field objectField, obj map[string]any, f frame) (Verdict, error) {
	at := f.descend(field.name)

	fv, present := obj[field.name]
	if !present {
		switch field.presence {
		case plan.PresenceRequired:
			return rejected(at, "field", "required but absent"), nil
		case plan.PresenceOptional:
			// An absent property has no value for the field's plan to check: the plan
			// constrains what is stored there, not whether anything is.
			return accepted(), nil
		default:
			return Verdict{}, internalf("unhandled plan.PresenceMode %d", field.presence)
		}
	}
	if fv == nil && field.nullable {
		// Nullability is carried by the field, not by its plan: the planner strips null
		// out of the field's own expression (design §7.1).
		return accepted(), nil
	}
	v, err := in.plan(field.plan, fv, at)
	if err != nil {
		return Verdict{}, err
	}
	if !v.Accepted {
		return rejectedBy(f, "field", strconv.Quote(field.name), v.Reason), nil
	}
	return accepted(), nil
}

// patternRules runs every pattern rule whose pattern matches name. It reports whether
// any rule matched, so the caller knows not to fall through to Additional.
func (in *interp) patternRules(rules []patternRule, name string, value any, f frame) (bool, Verdict, error) {
	matched := false
	for _, rule := range rules {
		switch in.matchPattern(rule.pattern, name) {
		case patternNoMatch:
			continue
		case patternUnknown:
			// Whether the rule covers name is undecidable, and both answers can reject a
			// valid instance: running the rule's plan on a name it does not cover, or
			// falling through to an Additional that forbids it. Claiming the name without
			// constraining it is the only branch that cannot (design §24).
			matched = true
			continue
		}
		matched = true
		v, err := in.plan(rule.plan, value, f.descend(name))
		if err != nil {
			return false, Verdict{}, err
		}
		if !v.Accepted {
			return true, rejectedBy(f, "patternProperties["+strconv.Quote(rule.pattern)+"]",
				strconv.Quote(name), v.Reason), nil
		}
	}
	return matched, accepted(), nil
}

// referencePlan follows a [plan.ReferencePredicate]: the instance must satisfy the plan
// Name resolves to, looked up in the document graph the root plan carried.
func (in *interp) referencePlan(name string, value any, f frame) (Verdict, error) {
	if f.active[name] {
		// Following the same reference twice at one instance node cannot terminate and
		// cannot be given a verdict; a silent accept would hide an unguarded cycle.
		return Verdict{}, internalf("reference cycle at %q with no instance descent", name)
	}
	return in.definition(name, value, f.follow(name))
}

// definition runs the whole-document plan name resolves to. f must already carry the
// follow marker.
func (in *interp) definition(name string, value any, f frame) (Verdict, error) {
	def, ok := in.defs[plan.SchemaID(name)]
	if !ok {
		return Verdict{}, internalf("reference %q resolves to no definition in the plan", name)
	}
	return in.plan(def, value, f)
}
