package frontend

import (
	"github.com/go-faster/errors"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// extensionsFromMap decodes `x-*` keywords into Go-native values. Keys are kept
// verbatim: the compiler never assigns meaning to a particular extension.
func extensionsFromMap(m *orderedmap.Map[string, *yaml.Node]) (map[string]any, error) {
	if m == nil || m.Len() == 0 {
		return nil, nil
	}
	out := make(map[string]any, m.Len())
	for name, node := range m.FromOldest() {
		v, err := valueFromNode(node)
		if err != nil {
			return nil, errors.Wrapf(err, "extension %q", name)
		}
		if v == nil {
			continue
		}
		out[name] = v.Decoded
	}
	return out, nil
}

func xmlFromSchema(x *base.XML) *XML {
	if x == nil {
		return nil
	}
	return &XML{
		Name:      x.Name,
		Namespace: x.Namespace,
		Prefix:    x.Prefix,
		Attribute: x.Attribute,
		Wrapped:   x.Wrapped,
	}
}
