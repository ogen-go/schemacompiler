package planwalk

import "github.com/ogen-go/schemacompiler/plan"

// AllRepresentations lists one zero value of every [plan.Representation] variant.
//
// The list is what ties a new variant to the switches that must learn it: a test drives
// each entry through [Representation] (and through the dump formatters), so a variant
// present here but missing from a switch fails rather than being rendered wrong.
func AllRepresentations() []plan.Representation {
	return []plan.Representation{
		plan.AnyRepresentation{},
		plan.NeverRepresentation{},
		plan.PrimitiveRepresentation{},
		plan.ObjectRepresentation{},
		plan.ArrayRepresentation{},
		plan.UnionRepresentation{},
		plan.RecursiveRepresentation{},
		plan.ReferenceRepresentation{},
	}
}

// AllDispatchPlans lists one zero value of every [plan.DispatchPlan] variant.
func AllDispatchPlans() []plan.DispatchPlan {
	return []plan.DispatchPlan{
		plan.NoDispatch{},
		plan.KindDispatch{},
		plan.LiteralDispatch{},
		plan.PropertyDispatch{},
		plan.PresenceDispatch{},
		plan.PredicateCountDispatch{},
	}
}

// AllPredicateExprs lists one zero value of every [plan.PredicateExpr] variant.
func AllPredicateExprs() []plan.PredicateExpr {
	return []plan.PredicateExpr{
		plan.MinLengthPredicate{},
		plan.MaxLengthPredicate{},
		plan.PatternPredicate{},
		plan.FormatPredicate{},
		plan.MinimumPredicate{},
		plan.MaximumPredicate{},
		plan.MultipleOfPredicate{},
		plan.MinItemsPredicate{},
		plan.MaxItemsPredicate{},
		plan.UniqueItemsPredicate{},
		plan.ContainsCountPredicate{},
		plan.NegationPredicate{},
		plan.RequiredPredicate{},
		plan.MinPropertiesPredicate{},
		plan.MaxPropertiesPredicate{},
		plan.DependentRequiredPredicate{},
		plan.PropertyNamesPredicate{},
	}
}

// AllResolutionPlans lists one zero value of every [plan.ResolutionPlan] variant.
func AllResolutionPlans() []plan.ResolutionPlan {
	return []plan.ResolutionPlan{
		plan.FullyResolved{},
		plan.StaticReferenceGraph{},
		plan.DynamicReferenceGraph{},
	}
}
