package tui

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/registry"
)

// TestEverySlashCommandIsRegistered is 5.22's law made mechanical, one
// direction only for this wave: every action that exists as typed text has
// to also be a registry row, so a future palette or action strip built from
// internal/registry can never be missing a door the composer already opens.
// (The reverse — every registry row also living on a visible object — is
// Waves 2–3's UI adoption; this wave only guarantees the registry is not
// behind the slash table it was seeded from.)
//
// This lives in internal/tui rather than internal/registry because
// slashCommands is package-private here and stays that way: the dependency
// this test takes runs tui → registry, the same direction the six render
// surfaces will take when they adopt the registry, never the reverse.
func TestEverySlashCommandIsRegistered(t *testing.T) {
	for _, command := range slashCommands {
		entry, ok := registry.BySlash(command.name)
		if !ok {
			t.Fatalf("slash command %q has no internal/registry entry — add one to catalog.go", command.name)
		}
		if entry.Slash != command.name {
			t.Fatalf("registry entry %q resolved for /%s but reports slash alias %q",
				entry.ID, command.name, entry.Slash)
		}
	}
}

// TestRegisteredSlashAliasesStillExist is the coverage test's own
// completeness check, run the other way: a registry row whose Slash names a
// command the table no longer has would let the first test pass by
// accident while the catalog quietly drifted ahead of what actually ships.
// Kept narrow to slash rows only — key-bound and belt-only entries have no
// table in this package to check against.
func TestRegisteredSlashAliasesStillExist(t *testing.T) {
	known := make(map[string]bool, len(slashCommands))
	for _, command := range slashCommands {
		known[command.name] = true
	}
	for _, entry := range registry.ForScope(registry.ScopeAny) {
		if entry.Slash == "" {
			continue
		}
		if !known[entry.Slash] {
			t.Fatalf("registry entry %q names slash alias %q, which slashCommands no longer has",
				entry.ID, entry.Slash)
		}
	}
}
