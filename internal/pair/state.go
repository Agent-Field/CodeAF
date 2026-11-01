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
func replaceState(home, profile string) error {
	// A clean slate must not offer the old chats to the new set on startup.
	// Set their folders aside rather than deleting locally readable history.
	for _, name := range []string{"projects", "cells", "sync"} {
		from := filepath.Join(home, "v3", name)
		if _, err := os.Stat(from); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		kept, err := os.MkdirTemp(home, "replaced-chats-")
		if err != nil {
			return err
		}
		if err := os.Rename(from, filepath.Join(kept, name)); err != nil {
			return err
		}
	}
	if profile == "" {
		profile = home
	}
	preferences, err := config.ReadPreferences(profile)
	if err != nil {
		return err
	}
	for key := range preferences {
		preferences[key] = ""
	}
	if err := config.ApplyPreferences(profile, preferences); err != nil {
		return err
	}
	path := filepath.Join(home, "graph.db")
	if _, err := os.Stat(path); err == nil {
		brain, err := store.Open(path)
		if err != nil {
			return err
		}
		err = brain.ClearSharedMemories()
		_ = brain.Close()
		if err != nil {
			return err
		}
	}
	names := []string{"preferences-sync.ledger", "memory-sync.ledger", "provider-keys.ledger", "vault.enc", "vault.enc.next"}
	if config.TeamsShareAt(profile) {
		if _, err := teams.SetAside(profile); err != nil && !os.IsNotExist(err) {
			return err
		}
		names = append(names, "teams-sync.ledger", "teams-traffic-sync.ledger")
	}
	for _, name := range names {
		if err := os.Remove(filepath.Join(home, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
