package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/szhekpisov/helm-diffyml/internal/diff"
)

func newReleaseCmd() *cobra.Command {
	var (
		namespace   string
		kubeContext string

		noNeat        bool
		noMaskSecrets bool
		noOmitHeader  bool
		output        string
		exitCode      bool
		dryRunPlugin  bool
	)

	cmd := &cobra.Command{
		Use:   "release REL_A REL_B [-- diffyml flags]",
		Short: "Diff between two live releases",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			relA, relB := args[0], args[1]

			if dryRunPlugin {
				out := cmd.OutOrStdout()
				fmt.Fprintln(out, "# helm-diffyml dry-run")
				fmt.Fprintf(out, "# from: helm get manifest %s\n", relA)
				fmt.Fprintf(out, "# to:   helm get manifest %s\n", relB)
				fmt.Fprintf(out, "# diff: diffyml --output %s\n", output)
				return nil
			}

			client, err := newClient(namespace, kubeContext, false)
			if err != nil {
				return err
			}

			from, err := client.GetManifest(relA, 0)
			if err != nil {
				return fmt.Errorf("helm get manifest %s: %w", relA, err)
			}
			if from == nil {
				return fmt.Errorf("release %q not found", relA)
			}
			to, err := client.GetManifest(relB, 0)
			if err != nil {
				return fmt.Errorf("helm get manifest %s: %w", relB, err)
			}
			if to == nil {
				return fmt.Errorf("release %q not found", relB)
			}

			opts := diff.DefaultOptions()
			opts.Neat = !noNeat
			opts.MaskSecrets = !noMaskSecrets
			opts.OmitHeader = !noOmitHeader
			opts.Output = output
			opts.ExitCode = exitCode
			opts.Extra = extractDiffymlExtraArgs(cmd)

			code, runErr := diff.Run(from, to, opts, cmd.OutOrStdout(), cmd.ErrOrStderr())
			osExit(code)
			return runErr
		},
	}

	addPluginMetaFlags(cmd, &noNeat, &noMaskSecrets, &noOmitHeader, &output, &exitCode, &dryRunPlugin)
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "namespace shared by both releases (defaults to $HELM_NAMESPACE)")
	cmd.Flags().StringVar(&kubeContext, "kube-context", "", "kubeconfig context")
	return cmd
}

// addPluginMetaFlags wires the shared plugin-meta flags onto a subcommand.
// Avoids per-subcommand boilerplate.
func addPluginMetaFlags(cmd *cobra.Command,
	noNeat, noMaskSecrets, noOmitHeader *bool,
	output *string,
	exitCode, dryRunPlugin *bool,
) {
	f := cmd.Flags()
	f.BoolVar(noNeat, "no-neat", false, "drop diffyml's --neat default")
	f.BoolVar(noMaskSecrets, "no-mask-secrets", false, "drop --mask-secrets default")
	f.BoolVar(noOmitHeader, "no-omit-header", false, "drop --omit-header default")
	f.StringVarP(output, "output", "o", "detailed", "diffyml output format")
	f.BoolVar(exitCode, "exit-code", false, "exit 1 if differences are found")
	f.BoolVar(dryRunPlugin, "dry-run", false, "print the plan without contacting the cluster")
}
