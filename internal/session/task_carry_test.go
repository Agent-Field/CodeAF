package session

// WHAT THE PRODUCTION CONSTRUCTOR CARRIES DOWN, PROVED THROUGH THE PRODUCTION
// CONSTRUCTOR.
//
// docs/TRAJECTORIES.md's C1 was one missing field in a literal that copies about
// thirty, and the reason eighteen tests missed it was that every one of them
// hand-built the worker's Config. So nothing in this file writes a Config
// literal for a worker: each test asks [Agent.newTaskAgent] for one, exactly as
// [Agent.workTaskNode] does, and asks the worker what it actually has.
//
// It covers the second level as well as the first, because that is where the
// remaining gaps lived: a PART's owner is not the conversation, it is its
// parent's worker, and anything a worker did not carry is something a part is
// built without.

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// pieceOf admits one part under a node and hands back the row. It is the shape
// [Agent.divideWork] and a node's own propose_task both admit: the proposing
// node's id, its depth plus one, and its own agent as the owner.
func pieceOf(t *testing.T, graph *TaskGraph, parent *TaskNode, owner *Agent, title string) *TaskNode {
	t.Helper()
	id := graph.reserve()
	graph.admit(id, taskSpec{
		title: title, brief: "b", acceptance: "a",
		parent: parent.id, depth: parent.depth + 1, owner: owner,
	})
	return graph.node(id)
}

// THE PROVIDER REPAIR REACHES EVERY WORKER, AND IT TRAVELS WITH THE CONNECTION
// RATHER THAN WITH THE CONFIG.
//
// Routing, ModelFallbacks and NearestModels are read in exactly one place —
// [New], where they are handed to the provider client (agent.go) — so what
// decides whether a node routes the person's way, falls back down the person's
// list, and gets the catalog's nearest-model rescue is whether it asks through
// the person's client. [Agent.newTaskAgent] hands that client down, and this
// pins it: the audit read the Config literal, found the three fields missing and
// wrote the node off as having "the zero routing strategy" — which would have
// been true if a node built its own connection, and is why nobody may make it
// build one without answering this test.
//
// The grandchild is the whole point. A part is built by its parent's WORKER, so
// a connection that stopped one level down would leave exactly the agents
// division creates asking through nothing.
func TestTheProductionConstructorCarriesTheProviderRepairOntoEveryWorker(t *testing.T) {
	client, err := provider.NewClient(provider.Config{
		APIKey:  "test-key",
		BaseURL: "https://example.invalid/api",
		Model:   "test/model",
		Timeout: time.Second,
		// The three the person set, resolved: how to choose an endpoint, where
		// to go when none of them will take the request at all, and who answers
		// when they wrote no list of their own.
		Routing:       provider.StaticRouting(provider.RoutingPrice),
		Fallbacks:     []string{"other/model"},
		NearestModels: func(string) []string { return []string{"nearest/model"} },
	})
	if err != nil {
		t.Fatalf("provider.NewClient: %v", err)
	}
	session, err := newAgent(Config{Workspace: t.TempDir(), Model: "test/model", System: "SYSTEM"}, client)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	worker, node := workerFor(t, session, taskSpec{
		title: "the whole job", brief: "b", acceptance: "a", depth: 1,
	})
	if inner := unwrapCompleter(worker.client); inner != Completer(client) {
		t.Fatal("a worker built by the real constructor asks through a connection that is not the person's, so its routing, its fallbacks and its nearest-model rescue are not theirs either")
	}
	// AND THE WRAPPER AROUND IT IS THE NODE'S OWN, which is the other half of
	// the same line: one connection, one cache lineage per agent.
	if worker.cacheKey == session.cacheKey {
		t.Fatal("the node took the conversation's prompt-cache lineage, so two different transcripts cold-start each other on one replica")
	}

	piece := pieceOf(t, session.graph(), node, worker, "one part of it")
	part, err := worker.newTaskAgent(context.Background(), t.TempDir(), piece, "")
	if err != nil {
		t.Fatalf("the production constructor refused to build a part's worker: %v", err)
	}
	t.Cleanup(func() { _ = part.Close() })
	if inner := unwrapCompleter(part.client); inner != Completer(client) {
		t.Fatal("a part asks through a connection that is not the person's: the repair reaches the first level of work and stops")
	}
}

