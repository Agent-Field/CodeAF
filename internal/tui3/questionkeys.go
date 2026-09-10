package tui3

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── ONE KEY GRAMMAR, SPELLED ONCE ───────────────────────────────────────────
//
// docs/design/questions/DESIGN.md's ONE KEY GRAMMAR law names fifteen keys and
// then says the thing that matters about them: "Spelled once in one table, read
// by every form and by the manual." This file is that table.
//
// IT IS ONE TABLE BECAUSE FOUR READERS AGREE THROUGH IT. The line, the card and
// the ratify row draw their answers row from it ([app.questionOffer]); the room
// reads it for its foot (lane S2 imports [questionKeys] rather than spelling a
// second set); `internal/manual/chat/questions.md` lists exactly these keys and
// exactly these words; and the tmux suite's needles come out of the same words.
// A key spelled in two places is a key that means two things the first week one
// of them moves — which is the defect `propose_task`'s two step defaults cost
// (CLAUDE.md's ONE SOURCE OF TRUTH).
//
// THE DIGITS ARE NOT IN IT. `1`–`9` pick the option at that index and there is
// nothing to spell: the option's own word is on its own row, and a table entry
// saying "1 — the first answer" would be furniture. The one exception the law
// carves out is task-states' `a`/`n`/`s`, which are that design's keys and are
// read off the [session.AnswerOption.Key] the lane put on the option — never
// re-decided here.

// questionVerb is one key in the grammar: what a person presses, what it does
// in their own words, and where it is offered.
//
// word is the OFFER'S spelling and the manual's — one lowercase phrase, no
// machinery vocabulary — because a key that reads one way on the row and
// another way on the page is two keys as far as anybody learning it is
// concerned.
type questionVerb struct {
	// key is the key as bubbletea spells it, which is what a comparison against
	// tea.KeyPressMsg.String() has to match.
	key string
	// word is what it does. It is what the answers row prints after the key and
	// what the manual's key list prints beside it.
	word string
	// forms is which of the four forms offer this key. A key drawn on a form
	// that does not act on it is a key that does nothing, which is worse than a
	// key that is not there.
	forms questionForms
	// needs is the condition under which the key is offered AT ALL, beyond the
	// form. The emptiness law lives here: no pick means no `enter →` line, no
	// rule to offer means no `r`, nothing real to undo means no `u`.
	needs questionNeed
	// giveUp is the order a narrow row surrenders its keys in — the HIGHEST
	// number goes first ([questionDropVerb]).
	//
	// IT IS RANKED AND NOT POSITIONAL, which is a repick against the first
	// draft of this table and the reason is one screen: dropping from the end
	// took `[r] make it a rule for everywhere` off a hundred-column row and
	// left `[c] change` and `[?] ask back` on it — the one key that was only
	// going to be offered once, given up for two that a person reaches by
	// simply typing. The rank says what a row is actually WORTH keeping, which
	// is not the order it reads best in.
	//
	// Zero is a key that is never given up: the way out, and the arrows that
	// are the only way to move a confirmation's cursor.
	giveUp int
}

// questionForms is a set of the forms one key belongs to.
type questionForms uint8

const (
	formsLine questionForms = 1 << iota
	formsCard
	formsRatify
	formsRoom
	// formsSheet is the batch form (questionsheet.go). Its two own keys are in
	// this table for the reason every other key is: the manual's page and the
	// row a person reads have to be spelt from one place.
	formsSheet
)

// formsBlock is the three forms THIS lane draws — the pinned block above the
// box. The room is lane S2's and is named here so its keys travel in the same
// table rather than in a second one beside it.
const formsBlock = formsLine | formsCard | formsRatify

func (f questionForms) holds(one questionForms) bool { return f&one != 0 }

// questionNeed is what has to be true before a key is offered.
type questionNeed uint8

