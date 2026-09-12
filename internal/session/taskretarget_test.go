package session

// THE ONE THING A PERSON MAY MOVE ON A NODE THAT IS ALREADY RUNNING.
//
// A task's model is settled at admission and frozen there (taskmodelfreeze_test.go
// holds that shut), and the freeze is against IMPLICIT DRIFT: a `/model` in the
// conversation must not reach across into work handed over before it turned.
// [Agent.RetargetTask] is the sanctioned exception in the other direction — the
// person, standing in one node's room, choosing for that node and nothing else.
//
// What these tests pin is the whole of the door's contract: which nodes it
// takes, which it refuses and in whose words, what moves when it takes one, and
// — the half that matters most — what does NOT move with it.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// retargetAgent is one running node with a worker in its room, which is the
// state the door is written for: a stubbed runner that parks on the node,
// attaches a child agent to its room the way the real runner does
// (task_run.go), and lands it when the test says so.
func retargetAgent(t *testing.T) (*Agent, *TaskNode, *Agent, func()) {
	t.Helper()
	return retargetAgentWorking(t, &scriptedCompleter{})
}

// retargetAgentWorking is the same rig with the worker's own completer named, so
// a fixture can put a REQUEST IN FLIGHT inside the room rather than only a node.
func retargetAgentWorking(t *testing.T, inside *scriptedCompleter) (*Agent, *TaskNode, *Agent, func()) {
	t.Helper()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.TaskModels = func() []string { return testModels }
	})
	child, _ := newTestAgent(t, inside, func(config *Config) {
		config.Model = "anthropic/claude-opus-5"
	})

	running := make(chan *TaskNode, 1)
	release := make(chan struct{})
	landed := make(chan struct{})
	graph := agent.graph()
	id := graph.reserve()
	// Only the node under test is PARKED. A test that admits a second one wants
	// it out of the way, not held open beside the subject.
	stubbedGraph(agent, func(node *TaskNode) {
		if node.id != id {
			node.finish("done", nil, "", "")
			node.graph.complete(node, TaskDone)
			return
		}
		node.openRoom().speaking(child)
		running <- node
		<-release
		node.finish("it landed", nil, "", "")
		node.graph.complete(node, TaskDone)
		close(landed)
	})
	// named, because this stands in for a proposal the model groomed and named
	// itself — the one kind of work the namer leaves alone (taskname.go).
	graph.admit(id, taskSpec{
		title: "Ship the parser fix", named: true, brief: "b", acceptance: "a",
		model: "anthropic/claude-opus-5",
	})

	var node *TaskNode
	select {
	case node = <-running:
	case <-time.After(5 * time.Second):
		t.Fatal("the admitted node never started")
	}
	return agent, node, child, func() {
		close(release)
		select {
		case <-landed:
		case <-time.After(5 * time.Second):
			t.Fatal("the node never landed")
		}
	}
}