// THE NO-TOOLS RESCUE MUST WORK AT EVERY DEPTH. [Agent.newTaskAgent] reads
// SupportsParameter off the parent's config to refuse a model that cannot hold a
// tool at all — and until this lane it did not carry the answer down, so the
// check ran for a conversation's own task and for nothing below it. C1 made that
// reachable far more often: every part division hands out is built by a worker.
func TestAPartIsBuiltWithTheSameNoToolsRescueItsParentHad(t *testing.T) {
	asked := map[string]bool{}
	session, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SupportsParameter = func(model, parameter string) (bool, bool) {
			asked[model] = true
			// The one model in this test that cannot hold a tool, and a
			// definite answer for everything else so the rescue has somewhere
			// to go.
			return !strings.Contains(model, "no-tools"), true
		}
	})
	worker, node := workerFor(t, session, taskSpec{
		title: "the whole job", brief: "b", acceptance: "a", depth: 1,
	})
	if worker.config.SupportsParameter == nil {
		t.Fatal("the worker cannot tell whether a model holds a tool, so every piece it hands out is built with no check at all")
	}

	piece := pieceOf(t, session.graph(), node, worker, "one part of it")
	piece.retarget("test/no-tools-model")
	part, err := worker.newTaskAgent(context.Background(), t.TempDir(), piece, "")
	if err != nil {
		t.Fatalf("the production constructor refused to build a part's worker: %v", err)
	}
	t.Cleanup(func() { _ = part.Close() })

	if !asked["test/no-tools-model"] {
		t.Fatalf("nobody asked whether the part's own model can hold a tool; what was asked about is %v", asked)
	}
	if got := part.Model(); got == "test/no-tools-model" {
		t.Fatal("the part started on a model that cannot call a tool: the rescue stopped one level up")
	}
	// AND THE ROW SAYS WHAT IT IS RUNNING ON. The swap is a fact about this
	// piece that a person reading its card is owed, exactly as it is for a task
	// the conversation handed out itself.
	piece.graph.mu.Lock()
	mend, ran := piece.mend, piece.ran
	piece.graph.mu.Unlock()
	if strings.TrimSpace(mend) == "" || ran != part.Model() {
		t.Fatalf("the part was moved to %q with mend %q and row model %q, want the swap said out loud", part.Model(), mend, ran)
	}
}

// AND THE LEASH'S SEAM REACHES AS FAR AS THE LEASH DOES. TaskProgressCheck is
// asked of the agent that OWNS the node (task_run.go's [Agent.taskProgress]),
// and a part's owner is its parent's worker — so a seam that stopped at the
// first level left a part's checkpoint as the one threshold no test could put a
// deterministic answer behind. Production leaves it nil and asks the real
// read-only checker either way.
func TestAPartsLeashIsCheckedByTheSameHandTheConversationSet(t *testing.T) {
	session, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.TaskProgressCheck = func(string, []string) (bool, string) { return false, "still circling" }
	})
	worker, node := workerFor(t, session, taskSpec{
		title: "the whole job", brief: "b", acceptance: "a", depth: 1,
	})
	if worker.config.TaskProgressCheck == nil {
		t.Fatal("the worker has no progress check, so nothing decides a part's checkpoint but the auditor a test cannot script")
	}
	piece := pieceOf(t, session.graph(), node, worker, "one part of it")
	working, reason := worker.taskProgress(context.Background(), piece, t.TempDir(), []string{"the same grep again"}, io.Discard)
	if working || !strings.Contains(reason, "still circling") {
		t.Fatalf("the part's checkpoint answered working=%v %q, want the conversation's own hand", working, reason)
	}
}
