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

## 0. What a plan accepts

The sections below map each plan variant onto ogen's IR — how to *lower* a plan. This
section states what a plan *means*: given a plan and a JSON instance, what must hold for
the instance to be valid. A decoder and a validator are both implementations of this
rule, so it is the contract both are written against.

Design §24 gives it only indirectly, as a soundness inequality over the whole conversion.
The operational reading is:

> An instance `x` satisfies a `CompilationPlan` `P` iff all three hold:
>
> - **`P.Representation` can hold `x`.** Design §24 requires the generated type to hold
>   every valid instance, so a value the representation cannot hold is thereby invalid.
> - **`P.Validation` accepts `x`**: every `GuardedPredicate` whose `Applicability`
>   contains `x`'s JSON kind accepts it. A predicate whose guard excludes the kind is
>   not applicable and does not reject (§3 of the design: a type-specific keyword does
>   not assert its type).
> - **`P.Dispatch` selects a branch whose own `CompilationPlan` accepts `x`**, by this
>   same rule, recursively.

The three are a conjunction, evaluated at the same instance node. `P.Resolution` is not a
fourth conjunct; see the reference-graph row below.

**The representation is a check, not only a storage choice.** This is the point most
easily missed when reading §1, which describes the Go type each variant becomes.
`PrimitiveRepresentation{Kind: KindString}` rejects `1`; `NeverRepresentation` rejects
everything; `ObjectRepresentation` rejects a non-object. Lowering a representation to a
Go type and letting the decoder's type mismatch be the rejection is a correct
implementation of this — but it must be a rejection, not a coercion.

### 0.1. Cases the variants do not settle on their face

| Case | Reading | Why |
|---|---|---|
| `ObjectRepresentation.Additional == nil` | **Cannot happen.** The slot is always stated. | `additionalProperties` absent is a plan over `AnyRepresentation` and `false` one over `NeverRepresentation`, so nil is not a third spelling of either. Enforced by `planwalk.NilPlanSlots` over both corpora, where all four such slots are nil zero times in 12354 occurrences. |
| `ArrayRepresentation.Rest.Plan.Representation == nil` | **Cannot happen**, same rule. | `items: false` reaches the plan as `NeverRepresentation`, which rejects every element past the prefix; `items` absent reaches it as `AnyRepresentation`. §1's "`Rest == nil` means fixed length" row described a state the planner does not produce. |
| A property matched by both a `Fields` entry and a `PatternRules` pattern | The declared **field owns** the name; the pattern rule is not additionally run against it. | The plan states one storage slot per property, and the planner has already intersected every matching pattern schema into that field's own plan (design §12.3). Running the rule again would double-apply it. |
| `PresenceDispatch` against a non-object | **Accepts.** | It comes from `dependentSchemas`, which applies to objects only (design §12.7), but a `DispatchPlan` carries no kind guard the way a `GuardedPredicate` does. A dispatch that cannot select cannot reject. |
| `KindDispatch` with no case for the instance's kind | **Rejects.** | Unlike the row above, the dispatch *can* select and declines to: the kinds in `Cases` are the kinds admitted. `Cases` is not required to cover every kind the paired representation admits. |
| A `ReferenceRepresentation` inside a nested plan | Resolve against the **root** plan's `Resolution`. | Only the root plan carries a populated `StaticReferenceGraph`; every nested plan gets `StaticReferenceGraph{}` with empty `Definitions` (§5, §9). A nested plan's `Resolution` is not a second copy of the graph, and reading it as one resolves nothing. |

`internal/planterp` is the executable form of this section: it interprets a plan as a
JSON validator and nothing else, and the conformance harness differentially tests it
against the JSON-Schema-Test-Suite. A disagreement between this section and that
interpreter is a bug in one of them, not a matter of reading.

### 0.2. What acceptance does *not* tell you

