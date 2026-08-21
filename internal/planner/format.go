package planner

import "github.com/ogen-go/schemacompiler/plan"

// pickFormat chooses the `format` that shapes the representation. allOf-composed
// siblings may declare several: they all stay in the validation plan, but only one can
// pick a Go type, so the first declared wins and the rest are reported.
func (b *builder) pickFormat(formats []string, path string) plan.Format {
	var chosen string
	seen := make(map[string]bool, len(formats))
	var extra []string
	for _, f := range formats {
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		if chosen == "" {
			chosen = f
			continue
		}
		extra = append(extra, f)
	}
	if len(extra) > 0 {
		b.diag(path, plan.SeverityInfo,
			"multiple formats declared; representation uses "+chosen+", others remain validation-only")
	}
	return plan.NewFormat(chosen)
}
