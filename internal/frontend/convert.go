package frontend

import (
	"context"
	"strconv"

	"github.com/go-faster/errors"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	lowbase "github.com/pb33f/libopenapi/datamodel/low/base"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// frame is one active schema resource in scope while converting: its base URI and the
// document pointer of its resource root (used to compute resource-relative pointers for
// [Registry] lookups). Every ancestor $id resource stays in scope: JSON Pointer fragments
// resolve structurally regardless of nested $id boundaries (design §10.1).
type frame struct {
	baseURI string
	root    string // docPointer of this frame's resource root
}

// scope is the resolution context threaded through schema conversion.
type scope struct {
	frames     []frame
	docPointer string
	// source is the retrieval URI as the caller supplied it, reported in diagnostic
	// positions; frames[0].baseURI holds its normalized form, which keys resolution.
	source string
}

func (sc scope) baseURI() string {
	return sc.frames[len(sc.frames)-1].baseURI
}

func (sc scope) child(pointerSegment string) scope {
	return scope{frames: sc.frames, docPointer: jsonPointerAppend(sc.docPointer, pointerSegment), source: sc.source}
}

func (sc scope) childIndex(i int) scope {
	return sc.child(strconv.Itoa(i))
}

// convState carries the state accumulated while converting a libopenapi high-level
// schema tree into the internal [Node] AST.
type convState struct {
	reg *Registry
	// refMap holds `$ref` strings stripped from yaml nodes before they were handed to
	// libopenapi (see loader.go); nil when converting an already-parsed base.Schema
	// (the FromLibOpenAPI entry point), in which case Ref is instead recovered from the
	// SchemaProxy chain.
	refMap map[*yaml.Node]string
	// refBaseURI records, for every node carrying a Ref, the base URI in effect where
	// that $ref was declared (needed to resolve it in the later resolveAll pass).
	refBaseURI map[*Node]string
	// unresolved accumulates references that could not be resolved (see resolveAll).
	unresolved []UnresolvedRef
	// loader fetches external documents on demand during resolution (nil disables it).
	loader Loader
	// loaded records every external base URI that has been attempted (loaded once,
	// success or failure), bounding the resolution worklist and breaking document cycles.
	loaded map[string]bool
	// loadErrs records why a given external base URI failed to load, folded into the
	// unresolved-ref diagnostic for refs that targeted it.
	loadErrs map[string]error
}

func newConvState(refMap map[*yaml.Node]string, loader Loader) *convState {
	return &convState{
		reg:        newRegistry(),
		refMap:     refMap,
		refBaseURI: make(map[*Node]string),
		loader:     loader,
		loaded:     make(map[string]bool),
		loadErrs:   make(map[string]error),
	}
}

// analyze resolves every recorded reference, then classifies recursion and inhabitation
// over the whole converted set.
func (st *convState) analyze(ctx context.Context) {
	st.resolveAll(ctx)
	st.reg.analyzeSCCs()
	st.reg.analyzeInhabitation()
}

// convertRoot converts hs into the internal AST, then resolves references and analyzes
// the reference graph.
func convertRoot(ctx context.Context, hs *base.Schema, refMap map[*yaml.Node]string, baseURI string, loader Loader) (*Schema, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	normalized, err := normalizeBaseURI(baseURI)
	if err != nil {
		return nil, err
	}

	st := newConvState(refMap, loader)
	sc := scope{frames: []frame{{baseURI: normalized, root: ""}}, source: baseURI}

	root, err := st.convertSchema(ctx, hs, sc)
	if err != nil {
		return nil, errors.Wrap(err, "convert root schema")
	}
	if _, ok := st.reg.resources[normalized]; !ok {
		st.reg.resources[normalized] = root
	}

	st.analyze(ctx)

	return &Schema{
		Registry:    st.reg,
		Root:        root,
		Unresolved:  st.unresolved,
		Uninhabited: st.reg.uninhabited,
	}, nil
}

// convertProxy converts a *base.SchemaProxy (a lazily-built child schema position) into a
// Node, short-circuiting boolean schemas before they ever reach libopenapi's Schema
// builder (which only understands mapping nodes), and reference proxies before they are
// followed (see [convState.referenceNode]).
func (st *convState) convertProxy(ctx context.Context, sp *base.SchemaProxy, sc scope) (*Node, error) {
	if sp == nil {
		return nil, nil
	}
	if sp.IsReference() {
		return st.referenceNode(ctx, sp, sc)
	}
	if vn := sp.GetValueNode(); vn != nil {
		if b, ok := boolSchemaValue(vn); ok {
			n := &Node{Always: &b, Pointer: sc.docPointer, Position: nodePosition(sc.file(), vn)}
			st.register(n, sc, sc.baseURI())
			return n, nil
		}
	}
	hs := sp.Schema()
	if hs == nil {
		err := sp.GetBuildError()
		if err == nil {
			err = errors.Errorf("schema build failed at %q with no diagnostic", sc.docPointer)
		}
		return nil, errors.Wrapf(err, "build schema at %q", sc.docPointer)
	}
	return st.convertSchema(ctx, hs, sc)
}

// referenceNode converts a `$ref` position into a Node carrying the reference plus any
// keywords declared alongside it, left for the later resolveAll pass to resolve against
// the [Registry].
//
// libopenapi's SchemaProxy.Schema() returns the *target* of a reference, not the reference
// itself, so descending into it would inline the target — losing the reference's identity
// as a named definition, and never terminating on a recursive schema. The standalone
// loader path never reaches here because stripRefs (loader.go) removes every `$ref` before
// libopenapi sees it; this is the equivalent for an already-parsed schema.
func (st *convState) referenceNode(ctx context.Context, sp *base.SchemaProxy, sc scope) (*Node, error) {
	ref := sp.GetReference()

	siblings, err := st.siblingSchema(ctx, sp.GetReferenceNode())
	if err != nil {
		return nil, errors.Wrapf(err, "build keywords alongside $ref at %q", sc.docPointer)
	}
	if siblings == nil {
		n := &Node{Ref: ref, Pointer: sc.docPointer, Position: nodePosition(sc.file(), sp.GetReferenceNode())}
		st.register(n, sc, sc.baseURI())
		st.refBaseURI[n] = sc.baseURI()
		return n, nil
	}

	n, err := st.convertSchema(ctx, siblings, sc)
	if err != nil {
		return nil, err
	}
	n.Ref = ref
	baseURI, err := nodeBaseURI(sc, n.ID)
	if err != nil {
		return nil, errors.Wrapf(err, "resolve $id %q at %q", n.ID, sc.docPointer)
	}
	st.refBaseURI[n] = baseURI
	return n, nil
}

// siblingSchema builds a schema from the keywords declared alongside a `$ref`, or returns
// nil when the reference stands alone. 2020-12 lets `$ref` coexist with sibling keywords,
// but a reference proxy's value node is the *target's*, so the siblings are recovered from
// the node that declared the reference (SchemaProxy.GetReferenceNode) and rebuilt without
// its `$ref` key.
//
// Nested `$ref`s inside those keywords are stripped into st.refMap the same way the
// standalone loader does, since this build has no index to resolve them against and
// low/base.Schema.Build would otherwise try to follow them (see stripRefs in loader.go).
func (st *convState) siblingSchema(ctx context.Context, refNode *yaml.Node) (*base.Schema, error) {
	n := resolveAlias(refNode)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil, nil
	}
	// The document's nodes are shared with the caller, so strip on a copy.
	stripped := deepCopyNode(n)
	if st.refMap == nil {
		st.refMap = make(map[*yaml.Node]string)
	}
	stripRefs(stripped, st.refMap)
	// stripRefs removed this node's own `$ref`; anything left is a sibling keyword.
	delete(st.refMap, stripped)
	if len(stripped.Content) == 0 {
		return nil, nil
	}

	low := new(lowbase.Schema)
	if err := low.Build(ctx, stripped, nil); err != nil {
		return nil, err
	}
	return base.NewSchema(low), nil
}

