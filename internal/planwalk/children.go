package planwalk

import (
	"fmt"
	"iter"

	"github.com/ogen-go/schemacompiler/plan"
)

// Children iterates n's direct children, each carrying the [Edge] it hangs off n by.
//
// It is [Fold] stopped at one level, and it is what an instance-directed consumer wants:
// such a consumer visits the children the instance selects, in an order and a
// multiplicity the instance decides (one Additional child answers for every uncovered
// property, one Rest item for every element past the prefix), so it must drive its own
// recursion rather than let the traversal drive it.
func Children(n Node) iter.Seq[Node] {
	return func(yield func(Node) bool) { children(n, yield) }
}

// children yields n's direct children in traversal order, stopping as soon as yield
// returns false.
//
// This is where the completeness guards live: every case destructures its value into an
// anonymous struct and reads THAT binding, so a plan struct that gains, renames, retypes
// or reorders a field stops the package compiling until the traversal names the change.
func children(n Node, yield func(Node) bool) {
	switch n.Kind {
	case NodePlan:
		planChildren(n.Plan, yield)
	case NodeRepresentation:
		representationChildren(n.Representation, yield)
	case NodeDispatch:
		dispatchChildren(n.Dispatch, yield)
	case NodeValidation:
		validationChildren(n.Validation, yield)
	case NodePredicate:
		predicateChildren(n.Predicate, yield)
	default:
		panic(fmt.Sprintf("planwalk: unhandled NodeKind %d", uint8(n.Kind)))
	}
}

func planChildren(p plan.CompilationPlan, yield func(Node) bool) {
	var t struct {
		Representation plan.Representation
		Validation     plan.ValidationPlan
		Dispatch       plan.DispatchPlan
		Resolution     plan.ResolutionPlan
		Capability     plan.CapabilityLevel
		Metadata       plan.Metadata
	} = p

	if t.Representation != nil {
		if !yield(Node{
			Kind:           NodeRepresentation,
			Edge:           Edge{Kind: EdgeRepresentation},
			Representation: t.Representation,
		}) {
			return
		}
	}
	if t.Dispatch != nil {
		if !yield(Node{Kind: NodeDispatch, Edge: Edge{Kind: EdgeDispatch}, Dispatch: t.Dispatch}) {
			return
		}
	}
	yield(Node{Kind: NodeValidation, Edge: Edge{Kind: EdgeValidation}, Validation: t.Validation})
}

//nolint:gocyclo // one case per plan.Representation variant; splitting it would only hide the exhaustiveness.
func representationChildren(r plan.Representation, yield func(Node) bool) {
	if r == nil {
		return
	}

	switch r := r.(type) {
	case plan.AnyRepresentation:
		var t struct{} = r
		_ = t
	case plan.NeverRepresentation:
		var t struct{} = r
		_ = t
	case plan.PrimitiveRepresentation:
		var t struct {
			Kind    plan.JSONKind
			Numeric plan.NumericDomain
			Format  string
		} = r
		_ = t
	case plan.ObjectRepresentation:
		var t struct {
			Fields       []plan.FieldRepresentation
			Additional   *plan.CompilationPlan
			PatternRules []plan.PatternFieldRepresentation
		} = r
		for i, f := range t.Fields {
			var g struct {
				Name     string
				Plan     plan.CompilationPlan
				Presence plan.PresenceMode
				Nullable bool
				Metadata plan.Metadata
			} = f
			if !yield(Node{Kind: NodePlan, Edge: Edge{
				Kind:     EdgeField,
				Name:     g.Name,
				Index:    i,
				Presence: g.Presence,
				Nullable: g.Nullable,
			}, Plan: g.Plan}) {
				return
			}
		}
		if t.Additional != nil {
			if !yield(Node{Kind: NodePlan, Edge: Edge{Kind: EdgeAdditional}, Plan: *t.Additional}) {
				return
			}
		}
		for i, pr := range t.PatternRules {
			var g struct {
				Pattern  string
				Plan     plan.CompilationPlan
				Metadata plan.Metadata
			} = pr
			if !yield(Node{Kind: NodePlan, Edge: Edge{
				Kind:  EdgePatternRule,
				Name:  g.Pattern,
				Index: i,
			}, Plan: g.Plan}) {
				return
			}
		}
	case plan.ArrayRepresentation:
		var t struct {
			Prefix []plan.ItemRepresentation
			Rest   plan.ItemRepresentation
		} = r
		for i, it := range t.Prefix {
			if !yield(Node{Kind: NodePlan, Edge: Edge{Kind: EdgePrefixItem, Index: i}, Plan: itemPlan(it)}) {
				return
			}
		}
		if rest := itemPlan(t.Rest); rest.Representation != nil {
			if !yield(Node{Kind: NodePlan, Edge: Edge{Kind: EdgeRestItem}, Plan: rest}) {
				return
			}
		}
	case plan.UnionRepresentation:
		var t struct {
			Alternatives []plan.Representation
		} = r
		for i, alt := range t.Alternatives {
			if !subRepresentation(alt, Edge{Kind: EdgeAlternative, Index: i}, yield) {
				return
			}
		}
	case plan.RecursiveRepresentation:
		var t struct {
			Name string
			Body plan.Representation
		} = r
		if !subRepresentation(t.Body, Edge{Kind: EdgeRecursiveBody, Name: t.Name}, yield) {
			return
		}
	case plan.ReferenceRepresentation:
		var t struct {
			Name string
		} = r
		_ = t
	default:
		panic(fmt.Sprintf("planwalk: unhandled plan.Representation variant %T", r))
	}
}

