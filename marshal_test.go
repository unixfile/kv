package kv

import (
	"reflect"
	"testing"
)

type testConfig struct {
	Host    string `kv:"host"`
	Port    int
	Debug   bool
	Ratio   float64
	Tags    []string
	Extra   map[string]string
	Ignored string `kv:"-"`
	Note    *string
}

func TestMarshal(t *testing.T) {
	note := "hi"
	c := testConfig{
		Host:    "localhost",
		Port:    8080,
		Debug:   true,
		Ratio:   2.5,
		Tags:    []string{"a", "b"},
		Extra:   map[string]string{"x": "1"},
		Ignored: "nope",
		Note:    &note,
	}
	f, err := Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	want := HashLine + "\n" +
		"debug true\n" +
		"extra.x 1\n" +
		"host localhost\n" +
		"note hi\n" +
		"port 8080\n" +
		"ratio 2.5\n" +
		"tags.0 a\n" +
		"tags.1 b\n"
	if f.String() != want {
		t.Errorf("Marshal:\n%q\nwant\n%q", f.String(), want)
	}
}

func TestMarshalOmitsNil(t *testing.T) {
	f, err := Marshal(testConfig{Host: "h"})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range f.Pairs {
		if p.Key.String() == "note" {
			t.Error("nil pointer field was marshalled")
		}
	}
}

func TestMarshalEmptyContainers(t *testing.T) {
	f, err := Marshal(struct {
		Tags  []string
		Extra map[string]string
	}{Tags: []string{}, Extra: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	want := HashLine + "\nextra\ntags\n"
	if f.String() != want {
		t.Errorf("empty containers:\n%q\nwant\n%q", f.String(), want)
	}
}

func TestMarshalPadding(t *testing.T) {
	f, err := Marshal(struct{ N []int }{N: make([]int, 11)})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Pairs[0].Key.String(); got != "n.00" {
		t.Errorf("first key = %q, want n.00", got)
	}
}

func TestMarshalErrors(t *testing.T) {
	if _, err := Marshal(nil); err == nil {
		t.Error("marshal nil: expected error")
	}
	if _, err := Marshal("scalar"); err == nil {
		t.Error("marshal top-level scalar: expected error")
	}
	if _, err := Marshal(struct{ C chan int }{}); err == nil {
		t.Error("marshal chan: expected error")
	}
	if _, err := Marshal(map[int]string{1: "x"}); err == nil {
		t.Error("marshal int-keyed map: expected error")
	}
	if _, err := Marshal(struct{ Bad string }{Bad: "\x01"}); err == nil {
		t.Error("marshal control char: expected error")
	}
}

func TestUnmarshal(t *testing.T) {
	note := "hi"
	in := testConfig{
		Host:  "localhost",
		Port:  8080,
		Debug: true,
		Ratio: 2.5,
		Tags:  []string{"a", "b"},
		Extra: map[string]string{"x": "1"},
		Note:  &note,
	}
	f, err := Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out testConfig
	if err := Unmarshal(f, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round trip:\n%+v\nwant\n%+v", out, in)
	}
}

func TestUnmarshalGeneric(t *testing.T) {
	f, err := Parse("a.0 x\na.1 y\nb.c 5\nm\n")
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := Unmarshal(f, &v); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"a": []any{"x", "y"},
		"b": map[string]any{"c": "5"},
		"m": map[string]any{},
	}
	if !reflect.DeepEqual(v, want) {
		t.Errorf("generic: %#v", v)
	}
}

func TestUnmarshalMarkerIsEmptyContainer(t *testing.T) {
	f, err := Parse("tags\n")
	if err != nil {
		t.Fatal(err)
	}
	var out struct{ Tags []string }
	if err := Unmarshal(f, &out); err != nil {
		t.Fatal(err)
	}
	if out.Tags == nil || len(out.Tags) != 0 {
		t.Errorf("marker into slice = %#v, want empty non-nil", out.Tags)
	}
}

func TestUnmarshalIgnoresUnknown(t *testing.T) {
	f, err := Parse("host x\nstray y\n")
	if err != nil {
		t.Fatal(err)
	}
	var out struct{ Host string }
	if err := Unmarshal(f, &out); err != nil {
		t.Fatal(err)
	}
	if out.Host != "x" {
		t.Errorf("host = %q", out.Host)
	}
}

func TestUnmarshalErrors(t *testing.T) {
	f, err := Parse("port abc\n")
	if err != nil {
		t.Fatal(err)
	}
	var out struct{ Port int }
	if err := Unmarshal(f, &out); err == nil {
		t.Error("bad int: expected error")
	}
	if err := Unmarshal(f, out); err == nil {
		t.Error("non-pointer target: expected error")
	}
	f2, err := Parse("a.b x\n")
	if err != nil {
		t.Fatal(err)
	}
	var s struct{ A []string }
	if err := Unmarshal(f2, &s); err == nil {
		t.Error("named children into slice: expected error")
	}
}

func TestDottedTagRejected(t *testing.T) {
	type c struct {
		URL string `kv:"server.url"`
	}
	if _, err := Marshal(c{URL: "x"}); err == nil {
		t.Error("marshal dotted tag: expected error")
	}
	f, err := Parse("server.url x\n")
	if err != nil {
		t.Fatal(err)
	}
	var out c
	if err := Unmarshal(f, &out); err == nil {
		t.Error("unmarshal dotted tag: expected error")
	}
}

func TestUnmarshalArrayNamedChildren(t *testing.T) {
	f, err := Parse("a.b x\n")
	if err != nil {
		t.Fatal(err)
	}
	var s struct{ A [2]string }
	if err := Unmarshal(f, &s); err == nil {
		t.Error("named children into array: expected error")
	}
}
