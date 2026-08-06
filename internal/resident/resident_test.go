package resident

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestTickAppliesSpliceAndPostsCompiledReceipt(t *testing.T) {
	graph := openStore(t)
	instruction := "  Benchmark the parser without changing its output.  "
	command, err := graph.RequestCommand(store.Command{
		SessionID:   "session-splice",
		Kind:        store.CommandSplice,
		Instruction: instruction,
	})
	if err != nil {
		t.Fatalf("request command: %v", err)
	}

	compile := func(_ context.Context, got, graphContext string) (Compiled, error) {
		if got != instruction {
			return Compiled{}, fmt.Errorf("instruction = %q, want verbatim %q", got, instruction)
		}
		if !strings.Contains(graphContext, "root | Permanent Aforge spine | running") {
			return Compiled{}, fmt.Errorf("graph context omitted active root: %q", graphContext)
		}
		return Compiled{
			Goal: "Benchmark the parser and preserve observable output",
			Assumptions: []string{
				"main is the comparison baseline",
				"the existing benchmark harness is sufficient",
			},
			Scale: "project",
		}, nil
	}
	plan := func(ctx context.Context, compiled Compiled) (store.Subtree, error) {
		anchor, ok := PlanAnchorFromContext(ctx)
		wantNodeID := fmt.Sprintf("task-%d", command.Seq)
		if !ok || anchor.NodeID != wantNodeID || anchor.SessionID != command.SessionID ||
			anchor.CommandSeq != command.Seq {
			return store.Subtree{}, fmt.Errorf("plan anchor = %+v ok=%t", anchor, ok)
		}
		if compiled.Goal != "Benchmark the parser and preserve observable output" {
			return store.Subtree{}, fmt.Errorf("goal = %q", compiled.Goal)
		}
		if compiled.Scale != "project" {
			return store.Subtree{}, fmt.Errorf("scale = %q, want project", compiled.Scale)
		}
		return store.Subtree{Nodes: []store.NodeSpec{
			{ID: "benchmark", Brief: compiled.Goal, Stage: 1},
			{ID: "compare", Parent: "benchmark", Brief: "Compare results", Stage: 2,
				Needs: []store.Need{{NodeID: "benchmark", Kind: store.FeedsInto}}},
		}}, nil
	}

	if err := New(graph, compile, plan).Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	settled := commandBySeq(t, graph, command.Seq)
	if settled.Status != store.CommandApplied || settled.Result != "spliced 2 nodes" {
		t.Fatalf("settled command = %+v", settled)
	}
	for _, id := range []string{"benchmark", "compare"} {
		node, ok, err := graph.Node(id)
		if err != nil || !ok {
			t.Fatalf("node %q: ok=%v err=%v", id, ok, err)
		}
		if node.Provenance.Intent != instruction {
			t.Fatalf("node %q intent = %q, want verbatim %q", id, node.Provenance.Intent, instruction)
		}
		if node.Provenance.SessionID != "session-splice" || node.Provenance.Origin != store.OriginUser {
			t.Fatalf("node %q provenance = %+v", id, node.Provenance)
		}
	}

	receipt := commandReceipt(t, graph, "session-splice", command.Seq)
	wantLines := []string{
		"Here's my reading: Benchmark the parser and preserve observable output",
		"Assumed: main is the comparison baseline",
		"Assumed: the existing benchmark harness is sufficient",
		"Correct me anytime — redirects are cheap.",
	}
	for _, line := range wantLines {
		if !strings.Contains(receipt.Body, line) {
			t.Errorf("receipt %q does not contain %q", receipt.Body, line)
		}
	}
}

func TestTickUsesDefaultCompilerAndPlanner(t *testing.T) {
	graph := openStore(t)
	command, err := graph.RequestCommand(store.Command{
		SessionID:   "session-default",
		Kind:        store.CommandSplice,
		Instruction: "Keep this request verbatim",
	})
	if err != nil {
		t.Fatalf("request command: %v", err)
	}

	if err := New(graph, nil, nil).Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	id := fmt.Sprintf("task-%d", command.Seq)
	node, ok, err := graph.Node(id)
	if err != nil || !ok {
		t.Fatalf("default node %q: ok=%v err=%v", id, ok, err)
	}
	if node.Parent != store.RootID || node.Brief != command.Instruction || node.Stage != 1 {
		t.Fatalf("default node = %+v", node)
	}
	if node.Provenance.Intent != command.Instruction {
		t.Fatalf("intent = %q, want %q", node.Provenance.Intent, command.Instruction)
	}
	settled := commandBySeq(t, graph, command.Seq)
	if settled.Status != store.CommandApplied || settled.Result != "spliced 1 nodes" {
		t.Fatalf("settled command = %+v", settled)
	}
	receipt := commandReceipt(t, graph, command.SessionID, command.Seq)
	if !strings.Contains(receipt.Body, "Here's my reading: "+command.Instruction) || strings.Contains(receipt.Body, "Assumed:") {
		t.Fatalf("default receipt = %q", receipt.Body)
	}
}

