package provider

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// THE GUARD OVER ONE MODEL STREAM.
//
// Two things go wrong with a request that the retry loop in retry.go cannot see,
// because both of them happen AFTER the response headers have arrived and a
// perfectly healthy-looking stream is open:
//
//   - SILENCE. The endpoint accepts the request and then says nothing, or says
//     so little that the answer will never arrive. The body watchdog in
//     transport.go counts ANY BYTE as progress, keepalive comments included, so
//     an endpoint that trickles colons down the wire forever is invisible to it.
//     What this counts instead is the model WRITING: a token of answer, a token
//     of reasoning, a fragment of a tool call. Nothing else moves the clock.
//
//     BUT AN ENDPOINT SPEAKING IS NOT AN ENDPOINT GONE. Keepalive comments do
//     not move the clock — they BUY PATIENCE instead, up to a hard cap. The
//     distinction was measured on 2026-08-24: five of the sixteen endpoints
//     serving one production model assemble a whole tool call server-side and
//     deliver it as a lump, keepalives flowing the entire time; the largest gap
//     seen was fifty seconds on a fourteen-kilobyte call that then FINISHED.
//     Cutting those streams at the plain bound turned every big write into a
//     guaranteed failure, three retries deep, while the answer was seconds from
//     landing. So a quiet stream whose endpoint is still speaking is given
//     [bufferedQuietBound] in total, and a quiet stream whose endpoint has
//     stopped speaking too is cut at the plain bounds — that one really is a
//     connection that is not coming back. The cap is what keeps the colon
//     trickler from owning this loop forever: patience is bounded, and the
//     bound is stated once, below.
//
//   - OVERRUN. The endpoint keeps writing and never stops. Every bound above
//     and every bound below is a SILENCE bound — the gap between two tokens,
//     the wait for the first one, the total quiet a buffering endpoint may
//     accumulate — and none of them has anything to say about a stream that
//     drips a token every second for half an hour. The streaming client's own
//     total deadline is switched off on purpose (retry.go's clientFor: a total
//     timeout killed every healthy long stream at the completion budget), so
//     until [streamWallFactor] there was NOTHING in this process that ended
//     such a request.
//
//     THIS IS INFERENCE FROM ABSENCE and it is worth saying plainly. In
//     SWE-Marathon run s10 the journal went thirty minutes with no row of any
//     kind — no call line, no error line (one is written per attempt now:
//     internal/session's journalFailedCall), and the last tool call had
//     returned in four tenths of a second. Nothing was proven to be dripping
//     tokens, because nothing recorded it. What IS a fact is that the only
//     unbounded thing left in the request path was a streamed reply, and that
//     the missing bound is missing whether or not it is what cost that run.
//
//   - DEGENERATION. The model loses the thread and writes soup — a run of one
//     letter, a paragraph repeated until the token budget is gone, words with
//     three alphabets inside them. It is a real thing that happened to this
//     session on 2026-08-20 against a 150k-token context, twice in one
//     conversation, and the second time was worse than the first BECAUSE THE
//     FIRST ONE WAS STILL IN THE CONTEXT. Junk in the transcript breeds junk.
//
// Both answers are the same shape: cut the request, say so, ask again. The
// asking again is [Agent.completeWithRetry]'s in internal/session, which already
// throws away the dead attempt's partial text, its warm tool batch and its
// half-arrived calls — this layer only detects, cuts, and names what happened.
//
// WHAT THIS LAYER NEVER DOES is decide the person's words. A cut leaves through
// [StreamCut], which internal/session turns into the sentence a person reads,
// for the reason patience.go gives about its own two seams: the words live where
// the words live.

const (
	// firstDeltaBound is how long a request may be open with the model having
	// written NOTHING — no answer token, no reasoning token, no tool-call
	// fragment.
	//
	// It is generous on purpose. A reasoning model at a long context legitimately
	// thinks for a minute before its first token, and cutting a call that was
	// about to answer is worse than waiting: the retry pays the whole prompt
	// again. Ninety seconds is past every healthy first token this adapter has
	// measured (velocity.go's ledger) and short of the two-minute header
	// deadline, so a stall is named here rather than surfacing as a torn
	// connection.
	firstDeltaBound = 90 * time.Second
	// midStreamGapBound is how long an ESTABLISHED stream may go quiet. It is
	// half the first bound because the question is different: a model that has
	// started writing has finished deciding, and forty-five seconds between two
	// tokens of one sentence is a connection that is not coming back.
	midStreamGapBound = 45 * time.Second
	// streamGapLumpTokens is what the mid-stream bound BECOMES on a lane whose
	// rate this process has measured, expressed as work rather than as time:
	// a stream that has gone quiet for as long as this lane would take to write
	// three and a half thousand tokens has stopped writing.
	//
	// It is the buffered lump of 2026-08-24 in the header, in tokens. That
	// measurement is the largest legitimate quiet stretch this adapter has ever
	// seen — fourteen kilobytes of tool call assembled server-side, delivered
	// whole, and finished — and fourteen kilobytes at the estimator's four
	// bytes to the token is three and a half thousand of them. So the bound
	// says the same thing at every speed: an endpoint that has not produced the
	// biggest lump we have ever measured, in the time IT takes to produce one,
	// is not producing anything.
	//
	// The arithmetic is the whole point of the change. A lane measured at 250
	// tokens a second is given fourteen seconds; one at 83, forty-two; one
	// slower than about 78 is given [midStreamGapBound] and nothing more,
	// because below that the derived figure is longer than the flat one it
	// replaces and the flat one already stood. See [gapFor].
	streamGapLumpTokens = 3_500
	// bufferedQuietBound is the TOTAL quiet a stream may accumulate while its
	// endpoint is still sending keepalives — the buffering case the header
	// describes, where the answer is being assembled server-side and will land
	// whole. Two and a half minutes covers every buffered delivery measured on
	// 2026-08-24 (the slowest healthy one took eighty-six seconds end to end)
	// with room for the bigger writes production makes, and it is still a
	// bound: an endpoint that speaks forever and answers never is cut here.
	bufferedQuietBound = 150 * time.Second
)

