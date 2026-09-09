package tui3

import (
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── THE QUESTION BLOCK ──────────────────────────────────────────────────────
//
// One renderer for every decision this engine hands a person. Before this file
// the surface had one block per lane — the approval gate, the connect offer, the
// harness offer, the task proposal, the standing card, the fuel gate, the stop
// card, the tab-close card — each with its own layout loop, its own key set, its
// own idea of what esc means, and three of them with no drawing at all, so work
// stopped on questions nobody in the product could see. docs/design/questions/
// DESIGN.md is the contract; this is the block half of it.
//
// THE ROWS, EXACTLY AS THEY ARE DRAWN.
//
// The line — a permission, a confirmation, anything whose whole decision fits
// beside its own answers. The row above it is the subject's OWN row, re-used
// rather than re-worded, because two renderings of one call is how a person
// approves something other than what they read:
//
//	  ╰─▶ bash rm -rf build
//	? allow this? [1] allow once · [2] always · [3] deny · [esc] later · 7s
//	  bash pattern "rm -rf *"
//	  2 more
//
// The card — a decision with more than a word behind each answer. Head, then
// the reason and who is asking, then one row per answer with what it costs,
// then the answers row:
//
//	? wants to start a task: rewrite the packer
//	  it will run on its own branch and open a pull request · aforge
//	    1  start it        on a branch of its own
//	  ▸ 2  not now         nothing runs
//	    3  change it first
//	  [enter] take the pick · [d] you decide · [esc] later · 9s
//
// The ratify line — the third rung of the ladder, where the work is already
// done and what is being asked is whether it holds. Nothing waits on it:
//
//	✓ renamed 12 files under src/ · [u] undo · [c] change
//
// The receipt, which stays exactly where the question was, dim, because the
// transcript is what happened and "you were asked and said this" is part of it:
//
//	  decided allow this? → allow once · you · 14:02 · c change
//
// And the withdrawn line, once, dim, when the asker took the question back:
//
//	  ⊘ allow this? — no longer needed · the turn moved on without it
//
// ── THE FOUR LAWS THIS FILE IS THE ENFORCEMENT OF ───────────────────────────
//
//   - NEVER MODAL, NEVER SUSPENDS THE KEYBOARD. The box below stays live and
//     typing into it is answering in words. This RETIRES the consent block's
//     "IT SUSPENDS THE KEYBOARD" law, which was the honest design when esc's
//     only two readings were "answer no" and "be trapped": a block a person
//     could not leave had to own every key so that nothing was typed into a
//     conversation that could not move. esc is `later` now, so there is a way
//     out that neither answers nor traps, and the keyboard goes back to the
//     person ([app.questionKey] takes only what it draws).
//   - THE SETTLE GUARD. A key that arrived before the question had been on
//     screen for [questionSettle] is DROPPED, never applied. A question that
//     lands under a hand already moving is a question answered by a keystroke
//     aimed at the sentence somebody was typing.
//   - THE BOX IS NEVER MOVED UNDER A HAND. A question raised while the box
//     holds words waits behind the chip until the words go or the hands stop
//     for [questionQuiet] ([app.questionQuieted]).
//   - THE ANSWER IS THE RECORD. Every answer leaves its line where the question
//     was, and the line is [session.DecisionRecord.Line]'s — the engine's own
//     rendering, so the row a person reads and the line the model reads cannot
//     become two accounts of one decision.

const (
	// questionSettle is how long a question must have been ON SCREEN before it
	// will take a key. 250ms is the settle guard's own number from
	// docs/design/questions/DESIGN.md, and it is about the hand rather than the
	// eye: it is roughly one keystroke at a fast typing speed, which is exactly
	// the window in which a question can arrive between a person deciding to
	// press a key and the key landing.
	questionSettle = 250 * time.Millisecond
	// questionQuiet is how long the box must have been still before a question
	// that arrived on top of a half-typed sentence is allowed to take its rows.
	// Three seconds is a pause somebody has stopped typing in rather than a gap
	// between two words.
	questionQuiet = 3 * time.Second
	// questionRuleAfter is how many same-shaped yeses are given before `r make
	// it a rule` is offered. The third, because two is a coincidence and a rule
	// offered on the first is the surface guessing at a habit somebody has not
	// formed (DESIGN.md's RULES ARE OFFERED, VISIBLE, FORGETTABLE).
	questionRuleAfter = 3
	// questionCardOptions is how many answers a card draws a row apiece for.
	// Past it the answers go on one row like the line's, because a block that
	// can be nine rows tall is a block that pushes the conversation off a short
	// screen — and the engine's own gate refuses more than four answers on
	// anything but a checklist ([session.Question.Check]), so this is a floor
	// under a bound that already exists rather than a second bound.
	questionCardOptions = 4
)

// questionAgent is the questions half of the agent under this surface, when it
// has one.
//
// IT IS AN OPTIONAL ASSERTION AND NOT A LINE ON [Agent], for stop.go's reason
// exactly: an agent that has never heard of questions keeps everything else it
// had, and this surface simply draws no engine questions rather than failing to
// compile against every fake in the tree. A CAPABILITY THAT CANNOT WORK IS
// ABSENT, NOT BROKEN — so with no door here the block still draws the questions
// the SURFACE raises (the stop card, the tab-close card), which need no engine
// at all.
type questionAgent interface {
	// OpenQuestions is every decision this session is waiting on somebody for,
	// oldest first. It is DERIVED from the lanes' own waits; there is no second
	// store (internal/session's question.go).
	OpenQuestions() []session.Question
	// WatchQuestions is a standing subscription to [session.EventQuestion],
	// [session.EventQuestionWithdrawn] and [session.EventQuestionAnswered] that
	// replays everything already open when a surface attaches.
	WatchQuestions() (<-chan session.Event, func())
	// ResolveQuestion is THE ONE DOOR every answer goes through: it reads the
	// lane off the answer and hands it to that lane's own resolver.
	ResolveQuestion(session.Answer) error
}

// questionDoors is that half of the agent, when it has one.
func (a *app) questionDoors() (questionAgent, bool) {
	doors, ok := a.agent.(questionAgent)
	return doors, ok && doors != nil
}

// questionShown is one question as this surface holds it: the object the engine
// (or this surface) raised, plus the four facts that are the SURFACE'S and
// belong nowhere else — when it was drawn, where the cursor is, whether a rule
// is on offer, and how to answer it when no engine is behind it.
type questionShown struct {
	question session.Question
	// local answers a question the SURFACE raised — the stop card, the
	// tab-close card, anything this program asks about itself. It is nil on
	// every question that came from the engine, which go through
	// [questionAgent.ResolveQuestion] instead, and the two are never both set:
	// one question has one resolver.
	local func(session.Answer)
	// shown is when this question first had a frame drawn with it on, which is
	// what [questionSettle] is measured from. It is NOT when the question was
	// raised: a question that waited behind a half-typed sentence
	// ([app.questionQuieted]) has been in this program for a while and on
	// screen for none of it, and the guard is about the screen.
	shown time.Time
	// pick is the answer the cursor is on, and it is meaningful only on the
	// confirmation kind — the one shape that keeps stop.go's law that the
	// cursor starts on the answer that loses nothing. Every other form is
	// answered by its digit or by the asker's own pick, so there is no cursor
	// to move and none is drawn.
	pick int
	// rule says `r make it a rule` is on this question's answers row: the third
	// same-shaped yes has been given ([app.questionRuleOffered]).
	rule bool
	// ruled says this question's SHAPE is answered by a rule this project has
	// written down (`/autonomy`), which is what puts `· your rule` on the row.
	//
	// NEVER A HIDDEN RULE (docs/design/questions/DESIGN.md). A clock ticking on
	// a question because of a setting somebody made three weeks ago, with
	// nothing on the row saying so, is exactly the thing that law forbids.
	ruled bool
	// undoable says the ratify row's `u` would reach something real. A ratify
	// question whose work cannot be taken back does not offer the key, which is
	// the emptiness law applied to an answer rather than to a number.
	undoable bool
	// spans is where this question's answers landed in columns, written by the
	// draw and read by the press — consent.go's own bargain, kept.
	spans []choiceSpan
	// row is which row of the block those spans are on.
	row int
}

// token is the one string this question is known by across the two maps below
// and across a fold. It is the lane and the lane's own id, which is
// [session.Question.Token] with the lane written in — two lanes may both be
// waiting on id 7.
func (q questionShown) token() string {
	return string(q.question.Kind) + ":" + q.question.Token()
}

// questionRecord is a question that has stopped being one: answered, and
// keeping its receipt, or withdrawn, and keeping its one dim sentence. Both
// stay where the question was.
type questionRecord struct {
	// record is the answered form — the engine's own [session.DecisionRecord],
	// rendered by the engine's own [session.DecisionRecord.Line] so a person
	// and the model read one account of one decision.
	record session.DecisionRecord
	// withdrawn is the reason the asker took it back, and it is the whole of
	// what distinguishes the two: a record with a withdrawal reason is the
	// ⊘ line, and a record without one is the receipt.
	withdrawn string
	// head is the question's own sentence, kept for the withdrawn line, which
	// has no record to read one off.
	head string
	// at is when it stopped being a question, which is what the fade below
	// [questionRecordFor] measures.
	at time.Time
	// reversible says the receipt offers `c change`. An irreversible decision
	// says `cannot change` instead, and the engine's own [session.
	// DecisionRecord.Line] already writes that half.
	reversible bool
}

// ── what is open, and which one is drawn ────────────────────────────────────

// questionOpen is every question this surface would draw, oldest first, with
// the folded ones and the ones still waiting on a quiet box left out.
//
// THE ORDER IS THE ENGINE'S AND IS NOT RE-DECIDED HERE. [session.Agent.
// OpenQuestions] sorts oldest first and says why (a map has no order, so two
// reads of one unchanged session would otherwise hand a surface two different
// lists). The surface's own questions are sorted into the same sequence by the
// same key, because a person answering down a queue does not care which side of
// the engine boundary each one came from.
func (a *app) questionOpen() []questionShown {
	// NOTHING OPEN ALLOCATES NOTHING. This is on the draw path and the draw
	// path is the one thing on a scrolling screen rebuilt from nothing every
	// frame ([TestOneScreenScrollOfFourThousandLinesStaysInsideTheAllocationLaw]
	// is the law), and the overwhelmingly common state of this block is empty.
	if len(a.questions) == 0 {
		return nil
	}
	out := make([]questionShown, 0, len(a.questions))
	for _, q := range a.questions {
		if a.questionFolded[q.token()] {
			continue
		}
		out = append(out, q)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].question.Asked.Equal(out[j].question.Asked) {
			return out[i].question.Asked.Before(out[j].question.Asked)
		}
		return out[i].token() < out[j].token()
	})
	return out
}

