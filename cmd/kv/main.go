// kv is a CLI for the keyval (.kv) format.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/unixfile/kv"
)

// cmdDef is one row in the command table: the canonical name, an
// optional single-character alias, the max positional argument count,
// an argument synopsis, and a one-line description. Every verb lives
// here exactly once; usage text and maxArgs checks are derived from it.
type cmdDef struct {
	name    string
	alias   string // "" if none
	maxArgs int
	args    string // synopsis, "" for no arguments
	desc    string
}

var commands = []cmdDef{
	{"create",     "c", 2, "<key> [value]", "write a new leaf"},
	{"read",       "r", 1, "[key]",         "print a value, all pairs under a prefix, or all"},
	{"update",     "u", 2, "<key> [value]", "overwrite a leaf"},
	{"delete",     "d", 1, "<key>",         "delete a leaf"},
	{"rmtree",     "D", 1, "<key>",         "delete a subtree"},
	{"push",       "p", 2, "<key> [value]", "append to a sequence"},
	{"pop",        "P", 1, "<key>",         "remove and print the last item"},
	{"keys",       "k", 1, "[key]",         "list child keys, containers dotted"},
	{"edit",       "e", 0, "",              "open in $EDITOR, values aligned"},
	{"fmt",        "f", 0, "",              "sort and validate"},
	{"tree",       "t", 0, "",              "print the tree"},
	{"kv",         "",  0, "",              "convert JSON (stdin) to keyval"},
	{"json",       "",  0, "",              "convert keyval to JSON"},
	{"completion", "",  1, "bash",          "print the bash completion script"},
}

// cmdByName maps every verb name (canonical and alias) to its definition.
var cmdByName map[string]*cmdDef

func init() {
	cmdByName = make(map[string]*cmdDef, len(commands)*2)
	for i := range commands {
		c := &commands[i]
		cmdByName[c.name] = c
		if c.alias != "" {
			cmdByName[c.alias] = c
		}
	}
}

// usageText builds the full usage string from the command table so the
// verb list, maxArgs enforcement and usage output share one source.
func usageText() string {
	nameW, aliasW, argsW := 0, 0, 0
	for _, c := range commands {
		if l := len(c.name); l > nameW {
			nameW = l
		}
		if l := len(c.alias); l > aliasW {
			aliasW = l
		}
		if l := len(c.args); l > argsW {
			argsW = l
		}
	}
	var verbs strings.Builder
	for _, c := range commands {
		fmt.Fprintf(&verbs, "  %-*s  %-*s  %-*s  %s\n",
			nameW, c.name, aliasW, c.alias, argsW, c.args, c.desc)
	}
	return "usage: kv [-f file] [-F file] <command> [args]\n\n" +
		"  -f file   use file for this command only\n" +
		"  -F file   set the tty session file; -F prints it, -F- detaches\n" +
		"  -v        print the version\n\n" +
		verbs.String() +
		"\n" +
		"  <key>=<value>              create or update a leaf\n" +
		"  <key>+=<value>             append to a sequence\n" +
		"  <key>--                    remove and print the last item\n" +
		"  <key>-                     delete a leaf\n" +
		"  <key>                      print a value or a subtree\n" +
		"\n" +
		"a trailing dot on create makes an empty container\n" +
		"a missing value is read from stdin\n" +
		"file resolution: -f, KV_FILE, session file, a lone *.kv here\n" +
		"without a file, fmt and the json verbs filter stdin to stdout\n" +
		"a *.json file is edited as JSON, converted on the fly\n"
}

func main() {
	os.Exit(run(os.Args[1:]))
}

// version reports the module version the Go toolchain stamped into the
// binary: the tag under go install, a pseudo-version or (devel) for a
// local build.
func version() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
		return bi.Main.Version
	}
	return "(devel)"
}

func fail(err error) int {
	fmt.Fprintf(os.Stderr, "kv: %v\n", err)
	return 1
}

