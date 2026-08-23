package planner_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/plan"
)

// TestBuild_DeclaredDiscriminatorLooksThroughNestedCombinators pins issue #45: a branch
// that is itself an `anyOf`/`oneOf` pins the discriminator property when every alternative
// does, so the accepted value set is the union over alternatives and the intersection over
// `allOf` members. The proof is not weakened to let more schemas through: alternatives
// pinning different values, or one not pinning at all, still fall to the asserted tier or
// below.
func TestBuild_DeclaredDiscriminatorLooksThroughNestedCombinators(t *testing.T) {
	const mapping = `"discriminator": {
		"propertyName": "petType",
		"mapping": {"cat": "#/$defs/Cat", "dog": "#/$defs/Dog"}
	}`
	const dogDef = `"Dog": {
		"type": "object",
		"required": ["petType"],
		"properties": {"petType": {"const": "dog"}, "bark": {"type": "boolean"}}
	}`

	for _, tt := range []struct {
		name     string
		catDef   string
		disc     string
		property string
		tag      plan.TagSource
		values   []any
		branches map[string]string
		warn     bool
	}{
		{
			name: "every alternative pins the same value",
			catDef: `"Cat": {"anyOf": [
				{"type": "object", "required": ["petType"], "properties": {"petType": {"const": "cat"}, "a": {"type": "string"}}},
				{"type": "object", "required": ["petType"], "properties": {"petType": {"const": "cat"}, "b": {"type": "string"}}}
			]}`,
			disc:     mapping,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"cat", "dog"},
			branches: map[string]string{"cat": "/$defs/Cat", "dog": "/$defs/Dog"},
		},
		{
			name: "oneOf alternatives pin the same value",
			catDef: `"Cat": {"oneOf": [
				{"type": "object", "required": ["petType", "a"], "properties": {"petType": {"const": "cat"}, "a": {"type": "string"}}},
				{"type": "object", "required": ["petType", "b"], "properties": {"petType": {"const": "cat"}, "b": {"type": "string"}}}
			]}`,
			disc:     mapping,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"cat", "dog"},
			branches: map[string]string{"cat": "/$defs/Cat", "dog": "/$defs/Dog"},
		},
		{
			name: "anyOf nested in an allOf member",
			catDef: `"Cat": {"allOf": [
				{"type": "object", "required": ["name"], "properties": {"name": {"type": "string"}}},
				{"anyOf": [
					{"type": "object", "required": ["petType"], "properties": {"petType": {"const": "cat"}, "a": {"type": "string"}}},
					{"type": "object", "required": ["petType"], "properties": {"petType": {"const": "cat"}, "b": {"type": "string"}}}
				]}
			]}`,
			disc:     mapping,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"cat", "dog"},
			branches: map[string]string{"cat": "/$defs/Cat", "dog": "/$defs/Dog"},
		},
		{
			name: "allOf nested in an anyOf alternative",
			catDef: `"Cat": {"anyOf": [
				{"allOf": [
					{"type": "object", "required": ["petType"], "properties": {"petType": {"type": "string"}}},
					{"type": "object", "properties": {"petType": {"const": "cat"}, "a": {"type": "string"}}}
				]},
				{"allOf": [
					{"type": "object", "required": ["petType"], "properties": {"petType": {"type": "string"}}},
					{"type": "object", "properties": {"petType": {"const": "cat"}, "b": {"type": "string"}}}
				]}
			]}`,
			disc:     mapping,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"cat", "dog"},
			branches: map[string]string{"cat": "/$defs/Cat", "dog": "/$defs/Dog"},
		},
		{
			name: "the accepted set is the union of the alternatives",
			catDef: `"Cat": {"anyOf": [
				{"type": "object", "required": ["petType"], "properties": {"petType": {"const": "cat"}, "a": {"type": "string"}}},
				{"type": "object", "required": ["petType"], "properties": {"petType": {"const": "kitten"}, "b": {"type": "string"}}}
			]}`,
			disc: `"discriminator": {
				"propertyName": "petType",
				"mapping": {"cat": "#/$defs/Cat", "kitten": "#/$defs/Cat", "dog": "#/$defs/Dog"}
			}`,
			property: "petType",
			tag:      plan.TagDeclared,
			values:   []any{"cat", "kitten", "dog"},
			branches: map[string]string{"cat": "/$defs/Cat", "kitten": "/$defs/Cat", "dog": "/$defs/Dog"},
		},
		{
			name: "alternatives pinning different values are not proven",
			catDef: `"Cat": {"anyOf": [
				{"type": "object", "required": ["petType"], "properties": {"petType": {"const": "cat"}, "a": {"type": "string"}}},
				{"type": "object", "required": ["petType"], "properties": {"petType": {"const": "kitten"}, "b": {"type": "string"}}}
			]}`,
			disc:     mapping,
			property: "petType",
			tag:      plan.TagAsserted,
			values:   []any{"cat", "dog"},
			branches: map[string]string{"cat": "/$defs/Cat", "dog": "/$defs/Dog"},
		},
		{
			name: "an alternative that does not pin leaves the branch unproven",
			catDef: `"Cat": {"anyOf": [
				{"type": "object", "required": ["petType"], "properties": {"petType": {"const": "cat"}, "a": {"type": "string"}}},
				{"type": "object", "required": ["petType"], "properties": {"petType": {"type": "string"}, "b": {"type": "string"}}}
			]}`,
			disc:     mapping,
			property: "petType",
			tag:      plan.TagAsserted,
			values:   []any{"cat", "dog"},
			branches: map[string]string{"cat": "/$defs/Cat", "dog": "/$defs/Dog"},
		},
		{
			name: "an alternative that does not require the property is invalid",
			catDef: `"Cat": {"anyOf": [
				{"type": "object", "required": ["petType"], "properties": {"petType": {"const": "cat"}, "a": {"type": "string"}}},
				{"type": "object", "properties": {"petType": {"const": "cat"}, "b": {"type": "string"}}}
			]}`,
			disc: mapping,
			warn: true,
		},
		{
			// Issue #71: branch 1's accepted set is a strict *subset* of branch 0's, so
			// `oneOf` accepts neither. The declaration must not paper over it.
			name: "an alternative that is another branch collides",
			catDef: `"Cat": {"anyOf": [
				{"type": "object", "required": ["petType"], "properties": {"petType": {"const": "cat"}, "a": {"type": "string"}}},
				{"$ref": "#/$defs/Dog"}
			]}`,
			disc: `"discriminator": {"propertyName": "petType"}`,
			warn: true,
		},
		{
			name: "an alternative reaching into another branch collides",
			catDef: `"Cat": {"anyOf": [
				{"type": "object", "required": ["petType"], "properties": {"petType": {"const": "cat"}, "a": {"type": "string"}}},
				{"type": "object", "required": ["petType"], "properties": {"petType": {"const": "dog"}, "b": {"type": "string"}}}
			]}`,
			disc: `"discriminator": {"propertyName": "petType"}`,
			warn: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := buildDoc(t, `{
				"oneOf": [{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}],
				`+tt.disc+`,
				"$defs": {`+tt.catDef+`, `+dogDef+`}
			}`)

			require.Equal(t, tt.warn, hasWarning(got.Diagnostics), "diagnostics: %v", got.Diagnostics)

			disp, ok := got.Plan.Dispatch.(*plan.PropertyDispatch)
			if tt.property == "" {
				require.False(t, ok, "expected no PropertyDispatch, got %#v", got.Plan.Dispatch)
				return
			}
			require.True(t, ok, "expected PropertyDispatch, got %T", got.Plan.Dispatch)
			require.Equal(t, tt.property, disp.Property)
			require.Equal(t, tt.tag, disp.Tag)
			require.Equal(t, tt.values, caseValues(disp.Cases))
			require.Equal(t, tt.branches, caseBranches(t, disp.Cases))
			require.Equal(t, tt.tag == plan.TagAsserted, hasSeverity(got.Diagnostics, plan.SeverityInfo),
				"an asserted dispatch, and only an asserted one, reports itself: %v", got.Diagnostics)
		})
	}
}
