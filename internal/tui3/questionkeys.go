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
	questionToggleKey  = " "
	// questionWalkKey is the PAIR of arrow keys, and it is spelled as the pair
	// because that is how it is drawn and how it is learnt: `←→ pick` is one
	// affordance, and a row that listed two keys for one act would be a row
	// teaching arithmetic instead of a choice. The routing reads `left` and
	// `right` individually ([app.questionOptionKey]); this is the SPELLING.
	questionWalkKey = "←→"
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
	{key: questionEnterKey, word: "take the pick", forms: formsBlock | formsRoom, needs: needPick, giveUp: 1},
	{key: questionLaterKey, word: "later", forms: formsBlock | formsRoom},
	{key: questionOpenKey, word: "open it", forms: formsLine | formsCard | formsRoom, needs: needRoom, giveUp: 3},
	{key: questionCommentKey, word: "change", forms: formsBlock | formsRoom, needs: needWords, giveUp: 5},
	{key: questionCompareKey, word: "compare", forms: formsRoom, giveUp: 4},
	{key: questionAskBackKey, word: "ask back", forms: formsCard | formsRoom, needs: needWords, giveUp: 6},
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
		return q.question.Ask != session.AskConfirmation &&
			q.question.Stakes != session.StakesIrreversible
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
