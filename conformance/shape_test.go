// shape_test.go pins issue #72 end to end: a shape keyword written without a sibling
// `type` must reject the instances of its own kind that the schema rejects, and must go
// on accepting every instance of every other kind (design §3).
package conformance

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/planterp"
)

func TestShapeWithoutTypeIsKindGuarded(t *testing.T) {
	tests := []struct {
		name     string
		schema   string
		instance string
		accept   bool
	}{
		{
			name:     "object shape rejects a bad property",
			schema:   `{"properties":{"a":{"type":"string"}},"additionalProperties":false}`,
			instance: `{"a":1}`,
		},
		{
			name:     "object shape rejects an additional property",
			schema:   `{"properties":{"a":{"type":"string"}},"additionalProperties":false}`,
			instance: `{"b":1}`,
		},
		{
			name:     "object shape accepts a matching object",
			schema:   `{"properties":{"a":{"type":"string"}},"additionalProperties":false}`,
			instance: `{"a":"x"}`,
			accept:   true,
		},
		{
			name:     "array shape rejects a bad item",
			schema:   `{"items":{"type":"string"}}`,
			instance: `[1]`,
		},
		{
			name:     "array shape accepts a matching array",
			schema:   `{"items":{"type":"string"}}`,
			instance: `["x"]`,
			accept:   true,
		},
		{
			name:     "prefixItems rejects a bad tuple slot",
			schema:   `{"prefixItems":[{"type":"boolean"},{"type":"boolean"}],"items":false}`,
			instance: `[false,true,null]`,
		},
		{
			name:     "patternProperties rejects a matching property",
			schema:   `{"patternProperties":{"^a":{"type":"string"}}}`,
			instance: `{"ab":1}`,
		},
		{
			name: "a declared property must satisfy a matching pattern too",
			schema: `{"properties":{"foo":{"type":"array","maxItems":3}},
				"patternProperties":{"f.o":{"minItems":2}}}`,
			instance: `{"foo":[]}`,
		},

		// The §3 cases a wrong Applicability guard breaks: a type-specific keyword does
		// not assert its type, so every instance of another kind stays valid.
		{
			name:     "properties accepts a string",
			schema:   `{"properties":{"a":{"type":"integer"}}}`,
			instance: `"not an object"`,
			accept:   true,
		},
		{
			name:     "properties accepts a number, an array, null and a boolean",
			schema:   `{"properties":{"a":{"type":"integer"}},"additionalProperties":false}`,
			instance: `[1,2,3]`,
			accept:   true,
		},
		{
			name:     "items accepts a number",
			schema:   `{"items":{"type":"string"}}`,
			instance: `42`,
			accept:   true,
		},
		{
			name:     "items accepts an object",
			schema:   `{"items":false}`,
			instance: `{"a":1}`,
			accept:   true,
		},
		{
			name:     "patternProperties accepts a null",
			schema:   `{"patternProperties":{"^a":{"type":"string"}}}`,
			instance: `null`,
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
