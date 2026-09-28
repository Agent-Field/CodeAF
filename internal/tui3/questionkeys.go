package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/standing"
)

// ── ONE KEY GRAMMAR, SPELLED ONCE ───────────────────────────────────────────
//
// docs/design/questions/DESIGN.md's ONE KEY GRAMMAR law names fifteen keys and
// then says the thing that matters about them: "Spelled once in one table, read
// by every form and by the manual." This file is that table.
//
// IT IS ONE TABLE BECAUSE FOUR READERS AGREE THROUGH IT. The line, the card and
// the ratify row draw their answers row from it ([app.questionKeyRow]); the room
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
	// tier is which of the two rows a question draws this key on: the primary
	// keys are written into the frame's bottom edge and the rest stand on one
	// dim row under the frame (owner ruling 2026-09-11, hints pick A).
	//
	// IT IS A COLUMN AND NOT A LIST OF NAMES SOMEWHERE ELSE. The primary tier is
	// "what a person needs in order to answer this at all" — walk, take, later,
	// and whatever key is the ANSWER on a shape that has one (`space` on a
	// checklist, `a`/`b` on a pair) — and the secondary tier is everything that
	// says something ABOUT the decision rather than giving it. A row that mixed
	// them was the owner's "no hierarchy": answers, navigation, dismissal, four
	// meta-verbs and a clock at one hue and one weight.
	tier keyTier
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

// keyTier is which of a question's two key rows a key is drawn on.
type keyTier uint8

const (
	// keySecondary is the dim row under the frame. It is the ZERO VALUE because
	// most of the table is a verb about the decision, and a key that forgets to
	// say which tier it is on belongs on the quieter one.
	keySecondary keyTier = iota
	// keyPrimary is written into the frame's bottom edge: at most walk, take and
	// later, plus the one key that answers a structured shape.
	keyPrimary
)

// questionForms is a set of the forms one key belongs to.
type questionForms uint8

const (
	formsLine questionForms = 1 << iota
	formsCard
	formsRatify
	formsRoom
	// formsTabs, formsReview and formsGroup are the three drawings of a SET —
	// several questions one step raised (questionset.go): a tab, the review that
	// sends them, and the one permission frame. Their own keys are in this table
	// for the reason every other key is: the manual's page and the row a person
	// reads have to be spelt from one place.
	formsTabs
	formsReview
	formsGroup
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
	// needTicked is a checklist with at least one row ticked, which is the
	// moment `enter` has something to send.
	needTicked
	// needBlanks is a sentence with holes in it. It is a condition of its own
	// rather than a shape the room simply draws, because `tab` walks BETWEEN
	// holes and a form with none of them would be offering a key that moves
	// nothing — the same reason `space` is conditional beside it.
	needBlanks
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
	// needOther is the pointer standing on the `something else…` row, where the
	// row is a box and the only keys that mean anything are the ones that send
	// what is in it, go back to the list, or put the question off.
	needOther
	// needMoves is a question with something the ARROWS move that is not a
	// cursor: a dial, or a hole with a short list of choices in it. It is a
	// second row on [questionWalkKey] rather than a second word on the first,
	// because `←→ pick` and `←→ move it` are different promises — one walks a
	// cursor between two answers and the other changes the answer itself — and
	// the two can never be true at once ([needWalk] is the confirmation kind,
	// which carries neither).
	needMoves
	// needCompare is a question whose answers have something to lay against
	// each other: the asker's dimensions, or the `+`/`−` lines the fallback
	// reads ([questionAxes]). THE EMPTINESS LAW: `x` on a question with nothing
	// to compare would open a table with no rows in it.
	needCompare
	// needBeside is a page drawn as two panes with the pointer on an answer and
	// the arrows still the list's: `→` has a pane to move into.
	needBeside
	// needReading is the arrows handed to the evidence pane (`→`).
	needReading
	// needScope is a question that offered more than one LIFETIME for its answer
	// and can be taken back (questionscope.go). A question with one lifetime has
	// nothing to cycle, and an irreversible one is asked every time.
	needScope
)

