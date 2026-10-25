package session

// THE GATE IS NOT A SNAPSHOT OF THE LAUNCH.
//
// Config.ApprovalPolicy is the policy a session started on, and until this file
// existed it was also the policy that session died on. A rule banked from a
// consent card — the "always" key, written into the person's own rows by
// internal/config's approvalmemory.go — was saved to disk and then not read
// again until the next launch, so the very next call to the same tool asked the
// same question. The one keystroke whose entire purpose is to be asked less did
// not reduce the asking at all until somebody quit and came back.
//
// Surfaces can push a finished policy after changing a rule. Sessions with a
// door onto settings also refresh at the decision point, so changes made in
// another window reach the next tool call. The settings adapter owns caching;
// no watcher or polling loop belongs to a session.
//
// THE POINTER IS GUARDED AND THE POLICY IS NOT. approval.Policy is read-only
// once built, so the lock here covers a pointer copy and nothing else: a call
// already inside Policy.Check can never see a rule set change underneath it, and
// a decision taken a moment before a push is still a decision about a rule set
// that really was in force. This is the same bargain the belt makes under armMu
// (session.go) — swap the header, never edit what a reader is walking.

import "github.com/Agent-Field/codeaf/internal/approval"

// SetApprovalPolicy replaces the standing gate for the rest of this session.
//
// A NIL NEVER REPLACES ANYTHING. Nil is the configured-nothing case and it
// means allow everything (Config.ApprovalPolicy states that law), which is
// exactly the wrong answer for a caller that failed to build a policy: a
// surface that could not read the settings rows must leave the gate that is
// already standing rather than open it. So a nil is dropped here rather than
// stored, and a caller that genuinely wants an ungated session simply never
// calls this.
//
// It cannot widen a floor, because none of the floors live in a Policy. The
// critical-command table, the refusal to vouch for a compound line, the degrade
// on a bash call whose command cannot be read, and the floor under calls that
// act in the person's name outside this machine are all inside
// internal/approval and apply to whatever rule set is handed to them. A pushed
// policy buys exactly what a relaunched one buys and not one rung more.
func (a *Agent) SetApprovalPolicy(policy *approval.Policy) {
	if policy == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.approvalPolicy = policy
	a.approvalVersion++
}

// approvalGate is the policy as it stands right now: the pushed one if there is
// one, otherwise the one the session launched on.
//
// EVERY READ OF THE POLICY GOES THROUGH HERE — consent.go's decide, looped.go's
// promptMode, and anything either of them grows. Both used to reach
// a.config.ApprovalPolicy directly and without a lock, which was correct only
// while nothing in the tree could ever write it; the moment
// [Agent.SetApprovalPolicy] existed, a single unguarded read became a race the
// detector finds in a second and a person would never reproduce.
func (a *Agent) approvalGate() *approval.Policy {
	if source, ok := a.jobBudgetOf().(interface{ FactoryAuto() bool }); ok && source.FactoryAuto() {
		return automaticJobPolicy(a.baseApprovalGate())
	}
	return a.baseApprovalGate()
}

func (a *Agent) baseApprovalGate() *approval.Policy {
	if a.approvalParent != nil {
		return a.approvalParent.baseApprovalGate()
	}
	if a.config.ApprovalGate != nil {
		// A failed read keeps the last working gate, just as a failed push does.
		policy, _ := a.refreshApprovalGate()
		return policy
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.approvalPolicyLocked()
}

// approvalPolicyLocked copies the decision snapshot while mu still protects
// the version check and publication that selected it.
func (a *Agent) approvalPolicyLocked() *approval.Policy {
	if a.approvalPolicy != nil {
		return a.approvalPolicy
	}
	return a.config.ApprovalPolicy
}
