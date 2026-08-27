// Package gentest compiles and runs generated code.
//
// Every other check of the backend reads what it produced. This one builds it: types.go is
// generated from schema.json and committed, so `go build ./...` compiles it and the tests
// below decode and encode with it. A codec that type-checks and does the wrong thing is
// exactly what the type checker cannot see.
package gentest

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/gogen"
	"github.com/ogen-go/schemacompiler/opt"
	"github.com/ogen-go/schemacompiler/plan"
)

// generated renders schema.json the way the committed file was rendered.
func generated(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("schema.json")
	require.NoError(t, err)
	res, err := schemacompiler.Compile(context.Background(), data, schemacompiler.Options{})
	require.NoError(t, err)
	graph, ok := res.Plan.Resolution.(*plan.StaticReferenceGraph)
	require.True(t, ok)
	types, err := gogen.Lower(graph.Definitions)
	require.NoError(t, err)
	files, err := gogen.Render(types, gogen.Options{Package: "gentest", Codec: true})
	require.NoError(t, err)
	require.Len(t, files, 1)
	return files[0].Content
}

// TestGeneratedFileIsCurrent fails when types.go stops matching what the backend produces,
// so the tests below cannot quietly go on exercising an older codec than the one shipping.
//
// Line endings are normalized before comparing. .gitattributes pins these files to LF, but
// the test should not depend on the checkout it is run from: a Windows clone with
// core.autocrlf on would otherwise fail here over bytes no compiler cares about.
func TestGeneratedFileIsCurrent(t *testing.T) {
	want, err := os.ReadFile("types.go")
	require.NoError(t, err)
	require.Equal(t, lf(want), lf(generated(t)),
		"types.go is stale; regenerate it with `go test ./internal/gentest -update`")
}

func lf(b []byte) string { return strings.ReplaceAll(string(b), "\r\n", "\n") }

func TestDecodeRejectsAMissingRequiredProperty(t *testing.T) {
	var p Pet
	err := json.Unmarshal([]byte(`{"nickname":null}`), &p)
	require.ErrorContains(t, err, `missing required property "name"`)
}

// TestDecodeEnforcesTheEnum is what the const block cannot do: Go constants restrict
// nothing, so the admitted values only hold if decoding checks them.
func TestDecodeEnforcesTheEnum(t *testing.T) {
	var s Status
	require.NoError(t, json.Unmarshal([]byte(`"active"`), &s))
	require.Equal(t, StatusActive, s)
	require.ErrorContains(t, json.Unmarshal([]byte(`"nope"`), &s), "not a valid Status")
}

func TestDecodeRejectsAnUnknownPropertyOnAClosedObject(t *testing.T) {
	var tag Tag
	require.NoError(t, json.Unmarshal([]byte(`{"name":"x"}`), &tag))
	require.ErrorContains(t, json.Unmarshal([]byte(`{"name":"x","extra":1}`), &tag), `unexpected property "extra"`)
}

// TestThreeStatePresenceRoundTrips is the distinction the whole opt package exists for:
// absent, present-null and present-value are three outcomes, and encoding has to put each
// back where it came from.
func TestThreeStatePresenceRoundTrips(t *testing.T) {
	for _, src := range []string{
		`{"name":"rex","nickname":null}`,
		`{"name":"rex","nickname":"rexy"}`,
		`{"age":3,"name":"rex","nickname":null}`,
		`{"name":"rex","nickname":null,"status":"archived"}`,
		`{"name":"rex","nickname":null,"tags":[{"name":"good"}]}`,
		`{"name":"rex","nickname":null,"unknown":{"a":1}}`,
	} {
		t.Run(src, func(t *testing.T) {
			var p Pet
			require.NoError(t, json.Unmarshal([]byte(src), &p))
			out, err := json.Marshal(p)
			require.NoError(t, err)
			require.JSONEq(t, src, string(out))
		})
	}
}

