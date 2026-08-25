package session

// ATTACHING, LEAVING, AND THE ONE PREDICATE — the three things a surface needs
// before it can hold several conversations and draw one of them.
//
// What is asserted here is not that the machinery exists but that it does the
// only two things worth doing: a reader that arrives half way through a turn
// sees the whole of it, once; and a reader that goes away leaves nothing behind
// — no watcher on a list, no queue that keeps filling, no pump parked on a
// channel nobody will ever read again.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── arriving at a turn already under way ────────────────────────────────────

// TestAttachReplaysTheTurnThenItsLiveTail is the whole of capability one: a
// reader that was not here when the turn started sees every word of it, in
// order, exactly once, and then the rest as it arrives.
func TestAttachReplaysTheTurnThenItsLiveTail(t *testing.T) {
	before := []string{"the ", "answer ", "so ", "far"}
	after := []string{", and ", "the rest"}
	whole := strings.Join(append(append([]string{}, before...), after...), "")

	// midTurn is closed once the first half is on the wire; carryOn is what the
	// test closes when it has attached, so the two halves are on either side of
	// the attach with nothing racing between them.
	midTurn := make(chan struct{})
	carryOn := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			for _, chunk := range before {
				provider.Emit(ctx, provider.StreamDelta, chunk)
			}
			close(midTurn)
			<-carryOn
			for _, chunk := range after {
				provider.Emit(ctx, provider.StreamDelta, chunk)
			}
			return textResponse(whole), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	first, err := agent.Submit(context.Background(), "say something long")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	<-midTurn

	late, running, stop := agent.Attach()
	if !running {
		t.Fatal("Attach during a turn said nothing was running")
	}
	if late == nil {
		t.Fatal("Attach during a turn answered with no channel")
	}
	defer stop()
	close(carryOn)

	// THE LATE READER HAS THE WHOLE ANSWER AND HAS IT ONCE. Concatenating its
	// deltas is exactly the no-gap-and-no-duplicate assertion: a gap loses text,
	// a duplicate repeats it, and either shows up in this one string.
	lateText, lateDeltas := deltaText(collect(t, late))
	if lateText != whole {
		t.Fatalf("the late reader saw %q, want the whole turn %q", lateText, whole)
	}
	// And the backlog it was handed is FOLDED: the four chunks that had already
	// gone out arrive as one delta, and only the two that came after it are
	// their own events.
	if want := 1 + len(after); lateDeltas != want {
		t.Fatalf("the late reader saw %d deltas, want %d — the backlog is meant to fold into one", lateDeltas, want)
	}

	// AND SUBMIT'S OWN CHANNEL IS UNTOUCHED. The caller that started the turn
	// still sees every chunk as it streamed, which is the contract Attach was
	// added beside rather than on top of.
	firstText, firstDeltas := deltaText(collect(t, first))
	if firstText != whole {
		t.Fatalf("the submitting reader saw %q, want %q", firstText, whole)
	}
	if want := len(before) + len(after); firstDeltas != want {
		t.Fatalf("the submitting reader saw %d deltas, want %d unfolded", firstDeltas, want)
	}
}

