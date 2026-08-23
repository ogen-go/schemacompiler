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

// NumericDomainPredicate is the integrality half of `type: "integer"`: the value must lie
// in Domain. It is guarded on numbers, since a non-number is rejected by the kind
// assertion beside it rather than by this.
//
// `type` splits into two checks because [KindNumber] and [NumericDomain] are two axes
// (design §6): the kind assertion states that the instance is a number, this states which
// numbers. [AnyNumber] imposes nothing and is never emitted.
type NumericDomainPredicate struct{ Domain NumericDomain }

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

// NegationPredicate is a negation that survived normalization (design §11.8): the
// instance is valid only if it does NOT satisfy Schema.
//
// Lowering contract. A backend runs Schema (a full [CompilationPlan]) against the whole
// instance and inverts the outcome. Like [ContainsCountPredicate] this forces
// CapabilityLevel PredicateDispatch: a backend either emits the sub-schema check or MUST
// refuse and surface the diagnostic (docs/integration.md §4). Nothing about the stored
// shape changes; the surrounding representation stays an over-approximation that only
// this predicate narrows, which is what keeps the plan sound (design §24).
type NegationPredicate struct{ Schema CompilationPlan }

// ShapePredicate is a whole sub-plan the instance must satisfy, and is the positive
// counterpart of [NegationPredicate]: where that one inverts its sub-plan's verdict, this
// one takes it as it stands.
//
// The planner emits it for the type-specific shape keywords — `properties`,
// `patternProperties`, `additionalProperties`, `prefixItems`, `items` — written without a
// sibling `type` (design §3, §12.1). Such a keyword does not assert its own type, so the
// enclosing representation must stay broad enough to hold a non-object (non-array)
// instance; the shape survives here instead, under the enclosing
// [GuardedPredicate.Applicability] guard that design §3's `compileTypeConditionalKeyword`
// prescribes. Schema is built for exactly that one kind, so its representation is the
// [ObjectRepresentation] or [ArrayRepresentation] a sibling `type` would have produced.
//
// It also carries the conjuncts of an `allOf` a `$ref` cannot absorb (issue #78). The
// reference supplies the representation — a name, not a structure — so every other member
// of the intersection survives here instead of being dropped, guarded on [SetAny] since
// none of them is kind-conditional.
//
// Lowering contract. A backend runs Schema against the whole instance whenever the
// instance's kind is in the guard, and takes its verdict. Nothing about the stored shape
// changes: the enclosing representation stays the over-approximation, and only this
// predicate narrows it, which is what keeps the plan sound (design §24). Unlike
// [NegationPredicate] this does not force PredicateDispatch — checking a value against a
// shape is ordinary validation, so Schema's own capability is all it costs.
type ShapePredicate struct{ Schema CompilationPlan }

// PropertyCheck is one declared property of an [ObjectStructurePredicate]: the plan its
// value must satisfy, whether it may be absent, and whether an explicit null is admitted
// beside it (design §7.1).
type PropertyCheck struct {
	Name     string
	Plan     CompilationPlan
	Presence PresenceMode
	Nullable bool
}

// PatternCheck is one `patternProperties` rule of an [ObjectStructurePredicate]. Pattern
// is the raw ECMA-262 source, as [PatternPredicate.Regex] is.
type PatternCheck struct {
	Pattern string
	Plan    CompilationPlan
}

// ObjectStructurePredicate is `properties`, `patternProperties` and `additionalProperties`
// as a check rather than as storage. It is [ObjectRepresentation]'s counterpart in the
// validation plan, and the two are emitted together and describe the same shape.
//
// It exists because design §4.1 makes acceptance the validation plan's job alone. It
// cannot be expressed as a [ShapePredicate] over an object plan: that plan's own
// acceptance would live in *its* representation, so the check would be circular rather
// than independent.
//
// Lowering contract. A property is governed by the first [PropertyCheck] whose Name it
// equals; failing that, by every [PatternCheck] whose Pattern it matches; failing that, by
// Additional. A nil Additional admits any value, matching `additionalProperties` absent.
// Declared names are not also run through a matching pattern: the planner has already
// intersected every matching pattern schema into the property's own plan (design §12.3).
type ObjectStructurePredicate struct {
	Properties []PropertyCheck
	Patterns   []PatternCheck
	Additional *CompilationPlan
}

// ArrayStructurePredicate is `prefixItems` and `items` as a check rather than as storage,
// and is [ArrayRepresentation]'s counterpart for the same reason
// [ObjectStructurePredicate] is [ObjectRepresentation]'s.
//
// Lowering contract. Element i is governed by Prefix[i] where one exists and by Rest
// otherwise. A nil Rest rejects every element past the prefix, matching `items: false`;
// `items` absent is a Rest admitting any value.
type ArrayStructurePredicate struct {
	Prefix []CompilationPlan
	Rest   *CompilationPlan
}

// ReferencePredicate is a `$ref` as a check: the instance must satisfy the plan Name
// resolves to. It is [ReferenceRepresentation]'s counterpart in the validation plan, for
// the reason [ObjectStructurePredicate] is [ObjectRepresentation]'s — design §4.1 puts
// acceptance in the validation plan, and a reference's target is where the accepted set
// of a `$ref` comes from.
//
// The target is not inline. Name is resolved against the whole-document graph the root
// plan's [ResolutionPlan] carries (docs/integration.md §5), exactly as the representation
// of the same name is, so a reference cycle stays a cycle in the graph rather than an
// infinite plan.
type ReferencePredicate struct{ Name string }

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
//
// Lowering contract. Schema is a full [CompilationPlan] run once per key, so like
// [ContainsCountPredicate] — its element-wise counterpart — this forces CapabilityLevel
// PredicateDispatch: a backend either emits the per-key check or MUST refuse and surface
// the diagnostic (docs/integration.md §4). The keys it sees include every name the
// representation has no field for, which is why the plan also reports
// [Requirements.RawEvaluation] here.
type PropertyNamesPredicate struct{ Schema CompilationPlan }

func (MinLengthPredicate) isPredicateExpr()         {}
func (MaxLengthPredicate) isPredicateExpr()         {}
func (PatternPredicate) isPredicateExpr()           {}
func (FormatPredicate) isPredicateExpr()            {}
func (MinimumPredicate) isPredicateExpr()           {}
func (MaximumPredicate) isPredicateExpr()           {}
func (NumericDomainPredicate) isPredicateExpr()     {}
func (ReferencePredicate) isPredicateExpr()         {}
func (ObjectStructurePredicate) isPredicateExpr()   {}
func (ArrayStructurePredicate) isPredicateExpr()    {}
func (MultipleOfPredicate) isPredicateExpr()        {}
func (MinItemsPredicate) isPredicateExpr()          {}
func (MaxItemsPredicate) isPredicateExpr()          {}
func (UniqueItemsPredicate) isPredicateExpr()       {}
func (ContainsCountPredicate) isPredicateExpr()     {}
func (NegationPredicate) isPredicateExpr()          {}
func (RequiredPredicate) isPredicateExpr()          {}
func (MinPropertiesPredicate) isPredicateExpr()     {}
func (MaxPropertiesPredicate) isPredicateExpr()     {}
func (DependentRequiredPredicate) isPredicateExpr() {}
func (PropertyNamesPredicate) isPredicateExpr()     {}
func (ShapePredicate) isPredicateExpr()             {}