// ── THE WALL ────────────────────────────────────────────────────────────────
//
// THE LAW: EVERY REQUEST CARRIES A WALL AS WELL AS A SILENCE BOUND. The three
// constants above bound how long a stream may say NOTHING. These three bound
// how long it may go on saying something, and they are a different question
// with a different answer: a reply that is still arriving is not a connection
// that has died, so the wall cannot be a small number and cannot be a fixed
// one.
//
// IT IS DERIVED FROM WHAT THE LANE ITSELF HAS SERVED, and never from a table of
// model sizes. A size table is a claim this process cannot check — the catalog
// row that said one model held 1.3M tokens is why compaction believes an
// endpoint's refusal over a card's figure (internal/session's TrustedWindowFor)
// — whereas "the longest reply this endpoint has actually finished for us, this
// hour" is a measurement, and velocity.go is already keeping it. The wall is that figure times [streamWallFactor], clamped between
// [streamWallFloor] and [streamWallCeiling].
const (
	// streamWallFactor is how many times the longest reply a lane has COMPLETED
	// the next one may run before it is cut.
	//
	// FIVE, because the spread between two healthy replies from one endpoint is
	// dominated by how much the model chose to write, and that ratio is
	// routinely three or four to one inside a single session — a one-line
	// confirmation against a whole-file rewrite is exactly that. Five is past
	// the widest honest ratio and short of an order of magnitude, which is the
	// range a reply that is never going to end lives in. Two would cut real
	// answers; fifty would be no wall at all.
	streamWallFactor = 5
	// streamWallFloor is the wall a lane WITH NO HISTORY gets, and it is the
	// outer bound of this whole file: the answer to "how long may a request run
	// when this process has measured nothing at all about who is serving it".
	//
	// It is five minutes because that is already this adapter's argued answer to
	// the same question asked about the same work delivered in one piece:
	// adaptiveCompletionTimeout's floor. A non-streamed call gets at least five
	// minutes in total, so a streamed one gets at least five minutes of
	// generation — and it gets that on the very first request of a cold process,
	// where there is nothing measured to multiply.
	//
	// IT IS NO LONGER THE FLOOR UNDER A MEASURED LANE, and that is the fix. A
	// floor that outranked the measurement made the measurement pointless: a
	// lane whose longest finished reply was twenty-four seconds still got five
	// whole minutes, so "derived from the lane's own history" was true of the
	// arithmetic and false of every fast endpoint in practice. Two streams in
	// the dogfood run of 2026-08-31 hung for exactly this figure, on lanes that
	// were demonstrably sustaining between 83 and 270 tokens a second. A lane
	// this process HAS measured is bounded by [streamWallMeasuredFloor] instead.
	streamWallFloor = 5 * time.Minute
	// streamWallMeasuredFloor is the least a lane whose history we hold may be
	// given, and it is [bufferedQuietBound] rather than a number of its own.
	//
	// The two bound the same thing from opposite sides and must not disagree.
	// A stream may legitimately go quiet for the buffered cap while its endpoint
	// assembles an answer server-side; a wall shorter than that cap would cut a
	// stream the silence bounds were still being patient with, which is a
	// guaranteed failure on exactly the deliveries the buffered cap was measured
	// to protect. So the shortest honest wall is the longest honest silence, and
	// it is written as that constant rather than as a second copy of its value.
	streamWallMeasuredFloor = bufferedQuietBound
	// streamWallCeiling is where a request ends whatever its lane's history
	// claims.
	//
	// Twenty minutes. The measured failure it exists for ran for thirty and was
	// still running when the run was killed, so a ceiling that could reach
	// thirty would not have caught it. Past twenty minutes on one request the
	// arithmetic has flipped anyway: re-asking a different endpoint pays the
	// prompt again, which is minutes at worst, against a wait that has already
	// cost more than that and has produced no evidence it will ever end.
	//
	// It is also what keeps one pathological completion from poisoning the
	// ledger. A lane that once took nineteen minutes and finished cannot use
	// that to buy itself an hour.
	streamWallCeiling = 20 * time.Minute
)

// stallFirstBound and stallGapBound are what the watchdog actually reads. The
// constants above are the figures — one source of truth for the manual page and
// for the sentence a cut is named with — and these exist only so a test can
// prove the machinery in milliseconds rather than in minutes.
var (
	stallFirstBound    = firstDeltaBound
	stallGapBound      = midStreamGapBound
	stallBufferedBound = bufferedQuietBound
	stallWallFloor     = streamWallFloor
	stallWallMeasured  = streamWallMeasuredFloor
	stallWallCeiling   = streamWallCeiling
	stallGapLumpTokens = streamGapLumpTokens
)

