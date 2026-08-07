package resident

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	executor "github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

type keepServiceCompleter struct{ calls atomic.Int32 }

func (completer *keepServiceCompleter) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	call := completer.calls.Add(1)
	message := ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "working"}}}
	switch call {
	case 1:
		message.ToolCalls = []ai.ToolCall{{ID: "start", Type: "function", Function: ai.ToolCallFunction{
			Name: "sh", Arguments: `{"cmd":"sleep 30","bg":true}`}}}
	case 2:
		message.ToolCalls = []ai.ToolCall{{ID: "keep", Type: "function", Function: ai.ToolCallFunction{
			Name: "job", Arguments: `{"id":1,"keep":{"name":"dev-server","health":"port:5173"}}`}}}
	default:
		message.Content = []ai.ContentPart{{Type: "text", Text: "preview is ready"}}
	}
	return &ai.Response{Choices: []ai.Choice{{Message: message, FinishReason: "stop"}},
		Usage: &ai.Usage{PromptTokens: 1, CompletionTokens: 1}}, nil
}

func promotionRunner(t *testing.T, serviceIntent bool, grace time.Duration) (*store.Store, *Runner) {
	t.Helper()
	graph := residentTestStore(t, "promotion.db")
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{ID: "leaf", Brief: "run app", Stage: 1}}},
		store.Provenance{Origin: store.OriginUser, SessionID: "promotion", Intent: "run the app", ServiceIntent: serviceIntent}); err != nil {
		t.Fatal(err)
	}
	workspace, err := executor.NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	linear := executor.NewLinear(&keepServiceCompleter{}, workspace, nil, 5, 100_000, time.Minute)
	runner := NewRunner(graph, func(ctx context.Context, node store.Node) (ExecResult, error) {
		outcome, err := linear.Run(ctx, executor.Task{
			NodeID: int(node.CreatedSeq), StoreNodeID: node.ID, Goal: node.Provenance.Intent, Brief: node.Brief,
		})
		if err != nil {
			return ExecResult{}, err
		}
		return ExecResult{Summary: outcome.Text, ServiceRequests: outcome.ServiceRequests}, nil
	}, "promotion-test", 1).WithServiceConsentGrace(grace)
	return graph, runner
}

func TestServiceIntentAutoPromotesAndDeclaresReceipt(t *testing.T) {
	graph, runner := promotionRunner(t, true, 50*time.Millisecond)
	if dispatched, err := runner.Tick(context.Background()); err != nil || dispatched != 1 {
		t.Fatalf("tick dispatched=%d err=%v", dispatched, err)
	}
	runner.Wait()
	services, err := graph.ActiveServices()
	if err != nil || len(services) != 1 {
		t.Fatalf("services=%+v err=%v", services, err)
	}
	t.Cleanup(func() { _ = NewServiceSupervisor(graph).Stop(services[0].ID, "test cleanup") })
	node, _, _ := graph.Node("leaf")
	if node.Status != store.Done || !strings.Contains(node.Summary,
		"dev-server keeps running · :5173 — say 'stop the dev-server' to end it") {
		t.Fatalf("promoted node = %+v", node)
	}
	questions, _ := graph.UnresolvedQuestions(10)
	if len(questions) != 0 {
		t.Fatalf("service-intent asked for redundant consent: %+v", questions)
	}
}

func TestServiceConfirmAnsweredKeepPromotesWithinGrace(t *testing.T) {
	graph, runner := promotionRunner(t, false, 3*time.Second)
	answered := make(chan struct{})
	go func() {
		defer close(answered)
		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
			questions, _ := graph.UnresolvedQuestions(10)
			for _, question := range questions {
				if strings.Contains(question.Text, "Keep dev-server") {
					_ = graph.ResolveQuestion(question.Seq, store.QuestionAnswered, "keep it running")
					return
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	if dispatched, err := runner.Tick(context.Background()); err != nil || dispatched != 1 {
		t.Fatalf("tick dispatched=%d err=%v", dispatched, err)
	}
	runner.Wait()
	<-answered
	services, err := graph.ActiveServices()
	if err != nil || len(services) != 1 || services[0].Name != "dev-server" {
		t.Fatalf("consented services=%+v err=%v", services, err)
	}
	t.Cleanup(func() { _ = NewServiceSupervisor(graph).Stop(services[0].ID, "test cleanup") })
	if services[0].AutoRestart {
		t.Fatal("plain keep opted into auto-restart")
	}
	node, _, _ := graph.Node("leaf")
	if !strings.Contains(node.Summary, "dev-server keeps running · :5173 — say 'stop the dev-server' to end it") {
		t.Fatalf("consented node summary = %q", node.Summary)
	}
}

func TestServiceConfirmDefaultsToStopAtGraceExpiry(t *testing.T) {
	graph, runner := promotionRunner(t, false, 40*time.Millisecond)
	if dispatched, err := runner.Tick(context.Background()); err != nil || dispatched != 1 {
		t.Fatalf("tick dispatched=%d err=%v", dispatched, err)
	}
	runner.Wait()
	services, _ := graph.ActiveServices()
	if len(services) != 0 {
		t.Fatalf("unanswered confirmation kept services: %+v", services)
	}
	questions, _ := graph.UnresolvedQuestions(10)
	if len(questions) != 0 {
		t.Fatalf("expired question remained unresolved: %+v", questions)
	}
	messages, _ := graph.Messages("promotion", 0, 20)
	var confirm string
	for _, message := range messages {
		if message.Role == store.RoleAgent && strings.Contains(message.Body, "Keep dev-server") {
			confirm = message.Body
		}
	}
	if !strings.Contains(confirm, `"default":"2"`) || !strings.Contains(confirm, "stop at task end") {
		t.Fatalf("confirm question = %q", confirm)
	}
	node, _, _ := graph.Node("leaf")
	if strings.Contains(node.Summary, "keeps running") {
		t.Fatalf("default-stop summary = %q", node.Summary)
	}
}
