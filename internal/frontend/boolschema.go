package frontend

import (
	"go.yaml.in/yaml/v4"
)

// booleanSchemaKeywords are the schema positions libopenapi types as a plain
// *SchemaProxy, so a boolean schema in one of them fails the build outright with
// "expected a single schema object". A boolean is a schema everywhere a schema is
// expected (2020-12 §4.3.2), so [expandBooleanSchemas] spells it as one before the build.
//
// `items`, `additionalProperties` and `unevaluatedProperties` are deliberately absent:
// libopenapi types those as DynamicValue[*SchemaProxy, bool] and reads the boolean
// natively, and the converter reads it back to close an object or a tuple. Rewriting them
// would hide that from the converter for no gain.
var booleanSchemaKeywords = map[string]struct{}{
	"contains":         {},
	"contentSchema":    {},
	"else":             {},
	"if":               {},
	"not":              {},
	"propertyNames":    {},
	"then":             {},
	"unevaluatedItems": {},
}

// expandBooleanSchemas rewrites `true` into `{}` and `false` into `{"not": {}}` wherever a
// keyword in [booleanSchemaKeywords] holds one. The two spellings accept and reject the
// same instances, so this changes what libopenapi can parse and nothing else.
//
// The walk is position-aware for the same reason [stripRefs] is: `not` and `if` are also
// ordinary property names, and inside `const`/`enum`/`default` a boolean is instance data.
// Rewriting one of those would corrupt the schema.
func expandBooleanSchemas(root *yaml.Node) {
	if root == nil {
		return
	}
	if root.Kind == yaml.DocumentNode {
		for _, c := range root.Content {
			expandBooleanSchemas(c)
		}
		return
	}
	if root.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i].Value
		pos, ok := keywordPositions[key]
		if !ok {
			continue
		}
		value := root.Content[i+1]
		switch pos {
		case posSchema:
			if _, rewritable := booleanSchemaKeywords[key]; rewritable {
				if n, ok := booleanSchemaNode(value); ok {
					root.Content[i+1] = n
					continue
				}
			}
			expandBooleanSchemas(value)
		case posSchemaList:
			if value.Kind == yaml.SequenceNode {
				for _, item := range value.Content {
					expandBooleanSchemas(item)
				}
			}
		case posSchemaMap:
			if value.Kind == yaml.MappingNode {
				for j := 0; j+1 < len(value.Content); j += 2 {
					expandBooleanSchemas(value.Content[j+1])
				}
			}
		}
	}
}

// booleanSchemaNode is the object spelling of a boolean schema, positioned where the
// boolean was so a diagnostic still points at what the author wrote.
func booleanSchemaNode(vn *yaml.Node) (*yaml.Node, bool) {
	value, ok := boolSchemaValue(vn)
	if !ok {
		return nil, false
	}
	n := resolveAlias(vn)
	empty := func() *yaml.Node {
		return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Line: n.Line, Column: n.Column}
	}
	if value {
		return empty(), true
	}
	return &yaml.Node{
		Kind:   yaml.MappingNode,
		Tag:    "!!map",
		Line:   n.Line,
		Column: n.Column,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "not", Line: n.Line, Column: n.Column},
			empty(),
		},
	}, true
}
