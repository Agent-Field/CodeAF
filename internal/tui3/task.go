package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE TASK SURFACE: A DECISION, AND THEN A PRESENCE.
//
// internal/session's task_contract.go says what a task IS — a node in a
// dynamically evolving DAG, of which v1 ships the degenerate one-node case —
// and this file is the two places that node touches a person:
//
//   - THE PROPOSAL IS A DECISION MOMENT, INLINE. It arrives mid-conversation,
//     it blocks the turn that raised it, and it is answered once. That is the
//     shape the consent question already has on this surface, so it takes the
//     consent grammar whole: the question hue (violet, the fifth colour, spent
//     nowhere else), the row that keeps its verdict afterwards, the keyboard
//     lane at the bottom of the screen. It is drawn in the TRANSCRIPT rather
//     than above the input because a proposal is part of what happened — the
//     model asked, you answered, the work started — and a modal that vanished
//     would leave the conversation unable to explain where a running node came
//     from.
//   - THE RUNNING NODE IS A STANDING PRESENCE, IN A RAIL. It outlives the turn
//     that proposed it: its "done" lands minutes later with no turn open and no
//     stream to land on. A transcript line cannot hold something that is still
//     true, so the right column holds it — one row per node, alive at the top,
//     gone when it has come home — and the transcript gets one dim line when it
//     lands. THE RAIL IS PRESENCE; THE NOTE IS HISTORY. Neither says the other's
//     half.
//
// THE RAIL IS TREE-READY AND THE TREE IS NOT DRAWN. v1's graph has no edges, so
// nothing here draws one — but every node carries its DependsOn, the row model
// stores it, and a node whose prerequisites are unmet renders v1's own sentence
// under it ("waits: collect sources", internal/tui's model_test.go). When edges
// arrive the shape is already in the model; what changes is how many rows have
// something to say.

// ── the two rows of state ───────────────────────────────────────────────────

// taskCard is one proposal, from the question to the verdict.
//
// It is a pointer held in two places — the transcript entry that draws it and
// [app.task], the lane that answers it — so the answer and the row can never
// disagree about what was decided.
type taskCard struct {
	id                                uint64
	title, summary, brief, acceptance string
	dependsOn                         []uint64
	// deadline is when silence becomes approval, or zero when the clock is off
	// (session's TaskNotice.Deadline). A zero deadline draws no countdown: a
	// number counting down to nothing is a promise the engine did not make.
	deadline time.Time
	// born is when the question arrived, and it exists for the METER: a bar that
	// drains needs both ends of the span, and the notice carries only the far
	// one. It is taken from the surface's own clock at the moment the card is
	// built, which is the moment the person could first have answered.
	born time.Time
	// open says the brief and the acceptance are showing, behind the same
	// expand mechanic a tool row's detail is behind.
	open bool
	// choice is which of [taskChoiceWords] has the keyboard — the row's own
	// cursor, and the thing enter acts on.
	choice int
	// typing says the redirect lane has the focus: the person asked for the box,
	// so the letters that would otherwise answer the question are text again.
	typing bool
	// verdict is what was decided, in the words the row keeps afterwards. It is
	// empty for exactly as long as the question is open.
	verdict string
	// answer is the OPTION that settled it — "yes", "redirect", "no" — kept
	// beside the verdict the way a consent row keeps "allowed". It is empty when
	// nobody chose: a clock that ran out and a turn that ended chose nothing.
	answer string

	// choiceRow is where the choices row sits inside this card's rendered rows,
	// or -1, and spans are the columns each option occupies on it. Both are
	// written by [app.taskCardRows] and read by the hit-testing (app.go's
	// [app.choicePress]) — one layout, one set of targets, because a row whose
	// drawing and whose clicks disagreed would answer a question the person did
	// not ask.
	choiceRow int
	spans     []choiceSpan
}

// choiceSpan is one option's columns on the choices row: [from, to) answers to
// the option at.
type choiceSpan struct{ from, to, at int }

// settled reports whether this proposal has been answered.
func (c *taskCard) settled() bool { return c.verdict != "" }

// taskNode is one node's life, as the rail holds it.
//
// It is a struct of its own rather than the session's TaskNotice because the
// rail needs one fact the notice cannot carry: WHEN the node started, so the
// elapsed clock counts on the frame tick instead of freezing at whatever the
// last update happened to say. Everything else is the notice, kept.
type taskNode struct {
	id    uint64
	title string
	state session.TaskState
	// dependsOn is the structural half of this file (see the header): stored
	// always, drawn only when a prerequisite is unmet.
	dependsOn []uint64
	// began is the moment the node started, derived once from the update's own
	// Elapsed so the clock is the frame's and not the event's.
	began time.Time
	// elapsed is the node's final age, as the update that ended it reported.
	elapsed               time.Duration
	report, branch, merge string
}

// A NODE NEVER LEAVES THE ROSTER. It used to: a finished node whose branch had
// come home dropped off the rail, because the rail was a presence list and a row
// that never left would have turned it into a log. The column is the session's
// record of its own work now, and what it does with a settled node instead is
// GROUP it — see task.go's roster section, and [app.railGroupOf] for the one
// placement that is not simply the engine's state read out.

// The merge words session publishes (task_run.go's mergeMerged and friends),
// restated here because the surface reads them and internal/session exports
// them nowhere.
//
// THEY ARE THE ENGINE'S WORDS AND NOT THIS SURFACE'S. Three of them are read out
// as they stand, because "merged" and "conflicted" mean on screen what they mean
// in the branch. "aborted" does not, and it is translated where it is drawn (see
// [taskStoppedKept]).
const (
	mergeWordMerged     = "merged"
	mergeWordConflicted = "conflicted"
	mergeWordInPlace    = "inplace"
	mergeWordAborted    = "aborted"
)

// The two words a stopped node is drawn with.
//
// A node stops for reasons that are nobody's failure — a person pressed c on its
// room, it spent the steps it was given, its deadline came — and session marks
// every one of them "aborted", which is a word a person reads as "it crashed".
// It did not: it stopped, and its branch was kept precisely so the work is still
// there. Both halves are on screen because the second is the one that says what
// to do next.
const (
	taskStoppedWord = "stopped"
	taskStoppedKept = "stopped — branch kept"
)

// taskAgent is the slice of *session.Agent this file needs, and it is asserted
// rather than added to [Agent].
//
// The reason is the seam's own: the task contract is OPTIONAL. A surface can be
// driven by a scripted agent that has never heard of a task (every other test in
// this package is), and widening the package interface would make a session
// without a tasker un-representable — which is exactly the thing the door
// currently hands us on a build with the engine turned off.
type taskAgent interface {
	// ResolveTask answers one proposal: approved as briefed, approved with a
	// correction appended, or denied.
	ResolveTask(id uint64, answer session.TaskAnswer)
	// TaskUpdates is the STANDING subscription — one channel for the session's
	// whole life, because a node's most important event happens when no turn is
	// open (session's task_run.go).
	TaskUpdates() <-chan session.Event
	// PendingTasks names the proposals the engine is still waiting on. It is
	// how this surface finds out that the card on screen is about a question
	// nobody is asking any more.
	PendingTasks() []uint64
}

// tasker is the agent under this surface, when it has a tasker at all.
func (a *app) tasker() (taskAgent, bool) {
	agent, ok := a.agent.(taskAgent)
	return agent, ok
}

// ── the update lane ─────────────────────────────────────────────────────────

// watchTasks opens the standing subscription and starts pumping it into the
// program loop. It is called once at boot and again wherever the agent under
// this surface is REPLACED (/new), because the channel belongs to the agent that
// handed it over.
//
// The generation is the same device the turn stream uses: a lane from an agent
// that has been closed may still deliver, and an event from a conversation that
// no longer exists must not upsert a node into the one that does.
func (a *app) watchTasks() tea.Cmd {
	agent, ok := a.tasker()
	if !ok {
		return nil
	}
	a.taskGen++
	a.taskLane = agent.TaskUpdates()
	return waitTask(a.taskLane, a.taskGen)
}

// waitTask takes one event off the standing lane and asks for the next.
func waitTask(ch <-chan session.Event, gen int) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return taskLaneClosedMsg{gen: gen}
		}
		return taskEventMsg{gen: gen, ev: ev}
	}
}

// taskEvent folds one event from the standing lane in and re-arms the pump.
func (a *app) taskEvent(ev session.Event) tea.Cmd {
	switch ev.Kind {
	case session.EventTaskProposal:
		a.proposeTask(ev)
	case session.EventTaskUpdate:
		a.taskUpdate(ev)
	}
	return tea.Batch(waitTask(a.taskLane, a.taskGen), a.wake())
}

// ── the proposal ────────────────────────────────────────────────────────────

