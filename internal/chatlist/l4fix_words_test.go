package chatlist

import (
	"github.com/Agent-Field/codeaf/internal/directory"
	"strings"
	"testing"
)

// L4-G3: The sync-off command refusal names the service's address.
func TestL4FixSyncOffWords(t *testing.T) {
	if SyncOff != "sync is off: set CODEAF_SYNC_URL to your sync service's address" {
		t.Fatal(SyncOff)
	}
}

// L4-G4: A live hold alone never claims work is running on the holding computer.
func TestL4FixIdleHolderWords(t *testing.T) {
	rows := Rows(listing(map[string]directory.Cell{"c": cell(other, 1_010_000, nil)}), self, plain)
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	line := StatusLine(rows[0])
	if strings.Contains(line, "running on") || line != "held on host-a" {
		t.Fatal(line)
	}
	if text := TakeoverLine(rows[0]); strings.Contains(text, "running on") || !strings.Contains(text, "held on host-a") {
		t.Fatal(text)
	}
}