// wallFor turns the longest reply a lane has COMPLETED into the wall its next
// reply is bounded by. A lane nothing is known about — a cold process, a model
// whose first request this is, or a session with `routing off` — passes zero
// and gets the floor, which is the whole of what the floor is for.
func wallFor(longest time.Duration) time.Duration {
	// A LANE NOTHING IS KNOWN ABOUT GETS THE OUTER BOUND AND NOT A DERIVATION.
	// There is no measurement to be in proportion to, and five minutes is this
	// file's stated answer to that case; every other lane is bounded by what it
	// has actually done.
	if longest <= 0 {
		return stallWallFloor
	}
	wall := longest * streamWallFactor
	if wall < stallWallMeasured {
		wall = stallWallMeasured
	}
	if wall > stallWallCeiling {
		wall = stallWallCeiling
	}
	return wall
}

// gapFor turns a lane's MEASURED OUTPUT RATE, in tokens per second, into how
// long a stream it is serving may go quiet between two tokens.
//
// It is the silence half of the same law the wall is the duration half of: a
// bound stated in what the lane itself does rather than in a figure invented for
// every lane at once. [midStreamGapBound] is what a stranger gets and it is also
// the ceiling here, because this may only ever TIGHTEN patience — the flat bound
// was argued against the slowest healthy endpoint this adapter has seen, and a
// derivation that loosened it would be re-opening a question that is settled.
//
// The floor is [LagGap]. Below that figure the lag law itself still calls a
// quiet stretch streaming rather than buffering (velocity.go states the
// measurement: every endpoint that streamed stayed under four seconds between
// deltas, every one that buffered sat at twelve or worse), so cutting there
// would be cutting a stream that the layer next door is still describing as
// healthy — two bounds in one process disagreeing about the same silence.
//
// A rate of zero is a lane this process has not rated: it gets the flat bound,
// which is what it always got.
func gapFor(rate float64) time.Duration {
	if rate <= 0 {
		return stallGapBound
	}
	gap := time.Duration(float64(stallGapLumpTokens) / rate * float64(time.Second))
	if gap < LagGap {
		gap = LagGap
	}
	if gap > stallGapBound {
		gap = stallGapBound
	}
	return gap
}

// CutReason says which of the two things went wrong, and it is the only thing
// this package decides about a cut. The sentence is composed upstream.
type CutReason int

const (
	// CutSilent is a request that produced nothing within firstDeltaBound.
	CutSilent CutReason = iota
	// CutStalled is a request that started writing and then went quiet for
	// midStreamGapBound.
	CutStalled
	// CutBabble is a reply that stopped being language: a repetition loop, or
	// text switching alphabet inside its own words.
	CutBabble
	// CutOverrun is a reply that never stopped: an endpoint that kept writing
	// past the wall its own history earned it. See [wallFor].
	CutOverrun
	// CutMachinery is a reply that is the model's own tool grammar written as
	// text: the request declared tools, the answer called none, and the content
	// spells a declared tool's name fenced in delimiter bytes inside
	// delimiter-dense text. It means the serving endpoint did not parse its
	// model's chat template, and the reply is unusable no matter how healthy
	// the stream that carried it was. See [MachineryLeak].
	CutMachinery
)

// word is the short machine-readable name of a cut: what the lane's belief
// records as the reason its answer could not be used, and what a log row spells
// when it says why.
//
// IT IS NOT A SENTENCE A PERSON READS. [StreamCut.Error] composes those, and the
// two must not become one thing — a word short enough to key a ledger on is too
// short to explain anything, and a sentence is too long to be a key.
func (r CutReason) word() string {
	switch r {
	case CutSilent:
		return "silent"
	case CutStalled:
		return "stalled"
	case CutBabble:
		return "babble"
	case CutOverrun:
		return "overrun"
	case CutMachinery:
		return "machinery"
	}
	return "cut"
}

// StreamCut is the error a guarded stream fails with. It is a distinct type
// rather than a message because the decision upstream — retry, and how many
// times — is made on the reason, and a decision made by matching substrings of
// an error string is a decision that breaks the next time somebody rewords it.
type StreamCut struct {
	Reason CutReason
	// Waited is how long the stream was quiet, on the two silence reasons, and
	// zero on CutBabble. It is the constant that fired rather than a measurement
	// — the plain bound on an outright silence, [bufferedQuietBound] when
	// keepalives bought the stream its full patience and it still never wrote —
	// because the timer is what decided, and the timer's own bound is the
	// honest figure.
	//
	// On CutOverrun it is THE WALL THAT FIRED, which is derived rather than
	// constant ([wallFor]): the same rule, that the figure a person is told is
	// the figure the timer was set to.
	Waited time.Duration
	// Provider is the endpoint the stream named as serving it, "" when no chunk
	// ever did. Ran is how long the request had been open and Tokens is how much
	// answer had arrived, both measured rather than derived.
	//
	// THE THREE OF THEM EXIST FOR THE JOURNAL. A cut is the one failure that got
	// somewhere before it failed, and the autopsy question about it — was this
	// endpoint producing nothing, or producing forever? — cannot be answered
	// from a reason word alone. internal/session's journalFailedCall writes them
	// onto the error row.
	Provider string
	Ran      time.Duration
	Tokens   int
	// Rerouted says the ledger ACTED on this cut: the endpoint that went quiet
	// was named on the wire and struck out of the (model, endpoint) lane, so the
	// very next attempt is encoded away from it (velocity.go's noteCutProvider).
	//
	// It exists for one question a layer above has to answer before it moves a
	// turn to another MODEL — has endpoint diversity actually been tried? Two
	// different things make the answer no and both land here as false: `routing
	// off`, where the ledger is switched off by the person's own instruction,
	// and a stream that died before any chunk named its provider, where there
	// was nothing to strike. In both, asking the same model again lands on the
	// same lane deterministically, and the honest move is to stop asking it
	// sooner. The decision itself is not this package's — internal/session's
	// loop.go states the rule — and this is the one fact it cannot see.
	Rerouted bool
}

