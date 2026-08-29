// Package shaped is the one place the harness asks a model for an answer of a
// stated shape and reads what comes back.
//
// ── WHY IT IS ONE PLACE ──
//
// Before it existed, every pass that wanted JSON out of a model built its own
// version of the same three-step sequence: send with some ceiling, decode, and
// decide what an unusable reply means. The planner had one, the delivery gate
// had another, the intent compiler had a third, and two of them scanned for
// braces by hand. They disagreed about everything that matters. The s4 sweep
// (bench/deepswe/AUTOPSY.md) cost two of five runs to that disagreement, on one
// model, in one afternoon:
//
//   - the planner's fan-out was cut at the completion ceiling, the object was
//     incomplete, and the run exited 1 with zero nodes and a third of a cent
//     spent — nothing attempted, on a task that had scored 17/20 the sweep
//     before;
//   - the delivery gate's verdict came back unreadable, which its own caller
//     treated as an abstention, so the work was "delivered as done, unjudged"
//     and the run exited 0.
//
// Neither is a failure of a model. Both are failures of a boundary that had no
// owner: a ceiling nobody derived from the ask, a truncation nobody continued,
// and a non-answer that meant something different at each door it arrived at.
//
// ── THE THREE THINGS THIS OWNS ──
//
// SIZING. The ceiling on a structured reply is derived from the ask — how many
// objects it asks for and how much of the material it must echo back — and then
// raised by what this model has actually been seen to need on this lane. See
// ceiling.go; the derivation is documented in PERF.md.
//
// REPAIR. A reply cut off mid-object is CONTINUED rather than re-bought: the
// expensive half of it is already in hand, and re-asking spends the same tokens
// to hit the same wall, which is precisely what the planner did before it gave
// up. A reply that never began an object is a different failure — prose, or a
// whole ceiling spent thinking — and there is nothing there to continue, so that
// one is asked again, once, with the format contract and its own offending words
// quoted back at it.
//
// THE VERDICT ON FAILURE. When the repairs are spent, this returns a TYPED
// fault. A caller may not read it as "the model declined to answer" or as
// "nothing to judge": the answer was not delivered, which is a fact about the
// run and has to reach the exit code. FAILSAFE.md's floor — a fail-safe cannot
// deliver nothing as done — is enforced by that type existing and by callers
// switching on it.
//
// What is NOT here: whether an answer that parsed is RIGHT. Only the call site
// can know that, and it reports it as it always did (provider.Report).
package shaped

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Completer is the half of a model client this package uses. It is stated
// structurally rather than imported so that the planner's own Completer, the
// pooled client the gate holds, and a test's stub all satisfy it without any of
// them learning about this package.
type Completer interface {
	CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error)
}

// Ask is one request for a shaped answer.
//
// Everything on it except the messages is here because the seam needs it to
// size or to repair the call — there is no field a caller sets that only its own
// pass reads.
type Ask struct {
	// Lane names the pass making the ask, in the words a person would use for
	// it: "plan", "gate", "compile". It is the memo's key beside the model and
	// the subject of the line the stream prints, so it is a name and never a
	// class identifier.
	Lane string

	// Messages are the request as the caller assembled it. The seam appends to
	// a copy when it repairs and never rewrites what is here.
	Messages []ai.Message

	// Schema is the shape being asked for. It is used for two things: sent on
	// the wire when Routed, and read by the ceiling derivation, which counts
	// what one answer of this shape has to hold.
	Schema json.RawMessage

	// Routed says whether this client can carry a structured-output request. A
	// build with no router has no second rung to unlock and sends none — the
	// tolerance in provider.DecodeJSONObject is what stands in for it.
	Routed bool

	// Answers is how many objects of the schema's item shape the ask expects
	// back — the fan-out's permitted width, a panel's seat count. Zero and one
	// both mean "one object", which is every other call in the system.
	//
	// IT IS THE ASK'S OWN NUMBER AND NEVER A GUESS. Where a prompt tells the
	// model how many parts it may return, that same figure is what is passed
	// here, so the sentence the model reads and the room it is given can never
	// drift apart.
	Answers int

	// Echo is the material this answer has to carry back verbatim, when there is
	// any. The intent compiler's brief restates the user's instruction twice —
	// once inside the goal and once under "Verbatim request:" — so its reply
	// cannot be smaller than the ask however small the schema is. A pass whose
	// answer quotes nothing leaves this empty.
	Echo string
}

