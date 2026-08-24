package schemacompiler_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/internal/dump"
	"github.com/ogen-go/schemacompiler/plan"
)

func compileNullable(t *testing.T, schema string) (res *schemacompiler.Result, dumped string) {
	t.Helper()
	res, err := schemacompiler.Compile(context.Background(), []byte(schema), schemacompiler.Options{})
	require.NoError(t, err)

	var out strings.Builder
	dump.Plan(&out, res.Plan)
	return res, out.String()
}

const nullablePets = `"$defs": {
	"Cat": {"type": "object", "required": ["kind"], "properties": {"kind": {"const": "cat"}}},
	"Dog": {"type": "object", "required": ["kind"], "properties": {"kind": {"const": "dog"}}}
}`

const ignoredNullable = "ignoring `nullable: true`"

// requireIgnored asserts res carries exactly one ignored-`nullable` warning.
func requireIgnored(t *testing.T, res *schemacompiler.Result) {
	t.Helper()
	var found int
	for _, d := range res.Diagnostics {
		if strings.Contains(d.Message, ignoredNullable) {
			require.Equal(t, plan.SeverityWarning, d.Severity)
			found++
		}
	}
	require.Equal(t, 1, found, "diagnostics: %v", res.Diagnostics)
}

// `nullable: true` admits null wherever a sibling `type` gives it something to widen.
func TestNullablePlan_AdmitsNull(t *testing.T) {
	for _, tt := range []struct {
		name   string
		schema string
	}{
		{"Scalar", `{"type": "string", "nullable": true}`},
		{"TypeArray", `{"type": ["string", "integer"], "nullable": true}`},
		{"Object", `{"type": "object", "properties": {"a": {"type": "string"}}, "nullable": true}`},
		{"Array", `{"type": "array", "items": {"type": "string"}, "nullable": true}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res, got := compileNullable(t, tt.schema)
			require.Contains(t, got, "Primitive null")
			require.Contains(t, got, "case null")
			require.Empty(t, res.Diagnostics)
		})
	}
}

// Without a `type` in the same Schema Object there is nothing for null to be added to
// (OAS 3.0.3 line 2335), so the keyword is inert — and says so.
func TestNullablePlan_IgnoredWithoutType(t *testing.T) {
	for _, tt := range []struct {
		name   string
		schema string
	}{
		{"Bare", `{"nullable": true}`},
		{"OneOf", `{"oneOf": [{"type": "string"}, {"type": "integer"}], "nullable": true}`},
		{"AnyOf", `{"anyOf": [{"type": "string"}, {"type": "integer"}], "nullable": true}`},
		{"AllOf", `{"allOf": [{"type": "string"}], "nullable": true}`},
		{"Ref", `{` + nullablePets + `, "$ref": "#/$defs/Cat", "nullable": true}`},
		{"Enum", `{"enum": ["a", "b"], "nullable": true}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res, got := compileNullable(t, tt.schema)
			require.NotContains(t, got, "Primitive null")
			require.NotContains(t, got, "case null")
			requireIgnored(t, res)
		})
	}
}

// A nullable property lowers to the design §7.1 shape: nullability on the field, the
// non-null value in the field's own representation.
func TestNullablePlan_Property(t *testing.T) {
	res, _ := compileNullable(t, `{
		"type": "object",
		"required": ["a"],
		"properties": {"a": {"type": "string", "nullable": true}}
	}`)

	obj, ok := res.Plan.Representation.(*plan.ObjectRepresentation)
	require.True(t, ok)

	field := mustField(t, obj, "a")
	require.Equal(t, plan.PresenceRequired, field.Presence)
	require.True(t, field.Nullable)
	require.Equal(t, &plan.PrimitiveRepresentation{Kind: plan.KindString}, field.Plan.Representation)
}

