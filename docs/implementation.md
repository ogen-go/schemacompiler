# schemacompiler — implementation plan

Compiles JSON Schema **Draft 2020-12** into an analyzed **CompilationPlan** that a
code generator (ogen) lowers into Go types, decoders, and validators.

This document is the contract for implementation. The full design rationale lives in
[`_ref/json-schema-to-go-design.md`](../_ref/json-schema-to-go-design.md); section
numbers below (§) refer to it. Read it before touching a phase.

## Scope

schemacompiler is a **frontend / analysis library**. It stops at the analyzed plan:

```
load + resolve ──► semantic IR ──► normalize ──► plans + classify ──► Result
   (frontend)        (ir)           (norm)          (planner)
```

**Go code generation is out of scope** — ogen owns lowering. We faithfully implement
the design's own IR (do not bend it to ogen's current `gen/ir`; ogen reworks its
generator to consume `plan.CompilationPlan`). §5 in this doc's Phase 5 documents the
`plan → ogen gen/ir` mapping.

## Parser: libopenapi

Parsing uses `github.com/pb33f/libopenapi` (`datamodel/high/base.Schema`), chosen to
**join with ogen's future parser** — ogen is migrating to libopenapi, and each OpenAPI
3.1 component schema already *is* a `base.Schema`, so ogen feeds schemacompiler with no
re-parse.

**The `frontend` adapter is the only package allowed to import libopenapi.** It converts
`base.Schema` → our presence-normalized internal AST. This isolates libopenapi's v0.x
churn and keeps `ir` / `norm` / `plan` hermetically testable.

Known libopenapi gaps we cover ourselves:
- `$dynamicRef` / `$dynamicAnchor` are stored but **not resolved** — frontend implements
  dynamic-scope resolution (§10.2).
- `NewDocument` is OpenAPI-only — frontend has a **standalone loader shim**
  (`yaml.Node → lowbase.Build → base.NewSchema`) for bare schema files and the
  conformance suite.

## v1 scope: classify the hard tail as Unsupported

For v1 the following constructs are **not** fully compiled. The planner classifies them
at their capability level and emits a `SeverityError` (or `SeverityWarning`) diagnostic
explaining why, rather than attempting a runtime engine. The generator (ogen) is expected
to refuse to generate for these, which is acceptable:

- **`$dynamicRef` / `$dynamicAnchor`** → `DynamicSchemaResolution`, Unsupported. The
  frontend still records the dynamic graph; the planner stops there with a diagnostic.
- **`unevaluatedProperties` / `unevaluatedItems`** → `EvaluationStateValidation`,
  Unsupported (no evaluated-annotation tracking engine in v1). §14 machinery is stubbed.
- **`contains` / `minContains` / `maxContains` match-counting** and **overlapping
  `oneOf`/`anyOf` requiring predicate-count dispatch** → keep the plan
  (`PredicateCountDispatch`) but classify as `PredicateDispatch`; a diagnostic notes the
  runtime match-count cost. (These are representable, just flagged — do not drop them.)

Everything that normalizes to `DirectGoType`, `GoTypeWithValidation`, or `StaticDispatch`
is fully supported and is the primary target. The point of the capability ladder is to
draw this line explicitly and soundly, never to emit a wrong narrow type (invariant 4).

## Core invariants (do not violate)

1. **Type-specific keywords are kind-guarded predicates, not type assertions** (§3).
   `{"minLength": 5}` accepts every non-string value. `properties` alone does not make an
   object type. Guards are only dropped when an enclosing kind restriction makes them
   redundant.
2. **`oneOf` is `ExactlyOne`, not union**, until normalization *proves* disjointness (§5,
   §11.7, §15.3). Never flatten eagerly.
3. **Presence, nullability, and value are independent** (§7.1): absent ≠ present-null ≠
   present-value. The semantic IR preserves all three even if a backend collapses them.
4. **Soundness** (§24): the Go representation may *over*-approximate (extra values caught
   by residual validation) but must **never under-approximate** — it must be able to hold
   every valid instance. When exactness is impossible, emit a broad representation + exact
   residual validator, never a narrow wrong type.
5. **Normalization owns optimization** (§23), not the backend. The backend receives an
   already-analyzed plan.

## Package layout

```
schemacompiler            root: Compile(ctx, ...) (*Result, error), Options, Result   [public]
  plan/                   output IR consumed by ogen: CompilationPlan, Representation, [public]
                          ValidationPlan, DispatchPlan, ResolutionPlan, Capability,
                          Exactness, Diagnostic
  internal/
    frontend/             libopenapi adapter, loader shim, resolver, ref-graph SCC
    ir/                   semantic Expr + KindSet + NumericDomain + semantic compile
    norm/                 normalization loop over ir.Expr
    planner/              ir.Expr → plan.CompilationPlan + classify
  conformance/            JSON-Schema-Test-Suite harness (test-only)
```

Only `plan` and the root package are importable by ogen. Everything analytical is
`internal/`. The `plan` types are pure data (no libopenapi, no ir imports).

## Conventions (match ogen)

- Errors: `github.com/go-faster/errors` (`errors.Wrap`/`Wrapf`). Never
  `return errors.Wrap(f(), msg)` — guard `if err := f(); err != nil` first.
- Tests: `stretchr/testify/require`; table tests for small funcs; **golden files via
  `go-faster/sdk/gold`** for IR/plan snapshots; fuzzers seeded from the test corpus for
  the frontend loader/resolver.
- Formatting: `golangci-lint fmt ./...` (or `goimports -local github.com/ogen-go/schemacompiler`).
- File structure: split logical sections into separate files, not `//`-comment dividers.
- Godoc short; ref types with `[TypeName]`.

## Phases

| # | Package | Deliverable |
|---|---------|-------------|
| 0 | scaffolding | this doc + compiling contract stubs (main) |
| 1 | `frontend` | libopenapi adapter, standalone loader, own `$ref`/`$id`/`$anchor`/JSON-Pointer + `$dynamicRef` resolution, resource registry, ref-graph SCC → guarded/unguarded recursion (§19) |
| 2 | `ir` | `Expr`, `KindSet`, `NumericDomain`, `Compile(schema) Expr` — keyword→Expr as guarded predicates (§3, §6, §11–14) |
| 3 | `norm` | rewrite/subsumption/disjointness/constraint-push/discriminator loop with expansion budget (§15–18) |
| 4 | `planner` + `plan` | Representation/Validation/Dispatch/Resolution inference + recursive classify → Capability + Exactness + Diagnostics (§7–10, §22, §24, §25) |
| 5 | `conformance` | JSON-Schema-Test-Suite 2020-12 harness (no silent caps — log skips), e2e goldens, `plan → ogen gen/ir` integration notes |

Each phase must land with tests green and `golangci-lint run` clean before the next
starts. Phases 2–4 depend only on the internal AST from Phase 1, so their logic can be
built against hand-written AST fixtures in parallel with Phase 1 hardening.
