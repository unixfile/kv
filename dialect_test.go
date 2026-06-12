package kv

import "testing"

func TestParseSegmentJSONKeys(t *testing.T) {
	verbatim := []string{
		"maxRetries", "Content-Type", "$schema", "_id", "@type",
		"2x", "färg", "UPPER", `a\b`, "a=b",
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
	if _, err := parseSegment("maxRetries", Strict); err == nil {
		t.Error("parseSegment(maxRetries, Strict): expected error")
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

// a dotted object name would silently split into path structure, so it
// is an error in every dialect, and for marshalled map keys too
func TestDottedNamesRejected(t *testing.T) {
	for _, d := range []Dialect{Strict, JSONKeys} {
		if _, err := d.FromJSON([]byte(`{"a.b": 1}`)); err == nil {
			t.Errorf("dialect %d: dotted object name accepted", d)
		}
	}
	if _, err := Marshal(map[string]string{"a.b": "x"}); err == nil {
		t.Error("Marshal: dotted map key accepted")
	}
}
