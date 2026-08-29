package gogen

import (
	"encoding/json"
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
		return r.tupleCodec(n, u)
	default:
		return ""
	}
}

// enumCodec enforces the admitted values. Two ways, and the difference is only how the
// comparison is written.
//
// Literals with a Go form are compared as Go values, which is exact and cheap. Everything
// else — an object, an array, a mixed-kind set — is compared as a decoded JSON value, so
// key order and number spelling do not matter, and the value handed back is whatever the
// schema said it was. There is nothing to name in that case and nothing to name it with:
// an enum entry is a JSON literal inside an array, not a schema, so it has nowhere to carry
// an `x-go-name`. What it loses is the constants, never the check.
func (r *renderer) enumCodec(n *Named, e *Enum) string {
	if lits, ok := goLiterals(e); ok {
		r.primitiveEnumCodec(n, e, lits)
		return ""
	}
	cases, ok := canonicalCases(e)
	if !ok {
		return "two admitted values share one canonical form"
	}

	if s, ok := deref(e.Elem).(*Struct); ok {
		if len(s.Patterns) > 0 {
			return patternReason
		}
		r.need("encoding/json", "fmt")
		r.needHelper("codec")
		if s.Additional != nil {
			r.marshalStruct(n, s)
		}
		fmt.Fprintf(&r.b, "// UnmarshalJSON implements json.Unmarshaler.\nfunc (s *%s) UnmarshalJSON(data []byte) error {\n", n.Name)
		r.enumGuard(n, cases)
		r.unmarshalStructBody(n, s)
		r.b.WriteString("}\n\n")
		return ""
	}
	if _, ok := deref(e.Elem).(*Presence); ok {
		return "an enum whose values include null needs a decoder that dispatches on kind"
	}

	r.need("encoding/json", "fmt")
	p := &r.b
	fmt.Fprintf(p, "// UnmarshalJSON implements json.Unmarshaler.\nfunc (v *%s) UnmarshalJSON(data []byte) error {\n", n.Name)
	r.enumGuard(n, cases)
	p.WriteString("var out ")
	r.typ(e.Elem)
	p.WriteString("\nif err := json.Unmarshal(data, &out); err != nil {\nreturn err\n}\n")
	fmt.Fprintf(p, "*v = %s(out)\nreturn nil\n}\n\n", n.Name)
	return ""
}

// enumGuard writes the membership check: the instance in canonical form against the
// admitted values in canonical form.
//
// Canonical means decoded and re-encoded, so object key order and number spelling stop
// mattering — which is what JSON equality means. The generator canonicalizes the literals
// with the same two calls the generated code makes on the instance, so the two sides cannot
// disagree about what equal is. Nothing is stored: the admitted set is a list of string
// cases in a switch, not a package variable holding decoded values.
func (r *renderer) enumGuard(n *Named, cases []string) {
	r.needHelper("canon")
	quoted := make([]string, len(cases))
	for i, c := range cases {
		quoted[i] = strconv.Quote(c)
	}
	fmt.Fprintf(&r.b, "canon, err := canonicalJSON(data)\nif err != nil {\nreturn err\n}\n"+
		"switch canon {\ncase %s:\ndefault:\nreturn fmt.Errorf(\"%%s is not an admitted %s\", data)\n}\n",
		strings.Join(quoted, ", "), n.Name)
}

// canonicalCases is every admitted value in canonical form, or false when two of them share
// one — which float64 can do to two integers past its precision. Refusing there is the same
// rule as a colliding constant name: a check that cannot tell two admitted values apart is
// not one to generate.
func canonicalCases(e *Enum) ([]string, bool) {
	out := make([]string, 0, len(e.Values))
	seen := make(map[string]bool, len(e.Values))
	for _, v := range e.Values {
		c, err := canonicalJSON(rawOf(v))
		if err != nil || seen[c] {
			return nil, false
		}
		seen[c] = true
		out = append(out, c)
	}
	return out, true
}

// canonicalJSON is the generator's half of the comparison, and is the two calls the
// generated helper makes.
func canonicalJSON(data []byte) (string, error) {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return "", err
	}
	out, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// rawOf is the literal's source bytes, or its Go value re-encoded when the plan kept none.
