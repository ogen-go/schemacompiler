package planner

import (
	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/plan"
)

// shapeKinds is the two kinds an [ir.ShapeDetail] can restrict, in the order their
// guarded predicates are emitted.
var shapeKinds = [...]plan.JSONKind{plan.KindObject, plan.KindArray}

// guardedShapes lowers the object/array shape keywords of an expression that carries no
// `type` restriction into kind-guarded predicates (design §3):
//
//	compileTypeConditionalKeyword(kind, predicate) =
//	    GuardedPredicate(guard = InstanceKindIs(kind), predicate = predicate)
//
// The predicate is the shape itself, carried as the sub-plan the same keywords would have
// produced under a sibling `type` — [builder.buildLeaf] for that one kind, which is
// exactly the sub-builder `{"type": "object", ...}` goes through. So the two spellings
// agree on the constraint and differ only in the enclosing representation, which stays Any
// because a non-object instance is still valid (design §12.1, §20.3).
//
// Only the shapes are carried down. Sibling predicates (`required`, `minProperties`,
// `minItems`, …) already reach the plan as their own [plan.GuardedPredicate] next to this
// one, so passing them in would enforce each of them twice.
func (b *builder) guardedShapes(c components, path string) ([]plan.GuardedPredicate, plan.CapabilityLevel, []plan.ResolutionPlan) {
	var (
		out      []plan.GuardedPredicate
		capLevel = plan.DirectGoType
		resParts []plan.ResolutionPlan
	)
	for _, kind := range shapeKinds {
		if !hasShape(c.shapes, kind) {
			continue
		}
		sub := b.buildLeaf(kind, components{shapes: c.shapes, numeric: plan.AnyNumber}, path)
		out = append(out, plan.GuardedPredicate{
			Applicability: kindBit(kind),
			Expression:    plan.ShapePredicate{Schema: sub},
		})
		capLevel = maxCapability(capLevel, sub.Capability)
		resParts = append(resParts, sub.Resolution)
	}
	return out, capLevel, resParts
}

// hasShape reports whether shapes carries a shape detail restricting kind.
func hasShape(shapes []ir.ShapeDetail, kind plan.JSONKind) bool {
	for _, sd := range shapes {
		switch sd.(type) {
		case ir.ObjectShape:
			if kind == plan.KindObject {
				return true
			}
		case ir.ArrayShape:
			if kind == plan.KindArray {
				return true
			}
		}
	}
	return false
}