// ErrUnreadable is what a caller gets when the model would not answer in the
// shape that was asked for, after this seam has done everything it can.
//
// IT IS A FAULT AND NEVER AN ABSTENTION. The distinction is the whole reason the
// type exists: the delivery gate used to read an unreadable verdict as "no
// opinion" and ship the work as done, which is the exact failure FAILSAFE.md's
// floor forbids. A caller that catches this must either retry the work or end
// the run short of whole; it may not proceed as though nothing had happened.
var ErrUnreadable = errors.New("the model did not answer in the shape this asked for")

// Unreadable reports whether an error is that fault, wrapping included.
func Unreadable(err error) bool { return errors.Is(err, ErrUnreadable) }

// Answer sends one ask, repairs what can be repaired, and decodes the result
// into the caller's destination.
//
// The response it returns is the LAST one on the wire, so a caller adding up
// usage adds up the repair as well as the original — a repaired call costs what
// it costs, and a bill that hid the repair would make this seam look free. When
// an answer was continued, the returned response carries the joined text, so a
// caller that re-reads the text sees the whole answer rather than the fragment.
//
// It reports the two verdicts it can determine by itself — a transport failure
// and a format failure — and leaves the semantic one to the caller, which is the
// contract plan.structured established and this inherits unchanged.
func Answer(ctx context.Context, client Completer, ask Ask, into any) (*ai.Response, error) {
	if client == nil {
		return nil, fmt.Errorf("%s: no client", ask.laneWords())
	}
	model := provider.CallFrom(ctx).Model()
	ceiling := Room(ask, model)

	response, err := client.CompleteWithMessages(ctx, ask.Messages, ask.request(ceiling)...)
	if err != nil {
		provider.Report(ctx, provider.VerdictProviderFailure)
		return nil, err
	}
	// A MODEL THAT NAMED ITSELF IS BETTER EVIDENCE THAN A ROUTER'S PIN. The pin
	// is empty on every path without a router, and the memo below is worth
	// exactly as much as its key is accurate.
	if model == "" && response != nil {
		model = response.Model
	}

	answered := text(response)
	// THE VERDICT ON A CUT REPLY IS WHETHER IT DECODES, NEVER THE FINISH REASON
	// ALONE (44e6a4f4). A model that reached the ceiling on tokens which never
	// became text has still answered, and a repair taken on the finish reason
	// spent forty-five seconds fetching a second copy of an answer already in
	// hand.
	if provider.DecodeJSONObject(answered, into) == nil {
		return response, nil
	}

	repaired, err := repair(ctx, client, ask, model, ceiling, response, answered, into)
	if err != nil {
		// A model that would not answer in shape is a format failure and moves a
		// rating. A provider that could not be reached partway through a repair
		// is the weather and moves nothing — rating a model down because its
		// endpoint was busy would make the most popular model look weakest.
		if Unreadable(err) {
			provider.Report(ctx, provider.VerdictFormatFailure)
		} else {
			provider.Report(ctx, provider.VerdictProviderFailure)
		}
		return repaired, err
	}
	return repaired, nil
}