// proposeTask draws the decision moment (session.EventTaskProposal).
//
// The card lands in the transcript and takes the keyboard's answer lane. One
// question at a time is the engine's own serialization (its ask blocks the tool
// call that raised it, exactly as consent's does), and a second card arriving
// anyway is not dropped: the older one settles as expired, because a question
// that can no longer be answered must stop looking like one.
func (a *app) proposeTask(ev session.Event) {
	notice := ev.Task
	if notice == nil {
		return
	}
	if a.task != nil && !a.task.settled() {
		a.task.verdict = taskExpiredWord
	}
	card := &taskCard{
		id:         notice.ID,
		title:      strings.TrimSpace(notice.Title),
		summary:    strings.TrimSpace(notice.Summary),
		brief:      strings.TrimSpace(notice.Brief),
		acceptance: strings.TrimSpace(notice.Acceptance),
		dependsOn:  notice.DependsOn,
		deadline:   notice.Deadline,
		born:       a.now(),
		// THE CARD OPENS ON "YES", because that is what the block is proposing and
		// a cursor parked on the destructive answer is a cursor that makes the
		// safe answer the one you have to aim at. The clock behind it says the
		// same thing: silence is approval.
		choice:    choiceYes,
		choiceRow: -1,
	}
	a.task = card
	a.closeLive()
	// The typed lists follow the draft, and the draft is now the redirect lane:
	// a completion list left open under it would be answering keys that belong
	// to the question (consent.go makes the same call for the same reason).
	a.closeLists()
	a.entries = append(a.entries, entry{kind: entryTask, turn: a.turn, card: card})
	a.follow()
	a.touch()
}

// The words a settled card keeps. They are sentences rather than states because
// the row is read once, later, by somebody reconstructing what happened.
const (
	taskApprovedWord  = "approved"
	taskRedirectWord  = "approved · you redirected it"
	taskDeclinedWord  = "declined"
	taskClockWord     = "approved · the clock"
	taskExpiredWord   = "expired · the turn ended"
	taskRedirectLane  = "redirect this task… (enter sends it, esc declines)"
	taskProposalHint  = "y yes · r redirect · n no"
	taskExpandHint    = "ctrl+e for the brief"
	taskAcceptanceTag = "done when: "
	// taskWaitingWord is what stands where the meter would be on a proposal the
	// engine is holding open indefinitely. A bar with no end to drain toward
	// would be an animation inventing a deadline nobody set.
	taskWaitingWord = "waiting on you"
	taskAutoWord    = "auto-starts in "
)

// THE THREE ANSWERS, and they are a ROW OF OPTIONS rather than three keys named
// in a sentence.
//
// The lane underneath was the whole interface until this wave: bare enter
// approved, esc declined, and the only thing on screen that said so was a hint
// in the legend, forty rows away from the question. A decision moment with
// nothing to point at is a decision moment a person answers by guessing — so the
// options are drawn where the question is, in the consent block's own bracket
// idiom, and every one of them is reachable three ways: the pointer, ←/→ and
// enter, and the letter each option starts with.
//
// The letters are the option's own initials — y, r, n — which is what makes them
// learnable without a legend. They are taken only while the box is EMPTY and the
// redirect lane has not been asked for (see [app.taskKey]): the moment a person
// is writing a correction, a letter is a letter.
const (
	choiceYes = iota
	choiceRedirect
	choiceNo
)

var taskChoiceWords = [...]string{"yes", "redirect", "no"}

// awaitingTask reports whether a proposal owns the answer lane.
func (a *app) awaitingTask() bool { return a.task != nil && !a.task.settled() }

// taskKey is the proposal's claim on the keyboard, and it is deliberately NOT
// modal.
//
// The consent question suspends the draft because there is nothing useful to
// type at it. A proposal is the opposite: the most valuable thing a person can
// do with a groomed piece of work is CORRECT it, so the input box stays live and
// becomes the redirect lane.
//
// THE KEYS ARE TAKEN IN TWO TIERS, and the tier is decided by what is in the
// box:
//
//	always      enter answers the focused option · esc declines · ctrl+e the brief
//	empty box   ←/→ move the focus · y, r, n pick an option outright
//
// The second tier is given back the moment there is a sentence in the box, and
// the moment the redirect lane has been asked for. That is the whole guard
// against the obvious defect: "yes, but keep the tests" begins with a y, and a
// surface that read that as approval would have approved something the person
// was in the middle of correcting. ←/→ survive the redirect lane because there
// is no caret to move in an empty box, and because a focus a person can enter
// and not leave is a trap.
func (a *app) taskKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.awaitingTask() {
		return nil, false
	}
	card := a.task
	switch msg.String() {
	case "enter":
		return a.takeChoice(card.choice), true
	case "esc":
		// esc is the dismiss key everywhere on this surface, so it stays the
		// outright no — from the lane as well as from the row.
		a.answerTask(false, "")
		return nil, true
	case "ctrl+e":
		// end-of-line keeps the key the moment there is a line to end: the same
		// rule input.go applies to the thinking block.
		if !a.input.empty() {
			return nil, false
		}
		a.toggleCard()
		return nil, true
	}
	// The model picker is the one overlay that can be up over a proposal with an
	// empty box, and its filter answers to the same letters.
	if !a.input.empty() || a.pick.open {
		return nil, false
	}
	switch msg.String() {
	case "left":
		a.moveChoice(-1)
		return nil, true
	case "right":
		a.moveChoice(1)
		return nil, true
	}
	if card.typing {
		return nil, false
	}
	switch msg.String() {
	case "y":
		return a.takeChoice(choiceYes), true
	case "r":
		return a.takeChoice(choiceRedirect), true
	case "n":
		return a.takeChoice(choiceNo), true
	}
	return nil, false
}

// takeChoice acts on one option, whether a key, an arrow's enter or a click
// asked for it.
//
// REDIRECT IS THE ONE OPTION THAT DOES NOT ANSWER. It is a request for the box —
// the placeholder is already down there saying what the box is for — so it takes
// the focus and waits; the enter that follows carries the words. Yes and
// redirect converge the moment something IS typed, which is the behaviour the
// bare lane always had: a correction in the box is a correction whichever of the
// two a person reached for.
func (a *app) takeChoice(at int) tea.Cmd {
	card := a.task
	if card == nil || card.settled() || at < 0 || at >= len(taskChoiceWords) {
		return nil
	}
	card.choice = at
	text := strings.TrimSpace(a.input.String())
	if at == choiceNo {
		a.answerTask(false, "")
		return nil
	}
	if at == choiceRedirect && text == "" {
		card.typing = true
		a.markCardStale(card)
		a.touch()
		return nil
	}
	a.answerTask(true, text)
	return a.edited()
}

// moveChoice walks the row and STOPS at its ends rather than wrapping. Three
// options are a row a person reads at a glance, and a cursor that reappeared at
// the far end would put "no" under a key pressed to reach "yes".
func (a *app) moveChoice(delta int) {
	card := a.task
	if card == nil || card.settled() {
		return
	}
	at := card.choice + delta
	switch {
	case at < 0:
		at = 0
	case at >= len(taskChoiceWords):
		at = len(taskChoiceWords) - 1
	}
	card.choice = at
	// Landing on redirect is asking for the box, exactly as pressing r is.
	card.typing = at == choiceRedirect
	a.markCardStale(card)
	a.touch()
}

// answerTask resolves the open proposal and annotates its row.
//
// The redirect is APPENDED to the brief by the engine (session's TaskAnswer), so
// what travels is the person's words verbatim and what stays here is the fact
// that they said them. The draft is cleared either way: the sentence in the box
// was about this question, and leaving it there would make the next enter send
// it to the model.
func (a *app) answerTask(approve bool, redirect string) {
	card := a.task
	if card == nil || card.settled() {
		return
	}
	switch {
	case approve && redirect != "":
		card.verdict, card.answer = taskRedirectWord, taskChoiceWords[choiceRedirect]
	case approve:
		card.verdict, card.answer = taskApprovedWord, taskChoiceWords[choiceYes]
	default:
		card.verdict, card.answer = taskDeclinedWord, taskChoiceWords[choiceNo]
	}
	card.typing = false
	if agent, ok := a.tasker(); ok {
		agent.ResolveTask(card.id, session.TaskAnswer{Approved: approve, Redirect: redirect})
	}
	a.input.reset()
	a.endRecall()
	a.closeLists()
	a.markCardStale(card)
	a.touch()
}

// toggleCard opens or closes the open proposal's brief.
func (a *app) toggleCard() {
	if a.task == nil {
		return
	}
	a.task.open = !a.task.open
	a.markCardStale(a.task)
	a.touch()
}

// toggleCardAt is the click: the whole card is the target, the way the whole of
// a thinking block is (thinking.go) — a card is a paragraph, and asking somebody
// to hit its first row is asking them to aim.
func (a *app) toggleCardAt(i int) {
	if i < 0 || i >= len(a.entries) || a.entries[i].kind != entryTask {
		return
	}
	card := a.entries[i].card
	if card == nil {
		return
	}
	card.open = !card.open
	a.entries[i].stale = true
	a.touch()
}

// markCardStale drops the cached rows of the entry that draws this card.
func (a *app) markCardStale(card *taskCard) {
	for i := range a.entries {
		if a.entries[i].kind == entryTask && a.entries[i].card == card {
			a.entries[i].stale = true
			return
		}
	}
}

