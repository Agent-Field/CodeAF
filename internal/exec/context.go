package exec

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// How many bytes of raw tool output the transcript may carry before older
// results start fading. Everything past the window is still referenced, just
// not quoted.
const (
	// observationBudget is the minimum the unknown-context default may fall to,
	// and nothing else. It is deliberately not a floor under the computed
	// window: when a model's context really is small, the honest answer is a
	// small window, and clamping it upwards would size a prompt past what the
	// model accepts in order to look generous.
	observationBudget = 24 << 10

	// minObservationBudget is arithmetic protection rather than policy. Below
	// it the fixed floor and the completion reserve have already eaten the
	// model's whole context and this loop cannot run there at all; the window
	// stops at a value the decay pass can still reason about instead of going
	// to zero or negative.
	minObservationBudget = 4 << 10

	// defaultObservationBudget is the window a leaf carries when nothing can
	// say how much context its model actually has.
	//
	// It replaces a category error. The window used to be one sixth of the
	// leaf's *token budget* — a ceiling on cumulative spend across every turn
	// of the run, which has nothing to do with how much material fits in one
	// request. At the default budget that arithmetic produced a 25KB window in
	// front of a model holding hundreds of thousands of tokens, so a single
	// 26KB subject did not fit in the memory meant to hold it and the leaf
	// re-read what it had already been shown.
	//
	// The number is conservative on purpose, and that is the correction to the
	// obvious first instinct. Not knowing a model's context length is not
	// evidence that it is large: an offline catalog, an unrecognised slug and a
	// row cached before the field was kept all look identical here, and a
	// small-context model is exactly the kind that goes unrecognised. A
	// generous default would be a silent regression for it — prompts sized past
	// what it accepts, failing as provider rejections rather than as
	// degradation. So the unknown case buys back a little room over the 25KB the
	// spend ceiling used to give, and no more; the real win is claimed only when
	// the catalog actually answers, which is the ordinary case on every surface
	// that loads one.
	defaultObservationBudget = 32 << 10

	// observationBytesPerToken converts a context length into the byte budget
	// this package actually measures. Four is the conservative direction for
	// mixed prose and code: over-estimating bytes per token would size the
	// window past the context it is supposed to fit inside. It is the shared
	// estimator rather than a second opinion about the same question — when a
	// real tokenizer arrives it replaces ctxbudget's constant and this follows.
	observationBytesPerToken = ctxbudget.BytesPerToken

	// observationFixedFloorTokens is the measured per-turn cost of everything
	// that is not observations — the standing contract, the tool schemas, the
	// brief. Measured at ~3,778 tokens across 332 leaf turns; rounded up,
	// because the window must not be sized from an optimistic floor.
	observationFixedFloorTokens = 4_000
)

// observationWindow sizes the leaf's observation window from the one quantity
// that governs it: how much the model can hold in a single request.
//
// The share of the context it may take is no longer this file's opinion. It is
// the process-wide fill law — see ctxbudget, which owns the one statement of how
// full an agent's window may get before compaction fires, and the completion
// reserve every call keeps for its answer and its reasoning. What is left after
// the fixed floor and that reserve is what observations may carry. The rest of
// the window is not slack: it is the assistant messages, which are compressed
// state and fade last and only under pressure (see fold), plus the user's brief,
// the upstream results, and everything the loop appends as it goes.
//
// There is no longer a ceiling over the answer that is this file's own opinion,
// and the deleted one is worth naming because it was the only absolute byte
// number left in the sizing. It stood at 64KB and it was a cost decision wearing
// a memory decision's clothes: the window is re-sent every turn and billed
// against the leaf's spend ceiling, so a big window on a small grant was
// measured crossing the wrap-up threshold before the leaf had done any work.
// Capping the memory at a buried literal was the wrong half to fix.
//
// What replaced it is not that literal coming back. The pot the fill law is
// taken of is now min(window, working set) — see ctxbudget.WithinWorkingSet —
// and the difference between the two is the whole point: 64KB was a number this
// file believed about every model in existence, while the working set is a named
// setting with a stated default, an environment pin, a row in the sheet, and a
// reason measured on this harness. It is also the correction to a real, measured
// failure of the unbounded version: on a 1M-context model the fill law alone
// handed a leaf a 2.2MB observation window against 181KB of tool output across
// TWELVE nodes, so the decayer fired zero times, nothing ever left any
// transcript, and 90% of every input token billed was material the model had
// already been shown.
//
// A known context under the ceiling still gets the law's own answer, including
// when that answer is small. Only the unknown case takes a default, and only the
// default gets a floor under it — see observationBudget.
func observationWindow(contextTokens int) int {
	budget := ctxbudget.For(contextTokens).WithinWorkingSet().WithFloor(observationFixedFloorTokens)
	if !budget.Known() {
		if defaultObservationBudget < observationBudget {
			return observationBudget
		}
		return defaultObservationBudget
	}
	// Deliberately not BytesOr: an unaffordable window on a genuinely small
	// model must come back small, not fall through to the unknown-case default.
	// Sizing a prompt past what the model accepts in order to look generous is
	// a provider rejection rather than a shorter memory.
	if window := budget.Bytes(); window > minObservationBudget {
		return window
	}
	return minObservationBudget
}

