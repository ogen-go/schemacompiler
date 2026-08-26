package gogen

import (
	"fmt"
	"slices"
	"strings"

	"github.com/go-faster/errors"

	"github.com/ogen-go/schemacompiler/plan"
)

// Names maps each schema to the Go type name its plan lowers to.
type Names map[plan.SchemaID]string

// CollisionError reports two or more schemas deriving one Go name. It is an error rather
// than a suffix: `Pet2` would mean adding one schema silently renames another author's
// type, and the rename would land in code the author does not own.
type CollisionError struct {
	Name string
	// Pointers are the colliding schemas, sorted.
	Pointers []plan.SchemaID
}

func (e *CollisionError) Error() string {
	ptrs := make([]string, len(e.Pointers))
	for i, p := range e.Pointers {
		ptrs[i] = string(p)
	}
	return fmt.Sprintf("%d schemas derive the Go name %q (%s); set %s on all but one",
		len(e.Pointers), e.Name, strings.Join(ptrs, ", "), NameExtension)
}

// Assign names every schema in plans, applying [NameExtension] where the author set one
// and [TypeName] everywhere else.
//
// Every failure is reported, not just the first: an author fixing names wants the whole
// list, and fixing one collision can reveal the next. The errors are joined in pointer
// order so a run is reproducible.
func Assign(plans map[plan.SchemaID]plan.CompilationPlan) (Names, error) {
	ids := make([]plan.SchemaID, 0, len(plans))
	for id := range plans {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	names := make(Names, len(plans))
	owners := make(map[string][]plan.SchemaID, len(plans))
	var errs []error
	for _, id := range ids {
		name, err := nameOf(id, plans[id].Metadata)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		names[id] = name
		owners[name] = append(owners[name], id)
	}

	collided := make([]string, 0)
	for name, ptrs := range owners {
		if len(ptrs) > 1 {
			collided = append(collided, name)
		}
	}
	slices.Sort(collided)
	for _, name := range collided {
		errs = append(errs, &CollisionError{Name: name, Pointers: owners[name]})
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return names, nil
}

// nameOf prefers the author's name to the derived one. An override that is not a string,
// or not a Go identifier, is an error: it is the one place the author is telling the
// backend a name outright, so silently falling back to the derived one would ignore them.
func nameOf(id plan.SchemaID, meta plan.Metadata) (string, error) {
	raw, ok := meta.Extensions[NameExtension]
	if !ok {
		return TypeName(string(id))
	}
	name, ok := raw.(string)
	if !ok {
		return "", errors.Errorf("pointer %q: %s must be a string, got %T", id, NameExtension, raw)
	}
	if err := checkIdentifier(name); err != nil {
		return "", errors.Wrapf(err, "pointer %q: %s", id, NameExtension)
	}
	return name, nil
}
