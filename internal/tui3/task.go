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
	// name is the two-or-three-word NAME cut from the title, sub the one-line
	// sentence under it, and ident the glyph and hue this node is followed by
	// for the rest of its life (taskident.go). All three are derived once, here,
	// because a card is read on every frame while its clock runs and a name
	// recomputed thirty times a second is a name computed twenty-nine times too
	// often.
	name, sub string
	ident     taskIdent
	dependsOn []uint64
	// model is the model this work will run on, and it is a FACT rather than a
	// question: the engine resolved it before anybody was asked (session's
	// taskmodel.go), and the card states it because a proposal that did not say
	// whose hands the work is going into would be hiding the one thing about it
	// nobody can find out afterwards.
	//
	// options is the exception: one word that fitted more than one model, which
	// the engine will not choose between. Then the card OFFERS them — model is
	// whichever is picked, and the leading one is what silence takes — and the
	// chosen id travels back on the answer.
	model   string
	options []string
	// pick is which of options has the keyboard, and modelRow/modelSpans are
	// where that row landed and what each chip occupies, for the click. They are
	// the choices row's own machinery ([app.taskCardRows]) for the same reason:
	// one layout, one set of targets.
	pick       int
	modelRow   int
	modelSpans []choiceSpan
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

	// forming says this card is a propose_task call that is STILL ARRIVING —
	// the block drawn from the first fragment of the call, before there is a
	// proposal to put in it ([app.formTask]).
	//
	// A FORMING CARD IS NOT A QUESTION. It has no id, no deadline, no options
	// and no answer lane: it is never [app.task], nothing on the keyboard is
	// pointed at it, and the frame it draws is furniture grey rather than the
	// question hue. It exists so the block GROWS where it will stand instead of
	// arriving whole under a reply somebody is in the middle of reading.
	forming bool
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
	id uint64
	// title is what the rail draws: the NAME, two or three words, derived once
	// from the engine's own title (taskident.go). label is that title whole,
	// kept because the cards have width for it and because a name is a cut of
	// something and the thing it was cut from is worth keeping.
	title, label string
	// assignment is the sentence the subtitle comes from — the proposal's
	// summary, or its brief when there was no summary — and brief/acceptance are
	// the node's contract with its runner, carried here so a card that lands ten
	// minutes later can still say what the work was FOR. All three arrive on the
	// proposal, not on the updates: an update carries a state, and the contract
	// is frozen at admission (internal/session's task_room.go says so).
	assignment, brief, acceptance string
	// ident is the glyph and the hue this node is followed by, keyed on the id
	// and stable for its whole life (taskident.go).
	ident taskIdent
	// model is the model this node runs on, as the engine published it
	// (session's TaskNotice.Model). Empty means nobody said — a scripted agent,
	// an older engine — and every row that draws it draws nothing instead, the
	// way the spend does.
	model string
	state session.TaskState
	// dependsOn is the structural half of this file (see the header): stored
	// always, drawn only when a prerequisite is unmet.
	dependsOn []uint64
	// began is the moment the node started, derived once from the update's own
	// Elapsed so the clock is the frame's and not the event's. met is when this
	// surface first heard of the node at all, which is the honest spawn time for
	// a node that never reached running.
	began, met time.Time
	// elapsed is the node's final age, as the update that ended it reported.
	elapsed time.Duration
	// cost is what this node's own agent has spent, as the engine last
	// published it (session's TaskNotice.CostUSD). Zero means nobody published a
	// price, which is not the same claim as "it cost nothing" — the focus header
	// draws no figure for it rather than a $0.00 (room.go).
	cost float64
	// liveCost is the same quantity as cost, counted the other way round: the
	// sum of the turn totals the pilot lane has heard (session.EventTurnDone's
	// Usage.CostUSD) while this node ran. It exists because the notice above
	// arrives ONLY at a state change (session's task_run.go announces from
	// setState and nowhere else), so between "running" and "done" the engine's
	// figure is a number from eleven minutes ago, and a bill that stands still
	// for eleven minutes is a bill nobody believes. Read it through [taskNode.spent].
	liveCost float64
	// tokens is what this node has burned, input plus output, summed over the
	// turns the pilot has seen. Zero means NOBODY COUNTED — no notice carries a
	// token figure, so a node this surface met after it landed has none and
	// never will — and it is not the claim that a node thought for free: a
	// surface draws nothing at all for it, the way it draws nothing for an
	// unpublished price (session's task_contract.go on CostUSD).
	tokens                int
	report, branch, merge string
	changed               []string
	// tool is what the node is doing RIGHT NOW, one line, and toolBegan when it
	// started doing it. They are written by the pilot lane below and are empty
	// between calls: a row that kept the last call's name would be claiming a
	// present that has passed.
	tool      string
	toolBegan time.Time
	// mending is the one plain line naming the gap the node is closing right
	// now, as the engine published it (session's TaskNotice.Mending), and it is
	// empty at every other moment of the node's life.
	//
	// THE MACHINERY IS NOT THE SURFACE'S TO MENTION. What puts a line here is a
	// private round the engine runs on work that came back short, and a person
	// has no use for the existence of that round — what they have a use for is
	// the SENTENCE: the node is still going, and this is what is left in it. So
	// the state stays running (it is running), and this is drawn as the news it
	// is rather than as a fourth state nobody asked for.
	mending string
	// waiting is why this node is not spending its time on the work, in the
	// engine's own word (session's TaskNotice.Waiting): what a queued node is
	// held behind, or the pacing a running node's calls are under. It is empty
	// at every moment the node is simply getting on with it.
	//
	// A HOLD IS NOT A STATE AND IT IS NOT A FAULT. Nothing has gone wrong with a
	// node that is waiting for a slot or being paced by its provider — it is
	// admitted, it is next, and it is going to run — so the state stays exactly
	// where the engine put it and this is drawn as the one thing a person cannot
	// otherwise tell: the difference between work that is stuck and work that is
	// merely waiting its turn. Like [taskNode.mending] it is a report of RIGHT
	// NOW, so it goes the moment the engine stops sending it.
	waiting string
	// froze is the clock this node's row is drawn against while somebody is
	// standing in its room, or zero. See [app.taskNow].
	froze time.Time
}

// taskLive is everything a node is saying about its PRESENT: the gap it is
// closing, and the hold it is under. They are the two fields the engine sends
// without moving the node's state (session's TaskNotice.Mending and .Waiting),
// which makes them the two the de-dup has to look at by hand, and they are one
// comparable value so that it asks ONE question about them rather than a clause
// per field — and so the third of them, when there is one, joins the law in a
// single place instead of three.
type taskLive struct{ mending, waiting string }

// taskLiveLines is what an update says about the node's present, trimmed.
func taskLiveLines(notice *session.TaskNotice) taskLive {
	return taskLive{
		mending: strings.TrimSpace(notice.Mending),
		waiting: strings.TrimSpace(notice.Waiting),
	}
}

// liveLines is what the node is already saying about its present.
func (n *taskNode) liveLines() taskLive { return taskLive{mending: n.mending, waiting: n.waiting} }

// spawnedAt is when this node's work started, in wall-clock: the moment it
// began running, or — for a node that failed before it ever ran — the moment
// this surface first met it. It is what the completion card's "spawned 14:02"
// says, and it is deliberately not the moment the proposal was made: a question
// asked at 13:58 and answered at 14:02 spawned at 14:02.
func (n *taskNode) spawnedAt() time.Time {
	if !n.began.IsZero() {
		return n.began
	}
	return n.met
}

