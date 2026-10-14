package session

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/config"
)

// Task calls ask through their owning conversation, whose consent ids and
// question subscription already span every window. The task remains blocked;
// the conversation can continue while its questions stand.
func (a *Agent) appPermissionOwner() *Agent {
	if a.config.appApprovalOwner != nil {
		return a.config.appApprovalOwner
	}
	return a
}

// EnableAppPermissions applies the app gate to a shared engine for its lifetime.
func (a *Agent) EnableAppPermissions() { a.appPermissions.Store(true) }
func (a *Agent) appPermissionMode() bool {
	owner := a.appPermissionOwner()
	return owner.config.DeterministicNames || owner.appPermissions.Load()
}

func (a *Agent) appPermissionDecision(call ai.ToolCall, decision approval.Decision, governed bool) (approval.Decision, bool) {
	if !a.appPermissionMode() {
		return decision, governed
	}
	owner := a.appPermissionOwner()
	if a.config.InTask {
		decision, governed = owner.decide(call)
	}
	args := json.RawMessage(call.Function.Arguments)
	floor, err := config.AppPermissionAlwaysAsk(owner.config.ProfileDir, call.Function.Name, args, a.config.InTask)
	if err != nil {
		return approval.Decision{Action: approval.ActionDeny, Rule: "could not read permission settings"}, true
	}
	if decision.Action == approval.ActionDeny {
		return decision, true
	}
	if floor {
		return approval.Decision{Action: approval.ActionPrompt, Rule: "this action always asks"}, true
	}
	r, err := config.MatchAppPermissionRule(owner.config.ProfileDir, owner.id, call.Function.Name, args, a.config.InTask, call.ID)
	if err != nil {
		return approval.Decision{Action: approval.ActionDeny, Rule: "could not read saved permission rules"}, true
	}
	if r != nil {
		q := owner.consentAsk(0, call, decision)
		q.Ref = "auto/" + call.ID
		q.Asked = time.Now()
		q.Head = config.AppPermissionCommand(call.Function.Name, args)
		q.Reason = "you always allow this"
		owner.emitQuestion(EventQuestionAnswered, q, &Answer{Kind: QuestionConsent, Ref: q.Ref, Key: "1", From: "rule:" + r.ID, DecidedBy: DecidedByDial})
		return approval.Decision{Action: approval.ActionAllow, Rule: "saved permission " + r.ID}, true
	}
	return decision, governed
}

func (a *Agent) appTaskPermission(ctx context.Context, hub *eventHub, call ai.ToolCall, decision approval.Decision) (toolResult, bool) {
	owner := a.appPermissionOwner()
	answer, err := owner.askAnswerFor(ctx, hub, call, decision, a.config.appApprovalTask)
	if err != nil {
		return refusal(notApprovedWording(err)), false
	}
	if !answer.allow {
		return refusal(personDeclined), false
	}
	return toolResult{}, true
}

func appTaskPermissionName(node *TaskNode) string {
	if node.spec.planID != "" {
		return planStoreID(node.spec.planID)
	}
	return fmt.Sprintf("%d", node.id)
}

// ReplyRunning separates the inline reply from tasks waiting on permissions.
func (a *Agent) ReplyRunning() bool { a.mu.Lock(); defer a.mu.Unlock(); return a.running }
