package nodetree

import (
	"iter"
	"strconv"
	"strings"

	"github.com/ogen-go/schemacompiler/plan"
)

// Error is one reason an instance was rejected.
type Error struct {
	// Location is the JSON Pointer of the offending instance value.
	Location string
	// Keyword names the check that failed, as the schema spells it.
	Keyword string
	Message string
}

func (e Error) Error() string {
	// Location is a JSON Pointer, and RFC 6901 spells the whole document "" — which reads
	// as nothing at all in a message, so only the rendering substitutes a word for it.
	at := e.Location
	if at == "" {
		at = "(root)"
	}
	if e.Message == "" {
		return at + ": " + e.Keyword
	}
	return at + ": " + e.Keyword + ": " + e.Message
}

// loc is the instance path under construction, as a parent-linked chain of stack frames.
// Nothing is formatted while descending: a validation that reports no error never builds
// a string, which is the whole point of keeping it separate from [Error.Location].
type loc struct {
	parent  *loc
	name    string
	index   int
	indexed bool
}

func (l *loc) child(name string) loc { return loc{parent: l, name: name} }
func (l *loc) elem(i int) loc        { return loc{parent: l, index: i, indexed: true} }

// childLocation formats one level below l without keeping the child alive past the call.
func childLocation(l *loc, name string) string {
	c := l.child(name)
	return c.String()
}

func kindName(k plan.JSONKind) string {
	switch k {
	case plan.KindNull:
		return "null"
	case plan.KindBoolean:
		return "boolean"
	case plan.KindNumber:
		return "number"
	case plan.KindString:
		return "string"
	case plan.KindArray:
		return "array"
	case plan.KindObject:
		return "object"
	default:
		return "kind(" + strconv.Itoa(int(k)) + ")"
	}
}

func kindSetName(s plan.KindSet) string {
	var names []string
	for k := plan.KindNull; k <= plan.KindObject; k++ {
		if s.Has(k) {
			names = append(names, kindName(k))
		}
	}
	if len(names) == 0 {
		return "nothing"
	}
	return strings.Join(names, "|")
}

// String materializes the chain as a JSON Pointer, and is called only when an [Error] is
// actually yielded.
func (l *loc) String() string {
	if l == nil {
		return ""
	}
	var b strings.Builder
	l.writeTo(&b)
	return b.String()
}

func (l *loc) writeTo(b *strings.Builder) {
	if l == nil {
		return
	}
	l.parent.writeTo(b)
	b.WriteByte('/')
	if l.indexed {
		b.WriteString(strconv.Itoa(l.index))
		return
	}
	b.WriteString(escapePointer(l.name))
}

// escapePointer applies RFC 6901's two escapes.
func escapePointer(s string) string {
	if !strings.ContainsAny(s, "~/") {
		return s
	}
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(s)
}

// reporter is the error-reporting half of a node. It is a second interface rather than a
// second method on [node] so that adding reporting cannot slow the accepting path: a node
// that has nothing to add falls back to [report].
//
// yield follows the [iter.Seq] protocol — returning false means the consumer has stopped
// pulling, and every node must propagate that immediately.
type reporter interface {
	errs(raw []byte, at *loc, yield func(Error) bool) bool
}

// report yields n's errors for raw, or a bare rejection if n does not describe its own.
func report(n node, raw []byte, at *loc, yield func(Error) bool) bool {
	if r, ok := n.(reporter); ok {
		return r.errs(raw, at, yield)
	}
	if n.ok(raw) {
		return true
	}
	return yield(Error{Location: at.String(), Keyword: "schema", Message: "instance is not valid"})
}

// leaf yields one error when the check failed, and is what every keyword-shaped node uses.
func leaf(n node, raw []byte, at *loc, keyword, message string, yield func(Error) bool) bool {
	if n.ok(raw) {
		return true
	}
	return yield(Error{Location: at.String(), Keyword: keyword, Message: message})
}

// IterErrors yields every reason data fails, in document order, stopping early if the
// consumer stops pulling.
//
// It is a separate traversal from [Validator.IsValid], which builds no path and allocates
// no error. Use IsValid when only the answer matters.
func (v *Validator) IterErrors(data []byte) iter.Seq[Error] {
	return func(yield func(Error) bool) {
		// One constraint can reach two nodes: design §4.1 has the object structure
		// predicate restate what the representation stores, so a `required` property is
		// checked both by [requiredNode] and by [objectStructure]'s presence mask. The
		// fast path short-circuits and never notices; a reporting walk would say it
		// twice. Distinct reasons, reported once each.
		var seen []Error
		report(v.root, data, nil, func(e Error) bool {
			// A slice rather than a set: a rejected instance has a handful of reasons,
			// and a linear scan over those beats hashing every one of them.
			for _, s := range seen {
				if s == e {
					return true
				}
			}
			seen = append(seen, e)
			return yield(e)
		})
	}
}

// Validate returns the first reason data fails, or nil. It does not go through
// [Validator.IterErrors]: stopping at the first error makes deduplication pointless, and
// paying for it would be the common case paying for the rare one.
func (v *Validator) Validate(data []byte) error {
	var first Error
	found := false
	report(v.root, data, nil, func(e Error) bool {
		first, found = e, true
		return false
	})
	if !found {
		return nil
	}
	return first
}
