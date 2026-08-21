package standing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLedgerSumsTodayForOneItemAndForAll(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)

	lines := []Entry{
		{At: now, ItemID: "aaaaaaaaaaaaaaaa", Kind: entryCheck, USD: 0.001},
		{At: now.Add(time.Minute), ItemID: "aaaaaaaaaaaaaaaa", Kind: string(ActionSay), USD: 0.01},
		{At: now.Add(2 * time.Minute), ItemID: "bbbbbbbbbbbbbbbb", Kind: string(ActionTask), USD: 0.4, Run: "runs/0001"},
		{At: now.Add(3 * time.Minute), ItemID: "bbbbbbbbbbbbbbbb", Kind: string(ActionTask), USD: 0.6, Run: "runs/0002"},
		// Yesterday's line must not be counted by today's rail.
		{At: now.Add(-24 * time.Hour), ItemID: "aaaaaaaaaaaaaaaa", Kind: string(ActionSay), USD: 5},
	}
	for _, entry := range lines {
		if err := store.Append(entry); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	first, err := store.Today("aaaaaaaaaaaaaaaa", now)
	if err != nil {
		t.Fatalf("today: %v", err)
	}
	if first.Fired != 1 {
		t.Fatalf("the first item fired %d times today, wanted 1 — a check is not a firing", first.Fired)
	}
	if first.USD < 0.0109 || first.USD > 0.0111 {
		t.Fatalf("the first item spent %v, wanted the say and the check", first.USD)
	}

	second, err := store.Today("bbbbbbbbbbbbbbbb", now)
	if err != nil {
		t.Fatalf("today: %v", err)
	}
	if second.Fired != 2 || second.USD < 0.999 || second.USD > 1.001 {
		t.Fatalf("the second item is %+v, wanted two firings and a dollar", second)
	}

	all, err := store.Today("", now)
	if err != nil {
		t.Fatalf("today: %v", err)
	}
	if all.Fired != 3 {
		t.Fatalf("the day fired %d times, wanted 3", all.Fired)
	}
	if all.USD < 1.0109 || all.USD > 1.0111 {
		t.Fatalf("the day spent %v, wanted 1.011", all.USD)
	}

	// One line per firing, in one file named for the local day.
	raw, err := os.ReadFile(store.LedgerPath(now))
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	if got := len(strings.Split(strings.TrimRight(string(raw), "\n"), "\n")); got != 4 {
		t.Fatalf("today's ledger has %d lines, wanted 4", got)
	}
	if !strings.HasSuffix(store.LedgerPath(now), "ledger-2026-08-20.jsonl") {
		t.Fatalf("the ledger is at %s", store.LedgerPath(now))
	}
}

func TestLedgerIsEmptyOnADayNothingHappened(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	spend, err := store.Today("", now)
	if err != nil {
		t.Fatalf("a day with no file is not a failure: %v", err)
	}
	if spend.Fired != 0 || spend.USD != 0 {
		t.Fatalf("an empty day is %+v", spend)
	}
}

