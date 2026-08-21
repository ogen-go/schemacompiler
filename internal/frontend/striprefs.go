package frontend

import (
	"go.yaml.in/yaml/v4"
)

// keywordPosition describes how a keyword's value node relates to schemas, which decides
// whether [stripRefs] may descend into it.
type keywordPosition uint8

const (
	// posSchema: the value node is itself a schema.
	posSchema keywordPosition = iota
	// posSchemaList: the value node is an array whose elements are schemas.
	posSchemaList
	// posSchemaMap: the value node is an object whose *keys are instance data* (property
	// names, patterns, definition names) and whose values are schemas.
	posSchemaMap
)

// keywordPositions maps every keyword libopenapi's low/base.Schema builder treats as a
// schema-bearing position. A keyword absent from this table carries no schema anywhere
// beneath it, so its contents are instance data and must never be interpreted: `const`,
// `enum`, `default`, `examples`, `x-` extensions and unknown keywords all land here.
var keywordPositions = map[string]keywordPosition{
	"additionalProperties":  posSchema,
	"contains":              posSchema,
	"contentSchema":         posSchema,
	"else":                  posSchema,
	"if":                    posSchema,
	"items":                 posSchema,
	"not":                   posSchema,
	"propertyNames":         posSchema,
	"then":                  posSchema,
	"unevaluatedItems":      posSchema,
	"unevaluatedProperties": posSchema,

	"allOf":       posSchemaList,
	"anyOf":       posSchemaList,
	"oneOf":       posSchemaList,
	"prefixItems": posSchemaList,

	"$defs":             posSchemaMap,
	"dependentSchemas":  posSchemaMap,
	"patternProperties": posSchemaMap,
	"properties":        posSchemaMap,
}

// stripRefs walks root as a *schema* position, extracting every `$ref` key/value pair into
// refs (keyed by the exact *yaml.Node it was removed from) and deleting it from the node's
// Content.
//
// This is done so libopenapi never sees a `$ref`: low/base.Schema.Build auto-follows any
// node containing a literal `$ref` key (regardless of sibling keywords), replacing the
// node in place with its resolved target via the low-level index/rolodex — machinery this
// package deliberately bypasses in favor of its own resolver (design §10). Stripping
// `$ref` upfront, recursively, lets libopenapi build every sibling keyword normally
// (matching JSON Schema 2020-12, where `$ref` coexists with other keywords) while we
// recover the reference string ourselves from the map.
//
// The walk is position-aware rather than a blind tree walk: `$ref` is a keyword only where
// a schema is expected. Under `properties`, `patternProperties`, `$defs` and
// `dependentSchemas` the map keys are instance data, and inside `const`/`enum`/`default`/
// `examples` the whole value is. A `$ref` in either place is ordinary data and must
// survive untouched.
//
// `$dynamicRef` needs no such treatment: libopenapi stores it verbatim without ever
// attempting to follow it.
func stripRefs(root *yaml.Node, refs map[*yaml.Node]string) {
	if root == nil {
		return
	}
	if root.Kind == yaml.DocumentNode {
		for _, c := range root.Content {
			stripRefs(c, refs)
		}
		return
	}
	if root.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "$ref" {
			refs[root] = root.Content[i+1].Value
			root.Content = append(root.Content[:i], root.Content[i+2:]...)
			i -= 2
		}
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		pos, ok := keywordPositions[root.Content[i].Value]
		if !ok {
			continue
		}
		value := root.Content[i+1]
		switch pos {
		case posSchema:
			stripRefs(value, refs)
		case posSchemaList:
			if value.Kind == yaml.SequenceNode {
				for _, item := range value.Content {
					stripRefs(item, refs)
				}
			}
		case posSchemaMap:
			if value.Kind == yaml.MappingNode {
				for j := 0; j+1 < len(value.Content); j += 2 {
					stripRefs(value.Content[j+1], refs)
				}
			}
		}
	}
}