// decayLowWaterPercent is where a firing decay pass stops before the loop has
// measured anything, as a percentage of the budget it fired at.
//
// Trimming to exactly the budget every turn looks frugal and is the most
// expensive thing the loop does. Every call rides a prefix cache: the provider
// bills the cached rate for the longest prefix that is byte-identical to the
// previous call and full price for everything after the first changed byte.
// Stubbing one old result rewrites a message near the front of the transcript,
// so the whole tail behind it — every assistant turn, every surviving result —
// is re-billed cold. In steady state the leaf adds N bytes of output per turn
// and must retire N bytes, so a trim-to-budget pass fires on *every* turn and
// the transcript is never cached past the fade line.
//
// Hysteresis converts that into one rewrite per K turns. On crossing the budget
// the pass retires in a single batch down to a low-water mark, leaving headroom
// behind it; the next several turns fit inside that headroom and touch nothing,
// so their prefixes are byte-identical and hit warm. One batch of stubs every K
// turns costs one invalidation where per-turn trimming costs K.
//
// The price is window size: on average the transcript carries less raw output
// than a trim-to-budget pass would leave. Nothing is lost either way — every
// stub still points at the spill file holding its bytes — so the whole question
// is what K comes out at, and that is where a fixed fraction went wrong. K is
// headroom divided by inflow, and the fraction only ever set the numerator. At
// the 25KB window this constant was tuned against, a quarter was 6.25KB of
// headroom against a measured 3.7KB of observation bytes per turn: K was 1.7,
// the batch bought less than two quiet turns, and hysteresis was paying its
// price in window size while delivering almost none of its benefit.
//
// So the mark is computed from the denominator instead, and this constant is
// only what stands in before the loop has enough turns to have measured it.
const decayLowWaterPercent = 75

// decayHeadroomTurns is the K the mark is solved for: how many turns of
// measured inflow one firing pass must buy before the next one. Seven sits in
// the middle of the six-to-eight range where the invalidation is amortized well
// enough that the remaining cost is noise, and going higher only trades window
// for a saving that is no longer there.
const decayHeadroomTurns = 7

// minInflowSamples is how many turns must have been measured before their mean
// is trusted over the fixed fraction. Three is enough to tell a leaf reading
// whole files from one running short checks, and few enough that the measured
// mark governs almost the entire run.
const minInflowSamples = 3

// decayHeadroomFloorPercent and decayHeadroomCeilingPercent bound the computed
// mark, and both bounds are about measurement rather than policy.
//
// A leaf whose turns each add more than the whole window would solve for a
// negative mark and retire everything, turning one batch into a full reset; the
// floor stops the pass giving up more than half the window in a single fire,
// which is the deepest the old fixed-fraction reasoning ever contemplated. At
// the other end a leaf whose first turns happen to be near-silent would solve
// for a mark so close to the budget that a single ordinary result crosses it
// again immediately — the every-turn rewrite the hysteresis exists to prevent,
// arriving through the measurement instead of through the arithmetic.
const (
	decayHeadroomFloorPercent   = 50
	decayHeadroomCeilingPercent = 90
)

// inflowMeter is the running mean of how many bytes of raw tool output one turn
// adds to the transcript. The loop already sees every result on its way into
// the messages, so this is a counter rather than a measurement pass.
type inflowMeter struct {
	turns int
	bytes int
}

func (m *inflowMeter) observe(bytes int) {
	if bytes < 0 {
		return
	}
	m.turns++
	m.bytes += bytes
}

// mean reports the measured per-turn inflow, and whether there is enough of it
// to act on.
func (m *inflowMeter) mean() (int, bool) {
	if m.turns < minInflowSamples {
		return 0, false
	}
	return m.bytes / m.turns, true
}

