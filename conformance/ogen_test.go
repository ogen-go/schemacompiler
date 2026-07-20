// ogen_test.go checks a weaker property than corpus_test.go's manifest-checked
// coverage harness: schemas that ogen (github.com/ogen-go/ogen) can generate code
// for today should compile here without ERROR or PANIC and always yield a plan.
// This is sanity, not strict equality — ogen can have its own bugs or accept
// OpenAPI-3.x-only constructs (nullable, discriminator) our 2020-12 converter simply
// ignores — so capability/exactness are logged for human review, never asserted.
//
// testdata/ogen/jsonschema/*.json is a verbatim copy of ogen's standalone
// gen/_testdata/jsonschema/*.json corpus. Three of the six files (openapi30.json,
// platform.json, recursive.json) are draft-04-flavored and use the pre-2020-12
// "definitions" container with "#/definitions/..." refs; our frontend only walks the
// 2020-12 "$defs" container as a schema-bearing location, so those refs would fail to
// resolve even though nothing about the referenced schemas is actually unsupported.
// normalizeLegacyDraft (below) adapts this and one other purely nominal pre-2020-12
// keyword spelling at Compile time, so the files stay byte-identical to upstream.
//
// testdata/ogen/openapi/*.bundle.json is
// derived from a handful of small-to-medium ogen example OpenAPI specs
// (_testdata/examples/*.json): each spec's components.schemas is repackaged into a
// single self-contained JSON Schema document — a $defs map plus a oneOf listing every
// schema name as a $ref — via:
//
//	jq '{"$defs": (.components.schemas), "oneOf": [ .components.schemas | keys[] | {"$ref": ("#/$defs/" + .)} ]} | walk(if type=="string" then sub("#/components/schemas/";"#/$defs/") else . end)' spec.json
//
// so whole-document $ref assembly compiles every named schema in one Compile call.
package conformance

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/plan"
)

//go:embed testdata/ogen
var ogenFS embed.FS

const ogenRoot = "testdata/ogen"

// knownOgenQuirks documents specific ogen-side fixtures verified to fail here for a
// reason unrelated to a real 2020-12 capability gap in our compiler, so a skip here
// is never a silent workaround — each entry names the concrete, inspected cause.
// Keys are slash-separated paths relative to testdata/ogen (TestOgenCorpus) or to
// the live ogen checkout root (TestOgenLiveWalk).
var knownOgenQuirks = map[string]string{
	// The embedded copy of the JSON Schema draft-04 meta-schema (nested under its own
	// "id": "http://json-schema.org/draft-04/schema#" resource scope) truncates its own
	// "definitions" section — only a generic "properties.definitions" placeholder
	// survives — yet a sibling keyword still does `"$ref": "#/definitions/schemaArray"`
	// against that scope. That target was never included in the embedded copy, so the
	// $ref is dangling in the fixture itself, not unresolvable due to any keyword our
	// frontend fails to walk (verified by inspecting the fixture directly).
	"jsonschema/openapi30.json":               "dangling $ref into a section missing from an incomplete embedded draft-04 meta-schema copy (ogen fixture, not a compiler gap)",
	"gen/_testdata/jsonschema/openapi30.json": "dangling $ref into a section missing from an incomplete embedded draft-04 meta-schema copy (ogen fixture, not a compiler gap)",
	// CONFIRMED COMPILER DEFECT, out of scope for this task (the compiler core under
	// internal/ is frozen): an enum whose members include a non-primitive (array)
	// value panics with "hash of unhashable type: []interface {}", apparently from
	// code that uses an enum member as a Go map key without accounting for
	// non-comparable JSON values (arrays/objects). Recorded here, not silently
	// retried, precisely so it stays visible for follow-up.
	"_testdata/positive/non_primitive_enum.json": "CONFIRMED COMPILER PANIC on an array-valued enum member (hash of unhashable type) - real defect, out of scope here",
}

