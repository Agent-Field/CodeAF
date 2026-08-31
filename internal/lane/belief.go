package lane

// ── THE LEDGER: WHAT THIS PROCESS HAS MEASURED ──────────────────────────────
//
// One [Belief] per lane per model, each of them two Kalman filters and a Beta,
// aged on read and folded on write. The sheet primes it, a finished stream
// updates it, a probe updates it sharply, an unusable answer moves its quality,
// and a file carries it between processes.
//
// THIS FILE BELIEVES NOTHING YET. The empty ledger below accepts every
// observation and remembers none of it, and answers every question with "no
// belief" — which is exactly what a chooser must do something sensible with
// anyway, on the first call of a fresh machine. Lane L-A replaces it.
//
// The one thing the empty ledger must never do is invent a number. A ledger
// that answered with a plausible-looking posterior would be a router steering
// on a measurement nobody took, and the picker would draw it as fact.

// ledger is the empty ledger: it forgets everything, immediately and honestly.
type ledger struct{}

// newLedger builds the empty ledger. It is called from the registry and nowhere
// else.
func newLedger() *ledger { return &ledger{} }

// Note accepts a sighting and keeps none of it.
func (l *ledger) Note(Sighting) {}

// NoteOutcome accepts an outcome and keeps none of it.
func (l *ledger) NoteOutcome(Outcome) {}

// Belief has none.
func (l *ledger) Belief(ID) (Belief, bool) { return Belief{}, false }

// Beliefs has none.
func (l *ledger) Beliefs(string) []Belief { return nil }

// Prime accepts a sheet row and keeps none of it.
func (l *ledger) Prime(Row, float64) {}
