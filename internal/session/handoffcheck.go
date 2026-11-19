package session

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── a turn that handed work out ends honestly about it ──────────────────────
//
// THE MEASURED FAILURE (connected-canvas qloop run 16, vector-db t1, scored
// 3.6). The turn built two charts, handed the comparison table to task 1 and
// the brief to task 2, and task 2 did not start (program_depends.go says why).
// The model read the refusal, called `tasks`, saw only task 1 running, and
// ended the turn: "The brief — the write-up, which follows once the table
// lands … I'll report when the table and the brief are in." The turn sealed as
// finished, the person's chat showed "Not started · The research brief doc"
// under it, and the brief stayed an empty page. Nothing was ever going to
// start it: a task that did not start has no report to come.
//
// So a reply about to end a turn is read for two facts, both off this turn's
// own transcript and the work still out, never off a second model:
//
//   - WORK HANDED OUT THAT DID NOT START ([turnUnstartedHandoffs]). Every door
//     that starts a run answers a failed start in one spelling
//     ([runDidNotStart]), and the call that asked for it carries its title. A
//     later start of the same title in the same turn is a retry, and clears it.
//     Anything left is sent back once with the lines in hand: start it again,
//     or say plainly which work did not start and what that leaves undone.
//   - A PROMISE OF A LATER REPORT WITH NOTHING OUT ([promisesALaterReport],
//     [Agent.workStillOut]). "I'll report when it lands" is the truth while a
//     run, a task or a background command of this conversation is still
//     going, because its ending starts a turn here by itself. With nothing
//     going it is a promise nobody will keep, and the turn is sent back once
//     to finish the work now or say what is and is not done.
//
// IT IS THE REPLY CHECK'S SEAM AND ITS SHAPE (replycheck.go, failurecheck.go):
// one entry from [Agent.checkpointReopen], ONCE A TURN read off the transcript
// ([handoffCheckSpent]), [NoChangeReply] keeps a reply that already said it,
// and the line said while it works is a carry-on notice typed
// [NoticeKindSentBack], which the app keeps as a quiet line of the turn. Its
// record is the continuation in the transcript and a rule line in the session
// journal ([Agent.journalSentBack]).

// handoffCheckNote is the dim line for the unstarted arm.
const handoffCheckNote = "some work it handed out did not start · asking it to start it again or say so"

// handoffPromiseNote is the dim line for the promise arm.
const handoffPromiseNote = "it promised a later report with nothing running · asking it to finish or say what is left"

// handoffCheckLeadOpening and [handoffCheckLeadClosing] frame the unstarted
// arm's continuation; the work that did not start is between them.
const handoffCheckLeadOpening = "[carry on] Before your reply goes out: work you handed out this turn did not start, so nothing is running for it and no report about it will come:\n"

const handoffCheckLeadClosing = "\nStart it again now if it can start (each line says what stopped it), or write the whole reply again for the person " +
	"saying plainly which work did not start and what that leaves undone. Do not promise a report about work that is not running. " +
	"If the reply already says that, reply with exactly " + NoChangeReply + " and nothing else."

// handoffPromiseLead is the promise arm's continuation, one fixed sentence.
const handoffPromiseLead = "[carry on] Your reply tells the person you will report back later, but nothing you started is still running, " +
	"so nothing will start a turn here to report from. Finish the work now in this same turn, or write the whole reply again " +
	"without promising a later report, saying what is done and what is not. If the reply makes no such promise, reply with exactly " +
	NoChangeReply + " and nothing else."

// handoffCheckLead is the unstarted arm's whole continuation.
func handoffCheckLead(unstarted []string) string {
	return handoffCheckLeadOpening + strings.Join(unstarted, "\n") + handoffCheckLeadClosing
}

// isHandoffCheckLead recognises either arm's continuation, so every reader
// that hides the harness's own notes hides these too ([isContinuationContext])
// and a [NoChangeReply] after one is honoured ([answeredNoteUnchanged]).
func isHandoffCheckLead(text string) bool {
	return text == handoffPromiseLead ||
		(strings.HasPrefix(text, handoffCheckLeadOpening) && strings.HasSuffix(text, handoffCheckLeadClosing))
}

// runDidNotStartOpening and [runDidNotStartMiddle] are how [runDidNotStart]
// spells its first words, so this file reads the one spelling every start door
// answers in rather than a second copy of it.
const (
	runDidNotStartOpening = "task "
	runDidNotStartMiddle  = " did not start: "
	runStartedMiddle      = " started: "
)

// handoffCheckListed bounds how many unstarted pieces of work one send-back
// names, and [failureCheckLineBytes] bounds each line, as the failure check's.
const handoffCheckListed = 4

// turnUnstartedHandoffs reads this turn — from the person's message
// ([personTurnOpenedAt]) — for work handed out that did not start and was not
// started again under the same title later in the turn. One line each, naming
// the task, its title when the call carried one, and what stopped it.
func turnUnstartedHandoffs(messages []ai.Message) []string {
	from := personTurnOpenedAt(messages)
	titles := map[string]string{}
	for _, message := range messages[from:] {
		for _, call := range message.ToolCalls {
			titles[strings.TrimSpace(call.ID)] = callTitle(call.Function.Arguments)
		}
	}
	type unstarted struct{ title, line string }
	var open []unstarted
	for _, message := range messages[from:] {
		if message.Role != "tool" {
			continue
		}
		first := firstLine(partsText(message))
		if !strings.HasPrefix(first, runDidNotStartOpening) {
			continue
		}
		title := titles[strings.TrimSpace(message.ToolCallID)]
		if started, ok := startedTitle(first); ok {
			kept := open[:0]
			for _, item := range open {
				if !strings.EqualFold(item.title, started) && !strings.EqualFold(item.title, title) {
					kept = append(kept, item)
				}
			}
			open = kept
			continue
		}
		head, reason, ok := strings.Cut(first, runDidNotStartMiddle)
		if !ok {
			continue
		}
		name := head
		if title != "" {
			name += " (" + title + ")"
		}
		reason = strings.TrimSuffix(strings.TrimSpace(strings.SplitN(reason, ". Nothing is running", 2)[0]), ".")
		open = append(open, unstarted{title: title, line: clip("- "+name+": "+reason, failureCheckLineBytes)})
	}
	var lines []string
	for index, item := range open {
		if index == handoffCheckListed {
			lines = append(lines, "- and more")
			break
		}
		lines = append(lines, item.line)
	}
	return lines
}

