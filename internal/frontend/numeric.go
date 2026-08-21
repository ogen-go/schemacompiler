package frontend

import (
	"math"
	"strconv"

	lowbase "github.com/pb33f/libopenapi/datamodel/low/base"
	"go.yaml.in/yaml/v4"
)

func readExclusiveBounds(low *lowbase.Schema) (exMin, exMax *float64) {
	if low == nil || low.RootNode == nil {
		return nil, nil
	}
	return readFloatKeyword(low.RootNode, "exclusiveMinimum"), readFloatKeyword(low.RootNode, "exclusiveMaximum")
}

// readFloatKeyword reads a numeric keyword directly from the source yaml node, bypassing
// libopenapi's exclusiveMinimum/exclusiveMaximum parsing: with no OpenAPI SpecIndex (as
// used by the standalone loader), libopenapi only recognizes integer-tagged scalars for
// these two keywords and silently drops float values (a libopenapi API surprise).
func readFloatKeyword(root *yaml.Node, key string) *float64 {
	v := resolveAlias(keywordNode(root, key))
	if v == nil || v.Kind != yaml.ScalarNode {
		return nil
	}
	f, err := strconv.ParseFloat(v.Value, 64)
	if err != nil {
		return nil
	}
	return &f
}

// countKeyword reads one of the eight keywords 2020-12 defines as a non-negative integer
// (maxLength, minLength, maxItems, minItems, maxContains, minContains, maxProperties,
// minProperties) from the source yaml node, falling back to libopenapi's parse only where
// no node is available (issue #74).
//
// The node is authoritative because libopenapi types these keywords as *int64 and only
// fills them from an integer-tagged scalar: `{"maxLength": 2.0}` is a legal schema meaning
// 2 — a JSON number is an integer whenever its value is one, however it is spelled — but
// arrives as a nil pointer, which [int64PtrToUint64Ptr] cannot tell from an authored 0.
// Synthesizing 0 there is the most restrictive bound there is, so it rejected instances the
// schema accepts (design §24).
//
// A value that is no non-negative integer at all makes the schema invalid; the keyword is
// left absent, which only widens what the plan accepts, and reported through
// [Schema.InvalidKeyword] so the author is not left guessing (design §25).
func (st *convState) countKeyword(sc scope, low *lowbase.Schema, key string, parsed *int64) *uint64 {
	var root *yaml.Node
	if low != nil {
		root = low.RootNode
	}
	vn := resolveAlias(keywordNode(root, key))
	if vn == nil {
		return int64PtrToUint64Ptr(parsed)
	}
	if v, ok := nonNegativeInteger(vn); ok {
		return &v
	}
	st.invalidKeyword = append(st.invalidKeyword, InvalidKeyword{
		Pointer:  jsonPointerAppend(sc.docPointer, key),
		Position: nodePosition(sc.file(), vn),
		Keyword:  key,
		Value:    scalarText(vn),
		Reason:   "expected a non-negative integer",
	})
	return nil
}

// nonNegativeInteger reads n as a non-negative integer, accepting every spelling whose
// value is one: `2`, `2.0` and `2e0` all mean 2.
func nonNegativeInteger(n *yaml.Node) (uint64, bool) {
	if n == nil || n.Kind != yaml.ScalarNode {
		return 0, false
	}
	switch n.Tag {
	case "!!int", "!!float":
	default:
		return 0, false
	}
	var u uint64
	if err := n.Decode(&u); err == nil {
		return u, true
	}
	var f float64
	if err := n.Decode(&f); err != nil {
		return 0, false
	}
	if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f != math.Trunc(f) || f >= float64(1<<64) {
		return 0, false
	}
	return uint64(f), true
}

// scalarText is the source text of a scalar node, for diagnostics.
func scalarText(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.ScalarNode {
		return ""
	}
	return n.Value
}

func int64PtrToUint64Ptr(p *int64) *uint64 {
	if p == nil || *p < 0 {
		return nil
	}
	v := uint64(*p)
	return &v
}
