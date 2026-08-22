package planterp

import (
	"strconv"

	"github.com/ogen-go/schemacompiler/internal/planwalk"
	"github.com/ogen-go/schemacompiler/plan"
)

// representation reports whether the Go shape the plan describes can hold value
// (design §7). A representation that cannot hold it is a rejection: soundness (§24)
// says every valid instance must fit, so a value that does not fit is not valid.
func (in *interp) representation(r plan.Representation, value any, f frame) (Verdict, error) {
	if r == nil {
		// No representation at all constrains nothing; the plan carries no shape to
		// check the value against.
		return accepted(), nil
	}

	switch r := r.(type) {
	case plan.AnyRepresentation:
		return accepted(), nil
	case plan.NeverRepresentation:
		return rejected(f, "never", "no instance is valid"), nil
	case plan.PrimitiveRepresentation:
		return in.primitive(r, value, f)
	case plan.ObjectRepresentation:
		return in.object(r, value, f)
	case plan.ArrayRepresentation:
		return in.array(r, value, f)
	case plan.UnionRepresentation:
		return in.union(r, value, f)
	case plan.RecursiveRepresentation:
		return in.recursive(r, value, f)
	case plan.ReferenceRepresentation:
		return in.reference(r.Name, value, f)
	default:
		return Verdict{}, internalf("unhandled plan.Representation variant %T", r)
	}
}

func (in *interp) primitive(r plan.PrimitiveRepresentation, value any, f frame) (Verdict, error) {
	k, err := kindOf(value)
	if err != nil {
		return Verdict{}, withPath(f.path, err)
	}
	if k != r.Kind {
		return rejected(f, "primitive", "value is "+kindName(k)+", representation is "+kindName(r.Kind)), nil
	}
	if r.Kind != plan.KindNumber {
		return accepted(), nil
	}

	integral, err := isInteger(value)
	if err != nil {
		return Verdict{}, withPath(f.path, err)
	}
	switch r.Numeric {
	case plan.AnyNumber:
		return accepted(), nil
	case plan.IntegerOnly:
		if !integral {
			return rejected(f, "primitive", "number is not an integer"), nil
		}
		return accepted(), nil
	case plan.NonIntegerOnly:
		if integral {
			return rejected(f, "primitive", "number is an integer"), nil
		}
		return accepted(), nil
	default:
		return Verdict{}, internalf("unhandled plan.NumericDomain %d", r.Numeric)
	}
}

// objectShape is an [plan.ObjectRepresentation] as the instance-directed pass needs it:
// the declared fields in plan order, the plan covering everything else, and the pattern
// rules in plan order. It is assembled from the representation's [planwalk.Edge]-labeled
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

// objectShapeOf reads the representation's children, having first checked the slots that
// carry no representation at all. Only Additional documents an absent slot as meaningful;
// a plan without a representation anywhere else is malformed plan data, and interpreting
// it would constrain nothing - widening the accepted set silently, where design §24 wants
// a loud failure.
func objectShapeOf(r plan.ObjectRepresentation, f frame) (objectShape, error) {
	for _, field := range r.Fields {
		if field.Plan.Representation == nil {
			return objectShape{}, internalf("field %q at %s has no representation",
				field.Name, instanceLocation(f))
		}
	}
	for i, rule := range r.PatternRules {
		if rule.Plan.Representation == nil {
			return objectShape{}, internalf("pattern rule %d (%q) at %s has no representation",
				i, rule.Pattern, instanceLocation(f))
		}
	}

	shape := objectShape{declared: make(map[string]struct{}, len(r.Fields))}
	for c := range planwalk.Children(planwalk.RepresentationNode(r)) {
		switch c.Edge.Kind {
		case planwalk.EdgeField:
			shape.fields = append(shape.fields, objectField{
				name:     c.Edge.Name,
				plan:     c.Plan,
				presence: c.Edge.Presence,
				nullable: c.Edge.Nullable,
			})
			shape.declared[c.Edge.Name] = struct{}{}
		case planwalk.EdgeAdditional:
			additional := c.Plan
			shape.additional = &additional
		case planwalk.EdgePatternRule:
			shape.patterns = append(shape.patterns, patternRule{
				pattern: c.Edge.Name,
				plan:    c.Plan,
			})
		default:
			return objectShape{}, internalf("unhandled object representation child edge %s", c.Edge.Kind)
		}
	}
	return shape, nil
}