const (
	// needAlways is a key every question of that form offers.
	needAlways questionNeed = iota
	// needPick is a question the asker named a pick on. Without one there is
	// nothing for `enter` to take and the row does not say there is.
	needPick
	// needRule is the third same-shaped yes, which is the only moment `r` is
	// offered (docs/design/questions/DESIGN.md's RULES ARE OFFERED, VISIBLE,
	// FORGETTABLE).
	needRule
	// needUndo is an answer that can still be taken back — `u` while real.
	needUndo
	// needRoom is a question with more behind it than the block is drawing: a
	// body, a block, a dimension to compare on. Opening one that has nothing
	// more to show is a page that says what the row said.
	needRoom
	// needWords is a question that takes a typed answer at all.
	needWords
	// needDial is a question whose kind can be handed to the dial from now on.
	needDial
	// needWalk is a question with a cursor to move: the confirmation kind, and
	// nothing else on this block ([questionSafeAt] says why).
	needWalk
	// needHands is a decision NOBODY BUT A PERSON MAY EVER MAKE. It is the
	// condition on `d you decide` and on `D`, and it is stop.go's law widened
	// to every question of that shape: "There is no bypass key, no modifier
	// that skips the question, and no don't-ask-me-again." A confirmation is
	// asked because the act cannot be taken back, so handing it to the asker
	// would be the surface deciding an irreversible thing on somebody's behalf
	// — which is exactly what the question exists to prevent.
	needHands
	// ── the room's own conditions (lane S2) ────────────────────────────────
	//
	// They are HERE and not in a second table beside this one, because a key is
	// a key wherever it is drawn: the room's `a`/`b`/`=` and its `n` have to be
	// spelled once for the manual's key list to be true, and the day one of them
	// moves it moves in one place.

	// needChecklist is a question answered by ticking several rows.
	needChecklist
	// needOrdered is a checklist whose ORDER is part of the answer. It is the
	// same shape as [needChecklist] with a second promise, so the two keys are
	// separate rows rather than one that sometimes does nothing.
	needOrdered
	// needPairs is a run of two-way questions.
	needPairs
	// needOptions is a question with answers to say "none of these" about. A
	// reframe on a question with no list is just words, and the box already
	// takes words.
	needOptions
	// needMoves is a question with something the ARROWS move that is not a
	// cursor: a dial, or a hole with a short list of choices in it. It is a
	// second row on [questionWalkKey] rather than a second word on the first,
	// because `←→ pick` and `←→ move it` are different promises — one walks a
	// cursor between two answers and the other changes the answer itself — and
	// the two can never be true at once ([needWalk] is the confirmation kind,
	// which carries neither).
	needMoves
)

// The keys, by the name each is referred to by. They are constants rather than
// literals at the call sites for the reason every key on this surface is: a
// comparison against a literal is a key nothing can find when it moves.
const (
	questionEnterKey   = "enter"
	questionLaterKey   = "esc"
	questionOpenKey    = "o"
	questionCommentKey = "c"
	questionCompareKey = "x"
	questionAskBackKey = "?"
	questionDecideKey  = "d"
	questionDialKey    = "D"
	questionRuleKey    = "r"
	questionUndoKey    = "u"
	questionBlankKey   = "tab"
	// questionToggleKey IS `space` AND NOT `" "`, which the table's own contract
	// demands: "key is the key as bubbletea spells it, which is what a comparison
	// against tea.KeyPressMsg.String() has to match" — and that library spells a
	// space bar `space`, whichever way the press arrives. It was the byte until
	// the room became the first form to route this key, and a comparison against
	// the byte matched nothing at all.
	questionToggleKey = "space"
	// questionSendKey and questionAlikeKey are the sheet's, and they are LETTERS
	// where a question's answers are digits because a sheet's digits are already
	// its answers: `1`-`9` answer the row the cursor is on, so the two acts that
	// are about the WHOLE batch cannot also be digits.
	//
	// AND IT IS `g` RATHER THAN THE ROOM'S `=` ([questionSameKey]) because the
	// two are not the same act. The room's `=` says "this pair keeps coming up,
	// answer it the same way from now on" — a rule about the future. The sheet's
	// `g` says "these rows in front of me take the answer I just gave" — one
	// batch, now, nothing written down.
	questionSendKey  = "s"
	questionAlikeKey = "g"
	// questionWalkKey is the PAIR of arrow keys, and it is spelled as the pair
	// because that is how it is drawn and how it is learnt: `←→ pick` is one
	// affordance, and a row that listed two keys for one act would be a row
	// teaching arithmetic instead of a choice. The routing reads `left` and
	// `right` individually ([app.questionOptionKey]); this is the SPELLING.
	questionWalkKey = "←→"
	// The room's own keys (lane S2). `a` and `b` are the two sides of a pair and
	// `a` is also the checklist's "take its suggestion" — the one collision the
	// grammar has, and it is the same instinct at two shapes: take the thing on
	// the left.
	questionSuggestKey = "a"
	questionPairAKey   = "a"
	questionPairBKey   = "b"
	questionSameKey    = "="
	questionReframeKey = "n"
	// questionOrderKey is the PAIR of shifted arrows, spelled as a pair for
	// [questionWalkKey]'s reason: `shift+↑↓ order them` is one affordance. The
	// routing reads `shift+up` and `shift+down` individually.
	questionOrderKey = "shift+↑↓"
)