func rawOf(v EnumValue) []byte {
	if len(v.Raw) > 0 {
		return v.Raw
	}
	out, err := json.Marshal(v.Value)
	if err != nil {
		return []byte("null")
	}
	return out
}

// goLiterals is the Go source of every admitted value, or false when one has none.
func goLiterals(e *Enum) ([]string, bool) {
	if _, ok := deref(e.Elem).(*Primitive); !ok {
		return nil, false
	}
	lits := make([]string, len(e.Values))
	for i, v := range e.Values {
		lit, ok := goLiteral(v, e.Elem)
		if !ok {
			return nil, false
		}
		lits[i] = lit
	}
	return lits, true
}

func (r *renderer) structCodec(n *Named, s *Struct) string {
	if len(s.Patterns) > 0 {
		return patternReason
	}
	r.need("encoding/json", "fmt")

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
		r.needHelper("codec")
		name := slotsOf(s).Additional
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
	fmt.Fprintf(&r.b, "// UnmarshalJSON implements json.Unmarshaler.\nfunc (s *%s) UnmarshalJSON(data []byte) error {\n", n.Name)
	r.unmarshalStructBody(n, s)
	r.b.WriteString("}\n\n")
}

func (r *renderer) unmarshalStructBody(n *Named, s *Struct) {
	p := &r.b
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
		r.needHelper("codec")
		name := slotsOf(s).Additional
		p.WriteString("if len(raw) > 0 {\n")
		fmt.Fprintf(p, "out.%s = make(map[string]", name)
		r.typ(s.Additional)
		fmt.Fprintf(p, ", len(raw))\nfor _, k := range sortedKeys(raw) {\nvar e ")
		r.typ(s.Additional)
		p.WriteString("\nif err := json.Unmarshal(raw[k], &e); err != nil {\nreturn fmt.Errorf(\"decode %q: %w\", k, err)\n}\n")
		fmt.Fprintf(p, "out.%s[k] = e\n}\n}\n", name)
	} else {
		r.needHelper("codec")
		p.WriteString("if len(raw) > 0 {\nreturn fmt.Errorf(\"unexpected property %q\", sortedKeys(raw)[0])\n}\n")
	}

	p.WriteString("*s = out\nreturn nil\n")
}

func (r *renderer) primitiveEnumCodec(n *Named, e *Enum, lits []string) {
	prim := deref(e.Elem).(*Primitive)
	r.need("encoding/json", "fmt")

	p := &r.b
	fmt.Fprintf(p, "// UnmarshalJSON implements json.Unmarshaler. Go constants restrict nothing,\n"+
		"// so this is where the admitted values are enforced.\nfunc (v *%s) UnmarshalJSON(data []byte) error {\n", n.Name)
	fmt.Fprintf(p, "var raw %s\nif err := json.Unmarshal(data, &raw); err != nil {\nreturn err\n}\n", goPrimitive(prim.Kind))
	fmt.Fprintf(p, "switch raw {\ncase %s:\n*v = %s(raw)\nreturn nil\ndefault:\n", strings.Join(lits, ", "), n.Name)
	fmt.Fprintf(p, "return fmt.Errorf(\"%%v is not a valid %s\", raw)\n}\n}\n\n", n.Name)
}