// spillFunc preserves a decaying observation's full body in the workspace and
// returns the workspace-relative path it went to. ok=false means the bytes
// could not be written and the caller must not claim a path.
type spillFunc func(toolCallID, body string) (path string, ok bool)

// foldKeepAssistantTurns is how many of the newest assistant messages the fold
// pass may never touch, and it is the invariant the original "never touched"
// rule was really protecting.
//
// The rule was written as "assistant messages are compressed state", and that
// reasoning is sound about the RECENT ones and only about those. The model's
// last few turns are its live working memory — the plan it is executing, the
// conclusion it just reached, the draft that becomes the deliverable when the
// loop ends — and folding any of them would be deleting the thing the next turn
// is written against. Twenty turns back, the same message is a paragraph about a
// file that has since been rewritten, re-billed on every remaining turn: it is
// spent, exactly as a tool result is spent, and the measured evidence is that
// assistant output was 45% of context growth while the decayer could not reach
// a byte of it.
//
// Three keeps the current thought and the two it was built on. It is deliberately
// smaller than the eight-to-sixteen turns a well-sized leaf runs, because a fold
// that kept most of the run would reclaim nothing, and deliberately more than
// one, because a model that has just been told its own last message is a stub
// has been made to distrust its own memory.
//
// The deliverable is covered by this same keep and needs no separate rule: the
// leaf's contract is that the final message IS the deliverable (see
// systemPrompt), the fold runs at the top of a turn before that message exists,
// and lastAssistantText — what land() hands over when a leaf is stopped early —
// reads the newest assistant text there is, which is never foldable.
const foldKeepAssistantTurns = 3

// decayer shrinks old tool results in place, losslessly, and — only when that
// is not enough — folds aged assistant text the same way.
//
// The asymmetry is the whole idea, and it is an ORDER rather than an exemption.
// A tool result is *spent raw material*: by the time three more turns have
// happened its value has usually already been extracted into the reasoning above
// it, and all it does is get re-billed on every remaining turn, so it retires
// first and it retires deep. An assistant message is *compressed state* — the
// model already read the raw output and wrote down what mattered — so it is
// worth many times its bytes and it is touched last, never while stubbing tool
// output would do, and never within foldKeepAssistantTurns of the live edge.
//
// What is not true, and was assumed for a long time, is that compressed state is
// worth its bytes forever. See fold.
//
// Newest results are kept in full until the budget runs out, then everything
// older collapses to a single line naming what it was and where its bytes
// went: before a result is stubbed, its full body is written to the
// workspace's observation directory, so the stub is a pointer rather than a
// tombstone — the agent can re-read the file with sh if it turns out to
// matter. Budgeting by bytes rather than by count means a turn full of small
// results all survive, while one huge one retires early — which is the right
// trade, because the huge one is what costs.
//
// Fading happens in batches, not continuously. See decayLowWaterPercent: a pass
// that fires clears down to the low-water mark so that most turns can leave the
// transcript untouched, because an untouched transcript is a cached one.
//
// A tool message is only ever shortened, never removed. Its ToolCallID pairs
// with the assistant turn that requested it, and an unpaired tool_call is a
// hard provider error rather than a degraded prompt.
type decayer struct {
	labels map[string]string // tool_call_id → what the call was
	spill  spillFunc         // writes a body to the workspace; nil when there is no filesystem
	// spilled remembers every tool_call_id already stubbed, and the path its
	// bytes went to ("" when no file could be written). It is both the
	// write-once guard and the already-stubbed detector: decay runs over the
	// same transcript every turn, and without it the same result would be
	// re-spilled and re-stubbed each time.
	spilled map[string]string
	// preserved is the set of results whose bytes are already on disk before
	// any decay pass has looked at them, and the path each one went to. Only
	// the pointer mechanism puts anything here — see observations.preserve —
	// and it is deliberately not the same map as spilled, which doubles as the
	// already-stubbed detector: a result recorded there would never be stubbed
	// at all.
	//
	// It carries an invariant the pointer mechanism depends on absolutely. A
	// pointer written into the transcript is never revisited, so its target
	// must still be readable at the end of the run as well as at the moment it
	// was written. Preserving first and stubbing later is what makes that true
	// regardless of what happens on the way: the retire pass below reads a
	// preserved path instead of writing a fresh spill, so a disk that has
	// filled up or a directory that has gone away between the two can no longer
	// turn a live target into a pathless tombstone with pointers aimed at it.
	preserved map[string]string
	// consumedFrom is how far into the transcript this leaf has demonstrably
	// acted. Everything below the mark was already in front of the model at the
	// moment it last changed the workspace, so its value has been extracted;
	// everything at or above it is material the model has read and not yet used.
	// An index is identity enough because the transcript is append-only — the
	// same property the "#index" fallback key below already relies on.
	consumedFrom int
	// consumedKeys is the same fact for the results an index cannot speak for:
	// a result whose bytes were quoted again later, where the second copy is a
	// pointer naming a durable path that outlives the stub. See observations.
	consumedKeys map[string]bool
	// inflow is what this leaf actually adds per turn, which is what decides
	// how deep a firing pass has to reach. See decayHeadroomTurns.
	inflow inflowMeter
}

