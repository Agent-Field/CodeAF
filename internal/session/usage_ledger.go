package session

// THE USAGE LEDGER: WHAT THIS MACHINE SPENT, BY DAY, BY MODEL, ON WHAT.
//
// Every figure a spend page wants already existed and none of it was reachable.
// One `journalUsage` line is written per model call (sessionfile.go) and it
// carries the model, the role, the calls, the four token counts, the cost and a
// timestamp — model, cost and time coexisting on exactly one record in the whole
// program. But that record lives INSIDE one conversation's transcript, and the
// replay arm that reads it sums the numbers into a flat [Usage] and throws the
// model, the role and the timestamp away. So "what did opus cost me this month"
// could only be answered by opening every transcript on the machine — the exact
// read `Meta.SpentUSD` was invented to avoid — and "what did I spend on Tuesday"
// could not be answered at all, because a conversation's own spend is a lifetime
// scalar with no day in it.
//
// This is the second write of the same fact, into one machine-wide append-only
// file, keyed by day. It is GLOBAL where a transcript is per-conversation, for
// artifacts.jsonl's reason: "what has this machine been spending on" is a
// cross-project question, and a per-project answer to it is not an answer.
//
// ── SIX RULES ──
//
//   - ONE ROW PER CALL, WRITTEN AS THE CALL IS DECODED. The provider's usage
//     block is in hand at exactly one moment — [Agent.addUsage], the same line
//     that banks the money into the session's own meter — and the row goes out
//     from there. It used to be written at the END OF A TURN instead
//     ([Agent.sealTurn]), which meant a turn that never sealed — interrupted,
//     stopped, crashed, or simply still running while somebody looked — was
//     money the meter had and this file never got. One measured chat held
//     $0.087 of it, every call of a final interrupted turn, invisible to every
//     surface but the status line (issue #269). [Agent.bank] is now the ONE
//     door: nothing can move the meter without offering this file a row, so the
//     two cannot come apart. The turn's seal still stamps the TRANSCRIPT — the
//     duration and the turn's own shape are facts about a turn — and writes
//     nothing here.
//   - IT IS WRITTEN WHERE THE CALL WAS MADE, AND A FOLD IS NOT A CALL. A task
//     node journals its own turns and then its whole tally is folded into the
//     conversation that spawned it (task_run.go's [Agent.foldTaskUsage]), which
//     is right for a session's own books and would be double counting here. The
//     fold has its own door ([Agent.addFoldedUsage]) that writes no ledger line,
//     and this file's totals are therefore each call once. EVERY CHILD AGENT IS
//     UNDER THIS RULE, not only a task node: a fork's hand (fork.go's
//     [Agent.foldHandUsage]) and an adaptive run's worker (orchestrate.go's
//     [orchestrateExec.spend]) each journal their own calls here and each fold
//     home silently. Both folded through the auxiliary door until issue #168,
//     and the day this file's rail reads was double on every one of them.
//   - IT NEVER BLOCKS A TURN, AND THE TURN'S OWN GOROUTINE NEVER TOUCHES THE
//     DISK. [RecordUsage] serializes the row and hands it to a bounded queue
//     that one background writer per ledger drains ([usageWriter]); a full
//     queue drops the row rather than waiting on it, and so does a write that
//     fails. A home directory on a stalled mount therefore costs a spending
//     record and never a person's turn — which the older shape, an
//     open-write-close under a process-wide mutex, could not promise. AND THE
//     DROP IS COUNTED WHERE IT HAPPENS ([UsageDrops]), because a gap nobody can
//     see is a bill that reads as smaller than it was: the spend surfaces say
//     how many records went missing rather than quietly under-reporting.
//   - A LINE THAT SPENT NOTHING IS NOT WRITTEN. The emptiness law applied to a
//     file: a call whose usage block or later receipt carries no cost and no
//     tokens leaves no priced row — an explicit unbilled marker records a
//     missing receipt separately, so a day with no line is a day nothing was
//     measured as spent, rather than a day whose rows all say zero.
//   - A CALL THE WIRE NEVER PRICED IS ASKED ABOUT LATE, NEVER GUESSED. A cut
//     stream that named its generation can be matched to the provider's own
//     receipt after the turn has already moved on. A found receipt enters
//     through [Agent.bank] and marks its row reconciled; no id, no route, or no
//     receipt leaves an unbilled marker without invented money and moves
//     [UnbilledCalls] as well. The
//     fetch is background work in internal/provider, so neither the request nor
//     this ledger may make a reply wait.
//   - IT IS READ THROUGH A CACHE THAT READS THE TAIL. Home's clock beats every
//     three seconds and this file grows by a line per call, so a reader that
//     re-parsed the whole file whenever it changed would re-parse it after every
//     turn forever. [UsageCache] keeps what it has parsed and reads only what
//     was appended since ([UsageCache.Read]).
//
// WHAT IT DOES NOT HOLD, said plainly, because a page must not imply otherwise:
// the four-way token split (input and output are kept, the cache share is not —
// the journal this line's session id names has it), which of the five router
// slots a call ran under (nothing records that anywhere; `Role` here is
// internal/roles' auxiliary name, and only three auxiliary calls in the whole
// program name themselves at all), and any title for a task or a standing item —
// only their ids, which a page joins against the task index and the standing
// store it is already reading.

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// UsageLedgerName is the file, under the v3 home directory
// (~/.aforge/v3/usage.jsonl). It is a name beside a path function rather than a
// literal at every call site, for [ArtifactsIndexName]'s reason: two spellings
// of one path are two ledgers with half a person's spending in each.
const UsageLedgerName = "usage.jsonl"

