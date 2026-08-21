package ir

import (
	"github.com/ogen-go/schemacompiler/internal/frontend"
	"github.com/ogen-go/schemacompiler/plan"
)

// MetadataOf extracts the non-semantic annotations of n.
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
		Extensions:  n.Extensions,
	}
	if n.Default != nil {
		m.Default = n.Default.Raw
	}
	for _, e := range n.Examples {
		m.Examples = append(m.Examples, e.Raw)
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
