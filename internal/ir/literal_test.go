package ir

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func parseLiteral(t *testing.T, s string) Literal {
	t.Helper()
	var v any
	require.NoError(t, json.Unmarshal([]byte(s), &v))
	return Literal{Value: v, Raw: []byte(s)}
}

func TestLiteralEqual(t *testing.T) {
	tests := []struct {
		name  string
		a, b  string
		equal bool
	}{
		{name: "identical strings", a: `"a"`, b: `"a"`, equal: true},
		{name: "different strings", a: `"a"`, b: `"b"`},
		{name: "integer and decimal", a: `1`, b: `1.0`, equal: true},
		{name: "integer and exponent", a: `100`, b: `1e2`, equal: true},
		{name: "signed zero", a: `0`, b: `-0.0`, equal: true},
		{name: "different numbers", a: `1`, b: `2`},
		{
			name: "integers beyond float64 precision",
			a:    `10000000000000000000000001`, b: `10000000000000000000000002`,
		},
		{
			name: "decimals beyond float64 precision",
			a:    `0.10000000000000000000000001`, b: `0.10000000000000000000000002`,
		},
		{name: "object member order", a: `{"a":1,"b":2}`, b: `{"b":2,"a":1}`, equal: true},
		{name: "object nested number notation", a: `{"a":1}`, b: `{"a":1.0}`, equal: true},
		{name: "object different members", a: `{"a":1}`, b: `{"a":2}`},
		{name: "object extra member", a: `{"a":1}`, b: `{"a":1,"b":2}`},
		{name: "array element order", a: `[1,2]`, b: `[2,1]`},
		{name: "array same order", a: `[1,2]`, b: `[1.0,2.0]`, equal: true},
		{name: "null", a: `null`, b: `null`, equal: true},
		{name: "null and false", a: `null`, b: `false`},
		{name: "booleans", a: `true`, b: `true`, equal: true},
		{name: "different booleans", a: `true`, b: `false`},
		{name: "string and number", a: `"1"`, b: `1`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, b := parseLiteral(t, tt.a), parseLiteral(t, tt.b)
			require.Equal(t, tt.equal, a.Equal(b))
			require.Equal(t, tt.equal, b.Equal(a), "symmetry")
		})
	}
}

// TestLiteralEqualSynthesized covers literals with no raw source bytes, which compare by
// decoded value: every Go numeric type stands for one JSON number there.
func TestLiteralEqualSynthesized(t *testing.T) {
	tests := []struct {
		name  string
		a, b  Literal
		equal bool
	}{
		{name: "int and float", a: Literal{Value: 1}, b: Literal{Value: 1.0}, equal: true},
		{name: "int64 and float", a: Literal{Value: int64(7)}, b: Literal{Value: 7.0}, equal: true},
		{name: "different numbers", a: Literal{Value: 1}, b: Literal{Value: 2.0}},
		{name: "number and string", a: Literal{Value: 1}, b: Literal{Value: "1"}},
		{name: "strings", a: Literal{Value: "a"}, b: Literal{Value: "a"}, equal: true},
		{name: "nulls", a: Literal{}, b: Literal{}, equal: true},
		{name: "against a raw literal", a: Literal{Value: "a"}, b: Literal{Value: "a", Raw: []byte(`"a"`)}, equal: true},
		{
			name: "objects",
			a:    Literal{Value: map[string]any{"a": 1.0}}, b: Literal{Value: map[string]any{"a": 1.0}}, equal: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.equal, tt.a.Equal(tt.b))
			require.Equal(t, tt.equal, tt.b.Equal(tt.a), "symmetry")
		})
	}
}
