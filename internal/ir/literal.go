package ir

import (
	"bytes"
	"reflect"

	"github.com/ogen-go/schemacompiler/internal/jsonequal"
)

// Equal reports whether l and o denote the same JSON value under JSON Schema equality:
// numbers compare mathematically (`1` == `1.0`, exact for values beyond float64 range),
// objects ignore member order, arrays are order-sensitive, and null/boolean/string
// compare directly. Literals carrying raw source bytes compare through those, so
// precision survives; a synthesized literal without them falls back to its decoded value.
func (l Literal) Equal(o Literal) bool {
	if l.Raw != nil && o.Raw != nil {
		if bytes.Equal(l.Raw, o.Raw) {
			return true
		}
		if eq, err := jsonequal.Equal(l.Raw, o.Raw); err == nil {
			return eq
		}
	}
	return decodedEqual(l.Value, o.Value)
}

// decodedEqual compares two decoded JSON values, treating every Go numeric
// representation as one JSON number so a synthesized int literal matches a parsed
// float64 one.
func decodedEqual(a, b any) bool {
	af, aok := asFloat(a)
	bf, bok := asFloat(b)
	if aok || bok {
		return aok && bok && af == bf
	}
	return reflect.DeepEqual(a, b)
}

func asFloat(v any) (float64, bool) {
	switch v := v.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	}
	return 0, false
}
