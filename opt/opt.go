package opt

// Opt is a value that may be absent. Its zero value is absent.
type Opt[T any] struct {
	// The guard leads so it adds no trailing padding: Go gives a struct whose last field
	// is zero-sized an extra byte, to keep a pointer past the end from aliasing the next
	// allocation.
	_   [0]func()
	val T
	set bool
}

// Some returns v as a present value.
func Some[T any](v T) Opt[T] { return Opt[T]{val: v, set: true} }

// Get returns the value and whether it is present.
func (o Opt[T]) Get() (T, bool) { return o.val, o.set }

// IsSet reports whether a value is present.
func (o Opt[T]) IsSet() bool { return o.set }

// IsZero reports whether the value is absent, so `json:",omitzero"` omits it.
func (o Opt[T]) IsZero() bool { return !o.set }

// Or returns the value, or def when absent.
func (o Opt[T]) Or(def T) T {
	if o.set {
		return o.val
	}
	return def
}

// Set makes the value present.
func (o *Opt[T]) Set(v T) { o.val, o.set = v, true }

// Unset makes the value absent, clearing it so a large value is not retained.
func (o *Opt[T]) Unset() { *o = Opt[T]{} }
