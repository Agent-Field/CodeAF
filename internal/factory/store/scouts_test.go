package store

import (
	"testing"
	"time"
)

func TestTheScoutLedgerCountsTheMonthTheRepoDayAndThePrice(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	for i, n := range []struct {
		repo string
		usd  float64
		at   time.Time
	}{
		{"acme/api", 0.01, day.AddDate(0, -1, 0)}, // last month
		{"acme/api", 0.02, day.AddDate(0, 0, -1)}, // yesterday
		{"acme/api", 0.03, day},
		{"acme/web", 0.05, day},
	} {
		if err := st.NoteScout(n.repo, n.usd, n.at); err != nil {
			t.Fatalf("note %d: %v", i, err)
		}
	}
	got, err := st.Scouts(day)
	if err != nil {
		t.Fatal(err)
	}
	if got.Month < 0.0999 || got.Month > 0.1001 {
		t.Errorf("month = %v, want 0.10", got.Month)
	}
	if got.Today["acme/api"] != 1 || got.Today["acme/web"] != 1 {
		t.Errorf("today = %v", got.Today)
	}
	if got.Typical != 0.02 {
		t.Errorf("typical = %v, want the median 0.02", got.Typical)
	}
}
