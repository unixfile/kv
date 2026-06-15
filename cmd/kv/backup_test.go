package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unixfile/kv"
)

func TestBackupOnSave(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	path := filepath.Join(t.TempDir(), "x.kv")
	bdir := filepath.Join(state, "kv")

	f1, err := kv.Parse("a 1\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := saveFile(path, f1); err != nil {
		t.Fatal(err)
	}
	if n := countBackups(t, bdir); n != 0 {
		t.Errorf("new file made %d backups, want 0", n)
	}

	f2, err := kv.Parse("a 2\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := saveFile(path, f2); err != nil {
		t.Fatal(err)
	}
	names := backupNames(t, bdir)
	if len(names) != 1 {
		t.Fatalf("backups after overwrite: %v", names)
	}
	b, err := os.ReadFile(filepath.Join(bdir, names[0]))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != kv.HashLine+"\na 1\n" {
		t.Errorf("backup content %q, want previous file content", b)
	}

	for i := 0; i < 14; i++ {
		if err := saveFile(path, f1); err != nil {
			t.Fatal(err)
		}
	}
	if n := countBackups(t, bdir); n != backupKeep {
		t.Errorf("pruned to %d backups, want %d", n, backupKeep)
	}
}

func countBackups(t *testing.T, dir string) int {
	return len(backupNames(t, dir))
}

// backupNames lists the backup copies in dir, skipping the recent list
// that shares the state dir.
func backupNames(t *testing.T, dir string) []string {
	ents, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		if e.Name() != "recent" {
			names = append(names, e.Name())
		}
	}
	return names
}

func TestRecentList(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	reg := filepath.Join(state, "kv", "recent")

	noteRecent("/tmp/a.kv")
	noteRecent("/tmp/b.kv")
	noteRecent("/tmp/a.kv") // re-noting moves to the front, no duplicate
	b, err := os.ReadFile(reg)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "/tmp/a.kv\n/tmp/b.kv\n" {
		t.Errorf("recent list %q, want a then b", b)
	}

	for i := 0; i < 2*recentKeep; i++ {
		noteRecent(filepath.Join("/tmp", string(rune('a'+i))+".kv"))
	}
	b, err = os.ReadFile(reg)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")); n != recentKeep {
		t.Errorf("recent list has %d entries, want %d", n, recentKeep)
	}
}

// /a_b and /a/b flatten to the same name; the hash suffix must keep
// their backups in separate prune namespaces.
func TestBackupNamesDistinct(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	root := t.TempDir()
	p1 := filepath.Join(root, "a_b", "f.kv")
	p2 := filepath.Join(root, "a", "b", "f.kv")
	for _, p := range []string{p1, p2} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("a 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := backup(p); err != nil {
			t.Fatal(err)
		}
	}
	ents, err := os.ReadDir(filepath.Join(state, "kv"))
	if err != nil {
		t.Fatal(err)
	}
	prefixes := map[string]bool{}
	for _, e := range ents {
		name := e.Name()
		if i := strings.LastIndexByte(name, '.'); i >= 0 {
			name = name[:i] // strip the timestamp
		}
		prefixes[name] = true
	}
	if len(prefixes) != 2 {
		t.Errorf("backup name prefixes = %v, want 2 distinct", prefixes)
	}
}