// The keys, by the name each is referred to by. They are constants rather than
// literals at the call sites for the reason every key on this surface is: a
// comparison against a literal is a key nothing can find when it moves.
const (
	questionEnterKey   = "enter"
	questionLaterKey   = "esc"
	questionOpenKey    = "O"
	questionCommentKey = "o"
	questionNoteKey    = "c"
	questionCompareKey = "x"
	questionAskBackKey = "?"
	questionDecideKey  = "d"
	questionDialKey    = "D"
	questionRuleKey    = "r"
	questionUndoKey    = "u"
	// questionScopeKey cycles HOW LONG the answer lasts (questionscope.go). It
	// is `t`, the initial of what it changes, and it is a letter a sentence can
	// start with, so it waits for the block to be aimed at like every other.
	questionScopeKey = "t"
	questionBlankKey = "tab"
	// questionToggleKey IS `space` AND NOT `" "`, which the table's own contract
	// demands: "key is the key as bubbletea spells it, which is what a comparison
	// against tea.KeyPressMsg.String() has to match" — and that library spells a
	// space bar `space`, whichever way the press arrives. It was the byte until
	// the room became the first form to route this key, and a comparison against
	// the byte matched nothing at all.
	questionToggleKey = "space"
	// questionTabBackKey is the left arrow ALONE, on the review of a set: the
	// review is the last tab, so there is nowhere to its right and `←` is not
	// half of a pair there. The routing reads `left` ([app.questionReviewKey]);
	// this is the SPELLING.
	questionTabBackKey = "←"
	// questionBackKey is the up arrow ALONE, which is the one place on this
	// surface a single arrow is a key in its own right: from the box at the foot
	// of a panel there is nowhere below to go, so `↑` is not half of a pair. The
	// routing reads `up` ([app.questionOtherKey]); this is the SPELLING.
	questionBackKey = "↑"
	// questionWalkKey is the PAIR of arrow keys, and it is spelled as the pair
	// because that is how it is drawn and how it is learnt: `←→ pick` is one
	// affordance, and a row that listed two keys for one act would be a row
	// teaching arithmetic instead of a choice. The routing reads `left` and
	// `right` individually ([app.questionOptionKey]); this is the SPELLING.
	questionWalkKey = "←→"
	// questionWalkDownKey is the same walk spelled for a card, whose answers
	// stand in a column.
	questionWalkDownKey = "↑↓"
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
	// questionDetailKey and questionAnswersKey are the page's two side arrows
	// SPELLED ALONE, for [questionBackKey]'s reason: on a page of two panes `→`
	// goes into the evidence and `←` comes back out, which are two different
	// acts rather than one pair. The routing reads `right` and `left`.
	questionDetailKey  = "→"
	questionAnswersKey = "←"
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
	// THE WALK COMES FIRST, because every drawing the owner picked reads its
	// keys in the order a hand uses them: choose, then take it, then put it
	// off (docs/design/questions/picked — `↑↓ choose · enter take it · esc
	// later`). It is given up early all the same; the order is not the rank.
	//
	// THE POINTER'S KEYS ARE SPELLED THE WAY THE FORM LAYS ITS ANSWERS OUT:
	// across on a line, down on a card. Both pairs walk on both.
	// Both are given up early: the arrows work whether or not the row names
	// them, and the band on the pointed answer already says there is a
	// pointer — a row that kept `choose` and lost the line form for it would
	// have spent the small shape on its own legend.
	{key: questionWalkKey, word: "choose", forms: formsLine, needs: needWalk, tier: keyPrimary, giveUp: 2},
	// AND THE ROOM SPELLS ITS WALK DOWNWARDS BECAUSE ITS ANSWERS STAND IN A
	// COLUMN OF SECTIONS. It said `←→ choose` and neither key moved anything on
	// it — the pair that walks its sections is `↑↓`, and the side arrows there
	// open a section and fold it (questionroom.go). A row that names the keys
	// that do nothing is worse than a row that names none: the owner pressed
	// them, 2026-09-10, and reported "no arrow or click".
	{key: questionWalkDownKey, word: "choose", forms: formsCard | formsRoom | formsGroup, needs: needWalk, tier: keyPrimary, giveUp: 2},
	// ON A SET `←→` MOVE BETWEEN QUESTIONS, and its edge says so first
	// ([app.questionTabRows] puts it there), because it is the one thing a set
	// does that a panel of one does not. It is never given up: a set whose edge
	// lost it would be tabs nobody knew how to reach.
	{key: questionWalkKey, word: questionTabWord, forms: formsTabs, tier: keyPrimary},
	// `enter` IS NOT ON A RATIFY LINE. Nothing is waiting on it — the work
	// already happened — so there is no pick for enter to take, and a line that
	// offered `enter take it` beside `u undo` named a key that answers nothing.
	{key: questionEnterKey, word: "take it", forms: formsLine | formsCard | formsRoom | formsGroup, needs: needPick, tier: keyPrimary, giveUp: 1},
	// THE REVIEW'S TWO ARE WHAT A REVIEW IS FOR: send what is held, or go back
	// and change it. Neither is ever given up — a review whose edge lost `send`
	// is a set nobody can finish.
	{key: questionEnterKey, word: "send", forms: formsReview, tier: keyPrimary},
	{key: questionTabBackKey, word: "back", forms: formsReview, tier: keyPrimary},
	// AND NEITHER IS `esc later`: a line that says what was already done is not a
	// question to come back to.
	{key: questionLaterKey, word: "later", forms: formsLine | formsCard | formsRoom | formsReview | formsGroup, tier: keyPrimary},
	// THE TWO KEYS OF THE `something else…` ROW. They are rows of their own
	// rather than second words on `enter` and the walk, because a key that reads
	// one way on one row and another way on the next is two keys as far as
	// anybody learning it is concerned — and while this row is a box, these two
	// and `esc` are the only keys that are not typing.
	{key: questionEnterKey, word: "send it", forms: formsCard, needs: needOther, tier: keyPrimary},
	{key: questionBackKey, word: "back to the list", forms: formsCard, needs: needOther, tier: keyPrimary},
	{key: questionOpenKey, word: "open full", forms: formsLine | formsCard, needs: needRoom, giveUp: 3},
	{key: questionCommentKey, word: "other", forms: formsLine | formsCard | formsRoom, needs: needWords, giveUp: 5},
	{key: questionNoteKey, word: "note", forms: formsRoom, needs: needWords, giveUp: 5},
	{key: questionNoteKey, word: "change", forms: formsRatify, needs: needWords, giveUp: 5},
	{key: questionCompareKey, word: "compare", forms: formsRoom, needs: needCompare, giveUp: 4},
	// `?` IS ON THE ROW AS WELL AS THE PANEL. Asking the asker back is a way of
	// answering any question that takes words, and a question drawn as one row is
	// still a question somebody may not understand — the row gave `c change` and
	// withheld `? ask back`, which is half a door.
	{key: questionAskBackKey, word: "clarify", forms: formsLine | formsCard | formsRoom, needs: needWords, giveUp: 4},
	{key: questionDecideKey, word: "you decide", forms: formsCard | formsRoom, needs: needHands, giveUp: 4},
	{key: questionDialKey, word: "decide these from now on", forms: formsCard | formsRoom, needs: needDial, giveUp: 7},
	// THE RULE OFFER IS THE LAST THING GIVEN UP AFTER THE WAY OUT, because it
	// is the only key here that is on the row ONCE: the third same-shaped yes
	// happens once, and a row that dropped it to keep `[c] change` would have
	// spent the offer on nothing. `u` is beside it for the same reason — a
	// ratify line whose undo is off the row is a ratify line with no undo.
	// HOW LONG THE ANSWER LASTS sits beside the rule offer, because the two are
	// the same instinct at two strengths — "not this again" now, and "write it
	// down" — and it is given up late for the same reason: a frame that drew the
	// lifetimes and no key to change them would be a row nobody can work.
	{key: questionScopeKey, word: "how long", forms: formsBlock | formsRoom, needs: needScope, giveUp: 2},
	{key: questionRuleKey, word: "make it a rule", forms: formsBlock | formsRoom, needs: needRule, giveUp: 2},
	{key: questionUndoKey, word: "undo", forms: formsRatify | formsRoom, needs: needUndo, giveUp: 2},
	// `tab` IS THE BLANKS SHAPE'S OWN VERB AND IS RANKED WITH THE ANSWERS, for
	// the checklist's `space` law said one shape over: a sentence with holes
	// whose foot kept `[c] change` and `[x] compare` and gave up `[tab] next
	// blank` is a form nobody can walk. It was ranked sixth — the same rank
	// that hid `tick it` at a hundred columns — so the e2e gallery width
	// (questionCols) and every ordinary room drew a blanks page with no key
	// that moves between the holes. Ranked zero beside `a`/`b` and `←→ move
	// it`; offered only where there is more than one hole ([needBlanks]).
	{key: questionBlankKey, word: "next blank", forms: formsRoom, needs: needBlanks, tier: keyPrimary},
	// `space` IS THE CHECKLIST'S ANSWER AND IS RANKED WITH THE ANSWERS, which is
	// this table's own law read on the one shape that was breaking it: "The
	// options are never dropped at all: an offer with an answer missing is an
	// offer that hides an answer" ([app.questionKeyRow]). Every other shape's
	// answer key is already ranked zero — `a`/`b` on a pair, `←→` on a dial —
	// and this one was ranked sixth, so at a hundred columns a checklist gave up
	// the only verb that works it and drew as an ordinary list. Measured on a
	// real screen: four answers, no marks anybody could act on, and `[c] change`
	// kept in its place.
	{key: questionToggleKey, word: "tick it", forms: formsCard | formsRoom, needs: needChecklist, tier: keyPrimary},
	// A CHECKLIST IN THE CARD SENDS ON ENTER, once something is ticked. The
	// card's rows carry the ticks themselves now, so the block is where a
	// checklist is answered and not only where it is read; the row is offered
	// only when there is something to send, because `enter` over an empty
	// checklist would send nothing and say it sent.
	{key: questionEnterKey, word: "send what is ticked", forms: formsCard, needs: needTicked, tier: keyPrimary},
	// `tab` WALKS THE POINTER ON THE CARD, and it is on the row because a key
	// that moves a mark nobody was told about is a key nobody presses; it is
	// given up with `open it`, well after the verbs that change the question.
	{key: questionBlankKey, word: "next row", forms: formsCard, needs: needChecklist, giveUp: 3},
	// The room's own four. `a` is the one collision in the grammar — "take its
	// suggestion" on a checklist and "the first one" on a pair — and it is two
	// rows here rather than one key with two words, because the offer row prints
	// the word and the row a person reads may not be ambiguous even where the
	// key is. Which of the two is live is decided by which shape is on screen,
	// and the needs below are what decide it.
	{key: questionSuggestKey, word: "take its suggestion", forms: formsRoom, needs: needChecklist, giveUp: 5},
	{key: questionOrderKey, word: "order them", forms: formsRoom, needs: needOrdered, giveUp: 7},
	{key: questionPairAKey, word: "the first", forms: formsRoom, needs: needPairs, tier: keyPrimary},
	{key: questionPairBKey, word: "the second", forms: formsRoom, needs: needPairs, tier: keyPrimary},
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
	{key: questionWalkKey, word: "move it", forms: formsCard | formsRoom, needs: needMoves, tier: keyPrimary},
	// THE PAGE'S SIDEWAYS WALK (questionpage.go). `→` hands the arrows to the
	// evidence beside the list, so a diagram taller than the page is read to its
	// end; while they are the pane's, `↑↓` scroll it and `←` hands them back.
	// They are rows of their own rather than second words on the walk for the
	// reason [needMoves] states: `choose` moves a pointer and `scroll` moves a
	// window, and a row that said one while the keys did the other is the defect
	// a key row exists to prevent. `→` is offered only where there IS a pane
	// beside the list — a page of one column unfolds the evidence under the
	// pointer and has nothing to move into.
	{key: questionDetailKey, word: "detail", forms: formsRoom, needs: needBeside, tier: keyPrimary, giveUp: 3},
	{key: questionWalkDownKey, word: "scroll", forms: formsRoom, needs: needReading, tier: keyPrimary},
	{key: questionAnswersKey, word: "back to the answers", forms: formsRoom, needs: needReading, tier: keyPrimary},
}

