package frontend

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func mustLoad(t *testing.T, doc string) *Schema {
	t.Helper()
	s, err := Load(context.Background(), []byte(doc), "")
	require.NoError(t, err)
	require.NotNil(t, s)
	require.NotNil(t, s.Root)
	return s
}

func TestLoad_BooleanSchemas(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  string
		want bool
	}{
		{"true", `true`, true},
		{"false", `false`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := mustLoad(t, tc.doc)
			require.NotNil(t, s.Root.Always)
			require.Equal(t, tc.want, *s.Root.Always)
		})
	}
}

func TestLoad_BasicAssertions(t *testing.T) {
	doc := `{
		"type": "object",
		"title": "widget",
		"properties": {
			"name": {"type": "string", "minLength": 1},
			"count": {"type": "integer", "minimum": 0, "exclusiveMaximum": 10.5}
		},
		"required": ["name"],
		"additionalProperties": false
	}`
	s := mustLoad(t, doc)
	root := s.Root

	require.True(t, root.HasType)
	require.Equal(t, KindObject, root.Types)
	require.Equal(t, "widget", root.Title)
	require.Equal(t, []string{"name"}, root.Required)
	require.NotNil(t, root.AdditionalProperties)
	require.NotNil(t, root.AdditionalProperties.Always)
	require.False(t, *root.AdditionalProperties.Always)

	require.Len(t, root.Properties, 2)
	name := root.Properties[0]
	require.Equal(t, "name", name.Name)
	require.Equal(t, KindString, name.Schema.Types)
	require.NotNil(t, name.Schema.MinLength)
	require.EqualValues(t, 1, *name.Schema.MinLength)

	count := root.Properties[1]
	require.Equal(t, "count", count.Name)
	require.True(t, count.Schema.IntegerType)
	require.NotNil(t, count.Schema.Minimum)
	require.EqualValues(t, 0, *count.Schema.Minimum)
	require.NotNil(t, count.Schema.ExclusiveMaximum)
	require.EqualValues(t, 10.5, *count.Schema.ExclusiveMaximum)
}

func TestLoad_ConstEnum(t *testing.T) {
	doc := `{
		"const": 42,
		"enum": ["a", "b", 3]
	}`
	s := mustLoad(t, doc)
	root := s.Root
	require.NotNil(t, root.Const)
	require.InEpsilon(t, 42.0, root.Const.Decoded, 0)
	require.JSONEq(t, "42", string(root.Const.Raw))

	require.Len(t, root.Enum, 3)
	require.JSONEq(t, `"a"`, string(root.Enum[0].Raw))
	require.JSONEq(t, `"b"`, string(root.Enum[1].Raw))
	require.JSONEq(t, `3`, string(root.Enum[2].Raw))
}

func TestLoad_ArrayKeywords(t *testing.T) {
	doc := `{
		"type": "array",
		"prefixItems": [{"type": "string"}, {"type": "number"}],
		"items": false,
		"contains": {"type": "boolean"},
		"minItems": 1,
		"uniqueItems": true
	}`
	s := mustLoad(t, doc)
	root := s.Root
	require.Len(t, root.PrefixItems, 2)
	require.Equal(t, KindString, root.PrefixItems[0].Types)
	require.Equal(t, KindNumber, root.PrefixItems[1].Types)
	require.NotNil(t, root.Items)
	require.NotNil(t, root.Items.Always)
	require.False(t, *root.Items.Always)
	require.NotNil(t, root.Contains)
	require.Equal(t, KindBoolean, root.Contains.Types)
	require.NotNil(t, root.MinItems)
	require.EqualValues(t, 1, *root.MinItems)
	require.True(t, root.UniqueItems)
}

func TestLoad_RefAndDefs(t *testing.T) {
	doc := `{
		"$defs": {
			"Name": {"type": "string"}
		},
		"type": "object",
		"properties": {
			"name": {"$ref": "#/$defs/Name"}
		}
	}`
	s := mustLoad(t, doc)
	root := s.Root
	require.Len(t, root.Defs, 1)
	require.Equal(t, "Name", root.Defs[0].Name)

	nameProp := root.Properties[0].Schema
	require.Equal(t, "#/$defs/Name", nameProp.Ref)
	require.NotNil(t, nameProp.Resolved)
	require.Same(t, root.Defs[0].Schema, nameProp.Resolved)
}

