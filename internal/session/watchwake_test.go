package session

// A WATCH THAT FIRES WAKES THE CONVERSATION, AND ONLY THE FIRING DOES.
//
// THE MEASURED SILENCE these cases are written from: a session watching
// `gh pr checks` for the checks to settle. The watch did its job — it saw the
// pending count go to zero — and its news rode the ambient lane, which waits for
// a turn boundary. There was no turn boundary coming: the person had walked
// away, and the conversation that had learned the checks were green sat there
// saying nothing until they came back and typed. A watch's LAST note is the
// answer to the question it was started for, and an answer nobody is ever asked
// for is not an answer.
//
// Both halves are tested, because half of this was always right: a periodic tick
// is telemetry with a complete log behind it and must stay quiet, and a turn per
// delta would turn a quiet observer into an autonomous conversation.
//
// Every case here drives the REAL timer loop rather than calling the lane's door,
// for tools_watch_test.go's reason: the thing under test is which news a tick is,
// and a fixture that decided that for itself would be testing the fixture.

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// firingWatch starts a change watch with an `until` over one file in the
// workspace, and answers with the job's id. The file is fed first so the
// baseline tick has something to be a baseline of.
func firingWatch(t *testing.T, agent *Agent, workspace, name, until string, opening ...string) int {
	t.Helper()
	feed(t, workspace, name, opening...)
	text, isError := startWatchTool(t, agent, map[string]any{
		"command":       "cat " + name,
		"every_seconds": watchMinEvery,
		"until":         until,
		"name":          "pr-checks",
	})
	if isError {
		t.Fatalf("the watch would not start: %s", text)
	}
	id := watchID(t, agent)
	waitTicks(t, agent, id, 1)
	return id
}

// owedWatchNotes is every note on the OWED lane, with the two marks that decide
// what the lane does with it: whether a turn is owed for it, and whether it was
// collapsed into a batch key.
func owedWatchNotes(agent *Agent) []userMessage {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	queued := make([]userMessage, len(agent.steering))
	copy(queued, agent.steering)
	return queued
}

// ── the two lanes ───────────────────────────────────────────────────────────

// A TICK IS AMBIENT AND THE FIRING IS OWED, on one watch, in one run.
//
// The session is held before its public opening so both notes stay on their
// queues to be looked at: what this case is about is WHICH LANE each one took,
// and a wake that drained the owed note into a turn would take the evidence away
// with it. The wake itself is the next case.
func TestAWatchTickIsAmbientAndItsFiringIsOwed(t *testing.T) {
	agent, workspace := jobsAgent(t)
	agent.mu.Lock()
	agent.opened = false
	agent.mu.Unlock()

	id := firingWatch(t, agent, workspace, "checks.log", "all checks passed", "2 checks pending")

	// A DELTA. The output moved and the terms were not met, so this is telemetry
	// and it waits for a boundary.
	feed(t, workspace, "checks.log", "2 checks pending", "1 check pending")
	waitFor(t, "the tick to reach the ambient queue", func() bool {
		return len(ambientQueue(agent)) == 1
	})
	if queued := ambientQueue(agent); !strings.Contains(queued[0], "1 check pending") {
		t.Fatalf("the tick is not the delta: %v", queued)
	}
	if queued := steeringQueue(agent); len(queued) != 0 {
		t.Fatalf("a tick woke its way onto the owed lane: %v", queued)
	}

	// AND THE FIRING. `until` matched, the watch is over, and this is the answer
	// to the question it was started for.
	feed(t, workspace, "checks.log", "all checks passed")
	waitFor(t, "the firing to reach the owed queue", func() bool {
		return len(steeringQueue(agent)) == 1
	})

	queued := owedWatchNotes(agent)
	note := queued[0].text()
	if !strings.Contains(note, "until matched") || !strings.Contains(note, "all checks passed") {
		t.Fatalf("the firing lost the terms that were met or the evidence behind them: %q", note)
	}
	if !queued[0].wake {
		t.Fatalf("the firing is on the owed lane without the mark that starts a turn: %q", note)
	}
	// AND IT KEEPS EVERY WORD. The batch key is what collapses repeated ticks to
	// a count, and spending the one line of evidence to save a line is exactly
	// what owed news does not do.
	if queued[0].batchKey != "" {
		t.Fatalf("the firing carries a tick's batch key %q", queued[0].batchKey)
	}
	// The tick is still where it was: firing is not a flush of the other lane.
	if got := len(ambientQueue(agent)); got != 1 {
		t.Fatalf("the ambient queue holds %d notes after the firing, want the one tick", got)
	}
	// AND THE WATCH IS OVER. Nothing further will ever come from it, which is the
	// whole reason its last note is owed.
	waitFor(t, "the watch to settle", func() bool {
		target := agent.jobs.find(id)
		return target != nil && target.info().state != jobRunning
	})
}

