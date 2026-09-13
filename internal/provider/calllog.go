package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/calllog"
	"github.com/Agent-Field/aforge-v2/internal/trace"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The adapter's end of aforge's model-call log (internal/calllog says why it
// exists at all).
//
// IT IS WRITTEN HERE BECAUSE EVERY OUTBOUND CALL IN THE PROCESS PASSES THROUGH
// HERE. The chat's turn, `aforge do`, a plan's briefs and contracts, the
// delivery gate, a reflex, the document route — they are a dozen packages that
// share exactly one door, and a record written at that door cannot be missed by
// a caller who forgot to write one.
//
// ONE BUILDER, TWO PATHS. The non-streamed completion and the streamed one
// finish in different functions with different facts in hand, and a record
// assembled separately in each would have drifted the first time a field was
// added. Everything below funnels through [Client.record], which reads each
// field from where the adapter had already worked it out — the ceiling from
// thinking.go, the effort from the encoder's own resolvers, the serving
// endpoint from the decode, the relax rungs from the ladder — and computes
// nothing of its own.

// The names a record's `learned` field uses for the quirks one answer taught.
// They are constants rather than literals at the four sites that write them,
// because a name that appears twice is a name that will one day be spelled two
// ways, and this one is what a person greps the log for.
const (
	learnedReasoningMandatory      = "reasoning_mandatory"
	learnedReasoningDisableIgnored = "reasoning_disable_ignored"
	learnedCacheControlRefused     = "cache_control_refused"
	learnedReasoningBudgetRefused  = "reasoning_budget_refused"
	learnedReasoningReplayRefused  = "reasoning_replay_refused"
)

type callTagKey struct{}
type callNodeKey struct{}

// WithCallTag names what a call is FOR — "turn", "leaf", "compile", "gate",
// "reflex" — for the one reader that cannot work it out for itself: a person
// reading the log a fortnight later.
//
// It rides the context for the reason patience does (patience.go): the adapter
// underneath is shared by every agent in the process, so the call is the only
// thing that knows whose call it is.
//
// A call that sets no tag is not untagged if it opened a routing slot: the tag
// falls back to that slot's class, shortened to its own last word (callTag), so
// `plan.brief` reads as "brief" without anybody spelling "brief" twice.
func WithCallTag(ctx context.Context, tag string) context.Context {
	if tag = strings.TrimSpace(tag); tag == "" {
		return ctx
	}
	return context.WithValue(ctx, callTagKey{}, tag)
}

// WithCallNode names the work a call belongs to — a plan node's key, a task's
// id — so a log full of leaf calls can be read one node at a time. It is
// separate from the tag because the two are known in different places: the tag
// is a fact about the code making the call, the node a fact about the work.
func WithCallNode(ctx context.Context, node string) context.Context {
	if node = strings.TrimSpace(node); node == "" {
		return ctx
	}
	return context.WithValue(ctx, callNodeKey{}, node)
}

// callTag is what this call was for: the explicit tag when one was set, and
// otherwise the routing class's own last word — `plan.contract` becomes
// "contract", `exec.leaf` becomes "leaf". Deriving it rather than asking the
// planning packages to repeat themselves is what keeps the two from disagreeing.
func callTag(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if tag, _ := ctx.Value(callTagKey{}).(string); tag != "" {
		return tag
	}
	class := string(CallClassFrom(ctx))
	if dot := strings.LastIndex(class, "."); dot >= 0 {
		return class[dot+1:]
	}
	return class
}

func callNode(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	node, _ := ctx.Value(callNodeKey{}).(string)
	return node
}

// callTrace is what one CALL — not one attempt — accumulates as it is retried,
// repaired and relaxed on its way to an answer. It hangs off callKnobs by
// pointer so that it survives the copies the repair chain makes of them, which
// is the only way the completed record at the end can say the call took three
// attempts and taught the adapter two things on the way.
type callTrace struct {
	// connectionRecovered excludes a locally interrupted call from provider timing.
	connectionRecovered bool
	// attempts is how many times the transport actually put this call on the
	// wire, across every retry and every repaired shape.
	attempts int
	// learned is every quirk this call's refusals taught, in the order they
	// were learned.
	learned []string
	// attemptID pairs the START row the transport wrote as the current attempt
	// went out with whichever row ends it. It is replaced per attempt, so an end
	// row written after the transport returned always names the attempt that
	// actually produced the answer.
	attemptID string
	// open is the attempt that has a start row on the wire and nothing under it
	// yet, and nil whenever the log is square.
	//
	// IT IS WHAT MAKES A FINISH ROW UNCONDITIONAL. Every path in this package
	// that ends an attempt writes its row, and the file says so in five separate
	// comments — and on 527 of 16,921 attempts over the ten days to 2026-09-10
	// it did not happen, because an exit nobody had thought of returned past all
	// of them. A law kept by every path remembering to keep it is a law with one
	// counter-example per exit, so it is kept HERE instead: the transport
	// remembers what it opened and closes whatever is still open
	// ([Client.closeOpenAttempt]).
	//
	// IT IS NOT GUARDED BY A LOCK and does not need one. A trace belongs to ONE
	// call — [knobsFrom] mints a fresh one per entry, and each arm of a race
	// enters on its own context and gets its own — and the transport under it is
	// sequential.
	open *openAttempt
	// conn is what the transport had to do to get the CURRENT attempt onto the
	// wire — whether the pool already had a connection, and where the time went
	// when it did not. It is replaced per attempt for [callTrace.attemptID]'s
	// reason: a row about attempt two may not carry attempt one's handshake.
	//
	// THE POINTER NEEDS NO LOCK FOR `open`'s REASON ABOVE — a trace belongs to
	// one call and the transport under it is sequential. The mutex inside
	// [connFacts] is a different question and a real one: those fields are
	// written by `httptrace` hooks on the transport's own dial goroutines.
	conn *connFacts
	// body is the request as it was last encoded, kept ONLY when somebody
	// asked for it: the old bodies pin, which puts it on the line of the
	// model-call log, or the debug record, which is where bodies live now
	// (recordBodies). It is nil on every ordinary run, which is what keeps a
	// person's prompts out of a file they did not ask to have them in.
	body []byte
}

func newCallTrace() *callTrace { return &callTrace{} }

// openAttempt is one attempt that went out and has not been written down yet:
// everything the closing row would need if nothing else ever writes one.
//
// It keeps the request and the knobs AS THEY WERE WHEN THE ATTEMPT WENT OUT,
// because the ladder relaxes knobs between rungs and a row built from the
// current ones would describe a request that never travelled.
type openAttempt struct {
	id      string
	attempt int
	began   time.Time
	request *ai.Request
	knobs   callKnobs
	stream  bool
}

// The words [calllog.Record.Ended] uses. They are constants for the reason the
// `learned` names are: a value a person greps a log for may not be spelled two
// ways, and these four are the whole vocabulary.
//
// THEY DESCRIBE WHO CLOSED THE ROW AND NOT WHAT THE CALL MEANT. Why a call
// failed is the taxonomy's business and is on the row already; this says only
// that the path which ended this attempt did not write its own row.
const (
	// endedHopped is another attempt beginning before this one was written —
	// a retry, a repaired shape, a ladder rung.
	endedHopped = "hopped"
	// endedCancelled and endedDeadline are the caller's own context ending the
	// call: a hedge loser, a person's stop key, a role's patience.
	endedCancelled = "cancelled"
	endedDeadline  = "deadline"
	// endedAbandoned is the one that should never happen and did: the call
	// returned, the context is fine, and nothing wrote the row.
	endedAbandoned = "abandoned"
)

// track is what one row this trace wrote does to the open attempt: a start row
// opens one, and any other row about the same attempt closes it.
func (t *callTrace) track(facts recordFacts, id string) {
	if t == nil {
		return
	}
	if facts.phase != calllog.PhaseStart {
		if t.open != nil && t.open.id == id {
			t.open = nil
		}
		return
	}
	t.open = &openAttempt{
		id:      id,
		attempt: facts.attempt,
		began:   facts.began,
		request: facts.request,
		knobs:   facts.knobs,
		stream:  facts.stream,
	}
}

// closeOpenAttempt writes the row for an attempt nothing else wrote, and does
// nothing at all when the log is already square — which is every ordinary call.
//
// It is called from two places and they are the only two: the moment a new
// attempt begins ([Client.record], where the word is "hopped"), and the way out
// of the transport's own doors, where the word comes from the caller's context.
func (c *Client) closeOpenAttempt(ctx context.Context, trace *callTrace, ended string) {
	if trace == nil || trace.open == nil {
		return
	}
	open := trace.open
	trace.open = nil
	c.record(recordFacts{
		ctx: ctx, request: open.request, knobs: open.knobs, stream: open.stream,
		attempt: open.attempt, began: open.began, ended: ended, id: open.id,
	})
}

// endedBy is the word for a call that came back to its door with an attempt
// still open. The caller's own context is the only thing that can say more than
// "nobody wrote it".
func endedBy(ctx context.Context) string {
	if ctx == nil || ctx.Err() == nil {
		return endedAbandoned
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return endedDeadline
	}
	return endedCancelled
}

// begin opens one attempt: a fresh pairing token, and one more on the count.
// The token is minted here rather than at the write so that every row about
// this attempt — the start, and whichever of the four ends it — names the same
// call.
func (t *callTrace) begin() {
	if t == nil {
		return
	}
	t.attempts++
	t.attemptID = calllog.NewID()
}

func (t *callTrace) note(learned ...string) {
	if t == nil {
		return
	}
	t.learned = append(t.learned, learned...)
}

// recordFacts is everything one row is built from, gathered by whichever path
// finished the call. Every field is optional: a call that never reached an
// endpoint has no status and no tokens, and the emptiness law leaves both off
// the line rather than writing a zero somebody could read as a measurement.
type recordFacts struct {
	ctx     context.Context
	request *ai.Request
	knobs   callKnobs
	stream  bool
	// attempt is the 1-based attempt this row is about, on a row about ONE
	// attempt. A row about the whole call leaves it to the trace's count.
	attempt int
	began   time.Time
	status  int
	served  string
	err     error
	// ttft is how long this attempt's first token really took, on the paths
	// that know it for themselves.
	//
	// THE WATCH'S OWN FIGURE IS ONLY EVER ON A RACED CALL, and a stream that was
	// cut or refused halfway through is exactly the row where the figure matters
	// and exactly the row a race may not have been running on. A failed attempt
	// that produced a first token and then died is a machine that is ALIVE and
	// slow; one that produced nothing is a path that never opened, and nothing
	// else on the row separates them.
	ttft time.Duration
	// retryAfter is the comeback time a refusal named, where it named one. It is
	// carried rather than derived because the header is read once, by the loop
	// that hands it to the limiter, and re-reading a body that has been drained
	// is not possible (retry.go).
	retryAfter time.Duration
	// response is the assembled answer, on the row that has one.
	response        *ai.Response
	reasoningTokens int
	// learned is what THIS row's answer taught, which is not the same as what
	// the call has learned so far: a repaired 400 carries its own lesson.
	learned      []string
	responseBody []byte
	// reasoning is the working the endpoint sent on a channel of its own, kept
	// only by the streamed path and only while somebody is recording — a
	// streamed answer has no whole body to read it back out of afterwards
	// (client.go's stream loop).
	reasoning string
	// phase is calllog.PhaseStart on the row written as a call goes out, and
	// empty on the row that ends it.
	phase string
	// ended is set ONLY on a row the transport wrote because nothing else did
	// (calllog.Record.Ended).
	ended string
	// id names the attempt this row is about when it is NOT the trace's current
	// one. A closing row is written after the attempt it is about has been
	// overtaken, so reading the id off the trace would pair it with the wrong
	// start row — which is the very mis-pairing the closing row exists to end.
	id string
}

// logNow is the wall clock the model-call log stamps its rows from, and it is
// deliberately NOT the client's seamed [Client.clock].
//
// That seam exists so a test can state a two-second first token without waiting
// two seconds, and what it usually holds is a scripted list of instants. A
// bookkeeping line that read one would silently spend a tick the measurement
// under test was counting — which is exactly what it did the first time this
// was written the obvious way. The log is an account of what happened in the
// world, and it reads the world's own clock.
func logNow() time.Time { return time.Now() }

// record writes one row. It is the only writer, and it never fails a call:
// everything under it is best-effort by construction (internal/calllog).
func (c *Client) record(facts recordFacts) {
	// A NEW ATTEMPT CLOSES THE ONE BEFORE IT. Reaching here with a start row
	// while another attempt is still open means that attempt ended and its path
	// did not write it down — a retry, a repaired shape, a ladder rung — and the
	// row goes out now rather than never (callTrace.open).
	if facts.phase == calllog.PhaseStart {
		c.closeOpenAttempt(facts.ctx, facts.knobs.trace, endedHopped)
	}
	// AND WHOEVER ASKED TO WATCH THIS CALL RUN IS TOLD FROM THE SAME TWO ROWS
	// (callprogress.go). This door is the only writer the log has and its law is
	// that every attempt gets a start row and exactly one row that ends it — so
	// it is also the only place in the process that can say, once and honestly,
	// that a request went out and how it came back. The token counts between the
	// two come from the read loop's own watch, which is where they are already
	// counted. One nil check on every call nobody is watching.
	//
	// AN ATTEMPT'S ENDING IS NOT THE CALL'S, which is why the second of these is
	// `landed` and not an ending: this row is written again for every retry,
	// every repaired shape and every rung of the ladder, and no row here can know
	// whether another attempt is coming. The ending is said by the door the
	// request returns through ([streamWatch.callFinished]).
	if facts.phase == calllog.PhaseStart {
		streamWatchFrom(facts.ctx).callOpened(facts.began)
	} else {
		streamWatchFrom(facts.ctx).callLanded(callEndOf(facts), facts.err)
	}
	// Built even when the file is off: the log's in-memory half (calllog.Last)
	// is what the headless waiting line reads, and the file's own switch is
	// read where the file is written.
	model := c.modelFor(facts.request)
	record := calllog.Record{
		Time: logNow().Format("2006-01-02T15:04:05.000Z07:00"),
		// The run this call belongs to, read off the context the caller
		// threaded — the same id that names the debug record's folder and that
		// the headless verbs publish in their `--json` envelope. A call made
		// with no run on its context falls back to this process's own, which is
		// exactly what the record does (internal/trace's RunFrom).
		Run:       trace.RunFrom(facts.ctx),
		Phase:     facts.phase,
		Tag:       callTag(facts.ctx),
		Node:      callNode(facts.ctx),
		Model:     model,
		Served:    recordedServed(facts),
		Effort:    c.recordedEffort(model, facts.knobs),
		EffortPin: c.recordedEffortPin(model, facts.knobs),
		// The ceiling that TRAVELLED, from the one function that works it out
		// for the encoder and the transport alike (thinking.go's ceilingFor).
		// The caller's own figure is deliberately not here: the gap between the
		// two is the thinking-pass economy, and a log that showed the smaller
		// number would explain nothing about a reply that came back empty.
		MaxTokens:      c.recordedCeiling(facts.request, facts.knobs),
		Messages:       len(facts.request.Messages),
		Tools:          len(facts.request.Tools),
		Stream:         facts.stream,
		Attempt:        facts.attempt,
		Relaxed:        c.relaxNames(model, facts.knobs),
		Status:         facts.status,
		Millis:         logNow().Sub(facts.began).Milliseconds(),
		Learned:        facts.learned,
		EmptyAtCeiling: c.emptyAtCeiling(facts.request, facts.response),
	}
	if facts.err != nil {
		record.Error = calllog.ClipError(namedCancel(facts.ctx, facts.err))
	}
	// ── A FAILURE IS PRICED LIKE AN ANSWER, BECAUSE IT WAS BILLED LIKE ONE
	//
	// This block used to run only on the row that carried a whole answer, so a
	// stream cut at eighteen thousand tokens, a 429 delivered after a 200 had
	// opened and a connection torn halfway through a reply each left a row
	// saying the call cost nothing. Over the ten days to 2026-09-10 that was
	// 1,884 of 1,887 in-stream failures: $201.15 of recorded spend with $0.00
	// attributed to anything that went wrong (docs/design/recovery/census-
	// 20260910.md §8, finding 6).
	//
	// The tokens were generated and the provider counted them. Whether this
	// process could USE the answer is a different question from what it cost,
	// and only one of the two is a fact about the money.
	if facts.response != nil {
		record.Finish = FinishReason(facts.response)
		if usage := facts.response.Usage; usage != nil {
			record.PromptTokens = usage.PromptTokens
			record.CompletionTokens = usage.CompletionTokens
			record.CachedTokens = usage.CacheReadTokens()
			if usage.Cost != nil {
				record.Cost = *usage.Cost
			}
		}
		record.ReasoningTokens = facts.reasoningTokens
	}
	if facts.ttft > 0 {
		record.TTFTms = facts.ttft.Milliseconds()
	}
	if facts.retryAfter > 0 {
		record.RetryAfterS = facts.retryAfter.Seconds()
	}
	// ── WHY IT WAITED AND WHAT WAS DONE ABOUT IT
	//
	// The controller is what knows: which machine was asked for, when it was
	// going to be acted on, how long the silence had really run, what was done,
	// and what the arms that did not answer cost. Read from the watch driving
	// THIS attempt, so that the row of a rescue says what the rescue did and
	// the row of the request it rescued says what happened to that one.
	if wait, watched := streamWatchFrom(facts.ctx).facts(); watched {
		record.Lane = recordedLane(facts.knobs, wait.lane)
		record.HazardCeilingMs = wait.deadline.Milliseconds()
		// WHAT WAS PLANNED AND WHAT HAPPENED ARE TWO FIELDS, AND THE ROW MAY
		// CARRY BOTH. The ceiling above is when the watch was going to start
		// thinking about a second machine; these two are the bound that actually
		// ended the attempt, and they stay empty on every attempt no bound of
		// ours ended, which is almost all of them.
		record.AppliedMs = wait.applied.Milliseconds()
		record.AppliedWord = wait.appliedWord
		// AN ARM THAT LOST A RACE IS EXHAUST AND NOT A FAILURE, and its row is
		// the only place that can say so: the error it carries is `context
		// canceled`, indistinguishable in the file from a caller walking away.
		record.Exhaust = wait.exhaust
		if record.TTFTms == 0 {
			// The watch's reading is the FALLBACK and not the source. A raced
			// call has both; an unwatched one has only what the stream loop
			// measured for itself, and a row whose own figure was overwritten
			// by a zero from a watch that never saw a token would be a first
			// token this build had and threw away.
			record.TTFTms = wait.ttft.Milliseconds()
		}
		record.SilenceMs = wait.silence.Milliseconds()
		record.Action = wait.action
		record.Reason = wait.reason
		record.Refused = wait.refused
		// THE TWO FIGURES ARE WRITTEN ONLY WHEN THERE ARE TWO FIGURES. A wait
		// nobody could price and a rescue that had nowhere to go are both
		// NOTHING, and under the emptiness law a row with nothing to say about
		// a number says nothing — it does not carry a sentence explaining a
		// float (internal/calllog's finite.go, which is now the last line of
		// defence rather than the ordinary road).
		if seconds, known := wait.wait.Get(); known {
			record.WaitS = seconds
		}
		if seconds, known := wait.cost.Get(); known {
			record.CostS = seconds
		}
		record.Hedged = wait.hedged
		record.WasteUSD = wait.waste
		record.Note = wait.note
		if wait.arms > 1 {
			record.Arms = wait.arms
		}
	}
	if facts.knobs.trace != nil {
		record.ID = facts.knobs.trace.attemptID
	}
	if facts.id != "" {
		record.ID = facts.id
	}
	// WHAT THE CONNECTION ITSELF COST, on the row that ends the attempt it was
	// measured on. It is stamped after the id is settled because the facts are
	// filed under the attempt they belong to and a closing row is written about
	// an attempt that has already been overtaken (see [recordFacts.id]).
	if facts.phase != calllog.PhaseStart && facts.knobs.trace != nil {
		facts.knobs.trace.stampConn(&record)
	}
	if facts.attempt == 0 && facts.knobs.trace != nil {
		// A row about the whole call says how many times it went out; a row
		// about one attempt already said which attempt it was.
		record.Attempt = facts.knobs.trace.attempts
	}
	if calllog.Bodies() && calllog.Path() != "" {
		if facts.knobs.trace != nil {
			record.RequestBody = string(facts.knobs.trace.body)
		}
		record.ResponseBody = string(facts.responseBody)
	}
	record.Ended = facts.ended
	facts.knobs.trace.track(facts, record.ID)
	calllog.Append(record)
	c.recordBodies(facts, record, model)
}

// recordBodies puts the whole request and the whole answer in the debug record
// (internal/trace), and it is written HERE for the reason the log above is:
// every outbound call in the process passes through this one door, so a body
// cannot be missed by a caller who forgot to keep one.
//
// The two records are deliberately different shapes. The log is the INDEX — one
// line per call, always on, holding none of the person's own words — and the
// record is the bodies, kept only for the run somebody switched it on for. The
// recorder is asked FIRST, because it answers nil in two atomic loads on every
// run nobody is debugging and everything below it costs real work.
//
// ONLY THE ROW THAT ENDS AN ATTEMPT WRITES A BODY. The start row is written
// before the wire has said anything, and its body file would be overwritten by
// its own successor a moment later — the same record, paid for twice.
func (c *Client) recordBodies(facts recordFacts, record calllog.Record, model string) {
	recorder := trace.For(facts.ctx)
	if recorder == nil || facts.phase == calllog.PhaseStart {
		return
	}
	body := trace.CallBody{
		CallID:    record.ID,
		Node:      record.Node,
		Model:     model,
		Response:  facts.responseBody,
		Finish:    record.Finish,
		Reasoning: facts.reasoning,
	}
	if facts.knobs.trace != nil {
		body.Request = facts.knobs.trace.body
	}
	if facts.err != nil {
		// UNCLIPPED, unlike the log's own field. The reason to keep a record at
		// all is that the exact words of the refusal are what is wrong, and a
		// sentence cut at the log's width is a sentence somebody has to go back
		// to the provider to finish reading.
		body.Error = namedCancel(facts.ctx, facts.err)
	}
	if len(body.Response) == 0 && facts.response != nil {
		// A STREAMED ANSWER HAS NO WHOLE BODY. It arrived as hundreds of frames
		// and was assembled as it came, so what the record keeps is the
		// ASSEMBLED reply — the same answer, in one document. Without this the
		// commonest call this build makes would leave a record with a request
		// in it and nothing that came back.
		if assembled, err := json.Marshal(facts.response); err == nil {
			body.Response = assembled
		}
	}
	recorder.Call(facts.ctx, body)
}

// firstTokenAfter is how long this attempt waited for its first token, and zero
// when no token ever came. Zero is the honest answer there and the emptiness
// law leaves it off the row: a stream that never wrote is not a stream whose
// first token was instant, and the difference is the difference between a
// machine that is alive and slow and a path that never opened.
//
// IT IS MEASURED ON THE CLIENT'S OWN SEAM and not on the log's wall clock, for
// the same reason the two are separate at all (logNow): both instants come from
// the stream loop, which a test scripts, and subtracting a scripted instant
// from a real one would be a measurement of the test harness.
func firstTokenAfter(began, first time.Time) time.Duration {
	if began.IsZero() || first.IsZero() || !first.After(began) {
		return 0
	}
	return first.Sub(began)
}

// recordedServed is the machine that ANSWERED this attempt, and nothing at all
// when nobody can say who that was.
//
// TWO SOURCES AND NEVER A THIRD.
//
//  1. The stream's own `provider` field, which is the machine naming itself.
//  2. The one machine this request DEMANDED, when it demanded exactly one: a
//     router asked for a single-machine `only` either answers from that machine
//     or refuses, so the demand and the answer are the same fact by
//     construction. This is what puts a name on the row of a 429 — a refusal
//     from a pool that never opened a stream to name itself.
//
// THE LANE IS NOT A SOURCE, AND THAT IS THE LAW THIS FUNCTION EXISTS TO STATE.
// `lane` is who the preference ASKED FOR. The two disagreed on 3,728 of 10,107
// finishes over the ten days to 2026-09-10, and every per-lane belief this
// build holds — velocity, strikes, pacing, the sheet's own uptime — has been
// written against the asked-for name, which is how a machine gets struck for
// another machine's refusal (docs/design/recovery/DESIGN.md §1's third
// reading). Filling an empty `served` with `lane` would make that disagreement
// permanently invisible, so it is never done.
func recordedServed(facts recordFacts) string {
	if served := strings.TrimSpace(facts.served); served != "" {
		return served
	}
	return soleDemandedLane(facts.knobs)
}

// soleDemandedLane is the one machine THIS ATTEMPT'S BODY was pinned to, and ""
// when it was free to be routed anywhere. A rescue demands the single arm it
// walked to (hedge.go's [hedgePreference]) and a person's pin is one machine
// (lanes.go), so either is a commitment the router cannot answer around.
//
// AND AN ATTEMPT THAT HAS WITHDRAWN THE DEMAND HAS NO COMMITMENT LEFT, which is
// the half this used to get wrong. The knobs travel by value through the widen
// and the ladder, so the choice that named the machine is still sitting on them
// after the encoder has stopped sending it ([relaxedPreferences] takes `only`
// off) — and the bare retry of a retired pin was logged `served: DeepSeek` over
// a body with no `provider` key at all, which is the one machine that certainly
// did not answer it. The demand is read the way the encoder reads it or not at
// all.
func soleDemandedLane(knobs callKnobs) string {
	if !knobs.carriesTheDemand() {
		return ""
	}
	if lane := strings.TrimSpace(knobs.hedgeLane); lane != "" {
		return lane
	}
	if knobs.laneChoice != nil && len(knobs.laneChoice.Only) == 1 {
		return strings.TrimSpace(knobs.laneChoice.Only[0])
	}
	return ""
}

// recordedLane is the machine THIS ATTEMPT'S preference asked for: the head of
// the order it sent, or the one machine it demanded.
//
// THE WATCH KNOWS WHAT THE CALL CHOSE AND NOT WHAT EACH ATTEMPT SENT
// (waitreport.go's askedLane), and the two part company exactly once: on the
// rung that widens. It takes `only` and `allow_fallbacks` off and leaves
// `order` standing, so a lane the choice merely RANKED is still this body's ask
// and a lane it DEMANDED is not — and a pinned request's bare retry was
// reported under the name of the machine whose refusal it was recovering from.
func recordedLane(knobs callKnobs, asked string) string {
	if asked == "" || knobs.carriesTheDemand() {
		return asked
	}
	if knobs.laneChoice != nil && len(knobs.laneChoice.Order) > 0 && !knobs.noProvider {
		return asked
	}
	return ""
}

// builtString reads a builder that may never have been made. The streamed
// path only builds one while a record is open, and the emptiness law is the
// same in a struct literal as it is on a screen: nothing recorded is nothing
// written, not an empty field somebody could read as a model that said nothing.
func builtString(builder *strings.Builder) string {
	if builder == nil {
		return ""
	}
	return builder.String()
}

// namedCancel is the error a row carries, with WHOEVER CANCELLED THE CALL on it.
//
// `context canceled` is the same eight characters for a person's stop key, for
// a window taking a conversation over, for a session shutting down and for a
// hedge arm that lost its race — and a row that cannot tell them apart is a row
// nobody can autopsy. It was exactly what a healthy 109-second reply left
// behind on 2026-09-09 when something ended it and nothing said what.
//
// Whoever cancels attaches a cause ([context.WithCancelCause]; internal/
// session's stopcause.go is where this build's turn doors do it), and a cause
// propagates to every child context — which is what makes this readable from
// the arm rather than only from the turn. A cancellation NOBODY named reads as
// itself, because inventing a door would be worse than the eight characters.
func namedCancel(ctx context.Context, err error) string {
	said := err.Error()
	if ctx == nil || !errors.Is(err, context.Canceled) {
		return said
	}
	switch cause := context.Cause(ctx); {
	case cause == nil, errors.Is(cause, context.DeadlineExceeded):
		return said
	case cause.Error() == context.Canceled.Error():
		// Nobody named a door: [context.Cause] answers the plain cancellation
		// for a context cancelled without one, and repeating it would be a row
		// saying the same thing twice.
		return said
	default:
		return said + " (" + cause.Error() + ")"
	}
}

// recordedEffort is the reasoning knob AS IT TRAVELLED, spelled the way the
// encoder decided it — not the way the caller asked. The two differ on every
// model whose endpoint refuses a disable, which is exactly the case a person
// reads this log to understand.
func (c *Client) recordedEffort(model string, knobs callKnobs) string {
	if knobs.relaxed.has(relaxReasoning) {
		// The ladder took the knob off the body altogether, so the model ran at
		// its own published default and this request said nothing about it.
		return ""
	}
	effort := c.resolveEffort(model, knobs.effort)
	if budget := c.resolveReasoningBudget(model, knobs.effort); budget > 0 {
		// A BUDGET THE WALL DERIVED IS NAMED BY THE WALL, never by its count.
		// This string is also the key the thinking-duration belief is filed
		// under (waitplan.go's Think, client.go's NoteThought), and a count
		// read off a rate that moves with every answer would file each walled
		// thought under a rung nobody will ever ask about again.
		if asked := c.effortAsked(knobs.effort); budget != asked.budget {
			return fmt.Sprintf("%s within %s", effort, asked.wall)
		}
		return fmt.Sprintf("%s %d tokens", effort, budget)
	}
	if effort == EffortNone {
		return ""
	}
	return string(effort)
}

// recordedEffortPin is the client's pinned effort when this call did not carry
// it, and nothing at all when the pin travelled or the client has no pin. It
// compares against [Client.recordedEffort], the encoder's own reading, so a
// relaxed request and a model that replaces an unsupported disable are named
// from what went over the wire rather than from what the caller first wanted.
func (c *Client) recordedEffortPin(model string, knobs callKnobs) string {
	pin := string(c.config.Effort)
	if pin == "" || c.recordedEffort(model, knobs) == pin {
		return ""
	}
	return pin
}

// recordedCeiling is the max_tokens the request carried, or nothing when it
// carried none.
func (c *Client) recordedCeiling(request *ai.Request, knobs callKnobs) int {
	ceiling, carried := c.ceilingFor(request, knobs)
	if !carried {
		return 0
	}
	return ceiling
}

// emptyAtCeiling is the thinking-ate-the-answer signature, judged against the
// CALLER'S figure exactly as the adapter's own recovery judges it
// (learnFromAnswer). It is the one field of the record that is a conclusion
// rather than a fact, and it is the conclusion the log exists to make easy.
func (c *Client) emptyAtCeiling(request *ai.Request, response *ai.Response) bool {
	if request == nil || request.MaxTokens == nil || response == nil {
		return false
	}
	return EmptyAtCeiling(response, *request.MaxTokens)
}

// relaxNames spells the rungs a body has already climbed, in the ladder's own
// words (endpoints.go's relaxRungs). It is the same table the retry line a
// person watches is built from, so the log and the surface can never call the
// same rung two things.
//
// AND A RUNG THAT TAKES OFF MORE THAN ONE FIELD NAMES THE ONES THIS BODY HAD
// (endpoints.go's [relaxStep.took]). The row is read to find out what changed
// about a request, so a rung's general word is only worth writing where it is
// also the particular truth — and the widening rung's was not, on every pinned
// request there has ever been.
func (c *Client) relaxNames(model string, knobs callKnobs) []string {
	var names []string
	for _, rung := range relaxRungs {
		if !knobs.relaxed.has(rung.bit) {
			continue
		}
		if rung.took != nil {
			if took := c.widenedNames(model, knobs); len(took) > 0 {
				names = append(names, took...)
				continue
			}
		}
		names = append(names, rung.name)
	}
	return names
}
