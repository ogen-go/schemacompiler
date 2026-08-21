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
	// IgnoredNullable lists every inert `nullable: true` (see [Schema.IgnoredNullable]).
	IgnoredNullable []IgnoredNullable
	// InvalidKeyword lists every keyword with a value the spec does not admit
	// (see [Schema.InvalidKeyword]).
	InvalidKeyword []InvalidKeyword
	// UnusedDiscriminator lists every `discriminator` that names no union to dispatch on
	// (see [Schema.UnusedDiscriminator]).
	UnusedDiscriminator []UnusedDiscriminator
	// Uninhabited lists recursive schemas proven to have no finite JSON instance.
	Uninhabited []UninhabitedNode
}

// FromLibOpenAPIDocument converts an OpenAPI document's component schemas into one shared
// registry, registering each under `/components/schemas/<name>`, and resolves external
// references through loader (nil disables external resolution).
func FromLibOpenAPIDocument(ctx context.Context, schemas *orderedmap.Map[string, *base.SchemaProxy], baseURI string, loader Loader) (*Document, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	normalized, err := normalizeBaseURI(baseURI)
	if err != nil {
		return nil, err
	}

	st := newConvState(nil, loader)
	out := &Document{Schemas: make(map[string]*Node, orderedmap.Len(schemas))}
	if schemas != nil {
		for name, sp := range schemas.FromOldest() {
			if sp == nil {
				continue
			}
			pointer := ComponentPointer(name)
			sc := scope{frames: []frame{{baseURI: normalized, root: ""}}, docPointer: pointer, source: baseURI}
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
	out.IgnoredNullable = st.ignoredNullable
	out.InvalidKeyword = st.invalidKeyword
	out.UnusedDiscriminator = st.unusedDiscriminator
	return out, nil
}

// FromLibOpenAPIProxy adapts an already-parsed libopenapi schema position, including a
// boolean or `$ref` one, into the internal AST, resolving external references through
// loader (nil disables external resolution).
func FromLibOpenAPIProxy(ctx context.Context, sp *base.SchemaProxy, baseURI string, loader Loader) (*Schema, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if sp == nil {
		return nil, errors.New("nil schema")
	}
	normalized, err := normalizeBaseURI(baseURI)
	if err != nil {
		return nil, err
	}

	st := newConvState(nil, loader)
	sc := scope{frames: []frame{{baseURI: normalized, root: ""}}, source: baseURI}
	root, err := st.convertProxy(ctx, sp, sc)
	if err != nil {
		return nil, errors.Wrap(err, "convert root schema")
	}
	if _, ok := st.reg.resources[normalized]; !ok {
		st.reg.resources[normalized] = root
	}

	st.analyze(ctx)

	return &Schema{
		Registry:    st.reg,
		Root:        root,
		Unresolved:  st.unresolved,
		Uninhabited: st.reg.uninhabited,

		IgnoredNullable:     st.ignoredNullable,
		InvalidKeyword:      st.invalidKeyword,
		UnusedDiscriminator: st.unusedDiscriminator,
	}, nil
}
