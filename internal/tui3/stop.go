package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/orchestrate"
	"github.com/Agent-Field/codeaf/internal/session"
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
//	 ?  Stop this run?
//	      In-flight nodes halt; partial results stay.
//	      1  stop it
//	      2  keep going
//	    [enter] take the pick · [esc] keep going · [←→] pick
//
// The block draws those rows (question.go) and this file owns the words in them.
// The head is the QUESTION and the row under it is the PROMISE — what stopping
// does not take away — on rows of their own, because one sentence carrying both
// said the promise twice. The answer the cursor is on is lit across its whole
// row and the whole row is pressable.
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
// answers, a DIGIT MOVES THE CURSOR onto the answer it names rather than giving
// it, enter takes the one under the cursor, esc is "keep going", and a press
// anywhere on an answer's row is that answer.
//
// `x` IS TAKEN OVER AN EMPTY BOX AND NOWHERE ELSE, which is the rule every key
// on this surface that is also a letter is held to (task.go's [app.taskKey],
// room.go). A room's box steers the worker in it; a page whose keys ate the
// sentence somebody was typing would be a page you cannot steer. The known cost
// is the one keystroke: `x` typed as the first letter of a sentence into a room
// raises the card instead of going in the box, and the card's default answer
// puts it away again with nothing else changed.
//
// ── AND ONE ROW NEEDS NO CURSOR AT ALL (#892) ──
//
// The roster's keyboard was made askable (`alt+t`) on the argument that "while
// the roster does not hold the keyboard there is no cursor and nothing is being
// aimed at" — and with three rows on screen that is exactly right: a bare `x`
// would have to guess which one, and guessing wrong ends an hour of work. But
// ONE stoppable row is the case the argument was written against and does not
// cover: there is nothing to choose between, so the keystroke cannot be aimed at
// the wrong thing, and the law the argument serves — one bare keystroke over a
// list should not be able to end an hour of work — is already satisfied by the
// card, which asks and defaults to *keep going*, not by the focus requirement.
// So with exactly one stoppable row visible, `x` raises that row's card without
// `alt+t` first. Two or more keep today's behaviour and fall through, because
// with nothing to aim at the only honest answer is the letter the key already
// is.
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
// The id is already PREFIXED (session's CancelTask, CancelRun, CancelJob): the surface
// knows which kind of thing it is pointing at and the session must not have to
// guess from a bare number.
type stopTarget struct {
	id string
	// plan is the store's own id for a run's task, set INSTEAD of id when the
	// card is raised from that task's page (taskplan.go's [app.taskPlanStop]). A
	// page speaks the store's ids and knows no row number, so its stop travels
	// the plan's own door ([session.Agent.PlanCancel]), which ends the run when
	// the task is the run's own.
	plan string
	// noun is what this work is called in the card's question — "run", "task"
	// or "job" — and detail is the promise under it. They travel together because
	// a question and the promise that answers it must not be able to disagree
	// about what is being ended.
	noun, detail string
}

func (t stopTarget) empty() bool { return t.id == "" && t.plan == "" }

// question is the card's whole first line.
func (t stopTarget) question() string {
	return "Stop this " + t.noun + "?"
}

// promise is what stopping this work does NOT take away, and it is the question's
// reason on the block ([app.stopShown]). It is a row of its own now rather than
// the tail of the question: the block draws the head and the reason on separate
// rows, and a sentence carrying both would say the promise twice.
func (t stopTarget) promise() string { return t.detail }

// The two nouns and the two promises. Each promise is what actually happens —
// not a reassurance — because the person reading it is deciding whether they
// can afford the ending.
const (
	stopRunNoun  = "run"
	stopTaskNoun = "task"
	stopJobNoun  = "job"
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
	// stopQuickDetail is what the same card says over a QUICK node (session's
	// TaskKindQuick). It has no worktree and no branch — it works in the folder
	// the person is already in — so `the branch it wrote on is kept` would send
	// somebody looking for a branch that was never cut. What is true instead is
	// that anything it has already written is where they are, and the answer it
	// was going to hand back is the half that is lost.
	stopQuickDetail = "Its work halts; whatever it already wrote is in your folder."
	// stopJobDetail is what the same card says over a background job. It has no
	// branch and wrote no files a merge would keep — what remains after a stop
	// is the log, which is the whole record of what the process did.
	stopJobDetail = "The process is ended; its log is kept."
)

// stopCard is one raised confirmation, and what is left of it is what it is
// ABOUT.
//
// THE BLOCK DRAWS IT AND THE BLOCK HOLDS THE CURSOR (question.go). Where the
// cursor is, which answer `enter` takes, which columns a press lands in and how
// the two rows are painted are the block's now — one renderer for every decision
// this program hands a person — and every law they carried is the same law said
// once instead of nine times.
type stopCard struct {
	target stopTarget
}

