package planterp_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/planterp"
	"github.com/ogen-go/schemacompiler/plan"
)

func decode(t *testing.T, raw string) any {
	t.Helper()

	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.UseNumber()
	var out any
	require.NoError(t, dec.Decode(&out))
	return out
}

func guarded(guard plan.KindSet, e plan.PredicateExpr) plan.ValidationPlan {
	return plan.ValidationPlan{Predicates: []plan.GuardedPredicate{{Applicability: guard, Expression: e}}}
}

func leaf(r plan.Representation) plan.CompilationPlan {
	return plan.CompilationPlan{Representation: r}
}

type interpCase struct {
	name   string
	plan   plan.CompilationPlan
	value  string
	accept bool
}

func runCases(t *testing.T, cases []interpCase) {
	t.Helper()

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			verdict, err := planterp.Interpret(tt.plan, decode(t, tt.value))
			require.NoError(t, err)
			require.Equal(t, tt.accept, verdict.Accepted, "reason: %s", verdict.Reason)
		})
	}
}

func TestRepresentation(t *testing.T) {
	object := plan.ObjectRepresentation{
		Fields: []plan.FieldRepresentation{
			{Name: "req", Plan: plan.CompilationPlan{Representation: plan.PrimitiveRepresentation{Kind: plan.KindString}}, Presence: plan.PresenceRequired},
			{Name: "opt", Plan: plan.CompilationPlan{Representation: plan.PrimitiveRepresentation{Kind: plan.KindString}}, Presence: plan.PresenceOptional},
			{Name: "null", Plan: plan.CompilationPlan{Representation: plan.PrimitiveRepresentation{Kind: plan.KindString}}, Nullable: true, Presence: plan.PresenceOptional},
		},
		Additional:   &plan.CompilationPlan{Representation: plan.NeverRepresentation{}},
		PatternRules: []plan.PatternFieldRepresentation{{Pattern: "^x-", Plan: plan.CompilationPlan{Representation: plan.PrimitiveRepresentation{Kind: plan.KindNumber}}}},
	}
	array := plan.ArrayRepresentation{
		Prefix: []plan.ItemRepresentation{{Plan: plan.CompilationPlan{Representation: plan.PrimitiveRepresentation{Kind: plan.KindBoolean}}}},
		Rest:   plan.ItemRepresentation{Plan: plan.CompilationPlan{Representation: plan.PrimitiveRepresentation{Kind: plan.KindString}}},
	}

	runCases(t, []interpCase{
		{name: "any accepts null", plan: leaf(plan.AnyRepresentation{}), value: `null`, accept: true},
		{name: "never rejects null", plan: leaf(plan.NeverRepresentation{}), value: `null`},
		{name: "string accepts string", plan: leaf(plan.PrimitiveRepresentation{Kind: plan.KindString}), value: `"a"`, accept: true},
		{name: "string rejects number", plan: leaf(plan.PrimitiveRepresentation{Kind: plan.KindString}), value: `1`},
		{
			name:   "integer domain accepts 1.0",
			plan:   leaf(plan.PrimitiveRepresentation{Kind: plan.KindNumber, Numeric: plan.IntegerOnly}),
			value:  `1.0`,
			accept: true,
		},
		{
			name:  "integer domain rejects 1.5",
			plan:  leaf(plan.PrimitiveRepresentation{Kind: plan.KindNumber, Numeric: plan.IntegerOnly}),
			value: `1.5`,
		},
		{
			name:  "non-integer domain rejects 1",
			plan:  leaf(plan.PrimitiveRepresentation{Kind: plan.KindNumber, Numeric: plan.NonIntegerOnly}),
			value: `1`,
		},
		{name: "object requires a required field", plan: leaf(object), value: `{}`},
		{name: "object accepts the required field", plan: leaf(object), value: `{"req":"a"}`, accept: true},
		{name: "object rejects a mistyped optional field", plan: leaf(object), value: `{"req":"a","opt":1}`},
		{name: "object accepts null in a nullable field", plan: leaf(object), value: `{"req":"a","null":null}`, accept: true},
		{name: "object rejects null in a non-nullable field", plan: leaf(object), value: `{"req":"a","opt":null}`},
		{name: "object routes a matching name to its pattern rule", plan: leaf(object), value: `{"req":"a","x-n":1}`, accept: true},
		{name: "object rejects a mistyped pattern match", plan: leaf(object), value: `{"req":"a","x-n":"s"}`},
		{name: "object rejects an unmatched name against Never additional", plan: leaf(object), value: `{"req":"a","other":1}`},
		{name: "object rejects a non-object", plan: leaf(object), value: `[]`},
		{name: "array checks the tuple prefix", plan: leaf(array), value: `[true,"a"]`, accept: true},
		{name: "array rejects a mistyped prefix item", plan: leaf(array), value: `["a"]`},
		{name: "array rejects a mistyped rest item", plan: leaf(array), value: `[true,1]`},
		{
			name: "array with no rest rejects items past the prefix",
			plan: leaf(plan.ArrayRepresentation{
				Prefix: []plan.ItemRepresentation{{Plan: plan.CompilationPlan{Representation: plan.AnyRepresentation{}}}},
			}),
			value: `[1,2]`,
		},
		{
			name: "union accepts any alternative",
			plan: leaf(plan.UnionRepresentation{Alternatives: []plan.Representation{
				plan.PrimitiveRepresentation{Kind: plan.KindString},
				plan.PrimitiveRepresentation{Kind: plan.KindNumber},
			}}),
			value:  `1`,
			accept: true,
		},
		{
			name: "union rejects when no alternative holds",
			plan: leaf(plan.UnionRepresentation{Alternatives: []plan.Representation{
				plan.PrimitiveRepresentation{Kind: plan.KindString},
			}}),
			value: `1`,
		},
		{
			name: "recursive binder resolves its own name",
			plan: leaf(plan.RecursiveRepresentation{
				Name: "node",
				Body: plan.ArrayRepresentation{
					Rest: plan.ItemRepresentation{Plan: plan.CompilationPlan{Representation: plan.ReferenceRepresentation{Name: "node"}}},
				},
			}),
			value:  `[[[]]]`,
			accept: true,
		},
	})
}

