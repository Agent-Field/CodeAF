package session

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The one-action envelope, and the road a malformed response takes.
//
// THE ENVELOPE IS THE EXPERIMENT'S SECOND BET (docs/design/bash-task-loop/
// DESIGN.md, Decision 2): a bash-belt worker gets the one-action discipline —
// exactly one tool call per response — in exchange for parallelism moving into
// the shell, where `&` and `xargs -P` have always lived. A response carrying
// two calls is not a batch, it is a response the model could not drive the
// belt with, and running a piece of it would answer a question nobody asked.
//
// THE ONE CALL MAY NAME ANY HAND THIS BELT CARRIES, and bash is only the
// commonest. The belt keeps `jobs`, `read_document` and `manual` because none
// of them can be a shell command (Decision 6 and the tool table), and the
// worker's page sends the worker to the first two. An envelope that refused
// every name but bash made those hands present and broken at once: a worker
// whose `find /` had become a job called `jobs` to stop it, was told `jobs`
// was not on this belt, and the walk ran on for the rest of the task. A name
// the belt does not carry is still refused here, before anything runs.
//
// THE REJECT IS THE SAME ON BOTH BRANCHES. A response whose calls are
// addressable — every call carries a non-empty, unique id — is answered with
// the diagnostic as each call's tool result, exactly as a held process rule is
// (processrule.go's [Agent.withholdSubmission]): the transcript's shape is not
// negotiable, an assistant message naming three calls needs three tool
// messages after it, and nothing reaches the world. A response whose envelope
// cannot be addressed is DROPPED — the malformed assistant message leaves the
// replay history it just entered, and the diagnostic goes back as a user
// message — because a tool result must name a call the request carried, and
// this one did not.
//
// FOUR CONSECUTIVE invalid actions end the turn on the loop road the runner
// already reads (looped.go's [loopLeftUndoneNote], task_run.go's
// [endingOfClaim]): the model could not drive the belt, and a fifth request
// would be the person paying for the same lesson again. The count is read
// FROM THE RECORD, never from a counter on the agent — the transcript is the
// record, a worker's run is many turns, and state that survives in a struct
// while the transcript is the thing the next request is rebuilt from is state
// waiting to disagree with what the model actually reads.

const (
	// bashEnvelopeMark opens every diagnostic the envelope writes, on the tool
	// results or the user message that carries it. It is the one byte sequence
	// the trailing walk reads back, so it is a constant for the same reason
	// [invalidArgumentsPrefix] is: two spellings of the mark are two facts.
	bashEnvelopeMark = "[not run] "

	// bashEnvelopeLimit is how many consecutive invalid actions end the turn.
	// It is THE ONLY PLACE THE NUMBER LIVES: the doctrine page teaches the
	// envelope, not a number, so the page and this constant cannot drift
	// apart.
	bashEnvelopeLimit = 4
)

// bashEnvelopeStop is the notice a turn lands under when the budget is spent.
// The cause leads, because "went in circles" alone is a row with no handle on
// it, and the recorded sentence ends with [loopLeftUndoneNote] — the one
// sentence [endingOfClaim] reads a worker's last words for — so the node's
// landing is written by the road that already exists instead of a second one.
const bashEnvelopeStop = "stopped: four responses in a row carried no valid single bash action, so nothing was run · " + loopLeftUndoneNote

// bashEnvelopeFault is what is wrong with one submission under the envelope,
// and empty for one that may run. A response with no calls is a final answer,
// not an invalid action — ending the turn in words is how this loop finishes.
//
// WHAT THE BELT CARRIES IS READ OFF THE BELT ITSELF ([Agent.beltTools]), the
// same list the request's definitions were built from, so the envelope and the
// wire cannot come to disagree about which names a call may carry.
func (a *Agent) bashEnvelopeFault(calls []ai.ToolCall) string {
	if len(calls) == 0 {
		return ""
	}
	if len(calls) > 1 {
		return bashEnvelopeMark + "no action executed: return exactly one bash tool call per response — this belt has one hand, and the shell carries its own parallelism (& with wait, xargs -P)"
	}
	call := calls[0]
	if !bashCallsAddressable(calls) {
		return bashEnvelopeMark + "no action executed: a tool call carries no id, so its result could never be paired with it — send the bash call again as the provider's tool-call form"
	}
	if !a.beltCarries(call.Function.Name) {
		return bashEnvelopeMark + "no action executed: `" + call.Function.Name + "` is not on this belt — bash is the hand for files and commands, and what the belt cannot do is spelled in its own page"
	}
	// THE ARGUMENTS ARE READ WITH THE ONE DECODER EVERY TOOL USES, so a bash
	// call is refused in the same words on the branch belt as on today's — a
	// model learns one grammar of refusal, and the envelope adds nothing to it.
	if invalid := invalidBashArguments(call.Function.Name, json.RawMessage(call.Function.Arguments)); invalid != "" {
		return bashEnvelopeMark + "no action executed: " + invalid
	}
	return ""
}

// beltCarries answers whether a tool of this name is on the belt the request
// was built from.
func (a *Agent) beltCarries(name string) bool {
	for _, tool := range a.beltTools() {
		if tool.Name == name {
			return true
		}
	}
	return false
}

