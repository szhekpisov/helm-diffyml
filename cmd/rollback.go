package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/szhekpisov/helm-diffyml/internal/diff"
	"github.com/szhekpisov/helm-diffyml/internal/helmclient"
)

func newRollbackCmd() *cobra.Command {
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
		Use:   "rollback RELEASE [REVISION] [-- diffyml flags]",
		Short: "Preview a helm rollback",
		Long: `Diffs the current release manifest against the manifest of REVISION
(helm get manifest --revision N). When REVISION is omitted, the immediately
previous revision is used (matches the default of helm rollback).`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			release := args[0]

			var revision int
			if len(args) == 2 {
				r, err := parsePositiveInt("REVISION", args[1])
				if err != nil {
					return err
				}
				revision = r
			}

			client, err := helmclient.New(namespace, kubeContext, false)
			if err != nil {
				return err
			}

			// Resolve REVISION via helm history when the user didn't pass one.
			if revision == 0 {
				revision, err = client.PreviousRevision(release)
				if err != nil {
					return fmt.Errorf("helm history %s: %w", release, err)
				}
				if revision == 0 {
					return fmt.Errorf("%q has no previous revision to roll back to", release)
				}
			}

			if dryRunPlugin {
				out := cmd.OutOrStdout()
				fmt.Fprintln(out, "# helm-diffyml dry-run")
				fmt.Fprintf(out, "# from: helm get manifest %s\n", release)
				fmt.Fprintf(out, "# to:   helm get manifest %s --revision %d\n", release, revision)
				fmt.Fprintf(out, "# diff: diffyml --output %s\n", output)
				return nil
			}

			from, err := client.GetManifest(release, 0)
			if err != nil {
				return fmt.Errorf("helm get manifest %s: %w", release, err)
			}
			if from == nil {
				return fmt.Errorf("release %q not found", release)
			}
			to, err := client.GetManifest(release, revision)
			if err != nil {
				return fmt.Errorf("helm get manifest %s --revision %d: %w", release, revision, err)
			}
			if to == nil {
				return fmt.Errorf("release %q revision %d not found", release, revision)
			}

			opts := diff.DefaultOptions()
			opts.Neat = !noNeat
			opts.MaskSecrets = !noMaskSecrets
			opts.OmitHeader = !noOmitHeader
			opts.Output = output
			opts.ExitCode = exitCode
			opts.Extra = extractDiffymlExtraArgs(cmd)

			code, runErr := diff.Run(from, to, opts, cmd.OutOrStdout(), cmd.ErrOrStderr())
			os.Exit(code)
			return runErr
		},
	}

	addPluginMetaFlags(cmd, &noNeat, &noMaskSecrets, &noOmitHeader, &output, &exitCode, &dryRunPlugin)
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "release namespace (defaults to $HELM_NAMESPACE)")
	cmd.Flags().StringVar(&kubeContext, "kube-context", "", "kubeconfig context")
	return cmd
}
