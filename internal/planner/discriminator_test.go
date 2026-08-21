package planner

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/plan"
)

func TestRefMatchesTarget(t *testing.T) {
	for _, tt := range []struct {
		name   string
		ref    string
		target plan.SchemaID
		want   bool
	}{
		{"pointer reference", "#/components/schemas/Cat", "/components/schemas/Cat", true},
		{"pointer without fragment marker", "/components/schemas/Cat", "/components/schemas/Cat", true},
		{"component name", "Cat", "/components/schemas/Cat", true},
		{"other component", "Dog", "/components/schemas/Cat", false},
		{"other pointer", "#/components/schemas/Dog", "/components/schemas/Cat", false},
		{"external reference", "other.yaml#/Cat", "/components/schemas/Cat", false},
		{"unresolved raw reference", "#/components/schemas/Cat", "#/components/schemas/Cat", true},
		{"defs pointer", "#/$defs/Cat", "/$defs/Cat", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, refMatchesTarget(tt.ref, tt.target))
		})
	}
}
