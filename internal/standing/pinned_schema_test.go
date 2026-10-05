package standing

// Tests for the integration-critical core behaviours: the schema fence that
// protects the ratifying session's pinned model from baseline readers, the
// folder-addressed inbox lock that must not depend on a process's TEMP, the
// bounded glob that must never fall back to an unbounded one, the ratifier's
// visible baseline flag, and the first look that clears it.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A PINNED ORDER IS WRITTEN BEYOND A BASELINE READER'S REACH. A schema-1/2
// build decodes the document without Origin.OneModel/PinnedModel and would
// re-marshal the silent re-routing back, so the fields must fence the document
// at Schema 3 exactly as a Pending or a fingerprint does.
func TestAPinnedOrderIsFencedFromBaselineReaders(t *testing.T) {
	now := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	workspace := t.TempDir()

	pinned := newWatch(workspace, "*.sql")
	pinned.Origin.OneModel = true
	pinned.Origin.PinnedModel = "deepseek/deepseek-v4.1-flash"
	made, err := store.Create(pinned)
	if err != nil {
		t.Fatalf("create pinned: %v", err)
	}
	if got := schemaOnDisk(t, store, made.ID); got != Schema {
		t.Fatalf("a pinned order was written at schema %d, not the barrier %d", got, Schema)
	}
	back, err := store.Get(made.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !back.Origin.OneModel || back.Origin.PinnedModel != "deepseek/deepseek-v4.1-flash" {
		t.Fatalf("the pinned policy did not survive the write: %+v", back.Origin)
	}
	// A plain order stays readable by those same builds, so the fence is only on
	// the semantics that need it.
	plain, err := store.Create(newWatch(t.TempDir(), "*.md"))
	if err != nil {
		t.Fatalf("create plain: %v", err)
	}
	if got := schemaOnDisk(t, store, plain.ID); got > 2 {
		t.Fatalf("an unprefixed order was fenced at schema %d for no reason", got)
	}
}

// THE LOCK IS ADDRESSED BY THE FOLDER, NOT THE PROCESS. A symlink alias and a
// different TEMP directory must resolve to the same seen record and the same
// lock, so a delivery handed over under one spelling is not delivered again
// under the other.
func TestInboxLockIsAddressedByTheFolderNotTheProcess(t *testing.T) {
	real := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	id := "delivery-lock-1"

	tempOne := t.TempDir()
	t.Setenv("TMPDIR", tempOne)
	if err := Deliver(alias, Note{ID: id, Text: "the last run failed"}); err != nil {
		t.Fatalf("Deliver through the alias: %v", err)
	}
	// DRAINED THROUGH THE ALIAS AND RECORDED AS SEEN. The seen record is written
	// under the folder both spellings resolve to.
	first, err := Drain(alias)
	if err != nil {
		t.Fatalf("Drain through the alias: %v", err)
	}
	if len(first) != 1 || first[0].ID != id {
		t.Fatalf("the alias drained %+v, want the one note", first)
	}
	// A different process TEMP must not take a different lock or a different
	// seen record: the identity is already spent, so the same delivery under
	// the real spelling is not appended a second time.
	tempTwo := t.TempDir()
	t.Setenv("TMPDIR", tempTwo)
	if err := Deliver(real, Note{ID: id, Text: "the last run failed"}); err != nil {
		t.Fatalf("Deliver under a second TEMP: %v", err)
	}
	notes, err := Drain(real)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(notes) != 0 {
		t.Fatalf("an identity drained through the alias was delivered again: %+v", notes)
	}
	// AND THE LOCK LIVES WITH THE FOLDER. Nothing hash-named was written into
	// either temp directory.
	for _, dir := range []string{tempOne, tempTwo} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read temp: %v", err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "codeaf-inbox-") {
				t.Fatalf("the lock was addressed by the process TEMP: %s", entry.Name())
			}
		}
	}
	if _, err := os.Stat(filepath.Join(real, inboxLockName)); err != nil {
		t.Fatalf("the lock did not travel with the inbox folder: %v", err)
	}
}

