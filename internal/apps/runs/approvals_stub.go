package runs

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Approvals is the stub approvals store. B4 replaces this file.
type Approvals struct{}

var _ svc.Approvals = (*Approvals)(nil)

func NewApprovals(svc.Config, *svc.Services) *Approvals { return &Approvals{} }

func (*Approvals) Open(context.Context, svc.Scope, bridge.Approval) (bridge.Approval, error) {
	return bridge.Approval{}, bridge.NotBuilt("approvals")
}

func (*Approvals) List(context.Context, svc.Scope, bridge.ListApprovalsParams) (bridge.ListApprovalsResult, error) {
	return bridge.ListApprovalsResult{}, bridge.NotBuilt("approvals")
}

func (*Approvals) Decide(context.Context, svc.Scope, bridge.DecideApprovalParams) (bridge.Approval, error) {
	return bridge.Approval{}, bridge.NotBuilt("approvals")
}