// stopActWord is ENDING THIS WORK, said once for every surface that offers it:
// the card's own first answer, and the verb the tasks place puts on its `→`
// strip (place_tasks.go's [tasksPlace.verbs]). Two spellings of one act is two
// things for a person to learn about one key.
const stopActWord = "stop it"

// stopAnswers are the two answers, in the order they are drawn: the act first
// because it is what the card is about, the refusal second because it is where
// the cursor starts.
var stopAnswers = [...]string{stopActWord, stopKeepWord}

// stopKeepWord is the answer that loses nothing, and it is marked
// [session.AnswerOption.Safe] on the question this file raises — which is what
// puts the cursor on it, what `esc` answers with, and what the row calls `esc`
// ([questionSafeAt], [questionLaterWord]). The law it carries is unchanged: a
// card whose destructive answer is under the enter key is a card that stops work
// when somebody presses enter for the reason people press enter, which is to
// make a question go away.
const stopKeepWord = "keep going"

// stopKeepAt is which of them is "no", and it is the cursor's home.
const stopKeepAt = 1

// stopQuestionKind is the lane this file's question travels under.
//
// IT IS NOT ONE OF internal/session's, and that is the point: nothing in the
// engine raises this question, nothing in the engine answers it, and it never
// goes through [session.Agent.ResolveQuestion] — it is answered by the closure
// the block carries for exactly this ([questionShown.local]). A surface question
// borrowing an engine lane's name would be a token two different things could
// collide on.
const stopQuestionKind session.QuestionKind = "surface-stop"

// stopping reports whether the card owns the keyboard.
func (a *app) stopping() bool { return a.stop != nil }

// raiseStop puts the question up over whatever the person was looking at. The
// page underneath stays exactly where it was: the question is about the work,
// not about the page, and answering either way leaves them where they were.
func (a *app) raiseStop(target stopTarget) {
	if target.empty() {
		return
	}
	if _, ok := a.stopDoors(); !ok && target.plan == "" {
		// A RUN'S OWN TASK TRAVELS THE PLAN'S DOOR AND NOT THIS ONE, and the page
		// that raised the card for it has already found that door
		// (taskplan.go's [app.taskPlanStop]).
		//
		// THE BUILD GUARD (room.go's, roomorch.go's): the door is an assertion
		// and not a compile-time requirement, so a surface driven by an agent
		// that cannot stop work says so and changes nothing.
		a.note(stopUnavailableWord)
		return
	}
	a.stop = &stopCard{target: target}
	// AND A JOB'S PAGE STEPS ASIDE FOR THE CARD. The block draws every question
	// above the message box (question.go, view.go's [app.chrome]) and a job's
	// page takes the frame WHOLE (jobpage.go) — so a card raised over it was a
	// question nobody could see, holding the keyboard, answered by whatever the
	// next keystroke happened to be. Closing puts the question where it is read,
	// and the engine's own sentence about what stopped lands in the conversation
	// directly under it ([app.stopSay]). The log is not going anywhere: the
	// job's row in the column opens the page again. Every OTHER place this card
	// is raised from — a room, a run's page — is drawn over the conversation
	// frame and keeps its chrome, so none of them needs this.
	if a.jobPageOpen() {
		a.closeJobPage()
	}
	// The typed lists follow the draft, and the draft is spoken for while a
	// question is up — the same law the approval question states (consent.go).
	a.closeLists()
	a.raiseQuestion(a.stopShown(target))
	a.touch()
}

// stopShown is the question the block puts up, and the closure that answers it.
//
// IT IS A CONFIRMATION, which is the shape on that block whose whole job is this
// card's law: the cursor starts on the answer that loses nothing, a key that
// NAMES an answer moves the cursor onto it rather than giving it, `enter` takes
// what the cursor is on, and `esc` is the safe answer and never the act. Nothing
// is decided by one keystroke.
//
// It BLOCKS NOTHING, and says so by carrying no Blocking at all: the person
// raised it with their own hand, no work is waiting behind it, and a clock on
// the end of its row would be a countdown to something nobody set.
func (a *app) stopShown(target stopTarget) questionShown {
	return questionShown{
		question: session.Question{
			Kind:    stopQuestionKind,
			Ref:     target.id,
			Ask:     session.AskConfirmation,
			Form:    session.FormCard,
			Asker:   session.Asker{Kind: session.AskerSurface},
			Head:    target.question(),
			Reason:  target.promise(),
			Options: stopOptions(),
			Stakes:  session.StakesIrreversible,
			Asked:   a.now(),
		},
		pick: stopKeepAt,
		local: func(answer session.Answer) tea.Cmd {
			return a.stopTake(stopAnswerAt(answer.FirstKey()))
		},
	}
}

