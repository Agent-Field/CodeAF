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
	names := []string{"preferences-sync.ledger", "memory-sync.ledger", "provider-keys.ledger", "vault.enc"}
	if config.TeamsSharingAt(profile, paired) {
		path, err := teams.SetAside(profile)
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