// tupleCodec writes a tuple as the JSON array it is.
//
// Without it a tuple encodes as a JSON *object* — `{"F0":"x","F1":true}` — because that is
// what encoding/json does with a struct, so this is not a convenience but the difference
// between right and wrong output.
//
// A slot past `minItems` is optional, and encoding stops at the first absent one: an array
// is positional, so there is no way to write a later item without the earlier one. A slot
// before it is required, and the decoder enforces that for the reason it enforces a required
// property — a bare slot has nowhere to record that the item was missing.
func (r *renderer) tupleCodec(n *Named, t *Tuple) string {
	if len(t.Elems) == 0 {
		return ""
	}
	r.need("encoding/json", "fmt")

	p := &r.b
	fmt.Fprintf(p, "// MarshalJSON implements json.Marshaler.\nfunc (s %s) MarshalJSON() ([]byte, error) {\n", n.Name)
	p.WriteString("b := []byte{'['}\nn := 0\n")
	for i, e := range t.Elems {
		name := slotsOfTuple(t).Elems[i]
		if pres, ok := e.(*Presence); ok && pres.Optional {
			fmt.Fprintf(p, "if v, ok := s.%s.Get(); ok {\n", name)
			r.appendItem("v", i)
			fmt.Fprintf(p, "} else if n < %d {\n"+
				"return nil, fmt.Errorf(\"item %d is set but item %%d is not; an array has no gap\", n)\n}\n", i, i)
			continue
		}
		r.appendItem("s."+name, i)
	}
	if t.Rest != nil {
		fmt.Fprintf(p, "for _, v := range s.%s {\n", slotsOfTuple(t).Rest)
		r.appendItem("v", -1)
		p.WriteString("}\n")
	}
	p.WriteString("b = append(b, ']')\nreturn b, nil\n}\n\n")

	fmt.Fprintf(p, "// UnmarshalJSON implements json.Unmarshaler.\nfunc (s *%s) UnmarshalJSON(data []byte) error {\n", n.Name)
	p.WriteString("var raw []json.RawMessage\nif err := json.Unmarshal(data, &raw); err != nil {\nreturn err\n}\n")
	fmt.Fprintf(p, "var out %s\n", n.Name)
	for i, e := range t.Elems {
		name := slotsOfTuple(t).Elems[i]
		fmt.Fprintf(p, "if len(raw) > %d {\nif err := json.Unmarshal(raw[%d], &out.%s); err != nil {\n"+
			"return fmt.Errorf(\"decode item %d: %%w\", err)\n}\n", i, i, name, i)
		if pres, ok := e.(*Presence); !ok || !pres.Optional {
			fmt.Fprintf(p, "} else {\nreturn fmt.Errorf(\"missing item %d\")\n}\n", i)
			continue
		}
		p.WriteString("}\n")
	}
	if t.Rest != nil {
		rest := slotsOfTuple(t).Rest
		fmt.Fprintf(p, "if len(raw) > %d {\nout.%s = make([]", len(t.Elems), rest)
		r.typ(t.Rest)
		fmt.Fprintf(p, ", 0, len(raw)-%d)\nfor i, item := range raw[%d:] {\nvar e ", len(t.Elems), len(t.Elems))
		r.typ(t.Rest)
		fmt.Fprintf(p, "\nif err := json.Unmarshal(item, &e); err != nil {\n"+
			"return fmt.Errorf(\"decode item %%d: %%w\", i+%d, err)\n}\n", len(t.Elems))
		fmt.Fprintf(p, "out.%s = append(out.%s, e)\n}\n}\n", rest, rest)
	} else {
		fmt.Fprintf(p, "if len(raw) > %d {\nreturn fmt.Errorf(\"expected at most %d items, got %%d\", len(raw))\n}\n",
			len(t.Elems), len(t.Elems))
	}
	p.WriteString("*s = out\nreturn nil\n}\n\n")
	return ""
}

// appendItem writes one array element, separated from the last.
func (r *renderer) appendItem(value string, index int) {
	p := &r.b
	p.WriteString("if n > 0 {\nb = append(b, ',')\n}\nn++\n")
	p.WriteString("{\nd, err := json.Marshal(" + value + ")\nif err != nil {\n")
	if index < 0 {
		p.WriteString("return nil, fmt.Errorf(\"encode item: %w\", err)\n")
	} else {
		fmt.Fprintf(p, "return nil, fmt.Errorf(\"encode item %d: %%w\", err)\n", index)
	}
	p.WriteString("}\nb = append(b, d...)\n}\n")
}

// patternReason is why a pattern-routing decoder is not written: Go's `regexp` is RE2 and
// answers differently from ECMA-262 on the constructs that differ, which is issue #111 one
// layer down — a decoder using it would route keys the schema does not.
const patternReason = "patternProperties needs an ECMA-262 engine in generated code"

// canonHelper is what an admitted-value check compares against. It holds no state: the
// admitted set is a switch over string cases, so there is nothing to initialize, nothing a
// caller can reach in, and no init order to depend on.
const canonHelper = `
// canonicalJSON re-encodes a value in one form, so two spellings of the same JSON compare
// equal: object keys sort and numbers take a single format.
func canonicalJSON(data []byte) (string, error) {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return "", err
	}
	out, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
`

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
