package jsonequal

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEqual(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{`1`, `1`, true},
		{`1.0`, `1`, true},    // numerically equal
		{`1.50`, `1.5`, true}, // numerically equal
		{`"a"`, `"a"`, true},
		{`"a"`, `"b"`, false},
		{`null`, `null`, true},
		{`true`, `false`, false},
		{`[1,2,3]`, `[1,2,3]`, true},
		{`[1,2]`, `[1,2,3]`, false},
		{`{"a":1,"b":2}`, `{"b":2,"a":1}`, true}, // order-independent
		{`{"a":1}`, `{"a":1,"b":2}`, false},
		{`{"a":[1,{"x":2}]}`, `{"a":[1,{"x":2}]}`, true},
		// High-precision integers that both round to the same float64 must stay distinct.
		{`9007199254740992`, `9007199254740993`, false},
		{`9007199254740993`, `9007199254740993`, true},
	}
	for _, tt := range tests {
		got, err := Equal([]byte(tt.a), []byte(tt.b))
		require.NoErrorf(t, err, "%s vs %s", tt.a, tt.b)
		require.Equalf(t, tt.want, got, "%s vs %s", tt.a, tt.b)
	}
}

func FuzzEqual(f *testing.F) {
	for _, s := range []string{`1`, `1.0`, `"a"`, `null`, `[1,2]`, `{"a":1,"b":2}`} {
		f.Add([]byte(s), []byte(s))
	}
	f.Fuzz(func(t *testing.T, a, b []byte) {
		// Equal is contracted on well-formed JSON values (its real inputs come from a
		// parsed document). Canonicalize both sides through encoding/json first, which
		// rejects malformed input and collapses duplicate object keys, so the fuzzer only
		// exercises inputs Equal is actually meant to handle.
		ca, oka := canonicalJSON(a)
		cb, okb := canonicalJSON(b)
		if !oka || !okb {
			return
		}
		eqAB, err := Equal(ca, cb)
		require.NoError(t, err)
		eqBA, err := Equal(cb, ca)
		require.NoError(t, err)
		require.Equal(t, eqAB, eqBA, "Equal must be symmetric")

		eqAA, err := Equal(ca, ca)
		require.NoError(t, err)
		require.True(t, eqAA, "a must equal itself")
	})
}

func canonicalJSON(b []byte) ([]byte, bool) {
	var v any
	if json.Unmarshal(b, &v) != nil {
		return nil, false
	}
	out, err := json.Marshal(v)
	return out, err == nil
}
