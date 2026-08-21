package plan_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/plan"
)

func TestClassifyFormat(t *testing.T) {
	for _, tt := range []struct {
		name string
		want plan.FormatClass
	}{
		{"", plan.FormatNone},
		{"date-time", plan.FormatRepresentational},
		{"date", plan.FormatRepresentational},
		{"uuid", plan.FormatRepresentational},
		{"ipv4", plan.FormatRepresentational},
		{"ipv6", plan.FormatRepresentational},
		{"byte", plan.FormatRepresentational},
		{"int32", plan.FormatRepresentational},
		{"decimal", plan.FormatRepresentational},
		{"email", plan.FormatValidationOnly},
		{"hostname", plan.FormatValidationOnly},
		{"uri", plan.FormatValidationOnly},
		{"regex", plan.FormatValidationOnly},
		{"json-pointer", plan.FormatValidationOnly},
		{"phone-number", plan.FormatUnrecognized},
		{"Date-Time", plan.FormatUnrecognized},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, plan.ClassifyFormat(tt.name))

			f := plan.NewFormat(tt.name)
			require.Equal(t, tt.name, f.Name)
			require.Equal(t, tt.want, f.Class)
			require.Equal(t, tt.want == plan.FormatRepresentational, f.Representational())
		})
	}
}
