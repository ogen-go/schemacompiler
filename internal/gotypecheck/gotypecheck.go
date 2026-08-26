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

// Source renders named as one self-contained Go file, in the order given.
func Source(named []*gogen.Named) string {
	var b strings.Builder
	b.WriteString(preamble)
	for _, n := range named {
		b.WriteString("\ntype ")
		b.WriteString(n.Name)
		b.WriteString(" ")
		b.WriteString(Type(n.Underlying))
		b.WriteString("\n")
	}
	return b.String()
}

// Type renders t as a Go type expression, on one line.
//
// It is the only rendering of a [gogen.GoType] the tests have, deliberately: a compact
// notation of its own would be a second answer to "what does this lower to", and the
// interesting failures are the ones where that answer looks right and does not compile.
// What a table holds here is Go, and [Check] is what says so.
func Type(t gogen.GoType) string {
	var b strings.Builder
	write(&b, t)
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
		// The key type is always string. A pattern is an annotation on the map, not part
		// of its Go type, so it is a comment — which is also the only place it can go
		// without spelling something Go does not mean.
		b.WriteString("map[string]")
		write(b, t.Elem)
		if t.Pattern != "" {
			b.WriteString(" /*")
			b.WriteString(comment(t.Pattern))
			b.WriteString("*/")
		}
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
		// A sum is an interface however it is spelled, and an interface holds its value
		// behind indirection whatever the variants are. They are named in a method
		// signature so that the rendering still says which types the sum is over.
		b.WriteString("interface{ variant(")
		for i, v := range t.Variants {
			if i > 0 {
				b.WriteString(", ")
			}
			write(b, v)
		}
		b.WriteString(") }")
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
	if len(t.Fields) == 0 && len(t.Patterns) == 0 && t.Additional == nil {
		b.WriteString("struct{}")
		return
	}

	b.WriteString("struct { ")
	taken := make(map[string]bool, len(t.Fields))
	for i, f := range t.Fields {
		if i > 0 {
			b.WriteString("; ")
		}
		taken[f.Name] = true
		b.WriteString(f.Name)
		b.WriteString(" ")
		write(b, f.Type)
	}
	for i, p := range t.Patterns {
		if len(t.Fields) > 0 || i > 0 {
			b.WriteString("; ")
		}
		name := freeName("Pattern"+strconv.Itoa(i)+"Props", taken)
		taken[name] = true
		b.WriteString(name)
		b.WriteString(" ")
		write(b, p)
	}
	if t.Additional != nil {
		if len(t.Fields) > 0 || len(t.Patterns) > 0 {
			b.WriteString("; ")
		}
		b.WriteString(freeName("Extra", taken))
		b.WriteString(" map[string]")
		write(b, t.Additional)
	}
	b.WriteString(" }")
}

func writeTuple(b *strings.Builder, t *gogen.Tuple) {
	if len(t.Elems) == 0 && t.Rest == nil {
		b.WriteString("struct{}")
		return
	}

	b.WriteString("struct { ")
	taken := make(map[string]bool, len(t.Elems))
	for i, e := range t.Elems {
		if i > 0 {
			b.WriteString("; ")
		}
		name := freeName("F"+strconv.Itoa(i), taken)
		taken[name] = true
		b.WriteString(name)
		b.WriteString(" ")
		write(b, e)
	}
	if t.Rest != nil {
		if len(t.Elems) > 0 {
			b.WriteString("; ")
		}
		b.WriteString(freeName("Rest", taken))
		b.WriteString(" []")
		write(b, t.Rest)
	}
	b.WriteString(" }")
}

// comment makes s safe inside a block comment. A `patternProperties` regex may contain
// anything at all, and `*/` in one would end the comment early and the file with it.
func comment(s string) string {
	return strconv.Quote(strings.ReplaceAll(s, "*/", "*\\/"))
}

// freeName is a generated slot's name, kept clear of the declared fields it sits beside.
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
