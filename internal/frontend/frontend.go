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
}