// rejectBashEnvelope answers one invalid submission without running any of it
// and without letting the malformed shape re-enter what the model reads next.
//
// THE ADDRESSABLE CASE KEEPS THE ASSISTANT MESSAGE and answers every call it
// named, exactly as a held submission is answered: three tool messages after a
// three-call assistant message is the transcript's shape, not a choice. The
// UNADDRESSABLE case — no id, a repeated id, so no tool result can be paired
// with its call — takes the assistant message back out, because a request
// rebuilt with it would carry tool calls whose results can never be paired
// with them, and appends the diagnostic as a user message instead. Both
// branches obey one law: the diagnostic reaches the model, the malformed
// envelope does not.
func (a *Agent) rejectBashEnvelope(hub *eventHub, calls []ai.ToolCall, diagnostic string) {
	if bashCallsAddressable(calls) {
		for _, call := range calls {
			a.record(ai.Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    []ai.ContentPart{{Type: "text", Text: diagnostic}},
			})
			hub.send(Event{
				Kind:        EventToolFailed,
				Tool:        call.Function.Name,
				CallID:      call.ID,
				Hint:        clip(firstLine(diagnostic), hintLimit),
				Args:        argsText(call),
				Output:      capOutput(diagnostic),
				HarnessMade: true,
			})
		}
		return
	}
	// AND THE MESSAGE THAT CANNOT BE ANSWERED GOES BACK OUT OF THE TRANSCRIPT.
	// It was recorded a few lines above this seam, so what is dropped is
	// exactly the response under test — the drop is guarded to that shape, and
	// the journal keeps what arrived, because the person's record shows what
	// happened even when the model's replay history must not carry it.
	a.dropLastAssistant()
	a.record(textMessage("user", diagnostic))
	hub.send(Event{
		Kind:        EventNotice,
		Text:        diagnostic,
		HarnessMade: true,
	})
}

// dropLastAssistant removes the assistant message [Agent.recordAssistant]
// appended moments ago, with its reasoning beside it, so the sidecar keeps the
// transcript's length. It answers nothing for a transcript whose last message
// is not the assistant message the caller means.
func (a *Agent) dropLastAssistant() {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := len(a.messages)
	if n == 0 || a.messages[n-1].Role != "assistant" {
		return
	}
	a.messages = a.messages[:n-1]
	if len(a.messageReasoning) == n {
		a.messageReasoning = a.messageReasoning[:n-1]
	}
}

// bashCallsAddressable answers whether every call in the batch can be given a
// tool result the next request can pair with it: an id on each, and no two
// alike. The two-branch split above turns on exactly this answer.
func bashCallsAddressable(calls []ai.ToolCall) bool {
	seen := make(map[string]bool, len(calls))
	for _, call := range calls {
		if call.ID == "" || call.Function.Name == "" {
			return false
		}
		if seen[call.ID] {
			return false
		}
		seen[call.ID] = true
	}
	return len(calls) > 0
}

// trailingBashEnvelopeRejections counts the invalid actions at the tail of the
// transcript, and it is READ FROM THE RECORD because the record is the thing
// the next request is rebuilt from. A rejection that reached the model is
// either a block of tool results carrying the mark or a user message carrying
// it; a valid action's result carries no mark and breaks the chain, which is
// the whole of what "consecutive" means. Nothing anywhere has to remember
// this number across turns, because the transcript already does.
func (a *Agent) trailingBashEnvelopeRejections() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	count := 0
	for index := len(a.messages) - 1; index >= 0; index-- {
		message := a.messages[index]
		switch message.Role {
		case "tool":
			// One rejected response answers all of its calls with the same
			// diagnostic; the count is of RESPONSES, so the block is walked
			// over and its assistant message counted once.
			if !strings.HasPrefix(messageContentText(message), bashEnvelopeMark) {
				return count
			}
		case "user":
			// THE HARNESS'S OWN OPENINGS ARE TRANSPARENT TO THE COUNT. The frame
			// ([bashBeltFrameOpening]) and the other two volatile openings land
			// between every rejection and the next, and they are nobody's answer
			// to anything — the count walks over them the same way the replay
			// history never lets them stand for a person's words
			// ([isVolatileNote]'s whole job). Breaking on them instead would
			// leave the count at zero on a belt where a frame lands every step.
			if isVolatileNote(messageContentText(message)) {
				continue
			}
			if !strings.HasPrefix(messageContentText(message), bashEnvelopeMark) {
				return count
			}
			count++
		case "assistant":
			if len(message.ToolCalls) == 0 {
				return count
			}
			count++
		default:
			return count
		}
	}
	return count
}

// enforceBashEnvelope is the branch belt's turn at the batch seam: it asks the
// validator about the submission and, when the answer is no, rejects it without
// running any of it — the loop takes its next round without the batch (skip),
// exactly as a held process rule's submission is answered — and it answers
// whether the turn has ENDED because the budget for invalid actions is spent.
func (a *Agent) enforceBashEnvelope(ctx context.Context, hub *eventHub, calls []ai.ToolCall, turn *Usage, started time.Time, model string) (skip, ended bool) {
	if !a.config.mayBashBelt() {
		return false, false
	}
	fault := a.bashEnvelopeFault(calls)
	if fault == "" {
		return false, false
	}
	a.rejectBashEnvelope(hub, calls, fault)
	if a.trailingBashEnvelopeRejections() < bashEnvelopeLimit {
		return true, false
	}
	// THE BUDGET IS SPENT, AND THE TURN ENDS ON THE ROAD THE RUNNER ALREADY
	// READS. No checkpoint hand-off: what would be handed over is a worker
	// under the same belt that could not drive four responses, and the honest
	// thing to say is that the work stopped (processrule.go's
	// [Agent.stopForProcessRule] states the same reasoning).
	hub.send(Event{Kind: EventNotice, Text: bashEnvelopeStop})
	a.record(textMessage("assistant", bashEnvelopeStop))
	hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(*turn, started, model)})
	a.maybeTitle(ctx, hub)
	return true, true
}