func (c *StreamCut) Error() string {
	if c == nil {
		return ""
	}
	switch c.Reason {
	case CutSilent:
		return fmt.Sprintf("nothing came back from the model in %s", roundSeconds(c.Waited))
	case CutStalled:
		return fmt.Sprintf("the model stopped mid-reply and went quiet for %s", roundSeconds(c.Waited))
	case CutOverrun:
		return fmt.Sprintf("the reply ran past %s without finishing and was cut", roundSeconds(c.Waited))
	case CutMachinery:
		return "the reply was the model's own internal markup instead of an answer and was cut"
	default:
		return "the reply stopped being language and was cut"
	}
}

// CutFrom reports whether an error is a guarded stream's cut, and which kind.
func CutFrom(err error) (*StreamCut, bool) {
	for err != nil {
		if cut, ok := err.(*StreamCut); ok {
			return cut, true
		}
		unwrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			return nil, false
		}
		err = unwrapped.Unwrap()
	}
	return nil, false
}

// ── WHAT WAS THE PATH'S FAULT AND NOT THE ENDPOINT'S ────────────────────────

// pathFaultPhrases are what the Go network stack says when the far end drops a
// connection an answer was still arriving on. They are the WIRE and nothing
// else: a refusal the router made about our bytes carries a status and is
// classified long before this is asked.
var pathFaultPhrases = []string{
	"connection reset",
	"broken pipe",
	"unexpected eof",
	"reset before headers",
	"socket hang up",
	"socket connection was closed",
}

