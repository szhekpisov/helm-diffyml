package cmd

import (
	"bytes"
	"os"
	"testing"
)

// TestExecute exercises the package-level Execute() entry point against the
// process's rootCmd, so the lines in cmd/root.go count toward coverage.
func TestExecute(t *testing.T) {
	prevArgs := os.Args
	t.Cleanup(func() { os.Args = prevArgs })
	os.Args = []string{"helm-diffyml", "version"}

	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)

	if err := Execute(); err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr.String())
	}
}