func newDecayer(labels map[string]string, spill spillFunc) *decayer {
	return &decayer{labels: labels, spill: spill, spilled: map[string]string{},
		preserved: map[string]string{}, consumedKeys: map[string]bool{}}
}

// actedPast records that everything the transcript held below index was in
// front of the model when it last changed the workspace. The loop calls it with
// the artifact-count signal the no-progress guard already reads, which is the
// only mutation detector in the leaf and stays the only one.
func (d *decayer) actedPast(index int) {
	if index > d.consumedFrom {
		d.consumedFrom = index
	}
}

// used records that one result's bytes have been asked for a second time and
// answered from somewhere durable, which spends the copy in the transcript.
func (d *decayer) used(key string) { d.consumedKeys[key] = true }

// retired reports whether a result has already been shortened to a stub — so
// its bytes are no longer quoted where the model can read them — and the
// address they went to, which is "" when no file could be written.
func (d *decayer) retired(key string) (string, bool) {
	path, stubbed := d.spilled[key]
	return path, stubbed
}

// inherit hands a durable address that already exists to a second key holding
// exactly those bytes, so the retire pass reuses the file rather than writing a
// second copy of material that is already on disk.
func (d *decayer) inherit(key, path string) {
	if key != "" && path != "" {
		d.preserved[key] = path
	}
}

// preserve writes a result's bytes to the workspace ahead of any decay, and
// reports the path they can be read back from. It is idempotent by key: the
// same result is written once however many times it repeats.
//
// ok=false means these bytes cannot be given a durable address, and the caller
// must then treat them as unpointable rather than claiming a path that does not
// exist.
func (d *decayer) preserve(key, body string) (string, bool) {
	if path := d.preserved[key]; path != "" {
		return path, true
	}
	// Decay may already have written exactly these bytes under exactly this
	// key, in which case the address exists and writing it again would only
	// cost a syscall to produce the same file.
	if path := d.spilled[key]; path != "" {
		d.preserved[key] = path
		return path, true
	}
	if d.spill == nil {
		return "", false
	}
	path, ok := d.spill(key, body)
	if !ok {
		// Not recorded as a failure: a full disk now may not be a full disk in
		// three turns, and the only cost of asking again is one write attempt
		// on a body that has already repeated once.
		return "", false
	}
	d.preserved[key] = path
	return path, true
}

// observe records one turn's worth of new raw tool output. The loop calls it
// once per turn, with the bytes it is about to append.
func (d *decayer) observe(bytes int) { d.inflow.observe(bytes) }

// decay stubs raw tool output once the live window outgrows the budget, and
// does nothing at all until then. It returns how many results were newly
// stubbed.
//
// The check is the cheap half and the point of the whole design: most turns
// answer "still under budget" by summing lengths, mutate nothing, and leave the
// prompt prefix byte-identical to the previous turn. When the window does cross
// the line, one pass retires in bulk to the low-water mark rather than shaving
// off exactly the overflow, buying several quiet turns before the next rewrite.
func (d *decayer) decay(messages []ai.Message, budget int) int {
	if liveObservationBytes(messages) <= budget {
		return 0
	}
	return d.retire(messages, budget, d.lowWater(budget))
}

// lowWater is where this leaf's firing pass stops: far enough below the budget
// that decayHeadroomTurns of its own measured inflow fit in the gap.
//
// Solving for the gap rather than declaring it is the whole of the fix. The
// same fraction is a batch that buys eight quiet turns for a leaf running short
// checks and a batch that buys one for a leaf reading files, and the leaf is the
// only thing that knows which it is.
func (d *decayer) lowWater(budget int) int {
	inflow, measured := d.inflow.mean()
	if !measured || inflow <= 0 {
		return decayLowWater(budget)
	}
	mark := budget - decayHeadroomTurns*inflow
	if floor := budget * decayHeadroomFloorPercent / 100; mark < floor {
		mark = floor
	}
	if ceiling := budget * decayHeadroomCeilingPercent / 100; mark > ceiling {
		mark = ceiling
	}
	return mark
}

