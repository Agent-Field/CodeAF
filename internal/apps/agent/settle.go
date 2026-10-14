package agent

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// ApprovalSettled hears how an approval this agent opened ended. The dock
// gets the final approval, then the outcome goes into the thread as the
// tool's result and the agent takes a turn to tell the person.
func (s *Service) ApprovalSettled(scope svc.Scope, item bridge.Approval, output any) {
	go s.resume(scope, item, output)
}

func (s *Service) resume(scope svc.Scope, item bridge.Approval, output any) {
	t := &turn{svc: s, scope: scope, thread: item.Thread, id: randomID("u")}
	t.emit(bridge.AgentEvent{Kind: bridge.AgentApproval, Approval: &item})
	bundle, err := s.all.Store.Definition(s.ctx, scope.App)
	if err == nil && (bundle == nil || bundle.App == nil || bundle.Agent == nil) {
		err = bridge.Errorf(bridge.CodeConflict, "the app has no agent yet")
	}
	if err != nil {
		t.fail(err)
		return
	}
	t.bundle = bundle
	t.converse(settledText(item, output))
}

// settledText is the outcome as the model reads it, in the same shape as any
// other tool result so it can answer in one line.
func settledText(item bridge.Approval, output any) string {
	asked := " (approval " + item.ID + ")"
	switch item.State {
	case bridge.ApprovalDone:
		raw, _ := json.Marshal(output)
		payload, _ := json.Marshal(item.Payload)
		return resultText(item.Tool, string(raw)) + "\nThe person approved" + asked + " and it ran with " + string(payload) +
			". Tell them in one short sentence what was done."
	case bridge.ApprovalFailed:
		return "Result of " + item.Tool + ":\n" + `{"error":` + quote(item.Error) + "}\nThe person approved" + asked +
			" but it failed and did not complete. Tell them plainly; do not retry unless they ask."
	}
	return "The person skipped this" + asked + ": " + item.Title + ". " + item.Tool +
		" did not run. Acknowledge it briefly; do not retry unless they ask."
}

func quote(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}

// inflight holds the cancel of each thread's running turn, for agent.stop.
type inflight struct {
	mu     sync.Mutex
	cancel map[string]context.CancelFunc
}

func newInflight() *inflight { return &inflight{cancel: map[string]context.CancelFunc{}} }

// begin gives a turn its context. Turns on one thread are already serialised,
// so at most one cancel is held per thread.
func (f *inflight) begin(parent context.Context, app, thread string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	key := app + "/" + thread
	f.mu.Lock()
	f.cancel[key] = cancel
	f.mu.Unlock()
	return ctx, func() {
		f.mu.Lock()
		delete(f.cancel, key)
		f.mu.Unlock()
		cancel()
	}
}

// stop cancels the thread's running turn, if any. Stopping an idle thread is
// not an error: the person's Stop and the turn's end can cross.
func (f *inflight) stop(app, thread string) {
	f.mu.Lock()
	cancel := f.cancel[app+"/"+thread]
	f.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
