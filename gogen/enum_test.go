package gogen_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/gogen"
	"github.com/ogen-go/schemacompiler/internal/gotypecheck"
	"github.com/ogen-go/schemacompiler/plan"
)

// TestLowerEnumIsNotAny is the hole this closes. `enum` and `const` reach a plan as a
// [plan.LiteralDispatch] and carry no predicate at all, so a backend reading only the
// representation lowered `{"type":"string","enum":[...]}` to an interface over N copies of
// `string` — which is `any` — and one reading only the validation plan enforced nothing.
func TestLowerEnumIsNotAny(t *testing.T) {
	for _, tt := range []struct {
		name   string
		def    string
		want   string
		values []string
	}{
		{"string enum", `{"type":"string","enum":["a","b"]}`, "string", []string{"a", "b"}},
		{"integer enum", `{"type":"integer","enum":[1,2]}`, "int64", []string{"1", "2"}},
		{"number enum", `{"type":"number","enum":[1.5]}`, "float64", []string{"1.5"}},
		{"boolean enum", `{"type":"boolean","enum":[true]}`, "bool", []string{"true"}},
		{"const", `{"const":"only"}`, "string", []string{"only"}},
		{"single-kind enum with no type", `{"enum":["a","b"]}`, "string", []string{"a", "b"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			types := lower(t, fmt.Sprintf(`{"$defs":{"T":%s},"$ref":"#/$defs/T"}`, tt.def))
			e, ok := types["T"].Underlying.(*gogen.Enum)
			require.Truef(t, ok, "got %T", types["T"].Underlying)
			require.Equal(t, tt.want, shape(t, e.Elem))
			require.Len(t, e.Values, len(tt.values))
			for i, want := range tt.values {
				require.Equal(t, want, fmt.Sprint(e.Values[i].Value))
			}
		})
	}
}

// TestLowerDedupesUnionVariants is why the enum stopped being `any`: a same-kinded enum is
// a union with one alternative per literal, and every alternative lowers to the same shape.
func TestLowerDedupesUnionVariants(t *testing.T) {
	types := lower(t, `{"$defs":{"T":{"type":"string","enum":["a","b","c"]}},"$ref":"#/$defs/T"}`)
	require.Equal(t, "string", shape(t, types["T"].Underlying.(*gogen.Enum).Elem))

	// Genuinely different alternatives still make a sum.
	types = lower(t, `{"$defs":{"T":{"type":["string","number"]}},"$ref":"#/$defs/T"}`)
	sum, ok := types["T"].Underlying.(*gogen.Interface)
	require.Truef(t, ok, "got %T", types["T"].Underlying)
	require.Len(t, sum.Variants, 2)
}

func TestRenderEnumConstants(t *testing.T) {
	src := render(t, `{"$defs":{"Status":{"type":"string","enum":["active","in-progress"]}},"$ref":"#/$defs/Status"}`)
	require.Contains(t, src, "type Status string")
	require.Contains(t, src, "// The values Status admits.")
	require.Contains(t, src, `StatusActive     Status = "active"`)
	require.Contains(t, src, `StatusInProgress Status = "in-progress"`)
}

