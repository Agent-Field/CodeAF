package session

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
)

// WHICH MODEL A PIECE OF WORK RUNS ON, AND WHETHER THE ROW SAYS SO.
//
// Three holes, all of them on the publishing side rather than in the choosing:
// a person's task picked its model minutes after it was admitted, a run's nodes
// resolved the worker tier and then published nothing about it, and a node whose
// model was swapped for an incapable one kept naming the model it had refused.
// The law all three now keep: A ROW NAMES THE MODEL ITS WORK IS ACTUALLY ON, and
// the id is settled once — at admission for a task, at the run's start for its
// nodes — so nothing about it moves under a later /model.
//
// What none of this claims is anything deeper than the first node. A task may
// spawn work of its own and that work settles its own model when IT is admitted.

// A PERSON'S TASK FREEZES ITS MODEL AT ADMISSION. It used to admit an empty
// model field and let the worker fall to whatever the conversation was on when
// it eventually started, which is the same task on a different model depending
// on how long the queue was.
func TestAPersonsTaskFreezesItsModelAtAdmission(t *testing.T) {
	agent, ran := shapeAgent(t, &scriptedCompleter{})
	id, _, _, err := agent.StartTask(t.Context(), "tidy the loader")
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	node := agent.graph().node(id)
	if node == nil {
		t.Fatal("the task was not admitted")
	}
	if got := node.model(); got != "test/model" {
		t.Fatalf("the task was admitted on %q, want the conversation's %q", got, "test/model")
	}
	// AND THE SWITCH AFTER IT DOES NOT REACH BACK. This is the whole of the
	// freeze: the id is the spec's from here on, and the conversation moving is
	// not a fact about work that was already handed over.
	agent.SetModel("someone/else-9")
	if got := node.model(); got != "test/model" {
		t.Fatalf("a /model after admission moved the task to %q", got)
	}
	// And the row a surface draws says the same thing, which is the half that was
	// blank before: an empty spec model published an empty [TaskNotice.Model], so
	// the node's own room had nothing to say about what was running it.
	if got := node.notice().Model; got != "test/model" {
		t.Fatalf("the task's row names %q", got)
	}
	settled(t, ran)
}

// THE CONFIGURED task.model WINS AT THAT SAME MOMENT, which is the other half of
// the ladder [Agent.resolveTaskModel] walks for a proposal that names nothing.
func TestAPersonsTaskTakesTheConfiguredTaskModel(t *testing.T) {
	agent, ran := shapeAgent(t, &scriptedCompleter{})
	agent.mu.Lock()
	agent.config.TaskModel = "cheap/worker-2"
	agent.mu.Unlock()

	id, _, _, err := agent.StartTask(t.Context(), "tidy the loader")
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if got := agent.graph().node(id).model(); got != "cheap/worker-2" {
		t.Fatalf("the task was admitted on %q, want the configured %q", got, "cheap/worker-2")
	}
	settled(t, ran)
}

// A RUN'S NODES SAY WHICH MODEL IS DOING THE WORK, and it is not the planner's
// on the row above them. These are the one class of work whose model genuinely
// differs from the conversation's under the shipped crew — one careful call
// decides what happens, many cheap ones do it — and they were the one class of
// row that published nothing at all.
func TestARunsNodesPublishTheModelTheyRunOn(t *testing.T) {
	agent, updates := familyAgent(t)
	family := agent.newOrchestrateFamily("audit the pricing code", "thinking/planner", "run-pricing")
	family.worker = "cheap/worker"

	family.upsert([]orchestrate.NodeStatus{
		node("n1", "tariff table", "You are reading the tariff table. Report what it charges.", orchestrate.Running),
		node("n2", "invoice writer", "You are reading the invoice writer. Report what it emits.", orchestrate.Queued),
	})

	notices := familyNotices(t, updates)
	// Four rows: the run, its two nodes, and the run again once its workers exist
	// and the forming line comes off it ([orchestrateFamily.formingLocked]).
	if len(notices) != 4 {
		t.Fatalf("%d rows published, want the run, its two nodes and the run again: %+v", len(notices), notices)
	}
	// The root keeps the planner's — the judgement the tank is paying for.
	if notices[0].Model != "thinking/planner" {
		t.Fatalf("the run's own row names %q", notices[0].Model)
	}
	for _, kid := range notices[1:3] {
		if kid.Model != "cheap/worker" {
			t.Fatalf("node %q names %q, want the worker's %q", kid.Node, kid.Model, "cheap/worker")
		}
	}
}

// AND A FAMILY NOBODY HANDED A WORKER MODEL PUBLISHES NOTHING rather than
// inventing one — the emptiness law, said about a model name.
func TestARunWithNoWorkerModelSaysNothingAboutOne(t *testing.T) {
	agent, updates := familyAgent(t)
	family := agent.newOrchestrateFamily("audit the pricing code", "", "run-pricing")

	family.upsert([]orchestrate.NodeStatus{node("n1", "tariff table", "You are reading the tariff table. Report what it charges.", orchestrate.Running)})

	notices := familyNotices(t, updates)
	// The run, its one node, and the run again with the forming line taken off.
	if len(notices) != 3 {
		t.Fatalf("%d rows published: %+v", len(notices), notices)
	}
	if notices[1].Model != "" {
		t.Fatalf("a node with no resolved model published %q", notices[1].Model)
	}
}

// THE TOOL-USE RESCUE IS TOLD IN ONE VOICE. When a node's frozen model turns out
// to have no tool use, [Agent.newTaskAgent] swaps it for the worker tier and says
// so on `mend`. The row used to keep publishing the REJECTED id beside that
// sentence, so one card named two models and disagreed with itself about which
// one was running.
func TestANodeRescuedOntoAnotherModelNamesTheOneItRunsOn(t *testing.T) {
	// The runner is stubbed for [shapeAgent]'s reason: admission STARTS a node,
	// and what this test is about is the row the node publishes rather than a
	// worktree still being written while the temp directory is taken away.
	agent, ran := shapeAgent(t, &scriptedCompleter{})
	graph := agent.graph()
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "ship it", brief: "ship it", model: "toyless/model"})
	settled(t, ran)
	node := graph.node(id)

	if got := node.notice().Model; got != "toyless/model" {
		t.Fatalf("before any swap the row names %q", got)
	}
	// What the rescue writes, in the one place it writes it.
	graph.mu.Lock()
	node.mend = "model toyless/model has no tools; using cheap/worker"
	node.ran = "cheap/worker"
	graph.mu.Unlock()

	notice := node.notice()
	if notice.Model != "cheap/worker" {
		t.Fatalf("the rescued row names %q, want the model it is on", notice.Model)
	}
	if notice.Mending != "model toyless/model has no tools; using cheap/worker" {
		t.Fatalf("the sentence beside it reads %q", notice.Mending)
	}
	// THE SPEC IS STILL WHAT WAS ASKED FOR. The contract is frozen and
	// checkpointed; the swap is a second fact about the node, not an edit to it.
	if got := node.model(); got != "toyless/model" {
		t.Fatalf("the rescue rewrote the frozen spec to %q", got)
	}
}
