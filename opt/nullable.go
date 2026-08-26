package opt

// Nullable is a value that is always present and may be null, which is what a required
// property whose type admits null holds. Its zero value is null, since that is the only
// state a value can be in before one is assigned.
type Nullable[T any] struct {
	_   [0]func()
	val T
	// set is "present and non-null", so the zero value is null. A required nullable
	// field a decoder never wrote reads as null rather than as a fabricated "" or 0,
	// which would be a real value the schema may well reject.
	set bool
}

// NonNull returns v as a non-null value.
func NonNull[T any](v T) Nullable[T] { return Nullable[T]{val: v, set: true} }

// Get returns the value and whether it is non-null.
func (n Nullable[T]) Get() (T, bool) { return n.val, n.set }

// IsNull reports whether the value is null.
func (n Nullable[T]) IsNull() bool { return !n.set }

// Or returns the value, or def when null.
func (n Nullable[T]) Or(def T) T {
	if !n.set {
		return def
	}
	return n.val
}

// Set makes the value non-null.
func (n *Nullable[T]) Set(v T) { n.val, n.set = v, true }

// SetNull makes the value null, clearing it so a large value is not retained.
func (n *Nullable[T]) SetNull() { *n = Nullable[T]{} }