// PathFault reports whether an error is about the CONNECTION rather than about
// the endpoint behind it.
//
// IT IS A SEAM FOR THE LAYERS ABOVE, AND IT IS NOT AN INVITATION TO RETRY. The
// waiting policy (docs/ARCHITECTURE.md) gives every re-ask on one question to
// one owner — the funnel, where the lane watch already treats a path with no
// heartbeat and no byte as a fault, refuses to charge the lane's belief for it,
// and walks to the next machine behind the same model. A layer that answered a
// reset with a re-ask of its own would be a second clock on one silence, which
// is the exact defect that policy exists to end.
//
// SO WHAT A CALLER MAY DO WITH IT IS CHOOSE ITS WORDS. A task node whose run
// ended on a dropped connection is not a node that failed at the work, and
// "lost the connection" is a truer ending than "failed" — that is the whole of
// what this predicate is for.
func PathFault(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, phrase := range pathFaultPhrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

// roundSeconds spells a bound the way a person says it. The constants above are
// whole seconds, so this is exact rather than approximate.
func roundSeconds(d time.Duration) string {
	return d.Round(time.Second).String()
}

// ── the off switch ──────────────────────────────────────────────────────────

type babbleGuardKey struct{}

// WithoutBabbleGuard takes the degeneration guard off every call made under
// ctx. The silence watchdog has no switch and is not affected: a request that
// produced nothing in ninety seconds has failed by any reading, and there is no
// preference under which sitting on it is the answer.
//
// It rides the context for the reason patience.go's seams do — the adapter is
// shared by every agent in the process, so a field on the client would make a
// task node's setting the conversation's.
func WithoutBabbleGuard(ctx context.Context) context.Context {
	return context.WithValue(ctx, babbleGuardKey{}, false)
}

// babbleGuardOn reports whether this call watches for degeneration. Default on:
// an unstamped context is every call that existed before this did.
func babbleGuardOn(ctx context.Context) bool {
	if ctx == nil {
		return true
	}
	on, stamped := ctx.Value(babbleGuardKey{}).(bool)
	return !stamped || on
}

// ── the silence watchdog ────────────────────────────────────────────────────

// stallWatch cuts a request that is not being written to.
//
// One timer, reset by the model writing anything. It fires at most once — the
// cancel it pulls is the attempt's, and pulling it twice would be a second cut
// of a stream that is already dead. Keepalives never reset the timer: they are
// weighed only when it fires, which is what keeps the hot read loop free of
// timer traffic for lines that carry no answer.
type stallWatch struct {
	mu     sync.Mutex
	timer  *time.Timer
	cancel context.CancelFunc
	// clock exists so a test can hold time still while the timer machinery
	// runs at millisecond bounds; everything real reads time.Now through it.
	clock  func() time.Time
	spoken bool
	// quietSince is when the model last wrote — the birth of the watch until
	// it has — and it is what the buffered cap is measured against: the cap
	// bounds ACCUMULATED quiet, not the gap between two keepalives.
	quietSince time.Time
	// lastAlive is when the endpoint last said anything that was not an
	// answer — a keepalive comment. Zero is an endpoint that never has.
	lastAlive time.Time
	tripped   *StreamCut
	// born is when the headers landed, and it is what the wall is measured
	// from: the wall bounds the whole request, not one quiet stretch of it.
	born time.Time
	// wallTimer fires when the request has been open longer than the lane's
	// own history says any reply of its ever takes. walled is the bound it was
	// set to, kept so the sentence a person reads names the figure that
	// decided. rewalled says the wall has already been re-derived once, from
	// the endpoint the stream named — see [stallWatch.rewall].
	wallTimer *time.Timer
	walled    time.Duration
	rewalled  bool
	// gap is the mid-stream silence bound in force. It opens at the flat bound
	// every stranger gets and narrows once the stream names its lane, to what
	// that lane's measured rate says a gap should be ([gapFor], [regap]).
	gap time.Duration
	// regapped says the gap has already been narrowed once, for [rewall]'s
	// reason exactly: a bound that could be moved repeatedly by chunks would
	// not be a bound.
	regapped bool
}

// newStallWatch starts both clocks: the silence timer, and the wall.
//
// The wall is passed in rather than read here because deriving it needs the
// ledger, and this file is deliberately the layer that only detects and cuts.
// A caller with nothing to derive from passes wallFor(0), which is the floor.
func newStallWatch(cancel context.CancelFunc, wall time.Duration) *stallWatch {
	watch := &stallWatch{cancel: cancel, clock: time.Now}
	watch.born = watch.clock()
	watch.quietSince = watch.born
	watch.walled = wall
	watch.gap = stallGapBound
	watch.timer = time.AfterFunc(stallFirstBound, func() { watch.fire() })
	watch.wallTimer = time.AfterFunc(wall, func() { watch.overran() })
	return watch
}

// rewall re-derives the wall now that the stream has said WHO IS SERVING IT.
//
// THE WALL IS THE LANE'S AND NOT THE MODEL'S, and which lane a request landed
// on is a thing nothing knows until the first chunk names it. So the request
// opens under the lineage's widest wall — the most any endpoint of this model
// has earned, which is the only honest bound before the answer to "who" exists
// — and narrows to the serving lane's own the moment it is known. A lane this
// process has never seen inherits the lineage's, which is what keeps a router
// moving a session onto a fresh endpoint from cutting its first long reply.
//
// It happens ONCE. A stream that renamed its provider halfway through is not a
// thing this wire does, and a wall that could be pushed out repeatedly by
// chunks would not be a wall.
//
// The new bound is measured from [stallWatch.born] rather than from now, so
// narrowing is real: a lane whose wall is already spent is cut immediately
// instead of being given the whole of it again.
func (w *stallWatch) rewall(wall time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.tripped != nil || w.rewalled || wall <= 0 {
		return
	}
	w.rewalled = true
	w.walled = wall
	left := w.born.Add(wall).Sub(w.clock())
	if left < 0 {
		left = 0
	}
	w.wallTimer.Reset(left)
}

// regap re-derives the mid-stream silence bound now that the stream has said WHO
// IS SERVING IT, and it is [stallWatch.rewall]'s twin in every respect: the same
// seam, the same once-only rule, the same reason.
//
// The stream opens on the flat bound because nothing is known about the lane
// before the first chunk names it. From that moment the bound is what THIS lane's
// measured rate says a gap should be, which on a fast endpoint is a small
// fraction of the flat figure — and a small fraction of it is the whole point:
// sixty seconds of nothing from an endpoint sustaining two hundred and fifty
// tokens a second is a dead stream, not a patient one.
//
// It only ever narrows ([gapFor] caps at the flat bound), and it re-arms the
// timer from when the stream last WROTE rather than from now, so narrowing is
// real: a lane whose new bound is already spent is cut immediately instead of
// being given the whole of it again.
func (w *stallWatch) regap(gap time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.tripped != nil || w.regapped || gap <= 0 || gap >= w.gap {
		return
	}
	w.regapped = true
	w.gap = gap
	// The first-token bound is a different question with a different answer and
	// is not this bound's business: a request that has not been answered at all
	// is judged by [firstDeltaBound] until it is.
	if !w.spoken {
		return
	}
	left := w.quietSince.Add(gap).Sub(w.clock())
	if left < 0 {
		left = 0
	}
	w.timer.Reset(left)
}

// overran is the wall firing: the endpoint is writing, it has been writing for
// longer than anything of its own has ever taken to finish, and it is not going
// to stop. It is the same cut every other reason makes — cancel the request,
// name what happened — so the decode loop's one cut path answers it unchanged.
func (w *stallWatch) overran() {
	w.mu.Lock()
	if w.tripped != nil {
		w.mu.Unlock()
		return
	}
	w.tripped = &StreamCut{
		Reason: CutOverrun,
		Waited: w.walled,
		Ran:    w.clock().Sub(w.born),
	}
	cancel := w.cancel
	w.mu.Unlock()
	cancel()
}

// progress says the model wrote something. It restarts the clock at the
// mid-stream bound, because from the first token onwards the question is about
// gaps rather than about the wait to be served — and it restarts the buffered
// cap too, because the cap is about one quiet stretch, not about the whole
// stream.
func (w *stallWatch) progress() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.tripped != nil {
		return
	}
	w.spoken = true
	w.quietSince = w.clock()
	w.timer.Reset(w.gap)
}

// alive says the endpoint spoke without answering — a keepalive line. It only
// stamps the time: whether that buys the stream anything is decided when the
// timer fires, against the cap, so a trickle of comments can never hold the
// watch open by itself.
func (w *stallWatch) alive() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.tripped != nil {
		return
	}
	w.lastAlive = w.clock()
}

