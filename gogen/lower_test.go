package gogen_test

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/gogen"
	"github.com/ogen-go/schemacompiler/internal/gotypecheck"
	"github.com/ogen-go/schemacompiler/plan"
)

// definitions compiles a document and returns the plans of every schema it names. Only
// referenced `$defs` reach the graph, so every fixture below references what it declares.
func definitions(t *testing.T, schema string) map[plan.SchemaID]plan.CompilationPlan {
	t.Helper()
	res, err := schemacompiler.Compile(context.Background(), []byte(schema), schemacompiler.Options{})
	require.NoError(t, err)
	graph, ok := res.Plan.Resolution.(*plan.StaticReferenceGraph)
	require.True(t, ok, "resolution is %T, not a static reference graph", res.Plan.Resolution)
	return graph.Definitions
}

func lower(t *testing.T, schema string) map[string]*gogen.Named {
	t.Helper()
	types, err := gogen.Lower(definitions(t, schema))
	require.NoError(t, err)
	byName := make(map[string]*gogen.Named, len(types))
	for _, n := range types {
		byName[n.Name] = n
	}
	return byName
}

// requireGoType parses want as a Go type expression. Holding an expectation that is not
// Go is what the old hand-rolled notation allowed, and it is the failure this whole file
// is meant to catch one layer down: a shape that reads right and does not compile.
// shape renders t and strips what belongs to the declaration rather than the type: struct
// tags and doc comments, which are rendering decisions and not lowering ones. It goes
// through go/parser and go/printer to do it, so an expectation below is Go by construction
// — the tables used to assert in a notation of their own, which reads like Go, is not Go,
// and parses as nothing.
var spaces = regexp.MustCompile(`[ \t]+`)

func shape(t *testing.T, g gogen.GoType) string {
	t.Helper()
	src := gogen.TypeExpr(g)
	expr, err := parser.ParseExpr(src)
	require.NoErrorf(t, err, "rendered %q is not a Go type expression", src)

	ast.Inspect(expr, func(n ast.Node) bool {
		if f, ok := n.(*ast.Field); ok {
			f.Tag, f.Doc, f.Comment = nil, nil, nil
		}
		return true
	})

	var b strings.Builder
	require.NoError(t, printer.Fprint(&b, token.NewFileSet(), expr))

	one := strings.ReplaceAll(b.String(), "\n", "; ")
	one = spaces.ReplaceAllString(one, " ")
	one = strings.ReplaceAll(strings.ReplaceAll(one, "{ ; ", "{ "), " ; }", " }")
	one = strings.ReplaceAll(strings.ReplaceAll(one, "{; ", "{ "), "; }", " }")
	one = strings.ReplaceAll(one, "struct{ ", "struct { ")
	_, err = parser.ParseExpr(one)
	require.NoErrorf(t, err, "collapsed %q is not a Go type expression", one)
	return one
}

