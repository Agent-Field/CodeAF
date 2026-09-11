package standing

// filewatch_test.go is validator S02 and the touch case scripted: `**` read as
// `*`, so an edit two folders down woke nothing; and a file whose time moved
// with its contents unchanged woke a paid run.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// watchingPattern is a task item watching glob in its own workspace, with its
// report outside it.
func watchingPattern(t *testing.T, store *Store, glob string) (Item, string) {
	t.Helper()
	workspace := t.TempDir()
	made, err := store.Create(Item{
		Words:     "keep an inbox report",
		Workspace: workspace,
		When:      When{Kind: WhenFile, Glob: glob, Words: "when " + glob + " changes"},
		Does:      Action{Kind: ActionTask, Brief: "report what changed", Report: "reports/inbox.md"},
		Rails:     Rails{MaxPerDay: 50},
	})
	if err != nil {
		t.Fatal(err)
	}
	return made, workspace
}

func writeNested(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, text)
}

// A NESTED EDIT WAKES A RECURSIVE WATCH. `inbox/**/*.md` reaches every folder
// under inbox, however deep, and only its Markdown.
func TestANestedEditWakesARecursiveWatch(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, workspace := watchingPattern(t, store, "inbox/**/*.md")
	deep := filepath.Join(workspace, "inbox", "2026", "09", "a.md")
	writeNested(t, deep, "first")
	writeNested(t, filepath.Join(workspace, "inbox", "top.md"), "top")
	writeNested(t, filepath.Join(workspace, "inbox", "2026", "notes.txt"), "not watched")
	runner := &occurrenceRunner{}
	mustTick(t, newTicker(store, runner, now)) // the baseline

	writeFile(t, deep, "first, edited")
	writeFile(t, filepath.Join(workspace, "inbox", "2026", "notes.txt"), "not watched, edited")
	store.clock = held(now.Add(time.Minute))
	if pass := mustTick(t, newTicker(store, runner, now.Add(time.Minute))); pass.Fired != 1 || len(runner.seen) != 1 {
		t.Fatalf("an edit two folders down did not wake the watch: %+v", pass)
	}
	if got := changeList(runner.seen[0]); got != "modified inbox/2026/09/a.md" {
		t.Fatalf("the change list is %q", got)
	}
	item, _ := store.Get(made.ID)
	if item.Fingerprint == "" {
		t.Fatalf("the watch kept no reading")
	}
}

// A WATCH PAST ITS LIMIT IS REFUSED WHEN IT IS SET UP, and when an edit moves
// it there, with the one line that says what the limit is and why.
func TestAWatchPastItsLimitIsRefusedAtSetup(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	workspace := t.TempDir()
	for folder := 0; folder <= WatchLimit/100; folder++ {
		dir := filepath.Join(workspace, "inbox", fmt.Sprintf("%03d", folder))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for file := range 100 {
			writeFile(t, filepath.Join(dir, fmt.Sprintf("%03d.md", file)), "x")
		}
	}
	item := Item{
		Words:     "keep an inbox report",
		Workspace: workspace,
		When:      When{Kind: WhenFile, Glob: "inbox/**", Words: "when inbox/** changes"},
		Does:      Action{Kind: ActionTask, Brief: "report what changed", Report: "reports/inbox.md"},
		Rails:     Rails{MaxPerDay: 50},
	}
	_, err := store.Create(item)
	if !errors.As(err, new(WatchTooLarge)) || !strings.HasPrefix(err.Error(), fmt.Sprintf("inbox/** reaches more than %d files and folders", WatchLimit)) {
		t.Fatalf("a watch past its limit was set up: %v", err)
	}
	if items, _ := store.List(); len(items) != 0 {
		t.Fatalf("the refused watch was written: %+v", items)
	}

	item.When = When{Kind: WhenFile, Glob: "inbox/000/*", Words: "when inbox/000/* changes"}
	made, err := store.Create(item)
	if err != nil {
		t.Fatalf("a watch inside its limit was refused: %v", err)
	}
	_, _, err = store.Revise(made.ID, made.SpecRevision, func(it *Item) error {
		it.When = When{Kind: WhenFile, Glob: "inbox/**/*.md", Words: "when inbox/**/*.md changes"}
		return nil
	})
	if !errors.As(err, new(WatchTooLarge)) {
		t.Fatalf("an edit moved a watch past its limit: %v", err)
	}
}

