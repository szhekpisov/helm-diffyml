// Package helmclient wraps the relevant subset of helm.sh/helm/v3 actions
// behind a small, plugin-friendly API. Every method here matches one of the
// shell-era inner `helm` invocations.
package helmclient

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
)

// Client encapsulates helm's action configuration and the EnvSettings that
// describe how this binary discovered the cluster.
type Client struct {
	settings *cli.EnvSettings
	cfg      *action.Configuration
}

// New builds a Client. namespace overrides $HELM_NAMESPACE when non-empty.
// kubeContext overrides $KUBE_CONTEXT when non-empty. debug forwards helm's
// internal action logs to stderr.
func New(namespace, kubeContext string, debug bool) (*Client, error) {
	settings := cli.New()
	if namespace != "" {
		settings.SetNamespace(namespace)
	}
	if kubeContext != "" {
		settings.KubeContext = kubeContext
	}

	debugLog := func(format string, v ...interface{}) {}
	if debug {
		debugLog = func(format string, v ...interface{}) {
			log.Printf(format, v...)
		}
	}

	cfg := new(action.Configuration)
	if err := cfg.Init(
		settings.RESTClientGetter(),
		settings.Namespace(),
		os.Getenv("HELM_DRIVER"),
		debugLog,
	); err != nil {
		return nil, fmt.Errorf("init helm action config: %w", err)
	}
	return &Client{settings: settings, cfg: cfg}, nil
}

// Settings exposes the underlying cli.EnvSettings (caller may need
// settings.Namespace() etc).
func (c *Client) Settings() *cli.EnvSettings { return c.settings }

// NewWithConfig is the test seam: build a Client from a pre-constructed
// action.Configuration (e.g. wired to driver.NewMemory + a fake KubeClient).
// Production code should use New() — this constructor is for unit tests
// that want to drive helmclient methods without touching a real cluster.
func NewWithConfig(settings *cli.EnvSettings, cfg *action.Configuration) *Client {
	return &Client{settings: settings, cfg: cfg}
}

// GetManifest fetches the stored manifest of name. revision==0 ⇒ latest.
// Returns (nil, nil) when the release does not exist (so callers can treat
// "not found" as an empty Source A in the upgrade subcommand).
func (c *Client) GetManifest(name string, revision int) ([]byte, error) {
	get := action.NewGet(c.cfg)
	get.Version = revision
	rel, err := get.Run(name)
	if err != nil {
		if errors.Is(err, driver.ErrReleaseNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return []byte(rel.Manifest), nil
}

// History returns revisions sorted ascending. max==0 ⇒ helm default (256).
func (c *Client) History(name string, max int) ([]*release.Release, error) {
	h := action.NewHistory(c.cfg)
	if max > 0 {
		h.Max = max
	}
	return h.Run(name)
}

// PreviousRevision returns the revision number immediately before the
// newest one, or 0 if no such revision exists. Helm rollbacks default to
// this value when invoked without an explicit revision.
func (c *Client) PreviousRevision(name string) (int, error) {
	hist, err := c.History(name, 0)
	if err != nil {
		return 0, err
	}
	if len(hist) < 2 {
		return 0, nil
	}
	// action.NewHistory returns oldest→newest. The previous revision is
	// the second-to-last entry.
	return hist[len(hist)-2].Version, nil
}

// Template renders the chart locally (the equivalent of `helm template`).
// No cluster contact, no `lookup`. Honours opts.ReuseValues by merging the
// existing release's stored values into the CLI-supplied values (CLI wins
// on key conflict). opts.ResetValues is a no-op for Template — the helm
// template path always starts from chart defaults + CLI overrides anyway.
func (c *Client) Template(releaseName, chartPath string, opts RenderOptions) ([]byte, error) {
	chart, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("load chart: %w", err)
	}
	vals, err := opts.merged(c.settings)
	if err != nil {
		return nil, err
	}
	if opts.ReuseValues && !opts.ResetValues {
		vals, err = c.mergeReuseValues(releaseName, vals)
		if err != nil {
			return nil, err
		}
	}

	inst := action.NewInstall(c.cfg)
	inst.DryRun = true
	inst.Replace = true
	inst.ClientOnly = true
	inst.ReleaseName = releaseName
	inst.Namespace = c.namespaceFor(opts)
	inst.PostRenderer = nil // template path doesn't use post-renderers
	inst.Devel = opts.Devel
	inst.Version = opts.ChartVersion
	inst.IncludeCRDs = true
	inst.Verify = false

	rel, err := inst.Run(chart, vals)
	if err != nil {
		return nil, err
	}
	return []byte(mergeHooks(rel.Manifest, rel.Hooks, opts.IncludeHooks, opts.IncludeTests)), nil
}

// mergeHooks appends non-test (and optionally test) hook manifests to the
// rendered manifest. helm template normally returns rel.Manifest with the
// non-hook resources only; rel.Hooks holds each hook's rendered YAML
// alongside its event metadata.
func mergeHooks(manifest string, hooks []*release.Hook, includeNonTest, includeTests bool) string {
	if !includeNonTest && !includeTests {
		return manifest
	}
	var b strings.Builder
	b.WriteString(manifest)
	for _, h := range hooks {
		isTest := false
		for _, e := range h.Events {
			if e == release.HookTest {
				isTest = true
				break
			}
		}
		if (isTest && !includeTests) || (!isTest && !includeNonTest) {
			continue
		}
		if !strings.HasSuffix(b.String(), "\n") {
			b.WriteString("\n")
		}
		b.WriteString("---\n")
		b.WriteString(h.Manifest)
	}
	return b.String()
}

// mergeReuseValues fetches the deployed release's values and merges the
// caller's CLI-derived values on top (CLI wins on conflict). If the release
// doesn't exist yet, returns the CLI values unchanged.
func (c *Client) mergeReuseValues(releaseName string, cliVals map[string]any) (map[string]any, error) {
	getter := action.NewGetValues(c.cfg)
	getter.AllValues = true
	existing, err := getter.Run(releaseName)
	if err != nil {
		if errors.Is(err, driver.ErrReleaseNotFound) {
			return cliVals, nil
		}
		return nil, fmt.Errorf("get release values for --reuse-values: %w", err)
	}
	if len(existing) == 0 {
		return cliVals, nil
	}
	// CoalesceTables merges src into dst, dst wins. CLI is dst so it
	// overrides the previous release's values exactly like helm does.
	return chartutil.CoalesceTables(cliVals, existing), nil
}

// UpgradeDryRun runs `helm upgrade --dry-run` against the live cluster so
// that `lookup`, post-renderers, and live state participate in the render.
func (c *Client) UpgradeDryRun(releaseName, chartPath string, opts RenderOptions) ([]byte, error) {
	chart, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("load chart: %w", err)
	}
	vals, err := opts.merged(c.settings)
	if err != nil {
		return nil, err
	}

	up := action.NewUpgrade(c.cfg)
	up.DryRun = true
	up.Namespace = c.namespaceFor(opts)
	up.Devel = opts.Devel
	up.Version = opts.ChartVersion
	up.ResetValues = opts.ResetValues
	up.ReuseValues = opts.ReuseValues

	rel, err := up.Run(releaseName, chart, vals)
	if err != nil {
		return nil, err
	}
	return []byte(mergeHooks(rel.Manifest, rel.Hooks, opts.IncludeHooks, opts.IncludeTests)), nil
}

