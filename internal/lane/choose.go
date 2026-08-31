package lane

// ── THE CHOICE: A GATE, A FRONTIER, AND ONE SCALAR ──────────────────────────
//
// Four things are wanted at once — cheap, quick to start, quick to finish,
// right — and a fixed weight over them is a guess about a trade the person
// never made. The design's move is to stop guessing the weight and DERIVE it
// from the request: the price of a second is a property of who is waiting and
// what their wait costs (λ, in [Request.ValueOfTime]), and the value of
// throughput saturates at reading speed ([PerceivedSeconds]). With those two
// facts the four objectives collapse to one scalar PER REQUEST, different for a
// talk turn and for a background node, and the Pareto frontier over lanes is
// only the candidate set that scalar chooses from.
//
// The order is fixed and each step has a law:
//
//  1. GATE — deterministic, never sampled. Tools, output ceiling, context,
//     quantization, uptime, and the quality posterior against QualityNeed. A
//     gate drop is never a sampled event: a lane that would drop the tool call
//     is a wrong answer rather than a slow one.
//  2. PRUNE — drop a lane another lane beats on all of first token, rate,
//     price and quality at the p75. Scored at p75 rather than the mean so that
//     an UNCERTAIN lane stays in: it might be good, and that is the only kind
//     of exploration worth paying for.
//  3. SCORE — perceived seconds plus price through λ, Thompson-sampled from
//     the posteriors, with the sampling spread scaled by the horizon.
//  4. ASK — Order is the survivors by sampled score, with fallbacks left on.
//
// THIS FILE CHOOSES NOTHING YET. The empty chooser has no opinion, which is a
// real answer: the transport sends what it would have sent before this package
// existed. Lane L-B replaces it.
//
// Nothing in this file, in frontier.go or in value.go may read a clock. The
// moment is [Request.Now] and a structural test in this directory enforces it,
// because a choice that reads the wall is a choice no test can pin.

// chooser is the empty chooser: it has no opinion about anything.
type chooser struct{}

// newChooser builds the empty chooser. It is called from the registry and
// nowhere else.
func newChooser() *chooser { return &chooser{} }

// Choose returns no opinion. The zero [Choice] asks for no order, no pin and no
// refusal, and carries no explanation — under the emptiness law a surface draws
// nothing at all for it, which is the truth here.
func (c *chooser) Choose(Request) Choice { return Choice{} }