// spent is what this node has cost, in dollars, from whichever of its two lanes
// knows the most — the engine's published figure, or the pilot's running sum.
// Zero means nobody has priced it, which is not "it was free", and a surface
// that draws this draws nothing rather than a $0.00 it made up.
//
// THE TWO FIGURES ARE THE SAME MONEY AND ARE NEVER ADDED. session's spend()
// answers a running node by asking its child agent for Usage().CostUSD — the
// cumulative meter — and a turn's Usage.CostUSD is one step of that same meter,
// so a surface that summed the notice and the turns would bill every turn twice.
// They are RECONCILED: the larger of the two is the more recent reading of one
// number, and taking the larger is also what makes this monotonic, because both
// halves only ever grow.
//
// ONCE THE NODE LANDS THE NOTICE WINS OUTRIGHT. The landing figure is frozen
// from the engine's own books (session's foldTaskUsage), and a surface holding a
// larger guess after the work is over would be disputing the bill rather than
// reporting it. The cost of that rule is the one case where this can read low: a
// node that was already running when this surface attached has spend the pilot
// never heard, so the row stays on the engine's older figure until the run's new
// turns overtake it and the landing corrects it. Reading low for a minute is the
// cheaper wrong — the alternative overstates what a person is being charged.
func (n *taskNode) spent() float64 {
	switch n.state {
	case session.TaskDone, session.TaskFailed, session.TaskUnverified:
		if n.cost > 0 {
			return n.cost
		}
	}
	if n.liveCost > n.cost {
		return n.liveCost
	}
	return n.cost
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

// The words a node NOBODY COULD JUDGE is drawn with (session's TaskUnverified).
//
// It is the third settled state and it is neither of the other two: the run is
// over, the branch is kept, and nothing came back that could call the work
// finished or call it wrong — so the surface must not spend "done" on it and
// must not spend "failed" on it either.
//
// THE MACHINERY IS NOT THE SURFACE'S TO MENTION. These words used to be the
// checking apparatus read out loud — "unverified", "auditor inconclusive" — and
// that is a person being handed this program's internal org chart in place of
// their answer. Nobody delegating a piece of work asked for a verdict; they
// asked for the work. So the state is spelled as the only thing about it that is
// a person's business: it FINISHED, and it is on them to look at it. The
// identifiers keep their old names because they name a state in the code, and
// the code is not the surface.
const (
	taskUnverifiedWord  = "needs your look"
	taskUnverifiedGloss = "finished, but needs your look"
	taskUnverifiedWaits = "finished — look it over"
	// taskBranchKept is [taskStoppedKept] without the stop: this node wears the
	// same "aborted" merge, and nothing about it stopped.
	taskBranchKept = "branch kept"
)

// taskFinishingWord is what a node says while it is closing a gap in work it has
// otherwise finished (session's TaskNotice.Mending, carried on [taskNode.mending]).
//
// IT IS NOT A STATE AND IT DOES NOT REPLACE ONE. The node is running — the
// engine says so on every one of these updates — and "finishing" is the surface
// saying WHICH PART of running this is, in the one word that is true of it from
// the outside: the work is nearly there and something is being tied off. What is
// being tied off is the sentence beside it, in the engine's own plain words.
const taskFinishingWord = "finishing"

// taskHeldWord is what a node says while it is HELD — admitted, next, and
// spending its time on something that is not the work (session's
// TaskNotice.Waiting, carried on [taskNode.waiting]).
//
// IT IS THE SAME SPLIT [taskFinishingWord] IS BUILT ON: the word is this
// surface's, saying which part of queued or running this is, and what follows
// the separator is the engine's own reason, verbatim. And it is the same law —
// the hold is not a state, nothing about the node moved, and a person is owed
// the difference between "this has been sitting there for four minutes doing
// nothing" and "this has been sitting there for four minutes because the
// machine is full".
//
// The identifier is "held" and the word is "waiting" because [taskWaitingWord]
// is already spent, on the meter of a proposal that is waiting on a person. Two
// different moments, one honest English word for both, and the card's had the
// name first.
const taskHeldWord = "waiting"

// The reasons the engine holds a node with (session's TaskNotice.Waiting),
// restated here for the same reason the merge words above are: the surface
// reads them out and internal/session exports them nowhere.
//
// THEY ARE READ OUT AS THEY STAND. Unlike "aborted", each of these three means
// on screen exactly what it means in the engine — a cap that is full, a machine
// under load, a provider pacing the calls — and all three are already the plain
// words a person would use for them. There is nothing to translate.
const (
	waitWordSlot    = "slot"
	waitWordMachine = "machine busy"
	waitWordRate    = "rate limited"
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
//
// AN UPDATE IS ALSO WHAT MAKES THE "@" LIST STALE. A node that has just started
// belongs under "running" and a node that has just landed belongs in the project
// index, and this is the one lane that knows either happened (taskmention.go).
func (a *app) taskEvent(ev session.Event) tea.Cmd {
	var pilot, mentions tea.Cmd
	switch ev.Kind {
	case session.EventTaskProposal:
		a.proposeTask(ev)
	case session.EventTaskUpdate:
		pilot = a.taskUpdate(ev)
		mentions = a.refreshTasks()
	}
	return tea.Batch(waitTask(a.taskLane, a.taskGen), pilot, a.wake(), mentions)
}

// ── the pilot lanes ─────────────────────────────────────────────────────────
//
// A RUNNING NODE IS WATCHED WHETHER OR NOT ANYBODY IS IN ITS ROOM.
//
// The standing subscription (above) carries a node's LIFE — queued, running,
// done — and it is silent for the whole of the interesting part: a node runs
// for eleven minutes and says nothing on that lane between minute zero and
// minute eleven. The rail's answer to "is this alive" was therefore a spinner
// and a clock counting the node's own age, which are both true of a node that
// has been stuck on one `go test` for four minutes and of one that is calling a
// tool every second.
//
// A pilot is the room's own door ([taskRoomAgent.WatchTask]) opened WITHOUT a
// room: one watcher per running node, kept for exactly as long as the node
// runs, folding three facts out of the stream and dropping everything else —
// what the node is doing, when it started doing it, and what the step it just
// finished cost. That is what the rail's elapsed clock is measured against (see
// [app.taskClock]) and what its tokens and its price are counted from (see
// [taskNode.spent]).
//
// THE SPEND IS HERE BECAUSE THE STANDING LANE IS SILENT. A node's updates carry
// a price and arrive only at a state change, so the rail's figure would be the
// one the node started with for the whole of the run; the child's own turns end
// several times a minute (session's task_run.go drives it through Submit after
// Submit), and each of those says what it has cost so far.
//
// IT COSTS A MESSAGE PER EVENT AND A REPAINT PER TOOL CALL, and the second half
// of that sentence is the design: a node's text deltas arrive here and are
// dropped without touching the frame, because the rail does not draw what a
// node SAYS. Only a call beginning or ending is news to a 24-column row.
type taskPilot struct {
	id   uint64
	gen  int
	lane <-chan session.Event
}

// flyPilot opens the watcher on one running node, or answers nil when there is
// nothing to watch it with — an agent with no room doors, a node already being
// watched, an id the engine does not know.
func (a *app) flyPilot(id uint64) tea.Cmd {
	doors, ok := a.roomDoors()
	if !ok || a.pilots[id] != nil {
		return nil
	}
	lane, err := doors.WatchTask(id)
	if err != nil {
		return nil
	}
	if a.pilots == nil {
		a.pilots = map[uint64]*taskPilot{}
	}
	a.pilotGen++
	pilot := &taskPilot{id: id, gen: a.pilotGen, lane: lane}
	a.pilots[id] = pilot
	return waitPilot(lane, pilot.gen, id)
}

// waitPilot takes one event off a pilot's lane and asks for the next.
func waitPilot(ch <-chan session.Event, gen int, id uint64) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return taskPilotClosedMsg{gen: gen, id: id}
		}
		return taskPilotMsg{gen: gen, id: id, ev: ev}
	}
}

// pilotEvent folds one watched node's event in and re-arms its lane.
//
// The generation is the same device every other lane on this surface uses: a
// pilot that was landed while its channel still had events in flight must not
// write the current tool of a node that has finished.
func (a *app) pilotEvent(msg taskPilotMsg) tea.Cmd {
	pilot := a.pilots[msg.id]
	if pilot == nil || pilot.gen != msg.gen {
		return nil
	}
	if node := a.tasks[msg.id]; node != nil {
		switch msg.ev.Kind {
		case session.EventToolAnnounced:
			// ASKED FOR, NOT STARTED — so the name changes and the clock does not
			// start. It is the tool row's own distinction (toolview.go's
			// toolQueued), and it matters here for the same reason: a clock that
			// started at the announcement would be measuring how long a response
			// took to stream.
			node.tool, node.toolBegan = taskCallWord(msg.ev.Tool, msg.ev.Args, msg.ev.Hint), time.Time{}
			a.touch()
		case session.EventToolBegin:
			node.tool, node.toolBegan = taskCallWord(msg.ev.Tool, msg.ev.Args, msg.ev.Hint), a.now()
			a.touch()
		case session.EventToolEnd, session.EventToolFailed:
			node.tool, node.toolBegan = "", time.Time{}
			a.touch()
		case session.EventTurnDone:
			// ONE STEP'S ACCOUNTING, ADDED TO THE NODE'S. The child is driven
			// through many Submits and this is the end of one of them, so the
			// figures are a turn's own and the running totals are the sum of them
			// ([taskNode.tokens], [taskNode.liveCost] — and read the reconciliation
			// law on [taskNode.spent] before adding a second lane to either).
			//
			// A TURN THAT PRICED NOTHING IS NOT NEWS. An unpriced model and a
			// provider that returned no usage both land here as zeroes, and adding
			// zero to a total is a repaint of a frame that has not changed — which
			// this lane, one message per event, cannot afford to spend.
			used := msg.ev.Usage
			if used.Input+used.Output > 0 || used.CostUSD > 0 {
				node.tokens += used.Input + used.Output
				node.liveCost += used.CostUSD
				a.touch()
			}
		}
	}
	return tea.Batch(waitPilot(pilot.lane, pilot.gen, pilot.id), a.wake())
}

// taskCallWord is one call in the ONE LINE a 24-cell rail row has for it: the
// tool's name with the verb said once, and the payload's own target ahead of the
// session's gloss.
//
// It is composed from the same two renderers the conversation's tool line is
// built from (toolview.go's [toolWords], toolstat.go's [toolTarget]) rather than
// being a rendering of its own — which is the law room.go states in its header
// after giving up a one-line renderer of exactly this shape. The rail cannot use
// the tool LINE itself: that block is two cells of rail, a right-aligned stat
// and an expansion, and this column has room for none of them.
func taskCallWord(tool, args, hint string) string {
	name, _ := toolWords(tool, hint)
	if target := toolTarget(tool, args, hint); target != "" {
		return name + " " + target
	}
	return name
}

// landPilot forgets one node's watcher. The lane closes itself when the node
// reaches its final state; this is the surface's half, and it is called from
// the update that says so rather than waiting for the close, so the row stops
// claiming a current call the instant the work is over.
func (a *app) landPilot(id uint64) {
	if node := a.tasks[id]; node != nil {
		node.tool, node.toolBegan = "", time.Time{}
	}
	delete(a.pilots, id)
}

// ── the proposal ────────────────────────────────────────────────────────────

