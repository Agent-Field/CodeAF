package main

import (
	"flag"
	"os"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

// Linear mode's settings row landed in internal/config/settings.go
// (KeyLinearMode, LinearModeAt, AFORGE_CHAT_LINEAR) — the row the comment in
// chatv2.go said Wave 4 owed the flag. This file is only the seam that lets
// `aforge chat --v2` reach it, kept separate from chatv2.go so it does not
// collide with the assembly wave's edits to that file.

// resolveLinear decides whether this invocation renders in the accessible
// single-column mode (10.1.5). Precedence mirrors wantChatV2's: an explicit
// --linear typed on this command line outranks everything else; short of
// that, the registry resolves it — AFORGE_CHAT_LINEAR before the profile's
// persisted choice before the built-in default, off.
func resolveLinear(flags *flag.FlagSet, flagValue bool) bool {
	explicit := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "linear" {
			explicit = true
		}
	})
	if explicit {
		return flagValue
	}
	return config.LinearModeAt(strings.TrimSpace(os.Getenv("AFORGE_PROFILE_DIR")))
}
