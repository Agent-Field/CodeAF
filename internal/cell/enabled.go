package cell

import (
	"runtime"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/furrowbin"
)

// EnvVar opts into moving chats on every channel, but cannot supply an engine
// for an unsupported platform.
const EnvVar = "CODEAF_CELLS"

const (
	OffReason     = "moving chats is off; CODEAF_CELLS=1 turns it on"
	WindowsReason = "moving chats is not available on Windows because furrow needs Unix; run codeaf in WSL"
)

// releaseRevision keeps the build-stamp seam for the channel matrix. C26
// deliberately makes the opt-in independent of this value.
var releaseRevision = buildinfo.Revision

// Enabled reports the platform and opt-in policy. Startup also asks Available
// before exposing a capability that needs the carried engine.
func Enabled() bool { return DisabledReason() == "" }

// DisabledReason is cheap enough for menus: it never starts an engine or reads
// the filesystem. Windows cannot opt into a Unix-only engine.
func DisabledReason() string {
	if runtime.GOOS == "windows" {
		return WindowsReason
	}
	if env.Get(EnvVar) == "1" {
		return ""
	}
	return OffReason
}

// UnavailableReason is asked at startup and at explicit command doors, never
// during paint. Engine detection is cached for the life of this process.
func UnavailableReason() string {
	if reason := DisabledReason(); reason != "" {
		return reason
	}
	if !engineAvailable() {
		return furrowbin.CellsUnavailable
	}
	return ""
}

// Available adds the engine contract to the rollout policy before any cell,
// identity or relay client is opened.
func Available() bool { return UnavailableReason() == "" }

// engineAvailable is the capability measurement, separate from rollout so a
// test of wiring can declare availability without staging or starting furrow.
var engineAvailable = furrowbin.CellsAvailable

// UseAvailabilityInTests replaces only the engine answer and restores it when
// the caller is done. Production retains the cached engine contract probe.
func UseAvailabilityInTests(available bool) func() {
	previous := engineAvailable
	engineAvailable = func() bool { return available }
	return func() { engineAvailable = previous }
}