// proposeTask draws the decision moment (session.EventTaskProposal).
//
// The card lands in the transcript and takes the keyboard's answer lane. One
// question at a time is the engine's own serialization (its ask blocks the tool
// call that raised it, exactly as consent's does), and a second card arriving
// anyway is not dropped: the older one settles as expired, because a question
// that can no longer be answered must stop looking like one.
// taskTool is the call a proposal comes out of (session's task.go). The name is
// written down once here because two different events are recognized by it: the
// forming fragments this file opens a card on, and nothing else on this surface
// may guess at the spelling.
const taskTool = "propose_task"

// formTask opens the SPAWN CARD the moment a propose_task call starts arriving
// (session.EventToolForming), and fills its title in as the wire says it.
//
// THE DEFECT THIS CLOSES: a groomed proposal is the longest call the model
// makes — a title, a summary, a brief of several paragraphs, an acceptance
// condition — and every one of those seconds used to be silence, ending in a
// question-hued block appearing whole under whatever the person was reading.
// The card now grows where it will stand: dim, titled as soon as the title
// field closes, with "forming…" where the countdown will be.
//
// It is one card per call and it is never [app.task]: this is not a question
// yet, and a block that took the answer lane before there was anything to
// answer would be a keyboard pointed at nothing.
func (a *app) formTask(ev session.Event) {
	card := a.formingCard()
	if card == nil {
		card = &taskCard{forming: true, born: a.now(), choiceRow: -1, modelRow: -1}
		a.closeLive()
		a.entries = append(a.entries, entry{kind: entryTask, turn: a.turn, card: card})
		a.follow()
	}
	// The gloss is "propose_task <title>" once the title field has CLOSED
	// (session's toolhint.go), and empty until then — so the name lands once,
	// whole, and the card never shows half a title.
	if _, title := toolWords(ev.Tool, ev.Hint); title != "" && title != card.title {
		card.title = title
		card.name = taskTitleOf(title, "", 0)
		a.markCardStale(card)
	}
	a.touch()
}

// formingCard is the card a propose_task call is currently forming into, or
// nil. It walks newest first: a forming card is by construction the most recent
// one on the transcript, and every settled card behind it is somebody else's.
func (a *app) formingCard() *taskCard {
	for i := len(a.entries) - 1; i >= 0; i-- {
		e := &a.entries[i]
		if e.kind != entryTask || e.card == nil {
			continue
		}
		if e.card.forming && !e.card.settled() {
			return e.card
		}
	}
	return nil
}

// dropFormingCard resolves a card whose call never arrived — the turn ended, or
// the stream died, between two fragments of a propose_task.
//
// It SETTLES rather than disappears, for [app.dropForming]'s reason: the model
// began asking for work and did not finish, which is a thing that happened, and
// the two-row settled card is exactly the shape this surface keeps such facts
// in.
func (a *app) dropFormingCard() {
	for i := range a.entries {
		e := &a.entries[i]
		if e.kind == entryTask && e.card != nil && e.card.forming && !e.card.settled() {
			e.card.verdict = taskFormingLost
			e.stale = true
		}
	}
}

func (a *app) proposeTask(ev session.Event) {
	notice := ev.Task
	if notice == nil {
		return
	}
	if a.task != nil && !a.task.settled() {
		a.task.verdict = taskExpiredWord
	}
	title := strings.TrimSpace(notice.Title)
	summary := strings.TrimSpace(notice.Summary)
	brief := strings.TrimSpace(notice.Brief)
	name := taskTitleOf(title, firstNonEmpty(summary, brief), notice.ID)
	card := &taskCard{
		id:         notice.ID,
		title:      title,
		summary:    summary,
		brief:      brief,
		acceptance: strings.TrimSpace(notice.Acceptance),
		name:       name,
		sub:        taskSubtitleOf(name, firstNonEmpty(summary, brief)),
		ident:      identFor(notice.ID),
		dependsOn:  notice.DependsOn,
		model:      strings.TrimSpace(notice.Model),
		options:    notice.ModelOptions,
		deadline:   notice.Deadline,
		born:       a.now(),
		// THE CARD OPENS ON "YES", because that is what the block is proposing and
		// a cursor parked on the destructive answer is a cursor that makes the
		// safe answer the one you have to aim at. The clock behind it says the
		// same thing: silence is approval.
		choice:    choiceYes,
		choiceRow: -1,
		// And on the CLOSEST model, which is the one the engine put first and the
		// one the countdown will settle on. The models row is a correction, not a
		// decision the work is waiting behind.
		pick:     0,
		modelRow: -1,
	}
	a.task = card
	a.closeLive()
	// The typed lists follow the draft, and the draft is now the redirect lane:
	// a completion list left open under it would be answering keys that belong
	// to the question (consent.go makes the same call for the same reason).
	a.closeLists()
	// THE BLOCK THE CALL WAS FORMING INTO BECOMES THIS ONE. The person has been
	// watching this proposal arrive; the question is that block's next state,
	// not a second copy of it underneath ([app.formTask]).
	if at := a.formingCardAt(); at >= 0 {
		a.entries[at].card = card
		a.entries[at].turn = a.turn
		a.entries[at].stale = true
		a.follow()
		a.touch()
		return
	}
	a.entries = append(a.entries, entry{kind: entryTask, turn: a.turn, card: card})
	a.follow()
	a.touch()
}

// formingCardAt is [app.formingCard]'s index, for the one caller that has to
// replace the card rather than read it.
func (a *app) formingCardAt() int {
	for i := len(a.entries) - 1; i >= 0; i-- {
		e := &a.entries[i]
		if e.kind != entryTask || e.card == nil {
			continue
		}
		if e.card.forming && !e.card.settled() {
			return i
		}
	}
	return -1
}

// The words a settled card keeps. They are sentences rather than states because
// the row is read once, later, by somebody reconstructing what happened.
const (
	taskApprovedWord = "approved"
	taskRedirectWord = "approved · you redirected it"
	taskDeclinedWord = "declined"
	taskClockWord    = "approved · the clock"
	taskExpiredWord  = "expired · the turn ended"
	taskRedirectLane = "redirect this task… (enter sends it, esc declines)"
	taskProposalHint = "y yes · r redirect · n no"
	taskExpandHint   = "ctrl+e for the brief"
	// taskModelTag labels the one fact a proposal carries that nobody can find
	// out afterwards: whose hands the work is going into.
	taskModelTag      = "model "
	taskAcceptanceTag = "done when: "
	// taskWaitingWord is what stands where the meter would be on a proposal the
	// engine is holding open indefinitely. A bar with no end to drain toward
	// would be an animation inventing a deadline nobody set.
	taskWaitingWord = "waiting on you"
	taskAutoWord    = "auto-starts in "
	// taskFormingWord stands where the meter will be while the call that fills
	// this card is still arriving. It is a state and not a promise: there is no
	// deadline yet, because the engine has not been handed anything to hold.
	taskFormingWord = "forming…"
	// taskFormingName is what the head says before the title field has closed.
	// A block with no word in it reads as a rendering fault; this reads as the
	// one thing that is known — something is being proposed.
	taskFormingName = "task"
	// taskFormingLost is what a card keeps when the call never finished
	// arriving: the turn ended, or the stream died, mid-proposal.
	taskFormingLost = "cancelled · the proposal never arrived"
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
	// THE DIGITS BELONG TO THE MODELS ROW, on the one card that has one, and they
	// are taken in the same tier as the three letters and under the same guard:
	// a person writing "3 files should change" is writing, not choosing.
	if at, ok := taskModelKey(msg.String()); ok && at < len(card.options) {
		a.takeModel(at)
		return nil, true
	}
	return nil, false
}

// taskModelKey reads a digit as one of the models on offer, zero-indexed. Only
// the four the shortlist can hold are keys; anything else is not this row's.
func taskModelKey(key string) (int, bool) {
	switch key {
	case "1":
		return 0, true
	case "2":
		return 1, true
	case "3":
		return 2, true
	case "4":
		return 3, true
	}
	return 0, false
}

