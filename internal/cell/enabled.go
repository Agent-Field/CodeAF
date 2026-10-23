package cell

import (
	"runtime"

	"github.com/Agent-Field/codeaf/internal/furrowbin"
)

const WindowsReason = "moving chats is not available on Windows yet; run codeaf in WSL"

// Enabled reports platform support. Startup also asks Available
// before exposing a capability that needs the carried engine.
func Enabled() bool { return DisabledReason() == "" }

// DisabledReason is cheap enough for menus: it never starts an engine or reads
// the filesystem. Windows needs the engine port before it can move chats.
func DisabledReason() string {
	if runtime.GOOS == "windows" {
		return WindowsReason
	}
	return ""
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

// Available adds the engine contract to platform support before any cell,
// identity or relay client is opened.
func Available() bool { return UnavailableReason() == "" }

// engineAvailable is the capability measurement, separate from platform support so a
// test of wiring can declare availability without staging or starting furrow.
var engineAvailable = furrowbin.CellsAvailable

// UseAvailabilityInTests replaces only the engine answer and restores it when
// the caller is done. Production retains the cached engine contract probe.
func UseAvailabilityInTests(available bool) func() {
	previous := engineAvailable
	engineAvailable = func() bool { return available }
	return func() { engineAvailable = previous }
}
