package standing

// probe_dedup_test.go is the focused proof of the probe recurrence contract: an
// UNCHANGED continuous positive is reported once and then quiet, a reading that
// moved (or a decided no) makes a fresh edge, an unknown or a clipped reading
// certifies nothing, and the identity survives a restart and a failed delivery.
// It also pins that the machinery, not the sentinel's wording, is what bounds
// the repeat: a model that answers "no, already reported" and a model that
// answers "yes, already reported" on the SAME observed bytes both stay quiet.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// countingSentinel answers the same verdict every time with a line that VARIES,
// which is exactly the model behaviour the machinery must not depend on.
func countingSentinel(reading SentinelReading) (SentinelVerdict, *int) {
	calls := 0
	return func(_ context.Context, _ Judgment) (SentinelReading, string, float64, error) {
		calls++
		return reading, fmt.Sprintf("look %d says %v", calls, reading), 0, nil
	}, &calls
}

func probeTicker(store *Store, runner Runner, sentinel SentinelVerdict, at time.Time) *Ticker {
	store.clock = held(at)
	return &Ticker{Store: store, Runner: runner, SentinelVerdict: sentinel, Now: held(at)}
}

func TestAnUnchangedPositiveProbeIsReportedOnce(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{evidence: `{"ready": true}`}
	made, err := store.Create(newProbe("notify me once when ready becomes true; quiet on false or unavailable"))
	if err != nil {
		t.Fatal(err)
	}
	sentinel, calls := countingSentinel(VerdictYes)

	if pass := mustTick(t, probeTicker(store, runner, sentinel, now)); pass.Fired != 1 {
		t.Fatalf("the rising edge did not fire: %+v", pass)
	}
	if len(runner.said) != 1 {
		t.Fatalf("the rising edge said %d lines, wanted one", len(runner.said))
	}
	fired, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fired.Positive == "" {
		t.Fatalf("the affirmative reading left no identity: %+v", fired)
	}
	if fired.Schema != Schema {
		t.Fatalf("the item carrying a positive identity is schema %d, wanted %d", fired.Schema, Schema)
	}

	// AT LEAST TWO LATER ELIGIBLE CHECKS: both are due, both observe the same
	// bytes, and neither may fire or deliver again.
	for i := 2; i <= 3; i++ {
		at := now.Add(time.Duration(i)*5*time.Minute + time.Minute)
		pass := mustTick(t, probeTicker(store, runner, sentinel, at))
		if pass.Fired != 0 {
			t.Fatalf("check %d re-fired an unchanged positive: %+v", i, pass)
		}
		if pass.Checked != 1 {
			t.Fatalf("check %d is %+v, wanted one quiet check", i, pass)
		}
	}
	if len(runner.said) != 1 {
		t.Fatalf("an unchanged positive was delivered %d times, wanted once", len(runner.said))
	}
	after, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Positive != fired.Positive {
		t.Fatalf("the identity moved on a quiet check: %q -> %q", fired.Positive, after.Positive)
	}
	if *calls < 3 {
		t.Fatalf("the sentinel was asked %d times, wanted every due check judged", *calls)
	}
}

func TestASentinelThatSaysNoOnTheSameReadingDoesNotRearm(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{evidence: `{"ready": true}`}
	made, err := store.Create(newProbe("notify me once when ready becomes true"))
	if err != nil {
		t.Fatal(err)
	}

	yes := func(_ context.Context, _ Judgment) (SentinelReading, string, float64, error) {
		return VerdictYes, "ready is true", 0, nil
	}
	if pass := mustTick(t, probeTicker(store, runner, yes, now)); pass.Fired != 1 {
		t.Fatalf("the first true did not fire: %+v", pass)
	}
	before, _ := store.Get(made.ID)

	// THE MODEL FLIP-FLOPS on bytes that did not move. A "no" about the reading
	// already reported is not a state change and must not rearm the watch.
	no := func(_ context.Context, _ Judgment) (SentinelReading, string, float64, error) {
		return VerdictNo, "same state already reported, nothing changed", 0, nil
	}
	at := now.Add(6 * time.Minute)
	if pass := mustTick(t, probeTicker(store, runner, no, at)); pass.Fired != 0 {
		t.Fatalf("a no on the same bytes fired: %+v", pass)
	}
	mid, _ := store.Get(made.ID)
	if mid.Positive != before.Positive {
		t.Fatalf("a no on the same bytes dropped the identity: %q -> %q", before.Positive, mid.Positive)
	}

	// And a later "yes" on the same bytes is STILL the state already reported.
	at = now.Add(11 * time.Minute)
	if pass := mustTick(t, probeTicker(store, runner, yes, at)); pass.Fired != 0 {
		t.Fatalf("a late yes on unchanged bytes fired again: %+v", pass)
	}
	if len(runner.said) != 1 {
		t.Fatalf("the unchanged positive was delivered %d times, wanted once", len(runner.said))
	}
}

