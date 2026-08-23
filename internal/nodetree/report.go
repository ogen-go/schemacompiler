package nodetree

import (
	"strconv"

	"github.com/go-faster/jx"

	"github.com/ogen-go/schemacompiler/internal/ecmaregex"
	"github.com/ogen-go/schemacompiler/plan"
)

func (a all) errs(raw []byte, at *loc, yield func(Error) bool) bool {
	for _, n := range a {
		if !report(n, raw, at, yield) {
			return false
		}
	}
	return true
}

func (g guard) errs(raw []byte, at *loc, yield func(Error) bool) bool {
	k, valid := kindOf(raw)
	if !valid {
		return yield(Error{Location: at.String(), Keyword: "json", Message: "malformed value"})
	}
	if !g.set.Has(k) {
		return true
	}
	return report(g.inner, raw, at, yield)
}

func (a assertKind) errs(raw []byte, at *loc, yield func(Error) bool) bool {
	k, valid := kindOf(raw)
	if !valid {
		return yield(Error{Location: at.String(), Keyword: "json", Message: "malformed value"})
	}
	if !a.set.Has(k) {
		return yield(Error{
			Location: at.String(),
			Keyword:  "type",
			Message:  "value is " + kindName(k) + ", want " + kindSetName(a.set),
		})
	}
	if a.inner == nil {
		return true
	}
	return report(a.inner, raw, at, yield)
}

func (l lengthBound) errs(raw []byte, at *loc, yield func(Error) bool) bool {
	keyword := "maxLength"
	if l.isMin {
		keyword = "minLength"
	}
	return leaf(l, raw, at, keyword, strconv.FormatUint(l.bound, 10), yield)
}

func (p patternNode) errs(raw []byte, at *loc, yield func(Error) bool) bool {
	return leaf(p, raw, at, "pattern", strconv.Quote(p.regex), yield)
}

func (n numericBound) errs(raw []byte, at *loc, yield func(Error) bool) bool {
	keyword := "maximum"
	switch {
	case n.isMin && n.exclusive:
		keyword = "exclusiveMinimum"
	case n.isMin:
		keyword = "minimum"
	case n.exclusive:
		keyword = "exclusiveMaximum"
	}
	return leaf(n, raw, at, keyword, n.want.RatString(), yield)
}

func (m multipleOf) errs(raw []byte, at *loc, yield func(Error) bool) bool {
	return leaf(m, raw, at, "multipleOf", m.divisor.RatString(), yield)
}

func (n numericDomain) errs(raw []byte, at *loc, yield func(Error) bool) bool {
	want := "an integer"
	if n.domain == plan.NonIntegerOnly {
		want = "a non-integer"
	}
	return leaf(n, raw, at, "type", "number is not "+want, yield)
}

func (i itemsBound) errs(raw []byte, at *loc, yield func(Error) bool) bool {
	keyword := "maxItems"
	if i.isMin {
		keyword = "minItems"
	}
	return leaf(i, raw, at, keyword, strconv.FormatUint(i.bound, 10), yield)
}

func (u uniqueItems) errs(raw []byte, at *loc, yield func(Error) bool) bool {
	return leaf(u, raw, at, "uniqueItems", "duplicate element", yield)
}

func (r requiredNode) errs(raw []byte, at *loc, yield func(Error) bool) bool {
	seen, ok := r.set.presenceOf(raw)
	if !ok {
		return true
	}
	for i, name := range r.set.names {
		if seen.has(i) {
			continue
		}
		if !yield(Error{Location: at.String(), Keyword: "required", Message: strconv.Quote(name) + " is absent"}) {
			return false
		}
	}
	return true
}

func (p propertiesBound) errs(raw []byte, at *loc, yield func(Error) bool) bool {
	keyword := "maxProperties"
	if p.isMin {
		keyword = "minProperties"
	}
	return leaf(p, raw, at, keyword, strconv.FormatUint(p.bound, 10), yield)
}

func (dr dependentRequired) errs(raw []byte, at *loc, yield func(Error) bool) bool {
	return leaf(dr, raw, at, "dependentRequired", "", yield)
}

func (n negation) errs(raw []byte, at *loc, yield func(Error) bool) bool {
	// The operand's own errors are why it *passed*, so reporting them would invert the
	// explanation. The negation is the whole message.
	return leaf(n, raw, at, "not", "instance matches the negated sub-schema", yield)
}

func (s shape) errs(raw []byte, at *loc, yield func(Error) bool) bool {
	return report(s.inner, raw, at, yield)
}

func (r reference) errs(raw []byte, at *loc, yield func(Error) bool) bool {
	n, ok := r.defs[r.name]
	if !ok || n == nil {
		return true
	}
	return report(n, raw, at, yield)
}

func (p propertyNames) errs(raw []byte, at *loc, yield func(Error) bool) bool {
	d := decoder(raw)
	defer jx.PutDecoder(d)

	stopped := false
	if err := d.ObjBytes(func(d *jx.Decoder, key []byte) error {
		if stopped {
			return d.Skip()
		}
		name := string(key)
		if !p.inner.ok(jx.Raw(strconv.Quote(name))) {
			if !yield(Error{
				Location: at.String(),
				Keyword:  "propertyNames",
				Message:  strconv.Quote(name) + " is not an allowed name",
			}) {
				stopped = true
			}
		}
		return d.Skip()
	}); err != nil {
		return true
	}
	return !stopped
}

