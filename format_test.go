package schemacompiler_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/plan"
)

func compileString(t *testing.T, schema string) *schemacompiler.Result {
	t.Helper()
	res, err := schemacompiler.Compile(context.Background(), []byte(schema), schemacompiler.Options{})
	require.NoError(t, err)
	require.NotNil(t, res)
	return res
}

var (
	stringFormats = []string{
		"date-time", "date", "time", "duration", "uuid", "ipv4", "ipv6", "byte", "binary",
		"email", "idn-email", "hostname", "idn-hostname", "uri", "uri-reference",
		"uri-template", "iri", "iri-reference", "json-pointer", "relative-json-pointer",
		"regex", "password", "phone-number",
	}
	numberFormats = []string{"int32", "int64", "float", "double", "decimal"}
)

// TestFormatApplicability is the cross-product of every known format name against the
// JSON kinds it may meet: a format applies to exactly one kind (design §3), so it lands
// on the representation and in the validation plan for that kind and is dropped as
// vacuous for every other.
func TestFormatApplicability(t *testing.T) {
	kinds := []struct {
		typ  string
		kind plan.JSONKind
	}{
		{"string", plan.KindString},
		{"number", plan.KindNumber},
		{"object", plan.KindObject},
	}
	for _, group := range []struct {
		formats     []string
		applicable  string
		numericKind plan.NumericDomain
	}{
		{stringFormats, "string", plan.AnyNumber},
		{numberFormats, "number", plan.AnyNumber},
	} {
		for _, name := range group.formats {
			for _, k := range kinds {
				t.Run(name+"/"+k.typ, func(t *testing.T) {
					res := compileString(t, `{"type":"`+k.typ+`","format":"`+name+`"}`)
					require.Empty(t, res.Diagnostics)

					if k.typ != group.applicable {
						require.NotEqual(t, plan.PrimitiveRepresentation{
							Kind:   k.kind,
							Format: name,
						}, res.Plan.Representation, "format must not reach an inapplicable kind")
						require.Empty(t, checks(res.Plan))
						return
					}

					require.Equal(t, plan.PrimitiveRepresentation{
						Kind:    k.kind,
						Numeric: group.numericKind,
						Format:  name,
					}, res.Plan.Representation)
					require.Len(t, checks(res.Plan), 1)
					require.Equal(t, plan.GuardedPredicate{
						Applicability: kindSet(k.kind),
						Expression:    plan.FormatPredicate{Format: name},
					}, checks(res.Plan)[0])
				})
			}
		}
	}
}

func kindSet(k plan.JSONKind) plan.KindSet {
	switch k {
	case plan.KindString:
		return plan.SetString
	case plan.KindNumber:
		return plan.SetNumber
	default:
		return plan.SetObject
	}
}

// TestFormatWithoutType locks the applicability guard of a bare `format`: {"format":"uuid"}
// accepts every non-string value, so a backend generating from Applicability must not
// reject the number 1 (design §3, §24 exact conversion).
func TestFormatWithoutType(t *testing.T) {
	for _, tt := range []struct {
		format string
		guard  plan.KindSet
	}{
		{"uuid", plan.SetString},
		{"date-time", plan.SetString},
		{"email", plan.SetString},
		{"phone-number", plan.SetString},
		{"int32", plan.SetNumber},
		{"double", plan.SetNumber},
	} {
		t.Run(tt.format, func(t *testing.T) {
			res := compileString(t, `{"format":"`+tt.format+`"}`)

			require.Equal(t, plan.AnyRepresentation{}, res.Plan.Representation)
			require.Len(t, checks(res.Plan), 1)
			require.Equal(t, plan.GuardedPredicate{
				Applicability: tt.guard,
				Expression:    plan.FormatPredicate{Format: tt.format},
			}, checks(res.Plan)[0])
		})
	}
}

