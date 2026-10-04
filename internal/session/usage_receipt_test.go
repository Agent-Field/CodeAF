package session

import (
	"path/filepath"
	"testing"

	procexec "github.com/Agent-Field/codeaf/internal/executor"
)

// noteSeat is a seat that keeps the model calls it is told of.
type noteSeat struct {
	procexec.Seat
	got []procexec.ModelCall
}

func (s *noteSeat) NoteModelCall(m procexec.ModelCall) { s.got = append(s.got, m) }

// Every call the ledger row is written for is also told to the session's seat,
// with the same figures: the ledger and the receipt are one call seen twice.
func TestABankedCallReachesTheSeatBesideTheLedger(t *testing.T) {
	seat := &noteSeat{Seat: procexec.Host}
	agent, _ := ledgerAgent(t, filepath.Join(t.TempDir(), UsageLedgerName), func(c *Config) { c.Seat = seat })
	agent.bank(bankedCall{used: Usage{Input: 900, Output: 30, CacheRead: 400, CostUSD: 0.0123, Calls: 1},
		model: "vendor/m", role: "planner", ledger: true})
	agent.bank(bankedCall{used: Usage{Input: 5, Calls: 1}, model: "vendor/m", ledger: false})
	if len(seat.got) != 1 {
		t.Fatalf("seat told of %d calls, want 1: %+v", len(seat.got), seat.got)
	}
	want := procexec.ModelCall{Model: "vendor/m", Role: "planner", TokensIn: 900, TokensOut: 30, TokensCached: 400, CostMicroUSD: 12300}
	if seat.got[0] != want {
		t.Fatalf("got %+v want %+v", seat.got[0], want)
	}
}
