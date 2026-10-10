package placegraph

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAPlaceIdleSixtyDaysIsSuggested(t *testing.T) {
	in := StaleInput{Snap: staleSnap(idle("old", staleNow.Add(-60*day))), Now: staleNow.Add(-time.Nanosecond)}
	if got := Suggestions(in); len(got) != 0 {
		t.Fatalf("early offer: %+v", got)
	}
	in.Now = staleNow
	if got := Suggestions(in); len(got) != 1 || got[0] != (Suggestion{Kind: "mergeOrArchive", PlaceID: "old"}) {
		t.Fatalf("sixty-day offer: %+v", got)
	}
	in.Activity = map[string]time.Time{"old": staleNow.Add(-day)}
	if got := Suggestions(in); len(got) != 0 {
		t.Fatalf("recent chat activity was ignored: %+v", got)
	}
}

func TestPinnedAndArchivedPlacesAreNeverSuggested(t *testing.T) {
	archived := idle("archived", staleNow.Add(-90*day))
	archived.Archived = true
	snap := staleSnap(archived, idle("pinned", staleNow.Add(-90*day)), idle("plain", staleNow.Add(-90*day)))
	snap.Pinned = []string{"pinned"}
	if got := Suggestions(StaleInput{Snap: snap, Now: staleNow}); len(got) != 1 || got[0].PlaceID != "plain" {
		t.Fatalf("pinned or archived place offered: %+v", got)
	}
}

func TestSnoozeHidesItThirtyDays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "places-stale.json")
	book, err := OpenStale(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := book.Snooze("old", staleNow); err != nil {
		t.Fatal(err)
	}
	// A fresh reader proves the snooze is shared durable state, not a window guess.
	book, err = OpenStale(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, advance := range []time.Duration{0, 30*day - time.Nanosecond, 30 * day} {
		now := staleNow.Add(advance)
		got := Suggestions(StaleInput{Snap: staleSnap(idle("old", staleNow.Add(-90*day))), Now: now, Snoozed: book.Active(now)})
		want := 0
		if advance == 30*day {
			want = 1
		}
		if len(got) != want {
			t.Fatalf("after %v: %+v, want %d offers", advance, got, want)
		}
	}
}