func TestReference(t *testing.T) {
	definitions := map[plan.SchemaID]plan.CompilationPlan{
		"#/$defs/s": leaf(plan.PrimitiveRepresentation{Kind: plan.KindString}),
	}
	referring := plan.CompilationPlan{
		Representation: plan.ReferenceRepresentation{Name: "#/$defs/s"},
		Resolution:     plan.StaticReferenceGraph{Definitions: definitions},
	}

	runCases(t, []interpCase{
		{name: "static graph resolves the target", plan: referring, value: `"a"`, accept: true},
		{name: "static graph applies the target", plan: referring, value: `1`},
	})

	t.Run("dynamic graph reuses its static definitions", func(t *testing.T) {
		p := plan.CompilationPlan{
			Representation: plan.ReferenceRepresentation{Name: "#/$defs/s"},
			Resolution:     plan.DynamicReferenceGraph{StaticDefinitions: definitions},
		}
		verdict, err := planterp.Interpret(p, "a")
		require.NoError(t, err)
		require.True(t, verdict.Accepted)
	})

	t.Run("an unresolvable reference is not a verdict", func(t *testing.T) {
		_, err := planterp.Interpret(leaf(plan.ReferenceRepresentation{Name: "#/$defs/missing"}), "a")
		require.ErrorContains(t, err, "resolves to no definition")
	})

	t.Run("a cycle with no instance descent fails loudly", func(t *testing.T) {
		p := plan.CompilationPlan{
			Representation: plan.ReferenceRepresentation{Name: "#/$defs/loop"},
			Resolution: plan.StaticReferenceGraph{Definitions: map[plan.SchemaID]plan.CompilationPlan{
				"#/$defs/loop": leaf(plan.ReferenceRepresentation{Name: "#/$defs/loop"}),
			}},
		}
		_, err := planterp.Interpret(p, "a")
		require.ErrorContains(t, err, "reference cycle")
	})

	t.Run("recursion through an instance descent terminates", func(t *testing.T) {
		p := plan.CompilationPlan{
			Representation: plan.ReferenceRepresentation{Name: "#/$defs/list"},
			Resolution: plan.StaticReferenceGraph{Definitions: map[plan.SchemaID]plan.CompilationPlan{
				"#/$defs/list": leaf(plan.ArrayRepresentation{
					Rest: plan.ItemRepresentation{Plan: plan.CompilationPlan{Representation: plan.ReferenceRepresentation{Name: "#/$defs/list"}}},
				}),
			}},
		}
		verdict, err := planterp.Interpret(p, decode(t, `[[],[[]]]`))
		require.NoError(t, err)
		require.True(t, verdict.Accepted)
	})
}