// questionKeys IS THE TABLE. Order is the order the answers row prints them in,
// and it is ordered by how often a person reaches for each: take the pick, put
// it off, open it up, then the four that say something about the decision
// rather than giving it.
//
// `esc` IS LATER AND NOT CANCEL, and that is the one word in this table worth
// arguing about. The consent block spelled it `cancel` for a year and cancel
// meant deny — the safe reading of "get this off my screen" when the only
// alternative was a modal nobody could leave. The block is not modal any more
// (THE NEVER MODAL law), so there is a third thing esc can do that is neither
// answering nor trapping: fold the question to the chip, leave the turn paused
// on it, and let the person type. Nothing is cancelled, so the word may not say
// cancelled.
var questionKeys = []questionVerb{
	{key: questionEnterKey, word: "take the pick", forms: formsBlock | formsRoom | formsSheet, needs: needPick, giveUp: 1},
	// THE SHEET'S TWO SIT WHERE A PERSON REACHES FOR THEM — beside `enter`,
	// because answering a batch is open-one, answer, send — and only `g` is ever
	// given up: `s` is the reason the sheet exists (a batch answered row by row
	// and then not sent is a batch nobody answered), so it is ranked zero beside
	// `esc`.
	{key: questionSendKey, word: questionSheetSendWord, forms: formsSheet},
	{key: questionAlikeKey, word: questionSheetSameWord, forms: formsSheet, giveUp: 2},
	{key: questionLaterKey, word: "later", forms: formsBlock | formsRoom | formsSheet},
	{key: questionOpenKey, word: "open it", forms: formsLine | formsCard, needs: needRoom, giveUp: 3},
	{key: questionCommentKey, word: "change", forms: formsBlock | formsRoom, needs: needWords, giveUp: 5},
	{key: questionCompareKey, word: "compare", forms: formsRoom, giveUp: 4},
	{key: questionAskBackKey, word: "ask back", forms: formsCard | formsRoom, needs: needWords, giveUp: 4},
	{key: questionDecideKey, word: "you decide", forms: formsCard | formsRoom, needs: needHands, giveUp: 4},
	{key: questionDialKey, word: "decide these from now on", forms: formsCard | formsRoom, needs: needDial, giveUp: 7},
	{key: questionWalkKey, word: "pick", forms: formsBlock | formsRoom, needs: needWalk},
	// THE RULE OFFER IS THE LAST THING GIVEN UP AFTER THE WAY OUT, because it
	// is the only key here that is on the row ONCE: the third same-shaped yes
	// happens once, and a row that dropped it to keep `[c] change` would have
	// spent the offer on nothing. `u` is beside it for the same reason — a
	// ratify line whose undo is off the row is a ratify line with no undo.
	{key: questionRuleKey, word: "make it a rule", forms: formsBlock | formsRoom, needs: needRule, giveUp: 2},
	{key: questionUndoKey, word: "undo", forms: formsRatify | formsRoom, needs: needUndo, giveUp: 2},
	{key: questionBlankKey, word: "next blank", forms: formsRoom, giveUp: 6},
	{key: questionToggleKey, word: "tick it", forms: formsRoom, giveUp: 6},
	// The room's own four. `a` is the one collision in the grammar — "take its
	// suggestion" on a checklist and "the first one" on a pair — and it is two
	// rows here rather than one key with two words, because the offer row prints
	// the word and the row a person reads may not be ambiguous even where the
	// key is. Which of the two is live is decided by which shape is on screen,
	// and the needs below are what decide it.
	{key: questionSuggestKey, word: "take its suggestion", forms: formsRoom, needs: needChecklist, giveUp: 5},
	{key: questionOrderKey, word: "order them", forms: formsRoom, needs: needOrdered, giveUp: 7},
	{key: questionPairAKey, word: "the first", forms: formsRoom, needs: needPairs},
	{key: questionPairBKey, word: "the second", forms: formsRoom, needs: needPairs},
	{key: questionSameKey, word: "same either way", forms: formsRoom, needs: needPairs, giveUp: 3},
	// THE ANSWER THAT IS NOT ON THE LIST. It is last in the table and nearly
	// first to be given up, because it is the rarest answer to any question —
	// and it is on the row at all because a person who thinks the question is
	// wrong has no other way to say so without it reading as a refusal.
	{key: questionReframeKey, word: "none of these", forms: formsRoom, needs: needOptions, giveUp: 8},
	// `←→ move it` IS ON THE CARD AS WELL AS IN THE ROOM, because the card draws
	// the sentence with a hole in it too — a task proposal's model shortlist is
	// exactly that shape ([session.TaskModelShape]). It is a second row on
	// [questionWalkKey] rather than a second word on the first for the reason
	// [needMoves] states: `pick` walks a cursor between two answers and `move it`
	// changes the answer itself, and the two are never true at once.
	{key: questionWalkKey, word: "move it", forms: formsCard | formsRoom, needs: needMoves},
	// INSIDE THE ROOM `o` OPENS AN ANSWER RATHER THAN THE PAGE, which is why it
	// is a second row rather than a second word: `[o] open it` on a card is a
	// promise about a page, and repeating that promise on the page itself would
	// be an offer to go where somebody already is. It is the first thing a narrow
	// row gives up, because every answer already wears its own ▸/▾.
	{key: questionOpenKey, word: "open this answer", forms: formsRoom, needs: needOptions, giveUp: 9},
}

