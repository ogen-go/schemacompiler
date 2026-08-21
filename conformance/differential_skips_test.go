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
// Counts at the time of writing:
//
//	#74    4 instances
//	#75    2 instances
//	#76    1 instances
//	#92    1 instances
//	#93    2 instances
var diffSkips = map[string]string{
	// #74 an integer keyword spelled as a decimal becomes 0
	"maxContains.json :: maxContains with contains, value with a decimal :: one element matches, valid maxContains": "#74",
	"maxItems.json :: maxItems validation with a decimal :: shorter is valid":                                       "#74",
	"maxLength.json :: maxLength validation with a decimal :: shorter is valid":                                     "#74",
	"maxProperties.json :: maxProperties validation with a decimal :: shorter is valid":                             "#74",

	// #75 minContains/maxContains without `contains` synthesize a match-count
	"maxContains.json :: maxContains without contains is ignored :: two items still valid against lone maxContains":  "#75",
	"minContains.json :: minContains without contains is ignored :: zero items still valid against lone minContains": "#75",

	// #76 a `$ref` key inside an enum value is consumed as a reference
	"ref.json :: naive replacement of $ref with its destination is not correct :: match the enum exactly": "#76",

	// #92 `$vocabulary` is ignored, so a keyword a custom metaschema removes is still
	// enforced. Uncovered by the #72 fix: the schema's `properties` used to be dropped
	// whole, so the `minimum` inside it never ran.
	"vocabulary.json :: schema that uses custom metaschema with with no validation vocabulary :: no validation: invalid number, but it still validates": "#92",

	// #93 a property named `$ref` is consumed as a reference, dropping `properties`
	"ref.json :: property named $ref that is not a reference :: property named $ref invalid":    "#93",
	"ref.json :: property named $ref, containing an actual $ref :: property named $ref invalid": "#93",
}
