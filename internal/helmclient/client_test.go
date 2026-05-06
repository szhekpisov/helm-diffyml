package helmclient

import (
	"io"
	"path/filepath"
	"testing"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	kubefake "helm.sh/helm/v3/pkg/kube/fake"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
	helmtime "helm.sh/helm/v3/pkg/time"
)

// memoryClient builds a *Client backed by an in-memory release storage and a
// fake KubeClient. Sufficient for exercising every helmclient method that
// does not need a live cluster (everything except the dynamic-client lookups
// inside ThreeWayMerged, which are tested separately).
func memoryClient(t *testing.T) *Client {
	t.Helper()
	mem := driver.NewMemory()
	cfg := &action.Configuration{
		Releases:     storage.Init(mem),
		KubeClient:   &kubefake.PrintingKubeClient{Out: io.Discard},
		Capabilities: chartutil.DefaultCapabilities,
		Log:          func(string, ...interface{}) {},
	}
	settings := cli.New()
	settings.SetNamespace("default")
	return NewWithConfig(settings, cfg)
}

// loadFixtureChart walks up to test/fixture-chart from any internal subpath.
func loadFixtureChart(t *testing.T) (string, *chart.Chart) {
	t.Helper()
	// Tests run with cwd = the package dir, so step up two levels to repo root.
	chartPath, err := filepath.Abs(filepath.Join("..", "..", "test", "fixture-chart"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := loader.Load(chartPath)
	if err != nil {
		t.Fatalf("load fixture chart: %v", err)
	}
	return chartPath, c
}

// seedRelease puts a deployed release into the in-memory storage so
// GetManifest/History have something to read.
func seedRelease(t *testing.T, c *Client, name string, version int, manifest string) {
	t.Helper()
	ch, _ := loadFixtureChart(t)
	_ = ch
	rel := &release.Release{
		Name:      name,
		Namespace: "default",
		Version:   version,
		Info: &release.Info{
			FirstDeployed: helmtime.Now(),
			LastDeployed:  helmtime.Now(),
			Status:        release.StatusDeployed,
		},
		Chart:    &chart.Chart{Metadata: &chart.Metadata{Name: "fix", Version: "0.1.0"}},
		Manifest: manifest,
	}
	if err := c.cfg.Releases.Create(rel); err != nil {
		t.Fatalf("seed release: %v", err)
	}
}

const seededManifestRev1 = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  replicas: 2
`

func TestNewWithConfigSettings(t *testing.T) {
	c := memoryClient(t)
	if c.Settings() == nil {
		t.Error("Settings() returned nil")
	}
	if c.namespaceFor(RenderOptions{Namespace: "explicit"}) != "explicit" {
		t.Errorf("namespaceFor should honour explicit option")
	}
	if c.namespaceFor(RenderOptions{}) != "default" {
		t.Errorf("namespaceFor should fall back to client namespace; got %q", c.namespaceFor(RenderOptions{}))
	}
}

func TestGetManifestNotFoundReturnsNil(t *testing.T) {
	c := memoryClient(t)
	got, err := c.GetManifest("nope", 0)
	if err != nil {
		t.Fatalf("expected nil err on missing release, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil bytes for missing release, got %q", got)
	}
}

func TestGetManifestExisting(t *testing.T) {
	c := memoryClient(t)
	seedRelease(t, c, "alpha", 1, seededManifestRev1)
	got, err := c.GetManifest("alpha", 0)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != seededManifestRev1 {
		t.Errorf("manifest mismatch:\n%s", got)
	}
}

func TestHistoryAndPreviousRevision(t *testing.T) {
	c := memoryClient(t)
	seedRelease(t, c, "alpha", 1, seededManifestRev1)
	seedRelease(t, c, "alpha", 2, seededManifestRev1+"# rev2\n")
	seedRelease(t, c, "alpha", 3, seededManifestRev1+"# rev3\n")

	hist, err := c.History("alpha", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 3 {
		t.Errorf("expected 3 history entries, got %d", len(hist))
	}

	prev, err := c.PreviousRevision("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if prev != 2 {
		t.Errorf("expected previous=2 (newest is 3), got %d", prev)
	}
}

func TestPreviousRevisionSingleRelease(t *testing.T) {
	c := memoryClient(t)
	seedRelease(t, c, "solo", 1, seededManifestRev1)
	prev, err := c.PreviousRevision("solo")
	if err != nil {
		t.Fatal(err)
	}
	if prev != 0 {
		t.Errorf("with only one revision, previous should be 0, got %d", prev)
	}
}

func TestPreviousRevisionMissingRelease(t *testing.T) {
	c := memoryClient(t)
	if _, err := c.PreviousRevision("does-not-exist"); err == nil {
		t.Error("expected error from missing release history")
	}
}

func TestTemplate(t *testing.T) {
	c := memoryClient(t)
	chartPath, _ := loadFixtureChart(t)

	out, err := c.Template("my-rel", chartPath, RenderOptions{Namespace: "default"})
	if err != nil {
		t.Fatalf("Template: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("Template returned empty bytes")
	}
	for _, want := range []string{"Deployment", "ConfigMap", "Secret", "my-rel-web"} {
		if !contains(out, want) {
			t.Errorf("expected rendered manifest to contain %q, got:\n%s", want, out)
		}
	}
}

func TestTemplateMissingChart(t *testing.T) {
	c := memoryClient(t)
	if _, err := c.Template("rel", "/nope/missing-chart", RenderOptions{}); err == nil {
		t.Error("expected error loading missing chart")
	}
}

func TestInstallDryRunRendersChart(t *testing.T) {
	c := memoryClient(t)
	chartPath, _ := loadFixtureChart(t)
	out, err := c.InstallDryRun("fresh", chartPath, RenderOptions{})
	if err != nil {
		t.Fatalf("InstallDryRun: %v", err)
	}
	if !contains(out, "Deployment") {
		t.Errorf("expected dry-run output to contain rendered Deployment, got:\n%s", out)
	}
}

func TestInstallDryRunMissingChart(t *testing.T) {
	c := memoryClient(t)
	if _, err := c.InstallDryRun("rel", "/no/such/chart", RenderOptions{}); err == nil {
		t.Error("expected error loading missing chart")
	}
}

func TestUpgradeDryRunRendersChart(t *testing.T) {
	c := memoryClient(t)
	seedRelease(t, c, "rel", 1, seededManifestRev1)
	chartPath, _ := loadFixtureChart(t)

	out, err := c.UpgradeDryRun("rel", chartPath, RenderOptions{})
	if err != nil {
		t.Fatalf("UpgradeDryRun: %v", err)
	}
	if !contains(out, "Deployment") {
		t.Errorf("expected dry-run upgrade output, got:\n%s", out)
	}
}

func TestUpgradeDryRunMissingChart(t *testing.T) {
	c := memoryClient(t)
	seedRelease(t, c, "rel", 1, seededManifestRev1)
	if _, err := c.UpgradeDryRun("rel", "/no/such/chart", RenderOptions{}); err == nil {
		t.Error("expected error loading missing chart")
	}
}

func TestTemplateReuseValuesMergesDeployedValues(t *testing.T) {
	chartPath, fixtureChart := loadFixtureChart(t)
	// Each scenario seeds its own client because action.NewInstall.Run
	// (used by Template) mutates the in-memory release storage with a
	// "pending" placeholder, which would otherwise hide the seeded values
	// from subsequent action.NewGetValues calls.
	seed := func(name string) *Client {
		c := memoryClient(t)
		rel := &release.Release{
			Name:      name,
			Namespace: "default",
			Version:   1,
			Info: &release.Info{
				FirstDeployed: helmtime.Now(),
				LastDeployed:  helmtime.Now(),
				Status:        release.StatusDeployed,
			},
			Chart: fixtureChart,
			Config: map[string]interface{}{
				"config": map[string]interface{}{"greeting": "bonjour"},
				"image":  map[string]interface{}{"tag": "1.27"},
			},
		}
		if err := c.cfg.Releases.Create(rel); err != nil {
			t.Fatal(err)
		}
		return c
	}

	// Without ReuseValues: chart defaults win.
	out, err := seed("rv-default").Template("rv-default", chartPath, RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(out, "greeting: \"hello\"") {
		t.Errorf("expected chart-default greeting=hello, got:\n%s", out)
	}

	// With ReuseValues: the release's bonjour wins (no CLI override).
	out, err = seed("rv-reuse").Template("rv-reuse", chartPath, RenderOptions{ReuseValues: true})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(out, "greeting: \"bonjour\"") {
		t.Errorf("expected reused greeting=bonjour, got:\n%s", out)
	}

	// With both ReuseValues and ResetValues: ResetValues wins (chart defaults).
	out, err = seed("rv-reset").Template("rv-reset", chartPath, RenderOptions{ReuseValues: true, ResetValues: true})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(out, "greeting: \"hello\"") {
		t.Errorf("expected reset=hello (ResetValues wins), got:\n%s", out)
	}
}

func TestTemplateReuseValuesAgainstMissingRelease(t *testing.T) {
	c := memoryClient(t)
	chartPath, _ := loadFixtureChart(t)
	out, err := c.Template("never-installed", chartPath, RenderOptions{ReuseValues: true})
	if err != nil {
		t.Fatalf("expected no error when reuse target doesn't exist: %v", err)
	}
	if !contains(out, "greeting: \"hello\"") {
		t.Errorf("expected chart-default render when no release to reuse from, got:\n%s", out)
	}
}

func TestRenderForThreeWayBranches(t *testing.T) {
	c := memoryClient(t)
	chartPath, _ := loadFixtureChart(t)

	// Default path: helm template render (no upgrade dry-run).
	out, err := c.renderForThreeWay("rel", chartPath, RenderOptions{}, false)
	if err != nil || len(out) == 0 {
		t.Fatalf("renderForThreeWay default: out=%d err=%v", len(out), err)
	}

	// Missing release with useUpgradeDryRun → install --dry-run path.
	out, err = c.renderForThreeWay("nonexistent", chartPath, RenderOptions{}, true)
	if err != nil || len(out) == 0 {
		t.Fatalf("renderForThreeWay install path: out=%d err=%v", len(out), err)
	}

	// Existing release with useUpgradeDryRun → upgrade --dry-run path.
	seedRelease(t, c, "extant", 1, seededManifestRev1)
	out, err = c.renderForThreeWay("extant", chartPath, RenderOptions{}, true)
	if err != nil || len(out) == 0 {
		t.Fatalf("renderForThreeWay upgrade path: out=%d err=%v", len(out), err)
	}
}

func TestStoredManifestByKey(t *testing.T) {
	c := memoryClient(t)
	seedRelease(t, c, "alpha", 1, `apiVersion: v1
kind: ConfigMap
metadata:
  name: cm
data:
  k: v
---
apiVersion: v1
kind: Secret
metadata:
  name: s
stringData:
  api: x
`)
	idx, err := c.storedManifestByKey("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := idx["configmap//cm"]; !ok {
		t.Errorf("expected configmap//cm key, got %v", keys(idx))
	}
	if _, ok := idx["secret//s"]; !ok {
		t.Errorf("expected secret//s key, got %v", keys(idx))
	}

	// Missing release returns an empty map.
	empty, err := c.storedManifestByKey("nope")
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Errorf("expected empty map for missing release, got %v", empty)
	}
}

func TestThreeWayMergedBadKubeconfigSurfacesError(t *testing.T) {
	// Render a real chart so the loop body fires and triggers lazy
	// dynamicLookup; point KUBECONFIG at a missing file so the lookup
	// fails deterministically in any environment.
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "missing.kubeconfig"))
	c := memoryClient(t)
	chartPath, _ := loadFixtureChart(t)
	_, _, err := c.ThreeWayMerged("rel", chartPath, RenderOptions{}, false)
	if err == nil {
		t.Fatal("expected error from missing kubeconfig dynamicLookup")
	}
}

func TestThreeWayMergedEmptyRenderSkipsDynamicLookup(t *testing.T) {
	// If the chart renders nothing, ThreeWayMerged completes without ever
	// building a dynamic client (and therefore without needing a cluster).
	dir := t.TempDir()
	if err := writeEmptyChart(dir); err != nil {
		t.Fatal(err)
	}
	c := memoryClient(t)
	live, projected, err := c.ThreeWayMerged("rel", dir, RenderOptions{}, false)
	if err != nil {
		t.Fatalf("ThreeWayMerged on empty chart: %v", err)
	}
	if len(live) != 0 || len(projected) != 0 {
		t.Errorf("expected empty streams; got live=%d projected=%d", len(live), len(projected))
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func writeEmptyChart(dir string) error {
	if err := writeFile(filepath.Join(dir, "Chart.yaml"), "apiVersion: v2\nname: empty\nversion: 0.0.1\n"); err != nil {
		return err
	}
	return nil
}

func writeFile(path, content string) error {
	return osWriteFile(path, []byte(content), 0o644)
}

// indirection so we don't drag in an extra os import in the test file body
var osWriteFile = func(path string, data []byte, perm uint32) error {
	return writeFileImpl(path, data, perm)
}

func contains(haystack []byte, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == needle {
			return true
		}
	}
	return false
}
