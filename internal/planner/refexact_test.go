package planner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/frontend"
	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/plan"
)

func loadRegistry(t *testing.T, doc string) *frontend.Schema {
	t.Helper()

	s, err := frontend.Load(context.Background(), []byte(doc), "")
	require.NoError(t, err)
	return s
}

// TestRefTrusted_TargetRungs pins what [builder.refTrusted] answers per target, and asserts
// the rung each target actually lands on, so a row cannot pass for the wrong reason.
func TestRefTrusted_TargetRungs(t *testing.T) {
	const unsupported = `{"anyOf": [true, {"properties": {"foo": true}}], "unevaluatedProperties": false}`

	for _, tt := range []struct {
		name    string
		defs    string
		target  plan.SchemaID
		rung    plan.Exactness
		trusted bool
	}{
		{
			name:    "exact target",
			defs:    `{"S": {"type": "string"}}`,
			target:  "/$defs/S",
			rung:    plan.ExactPureRepresentation,
			trusted: true,
		},
		{
			name:    "target closed by a residual validator",
			defs:    `{"S": {"type": "string", "minLength": 3}}`,
			target:  "/$defs/S",
			rung:    plan.ExactWithValidation,
			trusted: true,
		},
		{
			name: "target with an asserted discriminator",
			defs: `{"S": {"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				"discriminator": {"propertyName": "petType",
					"mapping": {"cat": "#/$defs/Dog", "dog": "#/$defs/Cat"}}},
				"Cat": {"type": "object", "required": ["petType", "name"],
					"properties": {"petType": {"type": "string"}, "name": {"type": "string"}}},
				"Dog": {"type": "object", "required": ["petType", "bark"],
					"properties": {"petType": {"type": "string"}, "bark": {"type": "boolean"}}}}`,
			target:  "/$defs/S",
			rung:    plan.SoundOverApproximation,
			trusted: false,
		},
		{
			name:    "target with a dropped negation",
			defs:    `{"S": {"type": "object", "properties": {"a": {"not": ` + unsupported + `}}}}`,
			target:  "/$defs/S",
			rung:    plan.DeclaredIncomplete,
			trusted: false,
		},
		{
			name:    "unmodeled target",
			defs:    `{"S": ` + unsupported + `}`,
			target:  "/$defs/S",
			rung:    plan.UnsupportedConversion,
			trusted: false,
		},
		{
			name:    "guarded-recursive target",
			defs:    `{"S": {"type": "object", "properties": {"next": {"$ref": "#/$defs/S"}}}}`,
			target:  "/$defs/S",
			rung:    plan.ExactPureRepresentation,
			trusted: false,
		},
		{
			name:    "target no reference names",
			defs:    `{"S": {"type": "string"}}`,
			target:  "/$defs/Absent",
			trusted: false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := loadRegistry(t, `{"$ref": "#/$defs/S", "$defs": `+tt.defs+`}`)
			b := newBuilder(s.Registry)

			require.Equal(t, tt.trusted, b.refTrusted(tt.target, nil))

			node, ok := b.refTargets()[string(tt.target)]
			if !ok {
				return
			}
			sub := newBuilder(s.Registry)
			require.Equal(t, tt.rung, exactnessOf(sub.build(ir.Compile(node), ""), sub.gaps),
				"the row must exercise the rung it names")
		})
	}
}

// TestRefTrusted_NoRegistry pins the hand-built-fixture path: with no registry there is no
// target index, so no reference can be followed and none is trusted.
func TestRefTrusted_NoRegistry(t *testing.T) {
	b := newBuilder(nil)

	require.False(t, b.refTrusted("/$defs/S", nil))
	require.Nil(t, b.refTargets())
}

// TestRefTrusted_LeavesNoTrace pins the two hazards of building a target only to inspect it:
// its diagnostics must not be re-emitted at every reference site, and its gaps must not
// demote the exactness of the plan that refers to it (design §25).
func TestRefTrusted_LeavesNoTrace(t *testing.T) {
	s := loadRegistry(t, `{"$ref": "#/$defs/S", "$defs": {"S":
		{"anyOf": [true, {"properties": {"foo": true}}], "unevaluatedProperties": false}}}`)

	sub := newBuilder(s.Registry)
	sub.build(ir.Compile(s.Registry.RefTargets()["/$defs/S"]), "")
	require.NotEmpty(t, sub.diags, "the target must be one whose build says something")

	b := newBuilder(s.Registry)
	b.diag("/pre", plan.SeverityWarning, "pre-existing")
	before := b.gaps

	require.False(t, b.refTrusted("/$defs/S", nil))

	require.Len(t, b.diags, 1)
	require.Equal(t, "pre-existing", b.diags[0].Message)
	require.Equal(t, before, b.gaps)
}

// TestRefTrusted_Memoized pins that a target shared by many reference sites is inspected
// once per Build call.
func TestRefTrusted_Memoized(t *testing.T) {
	s := loadRegistry(t, `{"$ref": "#/$defs/S", "$defs": {"S": {"type": "string"}}}`)
	b := newBuilder(s.Registry)

	require.True(t, b.refTrusted("/$defs/S", nil))
	require.Equal(t, map[plan.SchemaID]bool{"/$defs/S": true}, b.refTrust)

	delete(b.refCache, "/$defs/S")
	require.True(t, b.refTrusted("/$defs/S", nil),
		"a memoized answer must not re-resolve the target")
}
