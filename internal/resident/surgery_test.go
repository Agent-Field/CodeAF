package resident

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestPauseResumeAndReprioritizeApplyJournaledSchedulerState(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "release", Brief: "ship release", Title: "Release", Stage: 2},
		{ID: "docs", Parent: "release", Brief: "write docs", Title: "Docs", Stage: 1},
		{ID: "tests", Parent: "release", Brief: "run tests", Title: "Tests", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "surgery", Intent: "ship release"}); err != nil {
		t.Fatal(err)
	}
	reconciler := New(graph, nil, nil)
	pause, err := graph.RequestCommand(store.Command{
		SessionID: "surgery", Kind: store.CommandPause, Target: "release", Instruction: "pause while I think",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"release", "docs", "tests"} {
		node, _, _ := graph.Node(id)
		if !node.Held || node.Status != store.Pending {
			t.Fatalf("paused %s = %+v", id, node)
		}
	}
	pauseReceipt := commandReceipt(t, graph, "surgery", pause.Seq)
	if pauseReceipt.NodeID != "release" || !strings.Contains(pauseReceipt.Body, "paused — 3 nodes held") {
		t.Fatalf("pause receipt = %+v", pauseReceipt)
	}

	resume, err := graph.RequestCommand(store.Command{
		SessionID: "surgery", Kind: store.CommandResume, Target: "release", Instruction: "resume it",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"release", "docs", "tests"} {
		node, _, _ := graph.Node(id)
		if node.Held {
			t.Fatalf("resumed %s remained held", id)
		}
	}
	if receipt := commandReceipt(t, graph, "surgery", resume.Seq); receipt.NodeID != "release" {
		t.Fatalf("resume receipt anchor = %+v", receipt)
	}

	priority, err := graph.RequestCommand(store.Command{
		SessionID: "surgery", Kind: store.CommandReprioritize, Target: "tests", Instruction: "do the tests first",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	testsNode, _, _ := graph.Node("tests")
	if testsNode.Priority <= 0 {
		t.Fatalf("tests priority = %d", testsNode.Priority)
	}
	if receipt := commandReceipt(t, graph, "surgery", priority.Seq); receipt.NodeID != "tests" ||
		!strings.Contains(receipt.Body, "before its pending siblings") {
		t.Fatalf("priority receipt = %+v", receipt)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	testsNode, _, _ = graph.Node("tests")
	if testsNode.Priority <= 0 || testsNode.Held {
		t.Fatalf("rebuilt tests state = %+v", testsNode)
	}
}

func TestRunningAmendmentIsDeliveredAtNextTurnAndReceiptSaysSo(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "readme", Brief: "update README", Title: "README", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, SessionID: "amend", Intent: "update README"}); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("readme", "worker")
	if err != nil || !won {
		t.Fatalf("claim won=%t err=%v", won, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	command, err := graph.RequestCommand(store.Command{
		SessionID: "amend", Kind: store.CommandAmend, Target: "readme",
		Instruction: "also cover the changelog",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(graph, nil, nil).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	receipt := commandReceipt(t, graph, "amend", command.Seq)
	if receipt.NodeID != "readme" || !strings.Contains(receipt.Body, "applies at the next turn") {
		t.Fatalf("running amend receipt = %+v", receipt)
	}
	messages, err := graph.NodeMessages("readme", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	foundGuidance := false
	for _, message := range messages {
		foundGuidance = foundGuidance || message.Role == store.RoleUser && message.Body == "also cover the changelog"
	}
	if !foundGuidance {
		t.Fatalf("running amendment missing steering message: %+v", messages)
	}
}

func TestRestartRespliceIsFreshAndLinksFailedPredecessor(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "failed-audio", Brief: "render audio", Title: "Audio render", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, SessionID: "restart", Intent: "render audio"}); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("failed-audio", "worker")
	if err != nil || !won {
		t.Fatalf("claim won=%t err=%v", won, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if err := graph.Fail(claim, "encoder crashed"); err != nil {
		t.Fatal(err)
	}
	command, err := graph.RequestCommand(store.Command{
		SessionID: "restart", Kind: store.CommandRestart, Target: "failed-audio", Instruction: "restart the failed one",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(graph, nil, nil).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	retryID := "retry-" + fmt.Sprint(command.Seq) + "-1"
	retry, found, err := graph.Node(retryID)
	if err != nil || !found || retry.Status != store.Pending || retry.Provenance.RetryOf != "failed-audio" ||
		retry.Provenance.Intent != "render audio" {
		t.Fatalf("retry = %+v found=%t err=%v", retry, found, err)
	}
	receipt := commandReceipt(t, graph, "restart", command.Seq)
	if receipt.NodeID != "failed-audio" || !strings.Contains(receipt.Body, "fresh work linked") {
		t.Fatalf("restart receipt = %+v", receipt)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	retry, found, err = graph.Node(retryID)
	if err != nil || !found || retry.Provenance.RetryOf != "failed-audio" {
		t.Fatalf("rebuilt retry = %+v found=%t err=%v", retry, found, err)
	}
}

// failedJobToRestart is the setup every restart-arm test shares.
func failedJobToRestart(t *testing.T, graph *store.Store) {
	t.Helper()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "weak-draft", Brief: "draft the essay", Title: "Essay draft", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, SessionID: "restart", Intent: "draft the essay"}); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("weak-draft", "worker")
	if err != nil || !won {
		t.Fatalf("claim won=%t err=%v", won, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if err := graph.Fail(claim, "thin"); err != nil {
		t.Fatal(err)
	}
}

func TestRestartCarriesTheModelTheAskNamed(t *testing.T) {
	graph := openStore(t)
	failedJobToRestart(t, graph)
	command, err := graph.RequestCommand(store.Command{
		SessionID: "restart", Kind: store.CommandRestart, Target: "weak-draft",
		Instruction: "rerun that with the better model\n\n" + RestartModelMarker + " " + RestartBoostModel,
	})
	if err != nil {
		t.Fatal(err)
	}
	var asked []string
	var boosted bool
	reconciler := New(graph, nil, nil).WithModelResolver(
		func(names []string, boost bool) (string, bool) {
			asked, boosted = names, boost
			return "anthropic/claude-opus", true
		})
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 0 || !boosted {
		t.Fatalf("the boost slot was not read back: names=%v boost=%t", asked, boosted)
	}
	retry, found, err := graph.Node("retry-" + fmt.Sprint(command.Seq) + "-1")
	if err != nil || !found {
		t.Fatalf("retry: found=%t err=%v", found, err)
	}
	if retry.Provenance.WorkModel != "anthropic/claude-opus" {
		t.Fatalf("the restart ran on the default anyway: %+v", retry.Provenance)
	}
	receipt := commandReceipt(t, graph, "restart", command.Seq)
	if !strings.Contains(receipt.Body, "on anthropic/claude-opus") {
		t.Fatalf("restart receipt did not state the outcome: %q", receipt.Body)
	}
}

func TestRestartSaysSoWhenTheModelItWasAskedForIsNotThere(t *testing.T) {
	graph := openStore(t)
	failedJobToRestart(t, graph)
	command, err := graph.RequestCommand(store.Command{
		SessionID: "restart", Kind: store.CommandRestart, Target: "weak-draft",
		Instruction: "rerun that on gpt-9\n\n" + RestartModelMarker + " gpt-9",
	})
	if err != nil {
		t.Fatal(err)
	}
	reconciler := New(graph, nil, nil).WithModelResolver(
		func([]string, bool) (string, bool) { return "", false })
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	retry, found, err := graph.Node("retry-" + fmt.Sprint(command.Seq) + "-1")
	if err != nil || !found || retry.Provenance.WorkModel != "" {
		t.Fatalf("an unresolvable name reached the executor: %+v found=%t err=%v", retry, found, err)
	}
	receipt := commandReceipt(t, graph, "restart", command.Seq)
	if !strings.Contains(receipt.Body, "I don't have gpt-9") {
		t.Fatalf("the restart quietly used the default: %q", receipt.Body)
	}
}
