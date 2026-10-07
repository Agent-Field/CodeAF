package standing

// A PROBE'S NEXT DUE IS THE PASS'S MOMENT, NOT THE LOOK'S.
//
// [Interval] is the only clock in this build: the operating system's timer and
// every open window's own ticker run one pass five minutes apart, and a probe
// merely asks whether a pass may take its look. The look itself happens a beat
// AFTER the pass began -- a lock taken, a settlement, an earlier item's slow
// probe -- and the moment it happened is a reading of this process's clock, not
// a point on the pass grid. These tests pin the two ways writing that reading
// into the item's phase loses a look:
//
//   - the NEXT pass, which is the native timer's own next grid point, lands a
//     few milliseconds before the due the look wrote, so the item reads as
//     asleep and the look is skipped for a whole interval (the observed field
//     case: lastChecked 05:49:24.797238166, nextDue 05:54:24.797238166, the
//     05:54:24 wake checked 0), and
//   - an item reached late, behind a look that took a minute, would be pushed a
//     cadence past the next pass for no reason but the item in front of it.
//
// The fix anchors the due one cadence past the PASS, so a look every five
// minutes is taken every five minutes, and no burst answers a sleepy machine.

import (
	"context"
	"testing"
	"time"
)

// mustNano reads a wall-clock moment written by the native watch's own record.
func mustNano(t *testing.T, rfc string) time.Time {
	t.Helper()
	moment, err := time.Parse(time.RFC3339Nano, rfc)
	if err != nil {
		t.Fatalf("cannot read %q: %v", rfc, err)
	}
	return moment
}

// readyProbe is the compiled shape of the five-minute readiness watch: a probe
// that says yes when the endpoint reports ready, optionally once.
func readyProbe(words string, every time.Duration, once bool) Item {
	item := reminder(words, time.Time{})
	item.When = When{
		Kind:       WhenProbe,
		Words:      words,
		Probe:      Probe{Command: "curl -s --max-time 10 http://127.0.0.1:18777/ready"},
		ProbeEvery: every,
		OneShot:    once,
	}
	item.Does = Action{Kind: ActionSay, Say: "it is ready: {{evidence}}"}
	return item
}

