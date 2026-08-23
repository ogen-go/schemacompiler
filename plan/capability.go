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