// tickTasks is the countdown, and it runs on the frame clock that is already
// turning (app.go's [app.paint]) rather than on a ticker of its own.
//
// At the deadline the card STOPS ASKING. It does not answer: the clock belongs
// to the engine — the deadline on screen is the one the engine is waiting on —
// and a surface that raced it would be a second authority on the same question.
// What it does is stop claiming a question is open when it is not.
func (a *app) tickTasks() {
	if !a.awaitingTask() || a.task.deadline.IsZero() {
		return
	}
	if a.now().Before(a.task.deadline) {
		return
	}
	a.task.verdict = taskClockWord
	a.markCardStale(a.task)
	a.touch()
}

// syncTaskAsk drops a card about a question nobody is asking any more.
//
// It runs where consent's [app.dropAsks] runs — a turn ending — and it asks the
// engine rather than assuming: [taskAgent.PendingTasks] is the same
// pending-id machinery consent has, and a proposal missing from it has already
// been approved by the clock, denied elsewhere, or died with its turn.
func (a *app) syncTaskAsk() {
	if !a.awaitingTask() {
		return
	}
	agent, ok := a.tasker()
	if !ok {
		return
	}
	for _, id := range agent.PendingTasks() {
		if id == a.task.id {
			return
		}
	}
	// The deadline is what distinguishes the two honest stories: a clock that
	// ran out approved it, and anything else ended with the turn.
	word := taskExpiredWord
	if !a.task.deadline.IsZero() && !a.now().Before(a.task.deadline) {
		word = taskClockWord
	}
	a.task.verdict = word
	a.markCardStale(a.task)
	a.touch()
}

// ── the card, drawn ─────────────────────────────────────────────────────────
//
// THE PROPOSAL IS A CONTAINED BLOCK, and it is contained because of what it sits
// between. Every other entry on this surface is a paragraph in a conversation:
// it begins where the last one ended and nothing is lost when the eye runs from
// one into the next. A question is not a paragraph. It has a top, three answers
// and a clock, and when its rows flowed into the reply underneath it the result
// was a decision a person had to reconstruct the boundaries of before they could
// make it.
//
//	╭─ ? Fix the nil-map crash ─────────────────────────────────────────────
//	│ The parser drops a key on an empty map. This adds the guard and the
//	│ regression test.
//	│ [ yes ]  [ redirect ]  [ no ]
//	│ ███████████████░░░░░  auto-starts in 3.2s
//	│ ctrl+e for the brief
//	╰──────────────────────────────────────────────────────────────────────
//
// Opened, the brief and the acceptance sit under the summary, dim, because they
// are the node's contract with its runner rather than the sentence a person
// decides on. SETTLED, THE BLOCK COLLAPSES to its head and one verdict line —
// what was chosen and what that came to — because a question that has been
// answered is a fact, and a fact does not need a frame around it.
//
// The blank row that follows the block is [app.layout]'s (render.go): spacing is
// emitted in exactly one place on this surface, and a block that left its own
// gap would be the second.
func (a *app) taskCardRows(card *taskCard, width int) []string {
	if card == nil || width < 4 {
		return nil
	}
	// The hit targets are rebuilt with the rows that carry them, and cleared
	// first: a settled card has no options, and a stale span is a click that
	// answers a question nobody is asking.
	card.choiceRow, card.spans = -1, nil
	head := a.taskHead(card, width)
	if card.settled() {
		return []string{head, a.taskFoot(card, width)}
	}
	stem := a.pal.ask(a.blockStem())
	room := width - ansi.StringWidth(a.blockStem())
	out := []string{head}
	for _, line := range taskSummaryLines(card.summary, room) {
		out = append(out, stem+a.pal.ink(line))
	}
	if card.open {
		for _, line := range wrap(card.brief, room) {
			out = append(out, stem+a.pal.dim(line))
		}
		if card.acceptance != "" {
			// The done-condition is LABELLED rather than run on: it is the one line
			// in the brief a person reads to decide whether the work will be
			// finished by something they would call finished.
			for _, line := range wrap(taskAcceptanceTag+card.acceptance, room) {
				out = append(out, stem+a.pal.dim(line))
			}
		}
	}
	choices, spans := a.taskChoices(card, ansi.StringWidth(a.blockStem()), room)
	card.choiceRow, card.spans = len(out), spans
	out = append(out, stem+choices)
	out = append(out, stem+a.taskMeter(card, room))
	if !card.open && card.brief != "" {
		out = append(out, stem+a.pal.dim(fit(taskExpandHint, room)))
	}
	return append(out, a.taskFoot(card, width))
}

// taskSummaryLines is the sentence a person decides on, and it is CAPPED.
//
// The summary is the engine's two or three lines about what it wants to go and
// do; a model that wrote six would otherwise turn the block into a page with a
// clock at the bottom of it. Everything past the cap is in the brief, one
// keystroke away, which is where the long form belongs anyway.
func taskSummaryLines(summary string, width int) []string {
	lines := wrap(summary, width)
	if len(lines) <= taskSummaryRows {
		return lines
	}
	lines = lines[:taskSummaryRows]
	lines[taskSummaryRows-1] = fit(lines[taskSummaryRows-1]+" "+glyphMore, width)
	return lines
}

// taskSummaryRows is that cap. Three is what a decision fits in.
const taskSummaryRows = 3

// The block's own furniture, and its ASCII stand-ins. The stem is the one an
// expanded tool call already hangs from (styles.go's railCont), because a
// vertical line meaning "these rows are one thing" is a vocabulary this surface
// already has.
const (
	taskHeadCorner  = "╭─"
	taskFootCorner  = "╰─"
	taskCornerASCII = "+-"
	taskRule        = "─"
	taskRuleASCII   = "-"
)

// blockStem is the card's left edge.
func (a *app) blockStem() string {
	if a.pal.ascii {
		return railContASCII
	}
	return railCont
}

// blockRule is the line the head and the foot are drawn with.
func (a *app) blockRule() string {
	if a.pal.ascii {
		return taskRuleASCII
	}
	return taskRule
}

// blockPaint is the hue the frame itself takes: THE QUESTION HUE WHILE IT IS A
// QUESTION, and the furniture grey the moment it is not. Violet on this surface
// means somebody is being asked something, and a settled card that kept it would
// be a block still shouting about a decision that has been made.
func (a *app) blockPaint(card *taskCard) func(string) string {
	if card.settled() {
		return a.pal.dim
	}
	return a.pal.ask
}

// taskHead is the block's top: the corner, the question glyph, the title, and
// the rule that runs out to the frame's edge.
//
// THE CLOCK IS NOT UP HERE ANY MORE. It used to ride the right end of this row,
// which put the one thing on the block that changes every frame on the same line
// as the one thing worth reading once — and it said "4s", which is a number
// rather than a countdown. Both now live on the meter (see [app.taskMeter]).
func (a *app) taskHead(card *taskCard, width int) string {
	paint, rule := a.blockPaint(card), a.blockRule()
	corner := taskHeadCorner
	if a.pal.ascii {
		corner = taskCornerASCII
	}
	// The "?" is the consent block's own glyph, and it is the same glyph for the
	// same reason it is the same hue: this is that moment, about a different
	// kind of thing. It is already its own ASCII, so the linear tier needs no
	// stand-in for it.
	head := corner + " " + glyphAsk + " "
	title := fit(card.title, width-ansi.StringWidth(head)-1)
	line := paint(head)
	if card.settled() {
		line += a.pal.muted(title)
	} else {
		line += a.pal.askBold(title)
	}
	if fill := width - ansi.StringWidth(head) - ansi.StringWidth(title) - 1; fill > 0 {
		line += paint(" " + strings.Repeat(rule, fill))
	}
	return line
}

// taskFoot closes the block — and, once the question is answered, IS the answer.
//
// A settled card is two rows: the head it always had, and this, which keeps both
// halves of what happened. The option is what the person reached for and the
// verdict is what it came to, and they are different facts — "redirect" says
// they corrected it, "approved · you redirected it" says the work started.
func (a *app) taskFoot(card *taskCard, width int) string {
	paint, rule := a.blockPaint(card), a.blockRule()
	corner := taskFootCorner
	if a.pal.ascii {
		corner = taskCornerASCII
	}
	if !card.settled() {
		if fill := width - ansi.StringWidth(corner); fill > 0 {
			return paint(corner + strings.Repeat(rule, fill))
		}
		return paint(corner)
	}
	word := card.verdict
	if card.answer != "" {
		word = card.answer + " · " + card.verdict
	}
	return paint(corner+" ") + a.pal.dim(fit(word, width-ansi.StringWidth(corner)-1))
}