func TestLowerShapes(t *testing.T) {
	tests := []struct {
		name string
		def  string
		want string
	}{
		{
			"required field", `{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]}`,
			`struct { A string; AdditionalProps map[string]any }`,
		},
		{
			"optional field", `{"type":"object","properties":{"a":{"type":"string"}}}`,
			`struct { A opt.Opt[string]; AdditionalProps map[string]any }`,
		},
		{
			"nullable required field", `{"type":"object","properties":{"a":{"type":["string","null"]}},"required":["a"]}`,
			`struct { A opt.Nullable[string]; AdditionalProps map[string]any }`,
		},
		{
			"nullable optional field", `{"type":"object","properties":{"a":{"type":["string","null"]}}}`,
			`struct { A opt.OptNullable[string]; AdditionalProps map[string]any }`,
		},
		{
			"closed empty object", `{"type":"object","additionalProperties":false}`,
			`struct{}`,
		},
		{
			"open map", `{"type":"object","additionalProperties":{"type":"string"}}`,
			`map[string]string`,
		},
		{
			"struct with overflow", `{"type":"object","properties":{"a":{"type":"string"}},"required":["a"],"additionalProperties":{"type":"number"}}`,
			`struct { A string; AdditionalProps map[string]float64 }`,
		},
		{
			"closed struct", `{"type":"object","properties":{"a":{"type":"string"}},"required":["a"],"additionalProperties":false}`,
			`struct { A string }`,
		},
		// A lone rule over a closed object is the only thing governing the keys, so it is
		// the map. Anything else needs a slot per governing rule, and a struct has slots.
		{
			"sole pattern rule", `{"type":"object","patternProperties":{"^a":{"type":"string"}},"additionalProperties":false}`,
			`map[string]string`,
		},
		{
			"pattern rule beside open additional", `{"type":"object","patternProperties":{"^a":{"type":"string"}}}`,
			`struct { Pattern0Props map[string]string; AdditionalProps map[string]any }`,
		},
		{
			"two rules keep their own element types", `{"type":"object","patternProperties":{"^a":{"type":"string"},"^b":{"type":"number"}},"additionalProperties":false}`,
			`struct { Pattern0Props map[string]string; Pattern1Props map[string]float64 }`,
		},
		{
			"pattern rule beside a declared field", `{"type":"object","properties":{"a":{"type":"string"}},"required":["a"],"patternProperties":{"^x":{"type":"number"}},"additionalProperties":false}`,
			`struct { A string; Pattern0Props map[string]float64 }`,
		},
		{
			"homogeneous array", `{"type":"array","items":{"type":"string"}}`,
			`[]string`,
		},
		{
			"tuple", `{"type":"array","prefixItems":[{"type":"string"},{"type":"boolean"}],"items":false}`,
			`struct { F0 opt.Opt[string]; F1 opt.Opt[bool] }`,
		},
		{
			"tuple with rest", `{"type":"array","prefixItems":[{"type":"string"}],"items":{"type":"number"}}`,
			`struct { F0 opt.Opt[string]; Rest []float64 }`,
		},
		// `prefixItems` applies to the positions an instance has, so a shorter array is
		// admitted and every slot past `minItems` may be absent.
		{
			"minItems makes the slots it covers required", `{"type":"array","prefixItems":[{"type":"string"},{"type":"boolean"}],"items":false,"minItems":1}`,
			`struct { F0 string; F1 opt.Opt[bool] }`,
		},
		{"integer", `{"type":"integer"}`, `int64`},
		{"format survives", `{"type":"string","format":"date-time"}`, `string`},
		{"any", `{}`, `any`},
		{"union", `{"oneOf":[{"type":"string"},{"type":"boolean"}]}`, `any`},
		{"union with null alternative", `{"type":["string","boolean","null"]}`, `opt.Nullable[any]`},
		{
			"nested object is anonymous", `{"type":"object","properties":{"a":{"type":"object","properties":{"b":{"type":"string"}},"required":["b"]}},"required":["a"]}`,
			`struct { A struct { B string; AdditionalProps map[string]any }; AdditionalProps map[string]any }`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := fmt.Sprintf(`{"$defs":{"T":%s},"$ref":"#/$defs/T"}`, tt.def)
			types := lower(t, schema)
			decl, ok := types["T"]
			require.True(t, ok, "lowered %v", types)
			require.Equal(t, tt.want, shape(t, decl.Underlying))
			require.False(t, decl.Recursive)
		})
	}
}

