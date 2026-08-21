package schemacompiler

import (
	"github.com/ogen-go/schemacompiler/internal/frontend"
	"github.com/ogen-go/schemacompiler/plan"
)

// annotateMetadata attaches the non-semantic annotations of n (and of its property
// sub-schemas) to an already-built plan. Metadata is dropped by ir.Compile on purpose —
// it carries no semantics — so it is re-attached here by walking the representation
// alongside the frontend AST it was built from.
func annotateMetadata(p *plan.CompilationPlan, n *frontend.Node) {
	if n == nil {
		return
	}
	p.Metadata = nodeMetadata(n)
	annotateRepresentation(p.Representation, n)
}

func annotateRepresentation(r plan.Representation, n *frontend.Node) {
	if n == nil {
		return
	}
	switch r := r.(type) {
	case plan.ObjectRepresentation:
		annotateFields(r, n)
	case plan.ArrayRepresentation:
		for i, item := range r.Prefix {
			if i < len(n.PrefixItems) {
				annotateRepresentation(item, n.PrefixItems[i])
			}
		}
		annotateRepresentation(r.Rest, n.Items)
	case plan.RecursiveRepresentation:
		annotateRepresentation(r.Body, n)
	}
}

// annotateFields fills in per-property metadata. An object representation may have been
// assembled from several conjoined schemas, so every allOf branch contributes too; a
// branch declaring the same property later wins.
func annotateFields(r plan.ObjectRepresentation, n *frontend.Node) {
	for _, branch := range n.AllOf {
		if branch != nil {
			annotateFields(r, branch)
		}
	}
	for _, prop := range n.Properties {
		f, ok := r.Fields[prop.Name]
		if !ok {
			continue
		}
		f.Metadata = nodeMetadata(prop.Schema)
		r.Fields[prop.Name] = f
		annotateRepresentation(f.Representation, prop.Schema)
	}
	annotateRepresentation(r.Additional, n.AdditionalProperties)
}

func nodeMetadata(n *frontend.Node) plan.Metadata {
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
