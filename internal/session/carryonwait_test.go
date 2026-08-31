package session

// A TURN WAITING ON THE WORLD IS WAITING, AND CARRYING ON HAS A CEILING.
//
// THE MEASURED FAILURE these cases are written from: 2026-08-31,
// 15:36:25–15:41:45Z. A conversation was waiting for GitHub's checks on two pull
// requests with a watch of its own running over `gh pr checks`. Every turn ended
// by saying exactly that — "the watch (job 5) fires when the pending count
// settles; nothing actionable until then" — and the end-of-turn reader, which can
// only answer done, stop or carry on, answered carry on, because the ask really
// was not finished. Twenty carry-ons in five minutes, each one a reader call and
// another poll of the very command that was going to report, until the ceiling
// converted the wait into a task whose acceptance nobody could ever fail.
//
// Two rules close it, and both are read here through [Agent.Submit] rather than
// by calling the gate: what the measurement was about is what a whole turn costs.

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// liveJob puts one running job on this session's registry and leaves it running
// for the rest of the case.
//
// It is built rather than started because what these cases need is the FACT of a
// live job at the moment a turn ends, and a real `sleep` would make the test
// about whether the sleep outlived the turn. The two fields that are not status
// are what Close needs to let go of it at once: a stop function so the signal
// round returns without a process, and a closed done so the shared grace is not
// spent waiting on a channel nothing will ever close.
func liveJob(agent *Agent, id int, kind jobKind, label string) {
	settled := make(chan struct{})
	close(settled)
	agent.jobs.mu.Lock()
	defer agent.jobs.mu.Unlock()
	agent.jobs.jobs = append(agent.jobs.jobs, &job{
		id: id, kind: kind, label: label, command: label,
		// The sink is real so the rows this fixture plants walk the same footer
		// code a live job does: a nil sink here cost every tool result in these
		// cases its job footer, behind a recovered panic nobody saw.
		sink:    &jobSink{},
		started: time.Now(), done: settled, stop: func() {},
	})
}

// waitingSteps is [stoppingSteps] for a turn that keeps stopping: the rounds are
// spent, and then EVERY answer is words. The stop line carries its own number so
// four of them in a row are four different sentences — the repetition ladder is
// a different mechanism with a different job, and a fixture that tripped it
// would be measuring that one instead of this one.
func waitingSteps(rounds int, stopped string, remains func() string) []step {
	var done, stops atomic.Int64
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
			if call := done.Add(1); call <= int64(rounds) {
				return toolResponseWithText(fmt.Sprintf("call-%d", call), "ls",
					fmt.Sprintf(`{"path":"./%d"}`, call), "Looking at the next path."), nil
			}
			return textResponse(fmt.Sprintf("%s (%d)", stopped, stops.Add(1))), nil
		}
	}
	return steps
}

// saidHowOften counts the notices carrying one line.
func saidHowOften(said []string, want string) int {
	seen := 0
	for _, line := range said {
		if strings.Contains(line, want) {
			seen++
		}
	}
	return seen
}

// ── a turn waiting on its own background work ───────────────────────────────

// A TURN THAT ENDS WHILE A JOB THIS CONVERSATION STARTED IS STILL RUNNING IS
// NEVER READ AND NEVER CARRIED ON.
//
// The job's exit is queued as an OWED note and starts a turn by itself the
// moment it lands, so the continuation the reader would buy already exists and
// is already on its way. Carrying on can only fill the gap before it with polls
// of the thing that is about to report — which is the whole of what the measured
// five minutes were.
func TestATurnWaitingOnItsOwnBackgroundJobIsNotCarriedOn(t *testing.T) {
	const waiting = "the watch (job 5) fires when the pending count settles; nothing actionable until then"

	var remainsAsks atomic.Int64
	// Past the first rung, so the price gate is not what is keeping the turn
	// shut, and the reader would say there is work left if anybody asked it.
	completer := &scriptedCompleter{steps: waitingSteps(checkpointMarkAt(1), waiting, func() string {
		remainsAsks.Add(1)
		return "the checks have not landed and neither pull request is merged"
	})}
	agent := checkpointAgent(t, completer)
	stubbedGraph(agent, func(node *TaskNode) {})
	liveJob(agent, 5, jobKindWatch, "pr checks")

	events, err := agent.Submit(context.Background(), "merge both pull requests once the checks are green")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if got := remainsAsks.Load(); got != 0 {
		t.Errorf("a turn waiting on its own live job was read %d times for what remains", got)
	}
	if saidSomething(noticeTexts(collected), checkpointCarryOnNote) {
		t.Errorf("a turn waiting on its own live job was carried on: %q", noticeTexts(collected))
	}
	if strings.Contains(transcriptText(agent), checkpointCarryOnLead) {
		t.Error("a continuation was written into a turn that was waiting on a job it started")
	}
	// AND THE GATE IS THE LIVE WORK TREE'S OWN ANSWER, which is what the head
	// count over the roster is drawn from: one fact, one definition.
	if !agent.turnIsWaitingOnItsOwnWork() {
		t.Error("a session with a running watch does not read as waiting on its own work")
	}
}

