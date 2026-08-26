package opt

// presence is the three-state tag. Absent leads so it is the zero value.
type presence uint8

const (
	absent presence = iota
	null
	present
)

// OptNullable is a value that may be absent, null, or present, which is what an optional
// property whose type admits null holds. Its zero value is absent.
type OptNullable[T any] struct {
	_     [0]func()
	val   T
	state presence
}

// Present returns v as a present, non-null value.
func Present[T any](v T) OptNullable[T] { return OptNullable[T]{val: v, state: present} }

// Null returns an explicit null.
func Null[T any]() OptNullable[T] { return OptNullable[T]{state: null} }

// Get returns the value and whether it is present and non-null.
func (o OptNullable[T]) Get() (T, bool) { return o.val, o.state == present }

// IsSet reports whether a non-null value is present.
func (o OptNullable[T]) IsSet() bool { return o.state == present }

// IsNull reports whether the value is present and null.
func (o OptNullable[T]) IsNull() bool { return o.state == null }

// IsAbsent reports whether the value is absent. It is not the negation of [OptNullable.IsSet]:
// a null value is neither set nor absent.
func (o OptNullable[T]) IsAbsent() bool { return o.state == absent }

// IsZero reports whether the value is absent, so `json:",omitzero"` omits it.
func (o OptNullable[T]) IsZero() bool { return o.state == absent }

// Or returns the value, or def when absent or null.
func (o OptNullable[T]) Or(def T) T {
	if o.state == present {
		return o.val
	}
	return def
}

// Set makes the value present and non-null.
func (o *OptNullable[T]) Set(v T) { o.val, o.state = v, present }

// SetNull makes the value null, clearing it so a large value is not retained.
func (o *OptNullable[T]) SetNull() { *o = OptNullable[T]{state: null} }

// Unset makes the value absent, clearing it so a large value is not retained.
func (o *OptNullable[T]) Unset() { *o = OptNullable[T]{} }
