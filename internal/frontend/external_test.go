package frontend

import (
	"context"
	"net/url"
	"testing"

	"github.com/go-faster/errors"
	"github.com/stretchr/testify/require"
)

// mapLoader serves documents from an in-memory map keyed by absolute URI, recording how
// many times each URI was fetched.
func mapLoader(docs map[string]string, calls map[string]int) Loader {
	return func(_ context.Context, u *url.URL) ([]byte, error) {
		key := u.String()
		calls[key]++
		data, ok := docs[key]
		if !ok {
			return nil, errors.Errorf("no document %q", key)
		}
		return []byte(data), nil
	}
}

func TestExternalRef_PointerFragment(t *testing.T) {
	// A relative $ref resolves against the root base URI, and the fetched document's
	// #/$defs/Thing pointer is found within it.
	docs := map[string]string{
		"https://ex.com/other.json": `{"$defs": {"Thing": {"type": "string"}}}`,
	}
	calls := map[string]int{}
	root := `{"type": "object", "properties": {"thing": {"$ref": "other.json#/$defs/Thing"}}}`

	s, err := LoadWithLoader(context.Background(), []byte(root), "https://ex.com/root.json", mapLoader(docs, calls))
	require.NoError(t, err)
	require.Empty(t, s.Unresolved)

	thing := s.Root.Properties[0].Schema
	require.NotNil(t, thing.Resolved)
	require.Equal(t, KindString, thing.Resolved.Types)
	require.Equal(t, 1, calls["https://ex.com/other.json"])
}

func TestExternalRef_WholeResource(t *testing.T) {
	// A fragment-less external $ref resolves to the fetched document root.
	docs := map[string]string{
		"https://ex.com/prim.json": `{"type": "string", "minLength": 2}`,
	}
	calls := map[string]int{}
	root := `{"properties": {"p": {"$ref": "https://ex.com/prim.json"}}}`

	s, err := LoadWithLoader(context.Background(), []byte(root), "https://ex.com/root.json", mapLoader(docs, calls))
	require.NoError(t, err)
	require.Empty(t, s.Unresolved)

	p := s.Root.Properties[0].Schema
	require.NotNil(t, p.Resolved)
	require.Equal(t, KindString, p.Resolved.Types)
	require.NotNil(t, p.Resolved.MinLength)
}

func TestExternalRef_Transitive(t *testing.T) {
	// root -> a.json -> b.json: a newly-loaded document's own external ref is followed.
	docs := map[string]string{
		"https://ex.com/a.json": `{"$defs": {"A": {"$ref": "b.json#/$defs/B"}}}`,
		"https://ex.com/b.json": `{"$defs": {"B": {"type": "integer"}}}`,
	}
	calls := map[string]int{}
	root := `{"$ref": "a.json#/$defs/A"}`

	s, err := LoadWithLoader(context.Background(), []byte(root), "https://ex.com/root.json", mapLoader(docs, calls))
	require.NoError(t, err)
	require.Empty(t, s.Unresolved)

	a := s.Root.Resolved
	require.NotNil(t, a)
	require.NotNil(t, a.Resolved)
	require.True(t, a.Resolved.IntegerType)
	require.Equal(t, 1, calls["https://ex.com/a.json"])
	require.Equal(t, 1, calls["https://ex.com/b.json"])
}

func TestExternalRef_CrossDocumentCycle(t *testing.T) {
	// a.json (root) <-> b.json, each referencing the other through an object property: a
	// guarded cross-document cycle must terminate and classify as recursive-guarded.
	docs := map[string]string{
		"https://ex.com/b.json": `{"type": "object", "properties": {"a": {"$ref": "a.json"}}}`,
	}
	calls := map[string]int{}
	root := `{"type": "object", "properties": {"b": {"$ref": "b.json"}}}`

	s, err := LoadWithLoader(context.Background(), []byte(root), "https://ex.com/a.json", mapLoader(docs, calls))
	require.NoError(t, err)
	require.Empty(t, s.Unresolved)

	bNode := s.Root.Properties[0].Schema.Resolved // b.json root
	require.NotNil(t, bNode)
	backToA := bNode.Properties[0].Schema.Resolved // a.json root == s.Root
	require.Same(t, s.Root, backToA)
	require.Equal(t, Guarded, s.Registry.ClassifyRecursion(s.Root))
	require.Equal(t, 1, calls["https://ex.com/b.json"])
}

func TestExternalRef_NilLoaderStaysUnresolved(t *testing.T) {
	// Without a loader, an external ref degrades to a diagnostic (unchanged behavior).
	root := `{"properties": {"p": {"$ref": "https://ex.com/other.json#/$defs/Thing"}}}`

	s, err := LoadWithLoader(context.Background(), []byte(root), "https://ex.com/root.json", nil)
	require.NoError(t, err)
	require.Len(t, s.Unresolved, 1)
	require.Equal(t, "https://ex.com/other.json#/$defs/Thing", s.Unresolved[0].Ref)
	require.Nil(t, s.Root.Properties[0].Schema.Resolved)
}

func TestExternalRef_LoaderErrorReported(t *testing.T) {
	// A fetch failure leaves the ref unresolved, folds the loader error into the reason,
	// and is attempted only once.
	calls := map[string]int{}
	root := `{"properties": {"p": {"$ref": "https://ex.com/missing.json"}}}`

	s, err := LoadWithLoader(context.Background(), []byte(root), "https://ex.com/root.json", mapLoader(map[string]string{}, calls))
	require.NoError(t, err)
	require.Len(t, s.Unresolved, 1)
	require.Contains(t, s.Unresolved[0].Reason, "no document")
	require.Nil(t, s.Root.Properties[0].Schema.Resolved)
	require.Equal(t, 1, calls["https://ex.com/missing.json"])
}

func TestExternalRef_LoadedOncePerDocument(t *testing.T) {
	// Two refs to the same external document trigger a single fetch.
	docs := map[string]string{
		"https://ex.com/other.json": `{"$defs": {"A": {"type": "string"}, "B": {"type": "number"}}}`,
	}
	calls := map[string]int{}
	root := `{"properties": {
		"a": {"$ref": "other.json#/$defs/A"},
		"b": {"$ref": "other.json#/$defs/B"}
	}}`

	s, err := LoadWithLoader(context.Background(), []byte(root), "https://ex.com/root.json", mapLoader(docs, calls))
	require.NoError(t, err)
	require.Empty(t, s.Unresolved)
	require.NotNil(t, s.Root.Properties[0].Schema.Resolved)
	require.NotNil(t, s.Root.Properties[1].Schema.Resolved)
	require.Equal(t, 1, calls["https://ex.com/other.json"])
}

func TestExternalRef_MissingFragmentNotRefetched(t *testing.T) {
	// The document loads, but the requested pointer is absent: a genuine dangling ref, not
	// a reason to refetch. The document is fetched exactly once.
	docs := map[string]string{
		"https://ex.com/other.json": `{"$defs": {"Present": {"type": "string"}}}`,
	}
	calls := map[string]int{}
	root := `{"properties": {"p": {"$ref": "other.json#/$defs/Absent"}}}`

	s, err := LoadWithLoader(context.Background(), []byte(root), "https://ex.com/root.json", mapLoader(docs, calls))
	require.NoError(t, err)
	require.Len(t, s.Unresolved, 1)
	require.Contains(t, s.Unresolved[0].Reason, "not found")
	require.Equal(t, 1, calls["https://ex.com/other.json"])
}
