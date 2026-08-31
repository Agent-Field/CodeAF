package lane

import (
	"context"
	"errors"
)

// ── THE SHEET: A PRIOR NOBODY HAD TO PAY FOR ────────────────────────────────
//
// OpenRouter publishes, per model, one row per lane: first-token latency and
// throughput at the 50th, 75th, 90th and 99th percentiles over the last half
// hour, plus uptime, tariff, quantization, context and output ceilings, and
// whether the lane takes tool calls. That is a free prior for every lane of
// every model, which is why nothing in this design is blind on the first call
// of a process.
//
// THIS FILE FETCHES NOTHING YET. What is here is the seam and the law it must
// be built behind — one reading per beat, nothing on the send path, absence
// rather than a guess — and lane L-A replaces it with the real client. Until
// then [Sheet.Rows] returns nothing, which every caller already handles because
// an empty sheet is a normal state on a cold machine.

// ErrNoSheet is what a refresh returns while no sheet client is wired in. It is
// an error rather than a silent success because a beat that thinks it fetched
// is a beat nobody will ever notice is dead.
var ErrNoSheet = errors.New("lane: no sheet client")

// sheet is the empty sheet: it holds nothing and fetches nothing.
type sheet struct{}

// newSheet builds the empty sheet. It is called from the registry and nowhere
// else.
func newSheet() *sheet { return &sheet{} }

// Rows returns nothing, because nothing has been fetched. It takes no lock and
// makes no call, which is the property the send path depends on.
func (s *sheet) Rows(string) []Row { return nil }

// Refresh does not fetch. It says so rather than reporting a success it did not
// have.
func (s *sheet) Refresh(context.Context, string) error { return ErrNoSheet }
