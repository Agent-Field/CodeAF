package tui3

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE DECISION, PUT WHERE THE PERSON IS STANDING.
//
// A task that finishes with nobody able to say whether it holds lands as the
// person's call. It is neither done nor incomplete, its branch is kept, and
// everything queued behind it waits — and until this file the ONLY way to move
// it was to talk the model into calling its `tasks` tool. The card said one
// thing, the roster said another, and neither of them had a door. What people
// did instead was type "accept it" at the conversation and hope the model spent
// the right verb on the right id.
//
// So the card asks, and the card is answered:
//
//	? ◆ Port the parser · your call · 6m40s · 2 files · branch kept · task/parser
//	  nobody could check it
//	  [a] accept · [n] not right · [s] tell it · [d] let aforge decide this one
//
// ── ONE QUESTION, SIX SHAPES, ALWAYS THE SAME THREE COLUMNS ──
//
// There are six things a task can be asking ([session.TaskAskKind]) and the
// person learns ONE row for all of them: yes, no, tell it. What changes between
// a conflict and a check nobody could finish is the WORDS on the first two
// chips, and those words are the engine's — [session.TaskAsk] carries the reason
// sentence and both verbs, spelled once in internal/session, so a chip, a rail
// row and the note the model reads are three drawings of one sentence
// (docs/design/task-states/DESIGN.md).
//
// ── THE WORDS ARE THE PERSON'S AND THE VERBS ARE THE ENGINE'S ──
//
// session's three resolutions are `accept`, `reaudit` and `refute`
// (task_contract.go's [session.TaskResolutions]) and not one of those three
// words is on this row. `reaudit` names the apparatus, and `refute` is a
// courtroom — the vocabulary law this surface holds every task word to. What a
// person is choosing between is: take it, or say it is not finished. That is
// what the row says, and the mapping to the engine's verbs happens once, here.
//
// `check again` LEFT THE ROW ENTIRELY. The engine retries a check that never
// answered by itself, through the provider failover ladder, before anything
// reaches this card — so a person offered `look again` was being asked to spend
// a round the machine had already spent. The verb still exists for the model.
//
// ── `[d]` IS ONE CARD AND NOT A PREFERENCE ──
//
// `decide these for me` used to sit on this row, and it did two things at once:
// it handed THIS card over AND flipped `task.settle` to `auto` for every landing
// after it. That is a persistent preference disguised as an answer, and it is
// why the owner found a card with no choices and no explanation. What is here
// now is `[d] let aforge decide this one`, drawn dimmer than the three answers,
// which hands over this card and CHANGES NO SETTING. The standing row lives in
// `/settings` under Session and nowhere else.
//
// ── UNDER `auto` THE CARD SAYS WHO IS DECIDING ──
//
// When the model is holding the question the card draws no chips, and the reason
// row says so and offers the way back: `nobody could check it · aforge is
// deciding · [t] take it back`. Pressing `t` draws the chips and resolves
// nothing. AND A TASK NEVER STAYS UNOWNED PAST THE END OF A TURN — when the
// model's turn ends with the question unanswered the engine hands it back, the
// card picks up its chips, and if the model's last message asked about that task
// then those chips are the answer surface for it: model text above, chips below,
// one ask (taskdone.go's [app.handedBackCard]).
//
// ── KEYS AND POINTER ANSWER THE SAME ROW ──
//
// The keys are bare letters, taken ONLY on a SELECTED card over an EMPTY box,
// which is the rule every letter on this surface is held to (stop.go's `x`,
// task.go's [app.taskKey]). The pointer answers the same columns, and each key
// owns its own word and the separator after it, which is the offer row's rule
// (harness.go's recordHarnessTaps): `[a]` is three cells and `[a] accept` is
// ten, and that is the difference between a target somebody hits and one they
// aim at.
//
// ── DECIDED MEANS THE CONTROLS ARE GONE ──
//
// An answered card does not grey its choices out, it stops having them: the two
// rows are replaced by one dim line saying what was decided.
//
// WHAT IS NOT REWRITTEN IS THE LANDING ABOVE THEM. The head is a record of how
// this work came home — the mark, the span, the files, the branch that was kept
// — and every one of those facts is still exactly as true as it was. Rewriting
// the head to "done" would be this surface reporting a merge it has not been
// told happened, on a card whose "branch kept" would then read as a stop that
// never occurred. The engine re-settles the node on the answer and publishes it,
// and THAT lands as a second card saying what became of the work. So the
// transcript reads as what it is: this landed as your call → you took it as
// done → task 7 done · merged.

// settleAnswer is one of the five things a landing card offers. It is this
// surface's own enumeration and not [session.TaskResolution], because only two
// of the five are resolutions at all: two of them move a decision from one pair
// of hands to another and the third opens a box.
type settleAnswer uint8

const (
	// settleYes and settleNo are the ask's own two answers, whatever it is
	// asking. What they SAY comes from [session.TaskAsk]; what they DO is decided
	// once, by kind, in [app.settleCard].
	settleYes settleAnswer = iota
	settleNo
	// settleTell is the third column on every card: say something to this task
	// rather than answering the question. IT NEVER RESOLVES ANYTHING — "looks
	// good" typed on a card must not silently become an accept.
	settleTell
	// settleHand is `[d]`, this card only, no setting written.
	settleHand
	// settleBack is `[t]`, offered only while the model is holding the question.
	settleBack
)

