package cmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/szhekpisov/helm-diffyml/internal/diff"
)

func newRevisionCmd() *cobra.Command {
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
		Use:   "revision RELEASE REV_A REV_B [-- diffyml flags]",
		Short: "Diff between two revisions of one release",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			release := args[0]
			revA, err := parsePositiveInt("REV_A", args[1])
			if err != nil {
				return err
			}
			revB, err := parsePositiveInt("REV_B", args[2])
			if err != nil {
				return err
			}

			if dryRunPlugin {
				out := cmd.OutOrStdout()
				fmt.Fprintln(out, "# helm-diffyml dry-run")
				fmt.Fprintf(out, "# from: helm get manifest %s --revision %d\n", release, revA)
				fmt.Fprintf(out, "# to:   helm get manifest %s --revision %d\n", release, revB)
				fmt.Fprintf(out, "# diff: diffyml --output %s\n", output)
				return nil
			}

			client, err := newClient(namespace, kubeContext, false)
			if err != nil {
				return err
			}

			from, err := client.GetManifest(release, revA)
			if err != nil {
				return fmt.Errorf("helm get manifest %s --revision %d: %w", release, revA, err)
			}
			if from == nil {
				return fmt.Errorf("release %q revision %d not found", release, revA)
			}
			to, err := client.GetManifest(release, revB)
			if err != nil {
				return fmt.Errorf("helm get manifest %s --revision %d: %w", release, revB, err)
			}
			if to == nil {
				return fmt.Errorf("release %q revision %d not found", release, revB)
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
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "release namespace (defaults to $HELM_NAMESPACE)")
	cmd.Flags().StringVar(&kubeContext, "kube-context", "", "kubeconfig context")
	return cmd
}

func parsePositiveInt(name, raw string) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q", name, raw)
	}
	return n, nil
}
