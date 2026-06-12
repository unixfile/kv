// Package kv models, parses and serializes the keyval (.kv) format,
// spec v1.0. The library never touches the filesystem; callers own all
// file I/O.
package kv

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// HashLine identifies the format, pointing at its spec. Any first line
// starting with # is accepted on read; this exact line is written when
// none exists.
const HashLine = "# github.com/unixfile/keyval"

// Dialect selects the key grammar. Strict is the format's own grammar
// and the only one valid in a .kv file. JSONKeys also accepts the wider
// names of foreign JSON documents, kept verbatim: anything without a
// space, dot or control character, not starting with #. It exists for
// the JSON bridge — .json files and the JSON conversions — and must
// never reach a .kv file. The package-level functions are the Strict
// shorthand.
type Dialect int

const (
	Strict Dialect = iota
	JSONKeys
)

// Segment is one dotted-path component: a name or an index. The first
// character decides which. An index segment's digits denote its integer
// value; leading zeros are padding.
type Segment struct {
	Raw     string
	Name    string // name segments only
	Index   int    // index segments only
	IsIndex bool
}

func parseSegment(s string, d Dialect) (Segment, error) {
	if s == "" {
		return Segment{}, fmt.Errorf("empty segment")
	}
	if allDigits(s) {
		idx, err := strconv.Atoi(s)
		if err != nil {
			return Segment{}, fmt.Errorf("segment %q: bad index: %v", s, err)
		}
		return Segment{Raw: s, Index: idx, IsIndex: true}, nil
	}
	if d == JSONKeys {
		for i := 0; i < len(s); i++ {
			if s[i] <= ' ' || s[i] == 0x7f {
				return Segment{}, fmt.Errorf("segment %q: invalid character %q", s, string(s[i]))
			}
		}
		if s[0] == '#' {
			return Segment{}, fmt.Errorf("segment %q: a name may not start with #", s)
		}
		return Segment{Raw: s, Name: s}, nil
	}
	switch c := s[0]; {
	case c >= '0' && c <= '9':
		return Segment{}, fmt.Errorf("segment %q: bad index: not all digits", s)
	case c >= 'a' && c <= 'z':
		for i := 1; i < len(s); i++ {
			c := s[i]
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
				return Segment{}, fmt.Errorf("segment %q: invalid character %q", s, string(c))
			}
		}
		return Segment{Raw: s, Name: s}, nil
	}
	return Segment{}, fmt.Errorf("segment %q: must start with a-z or 0-9", s)
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// width is the digit count of an index segment including padding,
// 0 for a name.
func (s Segment) width() int {
	if !s.IsIndex {
		return 0
	}
	return len(s.Raw)
}

func sameNode(a, b Segment) bool {
	if a.IsIndex != b.IsIndex {
		return false
	}
	if a.IsIndex {
		return a.Index == b.Index
	}
	return a.Name == b.Name
}

type Key []Segment

func ParseKey(s string) (Key, error) { return Strict.ParseKey(s) }

// ParseKey parses a dotted path under the dialect's key grammar.
func (d Dialect) ParseKey(s string) (Key, error) {
	parts := strings.Split(s, ".")
	k := make(Key, 0, len(parts))
	for _, p := range parts {
		seg, err := parseSegment(p, d)
		if err != nil {
			return nil, fmt.Errorf("key %q: %v", s, err)
		}
		k = append(k, seg)
	}
	return k, nil
}

func (k Key) String() string {
	raw := make([]string, len(k))
	for i, s := range k {
		raw[i] = s.Raw
	}
	return strings.Join(raw, ".")
}

// hasNodePrefix reports whether prefix names k or an ancestor of k,
// comparing nodes (name+index), so name and name0 are the same node.
func hasNodePrefix(k, prefix Key) bool {
	if len(k) < len(prefix) {
		return false
	}
	for i := range prefix {
		if !sameNode(k[i], prefix[i]) {
			return false
		}
	}
	return true
}

type Pair struct {
	Key    Key
	Value  string // unescaped; empty and ignored for markers
	Marker bool   // container marker: no value in file, auto-removed when children arrive
}

