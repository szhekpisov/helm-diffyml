package helmclient

import (
	"bytes"
	"strings"
	"testing"
)

const renderedFixture = `---
apiVersion: v1
kind: Secret
metadata:
  name: my-rel-api
  namespace: prod
stringData:
  api-key: secret
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-rel-config
data:
  greeting: hello
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-rel-web
spec:
  replicas: 2
`

func TestSplitYAMLToUnstructured(t *testing.T) {
	objs, err := splitYAMLToUnstructured([]byte(renderedFixture))
	if err != nil {
		t.Fatalf("split error: %v", err)
	}
	if len(objs) != 3 {
		t.Fatalf("expected 3 objects, got %d", len(objs))
	}
	kinds := []string{objs[0].GetKind(), objs[1].GetKind(), objs[2].GetKind()}
	if kinds[0] != "Secret" || kinds[1] != "ConfigMap" || kinds[2] != "Deployment" {
		t.Errorf("unexpected kinds: %v", kinds)
	}
	if objs[0].GetNamespace() != "prod" {
		t.Errorf("expected namespace prod from explicit metadata.namespace, got %q", objs[0].GetNamespace())
	}
	if objs[1].GetNamespace() != "" {
		t.Errorf("ConfigMap had no namespace; expected empty, got %q", objs[1].GetNamespace())
	}
}

func TestSplitYAMLToUnstructuredEmptyDocsSkipped(t *testing.T) {
	in := []byte("---\n---\n---\nkind: Foo\nmetadata:\n  name: f\n")
	objs, err := splitYAMLToUnstructured(in)
	if err != nil {
		t.Fatalf("split error: %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("empty docs should be skipped, got %d objects", len(objs))
	}
}

func TestSplitYAMLToUnstructuredMalformed(t *testing.T) {
	_, err := splitYAMLToUnstructured([]byte("kind: Foo\nname: : :\n"))
	if err == nil {
		t.Fatal("expected error on malformed yaml")
	}
}

func TestObjKeyAndKeyFromYAMLDoc(t *testing.T) {
	objs, _ := splitYAMLToUnstructured([]byte(renderedFixture))
	want := []string{
		"secret/prod/my-rel-api",
		"configmap//my-rel-config",
		"deployment//my-rel-web",
	}
	for i, obj := range objs {
		if got := objKey(obj); got != want[i] {
			t.Errorf("objKey[%d]=%q want %q", i, got, want[i])
		}
	}

	doc := "kind: Secret\nmetadata:\n  name: alpha\n  namespace: ns1\n"
	got, err := keyFromYAMLDoc(doc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "secret/ns1/alpha" {
		t.Errorf("keyFromYAMLDoc=%q", got)
	}

	if _, err := keyFromYAMLDoc("metadata:\n  name: alpha\n"); err == nil {
		t.Error("expected error when kind is missing")
	}
	if _, err := keyFromYAMLDoc("kind: Foo\n"); err == nil {
		t.Error("expected error when name is missing")
	}
	if _, err := keyFromYAMLDoc("not: : valid"); err == nil {
		t.Error("expected error on malformed yaml")
	}
}

func TestSplitYAMLDocs(t *testing.T) {
	in := "---\nfoo: 1\n---\n\n---\nbar: 2\n"
	out := splitYAMLDocs(in)
	if len(out) != 2 {
		t.Fatalf("expected 2 docs, got %d: %v", len(out), out)
	}
	if !strings.Contains(out[0], "foo: 1") || !strings.Contains(out[1], "bar: 2") {
		t.Errorf("doc bodies wrong: %v", out)
	}
}

func TestLastAppliedFromLive(t *testing.T) {
	live := []byte(`{
  "metadata": {
    "annotations": {
      "kubectl.kubernetes.io/last-applied-configuration": "{\"kind\":\"X\"}"
    }
  }
}`)
	got := lastAppliedFromLive(live)
	if string(got) != `{"kind":"X"}` {
		t.Errorf("unexpected last-applied: %q", got)
	}

	if got := lastAppliedFromLive([]byte(`{"metadata":{}}`)); got != nil {
		t.Errorf("expected nil when annotation absent, got %q", got)
	}
	if got := lastAppliedFromLive([]byte("not json")); got != nil {
		t.Errorf("expected nil on non-json input, got %q", got)
	}
}

func TestWriteJSONAsYAMLDoc(t *testing.T) {
	var buf bytes.Buffer
	if err := writeJSONAsYAMLDoc(&buf, []byte(`{"kind":"Foo","metadata":{"name":"alpha"}}`)); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "---\n") {
		t.Errorf("expected leading separator, got: %q", out)
	}
	if !strings.Contains(out, "kind: Foo") || !strings.Contains(out, "name: alpha") {
		t.Errorf("expected yaml-encoded body, got: %q", out)
	}
}

func TestShortPatch(t *testing.T) {
	short := []byte(`{"a":1}`)
	if got := shortPatch(short); got != `{"a":1}` {
		t.Errorf("short input should pass through, got %q", got)
	}

	long := bytes.Repeat([]byte("x"), 250)
	got := shortPatch(long)
	if !strings.HasSuffix(got, "...(truncated)") {
		t.Errorf("expected truncation marker, got: %q", got)
	}
	if len(got) >= len(long) {
		t.Errorf("expected truncated output to be shorter than input")
	}
}
