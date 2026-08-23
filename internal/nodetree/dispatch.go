package nodetree

import (
	"github.com/go-faster/jx"

	"github.com/ogen-go/schemacompiler/internal/jsonequal"
	"github.com/ogen-go/schemacompiler/plan"
)

type kindDispatch struct{ cases map[plan.JSONKind]node }

func (k kindDispatch) ok(raw []byte) bool {
	kind, valid := kindOf(raw)
	if !valid {
		return false
	}
	n, ok := k.cases[kind]
	if !ok {
		return false
	}
	return n.ok(raw)
}

type literalCase struct {
	raw   []byte
	inner node
}

// selectCase finds the case whose literal equals selector, comparing as JSON values
// rather than bytes so `1` and `1.0` agree.
func selectCase(cases []literalCase, selector []byte) (node, bool) {
	for _, c := range cases {
		eq, err := jsonequal.Equal(c.raw, selector)
		if err != nil || !eq {
			continue
		}
		return c.inner, true
	}
	return nil, false
}

type literalDispatch struct{ cases []literalCase }

func (l literalDispatch) ok(raw []byte) bool {
	n, ok := selectCase(l.cases, raw)
	if !ok {
		return false
	}
	return n.ok(raw)
}

type propertyDispatch struct {
	property string
	cases    []literalCase
}

func (p propertyDispatch) ok(raw []byte) bool {
	tag, found, isObject := property(raw, p.property)
	if !isObject || !found {
		return false
	}
	n, ok := selectCase(p.cases, tag)
	if !ok {
		return false
	}
	return n.ok(raw)
}

type presenceDispatch struct {
	property        string
	present, absent node
}

func (p presenceDispatch) ok(raw []byte) bool {
	_, found, isObject := property(raw, p.property)
	if !isObject {
		// dependentSchemas applies to objects only; a dispatch that cannot select cannot
		// reject (matching planterp).
		return true
	}
	if found {
		return p.present.ok(raw)
	}
	return p.absent.ok(raw)
}

// property returns the raw value of name, whether it was found, and whether raw was an
// object at all.
func property(raw []byte, name string) (value []byte, found, isObject bool) {
	d := decoder(raw)
	defer jx.PutDecoder(d)
	if err := d.ObjBytes(func(d *jx.Decoder, key []byte) error {
		if found || string(key) != name {
			return d.Skip()
		}
		v, err := d.Raw()
		if err != nil {
			return err
		}
		value, found = v, true
		return nil
	}); err != nil {
		return nil, false, false
	}
	return value, found, true
}

type predicateCountDispatch struct {
	branches []node
	min, max int
}

func (p predicateCountDispatch) ok(raw []byte) bool {
	matches := 0
	for _, b := range p.branches {
		if b.ok(raw) {
			matches++
			if matches > p.max {
				return false
			}
		}
	}
	return matches >= p.min
}