// stopOptions is the two answers as the block takes them. The refusal wears the
// SAFE mark, which is the one place the cursor's home is decided.
func stopOptions() []session.AnswerOption {
	return []session.AnswerOption{
		{Key: "1", Label: stopAnswers[0]},
		{Key: "2", Label: stopAnswers[stopKeepAt], Safe: true},
	}
}

// stopAnswerAt reads one answer key back as its index in [stopAnswers]. An
// answer this card does not know is the refusal, which is the reading that
// cannot end work nobody meant to end.
func stopAnswerAt(key string) int {
	if key == "1" {
		return 0
	}
	return stopKeepAt
}

// stopUnavailableWord is the degraded case, in the vocabulary the other two
// unavailable doors on this surface use.
const stopUnavailableWord = "stopping work is unavailable — this session has no door onto it"

// dropStop takes the question down and changes nothing else.
func (a *app) dropStop() {
	if a.stop == nil {
		return
	}
	// AND THE QUESTION GOES WITH THE CARD. The block closes its own the moment
	// an answer is sent ([app.closeQuestion]), so this is for every other way the
	// card comes down — and a question left standing for a card that is gone
	// would be the chip counting a decision nobody can reach.
	a.dropStopQuestion()
	a.stop = nil
	a.touch()
}

// dropStopQuestion takes this file's question off the block if it is still
// there.
func (a *app) dropStopQuestion() {
	for _, open := range a.questions {
		if open.question.Kind != stopQuestionKind {
			continue
		}
		a.closeQuestion(open, session.Answer{})
		return
	}
}

// stopTake answers the card. Anything but the act is the card simply going
// away; the act asks the session and reports what it said.
func (a *app) stopTake(at int) tea.Cmd {
	card := a.stop
	if card == nil {
		return nil
	}
	target := card.target
	a.dropStop()
	if at != 0 {
		return nil
	}
	if target.plan != "" {
		// A RUN'S OWN TASK, STOPPED FROM ITS PAGE, goes through the plan's door
		// like every other verb that page has, off the loop and in the order it
		// was pressed ([app.taskPlanStopTaken]).
		return a.taskPlanStopTaken(target.plan)
	}
	doors, ok := a.stopDoors()
	if !ok {
		a.note(stopUnavailableWord)
		return nil
	}
	line, err := doors.Cancel(target.id)
	if err != nil {
		// The engine's own sentence, kept: a stop that could not be given is
		// work still running, and a surface that swallowed the reason would leave
		// a person pressing the same key again (roomorch.go's [app.orchAnswer]
		// keeps the same rule about the same kind of refusal).
		a.stopSay(err.Error())
		return nil
	}
	a.stopSay(line)
	return nil
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

// railFocusNode is the node the roster's cursor is standing on, or nil — while
// the roster does not hold the keyboard there is no cursor and nothing is being
// aimed at (taskstrip.go's [app.stripFocused] reads the same two fields).
func (a *app) railFocusNode() *taskNode {
	if !a.railHold {
		return nil
	}
	if a.railWhere.id == 0 {
		// A TASK'S ROW IN THE BAND IS THAT TASK, for every key the held column
		// answers (sidecol.go).
		return a.tasks[sideTaskOf(a.railWhere.key)]
	}
	return a.tasks[a.railWhere.id]
}

// stopVisibleCount is how many stoppable task rows the roster is drawing right
// now, and which one the only such row is. It is asked only on the path where
// the roster does NOT hold the keyboard ([app.stopHere]'s last resort), so the
// answer is a fact about what is on the frame, not about where a cursor is.
//
// IT COUNTS WHAT THE COLUMN ACTUALLY DRAWS and not what the session holds,
// because a row a fold is hiding is a row nobody can see and a keystroke aimed
// at an unseen row is the guess this whole path exists to avoid: [app.railView]
// is the one door onto the roster's geometry, the same door the pointer and the
// frame are answered through, so this and they cannot disagree about what is
// on screen. A group folded to its heading hides its rows from this count as
// it hides them from the eye.
//
// Counted here and not beside the caller so the one-row rule has one statement
// rather than two copies a second caller could get wrong.
func (a *app) stopVisible() (int, *taskNode) {
	entries := a.railEntries()
	var sole *taskNode
	count := 0
	for _, e := range entries {
		node := e.node
		if node == nil {
			continue
		}
		if a.stopTaskTarget(node).empty() {
			continue
		}
		if count++; count > 1 {
			return count, nil
		}
		sole = node
	}
	return count, sole
}

// stopHere is the work `x` and the header's ✕ are aimed at, or the empty target
// when there is nothing here to stop.
//
// THE PAGE YOU ARE STANDING ON OUTRANKS THE LIST YOU CAN SEE. A job's own page
// and a node's room are where you ARE; the roster is a list behind them. A
// person standing inside one who presses `x` means that work, whatever row the
// roster's cursor happens to be resting on.
func (a *app) stopHere() stopTarget {
	if run := a.orchOf(); run != nil {
		return stopRunTarget(run.id, run.snap, run.known)
	}
	if job := a.jobPageJob(); job != nil {
		return stopJobTarget(job)
	}
	if a.room != nil {
		// A PAGE READ THROUGH ANOTHER CONVERSATION STOPS NOTHING. The stop door is
		// this window's engine and the id is another conversation's, so `x` here
		// would end whatever this session calls task 7 — healthy work, on a page
		// that is not about it ([app.roomIsGuest]). The target is empty, which is
		// how every other unstoppable row on this surface is already handled: the
		// verb is not offered and the key does nothing.
		if a.roomIsGuest() {
			return stopTarget{}
		}
		// A RUN'S TASK IS STOPPED THROUGH THE PLAN'S DOOR (planroom.go).
		if a.room.plan != nil {
			return a.planRoomStopTarget()
		}
		return a.stopTaskTarget(a.tasks[a.room.id])
	}
	// A HELD ROSTER IS STILL THE FIRST ANSWER OFF IT, because its cursor is where
	// a person's eye is and the aim is already theirs. Below that, one visible
	// stoppable row is aimed at without asking for the keyboard first — see the
	// header's third law — and two or more are the case the focus requirement was
	// written for: nothing to aim at, so the key is the letter it is.
	if node := a.railFocusNode(); node != nil {
		return a.stopTaskTarget(node)
	}
	count, sole := a.stopVisible()
	if count == 1 {
		return a.stopTaskTarget(sole)
	}
	return stopTarget{}
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
			// A quick node has no branch either and never had one: it works in the
			// person's own folder (session's TaskKindQuick).
			detail: stopDetailFor(node.kind),
		}
	}
	return stopTarget{}
}

