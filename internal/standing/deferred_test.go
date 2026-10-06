package standing

// deferred_test.go is the focused proof of the deferred-delivery core: the
// arming seam, honest content fingerprints, three-valued judgment, durable
// ActionSay identity, inbox identity across drain and restart, and the consent
// and storage failures the pass has to survive. Every test here fails on a
// plausible bug, not on plumbing.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// verdicts is a three-valued sentinel that says what a test told it to.
func verdicts(replies ...SentinelReading) (SentinelVerdict, *int) {
	asked := 0
	return func(_ context.Context, _ Judgment) (SentinelReading, string, float64, error) {
		verdict := VerdictUnknown
		if asked < len(replies) {
			verdict = replies[asked]
		}
		asked++
		return verdict, "a judgment", 0, nil
	}, &asked
}

// judgings is a three-valued sentinel that records every Judgment it is handed.
func judgings(reply SentinelReading) (SentinelVerdict, *[]Judgment) {
	seen := &[]Judgment{}
	return func(_ context.Context, judgment Judgment) (SentinelReading, string, float64, error) {
		*seen = append(*seen, judgment)
		return reply, "a judgment", 0, nil
	}, seen
}

// newWatch is a WhenFile watch over a glob in a workspace, with rails and a line.
func newWatch(workspace, glob string) Item {
	return Item{
		Words:     "tell me when " + glob + " changes",
		Workspace: workspace,
		When:      When{Kind: WhenFile, Words: "when " + glob + " changes", Glob: glob},
		Does:      Action{Kind: ActionSay, Say: "it changed: {{evidence}}"},
		Rails:     Rails{PerRunUSD: 0.05, MaxPerDay: 3},
	}
}

// newProbe is a WhenProbe watch with a line to say.
func newProbe(words string) Item {
	return Item{
		Words:     words,
		Workspace: "/tmp/project",
		When:      When{Kind: WhenProbe, Words: words, Probe: Probe{Command: "true"}, ProbeEvery: 5 * time.Minute},
		Does:      Action{Kind: ActionSay, Say: "it is time"},
		Rails:     Rails{PerRunUSD: 0.05, MaxPerDay: 3},
	}
}

// ── file hint evidence ──────────────────────────────────────────────────────

