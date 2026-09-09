package session

// A TURN THAT HANDED ITS ASK OFF IS FINISHED; THE ASK IS SOMEBODY ELSE'S NOW.
//
// THE MEASURED FAILURE these cases are written from: a live run on 2026-09-04.
// The person typed "Please hand this work to a task: run ./slow-build.sh, wait
// for it to finish, and tell me the marker it wrote to build.log. Start it now;
// keep the main conversation available while it runs." The turn proposed the
// task, said it had started and that the conversation stayed free, and stopped.
// The end-of-turn reader was asked whether the ASK was finished, said no —
// truthfully, the build was still running — and the turn was re-opened with
// an automatic continuation. With nothing left to do the model polled `tasks` and
// started a watch over its own running task.
//
// The cases below drive [Agent.Submit] rather than calling the gate, because
// what the failure cost was whole turns.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the fixture ─────────────────────────────────────────────────────────────

// The caller performs several ordinary rounds, explicitly starts a task, then
// reports that its work is running independently.
func handedOffSteps(rounds int, said string) []step {
	var done atomic.Int64
	steps := make([]step, rounds+40)
	for index := range steps {
		steps[index] = func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			call := done.Add(1)
			switch {
			case call < int64(rounds):
				return toolResponseWithText(fmt.Sprintf("call-%d", call), "ls",
					fmt.Sprintf(`{"path":"./%d"}`, call), "Looking at the next path."), nil
			case call == int64(rounds):
				return proposeCall(slowBuildTitle, slowBuildBrief)(ctx, messages)
			}
			return textResponse(said), nil
		}
	}
	return steps
}

const (
	slowBuildTitle = "run the slow build"
	slowBuildBrief = "run ./slow-build.sh, wait for it to finish, and report the marker it wrote to build.log"
	slowBuildAsk   = "hand this work to a task: run ./slow-build.sh and tell me the marker it writes; " +
		"keep this conversation available while it runs"
	slowBuildSaid = "I've started task 1 for the build. This conversation stays free while it runs."
)

// proposeAs is [proposeCall] with the call id given, because a turn that
// proposes twice must not answer two calls under one id.
func proposeAs(id, title, brief string) step {
	arguments, _ := json.Marshal(taskArguments{
		Title:       title,
		Summary:     "two lines the person reads",
		Brief:       brief + "\n" + taskBriefMark,
		Deliverable: "the file, at the path named in the brief",
		Acceptance:  "the file is there",
	})
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse(id, "propose_task", string(arguments)), nil
	}
}

// steerHere is the step a turn parks in so a steer can land inside it: it says
// it has begun and then waits to be cut, which is what [Agent.Steer] does to a
// generation in flight.
func steerHere(begun chan struct{}) step {
	return func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		close(begun)
		<-ctx.Done()
		return &ai.Response{Usage: &ai.Usage{PromptTokens: 9, CompletionTokens: 2}}, ctx.Err()
	}
}

// approveTasks drains one turn answering every proposal with yes, which is what
// the person did in the measured run.
func approveTasks(t *testing.T, agent *Agent, events <-chan Event) []Event {
	t.Helper()
	return drainAnsweringTasks(t, events, func(event Event) {
		agent.ResolveTask(event.Task.ID, TaskAnswer{Approved: true})
	})
}

// approvingInBackground is [approveTasks] for a case that has something else to
// do while the turn runs — a steer to send. It touches nothing on *testing.T
// from the goroutine; [turnEvents] is where the waiting and the failing happen.
func approvingInBackground(agent *Agent, events <-chan Event) chan []Event {
	out := make(chan []Event, 1)
	go func() {
		var collected []Event
		for event := range events {
			collected = append(collected, event)
			if event.Kind == EventTaskProposal {
				agent.ResolveTask(event.Task.ID, TaskAnswer{Approved: true})
			}
		}
		out <- collected
	}()
	return out
}

// turnEvents waits for a background drain to finish its turn.
func turnEvents(t *testing.T, out chan []Event) []Event {
	t.Helper()
	select {
	case collected := <-out:
		return collected
	case <-time.After(20 * time.Second):
		t.Fatal("the turn never finished")
		return nil
	}
}

// theRunningNode is the one node a fixture handed out, once the frontier has it.
func theRunningNode(t *testing.T, graph *TaskGraph) *TaskNode {
	t.Helper()
	var found *TaskNode
	waitFor(t, "the handed-off task to be running", func() bool {
		graph.mu.Lock()
		defer graph.mu.Unlock()
		for _, node := range graph.nodes {
			if node.state == TaskRunning {
				found = node
				return true
			}
		}
		return false
	})
	return found
}

// ── the turn that handed its ask off ────────────────────────────────────────

