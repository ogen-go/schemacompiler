package nodetree_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// names builds n property names, past the 64 a single bitmask can hold.
func names(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = prefix + strconv.Itoa(i)
	}
	return out
}

func quoteJoin(names []string) string {
	return `"` + strings.Join(names, `","`) + `"`
}

func objectWith(names []string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = `"` + n + `":1`
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// TestRequiredBeyondSixtyFourNames pins that the bitmask is an optimization, not a limit:
// a schema naming more than 64 required properties still compiles and still enforces them.
func TestRequiredBeyondSixtyFourNames(t *testing.T) {
	want := names("p", 70)
	v := compile(t, `{"type":"object","required":[`+quoteJoin(want)+`]}`)

	require.True(t, v.IsValid([]byte(objectWith(want))))
	require.False(t, v.IsValid([]byte(objectWith(want[:69]))), "the 70th name is still required")
	require.False(t, v.IsValid([]byte(objectWith(want[1:]))), "the first name is still required")
}

// TestDependentRequiredBeyondSixtyFourNames pins the same for dependentRequired, whose
// name list is the union over every entry and so overflows 64 sooner than the author's
// own reading of the schema suggests.
func TestDependentRequiredBeyondSixtyFourNames(t *testing.T) {
	req := names("r", 70)
	v := compile(t, `{"type":"object","dependentRequired":{"a":[`+quoteJoin(req)+`]}}`)

	require.True(t, v.IsValid([]byte(objectWith(req))), "the trigger is absent, so nothing is required")
	require.True(t, v.IsValid([]byte(objectWith(append([]string{"a"}, req...)))),
		"every dependent name is present")
	require.False(t, v.IsValid([]byte(objectWith(append([]string{"a"}, req[:69]...)))),
		"the 70th dependent name is missing")
}

// TestObjectStructureBeyondSixtyFourProperties pins that a wide `properties` still
// compiles and still checks the property past the 64th.
func TestObjectStructureBeyondSixtyFourProperties(t *testing.T) {
	decl := names("p", 70)
	parts := make([]string, len(decl))
	for i, n := range decl {
		parts[i] = `"` + n + `":{"type":"integer"}`
	}
	v := compile(t, `{"type":"object","properties":{`+strings.Join(parts, ",")+`}}`)

	require.True(t, v.IsValid([]byte(objectWith(decl))))
	require.False(t, v.IsValid([]byte(`{"p69":"not an integer"}`)), "the 70th property is still checked")
}

// TestRestatedRequiredIsStillEnforced pins the behavior behind withoutRestatedRequired:
// the structure keeps checking the names the standalone predicate stopped checking, and a
// non-object is still nobody's business — both are guards.
func TestRestatedRequiredIsStillEnforced(t *testing.T) {
	v := compile(t, `{"type":"object","required":["a","b"],
		"properties":{"a":{"type":"string"},"b":{"type":"integer"}}}`)

	require.True(t, v.IsValid([]byte(`{"a":"x","b":1}`)))
	require.False(t, v.IsValid([]byte(`{"a":"x"}`)), "b is required")
	require.False(t, v.IsValid([]byte(`{"b":1}`)), "a is required")
	require.False(t, v.IsValid([]byte(`{}`)))
	require.False(t, v.IsValid([]byte(`[]`)), "the sibling type assertion still rejects a non-object")
}

// TestRequiredWithoutMatchingPropertyIsEnforced pins the case the drop must not reach: no
// `properties` entry names it, so the standalone check is the only thing enforcing it.
func TestRequiredWithoutMatchingPropertyIsEnforced(t *testing.T) {
	v := compile(t, `{"type":"object","required":["a"],"properties":{"b":{"type":"string"}}}`)

	require.True(t, v.IsValid([]byte(`{"a":1}`)))
	require.False(t, v.IsValid([]byte(`{"b":"x"}`)), "a is required though undeclared")
}

// TestFoldedCountBoundsAreEnforced pins the behavior behind the count folding: the bound
// still rejects, still tolerates a kind it does not apply to, and still reports under its
// own keyword rather than the structure's.
func TestFoldedCountBoundsAreEnforced(t *testing.T) {
	for _, tt := range []struct {
		name    string
		schema  string
		accept  []string
		reject  string
		keyword string
	}{
		{
			name:    "maxItems beside items",
			schema:  `{"type":"array","items":{"type":"string"},"maxItems":2}`,
			accept:  []string{`["a","b"]`, `[]`},
			reject:  `["a","b","c"]`,
			keyword: "maxItems",
		},
		{
			name:    "minItems beside items",
			schema:  `{"type":"array","items":{"type":"string"},"minItems":2}`,
			accept:  []string{`["a","b"]`, `["a","b","c"]`},
			reject:  `["a"]`,
			keyword: "minItems",
		},
		{
			name:    "maxProperties beside properties",
			schema:  `{"type":"object","properties":{"a":{"type":"string"}},"maxProperties":2}`,
			accept:  []string{`{"a":"x","b":1}`, `{}`},
			reject:  `{"a":"x","b":1,"c":2}`,
			keyword: "maxProperties",
		},
		{
			name:    "minProperties beside properties",
			schema:  `{"type":"object","properties":{"a":{"type":"string"}},"minProperties":2}`,
			accept:  []string{`{"a":"x","b":1}`},
			reject:  `{"a":"x"}`,
			keyword: "minProperties",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			v := compile(t, tt.schema)
			for _, doc := range tt.accept {
				require.True(t, v.IsValid([]byte(doc)), doc)
			}
			require.False(t, v.IsValid([]byte(tt.reject)), tt.reject)

			errs := errorsOf(v, tt.reject)
			require.Len(t, errs, 1)
			require.Equal(t, tt.keyword, errs[0].Keyword)
			require.Equal(t, "", errs[0].Location)
			require.Error(t, v.Validate([]byte(tt.reject)))
		})
	}
}

// TestFoldedBoundIgnoresOtherKinds pins that folding kept the bound a guard: a string is
// neither an array nor an object, so neither the structure nor the bound applies.
func TestFoldedBoundIgnoresOtherKinds(t *testing.T) {
	v := compile(t, `{"items":{"type":"string"},"maxItems":1,"properties":{"a":{}},"minProperties":3}`)

	require.True(t, v.IsValid([]byte(`"a string"`)))
	require.True(t, v.IsValid([]byte(`42`)))
	require.False(t, v.IsValid([]byte(`["a","b"]`)), "the array bound still applies to an array")
	require.False(t, v.IsValid([]byte(`{"a":1}`)), "the object bound still applies to an object")
}