// TestLowerTypesTheOverflow pins that the overflow map follows the schema. `any` is what
// `additionalProperties` absent and `additionalProperties: true` both mean — the two are
// the same claim about what is accepted — and it is not what a stated schema means.
func TestLowerTypesTheOverflow(t *testing.T) {
	for _, tt := range []struct {
		name string
		defs string
		want string
	}{
		{
			"a reference is carried into the element type",
			`"Pet":{"type":"object","properties":{"n":{"type":"string"}},"required":["n"],"additionalProperties":false},
			 "T":{"type":"object","properties":{"a":{"type":"string"}},"required":["a"],"additionalProperties":{"$ref":"#/$defs/Pet"}}`,
			`struct { A string; AdditionalProps map[string]Pet }`,
		},
		{
			"so is a scalar",
			`"T":{"type":"object","properties":{"a":{"type":"string"}},"required":["a"],"additionalProperties":{"type":"integer"}}`,
			`struct { A string; AdditionalProps map[string]int64 }`,
		},
		{
			"additionalProperties: true accepts anything, so the element type is anything",
			`"T":{"type":"object","properties":{"a":{"type":"string"}},"required":["a"],"additionalProperties":true}`,
			`struct { A string; AdditionalProps map[string]any }`,
		},
		{
			"and absent says the same thing",
			`"T":{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]}`,
			`struct { A string; AdditionalProps map[string]any }`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			types := lower(t, fmt.Sprintf(
				`{"$defs":{%s},"type":"object","properties":{"t":{"$ref":"#/$defs/T"}}}`, tt.defs))
			require.Equal(t, tt.want, shape(t, types["T"].Underlying))
		})
	}
}

func TestLowerBreaksCyclesAtTheNode(t *testing.T) {
	tests := []struct {
		name      string
		defs      string
		want      map[string]string
		recursive []string
	}{
		{
			name: "self reference through a field",
			defs: `"Node":{"type":"object","properties":{"child":{"$ref":"#/$defs/Node"}}}`,
			want: map[string]string{"Node": `struct { Child opt.Opt[*Node]; AdditionalProps map[string]any }`},
			// The cycle is Node -> Node: an opt.Opt stores its value inline, so
			// without the pointer the type would not compile.
			recursive: []string{"Node"},
		},
		{
			name: "a slice already breaks the cycle",
			defs: `"Tree":{"type":"object","properties":{"kids":{"type":"array","items":{"$ref":"#/$defs/Tree"}}}}`,
			want: map[string]string{"Tree": `struct { Kids opt.Opt[[]Tree]; AdditionalProps map[string]any }`},
		},
		{
			name: "a map already breaks the cycle",
			defs: `"Bag":{"type":"object","properties":{"more":{"type":"object","additionalProperties":{"$ref":"#/$defs/Bag"}}}}`,
			want: map[string]string{"Bag": `struct { More opt.Opt[map[string]Bag]; AdditionalProps map[string]any }`},
		},
		{
			name: "mutual recursion points both ways",
			defs: `"A":{"type":"object","properties":{"b":{"$ref":"#/$defs/B"}}},
				"B":{"type":"object","properties":{"a":{"$ref":"#/$defs/A"}}}`,
			want: map[string]string{
				"A": `struct { B opt.Opt[*B]; AdditionalProps map[string]any }`,
				"B": `struct { A opt.Opt[*A]; AdditionalProps map[string]any }`,
			},
			recursive: []string{"A", "B"},
		},
		{
			// The reference from outside the cycle is a pointer too. Which edge to cut
			// is not canonical; which node is in a cycle is.
			name: "a reference from outside the cycle is a pointer",
			defs: `"Node":{"type":"object","properties":{"child":{"$ref":"#/$defs/Node"}}},
				"Holder":{"type":"object","properties":{"n":{"$ref":"#/$defs/Node"}},"required":["n"]}`,
			want: map[string]string{
				"Node":   `struct { Child opt.Opt[*Node]; AdditionalProps map[string]any }`,
				"Holder": `struct { N *Node; AdditionalProps map[string]any }`,
			},
			recursive: []string{"Node"},
		},
		{
			name: "cycle through a tuple slot",
			defs: `"Pair":{"type":"array","prefixItems":[{"type":"string"},{"$ref":"#/$defs/Pair"}],"items":false}`,
			want: map[string]string{"Pair": `struct { F0 opt.Opt[string]; F1 opt.Opt[*Pair] }`},
			// prefixItems is a tuple slot, which stores inline like a struct field.
			recursive: []string{"Pair"},
		},
		{
			name: "cycle through a union alternative",
			defs: `"Alt":{"type":"object","properties":{"v":{"oneOf":[{"type":"string"},{"$ref":"#/$defs/Alt"}]}}}`,
			// A union is an interface, which is indirection the language already gives.
			want: map[string]string{"Alt": `struct { V opt.Opt[any]; AdditionalProps map[string]any }`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var refs []string
			for name := range tt.want {
				refs = append(refs, fmt.Sprintf(`%q:{"$ref":"#/$defs/%s"}`, strings.ToLower(name), name))
			}
			schema := fmt.Sprintf(`{"$defs":{%s},"type":"object","properties":{%s}}`,
				tt.defs, strings.Join(refs, ","))

			types := lower(t, schema)
			for name, want := range tt.want {
				decl, ok := types[name]
				require.True(t, ok, "no type %q in %v", name, types)
				require.Equal(t, want, shape(t, decl.Underlying), "type %q", name)
			}
			var got []string
			for name, decl := range types {
				if decl.Recursive {
					got = append(got, name)
				}
			}
			require.ElementsMatch(t, tt.recursive, got, "recursive set")
		})
	}
}

