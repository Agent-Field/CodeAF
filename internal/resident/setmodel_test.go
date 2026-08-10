package resident

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// spliceModelJob is one job with two leaves and a cheap pin — enough for a
// rebinding to have something to move, something already in flight, and a
// sibling job it must not touch.
func spliceModelJob(t *testing.T, graph *store.Store) {
	t.Helper()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "the job", Title: "Job", Stage: 2},
		{ID: "flight", Parent: "job", Brief: "already in flight", Title: "Flight", Stage: 1},
		{ID: "queued", Parent: "job", Brief: "not started", Title: "Queued", Stage: 1},
	}}, store.Provenance{
		Origin: store.OriginUser, SessionID: "models", Intent: "a job with a pin",
		WorkModel: "cheap-model",
	}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "elsewhere", Brief: "an unrelated job", Title: "Elsewhere", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "models", Intent: "another job"}); err != nil {
		t.Fatal(err)
	}
}

// spokenReceipt finds the one visible line a command produced. It is spoken
// rather than filed because nothing answers for CommandSetModel in its own
// voice yet — no head sentence precedes it, so its receipt is the answer.
func spokenReceipt(t *testing.T, graph *store.Store, sessionID string, commandSeq int64) store.Message {
	t.Helper()
	messages, err := graph.Messages(sessionID, 0, 0)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	for _, message := range messages {
		if message.CommandSeq == commandSeq {
			if message.Role != store.RoleAgent {
				t.Fatalf("set-model receipt role = %s, want it spoken", message.Role)
			}
			return message
		}
	}
	t.Fatalf("no receipt for command %d in %+v", commandSeq, messages)
	return store.Message{}
}

// TestSetModelRepointsLiveWorkAndSaysSo is the arm end to end: the command is
// applied, every live node under the target picks up the new model, the leaf
// already running is left exactly as it was, and the person is told both halves
// in one line.
func TestSetModelRepointsLiveWorkAndSaysSo(t *testing.T) {
	graph := openStore(t)
	spliceModelJob(t, graph)
	claim, won, err := graph.Claim("flight", "worker-1")
	if err != nil || !won {
		t.Fatalf("claim: %v won=%t", err, won)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}

	reconciler := New(graph, nil, nil)
	command, err := graph.RequestCommand(store.Command{
		SessionID: "models", Kind: store.CommandSetModel, Target: "job",
		Instruction: "strong-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	if settled := commandBySeq(t, graph, command.Seq); settled.Status != store.CommandApplied {
		t.Fatalf("command = %s (%s)", settled.Status, settled.Result)
	}
	for id, want := range map[string]string{
		"job":       "strong-model",
		"flight":    "strong-model",
		"queued":    "strong-model",
		"elsewhere": "",
	} {
		node, found, err := graph.Node(id)
		if err != nil || !found {
			t.Fatalf("read %q: %v found=%t", id, err, found)
		}
		if node.Provenance.WorkModel != want {
			t.Fatalf("%q work model = %q, want %q", id, node.Provenance.WorkModel, want)
		}
	}
	// The leaf in flight keeps its claim: a model change is never a stop, and
	// the turn that is already running settles as if nothing happened.
	running, _, _ := graph.Node("flight")
	if running.Status != store.Running || running.Owner != "worker-1" {
		t.Fatalf("running leaf = %+v", running)
	}
	if err := graph.Complete(claim, "finished on the model it started with"); err != nil {
		t.Fatalf("complete after rebinding: %v", err)
	}

	receipt := spokenReceipt(t, graph, "models", command.Seq)
	if !strings.Contains(receipt.Body, "switching remaining work to strong-model") {
		t.Fatalf("receipt = %q", receipt.Body)
	}
	if !strings.Contains(receipt.Body, "1 already running finishes on the model they started on") {
		t.Fatalf("receipt did not name the running work: %q", receipt.Body)
	}
}

// TestSetModelSurvivesRebuild proves the rebinding is journal-durable through
// the reconciler path and not only through the store surface: a replay of
// everything the command wrote lands on the same models.
func TestSetModelSurvivesRebuild(t *testing.T) {
	graph := openStore(t)
	spliceModelJob(t, graph)
	reconciler := New(graph, nil, nil)
	if _, err := graph.RequestCommand(store.Command{
		SessionID: "models", Kind: store.CommandSetModel, Target: "job",
		Instruction: "strong-model",
	}); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	for _, id := range []string{"job", "flight", "queued"} {
		node, _, err := graph.Node(id)
		if err != nil {
			t.Fatal(err)
		}
		if node.Provenance.WorkModel != "strong-model" {
			t.Fatalf("%q work model after rebuild = %q", id, node.Provenance.WorkModel)
		}
	}
}

// TestSetModelRefusesWhenThereIsNothingLeftToMove is the race the funnel cannot
// catch: the target was live when the command was written and finished before
// the reconciler reached it. The refusal speaks, because a request that
// silently did nothing is the failure the receipt rules exist to prevent.
func TestSetModelRefusesWhenThereIsNothingLeftToMove(t *testing.T) {
	graph := openStore(t)
	spliceModelJob(t, graph)
	reconciler := New(graph, nil, nil)
	command, err := graph.RequestCommand(store.Command{
		SessionID: "models", Kind: store.CommandSetModel, Target: "job",
		Instruction: "strong-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"flight", "queued", "job"} {
		claim, won, err := graph.Claim(id, "worker-1")
		if err != nil || !won {
			t.Fatalf("claim %q: %v won=%t", id, err, won)
		}
		if err := graph.Complete(claim, "done before the command landed"); err != nil {
			t.Fatal(err)
		}
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	settled := commandBySeq(t, graph, command.Seq)
	if settled.Status != store.CommandRejected {
		t.Fatalf("command = %s (%s)", settled.Status, settled.Result)
	}
	receipt := spokenReceipt(t, graph, "models", command.Seq)
	if !strings.Contains(receipt.Body, "I couldn't switch models") {
		t.Fatalf("receipt = %q", receipt.Body)
	}
	node, _, _ := graph.Node("job")
	if node.Provenance.WorkModel != "cheap-model" {
		t.Fatalf("settled job was re-pointed to %q", node.Provenance.WorkModel)
	}
}

// TestSetModelWithoutAModelRefuses covers the arm's own guard. The funnel
// already refuses an empty instruction, so this can only be reached by a caller
// inside the process — and it must decline rather than write an empty pin over
// a live subtree.
func TestSetModelWithoutAModelRefuses(t *testing.T) {
	graph := openStore(t)
	spliceModelJob(t, graph)
	reconciler := New(graph, nil, nil)
	outcome, err := reconciler.setModel(store.Command{
		SessionID: "models", Kind: store.CommandSetModel, Target: "job", Instruction: "   ",
	})
	if err != nil {
		t.Fatalf("blank model faulted instead of refusing: %v", err)
	}
	if outcome.status != store.CommandRejected || !strings.Contains(outcome.receipt, "no model was named") {
		t.Fatalf("outcome = %+v", outcome)
	}
	node, _, _ := graph.Node("job")
	if node.Provenance.WorkModel != "cheap-model" {
		t.Fatalf("blank model reached the graph: %q", node.Provenance.WorkModel)
	}

	// A target that vanished entirely is news, not a fault, and never a panic.
	missing, err := reconciler.setModel(store.Command{
		SessionID: "models", Kind: store.CommandSetModel, Target: "ghost", Instruction: "strong-model",
	})
	if err != nil {
		t.Fatalf("missing target faulted instead of refusing: %v", err)
	}
	if missing.status != store.CommandRejected {
		t.Fatalf("missing target outcome = %+v", missing)
	}
}