func itemPlan(i plan.ItemRepresentation) plan.CompilationPlan {
	var t struct {
		Plan     plan.CompilationPlan
		Metadata plan.Metadata
	} = i
	return t.Plan
}

// subRepresentation yields one representation child, dropping an absent one. It reports
// whether the traversal should continue.
func subRepresentation(r plan.Representation, edge Edge, yield func(Node) bool) bool {
	if r == nil {
		return true
	}
	return yield(Node{Kind: NodeRepresentation, Edge: edge, Representation: r})
}

func dispatchChildren(d plan.DispatchPlan, yield func(Node) bool) {
	if d == nil {
		return
	}

	switch d := d.(type) {
	case plan.NoDispatch:
		var t struct{} = d
		_ = t
	case plan.KindDispatch:
		var t struct {
			Cases map[plan.JSONKind]plan.CompilationPlan
		} = d
		for kind, branch := range t.Cases {
			if !yield(Node{Kind: NodePlan, Edge: Edge{Kind: EdgeKindCase, Case: kind}, Plan: branch}) {
				return
			}
		}
	case plan.LiteralDispatch:
		var t struct {
			Cases []plan.LiteralCase
		} = d
		literalCaseChildren(t.Cases, Edge{Kind: EdgeLiteralCase}, yield)
	case plan.PropertyDispatch:
		var t struct {
			Property string
			Cases    []plan.LiteralCase
			Tag      plan.TagSource
		} = d
		literalCaseChildren(t.Cases, Edge{Kind: EdgePropertyCase, Name: t.Property, Tag: t.Tag}, yield)
	case plan.PresenceDispatch:
		var t struct {
			Property string
			Present  plan.CompilationPlan
			Absent   plan.CompilationPlan
		} = d
		if !yield(Node{Kind: NodePlan, Edge: Edge{Kind: EdgePresent, Name: t.Property}, Plan: t.Present}) {
			return
		}
		if !yield(Node{Kind: NodePlan, Edge: Edge{Kind: EdgeAbsent, Name: t.Property}, Plan: t.Absent}) {
			return
		}
	case plan.PredicateCountDispatch:
		var t struct {
			Branches []plan.CompilationPlan
			Minimum  int
			Maximum  int
		} = d
		for i, branch := range t.Branches {
			if !yield(Node{Kind: NodePlan, Edge: Edge{Kind: EdgeCountBranch, Index: i}, Plan: branch}) {
				return
			}
		}
	default:
		panic(fmt.Sprintf("planwalk: unhandled plan.DispatchPlan variant %T", d))
	}
}

func literalCaseChildren(cases []plan.LiteralCase, edge Edge, yield func(Node) bool) {
	for i, c := range cases {
		var t struct {
			Value any
			Raw   []byte
			Plan  plan.CompilationPlan
		} = c

		e := edge
		e.Index, e.Value, e.Raw = i, t.Value, t.Raw
		if !yield(Node{Kind: NodePlan, Edge: e, Plan: t.Plan}) {
			return
		}
	}
}

func validationChildren(v plan.ValidationPlan, yield func(Node) bool) {
	var t struct {
		Predicates []plan.GuardedPredicate
	} = v

	for i, gp := range t.Predicates {
		var g struct {
			Applicability plan.KindSet
			Assert        bool
			Expression    plan.PredicateExpr
		} = gp
		// A kind assertion is the one predicate with no Expression, and it still has to
		// be visited: dropping it would make the check invisible to every consumer.
		if g.Expression == nil && !g.Assert {
			continue
		}
		if !yield(Node{
			Kind:      NodePredicate,
			Edge:      Edge{Kind: EdgeGuardedPredicate, Index: i, Applicability: g.Applicability, Assert: g.Assert},
			Predicate: g.Expression,
		}) {
			return
		}
	}
}