// deepCopyNode clones a yaml node tree so it can be modified without touching the document
// it came from.
func deepCopyNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	c := *n
	c.Content = make([]*yaml.Node, len(n.Content))
	for i, child := range n.Content {
		c.Content[i] = deepCopyNode(child)
	}
	return &c
}

// nodeBaseURI returns the base URI in effect for a node declaring $id; an empty id keeps
// the enclosing scope's.
func nodeBaseURI(sc scope, id string) (string, error) {
	if id == "" {
		return sc.baseURI(), nil
	}
	return resolveURI(sc.baseURI(), id)
}

func boolSchemaValue(vn *yaml.Node) (value, ok bool) {
	n := resolveAlias(vn)
	if n == nil || n.Kind != yaml.ScalarNode || n.Tag != "!!bool" {
		return false, false
	}
	switch n.Value {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}

// convertSchema converts one already-built high-level schema object into a Node.
func (st *convState) convertSchema(ctx context.Context, hs *base.Schema, sc scope) (*Node, error) {
	if hs == nil {
		return nil, nil
	}
	low := hs.GoLow()

	n := &Node{Pointer: sc.docPointer, Position: schemaPosition(sc.file(), hs)}

	// $id: establishes a new base URI for this node and everything beneath it.
	effectiveBaseURI := sc.baseURI()
	childFrames := sc.frames
	if hs.Id != "" {
		n.ID = hs.Id
		abs, err := nodeBaseURI(sc, hs.Id)
		if err != nil {
			return nil, errors.Wrapf(err, "resolve $id %q at %q", hs.Id, sc.docPointer)
		}
		effectiveBaseURI = abs
		childFrames = append(append([]frame(nil), sc.frames...), frame{baseURI: abs, root: sc.docPointer})
		st.reg.resources[abs] = n
	}
	childScope := scope{frames: childFrames, docPointer: sc.docPointer, source: sc.source}

	n.Anchor = hs.Anchor
	n.DynamicAnchor = hs.DynamicAnchor
	n.DynamicRef = hs.DynamicRef
	if hs.DynamicRef != "" {
		st.reg.hasDynamicRefs = true
	}

	if low != nil && low.RootNode != nil {
		n.Ref = st.refMap[low.RootNode]
	}
	if n.Ref == "" && hs.ParentProxy != nil && hs.ParentProxy.IsReference() {
		n.Ref = hs.ParentProxy.GetReference()
	}
	if n.Ref != "" {
		st.refBaseURI[n] = effectiveBaseURI
	}

	// type
	if len(hs.Type) > 0 {
		n.HasType = true
		for _, t := range hs.Type {
			switch t {
			case "null":
				n.Types |= KindNull
			case "boolean":
				n.Types |= KindBoolean
			case "number":
				n.Types |= KindNumber
			case "integer":
				n.Types |= KindNumber
				n.IntegerType = true
			case "string":
				n.Types |= KindString
			case "array":
				n.Types |= KindArray
			case "object":
				n.Types |= KindObject
			}
		}
	}

	if hs.Const != nil {
		v, err := valueFromNode(hs.Const)
		if err != nil {
			return nil, errors.Wrapf(err, "const at %q", sc.docPointer)
		}
		n.Const = v
	}
	for _, e := range hs.Enum {
		v, err := valueFromNode(e)
		if err != nil {
			return nil, errors.Wrapf(err, "enum at %q", sc.docPointer)
		}
		if v != nil {
			n.Enum = append(n.Enum, *v)
		}
	}

	n.Minimum = hs.Minimum
	n.Maximum = hs.Maximum
	n.MultipleOf = hs.MultipleOf
	n.ExclusiveMinimum, n.ExclusiveMaximum = readExclusiveBounds(low)

	n.MinLength = int64PtrToUint64Ptr(hs.MinLength)
	n.MaxLength = int64PtrToUint64Ptr(hs.MaxLength)
	n.Pattern = optionalPattern(low, hs.Pattern)
	n.Format = hs.Format

	n.MinItems = int64PtrToUint64Ptr(hs.MinItems)
	n.MaxItems = int64PtrToUint64Ptr(hs.MaxItems)
	n.MinContains = int64PtrToUint64Ptr(hs.MinContains)
	n.MaxContains = int64PtrToUint64Ptr(hs.MaxContains)
	n.UniqueItems = hs.UniqueItems != nil && *hs.UniqueItems

	n.MinProperties = int64PtrToUint64Ptr(hs.MinProperties)
	n.MaxProperties = int64PtrToUint64Ptr(hs.MaxProperties)
	n.Required = hs.Required

	n.Discriminator = convertDiscriminator(hs.Discriminator)

	n.Title = hs.Title
	n.Description = hs.Description
	n.Deprecated = hs.Deprecated != nil && *hs.Deprecated
	n.ReadOnly = hs.ReadOnly != nil && *hs.ReadOnly
	n.WriteOnly = hs.WriteOnly != nil && *hs.WriteOnly
	if hs.Default != nil {
		v, err := valueFromNode(hs.Default)
		if err != nil {
			return nil, errors.Wrapf(err, "default at %q", sc.docPointer)
		}
		n.Default = v
	}
	for _, e := range hs.Examples {
		v, err := valueFromNode(e)
		if err != nil {
			return nil, errors.Wrapf(err, "examples at %q", sc.docPointer)
		}
		if v != nil {
			n.Examples = append(n.Examples, *v)
		}
	}
	n.XML = xmlFromSchema(hs.XML)
	ext, err := extensionsFromMap(hs.Extensions)
	if err != nil {
		return nil, errors.Wrapf(err, "extensions at %q", sc.docPointer)
	}
	n.Extensions = ext

	st.register(n, sc, effectiveBaseURI)

	// prefixItems / items / contains (array, instance-descent)
	for i, sp := range hs.PrefixItems {
		child, err := st.convertProxy(ctx, sp, childScope.child("prefixItems").childIndex(i))
		if err != nil {
			return nil, err
		}
		n.PrefixItems = append(n.PrefixItems, child)
		st.addEdge(n, child, true)
	}
	if hs.Items != nil {
		child, err := st.convertDynamicSchema(ctx, hs.Items, childScope.child("items"), schemaKeywordNode(hs, "items"))
		if err != nil {
			return nil, err
		}
		n.Items = child
		st.addEdge(n, child, true)
	}
	if hs.Contains != nil {
		child, err := st.convertProxy(ctx, hs.Contains, childScope.child("contains"))
		if err != nil {
			return nil, err
		}
		n.Contains = child
		st.addEdge(n, child, true)
	}

	// object keywords (instance-descent for property/item value positions)
	if err := st.convertNamedSchemas(ctx, hs.Properties, childScope, "properties", &n.Properties, n, true); err != nil {
		return nil, err
	}
	if err := st.convertNamedSchemas(ctx, hs.PatternProperties, childScope, "patternProperties", &n.PatternProperties, n, true); err != nil {
		return nil, err
	}
	if err := st.convertNamedSchemas(ctx, hs.DependentSchemas, childScope, "dependentSchemas", &n.DependentSchemas, n, false); err != nil {
		return nil, err
	}
	if err := st.convertNamedSchemas(ctx, hs.Defs, childScope, "$defs", &n.Defs, nil, false); err != nil {
		return nil, err
	}
	if hs.AdditionalProperties != nil {
		child, err := st.convertDynamicSchema(ctx, hs.AdditionalProperties, childScope.child("additionalProperties"), schemaKeywordNode(hs, "additionalProperties"))
		if err != nil {
			return nil, err
		}
		n.AdditionalProperties = child
		st.addEdge(n, child, true)
	}
	if hs.PropertyNames != nil {
		child, err := st.convertProxy(ctx, hs.PropertyNames, childScope.child("propertyNames"))
		if err != nil {
			return nil, err
		}
		n.PropertyNames = child
		st.addEdge(n, child, false)
	}
	if hs.UnevaluatedProperties != nil {
		child, err := st.convertDynamicSchema(ctx, hs.UnevaluatedProperties, childScope.child("unevaluatedProperties"), schemaKeywordNode(hs, "unevaluatedProperties"))
		if err != nil {
			return nil, err
		}
		n.UnevaluatedProperties = child
		st.addEdge(n, child, true)
	}
	if hs.UnevaluatedItems != nil {
		child, err := st.convertProxy(ctx, hs.UnevaluatedItems, childScope.child("unevaluatedItems"))
		if err != nil {
			return nil, err
		}
		n.UnevaluatedItems = child
		st.addEdge(n, child, true)
	}
	if dr := hs.DependentRequired; dr != nil {
		for k, v := range dr.FromOldest() {
			n.DependentRequired = append(n.DependentRequired, DependentRequired{Property: k, Requires: v})
		}
	}

	// applicators (same instance, not descent)
	for i, sp := range hs.AllOf {
		child, err := st.convertProxy(ctx, sp, childScope.child("allOf").childIndex(i))
		if err != nil {
			return nil, err
		}
		n.AllOf = append(n.AllOf, child)
		st.addEdge(n, child, false)
	}
	for i, sp := range hs.AnyOf {
		child, err := st.convertProxy(ctx, sp, childScope.child("anyOf").childIndex(i))
		if err != nil {
			return nil, err
		}
		n.AnyOf = append(n.AnyOf, child)
		st.addEdge(n, child, false)
	}
	for i, sp := range hs.OneOf {
		child, err := st.convertProxy(ctx, sp, childScope.child("oneOf").childIndex(i))
		if err != nil {
			return nil, err
		}
		n.OneOf = append(n.OneOf, child)
		st.addEdge(n, child, false)
	}
	if hs.Not != nil {
		child, err := st.convertProxy(ctx, hs.Not, childScope.child("not"))
		if err != nil {
			return nil, err
		}
		n.Not = child
		st.addEdge(n, child, false)
	}
	if hs.If != nil {
		child, err := st.convertProxy(ctx, hs.If, childScope.child("if"))
		if err != nil {
			return nil, err
		}
		n.If = child
		st.addEdge(n, child, false)
	}
	if hs.Then != nil {
		child, err := st.convertProxy(ctx, hs.Then, childScope.child("then"))
		if err != nil {
			return nil, err
		}
		n.Then = child
		st.addEdge(n, child, false)
	}
	if hs.Else != nil {
		child, err := st.convertProxy(ctx, hs.Else, childScope.child("else"))
		if err != nil {
			return nil, err
		}
		n.Else = child
		st.addEdge(n, child, false)
	}

	return n, nil
}

// convertDynamicSchema converts a bool-or-schema keyword (additionalProperties, items,
// unevaluatedProperties) into a Node, representing the boolean arm via Node.Always.
// valueNode is the keyword's source yaml node, used to position the boolean arm (the
// high-level DynamicValue keeps none).
func (st *convState) convertDynamicSchema(ctx context.Context, dv *base.DynamicValue[*base.SchemaProxy, bool], sc scope, valueNode *yaml.Node) (*Node, error) {
	if dv == nil {
		return nil, nil
	}
	if dv.IsB() {
		b := dv.B
		n := &Node{Always: &b, Pointer: sc.docPointer, Position: nodePosition(sc.file(), valueNode)}
		st.register(n, sc, sc.baseURI())
		return n, nil
	}
	return st.convertProxy(ctx, dv.A, sc)
}

func convertDiscriminator(d *base.Discriminator) *Discriminator {
	if d == nil || d.PropertyName == "" {
		return nil
	}
	out := &Discriminator{PropertyName: d.PropertyName}
	for value, ref := range d.Mapping.FromOldest() {
		out.Mapping = append(out.Mapping, DiscriminatorMapping{Value: value, Ref: ref})
	}
	return out
}

func (st *convState) convertNamedSchemas(
	ctx context.Context,
	m *orderedmap.Map[string, *base.SchemaProxy],
	sc scope,
	keyword string,
	out *[]NamedSchema,
	parent *Node,
	descent bool,
) error {
	if m == nil {
		return nil
	}
	for name, sp := range m.FromOldest() {
		child, err := st.convertProxy(ctx, sp, sc.child(keyword).child(name))
		if err != nil {
			return errors.Wrapf(err, "%s[%q]", keyword, name)
		}
		*out = append(*out, NamedSchema{Name: name, Schema: child})
		if parent != nil {
			st.addEdge(parent, child, descent)
		}
	}
	return nil
}

func (st *convState) addEdge(from, to *Node, descent bool) {
	if from == nil || to == nil {
		return
	}
	st.reg.edges[from] = append(st.reg.edges[from], edge{to: to, descent: descent})
}

// register records n in the registry: as a graph node, under every enclosing resource's
// (baseURI, resource-relative-pointer) pair, and (if present) under its $anchor /
// $dynamicAnchor within the nearest enclosing resource.
func (st *convState) register(n *Node, sc scope, nearestBaseURI string) {
	st.reg.nodes = append(st.reg.nodes, n)
	for _, f := range sc.frames {
		rel := sc.docPointer[len(f.root):]
		st.reg.pointers[f.baseURI+"\x00"+rel] = n
	}
	if n.Anchor != "" {
		st.reg.anchors[nearestBaseURI+"#"+n.Anchor] = n
	}
	if n.DynamicAnchor != "" {
		st.reg.dynAnchors[nearestBaseURI+"#"+n.DynamicAnchor] = n
	}
}

func readExclusiveBounds(low *lowbase.Schema) (exMin, exMax *float64) {
	if low == nil || low.RootNode == nil {
		return nil, nil
	}
	return readFloatKeyword(low.RootNode, "exclusiveMinimum"), readFloatKeyword(low.RootNode, "exclusiveMaximum")
}

// readFloatKeyword reads a numeric keyword directly from the source yaml node, bypassing
// libopenapi's exclusiveMinimum/exclusiveMaximum parsing: with no OpenAPI SpecIndex (as
// used by the standalone loader), libopenapi only recognizes integer-tagged scalars for
// these two keywords and silently drops float values (a libopenapi API surprise).
func readFloatKeyword(root *yaml.Node, key string) *float64 {
	v := resolveAlias(keywordNode(root, key))
	if v == nil || v.Kind != yaml.ScalarNode {
		return nil
	}
	f, err := strconv.ParseFloat(v.Value, 64)
	if err != nil {
		return nil
	}
	return &f
}

func int64PtrToUint64Ptr(p *int64) *uint64 {
	if p == nil || *p < 0 {
		return nil
	}
	v := uint64(*p)
	return &v
}

func optionalPattern(low *lowbase.Schema, val string) *string {
	if low != nil && !low.Pattern.IsEmpty() {
		v := val
		return &v
	}
	if val != "" {
		v := val
		return &v
	}
	return nil
}