type File struct {
	Hash  string // first line if it starts with #, verbatim
	Pairs []Pair // kept sorted by key string
}

// checkValue rejects control characters the format cannot represent:
// everything below 0x20 except \n, \t and \r, which have escapes. The
// mutating operations and the converters share this gate, so a value a
// File accepts always survives String and a reparse.
func checkValue(v string) error {
	for i := 0; i < len(v); i++ {
		if c := v[i]; c < 0x20 && c != '\n' && c != '\t' && c != '\r' {
			return fmt.Errorf("value has unrepresentable control character 0x%02x", c)
		}
	}
	return nil
}

// escapeValue renders a raw value for a file line. The \0 marker is
// appended when needed to survive the trailing-whitespace trim on read.
func escapeValue(v string) string {
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		switch v[i] {
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteByte(v[i])
		}
	}
	s := b.String()
	if s == "" || s[len(s)-1] == ' ' {
		s += `\0`
	}
	return s
}

func unescapeValue(raw string) (string, error) {
	raw = strings.TrimRight(raw, " ")
	var b strings.Builder
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c != '\\' {
			if c < 0x20 {
				return "", fmt.Errorf("raw control character 0x%02x in value", c)
			}
			b.WriteByte(c)
			continue
		}
		i++
		if i == len(raw) {
			return "", fmt.Errorf("trailing backslash")
		}
		switch raw[i] {
		case '\\':
			b.WriteByte('\\')
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '0':
			if i != len(raw)-1 {
				return "", fmt.Errorf(`\0 marker not at end of value`)
			}
		default:
			return "", fmt.Errorf(`unknown escape \%s`, string(raw[i]))
		}
	}
	return b.String(), nil
}

// Parse parses file content strictly: out-of-order lines, stale markers
// and every other spec violation are errors.
func Parse(data string) (*File, error) {
	return Strict.Parse(data)
}

// ParseLenient parses file content the way fmt does: lines are sorted
// and stale markers dropped before validation, so an appended file
// normalizes instead of failing.
func ParseLenient(data string) (*File, error) {
	return Strict.ParseLenient(data)
}

// Parse is Parse under the dialect's key grammar.
func (d Dialect) Parse(data string) (*File, error) {
	return parseFile(data, true, d)
}

// ParseLenient is ParseLenient under the dialect's key grammar.
func (d Dialect) ParseLenient(data string) (*File, error) {
	return parseFile(data, false, d)
}

// parseFile parses file content. With sorted set, out-of-order lines are
// an error; otherwise the pairs are sorted (fmt's lenient mode).
func parseFile(data string, sorted bool, d Dialect) (*File, error) {
	f := &File{}
	data = strings.TrimSuffix(data, "\n")
	if data == "" {
		return f, nil
	}
	for n, line := range strings.Split(data, "\n") {
		if n == 0 && strings.HasPrefix(line, "#") {
			f.Hash = line
			continue
		}
		if line == "" {
			return nil, fmt.Errorf("line %d: empty line", n+1)
		}
		if strings.HasPrefix(line, "#") {
			return nil, fmt.Errorf("line %d: # is only allowed on the first line", n+1)
		}
		sp := strings.IndexByte(line, ' ')
		rawKey := line
		if sp >= 0 {
			rawKey = line[:sp]
		}
		key, err := d.ParseKey(rawKey)
		if err != nil {
			return nil, fmt.Errorf("line %d: %v", n+1, err)
		}
		if sp < 0 {
			f.Pairs = append(f.Pairs, Pair{Key: key, Marker: true})
		} else {
			val, err := unescapeValue(line[sp+1:])
			if err != nil {
				return nil, fmt.Errorf("line %d: %v", n+1, err)
			}
			f.Pairs = append(f.Pairs, Pair{Key: key, Value: val})
		}
	}
	if !sorted {
		sortPairs(f)
		removeStaleMarkers(f)
	} else {
		if err := checkStaleMarkers(f); err != nil {
			return nil, err
		}
	}
	if err := validate(f); err != nil {
		return nil, err
	}
	return f, nil
}

