package tui3

import (
	"cmp"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── SEVERAL QUESTIONS FROM ONE STEP ARE ONE PANEL ───────────────────────────
//
// A model that calls `ask` three times in one batch, or a batch of four reads
// that each need a permission, puts several questions on somebody's screen in
// the same instant. They are one moment to the person, and they used to be a
// queue: the first drawn, `2 more` under it, no way back to one already
// answered, and every answer sent the moment its key was pressed.
//
// THE SET IS THE ENGINE'S STEP, AND NOTHING ELSE. [session.Question.Batch] is
// minted once per step of a turn and stamped on every question raised from
// inside it, so "these belong together" is a fact the engine states rather than
// a guess this surface makes. It replaced two guesses at once: the SHEET, which
// held quiet questions back until "the model spoke again" (a proxy this surface
// had to invent because the engine had no step to name) and then drew them as a
// list with keys of its own, and the queue every blocking question from one
// batch still stood in. Both are deleted; the set is read off the questions.
//
// ── THE TWO DRAWINGS, EXACTLY AS THEY ARE DRAWN ─────────────────────────────
//
// TABS (owner pick `multi — A`): one panel, a tab per question across the top
// edge and a review tab after them. `←→` move between questions, `↑↓` walk the
// answers of the one on screen, and `enter` takes an answer FOR THIS TAB — it is
// held, and the panel moves on to the next question still waiting.
//
//	╭─ ● storage   ○ naming   ○ tests   ✓ review ───────────────── 1 of 3 ─╮
//	│                                                                      │
//	│ ? Which storage for the session index?                               │
//	│                                                                      │
//	│ ▸ 1  SQLite        one file beside the conversation   ◆ recommended  │
//	│   2  JSONL         append-only, no new dependency                    │
//	│   3  something else…                                                 │
//	│                                                                      │
//	╰─ ←→ question · ↑↓ choose · enter take it · esc later ────────────────╯
//
//	╭─ ✓ storage   ✓ naming   ○ tests   ● review ───────────────── review ─╮
//	│                                                                      │
//	│ ✓ storage       SQLite                                               │
//	│ ✓ naming        session-index.db                                     │
//	│ ○ tests         not answered — ← to go back                          │
//	│                                                                      │
//	│ ▸ send the 2 answered · 1 stays open                                 │
//	│                                                                      │
//	╰─ enter send · ← back · esc later ────────────────────────────────────╯
//
// ONE PERMISSION FRAME (the owner's approved "group permissions"): where every
// question in the set is a permission with a plain grant and an answer that
// loses nothing, the set is ONE decision about several calls, and it is asked
// as one. `one by one` opens the same set as the tabs above.
//
//	╭─ ? allow these 4? ──────────────────────────────────────────── read ─╮
//	│ read  internal/session/store.go                                      │
//	│ read  internal/session/loop.go                                       │
//	│ read  internal/session/agent.go                                      │
//	│ read  go.mod                                                         │
//	│                                                                      │
//	│ ▸ 1  allow all 4                                                     │
//	│   2  one by one                                                      │
//	│   3  deny all                                          safe answer   │
//	╰─ ↑↓ choose · enter take it · esc later ──────────────────────────────╯
//
// The pointer above stands on `allow all 4` because four reads are ordinary
// calls: it opens where the members' own pointers would ([questionGroupStart]),
// which the gate's grade decides (#953). A frame holding one call nobody graded
// opens on `deny all` instead. `deny all` wears `safe answer` either way.
//
// ── THE LAWS, AND WHAT EACH ONE REFUSES ─────────────────────────────────────
//
//   - AN ANSWER IN A SET IS HELD UNTIL THE SET IS SENT, AND ONLY A KEY ON THE
//     PANEL HOLDS ONE. Every key a tab takes goes through the block's own
//     routing for the question on screen — the pointer, the digits, the
//     `something else…` row, the second beat, the lifetime row — and what that
//     routing would have sent is held on the question instead
//     ([questionShown.staged]). The hold is lent to the block for the length of
//     ONE keystroke ([app.questionStaging]), so an answer from home, from a
//     second window or from the project's own rule while nobody is here goes
//     straight through the door exactly as it always did.
//   - THE REVIEW SENDS THROUGH THE ONE DOOR, IN TAB ORDER, IN ONE COMMAND
//     ([app.sendAnswers]). A question still unanswered stays open and says so;
//     nothing is delegated to a pick the person did not press.
//   - `esc` IS LATER FOR THE WHOLE SET. It folds every tab to the one rule the
//     block folds a question to, and whatever was held stays held.
//   - AN IRREVERSIBLE QUESTION NEVER JOINS. It keeps its own frame with the
//     pointer on the answer that loses nothing, and neither a review nor an
//     `allow all` may carry it along with its neighbours ([app.questionJoinsSet]).

// The set's own words, spelled once and quoted by the manual.
const (
	// questionReviewWord is the last tab, and the top edge's aside while it is
	// the one on screen.
	questionReviewWord = "review"
	// questionTabWord is `←→` on a tab: it moves to another QUESTION, which is
	// the one thing those two keys do on this panel.
	questionTabWord = "question"
	// questionOfWord joins the tab on screen to the count, in the aside: `1 of 3`.
	questionOfWord = " of "
	// questionNotAnsweredWord is a review row whose question has no answer held.
	// It says where the answer is given, because a review a person cannot act
	// on from is a list of complaints.
	questionNotAnsweredWord = "not answered — ← to go back"
	// questionResumesWord is what sending everything does, and it is said only
	// where it is true: every question in the set is holding the turn.
	questionResumesWord = "the turn resumes with every answer"
	// questionAllowAllWord, questionApartWord and questionDenyAllWord are the
	// permission frame's three answers.
	questionAllowAllWord = "allow all "
	questionApartWord    = "one by one"
	questionDenyAllWord  = "deny all"
	// questionTimesWord counts one thing wanted that several questions of the
	// frame ask for alike, as `ask  ×3`.
	questionTimesWord = "  ×"
)

// questionReviewTab is the review tab's place in [questionSetState.focus]. It
// can never be a question's token, which is always a lane and an id.
const questionReviewTab = "review"

// questionSendAt is the review's send row among its pressable rows, which are
// otherwise the set's questions by index.
const questionSendAt = questionBandBack - 1

// The permission frame's rows, in the order they are drawn and numbered.
const (
	questionGroupAllow = iota
	questionGroupApart
	questionGroupDeny
	questionGroupRows
)

// questionSetState is what this window remembers about the set on screen that
// the questions themselves cannot carry: which tab is showing, where the
// permission frame's pointer stands, and whether the person asked to go one by
// one.
//
// IT IS KEYED BY THE STEP, so a set that has just been sent and a set the next
// step raises can never share a pointer: a state whose batch is not the one on
// screen is read as the zero state ([app.questionSetNow]).
type questionSetState struct {
	batch string
	// focus is the token of the tab showing, [questionReviewTab], or empty for
	// "the first question still waiting".
	focus string
	// pick is the permission frame's pointer, and pickSet whether it has been
	// placed yet (it opens where the members' own pointers would, once).
	pick    int
	pickSet bool
	// apart is `one by one`: the permission frame drawn as the tabs.
	apart bool
}

// ── who is in a set ─────────────────────────────────────────────────────────

// questionJoinsSet reports whether one question may be answered as a tab of
// several. Every rung is a property of the question and states what it refuses.
func (a *app) questionJoinsSet(q questionShown) bool {
	switch {
	case strings.TrimSpace(q.question.Batch) == "":
		// A question raised outside any step has no neighbours — every landing,
		// every fuel gate, anything this surface raised about itself.
		return false
	case !questionWaits(q.question):
		// Nothing waits on a ratify line or an assumptions card, so there is no
		// answer to hold and nothing to send.
		return false
	case q.question.Ask == session.AskConfirmation || q.question.Stakes == session.StakesIrreversible:
		// AN IRREVERSIBLE QUESTION NEVER JOINS. It is asked on its own, with the
		// pointer on the answer that loses nothing, every time.
		return false
	case a.questionOffers(q, needMoves):
		// `←→` move a hole or a dial on such a question, and on a set they are
		// the keys between questions — one key may not mean both on one screen.
		return false
	}
	return true
}

// questionSetOf is the set the front of the queue belongs to: every open
// question that shares its step and may join one, in the order of the rows
// they are about. Fewer than two is no set at all, and the result is nil.
//
// THE TABS READ IN THE TRANSCRIPT'S ORDER, NOT IN THE ORDER THEY ARRIVED. The
// calls of one step are gated side by side, so their questions land in
// whichever order the gates happened to finish in — four reads drawn as
// `e f g h` in the transcript arrived as `g f e h` — and a panel that listed
// them so would have a person answering the third file while looking at the
// first. A question with no row of its own keeps its arrival place after the
// ones that have one.
//
// IT IS READ, NEVER KEPT. A question answered in another window, withdrawn, or
// raised a moment later by the same step changes the set on the next frame
// because the set is nothing but a reading of [app.questions].
func (a *app) questionSetOf(open []questionShown) []questionShown {
	if len(open) < 2 || !a.questionJoinsSet(open[0]) {
		return nil
	}
	batch := open[0].question.Batch
	var set []questionShown
	for _, q := range open {
		if q.question.ClarificationDepth == open[0].question.ClarificationDepth && q.question.Batch == batch && a.questionJoinsSet(q) {
			set = append(set, q)
		}
	}
	if len(set) < 2 {
		return nil
	}
	at := func(q questionShown) int {
		if row := a.questionSubjectAt(q.question); row >= 0 {
			return row
		}
		return len(a.entries)
	}
	slices.SortStableFunc(set, func(x, y questionShown) int { return cmp.Compare(at(x), at(y)) })
	return set
}

// questionSet is the set on the block now, or nil.
func (a *app) questionSet() []questionShown { return a.questionSetOf(a.questionOpen()) }

// questionTabbed reports whether this question is drawn as one tab of a set —
// the chooser's rung (questionchooser.go).
func (a *app) questionTabbed(q questionShown) bool {
	for _, one := range a.questionSet() {
		if one.token() == q.token() {
			return true
		}
	}
	return false
}

// questionSetNow is this window's state for the set on screen, reset to the
// zero state for a set it has not seen before.
func (a *app) questionSetNow(set []questionShown) *questionSetState {
	if len(set) == 0 {
		return &questionSetState{}
	}
	if a.questionSetAt.batch != set[0].question.Batch {
		a.questionSetAt = questionSetState{batch: set[0].question.Batch}
	}
	return &a.questionSetAt
}

// questionSetFocus is which tab of the set is showing: an index into it, or -1
// for the review.
//
// A TAB THAT WENT AWAY IS NOT A TAB THE PANEL STANDS ON. Its question was
// answered somewhere else or withdrawn, so the panel moves to the first question
// still waiting — or to the review, where everything left has an answer held.
func (a *app) questionSetFocus(set []questionShown) int {
	state := a.questionSetNow(set)
	if state.focus == questionReviewTab {
		return -1
	}
	for i, q := range set {
		if q.token() == state.focus {
			return i
		}
	}
	for i, q := range set {
		if q.staged == nil {
			return i
		}
	}
	if state.focus != "" {
		return -1
	}
	return 0
}

// questionSetHead is the question the block is drawing when a set is up: the
// tab on screen, or the set's first question while the review or the permission
// frame is showing (that one's stamp is the set's settle guard).
func (a *app) questionSetHead(set []questionShown) questionShown {
	if a.questionGrouped(set) {
		return set[0]
	}
	if at := a.questionSetFocus(set); at >= 0 {
		return set[at]
	}
	return set[0]
}

// questionSetReviewing reports whether the review tab is the one on screen.
func (a *app) questionSetReviewing(set []questionShown) bool {
	return !a.questionGrouped(set) && a.questionSetFocus(set) < 0
}

// ── the permission frame ────────────────────────────────────────────────────

// questionGrouped reports whether the set is asked as ONE permission frame.
//
// BOTH CLAUSES ARE LOAD-BEARING, and they answer different questions. Asking
// PERMISSION is what lets the frame use permission's words: `allow all 4` and
// `deny all` are a sentence about granting, and two confirmations from one step
// — `stop this task?`, `close this tab?` — would wear it as a lie however their
// answers happen to be shaped. Offering a plain GRANT AND AN ANSWER THAT LOSES
// NOTHING is what lets those two rows do anything: each is that question's own
// answer given in its own keys ([questionGrantAndSafe]), so a question with no
// such pair has nothing for either row to give it.
//
// What is NOT here is any reading of the head or of [session.Question.Kind]: the
// set itself is keyed on the engine's step ([questionSetOf]) and the frame on
// the answers, never on how a question is spelled.
func (a *app) questionGrouped(set []questionShown) bool {
	if len(set) < 2 || a.questionSetNow(set).apart {
		return false
	}
	for _, q := range set {
		if q.question.Ask != session.AskPermission {
			return false
		}
		if _, _, ok := questionGrantAndSafe(q.question); !ok {
			return false
		}
	}
	return true
}

// questionGrantAndSafe is one question's plain grant — the first answer that
// neither loses nothing nor grants more than was asked — and its answer that
// loses nothing, by key.
func questionGrantAndSafe(q session.Question) (grant, safe string, ok bool) {
	for i, option := range q.Options {
		key := questionOptionKeyAt(q, i)
		switch {
		case option.Safe:
			if safe == "" {
				safe = key
			}
		case !option.Widening:
			if grant == "" {
				grant = key
			}
		}
	}
	return grant, safe, grant != "" && safe != ""
}

// questionGroupStart is where the permission frame's pointer opens: on `allow
// all` only where every question's OWN pointer would open on its grant, and on
// `deny all` otherwise. It is [questionPointerStart] read over the set rather
// than a second rule — a set may not make `enter` mean yes where any one of its
// questions, asked alone, would not.
func questionGroupStart(set []questionShown) int {
	for _, q := range set {
		grant, _, _ := questionGrantAndSafe(q.question)
		at := questionPointerStart(q.question)
		if at >= len(q.question.Options) || questionOptionKeyAt(q.question, at) != grant {
			return questionGroupDeny
		}
	}
	return questionGroupAllow
}

// questionSetMarksSafe is [questionMarksSafe] read over the set: the frame marks
// `deny all` only where EVERY member, asked on its own, would mark its own
// refusal.
//
// IT IS THE SAME FOLD AS [questionGroupStart], for the same reason — the frame
// is a way of answering these questions, never a second opinion about them.
// `2 one by one` opens the very same questions as tabs, and a frame that marked
// a row its tabs leave unmarked would change what the surface claims about them
// between one keystroke and the next: a member whose asker picked an answer says
// why the pointer is there with `◆ recommended` on the pick's own row, and a
// reversible call is not a question only a person may answer at all.
func questionSetMarksSafe(set []questionShown) bool {
	for _, q := range set {
		if !questionMarksSafe(q.question) {
			return false
		}
	}
	return len(set) > 0
}

// questionGroupPick is the frame's pointer, placed on first reading.
func (a *app) questionGroupPick(set []questionShown) int {
	state := a.questionSetNow(set)
	if !state.pickSet {
		state.pick, state.pickSet = questionGroupStart(set), true
	}
	return state.pick
}

// A GROUPED PERMISSION CARRIES NO LIFETIME ROW, for the reason a single one
// does not (questionscope.go's [questionScopes]): [session.Answer.Scope] never
// reaches the consent gate, so a row saying `from now on` over four calls would
// promise four times over what it cannot do once. The frame offers `t` to
// nobody.
//
// THIS WAS WRITTEN AS A PROMISE — "the row comes back here with the grading
// (#953) — one row for the set, moved together". The grading landed
// (f3a734ba1), and the row did NOT come back: nothing here changed and no test
// went red, because the only thing guarding the promise was a comment. What is
// owed, and whether it is owed at all, is issue #995; until somebody rules on
// it this says what the code does rather than what a past change hoped for.

// answerQuestionGroup is one of the frame's three rows taken.
//
// `allow all` AND `deny all` ARE EACH QUESTION'S OWN ANSWER, IN ITS OWN KEY,
// through the one door and in one command — with the lifetime each would carry
// had it been answered alone ([questionScopeOf]), so a question answered as one
// of four is the same record as a question answered by itself. `one by one`
// answers nothing: it opens the same set as tabs.
func (a *app) answerQuestionGroup(set []questionShown, row int) tea.Cmd {
	if row == questionGroupApart {
		state := a.questionSetNow(set)
		state.apart, state.focus = true, ""
		a.touch()
		return nil
	}
	all := make([]questionAnswer, 0, len(set))
	for _, q := range set {
		grant, safe, _ := questionGrantAndSafe(q.question)
		key := safe
		if row == questionGroupAllow {
			key = grant
		}
		answer := session.Answer{Key: key, Picked: []string{key}}
		if scope := questionScopeOf(q, key); scope != "" {
			answer.Scope = scope
		}
		all = append(all, questionAnswer{q: q, answer: answer})
	}
	return a.sendAnswers(all)
}

// ── holding and sending ─────────────────────────────────────────────────────

// stageAnswers holds every answer in `all` that belongs to the set a keystroke
// is being routed for, and hands back the rest for the door.
//
// IT HOLDS ONLY WHAT WOULD HAVE SETTLED THE QUESTION. `? ask back` and a
// landing's `tell it` send words while the question stays open
// ([session.AnswerResolves]), so there is nothing for a review to send later
// and they go through now, exactly as they would from a panel of one.
func (a *app) stageAnswers(all []questionAnswer) []questionAnswer {
	if a.questionStaging == "" {
		return all
	}
	rest := all[:0:0]
	staged := ""
	for _, one := range all {
		open := a.questionHeld(one.q.token())
		if open == nil || open.question.Batch != a.questionStaging || !session.AnswerResolves(one.answer) {
			rest = append(rest, one)
			continue
		}
		answer := one.answer
		open.staged = &answer
		// THE POINTER STANDS ON THE ANSWER HELD, so going back to the tab shows
		// what was taken rather than wherever the arrows last were.
		if at, ok := questionOptionAt(open.question, answer.FirstKey()); ok {
			open.pick = at
		}
		staged = open.token()
	}
	if staged != "" {
		a.advanceQuestionSet(staged)
		a.touch()
	}
	return rest
}

// advanceQuestionSet moves the panel on after an answer is held for the
// question `from`: to the next question still waiting after it, round to the
// first, and to the review once every question has an answer held.
func (a *app) advanceQuestionSet(from string) {
	set := a.questionSet()
	if len(set) == 0 {
		return
	}
	state := a.questionSetNow(set)
	at := 0
	for i, q := range set {
		if q.token() == from {
			at = i
		}
	}
	for step := 1; step <= len(set); step++ {
		at := (at + step) % len(set)
		if set[at].staged == nil {
			state.focus = set[at].token()
			return
		}
	}
	state.focus = questionReviewTab
}

// sendQuestionSet is `enter` on the review: every answer held, in tab order,
// through the one door in one command. A question with nothing held stays open.
func (a *app) sendQuestionSet(set []questionShown) tea.Cmd {
	all := make([]questionAnswer, 0, len(set))
	for _, q := range set {
		if q.staged != nil {
			all = append(all, questionAnswer{q: q, answer: *q.staged})
		}
	}
	if len(all) == 0 {
		return nil
	}
	state := a.questionSetNow(set)
	state.focus = ""
	return a.sendAnswers(all)
}

// moveQuestionTab is `←` or `→`: the next tab that way, the review last, and no
// wrapping — a row of tabs somebody is reading along is not a carousel.
func (a *app) moveQuestionTab(set []questionShown, by int) {
	state := a.questionSetNow(set)
	at := a.questionSetFocus(set)
	if at < 0 {
		at = len(set)
	}
	to := at + by
	switch {
	case to < 0 || to > len(set):
		return
	case to == len(set):
		state.focus = questionReviewTab
	default:
		state.focus = set[to].token()
	}
	a.touch()
}

// foldQuestionSet is `esc` on the set: every tab folds, and whatever was held
// stays held for when it is opened again.
func (a *app) foldQuestionSet(set []questionShown) {
	for _, q := range set {
		a.foldQuestion(q)
	}
}

// ── the keyboard ────────────────────────────────────────────────────────────

// questionSetKey routes one key while a set is on the block, and reports
// whether it took it. `head` is [app.questionHead]'s reading, which is the tab
// on screen.
func (a *app) questionSetKey(set []questionShown, head questionShown, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	if key == "ctrl+c" {
		return nil, false
	}
	if a.questionGrouped(set) || a.questionSetReviewing(set) {
		a.holdQuestionClocks()
		if !a.questionSettled(head) {
			// THE SETTLE GUARD, for the set's own rows exactly as for a tab's.
			return nil, true
		}
		var cmd tea.Cmd
		var took bool
		if a.questionGrouped(set) {
			cmd, took = a.questionGroupKey(set, key)
		} else {
			cmd, took = a.questionReviewKey(set, key)
		}
		if took && questionAimKey(key) {
			a.aimQuestion(head.token())
		}
		return cmd, took
	}
	if a.questionSetOwnsKey(head, key) {
		a.holdQuestionClocks()
		if !a.questionSettled(head) {
			return nil, true
		}
		switch key {
		case "left":
			a.moveQuestionTab(set, -1)
		case "right":
			a.moveQuestionTab(set, 1)
		case questionLaterKey:
			a.foldQuestionSet(set)
		}
		a.aimQuestion(head.token())
		return nil, true
	}
	// EVERY OTHER KEY IS THE BLOCK'S OWN ROUTING FOR THE QUESTION ON SCREEN, with
	// the set's hold lent to it for this one keystroke.
	a.questionStaging = head.question.Batch
	defer func() { a.questionStaging = "" }()
	return a.questionKeyOn(head, msg)
}

// questionSetOwnsKey reports whether a key on a tab is the SET's rather than the
// question's: `←→` between questions and `esc` for the whole set.
//
// A TAB THAT IS PART-WAY THROUGH SOMETHING KEEPS ITS OWN KEYS. The second beat
// walks its shapes with `←→` and backs out with `esc`; the `something else…`
// row and a box pointed at the question are caret and text, where `←→` move the
// caret; and with words in the message box every arrow is the box's.
func (a *app) questionSetOwnsKey(head questionShown, key string) bool {
	if key != "left" && key != "right" && key != questionLaterKey {
		return false
	}
	if len(head.beat) > 0 || head.writing != "" {
		return false
	}
	if key == questionLaterKey {
		return true
	}
	if strings.TrimSpace(a.input.String()) != "" || a.questionOthering(head) {
		return false
	}
	return true
}

// questionReviewKey is a key on the review tab.
func (a *app) questionReviewKey(set []questionShown, key string) (tea.Cmd, bool) {
	switch key {
	case questionLaterKey:
		a.foldQuestionSet(set)
		return nil, true
	case "left", "shift+tab", "up":
		a.moveQuestionTab(set, -1)
		return nil, true
	case "right", "tab", "down":
		return nil, true
	case questionEnterKey:
		if strings.TrimSpace(a.input.String()) != "" {
			return nil, false
		}
		return a.sendQuestionSet(set), true
	}
	return nil, false
}

// questionGroupKey is a key on the permission frame.
func (a *app) questionGroupKey(set []questionShown, key string) (tea.Cmd, bool) {
	pick := a.questionGroupPick(set)
	state := a.questionSetNow(set)
	typing := strings.TrimSpace(a.input.String()) != ""
	switch key {
	case questionLaterKey:
		a.foldQuestionSet(set)
		return nil, true
	case "up", "left", "shift+tab":
		if pick > 0 {
			state.pick = pick - 1
			a.touch()
		}
		return nil, true
	case "down", "right", "tab":
		if pick < questionGroupRows-1 {
			state.pick = pick + 1
			a.touch()
		}
		return nil, true
	case questionEnterKey:
		if typing {
			return nil, false
		}
		return a.answerQuestionGroup(set, pick), true
	}
	if typing {
		return nil, false
	}
	if row := questionGroupDigit(key); row >= 0 {
		return a.answerQuestionGroup(set, row), true
	}
	return nil, false
}

// questionGroupDigit is the row a digit names on the frame, or -1.
func questionGroupDigit(key string) int {
	for row := 0; row < questionGroupRows; row++ {
		if key == itoa(row+1) {
			return row
		}
	}
	return -1
}

// questionSetPress resolves a click on the set's own rows — the permission
// frame's three answers and the review's rows — and reports whether the set
// had it. A click on a tab's answers is the block's own ([app.questionPress]).
func (a *app) questionSetPress(set []questionShown, head questionShown, x, y int) (tea.Cmd, bool, bool) {
	grouped, reviewing := a.questionGrouped(set), a.questionSetReviewing(set)
	if !grouped && !reviewing {
		return nil, false, false
	}
	mark, found := a.chromeAt(y)
	if !found || mark.kind != chromeQuestion {
		return nil, false, true
	}
	for _, band := range a.questionBands {
		if band.row != mark.index || !band.span.holds(x) {
			continue
		}
		a.aimQuestion(head.token())
		if grouped {
			return a.answerQuestionGroup(set, band.at), true, true
		}
		if band.at == questionSendAt {
			return a.sendQuestionSet(set), true, true
		}
		a.questionSetNow(set).focus = set[band.at].token()
		a.touch()
		return nil, true, true
	}
	return nil, false, true
}

// ── drawing ─────────────────────────────────────────────────────────────────

// questionSetRows draws the set: the permission frame, the review, or the tab
// on screen.
func (a *app) questionSetRows(set []questionShown, width int) []string {
	// THE SET ARRIVED AS ONE OBJECT, so every question in it is on the screen
	// from the frame it was first drawn on — a tab's words are in the top edge
	// whichever tab is showing.
	for _, q := range set {
		a.markQuestionShown(q.token())
	}
	switch {
	case a.questionGrouped(set):
		return a.questionGroupRows(set, width)
	case a.questionSetReviewing(set):
		return a.questionReviewRows(set, width)
	}
	return a.questionTabRows(set, a.questionSetFocus(set), width)
}

// questionTabRows is one tab: the question's own sentence first, then exactly
// what a panel of one draws under it ([app.questionPanelInside]) — its answers,
// or its beat's shapes where it is part-way through the widening answer — with
// the tabs in the top edge and `←→ question` in the bottom one.
func (a *app) questionTabRows(set []questionShown, at, width int) []string {
	q := set[at]
	inner := frameInner(width)
	room := max(inner-2*len(questionPanelGap), 8)
	rows := []string{""}
	headRows := wrap(strings.TrimSpace(q.question.Head), max(room-2, 8))
	for i, line := range headRows {
		lead := "  "
		if i == 0 {
			lead = a.questionMarkFor(q.question) + " "
		}
		rows = append(rows, questionPanelGap+lead+a.pal.ink(line))
	}
	// The frame's top edge, the blank row and the sentence all stand above what
	// the question itself draws, and the inside records its marks knowing it.
	body, keyRow, keys := a.questionPanelInside(q, width, inner, 1+1+len(headRows), questionSetVerbs(formsTabs))
	rows = append(rows, body...)
	aside := a.pal.dim(itoa(at+1) + questionOfWord + itoa(len(set)))
	panel := framed{
		title: a.questionTabStrip(set, at, frameEdgeRoom(width)-ansi.StringWidth(ansi.Strip(aside))-2),
		aside: aside,
		keys:  keyRow,
	}
	out, _ := panel.draw(a.pal, width, rows)
	if second := a.questionPanelSecond(q, keys, width); second != "" && q.writing == "" {
		out = append(out, second)
	}
	return out
}

// questionReviewRows is the review: every question with the answer held for it,
// and the one row that sends them.
func (a *app) questionReviewRows(set []questionShown, width int) []string {
	inner := frameInner(width)
	room := max(inner-2*len(questionPanelGap), 8)
	labels := a.questionTabLabels(set)
	column := 0
	for _, label := range labels {
		column = max(column, ansi.StringWidth(label))
	}
	column = min(column, room/3)
	rows := []string{""}
	held := 0
	for i, q := range set {
		mark, words := a.pal.dim(a.icon(tokens.GStepPending)), a.pal.dim(questionNotAnsweredWord)
		if q.staged != nil {
			held++
			mark, words = a.pal.dim(a.icon(tokens.GSettled)), a.pal.ink(questionHeldWords(q))
		}
		label := fit(labels[i], column)
		line := mark + " " + a.pal.ink(label) + strings.Repeat(" ", column-ansi.StringWidth(label)+3) + words
		a.questionBands = append(a.questionBands, questionBand{
			row: 1 + len(rows), span: hudSpan{from: 0, to: inner + 2}, at: i,
		})
		rows = append(rows, questionPanelGap+fitPainted(line, room))
	}
	rows = append(rows, "")
	keys := questionSetVerbs(formsReview)
	if held == 0 {
		// A REVIEW WITH NOTHING HELD SAYS SO BY WHAT IS NOT THERE. Every row
		// already reads `not answered — ← to go back` and the send row is absent,
		// so a sentence under them saying the same thing a third time is the
		// emptiness law's "never a line saying a group is empty" — and on a set
		// of two it printed one sentence three times.
		keys = questionDropVerbKey(keys, questionEnterKey)
	} else {
		aside := ""
		if held == len(set) && questionSetHoldsTheTurn(set) {
			aside = a.pal.dim(questionResumesWord)
		}
		a.questionBands = append(a.questionBands, questionBand{
			row: 1 + len(rows), span: hudSpan{from: 0, to: inner + 2}, at: questionSendAt,
		})
		rows = append(rows, a.questionSendRow(questionSendWord(held, len(set)), aside, room), "")
	}
	aside := a.pal.dim(questionReviewWord)
	panel := framed{
		title: a.questionTabStrip(set, -1, frameEdgeRoom(width)-ansi.StringWidth(questionReviewWord)-2),
		aside: aside,
		keys:  a.questionKeyRow(questionShown{}, keys, frameEdgeRoom(width)),
	}
	out, _ := panel.draw(a.pal, width, rows)
	return out
}

// questionSendRow is the review's one answer: the pointer, what it sends, and
// what sending does, on the ground a pointed row stands on. It has no key
// cell, because `enter` is the only key that takes it and the edge says so.
func (a *app) questionSendRow(word, aside string, room int) string {
	line := questionPanelGap + a.pal.warnBold(a.icon(tokens.GPointer)) + " " + a.pal.ink(word)
	if aside != "" {
		if gap := room - ansi.StringWidth(ansi.Strip(line)) - ansi.StringWidth(ansi.Strip(aside)); gap > 1 {
			line += strings.Repeat(" ", gap) + aside
		}
	}
	width := room + 2*len(questionPanelGap)
	if a.questionHovering(questionHoverPanel, questionSendAt) {
		return a.pal.cursor(line, width)
	}
	return a.pal.background(line, width, a.pal.ramp.selected)
}

// questionGroupRows is the permission frame: what each call wants, then the
// three answers, then the one row of lifetimes they all offer.
func (a *app) questionGroupRows(set []questionShown, width int) []string {
	inner := frameInner(width)
	room := max(inner-2*len(questionPanelGap), 8)
	rows := make([]string, 0, len(set)+8)
	tools := make([]string, 0, 2)
	// ONE ROW PER THING WANTED, NOT PER QUESTION. Three `ask` calls with nothing
	// to tell them apart are one row that says `×3`, because three identical rows
	// read as the surface stuttering rather than as three requests.
	var wants []string
	count := map[string]int{}
	for _, q := range set {
		tool, call := a.questionPanelTool(q), a.questionPanelCall(q)
		// A CALL TOO LONG FOR ITS ROW LOSES ITS MIDDLE, never its end: four
		// reads from one folder differ only in the file, and a row cut from the
		// right would draw four copies of the folder.
		callRoom := room
		if tool != "" && call != tool {
			callRoom -= ansi.StringWidth(tool) + 2
		}
		if ansi.StringWidth(call) > callRoom {
			call = rowMiddle(call, callRoom)
		}
		var want string
		switch {
		case tool != "" && call != "" && call != tool:
			want = a.pal.dim(tool) + "  " + a.pal.data(call)
		case call != "":
			want = a.pal.data(call)
		default:
			want = a.pal.ink(strings.TrimSpace(q.question.Head))
		}
		if count[want]++; count[want] == 1 {
			wants = append(wants, want)
		}
		if tool != "" && !questionHasWord(tools, tool) {
			tools = append(tools, tool)
		}
	}
	for _, want := range wants {
		if n := count[want]; n > 1 {
			want += a.pal.dim(questionTimesWord + itoa(n))
		}
		rows = append(rows, questionPanelGap+fitPainted(want, room))
	}
	rows = append(rows, "")
	pick := a.questionGroupPick(set)
	words := [questionGroupRows]string{
		questionAllowAllWord + itoa(len(set)), questionApartWord, questionDenyAllWord,
	}
	for row, word := range words {
		aside := ""
		if row == questionGroupDeny && questionSetMarksSafe(set) {
			// `deny all` IS THE ANSWER THAT LOSES NOTHING, and it says so on the
			// terms its own members would. It is the safe answer by construction
			// — the frame forms only where every member has one
			// ([questionGrantAndSafe]) and the row sends each question its own —
			// and WHETHER THE MARK IS DRAWN is [questionMarksSafe], read over the
			// set exactly as [questionGroupStart] reads the pointer's own rule.
			//
			// NOT UNDER THE POINTER, AND NOT UNCONDITIONALLY EITHER: both were
			// tried and both drifted. Under the pointer was the same sentence
			// only while a permission opened on `deny` for every call (#933), so
			// the mark vanished from every ordinary frame once the gate graded
			// (#953) — off the very frames whose pointer stands on the act.
			// Unconditionally put it on sets whose own tabs draw no mark at all,
			// so `2 one by one` contradicted the frame it was pressed from.
			aside = a.questionSafeAside()
		}
		for _, line := range a.questionPanelRow(set[0], itoa(row+1), "", word, "", aside, 0, room, row == pick, a.questionHovering(questionHoverPanel, row)) {
			a.questionBands = append(a.questionBands, questionBand{
				row: 1 + len(rows), span: hudSpan{from: 0, to: inner + 2}, at: row,
			})
			rows = append(rows, line)
		}
	}
	rows = append(rows, "")
	keys := questionSetVerbs(formsGroup)
	title := a.questionMarkFor(set[0].question) + " " + a.pal.ink("allow these "+itoa(len(set))+"?")
	panel := framed{
		title: title,
		aside: a.pal.dim(strings.Join(tools, questionKeyGap)),
		keys:  a.questionKeyRow(questionShown{}, questionKeysOnTier(keys, keyPrimary), frameEdgeRoom(width)),
	}
	out, _ := panel.draw(a.pal, width, rows)
	if second := questionKeysOnTier(keys, keySecondary); len(second) > 0 {
		out = append(out, "  "+a.questionKeyRow(questionShown{}, second, max(width-2, 1)))
	}
	return out
}

// questionTabStrip is the top edge of a set: a mark and a word per question and
// the review last. The tab on screen wears the filled dot and ink, a question
// with an answer held wears the tick, and one still waiting the hollow dot.
//
// THE WORDS SHARE THE EDGE EQUALLY and are cut to their share rather than
// pushing the review off the end: the review is the tab that sends, and an edge
// that dropped it would be a set with no way to finish it.
func (a *app) questionTabStrip(set []questionShown, at, room int) string {
	labels := a.questionTabLabels(set)
	const gap = "   "
	// Every tab spends its mark, a space and the gap before the next; the
	// review's word is kept whole.
	overhead := (len(set)+1)*(2+len(gap)) - len(gap) + ansi.StringWidth(questionReviewWord)
	share := (room - overhead) / len(set)
	parts := make([]string, 0, len(set)+1)
	// THE MARKS ARE THE STEP DOTS' OWN, because a strip of tabs is a sequence
	// with a place in it and that is what those two marks already say: the
	// filled dot is where you are and the hollow one is still to come. Minting
	// a pair of tab-shaped glyphs drew the same two cells in all three tiers
	// under two more names, which is the one-glyph-one-meaning law's whole
	// subject. `✓` keeps its own meaning — settled — and a tab wears it only
	// once its question has an answer held.
	held := 0
	for i, q := range set {
		label := ""
		if share >= 3 {
			label = fit(labels[i], share)
		}
		switch {
		case i == at:
			parts = append(parts, a.pal.warnBold(a.icon(tokens.GStepDone))+" "+a.pal.ink(label))
		case q.staged != nil:
			held++
			parts = append(parts, a.pal.dim(a.icon(tokens.GSettled)+" "+label))
		default:
			parts = append(parts, a.pal.dim(a.icon(tokens.GStepPending)+" "+label))
		}
	}
	if at >= 0 && set[at].staged != nil {
		held++
	}
	// AND THE REVIEW IS SETTLED ONLY WHEN THERE IS NOTHING LEFT TO HOLD. It wore
	// the tick whenever it was not the tab on screen, so `✓ review` stood over a
	// set nobody had answered — the one mark on this surface that means "done",
	// promising it about the one tab that had not been opened.
	switch {
	case at < 0:
		parts = append(parts, a.pal.warnBold(a.icon(tokens.GStepDone))+" "+a.pal.ink(questionReviewWord))
	case held == len(set):
		parts = append(parts, a.pal.dim(a.icon(tokens.GSettled)+" "+questionReviewWord))
	default:
		parts = append(parts, a.pal.dim(a.icon(tokens.GStepPending)+" "+questionReviewWord))
	}
	return strings.Join(parts, gap)
}

// questionTabLabels is what each tab is called: what tells a question apart
// from its neighbours.
//
// A QUESTION ABOUT A CALL IS CALLED BY WHAT THE CALL TOUCHES, because four
// permissions from one batch all say `needs your ok to run read` and differ only
// in the file. ANY OTHER QUESTION IS CALLED BY ITS OWN SENTENCE. Either way the
// lead every label shares is taken off ([questionTrimShared]) — `read
// package.json?` and `read go.mod?` are `package.json` and `go.mod`, and
// `/tmp/work/e.txt` and `/tmp/work/f.txt` are `e.txt` and `f.txt` — because the
// words the set shares are the words that do not help anybody pick a tab.
func (a *app) questionTabLabels(set []questionShown) []string {
	labels := make([]string, len(set))
	for i, q := range set {
		labels[i] = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(q.question.Head), "?"))
		if call := a.questionPanelCall(q); call != "" && call != a.questionPanelTool(q) {
			labels[i] = call
		}
	}
	return questionTrimShared(labels)
}

