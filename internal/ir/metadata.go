package ir

import (
	"bytes"

	"github.com/ogen-go/schemacompiler/internal/frontend"
	"github.com/ogen-go/schemacompiler/plan"
)

// MetadataOf extracts the non-semantic annotations of n. Every value it returns is a
// deep copy: the plan is public, so a consumer mutating it must not reach back into the
// compiler's own AST.
func MetadataOf(n *frontend.Node) plan.Metadata {
	if n == nil {
		return plan.Metadata{}
	}
	m := plan.Metadata{
		Title:       n.Title,
		Description: n.Description,
		Deprecated:  n.Deprecated,
		ReadOnly:    n.ReadOnly,
		WriteOnly:   n.WriteOnly,
		Extensions:  copyExtensions(n.Extensions),
	}
	if n.Default != nil {
		m.Default = bytes.Clone(n.Default.Raw)
	}
	for _, e := range n.Examples {
		m.Examples = append(m.Examples, bytes.Clone(e.Raw))
	}
	if x := n.XML; x != nil {
		m.XML = &plan.XMLMetadata{
			Name:      x.Name,
			Namespace: x.Namespace,
			Prefix:    x.Prefix,
			Attribute: x.Attribute,
			Wrapped:   x.Wrapped,
		}
	}
	return m
}

func copyExtensions(ext map[string]any) map[string]any {
	if ext == nil {
		return nil
	}
	out := make(map[string]any, len(ext))
	for k, v := range ext {
		out[k] = copyExtensionValue(v)
	}
	return out
}

func copyExtensionValue(v any) any {
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			out[k] = copyExtensionValue(e)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = copyExtensionValue(e)
		}
		return out
	case []byte:
		return bytes.Clone(v)
	default:
		return v
	}
}