// ── the one key row ─────────────────────────────────────────────────────────

// questionKeyRow composes one row of keys at a width and paints it: the key in
// the payload hue, its word dim, joined by the surface's own separator —
// `↑↓ choose · enter take it · esc later`.
//
// IT IS THE ONE COMPOSER, and before this there were four. The block's answers
// row painted the words bold and the keys plain (an off-by-one in its own
// painter); the page's foot painted keys in the question hue and words dim; the
// batch's row painted both in the question hue; and the rule above the block
// spelled the same keys a third way, in cyan. Four paintings of one grammar is
// four things to learn, and three of them were wrong about which cell a person
// scans for.
//
// THE KEY IS THE PAYLOAD AND THE VERB IS PROSE (docs/DESIGN-LANGUAGE.md's THE
// PAYLOAD RULE: "the hint slot lifts the key and never the verb beside it"), so
// the key steps up to `data` and the word stays dim. The brackets are gone with
// the four painters: `[enter] take it` spent three cells saying what the hue
// already says.
//
// It degrades the way every row of keys on this surface degrades — by dropping
// the least valuable key until what is left fits ([questionDropVerb]) — and a
// row with nothing left that fits is cut rather than dropped, because a person
// who cannot see `esc` is a person with no way off the question.
func (a *app) questionKeyRow(q questionShown, keys []questionVerb, width int) string {
	for {
		row := a.paintQuestionKeys(q, keys)
		if ansi.StringWidth(questionKeyWords(q, keys)) <= width {
			return row
		}
		dropped, ok := questionDropVerb(keys)
		if !ok {
			return fit(row, width)
		}
		keys = dropped
	}
}

