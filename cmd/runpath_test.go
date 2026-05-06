package cmd

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/szhekpisov/helm-diffyml/internal/helmclient"
)

const (
	yamlReplicas2 = "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\nspec:\n  replicas: 2\n"
	yamlReplicas3 = "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\nspec:\n  replicas: 3\n"
)

// fakeRenderer captures calls and returns canned bytes. Each method's call
// can be inspected via the corresponding *Calls counter.
type fakeRenderer struct {
	getManifest    []byte
	getManifestErr error

	template          []byte
	templateCalls     int
	upgradeDryRun     []byte
	upgradeDryRunCalls int
	installDryRun     []byte
	installDryRunCalls int

	threeWayLive       []byte
	threeWayProjected  []byte
	threeWayCalls      int
	threeWayUseDryRun  bool

	previousRevision    int
	previousRevisionErr error
}

func (f *fakeRenderer) GetManifest(string, int) ([]byte, error) {
	return f.getManifest, f.getManifestErr
}
func (f *fakeRenderer) PreviousRevision(string) (int, error) {
	return f.previousRevision, f.previousRevisionErr
}
func (f *fakeRenderer) Template(string, string, helmclient.RenderOptions) ([]byte, error) {
	f.templateCalls++
	return f.template, nil
}
func (f *fakeRenderer) UpgradeDryRun(string, string, helmclient.RenderOptions) ([]byte, error) {
	f.upgradeDryRunCalls++
	return f.upgradeDryRun, nil
}
func (f *fakeRenderer) InstallDryRun(string, string, helmclient.RenderOptions) ([]byte, error) {
	f.installDryRunCalls++
	return f.installDryRun, nil
}
func (f *fakeRenderer) ThreeWayMerged(_, _ string, _ helmclient.RenderOptions, useDryRun bool) ([]byte, []byte, error) {
	f.threeWayCalls++
	f.threeWayUseDryRun = useDryRun
	return f.threeWayLive, f.threeWayProjected, nil
}

// withFakes returns a Deps wired to the supplied fake renderer plus a
// pointer that captures any exit code propagated by deps.Exit.
func withFakes(_ *testing.T, fr *fakeRenderer) (Deps, *int) {
	captured := -1
	deps := Deps{
		NewClient: func(string, string, bool) (helmclient.Renderer, error) { return fr, nil },
		Exit:      func(code int) { captured = code },
	}
	return deps, &captured
}

