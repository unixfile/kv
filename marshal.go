package kv

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Marshal renders a Go value as a keyval file. Structs and maps become
// named children, slices and arrays indexed children, scalars leaves.
// Struct fields map to their lowercased name, overridden by a `kv:"name"`
// tag; `kv:"-"` skips a field. Nil pointers are omitted; empty maps and
// slices become markers. The top-level value must be a struct, map,
// slice or array.
func Marshal(v any) (*File, error) {
	rv, ok := deref(reflect.ValueOf(v))
	if !ok {
		return nil, fmt.Errorf("cannot marshal nil")
	}
	switch rv.Kind() {
	case reflect.Struct, reflect.Map, reflect.Slice, reflect.Array:
	default:
		return nil, fmt.Errorf("cannot marshal %s at top level", rv.Kind())
	}
	f := &File{Hash: HashLine}
	if err := encode(rv, "", f); err != nil {
		return nil, err
	}
	sortPairs(f)
	if err := validate(f); err != nil {
		return nil, err
	}
	return f, nil
}

// Unmarshal reads a keyval file into the Go value pointed to by v.
// Unknown keys are ignored; a leaf must match a scalar field, a branch a
// struct, map, slice or array. A marker yields the empty container. An
// empty interface target gets nested map[string]any, []any and string.
func Unmarshal(f *File, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("unmarshal target must be a non-nil pointer")
	}
	return decode(buildTree(f), rv.Elem(), "")
}

// deref follows pointers and interfaces to the concrete value. The bool
// is false when a nil is reached or the value is invalid.
func deref(rv reflect.Value) (reflect.Value, bool) {
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return rv, false
		}
		rv = rv.Elem()
	}
	return rv, rv.IsValid()
}

// fieldName resolves a struct field's segment name from its kv tag or
// lowercased Go name. The bool is false for skipped fields.
func fieldName(sf reflect.StructField) (string, bool) {
	if !sf.IsExported() {
		return "", false
	}
	if tag, ok := sf.Tag.Lookup("kv"); ok {
		if tag == "-" {
			return "", false
		}
		return tag, true
	}
	return strings.ToLower(sf.Name), true
}

