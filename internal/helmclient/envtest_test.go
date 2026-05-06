package helmclient

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// envtest is shared across this package's tests so the API server is started
// at most once. KUBEBUILDER_ASSETS must point at the kube-apiserver/etcd
// binaries (run `setup-envtest use <ver> -p path` to install). Tests
// requiring envtest call requireEnvtest() and skip cleanly when binaries
// aren't installed.

var (
	envtestCfg     *rest.Config
	envtestSkipMsg string
	envtestEnv     *envtest.Environment
)

func TestMain(m *testing.M) {
	envtestEnv = &envtest.Environment{}
	cfg, err := envtestEnv.Start()
	if err != nil {
		envtestSkipMsg = err.Error()
	} else {
		envtestCfg = cfg
	}

	code := m.Run()

	if envtestCfg != nil {
		_ = envtestEnv.Stop()
	}
	os.Exit(code)
}

func requireEnvtest(t *testing.T) *rest.Config {
	t.Helper()
	if envtestCfg == nil {
		t.Skipf("envtest unavailable (set KUBEBUILDER_ASSETS via `setup-envtest use 1.29 -p path`): %s", envtestSkipMsg)
	}
	return envtestCfg
}

// writeKubeconfig serialises a rest.Config as a kubeconfig file so cli.New()
// can pick it up via KUBECONFIG. Avoids reaching into helm's internal
// RESTClientGetter wiring.
func writeKubeconfig(t *testing.T, cfg *rest.Config) string {
	t.Helper()
	caB64 := base64.StdEncoding.EncodeToString(cfg.CAData)
	certB64 := base64.StdEncoding.EncodeToString(cfg.CertData)
	keyB64 := base64.StdEncoding.EncodeToString(cfg.KeyData)
	body := fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: envtest
clusters:
- name: envtest
  cluster:
    server: %s
    certificate-authority-data: %s
contexts:
- name: envtest
  context:
    cluster: envtest
    user: envtest
users:
- name: envtest
  user:
    client-certificate-data: %s
    client-key-data: %s
`, cfg.Host, caB64, certB64, keyB64)
	path := filepath.Join(t.TempDir(), "kubeconfig.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// liveClient returns a *Client wired to the envtest API server, including a
// fresh in-memory release store so each test starts from a clean slate.
func liveClient(t *testing.T) *Client {
	cfg := requireEnvtest(t)
	t.Setenv("KUBECONFIG", writeKubeconfig(t, cfg))
	c, err := New("default", "", false)
	if err != nil {
		t.Fatalf("New against envtest: %v", err)
	}
	return c
}

// TestThreeWayMergedAgainstLiveClusterPureAdditions hits the not-found
// branch of getLive: the chart renders three resources, none exist yet, so
// each becomes a pure addition (live empty, projected populated).
func TestThreeWayMergedAgainstLiveClusterPureAdditions(t *testing.T) {
	c := liveClient(t)
	chartPath, _ := loadFixtureChart(t)

	live, projected, err := c.ThreeWayMerged("fresh-rel", chartPath, RenderOptions{}, false)
	if err != nil {
		t.Fatalf("ThreeWayMerged: %v", err)
	}
	if len(live) != 0 {
		t.Errorf("expected empty live stream for pure additions, got %d bytes", len(live))
	}
	for _, want := range []string{"Deployment", "ConfigMap", "Secret"} {
		if !strings.Contains(string(projected), want) {
			t.Errorf("expected projected to contain %q, got:\n%s", want, projected)
		}
	}
}

// TestThreeWayMergedAgainstLiveClusterDriftDetection seeds a Deployment in
// the envtest cluster, then renders a chart that targets the same name with
// a different replica count. The three-way result should contain both live
// and projected streams with the replica change visible.
func TestThreeWayMergedAgainstLiveClusterDriftDetection(t *testing.T) {
	c := liveClient(t)
	chartPath, _ := loadFixtureChart(t)

	// Create a live Deployment matching the rendered name (`<release>-web`)
	// with replicas=99 so the chart's replicas=2 default produces a clear
	// 99 → 2 diff in the three-way result.
	dyn, mapper, err := c.dynamicLookup()
	if err != nil {
		t.Fatalf("dynamicLookup: %v", err)
	}
	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		t.Fatalf("rest mapping: %v", err)
	}
	dep := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":      "drift-rel-web",
			"namespace": "default",
		},
		"spec": map[string]any{
			"replicas": int64(99),
			"selector": map[string]any{
				"matchLabels": map[string]any{"app.kubernetes.io/name": "drift-rel"},
			},
			"template": map[string]any{
				"metadata": map[string]any{
					"labels": map[string]any{"app.kubernetes.io/name": "drift-rel"},
				},
				"spec": map[string]any{
					"containers": []any{
						map[string]any{"name": "web", "image": "nginx:1.25"},
					},
				},
			},
		},
	}}
	if _, err := dyn.Resource(mapping.Resource).Namespace("default").Create(context.TODO(), dep, metav1Create()); err != nil {
		t.Fatalf("seed deployment: %v", err)
	}

	live, projected, err := c.ThreeWayMerged("drift-rel", chartPath, RenderOptions{}, false)
	if err != nil {
		t.Fatalf("ThreeWayMerged: %v", err)
	}
	if !strings.Contains(string(live), "replicas: 99") {
		t.Errorf("expected live stream to contain replicas: 99, got:\n%s", live)
	}
	if !strings.Contains(string(projected), "replicas: 2") {
		t.Errorf("expected projected stream to contain replicas: 2, got:\n%s", projected)
	}
}

// TestNewWithExtraOptions covers the kubeContext + debug branches of New
// against a live envtest cluster.
func TestNewWithExtraOptions(t *testing.T) {
	cfg := requireEnvtest(t)
	t.Setenv("KUBECONFIG", writeKubeconfig(t, cfg))
	c, err := New("default", "envtest", true)
	if err != nil {
		t.Fatalf("New with debug + kubeContext: %v", err)
	}
	if c.Settings().KubeContext != "envtest" {
		t.Errorf("expected kubeContext to be set, got %q", c.Settings().KubeContext)
	}
}

// TestNewBadDriverReturnsError exercises New's cfg.Init error branch.
func TestNewBadDriverReturnsError(t *testing.T) {
	cfg := requireEnvtest(t)
	t.Setenv("KUBECONFIG", writeKubeconfig(t, cfg))
	t.Setenv("HELM_DRIVER", "this-driver-does-not-exist")
	if _, err := New("default", "", false); err == nil {
		t.Fatal("expected init error from bogus HELM_DRIVER")
	}
}

// TestGetLiveNotFound exercises getLive for a resource that doesn't exist.
func TestGetLiveNotFound(t *testing.T) {
	c := liveClient(t)
	_, mapper, err := c.dynamicLookup()
	if err != nil {
		t.Fatal(err)
	}
	dyn, _, _ := c.dynamicLookup()
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": "absent"},
	}}
	if _, err := getLive(context.TODO(), dyn, mapper, obj, "default"); !apierrors.IsNotFound(err) {
		t.Errorf("expected NotFound, got %v", err)
	}
}
