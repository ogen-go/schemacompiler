package gogen_test

import (
	"strings"
	"testing"

	"github.com/go-faster/sdk/gold"
	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/gogen"
	"github.com/ogen-go/schemacompiler/internal/gotypecheck"
)

func validated(t *testing.T, schema string) string {
	t.Helper()
	types, err := gogen.Lower(definitions(t, schema))
	require.NoError(t, err)
	files, err := gogen.Render(types, gogen.Options{Validate: true})
	require.NoError(t, err)
	require.NoError(t, gotypecheck.Check(files, "../opt"))
	return string(files[0].Content)
}

// body returns the Validate method of name, so a table states the checks rather than the
// whole file.
func body(t *testing.T, src, name string) string {
	t.Helper()
	head := "func (s " + name + ") Validate() error {\n"
	_, rest, ok := strings.Cut(src, head)
	if !ok {
		return ""
	}
	method, _, ok := strings.Cut(rest, "\n}\n")
	require.True(t, ok, "method is not terminated")
	end, ok := strings.CutSuffix(method, "\n\treturn nil")
	require.True(t, ok, "method does not end in a bare return")
	return end
}

// TestValidateChecksWhatTheTypeDoesNotState golden-files the whole rendered file rather
// than the method body: what a check costs in imports is part of what it costs.
func TestValidateChecksWhatTheTypeDoesNotState(t *testing.T) {
	tests := []struct {
		name   string
		schema string
	}{
		{"string_bounds", `{"type":"string","minLength":2,"maxLength":4}`},
		{"integer_bounds", `{"type":"integer","minimum":1,"exclusiveMaximum":10}`},
		{"float_domain", `{"type":"number","multipleOf":0.5,"minimum":0}`},
		{"array_bounds", `{"type":"array","items":{"type":"string"},"minItems":1,"uniqueItems":true}`},
		{"property_reached_through_presence", `{"type":"object","properties":{"a":{"type":"string","minLength":1}}}`},
		{"index_known_only_at_run_time", `{"type":"array","items":{"type":"string","maxLength":2}}`},
		{"dependent_required", `{"type":"object","properties":{"a":{"type":"string"},"b":{"type":"string"}},"dependentRequired":{"a":["b"]}}`},
		{"object_property_count", `{"type":"object","properties":{"a":{"type":"string"}},"minProperties":1,"maxProperties":3}`},
		{"map_property_count", `{"type":"object","additionalProperties":{"type":"string"},"minProperties":2}`},
		{"pattern_properties", `{"type":"object","patternProperties":{"^a":{"type":"string","minLength":2}}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gold.Str(t, validated(t, `{"$defs":{"T":`+tt.schema+`},"$ref":"#/$defs/T"}`), tt.name+".go.golden")
		})
	}
}

// TestValidateIsAbsentWhenTheTypeStatesEverything pins what design §22 makes
// [plan.DirectGoType] mean: a type that needs no validator gets no method, rather than an
// empty one that reads as a check having been made.
func TestValidateIsAbsentWhenTheTypeStatesEverything(t *testing.T) {
	src := validated(t, `{"$defs":{"T":{"type":"string"}},"$ref":"#/$defs/T"}`)
	require.NotContains(t, src, "Validate() error")
}

// TestValidateReachesThroughATypeThatNeedsOne is the fixpoint: a type whose own shape
// states everything still needs a method when it holds one that does not.
func TestValidateReachesThroughATypeThatNeedsOne(t *testing.T) {
	src := validated(t, `{
		"$defs":{
			"Outer":{"type":"object","properties":{"inner":{"$ref":"#/$defs/Inner"}},"required":["inner"]},
			"Inner":{"type":"string","minLength":1}
		},
		"$ref":"#/$defs/Outer"
	}`)
	require.Equal(t, "\tif err := s.Inner.Validate(); err != nil {\n"+
		"\t\treturn fmt.Errorf(\".inner: %w\", err)\n"+
		"\t}", body(t, src, "Outer"))
}

// TestValidateDoesNotCallAMethodThatWasNotGenerated is the other half of the fixpoint, and
// the half a compiler catches only because the type checker runs here: starting from
// "everything needs one" would keep the call and generate no method for a recursive cycle
// whose members check nothing.
func TestValidateDoesNotCallAMethodThatWasNotGenerated(t *testing.T) {
	src := validated(t, `{
		"$defs":{"Node":{"type":"object","properties":{"next":{"$ref":"#/$defs/Node"}}}},
		"$ref":"#/$defs/Node"
	}`)
	require.NotContains(t, src, "Validate() error")
}

// TestValidateCountsTheItemsATupleEncodes is issue #161's worked example. `minItems` past
// the prefix is a bound on the whole array, and the tuple's own shape states only the
// slots.
func TestValidateCountsTheItemsATupleEncodes(t *testing.T) {
	src := validated(t, `{
		"$defs":{"T":{"type":"array","prefixItems":[{"type":"string"},{"type":"boolean"}],"minItems":4}},
		"$ref":"#/$defs/T"
	}`)
	require.Equal(t, "\t{\n"+
		"\t\tcount := 0\n"+
		"\t\tcount++\n"+
		"\t\tcount++\n"+
		"\t\tcount += len(s.Rest)\n"+
		"\t\tif count < 4 {\n"+
		"\t\t\treturn errors.New(\"must have at least 4 items\")\n"+
		"\t\t}\n"+
		"\t}", body(t, src, "T"))
}

// TestUnenforcedIsReported is the invariant issue #161 is really about: what no check was
// written for is declared rather than dropped, which is the only thing design §24 permits
// over-accepting under.
func TestUnenforcedIsReported(t *testing.T) {
	types, err := gogen.Lower(definitions(t, `{"$defs":{"T":{"type":"string","format":"email"}},"$ref":"#/$defs/T"}`))
	require.NoError(t, err)
	require.Equal(t, map[string]int{"`Format` has no generated check yet": 1}, gogen.Unenforced(types))

	src := validated(t, `{"$defs":{"T":{"type":"string","format":"email"}},"$ref":"#/$defs/T"}`)
	require.Contains(t, src, "// Not enforced by the generated validators:\n//       1 `Format` has no generated check yet")
}
