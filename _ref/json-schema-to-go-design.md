# Compiling Modern JSON Schema into Go Types

## Design document

**Target dialect:** JSON Schema Draft 2020-12  
**Primary target:** Go type declarations, decoders, and validators  
**Status:** Proposed architecture

---

## 1. Purpose

JSON Schema resembles a type-definition language, but its semantics are those of a predicate over the universe of JSON values.

A Go type answers:

> How is a value represented and constructed?

A JSON Schema answers:

> Does this already-existing JSON value satisfy this schema?

The compiler described here translates a JSON Schema into four separate artifacts:

1. a Go representation;
2. residual validation logic;
3. an instance-dispatch plan;
4. a schema-resolution plan.

This separation is necessary because many JSON Schema constraints cannot be represented by an ordinary Go type, while other apparently complex schemas can be simplified into direct Go types or finite static dispatch.

The intended semantic contract is:

\[
x \models S
\iff
x \in \llbracket G(S) \rrbracket
\land
V(S, x)
\]

where:

- \(S\) is a JSON Schema;
- \(G(S)\) is the generated Go representation;
- \(V(S,x)\) is the residual validator.

When dynamic references are present, validation may additionally depend on a resolution environment:

\[
x \models S
\iff
V(S, x, R)
\]

where \(R\) is the dynamic reference scope.

---

## 2. Core distinctions

The compiler must not classify a whole schema merely by the keywords it contains. It should first preserve exact semantics, normalize the schema, and classify the resulting plan.

Four concepts must remain distinct.

### 2.1 Representation

The Go data shape capable of storing accepted values.

Examples:

```go
string
float64
[]Item
map[string]Value
struct { ... }
```

### 2.2 Validation

A predicate that cannot be enforced by Go's ordinary type system.

Examples:

```text
minimum length
numeric range
regular-expression match
required-property presence
array uniqueness
exactly one branch matches
```

### 2.3 Instance dispatch

Selection among a finite set of schemas or representations already known at generation time.

Examples:

```text
dispatch by JSON kind
dispatch by a "kind" property
dispatch by property presence
evaluate several overlapping oneOf branches
```

### 2.4 Schema resolution

Determination of which schema resource or anchor a reference denotes.

Static `$ref` resolution can normally happen at schema-load or generation time. `$dynamicRef` can require scope-sensitive resolution while validation is running.

Runtime dispatch is not dynamic resolution.

---

## 3. Applicability of type-specific keywords

A type-specific keyword does not implicitly assert its corresponding JSON type.

For example:

```json
{
  "minLength": 5
}
```

accepts:

- every non-string JSON value;
- strings containing at least five Unicode code points.

It does not mean:

```json
{
  "type": "string",
  "minLength": 5
}
```

The semantic form of `minLength` is:

\[
\operatorname{String}(x)
\implies
\operatorname{Length}(x) \ge 5
\]

Equivalently:

\[
\neg \operatorname{String}(x)
\lor
\operatorname{Length}(x) \ge 5
\]

This rule applies to all type-specific keyword families.

| Applicable kind | Keywords |
|---|---|
| String | `minLength`, `maxLength`, `pattern`, some `format` behavior |
| Number | `minimum`, `maximum`, `exclusiveMinimum`, `exclusiveMaximum`, `multipleOf` |
| Array | `minItems`, `maxItems`, `uniqueItems`, `contains`, `minContains`, `maxContains`, `prefixItems`, `items` |
| Object | `properties`, `patternProperties`, `additionalProperties`, `propertyNames`, `required`, `dependentRequired`, `dependentSchemas`, `minProperties`, `maxProperties` |

Pseudocode:

```text
compileTypeConditionalKeyword(kind, predicate):
    return GuardedPredicate(
        guard = InstanceKindIs(kind),
        predicate = predicate
    )
```

At runtime:

```text
validateMinLength(value, minimum):
    if value.kind != String:
        return valid

    return unicodeCodePointLength(value) >= minimum
```

This distinction is central to sound Go type generation. A schema containing `properties` but no object type assertion still accepts non-object values.

### 3.1 Guards and assertions are not the same thing

The rule above constrains type-*conditional* keywords only. It does not forbid
assertions, and must not be read as doing so: `type`, `const` and `enum` assert.

```text
{"minLength": 5}       guard      a number is accepted
{"type": "string"}     assertion  a number is rejected
```

Both appear in the validation IR (§8) and both carry a kind set, but they use it in
opposite directions. A guard whose kind set excludes the instance contributes nothing; an
assertion whose kind set excludes the instance rejects it. Compiling `type` into a guard
loses the schema; compiling `minLength` into an assertion invents one. §8 gives them
distinct forms so that neither mistake is expressible.

---

## 4. Compiler output model

The compiler should return a plan rather than a single type.

```go
type CompilationPlan struct {
    Representation Representation
    Validation     ValidationPlan
    Dispatch       DispatchPlan
    Resolution     ResolutionPlan
    Metadata       Metadata
    Capability     CapabilityLevel
}
```

### 4.1 What a plan accepts

An instance \(x\) satisfies a plan \(P\) when

\[
x \in V(P) \cap \llbracket D(P)\rrbracket
\]

that is: every predicate in `P.Validation` applicable to \(x\) accepts it, and
`P.Dispatch` selects a branch whose own plan accepts it, by the same rule, recursively.

`P.Representation` is **not** a conjunct. It is a storage decision, related to acceptance
only by a side-condition (§7, §24):

\[
V(P) \cap \llbracket D(P)\rrbracket
\subseteq
\llbracket R(P)\rrbracket
\]

Acceptance is therefore definable without reference to any Go type, and is testable
without a backend. `{"not": {}}` accepts nothing; which Go value represents it is not a
question the semantics has to answer.

`P.Resolution` is not a conjunct either: it is the environment references resolve in.

### 4.2 Capability levels

```go
type CapabilityLevel uint8

const (
    DirectGoType CapabilityLevel = iota
    GoTypeWithValidation
    StaticDispatch
    PredicateDispatch
    EvaluationStateValidation
    DynamicSchemaResolution
    Unsupported
)
```

#### `DirectGoType`

A normal Go type captures the accepted set closely enough that no schema-specific runtime check remains.

