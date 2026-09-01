package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// STOPPING WORK, AND ASKING FIRST.
//
// Until this file the only thing on this surface that could end work was a
// sentence typed at the model: "stop task 7", and hope. Everything else —
// walking into a node, steering it, reading its transcript, watching what it
// costs — a person could do with their own hands, and the one act with a cost
// attached was the one they had to ask somebody else to do.
//
// So there is a key and there is a button, and between them and the work there
// is exactly one card.
//
//	 ? Stop this run? In-flight nodes halt; partial results stay.
//	   [stop it]  ▌[keep going]
//
// ── THE CARD IS ALWAYS ASKED, AND ITS DEFAULT IS "NO" ──
//
// There is no bypass key, no modifier that skips the question, and no
// "don't ask me again". Stopping is not reversible — a cut node's turn is over
// and its context is gone — and every gesture on this surface that cannot be
// undone is confirmed once, in the same shape (permissions.go's floor states
// the general law). The CURSOR STARTS ON "keep going", which is the same rule
// said in the one place a person actually reads it: a card whose destructive
// answer is under the enter key is a card that stops work when somebody presses
// enter for the reason people press enter, which is to make a question go away.
//
// ── THE KEY AND THE POINTER ANSWER THE SAME CARD ──
//
// `x` raises it — on the roster's focused chip, and inside a room or a run's
// page — and a ✕ in the room's header and on the focused chip raises exactly
// the same card. One question, two hands, no second grammar: ←/→ walk the two
// answers, enter takes the one under the cursor, esc is "keep going", and a
// press on either answer is that answer.
//
// `x` IS TAKEN OVER AN EMPTY BOX AND NOWHERE ELSE, which is the rule every key
// on this surface that is also a letter is held to (task.go's [app.taskKey],
// room.go). A room's box steers the worker in it; a page whose keys ate the
// sentence somebody was typing would be a page you cannot steer. The known cost
// is the one keystroke: `x` typed as the first letter of a sentence into a room
// raises the card instead of going in the box, and the card's default answer
// puts it away again with nothing else changed.
//
// ── ESC IS STILL BACK, AND NEVER STOP ──
//
// esc closes a card, then a room, then a page, in that order, and it never ends
// work. A dismiss key that also cancelled would make leaving a page a thing
// people did carefully.

// stopAgent is the one door onto ending work, whatever kind it is
// (internal/session's cancel.go). It is a fourth optional assertion on this
// surface for [taskRoomAgent]'s reason exactly: an agent that has never heard of
// stopping keeps everything else it had, and this surface says so in one note
// rather than drawing a button that could only fail.
type stopAgent interface {
	// Cancel stops the work an id names and answers with the line to show.
	Cancel(id string) (string, error)
}

// stopDoors is the stopping half of the agent under this surface, when it has
// one.
func (a *app) stopDoors() (stopAgent, bool) {
	doors, ok := a.agent.(stopAgent)
	return doors, ok
}

// ── what the card is about ──────────────────────────────────────────────────

// stopTarget is one piece of work this surface can offer to stop: the id the
// session's own door takes, and the words the card says about it.
//
// The id is already PREFIXED (session's CancelTask, CancelRun): the surface
// knows which kind of thing it is pointing at and the session must not have to
// guess from a bare number.
type stopTarget struct {
	id string
	// noun is what this work is called in the card's question — "run" or
	// "task" — and detail is the promise under it. They travel together because
	// a question and the promise that answers it must not be able to disagree
	// about what is being ended.
	noun, detail string
}

func (t stopTarget) empty() bool { return t.id == "" }

// question is the card's whole first line.
func (t stopTarget) question() string {
	return "Stop this " + t.noun + "? " + t.detail
}

