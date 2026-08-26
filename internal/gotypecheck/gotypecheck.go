// Package gotypecheck type-checks generated Go source.
//
// It exists for one assertion the backend's own tests cannot make about themselves:
// whether a lowered type graph is legal Go. "Invalid recursive type" is a property of the
// whole graph, not of any one type, so a test can assert that a pointer appears exactly
// where it expects and still describe a package that does not build. go/parser does not
// see it either, since such a file parses; only go/types does.
package gotypecheck

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"

	"github.com/go-faster/errors"

	"github.com/ogen-go/schemacompiler/gogen"
)

// Check type-checks files, resolving their `opt` import against the real package in
// optDir.
//
// Checking against the real `opt` rather than a stand-in is what makes the result mean
// something: inline storage is the property that decides whether a cycle compiles, and a
// shim that got it wrong would prove the opposite of what this asserts. It costs nothing
// to do properly — `opt` imports nothing, so type-checking it needs no importer of its own.
func Check(files []gogen.File, optDir string) error {
	fset := token.NewFileSet()
	opt, err := checkOpt(fset, optDir)
	if err != nil {
		return errors.Wrap(err, "type-check opt")
	}

	parsed := make([]*ast.File, 0, len(files))
	for _, f := range files {
		file, err := parser.ParseFile(fset, f.Name, f.Content, parser.SkipObjectResolution)
		if err != nil {
			return errors.Wrapf(err, "parse %s", f.Name)
		}
		parsed = append(parsed, file)
	}

	// Every error, not just the first: one lowering bug usually shows up in every type
	// that references what it broke.
	var errs []error
	conf := types.Config{
		Importer: importerFunc(func(path string) (*types.Package, error) {
			if path == gogen.DefaultOptPackage {
				return opt, nil
			}
			return nil, errors.Errorf("generated code imports %q, which nothing provides", path)
		}),
		Error: func(err error) { errs = append(errs, err) },
	}
	_, _ = conf.Check("generated", fset, parsed, nil)
	return errors.Join(errs...)
}

func checkOpt(fset *token.FileSet, dir string) (*types.Package, error) {
	names, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	var parsed []*ast.File
	for _, name := range names {
		if filepath.Base(name) == "doc.go" || len(name) > 8 && name[len(name)-8:] == "_test.go" {
			continue
		}
		src, err := os.ReadFile(name) //nolint:gosec // test support, path from a caller-supplied directory
		if err != nil {
			return nil, err
		}
		file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, file)
	}
	if len(parsed) == 0 {
		return nil, errors.Errorf("no Go sources in %q", dir)
	}
	return (&types.Config{}).Check(gogen.DefaultOptPackage, fset, parsed, nil)
}

type importerFunc func(path string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }
