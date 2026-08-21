package dump

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/ogen-go/schemacompiler/internal/planwalk"
	"github.com/ogen-go/schemacompiler/plan"
)

// PlanDOT renders p (and every plan reachable from it via dispatch branches and
// ReferenceRepresentation lookups into defs) as Graphviz DOT source, for visualizing the
// dispatch/reference structure of a whole-document plan.
//
// A reference is followed wherever [planwalk] reaches one, not only at the top of a plan's
// representation: a `$ref` nested in an object field, an array item, a union alternative or
// a recursive body draws an edge from the plan that contains it, as does one inside the
// plan a `contains`/`propertyNames` predicate carries. A plan with several references to
// the same definition draws one edge, so the graph stays a reachability picture rather than
// a syntax tree.
//
// defs is the whole-document definition set (a [plan.StaticReferenceGraph.Definitions] or
// [plan.DynamicReferenceGraph.StaticDefinitions]); a ReferenceRepresentation whose name is
// absent from defs is drawn as a stub node. Output is deterministic: dispatch cases,
// reference targets and defs are visited in a stable sorted order, and a def is rendered at
// most once even when reachable from multiple places (recursive or shared definitions).
func PlanDOT(w io.Writer, p plan.CompilationPlan, defs map[plan.SchemaID]plan.CompilationPlan) {
	g := &planGraph{defs: defs, defNode: make(map[plan.SchemaID]string)}
	rootID := g.newID()
	g.visitPlan(rootID, p)

	_, _ = fmt.Fprintln(w, "digraph plan {")
	_, _ = fmt.Fprintln(w, `  // legend: solid edge = dispatch branch, dashed edge = reference`)
	_, _ = fmt.Fprintln(w, "  rankdir=LR;")
	for _, id := range g.nodeOrder {
		_, _ = fmt.Fprintf(w, "  %s [label=%s];\n", id, strconv.Quote(g.nodeLabel[id]))
	}
	for _, e := range g.edges {
		_, _ = fmt.Fprintf(w, "  %s -> %s [style=%s%s];\n", e.from, e.to, e.style, e.labelAttr())
	}
	_, _ = fmt.Fprintln(w, "}")
}

type planEdge struct {
	from, to, style, label string
}

func (e planEdge) labelAttr() string {
	if e.label == "" {
		return ""
	}
	return ` label=` + strconv.Quote(e.label)
}

// planGraph accumulates the nodes and edges of a [PlanDOT] traversal.
type planGraph struct {
	defs map[plan.SchemaID]plan.CompilationPlan

	nextID    int
	nodeOrder []string
	nodeLabel map[string]string
	edges     []planEdge

	// defNode memoizes the node id assigned to each rendered definition, so a
	// definition reachable from multiple places (including recursively from itself)
	// is rendered exactly once.
	defNode map[plan.SchemaID]string
}

func (g *planGraph) newID() string {
	id := fmt.Sprintf("p%d", g.nextID)
	g.nextID++
	return id
}

// visitPlan renders p under the already-allocated node id, then recurses into its
// dispatch branches and into every reference target reachable from it.
func (g *planGraph) visitPlan(id string, p plan.CompilationPlan) {
	if g.nodeLabel == nil {
		g.nodeLabel = make(map[string]string)
	}
	g.nodeOrder = append(g.nodeOrder, id)
	g.nodeLabel[id] = planNodeLabel(p)

	g.visitDispatch(id, p.Dispatch)

	for _, target := range localReferenceTargets(p) {
		g.visitReference(id, target)
	}
}

// localReferenceTargets returns the sorted, deduplicated names of every
// [plan.ReferenceRepresentation] p reaches other than through its dispatch branches: the
// whole representation tree (object fields, additional and pattern values, array prefix
// and rest items, union alternatives, recursive bodies) plus the plans a residual
// predicate carries. Dispatch branches are excluded because [planGraph.visitDispatch]
// renders each of them as its own node, which draws their reference edges from there.
//
// Sorting is what keeps [PlanDOT] deterministic: the walk crosses maps
// ([plan.ObjectRepresentation.Fields]) whose iteration order is not stable.
func localReferenceTargets(p plan.CompilationPlan) []plan.SchemaID {
	seen := make(map[plan.SchemaID]struct{})
	var targets []plan.SchemaID
	collect := func(r plan.Representation) {
		ref, ok := r.(plan.ReferenceRepresentation)
		if !ok {
			return
		}
		name := plan.SchemaID(ref.Name)
		if _, dup := seen[name]; dup {
			return
		}
		seen[name] = struct{}{}
		targets = append(targets, name)
	}

	planwalk.Representation(p.Representation, collect)
	planwalk.Validation(p.Validation, func(child plan.CompilationPlan) {
		planwalk.Plan(child, collect)
	})

	sort.Slice(targets, func(i, j int) bool { return targets[i] < targets[j] })
	return targets
}

// visitReference draws a dashed "ref" edge from id to the node for target, rendering
// target's own plan from defs the first time it is seen (guarding recursive/shared
// definitions against re-rendering), or a stub node when target is absent from defs.
func (g *planGraph) visitReference(id string, target plan.SchemaID) {
	if existing, ok := g.defNode[target]; ok {
		g.edges = append(g.edges, planEdge{from: id, to: existing, style: "dashed", label: "ref"})
		return
	}

	defID := g.newID()
	g.defNode[target] = defID
	g.edges = append(g.edges, planEdge{from: id, to: defID, style: "dashed", label: "ref"})

	defPlan, ok := g.defs[target]
	if !ok {
		g.nodeOrder = append(g.nodeOrder, defID)
		g.nodeLabel[defID] = "?" + string(target)
		return
	}
	g.visitPlan(defID, defPlan)
}

