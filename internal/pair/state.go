package pair

import (
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/store"
	"github.com/Agent-Field/codeaf/internal/teams"
	"os"
	"path/filepath"
)

// replaceState removes only the shared classes the person chose to replace.
// The database's other state and machine facts survive this explicit door.
func replaceState(home, profile string, paired func() bool) (string, error) {
	aside := ""
	// A clean slate must not offer the old chats to the new set on startup.
	// Set their folders aside rather than deleting locally readable history.
	for _, name := range []string{"projects", "cells", "sync"} {
		from := filepath.Join(home, "v3", name)
		if _, err := os.Stat(from); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return aside, err
		}
		kept, err := os.MkdirTemp(home, "replaced-chats-")
		if err != nil {
			return aside, err
		}
		if err := os.Rename(from, filepath.Join(kept, name)); err != nil {
			return aside, err
		}
	}
	if profile == "" {
		profile = home
	}
	preferences, err := config.ReadPreferences(profile)
	if err != nil {
		return aside, err
	}
	for key := range preferences {
		preferences[key] = ""
	}
	if err := config.ApplyPreferences(profile, preferences); err != nil {
		return aside, err
	}
	path := filepath.Join(home, "graph.db")
	if _, err := os.Stat(path); err == nil {
		brain, err := store.Open(path)
		if err != nil {
			return aside, err
		}
		err = brain.ClearSharedMemories()
		_ = brain.Close()
		if err != nil {
			return aside, err
		}
	}
	names := []string{"preferences-sync.ledger", "memory-sync.ledger", "provider-keys.ledger", "vault.enc", "vault.enc.next"}
	if config.TeamsSharingAt(profile, paired) {
		path, err := teams.MoveAside(profile)
		if err != nil && !os.IsNotExist(err) {
			return aside, err
		}
		if err == nil {
			aside = path
		}
		names = append(names, "teams-sync.ledger", "teams-traffic-sync.ledger")
	}
	for _, name := range names {
		if err := os.Remove(filepath.Join(home, name)); err != nil && !os.IsNotExist(err) {
			return aside, err
		}
	}
	return aside, nil
}

// HasTeams includes unreadable files conservatively because replacement could
// otherwise set aside a person's teams without first describing that action.
func HasTeams(profile string) bool {
	f, err := teams.Snapshot(profile)
	if err == nil {
		return len(f.Teams) > 0
	}
	return true
}
