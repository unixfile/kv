package kv

import "strconv"

// node is a materialized tree built from a File's sorted pairs, shared
// by the converters. Named children keep first-occurrence order, which
// for a valid file is sorted order; indexed children sit at their
// integer positions.
type node struct {
	leaf    bool
	value   string
	marker  bool
	names   []string
	named   map[string]*node
	indexed []*node
}

// buildTree materializes f. The file is assumed valid: sorted pairs,
// consecutive indices, no leaf/branch conflicts.
func buildTree(f *File) *node {
	root := &node{}
	for _, p := range f.Pairs {
		n := root
		for _, seg := range p.Key {
			n = n.child(seg)
		}
		if p.Marker {
			n.marker = true
		} else {
			n.leaf = true
			n.value = p.Value
		}
	}
	return root
}

// materialize renders the tree as nested Go values: a node with only
// indexed children becomes []any, any named child makes it a
// map[string]any with indexed siblings keyed by their integer, and a
// marker becomes an empty map. Leaves go through scalar.
func materialize(n *node, scalar func(string) any) any {
	if n.leaf {
		return scalar(n.value)
	}
	if len(n.named) == 0 {
		if len(n.indexed) == 0 {
			return map[string]any{} // marker, or the empty file at root
		}
		arr := make([]any, len(n.indexed))
		for i, c := range n.indexed {
			arr[i] = materialize(c, scalar)
		}
		return arr
	}
	obj := make(map[string]any, len(n.named)+len(n.indexed))
	for i, c := range n.indexed {
		obj[strconv.Itoa(i)] = materialize(c, scalar)
	}
	for name, c := range n.named {
		obj[name] = materialize(c, scalar)
	}
	return obj
}

func (n *node) child(seg Segment) *node {
	if seg.IsIndex {
		for len(n.indexed) <= seg.Index {
			n.indexed = append(n.indexed, &node{})
		}
		return n.indexed[seg.Index]
	}
	if n.named == nil {
		n.named = map[string]*node{}
	}
	c := n.named[seg.Name]
	if c == nil {
		c = &node{}
		n.named[seg.Name] = c
		n.names = append(n.names, seg.Name)
	}
	return c
}