// InstallDryRun runs `helm install --dry-run` against the live cluster.
// Used as the source-B for upgrade --use-upgrade-dry-run when the release
// does not yet exist.
func (c *Client) InstallDryRun(releaseName, chartPath string, opts RenderOptions) ([]byte, error) {
	chart, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("load chart: %w", err)
	}
	vals, err := opts.merged(c.settings)
	if err != nil {
		return nil, err
	}

	inst := action.NewInstall(c.cfg)
	inst.DryRun = true
	inst.ReleaseName = releaseName
	inst.Namespace = c.namespaceFor(opts)
	inst.Devel = opts.Devel
	inst.Version = opts.ChartVersion

	rel, err := inst.Run(chart, vals)
	if err != nil {
		return nil, err
	}
	return []byte(mergeHooks(rel.Manifest, rel.Hooks, opts.IncludeHooks, opts.IncludeTests)), nil
}

func (c *Client) namespaceFor(opts RenderOptions) string {
	if opts.Namespace != "" {
		return opts.Namespace
	}
	return c.settings.Namespace()
}

// RenderOptions carries the helm-passthrough flags applicable to source-B
// rendering. Empty fields take their helm defaults.
type RenderOptions struct {
	// Namespace overrides the client-level namespace for this render only.
	Namespace string
	// ReuseValues, when true, merges the existing release's stored values
	// with the CLI-supplied values (CLI wins on conflict). Mirrors
	// `helm upgrade --reuse-values`.
	ReuseValues bool
	// ResetValues, when true, ignores the existing release's stored values
	// and starts from chart defaults + CLI overrides. Mirrors
	// `helm upgrade --reset-values`. If both are set, ResetValues wins
	// (matching helm-diff's behaviour).
	ResetValues bool
	// IncludeHooks, when true, appends non-test hook resources to the
	// rendered manifest (matching helm-diff's default).
	IncludeHooks bool
	// IncludeTests, when true, additionally includes test-event hooks
	// (helm.sh/hook=test). Has no effect unless IncludeHooks is also set.
	IncludeTests bool
	// ValueFiles is `-f, --values FILE` (multiple allowed).
	ValueFiles []string
	// Set is `--set NAME=VALUE` (multiple allowed).
	Set []string
	// SetString is `--set-string NAME=VALUE`.
	SetString []string
	// SetFile is `--set-file NAME=PATH`.
	SetFile []string
	// Devel is `--devel`.
	Devel bool
	// ChartVersion is `--version VERSION`.
	ChartVersion string
}

func (o RenderOptions) merged(settings *cli.EnvSettings) (map[string]interface{}, error) {
	v := &values.Options{
		ValueFiles:   o.ValueFiles,
		Values:       o.Set,
		StringValues: o.SetString,
		FileValues:   o.SetFile,
	}
	return v.MergeValues(getter.All(settings))
}
