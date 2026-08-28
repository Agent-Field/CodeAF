package pair

// Which relay this machine uses.
//
// THERE IS NO BUILT-IN DEFAULT, AND THAT IS DELIBERATE. The relay service is
// not deployed; a default pointing at an address that does not answer would
// turn "nothing is set up" — which has a next step — into "the network is
// broken", which does not. So an unset relay is its own state with its own
// sentence ([NoRelay]), and the day the service goes live is the day a default
// belongs in this file and not before.
//
// A MACHINE REMEMBERS THE RELAY IT WAS PAIRED THROUGH, so a device that has
// used two of them reaches each machine through the right one without anybody
// having to say which. The environment variable still wins over that, because
// an override that could not override would not be one.

import (
	"os"
	"path/filepath"
	"strings"
)

// RelayEnv names the override, exported so that a sentence and the code that
// reads it spell it the same way.
const RelayEnv = "AFORGE_RELAY"

// RelayFile is where a relay address is kept when somebody wants it to outlive
// a shell.
func RelayFile() string { return filepath.Join(Dir(), "relay") }

// Relay is the address this machine uses when nothing else says otherwise.
// Empty means none is set up.
func Relay() string {
	if set := strings.TrimSpace(os.Getenv(RelayEnv)); set != "" {
		return set
	}
	raw, err := os.ReadFile(RelayFile())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// RelayFor is the address to reach one machine through: the override, then the
// relay that machine was paired through, then the configured one.
func RelayFor(known Known, found bool) string {
	if set := strings.TrimSpace(os.Getenv(RelayEnv)); set != "" {
		return set
	}
	if found && strings.TrimSpace(known.Service) != "" {
		return strings.TrimSpace(known.Service)
	}
	return Relay()
}