Example:

```json
{ "type": "string" }
```

#### `GoTypeWithValidation`

The Go representation is statically known, but residual predicates remain.

Example:

```json
{
  "type": "string",
  "minLength": 3
}
```

#### `StaticDispatch`

All alternatives are known at generation time and a finite structural discriminator selects the branch.

Examples:

```json
{
  "oneOf": [
    { "type": "string" },
    { "type": "number" }
  ]
}
```

or a tagged object union selected by a required literal property.

#### `PredicateDispatch`

The alternatives are statically known, but selecting or validating them requires evaluating predicates, often including an exact match count.

Example:

```json
{
  "oneOf": [
    { "type": "string", "pattern": "^a" },
    { "type": "string", "minLength": 5 }
  ]
}
```

#### `EvaluationStateValidation`

Validation depends on which object properties or array indices were evaluated by successful adjacent applicators.

Examples:

```text
unevaluatedProperties
unevaluatedItems
```

#### `DynamicSchemaResolution`

The schema target itself can depend on runtime dynamic scope.

Primary example:

```text
$dynamicRef
```

#### Reading the ladder

The levels are ordered by **how much of the raw JSON document the generated code must
retain and inspect**, not by a vague sense of difficulty:

```text
DirectGoType               nothing beyond decoding
GoTypeWithValidation       value-local checks
StaticDispatch             a cheap peek before selecting a branch
PredicateDispatch          full document access while selecting
EvaluationStateValidation  full access, plus which locations were evaluated
DynamicSchemaResolution    plus the runtime dynamic scope
```

Read this way the ladder is the *inspection floor* of §7.3, and the boundary between
`StaticDispatch` and `PredicateDispatch` is exactly the boundary between a plan a backend
may discharge by decode-then-validate and one it must evaluate against raw JSON (§24.3).

---

## 5. Semantic intermediate representation

The first IR should preserve JSON Schema semantics. It should not immediately lower every combinator into Go types.

```text
Expr :=
    Any
  | Never
  | KindSet(kinds)
  | Literal(value)
  | Shape(shape)
  | Predicate(predicate)
  | All(expr...)
  | AnyOf(expr...)
  | ExactlyOne(expr...)
  | Not(expr)
  | Ref(schemaID)
  | DynamicRef(reference)
  | Annotated(expr, evaluationAnnotations)
```

The distinction among `AnyOf`, `ExactlyOne`, and `All` must remain explicit.

```text
allOf  -> All
anyOf  -> AnyOf
oneOf  -> ExactlyOne
not    -> Not
```

Flattening `oneOf` into an ordinary union before proving branch disjointness is unsound.

---

## 6. JSON-kind abstraction

Each expression should carry an abstract set of possible JSON kinds.

```go
type KindSet uint8

const (
    KindNull KindSet = 1 << iota
    KindBoolean
    KindNumber
    KindString
    KindArray
    KindObject
)

const KindAny = KindNull |
    KindBoolean |
    KindNumber |
    KindString |
    KindArray |
    KindObject
```

`integer` is best modeled as a numeric-domain refinement rather than a separate JSON syntactic kind:

```go
type NumericDomain uint8

const (
    AnyNumber NumericDomain = iota
    IntegerOnly
    NonIntegerOnly
)
```

### 6.1 Basic inference

```text
kinds(true)  = all JSON kinds
kinds(false) = empty

kinds(type: "string") = {string}
kinds(type: ["string", "number"]) = {string, number}

kinds(const: v) = {kind(v)}
kinds(enum: values) = union(kind(v))

kinds(minLength: n) = all JSON kinds
kinds(minimum: n) = all JSON kinds
kinds(required: [...]) = all JSON kinds
kinds(properties: {...}) = all JSON kinds
```

Boolean composition:

```text
kinds(All(A, B)) = kinds(A) ∩ kinds(B)
kinds(AnyOf(A, B)) = kinds(A) ∪ kinds(B)
```

For `Not`, kind complement is exact only when the operand accepts or rejects whole kinds. Otherwise a conservative result is required.

---

## 7. Representation IR

The representation IR expresses what can be mapped to Go. It is a **storage** decision,
not a check: it does not participate in acceptance (§4.1), and is bound to it only by the
side-condition that it must be able to hold everything the plan accepts (§24.2).

Every applicator slot carries a whole `CompilationPlan`, never a bare `Representation`. A
subschema is never merely a type — any subschema may carry validation, dispatch and
references of its own — so a slot typed as `Representation` cannot express
`{"properties": {"a": {"type": "string", "minLength": 5}}}` at all.

```go
type Representation interface {
    isRepresentation()
}

type AnyRepresentation struct{}
type NeverRepresentation struct{}

type PrimitiveRepresentation struct {
    Kind    JSONKind
    Numeric NumericDomain
    Format  string
}

type ObjectRepresentation struct {
    // Fields is ordered, and the order is the source schema's (§7.2).
    Fields       []FieldRepresentation
    // Additional is the plan for every property no field and no pattern rule covers.
    // nil means such properties are not stored; it is a statement about storage and
    // never rejects. `additionalProperties: false` is NeverRepresentation, and an
    // absent `additionalProperties` is AnyRepresentation.
    Additional   *CompilationPlan
    PatternRules []PatternFieldRepresentation
}

type FieldRepresentation struct {
    Name     string
    Plan     CompilationPlan
    Presence PresenceMode
    Nullable bool
}

type ItemRepresentation struct {
    Plan CompilationPlan
}

type PatternFieldRepresentation struct {
    Pattern string
    Plan    CompilationPlan
}

type ArrayRepresentation struct {
    Prefix []ItemRepresentation
    // Rest describes items beyond the prefix. A zero Rest means there are none: unlike
    // a nil Additional, this one is load-bearing, because a tuple of fixed length is
    // what the schema said.
    Rest   ItemRepresentation
}

type UnionRepresentation struct {
    Alternatives []Representation
}

type RecursiveRepresentation struct {
    Name string
    Body Representation
}

type ReferenceRepresentation struct {
    Name string
}
```

### 7.1 Presence and nullability

These are independent:

```text
property absent
property present with null
property present with a non-null value
```

A pointer does not always distinguish all three states.