A plan accepting an instance the schema rejects is only a defect when the plan claims
nothing about it. `Diagnostic.Kind` is what says whether it does — `DiagnosticAssumed` and
`DiagnosticUnenforced` both announce that the plan admits a superset, and name the
construct responsible. Design §24 permits a superset; it never permits a plan to reject an
instance the schema accepts, whatever the plan declares. See §6 for how `Capability` and
the diagnostics gate lowering.

## 1. Representation → `ir.Type`

`gen/ir/type.go:13-28` discriminates `ir.Type` by `ir.Kind`: `KindPrimitive, KindArray,
KindMap, KindAlias, KindConst, KindEnum, KindStruct, KindPointer, KindInterface,
KindGeneric, KindSum, KindAny, KindStream`.

| `plan.Representation` | `ir.Kind` | Notes |
|---|---|---|
| `AnyRepresentation` | `KindAny` | Backend's "unknown JSON value" (`json.RawMessage`-like). |
| `NeverRepresentation` | — | No instance is ever valid; ogen has no direct analog. The generator should refuse (emit a diagnostic) rather than invent an uninhabited type, unless the containing context (e.g. an unreachable union branch) can simply omit it. |
| `PrimitiveRepresentation{Kind, Numeric, Format}` | `KindPrimitive` | `Numeric == IntegerOnly` selects an integer Go type (`int64`/`int32`); `NonIntegerOnly`/`AnyNumber` select `float64`. `Format` is the raw `format` name the backend may refine that choice with: see §1.1. |
| `ObjectRepresentation{Fields, Additional, PatternRules}` | `KindStruct` (+ `KindMap` when `Additional`/`PatternRules` dominate and there are no named `Fields`) | Each of the three slots carries a whole `CompilationPlan`, not a bare `Representation`: lower it by recursing through §1-§4 on that plan, so the sub-schema's own validation and dispatch land on the value stored there. `Fields` is an ordered `[]FieldRepresentation` carrying its own `Name` — see §2.2 for what the order means. `Additional` is a `*CompilationPlan`: nil means additional properties are not representable as a field, which is a statement about storage and never a rejection. `PatternRules` covers only the property names no `Fields` entry declares: a matching pattern's schema is already intersected into the declared field's own plan (design §12.3), so a field is lowered from its plan alone and never re-checked against `PatternRules`. `PatternRules` itself has no first-class ogen construct today; the generator would need a custom map-with-pattern-validation field, or fall back to `KindMap` plus a residual `PatternPredicate` in `ValidationPlan` (soundness-preserving over-approximation, design §24). |
| `ArrayRepresentation{Prefix, Rest}` | `KindArray` when `Prefix` is empty; a tuple-as-struct (`KindStruct` with positional fields) when `Prefix` is non-empty, following ogen's existing `prefixItems` tuple lowering | Every `ItemRepresentation` carries a whole `CompilationPlan` for the values at that position, lowered by recursing through §1-§4. A `Rest` over `NeverRepresentation` is `items: false` — a fixed-length tuple. There is no first-class ogen fixed-length-array kind, so treat it as a tuple struct with a validated length rather than a fixed-size Go array. |
| `UnionRepresentation{Alternatives}` | `KindSum` | Paired with a `plan.DispatchPlan` (see §3) to fill `SumSpec`. |
| `ReferenceRepresentation{Name}` | `KindAlias` (or a direct reference to the already-generated named type) | Requires the referenced name to have already been lowered; the referenced plan is found in `DocumentResult.Plans` (or `plan.StaticReferenceGraph.Definitions`) under the same `SchemaID`; see "Whole-document compilation". Guarded recursion (design §19) arrives here too, as a reference that closes a cycle in the graph: the guardedness proof is schemacompiler's (`internal/frontend`'s SCC classification), and an unguarded cycle never reaches lowering because the planner classifies it `Unsupported`. |


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

`plan.FieldRepresentation{Name, Plan, Presence, Nullable}` (design §7.1: presence and
nullability are independent) maps directly. `Name` is the property name and `Plan` the
field value's own plan, lowered by recursion; `Presence` and `Nullable` describe the slot
around it, and the planner strips `null` out of `Plan` when it sets `Nullable`, so the two
never overlap:

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

