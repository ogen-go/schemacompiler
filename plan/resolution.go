package plan

// SchemaID identifies a schema resource (its resolved absolute URI, design §10).
type SchemaID string

// ResolutionPlan describes how references are resolved (design §10). Static $ref
// resolution happens at compile time; $dynamicRef may need runtime scope.
type ResolutionPlan interface {
	isResolutionPlan()
}

// FullyResolved means no residual reference machinery is needed at runtime.
type FullyResolved struct{}

// StaticReferenceGraph is a set of named definitions resolved at compile time,
// including ordinary recursive references (design §10.1).
type StaticReferenceGraph struct {
	Definitions map[SchemaID]CompilationPlan
}

// DynamicReferenceGraph resolves against the dynamic-anchor scope accumulated
// during validation (design §10.2).
type DynamicReferenceGraph struct {
	StaticDefinitions map[SchemaID]CompilationPlan
	DynamicAnchors    map[string][]SchemaID
}

func (FullyResolved) isResolutionPlan()         {}
func (StaticReferenceGraph) isResolutionPlan()  {}
func (DynamicReferenceGraph) isResolutionPlan() {}
