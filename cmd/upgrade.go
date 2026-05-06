package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/szhekpisov/helm-diffyml/internal/diff"
	"github.com/szhekpisov/helm-diffyml/internal/helmclient"
)

func newUpgradeCmd() *cobra.Command {
	var (
		// Helm-passthrough.
		valueFiles   []string
		setVals      []string
		setStrVals   []string
		setFileVals  []string
		namespace    string
		kubeContext  string
		chartVersion string
		devel        bool

		// Plugin-meta defaults match the shell-era plugin: --neat,
		// --mask-secrets, --omit-header, -o detailed enabled by default.
		noNeat        bool
		noMaskSecrets bool
		noOmitHeader  bool
		output        string
		exitCode      bool
		dryRunPlugin  bool

		useUpgradeDryRun   bool
		noUseUpgradeDryRun bool

		threeWayMerge   bool
		noThreeWayMerge bool
	)

	cmd := &cobra.Command{
		Use:   "upgrade RELEASE CHART [-- diffyml flags]",
		Short: "Diff between current release and pending upgrade",
		Long: `Source A is the manifest currently stored for RELEASE
(helm get manifest); empty if RELEASE doesn't exist yet (initial-install
preview). Source B is a freshly rendered chart (helm template) or, with
--use-upgrade-dry-run, helm upgrade --dry-run --output yaml (with helm
install --dry-run as the fallback for missing releases).`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			release := args[0]
			chartPath := args[1]

			// Resolve the env-var default for --use-upgrade-dry-run, then
			// let CLI flags override.
			useDryRun := envFlagTrue("HELM_DIFFYML_USE_UPGRADE_DRY_RUN")
			if cmd.Flags().Changed("use-upgrade-dry-run") {
				useDryRun = useUpgradeDryRun
			}
			if noUseUpgradeDryRun {
				useDryRun = false
			}

			useThreeWay := envFlagTrue("HELM_DIFFYML_THREE_WAY_MERGE")
			if cmd.Flags().Changed("three-way-merge") {
				useThreeWay = threeWayMerge
			}
			if noThreeWayMerge {
				useThreeWay = false
			}

			client, err := helmclient.New(namespace, kubeContext, false)
			if err != nil {
				return err
			}

			renderOpts := helmclient.RenderOptions{
				Namespace:    namespace,
				ValueFiles:   valueFiles,
				Set:          setVals,
				SetString:    setStrVals,
				SetFile:      setFileVals,
				Devel:        devel,
				ChartVersion: chartVersion,
			}

			// Plugin --dry-run: print the plan without contacting the
			// cluster, mirroring the shell behaviour.
			if dryRunPlugin {
				return printUpgradePlan(cmd, release, chartPath, useDryRun, useThreeWay, renderOpts, output)
			}

			var from, to []byte
			switch {
			case useThreeWay:
				// Source A becomes the live cluster state for each
				// resource; Source B is the live state with the new chart
				// applied via three-way JSON merge patch. Composes with
				// --use-upgrade-dry-run for the modified-side render.
				from, to, err = client.ThreeWayMerged(release, chartPath, renderOpts, useDryRun)
				if err != nil {
					return fmt.Errorf("three-way merge for %s: %w", release, err)
				}
			default:
				from, err = client.GetManifest(release, 0)
				if err != nil {
					return fmt.Errorf("helm get manifest %s: %w", release, err)
				}
				switch {
				case useDryRun && len(from) == 0:
					to, err = client.InstallDryRun(release, chartPath, renderOpts)
					if err != nil {
						return fmt.Errorf("helm install --dry-run %s: %w", release, err)
					}
				case useDryRun:
					to, err = client.UpgradeDryRun(release, chartPath, renderOpts)
					if err != nil {
						return fmt.Errorf("helm upgrade --dry-run %s: %w", release, err)
					}
				default:
					to, err = client.Template(release, chartPath, renderOpts)
					if err != nil {
						return fmt.Errorf("helm template %s: %w", release, err)
					}
				}
			}

			opts := diff.DefaultOptions()
			opts.Neat = !noNeat
			opts.MaskSecrets = !noMaskSecrets
			opts.OmitHeader = !noOmitHeader
			opts.Output = output
			opts.ExitCode = exitCode
			opts.Extra = extractDiffymlExtraArgs(cmd)

			code, runErr := diff.Run(from, to, opts, cmd.OutOrStdout(), cmd.ErrOrStderr())
			// Forward verbatim exit codes (1 = diffs with --exit-code,
			// 255 = diffyml tool error).
			os.Exit(code)
			return runErr // unreachable, but satisfies the signature
		},
	}

	f := cmd.Flags()
	f.StringSliceVarP(&valueFiles, "values", "f", nil, "values file (-f, repeatable)")
	f.StringSliceVar(&setVals, "set", nil, "key=value override (repeatable)")
	f.StringSliceVar(&setStrVals, "set-string", nil, "key=value override forced to string")
	f.StringSliceVar(&setFileVals, "set-file", nil, "key=path override sourced from file")
	f.StringVarP(&namespace, "namespace", "n", "", "release namespace (defaults to $HELM_NAMESPACE)")
	f.StringVar(&kubeContext, "kube-context", "", "kubeconfig context")
	f.StringVar(&chartVersion, "version", "", "chart version constraint")
	f.BoolVar(&devel, "devel", false, "consider development chart versions")

	f.BoolVar(&noNeat, "no-neat", false, "drop diffyml's --neat default (keep Helm/ArgoCD/Flux noise)")
	f.BoolVar(&noMaskSecrets, "no-mask-secrets", false, "drop --mask-secrets default")
	f.BoolVar(&noOmitHeader, "no-omit-header", false, "drop --omit-header default")
	f.StringVarP(&output, "output", "o", "detailed", "diffyml output format (compact|brief|github|gitlab|gitea|json|json-patch|detailed)")
	f.BoolVar(&exitCode, "exit-code", false, "exit 1 if differences are found")
	f.BoolVar(&dryRunPlugin, "dry-run", false, "print the plan without contacting the cluster")

	f.BoolVar(&useUpgradeDryRun, "use-upgrade-dry-run", false, "use helm upgrade --dry-run instead of helm template for source B")
	f.BoolVar(&noUseUpgradeDryRun, "no-use-upgrade-dry-run", false, "override HELM_DIFFYML_USE_UPGRADE_DRY_RUN=true on a single call")

	f.BoolVar(&threeWayMerge, "three-way-merge", false, "diff against live cluster state (catches out-of-band drift); composes with --use-upgrade-dry-run")
	f.BoolVar(&noThreeWayMerge, "no-three-way-merge", false, "override HELM_DIFFYML_THREE_WAY_MERGE=true on a single call")

	return cmd
}