func TestDispatch(t *testing.T) {
	str := leaf(plan.PrimitiveRepresentation{Kind: plan.KindString})
	num := leaf(plan.PrimitiveRepresentation{Kind: plan.KindNumber})

	withDispatch := func(d plan.DispatchPlan) plan.CompilationPlan {
		return plan.CompilationPlan{Representation: plan.AnyRepresentation{}, Dispatch: d}
	}
	kindDispatch := withDispatch(plan.KindDispatch{Cases: map[plan.JSONKind]plan.CompilationPlan{
		plan.KindString: str,
		plan.KindNumber: num,
	}})
	literalDispatch := withDispatch(plan.LiteralDispatch{Cases: []plan.LiteralCase{
		{Value: "a", Raw: []byte(`"a"`), Plan: str},
		{Value: 9007199254740993.0, Raw: []byte(`9007199254740993`), Plan: num},
	}})
	propertyDispatch := withDispatch(plan.PropertyDispatch{
		Property: "kind",
		Cases: []plan.LiteralCase{{
			Value: "cat",
			Plan: leaf(plan.ObjectRepresentation{
				Fields:     []plan.FieldRepresentation{{Name: "purrs", Plan: plan.CompilationPlan{Representation: plan.PrimitiveRepresentation{Kind: plan.KindBoolean}}}},
				Additional: &plan.CompilationPlan{Representation: plan.AnyRepresentation{}},
			}),
		}},
	})
	presenceDispatch := withDispatch(plan.PresenceDispatch{
		Property: "a",
		Present:  leaf(plan.ObjectRepresentation{Fields: []plan.FieldRepresentation{{Name: "b", Presence: plan.PresenceRequired, Plan: plan.CompilationPlan{Representation: plan.AnyRepresentation{}}}}, Additional: &plan.CompilationPlan{Representation: plan.AnyRepresentation{}}}),
		Absent:   leaf(plan.AnyRepresentation{}),
	})
	countDispatch := func(minimum, maximum int) plan.CompilationPlan {
		return withDispatch(plan.PredicateCountDispatch{Branches: []plan.CompilationPlan{str, num, leaf(plan.AnyRepresentation{})}, Minimum: minimum, Maximum: maximum})
	}

	runCases(t, []interpCase{
		{name: "kind dispatch selects a case", plan: kindDispatch, value: `"a"`, accept: true},
		{name: "kind dispatch rejects an uncovered kind", plan: kindDispatch, value: `true`},
		{name: "literal dispatch matches a string", plan: literalDispatch, value: `"a"`, accept: true},
		{name: "literal dispatch matches past float64 precision", plan: literalDispatch, value: `9007199254740993`, accept: true},
		{name: "literal dispatch rejects a near miss", plan: literalDispatch, value: `9007199254740992`},
		{name: "literal dispatch rejects an unlisted value", plan: literalDispatch, value: `"b"`},
		{name: "property dispatch selects on the tag", plan: propertyDispatch, value: `{"kind":"cat","purrs":true}`, accept: true},
		{name: "property dispatch applies the selected branch", plan: propertyDispatch, value: `{"kind":"cat","purrs":1}`},
		{name: "property dispatch rejects an absent tag", plan: propertyDispatch, value: `{}`},
		{name: "property dispatch rejects an unlisted tag", plan: propertyDispatch, value: `{"kind":"dog"}`},
		{name: "presence dispatch takes the present branch", plan: presenceDispatch, value: `{"a":1}`},
		{name: "presence dispatch satisfies the present branch", plan: presenceDispatch, value: `{"a":1,"b":2}`, accept: true},
		{name: "presence dispatch takes the absent branch", plan: presenceDispatch, value: `{}`, accept: true},
		{name: "oneOf-shaped count rejects two matches", plan: countDispatch(1, 1), value: `"a"`},
		{name: "anyOf-shaped count accepts two matches", plan: countDispatch(1, 3), value: `"a"`, accept: true},
		{name: "count rejects too few matches", plan: countDispatch(2, 3), value: `true`},
	})
}

