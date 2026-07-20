package frontend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/go-faster/sdk/gold"
	"github.com/stretchr/testify/require"
)

// dumpNode renders a deterministic, human-readable tree of n for golden comparison. It
// intentionally ignores Node.Resolved (printed as a pointer summary only) to keep the
// dump acyclic and stable across recursive schemas.
func dumpNode(n *Node, indent string, out *strings.Builder) {
	if n == nil {
		fmt.Fprintf(out, "%s<nil>\n", indent)
		return
	}
	if n.Always != nil {
		fmt.Fprintf(out, "%sAlways=%v\n", indent, *n.Always)
		return
	}
	fmt.Fprintf(out, "%sNode pointer=%q\n", indent, n.Pointer)
	if n.ID != "" {
		fmt.Fprintf(out, "%s  id=%q\n", indent, n.ID)
	}
	if n.Ref != "" {
		fmt.Fprintf(out, "%s  ref=%q resolved=%v\n", indent, n.Ref, n.Resolved != nil)
	}
	if n.Anchor != "" {
		fmt.Fprintf(out, "%s  anchor=%q\n", indent, n.Anchor)
	}
	if n.HasType {
		fmt.Fprintf(out, "%s  type=%08b integer=%v\n", indent, n.Types, n.IntegerType)
	}
	if n.Title != "" {
		fmt.Fprintf(out, "%s  title=%q\n", indent, n.Title)
	}
	if len(n.Required) > 0 {
		req := append([]string(nil), n.Required...)
		sort.Strings(req)
		fmt.Fprintf(out, "%s  required=%v\n", indent, req)
	}
	if n.MinLength != nil {
		fmt.Fprintf(out, "%s  minLength=%d\n", indent, *n.MinLength)
	}
	if n.MaxLength != nil {
		fmt.Fprintf(out, "%s  maxLength=%d\n", indent, *n.MaxLength)
	}
	if n.Minimum != nil {
		fmt.Fprintf(out, "%s  minimum=%v\n", indent, *n.Minimum)
	}
	if n.UniqueItems {
		fmt.Fprintf(out, "%s  uniqueItems=true\n", indent)
	}
	if n.Const != nil {
		fmt.Fprintf(out, "%s  const=%s\n", indent, string(n.Const.Raw))
	}
	if n.AdditionalProperties != nil {
		fmt.Fprintf(out, "%s  additionalProperties:\n", indent)
		dumpNode(n.AdditionalProperties, indent+"    ", out)
	}
	if n.Items != nil {
		fmt.Fprintf(out, "%s  items:\n", indent)
		dumpNode(n.Items, indent+"    ", out)
	}
	for _, p := range n.Properties {
		fmt.Fprintf(out, "%s  properties[%q]:\n", indent, p.Name)
		dumpNode(p.Schema, indent+"    ", out)
	}
	for _, d := range n.Defs {
		fmt.Fprintf(out, "%s  $defs[%q]:\n", indent, d.Name)
		dumpNode(d.Schema, indent+"    ", out)
	}
	for i, c := range n.OneOf {
		fmt.Fprintf(out, "%s  oneOf[%d]:\n", indent, i)
		dumpNode(c, indent+"    ", out)
	}
	if n.Not != nil {
		fmt.Fprintf(out, "%s  not:\n", indent)
		dumpNode(n.Not, indent+"    ", out)
	}
	if n.If != nil {
		fmt.Fprintf(out, "%s  if:\n", indent)
		dumpNode(n.If, indent+"    ", out)
	}
	if n.Then != nil {
		fmt.Fprintf(out, "%s  then:\n", indent)
		dumpNode(n.Then, indent+"    ", out)
	}
	if n.Else != nil {
		fmt.Fprintf(out, "%s  else:\n", indent)
		dumpNode(n.Else, indent+"    ", out)
	}
}

func TestLoad_Golden(t *testing.T) {
	corpus, err := filepath.Glob("testdata/corpus/*.json")
	require.NoError(t, err)
	require.NotEmpty(t, corpus)

	for _, path := range corpus {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(path) //nolint:gosec
			require.NoError(t, err)

			s, err := Load(context.Background(), data, "")
			require.NoError(t, err)

			var out strings.Builder
			dumpNode(s.Root, "", &out)
			gold.Str(t, out.String(), name+".golden")
		})
	}
}
