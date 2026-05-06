package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/szhekpisov/helm-diffyml/internal/build"
	"github.com/szhekpisov/helm-diffyml/internal/diff"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print plugin version and embedded diffyml version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "helm-diffyml: %s (commit %s, built %s)\n",
				build.Version, build.Commit, build.Date)
			fmt.Fprintf(out, "diffyml:      %s (embedded)\n", diff.VersionString())
			return nil
		},
	}
}