// questionVerbFor is one key's row in the table, by key. It is what a caller
// that already knows which key it wants asks rather than ranging.
func questionVerbFor(key string) (questionVerb, bool) {
	for _, verb := range questionKeys {
		if verb.key == key {
			return verb, true
		}
	}
	return questionVerb{}, false
}

// questionSheetKeyWord is one key's word ON A SHEET, which is the table's word
// for every key but `enter`.
//
// `enter` IS ONE KEY WITH ONE MEANING AND TWO SENTENCES. It always means "act on
// what the cursor is on"; on the block that is the answer the asker recommends,
// and on a sheet the cursor is on a ROW rather than an answer, so acting on it
// opens that row. The substitution is done here for the same reason `r`'s is
// done in [app.questionVerbParts] — the table holds one row per key, and a word
// that depends on what is being drawn is filled in by the drawer.
func questionSheetKeyWord(verb questionVerb) string {
	if verb.key == questionEnterKey {
		return questionSheetOpenWord
	}
	return verb.word
}

// questionKeyWord is one key's word, or "" where the table does not have it.
// The manual's page and the offer row both print through here.
func questionKeyWord(key string) string {
	verb, ok := questionVerbFor(key)
	if !ok {
		return ""
	}
	return verb.word
}

// questionChipKey is the chord that raises the newest open question from
// wherever a person is standing — the status line's chip says it, and it works
// on every page.
//
// IT IS `alt+a` AND THE CHOICE IS ARITHMETIC RATHER THAN TASTE. Every ctrl
// letter on this surface is already bound (`ctrl+g` is the rail's stow,
// `ctrl+k` the switcher, `ctrl+q` the follow-up), and the two chords that read
// best on paper are the two that do not survive a real terminal: `ctrl+?` is
// DEL on most of them and `ctrl+/` arrives as `ctrl+_`. `alt+a` is free in this
// surface's whole table, it is the initial of the thing it does, and alt is
// already this surface's own modifier for reaching a place (`alt+1`…`alt+7`) —
// with chords.go already holding the Mac option-as-meta question open, so the
// one terminal that would send `å` instead is the one this surface already
// watches for.
const questionChipKey = "alt+a"

// questionAnswerKeys is which keys a question of this shape actually offers, in
// the table's order, with the emptiness law applied.
//
// It is what the offer row prints and what [app.questionKey] routes against —
// ONE reading, so a key drawn and a key taken cannot come apart.
func (a *app) questionAnswerKeys(q questionShown, form questionForms) []questionVerb {
	out := make([]questionVerb, 0, len(questionKeys))
	for _, verb := range questionKeys {
		if !verb.forms.holds(form) {
			continue
		}
		if !a.questionOffers(q, verb.needs) {
			continue
		}
		out = append(out, verb)
	}
	return out
}

