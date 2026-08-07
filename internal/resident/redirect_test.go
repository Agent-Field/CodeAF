package resident

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The whole loop in one pass: the remaining plan is revised, the leaf that is
// mid-turn hears the user's words, and the thread gets one honest line.
func TestUserRedirectRevisesInformsAndReceiptsOnce(t *testing.T) {
	graph := openStore(t)
	spliceRedirectJob(t, graph)
	startRedirectLeaf(t, graph, "api-n1")

	reconciler := New(graph, nil, nil).WithRedirector(
		func(_ context.Context, job store.Node, message string, flavor RevisionFlavor) (Redirection, error) {
			if job.ID != "api" || message != "no, use the v2 API not v1" || flavor != RevisionRedirect {
				t.Fatalf("redirector saw job=%s message=%q flavor=%s", job.ID, message, flavor)
			}
			return Redirection{Added: 1, Amended: 2, Notes: []string{"remove api-n7: node 7 does not exist"}}, nil
		})
	command, err := graph.RequestCommand(store.Command{
		SessionID: "redirect", Kind: store.CommandRedirect, Target: "api",
		Instruction: "no, use the v2 API not v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	receipt := commandReceipt(t, graph, "redirect", command.Seq)
	if receipt.NodeID != "api" ||
		!strings.Contains(receipt.Body, "redirected v1 API client — 2 steps amended, 1 added; 1 running worker informed") ||
		!strings.Contains(receipt.Body, "node 7 does not exist") {
		t.Fatalf("redirect receipt = %+v", receipt)
	}
	if lines := steerMailbox(t, graph, "api-n1"); len(lines) != 1 ||
		lines[0] != "redirection from the user: no, use the v2 API not v1" {
		t.Fatalf("steering mailbox = %+v", lines)
	}
	if lines := steerMailbox(t, graph, "api-n2"); len(lines) != 0 {
		t.Fatalf("a pending leaf was told mid-turn: %+v", lines)
	}
}

// Nothing to edit and nobody mid-turn is still an answer, never silence.
func TestUserRedirectThatChangesNothingStillSpeaks(t *testing.T) {
	graph := openStore(t)
	spliceRedirectJob(t, graph)
	command, err := graph.RequestCommand(store.Command{
		SessionID: "quiet", Kind: store.CommandRedirect, Target: "api",
		Instruction: "also keep the retry behaviour",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(graph, nil, nil).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	receipt := commandReceipt(t, graph, "quiet", command.Seq)
	if !strings.Contains(receipt.Body, "nothing in the remaining plan needed to change") {
		t.Fatalf("silent redirect receipt = %+v", receipt)
	}
}

// Removing work that already started is not an edit the store will make. It
// degrades to the cancel control — quietly for a cheap young leaf, and only
// with consent once there is real money in it.
func TestRedirectRemovalOfRunningWorkHonoursTheConsequenceGate(t *testing.T) {
	tests := []struct {
		name      string
		cost      float64
		cancelled bool
	}{
		{"cheap young leaf", store.SurgerySpendGateUSD, true},
		{"past the spend gate", store.SurgerySpendGateUSD + 0.6, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			graph := openStore(t)
			spliceRedirectJob(t, graph)
			startRedirectLeaf(t, graph, "api-n1")
			if err := graph.RecordUsage(store.NodeUsage{NodeID: "api-n1", Cost: test.cost}); err != nil {
				t.Fatal(err)
			}
			reconciler := New(graph, nil, nil).WithRedirector(
				func(context.Context, store.Node, string, RevisionFlavor) (Redirection, error) {
					return Redirection{RunningRemovals: []string{"api-n1"}}, nil
				})
			command, err := graph.RequestCommand(store.Command{
				SessionID: "cut", Kind: store.CommandRedirect, Target: "api",
				Instruction: "don't bother with the v1 fallback",
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := reconciler.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			leaf, _, err := graph.Node("api-n1")
			if err != nil {
				t.Fatal(err)
			}
			if leaf.CancelRequested != test.cancelled {
				t.Fatalf("cancel requested = %t, want %t", leaf.CancelRequested, test.cancelled)
			}
			receipt := commandReceipt(t, graph, "cut", command.Seq)
			if strings.Contains(receipt.Body, "1 dropped") != test.cancelled {
				t.Fatalf("receipt = %q", receipt.Body)
			}
			questions, err := graph.UnresolvedQuestions(10)
			if err != nil {
				t.Fatal(err)
			}
			if test.cancelled {
				if len(questions) != 0 {
					t.Fatalf("cheap cancellation asked anyway: %+v", questions)
				}
				return
			}
			if len(questions) != 1 || questions[0].Category != store.QuestionCategorySurgeryConfirm ||
				questions[0].DefaultAnswer != "2" || !strings.Contains(questions[0].Text, "~$0.85 spent") {
				t.Fatalf("gate question = %+v", questions)
			}
			action, target, _, ok := store.DecodeRedirectOption(questions[0].Options[0].Value)
			if !ok || action != "cancel" || target != "api-n1" {
				t.Fatalf("gate option = %+v", questions[0].Options[0])
			}
		})
	}
}

// The broadcast is AttachAmendment's move made plural, and it must land where
// the executor's steering mailbox actually reads.
func TestBroadcastRedirectionReachesEveryRunningLeaf(t *testing.T) {
	graph := openStore(t)
	spliceRedirectJob(t, graph)
	startRedirectLeaf(t, graph, "api-n1")
	startRedirectLeaf(t, graph, "api-n2")

	informed, err := BroadcastRedirection(graph, "api", "steer", "focus on the v2 API instead")
	if err != nil || informed != 2 {
		t.Fatalf("informed = %d err=%v", informed, err)
	}
	for _, id := range []string{"api-n1", "api-n2"} {
		lines := steerMailbox(t, graph, id)
		if len(lines) != 1 || !strings.HasSuffix(lines[0], "focus on the v2 API instead") {
			t.Fatalf("%s mailbox = %+v", id, lines)
		}
	}
}

func TestUserRevisionEventSpeaksWithTheOwnersAuthority(t *testing.T) {
	event := UserRevisionEvent("  drop the docs part  ", RevisionRedirect)
	if !strings.Contains(event, "drop the docs part") ||
		!strings.Contains(event, "owner of the work") ||
		!strings.Contains(event, "never re-add work they cut") {
		t.Fatalf("user revision event = %q", event)
	}
}

// Impatience must buy something real: claim order ahead of the queue, the
// words in every worker's transcript, and a shorter tail. Nothing here compiles.
func TestExpediteMovesInformsAndTrims(t *testing.T) {
	graph := openStore(t)
	spliceRedirectJob(t, graph)
	// A second queued job, so "moved it to the front" has a front to move to.
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "docs", Title: "the docs", Brief: "write the docs", Stage: 2},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "hurry", Intent: "write the docs"}); err != nil {
		t.Fatal(err)
	}
	startRedirectLeaf(t, graph, "api-n1")

	reconciler := New(graph, nil, nil).WithRedirector(
		func(_ context.Context, job store.Node, message string, flavor RevisionFlavor) (Redirection, error) {
			if job.ID != "api" || flavor != RevisionExpedite || message != urgencyRevisionInstruction {
				t.Fatalf("redirector saw job=%s flavor=%s message=%q", job.ID, flavor, message)
			}
			return Redirection{Dropped: 1}, nil
		})
	command, err := graph.RequestCommand(store.Command{
		SessionID: "hurry", Kind: store.CommandExpedite, Target: "api",
		Instruction: "please complete the dinance research fast and give me result immediatly",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	job, _, err := graph.Node("api")
	if err != nil {
		t.Fatal(err)
	}
	if job.Priority <= 0 {
		t.Fatalf("expedited job priority = %d", job.Priority)
	}
	if lines := steerMailbox(t, graph, "api-n1"); len(lines) != 1 ||
		lines[0] != redirectSteerPrefix+urgencySteerLine {
		t.Fatalf("steering mailbox = %+v", lines)
	}
	if lines := steerMailbox(t, graph, "api-n2"); len(lines) != 0 {
		t.Fatalf("a pending leaf was told mid-turn: %+v", lines)
	}
	receipt := commandReceipt(t, graph, "hurry", command.Seq)
	if receipt.NodeID != "api" || !strings.Contains(receipt.Body,
		"understood — v1 API client: moved it to the front of the queue, "+
			"told its 1 running worker to cut to the essentials, dropped 1 remaining step") {
		t.Fatalf("expedite receipt = %+v", receipt)
	}
	if commands, err := graph.PendingCommands(10); err != nil || len(commands) != 0 {
		t.Fatalf("expedite queued more work: %+v err=%v", commands, err)
	}
}

// Urgency with nothing to spend it on says exactly that. A promise of speed
// with no mechanism behind it is the failure this path exists to end.
func TestExpediteWithNothingToAccelerateSaysSo(t *testing.T) {
	graph := openStore(t)
	spliceRedirectJob(t, graph)
	for _, id := range []string{"api-n1", "api-n2"} {
		if err := graph.CancelPending(id, "settled before the user asked"); err != nil {
			t.Fatal(err)
		}
	}
	reconciler := New(graph, nil, nil).WithRedirector(
		func(context.Context, store.Node, string, RevisionFlavor) (Redirection, error) {
			t.Fatal("a job with no unstarted tail should not spend a model call")
			return Redirection{}, nil
		})
	command, err := graph.RequestCommand(store.Command{
		SessionID: "nothing", Kind: store.CommandExpedite, Target: "api",
		Instruction: "hurry up with that job",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	receipt := commandReceipt(t, graph, "nothing", command.Seq)
	if !strings.Contains(receipt.Body, "there is nothing here left to accelerate") ||
		strings.Contains(receipt.Body, "still to run") {
		t.Fatalf("nothing-to-accelerate receipt = %+v", receipt)
	}
}

func TestUrgencyRevisionEventLicensesTheTrimAndNothingElse(t *testing.T) {
	event := UserRevisionEvent(urgencyRevisionInstruction, RevisionExpedite)
	if !strings.Contains(event, "out of patience") ||
		!strings.Contains(event, "shortest path to the core deliverable") ||
		!strings.Contains(event, "Never add work") {
		t.Fatalf("urgency revision event = %q", event)
	}
}

func spliceRedirectJob(t *testing.T, graph *store.Store) {
	t.Helper()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "api", Title: "v1 API client", Brief: "write a client for the v1 API", Stage: 2},
		{ID: "api-n1", Parent: "api", Title: "Endpoints", Brief: "map the v1 endpoints", Stage: 1},
		{ID: "api-n2", Parent: "api", Title: "Auth", Brief: "wire up auth", Stage: 1},
	}}, store.Provenance{
		Origin: store.OriginUser, SessionID: "redirect", Intent: "write a client for the v1 API",
	}); err != nil {
		t.Fatal(err)
	}
}

func startRedirectLeaf(t *testing.T, graph *store.Store, id string) {
	t.Helper()
	claim, won, err := graph.Claim(id, "worker-"+id)
	if err != nil || !won {
		t.Fatalf("claim %s won=%t err=%v", id, won, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
}

// steerMailbox is the chat executor's steering closure, reproduced: the lines
// a worker would receive before its next turn.
func steerMailbox(t *testing.T, graph *store.Store, id string) []string {
	t.Helper()
	messages, err := graph.NodeMessages(id, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, message := range messages {
		if message.Role == store.RoleUser {
			lines = append(lines, message.Body)
		}
	}
	return lines
}

// The plan pass is a model call and may fail. The words still have to reach
// the workers, and the receipt has to admit which half happened.
func TestRedirectStillInformsWorkersWhenTheRevisionFails(t *testing.T) {
	graph := openStore(t)
	spliceRedirectJob(t, graph)
	startRedirectLeaf(t, graph, "api-n1")
	reconciler := New(graph, nil, nil).WithRedirector(
		func(context.Context, store.Node, string, RevisionFlavor) (Redirection, error) {
			return Redirection{}, errors.New("provider unavailable")
		})
	command, err := graph.RequestCommand(store.Command{
		SessionID: "broken", Kind: store.CommandRedirect, Target: "api",
		Instruction: "focus on the v2 API instead",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	receipt := commandReceipt(t, graph, "broken", command.Seq)
	if !strings.Contains(receipt.Body, "the remaining plan is unchanged; 1 running worker informed") ||
		!strings.Contains(receipt.Body, "provider unavailable") {
		t.Fatalf("failed-revision receipt = %+v", receipt)
	}
	if lines := steerMailbox(t, graph, "api-n1"); len(lines) != 1 {
		t.Fatalf("steering lost to a failed revision: %+v", lines)
	}
}
