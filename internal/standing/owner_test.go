package standing

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ownedItem is a file watch in workspace that keeps report current.
func ownedItem(workspace, report string) Item {
	return Item{
		Words: "keep " + report + " current", Workspace: workspace,
		When:  When{Kind: WhenFile, Glob: "inbox/*"},
		Does:  Action{Kind: ActionTask, Brief: "Summarise inbox/.", Report: report},
		Rails: Rails{MaxPerDay: DefaultMaxPerDay, PerRunUSD: DefaultPerRunUSD},
	}
}

// A REPORT PATH HAS ONE LIVE OWNER, kept on its own record: a second item is
// refused as [ErrReportOwned], the owner's stop frees the path, and the record
// goes with the stop.
func TestAReportPathKeepsOneLiveOwnerOnItsRecord(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	first, err := store.Create(ownedItem(workspace, "reports/r.md"))
	if err != nil {
		t.Fatal(err)
	}
	path := ReportPath(first)
	if owner, live := store.LiveOwner(path); !live || owner.ID != first.ID {
		t.Fatalf("the path's owner = %+v (live %v), want %s", owner, live, first.ID)
	}
	var owned *ReportOwnedError
	if _, err := store.Create(ownedItem(workspace, "reports/./r.md")); !errors.Is(err, ErrReportOwned) || !errors.As(err, &owned) || owned.Owner.ID != first.ID {
		t.Fatalf("a second owner spelled another way was not refused as the first's: %v", err)
	}
	if _, err := store.SetStatus(first.ID, StatusRetired, StoppedWhy); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.pathRecord(path, ".owner.json")); !os.IsNotExist(err) {
		t.Fatalf("a stopped owner's record was kept (%v)", err)
	}
	second, err := store.Create(ownedItem(workspace, "reports/r.md"))
	if err != nil {
		t.Fatalf("a successor after the stop was refused: %v", err)
	}
	moved, _, err := store.Revise(second.ID, second.SpecRevision, func(item *Item) error {
		item.Does.Report = "reports/elsewhere.md"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, live := store.LiveOwner(path); live {
		t.Fatal("a report moved elsewhere kept its claim on the old path")
	}
	if owner, live := store.LiveOwner(ReportPath(moved)); !live || owner.ID != second.ID {
		t.Fatalf("the moved report's path is not its: %+v", owner)
	}
}

// THE OWNERS ALREADY THERE ARE RECORDED ONCE, and the one that published keeps
// the path: two live items from before the record, one of which published
// under its own receipt, and a third that is stopped.
func TestTheOwnersBeforeTheRecordAreRecordedOnce(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	older, newer := ownedItem(workspace, "reports/r.md"), ownedItem(workspace, "reports/r.md")
	older.ID, newer.ID = "0000000000000001", "0000000000000002"
	older.Status, newer.Status = StatusActive, StatusActive
	older.Created, newer.Created = store.now().Add(-2), store.now().Add(-1)
	for _, item := range []Item{older, newer} {
		if err := store.writeUnlocked(item); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(store.ItemDir(newer.ID), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.ItemDir(newer.ID), publicationsFile), []byte(`{"reports/r.md":{"path":"reports/r.md","sha256":"newer","bytes":5}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	path := ReportPath(older)
	if owner, live := store.LiveOwner(path); !live || owner.ID != newer.ID {
		t.Fatalf("the recorded owner = %+v, want the one that published (%s)", owner, newer.ID)
	}
	if receipt, err := store.Receipt(path); err != nil || receipt == nil || receipt.SHA256 != "newer" {
		t.Fatalf("its receipt was not moved to the path: %+v (%v)", receipt, err)
	}
	if _, err := os.Stat(filepath.Join(store.root, receiptsDir, ownersMarker)); err != nil {
		t.Fatalf("the one-time record left no marker: %v", err)
	}
}

// A WATCH DOES NOT EXPAND BRACES. The live one-path run of 2026-09-11 edited a
// watch to `{inbox/*,notes/*}` to add a folder; the matcher reads braces as
// the characters, so the watch matched nothing and never fired, silently. It
// is refused where both doors ask, naming what to do instead.
func TestAWatchWithBracesIsRefused(t *testing.T) {
	item := ownedItem(t.TempDir(), "reports/r.md")
	item.When.Glob = "{inbox/*,notes/*}"
	if err := item.CheckWatch(); err == nil || !strings.Contains(err.Error(), "braces") {
		t.Fatalf("a watch with braces was taken: %v", err)
	}
}

// ── the second review of the one-owner round (cb53c18da) ────────────────────

// THE REFUSAL ASKS, IT NEVER SENDS THE MODEL TO A STOP. "edit that one, or stop
// it first" was read as an instruction: in the live one-path run the model
// stopped the person's order twice, and nothing kept the file after it. A stop
// is permanent and nobody asked for one.
func TestTheOwnerRefusalAsksRatherThanStops(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	if _, err := store.Create(ownedItem(workspace, "reports/r.md")); err != nil {
		t.Fatal(err)
	}
	_, err = store.Create(ownedItem(workspace, "reports/r.md"))
	if err == nil || strings.Contains(err.Error(), "stop it first") || !strings.Contains(err.Error(), "edit that one to cover this, or ask the person — a stop is permanent") {
		t.Fatalf("the refusal = %v", err)
	}
}

// A STALE OWNER RECORD IS NOT AN OWNER. A crash between a revision and the
// release of the path it left, or a restore that failed, leaves a record naming
// an item whose report is elsewhere now; that path is free. And a release asked
// for an item that keeps the path again is not made.
func TestAStaleOwnerRecordIsNotAnOwner(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	first, err := store.Create(ownedItem(workspace, "reports/r.md"))
	if err != nil {
		t.Fatal(err)
	}
	path := ReportPath(first)
	store.releaseReport(path, first.ID)
	if _, live := store.LiveOwner(path); !live {
		t.Fatal("a release asked for an item that still keeps the path was made")
	}
	moved, _, err := store.Revise(first.ID, first.SpecRevision, func(item *Item) error {
		item.Does.Report = "reports/elsewhere.md"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.keepOwner(path, moved.ID); err != nil { // the crash's leftover
		t.Fatal(err)
	}
	if owner, live := store.LiveOwner(path); live {
		t.Fatalf("a record naming %s, whose report is elsewhere, still owns the path", owner.ID)
	}
	if _, err := store.Create(ownedItem(workspace, "reports/r.md")); err != nil {
		t.Fatalf("a free path was refused on a stale record: %v", err)
	}
}

// THE ONE-TIME RECORD FINISHES WHATEVER IT SKIPS. An unreadable item, or a path
// whose receipt cannot be read, used to leave the marker unwritten, so every
// guarded write read every item again. It records what it can, writes the
// marker with what it skipped, and never reads every item again.
func TestTheOneTimeRecordFinishesWhateverItSkips(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	good, blocked := ownedItem(workspace, "reports/r.md"), ownedItem(workspace, "reports/blocked.md")
	good.ID, blocked.ID = "0000000000000001", "0000000000000002"
	good.Status, blocked.Status = StatusActive, StatusActive
	for _, item := range []Item{good, blocked} {
		if err := store.writeUnlocked(item); err != nil {
			t.Fatal(err)
		}
	}
	const unreadable = "0000000000000009"
	if err := os.WriteFile(store.ItemPath(unreadable), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	receipt := store.receiptFile(ReportPath(blocked))
	if err := os.MkdirAll(filepath.Dir(receipt), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(receipt, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if owner, live := store.LiveOwner(ReportPath(good)); !live || owner.ID != good.ID {
		t.Fatalf("the readable item was not recorded: %+v", owner)
	}
	raw, err := os.ReadFile(filepath.Join(store.root, receiptsDir, ownersMarker))
	if err != nil {
		t.Fatalf("a record that skipped something left no marker: %v", err)
	}
	for _, want := range []string{`"skipped": 2`, unreadable, ReportPath(blocked)} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("the marker does not say %q:\n%s", want, raw)
		}
	}
	before := ListReads()
	store.LiveOwner(ReportPath(good))
	if n := ListReads() - before; n != 0 {
		t.Fatalf("after the marker a lookup read every item %d time(s)", n)
	}
}

// A PATTERN WITH BRACES IS REFUSED AT SETUP, NEVER AT EVERY CHECK. The refusal
// was in the reader the ticker uses too, so an item made before it failed every
// check with a line written for the model. It is a watch that matches nothing,
// said once in its log.
func TestAnOldBracePatternIsAQuietWatchNotAFailure(t *testing.T) {
	now := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	item := ownedItem(t.TempDir(), "reports/r.md")
	item.ID, item.Status, item.Created = "0000000000000003", StatusActive, now
	item.When.Glob = "{inbox/*,notes/*}"
	if err := item.CheckWatch(); err == nil || !strings.Contains(err.Error(), "braces") {
		t.Fatalf("a brace pattern was taken at setup: %v", err)
	}
	if err := store.writeUnlocked(item); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		pass := mustTick(t, newTicker(store, &fakeRunner{}, now))
		if pass.Errors != 0 || pass.Fired != 0 {
			t.Fatalf("an old brace watch failed or fired: %+v", pass)
		}
	}
	after, err := store.Get(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.FailedChecks != 0 || !strings.Contains(after.LastCheckLine, "braces") {
		t.Fatalf("the watch's check = %q (failed %d)", after.LastCheckLine, after.FailedChecks)
	}
	log, _ := os.ReadFile(store.LogPath(item.ID))
	if n := strings.Count(string(log), "braces"); n != 1 {
		t.Fatalf("the log says it %d times:\n%s", n, log)
	}
}

// RETIRING BY EXPIRY OR BY A ONE-OFF FIRING RELEASES THE PATH, as a stop does.
func TestRetiringOnItsOwnReleasesTheReportPath(t *testing.T) {
	now := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	workspace := t.TempDir()
	timed := ownedItem(workspace, "reports/timed.md")
	timed.When = When{Kind: WhenEvery, Every: "1m"}
	timed.Rails.Expires = now.Add(time.Minute)
	expiring, err := store.Create(timed)
	if err != nil {
		t.Fatal(err)
	}
	once := ownedItem(workspace, "reports/once.md")
	once.When = When{Kind: WhenAt, At: now.Add(time.Minute)}
	oneOff, err := store.Create(once)
	if err != nil {
		t.Fatal(err)
	}
	later := now.Add(2 * time.Minute)
	store.clock = held(later)
	mustTick(t, newTicker(store, &fakeRunner{}, later))
	for _, item := range []Item{expiring, oneOff} {
		current, err := store.Get(item.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status != StatusRetired {
			t.Fatalf("%s did not retire: %q", item.Words, current.Status)
		}
		if _, err := os.Stat(store.pathRecord(ReportPath(item), ".owner.json")); !os.IsNotExist(err) {
			t.Errorf("%s retired (%s) and kept its owner record (%v)", item.Words, current.RetiredWhy, err)
		}
	}
}
