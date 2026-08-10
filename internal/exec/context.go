package exec

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

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
	observationBudget    = 24 << 10
	maxObservationBudget = 256 << 10

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
	// window past the context it is supposed to fit inside.
	observationBytesPerToken = 4

	// observationFixedFloorTokens is the measured per-turn cost of everything
	// that is not observations — the standing contract, the tool schemas, the
	// brief. Measured at ~3,778 tokens across 332 leaf turns; rounded up,
	// because the window must not be sized from an optimistic floor.
	observationFixedFloorTokens = 4_000

	// observationCompletionReserve is what the turn itself needs room for:
	// reasoning tokens plus the reply. A window that fills the context to the
	// brim leaves the model no room to think, which is the same failure as
	// forgetting, arriving from the other side.
	observationCompletionReserve = 32_000
)

// observationWindow sizes the leaf's observation window from the one quantity
// that governs it: how much the model can hold in a single request.
//
// Half the usable context, and no more. The other half is not slack — it is the
// assistant messages, which are compressed state and are never faded, plus the
// user's brief, the upstream results, and everything the loop appends as it
// goes. A window sized at the whole remainder would be right on turn one and
// wrong by turn ten, and the failure would arrive as a provider rejection
// rather than as degradation.
//
// A known context always gets the formula's own answer, including when that
// answer is small. Only the unknown case takes a default, and only the default
// gets a floor under it — see observationBudget.
func observationWindow(contextTokens int) int {
	if contextTokens <= 0 {
		if defaultObservationBudget < observationBudget {
			return observationBudget
		}
		return defaultObservationBudget
	}
	usable := contextTokens/2 - observationFixedFloorTokens - observationCompletionReserve
	budget := usable * observationBytesPerToken
	if budget < minObservationBudget {
		return minObservationBudget
	}
	if budget > maxObservationBudget {
		return maxObservationBudget
	}
	return budget
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

// decayer shrinks old tool results in place, losslessly.
//
// The asymmetry is the whole idea. An assistant message is *compressed state*:
// the model already read the raw output and wrote down what mattered, so those
// messages are never touched. A tool result is *spent raw material* — by the
// time three more turns have happened, its value has usually already been
// extracted into the reasoning above it, and all it does is get re-billed on
// every remaining turn.
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
	// inflow is what this leaf actually adds per turn, which is what decides
	// how deep a firing pass has to reach. See decayHeadroomTurns.
	inflow inflowMeter
}

func newDecayer(labels map[string]string, spill spillFunc) *decayer {
	return &decayer{labels: labels, spill: spill,
		spilled: map[string]string{}, preserved: map[string]string{}}
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

// retire walks the transcript newest-first and stubs whatever raw tool output
// no longer fits, oldest results going first. Selection is unchanged from the
// trim-to-budget pass it replaces — keep while it fits, stub when it does not —
// only the level it fills to is lower.
//
// The most recent turn's results are the exception, and they are measured
// against the full budget rather than the low-water mark. Decay runs *before* a
// turn, so those bytes are the raw material the model has not read yet; the
// low-water mark governs how deep into history the batch reaches, never whether
// the current turn gets to see its own output. Everything from the last
// assistant message backwards is history and fades to the mark.
func (d *decayer) retire(messages []ai.Message, budget, lowWater int) int {
	spent, decayed := 0, 0
	// fresh is true while the walk is still inside the results of the newest
	// assistant turn; the transcript is append-only, so crossing an assistant
	// message is exactly the boundary between this turn and history.
	fresh := true
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "assistant" {
			fresh = false
			continue
		}
		if messages[index].Role != "tool" {
			continue
		}
		limit := lowWater
		if fresh {
			limit = budget
		}
		body := contentOf(messages[index])
		id := messages[index].ToolCallID
		// The transcript is append-only, so a message's index is a stable
		// identity even for the rare tool message without a call id.
		key := id
		if key == "" {
			key = fmt.Sprintf("#%d", index)
		}
		if _, done := d.spilled[key]; done {
			// Already a stub from an earlier turn; it stays exactly as it is.
			spent += len(body)
			continue
		}
		if spent+len(body) <= limit {
			spent += len(body)
			continue
		}
		label := labelFor(d.labels, id)
		stub := fmt.Sprintf("[%s — %d bytes, superseded]", label, len(body))
		if len(stub) >= len(body) {
			// Already smaller than the note describing it; leave it alone.
			// Checked before spilling so no file is written for a result that
			// is kept.
			spent += len(body)
			continue
		}
		// Preserve the bytes before shortening the message: decay must defer
		// detail, never destroy it.
		//
		// A result the pointer mechanism already preserved is not written
		// again, and its recorded path is used verbatim. That is not an
		// optimisation: pointers elsewhere in the transcript already name that
		// path and are never rewritten, so the stub replacing their target has
		// to agree with them, and a fresh write here could fail and leave the
		// target pathless with pointers still aimed at it.
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
		messages[index].Content = text(stub)
		decayed++
	}
	return decayed
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
			return pointer
		}
	}
	o.first[hash] = observationCopy{turn: turn, key: call.ID, label: callLabel(call)}
	return body
}

// pointerTo names where the earlier copy of some bytes can be read, or reports
// that no pointer may be emitted at it.
//
// Every pointer names the durable path, whether or not the earlier copy is
// still quoted in the transcript, and that redundancy is the point: the "above"
// half of the sentence is true when it is written and may stop being true when
// decay stubs the target, while the path half was made true before the pointer
// existed and stays true for the rest of the run.
func (o *observations) pointerTo(earlier observationCopy) (string, bool) {
	if earlier.path == "" {
		// Nothing durable behind those bytes, so nothing may point at them and
		// this copy is carried in full instead.
		return "", false
	}
	if _, stubbed := o.fade.spilled[earlier.key]; !stubbed {
		return fmt.Sprintf("[identical to the result of %s at turn %d — read it above, or from %s]",
			earlier.label, earlier.turn, earlier.path), true
	}
	return fmt.Sprintf("[identical to the result of %s at turn %d, whose bytes are in %s — read the part you need with sh]",
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