// taskChoices draws the row of options and reports what each one occupies, in
// screen columns, so a click can be resolved to the option under it.
//
// An option that does not fit is DROPPED rather than truncated, which is the
// rule the consent offer follows for the same reason (consent.go): half an
// answer is an answer somebody presses by mistake.
func (a *app) taskChoices(card *taskCard, left, width int) (string, []choiceSpan) {
	var line string
	var spans []choiceSpan
	at := left
	end := left + width
	for i, word := range taskChoiceWords {
		chip := "[ " + word + " ]"
		gap := 0
		if i > 0 {
			gap = 2
		}
		if at+gap+ansi.StringWidth(chip) > end {
			break
		}
		if gap > 0 {
			line += strings.Repeat(" ", gap)
			at += gap
		}
		line += a.taskChip(word, i == card.choice)
		spans = append(spans, choiceSpan{from: at, to: at + ansi.StringWidth(chip), at: i})
		at += ansi.StringWidth(chip)
	}
	return line, spans
}

// taskChip is one option. The focused one takes the question hue and the weight
// together; the others keep the hue and spend the weight on their INITIAL, which
// is the key that picks them — the same trick the consent offer plays with its
// bracketed letters, minus the brackets nobody needs when the letter is already
// the first thing in the word.
func (a *app) taskChip(word string, focus bool) string {
	if focus {
		return a.pal.askBold("[ " + word + " ]")
	}
	return a.pal.dim("[ ") + a.pal.askBold(word[:1]) + a.pal.ask(word[1:]) + a.pal.dim(" ]")
}

// taskMeter is THE COUNTDOWN, AS A COUNTDOWN.
//
// What stood here was the string "4s", redrawn every frame — a number that a
// person had to read, twice, a second apart, before it told them anything. A
// draining bar is the same fact in a channel that needs no reading at all: the
// question "how much of my time to decide is left" is answered by how much of
// the row is still filled, and the number beside it is there for the person who
// wants the figure rather than the shape.
//
// It is recomputed from the DEADLINE on every frame ([app.tickTasks] runs on the
// same clock), never stepped: a bar that advanced itself would drift from the
// clock the engine is actually holding the proposal against.
func (a *app) taskMeter(card *taskCard, width int) string {
	if card.deadline.IsZero() {
		return a.pal.dim(fit(taskWaitingWord, width))
	}
	left := card.deadline.Sub(a.now())
	word := taskAutoWord + countdownFine(left)
	cells := taskMeterCells
	if room := width - ansi.StringWidth(word) - 2; cells > room {
		cells = room
	}
	if cells < 1 {
		return a.pal.dim(fit(word, width))
	}
	span := card.deadline.Sub(card.born)
	frac := 0.0
	if span > 0 {
		frac = float64(left) / float64(span)
	}
	return a.progress(frac, cells) + "  " + a.pal.dim(word)
}

// taskMeterCells is the meter's widest. Twenty cells is a bar a person reads as
// a proportion; past that it is a progress dialog, and this surface does not
// have those.
const taskMeterCells = 20

// countdownFine spells the time LEFT beside the meter, and it spells the last
// ten seconds in tenths.
//
// The tenth is the whole point of the pair: at one figure per second the number
// beside a moving bar looks stuck, and "3.2s" is the digit that proves the same
// thing the bar does — this is running, and it is running out. Above ten seconds
// the tenth is noise and it falls back to [countdownWord].
func countdownFine(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d >= 10*time.Second {
		return countdownWord(d)
	}
	// Rounded UP, by the law [countdownWord] follows: the last tenth a person
	// has is drawn as a tenth rather than as a zero.
	tenths := int((d + 100*time.Millisecond - 1) / (100 * time.Millisecond))
	return itoa(tenths/10) + "." + itoa(tenths%10) + "s"
}

// countdownWord spells the time LEFT, rounded up, so the last second a person
// has is drawn as a second rather than as a zero. It is the count-up's mirror
// ([countUpWord]) and it is spelled the same way — spaced parts, no padding —
// because the two numbers appear on one screen.
func countdownWord(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	seconds := int((d + time.Second - 1) / time.Second)
	if seconds < 60 {
		return itoa(seconds) + "s"
	}
	return itoa(seconds/60) + "m " + itoa(seconds%60) + "s"
}

// ── the roster ──────────────────────────────────────────────────────────────
//
// THE RAIL WAS A PRESENCE LIST AND IT IS NOW A ROSTER, because the two stop
// being the same thing somewhere around the fortieth node. A presence list holds
// what is alive and forgets everything else, which is exactly right for a
// session with three nodes in it and useless for a day's work: "where did that
// task go" is the commonest question a person asks a column of work, and a
// column that dropped every landed node had already thrown the answer away.
//
// So the column keeps EVERY node the session has admitted, and it survives
// hundreds of them by four mechanisms and no new scroll machinery:
//
//   - ATTENTION FIRST. Five groups, in the order a person needs them — what is
//     asking for a decision, what is running, what is waiting for a slot, what
//     is parked behind other work, what is over — and newest first inside each,
//     because the node you just started is the node you are watching. This is
//     the one law the old rail stated and this file now overturns ("a list that
//     reordered itself would move the row a person is watching"): at three rows
//     admission order IS the shape, and at three hundred it is a haystack. The
//     order is stable in the way that matters — a row moves when its STATE
//     moves, which is the one event a person is watching for anyway.
//   - THE TAIL IS FOLDED. `parked` and `done` open closed, one heading each with
//     its population on it: a hundred and forty-eight settled nodes are a fact,
//     not a hundred and forty-eight rows.
//   - THE COLUMN IS A WINDOW. What shows is a slice of the line list around the
//     focus, taken by [listTop] — the same function the model picker and the two
//     typed lists scroll with, because a second scroller on this surface would be
//     a second set of off-by-ones.
//   - THE FOOTER SAYS THE WHOLE. What the window cannot show — the spend, the
//     weight, the count of everything folded away — is one dim block at the
//     bottom of the column.
//
// THE KEYBOARD IS ASKED FOR, NEVER TAKEN (ctrl+t, esc to give it back). The
// draft is this surface's rest state and a map that stole keys from it would
// make typing a thing you check before you do — see the marker law at
// [app.railRows].

const (
	// railCols is the whole charge a full rail makes on the frame: the seam,
	// its gutter, and the column the nodes are drawn in.
	railCols = 30
	// railSlimCols is the charge under a narrower frame: the same column,
	// tighter.
	railSlimCols = 24
	// railFloor is the frame a FULL rail takes. Under it the conversation
	// would be reading at ninety columns to keep a column of titles on screen.
	railFloor = 120
	// railSlimFloor is the frame a rail of any width takes. Under it the
	// transcript is the thing a person came for; the nodes still land in it
	// as notes when they finish.
	railSlimFloor = 100
	// railSeam is the one line the rail draws, and it is the same line the
	// legend draws below: a seam, not a border.
	railSeam = "│ "
	// railMark is the seam cell of the FOCUSED row — the same two columns, one
	// glyph heavier. The focus is a marker rather than a band because this column
	// is two cells from the conversation: a filled row here would be a block of
	// colour beside a paragraph a person is reading.
	railMark      = "▌ "
	railMarkASCII = "> "
)

// The disclosure marks a group heading wears, and their ASCII stand-ins: closed
// points at what it is hiding, open points down at what it showed.
const (
	glyphShut      = "▸"
	glyphShutASCII = ">"
	glyphOpen      = "▾"
	glyphOpenASCII = "v"
)

// What a heading offers the keyboard, said on the heading itself and only when
// it is focused (see [app.railHeading]).
const (
	railOpenHint = "enter/→ expand"
	railFoldHint = "enter/← collapse"
	// railHoldHint is what the legend's hint slot says while the roster has the
	// keyboard (render.go's [app.hintWord]) — the same six keys [app.railKey]
	// takes, quoted from the handler rather than authored twice.
	railHoldHint = "↑↓ move · →← fold · enter open · esc"
)

// railGroup is what a node is DOING, which is the only thing the roster sorts
// by. The order of these constants IS the order of the column.
type railGroup uint8

const (
	// railAttention is work that is waiting on a PERSON: it failed, or it
	// finished and its branch never came home. Both are the same sentence — this
	// is not going anywhere until you look at it — and they lead the column
	// because everything below them is a thing that is still moving by itself.
	railAttention railGroup = iota
	// railRunning is a child agent working in its worktree right now.
	railRunning
	// railIdle is admitted, unblocked, and not started: nothing is in its way
	// except a slot.
	railIdle
	// railParked is admitted and BLOCKED — its prerequisites are unfinished, so
	// nothing about it will change until other work does. That is what makes it
	// the group that folds: a parked node is a promise, not a happening.
	railParked
	// railDone is over and delivered.
	railDone
	railGroupCount
)

// The word each group wears, in the roster's heading and in its footer alike.
// One vocabulary: a person who reads "needs you" at the top must not have to
// learn that the bottom calls the same thing "blocked".
var railGroupWords = [railGroupCount]string{"needs you", "running", "idle", "parked", "done"}