// questionKeyWords is that row unpainted, which is what measures it.
func questionKeyWords(q questionShown, keys []questionVerb) string {
	parts := make([]string, 0, len(keys))
	for _, verb := range keys {
		parts = append(parts, questionKeySpelling(verb.key)+" "+questionVerbWord(q, verb))
	}
	return strings.Join(parts, questionKeyGap)
}

// paintQuestionKeys is [questionKeyWords] with the payload rule applied.
func (a *app) paintQuestionKeys(q questionShown, keys []questionVerb) string {
	var b strings.Builder
	for i, verb := range keys {
		if i > 0 {
			b.WriteString(a.pal.dim(questionKeyGap))
		}
		b.WriteString(a.pal.data(questionKeySpelling(verb.key)))
		b.WriteString(a.pal.dim(" " + questionVerbWord(q, verb)))
	}
	return b.String()
}

// questionKeyGap is the separator between two keys on a row, spelled once
// because three rows use it and the manual quotes it.
const questionKeyGap = " · "

// questionVerbWord is one key's word on THIS question: the table's word, with
// the two the question itself decides written in — how far a rule would reach,
// and what `esc` actually does on a confirmation.
func questionVerbWord(q questionShown, verb questionVerb) string {
	switch verb.key {
	case questionCommentKey:
		if q.question.Kind == session.QuestionStanding {
			// THE CORRECTION BUTTON SAYS WHAT IT IS. A rule's is about where
			// the rule reaches. The head is the kind, which is how this row
			// knows without a second copy of the words.
			if q.question.Head == session.StandingHeadRule {
				return session.StandingChangeWord(standing.Item{When: standing.When{Kind: standing.WhenHold}})
			}
			return standChangeWord
		}
		if q.question.Ask == session.AskRatify {
			return "change"
		}
	case questionRuleKey:
		return questionRuleWord(q.question)
	case questionLaterKey:
		return questionLaterWord(q.question)
	}
	return verb.word
}

