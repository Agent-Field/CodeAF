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

// A restart used to be a clean slate in the worst sense: a fresh worker in a
// fresh directory, told nothing about the attempt it replaced, and stripped of
// the model the user had pinned — so pinning a strong model, watching it fail
// and saying "try again" handed the work back to the cheap one.
func TestRestartInheritsWhatItsPredecessorKnew(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "failed-audit", Brief: "audit the service", Title: "Audit", Stage: 1,
	}}}, store.Provenance{
		Origin: store.OriginUser, SessionID: "restart", Intent: "audit the service",
		WorkModel: "strong/two", Attachments: []string{"/docs/contract.pdf"},
	}); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("failed-audit", "worker")
	if err != nil || !won {
		t.Fatalf("claim won=%t err=%v", won, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	// The failure names the partial it left behind, which is how the retry can
	// reach files nobody else knows about.
	if err := graph.Fail(claim, "it stopped early\n\nFiles it left behind:\n/w/job/01-notes.md"); err != nil {
		t.Fatal(err)
	}
	command, err := graph.RequestCommand(store.Command{
		SessionID: "restart", Kind: store.CommandRestart, Target: "failed-audit",
		Instruction: "try again",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(graph, nil, nil).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	retryID := "retry-" + fmt.Sprint(command.Seq) + "-1"
	retry, found, err := graph.Node(retryID)
	if err != nil || !found {
		t.Fatalf("retry found=%t err=%v", found, err)
	}
	if retry.Provenance.WorkModel != "strong/two" {
		t.Fatalf("the pinned model was dropped on the retry: %+v", retry.Provenance)
	}
	if len(retry.Provenance.Attachments) != 1 {
		t.Fatalf("attachments = %v", retry.Provenance.Attachments)
	}
	// The predecessor is wired in, so the fresh worker starts from what the
	// first attempt actually produced rather than from the brief again.
	inputs, err := graph.DependencyInputs(retryID, store.MaxDigestBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 1 || inputs[0].NodeID != "failed-audit" {
		t.Fatalf("inputs = %+v; the retry cannot read its predecessor", inputs)
	}
	if !strings.Contains(inputs[0].Digest, "it stopped early") {
		t.Fatalf("digest = %q", inputs[0].Digest)
	}
	if len(inputs[0].Artifacts) != 1 || inputs[0].Artifacts[0] != "/w/job/01-notes.md" {
		t.Fatalf("artifacts = %v; the partial is unreachable again", inputs[0].Artifacts)
	}
	// And it is still claimable: a settled predecessor is a satisfied
	// dependency, not a wedge.
	ready, err := graph.Ready(0)
	if err != nil {
		t.Fatal(err)
	}
	claimable := false
	for _, node := range ready {
		claimable = claimable || node.ID == retryID
	}
	if !claimable {
		t.Fatal("the retry is not ready — the predecessor edge wedged it")
	}
}
