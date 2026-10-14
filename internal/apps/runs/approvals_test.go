package runs

import (
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// heardAgent stands in for the app agent and keeps what it was told.
type heardAgent struct {
	svc.Agent
	mu    sync.Mutex
	heard []bridge.Approval
}

func (a *heardAgent) ApprovalSettled(_ svc.Scope, item bridge.Approval, _ any) {
	a.mu.Lock()
	a.heard = append(a.heard, item)
	a.mu.Unlock()
}

func (h *harness) openAgentApproval(t *testing.T, tool string) bridge.Approval {
	t.Helper()
	item, err := h.inbox.Open(h.ctx, h.scope(), bridge.Approval{
		Kind: "send", Title: "Approval needed: " + tool, Tool: tool, Thread: "t1",
		Payload: map[string]any{"to": "priya@example.com", "subject": "Hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func (h *harness) callsTo(tool string) []recorded {
	h.eng.mu.Lock()
	defer h.eng.mu.Unlock()
	var out []recorded
	for _, call := range h.eng.calls {
		if call.Tool == tool {
			out = append(out, call)
		}
	}
	return out
}

func TestADecidedAgentApprovalRunsOnceAndTellsTheThread(t *testing.T) {
	tests := []struct {
		name    string
		tool    string
		answer  string
		edit    map[string]any
		state   bridge.ApprovalState
		calls   int
		result  string
		failure string
		to      string
	}{
		{name: "approve runs it", tool: "gmail.send", answer: "yes", state: bridge.ApprovalDone, calls: 1, result: "Ran gmail send (msg-fake)", to: "priya@example.com"},
		{name: "an edit runs as edited", tool: "gmail.send", answer: "yes", edit: map[string]any{"to": "ada@example.com"}, state: bridge.ApprovalDone, calls: 1, result: "Ran gmail send (msg-fake)", to: "ada@example.com"},
		{name: "a refused call fails with its reason", tool: "slack.post", answer: "yes", state: bridge.ApprovalFailed, failure: "the app does not allow slack.post"},
		{name: "skip never runs", tool: "gmail.send", answer: "no", state: bridge.ApprovalRejected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			agent := &heardAgent{}
			h.all.Agent = agent
			item := h.openAgentApproval(t, tt.tool)
			got, err := h.inbox.Decide(h.ctx, h.scope(), bridge.DecideApprovalParams{ApprovalID: item.ID, Answer: tt.answer, Edit: tt.edit})
			if err != nil {
				t.Fatal(err)
			}
			if got.State != tt.state || got.Result != tt.result || got.Error != tt.failure {
				t.Fatalf("decided = %+v", got)
			}
			calls := h.callsTo(tt.tool)
			if len(calls) != tt.calls || (tt.calls > 0 && calls[0].Args["to"] != tt.to) {
				t.Fatalf("calls = %+v", calls)
			}
			if len(agent.heard) != 1 || agent.heard[0].State != tt.state || agent.heard[0].Thread != "t1" {
				t.Fatalf("agent heard %+v", agent.heard)
			}
			all, _ := h.inbox.List(h.ctx, h.scope(), bridge.ListApprovalsParams{State: tt.state})
			if len(all.Approvals) != 1 {
				t.Fatalf("stored %s = %+v", tt.state, all.Approvals)
			}
		})
	}
}

func TestADoubleApproveRunsTheActionOnce(t *testing.T) {
	h := newHarness(t)
	h.all.Agent = &heardAgent{}
	item := h.openAgentApproval(t, "gmail.send")
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = h.inbox.Decide(h.ctx, h.scope(), bridge.DecideApprovalParams{ApprovalID: item.ID, Answer: "yes"})
		}()
	}
	wg.Wait()
	refused := 0
	for _, err := range errs {
		if typed, ok := err.(*bridge.Error); ok && typed.Code == bridge.CodeConflict {
			refused++
		}
	}
	if refused != len(errs)-1 || len(h.callsTo("gmail.send")) != 1 {
		t.Fatalf("errs = %v, calls = %d", errs, len(h.callsTo("gmail.send")))
	}
}
