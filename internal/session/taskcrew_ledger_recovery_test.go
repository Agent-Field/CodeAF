package session

import (
	"testing"
	"time"
)

func TestRestoredCrewDayKeepsSavedSpendAheadOfTheLedger(t *testing.T) {
	now := time.Now()
	ledger := 1.0
	day := newLedgerSpendDay(func() float64 { return ledger }, func() time.Time { return now })
	saved := &crewGuardRecord{Day: now.Format("2006-01-02"), DaySpent: 5, DayLast: map[string]float64{"accepted/worker": .75}}
	restoreCrewDay(day, saved)
	if got := day.Total(); got != 5 {
		t.Fatalf("saved spend lost to initial ledger refresh: got %v, want 5", got)
	}
	if got := day.lastCost("accepted/worker"); got != .75 {
		t.Fatalf("saved call estimate lost: %v", got)
	}
	if _, ok := day.hold("accepted/worker", .1, 5.5); ok {
		t.Fatal("restored last-call price no longer guards the next call")
	}
	ledger = 8
	if got := day.Total(); got != 8 {
		t.Fatalf("newer shared spend hidden by recovery: %v", got)
	}
	now = now.AddDate(0, 0, 1)
	ledger = 0
	if got := day.Total(); got != 0 {
		t.Fatalf("old spend carried into the next day: %v", got)
	}
}
