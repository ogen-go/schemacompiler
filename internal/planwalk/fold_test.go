package planwalk_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/internal/planwalk"
	"github.com/ogen-go/schemacompiler/plan"
)

func ref(name string) plan.Representation { return plan.ReferenceRepresentation{Name: name} }

func leaf(name string) plan.CompilationPlan {
	return plan.CompilationPlan{Representation: ref(name)}
}

// label renders a node as edge + payload, so a fold's node sequence can be pinned as
// plain strings.
func label(n planwalk.Node) string {
	s := n.Edge.Kind.String()
	switch n.Kind {
	case planwalk.NodeRepresentation:
		if r, ok := n.Representation.(plan.ReferenceRepresentation); ok {
			s += ":" + r.Name
		} else {
			s += fmt.Sprintf(":%T", n.Representation)
		}
	case planwalk.NodePlan:
		switch r := n.Plan.Representation.(type) {
		case nil:
		case plan.ReferenceRepresentation:
			s += ":" + r.Name
		default:
			if n.Edge.Kind != planwalk.EdgeRoot {
				s += fmt.Sprintf(":%T", r)
			}
		}
	case planwalk.NodeDispatch:
		s += fmt.Sprintf(":%T", n.Dispatch)
	case planwalk.NodePredicate:
		s += fmt.Sprintf(":%T", n.Predicate)
	case planwalk.NodeValidation:
	}
	return s
}

func foldLabels(p plan.CompilationPlan) []string {
	return planwalk.Fold(p, nil, func(acc []string, n planwalk.Node) ([]string, planwalk.Action) {
		return append(acc, label(n)), planwalk.Descend
	})
}

func TestFoldOrderAndEdges(t *testing.T) {
	tests := []struct {
		name string
		plan plan.CompilationPlan
		want []string
	}{
		{
			name: "representation tree",
			plan: plan.CompilationPlan{Representation: plan.ObjectRepresentation{
				Fields:       []plan.FieldRepresentation{{Name: "f", Plan: plan.CompilationPlan{Representation: ref("field")}}},
				Additional:   &plan.CompilationPlan{Representation: ref("additional")},
				PatternRules: []plan.PatternFieldRepresentation{{Pattern: "^x", Plan: plan.CompilationPlan{Representation: ref("pattern")}}},
			}},
			want: []string{
				"root",
				"representation:plan.ObjectRepresentation",
				"field:field",
				"representation:field",
				"validation",
				"additional:additional",
				"representation:additional",
				"validation",
				"pattern-rule:pattern",
				"representation:pattern",
				"validation",
				"validation",
			},
		},
		{
			name: "array and union",
			plan: plan.CompilationPlan{Representation: plan.ArrayRepresentation{
				Prefix: []plan.ItemRepresentation{{Plan: plan.CompilationPlan{Representation: plan.UnionRepresentation{
					Alternatives: []plan.Representation{ref("alt0"), ref("alt1")},
				}}}},
				Rest: plan.ItemRepresentation{Plan: plan.CompilationPlan{Representation: plan.RecursiveRepresentation{
					Name: "R", Body: ref("body"),
				}}},
			}},
			want: []string{
				"root",
				"representation:plan.ArrayRepresentation",
				"prefix-item:plan.UnionRepresentation",
				"representation:plan.UnionRepresentation",
				"alternative:alt0",
				"alternative:alt1",
				"validation",
				"rest-item:plan.RecursiveRepresentation",
				"representation:plan.RecursiveRepresentation",
				"recursive-body:body",
				"validation",
				"validation",
			},
		},
		{
			name: "kind dispatch",
			plan: plan.CompilationPlan{Dispatch: plan.KindDispatch{
				Cases: map[plan.JSONKind]plan.CompilationPlan{plan.KindObject: leaf("case")},
			}},
			want: []string{
				"root",
				"dispatch:plan.KindDispatch",
				"kind-case:case",
				"representation:case",
				"validation",
				"validation",
			},
		},
		{
			name: "literal dispatch",
			plan: plan.CompilationPlan{Dispatch: plan.LiteralDispatch{
				Cases: []plan.LiteralCase{{Value: "a", Plan: leaf("a")}},
			}},
			want: []string{
				"root",
				"dispatch:plan.LiteralDispatch",
				"literal-case:a",
				"representation:a",
				"validation",
				"validation",
			},
		},
		{
			name: "property dispatch",
			plan: plan.CompilationPlan{Dispatch: plan.PropertyDispatch{
				Property: "kind",
				Cases:    []plan.LiteralCase{{Value: "dog", Plan: leaf("dog")}},
			}},
			want: []string{
				"root",
				"dispatch:plan.PropertyDispatch",
				"property-case:dog",
				"representation:dog",
				"validation",
				"validation",
			},
		},
		{
			name: "presence dispatch",
			plan: plan.CompilationPlan{Dispatch: plan.PresenceDispatch{
				Property: "p", Present: leaf("present"), Absent: leaf("absent"),
			}},
			want: []string{
				"root",
				"dispatch:plan.PresenceDispatch",
				"present:present",
				"representation:present",
				"validation",
				"absent:absent",
				"representation:absent",
				"validation",
				"validation",
			},
		},
		{
			name: "predicate-count dispatch",
			plan: plan.CompilationPlan{Dispatch: plan.PredicateCountDispatch{
				Branches: []plan.CompilationPlan{leaf("b0"), leaf("b1")},
			}},
			want: []string{
				"root",
				"dispatch:plan.PredicateCountDispatch",
				"count-branch:b0",
				"representation:b0",
				"validation",
				"count-branch:b1",
				"representation:b1",
				"validation",
				"validation",
			},
		},
		{
			name: "predicates carrying plans",
			plan: plan.CompilationPlan{Validation: plan.ValidationPlan{Predicates: []plan.GuardedPredicate{
				{Expression: plan.MinLengthPredicate{Value: 1}},
				{Expression: plan.ContainsCountPredicate{Schema: leaf("contains")}},
				{Expression: plan.NegationPredicate{Schema: leaf("negated")}},
				{Expression: plan.PropertyNamesPredicate{Schema: leaf("names")}},
			}}},
			want: []string{
				"root",
				"validation",
				"guarded-predicate:plan.MinLengthPredicate",
				"guarded-predicate:plan.ContainsCountPredicate",
				"contains-schema:contains",
				"representation:contains",
				"validation",
				"guarded-predicate:plan.NegationPredicate",
				"negation-schema:negated",
				"representation:negated",
				"validation",
				"guarded-predicate:plan.PropertyNamesPredicate",
				"property-names-schema:names",
				"representation:names",
				"validation",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, foldLabels(tt.plan))
		})
	}
}

