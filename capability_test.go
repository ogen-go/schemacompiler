package schemacompiler

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/plan"
)

func refPlan(target string, level plan.CapabilityLevel) plan.CompilationPlan {
	return plan.CompilationPlan{
		Representation: &plan.ObjectRepresentation{
			Fields: []plan.FieldRepresentation{
				{Name: "f", Plan: plan.CompilationPlan{Representation: &plan.ReferenceRepresentation{Name: target}}},
			},
		},
		Capability: level,
	}
}

func TestRollUpCapabilities(t *testing.T) {
	for _, tt := range []struct {
		name      string
		plans     map[plan.SchemaID]plan.CompilationPlan
		want      map[plan.SchemaID]plan.CapabilityLevel
		wantDiags int
	}{
		{
			name: "reference raises referrer",
			plans: map[plan.SchemaID]plan.CompilationPlan{
				"A": refPlan("B", plan.DirectGoType),
				"B": {Capability: plan.EvaluationStateValidation},
			},
			want: map[plan.SchemaID]plan.CapabilityLevel{
				"A": plan.EvaluationStateValidation,
				"B": plan.EvaluationStateValidation,
			},
		},
		{
			name: "transitive",
			plans: map[plan.SchemaID]plan.CompilationPlan{
				"A": refPlan("B", plan.DirectGoType),
				"B": refPlan("C", plan.DirectGoType),
				"C": {Capability: plan.Unsupported},
			},
			want: map[plan.SchemaID]plan.CapabilityLevel{
				"A": plan.Unsupported,
				"B": plan.Unsupported,
				"C": plan.Unsupported,
			},
		},
		{
			name: "cycle terminates",
			plans: map[plan.SchemaID]plan.CompilationPlan{
				"A": refPlan("B", plan.DirectGoType),
				"B": refPlan("A", plan.GoTypeWithValidation),
			},
			want: map[plan.SchemaID]plan.CapabilityLevel{
				"A": plan.GoTypeWithValidation,
				"B": plan.GoTypeWithValidation,
			},
		},
		{
			name: "never lowered",
			plans: map[plan.SchemaID]plan.CompilationPlan{
				"A": refPlan("B", plan.StaticDispatch),
				"B": {Capability: plan.DirectGoType},
			},
			want: map[plan.SchemaID]plan.CapabilityLevel{
				"A": plan.StaticDispatch,
				"B": plan.DirectGoType,
			},
		},
		{
			name: "reference to no plan is unsupported",
			plans: map[plan.SchemaID]plan.CompilationPlan{
				"A": refPlan("Missing", plan.DirectGoType),
				"B": refPlan("A", plan.DirectGoType),
			},
			want: map[plan.SchemaID]plan.CapabilityLevel{
				"A": plan.Unsupported,
				"B": plan.Unsupported,
			},
			wantDiags: 1,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			diags := rollUpCapabilities(tt.plans, nil)
			got := make(map[plan.SchemaID]plan.CapabilityLevel, len(tt.plans))
			for id, p := range tt.plans {
				got[id] = p.Capability
			}
			require.Equal(t, tt.want, got)
			require.Len(t, diags, tt.wantDiags)
			for _, d := range diags {
				require.Equal(t, plan.SeverityError, d.Severity)
			}
		})
	}
}

func TestPlanReferencesNested(t *testing.T) {
	p := plan.CompilationPlan{
		Representation: &plan.ArrayRepresentation{
			Prefix: []plan.ItemRepresentation{{Plan: plan.CompilationPlan{Representation: &plan.ReferenceRepresentation{Name: "P"}}}},
			Rest: plan.ItemRepresentation{Plan: plan.CompilationPlan{Representation: &plan.UnionRepresentation{
				Alternatives: []plan.Representation{
					&plan.ReferenceRepresentation{Name: "U"},
					&plan.UnionRepresentation{Alternatives: []plan.Representation{
						&plan.ReferenceRepresentation{Name: "B"},
					}},
				},
			}}},
		},
		Dispatch: &plan.PredicateCountDispatch{
			Branches: []plan.CompilationPlan{
				{Representation: &plan.ReferenceRepresentation{Name: "D"}},
			},
		},
		Validation: plan.ValidationPlan{Predicates: []plan.GuardedPredicate{
			{Expression: &plan.ContainsCountPredicate{
				Schema: plan.CompilationPlan{Representation: &plan.ReferenceRepresentation{Name: "C"}},
			}},
			{Expression: &plan.PropertyNamesPredicate{
				Schema: plan.CompilationPlan{Representation: &plan.ReferenceRepresentation{Name: "N"}},
			}},
		}},
	}

	var got []plan.SchemaID
	planReferences(p, func(id plan.SchemaID) { got = append(got, id) })
	require.ElementsMatch(t, []plan.SchemaID{"P", "U", "B", "D", "C", "N"}, got)
}
