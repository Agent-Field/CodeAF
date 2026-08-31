// Package lane is what this build believes about the machines behind a model.
//
// ── WHY THERE IS A PACKAGE AT ALL ───────────────────────────────────────────
//
// A model id is an address; the LANE is the machine. One model id is served by
// a dozen endpoints that differ by 7× on the wait before the first token and by
// 12× on how fast they write, at roughly the same price — and they differ in
// CAPABILITY too, so the fastest of them may be the one that drops the tool
// call. Choosing among them is a bigger speed lever than choosing the model,
// and until this package nothing in the build held an opinion about it that
// survived either a restart or a request.
//
// The package holds the opinion and nothing else. It has no transport: it never
// opens a connection, never reads a clock of its own, and never draws anything.
// A sighting is handed to it, a request is asked of it, and what comes back is
// a preference some other layer puts on a wire. That boundary is the whole
// design — see docs/ARCHITECTURE.md, "Lanes" — and four structural tests in
// this directory hold it.
//
// ── THE LAWS ────────────────────────────────────────────────────────────────
//
// NO FETCH ON THE SEND PATH. The sheet is refreshed on a beat, never by a
// request that is about to be sent. A missing sheet means "no prior, use the
// belief alone" — it never means "wait while I go and look".
//
// EVERY NUMBER A PERSON SEES IS THE POSTERIOR. The sheet is a thirty-minute
// aggregate over everybody's prompts; our own sightings are about our prompts
// from our region. Both are evidence and neither is truth, so what a picker
// shows is the belief that combined them, never the raw sheet row.
//
// QUALITY IS A GATE AND NEVER A WEIGHT. A lane that drops tool calls, truncates
// or returns JSON the decoder refuses leaves the candidate set. Weighing
// quality against price is how a router learns to ship wrong answers cheaply.
//
// A HEDGE IS A MEASUREMENT. The second request a slow stream earns is also the
// only cheap way to learn what the alternative lane would have done, so its
// result is fed back as a sighting like any other.
//
// NO FIXED THRESHOLDS. The four constants the strike ledger ran on — two
// seconds, thirty tokens a second, fifteen seconds, two strikes — are what this
// package exists to retire. Slow means "surprising for this lane, ten minutes
// ago", which is a number the posterior already carries.
package lane

import (
	"math"
	"time"
)

// ── WHAT A LANE IS ──────────────────────────────────────────────────────────

// ID names one machine serving one model.
//
// Both halves are needed and neither is enough. The same endpoint serves many
// models at different speeds, and the same model is served by endpoints that
// have nothing in common, so a belief keyed on either alone is a belief about
// an average nobody ever waits on.
//
// Lane is spelled exactly as the wire spelled it — the `provider` field of a
// streamed chunk, the `provider_name` of a sheet row. Nothing in this package
// knows a vendor's name; every name in it arrived from the wire a moment ago.
type ID struct {
	Model string
	Lane  string
}

// Zero reports whether the id names nothing. A sighting that could not say who
// served it carries a zero id, and a zero id is never written to the ledger:
// crediting an anonymous measurement to some lane is how a ledger learns a
// fact about a machine that was not involved.
func (id ID) Zero() bool { return id.Model == "" || id.Lane == "" }

// String is the key form, "model|lane". It is a map key and a file key and is
// never shown to a person.
func (id ID) String() string { return id.Model + "|" + id.Lane }

// Facts are what a lane IS, as opposed to how fast it has lately been.
//
// They are the gate's evidence and they are deterministic: a lane that cannot
// take a tool call is dropped from the candidate set outright, never sampled
// and found wanting. A gate drop is never a sampled event — a "fast" lane that
// silently drops the tool call is a wrong answer rather than a fast one.
//
// Prices are US DOLLARS PER TOKEN, the unit the router publishes, and not the
// per-million figure a person reads. The conversion belongs to whatever draws
// it, in one place, so that two surfaces cannot disagree about a factor of a
// million.
type Facts struct {
	// Tools is whether this lane honours a tool call.
	Tools bool
	// Quant is the weight precision the lane serves at, spelled as the sheet
	// spells it ("fp8", "bf16", "fp4"), empty when the sheet did not say.
	Quant string
	// MaxOut is the longest answer the lane will write, and Context the longest
	// conversation it will read. Zero is "the sheet did not say", never "none".
	MaxOut  int
	Context int
	// Uptime5m is the share of the last five minutes the lane was answering, 0
	// to 100. It is the freshest availability figure the sheet carries and it
	// is what lets a lane earn its way back without a penalty box.
	Uptime5m float64
	// PriceIn, PriceOut and PriceCache are the lane's own tariff per token for
	// a fresh prompt token, an output token, and a token read back out of its
	// prompt cache. THEY ARE THE LANE'S AND NOT THE MODEL'S: an endpoint's
	// tariff is its own, and the model id's published list price is a figure
	// none of them is obliged to match.
	PriceIn    float64
	PriceOut   float64
	PriceCache float64
	// Caches is whether the lane holds a prompt prefix between requests. It is
	// what makes price path-dependent: the cheapest lane on the sheet is not
	// the cheapest lane for a request whose prefix another lane already holds.
	Caches bool
}