func TestPredicates(t *testing.T) {
	two := uint64(2)
	unrestricted := func(guard plan.KindSet, e plan.PredicateExpr) plan.CompilationPlan {
		return plan.CompilationPlan{Representation: plan.AnyRepresentation{}, Validation: guarded(guard, e)}
	}

	runCases(t, []interpCase{
		{name: "minLength counts code points", plan: unrestricted(plan.SetString, plan.MinLengthPredicate{Value: 2}), value: `"é"`},
		{name: "minLength accepts two code points", plan: unrestricted(plan.SetString, plan.MinLengthPredicate{Value: 2}), value: `"éé"`, accept: true},
		{name: "a guard that does not fire passes vacuously", plan: unrestricted(plan.SetString, plan.MinLengthPredicate{Value: 2}), value: `1`, accept: true},
		{name: "maxLength rejects a longer string", plan: unrestricted(plan.SetString, plan.MaxLengthPredicate{Value: 1}), value: `"ab"`},
		{name: "pattern is unanchored", plan: unrestricted(plan.SetString, plan.PatternPredicate{Regex: "b"}), value: `"abc"`, accept: true},
		{name: "pattern rejects a non-match", plan: unrestricted(plan.SetString, plan.PatternPredicate{Regex: "^b"}), value: `"abc"`},
		{name: "format is an annotation, not an assertion", plan: unrestricted(plan.SetString, plan.FormatPredicate{Format: "email"}), value: `"not-an-email"`, accept: true},
		{name: "minimum accepts its boundary", plan: unrestricted(plan.SetNumber, plan.MinimumPredicate{Value: 1.1}), value: `1.1`, accept: true},
		{name: "exclusiveMinimum rejects its boundary", plan: unrestricted(plan.SetNumber, plan.MinimumPredicate{Value: 1.1, Exclusive: true}), value: `1.1`},
		{name: "maximum rejects above", plan: unrestricted(plan.SetNumber, plan.MaximumPredicate{Value: 3}), value: `3.5`},
		{name: "exclusiveMaximum rejects its boundary", plan: unrestricted(plan.SetNumber, plan.MaximumPredicate{Value: 3, Exclusive: true}), value: `3`},
		{name: "multipleOf uses decimal, not binary, arithmetic", plan: unrestricted(plan.SetNumber, plan.MultipleOfPredicate{Value: 0.0001}), value: `0.0075`, accept: true},
		{name: "multipleOf rejects a non-multiple", plan: unrestricted(plan.SetNumber, plan.MultipleOfPredicate{Value: 2}), value: `7`},
		{name: "minItems rejects a short array", plan: unrestricted(plan.SetArray, plan.MinItemsPredicate{Value: 2}), value: `[1]`},
		{name: "maxItems rejects a long array", plan: unrestricted(plan.SetArray, plan.MaxItemsPredicate{Value: 1}), value: `[1,2]`},
		{name: "uniqueItems compares JSON-deep", plan: unrestricted(plan.SetArray, plan.UniqueItemsPredicate{}), value: `[{"a":1,"b":2},{"b":2,"a":1}]`},
		{name: "uniqueItems accepts distinct items", plan: unrestricted(plan.SetArray, plan.UniqueItemsPredicate{}), value: `[1,"1",true]`, accept: true},
		{name: "required rejects an absent property", plan: unrestricted(plan.SetObject, plan.RequiredPredicate{Properties: []string{"a"}}), value: `{}`},
		{name: "minProperties rejects too few", plan: unrestricted(plan.SetObject, plan.MinPropertiesPredicate{Value: 2}), value: `{"a":1}`},
		{name: "maxProperties rejects too many", plan: unrestricted(plan.SetObject, plan.MaxPropertiesPredicate{Value: 1}), value: `{"a":1,"b":2}`},
		{
			name:  "dependentRequired fires only when the trigger is present",
			plan:  unrestricted(plan.SetObject, plan.DependentRequiredPredicate{Entries: []plan.DependentRequiredEntry{{Property: "a", Requires: []string{"b"}}}}),
			value: `{"a":1}`,
		},
		{
			name:   "dependentRequired passes when the trigger is absent",
			plan:   unrestricted(plan.SetObject, plan.DependentRequiredPredicate{Entries: []plan.DependentRequiredEntry{{Property: "a", Requires: []string{"b"}}}}),
			value:  `{"c":1}`,
			accept: true,
		},
		{
			name: "propertyNames runs its nested plan on every key",
			plan: unrestricted(plan.SetObject, plan.PropertyNamesPredicate{
				Schema: plan.CompilationPlan{
					Representation: plan.PrimitiveRepresentation{Kind: plan.KindString},
					Validation:     guarded(plan.SetString, plan.MaxLengthPredicate{Value: 2}),
				},
			}),
			value: `{"abc":1}`,
		},
		{
			name: "contains counts matching elements",
			plan: unrestricted(plan.SetArray, plan.ContainsCountPredicate{
				Schema: leaf(plan.PrimitiveRepresentation{Kind: plan.KindString}),
				Min:    2,
				Max:    &two,
			}),
			value:  `[1,"a","b"]`,
			accept: true,
		},
		{
			name: "contains rejects too many matches",
			plan: unrestricted(plan.SetArray, plan.ContainsCountPredicate{
				Schema: leaf(plan.PrimitiveRepresentation{Kind: plan.KindString}),
				Min:    1,
				Max:    &two,
			}),
			value: `["a","b","c"]`,
		},
	})
}