// The row's words, spelled once. THE TWO ANSWERS ARE NOT HERE: they are
// [session.TaskAsk]'s Yes and No, which is what lets `drop it` and `not right`
// be the right words on their own cards without either becoming a fourth thing
// the engine has to know about.
const (
	// The keys, and the words for the three columns whose wording never changes.
	settleYesKey   = "[a]"
	settleNoKey    = "[n]"
	settleTellKey  = "[s]"
	settleTellWord = " tell it"
	settleHandKey  = "[d]"
	settleHandWord = " let aforge decide this one"
	settleBackKey  = "[t]"
	settleBackWord = " take it back"
	settleGap      = " · "
	// settleAutoWord is what stands between the reason and the way back while the
	// model is holding this question. It is a WHOLE CLAUSE and not a word, because
	// a row that read `nobody could check it · auto` would have told a person the
	// name of a setting instead of who is deciding.
	settleAutoWord = " · aforge is deciding · "
	// settleHidLead opens the count of what the row could not fit, in this
	// surface's own grammar for a fold that hid something (`▸ +1`, `holds 3 more`,
	// `▸ N earlier`).
	//
	// A NARROW ROW MAY DROP AN ANSWER; IT MAY NOT DROP IT SILENTLY. At sixty
	// columns the row ended after the second answer with nothing to say that the
	// rest were still keys that worked, so the narrowest terminal was the one
	// where a person parked on a decision was told least about how to answer it —
	// and told nothing about being told less. It is dim and answers to no press:
	// it is a count and not another chip.
	settleHidLead = " · +"
)

// The receipts. Each says what the person did, in their own voice, because the
// row it replaces was a question they answered and not a state the machine
// reached.
const (
	settleTookLine     = "you took this as done"
	settleNotRightLine = "you said it is not finished"
	// settleResolveLine is the conflict's own yes: nothing has been taken as done
	// and nothing has been refused — one more merge round is being spent, and what
	// comes of it lands as its own card.
	settleResolveLine = "sent to be resolved"
	settleHandedLine  = "handed to aforge for this one"
	// settleGoneLine is the quiet refresh. A card can be answered from two places
	// at once — this row, and the model's own `tasks … resolve` — and whichever
	// arrives second finds the question already gone. That is not an error worth a
	// card of its own: it is the row catching up with a decision that was made.
	settleGoneLine = "already answered"
	// settleTroubleLine is the OTHER refusal, and it is a different fact: the
	// question is still standing and this particular answer could not be spent —
	// the working copy is gone, the merge round would not run. So the choices
	// STAY, because the others may still work, and the row says so in one line
	// instead of putting the engine's sentence — which names the apparatus — in
	// front of somebody.
	settleTroubleLine = "that one could not be taken — try another"
)

// settleAgent is the door onto deciding a landing nobody could check
// (internal/session's task_audit.go). It is asserted rather than added to
// [Agent] for the reason [taskAgent] is: a surface driven by a scripted agent
// that has never heard of a task must stay representable, and a card that
// offered choices it could not spend would be a button that only fails.
type settleAgent interface {
	// ResolveUnverified settles one node on the person's word — accept, reaudit
	// or refute — and answers with the refusal when somebody got there first.
	ResolveUnverified(id uint64, resolution session.TaskResolution, why string) error
	// HandUnverifiedToModel gives one node's decision to the model instead of
	// taking it: the node does not move, and the model is asked to read the work
	// and settle it.
	HandUnverifiedToModel(id uint64) error
	// TakeBackDecision is that in reverse, and it resolves nothing either: what
	// changes is who is holding the question.
	TakeBackDecision(id uint64) error
}

// conflictAgent is the merge round, ASSERTED SEPARATELY from the rest.
//
// IT IS ITS OWN INTERFACE BECAUSE IT IS ITS OWN CAPABILITY. Resolving a conflict
// spends a worker, a worktree and a merge round; an engine can perfectly well
// take an accept and have no such door. A capability that cannot work is absent,
// not broken — so where this assertion fails the conflict card simply has no
// `[a]`, and `[n]`, `[s]` and `[d]` stand on their own rather than one of them
// failing every time it is pressed.
type conflictAgent interface {
	ResolveConflict(id uint64) error
}

// settleDoors is the deciding half of the agent under this surface, when it has
// one.
func (a *app) settleDoors() (settleAgent, bool) {
	doors, ok := a.agent.(settleAgent)
	return doors, ok
}

// conflictDoors is the merge round under this surface, when it has one.
func (a *app) conflictDoors() (conflictAgent, bool) {
	doors, ok := a.agent.(conflictAgent)
	return doors, ok
}

// ── the row, drawn ──────────────────────────────────────────────────────────

// settleAsking reports whether this card is still a question the PERSON is
// holding: it landed as their call, nobody has answered it here, the model is
// not deciding it, and there is at least one answer this surface could spend.
//
// THE ABSENCE LAW IS THE LAST CLAUSE. A capability with nothing behind it is
// left off entirely rather than drawn and broken, so a surface whose agent has
// no resolver draws no chips at all and the card reads exactly as it did before
// this file existed.
func (a *app) settleAsking(card *taskDone) bool {
	if !card.stillAsking() || card.status.Ask.Owner != session.TaskAskOwnerPerson {
		return false
	}
	return len(a.settleChoices(card)) > 0
}

