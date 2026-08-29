package gogen

import (
	"slices"
	"strconv"

	"github.com/go-faster/errors"

	"github.com/ogen-go/schemacompiler/plan"
)

// Lower turns a document's schemas into Go types.
//
// It takes the whole map rather than one plan at a time, and that is a requirement, not a
// convenience: whether a type needs pointer indirection is a property of the reference
// graph, so lowering schema by schema would give two callers two different shapes for the
// same schema (docs/backend.md §7).
//
// Lowering is three ordered passes. Container shape comes from [plan.Representation]
// alone; the recursion pass then reads the Go-storage edges that shape implies and marks
// every type in a cycle; finally each direct reference to a marked type becomes a
// [Pointer]. The order is one-way — no pass reads a later one's output — so a second run
// over the same input produces the same types.
//
// The result is sorted by schema ID. Validation, dispatch and encoding are not lowered
// here; this is the representation only.
func Lower(plans map[plan.SchemaID]plan.CompilationPlan) ([]*Named, error) {
	names, err := Assign(plans)
	if err != nil {
		return nil, err
	}

	ids := make([]plan.SchemaID, 0, len(plans))
	for id := range plans {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	l := &lowerer{named: make(map[plan.SchemaID]*Named, len(plans))}
	for _, id := range ids {
		l.named[id] = &Named{ID: id, Name: names[id], Metadata: plans[id].Metadata}
	}

	types := make([]*Named, 0, len(ids))
	var errs []error
	for _, id := range ids {
		n := l.named[id]
		u, err := l.plan(plans[id])
		if err != nil {
			errs = append(errs, errors.Wrapf(err, "schema %q", id))
			continue
		}
		n.Underlying = u
		types = append(types, n)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	breakCycles(types)

	// Splitting last: what a type can carry depends on the shape every earlier pass
	// settled, pointer indirection included.
	for _, n := range types {
		n.Checks = Split(n, plans[n.ID])
	}
	return types, nil
}

// dedupeTypes keeps the first of each structurally identical alternative, in order.
func dedupeTypes(variants []GoType) []GoType {
	seen := make(map[string]bool, len(variants))
	out := variants[:0:0]
	for _, v := range variants {
		key := TypeExpr(v)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, v)
	}
	return out
}

type lowerer struct {
	named map[plan.SchemaID]*Named
}

func (l *lowerer) plan(p plan.CompilationPlan) (GoType, error) {
	t, err := l.rep(p.Representation)
	if err != nil {
		return nil, err
	}
	return l.dispatch(t, p.Dispatch), nil
}

// dispatch folds what the plan selects on into the stored shape.
//
// Only [plan.LiteralDispatch] changes it. The others pick between alternatives the
// representation already holds — the shape is the same whichever branch runs — so what
// they add is selection, which belongs to a decoder rather than to a type.
func (l *lowerer) dispatch(t GoType, d plan.DispatchPlan) GoType {
	lit, ok := d.(*plan.LiteralDispatch)
	if !ok || len(lit.Cases) == 0 {
		return t
	}
	values := make([]EnumValue, len(lit.Cases))
	for i, c := range lit.Cases {
		values[i] = EnumValue{Name: enumValueName(c.Value, c.Raw), Value: c.Value, Raw: c.Raw}
	}
	return &Enum{Elem: t, Values: values}
}

// enumValueName derives a constant name from a literal, or "" when it cannot.
//
// A name that cannot be derived is a rendering loss and not a semantic one — the value is
// still in the plan and still enforced — so unlike a type name (§1) this does not refuse.
// A leading digit is left in: the renderer prefixes the type name, so `1` becomes `Code1`,
// and it is the prefixed spelling that has to be an identifier.
func enumValueName(v any, raw []byte) string {
	switch v := v.(type) {
	case string:
		return camel(v)
	case bool:
		return camel(strconv.FormatBool(v))
	case nil:
		return "Null"
	default:
		text, ok := numericText(v, raw)
		if !ok {
			return ""
		}
		return camel(text)
	}
}

// numericText is a JSON number's source text.
//
// It reads [plan.LiteralCase.Raw] first, so a literal past float64's precision keeps the
// digits the document wrote. The fallback covers every numeric spelling [plan.LiteralCase]
// documents: an integer arrives as int, not float64, so a backend switching on float64
// alone misses every integer enum (issue #152).
func numericText(v any, raw []byte) (string, bool) {
	if len(raw) > 0 {
		return string(raw), true
	}
	switch v := v.(type) {
	case int:
		return strconv.Itoa(v), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case uint64:
		return strconv.FormatUint(v, 10), true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	default:
		return "", false
	}
}

func (l *lowerer) rep(r plan.Representation) (GoType, error) {
	switch r := r.(type) {
	case nil:
		return &Any{}, nil
	case *plan.AnyRepresentation:
		return &Any{}, nil
	case *plan.NeverRepresentation:
		return &Never{}, nil
	case *plan.PrimitiveRepresentation:
		return primitive(r)
	case *plan.ObjectRepresentation:
		return l.object(r)
	case *plan.ArrayRepresentation:
		return l.array(r)
	case *plan.UnionRepresentation:
		return l.union(r)
	case *plan.ReferenceRepresentation:
		n, ok := l.named[plan.SchemaID(r.Name)]
		if !ok {
			return nil, errors.Errorf("reference %q resolves to no schema in this document", r.Name)
		}
		return n, nil
	default:
		return nil, errors.Errorf("unhandled plan.Representation variant %T", r)
	}
}

func primitive(r *plan.PrimitiveRepresentation) (GoType, error) {
	switch r.Kind {
	case plan.KindString:
		return &Primitive{Kind: PrimitiveString, Format: r.Format}, nil
	case plan.KindBoolean:
		return &Primitive{Kind: PrimitiveBool, Format: r.Format}, nil
	case plan.KindNull:
		return &Primitive{Kind: PrimitiveNull, Format: r.Format}, nil
	case plan.KindNumber:
		if r.Numeric == plan.IntegerOnly {
			return &Primitive{Kind: PrimitiveInt, Format: r.Format}, nil
		}
		return &Primitive{Kind: PrimitiveFloat, Format: r.Format}, nil
	default:
		return nil, errors.Errorf("primitive representation of non-scalar kind %d", r.Kind)
	}
}

func (l *lowerer) object(o *plan.ObjectRepresentation) (GoType, error) {
	extra, err := l.overflow(o)
	if err != nil {
		return nil, err
	}
	patterns, err := l.patterns(o)
	if err != nil {
		return nil, err
	}

	// An object with nothing declared is a map when exactly one thing governs its keys:
	// a lone pattern rule over a closed object, or `additionalProperties` with no rules.
	// Anything else needs a slot per governing rule, and a struct is what has slots.
	if len(o.Fields) == 0 {
		switch {
		case len(patterns) == 1 && extra == nil:
			return patterns[0], nil
		case len(patterns) == 0 && extra != nil:
			return &Map{Elem: extra}, nil
		case len(patterns) == 0 && extra == nil:
			return &Struct{}, nil
		}
	}

	fields := make([]Field, 0, len(o.Fields))
	taken := make(map[string]string, len(o.Fields))
	for _, f := range o.Fields {
		field, err := l.field(f)
		if err != nil {
			return nil, err
		}
		if prev, ok := taken[field.Name]; ok {
			return nil, errors.Errorf("properties %q and %q both derive the Go field name %q; set %s on one",
				prev, f.Name, field.Name, NameExtension)
		}
		taken[field.Name] = f.Name
		fields = append(fields, field)
	}
	return &Struct{Fields: fields, Patterns: patterns, Additional: extra}, nil
}

// patterns is one map per `patternProperties` rule, keeping the rule's own element type
// and the regex that routes keys to it.
func (l *lowerer) patterns(o *plan.ObjectRepresentation) ([]*Map, error) {
	if len(o.PatternRules) == 0 {
		return nil, nil
	}
	out := make([]*Map, len(o.PatternRules))
	for i, r := range o.PatternRules {
		elem, err := l.plan(r.Plan)
		if err != nil {
			return nil, errors.Wrapf(err, "patternProperties[%q]", r.Pattern)
		}
		out[i] = &Map{Elem: elem, Pattern: r.Pattern}
	}
	return out, nil
}

// overflow is the type of every property no declared field and no pattern rule covers, or
// nil when the object admits none.
func (l *lowerer) overflow(o *plan.ObjectRepresentation) (GoType, error) {
	if o.Additional == nil {
		return nil, errors.New("object representation states no additionalProperties plan")
	}
	add, err := l.plan(*o.Additional)
	if err != nil {
		return nil, err
	}
	if _, closed := add.(*Never); closed {
		return nil, nil
	}
	return add, nil
}

func (l *lowerer) field(f plan.FieldRepresentation) (Field, error) {
	name, err := fieldName(f)
	if err != nil {
		return Field{}, err
	}
	t, err := l.plan(f.Plan)
	if err != nil {
		return Field{}, errors.Wrapf(err, "property %q", f.Name)
	}
	return Field{
		Name:     name,
		JSON:     f.Name,
		Type:     withPresence(t, f.Presence == plan.PresenceOptional, f.Nullable),
		Metadata: f.Metadata,
	}, nil
}

func fieldName(f plan.FieldRepresentation) (string, error) {
	if raw, ok := f.Metadata.Extensions[NameExtension]; ok {
		name, ok := raw.(string)
		if !ok {
			return "", errors.Errorf("property %q: %s must be a string, got %T", f.Name, NameExtension, raw)
		}
		if err := checkIdentifier(name); err != nil {
			return "", errors.Wrapf(err, "property %q: %s", f.Name, NameExtension)
		}
		return name, nil
	}
	name := camel(unescapePointer(f.Name))
	if name == "" {
		return "", errors.Errorf("property %q contributes no field name; set %s", f.Name, NameExtension)
	}
	if err := checkIdentifier(name); err != nil {
		return "", errors.Wrapf(err, "property %q derives %q", f.Name, name)
	}
	return name, nil
}

// withPresence folds optionality and nullability into t. A type that is already a
// [Presence] absorbs them rather than nesting: `{"type":["string","null"]}` on an optional
// property is one opt.OptNullable, not an opt.Opt of an opt.Nullable.
func withPresence(t GoType, optional, nullable bool) GoType {
	if p, ok := t.(*Presence); ok {
		p.Optional = p.Optional || optional
		p.Nullable = p.Nullable || nullable
		return p
	}
	if !optional && !nullable {
		return t
	}
	return &Presence{Elem: t, Optional: optional, Nullable: nullable}
}

func (l *lowerer) array(a *plan.ArrayRepresentation) (GoType, error) {
	// A closed array reaches here two ways: no rest plan at all, and a rest plan over
	// [plan.NeverRepresentation] from `items: false`. They mean the same thing, and a
	// slice of a type holding no value is not a shape worth generating.
	var rest GoType
	if a.Rest.Plan.Representation != nil {
		t, err := l.plan(a.Rest.Plan)
		if err != nil {
			return nil, errors.Wrap(err, "items")
		}
		if _, closed := t.(*Never); !closed {
			rest = t
		}
	}

	if len(a.Prefix) == 0 {
		if rest == nil {
			return &Tuple{}, nil
		}
		return &Slice{Elem: rest}, nil
	}

	// An optional slot has to be wrapped: a bare slot cannot tell an absent item from a
	// zero one, so encoding would put back a value the instance never carried.
	elems := make([]GoType, len(a.Prefix))
	for i, p := range a.Prefix {
		t, err := l.plan(p.Plan)
		if err != nil {
			return nil, errors.Wrapf(err, "prefixItems[%d]", i)
		}
		elems[i] = withPresence(t, p.Presence == plan.PresenceOptional, false)
	}
	return &Tuple{Elems: elems, Rest: rest}, nil
}

// union lowers alternatives to an interface sum, lifting a null alternative out into
// [Presence]: `null` carries no value, so a variant for it would be a type with nothing
// in it and a second way to spell what nullability already says.
func (l *lowerer) union(u *plan.UnionRepresentation) (GoType, error) {
	var (
		variants []GoType
		nullable bool
	)
	for i, alt := range u.Alternatives {
		if p, ok := alt.(*plan.PrimitiveRepresentation); ok && p.Kind == plan.KindNull {
			nullable = true
			continue
		}
		t, err := l.rep(alt)
		if err != nil {
			return nil, errors.Wrapf(err, "alternative %d", i)
		}
		variants = append(variants, t)
	}

	// Alternatives that lower to the same shape are one slot, not several. An `enum` of
	// same-kinded literals reaches here as a union with one alternative per literal, and
	// without this every string enum in a document would be an interface over N copies of
	// `string` — which is to say, `any`.
	variants = dedupeTypes(variants)

	var inner GoType
	switch len(variants) {
	case 0:
		inner = &Primitive{Kind: PrimitiveNull}
		nullable = false
	case 1:
		inner = variants[0]
	default:
		inner = &Interface{Variants: variants}
	}

	// A sum already holds a null: it is stored as `any` (§12), a nil interface is what
	// [encoding/json] decodes `null` into, and it encodes one back. Saying so a second
	// time with a presence wrapper costs more than it states — a declaration written over
	// `opt.Nullable[any]` carries no methods at all, since a defined type does not inherit
	// them from the type it is defined as, so it decodes nothing. The null goes in as an
	// alternative instead, where a renderer that one day gives a sum a real shape will
	// still find it.
	if nullable {
		switch inner.(type) {
		case *Any:
			nullable = false
		case *Interface:
			inner = &Interface{Variants: append(variants, &Primitive{Kind: PrimitiveNull})}
			nullable = false
		}
	}
	return withPresence(inner, false, nullable), nil
}
