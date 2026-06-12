package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unixfile/kv"
)

func TestRenderTOC(t *testing.T) {
	f, err := kv.Parse(kv.HashLine + "\n" +
		"build.00 ./configure\n" +
		"build.parallel yes\n" +
		"person.0.email \\0\n" +
		"planets\n" +
		"title Spring cleaning\n")
	if err != nil {
		t.Fatal(err)
	}
	want := "build.00......... ./configure\n" +
		"build.parallel... yes\n" +
		"person.0.email... \\0\n" +
		"planets..........\n" +
		"title............ Spring cleaning\n"
	if got := renderTOC(f); got != want {
		t.Errorf("renderTOC:\n%q\nwant\n%q", got, want)
	}
}

func TestRenderTOCEmpty(t *testing.T) {
	f := &kv.File{Hash: kv.HashLine}
	if got := renderTOC(f); got != "" {
		t.Errorf("renderTOC empty: %q", got)
	}
}

func TestParseTOCRoundTrip(t *testing.T) {
	in := kv.HashLine + "\n" +
		"build.00 ./configure --prefix=/usr\n" +
		"build.01 make\n" +
		"person.0.name Anna\n" +
		"planets\n" +
		"title Spring cleaning\n"
	f, err := kv.Parse(in)
	if err != nil {
		t.Fatal(err)
	}
	back, err := parseTOC(renderTOC(f), f.Hash, kv.Strict)
	if err != nil {
		t.Fatal(err)
	}
	if back.String() != in {
		t.Errorf("round trip:\n%q\nwant\n%q", back.String(), in)
	}
}

func TestParseTOCHelpful(t *testing.T) {
	draft := kv.HashLine + "\n" +
		"# error: leftover comment\n" +
		"\n" +
		"zeta...... last\n" +
		"alpha plain kv line\n" +
		"   \n" +
		"marker.......\n"
	f, err := parseTOC(draft, kv.HashLine, kv.Strict)
	if err != nil {
		t.Fatal(err)
	}
	want := kv.HashLine + "\n" +
		"alpha plain kv line\n" +
		"marker\n" +
		"zeta last\n"
	if f.String() != want {
		t.Errorf("parseTOC:\n%q\nwant\n%q", f.String(), want)
	}
}

func TestParseTOCJSONKeys(t *testing.T) {
	draft := "Content-Type...... json\nmaxRetries........ 3\n"
	f, err := parseTOC(draft, kv.HashLine, kv.JSONKeys)
	if err != nil {
		t.Fatal(err)
	}
	want := kv.HashLine + "\nContent-Type json\nmaxRetries 3\n"
	if f.String() != want {
		t.Errorf("parseTOC:\n%q\nwant\n%q", f.String(), want)
	}
	if _, err := parseTOC(draft, kv.HashLine, kv.Strict); err == nil {
		t.Error("strict parseTOC accepted foreign keys")
	}
}

func TestRenderTOCRuneWidth(t *testing.T) {
	f, err := kv.JSONKeys.ParseLenient("form kvadrat\nfärg röd\n")
	if err != nil {
		t.Fatal(err)
	}
	want := "form... kvadrat\nfärg... röd\n"
	if got := renderTOC(f); got != want {
		t.Errorf("renderTOC:\n%q\nwant\n%q", got, want)
	}
}

func TestDetoc(t *testing.T) {
	cases := map[string]string{
		"key...... value":     "key value",
		"key......":           "key",
		"key...... ...dots":   "key ...dots",
		"key..3.14":           "key 3.14",
		"key plain":           "key plain",
		"bare":                "bare",
		"a.0.b.... x":         "a.0.b x",
		"key...... two words": "key two words",
	}
	for in, want := range cases {
		if got := detoc(in); got != want {
			t.Errorf("detoc(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoneFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	touch := func(name string) {
		if err := os.WriteFile(name, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := loneFile(); got != "" {
		t.Errorf("empty dir: %q", got)
	}
	touch("notes.txt")
	touch(".hidden.kv")
	if err := os.Mkdir("sub.kv", 0o755); err != nil {
		t.Fatal(err)
	}
	if got := loneFile(); got != "" {
		t.Errorf("no candidates: %q", got)
	}
	touch("b.json")
	if got := loneFile(); got != "" {
		t.Errorf("json never resolves implicitly: %q, want none", got)
	}
	touch("a.kv")
	if got := loneFile(); got != "a.kv" {
		t.Errorf("lone kv: %q, want a.kv", got)
	}
	touch("c.kv")
	if got := loneFile(); got != "" {
		t.Errorf("two kv files: %q, want none", got)
	}
}

// A save that fails after a successful edit must keep the draft and
// leave the target untouched.
func TestEditDraftKeptOnFailedSave(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	drafts := t.TempDir()
	t.Setenv("TMPDIR", drafts)

	ed := filepath.Join(t.TempDir(), "ed")
	if err := os.WriteFile(ed, []byte("#!/bin/sh\nprintf 'b........ 2\\n' >> \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", ed)

	dir := t.TempDir()
	path := filepath.Join(dir, "x.kv")
	orig := "a 1\n"
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	// a read-only directory makes the atomic save fail at its temp file
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)

	if code := runEdit(path); code != 1 {
		t.Fatalf("edit with read-only dir: exit %d, want 1", code)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != orig {
		t.Errorf("target after failed save: %q, %v, want untouched", b, err)
	}
	kept, err := filepath.Glob(filepath.Join(drafts, "kv-edit-*.kv"))
	if err != nil || len(kept) != 1 {
		t.Fatalf("kept drafts = %v, %v, want exactly one", kept, err)
	}
	b, err := os.ReadFile(kept[0])
	if err != nil || !strings.Contains(string(b), "b........ 2") {
		t.Errorf("draft content: %q, %v, want the edit preserved", b, err)
	}
}
