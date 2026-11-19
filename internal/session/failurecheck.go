package session

import (
	"context"
	"strconv"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── a question after failed steps says what failed ──────────────────────────
//
// A TURN THAT CHANGED NOTHING AND THEN ASKS IS A TURN THE PERSON CANNOT READ.
// Measured on the connected-canvas qloop (run 12, sales-dashboard t3, "the chart
// is wrong, use net"): eight steps, two of them failed, three minutes, nothing
// on the canvas moved, and the turn ended on a choice card. The person saw
// "8 actions · 2 failed" and a question, and nowhere did anything say what had
// failed or that the work was untouched. It scored 5.2.
//
// THE ENGINE CANNOT SEE "NOTHING CHANGED", AND THIS DOES NOT PRETEND TO. The
// work in that turn is a canvas reached through shell lines, and a shell line's
// effect is not in its name. What the engine does know is its own account of
// each step: which ones FAILED ([toolStatus] wrote it on the result), and which
// ones were the belt's own writers and succeeded ([checkpointWriters]). So of
// the two remedies the brief allowed — send the failures back so the model
// retries once, or make the reply say plainly what failed and that nothing
// changed — this is BOTH IN ONE SEND-BACK, and the model, which does know what
// it changed, picks: redo the step that was meant to make the change, or put
// the failures and what did and did not change into the question. A rule that
// asserted "nothing changed" itself would be the engine lying on every turn
// whose shell line did change something.
//
// IT HAS TWO DOORS, BECAUSE A QUESTION HAS TWO SHAPES. A reply in words that
// ends on a question reaches [Agent.checkpointReopen] like any reply, and is sent
// back with [failureCheckLead]. An `ask` card never ends a turn that way — the
// turn parks inside the tool — so the card is held once before it is drawn
// ([askHeldAfterFailuresLead], from [Agent.raiseAsk]), and nothing reaches the
// person until the model asks again.
//
// ONCE A TURN, ACROSS BOTH DOORS ([failureCheckSpent]), read off the transcript
// so neither door needs the other's state. The price: a turn that recovered
// from a failed step, finished, and ends on an offer ("Want the subline
// larger?") is asked once and answers [NoChangeReply], or asks its card again.

// failureCheckNote is the dim line a person reads when the turn is sent back.
const failureCheckNote = "some of its steps failed before it asked you · asking it to retry them or say what failed"

// failureCheckLeadOpening and [failureCheckLeadClosing] frame the reply door's
// continuation; the failed steps are between them.
const failureCheckLeadOpening = "[carry on] Your reply ends by asking the person, and some of your steps this turn failed:\n"

const failureCheckLeadClosing = "\nIf one of them was meant to make the change the person asked for, do it correctly now in this same turn and then answer. " +
	"If you still need to ask, write the reply again and say plainly in it which steps failed and what you did and did not change. " +
	"If the reply already says that, or those failures did not touch what the person asked for, reply with exactly " +
	NoChangeReply + " and nothing else."

// askHeldAfterFailuresLead opens what `ask` answers when it holds a card. IT IS
// NOT [askRefusedLead]: the card was well-formed, nothing is wrong with it, and
// [askRefusedAndNotRetried] must not send the model round again to "fix" a card
// it may now rightly never ask.
const askHeldAfterFailuresLead = "the person has not seen this question yet: some of your steps this turn failed:\n"

const askHeldAfterFailuresClosing = "\nIf one of them was meant to make the change the person asked for, do it correctly now instead of asking. " +
	"If you still need the person, call ask again and say plainly in it which steps failed and what you did and did not change; it will be shown."

// failureCheckLead is the reply door's whole continuation.
func failureCheckLead(failed []string) string {
	return failureCheckLeadOpening + strings.Join(failed, "\n") + failureCheckLeadClosing
}

// isFailureCheckLead recognises the reply door's continuation by its two ends.
func isFailureCheckLead(text string) bool {
	return strings.HasPrefix(text, failureCheckLeadOpening) && strings.HasSuffix(text, failureCheckLeadClosing)
}

const (
	// failureCheckListed is the most failed steps a send-back names.
	failureCheckListed = 4
	// failureCheckLineBytes bounds the line naming one of them.
	failureCheckLineBytes = 200
)

// turnFailedSteps reads this turn — from the person's message
// ([personTurnOpenedAt]) — for the steps that failed, one line each, and for
// whether one of the belt's own writers succeeded.
func (a *Agent) turnFailedSteps() (failed []string, wrote bool) {
	messages := a.snapshot()
	a.mu.Lock()
	marks := a.presentation
	a.mu.Unlock()
	from := personTurnOpenedAt(messages)
	names := map[string]string{}
	for _, message := range messages[from:] {
		for _, call := range message.ToolCalls {
			names[strings.TrimSpace(call.ID)] = strings.TrimSpace(call.Function.Name)
		}
	}
	count := 0
	for _, message := range messages[from:] {
		if message.Role != "tool" {
			continue
		}
		name := names[strings.TrimSpace(message.ToolCallID)]
		mark := marks.of(message)
		if mark == nil || mark.ToolOutcome != "failed" {
			if mark != nil && mark.ToolOutcome == "ok" && checkpointWriters[name] {
				wrote = true
			}
			continue
		}
		count++
		if len(failed) < failureCheckListed {
			failed = append(failed, clip("- "+name+": "+firstLine(partsText(message)), failureCheckLineBytes))
		}
	}
	if count > len(failed) {
		failed = append(failed, "- and "+strconv.Itoa(count-len(failed))+" more")
	}
	return failed, wrote
}

// failureCheckSpent reports that this turn has already been asked, by either
// door.
func failureCheckSpent(messages []ai.Message) bool {
	for index := personTurnOpenedAt(messages); index < len(messages); index++ {
		text := strings.TrimSpace(partsText(messages[index]))
		switch messages[index].Role {
		case "user":
			if isFailureCheckLead(text) {
				return true
			}
		case "tool":
			if strings.HasPrefix(text, askHeldAfterFailuresLead) {
				return true
			}
		}
	}
	return false
}

// failuresBeforeAQuestion is the shared test both doors ask: the turn has
// failed steps, none of the belt's writers succeeded, and it has not been asked
// already. It answers the lines to name.
func (a *Agent) failuresBeforeAQuestion() ([]string, bool) {
	if failureCheckSpent(a.snapshot()) {
		return nil, false
	}
	failed, wrote := a.turnFailedSteps()
	if len(failed) == 0 || wrote {
		return nil, false
	}
	return failed, true
}

// replyFailuresBack is the reply door, from [Agent.checkpointReopen].
func (a *Agent) replyFailuresBack(ctx context.Context, hub *eventHub, meter *checkpointMeter, said string) bool {
	if meter == nil || ctx.Err() != nil || !endsAskingThePerson(said) || IsNoChangeReply(strings.TrimSpace(said)) {
		return false
	}
	failed, ok := a.failuresBeforeAQuestion()
	if !ok {
		return false
	}
	a.sendBack(hub, "failures-before-question", failureCheckNote, failureCheckLead(failed))
	return true
}

// holdAskAfterFailures is the card door, from [Agent.raiseAsk]: the text `ask`
// answers instead of drawing the card, or "" to draw it.
//
// IT SAYS NOTHING TO THE PERSON. A tool has no stream of its own to say it on,
// and there is nothing yet to say: no card was drawn, and the next thing the
// person sees is either the work done or the card asked again with the
// failures in it.
func (a *Agent) holdAskAfterFailures() string {
	failed, ok := a.failuresBeforeAQuestion()
	if !ok {
		return ""
	}
	return askHeldAfterFailuresLead + strings.Join(failed, "\n") + askHeldAfterFailuresClosing
}
