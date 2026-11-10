package session

import (
	"path/filepath"
	"testing"
	"time"

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
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := ledgerAgent(t, path, func(c *Config) { c.Seat = seat })
	agent.bank(bankedCall{used: Usage{Input: 900, Output: 30, CacheRead: 400, CostUSD: 0.0123, Calls: 1},
		model: "vendor/m", role: "planner", ledger: true})
	agent.bank(bankedCall{used: Usage{Input: 5, Calls: 1}, model: "vendor/m", ledger: false})
	if len(seat.got) != 1 {
		t.Fatalf("seat told of %d calls, want 1: %+v", len(seat.got), seat.got)
	}
	if !FlushUsage() {
		t.Fatal("ledger did not flush")
	}
	rows, err := ReadUsage(path, time.Time{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("ledger rows=%v err=%v", rows, err)
	}
	if rows[0].ID == "" {
		t.Fatal("banked call has no ledger identity")
	}
	want := procexec.ModelCall{ID: rows[0].ID, Model: "vendor/m", Role: "planner", TokensIn: 900, TokensOut: 30, TokensCached: 400, CostMicroUSD: 12300}
	if seat.got[0] != want {
		t.Fatalf("got %+v want %+v", seat.got[0], want)
	}
}

// Remote accounting bypasses RecordUsage, but its new charge must still be
// distinguishable from the historical prefix used by legacy take imports.
func TestRemoteUsageCannotCoverAnOldSavedCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), UsageLedgerName)
	defer StopUsageWriter(path)
	agent, _ := ledgerAgent(t, path)
	if err := agent.recordUsageLine(bankedCall{
		model: "remote/m", used: Usage{Input: 30, Output: 3, Calls: 1, CostUSD: .003},
		remote: &remoteUsage{Task: "1", Computer: "fixture", Event: "attempt:1", Seat: SeatWorker},
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := ReadUsage(path, time.Time{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("remote ledger rows=%v err=%v", rows, err)
	}
	if !rows[0].SharedID || rows[0].USD != .003 {
		t.Fatalf("remote charge must retain its price without legacy coverage: %+v", rows[0])
	}
}