// usageDayLayout is the LOCAL calendar day a line belongs to, spelled exactly
// as internal/standing's daily ledger spells its own file names
// (standing.go's LedgerPath). A day is the unit a person asks about — "what did
// Tuesday cost" — and it is local because their Tuesday is, so a line carries
// the day it was made in beside the instant it was made at, and no reader has
// to re-derive a calendar from a timestamp in some other zone.
const usageDayLayout = "2006-01-02"

// UsageLedgerPath is this machine's ledger, resolved through internal/home so
// AFORGE_HOME moves it with everything else (Decision 26 — one home, one seam).
// It is the fallback the engine writes to; a caller with a path of its own —
// a test, a second brain on one laptop — hands one to [RecordUsage] instead.
func UsageLedgerPath() string { return home.Join("v3", UsageLedgerName) }

// UsageLine is one model call as the ledger remembers it.
//
// Every field is a fact somebody asked for by name on the spend page, and there
// is nothing here that is not: which day, which model, what for, how many calls,
// how many tokens, how much money, and the three ids that say what the money was
// spent ON — a conversation, a piece of work, a standing promise.
type UsageLine struct {
	// At is the instant the call was journaled, RFC3339 with nanoseconds.
	At time.Time `json:"at"`
	// Day is At's LOCAL calendar day, "2006-01-02". It is written down rather
	// than derived on read because the reader may be a different process in a
	// different zone, and a day that moves depending on who is asking is not a
	// day (see [usageDayLayout]).
	Day string `json:"day"`
	// Model is what answered, and empty where nobody said. It is the id the
	// provider knows, not a pretty name: a page that wants "opus 4.1" makes that
	// word itself, from one place, the way every other surface does.
	Model string `json:"model,omitempty"`
	// Role is WHAT the call was for — "title", "taskname", "intake" — on the
	// three auxiliary calls in the program that name themselves, and empty on
	// every other line including every call a turn makes for itself. It is internal/roles'
	// vocabulary and NOT the five router slots (execution, conversation,
	// verification, naming, planning): nothing in the program records which slot
	// a call ran under, and a page that labelled this column with those words
	// would be inventing the join.
	Role string `json:"role,omitempty"`
	// Calls is how many provider requests this line covers, and it is ONE. The
	// field is kept rather than dropped because rows written before issue #269
	// carry a whole turn's worth on one line — an observed `calls: 41` — and a
	// reader summing a year of this file has to be able to add those honestly.
	// Nothing this build writes says anything but 1.
	Calls int `json:"calls,omitempty"`
	// Input and Output are the tokens. The cache split is deliberately not here:
	// this file answers "how much and on what", and the four-way breakdown with
	// the cached share in it is in the journal the session id names.
	Input  int `json:"in,omitempty"`
	Output int `json:"out,omitempty"`
	// USD is what it cost, and zero means nobody could price it rather than that
	// it was free — the same reading [TaskIndexEntry.Cost] has.
	USD float64 `json:"usd,omitempty"`
	// Reconciled marks a row whose figures came from the provider's own receipt
	// after the stream ended without a usage block. It is additive and omitted
	// from every ordinary row and every row written before this field existed.
	Reconciled bool `json:"reconciled,omitempty"`
	// Unbilled marks a missing provider receipt, never a measured zero price.
	Unbilled bool `json:"unbilled,omitempty"`
	// Empty marks a paid request that returned no answer.
	// The role beside it names the reflex, so the row remains useful even to a
	// reader that does not know this build's aggregate counters.
	Empty bool `json:"empty,omitempty"`
	// Session is the 16-hex id of the conversation the call was made in. For a
	// piece of work it is the NODE's own journal id and not the conversation
	// that asked for it, which is why Task sits beside it: the pair is what
	// identifies where the money went.
	Session string `json:"session,omitempty"`
	// Task is the id of the node this call was made inside, and empty in a
	// conversation. It is the node's id within its session, exactly as
	// [TaskIndexEntry.ID] is, so the two join.
	Task string `json:"task,omitempty"`
	// Root is the CONVERSATION the work this call was made inside belongs to,
	// and it is empty on a conversation's own line — where Session already names
	// it — and on a standing firing no conversation asked for.
	//
	// IT IS THE ONE FIELD THAT MAKES A FAMILY ADDABLE. Session on a node's line
	// is the NODE's journal, which is a file nobody outside the family has heard
	// of, and Task is a small integer that restarts with every conversation; so
	// with those two alone, "what has this conversation's work cost so far"
	// could not be asked of this file at all — it could only be waited for,
	// until each node closed and its tally was folded into the conversation's
	// own books ([Agent.foldTaskUsage]). It is written on every agent inside a
	// family, at every depth and on the check and repair rounds as well — and on
	// the hands a turn forked (fork.go), which keep no journal of their own and
	// whose lines therefore named nothing joinable at all before it — so a reader
	// that sums it gets the whole subtree.
	//
	// A FOLD STILL WRITES NO LINE, so a closed node's calls are counted here
	// exactly once — under the node that made them — and never again under the
	// conversation they were folded into.
	//
	// It is ADDITIVE, and absence is ordinary: every line written before this
	// field existed decodes without it, which reads as "this is not a family's
	// line", and every one of them is a conversation's or a standing item's.
	Root string `json:"root,omitempty"`
	// Standing is the id of the standing item whose firing made this call, and
	// empty everywhere else. A firing is InTask and may also carry a Task id;
	// [UsageBySubject] prefers this one, because a person recognises the promise
	// they made long before they recognise the run it spawned.
	Standing string `json:"standing,omitempty"`
	// Workspace is the project root the call was made against — what a page
	// groups by, and empty for a conversation held nowhere in particular.
	Workspace string `json:"workspace,omitempty"`

	// ── what the lane that served it did (docs/ARCHITECTURE.md, Decision 10)
	//
	// A model id is an address and the LANE is the machine behind it. One id is
	// served by a dozen endpoints that differ by 7× on the wait before the
	// first token and by 12× on how fast they write, at roughly the same price,
	// so a spending row that names only the model cannot answer the question a
	// person asks after a slow afternoon: was it the model, or was it the
	// machine we happened to be routed to. These five fields are that answer,
	// and they are the only ones in this struct that describe HOW rather than
	// HOW MUCH.
	//
	// EVERY ONE OF THEM IS omitempty AND EVERY ZERO MEANS "NOBODY SAID". A call
	// to an endpoint that is not a router names no lane; a call that was not
	// timed has no first-token figure; a call that was never hedged has no
	// waste. The emptiness law is the whole reason they can be added to a file
	// that already has a year of rows in it: an old row decodes with all five
	// absent, which reads as "not known", which is the truth about it.

	// Lane is the machine that answered, spelled exactly as the router spelled
	// it — the `provider` field of a streamed chunk. It is the vendor's own
	// name and it arrived from the wire; nothing in this build holds a list of
	// them.
	Lane string `json:"lane,omitempty"`
	// TTFTms is the wait before the first token, in milliseconds. It is the
	// half of a call's duration a person actually feels: the rest of the answer
	// arrives while they are reading.
	TTFTms int64 `json:"ttft_ms,omitempty"`
	// TPS is output tokens per second over the generation window — the first
	// token to the last, and NOT the whole call, because dividing an answer by
	// a duration that begins with a queue is how a warm lane behind a long
	// prompt gets recorded as a slow one.
	TPS float64 `json:"tps,omitempty"`
	// Hedged marks a call that was rescued: it went slow, a second request went
	// to another lane, and one of the two came back first. It is on the row
	// because a hedge is the one thing in this build that can spend money
	// twice, and a bill that cannot be told apart from an ordinary one is a
	// mechanism nobody can audit.
	Hedged bool `json:"hedged,omitempty"`
	// HedgeWasteUSD is what the LOSING half of that pair cost. Cancelling a
	// stream stops the billing on most lanes and on some it does not, so this
	// is zero on a clean rescue and a real figure on a lane that charged for
	// the tokens it had already written. It is the number the hedge budget is
	// judged on: the mechanism is worth having exactly while this stays small
	// beside the seconds it bought.
	HedgeWasteUSD float64 `json:"hedge_waste_usd,omitempty"`
}

