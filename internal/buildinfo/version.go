// Package buildinfo contains the public Aegis and runtime-adapter version.
package buildinfo

import (
	"runtime/debug"
	"strconv"
	"strings"
)

// Version is overridden with -X for release archives. For binaries produced by
// `go install module@version`, init recovers the module version from Go's
// embedded build information.
var Version string

func init() {
	if Version != "" {
		Version = Normalize(Version)
		return
	}
	Version = "dev"
	if info, ok := debug.ReadBuildInfo(); ok && isStable(info.Main.Version) {
		Version = Normalize(info.Main.Version)
	}
}

func Normalize(version string) string { return strings.TrimPrefix(version, "v") }

func isStable(version string) bool {
	if !strings.HasPrefix(version, "v") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return false
		}
		if _, err := strconv.ParseUint(part, 10, 64); err != nil {
			return false
		}
	}
	return true
}
