package nodetree

import "github.com/ogen-go/schemacompiler/plan"

// withoutRestatedRequired drops a RequiredPredicate whose names a sibling
// ObjectStructurePredicate already marks required.
//
// Design §4.1 has the structure restate what the representation stores, so the two arrive
// side by side and each walks the instance's keys itself. The second walk decides nothing
// the first has not already decided: profiling the fast path, 18% of every ObjBytes call
// came from the restatement. Sibling predicates are conjoined, so dropping a member whose
// answer another member subsumes leaves the accepted set alone.
func withoutRestatedRequired(preds []plan.GuardedPredicate) []plan.GuardedPredicate {
	out := preds
	dropped := false
	for i, gp := range preds {
		req, isRequired := gp.Expression.(plan.RequiredPredicate)
		if !isRequired || gp.Assert {
			continue
		}
		if !requiredIsRestated(req, gp, preds) {
			continue
		}
		if !dropped {
			out = append(preds[:i:i], preds[i+1:]...)
			dropped = true
			continue
		}
		// A second drop indexes into preds, which out no longer mirrors; rebuild instead.
		return withoutRestatedRequired(out)
	}
	return out
}

// requiredIsRestated reports whether some sibling object structure requires every name req
// does. The sibling has to guard the same kinds: a structure that applies to fewer kinds
// than the required check cannot stand in for it.
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
