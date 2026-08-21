package planterp

import (
	"fmt"
	"strings"

	"github.com/go-faster/errors"
)

// ValidateError is the plan rejecting an instance. It is a normal outcome, carried by
// [Verdict.Reason]; [Interpret]'s error slot never holds one.
type ValidateError struct {
	// Path is a JSON Pointer into the instance, empty at the instance root.
	Path string
	// Constraint names the plan construct that rejected: "minLength", "union",
	// "predicate-count dispatch".
	Constraint string
	// Detail is what the constraint has to say about this instance, if anything.
	Detail string
	// Cause is the branch, element or property rejection this one stands on.
	Cause *ValidateError
}

// Error renders the chain innermost first, so the most located rejection leads and the
// constraints it failed under follow.
func (e *ValidateError) Error() string {
	if e == nil {
		return "<nil>"
	}

	var chain []*ValidateError
	for c := e; c != nil; c = c.Cause {
		chain = append(chain, c)
	}

	var b strings.Builder
	for i := len(chain) - 1; i >= 0; i-- {
		if i != len(chain)-1 {
			b.WriteString("\n  via: ")
		}
		chain[i].writeLine(&b)
	}
	return b.String()
}

func (e *ValidateError) writeLine(b *strings.Builder) {
	if e.Path != "" {
		b.WriteString(e.Path)
		b.WriteString(": ")
	}
	b.WriteString(e.Constraint)
	if e.Detail != "" {
		b.WriteString(": ")
		b.WriteString(e.Detail)
	}
}

// Leaf is the innermost rejection of the chain: the most located thing that went wrong,
// which is what a reader wants first and what [ValidateError.Error] leads with.
func (e *ValidateError) Leaf() *ValidateError {
	for e != nil && e.Cause != nil {
		e = e.Cause
	}
	return e
}

func (e *ValidateError) Unwrap() error {
	if e == nil || e.Cause == nil {
		return nil
	}
	return e.Cause
}

// InvalidValueError is a value that [encoding/json] does not produce reaching the
// interpreter: the caller decoded into something else, or handed it a Go value directly.
type InvalidValueError struct {
	// Path is a JSON Pointer into the instance, empty at the instance root.
	Path string
	// Value is the offending value.
	Value any
	// Detail says what is wrong with it.
	Detail string
}

func (e *InvalidValueError) Error() string {
	if e.Path == "" {
		return "planterp: " + e.Detail
	}
	return "planterp: " + e.Path + ": " + e.Detail
}

func invalidValuef(value any, format string, args ...any) error {
	return &InvalidValueError{Value: value, Detail: fmt.Sprintf(format, args...)}
}

// withPath fills in the instance location of an [InvalidValueError] raised by a helper
// that does not know one. The helpers are leaves, so the first frame-aware caller on the
// way out is the one that knows.
func withPath(path string, err error) error {
	var invalid *InvalidValueError
	if path != "" && errors.As(err, &invalid) && invalid.Path == "" {
		invalid.Path = path
	}
	return err
}

// InternalError is the interpreter failing to read the plan: a variant it has no case
// for, a reference it cannot resolve, plan data it cannot make sense of. It is never a
// statement about the instance.
type InternalError struct {
	Detail string
}

func (e *InternalError) Error() string { return "planterp: " + e.Detail }

func internalf(format string, args ...any) error {
	return &InternalError{Detail: fmt.Sprintf(format, args...)}
}
