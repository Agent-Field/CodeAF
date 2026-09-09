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
// This file is the seam that fixes that, and it is deliberately a PUSH rather
// than a re-read. The policy is built from three settings rows resolved through
// the project layer (cmd/aforge's v3Policy), and a gate that re-read them on the
// tool path would put a file walk in front of every call and would change its
// mind halfway through a batch. What a surface does instead is rebuild the
// policy at the moment somebody asked for a change and hand the finished thing
// over.
//
// THE POINTER IS GUARDED AND THE POLICY IS NOT. approval.Policy is read-only
// once built, so the lock here covers a pointer copy and nothing else: a call
// already inside Policy.Check can never see a rule set change underneath it, and
// a decision taken a moment before a push is still a decision about a rule set
// that really was in force. This is the same bargain the belt makes under armMu
// (session.go) — swap the header, never edit what a reader is walking.

import "github.com/Agent-Field/aforge-v2/internal/approval"

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
}

// approvalGate is the policy as it stands right now: the pushed one if there is
// one, otherwise the one the session launched on.
//
// Every policy reader uses this synchronized snapshot. A pushed policy can
// change while a tool is being considered, so reading the launch config alone
// would miss the latest decision and reading the current pointer unlocked
// would race with SetApprovalPolicy.
func (a *Agent) approvalGate() *approval.Policy {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.approvalPolicy != nil {
		return a.approvalPolicy
	}
	return a.config.ApprovalPolicy
}
