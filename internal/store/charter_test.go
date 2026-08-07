package store

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCharterCRUDAndRebuildRoundTrip(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "charters.db"))
	charter := mustTestCharter(t, "charter-release", CharterActive, CharterRails{
		PerFiringBudgetUSD: 0.20, MaxFiringsPerDay: 3,
	})
	if err := graph.CreateCharter(charter); err != nil {
		t.Fatal(err)
	}

	created, found, err := graph.Charter(charter.ID)
	if err != nil || !found {
		t.Fatalf("created charter found=%t err=%v", found, err)
	}
	if created.Invariant != charter.Invariant || !reflect.DeepEqual(created.Rails(), charter.Rails()) {
		t.Fatalf("created charter = %+v rails=%+v", created, created.Rails())
	}
	node, found, err := graph.Node(charter.ID)
	if err != nil || !found || node.Parent != RootID || node.Group != CharterGroup ||
		node.Status != Done || !node.FoldRoot || !IsOrganizationalGroup(node.Group) {
		t.Fatalf("charter node = %+v found=%t err=%v", node, found, err)
	}
	ready, err := graph.Ready(0)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range ready {
		if candidate.ID == charter.ID {
			t.Fatal("charter became a ready job card")
		}
	}

	revisedWatch := WatchSpec{Kind: WatchCron, Cron: &CronSchedule{Kind: CronWeekdays, Hour: 9, Minute: 15}}
	revisedRails := CharterRails{PerFiringBudgetUSD: 0.35, MaxFiringsPerDay: 2}
	if err := graph.ReviseCharter(charter.ID, "Keep release notes current.", revisedWatch,
		"Did a release land?", CharterAction{Template: "Update the release notes"}, revisedRails); err != nil {
		t.Fatal(err)
	}
	if err := graph.SetCharterStatus(charter.ID, CharterPaused, Ratification{}); err != nil {
		t.Fatal(err)
	}
	if err := graph.SetCharterStatus(charter.ID, CharterActive, Ratification{
		Origin: OriginUser, SessionID: "charter-session", Evidence: "yes, resume it",
	}); err != nil {
		t.Fatal(err)
	}

	before, err := graph.Charters()
	if err != nil {
		t.Fatal(err)
	}
	snapshotBefore, err := graph.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	eventsBefore, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	after, err := graph.Charters()
	if err != nil {
		t.Fatal(err)
	}
	snapshotAfter, err := graph.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	eventsAfter, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("rebuilt charters differ\nbefore: %#v\nafter:  %#v", before, after)
	}
	if !reflect.DeepEqual(snapshotBefore, snapshotAfter) {
		t.Fatal("rebuilt charter node differs")
	}
	if !reflect.DeepEqual(eventsBefore, eventsAfter) {
		t.Fatal("rebuild changed charter journal")
	}

	if err := graph.SetCharterStatus(charter.ID, CharterRetired, Ratification{}); err != nil {
		t.Fatal(err)
	}
	retired, _, err := graph.Charter(charter.ID)
	if err != nil || retired.Status != CharterRetired {
		t.Fatalf("retired charter = %+v err=%v", retired, err)
	}
	if err := graph.SetCharterStatus(charter.ID, CharterActive, charter.Ratification); err == nil {
		t.Fatal("retired charter was reactivated")
	}
}

func TestCharterCannotExistWithoutRails(t *testing.T) {
	watch := WatchSpec{Kind: WatchPoll, Poll: &PollWatch{Condition: "check", Cadence: time.Minute}}
	_, err := NewCharter("unbounded", "keep this true", watch, "check it",
		CharterAction{Template: "repair it"}, CharterRails{}, CharterProposed, Ratification{})
	if err == nil {
		t.Fatal("constructed charter without mandatory rails")
	}
}