Possible wrappers:

```go
type Optional[T any] struct {
    Value T
    Set   bool
}

type Nullable[T any] struct {
    Value T
    Null  bool
}

type OptionalNullable[T any] struct {
    Value T
    Set   bool
    Null  bool
}
```

Generator policy may choose pointers where loss of the absent/null distinction is acceptable, but the semantic IR should preserve the distinction.

### 7.2 Field order

`Fields` is a slice, and its order is the order the properties appear in the source
schema. Backends should generate struct fields in that order and must not re-sort them:
the order is not semantic, but it is the only thing that makes generated output stable
and reviewable. A backend needing lookup by name should build its own map.

### 7.3 Fidelity

A representation is not merely a Go type but a Go type at some **fidelity**:

```text
transparent   json.RawMessage, json.Number, map[string]RawMessage
              round-trips; everything about the instance remains inspectable

typed         string, int64, float64, struct{...}
              ergonomic; may lose precision, unknown keys, or the original spelling
```

Write \(\llbracket R\rrbracket\) for the set of JSON documents that survive
\(\text{decode}\) followed by \(\text{encode}\) unchanged as JSON values. Fidelity
has two independent aspects, and conflating them overstates the cost of the harder plans:

```text
inspection fidelity   what must be available while deciding
storage fidelity      what the Go type retains afterwards
```

A `PredicateDispatch` over two object branches needs the full property set to *decide*
which branch applies, and a typed struct to *store* the winner. That is a decoder-shape
cost, not a type-shape cost.

Fidelity is a property of a slot, and is the join of what the slot's own checks need and
what the dispatch at that slot needs in order to select. It is local — computable from the
slot — for every keyword except the annotation-dependent ones (`unevaluatedProperties`,
`unevaluatedItems`), whose requirement is determined by applicators elsewhere in the
schema and is why they sit where they do on the ladder (§4.2).

---

## 8. Validation IR

The validation plan is **total**: together with dispatch it defines the accepted set
(§4.1). Nothing about acceptance is delegated to the representation, so a check that the
instance is of the right kind must appear here rather than being implied by the Go type.

Checks should be explicit and kind-scoped.

```go
type ValidationPlan struct {
    Predicates []GuardedPredicate
}

type GuardedPredicate struct {
    Applicability KindSet
    Assert        bool
    Expression    PredicateExpr
}
```

`Applicability` is read in one of two ways, per §3.1:

```text
Assert == false   guard      instance kind outside the set -> contributes nothing
Assert == true    assertion  instance kind outside the set -> rejects
```

`type: "string"` is `{Applicability: {string}, Assert: true}`. `minLength: 5` is
`{Applicability: {string}, Assert: false, Expression: CodePointLength(Current) >= 5}`.

Examples:

```text
minLength: 5
    applicability = {string}
    expression = CodePointLength(Current) >= 5

required: ["name"]
    applicability = {object}
    expression = HasProperty(Current, "name")

uniqueItems: true
    applicability = {array}
    expression = PairwiseJSONDistinct(Current)
```

A schema with no explicit `type` may therefore receive an `any` representation plus guarded validation.

### 8.1 Plan-valued predicates

Not every check is a flat, value-local expression. Some carry a whole subschema, and
`PredicateExpr` must have variants able to hold a `CompilationPlan`:

```text
ShapePredicate{Schema}      an object or array shape written without a sibling `type`,
                            and the members of an intersection a reference cannot absorb
                            (§11.5)

NegationPredicate{Schema}   a residual `not` that survives normalization (§11.8)

ContainsPredicate{Schema, Min, Max}
                            `contains` with its count bounds (§13.5)
```

A backend runs `Schema` against the instance and takes its verdict; for
`NegationPredicate` it takes the inverse. Without these, a shape keyword lacking a
sibling `type` has nowhere to go and is silently dropped.

### 8.2 Approximation polarity

`NegationPredicate` inverts the direction in which an unenforceable check is safe, and
this binds the **consumer**, not only the compiler.

Everywhere else, a check a backend cannot enforce must resolve to *accept*: that widens
the accepted set, which §24.1 permits. Underneath a negation the same choice narrows it,
which §24.1 forbids. A backend that cannot enforce a check inside a negated subplan must
therefore resolve it to *accept the enclosing negation*, never to "the check did not
fire".

Correspondingly, a compiler may only emit `NegationPredicate` over a subplan it has
established is exact. Negating an over-approximation yields an under-approximation.

---

## 9. Dispatch IR

Dispatch should be represented independently from schema resolution.

```go
type DispatchPlan interface {
    isDispatchPlan()
}

type NoDispatch struct{}

type KindDispatch struct {
    Cases map[JSONKind]CompilationPlan
}

type LiteralDispatch struct {
    Cases map[ComparableJSONValue]CompilationPlan
}

type PropertyDispatch struct {
    Property string
    Cases    map[ComparableJSONValue]CompilationPlan
}

type PresenceDispatch struct {
    Property string
    Present  CompilationPlan
    Absent   CompilationPlan
}

type PredicateCountDispatch struct {
    Branches []CompilationPlan
    Minimum  int
    Maximum  int
}
```

For `oneOf`, generic fallback is:

```text
minimum matches = 1
maximum matches = 1
```

For `anyOf`:

```text
minimum matches = 1
maximum matches = unbounded
```

---

## 10. Resolution IR

```go
type ResolutionPlan interface {
    isResolutionPlan()
}

type FullyResolved struct{}

type StaticReferenceGraph struct {
    Definitions map[SchemaID]CompilationPlan
}

type DynamicReferenceGraph struct {
    StaticDefinitions map[SchemaID]CompilationPlan
    DynamicAnchors     map[string][]SchemaID
}
```

### 10.1 `$ref`

Resolve URI references, `$id`, JSON Pointer fragments, and `$anchor` during schema loading or generation. Ordinary recursive references become named recursive Go types where possible.

A reference contributes a **name**, not a structure. A plan built for a reference is
therefore planned from the reference's identity alone, and knows nothing about what its
target turned out to be.

That makes any property derived from a plan's shape ambiguous across a reference, and the
document must be read as choosing one of two meanings each time:

