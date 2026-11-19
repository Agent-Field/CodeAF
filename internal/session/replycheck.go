package session

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// ── the reply is read against the turn's own looks before it goes out ───────
//
// A REPLY ABOUT THE WORK IS A CLAIM ABOUT SOMETHING THE PERSON CAN SEE, and on
// the connected-canvas qloop (run 12, deepseek-v4.1-flash) it was the largest
// single cause of a low score: five turns told the person something the work
// did not show. "no layer overlaps the food or the cup" over a headline band
// that covers the cup; "Cost chart bars now: pgvector $2,100, Qdrant $2,300,
// …" over a chart whose bars run pgvector, Pinecone, Weaviate; a line quoted
// from a page that does not carry it; "each card shows role, source and
// recruiter" over cards that show two of those; "drag them across as work
// moves" over lanes that are workstreams, not stages. A skill paragraph and a
// prompt clause (#2835) did not move it.
//
// EVERY ONE OF THE FIVE HAD LOOKED, AFTER ITS LAST CHANGE. That is the fact the
// first sketch of this guard missed: it was going to fire when the work was
// edited after the last look, and it would have fired on none of them. The
// model looked, was told by its own look what the work showed, and wrote what
// it had meant to make instead. So the guard has two arms, both read off this
// turn's own transcript:
//
//   - THE STALE LOOK ([replyStaleLookLead]). The turn looked at the work, then
//     ran something that may have changed it, and is now describing it. Nothing
//     can be compared — the look is about the work as it was — so the model is
//     sent back once to look again. It costs no reader.
//   - THE CONTRADICTED LOOK ([replyCheckLeadOpening]). The newest look came
//     after the last change, so it is the best account of the work there is.
//     One call to [roles.RoleReplyCheck] is handed that look's words and the
//     reply, and asked for the statements the look contradicts — only those, so
//     what the look did not cover is never raised. If it names any, the model is
//     sent back once with them in hand.
//
// BOTH ARE ONCE A TURN, TOGETHER ([checkpointMeter.lookedBack]): at most one
// extra pass, at most one reader call, and a reply written after either is the
// reply the turn ends on. Each offers [NoChangeReply], which ends the turn on
// the reply already given and draws nothing ([answeredNoteUnchanged]), so a
// reply that was right costs one short call and is never written twice.
//
// THE READER'S BILL IS CAPPED. The reply is clipped to [replyCheckReplyBytes]
// and the looks to [replyCheckLookBytes], newest first, so one call reads at
// most a few thousand tokens whatever the turn did; it runs on the small-work
// tier; and it is asked only on a turn that looked at its work with a second
// model's eyes, which is where the words to compare against exist at all. With
// no model for the role, the arm is absent ([Agent.readReplyAgainstLooks]),
// and the stale arm — which needs no model — still stands.

func init() {
	roles.Register(roles.RoleReplyCheck, roles.TierLow, "whether a reply about the work says what the last look at it showed")
}

// replyStaleLookNote is the dim line a person reads when the turn is sent back
// to look again. It names what happened in their words; the reply above it
// stays on screen until the new one arrives.
const replyStaleLookNote = "it changed the work after its last look · asking it to look again before it replies"

// replyCheckNote is the line for the contradicted arm.
const replyCheckNote = "its reply did not match its own last look · asking it to correct the reply"

// replyStaleLookLead is the stale arm's continuation, one fixed sentence.
const replyStaleLookLead = "[carry on] Your reply describes the work, but you ran more after your last look at it, " +
	"so the reply may describe it as it was before. Look at it again now, then write the whole reply again for the person " +
	"from what that look shows. If nothing your reply describes has changed since that look, reply with exactly " +
	NoChangeReply + " and nothing else."

// replyCheckLeadOpening opens the contradicted arm's continuation; the reader's
// lines follow it, and [replyCheckLeadClosing] ends it. The two halves are what
// [isReplyCheckLead] recognises, so the varying middle never has to be matched.
const replyCheckLeadOpening = "[carry on] Before your reply goes out: your own look at the work this turn shows something different from what the reply says.\n"

const replyCheckLeadClosing = "\nWrite the whole reply again for the person, saying only what your look shows; look again first if you think it is wrong. " +
	"If the reply is right as written, reply with exactly " + NoChangeReply + " and nothing else."

// replyCheckLead is the contradicted arm's whole continuation.
func replyCheckLead(found []string) string {
	return replyCheckLeadOpening + strings.Join(found, "\n") + replyCheckLeadClosing
}