func TestAChangedReadingIsAFreshEdge(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{evidence: `{"ready": true, "n": 1}`}
	if _, err := store.Create(newProbe("tell me when the condition is true")); err != nil {
		t.Fatal(err)
	}
	sentinel, _ := countingSentinel(VerdictYes)
	if pass := mustTick(t, probeTicker(store, runner, sentinel, now)); pass.Fired != 1 {
		t.Fatalf("the first reading did not fire: %+v", pass)
	}
	// A REAL change in what the probe saw is a new observation and fires.
	runner.evidence = `{"ready": true, "n": 2}`
	at := now.Add(6 * time.Minute)
	if pass := mustTick(t, probeTicker(store, runner, sentinel, at)); pass.Fired != 1 {
		t.Fatalf("a changed reading did not fire: %+v", pass)
	}
	if len(runner.said) != 2 {
		t.Fatalf("wanted two deliveries for two distinct readings, got %d", len(runner.said))
	}
}

func TestADecidedNoRearmsTheProbeForANewTrue(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	trueReading := `{"ready": true}`
	falseReading := `{"ready": false}`
	runner := &fakeRunner{evidence: trueReading}
	made, err := store.Create(newProbe("notify me once when ready becomes true"))
	if err != nil {
		t.Fatal(err)
	}
	yes := func(_ context.Context, _ Judgment) (SentinelReading, string, float64, error) {
		return VerdictYes, "ready is true", 0, nil
	}
	no := func(_ context.Context, _ Judgment) (SentinelReading, string, float64, error) {
		return VerdictNo, "ready is false", 0, nil
	}
	if pass := mustTick(t, probeTicker(store, runner, yes, now)); pass.Fired != 1 {
		t.Fatalf("true did not fire: %+v", pass)
	}

	// FALSE CLEARS THE IDENTITY, so the next true is a genuine rising edge again.
	runner.evidence = falseReading
	at := now.Add(6 * time.Minute)
	if pass := mustTick(t, probeTicker(store, runner, no, at)); pass.Fired != 0 {
		t.Fatalf("false fired: %+v", pass)
	}
	cleared, _ := store.Get(made.ID)
	if cleared.Positive != "" {
		t.Fatalf("a decided no did not rearm the watch: identity %q survived", cleared.Positive)
	}

	// TRUE AGAIN, the SAME bytes as the first true: it must fire, because the
	// condition went away and came back.
	runner.evidence = trueReading
	at = now.Add(11 * time.Minute)
	if pass := mustTick(t, probeTicker(store, runner, yes, at)); pass.Fired != 1 {
		t.Fatalf("true-false-true did not re-arm: %+v", pass)
	}
	if len(runner.said) != 2 {
		t.Fatalf("wanted two notifications across the two true edges, got %d", len(runner.said))
	}
}

func TestAnUnknownOnTheSameReadingWritesNothingAndKeepsTheIdentity(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{evidence: `{"ready": true}`}
	made, err := store.Create(newProbe("notify me once when ready becomes true"))
	if err != nil {
		t.Fatal(err)
	}
	yes := func(_ context.Context, _ Judgment) (SentinelReading, string, float64, error) {
		return VerdictYes, "ready is true", 0, nil
	}
	if pass := mustTick(t, probeTicker(store, runner, yes, now)); pass.Fired != 1 {
		t.Fatalf("the first true did not fire: %+v", pass)
	}
	before, _ := store.Get(made.ID)

	unknown := func(_ context.Context, _ Judgment) (SentinelReading, string, float64, error) {
		return VerdictUnknown, "the endpoint timed out", 0, nil
	}
	at := now.Add(6 * time.Minute)
	if pass := mustTick(t, probeTicker(store, runner, unknown, at)); pass.Fired != 0 {
		t.Fatalf("an unknown fired: %+v", pass)
	}
	after, _ := store.Get(made.ID)
	if !after.LastChecked.Equal(before.LastChecked) {
		t.Fatalf("an unknown marked a check that wrote nothing: %s -> %s", before.LastChecked, after.LastChecked)
	}
	if after.Positive != before.Positive {
		t.Fatalf("an unknown moved the positive identity: %q -> %q", before.Positive, after.Positive)
	}

	// A later yes on the SAME bytes is still the state already reported.
	at = now.Add(11 * time.Minute)
	if pass := mustTick(t, probeTicker(store, runner, yes, at)); pass.Fired != 0 {
		t.Fatalf("an unknown then a yes on unchanged bytes re-fired: %+v", pass)
	}
}

