package lane

import (
	"context"
	"time"
)

// ── THE FIVE SEAMS ──────────────────────────────────────────────────────────
//
// Five things happen to a lane and each of them is one interface here. They are
// interfaces rather than a struct with five methods for the reason the response
// boundary is a registry rather than a switch: a second implementation of any
// one of them — a sheet read from a file for a bench, a chooser that only ever
// answers with a pin, a prober that is switched off — has to be writable
// without any other seam learning a new name.
//
// The concrete implementations are constructed in exactly one place
// ([Default]), and a structural test in this directory fails the build when
// they are constructed anywhere else.

// Sheet is the public account of who serves a model and how fast.
//
// IT HAS TWO HALVES ON PURPOSE. [Sheet.Rows] reads what is already in memory
// and can be called from anywhere, including the encoder that runs immediately
// before a send. [Sheet.Refresh] goes to the network and MAY NEVER BE CALLED
// FROM A SEND PATH — it belongs to a background beat and to nothing else. A
// missing sheet is "no prior, use the belief alone"; it is never a reason to
// make somebody wait. `internal/provider/lane_law_test.go` fails the build when
// any non-test file in the transport package so much as names Refresh.
type Sheet interface {
	// Rows is what is known about model's lanes right now, from memory, with
	// no clock and no connection. It returns nothing when nothing has been
	// fetched, which is a normal state and not an error.
	Rows(model string) []Row
	// Refresh fetches model's sheet and replaces what Rows returns. It is
	// called from the beat.
	Refresh(ctx context.Context, model string) error
}

// Ledger is what this process has measured and what it now believes.
//
// It is the only mutable state in the package. Everything that writes to it is
// an observation — a finished stream, a probe, an answer the caller could not
// use, a sheet row — and everything that reads from it gets a [Belief] that has
// already been aged to the moment it is asked about.
type Ledger interface {
	// Note folds one timed answer in.
	Note(Sighting)
	// NoteOutcome folds in whether an answer could be used.
	NoteOutcome(Outcome)
	// Belief is what is believed about one lane, false when nothing is.
	Belief(ID) (Belief, bool)
	// Beliefs is every lane believed in for one model, in no promised order.
	Beliefs(model string) []Belief
	// Prime folds a sheet row in as a PSEUDO-OBSERVATION worth 1/k of a real
	// sighting. The sheet is a thirty-minute aggregate over everybody's
	// prompts and our own measurements are about our prompts from our region:
	// both are evidence, neither is truth, and k is how much less the public
	// one weighs. It is also what gives a lane nobody has used a prior, so
	// that nothing is blind on the first call of a process.
	Prime(row Row, k float64)
}

// Store is where a belief sleeps between processes.
//
// A NEW PROCESS STARTS FROM YESTERDAY'S BELIEF, AGED. That is the whole point
// of persisting: [Posterior.Predict] widens a stale belief until it is worth
// about as much as the sheet, so what survives a restart is "mostly the prior,
// a little memory" rather than a claim about a machine that was busy last
// Tuesday.
//
// IT IS TWO FILES AND [Store.Load] IS THE COMPACTED HALF. `~/.aforge/v3/lanes.json`
// ([StorePath]) holds the state as of the last compaction, and
// `~/.aforge/v3/lanes.log` (journal.go) holds one appended line per
// observation since. Load answers with the state alone; replaying the journal
// over it is [Journal]'s, and the ledger does both. The split is what lets two
// processes share an afternoon: last-writer-wins over one file cannot merge the
// levels of a hierarchy that both of them folded evidence into, so neither of
// them writes the whole belief on the send path and the compaction that does is
// taken under the exclusive lock.
type Store interface {
	Load() ([]Belief, error)
	Save([]Belief) error
}

// ── ASKING FOR A LANE ───────────────────────────────────────────────────────

// Request is everything the chooser is allowed to know.
//
// It carries its own Now because the chooser is PURE: same request, same
// beliefs, same answer. That is what makes the choice testable at all, and a
// clock read inside it would make every test of it a test of the machine it ran
// on. A structural test fails the build when a choosing file names time.Now.
type Request struct {
	// Model is the model as it was asked for, normalized.
	Model string
	// PromptTokens is roughly how long the conversation is. It is the noise
	// term on a first-token measurement — most of a long prompt's wait is
	// prefill, which is not the lane's fault — and it is half of the price.
	PromptTokens int
	// Prefix identifies the conversation whose prompt cache is at stake, empty
	// when there is none. A lane that served this prefix recently probably
	// still holds it, and a switch forfeits it: the score pays that forfeit
	// explicitly, which is also what stops the router flapping between two
	// lanes that are otherwise equal.
	Prefix string
	// Visible is how many tokens of this answer a person will read and Hidden
	// how many they will not — reasoning, tool-call JSON, anything that is
	// pure waiting. The split is what [PerceivedSeconds] needs and it is the
	// difference between a talk turn and a tool loop.
	Visible int
	Hidden  int
	// Tools says the request carries tools, and MaxTokens the ceiling on the
	// answer. Both are gate facts: a lane that cannot take a tool call or
	// cannot write that many tokens is dropped, never scored.
	Tools     bool
	MaxTokens int
	// ValueOfTime is λ, IN SECONDS PER DOLLAR: how many seconds of waiting one
	// dollar is worth buying out of. It is a property of who is waiting and
	// what their wait costs, and it is computed per request from attention and
	// slack rather than set in a config — a person watching an empty line is
	// worth about ninety seconds to the dollar, a node with slack off the
	// critical path is worth nothing and price wins outright.
	//
	// ZERO IS PRICE ONLY. It is the honest reading of "nobody is waiting" and
	// it is what the routing row's `price` word sets for everything.
	ValueOfTime float64
	// QualityNeed is the share of answers that must come back usable for a
	// lane to stay in the candidate set — higher for work than for talk. It is
	// a GATE and never a weight.
	QualityNeed float64
	// Horizon is roughly how many more calls this session will make. It scales
	// exploration: a three-call session should never explore and a
	// five-hundred-call swarm should explore early, and Thompson sampling on
	// its own is blind to the difference.
	Horizon int
	// Now is the moment the request is being made. See the note on the type.
	Now time.Time
}

