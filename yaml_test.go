package kv

import (
	"strings"
	"testing"
)

func TestFromYAML(t *testing.T) {
	in := `# a comment
apiVersion: v1
kind: Pod
metadata:
  name: web
  labels: {}
spec:
  containers:
    - name: nginx
      ports:
        - containerPort: 0x50
      env: []
  enabled: True
  ratio: 1.50
  nothing: ~
  note: |
    two
    lines
`
	f, err := JSONKeys.FromYAML([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := HashLine + `
apiVersion v1
kind Pod
metadata.labels
metadata.name web
spec.containers.0.env
spec.containers.0.name nginx
spec.containers.0.ports.0.containerPort 80
spec.enabled true
spec.note two\nlines\n
spec.nothing null
spec.ratio 1.50
`
	if f.String() != want {
		t.Errorf("kv form:\n%s\nwant\n%s", f.String(), want)
	}
	// camelCase names fit the strict grammar since spec 1.1
	if _, err := FromYAML([]byte(in)); err != nil {
		t.Errorf("strict FromYAML: %v", err)
	}
	if _, err := FromYAML([]byte("app.kubernetes.io/name: x\n")); err == nil {
		t.Error("dotted name accepted")
	}
	if _, err := FromYAML([]byte("a-b: x\n")); err == nil {
		t.Error("strict FromYAML accepted a foreign name")
	}
}

func TestFromYAMLAnchorsAndMerge(t *testing.T) {
	in := `base: &base
  cpu: 1
  mem: 2
web:
  <<: *base
  mem: 4
list: [*base]
`
	f, err := FromYAML([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := HashLine + `
base.cpu 1
base.mem 2
list.0.cpu 1
list.0.mem 2
web.cpu 1
web.mem 4
`
	if f.String() != want {
		t.Errorf("kv form:\n%s\nwant\n%s", f.String(), want)
	}
}

func TestYAMLStream(t *testing.T) {
	in := "kind: Service\n---\n---\nkind: Deployment\nreplicas: 3\n"
	f, err := FromYAML([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := HashLine + "\n0.kind Service\n1.kind Deployment\n1.replicas 3\n"
	if f.String() != want {
		t.Errorf("kv form:\n%q\nwant\n%q", f.String(), want)
	}
	out, err := ToYAML(f)
	if err != nil {
		t.Fatal(err)
	}
	if want := "kind: Service\n---\nkind: Deployment\nreplicas: 3\n"; string(out) != want {
		t.Errorf("stream:\n%q\nwant\n%q", out, want)
	}
}

func TestToYAMLScalars(t *testing.T) {
	f, err := Parse(HashLine + "\na 8080\nb true\nc null\nd 1.50\ne 08\nf yes\ng hello world\nh.0 x\ni\n")
	if err != nil {
		t.Fatal(err)
	}
	out, err := ToYAML(f)
	if err != nil {
		t.Fatal(err)
	}
	want := "a: 8080\nb: true\nc: null\nd: 1.50\ne: \"08\"\nf: \"yes\"\ng: hello world\nh:\n  - x\ni: {}\n"
	if string(out) != want {
		t.Errorf("yaml:\n%s\nwant\n%s", out, want)
	}
	back, err := FromYAML(out)
	if err != nil {
		t.Fatal(err)
	}
	if back.String() != f.String() {
		t.Errorf("round trip:\n%s\nwant\n%s", back.String(), f.String())
	}
}

func TestFromYAMLErrors(t *testing.T) {
	cases := map[string]string{
		"top-level scalar": "5\n",
		"duplicate name":   "a: 1\na: 2\n",
		"bad syntax":       "a: [1\n",
		"alias bomb": "a: &a [x,x,x,x,x,x,x,x,x,x]\nb: &b [*a,*a,*a,*a,*a,*a,*a,*a,*a,*a]\n" +
			"c: &c [*b,*b,*b,*b,*b,*b,*b,*b,*b,*b]\nd: &d [*c,*c,*c,*c,*c,*c,*c,*c,*c,*c]\n" +
			"e: &e [*d,*d,*d,*d,*d,*d,*d,*d,*d,*d]\nf: &f [*e,*e,*e,*e,*e,*e,*e,*e,*e,*e]\n" +
			"g: [*f,*f,*f,*f,*f,*f,*f,*f,*f,*f]\n",
	}
	for name, in := range cases {
		if _, err := FromYAML([]byte(in)); err == nil {
			t.Errorf("%s: expected error", name)
		} else if name == "alias bomb" && !strings.Contains(err.Error(), "too large") {
			t.Errorf("alias bomb: %v", err)
		}
	}
	if f, err := FromYAML(nil); err != nil || len(f.Pairs) != 0 {
		t.Errorf("empty stream: %v, %v", f, err)
	}
}
