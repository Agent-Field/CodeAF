package main

import (
	"fmt"
	"runtime/debug"
	"strings"
)

// version is what this build calls itself. The release workflow stamps it at
// link time — `-ldflags "-X main.version=v0.1.0"` for a semver tag, or
// `build-<sha12>` for the edge channel off master — and leaves it alone
// otherwise. An unstamped build is not a broken build: `go build ./cmd/aforge`
// from a checkout and `go install` both fall through to the module's own build
// info below, and a build with nothing to say still says "dev".
var version = "dev"

// runVersion answers the one question an installer, a doctor, or a packaging
// script asks before it trusts the binary on the PATH: is aforge here, and
// which one is it. It reads nothing, writes nothing, and needs no API key —
// probing for the binary must never be a configuration problem.
func runVersion() error {
	fmt.Println(versionString())
	return nil
}

func versionString() string {
	return "aforge " + resolvedVersion()
}

// resolvedVersion prefers the link-time stamp, falls back to the version the
// Go toolchain recorded for the main module, and ends at "dev" rather than at
// nothing. Order matters: a released binary is stamped, a `go install
// github.com/...@v1.2.3` binary is not stamped but carries build info, and a
// local `go build` has neither.
func resolvedVersion() string {
	if stamped := strings.TrimSpace(version); !placeholderVersion(stamped) {
		return stamped
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if built := strings.TrimSpace(info.Main.Version); !placeholderVersion(built) {
			return built
		}
	}
	return "dev"
}

// placeholderVersion names the strings that mean "nobody stamped this". Go
// writes "(devel)" into build info for a module built from a working tree, and
// the unstamped default above is "dev"; neither is a version anyone can look up.
func placeholderVersion(candidate string) bool {
	switch strings.ToLower(strings.TrimSpace(candidate)) {
	case "", "dev", "devel", "(devel)", "unknown":
		return true
	}
	return false
}