// The two nouns and the two promises. Each promise is what actually happens —
// not a reassurance — because the person reading it is deciding whether they
// can afford the ending.
const (
	stopRunNoun  = "run"
	stopTaskNoun = "task"
	// stopRunDetail is internal/orchestrate's own law said to a person: the
	// contexts of the nodes in flight are cut and the digests of the nodes that
	// landed are kept.
	stopRunDetail = "In-flight nodes halt; partial results stay."
	// stopTaskDetail is task_run.go's abortedMerge said to a person: nothing a
	// node wrote is thrown away by ending it, and the branch is where it is.
	stopTaskDetail = "Its work halts; the branch it wrote on is kept."
	// stopDesignDetail is what the same card says over a node that is DESIGNING a
	// harness (session's harness_task.go). It has no branch and it wrote no
	// files, and nothing reaches the registry until somebody approves the card —
	// so the reassurance above would be pointing at work that does not exist, and
	// this is the honest promise in its place.
	stopDesignDetail = "The page it is writing is dropped; nothing was saved."
)

// stopCard is one raised confirmation: what it is about, and which answer the
// cursor is on.
type stopCard struct {
	target stopTarget
	// pick indexes [stopAnswers]. It opens on the SAFE one and this is the only
	// place that is decided.
	pick int
	// spans are the two answers' columns on the row they were drawn on, written
	// by the layout and read by the press — the same bargain every other
	// pointer target on this surface makes (render.go's [hudSpan] callers).
	spans []hudSpan
}

// stopActWord is ENDING THIS WORK, said once for every surface that offers it:
// the card's own first answer, and the verb the tasks place puts on its `→`
// strip (place_tasks.go's [tasksPlace.verbs]). Two spellings of one act is two
// things for a person to learn about one key.
const stopActWord = "stop it"

// stopAnswers are the two answers, in the order they are drawn: the act first
// because it is what the card is about, the refusal second because it is where
// the cursor starts.
var stopAnswers = [...]string{stopActWord, "keep going"}

// stopKeepAt is which of them is "no", and it is the cursor's home.
const stopKeepAt = 1

// stopping reports whether the card owns the keyboard.
func (a *app) stopping() bool { return a.stop != nil }

// raiseStop puts the question up over whatever the person was looking at. The
// page underneath stays exactly where it was: the question is about the work,
// not about the page, and answering either way leaves them where they were.
func (a *app) raiseStop(target stopTarget) {
	if target.empty() {
		return
	}
	if _, ok := a.stopDoors(); !ok {
		// THE BUILD GUARD (room.go's, roomorch.go's): the door is an assertion
		// and not a compile-time requirement, so a surface driven by an agent
		// that cannot stop work says so and changes nothing.
		a.note(stopUnavailableWord)
		return
	}
	a.stop = &stopCard{target: target, pick: stopKeepAt}
	// The typed lists follow the draft, and the draft is spoken for while a
	// question is up — the same law the approval question states (consent.go).
	a.closeLists()
	a.touch()
}

// stopUnavailableWord is the degraded case, in the vocabulary the other two
// unavailable doors on this surface use.
const stopUnavailableWord = "stopping work is unavailable — this session has no door onto it"

// dropStop takes the question down and changes nothing else.
func (a *app) dropStop() {
	if a.stop == nil {
		return
	}
	a.stop = nil
	a.touch()
}

// stopTake answers the card. Anything but the act is the card simply going
// away; the act asks the session and reports what it said.
func (a *app) stopTake(at int) {
	card := a.stop
	if card == nil {
		return
	}
	target := card.target
	a.dropStop()
	if at != 0 {
		return
	}
	doors, ok := a.stopDoors()
	if !ok {
		a.note(stopUnavailableWord)
		return
	}
	line, err := doors.Cancel(target.id)
	if err != nil {
		// The engine's own sentence, kept: a stop that could not be given is
		// work still running, and a surface that swallowed the reason would leave
		// a person pressing the same key again (roomorch.go's [app.orchAnswer]
		// keeps the same rule about the same kind of refusal).
		a.stopSay(err.Error())
		return
	}
	a.stopSay(line)
}

