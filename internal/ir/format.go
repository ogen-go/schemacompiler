package ir

import (
	"github.com/ogen-go/schemacompiler/internal/frontend"
	"github.com/ogen-go/schemacompiler/plan"
)

// numericFormats are the OpenAPI data-type formats whose applicable JSON kind is
// number. Every other `format` name applies to strings: JSON Schema 2020-12 defines
// `format` over string instances only, and design §3 lists it under String.
var numericFormats = map[string]struct{}{
	"int32":   {},
	"int64":   {},
	"float":   {},
	"double":  {},
	"decimal": {},
}

// formatKinds is the applicable kind set of one `format` name (design §3). The guard
// is per name — never the union of every kind some format could apply to, which would
// make `{"type":"number","format":"uuid"}` a uuid and reject the number 1 under
// `{"format":"uuid"}`.
func formatKinds(name string) plan.KindSet {
	if _, ok := numericFormats[name]; ok {
		return plan.SetNumber
	}
	return plan.SetString
}

func compileFormat(n *frontend.Node) []Expr {
	if n.Format == "" {
		return nil
	}
	return []Expr{Predicate{
		Guard:  formatKinds(n.Format),
		Detail: FormatDetail{Format: n.Format},
	}}
}
