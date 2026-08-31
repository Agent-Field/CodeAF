package lane

// ── λ: WHAT A SECOND IS WORTH ───────────────────────────────────────────────
//
// One number, in SECONDS PER DOLLAR, and it is not a setting. Three questions
// this build can already answer decide it:
//
//   - Is a person watching this stream right now? Then their attention has a
//     wall value, and at a nominal rate a dollar buys something like ninety
//     seconds of it — in plain terms, spend up to a cent to save a second.
//   - Is this call on the critical path of a task? Then λ is the wall value of
//     the whole task, because the person waits for its end. Off the path with
//     slack to spare, λ falls to zero and price wins outright.
//   - Is there a deadline? Then λ rises as it nears, because a missed deadline
//     is a step cost rather than a slow one.
//
// WHY NOT A SLIDER. A "speed versus cost" control asks the person to state λ
// once, for every future request, when the graph already knows it per request
// and the answer differs by two orders of magnitude between a chat turn and a
// background node. The routing row keeps its three words and gains this
// meaning: `price` sets λ to zero for everything, `latency` computes it, `off`
// stays total.
//
// This file computes nothing yet. Lane L-B fills it in, plumbs `WithValueOfTime`
// through `internal/session`, and may not read a clock — see choose.go.

// valueOfTime is λ for one request, in seconds per dollar.
//
// It answers zero — price only — until lane L-B computes it. That is the safe
// empty answer rather than the useful one: with λ at zero the chooser prefers
// the cheapest acceptable lane, which is a defensible thing to do and never a
// surprise on a bill.
func valueOfTime(Request) float64 { return 0 }