// stopSay puts one line where the person is looking: on the run's page when one
// is open, and in the conversation otherwise. It is [app.orchNoteEvent]'s
// arrangement for the same reason — a person watching a graph reads the news
// where the graph is.
func (a *app) stopSay(line string) {
	if line = strings.TrimSpace(line); line == "" {
		return
	}
	if run := a.orchOf(); run != nil {
		run.notes = append(run.notes, line)
		a.roomTouched()
		return
	}
	a.note(line)
}

// ── what is stoppable from where you are standing ───────────────────────────

// stopHere is the work `x` and the header's ✕ are aimed at, or the empty target
// when there is nothing here to stop.
//
// THE ROOM OUTRANKS THE ROSTER, because a room is where you ARE and the roster
// is a list you can see from anywhere. A person standing inside a node's page
// who presses `x` means that node, whatever row the roster's cursor happens to
// be resting on behind them.
func (a *app) stopHere() stopTarget {
	if run := a.orchOf(); run != nil {
		return stopRunTarget(run.id, run.snap, run.known)
	}
	if a.room != nil {
		return a.stopTaskTarget(a.tasks[a.room.id])
	}
	return a.stopTaskTarget(a.railFocusNode())
}

// railFocusNode is the node the roster's cursor is standing on, or nil — while
// the roster does not hold the keyboard there is no cursor and nothing is being
// aimed at. Two fields are the whole of that answer, and both have to hold: the
// column has the keyboard, and its cursor is on a row rather than on nothing.
func (a *app) railFocusNode() *taskNode {
	if !a.railHold || a.railWhere.id == 0 {
		return nil
	}
	return a.tasks[a.railWhere.id]
}

// stopRunTarget is one adaptive run as a card can offer it, and the empty
// target for a run that is already over — a ✕ on a finished run is a dead cell,
// and a card raised over one would be a question with no answer that does
// anything.
//
// AND A RUN THIS SESSION CANNOT SEE IS OFFERED NOTHING EITHER, which is what
// `known` is for. A run whose rows were restored from yesterday's checkpoint
// draws a page and no shape (roomorch.go's orchUnknownWord): the session holds
// no orchestrator for that id, so its snapshot is the zero one — neither Done
// nor Stopped — and without this clause the ✕ was drawn over it and answered
// "there is no run … in this session" (session's cancel.go). The rows are
// history the column keeps; history is not stoppable.
func stopRunTarget(id string, snap orchestrate.Snapshot, known bool) stopTarget {
	if strings.TrimSpace(id) == "" || !known || snap.Done || snap.Stopped {
		return stopTarget{}
	}
	return stopTarget{id: session.CancelRun + ":" + id, noun: stopRunNoun, detail: stopRunDetail}
}

// stopTaskTarget is one node as a card can offer it. Only work that has not
// settled is offered: queued and running are the two states a stop means
// anything in, and the third — a node held at a run's fuel gate — is the run's
// question and not this one.
func (a *app) stopTaskTarget(node *taskNode) stopTarget {
	if node == nil {
		return stopTarget{}
	}
	// A BACKGROUND JOB IS NOT STOPPABLE FROM HERE, AND SO NO ✕ IS DRAWN ON IT.
	// The id on a job's row is the roster's own and names nothing [session.Agent.Cancel]
	// can find (session's TaskKindJob says so out loud), so a key wired to it
	// would raise a card whose only possible answer was the engine refusing. A
	// capability that cannot work is absent, not broken: the row draws no ✕, `x`
	// aims past it, and the way to end a job is the `jobs` tool's own kill.
	if node.kind == session.TaskKindJob {
		return stopTarget{}
	}
	switch node.state {
	case session.TaskQueued, session.TaskRunning:
		return stopTarget{
			id:   session.CancelTask + ":" + itoa(int(node.id)),
			noun: stopTaskNoun,
			// WHAT A STOP KEEPS DEPENDS ON WHAT THE NODE IS. Ordinary work leaves
			// a branch behind and the card says where it is; a harness being
			// designed has no worktree and wrote no files, and nothing reaches the
			// registry until somebody approves the card — so the reassurance would
			// be pointing at work that does not exist (session's TaskKindHarness).
			detail: stopDetailFor(node.kind),
		}
	}
	return stopTarget{}
}