// THE DEFECT ITSELF: the turn did exactly what was asked and was told it had
// not finished.
//
// Nothing is read, nothing is carried on, and the turn ends where the model
// ended it — so there is no round in which a model with nothing to do polls the
// task it just started or opens a watch over it.
func TestATurnThatHandedItsAskToATaskIsNotCarriedOn(t *testing.T) {
	completer := &scriptedCompleter{steps: handedOffSteps(10, slowBuildSaid)}
	agent := checkpointAgent(t, completer)
	// The runner never lands the node, so the work is still out when the turn
	// ends — which is the whole of the shape being tested.
	graph := stubbedGraph(agent, func(node *TaskNode) {})

	events, err := agent.Submit(context.Background(), slowBuildAsk)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := approveTasks(t, agent, events)

	if count := admitted(graph); count != 1 {
		t.Fatalf("%d tasks were admitted, want the one the person asked for", count)
	}
	if saidSomething(noticeTexts(collected), checkpointCarryOnNote) {
		t.Errorf("a turn that handed its ask off was carried on: %q", noticeTexts(collected))
	}
	if strings.Contains(transcriptText(agent), checkpointCarryOnLead) {
		t.Error("a continuation was written into a turn whose work is out with a task")
	}
	// AND THE TURN ENDED ON THE MODEL'S OWN WORDS, with no round after them: a
	// re-opened turn is where the polling and the watch came from.
	if last := lastMessage(agent); last.Role != "assistant" || !strings.Contains(messageText(last), slowBuildSaid) {
		t.Errorf("the turn ended as a %s saying %q, want the handoff line the model wrote",
			last.Role, messageText(last))
	}
	if runningWatches(agent) != 0 {
		t.Error("a watch was opened over the conversation's own running task")
	}
}

// AND THE LANDING IS WHAT BRINGS IT BACK — ONCE.
//
// This is the other half of the rule above and the reason it is safe: the turn
// ends because the outcome is owed by somebody, and that somebody's report
// starts a turn here by itself with the news in front of it.
func TestTheHandedOffTaskWakesTheConversationOnceWhenItLands(t *testing.T) {
	const marker = "BUILD-OK-42"
	const answer = "the build finished and the marker is BUILD-OK-42"

	var woke atomic.Int64
	steps := handedOffSteps(10, slowBuildSaid)
	// The woken turn is told apart by the news it carries, and answers the
	// person the way the manual says a landing is answered.
	for index := range steps {
		inner := steps[index]
		steps[index] = func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			if strings.Contains(userTextIn(messages), marker) {
				woke.Add(1)
				return textResponse(answer), nil
			}
			return inner(ctx, messages)
		}
	}
	completer := &scriptedCompleter{steps: steps}
	agent := checkpointAgent(t, completer)
	graph := stubbedGraph(agent, func(node *TaskNode) {})

	events, err := agent.Submit(context.Background(), slowBuildAsk)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	approveTasks(t, agent, events)

	node := theRunningNode(t, graph)
	node.finish("the script wrote "+marker+" to build.log", nil, "", "")
	node.graph.complete(node, TaskDone)

	waitFor(t, "the landing to reach the person", func() bool {
		return strings.Contains(transcriptText(agent), answer)
	})
	// AND EXACTLY ONE TURN WAS STARTED BY IT. A second wake for one landing is
	// the conversation answering the same news twice.
	time.Sleep(250 * time.Millisecond)
	if got := woke.Load(); got != 1 {
		t.Errorf("the landing started %d turns, want one", got)
	}
}

// AND THE CONVERSATION ANSWERS SOMETHING ELSE WHILE THE WORK IS OUT.
//
// "Keep the main conversation available while it runs" is half the ask, and a
// gate that ended turns by pretending the session was busy would fail it.
func TestANewQuestionIsAnsweredWhileTheHandedOffTaskRuns(t *testing.T) {
	const asked = "while that runs — what does the -j flag in that script do?"
	const answered = "-j sets how many compile jobs run at once"

	steps := handedOffSteps(10, slowBuildSaid)
	for index := range steps {
		inner := steps[index]
		steps[index] = func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			if strings.Contains(userTextIn(messages), "-j flag") {
				return textResponse(answered), nil
			}
			return inner(ctx, messages)
		}
	}
	completer := &scriptedCompleter{steps: steps}
	agent := checkpointAgent(t, completer)
	graph := stubbedGraph(agent, func(node *TaskNode) {})

	events, err := agent.Submit(context.Background(), slowBuildAsk)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	approveTasks(t, agent, events)
	node := theRunningNode(t, graph)

	second, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("second Submit: %v", err)
	}
	collect(t, second)

	if !strings.Contains(transcriptText(agent), answered) {
		t.Errorf("the question asked while the task ran was never answered:\n%s", transcriptText(agent))
	}
	if state := node.stateNow(); state != TaskRunning {
		t.Errorf("the handed-off task is %s, want it still running through the second turn", state)
	}
}

