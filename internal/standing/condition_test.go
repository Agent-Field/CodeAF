package standing

// condition_test.go is rulings R4 and R8 of wave 5, scripted from the chat
// scoreboard's nested and two-reviews cases (2026-09-11):
//
//   - a file watch carrying `when.hint` asked the sentinel with NO evidence, so
//     it answered "no changes" while the changes were on disk and the watch
//     could never fire;
//   - a hint on a kind that gathers nothing was accepted, and judged on words;
//   - a say line carried a literal `{{file}}` nothing fills;
//   - a say ping was the raw reading — "WHAT CHANGED SINCE THE LAST READING: …
//     ALL MATCHING FILES: …" — rather than a line a person reads.
//
// They use only the store, the pass and CheckWatch, so each one runs unchanged
// against the logic it was written to catch.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// clientsWatch is the nested case's item as the chat proposed it: a recursive
// watch over the client folders, with the model's condition on it.
func clientsWatch(t *testing.T, store *Store) (Item, string) {
	t.Helper()
	workspace := t.TempDir()
	writeNested(t, filepath.Join(workspace, "inbox", "clients", "acme", "thread.md"), "Acme: can we move the review to Friday?\n")
	writeNested(t, filepath.Join(workspace, "inbox", "clients", "beta", "thread.md"), "Beta: invoice received, thanks.\n")
	made, err := store.Create(Item{
		Words:     "keep an eye on my inbox folder — each client has its own subfolder in there — and keep reports/clients.md current with what each client is waiting on",
		Workspace: workspace,
		When: When{Kind: WhenFile, Glob: "inbox/clients/**/*", Words: "whenever a file changes inside inbox/clients/",
			Hint: "any .md file change inside a client subfolder"},
		Does:  Action{Kind: ActionTask, Brief: "summarise what each client is waiting on", Report: "reports/clients.md"},
		Rails: Rails{MaxPerDay: 50},
	})
	if err != nil {
		t.Fatal(err)
	}
	return made, workspace
}

// evidenceJudge says yes only when the evidence names every fragment, and
// keeps what it was shown — a sentinel that needs to see the change to say so.
func evidenceJudge(fragments ...string) (Sentinel, *[]string) {
	var shown []string
	return func(_ context.Context, judgment Judgment) (bool, string, float64, error) {
		shown = append(shown, judgment.Evidence)
		for _, fragment := range fragments {
			if !strings.Contains(judgment.Evidence, fragment) {
				return false, "no .md file changed in any client subfolder", 0, nil
			}
		}
		return true, "two client threads changed", 0, nil
	}, &shown
}

// A CONDITIONED FILE WATCH IS JUDGED ON WHAT CHANGED. The nested case: the
// Acme thread grows and a new client's folder appears; the condition is asked
// with the change list, the sizes and how the changed files now read, and the
// watch fires.
func TestAConditionedFileWatchIsJudgedOnWhatChanged(t *testing.T) {
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	_, workspace := clientsWatch(t, store)
	sentinel, shown := evidenceJudge("inbox/clients/acme/thread.md", "inbox/clients/gamma/thread.md", "a quote for the new site")
	runner := &occurrenceRunner{}
	ticker := newTicker(store, runner, now)
	ticker.Sentinel = sentinel
	mustTick(t, ticker) // the baseline asks nothing
	if len(*shown) != 0 {
		t.Fatalf("the baseline was judged: %q", *shown)
	}

	acme := filepath.Join(workspace, "inbox", "clients", "acme", "thread.md")
	writeFile(t, acme, "Acme: can we move the review to Friday?\nMe: Friday works.\nAcme: great, see you then.\n")
	writeNested(t, filepath.Join(workspace, "inbox", "clients", "gamma", "thread.md"), "Gamma: could you send a quote for the new site?\n")
	later := now.Add(5 * time.Minute)
	store.clock = held(later)
	ticker = newTicker(store, runner, later)
	ticker.Sentinel = sentinel
	pass := mustTick(t, ticker)
	if len(*shown) != 1 {
		t.Fatalf("the condition was asked %d times, wanted once", len(*shown))
	}
	if pass.Fired != 1 || runner.calls != 1 {
		t.Fatalf("a condition shown two changed client threads did not fire: %+v; it was shown %q", pass, (*shown)[0])
	}
	if !strings.Contains((*shown)[0], "bytes") {
		t.Fatalf("the evidence carries no sizes: %q", (*shown)[0])
	}
}

