package schemacompiler

import (
	"github.com/ogen-go/schemacompiler/internal/frontend"
	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/internal/norm"
	"github.com/ogen-go/schemacompiler/internal/planner"
	"github.com/ogen-go/schemacompiler/plan"
)

// buildPlan runs the analysis pipeline for a single schema node:
// ir.Compile → norm.Normalize → planner.Build, then re-attaches the metadata that
// ir.Compile drops.
func buildPlan(n *frontend.Node, reg *frontend.Registry, budget int) planner.Result {
	origin := planner.Origin{Pointer: n.Pointer, Position: n.Position}
	res := planner.BuildAt(norm.Normalize(ir.Compile(n), budget), reg, origin)
	annotateMetadata(&res.Plan, n)
	return res
}

// definitions is the assembled set of named $ref-target plans for a document, plus the
// diagnostics and worst-case exactness accumulated while compiling them.
type definitions struct {
	plans     map[plan.SchemaID]plan.CompilationPlan
	diags     []plan.Diagnostic
	exactness plan.Exactness
}

// buildDefinitions compiles every static $ref target in the document into its own plan,
// keyed by SchemaID, so a code generator can emit a named type per referenced schema and
// tie recursive knots (design §10.1, §19). Each target is compiled once; references
// inside a target lower to ReferenceRepresentation leaves rather than recursing here.
func buildDefinitions(reg *frontend.Registry, budget int) definitions {
	out := definitions{plans: make(map[plan.SchemaID]plan.CompilationPlan)}
	if reg == nil {
		return out
	}
	for id, node := range reg.RefTargets() {
		res := buildPlan(node, reg, budget)
		out.plans[plan.SchemaID(id)] = res.Plan
		out.diags = append(out.diags, res.Diagnostics...)
		out.exactness = maxExactness(out.exactness, res.Exactness)
	}
	return out
}

// unresolvedDiagnostics reports every dangling `$ref` the loader could not resolve as a
// SeverityError diagnostic (design §25). Loading does not fail on these, so the rest of
// the document still yields a plan.
func unresolvedDiagnostics(refs []frontend.UnresolvedRef) []plan.Diagnostic {
	if len(refs) == 0 {
		return nil
	}
	diags := make([]plan.Diagnostic, len(refs))
	for i, u := range refs {
		diags[i] = plan.Diagnostic{
			Pointer:  u.Pointer,
			Position: u.Position,
			Severity: plan.SeverityError,
			Message:  "unresolved $ref " + u.Ref + ": " + u.Reason,
		}
	}
	return diags
}

// uninhabitedDiagnostics reports every recursive schema proven to have no finite instance
// (required self-recursion) as a SeverityWarning: the schema is well-formed and its Go type
// is representable, but no value inhabits it, so a generator should not emit a dead type
// (design §25, issue #8).
func uninhabitedDiagnostics(nodes []frontend.UninhabitedNode) []plan.Diagnostic {
	if len(nodes) == 0 {
		return nil
	}
	diags := make([]plan.Diagnostic, len(nodes))
	for i, u := range nodes {
		diags[i] = plan.Diagnostic{
			Pointer:  u.Pointer,
			Position: u.Position,
			Severity: plan.SeverityWarning,
			Message:  "uninhabited schema: " + u.Reason,
		}
	}
	return diags
}

// maxCapability returns the higher (more costly) of two capability levels (design §22).
func maxCapability(a, b plan.CapabilityLevel) plan.CapabilityLevel {
	if b > a {
		return b
	}
	return a
}

// maxExactness returns the worse (less exact) of two exactness levels (design §24).
func maxExactness(a, b plan.Exactness) plan.Exactness {
	if b > a {
		return b
	}
	return a
}

// dedupeDiagnostics removes diagnostics that are identical in pointer, position, severity,
// and message — a schema referenced from several places would otherwise report the same
// finding more than once.
func dedupeDiagnostics(diags []plan.Diagnostic) []plan.Diagnostic {
	if len(diags) == 0 {
		return nil
	}
	seen := make(map[plan.Diagnostic]struct{}, len(diags))
	out := diags[:0]
	for _, d := range diags {
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		out = append(out, d)
	}
	return out
}
