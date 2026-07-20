package conformance

import (
	"context"
	"embed"
	"io/fs"
	"path"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/plan"
)

// Real-world JSON Schema 2020-12 meta-schemas fetched from spec.openapis.org. They are
// large, self-contained, and lean heavily on the "hard tail" ($dynamicRef/$dynamicAnchor,
// unevaluatedProperties), so they are the strongest available check that Compile survives
// genuine complex 2020-12 input without panicking and classifies the unsupported
// constructs (rather than crashing or silently mis-handling them).
//
//go:embed testdata/meta
var metaFS embed.FS

const metaRoot = "testdata/meta"

// metaExpect pins the capability each meta-schema compiles to (verified against the live
// planner). These are v1-Unsupported tiers on purpose — the value is that Compile reaches
// them cleanly with diagnostics instead of erroring.
var metaExpect = map[string]plan.CapabilityLevel{
	"oas31.json":     plan.DynamicSchemaResolution,   // uses $dynamicRef/$dynamicAnchor
	"overlay11.json": plan.EvaluationStateValidation, // uses unevaluatedProperties
	"arazzo11.json":  plan.EvaluationStateValidation, // uses unevaluatedProperties
}

func TestMetaSchemas(t *testing.T) {
	var count int
	require.NoError(t, fs.WalkDir(metaFS, metaRoot, func(p string, d fs.DirEntry, err error) error {
		require.NoError(t, err)
		if d.IsDir() || path.Ext(p) != ".json" {
			return nil
		}
		rel := path.Base(p)
		t.Run(rel, func(t *testing.T) {
			data, err := metaFS.ReadFile(p)
			require.NoError(t, err)

			res, err := schemacompiler.Compile(context.Background(), data, schemacompiler.Options{})
			require.NoError(t, err, "Compile must not error on a real 2020-12 meta-schema")
			require.NotNil(t, res)
			require.NotNil(t, res.Plan.Representation, "plan must carry a representation")

			var errDiags int
			for _, dg := range res.Diagnostics {
				if dg.Severity == plan.SeverityError {
					errDiags++
				}
			}
			t.Logf("%s: capability=%d exactness=%d diagnostics=%d (errors=%d)",
				rel, res.Capability, res.Exactness, len(res.Diagnostics), errDiags)

			if want, ok := metaExpect[rel]; ok {
				require.Equal(t, want, res.Capability, "capability for %s", rel)
				require.NotEmpty(t, res.Diagnostics, "a hard-tail meta-schema should carry diagnostics")
			}
		})
		count++
		return nil
	}))
	require.NotZero(t, count, "meta corpus must not be empty")
}
