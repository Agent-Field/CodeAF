package factory_test

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// TODAY'S FIGURE COUNTS WHAT IS PLACED TODAY. A job keeps one total, so a job
// that began on an earlier day cannot say what part of it fell today, and moving
// today does not make all of it today's. The figure must not claim more than was
// spent today, and it counts a job's chat spend the way every spend read does.
func TestShiftSpentTodayCountsOnlyWhatIsPlacedToday(t *testing.T) {
	_, st := openLocal(t)
	now := time.Now()
	yesterday := now.Add(-48 * time.Hour)
	for _, it := range []factory.Item{
		{Repo: "acme/web", Title: "began today", Created: now, Changed: now, Stream: &factory.Stream{Spent: 1.25, Started: now}},
		{Repo: "acme/web", Title: "began earlier, moved today", Created: yesterday, Changed: now, Stream: &factory.Stream{Spent: 40, Started: yesterday, Ended: now}},
		{Repo: "acme/web", Title: "chat only, today", Created: now, Changed: now, ChatSpent: 0.5},
		{Repo: "acme/web", Title: "chat only, earlier, moved today", Created: yesterday, Changed: now, ChatSpent: 7},
	} {
		if _, err := st.Create(it); err != nil {
			t.Fatal(err)
		}
	}
	snap, err := factory.LocalSeam(st, now.Add(-time.Hour)).Load()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := snap.Shift.SpentToday, 1.75; got != want {
		t.Fatalf("SpentToday = %v, want %v", got, want)
	}
}
