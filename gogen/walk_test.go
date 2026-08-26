package gogen_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/gogen"
	"github.com/ogen-go/schemacompiler/internal/gotypecheck"
)

func edgeString(e gogen.Edge) string {
	var b strings.Builder
	b.WriteString(e.Kind.String())
	if e.Name != "" {
		fmt.Fprintf(&b, "(%s)", e.Name)
	}
	if e.Kind == gogen.EdgeField || e.Kind == gogen.EdgePattern ||
		e.Kind == gogen.EdgeTupleElem || e.Kind == gogen.EdgeVariant {
		fmt.Fprintf(&b, "[%d]", e.Index)
	}
	if e.Indirect {
		b.WriteString("*")
	}
	return b.String()
}

func TestChildren(t *testing.T) {
	named := &gogen.Named{ID: "/$defs/T", Name: "T", Underlying: str()}

	for _, tt := range []struct {
		name string
		t    gogen.GoType
		want []string
	}{
		{"named", named, []string{"underlying"}},
		{"primitive", str(), nil},
		{"any", anyT(), nil},
		{"never", &gogen.Never{}, nil},
		// A slice, a map and an interface hold their element behind indirection Go
		// already provides, which is exactly what a cycle cannot close through.
		{"slice", &gogen.Slice{Elem: str()}, []string{"elem*"}},
		{"map", &gogen.Map{Elem: str()}, []string{"elem*"}},
		{
			"interface", &gogen.Interface{Variants: []gogen.GoType{str(), num()}},
			[]string{"variant[0]*", "variant[1]*"},
		},
		{"pointer", &gogen.Pointer{Elem: named}, []string{"pointee*"}},
		// A struct field, a tuple slot and an opt wrapper store the value inline.
		{"presence", &gogen.Presence{Elem: str(), Optional: true}, []string{"stored"}},
		{
			"tuple", &gogen.Tuple{Elems: []gogen.GoType{str(), num()}, Rest: anyT()},
			[]string{"tuple-elem[0]", "tuple-elem[1]", "tuple-rest*"},
		},
		{"struct", &gogen.Struct{
			Fields:     []gogen.Field{{Name: "A", JSON: "a", Type: str()}},
			Patterns:   []*gogen.Map{{Elem: num(), Pattern: "^p"}},
			Additional: anyT(),
		}, []string{"field(A)[0]", "pattern(^p)[0]", "additional*"}},
		{
			"closed struct", &gogen.Struct{Fields: []gogen.Field{{Name: "A", JSON: "a", Type: str()}}},
			[]string{"field(A)[0]"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for n := range gogen.Children(tt.t) {
				got = append(got, edgeString(n.Edge))
			}
			require.Equal(t, tt.want, got)
		})
	}
}

func TestChildrenCarriesTheJSONName(t *testing.T) {
	s := &gogen.Struct{Fields: []gogen.Field{{Name: "UserID", JSON: "user_id", Type: str()}}}
	n := slices.Collect(gogen.Children(s))
	require.Len(t, n, 1)
	require.Equal(t, "UserID", n[0].Edge.Name)
	require.Equal(t, "user_id", n[0].Edge.JSON)
}

func TestFoldVisitsInPreOrder(t *testing.T) {
	s := &gogen.Struct{
		Fields: []gogen.Field{{Name: "A", JSON: "a", Type: &gogen.Presence{Elem: &gogen.Slice{Elem: str()}, Optional: true}}},
	}
	var got []string
	gogen.Fold(s, 0, func(acc int, n gogen.Node) (int, gogen.Action) {
		got = append(got, edgeString(n.Edge)+":"+gotypecheck.Type(n.Type))
		return acc, gogen.Descend
	})
	require.Equal(t, []string{
		"root:struct { A Opt[[]string] }",
		"field(A)[0]:Opt[[]string]",
		"stored:[]string",
		"elem*:string",
	}, got)
}

// TestFoldTerminatesOnACycle is the difference from planwalk: a lowered graph is cyclic by
// construction, so the walk has to stop itself. Every cycle closes through a Named, which
// is what makes recording those sufficient.
func TestFoldTerminatesOnACycle(t *testing.T) {
	node := &gogen.Named{ID: "/$defs/Node", Name: "Node"}
	node.Underlying = &gogen.Struct{Fields: []gogen.Field{
		{Name: "Child", JSON: "child", Type: &gogen.Presence{Elem: &gogen.Pointer{Elem: node}, Optional: true}},
	}}

	var visited []string
	revisits := gogen.Fold(node, 0, func(acc int, n gogen.Node) (int, gogen.Action) {
		visited = append(visited, edgeString(n.Edge))
		if n.Revisit {
			acc++
		}
		return acc, gogen.Descend
	})
	require.Equal(t, []string{"root", "underlying", "field(Child)[0]", "stored", "pointee*"}, visited)
	require.Equal(t, 1, revisits, "the reference back to Node is delivered once, not descended")
}

func TestFoldSkipAndStop(t *testing.T) {
	s := &gogen.Struct{Fields: []gogen.Field{
		{Name: "A", JSON: "a", Type: &gogen.Slice{Elem: str()}},
		{Name: "B", JSON: "b", Type: num()},
	}}

	var skipped []string
	gogen.Fold(s, 0, func(acc int, n gogen.Node) (int, gogen.Action) {
		skipped = append(skipped, edgeString(n.Edge))
		if n.Edge.Kind == gogen.EdgeField && n.Edge.Index == 0 {
			return acc, gogen.Skip
		}
		return acc, gogen.Descend
	})
	require.Equal(t, []string{"root", "field(A)[0]", "field(B)[1]"}, skipped, "the slice under A is not entered")

	var stopped []string
	gogen.Fold(s, 0, func(acc int, n gogen.Node) (int, gogen.Action) {
		stopped = append(stopped, edgeString(n.Edge))
		if n.Edge.Kind == gogen.EdgeField {
			return acc, gogen.Stop
		}
		return acc, gogen.Descend
	})
	require.Equal(t, []string{"root", "field(A)[0]"}, stopped)
}

// TestEveryVariantIsWalkable drives every [gogen.GoType] through the traversals Go cannot
// check exhaustively for us. Each ends in a panicking default, so a variant added to the
// sum and forgotten here fails loudly rather than being silently skipped.
func TestEveryVariantIsWalkable(t *testing.T) {
	for _, v := range gogen.AllGoTypes() {
		t.Run(fmt.Sprintf("%T", v), func(t *testing.T) {
			require.NotPanics(t, func() { _ = slices.Collect(gogen.Children(v)) }, "Children")
			require.NotPanics(t, func() {
				gogen.Fold(v, 0, func(acc int, _ gogen.Node) (int, gogen.Action) { return acc, gogen.Descend })
			}, "Fold")
			require.NotPanics(t, func() { _ = gogen.Kinds(v) }, "Kinds")
			require.NotPanics(t, func() { _ = gotypecheck.Type(v) }, "gotypecheck.Type")
		})
	}
}

// TestEveryVariantHasAnEdgeName pins that a new EdgeKind gets a name rather than printing
// as a number in whatever diagnostic first meets it.
func TestEveryVariantHasAnEdgeName(t *testing.T) {
	seen := map[gogen.EdgeKind]bool{gogen.EdgeRoot: true}
	for _, v := range gogen.AllGoTypes() {
		for n := range gogen.Children(v) {
			seen[n.Edge.Kind] = true
		}
	}
	require.Len(t, seen, 11, "every EdgeKind must be reachable from AllGoTypes")
	for k := range seen {
		require.NotContains(t, k.String(), "edge-kind(", "EdgeKind %d has no name", uint8(k))
	}
}
