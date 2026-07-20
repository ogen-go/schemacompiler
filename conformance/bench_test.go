// bench_test.go is a memory-footprint / OOM guard: it embeds one medium-sized
// vendored ogen bundle (see ogen_test.go's package doc for how testdata/ogen is
// derived) and checks that compiling it does not allocate pathologically — the
// concern is the normalization/expansion budget going quadratic, not micro-optimizing
// allocations for a single document. TestOgenGiantSpecMemory extends the same check,
// opt-in, to ogen's largest example specs without vendoring them.
package conformance

import (
	"context"
	_ "embed"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
)

//go:embed testdata/ogen/openapi/telegram_bot_api.bundle.json
var largeBundle []byte

// maxReasonableAllocBytes is a generous upper bound meant to catch pathological
// blowup (e.g. exponential combinator expansion), not to police allocation counts for
// a few-hundred-KB to few-MB document.
const maxReasonableAllocBytes = 4 << 30 // 4 GiB

// BenchmarkCompileLarge compiles a ~290KB vendored OpenAPI components.schemas bundle
// (227 schemas) repeatedly, reporting allocations so a future regression that makes
// normalization/expansion quadratic shows up as a benchstat delta.
func BenchmarkCompileLarge(b *testing.B) {
	b.ReportAllocs()
	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		res, err := schemacompiler.Compile(ctx, largeBundle, schemacompiler.Options{})
		if err != nil {
			b.Fatal(err)
		}
		if res == nil {
			b.Fatal("Compile returned a nil result with no error")
		}
	}
}

// TestCompileLargeMemory compiles the same vendored bundle once and reports the
// allocation delta via runtime.MemStats, failing only on a gross blowup.
func TestCompileLargeMemory(t *testing.T) {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	res, err := schemacompiler.Compile(context.Background(), largeBundle, schemacompiler.Options{})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, res.Plan.Representation)

	runtime.ReadMemStats(&after)
	allocDelta := after.TotalAlloc - before.TotalAlloc

	t.Logf("compiling %d bytes: TotalAlloc delta=%d bytes (%.2f MiB), HeapAlloc after=%d bytes (%.2f MiB)",
		len(largeBundle), allocDelta, float64(allocDelta)/(1<<20), after.HeapAlloc, float64(after.HeapAlloc)/(1<<20))

	require.Lessf(t, allocDelta, uint64(maxReasonableAllocBytes),
		"compiling a %d-byte document allocated %d bytes, suspiciously high (possible quadratic blowup)",
		len(largeBundle), allocDelta)
}

// TestOgenGiantSpecMemory is an opt-in test that extracts and compiles the
// components.schemas of ogen's largest example specs (api.github.com.json ~3.6MB,
// k8s.json ~4.7MB) directly from a sibling ogen checkout, logging peak memory and
// wall time. These are never vendored — too large for a hermetic corpus — so the test
// skips cleanly when no ogen checkout is available (see resolveOgenRoot in
// ogen_test.go).
func TestOgenGiantSpecMemory(t *testing.T) {
	root := resolveOgenRoot(t)

	for _, name := range []string{"api.github.com.json", "k8s.json"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(root, "_testdata", "examples", name)
			data, err := os.ReadFile(path) //nolint:gosec // test-only, opt-in path
			if err != nil {
				t.Skipf("cannot read %s: %v", path, err)
			}

			bundle, err := bundleComponentSchemas(data)
			require.NoError(t, err, "extract components.schemas")
			require.NotNil(t, bundle, "%s must have components.schemas", name)

			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			start := time.Now()

			res, err := compileSafely(t, bundle)

			elapsed := time.Since(start)
			runtime.ReadMemStats(&after)

			require.NoError(t, err, "Compile must not error on %s", name)
			require.NotNil(t, res)
			require.NotNil(t, res.Plan.Representation)

			allocDelta := after.TotalAlloc - before.TotalAlloc
			t.Logf("%s: %d bytes in, wall=%s, TotalAlloc delta=%.2f MiB, HeapAlloc after=%.2f MiB",
				name, len(bundle), elapsed, float64(allocDelta)/(1<<20), float64(after.HeapAlloc)/(1<<20))

			require.Lessf(t, allocDelta, uint64(maxReasonableAllocBytes),
				"%s allocated %d bytes compiling, suspiciously high", name, allocDelta)
		})
	}
}
