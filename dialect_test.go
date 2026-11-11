package kv

import "testing"

func TestParseSegmentJSONKeys(t *testing.T) {
	verbatim := []string{
		"maxRetries", "Content-Type", "$schema", "_id", "@type",
		"2x", "färg", "UPPER", "a=b",
	}
	for _, in := range verbatim {
		seg, err := parseSegment(in, JSONKeys)
		if err != nil {
			t.Errorf("parseSegment(%q, JSONKeys): %v", in, err)
			continue
		}
		if seg.IsIndex || seg.Name != in || seg.Raw != in {
			t.Errorf("parseSegment(%q, JSONKeys) = %+v, want verbatim name", in, seg)
		}
	}
	if seg, err := parseSegment("07", JSONKeys); err != nil || !seg.IsIndex || seg.Index != 7 {
		t.Errorf("parseSegment(07, JSONKeys) = %+v, %v, want index 7", seg, err)
	}
	for _, in := range []string{"", "a b", "#x", "a\tb", "a\nb", "\x7f"} {
		if _, err := parseSegment(in, JSONKeys); err == nil {
			t.Errorf("parseSegment(%q, JSONKeys): expected error", in)
		}
	}
	for _, in := range []string{"maxRetries", "UPPER", "apiVersion", "A_1"} {
		seg, err := parseSegment(in, Strict)
		if err != nil || seg.IsIndex || seg.Name != in {
			t.Errorf("parseSegment(%q, Strict) = %+v, %v, want name", in, seg, err)
		}
	}
	for _, in := range []string{"Content-Type", "$schema", "_id", "2x", "färg"} {
		if _, err := parseSegment(in, Strict); err == nil {
			t.Errorf("parseSegment(%q, Strict): expected error", in)
		}
	}
}

// the name a/b must stay distinct from the path a.b inside validate's
// node ids; a naive separator would merge them
func TestValidateForeignNamesNoCollision(t *testing.T) {
	f, err := JSONKeys.ParseLenient("a.b x\na/b y\n")
	if err != nil {
		t.Fatalf("ParseLenient: %v", err)
	}
	if len(f.Pairs) != 2 {
		t.Fatalf("pairs: %d, want 2", len(f.Pairs))
	}
}

func TestFromJSONForeignKeys(t *testing.T) {
	in := []byte(`{"maxRetries": 3, "Content-Type": "json", "tags": ["a"]}`)
	if _, err := FromJSON(in); err == nil {
		t.Error("strict FromJSON accepted foreign keys")
	}
	f, err := JSONKeys.FromJSON(in)
	if err != nil {
		t.Fatal(err)
	}
	want := HashLine + "\nContent-Type json\nmaxRetries 3\ntags.0 a\n"
	if f.String() != want {
		t.Errorf("kv form:\n%q\nwant\n%q", f.String(), want)
	}
	out, err := ToJSON(f)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON := "{\n  \"Content-Type\": \"json\",\n  \"maxRetries\": 3,\n  \"tags\": [\n    \"a\"\n  ]\n}\n"
	if string(out) != wantJSON {
		t.Errorf("round trip:\n%s\nwant\n%s", out, wantJSON)
	}
}

// a dotted object name would silently split into path structure, so
// the strict grammar rejects it, for marshalled map keys too
func TestDottedNamesRejected(t *testing.T) {
	if _, err := FromJSON([]byte(`{"a.b": 1}`)); err == nil {
		t.Error("strict FromJSON: dotted object name accepted")
	}
	if _, err := Marshal(map[string]string{"a.b": "x"}); err == nil {
		t.Error("Marshal: dotted map key accepted")
	}
}

// JSONKeys writes a dot inside a name as \. and a backslash as \\
func TestEscapedNames(t *testing.T) {
	k, err := JSONKeys.ParseKey(`labels.app\.kubernetes\.io/name.a\\.0`)
	if err != nil {
		t.Fatal(err)
	}
	if len(k) != 4 || k[1].Name != "app.kubernetes.io/name" || k[2].Name != `a\` || !k[3].IsIndex {
		t.Errorf("ParseKey = %+v", k)
	}
	if got := k.String(); got != `labels.app\.kubernetes\.io/name.a\\.0` {
		t.Errorf("String = %q", got)
	}
	for _, in := range []string{`a\b`, `a\`, `\`} {
		if _, err := JSONKeys.ParseKey(in); err == nil {
			t.Errorf("ParseKey(%q): expected error", in)
		}
	}
	if _, err := ParseKey(`a\.b`); err == nil {
		t.Error("strict ParseKey accepted an escape")
	}
	for in, want := range map[string]string{`a.`: `a`, `a\.`: `a\.`, `a\\.`: `a\\`, `a`: `a`} {
		if got, _ := TrimDot(in); got != want {
			t.Errorf("TrimDot(%q) = %q, want %q", in, got, want)
		}
	}

	in := []byte(`{"labels": {"app.kubernetes.io/name": "web", "a\\b": 1, "app": {"x": 2}}}`)
	f, err := JSONKeys.FromJSON(in)
	if err != nil {
		t.Fatal(err)
	}
	want := HashLine + "\nlabels.a\\\\b 1\nlabels.app.x 2\nlabels.app\\.kubernetes\\.io/name web\n"
	if f.String() != want {
		t.Errorf("kv form:\n%s\nwant\n%s", f.String(), want)
	}
	back, err := JSONKeys.Parse(f.String())
	if err != nil {
		t.Fatal(err)
	}
	out, err := ToJSON(back)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON := "{\n  \"labels\": {\n    \"a\\\\b\": 1,\n    \"app\": {\n      \"x\": 2\n    },\n    \"app.kubernetes.io/name\": \"web\"\n  }\n}\n"
	if string(out) != wantJSON {
		t.Errorf("round trip:\n%s\nwant\n%s", out, wantJSON)
	}
}

// a string that looks like another type keeps its quotes as a leaf and
// returns as the same string
func TestQuotedStrings(t *testing.T) {
	in := `{"a": "8080", "b": "true", "c": "null", "d": "plain", "e": "\"q\"", "f": 8080, "g": "", "h": "08", "i": "<&>1"}`
	f, err := FromJSON([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := HashLine + `
a "8080"
b "true"
c "null"
d plain
e "\\"q\\""
f 8080
g \0
h 08
i <&>1
`
	if f.String() != want {
		t.Errorf("kv form:\n%s\nwant\n%s", f.String(), want)
	}
	back, err := Parse(f.String())
	if err != nil {
		t.Fatal(err)
	}
	out, err := ToJSON(back)
	if err != nil {
		t.Fatal(err)
	}
	f2, err := FromJSON(out)
	if err != nil || f2.String() != f.String() {
		t.Errorf("JSON round trip: %v\n%s", err, out)
	}
	y, err := ToYAML(back)
	if err != nil {
		t.Fatal(err)
	}
	wantYAML := "a: \"8080\"\nb: \"true\"\nc: \"null\"\nd: plain\ne: '\"q\"'\nf: 8080\ng: \"\"\nh: \"08\"\ni: <&>1\n"
	if string(y) != wantYAML {
		t.Errorf("yaml:\n%s\nwant\n%s", y, wantYAML)
	}
	f3, err := FromYAML(y)
	if err != nil || f3.String() != f.String() {
		t.Errorf("YAML round trip: %v\n%s", err, f3)
	}
}