// repair is the seam's second half: everything that happens after a reply
// failed to decode. It is separate from Answer only so that the happy path —
// which is almost every call — reads as the three lines it actually is.
func repair(ctx context.Context, client Completer, ask Ask, model string, ceiling int,
	response *ai.Response, answered string, into any) (*ai.Response, error) {

	// A CUT ANSWER IS CONTINUED, NOT RE-BOUGHT, AND THE READING IS STRUCTURAL.
	// "Was an object begun and left open" is a fact about the text; the finish
	// reason is a claim about the call, and the two disagree often enough that
	// only the first can be trusted. Where they agree the memo learns from it:
	// a ceiling this model has been watched overrunning on this lane is one this
	// process will not send again.
	spent := spentOn(response, ceiling)
	if cutAtCeiling(response) {
		if provider.NoteAnswerCut(model, ask.Lane, spent) {
			// Nothing waits on this. The write is off the request path in the
			// memo itself; what happens here is only that the fact is now true
			// for the next call and for the next process.
			_ = spent
		}
	}

	joined := answered
	for round := 1; ; round++ {
		partial, unclosed := provider.UnclosedJSONObject(joined)
		if !unclosed {
			break
		}
		// THE ROUND LAW: a continuation is admitted only while the last one ADDED
		// text and the total spend is still inside what one completion is allowed
		// to be. Both quantities move one way, so the loop cannot fail to end —
		// this is the growth journal's argument applied to a single answer
		// instead of to a graph, and for the same reason: a bound that is
		// derived from what happened terminates, while a counter is a guess
		// about how many times something ought to be worth trying.
		if spent >= reserve() {
			break
		}
		note(ctx, Repair{Lane: ask.Lane, Model: model, Kind: RepairContinued,
			Round: round, Spent: spent, Ceiling: ceiling})
		more, err := client.CompleteWithMessages(ctx, continuation(ask, partial), ask.request(ceiling)...)
		if err != nil {
			return response, fmt.Errorf("%s: %w", ask.laneWords(), err)
		}
		added := strings.TrimSpace(text(more))
		if added == "" {
			break
		}
		response = more
		spent += spentOn(more, ceiling)
		joined = stitch(partial, added)
		if provider.DecodeJSONObject(joined, into) == nil {
			return withText(response, joined), nil
		}
		if cutAtCeiling(more) {
			provider.NoteAnswerCut(model, ask.Lane, completionTokens(more))
			continue
		}
		// It stopped of its own accord and the object is still not whole. There
		// is nothing left for a continuation to do, so this falls through to the
		// one re-ask below with whatever was said.
		break
	}

	// NOTHING TO CONTINUE. Either no object was ever begun — prose, or a whole
	// ceiling spent deliberating — or the continuation could not close one. One
	// re-ask, carrying the contract and the model's own offending words, at the
	// doubled room the harness has always used for a reply that ran out (the
	// same arithmetic as plan.retryTokenBudget and revision.retryVerdictTokens,
	// which is now written once, here).
	note(ctx, Repair{Lane: ask.Lane, Model: model, Kind: RepairReasked,
		Round: 1, Spent: spent, Ceiling: ceiling})
	again, err := client.CompleteWithMessages(ctx, reask(ask, joined), ask.request(doubled(spent, ceiling))...)
	if err != nil {
		return response, fmt.Errorf("%s: %w", ask.laneWords(), err)
	}
	if provider.DecodeJSONObject(text(again), into) == nil {
		return again, nil
	}
	// A second unreadable answer is the model's problem and not the budget's.
	// What matters here is only that it is reported as a FAULT, in a type the
	// caller cannot mistake for silence.
	note(ctx, Repair{Lane: ask.Lane, Model: model, Kind: RepairFailed,
		Round: 1, Spent: spent + spentOn(again, ceiling), Ceiling: ceiling})
	return again, fmt.Errorf("%s: %w%s", ask.laneWords(), ErrUnreadable, detail(again))
}