// usageFromResponse folds what a finished call taught us about its lane onto
// the line that records what it cost.
//
// IT TAKES PLAIN VALUES AND NOT A PROVIDER TYPE, deliberately. This file is the
// engine's ledger and the transport is `internal/provider`; a struct passed
// between them would be a third place that has to agree about units, and the
// units are exactly where this has gone wrong before (a per-token price read as
// a per-million one). So: ttft and gen are durations, output is a token count,
// and the only arithmetic here is the one derivation — tokens per second over
// the generation window — which lives here so that two callers cannot compute
// it two ways.
//
// THE EMPTINESS LAW IS ENFORCED HERE RATHER THAN TRUSTED. A zero duration
// writes no first-token figure, an answer too short or too quick to rate writes
// no rate, and a hedge that wasted nothing writes no waste. That is what keeps
// an ordinary row exactly as wide as it was before lanes existed, and it is why
// a reader may take any figure that IS on a row as something somebody measured.
func usageFromResponse(line UsageLine, lane string, ttft, gen time.Duration, output int, hedged bool, hedgeWaste float64) UsageLine {
	line.Lane = strings.TrimSpace(lane)
	if ttft > 0 {
		line.TTFTms = ttft.Milliseconds()
	}
	if output > 0 && gen > 0 {
		line.TPS = float64(output) / gen.Seconds()
	}
	line.Hedged = hedged
	if hedgeWaste > 0 {
		line.HedgeWasteUSD = hedgeWaste
	}
	return line
}

// ── WHAT ONE AGENT'S OWN CALL WAS SERVED BY ─────────────────────────────────
//
// The five fields above are known at two different moments and the row is
// written at the second. The first-token wait is known while the answer is
// still arriving, spread across chunks that reach the loop one at a time; the
// lane and the hedge are known when the answer lands, and the row goes out in
// that same breath ([Agent.addUsage]). So something has to hold what was seen
// ACROSS THE STREAM until the answer closes it, and that is the whole of what a
// witness is.
//
// IT BELONGS TO ONE AGENT AND IS FILLED ONLY BY THAT AGENT'S OWN REQUESTS.
// `internal/provider` folds every finished stream into a ledger keyed by MODEL
// and shared by every agent in this process ([provider.LastServed]), so reading
// a first-token wait back out of THAT would credit one conversation's lane to
// another conversation's row the moment two of them work at once — the invented
// measurement docs/ARCHITECTURE.md Decision 10 forbids. A witness cannot make
// that mistake, because nothing but this agent's own stream ever writes to it.
//
// AND IT HOLDS THE MOST RECENT ANSWER AND NOTHING ELSE, exactly as
// [provider.ServedEndpoint] does and for its reason. A turn is many requests
// and one row, so all five figures describe the SAME request — the last one —
// rather than a lane from one, a wait from another and a rate from a third.

// laneFacts is what a finished request taught us about the machine that served
// it, in the plain values [usageFromResponse] writes a row from. It is plain
// values and not a provider type for that function's own stated reason: the
// units are where this has gone wrong before, and a struct shared with the
// transport would be a third place that has to agree about them.
type laneFacts struct {
	Lane   string
	TTFT   time.Duration
	Gen    time.Duration
	Output int
	Hedged bool
	Waste  float64
}

