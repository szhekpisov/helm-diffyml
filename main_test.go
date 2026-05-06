package main

import (
	"os"
	"testing"
)

// TestRunSuccess covers the happy-path of run() — the same plumbing main()
// invokes — by pointing os.Args at the version subcommand.
func TestRunSuccess(t *testing.T) {
	prev := os.Args
	t.Cleanup(func() { os.Args = prev })
	os.Args = []string{"helm-diffyml", "version"}

	if code := run(); code != 0 {
		t.Errorf("run() = %d, want 0", code)
	}
}

// TestMainInvokesRun stubs osExit and calls main(). With os.Args pointed at
// a known-good subcommand, main returns 0 via osExit; the captured value
// confirms wiring without aborting the test binary.
func TestMainInvokesRun(t *testing.T) {
	prevArgs := os.Args
	prevExit := osExit
	t.Cleanup(func() {
		os.Args = prevArgs
		osExit = prevExit
	})
	os.Args = []string{"helm-diffyml", "version"}

	captured := -1
	osExit = func(code int) { captured = code }
	main()

	if captured != 0 {
		t.Errorf("main() exited with %d, want 0", captured)
	}
}