// request is the option list one send goes out with: the shape, when this build
// can carry one, and the room.
//
// The list is built fresh every time and the ceiling goes last, where it wins.
// Neither is a nicety — two appends onto one slice with spare capacity write
// over each other, and a repair would go out carrying the first send's ceiling.
func (a Ask) request(ceiling int) []ai.Option {
	options := make([]ai.Option, 0, 2)
	if a.Routed && len(a.Schema) > 0 {
		options = append(options, ai.WithSchema(a.Schema))
	}
	return append(options, ai.WithMaxTokens(ceiling))
}

// laneWords is how this seam names itself in an error a person may read. The
// lane is the caller's own word for the pass, so "plan request" and "gate
// request" are what appear rather than a package name nobody outside this file
// has heard of.
func (a Ask) laneWords() string {
	if lane := strings.TrimSpace(a.Lane); lane != "" {
		return lane + " request"
	}
	return "structured request"
}

// continuation asks for the rest of an answer that ran out of room.
//
// The partial reply is put back as the assistant turn it was, and the
// instruction follows it, because that is the arrangement every provider agrees
// on: an assistant message is what was said, and a user message is what is being
// asked next. The instruction is explicit about emitting no preamble and no
// fence, because a continuation that starts with "Here is the rest:" is a
// continuation that cannot be joined.
func continuation(ask Ask, partial string) []ai.Message {
	messages := make([]ai.Message, 0, len(ask.Messages)+2)
	messages = append(messages, ask.Messages...)
	messages = append(messages,
		ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: partial}}},
		ai.Message{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: continuePrompt}}})
	return messages
}

const continuePrompt = `Your answer above was cut off before the JSON object closed — you reached the output limit, not the end of what you had to say.

Continue from exactly where it stops. Emit ONLY the characters that come next in that same object, ending with its closing brace. Do not repeat anything you already wrote, do not restate the object from the beginning, do not add any explanation, and do not wrap it in a code fence. Your reply will be appended to what you wrote, character for character.

If continuing is not possible, reply with the whole object again, complete and shorter, and nothing else.`

// reask is the one retry for an answer that was not an object at all. It quotes
// the offending reply back, because a model told only "that was not JSON" has no
// idea which part of what it said was the problem, and because a reply it can
// see is a reply it can correct rather than regenerate.
func reask(ask Ask, offending string) []ai.Message {
	contract := "Your reply could not be read as the JSON object this asked for."
	if quoted := strings.TrimSpace(offending); quoted != "" {
		contract += "\n\nThis is what you sent:\n" + clip(quoted, offendingQuoteBytes)
	}
	contract += "\n\nAnswer again. Return exactly one JSON object and nothing else — no prose before or after it, no code fence, every brace and quote closed."
	if len(ask.Schema) > 0 {
		contract += "\n\nThe object must match this schema:\n" + string(ask.Schema)
	}
	messages := make([]ai.Message, 0, len(ask.Messages)+1)
	messages = append(messages, ask.Messages...)
	return append(messages, ai.Message{Role: "user",
		Content: []ai.ContentPart{{Type: "text", Text: contract}}})
}

// offendingQuoteBytes bounds the reply quoted back to the model. It is the one
// piece of the re-ask that could be unbounded — a model that answered with a
// whole document is exactly the model this path is for — and the first part of a
// wrong answer is where the wrongness is, so the head is what is kept.
const offendingQuoteBytes = 4000