// hedgeSeen is one call's [provider.HedgeReport] read into plain values, so
// that everything below this line is drivable without a transport.
type hedgeSeen struct {
	lane   string
	hedged bool
	waste  float64
	// fault says the watch judged the failure to be the PATH rather than the
	// machine, which is the one verdict that must not reach a lane's row as
	// timing.
	fault bool
}

// readHedge reads one call's hedge report, and answers correctly for the nil
// report every call in a build with no watch behind it has.
//
// THE LANE IS THE WINNER WHEN THERE WAS A RACE AND THE SERVED ENDPOINT
// OTHERWISE. The report names a winner only on a call that actually hedged
// (internal/provider's hedgeRace.settle writes the pair under that condition
// alone), and on every ordinary call the endpoint slot the turn already stamps
// is the same fact read off the same chunk. So one of the two always names the
// machine that answered and neither of them is a guess.
func readHedge(report *provider.HedgeReport, served string) hedgeSeen {
	seen := hedgeSeen{
		lane:   strings.TrimSpace(served),
		hedged: report.Hedged(),
		waste:  report.Waste(),
		fault:  report.PathFault(),
	}
	if winner, _ := report.Lanes(); strings.TrimSpace(winner) != "" {
		seen.lane = strings.TrimSpace(winner)
	}
	return seen
}

// responseOutput is how many tokens an answer wrote, and nothing at all where
// the provider counted none — a cut stream, a refused call, an answer that came
// back with no usage frame behind it. It is the denominator of the rate, so a
// guess here would be a figure on the row that nobody measured.
func responseOutput(response *ai.Response) int {
	if response == nil || response.Usage == nil {
		return 0
	}
	return response.Usage.CompletionTokens
}

// laneWitness is one agent's account of how its own last request was served.
type laneWitness struct {
	// mu is the witness's own, and it is deliberately not the agent's: this is
	// written from the stream goroutine while the turn holds a.mu for its own
	// state transitions, and a measurement is never worth a lock ordering.
	mu sync.Mutex
	// began is when the request went out; first and last are the arrival of the
	// first and the latest chunk of the answer to it. They are the session's OWN
	// clock on its OWN stream, which is the only clock in this program that can
	// time one agent's call without borrowing another agent's.
	//
	// NOTHING IS HELD BEYOND THEM. The witness used to keep the finished row
	// until the turn's seal came for it, because the seal was where the ledger
	// was written; the row is now written on the call itself
	// ([laneWitness.answered] hands it straight to [Agent.addUsage]), so there is
	// no measurement waiting here for a later writer to mis-file.
	began time.Time
	first time.Time
	last  time.Time
}

// reset forgets the turn before. A turn is the scope a measurement is true in,
// so a fresh one starts knowing nothing rather than knowing what the last one
// happened to be served by.
func (w *laneWitness) reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.began, w.first, w.last = time.Time{}, time.Time{}, time.Time{}
}

// sent starts the first-token clock, and it is called where the request
// actually goes out and nowhere else: a clock started any earlier would time
// this package's own preparation and write it down as the lane's wait.
func (w *laneWitness) sent(now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.began, w.first, w.last = now, time.Time{}, time.Time{}
}

// token records one chunk arriving. The first closes the first-token wait and
// every one after it extends the generation window — the two are separated
// here for [UsageLine.TPS]'s stated reason: a rate over a window that begins
// with a queue records a warm lane behind a long prompt as a slow one.
func (w *laneWitness) token(now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.began.IsZero() {
		return
	}
	if w.first.IsZero() {
		w.first = now
	}
	w.last = now
}

// answered folds one finished request's lane and hedge onto what was timed.
//
// A PATH FAULT IS NOT A LANE'S FAULT. Where the watch judged the failure to be
// the path rather than the machine, the seconds are a fact about somebody's
// network and there is no fact about the endpoint in them at all — so the
// timing is dropped here exactly as internal/provider's [streamWatch.sighting]
// refuses to fold it into a belief. What the call spent and whether it hedged
// are still true and stay.
// IT HANDS THE ROW BACK AND KEEPS NOTHING, which is what makes a measurement
// belong to exactly one call. Both readers want it in the same breath: the
// ledger row this call is about ([Agent.addUsage]) and the status line, which
// has to hear who answered now — an answer whose lane arrives a minute later is
// a fact about a turn nobody is looking at any more.
func (w *laneWitness) answered(seen hedgeSeen, output int) laneFacts {
	w.mu.Lock()
	defer w.mu.Unlock()
	row := laneFacts{Lane: seen.lane, Hedged: seen.hedged, Waste: seen.waste}
	if !seen.fault && !w.began.IsZero() && !w.first.IsZero() {
		row.TTFT = w.first.Sub(w.began)
		row.Gen = w.last.Sub(w.first)
		row.Output = output
	}
	w.began, w.first, w.last = time.Time{}, time.Time{}, time.Time{}
	return row
}

// ── the writer ──────────────────────────────────────────────────────────────
//
// One goroutine per ledger path, holding one descriptor, draining one bounded
// queue. It is internal/history's shape and it is here for internal/history's
// reason — "capture must never block the input path" — with one difference that
// matters: history batches on a ticker because a person can type faster than a
// disk, and a ledger row is written at most once per model call, so this writer
// wakes on the row itself and there is nothing to batch.

// usageQueueDepth is how many serialized rows one ledger's queue holds before a
// row is DROPPED rather than waited on. Two hundred and fifty-six is far more
// than the handful of calls that can be in flight at once in this process, so
// the queue only fills when the disk behind it has stopped answering — which is
// exactly the case the drop exists for.
const usageQueueDepth = 256

