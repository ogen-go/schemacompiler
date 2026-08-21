// Package plan defines the analyzed compilation plan produced by schemacompiler
// and consumed by a code generator (ogen). All types here are pure data: they
// import neither the parser (libopenapi) nor the internal analysis packages.
//
// See docs/implementation.md and _ref/json-schema-to-go-design.md for rationale.
package plan

// CapabilityLevel ranks how far a schema can be lowered into a Go representation,
// from a direct type to constructs requiring runtime schema resolution (design §4.1).
//
// The levels are ordered: the capability of a composite is at least the maximum
// capability of its parts (design §22).
type CapabilityLevel uint8

const (
	// DirectGoType is a normal Go type that captures the accepted set with no residual check.
	DirectGoType CapabilityLevel = iota
	// GoTypeWithValidation has a statically known representation with residual predicates remaining.
	GoTypeWithValidation
	// StaticDispatch selects among finite alternatives with a structural discriminator.
	StaticDispatch
	// PredicateDispatch selects among known alternatives via predicate/match-count evaluation.
	PredicateDispatch
	// EvaluationStateValidation depends on evaluated properties/items
	// (unevaluatedProperties, unevaluatedItems).
	EvaluationStateValidation
	// DynamicSchemaResolution means the target schema depends on runtime dynamic scope ($dynamicRef).
	DynamicSchemaResolution
	// Unsupported means no sound conversion is available.
	Unsupported
)

// Exactness describes how faithfully the Go representation plus validator reproduces
// the schema's accepted set (design §25).
type Exactness uint8

const (
	// ExactPureRepresentation means the Go type alone is exact; no validator needed.
	ExactPureRepresentation Exactness = iota
	// ExactWithValidation means the Go type plus residual validator is exact. The Go type
	// alone may admit extra values — `any` for a bare `{"minLength":3}` no less than
	// `string` for `{"type":"string","minLength":3}` — as long as the residual validator
	// rejects them: exactness is a property of the plan's accepted set, not of the width
	// of its representation (design §24's biconditional, issue #95).
	ExactWithValidation
	// SoundOverApproximation means the plan's accepted set is a strict superset of the
	// schema's even after the residual validator runs, with the plan's own machinery
	// bounding the excess: today an asserted discriminator ([TagAsserted]), which trusts a
	// declared tag rather than proving the branches disjoint while every branch still
	// validates what it selects.
	SoundOverApproximation
	// DeclaredIncomplete means the plan admits extra values and nothing in it closes the
	// gap: a constraint was dropped because enforcing it would not have been sound, and no
	// residual validation catches the instances it would have rejected. Still lowerable —
	// the backend decides whether to generate a type that accepts more than the schema or
	// to refuse (issue #84).
	DeclaredIncomplete
	// UnsupportedConversion means no sound conversion is available.
	UnsupportedConversion
)