// railGroupOf places one node.
//
// A KEPT BRANCH IS ATTENTION, and it is the one placement that is not simply the
// engine's state read out. session keeps the branch of a node that conflicted or
// was stopped (task_run.go's mergeConflicted and mergeAborted), and a kept
// branch is work that is finished and NOT DELIVERED — the one outcome on this
// surface a person still has to do something about.
func (a *app) railGroupOf(node *taskNode) railGroup {
	switch node.state {
	case session.TaskRunning:
		return railRunning
	case session.TaskFailed:
		return railAttention
	case session.TaskQueued:
		if a.railWaits(node) != "" {
			return railParked
		}
		return railIdle
	}
	switch node.merge {
	case mergeWordConflicted, mergeWordAborted:
		return railAttention
	}
	return railDone
}

// railShut reports whether a group is drawn as its heading alone.
//
// The default is the design and the map is the person's correction of it, which
// is why this is not a plain bool per group: a group nobody has touched must
// follow the default even after its population changes underneath it.
func (a *app) railShut(g railGroup) bool {
	if open, said := a.railOpen[g]; said {
		return !open
	}
	return g == railParked || g == railDone
}

// railSetOpen folds a group open or closed.
func (a *app) railSetOpen(g railGroup, open bool) {
	if a.railOpen == nil {
		a.railOpen = map[railGroup]bool{}
	}
	if a.railShut(g) == !open {
		return
	}
	a.railOpen[g] = open
	a.touch()
}

// railToggle is what enter on a heading does.
func (a *app) railToggle(g railGroup) { a.railSetOpen(g, a.railShut(g)) }

// railEntry is one navigable thing in the roster: a group's heading, or a node
// under an open one.
type railEntry struct {
	group railGroup
	// node is the node this row is about, or nil on a heading.
	node *taskNode
	// count is a heading's population, and it is carried on the entry rather
	// than recounted at paint time so the heading and the footer cannot disagree
	// about how many nodes are folded behind one line.
	count int
}

// railSpot names an entry by IDENTITY rather than by index, and it is what the
// focus is stored as.
//
// An index would be a cursor that jumps: a node landing moves it between groups,
// a heading folding takes a hundred and forty-eight rows out from under it, and
// both of those happen while nobody is touching the keyboard. A (group, id) pair
// survives all of it, and when the thing it names is genuinely gone the fallback
// is its heading — which is where its rows went.
type railSpot struct {
	group railGroup
	// id is the node's, or zero for the group's heading.
	id uint64
}

func railSpotOf(e railEntry) railSpot {
	if e.node == nil {
		return railSpot{group: e.group}
	}
	return railSpot{group: e.group, id: e.node.id}
}

// railMembers buckets every node this session has admitted, NEWEST FIRST inside
// each group.
func (a *app) railMembers() [railGroupCount][]*taskNode {
	var out [railGroupCount][]*taskNode
	for i := len(a.taskOrder) - 1; i >= 0; i-- {
		node := a.tasks[a.taskOrder[i]]
		if node == nil {
			continue
		}
		g := a.railGroupOf(node)
		out[g] = append(out[g], node)
	}
	return out
}

// railEntries is the roster's row model: every non-empty group's heading, and
// the nodes of the groups that are open.
func (a *app) railEntries() []railEntry {
	members := a.railMembers()
	out := make([]railEntry, 0, len(a.taskOrder)+int(railGroupCount))
	for g := railGroup(0); g < railGroupCount; g++ {
		if len(members[g]) == 0 {
			continue
		}
		out = append(out, railEntry{group: g, count: len(members[g])})
		if a.railShut(g) {
			continue
		}
		for _, node := range members[g] {
			out = append(out, railEntry{group: g, node: node, count: len(members[g])})
		}
	}
	return out
}

// railColsFor is how wide the rail is at a frame width: full from railFloor,
// slim down to railSlimFloor, gone under that.
func railColsFor(width int) int {
	switch {
	case width >= railFloor:
		return railCols
	case width >= railSlimFloor:
		return railSlimCols
	}
	return 0
}

// railShowing reports whether the frame has a roster on it right now.
//
// ONE NODE RAISES IT AND NOTHING PUTS IT AWAY but /new. The old rail left when
// the last live node landed, which was honest about presence and wrong about a
// roster: the column is now the session's record of its own work, and a record
// that vanished the moment the work finished would be a record of nothing.
func (a *app) railShowing() bool {
	width, _ := a.size()
	if railColsFor(width) == 0 {
		return false
	}
	return len(a.taskOrder) > 0
}

// railWidth is what the rail costs the conversation, in columns.
func (a *app) railWidth() int {
	if !a.railShowing() {
		return 0
	}
	width, _ := a.size()
	return railColsFor(width)
}

// bodyWidth is the conversation's own width, and it is what EVERY geometric
// question about the transcript resolves through — what the frame draws, where
// the wheel lands, which row a click hit. A rail the layout knew about and the
// hit-testing did not would deliver clicks to rows wrapped at another width.
func (a *app) bodyWidth() int {
	width, _ := a.size()
	if body := width - a.railWidth(); body > 0 {
		return body
	}
	return width
}

// railLine is one drawn line of the roster and what it belongs to. It is the
// column's [row] (render.go): the frame draws the text, the pointer hit-tests
// the entry, and the focus marker lands on the head — one mapping from geometry
// to the roster, because two would be a click that opened the node above the one
// under the pointer.
type railLine struct {
	text string
	// entry indexes [app.railEntries], or -1 for the padding and the footer.
	entry int
	// head says this is the entry's FIRST line, which is the one a marker goes
	// on: a two-line node with two markers would read as two nodes.
	head bool
}

// railLines renders every entry, in order. It is the unwindowed list, and the
// window is taken out of it by [app.railView].
func (a *app) railLines(entries []railEntry, focus, width int) []railLine {
	out := make([]railLine, 0, len(entries)+len(entries)/2)
	for i := range entries {
		e := entries[i]
		if e.node == nil {
			out = append(out, railLine{text: a.railHeading(e, i == focus, width), entry: i, head: true})
			continue
		}
		for j, text := range a.railNodeRows(e.node, width) {
			out = append(out, railLine{text: text, entry: i, head: j == 0})
		}
	}
	return out
}

// railView is the whole column at a height: the window over the entries, the
// padding under it, and the footer at the bottom of it — exactly height lines.
//
// EVERY GEOMETRIC QUESTION ABOUT THE ROSTER GOES THROUGH HERE, the way every
// question about the conversation goes through [app.window]: the frame draws
// this, the pointer resolves through this, and the focus scrolls this. The
// window offset is written back as it is resolved, which is the same bargain
// [app.visible] makes with its row cache — the alternative is a scroll position
// recomputed in three places that agree until they do not.
//
// It reports the focused entry's index alongside the lines so its two callers do
// not each rebuild the entry list to ask the same question.
//
// WHAT IT COSTS IS BOUNDED BY WHAT IS OPEN, not by the session's length: every
// visible entry is rendered to measure the list, and the two groups that grow
// without limit are the two that open folded. The live groups are bounded by
// what the executor can actually run at once — and a person who expands `done
// 300` pays for it on the frames they are looking at it, which are frames with
// nothing animating on them (see [app.tasksAnimating]).
func (a *app) railView(height int) ([]railLine, int) {
	if height <= 0 || !a.railShowing() {
		return nil, -1
	}
	width, _ := a.size()
	room := railColsFor(width) - ansi.StringWidth(railSeam)
	entries := a.railEntries()
	focus := a.railFocusIndex(entries)

	foot := a.railFootRows(room, height)
	body := height - len(foot)
	if body < 1 {
		body, foot = height, nil
	}
	lines := a.railLines(entries, focus, room)

	// The cursor the window follows is the focused entry's first line, and the
	// offset itself when nothing is focused: a roster nobody is navigating stays
	// where it was rather than snapping back to the top under a landing node.
	cursor := a.railTop
	if focus >= 0 {
		for i, line := range lines {
			if line.entry == focus && line.head {
				cursor = i
				break
			}
		}
	}
	a.railTop = listTop(cursor, a.railTop, len(lines), body)

	out := make([]railLine, 0, height)
	for i := a.railTop; i < len(lines) && len(out) < body; i++ {
		out = append(out, lines[i])
	}
	for len(out) < body {
		out = append(out, railLine{entry: -1})
	}
	for _, text := range foot {
		out = append(out, railLine{text: text, entry: -1})
	}
	return out, focus
}

// railRows draws the roster to exactly height rows, or nil when there is none.
//
// The rows sit at the TOP of the column: the conversation grows upward from the
// input and the roster does not, because a list is read from its first row down.
//
// THE SEAM CARRIES THE FOCUS. Every line opens with the same two cells, and on
// the focused row those two cells are a heavier glyph in the accent — a marker
// rather than a band, because this column is two cells from a paragraph somebody
// is reading. It is drawn only while the roster HOLDS the keyboard: a cursor on
// a map that keys do not reach is a cursor that lies about what enter will do.
func (a *app) railRows(height int) []string {
	view, focus := a.railView(height)
	if len(view) == 0 {
		return nil
	}
	out := make([]string, len(view))
	for i, line := range view {
		lead := a.pal.dim(railSeam)
		if focus >= 0 && line.head && line.entry == focus {
			lead = a.pal.accent(a.linearMark(railMark, railMarkASCII))
		}
		out[i] = lead + line.text
	}
	return out
}