// questionKeysOnTier is the keys of one tier, in the table's order.
func questionKeysOnTier(keys []questionVerb, tier keyTier) []questionVerb {
	out := make([]questionVerb, 0, len(keys))
	for _, verb := range keys {
		if verb.tier == tier {
			out = append(out, verb)
		}
	}
	return out
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
// The owner assigned `alt+a` to approvals and moved this door to `alt+y`.
// It still works on every page, including while the draft has text in it.
const questionChipKey = "alt+y"

// questionAnswerKeys is which keys a question of this shape actually offers, in
// the table's order, with the emptiness law applied.
//
// It is what the offer row prints and what [app.questionKey] routes against —
// ONE reading, so a key drawn and a key taken cannot come apart.
func (a *app) questionAnswerKeys(q questionShown, form questionForms) []questionVerb {
	out := make([]questionVerb, 0, len(questionKeys))
	// WHILE THE LAST ROW IS A BOX, EVERY LETTER TYPES. `c`, `d`, `?` and the
	// rest are letters, and a row that went on offering them while somebody was
	// writing an answer would be promising keys that put a `d` in their sentence
	// — which is the modal trap this block exists not to have. What is left is
	// the two keys that row draws and the way out.
	if a.questionOthering(q) {
		for _, verb := range questionKeys {
			if !verb.forms.holds(form) {
				continue
			}
			if verb.needs == needOther || verb.key == questionLaterKey {
				out = append(out, verb)
			}
		}
		return out
	}
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
	case needOther:
		return a.questionOthering(q)
	case needPick:
		// THE `something else…` ROW HAS NOTHING FOR `enter take it` TO TAKE: what
		// enter does there is send the words, which is [needOther]'s own row.
		if a.questionOthering(q) {
			return false
		}
		// A CHECKLIST HAS NO PICK TO TAKE: `enter` sends what is ticked
		// ([app.questionTickKey]), and a row saying `take the pick` beside
		// `send what is ticked` is two promises on one key. The asker's
		// suggestion is a word on its row instead ([app.questionPanelOption]).
		if q.question.Input.Kind == session.InputChecklist {
			return false
		}
		// EVERY QUESTION WITH ANSWERS HAS A POINTER AND ENTER TAKES IT
		// ([questionPointerStart]); a question with none written down offers
		// enter only when the asker recommended something.
		if room := a.questionPageOf(q); room != nil {
			// THE PAGE'S ENTER SENDS WHAT ITS FOOT SAYS ([app.questionRoomSends]),
			// and is offered only where that is something. A shape is sent
			// whole when it reads right, and says so in the foot instead.
			if room.input.kind == session.InputNone {
				keys, words := a.questionRoomSends()
				return len(keys) > 0 || words != ""
			}
			return len(room.picked) > 0 || (q.question.Pick != nil && strings.TrimSpace(q.question.Pick.Key) != "")
		}
		if q.question.Input.Kind == session.InputText {
			// A question answered in words has nothing for enter to take while
			// the box is empty AND nothing stands under the pointer — the same
			// give-up [app.questionEnter] makes ([#1506]): a correction or a
			// standing card still carries answers the arrows walk, so a
			// pointer on one is enter taking it.
			if q.question.Pick != nil && strings.TrimSpace(q.question.Pick.Key) != "" {
				return true
			}
			// A PICK LIVES ON A CHOICE. A connect question keeps one answer, the
			// way out, and its enter means the words — the offer row says so.
			if q.question.Ask != session.AskChoice && q.question.Ask != session.AskJudgement {
				return false
			}
			return q.pick >= 0 && q.pick < len(q.question.Options)
		}
		if len(q.question.Options) > 0 {
			return true
		}
		return q.question.Pick != nil && strings.TrimSpace(q.question.Pick.Key) != ""
	case needRule:
		return q.rule
	case needScope:
		return len(questionScopes(q.question)) > 0
	case needUndo:
		return q.undoable
	case needRoom:
		return questionHasMore(q.question)
	case needWords:
		return questionTakesWords(q.question)
	case needDial:
		return q.question.Kind != "" && q.question.Asker.Kind != session.AskerSurface &&
			a.questionOffers(q, needHands)
	case needBeside:
		room := a.questionPageOf(q)
		return room != nil && !room.reading && room.focus < len(q.question.Options) &&
			a.questionRoomBeside(a.bodyWidth())
	case needReading:
		room := a.questionPageOf(q)
		return room != nil && room.reading
	case needWalk:
		// WHILE THE ARROWS ARE THE EVIDENCE PANE'S they scroll it, and the row
		// that says so is [needReading]'s.
		if room := a.questionPageOf(q); room != nil && room.reading {
			return false
		}
		// Every question with answers has the pointer ([questionPointerStart]);
		// a checklist walks its own ticks with the same keys and says so on
		// its `next row` line instead, and where `←→` move a hole
		// ([needMoves]) the row says that — one key, one meaning — while the
		// vertical pair still walks the pointer.
		if q.question.Input.Kind == session.InputChecklist || a.questionOffers(q, needMoves) {
			return false
		}
		return len(q.question.Options) > 1
	case needHands:
		return !questionHandsOnly(q.question)
	case needChecklist:
		return q.question.Input.Kind == session.InputChecklist
	case needTicked:
		if q.holes.kind != session.InputChecklist {
			return false
		}
		for _, ticked := range q.holes.ticks {
			if ticked {
				return true
			}
		}
		return false
	case needBlanks:
		return q.question.Input.Kind == session.InputBlanks && len(q.question.Input.Blanks) > 1
	case needOrdered:
		// AN ORDER KEY IS OFFERED ONLY WHERE THERE IS AN ORDER TO CHANGE. Two
		// rows have one arrangement and no second one, so the key would move a
		// list a person cannot see move.
		return q.question.Input.Kind == session.InputChecklist && len(q.question.Options) > 2
	case needPairs:
		return q.question.Input.Kind == session.InputPairs
	case needOptions:
		return len(q.question.Options) > 0
	case needCompare:
		axes, _ := questionAxes(q.question)
		return len(axes) > 0
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

// questionPageOf is the page, where the page is open over THIS question, and nil
// everywhere else — a condition about the page's pointer is a condition about
// no other drawing of the question.
func (a *app) questionPageOf(q questionShown) *questionRoom {
	if room := a.qroom; room != nil && room.head.token() == q.token() {
		return room
	}
	return nil
}

// questionHandsOnly reports whether this is a decision NOBODY BUT A PERSON MAY
// EVER MAKE.
//
// It is stop.go's law widened to every question of that shape: "There is no
// bypass key, no modifier that skips the question, and no don't-ask-me-again."
// A confirmation is asked because the act cannot be taken back, so handing it to
// the asker would be the surface deciding an irreversible thing on somebody's
// behalf — which is exactly what the question exists to prevent.
//
// AND A PERMISSION THAT IS NOT CHEAP TO TAKE BACK IS ONE OF THEM. The approval
// gate asks because a policy said a person has to see this call; `you decide` on
// it would give that decision straight back to the thing the gate was put in
// front of, which is the gate answering itself with one keystroke.
// docs/design/questions/DESIGN.md's kind table says the same about the clock — a
// permission may act on its own "only when Stakes == reversible" — and a key
// that skips a question is a clock a person wound by hand.
//
// TWO READERS AGREE THROUGH IT, which is why it is a function and not two
// conditions: `d`/`D` are refused on these ([needHands]) and the pointer opens
// on the answer that loses nothing ([questionPointerStart]). They are the same
// claim about the same question — nobody may answer this but the person — and a
// surface that read it twice would one day offer to decide a gate its own
// pointer was standing clear of.
func questionHandsOnly(q session.Question) bool {
	if q.Ask == session.AskConfirmation || q.Stakes == session.StakesIrreversible {
		return true
	}
	if q.Ask == session.AskPermission {
		return q.Stakes != session.StakesReversible
	}
	return false
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

// questionOwnsBox reports whether a sentence already in the box, sent with
// `enter`, is an ANSWER TO THIS QUESTION rather than a message to the
// conversation.
//
// THE DEFAULT IS THAT IT IS NOT. A question the engine asks leaves the box alone
// — the letters are the person's, the question waits, and enter sends the
// sentence — because a block that swallowed every draft would make it impossible
// to say anything while a question was open. Two shapes are the exception, and
// both of them are the same fact said twice:
//
//   - A QUESTION HOLDING THE TURN HAS NOWHERE ELSE FOR THE SENTENCE TO GO. The
//     conversation cannot move until it is answered, so a sentence sent at it
//     would sit in the composer unread.
//   - A QUESTION THAT ASKED FOR WORDS OWNS THE BOX BY SAYING SO. [session.
//     InputText] is the asker stating that the answer IS a sentence — a standing
//     card's correction ("make it 2pm"), a connect key, a running sub-harness's
//     own question — and the box under the question is the answer lane the whole
//     block is built on. The sub-harness lane is why this is not simply
//     `Blocking.Turn`: its question blocks a TASK and not the turn, and its only
//     answer is words, so the one thing it can be answered with used to go to the
//     conversation instead.
func questionOwnsBox(q session.Question) bool {
	if !questionTakesWords(q) {
		return false
	}
	return q.Blocking.Turn || q.Input.Kind == session.InputText
}

// ── THE BOX KEEPS THE FIRST LETTER ──────────────────────────────────────────
//
// WHAT WAS MEASURED, on dev@333acc67d. A question is on the block, the box
// under it is empty, and the person begins their next sentence: `do the schema
// first` handed the call back to the asker on its first letter
// ([questionDecideKey]) and left `o the schema first` in the box. `change the
// column instead` opened the change prompt on its `c`. `remove the old rows`
// wrote a rule. None of those keys was aimed at the block; each was the first
// character of a sentence, and what stayed in the box was the rest of it.
//
// `o other` AND `? clarify` ARE EXPLICIT TEXT DOORS AND WORK IMMEDIATELY.
// They open text entry rather than deciding anything. Other letter commands
// retain the aiming rule below; an existing draft keeps all its letters.
//
// THE RULE FOR THOSE OTHER COMMANDS, AND IT IS WRITTEN HERE BECAUSE THIS IS THE TABLE THE KEYS IT IS
// ABOUT LIVE IN: A VERB ON THIS TABLE BELONGS TO THE BOX UNTIL THE QUESTION HAS
// THE HAND. `d`, `c`, `o`, `r`, `u`, `x`, `?`, `D`, `s`, `g` are every one of
// them the first letter of a word somebody types into a chat box, and the block
// does not take one from a person who has not yet aimed at it.
//
// AIMING IS A KEY THAT COULD NEVER BE TEXT, or a click on the block's own rows:
// `↑↓` choosing, `tab` walking, `enter` taking, `esc` putting it off, the mouse
// landing on an answer. That is the owner's own grammar for this block read in
// the order a hand uses it — choose, then take it, then put it off
// (docs/design/questions/picked) — and every one of those gestures is somebody
// looking at the question rather than at the box.
//
// AND THE ANSWERS' OWN KEYS ARE NOT ON THIS ROAD. `1`-`9`, and the letters a
// lane fixes on an option itself (task-states' `a`, `n`, `s`), NAME an answer
// that is drawn on the row in front of the person — `[1] allow once` is a
// promise this surface makes in ink, and a key that is drawn as pressable and
// is not pressable is a worse defect than the one this rule closes. They answer
// the moment the row is on screen, exactly as they always did. The verbs are
// the half where the harm is and the drawing costs nothing: a row still says
// `[d] you decide`, and the first `d` of a sentence goes where the sentence is
// going.
//
// A QUESTION THE PERSON THEMSELVES RAISED IS EXEMPT, and that is not a special
// case but the same rule from the other side: the stop card and the tab-close
// card are put up BY a keystroke, so the hand is already on them, and both have
// taken the whole keyboard while they were up since before this block existed
// (stop.go, tabclose.go).
//
// AND THE HAND IS PER QUESTION. It is given up when the question is answered,
// put off, or replaced by the next one — so the first letter into an empty box
// is the box's again for every question in turn, rather than once per session.

// questionAimKey reports whether a key aims at the block: a key that is not a
// character a sentence could carry, pressed while the block is up.
func questionAimKey(key string) bool {
	switch key {
	case "up", "down", "left", "right", "tab", "shift+tab", questionEnterKey, questionLaterKey:
		return true
	}
	return false
}

// questionTextKey reports whether a key is one a sentence could start with —
// every key that arrives as a single character.
//
// `space` is deliberately not one: bubbletea spells it `space` rather than
// `" "`, nobody begins a sentence with it, and it is the checklist's tick.
func questionTextKey(key string) bool {
	return len([]rune(key)) == 1
}

// questionHasTheHand reports whether the block may take a verb that could have
// been the first letter of a sentence.
//
// THE HAND IS THE TOKEN IT WAS AIMED AT, and that is the whole of why there is
// no place that gives it back. A flag had to be dropped everywhere a question
// could leave the front — answered, folded, withdrawn, replaced by the next one
// in the queue — and every site that was missed was a question inheriting a
// keyboard aimed at a different one: `esc` on the first of two folded it and
// left the second holding the hand, so the next `d` decided a question nobody
// had looked at. A token cannot be inherited: it either names the question in
// front or it names one that is not.
func (a *app) questionHasTheHand(q session.Question) bool {
	if questionRaisedHere(q) {
		return true
	}
	return a.questionHand != "" && a.questionHand == questionTokenOf(q)
}

// aimQuestion is the person looking at one question rather than at the box.
func (a *app) aimQuestion(token string) { a.questionHand = token }
