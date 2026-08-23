package nodetree

import (
	"encoding/json"

	"github.com/go-faster/jx"

	"github.com/ogen-go/schemacompiler/internal/ecmaregex"
	"github.com/ogen-go/schemacompiler/plan"
)

type fieldCheck struct {
	name     string
	inner    node
	required bool
	nullable bool
}

type patternField struct {
	pattern string
	inner   node
}

// objectStructure is `properties`, `patternProperties` and `additionalProperties` as one
// pass over the instance's keys. It mirrors planterp's objectAgainst, but visits each key
// once instead of building a map first.
type objectStructure struct {
	fields     []fieldCheck
	declared   map[string]struct{}
	patterns   []patternField
	additional node
	// requiredMask has a bit per required field, matched against the bits set while
	// walking the instance's keys. It replaces a per-instance map.
	requiredMask uint64
}

func (o objectStructure) ok(raw []byte) bool {
	d := decoder(raw)
	defer jx.PutDecoder(d)

	var seen uint64
	valid := true
	if err := d.ObjBytes(func(d *jx.Decoder, key []byte) error {
		if !valid {
			return d.Skip()
		}
		name := string(key)
		if _, declared := o.declared[name]; declared {
			for i, f := range o.fields {
				if f.name != name {
					continue
				}
				seen |= 1 << uint(i)
				value, err := d.Raw()
				if err != nil {
					return err
				}
				if f.nullable && isNull(value) {
					return nil
				}
				if !f.inner.ok(value) {
					valid = false
				}
				return nil
			}
		}
		value, err := d.Raw()
		if err != nil {
			return err
		}
		matched := false
		for _, p := range o.patterns {
			switch res, err := ecmaregex.MatchString(p.pattern, name); {
			case err != nil, res == ecmaregex.Unknown:
				// Undecidable: claim the name without constraining it, the only branch
				// that cannot reject a valid instance (design §24).
				matched = true
				continue
			case res == ecmaregex.NoMatch:
				continue
			}
			matched = true
			if !p.inner.ok(value) {
				valid = false
				return nil
			}
		}
		if matched || o.additional == nil {
			return nil
		}
		if !o.additional.ok(value) {
			valid = false
		}
		return nil
	}); err != nil {
		// Not an object: the kind assertion beside this predicate rejects one, and a
		// guard that cannot apply contributes nothing.
		return true
	}
	if !valid {
		return false
	}
	return seen&o.requiredMask == o.requiredMask
}

func isNull(raw []byte) bool {
	k, ok := kindOf(raw)
	return ok && k == plan.KindNull
}

// arrayStructure is `prefixItems` and `items`. A nil rest with restForbidden rejects every
// element past the prefix, matching `items: false`.
type arrayStructure struct {
	prefix        []node
	rest          node
	restForbidden bool
}

func (a arrayStructure) ok(raw []byte) bool {
	d := decoder(raw)
	defer jx.PutDecoder(d)

	i := 0
	valid := true
	if err := d.Arr(func(d *jx.Decoder) error {
		if !valid {
			return d.Skip()
		}
		var slot node
		switch {
		case i < len(a.prefix):
			slot = a.prefix[i]
		case a.restForbidden:
			valid = false
			i++
			return d.Skip()
		default:
			slot = a.rest
		}
		i++
		value, err := d.Raw()
		if err != nil {
			return err
		}
		if !slot.ok(value) {
			valid = false
		}
		return nil
	}); err != nil {
		return true
	}
	return valid
}

func encodeLiteral(v any) ([]byte, error) {
	return json.Marshal(v)
}