// railEntryAt is the roster's hit-testing: which entry is drawn on this screen
// row, and whether there is one at all.
//
// The roster's first row is the frame's first row (view.go joins it from index
// zero), so a roster index and a screen row are the same number.
func (a *app) railEntryAt(y int) (railEntry, bool) {
	view, _ := a.railView(a.viewHeight())
	if y < 0 || y >= len(view) || view[y].entry < 0 {
		return railEntry{}, false
	}
	entries := a.railEntries()
	if at := view[y].entry; at < len(entries) {
		return entries[at], true
	}
	return railEntry{}, false
}

// railNodeAt is which NODE is drawn on this screen row, or nil — a heading is a
// row about rows, and it opens nothing.
func (a *app) railNodeAt(y int) *taskNode {
	e, ok := a.railEntryAt(y)
	if !ok {
		return nil
	}
	return e.node
}

// railHeading is one group's line: the disclosure mark, the group's word, its
// population, and — on the focused heading, when the column has room for it —
// what the keyboard would do to it.
//
//	▾ running 3
//	▸ parked 148 — enter/→ expand
//
// The hint rides the FOCUSED heading only. Six of them down one column would be
// a legend printed once per group, and the person who needs it is the person
// whose cursor is already on the row.
func (a *app) railHeading(e railEntry, focus bool, width int) string {
	mark, hint := a.linearMark(glyphOpen, glyphOpenASCII), railFoldHint
	if a.railShut(e.group) {
		mark, hint = a.linearMark(glyphShut, glyphShutASCII), railOpenHint
	}
	line := mark + " " + railGroupWords[e.group] + " " + itoa(e.count)
	if !focus {
		return a.pal.dim(fit(line, width))
	}
	if ansi.StringWidth(line)+ansi.StringWidth(hint)+3 <= width {
		line += " — " + hint
	}
	return a.pal.bold(a.pal.accent(fit(line, width)))
}

// ── the focus, and the keyboard it answers to ───────────────────────────────

// railFocusAt finds the entry a spot names, or -1.
func railFocusAt(entries []railEntry, spot railSpot) int {
	for i, e := range entries {
		if e.group != spot.group {
			continue
		}
		switch {
		case e.node == nil && spot.id == 0:
			return i
		case e.node != nil && e.node.id == spot.id:
			return i
		}
	}
	return -1
}

// railFocusIndex is where the cursor is in the current entry list, or -1 when
// the roster does not have the keyboard.
func (a *app) railFocusIndex(entries []railEntry) int {
	if !a.railHold || len(entries) == 0 {
		return -1
	}
	if at := railFocusAt(entries, a.railWhere); at >= 0 {
		return at
	}
	// THE CURSOR FOLLOWS THE WORK, not the group. A node that lands moves out
	// from under a person who was watching it — that is the moment they were
	// watching FOR — so the row is looked for wherever it went before anything
	// else is tried.
	if a.railWhere.id != 0 {
		for i, e := range entries {
			if e.node != nil && e.node.id == a.railWhere.id {
				return i
			}
		}
		// It went somewhere folded. Its new heading is where its row is, which is
		// the honest place to stand.
		if node := a.tasks[a.railWhere.id]; node != nil {
			if at := railFocusAt(entries, railSpot{group: a.railGroupOf(node)}); at >= 0 {
				return at
			}
		}
	}
	// The group itself emptied out from under the cursor. Its heading is gone
	// with it, so the top of the column is all that is left — and a roster that
	// dropped to row zero for any lesser reason would be moving a person's place
	// for them.
	if at := railFocusAt(entries, railSpot{group: a.railWhere.group}); at >= 0 {
		return at
	}
	return 0
}

// railTake gives the roster the keyboard, or hands it back.
func (a *app) railTake(hold bool) {
	if hold && !a.railShowing() {
		return
	}
	a.railHold = hold
	if hold {
		if entries := a.railEntries(); railFocusAt(entries, a.railWhere) < 0 && len(entries) > 0 {
			a.railWhere = railSpotOf(entries[0])
		}
	}
	a.touch()
}

// railKey is the roster's claim on the keyboard, and it is a claim it can only
// make ONCE IT HAS BEEN GIVEN ONE (ctrl+t on, esc off).
//
// The draft is this surface's rest state — a person types at it without looking
// — so a map that answered ↑ whenever it happened to be on screen would make
// every keystroke a question about which zone has the focus. Held, it takes six
// keys and gives everything else back: the letters still reach the box, so a
// person who starts typing is typing, not navigating.
//
// The guard is the same precedence law input.go states, restated rather than
// relied on because those keys are that file's: the door, the question the
// SESSION is blocked on, the three modal overlays and the two typed lists all
// outrank a map of work.
func (a *app) railKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	switch {
	case key == "ctrl+c", a.asking(), a.awaitingTask(),
		a.sheet.open, a.pick.open, a.copy.on, a.welcome.open,
		a.menu.open, a.comp.open:
		return nil, false
	}
	if key == "ctrl+t" {
		if !a.railHold && !a.railShowing() {
			// Nothing to hold. The key falls through rather than being eaten
			// silently, so a surface that grows another meaning for it later is
			// not fighting a map that is not on screen.
			return nil, false
		}
		a.railTake(!a.railHold)
		return nil, true
	}
	if !a.railHold {
		return nil, false
	}
	switch key {
	case "esc":
		// esc is the dismiss key everywhere on this surface, and what it dismisses
		// here is the focus itself — back to the box, which is where the keyboard
		// lives when nobody has asked for it.
		a.railTake(false)
		return nil, true
	case "up":
		a.railMove(-1)
		return nil, true
	case "down":
		a.railMove(1)
		return nil, true
	case "right":
		a.railFold(true)
		return nil, true
	case "left":
		a.railFold(false)
		return nil, true
	case "enter":
		return a.railEnter(), true
	}
	return nil, false
}

// railMove walks the entry list, headings included.
//
// A HEADING IS NAVIGABLE AND ACTIVATES NOTHING. It is the row that folds its
// group, so a cursor that skipped it would put the fold behind a key nobody
// could aim — and it opens no room, because a heading is a fact about the rows
// under it and not a piece of work. The walk clamps at both ends, the way every
// other list on this surface does ([moveCursor]).
func (a *app) railMove(delta int) {
	entries := a.railEntries()
	at := a.railFocusIndex(entries)
	if at < 0 {
		return
	}
	a.railWhere = railSpotOf(entries[moveCursor(at, delta, len(entries))])
	a.touch()
}

// railFold is →/←: open the focused row's group, or close it.
//
// FROM A NODE, ← CLOSES THE GROUP THE NODE IS IN and leaves the cursor on the
// heading. That is where the row just went, and a cursor left pointing into a
// hundred and forty-eight rows nobody can see would be a position that means
// nothing.
func (a *app) railFold(open bool) {
	entries := a.railEntries()
	at := a.railFocusIndex(entries)
	if at < 0 {
		return
	}
	e := entries[at]
	if !open {
		a.railWhere = railSpot{group: e.group}
	}
	a.railSetOpen(e.group, open)
}

// railEnter is the one activating key: a heading folds, a node opens its room.
func (a *app) railEnter() tea.Cmd {
	entries := a.railEntries()
	at := a.railFocusIndex(entries)
	if at < 0 {
		return nil
	}
	e := entries[at]
	if e.node == nil {
		a.railToggle(e.group)
		return nil
	}
	a.openRoomFor(e.node.id, e.node.title)
	return a.takeRoomPump()
}

// ── the footer ──────────────────────────────────────────────────────────────

// railFootOrder is the order the footer counts the groups in, and it is not the
// column's order: the column leads with what is asking for a decision because
// that is where the eye starts, and the footer leads with what is HAPPENING
// because a total is read as a state of the session.
var railFootOrder = [railGroupCount]railGroup{railRunning, railAttention, railIdle, railParked, railDone}

// railFootMax is how many lines the footer may spend. Three is the whole
// aggregate at the full width; a fourth would be the column reporting on itself.
const railFootMax = 3

