package ir

// AllPredicateDetails lists one zero value of every [PredicateDetail] variant.
//
// The list is what ties a new detail to the switches that must learn it: a test drives
// each entry through the planner, so a detail present here but missing from
// internal/planner's mapPredicate fails rather than silently vanishing from the plan
// (issue #64).
func AllPredicateDetails() []PredicateDetail {
	return []PredicateDetail{
		&DroppedKeywordDetail{},
		&MinLengthDetail{},
		&MaxLengthDetail{},
		&PatternDetail{},
		&FormatDetail{},
		&MinimumDetail{},
		&MaximumDetail{},
		&ExclusiveMinimumDetail{},
		&ExclusiveMaximumDetail{},
		&MultipleOfDetail{},
		&MinItemsDetail{},
		&MaxItemsDetail{},
		&UniqueItemsDetail{},
		&ContainsDetail{},
		&RequiredDetail{},
		&MinPropertiesDetail{},
		&MaxPropertiesDetail{},
		&DependentRequiredDetail{},
		&PropertyNamesDetail{},
	}
}
