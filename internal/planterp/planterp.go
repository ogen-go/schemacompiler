// Package planterp interprets a [plan.CompilationPlan] as a JSON validator, for
// differential testing against the JSON-Schema-Test-Suite (issue #70).
//
// The interpreter reads the plan and nothing else: it never consults the schema text,
// the frontend, or the ir, and never re-derives a keyword's meaning from knowledge of
// what the keyword was. A constraint that did not survive into the plan cannot be
// enforced here, and the instance is accepted. That is the measurement — a plan that
// claims exactness while accepting an invalid instance has lost a constraint.
//
// Every switch over a plan interface ends in a default that returns an error, so a new
// variant fails loudly instead of being silently accepted. Struct destructuring uses the
// same binding guards as [github.com/ogen-go/schemacompiler/internal/planwalk]: a plan
// struct that gains or reorders a field stops this package compiling.
package planterp

import (
	"github.com/go-faster/errors"

	"github.com/ogen-go/schemacompiler/plan"
)

// Verdict is the outcome of interpreting a plan against one instance.
type Verdict struct {
	// Accepted reports whether the plan admits the instance.
	Accepted bool
	// Reason names the constraint that rejected it; empty when accepted.
	Reason string
	// Approximated lists constraints the interpreter could not enforce even though the
	// plan carries them: an ECMA-262 pattern Go's RE2 cannot compile, and `format`,
	// which the 2020-12 format-annotation dialect does not assert. An acceptance with a
	// non-empty Approximated is not evidence that the plan is exact.
	Approximated []string
}

func accepted() Verdict { return Verdict{Accepted: true} }

func rejected(reason string) Verdict { return Verdict{Reason: reason} }

// Interpret reports whether value satisfies p. value is a JSON document decoded by
// [encoding/json] into `any`; numbers may be float64 or [encoding/json.Number].
//
// An error means the interpreter does not understand the plan (an unknown variant, an
// unresolvable reference, a reference cycle with no instance descent) and is never a
// verdict: callers must not read it as acceptance.
func Interpret(p plan.CompilationPlan, value any) (Verdict, error) {
	in := &interp{}
	if err := in.loadDefinitions(p.Resolution); err != nil {
		return Verdict{}, err
	}
	v, err := in.plan(p, value, newFrame())
	if err != nil {
		return Verdict{}, err
	}
	v.Approximated = in.approx
	return v, nil
}

// interp holds the whole-document reference environment, which only the root plan
// carries (docs/integration.md §5, §8).
type interp struct {
	defs map[plan.SchemaID]plan.CompilationPlan
	// approx accumulates the constraints the interpreter could not enforce.
	approx []string
}

func (in *interp) loadDefinitions(r plan.ResolutionPlan) error {
	if r == nil {
		return nil
	}
	switch r := r.(type) {
	case plan.FullyResolved:
		var t struct{} = r
		_ = t
		return nil
	case plan.StaticReferenceGraph:
		var t struct {
			Definitions map[plan.SchemaID]plan.CompilationPlan
		} = r
		in.defs = t.Definitions
		return nil
	case plan.DynamicReferenceGraph:
		var t struct {
			StaticDefinitions map[plan.SchemaID]plan.CompilationPlan
			DynamicAnchors    map[string][]plan.SchemaID
		} = r
		in.defs = t.StaticDefinitions
		// DynamicAnchors describe a runtime dynamic scope the plan does not resolve
		// (design §10.2); a reference needing one fails when it is followed.
		return nil
	default:
		return errors.Errorf("planterp: unhandled plan.ResolutionPlan variant %T", r)
	}
}

// frame is the interpreter state at one instance node: the recursion binders in scope,
// and the reference names already being followed without descending into a sub-value.
type frame struct {
	binders map[string]plan.Representation
	active  map[string]bool
}

func newFrame() frame {
	return frame{binders: map[string]plan.Representation{}, active: map[string]bool{}}
}

// descend returns the frame for a sub-value: reference following starts over, since
// following a reference across an instance descent is ordinary recursion, not a cycle.
func (f frame) descend() frame {
	return frame{binders: f.binders, active: map[string]bool{}}
}

func (f frame) bind(name string, body plan.Representation) frame {
	binders := make(map[string]plan.Representation, len(f.binders)+1)
	for k, v := range f.binders {
		binders[k] = v
	}
	binders[name] = body
	return frame{binders: binders, active: f.active}
}

func (f frame) follow(name string) frame {
	active := make(map[string]bool, len(f.active)+1)
	for k := range f.active {
		active[k] = true
	}
	active[name] = true
	return frame{binders: f.binders, active: active}
}

// plan interprets one CompilationPlan: the representation must be able to hold the
// value, the residual validation must pass, and the dispatch must select a branch that
// accepts it (design §4).
//
// Resolution is not consulted here: only the root plan carries a populated graph
// (docs/integration.md §5), and [interp.defs] already holds it.
func (in *interp) plan(p plan.CompilationPlan, value any, f frame) (Verdict, error) {
	var t struct {
		Representation plan.Representation
		Validation     plan.ValidationPlan
		Dispatch       plan.DispatchPlan
		Resolution     plan.ResolutionPlan
		Capability     plan.CapabilityLevel
		Metadata       plan.Metadata
	} = p

	v, err := in.representation(t.Representation, value, f)
	if err != nil || !v.Accepted {
		return v, err
	}
	v, err = in.validation(t.Validation, value, f)
	if err != nil || !v.Accepted {
		return v, err
	}
	return in.dispatch(t.Dispatch, value, f)
}

// sub runs a nested plan against a sub-value of the instance.
func (in *interp) sub(p plan.CompilationPlan, value any, f frame) (Verdict, error) {
	return in.plan(p, value, f.descend())
}

func (in *interp) approximate(what string) {
	in.approx = append(in.approx, what)
}