// railFootRows is the aggregate: what the window cannot show, said once at the
// bottom of the column.
//
//	Σ $1.42 · 312k tok
//	3 running · 1 needs you
//	148 parked · 12 done
//
// THE MONEY IS THE SESSION'S, AND THAT IS THE HONEST SUM. Per-node spend is not
// on the seam and cannot be: internal/session folds a finished node's usage into
// the session's own auxiliary total the moment its child closes (task_run.go's
// foldTaskUsage), so the figure beside the Σ ALREADY CONTAINS every node in this
// column, plus the conversation that proposed them. It is therefore drawn as the
// whole and never per row — a per-row share is the one number this surface would
// have to invent — and the Σ is what says so.
//
// The two figures are drawn only when they are not zero. A session that has been
// told nothing about what it spent says nothing, rather than reporting $0.00
// beside a hundred and forty-eight nodes.
func (a *app) railFootRows(width, height int) []string {
	if width < 8 || height < 4 {
		return nil
	}
	var segs []string
	if a.cost > 0 {
		segs = append(segs, dollars(a.cost))
	}
	if a.tokens > 0 {
		segs = append(segs, tokenWord(a.tokens)+" tok")
	}
	members := a.railMembers()
	for _, g := range railFootOrder {
		if n := len(members[g]); n > 0 {
			segs = append(segs, itoa(n)+" "+railGroupWords[g])
		}
	}
	if len(segs) == 0 {
		return nil
	}
	// The footer never takes more than a third of the column: a roster that is
	// mostly its own summary has stopped being a roster.
	rooms := min(railFootMax, height/3)
	lines := railPack(segs, width, rooms, railSigma)
	out := make([]string, 0, len(lines)+1)
	// ONE BLANK ABOVE IT, when the column can lend one — whitespace is how this
	// surface separates blocks, and a rule across a two-cell column would be a
	// border on a seam.
	if len(lines)+1 < height {
		out = append(out, "")
	}
	for _, line := range lines {
		out = append(out, a.pal.dim(line))
	}
	return out
}

// railSigma opens the footer's first line, and it is the whole of what makes the
// figures behind it readable: this is the sum of everything, including what the
// column folded away.
const railSigma = "Σ "

// railPack folds the footer's segments into at most rooms lines of at most width
// cells, joined by this surface's own separator.
//
// A segment that will not fit is DROPPED and the fold is said out loud with the
// ellipsis this surface truncates everything with: a footer that silently stops
// counting is a footer that claims the session is smaller than it is.
func railPack(segs []string, width, rooms int, lead string) []string {
	if rooms < 1 || width < 1 {
		return nil
	}
	out := make([]string, 0, rooms)
	line := lead
	for _, seg := range segs {
		add := seg
		if line != lead {
			add = " · " + seg
		}
		if ansi.StringWidth(line)+ansi.StringWidth(add) <= width {
			line += add
			continue
		}
		// THE FIRST SEGMENT KEEPS THE Σ whatever the width: a column too narrow
		// for "Σ $1.42" is a column that has to choose, and the sign is what says
		// the figure is a total rather than a row's.
		if line == lead {
			line = fit(lead+seg, width)
			continue
		}
		out = append(out, line)
		if len(out) == rooms {
			out[rooms-1] = fit(out[rooms-1]+" "+glyphMore, width)
			return out
		}
		line = fit(seg, width)
	}
	// The loop returns the moment the last line is spoken for, so what reaches
	// here is a line with room left in the block.
	if line != lead {
		out = append(out, line)
	}
	return out
}

// railNodeRows is one node: WHAT IT IS on the first line, and what is true of it
// on the second.
//
//	⠙ Fix the nil-map crash  #7
//	  12s
//	✓ Collect sources        #9
//	  merged
//	◌ Mix audio             #11
//	  waits: Collect sources
//
// THE NAME LEADS AND THE HANDLE TRAILS. The glyph and the title are what a person
// reads down this column — the state, and the words they themselves approved —
// and the id is what identifies the node to the MACHINE: it is the number the
// engine says in its own sentences ("task 7 finished", session's task_run.go),
// the thing to type when you go looking for the branch, and the least interesting
// fact on the row. So it is dim, it is at the far end, and the title is measured
// against what is left rather than the other way round: a column that led with
// its ids would read like a process table.
//
// THE SEAM CARRIES NO AGENT TYPE because the seam has none — internal/session's
// TaskNotice names a node's work and never its worker. When it grows one it joins
// the id in exactly this slot, in exactly this hue.
func (a *app) railNodeRows(node *taskNode, width int) []string {
	title, room := node.title, width-2
	meta := railMetaWord(node)
	// A column too narrow to carry both spends what it has on the name. The
	// handle is a convenience; the title is the row.
	if room-ansi.StringWidth(meta)-1 >= railTitleFloor {
		room -= ansi.StringWidth(meta) + 1
	} else {
		meta = ""
	}
	title = fit(title, room)
	line := a.railGlyph(node) + " " + a.railTitle(node, title)
	if meta != "" {
		if pad := room - ansi.StringWidth(title) + 1; pad > 0 {
			line += strings.Repeat(" ", pad)
		}
		line += a.pal.dim(meta)
	}
	out := []string{line}
	for _, under := range a.railUnder(node, width-2) {
		out = append(out, "  "+under)
	}
	return out
}

// railTitleFloor is how little room a title may be left with before the id gives
// up its cells. Twelve is about two words — under that the row has stopped
// naming the work.
const railTitleFloor = 12

// railMetaWord is the node's handle: the id the engine calls it by.
func railMetaWord(node *taskNode) string { return "#" + itoa(int(node.id)) }

// railTitle paints an already-fitted title. The cut happens at the call site
// because that is where the id's cells are measured out of it ([app.railNodeRows]):
// a title fitted here and trimmed there would be a row measured twice.
func (a *app) railTitle(node *taskNode, title string) string {
	if node.state == session.TaskRunning {
		return a.pal.ink(title)
	}
	return a.pal.muted(title)
}

// railUnder is what a node says under its own title: the clock while it runs,
// what it waits on while it is blocked, and how the branch came home once it has
// landed.
//
// It WRAPS rather than truncates, up to [railUnderRows]. Everything else on this
// surface cuts to an ellipsis, and everything else on this surface is cutting a
// sentence a person can reconstruct; the two facts down here — the name of a
// branch that did not merge, the name of the node being waited on — are the only
// handles back to work that is not on screen, and half of one of those is worth
// nothing at all.
func (a *app) railUnder(node *taskNode, width int) []string {
	paint, text := a.pal.dim, ""
	switch node.state {
	case session.TaskRunning:
		text = countUpWord(a.now().Sub(node.began))
	case session.TaskQueued:
		// THE DEPENDENCY SENTENCE, and it is v1's own words — internal/tui says
		// "waits: <title>" and a person who has used that surface has already
		// learned what it means. In a one-node graph nothing is ever unmet and
		// this draws nothing at all; the model carries the edges regardless, so
		// the day the executor grows them the rail already knows.
		if waits := a.railWaits(node); waits != "" {
			text = "waits: " + waits
		}
	default:
		switch node.merge {
		case mergeWordConflicted:
			// THE ONE LOUD ROW ON THE RAIL. A branch that did not merge is work
			// that is finished and not delivered, and its branch is the only
			// thing that gets a person back to it.
			paint, text = a.pal.bad, mergeWordConflicted+" · "+node.branch
		case mergeWordAborted:
			// STOPPED, AND ITS BRANCH KEPT — in those words, and not in the
			// engine's. "aborted" is internal/session's vocabulary for a branch
			// that never merged, and on a screen it reads as a crash: the commonest
			// way a node wears this word is that a person stopped it, or that it
			// ran out of the steps it was given, and neither of those is a failure
			// of anything. The row says what is true and what to do about it —
			// nothing went wrong, and the work is still on that branch.
			text = taskStoppedKept + " · " + node.branch
		default:
			text = node.merge
		}
	}
	if text == "" {
		return nil
	}
	lines := railWrap(text, width)
	if len(lines) > railUnderRows {
		lines = lines[:railUnderRows]
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, paint(line))
	}
	return out
}

// railUnderRows caps that block. Two is what a branch name or a prerequisite's
// title takes at this width; past it the rail would be a paragraph, and the
// transcript is where paragraphs live.
const railUnderRows = 2

// railWrap breaks a rail sentence ON ITS SPACES, and breaks a word only when
// one word is wider than the whole column.
//
// Neither of the two wrappers this tree already has does that. The transcript's
// [wrap] breaks anywhere, which is right for a paragraph; ansi.Wordwrap always
// treats a hyphen as a breakpoint, which turns "task/fix-nil-map" into
// "task/fix-nil-" and "map". A branch cut in the middle of its name is a branch
// nobody can retype, and retyping it is the entire reason it is on screen.
func railWrap(text string, width int) []string {
	if width < 1 {
		return nil
	}
	var out []string
	for _, word := range strings.Fields(text) {
		if len(out) > 0 {
			if joined := out[len(out)-1] + " " + word; ansi.StringWidth(joined) <= width {
				out[len(out)-1] = joined
				continue
			}
		}
		if ansi.StringWidth(word) <= width {
			out = append(out, word)
			continue
		}
		out = append(out, strings.Split(ansi.Wrap(word, width, ""), "\n")...)
	}
	return out
}

// railWaits names the prerequisites this node is still blocked on, oldest
// first. A dependency this surface has never seen an update for is skipped
// rather than named as an id: a row that says "waits: 7" is a row that has told
// a person nothing.
func (a *app) railWaits(node *taskNode) string {
	var names []string
	for _, id := range node.dependsOn {
		dep := a.tasks[id]
		if dep == nil || dep.state == session.TaskDone {
			continue
		}
		names = append(names, dep.title)
	}
	return strings.Join(names, " · ")
}