// TestAttachReplayHandsTheTurnToTheStreamNotTheEntries pins the atomic door's
// whole reason to exist: a turn's completed steps are journaled the moment they
// complete, so a surface that replayed the transcript and then attached drew
// those steps twice — once in their journal form, once again out of the
// backlog. AttachReplay splits the conversation at the turn's floor instead:
// the entries stop where the running turn's work begins, the stream carries the
// turn whole, and the person's own words are in the entries because no event
// ever re-carries them.
func TestAttachReplayHandsTheTurnToTheStreamNotTheEntries(t *testing.T) {
	midTurn := make(chan struct{})
	carryOn := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			// One whole step — prose and a tool call — recorded in the
			// transcript before the turn's second request goes out.
			return toolResponseWithText("call-1", "read", `{"path":"go.mod"}`, "let me look first"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(midTurn)
			<-carryOn
			return textResponse("the answer"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	first, err := agent.Submit(context.Background(), "go look")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	<-midTurn

	entries, events, stop := agent.AttachReplay()
	if events == nil {
		t.Fatal("AttachReplay during a turn answered with no stream")
	}
	defer stop()
	// THE ENTRIES HOLD THE QUESTION AND NONE OF THE TURN'S WORK. The completed
	// first step is already in the transcript — that is the defect's whole
	// setup — and it must be the stream's to draw, not the replay's.
	saidGoLook := false
	for _, entry := range entries {
		if entry.Role == "user" && strings.Contains(entry.Text, "go look") {
			saidGoLook = true
		}
		if entry.Tool == "read" || strings.Contains(entry.Text, "let me look first") {
			t.Fatalf("the running turn's work leaked into the replay entries: %+v", entry)
		}
	}
	if !saidGoLook {
		t.Fatal("the person's own message is missing from the replay entries")
	}

	close(carryOn)
	streamed := collect(t, events)
	sawRead := false
	for _, event := range streamed {
		if event.Tool == "read" {
			sawRead = true
		}
	}
	if !sawRead {
		t.Fatal("the backlog did not replay the completed step's tool call")
	}
	collect(t, first)

	// AND ONCE THE TURN IS OVER THE SAME DOOR IS THE WHOLE RECORD: no stream,
	// and the work that was the stream's to draw is the entries' again.
	entries, events, stop = agent.AttachReplay()
	if events != nil {
		t.Fatal("AttachReplay on an idle session handed back a stream")
	}
	stop()
	sawRead = false
	for _, entry := range entries {
		if entry.Tool == "read" {
			sawRead = true
		}
	}
	if !sawRead {
		t.Fatal("the settled turn's work is missing from the idle replay entries")
	}
}

// TestAttachWithNoTurnRunningSaysSo is the other half of the contract: nothing
// to watch is an answer, not an empty stream a caller has to wait on.
func TestAttachWithNoTurnRunningSaysSo(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	events, running, stop := agent.Attach()
	if running || events != nil {
		t.Fatalf("Attach on an idle session = (%v, %v), want (nil, false)", events, running)
	}
	if stop == nil {
		t.Fatal("Attach answered with a nil stop; it is never nil")
	}
	stop()
	stop()

	// And again once a turn has been and gone: a finished turn is the journal's,
	// not this door's.
	first, err := agent.Submit(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, first)
	if _, running, _ := agent.Attach(); running {
		t.Fatal("Attach after the turn ended said a turn was running")
	}
}

// TestAttachStopLeavesTheHubHoldingNothing is capability two on the turn
// stream: the reader goes, the hub forgets it, the pump ends, and the turn
// carries on for whoever is still reading.
func TestAttachStopLeavesTheHubHoldingNothing(t *testing.T) {
	midTurn := make(chan struct{})
	carryOn := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "opening")
			close(midTurn)
			<-carryOn
			provider.Emit(ctx, provider.StreamDelta, " and closing")
			return textResponse("opening and closing"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	first, err := agent.Submit(context.Background(), "go")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	<-midTurn

	late, running, stop := agent.Attach()
	if !running {
		t.Fatal("Attach during a turn said nothing was running")
	}
	agent.mu.Lock()
	hub := agent.hub
	agent.mu.Unlock()
	if got := hubSubscribers(hub); got != 2 {
		t.Fatalf("the hub holds %d subscribers after an attach, want 2", got)
	}

	stop()
	stop() // twice is once
	if got := hubSubscribers(hub); got != 1 {
		t.Fatalf("the hub holds %d subscribers after stop, want 1 — the attacher is still on the list", got)
	}
	// The pump ended, so the channel closes rather than parking on its next send.
	waitClosed(t, late, "the stopped attach's channel")

	// AND THE TURN IS FINE. A reader leaving is not an interrupt: the rest of
	// the answer reaches the caller that asked for it.
	close(carryOn)
	text, _ := deltaText(collect(t, first))
	if text != "opening and closing" {
		t.Fatalf("after a late reader left, the turn said %q", text)
	}
}

// ── leaving each standing lane ──────────────────────────────────────────────

// TestEveryStandingLaneCanBeLeft walks the three lanes that are a list of
// streams on the agent. For each: subscribing puts one watcher on the list,
// stopping takes it off, a send afterwards does not block, and the channel ends.
func TestEveryStandingLaneCanBeLeft(t *testing.T) {
	for _, lane := range []struct {
		name  string
		watch func(*Agent) (<-chan Event, func())
		held  func(*Agent) int
		emit  func(*Agent)
	}{
		{
			name:  "task updates",
			watch: (*Agent).WatchTaskUpdates,
			held:  func(a *Agent) int { return len(a.taskWatchers) },
			emit:  func(a *Agent) { a.emitTaskUpdate(TaskNotice{ID: 1, Title: "after"}) },
		},
		{
			name:  "adaptive runs",
			watch: (*Agent).WatchOrchestrations,
			held:  func(a *Agent) int { return len(a.orchestrateWatchers) },
			emit:  func(a *Agent) { a.emitOrchestrate(Event{Kind: EventOrchestrateNote, Text: "after"}) },
		},
		{
			name:  "harness designs",
			watch: (*Agent).WatchHarnessDesigns,
			held:  func(a *Agent) int { return len(a.harnessWatchers) },
			emit:  func(a *Agent) { a.emitHarness(Event{Kind: EventHarnessDesign, Text: "after"}) },
		},
	} {
		t.Run(lane.name, func(t *testing.T) {
			agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
			held := func() int {
				agent.mu.Lock()
				defer agent.mu.Unlock()
				return lane.held(agent)
			}
			if got := held(); got != 0 {
				t.Fatalf("a fresh session already holds %d watchers on this lane", got)
			}
			events, stop := lane.watch(agent)
			if got := held(); got != 1 {
				t.Fatalf("after subscribing the session holds %d watchers, want 1", got)
			}

			stop()
			stop() // twice is once
			if got := held(); got != 0 {
				t.Fatalf("after stop the session still holds %d watchers on this lane", got)
			}
			// A SEND AFTERWARDS MUST NOT BLOCK. It is a fan-out over a list this
			// stream is no longer on, so this call returning at all is the
			// assertion; the timeout is what catches the version where it does not.
			done := make(chan struct{})
			go func() { defer close(done); lane.emit(agent) }()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("a send after stop blocked")
			}
			waitClosed(t, events, "the stopped lane's channel")
		})
	}
}

// TestTheWakeLaneCanBeLeft is the same for the lane that is a channel of
// channels rather than a list of streams. The session's own close ends every
// wake lane the same way, so a reader needs no second rule for either.
func TestTheWakeLaneCanBeLeft(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	held := func() int {
		agent.mu.Lock()
		defer agent.mu.Unlock()
		return len(agent.wakeLanes)
	}

	lane, stop := agent.WatchWakes()
	if got := held(); got != 1 {
		t.Fatalf("after subscribing the session holds %d wake lanes, want 1", got)
	}
	// And the old door is the same subscription, so a second reader shows up
	// beside the first.
	agent.Wakes()
	if got := held(); got != 2 {
		t.Fatalf("Wakes did not take a lane of its own: the session holds %d", got)
	}

	stop()
	stop() // twice is once
	if got := held(); got != 1 {
		t.Fatalf("after stop the session holds %d wake lanes, want 1", got)
	}
	select {
	case _, open := <-lane:
		if open {
			t.Fatal("the stopped wake lane carried a turn")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stopped wake lane never ended")
	}

	// A wake afterwards must not block on the lane that left. wakeLocked holds
	// a.mu while it hands the stream over, so this is also the assertion that
	// stop did not leave a closed channel on the list to send into.
	done := make(chan struct{})
	go func() {
		defer close(done)
		agent.mu.Lock()
		agent.wakeLocked()
		agent.mu.Unlock()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a wake after stop blocked")
	}
}

// TestATaskRoomCanBeLeftWhileTheNodeRuns is the pilot lane: a person opens a
// node's page, reads it and walks out, and the node goes on running.
func TestATaskRoomCanBeLeftWhileTheNodeRuns(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	graph := agent.graph()
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	graph.run = func(node *TaskNode) {
		<-release
		node.finish("done", nil, "", "")
		node.graph.complete(node, TaskDone)
	}
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "a node to watch", brief: "b", acceptance: "a"})
	node := graph.node(id)
	room := node.openRoom()

	events, stop, err := agent.WatchTaskRoom(id)
	if err != nil {
		t.Fatalf("WatchTaskRoom: %v", err)
	}
	if got := roomWatchers(room); got != 1 {
		t.Fatalf("the room holds %d watchers after one join, want 1", got)
	}

	stop()
	stop() // twice is once
	if got := roomWatchers(room); got != 0 {
		t.Fatalf("the room still holds %d watchers after leaving", got)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		room.publish(Event{Kind: EventTextDelta, Text: "nobody is listening"})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("publishing to a room somebody left blocked")
	}
	waitClosed(t, events, "the left room's channel")

	once.Do(func() { close(release) })
	waitDoneNode(t, node)
}