func envFlagTrue(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// extractDiffymlExtraArgs returns the args passed after a literal `--`. Cobra
// stores them on the command as ArgsLenAtDash() / args slice. Since we use
// ExactArgs(2) on positional args, anything past the dash arrives as part of
// `args`, but Cobra also exposes `cmd.Flags().Args()` for the full list.
// Simpler: walk os.Args ourselves looking for the first standalone "--".
func extractDiffymlExtraArgs(cmd *cobra.Command) []string {
	for i, a := range os.Args {
		if a == "--" {
			return os.Args[i+1:]
		}
	}
	return nil
}

func printUpgradePlan(cmd *cobra.Command, release, chartPath string, useDryRun, useThreeWay bool, opts helmclient.RenderOptions, output string) error {
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "# helm-diffyml dry-run")
	switch {
	case useThreeWay && useDryRun:
		fmt.Fprintf(out, "# mode: three-way-merge against live cluster state (target via helm upgrade --dry-run)\n")
	case useThreeWay:
		fmt.Fprintf(out, "# mode: three-way-merge against live cluster state (target via helm template)\n")
	case useDryRun:
		fmt.Fprintf(out, "# from: helm get manifest %s\n", release)
		fmt.Fprintf(out, "# to:   helm upgrade %s %s --dry-run --output yaml (or helm install --dry-run for missing releases)\n", release, chartPath)
	default:
		fmt.Fprintf(out, "# from: helm get manifest %s\n", release)
		fmt.Fprintf(out, "# to:   helm template %s %s\n", release, chartPath)
	}
	fmt.Fprintf(out, "# diff: diffyml --output %s [%s]\n", output, summariseRender(opts))
	return nil
}

func summariseRender(opts helmclient.RenderOptions) string {
	var parts []string
	for _, vf := range opts.ValueFiles {
		parts = append(parts, "-f "+vf)
	}
	for _, s := range opts.Set {
		parts = append(parts, "--set "+s)
	}
	for _, s := range opts.SetString {
		parts = append(parts, "--set-string "+s)
	}
	for _, s := range opts.SetFile {
		parts = append(parts, "--set-file "+s)
	}
	if opts.Namespace != "" {
		parts = append(parts, "-n "+opts.Namespace)
	}
	if opts.ChartVersion != "" {
		parts = append(parts, "--version "+opts.ChartVersion)
	}
	if opts.Devel {
		parts = append(parts, "--devel")
	}
	return strings.Join(parts, " ")
}
