// Command schemac is a debugging CLI for the schemacompiler pipeline. It reads a single
// JSON Schema document (file argument or stdin) and prints one or more pipeline stages:
// the semantic IR, the normalized IR, the analyzed compilation plan, a Graphviz DOT
// rendering of the reference graph, and/or a Graphviz DOT rendering of the plan's
// dispatch/reference structure.
//
// Examples:
//
//	schemac -all schema.json
//	schemac -graph schema.json | dot -Tsvg > graph.svg
//	schemac -plangraph schema.json | dot -Tsvg > plan.svg
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/go-faster/errors"

	schemacompiler "github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/internal/dump"
	"github.com/ogen-go/schemacompiler/internal/frontend"
	"github.com/ogen-go/schemacompiler/internal/ir"
	"github.com/ogen-go/schemacompiler/internal/norm"
	"github.com/ogen-go/schemacompiler/plan"
)

// pipelineBudget bounds combinator expansion during normalization for the -ir/-norm/-graph
// stages, run directly against the internal packages. It mirrors the root package's
// default (schemacompiler.defaultExpansionBudget is unexported); -plan instead goes
// through schemacompiler.Compile, which applies that default itself.
const pipelineBudget = 1000

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "schemac:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("schemac", flag.ContinueOnError)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(fs.Output(), "usage: schemac [flags] [schema-file]")
		_, _ = fmt.Fprintln(fs.Output(), "reads a JSON Schema document from the file argument, or stdin if omitted.")
		fs.PrintDefaults()
	}

	var (
		showIR        = fs.Bool("ir", false, "dump the semantic IR (ir.Compile output)")
		showNorm      = fs.Bool("norm", false, "dump the normalized IR (norm.Normalize output)")
		showPlan      = fs.Bool("plan", false, "dump the analyzed CompilationPlan")
		showGraph     = fs.Bool("graph", false, "emit Graphviz DOT of the reference graph")
		showPlanGraph = fs.Bool("plangraph", false, "emit Graphviz DOT of the plan's dispatch/reference structure")
		showAll       = fs.Bool("all", false, "show IR, normalized IR, plan, graph, and plangraph")
		showDiags     = fs.Bool("diagnostics", true, "print compilation diagnostics")
		baseURI       = fs.String("base-uri", "", "retrieval URI of the root schema, for relative $ref/$id resolution")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *showAll {
		*showIR, *showNorm, *showPlan, *showGraph, *showPlanGraph = true, true, true, true, true
	}
	if !*showIR && !*showNorm && !*showPlan && !*showGraph && !*showPlanGraph {
		*showPlan = true
	}

	data, err := readInput(fs.Args())
	if err != nil {
		return errors.Wrap(err, "read input")
	}

	ctx := context.Background()

	if *showIR || *showNorm || *showGraph {
		schema, err := frontend.Load(ctx, data, *baseURI)
		if err != nil {
			return errors.Wrap(err, "load schema")
		}

		if *showIR {
			_, _ = fmt.Fprintln(stdout, "=== ir ===")
			dump.Expr(stdout, ir.Compile(schema.Root))
		}
		if *showNorm {
			_, _ = fmt.Fprintln(stdout, "=== normalized ===")
			dump.Expr(stdout, norm.Normalize(ir.Compile(schema.Root), pipelineBudget))
		}
		if *showGraph {
			_, _ = fmt.Fprintln(stdout, "=== graph ===")
			schema.WriteDOT(stdout)
		}
	}

	if *showPlan || *showPlanGraph || *showDiags {
		result, err := schemacompiler.Compile(ctx, data, schemacompiler.Options{BaseURI: *baseURI})
		if err != nil {
			return errors.Wrap(err, "compile")
		}
		if *showPlan {
			_, _ = fmt.Fprintln(stdout, "=== plan ===")
			dump.Plan(stdout, result.Plan)
		}
		if *showPlanGraph {
			_, _ = fmt.Fprintln(stdout, "=== plangraph ===")
			dump.PlanDOT(stdout, result.Plan, planDefinitions(result.Plan))
		}
		if *showDiags {
			_, _ = fmt.Fprintln(stdout, "=== diagnostics ===")
			if len(result.Diagnostics) == 0 {
				_, _ = fmt.Fprintln(stdout, "(none)")
			}
			for _, d := range result.Diagnostics {
				_, _ = fmt.Fprintf(stdout, "%s: %s: %s\n", severityString(d.Severity), d.Pointer, d.Message)
			}
		}
	}

	return nil
}

// readInput reads the schema document from args[0], or stdin when no file is given.
func readInput(args []string) ([]byte, error) {
	if len(args) > 1 {
		return nil, errors.Errorf("expected at most one schema file argument, got %d", len(args))
	}
	if len(args) == 1 {
		//nolint:gosec
		return os.ReadFile(args[0])
	}
	return io.ReadAll(os.Stdin)
}

// planDefinitions extracts the whole-document definition set from p's Resolution, for
// dump.PlanDOT to follow ReferenceRepresentation edges into named definitions.
func planDefinitions(p plan.CompilationPlan) map[plan.SchemaID]plan.CompilationPlan {
	switch res := p.Resolution.(type) {
	case plan.StaticReferenceGraph:
		return res.Definitions
	case plan.DynamicReferenceGraph:
		return res.StaticDefinitions
	default:
		return nil
	}
}

func severityString(s plan.Severity) string {
	switch s {
	case plan.SeverityInfo:
		return "info"
	case plan.SeverityWarning:
		return "warning"
	case plan.SeverityError:
		return "error"
	default:
		return fmt.Sprintf("severity(%d)", s)
	}
}
