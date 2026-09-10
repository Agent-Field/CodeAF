package tui3

// ── THE SHEET: MANY DECISIONS, ONE READING ──────────────────────────────────
//
// A step that raised four quiet questions used to deliver them one at a time,
// each landing on top of the last, so a person answered the fourth first and
// never saw that the first three were the same decision asked about four files.
// The sheet is what arrives instead: everything one step gathered, grouped by
// the SHAPE of the decision, answered down the list, sent in one press.
//
// ── THE ROWS, EXACTLY AS THEY ARE DRAWN ─────────────────────────────────────
//
//	? 4 questions raised together
//	  asking permission
//	  ✓ read vendor/modernc.org?        allow once
//	  ? read vendor/golang.org/x?       1 allow once · 2 not now
//	▸ ? write to .github/workflows?     1 allow once · 2 not now
//	  choosing
//	  ? which index should this use?    1 sqlite · 2 in memory
//	  [enter] open it · [s] send what is answered (1) · [esc] later
//
// The cursor is the one `▸`; `✓` is a row this person has answered and the word
// after it is the answer they gave; `?` is a row still waiting, with the answers
// it offers. Both marks come from the vocabulary through its one door, and only
// the waiting mark is amber — a row that has been answered is not waiting on
// anybody.
//
// `1`-`9` answer the row the cursor is on, in THAT question's own keys, and the
// cursor walks on to the next row still waiting. `↑↓` move without answering,
// and `g` spreads THE LAST ANSWER GIVEN rather than the focused row's — which is
// the same row, one keystroke earlier.
//
// ── THE FOUR LAWS ───────────────────────────────────────────────────────────
//
//   - ANSWERS ARE HELD AGAINST THE QUESTION AND NEVER AGAINST THE ROW. A
//     withdrawal takes its row out and the sheet re-flows; if answers were kept
//     by position, that re-flow would silently move somebody's `allow once` onto
//     the question underneath it. They are kept by token ([questionToken]).
//   - `s` SENDS WHAT IS ANSWERED AND DELEGATES THE REST — to each question's OWN
//     pick, never to a key this file chose. A question the asker named no pick
//     on is left open, because delegating it would mean inventing an answer
//     nobody offered.
//   - EVERY ROW WEARS ITS OWN ANSWERS, because a sheet you have to open every
//     row of to use is a list of sentences rather than a way to answer. A row
//     already answered wears the word it was answered with instead.
//   - "SAME ANSWER FOR ALL LIKE THIS" IS OFFERED PER KEY AND NOT PER POSITION.
//     `g` gives the focused row's answer to every other row of the same shape
//     THAT OFFERS THAT KEY under that key. A question whose second option is
//     `deny` and another whose second option is `always` are not the same
//     answer, and a sheet that treated them as one would deny one and approve
//     the other.
//   - THE SHEET STANDS DOWN FOR A QUESTION BEING READ. `enter` takes one row
//     out and puts it on the block in its own form, with everything the block
//     draws; the sheet comes back when that one is answered or folded. The two
//     are never on screen together, because the block is above the message box
//     and a screen with both would be a screen with no conversation left on it.

