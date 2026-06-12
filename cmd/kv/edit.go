package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"

	"github.com/unixfile/kv"
)

// renderTOC renders a file for editing: every key is followed by a
// dotted leader ending in a shared column, then one space and the value.
// Markers get the full leader and nothing after. The hash line is not
// editable and stays out of the draft. The dots are presentation only;
// values keep their canonical kv escaping because each line is derived
// from File.String.
func renderTOC(f *kv.File) string {
	lines := strings.Split(strings.TrimSuffix(f.String(), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	keys := make([]string, len(lines))
	width := 0
	for i, line := range lines {
		if strings.HasPrefix(line, "#") {
			continue
		}
		k := line
		if sp := strings.IndexByte(line, ' '); sp >= 0 {
			k = line[:sp]
		}
		keys[i] = k
		// rune count, not bytes, so UTF-8 keys line up on screen
		if n := utf8.RuneCountInString(k); n > width {
			width = n
		}
	}
	col := width + 3
	var b strings.Builder
	for i, line := range lines {
		if strings.HasPrefix(line, "#") {
			continue
		}
		k := keys[i]
		b.WriteString(k)
		b.WriteString(strings.Repeat(".", col-utf8.RuneCountInString(k)))
		if rest := line[len(k):]; rest != "" {
			b.WriteByte(' ')
			b.WriteString(rest[1:])
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// detoc strips the dotted leader from one TOC line, returning a
// canonical kv line. The leader is the first run of two or more dots,
// provided no space comes before it; a line without one passes through
// unchanged, so plain kv lines may be typed in the editor.
func detoc(line string) string {
	sp := strings.IndexByte(line, ' ')
	dd := strings.Index(line, "..")
	if dd < 0 || sp >= 0 && sp < dd {
		return line
	}
	key := line[:dd]
	rest := strings.TrimLeft(line[dd:], ".")
	rest = strings.TrimPrefix(rest, " ")
	if rest == "" {
		return key
	}
	return key + " " + rest
}

// parseTOC parses an edited TOC draft. Blank lines and comment lines
// are dropped, leaders stripped, and the result parsed leniently so out
// of order keys sort themselves. The hash line is not editable; the
// caller passes the one to keep, and the key grammar of the document
// being edited.
func parseTOC(draft, hash string, g kv.Dialect) (*kv.File, error) {
	var lines []string
	for _, line := range strings.Split(draft, "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		line = detoc(line)
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, line)
	}
	src := ""
	if len(lines) > 0 {
		src = strings.Join(lines, "\n") + "\n"
	}
	f, err := g.ParseLenient(src)
	if err != nil {
		return nil, err
	}
	f.Hash = hash
	return f, nil
}

// runEdit opens the file as a TOC draft in the editor and writes the
// parsed result back. A parse error is injected as a comment at the top
// of the draft and the editor reopens on confirmation; declining keeps
// the draft in /tmp. A nonzero editor exit aborts.
func runEdit(path string) int {
	// snapshot the bytes on disk; the save compares against them to
	// catch writes that happen while the editor is open
	snap, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fail(err)
	}
	f, err := loadFile(path, true)
	if err != nil {
		return fail(err)
	}
	tmp, err := os.CreateTemp("", "kv-edit-*.kv")
	if err != nil {
		return fail(err)
	}
	draft := renderTOC(f)
	if _, err := tmp.WriteString(draft); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return fail(err)
	}

	last := draft
	for {
		if err := runEditor(tmp.Name()); err != nil {
			os.Remove(tmp.Name())
			return fail(fmt.Errorf("editor: %v", err))
		}
		b, err := os.ReadFile(tmp.Name())
		if err != nil {
			return fail(err)
		}
		cur := string(b)
		if cur == last {
			os.Remove(tmp.Name())
			fmt.Fprintln(os.Stderr, "kv: no changes")
			return 0
		}
		edited, perr := parseTOC(cur, f.Hash, dialect(path))
		if perr == nil {
			disk, err := os.ReadFile(path)
			if err != nil && !os.IsNotExist(err) {
				return fail(err)
			}
			if !bytes.Equal(disk, snap) {
				fmt.Fprintf(os.Stderr, "kv: %s changed on disk while editing\noverwrite? [y/N] ", path)
				var ans string
				fmt.Fscanln(os.Stdin, &ans)
				if !strings.EqualFold(ans, "y") {
					fmt.Fprintf(os.Stderr, "kv: aborted, draft kept at %s\n", tmp.Name())
					return 1
				}
			}
			if err := saveFile(path, edited); err != nil {
				fmt.Fprintf(os.Stderr, "kv: %v\nkv: draft kept at %s\n", err, tmp.Name())
				return 1
			}
			os.Remove(tmp.Name())
			return 0
		}
		fmt.Fprintf(os.Stderr, "kv: %v\nedit again? [Y/n] ", perr)
		var ans string
		fmt.Fscanln(os.Stdin, &ans)
		if strings.EqualFold(ans, "n") {
			fmt.Fprintf(os.Stderr, "kv: aborted, draft kept at %s\n", tmp.Name())
			return 1
		}
		last = "# error: " + perr.Error() + "\n" + stripErrorLines(cur)
		if err := os.WriteFile(tmp.Name(), []byte(last), 0o600); err != nil {
			return fail(err)
		}
	}
}

// stripErrorLines removes previously injected error comments so they do
// not pile up across retries.
func stripErrorLines(s string) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "# error:") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// runEditor runs $VISUAL, $EDITOR or vi on path, attached to the
// terminal. The variable may carry arguments, so it runs through sh
// with the path as $0.
func runEditor(path string) error {
	ed := os.Getenv("VISUAL")
	if ed == "" {
		ed = os.Getenv("EDITOR")
	}
	if ed == "" {
		ed = "vi"
	}
	c := exec.Command("sh", "-c", ed+` "$0"`, path)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
