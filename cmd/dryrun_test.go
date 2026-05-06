package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestUpgradeDryRunDefaults(t *testing.T) {
	out, _, err := runSubcommand(t, "upgrade", "my-rel", "/tmp/chart", "-f", "/tmp/v.yaml", "--dry-run")
	if err != nil {
		t.Fatalf("dry-run returned error: %v\nout=%q", err, out)
	}
	wantContains := []string{
		"# helm-diffyml dry-run",
		"# from: helm get manifest my-rel",
		"# to:   helm template my-rel /tmp/chart",
		"--output detailed",
		"-f /tmp/v.yaml",
	}
	for _, s := range wantContains {
		if !strings.Contains(out, s) {
			t.Errorf("expected output to contain %q, got:\n%s", s, out)
		}
	}
	// Defaults imply --neat/--mask-secrets/--omit-header are baked into
	// internal/diff. The dry-run summary shows --output but not those — the
	// shell-era assertion covered that they are *applied*, which is now an
	// internal/diff package concern.
}

func TestUpgradeDryRunUseUpgradeDryRunFlag(t *testing.T) {
	out, _, err := runSubcommand(t, "upgrade", "my-rel", "/tmp/chart", "--use-upgrade-dry-run", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "helm upgrade my-rel /tmp/chart --dry-run --output yaml") {
		t.Errorf("expected source B to be helm upgrade --dry-run, got:\n%s", out)
	}
	if strings.Contains(out, "helm template") {
		t.Errorf("did not expect helm template in source B with --use-upgrade-dry-run, got:\n%s", out)
	}
}

func TestUpgradeDryRunEnvFlipsDefault(t *testing.T) {
	t.Setenv("HELM_DIFFYML_USE_UPGRADE_DRY_RUN", "true")
	out, _, err := runSubcommand(t, "upgrade", "my-rel", "/tmp/chart", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "helm upgrade my-rel /tmp/chart --dry-run --output yaml") {
		t.Errorf("env-var should enable --use-upgrade-dry-run, got:\n%s", out)
	}
}

func TestUpgradeDryRunNoUseOverridesEnv(t *testing.T) {
	t.Setenv("HELM_DIFFYML_USE_UPGRADE_DRY_RUN", "true")
	out, _, err := runSubcommand(t, "upgrade", "my-rel", "/tmp/chart", "--no-use-upgrade-dry-run", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "helm template my-rel /tmp/chart") {
		t.Errorf("--no-use-upgrade-dry-run should restore helm template, got:\n%s", out)
	}
}

func TestReleaseDryRun(t *testing.T) {
	out, _, err := runSubcommand(t, "release", "rel-a", "rel-b", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		"# helm-diffyml dry-run",
		"# from: helm get manifest rel-a",
		"# to:   helm get manifest rel-b",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("expected %q in:\n%s", s, out)
		}
	}
}

func TestRevisionDryRun(t *testing.T) {
	out, _, err := runSubcommand(t, "revision", "my-rel", "1", "2", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		"# from: helm get manifest my-rel --revision 1",
		"# to:   helm get manifest my-rel --revision 2",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("expected %q in:\n%s", s, out)
		}
	}
}

func TestRevisionRejectsNonInteger(t *testing.T) {
	_, errOut, err := runSubcommand(t, "revision", "my-rel", "abc", "2")
	if err == nil {
		t.Fatal("expected error for non-integer revision")
	}
	if !strings.Contains(err.Error()+errOut, "must be a positive integer") {
		t.Errorf("expected positive-integer error, got: err=%v stderr=%q", err, errOut)
	}
}

func TestRollbackExplicitRevisionDryRun(t *testing.T) {
	out, _, err := runSubcommand(t, "rollback", "my-rel", "3", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		"# from: helm get manifest my-rel",
		"# to:   helm get manifest my-rel --revision 3",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("expected %q in:\n%s", s, out)
		}
	}
}

func TestVersionPrintsBothLines(t *testing.T) {
	out, _, err := runSubcommand(t, "version")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"helm-diffyml:", "diffyml:"} {
		if !strings.Contains(out, s) {
			t.Errorf("expected %q in version output:\n%s", s, out)
		}
	}
}

// runSubcommand invokes the root cobra command with the given args, capturing
// stdout and stderr. It avoids os.Exit — for that, individual subcommand
// handlers gate the os.Exit on a *Run* path, not the dry-run path that the
// tests exercise here.
func runSubcommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := buildTestRoot()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}
