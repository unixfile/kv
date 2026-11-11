# JSON and YAML conversion

kv reads and writes JSON and YAML by converting to and from keyval.
Objects and mappings become named children. Arrays and sequences become
indexed children. Empty containers become markers.

```
$ kv kv < deploy.yaml > deploy.kv
$ kv -f deploy.kv yaml
$ kv -f deploy.yaml spec.replicas=5
```

`kv kv` detects the input. Input that opens with `{` or `[` is read as
JSON, and as YAML flow syntax if that fails. Everything else is YAML.

## Scalars

keyval has no types, so conversion guesses. A leaf spelled exactly like
a JSON number, `true`, `false` or `null` converts to that type;
everything else stays a string. Scalars therefore round-trip, including
null.

A string that looks like another type keeps its quotes as a leaf, so
it returns as the same string:

```
$ printf 'port: "8080"\ndebug: "true"\nreplicas: 3\n' | kv kv
# github.com/unixfile/keyval
debug "true"
port "8080"
replicas 3
```

The same holds when setting a value. Kubernetes wants strings in `env`
values, labels and annotations, so quote a value that looks like a
number or a boolean:

```
$ kv -f deploy.yaml 'spec.template.spec.containers.0.env.0.value="true"'
```

Without the quotes the value is written as the boolean true. A leaf
spelled like a JSON string literal, quotes at both ends, always converts
to the string inside them.

Markers become `{}`, so an empty array returns as an empty object.

## Keys

Keys follow the document. A `.kv` file allows only the strict grammar:
names of letters, digits and underscores, and indices. When the
document is JSON or YAML, a file with that extension or a conversion
verb on stdin and stdout, keys are taken verbatim, so `maxRetries`,
`Content-Type` and `$schema` all work. Keys are never folded or
renamed.

A dot inside a JSON or YAML name is written `\.`, and a backslash `\\`,
so the dot that separates segments stays unambiguous:

```
$ kv -f deploy.yaml 'metadata.labels.app\.kubernetes\.io/name=web'
$ kv -f deploy.yaml metadata.labels
metadata.labels.app\.kubernetes\.io/name web
```

The escape belongs to this bridge, not to the keyval format. A `.kv`
file cannot hold such a name, so converting that document into a `.kv`
file fails.

Names with spaces or control characters cannot be represented, and
neither can numeric names that do not count from zero. A key holding an
operator character needs the verb form on the command line:
`kv r 'a=b'`.

## What YAML loses

- Comments are dropped, also when a YAML file is edited in place
- Anchors, aliases and merge keys are expanded into plain copies
- Key order becomes sorted order
- Numbers are written in JSON spelling: `0x10` becomes 16
- Block and flow style are chosen by the writer, with two-space
  indentation

## Several YAML documents

A stream of two or more documents separated by `---` becomes a
top-level sequence, one index per document: `0.kind`, `1.kind`. Empty
documents are skipped. A stream of one document keeps its keys
unprefixed.

Writing mirrors this: a top-level sequence of two or more members is
written as a stream. A single YAML document that is a list of two or
more items therefore returns as a stream.