func (c containsCount) errs(raw []byte, at *loc, yield func(Error) bool) bool {
	items, ok := rawElems(raw)
	if !ok {
		return true
	}
	var n uint64
	for _, item := range items {
		if c.inner.ok(item) {
			n++
		}
	}
	if n >= c.min && (!c.hasMax || n <= c.max) {
		return true
	}
	want := "at least " + strconv.FormatUint(c.min, 10)
	if c.hasMax {
		want = strconv.FormatUint(c.min, 10) + "-" + strconv.FormatUint(c.max, 10)
	}
	return yield(Error{
		Location: at.String(),
		Keyword:  "contains",
		Message:  strconv.FormatUint(n, 10) + " elements match, want " + want,
	})
}

func (o objectStructure) errs(raw []byte, at *loc, yield func(Error) bool) bool {
	d := decoder(raw)
	defer jx.PutDecoder(d)

	seen := o.set.newPresence()
	stopped := false
	if err := d.ObjBytes(func(d *jx.Decoder, key []byte) error {
		if stopped {
			return d.Skip()
		}
		field, declared := o.set.lookup(key)
		name := string(key)
		value, err := d.Raw()
		if err != nil {
			return err
		}
		child := at.child(name)

		if declared {
			seen.set(field)
			f := o.fields[field]
			if f.nullable && isNull(value) {
				return nil
			}
			if !report(f.inner, value, &child, yield) {
				stopped = true
			}
			return nil
		}

		matched := false
		for _, p := range o.patterns {
			switch res, err := ecmaregex.MatchString(p.pattern, name); {
			case err != nil, res == ecmaregex.Unknown:
				matched = true
				continue
			case res == ecmaregex.NoMatch:
				continue
			}
			matched = true
			if !report(p.inner, value, &child, yield) {
				stopped = true
				return nil
			}
		}
		if matched || o.additional == nil {
			return nil
		}
		if !report(o.additional, value, &child, yield) {
			stopped = true
		}
		return nil
	}); err != nil {
		return true
	}
	if stopped {
		return false
	}
	for i, f := range o.fields {
		if !f.required || seen.has(i) {
			continue
		}
		if !yield(Error{
			Location: at.String(),
			Keyword:  "required",
			Message:  strconv.Quote(f.name) + " is absent",
		}) {
			return false
		}
	}
	return true
}

func (a arrayStructure) errs(raw []byte, at *loc, yield func(Error) bool) bool {
	d := decoder(raw)
	defer jx.PutDecoder(d)

	i := 0
	stopped := false
	if err := d.Arr(func(d *jx.Decoder) error {
		if stopped {
			return d.Skip()
		}
		index := i
		i++
		value, err := d.Raw()
		if err != nil {
			return err
		}
		elem := at.elem(index)
		switch {
		case index < len(a.prefix):
			if !report(a.prefix[index], value, &elem, yield) {
				stopped = true
			}
		case a.restForbidden:
			if !yield(Error{
				Location: elem.String(),
				Keyword:  "items",
				Message:  "element is past the tuple prefix",
			}) {
				stopped = true
			}
		default:
			if !report(a.rest, value, &elem, yield) {
				stopped = true
			}
		}
		return nil
	}); err != nil {
		return true
	}
	return !stopped
}

func (k kindDispatch) errs(raw []byte, at *loc, yield func(Error) bool) bool {
	kind, valid := kindOf(raw)
	if !valid {
		return yield(Error{Location: at.String(), Keyword: "json", Message: "malformed value"})
	}
	n, ok := k.cases[kind]
	if !ok {
		return yield(Error{
			Location: at.String(),
			Keyword:  "type",
			Message:  "no branch accepts " + kindName(kind),
		})
	}
	return report(n, raw, at, yield)
}

func (l literalDispatch) errs(raw []byte, at *loc, yield func(Error) bool) bool {
	n, ok := selectCase(l.cases, raw)
	if !ok {
		return yield(Error{Location: at.String(), Keyword: "enum", Message: "value is not one of the allowed literals"})
	}
	return report(n, raw, at, yield)
}

func (p propertyDispatch) errs(raw []byte, at *loc, yield func(Error) bool) bool {
	tag, found, isObject := property(raw, p.property)
	if !isObject {
		return yield(Error{Location: at.String(), Keyword: "discriminator", Message: "value is not an object"})
	}
	if !found {
		return yield(Error{
			Location: at.String(),
			Keyword:  "discriminator",
			Message:  strconv.Quote(p.property) + " is absent",
		})
	}
	n, ok := selectCase(p.cases, tag)
	if !ok {
		return yield(Error{
			Location: childLocation(at, p.property),
			Keyword:  "discriminator",
			Message:  "no branch is tagged " + string(tag),
		})
	}
	return report(n, raw, at, yield)
}

func (p presenceDispatch) errs(raw []byte, at *loc, yield func(Error) bool) bool {
	_, found, isObject := property(raw, p.property)
	if !isObject {
		return true
	}
	if found {
		return report(p.present, raw, at, yield)
	}
	return report(p.absent, raw, at, yield)
}

func (p predicateCountDispatch) errs(raw []byte, at *loc, yield func(Error) bool) bool {
	matches := 0
	for _, b := range p.branches {
		if b.ok(raw) {
			matches++
		}
	}
	if matches >= p.min && matches <= p.max {
		return true
	}
	return yield(Error{
		Location: at.String(),
		Keyword:  "oneOf",
		Message: strconv.Itoa(matches) + " branches match, want " +
			strconv.Itoa(p.min) + "-" + strconv.Itoa(p.max),
	})
}