// questionHead is the question the block is drawing, and whether there is one.
func (a *app) questionHead() (questionShown, bool) {
	open := a.questionOpen()
	if len(open) == 0 {
		return questionShown{}, false
	}
	return open[0], true
}

// questioning reports whether the block is on screen at all.
func (a *app) questioning() bool {
	_, ok := a.questionHead()
	return ok
}

// questionCount is how many questions are open in this conversation, folded
// ones included. It is what the chip counts, and it counts the folded ones on
// purpose: `esc` is later and not cancelled, so a question a person put off is
// still a question the work is waiting on, and a count that dropped when they
// pressed esc would be the surface telling them they had finished.
func (a *app) questionCount() int { return len(a.questions) + a.sheetOpen() }

// ── raising one ─────────────────────────────────────────────────────────────

// raiseQuestion puts one question on the block.
//
// IT REPLACES BY TOKEN RATHER THAN APPENDING, because the engine re-emits an
// open question whenever a surface attaches ([session.Agent.WatchQuestions]
// replays), and a queue that grew a row on every reattach would say `4 more`
// about one decision.
//
// AND IT NEVER MOVES THE BOX UNDER A HAND. A question that arrives while there
// are words in the box is HELD — it goes on the list, so the chip counts it and
// home can see it, and [app.questionQuieted] is what lets it take its rows.
func (a *app) raiseQuestion(q questionShown) {
	if strings.TrimSpace(q.question.Head) == "" {
		// A QUESTION WITH NOTHING TO READ IS NOT DRAWN. The engine's own gate
		// refuses one at the door ([session.Question.Check]); this is the same
		// refusal on the surface side, for a lane that built its fallback out
		// of a card that carried no words.
		return
	}
	if q.question.Asked.IsZero() {
		q.question.Asked = a.now()
	}
	q.pick = questionSafeAt(q.question)
	for i := range a.questions {
		if a.questions[i].token() != q.token() {
			continue
		}
		// The SHOWN stamp survives the replay. A question re-sent by a
		// reattaching watcher has not just arrived, and restamping it would
		// hand it a fresh settle guard every few seconds — which is a question
		// that never becomes answerable on a link that reconnects.
		q.shown = a.questions[i].shown
		q.pick = a.questions[i].pick
		a.questions[i] = q
		a.touch()
		return
	}
	q.ruled = a.autonomyRuled(q.question.Ask)
	a.questions = append(a.questions, q)
	a.questionRule(&a.questions[len(a.questions)-1])
	a.touch()
}

// questionSafeAt is the index of the answer the cursor starts on: the one
// marked safe, or the last one.
//
// THIS IS stop.go's AND tabclose.go's LAW, VERBATIM AND UNWEAKENED — "the
// answer under `enter` is the one a person gets by pressing the key they press
// to make a question go away, so it has to be the answer that loses nothing."
// The lane says which answer that is by marking it [session.AnswerOption.Safe];
// a lane that marked none gets the last, which is where every card on this
// surface has always put its way out.
func questionSafeAt(q session.Question) int {
	for i, option := range q.Options {
		if option.Safe {
			return i
		}
	}
	if len(q.Options) == 0 {
		return 0
	}
	return len(q.Options) - 1
}

// withdrawQuestion takes one off the block and leaves its one dim sentence
// where it was.
func (a *app) withdrawQuestion(q session.Question, reason string) {
	token := questionToken(q)
	// A QUESTION THAT NEVER REACHED A ROW IS STILL TAKEN BACK. It may be inside
	// a step the rule is still gathering, in which case the boundary must not
	// release a decision that stopped needing to be made
	// (questiondelivery.go's [questionDeliveryRule.forget]).
	a.questionReach.forget(q)
	kept := a.questions[:0]
	found := a.questionBatch != nil && a.questionBatch.withdraw(q)
	for _, open := range a.questions {
		if open.token() == token {
			found = true
			continue
		}
		kept = append(kept, open)
	}
	a.questions = kept
	delete(a.questionFolded, token)
	if !found {
		// A question this surface never drew leaves no line. The sentence is
		// there to explain a row somebody was looking at; printed under nothing
		// it is a report about a decision they were never asked to make.
		return
	}
	a.questionRecords = append(a.questionRecords, questionRecord{
		head: strings.TrimSpace(q.Head), withdrawn: strings.TrimSpace(reason), at: a.now(),
	})
	a.touch()
}

// questionQuieted is THE BOX IS NEVER MOVED UNDER A HAND, answered.
//
// A question may take its rows when the box is empty, or when the box has been
// still for [questionQuiet]. Both halves matter: the empty box is the ordinary
// case and costs no wait at all, and the pause is what keeps a question from
// being stuck behind a draft somebody typed and walked away from.
func (a *app) questionQuieted() bool {
	if strings.TrimSpace(a.input.String()) == "" {
		return true
	}
	return a.now().Sub(a.questionTyped) >= questionQuiet
}

// questionSettled reports whether this question has been on screen long enough
// to take a key. A question that has never been drawn has no shown stamp and is
// never settled — which is the guard doing its job on the frame the question
// arrives on.
func (a *app) questionSettled(q questionShown) bool {
	return !q.shown.IsZero() && a.now().Sub(q.shown) >= questionSettle
}

// markQuestionShown stamps the head question the first time a frame is drawn
// with it on. It is called from the DRAW, which is the only place that knows
// the question was actually on a screen — the settle guard is a claim about
// what a person could have seen.
func (a *app) markQuestionShown(token string) {
	for i := range a.questions {
		if a.questions[i].token() != token || !a.questions[i].shown.IsZero() {
			continue
		}
		a.questions[i].shown = a.now()
		return
	}
}

// ── the rule offer ──────────────────────────────────────────────────────────

// questionRule decides whether this question's answers row carries `r make it a
// rule for <scope>`.
//
// THE THIRD SAME-SHAPED YES, AND NEVER A HIDDEN RULE. The count is per SHAPE —
// the lane and the subject's name together, which is "the same question about
// the same thing" — and it is only ever an OFFER: nothing is written until
// somebody presses the key, and a row that a rule later answers says so out
// loud (`· your rule from <day> · change`).
func (a *app) questionRule(q *questionShown) {
	if q.question.Ask != session.AskPermission || len(q.question.Scope) == 0 {
		return
	}
	q.rule = a.questionYeses[questionShape(q.question)] >= questionRuleAfter-1
}

// questionShape is what "the same question again" means: the lane it came from
// and what it is about. The head is deliberately not in it — a gate that asks
// about `rm -rf build` and then about `rm -rf dist` is asking one question
// twice, and a shape keyed on the sentence would never notice.
func questionShape(q session.Question) string {
	subject := strings.TrimSpace(q.Subject.Name)
	if subject == "" {
		subject = strings.TrimSpace(q.Subject.Ref)
	}
	return string(q.Kind) + "/" + string(q.Ask) + "/" + subject
}

