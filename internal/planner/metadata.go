package planner

import "github.com/ogen-go/schemacompiler/plan"

func metadataEmpty(m plan.Metadata) bool {
	return m.Title == "" &&
		m.Description == "" &&
		!m.Deprecated &&
		!m.ReadOnly &&
		!m.WriteOnly &&
		len(m.Default) == 0 &&
		len(m.Examples) == 0 &&
		m.XML == nil &&
		len(m.Extensions) == 0
}
