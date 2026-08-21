package schemacompiler_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler/plan"
)

// collectMetadata walks a representation and records every carrier of [plan.Metadata]
// under a stable key, so a test can assert what survived regardless of the shape the
// planner picked.
func collectMetadata(r plan.Representation, prefix string, out map[string]plan.Metadata) {
	switch r := r.(type) {
	case plan.ObjectRepresentation:
		for _, f := range r.Fields {
			key := prefix + "field:" + f.Name
			out[key] = f.Metadata
			collectMetadata(f.Plan.Representation, key+"/", out)
		}
		for _, pr := range r.PatternRules {
			key := prefix + "pattern:" + pr.Pattern
			out[key] = pr.Metadata
			collectMetadata(pr.Plan.Representation, key+"/", out)
		}
		if r.Additional != nil {
			collectMetadata(r.Additional.Representation, prefix+"additional/", out)
		}
	case plan.ArrayRepresentation:
		for i, p := range r.Prefix {
			key := fmt.Sprintf("%sprefix:%d", prefix, i)
			out[key] = p.Metadata
			collectMetadata(p.Plan.Representation, key+"/", out)
		}
		if r.Rest.Plan.Representation != nil {
			key := prefix + "items"
			out[key] = r.Rest.Metadata
			collectMetadata(r.Rest.Plan.Representation, key+"/", out)
		}
	case plan.UnionRepresentation:
		for _, alt := range r.Alternatives {
			collectMetadata(alt, prefix, out)
		}
	case plan.RecursiveRepresentation:
		collectMetadata(r.Body, prefix, out)
	}
}

// titles indexes every collected carrier by key, keeping only the ones that carry a
// title, so a table can spell out exactly what must survive.
func titles(t *testing.T, p plan.CompilationPlan) map[string]string {
	t.Helper()
	all := make(map[string]plan.Metadata)
	collectMetadata(p.Representation, "", all)
	out := make(map[string]string)
	for k, m := range all {
		if m.Title != "" {
			out[k] = m.Title
		}
	}
	return out
}

func TestCompile_MetadataSurvivesEveryShape(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		want   map[string]string
	}{
		{
			name:   "flat object",
			schema: `{"type":"object","properties":{"a":{"title":"A"}}}`,
			want:   map[string]string{"field:a": "A"},
		},
		{
			name:   "nullable object",
			schema: `{"type":["object","null"],"properties":{"a":{"title":"A"}}}`,
			want:   map[string]string{"field:a": "A"},
		},
		{
			name: "oneOf of object branches",
			schema: `{"oneOf":[
				{"type":"object","properties":{"a":{"type":"string","title":"A"}},"required":["a"]},
				{"type":"object","properties":{"b":{"type":"number","title":"B"}},"required":["b"]}
			]}`,
			want: map[string]string{"field:a": "A", "field:b": "B"},
		},
		{
			name: "anyOf of object branches",
			schema: `{"anyOf":[
				{"type":"object","properties":{"a":{"type":"string","title":"A"}},"required":["a"]},
				{"type":"object","properties":{"b":{"type":"number","title":"B"}},"required":["b"]}
			]}`,
			want: map[string]string{"field:a": "A", "field:b": "B"},
		},
		{
			name: "if/then/else",
			schema: `{
				"type":"object",
				"if":{"properties":{"kind":{"const":"x"}}},
				"then":{"properties":{"a":{"type":"string","title":"A"}}},
				"else":{"properties":{"b":{"type":"string","title":"B"}}}
			}`,
			want: map[string]string{"field:a": "A", "field:b": "B"},
		},
		{
			name: "dependentSchemas",
			schema: `{
				"type":"object",
				"properties":{"trigger":{"type":"string","title":"T"}},
				"dependentSchemas":{"trigger":{"properties":{"a":{"type":"string","title":"A"}}}}
			}`,
			want: map[string]string{"field:trigger": "T", "field:a": "A"},
		},
		{
			name: "patternProperties per rule",
			schema: `{"type":"object","patternProperties":{
				"^a":{"type":"string","title":"PatA"},
				"^b":{"type":"number","title":"PatB"}
			}}`,
			want: map[string]string{"pattern:^a": "PatA", "pattern:^b": "PatB"},
		},
		{
			name:   "array items",
			schema: `{"type":"array","items":{"type":"string","title":"Item"}}`,
			want:   map[string]string{"items": "Item"},
		},
		{
			name: "array prefixItems",
			schema: `{"type":"array","prefixItems":[{"type":"string","title":"First"}],
				"items":{"type":"number","title":"Rest"}}`,
			want: map[string]string{"prefix:0": "First", "items": "Rest"},
		},
		{
			name:   "additionalProperties",
			schema: `{"type":"object","additionalProperties":{"type":"string","title":"Extra"}}`,
			want:   map[string]string{},
		},
		{
			name: "allOf merge",
			schema: `{"allOf":[
				{"type":"object","properties":{"a":{"type":"string","title":"A"}}},
				{"type":"object","properties":{"b":{"type":"string","title":"B"}}}
			]}`,
			want: map[string]string{"field:a": "A", "field:b": "B"},
		},
		{
			name: "nested nullable object inside oneOf",
			schema: `{"oneOf":[
				{"type":"object","properties":{
					"a":{"type":["object","null"],"properties":{"inner":{"type":"string","title":"Inner"}},"title":"A"}
				},"required":["a"]},
				{"type":"object","properties":{"b":{"type":"number","title":"B"}},"required":["b"]}
			]}`,
			want: map[string]string{"field:a": "A", "field:a/field:inner": "Inner", "field:b": "B"},
		},
		{
			name: "array of nullable objects",
			schema: `{"type":"array","items":{
				"type":["object","null"],
				"title":"Item",
				"properties":{"a":{"type":"string","title":"A"}}
			}}`,
			want: map[string]string{"items": "Item", "items/field:a": "A"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := titles(t, compile(t, tt.schema))
			for key, want := range tt.want {
				require.Equal(t, want, got[key], "key %q; collected %v", key, got)
			}
			require.Len(t, got, len(tt.want), "unexpected extra carriers: %v", got)
		})
	}
}

