// The crudpP operations, plus tree rendering. All mutations end in
// finish, which restores the sort invariant and revalidates; a failed
// validation rolls back and nothing is written to disk.

package kv

import (
	"fmt"
	"strings"
)

// snapshot deep-copies the pairs (shiftDown and push rewrite segments in
// place) so a failed mutation can be rolled back.
func snapshot(f *File) []Pair {
	out := make([]Pair, len(f.Pairs))
	for i, p := range f.Pairs {
		k := make(Key, len(p.Key))
		copy(k, p.Key)
		out[i] = Pair{Key: k, Value: p.Value, Marker: p.Marker}
	}
	return out
}

func finish(f *File, saved []Pair) error {
	removeStaleMarkers(f)
	sortPairs(f)
	if err := validate(f); err != nil {
		f.Pairs = saved
		return err
	}
	return nil
}

func pad(idx, width int) string {
	return fmt.Sprintf("%0*d", width, idx)
}

// findLeaf returns the index of the pair whose key names the same node
// path as key, or -1. Index segments match by value, so x.0 finds x.00.
func findLeaf(f *File, key Key) int {
	for i, p := range f.Pairs {
		if !p.Marker && len(p.Key) == len(key) && hasNodePrefix(p.Key, key) {
			return i
		}
	}
	return -1
}

// subtree returns the indices of all pairs at or under key.
func subtree(f *File, key Key) []int {
	var out []int
	for i, p := range f.Pairs {
		if hasNodePrefix(p.Key, key) {
			out = append(out, i)
		}
	}
	return out
}

func removePairs(f *File, idx []int) {
	keep := f.Pairs[:0]
	skip := map[int]bool{}
	for _, i := range idx {
		skip[i] = true
	}
	for i, p := range f.Pairs {
		if !skip[i] {
			keep = append(keep, p)
		}
	}
	f.Pairs = keep
}

// adoptRaw replaces segment spellings in key with those already in the
// file, deepest existing prefix first, so new pairs don't introduce
// conflicting paddings (user writes x.0 where the file spells it x.00).
func adoptRaw(f *File, key Key) Key {
	out := make(Key, len(key))
	copy(out, key)
	best := 0
	var from Key
	for _, p := range f.Pairs {
		n := min(len(p.Key), len(key))
		m := 0
		for m < n && sameNode(p.Key[m], out[m]) {
			m++
		}
		if m > best {
			best, from = m, p.Key
		}
	}
	for i := range best {
		out[i].Raw = from[i].Raw
	}
	return out
}

// shiftDown renumbers the removed node's higher-indexed siblings down by
// one, rewriting that segment in every pair under each sibling. Each
// segment keeps its digit width. Removing a named node shifts nothing.
func shiftDown(f *File, key Key) {
	d := len(key) - 1
	rm := key[d]
	if !rm.IsIndex {
		return
	}
	for i := range f.Pairs {
		k := f.Pairs[i].Key
		if len(k) <= d || !hasNodePrefix(k[:d], key[:d]) {
			continue
		}
		s := &k[d]
		if !s.IsIndex || s.Index <= rm.Index {
			continue
		}
		w := s.width()
		s.Index--
		s.Raw = pad(s.Index, w)
	}
}

func (f *File) Create(key Key, val string) error {
	if key[len(key)-1].IsIndex {
		return fmt.Errorf("cannot create an index directly, use push: %s", key)
	}
	if err := checkValue(val); err != nil {
		return fmt.Errorf("key %s: %v", key, err)
	}
	if findLeaf(f, key) >= 0 {
		return fmt.Errorf("key exists: %s", key)
	}
	for _, p := range f.Pairs {
		if p.Marker && len(p.Key) == len(key) && hasNodePrefix(p.Key, key) {
			return fmt.Errorf("key is a container: %s", key)
		}
		if !p.Marker && hasNodePrefix(p.Key, key) && len(p.Key) >= len(key) {
			return fmt.Errorf("key is a branch: %s", key)
		}
	}
	saved := snapshot(f)
	f.Pairs = append(f.Pairs, Pair{Key: adoptRaw(f, key), Value: val})
	return finish(f, saved)
}

