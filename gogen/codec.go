package gogen

import (
	"fmt"
	"strconv"
	"strings"
)

// codec writes the JSON methods a named type needs beyond what [encoding/json] does on its
// own, and returns why it wrote none when it wrote none.
//
// Most types need nothing: a `map[string]T` or a `[]T` already encodes correctly. Three do.
// An object needs one because presence is not a thing struct tags can express — `omitempty`
// does not look inside [opt.Opt], and an overflow map has to be flattened into the same
// object rather than nested under a key. An enum needs one because Go constants restrict
// nothing, so the literals are only enforced if decoding checks them. A required property
// needs one because [Split] discharges `required` against the field not being optional,
// which is a claim only a decoder can make true.
func (r *renderer) codec(n *Named) string {
	switch u := n.Underlying.(type) {
	case *Struct:
		return r.structCodec(n, u)
	case *Enum:
		return r.enumCodec(n, u)
	case *Tuple:
		return "a tuple is encoded as an array, which is not written yet"
	default:
		return ""
	}
}

func (r *renderer) structCodec(n *Named, s *Struct) string {
	if len(s.Patterns) > 0 {
		// A pattern routes keys by an ECMA-262 regex. Go's `regexp` is RE2 and answers
		// differently on the constructs that differ, which is issue #111 one layer down:
		// a decoder that used it would route keys the schema does not.
		return "patternProperties needs an ECMA-262 engine in generated code"
	}
	r.usesJSON = true
	r.usesFmt = true

	// Encoding only needs writing when the overflow map has to be flattened into the same
	// object, which is the one thing struct tags cannot say. Presence is already covered:
	// `omitzero` consults the field's IsZero, so encoding/json leaves an absent one out.
	if s.Additional != nil {
		r.marshalStruct(n, s)
	}
	r.unmarshalStruct(n, s)
	return ""
}

func (r *renderer) marshalStruct(n *Named, s *Struct) {
	p := &r.b
	fmt.Fprintf(p, "// MarshalJSON implements json.Marshaler.\nfunc (s %s) MarshalJSON() ([]byte, error) {\n", n.Name)
	p.WriteString("b := []byte{'{'}\nn := 0\nvar err error\n")

	for _, f := range s.Fields {
		key := strconv.Quote(f.JSON)
		switch pres, _ := f.Type.(*Presence); {
		case pres != nil && pres.Optional && pres.Nullable:
			// Absent writes no key; an explicit null writes one holding null. That
			// distinction is the whole point of the three-state type.
			fmt.Fprintf(p, "if !s.%s.IsAbsent() {\n", f.Name)
			r.encodeField(key, "s."+f.Name, f.JSON)
			p.WriteString("}\n")
		case pres != nil && pres.Optional:
			fmt.Fprintf(p, "if v, ok := s.%s.Get(); ok {\n", f.Name)
			r.encodeField(key, "v", f.JSON)
			p.WriteString("}\n")
		default:
			r.encodeField(key, "s."+f.Name, f.JSON)
		}
	}

	if s.Additional != nil {
		r.usesSort = true
		name := overflowName(s)
		fmt.Fprintf(p, "for _, k := range sortedKeys(s.%s) {\n", name)
		r.encodeField("k", "s."+name+"[k]", "")
		p.WriteString("}\n")
	}

	p.WriteString("b = append(b, '}')\nreturn b, nil\n}\n\n")
}

// encodeField writes one key and its value. The value goes through json.Marshal, so a
// nested type's own methods run and this does not restate them.
func (r *renderer) encodeField(key, value, name string) {
	p := &r.b
	fmt.Fprintf(p, "if b, err = encodeKey(b, &n, %s); err != nil {\nreturn nil, err\n}\n", key)
	p.WriteString("{\nd, err := json.Marshal(" + value + ")\nif err != nil {\n")
	if name == "" {
		p.WriteString("return nil, fmt.Errorf(\"encode %q: %w\", k, err)\n")
	} else {
		fmt.Fprintf(p, "return nil, fmt.Errorf(\"encode %%q: %%w\", %s, err)\n", strconv.Quote(name))
	}
	p.WriteString("}\nb = append(b, d...)\n}\n")
}

