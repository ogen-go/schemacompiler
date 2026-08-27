package gogen_test

import (
	"strings"
	"testing"

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

func TestValidateChecksWhatTheTypeDoesNotState(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		want   string
	}{
		{
			name:   "a string bound is a rune count, not a byte count",
			schema: `{"type":"string","minLength":2,"maxLength":4}`,
			want: "\tif utf8.RuneCountInString(string(s)) < 2 {\n" +
				"\t\treturn errors.New(\"must be at least 2 characters\")\n" +
				"\t}\n" +
				"\tif utf8.RuneCountInString(string(s)) > 4 {\n" +
				"\t\treturn errors.New(\"must be at most 4 characters\")\n" +
				"\t}",
		},
		{
			name:   "an integer bound compares against an integer",
			schema: `{"type":"integer","minimum":1,"exclusiveMaximum":10}`,
			want: "\tif s < 1 {\n" +
				"\t\treturn errors.New(\"must be at least 1\")\n" +
				"\t}\n" +
				"\tif s >= 10 {\n" +
				"\t\treturn errors.New(\"must be less than 10\")\n" +
				"\t}",
		},
		{
			name:   "integrality over a float is a truncation test",
			schema: `{"type":"number","multipleOf":0.5,"minimum":0}`,
			want: "\tif s < 0.0 {\n" +
				"\t\treturn errors.New(\"must be at least 0\")\n" +
				"\t}\n" +
				"\tif math.Mod(float64(s), 0.5) != 0 {\n" +
				"\t\treturn errors.New(\"must be a multiple of 0.5\")\n" +
				"\t}",
		},
		{
			name:   "an item bound reads the slice the items were stored in",
			schema: `{"type":"array","items":{"type":"string"},"minItems":1,"uniqueItems":true}`,
			want: "\tif len(s) < 1 {\n" +
				"\t\treturn errors.New(\"must have at least 1 item\")\n" +
				"\t}\n" +
				"\t{\n" +
				"\t\tseen := make(map[string]struct{}, len(s))\n" +
				"\t\tfor _, v := range s {\n" +
				"\t\t\tif _, dup := seen[v]; dup {\n" +
				"\t\t\t\treturn errors.New(\"must not contain duplicate items\")\n" +
				"\t\t\t}\n" +
				"\t\t\tseen[v] = struct{}{}\n" +
				"\t\t}\n" +
				"\t}",
		},
		{
			name:   "a check on a property reaches through the presence the field stores",
			schema: `{"type":"object","properties":{"a":{"type":"string","minLength":1}}}`,
			want: "\tif v0, ok := s.A.Get(); ok {\n" +
				"\t\tif utf8.RuneCountInString(string(v0)) < 1 {\n" +
				"\t\t\treturn errors.New(\".a: must be at least 1 character\")\n" +
				"\t\t}\n" +
				"\t}",
		},
		{
			name:   "an index only known at run time makes the path a format string",
			schema: `{"type":"array","items":{"type":"string","maxLength":2}}`,
			want: "\tfor i0, v0 := range s {\n" +
				"\t\tif utf8.RuneCountInString(string(v0)) > 2 {\n" +
				"\t\t\treturn fmt.Errorf(\"[%d]: must be at most 2 characters\", i0)\n" +
				"\t\t}\n" +
				"\t}",
		},
		{
			name:   "a property required but stored optional is checked, not assumed",
			schema: `{"type":"object","properties":{"a":{"type":"string"},"b":{"type":"string"}},"dependentRequired":{"a":["b"]}}`,
			want: "\tif s.A.IsSet() {\n" +
				"\t\tif !s.B.IsSet() {\n" +
				"\t\t\treturn errors.New(\"property \\\"b\\\" is required when \\\"a\\\" is present\")\n" +
				"\t\t}\n" +
				"\t}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := validated(t, `{"$defs":{"T":`+tt.schema+`},"$ref":"#/$defs/T"}`)
			require.Equal(t, tt.want, body(t, src, "T"))
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
