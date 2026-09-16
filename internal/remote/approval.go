package remote

import (
	"encoding/json"
	"errors"
)

// ── THE CONVERSATION'S POSTURE ON THE TOOL GATE, ACROSS THE WIRE ────────────
//
// internal/session holds the posture and its door (approvalposture.go), and
// this file is effort.go's shape one dial over: a hosted conversation gets the
// approvals chip, the chord and the command, on the machine the conversation
// runs on — which is the only machine whose gate they could honestly move.
//
// THE READ A FRAME TAKES IS NOT A CALL. The posture is drawn on the seam beside
// the rung on every repaint, so it rides the fact set the engine states unasked
// ([session.Facts.Approval], replica.go) and this end answers from memory. The
// two calls below happen on a KEYSTROKE and never on a frame.
//
// AND THE CAPABILITY IS THE WELCOME'S TO ANSWER, for [Welcome.Effort]'s reason:
// every connection has these methods on it, and "" is what a far engine with no
// posture door answers as well as what a session with no gate answers. So
// [Welcome.Approval] says whether the far engine has the door, and a surface
// reads it before it offers to move anything ([Agent.ApprovalDial]). An engine
// too old to have it leaves the chip a reading of [Welcome.ApprovalMode] — the
// far machine's row, carried once — and the surface's doors say so.

// approvalDoor is the posture as an engine must have it to serve one: the
// slice of *session.Agent this file speaks to.
type approvalDoor interface {
	ApprovalDial() bool
	ResolvedApprovalPosture() string
	SetApprovalPosture(posture string) error
}

// approvalKnown is whether this engine has the whole door AND a gate behind
// it: an agent with the methods and no gate (a worker, a headless run) has no
// dial, exactly as it has none locally.
func approvalKnown(agent any) bool {
	door, ok := agent.(approvalDoor)
	return ok && door.ApprovalDial()
}

// ApprovalDial answers for THE MACHINE AT THE OTHER END, off what it said at
// the door.
func (a *Agent) ApprovalDial() bool { return a.c.Welcome().Approval }

// ResolvedApprovalPosture is the posture the far gate is standing at. IT IS A
// MEMORY READ, for [Agent.ResolvedEffort]'s reason: the seam draws it on every
// frame, and a View over --host issues zero far calls.
func (a *Agent) ResolvedApprovalPosture() string { return a.c.facts.read().Approval }

// SetApprovalPosture moves the far conversation's posture and says why not —
// the local door's contract exactly (internal/session's
// [session.Agent.SetApprovalPosture]), so the surface's one path from the
// chord, the press and the command cannot mean two things depending on which
// machine the conversation is on.
//
// IT READS THE RESOLVED POSTURE BACK BEFORE IT RETURNS, on
// [Agent.SetConversationEffort]'s terms: the caller says the new word in the
// very next statement, and the engine's own push is still on the wire.
func (a *Agent) SetApprovalPosture(posture string) error {
	if !a.ApprovalDial() {
		return errors.New("the engine this conversation runs on has no dial onto what runs without asking; update it and reconnect")
	}
	payload, err := a.c.call(nil, MethodSetApproval, posture)
	if err != nil {
		return err
	}
	var refusal string
	_ = json.Unmarshal(payload, &refusal)
	if refusal != "" {
		return errors.New(refusal)
	}
	a.c.facts.setApproval(a.farResolvedApproval())
	return nil
}

// farResolvedApproval asks the engine what the posture resolved to, once, on
// the keystroke that moved it — [Agent.farResolvedEffort]'s bargain.
func (a *Agent) farResolvedApproval() string {
	payload, err := a.c.call(nil, MethodResolvedApproval, nil)
	if err != nil {
		return ""
	}
	var posture string
	_ = json.Unmarshal(payload, &posture)
	return posture
}
