package dump

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/ogen-go/schemacompiler/plan"
)

// PlanDOT renders p (and every plan reachable from it via dispatch branches and
// ReferenceRepresentation lookups into defs) as Graphviz DOT source, for visualizing the
// dispatch/reference structure of a whole-document plan.
//
// defs is the whole-document definition set (a [plan.StaticReferenceGraph.Definitions] or
// [plan.DynamicReferenceGraph.StaticDefinitions]); a ReferenceRepresentation whose name is
// absent from defs is drawn as a stub node. Output is deterministic: dispatch cases and
// defs are visited in a stable sorted order, and a def is rendered at most once even when
// reachable from multiple places (recursive or shared definitions).
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
// dispatch branches and any reference target.
func (g *planGraph) visitPlan(id string, p plan.CompilationPlan) {
	if g.nodeLabel == nil {
		g.nodeLabel = make(map[string]string)
	}
	g.nodeOrder = append(g.nodeOrder, id)
	g.nodeLabel[id] = planNodeLabel(p)

	g.visitDispatch(id, p.Dispatch)

	if ref, ok := p.Representation.(plan.ReferenceRepresentation); ok {
		g.visitReference(id, plan.SchemaID(ref.Name))
	}
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
	case nil, plan.NoDispatch:
		// No branches.
	case plan.KindDispatch:
		kinds := make([]plan.JSONKind, 0, len(d.Cases))
		for k := range d.Cases {
			kinds = append(kinds, k)
		}
		sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
		for _, k := range kinds {
			g.addBranch(id, jsonKindString(k), d.Cases[k])
		}
	case plan.LiteralDispatch:
		g.visitLiteralCases(id, d.Cases)
	case plan.PropertyDispatch:
		for _, c := range sortedLiteralCases(d.Cases) {
			g.addBranch(id, fmt.Sprintf("%s=%v", d.Property, c.Value), c.Plan)
		}
	case plan.PresenceDispatch:
		g.addBranch(id, "present", d.Present)
		g.addBranch(id, "absent", d.Absent)
	case plan.PredicateCountDispatch:
		for i, br := range d.Branches {
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
		return "any"
	case plan.NeverRepresentation:
		return "never"
	case plan.PrimitiveRepresentation:
		if dom := numericDomainString(r.Numeric); dom != "" {
			return jsonKindString(r.Kind) + "(" + dom + ")"
		}
		return jsonKindString(r.Kind)
	case plan.ObjectRepresentation:
		names := make([]string, 0, len(r.Fields))
		for name := range r.Fields {
			names = append(names, name)
		}
		sort.Strings(names)
		return "object{" + strings.Join(names, ",") + "}"
	case plan.ArrayRepresentation:
		return "array"
	case plan.UnionRepresentation:
		return "union"
	case plan.RecursiveRepresentation:
		return "rec:" + r.Name
	case plan.ReferenceRepresentation:
		return "ref:" + r.Name
	default:
		return fmt.Sprintf("<unknown Representation %T>", r)
	}
}
