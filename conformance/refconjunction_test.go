// refconjunction_test.go pins issue #78 end to end: every member of a conjunction that
// contains a `$ref` must reach the plan, not just the first one (design §11.5).
package conformance

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/planterp"
	"github.com/ogen-go/schemacompiler/plan"
)

func TestRefConjunctionKeepsEveryMember(t *testing.T) {
	const (
		strings = `"$defs":{"A":{"type":"string","maxLength":3},"B":{"type":"string","minLength":2},
			"C":{"type":"string","pattern":"^a"}}`
		pets = `"$defs":{"Cat":{"type":"object","properties":{"petType":{"const":"cat"}},"required":["petType"]},
			"Kitten":{"type":"object","properties":{"petType":{"const":"kitten"}},"required":["petType"]},
			"Dog":{"type":"object","properties":{"petType":{"const":"dog"}},"required":["petType"]}}`
		recursive = `"$defs":{"Node":{"type":"object","properties":{"next":{"$ref":"#/$defs/Node"}}},
			"Named":{"type":"object","required":["name"]}}`
	)
	tests := []struct {
		name     string
		schema   string
		instance string
		accept   bool
	}{
		{
			name:     "two refs: the first one rejects",
			schema:   `{` + strings + `,"allOf":[{"$ref":"#/$defs/A"},{"$ref":"#/$defs/B"}]}`,
			instance: `"abcd"`,
		},
		{
			name:     "two refs: the second one rejects",
			schema:   `{` + strings + `,"allOf":[{"$ref":"#/$defs/A"},{"$ref":"#/$defs/B"}]}`,
			instance: `"a"`,
		},
		{
			name:     "two refs: both accept",
			schema:   `{` + strings + `,"allOf":[{"$ref":"#/$defs/A"},{"$ref":"#/$defs/B"}]}`,
			instance: `"ab"`,
			accept:   true,
		},
		{
			name: "two refs whose intersection is uninhabited",
			schema: `{"$defs":{"A":{"type":"string","maxLength":3},"B":{"type":"string","minLength":9}},
				"allOf":[{"$ref":"#/$defs/A"},{"$ref":"#/$defs/B"}]}`,
			instance: `"abc"`,
		},
		{
			name:     "three refs: the last one rejects",
			schema:   `{` + strings + `,"allOf":[{"$ref":"#/$defs/A"},{"$ref":"#/$defs/B"},{"$ref":"#/$defs/C"}]}`,
			instance: `"bc"`,
		},
		{
			name:     "three refs: all accept",
			schema:   `{` + strings + `,"allOf":[{"$ref":"#/$defs/A"},{"$ref":"#/$defs/B"},{"$ref":"#/$defs/C"}]}`,
			instance: `"ab"`,
			accept:   true,
		},
		{
			name: "a ref with a local sibling shape",
			schema: `{"$defs":{"A":{"type":"object","properties":{"a":{"type":"string"}}}},
				"allOf":[{"$ref":"#/$defs/A"},{"type":"object","properties":{"b":{"type":"integer"}},"required":["b"]}]}`,
			instance: `{"a":"x","b":"not an integer"}`,
		},
		{
			name: "a ref with a local sibling shape that accepts",
			schema: `{"$defs":{"A":{"type":"object","properties":{"a":{"type":"string"}}}},
				"allOf":[{"$ref":"#/$defs/A"},{"type":"object","properties":{"b":{"type":"integer"}},"required":["b"]}]}`,
			instance: `{"a":"x","b":1}`,
			accept:   true,
		},
		{
			name:     "a ref with a local sibling predicate",
			schema:   `{"$defs":{"A":{"type":"string"}},"allOf":[{"$ref":"#/$defs/A"},{"maxLength":2}]}`,
			instance: `"abc"`,
		},
		{
			name:     "a recursive ref conjoined with another ref",
			schema:   `{` + recursive + `,"allOf":[{"$ref":"#/$defs/Node"},{"$ref":"#/$defs/Named"}]}`,
			instance: `{"next":{"name":"inner"}}`,
		},
		{
			name:     "a recursive ref conjoined with another ref, accepted",
			schema:   `{` + recursive + `,"allOf":[{"$ref":"#/$defs/Node"},{"$ref":"#/$defs/Named"}]}`,
			instance: `{"name":"outer","next":{}}`,
			accept:   true,
		},

		// The discriminator repro of issue #78: Cat and Kitten pin petType to different
		// consts, so branch 0 is uninhabited and the schema means Dog alone.
		{
			name: "a discriminated branch intersecting two components",
			schema: `{"oneOf":[{"allOf":[{"$ref":"#/$defs/Cat"},{"$ref":"#/$defs/Kitten"}]},{"$ref":"#/$defs/Dog"}],
				"discriminator":{"propertyName":"petType"},` + pets + `}`,
			instance: `{"petType":"cat"}`,
		},
		{
			name: "a discriminated branch intersecting two components, the live branch",
			schema: `{"oneOf":[{"allOf":[{"$ref":"#/$defs/Cat"},{"$ref":"#/$defs/Kitten"}]},{"$ref":"#/$defs/Dog"}],
				"discriminator":{"propertyName":"petType"},` + pets + `}`,
			instance: `{"petType":"dog"}`,
			accept:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := compileQuietly(json.RawMessage(tt.schema))
			require.NoError(t, err)
			require.Less(t, res.Capability, plan.EvaluationStateValidation)

			value, err := decodeInstance(json.RawMessage(tt.instance))
			require.NoError(t, err)
			verdict, err := planterp.Interpret(res.Plan, value)
			require.NoError(t, err)
			require.Equal(t, tt.accept, verdict.Accepted, "reason: %v", verdict.Reason)
		})
	}
}