import (
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// questionSheetFloor is how many questions one step has to have gathered before
// they arrive as a sheet. TWO, because one question is a question: a sheet drawn
// over a single row would be a heading, a group label and an answers row spent
// on something the line form says in one.
const questionSheetFloor = 2

// The sheet's own words, spelled once and quoted in the manual exactly.
const (
	// questionSheetTogether is the head's tail. It says the one thing about a
	// sheet that is not visible from its rows — these arrived at the same
	// moment, from one piece of work — which is what makes answering them as a
	// group reasonable rather than a shortcut.
	questionSheetTogether = " raised together"
	// questionSheetSendWord is `s`, in the manual's spelling and the row's.
	questionSheetSendWord = "send what is answered"
	// questionSheetSameWord is `g`.
	questionSheetSameWord = "same answer for all like this"
	// questionSheetOpenWord is `enter` on the focused row.
	questionSheetOpenWord = "open it"
	// questionSheetHeldWord is what the sheet says about a row `s` could not
	// delegate. It is the emptiness law's opposite number: something WAS left
	// undone, so the surface says so rather than closing quietly.
	questionSheetHeldWord = " still needs you"
)

// questionSheet is one batch as this surface holds it.
type questionSheet struct {
	// questions are the rows, sorted by shape and then by when each was asked.
	questions []session.Question
	// answers are what this person has given so far, keyed by [questionToken]
	// (this file's first law).
	answers map[string]session.Answer
	// cursor is the row `enter` and the digits act on.
	cursor int
	// last is the row an answer was most recently given to, by token.
	//
	// `g` READS THE LAST ANSWER AND NOT THE FOCUSED ROW, because the moment
	// somebody wants "the same for all of these" is the moment just after they
	// gave one — and by then the cursor has walked on to the next row still
	// waiting ([app.answerSheetRow]), which has no answer to copy.
	last string
}

// newQuestionSheet builds one from what a step boundary released.
func newQuestionSheet(questions []session.Question) *questionSheet {
	s := &questionSheet{
		questions: append([]session.Question(nil), questions...),
		answers:   map[string]session.Answer{},
	}
	s.flow()
	return s
}

// flow is the grouping, and it is re-run after every removal so the sheet a
// person is reading is always grouped the way it was drawn.
//
// IT IS STABLE ON THE ASK KIND AND THEN ON THE ASKING TIME, which is the order
// [session.Agent.OpenQuestions] already puts questions in, narrowed by the one
// thing the sheet adds: like goes with like.
func (s *questionSheet) flow() {
	sort.SliceStable(s.questions, func(i, j int) bool {
		if s.questions[i].Ask != s.questions[j].Ask {
			return questionShapeOrder(s.questions[i].Ask) < questionShapeOrder(s.questions[j].Ask)
		}
		return s.questions[i].Asked.Before(s.questions[j].Asked)
	})
	if s.cursor >= len(s.questions) {
		s.cursor = len(s.questions) - 1
	}
	if s.cursor < 0 {
		s.cursor = 0
	}
}

// answered reports whether one row has an answer on it, and what it was.
func (s *questionSheet) answered(q session.Question) (session.Answer, bool) {
	answer, ok := s.answers[questionToken(q)]
	return answer, ok
}

// answerAt writes one row's answer, and reports whether the key was one that
// row offered. A key the question never offered is not an answer to it.
func (s *questionSheet) answerAt(at int, key string, now time.Time) bool {
	if at < 0 || at >= len(s.questions) {
		return false
	}
	q := s.questions[at]
	if _, ok := q.Option(key); !ok {
		return false
	}
	s.answers[questionToken(q)] = session.Answer{
		At: now, Kind: q.Kind, ID: q.ID, Ref: q.Ref, Ask: q.Ask,
		Key: key, Picked: []string{key}, DecidedBy: session.DecidedByPerson,
	}
	s.last = questionToken(q)
	return true
}

// lastAnswered is the row an answer was most recently given to, and whether
// that row is still on the sheet.
func (s *questionSheet) lastAnswered() (session.Question, session.Answer, bool) {
	for _, q := range s.questions {
		if questionToken(q) != s.last {
			continue
		}
		if answer, ok := s.answered(q); ok {
			return q, answer, true
		}
	}
	return session.Question{}, session.Answer{}, false
}

// sameForAll is `g`: the focused row's answer given to every other row of the
// same shape that offers the same key. It answers how many rows it reached,
// which is what the message line says out loud — a key that quietly answered
// four decisions would be the hidden rule this design refuses.
func (s *questionSheet) sameForAll(now time.Time) int {
	focus, answer, ok := s.lastAnswered()
	if !ok {
		return 0
	}
	reached := 0
	for i, q := range s.questions {
		if questionToken(q) == questionToken(focus) || q.Ask != focus.Ask {
			continue
		}
		if _, already := s.answered(q); already {
			continue
		}
		if s.answerAt(i, answer.Key, now) {
			reached++
		}
	}
	// The last answer stays the one the person GAVE, so pressing `g` twice on a
	// sheet that grew a row does the same thing rather than spreading whichever
	// row the loop happened to reach last.
	s.last = questionToken(focus)
	return reached
}

// sameForAllReaches is whether `g` would reach anything, which is the condition
// the key is offered under (the emptiness law: a key that would do nothing is
// not on the row).
func (s *questionSheet) sameForAllReaches() bool {
	focus, answer, ok := s.lastAnswered()
	if !ok {
		return false
	}
	for _, q := range s.questions {
		if questionToken(q) == questionToken(focus) || q.Ask != focus.Ask {
			continue
		}
		if _, already := s.answered(q); already {
			continue
		}
		if _, offers := q.Option(answer.Key); offers {
			return true
		}
	}
	return false
}

// takeAt lifts one row out of the sheet — `enter`'s half of THE SHEET STANDS
// DOWN — and hands it back whole so the block can draw it in its own form.
func (s *questionSheet) takeAt(at int) (session.Question, bool) {
	if at < 0 || at >= len(s.questions) {
		return session.Question{}, false
	}
	q := s.questions[at]
	delete(s.answers, questionToken(q))
	s.questions = append(s.questions[:at:at], s.questions[at+1:]...)
	s.flow()
	return q, true
}

// withdraw takes one row out because it stopped being a question, and reports
// whether this sheet had it. The answer that was on it goes with it; the rest
// keep theirs, which is this file's first law doing its job.
func (s *questionSheet) withdraw(q session.Question) bool {
	token := questionToken(q)
	for i := range s.questions {
		if questionToken(s.questions[i]) != token {
			continue
		}
		delete(s.answers, token)
		s.questions = append(s.questions[:i:i], s.questions[i+1:]...)
		s.flow()
		return true
	}
	return false
}

// send is `s`: what has been answered, plus every remaining question delegated
// to its OWN pick. Anything with no pick is handed back still open.
func (s *questionSheet) send(now time.Time) (answers []session.Answer, held []session.Question) {
	for _, q := range s.questions {
		if answer, ok := s.answered(q); ok {
			answers = append(answers, answer)
			continue
		}
		key := ""
		if q.Pick != nil {
			key = strings.TrimSpace(q.Pick.Key)
		}
		if key == "" {
			held = append(held, q)
			continue
		}
		if _, ok := q.Option(key); !ok {
			held = append(held, q)
			continue
		}
		answers = append(answers, session.Answer{
			At: now, Kind: q.Kind, ID: q.ID, Ref: q.Ref, Ask: q.Ask,
			Key: key, Picked: []string{key}, DecidedBy: session.DecidedByAsker,
		})
	}
	return answers, held
}

// answeredCount is how many rows carry an answer, which the send key wears.
func (s *questionSheet) answeredCount() int { return len(s.answers) }

// questionShapeOrder is which group goes above which.
//
// IT IS THE KIND TABLE'S OWN ORDER (docs/design/questions/DESIGN.md's "Kinds and
// their defaults") AND NOT THE ALPHABET. The table is ordered by how much of the
// person a question wants — a permission is a key, a judgement is a reading, a
// clarification is a sentence only they can write — so a sheet grouped in that
// order is a sheet whose cheap rows are at the top. Sorting on the value's own
// spelling would put `assumptions it made` above `asking permission` because of
// how the two words happen to start.
func questionShapeOrder(kind session.AskKind) int {
	for i, one := range []session.AskKind{
		session.AskPermission, session.AskChoice, session.AskJudgement,
		session.AskClarification, session.AskConfirmation, session.AskLanding,
		session.AskAssumption, session.AskRatify,
	} {
		if one == kind {
			return i
		}
	}
	return 99
}

// ── the shapes, in a person's words ─────────────────────────────────────────

// questionShapeWord is one group's heading.
//
// NO MACHINERY VOCABULARY. [session.AskKind]'s own spellings are the engine's
// nouns — `permission`, `judgement`, `ratify` — and three of them are words
// about a taxonomy rather than about what is being asked. What goes on a
// heading is what the rows under it have in common, said the way somebody would
// say it out loud.
func questionShapeWord(kind session.AskKind) string {
	switch kind {
	case session.AskPermission:
		return "asking permission"
	case session.AskChoice:
		return "choosing"
	case session.AskJudgement:
		return "your judgement"
	case session.AskClarification:
		return "what you meant"
	case session.AskConfirmation:
		return "confirming"
	case session.AskLanding:
		return "your call"
	case session.AskAssumption:
		return "assumptions it made"
	case session.AskRatify:
		return "already done"
	}
	return ""
}

// ── drawing ─────────────────────────────────────────────────────────────────

// questionSheetRows draws it, at the frame's own width.
func (a *app) questionSheetRows(s *questionSheet, width int) []string {
	if s == nil || len(s.questions) == 0 || width < 1 {
		return nil
	}
	out := make([]string, 0, len(s.questions)+4)
	out = append(out, a.questionMark()+" "+a.pal.ask(fit(
		itoa(len(s.questions))+plural(" question", len(s.questions))+questionSheetTogether, width-2)))
	group := session.AskKind("")
	for i, q := range s.questions {
		if q.Ask != group {
			group = q.Ask
			if word := questionShapeWord(group); word != "" {
				out = append(out, a.pal.dim(fit("  "+word, width)))
			}
		}
		out = append(out, a.questionSheetRow(s, i, q, width))
	}
	out = append(out, a.questionSheetOffer(s, width))
	return out
}

// questionSheetRow is one row: the cursor, the state mark, the asker's own
// sentence, and then either the answers that row offers or the one that was
// given.
//
// EVERY ROW WEARS ITS OWN ANSWERS, and that is what makes the sheet answerable
// at all. The digits answer the row the cursor is on, and a person cannot press
// a digit for an answer they cannot see — a sheet that only listed sentences
// would be a list you had to open every row of to use.
//
// AND THE KEYS ARE EACH QUESTION'S OWN. They are read off
// [session.AnswerOption.Key], never numbered by position, so `2` means what that
// question says `2` means on the row it is drawn on and nothing else.
func (a *app) questionSheetRow(s *questionSheet, at int, q session.Question, width int) string {
	cursor, plainCursor := a.pal.ask("  "), "  "
	if at == s.cursor {
		plainCursor = a.icon(tokens.GCollapsed) + " "
		cursor = a.pal.ask(plainCursor)
	}
	answer, given := s.answered(q)
	// EACH ROW WEARS ITS OWN SHAPE'S MARK. A sheet is questions of several
	// shapes gathered at one boundary, so an assumptions row and a permission
	// row sit under each other here — and the one that is waiting on somebody is
	// the only one that may say so ([questionAskSlot]).
	slot, _ := questionAskSlot(q.Ask)
	mark, plainMark := a.questionMarkFor(q), a.icon(slot)
	if given {
		plainMark = a.icon(tokens.GSettled)
		mark = a.pal.dim(plainMark)
	}
	head := strings.TrimSpace(q.Head)
	tail := questionSheetAnswers(q)
	if given {
		tail = ""
		if option, ok := q.Option(answer.Key); ok {
			tail = strings.TrimSpace(option.Label)
		}
	}
	text := plainCursor + plainMark + " " + head
	if tail == "" {
		return a.pal.ask(fit(text, width))
	}
	// THE ANSWERS ARE IN A COLUMN, AND THE COLUMN IS SET BY THE LONGEST
	// SENTENCE IN THE SHEET RATHER THAN BY THE EDGE OF THE SCREEN.
	//
	// The first draft right-aligned them, which is the switcher's arrangement,
	// and on a 160-column terminal it put `allow once` a hundred cells away from
	// the question it answered — a column that is scannable and unreadable at
	// the same time, because the eye cannot carry a row that far. Setting the
	// column just past the longest head keeps both halves: the keys line up
	// under each other, and each one is beside its own sentence.
	column := s.answerColumn() + 2
	if column+ansi.StringWidth(tail) > width {
		column = width - ansi.StringWidth(tail)
	}
	if column < ansi.StringWidth(text)+2 {
		return a.pal.ask(fit(text+"  "+tail, width))
	}
	gap := column - ansi.StringWidth(text)
	// Painted in pieces rather than nested, for [app.questionOptionRow]'s
	// reason: these hues are raw SGR with an explicit reset, so a colour inside
	// a colour ends the outer one early.
	line := cursor + mark + a.pal.ask(" "+head) + strings.Repeat(" ", gap)
	if given {
		return line + a.pal.dim(tail)
	}
	return line + a.pal.ask(tail)
}

// answerColumn is where every row's answers begin: one cell past the longest
// sentence on the sheet, counting the cursor and mark cells in front of it.
func (s *questionSheet) answerColumn() int {
	widest := 0
	for _, q := range s.questions {
		if n := ansi.StringWidth(strings.TrimSpace(q.Head)); n > widest {
			widest = n
		}
	}
	// two cells of cursor, one of mark, one of space.
	return widest + 4
}

// questionSheetAnswers is one row's answers as the row prints them: each key
// and its own word, joined by the surface's own separator.
func questionSheetAnswers(q session.Question) string {
	parts := make([]string, 0, len(q.Options))
	for _, option := range q.Options {
		key := strings.TrimSpace(option.Key)
		word := strings.TrimSpace(option.Label)
		if key == "" || word == "" {
			continue
		}
		parts = append(parts, key+" "+word)
	}
	return strings.Join(parts, " · ")
}

// questionSheetOffer is the sheet's answers row, built from the one key table
// and degraded the way every other answers row on this surface is: the tail
// goes from the end backwards until what is left fits, and `s` and `esc` are
// never given up ([app.questionOffer] states the same law for the block).
//
// `s` WEARS ITS COUNT, which is the emptiness law read forwards: a send key
// that said nothing about how much it would send is a key a person presses to
// find out.
func (a *app) questionSheetOffer(s *questionSheet, width int) string {
	keys := make([]questionVerb, 0, 4)
	for _, verb := range questionKeys {
		if !verb.forms.holds(formsSheet) {
			continue
		}
		if verb.key == questionAlikeKey && !s.sameForAllReaches() {
			continue
		}
		verb.word = questionSheetKeyWord(verb)
		if verb.key == questionSendKey {
			if n := s.answeredCount(); n > 0 {
				verb.word += " (" + itoa(n) + ")"
			}
		}
		keys = append(keys, verb)
	}
	for {
		parts := make([]string, 0, len(keys)*4)
		for _, verb := range keys {
			if len(parts) > 0 {
				parts = append(parts, "", " · ")
			}
			parts = append(parts, "["+questionKeySpelling(verb.key)+"]", " "+verb.word)
		}
		plain := "  " + strings.Join(parts, "")
		if ansi.StringWidth(plain) <= width {
			out := a.pal.ask("  ")
			for i, part := range parts {
				if i%2 == 1 {
					out += a.pal.askBold(part)
					continue
				}
				out += a.pal.ask(part)
			}
			return out
		}
		dropped, ok := questionDropVerb(keys)
		if !ok {
			return a.pal.ask(fit(plain, width))
		}
		keys = dropped
	}
}

// ── the sheet on this surface ───────────────────────────────────────────────

// sheetOpen is how many questions the sheet is holding, and zero when there is
// none. The chip counts these ([app.questionCount]) because a question in a
// sheet is a question the work is waiting on exactly as much as one on the
// block.
func (a *app) sheetOpen() int {
	if a.questionBatch == nil {
		return 0
	}
	return len(a.questionBatch.questions)
}

// sheetShowing reports whether the sheet has the rows and the keyboard: it is
// holding something, and nobody has put it off.
func (a *app) sheetShowing() bool {
	return a.sheetOpen() > 0 && !a.questionBatchFolded
}

// questionBoundary is a step ending: everything the rule gathered arrives.
//
// TWO OR MORE MAKE A SHEET AND ONE MAKES A QUESTION ([questionSheetFloor]). A
// sheet drawn over a single row would be three rows of furniture around one
// decision the line form already says in one.
func (a *app) questionBoundary() tea.Cmd {
	return a.questionBoundaryFor(a.questionStep())
}

// questionGatherFor is the longest a quiet question waits for its step to end.
// Three seconds is [questionQuiet]'s number and is chosen the same way: it is a
// pause somebody would notice as one rather than a gap between two things
// happening. See [app.deliverQuestion] for why the hold must be bounded at all.
const questionGatherFor = 3 * time.Second

// questionGatherMsg is that clock going off, carrying the step it was armed for
// so a batch cannot be released by a timer belonging to the step before it.
type questionGatherMsg struct{ step string }

// gatherQuestions arms it.
func (a *app) gatherQuestions() tea.Cmd {
	step := a.questionStep()
	if step == "" {
		return nil
	}
	return tea.Tick(questionGatherFor, func(time.Time) tea.Msg {
		return questionGatherMsg{step: step}
	})
}

// questionBoundaryFor releases one named step's batch.
func (a *app) questionBoundaryFor(step string) tea.Cmd {
	if step == "" {
		return nil
	}
	held := a.questionReach.boundary(step)
	if len(held) == 0 {
		return nil
	}
	// THE STEP MOVES ON ONLY WHEN IT HANDED SOMETHING OVER, so a timer that
	// fired over an empty batch does not push the next question into a step of
	// its own.
	if step == a.questionStep() {
		a.questionStepEnded()
	}
	// A QUESTION OUTRANKS A PANEL, on question.go's terms: a batch drawn under
	// a fullscreen overlay is a batch nobody can see to answer.
	a.closeSettings()
	a.closeExpand()
	if len(held) < questionSheetFloor {
		for _, q := range held {
			a.raiseQuestion(questionShown{question: q})
		}
		return nil
	}
	if a.questionBatch == nil {
		a.questionBatch = newQuestionSheet(held)
	} else {
		// A SECOND BOUNDARY JOINS THE SHEET RATHER THAN REPLACING IT. A person
		// half-way down a list of decisions has not finished with it because
		// the work moved on, and a sheet swapped out from under them would lose
		// every answer they had given.
		for _, q := range held {
			a.questionBatch.questions = appendQuestionOnce(a.questionBatch.questions, q)
		}
		a.questionBatch.flow()
	}
	a.touch()
	return nil
}

// questionSheetKey routes the sheet's own keys. It is asked only when the sheet
// is what is drawn ([app.questionKey]).
func (a *app) questionSheetKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.sheetShowing() || !a.questionQuieted() {
		return nil, false
	}
	key := msg.String()
	if key == "ctrl+c" {
		// Leaving is never modal, on [app.questionKey]'s terms exactly.
		return nil, false
	}
	if key == questionLaterKey {
		// esc IS LATER. The sheet folds to the chip; nothing is answered,
		// nothing is cancelled, and every answer already given is still on it.
		a.questionBatchFolded = true
		a.touch()
		return nil, true
	}
	switch key {
	case "up", "shift+tab":
		a.moveSheetCursor(-1)
		return nil, true
	case "down", "tab":
		a.moveSheetCursor(1)
		return nil, true
	}
	if strings.TrimSpace(a.input.String()) != "" {
		// EVERY PRINTABLE KEY BELONGS TO THE COMPOSER, which is the block's law
		// and is the sheet's for the same reason: the rows are still there and
		// still answerable the moment the box is clear.
		return nil, false
	}
	switch key {
	case questionEnterKey:
		return a.openSheetRow()
	case questionSendKey:
		return a.sendSheet()
	case questionAlikeKey:
		return a.sameSheetAnswer()
	}
	// A DIGIT ANSWERS THE ROW THE CURSOR IS ON, which is ONE KEY GRAMMAR read
	// literally: `1`-`9` pick, on every form. The keys are that row's own
	// ([questionSheet.answerAt] looks them up in its options), so a digit the
	// focused question never offered is not an answer and falls through.
	if a.answerSheetRow(key) {
		return nil, true
	}
	return nil, false
}

