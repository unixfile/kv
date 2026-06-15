package kv

import (
	"strings"
	"testing"
)

func TestParseSegment(t *testing.T) {
	cases := []struct {
		in      string
		name    string
		index   int
		isIndex bool
	}{
		{"name", "name", 0, false},
		{"md5", "md5", 0, false},
		{"k8s", "k8s", 0, false},
		{"sha256", "sha256", 0, false},
		{"first_name", "first_name", 0, false},
		{"x9", "x9", 0, false},
		{"0", "", 0, true},
		{"7", "", 7, true},
		{"31", "", 31, true},
		{"007", "", 7, true},
	}
	for _, c := range cases {
		seg, err := parseSegment(c.in, Strict)
		if err != nil {
			t.Errorf("parseSegment(%q): %v", c.in, err)
			continue
		}
		if seg.Name != c.name || seg.Index != c.index || seg.IsIndex != c.isIndex {
			t.Errorf("parseSegment(%q) = %+v, want name %q index %d isIndex %v",
				c.in, seg, c.name, c.index, c.isIndex)
		}
	}
	for _, in := range []string{"", "Name", "9a", "a-b", "_a", "a b", "0_", "-1"} {
		if _, err := parseSegment(in, Strict); err == nil {
			t.Errorf("parseSegment(%q): expected error", in)
		}
	}
}

func TestEscapeRoundTrip(t *testing.T) {
	values := []string{
		"", "a", "a ", "a  ", "  a", "a\nb", "a\tb", "a\rb",
		"a\\b", "a\\", `a\0b`, "a \\", " ", "räksmörgås",
	}
	for _, v := range values {
		esc := escapeValue(v)
		if strings.ContainsAny(esc, "\n\t\r") {
			t.Errorf("escapeValue(%q) = %q: contains raw control char", v, esc)
		}
		got, err := unescapeValue(esc)
		if err != nil {
			t.Errorf("unescapeValue(%q): %v", esc, err)
			continue
		}
		if got != v {
			t.Errorf("round trip %q -> %q -> %q", v, esc, got)
		}
	}
}

