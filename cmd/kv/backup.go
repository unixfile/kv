package main

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const backupKeep = 10

// backupDir returns the backup directory under the XDG state home.
func backupDir() (string, error) {
	d := os.Getenv("XDG_STATE_HOME")
	if d == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		d = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(d, "kv"), nil
}

// backup copies the current content of path into the backup directory
// before a save overwrites it, then prunes old copies. A missing file
// needs no backup. The name flattens the absolute path for readability
// plus a hash of it for uniqueness — slash and underscore collide in
// the flattening alone — followed by a nanosecond timestamp.
func backup(path string) error {
	cur, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	dir, err := backupDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	h := fnv.New32a()
	h.Write([]byte(abs))
	name := fmt.Sprintf("%s-%08x", strings.ReplaceAll(abs, "/", "_"), h.Sum32())
	dst := filepath.Join(dir, fmt.Sprintf("%s.%d", name, time.Now().UnixNano()))
	if err := os.WriteFile(dst, cur, 0o600); err != nil {
		return err
	}
	pruneBackups(dir, name+".")
	return nil
}

// pruneBackups keeps the newest backupKeep copies carrying the prefix.
// The timestamps share a width for centuries, so the names sort
// chronologically.
func pruneBackups(dir, prefix string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), prefix) && allDigits(e.Name()[len(prefix):]) {
			names = append(names, e.Name())
		}
	}
	if len(names) <= backupKeep {
		return
	}
	sort.Strings(names)
	for _, n := range names[:len(names)-backupKeep] {
		os.Remove(filepath.Join(dir, n))
	}
}

const recentKeep = 20

// noteRecent records path in the recent-files list in the state dir,
// newest first, deduplicated, capped at recentKeep. The list only feeds
// shell completion, so every failure is silently ignored.
func noteRecent(path string) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return
	}
	dir, err := backupDir()
	if err != nil {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	reg := filepath.Join(dir, "recent")
	lines := []string{abs}
	if b, err := os.ReadFile(reg); err == nil {
		for _, l := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
			if l != abs && l != "" && len(lines) < recentKeep {
				lines = append(lines, l)
			}
		}
	}
	os.WriteFile(reg, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
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
