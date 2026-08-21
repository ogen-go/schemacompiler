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
	if m.Title != "" {
		t.line("title=%q", m.Title)
	}
	if m.Description != "" {
		t.line("description=%q", m.Description)
	}
	if m.Deprecated {
		t.line("deprecated=true")
	}
	if m.ReadOnly {
		t.line("readOnly=true")
	}
	if m.WriteOnly {
		t.line("writeOnly=true")
	}
	if len(m.Default) > 0 {
		t.line("default=%s", m.Default)
	}
	for i, e := range m.Examples {
		t.line("example[%d]=%s", i, e)
	}
	if x := m.XML; x != nil {
		t.line("xml name=%q namespace=%q prefix=%q attribute=%v wrapped=%v",
			x.Name, x.Namespace, x.Prefix, x.Attribute, x.Wrapped)
	}
	names := make([]string, 0, len(m.Extensions))
	for name := range m.Extensions {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t.line("extension %q=%s", name, extensionValue(m.Extensions[name]))
	}
}

func extensionValue(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