//nolint:gocyclo // one case per plan.PredicateExpr variant; splitting it would only hide the exhaustiveness.
func predicateChildren(e plan.PredicateExpr, yield func(Node) bool) {
	if e == nil {
		return
	}

	switch e := e.(type) {
	case plan.MinLengthPredicate:
		var t struct{ Value uint64 } = e
		_ = t
	case plan.MaxLengthPredicate:
		var t struct{ Value uint64 } = e
		_ = t
	case plan.PatternPredicate:
		var t struct{ Regex string } = e
		_ = t
	case plan.FormatPredicate:
		var t struct{ Format string } = e
		_ = t
	case plan.MinimumPredicate:
		var t struct {
			Value     float64
			Exclusive bool
		} = e
		_ = t
	case plan.MaximumPredicate:
		var t struct {
			Value     float64
			Exclusive bool
		} = e
		_ = t
	case plan.NumericDomainPredicate:
		var t struct{ Domain plan.NumericDomain } = e
		_ = t
	case plan.MultipleOfPredicate:
		var t struct{ Value float64 } = e
		_ = t
	case plan.MinItemsPredicate:
		var t struct{ Value uint64 } = e
		_ = t
	case plan.MaxItemsPredicate:
		var t struct{ Value uint64 } = e
		_ = t
	case plan.UniqueItemsPredicate:
		var t struct{} = e
		_ = t
	case plan.ContainsCountPredicate:
		var t struct {
			Schema plan.CompilationPlan
			Min    uint64
			Max    *uint64
		} = e
		if !yield(Node{Kind: NodePlan, Edge: Edge{Kind: EdgeContainsSchema}, Plan: t.Schema}) {
			return
		}
	case plan.NegationPredicate:
		var t struct{ Schema plan.CompilationPlan } = e
		if !yield(Node{Kind: NodePlan, Edge: Edge{Kind: EdgeNegationSchema}, Plan: t.Schema}) {
			return
		}
	case plan.RequiredPredicate:
		var t struct{ Properties []string } = e
		_ = t
	case plan.MinPropertiesPredicate:
		var t struct{ Value uint64 } = e
		_ = t
	case plan.MaxPropertiesPredicate:
		var t struct{ Value uint64 } = e
		_ = t
	case plan.DependentRequiredPredicate:
		var t struct{ Entries []plan.DependentRequiredEntry } = e
		_ = t
	case plan.PropertyNamesPredicate:
		var t struct{ Schema plan.CompilationPlan } = e
		if !yield(Node{Kind: NodePlan, Edge: Edge{Kind: EdgePropertyNamesSchema}, Plan: t.Schema}) {
			return
		}
	case plan.ShapePredicate:
		var t struct{ Schema plan.CompilationPlan } = e
		if !yield(Node{Kind: NodePlan, Edge: Edge{Kind: EdgeShapeSchema}, Plan: t.Schema}) {
			return
		}
	case plan.ObjectStructurePredicate:
		// The same edges an ObjectRepresentation uses: the relation "this plan governs
		// the property named N" is the same one, so a generic consumer needs no new case.
		var t struct {
			Properties []plan.PropertyCheck
			Patterns   []plan.PatternCheck
			Additional *plan.CompilationPlan
		} = e
		for i, pc := range t.Properties {
			var c struct {
				Name     string
				Plan     plan.CompilationPlan
				Presence plan.PresenceMode
				Nullable bool
			} = pc
			if !yield(Node{
				Kind: NodePlan,
				Edge: Edge{Kind: EdgeField, Index: i, Name: c.Name, Presence: c.Presence, Nullable: c.Nullable},
				Plan: c.Plan,
			}) {
				return
			}
		}
		for i, pc := range t.Patterns {
			var c struct {
				Pattern string
				Plan    plan.CompilationPlan
			} = pc
			if !yield(Node{
				Kind: NodePlan,
				Edge: Edge{Kind: EdgePatternRule, Index: i, Name: c.Pattern},
				Plan: c.Plan,
			}) {
				return
			}
		}
		if t.Additional != nil {
			if !yield(Node{Kind: NodePlan, Edge: Edge{Kind: EdgeAdditional}, Plan: *t.Additional}) {
				return
			}
		}
	case plan.ArrayStructurePredicate:
		var t struct {
			Prefix []plan.CompilationPlan
			Rest   *plan.CompilationPlan
		} = e
		for i, sub := range t.Prefix {
			if !yield(Node{Kind: NodePlan, Edge: Edge{Kind: EdgePrefixItem, Index: i}, Plan: sub}) {
				return
			}
		}
		if t.Rest != nil {
			if !yield(Node{Kind: NodePlan, Edge: Edge{Kind: EdgeRestItem}, Plan: *t.Rest}) {
				return
			}
		}
	default:
		panic(fmt.Sprintf("planwalk: unhandled plan.PredicateExpr variant %T", e))
	}
}