func TestTickCancelsClaimableSubtreeAndReportsCounts(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "cancel-root", Brief: "Cancel this run", Stage: 1},
		{ID: "fetch", Parent: "cancel-root", Brief: "Fetch evidence", Stage: 2},
		{ID: "publish", Parent: "cancel-root", Brief: "Publish result", Stage: 3,
			Needs: []store.Need{{NodeID: "fetch", Kind: store.Blocks}}},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "session-cancel", Intent: "prepare a report"}); err != nil {
		t.Fatalf("splice fixture: %v", err)
	}
	command, err := graph.RequestCommand(store.Command{
		SessionID:   "session-cancel",
		Kind:        store.CommandCancel,
		Target:      "cancel-root",
		Instruction: "stop this work",
	})
	if err != nil {
		t.Fatalf("request cancel: %v", err)
	}

	if err := New(graph, nil, nil).Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	for _, id := range []string{"cancel-root", "fetch", "publish"} {
		node, ok, err := graph.Node(id)
		if err != nil || !ok {
			t.Fatalf("node %q: ok=%v err=%v", id, ok, err)
		}
		if node.Status != store.Failed || node.Error != "cancelled by resident request" {
			t.Errorf("cancelled node %q = status %s error %q", id, node.Status, node.Error)
		}
	}
	settled := commandBySeq(t, graph, command.Seq)
	const result = "cancelled 3 nodes, 0 in flight left to land"
	if settled.Status != store.CommandApplied || settled.Result != result {
		t.Fatalf("settled cancel = %+v", settled)
	}
	receipt := commandReceipt(t, graph, command.SessionID, command.Seq)
	if !strings.Contains(receipt.Body, result) {
		t.Fatalf("cancel receipt = %q", receipt.Body)
	}
}

func TestTickRejectsAmendAndPostsHonestReason(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "existing", Brief: "Existing work", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "session-amend", Intent: "do existing work"}); err != nil {
		t.Fatalf("splice fixture: %v", err)
	}
	command, err := graph.RequestCommand(store.Command{
		SessionID:   "session-amend",
		Kind:        store.CommandAmend,
		Target:      "existing",
		Instruction: "change the output format",
	})
	if err != nil {
		t.Fatalf("request amend: %v", err)
	}

	if err := New(graph, nil, nil).Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	const reason = "amend is not implemented yet; cancel and re-ask, or splice an addition"
	settled := commandBySeq(t, graph, command.Seq)
	if settled.Status != store.CommandRejected || settled.Result != reason {
		t.Fatalf("settled amend = %+v", settled)
	}
	receipt := commandReceipt(t, graph, command.SessionID, command.Seq)
	if receipt.Body != reason {
		t.Fatalf("amend receipt = %q, want %q", receipt.Body, reason)
	}
}

func TestCompletionAnnouncementIsDeduplicatedAcrossRestart(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "land", Brief: "Write the report\nwith an appendix", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "session-watch", Intent: "write a report"}); err != nil {
		t.Fatalf("splice fixture: %v", err)
	}

	first := New(graph, nil, nil)
	if err := first.Tick(context.Background()); err != nil {
		t.Fatalf("initialize watcher: %v", err)
	}
	claim, won, err := graph.Claim("land", "worker")
	if err != nil || !won {
		t.Fatalf("claim: won=%v err=%v", won, err)
	}
	if err := graph.Complete(claim, "Report is in cas://report\nsecondary detail"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if err := first.Tick(context.Background()); err != nil {
		t.Fatalf("announce completion: %v", err)
	}
	if err := first.Tick(context.Background()); err != nil {
		t.Fatalf("repeat tick: %v", err)
	}

	messages, err := graph.Messages("session-watch", 0, 0)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages after repeated tick = %+v", messages)
	}
	// A deliverable owner (parented on the spine root) posts its whole
	// summary: that summary is the answer the user asked for, and a one-line
	// notice was measured to hide the result entirely.
	if messages[0].NodeID != "land" || messages[0].CommandSeq != 0 ||
		messages[0].Body != "Report is in cas://report\nsecondary detail" {
		t.Fatalf("announcement = %+v", messages[0])
	}

	restarted := New(graph, nil, nil)
	if err := restarted.Tick(context.Background()); err != nil {
		t.Fatalf("restart tick: %v", err)
	}
	messages, err = graph.Messages("session-watch", 0, 0)
	if err != nil {
		t.Fatalf("messages after restart: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("restart re-announced history: %+v", messages)
	}
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := graph.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return graph
}

func commandBySeq(t *testing.T, graph *store.Store, seq int64) store.Command {
	t.Helper()
	command, ok, err := graph.CommandBySeq(seq)
	if err != nil || !ok {
		t.Fatalf("command %d: ok=%v err=%v", seq, ok, err)
	}
	return command
}

func commandReceipt(t *testing.T, graph *store.Store, sessionID string, commandSeq int64) store.Message {
	t.Helper()
	messages, err := graph.Messages(sessionID, 0, 0)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	for _, message := range messages {
		if message.CommandSeq == commandSeq {
			if message.Role != store.RoleSystem {
				t.Fatalf("command receipt role = %s", message.Role)
			}
			return message
		}
	}
	t.Fatalf("no receipt for command %d in %+v", commandSeq, messages)
	return store.Message{}
}

func TestTickReturnsCancelledContextWithoutTouchingCommand(t *testing.T) {
	graph := openStore(t)
	command, err := graph.RequestCommand(store.Command{Kind: store.CommandSplice, Instruction: "later"})
	if err != nil {
		t.Fatalf("request command: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := New(graph, nil, nil).Tick(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("tick error = %v, want context.Canceled", err)
	}
	if got := commandBySeq(t, graph, command.Seq).Status; got != store.CommandPending {
		t.Fatalf("command status = %s, want pending", got)
	}
}
