package conformance

// diffSkips quarantines the plan/suite disagreements that are known compiler bugs, so
// TestPlanInterpreterDifferential stays green in CI while still running — and still
// reporting — every one of them. Each key is
// "<file> :: <schema description> :: <instance description>" as the suite spells them,
// and each value names the issue it waits on. Deleting an entry is how a fix is
// confirmed: the test fails when an entry stops firing.
//
// #72 had 71 entries and now has none: object and array shape keywords written without a
// sibling `type` survive as kind-guarded plan.ShapePredicates, so the shape is enforced
// for an object or array instance and stays vacuous for every other kind. Two of its
// entries moved to #93 and one instance of vocabulary.json moved to #92 — neither is about
// the missing shape, both were hidden behind it.
//
// #73 had five entries and now has none: the planner carries a `not` as a
// plan.NegationPredicate where the negated sub-schema is exactly modeled, and declares the
// over-approximation where it is not.
//
// #68 had four entries and now has none: an object field, an array item and a pattern rule
// each carry the whole sub-plan of the schema written there, so its validation and dispatch
// survive.
//
// #93 had two entries and #76 one, and both now have none: the frontend strips `$ref` by
// schema position rather than by a blind tree walk, so a `$ref` written where the document
// holds instance data — a `properties`/`patternProperties`/`$defs`/`dependentSchemas` key,
// or anywhere inside a `const`/`enum` value — stays data.
//
// #74 had eight entries and now has none: the eight keywords 2020-12 defines as
// non-negative integers are read from the source yaml node, so a decimal-spelled integer
// (`maxLength: 2.0`) reads as its integer value, and a value that is no non-negative
// integer at all leaves the keyword absent with a diagnostic instead of synthesizing a
// bound of 0.
//
// #95 added four entries: a plan whose representation is `any` and a plan classified
// PredicateDispatch both used to report SoundOverApproximation, which the oracle exempts,
// so nothing held them to anything. They report ExactWithValidation now, which made the
// `min*` half of #74 visible — nine disagreements in all, of which #98 and #99 fixed five
// outright before they could be quarantined.
//
// Counts at the time of writing:
//
//	#92    1 instances
var diffSkips = map[string]string{
	// #92 `$vocabulary` is ignored, so a keyword a custom metaschema removes is still
	// enforced. Uncovered by the #72 fix: the schema's `properties` used to be dropped
	// whole, so the `minimum` inside it never ran.
	"vocabulary.json :: schema that uses custom metaschema with with no validation vocabulary :: no validation: invalid number, but it still validates": "#92",
}
