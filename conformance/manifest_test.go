package conformance

import "github.com/ogen-go/schemacompiler/plan"

// caseExpectation records what a corpus schema is expected to produce. Capability is
// always checked. wantKind, when set, asserts a diagnostic of that kind was produced —
// used for entries whose whole point is to exercise a capability downgrade or a
// flagged-but-representable construct. It replaces an exactness column that only ever
// restated the capability (design §25.1), and says more: which construct, and whether the
// plan still accepts what the schema does.
type caseExpectation struct {
	Capability plan.CapabilityLevel
	wantKind   plan.DiagnosticKind
}

func withKind(e caseExpectation, k plan.DiagnosticKind) caseExpectation {
	e.wantKind = k
	return e
}

// manifest maps each corpus schema's path (relative to testdata/corpus) to its
// expected capability and fidelity (design §4.1, §24-25). Every entry was verified
// against the live planner (internal/planner) rather than guessed, since the
// planner's classification rules (internal/planner/classify.go) are the ground
// truth this harness checks against.
var manifest = map[string]caseExpectation{
	// --- DirectGoType: a plain Go type, no residual validator needed. ---
	"direct/string.json":                       {Capability: plan.DirectGoType},
	"direct/integer.json":                      {Capability: plan.DirectGoType},
	"direct/boolean.json":                      {Capability: plan.DirectGoType},
	"direct/null.json":                         {Capability: plan.DirectGoType},
	"direct/number.json":                       {Capability: plan.DirectGoType},
	"direct/any.json":                          {Capability: plan.DirectGoType},
	"direct/array_items.json":                  {Capability: plan.DirectGoType},
	"direct/object_optional.json":              {Capability: plan.DirectGoType},
	"direct/pattern_properties.json":           {Capability: plan.DirectGoType},
	"direct/additional_properties_schema.json": {Capability: plan.DirectGoType},
	"direct/additional_properties_false.json":  {Capability: plan.DirectGoType},
	// A string format against a number type: the guard never fires, so nothing survives
	// onto the representation or into the validator.
	"direct/format_inapplicable_kind.json": {Capability: plan.DirectGoType},
	"array/tuple.json":                     {Capability: plan.DirectGoType},
	"array/tuple_items.json":               {Capability: plan.DirectGoType},

	// --- GoTypeWithValidation: static representation, residual predicate(s) remain. ---
	"validation/string_minlength.json":   {Capability: plan.GoTypeWithValidation},
	"validation/string_pattern.json":     {Capability: plan.GoTypeWithValidation},
	"validation/number_range.json":       {Capability: plan.GoTypeWithValidation},
	"validation/integer_multipleof.json": {Capability: plan.GoTypeWithValidation},
	"validation/array_minmax_items.json": {Capability: plan.GoTypeWithValidation},
	"validation/array_unique_items.json": {Capability: plan.GoTypeWithValidation},
	// `format` is a kind-guarded assertion (design §3): it reaches the representation and
	// the validation plan only for the kind it applies to.
	"validation/format_string.json": {Capability: plan.GoTypeWithValidation},
	"validation/format_number.json": {Capability: plan.GoTypeWithValidation},
	// {"format": "uuid"} accepts every non-string value, so the representation widens to
	// any and the guarded assertion is all that remains — which is exactly what the
	// schema accepts, so the wider representation costs no fidelity (issue #95).
	"validation/format_no_type.json": {Capability: plan.GoTypeWithValidation},
	// Two distinct formats intersected by allOf (unordered, design §11.5): neither shapes
	// the representation, both stay in the validation plan, and the loss is reported.
	"validation/format_allof_conflict.json": withKind(caseExpectation{Capability: plan.GoTypeWithValidation}, plan.DiagnosticAdvisory),
	// `required` always leaves a residual RequiredPredicate (internal/planner/representation.go).
	"validation/object_required.json": {Capability: plan.GoTypeWithValidation},
	// Three-state presence/nullable field: nullable is folded into the field's own
	// representation checks, but `required` still leaves a residual predicate.
	"object/nullable_field.json": {Capability: plan.GoTypeWithValidation},

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
	"ref/defs_simple.json": {Capability: plan.DirectGoType},
	// A guarded (instance-descent) recursive $ref: representable as a recursive Go type.
	"ref/recursive_guarded.json": {Capability: plan.DirectGoType},

	// --- PredicateDispatch: alternatives known, needs predicate/match-count eval. ---
	// Representable (kept as PredicateCountDispatch, design's v1 scope), flagged with
	// a SeverityWarning diagnostic — not silently downgraded to Unsupported.
	"dispatch/oneof_overlapping.json": withKind(caseExpectation{Capability: plan.PredicateDispatch}, plan.DiagnosticCost),
	"dispatch/anyof_overlapping.json": withKind(caseExpectation{Capability: plan.PredicateDispatch}, plan.DiagnosticCost),
	"array/contains.json":             withKind(caseExpectation{Capability: plan.PredicateDispatch}, plan.DiagnosticCost),
	"conditional/if_then_else.json":   withKind(caseExpectation{Capability: plan.PredicateDispatch}, plan.DiagnosticCost),
	// A negation that survives normalization has no representation and no v1 validator
	// predicate below PredicateDispatch: it is carried as a plan.NegationPredicate over a
	// whole sub-schema, which a backend either runs or refuses (design §11.8, §24).
	"direct/not_keyword.json": withKind(caseExpectation{Capability: plan.PredicateDispatch}, plan.DiagnosticCost),

	// --- EvaluationStateValidation: v1-Unsupported, no evaluated-annotation engine. ---
	"unsupported/unevaluated_properties.json": withKind(caseExpectation{Capability: plan.EvaluationStateValidation}, plan.DiagnosticUnsupported),
	"unsupported/unevaluated_items.json":      withKind(caseExpectation{Capability: plan.EvaluationStateValidation}, plan.DiagnosticUnsupported),

	// --- DynamicSchemaResolution: v1-Unsupported, no dynamic-scope resolution engine. ---
	"unsupported/dynamic_ref.json": withKind(caseExpectation{Capability: plan.DynamicSchemaResolution}, plan.DiagnosticUnsupported),

	// --- Unsupported: no sound conversion (an unguarded reference cycle). ---
	"unsupported/unguarded_recursion.json": withKind(caseExpectation{Capability: plan.Unsupported}, plan.DiagnosticUnsupported),
}
