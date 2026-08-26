package opt_test

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/opt"
)

func TestOptStates(t *testing.T) {
	var o opt.Opt[string]
	require.False(t, o.IsSet())
	require.True(t, o.IsZero(), "the zero value is absent, so a field a document omits needs no initialization")
	require.Equal(t, "fallback", o.Or("fallback"))

	o.Set("v")
	v, ok := o.Get()
	require.True(t, ok)
	require.Equal(t, "v", v)
	require.False(t, o.IsZero())

	o.Unset()
	require.False(t, o.IsSet())
	got, _ := o.Get()
	require.Zero(t, got, "unsetting must clear the value, not just the flag")
}

func TestNullableStates(t *testing.T) {
	var n opt.Nullable[string]
	require.True(t, n.IsNull(), "Nullable has no absent state, so its zero value is null")

	n.Set("v")
	v, ok := n.Get()
	require.True(t, ok)
	require.Equal(t, "v", v)

	n.SetNull()
	require.True(t, n.IsNull())
	got, ok := n.Get()
	require.False(t, ok)
	require.Zero(t, got)
	require.Equal(t, "fallback", n.Or("fallback"))
}

// TestOptNullableKeepsThreeStates is the distinction the whole package exists for:
// `required` and `type: null` are different constraints, so absent and null cannot be the
// same state.
func TestOptNullableKeepsThreeStates(t *testing.T) {
	var o opt.OptNullable[string]
	require.True(t, o.IsAbsent())
	require.False(t, o.IsNull())
	require.False(t, o.IsSet())

	o.SetNull()
	require.False(t, o.IsAbsent(), "null is present; it is not absent")
	require.True(t, o.IsNull())
	require.False(t, o.IsSet())
	require.False(t, o.IsZero(), "an explicit null must be encoded, not omitted")

	o.Set("v")
	require.True(t, o.IsSet())
	require.False(t, o.IsNull())
	require.False(t, o.IsAbsent())

	o.Unset()
	require.True(t, o.IsAbsent())
	got, _ := o.Get()
	require.Zero(t, got)
}

func TestConstructors(t *testing.T) {
	require.True(t, opt.Some(1).IsSet())
	require.False(t, opt.NonNull(1).IsNull())
	require.True(t, opt.Present(1).IsSet())
	require.True(t, opt.Null[int]().IsNull())
	require.False(t, opt.Null[int]().IsAbsent())
}

// TestUncomparable pins the guard. `==` on a pointer-instantiated value would compare
// addresses rather than values — silently wrong rather than merely unavailable — and the
// guard also freezes comparability, so switching a field between Opt[T] and Opt[*T] cannot
// break caller code that never compiled.
func TestUncomparable(t *testing.T) {
	require.False(t, reflect.TypeOf(opt.Opt[int]{}).Comparable())
	require.False(t, reflect.TypeOf(opt.Nullable[int]{}).Comparable())
	require.False(t, reflect.TypeOf(opt.OptNullable[int]{}).Comparable())

	// DeepEqual still works, so testify assertions on generated structs are unaffected.
	require.True(t, reflect.DeepEqual(opt.Some(1), opt.Some(1)))
	require.False(t, reflect.DeepEqual(opt.Some(1), opt.Some(2)))
	require.Equal(t, opt.Some("v"), opt.Some("v"))
}

// TestNoTrailingPadding pins the guard's position. Go pads a struct whose last field is
// zero-sized, so a trailing guard would cost a word per optional field across every
// generated type.
func TestNoTrailingPadding(t *testing.T) {
	type guardLast struct {
		val int64
		set bool
		_   [0]func()
	}
	require.Greater(t, unsafe.Sizeof(guardLast{}), unsafe.Sizeof(opt.Opt[int64]{}),
		"the guard must not be the last field")
	require.Equal(t, unsafe.Sizeof(struct {
		val int64
		set bool
	}{}), unsafe.Sizeof(opt.Opt[int64]{}), "the guard must cost nothing")
}

// TestSelfReference is why the value is stored inline rather than behind a pointer: a
// generator instantiates with *T where a type reaches itself, and that is the only
// adaptation needed — there is no parallel pointer-flavored type.
type Node struct {
	Name     opt.Opt[string]
	Child    opt.Opt[*Node]
	Siblings opt.Opt[[]Node]
}

func TestSelfReference(t *testing.T) {
	n := Node{}
	n.Child.Set(&Node{Name: opt.Some("leaf")})
	child, ok := n.Child.Get()
	require.True(t, ok)
	name, _ := child.Name.Get()
	require.Equal(t, "leaf", name)

	n.Siblings.Set([]Node{{Name: opt.Some("sib")}})
	sibs, ok := n.Siblings.Get()
	require.True(t, ok)
	require.Len(t, sibs, 1)
}

func BenchmarkOptSetGet(b *testing.B) {
	var sink int
	for b.Loop() {
		var o opt.Opt[int]
		o.Set(7)
		v, _ := o.Get()
		sink += v
	}
	_ = sink
}
