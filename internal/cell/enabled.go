package cell

import (
	"runtime"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/furrowbin"
)

// EnvVar overrides the rollout on every channel, but cannot supply an engine
// for an unsupported platform.
const EnvVar = "CODEAF_CELLS"

const (
	OffReason     = "moving chats is off in this build; CODEAF_CELLS=1 turns it on"
	WindowsReason = "moving chats is not available on Windows because furrow needs Unix; run codeaf in WSL"
)

// releaseRevision is the build stamp, kept behind a seam so the channel matrix
// exercises Enabled without manufacturing a second copy of the rollout rule.
var releaseRevision = buildinfo.Revision

// Enabled reports the platform and channel policy. Startup also asks Available
// before exposing a capability that needs the carried engine.
func Enabled() bool { return DisabledReason() == "" }

// DisabledReason is cheap enough for menus: it never starts an engine or reads
// the filesystem. Windows cannot opt into a Unix-only engine.
func DisabledReason() string {
	if runtime.GOOS == "windows" {
		return WindowsReason
	}
	switch env.Get(EnvVar) {
	case "0":
		return OffReason
	case "1":
		return ""
	}
	switch buildinfo.Channel(releaseRevision()) {
	case "stable", "rc":
		return OffReason
	default:
		return ""
	}
}

// UnavailableReason is asked at startup and at explicit command doors, never
// during paint. Engine detection is cached for the life of this process.
func UnavailableReason() string {
	if reason := DisabledReason(); reason != "" {
		return reason
	}
	if !furrowbin.CellsAvailable() {
		return furrowbin.CellsUnavailable
	}
	return ""
}

// Available adds the engine contract to the rollout policy before any cell,
// identity or relay client is opened.
func Available() bool { return UnavailableReason() == "" }