// ── one predicate for "needs a person" ──────────────────────────────────────

// TestNeedsPersonAgreesWithPresence pins the thing that is worth pinning: the
// method a surface asks and the word the presence file writes come from one
// place, so they cannot answer differently about the same instant.
func TestNeedsPersonAgreesWithPresence(t *testing.T) {
	for _, lane := range []struct {
		name string
		ask  func(*Agent)
	}{
		{"an approval question", func(a *Agent) {
			a.mu.Lock()
			a.consent = map[uint64]chan consentAnswer{1: make(chan consentAnswer, 1)}
			a.mu.Unlock()
		}},
		{"a connect offer", func(a *Agent) {
			a.mu.Lock()
			a.connectAsks = map[string]connectAsk{"github": {}}
			a.mu.Unlock()
		}},
		{"a sub-harness offer", func(a *Agent) {
			a.mu.Lock()
			a.harnessAsks = map[uint64]harnessAsk{1: {answers: make(chan harnessAnswer, 1)}}
			a.mu.Unlock()
		}},
		{"a task proposal", func(a *Agent) {
			a.mu.Lock()
			a.taskAnswers = map[uint64]chan TaskAnswer{1: make(chan TaskAnswer, 1)}
			a.mu.Unlock()
		}},
		{"an adaptive run out of fuel", func(a *Agent) {
			run := orchestrate.New("migrate the thing", nil, nil, orchestrate.Options{Cap: 1})
			run.Charge(2)
			a.mu.Lock()
			a.orchestrations = map[string]*orchestration{"run-1": {run: run, cancel: func() {}}}
			a.mu.Unlock()
		}},
	} {
		t.Run(lane.name, func(t *testing.T) {
			agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
			if agent.NeedsPerson() {
				t.Fatal("a fresh session says it needs a person")
			}
			if state := agent.presenceSnapshot(time.Now()).State; state != PresenceIdle {
				t.Fatalf("a fresh session says %q, want %q", state, PresenceIdle)
			}

			lane.ask(agent)
			if !agent.NeedsPerson() {
				t.Fatalf("with %s out, NeedsPerson says no", lane.name)
			}
			snapshot := agent.presenceSnapshot(time.Now())
			if snapshot.State != PresenceWaiting {
				t.Fatalf("with %s out, presence says %q, want %q", lane.name, snapshot.State, PresenceWaiting)
			}
			if !snapshot.NeedsPerson() {
				t.Fatalf("with %s out, the presence row disagrees with the agent", lane.name)
			}
			// The line the file writes is the line the agent gives, always: one
			// call, one instant, no second reading of a lane that may have been
			// answered in between.
			if line := agent.WaitingOn(); line != snapshot.Reason {
				t.Fatalf("WaitingOn = %q but presence wrote %q", line, snapshot.Reason)
			}
		})
	}
}