// Flat returns all leaf pairs as flat key-value lines in sorted order,
// the grep-friendly view of the file.
func (f *File) Flat() string {
	var b strings.Builder
	for _, p := range f.Pairs {
		if !p.Marker {
			fmt.Fprintf(&b, "%s %s\n", p.Key, escapeValue(p.Value))
		}
	}
	return b.String()
}

func (f *File) Read(key Key) (string, error) {
	if i := findLeaf(f, key); i >= 0 {
		return f.Pairs[i].Value + "\n", nil
	}
	idx := subtree(f, key)
	if len(idx) == 0 {
		return "", fmt.Errorf("no such key: %s", key)
	}
	var b strings.Builder
	for _, i := range idx {
		p := f.Pairs[i]
		if !p.Marker {
			fmt.Fprintf(&b, "%s %s\n", p.Key, escapeValue(p.Value))
		}
	}
	return b.String(), nil
}

// opCreateMarker declares an empty container at key. The marker is
// removed automatically when the first child is written.
func (f *File) CreateMarker(key Key) error {
	if findLeaf(f, key) >= 0 {
		return fmt.Errorf("key is a leaf: %s", key)
	}
	for _, p := range f.Pairs {
		if p.Marker && len(p.Key) == len(key) && hasNodePrefix(p.Key, key) {
			return fmt.Errorf("container already marked: %s", key)
		}
		if !p.Marker && hasNodePrefix(p.Key, key) && len(p.Key) > len(key) {
			return fmt.Errorf("key already has children: %s", key)
		}
	}
	saved := snapshot(f)
	f.Pairs = append(f.Pairs, Pair{Key: adoptRaw(f, key), Marker: true})
	return finish(f, saved)
}

func (f *File) Update(key Key, val string) error {
	if err := checkValue(val); err != nil {
		return fmt.Errorf("key %s: %v", key, err)
	}
	i := findLeaf(f, key)
	if i < 0 {
		if len(subtree(f, key)) > 0 {
			return fmt.Errorf("key is a branch: %s", key)
		}
		return fmt.Errorf("no such key: %s", key)
	}
	f.Pairs[i].Value = val
	return nil
}

// Set creates the leaf at key or overwrites an existing one, the upsert
// behind the = operator.
func (f *File) Set(key Key, val string) error {
	if findLeaf(f, key) >= 0 {
		return f.Update(key, val)
	}
	return f.Create(key, val)
}

func (f *File) Delete(key Key) error {
	i := findLeaf(f, key)
	if i < 0 {
		if len(subtree(f, key)) > 0 {
			return fmt.Errorf("key is a branch, use rmtree: %s", key)
		}
		return fmt.Errorf("no such key: %s", key)
	}
	saved := snapshot(f)
	removePairs(f, []int{i})
	shiftDown(f, key)
	return finish(f, saved)
}

func (f *File) DeleteTree(key Key) error {
	idx := subtree(f, key)
	if len(idx) == 0 {
		return fmt.Errorf("no such key: %s", key)
	}
	saved := snapshot(f)
	removePairs(f, idx)
	shiftDown(f, key)
	return finish(f, saved)
}

// seqMembers reports the member count and widest digit run of the
// indexed children of the container at key. A valid file has consecutive
// indices, so the count is the highest index plus one.
func seqMembers(f *File, key Key) (count, width int) {
	d := len(key)
	for _, p := range f.Pairs {
		if len(p.Key) <= d || !hasNodePrefix(p.Key[:d], key) {
			continue
		}
		s := p.Key[d]
		if !s.IsIndex {
			continue
		}
		if s.Index+1 > count {
			count = s.Index + 1
		}
		if s.width() > width {
			width = s.width()
		}
	}
	return count, width
}

