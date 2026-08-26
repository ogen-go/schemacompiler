// Package opt carries the three-state presence and nullability model JSON Schema needs
// (design §7.1, §12.2), for generated code to store fields in.
//
// A JSON object property has three states a Go field must keep apart: absent, present and
// null, and present with a value. `required` and `type: null` are different constraints, so
// collapsing absent into null loses a distinction the schema draws. [Opt] and [Nullable]
// carry two of the states each, [OptNullable] all three.
//
// # Zero values
//
// The zero value is the state a decoder leaves a field in when it reads nothing: absent for
// [Opt] and [OptNullable], null for [Nullable], which has no absent state. So a field the
// document does not mention needs no explicit initialization.
//
// # Storage, and why these are not comparable
//
// The value is stored inline rather than behind a pointer, so an optional field costs no
// allocation. That cannot work for a type that reaches itself through a chain of plain
// struct fields — Go rejects `type Node struct { Child Opt[Node] }` as an invalid recursive
// type — so a generator emits `Opt[*Node]` for those. Instantiating with a pointer is the
// whole adaptation: there is no separate pointer-flavored type to keep in step.
//
// Every type here is deliberately uncomparable, via a `[0]func()` field. `==` on a
// pointer-instantiated one would compare addresses rather than values, which is silently
// wrong rather than merely unavailable; and a generated struct holding a slice or map is
// uncomparable regardless, so little is given up. Use [reflect.DeepEqual] or an explicit
// comparison. The guard is also what keeps comparability from changing later: switching a
// field between `Opt[T]` and `Opt[*T]` cannot break caller code that never compiled.
package opt
