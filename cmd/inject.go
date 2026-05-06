package cmd

import (
	"os"

	"github.com/szhekpisov/helm-diffyml/internal/helmclient"
)

// Deps bundles the external integrations every subcommand needs. Production
// code uses defaultDeps; unit tests construct a Deps with fakes.
//
// Passing this through buildRoot()/newXCmd(deps) avoids the package-level
// var test seam, which proved unreliable under Go's coverage-instrumentation
// rewriting on Linux/amd64 — the closures inside RunE were observed to bind
// the package var at construction time, ignoring later reassignments.
type Deps struct {
	// NewClient builds a Renderer (helmclient.Client in production, a fake
	// in tests). The Renderer talks to Helm and the cluster.
	NewClient func(namespace, kubeContext string, debug bool) (helmclient.Renderer, error)
	// Exit is the os.Exit indirection so tests can capture the propagated
	// exit code without aborting the test binary.
	Exit func(code int)
}

// defaultDeps wires Deps to the real helmclient.New + os.Exit.
func defaultDeps() Deps {
	return Deps{
		NewClient: func(namespace, kubeContext string, debug bool) (helmclient.Renderer, error) {
			return helmclient.New(namespace, kubeContext, debug)
		},
		Exit: os.Exit,
	}
}
