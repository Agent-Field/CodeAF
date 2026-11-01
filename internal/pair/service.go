package pair

// Which relay this machine uses.
//
// The relay a machine is reached through is the one it syncs through, unless
// somebody said otherwise. Sync already found an address — the person's
// variable, the relay a pairing saved, or the hosted default — and a second
// answer here would be a machine whose chats sync while `codeaf serve` says
// there is nowhere to be reachable from. So the caller hands in sync's answer
// and it is the last word, after the overrides this package has always read.
//
// A MACHINE REMEMBERS THE RELAY IT WAS PAIRED THROUGH, so a device that has
// used two of them reaches each machine through the right one without anybody
// having to say which. The environment variable still wins over that, because
// an override that could not override would not be one.

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/env"
)

// RelayEnv names the override, exported so that a sentence and the code that
// reads it spell it the same way.
const RelayEnv = "CODEAF_RELAY"

// RelayFile is where a relay address is kept when somebody wants it to outlive
// a shell.
func RelayFile() string { return filepath.Join(Dir(), "relay") }

// Relay is the address this machine uses: the override, the saved file, then
// sync, the address this machine syncs through (empty when sync is off).
// Empty means none is set up.
func Relay(sync string) string {
	if set := strings.TrimSpace(env.Get(RelayEnv)); set != "" {
		return set
	}
	if raw, err := os.ReadFile(RelayFile()); err == nil && strings.TrimSpace(string(raw)) != "" {
		return strings.TrimSpace(string(raw))
	}
	return strings.TrimSpace(sync)
}

// RelayFor is the address to reach one machine through: the override, then the
// relay that machine was paired through, then [Relay].
func RelayFor(known Known, found bool, sync string) string {
	if set := strings.TrimSpace(env.Get(RelayEnv)); set != "" {
		return set
	}
	if found && strings.TrimSpace(known.Service) != "" {
		return strings.TrimSpace(known.Service)
	}
	return Relay(sync)
}
