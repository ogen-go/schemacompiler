package schemacompiler_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/plan"
)

// uninhabitedReport returns the contradiction reported at pointer. The second result
// separates "nothing was reported" from "reported with an empty reason", which are the same
// string and must not be the same answer.
func uninhabitedReport(t *testing.T, schema, pointer string) (string, bool) {
	t.Helper()

	res, err := schemacompiler.Compile(context.Background(), []byte(schema), schemacompiler.Options{})
	require.NoError(t, err)
	for _, d := range res.Diagnostics {
		if d.Pointer != pointer || !strings.HasPrefix(d.Message, uninhabitedPrefix) {
			continue
		}
		require.Equal(t, plan.SeverityWarning, d.Severity)
		require.Equal(t, plan.DiagnosticAdvisory, d.Kind)
		return strings.TrimPrefix(d.Message, uninhabitedPrefix), true
	}
	return "", false
}

const uninhabitedPrefix = "uninhabited schema: "

// TestCompileUninhabitedIsReported pins issue #39. Normalization proves these empty and
// hands the backend a type nothing can be stored in; before this it did so in silence,
// which is indistinguishable from a schema that simply has no constraints.
//
// The message has to name the disagreement, since "this accepts nothing" is not actionable
// on its own — `{"allOf":[{"type":"object"},{"type":"null"}]}` is the natural-but-wrong way
// to make a type nullable, and the fix is only obvious once the two types are named.
func TestCompileUninhabitedIsReported(t *testing.T) {
	for _, tt := range []struct {
		name   string
		schema string
		want   string
	}{
		{
			name:   "disjoint declared types",
			schema: `{"allOf":[{"type":"object"},{"type":"null"}]}`,
			want:   "the constraints accept disjoint kinds (object, null)",
		},
		{
			name:   "const the sibling type excludes",
			schema: `{"type":"string","const":1}`,
			want:   "the constraints accept disjoint kinds (string, number)",
		},
		{
			name:   "every alternative excluded by a sibling",
			schema: `{"type":"string","enum":[1,2]}`,
			want: "no alternative in the union is satisfiable: " +
				"the constraints accept disjoint kinds (string, number)",
		},
		{
			name:   "a union with no alternatives",
			schema: `{"enum":[]}`,
			want:   "no alternative in the union is satisfiable",
		},
		{
			name:   "oneOf whose alternatives coincide",
			schema: `{"oneOf":[{"const":1},{"const":1.0}]}`,
			want:   "`oneOf` alternatives all accept the same values, so no instance matches exactly one",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, found := uninhabitedReport(t, tt.schema, "")
			require.True(t, found, "the schema must be reported uninhabited")
			require.Equal(t, tt.want, got)
		})
	}
}

// TestCompileUninhabitedIsQuietWhenAsked pins the other half, which is what stops the
// diagnostic being noise. An author who spells a schema empty means it; and §15.5 pruning a
// dead branch out of a live union is normalization working, not a mistake to report.
func TestCompileUninhabitedIsQuietWhenAsked(t *testing.T) {
	for _, tt := range []struct {
		name   string
		schema string
	}{
		{name: "boolean false", schema: `false`},
		{name: "the empty schema negated", schema: `{"not":{}}`},
		{
			name:   "false in an applicator position",
			schema: `{"type":"array","contains":false}`,
		},
		{
			name:   "a closed object",
			schema: `{"type":"object","additionalProperties":false}`,
		},
		{
			name:   "allOf with a deliberately empty member",
			schema: `{"allOf":[{"type":"string"},false]}`,
		},
		{
			name:   "a dead branch beside a live one",
			schema: `{"anyOf":[{"allOf":[{"type":"object"},{"type":"null"}]},{"type":"string"}]}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, found := uninhabitedReport(t, tt.schema, "")
			require.False(t, found, "nothing should be reported here")
		})
	}
}

// TestCompileUninhabitedNamesWhereItIs pins that the report points at the schema the author
// wrote, not at the root: an optional property that can never be present leaves the object
// itself perfectly inhabitable, so the root is the wrong place to say so.
func TestCompileUninhabitedNamesWhereItIs(t *testing.T) {
	const schema = `{"type":"object","properties":{"a":{"allOf":[{"type":"string"},{"type":"number"}]}}}`

	_, atRoot := uninhabitedReport(t, schema, "")
	require.False(t, atRoot, "the object itself is inhabitable")

	got, found := uninhabitedReport(t, schema, "/properties/a")
	require.True(t, found)
	require.Equal(t, "the constraints accept disjoint kinds (string, number)", got)
}
