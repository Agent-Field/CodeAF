package tui3

// ANSWER THE QUESTION WITHOUT OPENING THE WINDOW IT IS IN.
//
// Home already says which conversation is stopped on somebody: the row wears
// `▲`, sorts to the top of its project, and the state band spells out the one
// line it is stopped on (homebands.go's [drawStateBand]). Up to here that is a
// SIGN POST — it tells you where to walk. This band is the other half: the
// answers the card in that window is offering, under the line it is asking, as
// chips a digit or a click gives.
//
// ── WHY IT IS WORTH DOING AT ALL ──
//
// The whole cost of a question in another terminal is the walk: find the window,
// read the card back from the top because you have lost the thread of it, answer
// three keys' worth of it, walk back. The answer itself was never the work. A
// person glancing at home already has the one line in front of them — that line
// is what the card leads with too — and for `allow once`, `yes` and `deny` there
// is nothing more to know. So those go here, and everything that needs more of
// the card than one line stays in the window that has the card.
//
// ── THE FOUR LAWS ──
//
//   - IT DRAWS WHAT THE SESSION OFFERED, NEVER WHAT THIS BUILD KNOWS. The chips
//     come off [session.PresenceQuestion.Options], written by the session that
//     is waiting, so this surface can never advertise a key that session would
//     drop (answers.go states the same law from the writing end).
//
//   - IT NEVER REPEATS THE QUESTION. The line the session is stopped on is the
//     state band's, one row above, in ink. Printing it again here would be the
//     same sentence twice in the same colour on one small card — and home's
//     density is omission, never repetition. This band is the ANSWERS.
//
//   - A STALE WINDOW IS NOT ANSWERED. Every key and every click asks
//     [session.SessionPresence.Fresh] again, at the instant of the press and not
//     at the instant of the last reading: a window killed with a card on screen
//     leaves a file claiming a question nobody is waiting for, and an answer
//     sent into it would be a keystroke that quietly did nothing.
//
//   - THE ANSWER IS NOT THE ACT. Home leaves the answer on the other session's
//     doorstep and that session applies it on its own beat (answers.go), which
//     is a second or two later. So the band says `answered · waiting for it to
//     pick that up` until the question stops being in that session's presence,
//     which is the only proof this surface will ever have that it landed.

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

func init() {
	registerHomeBand(homeBand{name: "answer", order: bandOrderAnswer, draw: drawAnswerBand})
}

// The words this band says, quoted in the manual exactly as they are spelled
// here.
const (
	// answerSentWord opens home's message line after a key lands, with the
	// answer's own label after it — `answered · allow once`.
	answerSentWord = "answered · "
	// answerWaitingWord is the band while the other session has not picked it
	// up yet. It says what is true: the answer is on its way to a process that
	// looks for it every few seconds.
	answerWaitingWord = "answered · waiting for it to pick that up"
	// answerFailedWord is a doorstep that would not take it — a folder that is
	// gone, a disk that is read-only. It names the one thing left to do.
	answerFailedWord = "could not leave that answer — open the conversation and answer it there"
	// answerChipGap separates the chips, the same middot every other row of
	// answers on this surface uses.
	answerChipGap = " · "
)

// answerHoldFor is how long this window keeps saying it answered when the other
// session never picks it up. It is generous — a session drains on its presence
// heartbeat, and a machine under load can miss a few — and it exists only so
// that a window somebody killed one instant after being answered does not leave
// a line on this card for the rest of the day.
const answerHoldFor = time.Minute

// homeAnswered is one answer this window has already sent, kept by the session
// folder it was sent to.
//
// IT IS A CLAIM ABOUT THIS SCREEN AND NOT A RECORD OF ANYTHING. The record is
// the other session's transcript, where the card settles with the word it was
// answered with; this is only what lets the band stop offering chips for a
// question already answered from here.
type homeAnswered struct {
	kind  session.QuestionKind
	id    uint64
	label string
	at    time.Time
}

// answerable is the question one row is stopped on, when there is one this
// surface may draw answers for.
//
// The three conditions are separate facts and each is checked here rather than
// by the callers: the session says it is waiting, the claim is still fresh AT
// NOW, and the lane that raised it offered answers ([PresenceQuestion.Answerable]
// — the stuck-turn question deliberately does not).
func answerable(row session.SessionRow, now time.Time) (session.PresenceQuestion, bool) {
	if !row.NeedsPerson() || !row.Presence.Fresh(now) {
		return session.PresenceQuestion{}, false
	}
	question := row.Presence.Question
	if !question.Answerable() {
		return session.PresenceQuestion{}, false
	}
	return question, true
}

// answerSent is what this window already sent for one row's question, when it
// sent one and the question is still the same question.
func (a *app) answerSent(row session.SessionRow, question session.PresenceQuestion) (homeAnswered, bool) {
	sent, ok := a.answered[strings.TrimSpace(row.Dir)]
	if !ok || sent.kind != question.Kind || sent.id != question.ID {
		return homeAnswered{}, false
	}
	return sent, true
}