```text
node-local     a property of this plan alone, where a reference leaf contributes nothing
closure        a property of the plan together with every target reachable from it
```

The two disagree exactly where a reference's target is worse than the reference looks. A
whole-document result is a closure property: it must take the worst over the root and
every reachable target, or it reports a quality the document does not have. A query made
*within* the compilation of a single schema sees the node-local value, and must resolve
the target itself if it needs the closure. Neither reading is wrong; using one where the
other is meant is.

### 10.2 `$dynamicRef`

A dynamic reference can select a target according to dynamic scope accumulated through reference traversal. It belongs in `DynamicSchemaResolution` unless analysis proves that every reachable dynamic binding yields the same target.

---

## 11. Basic keyword conversion

### 11.1 Boolean schemas

```text
true  -> Any
false -> Never
```

### 11.2 `type`

```text
type: "null"    -> KindSet({null})
type: "boolean" -> KindSet({boolean})
type: "number"  -> KindSet({number})
type: "integer" -> All(KindSet({number}), IsInteger)
type: "string"  -> KindSet({string})
type: "array"   -> KindSet({array})
type: "object"  -> KindSet({object})
```

For a type array:

```text
type: [T1, ..., Tn]
    -> Kind/type union of T1 ... Tn
```

A type array is already a finite static kind assertion. It is not generic branch validation.

### 11.3 `const`

```text
const: v
    -> Literal(v)
```

It can contribute both:

- representation information;
- an equality predicate.

### 11.4 `enum`

```text
enum: [v1, ..., vn]
    -> AnyOf(Literal(v1), ..., Literal(vn))
```

A finite scalar enum can generate a named Go type and constants.

### 11.5 `allOf`

\[
C(\operatorname{allOf}(A_1,\dots,A_n))
=
\bigcap_i C(A_i)
\]

```text
allOf -> All
```

The intersection is unordered, and **no member may be dropped for being second**. A member
that compiles to a reference contributes its name rather than its structure (§10.1), so
the intersection cannot always be performed structurally. Where it cannot, it is realized
as the reference plus a residual obligation over the remaining members, carried as a
`ShapePredicate` (§8.1). Keeping the first member's representation and discarding the rest
is not a widening: it drops the discarded members' validation too, and §24.1 forbids it.

### 11.6 `anyOf`

\[
C(\operatorname{anyOf}(A_1,\dots,A_n))
=
\bigcup_i C(A_i)
\]

```text
anyOf -> AnyOf
```

### 11.7 `oneOf`

\[
C(\operatorname{oneOf}(A_1,\dots,A_n))
=
\{x \mid |\{i : x \models A_i\}|=1\}
\]

```text
oneOf -> ExactlyOne
```

Equivalent formula:

\[
\bigcup_i
\left(
C(A_i)
\cap
\bigcap_{j \ne i}\neg C(A_j)
\right)
\]

This formula is exact but should not normally be expanded eagerly.

### 11.8 `not`

```text
not: A -> Not(A)
```

Complement elimination is attempted during normalization. Otherwise it remains a residual predicate.

### 11.9 `if` / `then` / `else`

Let:

```text
P = compile(if)
T = compile(then or true)
E = compile(else or true)
```

Then:

\[
(P \cap T) \cup (\neg P \cap E)
\]

IR:

```text
AnyOf(
    All(P, T),
    All(Not(P), E)
)
```

The branches are known statically. This is instance-directed validation or dispatch, not dynamic schema resolution.

### 11.10 Regular expressions

`pattern` and `patternProperties` are **ECMA-262** regular expressions, applied as an
unanchored search rather than a full match.

This is not the dialect Go's standard `regexp` implements. RE2 rejects lookaround and
backreferences outright, and silently disagrees elsewhere — its `\s` is `[\t\n\f\r ]`,
which does not match U+00A0, U+2003, U+2029, U+FEFF or VT. A compiler that reaches for the
standard library therefore does not merely fail loudly on exotic patterns; it quietly
computes a different accepted set for ordinary ones.

Every component that interprets a pattern — the validator, and any static analysis that
intersects `patternProperties` schemas into a named property (§12.3) — must use the same
ECMA-262 engine, or say which constructs it cannot decide and treat them as unenforceable
in the direction §24.1 requires.

---

## 12. Object keywords

### 12.1 `properties`

Under a guaranteed object context, `properties` contributes field representations.

Without a guaranteed object context, it contributes only an object-guarded child-validation rule.

```json
{
  "properties": {
    "name": { "type": "string" }
  }
}
```

must not automatically become a Go struct because the schema accepts all non-object JSON values.

### 12.2 `required`

`required` is an object-applicable presence predicate. It does not imply `"type": "object"`.

It may influence field representation:

```text
required + non-null -> ordinary field may be possible
optional + non-null -> Optional[T] or pointer policy
required + nullable -> Nullable[T]
optional + nullable -> OptionalNullable[T]
```

### 12.3 `patternProperties`

Every matching pattern applies, and multiple matching schemas are intersected.

```text
constraintsFor(name):
    result = []

    if name in properties:
        result += properties[name]

    for pattern, schema in patternProperties:
        if pattern matches name:
            result += schema

    if result is empty:
        result += additionalProperties

    return All(result)
```

Pattern properties generally imply a map-like or hybrid struct-plus-map representation and runtime name dispatch.

### 12.4 `additionalProperties`

It applies only to names not matched by `properties` or `patternProperties`.

A closed static struct is most natural when:

```json
{
  "additionalProperties": false
}
```

and the allowed property-name set is finite.

### 12.5 `propertyNames`

This is a predicate over every object key, interpreted as a JSON string. It usually remains validation unless its accepted name language can be used to generate a finite field set.

### 12.6 `dependentRequired`

For a dependency \(p \to \{q_1,\dots,q_n\}\):

\[
\operatorname{Has}(p)
\implies
\bigwedge_i \operatorname{Has}(q_i)
\]

It is a residual presence predicate, though finite union expansion is possible.

### 12.7 `dependentSchemas`

For \(p \to S\):

\[
\operatorname{Has}(p)
\implies
x \models S
\]

Equivalent form:

\[
\neg \operatorname{Has}(p)
\lor
(\operatorname{Has}(p) \land C(S))
\]

This is instance dispatch by property presence.

