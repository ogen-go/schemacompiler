package planterp_test

import (
	"testing"

	"github.com/go-faster/errors"
	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/planterp"
	"github.com/ogen-go/schemacompiler/plan"
)

// TestNilRepresentationSlots pins what a nil Representation means in each slot.
// plan/representation.go documents exactly two of them — ObjectRepresentation.Additional
// and ArrayRepresentation.Rest — and those keep their documented meaning. Everywhere
// else a nil is malformed plan data: planwalk.Children drops such a slot, and dropping
// it silently would narrow the accepted set, the one direction design §24 forbids. So it
// is an InternalError, which fails loudly instead of moving a verdict.
func TestNilRepresentationSlots(t *testing.T) {
	strRep := plan.PrimitiveRepresentation{Kind: plan.KindString}

	tests := []struct {
		name string
		rep  plan.Representation
		// internal is the expected InternalError, empty when the nil is documented and
		// the plan still reaches a verdict.
		internal string
		value    any
		accept   bool
	}{
		{
			name: "documented nil Additional cannot reject",
			rep: plan.ObjectRepresentation{
				Fields: map[string]plan.FieldRepresentation{
					"a": {Representation: strRep, Presence: plan.PresenceOptional},
				},
			},
			value:  map[string]any{"b": 1.0},
			accept: true,
		},
		{
			name:   "documented nil Rest rejects items past the prefix",
			rep:    plan.ArrayRepresentation{Prefix: []plan.ItemRepresentation{{Representation: strRep}}},
			value:  []any{"a", "b"},
			accept: false,
		},
		{
			name:     "nil union alternative",
			rep:      plan.UnionRepresentation{Alternatives: []plan.Representation{strRep, nil}},
			value:    1.0,
			internal: "planterp: union alternative 1 at the instance root has no representation",
		},
		{
			name: "nil field representation",
			rep: plan.ObjectRepresentation{
				Fields:     map[string]plan.FieldRepresentation{"a": {Presence: plan.PresenceRequired}},
				Additional: plan.AnyRepresentation{},
			},
			value:    map[string]any{},
			internal: `planterp: field "a" at the instance root has no representation`,
		},
		{
			name: "nil pattern rule representation",
			rep: plan.ObjectRepresentation{
				PatternRules: []plan.PatternFieldRepresentation{{Pattern: "^a"}},
				Additional:   plan.AnyRepresentation{},
			},
			value:    map[string]any{"ab": 1.0},
			internal: `planterp: pattern rule 0 ("^a") at the instance root has no representation`,
		},
		{
			name:     "nil prefix item representation",
			rep:      plan.ArrayRepresentation{Prefix: []plan.ItemRepresentation{{}}},
			value:    []any{"a"},
			internal: "planterp: prefix item 0 at the instance root has no representation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := planterp.Interpret(plan.CompilationPlan{Representation: tt.rep}, tt.value)
			if tt.internal == "" {
				require.NoError(t, err)
				require.Equal(t, tt.accept, v.Accepted)
				return
			}

			var internal *planterp.InternalError
			require.ErrorAs(t, err, &internal)
			require.Equal(t, tt.internal, internal.Error())

			var validate *planterp.ValidateError
			var invalid *planterp.InvalidValueError
			require.False(t, errors.As(err, &validate), "a malformed plan is not a rejection")
			require.False(t, errors.As(err, &invalid))
		})
	}
}

// TestNilSlotLocationIsReported keeps the instance location on a malformed slot: one
// plan node is reached once per sub-value, so the path is what makes it findable.
func TestNilSlotLocationIsReported(t *testing.T) {
	p := plan.CompilationPlan{Representation: plan.ArrayRepresentation{
		Rest: plan.ItemRepresentation{Representation: plan.UnionRepresentation{
			Alternatives: []plan.Representation{nil},
		}},
	}}
	_, err := planterp.Interpret(p, []any{"a", "b"})

	var internal *planterp.InternalError
	require.ErrorAs(t, err, &internal)
	require.Equal(t, "planterp: union alternative 0 at /0 has no representation", internal.Error())
}