// decayLowWater is the mark before anything has been measured, and the shape
// every mark must keep: at or below the budget, so the post-decay invariant the
// loop depends on — raw live bytes never exceed the budget once decay has run —
// holds unchanged.
func decayLowWater(budget int) int {
	mark := budget * decayLowWaterPercent / 100
	if mark < 0 {
		mark = 0
	}
	return mark
}

// liveObservationBytes is the raw cost of the transcript's tool output as it
// currently stands: already-stubbed results count as their stub, because that
// is what the provider is actually billed for.
func liveObservationBytes(messages []ai.Message) int {
	total := 0
	for _, message := range messages {
		if message.Role != "tool" {
			continue
		}
		for _, part := range message.Content {
			total += len(part.Text)
		}
	}
	return total
}

// retireOrder is the order retire considers results for KEEPING in, and it is
// the law written down as three passes over one transcript.
type retireOrder int

const (
	// retireFresh is the newest assistant turn's own results: raw material the
	// model has not read yet, measured against the full budget.
	retireFresh retireOrder = iota
	// retireNeeded is history the leaf has not been shown to have acted on.
	retireNeeded
	// retireSpent is history whose value has already been extracted.
	retireSpent
)

// retire stubs whatever raw tool output no longer fits, and chooses what goes
// by how long ago each result was last needed.
//
// THE LAW: AN OBSERVATION IS RETIRED IN ORDER OF HOW LONG AGO IT WAS LAST
// NEEDED, NOT HOW LONG AGO IT ARRIVED. A leaf that must read more material than
// the window holds before it can act on any of it was losing the earliest reads
// first — the ones it had not used yet — and spent the rest of its budget
// getting the same material back. Age is the wrong question: what a result has
// cost is the same whenever it arrived, and what it is still worth is whether
// the model has acted past it.
//
// So the walk runs three times, newest-first each time, filling the same
// accumulator: the live turn's own results, then history nothing has acted
// past, then history that has been acted past. Whatever the mark leaves no room
// for is stubbed, which means spent results go first and the oldest of them
// goes first among those, and unconsumed material is reached for only when
// retiring every spent result did not bring the window under the mark. The
// window is a hard bound on request size, so "never" is not available; "last"
// is. Selection inside a pass is unchanged — keep while it fits, stub when it
// does not.
//
// The most recent turn's results are measured against the full budget rather
// than the low-water mark. Decay runs *before* a turn, so those bytes are the
// raw material the model has not read yet; the low-water mark governs how deep
// into history the batch reaches, never whether the current turn gets to see
// its own output. Everything from the last assistant message backwards is
// history and fades to the mark.
func (d *decayer) retire(messages []ai.Message, budget, lowWater int) int {
	spent, decayed := 0, 0
	// The newest assistant message is exactly the boundary between this turn
	// and history, because the transcript is append-only.
	live := -1
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "assistant" {
			live = index
			break
		}
	}
	for _, pass := range [...]retireOrder{retireFresh, retireNeeded, retireSpent} {
		for index := len(messages) - 1; index >= 0; index-- {
			if messages[index].Role != "tool" {
				continue
			}
			key := observationKey(messages[index], index)
			if d.orderOf(key, index, live) != pass {
				continue
			}
			limit := lowWater
			if pass == retireFresh {
				limit = budget
			}
			body := contentOf(messages[index])
			// Three reasons to leave a result exactly as it is, and each costs
			// what it costs: it is already a stub from an earlier turn, there
			// is still room for it, or it is shorter than the note that would
			// replace it.
			if _, done := d.retired(key); done || spent+len(body) <= limit ||
				!d.supersede(&messages[index], key, body) {
				spent += len(body)
				continue
			}
			decayed++
		}
	}
	return decayed
}

// orderOf places one result in the retirement order. live is the index of the
// newest assistant message, so anything after it belongs to the turn about to
// be read.
func (d *decayer) orderOf(key string, index, live int) retireOrder {
	switch {
	case index > live:
		return retireFresh
	case index < d.consumedFrom || d.consumedKeys[key]:
		return retireSpent
	default:
		return retireNeeded
	}
}