// A TOUCH, OR A REWRITE WITH THE SAME TEXT, IS NOT A CHANGE; an edit that keeps
// the size and changes the text is.
func TestATouchedOrIdenticallyRewrittenFileIsNotAChange(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, workspace := watchingPattern(t, store, "inbox/*")
	file := filepath.Join(workspace, "inbox", "a.md")
	writeNested(t, file, "ship Friday")
	runner := &occurrenceRunner{}
	mustTick(t, newTicker(store, runner, now)) // the baseline

	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(file, later, later); err != nil {
		t.Fatal(err)
	}
	store.clock = held(now.Add(time.Minute))
	if pass := mustTick(t, newTicker(store, runner, now.Add(time.Minute))); pass.Fired != 0 {
		t.Fatalf("a touched file woke a run: %+v", pass)
	}
	if item, _ := store.Get(made.ID); item.LastCheckLine != "nothing has changed" {
		t.Fatalf("the touched watch said %q", item.LastCheckLine)
	}

	writeFile(t, file, "ship Friday")
	evenLater := later.Add(time.Hour)
	if err := os.Chtimes(file, evenLater, evenLater); err != nil {
		t.Fatal(err)
	}
	store.clock = held(now.Add(2 * time.Minute))
	if pass := mustTick(t, newTicker(store, runner, now.Add(2*time.Minute))); pass.Fired != 0 {
		t.Fatalf("an identical rewrite woke a run: %+v", pass)
	}

	writeFile(t, file, "ship Monday")
	store.clock = held(now.Add(3 * time.Minute))
	if pass := mustTick(t, newTicker(store, runner, now.Add(3*time.Minute))); pass.Fired != 1 || len(runner.seen) != 1 {
		t.Fatalf("a real edit of the same size did not wake a run: %+v", pass)
	}
	if got := changeList(runner.seen[0]); got != "modified inbox/a.md" {
		t.Fatalf("the change list is %q", got)
	}
}

// A READING KEPT BEFORE CONTENTS WERE HASHED IS QUIET. Its digest is of times,
// this build's is of contents; the files did not move, so nothing runs.
func TestAReadingKeptBeforeContentsWereHashedIsQuiet(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, workspace := watchingPattern(t, store, "inbox/*")
	file := filepath.Join(workspace, "inbox", "a.md")
	writeNested(t, file, "ship Friday")
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	old := map[string]fileEntry{"inbox/a.md": {Size: info.Size(), MTime: info.ModTime().UnixNano()}}
	store.keepReading(made.ID, "0ld", old, false)
	made.Fingerprint = "0ld"
	if err := store.Save(made); err != nil {
		t.Fatal(err)
	}
	runner := &occurrenceRunner{}
	if pass := mustTick(t, newTicker(store, runner, now)); pass.Fired != 0 {
		t.Fatalf("an older reading of unchanged files woke a run: %+v", pass)
	}
	writeFile(t, file, "ship Monday")
	store.clock = held(now.Add(time.Minute))
	if pass := mustTick(t, newTicker(store, runner, now.Add(time.Minute))); pass.Fired != 1 {
		t.Fatalf("an edit after the older reading did not wake a run: %+v", pass)
	}
}

// A REPORT DEEP INSIDE A RECURSIVE WATCH IS REFUSED: `**` reaches it, so every
// report would wake the watch again.
func TestAReportDeepInsideARecursiveWatchIsRefused(t *testing.T) {
	item := Item{
		Words:     "keep a notes report",
		Workspace: "/tmp/project",
		When:      When{Kind: WhenFile, Glob: "**/*.md", Words: "when **/*.md changes"},
		Does:      Action{Kind: ActionTask, Brief: "report what changed", Report: "docs/reports/notes.md"},
		Rails:     Rails{MaxPerDay: 50},
	}
	if err := item.Validate(); err == nil || !strings.Contains(err.Error(), "one of the files it watches") {
		t.Fatalf("a report deep inside a recursive watch was admitted: %v", err)
	}
	item.When.Glob = "inbox/**/*.md"
	if err := item.Validate(); err != nil {
		t.Fatalf("a report outside the watch was refused: %v", err)
	}
}

// AN ITEM ANSWERS WHAT ITS WATCH REACHES BY THE PASS'S OWN READING, recursive
// and absolute patterns included, and a waking that is not a file watch reaches
// nothing.
func TestAnItemWatchesWhatItsPassReads(t *testing.T) {
	item := Item{Workspace: "/tmp/project", When: When{Kind: WhenFile, Glob: "inbox/**/*.md"}}
	for path, want := range map[string]bool{
		"inbox/today.md": true, "inbox/a/b/today.md": true, "reports/inbox-report.md": false, "inbox/today.txt": false,
	} {
		if got := item.Watches(path); got != want {
			t.Errorf("Watches(%q) = %v, want %v", path, got, want)
		}
	}
	item.When.Glob = "/tmp/project/inbox/*"
	if !item.Watches("inbox/today.md") || item.Watches("reports/today.md") {
		t.Error("an absolute pattern is not read against the workspace")
	}
	item.When = When{Kind: WhenEvery, Every: "0 9 * * *"}
	if item.Watches("inbox/today.md") {
		t.Error("a rhythm claims to watch a file")
	}
}