// A CONDITION THAT SAYS NO USES UP THOSE CHANGES, and one that could not be
// asked does not: the next pass judges the same changes again rather than
// finding nothing and never being asked about them.
func TestAConditionThatCouldNotBeAskedKeepsItsChanges(t *testing.T) {
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	_, workspace := clientsWatch(t, store)
	runner := &occurrenceRunner{}
	mustTick(t, newTicker(store, runner, now))

	writeNested(t, filepath.Join(workspace, "inbox", "clients", "gamma", "thread.md"), "Gamma: hello\n")
	failing := func(context.Context, Judgment) (bool, string, float64, error) {
		return false, "", 0, errors.New("the model could not be reached")
	}
	later := now.Add(5 * time.Minute)
	store.clock = held(later)
	ticker := newTicker(store, runner, later)
	ticker.Sentinel = failing
	if pass := mustTick(t, ticker); pass.Errors != 1 || pass.Fired != 0 {
		t.Fatalf("a condition that could not be asked is %+v", pass)
	}

	sentinel, shown := evidenceJudge("inbox/clients/gamma/thread.md")
	again := later.Add(5 * time.Minute)
	store.clock = held(again)
	ticker = newTicker(store, runner, again)
	ticker.Sentinel = sentinel
	if pass := mustTick(t, ticker); pass.Fired != 1 {
		t.Fatalf("the changes a failed judgment never saw were lost: %+v, shown %q", pass, *shown)
	}
}

// A CONDITION WITH NOTHING TO JUDGE IS REFUSED AT SETUP, naming the field. A
// moment, a rhythm, a quiet machine and a rule gather no evidence, and a
// judgment of words alone is a guess the person is billed for.
func TestAConditionWithNothingToJudgeIsRefused(t *testing.T) {
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	for _, when := range []When{
		{Kind: WhenAt, At: now.Add(time.Hour)},
		{Kind: WhenEvery, Every: "0 8 * * 1-5"},
		{Kind: WhenIdle, IdleFor: time.Hour},
		{Kind: WhenHold},
	} {
		t.Run(string(when.Kind), func(t *testing.T) {
			store := openStore(t, now)
			item := reminder("every weekday at 8, if there is anything worth saying", now.Add(time.Hour))
			item.When = when
			item.When.Hint = "yes when there is anything worth saying"
			if when.Kind == WhenHold {
				item.Does, item.Rails = Action{}, Rails{}
			}
			if err := item.CheckWatch(); err == nil || !strings.Contains(err.Error(), "when.hint") {
				t.Fatalf("a condition on %s was not refused by name before anybody was asked: %v", when.Kind, err)
			}
			if _, err := store.Create(item); err == nil || !strings.Contains(err.Error(), "when.hint") {
				t.Fatalf("a condition on %s stood: %v", when.Kind, err)
			}
		})
	}
}

// AN ITEM THAT ALREADY STANDS WITH SUCH A CONDITION IS NEVER JUDGED BLIND. It
// was accepted before the refusal existed; its condition cannot be answered, so
// it waits and says why rather than buying a guess every morning.
func TestAStandingConditionWithNothingToJudgeIsNotAsked(t *testing.T) {
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	routine := reminder("every weekday at 8, tell me what needs a reply", time.Time{})
	routine.When = When{Kind: WhenEvery, Every: "5m"}
	made, err := store.Create(routine)
	if err != nil {
		t.Fatal(err)
	}
	made.When.Hint = "yes when there is anything worth saying"
	if err := store.Save(made); err != nil {
		t.Fatal(err)
	}
	sentinel, asked := answers(true)
	due := now.Add(6 * time.Minute)
	store.clock = held(due)
	runner := &fakeRunner{}
	ticker := newTicker(store, runner, due)
	ticker.Sentinel = sentinel
	pass := mustTick(t, ticker)
	if *asked != 0 || pass.Fired != 0 || len(runner.said) != 0 {
		t.Fatalf("a condition with nothing to judge was asked %d times and the pass is %+v", *asked, pass)
	}
	back, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(back.LastCheckLine, "condition") {
		t.Fatalf("the item does not say why it waits: %q", back.LastCheckLine)
	}
}

