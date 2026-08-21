package frontend

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// Converting the whole component set into one registry resolves `$ref`s between siblings
// and classifies recursion across them.
func TestFromLibOpenAPIDocumentComponentRefsResolve(t *testing.T) {
	doc, err := FromLibOpenAPIDocument(context.Background(), componentSchemas(t, openAPIRefDoc, false), "")
	require.NoError(t, err)
	require.Empty(t, doc.Unresolved)

	node := doc.Schemas["/components/schemas/Node"]
	require.NotNil(t, node)
	pet := doc.Schemas["/components/schemas/Pet"]
	require.NotNil(t, pet)

	petRef := property(t, node, "pet")
	require.Equal(t, "#/components/schemas/Pet", petRef.Ref)
	require.Same(t, pet, petRef.Resolved)

	child := property(t, node, "child")
	require.Same(t, node, child.Resolved)
	require.Equal(t, Guarded, doc.Registry.ClassifyRecursion(node))
	require.Equal(t, NotRecursive, doc.Registry.ClassifyRecursion(pet))
}

func TestFromLibOpenAPIDocumentEmpty(t *testing.T) {
	doc, err := FromLibOpenAPIDocument(context.Background(), nil, "")
	require.NoError(t, err)
	require.Empty(t, doc.Schemas)
	require.NotNil(t, doc.Registry)
}

func TestFromLibOpenAPIProxyBoolSchema(t *testing.T) {
	const boolDoc = `
openapi: 3.1.0
info: {title: t, version: v}
paths: {}
components:
  schemas:
    Node: true
`
	s, err := FromLibOpenAPIProxy(context.Background(), rootComponentProxy(t, boolDoc, false), "")
	require.NoError(t, err)
	require.NotNil(t, s.Root.Always)
	require.True(t, *s.Root.Always)
}

func TestComponentPointer(t *testing.T) {
	for _, tt := range []struct {
		name string
		want string
	}{
		{"Pet", "/components/schemas/Pet"},
		{"a/b", "/components/schemas/a~1b"},
		{"a~b", "/components/schemas/a~0b"},
		{"", "/components/schemas/"},
	} {
		require.Equal(t, tt.want, ComponentPointer(tt.name))
	}
}
