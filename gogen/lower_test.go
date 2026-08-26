package gogen_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/gogen"
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

// sig spells a type compactly enough to assert in a table. A named type at a use site is
// its name alone, which is also what keeps sig terminating on a recursive graph.
func sig(t gogen.GoType) string {
	switch t := t.(type) {
	case *gogen.Named:
		return t.Name
	case *gogen.Pointer:
		return "*" + sig(t.Elem)
	case *gogen.Slice:
		return "[]" + sig(t.Elem)
	case *gogen.Map:
		return "map[string]" + sig(t.Elem)
	case *gogen.Tuple:
		parts := make([]string, 0, len(t.Elems)+1)
		for _, e := range t.Elems {
			parts = append(parts, sig(e))
		}
		if t.Rest != nil {
			parts = append(parts, "..."+sig(t.Rest))
		}
		return "tuple(" + strings.Join(parts, ",") + ")"
	case *gogen.Struct:
		parts := make([]string, 0, len(t.Fields)+1)
		for _, f := range t.Fields {
			parts = append(parts, fmt.Sprintf("%s %s", f.Name, sig(f.Type)))
		}
		if t.Additional != nil {
			parts = append(parts, "..."+sig(t.Additional))
		}
		return "struct{" + strings.Join(parts, ";") + "}"
	case *gogen.Presence:
		switch {
		case t.Optional && t.Nullable:
			return "optnull[" + sig(t.Elem) + "]"
		case t.Optional:
			return "opt[" + sig(t.Elem) + "]"
		default:
			return "null[" + sig(t.Elem) + "]"
		}
	case *gogen.Interface:
		parts := make([]string, len(t.Variants))
		for i, v := range t.Variants {
			parts[i] = sig(v)
		}
		return "sum(" + strings.Join(parts, "|") + ")"
	case *gogen.Primitive:
		if t.Format != "" {
			return t.Kind.String() + "/" + t.Format
		}
		return t.Kind.String()
	case *gogen.Any:
		return "any"
	case *gogen.Never:
		return "never"
	default:
		panic(fmt.Sprintf("unhandled %T", t))
	}
}

func TestLowerShapes(t *testing.T) {
	tests := []struct {
		name string
		def  string
		want string
	}{
		{
			"required field", `{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]}`,
			`struct{A string;...any}`,
		},
		{
			"optional field", `{"type":"object","properties":{"a":{"type":"string"}}}`,
			`struct{A opt[string];...any}`,
		},
		{
			"nullable required field", `{"type":"object","properties":{"a":{"type":["string","null"]}},"required":["a"]}`,
			`struct{A null[string];...any}`,
		},
		{
			"nullable optional field", `{"type":"object","properties":{"a":{"type":["string","null"]}}}`,
			`struct{A optnull[string];...any}`,
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
			`struct{A string;...float}`,
		},
		{
			"closed struct", `{"type":"object","properties":{"a":{"type":"string"}},"required":["a"],"additionalProperties":false}`,
			`struct{A string}`,
		},
		{
			"sole pattern rule", `{"type":"object","patternProperties":{"^a":{"type":"string"}},"additionalProperties":false}`,
			`map[string]string`,
		},
		{
			"pattern rule beside open additional", `{"type":"object","patternProperties":{"^a":{"type":"string"}}}`,
			`map[string]any`,
		},
		{
			"homogeneous array", `{"type":"array","items":{"type":"string"}}`,
			`[]string`,
		},
		{
			"tuple", `{"type":"array","prefixItems":[{"type":"string"},{"type":"boolean"}],"items":false}`,
			`tuple(string,bool)`,
		},
		{
			"tuple with rest", `{"type":"array","prefixItems":[{"type":"string"}],"items":{"type":"number"}}`,
			`tuple(string,...float)`,
		},
		{"integer", `{"type":"integer"}`, `int`},
		{"format survives", `{"type":"string","format":"date-time"}`, `string/date-time`},
		{"any", `{}`, `any`},
		{"union", `{"oneOf":[{"type":"string"},{"type":"boolean"}]}`, `sum(string|bool)`},
		{"union with null alternative", `{"type":["string","boolean","null"]}`, `null[sum(bool|string)]`},
		{
			"nested object is anonymous", `{"type":"object","properties":{"a":{"type":"object","properties":{"b":{"type":"string"}},"required":["b"]}},"required":["a"]}`,
			`struct{A struct{B string;...any};...any}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := fmt.Sprintf(`{"$defs":{"T":%s},"$ref":"#/$defs/T"}`, tt.def)
			types := lower(t, schema)
			decl, ok := types["T"]
			require.True(t, ok, "lowered %v", types)
			require.Equal(t, tt.want, sig(decl.Underlying))
			require.False(t, decl.Recursive)
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
			want: map[string]string{"Node": `struct{Child opt[*Node];...any}`},
			// The cycle is Node -> Node: an opt.Opt stores its value inline, so
			// without the pointer the type would not compile.
			recursive: []string{"Node"},
		},
		{
			name: "a slice already breaks the cycle",
			defs: `"Tree":{"type":"object","properties":{"kids":{"type":"array","items":{"$ref":"#/$defs/Tree"}}}}`,
			want: map[string]string{"Tree": `struct{Kids opt[[]Tree];...any}`},
		},
		{
			name: "a map already breaks the cycle",
			defs: `"Bag":{"type":"object","properties":{"more":{"type":"object","additionalProperties":{"$ref":"#/$defs/Bag"}}}}`,
			want: map[string]string{"Bag": `struct{More opt[map[string]Bag];...any}`},
		},
		{
			name: "mutual recursion points both ways",
			defs: `"A":{"type":"object","properties":{"b":{"$ref":"#/$defs/B"}}},
				"B":{"type":"object","properties":{"a":{"$ref":"#/$defs/A"}}}`,
			want: map[string]string{
				"A": `struct{B opt[*B];...any}`,
				"B": `struct{A opt[*A];...any}`,
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
				"Node":   `struct{Child opt[*Node];...any}`,
				"Holder": `struct{N *Node;...any}`,
			},
			recursive: []string{"Node"},
		},
		{
			name: "cycle through a tuple slot",
			defs: `"Pair":{"type":"array","prefixItems":[{"type":"string"},{"$ref":"#/$defs/Pair"}],"items":false}`,
			want: map[string]string{"Pair": `tuple(string,*Pair)`},
			// prefixItems is a tuple slot, which stores inline like a struct field.
			recursive: []string{"Pair"},
		},
		{
			name: "cycle through a union alternative",
			defs: `"Alt":{"type":"object","properties":{"v":{"oneOf":[{"type":"string"},{"$ref":"#/$defs/Alt"}]}}}`,
			// A union is an interface, which is indirection the language already gives.
			want: map[string]string{"Alt": `struct{V opt[sum(string|Alt)];...any}`},
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
				require.Equal(t, want, sig(decl.Underlying), "type %q", name)
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
			fmt.Fprintf(&b, "%s %s recursive=%v\n", n.Name, sig(n.Underlying), n.Recursive)
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
	require.Equal(t, `struct{Slash string}`, sig(types[0].Underlying))
}