// usageFlushLimit is the longest [FlushUsage] may wait for the rows in front of
// it, across every ledger it is flushing. Two seconds is far longer than a disk
// that is answering needs — a flush costs microseconds on a working mount — and
// far shorter than a person will wait for a terminal to come back from a mount
// that is not.
const usageFlushLimit = 2 * time.Second

// usageWrite is one thing a writer is asked to do: append a row, or — where the
// row is nil — close `done` once everything queued before it has been written.
// The flush travels through the SAME queue as the rows, which is what makes it
// an answer about them rather than a race with them.
type usageWrite struct {
	line []byte
	done chan struct{}
}

// usageWriter is one ledger file's background writer.
type usageWriter struct {
	queue chan usageWrite
}

// usageDropped is how many rows this process could not get onto a ledger — a
// queue full because the disk stopped answering, a directory that would not be
// made, a write that failed. It is process-wide rather than per-path because
// every reader of it asks one question — "is what I am looking at short, and by
// how much" — and a person reading a spending page does not hold a mental map
// of which ledger a row was bound for.
//
// IT EXISTS BECAUSE THE DROP ITSELF IS RIGHT AND THE SILENCE WAS NOT. The
// bargain above stands: a spending record is worth less than the turn that
// earned it, so the row gives way. But a ledger that quietly loses rows reads
// as a machine that spent less, which is the flattering direction and the one
// direction a bill must never be wrong in. So the drop is counted and the spend
// surfaces say so ([UsageDrops]). [UnbilledCalls] makes the same sentence about
// calls whose provider receipt could not be had.
var usageDropped atomic.Int64

// unbilledCalls is process-wide for the same reason [usageDropped] is: every
// reader asks whether the machine's figures are short, not which conversation
// first learned that one call could not be priced.
var unbilledCalls atomic.Int64

// UsageDrops is how many spending records this process failed to write down.
// Zero is the ordinary answer and a surface says nothing about it; anything
// else means every total taken off a ledger is short by that many calls.
func UsageDrops() int64 { return usageDropped.Load() }

// UnbilledCalls is how many calls this process knows the provider charged for
// and could not put a figure on. Zero is the ordinary answer and is rendered as
// nothing; a nonzero answer says every ledger total may be short by those calls.
func UnbilledCalls() int64 { return unbilledCalls.Load() }

// dropUsageRow counts one row that never reached a file.
func dropUsageRow() { usageDropped.Add(1) }

// countUnbilledCall records one charged call for which no receipt could be had.
func countUnbilledCall() { unbilledCalls.Add(1) }

// CountUnbilledCall lets a provider consumer outside a session agent report
// the same missing price. The resident leaf path is such a consumer: it has a
// node banker but no [Agent] method to receive the reconciliation.
func CountUnbilledCall() { countUnbilledCall() }

// usageWriters is the writer per path, and usageWritersMu guards the map ALONE.
// It is never held across a file operation, so [RecordUsage] can never be made
// to wait on a disk by another caller's write — the property the process-wide
// append mutex this replaced could not offer.
//
// A writer, once started, lives as long as the process. There is one path in an
// ordinary run and a handful in a test binary, so the map is bounded in practice
// by how many ledgers a process is asked to write rather than by anything this
// file has to enforce.
var (
	usageWritersMu sync.Mutex
	usageWriters   = map[string]*usageWriter{}
)

// usageWriterFor answers the writer for a path, starting it on the first row.
func usageWriterFor(path string) *usageWriter {
	usageWritersMu.Lock()
	defer usageWritersMu.Unlock()
	if writer := usageWriters[path]; writer != nil {
		return writer
	}
	writer := &usageWriter{queue: make(chan usageWrite, usageQueueDepth)}
	usageWriters[path] = writer
	go writer.run(path)
	return writer
}

// run drains the queue forever, holding ONE descriptor open across rows.
//
// The descriptor is opened lazily and dropped on the first write that fails, so
// the next row opens a fresh one: a ledger that was rotated or a mount that came
// back is picked up by the row after the failure rather than by a restart. The
// failing row itself is lost, which is this file's stated bargain — a spending
// record is worth less than the turn that earned it.
func (w *usageWriter) run(path string) {
	var file *os.File
	defer func() {
		if file != nil {
			_ = file.Close()
		}
	}()
	for work := range w.queue {
		if work.done != nil {
			close(work.done)
			continue
		}
		if file == nil {
			file = openUsageLedger(path)
			if file == nil {
				dropUsageRow()
				continue
			}
		}
		// ONE write per complete line, so O_APPEND's atomic offset covers the
		// whole row — which is also what lets [UsageCache] trust that the bytes
		// before the last newline are whole lines, and what keeps two processes
		// appending to one ledger from interleaving halves of two rows.
		if _, err := file.Write(work.line); err != nil {
			dropUsageRow()
			_ = file.Close()
			file = nil
		}
	}
}

// openUsageLedger opens one ledger for appending, creating its directory, and
// answers nil where it could not — the caller drops the row for [RecordUsage]'s
// reason and tries again on the next one.
func openUsageLedger(path string) *os.File {
	if directory := filepath.Dir(path); directory != "" && directory != "." {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil
	}
	return file
}