// settleAuto reports that the MODEL is holding this card's question, which is
// the one shape that draws a way back instead of chips.
func (a *app) settleAuto(card *taskDone) bool {
	if !card.stillAsking() || card.status.Ask.Owner != session.TaskAskOwnerModel {
		return false
	}
	_, ok := a.settleDoors()
	return ok
}

// stillAsking is the card's own half of both questions: a landing that is the
// person's call and has not been answered from here.
func (card *taskDone) stillAsking() bool {
	return card != nil && card.status.Tier == session.TaskTierYourCall && card.decided == ""
}

// settleChoice is one column of the answers row: which answer it stands for, its
// key, and the word beside it.
type settleChoice struct {
	answer settleAnswer
	key    string
	word   string
}

// settleChoices is the columns THIS card can actually offer, in the one order
// every card draws them: yes, no, tell it, and the dimmer hand-over.
//
// EVERY COLUMN IS PRESENT ONLY IF ITS DOOR IS. `[a]` on a conflict spends the
// merge round, which is a door an engine may not have; `[s]` needs a page to put
// the words on. Each is dropped on its own rather than taking the row down with
// it, which is what keeps a conflict answerable with `drop it` on an engine that
// cannot merge for you.
func (a *app) settleChoices(card *taskDone) []settleChoice {
	ask := card.status.Ask
	var out []settleChoice
	if yes := strings.TrimSpace(ask.Yes); yes != "" && a.canSettleYes(card) {
		out = append(out, settleChoice{answer: settleYes, key: settleYesKey, word: " " + yes})
	}
	if no := strings.TrimSpace(ask.No); no != "" && a.canSettleNo(card) {
		out = append(out, settleChoice{answer: settleNo, key: settleNoKey, word: " " + no})
	}
	// AND THE OTHER TWO COLUMNS ARE ONLY WORTH DRAWING BESIDE AN ANSWER. `tell it`
	// and the hand-over both MOVE the question, and a row offering only ways to
	// move a question with no way to answer it is a card that has stopped being a
	// question — which is a state this surface should not be in and should not
	// draw its way out of.
	if len(out) == 0 {
		return nil
	}
	if a.canSettleTell(card) {
		out = append(out, settleChoice{answer: settleTell, key: settleTellKey, word: settleTellWord})
	}
	if _, ok := a.settleDoors(); ok {
		out = append(out, settleChoice{answer: settleHand, key: settleHandKey, word: settleHandWord})
	}
	return out
}

// canSettleYes reports whether the ask's YES has a door behind it.
//
// THREE OF THE SIX ASKS ARE NOT THIS CARD'S TO ANSWER AND NEVER REACH IT. A
// landing card is written when a node comes home (taskdone.go's [app.landedCard]
// is reached from `done`, `failed` and `unverified` only), so `start`, `approve`
// and `cap` — a proposal nobody has agreed to, a design waiting to be approved,
// a run standing at its fuel gate — are all questions about work that has not
// landed, and each already has its own card elsewhere on this surface: the
// proposal (task.go), the design (harnesscard.go and roomapproval.go) and the
// gate. This card does not rebuild any of them; it says nothing about them.
func (a *app) canSettleYes(card *taskDone) bool {
	switch card.status.Ask.Kind {
	case session.TaskAskCheck, session.TaskAskHeld:
		_, ok := a.settleDoors()
		return ok
	case session.TaskAskConflict:
		_, ok := a.conflictDoors()
		return ok
	}
	return false
}

// canSettleNo reports whether the ask's NO has a door: for all three landings it
// is the same one, because saying a landing is not finished is one act however
// it came to be asked.
func (a *app) canSettleNo(card *taskDone) bool {
	switch card.status.Ask.Kind {
	case session.TaskAskCheck, session.TaskAskHeld, session.TaskAskConflict:
		_, ok := a.settleDoors()
		return ok
	}
	return false
}

// canSettleTell reports whether there is anywhere to say something to this task.
//
// THE PLACE IS THE NODE'S OWN PAGE and this surface has exactly one of those. A
// window whose agent has no rooms has nowhere to point the box, so the column is
// absent rather than a key that opens a refusal.
func (a *app) canSettleTell(card *taskDone) bool {
	if card == nil || card.id == 0 {
		return false
	}
	_, ok := a.roomDoors()
	return ok
}