// questionTrimShared takes off the lead every label shares, cut back to the
// last word or path boundary inside it so no label starts halfway through a
// word. It takes nothing when that would leave a label empty: a tab called by
// nothing is worse than a tab called by too much.
func questionTrimShared(labels []string) []string {
	if len(labels) < 2 {
		return labels
	}
	lead := labels[0]
	for _, label := range labels[1:] {
		n := 0
		for n < len(lead) && n < len(label) && lead[n] == label[n] {
			n++
		}
		lead = lead[:n]
	}
	cut := strings.LastIndexAny(lead, " /") + 1
	out := make([]string, len(labels))
	for i, label := range labels {
		out[i] = strings.TrimSpace(label[cut:])
		if out[i] == "" {
			return labels
		}
	}
	return out
}

// questionHeldWords is the answer held for one question, in the words its own
// answers are drawn in.
func questionHeldWords(q questionShown) string {
	if q.staged == nil {
		return ""
	}
	words := strings.Join(questionLabels(q.question, q.staged.Keys()), ", ")
	if change := strings.TrimSpace(q.staged.Change); change != "" {
		if words == "" {
			return change
		}
		return words + " · " + change
	}
	return words
}

// questionSendWord is the review's one answer, said as what it will send.
func questionSendWord(held, of int) string {
	if held == of {
		return "send all " + itoa(of)
	}
	left := of - held
	if left == 1 {
		return "send the " + itoa(held) + " answered · 1 stays open"
	}
	return "send the " + itoa(held) + " answered · " + itoa(left) + " stay open"
}

