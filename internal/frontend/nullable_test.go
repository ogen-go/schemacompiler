package frontend

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func nullableDoc(version, node string) string {
	return fmt.Sprintf(`
openapi: %s
info: {title: t, version: v}
paths: {}
components:
  schemas:
    Node: %s
    Cat: {type: object, required: [kind], properties: {kind: {const: cat}}}
    Dog: {type: object, required: [kind], properties: {kind: {const: dog}}}
`, version, node)
}

func nullableSchema(t *testing.T, version, node string) *Schema {
	t.Helper()
	s, err := FromLibOpenAPI(context.Background(), rootComponent(t, nullableDoc(version, node)), "")
	require.NoError(t, err)
	require.NotNil(t, s.Root)
	return s
}

// `nullable: true` widens the `type` keyword, and only where one is declared alongside it
// (OAS 3.0.3 line 2335). Every other spelling is inert and reported.
func TestNullable(t *testing.T) {
	for _, tt := range []struct {
		name    string
		version string
		node    string
		ignored bool
		check   func(t *testing.T, n *Node)
	}{
		{
			name:    "Scalar",
			version: "3.0.3",
			node:    "{type: string, nullable: true}",
			check: func(t *testing.T, n *Node) {
				require.Equal(t, KindString|KindNull, n.Types)
			},
		},
		{
			name:    "TypeArray",
			version: "3.0.3",
			node:    "{type: [string, integer], nullable: true}",
			check: func(t *testing.T, n *Node) {
				require.Equal(t, KindString|KindNumber|KindNull, n.Types)
			},
		},
		{
			name:    "False",
			version: "3.0.3",
			node:    "{type: string, nullable: false}",
			check: func(t *testing.T, n *Node) {
				require.Equal(t, KindString, n.Types)
			},
		},
		{
			name:    "FalseKeepsDeclaredNull",
			version: "3.1.0",
			node:    `{type: [string, "null"], nullable: false}`,
			check: func(t *testing.T, n *Node) {
				require.Equal(t, KindString|KindNull, n.Types)
			},
		},
		{
			name:    "AlreadyNullable",
			version: "3.1.0",
			node:    `{type: [string, "null"], nullable: true}`,
			check: func(t *testing.T, n *Node) {
				require.Equal(t, KindString|KindNull, n.Types)
			},
		},
		{
			name:    "OpenAPI31",
			version: "3.1.0",
			node:    "{type: string, nullable: true}",
			check: func(t *testing.T, n *Node) {
				require.Equal(t, KindString|KindNull, n.Types)
			},
		},
		// Sibling keywords "retain their defined behavior" (clause 2): the kind set gains
		// null, and whether anything else goes on rejecting it is their business.
		{
			name:    "TypeWithOneOf",
			version: "3.0.3",
			node:    "{type: object, oneOf: [{required: [a]}, {required: [b]}], nullable: true}",
			check: func(t *testing.T, n *Node) {
				require.Equal(t, KindObject|KindNull, n.Types)
				require.Len(t, n.OneOf, 2)
			},
		},
		{
			name:    "Enum",
			version: "3.0.3",
			node:    "{type: string, enum: [a, b], nullable: true}",
			check: func(t *testing.T, n *Node) {
				require.Equal(t, KindString|KindNull, n.Types)
				require.Len(t, n.Enum, 2)
			},
		},
		{
			name:    "Const",
			version: "3.0.3",
			node:    "{type: string, const: a, nullable: true}",
			check: func(t *testing.T, n *Node) {
				require.Equal(t, KindString|KindNull, n.Types)
				require.NotNil(t, n.Const)
			},
		},
		// No `type` in the same Schema Object: nothing to widen (clause 1).
		{
			name:    "Bare",
			version: "3.0.3",
			node:    "{nullable: true}",
			ignored: true,
			check: func(t *testing.T, n *Node) {
				require.False(t, n.HasType)
			},
		},
		{
			name:    "OneOf",
			version: "3.0.3",
			node:    "{oneOf: [{type: string}, {type: integer}], nullable: true}",
			ignored: true,
			check: func(t *testing.T, n *Node) {
				require.False(t, n.HasType)
				require.Len(t, n.OneOf, 2)
			},
		},
		{
			name:    "AnyOf",
			version: "3.0.3",
			node:    "{anyOf: [{type: string}, {type: integer}], nullable: true}",
			ignored: true,
			check: func(t *testing.T, n *Node) {
				require.False(t, n.HasType)
				require.Len(t, n.AnyOf, 2)
			},
		},
		{
			name:    "AllOf",
			version: "3.0.3",
			node:    "{allOf: [{type: object}], nullable: true}",
			ignored: true,
			check: func(t *testing.T, n *Node) {
				require.False(t, n.HasType)
				require.Len(t, n.AllOf, 1)
			},
		},
		{
			name:    "EnumWithoutType",
			version: "3.0.3",
			node:    "{enum: [a, b], nullable: true}",
			ignored: true,
			check: func(t *testing.T, n *Node) {
				require.False(t, n.HasType)
				require.Len(t, n.Enum, 2)
			},
		},
		{
			name:    "Discriminator",
			version: "3.0.3",
			node: "{oneOf: [{$ref: '#/components/schemas/Cat'}, {$ref: '#/components/schemas/Dog'}], " +
				"discriminator: {propertyName: kind}, nullable: true}",
			ignored: true,
			check: func(t *testing.T, n *Node) {
				require.False(t, n.HasType)
				require.NotNil(t, n.Discriminator)
				require.Equal(t, "kind", n.Discriminator.PropertyName)
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := nullableSchema(t, tt.version, tt.node)
			tt.check(t, s.Root)
			if tt.ignored {
				require.Len(t, s.IgnoredNullable, 1)
			} else {
				require.Empty(t, s.IgnoredNullable)
			}
		})
	}
}

