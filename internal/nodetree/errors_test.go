package nodetree_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/internal/nodetree"
)

func compile(t *testing.T, schema string) *nodetree.Validator {
	t.Helper()
	res, err := schemacompiler.Compile(context.Background(), []byte(schema), schemacompiler.Options{})
	require.NoError(t, err)
	v, err := nodetree.Compile(res.Plan)
	require.NoError(t, err)
	return v
}

func errorsOf(v *nodetree.Validator, doc string) []nodetree.Error {
	var out []nodetree.Error
	for e := range v.IterErrors([]byte(doc)) {
		out = append(out, e)
	}
	return out
}

// TestErrorLocations pins the JSON Pointer a failure is reported at, including the two
// escapes RFC 6901 defines, which a naive path built by string concatenation gets wrong.
func TestErrorLocations(t *testing.T) {
	for _, tt := range []struct {
		name     string
		schema   string
		doc      string
		location string
		keyword  string
	}{
		{
			name:     "root",
			schema:   `{"type":"object"}`,
			doc:      `[]`,
			location: "",
			keyword:  "type",
		},
		{
			name:     "property",
			schema:   `{"type":"object","properties":{"a":{"type":"string"}}}`,
			doc:      `{"a":1}`,
			location: "/a",
			keyword:  "type",
		},
		{
			name:     "nested property",
			schema:   `{"properties":{"a":{"properties":{"b":{"type":"string"}}}}}`,
			doc:      `{"a":{"b":1}}`,
			location: "/a/b",
			keyword:  "type",
		},
		{
			name:     "array element",
			schema:   `{"type":"array","items":{"type":"string"}}`,
			doc:      `["ok",1,"ok"]`,
			location: "/1",
			keyword:  "type",
		},
		{
			name:     "property containing a slash",
			schema:   `{"properties":{"a/b":{"type":"string"}}}`,
			doc:      `{"a/b":1}`,
			location: "/a~1b",
			keyword:  "type",
		},
		{
			name:     "property containing a tilde",
			schema:   `{"properties":{"a~b":{"type":"string"}}}`,
			doc:      `{"a~b":1}`,
			location: "/a~0b",
			keyword:  "type",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := errorsOf(compile(t, tt.schema), tt.doc)
			require.Len(t, got, 1, "%+v", got)
			require.Equal(t, tt.location, got[0].Location)
			require.Equal(t, tt.keyword, got[0].Keyword)
		})
	}
}

// TestIterErrorsReportsEveryReason keeps the reporting walk from stopping where the fast
// path would: IsValid short-circuits on the first failure, this must not.
func TestIterErrorsReportsEveryReason(t *testing.T) {
	v := compile(t, `{"type":"object","properties":{
		"a":{"type":"string"},"b":{"type":"number"},"c":{"type":"boolean"}}}`)

	got := errorsOf(v, `{"a":1,"b":"x","c":null}`)
	require.Len(t, got, 3)
	require.Equal(t, []string{"/a", "/b", "/c"},
		[]string{got[0].Location, got[1].Location, got[2].Location})
}

// TestIterErrorsStopsWhenTheConsumerDoes pins the iter.Seq contract: a break must not
// leave the walk running.
func TestIterErrorsStopsWhenTheConsumerDoes(t *testing.T) {
	v := compile(t, `{"type":"object","properties":{
		"a":{"type":"string"},"b":{"type":"number"},"c":{"type":"boolean"}}}`)

	n := 0
	for range v.IterErrors([]byte(`{"a":1,"b":"x","c":null}`)) {
		n++
		break
	}
	require.Equal(t, 1, n)
}

// TestRestatedConstraintIsReportedOnce pins the deduplication. `required` reaches two
// nodes because design §4.1 has the object structure predicate restate what the
// representation stores; the fast path short-circuits and never notices.
func TestRestatedConstraintIsReportedOnce(t *testing.T) {
	v := compile(t, `{"type":"object","required":["id"],"properties":{"id":{"type":"integer"}}}`)

	got := errorsOf(v, `{}`)
	require.Len(t, got, 1, "%+v", got)
	require.Equal(t, "required", got[0].Keyword)
}

// TestValidateAndIsValidAgree is the invariant the two traversals owe each other. The
// suite walk checks it across every corpus instance; this pins the shapes a reader is
// most likely to break.
func TestValidateAndIsValidAgree(t *testing.T) {
	for _, tt := range []struct{ schema, doc string }{
		{`{"type":"string"}`, `"ok"`},
		{`{"type":"string"}`, `1`},
		{`{"not":{"type":"string"}}`, `1`},
		{`{"not":{"type":"string"}}`, `"no"`},
		{`{"oneOf":[{"type":"string"},{"type":"number"}]}`, `"a"`},
		{`{"oneOf":[{"type":"string"},{"type":"number"}]}`, `null`},
		{`{"type":"array","contains":{"type":"string"},"minContains":2}`, `["a","b"]`},
		{`{"type":"array","contains":{"type":"string"},"minContains":2}`, `["a",1]`},
		{`{"type":"array","uniqueItems":true}`, `[1,2]`},
		{`{"type":"array","uniqueItems":true}`, `[1,1]`},
		{`{"propertyNames":{"minLength":3}}`, `{"abc":1}`},
		{`{"propertyNames":{"minLength":3}}`, `{"ab":1}`},
		{`{"$defs":{"S":{"type":"string"}},"$ref":"#/$defs/S"}`, `"ok"`},
		{`{"$defs":{"S":{"type":"string"}},"$ref":"#/$defs/S"}`, `1`},
	} {
		t.Run(tt.schema+"/"+tt.doc, func(t *testing.T) {
			v := compile(t, tt.schema)
			valid := v.IsValid([]byte(tt.doc))
			require.Equal(t, valid, v.Validate([]byte(tt.doc)) == nil,
				"IsValid and Validate disagree")
		})
	}
}