func (r *renderer) unmarshalStruct(n *Named, s *Struct) {
	p := &r.b
	fmt.Fprintf(p, "// UnmarshalJSON implements json.Unmarshaler.\nfunc (s *%s) UnmarshalJSON(data []byte) error {\n", n.Name)
	p.WriteString("var raw map[string]json.RawMessage\nif err := json.Unmarshal(data, &raw); err != nil {\nreturn err\n}\n")
	fmt.Fprintf(p, "var out %s\n", n.Name)

	for _, f := range s.Fields {
		key := strconv.Quote(f.JSON)
		fmt.Fprintf(p, "if v, ok := raw[%s]; ok {\n", key)
		fmt.Fprintf(p, "if err := json.Unmarshal(v, &out.%s); err != nil {\nreturn fmt.Errorf(\"decode %%q: %%w\", %s, err)\n}\n", f.Name, key)
		fmt.Fprintf(p, "delete(raw, %s)\n", key)
		if pres, ok := f.Type.(*Presence); !ok || !pres.Optional {
			// The type has no way to say the property was missing, so the decoder is
			// the only place `required` can be enforced.
			fmt.Fprintf(p, "} else {\nreturn fmt.Errorf(\"missing required property %%q\", %s)\n}\n", key)
			continue
		}
		p.WriteString("}\n")
	}

	if s.Additional != nil {
		r.usesSort = true
		name := overflowName(s)
		p.WriteString("if len(raw) > 0 {\n")
		fmt.Fprintf(p, "out.%s = make(map[string]", name)
		r.typ(s.Additional)
		fmt.Fprintf(p, ", len(raw))\nfor _, k := range sortedKeys(raw) {\nvar e ")
		r.typ(s.Additional)
		p.WriteString("\nif err := json.Unmarshal(raw[k], &e); err != nil {\nreturn fmt.Errorf(\"decode %q: %w\", k, err)\n}\n")
		fmt.Fprintf(p, "out.%s[k] = e\n}\n}\n", name)
	} else {
		r.usesSort = true
		p.WriteString("if len(raw) > 0 {\nreturn fmt.Errorf(\"unexpected property %q\", sortedKeys(raw)[0])\n}\n")
	}

	p.WriteString("*s = out\nreturn nil\n}\n\n")
}

func (r *renderer) enumCodec(n *Named, e *Enum) string {
	prim, ok := deref(e.Elem).(*Primitive)
	if !ok {
		return "an enum over mixed kinds needs a decoder that dispatches on kind"
	}
	lits := make([]string, len(e.Values))
	for i, v := range e.Values {
		lit, ok := goLiteral(v, e.Elem)
		if !ok {
			return "a literal with no Go form"
		}
		lits[i] = lit
	}
	r.usesJSON = true
	r.usesFmt = true

	p := &r.b
	fmt.Fprintf(p, "// UnmarshalJSON implements json.Unmarshaler. Go constants restrict nothing,\n"+
		"// so this is where the admitted values are enforced.\nfunc (v *%s) UnmarshalJSON(data []byte) error {\n", n.Name)
	fmt.Fprintf(p, "var raw %s\nif err := json.Unmarshal(data, &raw); err != nil {\nreturn err\n}\n", goPrimitive(prim.Kind))
	fmt.Fprintf(p, "switch raw {\ncase %s:\n*v = %s(raw)\nreturn nil\ndefault:\n", strings.Join(lits, ", "), n.Name)
	fmt.Fprintf(p, "return fmt.Errorf(\"%%v is not a valid %s\", raw)\n}\n}\n\n", n.Name)
	return ""
}

// overflowName repeats the name the struct renderer gave the overflow slot, which is the
// declared fields' names plus a suffix when one of them took it.
func overflowName(s *Struct) string {
	taken := make(map[string]bool, len(s.Fields))
	for _, f := range s.Fields {
		taken[f.Name] = true
	}
	for i := range s.Patterns {
		taken[freeName("Pattern"+strconv.Itoa(i)+"Props", taken)] = true
	}
	return freeName("AdditionalProps", taken)
}

// codecHelpers are the two functions the generated methods share, emitted once per file so
// each method stays about the type it belongs to.
const codecHelpers = `
func encodeKey(b []byte, n *int, k string) ([]byte, error) {
	if *n > 0 {
		b = append(b, ',')
	}
	*n++
	key, err := json.Marshal(k)
	if err != nil {
		return nil, err
	}
	b = append(b, key...)
	return append(b, ':'), nil
}

// sortedKeys orders map iteration, so encoding a value twice produces the same bytes.
func sortedKeys[T any](m map[string]T) []string {
	return slices.Sorted(maps.Keys(m))
}
`