// startedTitle reads a start receipt's first line, `task N started: <title>`.
func startedTitle(first string) (string, bool) {
	_, title, ok := strings.Cut(first, runStartedMiddle)
	if !ok || strings.Contains(first, runDidNotStartMiddle) {
		return "", false
	}
	return strings.TrimSpace(title), true
}

// callTitle is the `title` a hand-off call carried, or "".
func callTitle(arguments string) string {
	var args struct {
		Title string `json:"title"`
	}
	if json.Unmarshal([]byte(arguments), &args) != nil {
		return ""
	}
	return strings.TrimSpace(args.Title)
}

// laterReport is the narrow set of first-person promises to come back with
// news. IT IS NARROW ON PURPOSE: the arm it guards fires only when nothing is
// running, so a phrase it misses costs what the turn cost before this existed,
// and a phrase it over-reads costs one short call answered [NoChangeReply].
var laterReport = regexp.MustCompile(`(?i)\b(i'll|i will|i’ll|will)\s+(report|let you know|tell you when|update you|get back to you|ping you)\b|\bonce (it|they|the [a-z ]{1,40}) (lands?|(is|are) in|(is|are) done)\b|\bwhen (it|they|the [a-z ]{1,40}) (lands?|(is|are) in)\b`)

// promisesALaterReport reports whether a reply promises to come back later.
func promisesALaterReport(said string) bool {
	return laterReport.MatchString(said)
}

// workStillOut reports whether anything this conversation started is still
// going and will start a turn here when it ends: a background command
// ([Agent.turnIsWaitingOnItsOwnWork]), a task in the graph, or the run now
// turning under the bash belt.
//
// IT IS NOT [Agent.turnEndsWaiting]. That reading is about whether the turn
// may be carried on, and it is vetoed by the person's own message; this one is
// a fact about the world — is there a report coming — and a typed turn that
// promises one needs the fact.
func (a *Agent) workStillOut() bool {
	if a.turnIsWaitingOnItsOwnWork() {
		return true
	}
	if _, live := a.tasker().liveWorkOf(a); live {
		return true
	}
	a.beltMu.Lock()
	defer a.beltMu.Unlock()
	live := a.beltRun
	return live != nil && !live.ending && !live.closing && !live.stopped
}

// handoffCheckSpent reports that this turn has already been sent back by
// either arm.
func handoffCheckSpent(messages []ai.Message) bool {
	for index := personTurnOpenedAt(messages); index < len(messages); index++ {
		if messages[index].Role == "user" && isHandoffCheckLead(strings.TrimSpace(partsText(messages[index]))) {
			return true
		}
	}
	return false
}

// replyHandoffsBack is the check's one entry from [Agent.checkpointReopen]. It
// reports whether the turn was sent back; when it was, the note, the journal
// line and the continuation are already written.
func (a *Agent) replyHandoffsBack(ctx context.Context, hub *eventHub, meter *checkpointMeter, said string) bool {
	if meter == nil || ctx.Err() != nil || a.config.InTask {
		return false
	}
	said = strings.TrimSpace(said)
	if said == "" || IsNoChangeReply(said) {
		return false
	}
	messages := a.snapshot()
	if handoffCheckSpent(messages) {
		return false
	}
	if unstarted := turnUnstartedHandoffs(messages); len(unstarted) > 0 {
		a.sendBack(hub, "handoff-did-not-start", handoffCheckNote, handoffCheckLead(unstarted))
		return true
	}
	if promisesALaterReport(said) && !a.workStillOut() {
		a.sendBack(hub, "promise-with-nothing-out", handoffPromiseNote, handoffPromiseLead)
		return true
	}
	return false
}

// sendBack is what every check on a finished reply does when it sends the
// turn back: one carry-on line while it works, typed [NoticeKindSentBack], one
// rule line in the session journal, and the continuation the model reads.
//
// THE LINE IS TYPED BECAUSE THE APP DRAWS IT. It explains why a reply the
// person has already read is followed by more work and a second reply, and
// the terminal folds it with the turn's work while the desktop app keeps it as
// a quiet line of that turn (codeaf app's pump). Untyped, the app could only
// tell it from the completion check's carry-on and the nudges by its words,
// and it dropped all of them alike: qloop run 26 sent 16 of 28 turns back
// and not one of those lines reached the canvas chat or any kept file.
func (a *Agent) sendBack(hub *eventHub, check, note, lead string) {
	hub.send(Event{Kind: EventNotice, Text: note, NoticeKind: NoticeKindSentBack})
	a.journalSentBack(check)
	a.record(textMessage("user", lead))
}

// journalSentBack writes one send-back down in the session journal, as a rule
// moment ([journalRule]) with the conversation's running count beside it, so
// the record of the check survives the person never seeing a line for it.
func (a *Agent) journalSentBack(check string) {
	a.journalFile().appendRule(journalRule{Rule: check, Event: "sent-back", Count: a.processRules.mark(check, true)})
}
