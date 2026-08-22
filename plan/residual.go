package plan

// ResidualChecks is the runtime work a plan carries beyond its representation: every
// predicate in val that rep does not already imply.
//
// The validation plan is total (design §4.1) — acceptance is the checks and the dispatch,
// never the Go type — so a plan for `{"type":"string"}` carries a kind assertion even
// though the type it lowers to cannot hold anything else. A backend emitting validator
// code wants what is left after the type is chosen, and that is this. It is also the test
// design §22 decides [DirectGoType] by: a plan whose checks are all discharged needs no
// validator at all.
//
// Three kinds of check are discharged, and each restates what the representation was
// built from rather than adding to it:
//
//   - a bare kind assertion, against the kind the representation holds;
//   - a [NumericDomainPredicate] agreeing with a [PrimitiveRepresentation]'s own domain;
//   - an [ObjectStructurePredicate] or [ArrayStructurePredicate] describing the same
//     shape as the [ObjectRepresentation] or [ArrayRepresentation] beside it;
//   - a [ReferencePredicate] naming the same target as the [ReferenceRepresentation]
//     beside it.
//
// A structural predicate that does *not* describe the representation beside it is real
// work — a residual conjunct over a `$ref`, say, whose shape the reference's own type
// does not enforce.
func ResidualChecks(rep Representation, val ValidationPlan) []GuardedPredicate {
	var out []GuardedPredicate
	for _, gp := range val.Predicates {
		if dischargedBy(rep, gp.Expression) {
			continue
		}
		out = append(out, gp)
	}
	return out
}

// ResidualChecks is [ResidualChecks] over p's own representation and validation.
func (p CompilationPlan) ResidualChecks() []GuardedPredicate {
	return ResidualChecks(p.Representation, p.Validation)
}

func dischargedBy(rep Representation, e PredicateExpr) bool {
	switch e := e.(type) {
	case nil:
		return true
	case NumericDomainPredicate:
		prim, ok := rep.(PrimitiveRepresentation)
		return ok && prim.Numeric == e.Domain
	case ReferencePredicate:
		ref, ok := rep.(ReferenceRepresentation)
		return ok && ref.Name == e.Name
	case ObjectStructurePredicate:
		obj, ok := rep.(ObjectRepresentation)
		return ok && objectRestates(obj, e)
	case ArrayStructurePredicate:
		arr, ok := rep.(ArrayRepresentation)
		return ok && arrayRestates(arr, e)
	default:
		return false
	}
}

// objectRestates reports whether e describes the same object rep does, which is what makes
// it discharged rather than extra work.
//
// The sub-plans are not compared: the planner builds e from rep and shares them, so
// agreeing on the names, their presence and nullability, and the pattern list is enough to
// tell a restatement from a genuinely different shape.
func objectRestates(rep ObjectRepresentation, e ObjectStructurePredicate) bool {
	if len(rep.Fields) != len(e.Properties) || len(rep.PatternRules) != len(e.Patterns) {
		return false
	}
	if rep.Additional != e.Additional {
		return false
	}
	for i, f := range rep.Fields {
		c := e.Properties[i]
		if f.Name != c.Name || f.Presence != c.Presence || f.Nullable != c.Nullable {
			return false
		}
	}
	for i, r := range rep.PatternRules {
		if r.Pattern != e.Patterns[i].Pattern {
			return false
		}
	}
	return true
}

// arrayRestates is [objectRestates] for an array: the same tuple width, and the same
// answer to whether anything is admitted past it.
func arrayRestates(rep ArrayRepresentation, e ArrayStructurePredicate) bool {
	if len(rep.Prefix) != len(e.Prefix) {
		return false
	}
	return (rep.Rest.Plan.Representation != nil) == (e.Rest != nil)
}