// fire is the timer's callback, and it is two things on purpose: a decision
// taken entirely under the lock, and a cancellation taken entirely outside it.
//
// THE CANCEL MAY NOT RUN UNDER THIS LOCK. Cancelling a context runs whatever is
// waiting on it, in this goroutine, before the call returns — so holding the
// watch's lock across it would put this mutex underneath somebody else's
// ordering. That is why the unlock used to sit in the middle of the reasoning,
// and [stallWatch.verdict] is the same code with the whole critical section
// wrapped in a function, so the unlock can be a defer that no future early
// return can slip past (internal/guard's lockdefer_test.go states the law).
func (w *stallWatch) fire() {
	if cancel := w.verdict(); cancel != nil {
		cancel()
	}
}

// verdict is the whole of fire's reasoning, under the lock from first line to
// last. It answers with the cancellation the caller owes the stream, or nil when
// the watch re-armed instead and the stream lives.
func (w *stallWatch) verdict() context.CancelFunc {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.tripped != nil {
		return nil
	}
	now := w.clock()
	bound := stallFirstBound
	// speaking is the window a keepalive has to land inside to buy the stream
	// more patience, and IT IS THE FLAT BOUND EVEN WHEN THE SILENCE BOUND HAS
	// NARROWED. Whether an endpoint is still on the line is a question about the
	// connection; how fast the model behind it writes is a question about the
	// lane, and narrowing the second must not quietly answer the first. A
	// buffering endpoint sending a comment every twenty seconds is alive at any
	// rate it has ever been measured at, and the cap above is what bounds it.
	speaking := stallFirstBound
	if w.spoken {
		bound = w.gap
		speaking = stallGapBound
	}
	// THE EXTENSION, and its two conditions. The endpoint must still be
	// speaking — a keepalive inside the bound that just elapsed — and the
	// accumulated quiet must still be inside the cap. Both true, the timer is
	// re-armed for the shorter of another bound and what remains of the cap;
	// either false, the cut below is the answer. The re-arm happens with the
	// lock held and the timer already fired, so it cannot race a second fire.
	allowance := w.quietSince.Add(stallBufferedBound).Sub(now)
	if now.Sub(w.lastAlive) <= speaking && allowance > 0 {
		wait := bound
		if allowance < wait {
			wait = allowance
		}
		w.timer.Reset(wait)
		return nil
	}
	// Waited is the figure that actually decided: the plain bound when the
	// endpoint went silent outright, the cap when patience was extended and
	// ran out — so the sentence a person reads matches the wait they watched.
	waited := bound
	if allowance <= 0 {
		waited = stallBufferedBound
	}
	if w.spoken {
		w.tripped = &StreamCut{Reason: CutStalled, Waited: waited}
	} else {
		w.tripped = &StreamCut{Reason: CutSilent, Waited: waited}
	}
	return w.cancel
}

// cut is the trip, or nil. It is read after the stream has died, to tell a
// watchdog's cancellation apart from the person's own interrupt.
func (w *stallWatch) cut() *StreamCut {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.tripped
}

// stop ends the watch. BOTH timers are READ under the lock and stopped outside
// it, for [stallWatch.fire]'s reason one function up: Stop is somebody else's
// code and this lock stays underneath none of it. [stallWatch.heldTimers] is
// that read as its own function, so the unlock is a defer rather than a line in
// the middle.
func (w *stallWatch) stop() {
	quiet, wall := w.heldTimers()
	if quiet != nil {
		quiet.Stop()
	}
	if wall != nil {
		wall.Stop()
	}
}

// heldTimers are the watch's two clocks — the silence bound and the whole-request
// wall — read under the lock.
func (w *stallWatch) heldTimers() (*time.Timer, *time.Timer) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.timer, w.wallTimer
}

// ── the degeneration guard ──────────────────────────────────────────────────

const (
	// babbleWindow is the tail of assistant text the compression test reads, in
	// bytes. Four kilobytes is long enough to hold several repetitions of a
	// sentence-length cycle and short enough that the whole window is about the
	// same thought.
	babbleWindow = 4 << 10
	// babbleFloor is the compressed-to-raw ratio below which the window is a
	// loop of SOME cycle length — one letter, one line, one paragraph; the test
	// does not care which, which is the whole reason it is a compressor and not
	// a pattern.
	//
	// Measured against the two real degenerations of 2026-08-20 and a corpus of
	// legitimate replies (streamguard_test.go): the '    0\n' loop bottoms out
	// at 0.015, while a markdown table reaches 0.22, ASCII art 0.086, and prose
	// 0.24. Three and a half percent sits in the empty middle.
	babbleFloor = 0.035
	// babbleEvery is how much new text must arrive before the window is read
	// again. The test runs at DELTA CADENCE and not per byte: a compressor over
	// four kilobytes costs tens of microseconds, which is nothing once every
	// half-kilobyte and a real tax once per token.
	babbleEvery = 512
	// churnWindow is the tail the script test reads, in RUNES.
	churnWindow = 600
	// churnBound is mid-word alphabet switches per hundred runes, above which
	// the text is not being written in any language. Legitimate multilingual
	// writing switches at word boundaries; every innocent in the corpus scores
	// exactly zero, and the mixed-script degeneration scores 5.5.
	churnBound = 3.0
	// churnScripts is how many alphabets must be PRESENT before the churn figure
	// is believed at all. Two is a bilingual answer, which is ordinary.
	churnScripts = 3
	// churnRunes is how many runes an alphabet needs in the window to count as
	// present, so one loanword, one quoted name or one emoji is never a third
	// alphabet.
	churnRunes = 5
	// cleanCap bounds the tail this keeps. It is twice the compression window so
	// that the window is always full of real text after a fenced block passes.
	cleanCap = 2 * babbleWindow
)