// Clause 2: `nullable` widens the `type` keyword and nothing else, so sibling constraints
// that disallow null go on disallowing it — with no compensation and no second guess.
func TestNullablePlan_SiblingsMayRejectNull(t *testing.T) {
	for _, tt := range []struct {
		name   string
		schema string
	}{
		{"Enum", `{"type": "string", "enum": ["a", "b"], "nullable": true}`},
		{"Const", `{"type": "string", "const": "a", "nullable": true}`},
		{
			"TypeWithDiscriminatedOneOf",
			`{` + nullablePets + `, "type": "object", "oneOf": [
				{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}
			], "discriminator": {"propertyName": "kind"}, "nullable": true}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res, got := compileNullable(t, tt.schema)
			require.NotContains(t, got, "Primitive null")
			require.NotContains(t, got, "case null")
			// The keyword applied; only the siblings rejected null, so nothing is ignored.
			require.Empty(t, res.Diagnostics)
		})
	}
}

// Nullability must not disturb discriminator dispatch at any of its three tiers: the same
// schema with and without `nullable` reaches the same tier and the same diagnostics.
func TestNullablePlan_DiscriminatorTiers(t *testing.T) {
	const proven = `"$defs": {
		"Cat": {"type": "object", "required": ["kind"], "properties": {"kind": {"const": "cat"}}},
		"Dog": {"type": "object", "required": ["kind"], "properties": {"kind": {"const": "dog"}}}
	}`
	const asserted = `"$defs": {
		"Cat": {"type": "object", "required": ["kind"], "properties": {"kind": {"type": "string"}, "meow": {"type": "boolean"}}},
		"Dog": {"type": "object", "required": ["kind"], "properties": {"kind": {"type": "string"}, "bark": {"type": "boolean"}}}
	}`
	const unusable = `"$defs": {
		"Cat": {"type": "object", "properties": {"kind": {"const": "cat"}}},
		"Dog": {"type": "object", "properties": {"kind": {"const": "dog"}}}
	}`

	for _, tt := range []struct {
		name     string
		defs     string
		mapping  string
		dispatch string
		assumed  bool
	}{
		{
			name:     "Declared",
			defs:     proven,
			dispatch: `PropertyDispatch property="kind" declared`,
		},
		{
			name:     "Asserted",
			defs:     asserted,
			mapping:  `, "mapping": {"cat": "#/$defs/Cat", "dog": "#/$defs/Dog"}`,
			dispatch: `PropertyDispatch property="kind"` + "\n",
			assumed:  true,
		},
		{
			name:     "Unusable",
			defs:     unusable,
			dispatch: "PredicateCountDispatch min=1 max=1",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			schema := `{` + tt.defs + `, "type": "object", "oneOf": [
				{"$ref": "#/$defs/Cat"}, {"$ref": "#/$defs/Dog"}
			], "discriminator": {"propertyName": "kind"` + tt.mapping + `}%s}`

			base, withoutNull := compileNullable(t, fmt.Sprintf(schema, ""))
			res, got := compileNullable(t, fmt.Sprintf(schema, `, "nullable": true`))

			require.Contains(t, withoutNull, tt.dispatch)
			require.Contains(t, got, tt.dispatch)
			require.Equal(t, tt.assumed, hasKind(res.Diagnostics, plan.DiagnosticAssumed))
			require.Equal(t, base.Diagnostics, res.Diagnostics)
			require.Equal(t, withoutNull, got)
		})
	}
}

// Widening the kind set leaves the schema's annotations exactly where they were, so the
// `nullable` spelling and the `type` array spelling compile to the same plan.
func TestNullablePlan_MatchesTypeArraySpelling(t *testing.T) {
	for _, tt := range []struct {
		name     string
		nullable string
		declared string
	}{
		{"Scalar", `"type": "string"`, `"type": ["string", "null"]`},
		{"Object", `"type": "object"`, `"type": ["object", "null"]`},
		{"Array", `"type": "array"`, `"type": ["array", "null"]`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const rest = `, "title": "T", "description": "D", "x-k": "V",
				"properties": {"inner": {"type": "string", "title": "I"}},
				"items": {"type": "integer", "title": "R"}`

			nullable, _ := compileNullable(t, `{"type": "object", "properties": {"a": {`+
				tt.nullable+`, "nullable": true`+rest+`}}}`)
			declared, _ := compileNullable(t, `{"type": "object", "properties": {"a": {`+
				tt.declared+rest+`}}}`)

			require.Equal(t, declared, nullable)

			obj, ok := nullable.Plan.Representation.(*plan.ObjectRepresentation)
			require.True(t, ok)

			field := mustField(t, obj, "a")
			require.True(t, field.Nullable)
			require.Equal(t, "T", field.Metadata.Title)
			require.Equal(t, "D", field.Metadata.Description)
			require.Equal(t, map[string]any{"x-k": "V"}, field.Metadata.Extensions)
		})
	}
}

// Array positions carry their own annotations, and `nullable` on an item widens that
// position's kind set rather than the array's.
func TestNullablePlan_ArrayPositions(t *testing.T) {
	res, got := compileNullable(t, `{
		"type": "array",
		"prefixItems": [{"type": "string", "nullable": true, "title": "P0"}],
		"items": {"type": "integer", "nullable": true, "title": "R"}
	}`)

	require.Empty(t, res.Diagnostics)

	arr, ok := res.Plan.Representation.(*plan.ArrayRepresentation)
	require.True(t, ok)
	require.Len(t, arr.Prefix, 1)
	require.Equal(t, "P0", arr.Prefix[0].Metadata.Title)
	require.Equal(t, "R", arr.Rest.Metadata.Title)
	require.Contains(t, got, "Primitive null")
}

// `nullable` on the array itself is the design §7.1 field shape, not a union in the item.
func TestNullablePlan_ArrayItself(t *testing.T) {
	res, _ := compileNullable(t, `{
		"type": "object",
		"properties": {"a": {"type": "array", "nullable": true, "title": "A", "items": {"type": "string"}}}
	}`)

	obj, ok := res.Plan.Representation.(*plan.ObjectRepresentation)
	require.True(t, ok)

	field := mustField(t, obj, "a")
	require.True(t, field.Nullable)
	require.Equal(t, "A", field.Metadata.Title)
	require.IsType(t, &plan.ArrayRepresentation{}, field.Plan.Representation)
}

// `nullable: false` never removes a null the document declared itself, and repeating an
// already-declared null changes nothing.
func TestNullablePlan_NeverRemovesNull(t *testing.T) {
	for _, schema := range []string{
		`{"type": ["string", "null"], "nullable": false}`,
		`{"type": ["string", "null"], "nullable": true}`,
	} {
		res, got := compileNullable(t, schema)
		require.Contains(t, got, "case null")
		require.Contains(t, got, "case string")
		require.Empty(t, res.Diagnostics)
	}
}

// A nullable `$ref` sibling is inert, so the reference is left alone: the target is not
// resolved into the referring plan and stays a named definition.
func TestNullablePlan_RefSibling(t *testing.T) {
	res, got := compileNullable(t, `{`+nullablePets+`, "$ref": "#/$defs/Cat", "nullable": true}`)

	requireIgnored(t, res)
	require.Equal(t, &plan.ReferenceRepresentation{Name: "/$defs/Cat"}, res.Plan.Representation)
	require.Contains(t, got, `definition "/$defs/Cat"`)
}

// Clause 2 does not promise null is rejected — only that the siblings decide. Where the
// `oneOf` branches assert no kind of their own they all accept null, so exactly-one fails
// it at runtime while the representation still admits it (design §24).
func TestNullablePlan_KindlessBranchesDeferToPredicateCount(t *testing.T) {
	_, got := compileNullable(t, `{
		"type": "object",
		"oneOf": [{"required": ["a"]}, {"required": ["b"]}],
		"nullable": true
	}`)

	require.Contains(t, got, "PredicateCountDispatch min=1 max=1")
	require.Contains(t, got, "Primitive null")
	require.Contains(t, got, "Required [a]")
	require.Contains(t, got, "Required [b]")
}

// A nullable self-reference stays a resolvable recursive definition rather than an
// uninhabited or unresolved one. The repo stops at `$ref` (#23), so a sibling kind set
// does not reach the representation — identically for either spelling of null.
func TestNullablePlan_RecursiveRef(t *testing.T) {
	res, got := compileNullable(t, `{
		"type": "object",
		"properties": {"self": {"type": "object", "$ref": "#", "nullable": true}}
	}`)

	require.Empty(t, res.Diagnostics)
	require.Contains(t, got, `Reference ""`)
	require.Contains(t, got, `definition ""`)

	declared, _ := compileNullable(t, `{
		"type": "object",
		"properties": {"self": {"type": ["object", "null"], "$ref": "#"}}
	}`)
	require.Equal(t, declared, res)
}

// Capability roll-up across a `$ref` is unaffected: a nullable reference to a dangling
// target reaches Unsupported exactly as the plain spelling does.
func TestNullablePlan_CapabilityRollUp(t *testing.T) {
	for _, tt := range []struct {
		name     string
		nullable string
	}{
		{"Plain", ""},
		{"Nullable", `, "nullable": true`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res, _ := compileNullable(t, `{
				"$ref": "#/$defs/A",
				"$defs": {"A": {"type": "object", "properties": {
					"x": {"type": "object", "$ref": "#/$defs/Missing"`+tt.nullable+`}
				}}}
			}`)

			require.Equal(t, plan.Unsupported, res.Plan.Capability)
		})
	}
}

const nullableDocument = `
openapi: 3.0.3
info: {title: t, version: v}
paths: {}
components:
  schemas:
    Thing: {type: object, properties: {name: {type: string}}}
    Widened: {type: string, nullable: true}
    Ignored: {$ref: '#/components/schemas/Thing', nullable: true}
`

// The whole-document path reports an inert `nullable` too, pointed at the component that
// declared it.
func TestNullableDocument_IgnoredDiagnostic(t *testing.T) {
	res, err := schemacompiler.CompileDocument(context.Background(), schemacompiler.Document{
		Schemas: componentSchemas(t, nullableDocument),
	}, schemacompiler.Options{})
	require.NoError(t, err)

	var ignored []plan.Diagnostic
	for _, d := range res.Diagnostics {
		require.NotEqual(t, plan.SeverityError, d.Severity, d.Message)
		if strings.Contains(d.Message, ignoredNullable) {
			ignored = append(ignored, d)
		}
	}

	require.Len(t, ignored, 1)
	require.Equal(t, plan.SeverityWarning, ignored[0].Severity)
	require.Equal(t, "/components/schemas/Ignored", ignored[0].Pointer)
}

const nullableRefDocument = `
openapi: 3.0.3
info: {title: t, version: v}
paths: {}
components:
  schemas:
    Cat: {type: object, required: [kind], properties: {kind: {type: string}}}
    Holder: {type: object, properties: {a: %s}}
`

// holderField compiles an OpenAPI 3.0 document whose `Holder.a` is node, and returns that
// field plus the number of ignored-`nullable` warnings.
func holderField(t *testing.T, node string) (field plan.FieldRepresentation, warnings int) {
	t.Helper()

	res, err := schemacompiler.CompileDocument(context.Background(), schemacompiler.Document{
		Schemas: componentSchemas(t, fmt.Sprintf(nullableRefDocument, node)),
	}, schemacompiler.Options{})
	require.NoError(t, err)

	obj, ok := res.Plans["/components/schemas/Holder"].Representation.(*plan.ObjectRepresentation)
	require.True(t, ok)

	var warns int
	for _, d := range res.Diagnostics {
		if strings.Contains(d.Message, ignoredNullable) {
			warns++
		}
	}
	return mustField(t, obj, "a"), warns
}

// The repo stops at `$ref` (#23) rather than intersecting a sibling kind set into the
// target, so a `$ref` sibling's nullability does not reach the representation. That is
// pre-existing and orthogonal to `nullable`: both spellings of null behave identically,
// which is the property this pins.
func TestNullablePlan_RefSiblingKindSetNotRepresented(t *testing.T) {
	const cat = "'#/components/schemas/Cat'"

	nullable, nullableWarns := holderField(t, "{$ref: "+cat+", type: object, nullable: true}")
	declared, declaredWarns := holderField(t, `{$ref: `+cat+`, type: [object, "null"]}`)

	require.Equal(t, declared, nullable)
	require.False(t, nullable.Nullable)
	require.Equal(t, 0, nullableWarns)
	require.Equal(t, 0, declaredWarns)

	// Clause 1 fails without an authored `type`, so that spelling warns instead — and
	// with no `type` there is no sibling kind set to assert over the reference, which is
	// what separates it from the two spellings above (issue #60).
	ignored, ignoredWarns := holderField(t, "{$ref: "+cat+", nullable: true}")
	require.Equal(t, 1, ignoredWarns)
	require.Equal(t, declared.Plan.Representation, ignored.Plan.Representation)
	require.Equal(t, declared.Nullable, ignored.Nullable)
	require.Equal(t, plan.GuardedPredicate{Applicability: plan.SetObject, Assert: true},
		declared.Plan.Validation.Predicates[0])
	require.Equal(t, declared.Plan.Validation.Predicates[1:], ignored.Plan.Validation.Predicates)
}

// Away from a `$ref` the sibling kind set does reach the representation, and the two
// spellings agree there too.
func TestNullablePlan_DocumentSpellingsAgree(t *testing.T) {
	nullable, warns := holderField(t, "{type: object, nullable: true}")
	declared, _ := holderField(t, `{type: [object, "null"]}`)

	require.Equal(t, declared, nullable)
	require.True(t, nullable.Nullable)
	require.Equal(t, 0, warns)
}
