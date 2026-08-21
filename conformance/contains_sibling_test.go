// contains_sibling_test.go pins issue #75: `minContains`/`maxContains` have no effect
// unless `contains` is declared in the same schema object (draft 2020-12 6.4.4/6.4.5,
// design §13.5), so a lone bound neither rejects an array nor lifts the plan to
// predicate dispatch.
package conformance

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/planterp"
	"github.com/ogen-go/schemacompiler/plan"
)

func TestContainsGoverningSibling(t *testing.T) {
	tests := []struct {
		name     string
		schema   string
		instance string
		accept   bool
	}{
		{
			name:     "lone minContains does not reject an empty array",
			schema:   `{"minContains":1}`,
			instance: `[]`,
			accept:   true,
		},
		{
			name:     "lone maxContains does not reject a longer array",
			schema:   `{"maxContains":1}`,
			instance: `[1,2]`,
			accept:   true,
		},
		{
			name:     "lone minContains and maxContains together stay inert",
			schema:   `{"minContains":2,"maxContains":3}`,
			instance: `[1]`,
			accept:   true,
		},
		{
			name:     "a minContains whose contains sits in an allOf branch is inert",
			schema:   `{"allOf":[{"contains":{"type":"string"}}],"minContains":2}`,
			instance: `["a"]`,
			accept:   true,
		},
		{
			name:     "the branch's own contains still applies",
			schema:   `{"allOf":[{"contains":{"type":"string"}}],"minContains":2}`,
			instance: `[1]`,
		},
		{
			name:     "a maxContains whose contains sits in an allOf branch is inert",
			schema:   `{"allOf":[{"contains":{"type":"string"}}],"maxContains":1}`,
			instance: `["a","b"]`,
			accept:   true,
		},

		{
			name:     "contains alone still defaults to one match",
			schema:   `{"contains":{"type":"string"}}`,
			instance: `[1]`,
		},
		{
			name:     "and accepts one match",
			schema:   `{"contains":{"type":"string"}}`,
			instance: `[1,"a"]`,
			accept:   true,
		},
		{
			name:     "a sibling minContains still raises the bound",
			schema:   `{"contains":{"type":"string"},"minContains":2}`,
			instance: `["a"]`,
		},
		{
			name:     "and accepts two matches",
			schema:   `{"contains":{"type":"string"},"minContains":2}`,
			instance: `["a","b"]`,
			accept:   true,
		},
		{
			name:     "a sibling maxContains still caps the count",
			schema:   `{"contains":{"type":"string"},"maxContains":1}`,
			instance: `["a","b"]`,
		},
		{
			name:     "and accepts a count within the cap",
			schema:   `{"contains":{"type":"string"},"maxContains":1}`,
			instance: `["a",1]`,
			accept:   true,
		},
		{
			name:     "a sibling minContains of zero makes contains vacuous",
			schema:   `{"contains":{"type":"string"},"minContains":0}`,
			instance: `[1]`,
			accept:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := compileQuietly(json.RawMessage(tt.schema))
			require.NoError(t, err)

			value, err := decodeInstance(json.RawMessage(tt.instance))
			require.NoError(t, err)
			verdict, err := planterp.Interpret(res.Plan, value)
			require.NoError(t, err)
			require.Equal(t, tt.accept, verdict.Accepted, "reason: %v", verdict.Reason)
		})
	}
}

func TestContainsWithoutSiblingKeepsCapability(t *testing.T) {
	for _, schema := range []string{
		`{"minContains":1}`,
		`{"maxContains":1}`,
		`{"minContains":2,"maxContains":3}`,
	} {
		t.Run(schema, func(t *testing.T) {
			res, err := compileQuietly(json.RawMessage(schema))
			require.NoError(t, err)
			require.Empty(t, res.Plan.Validation.Predicates)
			require.Equal(t, plan.DirectGoType, res.Plan.Capability)
			require.Empty(t, res.Diagnostics)
		})
	}
}