// opForm rewrites operator syntax into a verb with arguments: key=value
// upserts, key+=value pushes, key-- pops, key- deletes and a bare key
// reads. Order matters: += before =, -- before -.
func opForm(arg string, rest []string) (string, []string) {
	if i := strings.Index(arg, "+="); i >= 0 {
		return "push", append([]string{arg[:i], arg[i+2:]}, rest...)
	}
	if i := strings.IndexByte(arg, '='); i >= 0 {
		return "set", append([]string{arg[:i], arg[i+1:]}, rest...)
	}
	if k, ok := strings.CutSuffix(arg, "--"); ok {
		return "pop", append([]string{k}, rest...)
	}
	if k, ok := strings.CutSuffix(arg, "-"); ok {
		return "delete", append([]string{k}, rest...)
	}
	return "read", append([]string{arg}, rest...)
}

func run(args []string) int {
	var fFlag, FFlag string
	i := 0
flags:
	for i < len(args) {
		switch args[i] {
		case "-f", "-F":
			if i+1 == len(args) {
				if args[i] == "-F" {
					return showSession()
				}
				return fail(fmt.Errorf("-f needs a file argument"))
			}
			if args[i] == "-f" {
				fFlag = args[i+1]
			} else {
				FFlag = args[i+1]
			}
			i += 2
		case "-F-":
			return detachSession()
		case "--verbs":
			for _, c := range commands {
				fmt.Println(c.name)
			}
			fmt.Println("help")
			return 0
		case "-v", "--version":
			fmt.Println(version())
			return 0
		case "-h", "--help", "help":
			fmt.Print(usageText())
			return 0
		default:
			break flags
		}
	}
	if FFlag != "" {
		abs, err := filepath.Abs(FFlag)
		if err != nil {
			return fail(err)
		}
		FFlag = abs
		reg, err := registerPath()
		if err != nil {
			return fail(fmt.Errorf("-F: %v", err))
		}
		if err := os.WriteFile(reg, []byte(FFlag+"\n"), 0o600); err != nil {
			return fail(err)
		}
	}
	if i == len(args) {
		if FFlag != "" {
			return 0
		}
		fmt.Fprint(os.Stderr, usageText())
		return 2
	}
	cmd, rest := args[i], args[i+1:]
	if _, ok := cmdByName[cmd]; !ok {
		if strings.HasPrefix(cmd, "-") {
			fmt.Fprint(os.Stderr, usageText())
			return 2
		}
		cmd, rest = opForm(cmd, rest)
	}
	max := 2 // "set" via opForm; overridden below for known verbs
	if c, ok := cmdByName[cmd]; ok {
		max = c.maxArgs
	}
	if len(rest) > max {
		return fail(fmt.Errorf("%s: unexpected argument %q; quote values with spaces", cmd, rest[max]))
	}

	path := fFlag
	if path == "" {
		path = FFlag
	}
	if path == "" {
		path = os.Getenv("KV_FILE")
	}
	if path == "" {
		reg, err := registerPath()
		if err == nil {
			if b, err := os.ReadFile(reg); err == nil {
				path = strings.TrimSuffix(string(b), "\n")
			}
		}
	}
	if path == "" {
		// the only resolution the user never named: say what was picked
		if path = loneFile(); path != "" {
			fmt.Fprintf(os.Stderr, "kv: %s\n", path)
		}
	}

	switch cmd {
	case "fmt", "f":
		return runFmt(path)
	case "kv":
		return runFromJSON(path)
	case "json":
		return runToJSON(path)
	case "completion":
		return runCompletion(rest)
	}

	if path == "" {
		return fail(fmt.Errorf("no file: use -f, KV_FILE, or set a session file with -F"))
	}

	g := dialect(path)
	key := func() (kv.Key, error) {
		if len(rest) < 1 {
			return nil, fmt.Errorf("%s needs a key", cmd)
		}
		return g.ParseKey(rest[0])
	}
	// read accepts a trailing dot as a convenience; strip it before parsing
	keyRead := func() (kv.Key, error) {
		if len(rest) < 1 {
			return nil, fmt.Errorf("%s needs a key", cmd)
		}
		return g.ParseKey(strings.TrimSuffix(rest[0], "."))
	}
	value := func() (string, error) {
		if len(rest) >= 2 {
			return rest[1], nil
		}
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", err
		}
		return strings.TrimSuffix(string(b), "\n"), nil
	}

	switch cmd {
	case "create", "c":
		if len(rest) < 1 {
			return fail(fmt.Errorf("%s needs a key", cmd))
		}
		rawKey := rest[0]
		f, err := loadFile(path, true)
		if err != nil {
			return fail(err)
		}
		var opErr error
		if strings.HasSuffix(rawKey, ".") {
			k, err := g.ParseKey(strings.TrimSuffix(rawKey, "."))
			if err != nil {
				return fail(err)
			}
			opErr = f.CreateMarker(k)
		} else {
			k, err := g.ParseKey(rawKey)
			if err != nil {
				return fail(err)
			}
			v, err := value()
			if err != nil {
				return fail(err)
			}
			opErr = f.Create(k, v)
		}
		if opErr != nil {
			return fail(opErr)
		}
		if err := saveFile(path, f); err != nil {
			return fail(err)
		}
	case "update", "u", "push", "p", "set":
		k, err := key()
		if err != nil {
			return fail(err)
		}
		v, err := value()
		if err != nil {
			return fail(err)
		}
		f, err := loadFile(path, true)
		if err != nil {
			return fail(err)
		}
		switch cmd[0] {
		case 'u':
			err = f.Update(k, v)
		case 'p':
			err = f.Push(k, v)
		case 's': // upsert via the = operator
			err = f.Set(k, v)
		}
		if err != nil {
			return fail(err)
		}
		if err := saveFile(path, f); err != nil {
			return fail(err)
		}
	case "read", "r":
		f, err := loadFile(path, false)
		if err != nil {
			return fail(err)
		}
		if len(rest) == 0 {
			fmt.Print(f.Flat())
			break
		}
		k, err := keyRead()
		if err != nil {
			return fail(err)
		}
		out, err := f.Read(k)
		if err != nil {
			return fail(err)
		}
		fmt.Print(out)
	case "delete", "d", "rmtree", "D":
		k, err := key()
		if err != nil {
			return fail(err)
		}
		f, err := loadFile(path, false)
		if err != nil {
			return fail(err)
		}
		if cmd == "delete" || cmd == "d" {
			err = f.Delete(k)
		} else {
			err = f.DeleteTree(k)
		}
		if err != nil {
			return fail(err)
		}
		if err := saveFile(path, f); err != nil {
			return fail(err)
		}
	case "pop", "P":
		k, err := key()
		if err != nil {
			return fail(err)
		}
		f, err := loadFile(path, false)
		if err != nil {
			return fail(err)
		}
		out, err := f.Pop(k)
		if err != nil {
			return fail(err)
		}
		if err := saveFile(path, f); err != nil {
			return fail(err)
		}
		fmt.Print(out)
	case "tree", "t":
		f, err := loadFile(path, false)
		if err != nil {
			return fail(err)
		}
		fmt.Print(f.Tree())
	case "keys", "k":
		f, err := loadFile(path, false)
		if err != nil {
			return fail(err)
		}
		var prefix kv.Key
		if len(rest) > 0 {
			prefix, err = g.ParseKey(strings.TrimSuffix(rest[0], "."))
			if err != nil {
				return fail(err)
			}
		}
		out, err := f.Keys(prefix)
		if err != nil {
			return fail(err)
		}
		fmt.Print(out)
	case "edit", "e":
		return runEdit(path)
	default:
		fmt.Fprintf(os.Stderr, "kv: unknown command %q\n", cmd)
		fmt.Fprint(os.Stderr, usageText())
		return 2
	}
	return 0
}

