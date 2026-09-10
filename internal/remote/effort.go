package remote

import "encoding/json"

// ── THE CONVERSATION'S THINKING RUNG, ACROSS THE WIRE ───────────────────────
//
// internal/session holds the ladder and the dial (its effort.go), and until this
// file a HOSTED conversation had neither: the surface asserts the dial on its
// agent, *remote.Agent did not carry it, so `aforge chat --host devbox` drew no
// rung, answered `ctrl+v` with nothing, and said nowhere that the knob every
// local conversation has was missing. The task's own rung crossed
// (wire_task.go); the conversation's did not.
//
// THE READ A FRAME TAKES IS NOT A CALL. The rung is drawn on the seam beside the
// model, which means it is read on every repaint — so it rides the fact set the
// engine states unasked ([session.Facts.Thinking], replica.go) and this end
// answers from memory. The two calls below happen on a KEYSTROKE and never on a
// frame, which is the same division tasksetup.go keeps.
//
// AND THE CAPABILITY IS THE WELCOME'S TO ANSWER, not the type system's. Every
// connection has these methods on it, and "" is a real rung on this ladder — a
// conversation asking for no thinking at all — so neither the assertion nor the
// answer can tell a far engine that has no dial from one whose dial is off.
// [Welcome.Effort] is the only thing that can, and a surface reads it before it
// draws anything ([Agent.EffortSupported]).

// effortDoor is the dial as an engine must have it to serve one: the slice of
// *session.Agent this file speaks to.
//
// It is asserted rather than added to [WrappedAgent] on [taskSetupDoor]'s
// terms — a capability is asserted, never required — so an engine built before
// the ladder keeps its conversations and simply advertises no dial.
type effortDoor interface {
	ConversationEffort() string
	ResolvedEffort() string
	SetConversationEffort(rung string) bool
}

// effortKnown is whether this engine has the whole dial. A partial one is no
// dial at all: the surface draws the resolved rung and moves the stored one, so
// an engine with one of the three would light a cell nothing could turn.
func effortKnown(agent any) bool { _, ok := agent.(effortDoor); return ok }

// EffortSupported answers for THE MACHINE AT THE OTHER END, off what it said at
// the door — a fact this end could not otherwise learn without setting a rung
// and reading the refusal, which is after the person has pressed the key.
func (a *Agent) EffortSupported() bool { return a.c.Welcome().Effort }

// ResolvedEffort is the rung the next turn will actually ask for.
//
// IT IS A MEMORY READ. The seam draws it on every frame, and PERF.md's law is
// that a View over --host issues zero far calls (replica.go states the whole
// reason).
func (a *Agent) ResolvedEffort() string { return a.c.facts.read().Thinking }

// ConversationEffort is the rung THIS conversation was set to, "" when nobody
// has set one.
//
// IT IS AN EXPLICIT READ, and it is allowed to be one because nothing draws it:
// it answers the question "what did I choose", asked by a person opening the
// dial, where the seam's own word is always the resolved rung ([Agent.ResolvedEffort]).
// A dead link or an engine with no dial answers "", which is the same string the
// local door gives for a conversation nobody has set — so a caller cannot tell
// them apart, and must ask [Agent.EffortSupported] first if it needs to.
func (a *Agent) ConversationEffort() string {
	if !a.EffortSupported() {
		return ""
	}
	payload, err := a.c.call(nil, MethodEffort, nil)
	if err != nil {
		return ""
	}
	var rung string
	_ = json.Unmarshal(payload, &rung)
	return rung
}

// SetConversationEffort sets this conversation's rung and reports whether the
// word was one — the local door's contract exactly (internal/session's
// [session.Agent.SetConversationEffort]), so the surface's one path from the
// chord and the menu (internal/tui3's setEffortRung) cannot mean two things
// depending on which machine the conversation is on.
//
// IT READS THE RESOLVED RUNG BACK BEFORE IT RETURNS, and that is not a
// convenience. The caller asks what the rung resolved to in the very next
// statement — to say "thinking stays high" when a level on the model is winning
// — and the engine's own push is still on the wire at that moment. A read that
// waited for the push would report the rung from before the keystroke.
func (a *Agent) SetConversationEffort(rung string) bool {
	if !a.EffortSupported() {
		return false
	}
	payload, err := a.c.call(nil, MethodSetEffort, rung)
	if err != nil {
		return false
	}
	var took bool
	if err := json.Unmarshal(payload, &took); err != nil || !took {
		return false
	}
	a.c.facts.setThinking(a.farResolvedEffort())
	return true
}

// farResolvedEffort asks the engine what the rung resolved to, once, on the
// keystroke that moved it. A failure here answers "" rather than guessing at
// the word that was asked for: the engine's announce is already on its way with
// the truth on it, and a guess would be a second authority for the length of
// one frame (replica.go's law).
func (a *Agent) farResolvedEffort() string {
	payload, err := a.c.call(nil, MethodResolvedEffort, nil)
	if err != nil {
		return ""
	}
	var rung string
	_ = json.Unmarshal(payload, &rung)
	return rung
}
