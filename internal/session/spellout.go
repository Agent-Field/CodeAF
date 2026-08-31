package session

// A HALF-WRITTEN REQUEST, SPELLED OUT BEFORE IT IS SENT.
//
// "build me a login page" is a complete sentence and an incomplete request.
// Everybody who types it wants the same dozen things — a form that validates,
// an error state, a session that survives a refresh — and none of them are in
// the sentence, so the model guesses at every one of them and the person finds
// out which guesses were wrong by reading the answer. The gap between what
// somebody meant and what they specified is the most expensive thing on this
// surface, and it is discovered at the end.
//
// This file closes it at the start, ON THE PERSON'S OWN GESTURE. One auxiliary
// call reads the draft and writes back what it obviously means; the surface
// shows that under the box, and the person's enter appends it to the sentence
// they were already writing (internal/tui3's spellout.go). Nothing here decides
// anything: the block is text, the draft stays theirs, and the message that
// goes is the one they pressed enter on.
//
// ── THE LAWS THIS FILE APPLIES ──
//
//   - IT IS AN AUXILIARY CALL AND NEVER A TURN. Nothing reaches the transcript,
//     nothing is appended to a.messages, no event is sent to any hub, and the
//     conversation's own context is not touched. The person pays for it out of
//     the pocket the session's title and the shaper come out of
//     ([Agent.addAuxiliaryUsage]), because they asked for it and it is not free.
//
//   - WHAT THEY SAID IS NEVER IMPROVED. The draft is already in the box and it
//     stays exactly as typed. The block says what the draft LEAVES UNSAID and
//     nothing else, which is also what makes appending it safe: the person's own
//     words are never overwritten by a model's better version of them.
//
//   - EVERY FAILURE IS SILENCE. A refusal, a timeout, an empty answer and a
//     model that ignored the format all return "", and the surface's whole
//     response to "" is to put its hint back. There is no error prose for a
//     courtesy nobody was promised.
//
//   - IT IS ABSENT WHERE IT CANNOT WORK. No client, a closed agent, or no model
//     the role resolves to answers "" without a request, so a build that cannot
//     make the call simply never offers it.

