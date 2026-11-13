package identity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTeamSyncLedgerDoesNotBlockJoiningAnUnusedHome(t *testing.T) {
	home := t.TempDir()
	if _, err := EnsureSolo(home); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"teams-sync.ledger", "teams-traffic-sync.ledger"} {
		if err := os.WriteFile(filepath.Join(home, name), []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	can, err := Joinable(home)
	if err != nil || !can {
		t.Fatalf("team sync ledger blocked join: %v %v", can, err)
	}
}
