package serve

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// watchFloor is a service over a store in a temporary folder, on a clock the
// test moves, with a scripted manager.
//
// THE CLOCK STARTS AT THE REAL NOW AND ONLY MOVES FORWARD: the store stamps a
// wait's Since and Deadline on its own clock, which a test cannot hold, so the
// service's clock is moved past them instead.
type watchFloor struct {
	t  *testing.T
	st *store.Store
	h  *Handler

	mu    sync.Mutex
	now   time.Time
	calls []factory.Stall
	// turn is the scripted manager; nil answers one plain line.
	turn func(ctx context.Context, it factory.Item, st factory.Stall) (string, error)
	done chan struct{}
}

func newWatchFloor(t *testing.T, manager bool) *watchFloor {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &watchFloor{t: t, st: st, now: time.Now(), done: make(chan struct{}, 16)}
	cfg := Config{Version: "test", Store: st, Now: f.clock, LeaseOptions: lease.Options{Now: f.clock}}
	if manager {
		cfg.Stalled = func(ctx context.Context, it factory.Item, stall factory.Stall) (string, error) {
			f.mu.Lock()
			f.calls = append(f.calls, stall)
			turn := f.turn
			f.mu.Unlock()
			defer func() { f.done <- struct{}{} }()
			if turn != nil {
				return turn(ctx, it, stall)
			}
			return "I looked. Nothing I can press moves it.", nil
		}
	}
	f.h = New(factory.Seam{}, cfg)
	t.Cleanup(f.h.Close)
	return f
}

func (f *watchFloor) clock() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *watchFloor) advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

func (f *watchFloor) asked() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// pass is one settle of the floor, as the job timer runs it.
func (f *watchFloor) pass() {
	f.t.Helper()
	held, err := f.h.leases()
	if err != nil {
		f.t.Fatal(err)
	}
	f.h.settleJobs(false, held)
}

