package planterp_test

import (
	"testing"

	"github.com/go-faster/errors"
	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/planterp"
	"github.com/ogen-go/schemacompiler/plan"
)

// nestedRejectionPlan is a one-branch `oneOf` over an object whose "items" array holds
// objects with a required string "name": the shortest plan that puts a rejection inside
// an array element inside a dispatch branch.
func nestedRejectionPlan() plan.CompilationPlan {
	element := plan.ObjectRepresentation{
		Fields: map[string]plan.FieldRepresentation{
			"name": {
				Representation: plan.PrimitiveRepresentation{Kind: plan.KindString},
				Presence:       plan.PresenceRequired,
			},
		},
		Additional: plan.AnyRepresentation{},
	}
	branch := plan.CompilationPlan{
		Representation: plan.ObjectRepresentation{
			Fields: map[string]plan.FieldRepresentation{
				"items": {
					Representation: plan.ArrayRepresentation{
						Rest: plan.ItemRepresentation{Representation: element},
					},
					Presence: plan.PresenceRequired,
				},
			},
			Additional: plan.AnyRepresentation{},
		},
	}
	return plan.CompilationPlan{Dispatch: plan.PredicateCountDispatch{
		Branches: []plan.CompilationPlan{branch},
		Minimum:  1,
		Maximum:  1,
	}}
}

// TestNestedValidateErrorRenders pins the located, chained rendering that makes a
// differential finding diagnosable without re-deriving it by hand (issue #83).
func TestNestedValidateErrorRenders(t *testing.T) {
	instance := map[string]any{"items": []any{
		map[string]any{"name": "a"},
		map[string]any{"name": "b"},
		map[string]any{"name": 1.0},
	}}

	v, err := planterp.Interpret(nestedRejectionPlan(), instance)
	require.NoError(t, err, "a rejection is a verdict, never an error")
	require.False(t, v.Accepted)
	require.Equal(t, "predicate-count dispatch", v.Reason.Constraint)
	require.Equal(t, "/items/2/name", v.Reason.Leaf().Path)
	require.Equal(t, "primitive", v.Reason.Leaf().Constraint)

	const want = `/items/2/name: primitive: value is number, representation is string
  via: /items/2: field: "name"
  via: /items: array item: 2
  via: field: "items"
  via: predicate-count dispatch: 0 branches match, want 1-1`
	require.Equal(t, want, v.Reason.Error())
	t.Log("\n" + v.Reason.Error())
}

// TestErrorKindsAreDiscriminated is the whole point of the split: the three outcomes are
// told apart by type, not by matching substrings of one flat message.
func TestErrorKindsAreDiscriminated(t *testing.T) {
	anyPlan := plan.CompilationPlan{Representation: plan.AnyRepresentation{}}

	t.Run("ValidateError", func(t *testing.T) {
		v, err := planterp.Interpret(nestedRejectionPlan(), map[string]any{})
		require.NoError(t, err)
		require.False(t, v.Accepted)

		var validate *planterp.ValidateError
		require.ErrorAs(t, error(v.Reason), &validate)

		var invalid *planterp.InvalidValueError
		var internal *planterp.InternalError
		require.False(t, errors.As(error(v.Reason), &invalid))
		require.False(t, errors.As(error(v.Reason), &internal))
	})

	t.Run("InvalidValueError", func(t *testing.T) {
		_, err := planterp.Interpret(anyPlan, int32(1))

		var invalid *planterp.InvalidValueError
		require.ErrorAs(t, err, &invalid)
		require.Equal(t, int32(1), invalid.Value)
		require.Equal(t, "planterp: not a decoded JSON value: int32", invalid.Error())

		var internal *planterp.InternalError
		require.False(t, errors.As(err, &internal))
	})

	t.Run("InternalError", func(t *testing.T) {
		// plan.Representation's method set is unexported, so no test outside package
		// plan can add a variant; an out-of-range enum reaches the same defaults.
		broken := plan.CompilationPlan{Representation: plan.PrimitiveRepresentation{
			Kind:    plan.KindNumber,
			Numeric: plan.NumericDomain(99),
		}}
		_, err := planterp.Interpret(broken, 1.0)

		var internal *planterp.InternalError
		require.ErrorAs(t, err, &internal)
		require.Equal(t, "planterp: unhandled plan.NumericDomain 99", internal.Error())

		var invalid *planterp.InvalidValueError
		require.False(t, errors.As(err, &invalid))
	})

	t.Run("InternalErrorFromUnresolvableReference", func(t *testing.T) {
		_, err := planterp.Interpret(plan.CompilationPlan{
			Representation: plan.ReferenceRepresentation{Name: "#/$defs/missing"},
		}, 1.0)

		var internal *planterp.InternalError
		require.ErrorAs(t, err, &internal)
		require.Contains(t, internal.Error(), "resolves to no definition")
	})
}

// TestInvalidValueErrorCarriesLocation keeps the offending value's position, so a bad
// fixture points at the sub-value that is wrong rather than at the whole document.
func TestInvalidValueErrorCarriesLocation(t *testing.T) {
	p := plan.CompilationPlan{Representation: plan.ArrayRepresentation{
		Rest: plan.ItemRepresentation{
			Representation: plan.PrimitiveRepresentation{Kind: plan.KindString},
		},
	}}
	_, err := planterp.Interpret(p, []any{"ok", int32(1)})

	var invalid *planterp.InvalidValueError
	require.ErrorAs(t, err, &invalid)
	require.Equal(t, "/1", invalid.Path)
}

func TestPointerTokensAreEscaped(t *testing.T) {
	p := plan.CompilationPlan{Representation: plan.ObjectRepresentation{
		Fields: map[string]plan.FieldRepresentation{
			"a/b~c": {
				Representation: plan.PrimitiveRepresentation{Kind: plan.KindString},
				Presence:       plan.PresenceRequired,
			},
		},
		Additional: plan.AnyRepresentation{},
	}}
	v, err := planterp.Interpret(p, map[string]any{"a/b~c": 1.0})
	require.NoError(t, err)
	require.False(t, v.Accepted)
	require.Equal(t, "/a~1b~0c", v.Reason.Leaf().Path)
}
