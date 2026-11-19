package session

import (
	"context"
	"encoding/json"
	"sort"
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
//     ran something that may have changed it, and is now describing it. The
//     model is sent back once to look again — after the reader has read the
//     reply against what the turn did see and raised nothing (run 24, below).
//   - THE CONTRADICTED LOOK ([replyCheckLeadOpening]). One call to
//     [roles.RoleReplyCheck] is handed the looks' words, what the turn's own
//     steps returned (replyevidence.go), the steps that may have changed the
//     work between them, and the reply, and asked for the statements that
//     evidence contradicts and the claims about how the work looks that
//     nothing it saw shows. If it names any, the model is sent back once with
//     them in hand.
//
// A STALE TURN THAT READ SOMETHING AFTER ITS LOOK IS READ TOO. "fits one
// page" after `pdfinfo` said `Pages: 2` (qloop run 13) is a turn whose newest
// look came before the export, so the stale arm alone sent it to look again,
// the look said one page, and the reply stood. Since run 24 every stale turn is
// read first; what the reader names is the send-back, and when it names
// nothing the stale arm stands.
//
// BOTH ARE ONCE A TURN, TOGETHER ([checkpointMeter.lookedBack]): at most one
// extra pass, at most one reader call, and a reply written after either is the
// reply the turn ends on. Each offers [NoChangeReply], which ends the turn on
// the reply already given and draws nothing ([answeredNoteUnchanged]), so a
// reply that was right costs one short call and is never written twice.
//
// THE READER'S BILL IS CAPPED. The reply is clipped to [replyCheckReplyBytes],
// the looks to [replyCheckLookBytes] and the observations to
// [replyEvidenceBytes], each newest first, so one call reads at most a few
// thousand tokens whatever the turn did; the steps between them are one line
// each under [replyCheckChangeBytes]; it runs on the small-work tier; and it is
// asked only on a turn that looked at its work with a second model's eyes,
// which is where the words to compare against exist at all. With
// no model for the role, the arm is absent ([Agent.readReplyAgainstLooks]),
// and the stale arm — which needs no model — still stands.

// RUN 24 STILL SENT SEVEN REPLIES OF FOURTEEN THAT THE WORK DID NOT BEAR OUT,
// with both arms and the observations in place, and the turns' own transcripts
// say why. Three holes, none of them about any one kind of work:
//
//   - ONLY A CONTRADICTION COULD BE RAISED. "clear of the cup and cake" went
//     out over a headline band that covers the cup; no look had been asked
//     about the cup and no output placed it, so nothing contradicted the
//     sentence and the reader, told to leave out what nothing covered, said
//     NONE. A reply that tells the person the work is FREE OF A PROBLEM —
//     clear of something, no overlap, nothing cut off, no lettering, fits,
//     renders cleanly — when nothing the turn saw looked at that, or when any
//     entry reports the problem, is now raised too, as UNSHOWN, and the model
//     is sent back to check it or to say plainly that it could not. It is that
//     narrow on purpose. Measured on run 24's replies, asking for every claim
//     about how the work looks raised something on 30 of 33 (most of it true
//     but simply not in the evidence), which is #2839's 30 of 47 again; asking
//     for these promises alone raised 11 to 14 of 27, and the lines read as the
//     judges' own findings.
//   - "LATER WINS" THREW EVIDENCE AWAY. "a row of equal width so the row stays
//     full" went out after a look that said the right half of the row was
//     empty, because a patch to one chart's callout came after it and marked
//     it (earlier); "no names or signage" went out after one look had read
//     "Harbour Café" off the shopfront and a later look at a crop said there
//     was no lettering. An earlier entry now holds for anything no later entry
//     covers unless a step after it plainly changed that very thing, and the
//     reader is shown those steps ([changedThisTurn]) so it can tell; two entries
//     about the same unchanged thing that disagree leave the claim UNSHOWN.
//     "renders clean" went out the same way, over three looks — all
//     (earlier) by then — that had described the References printing raw
//     citation markup.
//   - THE STALE ARM COULD SKIP THE READER. A turn that changed the work after
//     its last look, and read nothing after it that shared a word with the
//     reply, was sent to look again with no reader asked; the model answered
//     [NoChangeReply] and "$439k" went out over a takeaway its own look read as
//     "439 ($k)". One stale turn in fourteen took that path in run 24, and a
//     stale turn is now always read first; only when the reader raises nothing
//     is it sent to look again.
//
// THE PRICE IS STILL ONE SMALL-TIER CALL AND ONE SEND-BACK A TURN. Replayed
// on run 24's 27 replies that looked at their work, the reader raised
// something on 11 to 14 of them and 17 to 20 were sent back, against the 17
// the run sent back itself, fifteen of them with the bare look-again; a
// stale turn the reader objects to now carries its lines instead. The stale arm spends the reader it used
// to skip on one turn in fourteen: a few thousand tokens on the cheapest tier.

func init() {
	roles.Register(roles.RoleReplyCheck, roles.TierLow, "whether a reply about the work says what the turn's own looks and steps showed")
}

// replyStaleLookNote is the dim line a person reads when the turn is sent back
// to look again. It names what happened in their words; the reply above it
// stays on screen until the new one arrives.
const replyStaleLookNote = "it changed the work after its last look · asking it to look again before it replies"

// replyCheckNote is the line for the contradicted arm.
const replyCheckNote = "its reply did not match what it saw this turn · asking it to correct the reply"

// replyUnshownNote is the line when the reader raised only what nothing showed:
// the reply is not known to be wrong, it said more than the turn checked.
const replyUnshownNote = "its reply said more than it checked this turn · asking it to check or say so"

// replyStaleLookLead is the stale arm's continuation, one fixed sentence.
const replyStaleLookLead = "[carry on] Your reply describes the work, but you ran more after your last look at it, " +
	"so the reply may describe it as it was before. Look at it again now, then write the whole reply again for the person " +
	"from what that look shows. If nothing your reply describes has changed since that look, reply with exactly " +
	NoChangeReply + " and nothing else."

// replyCheckLeadOpening opens the contradicted arm's continuation; the reader's
// lines follow it under one heading per kind, and [replyCheckLeadClosing] ends
// it. The two halves are what [isReplyCheckLead] recognises, so the varying
// middle never has to be matched.
const replyCheckLeadOpening = "[carry on] Before your reply goes out: what you saw this turn — your own looks at the work and the output of your own steps — does not bear out everything the reply says.\n"

// replyCheckWrongHeading and replyCheckUnshownHeading head the two kinds of line.
const (
	replyCheckWrongHeading   = "What you saw shows otherwise:\n"
	replyCheckUnshownHeading = "Nothing you saw shows this:\n"
)

// replyCheckLeadClosing asks for a check, not a guess. A line under the second
// heading is not known to be wrong, so the model may look and keep it, drop it,
// or tell the person in its own words that it could not check it — which is
// the one honest thing left when the look cannot answer.
const replyCheckLeadClosing = "\nCheck each line: look at the work with a question about exactly that, or read it back. " +
	"Then write the whole reply again for the person, saying only what you saw. " +
	"Anything you still cannot check, leave out, or tell the person plainly that you could not check it. " +
	"If the reply is right as written, reply with exactly " + NoChangeReply + " and nothing else."

// replyCheckLeadHalves is every opening and closing this file has ever written,
// newest first: #2839's and #2836's are still recognised, so a turn a previous
// build sent back is not read as the person's own message after an upgrade.
var replyCheckLeadHalves = [][2]string{
	{replyCheckLeadOpening, replyCheckLeadClosing},
	{"[carry on] Before your reply goes out: what you saw this turn — your own looks at the work and the output of your own steps — shows something different from what the reply says.\n",
		"\nWrite the whole reply again for the person, saying only what that shows; look or read again first if you think it is wrong. " +
			"If the reply is right as written, reply with exactly " + NoChangeReply + " and nothing else."},
	{"[carry on] Before your reply goes out: your own look at the work this turn shows something different from what the reply says.\n",
		"\nWrite the whole reply again for the person, saying only what your look shows; look again first if you think it is wrong. " +
			"If the reply is right as written, reply with exactly " + NoChangeReply + " and nothing else."},
}

// replyFindings is what the reader raised, by kind.
type replyFindings struct {
	// wrong is what the evidence shows otherwise.
	wrong []string
	// unshown is how the work looks, as the reply says it, that nothing the
	// turn saw shows, or that two things it saw disagree about.
	unshown []string
}

func (found replyFindings) any() bool { return len(found.wrong)+len(found.unshown) > 0 }

// replyCheckLead is the contradicted arm's whole continuation.
func replyCheckLead(found replyFindings) string {
	var lead strings.Builder
	lead.WriteString(replyCheckLeadOpening)
	if len(found.wrong) > 0 {
		lead.WriteString(replyCheckWrongHeading + strings.Join(found.wrong, "\n") + "\n")
	}
	if len(found.unshown) > 0 {
		lead.WriteString(replyCheckUnshownHeading + strings.Join(found.unshown, "\n") + "\n")
	}
	return strings.TrimSuffix(lead.String(), "\n") + replyCheckLeadClosing
}

// isReplyCheckLead reports one of this file's continuations, so that every
// reader of the transcript that hides the harness's own notes hides these too
// ([isContinuationContext]) and a [NoChangeReply] after one is honoured.
func isReplyCheckLead(text string) bool {
	if text == replyStaleLookLead {
		return true
	}
	for _, halves := range replyCheckLeadHalves {
		if strings.HasPrefix(text, halves[0]) && strings.HasSuffix(text, halves[1]) {
			return true
		}
	}
	return false
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
	// replyCheckChangeBytes bounds the steps that may have changed the work,
	// all of them together, newest kept first; one is a single line of at most
	// [replyEvidenceCommandBytes].
	replyCheckChangeBytes = 1500
	// replyCheckChangeLeast is the shortest step line worth showing.
	replyCheckChangeLeast = 60
)

// replyCheckAsk is the reader's question, put after the evidence because the
// models on this tier answer whatever they read last.
//
// IT IS ASKED ABOUT ALL OF THE EVIDENCE, AND WHAT A LATER STEP DID NOT TOUCH
// STILL HOLDS. "top recall (97.0%)" went out in run 17 over an earlier look
// that said Milvus is at 98.0%, because the newest look was about table
// widths; "a row of equal width" went out in run 24 over an earlier look that
// said the row was half empty, because "the later one wins" let a patch to a
// different chart retire it. The CHANGE entries are there so the reader can
// see what came between. And a move is not a position: "Sofia Rossi: Rejected
// → Hired" was raised in run 17 because the look showed her in Hired, which is
// what the line says.
//
// TWO KINDS OF LINE, AND THE SECOND IS NARROW ON PURPOSE. WRONG is what the
// evidence shows otherwise, about anything. UNSHOWN is only a promise that the
// work is free of a problem, checked against every entry — the claim a person
// relies on without looking — because raising every uncovered statement named
// something on 30 of 47 turns when #2839 measured it, and on 30 of 33 on run
// 24 when it was every claim about how the work looks.
const replyCheckAsk = "Above is what an assistant saw this turn while working, oldest first. LOOK entries are a second model's description of a picture of its own work; " +
	"OUTPUT entries are what its own steps returned; CHANGE entries are steps that may have changed the work. Then the reply it is about to send to a person.\n" +
	"Read every statement in the reply about the work as it now stands — a number, a count, a page count, an order, a ranking such as top, best or only, " +
	"where something sits, what it overlaps or stays clear of, a size or width, a name or label, a quoted line, what a card or row shows, " +
	"and that something is absent or clean (no overlap, no lettering, nothing cut off, renders clean).\n" +
	"Which evidence counts: an entry marked (earlier) still holds for anything no later entry covers, unless a CHANGE after it plainly altered that very thing. " +
	"Where a later entry covers the same thing after such a change, the later one wins. " +
	"Where two entries about the same thing disagree and nothing between them changed it, neither settles it. " +
	"On how the work looks, a LOOK outranks an OUTPUT that says how something was set up: settings are what was asked for, a LOOK is what rendered. " +
	"A change such as \"A → B\", \"moved from\" or \"up to 4\" states a before and an after; it is contradicted only if the evidence shows a different before or after.\n" +
	"Write a line for each statement that is one of these:\n" +
	"WRONG — the evidence shows something different.\n" +
	"UNSHOWN — it tells the person the work is free of a problem: clear of or not covering something, no overlap, nothing cut off, no lettering, text or marks, fits, renders cleanly — " +
	"and no entry looked at that, or any entry, earlier ones included, reports that problem.\n" +
	"Check every such promise that the work is free of a problem first, against every entry, earlier ones included: one entry that reports the problem is enough to raise it. " +
	"Most of what a reply says is not in these entries, and that is expected: never raise a statement only because the evidence does not mention it or is cut short, unless it is such a promise.\n" +
	"Leave out offers, plans, questions, what the reply itself calls an assumption or a limit, and what a step's own output reports it did without an error.\n" +
	"Start each line with \"- WRONG: \" or \"- UNSHOWN: \", quote the reply's words, then say what the evidence shows instead and which entry, or that nothing looked at it. " +
	"At most five lines, WRONG first. If there is nothing to raise, answer exactly NONE."

// turnLook is one look this turn took at its work: where its call sits in the
// transcript, and the words a second model wrote about the picture, when it
// wrote any.
type turnLook struct {
	at       int
	order    int
	question string
	seen     string
	// before says a step that may change the work ran after this look.
	before bool
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
	order := 0
	for index := from; index < len(messages); index++ {
		message := messages[index]
		if message.Role != "assistant" {
			continue
		}
		for _, call := range message.ToolCalls {
			order++
			name := strings.TrimSpace(call.Function.Name)
			result := results[strings.TrimSpace(call.ID)]
			seen := lookWords(result)
			if name == "view_image" || seen != "" {
				looks = append(looks, turnLook{at: index, order: order, question: checkpointArgumentNamed(call.Function.Arguments, "question"), seen: seen})
				changedSince = false
				continue
			}
			if !approval.ReadOnly(name, json.RawMessage(call.Function.Arguments)) {
				for earlier := range looks {
					looks[earlier].before = true
				}
				if len(looks) > 0 {
					changedSince = true
				}
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
//
// THE READER IS ASKED FIRST IN BOTH ARMS. A stale turn used to go straight to
// "look again" unless an output after its newest look shared words with the
// reply, and a [NoChangeReply] to that ended the turn on a reply nothing had
// read (run 24, sales-dashboard t1). Now the reader reads it against
// everything the turn saw, and the bare stale arm is what is left when the
// reader raises nothing — or when there is no reader at all.
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
	messages := a.snapshot()
	looks, changedSince := lookedAtTheWork(messages)
	if len(looks) == 0 {
		return false
	}
	observed := relevantObservations(said, observedThisTurn(messages))
	found := a.readReplyAgainstLooks(ctx, said, looks, observed, changedThisTurn(messages))
	// THE READER IS SPENT EITHER WAY. A turn whose reply the reader cleared is
	// not read a second time on its next stop, which is what keeps this at one
	// call a turn.
	meter.lookedBack = true
	switch {
	case len(found.wrong) > 0:
		a.sendBack(hub, "reply-contradicts-look", replyCheckNote, replyCheckLead(found))
		return true
	case len(found.unshown) > 0:
		a.sendBack(hub, "reply-unshown", replyUnshownNote, replyCheckLead(found))
		return true
	case changedSince:
		a.sendBack(hub, "reply-stale-look", replyStaleLookNote, replyStaleLookLead)
		return true
	}
	return false
}

// readReplyAgainstLooks asks [roles.RoleReplyCheck] which statements in the
// reply the looks and the turn's observations contradict. Nothing comes back — no model for the role, no
// words to compare, a failed call, an answer not in the asked shape — and the
// reply goes out as written: A CAPABILITY THAT CANNOT WORK IS ABSENT.
func (a *Agent) readReplyAgainstLooks(ctx context.Context, said string, looks []turnLook, observed, changes []turnObservation) replyFindings {
	page := replyCheckPage(said, looks, observed, changes)
	if page == "" {
		return replyFindings{}
	}
	if !a.hasClient() {
		return replyFindings{}
	}
	if _, err := a.roleLadder(roles.RoleReplyCheck, ""); err != nil {
		return replyFindings{}
	}
	ctx, done := context.WithTimeout(ctx, checkpointSketchWindow)
	defer done()
	response, model, err := a.callRole(ctx, roles.RoleReplyCheck, "", []ai.Message{textMessage("user", page+"\n\n"+replyCheckAsk)})
	if err != nil || response == nil {
		return replyFindings{}
	}
	a.addAuxiliaryUsageAs(response, model, 1, string(roles.RoleReplyCheck))
	return replyCheckFindingsOf(response.Text())
}

// replyCheckPage is what the reader is shown: the looks that carry words, newest
// kept first under their bound; the observations [relevantObservations] already
// chose and bounded; the steps that may have changed the work, newest kept
// first under [replyCheckChangeBytes], each said once — a step whose output is
// already an OUTPUT entry is not repeated as a CHANGE; all of them in the order
// the turn met them; and the reply. A turn with no look that carries words and
// no observation — a model that saw the picture itself and read nothing that
// shares a word with the reply — has nothing to compare against, and the page
// is empty: steps alone are not evidence of anything.
func replyCheckPage(said string, looks []turnLook, observed, changes []turnObservation) string {
	type entry struct {
		order int
		text  string
	}
	var kept []entry
	room := replyCheckLookBytes
	for index := len(looks) - 1; index >= 0 && room > 0; index-- {
		look := looks[index]
		if look.seen == "" {
			continue
		}
		var text strings.Builder
		text.WriteString("LOOK")
		if look.before {
			text.WriteString(" (earlier)")
		}
		if look.question != "" {
			text.WriteString(" (asked: " + strconv.Quote(clip(look.question, 300)) + ")")
		}
		text.WriteString(":\n")
		text.WriteString(clip(look.seen, min(replyCheckOneLookBytes, room)))
		room -= text.Len()
		kept = append(kept, entry{look.order, text.String()})
	}
	shown := map[int]bool{}
	for _, observation := range observed {
		text := "OUTPUT"
		if observation.before {
			text += " (earlier)"
		}
		text += " of " + observation.step + ":\n" + observation.result
		kept = append(kept, entry{observation.order, text})
		shown[observation.order] = true
	}
	if len(kept) == 0 {
		return ""
	}
	// A step line cut to a few bytes names nothing, so the oldest are dropped
	// whole once there is no room for a useful one.
	room = replyCheckChangeBytes
	for index := len(changes) - 1; index >= 0 && room >= replyCheckChangeLeast; index-- {
		change := changes[index]
		if shown[change.order] {
			continue
		}
		text := "CHANGE: " + clip(change.step, min(replyEvidenceCommandBytes, room))
		room -= len(text)
		kept = append(kept, entry{change.order, text})
	}
	sort.SliceStable(kept, func(left, right int) bool { return kept[left].order < kept[right].order })
	texts := make([]string, len(kept))
	for index, one := range kept {
		texts[index] = one.text
	}
	return strings.Join(texts, "\n\n") + "\n\nREPLY:\n" + clip(said, replyCheckReplyBytes)
}

// replyCheckFindingsOf reads the reader's answer into the lines it raised, by
// kind. ONLY THE ASKED SHAPE COUNTS: "- " lines, at most [replyCheckFindings].
// NONE, an empty answer and prose that is neither are all "nothing raised",
// because a send-back the reader did not clearly ask for is a turn spent on a
// guess. A "- " line with no kind is read as WRONG, which is the only kind the
// ask before this one had.
func replyCheckFindingsOf(answer string) replyFindings {
	var found replyFindings
	answer = strings.TrimSpace(answer)
	if answer == "" || strings.EqualFold(strings.Trim(answer, ". "), "NONE") {
		return found
	}
	count := 0
	for _, line := range strings.Split(answer, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") || strings.TrimSpace(line[2:]) == "" {
			continue
		}
		body := strings.TrimSpace(line[2:])
		// "- NONE" is the asked answer in the asked shape's dress: measured on
		// the run 16 and 17 transcripts, the reader wrote it once in 47.
		if strings.EqualFold(strings.Trim(body, ". "), "NONE") {
			continue
		}
		kind, rest := findingKind(body)
		if saysNothingRaised(rest) {
			continue
		}
		text := clip("- "+strings.TrimSpace(rest), replyCheckFindingBytes)
		if kind == "UNSHOWN" {
			found.unshown = append(found.unshown, text)
		} else {
			found.wrong = append(found.wrong, text)
		}
		if count++; count == replyCheckFindings {
			break
		}
	}
	return found
}

// findingKind splits a raised line's kind off its words: "WRONG: …",
// "UNSHOWN: …", the same in bold or in brackets, or no kind at all.
func findingKind(body string) (kind, rest string) {
	trimmed := strings.TrimLeft(body, "*[ ")
	for _, name := range []string{"WRONG", "UNSHOWN"} {
		if len(trimmed) >= len(name) && strings.EqualFold(trimmed[:len(name)], name) {
			rest = strings.TrimLeft(trimmed[len(name):], "*]: —-")
			return name, rest
		}
	}
	return "", body
}

// saysNothingRaised reports a raised line that raises nothing: empty, or
// "none of the statements are contradicted" in the asked shape's dress, which
// the reader wrote once in 27 replies on run 24 when it answered each kind
// separately.
func saysNothingRaised(rest string) bool {
	words := strings.Fields(strings.ToLower(strings.Trim(rest, " .\"“”'")))
	if len(words) == 0 {
		return true
	}
	switch strings.Trim(words[0], ".,;:—-") {
	case "none", "nothing", "n/a":
		return true
	}
	return false
}
