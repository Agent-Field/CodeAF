package provider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/calllog"
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
	// body is the request as it was last encoded, kept ONLY when the bodies pin
	// is set. It is nil on every ordinary run, which is what keeps a person's
	// prompts out of a file they did not ask to have them in.
	body []byte
}

func newCallTrace() *callTrace { return &callTrace{} }

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
	// response is the assembled answer, on the row that has one.
	response        *ai.Response
	reasoningTokens int
	// learned is what THIS row's answer taught, which is not the same as what
	// the call has learned so far: a repaired 400 carries its own lesson.
	learned      []string
	responseBody []byte
	// phase is calllog.PhaseStart on the row written as a call goes out, and
	// empty on the row that ends it.
	phase string
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
	if calllog.Path() == "" {
		return
	}
	model := c.modelFor(facts.request)
	record := calllog.Record{
		Time:   logNow().Format("2006-01-02T15:04:05.000Z07:00"),
		Phase:  facts.phase,
		Tag:    callTag(facts.ctx),
		Node:   callNode(facts.ctx),
		Model:  model,
		Served: strings.TrimSpace(facts.served),
		Effort: c.recordedEffort(model, facts.knobs),
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
		Relaxed:        relaxNames(facts.knobs.relaxed),
		Status:         facts.status,
		Millis:         logNow().Sub(facts.began).Milliseconds(),
		Learned:        facts.learned,
		EmptyAtCeiling: c.emptyAtCeiling(facts.request, facts.response),
	}
	if facts.err != nil {
		record.Error = calllog.ClipError(facts.err.Error())
	}
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
	if facts.knobs.trace != nil {
		record.ID = facts.knobs.trace.attemptID
	}
	if facts.attempt == 0 && facts.knobs.trace != nil {
		// A row about the whole call says how many times it went out; a row
		// about one attempt already said which attempt it was.
		record.Attempt = facts.knobs.trace.attempts
	}
	if calllog.Bodies() {
		if facts.knobs.trace != nil {
			record.RequestBody = string(facts.knobs.trace.body)
		}
		record.ResponseBody = string(facts.responseBody)
	}
	calllog.Append(record)
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
		return fmt.Sprintf("%s %d tokens", effort, budget)
	}
	if effort == EffortNone {
		return ""
	}
	return string(effort)
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
func relaxNames(set relaxSet) []string {
	var names []string
	for _, rung := range relaxRungs {
		if set.has(rung.bit) {
			names = append(names, rung.name)
		}
	}
	return names
}
