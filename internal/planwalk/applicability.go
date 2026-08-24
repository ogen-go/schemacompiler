package planwalk

import "github.com/ogen-go/schemacompiler/plan"

// MeaningfulKinds returns the JSON kinds e says something about, and whether e is a known
// variant. A [plan.GuardedPredicate] whose Applicability reaches past this set fires on a
// kind its predicate cannot read: depending on how the consumer resolves that, it either
// rejects an instance the schema accepts — which design §24 forbids outright — or it is
// dead weight the plan claims as a check.
//
// Neither interpreter can catch that on its own. Both accept when a predicate meets a
// value it cannot read, which is the sound reading of a plan they are told to trust, so a
// guard widened past its keyword is invisible to them and to the differential harness
// built on them. It was invisible for `format` until issue #32, and this is the shape
// issue #60 tracks.
func MeaningfulKinds(e plan.PredicateExpr) (plan.KindSet, bool) {
	switch e.(type) {
	case *plan.MinLengthPredicate, *plan.MaxLengthPredicate, *plan.PatternPredicate:
		return plan.SetString, true

	case *plan.FormatPredicate:
		// `format` annotates a value of any kind (2020-12 §7.2.1). Only string formats
		// assert anything; the rest reach a backend through the representation.
		return plan.SetAny, true

	case *plan.MinimumPredicate, *plan.MaximumPredicate, *plan.MultipleOfPredicate,
		*plan.NumericDomainPredicate:
		return plan.SetNumber, true

	case *plan.MinItemsPredicate, *plan.MaxItemsPredicate, *plan.UniqueItemsPredicate,
		*plan.ContainsCountPredicate, *plan.ArrayStructurePredicate:
		return plan.SetArray, true

	case *plan.RequiredPredicate, *plan.MinPropertiesPredicate, *plan.MaxPropertiesPredicate,
		*plan.DependentRequiredPredicate, *plan.PropertyNamesPredicate,
		*plan.ObjectStructurePredicate:
		return plan.SetObject, true

	case *plan.ReferencePredicate, *plan.NegationPredicate, *plan.ShapePredicate:
		// Whole-instance obligations: the sub-plan decides which kinds it accepts.
		return plan.SetAny, true

	default:
		return 0, false
	}
}

// OverbroadGuard is one predicate whose guard is wider than [MeaningfulKinds] allows.
type OverbroadGuard struct {
	Predicate plan.PredicateExpr
	Guard     plan.KindSet
	Meaning   plan.KindSet
}

// OverbroadGuards returns every such predicate reachable from p.
func OverbroadGuards(p plan.CompilationPlan) []OverbroadGuard {
	return Fold(p, []OverbroadGuard(nil), func(acc []OverbroadGuard, n Node) ([]OverbroadGuard, Action) {
		if n.Edge.Kind != EdgeGuardedPredicate || n.Predicate == nil {
			return acc, Descend
		}
		meaning, known := MeaningfulKinds(n.Predicate)
		if known && n.Edge.Applicability&^meaning != 0 {
			acc = append(acc, OverbroadGuard{
				Predicate: n.Predicate,
				Guard:     n.Edge.Applicability,
				Meaning:   meaning,
			})
		}
		return acc, Descend
	})
}
