package kv

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestFromJSON(t *testing.T) {
	in := `{
		"build": {"parallel": "yes", "steps": ["./configure", "make"]},
		"count": 3,
		"empty": {},
		"flag": true,
		"nothing": null,
		"pi": 3.14
	}`
	f, err := FromJSON([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := HashLine + "\n" +
		"build.parallel yes\n" +
		"build.steps.0 ./configure\n" +
		"build.steps.1 make\n" +
		"count 3\n" +
		"empty\n" +
		"flag true\n" +
		"nothing null\n" +
		"pi 3.14\n"
	if f.String() != want {
		t.Errorf("FromJSON:\n%q\nwant\n%q", f.String(), want)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	in := `{"a":{"b":["x","y"],"n":5},"flag":false,"nada":null,"s":"08"}`
	f, err := FromJSON([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	out, err := ToJSON(f)
	if err != nil {
		t.Fatal(err)
	}
	var got, want any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(in), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip:\n%s\nwant\n%s", out, in)
	}
}

func TestFromJSONArrayPadding(t *testing.T) {
	in := `{"n": [0,1,2,3,4,5,6,7,8,9,10]}`
	f, err := FromJSON([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Pairs[0].Key.String(); got != "n.00" {
		t.Errorf("first key = %q, want n.00", got)
	}
	if got := f.Pairs[10].Key.String(); got != "n.10" {
		t.Errorf("last key = %q, want n.10", got)
	}
}

func TestFromJSONErrors(t *testing.T) {
	cases := map[string]string{
		"top-level scalar": `5`,
		"top-level null":   `null`,
		"uppercase key":    `{"Name": "x"}`,
		"dashed key":       `{"a-b": "x"}`,
		"control char":     `{"a": "xy"}`,
		"empty key":        `{"": "x"}`,
		"trailing garbage": `{"a": "x"} junk`,
		"second value":     `{"a": "x"} {"b": "y"}`,
	}
	for name, in := range cases {
		if _, err := FromJSON([]byte(in)); err == nil {
			t.Errorf("%s: expected error for %s", name, in)
		}
	}
}

func TestToJSONMarker(t *testing.T) {
	f, err := Parse("planets\n")
	if err != nil {
		t.Fatal(err)
	}
	out, err := ToJSON(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "{\n  \"planets\": {}\n}\n" {
		t.Errorf("marker to JSON: %q", out)
	}
}

func TestToJSONMixedNode(t *testing.T) {
	f, err := Parse("build.0 configure\nbuild.parallel yes\n")
	if err != nil {
		t.Fatal(err)
	}
	out, err := ToJSON(f)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]map[string]string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]map[string]string{"build": {"0": "configure", "parallel": "yes"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mixed node: %s", out)
	}
}

func TestGuessScalar(t *testing.T) {
	raw := []string{"5", "-1", "3.14", "1e5", "true", "false", "null"}
	for _, v := range raw {
		if _, ok := guessScalar(v).(json.RawMessage); !ok {
			t.Errorf("guessScalar(%q): expected raw JSON", v)
		}
	}
	str := []string{"", "08", "1.", "5 apples", "yes", "True", "0x1f", "1,5"}
	for _, v := range str {
		if _, ok := guessScalar(v).(string); !ok {
			t.Errorf("guessScalar(%q): expected string", v)
		}
	}
}

func TestEmptyTopLevelJSON(t *testing.T) {
	for _, in := range []string{`{}`, `[]`} {
		f, err := FromJSON([]byte(in))
		if err != nil {
			t.Fatal(err)
		}
		if len(f.Pairs) != 0 {
			t.Errorf("FromJSON(%s): expected empty file, got %q", in, f.String())
		}
	}
}
