package diff

import (
	"strings"
	"testing"
)

const fixtureA = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  replicas: 2
`

const fixtureB = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  replicas: 3
`

// TestSafeRunDetailedShowsValueChange verifies the diff library wiring: the
// plugin defaults must propagate through cli.Run and produce a detailed
// formatted change for our `replicas` bump.
func TestSafeRunDetailedShowsValueChange(t *testing.T) {
	out, code, err := SafeRun([]byte(fixtureA), []byte(fixtureB), DefaultOptions())
	if err != nil {
		t.Fatalf("unexpected error: %v\nout=%s", err, out)
	}
	if code != 0 {
		t.Fatalf("expected exit 0 (no --exit-code), got %d\nout=%s", code, out)
	}
	if !strings.Contains(out, "spec.replicas") {
		t.Errorf("expected diff to mention spec.replicas, got:\n%s", out)
	}
	if !strings.Contains(out, "2") || !strings.Contains(out, "3") {
		t.Errorf("expected diff to show 2 → 3, got:\n%s", out)
	}
}

// TestSafeRunExitCode flips --exit-code on and confirms the differences-found
// path returns 1.
func TestSafeRunExitCode(t *testing.T) {
	opts := DefaultOptions()
	opts.ExitCode = true
	_, code, err := SafeRun([]byte(fixtureA), []byte(fixtureB), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 1 {
		t.Errorf("expected exit 1 with --exit-code on differences, got %d", code)
	}
}

// TestSafeRunIdentical confirms that identical inputs return 0 even with
// --exit-code enabled.
func TestSafeRunIdentical(t *testing.T) {
	opts := DefaultOptions()
	opts.ExitCode = true
	_, code, err := SafeRun([]byte(fixtureA), []byte(fixtureA), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Errorf("expected exit 0 on identical inputs, got %d", code)
	}
}

// TestSafeRunNilFromContent simulates the upgrade-against-missing-release
// scenario: helm get manifest returned nothing, so the "from" side is nil.
// The diff library must treat that as an empty source rather than try to
// validate the synthetic <helm:from> sentinel as a real file path.
func TestSafeRunNilFromContent(t *testing.T) {
	out, code, err := SafeRun(nil, []byte(fixtureB), DefaultOptions())
	if err != nil {
		t.Fatalf("nil from should not error: %v\nout=%s", err, out)
	}
	if code != 0 {
		t.Errorf("expected exit 0 (no --exit-code), got %d\nout=%s", code, out)
	}
	// Pure addition — output should mention the resource we added.
	if !strings.Contains(out, "Deployment") || !strings.Contains(out, "web") {
		t.Errorf("expected pure-addition diff to mention the added Deployment, got:\n%s", out)
	}
}

// TestSafeRunBothNil — purely a regression test for the panic-y path. With
// nothing on either side, there is nothing to diff, but the call must
// neither panic nor try to read sentinel files.
func TestSafeRunBothNil(t *testing.T) {
	_, code, err := SafeRun(nil, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("both-nil should not error: %v", err)
	}
	if code != 0 {
		t.Errorf("expected exit 0 for empty-vs-empty, got %d", code)
	}
}