func TestCompile_PatternRuleMetadataIsPositional(t *testing.T) {
	p := compile(t, `{"allOf":[
		{"type":"object","patternProperties":{"^a":{"type":"string","title":"PatA"}}},
		{"type":"object","patternProperties":{"^b":{"type":"number","title":"PatB"}}}
	]}`)
	obj, ok := p.Representation.(plan.ObjectRepresentation)
	require.True(t, ok, "got %T", p.Representation)
	require.Len(t, obj.PatternRules, 2)
	for _, pr := range obj.PatternRules {
		switch pr.Pattern {
		case "^a":
			require.Equal(t, "PatA", pr.Metadata.Title)
		case "^b":
			require.Equal(t, "PatB", pr.Metadata.Title)
		default:
			t.Fatalf("unexpected pattern %q", pr.Pattern)
		}
	}
}

func TestCompile_ExtensionsSurviveNullableObject(t *testing.T) {
	p := compile(t, `{
		"type":["object","null"],
		"properties":{"a":{"type":"string","description":"A field.","x-ogen-name":"Alpha"}}
	}`)
	all := make(map[string]plan.Metadata)
	collectMetadata(p.Representation, "", all)
	m := all["field:a"]
	require.Equal(t, "A field.", m.Description)
	require.Equal(t, map[string]any{"x-ogen-name": "Alpha"}, m.Extensions)
}

func TestCompile_MetadataIsNotSharedBetweenPlans(t *testing.T) {
	// `/$defs/A/properties/x` is both a field of A's plan and a $ref target with a plan
	// of its own, so the same source node backs two plans in one compilation.
	res := compileResult(t, `{
		"$defs":{"A":{"type":"object","properties":{
			"x":{"type":"string","title":"X","default":"d","x-k":"v","x-nested":{"k":"v"}}
		}}},
		"type":"object",
		"properties":{
			"a":{"$ref":"#/$defs/A"},
			"b":{"$ref":"#/$defs/A/properties/x"}
		}
	}`)
	graph, ok := res.Plan.Resolution.(plan.StaticReferenceGraph)
	require.True(t, ok, "got %T", res.Plan.Resolution)

	own, ok := graph.Definitions["/$defs/A/properties/x"]
	require.True(t, ok, "definitions: %v", graph.Definitions)
	require.Equal(t, "X", own.Metadata.Title)

	aPlan, ok := graph.Definitions["/$defs/A"]
	require.True(t, ok)
	aObj, ok := aPlan.Representation.(plan.ObjectRepresentation)
	require.True(t, ok, "got %T", aPlan.Representation)
	field := mustField(t, aObj, "x")
	require.Equal(t, "X", field.Metadata.Title)

	own.Metadata.Extensions["mutated"] = true
	own.Metadata.Extensions["x-nested"].(map[string]any)["mutated"] = true
	own.Metadata.Default[0] = 'Z'

	require.NotContains(t, field.Metadata.Extensions, "mutated")
	require.NotContains(t, field.Metadata.Extensions["x-nested"], "mutated")
	require.Equal(t, `"d"`, string(field.Metadata.Default))
}

func TestCompile_MetadataKeysAreDeterministic(t *testing.T) {
	p := compile(t, `{"type":"object","properties":{"a":{"type":"string","x-b":1,"x-a":2,"x-c":3}}}`)
	obj, ok := p.Representation.(plan.ObjectRepresentation)
	require.True(t, ok)
	ext := mustField(t, obj, "a").Metadata.Extensions
	keys := make([]string, 0, len(ext))
	for k := range ext {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	require.Equal(t, "x-a,x-b,x-c", strings.Join(keys, ","))
}
