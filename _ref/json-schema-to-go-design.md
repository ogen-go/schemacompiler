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

### 4.1 Capability levels

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

The representation IR expresses what can be mapped to Go.

```go
type Representation interface {
    isRepresentation()
}

type AnyRepresentation struct{}
type NeverRepresentation struct{}

type PrimitiveRepresentation struct {
    Kind JSONKind
}

type ObjectRepresentation struct {
    Fields       map[string]FieldRepresentation
    Additional   AdditionalRepresentation
    PatternRules []PatternFieldRepresentation
}

type FieldRepresentation struct {
    Representation Representation
    Presence       PresenceMode
    Nullable       bool
}

type ArrayRepresentation struct {
    Prefix []Representation
    Rest   Representation
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

---

## 8. Validation IR

Residual checks should be explicit and kind-guarded.

```go
type ValidationPlan struct {
    Predicates []GuardedPredicate
}

type GuardedPredicate struct {
    Applicability KindSet
    Expression    PredicateExpr
}
```

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

    if validation is not empty:
        return GoTypeWithValidation

    if representation is exactly lowerable:
        return DirectGoType

    return Unsupported
```

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

A generated direct or validated Go representation must satisfy:

### Exact conversion

\[
x \models S
\iff
Decode_G(x)\text{ succeeds}
\land
Validate_G(x)\text{ succeeds}
\]

### Sound over-approximation

When exact representation is impossible, the Go type may contain additional values only if residual validation rejects them:

\[
\llbracket S\rrbracket
\subseteq
\llbracket G(S)\rrbracket
\]

and:

\[
x \models S
\iff
x \in \llbracket G(S)\rrbracket
\land
V(S,x)
\]

The compiler must not silently generate an under-approximate Go type that cannot represent a valid schema instance.

---

## 25. Recommended public result API

```go
type Exactness uint8

const (
    ExactPureRepresentation Exactness = iota
    ExactWithValidation
    SoundOverApproximation
    UnsupportedConversion
)

type Result struct {
    GoDeclarations []GoDeclaration
    Decoder        DecoderPlan
    Validator      ValidationPlan
    Resolver       ResolutionPlan
    Capability     CapabilityLevel
    Exactness      Exactness
    Diagnostics    []Diagnostic
}
```

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

## 26. Design conclusions

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

## 27. References

- JSON Schema Draft 2020-12 overview: <https://json-schema.org/draft/2020-12>
- JSON Schema Core, Draft 2020-12: <https://json-schema.org/draft/2020-12/json-schema-core>
- JSON Schema Validation, Draft 2020-12: <https://json-schema.org/draft/2020-12/json-schema-validation>
- Draft 2020-12 release notes: <https://json-schema.org/draft/2020-12/release-notes>
- JSON Schema Test Suite: <https://github.com/json-schema-org/JSON-Schema-Test-Suite>
