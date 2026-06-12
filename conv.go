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
// ToJSON recognizes on the way back, so scalar types round-trip. Object
// names must fit the dialect's key grammar and may never hold a dot,
// the path separator. The top-level value must be an object or an
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
		return emitLeaf(f, path, x, d)
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
			if err := checkName(path, name); err != nil {
				return err
			}
			if err := fromJSON(item, joinPath(path, name), f, d); err != nil {
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
// a JSON number, true, false or null is emitted as that type, everything
// else as a string; the guessing mirrors FromJSON and is the documented
// edge of this lossy conversion.
func ToJSON(f *File) ([]byte, error) {
	out, err := json.MarshalIndent(materialize(buildTree(f), guessScalar), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// guessScalar maps a leaf value back to a JSON type when it is spelled
// exactly like one. json.Valid is the number test: it rejects 08, 1.,
// and other near-numbers, so they stay strings.
func guessScalar(v string) any {
	switch v {
	case "true", "false", "null":
		return json.RawMessage(v)
	}
	if v != "" && (v[0] == '-' || v[0] >= '0' && v[0] <= '9') &&
		!strings.ContainsAny(v, " \t\n\r") && json.Valid([]byte(v)) {
		return json.RawMessage(v)
	}
	return v
}
