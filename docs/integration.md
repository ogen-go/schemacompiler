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
| `PrimitiveRepresentation{Kind, Numeric, Format}` | `KindPrimitive` | `Numeric == IntegerOnly` selects an integer Go type (`int64`/`int32`); `NonIntegerOnly`/`AnyNumber` select `float64`. `Format` is the raw `format` name the backend may refine that choice with: see §1.1. |
| `ObjectRepresentation{Fields, Additional, PatternRules}` | `KindStruct` (+ `KindMap` when `Additional`/`PatternRules` dominate and there are no named `Fields`) | See §2 for `FieldRepresentation` → field generics. `PatternRules` has no first-class ogen construct today; the generator would need a custom map-with-pattern-validation field, or fall back to `KindMap` plus a residual `PatternPredicate` in `ValidationPlan` (soundness-preserving over-approximation, design §24). |
| `ArrayRepresentation{Prefix, Rest}` | `KindArray` when `Prefix` is empty; a tuple-as-struct (`KindStruct` with positional fields) when `Prefix` is non-empty, following ogen's existing `prefixItems` tuple lowering | `Rest == nil` (no additional items) has no first-class ogen fixed-length-array kind; treat as a tuple struct with a validated length instead of relying on a fixed-size Go array. |
| `UnionRepresentation{Alternatives}` | `KindSum` | Paired with a `plan.DispatchPlan` (see §3) to fill `SumSpec`. |
| `RecursiveRepresentation{Name, Body}` | `KindPointer` wrapping the named type, or `KindStruct` with a named self-reference resolved through ogen's existing "generate the type once, reference it" pass | Corresponds to design §19's guarded recursion; ogen already generates self-referential structs for JSON Schema `$ref` cycles through object/array descent, so this is compatible in spirit, but the compile-time proof of guardedness now comes from schemacompiler (`internal/frontend`'s SCC classification) rather than ogen's own ref-graph walk. |
| `ReferenceRepresentation{Name}` | `KindAlias` (or a direct reference to the already-generated named type) | Requires the referenced name to have already been lowered; the referenced plan is found in `DocumentResult.Plans` (or `plan.StaticReferenceGraph.Definitions`) under the same `SchemaID`; see "Whole-document compilation". |


### 1.1. `format` → Go type

`PrimitiveRepresentation.Format` carries the `format` keyword onto the representation as
a plain name, exactly as written. The compiler assigns it no meaning: in JSON Schema
2020-12 `format` is an annotation (the standard dialect includes `format-annotation`;
assertion behavior requires opting into `format-assertion`), and design §3 lists it only
as a kind-guarded validation keyword, with no representation role anywhere in the
document. Choosing a Go type for a name is therefore a backend decision.

What a backend *may* do with the name — these are ogen's own mappings, not claims the
plan makes:

| Format name | ogen may generate |
|---|---|
| `date-time`, `date`, `time` | `time.Time` (or a bespoke bare-date/time type). |
| `uuid` | `uuid.UUID`. |
| `ipv4`, `ipv6` | `netip.Addr`. |
| `byte`, `binary` | `[]byte`. |
| `int32`, `int64`, `float`, `double` | `int32`/`int64`/`float32`/`float64`. |
| `email`, `hostname`, `uri`, `regex`, `password`, ... | `string` plus the residual validator. |
| anything else | The backend's own mapping (e.g. `x-ogen-type`), else the base primitive. |

`Format` never replaces validation: the matching `FormatPredicate` stays in
`ValidationPlan` in every case, so a backend that ignores `Format` rejects the same
instances (design §24 invariant 4). Deciding that a parse into `time.Time` subsumes that
predicate is an optimization, and optimization belongs to normalization, not the backend
(design §23) — a backend that keeps both is always correct.

Applicability is per name and follows the kind the format applies to (design §3): string
formats guard `{string}`, the OpenAPI numeric formats (`int32`, `int64`, `float`,
`double`, `decimal`) guard `{number}`. So `{"type": "number", "format": "uuid"}` carries
no format at all — the guard never fires — and `{"format": "uuid"}` constrains strings
only, leaving every non-string instance accepted.

Two limits: `Format` only lands on `PrimitiveRepresentation`, so a schema without a
`type` (which widens to `AnyRepresentation`) keeps its format in the validation plan
only; and `allOf` is an unordered intersection (design §11.5), so when composition
contributes two distinct format names neither can canonically shape the representation:
`Format` is left empty, every name stays in the validation plan, and the plan carries a
`SeverityInfo` diagnostic.

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
| `PropertyDispatch{Property, Cases, Tag}` | `Discriminator = Property`, `Mapping` built from `Cases` | Tagged union (design §18.2); this is ogen's explicit/implicit discriminator path. `TagDeclared`/`TagAsserted` mean an OpenAPI `discriminator` named the property (`handleExplicitDiscriminator`), `TagInferred` means it was recovered structurally (`implicitDiscriminatorKey`). `Tag` grades the disjointness evidence in three tiers (design §18, §15.3). `TagInferred` and `TagDeclared` are **proven**: every branch requires `Property`, pins it to a const/enum, and its cases cover every value it accepts. `TagAsserted` is **trusted, not proven**: every branch requires `Property` (OAS 3.0.3 line 2354 makes that mandatory) but leaves it unconstrained, so the `mapping` is taken as the "hint to shortcut validation and selection" OAS 3.0.3 line 2717 permits; the plan reports `SeverityInfo` and downgrades `Result.Exactness` to `SoundOverApproximation`. Lowering is identical for all three — the tier tells a backend whether decoding the selected variant is guaranteed to accept every valid instance. Declared cases come from `mapping` when it names the branch, else from the branch's own const/enum — never from the referenced component's name, which constrains nothing in the instance. A declaration that cannot drive dispatch at all — a `mapping` entry resolving to no branch, a `propertyName` some branch does not require, no value selecting some branch, or two branches sharing a value — is reported as a `SeverityWarning` diagnostic and the plan falls back to structural inference and then to `PredicateCountDispatch`, so a backend never switches on a value the author did not write. |
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

`Validators.Decimal` has no `plan.PredicateExpr` counterpart: it is selected from
`PrimitiveRepresentation.Format` instead (the `decimal` format name, §1.1), not from a
numeric domain — `plan.NumericDomain` still only distinguishes
`AnyNumber`/`IntegerOnly`/`NonIntegerOnly`.

## 5. Resolution → generator behavior

| `plan.ResolutionPlan` | Generator behavior |
|---|---|
| `FullyResolved` | Normal lowering, no residual reference machinery. Every plan `CompileDocument` returns carries this: the document's graph is `DocumentResult.Plans`, not a per-plan copy (§8). |
| `StaticReferenceGraph{Definitions}` | Each `SchemaID → CompilationPlan` entry becomes one named type generated once and referenced elsewhere (`ReferenceRepresentation`/`KindAlias`, §1), matching ogen's existing "one Go type per resolved schema" pass. `Compile`/`CompileSchema` attach it to the root plan; `CompileDocument` hands back the equivalent map as `DocumentResult.Plans` (§8). |
| `DynamicReferenceGraph{StaticDefinitions, DynamicAnchors}` | **Not representable.** `$dynamicRef` resolution depends on the runtime dynamic-scope stack (design §10.2, §19); ogen has no runtime schema-resolution engine (`gen` never references `unevaluatedProperties` or `dynamicRef` — confirmed by source search) and no typed error exists for it yet. The generator must refuse and surface the plan's diagnostic, following the same clean-failure pattern ogen already uses for other unsupported constructs (`ErrNotImplemented` in `gen/schema_gen_sum.go:341`, `gen/gen_security.go:111`; `ErrUnsupportedContentTypes` in `gen/errors.go:60,133`) rather than attempting a partial/unsound lowering. |

## 6. Capability gate

The generator should switch on `plan.CompilationPlan.Capability` before attempting to
lower anything, and refuse — surfacing `Result.Diagnostics` to the user — for anything
past `PredicateDispatch`. The gate is per plan and sound to use that way: a plan's
capability is rolled up to at least that of every plan it references (design §22), so a
generatable plan never points at a refused one. A reference that resolves to no compiled
schema at all (a dangling or unfetchable `$ref`) makes the referring plan `Unsupported`
with a `SeverityError` diagnostic, rather than leaving it optimistically generatable.

| `CapabilityLevel` | ogen generation | Rationale |
|---|---|---|
| `DirectGoType` | **Yes** | Plain `ir.Type`, no validator. |
| `GoTypeWithValidation` | **Yes** | `ir.Type` + `ir.Validators`. |
| `StaticDispatch` | **Yes** | `ir.Type{Kind: KindSum}` with a `SumSpec` strategy from §3 (`TypeDiscriminator`/`ValueDiscriminators`/`Discriminator`+`Mapping`). |
| `PredicateDispatch` | **Partial** | Representable as a sound over-approximation (design §24: the union of all branches, validated by re-running every branch's checks at decode time and counting matches) — see the **PredicateDispatch lowering contract** in §3 for the exact match-count algorithm. ogen has no existing `SumSpec` shape for "runtime match-count over N branches," so until that lowering is built, treat as refuse-with-diagnostic; once built, it is a legitimate (if slower) generation target — the plan is not dropped, per the "no silent caps" rule. |
| `EvaluationStateValidation` | **No — refuse** | No evaluated-annotation tracking in ogen (confirmed: no `unevaluatedProperties`/`dynamicRef` references in `gen/`). Surface the plan's `SeverityError` diagnostic. |
| `DynamicSchemaResolution` | **No — refuse** | Same: no dynamic-scope resolution engine exists or is planned for v1. |
| `Unsupported` | **No — refuse** | No sound conversion exists at all (e.g. an unguarded reference cycle, design §19); always carries a `SeverityError` diagnostic explaining why. |

## 7. Metadata → godoc, defaults, and extensions

`plan.Metadata` carries the non-semantic annotations of a schema; `plan.FieldRepresentation.Metadata`
carries the same for one property, so a property's own `title`/`description`/`deprecated`
survives into the generated field (ogen's `jsonschema.Property.Description` → field godoc).
`plan.PatternFieldRepresentation.Metadata` does the same per `patternProperties` entry and
`plan.ItemRepresentation.Metadata` per `prefixItems` position and per `items` schema.

Annotations are attached by `internal/planner` while it builds the representation, not by a
second walk over the finished plan, so they survive every shape the planner emits — including
the `UnionRepresentation` produced by `type: ["object","null"]`, `oneOf`/`anyOf`, `if`/`then`
and `dependentSchemas`.

Every value in a `plan.Metadata` is a deep copy: mutating `Extensions`, `Default` or `Examples`
on a returned plan affects nothing else.

| `plan.Metadata` | ogen |
|---|---|
| `Title`, `Description` | Type/field godoc (`jsonschema.Schema.Description`, `jsonschema.Property.Description`). |
| `Deprecated`, `ReadOnly`, `WriteOnly` | Deprecation note in godoc; read/write-only field filtering. |
| `Default`, `Examples` | Raw JSON exactly as written in the source document (no re-encoding), for `jsonschema.Schema.Default` and example generation. |
| `XML` | `jsonschema.Schema.XML` equivalent (name, namespace, prefix, attribute, wrapped). |
| `Extensions` | Every `x-*` keyword, decoded to Go-native values (`map[string]any`, `[]any`, scalars). |

`Extensions` is a deliberately generic passthrough: schemacompiler assigns no meaning to
any key and knows nothing about `x-ogen-*`. Interpreting `x-ogen-name`, `x-ogen-type`,
`x-ogen-properties`, `x-ogen-validate`, `x-ogen-time-format`, or `x-oapi-codegen-extra-tags`
is the generator's job — it reads them off `Metadata.Extensions` (per-property ones off the
field's `Metadata.Extensions`) and applies its own semantics, so new vendor keys need no
change here.

## 8. Whole-document compilation

`schemacompiler.CompileDocument` (`compile_document.go`) compiles an OpenAPI
`components.schemas` set as one unit: every component is converted into a single
`internal/frontend` registry under its JSON Pointer (`/components/schemas/<name>`), so
`$ref`s between siblings resolve and recursion is classified across component
boundaries. `DocumentResult.Plans` is keyed by that pointer — the same `plan.SchemaID` a
`ReferenceRepresentation.Name` carries — which gives a generator a stable type-naming key
and the graph to resolve references against.

`Plans` is the whole graph: a `ReferenceRepresentation.Name` is a key into it, and each
plan's own `Resolution` is `FullyResolved` rather than a second copy of the graph. Every
plan's `Capability` already accounts for what it references (design §22), so §6's per-plan
gate holds without falling back on `DocumentResult.Capability` — the document-wide worst
case, which would refuse a whole document over one bad component.

`schemacompiler.CompileSchema` compiles a single already-parsed `*base.SchemaProxy` (no
re-serialization); references to sibling components stay unresolved and are reported as
`SeverityError` diagnostics, so prefer `CompileDocument` when the whole set is available.
`schemacompiler.Compile` (raw bytes) keeps resolving a self-contained document's own
`$defs`/`$ref` into `plan.StaticReferenceGraph.Definitions` on the root plan.

All three honour `Options.Loader` for external `$ref` documents and `Options.BaseURI`
(`Document.BaseURI` overrides it) for the retrieval URI; a relative base URI is normalized
once on entry, so in-document references resolve against the key their targets were
registered under.