// A RUNNING NODE MOVES, AND EVERYTHING THAT RECORDS ITS MODEL MOVES WITH IT: the
// spec the checkpoint is written from, the row a surface draws, and the worker
// itself — which is what makes the switch a fact about the work rather than a
// label. WHEN it reaches the work is the separate law in personsword_test.go.
func TestRetargetTaskMovesARunningNodeAndEveryRowThatNamesIt(t *testing.T) {
	agent, node, child, land := retargetAgent(t)
	updates := agent.TaskUpdates()
	// THE LANE OPENS ON THE ROSTER ([Agent.replayTaskRoster]): the running
	// node's row arrives the moment the lane does, as the node stands now —
	// before the retarget. It comes off first so the assertion below reads
	// what the retarget published, not what the subscription replayed.
	select {
	case event := <-updates:
		if event.Kind != EventTaskUpdate || event.Task == nil || event.Task.ID != node.id {
			t.Fatalf("the lane opened on %v rather than the node's roster row", event.Kind)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the lane opened on nothing")
	}

	if _, err := agent.RetargetTask(node.id, "anthropic/claude-sonnet-5"); err != nil {
		t.Fatalf("RetargetTask on a running node: %v", err)
	}
	if got := node.model(); got != "anthropic/claude-sonnet-5" {
		t.Fatalf("the node's spec still says %q", got)
	}
	if got := node.notice().Model; got != "anthropic/claude-sonnet-5" {
		t.Fatalf("the node's row still names %q", got)
	}
	// THE WORKER ITSELF, which is the mechanism and not a copy of the fact:
	// [Agent.SetModel] is what /model uses, and its contract is that a turn in
	// flight finishes on the model it started on while the next one takes the new
	// id (agent_test.go's TestSetModelAppliesFromTheNextTurn holds that shut).
	if got := child.Model(); got != "anthropic/claude-sonnet-5" {
		t.Fatalf("the worker is still on %q", got)
	}
	// And a surface holding the standing subscription is told, so the roster row,
	// the room's status line and the card this lands as all say the new id.
	select {
	case event := <-updates:
		if event.Kind != EventTaskUpdate || event.Task == nil {
			t.Fatalf("the retarget published %v", event.Kind)
		}
		if event.Task.Model != "anthropic/claude-sonnet-5" {
			t.Fatalf("the published row names %q", event.Task.Model)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the retarget published nothing")
	}

	// AT LANDING THE PROJECT'S RECORD CARRIES THE MODEL IT ACTUALLY FINISHED ON.
	land()
	var found bool
	for _, entry := range agent.TaskIndex() {
		if entry.Title != "Ship the parser fix" {
			continue
		}
		found = true
		if entry.Model != "anthropic/claude-sonnet-5" {
			t.Fatalf("the landed row records %q", entry.Model)
		}
	}
	if !found {
		t.Fatal("the landed node left no row in the index")
	}
}

// THE WORD RESOLVES THROUGH ADMISSION'S OWN LADDER, so a room and a proposal
// cannot disagree about what one word means. A tail, a family word and a whole
// id all reach the same place.
func TestRetargetTaskResolvesTheWordTheWayAdmissionDoes(t *testing.T) {
	for _, tc := range []struct{ word, want string }{
		{"anthropic/claude-sonnet-5", "anthropic/claude-sonnet-5"},
		{"claude-sonnet-5", "anthropic/claude-sonnet-5"},
		{"sonnet 5", "anthropic/claude-sonnet-5"},
		{"  CLAUDE-OPUS-4.8  ", "anthropic/claude-opus-4.8"},
	} {
		agent, node, child, land := retargetAgent(t)
		if _, err := agent.RetargetTask(node.id, tc.word); err != nil {
			t.Fatalf("RetargetTask(%q): %v", tc.word, err)
		}
		if got := node.model(); got != tc.want {
			t.Fatalf("%q resolved to %q, want %q", tc.word, got, tc.want)
		}
		if got := child.Model(); got != tc.want {
			t.Fatalf("%q put the worker on %q, want %q", tc.word, got, tc.want)
		}
		land()
	}
}

// NOTHING ELSE IN THE SESSION MOVES. This is the half the whole exception rests
// on: the person chose for ONE node, so the conversation stays where it is, the
// next task still takes the ordinary ladder, and a second node is untouched.
func TestRetargetTaskMovesNothingButTheNodeItNames(t *testing.T) {
	agent, node, _, land := retargetAgent(t)
	other := agent.graph().reserve()
	agent.graph().admit(other, taskSpec{
		title: "the other one", brief: "b", acceptance: "a", model: "openai/gpt-5",
	})

	if _, err := agent.RetargetTask(node.id, "claude-sonnet-5"); err != nil {
		t.Fatalf("RetargetTask: %v", err)
	}
	if got := agent.Model(); got != "test/model" {
		t.Fatalf("retargeting a task moved the conversation to %q", got)
	}
	if second := agent.graph().node(other); second != nil && second.model() != "openai/gpt-5" {
		t.Fatalf("retargeting one task moved another to %q", second.model())
	}
	// A TASK ADMITTED AFTERWARDS STILL TAKES THE LADDER — the configured task
	// model, else the conversation's — because a pick made in one room is not a
	// preference the session learned (taskmodel.go's [Agent.defaultTaskModel]).
	if got := agent.resolveTaskModel("").model; got != "test/model" {
		t.Fatalf("a task admitted after the retarget would run on %q", got)
	}
	land()
}

// A saved continuation choice leaves the completed attempt and its worker intact.
func TestRetargetTaskSavesContinuationWithoutRewritingTheAttempt(t *testing.T) {
	agent, node, child, land := retargetAgent(t)
	land()
	waitDoneNode(t, node)
	before := node.notice()
	if before.Brief != "b" || before.Acceptance != "a" {
		t.Fatal("task snapshot omitted its original contract")
	}
	if _, err := agent.RetargetTask(node.id, "claude-sonnet-5"); err != nil {
		t.Fatal(err)
	}
	if err := agent.SetTaskEffort(node.id, "high"); err != nil {
		t.Fatal(err)
	}
	after := node.notice()
	if after.State != before.State || after.Model != before.Model || after.Report != before.Report || child.Model() != before.Model {
		t.Fatal("saving continuation settings changed the completed attempt")
	}
	if after.NextModel != "anthropic/claude-sonnet-5" {
		t.Fatalf("next model: %q", after.NextModel)
	}
	node.graph.mu.Lock()
	record := node.recordLocked()
	node.graph.mu.Unlock()
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var disk taskRecord
	if err := json.Unmarshal(encoded, &disk); err != nil {
		t.Fatal(err)
	}
	restored := restoreNode(newTaskGraph(), disk)
	if restored.nextModel != after.NextModel || restored.nextEffort == nil || *restored.nextEffort != "high" || restored.spec.model != before.Model {
		t.Fatal("continuation setup did not survive the checkpoint record")
	}
	seen := make(chan string, 1)
	node.graph.mu.Lock()
	node.graph.run = func(n *TaskNode) {
		seen <- n.model() + "/" + n.effortRung().String()
		n.finish("continued", nil, "", "")
		n.graph.complete(n, TaskDone)
	}
	node.graph.mu.Unlock()
	if err := agent.ContinueTask(node.id, "continue the work"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-seen:
		if got != "anthropic/claude-sonnet-5/high" {
			t.Fatalf("continued worker setup: %s", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("continuation never ran")
	}
	waitDoneNode(t, node)
	if node.notice().NextModel != "" {
		t.Fatal("consumed model choice remains pending")
	}
}

func TestRetargetTaskRefusesAnUnknownIdAndAnUnknownModel(t *testing.T) {
	agent, node, _, land := retargetAgent(t)
	defer land()

	if _, err := agent.RetargetTask(node.id+7, "claude-sonnet-5"); err == nil {
		t.Fatal("RetargetTask invented a task")
	} else if want := "no task"; !strings.Contains(err.Error(), want) {
		t.Fatalf("the refusal reads %q, want one naming the missing id", err)
	}
	if _, err := agent.RetargetTask(node.id, "   "); err == nil {
		t.Fatal("RetargetTask accepted an empty model")
	}
	_, err := agent.RetargetTask(node.id, "opos-5")
	if err == nil {
		t.Fatal("RetargetTask accepted a model this install does not have")
	}
	if want := `no model here is called "opos-5"`; !strings.Contains(err.Error(), want) {
		t.Fatalf("the refusal reads %q, want admission's own wording", err)
	}
	// A word that fits more than one model is a QUESTION, and this door has
	// nobody to ask — so it comes back as the same refusal a too-vague proposal
	// gets, naming the candidates.
	if _, err := agent.RetargetTask(node.id, "opus"); err == nil {
		t.Fatal("an ambiguous word was silently settled")
	} else if !strings.Contains(err.Error(), "matches several models") {
		t.Fatalf("the ambiguous refusal reads %q", err)
	}
	// None of them moved anything.
	if got := node.model(); got != "anthropic/claude-opus-5" {
		t.Fatalf("a refused retarget moved the node to %q", got)
	}
}

// A PERSON'S PICK SUPERSEDES THE TOOL-USE RESCUE. [TaskNode.ran] exists so the
// swap onto the worker tier can be told without unfreezing the spec; once the
// spec IS the person's own answer there is nothing left for it to say, and the
// sentence beside it goes with it — a row that kept either would name the
// rescued model while the person looked at the one they just chose.
func TestRetargetTaskTakesDownTheToolUseRescuesOwnNote(t *testing.T) {
	agent, node, _, land := retargetAgent(t)
	defer land()

	node.graph.mu.Lock()
	node.mend = taskModelRescueNote("openai/gpt-5", "anthropic/claude-opus-5")
	node.ran = "anthropic/claude-opus-5"
	node.graph.mu.Unlock()

	if _, err := agent.RetargetTask(node.id, "claude-sonnet-5"); err != nil {
		t.Fatalf("RetargetTask: %v", err)
	}
	notice := node.notice()
	if notice.Model != "anthropic/claude-sonnet-5" {
		t.Fatalf("the row still names the rescued model %q", notice.Model)
	}
	if notice.Mending != "" {
		t.Fatalf("the rescue's sentence outlived the pick that replaced it: %q", notice.Mending)
	}
	// A REPAIR ROUND'S LINE IS NOT THE RESCUE'S and must survive: it is about the
	// work, and this door has nothing to say about it.
	node.graph.mu.Lock()
	node.mend = "adding amp-labs to the report"
	node.ran = "anthropic/claude-opus-5"
	node.graph.mu.Unlock()
	if _, err := agent.RetargetTask(node.id, "claude-opus-4.8"); err != nil {
		t.Fatalf("RetargetTask: %v", err)
	}
	if got := node.notice().Mending; got != "adding amp-labs to the report" {
		t.Fatalf("a repair round's line was taken down with the rescue's: %q", got)
	}
}

// A PICK MADE WHILE THE GATE IS READING THE WORK IS THE NEXT RUN'S, AND THE ROW
// KEEPS THE MODEL THAT ACTUALLY RAN.
//
// This is the defect the phase read exists for. A node stays TaskRunning across
// its worker, the check and every repair round, and its worker stops reading the
// moment [runTaskChild] returns — minutes before a checked node settles. Reading
// the state alone, this door rewrote the frozen spec of work that was already
// finished, so the row, the checkpoint and the landed card all named a model that
// never ran a token of it while the bill named the one that did.
//
// It was measured: a node admitted on the worker tier with all nineteen of its
// calls billed there, and a card claiming the install's low tier because a pick
// landed during the check.
func TestAModelPickedWhileTheCheckReadsBecomesTheNextRunsAndLeavesTheRowAlone(t *testing.T) {
	agent, node, _, land := retargetAgent(t)
	defer land()

	// The check's own two facts, in the order the runner produces them: the
	// worker's reading is over, and the node has entered the gate's phase.
	node.openRoom().speaking(nil)
	node.living(TaskPhaseChecking)

	if _, err := agent.RetargetTask(node.id, "claude-sonnet-5"); err != nil {
		t.Fatalf("RetargetTask during the check: %v", err)
	}
	notice := node.notice()
	if notice.Model != "anthropic/claude-opus-5" {
		t.Fatalf("the row names %q, want the model the work actually ran on", notice.Model)
	}
	if notice.NextModel != "anthropic/claude-sonnet-5" {
		t.Fatalf("the pick was lost rather than kept for the next run: %q", notice.NextModel)
	}
	// AND THE SPEC ITSELF IS UNTOUCHED, which is the half a row cannot show: it is
	// what the checkpoint writes and what a resumed session would draw.
	node.graph.mu.Lock()
	frozen := node.spec.model
	node.graph.mu.Unlock()
	if frozen != "anthropic/claude-opus-5" {
		t.Fatalf("the frozen admitted id moved to %q", frozen)
	}
}

// AND A WORKER THAT IS STILL READING IS STILL MOVED, which is the ordinary case
// and the reason the read above is one condition rather than a refusal: the phase
// answers "is there a turn left to take this", and while the node's own worker
// holds it the answer is yes.
func TestAModelPickedWhileTheWorkerReadsStillMovesTheWork(t *testing.T) {
	agent, node, child, land := retargetAgent(t)
	defer land()

	node.living(TaskPhaseWorking)
	if _, err := agent.RetargetTask(node.id, "claude-sonnet-5"); err != nil {
		t.Fatalf("RetargetTask while the worker reads: %v", err)
	}
	notice := node.notice()
	if notice.Model != "anthropic/claude-sonnet-5" {
		t.Fatalf("the row names %q, want the model just picked", notice.Model)
	}
	if notice.NextModel != "" {
		t.Fatalf("a pick that moved the live work also armed the next run: %q", notice.NextModel)
	}
	if got := child.Model(); got != "anthropic/claude-sonnet-5" {
		t.Fatalf("the worker in the room is still on %q", got)
	}
}
