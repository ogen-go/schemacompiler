package planner

import (
	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/plan"
)

// buildAll is the entry point for an ir.All (or a bare node wrapped as a one-element
// All): it flattens the sibling contributions and routes to the right sub-builder
// (design §21.1, §22).
func (b *builder) buildAll(all ir.All, path string) plan.CompilationPlan {
	k := ir.Expr(all).Kinds()
	if k == 0 {
		return b.neverPlanAt(path)
	}
	c := flattenAll(all.Operands)
	if c.never {
		return b.neverPlanAt(path)
	}

	switch {
	case len(c.refs) == 1 && len(c.shapes) == 0 && len(c.predicates) == 0 &&
		c.literal == nil && len(c.combinators) == 0 && len(c.nots) == 0:
		// The common case: a bare `$ref` (or `$dynamicRef`) with no sibling keywords.
		return b.build(c.refs[0], path)

	case len(c.refs) > 0:
		// A $ref combined with sibling constraints (e.g. allOf-merged): the planner does
		// not resolve the ref target here (that requires whole-document context owned by
		// the caller), so it cannot precisely intersect the two. Widen soundly: keep the
		// reference's representation, fold in local residual validation only.
		b.diag(path, plan.SeverityWarning, "$ref combined with sibling constraints is not precisely merged; widened")
		base := b.build(c.refs[0], path)
		c.refs = nil
		rest := b.buildKindRestricted(k, c, path)
		return mergePlans(base, rest)

	case len(c.combinators) >= 1:
		primary := c.combinators[0]
		rest := c
		rest.combinators = append([]ir.Expr{}, c.combinators[1:]...)
		return b.buildUnionWithContext(k, primary, rest, path)

	default:
		return b.buildKindRestricted(k, c, path)
	}
}
