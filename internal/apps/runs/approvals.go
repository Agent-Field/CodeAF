package runs

import (
	"context"

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

// Decide answers an open approval and, on a run gate, continues or stops the run.
func (a *Approvals) Decide(ctx context.Context, scope svc.Scope, p bridge.DecideApprovalParams) (bridge.Approval, error) {
	if p.Answer != "yes" && p.Answer != "no" {
		return bridge.Approval{}, bridge.Errorf(bridge.CodeInvalid, "answer must be yes or no")
	}
	item, ok, err := a.eng.loadApproval(scope.App, p.ApprovalID)
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
	if p.Answer == "yes" {
		item.State = bridge.ApprovalApproved
		if item.Payload == nil {
			item.Payload = map[string]any{}
		}
		for key, value := range p.Edit {
			item.Payload[key] = value
		}
	} else {
		item.State = bridge.ApprovalRejected
	}
	item.DecidedAt = a.eng.clock().UTC().Format("2006-01-02T15:04:05Z")
	if err := a.eng.saveApproval(item); err != nil {
		return bridge.Approval{}, bridge.Errorf(bridge.CodeInternal, "could not save the approval")
	}
	a.eng.publishApproval(item)
	if item.RunID != "" {
		if err := a.eng.answer(item); err != nil {
			return bridge.Approval{}, err
		}
		a.eng.drive(ctx, scope.App, item.RunID)
	}
	return item, nil
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
