package cmd

import (
	"github.com/spf13/cobra"
)

// rootCmd is the entrypoint exposed via Helm's plugin runner.
var rootCmd = &cobra.Command{
	Use:   "helm-diffyml",
	Short: "Structural YAML diff for Helm releases",
	Long: `helm-diffyml wraps the diffyml structural-diff engine to compare
rendered Kubernetes manifests across Helm operations: pending upgrades,
existing releases, specific revisions, and rollbacks.`,
	SilenceUsage:  true,
	SilenceErrors: false,
}

// Execute runs the root command. Returns the error from Cobra; cmd-specific
// exit codes (1 for differences with --exit-code, 255 for diffyml errors) are
// propagated by os.Exit calls inside the subcommand handlers.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(newVersionCmd())
	rootCmd.AddCommand(newUpgradeCmd())
	rootCmd.AddCommand(newReleaseCmd())
	rootCmd.AddCommand(newRevisionCmd())
	rootCmd.AddCommand(newRollbackCmd())
}