func TestAClippedReadingCannotCertifyUnchangedState(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	// A reading longer than the clip: the sentinel sees only the tail, so the
	// machinery has no identity it can honestly compare.
	big := strings.Repeat("x", ProbeClip) + "tail"
	runner := &fakeRunner{evidence: big}
	made, err := store.Create(newProbe("notify me when the condition is true"))
	if err != nil {
		t.Fatal(err)
	}
	sentinel, _ := countingSentinel(VerdictYes)
	if pass := mustTick(t, probeTicker(store, runner, sentinel, now)); pass.Fired != 1 {
		t.Fatalf("the clipped reading did not fire: %+v", pass)
	}
	after, _ := store.Get(made.ID)
	if after.Positive != "" {
		t.Fatalf("a clipped reading certified an identity: %q", after.Positive)
	}
	// With no identity, a second identical look is judged afresh rather than
	// silently passed off as "the same state".
	at := now.Add(6 * time.Minute)
	if pass := mustTick(t, probeTicker(store, runner, sentinel, at)); pass.Fired != 1 {
		t.Fatalf("a clipped reading was suppressed as unchanged: %+v", pass)
	}
}

func TestPositiveIdentitySurvivesRestartAndFailedDelivery(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &deliveringRunner{fakeRunner: fakeRunner{evidence: `{"ready": true}`}}
	runner.failFor = 1 // the first delivery is lost
	made, err := store.Create(newProbe("notify me once when ready becomes true"))
	if err != nil {
		t.Fatal(err)
	}
	yes, _ := countingSentinel(VerdictYes)

	first := mustTick(t, probeTicker(store, runner, yes, now))
	if first.Fired != 0 {
		t.Fatalf("a failed delivery was counted as a firing: %+v", first)
	}
	stuck, _ := store.Get(made.ID)
	if len(stuck.Pending) != 1 {
		t.Fatalf("the failed delivery left %d intents, wanted the one it must settle", len(stuck.Pending))
	}
	if stuck.Positive == "" {
		t.Fatalf("the condition identity was not committed with the intent: %+v", stuck)
	}

	// RESTART: a fresh ticker settles the waiting line under its own identity
	// and then faces the same reading. It must NOT fire a second line.
	at := now.Add(6 * time.Minute)
	second := mustTick(t, probeTicker(store, runner, yes, at))
	if len(runner.ids) != 2 || runner.ids[0] != runner.ids[1] {
		t.Fatalf("the restart did not settle the same identity: %v", runner.ids)
	}
	if second.Fired != 1 {
		t.Fatalf("the waiting line was not settled exactly once: %+v", second)
	}
	settled, _ := store.Get(made.ID)
	if len(settled.Pending) != 0 {
		t.Fatalf("the settled intent is still pending: %+v", settled.Pending)
	}
	if runner.calls != 2 {
		t.Fatalf("wanted one failed attempt and one settlement, got %d calls", runner.calls)
	}
}

func TestAScheduledRecurringCheckStillSpeaksUnchangedText(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{}
	every := reminder("every minute, say hello", time.Time{})
	every.When = When{Kind: WhenEvery, Every: "1m"}
	every.Rails = Rails{PerRunUSD: 0.01, MaxPerDay: 100}
	made, err := store.Create(every)
	if err != nil {
		t.Fatal(err)
	}
	sentinel, _ := countingSentinel(VerdictYes)
	for i := 1; i <= 2; i++ {
		at := now.Add(time.Duration(i) * time.Minute)
		if pass := mustTick(t, probeTicker(store, runner, sentinel, at)); pass.Fired != 1 {
			t.Fatalf("a scheduled check %d did not fire on its due moment: %+v", i, pass)
		}
	}
	if len(runner.said) != 2 {
		t.Fatalf("a recurring schedule said %d lines, wanted one per due moment", len(runner.said))
	}
	after, _ := store.Get(made.ID)
	if after.Positive != "" {
		t.Fatalf("a scheduled rhythm grew a probe identity: %q", after.Positive)
	}
}