// Row is one line of the sheet: the public, thirty-minute account of a lane.
//
// The percentiles are the ROUTER'S, over everybody's prompts, and they are the
// prior rather than the belief. TTFT is in MILLISECONDS and Rate in TOKENS PER
// SECOND, which is how the sheet publishes them; the whole package stays in
// those units so that no seam has to remember a conversion.
//
// A row with a p50 and a p90 is enough to fit a log-normal prior — the median
// is the location and the spread between them is the scale — which is why the
// four percentiles are named fields rather than a slice nobody can index
// correctly twice.
type Row struct {
	ID    ID
	Facts Facts

	TTFTp50 float64
	TTFTp75 float64
	TTFTp90 float64
	TTFTp99 float64

	Ratep50 float64
	Ratep75 float64
	Ratep90 float64
	Ratep99 float64
}

// Known reports whether the row carries enough to fit a prior. A row whose p50
// is zero is a lane the sheet published no timing for, and a prior invented
// from it would be this process refusing lanes on a number nobody measured.
func (r Row) Known() bool { return r.TTFTp50 > 0 && r.Ratep50 > 0 }

// ── WHAT WE MEASURED OURSELVES ──────────────────────────────────────────────

// Sighting is one answer, timed.
//
// It is the ledger's unit. TTFT and Gen are separated because they fail for
// different reasons — queueing before the first token, contention during the
// writing — and timing them together would price a warm lane behind a long
// prompt as a slow one.
type Sighting struct {
	ID ID
	// TTFT is the wait before the first token, and Gen is the window from the
	// first token to the last. Gap is the widest quiet stretch inside the
	// answer, which is how a lane that assembles the reply server-side and
	// delivers it in lumps tells on itself.
	TTFT time.Duration
	Gen  time.Duration
	Gap  time.Duration
	// Tokens is what the answer was worth in output tokens. PromptTokens and
	// CachedTokens are what it cost to be read, and the second of them is the
	// only evidence there is that a lane really held our prefix.
	Tokens       int
	PromptTokens int
	CachedTokens int
	// Probe marks a sighting bought on purpose: a one-token request sent while
	// somebody was still typing, which measures exactly our path to this lane
	// right now. It is a first-token measurement and NEVER a rate one — an
	// answer one token long rates the handshake.
	Probe bool
	At    time.Time
}

// Rate is output tokens per second over the generation window, zero when the
// answer was too short or too quick to rate. It is derived here rather than
// carried so that two callers cannot compute it two ways.
func (s Sighting) Rate() float64 {
	if s.Tokens <= 0 || s.Gen <= 0 {
		return 0
	}
	return float64(s.Tokens) / s.Gen.Seconds()
}

// Outcome is what became of an answer: whether the caller could use it.
//
// It is the quality axis, and it is deliberately thin. The signals that fill it
// in are ones the harness already produces and that are attributable to the
// lane that served the call — a tool call the decoder refused, a reply that
// stopped on length below its own ceiling, an empty answer, a lane that claims
// caching and returned no cached tokens. Reason is for the log and never for a
// person; it is a short machine-readable word.
type Outcome struct {
	ID       ID
	Accepted bool
	Reason   string
	At       time.Time
}

// ── WHAT A WAIT COSTS A PERSON ──────────────────────────────────────────────

// ReadRate is how fast a person reads, in tokens per second.
//
// It is the ceiling on the value of throughput. Text that a person is reading
// as it arrives cannot be delivered usefully faster than they can take it in,
// so above this rate two lanes are the SAME SPEED to the person and the cheaper
// one wins. Eighteen is a normal adult reading pace of roughly 250 words a
// minute at about four tokens to three words; it is a property of people and
// not a dial, which is why it is stated once, here.
const ReadRate = 18.0

// PerceivedSeconds is how long an answer feels, in seconds.
//
// Hidden tokens — reasoning, tool-call JSON, anything a person never reads —
// are worth their full rate, because every one of them is pure waiting. Visible
// tokens are worth at most the reading rate. That single distinction is what
// makes a 75 tok/s lane and a 58 tok/s lane equal for a talk turn and different
// for a tool loop, and it is why this is the objective the chooser minimises
// rather than wall-clock time.
//
// ttft is in SECONDS and rate in TOKENS PER SECOND. A rate of zero or less is
// a lane that never finishes, and it is reported as such rather than as a large
// finite number somebody might then compare.
func PerceivedSeconds(ttft, rate float64, visible, hidden int) float64 {
	if rate <= 0 {
		return math.Inf(1)
	}
	return ttft + float64(hidden)/rate + float64(visible)/math.Min(rate, ReadRate)
}
