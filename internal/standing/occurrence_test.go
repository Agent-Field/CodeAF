package standing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// occurrenceRunner reads the record the pass wrote BEFORE calling it, which is
// the ordering the cause contract promises, and can change the item mid-run.
type occurrenceRunner struct {
	seen   []Occurrence
	during func(Item)
	calls  int
	// withholding is the code every failed run comes back withheld with.
	withholding string
	// failing is how many of the next runs come back failed, unpublished.
	failing int
}

func (r *occurrenceRunner) Probe(context.Context, Item) (string, error) { return "", nil }
func (r *occurrenceRunner) Say(context.Context, Item, string) (Outcome, error) {
	return Outcome{}, errors.New("unexpected say")
}
func (r *occurrenceRunner) Run(_ context.Context, item Item, runDir, evidence string) (Outcome, error) {
	r.calls++
	record, err := ReadOccurrence(runDir)
	if err != nil {
		return Outcome{}, err
	}
	r.seen = append(r.seen, record)
	if r.during != nil {
		r.during(item)
	}
	if r.failing > 0 {
		r.failing--
		return Outcome{Kind: OutcomeFailed, Text: "the run was cut off before it finished", Withheld: r.withholding}, nil
	}
	return Outcome{Kind: "landed", Text: evidence, Published: &Publication{Path: item.Does.Report, SHA256: "abc", Bytes: 3}}, nil
}

// changeList is an occurrence's changes as "kind path" lines.
func changeList(occurrence Occurrence) string {
	var got []string
	for _, change := range occurrence.Changes {
		got = append(got, change.Kind+" "+change.Path)
	}
	return strings.Join(got, ",")
}