### 2.1. OpenAPI 3.0 `nullable` — schemacompiler is stricter than ogen

schemacompiler reads OAS 3.0.3 line 2335 literally:

> A `true` value adds "null" to the allowed type specified by the `type` keyword, **only if
> `type` is explicitly defined within the same Schema Object**. Other Schema Object
> constraints retain their defined behavior, and therefore may disallow the use of `null`
> as a value.

So `nullable: true` widens the sibling `type` keyword and nothing else. Where no `type` is
declared in the same Schema Object — `{oneOf: [...], nullable: true}`, `{$ref: X, nullable:
true}`, a bare `{nullable: true}` — there is nothing to widen, the keyword has no effect,
and the plan carries a `SeverityWarning` diagnostic naming the schema.

**ogen does not do this.** `extendInfo` sets `Nullable` from the keyword unconditionally,
`type` or not. On the same 3.0 document the two disagree: ogen makes a nullable `$ref` or
`oneOf` nullable, schemacompiler leaves it non-nullable and warns. The divergence is
deliberate — schemacompiler will not assert nullability the document does not state — and
it is one-directional: schemacompiler never admits null where ogen would not.

A generator migrating onto the plan should surface that warning to the author rather than
re-deriving nullability from the raw document. The fix is the 3.1 spelling, which both
tools read the same way and which schemacompiler treats as exactly equivalent:

```yaml
# ignored, warns
value: {$ref: "#/components/schemas/Thing", nullable: true}
# honoured
value: {type: ["object", "null"], properties: {...}}
```

### 2.2. Field order is source order — preserve it

