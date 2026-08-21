// applicator_scope_test.go pins issue #94: `additionalProperties` and `items` are scoped
// to the schema object that declared them and never see a name or an index an applicator
// declared, while `properties` and the element schema of `items` still intersect across
// the branches of an `allOf` (design §11.5, §12.4, §13.2).
package conformance

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/planterp"
)

func TestApplicatorScopedKeywords(t *testing.T) {
	tests := []struct {
		name     string
		schema   string
		instance string
		accept   bool
	}{
		{
			name:     "additionalProperties does not see a property declared in allOf",
			schema:   `{"allOf":[{"properties":{"foo":{}}}],"additionalProperties":false}`,
			instance: `{"foo":1}`,
		},
		{
			name:     "additionalProperties still admits nothing else",
			schema:   `{"allOf":[{"properties":{"foo":{}}}],"additionalProperties":false}`,
			instance: `{"bar":1}`,
		},
		{
			name:     "an empty object satisfies both branches",
			schema:   `{"allOf":[{"properties":{"foo":{}}}],"additionalProperties":false}`,
			instance: `{}`,
			accept:   true,
		},
		{
			name:     "additionalProperties does not see a pattern declared in allOf",
			schema:   `{"allOf":[{"patternProperties":{"^f":{}}}],"additionalProperties":false}`,
			instance: `{"foo":1}`,
		},
		{
			name:     "an allOf branch's additionalProperties constrains a name the outer object declares",
			schema:   `{"properties":{"foo":{}},"allOf":[{"additionalProperties":{"type":"string"}}]}`,
			instance: `{"foo":1}`,
		},
		{
			name:     "and accepts it when it satisfies that additionalProperties",
			schema:   `{"properties":{"foo":{}},"allOf":[{"additionalProperties":{"type":"string"}}]}`,
			instance: `{"foo":"x"}`,
			accept:   true,
		},
		{
			name:     "items does not see a prefixItems declared in allOf",
			schema:   `{"allOf":[{"prefixItems":[{"type":"string"}]}],"items":{"type":"integer"}}`,
			instance: `["x"]`,
		},
		{
			name:     "and the index satisfies neither branch",
			schema:   `{"allOf":[{"prefixItems":[{"type":"string"}]}],"items":{"type":"integer"}}`,
			instance: `[1]`,
		},
		{
			name:     "an empty array satisfies both branches",
			schema:   `{"allOf":[{"prefixItems":[{"type":"string"}]}],"items":{"type":"integer"}}`,
			instance: `[]`,
			accept:   true,
		},
		{
			name:     "a longer prefix keeps the shorter branch's items on the extra index",
			schema:   `{"allOf":[{"prefixItems":[{"type":"string"}],"items":{"type":"boolean"}}],"prefixItems":[{},{"type":"string"}]}`,
			instance: `["x","y"]`,
		},

		// The converse (design §11.5): allOf is an unordered intersection, so what the
		// branches say about one name or one index must all apply.
		{
			name:     "properties intersect across allOf branches",
			schema:   `{"allOf":[{"properties":{"a":{"type":"string"}}},{"properties":{"a":{"minLength":2}}}]}`,
			instance: `{"a":"x"}`,
		},
		{
			name:     "and accept what both branches accept",
			schema:   `{"allOf":[{"properties":{"a":{"type":"string"}}},{"properties":{"a":{"minLength":2}}}]}`,
			instance: `{"a":"xy"}`,
			accept:   true,
		},
		{
			name:     "the items element schema intersects across allOf branches",
			schema:   `{"allOf":[{"items":{"type":"integer"}},{"items":{"minimum":3}}]}`,
			instance: `[1]`,
		},
		{
			name:     "and accepts what both branches accept",
			schema:   `{"allOf":[{"items":{"type":"integer"}},{"items":{"minimum":3}}]}`,
			instance: `[4]`,
			accept:   true,
		},
		{
			name: "both behaviors in one schema",
			schema: `{"allOf":[{"properties":{"a":{"type":"string"}}},{"properties":{"a":{"minLength":2}}}],
				"properties":{"a":{}},"additionalProperties":false}`,
			instance: `{"a":"xy"}`,
			accept:   true,
		},
		{
			name: "both behaviors in one schema, rejecting the intersection",
			schema: `{"allOf":[{"properties":{"a":{"type":"string"}}},{"properties":{"a":{"minLength":2}}}],
				"properties":{"a":{}},"additionalProperties":false}`,
			instance: `{"a":"x"}`,
		},
		{
			name: "both behaviors in one schema, rejecting the undeclared name",
			schema: `{"allOf":[{"properties":{"b":{}}}],
				"properties":{"a":{}},"additionalProperties":false}`,
			instance: `{"b":1}`,
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