// runRunPath builds a fresh root tree from the supplied Deps, executes it
// with the given args, and returns captured stdout / stderr / Execute error.
func runRunPath(_ *testing.T, deps Deps, args ...string) (string, string, error) {
	root := buildTestRootWith(deps)
	stdout := newBuf()
	stderr := newBuf()
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestUpgradeRunPathDefaultTemplate(t *testing.T) {
	fr := &fakeRenderer{
		getManifest: []byte(yamlReplicas2),
		template:    []byte(yamlReplicas3),
	}
	deps, exit := withFakes(t, fr)

	out, errOut, err := runRunPath(t, deps, "upgrade", "my-rel", "/tmp/chart")
	if err != nil {
		t.Fatalf("RunE returned error: %v\nstderr=%s", err, errOut)
	}
	if fr.templateCalls != 1 {
		t.Errorf("expected exactly one Template call, got %d", fr.templateCalls)
	}
	if !strings.Contains(out, "spec.replicas") || !strings.Contains(out, "2") || !strings.Contains(out, "3") {
		t.Errorf("expected detailed diff to mention replicas 2→3, got:\n%s", out)
	}
	if *exit != 0 {
		t.Errorf("expected exit 0 on differences without --exit-code, got %d", *exit)
	}
}

func TestUpgradeRunPathExitCodeOnDifferences(t *testing.T) {
	fr := &fakeRenderer{
		getManifest: []byte(yamlReplicas2),
		template:    []byte(yamlReplicas3),
	}
	deps, exit := withFakes(t, fr)

	_, _, err := runRunPath(t, deps, "upgrade", "my-rel", "/tmp/chart", "--exit-code")
	if err != nil {
		t.Fatalf("RunE returned error: %v", err)
	}
	if *exit != 1 {
		t.Errorf("expected exit 1 with --exit-code on differences, got %d", *exit)
	}
}

func TestUpgradeRunPathInstallDryRunForMissingRelease(t *testing.T) {
	fr := &fakeRenderer{
		// Empty manifest signals "release not found" → install --dry-run path.
		getManifest:    nil,
		installDryRun:  []byte(yamlReplicas3),
	}
	deps, _ := withFakes(t, fr)

	_, _, err := runRunPath(t, deps, "upgrade", "my-rel", "/tmp/chart", "--use-upgrade-dry-run")
	if err != nil {
		t.Fatalf("RunE returned error: %v", err)
	}
	if fr.installDryRunCalls != 1 || fr.upgradeDryRunCalls != 0 || fr.templateCalls != 0 {
		t.Errorf("expected install-dry-run to be the only render call; got install=%d upgrade=%d template=%d",
			fr.installDryRunCalls, fr.upgradeDryRunCalls, fr.templateCalls)
	}
}

func TestUpgradeRunPathUpgradeDryRunForExistingRelease(t *testing.T) {
	fr := &fakeRenderer{
		getManifest:   []byte(yamlReplicas2),
		upgradeDryRun: []byte(yamlReplicas3),
	}
	deps, _ := withFakes(t, fr)

	_, _, err := runRunPath(t, deps, "upgrade", "my-rel", "/tmp/chart", "--use-upgrade-dry-run")
	if err != nil {
		t.Fatalf("RunE returned error: %v", err)
	}
	if fr.upgradeDryRunCalls != 1 || fr.templateCalls != 0 {
		t.Errorf("expected upgrade-dry-run path; got upgrade=%d template=%d",
			fr.upgradeDryRunCalls, fr.templateCalls)
	}
}

func TestUpgradeRunPathThreeWayDispatch(t *testing.T) {
	fr := &fakeRenderer{
		threeWayLive:      []byte(yamlReplicas2),
		threeWayProjected: []byte(yamlReplicas3),
	}
	deps, _ := withFakes(t, fr)

	out, _, err := runRunPath(t, deps, "upgrade", "my-rel", "/tmp/chart", "--three-way-merge")
	if err != nil {
		t.Fatalf("RunE returned error: %v", err)
	}
	if fr.threeWayCalls != 1 || fr.templateCalls != 0 {
		t.Errorf("expected three-way path only; got threeWay=%d template=%d",
			fr.threeWayCalls, fr.templateCalls)
	}
	if fr.threeWayUseDryRun {
		t.Errorf("--use-upgrade-dry-run should NOT be passed without the flag")
	}
	if !strings.Contains(out, "replicas") {
		t.Errorf("expected diff to mention replicas, got:\n%s", out)
	}
}

func TestUpgradeRunPathThreeWayWithUseUpgradeDryRun(t *testing.T) {
	fr := &fakeRenderer{
		threeWayLive:      []byte(yamlReplicas2),
		threeWayProjected: []byte(yamlReplicas3),
	}
	deps, _ := withFakes(t, fr)

	_, _, err := runRunPath(t, deps, "upgrade", "my-rel", "/tmp/chart", "--three-way-merge", "--use-upgrade-dry-run")
	if err != nil {
		t.Fatalf("RunE returned error: %v", err)
	}
	if !fr.threeWayUseDryRun {
		t.Error("--three-way-merge + --use-upgrade-dry-run should propagate useDryRun=true to ThreeWayMerged")
	}
}

func TestUpgradeRunPathGetManifestError(t *testing.T) {
	fr := &fakeRenderer{getManifestErr: errors.New("boom")}
	deps, _ := withFakes(t, fr)

	_, _, err := runRunPath(t, deps, "upgrade", "my-rel", "/tmp/chart")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected wrapped 'boom' error, got %v", err)
	}
}

func TestUpgradeRunPathThreeWayError(t *testing.T) {
	er := &errRenderer{}
	deps := Deps{
		NewClient: func(string, string, bool) (helmclient.Renderer, error) { return er, nil },
		Exit:      func(int) {},
	}

	_, _, err := runRunPath(t, deps, "upgrade", "my-rel", "/tmp/chart", "--three-way-merge")
	if err == nil || !strings.Contains(err.Error(), "three-way merge") {
		t.Fatalf("expected three-way-wrapped error, got %v", err)
	}
}

type errRenderer struct {
	fakeRenderer
}

func (e *errRenderer) ThreeWayMerged(string, string, helmclient.RenderOptions, bool) ([]byte, []byte, error) {
	return nil, nil, errors.New("3way exploded")
}

