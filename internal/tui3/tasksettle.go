package tui3

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE DECISION, PUT WHERE THE PERSON IS STANDING.
//
// A task that finishes with nobody able to say whether it holds lands
// `needs your look` (task.go's [taskUnverifiedWord]). It is neither done nor
// failed, its branch is kept, and everything queued behind it waits — and until
// this file the ONLY way to move it was to talk the model into calling its
// `tasks` tool. The card said "needs your look", the roster said "needs your
// look", and neither of them had a door. What people did instead was type
// "accept it" at the conversation and hope the model spent the right verb on
// the right id.
//
// So the card asks, and the card is answered:
//
//	? ◆ Port the parser · needs your look 6m40s · 2 files · branch kept · task/parser
//	  "finished, but needs your look — nobody could say whether it holds" · spawned 14:02
//	  finished, but nobody has checked it — your call
//	  [a] accept · [l] look again · [n] not right · [d] decide these for me
//
// ── THE WORDS ARE THE PERSON'S AND THE VERBS ARE THE ENGINE'S ──
//
// session's three answers are `accept`, `reaudit` and `refute`
// (task_contract.go's [session.TaskResolutions]) and not one of those three
// words is on this row. `reaudit` names the apparatus, and `refute` is a
// courtroom — the vocabulary law this surface holds every task word to
// (task.go's [taskUnverifiedWord] states it). What a person is choosing between
// is: take it, have it checked again, or say it is not finished. That is what
// the row says, and the mapping to the engine's verbs happens once, here.
//
// ── THE FOURTH CHOICE IS THE ESCAPE HATCH ──
//
// `decide these for me` is the annoyance's own answer, placed at the moment the
// annoyance happens: it flips the person's `task.settle` row to `auto` — so
// every later landing goes to the model instead of to this card — AND hands
// THIS one over on the way past ([session.Agent.HandUnverifiedToModel]). It is
// drawn dimmer than the three answers because it is a preference and not an
// answer to the question in front of it.
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
// transcript reads as what it is: this landed needing a look → you took it as
// done → task 7 done · merged.

// settleAnswer is one of the four things the row offers. It is this surface's
// own enumeration and not [session.TaskResolution], because the fourth choice
// is not a resolution at all.
type settleAnswer uint8

const (
	settleTake settleAnswer = iota
	settleAgain
	settleNotRight
	settleAlways
)

// resolution is the engine's verb for one of the first three answers.
func (s settleAnswer) resolution() session.TaskResolution {
	switch s {
	case settleTake:
		return session.TaskAccept
	case settleAgain:
		return session.TaskReaudit
	case settleNotRight:
		return session.TaskRefute
	}
	return ""
}

// The row's words, spelled once.
const (
	// settleAskWord is the line above the answers: what is being ASKED, rather
	// than what happened. The head and the outcome line above it already say what
	// happened, in the engine's own sentence, and a card that only ever said that
	// left a person reading "needs your look" with no idea that looking was
	// something they were expected to finish.
	settleAskWord = "finished, but nobody has checked it — your call"
	// The four choices, each as a key and the word beside it.
	settleTakeKey      = "[a]"
	settleTakeWord     = " accept"
	settleAgainKey     = "[l]"
	settleAgainWord    = " look again"
	settleNotRightKey  = "[n]"
	settleNotRightWord = " not right"
	settleAlwaysKey    = "[d]"
	settleAlwaysWord   = " decide these for me"
	settleGap          = " · "
)

// The receipts. Each says what the person did, in their own voice, because the
// row it replaces was a question they answered and not a state the machine
// reached.
const (
	settleTookLine     = "you took this as done"
	settleAgainLine    = "sent back to be checked again"
	settleNotRightLine = "you said it is not finished"
	settleHandedLine   = "handed to the chat — it decides these from now on"
	// settleGoneLine is the quiet refresh. A card can be answered from two places
	// at once — this row, and the model's own `tasks … resolve` — and whichever
	// arrives second finds the question already gone. That is not an error worth a
	// card of its own: it is the row catching up with a decision that was made.
	settleGoneLine = "already answered"
	// settleTroubleLine is the OTHER refusal, and it is a different fact: the
	// question is still standing and this particular answer could not be spent —
	// there is no checker configured to look again, the working copy is gone. So
	// the choices STAY, because two of them may still work, and the row says so in
	// one line instead of putting the engine's sentence — which names the
	// apparatus — in front of somebody.
	settleTroubleLine = "that one could not be taken — try another"
	// settleSavedLine is appended to the hand-over receipt when the preference
	// actually reached the person's profile, and left off when it did not — the
	// same honesty the consent card's "always" is held to (consent.go): a surface
	// that says "saved" is making a claim about a file.
	settleSavedLine = " · saved"
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
}

