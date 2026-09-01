package store

import (
	"path/filepath"
	"testing"
	"time"
)

// The two reads notebook.go added, gated where they are actually interesting:
// the wall-clock merge, the day clamp, and the bulk round read's window.

// practiceRoot admits one practice root and stamps its clock directly, because
// the interesting inputs — a root that started yesterday, two that overlap, one
// still running — cannot be produced by Claim and Complete at test speed.
func practiceRoot(t *testing.T, graph *Store, id string, started time.Time, finished time.Time, group string) {
	t.Helper()
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: id, Brief: "practice " + id, Stage: 1, Group: group},
	}}, Provenance{Origin: OriginSelf, Intent: "practice " + id}); err != nil {
		t.Fatalf("splice %q: %v", id, err)
	}
	var finishedAt any
	if !finished.IsZero() {
		finishedAt = formatTime(finished)
	}
	if _, err := graph.db.Exec(`UPDATE nodes SET started_at = ?, finished_at = ? WHERE id = ?`,
		formatTime(started), finishedAt, id); err != nil {
		t.Fatalf("stamp %q: %v", id, err)
	}
}

// THE HOURS THESE READS ARE ASKED AT ARE CHOSEN, NEVER READ OFF THE WALL.
//
// [Store.PracticedToday] takes its clock as an argument, and every test here
// used to hand it `time.Now()` — which made the answer depend on what time of
// day the suite happened to run at. A `make check` that straddled local
// midnight on 2026-09-01 failed [TestPracticedTodayCountsARunningRound] and
// then passed three times in a row twenty minutes later, because a round that
// started twenty minutes ago starts YESTERDAY at ten past midnight and the read
// correctly clamps it to the ten minutes that are today.
//
// So the day boundary is exercised on purpose from both sides, and one instant
// in the middle of an ordinary afternoon as the control. The date is today's
// only so the fixture reads plausibly; nothing below depends on which day it is.
func practiceMoments() []struct {
	name string
	at   time.Time
} {
	day := time.Now()
	return []struct {
		name string
		at   time.Time
	}{
		{"a second before midnight", time.Date(day.Year(), day.Month(), day.Day(), 23, 59, 59, 0, time.Local)},
		{"a second after midnight", time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 1, 0, time.Local)},
		{"the middle of the afternoon", time.Date(day.Year(), day.Month(), day.Day(), 14, 30, 0, 0, time.Local)},
	}
}

// PracticedToday totals WALL CLOCK and not the sum of the parts: two rounds that
// ran at the same time cost one stretch of clock, and a receipt that added them
// would tell the reader the machine practised for longer than the day was.
func TestPracticedTodayMergesOverlapAndClampsToTheDay(t *testing.T) {
	for _, moment := range practiceMoments() {
		t.Run(moment.name, func(t *testing.T) {
			graph := openTestStore(t, filepath.Join(t.TempDir(), "practiced.db"))
			dayStart, _ := localDayBounds(moment.at)
			at := func(offset time.Duration) time.Time { return dayStart.Add(offset) }

			// Two overlapping rounds: 10:00–10:30 and 10:20–10:50, which is fifty
			// minutes of clock and not an hour of it.
			practiceRoot(t, graph, "p1", at(10*time.Hour), at(10*time.Hour+30*time.Minute), PracticeGroup)
			practiceRoot(t, graph, "p2", at(10*time.Hour+20*time.Minute), at(10*time.Hour+50*time.Minute), PracticeGroup)
			// A round that began yesterday and landed at 00:10 contributes ten minutes.
			practiceRoot(t, graph, "p3", at(-2*time.Hour), at(10*time.Minute), PracticeGroup)
			// Work that is not practice is not practice, whatever it cost.
			practiceRoot(t, graph, "w1", at(9*time.Hour), at(9*time.Hour+45*time.Minute), "")

			// SIXTY MINUTES AT EVERY HOUR OF THE DAY, because the day is what
			// the read clamps to and the hour it is asked at is not part of the
			// question — every round above is inside the day whichever instant
			// of it the receipt is drawn at.
			practiced, err := graph.PracticedToday(moment.at)
			if err != nil {
				t.Fatalf("PracticedToday: %v", err)
			}
			if want := 60 * time.Minute; practiced != want {
				t.Fatalf("practiced = %s, want %s", practiced, want)
			}
		})
	}
}

// A round still running is counted UP TO NOW, because it is still practising —
// and no further back than local midnight, because that is where today starts.
func TestPracticedTodayCountsARunningRound(t *testing.T) {
	for _, moment := range practiceMoments() {
		t.Run(moment.name, func(t *testing.T) {
			graph := openTestStore(t, filepath.Join(t.TempDir(), "running.db"))
			practiceRoot(t, graph, "p1", moment.at.Add(-20*time.Minute), time.Time{}, PracticeGroup)

			practiced, err := graph.PracticedToday(moment.at)
			if err != nil {
				t.Fatalf("PracticedToday: %v", err)
			}
			// Twenty minutes, or the whole of today where today is younger than
			// that: a round that began at 23:40 has spent one second of the day
			// it is read in, and saying twenty would be counting yesterday.
			dayStart, _ := localDayBounds(moment.at)
			want := 20 * time.Minute
			if since := moment.at.Sub(dayStart); since < want {
				want = since
			}
			// A minute of slack: the clock inside the read is the one that was passed in,
			// but the row's own stamp went through a format-and-parse round trip.
			if practiced < want-time.Minute || practiced > want+time.Minute {
				t.Fatalf("a running round measured %s, want about %s", practiced, want)
			}
		})
	}
}

// A machine that has not practised today says nothing, and says it without an
// error: zero is the honest total and the receipt drops the clause (§16).
func TestPracticedTodayIsZeroOnAQuietDay(t *testing.T) {
	for _, moment := range practiceMoments() {
		t.Run(moment.name, func(t *testing.T) {
			graph := openTestStore(t, filepath.Join(t.TempDir(), "quiet.db"))
			practiced, err := graph.PracticedToday(moment.at)
			if err != nil {
				t.Fatalf("PracticedToday: %v", err)
			}
			if practiced != 0 {
				t.Fatalf("a quiet day practised %s", practiced)
			}
			// A node with no start spent no time, and is not counted as if it had.
			if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
				{ID: "p1", Brief: "queued practice", Stage: 1, Group: PracticeGroup},
			}}, Provenance{Origin: OriginSelf, Intent: "queued practice"}); err != nil {
				t.Fatal(err)
			}
			practiced, err = graph.PracticedToday(moment.at)
			if err != nil || practiced != 0 {
				t.Fatalf("an unstarted round measured %s (%v)", practiced, err)
			}
		})
	}
}

// The bulk round read answers with nothing on an empty store rather than with an
// error, which is the shape a surface needs: an empty band is a true picture.
func TestRecentQuestionPracticesIsEmptyOnAFreshStore(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "rounds.db"))
	rounds, err := graph.RecentQuestionPractices(0)
	if err != nil {
		t.Fatalf("RecentQuestionPractices: %v", err)
	}
	if len(rounds) != 0 {
		t.Fatalf("a fresh store had %d rounds", len(rounds))
	}
}