// babbleWatch reads the assistant's TEXT as it streams and says when it has
// stopped being language.
//
// IT NEVER SEES A TOOL RESULT. A tool that prints a million zeros is doing its
// job, and a guard that read results would cut the turn that asked for them —
// so this is fed from the content deltas of the model's own reply and from
// nothing else.
//
// FENCED CODE IS EXCLUDED for the same reason: a zero matrix, a test log and a
// generated table are all legitimate replies that compress to nothing, and all
// three arrive inside ``` fences. A fence is honoured only when it is
// well-formed — an opener whose info string looks like a language tag, a closer
// that is bare — because the 2026-08-20 degeneration emitted
// "```ongoingSpark........................" mid-soup, and a scanner that
// believed that would have been blinded by the very text it was watching for.
type babbleWatch struct {
	// clean is the tail of text OUTSIDE fenced code, oldest bytes dropped.
	clean []byte
	// pending is the line being written, which is not yet known to be a fence.
	pending []byte
	inFence bool
	// since counts bytes of clean text added since the last test.
	since int
	// squeeze is the compressor [babbleWatch.loopedTail] runs the window
	// through, and counter is what it writes into. Both are held for the life of
	// the stream rather than built per test: zlib.NewWriter carries a deflate
	// state a hundred kilobytes wide, the test runs once every babbleEvery bytes
	// of a reply, and Reset leaves the writer in exactly the state a new one
	// would be in — so the ratio measured is the same ratio, bit for bit.
	squeeze *zlib.Writer
	counter countingWriter
}

func (b *babbleWatch) write(delta string) bool {
	for len(delta) > 0 {
		at := strings.IndexByte(delta, '\n')
		if at < 0 {
			b.grow(delta)
			break
		}
		b.grow(delta[:at+1])
		b.endLine()
		delta = delta[at+1:]
	}
	// A line long enough to be the whole window cannot be a fence marker, and
	// holding it would let a model that never presses return write past the
	// guard entirely.
	if len(b.pending) > babbleWindow {
		b.endLine()
	}
	if b.since < babbleEvery {
		return false
	}
	b.since = 0
	return b.tripped()
}

// grow adds one piece of the line being written, and counts it towards the next
// test.
//
// THE COUNTER IS BYTES OF WATCHED TEXT and not bytes of settled line, because a
// model in a repetition loop may never press return: the first version counted
// only completed lines, and an excerpt of the real 2026-08-20 soup — fifteen
// hundred runes with not one newline in them — streamed past the guard because
// the test was never due. Text inside a fence is not watched and is not counted,
// so a ten-megabyte code block costs one test rather than twenty thousand.
func (b *babbleWatch) grow(piece string) {
	b.pending = append(b.pending, piece...)
	if !b.inFence {
		b.since += len(piece)
	}
}

// endLine settles the pending line: it either toggles the fence, or joins the
// clean tail.
func (b *babbleWatch) endLine() {
	line := b.pending
	b.pending = nil
	if kind, ok := fenceLine(line); ok {
		switch {
		case b.inFence && kind != fenceOpen:
			b.inFence = false
			return
		case !b.inFence && kind != fenceClose:
			b.inFence = true
			return
		}
	}
	if b.inFence {
		return
	}
	b.clean = append(b.clean, line...)
	if len(b.clean) > cleanCap {
		b.clean = append(b.clean[:0], b.clean[len(b.clean)-cleanCap:]...)
	}
}

// window is the text the two tests read: the clean tail plus whatever line is
// still being written, when that line is not inside a fence.
func (b *babbleWatch) window() []byte {
	if b.inFence || len(b.pending) == 0 {
		return b.clean
	}
	return append(append(make([]byte, 0, len(b.clean)+len(b.pending)), b.clean...), b.pending...)
}

func (b *babbleWatch) tripped() bool {
	window := b.window()
	return b.loopedTail(window) || churnedTail(window)
}

type fenceKind int

const (
	fenceOpen fenceKind = iota
	fenceClose
	fenceEither
)

// fenceLine reports whether one line is a markdown code fence, and which end.
//
// A CLOSER IS BARE and an OPENER MAY CARRY A LANGUAGE TAG, and the tag has to
// look like one: up to twenty characters of the alphabet a language name is
// spelled in, and nothing else. Everything looser was tried against the real
// degenerate text and let it hide.
func fenceLine(line []byte) (fenceKind, bool) {
	trimmed := bytes.TrimSpace(line)
	if !bytes.HasPrefix(trimmed, []byte("```")) {
		return fenceEither, false
	}
	info := trimmed[3:]
	if len(info) == 0 {
		return fenceEither, true
	}
	if len(info) > 20 {
		return fenceEither, false
	}
	for _, r := range string(info) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '+', r == '#', r == '-', r == '_':
		default:
			return fenceEither, false
		}
	}
	return fenceOpen, true
}