// AN UNKNOWN PLACEHOLDER IS REFUSED AT SETUP. `{{evidence}}` is the only one;
// `{{file}}` reached a person as those six characters.
func TestAnUnknownPlaceholderIsRefused(t *testing.T) {
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	workspace := t.TempDir()
	item := Item{
		Words:     "for support/, just tell me here when a new ticket comes in",
		Workspace: workspace,
		When:      When{Kind: WhenFile, Glob: "support/*"},
		Does:      Action{Kind: ActionSay, Say: "New support ticket landed: {{evidence}}. File: {{file}}."},
		Rails:     Rails{MaxPerDay: 10},
	}
	err := item.CheckWatch()
	if err == nil || !strings.Contains(err.Error(), "{{file}}") || !strings.Contains(err.Error(), "{{evidence}}") || !strings.Contains(err.Error(), "does.say") {
		t.Fatalf("the unknown placeholder was not refused by name: %v", err)
	}
	if _, err := store.Create(item); err == nil {
		t.Fatal("a line carrying {{file}} stood")
	}
	item.Does.Say = "New support ticket landed: {{evidence}}"
	if _, err := store.Create(item); err != nil {
		t.Fatalf("the one placeholder was refused: %v", err)
	}
}

// A SAY PING IS THE COMPOSED LINE. The two-reviews case: the person reads one
// sentence naming what changed, never the reading it was measured from.
func TestASayPingIsTheComposedLineNotTheReading(t *testing.T) {
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "support"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(workspace, "support", "old.md"), "an old ticket\n")
	if _, err := store.Create(Item{
		Words:     "for support/, just tell me here when a new ticket comes in",
		Workspace: workspace,
		When:      When{Kind: WhenFile, Glob: "support/*"},
		Does:      Action{Kind: ActionSay, Say: "New support ticket landed: {{evidence}}"},
		Rails:     Rails{MaxPerDay: 10},
	}); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	mustTick(t, newTicker(store, runner, now))

	writeFile(t, filepath.Join(workspace, "support", "t1.md"), "Ticket 1: the export button does nothing\n")
	later := now.Add(5 * time.Minute)
	store.clock = held(later)
	if pass := mustTick(t, newTicker(store, runner, later)); pass.Said != 1 {
		t.Fatalf("the new ticket was not said: %+v", pass)
	}
	if got, want := runner.said[0], "New support ticket landed: 1 file added: support/t1.md"; got != want {
		t.Fatalf("the ping reads\n%q\nwanted\n%q", got, want)
	}
}

// THE CARD SAYS THE CONDITION. A conditioned watch reads `whenever …, only
// when: …` from the record; a watch with no condition keeps its own words.
func TestTheCardSaysAWatchsCondition(t *testing.T) {
	for _, c := range []struct {
		when When
		want string
	}{
		{When{Kind: WhenFile, Glob: "inbox/clients/**/*", Words: "whenever a file changes inside inbox/clients/", Hint: "any .md file change inside a client subfolder"},
			"whenever a file changes inside inbox/clients/, only when: any .md file change inside a client subfolder"},
		{When{Kind: WhenFile, Glob: "notes/spec.md", Hint: "yes when the offline section changed"},
			"whenever notes/spec.md changes, only when: the offline section changed"},
		{When{Kind: WhenFile, Glob: "*.md", Hint: "a new draft"}, "whenever a file matching *.md changes, only when: a new draft"},
		{When{Kind: WhenFile, Glob: "inbox/*", Words: "when inbox/* changes"}, "when inbox/* changes"},
		{When{Kind: WhenProbe, Words: "when CI goes red", Hint: "yes when conclusion=failure"}, "when CI goes red"},
	} {
		if got := c.when.CardWords(); got != c.want {
			t.Errorf("%+v reads\n%q\nwanted\n%q", c.when, got, c.want)
		}
	}
}