// observationKey is how a result is named for the life of the leaf. The
// transcript is append-only, so a message's index is a stable identity even for
// the rare tool message without a call id.
func observationKey(message ai.Message, index int) string {
	if message.ToolCallID != "" {
		return message.ToolCallID
	}
	return fmt.Sprintf("#%d", index)
}

// supersede shortens one result in place to a line naming what it was and where
// its bytes went, and reports whether it did. It answers false for a result
// already smaller than that note — checked before anything is written, so no
// file is produced for a result that is kept.
//
// The bytes are preserved before the message is shortened: decay must defer
// detail, never destroy it. A result the pointer mechanism already preserved is
// not written again, and its recorded path is used verbatim. That is not an
// optimisation: pointers elsewhere in the transcript already name that path and
// are never rewritten, so the stub replacing their target has to agree with
// them, and a fresh write here could fail and leave the target pathless with
// pointers still aimed at it.
func (d *decayer) supersede(message *ai.Message, key, body string) bool {
	label := labelFor(d.labels, message.ToolCallID)
	stub := fmt.Sprintf("[%s — %d bytes, superseded]", label, len(body))
	if len(stub) >= len(body) {
		return false
	}
	d.spilled[key] = ""
	path, kept := d.preserved[key]
	if !kept && d.spill != nil {
		path, kept = d.spill(key, body)
	}
	if kept {
		withPath := fmt.Sprintf("[%s — %d bytes, spilled to %s]", label, len(body), path)
		if len(withPath) < len(body) {
			stub = withPath
			d.spilled[key] = path
		}
	}
	message.Content = text(stub)
	return true
}

// liveTranscriptBytes is what the whole re-sent body of the transcript costs as
// it currently stands — spent raw material and compressed state together, each
// counted as what the provider is actually billed for.
//
// It is the quantity the working set is a statement about. liveObservationBytes
// answers a narrower question (how much raw output is quoted) and is what the
// tool-decay pass fires on; this one is what decides whether that pass was
// enough.
func liveTranscriptBytes(messages []ai.Message) int {
	total := 0
	for _, message := range messages {
		switch message.Role {
		case "tool", "assistant":
			for _, part := range message.Content {
				total += len(part.Text)
			}
		}
	}
	return total
}

// fold retires aged assistant text, and only once retiring tool output has
// failed to bring the transcript back inside the working set.
//
// The order is the policy. decay runs first and reaches as deep as it likes into
// spent raw material; fold is asked afterwards, sees what that left, and does
// nothing at all unless the whole live body — results and reasoning together —
// is still over. So on the ordinary leaf, whose bulk is tool output, this pass
// measures and mutates nothing, and the "assistant messages are never touched"
// behaviour of every run before this one is exactly what still happens. It fires
// on the leaf whose own prose became the context: measured, assistant output was
// 45% of context growth, and no governor in the harness could see a byte of it.
//
// When it does fire it folds every foldable message in one batch rather than
// shaving off the overflow, for the reason decayLowWaterPercent spells out at
// length: a rewrite anywhere in the transcript re-bills the entire tail cold, so
// the cheap shape is one invalidation followed by many quiet turns, and the
// expensive shape is a small correct rewrite every turn. Everything newer than
// foldKeepAssistantTurns is untouchable, which is what stops the batch reaching
// the model's live working memory or the deliverable.
//
// After that first batch the keep window slides forward one message per turn, so
// a transcript that stays over budget folds one newly-aged turn per turn. That
// is a rewrite per turn and it is affordable where the decay pass's would not
// be, because of WHERE it lands: the fold point advances with the tail, so what
// is re-billed cold behind it is the last few assistant turns and their results
// rather than the whole history a head-of-transcript rewrite invalidates.
//
// Nothing is lost. The full text goes to the same workspace spill the decay pass
// writes to, under the same write-once bookkeeping, and the stub left behind
// names that path — so a model that finds it needs its own earlier reasoning can
// read it back with sh, exactly as it can for a superseded result. A message
// whose bytes cannot be given a durable address is left alone rather than
// summarised away: a fold that cannot point is a deletion.
//
// Tool-call pairing is preserved absolutely. Only the text content of a message
// is replaced; its ToolCalls travel untouched, because an assistant tool_call
// with no answering tool message — or a tool message answering nothing — is a
// hard provider rejection rather than a degraded prompt.
func (d *decayer) fold(messages []ai.Message, budget int) int {
	if liveTranscriptBytes(messages) <= budget {
		return 0
	}
	// Which assistant message this is, counting from the start, so a stub can
	// say which turn's reasoning it stands for. The transcript is append-only,
	// so the ordinal is stable for the life of the leaf.
	ordinal := make([]int, len(messages))
	turns := 0
	for index := range messages {
		if messages[index].Role == "assistant" {
			turns++
			ordinal[index] = turns
		}
	}

	folded, fresh := 0, 0
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role != "assistant" {
			continue
		}
		if fresh < foldKeepAssistantTurns {
			fresh++
			continue
		}
		body := contentOf(messages[index])
		if body == "" {
			continue
		}
		key := fmt.Sprintf("assistant#%d", index)
		if _, done := d.spilled[key]; done {
			// Already a stub from an earlier pass; it stays exactly as it is.
			continue
		}
		// The length test comes before the write, so no file is produced for a
		// message that is going to be kept — the same discipline retire applies
		// to itself. The path only lengthens the note, so a body that cannot
		// beat the pathless form can never beat the real one either.
		if len(foldStub(ordinal[index], len(body), "")) >= len(body) {
			continue
		}
		path, kept := d.preserved[key]
		if !kept && d.spill != nil {
			path, kept = d.spill(key, body)
		}
		if !kept {
			// No durable address, so no pointer may be made: the bytes are
			// carried again, which is the cheap failure. See observations.
			continue
		}
		stub := foldStub(ordinal[index], len(body), path)
		if len(stub) >= len(body) {
			continue
		}
		d.spilled[key] = path
		messages[index].Content = text(stub)
		folded++
	}
	return folded
}

