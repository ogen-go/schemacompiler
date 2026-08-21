package planner

import (
	"sort"
	"strings"

	"github.com/ogen-go/schemacompiler/plan"
)

// pickFormat chooses the `format` annotation carried by the representation. allOf
// composition is an intersection (design §11.5) and therefore unordered, so distinct
// names have no canonical winner: the representation carries none of them and every
// one stays in the validation plan (design §24 sound over-approximation).
func (b *builder) pickFormat(formats []string, path string) string {
	seen := make(map[string]bool, len(formats))
	var names []string
	for _, f := range formats {
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		names = append(names, f)
	}
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	sort.Strings(names)
	b.diag(path+"/format", plan.SeverityInfo,
		"conflicting formats ("+strings.Join(names, ", ")+") composed by an unordered intersection; "+
			"the representation carries none, all remain validation-only")
	return ""
}
