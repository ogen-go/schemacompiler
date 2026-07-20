# schemacompiler [![Go Reference](https://img.shields.io/badge/go-pkg-00ADD8)](https://pkg.go.dev/github.com/ogen-go/schemacompiler#section-documentation) [![codecov](https://img.shields.io/codecov/c/github/ogen-go/schemacompiler?label=cover)](https://codecov.io/gh/ogen-go/schemacompiler) [![alpha](https://img.shields.io/badge/-alpha-orange)](https://github.com/ogen-go/schemacompiler)

Compiles JSON Schema **Draft 2020-12** into an analyzed plan that a code generator
(such as [ogen](https://github.com/ogen-go/ogen)) lowers into Go types, decoders, and
validators.

> **Status: alpha.** The API is unstable and the compiler targets a well-defined subset;
> unsupported constructs are reported as diagnostics rather than mis-compiled.

## What it does

JSON Schema is a *predicate* language, not a type language: many schemas describe
constraints an ordinary Go type cannot express, while others collapse into a direct type
or a finite static dispatch. schemacompiler separates the four concerns and produces an
analyzed [`plan.CompilationPlan`](./plan) rather than emitting Go directly:

- **Representation** — the Go data shape (`string`, `struct`, union, …).
- **Validation** — residual, kind-guarded predicates the type cannot enforce.
- **Dispatch** — how a value selects among known alternatives (kind / literal / tagged
  property / match-count).
- **Resolution** — how `$ref` / `$dynamicRef` targets are resolved.

Each plan is classified on a capability ladder, so a backend knows exactly how far a
schema can be lowered soundly.

| Capability | Meaning |
|---|---|
| `DirectGoType` | a plain Go type captures the value; no validator needed |
| `GoTypeWithValidation` | known type plus residual predicates |
| `StaticDispatch` | finite alternatives, discriminated at compile time |
| `PredicateDispatch` | alternatives need predicate / match-count evaluation |
| `EvaluationStateValidation` | needs `unevaluatedProperties` / `unevaluatedItems` tracking |
| `DynamicSchemaResolution` | target depends on runtime `$dynamicRef` scope |
| `Unsupported` | no sound conversion |

The compiler never generates an under-approximate type: when an exact representation is
impossible it widens to a broad type plus an exact residual validator, and records a
diagnostic explaining why.

## Usage

```go
res, err := schemacompiler.Compile(ctx, schemaBytes, schemacompiler.Options{})
if err != nil {
    // only malformed input fails; a dangling $ref is a diagnostic, not an error
}
fmt.Println(res.Capability, res.Exactness)
for _, d := range res.Diagnostics {
    fmt.Println(d.Severity, d.Pointer, d.Message)
}
```

The parser is [libopenapi](https://github.com/pb33f/libopenapi), isolated behind an
internal frontend adapter, so an OpenAPI 3.1 schema already parsed by ogen can be fed in
directly without re-parsing.

## Debugging: `cmd/schemac`

Inspect each stage of the pipeline for a schema:

```bash
go run ./cmd/schemac -all schema.json                     # IR, normalized IR, plan, graphs
go run ./cmd/schemac -graph schema.json | dot -Tsvg > refs.svg    # reference graph
go run ./cmd/schemac -plangraph schema.json | dot -Tsvg > plan.svg # dispatch tree
```

- `-graph` renders the **reference graph** (solid = instance descent, dashed = logical
  edge; recursive components colored green = guarded, red = unguarded).
- `-plangraph` renders the **dispatch tree** (how a value routes through kind/literal/
  property dispatch to representations).

## Architecture

```
load + resolve ──► semantic IR ──► normalize ──► plans + classify ──► Result
   (frontend)        (ir)           (norm)          (planner)
```

See [`docs/implementation.md`](./docs/implementation.md) for the design and invariants,
[`docs/integration.md`](./docs/integration.md) for how a generator consumes the plan, and
the reference design in [`_ref/`](./_ref).

## License

Apache-2.0 — see [LICENSE](./LICENSE).
