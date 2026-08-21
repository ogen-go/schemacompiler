package dump_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/dump"
	"github.com/ogen-go/schemacompiler/plan"
)

func refRep(name string) plan.Representation {
	return plan.ReferenceRepresentation{Name: name}
}

func targetDefs(names ...string) map[plan.SchemaID]plan.CompilationPlan {
	defs := make(map[plan.SchemaID]plan.CompilationPlan, len(names))
	for _, n := range names {
		defs[plan.SchemaID(n)] = plan.CompilationPlan{
			Representation: plan.PrimitiveRepresentation{Kind: plan.KindString},
			Dispatch:       plan.NoDispatch{},
			Resolution:     plan.FullyResolved{},
			Capability:     plan.DirectGoType,
		}
	}
	return defs
}

func TestPlanDOTFollowsNestedReferences(t *testing.T) {
	tests := []struct {
		name string
		p    plan.CompilationPlan
	}{
		{
			name: "object field",
			p: plan.CompilationPlan{Representation: plan.ObjectRepresentation{
				Fields: map[string]plan.FieldRepresentation{"a": {Plan: plan.CompilationPlan{Representation: refRep("A")}}},
			}},
		},
		{
			name: "object additional",
			p: plan.CompilationPlan{Representation: plan.ObjectRepresentation{
				Additional: &plan.CompilationPlan{Representation: refRep("A")},
			}},
		},
		{
			name: "object pattern rule",
			p: plan.CompilationPlan{Representation: plan.ObjectRepresentation{
				PatternRules: []plan.PatternFieldRepresentation{{Pattern: "^x", Plan: plan.CompilationPlan{Representation: refRep("A")}}},
			}},
		},
		{
			name: "array prefix item",
			p: plan.CompilationPlan{Representation: plan.ArrayRepresentation{
				Prefix: []plan.ItemRepresentation{{Plan: plan.CompilationPlan{Representation: refRep("A")}}},
			}},
		},
		{
			name: "array rest item",
			p: plan.CompilationPlan{Representation: plan.ArrayRepresentation{
				Rest: plan.ItemRepresentation{Plan: plan.CompilationPlan{Representation: refRep("A")}},
			}},
		},
		{
			name: "union alternative",
			p: plan.CompilationPlan{Representation: plan.UnionRepresentation{
				Alternatives: []plan.Representation{plan.PrimitiveRepresentation{Kind: plan.KindNull}, refRep("A")},
			}},
		},
		{
			name: "recursive body",
			p: plan.CompilationPlan{Representation: plan.RecursiveRepresentation{
				Name: "Node",
				Body: refRep("A"),
			}},
		},
		{
			name: "contains predicate plan",
			p: plan.CompilationPlan{
				Representation: plan.ArrayRepresentation{},
				Validation: plan.ValidationPlan{Predicates: []plan.GuardedPredicate{{
					Expression: plan.ContainsCountPredicate{
						Schema: plan.CompilationPlan{Representation: refRep("A")},
						Min:    1,
					},
				}}},
			},
		},
		{
			name: "property names predicate plan",
			p: plan.CompilationPlan{
				Representation: plan.ObjectRepresentation{},
				Validation: plan.ValidationPlan{Predicates: []plan.GuardedPredicate{{
					Expression: plan.PropertyNamesPredicate{
						Schema: plan.CompilationPlan{Representation: refRep("A")},
					},
				}}},
			},
		},
		{
			name: "reference nested under a dispatch branch",
			p: plan.CompilationPlan{
				Representation: plan.AnyRepresentation{},
				Dispatch: plan.PredicateCountDispatch{
					Branches: []plan.CompilationPlan{{Representation: plan.ObjectRepresentation{
						Fields: map[string]plan.FieldRepresentation{"a": {Plan: plan.CompilationPlan{Representation: refRep("A")}}},
					}}},
					Minimum: 1,
					Maximum: 1,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			dump.PlanDOT(&out, tt.p, targetDefs("A"))
			got := out.String()

			require.Contains(t, got, `[style=dashed label="ref"]`)
			require.Contains(t, got, `label="string [direct-go-type]"`)
			require.NotContains(t, got, `label="?A"`)
		})
	}
}

func TestPlanDOTNestedReferenceIsStableAndDeduplicated(t *testing.T) {
	p := plan.CompilationPlan{Representation: plan.ObjectRepresentation{
		Fields: map[string]plan.FieldRepresentation{
			"a": {Plan: plan.CompilationPlan{Representation: refRep("A")}},
			"b": {Plan: plan.CompilationPlan{Representation: refRep("B")}},
			"c": {Plan: plan.CompilationPlan{Representation: refRep("A")}},
			"d": {Plan: plan.CompilationPlan{Representation: refRep("C")}},
		},
	}}
	defs := targetDefs("A", "B", "C")

	var first strings.Builder
	dump.PlanDOT(&first, p, defs)
	want := first.String()

	require.Equal(t, 3, strings.Count(want, `[style=dashed label="ref"]`))

	for range 32 {
		var out strings.Builder
		dump.PlanDOT(&out, p, defs)
		require.Equal(t, want, out.String())
	}
}

func TestPlanDOTNestedReferenceToMissingDefIsStub(t *testing.T) {
	p := plan.CompilationPlan{Representation: plan.ArrayRepresentation{
		Rest: plan.ItemRepresentation{Plan: plan.CompilationPlan{Representation: refRep("Missing")}},
	}}

	var out strings.Builder
	dump.PlanDOT(&out, p, nil)
	require.Contains(t, out.String(), `label="?Missing"`)
}