// TestFormatOnTypeArray: a `type` array keeps every listed kind, so the format's guard
// still decides which of them it constrains.
func TestFormatOnTypeArray(t *testing.T) {
	res := compileString(t, `{"type":["string","number"],"format":"uuid"}`)

	dispatch, ok := res.Plan.Dispatch.(plan.KindDispatch)
	require.True(t, ok)
	for kind, c := range dispatch.Cases {
		if kind == plan.KindString {
			require.Equal(t, []plan.GuardedPredicate{{
				Applicability: plan.SetString,
				Expression:    plan.FormatPredicate{Format: "uuid"},
			}}, checks(c))
			continue
		}
		require.Empty(t, checks(c), "format is vacuous for kind %d", kind)
	}

	union, ok := res.Plan.Representation.(plan.UnionRepresentation)
	require.True(t, ok)
	require.Contains(t, union.Alternatives, plan.Representation(plan.PrimitiveRepresentation{
		Kind: plan.KindString, Format: "uuid",
	}))
	require.Contains(t, union.Alternatives, plan.Representation(plan.PrimitiveRepresentation{
		Kind: plan.KindNumber,
	}))
}

// TestFormatAllOfOrderIndependent: allOf is an unordered intersection (design §11.5), so
// two distinct formats must not let branch order pick the representation.
func TestFormatAllOfOrderIndependent(t *testing.T) {
	forward := compileString(t,
		`{"allOf":[{"type":"string","format":"date-time"},{"format":"uuid"}]}`)
	reverse := compileString(t,
		`{"allOf":[{"type":"string","format":"uuid"},{"format":"date-time"}]}`)

	require.Equal(t, forward.Plan.Representation, reverse.Plan.Representation)
	require.Equal(t, plan.PrimitiveRepresentation{Kind: plan.KindString}, forward.Plan.Representation)

	for _, res := range []*schemacompiler.Result{forward, reverse} {
		require.Len(t, checks(res.Plan), 2, "both formats stay in the validation plan")
		require.Len(t, res.Diagnostics, 1)
		require.Equal(t, plan.SeverityInfo, res.Diagnostics[0].Severity)
		require.NotEmpty(t, res.Diagnostics[0].Pointer)
		require.Contains(t, res.Diagnostics[0].Message, "date-time, uuid")
	}
	require.Equal(t, forward.Diagnostics[0].Message, reverse.Diagnostics[0].Message)
}

// TestFormatAllOfDisjointKinds: a string and a number format compose without conflict,
// because each guards a different kind and only one can reach a given representation.
func TestFormatAllOfDisjointKinds(t *testing.T) {
	res := compileString(t,
		`{"type":"number","allOf":[{"format":"uuid"},{"format":"int32"}]}`)

	require.Equal(t, plan.PrimitiveRepresentation{Kind: plan.KindNumber, Format: "int32"},
		res.Plan.Representation)
	require.Len(t, checks(res.Plan), 1)
	require.Empty(t, res.Diagnostics)
}

// TestFormatSameNameTwice: repeating one format across allOf is not a conflict.
func TestFormatSameNameTwice(t *testing.T) {
	res := compileString(t,
		`{"allOf":[{"type":"string","format":"uuid"},{"format":"uuid"}]}`)

	require.Equal(t, plan.PrimitiveRepresentation{Kind: plan.KindString, Format: "uuid"},
		res.Plan.Representation)
	require.Empty(t, res.Diagnostics)
}

// checks is the plan's real runtime work: every predicate NOT already discharged by the
// chosen representation. That is the same test [plan.DirectGoType] is decided by
// (design §22), so it drops the bare kind assertions design §4.1 requires every plan to
// carry, and a numeric-domain check the representation's own domain already implies
// (issue #115). Tests about what a keyword lowers to want what is left.
func checks(p plan.CompilationPlan) []plan.GuardedPredicate {
	prim, isPrim := p.Representation.(plan.PrimitiveRepresentation)
	var out []plan.GuardedPredicate
	for _, gp := range p.Validation.Predicates {
		switch e := gp.Expression.(type) {
		case nil:
			continue
		case plan.NumericDomainPredicate:
			if isPrim && prim.Numeric == e.Domain {
				continue
			}
		}
		out = append(out, gp)
	}
	return out
}