// loopedTail is the COMPRESSION COLLAPSE test: a window that zlib squeezes below
// babbleFloor is a repetition of some cycle, and the cycle's length does not
// matter to the answer.
//
// The window must be FULL before this may say anything. A short reply that
// happens to be one repeated line is somebody answering "no, no, no" and is not
// a model that has come off the rails.
func (b *babbleWatch) loopedTail(window []byte) bool {
	if len(window) < babbleWindow {
		return false
	}
	tail := window[len(window)-babbleWindow:]
	b.counter.n = 0
	if b.squeeze == nil {
		b.squeeze = zlib.NewWriter(&b.counter)
	} else {
		b.squeeze.Reset(&b.counter)
	}
	if _, err := b.squeeze.Write(tail); err != nil {
		return false
	}
	if err := b.squeeze.Close(); err != nil {
		return false
	}
	return float64(b.counter.n)/float64(len(tail)) < babbleFloor
}

// countingWriter is how many bytes the compressor produced. The compressed
// bytes themselves are never wanted, so they are never kept.
type countingWriter struct{ n int }

func (c *countingWriter) Write(p []byte) (int, error) {
	c.n += len(p)
	return len(p), nil
}

var _ io.Writer = (*countingWriter)(nil)

// churnedTail is the SCRIPT CHURN test: it counts alphabet switches BETWEEN TWO
// ADJACENT LETTERS, which is the difference between a multilingual answer and
// soup.
//
// A person writing about two languages puts a space, a comma or a quote between
// them — "the Russian is компьютер" switches alphabet at a boundary, and that
// switch is not counted here at all. A model that has lost the thread writes
// "стаthisada", where the switch is inside the word, and that is the only kind
// this counts.
//
// Latin, Han, Kana and Hangul are mutually TOLERANT, because CJK writing sets
// Latin words against native script with no separator as a matter of course:
// "このAPIはHTTPリクエストを受け取り" is ordinary Japanese and scored 27 switches
// per hundred runes before the tolerance existed.
func churnedTail(window []byte) bool {
	// The window is walked rather than materialized. Decoding it into a []rune
	// first cost sixteen kilobytes of garbage on every test to read six hundred
	// runes off the end of it, and a byte that is not valid UTF-8 becomes the
	// same replacement rune either way — which is what keeps this the same
	// measurement it was.
	total := utf8.RuneCount(window)
	if total < churnWindow {
		return false
	}
	tail := window
	for skip := total - churnWindow; skip > 0; skip-- {
		_, size := utf8.DecodeRune(tail)
		tail = tail[size:]
	}
	var counts [scriptCount]int
	switches := 0
	previous := scriptNeutral
	for len(tail) > 0 {
		r, size := utf8.DecodeRune(tail)
		tail = tail[size:]
		class := scriptOf(r)
		if class == scriptNeutral {
			previous = scriptNeutral
			continue
		}
		counts[class]++
		if previous != scriptNeutral && previous != class && !(tolerant(previous) && tolerant(class)) {
			switches++
		}
		previous = class
	}
	present := 0
	for _, count := range counts {
		if count >= churnRunes {
			present++
		}
	}
	if present < churnScripts {
		return false
	}
	return float64(switches)*100/float64(churnWindow) >= churnBound
}

// scriptClass is one alphabet, coarsely. Digits, punctuation, whitespace, marks
// and emoji are scriptNeutral: they belong to no alphabet and they BREAK a run,
// so a letter on either side of one is never a mid-word switch.
type scriptClass uint8

const (
	scriptNeutral scriptClass = iota
	scriptLatin
	scriptGreek
	scriptCyrillic
	scriptArmenian
	scriptHebrew
	scriptArabic
	scriptDevanagari
	scriptThai
	scriptKana
	scriptHan
	scriptHangul
	scriptOther
	// scriptCount is the width of [churnedTail]'s tally and not an alphabet. It
	// must stay last in this block, which is what makes the tally an array
	// rather than a map allocated once per test.
	scriptCount
)

// tolerant names the alphabets that legitimately sit against each other with no
// separator. See [churnedTail].
func tolerant(class scriptClass) bool {
	switch class {
	case scriptLatin, scriptHan, scriptKana, scriptHangul:
		return true
	}
	return false
}

func scriptOf(r rune) scriptClass {
	switch {
	case r < utf8.RuneSelf:
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return scriptLatin
		}
		return scriptNeutral
	case r >= 0x00C0 && r <= 0x024F, r >= 0x1E00 && r <= 0x1EFF:
		return scriptLatin
	case r >= 0x0370 && r <= 0x03FF, r >= 0x1F00 && r <= 0x1FFF:
		return scriptGreek
	case r >= 0x0400 && r <= 0x052F:
		return scriptCyrillic
	case r >= 0x0530 && r <= 0x058F:
		return scriptArmenian
	case r >= 0x0590 && r <= 0x05FF:
		return scriptHebrew
	case r >= 0x0600 && r <= 0x06FF, r >= 0x0750 && r <= 0x077F:
		return scriptArabic
	case r >= 0x0900 && r <= 0x097F:
		return scriptDevanagari
	case r >= 0x0E00 && r <= 0x0E7F:
		return scriptThai
	case r >= 0x3040 && r <= 0x30FF:
		return scriptKana
	case r >= 0x3400 && r <= 0x4DBF, r >= 0x4E00 && r <= 0x9FFF, r >= 0xF900 && r <= 0xFAFF:
		return scriptHan
	case r >= 0x1100 && r <= 0x11FF, r >= 0xAC00 && r <= 0xD7AF:
		return scriptHangul
	case r >= 0xFF21 && r <= 0xFF5A:
		// Fullwidth Latin. It is the same alphabet typed on a CJK keyboard.
		return scriptLatin
	case unicode.IsLetter(r):
		return scriptOther
	}
	return scriptNeutral
}