// turned waits for the manager turn in flight to end and its write to land.
func (f *watchFloor) turned(id, turns int) factory.Item {
	f.t.Helper()
	select {
	case <-f.done:
	case <-time.After(10 * time.Second):
		f.t.Fatal("the manager was never asked")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		it := f.job(id)
		if !f.h.stalls.busy(id) && (it.Stall == nil || it.Stall.Turns >= turns) {
			return it
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("the turn's write never landed: %+v", it.Stall)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (f *watchFloor) job(id int) factory.Item {
	f.t.Helper()
	it, err := f.st.Get(id)
	if err != nil {
		f.t.Fatal(err)
	}
	return it
}

func (f *watchFloor) create(it factory.Item) factory.Item {
	f.t.Helper()
	made, err := f.st.Create(it)
	if err != nil {
		f.t.Fatal(err)
	}
	return made
}

func watchStep(state factory.State, phase factory.PhaseState) factory.Item {
	return factory.Item{Title: "stalls", Repo: "acme/api", Kind: factory.KindIssue, Num: 7, State: state, Cap: 10, Assignee: "sam",
		Stream: &factory.Stream{Phases: []factory.Phase{{Name: "write", Kind: factory.StageChat, State: phase}}, Cur: 0}}
}

// queuedLate is a queued job whose wait for a computer has just run out.
func (f *watchFloor) queuedLate() factory.Item {
	it := f.create(watchStep(factory.StateQueued, factory.PhasePending))
	if it.Wait == nil || it.Wait.Who != factory.WaitRunner {
		f.t.Fatalf("a queued job waits on %+v", it.Wait)
	}
	f.advance(factory.WaitRunnerFor + time.Second)
	return it
}

// stalled is queuedLate seen by the watchdog and past its confirm window, so
// the next pass asks the manager.
func (f *watchFloor) stalled() factory.Item {
	it := f.queuedLate()
	f.pass()
	f.advance(factory.WatchConfirmFor)
	return it
}

func logHas(it factory.Item, words string) bool {
	if it.Stream == nil {
		return false
	}
	for _, line := range it.Stream.Log {
		if strings.Contains(line.Text, words) {
			return true
		}
	}
	return false
}

func TestWatchdogRecordsAStallAndAsksTheManagerOnceItHasStood(t *testing.T) {
	f := newWatchFloor(t, true)
	it := f.queuedLate()
	seen := f.clock()
	f.pass()
	got := f.job(it.ID)
	st := got.Stall
	if st == nil || st.Kind != factory.StallRunner || st.Wait != "runner:write" || st.Words == "" || !st.At.Equal(seen) || !st.NextAt.Equal(seen.Add(factory.WatchConfirmFor)) || st.Turns != 0 || st.GaveUp {
		t.Fatalf("first sighting recorded %+v", st)
	}
	if f.asked() != 0 || len(got.Stream.Log) != 0 || got.Wait.Who != factory.WaitRunner {
		t.Fatalf("a stall inside its confirm window reached someone: asked %d, log %v, wait %+v", f.asked(), got.Stream.Log, got.Wait)
	}
	// A pass inside the window changes nothing.
	f.advance(factory.WatchConfirmFor - time.Second)
	f.pass()
	if f.asked() != 0 {
		t.Fatal("the manager was asked inside the confirm window")
	}
	f.advance(time.Second)
	asked := f.clock()
	f.pass()
	got = f.turned(it.ID, 1)
	if f.asked() != 1 {
		t.Fatalf("the manager was asked %d times", f.asked())
	}
	st = got.Stall
	if st == nil || st.Turns != 1 || !st.NextAt.Equal(asked.Add(factory.StallBackoff[0])) || st.GaveUp {
		t.Fatalf("after the first turn: %+v", st)
	}
	if !logHas(got, "Asked the manager to look.") || !logHas(got, "Manager on the stall: I looked.") {
		t.Fatalf("the stall and the manager's words are not on the log: %+v", got.Stream.Log)
	}
	if got.Wait.Who != factory.WaitRunner {
		t.Fatalf("a stall the manager is still on reads %+v", got.Wait)
	}
	f.mu.Lock()
	call := f.calls[0]
	f.mu.Unlock()
	if call.Kind != factory.StallRunner || call.Wait != "runner:write" {
		t.Fatalf("the manager was handed %+v", call)
	}
	// Until the next look the manager is not asked again.
	f.advance(factory.StallBackoff[0] - time.Second)
	f.pass()
	if f.asked() != 1 || f.h.stalls.busy(it.ID) {
		t.Fatal("the manager was asked again before the back-off ran out")
	}
}

func TestWatchdogBacksOffAndHandsAStallNobodyMovedToThePerson(t *testing.T) {
	f := newWatchFloor(t, true)
	it := f.stalled()
	f.pass()
	got := f.turned(it.ID, 1)
	// The first back-off runs out with the job where it was: it is the
	// person's to decide, and the manager is asked again all the same.
	for turn, wait := range factory.StallBackoff {
		f.advance(wait)
		asked := f.clock()
		f.pass()
		got = f.turned(it.ID, turn+2)
		st := got.Stall
		if f.asked() != turn+2 || st == nil || st.Turns != turn+2 || !st.GaveUp {
			t.Fatalf("after back-off %d: asked %d, stall %+v", turn+1, f.asked(), st)
		}
		if !strings.HasSuffix(st.Words, factory.StallUnmoved) {
			t.Fatalf("words = %q", st.Words)
		}
		if w := got.Wait; w.Who != factory.WaitYou || w.Action != factory.ActComputer || w.Words != st.Words {
			t.Fatalf("a stall handed over reads %+v", w)
		}
		if turn+1 < len(factory.StallBackoff) {
			if !st.NextAt.Equal(asked.Add(factory.StallBackoff[turn+1])) {
				t.Fatalf("next look %v, want %v on from %v", st.NextAt, factory.StallBackoff[turn+1], asked)
			}
		} else if !st.NextAt.IsZero() {
			t.Fatalf("a stall out of turns is looked at again at %v", st.NextAt)
		}
	}
	if !logHas(got, "It now waits on its person to decide.") || !logHas(got, "Asked the manager again (turn 2 of 4).") {
		t.Fatalf("log: %+v", got.Stream.Log)
	}
	// Out of turns: nothing more is asked, however long it stands.
	f.advance(24 * time.Hour)
	f.pass()
	if f.asked() != len(factory.StallBackoff)+1 || f.h.stalls.busy(it.ID) {
		t.Fatalf("asked %d times", f.asked())
	}
	if at := factory.WatchAt(f.job(it.ID), factory.WatchFacts{}, f.clock()); !at.IsZero() {
		t.Fatalf("the timer still wakes for a stall the watchdog is done with: %v", at)
	}
}

func TestWatchdogHandsAStallToThePersonWhenTheTurnFails(t *testing.T) {
	f := newWatchFloor(t, true)
	f.turn = func(context.Context, factory.Item, factory.Stall) (string, error) {
		return "", errors.New("the provider\nrefused the call")
	}
	it := f.stalled()
	f.pass()
	got := f.turned(it.ID, 1)
	st := got.Stall
	if st == nil || !st.GaveUp || st.Turns != 1 || !strings.HasSuffix(st.Words, factory.StallTurnFailed) {
		t.Fatalf("after a failed turn: %+v", st)
	}
	if w := got.Wait; w.Who != factory.WaitYou || w.Action != factory.ActComputer || w.Words != st.Words {
		t.Fatalf("wait = %+v", w)
	}
	if !logHas(got, "The manager's turn on the stall failed: the provider refused the call") || !logHas(got, "It now waits on its person to decide.") {
		t.Fatalf("log: %+v", got.Stream.Log)
	}
}

func TestWatchdogDropsAStallWhenTheJobMovesOn(t *testing.T) {
	f := newWatchFloor(t, true)
	// The manager's turn moves the job: a computer takes it.
	f.turn = func(_ context.Context, it factory.Item, _ factory.Stall) (string, error) {
		err := f.st.Update(it.ID, func(cur *factory.Item) error {
			cur.State = factory.StateRunning
			cur.Stream.Phases[0].State = factory.PhaseRunning
			return nil
		})
		return "Started it again.", err
	}
	it := f.stalled()
	f.pass()
	got := f.turned(it.ID, 1)
	if got.Stall != nil || got.Wait.Who != factory.WaitWorking {
		t.Fatalf("a job that moved keeps its stall: %+v, wait %+v", got.Stall, got.Wait)
	}
	if !logHas(got, "Manager on the stall: Started it again.") || !logHas(got, "The job moved on from its stall.") {
		t.Fatalf("log: %+v", got.Stream.Log)
	}
	f.advance(time.Hour)
	f.h.watchJobs([]factory.Item{f.job(it.ID)}, f.h.watchFacts([]lease.Lease{{Job: it.ID}}), f.clock())
	if f.asked() != 1 || f.job(it.ID).Stall != nil {
		t.Fatal("a job that moved on was looked at again")
	}
}

func TestWatchdogDropsAStallWhoseCauseIsGone(t *testing.T) {
	f := newWatchFloor(t, true)
	it := f.create(watchStep(factory.StateRunning, factory.PhaseRunning))
	f.advance(factory.WatchWorkingFor + time.Second)
	// No lease holds the step.
	f.h.watchJobs([]factory.Item{f.job(it.ID)}, f.h.watchFacts(nil), f.clock())
	got := f.job(it.ID)
	if got.Stall == nil || got.Stall.Kind != factory.StallUnheld {
		t.Fatalf("a step nobody runs: %+v", got.Stall)
	}
	// A hold lent to the manager for a reply runs no step.
	f.h.watchJobs([]factory.Item{got}, f.h.watchFacts([]lease.Lease{{Job: it.ID, ThreadOnly: true}}), f.clock())
	if f.job(it.ID).Stall == nil {
		t.Fatal("a thread-only hold cleared the stall")
	}
	// Its runner comes back inside the confirm window: nobody hears of it.
	f.advance(factory.WatchConfirmFor)
	f.h.watchJobs([]factory.Item{got}, f.h.watchFacts([]lease.Lease{{Job: it.ID}}), f.clock())
	got = f.job(it.ID)
	if got.Stall != nil || f.asked() != 0 || len(got.Stream.Log) != 0 {
		t.Fatalf("a stall whose cause is gone: %+v, asked %d, log %v", got.Stall, f.asked(), got.Stream.Log)
	}
	// And a held step is never a stall, however long it runs.
	f.advance(6 * time.Hour)
	f.h.watchJobs([]factory.Item{got}, f.h.watchFacts([]lease.Lease{{Job: it.ID}}), f.clock())
	if f.job(it.ID).Stall != nil {
		t.Fatal("a held step was recorded as a stall")
	}
}

func TestWatchdogTakesBackAStallItHandedOverWhenItsCauseIsGone(t *testing.T) {
	f := newWatchFloor(t, false)
	it := f.create(watchStep(factory.StateRunning, factory.PhaseRunning))
	f.advance(factory.WatchWorkingFor + time.Second)
	f.h.watchJobs([]factory.Item{f.job(it.ID)}, f.h.watchFacts(nil), f.clock())
	f.advance(factory.WatchConfirmFor)
	f.h.watchJobs([]factory.Item{f.job(it.ID)}, f.h.watchFacts(nil), f.clock())
	got := f.job(it.ID)
	if got.Stall == nil || !got.Stall.GaveUp || got.Wait.Action != factory.ActComputer {
		t.Fatalf("stall %+v, wait %+v", got.Stall, got.Wait)
	}
	// The person it now waits on is offered a way to restart the step.
	if err := factory.WaitFault(got); err != nil {
		t.Fatalf("a stall handed to the person offers no way out: %v (offers %v)", err, got.Offers)
	}
	f.h.watchJobs([]factory.Item{got}, f.h.watchFacts([]lease.Lease{{Job: it.ID}}), f.clock())
	got = f.job(it.ID)
	if got.Stall != nil || got.Wait.Who != factory.WaitWorking || !logHas(got, "No longer stalled: a computer is running it again.") {
		t.Fatalf("stall %+v, wait %+v, log %v", got.Stall, got.Wait, got.Stream.Log)
	}
}

func TestWatchdogWithNoManagerGoesStraightToThePerson(t *testing.T) {
	f := newWatchFloor(t, false)
	it := f.stalled()
	f.pass()
	got := f.job(it.ID)
	st := got.Stall
	if st == nil || !st.GaveUp || st.Turns != 0 || !st.NextAt.IsZero() || !strings.HasSuffix(st.Words, factory.StallNoManager) {
		t.Fatalf("stall = %+v", st)
	}
	if w := got.Wait; w.Who != factory.WaitYou || w.Action != factory.ActComputer || w.Words != st.Words {
		t.Fatalf("wait = %+v", w)
	}
	if !logHas(got, "It now waits on its person to decide.") {
		t.Fatalf("log: %+v", got.Stream.Log)
	}
}

func TestWatchdogGivesNoTurnToAJobWhoseBudgetIsSpent(t *testing.T) {
	f := newWatchFloor(t, true)
	in := watchStep(factory.StateQueued, factory.PhasePending)
	in.Stream.Spent = in.Cap
	it := f.create(in)
	f.advance(factory.WaitRunnerFor + time.Second)
	f.pass()
	f.advance(factory.WatchConfirmFor)
	f.pass()
	got := f.job(it.ID)
	st := got.Stall
	if st == nil || !st.GaveUp || st.Turns != 0 || !st.NextAt.IsZero() || !strings.HasSuffix(st.Words, factory.StallNoBudget) {
		t.Fatalf("stall = %+v", st)
	}
	if f.asked() != 0 || f.h.stalls.busy(it.ID) {
		t.Fatal("a job with no budget left was given a manager turn")
	}
	if got.Wait.Who != factory.WaitYou || got.Wait.Action != factory.ActComputer {
		t.Fatalf("wait = %+v", got.Wait)
	}
}

// A job waiting its turn behind other work is in a queue that is moving, and
// is no stall however long it waits. It becomes one when a slot is free and
// still nothing takes it.
func TestWatchdogLeavesAJobQueuedBehindOtherWorkAlone(t *testing.T) {
	f := newWatchFloor(t, true)
	it := f.queuedLate()
	full := []machineCapacity{{owner: "sam", machine: "desk", slots: 1, used: 1}}
	free := []machineCapacity{{owner: "sam", machine: "desk", slots: 2, used: 1}}
	other := []machineCapacity{{owner: "kim", machine: "tower", slots: 1, used: 1}}
	if !queuedBehindWork(it, full) || queuedBehindWork(it, free) || queuedBehindWork(it, other) || queuedBehindWork(it, nil) {
		t.Fatalf("queued behind work: full %v, a slot free %v, someone else's computer %v, no computer %v",
			queuedBehindWork(it, full), queuedBehindWork(it, free), queuedBehindWork(it, other), queuedBehindWork(it, nil))
	}
	with := func(machines []machineCapacity) *watchFacts {
		facts := f.h.watchFacts(nil)
		facts.capOnce.Do(func() { facts.capacity = machines })
		return facts
	}
	f.advance(time.Hour)
	f.h.watchJobs([]factory.Item{f.job(it.ID)}, with(full), f.clock())
	if got := f.job(it.ID); got.Stall != nil {
		t.Fatalf("a job waiting its turn was recorded as a stall: %+v", got.Stall)
	}
	if at := jobTimerDue([]factory.Item{f.job(it.ID)}, f.clock(), with(full).of); !at.Equal(f.clock().Add(factory.WatchWorkingFor)) {
		t.Fatalf("the timer comes round %v on, want the watchdog's period", at.Sub(f.clock()))
	}
	// A slot is free and the job is still queued: that is a stall.
	f.h.watchJobs([]factory.Item{f.job(it.ID)}, with(free), f.clock())
	if got := f.job(it.ID); got.Stall == nil || got.Stall.Kind != factory.StallRunner {
		t.Fatalf("a queued job nothing takes: %+v", got.Stall)
	}
	// The slot fills with other work before the manager is asked: it goes.
	f.advance(factory.WatchConfirmFor)
	f.h.watchJobs([]factory.Item{f.job(it.ID)}, with(full), f.clock())
	if got := f.job(it.ID); got.Stall != nil || f.asked() != 0 {
		t.Fatalf("stall %+v, asked %d", got.Stall, f.asked())
	}
}

func TestWatchdogLeavesAPutAwayWaitAlone(t *testing.T) {
	f := newWatchFloor(t, true)
	in := watchStep(factory.StateQueued, factory.PhasePending)
	in.Stream.Paused = true
	it := f.create(in)
	f.advance(48 * time.Hour)
	f.pass()
	if got := f.job(it.ID); got.Stall != nil || !got.Wait.PutAway {
		t.Fatalf("a held job: stall %+v, wait %+v", got.Stall, got.Wait)
	}
}

func watchPullRequest() factory.Item {
	it := watchStep(factory.StateShipped, factory.PhaseDone)
	it.PullRequest = &factory.PullRequest{Number: 9, State: factory.PROpen, HeadSHA: "abc", ChecksRead: true, ReviewsRead: true, Checks: factory.PRChecks{Pending: 2}}
	it.FollowUp = &factory.PRFollowUp{Kind: "pr", State: "watching", Reason: "Watching CI, review feedback and conflicts."}
	return it
}

func TestWatchdogKeepsAQuietPullRequestTheFactoryStillReads(t *testing.T) {
	f := newWatchFloor(t, true)
	it := f.create(watchPullRequest())
	if it.Wait.Who != factory.WaitGitHub || it.Wait.Deadline.IsZero() {
		t.Fatalf("wait = %+v", it.Wait)
	}
	f.advance(factory.WaitGitHubFor + time.Minute)
	// The poll read GitHub a minute ago and found nothing changed, so it
	// wrote nothing on the job.
	if err := f.st.SetSourceMeta(factorygithub.Name, store.SourceMeta{Polled: f.clock().Add(-time.Minute), Tried: f.clock().Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	before, _ := f.st.Revision(it.ID)
	f.pass()
	got := f.job(it.ID)
	if got.Stall != nil || f.asked() != 0 {
		t.Fatalf("a pull request the factory still reads is a stall: %+v", got.Stall)
	}
	if after, _ := f.st.Revision(it.ID); after != before {
		t.Fatalf("a quiet pull request was written: revision %d, was %d", after, before)
	}
	// The timer comes round when the last read is twenty minutes old, not
	// every two seconds for a deadline that has passed.
	read := f.clock().Add(-time.Minute)
	if at := jobTimerDue([]factory.Item{got}, f.clock(), f.h.lastFacts().of); !at.Equal(read.Add(factory.WaitGitHubFor)) {
		t.Fatalf("the timer wakes %v on, want %v", at.Sub(f.clock()), factory.WaitGitHubFor-time.Minute)
	}
	// The poll stops: twenty minutes on, the pull request is unread.
	f.advance(factory.WaitGitHubFor + time.Minute)
	f.pass()
	got = f.job(it.ID)
	if got.Stall == nil || got.Stall.Kind != factory.StallGitHub {
		t.Fatalf("an unread pull request: %+v", got.Stall)
	}
	// GitHub is read again before the manager is asked: the stall goes.
	if err := f.st.SetSourceMeta(factorygithub.Name, store.SourceMeta{Polled: f.clock().Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	f.advance(factory.WatchConfirmFor)
	f.pass()
	if got = f.job(it.ID); got.Stall != nil || f.asked() != 0 {
		t.Fatalf("a pull request read again keeps its stall: %+v, asked %d", got.Stall, f.asked())
	}
}

func TestWatchdogStoresAWaitThatWasNeverStored(t *testing.T) {
	f := newWatchFloor(t, true)
	it := f.create(watchStep(factory.StateRunning, factory.PhaseRunning))
	// A job saved before waits were stored: its document carries none.
	path := f.st.ItemPath(it.ID)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var item map[string]json.RawMessage
	if err := json.Unmarshal(doc["item"], &item); err != nil {
		t.Fatal(err)
	}
	delete(item, "Wait")
	delete(item, "Offers")
	doc["item"], _ = json.Marshal(item)
	raw, _ = json.Marshal(doc)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	old := f.job(it.ID)
	if old.Wait != nil {
		t.Fatalf("the fixture still stores a wait: %+v", old.Wait)
	}
	if at := factory.WatchAt(old, factory.WatchFacts{Held: true}, f.clock()); !at.Equal(f.clock()) {
		t.Fatalf("the timer does not wake for a job with no stored wait: %v", at)
	}
	f.h.watchJobs([]factory.Item{old}, f.h.watchFacts([]lease.Lease{{Job: it.ID}}), f.clock())
	got := f.job(it.ID)
	if got.Wait == nil || got.Wait.Who != factory.WaitWorking || got.Stall != nil || !got.Changed.Equal(old.Changed) {
		t.Fatalf("wait %+v, stall %+v, changed %v (was %v)", got.Wait, got.Stall, got.Changed, old.Changed)
	}
}

func TestWatchdogSeesAnApprovalAutoGivesAndHasNot(t *testing.T) {
	// This service has no door to publish through, so the approval Auto would
	// give is never given.
	f := newWatchFloor(t, false)
	it := f.create(factory.Item{Title: "auto", Repo: "acme/api", Kind: factory.KindIssue, Num: 8, Autonomy: factory.AutonomyAuto, State: factory.StateLanded, Assignee: "sam", Cap: 10,
		HeadCommit: strings.Repeat("a", 40), Stages: []factory.Stage{{Name: "approve", Kind: factory.StageGate, On: true}},
		Proof: []factory.Claim{{Text: "regression passes", OK: true, Evidence: "test exit 0"}}, Stream: &factory.Stream{Spent: 1}})
	if !f.h.autoPresses(it) {
		t.Fatalf("the fixture is not an approval Auto gives: wait %+v", it.Wait)
	}
	if at := jobTimerDue([]factory.Item{it}, f.clock(), f.h.lastFacts().of); !at.Equal(it.Wait.Since.Add(factory.WatchAutoFor)) {
		t.Fatalf("the timer wakes at %v, want %v", at, it.Wait.Since.Add(factory.WatchAutoFor))
	}
	f.pass()
	if got := f.job(it.ID); got.Stall != nil {
		t.Fatalf("stalled at once: %+v", got.Stall)
	}
	f.advance(factory.WatchAutoFor + time.Second)
	f.pass()
	got := f.job(it.ID)
	if got.Stall == nil || got.Stall.Kind != factory.StallAuto {
		t.Fatalf("stall = %+v, wait %+v", got.Stall, got.Wait)
	}
	// In Manual the approval is the person's, and the stall goes.
	if err := f.st.Update(it.ID, func(cur *factory.Item) error { cur.Autonomy = factory.AutonomyManual; return nil }); err != nil {
		t.Fatal(err)
	}
	f.pass()
	if got = f.job(it.ID); got.Stall != nil {
		t.Fatalf("a person's approval keeps Auto's stall: %+v", got.Stall)
	}
}

func TestTheJobTimerWakesForTheWatchdogsClocks(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	stored := func(it factory.Item, at time.Time) factory.Item {
		factory.SettleWait(factory.Item{}, &it, at)
		return it
	}
	queued := stored(watchStep(factory.StateQueued, factory.PhasePending), now)
	late := stored(watchStep(factory.StateQueued, factory.PhasePending), now.Add(-time.Hour))
	asked := watchStep(factory.StateRunning, factory.PhaseRunning)
	asked.Asking = &factory.Asked{Stage: "write", Question: "Which table?", With: factory.AskedManager}
	asked = stored(asked, now)
	running := stored(watchStep(factory.StateRunning, factory.PhaseRunning), now.Add(-time.Hour))
	putAway := watchStep(factory.StateQueued, factory.PhasePending)
	putAway.Stream.Paused = true
	putAway = stored(putAway, now.Add(-time.Hour))
	stalled := late
	stalled.Stall = &factory.Stall{Kind: factory.StallRunner, Wait: "runner:write", At: now, NextAt: now.Add(7 * time.Minute)}
	due := late
	due.Stall = &factory.Stall{Kind: factory.StallRunner, Wait: "runner:write", At: now.Add(-time.Hour), NextAt: now.Add(-time.Minute)}
	finished := late
	finished.Stall = &factory.Stall{Kind: factory.StallRunner, Wait: "runner:write", At: now.Add(-time.Hour), GaveUp: true}
	unstored := watchStep(factory.StateQueued, factory.PhasePending)
	// What the service knows beside the documents moves the clock too.
	held := func(factory.Item) factory.WatchFacts { return factory.WatchFacts{Held: true} }
	if got := jobTimerDue([]factory.Item{running}, now, held); !got.Equal(now.Add(factory.WatchWorkingFor)) {
		t.Errorf("a held step is looked at again %v on, want the watchdog's period", got.Sub(now))
	}
	busy := func(factory.Item) factory.WatchFacts { return factory.WatchFacts{Busy: true} }
	if got := jobTimerDue([]factory.Item{late}, now, busy); !got.Equal(now.Add(factory.WatchWorkingFor)) {
		t.Errorf("a late job in a moving queue is looked at again %v on, want the watchdog's period", got.Sub(now))
	}
	for _, c := range []struct {
		name string
		it   factory.Item
		want time.Time
	}{
		{"a queued job's deadline", queued, now.Add(factory.WaitRunnerFor)},
		{"a held question's deadline", asked, now.Add(factory.WaitMainAgentFor)},
		{"a deadline already past", late, now.Add(2 * time.Second)},
		{"a step under way that nothing is known to hold", running, now.Add(2 * time.Second)},
		{"a stall's next look", stalled, now.Add(7 * time.Minute)},
		{"a stall whose look is due", due, now.Add(2 * time.Second)},
		{"a job with no stored wait", unstored, now.Add(2 * time.Second)},
		{"a stall the watchdog is done with", finished, time.Time{}},
		{"a wait its person put away", putAway, time.Time{}},
	} {
		if got := jobTimerDue([]factory.Item{c.it}, now, nil); !got.Equal(c.want) {
			t.Errorf("%s: due %v, want %v", c.name, got.Sub(now), c.want.Sub(now))
		}
	}
}

// A floor remembered as idle is looked at again when a wait's deadline comes,
// with no document changed and no request but the timer's.
func TestARememberedFloorIsLookedAtAgainWhenAWaitIsLate(t *testing.T) {
	f := newWatchFloor(t, false)
	it := f.create(watchStep(factory.StateQueued, factory.PhasePending))
	f.pass()
	f.h.jobsMu.Lock()
	kept, timer := f.h.floorKept, f.h.jobsTimer
	f.h.jobsMu.Unlock()
	if kept.sig == "" || !kept.next.Equal(it.Wait.Deadline) {
		t.Fatalf("the idle floor is remembered until %v, want the wait's deadline %v", kept.next, it.Wait.Deadline)
	}
	if timer == nil {
		t.Fatal("no timer is armed for the wait's deadline")
	}
	if !f.h.floorUnchanged() {
		t.Fatal("the floor is not idle before the deadline")
	}
	f.advance(factory.WaitRunnerFor + time.Second)
	if f.h.floorUnchanged() {
		t.Fatal("the floor still reads idle past the deadline")
	}
	f.pass()
	if got := f.job(it.ID); got.Stall == nil || got.Stall.Kind != factory.StallRunner {
		t.Fatalf("the late wait was not seen: %+v", got.Stall)
	}
}

// Closing the service ends a manager turn in flight and leaves its stall for
// the next service to ask about again.
func TestClosingEndsAManagerTurnOnAStall(t *testing.T) {
	f := newWatchFloor(t, true)
	began := make(chan struct{})
	f.turn = func(ctx context.Context, _ factory.Item, _ factory.Stall) (string, error) {
		close(began)
		<-ctx.Done()
		return "", ctx.Err()
	}
	it := f.stalled()
	f.pass()
	<-began
	f.h.Close()
	got := f.job(it.ID)
	if got.Stall == nil || got.Stall.Turns != 0 || got.Stall.GaveUp {
		t.Fatalf("a turn the close ended was counted: %+v", got.Stall)
	}
	if !got.Stall.NextAt.Equal(f.clock().Add(watchTurnFor)) {
		t.Fatalf("the lost turn is given again at %v", got.Stall.NextAt)
	}
}
