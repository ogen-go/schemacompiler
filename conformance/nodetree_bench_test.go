package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/internal/nodetree"
	"github.com/ogen-go/schemacompiler/internal/planterp"
)

const benchSchema = `{
  "type":"object",
  "required":["id","name","tags"],
  "properties":{
    "id":{"type":"integer","minimum":1},
    "name":{"type":"string","minLength":1,"maxLength":64},
    "email":{"type":"string","pattern":"^[^@]+@[^@]+$"},
    "tags":{"type":"array","items":{"type":"string","minLength":1},"maxItems":10},
    "meta":{"type":"object","additionalProperties":{"type":"string"}}
  }
}`

var benchDoc = []byte(`{"id":7,"name":"widget","email":"a@b.c","tags":["x","y","z"],"meta":{"k":"v"}}`)

func BenchmarkPlanterp(b *testing.B) {
	res, err := schemacompiler.Compile(context.Background(), []byte(benchSchema), schemacompiler.Options{})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dec := json.NewDecoder(bytes.NewReader(benchDoc))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			b.Fatal(err)
		}
		verdict, err := planterp.Interpret(res.Plan, v)
		if err != nil || !verdict.Accepted {
			b.Fatalf("%v %v", err, verdict.Reason)
		}
	}
}

func BenchmarkNodetree(b *testing.B) {
	res, err := schemacompiler.Compile(context.Background(), []byte(benchSchema), schemacompiler.Options{})
	if err != nil {
		b.Fatal(err)
	}
	v, err := nodetree.Compile(res.Plan)
	if err != nil {
		b.Fatal(err)
	}
	if !v.IsValid(benchDoc) {
		b.Fatal("should accept")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !v.IsValid(benchDoc) {
			b.Fatal("should accept")
		}
	}
}

var benchBadDoc = []byte(`{"id":0,"name":"","email":"nope","tags":["x",""],"meta":{"k":1}}`)

// BenchmarkNodetreeInvalid is the rejecting path with no reporting: it must not pay for
// error machinery either.
func BenchmarkNodetreeInvalid(b *testing.B) {
	v := benchValidator(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if v.IsValid(benchBadDoc) {
			b.Fatal("should reject")
		}
	}
}

// BenchmarkNodetreeValidate is the first-error path, which is where the lazy location is
// actually materialized.
func BenchmarkNodetreeValidate(b *testing.B) {
	v := benchValidator(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := v.Validate(benchBadDoc); err == nil {
			b.Fatal("should reject")
		}
	}
}

// BenchmarkNodetreeIterErrors drains every error, the most expensive mode.
func BenchmarkNodetreeIterErrors(b *testing.B) {
	v := benchValidator(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		n := 0
		for range v.IterErrors(benchBadDoc) {
			n++
		}
		if n == 0 {
			b.Fatal("should report")
		}
	}
}

func benchValidator(b *testing.B) *nodetree.Validator {
	b.Helper()
	res, err := schemacompiler.Compile(context.Background(), []byte(benchSchema), schemacompiler.Options{})
	if err != nil {
		b.Fatal(err)
	}
	v, err := nodetree.Compile(res.Plan)
	if err != nil {
		b.Fatal(err)
	}
	return v
}
