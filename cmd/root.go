package cmd

import (
	"github.com/spf13/cobra"
)

// buildRoot returns a fully-wired root command tree. Production code calls
// it with defaultDeps(); tests can pass their own Deps with fakes.
func buildRoot(deps Deps) *cobra.Command {
	root := &cobra.Command{
		Use:   "helm-diffyml",
		Short: "Structural YAML diff for Helm releases",
		Long: `helm-diffyml wraps the diffyml structural-diff engine to compare
rendered Kubernetes manifests across Helm operations: pending upgrades,
existing releases, specific revisions, and rollbacks.`,
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.AddCommand(newVersionCmd())
	root.AddCommand(newUpgradeCmd(deps))
	root.AddCommand(newReleaseCmd(deps))
	root.AddCommand(newRevisionCmd(deps))
	root.AddCommand(newRollbackCmd(deps))
	return root
}

// Execute runs the production root command. Subcommand handlers may call
// deps.Exit (default os.Exit) to propagate diff exit codes.
func Execute() error {
	return buildRoot(defaultDeps()).Execute()
}
