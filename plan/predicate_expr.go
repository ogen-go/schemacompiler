package plan

// This file defines the concrete [PredicateExpr] variants: residual, kind-guarded
// runtime checks the planner extracts from [ir] predicates (design §8, mirroring
// ir.PredicateDetail). Each carries typed operands so a backend can switch over them to
// emit validator code without re-parsing.

// MinLengthPredicate is `minLength`: a Unicode code-point length lower bound.
type MinLengthPredicate struct{ Value uint64 }

// MaxLengthPredicate is `maxLength`: a Unicode code-point length upper bound.
type MaxLengthPredicate struct{ Value uint64 }

// PatternPredicate is `pattern`; Regex is the raw ECMA-262 source.
type PatternPredicate struct{ Regex string }

// FormatPredicate is `format`.
type FormatPredicate struct{ Format string }

// MinimumPredicate is `minimum` (or `exclusiveMinimum` when Exclusive is set).
type MinimumPredicate struct {
	Value     float64
	Exclusive bool
}

// MaximumPredicate is `maximum` (or `exclusiveMaximum` when Exclusive is set).
type MaximumPredicate struct {
	Value     float64
	Exclusive bool
}

// MultipleOfPredicate is `multipleOf`.
type MultipleOfPredicate struct{ Value float64 }

// MinItemsPredicate is `minItems`.
type MinItemsPredicate struct{ Value uint64 }

// MaxItemsPredicate is `maxItems`.
type MaxItemsPredicate struct{ Value uint64 }

// UniqueItemsPredicate is `uniqueItems: true`: every pair of elements must be
// JSON-distinct.
type UniqueItemsPredicate struct{}

// ContainsCountPredicate is `contains`/`minContains`/`maxContains`: the number of array
// elements matching Schema must fall within [Min, Max] (Max nil means unbounded; Min
// defaults to 1 per spec when minContains is absent).
//
// Lowering contract. A backend runs Schema (a full [CompilationPlan]) against every array
// element and counts the elements that accept it. Letting n be that count, the instance is
// valid iff
//
//	Min <= n <= Max   (Max nil ⇒ no upper bound)
//
// This is the element-wise counterpart of [PredicateCountDispatch]'s branch match-count and,
// like it, forces CapabilityLevel PredicateDispatch: a backend either emits the count or
// MUST refuse and surface the diagnostic (docs/integration.md §4). The count is a
// validation step over the array's own representation; it does not change the stored shape.
type ContainsCountPredicate struct {
	Schema CompilationPlan
	Min    uint64
	Max    *uint64
}

// RequiredPredicate is `required`: every listed property must be present.
type RequiredPredicate struct{ Properties []string }

// MinPropertiesPredicate is `minProperties`.
type MinPropertiesPredicate struct{ Value uint64 }

// MaxPropertiesPredicate is `maxProperties`.
type MaxPropertiesPredicate struct{ Value uint64 }

// DependentRequiredEntry is one `dependentRequired` mapping: presence of Property
// requires presence of every name in Requires.
type DependentRequiredEntry struct {
	Property string
	Requires []string
}

// DependentRequiredPredicate is `dependentRequired`.
type DependentRequiredPredicate struct{ Entries []DependentRequiredEntry }

// PropertyNamesPredicate is `propertyNames`: Schema is evaluated against every own
// property name, interpreted as a JSON string.
type PropertyNamesPredicate struct{ Schema CompilationPlan }

func (MinLengthPredicate) isPredicateExpr()         {}
func (MaxLengthPredicate) isPredicateExpr()         {}
func (PatternPredicate) isPredicateExpr()           {}
func (FormatPredicate) isPredicateExpr()            {}
func (MinimumPredicate) isPredicateExpr()           {}
func (MaximumPredicate) isPredicateExpr()           {}
func (MultipleOfPredicate) isPredicateExpr()        {}
func (MinItemsPredicate) isPredicateExpr()          {}
func (MaxItemsPredicate) isPredicateExpr()          {}
func (UniqueItemsPredicate) isPredicateExpr()       {}
func (ContainsCountPredicate) isPredicateExpr()     {}
func (RequiredPredicate) isPredicateExpr()          {}
func (MinPropertiesPredicate) isPredicateExpr()     {}
func (MaxPropertiesPredicate) isPredicateExpr()     {}
func (DependentRequiredPredicate) isPredicateExpr() {}
func (PropertyNamesPredicate) isPredicateExpr()     {}
