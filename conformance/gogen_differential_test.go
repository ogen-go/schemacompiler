// gogen_differential_test.go is docs/backend.md §3: generated Go joins `planterp` and
// `nodetree` as a third validator over the same suite instances. It compiles what `gogen`
// renders and runs it, because what generated code accepts is not a property of its text.
//
// The check is one-directional, and deliberately. Generated code over-accepts by
// construction — every constraint it reports as unenforced is one it will not reject on —
// and design §24 permits exactly that while forbidding the other direction outright. So
// rejecting an instance `planterp` accepts is a failure; accepting one it rejects is a
// number.
package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/schemacompiler"
	"github.com/ogen-go/schemacompiler/gogen"
	"github.com/ogen-go/schemacompiler/internal/gorun"
	"github.com/ogen-go/schemacompiler/internal/planterp"
	"github.com/ogen-go/schemacompiler/plan"
)

// rootID is the pointer the root plan is filed under so it gets a Go type of its own. Only
// referenced definitions reach the graph, and the schema under test is nobody's referent.
const rootID plan.SchemaID = "/Case"

// genCase is one suite case that lowered, with the Go type its instances decode into.
type genCase struct {
	file  string
	desc  string
	root  string
	plan  plan.CompilationPlan
	tests []diffInstance
}

func TestGeneratedCodeAgreesWithPlanterp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping suite walk in -short mode")
	}
	cases, types, skipped := lowerSuite(t)
	require.NotEmpty(t, cases)

	files, err := gogen.Render(types, gogen.Options{
		Package:    "main",
		OptPackage: gorun.OptPackage,
		Codec:      true,
		Validate:   true,
	})
	require.NoError(t, err)

	var requests []genRequest
	for i, c := range cases {
		for _, inst := range c.tests {
			requests = append(requests, genRequest{Case: i, Data: inst.Data})
		}
	}
	input, err := json.Marshal(requests)
	require.NoError(t, err)

	out, err := gorun.Run(t.TempDir(), files, "../opt", genMain(cases), input)
	require.NoError(t, err, "the generated package must build and run")
	var got []genResponse
	require.NoError(t, json.Unmarshal(out, &got))
	require.Len(t, got, len(requests))

	compareVerdicts(t, cases, requests, got, skipped)
}

// lowerSuite compiles and lowers every suite case that gogen can produce a type for, and
// renames each case's types apart so one package can hold all of them.
func lowerSuite(t *testing.T) (cases []genCase, types []*gogen.Named, skipped map[string]int) {
	t.Helper()
	skipped = map[string]int{}
	for _, file := range suiteFiles(t) {
		rel, err := filepath.Rel(suiteRoot, file)
		require.NoError(t, err)
		rel = filepath.ToSlash(rel)
		if _, ok := outOfDialect[rel]; ok {
			continue
		}
		data, err := os.ReadFile(file) //nolint:gosec // test-only, path from a controlled walk
		require.NoError(t, err)
		var suite []diffCase
		if json.Unmarshal(data, &suite) != nil {
			continue
		}

		for _, c := range suite {
			if len(c.Schema) == 0 {
				continue
			}
			res, err := compileQuietly(c.Schema)
			if err != nil {
				skipped["compile"]++
				continue
			}
			if res.Capability == plan.Unsupported || res.Capability >= plan.EvaluationStateValidation {
				skipped["capability above StaticDispatch"]++
				continue
			}
			lowered, root, err := lowerCase(res, len(cases))
			if err != nil {
				skipped[err.Error()]++
				continue
			}
			cases = append(cases, genCase{file: rel, desc: c.Description, root: root, plan: res.Plan, tests: c.Tests})
			types = append(types, lowered...)
		}
	}
	return cases, types, skipped
}

func lowerCase(res *schemacompiler.Result, index int) ([]*gogen.Named, string, error) {
	defs := map[plan.SchemaID]plan.CompilationPlan{rootID: res.Plan}
	// A schema with no `$ref` is fully resolved and names no definitions; one with a
	// reference carries the graph the name resolves against (docs/integration.md §5).
	if graph, ok := res.Plan.Resolution.(*plan.StaticReferenceGraph); ok {
		for id, p := range graph.Definitions {
			defs[id] = p
		}
	}

	lowered, err := gogen.Lower(defs)
	if err != nil {
		return nil, "", fmt.Errorf("lower")
	}
	// One package holds every case, and two cases naming a `$defs` entry the same is the
	// rule rather than the exception. A use site holds the same *Named the declaration
	// does, so renaming after lowering renames every reference with it.
	var root string
	for _, n := range lowered {
		renamed := fmt.Sprintf("C%d%s", index, n.Name)
		if n.Name == "Case" {
			root = renamed
		}
		n.Name = renamed
	}
	if root == "" {
		return nil, "", fmt.Errorf("no type for the root schema")
	}
	return lowered, root, nil
}