// settleRows appends one card's reason row and answers row to out, and records
// the columns a press is resolved against.
//
// THE REASON IS DRAWN HERE AND NOT WITH THE REST OF THE CARD (taskdone.go's
// [app.doneUnder] hands it over), because on a your-call card the reason and the
// answers are one object: the reason is the question, the chips are its answers,
// and the row that says who is deciding carries a chip of its own. One layout
// writes all three so they cannot fall out of step at a width where one was cut.
//
// The spans are written HERE, by the layout that drew them, and read by nothing
// else: a hit-test that measured the row itself would be measuring a row this
// frame may not have drawn (the same order [app.roomApprovalRows] keeps).
func (a *app) settleRows(out []row, card *taskDone, entry, width, indent int) []row {
	if card == nil {
		return out
	}
	card.chips = nil
	// AND THE GUTTER IS UNPAID AGAIN, for [app.taskCardRows]'s reason: whatever
	// this call writes below is written against the row's own columns from zero,
	// and the transcript's pass moves it from there exactly once (gutter.go).
	card.gut = 0
	pad := strings.Repeat(" ", indent)
	room := width - 2 - indent
	if card.decided != "" {
		return append(out, row{
			text:  a.pal.dim(pad + "  " + fit(card.decided, room)),
			entry: entry, hit: hitDone,
		})
	}
	if a.settleAuto(card) {
		return a.settleAutoRow(out, card, entry, indent, room)
	}
	if !card.stillAsking() {
		return out
	}
	// THE REASON IS THE ONE ACCENT ON THIS CARD besides its glyph: it is the half
	// a person acts on, and the head one row above has already said everything
	// else in dim.
	//
	// IT IS DRAWN WHETHER OR NOT ANYTHING CAN BE ANSWERED. The absence law is
	// about CAPABILITIES — a chip with no door behind it is left off — and a
	// reason is not one: `your call` with nothing under it is the card with no
	// choices and no explanation, which is the defect this whole wave exists to
	// close. A window that cannot spend an answer can still say what is being
	// asked, and it must.
	out = append(out, row{
		text:  a.pal.ask(pad + "  " + fit(strings.TrimSpace(card.status.Ask.Reason), room)),
		entry: entry, hit: hitDone,
	})
	choices := a.settleChoices(card)
	parts, hid := settleParts(choices, room)
	// Narrower than the answers themselves. The row is cut rather than re-spelled
	// and it records NO targets, which is [app.roomApprovalRows]'s own rule: a
	// target under an ellipsis is a press that answers something nobody can read.
	if len(parts) == 0 {
		return out
	}
	card.chips = settleSpans(parts, indent+2)
	// THE HOVER BRIGHTENS THE CHIP AND NOT THE ROW, which is the jump chip's own
	// answer to the same shape (jumpchip.go): four targets share this line, and a
	// background band across all of it would say "you can press here" about three
	// answers the pointer is not on.
	hot := settleAnswer(255)
	if at := a.hoveringSettle(entry); at >= 0 && at < len(card.chips) {
		hot = card.chips[at].answer
	}
	line := pad + "  "
	for _, part := range parts {
		switch {
		case part.answer == settleHand && part.answer != hot:
			// The hand-over is drawn quieter than the answers beside it, because it
			// is not one of them: it moves the question rather than answering it.
			line += a.pal.dim(part.key + part.word)
		case part.answer == hot:
			line += a.pal.bold(a.pal.accent(part.key)) + a.pal.accent(part.word)
		default:
			line += a.pal.askBold(part.key) + a.pal.ask(part.word)
		}
		line += a.pal.dim(settleGap)
	}
	line = strings.TrimSuffix(line, a.pal.dim(settleGap))
	if hid > 0 {
		line += a.pal.dim(settleHidLead + itoa(hid))
	}
	out = append(out, row{text: line, entry: entry, hit: hitSettle})
	if card.trouble != "" {
		out = append(out, row{
			text:  a.pal.dim(pad + "  " + fit(card.trouble, room)),
			entry: entry, hit: hitDone,
		})
	}
	return out
}

// settleAutoRow is the whole of a card the model is deciding: one row, saying
// what the question is, who is holding it, and the way to take it back.
//
// THERE ARE NO CHIPS AND THERE IS NO SECOND PROMPT. A card with no choices and
// no explanation is the defect this clause exists to close — and a card that
// asked twice, once here and once in the model's own message above it, would be
// the same defect the other way round.
func (a *app) settleAutoRow(out []row, card *taskDone, entry, indent, room int) []row {
	pad := strings.Repeat(" ", indent)
	said := strings.TrimSpace(card.status.Ask.Reason) + settleAutoWord
	// The way back is what survives a narrow row: the reason is on the rail and in
	// the record, and the key is only here.
	if ansi.StringWidth(said)+ansi.StringWidth(settleBackKey+settleBackWord) > room {
		said = ""
	}
	at := indent + 2 + ansi.StringWidth(said)
	card.chips = []settleChip{{
		span:   hudSpan{from: at, to: at + ansi.StringWidth(settleBackKey+settleBackWord)},
		answer: settleBack,
	}}
	key, word := a.pal.askBold(settleBackKey), a.pal.ask(settleBackWord)
	if a.hoveringSettle(entry) == 0 {
		key, word = a.pal.bold(a.pal.accent(settleBackKey)), a.pal.accent(settleBackWord)
	}
	return append(out, row{
		text:  a.pal.dim(pad+"  "+said) + key + word,
		entry: entry, hit: hitSettle,
	})
}

// settleParts is the answers row in pieces, and the count of what would not fit.
//
// THE HAND-OVER IS THE FIRST THING CUT, then `tell it`. They are the two columns
// that move the question rather than answering it, and a row that fits by losing
// an answer is a question with no visible way to answer it (harness.go's own law
// about which half of an offer row survives). A row too narrow for even the
// first answer draws nothing at all.
func settleParts(choices []settleChoice, width int) ([]settleChoice, int) {
	for keep := len(choices); keep > 0; keep-- {
		fits := choices[:keep]
		hid := len(choices) - keep
		if settlePartsWidth(fits, hid) <= width {
			return fits, hid
		}
	}
	return nil, 0
}