func TestFoldEdgePayload(t *testing.T) {
	p := plan.CompilationPlan{
		Representation: plan.ObjectRepresentation{
			Fields: []plan.FieldRepresentation{
				{Name: "f", Plan: plan.CompilationPlan{Representation: ref("field")}, Presence: plan.PresenceRequired, Nullable: true},
			},
			PatternRules: []plan.PatternFieldRepresentation{
				{Pattern: "^x", Plan: plan.CompilationPlan{Representation: ref("p0")}},
				{Pattern: "^y", Plan: plan.CompilationPlan{Representation: ref("p1")}},
			},
		},
		Dispatch: plan.PropertyDispatch{
			Property: "kind",
			Tag:      plan.TagDeclared,
			Cases:    []plan.LiteralCase{{Value: "dog", Raw: []byte(`"dog"`), Plan: leaf("dog")}},
		},
		Validation: plan.ValidationPlan{Predicates: []plan.GuardedPredicate{
			{Applicability: plan.SetString, Expression: plan.MinLengthPredicate{Value: 3}},
		}},
	}

	byEdge := map[planwalk.EdgeKind][]planwalk.Edge{}
	planwalk.Fold(p, struct{}{}, func(acc struct{}, n planwalk.Node) (struct{}, planwalk.Action) {
		byEdge[n.Edge.Kind] = append(byEdge[n.Edge.Kind], n.Edge)
		return acc, planwalk.Descend
	})

	require.Len(t, byEdge[planwalk.EdgeField], 1)
	field := byEdge[planwalk.EdgeField][0]
	require.Equal(t, "f", field.Name)
	require.Equal(t, plan.PresenceRequired, field.Presence)
	require.True(t, field.Nullable)

	require.Len(t, byEdge[planwalk.EdgePatternRule], 2)
	require.Equal(t, "^x", byEdge[planwalk.EdgePatternRule][0].Name)
	require.Equal(t, 0, byEdge[planwalk.EdgePatternRule][0].Index)
	require.Equal(t, "^y", byEdge[planwalk.EdgePatternRule][1].Name)
	require.Equal(t, 1, byEdge[planwalk.EdgePatternRule][1].Index)

	require.Len(t, byEdge[planwalk.EdgePropertyCase], 1)
	tag := byEdge[planwalk.EdgePropertyCase][0]
	require.Equal(t, "kind", tag.Name)
	require.Equal(t, plan.TagDeclared, tag.Tag)
	require.Equal(t, "dog", tag.Value)
	require.Equal(t, []byte(`"dog"`), tag.Raw)

	require.Len(t, byEdge[planwalk.EdgeGuardedPredicate], 1)
	guard := byEdge[planwalk.EdgeGuardedPredicate][0]
	require.Equal(t, 0, guard.Index)
	require.True(t, guard.Applicability.Has(plan.KindString))
	require.False(t, guard.Applicability.Has(plan.KindNumber))
}

