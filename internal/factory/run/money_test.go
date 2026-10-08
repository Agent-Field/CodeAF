package run

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func moneyRig(t *testing.T, now *time.Time) (*Money, *store.Store, *[]float64) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var ledger []float64
	m := NewMoney(st, func(item int, usd float64) { ledger = append(ledger, usd) }, func() time.Time { return *now })
	return m, st, &ledger
}

func TestSpendAccumulatesAndLedgers(t *testing.T) {
	now := time.Date(2026, 10, 8, 10, 30, 0, 0, time.UTC)
	m, st, ledger := moneyRig(t, &now)
	it, _ := st.Create(factory.Item{Repo: "a/b", Title: "x", Changed: now.Add(-time.Hour)})
	before, _ := st.Get(it.ID)
	spend := m.Spend(it.ID)
	spend(0.5)
	spend(0)
	spend(0.25)
	got, _ := st.Get(it.ID)
	if got.Stream == nil || got.Stream.Spent != 0.75 {
		t.Fatalf("Spent = %+v", got.Stream)
	}
	if !got.Changed.Equal(before.Changed) {
		t.Fatalf("spending reset the age: %v -> %v", before.Changed, got.Changed)
	}
	if len(*ledger) != 2 || LedgerName != "factory run" {
		t.Fatalf("ledger = %v", *ledger)
	}
}

func TestSparklineBuckets(t *testing.T) {
	now := time.Date(2026, 10, 8, 10, 30, 0, 0, time.UTC)
	m, st, _ := moneyRig(t, &now)
	it, _ := st.Create(factory.Item{Repo: "a/b", Title: "x"})
	spend := m.Spend(it.ID)
	spend(1)
	spend(1)
	now = now.Add(2 * time.Hour)
	spend(1)
	got, _ := st.Get(it.ID)
	a := got.Stream.Activity
	if len(a) != ActivityHours || a[23] != 1 || a[21] != 2 || a[22] != 0 {
		t.Fatalf("activity = %v", a)
	}
	for i := 0; i < 20; i++ {
		spend(1)
	}
	got, _ = st.Get(it.ID)
	if got.Stream.Activity[23] != 7 {
		t.Fatalf("bucket not capped: %v", got.Stream.Activity)
	}
	now = now.Add(30 * time.Hour)
	spend(1)
	got, _ = st.Get(it.ID)
	for i, v := range got.Stream.Activity[:23] {
		if v != 0 {
			t.Fatalf("stale bucket %d: %v", i, got.Stream.Activity)
		}
	}
}

func TestCapReachedAndQuestion(t *testing.T) {
	m := NewMoney(nil, nil, nil)
	if m.CapReached(factory.Item{Stream: &factory.Stream{Spent: 99}}) {
		t.Fatal("no cap was reached")
	}
	it := factory.Item{Cap: 5, Stream: &factory.Stream{Spent: 4.99}}
	if m.CapReached(it) {
		t.Fatal("under the cap")
	}
	it.Stream.Spent = 5
	if !m.CapReached(it) {
		t.Fatal("at the cap")
	}
	q, kind := m.CapQuestion(it)
	if q != "cap of $5 reached · $5 more, or stop?" || kind != "cap" {
		t.Fatalf("%q %q", q, kind)
	}
	if q, _ := m.CapQuestion(factory.Item{Cap: 2.5}); q != "cap of $2.50 reached · $2.50 more, or stop?" {
		t.Fatal(q)
	}
}

func TestSpentTodayAcrossItemsAndDays(t *testing.T) {
	now := time.Date(2026, 10, 8, 23, 30, 0, 0, time.UTC)
	m, st, _ := moneyRig(t, &now)
	old, _ := st.Create(factory.Item{Repo: "a/b", Title: "old", Changed: now.Add(-48 * time.Hour), Stream: &factory.Stream{Spent: 9}})
	other, _ := st.Create(factory.Item{Repo: "a/b", Title: "other", Changed: now, Stream: &factory.Stream{Spent: 2}})
	mine, _ := st.Create(factory.Item{Repo: "a/b", Title: "mine", Changed: now.Add(-48 * time.Hour), Stream: &factory.Stream{Spent: 4}})
	_ = old
	_ = other
	m.Spend(mine.ID)(1.5)
	if got := m.SpentToday(); got != 3.5 {
		t.Fatalf("SpentToday = %v, want 3.5", got)
	}
	now = now.Add(2 * time.Hour)
	if got := m.SpentToday(); got != 0 {
		t.Fatalf("next day SpentToday = %v, want 0", got)
	}
}

func TestRailRead(t *testing.T) {
	now := time.Now()
	m, st, _ := moneyRig(t, &now)
	if m.Rail() != 0 {
		t.Fatal("rail on a new floor")
	}
	if err := st.SetRail(60); err != nil {
		t.Fatal(err)
	}
	if m.Rail() != 60 {
		t.Fatalf("Rail = %v", m.Rail())
	}
}