func TestNullableProperty(t *testing.T) {
	s := nullableSchema(t, "3.0.3", "{type: object, properties: {"+
		"scalar: {type: integer, nullable: true}, "+
		"union: {oneOf: [{type: string}, {type: integer}], nullable: true}, "+
		"ref: {$ref: '#/components/schemas/Cat', nullable: true}}}")

	scalar := property(t, s.Root, "scalar")
	require.Equal(t, KindNumber|KindNull, scalar.Types)
	require.True(t, scalar.IntegerType)

	union := property(t, s.Root, "union")
	require.False(t, union.HasType)
	require.Len(t, union.OneOf, 2)

	ref := property(t, s.Root, "ref")
	require.False(t, ref.HasType)
	require.Equal(t, "#/components/schemas/Cat", ref.Ref)

	require.Len(t, s.IgnoredNullable, 2)
	for _, ig := range s.IgnoredNullable {
		require.Contains(t, []string{"/properties/union", "/properties/ref"}, ig.Pointer)
		require.NotZero(t, ig.Position.Line)
	}
}

// A `nullable` sibling of a `$ref` neither resolves the target nor disturbs the
// reference's identity: the keyword is simply inert.
func TestNullableRefSibling(t *testing.T) {
	s := mustLoad(t, `{
		"$defs": {"Named": {"$id": "https://example.com/named", "type": "string"}},
		"$ref": "#/$defs/Named",
		"nullable": true
	}`)

	require.Equal(t, "#/$defs/Named", s.Root.Ref)
	require.False(t, s.Root.HasType)
	require.Len(t, s.IgnoredNullable, 1)

	target := s.Root.Resolved
	require.NotNil(t, target)
	require.Equal(t, KindString, target.Types)
	require.Equal(t, "https://example.com/named", target.ID)

	resource, ok := s.Registry.Resource("https://example.com/named")
	require.True(t, ok)
	require.Same(t, target, resource)
}

// A nullable recursive schema stays guarded, and adding null to the kind set introduces
// neither an uninhabited schema nor an extra graph edge.
func TestNullableRecursive(t *testing.T) {
	s := mustLoad(t, `{
		"type": "object",
		"properties": {"self": {"type": "object", "$ref": "#", "nullable": true}}
	}`)

	require.Empty(t, s.Uninhabited)
	require.Empty(t, s.Unresolved)
	require.Empty(t, s.IgnoredNullable)

	self := property(t, s.Root, "self")
	require.Equal(t, KindObject|KindNull, self.Types)
	require.Same(t, s.Root, self.Resolved)
	require.Equal(t, Guarded, s.Registry.ClassifyRecursion(s.Root))
}

// libopenapi resolves a reference proxy to its *target*, so the keywords declared beside a
// `$ref` are not the ones convertSchema is handed. Clause 1 must be decided on what the
// author wrote at this position, not on the target's `type`.
func TestNullableRefSiblingClause1(t *testing.T) {
	const cat = "'#/components/schemas/Cat'"

	for _, tt := range []struct {
		name    string
		node    string
		null    bool
		ignored int
	}{
		{
			name:    "NoAuthoredType",
			node:    "{$ref: " + cat + ", nullable: true}",
			ignored: 1,
		},
		{
			name: "AuthoredType",
			node: "{$ref: " + cat + ", type: object, nullable: true}",
			null: true,
		},
		{
			name: "NoNullable",
			node: "{$ref: " + cat + "}",
		},
		{
			name: "AuthoredTypeArray",
			node: `{$ref: ` + cat + `, type: [object, "null"], nullable: true}`,
			null: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := nullableSchema(t, "3.0.3", tt.node)

			require.Equal(t, "#/components/schemas/Cat", s.Root.Ref)
			require.Equal(t, tt.null, s.Root.Types&KindNull != 0)
			require.Len(t, s.IgnoredNullable, tt.ignored)
		})
	}
}
