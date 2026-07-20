// Package schemacompiler compiles JSON Schema Draft 2020-12 into an analyzed
// [plan.CompilationPlan] that a code generator lowers into Go types, decoders, and
// validators.
//
// It is a frontend/analysis library: it stops at the analyzed plan and does not emit
// Go source. See docs/implementation.md for the architecture.
package schemacompiler

import (
	"context"
	"net/url"

	"github.com/go-faster/errors"

	"github.com/ogen-go/schemacompiler/internal/frontend"
	"github.com/ogen-go/schemacompiler/plan"
)

// Loader fetches the raw bytes of an external schema document identified by uri (an
// absolute URI with the fragment removed). It is invoked lazily during reference
// resolution for any $ref whose target document is not the root schema. Callers supply the
// transport (filesystem, HTTP, an in-memory map, ...). A nil [Options.Loader] leaves
// external references unresolved and reported as diagnostics.
type Loader func(ctx context.Context, uri *url.URL) ([]byte, error)

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
	// Loader resolves external/remote $ref documents. When nil, only in-document references
	// resolve and external ones are reported as diagnostics.
	Loader Loader
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
	schema, err := frontend.LoadWithLoader(ctx, data, opts.BaseURI, frontend.Loader(opts.Loader))
	if err != nil {
		return nil, errors.Wrap(err, "load")
	}

	budget := opts.ExpansionBudget
	if budget <= 0 {
		budget = defaultExpansionBudget
	}

	root := buildPlan(schema.Root, schema.Registry, budget)

	// Whole-document assembly: compile every $ref target into a named definition and
	// attach the resulting reference graph to the root plan (design §10.1).
	defs := buildDefinitions(schema.Registry, budget)
	if len(defs.plans) > 0 {
		if schema.Registry.HasDynamicRefs() {
			root.Plan.Resolution = plan.DynamicReferenceGraph{StaticDefinitions: defs.plans}
		} else {
			root.Plan.Resolution = plan.StaticReferenceGraph{Definitions: defs.plans}
		}
	}

	// The document's capability and exactness are at least the worst over the root and
	// every reachable definition (design §22, §24).
	capLevel := root.Plan.Capability
	for _, d := range defs.plans {
		capLevel = maxCapability(capLevel, d.Capability)
	}
	root.Plan.Capability = capLevel

	diags := make([]plan.Diagnostic, 0, len(root.Diagnostics)+len(defs.diags)+len(schema.Unresolved)+len(schema.Uninhabited))
	diags = append(diags, root.Diagnostics...)
	diags = append(diags, defs.diags...)
	diags = append(diags, unresolvedDiagnostics(schema)...)
	diags = append(diags, uninhabitedDiagnostics(schema)...)

	return &Result{
		Plan:        root.Plan,
		Capability:  capLevel,
		Exactness:   maxExactness(root.Exactness, defs.exactness),
		Diagnostics: dedupeDiagnostics(diags),
	}, nil
}