`plan.ObjectRepresentation.Fields` is an ordered `[]FieldRepresentation`, not a map, and
its order is the order the properties were written in the source document (issue #89).
Where `allOf` branches contribute the same property, it keeps the first branch that
declared it. JSON Schema itself calls `properties` an unordered object, so this is a
fidelity guarantee rather than a semantic one — but it is the order the user sees in the
spec and expects in the generated struct, and it is what ogen's own ordered
`jsonschema.Schema.Properties` (`jsonschema/schema.go:70`) produces today.

**Backends should generate struct fields in this order and must not re-sort it.** Sorting
(alphabetically or otherwise) changes generated output for essentially every schema
relative to ogen. A backend needing per-name lookup should build one map per object rather
than scanning `Fields` per property; the ordered slice is the source of truth, and a
derived index is a local optimisation.

Iterating `Fields` is also what makes generated code reproducible: Go randomizes map
iteration, so a consumer that ranged over a keyed `Fields` emitted a different file per
run of the same generator on the same input.

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
| `PropertyDispatch{Property, Cases, Tag}` | `Discriminator = Property`, `Mapping` built from `Cases` | Tagged union (design §18.2); this is ogen's explicit/implicit discriminator path. `TagDeclared`/`TagAsserted` mean an OpenAPI `discriminator` named the property (`handleExplicitDiscriminator`), `TagInferred` means it was recovered structurally (`implicitDiscriminatorKey`). `Tag` grades the disjointness evidence in three tiers (design §18, §15.3). `TagInferred` and `TagDeclared` are **proven**: every branch requires `Property`, pins it to a const/enum, and its cases cover every value it accepts; a branch that is itself an `allOf`/`anyOf`/`oneOf` is looked through, the pinned value set being the intersection over `allOf` members and the union over alternatives (issue #45). `TagAsserted` is **trusted, not proven**: every branch requires `Property` (OAS 3.0.3 line 2354 makes that mandatory) but leaves it unconstrained, so the `mapping` is taken as the "hint to shortcut validation and selection" OAS 3.0.3 line 2717 permits; the plan reports a `SeverityInfo` diagnostic of kind `DiagnosticAssumed`. Lowering is identical for all three — the tier tells a backend whether decoding the selected variant is guaranteed to accept every valid instance. Declared cases come from `mapping` when it names the branch, else from the branch's own const/enum — never from the referenced component's name, which constrains nothing in the instance. A declaration that cannot drive dispatch at all — a `mapping` entry resolving to no branch, a `propertyName` some branch does not require, no value selecting some branch, or two branches sharing a value — is reported as a `SeverityWarning` diagnostic and the plan falls back to structural inference and then to `PredicateCountDispatch`, so a backend never switches on a value the author did not write. |
| `PresenceDispatch{Property, Present, Absent}` | `UniqueFields` (or a bespoke two-branch encoding) | `dependentSchemas`-shaped presence dispatch (design §12.7) has no exact ogen precedent (ogen's `UniqueFields` targets "which required field is present" disambiguation among ≥2 object variants, not a binary present/absent split against one schema); the generator should model this as a 2-case `UniqueFields` sum where one branch's unique field set is empty. |
| `PredicateCountDispatch{Branches, Minimum, Maximum}` | **not representable in `SumSpec` today** | No ogen construct evaluates every branch and counts matches at runtime; static dispatch strategies all assume exactly one statically-determined branch wins. Follow the **`PredicateCountDispatch` lowering contract** below: emit the runtime match-count, or refuse and surface the plan's `SeverityWarning` diagnostic. Do not approximate it with a lossy `SumSpec` encoding. |

**`discriminator` outside `oneOf`/`anyOf`.** OAS 3.0.3 line 2705 also allows the keyword
alongside `allOf`, and line 2761 lets a *parent* schema carry it while the alternatives are
every schema that includes the parent via `allOf`. Neither spelling lists the alternatives
in the schema, so neither produces a `PropertyDispatch`: the plan is `NoDispatch` and a
`SeverityWarning` diagnostic says so at the declaring schema's pointer (issue #46). A
backend must not synthesize the union itself from the plan — resolving the alternates needs
the whole document, and the parent's own schema accepts instances no child does, so a sum
type over the children would under-approximate it (design §24).

### `PredicateCountDispatch` lowering contract (runtime match-count)

`PredicateCountDispatch` (overlapping `oneOf`/`anyOf`), `ContainsCountPredicate`
(`contains`/`minContains`/`maxContains`, §4) and `NegationPredicate` (a `not` that survived
normalization, §4), together with `PropertyNamesPredicate` (§4), are what floors a plan
at `RawEvaluation`. Only the first selects a branch; the rest are validation that happens
to need the document. All are **representable** —
the plan is emitted, never dropped — but none has a static discriminator. A conforming
backend has exactly two options for each: emit the runtime check described here, or refuse
the schema and surface the plan's diagnostic. Silently narrowing to a static discriminator,
or dropping the constraint, is unsound and not permitted (the "no silent caps" rule).

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

**`NegationPredicate{Schema}`.** Run `Schema` (a full `CompilationPlan`) against the whole
instance and invert the outcome: the instance is valid iff `Schema` rejects it. Like the two
counts above it forces `CapabilityLevel.RawEvaluation`, so it arrives already flagged.

Negation inverts approximation polarity, so this predicate is emitted **only** when the
nested plan reproduces its schema exactly: a nested plan that accepts more than its schema
would make the negation reject valid instances, which §24 forbids. Where that cannot be
established the compiler drops the negation instead — the outer plan then accepts a superset
and carries a `DiagnosticUnenforced` naming the constraint it could not enforce. A backend
therefore never sees a `NegationPredicate` it must distrust, but it must not read the absence
of one as "the schema had no `not`"; the diagnostic is what says so.

`Schema` may itself be — or contain — a `ReferenceRepresentation`, which is emitted when the
target's own plan is exact (issue #108). Lower it exactly like any other reference: resolve
the name against the reference graph (§5) and invoke that plan's validator. A recursive target
is never emitted here, so the inversion cannot recurse without bound.

The polarity rule binds the backend too, and one rung lower than the plan. A backend that
cannot enforce some constraint *inside* `Schema` — an unasserted `format`, a regex its engine
does not accept — reaches an acceptance that is really an over-acceptance, and inverting it
rejects a valid instance. Such an acceptance must be reported as accepted here as well; only
an acceptance the backend actually checked may be inverted.

**Representation.** In every case the accepted value is stored via the plan's
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
| `ContainsCountPredicate` | No direct `Validators.Array` field for match-counting; needs custom generated code (or `Validators.Ogen` custom-param escape hatch) per the **`PredicateCountDispatch` lowering contract** in §3. This predicate always also forces `CapabilityLevel.RawEvaluation` (design's v1 scope), so it arrives already flagged. |
| `NegationPredicate` | No `validate.*` field: generate a call to the nested plan's own validator and invert it, per the **`PredicateCountDispatch` lowering contract** in §3. Always also forces `CapabilityLevel.RawEvaluation`. |
| `ShapePredicate` | No `validate.*` field: generate a call to the nested plan's own decoder+validator and take its verdict, gated on `Applicability` as every guarded predicate is. See §4.1. Unlike `NegationPredicate` it does **not** force `RawEvaluation`; it costs whatever `Schema.Capability` says. |
| `RequiredPredicate`, `MinPropertiesPredicate`, `MaxPropertiesPredicate`, `DependentRequiredPredicate`, `PropertyNamesPredicate` | `Validators.Object` (or, for `PropertyNamesPredicate`, a per-key loop calling the nested plan's own validator — no existing single `validate.Object` field covers it, likely another `Ogen` custom-param case) |

### 4.1. `ShapePredicate` — a shape keyword written without a sibling `type`

`properties`, `patternProperties`, `additionalProperties`, `prefixItems` and `items` do not
assert their own type (design §3). `{"properties": {"a": {"type": "string"}}}` accepts
every string, number, boolean, null and array, and only constrains an *object* instance.

So the plan keeps the representation broad — `AnyRepresentation`, per design §12.1: this
must not become a Go struct — and carries the shape as

```text
GuardedPredicate{
    Applicability: plan.SetObject,          // or SetArray
    Expression:    plan.ShapePredicate{Schema: <the object/array plan>},
}
```

`Schema` is exactly the plan the same keywords produce **with** the sibling `type`, so the
two spellings differ only in the enclosing representation and never in the constraint. A
backend that lowers `AnyRepresentation` to a `jx.Raw`/`any` slot lowers this predicate to
"if the value's JSON kind is in `Applicability`, run `Schema`'s own validator over it".

Two things a backend must not do with it:

- **Do not narrow the enclosing representation to `Schema`'s.** That is exactly the
  under-approximation design §24 forbids: a plain string is a valid instance and must still
  decode.
- **Do not drop the guard.** Applying an object shape to a non-object instance rejects a
  value the schema accepts, which is the same forbidden direction.

Sibling predicates are *not* repeated inside `Schema`. `{"properties": …, "required": ["a"]}`
carries the `RequiredPredicate` next to the `ShapePredicate`, under the same `SetObject`
guard, not inside it — so the nested `ObjectRepresentation`'s fields read
`PresenceOptional` and the presence requirement is the sibling predicate's. Enforce both.

`Validators.Decimal` has no `plan.PredicateExpr` counterpart: it is selected from
`PrimitiveRepresentation.Format` instead (the `decimal` format name, §1.1), not from a
numeric domain — `plan.NumericDomain` still only distinguishes
`AnyNumber`/`IntegerOnly`/`NonIntegerOnly`.

### 4.2. `ShapePredicate` — the conjuncts a `$ref` cannot absorb

`allOf` is an unordered intersection (design §11.5) and a `$ref` contributes a name rather
than a structure, so `{"allOf": [{"$ref": "#/$defs/A"}, X]}` cannot merge `X` into the
referenced type. The plan is `ReferenceRepresentation{A}` plus

```text
GuardedPredicate{
    Applicability: plan.SetAny,
    Expression:    plan.ShapePredicate{Schema: <the plan for X>},
}
```

with `X`'s own capability and reference graph rolled up into the enclosing plan (issue
#78). Members needing nothing but residual predicates — `{"allOf": [{"$ref": …},
{"maxLength": 3}]}` — are merged flat into the validation plan instead, so the wrapper
appears only where `X` has a representation or dispatch of its own. Both spellings are
exact: nothing is dropped, and the guard is `SetAny` because the member applies to every
kind the reference admits.

A backend that wants one merged Go struct for `allOf` composition must do that merge
itself from `Schema`; it must not simply generate the referenced type and skip the
predicate, which is the acceptance bug the wrapper exists to prevent.

## 5. Resolution → generator behavior

| `plan.ResolutionPlan` | Generator behavior |
|---|---|
| `FullyResolved` | Normal lowering, no residual reference machinery. Every plan `CompileDocument` returns carries this: the document's graph is `DocumentResult.Plans`, not a per-plan copy (§9). |
| `StaticReferenceGraph{Definitions}` | Each `SchemaID → CompilationPlan` entry becomes one named type generated once and referenced elsewhere (`ReferenceRepresentation`/`KindAlias`, §1), matching ogen's existing "one Go type per resolved schema" pass. `Compile`/`CompileSchema` attach it to the root plan; `CompileDocument` hands back the equivalent map as `DocumentResult.Plans` (§9). |
| `DynamicReferenceGraph{StaticDefinitions, DynamicAnchors}` | **Not representable.** `$dynamicRef` resolution depends on the runtime dynamic-scope stack (design §10.2, §19); ogen has no runtime schema-resolution engine (`gen` never references `unevaluatedProperties` or `dynamicRef` — confirmed by source search) and no typed error exists for it yet. The generator must refuse and surface the plan's diagnostic, following the same clean-failure pattern ogen already uses for other unsupported constructs (`ErrNotImplemented` in `gen/schema_gen_sum.go:341`, `gen/gen_security.go:111`; `ErrUnsupportedContentTypes` in `gen/errors.go:60,133`) rather than attempting a partial/unsound lowering. |

## 6. Capability gate

The generator should switch on `plan.CompilationPlan.Capability` before attempting to
lower anything, and refuse — surfacing `Result.Diagnostics` to the user — for anything
past `RawEvaluation`. The gate is per plan and sound to use that way: a plan's
capability is rolled up to at least that of every plan it references (design §22), so a
generatable plan never points at a refused one. A reference that resolves to no compiled
schema at all (a dangling or unfetchable `$ref`) makes the referring plan `Unsupported`
with a `SeverityError` diagnostic, rather than leaving it optimistically generatable — the
root schema included.

The gate is also per *plan*, not per representation node: an object field, a pattern
rule, an `Additional` value and an array item each carry their own `CompilationPlan`, and
each one's capability is rolled into the plan that contains it. So a field whose
sub-schema needs validation raises its parent to `GoTypeWithValidation`, and the
predicate that costs it sits in that field's own `ValidationPlan` — where the value it
constrains is. A backend that lowered only the root's `Validation` would emit nothing for
it (issue #68).

There is no `Exactness` field, and there deliberately is not one (design §25.1). Whether the
*generated program* reproduces the schema's accepted set depends on the lowering — integer
widths, whether unknown properties are retained, which regex engine runs — none of which the
compiler chooses, so a compiler-side exactness value is a claim it is not in a position to
make. What the compiler reports instead is two things a backend cannot derive for itself:
`Requirements` (§7), the checks the lowering must discharge, and `Diagnostic.Kind`, the
constructs it failed to enforce.

`Capability` and `Diagnostic.Kind` answer different questions — *can this be lowered at all*
and *does the plan still accept what its schema rejects* — and they are independent. A plan
the capability gate passes may carry a `DiagnosticUnenforced` at any level including
`DirectGoType` (a `not` whose operand is not exactly modeled is dropped rather than enforced,
which costs the plan nothing at runtime — issues #77, #82, #84). A backend that consults only
`Capability` therefore sees a fully generatable plan whose generated type accepts values the
schema rejects, with no residual validation to catch them. **Consult both**: `Capability`
decides whether to generate, the diagnostics decide what the generated code is worth.

| `Diagnostic.Kind` | Meaning | Backend action |
|---|---|---|
| `DiagnosticAdvisory` | A storage, dispatch or authoring note. The plan accepts exactly what the schema does. | Generate; surface if useful to the author. |
| `DiagnosticCost` | Reproducing the schema exactly requires work at runtime — match counting, trial validation, a residual sub-schema. The plan is exact; `Capability` carries the price. | Generate; emit the validator. |
| `DiagnosticAssumed` | The plan accepts a strict superset of the schema **even after the validator runs**, with the plan's own machinery bounding the excess: today only a `TagAsserted` discriminator, which trusts a declared tag instead of proving the branches disjoint, so a mis-tagged instance a second branch would also have matched is accepted. | Generate; emit the validator, but the accepted set is wider than the schema's — surface the diagnostic. |
| `DiagnosticUnenforced` | The plan admits extra values and **nothing in it closes the gap**: a constraint was dropped and no residual check replaces it. The message names the construct. | Representable; the choice is the backend's. Generate anyway (ogen's permissive behavior for keywords it ignores) or refuse — but surface the diagnostic either way. |
| `DiagnosticUnsupported` | No sound conversion exists for the construct. Always paired with a capability past `RawEvaluation`, so the gate below refuses these anyway. | Refuse; surface the diagnostic. |

A plan carrying no diagnostic of the last three kinds accepts exactly what its schema does,
to the extent the lowering discharges its `Requirements` (§7).

| `CapabilityLevel` | ogen generation | Rationale |
|---|---|---|
| `DirectGoType` | **Yes** | Plain `ir.Type`, no validator. |
| `GoTypeWithValidation` | **Yes** | `ir.Type` + `ir.Validators`. |
| `StaticDispatch` | **Yes** | `ir.Type{Kind: KindSum}` with a `SumSpec` strategy from §3 (`TypeDiscriminator`/`ValueDiscriminators`/`Discriminator`+`Mapping`). |
| `RawEvaluation` | **Partial** | The level says the check needs the raw JSON document, not that fidelity is lost: such plans carry only a `DiagnosticCost`. For `PredicateCountDispatch` the *representation* is a sound over-approximation (design §24: the union of all branches), closed by re-running every branch's checks at decode time and counting matches — such plans carry only a `DiagnosticCost` — see the **`PredicateCountDispatch` lowering contract** in §3 for the exact match-count algorithm. ogen has no existing `SumSpec` shape for "runtime match-count over N branches," so until that lowering is built, treat as refuse-with-diagnostic; once built, it is a legitimate (if slower) generation target — the plan is not dropped, per the "no silent caps" rule. |
| `EvaluationStateValidation` | **No — refuse** | No evaluated-annotation tracking in ogen (confirmed: no `unevaluatedProperties`/`dynamicRef` references in `gen/`). Surface the plan's `SeverityError` diagnostic. |
| `DynamicSchemaResolution` | **No — refuse** | Same: no dynamic-scope resolution engine exists or is planned for v1. |
| `Unsupported` | **No — refuse** | No sound conversion exists at all (e.g. an unguarded reference cycle, design §19); always carries a `SeverityError` diagnostic explaining why. |

## 7. Requirements → what the lowering must discharge

`Result.Requirements` / `DocumentResult.Requirements` (`plan/requirements.go`) name the
places where a lowering decision decides whether the generated program reproduces the
schema's accepted set. §6 says whether a plan can be generated at all; this says what the
generated code has to do to be correct once it is.

It exists because the compiler chooses none of that (design §25.1). How integers are
sized, whether unknown properties are retained, which regex engine runs at validation
time — all of it belongs to the backend, so an exactness verdict was never the compiler's
to give. What it can do is point at the decisions.

Each entry is a `plan.Location{Pointer, Position, Detail}`: the JSON Pointer of the schema
the requirement arises from (the same pointer space `Diagnostic.Pointer` uses), the source
position when the parser retained one, and a short `Detail` naming the construct so a
report needs no re-derivation. Slots are sorted by pointer then detail, so the field is
stable across runs.

| Slot | Reports | Discharged by | If ignored |
|---|---|---|---|
| `RawEvaluation` | Checks that inspect something decoding discards, so they cannot be evaluated against the decoded Go value alone (design §24.3). | Running the check against the raw JSON, or retaining what would be dropped. | The check silently measures the wrong thing. |
| `UnboundedNumeric` | Numeric slots the schema does not bound, so a fixed-width Go type narrows them (design §24.2). | Choosing a type that holds every value the schema admits, or **declaring** the narrowing. | Values the schema accepts overflow or lose precision, unreported. |
| `JSONEquality` | Checks defined by equality of JSON *values*, not of the decoded Go value. | Comparing at the JSON level. | The check rejects instances the schema accepts. |
| `ECMARegex` | Patterns RE2 does not read the same way (design §11.10). | An ECMA-262 engine — `github.com/dlclark/regexp2` in `regexp2.ECMAScript` mode is what this repo and ogen's runtime use. | The pattern enforces something other than what the author wrote, in either direction. |
| `EvaluationTracking` | Checks needing evaluated-location annotations: `unevaluatedProperties`, `unevaluatedItems`. | Annotation tracking through the whole applicator tree. | Nothing — the plan that produced the entry always has a capability past `RawEvaluation`, so §6 already refuses it. Attribute by `Location.Pointer` (§7.2): other plans in the same document are unaffected. |

**Every slot over-reports on purpose.** A backend that handles a requirement it did not
strictly need is correct; one that misses a real requirement is wrong *and silent*, which
is §24.3's third failure mode — a storage decision below a check quietly turning into a
§24.1 violation above it. `ECMARegex` is the clearest case: whether a given `\d` actually
diverges between the two engines is undecidable, so the escape is reported as written.
`^[a-z]+$` and `^[\t ]$` are not listed.

The converse also holds, and is what keeps the field readable: a requirement the compiler
can discharge statically is **not** reported. `{"type":"integer","maximum":1000}` is
provably adequate as an `int64`, so it does not appear in `UnboundedNumeric`; bare
`{"type":"integer"}` does. A slot that listed every numeric field would train a consumer
to ignore it.

### 7.1. `RawEvaluation` — the one that catches ogen

This is the slot a struct-generating backend has to read, because ogen's natural lowering
is exactly the thing it warns about: a generated struct has fields for the declared
properties and drops everything else, and several keywords are defined over the properties
that were dropped.

| Construct | Why the decoded value is not enough |
|---|---|
| `minProperties` / `maxProperties` | The count is over the instance's properties, undeclared ones included. A struct has already discarded them, so the count comes out low. |
| `propertyNames` | Applies to every property name in the instance, undeclared ones included. |
| `additionalProperties: false` | Rejects undeclared names — precisely the information a struct throws away, so nothing is left to reject. |
| `uniqueItems` | Also in `JSONEquality`: two JSON-distinct objects whose differences all sit in dropped properties decode to the same Go value, and the check then rejects an array the schema accepts. |

Discharging it means one of two things: evaluate those checks against the raw JSON before
or alongside decoding, or retain the dropped properties (an overflow map) so the decoded
value still carries what the check needs. Which one is the backend's call; doing neither is
not a third option.

### 7.2. Scope

Requirements are unioned over the whole compilation, not reported per plan: `Compile`
merges the root schema's with every reachable `$ref` target's, and `CompileDocument` merges
across every component as well. A backend generating one type at a time therefore cannot
read "does *this* plan need raw evaluation" off the field directly — use `Location.Pointer`
to attribute an entry, which is why every entry carries one.

`Requirements.Empty()` reporting true means the plans ask nothing beyond ordinary decoding
and validation. That is a real answer, not a default: the field is populated on both the
single-schema and the whole-document path, and the conformance suite asserts every slot is
reached by some schema, so an empty slot means "nothing here", never "the rule stopped
firing".

## 8. Metadata → godoc, defaults, and extensions

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

## 9. Whole-document compilation

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
