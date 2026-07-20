package frontend

import (
	"encoding/json"
	"strconv"

	"github.com/go-faster/errors"
	"go.yaml.in/yaml/v4"
)

// valueFromNode converts a *yaml.Node (as produced by libopenapi for const/enum/default/
// examples) into a [Value], decoding it into Go-native form and rendering canonical JSON
// bytes for precision-sensitive consumers.
func valueFromNode(n *yaml.Node) (*Value, error) {
	if n == nil {
		return nil, nil
	}
	var decoded any
	if err := n.Decode(&decoded); err != nil {
		return nil, errors.Wrap(err, "decode value node")
	}
	raw, err := nodeToJSON(n)
	if err != nil {
		return nil, errors.Wrap(err, "render value node as JSON")
	}
	return &Value{Decoded: decoded, Raw: raw}, nil
}

// nodeToJSON renders a yaml.Node into canonical JSON bytes, preserving object key order
// and numeric source text where possible.
func nodeToJSON(n *yaml.Node) (json.RawMessage, error) {
	n = resolveAlias(n)
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return json.RawMessage("null"), nil
		}
		return nodeToJSON(n.Content[0])
	case yaml.ScalarNode:
		return scalarToJSON(n)
	case yaml.SequenceNode:
		buf := []byte{'['}
		for i, item := range n.Content {
			if i > 0 {
				buf = append(buf, ',')
			}
			b, err := nodeToJSON(item)
			if err != nil {
				return nil, err
			}
			buf = append(buf, b...)
		}
		buf = append(buf, ']')
		return buf, nil
	case yaml.MappingNode:
		buf := []byte{'{'}
		for i := 0; i+1 < len(n.Content); i += 2 {
			if i > 0 {
				buf = append(buf, ',')
			}
			key, err := json.Marshal(n.Content[i].Value)
			if err != nil {
				return nil, err
			}
			buf = append(buf, key...)
			buf = append(buf, ':')
			val, err := nodeToJSON(n.Content[i+1])
			if err != nil {
				return nil, err
			}
			buf = append(buf, val...)
		}
		buf = append(buf, '}')
		return buf, nil
	default:
		return nil, errors.Errorf("unsupported yaml node kind %d", n.Kind)
	}
}

func resolveAlias(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	return n
}

func scalarToJSON(n *yaml.Node) (json.RawMessage, error) {
	switch n.Tag {
	case "!!null":
		return json.RawMessage("null"), nil
	case "!!bool":
		b, err := strconv.ParseBool(n.Value)
		if err != nil {
			return nil, errors.Wrap(err, "parse bool scalar")
		}
		if b {
			return json.RawMessage("true"), nil
		}
		return json.RawMessage("false"), nil
	case "!!int", "!!float":
		// Preserve the source lexical form: it is already valid JSON number syntax.
		return json.RawMessage(n.Value), nil
	default:
		b, err := json.Marshal(n.Value)
		if err != nil {
			return nil, errors.Wrap(err, "marshal string scalar")
		}
		return b, nil
	}
}

// jsonPointerAppend appends an escaped JSON Pointer segment to a pointer path.
func jsonPointerAppend(pointer, segment string) string {
	return pointer + "/" + jsonPointerEscape(segment)
}

func jsonPointerEscape(segment string) string {
	buf := make([]byte, 0, len(segment))
	for _, r := range segment {
		switch r {
		case '~':
			buf = append(buf, '~', '0')
		case '/':
			buf = append(buf, '~', '1')
		default:
			buf = append(buf, string(r)...)
		}
	}
	return string(buf)
}
