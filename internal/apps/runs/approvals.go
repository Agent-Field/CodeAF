package runs

import (
	"context"
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Approvals is the inbox of "Approval needed" items. The session package
// internal/approval decides tool consent for chat; it has no inbox. The desk
// reads this store from app/<id>/approvals.
type Approvals struct {
	eng *engine
}

var _ svc.Approvals = (*Approvals)(nil)

// NewApprovals returns the inbox backed by the same journal as the runs engine.
func NewApprovals(cfg svc.Config, all *svc.Services) *Approvals {
	return &Approvals{eng: shared(cfg, all)}
}

// Open records an approval and publishes it.
func (a *Approvals) Open(ctx context.Context, scope svc.Scope, item bridge.Approval) (bridge.Approval, error) {
	if item.Title == "" {
		return bridge.Approval{}, bridge.Errorf(bridge.CodeInvalid, "title is required")
	}
	if item.ID == "" {
		item.ID = a.eng.newID("appr_")
	}
	if item.App == "" {
		item.App = scope.App
	}
	if item.Who == "" {
		item.Who = scope.Caller.Member
	}
	if item.Who == "" {
		item.Who = svc.Owner
	}
	if item.State == "" {
		item.State = bridge.ApprovalOpen
	}
	if item.CreatedAt == "" {
		item.CreatedAt = a.eng.clock().UTC().Format("2006-01-02T15:04:05Z")
	}
	if err := a.eng.saveApproval(item); err != nil {
		return bridge.Approval{}, bridge.Errorf(bridge.CodeInternal, "could not save the approval")
	}
	a.eng.publishApproval(item)
	_ = ctx
	return item, nil
}

// List returns approvals. An empty state means the open ones.
func (a *Approvals) List(ctx context.Context, scope svc.Scope, p bridge.ListApprovalsParams) (bridge.ListApprovalsResult, error) {
	all, err := a.eng.loadApprovals(scope.App)
	if err != nil {
		return bridge.ListApprovalsResult{}, bridge.Errorf(bridge.CodeInternal, "could not list approvals")
	}
	want := p.State
	if want == "" {
		want = bridge.ApprovalOpen
	}
	out := []bridge.Approval{}
	for _, item := range all {
		if item.State == want {
			out = append(out, item)
		}
	}
	_ = ctx
	return bridge.ListApprovalsResult{Approvals: out}, nil
}

// Decide answers an open approval. On a run gate it continues or stops the
// run; on a standalone approval with a Tool (the app agent's) it runs the
// action as approved and tells the thread that asked.
func (a *Approvals) Decide(ctx context.Context, scope svc.Scope, p bridge.DecideApprovalParams) (bridge.Approval, error) {
	item, output, err := a.eng.decide(ctx, scope, p)
	if err != nil {
		return bridge.Approval{}, err
	}
	if item.RunID != "" {
		if err := a.eng.answer(item); err != nil {
			return bridge.Approval{}, err
		}
		a.eng.drive(ctx, scope.App, item.RunID)
		return item, nil
	}
	a.eng.tellThread(scope, item, output)
	return item, nil
}

// decide records the answer and, for a standalone approved action, runs it.
// The claim from open is made under decideMu, so a double click or a
// reconnect that repeats the answer is refused as already decided and the
// action runs once.
func (e *engine) decide(ctx context.Context, scope svc.Scope, p bridge.DecideApprovalParams) (bridge.Approval, any, error) {
	item, err := e.claim(scope, p)
	if err != nil || item.RunID != "" || item.Tool == "" || item.State != bridge.ApprovalApproved {
		return item, nil, err
	}
	output, runErr := e.callApproved(ctx, agentScope(scope), item.Tool, item.Payload)
	if runErr != nil {
		item.State, item.Error = bridge.ApprovalFailed, errorText(runErr)
	} else {
		item.State, item.Result = bridge.ApprovalDone, ranSummary(item.Tool, output)
	}
	if err := e.saveApproval(item); err != nil {
		return bridge.Approval{}, nil, bridge.Errorf(bridge.CodeInternal, "could not save the approval")
	}
	e.publishApproval(item)
	return item, output, nil
}

// claim moves an open approval to approved or rejected and publishes it.
func (e *engine) claim(scope svc.Scope, p bridge.DecideApprovalParams) (bridge.Approval, error) {
	if p.Answer != "yes" && p.Answer != "no" {
		return bridge.Approval{}, bridge.Errorf(bridge.CodeInvalid, "answer must be yes or no")
	}
	e.decideMu.Lock()
	defer e.decideMu.Unlock()
	item, ok, err := e.loadApproval(scope.App, p.ApprovalID)
	if err != nil {
		return bridge.Approval{}, bridge.Errorf(bridge.CodeInternal, "could not read the approval")
	}
	if !ok {
		return bridge.Approval{}, bridge.Errorf(bridge.CodeNotFound, "no approval "+p.ApprovalID)
	}
	if item.State != bridge.ApprovalOpen {
		return bridge.Approval{}, bridge.Errorf(bridge.CodeConflict, "approval is already decided")
	}
	if item.Who != "" && scope.Caller.Member != item.Who && scope.Caller.Member != svc.Owner {
		return bridge.Approval{}, bridge.Errorf(bridge.CodeDenied, "only "+item.Who+" can decide this")
	}
	applyAnswer(&item, p)
	item.DecidedAt = e.clock().UTC().Format("2006-01-02T15:04:05Z")
	if err := e.saveApproval(item); err != nil {
		return bridge.Approval{}, bridge.Errorf(bridge.CodeInternal, "could not save the approval")
	}
	e.publishApproval(item)
	return item, nil
}

func applyAnswer(item *bridge.Approval, p bridge.DecideApprovalParams) {
	if p.Answer != "yes" {
		item.State = bridge.ApprovalRejected
		return
	}
	item.State = bridge.ApprovalApproved
	if item.Payload == nil {
		item.Payload = map[string]any{}
	}
	for key, value := range p.Edit {
		item.Payload[key] = value
	}
}

// agentScope is the authority the action was asked for under: the app
// agent's, so the capability check is the one the agent's call passed.
func agentScope(scope svc.Scope) svc.Scope {
	scope.Caller.Agent = true
	return scope
}

// ApprovalListener hears how a thread's approval ended. The app agent
// implements it; runs finds it on all.Agent at call time, so svc.Agent keeps
// its one method.
type ApprovalListener interface {
	ApprovalSettled(scope svc.Scope, item bridge.Approval, output any)
}

func (e *engine) tellThread(scope svc.Scope, item bridge.Approval, output any) {
	if item.Thread == "" || e.all == nil {
		return
	}
	if listener, ok := e.all.Agent.(ApprovalListener); ok {
		listener.ApprovalSettled(scope, item, output)
	}
}

// ranSummary is the short line a done approval carries: what ran and the id
// it produced, when the connector returned one.
func ranSummary(tool string, output any) string {
	line := "Ran " + strings.Replace(tool, ".", " ", 1)
	if reply, ok := output.(map[string]any); ok {
		if id, ok := reply["id"].(string); ok && id != "" {
			line += " (" + id + ")"
		}
	}
	return line
}

func errorText(err error) string {
	var typed *bridge.Error
	if errors.As(err, &typed) {
		return typed.Message
	}
	return err.Error()
}

func (e *engine) answer(item bridge.Approval) error {
	job, err := e.loadJob(item.App, item.RunID)
	if err != nil || job == nil {
		return err
	}
	if item.State == bridge.ApprovalApproved {
		job.Answer = "yes"
		job.Edit = item.Payload
	} else {
		job.Answer = "no"
	}
	return e.saveJob(job)
}
