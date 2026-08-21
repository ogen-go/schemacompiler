package frontend

import (
	"context"

	"github.com/go-faster/errors"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/pb33f/libopenapi/orderedmap"
)

// ComponentsPrefix is the JSON Pointer prefix under which an OpenAPI document's component
// schemas are registered, so `#/components/schemas/<name>` references resolve.
const ComponentsPrefix = "/components/schemas"

// ComponentPointer returns the JSON Pointer of the named component schema.
func ComponentPointer(name string) string {
	return jsonPointerAppend(ComponentsPrefix, name)
}

// Document is a whole OpenAPI component set converted into one shared [Registry], so
// `$ref`s between sibling components resolve and recursion is classified across them.
type Document struct {
	// Registry holds every resolved schema resource of the document.
	Registry *Registry
	// Schemas maps each component's JSON Pointer ([ComponentPointer]) to its root Node.
	Schemas map[string]*Node
	// Unresolved lists every `$ref` whose target could not be found.
	Unresolved []UnresolvedRef
	// Uninhabited lists recursive schemas proven to have no finite JSON instance.
	Uninhabited []UninhabitedNode
}

// FromLibOpenAPIDocument converts an OpenAPI document's component schemas into one shared
// registry, registering each under `/components/schemas/<name>`.
func FromLibOpenAPIDocument(ctx context.Context, schemas *orderedmap.Map[string, *base.SchemaProxy], baseURI string) (*Document, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	st := newConvState(nil, nil)
	out := &Document{Schemas: make(map[string]*Node, orderedmap.Len(schemas))}
	if schemas != nil {
		for name, sp := range schemas.FromOldest() {
			if sp == nil {
				continue
			}
			pointer := ComponentPointer(name)
			sc := scope{frames: []frame{{baseURI: baseURI, root: ""}}, docPointer: pointer}
			n, err := st.convertProxy(ctx, sp, sc)
			if err != nil {
				return nil, errors.Wrapf(err, "convert component %q", name)
			}
			if n != nil {
				out.Schemas[pointer] = n
			}
		}
	}

	st.analyze(ctx)

	out.Registry = st.reg
	out.Unresolved = st.unresolved
	out.Uninhabited = st.reg.uninhabited
	return out, nil
}

// FromLibOpenAPIProxy adapts an already-parsed libopenapi schema position, including a
// boolean or `$ref` one, into the internal AST.
func FromLibOpenAPIProxy(ctx context.Context, sp *base.SchemaProxy, baseURI string) (*Schema, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if sp == nil {
		return nil, errors.New("nil schema")
	}
	st := newConvState(nil, nil)
	sc := scope{frames: []frame{{baseURI: baseURI, root: ""}}}
	root, err := st.convertProxy(ctx, sp, sc)
	if err != nil {
		return nil, errors.Wrap(err, "convert root schema")
	}
	if _, ok := st.reg.resources[baseURI]; !ok {
		st.reg.resources[baseURI] = root
	}

	st.analyze(ctx)

	return &Schema{
		Registry:    st.reg,
		Root:        root,
		Unresolved:  st.unresolved,
		Uninhabited: st.reg.uninhabited,
	}, nil
}