// questionRuleWord is what the `r` key offers, with the scope written into it:
// the widest scope the question said an answer could carry, in that scope's own
// word.
func questionRuleWord(q session.Question) string {
	scope := "this project"
	for _, one := range q.Scope {
		switch one {
		case session.ScopeAlways:
			scope = "everywhere"
		case session.ScopeProject:
			if scope != "everywhere" {
				scope = "this project"
			}
		case session.ScopeTask:
			if scope != "everywhere" && scope != "this project" {
				scope = "this task"
			}
		}
	}
	return "make it a rule for " + scope
}

// ── drawing ─────────────────────────────────────────────────────────────────

// questionHeight is how many rows the block spends. It is COUNTED by laying the
// rows out at the frame's own width rather than derived from a formula, for
// consent.go's reason: a block whose height and whose rows disagree puts the
// caret a row off the box.
func (a *app) questionHeight() int {
	if len(a.questions) == 0 && len(a.questionRecords) == 0 && a.sheetOpen() == 0 {
		return 0
	}
	width, _ := a.size()
	return len(a.questionRows(width))
}

// questionRows draws the block: the receipts and withdrawals that are still
// worth a row, then the head question in whichever of the three forms its
// evidence asks for.
//
// It is laid out by [app.chrome], directly above the input, because that is
// where this surface puts everything it wants answered.
func (a *app) questionRows(width int) []string {
	if len(a.questions) == 0 && len(a.questionRecords) == 0 && a.sheetOpen() == 0 {
		// The empty block, on the empty path: no spans to clear because none
		// were written, and nothing allocated (see [app.questionOpen]).
		return nil
	}
	a.questionSpans = nil
	if width < 1 {
		return nil
	}
	out := make([]string, 0, 8)
	for _, record := range a.questionRecordsShown() {
		out = append(out, a.questionRecordRow(record, width))
	}
	head, ok := a.questionHead()
	if !ok {
		// THE SHEET STANDS DOWN FOR A QUESTION BEING READ, and this is the
		// other half of that: with nothing on the block, the batch one step
		// gathered is what the rows are spent on (questionsheet.go).
		if a.questionQuieted() && a.sheetShowing() {
			out = append(out, a.questionSheetRows(a.questionBatch, width)...)
		}
		return out
	}
	if !a.questionQuieted() {
		// THE BOX IS NEVER MOVED UNDER A HAND. The question is open, the chip
		// is counting it and home can see it; what it may not do is take rows
		// out from under a sentence somebody is in the middle of.
		return out
	}
	a.markQuestionShown(head.token())
	switch a.questionForm(head.question) {
	case formsRatify:
		out = append(out, a.questionRatifyRows(head, width)...)
	case formsCard:
		out = append(out, a.questionCardRows(head, width)...)
	default:
		out = append(out, a.questionLineRows(head, width)...)
	}
	if more := len(a.questionOpen()) - 1; more > 0 {
		out = append(out, a.pal.dim(fit("  "+itoa(more)+" more", width)))
	}
	return out
}

// questionForm is which of the three forms this block draws one question in.
//
// FORMS PROMOTE AND NEVER DEMOTE (DESIGN.md). A lane that asked for a card gets
// a card even where the block could have drawn it on one row, because the lane
// is the thing that knows how much evidence there is; what this function
// decides is the floor, for the lanes that named no form at all.
func (a *app) questionForm(q session.Question) questionForms {
	if q.Ask == session.AskRatify {
		return formsRatify
	}
	switch q.Form {
	case session.FormCard, session.FormRoom, session.FormSheet:
		return formsCard
	case session.FormLine:
		return formsLine
	}
	if len(q.Options) > 2 || strings.TrimSpace(q.Reason) != "" {
		return formsCard
	}
	return formsLine
}

// questionMark is the one-cell glyph at the head of a question, and it is the
// vocabulary's own ([internal/tui2/tokens]) through the one door.
//
// AMBER, ALWAYS, AND ONLY HERE. `?` is [tokens.GNeedsHuman], whose binding says
// "waiting on a human (always amber)" in the vocabulary itself, and amber on
// this surface means that and nothing else. The block's WORDS keep the
// conversation's question hue ([palette.ask]), which docs/DESIGN-LANGUAGE.md
// pins and this wave does not move; the MARK is what carries the meaning, so
// the mark is what wears the meaning's colour.
func (a *app) questionMark() string {
	return a.pal.warnBold(a.icon(tokens.GNeedsHuman))
}

// questionLineRows is the line form: the subject's own row where there is one,
// then head and answers together on one row, then the reason.
func (a *app) questionLineRows(q questionShown, width int) []string {
	out := make([]string, 0, 3)
	if row, ok := a.questionSubjectRow(q, width); ok {
		out = append(out, row)
	}
	head := strings.TrimSpace(q.question.Head)
	a.questionSpans, a.questionSpanRow = nil, len(out)
	out = append(out, a.questionOffer(q, head+" ", formsLine, len(out), width))
	if reason := strings.TrimSpace(q.question.Reason); reason != "" {
		out = append(out, a.pal.dim(fit("  "+reason, width)))
	}
	return out
}

// questionCardRows is the card form: head, the reason and who asked, one row
// per answer, then the answers row.
func (a *app) questionCardRows(q questionShown, width int) []string {
	out := make([]string, 0, 8)
	out = append(out, a.questionMark()+" "+a.pal.ask(fit(strings.TrimSpace(q.question.Head), width-2)))
	if line := a.questionAttribution(q.question); line != "" {
		out = append(out, a.pal.dim(fit("  "+line, width)))
	}
	options := q.question.Options
	if len(options) > questionCardOptions {
		// Past the card's own bound the answers go on one row, which is the
		// line form's row drawn under a card's head. The digits are unchanged:
		// what a person gives up is the consequence beside each word, which is
		// the thing there is no room for rather than the thing they answer with.
		options = nil
	}
	for i, option := range options {
		out = append(out, a.questionOptionRow(q, i, option, width))
	}
	a.questionSpans, a.questionSpanRow = nil, len(out)
	out = append(out, a.questionOffer(q, "", formsCard, len(out), width))
	return out
}

// questionRatifyRows is the ratify line: the third rung of the ladder drawn as
// what it is — a statement about something already done, with the way back.
//
// NOTHING WAITS ON IT, which is the whole reason it is one row and wears the
// settled mark rather than the attention one. The work happened; a person who
// reads past it has ratified it by saying nothing, and that is the rung's
// bargain rather than a corner cut.
func (a *app) questionRatifyRows(q questionShown, width int) []string {
	head := strings.TrimSpace(q.question.Head)
	a.questionSpans, a.questionSpanRow = nil, 0
	line := a.pal.dim(a.icon(tokens.GSettled)) + " " + a.pal.ink(head)
	keys := a.questionAnswerKeys(q, formsRatify)
	tail := a.questionKeyTail(q, keys)
	if tail != "" && ansi.StringWidth(head)+ansi.StringWidth(tail)+2 <= width {
		return []string{line + a.pal.ask(tail)}
	}
	return []string{fit(line, width)}
}

// questionSubjectRow is the row the question is ABOUT, drawn by the renderer
// that already drew it in the transcript.
//
// IT SHOWS THE ROW THAT IS ALREADY THERE — consent.go's first decision, kept
// whole and generalised. The question is about something the transcript has
// drawn, so the block re-uses that row rather than describing it a second time
// in different words. Two renderings of one call is how a person ends up
// approving something other than what they read.
func (a *app) questionSubjectRow(q questionShown, width int) (string, bool) {
	at := a.questionSubjectAt(q.question)
	if at < 0 || at >= len(a.entries) {
		return "", false
	}
	if e := &a.entries[at]; e.kind == entryTool {
		return a.toolLine(e, at, true, width), true
	}
	return "", false
}

// questionSubjectAt finds the transcript row a question's subject names, or -1.
//
// THE CALL'S ID IS THE ANSWER WHEREVER THERE IS ONE, which is consent.go's own
// law and matters here for the same reason: a question that landed on the wrong
// row is a person reading one command and answering about another.
func (a *app) questionSubjectAt(q session.Question) int {
	if q.Subject.Kind != session.SubjectCall {
		return -1
	}
	call := strings.TrimSpace(q.Subject.CallID)
	if call == "" {
		return -1
	}
	for i := range a.entries {
		if a.entries[i].kind == entryTool && a.entries[i].callID == call {
			return i
		}
	}
	return -1
}

// questionAttribution is the dim line under a card's head: why now, and who is
// asking, in that order and joined by the surface's own separator.
//
// THE EMPTINESS LAW. No reason and no named asker is no row at all — not an
// empty one, and never the word "unknown".
func (a *app) questionAttribution(q session.Question) string {
	parts := make([]string, 0, 2)
	if reason := strings.TrimSpace(q.Reason); reason != "" {
		parts = append(parts, reason)
	}
	if who := questionAskerWord(q.Asker); who != "" {
		parts = append(parts, who)
	}
	return strings.Join(parts, " · ")
}