type genRequest struct {
	Case int             `json:"c"`
	Data json.RawMessage `json:"d"`
}

type genResponse struct {
	OK  bool   `json:"ok"`
	Err string `json:"err"`
}

// genMain writes the program that decodes each instance into the type its case lowered to
// and reports whether generated code accepted it.
func genMain(cases []genCase) string {
	var b strings.Builder
	b.WriteString(`package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type request struct {
	Case int             ` + "`json:\"c\"`" + `
	Data json.RawMessage ` + "`json:\"d\"`" + `
}

type response struct {
	OK  bool   ` + "`json:\"ok\"`" + `
	Err string ` + "`json:\"err\"`" + `
}

func main() {
	var reqs []request
	if err := json.NewDecoder(os.Stdin).Decode(&reqs); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out := make([]response, len(reqs))
	for i, r := range reqs {
		if err := admit(r.Case, r.Data); err != nil {
			out[i] = response{Err: err.Error()}
			continue
		}
		out[i] = response{OK: true}
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// validate runs the method when the type has one. Whether a type needs a validator is the
// backend's decision, so asking the value is more honest than predicting it.
func validate(v any) error {
	if x, ok := v.(interface{ Validate() error }); ok {
		return x.Validate()
	}
	return nil
}

// admit reports why generated code would not take data as a value of case c. A panic is a
// verdict too — it is recovered so one bad case does not take the run with it.
func admit(c int, data []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	switch c {
`)
	for i, c := range cases {
		fmt.Fprintf(&b, "\tcase %d:\n\t\tvar v %s\n", i, c.root)
		b.WriteString("\t\tif err := json.Unmarshal(data, &v); err != nil {\n\t\t\treturn err\n\t\t}\n\t\treturn validate(v)\n")
	}
	b.WriteString("\t}\n\treturn fmt.Errorf(\"no case %d\", c)\n}\n")
	return b.String()
}

// unrepresentableNumber reports that the decoder refused a number rather than the shape:
// a value past int64, or an integer the document spelled with a fraction or an exponent.
// It is matched on [encoding/json]'s own message because that is where the refusal is made.
func unrepresentableNumber(err string) bool {
	return strings.HasPrefix(err, "json: cannot unmarshal number")
}

func compareVerdicts(t *testing.T, cases []genCase, requests []genRequest, got []genResponse, skipped map[string]int) {
	t.Helper()
	var checked, agreed, overAccepted, panicked, unrepresentable int
	var findings []string
	for i, req := range requests {
		c := cases[req.Case]
		value, err := decodeInstance(req.Data)
		if err != nil {
			continue
		}
		want, err := planterp.Interpret(c.plan, value)
		if err != nil {
			continue // planterp's own guards are differential_test.go's business.
		}
		checked++

		accepted := got[i].OK
		if strings.HasPrefix(got[i].Err, "panic: ") {
			panicked++
			findings = append(findings, fmt.Sprintf("%s (%q): %s on %s", c.file, c.desc, got[i].Err, req.Data))
			continue
		}
		switch {
		case want.Accepted == accepted:
			agreed++
		case want.Accepted && unrepresentableNumber(got[i].Err):
			// A JSON number is unbounded and may be written any way that names its value;
			// int64 and float64 are neither. This is an under-accept and it is counted
			// rather than failed because no Go numeric type closes it — see issue #163.
			unrepresentable++
		case want.Accepted:
			// Rejecting an instance the oracle accepts is the one direction design §24
			// forbids: the generated type cannot hold a document the schema admits.
			findings = append(findings, fmt.Sprintf("%s (%q): rejected %s: %s", c.file, c.desc, req.Data, got[i].Err))
		default:
			overAccepted++
		}
	}

	require.NotZero(t, checked)
	t.Logf("%d cases, %d instances, %d agreed, %d over-accepted (%.1f%%), %d unrepresentable, %d panicked",
		len(cases), checked, agreed, overAccepted, float64(overAccepted)*100/float64(checked), unrepresentable, panicked)
	for _, reason := range sortedKeys(skipped) {
		t.Logf("  skipped %5d %s", skipped[reason], reason)
	}
	for _, f := range findings {
		t.Errorf("%s", f)
	}
}