func TestAProbeErrorLeavesTheOpportunityAndIdentityUntouched(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{evidence: `{"ready": true}`}
	made, err := store.Create(newProbe("notify me once when ready becomes true"))
	if err != nil {
		t.Fatal(err)
	}
	yes, _ := countingSentinel(VerdictYes)
	if pass := mustTick(t, probeTicker(store, runner, yes, now)); pass.Fired != 1 {
		t.Fatalf("the first true did not fire: %+v", pass)
	}
	before, _ := store.Get(made.ID)

	// The look itself failed: the due moment is kept and nothing is written.
	runner.probeErr = errors.New("the command did not finish")
	at := now.Add(6 * time.Minute)
	pass := mustTick(t, probeTicker(store, runner, yes, at))
	if pass.Fired != 0 {
		t.Fatalf("a probe error fired: %+v", pass)
	}
	after, _ := store.Get(made.ID)
	if !after.NextDue.Equal(before.NextDue) {
		t.Fatalf("a probe error consumed the opportunity: due %s -> %s", before.NextDue, after.NextDue)
	}
	if after.Positive != before.Positive {
		t.Fatalf("a probe error moved the identity: %q -> %q", before.Positive, after.Positive)
	}

	// A successful look at the SAME reading afterwards is still suppressed.
	runner.probeErr = nil
	at = now.Add(7 * time.Minute)
	if pass := mustTick(t, probeTicker(store, runner, yes, at)); pass.Fired != 0 {
		t.Fatalf("a recovered look re-fired an unchanged positive: %+v", pass)
	}
}

func TestAPositiveIdentityIsFencedFromBaselineReaders(t *testing.T) {
	// A build that predates Item.Positive would decode the document without it
	// and re-report an unchanged positive. The fence must be NEWER than the
	// schema that build reads: the build that introduced the identity was
	// itself a schema-3 reader, so riding version 3 would not stop it.
	if got := SchemaOf(Item{}); got != 1 {
		t.Fatalf("an ordinary item is schema %d, wanted 1", got)
	}
	if got := SchemaOf(Item{Positive: "abc"}); got != Schema {
		t.Fatalf("an item carrying a positive identity is schema %d, wanted %d", got, Schema)
	}
	if Schema <= schemaDeferred {
		t.Fatalf("the positive fence is %d, not newer than the schema-%d reader it must stop", Schema, schemaDeferred)
	}
}

// A ONE-SHOT OR POSITIVE DOCUMENT IS BEYOND A SCHEMA-3 READER, WHILE THE
// DOCUMENTS THAT PREDATE IT ARE NOT. The intermediate build that landed the
// positive identity at schema 3 would drop [When.OneShot] and [Item.Positive] and
// re-report; a durable pending line, a fingerprint or an ordinary reminder are
// exactly as readable as they always were.
func TestAOneShotOrPositiveDocumentIsBeyondTheSchemaThreeReader(t *testing.T) {
	if schemaDeferred != 3 {
		t.Fatalf("the deferred barrier is %d, wanted the 3 the predecessor reads", schemaDeferred)
	}
	if Schema != 4 {
		t.Fatalf("the newest schema is %d, wanted the 4 that fences the probe semantics", Schema)
	}
	if got := SchemaOf(Item{When: When{Kind: WhenProbe, OneShot: true}}); got != Schema {
		t.Fatalf("a one-shot condition is schema %d, wanted %d", got, Schema)
	}
	if got := SchemaOf(Item{Positive: "abc"}); got != Schema {
		t.Fatalf("a positive identity is schema %d, wanted %d", got, Schema)
	}
	// Native shapes an older build already reads stay where they were.
	if got := SchemaOf(Item{When: When{Kind: WhenAt}}); got != 1 {
		t.Fatalf("an at is schema %d, wanted 1", got)
	}
	if got := SchemaOf(Item{When: When{Kind: WhenEvery, Every: "1m"}}); got != 1 {
		t.Fatalf("an every is schema %d, wanted 1", got)
	}
	// The deferred fields of the PRECEDING barrier keep version 3, so a
	// schema-3 reader still reads them.
	if got := SchemaOf(Item{Pending: []Pending{{ID: "x", Kind: ActionSay, Text: "a line"}}}); got != schemaDeferred {
		t.Fatalf("a durable intent is schema %d, wanted the deferred barrier %d", got, schemaDeferred)
	}
}

