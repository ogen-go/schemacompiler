package schemacompiler_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/internal/planwalk"
	"github.com/ogen-go/schemacompiler/plan"
)

// requireNoNever asserts no NeverRepresentation survives anywhere in p: a dead branch
// must be dropped by normalization, not lowered into a hollow alternative or dispatch
// case a backend would have to generate something unreachable for (design §15.5).
func requireNoNever(t *testing.T, p plan.CompilationPlan) {
	t.Helper()
	planwalk.Plan(p, func(r plan.Representation) {
		require.NotEqual(t, plan.Representation(plan.NeverRepresentation{}), r, "hollow Never left in the plan")
	})
}

func fieldOf(t *testing.T, r plan.Representation, name string) plan.FieldRepresentation {
	t.Helper()
	obj, ok := r.(plan.ObjectRepresentation)
	require.True(t, ok, "expected an ObjectRepresentation, got %s", repShape(r))
	return mustField(t, obj, name)
}

// TestNullableUnionField pins that stripping null out of a property's representation
// (design §7.1) drops the null alternative instead of leaving a hollow Never behind, and
// that a union left with one alternative collapses to it (issue #50).
func TestNullableUnionField(t *testing.T) {
	for _, tt := range []struct {
		name     string
		schema   string
		rep      string
		presence plan.PresenceMode
		nullable bool
	}{
		{
			name:   "anyOf with null collapses",
			schema: `{"type":"object","properties":{"a":{"anyOf":[{"type":"string"},{"type":"null"}]}}}`,
			rep:    "primitive:string", presence: plan.PresenceOptional, nullable: true,
		},
		{
			name:   "oneOf with null collapses",
			schema: `{"type":"object","properties":{"a":{"oneOf":[{"type":"string"},{"type":"null"}]}}}`,
			rep:    "primitive:string", presence: plan.PresenceOptional, nullable: true,
		},
		{
			name:   "anyOf with null keeps the remaining alternatives",
			schema: `{"type":"object","properties":{"a":{"anyOf":[{"type":"string"},{"type":"integer"},{"type":"null"}]}}}`,
			rep:    "union(primitive:string,primitive:number)", presence: plan.PresenceOptional, nullable: true,
		},
		{
			name:   "type array spelling agrees",
			schema: `{"type":"object","properties":{"a":{"type":["string","null"]}}}`,
			rep:    "primitive:string", presence: plan.PresenceOptional, nullable: true,
		},
		{
			name:   "required property",
			schema: `{"type":"object","required":["a"],"properties":{"a":{"anyOf":[{"type":"string"},{"type":"null"}]}}}`,
			rep:    "primitive:string", presence: plan.PresenceRequired, nullable: true,
		},
		{
			name:   "null-only property keeps its null representation",
			schema: `{"type":"object","properties":{"a":{"type":"null"}}}`,
			rep:    "primitive:null", presence: plan.PresenceOptional, nullable: true,
		},
		{
			name:   "non-nullable union is untouched",
			schema: `{"type":"object","properties":{"a":{"anyOf":[{"type":"string"},{"type":"integer"}]}}}`,
			rep:    "union(primitive:string,primitive:number)", presence: plan.PresenceOptional,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res, err := schemacompiler.Compile(context.Background(), []byte(tt.schema), schemacompiler.Options{})
			require.NoError(t, err)
			f := fieldOf(t, res.Plan.Representation, "a")
			require.Equal(t, tt.rep, repShape(f.Plan.Representation), "field representation")
			require.Equal(t, tt.presence, f.Presence, "presence")
			require.Equal(t, tt.nullable, f.Nullable, "nullable")
			requireNoNever(t, res.Plan)
		})
	}
}

// TestNullableUnionRootAndItems pins the positions where nullability stays in the
// representation rather than being lifted out: a root schema and an array element have no
// FieldRepresentation to carry it, so the null alternative must survive there (design §7.1).
func TestNullableUnionRootAndItems(t *testing.T) {
	for _, tt := range []struct {
		name   string
		schema string
		rep    string
	}{
		{
			name:   "root union keeps null",
			schema: `{"anyOf":[{"type":"string"},{"type":"null"}]}`,
			rep:    "union(primitive:string,primitive:null)",
		},
		{
			name:   "root type array keeps null",
			schema: `{"type":["string","null"]}`,
			rep:    "union(primitive:null,primitive:string)",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res, err := schemacompiler.Compile(context.Background(), []byte(tt.schema), schemacompiler.Options{})
			require.NoError(t, err)
			require.Equal(t, tt.rep, repShape(res.Plan.Representation))
			requireNoNever(t, res.Plan)
		})
	}

	res, err := schemacompiler.Compile(context.Background(),
		[]byte(`{"type":"array","items":{"anyOf":[{"type":"string"},{"type":"null"}]}}`), schemacompiler.Options{})
	require.NoError(t, err)
	arr, ok := res.Plan.Representation.(plan.ArrayRepresentation)
	require.True(t, ok)
	require.Equal(t, "union(primitive:string,primitive:null)", repShape(arr.Rest.Plan.Representation))
	requireNoNever(t, res.Plan)
}

// TestDeadEnumMemberDropped pins that an enum member excluded by a sibling `type` leaves
// no trace in the plan — no Never alternative, no unreachable dispatch case (issue #59),
// and that the surviving schema compiles identically to the `const` spelling of it.
func TestDeadEnumMemberDropped(t *testing.T) {
	dead, err := schemacompiler.Compile(context.Background(),
		[]byte(`{"type":"string","enum":["a",1]}`), schemacompiler.Options{})
	require.NoError(t, err)
	requireNoNever(t, dead.Plan)

	same, err := schemacompiler.Compile(context.Background(),
		[]byte(`{"type":"string","const":"a"}`), schemacompiler.Options{})
	require.NoError(t, err)
	require.Equal(t, same.Plan, dead.Plan)
	require.Equal(t, same.Capability, dead.Capability)
	require.Equal(t, same.Diagnostics, dead.Diagnostics)
}
