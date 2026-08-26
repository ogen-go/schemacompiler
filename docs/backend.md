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
gogen.Lower(plans map[plan.SchemaID]plan.CompilationPlan) ([]*Named, error)
gogen.Render(types []*Named, opts Options) ([]File, error)
```

Two stages, both public. `Lower` owns plan → Go semantics; `Render` owns source text. ogen
may call `Render` or supply its own, which is the reason for the constraint in §4.

`Lower` takes no options because none of its decisions are the caller's: §7 is the reason
it takes the whole map rather than one plan, and a knob that changed a shape per call would
give two callers two Go types for one schema. Every element of the result is a declaration,
so `[]*Named` says what `[]GoType` would have left to a type assertion.

## 1. Naming

Restrictive and mechanical. The author names types; the backend does not guess.

- Drop structural segments from the pointer: `components`, `schemas`, `properties`,
  `$defs`, `definitions`, `allOf`, `additionalProperties`, `patternProperties`,
  `dependentSchemas`.
- `items`/`prefixItems` → `Item`. `oneOf`/`anyOf` → `Variant<n>`.
- Split each surviving segment on `_` and on every rune that cannot appear in a Go
  identifier, upper-case each word, concatenate.
- No initialism table, no pluralization, no `Id` → `ID`. `title` never participates in a
  name; it is godoc.
- `x-go-name` overrides outright. `Metadata.Extensions` already carries every `x-*` key,
  decoded and explicitly uninterpreted, so this needs nothing from the compiler.
- A collision is an **error naming both pointers**. Never `Pet2`: a suffix makes adding one
  schema silently rename another author's type.

Implemented in `gogen/name.go` (the rule) and `gogen/assign.go` (overrides and collisions),
pinned against a live ogen checkout by `TestGogenNamesOgenCorpus`.

Two properties of the rule fell out rather than being designed, and both are worth knowing.
Upper-casing the first word escapes every Go keyword for free, since all of them are
lower-case: a schema named `type` becomes `Type` and needs no annotation. And the identifier
check is what handles a name no rule could derive, rather than handing the author something
sanitized that they did not write.

The separator set is the widest of those two properties, and it was measured rather than
chosen. Restricting it to `[-_. ]` left 25 of 15253 corpus properties unnameable — five
distinct spellings, `$ref`, `$schema`, `@timestamp`, `@odata.location`, `+1` — in three
documents including k8s and the GitHub API. Requiring `x-go-name` there means patching a
specification the ogen user does not own. Treating a rune that cannot appear in an
identifier as a separator invents nothing (it only drops), and two names that drop to the
same identifier collide, which is already a hard error. What survives is the case the
escape hatch is actually for: GitHub's `+1` and `-1` reaction counts drop to a leading
digit and still ask the author for a name.

### Why the long spelling wins

Measured over 58 ogen corpus documents, 1891 named plans:

| rule | names | in-document collisions | p50 | p90 | p99 | max |
| --- | --- | --- | --- | --- | --- | --- |
| full camel-case | 1891 | **0** | 20 | 41 | 66 | 82 |
| last dot-segment only | 1701 | **81** | 16 | 28 | 35 | 45 |

Measured again against the shipped rule: 1891 schemas across 58 documents, **zero
collisions and zero schemas the rule cannot name**. On the JSON-Schema-Test-Suite, whose
definition names are adversarial by construction, it refuses 5 of 96 — every one a name
containing `%`, `~`, `/`, a quote, or nothing at all.

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
over a `map[string]T` can. `Requirements.RawEvaluation` over-reports deliberately
(integration.md §7) —
it names checks a *typical* struct lowering breaks — so reading it as the boundary both
delegates cases that do not need it and teaches the reader to trust a slot never meant to
be exact.

So: `preservedBy(GoType, plan.PredicateExpr) bool`, exhaustive over both sides, with a
`default` that panics rather than guesses. Delegation is per-predicate rather than
per-type: build a sub-plan of the delegated checks alone and compile that. §10 records what
it turned into once written.

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
| `RawEvaluation` | 120 | 6.3% |
| `EvaluationStateValidation` / `DynamicSchemaResolution` / `Unsupported` | 0 | 0% |

No schema in that corpus is refused. Across all 58 documents the diagnostics are 31
advisory, 95 cost and 11 assumed, with **zero `unenforced` and zero `unsupported`** — no
constraint is dropped anywhere in it. `unevaluatedProperties` and `$dynamicRef` (#5, #6)
account for roughly a fifth of the JSON-Schema-Test-Suite and none of this corpus; the
suite is adversarial by construction and is not the workload.

## 6. What this exposed in `plan`

The backend is the first consumer to read these closely, and three did not survive
contact. All three are now closed; they are recorded because the shape recurs.

- **Four sub-plan slots were documented as meaningfully nil and are never nil** —
  an object's `Additional` and an array's `Rest`, on both the representation and the
  structure predicate, nil zero times in 12354 occurrences. The planner states every
  sub-plan outright: `additionalProperties` absent is a plan over `AnyRepresentation`,
  `false` one over `NeverRepresentation`. integration.md §0.1 called the
  `Rest`-rejects/`Additional`-does-not asymmetry "deliberate, not an oversight" — a
  careful distinction between two states that cannot occur.
- **`RecursiveRepresentation` was never constructed.** Guarded recursion arrives as a
  `ReferenceRepresentation` closing a cycle in the `StaticReferenceGraph`, and an
  unguarded one is `Unsupported` before lowering. Neither interpreter had a case for it,
  so emitting one would have failed both. Removed.
- **`UnionRepresentation.Alternatives` is `[]Representation`, not `[]CompilationPlan`** —
  1211 occurrences, so this is live and load-bearing. It is the storage shape alone;
  branch plans live in `Dispatch`, which is what sum lowering reads.

`planwalk.ContractViolations` now enforces all three alongside the guard-width rule, over
the keyword matrix, the suite walk and both ogen corpus walks. Each was a documented
contract that nothing exercised, which is issue #60's shape one layer up: a rule nothing
tests is a rule nobody notices breaking.

One caveat remains, and is not a defect: `plan.ResidualChecks` compares
`ObjectRepresentation.Additional` by **pointer identity**, which holds because the planner
shares the pointer with the structure predicate it derives. A backend that rebuilds or
round-trips a plan breaks that comparison silently. `gogen` must not reconstruct plans it
intends to call `ResidualChecks` on.

## 7. Recursion is broken at the node, never at the edge

A schema may reference itself, and `opt.Opt[T]` stores its value inline, so some references
have to become pointers or the generated package does not compile. Which ones is the whole
question.

**Not the edge.** Choosing a minimal set of references to cut is the feedback arc set
problem: NP-hard, and worse, without a canonical answer. Two documents referencing the same
schema could cut differently and disagree about its Go shape.

**The node.** If a type is a member of a cycle of inline storage, *every* direct reference
to it is a pointer — including references from outside the cycle. SCC membership is
canonical, so "several graphs referencing the same schema" dissolves: there is one answer
per schema, and it does not depend on who asks or in what order.

This is why `Lower` takes the whole document map. Lowering schema by schema would compute
the components of a graph it cannot see.

Three ordered passes, none reading a later one's output:

1. **Shape** — from `plan.Representation` alone. References become `*Named` nodes, so the
   lowered types are already a graph.
2. **Components** — Tarjan over *Go-storage* edges, marking every type in a cycle.
3. **Indirection** — each direct reference to a marked type becomes a `Pointer`.

The edge set in pass 2 is not the frontend's. `internal/frontend` classifies recursion over
instance-descent edges to answer §19's guarded/unguarded question; a `oneOf` alternative is
a Go interface but not an instance descent, and an array item is a descent but is already
indirect in Go. Same algorithm, different graph, so Tarjan lives in `internal/scc` and each
caller brings its own edges.

What counts as inline: struct fields, tuple slots, an `opt` wrapper, and a named type's own
underlying type. What does not, because the language already indirects: slices, maps,
interfaces, and pointers themselves.

Only three `opt` types are needed rather than six. `opt.Opt[*Node]` breaks the cycle, so
instantiating with a pointer is the entire adaptation — there is no parallel `OptPtr`.

Measured over the 58 ogen documents: 1592 types lowered, **10 recursive (0.63%)**. Pointer
indirection is the one thing lowering adds that an author sees in their own code, and it
stays rare.

## 8. `patternProperties` gets a slot per rule

Rules do not share an element type, so a single map cannot hold them. The shape is ogen's,
and it was already right (`gen/schema_gen.go`):

| object | lowering |
| --- | --- |
| no fields, one rule, closed | `Map{Elem: T, Pattern: p}` |
| no fields, no rules, open | `Map{Elem: T}` |
| anything else | `Struct{Fields, Patterns: one Map per rule, Additional}` |

Each map keeps its own pattern, because nothing downstream can re-derive it — the plan
states the rules positionally, and a Go map type says only that its keys are strings.
A declared field is never routed through a rule: the planner has already intersected every
matching pattern schema into that field's plan (design §12.3), so its slot is exact.

The first lowering widened two-or-more rules to `map[string]any`, which threw away every
element type for no reason.

### Routing: validate against all, store in the first

A key is validated against every rule whose pattern it matches — JSON Schema conjoins them,
and `additionalProperties` applies only when none matched. ogen's decoder template loops all
patterns and sets `handled`, which is that rule, but it does two things we should not copy.

It re-runs the value decoder inside each matching branch, reading a token the previous branch
already consumed. Decode once into the value, then test the patterns against the key: at one
or two rules per object that loop is nothing.

And it stores the key in *every* matching map, so encoding emits it twice. Round-tripping
`{"ab":"x"}` under `{"^a":{"type":"string"},"b$":{"type":"string"}}` gives
`{"ab":"x","ab":"x"}`, and the Go value has two independent slots for one JSON key — a state
no document corresponds to.

Store in the **first** matching map instead. That is lossless: an accepted value under
overlapping rules i < j satisfies both, so it lies in ⟦Sᵢ⟧ ∩ ⟦Sⱼ⟧ ⊆ ⟦Sᵢ⟧, and mapᵢ's element
type was built to hold ⟦Sᵢ⟧. Every key then lives in exactly one slot and round-trip is
identity.

### Is the map/struct boundary itself sound?

Yes, and separately from the routing bug above.

The map case over-accepts *keys* and nothing else: `map[string]T` can hold a key no rule
matches, which a closed object rejects. That is §24's permitted direction, and the check that
closes it is `ObjectStructurePredicate` — subject to the §2 hazard below, but not to a
representation that cannot hold an accepted value.

A declared field never has to share its name with a rule, because the planner has already
intersected every matching pattern schema into it (§12.3). `{"properties":{"abc":{"type":
"string"}},"patternProperties":{"^a":{"type":"number"}}}` lowers `abc` to `Never`, which is
right — the intersection is empty and the property can never be present.

What it is not is *stable*. Adding a second `patternProperties` rule turns `map[string]T`
into a struct of two maps; adding one `properties` entry turns it into a struct too. A
one-keyword schema edit rewrites the generated API. That is inherent to letting the common
`additionalProperties`-only case be a map at all, it is long-standing ogen behaviour, and it
is a stability property rather than a soundness one — but it is the thing that will surprise
someone, so it is written down here.

### What it does not fix

`ResidualChecks` reports **zero** residual work for `{"type":"object","patternProperties":
{"^a":{"type":"string"}}}` at `DirectGoType`, because `objectRestates` discharges the
`ObjectStructurePredicate` against the *plan's* representation, which carries the rules
positionally. A `Map` with a `Pattern` now preserves the element type, but nothing in the Go
type enforces that a key not matching any rule is rejected when the object is closed.

So a backend reading `ResidualChecks` as its boundary still emits no validator here and
silently over-accepts. That is §2's rule with a concrete instance behind it: the boundary is
`preservedBy(GoType, expr)`, a property of the pair. `ResidualChecks` is discharged against
a representation `Lower` did not choose, and goes stale the moment it chooses another.

## 9. The type checker is the only witness for the recursion pass

"Invalid recursive type" is a property of the whole graph, not of any one type. A test can
assert a pointer appears exactly where it expects and still describe a package that does not
build, and `go/parser` does not help — such a file parses. Only `go/types` sees it.

`internal/gotypecheck` renders lowered types as one self-contained file and checks it. `opt`
is restated in the preamble rather than imported, so the checker needs no importer; what
matters is reproduced exactly, that the value is stored inline. Every fixture and all 57
lowerable corpus documents go through it, and a control test pins that the checker really
does reject the graph the recursion pass exists to prevent.

It is also the *only* rendering of a `GoType` the tests have. The first version of these
tests asserted against a compact notation of their own — `map[^a]string`, `opt[string]`,
`sum(string|bool)` — which reads like Go, is not Go, and parses as nothing. A second
notation is a second answer to "what does this lower to", and it is the one no tool can
check: a table written in it can be wrong in a way that looks right. The tables hold Go
type expressions now, `requireGoType` parses every expectation before comparing it, and a
control test pins that `map[^a]string` does not parse.

A pattern is a comment in that rendering, not part of the type. `map[^a]string` was the
sharpest version of the problem: Go map keys here are always `string`, so the notation
spelled a key type that does not exist.

## 10. What `preservedBy` turned out to be

Three outcomes, and they are not the three this document first guessed.

| | meaning |
| --- | --- |
| **Discharged** | the Go type states it; emit nothing |
| **Inline** | generated code decides it from the decoded value |
| **Delegate** | it needs the raw document |

`Discharged` was missing, and it is the largest of the three. `Inline` was assumed to be the
good outcome, but a predicate the type already enforces is better than one it can check: a
`required` property stored in a field that is not `opt.Opt` has nowhere to record absence, so
a decoded value is already proof the property was there. Same for a kind assertion over a
type that holds one kind, a `NumericDomainPredicate` over an integer type, and a
`ReferencePredicate` over the `Named` it refers to.

And "change the lowering so it becomes inlinable" never arose. §2 offered it for
`minProperties` over a struct that discards unknown properties — but `Lower` states
`additionalProperties` in every object, so an open one always has an overflow map and a
closed one admits no key it did not store. The case the fix was for does not exist in this IR.

### Delegation is exactly the `RawEvaluation` set

Four predicates survive no Go type: `NegationPredicate`, `ShapePredicate`,
`ContainsCountPredicate`, `PropertyNamesPredicate`. Those are precisely
`plan.RawEvaluation`'s members (design §4.2).

That is not a coincidence and not a shortcut. The ladder ranks *how much raw JSON generated
code must retain and inspect*; `preservedBy` asks whether a Go type keeps what a predicate
reads. "Needs the document" and "no type preserves it" are one statement read from either
end. `TestRawEvaluationAlwaysDelegates` pins it, so the two ends cannot drift apart.

Everything else delegates only when the value reaching it is raw — an `Any`, which keeps
everything and exposes nothing. `UniqueItemsPredicate` is the strict case: it compares whole
values, and two raw documents can differ byte for byte and be the same JSON value, so nothing
*beneath* the array may be raw either.

### Measured

58 ogen documents, each type's own predicates:

| | count | share |
| --- | --- | --- |
| discharged | 2468 | 61.3% |
| inline | 1435 | 35.6% |
| delegate | **124** | **3.1%** |

The 124 are 91 `ShapePredicate`, 31 `RequiredPredicate` over an untyped `Any`, and one each
of `FormatPredicate` and `PropertyNamesPredicate`. 66 of 1592 types carry no check of their
own at all.

### What this pass does not do

It classifies each type's *own* predicates. A nested plan is carried by a nested type, and
pairing the two trees — walking a `GoType` and a `CompilationPlan` in step so every
sub-schema's checks land on the slot that stores them — is the renderer's data model, not
this pass's. `Checks` sits on `Named` today; the sub-schema checks it recurses into are
reached through the structural predicates, which carry their own sub-plans.

That is also why the counts above are per-type rather than per-keyword: the 4181
`FormatPredicate`s the corpus contains live mostly in property sub-plans, and they will be
classified against the field types that hold them.

## 11. Two things `Render` cannot say yet

**A sum renders as `any`.** It needs a discriminator to be worth more, and dispatch is not
lowered. The alternatives go in the doc comment of the declaration or field that holds it —
not in the type, because Go block comments do not nest and a sum of a sum would have its
first `*/` close both. A sum nested deeper than that loses the note; the plan keeps it
either way. `any` over-accepts in the direction §24 permits, and the validation plan still
narrows it.

**A pattern is a comment.** `Pattern0Props map[string]T` says which values it holds but not
which keys route to it. That is decoder data, and there is no decoder yet.

Both are why generated code is not yet a third validator in the differential harness (§3):
it does not reject anything.
