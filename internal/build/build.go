// Package build holds version metadata stamped at link time by goreleaser.
package build

// Version is the plugin's release tag (e.g. "v0.1.0"). Defaults to "dev" for
// local `go build` invocations.
var Version = "dev"

// Commit is the git SHA of the build. Defaults to "none".
var Commit = "none"

// Date is the build timestamp in RFC3339 form. Defaults to "unknown".
var Date = "unknown"