// watching is a task item watching inbox/* in its own workspace.
func watching(t *testing.T, store *Store) (Item, string) {
	t.Helper()
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	item := Item{
		Words:     "keep an inbox report",
		Workspace: workspace,
		When:      When{Kind: WhenFile, Glob: "inbox/*", Words: "when inbox/* changes"},
		Does:      Action{Kind: ActionTask, Brief: "report what changed", Report: "reports/inbox.md"},
		Rails:     Rails{MaxPerDay: 50},
	}
	made, err := store.Create(item)
	if err != nil {
		t.Fatal(err)
	}
	return made, workspace
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAFiringRecordsItsCauseBeforeItRunsAndWhatChanged(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, workspace := watchingPattern(t, store, "inbox/*", map[string]string{"inbox/old.md": "old"})
	runner := &occurrenceRunner{}
	mustTick(t, newTicker(store, runner, now)) // nothing has changed since the yes

	writeFile(t, filepath.Join(workspace, "inbox", "new.md"), "new")
	writeFile(t, filepath.Join(workspace, "inbox", "old.md"), "old, longer")
	store.clock = held(now.Add(time.Minute))
	if pass := mustTick(t, newTicker(store, runner, now.Add(time.Minute))); pass.Fired != 1 {
		t.Fatalf("the change did not fire: %+v", pass)
	}
	if len(runner.seen) != 1 || runner.seen[0].Phase != PhaseAdmitted {
		t.Fatalf("the runner did not find an admitted record before it ran: %+v", runner.seen)
	}
	seen := runner.seen[0]
	if seen.Spec != 1 || seen.Attempt != 1 || seen.Brief != "report what changed" || seen.Report != "reports/inbox.md" || seen.Trigger.Glob != "inbox/*" {
		t.Fatalf("the admitted record lost the configuration it ran on: %+v", seen)
	}
	got := []string{}
	for _, change := range seen.Changes {
		got = append(got, change.Kind+" "+change.Path)
	}
	if strings.Join(got, ",") != "added inbox/new.md,modified inbox/old.md" || seen.ChangesUnknown {
		t.Fatalf("the change list is %v (unknown=%v)", got, seen.ChangesUnknown)
	}
	records, err := store.Occurrences(made.ID, 0)
	if err != nil || len(records) != 1 {
		t.Fatalf("records %+v %v", records, err)
	}
	done := records[0]
	if done.Phase != PhaseFinished || done.Outcome != "landed" || done.Published == nil || done.Published.Path != "reports/inbox.md" {
		t.Fatalf("the finished record is %+v", done)
	}
	// A run that published carries no withheld code at all, so the record reads
	// exactly as one written before the code existed.
	if raw, _ := os.ReadFile(filepath.Join(done.RunDir, OccurrenceFile)); strings.Contains(string(raw), `"withheld"`) {
		t.Fatalf("a published run was recorded as withheld:\n%s", raw)
	}
	item, _ := store.Get(made.ID)
	if done.Reading == "" || done.Reading != item.Fingerprint || item.LastRun != done.RunDir {
		t.Fatalf("the item and its record disagree: reading %q item %q lastRun %q", done.Reading, item.Fingerprint, item.LastRun)
	}
}

// A RUN WHOSE REPORT WAS WITHHELD SAYS WHY IN ITS RECORD, as the code the
// runner answered with, beside the outcome and the line a person reads.
func TestAWithheldRunIsRecordedWithItsCode(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, workspace := watching(t, store)
	runner := &occurrenceRunner{withholding: "at-a-limit", failing: 1}
	mustTick(t, newTicker(store, runner, now)) // the baseline
	writeFile(t, filepath.Join(workspace, "inbox", "new.md"), "new")
	store.clock = held(now.Add(time.Minute))
	mustTick(t, newTicker(store, runner, now.Add(time.Minute)))
	records, err := store.Occurrences(made.ID, 0)
	if err != nil || len(records) != 1 {
		t.Fatalf("records %+v %v", records, err)
	}
	done := records[0]
	if done.Outcome != OutcomeFailed || done.Published != nil || done.Withheld != "at-a-limit" {
		t.Fatalf("the withheld run was recorded as %+v", done)
	}
	if raw, _ := os.ReadFile(filepath.Join(done.RunDir, OccurrenceFile)); !strings.Contains(string(raw), `"withheld": "at-a-limit"`) {
		t.Fatalf("occurrence.json does not carry the code:\n%s", raw)
	}
}

func TestAnInterruptedAttemptIsRetriedAsTheSameOccurrence(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, workspace := watching(t, store)
	runner := &occurrenceRunner{}
	mustTick(t, newTicker(store, runner, now))
	writeFile(t, filepath.Join(workspace, "inbox", "a.md"), "a")
	before, _ := store.Get(made.ID)

	// What a process killed mid-run leaves: an admitted record from the item's
	// current state, a dead process, and an item document that never heard.
	crashed, err := store.newRunDir(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteOccurrence(crashed, Occurrence{ID: made.ID + "/0001", ItemID: made.ID, Key: occurrenceKey(before, now), Spec: 1, Attempt: 1, Phase: PhaseAdmitted, PID: 1 << 30, Admitted: now}); err != nil {
		t.Fatal(err)
	}

	store.clock = held(now.Add(time.Minute))
	mustTick(t, newTicker(store, runner, now.Add(time.Minute)))
	records, _ := store.Occurrences(made.ID, 0)
	if len(records) != 2 {
		t.Fatalf("records: %+v", records)
	}
	retry, old := records[0], records[1]
	if retry.Attempt != 2 || len(retry.Supersedes) != 1 || retry.Supersedes[0] != crashed || len(retry.Changes) != 1 || retry.Changes[0].Path != "inbox/a.md" {
		t.Fatalf("the retry is %+v", retry)
	}
	if old.Phase != PhaseInterrupted || old.SupersededBy != retry.ID {
		t.Fatalf("the crashed attempt is %+v", old)
	}
	// And it settles: nothing left to run on the next pass.
	store.clock = held(now.Add(2 * time.Minute))
	if pass := mustTick(t, newTicker(store, runner, now.Add(2*time.Minute))); pass.Fired != 0 || runner.calls != 1 {
		t.Fatalf("the settled watch ran again: %+v calls %d", pass, runner.calls)
	}
}

func TestAFinishedButUnrecordedOccurrenceIsNotRunTwice(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, workspace := watching(t, store)
	runner := &occurrenceRunner{}
	mustTick(t, newTicker(store, runner, now))
	writeFile(t, filepath.Join(workspace, "inbox", "a.md"), "a")
	before, _ := store.Get(made.ID)
	digest, _, _, err := fingerprint(workspace, "inbox/*", nil, true)
	if err != nil {
		t.Fatal(err)
	}

	// What a process that stopped between finishing and recording leaves.
	finished, _ := store.newRunDir(made.ID)
	if err := WriteOccurrence(finished, Occurrence{ID: made.ID + "/0001", ItemID: made.ID, Key: occurrenceKey(before, now), Spec: 1, Attempt: 1, Phase: PhaseFinished, Outcome: "landed", Reading: digest, Admitted: now, Finished: now}); err != nil {
		t.Fatal(err)
	}
	store.clock = held(now.Add(time.Minute))
	pass := mustTick(t, newTicker(store, runner, now.Add(time.Minute)))
	if runner.calls != 0 || pass.Fired != 0 {
		t.Fatalf("a finished occurrence ran again: calls %d pass %+v", runner.calls, pass)
	}
	item, _ := store.Get(made.ID)
	if item.Runs != 1 || item.LastRun != finished || item.Fingerprint != digest || item.LastOutcome != "landed" {
		t.Fatalf("the recovery did not record the occurrence: %+v", item)
	}
	if records, _ := store.Occurrences(made.ID, 0); len(records) != 1 {
		t.Fatalf("recovery made a new run folder: %d records", len(records))
	}
	store.clock = held(now.Add(2 * time.Minute))
	if pass := mustTick(t, newTicker(store, runner, now.Add(2*time.Minute))); pass.Fired != 0 || runner.calls != 0 {
		t.Fatalf("the recovered watch ran: %+v", pass)
	}
}

func TestAnEditDuringARunReachesOnlyTheNextOccurrence(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, workspace := watching(t, store)
	runner := &occurrenceRunner{}
	mustTick(t, newTicker(store, runner, now))
	runner.during = func(item Item) {
		if _, _, err := store.Revise(item.ID, 1, func(it *Item) error { it.Does.Brief = "report owners"; return nil }); err != nil {
			t.Errorf("revise during run: %v", err)
		}
	}
	writeFile(t, filepath.Join(workspace, "inbox", "a.md"), "a")
	store.clock = held(now.Add(time.Minute))
	mustTick(t, newTicker(store, runner, now.Add(time.Minute)))
	runner.during = nil
	item, _ := store.Get(made.ID)
	if item.SpecRevision != 2 || item.Does.Brief != "report owners" || item.Runs != 1 {
		t.Fatalf("the runtime writeback undid the edit or lost the run: %+v", item)
	}
	writeFile(t, filepath.Join(workspace, "inbox", "b.md"), "b")
	store.clock = held(now.Add(2 * time.Minute))
	mustTick(t, newTicker(store, runner, now.Add(2*time.Minute)))
	if len(runner.seen) != 2 || runner.seen[0].Spec != 1 || runner.seen[0].Brief != "report what changed" || runner.seen[1].Spec != 2 || runner.seen[1].Brief != "report owners" {
		t.Fatalf("occurrences did not keep their own versions: %+v", runner.seen)
	}
}

func TestReviseIsFencedOnTheInstructionsVersion(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, _ := watching(t, store)
	if _, _, err := store.Revise(made.ID, 1, func(it *Item) error { return nil }); !errors.Is(err, ErrUnchanged) {
		t.Fatalf("an empty revision: %v", err)
	}
	revised, changed, err := store.Revise(made.ID, 1, func(it *Item) error { it.When.Glob = "notes/*"; return nil })
	// A NEW PATTERN'S BASELINE IS TAKEN AT THE EDIT, not left for the next pass.
	if err != nil || revised.SpecRevision != 2 || revised.Fingerprint == "" || strings.Join(changed, ",") != "what wakes it" {
		t.Fatalf("revised %+v changed %v err %v", revised, changed, err)
	}
	if _, _, err := store.Revise(made.ID, 1, func(it *Item) error { it.Words = "stale"; return nil }); !errors.Is(err, ErrConflict) {
		t.Fatalf("a stale version was accepted: %v", err)
	}
	if _, _, err := store.Revise(made.ID, 2, func(it *Item) error { it.Does.Report = "notes/x.md"; return nil }); err == nil {
		t.Fatal("a report inside its own new watch was accepted")
	}
	if _, err := store.SetStatus(made.ID, StatusRetired, StoppedWhy); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Revise(made.ID, 2, func(it *Item) error { it.Words = "late"; return nil }); err == nil {
		t.Fatal("a stopped item was revised")
	}
}

func TestAReportMustBeTheWorksOwnAndOutsideItsWatch(t *testing.T) {
	base := Item{Words: "w", Workspace: "/tmp/project", When: When{Kind: WhenFile, Glob: "inbox/*"}, Does: Action{Kind: ActionTask, Brief: "b"}, Rails: Rails{MaxPerDay: 1}}
	for report, ok := range map[string]bool{
		"reports/out.md": true, "out.md": true,
		"/tmp/out.md": false, "../out.md": false, "reports/../../out.md": false, ".": false,
		"inbox/out.md": false, " reports/out.md": false,
	} {
		item := base
		item.Does.Report = report
		if err := item.Validate(); (err == nil) != ok {
			t.Errorf("report %q: err %v, want ok=%v", report, err, ok)
		}
	}
	say := base
	say.Does = Action{Kind: ActionSay, Say: "hi", Report: "out.md"}
	if say.Validate() == nil {
		t.Error("a say item was allowed a report")
	}
}

func TestAMissingPreviousReadingIsUnknownNotNothing(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, workspace := watching(t, store)
	runner := &occurrenceRunner{}
	mustTick(t, newTicker(store, runner, now))
	if err := os.RemoveAll(store.readingsDir(made.ID)); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(workspace, "inbox", "a.md"), "a")
	store.clock = held(now.Add(time.Minute))
	mustTick(t, newTicker(store, runner, now.Add(time.Minute)))
	if len(runner.seen) != 1 || !runner.seen[0].ChangesUnknown || len(runner.seen[0].Changes) != 0 {
		t.Fatalf("a lost reading was not reported as unknown: %+v", runner.seen)
	}
}

// A FOLDER THAT COMES BACK TO A STATE IT WAS IN BEFORE IS NOT AN OLD
// OCCURRENCE. Empty, a file, empty again, a new file: the third reading equals
// the first, and a key made of the reading alone took run 0001's record for an
// unrecorded occurrence, moved the watch back, and then did the same with 0002
// on the next pass — for ever, while the new file was never reported (review
// blocker 1, 2026-09-10).
func TestAFolderThatReturnsToAnEarlierStateStillReportsTheNextChange(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, workspace := watching(t, store)
	runner := &occurrenceRunner{}
	tick := func(minutes int) Pass {
		at := now.Add(time.Duration(minutes) * time.Minute)
		store.clock = held(at)
		return mustTick(t, newTicker(store, runner, at))
	}
	tick(0) // the baseline: an empty folder
	path := filepath.Join(workspace, "inbox", "a.md")
	writeFile(t, path, "a")
	tick(1)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	tick(2)
	writeFile(t, filepath.Join(workspace, "inbox", "c.md"), "c")
	if pass := tick(3); pass.Fired != 1 || runner.calls != 3 {
		t.Fatalf("the new file after an emptied folder did not run: %+v calls %d", pass, runner.calls)
	}
	records, _ := store.Occurrences(made.ID, 0)
	if len(records) != 3 || changeList(records[0]) != "added inbox/c.md" || changeList(records[1]) != "removed inbox/a.md" {
		t.Fatalf("records: %d, newest %q, before it %q", len(records), changeList(records[0]), changeList(records[1]))
	}
	for _, record := range records {
		if record.Attempt != 1 || len(record.Supersedes) != 0 {
			t.Fatalf("an old occurrence was counted as an earlier attempt: %+v", record)
		}
	}
	// And it settles, with the count and the dates moving forward only.
	item, _ := store.Get(made.ID)
	if pass := tick(4); pass.Fired != 0 || runner.calls != 3 {
		t.Fatalf("the settled watch ran again: %+v", pass)
	}
	after, _ := store.Get(made.ID)
	if item.Runs != 3 || after.Runs != 3 || after.LastFired.Before(item.LastFired) || after.LastRun != records[0].RunDir {
		t.Fatalf("the item moved backwards: before %d %v, after %d %v %s", item.Runs, item.LastFired, after.Runs, after.LastFired, after.LastRun)
	}
}

// A RUN THAT PUBLISHED AND THEN DIED IS NOT RUN AGAIN. The receipt is written
// the moment the report exists; a process stopped after that and before the
// record said finished must not pay for the work twice, rewrite the report or
// deliver a second note (review issue 4).
func TestAnOccurrenceThatPublishedBeforeItsProcessDiedIsRecordedNotRerun(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, workspace := watching(t, store)
	runner := &occurrenceRunner{}
	mustTick(t, newTicker(store, runner, now))
	writeFile(t, filepath.Join(workspace, "inbox", "a.md"), "a")
	before, _ := store.Get(made.ID)
	digest, _, _, err := fingerprint(workspace, "inbox/*", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	published, _ := store.newRunDir(made.ID)
	receipt := &Publication{Path: "reports/inbox.md", SHA256: "abc", Bytes: 3, At: now}
	if err := WriteOccurrence(published, Occurrence{ID: made.ID + "/0001", ItemID: made.ID, Key: occurrenceKey(before, now), Spec: 1, Attempt: 1, Phase: PhaseAdmitted, PID: 1 << 30, Admitted: now, Reading: digest, Published: receipt}); err != nil {
		t.Fatal(err)
	}
	store.clock = held(now.Add(time.Minute))
	if pass := mustTick(t, newTicker(store, runner, now.Add(time.Minute))); runner.calls != 0 || pass.Fired != 0 {
		t.Fatalf("a published occurrence ran again: calls %d pass %+v", runner.calls, pass)
	}
	records, _ := store.Occurrences(made.ID, 0)
	if len(records) != 1 || records[0].Phase != PhaseFinished || records[0].Outcome != "landed" || records[0].Published == nil || !strings.Contains(records[0].OutcomeText, "whether its note was delivered, and what it cost, is not known") {
		t.Fatalf("the published record was not recovered as landed: %+v", records)
	}
	item, _ := store.Get(made.ID)
	if item.Runs != 1 || item.Fingerprint != digest || item.LastOutcome != "landed" {
		t.Fatalf("the item did not record it: %+v", item)
	}
}

// THE CHANGES A FAILED RUN NEVER REPORTED ARE LISTED AGAIN. The watch moves on
// at every look, so the run after a failure used to be told only about what
// changed since the failure.
func TestTheRunAfterAFailedOneIsToldWhatTheFailedOneMissed(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, workspace := watching(t, store)
	runner := &occurrenceRunner{failing: 1}
	tick := func(minutes int) Pass {
		at := now.Add(time.Duration(minutes) * time.Minute)
		store.clock = held(at)
		return mustTick(t, newTicker(store, runner, at))
	}
	tick(0)
	writeFile(t, filepath.Join(workspace, "inbox", "a.md"), "a")
	if pass := tick(1); pass.Failed != 1 || len(pass.Notes) == 0 {
		t.Fatalf("a failed run was not counted and said: %+v", pass)
	}
	tick(2) // nothing changed; the failed run's baseline must survive this
	writeFile(t, filepath.Join(workspace, "inbox", "b.md"), "b")
	tick(3)
	records, _ := store.Occurrences(made.ID, 0)
	if len(records) != 2 || changeList(records[0]) != "added inbox/a.md,added inbox/b.md" || records[0].Since != records[1].Since {
		t.Fatalf("the run after a failure was told %q (since %q, failed since %q)", changeList(records[0]), records[0].Since, records[1].Since)
	}
	// Once one lands, the list is measured from its reading again.
	writeFile(t, filepath.Join(workspace, "inbox", "c.md"), "c")
	tick(4)
	records, _ = store.Occurrences(made.ID, 0)
	if changeList(records[0]) != "added inbox/c.md" {
		t.Fatalf("after a landed run the list reached back: %q", changeList(records[0]))
	}
}

// AN EDIT OF THE REPORT PATH ALONE IS AN EDIT (review blocker 2), and a
// report landing in a folder the watch matches is refused, because publishing
// moves that folder's modification time and the watch would read its own
// report as a change (review issue 3).
func TestTheReportPathIsPartOfTheInstructionsAndStaysOutOfWatchedFolders(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, _ := watching(t, store)
	revised, changed, err := store.Revise(made.ID, 1, func(it *Item) error { it.Does.Report = "reports/weekly.md"; return nil })
	if err != nil || strings.Join(changed, ",") != "report" || revised.Does.Report != "reports/weekly.md" || revised.SpecRevision != 2 {
		t.Fatalf("a report-only edit: %v %v %+v", changed, err, revised.Does)
	}
	base := Item{Words: "w", Workspace: "/tmp/project", Does: Action{Kind: ActionTask, Brief: "b"}, Rails: Rails{MaxPerDay: 1}}
	for _, c := range []struct {
		glob, report string
		ok           bool
	}{
		{"*", "reports/weekly.md", false},
		{"notes/*", "notes/out/summary.md", false},
		{"reports", "reports/x.md", false},
		{"inbox/*", "reports/x.md", true},
		{"inbox/*.md", "inbox-report.md", true},
	} {
		item := base
		item.When = When{Kind: WhenFile, Glob: c.glob}
		item.Does.Report = c.report
		if err := item.validateReport(); (err == nil) != c.ok {
			t.Errorf("watch %q report %q: err %v, want ok=%v", c.glob, c.report, err, c.ok)
		}
	}
}

// F7, PAST TEN THOUSAND. The run folders were read newest first by a string
// sort, which put "9999" ahead of "10000": from the ten-thousandth run on, the
// recovery window read the oldest records as the newest, and a finished
// occurrence it should have found was run again.
func TestRunsPastTenThousandAreStillReadNewestFirst(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, _ := watching(t, store)
	for _, name := range []string{"9998", "9999", "10000", "10001"} {
		runDir := filepath.Join(store.RunsDir(made.ID), name)
		if err := os.MkdirAll(runDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := WriteOccurrence(runDir, Occurrence{ID: made.ID + "/" + name, ItemID: made.ID, Phase: PhaseFinished}); err != nil {
			t.Fatal(err)
		}
	}
	records, err := store.Occurrences(made.ID, 2)
	if err != nil || len(records) != 2 {
		t.Fatalf("records %+v (%v)", records, err)
	}
	if filepath.Base(records[0].RunDir) != "10001" || filepath.Base(records[1].RunDir) != "10000" {
		t.Fatalf("newest first read %s, %s", filepath.Base(records[0].RunDir), filepath.Base(records[1].RunDir))
	}
}

// F7, A NUMBER MINTED ONCE. The next run took the highest folder on disk plus
// one, so when the sweep reaped the newest run for coming to nothing, the next
// firing was handed its number — and every reference to the reaped run now
// named a different one.
func TestARunNumberIsNeverHandedOutTwice(t *testing.T) {
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, _ := watching(t, store)
	var minted []string
	for range 3 {
		runDir, err := store.newRunDir(made.ID)
		if err != nil {
			t.Fatal(err)
		}
		minted = append(minted, runDir)
	}
	// The sweep reaps the newest, which came to nothing.
	if err := os.RemoveAll(minted[2]); err != nil {
		t.Fatal(err)
	}
	next, err := store.newRunDir(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, earlier := range minted {
		if filepath.Base(next) == filepath.Base(earlier) {
			t.Fatalf("the next run was handed %s, a number already minted", filepath.Base(next))
		}
	}
}
