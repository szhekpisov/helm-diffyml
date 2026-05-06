package diff

import "runtime/debug"

// diffymlVersion reads the diffyml module version from this binary's build
// info, so it stays in sync with go.mod without manual bookkeeping.
func diffymlVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, dep := range info.Deps {
		if dep.Path == "github.com/szhekpisov/diffyml" {
			return dep.Version
		}
	}
	return "unknown"
}
