// Package gotypecheck renders lowered [gogen] types as Go source and hands them to the Go
// type checker.
//
// It exists for one assertion the backend's own tests cannot make about themselves:
// whether a lowered type graph is legal Go. "Invalid recursive type" is not a property of
// any single type, and no string comparison sees it — a test can assert that a pointer
// appears exactly where it expects and still be describing a package that does not
// compile. go/parser does not see it either, since such a file parses; only go/types does.
package gotypecheck

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"github.com/go-faster/errors"

	"github.com/ogen-go/schemacompiler/gogen"
)

// preamble stands in for the [opt] package so the rendered file imports nothing and the
// checker needs no importer. What matters is reproduced exactly: the value is stored
// inline, which is what makes a cycle through one of these an invalid recursive type.
const preamble = `package p

type Opt[T any] struct {
	_   [0]func()
	val T
	set bool
}

type Nullable[T any] struct {
	_   [0]func()
	val T
	set bool
}

type OptNullable[T any] struct {
	_     [0]func()
	val   T
	state uint8
}
`

// Verify renders types and type-checks them, reporting every error the checker found.
func Verify(named []*gogen.Named) error {
	src := Source(named)
	if err := Check(src); err != nil {
		return errors.Wrap(err, "type-check")
	}
	return nil
}

// Source renders types as one self-contained Go file, in the order given.
func Source(named []*gogen.Named) string {
	var b strings.Builder
	b.WriteString(preamble)
	for _, n := range named {
		b.WriteString("\ntype ")
		b.WriteString(n.Name)
		b.WriteString(" ")
		write(&b, n.Underlying)
		b.WriteString("\n")
	}
	return b.String()
}

// Check parses and type-checks src, joining every error rather than stopping at the first:
// one lowering bug usually shows up in every type that references what it broke.
func Check(src string) error {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "lowered.go", src, parser.SkipObjectResolution)
	if err != nil {
		return errors.Wrap(err, "parse")
	}

	var errs []error
	conf := types.Config{Error: func(err error) { errs = append(errs, err) }}
	_, _ = conf.Check("p", fset, []*ast.File{file}, nil)
	return errors.Join(errs...)
}

func write(b *strings.Builder, t gogen.GoType) {
	switch t := t.(type) {
	case *gogen.Named:
		b.WriteString(t.Name)
	case *gogen.Pointer:
		b.WriteString("*")
		write(b, t.Elem)
	case *gogen.Slice:
		b.WriteString("[]")
		write(b, t.Elem)
	case *gogen.Map:
		b.WriteString("map[string]")
		write(b, t.Elem)
	case *gogen.Presence:
		switch {
		case t.Optional && t.Nullable:
			b.WriteString("OptNullable[")
		case t.Optional:
			b.WriteString("Opt[")
		default:
			b.WriteString("Nullable[")
		}
		write(b, t.Elem)
		b.WriteString("]")
	case *gogen.Struct:
		writeStruct(b, t)
	case *gogen.Tuple:
		writeTuple(b, t)
	case *gogen.Interface:
		// A sum is an interface whichever way it is spelled, and an interface holds its
		// value behind indirection no matter what the variants are. Naming them would
		// tell the checker nothing it does not already accept.
		b.WriteString("any")
	case *gogen.Primitive:
		b.WriteString(goPrimitive(t.Kind))
	case *gogen.Any:
		b.WriteString("any")
	case *gogen.Never:
		b.WriteString("struct{}")
	default:
		panic(fmt.Sprintf("gotypecheck: unhandled gogen.GoType variant %T", t))
	}
}

func writeStruct(b *strings.Builder, t *gogen.Struct) {
	b.WriteString("struct {\n")
	taken := make(map[string]bool, len(t.Fields))
	for _, f := range t.Fields {
		taken[f.Name] = true
		b.WriteString("\t")
		b.WriteString(f.Name)
		b.WriteString(" ")
		write(b, f.Type)
		b.WriteString(" `json:")
		b.WriteString(strconv.Quote(f.JSON))
		b.WriteString("`\n")
	}
	for i, p := range t.Patterns {
		name := freeName("Pattern"+strconv.Itoa(i)+"Props", taken)
		taken[name] = true
		b.WriteString("\t")
		b.WriteString(name)
		b.WriteString(" ")
		write(b, p)
		b.WriteString(" // pattern ")
		b.WriteString(strconv.Quote(p.Pattern))
		b.WriteString("\n")
	}
	if t.Additional != nil {
		b.WriteString("\t")
		b.WriteString(freeName("Extra", taken))
		b.WriteString(" map[string]")
		write(b, t.Additional)
		b.WriteString("\n")
	}
	b.WriteString("}")
}

func writeTuple(b *strings.Builder, t *gogen.Tuple) {
	b.WriteString("struct {\n")
	taken := make(map[string]bool, len(t.Elems))
	for i, e := range t.Elems {
		name := freeName("F"+strconv.Itoa(i), taken)
		taken[name] = true
		b.WriteString("\t")
		b.WriteString(name)
		b.WriteString(" ")
		write(b, e)
		b.WriteString("\n")
	}
	if t.Rest != nil {
		b.WriteString("\t")
		b.WriteString(freeName("Rest", taken))
		b.WriteString(" []")
		write(b, t.Rest)
		b.WriteString("\n")
	}
	b.WriteString("}")
}

// freeName is the overflow slot's name, kept clear of the declared fields it sits beside.
// A schema may well declare a property called `extra`, and a duplicate field would fail
// the check for a reason that is this renderer's rather than the lowering's.
func freeName(want string, taken map[string]bool) string {
	for taken[want] {
		want += "_"
	}
	return want
}

func goPrimitive(k gogen.PrimitiveKind) string {
	switch k {
	case gogen.PrimitiveString:
		return "string"
	case gogen.PrimitiveBool:
		return "bool"
	case gogen.PrimitiveInt:
		return "int64"
	case gogen.PrimitiveFloat:
		return "float64"
	case gogen.PrimitiveNull:
		return "struct{}"
	default:
		panic(fmt.Sprintf("gotypecheck: unhandled gogen.PrimitiveKind %v", k))
	}
}