// questionSetHoldsTheTurn reports whether every question in the set is holding
// the turn, which is the one case where "the turn resumes" is true of sending.
func questionSetHoldsTheTurn(set []questionShown) bool {
	for _, q := range set {
		if !q.question.Blocking.Turn {
			return false
		}
	}
	return true
}

// questionSetVerbs is the key table's rows for one of the set's drawings, in
// the table's order. The set's drawings are not a question, so there is no
// question's condition to read; what depends on the drawing is taken off by the
// drawer ([questionDropVerbKey]).
func questionSetVerbs(form questionForms) []questionVerb {
	out := make([]questionVerb, 0, 4)
	for _, verb := range questionKeys {
		if verb.forms.holds(form) {
			out = append(out, verb)
		}
	}
	return out
}

// questionDropVerbKey is a row of keys without one key.
func questionDropVerbKey(keys []questionVerb, key string) []questionVerb {
	out := keys[:0:0]
	for _, verb := range keys {
		if verb.key != key {
			out = append(out, verb)
		}
	}
	return out
}

func questionHasWord(words []string, word string) bool {
	for _, one := range words {
		if one == word {
			return true
		}
	}
	return false
}

// ── folded, and opened again ────────────────────────────────────────────────

// questionSetFolded is every folded question sharing a step with `q` that may
// join a set with it, `q` included — the set a fold rule is standing for.
func (a *app) questionSetFolded(q questionShown) []questionShown {
	if !a.questionJoinsSet(q) {
		return nil
	}
	var out []questionShown
	for _, one := range a.questions {
		if a.questionFolded[one.token()] && one.question.Batch == q.question.Batch && a.questionJoinsSet(one) {
			out = append(out, one)
		}
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

// questionSetFoldedRow is a folded set's one rule: how many questions, and what
// each is called.
func (a *app) questionSetFoldedRow(set []questionShown, width int) string {
	words := itoa(len(set)) + " questions · " + strings.Join(a.questionTabLabels(set), ", ")
	title := a.questionMarkFor(set[0].question) + a.pal.dim(" "+words)
	return framed{title: title, aside: a.pal.dim(questionOpenFoldWord)}.rule(a.pal, width)
}
