package planterp

import (
	"strconv"

	"github.com/go-faster/errors"

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
		var t struct{} = r
		_ = t
		return accepted(), nil

	case plan.NeverRepresentation:
		var t struct{} = r
		_ = t
		return rejected("never: no instance is valid"), nil

	case plan.PrimitiveRepresentation:
		var t struct {
			Kind    plan.JSONKind
			Numeric plan.NumericDomain
			Format  string
		} = r
		return in.primitive(t.Kind, t.Numeric, value)

	case plan.ObjectRepresentation:
		var t struct {
			Fields       map[string]plan.FieldRepresentation
			Additional   plan.Representation
			PatternRules []plan.PatternFieldRepresentation
		} = r
		return in.object(t.Fields, t.Additional, t.PatternRules, value, f)

	case plan.ArrayRepresentation:
		var t struct {
			Prefix []plan.ItemRepresentation
			Rest   plan.ItemRepresentation
		} = r
		return in.array(t.Prefix, t.Rest, value, f)

	case plan.UnionRepresentation:
		var t struct {
			Alternatives []plan.Representation
		} = r
		for _, alt := range t.Alternatives {
			v, err := in.representation(alt, value, f)
			if err != nil {
				return Verdict{}, err
			}
			if v.Accepted {
				return accepted(), nil
			}
		}
		return rejected("union: no alternative can hold the value"), nil

	case plan.RecursiveRepresentation:
		var t struct {
			Name string
			Body plan.Representation
		} = r
		return in.representation(t.Body, value, f.bind(t.Name, t.Body))

	case plan.ReferenceRepresentation:
		var t struct {
			Name string
		} = r
		return in.reference(t.Name, value, f)

	default:
		return Verdict{}, errors.Errorf("planterp: unhandled plan.Representation variant %T", r)
	}
}

func (in *interp) primitive(kind plan.JSONKind, numeric plan.NumericDomain, value any) (Verdict, error) {
	k, err := kindOf(value)
	if err != nil {
		return Verdict{}, err
	}
	if k != kind {
		return rejected("primitive: value is " + kindName(k) + ", representation is " + kindName(kind)), nil
	}
	if kind != plan.KindNumber {
		return accepted(), nil
	}

	integral, err := isInteger(value)
	if err != nil {
		return Verdict{}, err
	}
	switch numeric {
	case plan.AnyNumber:
		return accepted(), nil
	case plan.IntegerOnly:
		if !integral {
			return rejected("primitive: number is not an integer"), nil
		}
		return accepted(), nil
	case plan.NonIntegerOnly:
		if integral {
			return rejected("primitive: number is an integer"), nil
		}
		return accepted(), nil
	default:
		return Verdict{}, errors.Errorf("planterp: unhandled plan.NumericDomain %d", numeric)
	}
}

// object checks an ObjectRepresentation (design §7, §12). Declared fields own their
// property name; pattern rules and Additional cover the rest. A property covered by a
// field is not also run through a matching pattern rule: the plan states one storage
// slot per property, and it is the field.
func (in *interp) object(
	fields map[string]plan.FieldRepresentation,
	additional plan.Representation,
	patternRules []plan.PatternFieldRepresentation,
	value any,
	f frame,
) (Verdict, error) {
	obj, ok := value.(map[string]any)
	if !ok {
		k, err := kindOf(value)
		if err != nil {
			return Verdict{}, err
		}
		return rejected("object: value is " + kindName(k)), nil
	}

	for name, fr := range fields {
		v, err := in.field(name, fr, obj, f)
		if err != nil || !v.Accepted {
			return v, err
		}
	}

	for name, pv := range obj {
		if _, declared := fields[name]; declared {
			continue
		}
		matched, v, err := in.patternRules(patternRules, name, pv, f)
		if err != nil || !v.Accepted {
			return v, err
		}
		if matched {
			continue
		}
		if additional == nil {
			// A nil Additional means additional properties are not representable as a
			// field (plan.ObjectRepresentation's contract). That is a statement about
			// storage, not about validity, so it cannot reject on its own.
			continue
		}
		v, err = in.representation(additional, pv, f.descend())
		if err != nil {
			return Verdict{}, err
		}
		if !v.Accepted {
			return rejected("additionalProperties[" + strconv.Quote(name) + "]: " + v.Reason), nil
		}
	}
	return accepted(), nil
}