### 12.8 `minProperties` and `maxProperties`

These are cardinality predicates. They can produce contradiction checks during normalization but normally remain validation.

---

## 13. Array keywords

### 13.1 `prefixItems`

Contributes tuple-prefix representation:

```text
prefixItems: [A, B]
    -> prefix [representation(A), representation(B)]
```

### 13.2 `items`

Contributes the representation and validation for elements after the tuple prefix.

### 13.3 `minItems` and `maxItems`

These are length predicates, though fixed bounds can simplify tuple and rest representations.

### 13.4 `uniqueItems`

A relational predicate among all array elements:

\[
\forall i \ne j: a_i \ne_{\text{JSON}} a_j
\]

It generally requires runtime validation.

### 13.5 `contains`

Let:

\[
N_S(a)=|\{i \mid a_i \models S\}|
\]

Then `contains`, `minContains`, and `maxContains` impose bounds on \(N_S(a)\).

This is normally runtime validation. If the array is finitely bounded, exhaustive positional expansion is theoretically possible but can be exponential.

---

## 14. Annotation-dependent validation

`unevaluatedProperties` and `unevaluatedItems` cannot be handled by a purely local syntax-directed conversion.

Compilation must preserve successful evaluation annotations.

```go
type EvaluationAnnotations struct {
    EvaluatedProperties PropertySetExpr
    EvaluatedItems      IndexSetExpr
}
```

A branch-sensitive result is needed:

```go
type AnnotatedCase struct {
    Expr        Expr
    Annotations EvaluationAnnotations
}
```

Different successful `anyOf`, `oneOf`, conditional, or reference paths can produce different evaluated-location sets.

Applying `unevaluatedProperties: U` means:

```text
for every existing object property not included in the
successful case's evaluated-property set:
    validate the property's value against U
```

This capability is classified as `EvaluationStateValidation` unless annotation elimination produces a manageable static form.

---

## 15. Normalization strategy

Normalization should occur between semantic compilation and Go representation planning.

```text
JSON Schema
    -> exact semantic IR
    -> kind and constraint analysis
    -> normalized dispatch/validation IR
    -> Go representation planning
    -> code generation
```

Code generation should not be responsible for discovering fundamental schema equivalences.

### 15.1 Core rewrite rules

```text
All() -> Any
AnyOf() -> Never
ExactlyOne() -> Never

All(A) -> A
AnyOf(A) -> A
ExactlyOne(A) -> A

All(..., Never, ...) -> Never
AnyOf(..., Never, ...) -> remove Never
ExactlyOne(..., Never, ...) -> remove Never
```

Idempotence:

```text
All(A, A) -> A
AnyOf(A, A) -> A
```

But:

```text
ExactlyOne(A, A) -> Never
```

because every value satisfying `A` satisfies two branches.

### 15.2 Subsumption rules

If \(A \subseteq B\):

\[
A \land B = A
\]

\[
A \lor B = B
\]

For exactly one:

\[
ExactlyOne(A,B)=B\setminus A
\]

Example:

```json
{
  "oneOf": [
    { "type": "string" },
    { "type": "string", "minLength": 5 }
  ]
}
```

normalizes to:

```text
string with length < 5
```

Similarly:

```json
{
  "oneOf": [
    { "type": "number" },
    { "type": "integer" }
  ]
}
```

normalizes to:

```text
non-integral number
```

### 15.3 Disjoint `oneOf`

If all branches are pairwise disjoint:

\[
ExactlyOne(A_1,\dots,A_n)
=
AnyOf(A_1,\dots,A_n)
\]

Kind disjointness is a sufficient proof:

\[
Kinds(A_i)\cap Kinds(A_j)=\varnothing
\implies
A_i\cap A_j=\varnothing
\]

Example:

```json
{
  "oneOf": [
    { "type": "string" },
    { "type": "number" }
  ]
}
```

normalizes to static kind dispatch.

### 15.4 Common intersections pushed into alternatives

For ordinary union:

\[
T \cap (A \cup B)
=
(T\cap A)\cup(T\cap B)
\]

For exact-one:

\[
T \cap ExactlyOne(A_1,\dots,A_n)
=
ExactlyOne(T\cap A_1,\dots,T\cap A_n)
\]

This rule is especially useful for sibling `type`, `allOf`, object constraints, and `oneOf`.

### 15.5 Remove impossible branches

After pushing common constraints into branches:

```text
normalize each branch
remove Never branches
recompute disjointness
recompute discriminators
```

---

## 16. `type` arrays combined with combinators

### 16.1 `type` array with `oneOf`

```json
{
  "type": ["string", "number"],
  "oneOf": [
    { "type": "string", "minLength": 3 },
    { "type": "number", "minimum": 0 }
  ]
}
```

Meaning:

\[
(String \cup Number)
\cap
ExactlyOne(
    String_{\ge3},
    Number_{\ge0}
)
\]

The branches are kind-disjoint, so this becomes:

```text
String(minLength = 3)
|
Number(minimum = 0)
```

It requires static kind dispatch and branch-local validation.

### 16.2 Outer type removes a branch

```json
{
  "type": ["string", "number"],
  "oneOf": [
    { "type": "string" },
    { "type": "boolean" }
  ]
}
```

Push the outer type into each branch:

```text
(string | number) & string  = string
(string | number) & boolean = Never
```

Result:

```text
string
```

### 16.3 Outer type changes overlap

```json
{
  "type": "number",
  "oneOf": [
    { "minimum": 0 },
    { "maximum": 10 }
  ]
}
```

The outer type makes both branch predicates unconditional numeric predicates.

Exactly one succeeds when:

\[
x < 0 \lor x > 10
\]

The result is a number representation plus a residual range predicate.

---

## 17. Combinator nesting

Nested combinators must preserve grouping until a proof permits flattening.

### 17.1 `oneOf(anyOf(A,B), C)`

Meaning:

\[
ExactlyOne(A\cup B,\ C)
\]

This is not generally equivalent to:

\[
ExactlyOne(A,B,C)
\]

If an instance satisfies both \(A\) and \(B\), but not \(C\), the outer schema succeeds while the flattened exact-one expression fails.

Flattening is safe only when the resulting alternatives are pairwise disjoint.

