package conformance

import "github.com/ogen-go/schemacompiler/plan"

// caseExpectation records what a corpus schema is expected to produce. Capability
// is always checked; Exactness is checked only when nonZero is true, since some
// entries (e.g. plain "any") legitimately expect the zero value
// (plan.ExactPureRepresentation) while others simply don't pin exactness down.
type caseExpectation struct {
	Capability plan.CapabilityLevel
	Exactness  plan.Exactness
	checkExact bool
	// wantDiagnostic, when true, asserts at least one diagnostic (of any severity)
	// was produced — used for entries whose whole point is to exercise a
	// capability downgrade or a flagged-but-representable construct.
	wantDiagnostic bool
}

func exact(level plan.CapabilityLevel, ex plan.Exactness) caseExpectation {
	return caseExpectation{Capability: level, Exactness: ex, checkExact: true}
}

func withDiag(e caseExpectation) caseExpectation {
	e.wantDiagnostic = true
	return e
}

// manifest maps each corpus schema's path (relative to testdata/corpus) to its
// expected capability/exactness (design §4.1, §24-25). Every entry was verified
// against the live planner (internal/planner) rather than guessed, since the
// planner's classification rules (internal/planner/classify.go) are the ground
// truth this harness checks against.
var manifest = map[string]caseExpectation{
	// --- DirectGoType: a plain Go type, no residual validator needed. ---
	"direct/string.json":                       exact(plan.DirectGoType, plan.ExactPureRepresentation),
	"direct/integer.json":                      exact(plan.DirectGoType, plan.ExactPureRepresentation),
	"direct/boolean.json":                      exact(plan.DirectGoType, plan.ExactPureRepresentation),
	"direct/null.json":                         exact(plan.DirectGoType, plan.ExactPureRepresentation),
	"direct/number.json":                       exact(plan.DirectGoType, plan.ExactPureRepresentation),
	"direct/any.json":                          exact(plan.DirectGoType, plan.ExactPureRepresentation),
	"direct/array_items.json":                  exact(plan.DirectGoType, plan.ExactPureRepresentation),
	"direct/object_optional.json":              exact(plan.DirectGoType, plan.ExactPureRepresentation),
	"direct/pattern_properties.json":           exact(plan.DirectGoType, plan.ExactPureRepresentation),
	"direct/additional_properties_schema.json": exact(plan.DirectGoType, plan.ExactPureRepresentation),
	"direct/additional_properties_false.json":  exact(plan.DirectGoType, plan.ExactPureRepresentation),
	// `not` is representable structurally but the v1 validator does not enforce the
	// residual negation (design's v1 scope); it still counts as DirectGoType and
	// carries an informational diagnostic, not a capability downgrade.
	"direct/not_keyword.json": withDiag(exact(plan.DirectGoType, plan.ExactPureRepresentation)),
	"array/tuple.json":        exact(plan.DirectGoType, plan.ExactPureRepresentation),
	"array/tuple_items.json":  exact(plan.DirectGoType, plan.ExactPureRepresentation),

	// --- GoTypeWithValidation: static representation, residual predicate(s) remain. ---
	"validation/string_minlength.json":   exact(plan.GoTypeWithValidation, plan.ExactWithValidation),
	"validation/string_pattern.json":     exact(plan.GoTypeWithValidation, plan.ExactWithValidation),
	"validation/number_range.json":       exact(plan.GoTypeWithValidation, plan.ExactWithValidation),
	"validation/integer_multipleof.json": exact(plan.GoTypeWithValidation, plan.ExactWithValidation),
	"validation/array_minmax_items.json": exact(plan.GoTypeWithValidation, plan.ExactWithValidation),
	"validation/array_unique_items.json": exact(plan.GoTypeWithValidation, plan.ExactWithValidation),
	// `required` always leaves a residual RequiredPredicate (internal/planner/representation.go).
	"validation/object_required.json": exact(plan.GoTypeWithValidation, plan.ExactWithValidation),
	// Three-state presence/nullable field: nullable is folded into the field's own
	// representation checks, but `required` still leaves a residual predicate.
	"object/nullable_field.json": exact(plan.GoTypeWithValidation, plan.ExactWithValidation),

	// --- StaticDispatch: finite alternatives, discriminated at compile time. ---
	"enum/enum_strings.json": {Capability: plan.StaticDispatch},
	"enum/const_value.json":  {Capability: plan.StaticDispatch},
	// An enum with array/object members: these JSON values are not valid Go map keys, so
	// literal-dispatch dedup must not hash them (regression for an unhashable-type panic).
	"enum/non_primitive_enum.json":      {Capability: plan.StaticDispatch},
	"dispatch/oneof_kind_disjoint.json": {Capability: plan.StaticDispatch},
	"dispatch/oneof_tagged_union.json":  {Capability: plan.StaticDispatch},
	"dispatch/multi_type.json":          {Capability: plan.StaticDispatch},
	// A recursive oneOf whose branches are inlined (kind-tagged) vs. factored into named
	// $refs. Both must reach StaticDispatch: refs carry their target's kind summary, so
	// the factored form proves branch disjointness just like the inline form.
	"dispatch/recursive_union_inline.json":       {Capability: plan.StaticDispatch},
	"dispatch/recursive_union_ref_branches.json": {Capability: plan.StaticDispatch},
	// dependentSchemas desugars to a two-branch presence dispatch (design §12.7).
	"object/dependent_schemas.json": {Capability: plan.StaticDispatch},
	// A $ref to a sibling $defs entry with no residual predicate.
	"ref/defs_simple.json": exact(plan.DirectGoType, plan.ExactPureRepresentation),
	// A guarded (instance-descent) recursive $ref: representable as a recursive Go type.
	"ref/recursive_guarded.json": exact(plan.DirectGoType, plan.ExactPureRepresentation),

	// --- PredicateDispatch: alternatives known, needs predicate/match-count eval. ---
	// Representable (kept as PredicateCountDispatch, design's v1 scope), flagged with
	// a SeverityWarning diagnostic — not silently downgraded to Unsupported.
	"dispatch/oneof_overlapping.json": withDiag(caseExpectation{Capability: plan.PredicateDispatch}),
	"dispatch/anyof_overlapping.json": withDiag(caseExpectation{Capability: plan.PredicateDispatch}),
	"array/contains.json":             withDiag(caseExpectation{Capability: plan.PredicateDispatch}),
	"conditional/if_then_else.json":   withDiag(caseExpectation{Capability: plan.PredicateDispatch}),

	// --- EvaluationStateValidation: v1-Unsupported, no evaluated-annotation engine. ---
	"unsupported/unevaluated_properties.json": withDiag(exact(plan.EvaluationStateValidation, plan.UnsupportedConversion)),
	"unsupported/unevaluated_items.json":      withDiag(exact(plan.EvaluationStateValidation, plan.UnsupportedConversion)),

	// --- DynamicSchemaResolution: v1-Unsupported, no dynamic-scope resolution engine. ---
	"unsupported/dynamic_ref.json": withDiag(exact(plan.DynamicSchemaResolution, plan.UnsupportedConversion)),

	// --- Unsupported: no sound conversion (an unguarded reference cycle). ---
	"unsupported/unguarded_recursion.json": withDiag(exact(plan.Unsupported, plan.UnsupportedConversion)),
}
