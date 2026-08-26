# Backend: lowering a plan to Go (`gogen`)

This is a design, not an implementation. It records the decisions taken, the measurements
they rest on, and the places `plan` turns out not to be ready for its first real consumer.

schemacompiler stops at the analyzed plan (docs/implementation.md's "Scope"), and
docs/integration.md maps the plan onto ogen's existing `gen/ir`. `gogen` is the other
answer to the same question: a Go backend owned here, which ogen consumes as a library
rather than reimplementing. §§0, 5-9 of integration.md are backend-agnostic and apply
unchanged — what a plan *accepts*, the capability gate, `Requirements`, metadata,
whole-document compilation. §§1-4 map onto ogen's IR specifically and are reference
material here, not the specification.

## 0. Shape

```go
gogen.Lower(plans map[plan.SchemaID]plan.CompilationPlan, opts Options) ([]GoType, error)
gogen.Render(types []GoType, opts Options) ([]File, error)
```

Two stages, both public. `Lower` owns plan → Go semantics; `Render` owns source text. ogen
may call `Render` or supply its own, which is the reason for the constraint in §4.

## 1. Naming

Restrictive and mechanical. The author names types; the backend does not guess.

- Drop structural segments from the pointer: `components`, `schemas`, `properties`,
  `$defs`, `definitions`, `allOf`, `additionalProperties`, `patternProperties`,
  `dependentSchemas`.
- `items`/`prefixItems` → `Item`. `oneOf`/`anyOf` → `Variant<n>`.
- Split each surviving segment on `[-_. ]`, upper-case each word, concatenate.
- No initialism table, no pluralization, no `Id` → `ID`. `title` never participates in a
  name; it is godoc.
- `x-go-name` overrides outright. `Metadata.Extensions` already carries every `x-*` key,
  decoded and explicitly uninterpreted, so this needs nothing from the compiler.
- A collision is an **error naming both pointers**. Never `Pet2`: a suffix makes adding one
  schema silently rename another author's type.

### Why the long spelling wins

Measured over 58 ogen corpus documents, 1891 named plans:

| rule | names | in-document collisions | p50 | p90 | p99 | max |
| --- | --- | --- | --- | --- | --- | --- |
| full camel-case | 1891 | **0** | 20 | 41 | 66 | 82 |
| last dot-segment only | 1701 | **81** | 16 | 28 | 35 | 45 |

The 82-character names are k8s reverse-DNS component names
(`io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.CustomResourceDefinitionCondition`).
That length is the document's, not the rule's — nesting depth is not what drives it.

Every collision under the shorter rule is a version pair: `…flowcontrol.v1beta1.LimitResponse`
against `…flowcontrol.v1beta2.LimitResponse`. Those are distinct types, so the short name is
not merely inconvenient, it is wrong, and the 81 errors it raises have no good manual answer
beyond re-adding the version the rule just removed. Thirteen characters at p90 does not buy
that.

## 2. The validation boundary is derived, not looked up

Validation is split: constraints readable off the decoded Go value are generated inline;
the rest are delegated to a compiled `nodetree` over the raw JSON. The rule that decides is
**not** "consult `Requirements.RawEvaluation`":

> A predicate is inlined iff the Go type `gogen` chose preserves everything that predicate
> reads.

That is a property of the *pair*, and `gogen` chooses the lowering. `minProperties` over a
struct that discards unknown properties cannot be inlined; the identical `minProperties`
over a `map[string]T` can. `RawEvaluation` over-reports deliberately (integration.md §7) —
it names checks a *typical* struct lowering breaks — so reading it as the boundary both
delegates cases that do not need it and teaches the reader to trust a slot never meant to
be exact.

So: `preservedBy(GoType, plan.PredicateExpr) bool`, exhaustive over both sides, with a
`default` that panics rather than guesses. Three outcomes per predicate — inline it; change
the lowering so it becomes inlinable (an overflow map); or delegate it. Delegation is
per-predicate rather than per-type: build a sub-plan of the delegated checks alone and
compile that.

`nodetree`'s transitive dependencies are `jx`, `regexp2`, `go-faster/errors` and `plan` —
no libopenapi — so delegating costs a generated program nothing ogen does not already
carry.

## 3. Keeping the two paths honest

Two validation paths is the real cost of the split, and the answer is the harness that
already exists. **Generated code joins the differential harness as a third validator**:
`planterp` (the oracle), `nodetree` (the fast path), and generated Go, over the same
schemas and instances the suite walk already covers. An inline check that disagrees with
`planterp` is a bug found the same day rather than at a user's site.

Alongside it, the cheap always-on invariant from issue #60: every predicate in a plan is
*accounted for* — inlined, delegated, or reported — and never silently absent. That is the
exact failure class the keyword matrix was built for, arriving one layer down.

## 4. The IR must carry checks structurally

ogen rendering its own source means ogen must reproduce the §2 split faithfully or drift
from us in silence — issue #60's shape at a new seam. The constraint that prevents it:

**`GoType` carries checks as data, never as Go source text.** `Check{Kind: MinLength,
Value: 2}`, not `"if len(x.Name) < 2"`; `Delegate{Plan: SchemaID}` for the rest. A renderer
can then be syntactically wrong, which a compiler catches, but not semantically wrong,
which nothing catches.

For the same reason a generated program should carry the delegated plans in a compact
serialized form rather than as `plan.CompilationPlan` Go literals. `plan` is an analysis
output type today and free to change; embedding its Go-level shape in generated code
freezes it for every downstream user, and the literals are large.

## 5. What the corpus says about scope

Per-schema capability over the same 58 ogen documents, 1891 component schemas:

| capability | count | share |
| --- | --- | --- |
| `DirectGoType` | 144 | 7.6% |
| `GoTypeWithValidation` | 1123 | 59.4% |
| `StaticDispatch` | 504 | 26.7% |
| `PredicateDispatch` | 120 | 6.3% |
| `EvaluationStateValidation` / `DynamicSchemaResolution` / `Unsupported` | 0 | 0% |

No schema in that corpus is refused. Across all 58 documents the diagnostics are 31
advisory, 95 cost and 11 assumed, with **zero `unenforced` and zero `unsupported`** — no
constraint is dropped anywhere in it. `unevaluatedProperties` and `$dynamicRef` (#5, #6)
account for roughly a fifth of the JSON-Schema-Test-Suite and none of this corpus; the
suite is adversarial by construction and is not the workload.

## 6. What this exposes in `plan`

The backend is the first consumer to read these, and three do not survive contact.
Measured over the ogen corpus and the suite together, 3854 object representations:

- **`ObjectRepresentation.Additional` is never nil** — 0 of 3854 (3654 `Any`, 39 `Never`,
  161 other). integration.md §0.1 documents nil as "does not reject", a reading nothing
  produces and nothing tests. Worse, the sibling `ObjectStructurePredicate.Additional`
  documents nil as the *opposite* — "admits any value". Two nils with opposite meanings on
  adjacent types, one of them unreachable. Settle it before `Lower` reads either.
- **`RecursiveRepresentation` is never constructed** — 0 occurrences, and the only
  construction site in the repo is `planwalk`'s exhaustiveness table. Recursion reaches the
  plan as `ReferenceRepresentation` against the `StaticReferenceGraph` instead. It is dead
  API: either the planner should emit it or it should go.
- **`UnionRepresentation.Alternatives` is `[]Representation`, not `[]CompilationPlan`** —
  1279 occurrences, so this is live and load-bearing. Sum lowering must therefore read
  `Dispatch` for the branch plans and treat the representation as the storage shape alone.

One further caveat, not a defect: `plan.ResidualChecks` compares
`ObjectRepresentation.Additional` by **pointer identity**, which holds because the planner
shares the pointer with the structure predicate it derives. A backend that rebuilds or
round-trips a plan breaks that comparison silently. `gogen` must not reconstruct plans it
intends to call `ResidualChecks` on.
