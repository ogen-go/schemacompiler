// Package frontend is the only package permitted to import the parser (libopenapi).
// It loads a JSON Schema, resolves references, analyzes the reference graph, and
// produces the presence-normalized internal AST that ir compiles from.
//
// Isolating libopenapi here keeps ir/norm/planner hermetic and shields them from the
// parser's v0.x API churn. See docs/implementation.md (Phase 1).
//
// Entry points: [Load] (standalone schema documents, loader.go) and [FromLibOpenAPI]
// (already-parsed schemas, e.g. from an OpenAPI document, loader.go). Both share the
// conversion core in convert.go, then resolve.go resolves every `$ref` and scc.go
// classifies recursive schemas (design §10, §19); the resulting [Registry] is defined in
// registry.go.
package frontend

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
}

// UninhabitedNode records a recursive schema with no finite instance (issue #8).
type UninhabitedNode struct {
	// Pointer is the JSON Pointer to the uninhabited schema.
	Pointer string
	// Reason explains why no instance exists.
	Reason string
}

// UnresolvedRef records a `$ref` that did not resolve to a target.
type UnresolvedRef struct {
	// Pointer is the JSON Pointer to the schema that declared the reference.
	Pointer string
	// Ref is the raw `$ref` string.
	Ref string
	// Reason explains why resolution failed.
	Reason string
}
