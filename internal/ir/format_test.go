package ir

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/frontend"
	"github.com/ogen-go/schemacompiler/plan"
)

func TestCompile_FormatGuardPerName(t *testing.T) {
	for _, tt := range []struct {
		name string
		want plan.KindSet
	}{
		{"date-time", plan.SetString},
		{"date", plan.SetString},
		{"time", plan.SetString},
		{"duration", plan.SetString},
		{"uuid", plan.SetString},
		{"ipv4", plan.SetString},
		{"ipv6", plan.SetString},
		{"byte", plan.SetString},
		{"binary", plan.SetString},
		{"email", plan.SetString},
		{"idn-email", plan.SetString},
		{"hostname", plan.SetString},
		{"idn-hostname", plan.SetString},
		{"uri", plan.SetString},
		{"uri-reference", plan.SetString},
		{"uri-template", plan.SetString},
		{"iri", plan.SetString},
		{"iri-reference", plan.SetString},
		{"json-pointer", plan.SetString},
		{"relative-json-pointer", plan.SetString},
		{"regex", plan.SetString},
		{"password", plan.SetString},
		{"phone-number", plan.SetString},
		{"int32", plan.SetNumber},
		{"int64", plan.SetNumber},
		{"float", plan.SetNumber},
		{"double", plan.SetNumber},
		{"decimal", plan.SetNumber},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := Compile(&frontend.Node{Format: tt.name}).(All)
			require.Equal(t, []Expr{
				Predicate{Guard: tt.want, Detail: FormatDetail{Format: tt.name}},
			}, got.Operands)
			require.Equal(t, plan.SetAny, got.Kinds(), "a guarded format still accepts every kind")
		})
	}
}

func TestCompile_FormatNoneIsNotEmitted(t *testing.T) {
	require.Empty(t, Compile(&frontend.Node{}).(All).Operands)
}