// stopDetailFor is the second line of the confirmation, chosen by what the node
// is rather than by what it is doing.
func stopDetailFor(kind session.TaskKind) string {
	if kind == session.TaskKindHarness {
		return stopDesignDetail
	}
	return stopTaskDetail
}

// stopOffered reports whether there is anything here to stop, which is what
// decides whether the ✕ is drawn at all.
func (a *app) stopOffered() bool { return !a.stopHere().empty() }

// ── the keyboard ────────────────────────────────────────────────────────────

// stopKey is the card's claim on the keyboard, read in [app.Update] ABOVE the
// roster and above the room: it is a question raised from either of them, so a
// key that reached the page underneath would be a key aimed at the very thing
// being stopped.
//
// It reports whether it took the key. While the card is up it takes everything
// but the door, the way the steer guard does (room.go's [app.guardKey]): two
// keys answer, and every other key does nothing rather than typing into a box
// whose enter is spoken for.
func (a *app) stopKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	// THE CHEAP TEST FIRST. This runs on every keypress the surface takes, ahead
	// of the roster and the room, and all but two of them are neither the raise
	// key nor a key aimed at a card that is up.
	if !a.stopping() && key != stopRaiseKey {
		return nil, false
	}
	// The precedence law input.go states, restated rather than relied on: the
	// door, the question the SESSION is blocked on, the modal overlays and the
	// typed lists all outrank this, exactly as they outrank the room.
	//
	// HOME IS ON THAT LIST BESIDE THE OTHER FULLSCREEN PAGES, and was missing
	// from it. Its box is a search AND a new conversation at the same moment
	// (home.go), so at home an `x` is a character somebody is typing — and a stop
	// card raised over a screen the frame is not drawing would be a question
	// nobody can see, answered by the next key they press.
	switch {
	case key == "ctrl+c", a.asking(), a.awaitingTask(), a.guarding(),
		a.at(pageSettings), a.at(pageTasks), a.at(pageHome), a.deckShowing(), a.pick.open,
		a.roster.open, a.copy.on, a.welcome.open, a.menu.open, a.comp.open,
		a.rew.on, a.rewSheet.open:
		return nil, false
	}
	if a.stopping() {
		switch key {
		case "left":
			a.moveStop(-1)
		case "right":
			a.moveStop(1)
		case "enter":
			a.stopTake(a.stop.pick)
		case "esc":
			// esc IS "keep going" and never the act. The dismiss key on this
			// surface takes questions away; a dismiss that also ended work would
			// be the one key nobody could press safely.
			a.dropStop()
		}
		return nil, true
	}
	// A LETTER IS A LETTER THE MOMENT THERE IS A SENTENCE. See the header: the
	// box wins every time, and this key is no exception.
	if key != stopRaiseKey || !a.input.empty() || a.recalling() {
		return nil, false
	}
	target := a.stopHere()
	if target.empty() {
		// Nothing here to stop. The key falls through rather than being eaten,
		// so it reaches the box as the letter it is.
		return nil, false
	}
	a.raiseStop(target)
	return nil, true
}

// stopRaiseKey is the one key that raises the card.
const stopRaiseKey = "x"

// moveStop walks the two answers and STOPS at the ends rather than wrapping.
// Two answers are read at a glance, and a cursor that reappeared at the far end
// would put "stop it" under a key pressed to reach "keep going" — which is the
// one mistake this whole card exists to prevent.
func (a *app) moveStop(delta int) {
	card := a.stop
	if card == nil {
		return
	}
	at := card.pick + delta
	switch {
	case at < 0:
		at = 0
	case at >= len(stopAnswers):
		at = len(stopAnswers) - 1
	}
	card.pick = at
	a.touch()
}

// ── the card, drawn ─────────────────────────────────────────────────────────

// stopHeight is what the card costs the frame: the question, and the answers
// under it.
func (a *app) stopHeight() int {
	if !a.stopping() {
		return 0
	}
	return 2
}