// questionAskerWord is who is asking, in the words a person would use.
//
// NO MACHINERY VOCABULARY. The engine's own [session.AskerKind] spellings are
// `model`, `engine`, `task`, `surface`, `window`, and three of those are words
// about the program's insides. What a person needs to know is whether a person,
// this program, or a piece of work that is running asked — and the surface
// asking about itself needs no attribution at all, because they are looking at
// it.
func questionAskerWord(asker session.Asker) string {
	if name := strings.TrimSpace(asker.Name); name != "" {
		return name
	}
	switch asker.Kind {
	case session.AskerModel, session.AskerEngine:
		return product
	case session.AskerWindow:
		return "another window"
	}
	return ""
}

// questionOptionRow is one answer on a card: its key, its word, and what taking
// it produces.
//
// THE ASKER'S PICK IS MARKED AND THE MARK IS NOT A CURSOR. [session.Pick] is
// what the thing asking RECOMMENDS, which is a fact about the question; the
// cursor is where this person's keyboard is, which is a fact about them. Only
// the confirmation kind has a cursor at all (see [questionSafeAt]), so on every
// other card the one mark on the rows is the recommendation and cannot be
// misread as "the key you are about to press".
func (a *app) questionOptionRow(q questionShown, at int, option session.AnswerOption, width int) string {
	key := strings.TrimSpace(option.Key)
	if key == "" {
		key = itoa(at + 1)
	}
	picked := q.question.Pick != nil && strings.TrimSpace(q.question.Pick.Key) == key
	cursored := q.question.Ask == session.AskConfirmation && at == q.pick
	mark, plainMark := "  ", "  "
	if picked {
		plainMark = a.icon(tokens.GCollapsed) + " "
		mark = a.pal.ask(plainMark)
	}
	word := strings.TrimSpace(option.Label)
	if word == "" {
		word = key
	}
	say := strings.TrimSpace(option.Consequence)
	text := "  " + plainMark + key + "  " + word
	if say != "" {
		text += "  " + say
	}
	if ansi.StringWidth(text) > width {
		return a.pal.ask(fit(text, width))
	}
	// The row is painted in pieces rather than nested, for the reason
	// [app.paintIdentity] states: these hues are raw SGR with an explicit
	// reset, so a colour inside a colour ends the outer one early.
	line := a.pal.ask("  ") + mark + a.pal.askBold(key) + a.pal.ask("  "+word)
	if say != "" {
		line += a.pal.dim("  " + say)
	}
	if cursored {
		// THE EMPHASIS LAW, and pickrow.go's own two moves: the ground ladder's
		// selected step behind exactly this row's cells, and nothing else. No
		// ring, no second colour, and the row does not reflow.
		return a.pal.background(line, 0, a.pal.ramp.selected)
	}
	return line
}

// questionOffer is the answers row: the digits that pick, then the keys from
// [questionKeys] that this question actually offers, then the clock.
//
// LEAD IS WHAT GOES IN FRONT OF THE ANSWERS on the line form — the question's
// own head, because a line is one row and the head has nowhere else to be. On a
// card the head is already a row of its own and the lead is empty.
//
// IT DEGRADES BY DROPPING THE TAIL, NEVER BY CUTTING AN ANSWER. An offer with
// its last option truncated is an offer that hides an answer; a person who
// cannot see the clock still has every answer they had.
func (a *app) questionOffer(q questionShown, lead string, form questionForms, row, width int) string {
	// Pairs: the words at even indices, the keys — the only bold cells on the
	// row — at odd ones, which is consent.go's own painting and is kept because
	// the keys are what a person is scanning for.
	parts := make([]string, 0, len(q.question.Options)*2+8)
	if lead != "" {
		parts = append(parts, lead)
	}
	spans := make([]choiceSpan, 0, len(q.question.Options))
	// Both forms open with two cells — the line's mark and its space, the
	// card's plain indent — so an answer's columns are the same arithmetic on
	// either, which is what lets one press resolve against either row.
	at := ansi.StringWidth(lead) + 2
	for i, option := range q.question.Options {
		if form == formsCard && len(q.question.Options) <= questionCardOptions {
			// The card already drew a row per answer, keys and all.
			break
		}
		key := strings.TrimSpace(option.Key)
		if key == "" {
			key = itoa(i + 1)
		}
		word := strings.TrimSpace(option.Label)
		if word == "" {
			word = key
		}
		chip, tail := "["+key+"]", " "+word
		spans = append(spans, choiceSpan{
			from: at, to: at + ansi.StringWidth(chip+tail), at: i,
		})
		at += ansi.StringWidth(chip + tail)
		parts = append(parts, chip, tail)
		if i < len(q.question.Options)-1 {
			parts = append(parts, "", " · ")
			at += 3
		}
	}
	// THE ROW DEGRADES BY DROPPING ITS TAIL, NEVER BY CUTTING AN ANSWER. The
	// verbs go from the end backwards until what is left fits, and `esc` is the
	// one that is never dropped — it is the way out, and a row with no way off
	// it is the modal block this one replaces. The options are never dropped at
	// all: an offer with an answer missing is an offer that hides an answer.
	keys := a.questionAnswerKeys(q, form)
	clock := a.questionClock(q)
	for {
		tail := a.questionVerbParts(q, keys, len(parts) > 0)
		line := strings.Join(parts, "") + strings.Join(tail, "")
		if ansi.StringWidth("  "+line+clock) <= width {
			parts = append(parts, tail...)
			a.questionSpans, a.questionSpanRow = spans, row
			return a.questionPaint(q, parts, clock, form)
		}
		if dropped, ok := questionDropVerb(keys); ok {
			keys = dropped
			continue
		}
		if clock != "" {
			// THE CLOCK OUTLIVES EVERY VERB AND DIES BEFORE ANY ANSWER. It is
			// the only thing on this row that says what happens if nobody
			// presses anything, so it is worth more than `[d] you decide` — and
			// it is worth less than an answer, because a countdown a person
			// cannot see is still a countdown while an answer they cannot see
			// is not an answer (consent.go settled that half first).
			clock = ""
			continue
		}
		parts = append(parts, tail...)
		a.questionSpans, a.questionSpanRow = spans, row
		return a.pal.ask(fit("  "+strings.Join(parts, ""), width))
	}
}

// questionVerbParts is the tail of an answers row: the keys from
// [questionKeys], as the paired cells [app.questionPaint] inks.
func (a *app) questionVerbParts(q questionShown, keys []questionVerb, lead bool) []string {
	parts := make([]string, 0, len(keys)*4)
	for _, verb := range keys {
		word := verb.word
		if verb.key == questionRuleKey {
			word = questionRuleWord(q.question)
		}
		if lead || len(parts) > 0 {
			parts = append(parts, "", " · ")
		}
		parts = append(parts, "["+questionKeySpelling(verb.key)+"]", " "+word)
	}
	return parts
}

// questionDropVerb gives up the least valuable verb still on the row, and
// reports whether it found one. A verb ranked zero is never given up.
func questionDropVerb(keys []questionVerb) ([]questionVerb, bool) {
	at := -1
	for i := range keys {
		if keys[i].giveUp == 0 {
			continue
		}
		if at < 0 || keys[i].giveUp > keys[at].giveUp {
			at = i
		}
	}
	if at < 0 {
		return keys, false
	}
	return append(append([]questionVerb{}, keys[:at]...), keys[at+1:]...), true
}

// questionKeySpelling is how one key is PRINTED, which is not always how
// bubbletea spells it: the space bar is a key with no visible character, and a
// row that said `[ ]` would be a row with a hole in it.
func questionKeySpelling(key string) string {
	// Every key in the table is now spelled as the terminal sends it
	// ([questionToggleKey] says why the space bar was the last exception), so
	// there is nothing left to translate — and this stays as the ONE place a
	// translation would go if a key ever needs one again.
	return key
}

// questionKeyTail is the ratify row's keys, on one short tail. It is
// [app.questionOffer]'s tail alone: a ratify line has no options to draw, so
// the digits half of that row is always empty.
func (a *app) questionKeyTail(q questionShown, keys []questionVerb) string {
	if len(keys) == 0 {
		return ""
	}
	parts := make([]string, 0, len(keys))
	for _, verb := range keys {
		parts = append(parts, "["+questionKeySpelling(verb.key)+"] "+verb.word)
	}
	return " · " + strings.Join(parts, " · ")
}

// questionPaint inks one answers row: the words in the question hue, the keys —
// the only bold cells on it — inside them, and the clock's tail dim on the end.
func (a *app) questionPaint(q questionShown, parts []string, clock string, form questionForms) string {
	out := a.pal.ask("  ")
	if form == formsLine {
		out = a.questionMark() + " "
	}
	for i, part := range parts {
		if i%2 == 1 {
			out += a.pal.askBold(part)
			continue
		}
		out += a.pal.ask(part)
	}
	return out + a.pal.dim(clock)
}

