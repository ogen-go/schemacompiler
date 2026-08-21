package schemacompiler

import (
	"context"
	"maps"
	"slices"

	"github.com/go-faster/errors"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/pb33f/libopenapi/orderedmap"

	"github.com/ogen-go/schemacompiler/internal/frontend"
	"github.com/ogen-go/schemacompiler/plan"
)

// Document is an OpenAPI component schema set to compile as a whole.
type Document struct {
	// BaseURI is the retrieval URI of the document, used to resolve relative $ref/$id.
	// An empty value falls back to [Options.BaseURI].
	BaseURI string
	// Schemas is the document's `components.schemas`, already parsed by libopenapi.
	Schemas *orderedmap.Map[string, *base.SchemaProxy]
}

// DocumentResult is the output of compiling a whole document (design §25).
type DocumentResult struct {
	// Plans holds one analyzed plan per named schema, keyed by its JSON Pointer
	// (`/components/schemas/<name>`), plus a plan for every other static $ref target
	// reachable from them. It is the reference graph a generator resolves
	// [plan.ReferenceRepresentation] names against.
	Plans map[plan.SchemaID]plan.CompilationPlan
	// Capability is the worst capability level over every plan.
	Capability plan.CapabilityLevel
	// Exactness is the worst exactness level over every plan.
	Exactness plan.Exactness
	// Diagnostics explain capability or exactness downgrades.
	Diagnostics []plan.Diagnostic
}

// CompileDocument compiles an OpenAPI document's component schemas as one unit: all of
// them share a single registry, so `$ref`s between siblings resolve and recursion is
// classified across component boundaries.
func CompileDocument(ctx context.Context, doc Document, opts Options) (*DocumentResult, error) {
	baseURI := doc.BaseURI
	if baseURI == "" {
		baseURI = opts.BaseURI
	}

	d, err := frontend.FromLibOpenAPIDocument(ctx, doc.Schemas, baseURI)
	if err != nil {
		return nil, errors.Wrap(err, "convert document")
	}
	budget := expansionBudget(opts)

	res := &DocumentResult{Plans: make(map[plan.SchemaID]plan.CompilationPlan, len(d.Schemas))}
	var diags []plan.Diagnostic
	for _, pointer := range slices.Sorted(maps.Keys(d.Schemas)) {
		built := buildPlan(d.Schemas[pointer], d.Registry, budget)
		res.Plans[plan.SchemaID(pointer)] = built.Plan
		res.Capability = maxCapability(res.Capability, built.Plan.Capability)
		res.Exactness = maxExactness(res.Exactness, built.Exactness)
		diags = append(diags, built.Diagnostics...)
	}

	// A $ref may target a schema nested inside a component rather than a component root;
	// those need a plan under their own pointer too.
	defs := buildDefinitions(d.Registry, budget)
	for id, p := range defs.plans {
		if _, ok := res.Plans[id]; ok {
			continue
		}
		res.Plans[id] = p
		res.Capability = maxCapability(res.Capability, p.Capability)
	}
	res.Exactness = maxExactness(res.Exactness, defs.exactness)
	diags = append(diags, defs.diags...)

	diags = append(diags, unresolvedDiagnostics(d.Unresolved)...)
	diags = append(diags, uninhabitedDiagnostics(d.Uninhabited)...)
	res.Diagnostics = dedupeDiagnostics(diags)
	return res, nil
}

// CompileSchema compiles a single already-parsed libopenapi schema, the join point for a
// caller that holds one component rather than a whole document (e.g. ogen). References to
// sibling components stay unresolved: use [CompileDocument] to resolve those.
func CompileSchema(ctx context.Context, sp *base.SchemaProxy, opts Options) (*Result, error) {
	schema, err := frontend.FromLibOpenAPIProxy(ctx, sp, opts.BaseURI)
	if err != nil {
		return nil, errors.Wrap(err, "convert schema")
	}
	return compileSchema(schema, expansionBudget(opts)), nil
}
