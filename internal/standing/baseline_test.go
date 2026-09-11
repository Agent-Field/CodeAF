package standing

// baseline_test.go is ruling R10 of wave 5: a file watch takes its baseline AT
// THE YES. It used to take it on its first pass, silently, so a file that
// changed between the yes and that pass — up to five minutes, or until a window
// next opened — was folded into the baseline and never reported.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// supportWatch is a say item watching support/*, with one old ticket already
// there when it is agreed to.
func supportWatch(t *testing.T, store *Store) (Item, string) {
	t.Helper()
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "support"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(workspace, "support", "old.md"), "an old ticket\n")
	made, err := store.Create(Item{
		Words:     "tell me here when a new ticket comes in",
		Workspace: workspace,
		When:      When{Kind: WhenFile, Glob: "support/*"},
		Does:      Action{Kind: ActionSay, Say: "New ticket"},
		Rails:     Rails{MaxPerDay: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	return made, workspace
}

// A CHANGE BEFORE THE FIRST PASS IS REPORTED BY IT. The ticket that lands a
// minute after the yes is the first thing the person asked to hear about.
func TestAChangeBeforeTheFirstPassIsNotSwallowed(t *testing.T) {
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	_, workspace := supportWatch(t, store)
	writeFile(t, filepath.Join(workspace, "support", "t1.md"), "Ticket 1\n")

	later := now.Add(5 * time.Minute)
	store.clock = held(later)
	runner := &fakeRunner{}
	if pass := mustTick(t, newTicker(store, runner, later)); pass.Said != 1 {
		t.Fatalf("the ticket that landed before the first pass was swallowed: %+v", pass)
	}
}

// NOTHING THAT WAS THERE AT THE YES IS NEWS. The first pass over an unchanged
// folder says nothing, exactly as the old silent baseline did.
func TestTheFirstPassOverAnUnchangedWatchIsQuiet(t *testing.T) {
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	supportWatch(t, store)
	later := now.Add(5 * time.Minute)
	store.clock = held(later)
	runner := &fakeRunner{}
	if pass := mustTick(t, newTicker(store, runner, later)); pass.Fired != 0 {
		t.Fatalf("the files that were there at the yes were reported: %+v %v", pass, runner.said)
	}
}

// AN EDIT TO WHAT IT WATCHES IS A YES TOO. A new pattern's baseline is taken
// at the edit, so a change before the next pass is reported.
func TestAChangeAfterAWatchEditIsNotSwallowed(t *testing.T) {
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, workspace := supportWatch(t, store)
	if err := os.MkdirAll(filepath.Join(workspace, "tickets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Revise(made.ID, made.SpecRevision, func(item *Item) error {
		item.When.Glob = "tickets/*"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(workspace, "tickets", "t1.md"), "Ticket 1\n")
	later := now.Add(5 * time.Minute)
	store.clock = held(later)
	runner := &fakeRunner{}
	if pass := mustTick(t, newTicker(store, runner, later)); pass.Said != 1 {
		t.Fatalf("the ticket that landed after the edit was swallowed: %+v", pass)
	}
}

// A WATCH MADE BEFORE THE YES TOOK BASELINES STILL TAKES ONE QUIETLY. Its
// document names no reading, so its first pass reads the way things are and
// says nothing, as every watch's first pass used to.
func TestAWatchWithNoBaselineTakesOneQuietly(t *testing.T) {
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, workspace := supportWatch(t, store)
	made.Fingerprint = ""
	if err := store.Save(made); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(workspace, "support", "t1.md"), "Ticket 1\n")
	runner := &fakeRunner{}
	if pass := mustTick(t, newTicker(store, runner, now)); pass.Fired != 0 {
		t.Fatalf("a watch with no baseline spoke on its first pass: %+v", pass)
	}
	if back, _ := store.Get(made.ID); back.LastCheckLine != "nothing has changed yet" || back.Fingerprint == "" {
		t.Fatalf("the first pass kept no baseline: %q %q", back.LastCheckLine, back.Fingerprint)
	}
}

// AN EDIT DURING A PASS KEEPS THE READING IT NAMES (wave 5 review, blocker 1).
// The pass checks the item's revision once, before it walks; an edit after that
// check takes a new baseline and names it; the pass then tidied away every
// reading but its own and the one it started from — the one the item now names
// included. The next pass could not load it, read the folder as changed in
// unknown ways, and fired, billed, on a folder where nothing had changed. The
// four steps are the reviewer's replication, in order.
func TestAnEditDuringAPassKeepsTheReadingItNames(t *testing.T) {
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, workspace := supportWatch(t, store)
	if err := os.MkdirAll(filepath.Join(workspace, "tickets"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(workspace, "tickets", "old.md"), "an old ticket\n")

	// The in-flight pass has read the old pattern...
	digest, _, files, err := fingerprint(workspace, "support/*", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	// ...the person edits what it watches...
	if _, _, err := store.Revise(made.ID, made.SpecRevision, func(item *Item) error {
		item.When.Glob = "tickets/*"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// ...and the pass keeps its reading and tidies the rest, as the pass does.
	store.keepReading(made.ID, digest, files, true, made.Fingerprint)

	later := now.Add(5 * time.Minute)
	store.clock = held(later)
	runner := &fakeRunner{}
	if pass := mustTick(t, newTicker(store, runner, later)); pass.Fired != 0 {
		t.Fatalf("a folder where nothing changed fired after an edit met a pass: %+v %q", pass, runner.said)
	}
}

// A TOUCH BETWEEN THE YES AND THE FIRST PASS COUNTS AS A CHANGE. The baseline
// states and never reads, so it has no contents to compare a rewrite with;
// this pins that cost of an unread baseline, which the manual states.
func TestATouchBeforeTheFirstPassCountsAsAChange(t *testing.T) {
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	_, workspace := supportWatch(t, store)
	old := filepath.Join(workspace, "support", "old.md")
	touched := time.Now().Add(time.Hour)
	if err := os.Chtimes(old, touched, touched); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if pass := mustTick(t, newTicker(store, runner, now)); pass.Said != 1 {
		t.Fatalf("a touch in the gap was not counted, which the manual says it is: %+v", pass)
	}
}
