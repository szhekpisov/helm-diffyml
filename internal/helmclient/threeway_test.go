package helmclient

import (
	"bytes"
	"strings"
	"testing"

	"helm.sh/helm/v3/pkg/release"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

// TestComputeThreeWayStrategicMergeMergesArraysByKey demonstrates the
// strategic-merge advantage. A Deployment with two containers (sidecar +
// web) where the chart only changes web's image must NOT replace the
// sidecar — strategic merge lists `containers` with patchMergeKey=name.
func TestComputeThreeWayStrategicMergeMergesArraysByKey(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	original := []byte(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web"},"spec":{"template":{"spec":{"containers":[{"name":"web","image":"nginx:1.25"},{"name":"sidecar","image":"envoy:1.30"}]}}}}`)
	modified := []byte(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web"},"spec":{"template":{"spec":{"containers":[{"name":"web","image":"nginx:1.27"},{"name":"sidecar","image":"envoy:1.30"}]}}}}`)
	live := []byte(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web"},"spec":{"template":{"spec":{"containers":[{"name":"web","image":"nginx:1.25"},{"name":"sidecar","image":"envoy:1.30"}]}}}}`)

	patch, projected, err := computeThreeWay(gvk, original, modified, live)
	if err != nil {
		t.Fatalf("computeThreeWay: %v", err)
	}
	// Strategic patch should target only the changed container; if we had
	// gone through JSON merge, the patch would replace the entire array.
	if !contains(patch, "nginx:1.27") || !contains(patch, "name") {
		t.Errorf("expected patch to target the web container; got %s", patch)
	}
	if !contains(projected, "envoy:1.30") {
		t.Errorf("expected sidecar to survive in projected state; got %s", projected)
	}
}

// TestMergeHooks covers the includeNonTest / includeTests matrix that
// drives --no-hooks / --include-tests behaviour.
func TestMergeHooks(t *testing.T) {
	manifest := "kind: ConfigMap\nmetadata:\n  name: cm\n"
	hooks := []*release.Hook{
		{Name: "pre-up", Manifest: "kind: Job\nmetadata:\n  name: pre-up\n", Events: []release.HookEvent{release.HookPreUpgrade}},
		{Name: "test-conn", Manifest: "kind: Pod\nmetadata:\n  name: test-conn\n", Events: []release.HookEvent{release.HookTest}},
	}

	cases := []struct {
		name              string
		includeNonTest    bool
		includeTests      bool
		wantPreUpgrade    bool
		wantTestConn      bool
	}{
		{name: "neither", includeNonTest: false, includeTests: false, wantPreUpgrade: false, wantTestConn: false},
		{name: "non-test only (helm-diff default)", includeNonTest: true, includeTests: false, wantPreUpgrade: true, wantTestConn: false},
		{name: "with tests", includeNonTest: true, includeTests: true, wantPreUpgrade: true, wantTestConn: true},
		{name: "tests only (unusual)", includeNonTest: false, includeTests: true, wantPreUpgrade: false, wantTestConn: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeHooks(manifest, hooks, tc.includeNonTest, tc.includeTests)
			if has := strings.Contains(got, "pre-up"); has != tc.wantPreUpgrade {
				t.Errorf("pre-upgrade hook: got=%v want=%v\n%s", has, tc.wantPreUpgrade, got)
			}
			if has := strings.Contains(got, "test-conn"); has != tc.wantTestConn {
				t.Errorf("test hook: got=%v want=%v\n%s", has, tc.wantTestConn, got)
			}
			if !strings.Contains(got, "kind: ConfigMap") {
				t.Errorf("manifest body should always be present, got:\n%s", got)
			}
		})
	}
}

// TestComputeThreeWayUnknownKindFallsBackToJSONMerge ensures that types not
// in the kubectl scheme (CRDs, etc.) still produce a valid patch via JSON
// merge.
func TestComputeThreeWayUnknownKindFallsBackToJSONMerge(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Whatever"}
	original := []byte(`{"spec":{"size":1}}`)
	modified := []byte(`{"spec":{"size":3}}`)
	live := []byte(`{"spec":{"size":2}}`)
	patch, projected, err := computeThreeWay(gvk, original, modified, live)
	if err != nil {
		t.Fatalf("computeThreeWay (CRD): %v", err)
	}
	if !contains(patch, "size") {
		t.Errorf("expected json-merge patch to mention size, got %s", patch)
	}
	if !contains(projected, "3") {
		t.Errorf("expected projected size=3, got %s", projected)
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
