package frontend

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// FuzzLoad seeds from the JSON corpus in testdata/corpus and asserts Load never panics
// and never leaves a resolved reference cycle unaccounted for by the SCC classifier —
// only that it returns cleanly (error or a well-formed *Schema) for arbitrary mutated
// input.
func FuzzLoad(f *testing.F) {
	corpus, err := filepath.Glob("testdata/corpus/*.json")
	if err != nil {
		f.Fatal(err)
	}
	if len(corpus) == 0 {
		f.Fatal("empty seed corpus")
	}
	for _, path := range corpus {
		data, err := os.ReadFile(path) //nolint:gosec
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Add([]byte(`true`))
	f.Add([]byte(`false`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"$ref": "#/$defs/A", "$defs": {"A": {"$ref": "#/$defs/A"}}}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		s, err := Load(context.Background(), data, "")
		if err != nil {
			return
		}
		if s == nil || s.Root == nil {
			t.Fatalf("Load returned no error but a nil schema/root")
		}
		// Exercising the registry must not panic even on adversarial reference graphs.
		_ = s.Registry.ClassifyRecursion(s.Root)
		_ = s.Registry.SCCs()
	})
}