// railGlyph is the node's state, in one cell.
//
// ✓ IS THE ONE SUCCESS GLYPH ON THIS SURFACE, and the tool rows' law against it
// (toolview.go: a column of ticks is a column read to learn nothing) does not
// reach here. There, a quiet line IS the success and the row stays on screen; a
// rail row is a presence that disappears when the work comes home, so the tick
// is not decoration on a permanent row — it is the last thing the row says.
func (a *app) railGlyph(node *taskNode) string {
	switch node.state {
	case session.TaskDone:
		return a.pal.muted(a.linearMark(glyphDone, glyphDoneASCII))
	case session.TaskFailed:
		return a.pal.bad(a.linearMark(glyphBad, glyphBadASCII))
	case session.TaskRunning:
		if a.linear {
			return a.pal.accent(glyphRunASCII)
		}
		return a.pal.accent(tokens.Spinner(a.paints / spinnerStep))
	default:
		return a.pal.dim(a.linearMark(glyphQueued, glyphQueuedASCII))
	}
}

// glyphDone marks a node that landed. See [app.railGlyph] for why this surface
// has one at all.
const (
	glyphDone      = "✓"
	glyphDoneASCII = "+"
)

// railJoin lays one conversation row beside the rail's column for that row. It
// is the ONLY place the two columns meet, and it pads through
// [ansi.StringWidth] because a row measured through its escape sequences is a
// row measured wrong.
func (a *app) railJoin(text, rail string) string {
	if rail == "" {
		return text
	}
	body := a.bodyWidth()
	switch width := ansi.StringWidth(text); {
	case width < body:
		text += strings.Repeat(" ", body-width)
	case width > body:
		// A row WIDER than the column it is drawn in would run under the rail,
		// and the frozen viewport is where one comes from: copy mode snapshots
		// the transcript when the key is pressed, at whatever width it was laid
		// out for (copymode.go). Cutting the row to its column is the lesser of
		// the two wrongs — the alternative is a rail with a sentence through it.
		text = fit(text, body)
	}
	return text + rail
}

// ── the update, folded in ───────────────────────────────────────────────────

// taskUpdate upserts one node and, when it lands, writes the transcript's line
// about it.
//
// THE DE-DUP IS (ID, STATE), and it is not an optimization. An update that
// happens while a turn runs arrives on BOTH lanes — the turn's hub and the
// standing subscription (session's emitTaskUpdate says so out loud) — so every
// in-turn state change is delivered twice, and a surface that took both would
// write two "task done" lines into the conversation. The states a node moves
// through are monotonic (queued → running → done|failed), so a repeat of the
// state last seen for an id is always the second copy of one event.
func (a *app) taskUpdate(ev session.Event) {
	notice := ev.Task
	if notice == nil {
		return
	}
	if last, seen := a.taskSeen[notice.ID]; seen && last == notice.State {
		return
	}
	if a.taskSeen == nil {
		a.taskSeen = map[uint64]session.TaskState{}
	}
	a.taskSeen[notice.ID] = notice.State

	node := a.tasks[notice.ID]
	if node == nil {
		if a.tasks == nil {
			a.tasks = map[uint64]*taskNode{}
		}
		node = &taskNode{id: notice.ID}
		a.tasks[notice.ID] = node
		a.taskOrder = append(a.taskOrder, notice.ID)
	}
	if title := strings.TrimSpace(notice.Title); title != "" {
		node.title = title
	}
	node.state = notice.State
	if len(notice.DependsOn) > 0 {
		node.dependsOn = notice.DependsOn
	}
	if notice.Branch != "" {
		node.branch = notice.Branch
	}
	if notice.Merge != "" {
		node.merge = notice.Merge
	}
	if notice.Report != "" {
		node.report = notice.Report
	}
	// The clock is anchored ONCE, from the age the update reported, so the row
	// counts on the frame tick instead of standing still between events.
	if notice.State == session.TaskRunning && node.began.IsZero() {
		node.began = a.now().Add(-notice.Elapsed)
	}
	// A node that started is a proposal that was approved, whatever answered it:
	// the card stops asking here for the case where the engine's clock, and not
	// this surface, was the thing that said yes.
	if a.task != nil && a.task.id == notice.ID && !a.task.settled() {
		a.task.verdict = taskClockWord
		a.markCardStale(a.task)
	}
	switch notice.State {
	case session.TaskDone, session.TaskFailed:
		node.elapsed = notice.Elapsed
		a.landedNote(taskLandedWord(node))
	}
	a.touch()
}

// landedNote is the note a finished node writes, MARKED as one.
//
// The mark buys it one thing: the blank row after it (render.go's [app.layout]).
// A landed note is the end of something that started rows ago and outlived the
// turn it was proposed in, and a line about work that has come home wedged
// against the next paragraph reads as a sentence in it.
func (a *app) landedNote(text string) {
	a.note(text)
	if at := len(a.entries) - 1; at >= 0 && a.entries[at].kind == entryNote {
		a.entries[at].landed = true
	}
}

// taskLandedWord is the one line the transcript keeps about a node:
//
//	task Fix the nil-map crash done in 2m 10s · merged
//	task Collect sources failed in 4s — the tests did not build
//	task Mix audio failed in 2m · stopped — branch kept · task/mix — stopped: 40 steps and no finish
//
// It is written where the rail row is about to disappear, and between them they
// say the whole thing once: the rail said it was alive, this says how it ended.
//
// THE REASON IS THE ENGINE'S SENTENCE, VERBATIM. session's own report opens with
// "stopped: 40 steps and no finish" or "stopped before it finished"
// (task_run.go), and that word is the difference between work that broke and
// work that ran out — so nothing here rewrites it, and a node that ended with no
// report at all still leads with it, because a failure this surface was told
// nothing about is a node that stopped.
func taskLandedWord(node *taskNode) string {
	line := "task " + node.title
	if node.state == session.TaskFailed {
		line += " failed"
	} else {
		line += " done"
	}
	if word := countUpWord(node.elapsed); word != "" {
		line += " in " + word
	}
	// A KEPT BRANCH IS NAMED HERE TOO, and it is named for the reason the rail
	// names it: the rail row goes away when the person deals with it, and this
	// line is what is left when they scroll back looking for where the work went.
	switch node.merge {
	case mergeWordConflicted:
		line += " · " + node.merge + " · " + node.branch
	case mergeWordAborted:
		line += " · " + taskStoppedKept + " · " + node.branch
	case "":
	default:
		line += " · " + node.merge
	}
	if node.state == session.TaskFailed {
		line += " — " + firstNonEmpty(strings.TrimSpace(firstLine(node.report)), taskStoppedWord)
	}
	return line
}

// tasksAnimating reports whether anything on this surface's task side is
// moving: a countdown running down, a spinner turning, a clock counting up.
// It is what keeps the paint clock alive between turns — a node runs for
// minutes with no stream open, and the rail would otherwise freeze at whatever
// the last event drew.
func (a *app) tasksAnimating() bool {
	if a.awaitingTask() && !a.task.deadline.IsZero() {
		return true
	}
	if !a.railShowing() {
		return false
	}
	// A ROSTER FULL OF SETTLED WORK IS A STILL PICTURE. The column stands for the
	// whole session now, so "is anything on it moving" is a question about the
	// running nodes and not about the list's length — otherwise a session that
	// finished its work an hour ago would still be repainting a spinner-less
	// column thirty times a second.
	for _, node := range a.tasks {
		if node != nil && node.state == session.TaskRunning {
			return true
		}
	}
	return false
}

// dropTasks forgets the whole task side. It runs where the agent is replaced
// (/new): a rail carried into the next conversation would be claiming nodes
// that died with the session that started them.
func (a *app) dropTasks() {
	// A ROOM GOES WITH ITS NODE. The page on screen is one node's transcript,
	// and a node that died with its session is a page that cannot be steered,
	// cannot be finished and cannot be left by any door but this one (room.go).
	a.closeRoom()
	a.task = nil
	a.tasks = nil
	a.taskOrder = nil
	a.taskSeen = nil
	a.taskLane = nil
	// THE ROSTER GOES WITH ITS NODES, the keyboard included. A column that kept
	// its folds and its cursor into the next conversation would be a map of work
	// that no longer exists, holding keys the draft is waiting for.
	a.railOpen = nil
	a.railTop = 0
	a.railWhere = railSpot{}
	a.railHold = false
}

// redirectLane is the placeholder the input box wears while a proposal is open.
//
// It is applied to the block the input already rendered rather than passed into
// it, because the box's hint slot belongs to the model picker (input.go) and the
// two are never up at the same time. An empty draft renders as the bare prompt,
// which is exactly the row a placeholder goes on.
func (a *app) redirectLane(rows []string, width int) []string {
	if !a.awaitingTask() || len(rows) == 0 || !a.input.empty() || a.pick.open {
		return rows
	}
	room := width - ansi.StringWidth(prompt)
	out := append([]string(nil), rows...)
	out[0] = a.pal.dim(prompt) + a.pal.ask(fit(taskRedirectLane, room))
	return out
}
