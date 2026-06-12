package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtraArgsRejected(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "x.kv")
	if err := os.WriteFile(path, []byte("title x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"-f", path, "update", "title", "two words"}); code != 0 {
		t.Errorf("quoted value: exit %d", code)
	}
	bad := [][]string{
		{"-f", path, "update", "title", "two", "words"},
		{"-f", path, "read", "title", "extra"},
		{"-f", path, "tree", "extra"},
		{"-f", path, "fmt", "extra"},
	}
	for _, args := range bad {
		if code := run(args); code == 0 {
			t.Errorf("run(%v): extra argument accepted", args)
		}
	}
}

func TestOperators(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "x.kv")
	fp := []string{"-f", path}
	ok := func(args ...string) {
		t.Helper()
		if code := run(append(fp, args...)); code != 0 {
			t.Fatalf("run(%v): exit %d", args, code)
		}
	}
	bad := func(args ...string) {
		t.Helper()
		if code := run(append(fp, args...)); code == 0 {
			t.Fatalf("run(%v): expected failure", args)
		}
	}

	ok("title=Spring") // = creates
	ok("title=Autumn") // = updates
	ok("steps+=one")   // += pushes
	ok("steps+=two")
	ok("steps--")             // -- pops
	ok("steps.0")             // bare key reads
	ok("steps.0-")            // - deletes
	bad("steps.0")            // gone
	ok("note=")               // empty value, no stdin
	bad("title=two", "words") // unquoted value screams
	bad("title", "extra")     // bare read screams
	bad("ti-tle=x")           // bad key reported, not silently mangled

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "# github.com/unixfile/keyval\nnote \\0\ntitle Autumn\n"
	if string(b) != want {
		t.Errorf("file after operators:\n%q\nwant\n%q", b, want)
	}
}

func TestJSONFileMode(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "x.json")
	if err := os.WriteFile(path, []byte(`{"maxRetries": 3}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-f", path, "maxRetries=5"},
		{"-f", path, "tags+=staging"},
		{"-f", path, "u", "maxRetries", "7"},
	} {
		if code := run(args); code != 0 {
			t.Fatalf("run(%v): exit %d", args, code)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"maxRetries\": 7,\n  \"tags\": [\n    \"staging\"\n  ]\n}\n"
	if string(b) != want {
		t.Errorf("file after JSON mode:\n%s\nwant\n%s", b, want)
	}
	// the same key must stay foreign to a .kv file
	kvPath := filepath.Join(t.TempDir(), "y.kv")
	if code := run([]string{"-f", kvPath, "maxRetries=1"}); code == 0 {
		t.Error("strict file accepted a foreign key")
	}
}

func TestUnknownFlagShowsUsage(t *testing.T) {
	if code := run([]string{"-x"}); code != 2 {
		t.Errorf("unknown flag: exit %d, want 2", code)
	}
}

// The session file belongs in XDG_RUNTIME_DIR with private permissions.
// Needs a controlling terminal; skipped where go test has none.
func TestSessionFilePrivate(t *testing.T) {
	if _, err := registerPath(); err != nil {
		t.Skip("no tty")
	}
	runtimeDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	target := filepath.Join(t.TempDir(), "x.kv")
	if code := run([]string{"-F", target}); code != 0 {
		t.Fatalf("-F: exit %d", code)
	}
	reg, err := registerPath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(reg) != runtimeDir {
		t.Errorf("session file at %s, want it under %s", reg, runtimeDir)
	}
	st, err := os.Stat(reg)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("session file mode %o, want 600", st.Mode().Perm())
	}
}

func TestVersion(t *testing.T) {
	if version() == "" {
		t.Error("version() is empty")
	}
	if code := run([]string{"-v"}); code != 0 {
		t.Errorf("-v: exit %d", code)
	}
}