// object checks an ObjectRepresentation (design §7, §12). Declared fields own their
// property name; pattern rules and Additional cover the rest. A property covered by a
// field is not also run through a matching pattern rule: the plan states one storage slot
// per property, it is the field, and the planner has already intersected every matching
// pattern schema into that field's own plan (design §12.3).
func (in *interp) object(r plan.ObjectRepresentation, value any, f frame) (Verdict, error) {
	shape, err := objectShapeOf(r, f)
	if err != nil {
		return Verdict{}, err
	}

	obj, ok := value.(map[string]any)
	if !ok {
		k, err := kindOf(value)
		if err != nil {
			return Verdict{}, withPath(f.path, err)
		}
		return rejected(f, "object", "value is "+kindName(k)), nil
	}
	return in.objectAgainst(shape, obj, f)
}

// objectAgainst is the instance-directed half of an object check, shared by the
// [plan.ObjectRepresentation] reading above and the [plan.ObjectStructurePredicate]
// reading in structure.go. Both describe the same shape; only the source differs.
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

// array stays on the representation's own fields rather than on [planwalk.Children]:
// the traversal drops a Rest slot carrying no representation, which would hide both the
// tuple's length and the malformed-prefix check below.
func (in *interp) array(r plan.ArrayRepresentation, value any, f frame) (Verdict, error) {
	items, ok := value.([]any)
	if !ok {
		k, err := kindOf(value)
		if err != nil {
			return Verdict{}, withPath(f.path, err)
		}
		return rejected(f, "array", "value is "+kindName(k)), nil
	}

	for i, item := range items {
		slot := r.Rest
		if i < len(r.Prefix) {
			slot = r.Prefix[i]
		}
		at := f.descend(strconv.Itoa(i))
		if slot.Plan.Representation == nil {
			if i < len(r.Prefix) {
				return Verdict{}, internalf("prefix item %d at %s has no representation",
					i, instanceLocation(f))
			}
			// A nil Rest.Plan.Representation means there are no items past the prefix
			// (plan.ArrayRepresentation's contract).
			return rejected(at, "array", "item is past the tuple prefix"), nil
		}
		v, err := in.plan(slot.Plan, item, at)
		if err != nil {
			return Verdict{}, err
		}
		if !v.Accepted {
			return rejectedBy(f, "array item", strconv.Itoa(i), v.Reason), nil
		}
	}
	return accepted(), nil
}

func (in *interp) union(r plan.UnionRepresentation, value any, f frame) (Verdict, error) {
	for i, alt := range r.Alternatives {
		// planwalk.Children drops a nil alternative, which would turn a malformed union
		// into one that rejects rather than one that fails loudly (design §24).
		if alt == nil {
			return Verdict{}, internalf("union alternative %d at %s has no representation",
				i, instanceLocation(f))
		}
	}

	var last *ValidateError
	for c := range planwalk.Children(planwalk.RepresentationNode(r)) {
		if c.Edge.Kind != planwalk.EdgeAlternative {
			return Verdict{}, internalf("unhandled union representation child edge %s", c.Edge.Kind)
		}
		v, err := in.representation(c.Representation, value, f)
		if err != nil {
			return Verdict{}, err
		}
		if v.Accepted {
			return accepted(), nil
		}
		last = v.Reason
	}
	return rejectedBy(f, "union", "no alternative can hold the value", last), nil
}

func (in *interp) recursive(r plan.RecursiveRepresentation, value any, f frame) (Verdict, error) {
	for c := range planwalk.Children(planwalk.RepresentationNode(r)) {
		if c.Edge.Kind != planwalk.EdgeRecursiveBody {
			return Verdict{}, internalf("unhandled recursive representation child edge %s", c.Edge.Kind)
		}
		return in.representation(c.Representation, value, f.bind(c.Edge.Name, c.Representation))
	}
	return accepted(), nil
}

// reference follows a ReferenceRepresentation through the recursion binders in scope and
// then through the document's definition graph (design §10.1).
func (in *interp) reference(name string, value any, f frame) (Verdict, error) {
	if f.active[name] {
		// Following the same reference twice at one instance node cannot terminate and
		// cannot be given a verdict; a silent accept would hide an unguarded cycle.
		return Verdict{}, internalf("reference cycle at %q with no instance descent", name)
	}
	next := f.follow(name)

	if body, ok := f.binders[name]; ok {
		return in.representation(body, value, next)
	}
	def, ok := in.defs[plan.SchemaID(name)]
	if !ok {
		return Verdict{}, internalf("reference %q resolves to no definition in the plan", name)
	}
	return in.plan(def, value, next)
}
