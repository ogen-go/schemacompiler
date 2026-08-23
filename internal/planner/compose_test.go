package planner

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/plan"
)

// TestNegatable covers the half of the [withResidualNegation] gate that [exactlyModeled]
// cannot decide: a plan that accepts everything while its operand does not proves a keyword
// was dropped however exact the plan reports, and a reference is planned from its identity
// alone, so its target's fidelity never reaches this call (issue #82). With no registry
// there is no target to consult, so every reference stays untrusted; [TestNegatable_RefTarget]
// covers the resolvable cases.
func TestNegatable(t *testing.T) {
	anyPlan := plan.CompilationPlan{
		Representation: plan.AnyRepresentation{},
		Dispatch:       plan.NoDispatch{},
	}
	stringPlan := plan.CompilationPlan{
		Representation: plan.PrimitiveRepresentation{Kind: plan.KindString},
		Dispatch:       plan.NoDispatch{},
	}
	refPlan := plan.CompilationPlan{
		Representation: plan.ReferenceRepresentation{Name: "#/$defs/S"},
		Dispatch:       plan.NoDispatch{},
	}
	objectWithRef := plan.CompilationPlan{
		Representation: plan.ObjectRepresentation{Fields: []plan.FieldRepresentation{{Name: "a", Plan: refPlan}}},
		Dispatch:       plan.NoDispatch{},
	}

	for _, tt := range []struct {
		name    string
		p       plan.CompilationPlan
		operand ir.Expr
		want    bool
	}{
		{
			name:    "a primitive is trusted",
			p:       stringPlan,
			operand: ir.Kinds{Set: plan.KindSet(1 << plan.KindString)},
			want:    true,
		},
		{
			name:    "an object is trusted",
			p:       plan.CompilationPlan{Representation: plan.ObjectRepresentation{}, Dispatch: plan.NoDispatch{}},
			operand: ir.Kinds{Set: plan.KindSet(1 << plan.KindObject)},
			want:    true,
		},
		{
			name:    "an array is trusted",
			p:       plan.CompilationPlan{Representation: plan.ArrayRepresentation{}, Dispatch: plan.NoDispatch{}},
			operand: ir.Kinds{Set: plan.KindSet(1 << plan.KindArray)},
			want:    true,
		},
		{
			name:    "a reference with no registry to resolve it is not",
			p:       refPlan,
			operand: ir.Ref{Target: "#/$defs/S"},
			want:    false,
		},
		{
			name:    "a reference nested in a field with no registry is not",
			p:       objectWithRef,
			operand: ir.Kinds{Set: plan.KindSet(1 << plan.KindObject)},
			want:    false,
		},
		{
			name:    "a plan that enforces nothing is not",
			p:       anyPlan,
			operand: ir.Kinds{Set: plan.KindSet(1 << plan.KindString)},
			want:    false,
		},
		{
			name:    "an unconstrained operand really is unconstrained",
			p:       anyPlan,
			operand: ir.Any{},
			want:    true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, newBuilder(nil).negatable(tt.p, tt.operand))
		})
	}
}
