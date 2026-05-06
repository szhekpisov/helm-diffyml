package cmd

import (
	"os"

	"github.com/szhekpisov/helm-diffyml/internal/helmclient"
)

// osExit is a package-level indirection on os.Exit so unit tests can
// replace it with a no-op (otherwise os.Exit aborts the test binary
// before the assertion runs). All cmd handlers exit through this var.
var osExit = os.Exit

// newClient is a package-level indirection on helmclient.New so unit
// tests can plug in a fake Renderer without touching a real cluster.
var newClient = func(namespace, kubeContext string, debug bool) (helmclient.Renderer, error) {
	return helmclient.New(namespace, kubeContext, debug)
}