// A FAILED INTENT SAVE LEAVES THE ITEM EXACTLY AS IT WAS, and the notice is
// retried and delivered once.
//
// THE FAULT IS REAL AND NARROW. The store root is made unwritable, which fails
// [Store.saveActive] at its existing temp+rename primitive while the item's own
// folder, the tick lock and every read still work. No testing knob is added to
// production code for this: the identity a yes earns is committed in the SAME
// guarded save as the delivery intent, and a save that fails rolls the in-memory
// copy back, so the item the outer pass would persist carries neither a positive
// nor a pending.
func TestAFailedIntentLeavesNoIdentityAndDeliversOnceOnRetry(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{evidence: `{"ready": true}`}
	made, err := store.Create(newProbe("notify me when ready becomes true"))
	if err != nil {
		t.Fatal(err)
	}

	// A first pass that CANNOT fire mints the tick lock file, so the read-only
	// root below still lets a later pass take that lock.
	unknown := func(_ context.Context, _ Judgment) (SentinelReading, string, float64, error) {
		return VerdictUnknown, "no clear answer", 0, nil
	}
	if pass := mustTick(t, probeTicker(store, runner, unknown, now)); pass.Fired != 0 {
		t.Fatalf("the minting pass fired: %+v", pass)
	}

	root := store.Root()
	if err := os.Chmod(root, 0o500); err != nil {
		t.Skipf("cannot make the store root read-only here: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })

	yes, _ := countingSentinel(VerdictYes)
	ticker := probeTicker(store, runner, yes, now)
	item, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	var pass Pass
	if err := ticker.one(context.Background(), &pass, &item); err == nil {
		t.Fatal("the intent save was expected to fail on a read-only store root")
	}
	if item.Positive != "" {
		t.Fatalf("a failed intent left a positive identity in the item: %q", item.Positive)
	}
	if len(item.Pending) != 0 {
		t.Fatalf("a failed intent left %d pending lines", len(item.Pending))
	}
	onDisk, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if onDisk.Positive != "" || len(onDisk.Pending) != 0 || onDisk.Runs != 0 {
		t.Fatalf("the failed pass changed the document: positive=%q pending=%d runs=%d",
			onDisk.Positive, len(onDisk.Pending), onDisk.Runs)
	}

	// THE WORLD IS WRITABLE AGAIN. The next pass retries the same look and
	// delivers exactly one line.
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	at := now.Add(6 * time.Minute)
	if pass := mustTick(t, probeTicker(store, runner, yes, at)); pass.Fired != 1 {
		t.Fatalf("the retry did not deliver once: %+v", pass)
	}
	if len(runner.said) != 1 {
		t.Fatalf("the retry delivered %d lines, wanted one", len(runner.said))
	}
	fired, _ := store.Get(made.ID)
	if fired.Positive == "" {
		t.Fatal("the delivered reading left no identity")
	}

	// AND THE UNCHANGED READING IS NOT DELIVERED AGAIN.
	at = now.Add(12 * time.Minute)
	if pass := mustTick(t, probeTicker(store, runner, yes, at)); pass.Fired != 0 {
		t.Fatalf("the unchanged positive re-fired after the retry: %+v", pass)
	}
	if len(runner.said) != 1 {
		t.Fatalf("the notice was delivered %d times, wanted once", len(runner.said))
	}
}

// oneShotProbe is newProbe with the compiled one-shot intent set.
func oneShotProbe(words string) Item {
	item := newProbe(words)
	item.When.OneShot = true
	return item
}

// A ONE-SHOT CONDITION DELIVERS ONCE AND RETIRES, so a later false-then-true is
// never a second notice however the probe's bytes vary.
func TestAOneShotConditionDeliversOnceAndRetires(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{evidence: `{"ready": true}`}
	made, err := store.Create(oneShotProbe("notify me once when ready becomes true"))
	if err != nil {
		t.Fatal(err)
	}
	if made.Schema != Schema || !made.When.OneShot {
		t.Fatalf("the one-shot item is schema %d once=%v, wanted %d and true", made.Schema, made.When.OneShot, Schema)
	}
	yes, _ := countingSentinel(VerdictYes)
	no, _ := countingSentinel(VerdictNo)

	if pass := mustTick(t, probeTicker(store, runner, yes, now)); pass.Fired != 1 || pass.Said != 1 {
		t.Fatalf("the one-shot first true did not deliver once: %+v", pass)
	}
	fired, _ := store.Get(made.ID)
	if fired.Status != StatusRetired || fired.RetiredWhy != "fired" {
		t.Fatalf("a fulfilled one-shot is %q because %q, wanted retired because fired", fired.Status, fired.RetiredWhy)
	}

	// FALSE, THEN TRUE WITH DIFFERENT BYTES: a retired item is not looked at.
	runner.evidence = `{"ready": false}`
	if pass := mustTick(t, probeTicker(store, runner, no, now.Add(6*time.Minute))); pass.Fired != 0 || pass.Examined != 0 {
		t.Fatalf("a retired one-shot was looked at: %+v", pass)
	}
	runner.evidence = `{"ready":   true}` // different whitespace, different bytes
	if pass := mustTick(t, probeTicker(store, runner, yes, now.Add(12*time.Minute))); pass.Fired != 0 {
		t.Fatalf("a fulfilled one-shot re-notified: %+v", pass)
	}
	if len(runner.said) != 1 {
		t.Fatalf("a one-shot delivered %d lines, wanted one", len(runner.said))
	}
}

// A LOST ONE-SHOT DELIVERY IS NOT RETIRED: its pending intent is repaired under
// the same identity, delivered exactly once, and only then retired.
func TestAOneShotLostDeliveryIsNotRetiredAndSettlesOnce(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &deliveringRunner{fakeRunner: fakeRunner{evidence: `{"ready": true}`}}
	runner.failFor = 1
	made, err := store.Create(oneShotProbe("notify me once when ready becomes true"))
	if err != nil {
		t.Fatal(err)
	}
	yes, _ := countingSentinel(VerdictYes)

	if pass := mustTick(t, probeTicker(store, runner, yes, now)); pass.Fired != 0 {
		t.Fatalf("a failed delivery was counted as a firing: %+v", pass)
	}
	stuck, _ := store.Get(made.ID)
	if stuck.Status != StatusActive || len(stuck.Pending) != 1 {
		t.Fatalf("a lost one-shot delivery was retired or lost: status=%q pending=%d", stuck.Status, len(stuck.Pending))
	}

	second := mustTick(t, probeTicker(store, runner, yes, now.Add(6*time.Minute)))
	if second.Fired != 1 {
		t.Fatalf("the waiting one-shot line was not settled once: %+v", second)
	}
	if len(runner.ids) != 2 || runner.ids[0] != runner.ids[1] {
		t.Fatalf("the one-shot did not reuse its delivery identity: %v", runner.ids)
	}
	settled, _ := store.Get(made.ID)
	if settled.Status != StatusRetired || len(settled.Pending) != 0 {
		t.Fatalf("a settled one-shot is %q with %d pending, wanted retired and clean", settled.Status, len(settled.Pending))
	}
	if runner.calls != 2 {
		t.Fatalf("wanted one failed attempt and one settlement, got %d calls", runner.calls)
	}
}

// A ONE-SHOT TASK WHOSE EDITS ARE AMBIGUOUS STOPS FOR THE PERSON AND IS NEVER
// REPLAYED.
func TestAOneShotTaskThatFailsStaysForThePersonAndIsNotReplayed(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{evidence: `{"ready": true}`, runErr: errors.New("the edits are ambiguous")}
	item := oneShotProbe("when ready becomes true, fix the migration")
	item.Does = Action{Kind: ActionTask, Brief: "fix the migration"}
	made, err := store.Create(item)
	if err != nil {
		t.Fatal(err)
	}
	yes, _ := countingSentinel(VerdictYes)

	first := mustTick(t, probeTicker(store, runner, yes, now))
	if first.Fired != 0 {
		t.Fatalf("a failed one-shot task was counted as a firing: %+v", first)
	}
	stuck, _ := store.Get(made.ID)
	if stuck.TaskInflight == nil {
		t.Fatal("a failed one-shot task lost its in-flight marker, so it could replay")
	}
	if stuck.NeedsPerson == "" {
		t.Fatalf("a failed one-shot task was not left for the person: %+v", stuck)
	}

	if second := mustTick(t, probeTicker(store, runner, yes, now.Add(6*time.Minute))); second.Fired != 0 {
		t.Fatalf("a task waiting on the person ran again: %+v", second)
	}
	if len(runner.runDirs) != 1 {
		t.Fatalf("the ambiguous task ran %d times, wanted exactly one attempt", len(runner.runDirs))
	}
}
