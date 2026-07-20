package ir

// This file defines the concrete [PredicateDetail] variants: residual, kind-guarded
// assertions that survive semantic compilation (design §3, §11-13). Each carries its
// typed operand(s) so the planner (phase 4) can lower it to a [plan.PredicateExpr]
// without re-parsing.

// MinLengthDetail is `minLength` (string-guarded).
type MinLengthDetail struct{ Value uint64 }

// MaxLengthDetail is `maxLength` (string-guarded).
type MaxLengthDetail struct{ Value uint64 }

// PatternDetail is `pattern` (string-guarded); Regex is the raw ECMA-262 source.
type PatternDetail struct{ Regex string }

// FormatDetail is `format` (string-guarded); stubbed here, full semantics in phase 4.
type FormatDetail struct{ Format string }

// MinimumDetail is `minimum` (number-guarded).
type MinimumDetail struct{ Value float64 }

// MaximumDetail is `maximum` (number-guarded).
type MaximumDetail struct{ Value float64 }

// ExclusiveMinimumDetail is `exclusiveMinimum` (number-guarded).
type ExclusiveMinimumDetail struct{ Value float64 }

// ExclusiveMaximumDetail is `exclusiveMaximum` (number-guarded).
type ExclusiveMaximumDetail struct{ Value float64 }

// MultipleOfDetail is `multipleOf` (number-guarded).
type MultipleOfDetail struct{ Value float64 }

// MinItemsDetail is `minItems` (array-guarded).
type MinItemsDetail struct{ Value uint64 }

// MaxItemsDetail is `maxItems` (array-guarded).
type MaxItemsDetail struct{ Value uint64 }

// UniqueItemsDetail is `uniqueItems: true` (array-guarded); no operand.
type UniqueItemsDetail struct{}

// ContainsDetail is `contains`/`minContains`/`maxContains` (array-guarded). Schema is
// the compiled sub-expression each element is tested against; Min/Max are nil when the
// corresponding keyword is absent (default min is 1 per spec, applied by the planner).
type ContainsDetail struct {
	Schema Expr
	Min    *uint64
	Max    *uint64
}

// RequiredDetail is `required` (object-guarded): every listed property must be present.
// A single-property instance also serves as the `Has(p)` predicate used to desugar
// `dependentSchemas` (design §12.7).
type RequiredDetail struct{ Properties []string }

// MinPropertiesDetail is `minProperties` (object-guarded).
type MinPropertiesDetail struct{ Value uint64 }

// MaxPropertiesDetail is `maxProperties` (object-guarded).
type MaxPropertiesDetail struct{ Value uint64 }

// DependentRequiredEntry is one `dependentRequired` mapping: presence of Property
// requires presence of every name in Requires.
type DependentRequiredEntry struct {
	Property string
	Requires []string
}

// DependentRequiredDetail is `dependentRequired` (object-guarded).
type DependentRequiredDetail struct{ Entries []DependentRequiredEntry }

// PropertyNamesDetail is `propertyNames` (object-guarded): Schema is tested against
// every own property name, interpreted as a JSON string.
type PropertyNamesDetail struct{ Schema Expr }

func (MinLengthDetail) isPredicateDetail()         {}
func (MaxLengthDetail) isPredicateDetail()         {}
func (PatternDetail) isPredicateDetail()           {}
func (FormatDetail) isPredicateDetail()            {}
func (MinimumDetail) isPredicateDetail()           {}
func (MaximumDetail) isPredicateDetail()           {}
func (ExclusiveMinimumDetail) isPredicateDetail()  {}
func (ExclusiveMaximumDetail) isPredicateDetail()  {}
func (MultipleOfDetail) isPredicateDetail()        {}
func (MinItemsDetail) isPredicateDetail()          {}
func (MaxItemsDetail) isPredicateDetail()          {}
func (UniqueItemsDetail) isPredicateDetail()       {}
func (ContainsDetail) isPredicateDetail()          {}
func (RequiredDetail) isPredicateDetail()          {}
func (MinPropertiesDetail) isPredicateDetail()     {}
func (MaxPropertiesDetail) isPredicateDetail()     {}
func (DependentRequiredDetail) isPredicateDetail() {}
func (PropertyNamesDetail) isPredicateDetail()     {}