// settlePartsWidth is what one row of columns costs, separators and the count of
// what was dropped included.
func settlePartsWidth(fits []settleChoice, hid int) int {
	width := 0
	for i, part := range fits {
		if i > 0 {
			width += ansi.StringWidth(settleGap)
		}
		width += ansi.StringWidth(part.key + part.word)
	}
	if hid > 0 {
		width += ansi.StringWidth(settleHidLead + itoa(hid))
	}
	return width
}

// settleSpans is the pressable columns of the drawn row: each key AND the word
// beside it, never the separator between them — a press in the gap must not
// resolve as either answer.
func settleSpans(parts []settleChoice, from int) []settleChip {
	chips := make([]settleChip, 0, len(parts))
	at := from
	for i, part := range parts {
		if i > 0 {
			at += ansi.StringWidth(settleGap)
		}
		width := ansi.StringWidth(part.key + part.word)
		chips = append(chips, settleChip{span: hudSpan{from: at, to: at + width}, answer: part.answer})
		at += width
	}
	return chips
}

// settleChip is one pressable answer on the row.
type settleChip struct {
	span   hudSpan
	answer settleAnswer
}

// ── the pointer ─────────────────────────────────────────────────────────────

// settlePress resolves a click on an answers row and reports whether it took it.
//
// IT SWALLOWS EVERY PRESS ON ITS OWN ROW, hit or miss, for the reason the
// approval row does (roomapproval.go): this is a thing the surface is waiting on
// the person for, and a press that missed a chip and fell through would expand
// the card under somebody who was reaching for an answer.
func (a *app) settlePress(entry, x int) bool {
	card := a.settleCardOf(entry)
	if card == nil {
		return false
	}
	for _, chip := range card.chips {
		if chip.span.holds(x) {
			a.settleCard(card, chip.answer)
			return true
		}
	}
	return true
}

// hoveringSettle is which chip of this card's answers row the pointer is on, and
// -1 for none.
func (a *app) hoveringSettle(entry int) int {
	if a.hot.kind != hoverSettle || a.hot.entry != entry {
		return -1
	}
	return a.hot.index
}

// ── the keyboard ────────────────────────────────────────────────────────────

// settleCardKey routes the letters on a SELECTED card, and reports whether it
// took one.
//
// EVERY GUARD HERE IS THE SAME GUARD `x` HAS (stop.go). The card must be the
// selected one, the message box must be empty, and no overlay may be up —
// because these are letters, and a letter that answers a question while somebody
// is typing a sentence is a surface that decided their work was finished on the
// strength of the first word of a paragraph.
func (a *app) settleCardKey(msg tea.KeyPressMsg) bool {
	if a.at(pageSettings) || a.at(pageHome) || a.pick.open || a.copy.on || a.rew.on ||
		a.roomOpen() || a.chordsStandDown() {
		return false
	}
	// The selection indexes WHICHEVER LIST THE BODY IS DRAWING, and a room's page
	// is not the conversation (render.go's [app.bodyDeck]) — which is why a room
	// is refused above rather than looked up here.
	return a.settleKeyOn(a.doneCardAt(a.sel), msg)
}

// settleKeyOn spends one letter on one card, and reports whether the card was in
// a state to take it. It is the seam all three doors go through — the selected
// card, the open room and the roster's focused row — so the three cannot answer
// to different keys.
func (a *app) settleKeyOn(card *taskDone, msg tea.KeyPressMsg) bool {
	answer, ok := settleAnswerFor(msg.String())
	if !ok {
		return false
	}
	switch {
	case answer == settleBack:
		if !a.settleAuto(card) {
			return false
		}
	case !a.settleAsking(card) || !a.settleOffers(card, answer):
		return false
	}
	a.settleCard(card, answer)
	return true
}

// settleOffers reports whether one card is actually drawing one answer. A KEY
// THAT IS NOT ON THE CARD DOES NOTHING: `a` on a conflict this engine cannot
// merge for you is not a chip, so it may not be a keystroke either.
func (a *app) settleOffers(card *taskDone, answer settleAnswer) bool {
	for _, choice := range a.settleChoices(card) {
		if choice.answer == answer {
			return true
		}
	}
	return false
}

// settleAnswerFor is the five letters, spelled once, so the conversation's
// selected card, a node's room and the roster's focused row cannot answer to
// different keys.
func settleAnswerFor(key string) (settleAnswer, bool) {
	switch key {
	case "a":
		return settleYes, true
	case "n":
		return settleNo, true
	case "s":
		return settleTell, true
	case "d":
		return settleHand, true
	case "t":
		return settleBack, true
	}
	return 0, false
}

// ── the same question, asked inside the room ────────────────────────────────
//
// THE PERSON IS INVITED INTO THE ROOM TO DO THE LOOKING. The roster's row reads
// `finished — look it over`, enter opens the node's page, and the page is where
// the work actually is — the transcript, the calls, the files. So the page is
// where the question has to be asked as well: for a while it was not, and the
// room of a node that needed a look drew `task finished — esc to return` at its
// foot while the only answers were on a card back in the conversation, buried
// under the chat's own commentary about the landing, and answerable only once
// that card was walked to and selected. A person sat in the room of the very
// thing they were asked to decide about with no way to decide.
//
// THE STATE IS ONE STATE. The room does not get a question of its own: it draws
// the conversation's own card — found by the node's id, [app.doneCardFor] — with
// the same two rows, the same four chips and the same receipt, through the same
// renderer, so the two surfaces cannot disagree about whether the question is
// still standing. Answering in the room marks the card in the conversation
// decided; answering the card marks the room's foot; and a decision made
// anywhere else — the model's own `tasks … resolve`, another window — reaches
// both through the same `already answered` refresh. Nothing is cached: the foot
// is derived from the card on every draw, so a node settled while somebody was
// reading its room shows the receipt or the plain foot on the next frame and
// never a dead answers row.