### 17.2 `oneOf(allOf(A,B), C)`

Meaning:

\[
ExactlyOne(A\cap B,\ C)
\]

First simplify `All(A,B)`, then analyze exact-one disjointness or subsumption.

### 17.3 Sibling `oneOf` and `anyOf`

```json
{
  "oneOf": [A, B],
  "anyOf": [C, D]
}
```

Meaning:

\[
ExactlyOne(A,B)\cap(C\cup D)
\]

A useful factored form is:

\[
ExactlyOne(
    A\cap(C\cup D),
    B\cap(C\cup D)
)
\]

Do not expand eagerly into a full disjunctive normal form.

### 17.4 Sibling `oneOf` and `allOf`

```json
{
  "oneOf": [A, B],
  "allOf": [C, D]
}
```

Meaning:

\[
ExactlyOne(A,B)\cap C\cap D
\]

Push the common constraints into each exact-one branch:

\[
ExactlyOne(
    A\cap C\cap D,
    B\cap C\cap D
)
\]

### 17.5 `anyOf(oneOf(A,B), C)`

Meaning:

\[
ExactlyOne(A,B)\cup C
\]

This is not `oneOf(A,B,C)`. A value satisfying `C` and one of `A` or `B` is valid under the outer `anyOf`.

### 17.6 Multiple exact-one groups

Keep them factored:

```text
All(
    ExactlyOne(A, B),
    ExactlyOne(C, D)
)
```

Perform kind partitioning and discriminator analysis before considering Cartesian expansion.

---

## 18. Static discriminator analysis

A schema can be statically dispatched when branches are distinguishable using finite structural observations.

Preferred discriminator classes:

1. JSON kind;
2. literal value;
3. required literal object property;
4. finite enum object property;
5. tuple-position literal;
6. required versus forbidden property;
7. non-overlapping numeric intervals;
8. provably disjoint string languages.

### 18.1 Kind dispatch

```text
switch JSON kind:
    string -> branch A
    number -> branch B
    otherwise -> reject
```

### 18.2 Property dispatch

For branches requiring:

```json
{ "kind": { "const": "circle" } }
```

and:

```json
{ "kind": { "const": "rectangle" } }
```

generate:

```text
inspect "kind"
"circle"    -> Circle
"rectangle" -> Rectangle
other       -> reject
```

### 18.3 Partial dispatch

Example:

```json
{
  "oneOf": [
    { "type": "string", "pattern": "^a" },
    { "type": "string", "pattern": "^b" },
    { "type": "number" }
  ]
}
```

Normalized plan:

```text
KindDispatch {
    number:
        direct number branch

    string:
        ExactlyOne(
            pattern "^a",
            pattern "^b"
        )
}
```

Only the overlapping same-kind partition requires predicate-count validation.

---

## 19. Recursive schemas

Do not reject all reference cycles.

A recursive schema can describe finite JSON trees of unbounded depth:

```text
Node = null | { value: number, next: Node }
```

The reference graph should be analyzed by strongly connected components.

```text
for each recursive SCC:
    if every cycle crosses an instance-descent edge:
        classify as guarded recursive
    else:
        classify as unguarded semantic recursion
```

Instance-descent edges include traversal into:

```text
object property
array item
```

Guarded recursion can normally generate recursive Go types. Unguarded recursion may require a semantic validator or may be rejected by a configured structural subset.

---

## 20. Go lowering

### 20.1 Direct primitive

Schema:

```json
{ "type": "string" }
```

Generated representation:

```go
type Value string
```

### 20.2 Primitive plus validation

Schema:

```json
{
  "type": "string",
  "minLength": 3
}
```

Generated representation:

```go
type Value string

func (v Value) Validate() error {
    if utf8.RuneCountInString(string(v)) < 3 {
        return errors.New("must contain at least 3 Unicode code points")
    }
    return nil
}
```

### 20.3 Keyword without type restriction

Schema:

```json
{ "minLength": 3 }
```

Possible representation:

```go
type Value any
```

Validator:

```go
func ValidateValue(v any) error {
    s, ok := v.(string)
    if !ok {
        return nil
    }

    if utf8.RuneCountInString(s) < 3 {
        return errors.New("string must contain at least 3 Unicode code points")
    }

    return nil
}
```

### 20.4 Static kind union

Schema:

```json
{
  "oneOf": [
    { "type": "string" },
    { "type": "number" }
  ]
}
```

Possible generated wrapper:

```go
type Value struct {
    Kind   ValueKind
    String string
    Number json.Number
}
```

Custom decoding dispatches by the first JSON token. It does not trial-validate both branches.

### 20.5 Tagged object union

```go
type Shape interface {
    isShape()
}

type Circle struct {
    Kind   string  `json:"kind"`
    Radius float64 `json:"radius"`
}

type Rectangle struct {
    Kind   string  `json:"kind"`
    Width  float64 `json:"width"`
    Height float64 `json:"height"`
}
```

A custom unmarshaller reads the discriminator and decodes the corresponding concrete type.

### 20.6 Predicate dispatch

For overlapping branches, generate branch validators and count matches:

```go
matches := 0

if validateBranchA(raw) == nil {
    matches++
}
if validateBranchB(raw) == nil {
    matches++
}

if matches != 1 {
    return ErrOneOf
}
```

This is a fallback after static-dispatch analysis fails.

### 20.7 Dynamic resolution

For unresolved `$dynamicRef`, generated validation needs:

```text
schema registry
dynamic-anchor stack
scope-sensitive target lookup
runtime validation against resolved target
```

A broad representation such as `json.RawMessage` may be appropriate when the resolved target can change the representation itself.

---

## 21. Main compilation algorithm

```text
compile(schema, context, dynamicScope) -> CompilationPlan:
    semanticExpr = compileSemanticExpr(schema, context, dynamicScope)

    annotatedExpr =
        compileEvaluationAnnotations(
            semanticExpr,
            schema,
            context,
            dynamicScope
        )

    normalizedExpr =
        normalize(
            annotatedExpr,
            expansionBudget = context.expansionBudget
        )

    representation =
        inferRepresentation(normalizedExpr, context.goPolicy)

    validation =
        extractResidualValidation(normalizedExpr, representation)

    dispatch =
        buildDispatchPlan(normalizedExpr, representation)

    resolution =
        buildResolutionPlan(normalizedExpr, context)

    capability =
        classify(
            representation,
            validation,
            dispatch,
            resolution
        )

    return CompilationPlan(
        Representation = representation,
        Validation = validation,
        Dispatch = dispatch,
        Resolution = resolution,
        Capability = capability
    )
```

