package kv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// yamlMaxNodes bounds the nodes one conversion may visit. Aliases are
// expanded in place, so a small document can describe a huge tree.
const yamlMaxNodes = 1 << 20

// FromYAML converts under the strict key grammar; see Dialect.FromYAML.
func FromYAML(data []byte) (*File, error) { return Strict.FromYAML(data) }

// FromYAML converts a YAML stream to a keyval file, as FromJSON does
// for JSON: mappings become named children, sequences indexed children,
// empty containers markers. Scalars are written in their JSON spelling,
// so 0x10 becomes 16 and True becomes true. Comments are dropped and
// aliases and merge keys are expanded. A stream of two or more
// documents becomes a top-level sequence, one index per document; empty
// documents are skipped. A lone document must be a mapping or a
// sequence.
func (d Dialect) FromYAML(data []byte) (*File, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var docs []*yaml.Node
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		root := &doc
		if doc.Kind == yaml.DocumentNode {
			if len(doc.Content) == 0 {
				continue
			}
			root = doc.Content[0]
		}
		if root.Kind == yaml.ScalarNode && root.ShortTag() == "!!null" {
			continue
		}
		docs = append(docs, root)
	}
	f := &File{Hash: HashLine}
	w := &yamlWalk{f: f, d: d}
	switch len(docs) {
	case 0:
	case 1:
		if k := resolveAlias(docs[0]).Kind; k != yaml.MappingNode && k != yaml.SequenceNode {
			return nil, fmt.Errorf("top-level YAML value must be a mapping or sequence")
		}
		if err := w.walk(docs[0], "", 0); err != nil {
			return nil, err
		}
	default:
		width := len(strconv.Itoa(len(docs) - 1))
		for i, doc := range docs {
			if err := w.walk(doc, pad(i, width), 0); err != nil {
				return nil, err
			}
		}
	}
	sortPairs(f)
	if err := validate(f); err != nil {
		return nil, err
	}
	return f, nil
}

type yamlWalk struct {
	f     *File
	d     Dialect
	nodes int
}

func resolveAlias(n *yaml.Node) *yaml.Node {
	for i := 0; n.Kind == yaml.AliasNode && n.Alias != nil && i < 100; i++ {
		n = n.Alias
	}
	return n
}

func (w *yamlWalk) walk(n *yaml.Node, path string, depth int) error {
	if w.nodes++; w.nodes > yamlMaxNodes || depth > 1000 {
		return fmt.Errorf("key %q: YAML document too large or aliases nest too deep", path)
	}
	n = resolveAlias(n)
	switch n.Kind {
	case yaml.ScalarNode:
		v, err := yamlScalar(n)
		if err != nil {
			return fmt.Errorf("key %q: %v", path, err)
		}
		return emitLeaf(w.f, path, v, w.d)
	case yaml.SequenceNode:
		if len(n.Content) == 0 {
			return emitMarker(w.f, path, w.d)
		}
		width := len(strconv.Itoa(len(n.Content) - 1))
		for i, item := range n.Content {
			if err := w.walk(item, joinPath(path, pad(i, width)), depth+1); err != nil {
				return err
			}
		}
		return nil
	case yaml.MappingNode:
		names, items, err := yamlMapping(n, path, 0)
		if err != nil {
			return err
		}
		if len(names) == 0 {
			return emitMarker(w.f, path, w.d)
		}
		for i, name := range names {
			if err := checkName(path, name); err != nil {
				return err
			}
			if err := w.walk(items[i], joinPath(path, name), depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("key %q: unsupported YAML node", path)
}

// yamlMapping lists a mapping's names and values with merge keys, <<,
// expanded. A name written in the mapping wins over a merged one, and
// among merged mappings the first wins.
func yamlMapping(n *yaml.Node, path string, depth int) ([]string, []*yaml.Node, error) {
	if depth > 100 {
		return nil, nil, fmt.Errorf("key %q: merge keys nest too deep", path)
	}
	var names []string
	var items []*yaml.Node
	seen := map[string]bool{}
	var merges []*yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := resolveAlias(n.Content[i]), n.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			return nil, nil, fmt.Errorf("key %q: a mapping name must be a scalar", path)
		}
		if k.ShortTag() == "!!merge" {
			if m := resolveAlias(v); m.Kind == yaml.SequenceNode {
				merges = append(merges, m.Content...)
			} else {
				merges = append(merges, m)
			}
			continue
		}
		name, err := yamlScalar(k)
		if err != nil {
			return nil, nil, fmt.Errorf("key %q: %v", path, err)
		}
		if seen[name] {
			return nil, nil, fmt.Errorf("key %q: duplicate name %q", path, name)
		}
		seen[name] = true
		names = append(names, name)
		items = append(items, v)
	}
	for _, m := range merges {
		m = resolveAlias(m)
		if m.Kind != yaml.MappingNode {
			return nil, nil, fmt.Errorf("key %q: a merge key must name a mapping", path)
		}
		mn, mi, err := yamlMapping(m, path, depth+1)
		if err != nil {
			return nil, nil, err
		}
		for i, name := range mn {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
				items = append(items, mi[i])
			}
		}
	}
	return names, items, nil
}

// yamlScalar spells a scalar the way FromJSON would have received it:
// null, true, false and JSON numbers. Strings, timestamps and anything
// else keep their text.
func yamlScalar(n *yaml.Node) (string, error) {
	switch n.ShortTag() {
	case "!!null":
		return "null", nil
	case "!!bool":
		var b bool
		if err := n.Decode(&b); err != nil {
			return "", err
		}
		return strconv.FormatBool(b), nil
	case "!!int":
		if json.Valid([]byte(n.Value)) {
			return n.Value, nil
		}
		var v any
		if err := n.Decode(&v); err != nil {
			return "", err
		}
		return fmt.Sprint(v), nil
	case "!!float":
		if json.Valid([]byte(n.Value)) {
			return n.Value, nil
		}
		var v float64
		if err := n.Decode(&v); err != nil {
			return "", err
		}
		if math.IsInf(v, 0) || math.IsNaN(v) {
			return n.Value, nil
		}
		return strconv.FormatFloat(v, 'g', -1, 64), nil
	}
	return n.Value, nil
}

// ToYAML converts a keyval file to YAML with two-space indentation.
// The tree is shaped as ToJSON shapes it, and a leaf spelled like a
// JSON number, true, false or null is written as that type; every
// other leaf is a string. A top-level sequence of two or more members
// is written as a stream, one document per member, mirroring FromYAML.
func ToYAML(f *File) ([]byte, error) {
	root := materialize(buildTree(f), yamlGuessScalar)
	docs := []any{root}
	if arr, ok := root.([]any); ok && len(arr) > 1 {
		docs = arr
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	for _, doc := range docs {
		if err := enc.Encode(doc); err != nil {
			return nil, err
		}
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// yamlGuessScalar is guessScalar for the YAML encoder. A number keeps
// its spelling by travelling as a tagged scalar node.
func yamlGuessScalar(v string) any {
	raw, ok := guessScalar(v).(json.RawMessage)
	if !ok {
		return v
	}
	switch v {
	case "null":
		return nil
	case "true":
		return true
	case "false":
		return false
	}
	tag := "!!int"
	if strings.ContainsAny(v, ".eE") {
		tag = "!!float"
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: string(raw)}
}
