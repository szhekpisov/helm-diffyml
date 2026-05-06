package helmclient

// Renderer is the surface cmd/* depends on. The production implementation is
// *Client; tests can substitute a fake without spinning up a cluster.
type Renderer interface {
	GetManifest(name string, revision int) ([]byte, error)
	PreviousRevision(name string) (int, error)
	Template(release, chartPath string, opts RenderOptions) ([]byte, error)
	UpgradeDryRun(release, chartPath string, opts RenderOptions) ([]byte, error)
	InstallDryRun(release, chartPath string, opts RenderOptions) ([]byte, error)
	ThreeWayMerged(release, chartPath string, opts RenderOptions, useUpgradeDryRun bool) ([]byte, []byte, error)
}

// Compile-time assertion that *Client satisfies Renderer.
var _ Renderer = (*Client)(nil)