// Scored is one candidate lane with the numbers the choice was made on.
//
// It exists so that the choice can EXPLAIN ITSELF with the same numbers it
// decided on. A picker that recomputed them would be a second opinion nobody
// asked for, and the first time the two drifted the explanation would be a
// polite fiction.
//
// SCORE'S UNIT FOLLOWS λ, and lower is better either way. With somebody waiting
// (λ > 0) a dollar is worth λ seconds by construction, the two terms are
// commensurable, and the score is SECONDS — the perceived wait plus the price
// converted through λ. With nobody waiting (λ = 0) a second is worth nothing,
// dividing by λ is a division by zero dressed up as a preference, and the score
// is DOLLARS: what this request is expected to cost on this lane, with the
// perceived wait left to break the ties. The two are never compared with each
// other, because one request has one λ.
//
// TTFT is milliseconds, Rate tokens per second, Price the dollars this whole
// request is expected to cost on this lane, Quality the believed share of
// usable answers.
type Scored struct {
	ID      ID
	Score   float64
	TTFT    float64
	Rate    float64
	Price   float64
	Quality float64
}

// Choice is what one request should ask the router for.
//
// Order is a preference and Only is a demand: a pin sends Only, and everything
// else sends Order with fallbacks left on, because a slow answer beats no
// answer. Ignore names the lanes we are SURE about rather than the ones we are
// merely unlucky with, and it expires with the belief rather than on a timer.
//
// A zero Choice is "no opinion", which is a real answer and the right one when
// the ledger has never seen this model. The transport sends what it would have
// sent before.
type Choice struct {
	Order  []string
	Only   []string
	Ignore []string
	// IT SAYS NOTHING ABOUT TIME, and the absence is the law. Routing and
	// waiting are two questions and they must never share one nil: this answers
	// WHICH LANE, and [control.Plan] — built for every token-generating call,
	// whether or not any preference was expressed — answers WHEN TO ACT. They
	// were one value once, so a ledger that had never heard of a model produced
	// no routing opinion AND no clock, and the request that most needed a
	// deadline was the one request that got none. [PlanFor] is the other half
	// and `law_test.go` is what keeps them apart.
	//
	// Frontier is the candidate set after the gate and the Pareto prune — the
	// three to five lanes actually worth choosing between — with the numbers
	// each was scored on. It is what the picker's `auto` row says out loud.
	Frontier []Scored
	// Why is one short sentence in a person's own words, empty when there is
	// nothing honest to say. It is shown under the cursor in the picker.
	Why string
}

// Empty reports whether the choice asks for nothing at all.
func (c Choice) Empty() bool {
	return len(c.Order) == 0 && len(c.Only) == 0 && len(c.Ignore) == 0
}

// Chooser turns a request into a preference.
//
// It is PURE — no clock, no connection, no file — and [Request.Now] is how the
// time gets in. Everything it needs to know about the world it reads from the
// ledger and the sheet it was built with.
type Chooser interface {
	Choose(Request) Choice
}

// ── WATCHING ONE ANSWER ARRIVE ──────────────────────────────────────────────

// Verdict is what the watch says about a stream in flight.
//
// Reason is a short machine word for the log — it is never shown to a person.
// The only sentence a person sees about a slow stream is shown while something
// is already being done about it.
type Verdict struct {
	Hedge  bool
	Reason string
}

// Prober buys freshness before it is needed.
//
// The sheet is half an hour old and our own belief may be minutes old, but we
// know seconds in advance that a request is coming: the composer got a
// keystroke. A one-token request to the top of the frontier costs about two
// hundredths of a cent, warms the connection so that TLS is out of the real
// first-token wait, and lands as a sighting with a small R because it measured
// exactly our path, right now.
//
// It is fire-and-forget by construction: nothing waits for a probe, and a probe
// that fails teaches the ledger what a failure teaches it and nothing more.
type Prober interface {
	Probe(ctx context.Context, model string, lanes []string)
}