// visitDispatch adds one solid, labeled edge per dispatch branch reachable from the
// plan rendered at id, and recurses into each branch.
func (g *planGraph) visitDispatch(id string, d plan.DispatchPlan) {
	switch d := d.(type) {
	case nil:
		// No branches.
	case plan.NoDispatch:
		var t struct{} = d
		_ = t
	case plan.KindDispatch:
		var t struct {
			Cases map[plan.JSONKind]plan.CompilationPlan
		} = d
		kinds := make([]plan.JSONKind, 0, len(t.Cases))
		for k := range t.Cases {
			kinds = append(kinds, k)
		}
		sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
		for _, k := range kinds {
			g.addBranch(id, jsonKindString(k), t.Cases[k])
		}
	case plan.LiteralDispatch:
		var t struct {
			Cases []plan.LiteralCase
		} = d
		g.visitLiteralCases(id, t.Cases)
	case plan.PropertyDispatch:
		var t struct {
			Property string
			Cases    []plan.LiteralCase
			Tag      plan.TagSource
		} = d
		for _, c := range sortedLiteralCases(t.Cases) {
			g.addBranch(id, fmt.Sprintf("%s=%v", t.Property, c.Value), c.Plan)
		}
	case plan.PresenceDispatch:
		var t struct {
			Property string
			Present  plan.CompilationPlan
			Absent   plan.CompilationPlan
		} = d
		g.addBranch(id, "present", t.Present)
		g.addBranch(id, "absent", t.Absent)
	case plan.PredicateCountDispatch:
		var t struct {
			Branches []plan.CompilationPlan
			Minimum  int
			Maximum  int
		} = d
		for i, br := range t.Branches {
			g.addBranch(id, fmt.Sprintf("branch %d", i), br)
		}
	default:
		g.addBranch(id, fmt.Sprintf("<unknown DispatchPlan %T>", d), plan.CompilationPlan{})
	}
}

func (g *planGraph) visitLiteralCases(id string, cases []plan.LiteralCase) {
	for _, c := range sortedLiteralCases(cases) {
		g.addBranch(id, fmt.Sprintf("%v", c.Value), c.Plan)
	}
}

// sortedLiteralCases returns cases ordered by a stable, deterministic key (the literal
// value's formatted form), since JSON literals need not otherwise be orderable.
func sortedLiteralCases(cases []plan.LiteralCase) []plan.LiteralCase {
	out := append([]plan.LiteralCase(nil), cases...)
	sort.Slice(out, func(i, j int) bool {
		return fmt.Sprintf("%v", out[i].Value) < fmt.Sprintf("%v", out[j].Value)
	})
	return out
}

func (g *planGraph) addBranch(parent, label string, branch plan.CompilationPlan) {
	childID := g.newID()
	g.edges = append(g.edges, planEdge{from: parent, to: childID, style: "solid", label: label})
	g.visitPlan(childID, branch)
}

// planNodeLabel summarizes a plan's representation and capability for display, e.g.
// "string [direct-go-type]" or "object{a,b} [go-type-with-validation]". A
// PredicateCountDispatch's match-count window is folded into the label since it isn't
// otherwise attached to any single edge.
func planNodeLabel(p plan.CompilationPlan) string {
	label := representationSummary(p.Representation) + " [" + capabilityString(p.Capability) + "]"
	if cd, ok := p.Dispatch.(plan.PredicateCountDispatch); ok {
		label += fmt.Sprintf(" count[%d,%d]", cd.Minimum, cd.Maximum)
	}
	return label
}

// representationSummary renders a short, one-line summary of a Representation.
func representationSummary(r plan.Representation) string {
	switch r := r.(type) {
	case nil:
		return "<nil>"
	case plan.AnyRepresentation:
		var t struct{} = r
		_ = t
		return "any"
	case plan.NeverRepresentation:
		var t struct{} = r
		_ = t
		return "never"
	case plan.PrimitiveRepresentation:
		var t struct {
			Kind    plan.JSONKind
			Numeric plan.NumericDomain
			Format  string
		} = r
		if dom := numericDomainString(t.Numeric); dom != "" {
			return jsonKindString(t.Kind) + "(" + dom + ")"
		}
		return jsonKindString(t.Kind)
	case plan.ObjectRepresentation:
		var t struct {
			Fields       []plan.FieldRepresentation
			Additional   *plan.CompilationPlan
			PatternRules []plan.PatternFieldRepresentation
		} = r
		names := make([]string, 0, len(t.Fields))
		for _, f := range t.Fields {
			names = append(names, f.Name)
		}
		return "object{" + strings.Join(names, ",") + "}"
	case plan.ArrayRepresentation:
		var t struct {
			Prefix []plan.ItemRepresentation
			Rest   plan.ItemRepresentation
		} = r
		_ = t
		return "array"
	case plan.UnionRepresentation:
		var t struct {
			Alternatives []plan.Representation
		} = r
		_ = t
		return "union"
	case plan.RecursiveRepresentation:
		var t struct {
			Name string
			Body plan.Representation
		} = r
		return "rec:" + t.Name
	case plan.ReferenceRepresentation:
		var t struct {
			Name string
		} = r
		return "ref:" + t.Name
	default:
		return fmt.Sprintf("<unknown Representation %T>", r)
	}
}
