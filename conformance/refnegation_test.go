// refnegation_test.go is the end-to-end half of issue #108. No JSON-Schema-Test-Suite
// schema negates a `$ref`, so the differential oracle is byte-identical with and without
// the target-consulting gate; these cases are what actually hold it to §24.
package conformance

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/planterp"
	"github.com/ogen-go/schemacompiler/plan"
)

func TestNegatedRefIsEnforcedWithoutRejectingValid(t *testing.T) {
	const defs = `"$defs":{
		"S":{"type":"string","minLength":3},
		"Hop":{"$ref":"#/$defs/S"},
		"Node":{"type":"object","properties":{"next":{"$ref":"#/$defs/Node"}}}}`

	for _, tt := range []struct {
		name     string
		schema   string
		instance string
		accept   bool
		exact    bool
	}{
		{
			name:     "an instance the target accepts is rejected by the negation",
			schema:   `{"not":{"$ref":"#/$defs/S"},` + defs + `}`,
			instance: `"abcd"`,
		},
		{
			name:     "an instance the target rejects is accepted",
			schema:   `{"not":{"$ref":"#/$defs/S"},` + defs + `}`,
			instance: `"ab"`,
			accept:   true,
			exact:    true,
		},
		{
			name:     "a non-string is accepted",
			schema:   `{"not":{"$ref":"#/$defs/S"},` + defs + `}`,
			instance: `12`,
			accept:   true,
			exact:    true,
		},
		{
			name:     "the target is reached through several hops",
			schema:   `{"not":{"$ref":"#/$defs/Hop"},` + defs + `}`,
			instance: `"abcd"`,
		},
		{
			name:     "a recursive target keeps the negation dropped, so nothing valid is rejected",
			schema:   `{"not":{"$ref":"#/$defs/Node"},` + defs + `}`,
			instance: `{"next":{}}`,
			accept:   true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res, err := compileQuietly(json.RawMessage(tt.schema))
			require.NoError(t, err)
			require.Less(t, res.Exactness, plan.UnsupportedConversion)
			if tt.exact {
				require.Equal(t, plan.ExactWithValidation, res.Exactness)
			}

			value, err := decodeInstance(json.RawMessage(tt.instance))
			require.NoError(t, err)
			verdict, err := planterp.Interpret(res.Plan, value)
			require.NoError(t, err)
			require.Equal(t, tt.accept, verdict.Accepted, "reason: %v", verdict.Reason)
		})
	}
}
