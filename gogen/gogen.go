// Package gogen lowers an analyzed [plan.CompilationPlan] into Go types.
//
// It is the backend docs/backend.md designs: schemacompiler stops at the plan, and this
// package is what turns one into a Go type, a codec and a validator. Lowering is two
// stages, both public — [Names] and the rest of `Lower` decide Go semantics, and rendering
// source is a separate, replaceable layer — so a caller that wants its own file layout
// (ogen) can take the semantics and render them itself.
//
// This package depends on `plan` alone. It does not import the parser, the frontend or the
// analysis packages, and it never re-derives a keyword's meaning from knowledge of what the
// keyword was: everything it needs is in the plan.
package gogen
