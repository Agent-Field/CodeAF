package remote

// ── THE MODEL THE TURN IN FLIGHT IS ON, ACROSS THE WIRE ─────────────────────
//
// internal/session latches a turn's model when the turn opens and moves it only
// when the turn itself hops to a fallback ([session.Agent.TurnModel]). The
// surface names that model wherever it names this conversation's model, so the
// cell and the `via` machine beside it cannot disagree (internal/tui3's
// [app.wireModel]) — and over a connection the fact has to cross.
//
// IT RIDES THE PHOTOGRAPH AND IS NEVER A CALL. The seam draws it on every
// repaint, and PERF.md's law is that a View over --host issues zero far calls,
// so it arrives on the fact set the engine states unasked ([session.Facts],
// replica.go) exactly as the thinking rung beside it does.
//
// AND THE CAPABILITY IS THE WELCOME'S TO ANSWER, not the type system's. Every
// connection has the method on it, and "" is a real answer — an idle
// conversation — so neither the assertion nor the answer can tell an engine that
// has no latch from a conversation that is simply not working.
// [Welcome.TurnModel] is the only thing that can.

// turnModelDoor is the latch as an engine must have it to serve one.
//
// It is asserted rather than required for [effortDoor]'s reason — a capability
// is asserted, never required — so an engine built before the latch keeps its
// conversations and simply advertises nothing.
type turnModelDoor interface {
	TurnModel() string
}

// turnModelKnown is whether this engine can name the model a turn is on.
func turnModelKnown(agent any) bool { _, ok := agent.(turnModelDoor); return ok }

// TurnModelSupported answers for THE MACHINE AT THE OTHER END, off what it said
// at the door.
func (a *Agent) TurnModelSupported() bool { return a.c.Welcome().TurnModel }

// TurnModel is the model the turn in flight is on, and "" when nothing is in
// flight OR when the far engine does not keep the fact.
//
// THE TWO EMPTIES ARE DELIBERATELY THE SAME STRING HERE, and the caller tells
// them apart with [Agent.TurnModelSupported] before it draws anything — which is
// the same division [Agent.ResolvedEffort] and [Agent.EffortSupported] keep, for
// the same reason: a reading taken on every frame must not carry a second
// return nobody checks.
//
// IT IS A MEMORY READ, for [Agent.ResolvedEffort]'s stated reason.
func (a *Agent) TurnModel() string { return a.c.facts.read().TurnModel }