func runFmt(path string) int {
	if path != "" {
		if jsonPath(path) {
			f, err := loadFile(path, false)
			if err != nil {
				return fail(err)
			}
			return boolFail(saveFile(path, f))
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return fail(err)
		}
		f, err := kv.ParseLenient(string(b))
		if err != nil {
			return fail(err)
		}
		if f.Hash == "" {
			f.Hash = kv.HashLine
		}
		return boolFail(saveFile(path, f))
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fail(err)
	}
	f, err := kv.ParseLenient(string(b))
	if err != nil {
		return fail(err)
	}
	if f.Hash == "" {
		f.Hash = kv.HashLine
	}
	fmt.Print(f)
	return 0
}

// runFromJSON converts JSON on stdin to keyval: into the resolved file,
// or to stdout when none is set. Only a .kv target forces the strict
// key grammar; stdout is part of the JSON pipeline.
func runFromJSON(path string) int {
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fail(err)
	}
	g := kv.JSONKeys
	if path != "" && !jsonPath(path) {
		g = kv.Strict
	}
	f, err := g.FromJSON(b)
	if err != nil {
		return fail(err)
	}
	if path != "" {
		return boolFail(saveFile(path, f))
	}
	fmt.Print(f)
	return 0
}

// runToJSON converts the resolved file, or keyval on stdin when none is
// set, to JSON on stdout.
func runToJSON(path string) int {
	var f *kv.File
	var err error
	if path != "" {
		f, err = loadFile(path, false)
	} else {
		var b []byte
		if b, err = io.ReadAll(os.Stdin); err == nil {
			// the destination is JSON, so the wide key grammar applies
			f, err = kv.JSONKeys.Parse(string(b))
		}
	}
	if err != nil {
		return fail(err)
	}
	out, err := kv.ToJSON(f)
	if err != nil {
		return fail(err)
	}
	os.Stdout.Write(out)
	return 0
}