// foldStub is what a folded turn leaves in the transcript. It is written in the
// model's own first person because that is whose message it replaces, and it
// names the file rather than merely admitting something was there — a fold that
// does not say where is a deletion with a receipt.
func foldStub(turn, bytes int, path string) string {
	if path == "" {
		return fmt.Sprintf("[my turn %d — %d bytes of my own earlier reasoning, folded]", turn, bytes)
	}
	return fmt.Sprintf("[my turn %d — %d bytes of my own earlier reasoning, folded; the full text is in %s — read it with sh if it matters]",
		turn, bytes, path)
}

func labelFor(labels map[string]string, id string) string {
	if label, ok := labels[id]; ok && label != "" {
		return label
	}
	return "earlier tool result"
}

func contentOf(message ai.Message) string {
	total := 0
	for _, part := range message.Content {
		total += len(part.Text)
	}
	if total == 0 {
		return ""
	}
	body := make([]byte, 0, total)
	for _, part := range message.Content {
		body = append(body, part.Text...)
	}
	return string(body)
}

// observations content-addresses everything the transcript is asked to carry,
// so the same bytes are paid for once.
//
// It replaces a memo keyed on the *call*, which was the wrong key twice over.
// The memo answered a repeated call from the previous result without running
// it, which is wrong the moment anything has changed underneath — so it had to
// be emptied whenever a turn ran sh, write or edit, and sh is also the read
// tool and appears in 87% of turns. Measured over 332 leaf turns it caught none
// of the 27 duplicate fetches that actually happened, while 12.5% of every
// observation byte the loop paid for was material it had already been shown.
//
// Keying on the bytes instead inverts both halves. The call still runs, so
// nothing is ever answered from a stale copy — the result is fetched fresh and
// only then compared — and mutation needs no invalidation rule at all, because
// bytes identical to bytes already in the transcript are identical whatever
// happened in between. What is saved is not the call, which was never the
// expensive part; it is the second and third copy of the same material riding
// every remaining turn of the leaf.
//
// A pointer is only ever emitted at something the model can still reach, and
// reachability is established before the pointer exists rather than checked as
// it is written.
//
// The difference matters because a pointer, once in the transcript, is never
// revisited — rewriting a message is exactly the cost this mechanism exists to
// avoid. Checking at write time is therefore a check about the present tense
// used to make a promise about the future: a target that is live when the
// pointer is written can decay three turns later, and if the spill that decay
// attempts happens to fail, the target becomes a pathless tombstone with
// pointers already aimed at it and no way left to reach the bytes.
//
// So the first time a body repeats, the canonical copy is written to the
// workspace before any pointer at it is emitted, and the decay pass reuses that
// exact path when it later stubs the message. If that write cannot be made, no
// pointer is emitted at all and the bytes are carried again — the saving is
// forfeited, which is the cheap failure. The expensive one is a model sent to
// read something that is not there.
//
// The cost is one file per body that actually repeats, written with the content
// already in hand, and never a file for a body that only ever appears once.
type observations struct {
	fade *decayer
	// first maps the hash of a body to the earliest copy of it still in play.
	// It is never overwritten by a pointer, so a third copy of the same bytes
	// points back at the original rather than at a pointer to it.
	first map[string]observationCopy
}