// drawAnswerBand is the chips, or the line that says they have been pressed.
//
// A WINDOW THAT CANNOT ANSWER DRAWS NOTHING (the absence law): with no seam
// wired there is nowhere to leave the answer, and chips that did nothing would
// be worse than the walk they promised to save.
func drawAnswerBand(a *app, ctx bandContext) []string {
	row, pal := ctx.subject.row, ctx.pal
	question, ok := answerable(row, ctx.now)
	if !ok {
		return nil
	}
	if sent, ok := a.answerSent(row, question); ok {
		if ctx.now.Sub(sent.at) < answerHoldFor {
			return []string{pal.dim(fit(answerWaitingWord, ctx.width))}
		}
		return nil
	}
	if a.leaveAnswer == nil && !a.answeringHere(row) {
		return nil
	}
	lines := a.answerChipLines(question, ctx.width, pal)
	if len(lines) == 0 {
		return nil
	}
	return lines
}

// answerChip is one chip as it is drawn and as it is pressed: the key and its
// complete label stay together whichever row the packer gives them.
type answerChip struct {
	key   string
	text  string
	label string
}

// answerChips names the complete chips before the card packs them into rows.
func answerChips(question session.PresenceQuestion) []answerChip {
	chips := make([]answerChip, 0, len(question.Options))
	for _, option := range question.Options {
		if strings.TrimSpace(option.Key) == "" {
			continue
		}
		text := option.Key + " " + option.Label
		chips = append(chips, answerChip{
			key: option.Key, text: text, label: option.Label,
		})
	}
	return chips
}

// answerChipLines paints them: the key bold in the question's own hue, the word
// beside it in the same hue. Whole chips move to following rows when needed;
// a card never hides an answer merely because the answers cannot share a row.
//
// IT IS THE CARD'S INK AND NOT THE CARD'S HELPER. consent.go's [app.paintOffer]
// draws the same shape and cannot be borrowed — it asks whether the pointer is
// over the block it belongs to, and there is no block here, only a card in a
// column. What is shared is the thing that matters, which is that a key on this
// surface is bold and violet wherever it is offered.
func (a *app) answerChipLines(question session.PresenceQuestion, width int, pal palette) []string {
	chips := answerChips(question)
	if len(chips) == 0 {
		return nil
	}
	painted := make([]string, 0, len(chips))
	for _, chip := range chips {
		painted = append(painted, pal.askBold(chip.key)+pal.ask(" "+chip.label))
	}
	return bandClauses(width, 0, func(s string) string { return s }, painted...)
}

// ── answering ───────────────────────────────────────────────────────────────

// answerKey is the digit keys, read from home's own key hook with nothing typed
// (home.go). It answers only for the row UNDER THE CURSOR, and it reports
// whether it took the key — a digit that is not one of this question's answers
// falls through and is typed, exactly as it would be on any other row.
func (a *app) answerKey(key string) (tea.Cmd, bool) {
	subject, ok := a.homeSubject()
	if !ok || subject.kind != bandKindSession {
		return nil, false
	}
	row := subject.row
	question, ok := answerable(row, time.Now())
	if !ok {
		return nil, false
	}
	if _, sent := a.answerSent(row, question); sent {
		// ONE ANSWER PER QUESTION. The first one is on its way; a second press
		// would be a second line on the doorstep for a question that is already
		// answered, and the row is still saying so.
		return nil, false
	}
	if session.AnswerLabel(question.Kind, key) == "" {
		return nil, false
	}
	return a.sendAnswer(row, question, key)
}

// answerPress is the same thing under the pointer: a click on a chip is the
// chip's key.
//
// It finds the chip by REBUILDING THE FRAME the way [app.homePress] does and
// looking for the chip line's plain text on the row that was clicked. Nothing
// is recorded at draw time, which is the point: the only thing that could go
// wrong with a remembered span is that it is a frame out of date.
func (a *app) answerPress(x, y int) (tea.Cmd, bool) {
	if !a.home.open || y < 0 {
		return nil, false
	}
	subject, ok := a.homeSubject()
	if !ok || subject.kind != bandKindSession {
		return nil, false
	}
	row := subject.row
	question, ok := answerable(row, time.Now())
	if !ok {
		return nil, false
	}
	if _, sent := a.answerSent(row, question); sent {
		return nil, false
	}
	chips := answerChips(question)
	if len(chips) == 0 {
		return nil, false
	}
	width, height := a.size()
	lines, _, _, _ := a.homeFrame(width, height)
	if y >= len(lines) {
		return nil, false
	}
	// The painted row with its colour taken off, and the chip found inside it:
	// each chip may have moved to its own row, so the clicked row is the source
	// of truth rather than offsets from the former single-line layout.
	plain := ansi.Strip(lines[y])
	for _, chip := range chips {
		at := strings.Index(plain, chip.text)
		if at < 0 {
			continue
		}
		start := ansi.StringWidth(plain[:at])
		if x >= start && x < start+ansi.StringWidth(chip.text) {
			return a.sendAnswer(row, question, chip.key)
		}
	}
	return nil, false
}

