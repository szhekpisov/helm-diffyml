package cmd

import "github.com/spf13/cobra"

// buildTestRoot returns a fresh root command tree wired with production
// deps. Tests that don't care about the renderer (dry-run only) call this
// helper. Tests that need a fake renderer call buildTestRootWith(deps)
// directly with a captured Deps from withFakes.
func buildTestRoot() *cobra.Command {
	return buildRoot(defaultDeps())
}

// buildTestRootWith returns a fresh root tree wired with the supplied Deps.
func buildTestRootWith(deps Deps) *cobra.Command {
	return buildRoot(deps)
}