// answerSheetRow takes one digit for the focused row and walks on to the next
// row still waiting — a person answering down a list does not also want to move
// down it.
func (a *app) answerSheetRow(key string) bool {
	if !a.questionBatch.answerAt(a.questionBatch.cursor, key, a.now()) {
		return false
	}
	for at := a.questionBatch.cursor + 1; at < len(a.questionBatch.questions); at++ {
		if _, given := a.questionBatch.answered(a.questionBatch.questions[at]); !given {
			a.questionBatch.cursor = at
			break
		}
	}
	a.touch()
	return true
}

// moveSheetCursor walks the rows, stopping at both ends rather than wrapping —
// a list somebody is reading down is not a carousel.
func (a *app) moveSheetCursor(by int) {
	to := a.questionBatch.cursor + by
	if to < 0 || to >= len(a.questionBatch.questions) {
		return
	}
	a.questionBatch.cursor = to
	a.touch()
}

// openSheetRow is `enter`: the focused question comes out of the sheet and onto
// the block in whichever form its own evidence asks for.
func (a *app) openSheetRow() (tea.Cmd, bool) {
	q, ok := a.questionBatch.takeAt(a.questionBatch.cursor)
	if !ok {
		return nil, false
	}
	a.raiseQuestion(questionShown{question: q})
	return nil, true
}