// isReplyCheckLead reports one of this file's two continuations, so that every
// reader of the transcript that hides the harness's own notes hides these too
// ([isContinuationContext]) and a [NoChangeReply] after one is honoured.
func isReplyCheckLead(text string) bool {
	return text == replyStaleLookLead ||
		(strings.HasPrefix(text, replyCheckLeadOpening) && strings.HasSuffix(text, replyCheckLeadClosing))
}

const (
	// replyCheckReplyBytes bounds the reply the reader is shown.
	replyCheckReplyBytes = 4000
	// replyCheckLookBytes bounds the looks the reader is shown, all of them
	// together; one look is never given more than [replyCheckOneLookBytes].
	replyCheckLookBytes    = 9000
	replyCheckOneLookBytes = 4500
	// replyCheckFindings is the most statements the reader may raise.
	replyCheckFindings = 5
	// replyCheckFindingBytes bounds one of them.
	replyCheckFindingBytes = 300
)

// replyCheckAsk is the reader's question, put after the evidence because the
// models on this tier answer whatever they read last.
const replyCheckAsk = "Above are the looks an assistant took at its own work this turn (each describes a picture of the work; newest last), " +
	"and the reply it is about to send to a person.\n" +
	"List every statement in the reply about what the work shows or contains — an order, a count, a position, a label, " +
	"a quoted line, what overlaps what, what a card or row shows — that the NEWEST look contradicts. " +
	"Quote the reply's words, then say what the look shows instead. " +
	"Leave out anything the looks do not cover, and leave out offers, plans and questions.\n" +
	"Answer with at most five lines, each starting with \"- \". If the looks contradict nothing in the reply, answer exactly NONE."

// turnLook is one look this turn took at its work: where its call sits in the
// transcript, and the words a second model wrote about the picture, when it
// wrote any.
type turnLook struct {
	at       int
	question string
	seen     string
}

// lookedAtTheWork reads this turn — from the person's message
// ([personTurnOpenedAt]) to the end — for its looks and for whether anything
// that may change the work ran after the newest one.
//
// A LOOK IS READ OFF THE CALL AND ITS RESULT, NEVER OFF WHAT WAS SAID: a
// `view_image` call, or any result a second model wrote about a picture
// ("seen by …" from `view_image`, "[vision: …]" from a `read` of an image).
// AFTER THE NEWEST LOOK, any call that is not itself a look and not one the
// approval floor already knows cannot change anything ([approval.ReadOnly]) is
// taken to have changed the work. A shell line cannot be told apart from its
// name, and the price of that is one short call answered [NoChangeReply].
func lookedAtTheWork(messages []ai.Message) (looks []turnLook, changedSince bool) {
	from := personTurnOpenedAt(messages)
	results := map[string]string{}
	for _, message := range messages[from:] {
		if message.Role == "tool" {
			results[strings.TrimSpace(message.ToolCallID)] = strings.TrimSpace(partsText(message))
		}
	}
	for index := from; index < len(messages); index++ {
		message := messages[index]
		if message.Role != "assistant" {
			continue
		}
		for _, call := range message.ToolCalls {
			name := strings.TrimSpace(call.Function.Name)
			result := results[strings.TrimSpace(call.ID)]
			seen := lookWords(result)
			if name == "view_image" || seen != "" {
				looks = append(looks, turnLook{at: index, question: checkpointArgumentNamed(call.Function.Arguments, "question"), seen: seen})
				changedSince = false
				continue
			}
			if len(looks) > 0 && !approval.ReadOnly(name, json.RawMessage(call.Function.Arguments)) {
				changedSince = true
			}
		}
	}
	return looks, changedSince
}

// lookWords is what a second model said about a picture, without the line that
// names it, or "" when the result is not one.
func lookWords(result string) string {
	switch {
	case strings.HasPrefix(result, "seen by "):
		if colon := strings.Index(result, ": "); colon > 0 {
			return strings.TrimSpace(result[colon+2:])
		}
	case strings.HasPrefix(result, "[vision: "):
		if end := strings.Index(result, "]"); end > 0 {
			return strings.TrimSpace(result[end+1:])
		}
	}
	return ""
}

// personTurnOpenedAt is where the person's part of this turn begins: the newest
// user message that is not a note the harness wrote. [turnOpenedAt] stops at
// any user message, and a send-back of this file's own is one.
func personTurnOpenedAt(messages []ai.Message) int {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role != "user" {
			continue
		}
		text := strings.TrimSpace(partsText(messages[index]))
		if isCodeafNote(text) || isContinuationContext(text) || isReplyCheckLead(text) || isFailureCheckLead(text) {
			continue
		}
		return index
	}
	return 0
}

