package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

var _ runs.ApprovalListener = (*Service)(nil)

// collect gathers the thread's events until a turn ends.
func (r *rig) collect(t *testing.T, start func()) []bridge.AgentEvent {
	t.Helper()
	events, cancel := r.svc.all.Hub.Subscribe("app/crm/agent/*")
	defer cancel()
	start()
	var got []bridge.AgentEvent
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev := <-events:
			e := ev.Data.(bridge.AgentEvent)
			got = append(got, e)
			if e.Kind == bridge.AgentDone || e.Kind == bridge.AgentError {
				return got
			}
		case <-timeout:
			t.Fatalf("turn did not finish: %+v", got)
		}
	}
}

func TestADecidedApprovalResumesTheThreadThatAsked(t *testing.T) {
	tests := []struct {
		name   string
		item   bridge.Approval
		output any
		told   string
	}{
		{name: "done", item: bridge.Approval{State: bridge.ApprovalDone, Result: "Ran gmail send (msg-1)", Payload: map[string]any{"to": "ada@example.com"}},
			output: map[string]any{"id": "msg-1"}, told: `Result of gmail.send:` + "\n" + `{"id":"msg-1"}`},
		{name: "failed", item: bridge.Approval{State: bridge.ApprovalFailed, Error: "gmail is not connected"},
			told: `{"error":"gmail is not connected"}`},
		{name: "skipped", item: bridge.Approval{State: bridge.ApprovalRejected}, told: "The person skipped this"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, `{"tool":"gmail.send","args":{"to":"ada@example.com"}}`, `{"say":"Waiting for you."}`, `{"say":"Told."}`)
			asked := r.ask(t, "email Ada")
			opened := r.approvals.opened[0]
			if opened.Thread == "" {
				t.Fatalf("the approval must name its thread: %+v", opened)
			}
			item := tt.item
			item.ID, item.Thread, item.Tool, item.Title = opened.ID, opened.Thread, opened.Tool, opened.Title
			events := r.collect(t, func() { r.svc.ApprovalSettled(scope, item, tt.output) })
			if got := strings.Join(toStrings(kinds(events)), ","); got != "approval,delta,done" {
				t.Fatalf("events = %s", got)
			}
			if events[0].Approval.State != tt.item.State || events[0].Turn != events[2].Turn || events[0].Turn == asked[0].Turn {
				t.Fatalf("dock event = %+v", events[0])
			}
			last := r.model.calls[len(r.model.calls)-1].Messages
			if told := last[len(last)-1].Text; !strings.Contains(told, tt.told) || !strings.Contains(told, opened.ID) {
				t.Fatalf("the agent was told %q", told)
			}
			if len(r.conns.called) != 0 {
				t.Fatalf("the agent must not run the action itself: %v", r.conns.called)
			}
		})
	}
}

// blockingModel waits until its call is cancelled.
type blockingModel struct{ started chan struct{} }

func (m *blockingModel) Complete(ctx context.Context, _ svc.ModelCall) (svc.ModelReply, error) {
	close(m.started)
	<-ctx.Done()
	return svc.ModelReply{}, ctx.Err()
}

func TestStopEndsTheTurnInFlight(t *testing.T) {
	r := newRig(t)
	model := &blockingModel{started: make(chan struct{})}
	r.svc.all.Model = model
	var thread string
	events := r.collect(t, func() {
		res, err := r.svc.Send(context.Background(), scope, bridge.AgentSendParams{Text: "think hard"})
		if err != nil {
			t.Fatal(err)
		}
		thread = res.Thread
		<-model.started
		r.svc.Stop(scope, thread)
	})
	if got := strings.Join(toStrings(kinds(events)), ","); got != "delta,done" || events[0].Text != "Stopped." {
		t.Fatalf("events = %+v", events)
	}
	r.svc.Stop(scope, thread)
}
