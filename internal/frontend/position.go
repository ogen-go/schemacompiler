package frontend

import (
	"github.com/pb33f/libopenapi/datamodel/high/base"
	"go.yaml.in/yaml/v4"

	"github.com/ogen-go/schemacompiler/plan"
)

// file is the retrieval URI of the document being converted: the outermost frame's base
// URI, which a nested `$id` re-bases for reference resolution but does not move to
// another document.
func (sc scope) file() string {
	return sc.frames[0].baseURI
}

// nodePosition is the source position of yn within the document identified by file. A nil
// node (a schema reached through a path that kept no yaml node) degrades to the document
// identity alone.
func nodePosition(file string, yn *yaml.Node) plan.Position {
	yn = resolveAlias(yn)
	if yn == nil {
		return plan.Position{File: file}
	}
	return plan.Position{File: file, Line: yn.Line, Column: yn.Column}
}

// schemaPosition is the source position of an already-built high-level schema, recovered
// from the low-level model's root yaml node.
func schemaPosition(file string, hs *base.Schema) plan.Position {
	if hs == nil {
		return plan.Position{File: file}
	}
	low := hs.GoLow()
	if low == nil {
		return plan.Position{File: file}
	}
	return nodePosition(file, low.RootNode)
}

// keywordNode returns the value node of key within the mapping root, or nil.
func keywordNode(root *yaml.Node, key string) *yaml.Node {
	root = resolveAlias(root)
	if root == nil || root.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			return root.Content[i+1]
		}
	}
	return nil
}

func schemaKeywordNode(hs *base.Schema, key string) *yaml.Node {
	if hs == nil {
		return nil
	}
	low := hs.GoLow()
	if low == nil {
		return nil
	}
	return keywordNode(low.RootNode, key)
}
