package gogen

import (
	"fmt"
	"iter"

	"github.com/ogen-go/schemacompiler/plan"
)

// EdgeKind names the relation a child type has to the type that holds it.
type EdgeKind uint8

const (
	// EdgeRoot is the zero edge, carried by a type visited as a walk root rather than
	// reached from a parent.
	EdgeRoot EdgeKind = iota
	// EdgeUnderlying links a [Named] to the type it is declared as.
	EdgeUnderlying
	// EdgeField links a [Struct] to a declared field's type. Name is the Go field name,
	// JSON the property it stores, Index its position in Fields.
	EdgeField
	// EdgePattern links a [Struct] to the map of one `patternProperties` rule. Name is
	// the pattern and Index its position in Patterns.
	EdgePattern
	// EdgeAdditional links a [Struct] to the type of the properties no field and no
	// pattern covers.
	EdgeAdditional
	// EdgeElem links a [Slice] or a [Map] to its element type.
	EdgeElem
	// EdgeTupleElem links a [Tuple] to the slot at Index.
	EdgeTupleElem
	// EdgeTupleRest links a [Tuple] to the type of the items past its prefix.
	EdgeTupleRest
	// EdgeVariant links an [Interface] to the alternative at Index.
	EdgeVariant
	// EdgeStored links a [Presence] to the type it wraps.
	EdgeStored
	// EdgePointee links a [Pointer] to the type it points at.
	EdgePointee
)

var edgeKindNames = [...]string{
	EdgeRoot:       "root",
	EdgeUnderlying: "underlying",
	EdgeField:      "field",
	EdgePattern:    "pattern",
	EdgeAdditional: "additional",
	EdgeElem:       "elem",
	EdgeTupleElem:  "tuple-elem",
	EdgeTupleRest:  "tuple-rest",
	EdgeVariant:    "variant",
	EdgeStored:     "stored",
	EdgePointee:    "pointee",
}

func (k EdgeKind) String() string {
	if int(k) >= len(edgeKindNames) {
		return fmt.Sprintf("edge-kind(%d)", uint8(k))
	}
	return edgeKindNames[k]
}

// Edge is how a type hangs off the type that holds it: which slot it fills, plus what the
// holder knows about the slot that is not part of the child type itself.
type Edge struct {
	Kind EdgeKind
	// Name is the Go field name (EdgeField) or the pattern (EdgePattern).
	Name string
	// JSON is the property name the field stores (EdgeField).
	JSON string
	// Index is the position within the parent's slice: EdgeField, EdgePattern,
	// EdgeTupleElem, EdgeVariant.
	Index int
	// Indirect reports that Go already stores the child behind an indirection, so a
	// reference cycle closed through this edge is not a cycle in the type. It is the
	// whole of the rule the recursion pass needs, stated once (docs/backend.md §7).
	Indirect bool
}

// Node is a type reached by a walk, carrying the edge it hangs off its parent by.
type Node struct {
	Type GoType
	Edge Edge
	// Revisit reports that this [Named] has already been descended into, so the walk
	// delivered the reference but not its insides again. Every cycle in a lowered graph
	// closes through a Named, which is what makes stopping there sufficient.
	Revisit bool
}

// Action tells [Fold] what to do with the subtree beneath a node.
type Action uint8

const (
	// Descend visits the node's children.
	Descend Action = iota
	// Skip leaves the subtree unvisited and continues the walk.
	Skip
	// Stop ends the walk.
	Stop
)

// Children iterates t's direct children, each carrying the [Edge] it hangs off t by.
func Children(t GoType) iter.Seq[Node] {
	return func(yield func(Node) bool) {
		apply(t, func(n Node) GoType {
			if !yield(n) {
				return stopWalk
			}
			return n.Type
		})
	}
}

// Fold threads acc through every type reachable from t, in pre-order.
//
// A lowered graph is cyclic by construction — that is what the recursion pass is for — so
// the walk records the [Named] types it has entered and does not enter one twice. Such a
// node is still delivered, with [Node.Revisit] set, because a consumer counting references
// wants every occurrence even though a consumer collecting types wants each type once.
func Fold[T any](t GoType, acc T, visit func(T, Node) (T, Action)) T {
	entered := make(map[*Named]bool)
	acc, _ = fold(Node{Type: t}, acc, entered, visit)
	return acc
}

func fold[T any](n Node, acc T, entered map[*Named]bool, visit func(T, Node) (T, Action)) (T, bool) {
	if named, ok := n.Type.(*Named); ok && entered[named] {
		n.Revisit = true
	}

	acc, action := visit(acc, n)
	switch action {
	case Stop:
		return acc, false
	case Skip:
		return acc, true
	}
	if n.Revisit {
		return acc, true
	}
	if named, ok := n.Type.(*Named); ok {
		entered[named] = true
	}

	keepGoing := true
	apply(n.Type, func(child Node) GoType {
		if !keepGoing {
			return stopWalk
		}
		acc, keepGoing = fold(child, acc, entered, visit)
		if !keepGoing {
			return stopWalk
		}
		return child.Type
	})
	return acc, keepGoing
}

// stopWalk is the sentinel a child callback returns to end an [apply] early. It is never
// stored: apply checks for it before writing back.
var stopWalk GoType = &Never{}