// questionClock is the answers row's tail: how the silence is being held.
//
// SILENCE IS NEVER A NO. There is exactly one policy on this surface that acts
// without an answer — [session.PolicyRecommendThenAuto], the task proposal's,
// where the card is a chance to redirect rather than a gate — and its tail says
// what it is going to do, in those words. Every other question's clock says
// `waiting`, and waiting is what it does: F41 was a hidden ten-second timer
// that recorded "denied" and killed work nobody refused, and that is the one
// answer this surface must never give on somebody's behalf.
func (a *app) questionClock(q questionShown) string {
	if word := a.questionClockWord(q); word != "" {
		return " · " + word
	}
	return ""
}

// questionClockWord is that tail without the separator that joins it to a line
// of words.
func (a *app) questionClockWord(q questionShown) string {
	if q.question.Policy.Kind != session.PolicyRecommendThenAuto || q.question.Deadline.IsZero() {
		return ""
	}
	left := q.question.Deadline.Sub(a.now())
	if left <= 0 {
		return ""
	}
	// THE POLICY LINE, NOT A BARE NUMBER. `auto-starts in 9s` was machinery
	// describing itself; what a person needs is which answer is about to be
	// taken and when, which is the pick's own word and the clock together.
	word := "starts on its own"
	if q.question.Pick != nil {
		if option, ok := q.question.Option(q.question.Pick.Key); ok {
			if label := strings.TrimSpace(option.Label); label != "" {
				word = label
			}
		}
	}
	tail := word + " in " + countdownWord(left)
	if q.ruled {
		// THE ROW WEARS ITS RULE. `D` is already on the answers row and is the
		// door that changes it ([questionKeys]), so what this adds is the fact
		// and not a second key: the clock is running because of something this
		// project was told to do, and a person watching it run is owed that.
		tail += " · " + questionOwnRuleWord
	}
	return tail
}

// questionOwnRuleWord is that half, spelled once and quoted in the manual.
const questionOwnRuleWord = "your rule"

// questionAnimating reports whether a clock is running down, which is what
// keeps the paint clock turning while a question waits (app.go's [app.paint]).
func (a *app) questionAnimating() bool {
	if len(a.questions) == 0 {
		return false
	}
	head, ok := a.questionHead()
	if !ok {
		return false
	}
	return a.questionClockWord(head) != ""
}

// ── the receipt and the withdrawn line ──────────────────────────────────────

// questionRecordsShown is the receipts still worth a row.
//
// THEY ARE BOUNDED AND THEY FADE. A record is a statement about a decision
// somebody has just taken, and it belongs above the box for as long as it is
// news — after that it is history, and history lives in the transcript and in
// `decisions.jsonl` rather than in the chrome. So the block keeps the last
// [questionRecordsKept] and drops each after [questionRecordFor].
func (a *app) questionRecordsShown() []questionRecord {
	if len(a.questionRecords) == 0 {
		return nil
	}
	out := make([]questionRecord, 0, questionRecordsKept)
	for _, record := range a.questionRecords {
		if a.now().Sub(record.at) > questionRecordFor {
			continue
		}
		out = append(out, record)
	}
	if len(out) > questionRecordsKept {
		out = out[len(out)-questionRecordsKept:]
	}
	return out
}

const (
	// questionRecordsKept is how many receipts stand above the box at once. Two
	// is a person answering a queue and seeing what they just did; a column of
	// them is a block that has become a log.
	questionRecordsKept = 2
	// questionRecordFor is how long one stays. Half a minute is long enough to
	// read a line somebody caused and short enough that it is gone before it
	// becomes furniture.
	questionRecordFor = 30 * time.Second
)

// questionRecordRow draws one: the receipt, or the withdrawn line.
func (a *app) questionRecordRow(record questionRecord, width int) string {
	if record.withdrawn != "" {
		// WITHDRAWN, WITH A REASON, and never the word "cancelled": what a
		// person experiences is the thing no longer needing them.
		text := "  " + a.icon(tokens.GWithdrawn) + " " + record.head +
			" — no longer needed · " + record.withdrawn
		return a.pal.dim(fit(text, width))
	}
	text := "  decided " + record.record.Line()
	if record.reversible {
		text += " · " + questionCommentKey + " change"
	}
	return a.pal.dim(fit(text, width))
}

// ── answering ───────────────────────────────────────────────────────────────

// answerQuestion sends one answer through the one door, and leaves the receipt.
//
// THE ONE DOOR IS THE ENGINE'S ([session.Agent.ResolveQuestion]) for every
// question the engine raised, and the question's own closure for the ones this
// surface raised about itself. There is no third path and no place that knows
// what "yes" means twice.
//
// A REFUSED ANSWER LEAVES THE QUESTION OPEN. The engine's door returns an error
// for an answer that names nothing and for a lane it does not take; neither is
// a decision, so neither closes anything and neither writes a receipt.
func (a *app) answerQuestion(q questionShown, answer session.Answer) {
	answer.Kind = q.question.Kind
	answer.ID = q.question.ID
	answer.Ref = q.question.Ref
	answer.Ask = q.question.Ask
	if answer.At.IsZero() {
		answer.At = a.now()
	}
	if answer.DecidedBy == "" {
		answer.DecidedBy = session.DecidedByPerson
	}
	if q.local != nil {
		q.local(answer)
	} else {
		doors, ok := a.questionDoors()
		if !ok {
			return
		}
		if err := doors.ResolveQuestion(answer); err != nil {
			return
		}
	}
	a.closeQuestion(q, answer)
}

// closeQuestion takes an answered question off the block, writes its receipt,
// and counts the yes towards the rule offer.
//
// THE RECEIPT IS BUILT HERE AND NOT WAITED FOR. The engine emits
// [session.EventQuestionAnswered] with the record on it, and that event is what
// a SECOND window learns from; this window already knows, and a person who
// pressed a key and watched the row sit unchanged for a round trip would press
// it again.
func (a *app) closeQuestion(q questionShown, answer session.Answer) {
	token := q.token()
	kept := a.questions[:0]
	for _, open := range a.questions {
		if open.token() == token {
			continue
		}
		kept = append(kept, open)
	}
	a.questions = kept
	delete(a.questionFolded, token)
	a.recordQuestion(q, answer)
	a.countQuestionYes(q, answer)
	a.touch()
}

// recordQuestion writes the receipt from the question and the answer together,
// exactly as the engine writes its own record from the same two things
// (internal/session's decisionRecordOf) — so the line above the box and the
// line in `decisions.jsonl` are the same sentence.
func (a *app) recordQuestion(q questionShown, answer session.Answer) {
	labels := questionLabels(q.question, answer.Keys())
	record := session.DecisionRecord{
		ID: q.question.ID, Ref: q.question.Ref,
		Kind: q.question.Kind, Ask: q.question.Ask,
		Head: strings.TrimSpace(q.question.Head), Subject: q.question.Subject,
		Picked: answer.Keys(), Labels: labels, Change: answer.Words(),
		By: answer.DecidedBy, Stakes: q.question.Stakes, Scope: answer.Scope,
		At: answer.At,
	}
	a.questionRecords = append(a.questionRecords, questionRecord{
		record: record, head: record.Head, at: answer.At, reversible: record.Reversible(),
	})
}

// countQuestionYes counts one answer towards the third same-shaped yes that
// offers a rule. Only an answer that GRANTED something counts: a person who
// says no three times has not formed a habit worth writing down, they have
// answered three questions.
func (a *app) countQuestionYes(q questionShown, answer session.Answer) {
	if q.question.Ask != session.AskPermission {
		return
	}
	option, ok := q.question.Option(answer.FirstKey())
	if !ok || option.Safe {
		return
	}
	if a.questionYeses == nil {
		a.questionYeses = map[string]int{}
	}
	a.questionYeses[questionShape(q.question)]++
}

// foldQuestion is `esc`: LATER, and nothing is cancelled.
//
// The question stays open, the turn or the task stays paused on it, the chip
// keeps counting it, and the rows come off the screen so the person can type.
// It is the whole of what makes this block not modal.
func (a *app) foldQuestion(q questionShown) {
	if a.questionFolded == nil {
		a.questionFolded = map[string]bool{}
	}
	a.questionFolded[q.token()] = true
	a.touch()
}

