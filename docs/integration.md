# Integration: how ogen consumes a `plan.CompilationPlan`

This documents the mapping from schemacompiler's `plan.CompilationPlan` (design §4-10,
§18, §22, §25) onto ogen's existing code-generation IR under `gen/ir` (module
`github.com/ogen-go/ogen`, checked out separately). Every claim below was verified
against the ogen source; citations are `file:line`.

schemacompiler stops at the analyzed plan (docs/implementation.md's "Scope"); ogen owns
lowering the plan into Go source. **ogen's generator does not consume `plan` today** —
it currently consumes `*jsonschema.Schema` via `gen.GenerateSchema` (`gen/gen_schema.go:134`,
`GenerateSchemaOptions` at `gen/gen_schema.go:98-110`; both spec-driven and JSON-infer
entry points in `cmd/jschemagen/main.go:165` and `:276` call it after parsing with
`jsonschema.NewParser(...).Parse(...)`). Adopting `plan.CompilationPlan` as the input
means reworking that front half of the generator: the `gen/ir` output types below are
unaffected, only what feeds them changes.

## 1. Representation → `ir.Type`

`gen/ir/type.go:13-28` discriminates `ir.Type` by `ir.Kind`: `KindPrimitive, KindArray,
KindMap, KindAlias, KindConst, KindEnum, KindStruct, KindPointer, KindInterface,
KindGeneric, KindSum, KindAny, KindStream`.

| `plan.Representation` | `ir.Kind` | Notes |
|---|---|---|
| `AnyRepresentation` | `KindAny` | Backend's "unknown JSON value" (`json.RawMessage`-like). |
| `NeverRepresentation` | — | No instance is ever valid; ogen has no direct analog. The generator should refuse (emit a diagnostic) rather than invent an uninhabited type, unless the containing context (e.g. an unreachable union branch) can simply omit it. |
| `PrimitiveRepresentation{Kind, Numeric}` | `KindPrimitive` | `Numeric == IntegerOnly` selects an integer Go type (`int64`/`int32`); `NonIntegerOnly`/`AnyNumber` select `float64` (or ogen's decimal type, see `Validators.Decimal` below, when a decimal-precision format is present). |
| `ObjectRepresentation{Fields, Additional, PatternRules}` | `KindStruct` (+ `KindMap` when `Additional`/`PatternRules` dominate and there are no named `Fields`) | See §2 for `FieldRepresentation` → field generics. `PatternRules` has no first-class ogen construct today; the generator would need a custom map-with-pattern-validation field, or fall back to `KindMap` plus a residual `PatternPredicate` in `ValidationPlan` (soundness-preserving over-approximation, design §24). |
| `ArrayRepresentation{Prefix, Rest}` | `KindArray` when `Prefix` is empty; a tuple-as-struct (`KindStruct` with positional fields) when `Prefix` is non-empty, following ogen's existing `prefixItems` tuple lowering | `Rest == nil` (no additional items) has no first-class ogen fixed-length-array kind; treat as a tuple struct with a validated length instead of relying on a fixed-size Go array. |
| `UnionRepresentation{Alternatives}` | `KindSum` | Paired with a `plan.DispatchPlan` (see §3) to fill `SumSpec`. |
| `RecursiveRepresentation{Name, Body}` | `KindPointer` wrapping the named type, or `KindStruct` with a named self-reference resolved through ogen's existing "generate the type once, reference it" pass | Corresponds to design §19's guarded recursion; ogen already generates self-referential structs for JSON Schema `$ref` cycles through object/array descent, so this is compatible in spirit, but the compile-time proof of guardedness now comes from schemacompiler (`internal/frontend`'s SCC classification) rather than ogen's own ref-graph walk. |
| `ReferenceRepresentation{Name}` | `KindAlias` (or a direct reference to the already-generated named type) | Requires the referenced name to have already been lowered; see the "known v1 limitation" in §6 for why whole-document `$ref` assembly isn't wired yet in the root pipeline. |

## 2. Field presence/nullability → `GenericVariant`/`NilSemantic`

`gen/ir/generics.go:5-8`: `type GenericVariant struct { Nullable bool; Optional bool }`,
with `Name()` building the `Opt`/`Nil`/`OptNil` wrapper type name. `gen/ir/nil_semantic.go:4-10`:
`type NilSemantic string` with constants `NilInvalid`, `NilOptional`, `NilNull`, attached
to pointer (`KindPointer`) types.

`plan.FieldRepresentation{Presence, Nullable}` (design §7.1: presence and nullability are
independent) maps directly:

| `Presence` | `Nullable` | `GenericVariant` | `NilSemantic` (if `KindPointer` is used instead of a wrapper) |
|---|---|---|---|
| `PresenceRequired` | `false` | `{false, false}` (plain type) | n/a |
| `PresenceRequired` | `true` | `{Nullable: true, Optional: false}` → `Nil` wrapper | `NilNull` |
| `PresenceOptional` | `false` | `{Nullable: false, Optional: true}` → `Opt` wrapper | `NilOptional` |
| `PresenceOptional` | `true` | `{Nullable: true, Optional: true}` → `OptNil` wrapper | `NilOptional` used together with an explicit null sentinel, since one `NilSemantic` value cannot carry both absent and null; ogen currently disambiguates via the `Opt`+pointer combination rather than a single `NilSemantic`, so the three-state case needs `OptNil[T]` (or `Opt[Nilable[T]]`), not bare `NilSemantic` alone. |

This is the one place where ogen's existing two-axis model (`GenericVariant` for
struct-field wrapping, `NilSemantic` for standalone pointer fields) needs to fully absorb
schemacompiler's three explicit states (design §7.1: absent / present-null /
present-value) — today `NilSemantic` alone conflates "optional" and "nullable" into one
enum for bare pointer fields, so the generator should prefer the `GenericVariant`
wrapper path for any field where `Presence` and `Nullable` are asserted independently.

## 3. Dispatch → `SumSpec`

`gen/ir/type.go:56-85`:

```go
type SumSpec struct {
    Unique              []*Field
    DefaultMapping      string
    Discriminator       string
    Mapping             []SumSpecMap
    TypeDiscriminator   bool
    UniqueFieldTypes    map[string]string
    UniqueFields        map[string][]UniqueFieldVariant
    ValueDiscriminators map[string]ValueDiscriminator
}
```

Detection/preference order in `gen/schema_gen_sum.go`: explicit `discriminator` keyword
(`handleExplicitDiscriminator`, ~line 327) → implicit shared-property discriminator
(`implicitDiscriminatorKey`, ~line 446) → `TypeDiscriminator` via distinct JSON kind
(`canUseTypeDiscriminator`, ~line 168) → unique-fields/value-discrimination fallback
(`canUseValueDiscrimination`, ~line 939).

| `plan.DispatchPlan` | `SumSpec` strategy | Notes |
|---|---|---|
| `NoDispatch` | n/a | Single representation, no `SumSpec` needed. |
| `KindDispatch{Cases}` | `TypeDiscriminator = true` | One case per JSON kind (design §18.1); maps onto ogen's kind-based sum discrimination directly. |
| `LiteralDispatch{Cases}` | `ValueDiscriminators` | Enum/const union (design §18, discriminator class 2); each `LiteralCase.Value` becomes one entry in `ValueToVariant`. |
| `PropertyDispatch{Property, Cases}` | `Discriminator = Property`, `Mapping` built from `Cases` | Tagged union (design §18.2); this is ogen's explicit/implicit discriminator path. |
| `PresenceDispatch{Property, Present, Absent}` | `UniqueFields` (or a bespoke two-branch encoding) | `dependentSchemas`-shaped presence dispatch (design §12.7) has no exact ogen precedent (ogen's `UniqueFields` targets "which required field is present" disambiguation among ≥2 object variants, not a binary present/absent split against one schema); the generator should model this as a 2-case `UniqueFields` sum where one branch's unique field set is empty. |
| `PredicateCountDispatch{Branches, Minimum, Maximum}` | **not representable in `SumSpec` today** | No ogen construct evaluates every branch and counts matches at runtime; static dispatch strategies all assume exactly one statically-determined branch wins. Follow the **PredicateDispatch lowering contract** below: emit the runtime match-count, or refuse and surface the plan's `SeverityWarning` diagnostic. Do not approximate it with a lossy `SumSpec` encoding. |

### PredicateDispatch lowering contract (runtime match-count)

`PredicateCountDispatch` (overlapping `oneOf`/`anyOf`) and `ContainsCountPredicate`
(`contains`/`minContains`/`maxContains`, §4) are the two `PredicateDispatch`-level
constructs. Both are **representable** — the plan is emitted, never dropped — but neither
has a static discriminator. A conforming backend has exactly two options for each: emit the
runtime match-count described here, or refuse the schema and surface the plan's diagnostic.
Silently narrowing to a static discriminator, or dropping the constraint, is unsound and
not permitted (the "no silent caps" rule).

**`PredicateCountDispatch{Branches, Minimum, Maximum}`.** Decode the instance into the
enclosing `UnionRepresentation` over `Branches` (the sound over-approximation, §1). Then run
each branch's full `CompilationPlan` — representation decode **and** residual `Validation` —
against the instance and record whether it accepts. Let `k` be the number of accepting
branches; the instance is valid iff `Minimum <= k <= Maximum`. `oneOf` yields
`Minimum == Maximum == 1` (exactly one branch); `anyOf` yields `Minimum == 1`,
`Maximum == len(Branches)` (at least one). Every branch must be evaluated — the branches
overlap by construction, so no branch may be skipped on a static guess. For `oneOf` exactly
one branch accepts, so its representation is the value's authoritative concrete shape. This
generalizes design §20.6 beyond its `oneOf` `!= 1` sketch:

```go
matches := 0
for _, validate := range branchValidators {
    if validate(raw) == nil {
        matches++
    }
}
if matches < minimum || matches > maximum {
    return ErrPredicateCount // oneOf: matches != 1; anyOf: matches < 1
}
```

**`ContainsCountPredicate{Schema, Min, Max}`.** For an array instance, run `Schema` (a full
`CompilationPlan`) against every element and count the elements that accept. Let `n` be that
count; the instance is valid iff `Min <= n <= Max` (`Max == nil` ⇒ no upper bound). `Min`
already incorporates the `minContains` default of 1. This is the element-wise counterpart
of the branch match-count above, and the same "emit or refuse" rule applies.

**Representation.** In both cases the accepted value is stored via the plan's
`Representation` (a `UnionRepresentation` for dispatch; the array's own representation for
`contains`). The match-count is a validation step layered on an already-decoded value — it
accepts or rejects, it does not change the stored shape.

## 4. Validation → `ir.Validators`

`gen/ir/validation.go:19-27`:

```go
type Validators struct {
    String  validate.String
    Int     validate.Int
    Float   validate.Float
    Decimal validate.Decimal
    Array   validate.Array
    Object  validate.Object
    Ogen    map[string]any
}
```

Each `plan.GuardedPredicate{Applicability, Expression}` lowers to the matching
`validate.*` field, gated by `Applicability` (a `plan.KindSet`) exactly the way ogen
already gates validators by the field's static Go type — the kind guard becomes
redundant once the representation is chosen, since a Go `string` field can only ever
carry `plan.SetString`-applicable predicates. `plan.PredicateExpr` variant → target:

| `PredicateExpr` | Target |
|---|---|
| `MinLengthPredicate`, `MaxLengthPredicate`, `PatternPredicate`, `FormatPredicate` | `Validators.String` |
| `MinimumPredicate`, `MaximumPredicate`, `MultipleOfPredicate` | `Validators.Int` or `Validators.Float` (per `PrimitiveRepresentation.Numeric`) |
| `MinItemsPredicate`, `MaxItemsPredicate`, `UniqueItemsPredicate` | `Validators.Array` |
| `ContainsCountPredicate` | No direct `Validators.Array` field for match-counting; needs custom generated code (or `Validators.Ogen` custom-param escape hatch) per the **PredicateDispatch lowering contract** in §3. This predicate always also forces `CapabilityLevel.PredicateDispatch` (design's v1 scope), so it arrives already flagged. |
| `RequiredPredicate`, `MinPropertiesPredicate`, `MaxPropertiesPredicate`, `DependentRequiredPredicate`, `PropertyNamesPredicate` | `Validators.Object` (or, for `PropertyNamesPredicate`, a per-key loop calling the nested plan's own validator — no existing single `validate.Object` field covers it, likely another `Ogen` custom-param case) |

`Validators.Decimal` has no `plan.PredicateExpr` counterpart today; it would only come
into play if a future `format` value (e.g. an arbitrary-precision decimal) is recognized
as its own numeric domain, which v1 does not attempt (`plan.NumericDomain` only
distinguishes `AnyNumber`/`IntegerOnly`/`NonIntegerOnly`).

## 5. Resolution → generator behavior

| `plan.ResolutionPlan` | Generator behavior |
|---|---|
| `FullyResolved` | Normal lowering, no residual reference machinery. |
| `StaticReferenceGraph{Definitions}` | Each `SchemaID → CompilationPlan` entry becomes one named type generated once and referenced elsewhere (`ReferenceRepresentation`/`KindAlias`, §1), matching ogen's existing "one Go type per resolved schema" pass. |
| `DynamicReferenceGraph{StaticDefinitions, DynamicAnchors}` | **Not representable.** `$dynamicRef` resolution depends on the runtime dynamic-scope stack (design §10.2, §19); ogen has no runtime schema-resolution engine (`gen` never references `unevaluatedProperties` or `dynamicRef` — confirmed by source search) and no typed error exists for it yet. The generator must refuse and surface the plan's diagnostic, following the same clean-failure pattern ogen already uses for other unsupported constructs (`ErrNotImplemented` in `gen/schema_gen_sum.go:341`, `gen/gen_security.go:111`; `ErrUnsupportedContentTypes` in `gen/errors.go:60,133`) rather than attempting a partial/unsound lowering. |

## 6. Capability gate

The generator should switch on `plan.CompilationPlan.Capability` before attempting to
lower anything, and refuse — surfacing `Result.Diagnostics` to the user — for anything
past `PredicateDispatch`:

| `CapabilityLevel` | ogen generation | Rationale |
|---|---|---|
| `DirectGoType` | **Yes** | Plain `ir.Type`, no validator. |
| `GoTypeWithValidation` | **Yes** | `ir.Type` + `ir.Validators`. |
| `StaticDispatch` | **Yes** | `ir.Type{Kind: KindSum}` with a `SumSpec` strategy from §3 (`TypeDiscriminator`/`ValueDiscriminators`/`Discriminator`+`Mapping`). |
| `PredicateDispatch` | **Partial** | Representable as a sound over-approximation (design §24: the union of all branches, validated by re-running every branch's checks at decode time and counting matches) — see the **PredicateDispatch lowering contract** in §3 for the exact match-count algorithm. ogen has no existing `SumSpec` shape for "runtime match-count over N branches," so until that lowering is built, treat as refuse-with-diagnostic; once built, it is a legitimate (if slower) generation target — the plan is not dropped, per the "no silent caps" rule. |
| `EvaluationStateValidation` | **No — refuse** | No evaluated-annotation tracking in ogen (confirmed: no `unevaluatedProperties`/`dynamicRef` references in `gen/`). Surface the plan's `SeverityError` diagnostic. |
| `DynamicSchemaResolution` | **No — refuse** | Same: no dynamic-scope resolution engine exists or is planned for v1. |
| `Unsupported` | **No — refuse** | No sound conversion exists at all (e.g. an unguarded reference cycle, design §19); always carries a `SeverityError` diagnostic explaining why. |

## Known v1 limitation: whole-document `$ref` assembly is not wired in the root pipeline

`schemacompiler.Compile` (`schemacompiler.go:52-72`) calls `planner.Build(expr,
schema.Registry)` for the **root schema only**. `plan.StaticReferenceGraph.Definitions`
and cross-resource `RecursiveRepresentation` assembly depend on the planner being able
to look up other schema resources by `plan.SchemaID` (the frontend's resolved node
pointer) — but `internal/frontend.Registry` currently exposes no public "get Node by
SchemaID" accessor (it only exposes `SCCs()` for recursion classification, consumed in
`internal/planner/planner.go:49-59`). In practice this means: a single self-contained
schema (using only internal `$defs`/`$ref`, as in every `ref/*.json` corpus entry under
`conformance/testdata/corpus`) compiles and resolves correctly end-to-end, but a
multi-document OpenAPI component set (where one component's schema `$ref`s a sibling
top-level component) has no root-pipeline path today for the planner to reach that
sibling's plan and populate `Definitions`/`RecursiveRepresentation` across resource
boundaries.

This is a **follow-up**, not a Phase 5 blocker: ogen's own use case (feeding one
`base.Schema` per OpenAPI component into a per-component `Compile`-like call, per this
doc's design notes on libopenapi joining) works within today's scope, but true
whole-document generation — resolving `$ref`s across sibling components at the plan
level rather than one component at a time — needs `frontend.Registry` extended with a
public schema-by-`SchemaID` lookup, and `schemacompiler.Compile` (or a new multi-schema
entry point) threading that lookup through to the planner.