// A METACHARACTER IN ANY SEGMENT IS EXPANDED UNDER THE SAME BOUND. The old
// reader handed such a pattern to filepath.Glob, which materialises every match
// before any cap applies; the walker must handle it, and a pattern it cannot
// expand must answer truncation rather than pretend the set is empty.
func TestBoundedGlobExpandsAMetacharacterInAnySegment(t *testing.T) {
	workspace := t.TempDir()
	// "services/*/go.mod": a metacharacter NOT in the final segment.
	for _, name := range []string{"one", "two"} {
		dir := filepath.Join(workspace, "services", name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	matches, truncated, err := boundedGlob(filepath.Join(workspace, "services", "*", "go.mod"), 50)
	if err != nil || truncated {
		t.Fatalf("a metacharacter segment answered (%v, truncated=%v)", err, truncated)
	}
	if len(matches) != 2 {
		t.Fatalf("the walker found %v, want both go.mod files", matches)
	}

	// A DIRECTORY WHOSE NAME ITSELF CONTAINS A METACHARACTER is expanded by
	// listing the parent, never by filepath.Glob.
	odd := filepath.Join(workspace, "odd*dir")
	if err := os.MkdirAll(odd, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(odd, "schema.sql"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	matches, truncated, err = boundedGlob(filepath.Join(workspace, "odd*dir", "*.sql"), 50)
	if err != nil || truncated || len(matches) != 1 {
		t.Fatalf("a metacharacter directory answered (%v, truncated=%v, err=%v)", matches, truncated, err)
	}

	// AND THE COLLECTION IS STILL CAPPED, reporting truncation rather than a
	// short list read as a complete one.
	many := t.TempDir()
	for i := 0; i < 10; i++ {
		if err := os.WriteFile(filepath.Join(many, "f"+string(rune('a'+i))+".log"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	matches, truncated, err = boundedGlob(filepath.Join(many, "*.log"), 3)
	if err != nil || !truncated || len(matches) > 3 {
		t.Fatalf("the cap answered (%v, truncated=%v, err=%v)", matches, truncated, err)
	}
}

// THE BASELINE FLAG IS VISIBLE AND NEVER REPLACES A QUESTION, and the first
// look that reads everything clears it because the baseline has now been taken.
func TestTheBaselineFlagIsVisibleUntilTheFirstCompleteLook(t *testing.T) {
	now := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "schema.sql"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	watch := newWatch(workspace, "*.sql")
	made, err := store.Create(watch)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// A QUESTION IS NOT CLOBBERED BY A LATER FLAG: the field's first tenant wins.
	if err := store.NoteNeedsPerson(made.ID, "what do you want me to do about it"); err != nil {
		t.Fatalf("NoteNeedsPerson question: %v", err)
	}
	if err := store.NoteNeedsPerson(made.ID, NeedsBaselineLead); err != nil {
		t.Fatalf("NoteNeedsPerson flag: %v", err)
	}
	back, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.NeedsPerson != "what do you want me to do about it" {
		t.Fatalf("the flag overwrote a question: %q", back.NeedsPerson)
	}
	// On an item with no question the flag IS made visible.
	quietWatch, err := store.Create(newWatch(t.TempDir(), "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.NoteNeedsPerson(quietWatch.ID, NeedsBaselineLead); err != nil {
		t.Fatal(err)
	}
	quiet, err := store.Get(quietWatch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !IsBaselineLine(quiet.NeedsPerson) {
		t.Fatalf("the flag is not visible: %q", quiet.NeedsPerson)
	}

	// Put the flag on the watched item and let one complete look run: it
	// establishes the baseline and takes the stale flag down.
	flagged := back
	flagged.NeedsPerson = NeedsBaselineLead
	flagged.Fingerprint = ""
	flagged.Revision = 0
	if err := store.write(flagged); err != nil {
		t.Fatal(err)
	}
	ticker := newTicker(store, &fakeRunner{}, now.Add(time.Minute))
	mustTick(t, ticker)
	after, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Fingerprint == "" {
		t.Fatal("the first complete look did not set a baseline")
	}
	if after.NeedsPerson != "" {
		t.Fatalf("the stale baseline flag survived the look that set the baseline: %q", after.NeedsPerson)
	}
}

// A PAID CHECK NOBODY COULD DECIDE STILL COSTS, AND THE ITEM SAYS SO. The
// undecided path writes nothing that would consume the opportunity, but the
// billed cost must survive on the item's own lifetime figure across ticks and
// reloads, and the daily rail still fences what has been spent.
func TestAPaidUndecidedCheckPersistsItsCostWithoutConsumingTheLook(t *testing.T) {
	now := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	probe := newProbe("tell me when CI is red")
	made, err := store.Create(probe)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	const billed = 0.25
	refusing := func(context.Context, Judgment) (SentinelReading, string, float64, error) {
		return VerdictUnknown, "the provider refused", billed, errors.New("the provider refused")
	}

	spendAfterTick := func(at time.Time) Item {
		t.Helper()
		ticker := newTicker(store, &fakeRunner{}, at)
		ticker.SentinelVerdict = refusing
		mustTick(t, ticker)
		// RELOADED FROM DISK, so this is what a later pass would read.
		current, err := store.Get(made.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		return current
	}

	first := spendAfterTick(now.Add(time.Minute))
	if first.SpentUSD != billed {
		t.Fatalf("a billed refusal left the item's figure at %v, want %v", first.SpentUSD, billed)
	}
	// NOTHING ABOUT THE LOOK WAS CONSUMED: the due moment is where it was, no
	// check is stamped, no question is written, and it is still active.
	if !first.NextDue.Equal(made.NextDue) || !first.LastChecked.IsZero() || first.NeedsPerson != "" || first.Status != StatusActive {
		t.Fatalf("the undecided look consumed the opportunity: %+v", first)
	}

	second := spendAfterTick(now.Add(2 * time.Minute))
	if second.SpentUSD != 2*billed {
		t.Fatalf("the second billed refusal lost the first: %v", second.SpentUSD)
	}
	if second.Runs != 0 {
		t.Fatalf("an undecided check counted a firing: %d", second.Runs)
	}

	// THE DAY'S RAIL STILL FENCES ON WHAT WAS SPENT. Two billed checks are on
	// the ledger; a pass under that ceiling looks at nothing more.
	third := newTicker(store, &fakeRunner{}, now.Add(3*time.Minute))
	third.SentinelVerdict = refusing
	third.DailyRailUSD = 2 * billed
	pass := mustTick(t, third)
	if pass.Skipped != 1 || pass.Checked != 0 {
		t.Fatalf("the daily rail did not hold: %+v", pass)
	}
	after, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.SpentUSD != 2*billed {
		t.Fatalf("a skipped pass changed the item's figure to %v", after.SpentUSD)
	}
}
