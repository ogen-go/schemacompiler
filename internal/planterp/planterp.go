// Package planterp interprets a [plan.CompilationPlan] as a JSON validator, for
// differential testing against the JSON-Schema-Test-Suite (issue #70).
//
// The interpreter reads the plan and nothing else: it never consults the schema text,
// the frontend, or the ir, and never re-derives a keyword's meaning from knowledge of
// what the keyword was. A constraint that did not survive into the plan cannot be
// enforced here, and the instance is accepted. That is the measurement — a plan that
// reports no unenforced construct while accepting an invalid instance has lost one.
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

// loadDefinitions reads the document's reference graph. planwalk deliberately does not
// descend Resolution — its definitions are whole-document plans, visited once per
// reference if it did — so the binding guard on these variants lives here.
func (in *interp) loadDefinitions(r plan.ResolutionPlan) error {
	if r == nil {
		return nil
	}
	switch r := r.(type) {
	case *plan.FullyResolved:
		var t struct{} = *r
		_ = t
		return nil
	case *plan.StaticReferenceGraph:
		var t struct {
			Definitions map[plan.SchemaID]plan.CompilationPlan
		} = *r
		in.defs = t.Definitions
		return nil
	case *plan.DynamicReferenceGraph:
		var t struct {
			StaticDefinitions map[plan.SchemaID]plan.CompilationPlan
			DynamicAnchors    map[string][]plan.SchemaID
		} = *r
		in.defs = t.StaticDefinitions
		// DynamicAnchors describe a runtime dynamic scope the plan does not resolve
		// (design §10.2); a reference needing one fails when it is followed.
		return nil
	default:
		return internalf("unhandled plan.ResolutionPlan variant %T", r)
	}
}

// frame is the interpreter state at one instance node: where in the instance it is, and
// the reference names already being followed without descending into a sub-value.
type frame struct {
	path   string
	active map[string]bool
}

func newFrame() frame {
	return frame{active: map[string]bool{}}
}

// descend returns the frame for the sub-value at token: the path grows by one JSON
// Pointer reference token, and reference following starts over, since following a
// reference across an instance descent is ordinary recursion, not a cycle.
func (f frame) descend(token string) frame {
	return frame{path: pointerAppend(f.path, token), active: map[string]bool{}}
}

// here is [frame.descend] for a sub-plan run against the same instance node, as `not`
// and `propertyNames` are: the location does not move, but reference following starts
// over just the same.
func (f frame) here() frame {
	return frame{path: f.path, active: map[string]bool{}}
}

func (f frame) follow(name string) frame {
	active := make(map[string]bool, len(f.active)+1)
	for k := range f.active {
		active[k] = true
	}
	active[name] = true
	return frame{path: f.path, active: active}
}

// checks is the part of a plan the interpreter is allowed to see.
//
// Design §4.1 makes the validation plan and the dispatch the whole of what a plan
// accepts; the representation is where a value is stored, not what is admitted. That is
// enforced here by construction rather than by discipline: the evaluator below is handed
// a checks, so a representation is not something it declines to read, it is something it
// never holds. [checksOf] is the one place a [plan.CompilationPlan] is narrowed to one.
type checks struct {
	validation plan.ValidationPlan
	dispatch   plan.DispatchPlan
}

// checksOf drops a plan to the part that decides acceptance.
//
// Resolution is not carried: only the root plan has a populated graph
// (docs/integration.md §5), and [interp.defs] already holds it.
func checksOf(p plan.CompilationPlan) (checks, error) {
	var c checks
	for child := range planwalk.Children(planwalk.PlanNode(p)) {
		switch child.Edge.Kind {
		case planwalk.EdgeRepresentation:
			// Storage, not acceptance.
		case planwalk.EdgeValidation:
			c.validation = child.Validation
		case planwalk.EdgeDispatch:
			c.dispatch = child.Dispatch
		default:
			return checks{}, internalf("unhandled plan child edge %s", child.Edge.Kind)
		}
	}
	return c, nil
}

// plan narrows p and interprets it.
func (in *interp) plan(p plan.CompilationPlan, value any, f frame) (Verdict, error) {
	c, err := checksOf(p)
	if err != nil {
		return Verdict{}, err
	}
	return in.accept(c, value, f)
}

// accept decides one instance: the checks must pass, and the dispatch must select a
// branch that accepts it (design §4.1).
func (in *interp) accept(c checks, value any, f frame) (Verdict, error) {
	v, err := in.validation(c.validation, value, f)
	if err != nil || !v.Accepted {
		return v, err
	}
	return in.dispatch(c.dispatch, value, f)
}

func (in *interp) approximate(what string) {
	in.approx = append(in.approx, what)
}