func TestUnescapeErrors(t *testing.T) {
	for _, in := range []string{`a\0b`, `a\x`, `a\`, "a\tb", "a\rb"} {
		if _, err := unescapeValue(in); err == nil {
			t.Errorf("unescapeValue(%q): expected error", in)
		}
	}
}

func TestUnescapeTrim(t *testing.T) {
	cases := []struct{ in, want string }{
		{"abc  ", "abc"},
		{`ab  \0`, "ab  "},
		{`\0`, ""},
		{"", ""},
	}
	for _, c := range cases {
		got, err := unescapeValue(c.in)
		if err != nil {
			t.Errorf("unescapeValue(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("unescapeValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseFileValid(t *testing.T) {
	in := "# github.com/unixfile/kv\n" +
		"person.0.name.0 Charles\n" +
		"person.0.name.1 Ingvar\n" +
		"person.1.name.0 Anna\n"
	f, err := parseFile(in, true, Strict)
	if err != nil {
		t.Fatal(err)
	}
	if f.String() != in {
		t.Errorf("round trip:\n%q\nwant\n%q", f.String(), in)
	}
}

func TestMixedChildren(t *testing.T) {
	in := "build.0 configure\nbuild.1 make\nbuild.parallel yes\n"
	f, err := parseFile(in, true, Strict)
	if err != nil {
		t.Fatal(err)
	}
	if f.String() != in {
		t.Errorf("round trip:\n%q\nwant\n%q", f.String(), in)
	}
}

func TestParseFileErrors(t *testing.T) {
	cases := map[string]string{
		"out of order":      "b x\na y\n",
		"duplicate":         "a x\na y\n",
		"leaf and branch":   "a x\na.b y\n",
		"leaf and sequence": "name x\nname.0 y\n",
		"index gap":         "n.0 x\nn.2 y\n",
		"starts at 1":       "n.1 x\n",
		"empty line":        "a x\n\nb y\n",
		"hash mid-file":     "a x\n# foo\n",
		"uppercase":         "Name x\n",
		"padding mismatch":  "x.0.a 1\nx.00.b 2\n",
		"unpadded ten":      "n.0 a\nn.1 a\nn.10 a\nn.2 a\nn.3 a\nn.4 a\nn.5 a\nn.6 a\nn.7 a\nn.8 a\nn.9 a\n",
	}
	for name, in := range cases {
		if _, err := parseFile(in, true, Strict); err == nil {
			t.Errorf("%s: expected error for %q", name, in)
		}
	}
}

func mustKey(t *testing.T, s string) Key {
	t.Helper()
	k, err := ParseKey(s)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestPaddedIndexLookup(t *testing.T) {
	// over-padded single element is legal; lookup matches by value
	f, err := parseFile("x.00 v\n", true, Strict)
	if err != nil {
		t.Fatal(err)
	}
	out, err := f.Read(mustKey(t, "x.0"))
	if err != nil || out != "v\n" {
		t.Errorf("read x.0 against x.00 = %q, %v", out, err)
	}
}

func TestOps(t *testing.T) {
	f := &File{Hash: HashLine}

	// push creates intermediate nodes, including the next index position
	if err := f.Push(mustKey(t, "person.0.name"), "Anna"); err != nil {
		t.Fatal(err)
	}
	out, err := f.Read(mustKey(t, "person.0.name.0"))
	if err != nil || out != "Anna\n" {
		t.Fatalf("read pushed = %q, %v", out, err)
	}

	if err := f.Create(mustKey(t, "person.0.name.1"), "x"); err == nil {
		t.Error("create with index last: expected error")
	}
	if err := f.Create(mustKey(t, "person.0.age"), "30"); err != nil {
		t.Fatal(err)
	}
	if err := f.Create(mustKey(t, "person.0.age"), "31"); err == nil {
		t.Error("create existing: expected error")
	}
	if err := f.Create(mustKey(t, "person.0.name"), "x"); err == nil {
		t.Error("create on branch: expected error")
	}
	if err := f.Create(mustKey(t, "person.2.age"), "x"); err == nil {
		t.Error("create with index gap: expected error")
	}

	if err := f.Update(mustKey(t, "person.0.name.0"), "Anna-Lena"); err != nil {
		t.Fatal(err)
	}
	if err := f.Update(mustKey(t, "person.0.email"), "x"); err == nil {
		t.Error("update absent: expected error")
	}

	if err := f.Push(mustKey(t, "person.0.name"), "Svensson"); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(mustKey(t, "person.0.name.0"), "x"); err == nil {
		t.Error("push to leaf: expected error")
	}

	got, err := f.Pop(mustKey(t, "person.0.name"))
	if err != nil || got != "Svensson\n" {
		t.Errorf("pop = %q, %v", got, err)
	}

	if err := f.Delete(mustKey(t, "person.0")); err == nil {
		t.Error("delete branch: expected error")
	}
	if err := f.DeleteTree(mustKey(t, "person")); err != nil {
		t.Fatal(err)
	}
	if len(f.Pairs) != 0 {
		t.Errorf("expected empty file, got %q", f.String())
	}
}

func TestDeleteRenumbers(t *testing.T) {
	f, err := parseFile("n.0 a\nn.1 b\nn.2 c\n", true, Strict)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Delete(mustKey(t, "n.1")); err != nil {
		t.Fatal(err)
	}
	if f.String() != "n.0 a\nn.1 c\n" {
		t.Errorf("after delete:\n%q", f.String())
	}
}

func TestDeleteNameNoRenumber(t *testing.T) {
	f, err := parseFile("x.alpha 1\nx.beta 2\n", true, Strict)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Delete(mustKey(t, "x.alpha")); err != nil {
		t.Fatal(err)
	}
	if f.String() != "x.beta 2\n" {
		t.Errorf("after delete:\n%q", f.String())
	}
}

func TestRmtreeRenumbers(t *testing.T) {
	in := "person.0.name.0 Anna\nperson.1.name.0 Bo\nperson.2.name.0 Cilla\n"
	f, err := parseFile(in, true, Strict)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.DeleteTree(mustKey(t, "person.1")); err != nil {
		t.Fatal(err)
	}
	want := "person.0.name.0 Anna\nperson.1.name.0 Cilla\n"
	if f.String() != want {
		t.Errorf("after rmtree:\n%q\nwant\n%q", f.String(), want)
	}
}

func TestPushRepads(t *testing.T) {
	f := &File{}
	for i := range 11 {
		if err := f.Push(mustKey(t, "n"), "x"); err != nil {
			t.Fatalf("push %d: %v", i, err)
		}
	}
	keys := make([]string, len(f.Pairs))
	for i, p := range f.Pairs {
		keys[i] = p.Key.String()
	}
	got := strings.Join(keys, ",")
	want := "n.00,n.01,n.02,n.03,n.04,n.05,n.06,n.07,n.08,n.09,n.10"
	if got != want {
		t.Errorf("after 11 pushes: %s, want %s", got, want)
	}
}

func TestPushCreatesIntermediates(t *testing.T) {
	f, err := parseFile("person.0.name.0 Anna\n", true, Strict)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Push(mustKey(t, "person.1.name"), "Bo"); err != nil {
		t.Fatal(err)
	}
	want := "person.0.name.0 Anna\nperson.1.name.0 Bo\n"
	if f.String() != want {
		t.Errorf("after push:\n%q\nwant\n%q", f.String(), want)
	}
	if err := f.Push(mustKey(t, "person.3.name"), "x"); err == nil {
		t.Error("push with index gap: expected error")
	}
}

func TestNestedPush(t *testing.T) {
	f := &File{}
	if err := f.Push(mustKey(t, "m.0"), "a"); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(mustKey(t, "m.0"), "b"); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(mustKey(t, "m.1"), "c"); err != nil {
		t.Fatal(err)
	}
	want := "m.0.0 a\nm.0.1 b\nm.1.0 c\n"
	if f.String() != want {
		t.Errorf("nested push:\n%q\nwant\n%q", f.String(), want)
	}
}

func TestPushAdoptsRaw(t *testing.T) {
	// file spells the index x.00; a push under x.0 must adopt that
	f, err := parseFile("x.00.tag.0 t1\n", true, Strict)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Push(mustKey(t, "x.0.tag"), "t2"); err != nil {
		t.Fatal(err)
	}
	want := "x.00.tag.0 t1\nx.00.tag.1 t2\n"
	if f.String() != want {
		t.Errorf("after push:\n%q\nwant\n%q", f.String(), want)
	}
}

func TestCreateAdoptsRaw(t *testing.T) {
	f, err := parseFile("x.00.a 1\n", true, Strict)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Create(mustKey(t, "x.0.b"), "2"); err != nil {
		t.Fatal(err)
	}
	want := "x.00.a 1\nx.00.b 2\n"
	if f.String() != want {
		t.Errorf("after create:\n%q\nwant\n%q", f.String(), want)
	}
}

func TestReadPrefix(t *testing.T) {
	in := "person.0.name.0 Anna\nperson.0.name.1 Berg\nperson.1.name.0 Bo\n"
	f, err := parseFile(in, true, Strict)
	if err != nil {
		t.Fatal(err)
	}
	out, err := f.Read(mustKey(t, "person.0"))
	if err != nil {
		t.Fatal(err)
	}
	want := "person.0.name.0 Anna\nperson.0.name.1 Berg\n"
	if out != want {
		t.Errorf("read prefix = %q, want %q", out, want)
	}
}

func TestKeys(t *testing.T) {
	in := "box\nperson.0.name Anna\nperson.1.name Bo\nport 8080\n"
	f, err := parseFile(in, true, Strict)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		prefix string // "" for the top level
		want   string
	}{
		{"", "box.\nperson.\nport\n"},
		{"person", "person.0.\nperson.1.\n"},
		{"person.0", "person.0.name\n"},
		{"person.0.name", "person.0.name\n"}, // a leaf prefix prints itself
		{"box", ""},                          // empty container, no children
	}
	for _, c := range cases {
		var prefix Key
		if c.prefix != "" {
			prefix = mustKey(t, c.prefix)
		}
		out, err := f.Keys(prefix)
		if err != nil {
			t.Fatalf("keys %q: %v", c.prefix, err)
		}
		if out != c.want {
			t.Errorf("keys %q = %q, want %q", c.prefix, out, c.want)
		}
	}
	if _, err := f.Keys(mustKey(t, "nosuch")); err == nil {
		t.Error("keys on a missing prefix did not fail")
	}
}

func TestTreeString(t *testing.T) {
	in := "person.0.name.0 Anna\nperson.1.name.0 Charles\nperson.1.name.1 Ingvar\n"
	f, err := parseFile(in, true, Strict)
	if err != nil {
		t.Fatal(err)
	}
	want := "person\n" +
		"  0\n" +
		"    name\n" +
		"      0 Anna\n" +
		"  1\n" +
		"    name\n" +
		"      0 Charles\n" +
		"      1 Ingvar\n"
	got, err := f.Tree(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("tree:\n%q\nwant\n%q", got, want)
	}
}

func TestTreeSubtree(t *testing.T) {
	in := "person.0.name Anna\nperson.1.name Charles\ntitle Spring\n"
	f, err := parseFile(in, true, Strict)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		key  string
		want string
	}{
		{"person", "person\n  0\n    name Anna\n  1\n    name Charles\n"},
		{"person.1", "1\n  name Charles\n"},
		{"person.0.name", "name Anna\n"},
	}
	for _, c := range cases {
		got, err := f.Tree(mustKey(t, c.key))
		if err != nil {
			t.Fatalf("Tree(%s): %v", c.key, err)
		}
		if got != c.want {
			t.Errorf("Tree(%s):\n%q\nwant\n%q", c.key, got, c.want)
		}
	}
	if _, err := f.Tree(mustKey(t, "nope")); err == nil {
		t.Errorf("Tree(nope): expected no-such-key error")
	}
}

func TestFmtSortsAndValidates(t *testing.T) {
	f, err := parseFile("b y\na x\n", false, Strict)
	if err != nil {
		t.Fatal(err)
	}
	if f.Hash == "" {
		f.Hash = HashLine
	}
	want := HashLine + "\na x\nb y\n"
	if f.String() != want {
		t.Errorf("fmt output:\n%q\nwant\n%q", f.String(), want)
	}
}

func TestContainerMarker(t *testing.T) {
	f := &File{}
	if err := f.CreateMarker(mustKey(t, "planets")); err != nil {
		t.Fatal(err)
	}
	if f.String() != "planets\n" {
		t.Errorf("marker serialized as %q", f.String())
	}

	// read on an empty marked container: no output, no error
	out, err := f.Read(mustKey(t, "planets"))
	if err != nil || out != "" {
		t.Errorf("read marker = %q, %v", out, err)
	}

	// duplicate marker is an error
	if err := f.CreateMarker(mustKey(t, "planets")); err == nil {
		t.Error("duplicate marker: expected error")
	}

	// marker on a leaf is an error
	if err := f.Create(mustKey(t, "star"), "sun"); err != nil {
		t.Fatal(err)
	}
	if err := f.CreateMarker(mustKey(t, "star")); err == nil {
		t.Error("marker on leaf: expected error")
	}

	// marker on a container with children is an error
	if err := f.CreateMarker(mustKey(t, "galaxies")); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(mustKey(t, "galaxies"), "Milky Way"); err != nil {
		t.Fatal(err)
	}
	if err := f.CreateMarker(mustKey(t, "galaxies")); err == nil {
		t.Error("marker on non-empty container: expected error")
	}
}

func TestMarkerRemovedOnPush(t *testing.T) {
	f := &File{}
	if err := f.CreateMarker(mustKey(t, "planets")); err != nil {
		t.Fatal(err)
	}
	if err := f.Push(mustKey(t, "planets"), "Earth"); err != nil {
		t.Fatal(err)
	}
	// marker must be gone
	for _, p := range f.Pairs {
		if p.Marker {
			t.Errorf("stale marker in file: %q", f.String())
		}
	}
	if f.String() != "planets.0 Earth\n" {
		t.Errorf("after push: %q", f.String())
	}
}

func TestMarkerRemovedOnCreate(t *testing.T) {
	f := &File{}
	if err := f.CreateMarker(mustKey(t, "cfg")); err != nil {
		t.Fatal(err)
	}
	if err := f.Create(mustKey(t, "cfg.host"), "localhost"); err != nil {
		t.Fatal(err)
	}
	if len(f.Pairs) != 1 || f.Pairs[0].Marker {
		t.Errorf("marker not removed: %q", f.String())
	}
}

func TestMarkerRmtree(t *testing.T) {
	f := &File{}
	if err := f.CreateMarker(mustKey(t, "empty")); err != nil {
		t.Fatal(err)
	}
	if err := f.DeleteTree(mustKey(t, "empty")); err != nil {
		t.Fatal(err)
	}
	if len(f.Pairs) != 0 {
		t.Errorf("rmtree did not remove marker: %q", f.String())
	}
}

func TestMarkerRoundTrip(t *testing.T) {
	// markers are stored as bare keys (no trailing dot) in the file
	in := "# github.com/unixfile/kv\nplanet.0.name.0 Earth\nplanets\n"
	f, err := parseFile(in, true, Strict)
	if err != nil {
		t.Fatal(err)
	}
	if f.String() != in {
		t.Errorf("round trip:\n%q\nwant\n%q", f.String(), in)
	}
}

func TestMarkerStaleRemovedByFmt(t *testing.T) {
	// stale marker (has children) is cleaned up by lenient parseFile
	in := "cfg.host localhost\ncfg\n"
	f, err := parseFile(in, false, Strict)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range f.Pairs {
		if p.Marker {
			t.Errorf("stale marker survived fmt: %q", f.String())
		}
	}
}

func TestMarkerStaleRejectedByStrictParse(t *testing.T) {
	// sorted file with stale marker must fail strict parse (needs fmt first)
	in := "cfg\ncfg.host localhost\n"
	if _, err := parseFile(in, true, Strict); err == nil {
		t.Error("strict parse of stale marker: expected error")
	}
}

func TestMarkerTreeString(t *testing.T) {
	f := &File{}
	if err := f.CreateMarker(mustKey(t, "planets")); err != nil {
		t.Fatal(err)
	}
	if err := f.Create(mustKey(t, "star"), "sun"); err != nil {
		t.Fatal(err)
	}
	got, err := f.Tree(nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "planets.\nstar sun\n"
	if got != want {
		t.Errorf("treeString:\n%q\nwant\n%q", got, want)
	}
}

func TestEmptyValueLine(t *testing.T) {
	f := &File{}
	if err := f.Create(mustKey(t, "empty"), ""); err != nil {
		t.Fatal(err)
	}
	if f.String() != "empty \\0\n" {
		t.Errorf("empty value line: %q", f.String())
	}
	out, err := f.Read(mustKey(t, "empty"))
	if err != nil || out != "\n" {
		t.Errorf("read empty = %q, %v", out, err)
	}
}

func TestControlCharValuesRejected(t *testing.T) {
	f := &File{Hash: HashLine}
	if err := f.Create(mustKey(t, "a"), "x\x01y"); err == nil {
		t.Error("create control char: expected error")
	}
	if err := f.Create(mustKey(t, "a"), "ok"); err != nil {
		t.Fatal(err)
	}
	if err := f.Update(mustKey(t, "a"), "x\x1fy"); err == nil {
		t.Error("update control char: expected error")
	}
	if err := f.Push(mustKey(t, "s"), "x\x00y"); err == nil {
		t.Error("push control char: expected error")
	}
	// the escapable whitespace stays accepted and survives a reparse
	if err := f.Update(mustKey(t, "a"), "tab\there\nline\rreturn"); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(f.String()); err != nil {
		t.Errorf("reparse after update: %v", err)
	}
}

func TestCreateOnMarkerSaysContainer(t *testing.T) {
	f, err := parseFile("planets\n", true, Strict)
	if err != nil {
		t.Fatal(err)
	}
	err = f.Create(mustKey(t, "planets"), "x")
	if err == nil || !strings.Contains(err.Error(), "container") {
		t.Errorf("create on marker = %v, want container error", err)
	}
}

func TestSetUpserts(t *testing.T) {
	f := &File{Hash: HashLine}
	if err := f.Set(mustKey(t, "title"), "Spring"); err != nil {
		t.Fatal(err)
	}
	if err := f.Set(mustKey(t, "title"), "Autumn"); err != nil {
		t.Fatal(err)
	}
	if f.String() != HashLine+"\ntitle Autumn\n" {
		t.Errorf("after set twice:\n%q", f.String())
	}
}

func TestAllDigitsEmpty(t *testing.T) {
	if allDigits("") {
		t.Error(`allDigits("") = true, want false`)
	}
}
