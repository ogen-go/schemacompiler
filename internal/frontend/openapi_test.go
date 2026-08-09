package frontend

import (
	"context"
	"testing"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/stretchr/testify/require"
)

// componentSchema parses doc as an OpenAPI 3.1 document and returns the named component
// schema, mirroring how ogen hands schemacompiler an already-parsed schema.
func componentSchema(t *testing.T, doc, name string) *base.Schema {
	t.Helper()

	cfg := datamodel.NewDocumentConfiguration()
	cfg.TransformSiblingRefs = false
	cfg.MergeReferencedProperties = false
	cfg.SkipExternalRefResolution = true

	d, err := libopenapi.NewDocumentWithConfiguration([]byte(doc), cfg)
	require.NoError(t, err)

	model, errs := d.BuildV3Model()
	require.NoError(t, errs)

	sp, ok := model.Model.Components.Schemas.Get(name)
	require.True(t, ok, "component %q not found", name)
	return sp.Schema()
}

// property returns the named property schema of n.
func property(t *testing.T, n *Node, name string) *Node {
	t.Helper()
	for _, p := range n.Properties {
		if p.Name == name {
			return p.Schema
		}
	}
	t.Fatalf("property %q not found", name)
	return nil
}

const openAPIRefDoc = `
openapi: 3.1.0
info: {title: t, version: v}
paths: {}
components:
  schemas:
    Node:
      type: object
      properties:
        child: {$ref: '#/components/schemas/Node'}
        pet: {$ref: '#/components/schemas/Pet'}
    Pet:
      type: object
      properties:
        name: {type: string}
`

// A recursive component must not send conversion into infinite descent: libopenapi
// resolves a reference proxy to its target, so following it would revisit Node forever.
func TestFromLibOpenAPIRecursiveRefTerminates(t *testing.T) {
	s, err := FromLibOpenAPI(context.Background(), componentSchema(t, openAPIRefDoc, "Node"), "")
	require.NoError(t, err)
	require.NotNil(t, s.Root)

	child := property(t, s.Root, "child")
	require.Equal(t, "#/components/schemas/Node", child.Ref)
}

// A reference keeps its identity instead of being inlined: the target's keywords must not
// be copied into the referring node, so a backend can emit one named type per component.
func TestFromLibOpenAPIRefIsNotInlined(t *testing.T) {
	s, err := FromLibOpenAPI(context.Background(), componentSchema(t, openAPIRefDoc, "Node"), "")
	require.NoError(t, err)

	pet := property(t, s.Root, "pet")
	require.Equal(t, "#/components/schemas/Pet", pet.Ref)
	require.Empty(t, pet.Properties, "$ref target must not be inlined into the referring node")
	require.Zero(t, pet.Types)
}

// Component targets live outside the schema handed in, so they resolve to nothing today
// and are reported as diagnostics rather than aborting. Seeding the registry with the
// document's component set is the follow-up that makes them resolve.
func TestFromLibOpenAPIComponentRefsUnresolved(t *testing.T) {
	s, err := FromLibOpenAPI(context.Background(), componentSchema(t, openAPIRefDoc, "Node"), "")
	require.NoError(t, err)

	refs := make(map[string]string, len(s.Unresolved))
	for _, u := range s.Unresolved {
		refs[u.Pointer] = u.Ref
	}
	require.Equal(t, map[string]string{
		"/properties/child": "#/components/schemas/Node",
		"/properties/pet":   "#/components/schemas/Pet",
	}, refs)
}
