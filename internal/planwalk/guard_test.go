package planwalk_test

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"github.com/stretchr/testify/require"
)

// mirror is a stand-in for a real plan struct: the guard technique is a property of Go
// assignability, so proving it on a mirror proves it everywhere the same shape is used.
const mirror = `package p

type Representation interface{ isRepresentation() }

type Object struct {
	Fields     []string
	Additional Representation
}
`

// TestGuardFires proves the completeness guard actually rejects a drifted field list
// rather than being assumed to. Each case is a binding guard over the mirror struct
// above; only the one naming exactly its fields, in order, with matching types compiles.
func TestGuardFires(t *testing.T) {
	tests := []struct {
		name  string
		guard string
		ok    bool
	}{
		{
			name:  "exact",
			guard: "Fields []string\nAdditional Representation",
			ok:    true,
		},
		{
			name:  "field added to struct but not to guard",
			guard: "Fields []string",
			ok:    false,
		},
		{
			name:  "field renamed",
			guard: "Properties []string\nAdditional Representation",
			ok:    false,
		},
		{
			name:  "field retyped",
			guard: "Fields []int\nAdditional Representation",
			ok:    false,
		},
		{
			name:  "fields reordered",
			guard: "Additional Representation\nFields []string",
			ok:    false,
		},
		{
			name:  "extra field in guard",
			guard: "Fields []string\nAdditional Representation\nPatternRules []string",
			ok:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := mirror + "\nfunc walk(r Object) {\n\tvar t struct {\n" + tt.guard + "\n\t} = r\n\t_ = t\n}\n"

			err := typeCheck(t, src)
			if tt.ok {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), "cannot use r")
		})
	}
}

func typeCheck(t *testing.T, src string) error {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "guard.go", src, 0)
	require.NoError(t, err)

	conf := types.Config{Importer: importer.Default()}
	_, err = conf.Check("p", fset, []*ast.File{file}, nil)
	return err
}
