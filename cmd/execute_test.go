package cmd

import (
	"os"
	"testing"
)

// TestExecute exercises the package-level Execute() entry point. It uses
// the production deps (real helmclient.New + real os.Exit), but with
// os.Args pointed at the harmless `version` subcommand it never reaches
// the cluster paths.
func TestExecute(t *testing.T) {
	prevArgs := os.Args
	t.Cleanup(func() { os.Args = prevArgs })
	os.Args = []string{"helm-diffyml", "version"}

	if err := Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
}