// sendAnswer gives one answer, and says on the message line what it just did.
//
// THE TWO ROUTES ARE ONE DECISION MADE ONCE, here, so neither the keyboard nor
// the pointer has to know there are two: a question in THIS window is answered
// through the resolver this window already holds, and every other window's is
// left on its doorstep for it to pick up.
func (a *app) sendAnswer(row session.SessionRow, question session.PresenceQuestion, key string) (tea.Cmd, bool) {
	label := session.AnswerLabel(question.Kind, key)
	if label == "" {
		return nil, false
	}
	if a.answeringHere(row) {
		cmd, took := a.answerHere(question, key)
		if !took {
			return nil, false
		}
		a.home.say(answerSentWord+label, "")
		return cmd, true
	}
	dir := strings.TrimSpace(row.Dir)
	if a.leaveAnswer == nil || dir == "" {
		return nil, false
	}
	if err := a.leaveAnswer(dir, question.Kind, question.ID, key); err != nil {
		a.home.say(answerFailedWord, row.Dir)
		return nil, true
	}
	a.rememberAnswered(dir, question, label)
	a.home.say(answerSentWord+label, "")
	return nil, true
}

// rememberAnswered records what was sent, and forgets what has gone quiet. The
// sweep is here rather than on a clock because this is the only line that grows
// the map, so it is the only place it can be kept small.
func (a *app) rememberAnswered(dir string, question session.PresenceQuestion, label string) {
	now := time.Now()
	if a.answered == nil {
		a.answered = map[string]homeAnswered{}
	}
	for at, sent := range a.answered {
		if now.Sub(sent.at) >= answerHoldFor {
			delete(a.answered, at)
		}
	}
	a.answered[dir] = homeAnswered{kind: question.Kind, id: question.ID, label: label, at: now}
}

// answeringHere reports that the row on home IS the conversation this window is
// holding, so the question is one this process can answer in its own hands. The
// comparison is the transcript's path, which is how every other line on this
// screen tells its own row apart from the rest (home.go).
func (a *app) answeringHere(row session.SessionRow) bool {
	return row.Transcript != "" && row.Transcript == a.file
}

// answerHere answers this window's own question through the paths its own card
// answers through.
//
// THE CARD ON SCREEN IS SETTLED WHERE THERE IS ONE. The engine would take the
// answer either way — the resolvers are keyed by id and do not care who calls
// them — but a card left on screen for a question already answered is a surface
// telling a person something untrue about their own session. So the card's own
// answer path is preferred and the bare resolver is the fallback for a question
// whose card this surface never held.
//
// In practice this is a guard rather than a lane a person walks down: a question
// arriving while home is up CLOSES home (app.go), because a session blocked
// behind a fullscreen page is a question nobody can see. What is left is the
// narrow case where the two crossed, and the honest thing there is to answer
// rather than to write into a folder this process is itself holding open.
func (a *app) answerHere(question session.PresenceQuestion, key string) (tea.Cmd, bool) {
	action, ok := session.AnswerFromKey(question.Kind, key)
	if !ok {
		return nil, false
	}
	switch action.Kind {
	case session.QuestionConsent:
		if len(a.asks) > 0 && a.asks[0].id == question.ID {
			a.answerWith(action.Allow, action.Scope, answerConsentWord(action))
			return nil, true
		}
		if a.agent != nil {
			a.agent.ResolveConsentRemember(question.ID, action.Allow, action.Scope)
			return nil, true
		}
	case session.QuestionTask:
		if card := a.task; card != nil && card.id == question.ID && !card.settled() {
			a.answerTask(action.Task.Approved, "")
			return nil, true
		}
		if agent, ok := a.tasker(); ok {
			agent.ResolveTask(question.ID, action.Task)
			return nil, true
		}
	case session.QuestionStanding:
		if card := a.stand; card != nil && card.id == question.ID && !card.settled() {
			if action.Standing.Once {
				return a.answerStanding(action.Standing, standOnceDone, standOnceWord), true
			}
			return a.answerStanding(action.Standing, standSetWord, standYesWord), true
		}
		if agent, ok := a.stander(); ok {
			agent.ResolveStanding(question.ID, action.Standing)
			return nil, true
		}
	}
	return nil, false
}

// answerConsentWord is what the row in this window keeps.
//
// THE ALWAYS IS SPELLED WITH ITS REACH ON IT, because from home it is the
// tool-wide one and nothing narrower: the second beat that turns a shell always
// into a shape is a thing you do while looking at the command, and home has the
// one line the session is stopped on rather than the command
// ([session.AnswerFromKey] states the same at the other end). A row that said
// `always · saved` would be claiming a rule nobody wrote.
func answerConsentWord(action session.AnswerAction) string {
	if action.Allow && action.Scope == session.ConsentToolSession {
		return answerAlwaysWord
	}
	return decisionWord(action.Allow)
}

// answerAlwaysWord is that spelling.
const answerAlwaysWord = "always · this tool, this session"