// stopJobTarget is one background job as a card can offer it. Only a job that
// is still running is offered: a settled job has nothing to stop, and a key
// that would refuse is a key the page does not draw (jobpage.go).
func stopJobTarget(job *session.JobNotice) stopTarget {
	if job == nil || job.Over() {
		return stopTarget{}
	}
	return stopTarget{
		id:     session.CancelJob + ":" + itoa(job.ID),
		noun:   stopJobNoun,
		detail: stopJobDetail,
	}
}

// stopDetailFor is the second line of the confirmation, chosen by what the node
// is rather than by what it is doing.
func stopDetailFor(kind session.TaskKind) string {
	switch kind {
	case session.TaskKindHarness:
		return stopDesignDetail
	case session.TaskKindQuick:
		return stopQuickDetail
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
	//
	// AND SO IS A RUN TASK'S ROOM THAT IS ON ITS WAY. A hosted conversation reads the page
	// off the loop, and between the press and the answer the keys already belong
	// to the page ([railPlanPending]); read here first, this letter would raise
	// the card over a page that is not drawn yet and take the rest of the note.
	switch {
	case key == "ctrl+c", a.asking(), a.awaitingTask(), a.guarding(),
		a.railPlanPending.id != "",
		a.at(pageSettings), a.at(pageTasks), a.at(pageHome), a.deckShowing(), a.pick.open,
		a.roster.open, a.copy.on, a.welcome.open, a.menu.open, a.comp.open,
		a.rew.on, a.rewSheet.open:
		return nil, false
	}
	if a.stopping() {
		// EVERY KEY THAT ANSWERS THIS CARD IS THE BLOCK'S (question.go): ←/→ walk
		// the two answers, a digit moves the cursor onto the answer it names,
		// `enter` takes what the cursor is on and `esc` is *keep going*. It is
		// offered the key HERE rather than at its own rung because this rung is
		// read first — a question about ending work outranks the pages it is
		// about — and what the block hands back is SWALLOWED rather than passed
		// on: the block is not modal, but this card is raised over a page whose
		// own keys would otherwise act underneath it.
		if cmd, took := a.questionKey(msg); took {
			return cmd, true
		}
		return nil, true
	}
	// A LETTER IS A LETTER THE MOMENT THERE IS A SENTENCE. See the header: the
	// box wins every time, and this key is no exception.
	if key != stopRaiseKey || a.chordsStandDown() || a.recalling() {
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

// ── the pointer ─────────────────────────────────────────────────────────────

// stopPress resolves a click aimed at stopping something and reports whether it
// took it. Two targets, in the order they are stacked on screen: the card's own
// answers while it is up, and the ✕ in the room's header.
func (a *app) stopPress(x, y int) bool {
	if a.copy.on || a.rew.on {
		return false
	}
	if a.stopping() {
		// THE CARD'S OWN ANSWERS ARE THE BLOCK'S TARGETS NOW (question.go's
		// [app.questionPress], which the frame offers a press to before this).
		// Reaching here with the card up means the press was aimed somewhere
		// else, and the ✕ that raised it must not raise a second one.
		return false
	}
	return a.stopMarkPress(x, y)
}

// stopMarkPress answers a press on the ✕ in a room's header.
//
// THE TOUCH TARGET IS THREE ROWS TALL AT THE PHONE TIER and one row everywhere
// else. A finger covers about three rows of a terminal, and this is the one
// control on the surface whose miss is expensive in both directions — hitting
// it by accident raises a card the person did not want, missing it leaves them
// with no way to stop the work at all. The header is the first row of the frame,
// so the box can only grow downward; it is claimed here, ahead of the strip and
// the body, and it occupies three columns at the far right where neither of them
// draws anything (taskstrip.go's chips are left-aligned).
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
	if !a.roomOpen() || a.stopping() || !a.roomStop.holds(x) {
		return false
	}
	width, _ := a.size()
	rows := 1
	if layoutTier(width) == tierPhone {
		rows = stopTouchRows
	}
	// THE BOX STARTS ON THE ROW THE WORD WAS DRAWN ON, which is the room's FACTS
	// row — under the tab strip and under the trail, wherever those are drawn
	// (chattabs.go's [app.roomFactsRow]). A box measured from the top of the frame
	// would sit two rows above the word the moment the strip stood up, which on a
	// phone is a finger ending work it was nowhere near.
	top := a.roomFactsRow()
	if top < 0 || y < top || y >= top+rows {
		// A frame too short for the facts row draws no `Stop` at all, and a box
		// measured from a row that is not there would put a three-row phone target
		// over the trail and the tabs (room.go's [app.roomHeadHeight]).
		return false
	}
	return !a.stopHere().empty()
}

// stopTouchRows is how tall the ✕'s hit box is where a finger is the pointer.
const stopTouchRows = 3

// THE CONTROL IS THE WORD `Stop` AND NOT A MARK, and that is the whole of what
// changed about it.
//
// It was a heavy multiplication ✕ riding the right end of the trail row. Two
// things went wrong with that. A tab now carries its own `×`, which DISMISSES A
// VIEW and leaves the work running (chattabs.go) — so one frame had two crosses
// three rows apart meaning opposite things, and the expensive one was the one
// that looked incidental. And a mark alone never said what it ended: a person
// who had not already learned it had to press it to find out, which is the one
// control on this surface where finding out by pressing is the wrong way round.
//
// So it is spelled, in the same word the card's own button offers and the same
// word the engine puts on the wire ([taskStoppedByPerson]), on the facts row
// where the rest of the page's telemetry is. One act, one name, and no glyph a
// person has to be taught.
const (
	roomStopMark      = "Stop"
	roomStopMarkASCII = "Stop"
)

// roomStopWord is the `Stop` as the facts row draws it, or "" when there is
// nothing here to stop. It is UNPAINTED, for [app.roomHeadWord]'s reason: the
// whole line is painted once, and a hue nested inside a hue ends at the inner
// one's reset.
func (a *app) roomStopWord() string {
	// The expanded page reserves its stop action below the tree.
	if a.roomPanelShowing(a.viewHeight()) {
		return ""
	}
	if !a.stopOffered() {
		return ""
	}
	return a.linearMark(roomStopMark, roomStopMarkASCII)
}

// ── what a stopped thing looks like afterwards ──────────────────────────────

// The mark work a PERSON ended wears is tokens.GStopped, and it is NOT the
// failure cross: a cross is a finding, and nobody found anything wrong with
// work that was stopped. It is the filled square every device a person owns
// stops with. The slot is the shared vocabulary's, drawn through
// tasktier.go's one door like every other state.

// taskStoppedByPerson is the word the header and the roster spell a stopped
// node with. It is the same word internal/session uses on the wire and the same
// word the card's button offers, because one act should not have three names.
const taskStoppedByPerson = "stopped"
