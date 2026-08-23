// Package nodetree compiles a [plan.CompilationPlan] into a tree of executable nodes and
// validates raw JSON against it, without decoding the instance into `any`.
//
// It is the counterpart of internal/planterp, and deliberately not a replacement: planterp
// is the reference oracle and stays simple enough to be read as a specification, while
// this trades that legibility for speed. The differential test between the two is what
// keeps the trade honest.
//
// The design follows §4.1: acceptance is the validation plan and the dispatch, so the tree
// is built from Validation and Dispatch alone and never reads Representation. planterp
// already proved that is sufficient across the JSON-Schema-Test-Suite.
package nodetree

import (
	"github.com/go-faster/errors"
	"github.com/go-faster/jx"

	"github.com/ogen-go/schemacompiler/plan"
)

// node is one check in the compiled tree. raw is a complete JSON value, and for a
// byte-backed decoder it is a sub-slice of the caller's input rather than a copy.
//
// The interface has one method on purpose: a validator that also produced an error path
// would pay for it on the accepting path, which is the path that runs.
type node interface {
	ok(raw []byte) bool
}

// Validator is a compiled plan.
type Validator struct {
	root node
	defs map[string]node
}

// Compile builds the tree. It fails rather than skipping a construct it cannot lower:
// a validator that silently ignores a check accepts what the schema rejects, which is
// the one direction design §24 never permits.
func Compile(p plan.CompilationPlan) (*Validator, error) {
	v := &Validator{defs: map[string]node{}}
	if err := v.collectDefs(p); err != nil {
		return nil, err
	}
	root, err := v.compilePlan(p)
	if err != nil {
		return nil, err
	}
	v.root = root
	return v, nil
}

// IsValid reports whether data satisfies the plan.
func (v *Validator) IsValid(data []byte) bool {
	return v.root.ok(data)
}

func (v *Validator) collectDefs(p plan.CompilationPlan) error {
	graph, ok := p.Resolution.(plan.StaticReferenceGraph)
	if !ok {
		return nil
	}
	// Two passes: a definition may reference another (or itself), so every name must be
	// resolvable before any body is compiled. The indirection node reads v.defs at run
	// time, which is what ties the knot.
	for id := range graph.Definitions {
		v.defs[string(id)] = nil
	}
	for id, def := range graph.Definitions {
		n, err := v.compilePlan(def)
		if err != nil {
			return errors.Wrapf(err, "definition %q", id)
		}
		v.defs[string(id)] = n
	}
	return nil
}

// compilePlan lowers one plan node: its validation, then its dispatch. Representation is
// not read (design §4.1).
func (v *Validator) compilePlan(p plan.CompilationPlan) (node, error) {
	preds := p.Validation.Predicates
	fusion := newPlanFusion(preds)

	var out all
	for i, gp := range preds {
		if fusion.skip[i] {
			continue
		}
		n, err := v.compileGuarded(gp, fusion)
		if err != nil {
			return nil, err
		}
		if n != nil {
			out = append(out, n)
		}
	}
	d, err := v.compileDispatch(p.Dispatch)
	if err != nil {
		return nil, err
	}
	if d != nil {
		out = append(out, d)
	}
	switch len(out) {
	case 0:
		return always{}, nil
	case 1:
		return out[0], nil
	default:
		return out, nil
	}
}

func (v *Validator) compileGuarded(gp plan.GuardedPredicate, fusion planFusion) (node, error) {
	inner, err := v.compileExpr(gp.Expression, fusion)
	if err != nil {
		return nil, err
	}
	if gp.Assert {
		return assertKind{set: gp.Applicability, inner: inner}, nil
	}
	if inner == nil {
		return nil, nil
	}
	if gp.Applicability == plan.SetAny {
		return inner, nil
	}
	return guard{set: gp.Applicability, inner: inner}, nil
}

// kindOf reads the JSON kind from the first significant byte, which is all a well-formed
// value needs. jx is not involved: this runs once per node per instance.
func kindOf(raw []byte) (plan.JSONKind, bool) {
	for _, c := range raw {
		switch c {
		case ' ', '\t', '\n', '\r':
			continue
		case '{':
			return plan.KindObject, true
		case '[':
			return plan.KindArray, true
		case '"':
			return plan.KindString, true
		case 't', 'f':
			return plan.KindBoolean, true
		case 'n':
			return plan.KindNull, true
		default:
			if c == '-' || (c >= '0' && c <= '9') {
				return plan.KindNumber, true
			}
			return 0, false
		}
	}
	return 0, false
}

func decoder(raw []byte) *jx.Decoder {
	d := jx.GetDecoder()
	d.ResetBytes(raw)
	return d
}
