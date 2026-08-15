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

// PracticedToday totals WALL CLOCK and not the sum of the parts: two rounds that
// ran at the same time cost one stretch of clock, and a receipt that added them
// would tell the reader the machine practised for longer than the day was.
func TestPracticedTodayMergesOverlapAndClampsToTheDay(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "practiced.db"))
	now := time.Now()
	dayStart, _ := localDayBounds(now)
	at := func(offset time.Duration) time.Time { return dayStart.Add(offset) }

	// Two overlapping rounds: 10:00–10:30 and 10:20–10:50, which is fifty
	// minutes of clock and not an hour of it.
	practiceRoot(t, graph, "p1", at(10*time.Hour), at(10*time.Hour+30*time.Minute), PracticeGroup)
	practiceRoot(t, graph, "p2", at(10*time.Hour+20*time.Minute), at(10*time.Hour+50*time.Minute), PracticeGroup)
	// A round that began yesterday and landed at 00:10 contributes ten minutes.
	practiceRoot(t, graph, "p3", at(-2*time.Hour), at(10*time.Minute), PracticeGroup)
	// Work that is not practice is not practice, whatever it cost.
	practiceRoot(t, graph, "w1", at(9*time.Hour), at(9*time.Hour+45*time.Minute), "")

	practiced, err := graph.PracticedToday(now)
	if err != nil {
		t.Fatalf("PracticedToday: %v", err)
	}
	if want := 60 * time.Minute; practiced != want {
		t.Fatalf("practiced = %s, want %s", practiced, want)
	}
}

// A round still running is counted UP TO NOW, because it is still practising.
func TestPracticedTodayCountsARunningRound(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "running.db"))
	now := time.Now()
	practiceRoot(t, graph, "p1", now.Add(-20*time.Minute), time.Time{}, PracticeGroup)

	practiced, err := graph.PracticedToday(now)
	if err != nil {
		t.Fatalf("PracticedToday: %v", err)
	}
	// A minute of slack: the clock inside the read is the one that was passed in,
	// but the row's own stamp went through a format-and-parse round trip.
	if practiced < 19*time.Minute || practiced > 21*time.Minute {
		t.Fatalf("a running round measured %s", practiced)
	}
}

// A machine that has not practised today says nothing, and says it without an
// error: zero is the honest total and the receipt drops the clause (§16).
func TestPracticedTodayIsZeroOnAQuietDay(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "quiet.db"))
	practiced, err := graph.PracticedToday(time.Now())
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
	practiced, err = graph.PracticedToday(time.Now())
	if err != nil || practiced != 0 {
		t.Fatalf("an unstarted round measured %s (%v)", practiced, err)
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