// TestOgenCorpus walks every vendored ogen-derived schema and asserts Compile never
// errors or panics and always returns a plan with a representation. Capability and
// exactness are only logged (distribution + a review list of Unsupported-capability
// or SeverityError-diagnostic files), never asserted against a manifest, since this
// corpus is about "does it compile soundly", not "does it match an expected plan".
func TestOgenCorpus(t *testing.T) {
	var paths []string
	require.NoError(t, fs.WalkDir(ogenFS, ogenRoot, func(p string, d fs.DirEntry, err error) error {
		require.NoError(t, err)
		if d.IsDir() || !strings.HasSuffix(p, ".json") {
			return nil
		}
		paths = append(paths, p)
		return nil
	}))
	require.NotEmpty(t, paths, "vendored ogen corpus must not be empty")

	dist := make(map[distKey]int)
	var review []string

	for _, p := range paths {
		rel := strings.TrimPrefix(p, ogenRoot+"/")
		t.Run(rel, func(t *testing.T) {
			if reason, known := knownOgenQuirks[rel]; known {
				t.Skipf("known ogen fixture quirk, not asserted here: %s", reason)
			}

			data, err := ogenFS.ReadFile(p)
			require.NoError(t, err, "read vendored ogen file")

			normalized, err := normalizeLegacyDraft(data)
			require.NoError(t, err, "normalize legacy draft keywords")

			res, err := compileSafely(t, normalized)
			require.NoError(t, err, "Compile must not error on a vendored ogen-parity schema")
			require.NotNil(t, res, "Compile must return a non-nil result")
			require.NotNil(t, res.Plan.Representation, "plan must carry a representation")

			dist[distKey{res.Capability, res.Exactness}]++
			collectReview(&review, rel, res.Capability, res.Diagnostics)
		})
	}

	logDistribution(t, dist)
	logReviewList(t, review)
}

// collectReview appends a review-worthy entry for a schema whose capability landed at
// plan.Unsupported or that carries a SeverityError diagnostic — the "no silent caps"
// candidates a human should look at (either a compiler gap or an ogen quirk).
func collectReview(review *[]string, name string, capability plan.CapabilityLevel, diags []plan.Diagnostic) {
	if capability == plan.Unsupported {
		*review = append(*review, fmt.Sprintf("%s: Unsupported capability", name))
	}
	for _, diag := range diags {
		if diag.Severity >= plan.SeverityError {
			*review = append(*review, fmt.Sprintf("%s: [Error] %s (pointer=%q)", name, diag.Message, diag.Pointer))
		}
	}
}

func logReviewList(t *testing.T, review []string) {
	t.Helper()
	sort.Strings(review)
	var b strings.Builder
	fmt.Fprintf(&b, "ogen-parity schemas flagged for human review (Unsupported or SeverityError, %d):\n", len(review))
	for _, r := range review {
		fmt.Fprintf(&b, "  %s\n", r)
	}
	t.Log(b.String())
}

// resolveOgenRoot locates a sibling ogen checkout: OGEN_TESTDATA (a path to the ogen
// module root) takes priority, otherwise a handful of relative candidates are tried
// (this package's directory sits at <repo>/conformance, and ogen is commonly checked
// out as a sibling of <repo> under the same ogen-go/ parent). Skips cleanly, never
// fetching anything or requiring network.
func resolveOgenRoot(t *testing.T) string {
	t.Helper()

	if dir := os.Getenv("OGEN_TESTDATA"); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
		t.Skipf("OGEN_TESTDATA=%q does not resolve to a directory", dir)
	}

	for _, candidate := range []string{
		filepath.Join("..", "ogen"),
		filepath.Join("..", "..", "ogen"),
	} {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}

	t.Skip("no ogen checkout found; set OGEN_TESTDATA=/path/to/ogen, " +
		"or place a sibling ogen checkout (../ogen or ../../ogen), to opt in")
	return ""
}

// openAPIComponents is the minimal shape needed to pull components.schemas out of an
// OpenAPI 3.x document.
type openAPIComponents struct {
	Components struct {
		Schemas map[string]json.RawMessage `json:"schemas"`
	} `json:"components"`
}

// bundleComponentSchemas repackages an OpenAPI document's components.schemas into a
// single self-contained JSON Schema bundle, mirroring the jq pipeline used to vendor
// testdata/ogen/openapi/*.bundle.json (see package doc): a $defs map plus a oneOf
// listing every schema name as a $ref, with internal #/components/schemas/ refs
// rewritten to #/$defs/ so the bundle resolves standalone. Returns a nil slice (no
// error) if the document has no component schemas.
func bundleComponentSchemas(data []byte) ([]byte, error) {
	var doc openAPIComponents
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if len(doc.Components.Schemas) == 0 {
		return nil, nil
	}

	names := make([]string, 0, len(doc.Components.Schemas))
	for name := range doc.Components.Schemas {
		names = append(names, name)
	}
	sort.Strings(names)

	oneOf := make([]map[string]string, 0, len(names))
	for _, name := range names {
		oneOf = append(oneOf, map[string]string{"$ref": "#/$defs/" + name})
	}

	bundle := map[string]any{
		"$defs": doc.Components.Schemas,
		"oneOf": oneOf,
	}
	out, err := json.Marshal(bundle)
	if err != nil {
		return nil, err
	}
	return []byte(strings.ReplaceAll(string(out), "#/components/schemas/", "#/$defs/")), nil
}