func TestCharterWakeStateSurvivesRebuild(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "wake-rebuild.db"))
	charter := mustTestCharter(t, "charter-wake", CharterActive, CharterRails{
		PerFiringBudgetUSD: 0.1, MaxFiringsPerDay: 2,
	})
	if err := graph.CreateCharter(charter); err != nil {
		t.Fatal(err)
	}
	stored, _, _ := graph.Charter(charter.ID)
	wakeAt := stored.NextDue.Add(time.Second)
	wakeSeq, err := graph.BeginCharterWake(charter.ID, wakeAt, "poll due", CharterWatchState{
		NextDue: wakeAt.Add(time.Hour), GraphCursor: 17, GraphDay: "2030-04-03", GraphTriggered: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordSentinelCheck(charter.ID, SentinelCheck{WakeSeq: wakeSeq, Yes: true, Line: "condition occurred"}); err != nil {
		t.Fatal(err)
	}
	before, _, _ := graph.Charter(charter.ID)
	if !before.WakePending || !before.SentinelYes {
		t.Fatalf("pending firing = %+v", before)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	after, _, _ := graph.Charter(charter.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("wake changed during rebuild\nbefore: %#v\nafter:  %#v", before, after)
	}
}

func TestNextCronDueStructuredLocalSchedulesAreDSTSafe(t *testing.T) {
	location, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		schedule CronSchedule
		after    time.Time
		want     time.Time
	}{
		{
			name: "minutes", schedule: CronSchedule{Kind: CronEveryMinutes, Interval: 15},
			after: time.Date(2024, time.January, 2, 8, 7, 0, 0, location),
			want:  time.Date(2024, time.January, 2, 8, 22, 0, 0, location),
		},
		{
			name: "hours", schedule: CronSchedule{Kind: CronEveryHours, Interval: 2},
			after: time.Date(2024, time.January, 2, 8, 7, 0, 0, location),
			want:  time.Date(2024, time.January, 2, 10, 7, 0, 0, location),
		},
		{
			name: "spring gap", schedule: CronSchedule{Kind: CronDaily, Hour: 2, Minute: 30},
			after: time.Date(2024, time.March, 9, 3, 0, 0, 0, location),
			want:  time.Date(2024, time.March, 10, 3, 0, 0, 0, location),
		},
		{
			name: "weekday weekend", schedule: CronSchedule{Kind: CronWeekdays, Hour: 9, Minute: 0},
			after: time.Date(2024, time.March, 8, 10, 0, 0, 0, location),
			want:  time.Date(2024, time.March, 11, 9, 0, 0, 0, location),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NextCronDue(test.schedule, test.after)
			if err != nil {
				t.Fatal(err)
			}
			if !got.Equal(test.want) {
				t.Fatalf("next due = %s (%s), want %s (%s)", got, got.Location(), test.want, test.want.Location())
			}
		})
	}

	firstFall := time.Date(2024, time.November, 3, 1, 30, 0, 0, location)
	next, err := NextCronDue(CronSchedule{Kind: CronDaily, Hour: 1, Minute: 30}, firstFall.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	wantNextDay := time.Date(2024, time.November, 4, 1, 30, 0, 0, location)
	if !next.Equal(wantNextDay) {
		t.Fatalf("fall-back schedule double-fired: got %s, want %s", next, wantNextDay)
	}

	previousLocal := time.Local
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })
	persistedUTC := time.Date(2024, time.March, 9, 9, 0, 0, 0, location).UTC()
	reloadedNext, err := NextWatchDue(WatchSpec{Kind: WatchCron, Cron: &CronSchedule{
		Kind: CronDaily, Hour: 9,
	}}, persistedUTC)
	if err != nil {
		t.Fatal(err)
	}
	wantLocal := time.Date(2024, time.March, 10, 9, 0, 0, 0, location)
	if !reloadedNext.Equal(wantLocal) {
		t.Fatalf("persisted cron lost local DST policy: got %s, want %s", reloadedNext, wantLocal)
	}
}

func mustTestCharter(t *testing.T, id string, status CharterStatus, rails CharterRails) Charter {
	t.Helper()
	charter, err := NewCharter(id, "Keep releases documented.", WatchSpec{
		Kind: WatchPoll, Poll: &PollWatch{Condition: "look for releases", Cadence: time.Hour},
	}, "Did a new release occur?", CharterAction{Template: "Update release documentation"}, rails,
		status, Ratification{Origin: OriginUser, SessionID: "charter-session", Evidence: "yes, stand this up"})
	if err != nil {
		t.Fatal(err)
	}
	return charter
}

// The sentinel wrote down its reasoning at every wake and never saw a word of
// it again: Line and Error had no reader anywhere in the tree. For a poll
// charter the wake evidence is the constant condition string, so the judgment
// call was byte-identical every time — a firing the user has already declined
// is judged the same way an hour later, and again, forever.
func TestRecentSentinelJudgmentsCarryWhatBecameOfEachOne(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "sentinel-memory.db"))
	expires := time.Now().Add(24 * time.Hour)
	charter, err := NewCharter("watch-repeats", "Tell me when the build breaks",
		WatchSpec{Kind: WatchPoll, Poll: &PollWatch{Condition: "the build is red", Cadence: time.Minute}},
		"red build", CharterAction{Template: "look at the build"},
		CharterRails{PerFiringBudgetUSD: 1, MaxFiringsPerDay: 8, ExpiresAt: &expires},
		CharterActive, Ratification{Origin: OriginUser, Evidence: "ratified"})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.CreateCharter(charter); err != nil {
		t.Fatal(err)
	}
	judge := func(yes bool, line string) int64 {
		t.Helper()
		wakeSeq, err := graph.BeginCharterWake(charter.ID, time.Now(), "the build is red",
			CharterWatchState{NextDue: time.Now().Add(time.Minute)})
		if err != nil {
			t.Fatal(err)
		}
		if err := graph.RecordSentinelCheck(charter.ID, SentinelCheck{
			WakeSeq: wakeSeq, Yes: yes, Line: line,
		}); err != nil {
			t.Fatal(err)
		}
		return wakeSeq
	}

	judge(false, "the build has been green for an hour")
	// A new charter is on probation, so a yes asks before it fires — and this
	// is the refusal the next wake has to know about.
	wakeSeq := judge(true, "three jobs failed on main")
	if err := graph.DeclineCharterFiring(charter.ID, wakeSeq, "already looked at it", false); err != nil {
		t.Fatal(err)
	}

	judgments, err := graph.RecentSentinelJudgments(charter.ID, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(judgments) != 2 {
		t.Fatalf("judgments = %+v, want both wakes newest first", judgments)
	}
	if !judgments[0].Yes || judgments[0].Line != "three jobs failed on main" {
		t.Fatalf("newest judgment = %+v", judgments[0])
	}
	if !strings.Contains(judgments[0].Outcome, "declined") {
		t.Fatalf("outcome = %q; the sentinel cannot see that its last firing was refused", judgments[0].Outcome)
	}
	if judgments[1].Yes || judgments[1].Line != "the build has been green for an hour" {
		t.Fatalf("older judgment = %+v", judgments[1])
	}
	if judgments[1].Outcome != "" {
		t.Fatalf("a no was reported as having an outcome: %q", judgments[1].Outcome)
	}
}