// FlushUsage waits until every row handed to [RecordUsage] before this call has
// reached the disk. It is the seam a process on its way out uses
// ([v3Process.closeAll] calls it after the conversations have closed, so the
// last turn's spending is on disk before the terminal comes back), and the one
// a test uses instead of sleeping — internal/history's Close is the same door
// for the same reason.
//
// IT IS THE ONE PLACE IN THIS FILE THAT WAITS, deliberately: a caller asking for
// a flush is asking to be told when the writing is done, and a flush that gave
// up early would be an answer about nothing. Nothing on a turn path may call it.
func FlushUsage() {
	usageWritersMu.Lock()
	writers := make([]*usageWriter, 0, len(usageWriters))
	for _, writer := range usageWriters {
		writers = append(writers, writer)
	}
	usageWritersMu.Unlock()
	// AND IT WAITS UNDER A CEILING, because the one thing it waits on is the
	// thing this whole file exists to survive. A writer parked inside
	// [openUsageLedger] on a stalled mount never drains its queue again, so an
	// unbounded flush never returns — and the caller that pays for it is
	// [v3Process.closeAll], which means a hung ~/.aforge stopped the terminal
	// from coming back. The turn path was carefully kept off the disk and the
	// exit path was handed to it instead. The bargain at the top of this file
	// settles it: a spending record is worth less than the turn that earned it,
	// and it is worth less than the exit as well. One deadline covers the whole
	// call rather than each writer, so N stalled ledgers cost what one does.
	deadline := time.NewTimer(usageFlushLimit)
	defer deadline.Stop()
	for _, writer := range writers {
		done := make(chan struct{})
		// The ENQUEUE is guarded too, and not only the wait: a full queue in
		// front of a stalled writer blocks a plain send exactly as long.
		select {
		case writer.queue <- usageWrite{done: done}:
		case <-deadline.C:
			return
		}
		select {
		case <-done:
		case <-deadline.C:
			return
		}
	}
}

// RecordUsage queues one line. Every failure is silence, for
// [RecordArtifact]'s reason: the caller has just finished a piece of a person's
// turn, and there is nothing it could usefully do with the news that a spending
// record could not be written — least of all tell them about it mid-sentence.
//
// A LINE THAT SPENT NOTHING IS NOT WRITTEN, which is [sessionFile.appendUsage]'s
// own guard kept here as well rather than trusted: this file is appended to from
// more than one door over its life, and a zero row in a spending ledger is worse
// than no row — it is a day that looks measured and was not.
func RecordUsage(path string, line UsageLine) {
	if strings.TrimSpace(path) == "" {
		return
	}
	if !line.Unbilled && line.Input == 0 && line.Output == 0 && line.USD == 0 {
		return
	}
	if line.At.IsZero() {
		line.At = time.Now()
	}
	if strings.TrimSpace(line.Day) == "" {
		line.Day = line.At.Local().Format(usageDayLayout)
	}
	payload, err := json.Marshal(line)
	if err != nil {
		dropUsageRow()
		return
	}
	// THE ENQUEUE IS NON-BLOCKING AND THE ROW IS THE THING THAT GIVES WAY. A
	// full queue means the writer is stuck on a disk that is not answering, and
	// a turn made to wait behind it would be this file's fourth rule broken to
	// save a record of what the turn cost.
	select {
	case usageWriterFor(path).queue <- usageWrite{line: append(payload, '\n')}:
	default:
		dropUsageRow()
	}
}

// ReadUsage reads the ledger, OLDEST FIRST, keeping only lines at or after
// `since`. A zero `since` keeps everything.
//
// IT READS THE FILE AND NOT THIS PROCESS'S QUEUE, so a row recorded a moment ago
// may not be here yet ([RecordUsage] hands it to a background writer).
// [FlushUsage] is the door that waits for it, and it is for a process shutting
// down and for a test — a reader on a beat simply sees the row on its next look.
//
// The order is the file's own and not reversed, because every caller of this is
// an aggregation over a window rather than a list somebody scrolls: a series
// wants its days in the order days happen.
//
// IT TOLERATES EVERYTHING A LEDGER CAN BE. A file that is not there is a machine
// that has spent nothing and answers nil with no error — the first run of a new
// install must not be an error path. A line that does not parse is skipped, for
// the task index's reason: two processes appending can in the limit interleave,
// and one bad line must cost one call's record and not the page. A real failure
// to OPEN a file that exists is returned, because that a caller can say
// something about.
func ReadUsage(path string, since time.Time) ([]UsageLine, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	lines, _, err := scanUsage(file, since)
	return lines, err
}

// usageLineBytes bounds one line the reader will hold in memory. A usage row is
// a hundred-odd bytes of numbers and ids; anything past this is a file that has
// been concatenated with something else, and reading it into memory is not a
// service to anybody. Such a line is SKIPPED but still counted as consumed, so
// one absurd row costs its own record and never the offset.
const usageLineBytes = 64 * 1024

// scanUsage reads lines from r and answers them with the count of BYTES it
// consumed in whole lines — which is what makes the tail read in
// [UsageCache.Read] safe. A partial last line (a write caught mid-flight) is not
// counted, so the next read starts at its beginning and reads it whole.
//
// IT COUNTS BYTES AND NOT TOKENS, which is why it reads to the newline itself
// rather than through a bufio.Scanner. A scanner hands back the line with its
// terminator — and a stray carriage return — already stripped, so the caller can
// only GUESS at how much of the file it just consumed; guess wrong by one byte
// and the next tail read starts mid-row and silently loses everything after it.
// The one thing this offset must be is exact.
func scanUsage(reader io.Reader, since time.Time) ([]UsageLine, int64, error) {
	var lines []UsageLine
	var consumed int64
	buffered := bufio.NewReaderSize(reader, 32*1024)
	for {
		raw, err := buffered.ReadString('\n')
		if !strings.HasSuffix(raw, "\n") {
			// The tail of the file with no terminator on it: either the file ends
			// without one, or a writer is mid-append. Either way it is not a whole
			// line, so it is neither parsed nor counted.
			if err != nil && !errors.Is(err, io.EOF) {
				return lines, consumed, err
			}
			return lines, consumed, nil
		}
		consumed += int64(len(raw))
		if len(raw) > usageLineBytes {
			continue
		}
		var line UsageLine
		if json.Unmarshal([]byte(raw), &line) != nil {
			continue
		}
		if line.At.IsZero() {
			continue
		}
		if !since.IsZero() && line.At.Before(since) {
			continue
		}
		lines = append(lines, line)
	}
}