// TestNullIsNotAbsence is the ogen-like invariant. `age` is optional and not nullable, so
// an explicit null is a value the schema rejects. Taking it as absence would accept it and
// lose the difference on the way back out.
func TestNullIsNotAbsence(t *testing.T) {
	var p Pet
	err := json.Unmarshal([]byte(`{"name":"a","nickname":null,"age":null}`), &p)
	require.ErrorContains(t, err, `decode "age"`)
	require.ErrorContains(t, err, "not an admitted value")

	// `nickname` is required and nullable, so its null is admitted and survives a round
	// trip as a written key.
	require.NoError(t, json.Unmarshal([]byte(`{"name":"a","nickname":null}`), &p))
	require.True(t, p.Nickname.IsNull())
	out, err := json.Marshal(p)
	require.NoError(t, err)
	require.Contains(t, string(out), `"nickname":null`)
	require.NotContains(t, string(out), `"age"`)
}

// TestOmitzeroCarriesPresence pins that the struct tags do the work encoding/json can do,
// so the generator writes a marshaller only for what it cannot: flattening the overflow
// map into the same object. `Tag` is closed, so it has no MarshalJSON at all.
func TestOmitzeroCarriesPresence(t *testing.T) {
	out, err := json.Marshal(Tag{Name: "x"})
	require.NoError(t, err)
	require.JSONEq(t, `{"name":"x"}`, string(out), "an absent score is omitted by the tag alone")

	out, err = json.Marshal(Tag{Name: "x", Score: opt.Some(1.5)})
	require.NoError(t, err)
	require.JSONEq(t, `{"name":"x","score":1.5}`, string(out))
}

// TestEncodingIsDeterministic pins the sorted overflow walk: Go randomizes map iteration,
// so an unsorted encoder would produce different bytes for the same value.
func TestEncodingIsDeterministic(t *testing.T) {
	var p Pet
	require.NoError(t, json.Unmarshal([]byte(`{"name":"a","nickname":null,"x":1,"y":2,"z":3}`), &p))
	first, err := json.Marshal(p)
	require.NoError(t, err)
	for range 32 {
		again, err := json.Marshal(p)
		require.NoError(t, err)
		require.Equal(t, string(first), string(again))
	}
}

func TestRecursiveTypeRoundTrips(t *testing.T) {
	const src = `{"name":"child","nickname":null,"parent":{"name":"parent","nickname":null}}`
	var p Pet
	require.NoError(t, json.Unmarshal([]byte(src), &p))
	parent, ok := p.Parent.Get()
	require.True(t, ok)
	require.Equal(t, "parent", parent.Name)

	out, err := json.Marshal(p)
	require.NoError(t, err)
	require.JSONEq(t, src, string(out))
}

// TestStructuredEnumIsEnforced is the case with nothing to name. An enum entry is a JSON
// literal inside an array, not a schema, so it has nowhere to carry an `x-go-name` and no
// constant can be derived from it. The value is handed back as what the schema said it was,
// and the admitted set is still checked.
func TestStructuredEnumIsEnforced(t *testing.T) {
	var s Shape
	require.NoError(t, json.Unmarshal([]byte(`{"kind":"circle","r":1}`), &s))
	require.Equal(t, "circle", s["kind"])

	// Key order and number spelling are not what JSON equality is about.
	require.NoError(t, json.Unmarshal([]byte(`{"r":1.0,"kind":"circle"}`), &s))
	require.Equal(t, "circle", s["kind"])

	require.ErrorContains(t, json.Unmarshal([]byte(`{"kind":"circle","r":2}`), &s), "is not an admitted Shape")
	require.ErrorContains(t, json.Unmarshal([]byte(`{"kind":"triangle"}`), &s), "is not an admitted Shape")
}

func TestStructuredEnumRoundTrips(t *testing.T) {
	const src = `{"name":"a","nickname":null,"shape":{"kind":"square","side":2}}`
	var p Pet
	require.NoError(t, json.Unmarshal([]byte(src), &p))
	out, err := json.Marshal(p)
	require.NoError(t, err)
	require.JSONEq(t, src, string(out))
}