func (in *interp) field(name string, fr plan.FieldRepresentation, obj map[string]any, f frame) (Verdict, error) {
	var t struct {
		Representation plan.Representation
		Presence       plan.PresenceMode
		Nullable       bool
		Metadata       plan.Metadata
	} = fr

	fv, present := obj[name]
	if !present {
		switch t.Presence {
		case plan.PresenceRequired:
			return rejected("field " + strconv.Quote(name) + ": required but absent"), nil
		case plan.PresenceOptional:
			return accepted(), nil
		default:
			return Verdict{}, errors.Errorf("planterp: unhandled plan.PresenceMode %d", t.Presence)
		}
	}
	if fv == nil && t.Nullable {
		return accepted(), nil
	}
	v, err := in.representation(t.Representation, fv, f.descend())
	if err != nil {
		return Verdict{}, err
	}
	if !v.Accepted {
		return rejected("field " + strconv.Quote(name) + ": " + v.Reason), nil
	}
	return accepted(), nil
}

// patternRules runs every pattern rule whose pattern matches name. It reports whether
// any rule matched, so the caller knows not to fall through to Additional.
func (in *interp) patternRules(rules []plan.PatternFieldRepresentation, name string, value any, f frame) (bool, Verdict, error) {
	matched := false
	for _, rule := range rules {
		var t struct {
			Pattern        string
			Representation plan.Representation
			Metadata       plan.Metadata
		} = rule

		if !in.matchPattern(t.Pattern, name) {
			continue
		}
		matched = true
		v, err := in.representation(t.Representation, value, f.descend())
		if err != nil {
			return false, Verdict{}, err
		}
		if !v.Accepted {
			return true, rejected("patternProperties[" + strconv.Quote(t.Pattern) + "][" +
				strconv.Quote(name) + "]: " + v.Reason), nil
		}
	}
	return matched, accepted(), nil
}

func (in *interp) array(prefix []plan.ItemRepresentation, rest plan.ItemRepresentation, value any, f frame) (Verdict, error) {
	items, ok := value.([]any)
	if !ok {
		k, err := kindOf(value)
		if err != nil {
			return Verdict{}, err
		}
		return rejected("array: value is " + kindName(k)), nil
	}

	for i, item := range items {
		var slot plan.ItemRepresentation
		switch {
		case i < len(prefix):
			slot = prefix[i]
		default:
			slot = rest
		}
		var t struct {
			Representation plan.Representation
			Metadata       plan.Metadata
		} = slot
		if t.Representation == nil {
			if i < len(prefix) {
				return Verdict{}, errors.Errorf("planterp: prefix item %d has no representation", i)
			}
			// A nil Rest.Representation means there are no items past the prefix
			// (plan.ArrayRepresentation's contract).
			return rejected("array: item " + strconv.Itoa(i) + " is past the tuple prefix"), nil
		}
		v, err := in.representation(t.Representation, item, f.descend())
		if err != nil {
			return Verdict{}, err
		}
		if !v.Accepted {
			return rejected("array[" + strconv.Itoa(i) + "]: " + v.Reason), nil
		}
	}
	return accepted(), nil
}

// reference follows a ReferenceRepresentation through the recursion binders in scope and
// then through the document's definition graph (design §10.1).
func (in *interp) reference(name string, value any, f frame) (Verdict, error) {
	if f.active[name] {
		// Following the same reference twice at one instance node cannot terminate and
		// cannot be given a verdict; a silent accept would hide an unguarded cycle.
		return Verdict{}, errors.Errorf("planterp: reference cycle at %q with no instance descent", name)
	}
	next := f.follow(name)

	if body, ok := f.binders[name]; ok {
		return in.representation(body, value, next)
	}
	def, ok := in.defs[plan.SchemaID(name)]
	if !ok {
		return Verdict{}, errors.Errorf("planterp: reference %q resolves to no definition in the plan", name)
	}
	return in.plan(def, value, next)
}