func sortPairs(f *File) {
	sort.Slice(f.Pairs, func(i, j int) bool {
		return f.Pairs[i].Key.String() < f.Pairs[j].Key.String()
	})
}

type nodeInfo struct {
	raw       string
	leaf      bool
	branch    bool
	nextIndex int // next expected value for indexed children
}

// validate checks the sorted-pair invariants: strict lexicographic order,
// one raw spelling per node, no leaf/branch mixing, and per-parent
// indexed children consecutive from 0 in ascending order.
func validate(f *File) error {
	nodes := map[string]*nodeInfo{"": {}}
	prev := ""
	for i, p := range f.Pairs {
		ks := p.Key.String()
		if i > 0 && prev >= ks {
			return fmt.Errorf("key %q: out of order after %q", ks, prev)
		}
		prev = ks
		id := ""
		for j, seg := range p.Key {
			parent := nodes[id]
			// the joints are control bytes no name may hold in any
			// dialect, so distinct paths cannot collide
			if seg.IsIndex {
				id += "\x01" + strconv.Itoa(seg.Index)
			} else {
				id += "\x00" + seg.Name
			}
			info := nodes[id]
			if info == nil {
				if seg.IsIndex && seg.Index != parent.nextIndex {
					return fmt.Errorf("key %q: index %d out of sequence, expected %d", ks, seg.Index, parent.nextIndex)
				}
				if seg.IsIndex {
					parent.nextIndex++
				}
				info = &nodeInfo{raw: seg.Raw}
				nodes[id] = info
			} else if info.raw != seg.Raw {
				return fmt.Errorf("key %q: node written as both %q and %q", ks, info.raw, seg.Raw)
			}
			if j == len(p.Key)-1 {
				if p.Marker {
					if info.leaf {
						return fmt.Errorf("key %q: node is both leaf and branch", ks)
					}
					if info.branch {
						return fmt.Errorf("key %q: duplicate container marker", ks)
					}
					info.branch = true
				} else {
					if info.leaf {
						return fmt.Errorf("key %q: duplicate leaf", ks)
					}
					if info.branch {
						return fmt.Errorf("key %q: node is both leaf and branch", ks)
					}
					info.leaf = true
				}
			} else {
				if info.leaf {
					return fmt.Errorf("key %q: %q is both leaf and branch", ks, seg.Raw)
				}
				info.branch = true
			}
		}
	}
	return nil
}

func (f *File) String() string {
	var b strings.Builder
	if f.Hash != "" {
		b.WriteString(f.Hash)
		b.WriteByte('\n')
	}
	for _, p := range f.Pairs {
		b.WriteString(p.Key.String())
		if !p.Marker {
			b.WriteByte(' ')
			b.WriteString(escapeValue(p.Value))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// removeStaleMarkers removes any marker whose path is a proper prefix of
// a non-marker pair. Used by lenient (fmt) parsing only.
func removeStaleMarkers(f *File) {
	prefixes := stalePrefixes(f)
	if len(prefixes) == 0 {
		return
	}
	keep := make([]Pair, 0, len(f.Pairs))
	for _, p := range f.Pairs {
		if !p.Marker || !prefixes[p.Key.String()] {
			keep = append(keep, p)
		}
	}
	f.Pairs = keep
}

// checkStaleMarkers returns an error if any marker has children; used by
// strict parsing where a stale marker means the file needs normalizing.
func checkStaleMarkers(f *File) error {
	prefixes := stalePrefixes(f)
	for _, p := range f.Pairs {
		if p.Marker && prefixes[p.Key.String()] {
			return fmt.Errorf("key %q: stale marker alongside children; run fmt to normalize", p.Key.String())
		}
	}
	return nil
}

func stalePrefixes(f *File) map[string]bool {
	prefixes := map[string]bool{}
	for _, p := range f.Pairs {
		if p.Marker {
			continue
		}
		for i := 1; i < len(p.Key); i++ {
			prefixes[Key(p.Key[:i]).String()] = true
		}
	}
	return prefixes
}
