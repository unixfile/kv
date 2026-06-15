# kv

Command-line tool and Go library for the keyval format: flat key-value
pairs whose dotted paths build an implicit tree, a nested associative
array keyed by names or indices.

```
build.00 ./configure --prefix=/usr
build.01 make
build.parallel yes
person.0.email \0
person.0.name Charles Ingvar Jönsson
person.1.name Anna
planets
```

The format is specified at
[github.com/unixfile/keyval](https://github.com/unixfile/keyval).
This implementation is strictly downstream from the spec.

## Usage

```
usage: kv [-f file] [-F file] <command> [args]

  -f file   use file for this command only
  -F file   set the tty session file; -F prints it, -F- detaches
  -v        print the version

  create      c  <key> [value]  write a new leaf
  read        r  [key]          print a value, all pairs under a prefix, or all
  update      u  <key> [value]  overwrite a leaf
  delete      d  <key>          delete a leaf
  rmtree      D  <key>          delete a subtree
  push        p  <key> [value]  append to a sequence
  pop         P  <key>          remove and print the last item
  keys        k  [key]          list child keys, containers dotted
  edit        e                 open in $EDITOR, values aligned
  fmt         f                 sort and validate
  tree        t                 print the tree
  kv                            convert JSON (stdin) to keyval
  json                          convert keyval to JSON
  completion     bash           print the bash completion script

  <key>=<value>              create or update a leaf
  <key>+=<value>             append to a sequence
  <key>--                    remove and print the last item
  <key>-                     delete a leaf
  <key>                      print a value or a subtree
```

Operators are the short form for daily use. The commands name the same
operations and add strictness: create fails on an existing key, update
on a missing one. A command name wins over a key spelled the same, so a
key literally named `tree` needs `kv r tree`.

The file is resolved in order: `-f`, the `KV_FILE` environment
variable, the session file, and finally a lone `.kv` file in the
current directory; JSON never resolves implicitly. `-F` binds a file to the current terminal, so a
session needs no flag at all:

```
$ kv -F todo.kv
$ kv title="Spring cleaning"
$ kv steps+="buy boxes"
$ kv steps+="pack books"
$ kv t
steps
  0 buy boxes
  1 pack books
title Spring cleaning
$ kv steps--
pack books
$ kv title
Spring cleaning
```

`-F` identifies the terminal through `/proc`, so the session binding
is Linux-only; elsewhere use `-f`, `KV_FILE`, or a lone `.kv` file.

Mutations are atomic: write to a temp file, validate, rename. A failed
operation leaves the file untouched. Before each overwrite the old
content is copied to `${XDG_STATE_HOME:-$HOME/.local/state}/kv`, ten
copies deep per file.

## Install

```
go install github.com/unixfile/kv/cmd/kv@latest
```

Or clone and run `make install`.

## Edit

`kv e` opens the file in `$VISUAL`, `$EDITOR` or vi. The view puts a
dotted leader after every key so the values line up in one column, like
a table of contents:

```
build.00......... ./configure --prefix=/usr
build.01......... make
build.parallel... yes
person.0.email... \0
planets..........
title............ Spring cleaning
```

The dots are presentation only and are stripped on save. New lines may
be typed with or without them. Blank lines vanish, out of order keys
sort themselves. A parse error appears as a comment at the top and the
editor reopens; decline and the draft survives in /tmp. Exiting the
editor with a nonzero status, `:cq` in vim, aborts without writing. A
file that changed on disk while the editor was open prompts before
being overwritten.

## JSON

`kv kv` and `kv json` convert on stdin and stdout. A file named
`*.json` converts on the fly, which turns every command into a JSON
editor:

```
$ kv -f config.json update db.port 9090
$ kv -f config.json push tags staging
```

Conversion is heuristic and lossy at the edges. A leaf spelled exactly
like a JSON number, `true`, `false` or `null` converts to that type;
everything else stays a string. Scalars therefore round-trip, including
null. Markers become `{}`, so an empty JSON array returns as an empty
object.

Keys follow the document. A `.kv` file allows only the strict grammar:
lowercase names and indices. When the document is JSON, a `.json` file
or the json verbs, keys are taken verbatim, so `maxRetries`,
`Content-Type` and `$schema` all work. Keys are never folded or
renamed. Names with dots, spaces or control characters cannot be
represented, and neither can numeric names that do not count from
zero. A key holding an operator character needs the verb form on the
command line: `kv r 'a=b'`.

## Library

```go
import "github.com/unixfile/kv"

f, err := kv.Parse(data) // strict; kv.ParseLenient sorts and repairs
val, err := f.Read(key)
err = f.Push(key, "value")
out := f.String()
```

Structs marshal to lowercased field names. A `kv` tag renames a field;
`kv:"-"` skips it:

```go
type config struct {
	Host    string
	Port    int
	BaseURL string `kv:"base_url"`
}

f, err := kv.Marshal(config{Host: "localhost", Port: 8080})
var c config
err = kv.Unmarshal(f, &c)
```

`kv.FromJSON` and `kv.ToJSON` expose the converter in Go. The
package-level functions use the strict key grammar; the JSON-keys
dialect is explicit, as in `kv.JSONKeys.FromJSON`, `kv.JSONKeys.Parse`
and `kv.JSONKeys.ParseKey`. The library never touches the filesystem.

## Completion

Bash completion for verbs, flags, files and keys:

```
. <(kv completion bash)
```

Add that to `.bashrc` for every shell, or install it once:

```
kv completion bash > \
  ${XDG_DATA_HOME:-$HOME/.local/share}/bash-completion/completions/kv
```

## License

MIT, see [LICENSE](LICENSE).
