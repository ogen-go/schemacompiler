package frontend

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// countKeywordFields is every keyword 2020-12 defines as a non-negative integer, paired
// with the [Node] field it lands in.
var countKeywordFields = map[string]func(*Node) *uint64{
	"minLength":     func(n *Node) *uint64 { return n.MinLength },
	"maxLength":     func(n *Node) *uint64 { return n.MaxLength },
	"minItems":      func(n *Node) *uint64 { return n.MinItems },
	"maxItems":      func(n *Node) *uint64 { return n.MaxItems },
	"minContains":   func(n *Node) *uint64 { return n.MinContains },
	"maxContains":   func(n *Node) *uint64 { return n.MaxContains },
	"minProperties": func(n *Node) *uint64 { return n.MinProperties },
	"maxProperties": func(n *Node) *uint64 { return n.MaxProperties },
}

func TestLoad_CountKeywords(t *testing.T) {
	for _, tc := range []struct {
		name     string
		spelling string
		want     uint64
	}{
		{"integer", `2`, 2},
		{"zero", `0`, 0},
		{"decimal", `2.0`, 2},
		{"exponent", `1e2`, 100},
		{"decimal exponent", `2.0e1`, 20},
		{"large", `4294967296`, 4294967296},
	} {
		for keyword, field := range countKeywordFields {
			t.Run(keyword+"/"+tc.name, func(t *testing.T) {
				s := mustLoad(t, `{"`+keyword+`": `+tc.spelling+`}`)
				require.Empty(t, s.InvalidKeyword)
				got := field(s.Root)
				require.NotNil(t, got, "keyword must not be dropped")
				require.Equal(t, tc.want, *got)
			})
		}
	}
}

// TestLoad_CountKeywordsInvalid pins the answer for a value that is no non-negative
// integer: the keyword is left absent, never synthesized as 0, and the schema's invalidity
// is reported rather than swallowed.
func TestLoad_CountKeywordsInvalid(t *testing.T) {
	for _, tc := range []struct {
		name     string
		spelling string
	}{
		{"fractional", `2.5`},
		{"negative", `-1`},
		{"negative decimal", `-1.0`},
		{"string", `"2"`},
		{"boolean", `true`},
		{"null", `null`},
		{"array", `[2]`},
		{"object", `{"n": 2}`},
		{"above uint64", `18446744073709551616`},
	} {
		for keyword, field := range countKeywordFields {
			t.Run(keyword+"/"+tc.name, func(t *testing.T) {
				s := mustLoad(t, `{"`+keyword+`": `+tc.spelling+`}`)
				require.Nil(t, field(s.Root))
				require.Len(t, s.InvalidKeyword, 1)
				got := s.InvalidKeyword[0]
				require.Equal(t, keyword, got.Keyword)
				require.Equal(t, "/"+keyword, got.Pointer)
				require.Equal(t, "expected a non-negative integer", got.Reason)
				require.NotZero(t, got.Position.Line)
			})
		}
	}
}

func TestLoad_CountKeywordsAbsent(t *testing.T) {
	s := mustLoad(t, `{"type": "string"}`)
	require.Empty(t, s.InvalidKeyword)
	for keyword, field := range countKeywordFields {
		require.Nil(t, field(s.Root), keyword)
	}
}

func TestLoad_CountKeywordsNested(t *testing.T) {
	s := mustLoad(t, `{"properties": {"a": {"maxLength": 2.0}, "b": {"maxLength": 2.5}}}`)
	require.Len(t, s.Root.Properties, 2)

	a := s.Root.Properties[0].Schema
	require.NotNil(t, a.MaxLength)
	require.Equal(t, uint64(2), *a.MaxLength)
	require.Nil(t, s.Root.Properties[1].Schema.MaxLength)

	require.Len(t, s.InvalidKeyword, 1)
	require.Equal(t, "/properties/b/maxLength", s.InvalidKeyword[0].Pointer)
}

// TestLoad_CountKeywordsAlongsideRef covers the second conversion path: keywords declared
// beside a `$ref` are rebuilt from the node that declared them, not from the target.
func TestLoad_CountKeywordsAlongsideRef(t *testing.T) {
	s := mustLoad(t, `{
		"$ref": "#/$defs/A",
		"maxLength": 2.0,
		"$defs": {"A": {"type": "string"}}
	}`)
	require.Empty(t, s.InvalidKeyword)
	require.NotNil(t, s.Root.MaxLength)
	require.Equal(t, uint64(2), *s.Root.MaxLength)
}

func TestInt64PtrToUint64Ptr(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   *int64
		want *uint64
	}{
		{"nil", nil, nil},
		{"negative", ptr(int64(-1)), nil},
		{"zero", ptr(int64(0)), ptr(uint64(0))},
		{"positive", ptr(int64(7)), ptr(uint64(7))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, int64PtrToUint64Ptr(tc.in))
		})
	}
}

func ptr[T any](v T) *T { return &v }