// recordUsageLine is the engine's one door onto the ledger: the figures a
// journal line already carries, plus the four ids that say whose they are.
//
// IT IS CALLED FROM ONE PLACE — [Agent.bank], the door every call's money goes
// through on its way into the session's own meter — so the meter and this file
// can never come to hold different money. A fold passes through that door with
// nothing to write here ([Agent.addFoldedUsage] says why).
//
// THE GRAIN IS ONE CALL. It used to be one TURN, written at the seal, and a turn
// that never sealed was money the meter had and this file never got (issue
// #269). The seal now stamps the transcript alone.
//
// The record is made outside a.mu for [Agent.sealTurn]'s reason: nothing that
// can touch a file belongs under the agent's lock, and although the write itself
// now happens on a writer goroutine ([RecordUsage]), the lock is still released
// before the hand-off so no reader of the session's totals ever queues behind
// the ledger at all.
func (a *Agent) recordUsageLine(used Usage, model, role string, lane laneFacts, reconciled bool) {
	if used.Input == 0 && used.Output == 0 && used.CostUSD == 0 {
		return
	}
	path := strings.TrimSpace(a.config.usageLedger)
	if path == "" {
		path = UsageLedgerPath()
	}
	a.mu.Lock()
	session := a.sessionID()
	a.mu.Unlock()
	now := time.Now()
	line := UsageLine{
		At:         now,
		Day:        now.Local().Format(usageDayLayout),
		Model:      strings.TrimSpace(model),
		Role:       strings.TrimSpace(role),
		Calls:      used.Calls,
		Input:      used.Input,
		Output:     used.Output,
		USD:        used.CostUSD,
		Empty:      used.EmptyReflex > 0,
		Reconciled: reconciled,

		Session: session,
		// The node this agent IS, and nothing for a conversation — the same
		// figure [TaskNotice.Parent] is registered under, spelled the way
		// [TaskIndexEntry.ID] spells it so the two join.
		Task: usageTaskID(a.config.taskID),
		// The conversation the work is rooted in, which is nothing at all in a
		// conversation: Session above is already that answer, and writing it
		// twice would be the one-source-of-truth law broken on the same row.
		Root:      strings.TrimSpace(a.config.rootSession),
		Standing:  strings.TrimSpace(a.config.standingItemID),
		Workspace: strings.TrimSpace(a.config.Workspace),
	}

	// THE LANE HALF OF THE LINE GOES ON THROUGH ONE DOOR, and it comes in as an
	// argument rather than being read from anywhere here — because the only
	// honest source for it is the request this row is about, and by the time a
	// row is written that request is over. [runTurn] watches its own stream into
	// a [laneWitness] and hands what it saw down; an errand that made a call of
	// its own passes nothing, because nothing watched THAT call and a turn's
	// lane on an errand's row would be a measurement of one thing filed against
	// another.
	//
	// Under the emptiness law an unwatched call therefore writes all five
	// absent, which is the true sentence "nobody said" rather than a zero
	// somebody reads as a figure.
	RecordUsage(path, usageFromResponse(line, lane.Lane, lane.TTFT, lane.Gen, lane.Output, lane.Hedged, lane.Waste))
}

// usageTaskID spells a node's id the way the task index spells it, and answers
// nothing at all for a conversation — where "0" would be a node that does not
// exist rather than the absence of one.
func usageTaskID(id uint64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatUint(id, 10)
}

// UsageCache is the ledger read the way home may call it: as often as it likes.
//
// THE PROBLEM IT SOLVES IS NOT THE FIRST READ, IT IS THE THOUSANDTH. Home's
// clock beats every three seconds, and this file grows by a line on every model
// call — so a cache keyed on "has the file changed" would find that it HAS,
// after every single turn, and re-parse a year of spending to learn about one
// new line. So the cache is keyed on how far it has already read: an unchanged
// file answers from memory, a GROWN file is read from where the last read
// stopped, and a file that is not the one it was reading is read again from the
// beginning.
//
// "NOT THE ONE IT WAS READING" IS AN IDENTITY QUESTION AND NOT A SIZE ONE. A
// ledger that is rotated away and replaced grows back, and a cache that asked
// only "is it shorter than what I have parsed" would meet the replacement after
// it had passed that mark, keep the rows of a file that is gone, and seek into
// the new one past a prefix it never read — two ledgers added together, with
// somebody else's morning missing out of the middle. So the cache remembers WHICH
// file it read ([os.SameFile], which is the device and inode the filesystem
// reports) and how long it was, and it starts over the moment either says this
// is a different file or a shorter one.
//
// It holds every line it has ever parsed, which is the one thing that makes the
// tail read possible. That is bounded by [usageCacheLines]: past it the oldest
// are dropped and the cache says so ([UsageCache.Full]), because a page drawing
// a fortnight must not be the reason a long-lived window grows without end.
//
// A zero UsageCache is ready to use. It is NOT safe for concurrent use: it is
// held by one surface and read on that surface's own goroutine, which is where
// every reader of it lives.
type UsageCache struct {
	// Path is the ledger this cache is over. Empty means [UsageLedgerPath].
	Path string

	lines []UsageLine
	// read is how many bytes of whole lines have been parsed, and size/mod are
	// the file as it stood when that was true.
	read int64
	size int64
	mod  time.Time
	// info is the file those figures are about — kept whole rather than as a
	// device and an inode of this cache's own choosing, because [os.SameFile] is
	// the one comparison that is right on every filesystem Go runs on.
	info os.FileInfo
	// loaded says a first read has happened, so that a genuinely empty ledger is
	// distinguishable from one nobody has looked at yet.
	loaded bool
	full   bool
}

