package schemacompiler

import (
	"github.com/ogen-go/schemacompiler/internal/frontend"
	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/internal/norm"
	"github.com/ogen-go/schemacompiler/internal/planner"
	"github.com/ogen-go/schemacompiler/plan"
)

// buildPlan runs the analysis pipeline for a single schema node:
// ir.Compile → norm.Normalize → planner.Build.
func buildPlan(n *frontend.Node, reg *frontend.Registry, budget int) planner.Result {
	return planner.Build(norm.Normalize(ir.Compile(n), budget), reg)
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

// dedupeDiagnostics removes diagnostics that are identical in pointer, severity, and
// message — a schema referenced from several places would otherwise report the same
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
