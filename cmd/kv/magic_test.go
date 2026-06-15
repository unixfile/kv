package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// uninstallExpr is the sed address range the magic block documents for
// removal. The test runs it for real, so a drift between the comment and
// working behaviour fails here.
const uninstallExpr = `\|^# >>> github.com/unixfile/keyval >>>$|,\|^# <<< github.com/unixfile/keyval <<<$|d`

func TestMagicBlock(t *testing.T) {
	lines := strings.Split(strings.TrimRight(magicBlock, "\n"), "\n")
	open := "# >>> github.com/unixfile/keyval >>>"
	clse := "# <<< github.com/unixfile/keyval <<<"
	if lines[0] != open {
		t.Errorf("first line %q, want fence %q", lines[0], open)
	}
	if lines[len(lines)-1] != clse {
		t.Errorf("last line %q, want fence %q", lines[len(lines)-1], clse)
	}
	for _, want := range []string{
		"0\tstring\t#",
		"keyval data, github.com/unixfile/keyval", // URL shown by file(1)
		"!:mime\ttext/x-keyval",
		"!:ext\tkv",
		"sed -i '" + uninstallExpr + "'", // documents the tested removal
	} {
		if !strings.Contains(magicBlock, want) {
			t.Errorf("magicBlock missing %q", want)
		}
	}
	if code := run([]string{"magic"}); code != 0 {
		t.Errorf("magic verb: exit %d", code)
	}
}

// TestMagicUninstall runs the documented sed against a ~/.magic holding
// the block plus an unrelated rule: the block must vanish and the rule
// must survive. Guards the slash-in-fence delimiter choice.
func TestMagicUninstall(t *testing.T) {
	sed, err := exec.LookPath("sed")
	if err != nil {
		t.Skip("no sed")
	}
	magic := filepath.Join(t.TempDir(), ".magic")
	body := "# unrelated rule\n0\tstring\tFOO\tfoo\n" + magicBlock
	if err := os.WriteFile(magic, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(sed, "-i", uninstallExpr, magic).CombinedOutput(); err != nil {
		t.Fatalf("sed: %v: %s", err, out)
	}
	b, err := os.ReadFile(magic)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if strings.Contains(got, "keyval") {
		t.Errorf("uninstall left remnants:\n%s", got)
	}
	if !strings.Contains(got, "unrelated rule") {
		t.Errorf("uninstall removed an unrelated rule:\n%s", got)
	}
}

// TestMagicFileDetection drives file(1) against the printed block: the
// URL shows in the description, the MIME and extension resolve, a
// shebang-riding hash line matches, and a markdown heading does not.
func TestMagicFileDetection(t *testing.T) {
	fileBin, err := exec.LookPath("file")
	if err != nil {
		t.Skip("no file(1)")
	}
	dir := t.TempDir()
	magic := filepath.Join(dir, "keyval.magic")
	if err := os.WriteFile(magic, []byte(magicBlock), 0o644); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	plain := write("a.kv", "# github.com/unixfile/keyval\ntitle Autumn\n")
	shebang := write("b.kv", "#!/usr/bin/env kv # github.com/unixfile/keyval\nx 1\n")
	heading := write("c.md", "## github.com/unixfile/keyval is a format\n")

	out := func(args ...string) string {
		full := append([]string{"-b", "-m", magic}, args...)
		o, err := exec.Command(fileBin, full...).CombinedOutput()
		if err != nil {
			t.Fatalf("file %v: %v: %s", args, err, o)
		}
		return strings.TrimSpace(string(o))
	}

	if got := out(plain); !strings.Contains(got, "github.com/unixfile/keyval") {
		t.Errorf("description %q lacks the URL", got)
	}
	if got := out(shebang); !strings.Contains(got, "keyval data") {
		t.Errorf("shebang hash line not matched: %q", got)
	}
	if got := out("--mime-type", plain); got != "text/x-keyval" {
		t.Errorf("mime %q, want text/x-keyval", got)
	}
	if got := out("--extension", plain); got != "kv" {
		t.Errorf("extension %q, want kv", got)
	}
	if got := out(heading); strings.Contains(got, "keyval data") {
		t.Errorf("markdown heading wrongly matched: %q", got)
	}
}