// raiseFolded is the chip's own key: it unfolds the NEWEST open question and
// puts it back above the box.
//
// THE NEWEST AND NOT THE OLDEST, which is the one place this surface answers a
// queue backwards, and it is the chip's own argument: a person pressing the
// chip has just seen a count change, and what they are asking about is the
// thing that changed it. Answering the head of the queue instead would be the
// surface deciding they meant something else.
func (a *app) raiseFolded() {
	// THE SHEET COMES BACK FIRST. It is the newest thing anybody put off — a
	// batch arrives at a step's end, after every question already on the block
	// — and it is what the chip's count is mostly made of when there is one.
	if a.sheetOpen() > 0 && a.questionBatchFolded {
		a.questionBatchFolded = false
		a.touch()
		return
	}
	if len(a.questions) == 0 {
		return
	}
	newest, at := time.Time{}, -1
	for i, q := range a.questions {
		if !a.questionFolded[q.token()] {
			continue
		}
		if at < 0 || q.question.Asked.After(newest) {
			newest, at = q.question.Asked, i
		}
	}
	if at >= 0 {
		delete(a.questionFolded, a.questions[at].token())
	}
	a.touch()
}

// ── the keyboard ────────────────────────────────────────────────────────────
//
// THE BLOCK TAKES ONLY THE KEYS IT DRAWS, and that sentence is the whole
// difference between this and the block it replaces. consent.go owned every
// keystroke while it was up and said why: with no way out but an answer, a key
// that fell through would be typing into a conversation that could not move.
// esc is `later` now, so there is a way out, so there is no reason to hold the
// keyboard — and holding it was costing the one thing the ladder's last rung
// needs, which is somewhere to type the answer that was not on offer.
//
// A LETTER IS THE QUESTION'S OVER AN EMPTY BOX AND NOWHERE ELSE. That is the
// rule every key on this surface that is also a letter is held to (task.go's
// [app.taskKey], room.go, stop.go), and it is what makes `d`, `r` and `u` safe
// to put on a row: the moment there are words in the box, every printable key
// belongs to the composer and the only key still the question's is `esc`.
//
// AND THE WORDS IN THE BOX ARE THE ANSWER, on a question the turn is waiting
// on. `enter` under a blocking question sends what was typed as the answer's
// own words rather than as a message into a conversation that cannot carry it
// — which is what the box under a task proposal has always been for (task.go
// calls it the redirect lane) said once, for every lane.

// questionKey routes one keypress while the block is up, and reports whether it
// took it.
func (a *app) questionKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	head, ok := a.questionHead()
	if !ok {
		// WHAT IS DRAWN IS WHAT TAKES THE KEY. With nothing on the block the
		// sheet has the rows, so the sheet has the keyboard (questionsheet.go).
		return a.questionSheetKey(msg)
	}
	if !a.questionQuieted() {
		return nil, false
	}
	key := msg.String()
	if key == "ctrl+c" {
		// Leaving is never modal, and mid-turn ctrl+c is the interrupt, which
		// releases whatever the question was holding the honest way.
		return nil, false
	}
	// THE SETTLE GUARD, AND IT DROPS RATHER THAN DEFERS. A key that arrived
	// before the question had been on screen long enough was aimed at whatever
	// was there before it, and applying it late is applying it to the wrong
	// question rather than to none.
	if !a.questionSettled(head) {
		return nil, true
	}
	if key == questionLaterKey {
		a.foldQuestion(head)
		return nil, true
	}
	typing := strings.TrimSpace(a.input.String()) != ""
	if key == questionEnterKey {
		return a.questionEnter(head, typing)
	}
	if typing {
		// EVERY PRINTABLE KEY BELONGS TO THE COMPOSER. The question is still
		// there, still counted, still answerable the moment the box is clear.
		return nil, false
	}
	if cmd, taken := a.questionOptionKey(head, key); taken {
		return cmd, true
	}
	return a.questionVerbKey(head, key)
}

// questionEnter is `enter` under a question: send the words where there are
// some, and take the pick where there are not.
//
// `enter` TAKES THE PICK ONLY WHEN THERE IS ONE (DESIGN.md's key grammar, and
// the emptiness law behind it). A question with no pick draws no `enter →` line
// and this hands the key back, so enter on an empty box does whatever it always
// did.
func (a *app) questionEnter(head questionShown, typing bool) (tea.Cmd, bool) {
	if typing {
		if !head.question.Blocking.Turn || !questionTakesWords(head.question) {
			// The conversation can carry this sentence, so it does. A question
			// that is not holding the turn has no claim on the box.
			return nil, false
		}
		words := strings.TrimSpace(a.input.String())
		a.input.reset()
		a.answerQuestion(head, session.Answer{Change: words})
		return nil, true
	}
	if head.question.Ask == session.AskConfirmation {
		return a.questionPick(head, head.pick), true
	}
	if head.question.Pick == nil || strings.TrimSpace(head.question.Pick.Key) == "" {
		return nil, false
	}
	return a.questionAnswerKey(head, strings.TrimSpace(head.question.Pick.Key)), true
}

// questionOptionKey is a key that names one of the question's own answers: a
// digit, or the letter a lane fixed on the option itself (task-states' `a`,
// `n`, `s` are the only letters any lane uses, and they are read off the option
// rather than re-decided here).
func (a *app) questionOptionKey(head questionShown, key string) (tea.Cmd, bool) {
	if head.question.Ask == session.AskConfirmation {
		// ←/→ WALK THE ANSWERS, which is stop.go's and tabclose.go's own
		// grammar and the one form on this block that has a cursor at all.
		switch key {
		case "left", "shift+tab":
			if head.pick > 0 {
				a.moveQuestionPick(head, head.pick-1)
			}
			return nil, true
		case "right", "tab":
			if head.pick < len(head.question.Options)-1 {
				a.moveQuestionPick(head, head.pick+1)
			}
			return nil, true
		}
	}
	for _, option := range head.question.Options {
		if strings.TrimSpace(option.Key) == key {
			return a.questionAnswerKey(head, key), true
		}
	}
	return nil, false
}

// moveQuestionPick walks the cursor on the one form that has one.
func (a *app) moveQuestionPick(head questionShown, to int) {
	for i := range a.questions {
		if a.questions[i].token() == head.token() {
			a.questions[i].pick = to
			a.touch()
			return
		}
	}
}

// questionPick answers with the option at an index, which is what the cursor
// and the pointer both resolve to.
func (a *app) questionPick(head questionShown, at int) tea.Cmd {
	if at < 0 || at >= len(head.question.Options) {
		return nil
	}
	return a.questionAnswerKey(head, strings.TrimSpace(head.question.Options[at].Key))
}

// questionAnswerKey sends one key as the answer.
func (a *app) questionAnswerKey(head questionShown, key string) tea.Cmd {
	if key == "" {
		return nil
	}
	answer := session.Answer{Key: key, Picked: []string{key}}
	if scope := questionScopeOf(head.question, key); scope != "" {
		answer.Scope = scope
	}
	a.answerQuestion(head, answer)
	return nil
}

// questionScopeOf is how far one answer reaches, where the option says.
//
// THE WIDENING ANSWER CARRIES THE WIDEST SCOPE THE QUESTION OFFERED, and every
// other answer carries `once`. [session.AnswerOption.Widening] is the lane's own
// mark for "this grants more than the question asked about", so the surface
// never has to guess which of a lane's answers is the wide one.
func questionScopeOf(q session.Question, key string) session.AnswerScope {
	option, ok := q.Option(key)
	if !ok || !option.Widening {
		return session.ScopeOnce
	}
	widest := session.ScopeOnce
	for _, scope := range q.Scope {
		switch scope {
		case session.ScopeAlways:
			return session.ScopeAlways
		case session.ScopeProject:
			widest = session.ScopeProject
		case session.ScopeTask:
			if widest == session.ScopeOnce {
				widest = session.ScopeTask
			}
		}
	}
	return widest
}

// questionVerbKey routes the keys from [questionKeys] that are not answers: the
// ones that say something ABOUT the decision rather than giving it.
//
// It refuses a key the row did not draw, which is [app.questionAnswerKeys]
// read a second time rather than a second list — NO KEY DOES ANYTHING THAT IS
// NOT DRAWN ON SCREEN RIGHT NOW (verbstrip.go states the law this surface holds
// itself to).
func (a *app) questionVerbKey(head questionShown, key string) (tea.Cmd, bool) {
	form := a.questionForm(head.question)
	offered := false
	for _, verb := range a.questionAnswerKeys(head, form) {
		if verb.key == key {
			offered = true
			break
		}
	}
	if !offered {
		return nil, false
	}
	switch key {
	case questionWalkKey:
		// The arrows are routed as arrows ([app.questionOptionKey]); the pair's
		// spelling is only ever drawn.
		return nil, false
	case questionOpenKey:
		return a.openQuestionRoom(head), true
	case questionDecideKey:
		// YOU DECIDE hands the decision back to the asker and RECORDS that this
		// is what happened, which is the whole point of the answer: a decision
		// nobody made is a decision nobody can find later.
		a.answerQuestion(head, session.Answer{
			Key:       questionDecidedKeyOf(head.question),
			DecidedBy: session.DecidedByAsker,
		})
		return nil, true
	case questionDialKey:
		return a.questionDial(head), true
	case questionRuleKey:
		return a.questionMakeRule(head), true
	case questionUndoKey:
		return a.questionUndo(head), true
	case questionCommentKey, questionAskBackKey:
		// BOTH OPEN THE BOX RATHER THAN ANSWERING. `c` is "I will take one of
		// these but not as it stands" and `?` is "answer me this first"; each
		// needs a sentence, and the box is where sentences are typed on this
		// surface. The question stays open and the words go with the next
		// enter ([app.questionEnter]).
		//
		// Nothing is focused and nothing is opened: the box below is ALREADY
		// live and always was, which is what NEVER MODAL means. The key's whole
		// work is to be taken rather than to fall through and type its own
		// letter into the sentence it is inviting.
		return nil, true
	}
	return nil, false
}

