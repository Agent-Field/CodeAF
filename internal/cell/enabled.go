package cell

import (
	"github.com/Agent-Field/codeaf/internal/furrowbin"
)

// Enabled reports support for moving chats on every shipped platform.
// Startup also asks Available before exposing doors that need the engine.
func Enabled() bool { return true }

// DisabledReason remains the cheap menu door; only the engine probe can make
// moving chats unavailable now that Windows has a native engine (K1).
func DisabledReason() string { return "" }

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