func TestFileHintIsJudgedWithTheChangeItWasToldAbout(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "schema.sql"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	watch := newWatch(workspace, "*.sql")
	watch.When.Hint = "only if it looks important"
	made, err := store.Create(watch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Arm(made.ID); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(workspace, "schema.sql"), []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	sentinel, seen := judgings(VerdictNo)
	tick := &Ticker{Store: store, Runner: runner, SentinelVerdict: sentinel, Now: held(now.Add(time.Minute))}
	mustTick(t, tick)

	// THE BUG THIS PINS: the listing was computed and then thrown away, so the
	// sentinel judged a file change having been handed an empty string.
	if len(*seen) != 1 {
		t.Fatalf("the hint was judged %d times, wanted once", len(*seen))
	}
	if !strings.Contains((*seen)[0].Evidence, "schema.sql") {
		t.Fatalf("the hint judged with evidence %q, wanted the changed-file listing", (*seen)[0].Evidence)
	}
}

func TestHintOnARhythmIsJudgedWithNoEvidence(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	every := reminder("check the release every morning", time.Time{})
	every.When = When{Kind: WhenEvery, Words: "every morning", Every: "0 9 * * *", Hint: "only if there is news"}
	made, err := store.Create(every)
	if err != nil {
		t.Fatal(err)
	}
	// Make it due now rather than tomorrow morning.
	item, _ := store.Get(made.ID)
	item.NextDue = now
	if err := store.Save(item); err != nil {
		t.Fatal(err)
	}

	sentinel, seen := judgings(VerdictNo)
	tick := &Ticker{Store: store, SentinelVerdict: sentinel, Now: held(now.Add(time.Minute))}
	mustTick(t, tick)
	if len(*seen) != 1 {
		t.Fatalf("the rhythm's hint was judged %d times, wanted once", len(*seen))
	}
	if (*seen)[0].Evidence != "" {
		t.Fatalf("a rhythm's hint was handed evidence %q, wanted none", (*seen)[0].Evidence)
	}
}

// ── the arming seam ─────────────────────────────────────────────────────────

func TestArmCapturesTheBaselineAtRatificationNotAtTheFirstTick(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	workspace := t.TempDir()
	path := filepath.Join(workspace, "schema.sql")
	if err := os.WriteFile(path, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	watch := newWatch(workspace, "*.sql")
	made, err := store.Create(watch)
	if err != nil {
		t.Fatal(err)
	}

	// A CHANGE BETWEEN THE YES AND THE FIRST WAKE IS A CHANGE, because the
	// baseline was captured at the yes.
	if err := os.WriteFile(path, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	armed, err := store.Arm(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if armed.Fingerprint == "" {
		t.Fatal("arming left no baseline")
	}

	beforeArmChange := mustTick(t, newTicker(store, &fakeRunner{}, now.Add(time.Minute)))
	if beforeArmChange.Fired != 0 {
		t.Fatalf("the change captured BY arming fired: %+v", beforeArmChange)
	}

	// And a change after arming does fire.
	if err := os.WriteFile(path, []byte("three"), 0o600); err != nil {
		t.Fatal(err)
	}
	later := now.Add(2 * time.Minute)
	store.clock = held(later)
	if pass := mustTick(t, newTicker(store, &fakeRunner{}, later)); pass.Fired != 1 {
		t.Fatalf("a change after arming did not fire: %+v", pass)
	}
}

func TestUnarmedWatchKeepsTheSilentFirstReading(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	workspace := t.TempDir()
	path := filepath.Join(workspace, "schema.sql")
	if err := os.WriteFile(path, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(newWatch(workspace, "*.sql")); err != nil {
		t.Fatal(err)
	}
	// The file changes BEFORE the unarmed watch's first tick; legacy semantics
	// absorb it as the baseline and stay silent.
	if err := os.WriteFile(path, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	if pass := mustTick(t, newTicker(store, &fakeRunner{}, now)); pass.Fired != 0 {
		t.Fatalf("an unarmed first reading fired: %+v", pass)
	}
}

// ── content, not metadata ───────────────────────────────────────────────────

func TestFingerprintSeesContentNotMtimeAndIgnoresATouch(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	workspace := t.TempDir()
	path := filepath.Join(workspace, "schema.sql")
	if err := os.WriteFile(path, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	made, err := store.Create(newWatch(workspace, "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Arm(made.ID); err != nil {
		t.Fatal(err)
	}

	// A BARE TOUCH: new mtime, same bytes, same size. Not a change.
	touch := now.Add(time.Minute)
	if err := os.Chtimes(path, stamp.Add(time.Hour), stamp.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	store.clock = held(touch)
	if pass := mustTick(t, newTicker(store, &fakeRunner{}, touch)); pass.Fired != 0 {
		t.Fatalf("a bare touch fired: %+v", pass)
	}

	// AN EQUAL-SIZE, SAME-MTIME EDIT: same length, mtime restored. A change.
	if err := os.WriteFile(path, []byte("abd"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	edit := now.Add(2 * time.Minute)
	store.clock = held(edit)
	if pass := mustTick(t, newTicker(store, &fakeRunner{}, edit)); pass.Fired != 1 {
		t.Fatalf("an equal-size, same-mtime edit did not fire: %+v", pass)
	}
}

func TestFingerprintSeesARename(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	workspace := t.TempDir()
	old := filepath.Join(workspace, "schema.sql")
	if err := os.WriteFile(old, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	made, err := store.Create(newWatch(workspace, "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Arm(made.ID); err != nil {
		t.Fatal(err)
	}

	if err := os.Rename(old, filepath.Join(workspace, "schema-v2.sql")); err != nil {
		t.Fatal(err)
	}
	later := now.Add(time.Minute)
	store.clock = held(later)
	if pass := mustTick(t, newTicker(store, &fakeRunner{}, later)); pass.Fired != 1 {
		t.Fatalf("a rename did not fire: %+v", pass)
	}
}

func TestArmRefusesATruncatedBaseline(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	workspace := t.TempDir()
	// A file larger than the per-file cap makes the scan truncated.
	big := make([]byte, fingerprintPerFile+1024)
	for i := range big {
		big[i] = 'x'
	}
	if err := os.WriteFile(filepath.Join(workspace, "big.sql"), big, 0o600); err != nil {
		t.Fatal(err)
	}
	made, err := store.Create(newWatch(workspace, "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Arm(made.ID); !errors.Is(err, errBaselineIncomplete) {
		t.Fatalf("arming a truncated scan answered %v, wanted a refusal to invent a baseline", err)
	}
	back, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Fingerprint != "" {
		t.Fatalf("a truncated scan set a baseline: %q", back.Fingerprint)
	}
}

// ── three-valued judgment ───────────────────────────────────────────────────

func TestUnknownProbeConsumesNothingAndRetriesHonestly(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{evidence: "conclusion=maybe"}
	made, err := store.Create(newProbe("tell me when CI is red"))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := store.Get(made.ID)

	sentinel, _ := verdicts(VerdictUnknown)
	tick := &Ticker{Store: store, Runner: runner, SentinelVerdict: sentinel, Now: held(now)}
	pass := mustTick(t, tick)
	if pass.Fired != 0 {
		t.Fatalf("an unknown fired: %+v", pass)
	}
	if len(runner.said) != 0 {
		t.Fatalf("an unknown delivered: %v", runner.said)
	}

	after, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	// NOTHING WAS WRITTEN: no negative in the history, and the item is still due
	// at the moment it was due, so the next pass faces the same question.
	if len(after.Previous) != 0 {
		t.Fatalf("an unknown wrote a negative into the history: %v", after.Previous)
	}
	if !after.NextDue.Equal(before.NextDue) {
		t.Fatalf("an unknown moved the due moment: %s -> %s", before.NextDue, after.NextDue)
	}
	if !after.LastChecked.Equal(before.LastChecked) {
		t.Fatal("an unknown marked the item as checked")
	}

	// And a yes on the next pass still fires.
	yes, _ := verdicts(VerdictYes)
	tick = &Ticker{Store: store, Runner: runner, SentinelVerdict: yes, Now: held(now.Add(time.Minute))}
	if later := mustTick(t, tick); later.Fired != 1 {
		t.Fatalf("the opportunity was consumed; a yes did not fire: %+v", later)
	}
}

func TestUnknownChargesWhatTheCallCost(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{evidence: "conclusion=?"}
	made, err := store.Create(newProbe("tell me when CI is red"))
	if err != nil {
		t.Fatal(err)
	}
	sentinel := func(_ context.Context, _ Judgment) (SentinelReading, string, float64, error) {
		return VerdictUnknown, "the model refused", 0.25, errors.New("timeout")
	}
	tick := &Ticker{Store: store, Runner: runner, SentinelVerdict: sentinel, Now: held(now)}
	if pass := mustTick(t, tick); pass.Errors != 0 {
		t.Fatalf("a refusal was treated as the item's failure: %+v", pass)
	}
	spend, err := store.Today(made.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if spend.USD != 0.25 {
		t.Fatalf("a billed refusal was not charged: the ledger says %v", spend.USD)
	}
}

func TestALegacySentinelErrorIsUnknownNotAFailure(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{evidence: "conclusion=?"}
	made, err := store.Create(newProbe("tell me when CI is red"))
	if err != nil {
		t.Fatal(err)
	}
	sentinel := func(_ context.Context, _ Judgment) (bool, string, float64, error) {
		return false, "", 0, errors.New("the sentinel could not answer")
	}
	tick := &Ticker{Store: store, Runner: runner, Sentinel: sentinel, Now: held(now)}
	if pass := mustTick(t, tick); pass.Errors != 0 {
		t.Fatalf("a sentinel error was an item failure: %+v", pass)
	}
	after, _ := store.Get(made.ID)
	if len(after.Previous) != 0 {
		t.Fatalf("a sentinel error wrote a negative: %v", after.Previous)
	}
}

func TestNonzeroPredicateEvidenceIsEvidenceNotAPass(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	// A failing command's nonzero exit is evidence about the state of the
	// world, never an automatic yes for an arbitrary condition.
	runner := &fakeRunner{evidence: "exit status 1: 3 tests failed"}
	if _, err := store.Create(newProbe("tell me when the tests pass")); err != nil {
		t.Fatal(err)
	}
	sentinel, seen := judgings(VerdictNo)
	tick := &Ticker{Store: store, Runner: runner, SentinelVerdict: sentinel, Now: held(now)}
	if pass := mustTick(t, tick); pass.Fired != 0 {
		t.Fatalf("a nonzero exit acted as a yes: %+v", pass)
	}
	if len(*seen) != 1 || !strings.Contains((*seen)[0].Evidence, "exit status 1") {
		t.Fatalf("the sentinel did not receive the exit evidence: %+v", *seen)
	}
}

// ── durable ActionSay identity ──────────────────────────────────────────────

// deliveringRunner is a Runner that has learned the identity seam: it records
// every identity it is handed and can fail a delivery.
type deliveringRunner struct {
	fakeRunner
	ids     []string
	failFor int
	calls   int
}

func (d *deliveringRunner) Deliver(_ context.Context, _ Item, pending Pending) (Outcome, error) {
	d.calls++
	d.ids = append(d.ids, pending.ID)
	if d.failFor > 0 && d.calls <= d.failFor {
		return Outcome{}, errors.New("the window is closed")
	}
	return Outcome{Kind: "said"}, nil
}

func TestPendingSayIsDurableAndReusesItsIdentityAcrossAFailedDelivery(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &deliveringRunner{failFor: 1}
	made, err := store.Create(reminder("remind me at 6 to leave", now))
	if err != nil {
		t.Fatal(err)
	}

	// The first pass fails mid-delivery; the intent stays on the item.
	first := mustTick(t, newTicker(store, runner, now))
	if first.Errors != 1 {
		t.Fatalf("a failed delivery was not counted: %+v", first)
	}
	stuck, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stuck.Pending) != 1 {
		t.Fatalf("a failed delivery left %d intents, wanted the one it must settle", len(stuck.Pending))
	}

	// The restart re-delivers under the SAME identity and clears it on ack.
	again := now.Add(time.Minute)
	store.clock = held(again)
	second := mustTick(t, newTicker(store, runner, again))
	if second.Fired != 1 {
		t.Fatalf("the retry did not fire: %+v", second)
	}
	if len(runner.ids) != 2 {
		t.Fatalf("the retry delivered %d times, wanted two attempts", len(runner.ids))
	}
	if runner.ids[0] != runner.ids[1] {
		t.Fatalf("the retry minted a new identity: %q then %q", runner.ids[0], runner.ids[1])
	}
	done, _ := store.Get(made.ID)
	if len(done.Pending) != 0 {
		t.Fatalf("an acknowledged delivery left %d intents", len(done.Pending))
	}
}

func TestPendingIsRetriedBySettleAndNeverEvicted(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, err := store.Create(reminder("remind me at 6 to leave", now))
	if err != nil {
		t.Fatal(err)
	}
	item, _ := store.Get(made.ID)
	for i := 0; i < PendingKeep; i++ {
		item.Pending = append(item.Pending, Pending{ID: newID(), Kind: ActionSay, Text: "an older line", At: now})
	}
	if err := store.Save(item); err != nil {
		t.Fatal(err)
	}
	first := item.Pending[0].ID

	// A Runner whose delivery always fails: settle must retry the SAME identity
	// each pass, never evict authorized work, and finally surface it.
	runner := &deliveringRunner{failFor: 1 << 30}
	for attempt := 1; attempt <= PendingGiveUp; attempt++ {
		pass := mustTick(t, newTicker(store, runner, now))
		if pass.Errors == 0 {
			t.Fatalf("pass %d did not count the failed delivery: %+v", attempt, pass)
		}
		back, _ := store.Get(made.ID)
		if len(back.Pending) != PendingKeep {
			t.Fatalf("pass %d evicted authorized work: %d intents left", attempt, len(back.Pending))
		}
		if back.Pending[0].Attempts != attempt {
			t.Fatalf("pass %d bumped the wrong attempt count: %d", attempt, back.Pending[0].Attempts)
		}
	}
	if len(runner.ids) != PendingGiveUp {
		t.Fatalf("the retrying line was delivered %d times, wanted %d", len(runner.ids), PendingGiveUp)
	}
	for _, id := range runner.ids {
		if id != first {
			t.Fatalf("a retry minted a new identity: %q then %q", first, id)
		}
	}
	back, _ := store.Get(made.ID)
	if !strings.Contains(back.NeedsPerson, "could not be delivered") {
		t.Fatalf("a line that cannot be delivered was not surfaced: %q", back.NeedsPerson)
	}
}

// ── the inbox ───────────────────────────────────────────────────────────────

func TestInboxDedupsByIdentityAcrossDrainAndRestart(t *testing.T) {
	dir := t.TempDir()
	note := Note{ItemID: "item", Words: "w", Kind: "said", Text: "one line", ID: "n1"}

	if err := Deliver(dir, note); err != nil {
		t.Fatal(err)
	}
	// A torn or duplicated append of the same identity is one note.
	if err := Deliver(dir, note); err != nil {
		t.Fatal(err)
	}
	first, err := Drain(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].ID != "n1" {
		t.Fatalf("duplicate appends became %d notes: %+v", len(first), first)
	}

	// A restart re-runs the delivery whose acknowledgement was lost. The note
	// was already drained, so it is not put in front of the person again.
	if err := Deliver(dir, note); err != nil {
		t.Fatal(err)
	}
	second, err := Drain(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 0 {
		t.Fatalf("a replayed delivery was shown again: %+v", second)
	}

	// A note with no identity is passed through untouched.
	if err := Deliver(dir, Note{ItemID: "item", Kind: "said", Text: "legacy"}); err != nil {
		t.Fatal(err)
	}
	third, _ := Drain(dir)
	if len(third) != 1 {
		t.Fatalf("an identity-less note was dropped: %+v", third)
	}
}

func TestProjectInboxPeeksWithoutEmptyingAndDrainsWhole(t *testing.T) {
	root := t.TempDir()
	workspace := "/tmp/a-project"
	if err := DeliverProject(root, workspace, Note{ItemID: "i1", Kind: "said", Text: "news", ID: "a1"}); err != nil {
		t.Fatal(err)
	}
	peek := PeekProjectInbox(root, workspace)
	if len(peek) != 1 {
		t.Fatalf("peek saw %d notes, wanted the one", len(peek))
	}
	// Peeking must not consume.
	if again := PeekProjectInbox(root, workspace); len(again) != 1 {
		t.Fatalf("peeking emptied the inbox: %d notes left", len(again))
	}
	drained, err := DrainProject(root, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(drained) != 1 {
		t.Fatalf("drain took %d notes", len(drained))
	}
	empty, _ := DrainProject(root, workspace)
	if len(empty) != 0 {
		t.Fatalf("a second drain found %d notes", len(empty))
	}
}

func TestFailedInboxDeliveryPropagates(t *testing.T) {
	// A session "directory" that is really a file: the delivery cannot land, and
	// that must be an error rather than a silent drop.
	path := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Deliver(path, Note{ItemID: "i", Kind: "said", Text: "news"}); err == nil {
		t.Fatal("a delivery into a file was accepted")
	}
}

// ── consent during a probe ──────────────────────────────────────────────────

// pausingRunner pauses or retires the item from inside its own probe, standing
// in for the person acting while a slow probe is in flight.
type pausingRunner struct {
	fakeRunner
	store *Store
	apply func(Item) Item
}

func (p *pausingRunner) Probe(_ context.Context, item Item) (string, error) {
	current, err := p.store.Get(item.ID)
	if err != nil {
		return "", err
	}
	current = p.apply(current)
	if err := p.store.Save(current); err != nil {
		return "", err
	}
	return "conclusion=success", nil
}

func TestPauseDuringProbeStopsTheDeliveryAndTheWrite(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	if _, err := store.Create(newProbe("tell me when CI is red")); err != nil {
		t.Fatal(err)
	}
	pause := func(item Item) Item {
		item.Status = StatusPaused
		return item
	}
	runner := &pausingRunner{store: store, apply: pause}
	runner.outcome = Outcome{Kind: "said"}
	yes, _ := verdicts(VerdictYes)
	tick := &Ticker{Store: store, Runner: runner, SentinelVerdict: yes, Now: held(now)}
	pass := mustTick(t, tick)
	if pass.Fired != 0 {
		t.Fatalf("a paused item fired: %+v", pass)
	}
	if len(runner.said) != 0 {
		t.Fatalf("a paused item delivered: %v", runner.said)
	}
	items, _ := store.List()
	if items[0].Status != StatusPaused {
		t.Fatalf("the pause did not stick: %q", items[0].Status)
	}
	// THE PERSON'S ACT WINS COMPLETELY: no identity and no delivery intent
	// survive a pause that landed while the probe was in flight.
	if items[0].Positive != "" || len(items[0].Pending) != 0 || items[0].Runs != 0 {
		t.Fatalf("a pause mid-probe left durable state behind: %+v", items[0])
	}
}

func TestPauseThenResumeDuringProbeStillStopsTheDelivery(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	if _, err := store.Create(newProbe("tell me when CI is red")); err != nil {
		t.Fatal(err)
	}
	// Pause and resume: the status ends active, so only the revision can show
	// that the person acted at all.
	resume := func(item Item) Item {
		item.Status = StatusPaused
		if err := store.Save(item); err != nil {
			t.Errorf("pause: %v", err)
		}
		item.Status = StatusActive
		return item
	}
	runner := &pausingRunner{store: store, apply: resume}
	runner.outcome = Outcome{Kind: "said"}
	yes, _ := verdicts(VerdictYes)
	tick := &Ticker{Store: store, Runner: runner, SentinelVerdict: yes, Now: held(now)}
	if pass := mustTick(t, tick); pass.Fired != 0 {
		t.Fatalf("an item acted on mid-probe fired: %+v", pass)
	}
	if len(runner.said) != 0 {
		t.Fatalf("an item acted on mid-probe delivered: %v", runner.said)
	}
}

func TestRetireDuringProbeStopsTheDelivery(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	if _, err := store.Create(newProbe("tell me when CI is red")); err != nil {
		t.Fatal(err)
	}
	retire := func(item Item) Item {
		item.Status = StatusRetired
		item.RetiredWhy = "stopped by you"
		return item
	}
	runner := &pausingRunner{store: store, apply: retire}
	runner.outcome = Outcome{Kind: "said"}
	yes, _ := verdicts(VerdictYes)
	tick := &Ticker{Store: store, Runner: runner, SentinelVerdict: yes, Now: held(now)}
	if pass := mustTick(t, tick); pass.Fired != 0 {
		t.Fatalf("a retired item fired: %+v", pass)
	}
	if len(runner.said) != 0 {
		t.Fatalf("a retired item delivered: %v", runner.said)
	}
}

// ── storage safety ──────────────────────────────────────────────────────────

func TestSaveActiveDoesNotResurrectAMissingItem(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	ghost := reminder("remind me", now)
	ghost.ID = "0123456789abcdef"
	if err := store.saveActive(&ghost); !errors.Is(err, errConsentChanged) {
		t.Fatalf("saving a missing item answered %v, wanted a refusal", err)
	}
	if _, err := os.Stat(store.ItemPath(ghost.ID)); !os.IsNotExist(err) {
		t.Fatalf("a missing item was resurrected: %v", err)
	}
}

func TestSaveActiveRefusesOverAConcurrentEdit(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, err := store.Create(reminder("remind me at 6", now))
	if err != nil {
		t.Fatal(err)
	}
	stale, _ := store.Get(made.ID)

	// Somebody edits it after the pass took its copy.
	fresh, _ := store.Get(made.ID)
	fresh.Words = "remind me at seven"
	if err := store.Save(fresh); err != nil {
		t.Fatal(err)
	}
	if err := store.saveActive(&stale); !errors.Is(err, errConsentChanged) {
		t.Fatalf("a stale save answered %v, wanted a refusal", err)
	}
	back, _ := store.Get(made.ID)
	if back.Words != "remind me at seven" {
		t.Fatalf("the person's edit was overwritten: %q", back.Words)
	}
}

// ── rails still hold under the new paths ────────────────────────────────────

func TestRailsStillStopASecondFiringInADay(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{outcome: Outcome{Kind: "said"}}
	item := reminder("every hour, leave", time.Time{})
	item.When = When{Kind: WhenEvery, Words: "every hour", Every: "0 * * * *"}
	item.Rails.MaxPerDay = 1
	made, err := store.Create(item)
	if err != nil {
		t.Fatal(err)
	}
	due, _ := store.Get(made.ID)
	due.NextDue = now
	if err := store.Save(due); err != nil {
		t.Fatal(err)
	}

	if pass := mustTick(t, newTicker(store, runner, now)); pass.Fired != 1 {
		t.Fatalf("the first firing did not happen: %+v", pass)
	}
	second, _ := store.Get(made.ID)
	second.NextDue = now
	if err := store.Save(second); err != nil {
		t.Fatal(err)
	}
	soon := now.Add(time.Minute)
	store.clock = held(soon)
	if pass := mustTick(t, newTicker(store, runner, soon)); pass.Fired != 0 {
		t.Fatalf("a max-per-day rail was ignored: %+v", pass)
	}
}