// observationCopy is where one body's earliest copy lives: which turn produced
// it, what the call was, the decayer's key for the message holding it — which
// is what makes the spill state readable from here — and the durable path its
// bytes were written to before anything was allowed to point at them.
type observationCopy struct {
	turn  int
	key   string
	label string
	path  string
}

func newObservations(fade *decayer) *observations {
	return &observations{fade: fade, first: map[string]observationCopy{}}
}

// admit returns the text the transcript should carry for one finished call:
// the body itself the first time these bytes appear, a pointer to the earlier
// copy every time after.
func (o *observations) admit(turn int, call ai.ToolCall, result Result) string {
	body := result.Content
	if result.IsError {
		body = "ERROR: " + body
	}
	// Three kinds of result are never content-addressed. An error is short and
	// its whole value is being read where it happened. A result carrying a
	// background-job report describes state that was true when it was written
	// and is not true now, so two identical reports are a coincidence rather
	// than the same fact. And a result with multimodal follow-up content is not
	// its text — the image is what the model looks at, and a pointer would hand
	// it a sentence instead.
	if result.IsError || result.reportedJobs || len(result.Followup) > 0 || call.ID == "" {
		return body
	}
	sum := sha256.Sum256([]byte(body))
	hash := hex.EncodeToString(sum[:])
	if earlier, repeated := o.first[hash]; repeated {
		// A RE-READ OF RETIRED BYTES IS ANSWERED WITH THE BYTES. Once decay has
		// stubbed the earlier copy, a pointer at it would answer the model's
		// re-read with a description of the material it just asked for again,
		// and the only way left to it is reading the spill file in slices —
		// each slice new bytes, each pushing the window over once more. So the
		// body is carried whole and THIS copy becomes the canonical one, taking
		// over the address the stub already names: nothing is written twice,
		// and later duplicates point at a copy that is quoted.
		if path, stubbed := o.fade.retired(earlier.key); stubbed {
			o.fade.inherit(call.ID, path)
			o.first[hash] = observationCopy{turn: turn, key: call.ID, label: callLabel(call), path: path}
			return body
		}
		// The bytes in hand are the canonical copy's bytes — that is what the
		// hash match means — so the durable copy can be written from here, now,
		// without going back to whatever produced it.
		if earlier.path == "" {
			if path, ok := o.fade.preserve(earlier.key, body); ok {
				earlier.path = path
				o.first[hash] = earlier
			}
		}
		// A pointer that is not shorter than what it replaces has saved
		// nothing and cost the model a hop; the same discipline the decay
		// stub applies to itself.
		if pointer, reachable := o.pointerTo(earlier); reachable && len(pointer) < len(body) {
			// The earlier copy has now been read twice and is answerable from a
			// durable address either way, so it is spent: the decay pass may
			// retire it ahead of material the model has only read once. See
			// decayer.retire.
			o.fade.used(earlier.key)
			return pointer
		}
	}
	o.first[hash] = observationCopy{turn: turn, key: call.ID, label: callLabel(call)}
	return body
}

// pointerTo names where the earlier copy of some bytes can be read, or reports
// that no pointer may be emitted at it.
//
// Its target is always a copy still quoted in the transcript — admit carries
// the bytes whole rather than pointing at a stub — so the sentence can say
// "above" and mean it. It names the durable path as well, and that redundancy
// is the point: the "above" half is true when it is written and may stop being
// true when decay stubs the target three turns later, while the path half was
// made true before the pointer existed and stays true for the rest of the run.
func (o *observations) pointerTo(earlier observationCopy) (string, bool) {
	if earlier.path == "" {
		// Nothing durable behind those bytes, so nothing may point at them and
		// this copy is carried in full instead.
		return "", false
	}
	return fmt.Sprintf("[identical to the result of %s at turn %d — read it above, or from %s]",
		earlier.label, earlier.turn, earlier.path), true
}

// callLabel is what a decayed result is remembered as. It names the tool and
// enough of the arguments to recognise, so the model can tell "I already
// searched for X" from "I already read file Y" without the bytes.
func callLabel(call ai.ToolCall) string {
	arguments := call.Function.Arguments
	const maxLabelArgs = 60
	if len(arguments) > maxLabelArgs {
		arguments = arguments[:maxLabelArgs] + "…"
	}
	return call.Function.Name + " " + arguments
}