// stopRows draws it, in the question hue every other thing on this surface that
// is blocked on a keystroke wears (consent.go, room.go's guard). It shares the
// guard's slot above the draft — see [app.guardRows], which is the one place
// that says so.
func (a *app) stopRows(width int) []string {
	card := a.stop
	if card == nil || width < 4 {
		return nil
	}
	head := card.target.question()
	rows := []string{a.pal.askBold(glyphAsk) + a.pal.ask(fit(" "+head, width-ansi.StringWidth(glyphAsk)))}

	card.spans = card.spans[:0]
	line, at := stopAnswerPad, len(stopAnswerPad)
	for i, word := range stopAnswers {
		if i > 0 {
			line += stopAnswerGap
			at += len(stopAnswerGap)
		}
		// The cursor is the ROSTER'S OWN MARKER, borrowed through the helper the
		// run's page already borrows it with (roomorch.go's [app.orchLead]): it is
		// constant width, so moving between the two answers never reflows the row,
		// and it is the same cell this surface puts in front of "where enter goes"
		// everywhere else.
		lead := a.orchLead(i == card.pick)
		text := lead + "[" + word + "]"
		cols := ansi.StringWidth(text)
		painted := a.pal.ask(text)
		if i == card.pick {
			painted = a.pal.askBold(text)
		}
		// THE POINTER LIGHTS THE ANSWER AND NOT THE ROW, which is the settle row's
		// own answer to the same shape (tasksettle.go): two presses share this line
		// and they are the two ends of one decision, so a band across all of it would
		// promise "stop it" under a hand reaching for "keep going". The band goes
		// round exactly the answer's cells, OVER whatever ink it already wears —
		// weight says where the keyboard is and the background says where the pointer
		// is, and two channels stay legible together where two shades of one would
		// not.
		if a.hoveringStopAnswer(i) {
			painted = a.pal.cursor(painted, 0)
		}
		line += painted
		card.spans = append(card.spans, hudSpan{from: at, to: at + cols})
		at += cols
	}
	return append(rows, fit(line, width))
}

const (
	// stopAnswerPad is the answers' indent under the question, and
	// stopAnswerGap is what separates the two. The gap is wide because these are
	// the two ends of one decision and a person aiming a finger at one of them
	// must not be able to hit the other.
	stopAnswerPad = "  "
	stopAnswerGap = "   "
)

// ── the pointer ─────────────────────────────────────────────────────────────

// stopPress resolves a click aimed at stopping something and reports whether it
// took it. Two targets, in the order they are stacked on screen: the card's own
// answers while it is up, and the ✕ in the room's header.
func (a *app) stopPress(x, y int) bool {
	if a.copy.on || a.rew.on {
		return false
	}
	if a.stopping() {
		return a.stopCardPress(x, y)
	}
	return a.stopMarkPress(x, y)
}

// stopCardPress answers a press on the raised card.
//
// A PRESS ANYWHERE ON THE ANSWERS ROW IS THE ROW'S, whether or not it landed on
// an answer: the gap between them is three cells wide and it is a place people
// miss, and a miss that fell through to the draft box under it would put the
// caret in a sentence instead of answering a question about ending work.
func (a *app) stopCardPress(x, y int) bool {
	card := a.stop
	mark, ok := a.chromeAt(y)
	if card == nil || !ok || mark.kind != chromeStop {
		return false
	}
	if mark.index != 1 {
		return true // the question's own row; there is nothing on it to press
	}
	for at, span := range card.spans {
		if span.holds(x) {
			a.stopTake(at)
			return true
		}
	}
	return true
}

// stopMarkPress answers a press on the ✕ in a room's header.
//
// THE TOUCH TARGET IS THREE ROWS TALL AT THE PHONE TIER and one row everywhere
// else. A finger covers about three rows of a terminal, and this is the one
// control on the surface whose miss is expensive in both directions — hitting
// it by accident raises a card the person did not want, missing it leaves them
// with no way to stop the work at all. The top bar is the first row of the
// frame, so the box can only grow downward; it is claimed here, ahead of the bar
// and the body, and it rides the far right end of the bar where the transcript's
// own rows are ragged and rarely reach.
func (a *app) stopMarkPress(x, y int) bool {
	if !a.stopMarkAt(x, y) {
		return false
	}
	a.raiseStop(a.stopHere())
	return true
}

