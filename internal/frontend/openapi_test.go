package frontend

import (
	"context"
	"fmt"
	"testing"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"
)

// rootComponent parses doc as an OpenAPI 3.1 document and returns its Node component
// schema (every fixture here is rooted at Node), mirroring how ogen hands schemacompiler
// an already-parsed schema.
func rootComponent(t *testing.T, doc string) *base.Schema {
	t.Helper()
	return rootComponentProxy(t, doc, false).Schema()
}

// rootComponentProxy is [rootComponent] keeping the proxy, and with libopenapi's
// sibling-$ref transform selectable: ogen disables it, but a caller may not.
func rootComponentProxy(t *testing.T, doc string, transformSiblingRefs bool) *base.SchemaProxy {
	t.Helper()

	const name = "Node"
	sp, ok := componentSchemas(t, doc, transformSiblingRefs).Get(name)
	require.True(t, ok, "component %q not found", name)
	return sp
}

// componentSchemas parses doc as an OpenAPI 3.1 document and returns its whole component
// schema set.
func componentSchemas(t *testing.T, doc string, transformSiblingRefs bool) *orderedmap.Map[string, *base.SchemaProxy] {
	t.Helper()

	cfg := datamodel.NewDocumentConfiguration()
	cfg.TransformSiblingRefs = transformSiblingRefs
	cfg.MergeReferencedProperties = false
	cfg.SkipExternalRefResolution = true

	d, err := libopenapi.NewDocumentWithConfiguration([]byte(doc), cfg)
	require.NoError(t, err)

	model, errs := d.BuildV3Model()
	require.NoError(t, errs)

	return model.Model.Components.Schemas
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
	s, err := FromLibOpenAPI(context.Background(), rootComponent(t, openAPIRefDoc), "")
	require.NoError(t, err)
	require.NotNil(t, s.Root)

	child := property(t, s.Root, "child")
	require.Equal(t, "#/components/schemas/Node", child.Ref)
}

// A reference keeps its identity instead of being inlined: the target's keywords must not
// be copied into the referring node, so a backend can emit one named type per component.
func TestFromLibOpenAPIRefIsNotInlined(t *testing.T) {
	s, err := FromLibOpenAPI(context.Background(), rootComponent(t, openAPIRefDoc), "")
	require.NoError(t, err)

	pet := property(t, s.Root, "pet")
	require.Equal(t, "#/components/schemas/Pet", pet.Ref)
	require.Empty(t, pet.Properties, "$ref target must not be inlined into the referring node")
	require.Zero(t, pet.Types)
}

// A schema converted on its own cannot see its sibling components, so their targets are
// reported as diagnostics rather than aborting. [FromLibOpenAPIDocument] is the entry
// point that resolves them.
func TestFromLibOpenAPIStandaloneComponentRefsUnresolved(t *testing.T) {
	s, err := FromLibOpenAPI(context.Background(), rootComponent(t, openAPIRefDoc), "")
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

const openAPISiblingDoc = `
openapi: 3.1.0
info: {title: t, version: v}
paths: {}
components:
  schemas:
    Node:
      type: object
      properties:
        pet:
          $ref: '#/components/schemas/Pet'
          description: my pet
          readOnly: true
        litter:
          $ref: '#/components/schemas/Pet'
          description: a litter
          properties:
            runt: {$ref: '#/components/schemas/Pet'}
    Pet:
      type: object
      description: a pet
      properties:
        name: {type: string}
`

// 2020-12 lets `$ref` carry sibling keywords. libopenapi points a reference proxy at the
// target, so the siblings must be recovered from the node that declared the reference —
// under either setting of its sibling-$ref transform.
func TestFromLibOpenAPIRefSiblings(t *testing.T) {
	for _, transform := range []bool{false, true} {
		t.Run(fmt.Sprintf("TransformSiblingRefs=%v", transform), func(t *testing.T) {
			sp := rootComponentProxy(t, openAPISiblingDoc, transform)
			s, err := FromLibOpenAPI(context.Background(), sp.Schema(), "")
			require.NoError(t, err)

			pet := property(t, s.Root, "pet")
			require.Equal(t, "#/components/schemas/Pet", pet.Ref)
			require.Equal(t, "my pet", pet.Description, "keywords alongside $ref must survive")
			require.True(t, pet.ReadOnly)
			require.Empty(t, pet.Properties, "$ref target must not be inlined")
			require.NotEqual(t, "a pet", pet.Description, "description must be the sibling's, not the target's")
		})
	}
}

// A `$ref` inside a sibling keyword keeps its own reference: the sibling schema is rebuilt
// without an index, so nested references have to be stripped rather than followed.
func TestFromLibOpenAPIRefSiblingNestedRef(t *testing.T) {
	s, err := FromLibOpenAPI(context.Background(), rootComponent(t, openAPISiblingDoc), "")
	require.NoError(t, err)

	litter := property(t, s.Root, "litter")
	require.Equal(t, "#/components/schemas/Pet", litter.Ref)
	require.Equal(t, "a litter", litter.Description)

	runt := property(t, litter, "runt")
	require.Equal(t, "#/components/schemas/Pet", runt.Ref)
	require.Empty(t, runt.Properties)
}

// Recovering siblings must not disturb the document it read them from.
func TestFromLibOpenAPIRefSiblingsDoNotMutateDocument(t *testing.T) {
	sp := rootComponentProxy(t, openAPISiblingDoc, false)
	pet, ok := sp.Schema().Properties.Get("pet")
	require.True(t, ok)
	before := pet.GetReferenceNode()
	keysBefore := mappingKeys(before)

	_, err := FromLibOpenAPI(context.Background(), sp.Schema(), "")
	require.NoError(t, err)

	require.Equal(t, keysBefore, mappingKeys(pet.GetReferenceNode()))
	require.Contains(t, keysBefore, "$ref")
}

func mappingKeys(n *yaml.Node) []string {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	keys := make([]string, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		keys = append(keys, n.Content[i].Value)
	}
	return keys
}

const openAPIDiscriminatorDoc = `
openapi: 3.1.0
info: {title: t, version: v}
paths: {}
components:
  schemas:
    Node:
      oneOf:
        - $ref: '#/components/schemas/Cat'
        - $ref: '#/components/schemas/Dog'
      discriminator:
        propertyName: petType
        mapping:
          cat: '#/components/schemas/Cat'
          dog: '#/components/schemas/Dog'
    Cat: {type: object}
    Dog: {type: object}
`

// The discriminator keyword must survive conversion with its mapping in declaration
// order, so the planner can prefer it over structural inference (issue #17).
func TestFromLibOpenAPIDiscriminator(t *testing.T) {
	s, err := FromLibOpenAPI(context.Background(), rootComponent(t, openAPIDiscriminatorDoc), "")
	require.NoError(t, err)

	require.NotNil(t, s.Root.Discriminator)
	require.Equal(t, "petType", s.Root.Discriminator.PropertyName)
	require.Equal(t, []DiscriminatorMapping{
		{Value: "cat", Ref: "#/components/schemas/Cat"},
		{Value: "dog", Ref: "#/components/schemas/Dog"},
	}, s.Root.Discriminator.Mapping)
}

func TestFromLibOpenAPINoDiscriminator(t *testing.T) {
	s, err := FromLibOpenAPI(context.Background(), rootComponent(t, openAPIRefDoc), "")
	require.NoError(t, err)
	require.Nil(t, s.Root.Discriminator)
}
