package lane

import "context"

// ── THE PROBE: FRESHNESS BOUGHT TWO SECONDS EARLY ───────────────────────────
//
// The sheet is a half-hour aggregate and our own belief may be minutes old, and
// both of those are the best available right up until the moment a person
// starts typing — at which point we know, seconds in advance, that a request is
// coming and roughly where it will go.
//
// So on the first keystroke of a turn, debounced, a one-token request goes to
// the top two lanes of the frontier with `provider.only` naming each, and its
// first-token wait is recorded. It costs about two hundredths of a cent for a
// turn, it warms the connection so that a TLS handshake is out of the real
// measurement, and it lands in the ledger with a SMALL observation noise
// because it measured exactly our path to that lane, right now, rather than
// everybody's average over half an hour.
//
// It is a first-token measurement and never a rate one: an answer one token
// long rates the handshake and nothing else.
//
// Its budget is the point at which it stays cheap: at most one pair per twenty
// seconds per session, none at all when λ is zero — nobody is waiting, so
// freshness buys nothing — and none while the connection pool is pacing.
//
// THIS FILE PROBES NOTHING YET. Lane L-C fills it in and writes the transport
// half in `internal/provider/probe.go`.

// prober is the empty prober: it sends nothing.
type prober struct{}

// newProber builds the empty prober. It is called from the registry and nowhere
// else.
func newProber() *prober { return &prober{} }

// Probe sends nothing. It returns immediately, which is also the contract the
// real one keeps: nothing ever waits on a probe.
func (p *prober) Probe(context.Context, string, []string) {}