// detachSession removes the session file binding for this tty. A tty
// without a binding is already detached, so the removal is idempotent.
func detachSession() int {
	reg, err := registerPath()
	if err != nil {
		return fail(err)
	}
	if err := os.Remove(reg); err != nil && !os.IsNotExist(err) {
		return fail(err)
	}
	return 0
}

// showSession prints the session file path bound to this tty.
func showSession() int {
	reg, err := registerPath()
	if err != nil {
		return fail(err)
	}
	b, err := os.ReadFile(reg)
	if os.IsNotExist(err) {
		return fail(fmt.Errorf("no session file set for this tty"))
	}
	if err != nil {
		return fail(err)
	}
	fmt.Print(string(b))
	return 0
}


func boolFail(err error) int {
	if err != nil {
		return fail(err)
	}
	return 0
}

// jsonPath reports whether path should be read and written as JSON.
func jsonPath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".json")
}

// dialect picks the key grammar for a document: the strict grammar for
// .kv files, the verbatim JSON-keys grammar when the document is JSON.
func dialect(path string) kv.Dialect {
	if jsonPath(path) {
		return kv.JSONKeys
	}
	return kv.Strict
}

// loneFile returns the implicit file for the current directory: exactly
// one non-hidden .kv file. JSON never resolves implicitly — the bridge
// format demands an explicit -f, -F or KV_FILE.
func loneFile() string {
	ents, err := os.ReadDir(".")
	if err != nil {
		return ""
	}
	found := ""
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		if strings.EqualFold(filepath.Ext(name), ".kv") {
			if found != "" {
				return ""
			}
			found = name
		}
	}
	return found
}

// loadFile reads and strict-parses path, converting from JSON when the
// extension says so. With mayCreate set, a missing file yields a new
// empty file carrying the hash line.
func loadFile(path string, mayCreate bool) (*kv.File, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) && mayCreate {
		return &kv.File{Hash: kv.HashLine}, nil
	}
	if err != nil {
		return nil, err
	}
	noteRecent(path)
	if jsonPath(path) {
		return kv.JSONKeys.FromJSON(b)
	}
	return kv.Parse(string(b))
}

// saveFile writes atomically: temp file in the same directory, then
// rename. An existing file keeps its permission bits and its old
// content goes to the backup directory first; a failed backup warns but
// never blocks the save. A JSON path is written as JSON.
func saveFile(path string, f *kv.File) error {
	out := []byte(f.String())
	if jsonPath(path) {
		b, err := kv.ToJSON(f)
		if err != nil {
			return err
		}
		out = b
	}
	if err := backup(path); err != nil {
		fmt.Fprintf(os.Stderr, "kv: backup: %v\n", err)
	}
	dir := filepath.Dir(path)
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, ".kv-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	noteRecent(path)
	return nil
}

// registerPath derives the tty session file from the controlling
// terminal: kv-<tty path with / replaced by _> in the user's runtime
// dir, or /tmp without one. The standard fds are tried in order so a
// redirected stdin (value via heredoc) still resolves.
func registerPath() (string, error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = "/tmp"
	}
	for fd := 0; fd <= 2; fd++ {
		p, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
		if err != nil {
			continue
		}
		if strings.HasPrefix(p, "/dev/pts/") || strings.HasPrefix(p, "/dev/tty") {
			return filepath.Join(dir, "kv-"+strings.ReplaceAll(p, "/", "_")), nil
		}
	}
	return "", fmt.Errorf("no tty")
}
