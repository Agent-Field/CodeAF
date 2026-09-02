package remote

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// railAgent is a [fakeAgent] that carries the standing task lane the real
// session agent has: a roster replayed onto every subscription as it opens, and
// live updates fanned out to all of them afterwards.
type railAgent struct {
	*fakeAgent

	mu      sync.Mutex
	roster  []session.Event
	lanes   []chan session.Event
	pending []uint64
	held    []uint64
	watches int
}

func (r *railAgent) WatchTaskUpdates() (<-chan session.Event, func()) {
	lane := make(chan session.Event, 64)
	r.mu.Lock()
	r.watches++
	for _, event := range r.roster {
		lane <- event
	}
	r.lanes = append(r.lanes, lane)
	r.mu.Unlock()
	var once sync.Once
	return lane, func() { once.Do(func() { r.drop(lane) }) }
}

func (r *railAgent) drop(lane chan session.Event) {
	r.mu.Lock()
	for at, held := range r.lanes {
		if held == lane {
			r.lanes = append(r.lanes[:at], r.lanes[at+1:]...)
			break
		}
	}
	r.mu.Unlock()
	close(lane)
}

// land is one update the far conversation emits with no turn running — the
// shape a task started by hand takes.
func (r *railAgent) land(event session.Event) {
	r.mu.Lock()
	r.roster = append(r.roster, event)
	lanes := append([]chan session.Event(nil), r.lanes...)
	r.mu.Unlock()
	for _, lane := range lanes {
		lane <- event
	}
}

func (r *railAgent) PendingTasks() []uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]uint64(nil), r.pending...)
}

func (r *railAgent) HoldTask(id uint64) {
	r.mu.Lock()
	r.held = append(r.held, id)
	lanes := append([]chan session.Event(nil), r.lanes...)
	r.mu.Unlock()
	held := session.Event{
		Kind: session.EventTaskProposal,
		Tool: "propose_task",
		Task: &session.TaskNotice{ID: id},
	}
	for _, lane := range lanes {
		lane <- held
	}
}

func (r *railAgent) heldTask(id uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.held) == 1 && r.held[0] == id
}

func (r *railAgent) opened() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.watches
}

func taskEvent(id uint64, title string, state session.TaskState) session.Event {
	return session.Event{
		Kind: session.EventTaskUpdate,
		Tool: "propose_task",
		Task: &session.TaskNotice{ID: id, Title: title, State: state},
	}
}

// nextTask takes one update off a surface's lane, or fails the test rather than
// hanging the suite on a lane that never spoke.
func nextTask(t *testing.T, lane <-chan session.Event) session.Event {
	t.Helper()
	select {
	case event, ok := <-lane:
		if !ok {
			t.Fatal("the task lane closed with nothing on it")
		}
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("nothing arrived on the task lane")
	}
	return session.Event{}
}

// THE BUG THIS PINS: `/task solo …` over a connection started work on the far
// machine, answered "started", ran it to completion — and put no row on the rail
// of the person who typed it. A task command is a CALL and not a turn, so its
// updates went out on the standing lane only, which nothing carried.
func TestAHandStartedTaskReachesTheHostedRail(t *testing.T) {
	far := &railAgent{fakeAgent: &fakeAgent{}}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, Workspace: "/srv/app", SessionFile: "/srv/app/j.jsonl"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })

	lane, stop := loop.Client.Agent().WatchTaskUpdates()
	t.Cleanup(stop)
	waitFor(t, "the engine opened the surface's task lane", func() bool { return far.opened() == 1 })

	id, _, _, err := loop.Client.Agent().StartTask(context.Background(), "write hello into /tmp/room-proof.txt")
	if err != nil {
		t.Fatal(err)
	}
	far.land(taskEvent(id, "write hello", session.TaskRunning))

	event := nextTask(t, lane)
	if event.Kind != session.EventTaskUpdate || event.Task == nil {
		t.Fatalf("the lane carried %v, not a task update", event.Kind)
	}
	if event.Task.ID != id || event.Task.State != session.TaskRunning {
		t.Fatalf("row = %d %q, want %d running", event.Task.ID, event.Task.State, id)
	}

	// AND THE LANDING ARRIVES TOO, which is the half that happens minutes later
	// with no turn open anywhere.
	far.land(taskEvent(id, "write hello", session.TaskDone))
	if done := nextTask(t, lane); done.Task == nil || done.Task.State != session.TaskDone {
		t.Fatalf("landing = %v", done.Task)
	}
}