// roomSettleHintFor is the room's share of the hint slot while its node is still
// asking (room.go's [app.roomHint]). It names the answers THIS card is actually
// drawing, in the words the chips are drawing them in — because the two answers
// are the ask's own ([session.TaskAsk]) and a slot spelling `accept` over a card
// whose first chip says `resolve it` would be naming a key that is not there.
//
// It never names esc: the legend's left end already carries that key for as long
// as a room is open.
func (a *app) roomSettleHintFor(card *taskDone) []string {
	choices := a.settleChoices(card)
	rungs := make([]string, 0, len(choices))
	// EVERY RUNG IS A RANKED PREFIX OF THE ONE ABOVE IT, which is rowfit.go's law
	// 3 said about a sentence: what a narrow frame shows is the top of what a wide
	// one shows, in the same order, with a count of what went rather than a
	// silence, spelled the way the answers row itself spells one ([settleHidLead]).
	for keep := len(choices); keep > 0; keep-- {
		said := ""
		for _, choice := range choices[:keep] {
			if said != "" {
				said += settleGap
			}
			said += strings.TrimSuffix(strings.TrimPrefix(choice.key, "["), "]") + choice.word
		}
		if hid := len(choices) - keep; hid > 0 {
			said += settleHidLead + itoa(hid)
		}
		rungs = append(rungs, said)
	}
	return rungs
}

// settleHintAt is the longest rung the legend can actually draw at this width,
// beside whatever left label this frame is wearing.
//
// IT ASKS THE LEGEND'S OWN ARITHMETIC rather than a second copy of it: the left
// label comes from [app.legendLeft] and a line fits when its two labels and the
// border's own cells do ([app.legendLine]). A rung measured wrong here would be
// a rung the legend silently threw away, which is the defect being fixed one
// rung further down.
//
// tail is what the caller hangs off the end of every rung — the roster's own
// hold hint adds ` · esc`, because out there `esc` gives the column back and
// nothing else on the frame says so (taskeffort.go's [app.railHoldHintWord]);
// the room's slot adds nothing, because the legend's left end is already
// carrying that key. It is the last thing given up: a frame too narrow for even
// the first answer keeps the way out and drops the answers.
func (a *app) settleHintAt(card *taskDone, width int, tail string) string {
	for _, say := range a.roomSettleHintFor(card) {
		full := say + tail
		left, _ := a.legendLeft(width, legendRoom(width, full))
		if ansi.StringWidth(left)+ansi.StringWidth(full)+legendFurniture <= width {
			return full
		}
	}
	return strings.TrimPrefix(tail, railSep)
}

// legendFurniture is what [app.legendLine] spends on a line with a label at
// both ends before either label starts: `─ ` and one space after the left, one
// space before the right and ` ─` after it, and the one fill cell that line
// refuses to draw without.
const legendFurniture = 7

// doneEntryFor is the index of the LATEST landed card for one node, or -1. The
// latest, because the engine lands a node more than once — once needing a look,
// and again as done or failed once somebody decided — and the newest card is
// the one that says where the work stands now.
func (a *app) doneEntryFor(id uint64) int {
	for i := len(a.entries) - 1; i >= 0; i-- {
		if e := a.entries[i]; e.kind == entryDone && e.done != nil && e.done.id == id {
			return i
		}
	}
	return -1
}

// doneCardFor is the latest landed card for one node, or nil.
func (a *app) doneCardFor(id uint64) *taskDone {
	if i := a.doneEntryFor(id); i >= 0 {
		return a.entries[i].done
	}
	return nil
}

// roomSettleCard is the card the open room's foot is drawn from, or nil when
// the foot is the plain finished line: the room is on a landed node whose
// latest card either still asks or wears a receipt. A run's page has no node to
// decide about, an ordinary landing has nothing to ask, and a card under `auto`
// is being decided by somebody else. A guest belongs to another conversation;
// its numeric id must never select this conversation's decision card.
func (a *app) roomSettleCard() *taskDone {
	if a.room == nil || !a.room.done || a.room.orch != nil || a.roomIsGuest() {
		return nil
	}
	card := a.doneCardFor(a.room.id)
	if card == nil {
		return nil
	}
	if a.settleAsking(card) || a.settleAuto(card) || card.decided != "" {
		return card
	}
	return nil
}

// roomSettleAsking reports whether the open room's node is still a question.
func (a *app) roomSettleAsking() bool {
	return a.settleAsking(a.roomSettleCard())
}

// settleCardOf is the card one drawn row belongs to, and it is the seam the
// pointer reads through (hover.go, [app.settlePress]): a conversation row
// carries its entry's index, and a room's foot carries none — the room IS the
// selection, so its rows resolve to the room's own node.
func (a *app) settleCardOf(entry int) *taskDone {
	if entry >= 0 {
		return a.doneCardAt(entry)
	}
	return a.roomSettleCard()
}