// AND A TURN WITH NOTHING OF ITS OWN RUNNING IS CARRIED ON EXACTLY AS BEFORE.
//
// It is the same fixture with the job taken away, because a gate that closed the
// carry-on for every turn would pass the case above and lose the thing the
// carry-on was built for.
func TestATurnWithNothingRunningOfItsOwnIsStillCarriedOn(t *testing.T) {
	const stopped = "I've finished the parser, next I'll wire the handlers"
	const remains = "the handlers are not wired and the golden tests have never been run"

	var remainsAsks atomic.Int64
	completer := &scriptedCompleter{steps: waitingSteps(checkpointMarkAt(1), stopped, func() string {
		if remainsAsks.Add(1) == 1 {
			return remains
		}
		return checkpointNothingLeft
	})}
	agent := checkpointAgent(t, completer)
	stubbedGraph(agent, func(node *TaskNode) {})

	events, err := agent.Submit(context.Background(), "port the language server and get the golden tests passing")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if agent.turnIsWaitingOnItsOwnWork() {
		t.Fatal("a session that started nothing reads as waiting on its own work")
	}
	if got := remainsAsks.Load(); got != 2 {
		t.Fatalf("the ask was read %d times; want one that re-opened and one that let the turn end", got)
	}
	if !strings.Contains(transcriptText(agent), checkpointCarryOnLead+remains) {
		t.Errorf("the turn was not re-opened on what the reader said is left:\n%s", transcriptText(agent))
	}
	if !saidSomething(noticeTexts(collected), checkpointCarryOnNote) {
		t.Errorf("nobody said why the turn kept going; notices were %q", noticeTexts(collected))
	}
}

// ── and carrying on has a ceiling ───────────────────────────────────────────

// THE SAME READING ABOUT THE SAME STOPPED TURN IS BELIEVED A FIXED NUMBER OF
// TIMES AND THEN IT IS NOT.
//
// A reader that answers "not finished" to every one of a turn's endings has
// stopped being evidence and started being an echo, and the measured run is what
// says so: twenty of them, five minutes, no progress at all. So the ask is
// carried on [checkpointCarryOnCap] times, and the person is told once, in the
// register the other notes on this road use.
func TestCarryingOnAnAskHasACeilingOfItsOwn(t *testing.T) {
	var remainsAsks atomic.Int64
	// Past the first rung so the reader is armed, and a reader that never says
	// the ask is finished — the exact shape the measured conversation was in.
	completer := &scriptedCompleter{steps: waitingSteps(checkpointMarkAt(1),
		"still waiting on the checks; nothing actionable until then", func() string {
			remainsAsks.Add(1)
			return "the checks have not landed and neither pull request is merged"
		})}
	agent := checkpointAgent(t, completer)
	stubbedGraph(agent, func(node *TaskNode) {})

	events, err := agent.Submit(context.Background(), "merge both pull requests once the checks are green")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)
	notices := noticeTexts(collected)

	if got := saidHowOften(notices, checkpointCarryOnNote); got != checkpointCarryOnCap {
		t.Errorf("the ask was carried on %d times, want %d; notices were %q",
			got, checkpointCarryOnCap, notices)
	}
	if got := strings.Count(transcriptText(agent), checkpointCarryOnLead); got != checkpointCarryOnCap {
		t.Errorf("%d continuations were written into the turn, want %d", got, checkpointCarryOnCap)
	}
	// AND THE PERSON IS TOLD ONCE, WHICH IS THE HONEST HALF: the ask really is
	// still unfinished, and the harness is going to stop pushing rather than
	// push again.
	if got := saidHowOften(notices, checkpointCarriedOnNote()); got != 1 {
		t.Errorf("the ceiling on carrying on said its line %d times, want once; notices were %q", got, notices)
	}
	// AND THE READER IS SPENT ONE MORE TIME THAN THE CAP AND NOT TWENTY. The cap
	// is asked AFTER the reading so the line a person reads is true, which costs
	// exactly the one call that proves the ask is still open.
	if got := remainsAsks.Load(); got != int64(checkpointCarryOnCap)+1 {
		t.Errorf("the reader was spent %d times on one stuck ask, want %d",
			got, checkpointCarryOnCap+1)
	}
	// AND THE TURN DID NOT REACH THE CEILING ON CARRY-ONS ALONE, which is the
	// third half of the measured failure: the wait must never become a task.
	if saidSomething(notices, checkpointCeilingNote) {
		t.Errorf("carrying on drove the turn to the ceiling by itself: %q", notices)
	}
}
