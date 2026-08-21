package planner

import "github.com/ogen-go/schemacompiler/plan"

func metadataEmpty(m plan.Metadata) bool {
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

	return g.Title == "" &&
		g.Description == "" &&
		!g.Deprecated &&
		!g.ReadOnly &&
		!g.WriteOnly &&
		len(g.Default) == 0 &&
		len(g.Examples) == 0 &&
		g.XML == nil &&
		len(g.Extensions) == 0
}
