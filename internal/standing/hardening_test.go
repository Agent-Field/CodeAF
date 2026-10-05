package standing

// hardening_test.go is the failure-injection proof for the deferred-delivery
// core: reconcile-before-look, task in-flight reconciliation, the schema
// barrier, inbox crash/race/recovery, and the accounting/consent boundaries.
// Each test fails a plausible bug rather than restating code.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── B1: reconcile a durable pending independently of a fresh look ───────────

func TestSettleRetriesAFileWatchDeliveryWithoutANewLook(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	dir := t.TempDir()
	path := filepath.Join(dir, "schema.sql")
	if err := os.WriteFile(path, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	made, err := store.Create(newWatch(dir, "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Arm(made.ID); err != nil {
		t.Fatal(err)
	}
	// A change after arming makes the first tick fire, and its delivery fails.
	if err := os.WriteFile(path, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &deliveringRunner{failFor: 1}
	first := mustTick(t, newTicker(store, runner, now))
	if first.Errors == 0 {
		t.Fatalf("a failed delivery was not counted: %+v", first)
	}
	stuck, _ := store.Get(made.ID)
	if len(stuck.Pending) != 1 {
		t.Fatalf("a failed delivery left %d intents, wanted one", len(stuck.Pending))
	}
	firstID := stuck.Pending[0].ID

	// The file does NOT change again. The from-the-top settle must still retry
	// the SAME identity — the advanced fingerprint cannot strand the line.
	later := now.Add(time.Minute)
	store.clock = held(later)
	second := mustTick(t, newTicker(store, runner, later))
	if second.Fired != 1 {
		t.Fatalf("the stranded line was not settled: %+v", second)
	}
	if len(runner.ids) != 2 || runner.ids[0] != firstID || runner.ids[1] != firstID {
		t.Fatalf("the retry did not reuse the identity: %v (wanted %q twice)", runner.ids, firstID)
	}
	done, _ := store.Get(made.ID)
	if len(done.Pending) != 0 {
		t.Fatalf("a settled line left %d intents", len(done.Pending))
	}
}

func TestSettleSurfacesAPendingOnANonActiveItem(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, err := store.Create(reminder("remind me at 6 to leave", now))
	if err != nil {
		t.Fatal(err)
	}
	item, _ := store.Get(made.ID)
	item.Pending = append(item.Pending, Pending{ID: newID(), Kind: ActionSay, Text: "a waiting line", At: now})
	item.Status = StatusPaused
	if err := store.Save(item); err != nil {
		t.Fatal(err)
	}
	// A paused item is skipped by the walk, so its waiting line must still be
	// surfaced rather than silently forgotten.
	if pass := mustTick(t, newTicker(store, &fakeRunner{}, now)); pass.Fired != 0 {
		t.Fatalf("a paused item fired: %+v", pass)
	}
	back, _ := store.Get(made.ID)
	if !strings.Contains(back.NeedsPerson, "still waiting") {
		t.Fatalf("the waiting line on a paused item was not surfaced: %q", back.NeedsPerson)
	}
	if len(back.Pending) != 1 {
		t.Fatalf("the intent was lost: %d pending", len(back.Pending))
	}
}

func TestExpiredItemWithAnUndeliverableLineIsHonest(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	item := reminder("remind me at 6 to leave", now)
	item.Rails.Expires = now.Add(30 * time.Minute) // alive for the first firing
	made, err := store.Create(item)
	if err != nil {
		t.Fatal(err)
	}
	// A delivery that always fails, so the line cannot be settled before expiry.
	runner := &deliveringRunner{failFor: 1 << 30}
	first := mustTick(t, newTicker(store, runner, now))
	if first.Errors == 0 {
		t.Fatalf("a failed delivery was not counted: %+v", first)
	}
	stuck, _ := store.Get(made.ID)
	if len(stuck.Pending) != 1 {
		t.Fatalf("the failed firing left %d intents, wanted one", len(stuck.Pending))
	}

	// Past its end: the settle still tries (and fails), then the expiry retires
	// the item with its line still waiting.
	expired := now.Add(time.Hour)
	store.clock = held(expired)
	mustTick(t, newTicker(store, runner, expired))
	retired, _ := store.Get(made.ID)
	if retired.Status != StatusRetired {
		t.Fatalf("the expired item was not retired: %q", retired.Status)
	}
	if len(retired.Pending) != 1 {
		t.Fatalf("the expired item lost its waiting line: %d pending", len(retired.Pending))
	}

	// The next pass must surface the line on the retired item, not forget it.
	later := expired.Add(time.Minute)
	store.clock = held(later)
	second := mustTick(t, newTicker(store, runner, later))
	if second.Fired != 0 {
		t.Fatalf("a retired item fired: %+v", second)
	}
	back, _ := store.Get(made.ID)
	if !strings.Contains(back.NeedsPerson, "still waiting") {
		t.Fatalf("an expired item's waiting line was forgotten: %q", back.NeedsPerson)
	}
}

// ── B4: a task attempt with no known outcome is never replayed ──────────────

func TestTaskInFlightMarkerStopsAReplay(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	task := Item{
		Words:     "every night fix the vet warnings",
		Workspace: "/tmp/project",
		When:      When{Kind: WhenAt, Words: "tonight", At: now},
		Does:      Action{Kind: ActionTask, Brief: "fix the vet warnings"},
		Rails:     Rails{PerRunUSD: 0.05, MaxPerDay: 3},
	}
	made, err := store.Create(task)
	if err != nil {
		t.Fatal(err)
	}
	// The Runner edits the world (creates a run dir) and then fails.
	runner := &fakeRunner{runErr: errors.New("the task died after editing files")}
	first := mustTick(t, newTicker(store, runner, now))
	if first.Errors == 0 {
		t.Fatalf("a failed task was not counted: %+v", first)
	}
	if len(runner.runDirs) != 1 {
		t.Fatalf("the task ran %d times on the first pass", len(runner.runDirs))
	}
	stuck, _ := store.Get(made.ID)
	if stuck.TaskInflight == nil {
		t.Fatal("the in-flight marker was not persisted before the task ran")
	}
	if !strings.Contains(stuck.NeedsPerson, "waiting") {
		t.Fatalf("the ambiguous task was not surfaced: %q", stuck.NeedsPerson)
	}

	// A restart is just the next pass reading the same store: it must NOT run
	// the task again.
	second := mustTick(t, newTicker(store, runner, now.Add(time.Minute)))
	if second.Fired != 0 {
		t.Fatalf("an in-flight task fired again: %+v", second)
	}
	if len(runner.runDirs) != 1 {
		t.Fatalf("the task was replayed: %d runs", len(runner.runDirs))
	}
}

func TestTaskSuccessClearsItsMarker(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	task := Item{
		Words:     "every night fix the vet warnings",
		Workspace: "/tmp/project",
		When:      When{Kind: WhenAt, Words: "tonight", At: now},
		Does:      Action{Kind: ActionTask, Brief: "fix the vet warnings"},
		Rails:     Rails{PerRunUSD: 0.05, MaxPerDay: 3},
	}
	made, err := store.Create(task)
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{outcome: Outcome{Kind: "landed"}}
	if pass := mustTick(t, newTicker(store, runner, now)); pass.Fired != 1 {
		t.Fatalf("a successful task did not fire: %+v", pass)
	}
	done, _ := store.Get(made.ID)
	if done.TaskInflight != nil {
		t.Fatal("a task that reached an outcome kept its in-flight marker")
	}
}

// ── B7: the schema barrier excludes baseline readers for pending/task ───────

func TestSchemaThreeKeepsDurableStateOutOfOlderReaders(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)

	ordinary, err := store.Create(reminder("remind me at 6 to leave", now))
	if err != nil {
		t.Fatal(err)
	}
	if got := schemaOnDisk(t, store, ordinary.ID); got != 1 {
		t.Fatalf("an ordinary order was written at schema %d, hiding it for no reason", got)
	}

	// An item carrying a durable delivery intent is version 3, which an older
	// reader (which knows only up to 2) skips rather than decoding the loss.
	held, _ := store.Get(ordinary.ID)
	held.Pending = append(held.Pending, Pending{ID: newID(), Kind: ActionSay, Text: "a line", At: now})
	if err := store.Save(held); err != nil {
		t.Fatal(err)
	}
	if got := schemaOnDisk(t, store, ordinary.ID); got != Schema || got < 3 {
		t.Fatalf("an item with a durable intent was written at schema %d, not the barrier", got)
	}
	// Prove the old shape would drop it: decode with a struct that has no field.
	raw, err := os.ReadFile(store.ItemPath(ordinary.ID))
	if err != nil {
		t.Fatal(err)
	}
	var old struct {
		Schema int `json:"schema"`
	}
	if err := json.Unmarshal(raw, &old); err != nil {
		t.Fatal(err)
	}
	if old.Schema <= 2 {
		t.Fatalf("a baseline reader would accept the document (schema %d)", old.Schema)
	}

	// This build still reads its own document with the intent intact.
	back, err := store.Get(ordinary.ID)
	if err != nil || len(back.Pending) != 1 {
		t.Fatalf("this build lost the intent reading its own document: %v %d", err, len(back.Pending))
	}
}

// ── B8: inbox crash, race, recovery and bounds ──────────────────────────────

func TestOrphanDrainingIsRecoveredAlongsideANewInbox(t *testing.T) {
	dir := t.TempDir()
	orphan := InboxPath(dir) + ".deadbeef.draining"
	if err := os.WriteFile(orphan, []byte(`{"item":"i","kind":"said","text":"from the crash"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Deliver(dir, Note{ItemID: "i", Kind: "said", Text: "fresh", ID: "fresh1"}); err != nil {
		t.Fatal(err)
	}
	notes, err := Drain(dir)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if len(notes) != 2 {
		t.Fatalf("recovery returned %d notes, wanted the orphan and the fresh one: %+v", len(notes), notes)
	}
	seen := map[string]bool{}
	for _, note := range notes {
		seen[note.Text] = true
	}
	if !seen["from the crash"] || !seen["fresh"] {
		t.Fatalf("recovery lost a note: %+v", notes)
	}
	left, _ := drainFiles(dir)
	if len(left) != 0 {
		t.Fatalf("a drained file was left behind: %v", left)
	}
}

func TestUnreadableInboxIsReportedAndKept(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the file mode this test relies on")
	}
	dir := t.TempDir()
	path := InboxPath(dir)
	if err := os.WriteFile(path, []byte(`{"item":"i","kind":"said","text":"secret"}`+"\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	notes, err := Drain(dir)
	if err == nil {
		t.Fatalf("an unreadable inbox drained without a word: %+v", notes)
	}
	if len(notes) != 0 {
		t.Fatalf("an unreadable inbox produced notes: %+v", notes)
	}
	left, _ := drainFiles(dir)
	if len(left) != 1 {
		t.Fatalf("the unread inbox was deleted rather than kept: %v", left)
	}
}

func TestOversizedNoteIsRefusedAndAnOversizedLineIsKept(t *testing.T) {
	dir := t.TempDir()
	// A note too long to write is refused, not truncated.
	if err := Deliver(dir, Note{ItemID: "i", Kind: "said", Text: strings.Repeat("a", inboxMaxLine+16)}); err == nil {
		t.Fatal("an oversized note was accepted")
	}
	if _, err := os.Stat(InboxPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("a refused note left a file behind: %v", err)
	}

	// A line too long to read whole is reported and the file is kept.
	path := InboxPath(dir)
	if err := os.WriteFile(path, []byte(strings.Repeat("a", inboxMaxLine+16)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	notes, err := Drain(dir)
	if err == nil {
		t.Fatalf("an unreadably long line drained silently: %+v", notes)
	}
	left, _ := drainFiles(dir)
	if len(left) != 1 {
		t.Fatalf("an unreadably long inbox was deleted: %v", left)
	}
}

func TestConcurrentDeliverAndDrainLoseNothing(t *testing.T) {
	dir := t.TempDir()
	const count = 100
	stop := make(chan struct{})
	var deliverWg, drainWg sync.WaitGroup

	deliverWg.Add(1)
	go func() {
		defer deliverWg.Done()
		for i := 0; i < count; i++ {
			_ = Deliver(dir, Note{ItemID: "i", Kind: "said", Text: fmt.Sprintf("line %d", i), ID: fmt.Sprintf("id-%d", i)})
		}
	}()

	var (
		mu  sync.Mutex
		got = map[string]int{}
	)
	drainWg.Add(1)
	go func() {
		defer drainWg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			notes, _ := Drain(dir)
			mu.Lock()
			for _, note := range notes {
				if note.ID != "" {
					got[note.ID]++
				}
			}
			mu.Unlock()
		}
	}()

	deliverWg.Wait()
	close(stop)
	drainWg.Wait()

	// A final drain catches anything delivered last.
	if notes, err := Drain(dir); err != nil {
		t.Fatalf("final drain: %v", err)
	} else {
		mu.Lock()
		for _, note := range notes {
			got[note.ID]++
		}
		mu.Unlock()
	}
	mu.Lock()
	defer mu.Unlock()
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("id-%d", i)
		if got[id] != 1 {
			t.Fatalf("note %s was drained %d times, wanted once", id, got[id])
		}
	}
}

// ── B9: accounting and consent boundaries ───────────────────────────────────

func TestAccountingFailureDoesNotRelabelADelivery(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, err := store.Create(reminder("remind me at 6 to leave", now))
	if err != nil {
		t.Fatal(err)
	}
	// Make the item's LOG path unwritable by putting a directory where the log
	// file goes: the delivery still happens, but its log line cannot be written.
	if err := os.MkdirAll(store.LogPath(made.ID), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{outcome: Outcome{Kind: "said"}}
	pass := mustTick(t, newTicker(store, runner, now))
	if pass.Errors == 0 {
		t.Fatalf("an accounting failure was not counted: %+v", pass)
	}
	if len(runner.said) != 1 {
		t.Fatalf("the delivery did not happen: %v", runner.said)
	}
	back, _ := store.Get(made.ID)
	if strings.Contains(back.LastCheckLine, "could not check") {
		t.Fatalf("a delivered firing was relabelled a check failure: %q", back.LastCheckLine)
	}
	if len(back.Pending) != 0 {
		t.Fatalf("a successful delivery left %d intents", len(back.Pending))
	}
}

func TestPauseDuringDeliveryDoesNotClaimACancelledOne(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, err := store.Create(newProbe("tell me when CI is red"))
	if err != nil {
		t.Fatal(err)
	}
	// A runner that pauses the item from inside its own delivery, then succeeds.
	pause := func(item Item) Item {
		item.Status = StatusPaused
		return item
	}
	runner := &pausingSayer{store: store, apply: pause}
	yes, _ := verdicts(VerdictYes)
	tick := &Ticker{Store: store, Runner: runner, SentinelVerdict: yes, Now: held(now)}
	mustTick(t, tick)
	if len(runner.said) != 1 {
		t.Fatalf("the delivery did not run before the pause: %v", runner.said)
	}
	back, _ := store.Get(made.ID)
	if back.Status != StatusPaused {
		t.Fatalf("the pause did not stick: %q", back.Status)
	}
	if strings.Contains(back.LastCheckLine, "could not check") {
		t.Fatalf("a pause during delivery was relabelled a check failure: %q", back.LastCheckLine)
	}
}

// pausingSayer is a Runner whose Say pauses the item mid-delivery and then
// succeeds, standing in for the person acting while the line is going out.
type pausingSayer struct {
	fakeRunner
	store *Store
	apply func(Item) Item
}

func (p *pausingSayer) Say(_ context.Context, item Item, text string) (Outcome, error) {
	p.said = append(p.said, text)
	current, err := p.store.Get(item.ID)
	if err != nil {
		return Outcome{}, err
	}
	if err := p.store.Save(p.apply(current)); err != nil {
		return Outcome{}, err
	}
	return Outcome{Kind: "said"}, nil
}

// ── fingerprint bounds and instability ──────────────────────────────────────

func TestBoundedGlobCapsEnumeration(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 15; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%02d.sql", i)), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	matches, truncated, err := boundedGlob(filepath.Join(dir, "*.sql"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 10 || !truncated {
		t.Fatalf("boundedGlob returned %d matches and truncated=%v, wanted the cap", len(matches), truncated)
	}
}

func TestFingerprintMarksAnUnreadableFileTruncated(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the file mode this test relies on")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.sql"), []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}
	_, _, truncated, err := fingerprint(dir, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !truncated {
		t.Fatal("a file that could not be read was certified as a complete reading")
	}
}
