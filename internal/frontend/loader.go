package frontend

import (
	"context"
	"maps"
	"net/url"

	"github.com/go-faster/errors"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	lowbase "github.com/pb33f/libopenapi/datamodel/low/base"
	"go.yaml.in/yaml/v4"
)

// Loader fetches the raw bytes of an external schema document identified by uri (an
// absolute URI with the fragment removed). It is invoked lazily during reference
// resolution for any $ref whose target document is not the root. A nil Loader leaves
// external references unresolved (recorded as diagnostics), preserving the standalone
// single-document behavior.
type Loader func(ctx context.Context, uri *url.URL) ([]byte, error)

// loadDocument parses data (JSON or YAML; JSON is a YAML subset) into a *yaml.Node tree
// suitable for libopenapi's low-level Build path.
func loadDocument(data []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, errors.Wrap(err, "parse document")
	}
	if len(doc.Content) == 0 {
		return nil, errors.New("empty document")
	}
	return doc.Content[0], nil
}

// stripRefs walks root, extracting every `$ref` key/value pair into refs (keyed by the
// exact *yaml.Node it was removed from) and deleting it from the node's Content.
//
// This is done so libopenapi never sees a `$ref`: low/base.Schema.Build auto-follows any
// node containing a literal `$ref` key (regardless of sibling keywords), replacing the
// node in place with its resolved target via the low-level index/rolodex — machinery this
// package deliberately bypasses in favor of its own resolver (design §10). Stripping
// `$ref` upfront, recursively, lets libopenapi build every sibling keyword normally
// (matching JSON Schema 2020-12, where `$ref` coexists with other keywords) while we
// recover the reference string ourselves from the map.
//
// `$dynamicRef` needs no such treatment: libopenapi stores it verbatim without ever
// attempting to follow it.
func stripRefs(root *yaml.Node, refs map[*yaml.Node]string) {
	if root == nil {
		return
	}
	switch root.Kind {
	case yaml.MappingNode:
		for i := 0; i < len(root.Content); i += 2 {
			if root.Content[i].Value == "$ref" {
				refs[root] = root.Content[i+1].Value
				root.Content = append(root.Content[:i], root.Content[i+2:]...)
				i -= 2
				continue
			}
		}
		for _, c := range root.Content {
			stripRefs(c, refs)
		}
	case yaml.SequenceNode, yaml.DocumentNode:
		for _, c := range root.Content {
			stripRefs(c, refs)
		}
	}
}

// buildHighSchema parses data (JSON or YAML) into a high-level libopenapi schema plus the
// map of `$ref` strings stripped from it (see [stripRefs]). A boolean root schema (`true`/
// `false`) is reported via the returned bool pointer, in which case hs/refs are nil.
func buildHighSchema(ctx context.Context, data []byte) (hs *base.Schema, refs map[*yaml.Node]string, boolValue *bool, err error) {
	root, err := loadDocument(data)
	if err != nil {
		return nil, nil, nil, err
	}
	if b, ok := boolSchemaValue(root); ok {
		return nil, nil, &b, nil
	}

	refs = make(map[*yaml.Node]string)
	stripRefs(root, refs)

	low := new(lowbase.Schema)
	if err := low.Build(ctx, root, nil); err != nil {
		return nil, nil, nil, errors.Wrap(err, "build schema")
	}
	return base.NewSchema(low), refs, nil, nil
}

// Load parses a standalone Draft 2020-12 schema document (JSON or YAML) into the internal
// AST, resolving only in-document references. It is equivalent to [LoadWithLoader] with a
// nil loader.
func Load(ctx context.Context, data []byte, baseURI string) (*Schema, error) {
	return LoadWithLoader(ctx, data, baseURI, nil)
}

// LoadWithLoader parses a Draft 2020-12 schema document and resolves its references,
// fetching external/remote documents on demand via loader (nil disables external
// resolution). libopenapi's NewDocument is OpenAPI-only, so this uses the low-level build
// path (yaml.Node -> lowbase.Build -> base.NewSchema).
func LoadWithLoader(ctx context.Context, data []byte, baseURI string, loader Loader) (*Schema, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	hs, refs, boolValue, err := buildHighSchema(ctx, data)
	if err != nil {
		return nil, err
	}

	if boolValue != nil {
		reg := newRegistry()
		n := &Node{Always: boolValue}
		reg.resources[baseURI] = n
		reg.nodes = append(reg.nodes, n)
		reg.analyzeSCCs()
		return &Schema{Registry: reg, Root: n}, nil
	}

	return convertRoot(ctx, hs, refs, baseURI, loader)
}

// FromLibOpenAPI adapts an already-parsed libopenapi schema (e.g. one ogen extracted
// from an OpenAPI document) into the internal AST — the join point with ogen. No
// re-parsing occurs.
func FromLibOpenAPI(ctx context.Context, s *base.Schema, baseURI string) (*Schema, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, errors.New("nil schema")
	}
	return convertRoot(ctx, s, nil, baseURI, nil)
}

// tryLoad fetches and integrates the external document identified by base (an absolute URI
// with no fragment) into the current conversion state, so its nodes and references join the
// same [Registry]. It reports whether a document was newly loaded; a nil loader, a fetch
// error, or a parse error records the failure in st.loadErrs and returns false. Each base
// is attempted at most once (tracked in st.loaded), which also bounds the resolution
// worklist and breaks cross-document cycles.
func (st *convState) tryLoad(ctx context.Context, baseURI string) bool {
	if st.loaded[baseURI] {
		return false
	}
	st.loaded[baseURI] = true
	if st.loader == nil {
		return false
	}
	if err := ctx.Err(); err != nil {
		st.loadErrs[baseURI] = err
		return false
	}
	u, err := url.Parse(baseURI)
	if err != nil {
		st.loadErrs[baseURI] = errors.Wrapf(err, "parse %q", baseURI)
		return false
	}
	data, err := st.loader(ctx, u)
	if err != nil {
		st.loadErrs[baseURI] = err
		return false
	}
	if err := st.loadInto(ctx, data, baseURI); err != nil {
		st.loadErrs[baseURI] = err
		return false
	}
	return true
}

// loadInto parses data as a schema document rooted at base URI base and converts it into
// the shared conversion state, registering base as a resource so refs targeting it resolve.
func (st *convState) loadInto(ctx context.Context, data []byte, baseURI string) error {
	hs, refs, boolValue, err := buildHighSchema(ctx, data)
	if err != nil {
		return err
	}
	if boolValue != nil {
		n := &Node{Always: boolValue}
		st.reg.resources[baseURI] = n
		st.reg.pointers[baseURI+"\x00"] = n
		st.reg.nodes = append(st.reg.nodes, n)
		return nil
	}

	// The external document's stripped $refs merge into the shared refMap; *yaml.Node keys
	// are unique per parse, so the merge never collides.
	if st.refMap == nil {
		st.refMap = make(map[*yaml.Node]string)
	}
	maps.Copy(st.refMap, refs)

	sc := scope{frames: []frame{{baseURI: baseURI, root: ""}}}
	root, err := st.convertSchema(hs, sc)
	if err != nil {
		return errors.Wrapf(err, "convert %q", baseURI)
	}
	// Seed the retrieval URI as a resource (a declared root $id additionally registered its
	// own base URI during convertSchema).
	if _, ok := st.reg.resources[baseURI]; !ok {
		st.reg.resources[baseURI] = root
	}
	return nil
}