// TestFoldEveryEdgeKindIsReachable ties the EdgeKind list to the traversal: a kind that
// no plan shape produces is either dead or a slot the traversal forgot to walk.
func TestFoldEveryEdgeKindIsReachable(t *testing.T) {
	plans := []plan.CompilationPlan{
		{
			Representation: plan.ObjectRepresentation{
				Fields:       []plan.FieldRepresentation{{Name: "f", Plan: plan.CompilationPlan{Representation: ref("f")}}},
				Additional:   &plan.CompilationPlan{Representation: ref("a")},
				PatternRules: []plan.PatternFieldRepresentation{{Pattern: "^x", Plan: plan.CompilationPlan{Representation: ref("p")}}},
			},
			Dispatch: plan.KindDispatch{Cases: map[plan.JSONKind]plan.CompilationPlan{plan.KindObject: leaf("k")}},
			Validation: plan.ValidationPlan{Predicates: []plan.GuardedPredicate{
				{Expression: plan.ContainsCountPredicate{Schema: leaf("c")}},
				{Expression: plan.NegationPredicate{Schema: leaf("neg")}},
				{Expression: plan.PropertyNamesPredicate{Schema: leaf("n")}},
			}},
		},
		{
			Representation: plan.ArrayRepresentation{
				Prefix: []plan.ItemRepresentation{{Plan: plan.CompilationPlan{Representation: plan.UnionRepresentation{
					Alternatives: []plan.Representation{plan.RecursiveRepresentation{Name: "R", Body: ref("b")}},
				}}}},
				Rest: plan.ItemRepresentation{Plan: plan.CompilationPlan{Representation: ref("rest")}},
			},
			Dispatch: plan.LiteralDispatch{Cases: []plan.LiteralCase{{Value: 1, Plan: leaf("l")}}},
		},
		{Dispatch: plan.PropertyDispatch{Property: "k", Cases: []plan.LiteralCase{{Value: "a", Plan: leaf("pc")}}}},
		{Dispatch: plan.PresenceDispatch{Property: "p", Present: leaf("pr"), Absent: leaf("ab")}},
		{Dispatch: plan.PredicateCountDispatch{Branches: []plan.CompilationPlan{leaf("cb")}}},
	}

	seen := map[planwalk.EdgeKind]bool{}
	for _, p := range plans {
		planwalk.Fold(p, struct{}{}, func(acc struct{}, n planwalk.Node) (struct{}, planwalk.Action) {
			seen[n.Edge.Kind] = true
			return acc, planwalk.Descend
		})
	}

	for k := planwalk.EdgeRoot; k <= planwalk.EdgePropertyNamesSchema; k++ {
		require.True(t, seen[k], "edge kind %s is produced by no plan shape", k)
	}
}

func TestFoldThreadsAccumulator(t *testing.T) {
	p := plan.CompilationPlan{Representation: plan.UnionRepresentation{
		Alternatives: []plan.Representation{ref("a"), ref("b"), ref("c")},
	}}

	got := planwalk.Fold(p, 0, func(acc int, n planwalk.Node) (int, planwalk.Action) {
		if _, ok := n.Representation.(plan.ReferenceRepresentation); ok {
			acc++
		}
		return acc, planwalk.Descend
	})
	require.Equal(t, 3, got)
}