### 21.1 Semantic compilation

```text
compileSemanticExpr(schema):
    if schema == true:
        return Any

    if schema == false:
        return Never

    siblings = []

    for each recognized assertion/applicator:
        siblings += compileKeyword(keyword)

    return All(siblings)
```

Type-specific constraints are emitted as guarded predicates unless an enclosing kind restriction makes the guard redundant.

### 21.2 Normalization loop

```text
normalize(expr):
    repeat until stable or budget exhausted:
        flatten associative nodes
        simplify Any and Never
        intersect kind sets
        simplify literal constraints
        push common intersections into alternatives
        remove impossible alternatives
        prove subsumption
        prove pairwise disjointness
        detect kind discriminators
        detect literal/property discriminators
        simplify exact-one expressions
        merge equivalent representations
```

If expansion exceeds a budget, preserve a factored predicate-dispatch form rather than generating an exponential IR.

---

## 22. Classification algorithm

```text
classify(representation, validation, dispatch, resolution):
    if resolution requires dynamic-scope lookup:
        return DynamicSchemaResolution

    if validation requires evaluated-location annotations:
        return EvaluationStateValidation

    if dispatch requires branch validation or match counting:
        return PredicateDispatch

    if dispatch is finite and structurally decidable:
        return StaticDispatch

    if validation has a check the representation does not discharge:
        return GoTypeWithValidation

    if representation can hold the accepted set:
        return DirectGoType

    return Unsupported
```

The validation test is deliberately not "validation is empty". Under §4.1 the validation
plan is total, so it is never empty: `{"type": "string"}` carries a kind assertion even
though the Go type `string` already makes it unfalsifiable. `DirectGoType` therefore means
*every check is discharged by the chosen representation*, not *there are no checks*.

This is the price of separating acceptance from storage (§4.1). A plan states its
constraints once, semantically; a lowering then observes that some of them cost nothing
against the type it picked. Reading emptiness instead would put the compiler back in the
business of deciding what a Go type enforces, which §24.3 says it cannot do.

Classification is recursive. The capability of an object is at least the maximum capability of:

```text
all fields
additional-property representation
pattern-property rules
local validation
local dispatch
local resolution
```

---

## 23. Optimization policy

Optimization belongs primarily in the schema normalization layer, not in the Go backend.

The backend may make target-specific representation choices, but it should receive an already analyzed plan.

### Required normalization optimizations

```text
kind restriction propagation
impossible-branch elimination
subsumption
pairwise disjointness
static discriminator discovery
partial kind partitioning
common-constraint propagation
redundant combinator elimination
recursive-type naming
```

### Optional advanced analyses

```text
regular-language inclusion/disjointness for patterns
numeric interval algebra
property-name language analysis
finite-domain enumeration
annotation elimination
dynamic-reference target equivalence
```

---

## 24. Soundness requirements

Three sets, all of them sets of JSON documents:

```text
⟦S⟧   accepted by the schema
⟦P⟧   accepted by the plan            = V(P) ∩ ⟦D(P)⟧            (§4.1)
⟦R⟧   surviving decode-then-encode    unchanged as a JSON value  (§7.3)
```

`⟦P⟧` is defined without reference to Go. `⟦R⟧` is a property of a *lowering*, not of the
plan.

### 24.1 The acceptance invariant

\[
\llbracket S\rrbracket \subseteq \llbracket P\rrbracket
\]

A plan may accept more than its schema; it may never accept less. Rejecting a valid
instance is the single error this design admits no excuse for, at any level of support.
Where the containment is equality the plan is **exact**; where it is strict the plan admits
values the schema rejects, and must carry a diagnostic saying which construct it failed to
enforce.

### 24.2 Representation adequacy, and declared narrowing

The representation must be able to hold what the plan accepts:

\[
\llbracket P\rrbracket \subseteq \llbracket R\rrbracket
\]

**Silent** violation of this is forbidden. A violation that is *declared* is permitted, and
is sometimes the right engineering choice.

JSON numbers are arbitrary precision, so no fixed-width Go numeric type can hold all of
`{"type": "integer"}`; representing it as `int64` is nonetheless a reasonable default,
because the alternative penalizes every ordinary schema for an input that will not occur.
The requirement is therefore not that narrowing never happens, but that it is never
invisible: a representation may exclude part of `⟦P⟧` only if the excluded region is named
in a diagnostic.

Where the schema bounds the value the exclusion can be discharged statically:

```text
{"type":"integer"}                 -> int64, declared narrowing
{"type":"integer","maximum":1000}  -> int64, provably adequate
```

Note this is soundness of the *lowering*, not of the plan. A narrowed representation does
not change what the plan accepts; it changes what the generated program can carry.

### 24.3 Discharging a plan

A backend may evaluate a plan against the raw JSON document, which is always correct, or
decode into `R` and validate the decoded value, which is faster. The second is admissible
exactly when decoding is a **JSON-value isomorphism on `⟦P⟧`**: total, and injective up to
JSON-value equality.

Each failure of that condition has its own remedy, and they are not interchangeable:

```text
decode discards what a check inspects   -> evaluate against raw JSON
   e.g. the property set a dispatch needs to select a branch

decode loses precision or spelling      -> widen the type, or declare the narrowing
   e.g. int64 for an unbounded integer

decode is not injective on ⟦P⟧          -> evaluate against raw JSON
   e.g. uniqueItems over items that decode to a lossy struct: two JSON-distinct
        items become Go-equal, and the check rejects an array the schema accepts
```

The third is the dangerous one, because a *storage* choice made below a check silently
turns into a violation of §24.1 above it.

Since this condition is decided by the lowering, the compiler cannot evaluate it. What the
compiler owes the backend is the requirements the plan places on it (§25); what the backend
owes in return is to discharge them, and only then may it claim exactness.