// TestRenderEnumRefusesAmbiguousNames keeps a constant set all-or-nothing. A partial one
// reads as the whole admitted set while being a subset of it, and two literals that spell
// one identifier are a refusal for the reason a colliding type name is (§1).
func TestRenderEnumRefusesAmbiguousNames(t *testing.T) {
	for _, tt := range []struct{ name, def string }{
		// `1` and `-1` both camel-case to `1`.
		{"a sign that does not survive", `{"type":"integer","enum":[1,-1]}`},
		// `%%%` contributes no word at all.
		{"a literal that is all punctuation", `{"type":"string","enum":["%%%","ok"]}`},
		// Past int64, so the constant would not compile.
		{"a value the element type cannot hold", `{"type":"integer","enum":[12345678901234567890123]}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			src := render(t, fmt.Sprintf(`{"$defs":{"T":%s},"$ref":"#/$defs/T"}`, tt.def))
			require.Contains(t, src, "no distinct Go constant name")
			require.NotContains(t, src, "const (")
		})
	}
}

// TestRenderNestedEnumHasNoConstants records the limit: an enum nested in a field has no
// declaration to hang constants off. It keeps its values in the IR either way, so what is
// lost is the spelling and not the constraint.
func TestRenderNestedEnumHasNoConstants(t *testing.T) {
	src := render(t, `{"$defs":{"T":{"type":"object","properties":{"s":{"type":"string","enum":["x"]}},"required":["s"]}},"$ref":"#/$defs/T"}`)
	require.Contains(t, src, "S string `json:\"s\"`")
	require.NotContains(t, src, "const (")

	types := lower(t, `{"$defs":{"T":{"type":"object","properties":{"s":{"type":"string","enum":["x"]}},"required":["s"]}},"$ref":"#/$defs/T"}`)
	field := types["T"].Underlying.(*gogen.Struct).Fields[0]
	_, ok := field.Type.(*gogen.Enum)
	require.True(t, ok, "the field keeps its enum, got %T", field.Type)
}

func TestRenderEnumCompiles(t *testing.T) {
	types, err := gogen.Lower(definitions(t, `{"$defs":{
		"S":{"type":"string","enum":["a","b"]},
		"I":{"type":"integer","enum":[1,2]},
		"F":{"type":"number","enum":[1.5]},
		"B":{"type":"boolean","enum":[true,false]}
	},"type":"object","properties":{
		"s":{"$ref":"#/$defs/S"},"i":{"$ref":"#/$defs/I"},
		"f":{"$ref":"#/$defs/F"},"b":{"$ref":"#/$defs/B"}}}`))
	require.NoError(t, err)
	files, err := gogen.Render(types, gogen.Options{})
	require.NoError(t, err)
	require.NoError(t, gotypecheck.Check(files, "../opt"), "rendered:\n%s", files[0].Content)
	require.Equal(t, 4, strings.Count(string(files[0].Content), "const ("))
}

// TestRenderStructuredEnumIsEnforced covers the case with nothing to name. An enum entry is
// a JSON literal inside an array, not a schema, so it has nowhere to carry an `x-go-name`
// and no constant can be derived from it. The value is handed back as what the schema said
// it was, and the admitted set is still checked.
func TestRenderStructuredEnumIsEnforced(t *testing.T) {
	for _, tt := range []struct{ name, def, want string }{
		{"object", `{"enum":[{"a":1},{"b":2}]}`, "type T map[string]any"},
		{"array", `{"enum":[["x"],["y"]]}`, "type T []any"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			src := renderCodec(t, fmt.Sprintf(`{"$defs":{"T":%s},"$ref":"#/$defs/T"}`, tt.def))
			require.Contains(t, src, tt.want)
			require.Contains(t, src, "no distinct Go constant names")
			require.Contains(t, src, "is not an admitted T")
			require.NotContains(t, src, "const (")
			// No package state: the admitted set is a switch over string cases.
			require.NotContains(t, src, "\nvar ")
		})
	}
}

// TestRenderRefusesAmbiguousCanonicalForms keeps the check able to tell two admitted values
// apart. float64 collapses two integers past its precision onto one canonical form, and a
// check that cannot distinguish them is not one to generate.
func TestRenderRefusesAmbiguousCanonicalForms(t *testing.T) {
	src := renderCodec(t,
		`{"$defs":{"T":{"enum":[[10000000000000000000000001],[10000000000000000000000002]]}},"$ref":"#/$defs/T"}`)
	require.Contains(t, src, "share one canonical form")
	require.NotContains(t, src, "is not an admitted T")
}

// TestRenderEnumEnforcesEvenWhenUnnameable separates the two failures. A literal that no
// identifier can spell costs the constants; it does not cost the check, which compares
// values and never names.
func TestRenderEnumEnforcesEvenWhenUnnameable(t *testing.T) {
	src := renderCodec(t, `{"$defs":{"T":{"type":"string","enum":["%%%","ok"]}},"$ref":"#/$defs/T"}`)
	require.NotContains(t, src, "const (")
	require.Contains(t, src, `case "%%%", "ok":`)
}

// TestObjectEnumBesideATypeIsNotLowered records a gap rather than a behavior. Adding
// `type: object` to an object enum makes the planner choose [plan.PredicateCountDispatch]
// at [plan.RawEvaluation] instead of a literal dispatch — the branches are trial-validated,
// not compared — and that variant is not lowered, so nothing generated enforces it.
//
// The three selection dispatches are all in this position (issue #155). What makes it worth
// pinning is that `Checks` cannot see it either: dispatch is not validation, so a backend
// reads zero delegated checks and concludes nothing is missing.
func TestObjectEnumBesideATypeIsNotLowered(t *testing.T) {
	defs := definitions(t, `{"$defs":{"T":{"type":"object",
		"properties":{"a":{"type":"integer"}},"required":["a"],"additionalProperties":false,
		"enum":[{"a":1},{"a":2}]}},"$ref":"#/$defs/T"}`)
	require.IsType(t, &plan.PredicateCountDispatch{}, defs["/$defs/T"].Dispatch)

	types, err := gogen.Lower(defs)
	require.NoError(t, err)
	require.IsType(t, &gogen.Struct{}, types[0].Underlying, "no Enum, so no admitted-value check")
	require.True(t, types[0].Checks.Empty(), "and nothing in Checks says so either")
}

func renderCodec(t *testing.T, schema string) string {
	t.Helper()
	types, err := gogen.Lower(definitions(t, schema))
	require.NoError(t, err)
	files, err := gogen.Render(types, gogen.Options{Codec: true})
	require.NoError(t, err)
	require.NoError(t, gotypecheck.Check(files, "../opt"), "rendered:\n%s", files[0].Content)
	return string(files[0].Content)
}