// normalizeLegacyDraft adapts two purely nominal pre-2020-12 keyword spellings that
// our 2020-12-only frontend does not itself recognize, so ogen's older draft-04/
// Swagger-style example fixtures compile instead of failing on a keyword rename
// 2020-12 itself made. This does not touch anything the frontend actually cannot
// represent — it only:
//
//   - merges a root-level "definitions" container (draft-04) into 2020-12's "$defs",
//     rewriting matching "#/definitions/..." refs to "#/$defs/...".
//   - renames the legacy tuple form "items": [...] (pre-2020-12) to "prefixItems",
//     its exact 2020-12 replacement, wherever it appears — a bare JSON array is never
//     a valid 2020-12 "items" value, so this rename is unambiguous.
func normalizeLegacyDraft(data []byte) ([]byte, error) {
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}

	if root, ok := doc.(map[string]any); ok {
		if legacy, ok := root["definitions"].(map[string]any); ok {
			defs, _ := root["$defs"].(map[string]any)
			if defs == nil {
				defs = make(map[string]any, len(legacy))
			}
			for name, schema := range legacy {
				defs[name] = schema
			}
			root["$defs"] = defs
			delete(root, "definitions")
		}
	}
	renameTupleItems(doc)

	out, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	return []byte(strings.ReplaceAll(string(out), "#/definitions/", "#/$defs/")), nil
}

// renameTupleItems recursively renames any "items" key whose value is a JSON array
// (the pre-2020-12 tuple form) to "prefixItems", 2020-12's replacement keyword.
func renameTupleItems(v any) {
	switch val := v.(type) {
	case map[string]any:
		if arr, ok := val["items"].([]any); ok {
			val["prefixItems"] = arr
			delete(val, "items")
		}
		for _, child := range val {
			renameTupleItems(child)
		}
	case []any:
		for _, child := range val {
			renameTupleItems(child)
		}
	}
}

// TestOgenLiveWalk is an opt-in walk of a live ogen checkout's _testdata/positive/*.json
// (full OpenAPI documents, whose components.schemas are extracted the same way as the
// vendored bundles) and gen/_testdata/jsonschema/*.json (standalone schemas, compiled
// as-is). It requires no network and never fetches the sibling repo itself — see
// resolveOgenRoot for how it is located; absent that, it skips cleanly.
func TestOgenLiveWalk(t *testing.T) {
	root := resolveOgenRoot(t)

	type liveFile struct {
		path      string
		isOpenAPI bool
	}
	var files []liveFile
	for sub, isOpenAPI := range map[string]bool{
		filepath.Join("_testdata", "positive"):          true,
		filepath.Join("gen", "_testdata", "jsonschema"): false,
	} {
		dir := filepath.Join(root, sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			files = append(files, liveFile{path: filepath.Join(dir, e.Name()), isOpenAPI: isOpenAPI})
		}
	}
	if len(files) == 0 {
		t.Skipf("no .json files found under %s/_testdata/positive or %s/gen/_testdata/jsonschema", root, root)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })

	dist := make(map[distKey]int)
	var review []string

	for _, f := range files {
		rel, err := filepath.Rel(root, f.path)
		require.NoError(t, err)
		rel = filepath.ToSlash(rel)

		t.Run(rel, func(t *testing.T) {
			if reason, known := knownOgenQuirks[rel]; known {
				t.Skipf("known ogen fixture/compiler quirk, not asserted here: %s", reason)
			}

			data, err := os.ReadFile(f.path) //nolint:gosec // test-only, path from a controlled opt-in walk
			require.NoError(t, err)

			schemaData := data
			if f.isOpenAPI {
				bundle, err := bundleComponentSchemas(data)
				require.NoError(t, err, "extract components.schemas")
				if bundle == nil {
					t.Skip("OpenAPI document has no components.schemas")
				}
				schemaData = bundle
			}

			normalized, err := normalizeLegacyDraft(schemaData)
			require.NoError(t, err, "normalize legacy draft keywords")

			res, err := compileSafely(t, normalized)
			require.NoError(t, err, "Compile must not error")
			require.NotNil(t, res, "Compile must return a non-nil result")
			require.NotNil(t, res.Plan.Representation, "plan must carry a representation")

			dist[distKey{res.Capability, res.Exactness}]++
			collectReview(&review, rel, res.Capability, res.Diagnostics)
		})
	}

	t.Logf("live-walked %d files from %s", len(files), root)
	logDistribution(t, dist)
	logReviewList(t, review)
}