// TestLowerIsReproducible lowers the same input twice in one process. Map iteration is
// randomized per run, so an order leak from any of the three passes shows up here.
func TestLowerIsReproducible(t *testing.T) {
	defs := definitions(t, `{
		"$defs":{
			"A":{"type":"object","properties":{"b":{"$ref":"#/$defs/B"},"c":{"$ref":"#/$defs/C"}}},
			"B":{"type":"object","properties":{"a":{"$ref":"#/$defs/A"}}},
			"C":{"type":"object","properties":{"a":{"$ref":"#/$defs/A"},"b":{"$ref":"#/$defs/B"}}}
		},
		"type":"object","properties":{"a":{"$ref":"#/$defs/A"}}
	}`)

	render := func() string {
		types, err := gogen.Lower(defs)
		require.NoError(t, err)
		var b strings.Builder
		for _, n := range types {
			fmt.Fprintf(&b, "%s %s recursive=%v\n", n.Name, shape(t, n.Underlying), n.Recursive)
		}
		return b.String()
	}

	want := render()
	require.Contains(t, want, "recursive=true")
	for range 32 {
		require.Equal(t, want, render())
	}
}

func TestLowerReportsUnresolvedReference(t *testing.T) {
	_, err := gogen.Lower(map[plan.SchemaID]plan.CompilationPlan{
		"/$defs/A": {Representation: &plan.ReferenceRepresentation{Name: "/$defs/Missing"}},
	})
	require.ErrorContains(t, err, "resolves to no schema")
}

func TestLowerRefusesCollidingFieldNames(t *testing.T) {
	_, err := gogen.Lower(map[plan.SchemaID]plan.CompilationPlan{
		"/$defs/A": {Representation: &plan.ObjectRepresentation{
			Fields: []plan.FieldRepresentation{
				{Name: "user-id", Plan: plan.CompilationPlan{Representation: &plan.AnyRepresentation{}}},
				{Name: "user_id", Plan: plan.CompilationPlan{Representation: &plan.AnyRepresentation{}}},
			},
			Additional: &plan.CompilationPlan{Representation: &plan.NeverRepresentation{}},
		}},
	})
	require.ErrorContains(t, err, `both derive the Go field name "UserId"`)
}

