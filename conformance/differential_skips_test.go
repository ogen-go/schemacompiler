package conformance

// diffSkips quarantines the plan/suite disagreements that are known compiler bugs, so
// TestPlanInterpreterDifferential stays green in CI while still running — and still
// reporting — every one of them. Each key is
// "<file> :: <schema description> :: <instance description>" as the suite spells them,
// and each value names the issue it waits on. Deleting an entry is how a fix is
// confirmed: the test fails when an entry stops firing.
//
// #73 had five entries and now has none: the planner carries a `not` as a
// plan.NegationPredicate where the negated sub-schema is exactly modeled, and declares the
// over-approximation where it is not. #73 stays open for the case where the `not` sits in a
// property subschema, whose one suite instance is "not.json :: forbidden property ::
// property present" above — it is quarantined under #72, because the enclosing `properties`
// is dropped before the `not` is ever reached, and it waits on #68/#72 rather than on
// anything about negation.
//
// Counts at the time of writing:
//
//	#72   70 instances
//	#68    4 instances
//	#74    4 instances
//	#75    2 instances
//	#76    1 instances
var diffSkips = map[string]string{
	// #72 object/array shape keywords are dropped when the schema has no sibling `type`
	"additionalProperties.json :: additionalProperties being false does not allow other properties :: an additional property is invalid":             "#72",
	"additionalProperties.json :: additionalProperties can exist by itself :: an additional invalid property is invalid":                             "#72",
	"additionalProperties.json :: additionalProperties does not look in applicators :: properties defined in allOf are not examined":                 "#72",
	"additionalProperties.json :: additionalProperties with schema :: an additional invalid property is invalid":                                     "#72",
	"additionalProperties.json :: non-ASCII pattern with additionalProperties :: not matching the pattern is invalid":                                "#72",
	"dynamicRef.json :: $dynamicRef points to a boolean schema :: follow $dynamicRef to a false schema":                                              "#72",
	"items.json :: a schema given for items :: wrong type of items":                                                                                  "#72",
	"items.json :: items does not look in applicators, valid case :: prefixItems in allOf does not constrain items, invalid case":                    "#72",
	"items.json :: items with boolean schema (false) :: any non-empty array is invalid":                                                              "#72",
	"items.json :: items with heterogeneous array :: heterogeneous invalid instance":                                                                 "#72",
	"items.json :: prefixItems validation adjusts the starting index for items :: wrong type of second item":                                         "#72",
	"items.json :: prefixItems with no additional items allowed :: additional items are not permitted":                                               "#72",
	"not.json :: forbidden property :: property present":                                                                                             "#72",
	"optional/non-bmp-regex.json :: Proper UTF-16 surrogate pair handling: patternProperties :: doesn't match one":                                   "#72",
	"optional/non-bmp-regex.json :: Proper UTF-16 surrogate pair handling: patternProperties :: doesn't match two":                                   "#72",
	"optional/refOfUnknownKeyword.json :: reference of a root arbitrary keyword  :: mismatch":                                                        "#72",
	"optional/refOfUnknownKeyword.json :: reference of a root arbitrary keyword with encoded ref :: mismatch":                                        "#72",
	"optional/refOfUnknownKeyword.json :: reference of an arbitrary keyword of a sub-schema :: mismatch":                                             "#72",
	"optional/refOfUnknownKeyword.json :: reference of an arbitrary keyword of a sub-schema with encoded ref :: mismatch":                            "#72",
	"patternProperties.json :: multiple simultaneous patternProperties are validated :: an invalid due to both is invalid":                           "#72",
	"patternProperties.json :: multiple simultaneous patternProperties are validated :: an invalid due to one is invalid":                            "#72",
	"patternProperties.json :: multiple simultaneous patternProperties are validated :: an invalid due to the other is invalid":                      "#72",
	"patternProperties.json :: patternProperties validates properties matching a regex :: a single invalid match is invalid":                         "#72",
	"patternProperties.json :: patternProperties validates properties matching a regex :: multiple invalid matches is invalid":                       "#72",
	"patternProperties.json :: patternProperties with boolean schemas :: object with a property matching both true and false is invalid":             "#72",
	"patternProperties.json :: patternProperties with boolean schemas :: object with both properties is invalid":                                     "#72",
	"patternProperties.json :: patternProperties with boolean schemas :: object with property matching schema false is invalid":                      "#72",
	"patternProperties.json :: regexes are not anchored by default and are case sensitive :: recognized members are accounted for":                   "#72",
	"patternProperties.json :: regexes are not anchored by default and are case sensitive :: regexes are case sensitive, 2":                          "#72",
	"prefixItems.json :: a schema given for prefixItems :: wrong types":                                                                              "#72",
	"prefixItems.json :: prefixItems with boolean schemas :: array with two items is invalid":                                                        "#72",
	"properties.json :: object properties validation :: both properties invalid is invalid":                                                          "#72",
	"properties.json :: object properties validation :: one property invalid is invalid":                                                             "#72",
	"properties.json :: properties whose names are Javascript object property names :: __proto__ not valid":                                          "#72",
	"properties.json :: properties whose names are Javascript object property names :: constructor not valid":                                        "#72",
	"properties.json :: properties whose names are Javascript object property names :: toString not valid":                                           "#72",
	"properties.json :: properties with boolean schema :: both properties present is invalid":                                                        "#72",
	"properties.json :: properties with boolean schema :: only 'false' property present is invalid":                                                  "#72",
	"properties.json :: properties with escaped characters :: object with strings is invalid":                                                        "#72",
	"properties.json :: properties, patternProperties, additionalProperties interaction :: additionalProperty invalidates others":                    "#72",
	"properties.json :: properties, patternProperties, additionalProperties interaction :: patternProperty invalidates nonproperty":                  "#72",
	"properties.json :: properties, patternProperties, additionalProperties interaction :: patternProperty invalidates property":                     "#72",
	"properties.json :: properties, patternProperties, additionalProperties interaction :: property invalidates property":                            "#72",
	"ref.json :: URN base URI with NSS :: a non-string is invalid":                                                                                   "#72",
	"ref.json :: URN base URI with URN and JSON pointer ref :: a non-string is invalid":                                                              "#72",
	"ref.json :: URN base URI with URN and anchor ref :: a non-string is invalid":                                                                    "#72",
	"ref.json :: URN base URI with q-component :: a non-string is invalid":                                                                           "#72",
	"ref.json :: URN base URI with r-component :: a non-string is invalid":                                                                           "#72",
	"ref.json :: escaped pointer ref :: percent invalid":                                                                                             "#72",
	"ref.json :: escaped pointer ref :: slash invalid":                                                                                               "#72",
	"ref.json :: escaped pointer ref :: tilde invalid":                                                                                               "#72",
	"ref.json :: property named $ref that is not a reference :: property named $ref invalid":                                                         "#72",
	"ref.json :: property named $ref, containing an actual $ref :: property named $ref invalid":                                                      "#72",
	"ref.json :: ref applies alongside sibling keywords :: ref invalid":                                                                              "#72",
	"ref.json :: ref applies alongside sibling keywords :: ref valid, maxItems invalid":                                                              "#72",
	"ref.json :: refs with quote :: object with strings is invalid":                                                                                  "#72",
	"ref.json :: refs with relative uris and defs :: invalid on inner field":                                                                         "#72",
	"ref.json :: refs with relative uris and defs :: invalid on outer field":                                                                         "#72",
	"ref.json :: relative pointer ref to array :: mismatch array":                                                                                    "#72",
	"ref.json :: relative pointer ref to object :: mismatch":                                                                                         "#72",
	"ref.json :: relative refs with absolute uris and defs :: invalid on inner field":                                                                "#72",
	"ref.json :: relative refs with absolute uris and defs :: invalid on outer field":                                                                "#72",
	"ref.json :: root pointer ref :: mismatch":                                                                                                       "#72",
	"ref.json :: root pointer ref :: recursive mismatch":                                                                                             "#72",
	"ref.json :: simple URN base URI with JSON pointer :: a non-string is invalid":                                                                   "#72",
	"refRemote.json :: base URI change :: base URI change ref invalid":                                                                               "#72",
	"refRemote.json :: retrieved nested refs resolve relative to their URI not $id :: number is invalid":                                             "#72",
	"unevaluatedProperties.json :: property is evaluated in an uncle schema to unevaluatedProperties :: uncle keyword evaluation is not significant": "#72",
	"uniqueItems.json :: uniqueItems=false with an array of items and additionalItems=false :: extra items are invalid even if unique":               "#72",
	"vocabulary.json :: schema that uses custom metaschema with with no validation vocabulary :: applicator vocabulary still works":                  "#72",

	// #68 nested subschema plans are dropped from Field/Item/PatternField representations
	"default.json :: the default keyword does not do anything if the property is missing :: an explicit property value is checked against maximum (failing)": "#68",
	"enum.json :: enums in properties :: wrong bar value": "#68",
	"enum.json :: enums in properties :: wrong foo value": "#68",
	"infinite-loop-detection.json :: evaluating the same schema location against the same data location twice is not a sign of an infinite loop :: failing case": "#68",

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
}