// A RUN OF TICKS ON AN IDLE SESSION STARTS NOTHING AT ALL.
//
// The other half of the law, and the one that was already right: a delta is
// telemetry with a complete log behind it, and a turn per delta would turn a
// quiet observer into an autonomous conversation. This case leaves the session
// open — so a wake WOULD fire — and asserts that none does.
func TestWatchTicksNeverStartATurn(t *testing.T) {
	var requests atomic.Int64
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			requests.Add(1)
			return textResponse("nobody should have asked me anything"), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, nil)

	firingWatch(t, agent, workspace, "app.log", "this never appears", "starting up")
	for round, line := range []string{"one", "two", "three"} {
		feed(t, workspace, "app.log", "starting up", line)
		want := round + 1
		waitFor(t, fmt.Sprintf("tick %d to reach the ambient queue", want), func() bool {
			return len(ambientQueue(agent)) == want
		})
	}

	if got := requests.Load(); got != 0 {
		t.Fatalf("three watch ticks started %d turns on an idle session", got)
	}
	if queued := steeringQueue(agent); len(queued) != 0 {
		t.Fatalf("a tick reached the owed lane: %v", queued)
	}
}

// ── and the firing wakes ────────────────────────────────────────────────────

// THE DEFECT ITSELF: a watch fires on an idle session and the person gets
// nothing said.
//
// The firing must start a turn, and the model must have the news in front of it
// when it does — a session that learned the checks were green and never wrote the
// sentence about it is the same card-and-silence a landed task used to be.
func TestAFiringWatchWakesAnIdleSession(t *testing.T) {
	asked := make(chan []ai.Message, 4)
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			asked <- messages
			return textResponse("the checks are green on both pull requests"), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, nil)

	firingWatch(t, agent, workspace, "checks.log", "all checks passed", "2 checks pending")
	feed(t, workspace, "checks.log", "all checks passed")

	select {
	case messages := <-asked:
		text := userTextIn(messages)
		if !strings.Contains(text, "until matched") || !strings.Contains(text, "all checks passed") {
			t.Fatalf("the woken turn did not carry the watch's news:\n%s", text)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("a watch that fired never started a turn: the person is told nothing until they type")
	}

	waitFor(t, "the answer to reach the transcript", func() bool {
		return strings.Contains(transcriptText(agent), "the checks are green on both pull requests")
	})
}

// ── the whole measured loop ─────────────────────────────────────────────────

// watchWaitingSteps is [waitingSteps] with one answer added: the turn the FIRING
// starts is told apart by the news it carries, and says the thing a person came
// back to read.
func watchWaitingSteps(rounds int, waiting, fired, answer string, remains func() string) []step {
	var done atomic.Int64
	steps := make([]step, rounds+40)
	for index := range steps {
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForSketch(messages) {
				return textResponse(checkpointChainSketch), nil
			}
			if askedForHandoff(messages) {
				return textResponse("a draft of what is left"), nil
			}
			if askedToWriteHandoff(messages) {
				return textResponse("a brief somebody could work from, written by the mastermind"), nil
			}
			if askedForRemains(messages) {
				return textResponse(remains()), nil
			}
			if strings.Contains(userTextIn(messages), fired) {
				return textResponse(answer), nil
			}
			if call := done.Add(1); call <= int64(rounds) {
				return toolResponseWithText(fmt.Sprintf("call-%d", call), "ls",
					fmt.Sprintf(`{"path":"./%d"}`, call), "Looking at the next path."), nil
			}
			return textResponse(waiting), nil
		}
	}
	return steps
}

// THE MEASURED SHAPE, END TO END: the turn that waits is left alone, and the
// firing is what re-opens the conversation.
//
// It is one case rather than three because the two rules only mean anything
// together. The gate is what stops the harness filling the wait with polls of
// the very command that is about to report; the owed firing is what makes the
// silence it leaves behind end by itself. Either one without the other is a
// conversation that either burns money waiting or goes quiet forever.
func TestASessionWaitingOnAWatchIsWokenWhenItFires(t *testing.T) {
	const (
		waiting = "the watch fires when the checks settle; nothing actionable until then"
		answer  = "the checks are green — both pull requests can go in"
	)

	var remainsAsks atomic.Int64
	completer := &scriptedCompleter{steps: watchWaitingSteps(10,
		waiting, "until matched", answer, func() string {
			remainsAsks.Add(1)
			return "NOTHING LEFT TO DO"
		})}
	agent := checkpointAgent(t, completer)
	stubbedGraph(agent, func(node *TaskNode) {})
	workspace := agent.config.Workspace

	firingWatch(t, agent, workspace, "checks.log", "all checks passed", "2 checks pending")

	events, err := agent.Submit(context.Background(), "merge both pull requests once the checks are green")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	// THE WAIT IS A WAIT. Nothing read it, nothing carried it on, and nothing
	// polled the command the watch was already running.
	if got := remainsAsks.Load(); got != 0 {
		t.Fatalf("the turn waiting on its own watch was read %d times for what remains", got)
	}
	if saidSomething(noticeTexts(collected), checkpointCarryOnNote) {
		t.Fatalf("the turn waiting on its own watch was carried on: %q", noticeTexts(collected))
	}
	if !strings.Contains(transcriptText(agent), waiting) {
		t.Fatalf("the waiting turn never said what it was waiting on:\n%s", transcriptText(agent))
	}
	// AND THE WATCH IS WHY, read off the live work tree the roster's own head
	// count is drawn from rather than off anything this test set up.
	if !agent.turnIsWaitingOnItsOwnWork() {
		t.Fatal("the session does not read as waiting on the watch it started")
	}

	// AND THEN THE WORLD MOVES. Nobody types anything from here.
	feed(t, workspace, "checks.log", "all checks passed")

	waitFor(t, "the woken turn to report the news", func() bool {
		return strings.Contains(transcriptText(agent), answer)
	})
}