// ── and the three shapes the gate must NOT open for ─────────────────────────

// AND A HANDOFF MADE BEFORE THEIR NEXT SENTENCE DOES NOT ANSWER FOR IT.
//
// A steer does not start a new turn: it lands at the running turn's next
// boundary, so the turn number alone cannot tell the two requests apart. The
// person adds independent work here after the build is already out with a task,
// and the next model request must carry and complete that new instruction.
func TestAHandoffMadeBeforeTheirNextSentenceDoesNotAnswerForIt(t *testing.T) {
	const steered = "while that runs, the dates in NOTES.md are wrong — fix them"
	begun := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		proposeAs("build", slowBuildTitle, slowBuildBrief), steerHere(begun),
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if !strings.Contains(userTextIn(messages), steered) {
				return nil, fmt.Errorf("the new request never reached the worker")
			}
			return toolResponse("dates", "write", `{"path":"NOTES.md","content":"Updated release dates."}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("The release dates are updated; the build is still running."), nil
		},
	}}
	agent := checkpointAgent(t, completer)
	graph := stubbedGraph(agent, func(*TaskNode) {})
	drained := approvingInBackground(agent, mustSubmit(t, agent, slowBuildAsk))
	select {
	case <-begun:
	case <-time.After(5 * time.Second):
		t.Fatal("turn never reached steer boundary")
	}
	mustSteer(t, agent, steered)
	turnEvents(t, drained)
	if admitted(graph) != 1 {
		t.Fatal("the correction created an unrequested task")
	}
	data, err := os.ReadFile(filepath.Join(agent.config.Workspace, "NOTES.md"))
	if err != nil || string(data) != "Updated release dates." {
		t.Fatalf("new request not completed: %q %v", data, err)
	}
	if theRunningNode(t, graph).stateNow() != TaskRunning {
		t.Fatal("the independent build did not remain running")
	}
}

// AND WORK HANDED OUT AFTER THEIR NEXT SENTENCE QUALIFIES AGAIN.
//
// The same turn, the same steer, and this time the model hands the steered work
// out too. The request's work has an owner again, so the turn ends where the
// model ends it.
func TestWorkHandedOutAfterTheirNextSentenceQualifiesAgain(t *testing.T) {
	const steered = "while that runs, the dates in NOTES.md are wrong — fix them"
	const said = "both are out with tasks now; I'll report when they land"

	rounds := 10
	begun := make(chan struct{})
	var done atomic.Int64
	steps := make([]step, rounds+40)
	for index := range steps {
		steps[index] = func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			call := done.Add(1)
			switch {
			case call < int64(rounds):
				return toolResponseWithText(fmt.Sprintf("call-%d", call), "ls",
					fmt.Sprintf(`{"path":"./%d"}`, call), "Looking at the next path."), nil
			case call == int64(rounds):
				return proposeAs("call-task-1", slowBuildTitle, slowBuildBrief)(ctx, messages)
			case call == int64(rounds)+1:
				return steerHere(begun)(ctx, messages)
			case call == int64(rounds)+2:
				return proposeAs("call-task-2", "fix the release dates", "correct the dates in NOTES.md")(ctx, messages)
			}
			return textResponse(said), nil
		}
	}
	completer := &scriptedCompleter{steps: steps}
	agent := checkpointAgent(t, completer)
	graph := stubbedGraph(agent, func(node *TaskNode) {})

	events, err := agent.Submit(context.Background(), slowBuildAsk)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	drained := approvingInBackground(agent, events)
	select {
	case <-begun:
	case <-time.After(20 * time.Second):
		t.Fatal("the turn never reached the step the steer was to land in")
	}
	mustSteer(t, agent, steered)
	collected := turnEvents(t, drained)

	if count := admitted(graph); count != 2 {
		t.Fatalf("%d tasks were admitted, want the build and the steered work", count)
	}
	if saidSomething(noticeTexts(collected), checkpointCarryOnNote) {
		t.Errorf("the turn was carried on after handing the person's new words off: %q", noticeTexts(collected))
	}
}

// A HANDOFF THAT DIED IS NOT DELEGATED WORK.
//
// A node that failed inside the turn that made it has already reported, and the
// turn holding that report is a turn with news to answer rather than one waiting
// for it. Nothing here may read as "the outcome is somebody's now".
func TestAHandoffThatFailedInsideItsOwnTurnIsStillRead(t *testing.T) {
	failed := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		proposeAs("build", slowBuildTitle, slowBuildBrief),
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			select {
			case <-failed:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return toolResponse("inspect", "ls", `{"path":"."}`), nil
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if !strings.Contains(userTextIn(messages), "script exited 127") {
				return nil, fmt.Errorf("the failed task's result was not carried")
			}
			return textResponse("The build failed because its script was not found."), nil
		},
	}}
	agent := checkpointAgent(t, completer)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		node.finish("the script exited 127: ./slow-build.sh not found", nil, "", "")
		node.graph.complete(node, TaskFailed)
		close(failed)
	})
	approveTasks(t, agent, mustSubmit(t, agent, slowBuildAsk))
	if admitted(graph) != 1 {
		t.Fatal("failed result unexpectedly admitted duplicate work")
	}
	if !strings.Contains(transcriptText(agent), "The build failed because its script was not found.") {
		t.Fatal("failed task result never received an answer")
	}
}

// A WORKER'S OWN TURN IS NEVER GATED HERE.
//
// A node handing a piece out still owes its own report and checks. Its child
// admission is distinct from a conversation handing its request to a task.
func TestAWorkerThatHandsAPieceOutKeepsItsOwnChecks(t *testing.T) {
	completer := &scriptedCompleter{}
	worker, _ := newTestAgent(t, completer, func(config *Config) { config.InTask = true })
	conversation, _ := newTestAgent(t, completer, nil)

	for _, agent := range []*Agent{worker, conversation} {
		agent.mu.Lock()
		agent.running, agent.turnSeq = true, 3
		agent.mu.Unlock()
		graph := stubbedGraph(agent, func(node *TaskNode) {})
		graph.admit(graph.reserve(), taskSpec{
			title: "a piece of it", summary: "one", brief: "one", acceptance: "one",
			owner: agent,
		})
	}

	if worker.turnHandedItsAskOff() {
		t.Error("a task worker's turn read as handed off, so its own completion checks would be skipped")
	}
	if !conversation.turnHandedItsAskOff() {
		t.Error("a conversation that admitted live work in this turn does not read as having handed off")
	}
}

// AND THE FACT IS THE ADMISSION'S, TURN BY TURN.
//
// The gate is a stamp written at [TaskGraph.admit] and never a reading of what
// the model said, so this is what it can and cannot see, stated once.
func TestTheHandoffFactIsQualifiedToTheTurnThatMadeIt(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	// The runner is a stub, so the only landing in this case is the one it
	// makes itself.
	graph := stubbedGraph(agent, func(node *TaskNode) {})

	// Nobody is in a turn: work opened between turns is nobody's handoff.
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "between turns", summary: "one", brief: "one", acceptance: "one", owner: agent})
	if node := graph.node(id); node.admitAt.live() {
		t.Errorf("a node admitted outside a turn was stamped with %+v", node.admitAt)
	}

	agent.mu.Lock()
	agent.running, agent.turnSeq = true, 7
	agent.mu.Unlock()
	id = graph.reserve()
	graph.admit(id, taskSpec{title: "this turn", summary: "one", brief: "one", acceptance: "one", owner: agent})
	node := graph.node(id)
	if node.admitBy != agent || node.admitAt != (requestEpoch{turn: 7}) {
		t.Fatalf("the node was stamped %v/%+v, want this agent and turn 7", node.admitBy, node.admitAt)
	}
	if !agent.turnHandedItsAskOff() {
		t.Fatal("live work admitted in this turn does not read as handed off")
	}

	// The next turn is a new request, and last turn's handoff does not answer
	// for it.
	agent.mu.Lock()
	agent.turnSeq = 8
	agent.mu.Unlock()
	if agent.turnHandedItsAskOff() {
		t.Error("a node admitted in an earlier turn read as this turn's handoff")
	}

	// Neither does a handoff the person has spoken since, inside the same turn.
	agent.mu.Lock()
	agent.turnSeq = 7
	agent.mu.Unlock()
	agent.steerSeq.Add(1)
	if agent.turnHandedItsAskOff() {
		t.Error("work handed out before the person's next sentence read as the answer to it")
	}

	// And a settled node is news to answer, not work in flight.
	agent.steerSeq.Store(0)
	node.finish("done", nil, "", "")
	graph.complete(node, TaskDone)
	if agent.turnHandedItsAskOff() {
		t.Error("a landed node still read as work this turn is waiting on")
	}
}

// runningWatches counts the watches this session has open, which is what the
// measured run's model opened over its own task.
func runningWatches(agent *Agent) int {
	if agent.jobs == nil {
		return 0
	}
	open := 0
	for _, one := range agent.jobs.all() {
		if info := one.info(); info.kind == jobKindWatch && info.state == jobRunning {
			open++
		}
	}
	return open
}
