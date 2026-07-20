package frontend

import (
	"context"

	"github.com/go-faster/errors"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	lowbase "github.com/pb33f/libopenapi/datamodel/low/base"
	"go.yaml.in/yaml/v4"
)

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

// Load parses a standalone Draft 2020-12 schema document (JSON or YAML) into the
// internal AST. libopenapi's NewDocument is OpenAPI-only, so this uses the low-level
// build path (yaml.Node -> lowbase.Build -> base.NewSchema).
func Load(ctx context.Context, data []byte, baseURI string) (*Schema, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := loadDocument(data)
	if err != nil {
		return nil, err
	}

	if b, ok := boolSchemaValue(root); ok {
		reg := newRegistry()
		n := &Node{Always: &b}
		reg.resources[baseURI] = n
		reg.nodes = append(reg.nodes, n)
		reg.analyzeSCCs()
		return &Schema{Registry: reg, Root: n}, nil
	}

	refs := make(map[*yaml.Node]string)
	stripRefs(root, refs)

	low := new(lowbase.Schema)
	if err := low.Build(ctx, root, nil); err != nil {
		return nil, errors.Wrap(err, "build schema")
	}
	hs := base.NewSchema(low)

	return convertRoot(ctx, hs, refs, baseURI)
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
	return convertRoot(ctx, s, nil, baseURI)
}