// opPush appends a new leaf to the indexed children of the container at
// key. Missing intermediate nodes are created implicitly; validation
// keeps every sequence consecutive.
func (f *File) Push(key Key, val string) error {
	if err := checkValue(val); err != nil {
		return fmt.Errorf("key %s: %v", key, err)
	}
	if findLeaf(f, key) >= 0 {
		return fmt.Errorf("key is a leaf: %s", key)
	}
	count, width := seqMembers(f, key)
	if width == 0 {
		width = 1
	}
	saved := snapshot(f)
	newIdx := count
	if len(fmt.Sprint(newIdx)) > width {
		width = len(fmt.Sprint(newIdx))
		d := len(key)
		for i := range f.Pairs {
			k := f.Pairs[i].Key
			if len(k) <= d || !hasNodePrefix(k[:d], key) {
				continue
			}
			s := &k[d]
			if !s.IsIndex {
				continue
			}
			s.Raw = pad(s.Index, width)
		}
	}
	seg := Segment{Raw: pad(newIdx, width), Index: newIdx, IsIndex: true}
	nk := append(adoptRaw(f, key), seg)
	f.Pairs = append(f.Pairs, Pair{Key: nk, Value: val})
	return finish(f, saved)
}

// opPop removes the highest-indexed child of the container at key and
// returns what was removed: the value for a leaf, the raw lines for a
// branch.
func (f *File) Pop(key Key) (string, error) {
	count, _ := seqMembers(f, key)
	if count == 0 {
		return "", fmt.Errorf("no sequence at: %s", key)
	}
	member := append(append(Key{}, key...), Segment{Index: count - 1, IsIndex: true})
	out, err := f.Read(member)
	if err != nil {
		return "", err
	}
	saved := snapshot(f)
	removePairs(f, subtree(f, member))
	return out, finish(f, saved)
}

// Keys lists the immediate children of the node at prefix as full
// dotted paths in file spelling, one per line in sorted order, with a
// trailing dot on containers. A nil prefix lists the top level; a leaf
// prefix prints the leaf itself.
func (f *File) Keys(prefix Key) (string, error) {
	if len(prefix) > 0 {
		if i := findLeaf(f, prefix); i >= 0 {
			return f.Pairs[i].Key.String() + "\n", nil
		}
		if len(subtree(f, prefix)) == 0 {
			return "", fmt.Errorf("no such key: %s", prefix)
		}
	}
	d := len(prefix)
	var b strings.Builder
	var last Key
	for _, p := range f.Pairs {
		if len(p.Key) <= d || !hasNodePrefix(p.Key, prefix) {
			continue
		}
		child := p.Key[:d+1]
		if last != nil && sameNode(last[d], child[d]) {
			continue
		}
		last = child
		b.WriteString(child.String())
		// the first pair under a child is the child itself when it is
		// a leaf or marker; anything longer means a branch
		if len(p.Key) > d+1 || p.Marker {
			b.WriteByte('.')
		}
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// Tree renders the file as an indented tree, two spaces per level, with
// escaped values on the leaves. A non-empty prefix scopes the output to
// that subtree, re-rooted so the named node sits in the first column; a
// leaf prefix prints its single line. An unknown prefix is an error.
func (f *File) Tree(prefix Key) (string, error) {
	idx := subtree(f, prefix)
	if len(prefix) > 0 && len(idx) == 0 {
		return "", fmt.Errorf("no such key: %s", prefix)
	}
	start := 0
	if len(prefix) > 0 {
		start = len(prefix) - 1
	}
	var b strings.Builder
	var prev Key
	for _, i := range idx {
		p := f.Pairs[i]
		common := 0
		for common < len(prev) && common < len(p.Key)-1 && sameNode(prev[common], p.Key[common]) {
			common++
		}
		if common < start {
			common = start
		}
		for d := common; d < len(p.Key); d++ {
			b.WriteString(strings.Repeat("  ", d-start))
			b.WriteString(p.Key[d].Raw)
			if d == len(p.Key)-1 {
				if p.Marker {
					b.WriteByte('.')
				} else {
					b.WriteByte(' ')
					b.WriteString(escapeValue(p.Value))
				}
			}
			b.WriteByte('\n')
		}
		prev = p.Key
	}
	return b.String(), nil
}