// stitch joins a continuation onto the fragment it continues.
//
// IT DECIDES BY DECODING AND NEVER BY GUESSING, which is the only way to do this
// safely. The obvious heuristic — drop whatever the continuation repeats of the
// fragment's tail — is wrong in a way that is silent and expensive: a fragment
// ending "fix the fol" and a continuation beginning "low state" share the letter
// l, and trimming it produces "folow" inside an object that still parses. So the
// plain join is tried first, the continuation alone second (a model that ignored
// the instruction and re-sent the whole object has answered by another route),
// and an overlap is only ever removed when removing it is what makes the result
// a complete object.
//
// When nothing decodes, the plain join is returned — still open, still
// continuable, which is exactly what the next round needs.
func stitch(partial, added string) string {
	added = strings.TrimPrefix(strings.TrimSpace(added), "```json")
	added = strings.TrimPrefix(added, "```")
	if joined := partial + added; decodes(joined) {
		return joined
	}
	if decodes(added) {
		return added
	}
	limit := len(partial)
	if limit > overlapScanBytes {
		limit = overlapScanBytes
	}
	if limit > len(added) {
		limit = len(added)
	}
	for size := limit; size > 0; size-- {
		if !strings.HasSuffix(partial, added[:size]) {
			continue
		}
		if joined := partial + added[size:]; decodes(joined) {
			return joined
		}
	}
	return partial + added
}

// decodes asks only whether the text holds one complete JSON object, into a
// target that accepts any, so the caller's own destination is written exactly
// once and never from a candidate that lost.
func decodes(text string) bool {
	return provider.DecodeJSONObject(text, &struct{}{}) == nil
}

// overlapScanBytes bounds how far back a repeated tail is looked for. An
// unbounded scan over two long strings is quadratic for no gain: a model that
// repeats itself repeats a line or two, never a page.
const overlapScanBytes = 512

// cutAtCeiling reports the wire's own account of a truncation. It is never the
// reason a repair is chosen — the text decides that — but it is the reason the
// memo learns, because a reply that stopped because the room ran out is the only
// evidence that the room was too small.
func cutAtCeiling(response *ai.Response) bool {
	return response != nil && len(response.Choices) > 0 &&
		response.Choices[0].FinishReason == "length"
}

// spentOn is what one reply cost, in the units the round law counts.
//
// A provider that reports no usage is common enough to matter here, and the
// naive reading of it — nothing was spent — makes the bound above stop bounding:
// a model that answers with the same unterminated fragment forever would be
// continued forever, because the quantity that was supposed to grow never did. A
// reply that was CUT is the case that reads honestly without the usage block: it
// stopped because the room ran out, so it spent the room. That is what is
// counted when nothing else is known, and it keeps the number of continuations
// at reserve÷ceiling whatever a provider chooses to report.
func spentOn(response *ai.Response, ceiling int) int {
	if spent := completionTokens(response); spent > 0 {
		return spent
	}
	if cutAtCeiling(response) {
		return ceiling
	}
	return 0
}

func completionTokens(response *ai.Response) int {
	if response == nil || response.Usage == nil {
		return 0
	}
	return response.Usage.CompletionTokens
}

func text(response *ai.Response) string {
	if response == nil {
		return ""
	}
	return response.Text()
}

// withText hands back the joined answer on the last response, so a caller that
// reads the text after a continuation reads the whole of it rather than the
// final fragment. The usage and the choice metadata are the last call's, which
// is what they have always been.
func withText(response *ai.Response, joined string) *ai.Response {
	if response == nil || len(response.Choices) == 0 {
		return response
	}
	copied := *response
	copied.Choices = append([]ai.Choice(nil), response.Choices...)
	copied.Choices[0].Message.Content = []ai.ContentPart{{Type: "text", Text: joined}}
	return &copied
}

// detail turns an unusable reply into a diagnosable one. A truncated answer and
// a refused one both arrive as empty text, and only the finish reason tells them
// apart — on a reasoning model the usual cause is the whole budget being spent
// thinking, which the completion count makes obvious.
func detail(response *ai.Response) string {
	if response == nil || len(response.Choices) == 0 {
		return ""
	}
	words := fmt.Sprintf(" (finish_reason=%s", response.Choices[0].FinishReason)
	if usage := response.Usage; usage != nil {
		words += fmt.Sprintf(" completion_tokens=%d", usage.CompletionTokens)
	}
	return words + ")"
}

func clip(text string, bytes int) string {
	if len(text) <= bytes {
		return text
	}
	return text[:bytes] + "…"
}