func TestFoldSkipOmitsExactlyTheSubtree(t *testing.T) {
	p := plan.CompilationPlan{
		Representation: plan.UnionRepresentation{Alternatives: []plan.Representation{
			plan.ObjectRepresentation{Fields: []plan.FieldRepresentation{
				{Name: "deep", Plan: plan.CompilationPlan{Representation: ref("deep")}},
			}},
			ref("sibling"),
		}},
		Dispatch: plan.PredicateCountDispatch{Branches: []plan.CompilationPlan{leaf("branch")}},
	}

	full := foldLabels(p)
	require.Contains(t, full, "field:deep")
	require.Contains(t, full, "alternative:sibling")

	skipped := planwalk.Fold(p, nil, func(acc []string, n planwalk.Node) ([]string, planwalk.Action) {
		acc = append(acc, label(n))
		if _, ok := n.Representation.(plan.ObjectRepresentation); ok {
			return acc, planwalk.Skip
		}
		return acc, planwalk.Descend
	})

	require.NotContains(t, skipped, "field:deep")
	require.Contains(t, skipped, "alternative:sibling", "skip must continue with the next sibling")
	require.Contains(t, skipped, "count-branch:branch", "skip must not end the walk")
	require.Equal(t, len(full)-3, len(skipped), "skip must omit the subtree and nothing else")
}

func TestFoldStopEndsTheWalkImmediately(t *testing.T) {
	p := plan.CompilationPlan{
		Representation: plan.UnionRepresentation{Alternatives: []plan.Representation{
			plan.ObjectRepresentation{Fields: []plan.FieldRepresentation{
				{Name: "stop", Plan: plan.CompilationPlan{Representation: ref("stop-here")}},
			}},
			ref("never-1"),
		}},
		Dispatch: plan.PredicateCountDispatch{Branches: []plan.CompilationPlan{leaf("never-2")}},
		Validation: plan.ValidationPlan{Predicates: []plan.GuardedPredicate{
			{Expression: plan.ContainsCountPredicate{Schema: leaf("never-3")}},
		}},
	}

	visits := 0
	got := planwalk.Fold(p, nil, func(acc []string, n planwalk.Node) ([]string, planwalk.Action) {
		visits++
		acc = append(acc, label(n))
		if r, ok := n.Representation.(plan.ReferenceRepresentation); ok && r.Name == "stop-here" {
			return acc, planwalk.Stop
		}
		return acc, planwalk.Descend
	})

	require.Equal(t, []string{
		"root",
		"representation:plan.UnionRepresentation",
		"alternative:plan.ObjectRepresentation",
		"field:stop-here",
		"representation:stop-here",
	}, got)
	require.Equal(t, 5, visits, "no node may be visited after Stop")
}

func TestFoldStopAtRoot(t *testing.T) {
	p := plan.CompilationPlan{Representation: plan.UnionRepresentation{
		Alternatives: []plan.Representation{ref("a"), ref("b")},
	}}

	visits := 0
	acc := planwalk.Fold(p, "untouched", func(acc string, n planwalk.Node) (string, planwalk.Action) {
		visits++
		return acc, planwalk.Stop
	})
	require.Equal(t, 1, visits)
	require.Equal(t, "untouched", acc, "Stop returns the accumulator as it stands")
}

func TestChildrenIsOneLevel(t *testing.T) {
	obj := plan.ObjectRepresentation{
		Fields: []plan.FieldRepresentation{
			{Name: "f", Plan: plan.CompilationPlan{Representation: plan.UnionRepresentation{Alternatives: []plan.Representation{ref("nested")}}}},
		},
		Additional: &plan.CompilationPlan{Representation: ref("additional")},
	}

	var got []string
	for c := range planwalk.Children(planwalk.RepresentationNode(obj)) {
		got = append(got, label(c))
	}
	require.ElementsMatch(t, []string{"field:plan.UnionRepresentation", "additional:additional"}, got)
}

// TestChildrenBreakStops pins that abandoning a Children range does not keep walking.
func TestChildrenBreakStops(t *testing.T) {
	union := plan.UnionRepresentation{Alternatives: []plan.Representation{ref("a"), ref("b"), ref("c")}}

	n := 0
	for range planwalk.Children(planwalk.RepresentationNode(union)) {
		n++
		break
	}
	require.Equal(t, 1, n)
}

func TestFoldUnknownActionPanics(t *testing.T) {
	require.PanicsWithValue(t, "planwalk: unknown Action 7", func() {
		planwalk.Fold(plan.CompilationPlan{}, struct{}{}, func(acc struct{}, n planwalk.Node) (struct{}, planwalk.Action) {
			return acc, planwalk.Action(7)
		})
	})
}

func TestActionString(t *testing.T) {
	require.Equal(t, "descend", planwalk.Descend.String())
	require.Equal(t, "skip", planwalk.Skip.String())
	require.Equal(t, "stop", planwalk.Stop.String())
	require.Equal(t, "action(9)", planwalk.Action(9).String())
}