func TestLoad_RefWithSiblings(t *testing.T) {
	// JSON Schema 2020-12: $ref coexists with sibling keywords.
	doc := `{
		"$defs": {"Str": {"type": "string"}},
		"$ref": "#/$defs/Str",
		"title": "aliased"
	}`
	s := mustLoad(t, doc)
	root := s.Root
	require.Equal(t, "#/$defs/Str", root.Ref)
	require.Equal(t, "aliased", root.Title)
	require.NotNil(t, root.Resolved)
	require.Equal(t, KindString, root.Resolved.Types)
}

func TestLoad_AnchorRef(t *testing.T) {
	doc := `{
		"$defs": {
			"Name": {"$anchor": "nameAnchor", "type": "string"}
		},
		"properties": {
			"n": {"$ref": "#nameAnchor"}
		}
	}`
	s := mustLoad(t, doc)
	root := s.Root
	target, ok := s.Registry.Anchor("", "nameAnchor")
	require.True(t, ok)
	require.Same(t, root.Defs[0].Schema, target)
	require.Same(t, target, root.Properties[0].Schema.Resolved)
}

func TestLoad_IdScopedRef(t *testing.T) {
	doc := `{
		"$id": "https://example.com/root.json",
		"$defs": {
			"Name": {"$id": "name.json", "type": "string"}
		},
		"properties": {
			"n": {"$ref": "name.json"}
		}
	}`
	s := mustLoad(t, doc)
	root := s.Root
	require.Equal(t, "https://example.com/root.json", root.ID)
	nameNode, ok := s.Registry.Resource("https://example.com/name.json")
	require.True(t, ok)
	require.Same(t, nameNode, root.Properties[0].Schema.Resolved)
}

func TestLoad_DynamicRefRecorded(t *testing.T) {
	doc := `{
		"$id": "https://example.com/root.json",
		"$dynamicAnchor": "node",
		"properties": {
			"next": {"$dynamicRef": "#node"}
		}
	}`
	s := mustLoad(t, doc)
	root := s.Root
	require.True(t, s.Registry.HasDynamicRefs())
	next := root.Properties[0].Schema
	require.Equal(t, "#node", next.DynamicRef)
	require.Nil(t, next.Resolved)
	target, ok := s.Registry.DynamicAnchor("https://example.com/root.json", "node")
	require.True(t, ok)
	require.Same(t, root, target)
}

func TestLoad_RecursionGuarded(t *testing.T) {
	// Node = { value: number, next: Node | null } — recursion crosses a property edge.
	doc := `{
		"$defs": {
			"Node": {
				"type": "object",
				"properties": {
					"value": {"type": "number"},
					"next": {"$ref": "#/$defs/Node"}
				}
			}
		},
		"$ref": "#/$defs/Node"
	}`
	s := mustLoad(t, doc)
	node := s.Root.Resolved
	require.NotNil(t, node)
	require.Equal(t, Guarded, s.Registry.ClassifyRecursion(node))
}

func TestLoad_RecursionUnguarded(t *testing.T) {
	// A pure allOf cycle: no property/item boundary is ever crossed.
	doc := `{
		"$defs": {
			"A": {"allOf": [{"$ref": "#/$defs/B"}]},
			"B": {"allOf": [{"$ref": "#/$defs/A"}]}
		},
		"$ref": "#/$defs/A"
	}`
	s := mustLoad(t, doc)
	a := s.Root.Resolved
	require.NotNil(t, a)
	require.Equal(t, Unguarded, s.Registry.ClassifyRecursion(a))
}

func TestLoad_NotRecursive(t *testing.T) {
	doc := `{"type": "object", "properties": {"a": {"type": "string"}}}`
	s := mustLoad(t, doc)
	require.Equal(t, NotRecursive, s.Registry.ClassifyRecursion(s.Root))
}

func TestLoad_YAML(t *testing.T) {
	doc := "type: string\nminLength: 2\n"
	s := mustLoad(t, doc)
	require.Equal(t, KindString, s.Root.Types)
	require.NotNil(t, s.Root.MinLength)
	require.EqualValues(t, 2, *s.Root.MinLength)
}

func TestLoad_InvalidDocument(t *testing.T) {
	_, err := Load(context.Background(), []byte("[unterminated"), "")
	require.Error(t, err)
}

func TestLoad_UnresolvableRef(t *testing.T) {
	// A dangling $ref does not fail loading; it is recorded in Schema.Unresolved and the
	// referencing node is left with Resolved == nil.
	doc := `{"$ref": "#/$defs/Missing"}`
	s, err := Load(context.Background(), []byte(doc), "")
	require.NoError(t, err)
	require.Len(t, s.Unresolved, 1)
	require.Equal(t, "#/$defs/Missing", s.Unresolved[0].Ref)
	require.Nil(t, s.Root.Resolved)
}
