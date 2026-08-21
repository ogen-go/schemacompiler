// Package frontend is the only package permitted to import the parser (libopenapi).
// It loads a JSON Schema, resolves references, analyzes the reference graph, and
// produces the presence-normalized internal AST that ir compiles from.
//
// Isolating libopenapi here keeps ir/norm/planner hermetic and shields them from the
// parser's v0.x API churn. See docs/implementation.md (Phase 1).
//
// Entry points: [Load] (standalone schema documents, loader.go), [FromLibOpenAPI] /
// [FromLibOpenAPIProxy] (an already-parsed schema, loader.go and document.go) and
// [FromLibOpenAPIDocument] (a whole OpenAPI component set sharing one registry,
// document.go). They share the conversion core in convert.go, then resolve.go
// resolves every `$ref` and scc.go
// classifies recursive schemas (design §10, §19); the resulting [Registry] is defined in
// registry.go.
package frontend

import "github.com/ogen-go/schemacompiler/plan"

// Schema is a loaded, resolved schema document: the root [Node] plus the [Registry] of
// every resource reachable from it. The Node AST is defined in ast.go.
type Schema struct {
	// Registry holds every resolved schema resource reachable from the root.
	Registry *Registry
	// Root is the entry schema.
	Root *Node
	// Unresolved lists every `$ref` whose target could not be found. Loading does not
	// fail on a dangling reference: the reference is left unresolved (its [Node.Resolved]
	// stays nil) and reported here so callers can surface a diagnostic and still analyze
	// the rest of the document.
	Unresolved []UnresolvedRef
	// Uninhabited lists recursive schemas proven to have no finite JSON instance (required
	// self-recursion). The SCC pass classifies these representable/guarded, but no value
	// inhabits them; reported here so a caller can warn rather than emit a dead type.
	Uninhabited []UninhabitedNode
	// IgnoredNullable lists every `nullable: true` OAS 3.0.3 leaves inert for want of a
	// sibling `type`.
	IgnoredNullable []IgnoredNullable
	// UnusedDiscriminator lists every `discriminator` that names no union to dispatch on.
	UnusedDiscriminator []UnusedDiscriminator
}

// IgnoredNullable records an OpenAPI 3.0 `nullable: true` that had no effect: OAS 3.0.3
// line 2335 widens the `type` keyword and only that keyword, so without one declared in
// the same Schema Object there is nothing for null to be added to (issue #20).
type IgnoredNullable struct {
	// Pointer is the JSON Pointer to the schema that declared `nullable`.
	Pointer string
	// Position is the source location of that schema.
	Position plan.Position
}

// UnusedDiscriminator records an OpenAPI `discriminator` declared where the compiler has
// no union to dispatch on (issue #46). OAS 3.0.3 line 2705 makes the keyword legal only
// alongside `oneOf`, `anyOf` or `allOf`; only the first two name the alternatives, so an
// `allOf` declaration (the parent-schema idiom of line 2761, whose alternatives are the
// schemas elsewhere in the document that include the parent) and a declaration with no
// composite keyword at all are both recorded here rather than dropped silently.
type UnusedDiscriminator struct {
	// Pointer is the JSON Pointer to the schema that declared `discriminator`.
	Pointer string
	// Position is the source location of that schema.
	Position plan.Position
	// PropertyName is the declared `propertyName`.
	PropertyName string
}

// UninhabitedNode records a recursive schema with no finite instance (issue #8).
type UninhabitedNode struct {
	// Pointer is the JSON Pointer to the uninhabited schema.
	Pointer string
	// Position is the source location of that schema.
	Position plan.Position
	// Reason explains why no instance exists.
	Reason string
}

// UnresolvedRef records a `$ref` that did not resolve to a target.
type UnresolvedRef struct {
	// Pointer is the JSON Pointer to the schema that declared the reference.
	Pointer string
	// Position is the source location of the schema that declared the reference.
	Position plan.Position
	// Ref is the raw `$ref` string.
	Ref string
	// Reason explains why resolution failed.
	Reason string
}
