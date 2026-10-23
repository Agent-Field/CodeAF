package pair

import (
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/store"
	"os"
	"path/filepath"
)

// replaceState removes only the shared classes the person chose to replace.
// The database's other state and machine facts survive this explicit door.
func replaceState(home, profile string) error {
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
	for _, name := range []string{"preferences-sync.ledger", "memory-sync.ledger", "provider-keys.ledger", "vault.enc"} {
		if err := os.Remove(filepath.Join(home, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
