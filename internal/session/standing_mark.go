package session

// A DRAFT THE PERSON MARKED STANDING — the deterministic half of the ambient
// side's recognition problem.
//
// tools_standing.go's description is the other half, and it is the half that
// FAILS SILENTLY: it asks the model to notice that an ordinary sentence was a
// standing one, and a model that does not notice does the sentence as one-off
// work. The person watches something happen, believes a rule was made, and
// walks away from a machine that is holding nothing. Nothing on the screen says
// otherwise, because from the outside a recognised sentence and a missed one
// look identical.
//
// So there is a gesture that takes the noticing out of it. The surface's draft
// box has a chord that means "keep this true" (internal/tui3's standmark.go),
// and a sentence sent that way arrives here carrying [standingMarkInstruction]
// in front of it. The model is no longer being asked to spot anything: it is
// being told what the person did, and what it must do about it.
//
// ── THE LAWS THIS FILE APPLIES ──
//
//   - IT IS AN INSTRUCTION AND NEVER AN ARGUMENT. Nothing here calls `stand`,
//     fills a card in or arms an item; the model still compiles the when, the
//     does, the altitude and the title out of the sentence exactly as it does
//     when it recognised one on its own, and the card the person answers is the
//     ordinary ratification card. NOTHING STANDS UNTIL THEY SAY YES is
//     standing_contract.go's law and this door does not touch it.
//
//   - A MARKED SENTENCE IS NEVER DONE AS ORDINARY WORK. That is the whole
//     invariant. "Run the tests whenever I push" marked standing must not be a
//     test run; a sentence that cannot stand at all — "what time is it?" — is
//     answered with one line saying so and nothing else, because doing it
//     anyway is exactly the silent failure the gesture exists to end.
//
//   - THE PERSON NEVER SEES IT. The instruction rides in the message this turn
//     reasons from and nowhere else: [Agent.recordUserLocked] journals their own
//     sentence, so a resume, an export and the transcript on screen all show
//     what they typed.
//
//   - AND IT IS ABSENT WHERE IT CANNOT WORK. With no store behind the ambient
//     side the `stand` verb is not on the belt at all (tools_standing.go), so an
//     instruction to shape a proposal would be an instruction to reach for a
//     verb the model has not got. The door refuses instead, before anything is
//     journaled, on [Agent.SubmitImage]'s terms: nothing was recorded, nothing
//     was queued, and the person still holds their words.

import (
	"context"
	"errors"
	"strings"
)

// standingMarkInstruction is what the model reads in front of the sentence, and
// it is written as a report of something that already happened rather than as a
// suggestion: the person pressed a key whose whole meaning is this, so there is
// nothing here for the model to weigh up.
//
// It names the tool and the op because it is the one message on this surface
// that may: everything else about `stand` is recognition, and the model must
// arrive at the verb itself. Here the verb is settled and what is left is the
// compilation.
const standingMarkInstruction = "The person MARKED the sentence below as something to keep true — " +
	"they pressed the key that means exactly that, so this is not a guess and not a phrasing you have to weigh up.\n" +
	"Call `stand` with op=propose and shape their sentence into the card: their own words verbatim, " +
	"when it wakes, what a firing does, how far it reaches, a short title. " +
	"They answer the card; nothing stands until they do.\n" +
	"Do NOT carry the sentence out as one-off work, and do not do it once as well. " +
	"If it is not something that can stand at all — a question about right now, a one-off command with no condition in it — " +
	"say so in ONE short line, name what it would take to make it stand, and do nothing else.\n\n" +
	"What they marked:\n"

// standingMarkAbsent is the refusal where there is no ambient side. It is in
// the person's grammar because the surface prints it as it arrives.
const standingMarkAbsent = "session: nothing here can hold a standing order"

// SubmitStanding is [Agent.Submit] for a draft the person MARKED STANDING: the
// same turn, the same hub, the same events, the same steering rules — with
// [standingMarkInstruction] in front of the sentence for the model, and the
// sentence alone for the journal.
//
// A caller that already knows how to read a turn's channel needs to learn
// nothing new, which is [Agent.SubmitImage]'s bargain and the reason this is a
// method beside it rather than a flag inside Submit: the two doors differ in
// what the message is, and in nothing else.
func (a *Agent) SubmitStanding(ctx context.Context, text string) (<-chan Event, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("session: empty message")
	}
	// REFUSED BEFORE ANYTHING IS RECORDED, for the reason the header states: an
	// instruction to propose a standing order, sent to a model with no `stand`
	// verb on its belt, is a turn that can only end in an apology.
	if a.standingItems() == nil {
		return nil, errors.New(standingMarkAbsent)
	}
	return a.submitUser(ctx, standingMarked(text))
}

// standingMarked is the message itself: what the model reads, and what the
// person actually said.
func standingMarked(text string) userMessage {
	return userMessage{
		message: textMessage("user", standingMarkInstruction+text),
		said:    text,
	}
}
