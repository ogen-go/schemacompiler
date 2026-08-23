package plan_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/plan"
)

// TestResidualChecks pins design §22's discharge test: a check the representation already
// implies is not runtime work, and one describing a different shape is (issue #115).
func TestResidualChecks(t *testing.T) {
	stringRep := plan.PrimitiveRepresentation{Kind: plan.KindString}
	intRep := plan.PrimitiveRepresentation{Kind: plan.KindNumber, Numeric: plan.IntegerOnly}
	additional := &plan.CompilationPlan{Representation: &plan.AnyRepresentation{}}

	objRep := plan.ObjectRepresentation{
		Fields:     []plan.FieldRepresentation{{Name: "a", Presence: plan.PresenceRequired}},
		Additional: additional,
	}
	objCheck := plan.ObjectStructurePredicate{
		Properties: []plan.PropertyCheck{{Name: "a", Presence: plan.PresenceRequired}},
		Additional: additional,
	}

	arrRep := plan.ArrayRepresentation{
		Prefix: []plan.ItemRepresentation{{Plan: plan.CompilationPlan{Representation: &stringRep}}},
	}
	arrCheck := plan.ArrayStructurePredicate{Prefix: []plan.CompilationPlan{{Representation: &stringRep}}}

	tests := []struct {
		name     string
		rep      plan.Representation
		pred     plan.GuardedPredicate
		residual bool
	}{
		{
			name:     "a kind assertion is discharged",
			rep:      &stringRep,
			pred:     plan.GuardedPredicate{Applicability: plan.SetString, Assert: true},
			residual: false,
		},
		{
			name:     "a real check is not",
			rep:      &stringRep,
			pred:     plan.GuardedPredicate{Applicability: plan.SetString, Expression: &plan.MinLengthPredicate{Value: 3}},
			residual: true,
		},
		{
			name:     "a numeric domain matching the representation is discharged",
			rep:      &intRep,
			pred:     plan.GuardedPredicate{Expression: &plan.NumericDomainPredicate{Domain: plan.IntegerOnly}},
			residual: false,
		},
		{
			name:     "a numeric domain the representation does not carry is not",
			rep:      &plan.PrimitiveRepresentation{Kind: plan.KindNumber, Numeric: plan.AnyNumber},
			pred:     plan.GuardedPredicate{Expression: &plan.NumericDomainPredicate{Domain: plan.IntegerOnly}},
			residual: true,
		},
		{
			name:     "a structure predicate restating the representation is discharged",
			rep:      &objRep,
			pred:     plan.GuardedPredicate{Applicability: plan.SetObject, Expression: &objCheck},
			residual: false,
		},
		{
			name: "one naming a different property is not",
			rep:  &objRep,
			pred: plan.GuardedPredicate{Applicability: plan.SetObject, Expression: &plan.ObjectStructurePredicate{
				Properties: []plan.PropertyCheck{{Name: "b", Presence: plan.PresenceRequired}},
				Additional: additional,
			}},
			residual: true,
		},
		{
			name: "one requiring what the representation makes optional is not",
			rep:  &objRep,
			pred: plan.GuardedPredicate{Applicability: plan.SetObject, Expression: &plan.ObjectStructurePredicate{
				Properties: []plan.PropertyCheck{{Name: "a", Presence: plan.PresenceOptional}},
				Additional: additional,
			}},
			residual: true,
		},
		{
			name: "one bounding an Additional the representation leaves open is not",
			rep:  &objRep,
			pred: plan.GuardedPredicate{Applicability: plan.SetObject, Expression: &plan.ObjectStructurePredicate{
				Properties: []plan.PropertyCheck{{Name: "a", Presence: plan.PresenceRequired}},
			}},
			residual: true,
		},
		{
			name:     "an array structure restating the representation is discharged",
			rep:      &arrRep,
			pred:     plan.GuardedPredicate{Applicability: plan.SetArray, Expression: &arrCheck},
			residual: false,
		},
		{
			name: "one closing a tuple the representation leaves open is not",
			rep: &plan.ArrayRepresentation{
				Prefix: arrRep.Prefix,
				Rest:   plan.ItemRepresentation{Plan: plan.CompilationPlan{Representation: &plan.AnyRepresentation{}}},
			},
			pred:     plan.GuardedPredicate{Applicability: plan.SetArray, Expression: &arrCheck},
			residual: true,
		},
		{
			name:     "a reference naming the same target is discharged",
			rep:      &plan.ReferenceRepresentation{Name: "#/$defs/A"},
			pred:     plan.GuardedPredicate{Applicability: plan.SetAny, Expression: &plan.ReferencePredicate{Name: "#/$defs/A"}},
			residual: false,
		},
		{
			name:     "one naming a different target is not",
			rep:      &plan.ReferenceRepresentation{Name: "#/$defs/A"},
			pred:     plan.GuardedPredicate{Applicability: plan.SetAny, Expression: &plan.ReferencePredicate{Name: "#/$defs/B"}},
			residual: true,
		},
		{
			name:     "a structure predicate beside the wrong representation is not discharged",
			rep:      &stringRep,
			pred:     plan.GuardedPredicate{Applicability: plan.SetObject, Expression: &objCheck},
			residual: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := plan.ResidualChecks(tt.rep, plan.ValidationPlan{Predicates: []plan.GuardedPredicate{tt.pred}})
			require.Equal(t, tt.residual, len(got) == 1, "got %d residual checks", len(got))
		})
	}
}