// roomSettleRows is the foot of a room whose node needs a look: the ask line
// and the answers row while the question stands, the receipt once it is
// answered. It reports false when the room's foot is not this file's to draw.
//
// THE ROWS CARRY NO ENTRY, which is the room's own convention for a foot
// (room.go's [roomFinishedRefusal]) and what makes the pointer resolve them to the
// room's node rather than to an index into a list that is not on screen.
func (a *app) roomSettleRows(out []row, width int) ([]row, bool) {
	card := a.roomSettleCard()
	if card == nil {
		return out, false
	}
	out = a.settleRows(out, card, -1, width, 0)
	// The chips were just measured against THIS layout. The conversation's own
	// copy of the row may sit at a different indent (a rollup's), so its cache is
	// dropped and it re-measures on its next frame rather than pressing against
	// columns the room recorded.
	if i := a.doneEntryFor(card.id); i >= 0 {
		a.entries[i].stale = true
	}
	return out, true
}

// roomSettleKey routes the four letters inside the room of a node that is still
// asking, and reports whether it took one.
//
// THE ROOM'S BOX STEERS THE WORKER, so the letters fire ONLY over an EMPTY box —
// the law every bare letter on this surface is held to (stop.go's `x`,
// [app.settleCardKey]) — and never during a history walk, whose keys are the
// walk's. The overlays, the stop card and the steer guard are all refused
// before this is reached (room.go's [app.roomKey] and app.go's key order), and
// they stay refused: this route sits beside the room's other keys rather than
// loosening the conversation path's selection rule.
func (a *app) roomSettleKey(msg tea.KeyPressMsg) bool {
	if a.room == nil || a.chordsStandDown() || a.recalling() || a.rew.on {
		return false
	}
	return a.settleKeyOn(a.roomSettleCard(), msg)
}

// ── the same question, answered from the column ─────────────────────────────
//
// THE `!` SUMMONS SOMEBODY TO THE COLUMN AND THE COLUMN TOLD THEM NOTHING. A
// node lands `needs your look`, the roster's row says `finished — look it over`
// in the warn hue, and until this the three words that answer it — accept, look
// again, not right — existed only on the card back in the conversation and at
// the foot of the node's own room. A person standing on the row with the
// keyboard in their hand had to learn, from somewhere else, that entering the
// room was the way to answer; a key that only acts once you have found out what
// it is for is a key that is not there.
//
// SO THE ROW ANSWERS, and it answers with the same letters, through the same
// [app.settleCard], to the same card. There is no second state: [railSettleCard]
// finds the conversation's own card by the focused node's id, exactly as
// [app.roomSettleCard] finds it by the open room's, so answering from the
// column marks the card and the room's foot on the very next frame.
//
// THE GUARD IS THE GUARD EVERY BARE LETTER ON THIS SURFACE HAS (chordfocus.go):
// the roster must have been handed the keyboard, the box must be empty, and no
// history walk or rewind may be on. These letters are an answer to a question
// the surface is blocked on and drawn on screen — which is the one thing that
// earns a bare letter here — and the hint slot names them for as long as they
// work ([app.railHoldHintWord]).

// railSettleCard is the card the roster's FOCUSED ROW is asking with, or nil.
// It is nil unless the roster holds the keyboard, because without a hold there
// is no cursor and nothing is being aimed at ([app.railFocusNode] says the same
// about `x`).
//
// A CARD AFORGE IS DECIDING IS NOT ONE OF THEM. There is nothing to answer on it
// and one key that would work, and a column that offered `t` where every other
// state offers `a` and `n` would be teaching a fourth grammar for one row — so
// taking a decision back is done where the sentence saying aforge has it is
// drawn: on the card, and at the foot of the node's own room.
func (a *app) railSettleCard() *taskDone {
	node := a.railFocusNode()
	if node == nil {
		return nil
	}
	card := a.doneCardFor(node.id)
	if !a.settleAsking(card) {
		return nil
	}
	return card
}

// railSettleKey routes the settle letters on the roster's focused row and
// reports whether it took one.
func (a *app) railSettleKey(msg tea.KeyPressMsg) bool {
	if a.chordsStandDown() || a.recalling() || a.rew.on || a.roomOpen() {
		return false
	}
	return a.settleKeyOn(a.railSettleCard(), msg)
}

// settleTouched is the redraw after a card changed, wherever it was answered
// from: the conversation's copy of the row is dropped, and so is the room's
// page, which caches its rows as one list (room.go) and would otherwise keep
// drawing an answers row over a question that is gone.
func (a *app) settleTouched(card *taskDone) {
	if i := a.doneEntryFor(card.id); i >= 0 {
		a.entries[i].stale = true
	}
	if a.room != nil && a.room.id == card.id {
		a.room.dirty = true
	}
	a.touch()
}

// ── the answer, spent ───────────────────────────────────────────────────────

