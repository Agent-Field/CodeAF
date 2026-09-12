package remote

// ── THE MODEL THE TURN IN FLIGHT IS ON, ACROSS THE WIRE ─────────────────────
//
// internal/session publishes the model the work is talking to
// ([session.Agent.TurnModel]). The surface names that model wherever it names
// this conversation's model, so the cell and the `via` machine beside it cannot
// disagree (internal/tui3's [app.wireModel]) — and over a connection the fact
// has to cross.
//
// IT RIDES THE PHOTOGRAPH AND IS NEVER A CALL. The seam draws it on every
// repaint, and PERF.md's law is that a View over --host issues zero far calls,
// so it arrives on the fact set the engine states unasked ([session.Facts],
// replica.go) exactly as the thinking rung beside it does.
//
// AND IT NEEDS NO CAPABILITY FLAG, which is worth saying because the rung beside
// it has one. A flag exists to tell "this engine cannot answer" from "the answer
// is nothing", and those are only different when they look the same — an empty
// rung could mean either. Here they cannot be confused: "" already means NOTHING
// IS IN FLIGHT, which is exactly what a surface should draw against an engine
// that sends no such fact, and the seam falls back to the dial by itself. The
// version handshake settles the rest: [Version] is refused unequal at the door
// (client.go), so a connected engine is this build and always has the fact.

// TurnModel is the model the turn in flight is on, and "" when nothing is.
//
// IT IS A MEMORY READ, for [Agent.ResolvedEffort]'s stated reason.
func (a *Agent) TurnModel() string { return a.c.facts.read().TurnModel }
