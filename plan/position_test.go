package plan_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/plan"
)

func TestPositionString(t *testing.T) {
	for _, tc := range []struct {
		name string
		pos  plan.Position
		want string
		zero bool
	}{
		{"zero", plan.Position{}, "", true},
		{"file only", plan.Position{File: "schema.json"}, "schema.json", false},
		{"file and line", plan.Position{File: "schema.json", Line: 3}, "schema.json:3", false},
		{"full", plan.Position{File: "schema.json", Line: 3, Column: 7}, "schema.json:3:7", false},
		{"no file", plan.Position{Line: 3, Column: 7}, "3:7", false},
		{"column without line", plan.Position{File: "schema.json", Column: 7}, "schema.json", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.pos.String())
			require.Equal(t, tc.zero, tc.pos.IsZero())
		})
	}
}