// questionDecidedKeyOf is which answer `d you decide` gives: the asker's own
// pick where it named one, and the safe answer where it did not.
//
// A QUESTION WITH NO PICK HANDED BACK IS THE SAFE ANSWER AND NOT A GUESS. The
// person said "you choose"; with nothing recommended there is nothing to
// choose, and taking the answer that loses nothing is the only reading that
// cannot cost them something they did not agree to.
func questionDecidedKeyOf(q session.Question) string {
	if q.Pick != nil && strings.TrimSpace(q.Pick.Key) != "" {
		return strings.TrimSpace(q.Pick.Key)
	}
	if at := questionSafeAt(q); at < len(q.Options) {
		return strings.TrimSpace(q.Options[at].Key)
	}
	return ""
}

// questionMakeRule is `r`: the third same-shaped yes, taken and written down.
//
// It answers with the widest scope the question offered, which is what makes it
// a rule rather than a yes — and the receipt says so, because A RULE IS NEVER
// HIDDEN.
func (a *app) questionMakeRule(head questionShown) tea.Cmd {
	key := questionDecidedKeyOf(head.question)
	for _, option := range head.question.Options {
		if option.Widening {
			key = strings.TrimSpace(option.Key)
			break
		}
	}
	if key == "" {
		return nil
	}
	scope := session.ScopeProject
	for _, one := range head.question.Scope {
		if one == session.ScopeAlways {
			scope = session.ScopeAlways
		}
	}
	a.answerQuestion(head, session.Answer{
		Key: key, Picked: []string{key}, Scope: scope,
		Why: "a rule, from the third time this was asked",
	})
	return nil
}

// questionUndo is `u` on a ratify line: take back what was already done.
//
// It answers with the option the lane marked SAFE, which on a ratify question
// is the one that puts things back — `already done` is the other answer and is
// what happens by saying nothing.
func (a *app) questionUndo(head questionShown) tea.Cmd {
	at := questionSafeAt(head.question)
	if at >= len(head.question.Options) {
		return nil
	}
	return a.questionPick(head, at)
}

// questionDial is `D`: decide questions of this shape from now on, without
// asking.
//
// THE DIAL'S STORAGE IS LANE E2'S (`autonomy.json`, per project) AND THIS IS
// THE SEAM ONTO IT — wired now, through [session.Agent.SetAutonomy] and the wire
// frame that carries it to an engine in another process (internal/remote's
// MethodSetAutonomy, which every local chat window goes through).
//
// IT DOES BOTH HALVES OF THE PROMISE. The standing half writes the shape into
// the project's own settings, so the NEXT question of that shape is answered
// without asking; the immediate half answers the one in front of the person,
// because `D` is pressed while looking at a question and a key that set a
// setting and left that question sitting there would read as having done
// nothing. A session with nowhere to keep the setting still answers this one:
// losing the standing half is not a reason to lose the answer.
func (a *app) questionDial(head questionShown) tea.Cmd {
	key := questionDecidedKeyOf(head.question)
	if key == "" {
		return nil
	}
	// `D` DOES BOTH HALVES OF WHAT ITS WORD SAYS. It answers the question in
	// front of the person, and it writes the rule that answers the next one —
	// through the engine's own door (autonomysheet.go), which refuses the two
	// shapes no rule may ever cover. Before this wave it only did the first, so
	// `decide these from now on` was a key that decided exactly one.
	//
	// AND THE RULE IS SAID OUT LOUD, because NEVER A HIDDEN RULE: the row that
	// answers under it afterwards wears `· your rule`, and this line is the
	// moment it was written.
	if word := a.dialKind(head.question.Ask); word != "" {
		a.note(word)
	}
	a.answerQuestion(head, session.Answer{
		Key: key, Picked: []string{key}, Scope: session.ScopeProject,
		Why: "decide these from now on",
	})
	return nil
}

// dialKind writes this project's rule for one shape of question, and answers
// with what to say about it — "" where there was nothing to write or the engine
// refused it.
func (a *app) dialKind(kind session.AskKind) string {
	agent, ok := a.agent.(autonomyAgent)
	if !ok || kind == "" {
		return ""
	}
	if err := agent.SetAutonomy(kind, session.Policy{Kind: session.PolicyDecide}); err != nil {
		// THE REFUSAL IS THE PERSON'S TO READ. The engine turns down a rule
		// over a clarification and over anything destructive, and a key that
		// silently did nothing would be a key that promised a rule and wrote
		// none.
		return err.Error()
	}
	a.autonomyChanged()
	return questionShapeWord(kind) + " · " + autonomyDecideWord + questionDialFromNowWord
}

// questionDialFromNowWord is the tail of that line: where the rule reaches and
// how to take it back.
const questionDialFromNowWord = " from now on · /autonomy to change it"

// openQuestionRoom walks into the room over this question — lane S2's page.
//
// THE DOOR IS CALLED AND NEVER RE-IMPLEMENTED. The page is questionroom.go's,
// and everything it needs travels in the [questionShown] this block was already
// holding — the question, its resolver, whether a rule is on offer, whether an
// undo would reach anything — so opening one hands over what is already in hand
// rather than building a second reading of the same question.
//
// THE BLOCK GOES WITH IT. A question drawn twice — pinned above the box and
// spread over the page — is one question a person could answer in two places
// with two different sets of keys on screen at once, and the fold is exactly the
// state the chip already knows how to bring back.
func (a *app) openQuestionRoom(head questionShown) tea.Cmd {
	a.foldQuestion(head)
	a.raiseQuestionRoom(head)
	return nil
}

// ── the pointer ─────────────────────────────────────────────────────────────

// questionPress resolves a click on the block and reports whether it took it.
//
// IT DOES NOT SWALLOW WHAT IT DID NOT DRAW, which is the block's own law said
// to the pointer: consent.go swallowed every press inside itself because it was
// modal and a press falling through would expand a tool call while somebody was
// denying one. Nothing here is modal, so a press that hits no answer falls
// through to whatever is under it, exactly as a key does.
func (a *app) questionPress(x, y int) bool {
	head, ok := a.questionHead()
	if !ok || a.copy.on {
		return false
	}
	mark, found := a.chromeAt(y)
	if !found || mark.kind != chromeQuestion || mark.index != a.questionSpanRow {
		return false
	}
	for _, span := range a.questionSpans {
		if x >= span.from && x < span.to {
			a.questionPick(head, span.at)
			return true
		}
	}
	return false
}

// questionMark is what the pointer is over on row i of the block.
func (a *app) questionRowMark(i int) chromeRow {
	return chromeRow{kind: chromeQuestion, index: i}
}

// ── the chip ────────────────────────────────────────────────────────────────

// questionSegment is the status line's chip: `? 3 questions · alt+a`.
//
// IT IS REACHABLE FROM EVERY PAGE, which is the whole reason it is on the
// status row rather than in the block: the block is above the box in a
// conversation, and a person standing on home, in a room or on the tasks place
// has no block to look at. The chip is the one thing that is always there while
// anything is waiting.
//
// THE EMPTINESS LAW. Nothing open is nothing drawn — never `0 questions`.
func (a *app) questionSegment() string {
	count := a.questionCount()
	if count == 0 {
		return ""
	}
	word := " questions"
	if count == 1 {
		word = " question"
	}
	return a.icon(tokens.GNeedsHuman) + " " + itoa(count) + word + " · " + questionChipKey
}

// questionChipKeyPress is [questionChipKey] from wherever a person is standing:
// it brings the newest open question back above the box, and takes them to the
// conversation it belongs to.
func (a *app) questionChipKeyPress(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if msg.String() != questionChipKey || a.questionCount() == 0 {
		return nil, false
	}
	// A QUESTION NOBODY CAN SEE IS A QUESTION NOBODY CAN ANSWER. The block is
	// drawn above the box in the conversation, so the chip's key puts the
	// person there — the same move a question ARRIVING makes on its way in.
	a.closeSettings()
	a.closeExpand()
	a.closeHome()
	a.raiseFolded()
	return nil, true
}

// ── the lane ────────────────────────────────────────────────────────────────