// replyLooksBack is the guard's one entry from [Agent.checkpointReopen]. It
// reports whether the turn was sent back; when it was, the note and the
// continuation are already written.
func (a *Agent) replyLooksBack(ctx context.Context, hub *eventHub, meter *checkpointMeter, said string) bool {
	if meter == nil || meter.lookedBack || ctx.Err() != nil {
		return false
	}
	said = strings.TrimSpace(said)
	if said == "" || IsNoChangeReply(said) {
		return false
	}
	if _, interim := UserFacingUpdate(said); interim {
		return false
	}
	looks, changedSince := lookedAtTheWork(a.snapshot())
	if len(looks) == 0 {
		return false
	}
	if changedSince {
		meter.lookedBack = true
		a.sendBack(hub, "reply-stale-look", replyStaleLookNote, replyStaleLookLead)
		return true
	}
	found := a.readReplyAgainstLooks(ctx, said, looks)
	// THE READER IS SPENT EITHER WAY. A turn whose reply the reader cleared is
	// not read a second time on its next stop, which is what keeps this at one
	// call a turn.
	meter.lookedBack = true
	if len(found) == 0 {
		return false
	}
	a.sendBack(hub, "reply-contradicts-look", replyCheckNote, replyCheckLead(found))
	return true
}

// readReplyAgainstLooks asks [roles.RoleReplyCheck] which statements in the
// reply the looks contradict. Nothing comes back — no model for the role, no
// words to compare, a failed call, an answer not in the asked shape — and the
// reply goes out as written: A CAPABILITY THAT CANNOT WORK IS ABSENT.
func (a *Agent) readReplyAgainstLooks(ctx context.Context, said string, looks []turnLook) []string {
	page := replyCheckPage(said, looks)
	if page == "" {
		return nil
	}
	if !a.hasClient() {
		return nil
	}
	if _, err := a.roleLadder(roles.RoleReplyCheck, ""); err != nil {
		return nil
	}
	ctx, done := context.WithTimeout(ctx, checkpointSketchWindow)
	defer done()
	response, model, err := a.callRole(ctx, roles.RoleReplyCheck, "", []ai.Message{textMessage("user", page+"\n\n"+replyCheckAsk)})
	if err != nil || response == nil {
		return nil
	}
	a.addAuxiliaryUsageAs(response, model, 1, string(roles.RoleReplyCheck))
	return replyCheckFindingsOf(response.Text())
}

// replyCheckPage is what the reader is shown: the looks that carry words, newest
// kept first under the bound and then put back in order, and the reply. A turn
// whose looks carry no words — a model that saw the picture itself — has
// nothing to compare against, and the page is empty.
func replyCheckPage(said string, looks []turnLook) string {
	var kept []string
	room := replyCheckLookBytes
	for index := len(looks) - 1; index >= 0 && room > 0; index-- {
		look := looks[index]
		if look.seen == "" {
			continue
		}
		var entry strings.Builder
		entry.WriteString("LOOK")
		if look.question != "" {
			entry.WriteString(" (asked: " + strconv.Quote(clip(look.question, 300)) + ")")
		}
		entry.WriteString(":\n")
		entry.WriteString(clip(look.seen, min(replyCheckOneLookBytes, room)))
		room -= entry.Len()
		kept = append(kept, entry.String())
	}
	if len(kept) == 0 {
		return ""
	}
	for left, right := 0, len(kept)-1; left < right; left, right = left+1, right-1 {
		kept[left], kept[right] = kept[right], kept[left]
	}
	return strings.Join(kept, "\n\n") + "\n\nREPLY:\n" + clip(said, replyCheckReplyBytes)
}

// replyCheckFindingsOf reads the reader's answer into the lines it raised. ONLY
// THE ASKED SHAPE COUNTS: "- " lines, at most [replyCheckFindings]. NONE, an
// empty answer and prose that is neither are all "nothing contradicted", because
// a send-back the reader did not clearly ask for is a turn spent on a guess.
func replyCheckFindingsOf(answer string) []string {
	answer = strings.TrimSpace(answer)
	if answer == "" || strings.EqualFold(strings.Trim(answer, ". "), "NONE") {
		return nil
	}
	var found []string
	for _, line := range strings.Split(answer, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") || strings.TrimSpace(line[2:]) == "" {
			continue
		}
		found = append(found, clip(line, replyCheckFindingBytes))
		if len(found) == replyCheckFindings {
			break
		}
	}
	return found
}
