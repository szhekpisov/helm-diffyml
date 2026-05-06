// Package diff drives diffyml programmatically. The plugin's "Helm-tuned"
// defaults (--neat, --mask-secrets, --omit-header, -o detailed) are applied
// here unless the caller turns them off; --set-exit-code is opt-in.
package diff

import (
	"bytes"
	"io"

	diffymlcli "github.com/szhekpisov/diffyml/pkg/diffyml/cli"
)

// Options carries the plugin-meta flags forwarded by every subcommand.
type Options struct {
	// Neat enables --neat (strip Helm/ArgoCD/Flux noise).
	Neat bool
	// MaskSecrets enables --mask-secrets.
	MaskSecrets bool
	// OmitHeader enables --omit-header.
	OmitHeader bool
	// Output picks the format (compact|brief|github|gitlab|gitea|json|json-patch|detailed).
	Output string
	// ExitCode is the diffyml-side --set-exit-code (rc=1 when differences exist).
	ExitCode bool

	// Extra is anything the user supplied after a literal `--` on the
	// command line. Forwarded to diffyml verbatim.
	Extra []string
}

// DefaultOptions returns the plugin's tuned defaults: neat, masked, no
// header, detailed format, no forced exit code.
func DefaultOptions() Options {
	return Options{
		Neat:        true,
		MaskSecrets: true,
		OmitHeader:  true,
		Output:      "detailed",
	}
}

// Run executes diffyml against two pre-rendered manifest byte slices.
// `from` and `to` are the YAML payloads. Stdout/stderr go to the provided
// writers. Returns the exit code (0 = no diff/diffs without --exit-code,
// 1 = diffs with --exit-code, 255 = tool error) and any error.
func Run(from, to []byte, opts Options, stdout, stderr io.Writer) (int, error) {
	cfg := diffymlcli.NewCLIConfig()

	cfg.Neat = opts.Neat
	cfg.MaskSecrets = opts.MaskSecrets
	cfg.OmitHeader = opts.OmitHeader
	if opts.Output != "" {
		cfg.Output = opts.Output
	}
	cfg.SetExitCode = opts.ExitCode

	// Forward user-supplied verbatim flags by re-parsing them on top of cfg.
	if len(opts.Extra) > 0 {
		if err := cfg.ParseArgs(opts.Extra); err != nil {
			return diffymlcli.ExitCodeError, err
		}
	}

	// Sentinel paths so any error message refers to a recognisable input.
	cfg.FromFile = "<helm:from>"
	cfg.ToFile = "<helm:to>"

	rc := diffymlcli.NewRunConfig()
	rc.Stdout = stdout
	rc.Stderr = stderr
	// cli.Run validates FromFile/ToFile when FromContent/ToContent are nil,
	// which would fail for our sentinel <helm:*> paths. Always pass at least
	// an empty slice so the file-validation path is skipped.
	rc.FromContent = nonNil(from)
	rc.ToContent = nonNil(to)

	res := diffymlcli.Run(cfg, rc)
	return res.Code, res.Err
}

// VersionString returns the diffyml module version pulled from the binary's
// debug.BuildInfo. Kept here so cmd/version doesn't need a direct diffyml
// import.
func VersionString() string {
	return diffymlVersion()
}

// SafeRun is a convenience wrapper that always returns valid stdout bytes
// even on a tool error. Used by tests that want the output as a string.
func SafeRun(from, to []byte, opts Options) (string, int, error) {
	var stdout, stderr bytes.Buffer
	code, err := Run(from, to, opts, &stdout, &stderr)
	if stderr.Len() > 0 && err == nil {
		err = bytesAsError(stderr.Bytes())
	}
	return stdout.String(), code, err
}

type stderrErr struct{ b []byte }

func (e *stderrErr) Error() string { return string(e.b) }

func bytesAsError(b []byte) error { return &stderrErr{b} }

func nonNil(b []byte) []byte {
	if b == nil {
		return []byte{}
	}
	return b
}
