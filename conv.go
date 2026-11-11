package kv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// FromJSON converts under the strict key grammar; see Dialect.FromJSON.
func FromJSON(data []byte) (*File, error) { return Strict.FromJSON(data) }

// FromJSON converts a JSON document to a keyval file. Objects become
// named children, arrays indexed children, empty containers markers.
// Scalars keep their literal JSON spelling — 8080, true, null — which
// ToJSON recognizes on the way back, so scalar types round-trip. A
// string that would be mistaken for one of those keeps its quotes:
// "8080". Object names must fit the dialect's key grammar; a dot in a
// name is escaped under JSONKeys and an error under Strict. The top-level value must be an object or an
// array; an empty one yields an empty file.
func (d Dialect) FromJSON(data []byte) (*File, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return nil, err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("trailing data after JSON value")
	}
	switch root.(type) {
	case map[string]any, []any:
	default:
		return nil, fmt.Errorf("top-level JSON value must be an object or array")
	}
	f := &File{Hash: HashLine}
	if err := fromJSON(root, "", f, d); err != nil {
		return nil, err
	}
	sortPairs(f)
	if err := validate(f); err != nil {
		return nil, err
	}
	return f, nil
}

func joinPath(path, seg string) string {
	if path == "" {
		return seg
	}
	return path + "." + seg
}

// emitLeaf appends a value pair at the dotted path, refusing control
// characters the format cannot represent. Shared by FromJSON and Marshal.
func emitLeaf(f *File, path, val string, d Dialect) error {
	if err := checkValue(val); err != nil {
		return fmt.Errorf("key %q: %v", path, err)
	}
	k, err := d.ParseKey(path)
	if err != nil {
		return err
	}
	f.Pairs = append(f.Pairs, Pair{Key: k, Value: val})
	return nil
}

// emitMarker appends a marker at the dotted path. An empty path is the
// empty top-level container and emits nothing: the empty file.
func emitMarker(f *File, path string, d Dialect) error {
	if path == "" {
		return nil
	}
	k, err := d.ParseKey(path)
	if err != nil {
		return err
	}
	f.Pairs = append(f.Pairs, Pair{Key: k, Marker: true})
	return nil
}

// nameSegment spells an object or mapping name as a key segment.
// JSONKeys escapes a dot; the strict grammar has no escape and rejects
// the name.
func nameSegment(path, name string, d Dialect) (string, error) {
	if d == JSONKeys {
		return EscapeName(name), nil
	}
	return name, checkName(path, name)
}

// checkName rejects an object or map name holding a dot: joined into the
// path it would silently split into structure instead.
func checkName(path, name string) error {
	if strings.Contains(name, ".") {
		return fmt.Errorf("key %q: name %q holds a dot, the path separator", path, name)
	}
	return nil
}

func fromJSON(v any, path string, f *File, d Dialect) error {
	switch x := v.(type) {
	case json.Number:
		return emitLeaf(f, path, x.String(), d)
	case string:
		return emitLeaf(f, path, quoteString(x), d)
	case bool:
		if x {
			return emitLeaf(f, path, "true", d)
		}
		return emitLeaf(f, path, "false", d)
	case nil:
		return emitLeaf(f, path, "null", d)
	case []any:
		if len(x) == 0 {
			return emitMarker(f, path, d)
		}
		w := len(strconv.Itoa(len(x) - 1))
		for i, item := range x {
			if err := fromJSON(item, joinPath(path, pad(i, w)), f, d); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		if len(x) == 0 {
			return emitMarker(f, path, d)
		}
		for name, item := range x {
			seg, err := nameSegment(path, name, d)
			if err != nil {
				return err
			}
			if err := fromJSON(item, joinPath(path, seg), f, d); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("key %q: unsupported JSON value %T", path, v)
}

// ToJSON converts a keyval file to indented JSON. A node with only
// indexed children becomes an array; any named child makes it an object,
// with indexed siblings keyed by their integer. Markers become {}, so an
// empty array does not survive a round trip. A leaf spelled exactly like
// a JSON number, true, false or null is emitted as that type, and one
// spelled like a JSON string literal, "8080", as the string inside the
// quotes. Everything else is a string as it stands.
func ToJSON(f *File) ([]byte, error) {
	out, err := json.MarshalIndent(materialize(buildTree(f), guessScalar), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// quoteString spells a string value as a leaf. A string that
// guessScalar would read as another type, or as a quoted string, is
// written as a JSON string literal so it returns as the same string.
func quoteString(s string) string {
	if g, ok := guessScalar(s).(string); ok && g == s {
		return s
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return s
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// guessScalar maps a leaf value back to a JSON type when it is spelled
// exactly like one. json.Valid is the number test: it rejects 08, 1.,
// and other near-numbers, so they stay strings. A JSON string literal
// yields its content.
func guessScalar(v string) any {
	switch v {
	case "true", "false", "null":
		return json.RawMessage(v)
	}
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		var s string
		if json.Unmarshal([]byte(v), &s) == nil {
			return s
		}
		return v
	}
	if v != "" && (v[0] == '-' || v[0] >= '0' && v[0] <= '9') &&
		!strings.ContainsAny(v, " \t\n\r") && json.Valid([]byte(v)) {
		return json.RawMessage(v)
	}
	return v
}