// stopMarkAt is that hit-test with nothing done about it, so the pointer can ask
// the same question the press asks and the mark can brighten on exactly the
// cells a click would act on (hover.go's law).
//
// THE HIT BOX IS THE PRESS'S OWN, three rows tall at the phone tier included.
// A hover that answered for one row while a press answered for three would be a
// control that stops looking pressable at the exact cell a thumb was aiming for.
//
// It reports nothing while the card is already up, which is [app.stopPress]'s own
// branch stated here: the card has taken the question, and a ✕ that lit under the
// pointer would offer to raise a card that is on the screen.
func (a *app) stopMarkAt(x, y int) bool {
	// THE MARK IS ONLY EVER ON THE BAR, so a frame with no bar has no mark — even
	// while a room is open and a span from some earlier frame is still in the
	// field. This is asked before the span because the span is a memory and this
	// is the present: at the phone tier and on a window too short to breathe the
	// bar is not drawn (topbar.go's [app.topBarShowing]), and without this the
	// three-row hit box below sat over the top of the transcript.
	if a.headHeight() == 0 {
		return false
	}
	if !a.roomOpen() || a.stopping() || !a.roomStop.holds(x) {
		return false
	}
	width, _ := a.size()
	rows := 1
	if layoutTier(width) == tierPhone {
		rows = stopTouchRows
	}
	if y < 0 || y >= rows {
		return false
	}
	return !a.stopHere().empty()
}

// stopTouchRows is how tall the ✕'s hit box is where a finger is the pointer.
const stopTouchRows = 3

// The mark itself, and the separator that puts it after the way out. It is a
// heavy multiplication ✕ rather than the surface's own ✗ (styles.go's
// [glyphBad]) because that one MEANS something already — work that failed — and
// a control wearing a state's mark is a header saying this run went wrong.
const (
	roomStopMark      = "✕"
	roomStopMarkASCII = "X"
)

// roomStopWord is the ✕ as the top bar draws it, or "" when there is nothing
// here to stop. It is UNPAINTED, for the reason topbar.go's [app.topBarWord]
// states: the whole line is painted once, and a hue nested inside a hue ends at
// the inner one's reset.
func (a *app) roomStopWord() string {
	if !a.stopOffered() {
		return ""
	}
	return a.linearMark(roomStopMark, roomStopMarkASCII)
}

// ── what a stopped thing looks like afterwards ──────────────────────────────

// glyphStopped marks work a PERSON ended. It is not [glyphBad]: a cross is a
// finding, and nobody found anything wrong with work that was stopped — the
// circle with a bar through it is the mark every device a person owns uses for
// "not permitted to continue", which is exactly what a stop is.
const (
	glyphStopped      = "⊘"
	glyphStoppedASCII = "/"
)

// stoppedGlyph is one node's cell when a person ended it, and false for every
// other node. It is UNPAINTED, like every other cell in that table: the hue is
// [app.taskStateInk]'s, and it is the dim one — the roster, the strip and the
// composer's room segment all read the pair, so the mark is the same cell and
// the same colour wherever it is drawn.
//
// IT ANSWERS ONLY FOR WORK THAT HAS LANDED. A node whose stop is still going
// through — the context is cut, the child is winding up — is still running, and
// the spinner is the honest thing to draw until it is not.
func (a *app) stoppedGlyph(node *taskNode) (string, bool) {
	if node == nil || !node.stopped || node.state == session.TaskRunning {
		return "", false
	}
	return a.linearMark(glyphStopped, glyphStoppedASCII), true
}

// taskStoppedByPerson is the word the header and the roster spell a stopped
// node with. It is the same word internal/session uses on the wire and the same
// word the card's button offers, because one act should not have three names.
const taskStoppedByPerson = "stopped"
