package opt

import (
	"encoding/json"
	"errors"
)

// The presence types marshal as the value they hold, so an [Opt] of a type is encoded the
// way that type is. Absence has no JSON spelling of its own: a value that is absent is one
// its containing object does not write a key for, which is the container's decision and
// not this type's. Reached anywhere else — inside an array, say — absence encodes as
// `null`, since something has to occupy the slot.
//
// [encoding/json] is the only import this package has, and it is here rather than absent
// so that a generated type containing one of these is serializable without the generator
// restating what these already know. A backend that wants a different codec supplies its
// own presence types instead (`gogen.Options.OptPackage`).

var (
	jsonNull           = []byte("null")
	errNullNotAdmitted = errors.New("opt: null is not an admitted value here")
)

// MarshalJSON implements [json.Marshaler].
func (o Opt[T]) MarshalJSON() ([]byte, error) {
	if !o.set {
		return jsonNull, nil
	}
	return json.Marshal(o.val)
}

// UnmarshalJSON implements [json.Unmarshaler]. A JSON null is an error: this type is what a
// schema admitting no null lowers to, so a null here is a value the schema rejects. Taking
// it as absence would accept it and lose the difference on the way back out.
func (o *Opt[T]) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return errNullNotAdmitted
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	o.Set(v)
	return nil
}

// MarshalJSON implements [json.Marshaler].
func (n Nullable[T]) MarshalJSON() ([]byte, error) {
	if !n.set {
		return jsonNull, nil
	}
	return json.Marshal(n.val)
}

// UnmarshalJSON implements [json.Unmarshaler].
func (n *Nullable[T]) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		n.SetNull()
		return nil
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	n.Set(v)
	return nil
}

// MarshalJSON implements [json.Marshaler]. Absent and null both encode as `null`; the
// difference between them is whether a key is written at all, which the containing object
// decides.
func (o OptNullable[T]) MarshalJSON() ([]byte, error) {
	if o.state != present {
		return jsonNull, nil
	}
	return json.Marshal(o.val)
}

// UnmarshalJSON implements [json.Unmarshaler]. A JSON null is null rather than absent: the
// key was written, and absence is the containing object observing that it was not.
func (o *OptNullable[T]) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		o.SetNull()
		return nil
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	o.Set(v)
	return nil
}