// apply calls f for every type stored directly in t, writing back what f returns.
//
// It is the one traversal of the [GoType] variants, and everything else in this package is
// built on it: [Children] and [Fold] read through it, and the recursion pass rewrites
// through it. A second switch over these variants is a second place to forget one.
//
// Each case destructures its value into an anonymous struct and drives the walk from THAT
// binding, so a type that gains, renames, retypes or reorders a field stops this package
// compiling until the traversal names the change.
func apply(t GoType, f func(Node) GoType) {
	switch t := t.(type) {
	case *Named:
		v := struct {
			ID         plan.SchemaID
			Name       string
			Underlying GoType
			Recursive  bool
			Checks     Checks
			Metadata   plan.Metadata
		}(*t)
		if v.Underlying == nil {
			return
		}
		if got := f(Node{Type: v.Underlying, Edge: Edge{Kind: EdgeUnderlying}}); got != stopWalk {
			t.Underlying = got
		}

	case *Struct:
		v := struct {
			Fields     []Field
			Patterns   []*Map
			Additional GoType
		}(*t)
		for i := range v.Fields {
			fv := struct {
				Name     string
				JSON     string
				Type     GoType
				Metadata plan.Metadata
			}(v.Fields[i])
			got := f(Node{
				Type: fv.Type,
				Edge: Edge{Kind: EdgeField, Name: fv.Name, JSON: fv.JSON, Index: i},
			})
			if got == stopWalk {
				return
			}
			t.Fields[i].Type = got
		}
		for i, p := range v.Patterns {
			// A pattern slot is a map, so it is indirect and its own element is reached
			// by descending into it rather than by this edge.
			got := f(Node{Type: p, Edge: Edge{Kind: EdgePattern, Name: p.Pattern, Index: i}})
			if got == stopWalk {
				return
			}
			if m, ok := got.(*Map); ok {
				t.Patterns[i] = m
			}
		}
		if v.Additional != nil {
			if got := f(Node{
				Type: v.Additional,
				Edge: Edge{Kind: EdgeAdditional, Indirect: true},
			}); got != stopWalk {
				t.Additional = got
			}
		}

	case *Map:
		v := struct {
			Elem    GoType
			Pattern string
		}(*t)
		if got := f(Node{Type: v.Elem, Edge: Edge{Kind: EdgeElem, Indirect: true}}); got != stopWalk {
			t.Elem = got
		}

	case *Slice:
		v := struct{ Elem GoType }(*t)
		if got := f(Node{Type: v.Elem, Edge: Edge{Kind: EdgeElem, Indirect: true}}); got != stopWalk {
			t.Elem = got
		}

	case *Tuple:
		v := struct {
			Elems []GoType
			Rest  GoType
		}(*t)
		for i, e := range v.Elems {
			got := f(Node{Type: e, Edge: Edge{Kind: EdgeTupleElem, Index: i}})
			if got == stopWalk {
				return
			}
			t.Elems[i] = got
		}
		if v.Rest != nil {
			if got := f(Node{
				Type: v.Rest,
				Edge: Edge{Kind: EdgeTupleRest, Indirect: true},
			}); got != stopWalk {
				t.Rest = got
			}
		}

	case *Interface:
		v := struct{ Variants []GoType }(*t)
		for i, alt := range v.Variants {
			got := f(Node{Type: alt, Edge: Edge{Kind: EdgeVariant, Index: i, Indirect: true}})
			if got == stopWalk {
				return
			}
			t.Variants[i] = got
		}

	case *Presence:
		v := struct {
			Elem     GoType
			Optional bool
			Nullable bool
		}(*t)
		if got := f(Node{Type: v.Elem, Edge: Edge{Kind: EdgeStored}}); got != stopWalk {
			t.Elem = got
		}

	case *Pointer:
		v := struct{ Elem GoType }(*t)
		if got := f(Node{Type: v.Elem, Edge: Edge{Kind: EdgePointee, Indirect: true}}); got != stopWalk {
			t.Elem = got
		}

	case *Primitive:
		_ = struct {
			Kind   PrimitiveKind
			Format string
		}(*t)

	case *Any:
		_ = struct{}(*t)

	case *Never:
		_ = struct{}(*t)

	default:
		// Go has no sealed-interface exhaustiveness check, so a new variant reaches this
		// rather than being silently skipped. AllGoTypes drives every one through here.
		panic(fmt.Sprintf("gogen: unhandled GoType variant %T", t))
	}
}

// AllGoTypes is one of every [GoType] variant, each with its slots filled, for a test that
// drives every one through a traversal the compiler cannot check exhaustively.
func AllGoTypes() []GoType {
	return []GoType{
		&Named{ID: "/$defs/T", Name: "T", Underlying: &Any{}},
		&Primitive{Kind: PrimitiveString},
		&Any{},
		&Never{},
		&Slice{Elem: &Any{}},
		&Tuple{Elems: []GoType{&Any{}}, Rest: &Any{}},
		&Map{Elem: &Any{}, Pattern: "^m"},
		&Struct{
			Fields:     []Field{{Name: "F", JSON: "f", Type: &Any{}}},
			Patterns:   []*Map{{Elem: &Any{}, Pattern: "^p"}},
			Additional: &Any{},
		},
		&Presence{Elem: &Any{}, Optional: true, Nullable: true},
		&Interface{Variants: []GoType{&Any{}}},
		&Pointer{Elem: &Any{}},
	}
}
