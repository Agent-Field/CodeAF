package session

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// TestTeamSpendReadsTheSessionsLedger pins the one join internal/teams makes
// against this package without importing it (it cannot; this package imports
// it): a team's spend is read from the file [UsageLedgerPath] names, by the
// JSON names [UsageLine] writes. Two spellings of one path would be two
// ledgers, and a renamed field would read every team's day as $0.
func TestTeamSpendReadsTheSessionsLedger(t *testing.T) {
	if teams.UsageLedgerPath() != UsageLedgerPath() {
		t.Fatalf("teams reads %s, the ledger is %s", teams.UsageLedgerPath(), UsageLedgerPath())
	}
	raw, err := json.Marshal(UsageLine{At: time.Now(), Day: "2026-09-24", Calls: 1, USD: 1.5,
		Session: "0123456789abcdef", Root: "fedcba9876543210"})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"at", "day", "calls", "usd", "session", "root"} {
		if _, ok := fields[name]; !ok {
			t.Errorf("a usage line has no %q field, which internal/teams' spend.go reads", name)
		}
	}
}
