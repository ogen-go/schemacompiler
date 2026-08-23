package nodetree

import "github.com/ogen-go/schemacompiler/plan"

// countBounds is a min/max pair folded into a structure predicate, which counts what it
// walks anyway. Held apart from a standalone node so the count comes from the structure's
// single pass rather than a second one over the same instance.
type countBounds struct {
	min, max       uint64
	hasMin, hasMax bool
}

func (c countBounds) ok(n uint64) bool {
	if c.hasMin && n < c.min {
		return false
	}
	return !c.hasMax || n <= c.max
}

// planFusion says which of a plan's sibling predicates another member absorbs, and what it
// absorbed. Siblings are conjoined, so a member whose answer another member already
// reaches can be folded into it without moving the accepted set.
type planFusion struct {
	skip         []bool
	objectBounds countBounds
	arrayBounds  countBounds
}

// newPlanFusion decides the folding for one plan node's predicate list.
//
// Two shapes are worth folding, both found by profiling the fast path. Design §4.1 has
// ObjectStructurePredicate restate what the representation stores, so a sibling
// RequiredPredicate walks the keys a second time to decide what the structure has already
// decided. And a count bound — `minItems`, `maxProperties` — walks the instance only to
// size it, which the structure beside it is sizing as it goes.
func newPlanFusion(preds []plan.GuardedPredicate) planFusion {
	f := planFusion{skip: make([]bool, len(preds))}
	object, hasObject := loneStructure(preds, isObjectStructure)
	array, hasArray := loneStructure(preds, isArrayStructure)

	// foldable reports whether the structure at idx walks the same instances gp does, so
	// gp's answer can come out of that walk.
	foldable := func(idx int, has bool, gp plan.GuardedPredicate) bool {
		return has && preds[idx].Applicability == gp.Applicability
	}

	for i, gp := range preds {
		if gp.Assert {
			// An assertion also rejects the kinds it does not apply to, which a guarded
			// structure does not do. Folding it would widen the plan.
			continue
		}
		switch e := gp.Expression.(type) {
		case plan.RequiredPredicate:
			f.skip[i] = requiredIsRestated(e, gp, preds)
		case plan.MinPropertiesPredicate:
			if foldable(object, hasObject, gp) && !f.objectBounds.hasMin {
				f.objectBounds.min, f.objectBounds.hasMin = e.Value, true
				f.skip[i] = true
			}
		case plan.MaxPropertiesPredicate:
			if foldable(object, hasObject, gp) && !f.objectBounds.hasMax {
				f.objectBounds.max, f.objectBounds.hasMax = e.Value, true
				f.skip[i] = true
			}
		case plan.MinItemsPredicate:
			if foldable(array, hasArray, gp) && !f.arrayBounds.hasMin {
				f.arrayBounds.min, f.arrayBounds.hasMin = e.Value, true
				f.skip[i] = true
			}
		case plan.MaxItemsPredicate:
			if foldable(array, hasArray, gp) && !f.arrayBounds.hasMax {
				f.arrayBounds.max, f.arrayBounds.hasMax = e.Value, true
				f.skip[i] = true
			}
		}
	}
	return f
}

func isObjectStructure(e plan.PredicateExpr) bool {
	_, ok := e.(plan.ObjectStructurePredicate)
	return ok
}

func isArrayStructure(e plan.PredicateExpr) bool {
	_, ok := e.(plan.ArrayStructurePredicate)
	return ok
}

// loneStructure returns the index of the only guarded structure matching is. With more
// than one there is no single walk to fold into, so nothing is folded.
func loneStructure(preds []plan.GuardedPredicate, is func(plan.PredicateExpr) bool) (int, bool) {
	found := -1
	for i, gp := range preds {
		if gp.Assert || !is(gp.Expression) {
			continue
		}
		if found >= 0 {
			return 0, false
		}
		found = i
	}
	return found, found >= 0
}

// requiredIsRestated reports whether some sibling object structure requires every name req
// does. The sibling has to guard the same kinds: a structure applying to fewer kinds than
// the required check cannot stand in for it.
func requiredIsRestated(req plan.RequiredPredicate, gp plan.GuardedPredicate, preds []plan.GuardedPredicate) bool {
	for _, other := range preds {
		structure, isStructure := other.Expression.(plan.ObjectStructurePredicate)
		if !isStructure || other.Assert || other.Applicability != gp.Applicability {
			continue
		}
		if structureRequires(structure, req.Properties) {
			return true
		}
	}
	return false
}

func structureRequires(s plan.ObjectStructurePredicate, names []string) bool {
	for _, name := range names {
		found := false
		for _, pc := range s.Properties {
			if pc.Name == name && pc.Presence == plan.PresenceRequired {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