// usageCacheLines bounds what one cache holds in memory. Two hundred thousand
// lines is several years of heavy use at a few hundred bytes each — tens of
// megabytes at the very top, and a bound that exists so an always-open window
// cannot grow without one.
const usageCacheLines = 200_000

// Read answers every line at or after `since`, re-reading the file only where
// it has actually changed.
//
// Errors are answered BESIDE the lines and never instead of them, for
// [scanUsage]'s reason. A caller that only wants the figures may ignore the
// error entirely; a caller that wants to say "some of this could not be read"
// has it.
func (c *UsageCache) Read(since time.Time) ([]UsageLine, error) {
	path := strings.TrimSpace(c.Path)
	if path == "" {
		path = UsageLedgerPath()
	}
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// A machine that has spent nothing. The cache remembers that it looked,
		// so a ledger that appears later is picked up on the next beat.
		c.lines, c.read, c.size, c.mod, c.info, c.loaded = nil, 0, 0, time.Time{}, nil, true
		return nil, nil
	case err != nil:
		return c.since(since), err
	}
	// SAME FILE is asked FIRST, and it is asked here as well as below: a
	// replacement that happens to have the size and the modification time of the
	// file it replaced would otherwise be answered out of memory forever.
	same := c.loaded && c.info != nil && os.SameFile(info, c.info)
	if same && info.Size() == c.size && info.ModTime().Equal(c.mod) {
		return c.since(since), nil
	}
	if !same || info.Size() < c.size || info.Size() < c.read {
		// Either a different file wearing the same name, or the same one
		// truncated back under what we have already read. Nothing held is about
		// it, and the read starts at nothing — which is also what keeps the seek
		// below from ever landing past a prefix this cache did not read.
		c.lines, c.read, c.full = nil, 0, false
	}
	file, err := os.Open(path)
	if err != nil {
		return c.since(since), err
	}
	defer file.Close()
	// THE FILE THAT WAS STAT'ED AND THE FILE THAT IS OPEN NEED NOT BE THE SAME
	// ONE — a rotation can land between the two calls — so identity is asked
	// again, of the descriptor actually about to be read. What this protects is
	// the seek: reading from an offset into a file whose first bytes this cache
	// never saw would lose that prefix silently and forever.
	if opened, err := file.Stat(); err == nil {
		if c.read > 0 && (c.info == nil || !os.SameFile(opened, c.info) || opened.Size() < c.read) {
			c.lines, c.read, c.full = nil, 0, false
		}
		info = opened
	}
	if c.read > 0 {
		if _, err := file.Seek(c.read, io.SeekStart); err != nil {
			// A seek that fails leaves the cache exactly as it was rather than
			// half-advanced; the next beat tries again from the same offset.
			return c.since(since), err
		}
	}
	// The TAIL is read with no floor: a cache that filtered on the way in could
	// never answer a question about a window older than the first one it was
	// asked. The floor is applied on the way out, over what is held.
	fresh, consumed, scanErr := scanUsage(file, time.Time{})
	c.lines = append(c.lines, fresh...)
	c.read += consumed
	c.size, c.mod, c.info, c.loaded = info.Size(), info.ModTime(), info, true
	if len(c.lines) > usageCacheLines {
		c.lines = c.lines[len(c.lines)-usageCacheLines:]
		c.full = true
	}
	return c.since(since), scanErr
}

// Full says the cache has dropped its oldest lines to stay inside its bound, so
// a total taken from it is a total over what it still holds. A page quoting an
// all-time figure has to say so; a page drawing a fortnight never has to care.
func (c *UsageCache) Full() bool { return c.full }

// since is the held lines at or after a floor. A zero floor shares the backing
// array, which is the ordinary case and the whole slice.
//
// IT IS A FILTER AND NOT A PREFIX CUT, and the difference is the point. The file
// is APPENDED in the order writes land, which is not the order the rows are
// stamped: a second process can journal a call at 09:00 and reach the file after
// this one's 10:00 row is already in it — its own turn ran in between — and two
// processes writing one machine-wide ledger is the ordinary case here, not a
// pathological one. A floor implemented as "cut at the first row that is not
// below it" would meet that 10:00 row first and hand back the 09:00 row sitting
// behind it as though it were inside the window. So every row is asked.
func (c *UsageCache) since(floor time.Time) []UsageLine {
	if floor.IsZero() {
		return c.lines
	}
	kept := make([]UsageLine, 0, len(c.lines))
	for _, line := range c.lines {
		if !line.At.Before(floor) {
			kept = append(kept, line)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// RecordUnbilledCall keeps the missing receipt on disk with its owner. The
// marker carries no invented money or token count and survives process restart.
func RecordUnbilledCall(path string, line UsageLine) {
	countUnbilledCall()
	line.Unbilled = true
	RecordUsage(path, line)
}

// recordUnbilledReceipt uses the same ownership fields as an ordinary usage row.
func (a *Agent) recordUnbilledReceipt(model string) {
	path := strings.TrimSpace(a.config.usageLedger)
	if path == "" {
		path = UsageLedgerPath()
	}
	a.mu.Lock()
	owner := a.sessionID()
	a.mu.Unlock()
	RecordUnbilledCall(path, UsageLine{
		Session: owner, Model: model, Task: usageTaskID(a.config.taskID),
		Root: a.config.rootSession, Standing: a.config.standingItemID, Workspace: a.config.Workspace,
	})
}