// THE SAY CARD SHOWS THE LINE, with the placeholder said as what will stand
// there.
func TestTheSayCardShowsTheLine(t *testing.T) {
	item := Item{When: When{Kind: WhenFile, Glob: "support/*"}, Does: Action{Kind: ActionSay, Say: "New support ticket landed: {{evidence}}"}}
	if got, want := item.SayWords(), "New support ticket landed: [which files changed]"; got != want {
		t.Fatalf("the say card reads %q, wanted %q", got, want)
	}
	item.Does = Action{Kind: ActionTask, Brief: "report"}
	if got := item.SayWords(); got != "" {
		t.Fatalf("work that runs showed a line to say: %q", got)
	}
}

// WHAT A CONDITION IS SHOWN IS BOUNDED, and a file outside the project is
// named, never shown.
func TestAConditionsEvidenceIsBounded(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "keys")
	writeFile(t, outside, "PRIVATE KEY")
	before := map[string]fileEntry{}
	now := map[string]fileEntry{}
	var changes []Change
	for i := 0; i < 60; i++ {
		name := fmt.Sprintf("inbox/t%02d.md", i)
		writeNested(t, filepath.Join(workspace, name), strings.Repeat("line of a long thread\n", 400))
		now[name] = fileEntry{Size: 8800}
		changes = append(changes, Change{Path: name, Kind: "added"})
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "inbox", "0link.md")); err != nil {
		t.Fatal(err)
	}
	changes = append([]Change{{Path: "inbox/0link.md", Kind: "added"}}, changes...)
	evidence := conditionEvidence(workspace, changes, before, now, false, "")
	if len(evidence) > ProbeClip+len("\n…") {
		t.Fatalf("the evidence is %d bytes, past %d", len(evidence), ProbeClip)
	}
	if strings.Contains(evidence, "PRIVATE KEY") {
		t.Fatal("a file outside the project was shown to the judgment")
	}
	if !strings.Contains(evidence, "and 21 more") {
		t.Fatalf("the change list was not counted past its bound: %q", evidence[:200])
	}
	if got := strings.Count(evidence, "--- inbox/"); got != conditionExcerpts {
		t.Fatalf("%d files were opened, wanted %d", got, conditionExcerpts)
	}
}

// A CONDITION THAT CANNOT BE ASKED BACKS OFF, AND SAYS SO ONCE PER STEP (L9).
// A judge with no key fails on every pass; the watch waits twice as long after
// each failure, up to an hour, and its log grows by one line per step rather
// than one per pass — two hours of passes are a handful of tries, not
// twenty-four, and a handful of lines.
func TestAConditionThatKeepsFailingBacksOff(t *testing.T) {
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, workspace := clientsWatch(t, store)
	runner := &occurrenceRunner{}
	mustTick(t, newTicker(store, runner, now))
	writeNested(t, filepath.Join(workspace, "inbox", "clients", "gamma", "thread.md"), "Gamma: hello\n")

	asked := 0
	failing := func(context.Context, Judgment) (bool, string, float64, error) {
		asked++
		return false, "", 0, errors.New("no key for the judging model")
	}
	at := now
	for pass := 0; pass < 24; pass++ {
		at = at.Add(Interval)
		store.clock = held(at)
		ticker := newTicker(store, runner, at)
		ticker.Sentinel = failing
		mustTick(t, ticker)
	}
	if asked > 6 {
		t.Fatalf("a judgment that cannot be made was tried %d times in two hours", asked)
	}
	raw, err := os.ReadFile(store.LogPath(made.ID))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(raw), "could not check"); lines > 6 {
		t.Fatalf("the item's log grew by %d failure lines in two hours:\n%s", lines, raw)
	}

	sentinel, _ := evidenceJudge("inbox/clients/gamma/thread.md")
	at = at.Add(time.Hour)
	store.clock = held(at)
	ticker := newTicker(store, runner, at)
	ticker.Sentinel = sentinel
	if pass := mustTick(t, ticker); pass.Fired != 1 {
		t.Fatalf("the watch did not come back when its judgment did: %+v", pass)
	}
}

