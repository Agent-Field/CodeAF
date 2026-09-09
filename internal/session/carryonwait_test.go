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
	completer := &scriptedCompleter{steps: waitingSteps(10, waiting, func() string {
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

// ── and carrying on has a ceiling ───────────────────────────────────────────
