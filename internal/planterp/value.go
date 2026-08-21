package planterp

import (
	"bytes"
	"encoding/json"
	"math/big"
	"strconv"

	"github.com/go-faster/errors"

	"github.com/ogen-go/schemacompiler/plan"
)

// kindOf maps a decoded JSON value onto its [plan.JSONKind]. An unknown Go type is an
// error rather than a kind: guessing would let the interpreter accept a value it cannot
// reason about.
func kindOf(value any) (plan.JSONKind, error) {
	switch value.(type) {
	case nil:
		return plan.KindNull, nil
	case bool:
		return plan.KindBoolean, nil
	case float64, json.Number:
		return plan.KindNumber, nil
	case string:
		return plan.KindString, nil
	case []any:
		return plan.KindArray, nil
	case map[string]any:
		return plan.KindObject, nil
	default:
		return 0, errors.Errorf("planterp: not a decoded JSON value: %T", value)
	}
}

func kindName(k plan.JSONKind) string {
	switch k {
	case plan.KindNull:
		return "null"
	case plan.KindBoolean:
		return "boolean"
	case plan.KindNumber:
		return "number"
	case plan.KindString:
		return "string"
	case plan.KindArray:
		return "array"
	case plan.KindObject:
		return "object"
	default:
		return "kind(" + string(rune('0'+k)) + ")"
	}
}

// ratOf converts a decoded JSON number to an exact rational, so that comparisons and
// `multipleOf` do not inherit float64 rounding.
func ratOf(value any) (*big.Rat, error) {
	switch n := value.(type) {
	case json.Number:
		r, ok := new(big.Rat).SetString(n.String())
		if !ok {
			return nil, errors.Errorf("planterp: cannot parse number %q", n.String())
		}
		return r, nil
	case float64:
		r := new(big.Rat)
		if r.SetFloat64(n) == nil {
			return nil, errors.Errorf("planterp: number %v is not finite", n)
		}
		return r, nil
	default:
		return nil, errors.Errorf("planterp: not a JSON number: %T", value)
	}
}

// ratOfFloat converts a plan-carried float64 bound (plan.MinimumPredicate and friends)
// to a rational, through the shortest decimal that round-trips to it rather than through
// its binary expansion: `multipleOf: 0.0001` must compare as one ten-thousandth, not as
// 0.000100000000000000004792173602385929598312941379845142364501953125, or a bound the
// author wrote as a decimal rejects its own boundary value.
func ratOfFloat(v float64) (*big.Rat, error) {
	r, ok := new(big.Rat).SetString(strconv.FormatFloat(v, 'g', -1, 64))
	if !ok {
		return nil, errors.Errorf("planterp: bound %v is not a finite number", v)
	}
	return r, nil
}

// isInteger reports whether a JSON number has no fractional part. JSON Schema's
// `integer` type is value-based, not syntax-based: 1.0 is an integer.
func isInteger(value any) (bool, error) {
	r, err := ratOf(value)
	if err != nil {
		return false, err
	}
	return r.IsInt(), nil
}

// equalValues compares two decoded JSON values the way JSON Schema's `const`, `enum` and
// `uniqueItems` do: order-independent objects, exact numeric equality.
func equalValues(a, b any) (bool, error) {
	ka, err := kindOf(a)
	if err != nil {
		return false, err
	}
	kb, err := kindOf(b)
	if err != nil {
		return false, err
	}
	if ka != kb {
		return false, nil
	}

	switch ka {
	case plan.KindNull:
		return true, nil
	case plan.KindBoolean:
		return a.(bool) == b.(bool), nil
	case plan.KindString:
		return a.(string) == b.(string), nil
	case plan.KindNumber:
		ra, err := ratOf(a)
		if err != nil {
			return false, err
		}
		rb, err := ratOf(b)
		if err != nil {
			return false, err
		}
		return ra.Cmp(rb) == 0, nil
	case plan.KindArray:
		as, bs := a.([]any), b.([]any)
		if len(as) != len(bs) {
			return false, nil
		}
		for i := range as {
			eq, err := equalValues(as[i], bs[i])
			if err != nil || !eq {
				return eq, err
			}
		}
		return true, nil
	case plan.KindObject:
		am, bm := a.(map[string]any), b.(map[string]any)
		if len(am) != len(bm) {
			return false, nil
		}
		for k, av := range am {
			bv, ok := bm[k]
			if !ok {
				return false, nil
			}
			eq, err := equalValues(av, bv)
			if err != nil || !eq {
				return eq, err
			}
		}
		return true, nil
	default:
		return false, errors.Errorf("planterp: unhandled plan.JSONKind %d", ka)
	}
}

// literalValue is the instance-comparable value of a [plan.LiteralCase]. Raw is
// preferred when present: it is the exact source bytes, so a literal past float64's
// precision compares exactly (plan.LiteralCase's contract).
func literalValue(c plan.LiteralCase) any {
	var t struct {
		Value any
		Raw   []byte
		Plan  plan.CompilationPlan
	} = c

	if len(t.Raw) == 0 {
		return t.Value
	}
	dec := json.NewDecoder(bytes.NewReader(t.Raw))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		// Raw that does not parse is not a reason to guess: fall back to the Value the
		// plan says is authoritative when Raw is absent.
		return t.Value
	}
	return out
}