// settleCard is the one place a landed card is answered from, whichever door the
// answer came through — its keys or its clickable columns.
//
// THE REFUSAL IS A REFRESH AND NEVER AN ALERT. A node this surface still shows
// as asking can have been settled anywhere — the model's own `tasks … resolve`,
// a re-audit that finally answered, another window — and the engine says so in a
// sentence written for a person. It is not news worth a note in the middle of
// somebody's work: the card simply stops asking, which is the true thing about
// it, and the engine's own second card says what became of the work.
func (a *app) settleCard(card *taskDone, answer settleAnswer) {
	doors, ok := a.settleDoors()
	if !ok || card == nil {
		return
	}
	// TAKING IT BACK IS THE ONE ANSWER A CARD THE MODEL IS HOLDING CAN TAKE, and
	// every other one is refused there: a person who has handed a question over
	// takes it back before they answer it, which is one press and then the chips.
	if answer == settleBack {
		if !a.settleAuto(card) {
			return
		}
		if err := doors.TakeBackDecision(card.id); err != nil {
			a.settleRefused(card, err)
			return
		}
		// THE CARD IS ASKING AGAIN AND NOTHING WAS RESOLVED, so there is no receipt:
		// what changed is whose hands the question is in, and the chips appearing
		// where the sentence about aforge deciding stood say that better than a line
		// about it would.
		card.status.Ask.Owner, card.trouble = session.TaskAskOwnerPerson, ""
		a.settleTouched(card)
		return
	}
	if !a.settleAsking(card) || !a.settleOffers(card, answer) {
		return
	}
	switch answer {
	case settleTell:
		a.settleTell(card)
		return
	case settleHand:
		// IT CHANGES NO SETTING. The row that used to stand here flipped
		// `task.settle` to `auto` on the way past, which is a preference disguised
		// as an answer; this hands over THIS card and nothing else, and the standing
		// row is in /settings under Session.
		if err := doors.HandUnverifiedToModel(card.id); err != nil {
			a.settleRefused(card, err)
			return
		}
		// AND THE CARD RECORDS WHO IS HOLDING IT NOW, which is what stops every
		// other surface asking the person about a node they have just handed over
		// (taskreviewstate.go reads this). The engine wrote the same thing on the
		// node; this is the copy the card in front of somebody is drawn from.
		card.status.Ask.Owner = session.TaskAskOwnerModel
		card.decided, card.trouble = settleHandedLine, ""
		a.settleTouched(card)
		return
	}
	if err := a.spendSettle(doors, card, answer); err != nil {
		a.settleRefused(card, err)
		return
	}
	card.trouble = ""
	card.decided = settleReceipt(card.status.Ask.Kind, answer)
	a.settleTouched(card)
}

// spendSettle is the engine verb one answer maps to, decided by WHAT WAS ASKED.
//
// A CONFLICT'S YES IS NOT AN ACCEPT. Saying `resolve it` spends one more merge
// round — the person's branch is merged into the task's, a worker resolves the
// markers and the check runs again — and nothing is taken as done on the way
// past. Every other yes is the accept it reads as, and every no is the same act
// however it came to be asked.
func (a *app) spendSettle(doors settleAgent, card *taskDone, answer settleAnswer) error {
	if answer == settleYes && card.status.Ask.Kind == session.TaskAskConflict {
		merge, ok := a.conflictDoors()
		if !ok {
			// Unreachable while the chip is drawn only where the door is, and kept
			// because a refusal is cheaper than a nil call if that ever stops being
			// true (the absence law's own belt).
			return session.ErrTaskDecided
		}
		return merge.ResolveConflict(card.id)
	}
	if answer == settleYes {
		return doors.ResolveUnverified(card.id, session.TaskAccept, "")
	}
	return doors.ResolveUnverified(card.id, session.TaskRefute, "")
}

// settleReceipt is the line the card wears afterwards, in the person's own
// voice: what THEY did, not what the machine reached.
func settleReceipt(kind session.TaskAskKind, answer settleAnswer) string {
	switch {
	case answer == settleYes && kind == session.TaskAskConflict:
		return settleResolveLine
	case answer == settleYes:
		return settleTookLine
	}
	return settleNotRightLine
}

// settleTell is `[s]`, and it is the one column that ANSWERS NOTHING.
//
// IT OPENS THE NODE'S OWN PAGE AND POINTS THE BOX AT IT (room.go's
// [app.openRoom] retargets the composer to that task, recipient.go). That page
// is the existing place a person says something to one piece of work: it carries
// the address in its header, it keeps its own unsent draft, and its box is the
// steer surface this program already has (steer.go). What is typed there is sent
// as a steer and NOTHING HERE RESOLVES THE TASK — "looks good" typed on a card
// must not silently become an accept, which is the whole reason this is a third
// column rather than a third answer.
func (a *app) settleTell(card *taskDone) {
	a.openRoom(card.id, card.title)
}

// settleRefused is what the card does with an answer the engine would not take,
// and the whole of it is telling the two refusals apart.
//
// A DECISION SOMEBODY ELSE MADE IS A REFRESH ([session.ErrTaskDecided]): the
// question is genuinely gone, the card stops asking, and the engine's own second
// landed card says what became of the work. ANYTHING ELSE IS TROUBLE: the
// question is still standing and one answer could not be spent, so the choices
// stay and a dim line says so. Neither is a note in the middle of somebody's
// work — this is a card catching up, not news.
func (a *app) settleRefused(card *taskDone, err error) {
	if errors.Is(err, session.ErrTaskDecided) {
		card.decided, card.trouble = settleGoneLine, ""
	} else {
		card.trouble = settleTroubleLine
	}
	a.settleTouched(card)
}