// sameSheetAnswer is `g`, and it SAYS WHAT IT DID. A key that quietly answered
// four decisions would be the hidden rule DESIGN.md refuses; the count on the
// message line is what makes it visible.
func (a *app) sameSheetAnswer() (tea.Cmd, bool) {
	reached := a.questionBatch.sameForAll(a.now())
	if reached == 0 {
		return nil, false
	}
	a.note(itoa(reached) + plural(" more question", reached) + " answered the same way")
	a.touch()
	return nil, true
}

// sendSheet is `s`: everything answered goes through the one door, everything
// left is delegated to its own pick, and anything that had no pick stays.
func (a *app) sendSheet() (tea.Cmd, bool) {
	answers, held := a.questionBatch.send(a.now())
	if len(answers) == 0 {
		return nil, false
	}
	doors, ok := a.questionDoors()
	if !ok {
		return nil, false
	}
	cmds := make([]tea.Cmd, 0, len(answers))
	for _, answer := range answers {
		one := answer
		cmds = append(cmds, func() tea.Msg { _ = doors.ResolveQuestion(one); return nil })
	}
	a.questionBatch = nil
	a.questionBatchFolded = false
	if len(held) > 0 {
		// WHAT COULD NOT BE DELEGATED IS SAID OUT LOUD and put back on the
		// block. A sheet that closed over a decision nobody made would be the
		// surface losing a question, which is the one thing DESIGN.md says a
		// question is never allowed to do.
		a.note(itoa(len(held)) + plural(" question", len(held)) + questionSheetHeldWord)
		for _, q := range held {
			a.raiseQuestion(questionShown{question: q})
		}
	}
	a.touch()
	return tea.Batch(cmds...), true
}