// TestTheNextNativePassTakesAProbeWhoseDueWasWrittenByTheLook is the observed
// field skip, made deterministic: created at 05:46:01, looked at on the pass
// that began at 05:49:24.797238166, and the very next pass -- the native
// timer's own grid, a few milliseconds before the due the look wrote -- must
// take the look rather than find the item asleep.
func TestTheNextNativePassTakesAProbeWhoseDueWasWrittenByTheLook(t *testing.T) {
	created := mustNano(t, "2026-10-06T05:46:01.309853008Z")
	first := mustNano(t, "2026-10-06T05:49:24.797238166Z")
	// One whole Interval after the pass that took the look: the native timer's
	// next grid point, arriving a couple of milliseconds early on this process's
	// clock, which is what the wake log showed.
	second := first.Add(Interval - 2*time.Millisecond)

	clock := created
	store := openStore(t, created)
	store.clock = func() time.Time { return clock }
	item, err := store.Create(readyProbe("notify me once when the endpoint reports ready", Interval, true))
	if err != nil {
		t.Fatal(err)
	}

	runner := &fakeRunner{evidence: "ready: false"}
	sentinel, asked := answers(false, true)
	clock = first
	ticker := &Ticker{Store: store, Runner: runner, Now: func() time.Time { return clock }}
	ticker.Sentinel = sentinel

	pass := mustTick(t, ticker)
	if pass.Checked != 1 || pass.Fired != 0 || runner.probeSeen != 1 {
		t.Fatalf("the first pass is %+v with %d probes", pass, runner.probeSeen)
	}
	looked, err := store.Get(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !looked.LastChecked.Equal(first) {
		t.Fatalf("the first look was not remembered: %s", looked.LastChecked)
	}
	// The due the look wrote must be on or before the very next pass, whichever
	// instant within this pass the item happened to be reached.
	if looked.NextDue.After(first.Add(Interval)) {
		t.Fatalf("the next look is due %s, past the next pass %s", looked.NextDue, first.Add(Interval))
	}

	clock = second
	runner.evidence = "ready: true"
	pass = mustTick(t, ticker)
	if pass.Checked != 1 || pass.Fired != 1 || runner.probeSeen != 2 {
		t.Fatalf("the pass after the look skipped it: %+v with %d probes and %d judgments", pass, runner.probeSeen, *asked)
	}
	if len(runner.said) != 1 {
		t.Fatalf("the one-shot condition said %v", runner.said)
	}
	fired, err := store.Get(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fired.Status != StatusRetired || fired.Runs != 1 {
		t.Fatalf("the look that turned true did not fire the one-shot: %+v", fired)
	}
}

// TestASlowLookDoesNotCostTheItemBehindItItsOwnPass pins the second way the
// look's own reading corrupts a phase. Both items ride the five-minute pass;
// the one walked first answers for a minute and a half, so the item behind it
// is reached long after the pass began. Its next look is still one cadence past
// the PASS -- due on the normal next pass -- and not one cadence past the slow
// look it had to wait behind.
func TestASlowLookDoesNotCostTheItemBehindItItsOwnPass(t *testing.T) {
	created := mustNano(t, "2026-10-06T05:46:01.309853008Z")
	first := mustNano(t, "2026-10-06T05:49:24.797238166Z")
	second := first.Add(Interval) // the normal next native pass, one cadence on
	const slow = 90 * time.Second

	clock := created
	store := openStore(t, created)
	store.clock = func() time.Time { return clock }

	under, err := store.Create(readyProbe("the item behind the slow look", Interval, true))
	if err != nil {
		t.Fatal(err)
	}
	// Created a beat later, so it is walked FIRST (a pass walks newest first).
	clock = created.Add(time.Second)
	decoy, err := store.Create(readyProbe("the item whose look is slow", Interval, false))
	if err != nil {
		t.Fatal(err)
	}

	runner := &fakeRunner{evidence: "ready: false"}
	slowRunner := &delayedProbe{fakeRunner: runner, slow: decoy.ID, delay: slow, clock: func() { clock = clock.Add(slow) }}
	sentinel, _ := answers(false, false, false, true)
	clock = first
	ticker := &Ticker{Store: store, Runner: slowRunner, Now: func() time.Time { return clock }}
	ticker.Sentinel = sentinel

	pass := mustTick(t, ticker)
	if pass.Checked != 2 || pass.Fired != 0 {
		t.Fatalf("the first pass is %+v, wanted both items looked at and nothing fired", pass)
	}
	behind, err := store.Get(under.ID)
	if err != nil {
		t.Fatal(err)
	}
	if behind.LastChecked.Equal(first) {
		t.Fatalf("the item behind the slow look was reached at the pass's own moment: %s", behind.LastChecked)
	}
	if behind.NextDue.After(second) {
		t.Fatalf("the slow look pushed the next due to %s, past the next pass %s", behind.NextDue, second)
	}

	// The normal next pass, exactly one cadence on: both items are due, and the
	// one the slow look delayed fires on the yes it was owed.
	clock = second
	runner.evidence = "ready: true"
	pass = mustTick(t, ticker)
	if pass.Checked != 2 || pass.Fired != 1 {
		t.Fatalf("the next pass is %+v, wanted both items looked at and one firing", pass)
	}
	fired, err := store.Get(under.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fired.Status != StatusRetired || fired.Runs != 1 {
		t.Fatalf("the delayed item did not fire on the next pass: %+v", fired)
	}
}

// delayedProbe is a Runner whose look for one item takes time: after answering,
// it moves the test's clock, as a real look's cost does before the next item in
// the walk is reached.
type delayedProbe struct {
	*fakeRunner
	slow  string
	delay time.Duration
	clock func()
}

func (d *delayedProbe) Probe(ctx context.Context, item Item) (ProbeReading, error) {
	out, err := d.fakeRunner.Probe(ctx, item)
	if item.ID == d.slow && d.clock != nil {
		d.clock()
	}
	return out, err
}