// A window that arrives after the work started still learns every row, because
// the engine's subscription replays the roster as it opens.
func TestAHostedRailOpenedLateStillGetsTheRoster(t *testing.T) {
	far := &railAgent{fakeAgent: &fakeAgent{}}
	far.roster = []session.Event{
		taskEvent(7, "port the parser", session.TaskDone),
		taskEvent(8, "write hello", session.TaskRunning),
	}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })

	lane, stop := loop.Client.Agent().WatchTaskUpdates()
	t.Cleanup(stop)
	for _, want := range []uint64{7, 8} {
		if event := nextTask(t, lane); event.Task == nil || event.Task.ID != want {
			t.Fatalf("roster row = %v, want %d", event.Task, want)
		}
	}
}

// TAKING UP A SECOND CONVERSATION REPLACES THE LANE AND NEVER ADDS ONE. Two
// pumps on one channel would each take half the events, which is a rail that
// silently drops rows.
func TestWatchingTasksTwiceReplacesTheHostedLane(t *testing.T) {
	far := &railAgent{fakeAgent: &fakeAgent{}}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })

	first, _ := loop.Client.Agent().WatchTaskUpdates()
	waitFor(t, "the engine opened the surface's task lane", func() bool { return far.opened() == 1 })
	second, stop := loop.Client.Agent().WatchTaskUpdates()
	t.Cleanup(stop)
	waitFor(t, "the engine reopened the surface's task lane", func() bool { return far.opened() == 2 })

	select {
	case _, ok := <-first:
		if ok {
			t.Fatal("the replaced lane delivered an event")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the replaced lane never closed")
	}

	far.land(taskEvent(3, "still watching", session.TaskRunning))
	if event := nextTask(t, second); event.Task == nil || event.Task.ID != 3 {
		t.Fatalf("second lane = %v", event.Task)
	}
	// The far end holds exactly one subscription for this surface: the first was
	// left when the second replaced it.
	far.mu.Lock()
	open := len(far.lanes)
	far.mu.Unlock()
	if open != 1 {
		t.Fatalf("the engine holds %d subscriptions for one surface, want 1", open)
	}
}

// THE ENGINE'S SILENCE IS NOT A VERDICT. A surface reads a missing id as "nobody
// is asking this any more" and writes it on the card, so a call that could not be
// made has to come back as "this engine did not say".
func TestPendingTasksSaysWhenTheFarEndDidNotAnswer(t *testing.T) {
	far := &railAgent{fakeAgent: &fakeAgent{}, pending: []uint64{4, 9}}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	ids, known := loop.Client.Agent().TaskProposalsPending()
	if !known || len(ids) != 2 || ids[0] != 4 || ids[1] != 9 {
		t.Fatalf("pending = %v %v", ids, known)
	}
	_ = loop.Close()
	if ids, known := loop.Client.Agent().TaskProposalsPending(); known || ids != nil {
		t.Fatalf("a dead connection answered %v %v", ids, known)
	}
}

// V5: Protocol version 10 carries a proposal hold to the far engine without
// making the surface's key path wait for the round trip, and the far engine's
// zero-deadline proposal returns to the watching surface.
func TestTaskHoldCrossesTheVersionTenWire(t *testing.T) {
	if Version != 10 {
		t.Fatalf("task hold protocol version = %d, want 10", Version)
	}
	far := &railAgent{fakeAgent: &fakeAgent{}}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })

	lane, stop := loop.Client.Agent().WatchTaskUpdates()
	t.Cleanup(stop)
	waitFor(t, "the engine opened the surface's task lane", func() bool { return far.opened() == 1 })

	loop.Client.Agent().HoldTask(17)
	waitFor(t, "the far engine held proposal 17", func() bool { return far.heldTask(17) })
	held := nextTask(t, lane)
	if held.Kind != session.EventTaskProposal || held.Task == nil || held.Task.ID != 17 || !held.Task.Deadline.IsZero() {
		t.Fatalf("held proposal returning over the wire = kind %v task %+v", held.Kind, held.Task)
	}
}

// An engine with no graph offers no lane, and the surface draws the rail it has
// always drawn — nothing — rather than a lane that never speaks.
func TestATasklessEngineOffersNoRail(t *testing.T) {
	base := &tasklessAgent{WrappedAgent: &fakeAgent{}}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: base}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	lane, stop := loop.Client.Agent().WatchTaskUpdates()
	t.Cleanup(stop)
	if _, err := loop.Client.call(context.Background(), MethodTaskWatch, nil); err != nil {
		t.Fatalf("Task.Watch on a taskless engine = %v", err)
	}
	if _, known := loop.Client.Agent().TaskProposalsPending(); known {
		t.Fatal("a taskless engine claimed to know its pending proposals")
	}
	select {
	case event, ok := <-lane:
		if ok {
			t.Fatalf("a taskless engine put %v on the rail", event.Kind)
		}
	case <-time.After(200 * time.Millisecond):
		// Silence is the answer: the lane is open and nothing is on it.
	}
}
