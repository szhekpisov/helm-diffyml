package cmd

import "github.com/spf13/cobra"

// buildTestRoot returns a fresh root command tree for each test. We can't
// reuse the package-level rootCmd because cobra mutates it (visited flags,
// args, etc.) and tests run in parallel.
func buildTestRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "helm-diffyml",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newVersionCmd())
	root.AddCommand(newUpgradeCmd())
	root.AddCommand(newReleaseCmd())
	root.AddCommand(newRevisionCmd())
	root.AddCommand(newRollbackCmd())
	return root
}