// failingOnce is a watch whose condition could not be asked on its last check:
// the changes are held and the wait is running.
func failingOnce(t *testing.T) (*Store, Item, string, time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, workspace := clientsWatch(t, store)
	runner := &occurrenceRunner{}
	mustTick(t, newTicker(store, runner, now))
	writeNested(t, filepath.Join(workspace, "inbox", "clients", "gamma", "thread.md"), "Gamma: hello\n")
	later := now.Add(Interval)
	store.clock = held(later)
	ticker := newTicker(store, runner, later)
	ticker.Sentinel = func(context.Context, Judgment) (bool, string, float64, error) {
		return false, "", 0, errors.New("no key for the judging model")
	}
	mustTick(t, ticker)
	back, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.FailedChecks != 1 || back.NextDue.IsZero() {
		t.Fatalf("the failed check left no wait: %+v", back)
	}
	return store, back, workspace, later
}

// AN EDIT OF THE CONDITION ALONE KEEPS THE HELD CHANGES (L3). A person fixing
// the condition after a judge failed is not asking to forget what changed.
func TestAConditionEditKeepsTheChangesItHeld(t *testing.T) {
	store, made, _, at := failingOnce(t)
	if _, _, err := store.Revise(made.ID, made.SpecRevision, func(item *Item) error {
		item.When.Hint = "a client's thread changed"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sentinel, shown := evidenceJudge("inbox/clients/gamma/thread.md")
	ticker := newTicker(store, &occurrenceRunner{}, at)
	ticker.Sentinel = sentinel
	if pass := mustTick(t, ticker); pass.Fired != 1 {
		t.Fatalf("the edit folded the held changes into a new baseline: %+v, shown %q", pass, *shown)
	}
}

// A FAILING CHECK THAT FINISHES AFTER AN EDIT DOES NOT PUT BACK THE COUNT THE
// EDIT CLEARED. The edit is newer than the check.
func TestAnEditClearsTheFailureCountEvenMidCheck(t *testing.T) {
	store, made, _, at := failingOnce(t)
	at = at.Add(FailureCeiling)
	store.clock = held(at)
	ticker := newTicker(store, &occurrenceRunner{}, at)
	ticker.Sentinel = func(context.Context, Judgment) (bool, string, float64, error) {
		if _, _, err := store.Revise(made.ID, made.SpecRevision, func(item *Item) error {
			item.When.Hint = "a client's thread changed"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return false, "", 0, errors.New("no key for the judging model")
	}
	mustTick(t, ticker)
	back, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.FailedChecks != 0 || !back.NextDue.IsZero() {
		t.Fatalf("a check that began before the edit restored its failures: count %d, wait until %s", back.FailedChecks, back.NextDue)
	}
}

// RESUME MEANS NOW. A person resuming a watch that was waiting out failed
// checks is not asking it to sit out the rest of the hour.
func TestResumeClearsTheWait(t *testing.T) {
	store, made, _, _ := failingOnce(t)
	if _, err := store.SetStatus(made.ID, StatusPaused, ""); err != nil {
		t.Fatal(err)
	}
	back, err := store.SetStatus(made.ID, StatusActive, "")
	if err != nil {
		t.Fatal(err)
	}
	if back.FailedChecks != 0 || !back.NextDue.IsZero() {
		t.Fatalf("a resumed watch still waits: count %d, until %s", back.FailedChecks, back.NextDue)
	}
}
