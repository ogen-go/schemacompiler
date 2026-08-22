// Package planterp interprets a [plan.CompilationPlan] as a JSON validator, for
// differential testing against the JSON-Schema-Test-Suite (issue #70).
//
// The interpreter reads the plan and nothing else: it never consults the schema text,
// the frontend, or the ir, and never re-derives a keyword's meaning from knowledge of
// what the keyword was. A constraint that did not survive into the plan cannot be
// enforced here, and the instance is accepted. That is the measurement — a plan that
// claims exactness while accepting an invalid instance has lost a constraint.
//
// Structure comes from [github.com/ogen-go/schemacompiler/internal/planwalk]: child
// nodes and their [planwalk.Edge] payloads are enumerated there, so the binding guards
// that stop a plan struct changing shape unnoticed live in that package alone. What
// stays here is the instance-directed half — which child to visit, how often, and
// against which sub-value — plus a variant switch per plan interface whose default
// returns an [InternalError], so a new variant fails loudly instead of being silently
// accepted.
package planterp

import (
	"github.com/ogen-go/schemacompiler/internal/planwalk"
	"github.com/ogen-go/schemacompiler/plan"
)

// Verdict is the outcome of interpreting a plan against one instance.
type Verdict struct {
	// Accepted reports whether the plan admits the instance.
	Accepted bool
	// Reason is the rejection: where it happened, which constraint rejected, and the
	// failure underneath it. Nil when accepted.
	Reason *ValidateError
	// Approximated lists constraints the interpreter could not enforce even though the
	// plan carries them: a `pattern` the ECMA-262 engine cannot compile or cannot finish
	// matching, and `format`, which the 2020-12 format-annotation dialect does not assert.
	// An acceptance with a non-empty Approximated is not evidence that the plan is exact.
	Approximated []string
}

func accepted() Verdict { return Verdict{Accepted: true} }

func rejected(f frame, constraint, detail string) Verdict {
	return rejectedBy(f, constraint, detail, nil)
}

func rejectedBy(f frame, constraint, detail string, cause *ValidateError) Verdict {
	return Verdict{Reason: &ValidateError{
		Path:       f.path,
		Constraint: constraint,
		Detail:     detail,
		Cause:      cause,
	}}
}

// Interpret reports whether value satisfies p. value is a JSON document decoded by
// [encoding/json] into `any`; numbers may be float64 or [encoding/json.Number].
//
// The error slot never carries a [ValidateError]: a rejection is a verdict, not an
// error. It carries an [InternalError] when the interpreter cannot read the plan, and an
// [InvalidValueError] when value is not a decoded JSON document. Neither is a verdict:
// callers must not read either as acceptance.
func Interpret(p plan.CompilationPlan, value any) (Verdict, error) {
	return interpret(p, value, false)
}

// InterpretChecks is [Interpret] with the representation ignored: acceptance comes from
// the validation plan and the dispatch alone, which design §4.1 says is the whole of a
// plan's contract.
//
// It exists to be run beside [Interpret] over the corpus. Agreement on every instance is
// the evidence that acceptance really is independent of storage — a claim the design
// makes and that nothing else re-checks, so a plan that quietly moves a constraint back
// into its Go shape shows up as a disagreement rather than as prose going stale
// (issue #115).
func InterpretChecks(p plan.CompilationPlan, value any) (Verdict, error) {
	return interpret(p, value, true)
}

func interpret(p plan.CompilationPlan, value any, ignoreRepresentation bool) (Verdict, error) {
	in := &interp{ignoreRepresentation: ignoreRepresentation}
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
	// ignoreRepresentation drops the representation from the acceptance decision, leaving
	// only the checks design §4.1 makes the plan's contract. See [InterpretChecks].
	ignoreRepresentation bool
	// approx accumulates the constraints the interpreter could not enforce.
	approx []string
}

// loadDefinitions reads the document's reference graph. planwalk deliberately does not
// descend Resolution — its definitions are whole-document plans, visited once per
// reference if it did — so the binding guard on these variants lives here.
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
		return internalf("unhandled plan.ResolutionPlan variant %T", r)
	}
}

// frame is the interpreter state at one instance node: where in the instance it is, the
// recursion binders in scope, and the reference names already being followed without
// descending into a sub-value.
type frame struct {
	path    string
	binders map[string]plan.Representation
	active  map[string]bool
}

func newFrame() frame {
	return frame{binders: map[string]plan.Representation{}, active: map[string]bool{}}
}

// descend returns the frame for the sub-value at token: the path grows by one JSON
// Pointer reference token, and reference following starts over, since following a
// reference across an instance descent is ordinary recursion, not a cycle.
func (f frame) descend(token string) frame {
	return frame{path: pointerAppend(f.path, token), binders: f.binders, active: map[string]bool{}}
}

// here is [frame.descend] for a sub-plan run against the same instance node, as `not`
// and `propertyNames` are: the location does not move, but reference following starts
// over just the same.
func (f frame) here() frame {
	return frame{path: f.path, binders: f.binders, active: map[string]bool{}}
}

func (f frame) bind(name string, body plan.Representation) frame {
	binders := make(map[string]plan.Representation, len(f.binders)+1)
	for k, v := range f.binders {
		binders[k] = v
	}
	binders[name] = body
	return frame{path: f.path, binders: binders, active: f.active}
}

func (f frame) follow(name string) frame {
	active := make(map[string]bool, len(f.active)+1)
	for k := range f.active {
		active[k] = true
	}
	active[name] = true
	return frame{path: f.path, binders: f.binders, active: active}
}

// plan interprets one CompilationPlan: the representation must be able to hold the
// value, the residual validation must pass, and the dispatch must select a branch that
// accepts it (design §4).
//
// Resolution is not consulted here: only the root plan carries a populated graph
// (docs/integration.md §5), and [interp.defs] already holds it.
func (in *interp) plan(p plan.CompilationPlan, value any, f frame) (Verdict, error) {
	var (
		representation plan.Representation
		validation     plan.ValidationPlan
		dispatch       plan.DispatchPlan
	)
	for c := range planwalk.Children(planwalk.PlanNode(p)) {
		switch c.Edge.Kind {
		case planwalk.EdgeRepresentation:
			representation = c.Representation
		case planwalk.EdgeValidation:
			validation = c.Validation
		case planwalk.EdgeDispatch:
			dispatch = c.Dispatch
		default:
			return Verdict{}, internalf("unhandled plan child edge %s", c.Edge.Kind)
		}
	}

	v, err := in.representation(representation, value, f)
	if err != nil || !v.Accepted {
		return v, err
	}
	v, err = in.validation(validation, value, f)
	if err != nil || !v.Accepted {
		return v, err
	}
	return in.dispatch(dispatch, value, f)
}

func (in *interp) approximate(what string) {
	in.approx = append(in.approx, what)
}