// watchQuestions opens the standing subscription and starts pumping it.
//
// IT IS A LANE OF ITS OWN AND NOT THE TURN'S STREAM, which is the engine's own
// decision read back here: a question outlives the turn that raised it, it is
// re-sent whole to a surface that attaches mid-flight, and half of the lanes
// that raise one are not in a turn at all (a landed task, a run at its fuel
// gate). Folding it into the turn's stream would mean a question that only
// exists while somebody is being spoken to.
//
// It is called wherever [app.watchRuns] is, and for the same reason: the
// channel belongs to the agent that handed it over, so a replaced conversation
// gets a new one.
func (a *app) watchQuestions() tea.Cmd {
	doors, ok := a.questionDoors()
	if !ok {
		return nil
	}
	if a.questionWatch != nil {
		a.questionWatch()
	}
	a.questionGen++
	var lane <-chan session.Event
	lane, a.questionWatch = doors.WatchQuestions()
	a.questionLane = lane
	return waitQuestion(lane, a.questionGen)
}

// waitQuestion takes one event off the lane and asks for the next.
func waitQuestion(ch <-chan session.Event, gen int) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return questionLaneClosedMsg{gen: gen}
		}
		return questionEventMsg{gen: gen, ev: ev}
	}
}

// questionEvent folds one event from the lane in and re-arms the pump.
func (a *app) questionEvent(ev session.Event) tea.Cmd {
	return tea.Batch(a.questionFold(ev), waitQuestion(a.questionLane, a.questionGen), a.wake())
}

// questionFold is what the lane's three kinds DO.
//
// AN ANSWERED QUESTION FROM THE LANE IS SOMEBODY ELSE'S ANSWER. This window
// closes its own the moment it sends one ([app.closeQuestion]) rather than
// waiting for the round trip, so an EventQuestionAnswered that still finds the
// question open here was answered in another window or by the dial — which is
// FIRST ANSWER WINS, and the honest thing to draw is the receipt saying who
// decided and what.
func (a *app) questionFold(ev session.Event) tea.Cmd {
	if ev.Question == nil {
		return nil
	}
	switch ev.Kind {
	case session.EventQuestion:
		if !a.questionDrawnHere(*ev.Question) {
			return nil
		}
		if _, ok := a.questionDoors(); !ok {
			// A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN. With no
			// resolve door there is nowhere for an answer to go, so the honest
			// thing is not to put the question on screen — a row somebody can
			// read and press and never resolve is worse than one they answer in
			// the window that owns it. That is the state a `--host` window is in
			// today: events cross the wire and [session.Agent.ResolveQuestion]
			// does not.
			return nil
		}
		// WHERE IT GOES IS NOT DECIDED HERE. One rule answers that for the
		// block, for home, for the notification and for the bell, and it is the
		// only thing on this surface that knows what "away" means
		// (questiondelivery.go's [app.deliverQuestion]).
		return a.deliverQuestion(*ev.Question)
	case session.EventQuestionWithdrawn:
		reason := ""
		if ev.Question.Withdrawn != nil {
			reason = ev.Question.Withdrawn.Reason
		}
		a.withdrawQuestion(*ev.Question, reason)
	case session.EventQuestionAnswered:
		if ev.Answer == nil {
			return nil
		}
		a.foldOthersAnswer(*ev.Question, *ev.Answer)
	}
	return nil
}

// questionRaceFor is how long after this window's own answer another window's
// answer to the same question is still worth a row.
//
// A SECOND, WHICH IS THE LAW'S OWN NUMBER (docs/design/questions/DESIGN.md:
// "a differing answer inside a second is shown, not merged"). Past it the other
// window was simply late, and answers.go's own first law — late answers are
// ignored and nothing says so — is the honest reading: somebody who answered a
// minute ago has moved on, and a line about it would be news about nothing.
const questionRaceFor = time.Second

// foldOthersAnswer is FIRST ANSWER WINS, drawn.
//
// Three things can be true when the lane says a question was answered, and they
// are three different rows:
//
//   - THE QUESTION IS STILL OPEN HERE, so somebody answered it somewhere else.
//     The receipt is written with [session.DecidedByWindow] on it, which is what
//     keeps it from saying `you` about a key pressed on another screen.
//   - THIS WINDOW ANSWERED IT, and the lane is telling us what we already know
//     ([app.closeQuestion] does not wait for the round trip). Nothing is drawn.
//   - THIS WINDOW ANSWERED IT DIFFERENTLY, within [questionRaceFor]. Both are
//     shown and NEITHER IS MERGED: the first answer is the decision and the
//     second is a person finding out their key did not land.
func (a *app) foldOthersAnswer(q session.Question, answer session.Answer) {
	shown := questionShown{question: q}
	if a.questionIsOpen(shown.token()) {
		if answer.DecidedBy == "" || answer.DecidedBy == session.DecidedByPerson {
			answer.DecidedBy = session.DecidedByWindow
		}
		a.closeQuestion(shown, answer)
		return
	}
	mine, ok := a.questionAnswerHere(q)
	if !ok || a.now().Sub(mine.at) > questionRaceFor {
		return
	}
	if sameAnswer(mine.record.Picked, answer.Keys()) {
		return
	}
	a.questionRecords = append(a.questionRecords, questionRecord{
		record: session.DecisionRecord{
			ID: q.ID, Ref: q.Ref, Kind: q.Kind, Ask: q.Ask,
			Head: strings.TrimSpace(q.Head), Subject: q.Subject,
			Picked: answer.Keys(), Labels: questionLabels(q, answer.Keys()),
			By: session.DecidedByWindow, Stakes: q.Stakes, At: answer.At,
		},
		head: strings.TrimSpace(q.Head), at: a.now(),
	})
	a.note(questionRaceWord)
	a.touch()
}

// questionRaceWord is what a person is told when two windows answered one
// question at almost the same moment. It says which answer counted, because
// that is the only thing they cannot see from the two rows above it.
const questionRaceWord = "two windows answered that · the first one is the decision"

// questionIsOpen reports whether this block still holds one question.
func (a *app) questionIsOpen(token string) bool {
	for _, open := range a.questions {
		if open.token() == token {
			return true
		}
	}
	return false
}

// questionAnswerHere is the receipt this window already wrote for one question,
// when it wrote one recently enough to still be above the box.
func (a *app) questionAnswerHere(q session.Question) (questionRecord, bool) {
	for i := len(a.questionRecords) - 1; i >= 0; i-- {
		record := a.questionRecords[i]
		if record.withdrawn != "" {
			continue
		}
		if record.record.Kind == q.Kind && record.record.ID == q.ID && record.record.Ref == q.Ref {
			return record, true
		}
	}
	return questionRecord{}, false
}

// sameAnswer reports whether two answers picked the same keys, in the same
// order. Order matters on a checklist, where `1,3` and `3,1` are one answer and
// `1,3` and `1,2` are not.
func sameAnswer(one, two []string) bool {
	if len(one) != len(two) {
		return false
	}
	for i := range one {
		if one[i] != two[i] {
			return false
		}
	}
	return true
}

// questionLabels is each picked key in the question's OWN word for it, falling
// back to the key where a lane offered none.
func questionLabels(q session.Question, keys []string) []string {
	labels := make([]string, 0, len(keys))
	for _, key := range keys {
		if option, ok := q.Option(key); ok && strings.TrimSpace(option.Label) != "" {
			labels = append(labels, strings.TrimSpace(option.Label))
			continue
		}
		labels = append(labels, key)
	}
	return labels
}

// questionDrawnHere is which lanes THIS BLOCK draws, and it is the migration's
// seam rather than a permanent shape.
//
// EVERY LANE ENDS UP HERE. Until each older block is deleted its lane is left
// to it, because a question drawn twice on one screen is worse than a question
// drawn in the older place: a person answering the second copy of a decision
// they already made is the exact failure the one-renderer wave exists to end.
// So this list GROWS as blocks are retired, and it is the one place that says
// which are done.
func (a *app) questionDrawnHere(q session.Question) bool {
	switch q.Kind {
	case session.QuestionSubharnessAsk, session.QuestionFuel, session.QuestionConflict:
		// The three lanes the audit found with a resolver and NOTHING ANYWHERE
		// that drew them: work stopped on a question no surface in this product
		// could put to a person. They are drawn here first because there is no
		// older block to retire — this block is the only one they have ever had.
		return true
	case session.QuestionAsk:
		// AND THE MODEL'S OWN DOOR, for the same reason and more sharply. The
		// `ask` tool (session's tools_ask.go) is the last rung of the ladder,
		// it has no older block anywhere, and until it is drawn here every call
		// to it stops the turn on a question no window in this product can show
		// — which was observed on a real run: two `ask` calls waiting, the step
		// saying `still waiting for an answer`, and nothing on any screen to
		// answer with.
		return true
	}
	return false
}
