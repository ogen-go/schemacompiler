package dump

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/ogen-go/schemacompiler/plan"
)

// writeMetadata renders m as indented lines, omitting every unset annotation. Extension
// keys are sorted so the dump stays deterministic.
func writeMetadata(t *tw, m plan.Metadata) {
	var g struct {
		Title       string
		Description string
		Deprecated  bool
		ReadOnly    bool
		WriteOnly   bool
		Default     []byte
		Examples    [][]byte
		XML         *plan.XMLMetadata
		Extensions  map[string]any
	} = m

	if g.Title != "" {
		t.line("title=%q", g.Title)
	}
	if g.Description != "" {
		t.line("description=%q", g.Description)
	}
	if g.Deprecated {
		t.line("deprecated=true")
	}
	if g.ReadOnly {
		t.line("readOnly=true")
	}
	if g.WriteOnly {
		t.line("writeOnly=true")
	}
	if len(g.Default) > 0 {
		t.line("default=%s", g.Default)
	}
	for i, e := range g.Examples {
		t.line("example[%d]=%s", i, e)
	}
	if x := g.XML; x != nil {
		var xg struct {
			Name      string
			Namespace string
			Prefix    string
			Attribute bool
			Wrapped   bool
		} = *x
		t.line("xml name=%q namespace=%q prefix=%q attribute=%v wrapped=%v",
			xg.Name, xg.Namespace, xg.Prefix, xg.Attribute, xg.Wrapped)
	}
	names := make([]string, 0, len(g.Extensions))
	for name := range g.Extensions {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t.line("extension %q=%s", name, extensionValue(g.Extensions[name]))
	}
}

func extensionValue(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