---

## 25. Recommended public result API

```go
type Result struct {
    Plan         CompilationPlan
    Capability   CapabilityLevel
    Requirements Requirements
    Diagnostics  []Diagnostic
}
```

### 25.1 Why there is no `Exactness` field

Earlier revisions of this document returned an ordered `Exactness` alongside
`Capability`. That was a mistake, and the reason is §24.3: whether the generated program
reproduces the schema's accepted set depends on the lowering — how integers are sized,
whether unknown properties are retained, which regex engine runs — and the compiler does
not choose any of that. A compiler-side exactness value is a claim it is not in a position
to make.

The symptom was that the two middle rungs were never justified. `ExactWithValidation` and
`SoundOverApproximation` were both defined by the same biconditional and so could not be
told apart; measured against the JSON-Schema-Test-Suite, the middle of the ladder was
essentially unpopulated while the plans it was supposed to describe sat at the ends.

What replaces it is a statement of what the plan *requires of its consumer*:

```go
type Requirements struct {
    // RawEvaluation reports slots whose checks cannot be discharged by decoding
    // and validating, because decoding is not a JSON-value isomorphism there (§24.3).
    RawEvaluation []Location

    // UnboundedNumeric reports numeric slots the schema does not bound, so a
    // fixed-width Go type narrows them and must declare it (§24.2).
    UnboundedNumeric []Location

    // JSONEquality reports checks defined by JSON-value equality rather than by
    // equality of the decoded Go value, `uniqueItems` above all.
    JSONEquality []Location

    // ECMARegex reports patterns whose meaning differs under RE2 (§11.10).
    ECMARegex []Location

    // EvaluationTracking reports slots needing evaluated-location annotations.
    EvaluationTracking []Location
}
```

`Capability` remains, and remains an ordering: it is the inspection floor of §4.2, which
is a property of the plan and so genuinely is the compiler's to report. Exactness is what
a backend concludes once it has discharged the requirements above.

Diagnostics should explain why a stronger conversion was not possible:

```text
oneOf branches overlap on string instances
patternProperties requires runtime key matching
uniqueItems requires relational validation
unevaluatedProperties requires evaluated-property tracking
$dynamicRef target depends on dynamic scope
optional nullable property requires a three-state representation
```

---

## 26. Open questions

Recorded rather than answered. Each is a place where this document deliberately stops.

### 26.1 Normalization that manufactures negation

Subsumption (§15.2) rewrites `ExactlyOne(A, B)` with \(\llbracket B\rrbracket \subseteq
\llbracket A\rrbracket\) into `All(A, Not(B))`. The rewrite is correct, but `Not` is the
only node whose approximation polarity inverts (§8.2), so aggressive normalization
manufactures the one construct that is hardest to keep sound.

The un-normalized form needs no negation at all: a match-count dispatch over the original
branches is sound by construction, and lands at the same capability level. What the rewrite
buys is the representation — `A`'s named type instead of a union of both branches.

So the question is whether subsumption should remain an IR **rewrite**, or become an input
to *lowering* — the fact that \(\llbracket B\rrbracket \subseteq \llbracket A\rrbracket\)
choosing the representation, without the constraint ever being restated as a negation. A
third option is a dedicated exclusion node that carries the intent, which a backend could
specialize on when the excluded branch is a kind test or a literal, instead of validating
and inverting a whole subplan.

### 26.2 Predicates and applicators

This document treats validation as one list of predicates, some of which happen to carry a
subplan (§8.1). An alternative is to separate them: predicates that check the current
instance, and applicators that select child locations and apply a plan there.

The argument for separating them is that evaluation annotations
(`unevaluatedProperties`, `unevaluatedItems`) are produced by applicators and by nothing
else, so making applicators explicit is what a future implementation of those keywords
would build on.

The argument against is that the distinction does not line up with fidelity, which is what
actually decides how a check is executed (§7.3). `minLength` under `properties.a` is an
applicator by structure yet runs fine on the decoded field, while `patternProperties` needs
the raw property set. Splitting on applicator-versus-predicate would therefore not,
by itself, answer the question it appears to answer.

### 26.3 How much of §24.3 belongs to the compiler

§24.3 assigns the isomorphism condition to the backend, because the lowering decides it.
But some of its inputs are visible to the compiler — that an integer is unbounded, that
`uniqueItems` needs JSON-value equality — which is why §25 reports them.

Where the line falls is unsettled. A compiler that knew a backend's lowering table could
discharge the condition itself and report a real exactness; a compiler that models nothing
about Go types keeps the separation clean but leaves every backend to redo the same
analysis. This document currently takes the second position, and does so partly because no
backend consumes the plan yet.

---

## 27. Design conclusions

1. JSON Schema should be compiled as a predicate language first and a structural language second.
2. Type-specific keywords are guarded predicates and do not imply their applicable type.
3. Representation, validation, dispatch, and schema resolution are independent compiler concerns.
4. `oneOf` is initially exact-one semantics, but often normalizes to ordinary union or static dispatch.
5. A `type` array is a static kind assertion and is especially useful for pruning combinator branches.
6. Nested `oneOf`, `anyOf`, and `allOf` must preserve grouping until equivalence is proved.
7. Static runtime dispatch is not dynamic schema resolution.
8. `$ref` recursion can often become ordinary recursive Go types.
9. `$dynamicRef` is the primary source of genuine runtime schema resolution.
10. `unevaluatedProperties` and `unevaluatedItems` require branch-sensitive evaluation annotations.
11. Go generation should consume an optimized semantic plan rather than directly walking JSON Schema syntax.
12. The normal fallback is a broad Go representation plus an exact residual validator, never an unsound narrow representation.

---

## 28. References

- JSON Schema Draft 2020-12 overview: <https://json-schema.org/draft/2020-12>
- JSON Schema Core, Draft 2020-12: <https://json-schema.org/draft/2020-12/json-schema-core>
- JSON Schema Validation, Draft 2020-12: <https://json-schema.org/draft/2020-12/json-schema-validation>
- Draft 2020-12 release notes: <https://json-schema.org/draft/2020-12/release-notes>
- JSON Schema Test Suite: <https://github.com/json-schema-org/JSON-Schema-Test-Suite>
