package planner

import (
	"github.com/ogen-go/schemacompiler/internal/frontend"
	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/plan"
)

// buildRef builds the plan for a static $ref (design §10.1). The target's own plan is
// assembled elsewhere (by whichever caller walks every schema resource); here we only
// know the reference's identity and, via the registry, its recursion class (design
// §19). Enforces the v1 scope: unguarded recursion has no structural base case and is
// classified Unsupported.
func (b *builder) buildRef(v ir.Ref, path string) plan.CompilationPlan {
	capLevel := plan.DirectGoType
	class := frontend.NotRecursive
	if b.recur != nil {
		class = b.recur[v.Target]
	}
	if class == frontend.Unguarded {
		b.diag(path, plan.SeverityError,
			"unguarded recursive $ref: every cycle avoids an instance-descent edge, no structural base case")
		capLevel = plan.Unsupported
	}
	return plan.CompilationPlan{
		Representation: plan.ReferenceRepresentation{Name: string(v.Target)},
		Dispatch:       plan.NoDispatch{},
		Resolution:     plan.StaticReferenceGraph{},
		Capability:     capLevel,
	}
}

// buildDynamicRef enforces the v1 scope (docs/implementation.md): $dynamicRef targets
// depend on runtime dynamic scope and are not resolved in v1, so the plan widens to
// AnyRepresentation (design §24 invariant 4: never guess a narrow representation) and
// is classified Unsupported-adjacent DynamicSchemaResolution.
func (b *builder) buildDynamicRef(_ ir.DynamicRef, path string) plan.CompilationPlan {
	b.diag(path, plan.SeverityError, "$dynamicRef target depends on dynamic scope")
	return plan.CompilationPlan{
		Representation: plan.AnyRepresentation{},
		Dispatch:       plan.NoDispatch{},
		Resolution:     plan.DynamicReferenceGraph{},
		Capability:     plan.DynamicSchemaResolution,
	}
}