func TestLowerHonoursFieldNameExtension(t *testing.T) {
	types, err := gogen.Lower(map[plan.SchemaID]plan.CompilationPlan{
		"/$defs/A": {Representation: &plan.ObjectRepresentation{
			Fields: []plan.FieldRepresentation{{
				Name:     "slash/field",
				Plan:     plan.CompilationPlan{Representation: &plan.PrimitiveRepresentation{Kind: plan.KindString}},
				Presence: plan.PresenceRequired,
				Metadata: plan.Metadata{Extensions: map[string]any{gogen.NameExtension: "Slash"}},
			}},
			Additional: &plan.CompilationPlan{Representation: &plan.NeverRepresentation{}},
		}},
	})
	require.NoError(t, err)
	require.Len(t, types, 1)
	require.Equal(t, `struct { Slash string }`, shape(t, types[0].Underlying))
}

// TestLoweredTypesCompile renders each fixture and hands it to the Go type checker. It is
// the assertion no shape comparison can make: "invalid recursive type" is a property of the
// graph, not of any one type, so a test can assert a pointer appears exactly where it
// expects and still describe a package that does not build. go/parser does not see it
// either — such a file parses — which is why this goes all the way to go/types.
func TestLoweredTypesCompile(t *testing.T) {
	schemas := map[string]string{
		"self reference":     `{"$defs":{"Node":{"type":"object","properties":{"child":{"$ref":"#/$defs/Node"}}}},"type":"object","properties":{"n":{"$ref":"#/$defs/Node"}}}`,
		"mutual recursion":   `{"$defs":{"A":{"type":"object","properties":{"b":{"$ref":"#/$defs/B"}}},"B":{"type":"object","properties":{"a":{"$ref":"#/$defs/A"}}}},"type":"object","properties":{"a":{"$ref":"#/$defs/A"}}}`,
		"recursive tuple":    `{"$defs":{"Pair":{"type":"array","prefixItems":[{"type":"string"},{"$ref":"#/$defs/Pair"}],"items":false}},"type":"object","properties":{"p":{"$ref":"#/$defs/Pair"}}}`,
		"recursion via ref":  `{"$defs":{"A":{"$ref":"#/$defs/B"},"B":{"$ref":"#/$defs/A"}},"type":"object","properties":{"a":{"$ref":"#/$defs/A"}}}`,
		"cycle via a slice":  `{"$defs":{"Tree":{"type":"object","properties":{"kids":{"type":"array","items":{"$ref":"#/$defs/Tree"}}}}},"type":"object","properties":{"t":{"$ref":"#/$defs/Tree"}}}`,
		"pattern properties": `{"$defs":{"P":{"type":"object","properties":{"a":{"type":"string"}},"patternProperties":{"^x":{"type":"number"},"^y":{"$ref":"#/$defs/P"}}}},"type":"object","properties":{"p":{"$ref":"#/$defs/P"}}}`,
	}

	for name, schema := range schemas {
		t.Run(name, func(t *testing.T) {
			types, err := gogen.Lower(definitions(t, schema))
			require.NoError(t, err)
			files, err := gogen.Render(types, gogen.Options{})
			require.NoError(t, err)
			require.NoError(t, gotypecheck.Check(files, "../opt"), "rendered:\n%s", files[0].Content)
		})
	}
}

// TestLoweredTypesNeedTheRecursionPass is the control: without the pointers, the same
// fixture is the "invalid recursive type" this is all here to prevent. If it ever stops
// failing, [TestLoweredTypesCompile] has stopped proving anything.
func TestLoweredTypesNeedTheRecursionPass(t *testing.T) {
	node := &gogen.Named{ID: "/$defs/Node", Name: "Node"}
	node.Underlying = &gogen.Struct{Fields: []gogen.Field{
		{Name: "Child", JSON: "child", Type: &gogen.Presence{Elem: node, Optional: true}},
	}}
	files, err := gogen.Render([]*gogen.Named{node}, gogen.Options{})
	require.NoError(t, err)
	require.ErrorContains(t, gotypecheck.Check(files, "../opt"), "invalid recursive type")
}