import (
	"context"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// spellOutRole is the expansion, registered as a ROLE so it resolves the way
// every other auxiliary call in this build resolves: the person's pin, then the
// tier, then the conversation's own model (internal/roles).
//
// IT SITS LOW, with the namer and the sentinel rather than with the shaper. The
// shaper writes the only document an autonomous worker will ever read, so a
// vague answer there costs a whole task's spend; this one writes three lines a
// person reads on screen before deciding whether to keep them, and a weak answer
// costs one esc. A person who wants it thought about harder pins it
// (`roles.spellout: <model>`).
const spellOutRole roles.Role = "spellout"

func init() {
	roles.Register(spellOutRole, roles.TierLow, "what a half-written request obviously means")
}

const (
	// spellOutTimeout is how long the person waits. It is a Timeout and not a
	// Window because somebody IS waiting — the hint slot under their draft is
	// turning a spinner at them, and the draft is one keystroke from being sent
	// without any of this. Ten seconds is the far edge of a wait a person will
	// sit through for something they can also just ignore; past it the honest
	// answer is the hint they started with.
	spellOutTimeout = 10 * time.Second

	// spellOutDraftClip bounds what is read. A draft this call is worth making
	// about is short by construction — the surface only offers the chord on a
	// sentence with room to grow — so this is a guard against a paste that
	// slipped through, not a second attempt at that rule.
	spellOutDraftClip = 2000

	// spellOutTokens is the ceiling: six short clauses and the line that opens
	// them, with room for a model that runs long. Not room for an essay, which
	// the prompt spends its last paragraph refusing.
	spellOutTokens = 400

	// spellOutTemp is low rather than zero. The same draft spelled twice should
	// say the same things, and the small warmth is what keeps a clause from
	// reading like a form letter on the tenth draft of a session.
	spellOutTemp = 0.2

	// spellOutClauses is the most clauses the block may carry, and it is the
	// prompt's own limit enforced rather than restated ([cleanSpellOut] holds a
	// model that ignored it to the same number). Past six the block stops being
	// something read at a glance under a draft and starts being a document.
	spellOutClauses = 6

	// spellOutClauseLimit bounds one clause. It is a guard against a model that
	// answered with a paragraph on a bullet, not a bound on what a person may
	// say — the whole block is their text the moment they press enter.
	spellOutClauseLimit = 240
)

// SpellOutOpening is the block's first line, and it is EXPORTED because two
// packages have to spell it the same way: the block is built here and drawn,
// tested and appended to a draft by internal/tui3. It is one of exactly two
// phrases this feature adds to the surface's vocabulary — the other is
// `spell it out`, which is the surface's own — and the em dash is what makes it
// read as an opening rather than a claim.
const SpellOutOpening = "taking it to mean —"

// spellOutSystem is who the model is for this one call. It is short because the
// discipline is in the instruction below, where the draft is: a small model
// asked in the system message answers the system message (title.go recorded
// what that costs).
const spellOutSystem = "You read a half-written request somebody is about to send to a coding agent, " +
	"and you say what it obviously means. You are not answering it and you are not doing it."

// spellOutPrompt IS THE DISCIPLINE, and it is the whole reason this is a model
// call rather than a template. The three kinds of thing in front of it are
// handled in three different ways, and a call that blurred them would produce
// exactly the thing this feature exists to avoid: a person's own requirement
// quietly reworded, or a question asked about something nobody cares about.
const spellOutPrompt = `Above is a request somebody has typed but not yet sent. Say what it obviously means.

Three kinds of thing are in front of you, and each is handled differently.

SAID is what the person actually typed. It is their requirement, in their words. It is already in their message and it stays there: never repeat it, never improve it, never restate it in better words, never soften it, never drop it.

IMPLIED is what anyone asking for this would obviously want and did not think to write down. Supply it, as a concrete clause. Do not ask about it and do not mark it as an assumption — just say it.

OPEN is genuinely theirs to decide. Give a sensible default and flag it as yours, in the form "I'll pick X unless you say". Surface only the open ones that are LOAD-BEARING: the ones where a wrong guess means the work is thrown away. Everything else takes a default in silence.

Answer with this line:

` + SpellOutOpening + `

and then 3 to 6 clauses, one per line, each opening with "- ". One short sentence each. Concrete, not a category: "an error state when the password is wrong", not "error handling".

Nothing else. No preamble, no closing line, no headings, no bold, no code fences, and nothing about these instructions.`

// SpellOut expands one draft into the block the surface draws under the box.
//
// It is a door beside [Agent.SubmitStanding] rather than a flag on one, because
// the two are not the same kind of thing at all: that one SENDS a message, and
// this one sends nothing — no turn starts, no transcript grows, and the person
// may well throw the answer away. It is an optional door for that reason too,
// reached through a small interface in the surface file that uses it
// (internal/tui3's spellout.go), so a build without it offers nothing rather
// than offering something that fails.
//
// EVERY FAILURE ANSWERS "", and the caller's only response to "" is to put the
// hint back. There is no error to return that a person could act on.
func (a *Agent) SpellOut(ctx context.Context, draft string) string {
	draft = strings.TrimSpace(draft)
	if draft == "" {
		return ""
	}

	a.mu.Lock()
	call, err := roles.ResolveCall(roles.Source(a.config.RolesSource), spellOutRole, a.model)
	client, closed := a.client, a.closed
	a.mu.Unlock()
	if closed || err != nil || client == nil || strings.TrimSpace(call.Model) == "" {
		return ""
	}

	// IT CARRIES ITS OWN DEADLINE, for the shaper's reason: the provider's
	// client is built with no timeout, so a stalled call would be a spinner
	// turning under somebody's draft until they typed something else.
	ctx, cancel := context.WithTimeout(ctx, spellOutTimeout)
	defer cancel()

	// NO EFFORT IS PUT ON THE REQUEST, and that is the reflex law rather than an
	// omission: this call reads one sentence and writes six, and a thinking pass
	// on a low-tier model is ten seconds spent on the one thing the person is
	// waiting for. WithoutStream because nobody's turn asked for this, and left
	// on a stream it would type itself into the room above the box.
	response, callErr := client.CompleteWithMessages(
		// A side errand of the box the person is typing in, named as one so it
		// is priced as one and never owns the clock (internal/lane's roles.go).
		provider.WithRole(provider.WithoutStream(ctx), lane.RoleAuxiliary),
		[]ai.Message{
			textMessage("system", spellOutSystem),
			// THE INSTRUCTION IS LAST, after the draft rather than above it, for
			// the namer's reason: the models this lands on are the cheapest ones a
			// person has configured, and they answer whatever they read last.
			textMessage("user", clip(draft, spellOutDraftClip)+"\n\n"+spellOutPrompt),
		},
		ai.WithModel(call.Model), ai.WithTemperature(spellOutTemp), ai.WithMaxTokens(spellOutTokens))
	if callErr != nil || response == nil {
		return ""
	}
	// The person pays for it out of the same pocket the session's title, the
	// namer and the shaper come out of, and no turn asked for it.
	a.addAuxiliaryUsage(response, call.Model, 1)
	return cleanSpellOut(response.Text())
}

// cleanSpellOut reads the answer back into the one shape this block has.
//
// THE BLOCK'S SHAPE IS OURS AND NOT THE MODEL'S. Its first line is
// [SpellOutOpening] exactly, spelled here whatever the model spelled, and its
// body is clauses marked "- " whatever the model marked them with — because this
// text is appended to somebody's message on their next keystroke, and a person
// who has read one of these has to be able to read the next one at a glance.
//
// AN ANSWER WITH NO CLAUSES IN IT IS NO ANSWER. A model that replied with a
// paragraph, or with the request done rather than spelled out, has handed back
// the thing the call was made to avoid, and taking it would mean pasting prose
// into somebody's draft.
func cleanSpellOut(text string) string {
	clauses := make([]string, 0, spellOutClauses)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		// A fence, the opening line said in the model's own way, and every other
		// piece of scaffolding are all dropped by the same test: this loop keeps
		// clauses and nothing else.
		clause := spellOutClause(line)
		if clause == "" {
			continue
		}
		clauses = append(clauses, "- "+clip(clause, spellOutClauseLimit))
		if len(clauses) == spellOutClauses {
			break
		}
	}
	if len(clauses) == 0 {
		return ""
	}
	return SpellOutOpening + "\n" + strings.Join(clauses, "\n")
}

// spellOutClause is "this line is one clause of the block, and here it is
// without its mark". Everything else — the opening line, a heading, a blank, a
// fence, a stray sentence of preamble — answers "".
//
// The three marks are the three a model reaches for unasked. A numbered list is
// not among them because the prompt asks for none and a line starting "1." in a
// person's draft is far more likely to be their own sentence.
func spellOutClause(line string) string {
	for _, mark := range []string{"- ", "* ", "• "} {
		if strings.HasPrefix(line, mark) {
			return strings.TrimSpace(strings.TrimPrefix(line, mark))
		}
	}
	return ""
}