func TestReleaseRunPath(t *testing.T) {
	fr := &fakeRenderer{getManifest: []byte(yamlReplicas2)}
	deps, exit := withFakes(t, fr)

	_, _, err := runRunPath(t, deps, "release", "rel-a", "rel-b")
	if err != nil {
		t.Fatalf("release RunE returned error: %v", err)
	}
	if *exit != 0 {
		t.Errorf("expected exit 0 on identical inputs, got %d", *exit)
	}
}

func TestReleaseMissingRelease(t *testing.T) {
	fr := &fakeRenderer{getManifest: nil}
	deps, _ := withFakes(t, fr)

	_, _, err := runRunPath(t, deps, "release", "rel-a", "rel-b")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got %v", err)
	}
}

func TestRevisionRunPath(t *testing.T) {
	fr := &fakeRenderer{getManifest: []byte(yamlReplicas2)}
	deps, _ := withFakes(t, fr)
	_, _, err := runRunPath(t, deps, "revision", "my-rel", "1", "2")
	if err != nil {
		t.Fatalf("revision RunE returned error: %v", err)
	}
}

func TestRevisionMissingRelease(t *testing.T) {
	fr := &fakeRenderer{getManifest: nil}
	deps, _ := withFakes(t, fr)
	_, _, err := runRunPath(t, deps, "revision", "my-rel", "1", "2")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got %v", err)
	}
}

func TestRollbackImplicitRevisionResolved(t *testing.T) {
	fr := &fakeRenderer{
		previousRevision: 4,
		getManifest:      []byte(yamlReplicas2),
	}
	deps, _ := withFakes(t, fr)

	_, _, err := runRunPath(t, deps, "rollback", "my-rel")
	if err != nil {
		t.Fatalf("rollback RunE returned error: %v", err)
	}
}

func TestRollbackImplicitNoPreviousRevision(t *testing.T) {
	fr := &fakeRenderer{previousRevision: 0}
	deps, _ := withFakes(t, fr)

	_, _, err := runRunPath(t, deps, "rollback", "my-rel")
	if err == nil || !strings.Contains(err.Error(), "no previous revision") {
		t.Fatalf("expected no-previous error, got %v", err)
	}
}

func TestRollbackHistoryFails(t *testing.T) {
	fr := &fakeRenderer{previousRevisionErr: errors.New("history kaput")}
	deps, _ := withFakes(t, fr)

	_, _, err := runRunPath(t, deps, "rollback", "my-rel")
	if err == nil || !strings.Contains(err.Error(), "history kaput") {
		t.Fatalf("expected wrapped history error, got %v", err)
	}
}

func TestRollbackExplicitRevision(t *testing.T) {
	fr := &fakeRenderer{getManifest: []byte(yamlReplicas2)}
	deps, _ := withFakes(t, fr)

	_, _, err := runRunPath(t, deps, "rollback", "my-rel", "3")
	if err != nil {
		t.Fatalf("rollback RunE returned error: %v", err)
	}
}

func TestRollbackMissingRelease(t *testing.T) {
	fr := &fakeRenderer{getManifest: nil, previousRevision: 1}
	deps, _ := withFakes(t, fr)

	_, _, err := runRunPath(t, deps, "rollback", "my-rel", "1")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got %v", err)
	}
}

func TestExtractDiffymlExtraArgs(t *testing.T) {
	prev := os.Args
	t.Cleanup(func() { os.Args = prev })

	// No -- token: returns nil.
	os.Args = []string{"helm-diffyml", "upgrade", "rel", "chart"}
	if got := extractDiffymlExtraArgs(nil); got != nil {
		t.Errorf("expected nil when no -- token, got %v", got)
	}

	// With --: returns the suffix.
	os.Args = []string{"helm-diffyml", "upgrade", "rel", "chart", "--", "--ignore-api-version", "--filter", "Deployment/*"}
	got := extractDiffymlExtraArgs(nil)
	want := []string{"--ignore-api-version", "--filter", "Deployment/*"}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

func TestSummariseRenderAllFlags(t *testing.T) {
	got := summariseRender(helmclient.RenderOptions{
		ValueFiles:   []string{"v.yaml"},
		Set:          []string{"a=1"},
		SetString:    []string{"b=hi"},
		SetFile:      []string{"c=/tmp/file"},
		Namespace:    "prod",
		ChartVersion: "1.2.3",
		Devel:        true,
	})
	for _, want := range []string{
		"-f v.yaml",
		"--set a=1",
		"--set-string b=hi",
		"--set-file c=/tmp/file",
		"-n prod",
		"--version 1.2.3",
		"--devel",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected summary to contain %q, got: %s", want, got)
		}
	}
}
