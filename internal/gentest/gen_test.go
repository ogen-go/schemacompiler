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
	files, err := gogen.Render(types, gogen.Options{Package: "gentest", Codec: true, Validate: true})
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

// TestTupleIsAnArray is what a missing tuple codec costs: encoding/json writes a struct as
// a JSON object, so without these methods a `prefixItems` schema round-trips to
// `{"F0":1,"F1":2}` — not a wrong-looking array, a wrong kind.
func TestTupleIsAnArray(t *testing.T) {
	var p Point
	require.NoError(t, json.Unmarshal([]byte(`[1,2]`), &p))
	require.InDelta(t, 1.0, p.F0, 0)
	require.InDelta(t, 2.0, p.F1, 0)
	require.False(t, p.F2.IsSet(), "the third slot is past minItems, so it may be absent")

	out, err := json.Marshal(p)
	require.NoError(t, err)
	require.Equal(t, `[1,2]`, string(out))
}

// TestTupleSlotPresence is why the slots are not all bare. `prefixItems` applies to the
// positions an instance has, so a shorter array is admitted; a bare slot could not tell an
// absent item from a zero one, and encoding would put back an item that was never there.
func TestTupleSlotPresence(t *testing.T) {
	for _, src := range []string{`[1,2]`, `[1,2,"here"]`} {
		var p Point
		require.NoError(t, json.Unmarshal([]byte(src), &p))
		out, err := json.Marshal(p)
		require.NoError(t, err)
		require.Equal(t, src, string(out))
	}

	// The slots minItems covers are required, and the decoder is the only place that can
	// say so — the same reasoning as a required property.
	require.ErrorContains(t, json.Unmarshal([]byte(`[1]`), new(Point)), "missing item 1")

	// `items: false` closes the array, as `additionalProperties: false` closes an object.
	require.ErrorContains(t, json.Unmarshal([]byte(`[1,2,"x",4]`), new(Point)), "at most 3 items")
}

func TestTupleInsideAnObjectRoundTrips(t *testing.T) {
	const src = `{"at":[1,2],"name":"a","nickname":null}`
	var p Pet
	require.NoError(t, json.Unmarshal([]byte(src), &p))
	out, err := json.Marshal(p)
	require.NoError(t, err)
	require.JSONEq(t, src, string(out))
}

func TestValidateAcceptsAValidValue(t *testing.T) {
	var p Pet
	require.NoError(t, json.Unmarshal([]byte(`{"name":"Rex","nickname":null,"age":3,"tags":[{"name":"good"}],"at":[1,2]}`), &p))
	require.NoError(t, p.Validate())
}

func TestValidateRejectsABoundTheTypeCannotState(t *testing.T) {
	var p Pet
	require.NoError(t, json.Unmarshal([]byte(`{"name":"Rex","nickname":null,"age":-1}`), &p))
	require.EqualError(t, p.Validate(), ".age: must be at least 0")
}

// TestValidateNamesWhereItFailed is why the path is carried rather than reconstructed: the
// failing value is two levels down and nothing else identifies it.
func TestValidateNamesWhereItFailed(t *testing.T) {
	var p Pet
	require.NoError(t, json.Unmarshal([]byte(`{"name":"Rex","nickname":null,"tags":[{"name":"ok"},{"name":""}]}`), &p))
	require.EqualError(t, p.Validate(), `.tags[1]: .name: must be at least 1 character`)
}

// TestValidateFollowsARecursiveField is the fixpoint at run time: Pet holds a Pet, so the
// method has to call itself.
func TestValidateFollowsARecursiveField(t *testing.T) {
	var p Pet
	require.NoError(t, json.Unmarshal([]byte(`{"name":"Rex","nickname":null,"parent":{"name":"Max","nickname":null,"age":-2}}`), &p))
	require.EqualError(t, p.Validate(), ".parent: .age: must be at least 0")
}

// TestValidateCountsTheItemsATupleEncodes is issue #161's worked example: `minItems` is a
// bound on the array, and Point's shape states only its slots.
func TestValidateCountsTheItemsATupleEncodes(t *testing.T) {
	var p Point
	require.NoError(t, json.Unmarshal([]byte(`[1,2]`), &p))
	require.NoError(t, p.Validate())
}

// TestValidateIsNotCalledByDecoding says what generated code does not do. Decoding accepts
// what the Go shape can hold, which over-accepts in the direction design §24 permits;
// narrowing it is the caller's call to Validate.
func TestValidateIsNotCalledByDecoding(t *testing.T) {
	var g Tag
	require.NoError(t, json.Unmarshal([]byte(`{"name":"","score":99}`), &g))
	require.Error(t, g.Validate())
}

// TestNamedNullableRoundTrips is the fault the generated-code differential harness found
// first: a defined type does not inherit the methods of the type it is defined as, so
// `type Nickname opt.Nullable[string]` had no UnmarshalJSON and encoding/json saw a struct
// of unexported fields — it rejected every document, admitted ones included.
func TestNamedNullableRoundTrips(t *testing.T) {
	for _, in := range []string{`"Rex"`, `null`} {
		var n Nickname
		require.NoError(t, json.Unmarshal([]byte(in), &n))
		out, err := json.Marshal(n)
		require.NoError(t, err)
		require.JSONEq(t, in, string(out))
	}
}
