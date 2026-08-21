package schemacompiler

import (
	"github.com/ogen-go/schemacompiler/internal/frontend"
	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/plan"
)

// annotateMetadata attaches the schema's own annotations to an already-built plan.
// Per-property, per-pattern and per-item annotations are threaded through the planner
// instead (see [ir.PropertyExpr]), so only the root node is left to attach here.
func annotateMetadata(p *plan.CompilationPlan, n *frontend.Node) {
	if n == nil {
		return
	}
	p.Metadata = ir.MetadataOf(n)
}