// questionOffers answers one key's condition.
func (a *app) questionOffers(q questionShown, need questionNeed) bool {
	switch need {
	case needPick:
		if q.question.Ask == session.AskConfirmation {
			// A CONFIRMATION ALWAYS HAS A PICK AND IT IS THE CURSOR. It is the
			// one shape on this block where the person's own keyboard chooses
			// which answer `enter` takes ([questionSafeAt] puts it on the answer
			// that loses nothing), so the key is always offered — where every
			// other question offers it only when the ASKER recommended something.
			return true
		}
		return q.question.Pick != nil && strings.TrimSpace(q.question.Pick.Key) != ""
	case needRule:
		return q.rule
	case needUndo:
		return q.undoable
	case needRoom:
		return questionHasMore(q.question)
	case needWords:
		return questionTakesWords(q.question)
	case needDial:
		return q.question.Kind != "" && q.question.Asker.Kind != session.AskerSurface &&
			a.questionOffers(q, needHands)
	case needWalk:
		return q.question.Ask == session.AskConfirmation && len(q.question.Options) > 1
	case needHands:
		if q.question.Ask == session.AskConfirmation ||
			q.question.Stakes == session.StakesIrreversible {
			return false
		}
		// AND A PERMISSION THAT IS NOT CHEAP TO TAKE BACK IS NEVER HANDED OVER.
		// The approval gate asks because a policy said a person has to see this
		// call; `you decide` on it would give that decision straight back to the
		// thing the gate was put in front of, which is the gate answering itself
		// with one keystroke. docs/design/questions/DESIGN.md's kind table says
		// the same about the clock — a permission may act on its own "only when
		// Stakes == reversible" — and a key that skips a question is a clock a
		// person wound by hand.
		if q.question.Ask == session.AskPermission {
			return q.question.Stakes == session.StakesReversible
		}
		return true
	case needChecklist:
		return q.question.Input.Kind == session.InputChecklist
	case needOrdered:
		// AN ORDER KEY IS OFFERED ONLY WHERE THERE IS AN ORDER TO CHANGE. Two
		// rows have one arrangement and no second one, so the key would move a
		// list a person cannot see move.
		return q.question.Input.Kind == session.InputChecklist && len(q.question.Options) > 2
	case needPairs:
		return q.question.Input.Kind == session.InputPairs
	case needOptions:
		return len(q.question.Options) > 0
	case needMoves:
		// A CONFIRMATION'S ARROWS ARE ITS CURSOR AND NOTHING ELSE ([needWalk]),
		// which is stop.go's law: the cursor starts on the answer that loses
		// nothing and the arrows are how it is moved. So this condition steps
		// aside for that kind rather than the two sharing `←→` on one screen.
		if q.question.Ask == session.AskConfirmation {
			return false
		}
		if q.question.Input.Dial != nil {
			return true
		}
		for _, blank := range q.question.Input.Blanks {
			if len(blank.Choices) > 0 {
				return true
			}
		}
		return false
	}
	return true
}

// questionHasMore reports whether opening this question would show anything the
// block is not already showing. A room drawn over a question with two bare
// words in it is a page that says what the row said.
func questionHasMore(q session.Question) bool {
	if len(q.Attach) > 0 || q.Input.Kind == session.InputChecklist || q.Input.Dial != nil {
		return true
	}
	if len(q.Input.Blanks) > 0 {
		return true
	}
	for _, option := range q.Options {
		if strings.TrimSpace(option.Body) != "" || len(option.Blocks) > 0 || len(option.Dimensions) > 0 {
			return true
		}
	}
	return false
}

// questionTakesWords reports whether a typed answer means anything here.
//
// A CONFIRMATION TAKES NONE (docs/design/questions/DESIGN.md's kind table says
// so in one word), because the two answers ARE the question: `stop it` and
// `keep going` have no third reading a sentence could add, and a box under them
// would be a place to type something nothing reads.
func questionTakesWords(q session.Question) bool {
	if q.Ask == session.AskConfirmation {
		return false
	}
	return true
}