func TestLedgerKeepsCountingPastATornLine(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	if err := store.Append(Entry{At: now, ItemID: "aaaaaaaaaaaaaaaa", Kind: string(ActionSay), USD: 0.02}); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(store.LedgerPath(now), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("{\"at\":\"broken\n"); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if err := store.Append(Entry{At: now, ItemID: "aaaaaaaaaaaaaaaa", Kind: string(ActionSay), USD: 0.03}); err != nil {
		t.Fatal(err)
	}
	spend, err := store.Today("aaaaaaaaaaaaaaaa", now)
	if err != nil {
		t.Fatalf("today: %v", err)
	}
	if spend.Fired != 2 {
		t.Fatalf("a torn line stopped the count at %d", spend.Fired)
	}
}

// A WEEK IS A WALK OVER THE DAY FILES, and it answers per item so one reading
// serves a whole screen. This is what a card means by `3 runs this week`.
func TestRunsSinceSumsEveryDayInReachPerItem(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)

	for _, entry := range []Entry{
		{At: now.Add(-6 * 24 * time.Hour), ItemID: "aaaaaaaaaaaaaaaa", Kind: string(ActionSay), USD: 0.01},
		{At: now.Add(-2 * 24 * time.Hour), ItemID: "aaaaaaaaaaaaaaaa", Kind: string(ActionSay), USD: 0.02},
		{At: now, ItemID: "aaaaaaaaaaaaaaaa", Kind: entryCheck, USD: 0.01},
		{At: now, ItemID: "bbbbbbbbbbbbbbbb", Kind: string(ActionTask), USD: 0.40},
		// Outside the week, and outside the answer.
		{At: now.Add(-9 * 24 * time.Hour), ItemID: "aaaaaaaaaaaaaaaa", Kind: string(ActionSay), USD: 5},
	} {
		if err := store.Append(entry); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	week, err := store.RunsSince(now.Add(-7 * 24 * time.Hour))
	if err != nil {
		t.Fatalf("runs since: %v", err)
	}
	first := week["aaaaaaaaaaaaaaaa"]
	if first.Fired != 2 {
		t.Fatalf("the first item fired %d times this week, wanted 2 — a check is not a firing", first.Fired)
	}
	if first.USD < 0.0399 || first.USD > 0.0401 {
		t.Fatalf("the first item spent %v this week, wanted the two firings and the check", first.USD)
	}
	second := week["bbbbbbbbbbbbbbbb"]
	if second.Fired != 1 || second.USD < 0.399 || second.USD > 0.401 {
		t.Fatalf("the second item is %+v, wanted one firing and forty cents", second)
	}
	if len(week) != 2 {
		t.Fatalf("the week answered for %d items, wanted the two inside it", len(week))
	}
}

// A MOMENT INSIDE TODAY IS RESPECTED, not rounded out to the whole day: the
// file it lands in holds the hours on either side of it.
func TestRunsSinceSkipsWhatCameBeforeTheMoment(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	for _, entry := range []Entry{
		{At: now.Add(-4 * time.Hour), ItemID: "aaaaaaaaaaaaaaaa", Kind: string(ActionSay), USD: 1},
		{At: now.Add(-time.Hour), ItemID: "aaaaaaaaaaaaaaaa", Kind: string(ActionSay), USD: 2},
	} {
		if err := store.Append(entry); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	since, err := store.RunsSince(now.Add(-2 * time.Hour))
	if err != nil {
		t.Fatalf("runs since: %v", err)
	}
	if got := since["aaaaaaaaaaaaaaaa"]; got.Fired != 1 || got.USD != 2 {
		t.Fatalf("the answer is %+v, wanted only the line inside the window", got)
	}
}

// NOTHING TO SAY IS AN EMPTY ANSWER AND NEVER A FAILURE — a machine on which
// nothing has ever fired, and a moment in the future.
func TestRunsSinceIsEmptyWithNothingToCount(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	for _, from := range []time.Time{now.Add(-7 * 24 * time.Hour), now.Add(time.Hour), {}} {
		got, err := store.RunsSince(from)
		if err != nil {
			t.Fatalf("runs since %v: %v", from, err)
		}
		if len(got) != 0 {
			t.Fatalf("runs since %v answered %+v, wanted nothing", from, got)
		}
	}
}

func TestInboxDeliversDrainsAndIsEmptyWhenAbsent(t *testing.T) {
	sessionDir := filepath.Join(t.TempDir(), "sessions", "0123456789abcdef")

	notes, err := Drain(sessionDir)
	if err != nil {
		t.Fatalf("an absent inbox is not a failure: %v", err)
	}
	if len(notes) != 0 {
		t.Fatalf("an absent inbox answered %d notes", len(notes))
	}

	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	first := Note{At: now, ItemID: "aaaaaaaaaaaaaaaa", Words: "tell me when CI goes red", Kind: "said", Text: "CI on main is red"}
	second := Note{At: now.Add(time.Hour), ItemID: "bbbbbbbbbbbbbbbb", Words: "keep main green", Kind: "needs-you", Text: "the fix needs your look", Run: "runs/0001"}
	if err := Deliver(sessionDir, second); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if err := Deliver(sessionDir, first); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if _, err := os.Stat(InboxPath(sessionDir)); err != nil {
		t.Fatalf("the inbox was not written: %v", err)
	}

	notes, err = Drain(sessionDir)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if len(notes) != 2 {
		t.Fatalf("drain answered %d notes, wanted 2", len(notes))
	}
	if notes[0].Text != "CI on main is red" || notes[1].Kind != "needs-you" {
		t.Fatalf("drain is not oldest first: %+v", notes)
	}
	if notes[1].Run != "runs/0001" {
		t.Fatalf("a note lost the run a person can open: %+v", notes[1])
	}

	// A drained inbox is gone, and draining again is empty rather than a
	// repeat of the fold the person already read.
	if _, err := os.Stat(InboxPath(sessionDir)); !os.IsNotExist(err) {
		t.Fatalf("the inbox is still there after a drain: %v", err)
	}
	notes, err = Drain(sessionDir)
	if err != nil || len(notes) != 0 {
		t.Fatalf("a second drain answered %d notes and %v", len(notes), err)
	}
	if entries, err := os.ReadDir(sessionDir); err != nil || len(entries) != 0 {
		t.Fatalf("the drain left %v behind (%v)", entries, err)
	}
}