// settleDoors is the deciding half of the agent under this surface, when it has
// one.
func (a *app) settleDoors() (settleAgent, bool) {
	doors, ok := a.agent.(settleAgent)
	return doors, ok
}

// ── the row, drawn ──────────────────────────────────────────────────────────

// settleAsking reports whether this card is still a question: it landed needing
// a look, nobody has answered it here, and there is an engine under this surface
// that could take the answer.
//
// THE ABSENCE LAW IS THE LAST CLAUSE. A capability with nothing behind it is
// left off entirely rather than drawn and broken, so a surface whose agent has
// no resolver draws no chips at all and the card reads exactly as it did before
// this file existed.
func (a *app) settleAsking(card *taskDone) bool {
	if card == nil || !card.unverified || !card.asks || card.decided != "" {
		return false
	}
	_, ok := a.settleDoors()
	return ok
}

// settlePolicyAsks reports whether the person's `task.settle` row leaves the
// decision with them. It is read at LANDING and nowhere else ([taskDone.asks]):
// the row lives on disk, and a question asked once per frame about a file is a
// question asked sixty times a second.
func (a *app) settlePolicyAsks() bool {
	return config.TaskSettleAt(a.profileDir) != config.TaskSettleAuto
}

// settleRows appends one card's ask row and answers row to out, and records the
// columns a press is resolved against.
//
// The spans are written HERE, by the layout that drew them, and read by nothing
// else: a hit-test that measured the row itself would be measuring a row this
// frame may not have drawn (the same order [app.roomApprovalRows] keeps).
func (a *app) settleRows(out []row, card *taskDone, entry, width, indent int) []row {
	card.chips = nil
	if !a.settleAsking(card) {
		if card.decided != "" {
			pad := strings.Repeat(" ", indent)
			out = append(out, row{
				text:  a.pal.dim(pad + "  " + fit(card.decided, width-2-indent)),
				entry: entry, hit: hitDone,
			})
		}
		return out
	}
	pad := strings.Repeat(" ", indent)
	room := width - 2 - indent
	out = append(out, row{
		text:  a.pal.ask(pad + "  " + fit(settleAskWord, room)),
		entry: entry, hit: hitDone,
	})
	parts := settleParts(room)
	// Narrower than the three answers themselves. The row is cut rather than
	// re-spelled and it records NO targets, which is [app.roomApprovalRows]'s own
	// rule: a target under an ellipsis is a press that answers something nobody
	// can read.
	if ansi.StringWidth(strings.Join(parts, "")) > room {
		out = append(out, row{
			text:  a.pal.ask(pad + "  " + fit(strings.Join(parts, ""), room)),
			entry: entry, hit: hitDone,
		})
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
		answer, key := settleAnswerOf(part), isSettleKey(part)
		switch {
		case part == settleGap:
			line += a.pal.dim(part)
		case answer == hot && key:
			line += a.pal.bold(a.pal.accent(part))
		case answer == hot:
			line += a.pal.accent(part)
		case answer == settleAlways:
			// The preference is drawn quieter than the three answers beside it,
			// because it is not one of them: it settles what happens NEXT TIME.
			line += a.pal.dim(part)
		case key:
			line += a.pal.askBold(part)
		default:
			line += a.pal.ask(part)
		}
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

// settleParts is the answers row in pieces — key, word, separator, key, word …
// — so that the columns a press is resolved against are MEASURED off the row
// that was drawn rather than guessed at beside it.
//
// THE FOURTH CHOICE IS THE FIRST THING CUT. It is a preference, the three
// answers are the question, and a row that fits by losing an answer is a
// question with no visible way to answer it (harness.go's own law about which
// half of an offer row survives).
func settleParts(width int) []string {
	full := []string{
		settleTakeKey, settleTakeWord, settleGap,
		settleAgainKey, settleAgainWord, settleGap,
		settleNotRightKey, settleNotRightWord, settleGap,
		settleAlwaysKey, settleAlwaysWord,
	}
	if ansi.StringWidth(strings.Join(full, "")) <= width {
		return full
	}
	return full[:8]
}

// settleSpans is the pressable columns of the drawn row: each key AND the word
// beside it, never the separator between them — a press in the gap must not
// resolve as either answer.
func settleSpans(parts []string, from int) []settleChip {
	chips := make([]settleChip, 0, 4)
	at := from
	for i, part := range parts {
		width := ansi.StringWidth(part)
		if !isSettleKey(part) {
			at += width
			continue
		}
		to := at + width
		if i+1 < len(parts) {
			to += ansi.StringWidth(parts[i+1])
		}
		chips = append(chips, settleChip{span: hudSpan{from: at, to: to}, answer: settleAnswerOf(part)})
		at += width
	}
	return chips
}

// settleChip is one pressable answer on the row.
type settleChip struct {
	span   hudSpan
	answer settleAnswer
}

func isSettleKey(part string) bool {
	switch part {
	case settleTakeKey, settleAgainKey, settleNotRightKey, settleAlwaysKey:
		return true
	}
	return false
}

// settleAnswerOf maps a key or its word back to the answer it stands for.
func settleAnswerOf(part string) settleAnswer {
	switch part {
	case settleAgainKey, settleAgainWord:
		return settleAgain
	case settleNotRightKey, settleNotRightWord:
		return settleNotRight
	case settleAlwaysKey, settleAlwaysWord:
		return settleAlways
	}
	return settleTake
}

// ── the pointer ─────────────────────────────────────────────────────────────

// settlePress resolves a click on an answers row and reports whether it took it.
//
// IT SWALLOWS EVERY PRESS ON ITS OWN ROW, hit or miss, for the reason the
// approval row does (roomapproval.go): this is a thing the surface is waiting on
// the person for, and a press that missed a chip and fell through would expand
// the card under somebody who was reaching for an answer.
func (a *app) settlePress(entry, x int) bool {
	card := a.doneCardAt(entry)
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

// settleCardKey routes the four letters on a SELECTED card, and reports whether
// it took one.
//
// EVERY GUARD HERE IS THE SAME GUARD `x` HAS (stop.go). The card must be the
// selected one, the message box must be empty, and no overlay may be up —
// because these are letters, and a letter that answers a question while somebody
// is typing a sentence is a surface that decided their work was finished on the
// strength of the first word of a paragraph.
func (a *app) settleCardKey(msg tea.KeyPressMsg) bool {
	if a.sheet.open || a.home.open || a.pick.open || a.copy.on || a.rew.on ||
		a.roomOpen() || !a.input.empty() {
		return false
	}
	// The selection indexes WHICHEVER LIST THE BODY IS DRAWING, and a room's page
	// is not the conversation (render.go's [app.bodyDeck]) — which is why a room
	// is refused above rather than looked up here.
	card := a.doneCardAt(a.sel)
	if !a.settleAsking(card) {
		return false
	}
	var answer settleAnswer
	switch msg.String() {
	case "a":
		answer = settleTake
	case "l":
		answer = settleAgain
	case "n":
		answer = settleNotRight
	case "d":
		answer = settleAlways
	default:
		return false
	}
	a.settleCard(card, answer)
	return true
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
	if !a.settleAsking(card) {
		return
	}
	doors, ok := a.settleDoors()
	if !ok {
		return
	}
	if answer == settleAlways {
		a.settleAlways(doors, card)
		a.touch()
		return
	}
	if err := doors.ResolveUnverified(card.id, answer.resolution(), ""); err != nil {
		a.settleRefused(card, err)
		return
	}
	card.trouble = ""
	switch answer {
	case settleTake:
		card.decided = settleTookLine
	case settleAgain:
		card.decided = settleAgainLine
	case settleNotRight:
		card.decided = settleNotRightLine
	}
	a.touch()
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
	a.touch()
}

// settleAlways is the fourth choice: the preference and this one card, in that
// order.
//
// THE ORDER IS THE POINT. The row the person just pressed is the one that will
// stop appearing, so it is written first — a hand-over that succeeded while the
// preference failed to save would leave them pressing this again on the next
// landing, having been told it was handled.
func (a *app) settleAlways(doors settleAgent, card *taskDone) {
	saved := a.saveSettlePolicy(config.TaskSettleAuto)
	if err := doors.HandUnverifiedToModel(card.id); err != nil {
		a.settleRefused(card, err)
		return
	}
	card.decided, card.trouble = settleHandedLine, ""
	if saved {
		card.decided += settleSavedLine
	}
}

// saveSettlePolicy writes the person's `task.settle` row through the registry's
// own validated writer, and reports whether it reached the disk.
//
// IT GOES THROUGH THE REGISTRY AND NEVER NEAR THE FILE, which is the settings
// panel's own law (settings.go): one door, one validation, one place a person
// can go and undo it — the row is in /settings under Session, spelled exactly as
// it is here.
func (a *app) saveSettlePolicy(word string) bool {
	registry := a.registry()
	if registry == nil {
		return false
	}
	row, ok := registry.Row(config.KeyTaskSettle)
	if !ok {
		return false
	}
	return row.Apply(word) == nil
}