func encode(rv reflect.Value, path string, f *File) error {
	rv, ok := deref(rv)
	if !ok {
		return nil // nil pointer or interface: omitted
	}
	switch rv.Kind() {
	case reflect.String:
		return emitLeaf(f, path, rv.String(), Strict)
	case reflect.Bool:
		if rv.Bool() {
			return emitLeaf(f, path, "true", Strict)
		}
		return emitLeaf(f, path, "false", Strict)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return emitLeaf(f, path, strconv.FormatInt(rv.Int(), 10), Strict)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return emitLeaf(f, path, strconv.FormatUint(rv.Uint(), 10), Strict)
	case reflect.Float32, reflect.Float64:
		bits := 64
		if rv.Kind() == reflect.Float32 {
			bits = 32
		}
		return emitLeaf(f, path, strconv.FormatFloat(rv.Float(), 'g', -1, bits), Strict)
	case reflect.Slice, reflect.Array:
		n := rv.Len()
		if n == 0 {
			return emitMarker(f, path, Strict)
		}
		w := len(strconv.Itoa(n - 1))
		for i := 0; i < n; i++ {
			if err := encode(rv.Index(i), joinPath(path, pad(i, w)), f); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return fmt.Errorf("key %q: map key type must be string", path)
		}
		if rv.Len() == 0 {
			return emitMarker(f, path, Strict)
		}
		keys := make([]string, 0, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			keys = append(keys, iter.Key().String())
		}
		sort.Strings(keys)
		for _, k := range keys {
			if err := checkName(path, k); err != nil {
				return err
			}
			mv := rv.MapIndex(reflect.ValueOf(k).Convert(rv.Type().Key()))
			if err := encode(mv, joinPath(path, k), f); err != nil {
				return err
			}
		}
		return nil
	case reflect.Struct:
		t := rv.Type()
		for i := range t.NumField() {
			name, ok := fieldName(t.Field(i))
			if !ok {
				continue
			}
			if err := checkName(path, name); err != nil {
				return err
			}
			if err := encode(rv.Field(i), joinPath(path, name), f); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("key %q: cannot marshal %s", path, rv.Kind())
}

func decode(n *node, rv reflect.Value, path string) error {
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			rv.Set(reflect.New(rv.Type().Elem()))
		}
		rv = rv.Elem()
	}
	if rv.Kind() == reflect.Interface && rv.NumMethod() == 0 {
		rv.Set(reflect.ValueOf(materialize(n, func(s string) any { return s })))
		return nil
	}
	if n.leaf {
		return decodeScalar(n.value, rv, path)
	}
	switch rv.Kind() {
	case reflect.Struct:
		t := rv.Type()
		for i := range t.NumField() {
			name, ok := fieldName(t.Field(i))
			if !ok {
				continue
			}
			if err := checkName(path, name); err != nil {
				return err
			}
			c := n.named[name]
			if c == nil {
				continue
			}
			if err := decode(c, rv.Field(i), joinPath(path, name)); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		t := rv.Type()
		if t.Key().Kind() != reflect.String {
			return fmt.Errorf("key %q: map key type must be string", path)
		}
		m := reflect.MakeMapWithSize(t, len(n.names)+len(n.indexed))
		set := func(key string, c *node) error {
			ev := reflect.New(t.Elem()).Elem()
			if err := decode(c, ev, joinPath(path, key)); err != nil {
				return err
			}
			m.SetMapIndex(reflect.ValueOf(key).Convert(t.Key()), ev)
			return nil
		}
		for _, name := range n.names {
			if err := set(name, n.named[name]); err != nil {
				return err
			}
		}
		for i, c := range n.indexed {
			if err := set(strconv.Itoa(i), c); err != nil {
				return err
			}
		}
		rv.Set(m)
		return nil
	case reflect.Slice:
		if len(n.names) > 0 {
			return fmt.Errorf("key %q: node has named children, target is a slice", path)
		}
		s := reflect.MakeSlice(rv.Type(), len(n.indexed), len(n.indexed))
		for i, c := range n.indexed {
			if err := decode(c, s.Index(i), joinPath(path, strconv.Itoa(i))); err != nil {
				return err
			}
		}
		rv.Set(s)
		return nil
	case reflect.Array:
		if len(n.names) > 0 {
			return fmt.Errorf("key %q: node has named children, target is an array", path)
		}
		for i, c := range n.indexed {
			if i >= rv.Len() {
				break
			}
			if err := decode(c, rv.Index(i), joinPath(path, strconv.Itoa(i))); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("key %q: cannot unmarshal branch into %s", path, rv.Kind())
}

func decodeScalar(val string, rv reflect.Value, path string) error {
	switch rv.Kind() {
	case reflect.String:
		rv.SetString(val)
		return nil
	case reflect.Bool:
		switch val {
		case "true":
			rv.SetBool(true)
			return nil
		case "false":
			rv.SetBool(false)
			return nil
		}
		return fmt.Errorf("key %q: %q is not a bool", path, val)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := strconv.ParseInt(val, 10, rv.Type().Bits())
		if err != nil {
			return fmt.Errorf("key %q: %v", path, err)
		}
		rv.SetInt(i)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := strconv.ParseUint(val, 10, rv.Type().Bits())
		if err != nil {
			return fmt.Errorf("key %q: %v", path, err)
		}
		rv.SetUint(u)
		return nil
	case reflect.Float32, reflect.Float64:
		fl, err := strconv.ParseFloat(val, rv.Type().Bits())
		if err != nil {
			return fmt.Errorf("key %q: %v", path, err)
		}
		rv.SetFloat(fl)
		return nil
	}
	return fmt.Errorf("key %q: cannot unmarshal value into %s", path, rv.Kind())
}