// TestAPausedRunSaysWaitingOnYouInTheRunsOwnWords is the named behaviour change:
// a run that has spent its tank used to read as `working` to every other window
// on the machine, which was a window telling somebody there was nothing to do.
func TestAPausedRunSaysWaitingOnYouInTheRunsOwnWords(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	run := orchestrate.New("migrate the thing", nil, nil, orchestrate.Options{Cap: 2})
	agent.mu.Lock()
	agent.orchestrations = map[string]*orchestration{"run-1": {run: run, cancel: func() {}}}
	agent.mu.Unlock()

	// A run inside its tank is work, not a question.
	run.Charge(0.5)
	if agent.NeedsPerson() {
		t.Fatal("a run with fuel left says it needs a person")
	}

	run.Charge(2)
	snapshot := agent.presenceSnapshot(time.Now())
	if snapshot.State != PresenceWaiting {
		t.Fatalf("a run at the gate says %q, want %q", snapshot.State, PresenceWaiting)
	}
	if want := fuelGateLine + " · " + run.Snapshot().Fuel.Gauge(); snapshot.Reason != want {
		t.Fatalf("the gate's line is %q, want the run page's own words %q", snapshot.Reason, want)
	}

	// AND THE WORD CLEARS WHEN THE QUESTION DOES. The gate comes down here
	// through the run's own stop rather than through a top-up, because a top-up
	// is consumed by the run loop ([orchestrate.Orchestrator.openGate] answers
	// inside it) and no loop is standing in this test — there is no planner and
	// no executor, only a tank. What is being pinned either way is that this
	// predicate reads the run's state rather than remembering an event.
	run.Cancel()
	if agent.NeedsPerson() {
		t.Fatal("a stopped run still says it needs a person")
	}
	if state := agent.presenceSnapshot(time.Now()).State; state == PresenceWaiting {
		t.Fatal("presence still says waiting on you about a run that is over")
	}
}

// ── shared reading ──────────────────────────────────────────────────────────

// deltaText is one stream's assistant text, joined, and how many events carried
// it. The pair is what tells a gap from a fold: the string says whether any
// words were lost or repeated, the count says how they were packaged.
func deltaText(events []Event) (string, int) {
	var text strings.Builder
	count := 0
	for _, event := range events {
		if event.Kind == EventTextDelta {
			text.WriteString(event.Text)
			count++
		}
	}
	return text.String(), count
}

func hubSubscribers(hub *eventHub) int {
	if hub == nil {
		return 0
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	return len(hub.subscribers)
}

func roomWatchers(room *taskRoom) int {
	if room == nil {
		return 0
	}
	room.mu.Lock()
	defer room.mu.Unlock()
	return len(room.watchers)
}

// waitClosed fails unless the channel ends, which is what a pump that exited
// looks like from outside. A parked pump is exactly the leak these tests are
// about, and it shows up here as a timeout rather than as a hang.
func waitClosed(t *testing.T, events <-chan Event, what string) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case _, open := <-events:
			if !open {
				return
			}
		case <-deadline:
			t.Fatalf("%s never closed", what)
		}
	}
}