// TestApproximatedIsReported keeps an acceptance that the interpreter could not actually
// check from passing for evidence that the plan is exact.
func TestApproximatedIsReported(t *testing.T) {
	tests := []struct {
		name string
		expr plan.PredicateExpr
	}{
		{name: "format is never asserted", expr: plan.FormatPredicate{Format: "uuid"}},
		{name: "a pattern RE2 cannot compile", expr: plan.PatternPredicate{Regex: "(?=a)"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := plan.CompilationPlan{
				Representation: plan.AnyRepresentation{},
				Validation:     guarded(plan.SetString, tt.expr),
			}
			verdict, err := planterp.Interpret(p, "x")
			require.NoError(t, err)
			require.True(t, verdict.Accepted)
			require.NotEmpty(t, verdict.Approximated)
		})
	}
}

// TestNegationOverAnApproximatedSubPlanAccepts pins the interpreter's half of issue #82.
// The planner only emits a [plan.NegationPredicate] over a plan it proved exact, but the
// interpreter has constraints of its own it cannot check; inverting an acceptance it had to
// approximate would turn a harmless over-acceptance into the rejection of a valid instance,
// which design §24 forbids in every direction.
func TestNegationOverAnApproximatedSubPlanAccepts(t *testing.T) {
	tests := []struct {
		name string
		expr plan.PredicateExpr
	}{
		{name: "format is never asserted", expr: plan.FormatPredicate{Format: "uuid"}},
		{name: "a pattern RE2 cannot compile", expr: plan.PatternPredicate{Regex: "(?=a)"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := plan.CompilationPlan{
				Representation: plan.AnyRepresentation{},
				Capability:     plan.PredicateDispatch,
				Validation: guarded(plan.SetAny, plan.NegationPredicate{Schema: plan.CompilationPlan{
					Representation: plan.AnyRepresentation{},
					Validation:     guarded(plan.SetString, tt.expr),
				}}),
			}
			verdict, err := planterp.Interpret(p, "x")
			require.NoError(t, err)
			require.True(t, verdict.Accepted,
				"the sub-plan's acceptance was approximated, so inverting it would reject a valid instance")
			require.NotEmpty(t, verdict.Approximated)
		})
	}
}

// TestNegationOverAnEnforcedSubPlanInverts keeps the guard above from swallowing the
// predicate's whole purpose: an acceptance the interpreter really did check still inverts.
func TestNegationOverAnEnforcedSubPlanInverts(t *testing.T) {
	sub := plan.CompilationPlan{
		Representation: plan.AnyRepresentation{},
		Validation:     guarded(plan.SetString, plan.PatternPredicate{Regex: "^x"}),
	}
	p := plan.CompilationPlan{
		Representation: plan.AnyRepresentation{},
		Capability:     plan.PredicateDispatch,
		Validation:     guarded(plan.SetAny, plan.NegationPredicate{Schema: sub}),
	}

	verdict, err := planterp.Interpret(p, "x")
	require.NoError(t, err)
	require.False(t, verdict.Accepted)
	require.Empty(t, verdict.Approximated)

	verdict, err = planterp.Interpret(p, "y")
	require.NoError(t, err)
	require.True(t, verdict.Accepted)
}
