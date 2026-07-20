// Package schemacompiler compiles JSON Schema Draft 2020-12 into an analyzed
// [plan.CompilationPlan] that a code generator lowers into Go types, decoders, and
// validators.
//
// It is a frontend/analysis library: it stops at the analyzed plan and does not emit
// Go source. See docs/implementation.md for the architecture.
package schemacompiler

import (
	"context"

	"github.com/go-faster/errors"

	"github.com/ogen-go/schemacompiler/internal/frontend"
	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/internal/norm"
	"github.com/ogen-go/schemacompiler/internal/planner"
	"github.com/ogen-go/schemacompiler/plan"
)

// defaultExpansionBudget bounds combinator expansion during normalization when
// [Options.ExpansionBudget] is zero.
const defaultExpansionBudget = 1000

// Options configures a compilation.
type Options struct {
	// BaseURI is the retrieval URI of the root schema, used to resolve relative $ref/$id.
	BaseURI string
	// ExpansionBudget bounds combinator expansion during normalization; when exceeded a
	// factored predicate-dispatch form is preserved instead of exponential IR (design §21).
	// Zero selects a conservative default.
	ExpansionBudget int
}

// Result is the public output of compilation (design §25).
type Result struct {
	// Plan is the analyzed compilation plan for the root schema.
	Plan plan.CompilationPlan
	// Capability mirrors Plan.Capability for convenience.
	Capability plan.CapabilityLevel
	// Exactness reports how faithfully the representation reproduces the schema.
	Exactness plan.Exactness
	// Diagnostics explain capability or exactness downgrades.
	Diagnostics []plan.Diagnostic
}

// Compile parses, resolves, normalizes, and analyzes a single JSON Schema document.
//
// data is a standalone Draft 2020-12 schema (JSON or YAML). Callers that already hold a
// parsed libopenapi schema (e.g. ogen) should use the frontend adapter directly rather
// than re-serializing.
func Compile(ctx context.Context, data []byte, opts Options) (*Result, error) {
	schema, err := frontend.Load(ctx, data, opts.BaseURI)
	if err != nil {
		return nil, errors.Wrap(err, "load")
	}

	budget := opts.ExpansionBudget
	if budget <= 0 {
		budget = defaultExpansionBudget
	}

	expr := norm.Normalize(ir.Compile(schema.Root), budget)
	res := planner.Build(expr, schema.Registry)

	return &Result{
		Plan:        res.Plan,
		Capability:  res.Plan.Capability,
		Exactness:   res.Exactness,
		Diagnostics: res.Diagnostics,
	}, nil
}