// takeModel moves the choice of model. It ANSWERS NOTHING: the question is
// still whether the work goes at all, and picking the model it goes on is a
// correction to the proposal rather than a verdict on it.
func (a *app) takeModel(at int) {
	card := a.task
	if card == nil || card.settled() || at < 0 || at >= len(card.options) {
		return
	}
	card.pick = at
	card.model = card.options[at]
	a.markCardStale(card)
	a.touch()
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
	// A CARD THAT ASKED WHICH MODEL KEEPS THE ANSWER. The block collapses to its
	// verdict line, and on this one card that line is the only place the choice
	// the person just made is written down — everywhere else states the model the
	// work RAN on, which is the same fact only until somebody wonders whether it
	// was the one they picked.
	if approve && len(card.options) > 1 && card.model != "" {
		card.verdict += " · " + card.model
	}
	card.typing = false
	if agent, ok := a.tasker(); ok {
		// The model travels with the answer only when there was a choice to make:
		// on every ordinary proposal the engine already resolved it, and a surface
		// naming it back would be answering a question nobody asked (session's
		// TaskAnswer says the same in its own words).
		chosen := ""
		if len(card.options) > 1 {
			chosen = card.model
		}
		agent.ResolveTask(card.id, session.TaskAnswer{Approved: approve, Redirect: redirect, Model: chosen})
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

// openCard is ctrl+o on a SELECTED proposal, and it reports whether it took the
// key.
//
// THE BRIEF KEPT A KEY WHEN IT LOST THE CLICK. A press on a spawn card opens the
// node's room now (app.go's [app.press]) — the question a person has about
// running work is what it is doing — and the brief is still one fold away, on
// the key this surface already means "show me the rest of this" by. It sits
// beside the landed card's own ctrl+o (taskdone.go's [app.openDone]): same key,
// same gesture, the same object one state apart.
func (a *app) openCard(i int) bool {
	if i < 0 || i >= len(a.entries) || a.entries[i].kind != entryTask || a.entries[i].card == nil {
		return false
	}
	a.toggleCardAt(i)
	return true
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
//	╭─ ? ◆ Fix nil-map crash ───────────────────────────────────────────────
//	│ The parser drops a key on an empty map.
//	│ [ yes ]  [ redirect ]  [ no ]
//	│ ███████████████░░░░░  auto-starts in 3.2s
//	│ ctrl+e for the brief
//	╰──────────────────────────────────────────────────────────────────────
//
// COLLAPSED IS A NAME AND A SENTENCE, and that is the card law this surface now
// applies to all three of a task's cards — the proposal, the rail's presence and
// the card that lands when it finishes (taskdone.go). The head carries the
// two-or-three-word name and the node's own glyph; the line under it is the
// first sentence of the assignment and nothing more.
//
// It used to be three lines of summary, which is what a card looks like when
// nobody has decided what a card is: the third line of a summary is being read
// by somebody who has already decided, and the person who has not decided is
// reading the first. Everything the three lines held is one keystroke away and
// is now MORE than they held — opened, the card is the whole assignment, the
// brief, the done-condition and the context the work will run in.
//
// SETTLED, THE BLOCK COLLAPSES to its head and one verdict line — what was
// chosen and what that came to — because a question that has been answered is a
// fact, and a fact does not need a frame around it.
//
// The blank row that follows the block is [app.layout]'s (render.go): spacing is
// emitted in exactly one place on this surface, and a block that left its own
// gap would be the second.
func (a *app) taskCardRows(card *taskCard, width int, sel bool) []string {
	if card == nil || width < 4 {
		return nil
	}
	// The hit targets are rebuilt with the rows that carry them, and cleared
	// first: a settled card has no options, and a stale span is a click that
	// answers a question nobody is asking.
	card.choiceRow, card.spans = -1, nil
	card.modelRow, card.modelSpans = -1, nil
	// STILL ARRIVING: three rows, nothing to answer, and no hit targets — which
	// the two lines above have just made true for this frame.
	if card.forming && !card.settled() {
		return a.taskFormingRows(card, width, sel)
	}
	head := a.taskHead(card, width, sel)
	if card.settled() {
		return []string{head, a.taskFoot(card, width)}
	}
	stem := a.pal.ask(a.blockStem())
	room := width - ansi.StringWidth(a.blockStem())
	out := []string{head}
	if line := card.sub; line != "" {
		out = append(out, stem+a.pal.ink(fit(line, room)))
	}
	if card.open {
		// OPENED, THE CARD IS THE WHOLE CONTEXT. The summary goes first because it
		// is the sentence the collapsed row was a cut of, then the brief — which is
		// the node's entire contract with its runner and the thing a person is
		// actually auditing when they open a proposal at all.
		for _, line := range wrap(card.summary, room) {
			out = append(out, stem+a.pal.dim(line))
		}
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
	// THE MODELS ROW ONLY EXISTS WHEN THERE IS A CHOICE. One word, one model is
	// every ordinary proposal, and that model is said on the meta line below —
	// where it costs no row at all.
	if len(card.options) > 1 {
		models, spans := a.taskModels(card, ansi.StringWidth(a.blockStem()), room)
		if models != "" {
			card.modelRow, card.modelSpans = len(out), spans
			out = append(out, stem+models)
		}
	}
	choices, spans := a.taskChoices(card, ansi.StringWidth(a.blockStem()), room)
	card.choiceRow, card.spans = len(out), spans
	out = append(out, stem+choices)
	out = append(out, stem+a.taskMeter(card, room))
	if meta := a.taskMetaWord(card, room); meta != "" {
		out = append(out, stem+a.pal.dim(meta))
	}
	return append(out, a.taskFoot(card, width))
}

// The three-line summary cap that stood here is gone with the three lines: the
// collapsed card is a name and ONE sentence (taskident.go's [taskSubtitleOf]
// cuts it, and caps it at ninety cells), and the whole summary is drawn inside
// the expansion where nothing needs capping.

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
// A FORMING CARD IS GREY FOR THE SAME REASON A SETTLED ONE IS: violet means a
// person is being asked something, and a call that is still arriving has not
// asked yet. The frame taking the question hue is what the proposal landing
// LOOKS like — the block the person watched grow turns into a question.
func (a *app) blockPaint(card *taskCard) func(string) string {
	if card.settled() || card.forming {
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
func (a *app) taskHead(card *taskCard, width int, sel bool) string {
	paint, rule := a.blockPaint(card), a.blockRule()
	corner := taskHeadCorner
	if a.pal.ascii {
		corner = taskCornerASCII
	}
	// The "?" is the consent block's own glyph, and it is the same glyph for the
	// same reason it is the same hue: this is that moment, about a different
	// kind of thing. It is already its own ASCII, so the linear tier needs no
	// stand-in for it.
	//
	// THE NODE'S OWN GLYPH RIDES BESIDE IT, unpainted by the frame's hue and
	// painted by the ring instead (taskident.go). The two are different claims
	// and both are true of this row: somebody is being asked something, and the
	// thing being asked about is THAT one — the same mark that will be on the
	// rail in four seconds and on the card that lands in eleven minutes.
	head := corner + " " + glyphAsk + " "
	mark := a.taskMarkSel(card.ident, sel) + " "
	title := fit(card.name, width-ansi.StringWidth(head)-3)
	line := paint(head) + mark
	if card.settled() {
		line += a.pal.muted(title)
	} else {
		line += a.pal.askBold(title)
	}
	if fill := width - ansi.StringWidth(head) - ansi.StringWidth(title) - 3; fill > 0 {
		line += paint(" " + strings.Repeat(rule, fill))
	}
	return line
}

// taskFormingRows is the card while the call that will fill it is still
// arriving: the head with whatever the title has said so far, one row saying
// what state this is, and the foot.
//
//	╭─ ◌ Port the streaming ────────────────
//	│ forming…
//	╰───────────────────────────────────────
//
// The shape is the proposal's own, minus everything that would be a lie: no
// summary (nothing has closed), no models row (the engine has not resolved
// one), no choices (there is nothing to answer) and no meter (there is no
// deadline — the clock starts when the engine takes the proposal, not when the
// model starts writing it). What stands in the meter's place is the word for
// exactly what is happening.
func (a *app) taskFormingRows(card *taskCard, width int, sel bool) []string {
	stem := a.pal.dim(a.blockStem())
	room := width - ansi.StringWidth(a.blockStem())
	return []string{
		a.taskFormingHead(card, width, sel),
		stem + a.pal.dim(fit(taskFormingWord, room)),
		a.taskFoot(card, width),
	}
}

// taskFormingHead is [app.taskHead] for a block that is not yet a question.
//
// Two cells differ and both of them are the same decision. There is no "?",
// because nobody is being asked anything; and the node's own glyph is not there
// either, because the ident is derived from an id the engine has not minted —
// so the cell holds the forming mark instead, the same pulsing ◌ the tool row
// carries (toolview.go), which is the honest statement that this is arriving.
func (a *app) taskFormingHead(card *taskCard, width int, sel bool) string {
	rule := a.blockRule()
	corner := taskHeadCorner
	if a.pal.ascii {
		corner = taskCornerASCII
	}
	head := corner + " "
	mark := a.formingInk(a.linearMark(glyphQueued, glyphQueuedASCII)) + " "
	title := fit(firstNonEmpty(card.name, taskFormingName), width-ansi.StringWidth(head)-3)
	line := a.pal.dim(head) + mark
	if sel {
		line += a.pal.bold(a.pal.muted(title))
	} else {
		line += a.pal.muted(title)
	}
	if fill := width - ansi.StringWidth(head) - ansi.StringWidth(title) - 3; fill > 0 {
		line += a.pal.dim(" " + strings.Repeat(rule, fill))
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

// taskMetaWord is the card's dim last line: WHO the work goes to, and the key
// that opens what it is.
//
// The model leads because it is a fact about this proposal that nothing else on
// screen will ever say again — the rail has 24 columns and the landed card is
// twenty minutes away — where the expand hint is a key that is learned once and
// then never read. A narrow frame cuts the hint and keeps the model.
func (a *app) taskMetaWord(card *taskCard, width int) string {
	var parts []string
	if card.model != "" {
		parts = append(parts, taskModelTag+card.model)
	}
	if !card.open && card.brief != "" {
		parts = append(parts, taskExpandHint)
	}
	if len(parts) == 0 {
		return ""
	}
	return fit(strings.Join(parts, " · "), width)
}

// taskModels draws the row of models this work could run on, and reports what
// each one occupies so a click can be resolved to the model under it.
//
// IT IS A CORRECTION, NOT A GATE. The card arrives with the closest match
// already picked and the countdown already running, because the alternative is
// work that stops for a question the person did not ask — one word fitting two
// models is the harness's ambiguity, not theirs. What the row buys is the
// thirty seconds in which the choice is free to change.
//
// An option that does not fit is DROPPED rather than truncated, which is the
// rule [app.taskChoices] follows for the same reason: half a model id is a model
// somebody picks by mistake.
func (a *app) taskModels(card *taskCard, left, width int) (string, []choiceSpan) {
	words := taskModelWords(card.options)
	var line string
	var spans []choiceSpan
	at, end := left, left+width
	for i, word := range words {
		chip := "[ " + itoa(i+1) + " " + word + " ]"
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
		line += a.taskModelChip(itoa(i+1), word, i == card.pick)
		spans = append(spans, choiceSpan{from: at, to: at + ansi.StringWidth(chip), at: i})
		at += ansi.StringWidth(chip)
	}
	if len(spans) < 2 {
		// One chip is not a choice, and a row that offers one option is a row that
		// asks a question it has already answered.
		return "", nil
	}
	return line, spans
}

// taskModelChip is one model. The picked one takes the question hue and the
// weight together, and the others spend the weight on the DIGIT that picks
// them — the choices row's own trick, with a number where the initial would be:
// two model ids from one vendor share every letter that could have been a key.
func (a *app) taskModelChip(key, word string, focus bool) string {
	if focus {
		return a.pal.askBold("[ " + key + " " + word + " ]")
	}
	return a.pal.dim("[ ") + a.pal.askBold(key) + a.pal.ask(" "+word+" ]")
}

// taskModelWords is how the options are SPELLED on the row: the part after the
// vendor, which is the part that differs, unless two vendors carry the same one
// — in which case the vendor is the whole distinction and every chip keeps its
// full id. The meta line under the row always names the picked model in full.
func taskModelWords(options []string) []string {
	tails := make([]string, 0, len(options))
	seen := map[string]bool{}
	for _, option := range options {
		tail := option
		if slash := strings.LastIndex(option, "/"); slash >= 0 {
			tail = option[slash+1:]
		}
		if tail == "" || seen[strings.ToLower(tail)] {
			return append([]string(nil), options...)
		}
		seen[strings.ToLower(tail)] = true
		tails = append(tails, tail)
	}
	return tails
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
//   - ATTENTION FIRST, AND ATTENTION MEANS A DEMAND. Five groups, in the order a
//     person needs them — what is waiting on a decision of theirs, what is
//     running, what is waiting for a slot, what is parked behind other work,
//     what is over — and newest first inside each, because the node you just
//     started is the node you are watching. This is the one law the old rail
//     stated and this file now overturns ("a list that reordered itself would
//     move the row a person is watching"): at three rows admission order IS the
//     shape, and at three hundred it is a haystack. The order is stable in the
//     way that matters — a row moves when its STATE moves, which is the one
//     event a person is watching for anyway.
//
//     The leading group holds ONLY the work that will not move without a person:
//     a landing nobody could judge, and work that finished on a branch that
//     never came home. A plain failure is not one of those — the engine has
//     already spent its repair rounds on it by the time it lands — so it is
//     settled news and it goes in the fold with the rest of the record. That
//     argument is made in full at [app.railGroupOf], and it is the reason this
//     group can be the one group that never folds.
//   - THE TAIL IS FOLDED, AND THE INCOMPLETE WORK IS AT THE FRONT OF IT.
//     `parked` and `done` open closed, one heading each with its population on
//     it: a hundred and forty-eight settled nodes are a fact, not a hundred and
//     forty-eight rows. Inside `done`, 8.1.7's finalized-list clause holds — the
//     nodes that did not come off claim the slots first ([railFinalOrder]) —
//     because a fold a person opens is a fold they are searching.
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
	// railAttention is work that is waiting on a PERSON: nobody could say whether
	// it holds, or it finished and its branch never came home. Both are the same
	// sentence — this is not going anywhere until you look at it — and they lead
	// the column because everything below them is a thing that is still moving by
	// itself.
	//
	// A GROUP THAT NEVER FOLDS MUST NEVER ACCUMULATE THE UN-ACTIONABLE. This is
	// the one group with no fold and the top of the column, which is a standing
	// charge on the most valuable rows on the surface, and the only thing that
	// pays for it is that every row here is a DEMAND: something a person has to
	// do, that nothing else is going to do. A row that is merely bad news buys
	// nothing with that position — it pushes down the rows that are demands, and
	// it never leaves, so a day's work ends with the "needs you" heading standing
	// over forty things nobody needs to do. See [app.railGroupOf] for what that
	// costs a failure, and why a failure stopped paying it.
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
//
// A FAILURE IS SETTLED NEWS AND NOT A STANDING DEMAND, and this is the law that
// changed. "needs you" used to hold every failed node forever, and the reason it
// did was that a failure USED TO BE the moment a person was called in: work came
// back short and the only thing that could happen next was somebody looking at
// it. That is no longer where the decision is. The engine exhausts its repair
// rounds BEFORE a node is allowed to land failed (session's task_audit.go and
// the mending line it publishes while it runs), so by the time this surface sees
// the word the question "can this be salvaged automatically" has already been
// asked and answered. What is left is a report: this piece of work did not come
// off. That is worth keeping — it is why the roster keeps everything — and it is
// not worth the top of the column and a group that never folds.
//
// SO THE TEST IS "IS THERE SOMETHING TO DO", NOT "DID IT GO WRONG". Three
// outcomes pass it and nothing else does: work nobody could judge, which moves
// only when a person decides (session's ResolveUnverified); a branch that
// conflicted; and a run that stopped with its branch kept. The last two are the
// same fact — FINISHED WORK THAT IS NOT DELIVERED, sitting on a branch that
// nobody but a person is going to bring home — and they are read off the merge
// word rather than off the state, because a failed node and a done node can each
// wear either one. A failure with NO kept branch left nothing behind to deliver,
// so it is news, and news lives in the fold with the rest of the record —
// at the FRONT of that fold, where 8.1.7 puts the work that did not come off
// ([railFinalOrder]).
func (a *app) railGroupOf(node *taskNode) railGroup {
	switch node.state {
	case session.TaskRunning:
		return railRunning
	case session.TaskUnverified:
		// ATTENTION, AND IT IS THE PLAINEST CASE OF IT ON THIS COLUMN. An
		// unverified node is settled work that nobody can call finished, and the
		// only thing that moves it is a person deciding. It is named here rather
		// than left to the merge test below, which would file a node whose branch
		// went nowhere under "done".
		return railAttention
	case session.TaskQueued:
		if a.railWaits(node) != "" {
			return railParked
		}
		return railIdle
	}
	if taskUndelivered(node) {
		return railAttention
	}
	return railDone
}

// taskUndelivered reports whether this node's work is FINISHED AND NOT DELIVERED:
// it lives on a branch that never came home, and nothing but a person is going
// to bring it home.
//
// THE BRANCH IS THE WHOLE OF THE CLAIM. "conflicted" and "aborted" are the two
// merge words session writes when it keeps a branch (task_run.go's comeHome and
// abortedMerge), and a node wearing one of them WITH a branch name has real work
// sitting somewhere a person can go and get. A node that ran in the person's own
// tree, or one that ended before there was ever a branch, wears no name here and
// has left nothing behind — so it is not undelivered, it is simply over.
func taskUndelivered(node *taskNode) bool {
	switch node.merge {
	case mergeWordConflicted, mergeWordAborted:
		return strings.TrimSpace(node.branch) != ""
	}
	return false
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
// each group — except in the fold, where 8.1.7 has the first word.
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
	out[railDone] = railFinalOrder(out[railDone])
	return out
}

// railFinalOrder is 8.1.7's second clause, applied to the one group this surface
// has that is FINALIZED: incomplete work claims the slots first, and everything
// that merely finished follows it in the order it arrived.
//
// A FINALIZED LIST IS READ TO FIND OUT WHAT WENT WRONG. That is the whole of the
// law's reasoning and it is exactly why a failure could stop standing at the top
// of the column: it did not need the top of the column to stay visible, it
// needed to be the first thing behind the heading it went into. A fold opened on
// a hundred and forty landed nodes shows its first handful of rows, and the
// person opening it is looking for the one that did not come off — so that is
// the one the first rows are spent on, ahead of a hundred clean merges that have
// nothing left to say.
//
// IT IS A PARTITION AND NOT A SORT. Newest-first survives inside each half,
// because the order the roster is built on is arrival order and a group that
// re-sorted itself by anything else would move a row a person is watching for a
// reason they cannot see (the roster section's own law).
func railFinalOrder(nodes []*taskNode) []*taskNode {
	incomplete := 0
	for _, node := range nodes {
		if node.state == session.TaskFailed {
			incomplete++
		}
	}
	if incomplete == 0 || incomplete == len(nodes) {
		return nodes
	}
	out := make([]*taskNode, 0, len(nodes))
	for _, node := range nodes {
		if node.state == session.TaskFailed {
			out = append(out, node)
		}
	}
	for _, node := range nodes {
		if node.state != session.TaskFailed {
			out = append(out, node)
		}
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

// railAvail reports whether there is a roster to raise at all, at ANY width.
//
// It is the width-free half of [app.railShowing], and the two are different
// questions now: what the frame lends the roster is a question about columns,
// and whether the session has any work to show is not. ctrl+t asks this one.
func (a *app) railAvail() bool { return len(a.taskOrder) > 0 }

// railFull reports whether the roster is drawn OVER the body rather than beside
// it — the narrow frame's answer to the same key.
//
// THE COLUMN IS THE FIRST THING A NARROW FRAME GIVES UP and that left the work
// with no door on it at all: under [railSlimFloor] there was no rail, so there
// was no way into a running node except a card that had scrolled away. Squeezing
// the column further was never the fix — a roster at twelve columns is a list of
// first words — so under the breakpoint the same roster opens over the frame
// instead: the same entries, the same folds, the same footer, the same keys, at
// the width it actually has.
//
// It is the SAME STATE as the column's focus ([app.railHold]) and not a second
// flag, because it is the same act: ctrl+t asks for the roster, and what the
// frame does with the request is a question about its width. One state cannot
// disagree with itself about whether the roster is up.
func (a *app) railFull() bool {
	if !a.railHold || !a.railAvail() {
		return false
	}
	width, _ := a.size()
	return railColsFor(width) == 0
}

// railStanding reports whether the roster is on the frame in either shape.
func (a *app) railStanding() bool { return a.railShowing() || a.railFull() }

// railRoom is the columns the roster's TEXT gets, seam excluded: its column's
// width where it has one, the whole frame where it is drawn over the body.
func (a *app) railRoom() int {
	width, _ := a.size()
	if a.railFull() {
		return width - ansi.StringWidth(railSeam)
	}
	return railColsFor(width) - ansi.StringWidth(railSeam)
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
	if height <= 0 || !a.railStanding() {
		return nil, -1
	}
	room := a.railRoom()
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
	// THE SEAM IS A SEAM AND NOT A BORDER, so it is drawn only where there is
	// something on the other side of it. Over the body there is nothing to the
	// left of the roster, and a vertical rule down the left edge of a full-width
	// list is exactly the border this surface does not draw — so those two cells
	// go blank and keep their width, which is what the marker column is.
	seam := a.pal.dim(railSeam)
	if a.railFull() {
		seam = strings.Repeat(" ", ansi.StringWidth(railSeam))
	}
	out := make([]string, len(view))
	for i, line := range view {
		lead := seam
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
// It reads the SAME line list [app.railRows] draws — the same groups, the same
// folds, the same window — because a roster whose layout and whose clicks
// disagreed would open the room of the node above the one under the pointer.
//
// The roster's first row is the first row of the BODY REGION (view.go joins it
// from index zero there, and stacks it there whole when it is drawn over the
// body), which the rows the frame pins above that region move down by their own
// height — so a screen row is a roster index minus [app.topHeight] and not
// before.
func (a *app) railEntryAt(y int) (railEntry, bool) {
	y -= a.topHeight()
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
//
// On a frame with no column it is also what RAISES the roster, over the body
// ([app.railFull]): asking for the roster and asking for the keyboard are the
// same request, and which of the two shapes answers it is the frame's business
// and not the caller's.
func (a *app) railTake(hold bool) {
	if hold && !a.railAvail() {
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
		// ONE KEY AT EVERY WIDTH. With a column on the frame it hands the roster
		// the keyboard; without one it raises the roster over the body, which is
		// the same act with the same state behind it ([app.railFull]). Pressed
		// again — or esc — it puts it away.
		if !a.railHold && !a.railAvail() {
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
			add = railSep + seg
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
//	⠙ ◆ Fix nil-map           #7
//	  bash go test ./… · 42s
//	  42s · 9.9k · $0.31 · gpt-5
//	✓ ▲ Collect sources       #9
//	  merged · $0.42
//	◌ ● Mix audio            #11
//	  waits: Collect sources
//
// TWO GLYPHS OPEN THE ROW, AND THEY ANSWER TWO QUESTIONS. The first is the STATE
// and it changes as the work does; the second is the node's own identity and
// never changes at all (taskident.go). A person tracking one node out of four
// tracks the second one, which is exactly why it must not move when the first
// does — and it is the same mark the proposal card wore and the card that lands
// when the work is over will wear.
//
// THE NAME LEADS AND THE HANDLE TRAILS. The glyphs and the title are what a
// person reads down this column — the state, the node's own mark, and the words
// they themselves approved — and the id is what identifies the node to the
// MACHINE: it is the number the engine says in its own sentences ("task 7
// finished", session's task_run.go), the thing to type when you go looking for
// the branch, and the least interesting fact on the row. So it is dim, it is at
// the far end, and the title is measured against what is left rather than the
// other way round: a column that led with its ids would read like a process
// table.
//
// THE SUBTITLE IS NOT HERE. Every other place a task is drawn carries the one
// line that says what it is; this column is twenty-two cells wide and is a
// PRESENCE list — the question it answers is "what is alive", and a sentence
// clipped to twenty-two cells answers no question at all. The card that proposed
// the node and the card that lands when it finishes both carry it.
//
// THE SEAM CARRIES NO AGENT TYPE because the seam has none — internal/session's
// TaskNotice names a node's work and never its worker. When it grows one it joins
// the id in exactly this slot, in exactly this hue.
func (a *app) railNodeRows(node *taskNode, width int) []string {
	// The two glyphs and the two spaces after them are the row's fixed lead, and
	// the title is measured against what they leave.
	lead := a.railGlyph(node) + " " + a.taskMark(node.ident) + " "
	title, room := node.title, width-ansi.StringWidth(lead)
	// THE META SLOT IS THE HANDLE AND NOTHING ELSE. It used to carry the model
	// beside the id where the column could afford both, and that was the model
	// buying its cells from the NAME — the one thing this row exists to say. The
	// model rides the telemetry row under the title now ([app.railTelemetry]),
	// where it is beside the figures it belongs with and costs the title nothing;
	// the handle stays here, because it is what identifies the row to the machine
	// and it is four cells. It still stands down when the title cannot afford
	// even that.
	meta := railMetaWord(node)
	if room-ansi.StringWidth(meta)-1 < railTitleFloor {
		meta = ""
	}
	if meta != "" {
		room -= ansi.StringWidth(meta) + 1
	}
	title = fit(title, room)
	line := lead + a.railTitle(node, title)
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

// railModelWord is the model this node runs on, as a column this narrow can say
// it: the part of the id AFTER THE VENDOR, which is the part that names the
// model rather than who sells it. Empty when nobody published one, and then
// every row that would have drawn it draws nothing instead.
func railModelWord(node *taskNode) string {
	model := strings.TrimSpace(node.model)
	if slash := strings.LastIndex(model, "/"); slash >= 0 && slash+1 < len(model) {
		model = model[slash+1:]
	}
	return model
}

// railTitle paints an already-fitted title. The cut happens at the call site
// because that is where the id's cells are measured out of it ([app.railNodeRows]):
// a title fitted here and trimmed there would be a row measured twice.
func (a *app) railTitle(node *taskNode, title string) string {
	if node.state == session.TaskRunning {
		return a.pal.ink(title)
	}
	return a.pal.muted(title)
}

// railUnder is what a node says under its own title: what it is doing and what
// it is spending while it runs, what it waits on — or what is holding it —
// while it is blocked, and how the branch came home once it has landed.
//
//	bash go test ./…             a live call, in its own hue
//	finishing · adding amp-labs  the gap being closed, while there is one
//	waiting · rate limited       the hold, while something is holding it
//	42s · 9.9k · $0.31 · gpt-5   the telemetry, always, while it runs
//	merged · $0.42               what it came home as, and what it cost
//	conflicted · task/fix-nil    the one loud row, and its one handle back
//	waits: Collect sources       what has to happen before this can
//	waiting · machine busy       and what is holding it when nothing does
//
// THE ROWS THAT CARRY A HANDLE CARRY NOTHING ELSE. A conflicted branch, a kept
// branch, a prerequisite's name and "unverified — waiting on you" are each one
// fact a person has to ACT on, and a price appended to any of them would be a
// figure competing with the only thing on the row worth reading. The telemetry
// belongs to the states nobody has to do anything about — a node that is running
// and a node that came home clean.
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
		// A RUNNING NODE SAYS WHAT IT IS DOING, when the pilot lane has told this
		// surface (see [taskPilot]) — and that line carries its own clock in its
		// own hue, so it is built here rather than falling through to the
		// single-paint wrap below.
		//
		// AND THEN IT SAYS WHAT IT IS COSTING, always. The call is what the node is
		// doing this second and it is gone the second after; the telemetry is the
		// standing answer to "is this worth what it is burning", which is the
		// question a person opens this column for and cannot ask anywhere else
		// without leaving the conversation. Between calls the telemetry is the
		// whole of the under-block, which is what the bare clock used to be.
		//
		// AND WHILE THE NODE IS FINISHING OFF, THAT LINE IS THE ONE THAT WINS.
		// A node closing a named gap in work it has otherwise done is the most
		// specific news this column will ever have about it — "adding amp-labs to
		// the report" says both that the end is in sight and what the end is
		// missing — and the call it happens to be inside of while it does that
		// says neither. So it takes the call's row rather than a third one: the
		// block is capped at [railUnderRows] and the telemetry underneath is the
		// standing figure a person is owed at every moment of a run.
		//
		// AND A PACED NODE NEVER CLAIMS A LIVE CALL. While the provider is holding
		// this node's calls back (session's TaskNotice.Waiting) the tool line is
		// the last call, sitting there finished — so the row would be this column
		// asserting a present that is not happening, which is the one thing a row
		// down here may not do. The hold takes that row instead and says what is
		// actually true: the node is running, and it is waiting for its turn on
		// the wire. The telemetry underneath is unchanged, because the clock and
		// the bill go on being the clock and the bill.
		//
		// THE GAP OUTRANKS THE HOLD when a node somehow has both. "adding amp-labs
		// to the report" is news about the WORK and "rate limited" is news about
		// the wire, and of the two the first is the one a person came to this
		// column for; the hold only ever displaces the row that would otherwise be
		// false.
		rows := a.railMending(node, width)
		if len(rows) == 0 {
			rows = a.railWaiting(node, width)
		}
		if len(rows) == 0 {
			rows = a.railWorking(node, width)
		}
		if len(rows) < railUnderRows {
			if tele := a.railTelemetry(node, width); tele != "" {
				rows = append(rows, paint(tele))
			}
		}
		return rows
	case session.TaskQueued:
		// THE DEPENDENCY SENTENCE, and it is v1's own words — internal/tui says
		// "waits: <title>" and a person who has used that surface has already
		// learned what it means. In a one-node graph nothing is ever unmet and
		// this draws nothing at all; the model carries the edges regardless, so
		// the day the executor grows them the rail already knows.
		//
		// AND IT OUTRANKS THE HOLD WORD, on the one row a queued node gets. Both
		// are true of a node that is behind another node AND behind a full cap,
		// and only one of them is ACTIONABLE: a dependency names other work a
		// person can go and look at, reorder, or stop, while a hold names a queue
		// that is going to clear by itself. The more actionable fact takes the
		// row; the hold is what the row says when there is nothing better on it.
		waits := a.railWaits(node)
		if waits == "" {
			return a.railWaiting(node, width)
		}
		text = "waits: " + waits
	case session.TaskUnverified:
		// NOT THE MERGE SENTENCE. An unverified node wears session's "aborted"
		// merge like a stopped one does, and the row below would therefore say
		// "stopped — branch kept" about work that ran to the end. What it is
		// waiting for is a person, and that is what the row says.
		paint, text = a.pal.warn, taskUnverifiedWaits
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
			// THE MERGE WORD, AND WHAT THE WORK COST TO GET THERE. A node that came
			// home clean is the one settled row with nothing to act on, so it is the
			// one that can afford a figure — and the price is the fact a person goes
			// looking for afterwards, because the footer's Σ is the session's whole
			// spend and says nothing about which node ate it.
			//
			// THE MERGE WORD ALWAYS SURVIVES. The price is appended only when the
			// engine published one and only when the row has the cells for both: a
			// column too narrow for "merged · $0.42" says "merged", never "$0.42".
			text = node.merge
			if spent := node.spent(); text != "" && spent > 0 {
				if priced := text + railSep + dollars(spent); ansi.StringWidth(priced) <= width {
					text = priced
				}
			}
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
// title takes at this width, and it is exactly what a running node spends — the
// call it is in, then the telemetry — so the cap is the design and not a
// backstop. Past it the rail would be a paragraph, and the transcript is where
// paragraphs live.
const railUnderRows = 2

// railSep is what this column joins two facts on one row with, and it is the
// footer's own joiner ([railPack]): one vocabulary down the whole column.
const railSep = " · "

// ── THE ELAPSED CLOCK ───────────────────────────────────────────────────────
//
//	⠙ ◆ Fix nil-map
//	  bash go test ./…            under ten seconds: no number at all
//	  bash go test ./… · 24s      dim, because it is only slow
//	  bash go test ./… · 1m 8s    warn, because it is now the reason you are waiting
//
// A NUMBER THAT IS ALWAYS THERE IS A NUMBER NOBODY READS. The rail used to
// carry the node's own age from the first second, which is a figure that is
// true, ticking and almost never actionable: a node is SUPPOSED to take
// minutes. What is actionable is one CALL taking them — the test suite that
// hung, the fetch that is not coming back — so the clock is spent on the
// current call and appears only once that call has been running longer than a
// person would sit still for it.
//
// TWO STEPS, AND THE LOUD ONE IS NOT THE FAILURE HUE. Ten seconds is when the
// wait becomes a fact worth stating; a minute is when it becomes the thing
// about the row, and it takes [hueWarn] — the tier that means "this is about to
// be your problem" rather than "this went wrong", because a four-minute build
// is not a failure and a row that said it was would be the surface guessing.
//
// There is deliberately NO five-second step. The tool line has one (toolview.go
// escalates a bounded call into [hueBad] with five seconds left on its
// timeout), and that step means something precise there: the call is about to
// be killed at a moment the surface knows. Nothing is going to happen to a task
// at five seconds, and a colour that fired on nothing would teach a person to
// ignore the one that fires on something.
const (
	// taskToolFloor is how long a node's current call must run before its row
	// says so.
	taskToolFloor = 10 * time.Second
	// taskToolWarn is when that wait stops being background.
	taskToolWarn = time.Minute
)

// railWorking is the under-line of a node with a call in flight, or nil when
// the pilot lane has not told this surface what it is doing.
//
// ONE ROW, and the call's name gives up its tail to the clock rather than the
// other way round: which tool is running is a fact a person recognizes from its
// first few cells, and how long it has been running is the fact they came to
// the rail for.
func (a *app) railWorking(node *taskNode, width int) []string {
	if node.tool == "" {
		return nil
	}
	clock, tint := a.taskClock(node)
	tail := ""
	if clock != "" {
		tail = railSep + clock
	}
	name := fit(node.tool, width-ansi.StringWidth(tail))
	line := a.pal.dim(name)
	if clock != "" {
		line += a.pal.dim(railSep) + tint(clock)
	}
	return []string{line}
}

// railMending is the row a node wears while it is closing a named gap in work it
// has otherwise finished, or nil when there is no gap being closed.
//
//	finishing · adding amp-labs to the report   the whole of it, on a wide column
//	finishing · adding amp-labs to t…           and the same row, fitted
//
// THE WORD IS THE SURFACE'S AND THE SENTENCE IS THE ENGINE'S, which is the same
// split every other row down here is built on ([taskStoppedKept] states it about
// a merge word). "finishing" is this column saying which part of running this is;
// what follows the separator is the engine's own plain line about what is left,
// kept verbatim, because the whole value of the row is that it is SPECIFIC.
//
// IT CUTS RATHER THAN WRAPS. The two rows that wrap down here carry a handle
// back to work that is off screen — a branch name, a prerequisite's title — and
// half of one of those is worth nothing; this is a sentence, a person
// reconstructs a sentence from its front, and the row under it is the telemetry
// that has to survive too.
func (a *app) railMending(node *taskNode, width int) []string {
	if node.mending == "" {
		return nil
	}
	line := fit(taskFinishingWord+railSep+node.mending, width)
	if line == "" {
		return nil
	}
	return []string{a.pal.dim(line)}
}

// railWaiting is the row a node wears while it is HELD — behind a full slot,
// behind a machine under load, or behind a provider pacing its calls — or nil
// when nothing is holding it.
//
//	waiting · machine busy     a queued node the machine has no room for
//	waiting · slot             a queued node behind the parallelism cap
//	waiting · rate limited     a running node whose calls are being paced
//
// IT IS DIM, AND THAT IS THE WHOLE POINT OF IT. A hold is not a failure and it
// is not a thing to do anything about: the queue clears, the cap frees, the
// provider lets the next call through, and every one of those happens without a
// person. What the row is FOR is the question this column could not answer at
// all before it — a node that has sat still for four minutes is either stuck or
// merely waiting its turn, and those are opposite news wearing the same row. So
// it says which, in the quietest ink the column has.
//
// IT CUTS RATHER THAN WRAPS, for [app.railMending]'s reason: the two rows that
// wrap down here carry a handle back to work that is off screen, and this is a
// two-word state whose first word is the half that matters.
func (a *app) railWaiting(node *taskNode, width int) []string {
	if node.waiting == "" {
		return nil
	}
	line := fit(taskHeldWord+railSep+node.waiting, width)
	if line == "" {
		return nil
	}
	return []string{a.pal.dim(line)}
}

// railTelemetry is the standing row under a running node: how long it has been
// going, how much it has burned, what that has cost, and who is doing it.
//
//	42s · 9.9k · $0.31 · gpt-5    a full column, and everything known
//	42s · 9.9k · $0.31            a slim one: the worker is the first to go
//	42s · gpt-5                   an engine that publishes no usage
//	42s                           and one that publishes nothing at all
//
// RICHEST FIRST, DROPPED FROM THE RIGHT, AND THAT IS THE WHOLE WIDTH STORY. The
// segments are in the order a person needs them — the age is what says whether
// to look, the weight and the price are what say whether to stop it, the model
// is context for both — so the row is built whole and shortened by giving up its
// tail until it fits. That is one rule for a 26-cell column, a 20-cell one and
// the roster drawn over the whole frame: a mode switch here would be three
// layouts to keep true instead of one.
//
// ABSENCE RENDERS AS NOTHING. A figure nobody published is zero, and zero is not
// a measurement: the segment is skipped entirely rather than drawn as "$0.00" or
// "0" beside a node that has been working for a minute. A row with nothing known
// on it at all is no row.
func (a *app) railTelemetry(node *taskNode, width int) string {
	segs := make([]string, 0, 4)
	if clock := countUpWord(a.taskNow(node).Sub(node.began)); clock != "" {
		segs = append(segs, clock)
	}
	if node.tokens > 0 {
		// BARE, WITH NO UNIT ON IT. The footer's figure wears "tok" because it sits
		// beside a dollar sum and a row of counts; here the count is one of four
		// segments on a row twenty-odd cells wide, and three of those cells are the
		// difference between keeping the price and dropping it.
		segs = append(segs, tokenWord(node.tokens))
	}
	if spent := node.spent(); spent > 0 {
		segs = append(segs, dollars(spent))
	}
	if model := railModelWord(node); model != "" {
		segs = append(segs, model)
	}
	for ; len(segs) > 0; segs = segs[:len(segs)-1] {
		if line := strings.Join(segs, railSep); ansi.StringWidth(line) <= width {
			return line
		}
	}
	return ""
}

// taskClock is how long this node's current call has been running, and the hue
// that says how that is going. Both are empty under [taskToolFloor]: a call
// that has just started is a call nobody is waiting on yet.
func (a *app) taskClock(node *taskNode) (string, func(string) string) {
	if node.toolBegan.IsZero() {
		return "", nil
	}
	age := a.taskNow(node).Sub(node.toolBegan)
	if age < taskToolFloor {
		return "", nil
	}
	if age >= taskToolWarn {
		return countUpWord(age), a.pal.warn
	}
	return countUpWord(age), a.pal.dim
}

// taskNow is the clock ONE NODE's row is drawn against, and it is not always
// the surface's.
//
// A CLOCK FREEZES WHILE YOU ARE LOOKING INTO ITS TASK. The elapsed number
// exists to answer "should I go and look at this", and once a person is
// standing in the node's room they are already looking: the room shows the
// calls themselves, one line each, live. A number still climbing in the corner
// of the screen at that point is pressure applied to somebody who has already
// answered it — and it is pressure that keeps climbing while they read, which
// is the opposite of what a person reading needs. It thaws when they leave, at
// the value it would have had all along, because nothing here stops the clock
// so much as stops reporting it.
func (a *app) taskNow(node *taskNode) time.Time {
	if !node.froze.IsZero() {
		return node.froze
	}
	return a.now()
}

// freezeNode and thawNode are the two sides of that, called by the room's own
// door (room.go). They are here rather than there because the field is this
// file's and a room is a VIEW: it says where the person is, and what that means
// for a clock is the clock's own business.
func (a *app) freezeNode(id uint64) {
	if node := a.tasks[id]; node != nil && node.froze.IsZero() {
		node.froze = a.now()
	}
}

func (a *app) thawNode(id uint64) {
	if node := a.tasks[id]; node != nil {
		node.froze = time.Time{}
	}
}

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
	case session.TaskUnverified:
		return a.pal.warn(glyphUnverified)
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

// glyphUnverified marks the node nobody could judge, and it ASKS A QUESTION
// because that is what the state is: not a tick, which would claim a verdict
// nobody gave, and not a cross, which would claim a finding nobody made. It
// takes the warn hue rather than the ask hue — [glyphAsk] is the question the
// SESSION is blocked on and answering it is the next thing anyone does here,
// while this one waits for as long as it takes.
//
// It is the same cell in both glyph tiers: "?" is already a character a screen
// reader names, so there is nothing for the linear tier to stand in for.
const glyphUnverified = "?"

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
//
// UNVERIFIED IS THE ONE STATE THAT CAN BE LEFT AGAIN, and the pair holds through
// it: a person resolving one (session's ResolveUnverified) re-settles the node
// into done or failed, which is a state it has not been in, so the surface draws
// the second card — the decision is an event, and the card that says the work
// was accepted is the record of it.
func (a *app) taskUpdate(ev session.Event) tea.Cmd {
	notice := ev.Task
	if notice == nil {
		return nil
	}
	if last, seen := a.taskSeen[notice.ID]; seen && last == notice.State {
		// THE DE-DUP HAS EXCEPTIONS, and every one of them is news that arrives
		// without a state change. The pair above catches the same update arriving
		// on both lanes — those two carry identical figures and identical text —
		// but a node that has spent more since the last event is news, and the
		// focus header is where it is read (room.go).
		//
		// AND SO IS EVERY LINE THAT REPORTS THE PRESENT. A node closing a gap in
		// work it has otherwise finished stays RUNNING for the whole of it
		// (session's TaskNotice.Mending), and a node held behind a slot or paced
		// by its provider stays exactly where it was for the whole of THAT
		// (TaskNotice.Waiting) — so the sentence naming the gap, the word naming
		// the hold, and the empty strings that take either of them away again
		// would all be thrown out by a guard that only ever looked at the state.
		// The exception is written as one comparison over those live lines rather
		// than as a clause per field, because they are one kind of thing: what is
		// true of this node RIGHT NOW. Anything that is none of that is the
		// duplicate this guard exists for.
		node := a.tasks[notice.ID]
		if node == nil || (notice.CostUSD <= node.cost && taskLiveLines(notice) == node.liveLines()) {
			return nil
		}
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
		node = &taskNode{id: notice.ID, ident: identFor(notice.ID), met: a.now()}
		// THE CONTRACT IS COPIED OFF THE PROPOSAL, ONCE. The updates carry a state
		// and a title and nothing about what the work was for; the card that lands
		// minutes from now wants the brief, the acceptance and the sentence the
		// subtitle is cut from, and the only place any of those was ever said is
		// the question this surface already drew (see [app.cardFor]).
		if card := a.cardFor(notice.ID); card != nil {
			node.label = card.title
			node.assignment = firstNonEmpty(card.summary, card.brief)
			node.brief, node.acceptance = card.brief, card.acceptance
		}
		a.tasks[notice.ID] = node
		a.taskOrder = append(a.taskOrder, notice.ID)
	}
	if title := strings.TrimSpace(notice.Title); title != "" {
		node.label = title
	}
	node.title = taskTitleOf(node.label, node.assignment, node.id)
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
	// The model is kept whenever an update carries one and never overwritten
	// with an empty: it is a property of the work, settled at admission, and an
	// update that says nothing about it is not an update that changed it.
	if model := strings.TrimSpace(notice.Model); model != "" {
		node.model = model
	}
	if len(notice.Changed) > 0 {
		node.changed = notice.Changed
	}
	// The spend is kept whenever the engine has one to publish, and never
	// overwritten with a zero: a later update carrying no price would otherwise
	// take a figure off the focus header that was true (room.go).
	if notice.CostUSD > 0 {
		node.cost = notice.CostUSD
	}
	// THE LIVE LINES ARE COPIED WHOLE, INCLUDING THEIR ABSENCE, and they are the
	// fields on this node that are deliberately not kept when an update stops
	// carrying them. Everything above is a FACT about the work — a branch, a
	// price, a model — and a fact does not stop being true because the next event
	// was quiet about it. These are reports of what is happening RIGHT NOW, and a
	// surface still saying "finishing · adding the amp-labs section" about a node
	// that finished that ten seconds ago — or "waiting · machine busy" about a
	// node the machine let through a minute ago — is a surface reporting a
	// present that has passed (the same law [taskNode.tool] is held to).
	live := taskLiveLines(notice)
	node.mending, node.waiting = live.mending, live.waiting
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
	var pilot tea.Cmd
	switch notice.State {
	case session.TaskRunning:
		// The node is alive, so the surface starts WATCHING it (see [taskPilot]).
		pilot = a.flyPilot(notice.ID)
	case session.TaskDone, session.TaskFailed, session.TaskUnverified:
		// UNVERIFIED IS A LANDING. The run is over, the slot is handed back and
		// the pilot's lane has ended, so a surface that waited for one of the
		// other two would keep a spinner on a node nothing is doing and would
		// never write the one card that says a person has to decide
		// (session's task_contract.go).
		node.elapsed = notice.Elapsed
		a.landPilot(notice.ID)
		a.landedCard(node)
	}
	a.touch()
	return pilot
}

// cardFor is the proposal this surface drew about one node, or nil.
//
// It WALKS THE TRANSCRIPT, newest first, rather than keeping a second index of
// cards by id. The walk happens exactly once per node — at the moment the
// engine first admits it — and an index would be a third place that has to
// agree with [app.task] and the entry list about which card is which id, for a
// lookup that costs nothing on any conversation a person can scroll.
func (a *app) cardFor(id uint64) *taskCard {
	for i := len(a.entries) - 1; i >= 0; i-- {
		if e := &a.entries[i]; e.kind == entryTask && e.card != nil && e.card.id == id {
			return e.card
		}
	}
	return nil
}

// THE LANDED LINE IS NOW A CARD (taskdone.go). What stood here was the one dim
// sentence the transcript kept about a node — "task Fix the nil-map crash done
// in 2m 10s · merged" — and every fact in it survives, in a block that is read
// rather than skipped: the outcome, the elapsed, the kept branch and the
// engine's own failure sentence, verbatim, for the reason it was verbatim here.
// [app.landedCard] is the one place that record is written now.

// tasksAnimating reports whether anything on this surface's task side is
// moving: a countdown running down, a spinner turning, a clock counting up.
// It is what keeps the paint clock alive between turns — a node runs for
// minutes with no stream open, and the rail would otherwise freeze at whatever
// the last event drew.
func (a *app) tasksAnimating() bool {
	if a.awaitingTask() && !a.task.deadline.IsZero() {
		return true
	}
	// The strip turns the same spinner on a frame too narrow for a column, and
	// the roster over the body is the column by another shape — either one is a
	// reason to keep the paint clock alive (taskstrip.go, [app.railFull]).
	if !a.railStanding() && !a.stripShowing() {
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
	// THE WATCHERS GO TOO, and the generation is bumped so an event already in
	// flight on one of their lanes cannot write a current tool into the session
	// that replaced them (see [taskPilot]).
	a.pilots = nil
	a.pilotGen++
	// The "@" list's snapshot goes with them. The project's index survives — it
	// is the directory's and not this conversation's — but the live rows merged
	// into it are this conversation's, and the next "@" reads it again
	// (taskmention.go).
	a.dropTaskMentions()
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
