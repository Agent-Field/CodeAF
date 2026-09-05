package tui3

import (
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE ROOM: A TASK IS A PLACE, AND YOU CAN GO THERE.
//
// task.go says a node touches a person twice — as a decision (the proposal) and
// as a presence (the rail row) — and both of those are things you read ABOUT the
// work. Neither is the work. A node runs for minutes inside a worktree of its
// own, saying things and calling tools the whole time, and until this file the
// only trace of that on screen was a spinner and a clock: a person watching a
// node go the wrong way had exactly one move, which was to kill it and propose
// the corrected task again.
//
// So the rail row is a DOOR. Click it and the body region stops being the
// conversation and becomes that node's own transcript — its history replayed
// from its journal, then its present, live — and the input box below stops
// talking to the model and starts talking to the node. Esc comes back.
//
// ── A ROOM IS A VIEW, NOT A SECOND APP ──
//
// This is the whole reason the file is small. The room does not fork the update
// loop, does not hold a second agent, does not stop the turn: everything under
// it keeps running exactly as it was — the stream keeps streaming into
// [app.entries], the rail keeps ticking, the standing task lane keeps landing
// notes in the conversation — and the ONLY thing that changes is which rows
// [app.bodyRows] hands the frame. Esc restores the conversation exactly, scroll
// position included, because the conversation was never touched: the room keeps
// a scroll offset of its own and never writes the transcript's.
//
// The rail stays on screen for the same reason. The rail is how you leave one
// room for another, so it cannot be a thing you have to come back out to reach.
//
// ── A ROOM IS THE CONVERSATION'S OWN RENDERER, POINTED AT A NODE ──
//
// A room used to draw its own lines: an assistant block was plain text, a tool
// call was one dim line that did not expand, a person's message lost the
// pictures it carried, and reasoning was dropped on the floor. That was a
// SECOND RENDERING of the same four kinds of block, and it diverged the way a
// second rendering always does — not by decision, but one gap at a time, as the
// conversation grew a machine the page had never heard of.
//
// So the page is now built out of the SAME BLOCKS the conversation is made of
// (app.go's [entry]) and drawn by the SAME renderers (render.go's [deck] and
// [app.deckRows]). A node's call expands to its diff, its content, its output;
// its answer renders as markdown when it settles; its reasoning collapses to
// "thought for 6s · ctrl+e"; a person's steered message wears the person's hue.
// Nothing in this file paints a block, because the moment it did there would be
// two answers to "how does a tool call look" again.
//
// What a room still owns is what a room IS: which node, where the reader is in
// it, and the two doors — steering in, esc out.
//
// WITH ONE THING IT DOES DIFFERENTLY, AND IT IS DECLARED RATHER THAN SCATTERED:
// [taskRoom.deck] takes [overseerLens] (lens.go). Out in the thread a finished
// turn's machinery collapses to "▸ worked · 10 tool calls · ctrl+e"; in here
// the same chip is spent per SETTLED PHASE instead of per turn, because a
// node's life is one long turn and folding by turn swallowed the whole page the
// instant it stopped running. The live frontier never folds and keeps a whole
// screenful of calls. workfold.go states the law where the chips are derived,
// with the reversal it went through boxed beside it.
//
// ── THE DOORS ARE ASSERTED, NEVER REQUIRED ──
//
// [taskRoomAgent] is a SECOND interface rather than three more methods on
// task.go's [taskAgent], and the reason is the rail: widening taskAgent would
// mean a session that publishes task updates but has no room doors loses its
// rail as well as its rooms. The two capabilities are asked for separately and
// answered separately, and a surface whose agent cannot answer this one says so
// in a note and stays where it is.

// taskRoomAgent is the three doors onto one node (internal/session's
// task_room.go), asserted at the moment a room is opened.
type taskRoomAgent interface {
	// SteerTask puts the person's words into a running node's loop. It errors
	// when the node is unknown, not running, or has no worker up yet — all three
	// are "there is nobody in there to talk to", and all three are worth saying.
	//
	// The receipt is what the sending DID (internal/session's
	// [session.SteerReceipt]): delivered, delivered to a node that was WAITING ON
	// ITS OWN PIECES — which has no step coming to read the line at, so the line
	// is what wakes it — or HELD on the task's record while its work is being
	// checked. All three are successes and each has its own sentence, which this
	// surface draws and never rewrites.
	SteerTask(id uint64, text string) (session.SteerReceipt, error)
	// WatchTask subscribes to the node's live events, FROM NOW: no history is
	// replayed, and the channel closes at the node's final state. A finished node
	// answers with an already-closed channel rather than an error.
	//
	// It opens with ONE THING THAT ALREADY HAPPENED — the step the node is in the
	// middle of, which is in neither lane otherwise (internal/session's
	// [taskCatchup]): the reasoning it is spilling, the reply it has written so
	// far, and the calls it has asked for and not yet started. They arrive as the
	// ordinary kinds, in the order they happened, so nothing in here has to know
	// which side of the join an event came from.
	WatchTask(id uint64) (<-chan session.Event, error)
	// TaskJournal is the path to the node's transcript on disk, or "" for a node
	// whose journal this process never learned the name of.
	TaskJournal(id uint64) string
}

// roomDoors is the room's half of the agent under this surface, when it has one.
func (a *app) roomDoors() (taskRoomAgent, bool) {
	doors, ok := a.agent.(taskRoomAgent)
	return doors, ok
}

// taskSteerDoor is separate from the live-watch door because a hosted room is
// refreshed by bounded journal reads while its one write still crosses.
type taskSteerDoor interface {
	SteerTask(id uint64, text string) (session.SteerReceipt, error)
}

func (a *app) taskSteerDoors() (taskSteerDoor, bool) {
	door, ok := a.agent.(taskSteerDoor)
	return door, ok
}

// taskModelDoor is the fourth door onto a node (internal/session's
// [Agent.RetargetTask]): the person's explicit pick of another model for THIS
// node, taking effect on its next turn.
//
// IT IS ITS OWN INTERFACE for the reason [taskRoomAgent] is: a capability is
// asserted, never required. An engine that can stream a node and be steered but
// has never heard of retargeting keeps its rooms, and the model word in there is
// simply a fact with no door on it — which is the honest degraded state and the
// same one every node that is not running is in.
type taskModelDoor interface {
	RetargetTask(id uint64, model string) error
}

// taskModelDoors is that door under this surface, when it has one.
func (a *app) taskModelDoors() (taskModelDoor, bool) {
	door, ok := a.agent.(taskModelDoor)
	return door, ok
}

// roomModelMovable reports whether the room's model word is a DOOR as well as a
// fact — which is exactly the set of moments a press on it would do something.
//
// A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN, so this is what the render
// records the press target from ([app.identityParts]) rather than something the
// press checks after the fact. A finished, failed, stopped or unverified node's
// model is a fact about what happened and nothing can move it; a queued node has
// not started, and the engine refuses it in the same words; a run's page is a
// fleet rather than one node, and a node belonging to a run is not in the graph
// this door reaches. In every one of those the word is still drawn — a person is
// entitled to read what the work ran on — and it simply does not light and does
// not answer.
func (a *app) roomModelMovable() bool {
	if a.room == nil || a.room.orch != nil {
		return false
	}
	node := a.roomNode()
	if node == nil || node.run != "" {
		return false
	}
	if node.state != session.TaskRunning || node.stopped {
		return false
	}
	_, ok := a.taskModelDoors()
	return ok
}

// retargetTask is what choosing a model in a node's own picker does: ask the
// engine, and say what it said.
//
// THE ENGINE'S OWN SENTENCE IS KEPT on a refusal, the way a stop's is
// (stop.go's [app.stopTake]): "task 7 is done, not running" is the answer, and a
// surface that swallowed it would leave a person pressing the same name again.
// The gate above means a person cannot ordinarily reach one — the word is not
// pressable when it would refuse — but a node that landed in the instant between
// the frame and the press still gets told rather than ignored.
func (a *app) retargetTask(id uint64, model string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	door, ok := a.taskModelDoors()
	if !ok {
		a.note(taskModelUnavailableWord)
		return
	}
	if err := door.RetargetTask(id, model); err != nil {
		a.note(err.Error())
		return
	}
	// The engine publishes the new id on an update of its own, which is what moves
	// the roster row, the status line and the card this node eventually lands as —
	// so nothing here writes the node. The note is the RECORD of a decision, left
	// in the conversation where every other model change is written down
	// (palette.go's [app.switchModel] says `model · <id>`); the person standing in
	// the room reads the change off the status line under their hand.
	// The id and the model are both facts and the two `·` labels between them are
	// not, so the pair steps to ink and the scaffolding stays dim (payload.go).
	a.noteFacts(taskIDWord(id)+" · model · "+model, taskIDWord(id), model)
}

// taskModelUnavailableWord is the degraded case, in the vocabulary the other
// unavailable doors on this surface use (roomrefusal.go's [roomUnavailableRefusal],
// stop.go's [stopUnavailableWord]).
const taskModelUnavailableWord = "changing a task's model is unavailable — this session has no door onto it"

// ── the room's state ────────────────────────────────────────────────────────

// taskRoom is one node's page: what it has said, the lane carrying what it says
// next, and where the reader is in it.
type taskRoom struct {
	id    uint64
	title string
	// feed is this page's transcript and the reducer that grows it (feed.go).
	//
	// IT IS THE CONVERSATION'S OWN REDUCER AND NOT A SECOND ONE, which is the
	// whole of docs/design/lens/DESIGN.md's Decision 1: this file used to hold a
	// hand-copy of eleven of its methods, and being the copy is why a node's row
	// never said "took 4s", why a retry inside a task was invisible, and why an
	// end event paired by a different rule in here than out there. It is
	// EMBEDDED, so `room.entries`, `room.live`, `room.think` and `room.turn` mean
	// what they have always meant to every reader of them — the fields moved
	// house, not name.
	feed
	// unfolded is the page's OWN fold state, keyed by the page's own turns. It is
	// not the conversation's map for the reason the entries are not the
	// conversation's list: a turn number means nothing outside the list it counts
	// (render.go's [deck]).
	unfolded map[int]bool
	workOpen map[int]bool
	capOpen  map[int]bool
	lane     <-chan session.Event
	// stop LEAVES that lane, and is nil for an agent that offers no way out of
	// one. A room a person walked out of while the conversation goes on running
	// is a subscriber that must say goodbye: nothing else can tell a reader that
	// has gone from one that is redrawing, and a lane nobody drains parks a pump
	// (switcher.go's [laneStops] states the whole cost).
	stop func()
	// gen is the generation device the two other lanes on this surface use
	// (app.go's stream, task.go's standing subscription): a room that was closed
	// while its channel still had events in flight must not paint into the room
	// that replaced it.
	gen int
	// done says the WORK IS OVER — the lane closed, or the roster's row settled —
	// which is the one fact the room adds to what it is showing: a foot line, and
	// a refusal for anything typed after it.
	//
	// IT FOLLOWS THE ROW AND NEVER A DOOR'S REFUSAL ([roomRowDone] states the law
	// and [app.openRoom] says what broke without it): an id the graph has never
	// admitted is refused by every room door on a perfectly healthy session, and
	// reading that refusal as a landing put `task finished — esc to return` under
	// a header whose clock was still counting up.
	done bool

	// orch is set when this page is an ADAPTIVE RUN rather than a node
	// (roomorch.go): the same room, the same doors, the same geometry, drawing a
	// graph instead of a transcript. It is a field on this struct rather than a
	// second kind of page for the reason the page is built out of the
	// conversation's blocks — everything room.go promises (esc restores the
	// transcript, the rail stays, the scroll is the room's own, copy mode freezes
	// what is drawn) has to hold for both, and the only way to guarantee that is
	// for both to BE a room.
	orch *orchRun

	// harnessProgress is the design lane's one evolving thought inside the
	// design node's room. It is display-only: no journal line is minted for live
	// telemetry, and the next event replaces this string in place.
	harnessProgress string

	// The reader's own position. It is HERE and not on the app because that is
	// the whole promise of esc: the conversation's scroll is not touched while a
	// room is open, so returning to it restores nothing because nothing moved.
	offset int
	stick  bool

	// The row cache, on the same terms every other cached block on this surface
	// has one (render.go): rebuilt when the content or the width changes and at
	// no other time — and, unlike the conversation's, on the HEIGHT too, because
	// a room's fold keeps as many calls as its view is tall ([app.roomToolTail])
	// and a taller view is a different row list.
	rows          []row
	width         int
	height        int
	dirty         bool
	loading       bool
	readFailed    bool
	journal       []byte
	lastSteerAt   time.Time
	pendingSteers []roomSteerEcho
}

type roomRecordMsg struct {
	gen    int
	record session.TaskRecord
	err    error
}

// farRoomTickMsg is the room's own beat. It schedules a bounded read as a
// command; the update loop itself never waits on the other machine.
type farRoomTickMsg struct{ gen int }

// deck is the page as the renderers take it (render.go). It is a view over the
// live fields rather than a copy: the row cache each entry carries is written
// through it.
func (r *taskRoom) deck() deck {
	running := 0
	if r.lane != nil && !r.done {
		running = r.turn
	}
	// THE LENS IS THE WHOLE OF WHAT MAKES THIS PAGE A ROOM (lens.go): settled
	// phases fold to chips, the numbers gather in the header rather than under
	// each turn, the session's clock does not run over a node's turns, and a
	// folded cluster keeps a screenful of calls instead of three.
	return deck{
		entries: r.entries, unfolded: r.unfolded, workOpen: r.workOpen, capOpen: r.capOpen,
		lens: overseerLens, runningTurn: running,
	}
}

// The words the room says of itself.
const (
	// roomLegendWord replaces the path in the legend while a room is open: where
	// you are, and the keys that leave. It names both of them for the reason the
	// header does — a person's hand is either on esc or on the arrows.
	roomLegendWord = "room · esc/←← main"
	// roomLegendRecallWord stands in that word's place while a history walk is
	// on, because for exactly that long esc is the WALK's key and gives the
	// person their own draft back (recall.go's [app.recallCancel]) — the room is
	// one keystroke further away. The legend promises what the next esc does, and
	// a slot that kept promising "main" through a walk would be promising the
	// keystroke after the one the person is about to press.
	roomLegendRecallWord = "room · esc your line back"
	// roomRecallHint and roomStopHint are the room's half of the hint slot
	// (render.go's [app.hintWord]). Neither names esc: the legend's LEFT end is
	// already carrying that key while a room is open, and one row saying the same
	// thing twice is the defect the rewind mode's empty hint exists to avoid.
	roomRecallHint = "↑↓ history"
	roomStopHint   = "x stop"
	// roomGoneWord is the one line a landed node's room draws when there is
	// NOTHING to replay: no lane, and no journal entries. The engine keeps the
	// transcript's path across restarts and finds it by id when it was not
	// kept (session's task_room.go), so this is the page for a file that is
	// actually gone — a session folder somebody deleted — and it says so as a
	// fact rather than leaving a foot under a blank.
	roomGoneWord = "this task's transcript is not here any more"
	// roomYetWord stands in that line's place for a node that HAS NOT LANDED, and
	// it is the honest half of the same sentence: nothing has arrived on this page
	// is not the same fact as nothing is left of it. A node that is queued has
	// journaled nothing because it has not started, and one that has just started
	// has journaled nothing because its first message is still being written
	// ([session.ReadTranscript] reads a file the node is filling in), so BOTH of the
	// landed words above would be a lie — one saying a file was lost that was
	// never written, the other saying work finished that has not begun.
	//
	// It comes off the moment the page has any block at all, which for a running
	// node is the first thing that arrives on its lane: this line answers exactly
	// one question — why is there nothing here — and a page with something on it
	// is not asking it.
	roomYetWord = "nothing on this page yet — it fills in as the task works"
	// roomParkedWord opens the guard's line, after the node's title: what is
	// wrong, in three words, before the three keys that answer it.
	roomParkedWord = " is parked — "
	// roomBusyWord is the opener when the node is STILL RUNNING and simply has
	// nobody inside to read a line — mid-check, or landing. "is parked" would be
	// a lie about live work, and the revive key is withheld for the same reason:
	// asking the head to start the work again while the original is minutes from
	// done manufactures a duplicate task.
	roomBusyWord       = " cannot read this right now — "
	roomLoadingWord    = "loading this task's conversation…"
	roomReadFailedWord = "couldn't read this task's conversation · retrying"
	// roomSteerLane is the input's placeholder while a room is open, with the
	// node's title spliced in: the box says who it is talking to, because it is
	// the same box that talks to the model. It names the way out as well —
	// the box is where a person's eye is, and "who is listening" and "how do I
	// stop talking to them" are one question asked twice.
	roomSteerLane = "Steer "
	roomSteerBack = "… (esc: main)"
	// roomBackWord is the focus header's right end: the two gestures that return
	// to the conversation, in the order a hand reaches for them.
	//
	// It names ← and not ←← because that is what the arrow grammar settled on —
	// one ← steps back a level and two go home to the live edge (see
	// [app.navBack]) — and a header that named the second gesture for the first
	// would be the one row on the page that lies about a key.
	roomBackWord = "esc/← main"
	// roomCrumbRoot is where every breadcrumb starts, and it is the ONE name on
	// this surface for the conversation itself.
	roomCrumbRoot = "main"
	// roomCrumbSep separates one step of the trail from the next.
	roomCrumbSep = " ▸ "
	// The two sentences the kin block says, in the alphabet the rail already
	// spells a family relation in — "waits: <title>", so "part of: <title>" and
	// "spawned: <title>" (task.go's [app.railUnder]). A person who has read the
	// roster's rows has already learned this punctuation.
	roomKinUnderWord = "part of: "
	// AND `spawned:` WAS THE MACHINERY'S OWN WORD. It is what a process does to
	// another process, and this house bans it in anything a person reads — the
	// same rule that took `worktree` off the completion card. What actually
	// happened is that this piece of work handed some of itself out, which is the
	// verb the rest of the surface already uses for it.
	roomKinSpawnedWord = "handed out: "
	// roomKinStateSep joins a child to its state word on the spawned line. It is
	// the em dash the surface already uses to hang a condition off a name
	// (task.go's [taskStoppedKept], "stopped — branch kept"), so the two levels
	// of the list read apart: children are separated by [railSep], and a child
	// from its own state by this.
	roomKinStateSep = " — "
	// roomKinIndent hangs the kin rows under the trail rather than under the
	// state glyph, which is the whole of their layout: they are about the node
	// the line above names, and a block flush with the header would read as a
	// second header.
	roomKinIndent = "  "
)

// roomTail is how much of a journal a room opens showing. A node's transcript is
// a whole session file and can be hundreds of messages; this is about four
// screens, which is where a person's memory of "what has it been doing" ends.
// The rest is on disk, in a file the journal path names.
const roomTail = 120

// The lane's two messages — roomEventMsg and roomClosedMsg — are declared with
// the surface's other lanes in app.go, because a lane is a thing the program
// loop routes and one list of what it routes is worth more than three.

// ── opening and closing ─────────────────────────────────────────────────────

// openRoom enters a node's page: the journal replayed for history, then the live
// lane subscribed for the present.
//
// The two are separate doors on purpose (internal/session's task_room.go says
// why): a watcher gets what happens FROM NOW and no history is re-narrated, so
// the history has to come off disk or not at all. A node with no journal path
// opens on its live edge, which is honest — this process never learned which
// file that node wrote.
//
// THE ORDER IS THE JOURNAL AND THEN THE LANE, and it is not arbitrary. The file
// holds every message that has COMPLETED and the lane opens with the step in
// flight ([taskRoomAgent.WatchTask]), so reading first and subscribing second is
// what makes the two meet at one instant instead of overlapping: they are read
// microseconds apart, in the order the node writes them.
// The command that starts the lane is PARKED rather than returned, in
// [app.roomPump]. See that field for the call path that forces it.
// newRoom builds a node's page with its reducer already installed, and it is the
// ONE door that builds one.
//
// A PAGE WITH NO REDUCER IS A PAGE THAT DRAWS NOTHING, and a page whose reducer
// was built by struct literal is worse than that ([newFeed] says what the zero
// value does). Three places open a room — a node, a hosted node, an adaptive run
// — and three copies of the same eight fields is three chances for the fourth
// one to be built wrong.
func (a *app) newRoom(id uint64, title string) *taskRoom {
	a.roomGen++
	r := &taskRoom{
		id: id, title: title, gen: a.roomGen,
		unfolded: map[int]bool{},
		capOpen:  map[int]bool{},
		stick:    true,
		dirty:    true,
	}
	r.feed = newFeed(a.roomFeedHooks(r))
	r.mdAt = a.now()
	return r
}

// roomFeedHooks is the task page's whole declaration of what it is, as far as
// the reducer that grows its transcript is concerned (feed.go).
//
// THREE OF THEM, AND THE THIRD IS THE SECOND. Out in the conversation, growing
// at the edge and having stale rows are two facts with two answers — the frame
// clock throttles the flood of deltas and only the structural changes repaint.
// A room's row cache is keyed on ONE flag ([app.roomRows]), so both hooks are
// the same act in here, and [app.roomTouched] is that act named for the several
// other files that perform it.
//
// THE THREE THE CHAT INSTALLS ARE ABSENT AND THE ABSENCE IS THE DESIGN. A node
// draws no spawn card, so it forms none, refuses none and throws none away when
// a request is cut (lens.go's [lens.spawnCards] is where the chat declares that
// it does); a node's spend is folded by its pilot and never twice (see
// [app.roomEvent]).
//
// AND `closed` IS INSTALLED, which is the fourth (Decision 4). A call that
// finished is the only event that moves the ambient counts, and a node's calls
// move them exactly as the conversation's do: `bash` with background:true starts
// a process on this machine whoever asked for it. Folding it here — at the
// instant the wire says the call closed — is what makes the header instrument
// and the Σ segment one set of numbers instead of two clocks, and it is the one
// place a room can drop a cache the conversation owns ([app.tallyNode]).
//
// IT CLOSES OVER THE PAGE AND NOT OVER [app.room], because the page is built
// before it is the one on screen, and a reducer pointed at whatever room happens
// to be open is a reducer that paints a closed page's events into its successor.
func (a *app) roomFeedHooks(r *taskRoom) feedHooks {
	grew := func() {
		r.dirty = true
		if r.stick {
			r.offset = 0 // resolved from the bottom by roomOffsetFor
		}
		a.touch()
	}
	// snap is the conversation's own answer: a node's page paces its live edge
	// exactly as the transcript does, and stops for the same reader (reveal.go).
	return feedHooks{
		now: a.now, follow: grew, touch: grew,
		snap:   func() bool { return a.linear },
		closed: func(*entry, session.Event) { a.tallyNode(r.id, r.entries) },
	}
}

func (a *app) openRoom(id uint64, title string) {
	doors, ok := a.roomDoors()
	if !ok {
		// Local engine windows use this same door without a remote host label.
		if a.farRoomRecord != nil || a.farRecord != nil {
			node := a.tasks[id]
			if node != nil && (a.farRoomRecord != nil || node.transcript != "") {
				a.openFarRoom(node, title)
				return
			}
		}
		// THE BUILD GUARD. The doors are an assertion and not a compile-time
		// requirement, so a surface driven by an agent that has never heard of a
		// room says so and stays in the conversation.
		a.note(roomUnavailableRefusal.line())
		return
	}
	if title == "" {
		// A node nobody has named yet opens under the name a person can still say
		// out loud ([taskIDWord], taskident.go) — and the header takes the real one
		// the moment the engine publishes it ([app.taskUpdate]).
		title = taskIDWord(id)
	}
	room := a.newRoom(id, title)
	room.entries, room.turn = a.roomRecord(
		session.ReadTranscript(doors.TaskJournal(id)), roomTail)
	// AND WHAT THIS NODE HAS ALREADY STARTED JOINS THE SESSION'S COUNTS. Opening
	// the page is the moment this surface first READS a node's history, and a
	// background job it started an hour ago is as alive as one it starts while
	// somebody is watching ([app.tallyNode], Decision 4).
	a.tallyNode(id, room.entries)
	a.room = room
	prefetch := a.prefetchRoomPictures()
	// AND THE HISTORY IS MARKED WITH THE CONTEXT IT HAPPENED IN (turncontext.go).
	// The journal records what was said and never where the saying went, so the
	// mark is put on here — from the node the engine published, the same source
	// every live line in this room takes it from. It is the room's OWN context
	// because these lines were said into this node and nowhere else; it is empty
	// for an ordinary node, and then nothing is marked at all.
	a.markRoomContext(room)
	// THE NODE'S CLOCK STOPS BEING REPORTED WHILE YOU ARE IN HERE. The elapsed
	// number on the rail exists to ask "should you go and look at this", and the
	// person has just answered it (task.go's [app.taskNow] says the whole of it).
	a.freezeNode(id)
	// The selection and the pointer belong to the conversation, which is no
	// longer the thing on screen: a highlight under a room is a highlight on a
	// row nobody can see.
	a.sel = -1
	a.dropHover()
	a.touch()

	lane, stop, err := roomLaneOf(doors, id)
	if err != nil {
		// An unknown id. The room still opens — the journal is worth reading —
		// and it opens finished, because there is nothing to listen to. A call the
		// file left running is resolved on the way in for that same reason:
		// nothing is coming for it here either.
		//
		// THE REFUSAL ITSELF IS NOT DRAWN, and that is a law and not a taste. What
		// the door says here is `no task 4 in this session` — the engine telling a
		// caller that an id is not in its graph — which is machinery vocabulary
		// about a row the person is looking at RIGHT NOW, and it reads as the
		// surface having lost the work rather than as an answer to anything they
		// did.
		//
		// AND IT IS NOT APPENDED AS A BLOCK, which is the second half of the bug it
		// caused. The "there is nothing here" line below is drawn only for a room
		// whose block list is EMPTY, so one machinery note was enough to make the
		// page count itself as having a transcript — leaving a correct header over
		// a body holding the refusal and the foot, and nothing else. What the room
		// knows is drawn by [app.roomRecordRows] instead.
		//
		// AND THE ROOM DOES NOT TAKE THE REFUSAL FOR A LANDING, which is the third
		// half of the same bug. This branch used to set `done` outright, so every
		// reader of it spoke the landed vocabulary over work that was still going:
		// the foot said `task finished — esc to return` and the box's placeholder
		// said it again. What is true about whether the work is over is on the
		// ROSTER'S ROW, which the engine publishes on every notice, so that is
		// what is asked ([roomRowDone]).
		room.done = roomRowDone(a.tasks[id])
		room.resolveUnfinished()
		a.roomPump = tea.Batch(prefetch, a.wake())
		return
	}
	room.lane, room.stop = lane, stop
	a.roomPump = tea.Batch(waitRoom(lane, room.gen), prefetch, a.wake())
}

// openFarRoom opens a hosted node immediately and asks the engine for its
// bounded journal tail off the program loop. The id names running work before
// its record has a transcript URI, which is the gap the record-only door could
// never cross.
func (a *app) openFarRoom(node *taskNode, title string) {
	if title == "" {
		title = taskIDWord(node.id)
	}
	room := a.newRoom(node.id, title)
	room.done, room.loading = roomRowDone(node), true
	a.room = room
	a.sel = -1
	a.dropHover()
	a.touch()
	a.roomPump = a.readRoomRecord()
}

// Capture the reader and identity before leaving the program loop, for both
// the first read and refreshes. A window switch cannot change an in-flight read.
func (a *app) readRoomRecord() tea.Cmd {
	if a.room == nil {
		return nil
	}
	read, fallback, id, gen := a.farRoomRecord, a.farRecord, a.room.id, a.room.gen
	uri := ""
	if node := a.tasks[id]; node != nil {
		uri = node.transcript
	}
	if read == nil && (fallback == nil || uri == "") {
		return nil
	}
	return func() tea.Msg {
		var record session.TaskRecord
		var err error
		if read != nil {
			record, err = read(id, session.TaskJournalTail)
		} else {
			record, err = fallback(uri, session.TaskJournalTail)
		}
		return roomRecordMsg{gen: gen, record: record, err: err}
	}
}

func (a *app) farRoomRead(msg roomRecordMsg) tea.Cmd {
	if a.room == nil || a.room.gen != msg.gen {
		return nil
	}
	a.room.loading = false
	a.room.readFailed = msg.err != nil
	if msg.err == nil {
		a.refreshRoomRecord(msg.record.Journal)
	}
	a.room.resolveUnfinished()
	a.roomTouched()
	prefetch := a.prefetchRoomPictures()
	node := a.tasks[a.room.id]
	a.room.done = roomRowDone(node)
	if a.room.done && msg.err == nil {
		return prefetch
	}
	return tea.Batch(prefetch, farRoomTick(a.room.gen))
}

// farRoomEvery is deliberately slower than the paint clock: a journal tail is
// a disk-and-wire reading, not animation. Four reads a second keeps prose live
// without turning thirty frames a second into thirty calls.
const farRoomEvery = 250 * time.Millisecond

func farRoomTick(gen int) tea.Cmd {
	return tea.Tick(farRoomEvery, func(time.Time) tea.Msg { return farRoomTickMsg{gen: gen} })
}

func (a *app) farRoomPoll(gen int) tea.Cmd {
	// A JOB'S PAGE ANSWERS THIS BEAT FIRST, and it is the same beat on purpose:
	// one clock discipline for every bounded reading this surface takes
	// (joblog.go). It is asked ABOVE the room so a tick armed for the page is
	// not dropped on a room that happens to share a generation.
	if a.jobPageOpen() {
		return a.jobPagePoll(gen)
	}
	if a.room == nil || a.room.gen != gen {
		return nil
	}
	if a.room.done && !a.room.readFailed {
		return nil
	}
	return a.readRoomRecord()
}

// leavableRoomDoors is the room lane WITH A WAY OUT OF IT (session's
// task_room.go). It is asserted separately from [taskRoomAgent] for that
// interface's own reason: a scripted agent in this package's tests offers the
// lane and has never heard of the door.
type leavableRoomDoors interface {
	WatchTaskRoom(id uint64) (<-chan session.Event, func(), error)
}

// roomLaneOf opens one node's lane and hands back whatever way out the agent
// offers. A nil stop is an agent that can only be abandoned.
func roomLaneOf(doors taskRoomAgent, id uint64) (<-chan session.Event, func(), error) {
	if leavable, ok := doors.(leavableRoomDoors); ok {
		return leavable.WatchTaskRoom(id)
	}
	lane, err := doors.WatchTask(id)
	return lane, nil, err
}

// takeRoomPump hands the program loop whatever a door just parked, once.
func (a *app) takeRoomPump() tea.Cmd {
	cmd := a.roomPump
	a.roomPump = nil
	return cmd
}

// closeRoom returns to the conversation. The generation is bumped so an event
// already in flight on the old lane cannot land in a room that is no longer
// open, and nothing about the transcript is touched — which is the whole of
// "esc restores it exactly".
func (a *app) closeRoom() {
	if a.room == nil {
		return
	}
	a.roomGen++
	// AND THE LANE IS GIVEN BACK. A room closes while its conversation goes on
	// running, so there is nobody to close the channel for us the way an agent
	// being closed would (switcher.go's [laneStops]).
	if a.room.stop != nil {
		a.room.stop()
	}
	// The clock thaws where it was frozen, at the value it would have had all
	// along: nothing was stopped, only unreported (task.go's [app.taskNow]).
	a.thawNode(a.room.id)
	a.room = nil
	// The guard is a question about a line typed at THIS node. Leaving the room
	// takes it down: the two answers it offers are both about a page that is no
	// longer on screen, and the words are still in the box either way.
	a.guard = nil
	a.dropHover()
	a.touch()
}

// waitRoom takes one event off a room's lane and asks for the next.
func waitRoom(ch <-chan session.Event, gen int) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return roomClosedMsg{gen: gen}
		}
		return roomEventMsg{gen: gen, ev: ev}
	}
}

// roomOpen reports whether the body region is a room right now. Everything that
// asks a geometric question about the transcript asks this first.
func (a *app) roomOpen() bool { return a.room != nil }

// roomStandingOn reports whether this node's row is the door to the page that is
// on screen right now — "you are in here", asked of one row.
//
// IT IS THE ONE ANSWER BOTH LISTS OF THE WORK READ. The strip marks the chip of
// the room a person is standing in (taskstrip.go) and the roster now marks the
// row (task.go's [app.railRows]), and a surface where the tab bar and the column
// could disagree about which door you went through would be a surface with two
// answers to a question that has one.
//
// A RUN'S PAGE IS NOT A NODE'S, and it is matched by the door rather than by the
// id: [app.openOrchRoom] builds its room with id zero on purpose, so everything
// keyed on the id reads zero and marks nothing. What a run's rows carry instead
// is the run they belong to and, on a child, the node inside it — the same pair
// [app.railPress] opens the page with — so the row that lights is the row whose
// press would land exactly where the reader already is.
//
// Nothing at all while no room is open, which is the emptiness law said about a
// highlight: a mark for "where you are" on a surface you have not gone anywhere
// on is a mark that means nothing.
func (a *app) roomStandingOn(node *taskNode) bool {
	if a.room == nil || node == nil {
		return false
	}
	if run := a.orchOf(); run != nil {
		if node.run == "" || node.run != run.id {
			return false
		}
		// The run's page opens on the graph and descends into one node's card, so
		// the row standing for the page is the root while no card is open and the
		// child whose name the card carries once one is (roomorch.go).
		return node.node == run.card
	}
	return node.id != 0 && a.room.id == node.id
}

// openRoomFor opens the room of the node with this id, or closes it when it is
// already the room on screen. It is what BOTH doors resolve to — the rail click
// and the transcript walk — so a second press on either is always the way back.
func (a *app) openRoomFor(id uint64, title string) {
	if a.room != nil && a.room.id == id {
		a.closeRoom()
		return
	}
	a.openRoom(id, title)
}

// openRoomAt is the KEYBOARD door: enter on a selected proposal row opens that
// node's room, if there is a node behind it yet.
//
// It reports whether it took the gesture, because the row it is offered is the
// row [app.openTool] would otherwise expand — a proposal is not a tool call, so
// a false here is the honest "this is not mine".
//
// A card whose node this surface has never seen an update for opens nothing: the
// id is real, but the engine has not admitted it, and a room on a node that has
// not started is a page with nothing on it and nothing coming.
//
// A LANDED CARD OPENS ONE TOO (taskdone.go). Its lane is already closed, so the
// room is the node's journal and a foot saying the work is over — which is
// exactly the thing a person pressing enter on "what did that task actually do"
// is asking for. The card's own key (ctrl+o) opens the summary of it inline;
// this opens the whole transcript. Two questions, two answers, one card.
func (a *app) openRoomAt(i int) bool {
	if i < 0 || i >= len(a.entries) {
		return false
	}
	switch e := &a.entries[i]; e.kind {
	case entryTask:
		card := e.card
		if card == nil {
			return false
		}
		node := a.tasks[card.id]
		if node == nil {
			return false
		}
		a.openRoomFor(node.id, firstNonEmpty(node.title, card.name))
		return true
	case entryDone:
		if e.done == nil {
			return false
		}
		a.openRoomFor(e.done.id, e.done.title)
		return true
	}
	return false
}

// ── the record, replayed ───────────────────────────────────────────────────
//
// THE VIEW NEVER PARSES A RECORD AGAIN. This file used to hold a second reader
// of internal/session's session file — its own line struct, its own scanner, its
// own list of which roles exist, and hand-copied duplicates of two display caps
// that package does not export — and it was the copy that fell behind: it knew
// `user` and `assistant` and nothing else, so a line the session had MARKED came
// back as an ordinary question and yesterday's correction read as a second
// brief.
//
// The reading is internal/session's now ([session.ReadTranscript] and
// [session.ReadTranscriptBytes], which answer the same [session.DisplayEntry]
// shape a live conversation is drawn from), and the shaping is replay.go's one
// walk ([app.replayBlocks], under [roomReplay]). One record, one reading, one
// shaping — so a mark the engine writes cannot be a mark this page fails to
// know about (#252).

// ── the live lane ───────────────────────────────────────────────────────────

// roomEvent folds one of the child's events in and re-arms the pump.
//
// IT IS [app.apply]'s SHAPE, over the page's own reducer: what the event does to
// the transcript is [feed.ingest]'s, once, for every surface, and what it asks
// THIS program loop to do is the switch below. This file used to hold a
// hand-copy of the reducer instead — eleven functions that agreed with the
// conversation's on the day they were written and had fallen behind by six event
// kinds — which is the whole reason the reducer exists (feed.go states the law,
// docs/design/lens/DESIGN.md's Decision 1 states the design).
//
// THE SPEND IS NOT COUNTED HERE, AND THE OMISSION IS THE DESIGN. A node's turn
// totals are folded by its PILOT (task.go's [app.pilotEvent]), which keeps flying
// for as long as the node runs whether or not anybody is standing in its room —
// [app.openRoom] opens a watch of its own beside it rather than taking the
// pilot's over, because internal/session's door is a fan-out (task_room.go:
// openRoom().join()) and hands each caller its own copy of every event. A fold
// on this lane as well would therefore bill every turn twice for exactly as long
// as a person had the page open.
//
// AND NOTHING IN A TASK ASKS THIS KEYBOARD A QUESTION. A node's policy allows
// everything but the floor and the floor refuses rather than prompts, so a
// consent request and a task proposal reach the chat and never a room — the one
// class of event this page is deliberately without, and the reason is written
// where it is tested (salience_test.go) rather than only here.
func (a *app) roomEvent(ev session.Event) tea.Cmd {
	room := a.room
	if room == nil {
		return nil
	}
	var after tea.Cmd
	// THE COLLAPSE RULE, quoted from the conversation's pump (app.go's [app.apply],
	// thinking.go): the first thing a turn says that is not reasoning ends the
	// reasoning block, and EventThinking is exempt because it is the marker that
	// opened the run. A text delta settles the block and keeps it
	// ([feed.settleThought]) so an interleaved stream grows one block instead of
	// sawing the answer apart; everything else seals it.
	switch ev.Kind {
	case session.EventReasoning, session.EventThinking:
	case session.EventTextDelta:
		room.settleThought()
	default:
		room.collapseThought()
	}
	room.ingest(ev)
	switch ev.Kind {
	case session.EventToolEnd:
		// A FILE THE NODE JUST WROTE ON THE OTHER MACHINE IS FETCHED NOW,
		// speculatively, before anybody has clicked anything (remotefiles.go).
		// Nil on every local session.
		after = a.prefetchWritten(ev)

	case session.EventTurnDone:
		// THE STEP IS FINISHED AND ON DISK — the same event internal/session's
		// [taskCatchup] takes as the signal to drop what it was holding. The
		// block the node was writing is therefore over, and settling it here is
		// what stops the NEXT step's first word from being appended to the last
		// step's last paragraph. A design felt this hardest: its draft and the
		// revision the review pass writes are two replies on one lane, and
		// without this they arrived as one unbroken wall of JSON.
		room.closeLive()

	case session.EventError:
		a.roomNote("error: " + errText(ev.Err))
	}
	a.touch()
	return tea.Batch(after, waitRoom(room.lane, room.gen), a.wake())
}

// roomNote is the surface's own line inside the room — [feed.note] over the
// page's list, drawing the same dim "· " block, with the same
// same-sentence-twice rule and the same promise not to cut a reply in two.
//
// The blank is refused here rather than in the reducer because it is a fact
// about the CALLERS in this file: a refusal with nothing to say and an engine
// error with an empty reason both reach this door, and the emptiness law says a
// line about nothing is no line at all.
func (a *app) roomNote(text string) {
	if a.room == nil {
		return
	}
	if text = strings.TrimSpace(text); text != "" {
		a.room.note(text)
	}
}

// roomAppend adds one block and closes whatever was streaming: a block that
// follows the node's words is the node having stopped saying them.
func (a *app) roomAppend(e entry) {
	room := a.room
	if room == nil {
		return
	}
	room.entries = append(room.entries, e)
	// THE POINTER GOES AND THE EDGE SNAPS WITH IT (livestate.go). The block is
	// deliberately not settled here — [app.roomCloseLive] is the door that ends
	// one — but a block nobody is walking must not stay a prefix on the page.
	abandonLive(room.entries, &room.live)
	a.roomTouched()
}

// roomSaid is [feed.said] over the room's own transcript: the person's line goes
// in below the answer that is still streaming rather than cutting it in two.
// See [feed.said] for the defect and the reasoning.
func (a *app) roomSaid(e entry) {
	if a.room == nil {
		return
	}
	a.room.said(e)
	a.roomTouched()
}

// roomTouched drops the room's cached rows and keeps a reader at the live edge
// where they were already at it. It is the page's own [feedHooks.follow], named
// for the several files that perform that act without going through an event.
func (a *app) roomTouched() {
	if a.room != nil {
		a.room.follow()
	}
}

// The call's identity, its sentence and its rail all come from the same two
// renderers the conversation's tool line comes from (toolview.go, toolstat.go).
// This file used to hold a third, one-line rendering of a call; it is gone,
// and its absence is the point (see this file's header).

// ── steering ────────────────────────────────────────────────────────────────

// steer is enter, while a room is open: the sentence in the box goes to the
// NODE, and lands in the room as the person's own line.
//
// It is the person's voice under the person's law — the accent hue, the same
// glyph the conversation's user block wears — because from the node's side it is
// exactly what it looks like: somebody talking. The engine wraps it in nothing
// (internal/session's SteerTask), and neither does this.
//
// A NODE THAT IS NOT LISTENING RAISES THE GUARD instead of swallowing the line
// (see [steerGuard]). The box is not cleared in that case: the sentence is still
// the person's, and taking it away after telling them it went nowhere would be
// the surface losing their words twice.
func (a *app) steer() tea.Cmd {
	room := a.room
	line := strings.TrimSpace(a.input.String())
	if room == nil || line == "" {
		return nil
	}
	// A RUN'S PAGE STEERS THE PLANNER (roomorch.go). Same box, same enter, same
	// echo of the person's own words on the page they typed them into — the only
	// thing that changes is which door the sentence goes through, because there
	// is no worker in a run to talk to: there is a planner, and it reads steering
	// on its next call.
	if room.orch != nil {
		return a.orchSteer()
	}
	if room.done {
		a.raiseGuard(line, "")
		return nil
	}
	doors, ok := a.taskSteerDoors()
	if !ok {
		a.roomNote(roomUnavailableRefusal.line())
		return nil
	}
	// The worker reads the paste and the room's row keeps the tag (pastechip.go).
	// The chips are spent only once the engine has taken the line: a refusal
	// leaves the words in the box, chips and all.
	words := a.pastesUnfolded(line)
	receipt, err := doors.SteerTask(room.id, words)
	if err != nil {
		// The engine's own sentence, kept: "task 3 is done, not running" and
		// "task 3 has no worker to talk to yet" are different facts, and a
		// surface that flattened them to "could not steer" would be throwing
		// away the half that says what to do about it. It goes on the guard's
		// second row rather than into the room, because it is the reason the
		// question below is being asked.
		//
		// AND THE ENGINE SAYS WHICH KEYS ARE HONEST. A refusal that means the
		// node is still running with no reader inside it withholds revive
		// (session.ErrNobodyToRead); every other refusal — done, gone, never
		// started — is exactly what revive is for.
		if errors.Is(err, session.ErrNobodyToRead) {
			a.raiseBusyGuard(line, err.Error())
			return nil
		}
		a.raiseGuard(line, err.Error())
		return nil
	}
	// A STEERED LINE IS A LINE YOU TYPED, so ↑ brings it back. It is remembered
	// through the same door [app.enter] remembers a message through (recall.go),
	// and for the same reason: the box in here is the box out there, and a
	// sentence that could be recalled in the conversation but not in a room would
	// make the room a different editor wearing the same prompt. It is remembered
	// only once the engine has taken it — the guard above keeps the words in the
	// box, and a recall list holding sentences that went nowhere would be a
	// history of things that did not happen.
	a.remember(line)
	a.pastes = nil
	a.input.reset()
	a.endRecall()
	a.closeLists()
	// A STEERED LINE IS A CORRECTION AND NOT A NEW QUESTION, so it draws as the
	// elbow the conversation draws one as (steerelbow.go) and OPENS NO TURN.
	//
	// A task page is one question — the instruction at the top of it — and
	// everything said on the page after that bends work that is already moving.
	// Drawn as an ordinary message it bumped a counter nobody had opened a turn
	// on, and, worse, it was journaled and replayed as a question: yesterday's
	// correction reopened as a second brief. The chips are not spent here — a
	// room's box sends words, and the tray belongs to the conversation.
	room.collapseThought()
	// AND THE LINE SAYS WHAT THE SENDING DID, in the engine's own words. The
	// crossing to another agent is the fact this clause exists for: the page can
	// stay silent for a long moment afterwards — for as long as the node's
	// current step runs, and longer when it had parked on the pieces it handed
	// out — and silence is what the person would also see if the words had gone
	// nowhere at all.
	//
	// It is CONSUMED at once because that is the whole of what this door
	// promises: [session.Agent.SteerTask] delivers the line to an agent that was
	// listening, and what the node does with it next is the node's turn to take.
	now := a.now()
	a.roomSaid(entry{kind: entrySteer, turn: room.turn, context: a.turnContext(),
		steer: &steerElbow{
			words: line, at: now, consumed: true, landed: now,
			receipt: steerReceiptWords(receipt),
		}})
	if a.farRoomRecord != nil {
		room.keepSteerEcho(words, room.entries[len(room.entries)-1])
	}
	// The two wakeups the clause's fade needs and no ticker, which is [fadeTicks]'
	// whole bargain (steerelbow.go takes the same two for the same reason).
	return tea.Batch(a.edited(), fadeTicks())
}

// steerReceiptWords is the engine's own sentence for what the sending did, drawn
// verbatim. A held line — the task's work is being checked, so nobody read it
// yet and it is on the task's record — has its own words, and this surface does
// not invent a shorter one for it: "delivered" over a line nobody has read would
// be the room telling a person something that is not true.
func steerReceiptWords(receipt session.SteerReceipt) string {
	if words := strings.TrimSpace(receipt.Landing); words != "" {
		return words
	}
	return session.SteerDelivered(receipt.Waiting)
}

// ── the steer guard ─────────────────────────────────────────────────────────
//
// THE WORDS MUST NOT GO SOMEWHERE THE PERSON DID NOT SEND THEM.
//
// A room is the box talking to a node, and a node stops listening — it lands, it
// fails, it is stopped, its worker is gone. Two things a surface can do at that
// moment are wrong in the same way. It can drop the sentence, which loses work
// somebody typed. Or it can quietly send it to the main conversation instead,
// which is worse: the box says "steer <task>" right up until the enter, and a
// message that went to the head model instead of the node is a message the
// person believes a worker read. Neither is a decision this surface is entitled
// to make, so it asks:
//
//	fix the parser is parked — [r] revive and send · [m] send to main · [esc] cancel
//	task 4 is done, not running
//
// esc keeps the words in the box. r and m both spend them, and both say where.
//
// ── WHAT "REVIVE" MEANS, EXACTLY ──
//
// There is no engine door that restarts a finished node, and this file does not
// pretend there is one. Nodes are created by the head model's own tool
// (session's propose_task), which makes the head the only thing on this surface
// that can put a worker back in a room — so revive is a message to the head that
// NAMES the node and carries the person's words as the instruction for it. That
// is the same authority a task always came from, asked the same way, and it is
// the honest reading of the key: the words go to whoever can act on them, and
// the person is told which one that is before they press it.

// steerGuard is one raised question: the words, and why they could not go where
// they were pointed.
type steerGuard struct {
	// title is the node's, as the rail and the room's own placeholder name it —
	// the guard's line opens with it, because "is parked" about an unnamed task
	// is a sentence about nothing.
	title string
	// text is the person's sentence, held here rather than taken out of the box
	// — the box still shows it, and esc leaves it exactly where it was.
	text string
	// why is the engine's own sentence about the node, or empty when the room
	// simply saw its lane close.
	why string
	// busy is a node that is STILL RUNNING with nobody inside to read a line —
	// refused mid-check, or while the work lands (#273). A busy guard offers no
	// revive: the honest keys are m and esc, because "start it again" about work
	// that is minutes from done would race the original with a duplicate.
	busy bool
}

// raiseGuard puts the question up. The room stays open underneath it: the page
// is what the person was reading, and the question is about what to do with a
// line they typed into it.
func (a *app) raiseGuard(line, why string) {
	a.raiseGuardOf(line, why, false)
}

// raiseBusyGuard is the same question about a node that is STILL RUNNING with
// nobody inside to read the line ([steerGuard.busy]). Only the engine can tell
// the two apart — this page's own `done` is its record of a close it may not
// have been told about yet, and reading "still running" off it would withhold
// revive from a node the engine just said was finished (#273's own test,
// TestASteerTheEngineRefusedRaisesTheGuardWithItsReason).
func (a *app) raiseBusyGuard(line, why string) {
	a.raiseGuardOf(line, why, true)
}

func (a *app) raiseGuardOf(line, why string, busy bool) {
	if a.room == nil {
		return
	}
	a.guard = &steerGuard{title: a.room.title, text: line, why: why, busy: busy}
	// The typed lists follow the draft, and the draft is spoken for while the
	// question is up: the same law the approval question states (consent.go).
	a.closeLists()
	a.touch()
}

// dropGuard takes the question down and leaves the draft alone.
func (a *app) dropGuard() {
	if a.guard == nil {
		return
	}
	a.guard = nil
	a.touch()
}

// guarding reports whether the steer guard owns the keyboard.
func (a *app) guarding() bool { return a.guard != nil }

// guardKey routes one keypress while the guard is up, and reports whether it
// took it — which, apart from ctrl+c, is always: three keys answer, and every
// other key does nothing rather than typing into a box whose enter is spoken
// for.
func (a *app) guardKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.guarding() {
		return nil, false
	}
	switch msg.String() {
	case "ctrl+c":
		return nil, false
	case "r":
		// A busy guard has no revive (see [steerGuard.busy]): the key does
		// nothing rather than restarting work that is still running.
		if a.guard.busy {
			return nil, true
		}
		return a.guardSend(true), true
	case "m":
		return a.guardSend(false), true
	case "esc":
		// The words stay in the box. esc here is not "leave the room" — the room
		// is still open under the question — it is "I did not mean to send that
		// yet", and the sentence is exactly where it was.
		a.dropGuard()
		return nil, true
	}
	return nil, true
}

// guardSend spends the sentence on the main conversation, and says so by LEAVING
// THE ROOM: the box is talking to the head from this keystroke on, and a
// placeholder still reading "steer <task>" over a message that went to the model
// would be the lie this whole guard exists to prevent.
func (a *app) guardSend(revive bool) tea.Cmd {
	guard := a.guard
	if guard == nil {
		return nil
	}
	line := guard.text
	if revive {
		line = "The task \"" + guard.title + "\" is no longer running. " +
			"Start it again with this instruction: " + guard.text
	}
	a.dropGuard()
	a.closeRoom()
	a.input.reset()
	a.endRecall()
	a.closeLists()
	a.stick = true
	// The person's own sentence is what goes in the recall list, not the
	// wrapper this surface put around it: ↑ is for getting back what you typed.
	a.remember(guard.text)
	a.dropDraft()
	return a.submit(line)
}

// ── the guard, drawn ────────────────────────────────────────────────────────

// ONE SLOT, TWO QUESTIONS. The stop confirmation (stop.go) is drawn in exactly
// the rows the steer guard is drawn in, and the three functions below are where
// that is arranged: the frame asks the guard how tall it is, what it says, and
// what each of its rows IS for the pointer, and it never learns there are two
// kinds of question down there.
//
// They share rather than stack because they are the same shape of thing — the
// surface holding a keystroke back until it is told whether to act on it — and
// because they can never be up together: a guard is raised by an enter in a
// room's box, and the stop card is raised by a key that is only ever taken over
// an empty one.

// guardHeight is how many rows the question takes: the offer, and the engine's
// reason under it when there is one.
func (a *app) guardHeight() int {
	if n := a.stopHeight(); n > 0 {
		return n
	}
	if !a.guarding() {
		return 0
	}
	if a.guard.why == "" {
		return 1
	}
	return 2
}

// guardMark says what one row of the slot is for the pointer. The steer guard
// answers to no press — it is three keys and nothing else — and the stop card's
// answers are a row somebody can put a finger on.
func (a *app) guardMark(at int) chromeRow {
	if a.stopping() {
		return chromeRow{kind: chromeStop, index: at}
	}
	return chromeRow{}
}

// guardRows draws it, in the question hue the approval block wears and for the
// same reason: this is the surface blocked on a keyboard, and the one thing on
// screen that is blocked on you must not look like the things that are not.
func (a *app) guardRows(width int) []string {
	if rows := a.stopRows(width); len(rows) > 0 {
		return rows
	}
	if !a.guarding() {
		return nil
	}
	parts := []string{
		a.guard.title + roomParkedWord, "[r]", " revive and send · ", "[m]",
		" send to main · ", "[esc]", " cancel",
	}
	if a.guard.busy {
		parts = []string{
			a.guard.title + roomBusyWord, "[m]", " send to main · ", "[esc]", " cancel",
		}
	}
	line := strings.Join(parts, "")
	out := make([]string, 0, 2)
	if ansi.StringWidth(line) > width {
		out = append(out, a.pal.ask(fit(line, width)))
	} else {
		var painted string
		for i, part := range parts {
			if i%2 == 1 {
				painted += a.pal.askBold(part)
				continue
			}
			painted += a.pal.ask(part)
		}
		out = append(out, painted)
	}
	if a.guard.why != "" {
		out = append(out, a.pal.dim(fit("  "+a.guard.why, width)))
	}
	return out
}

// ── the keyboard ────────────────────────────────────────────────────────────

// roomKey is the room's claim on the keyboard, and it is read BEFORE [app.key].
//
// It has to be: esc and enter are input.go's keys and input.go is not this
// slice's file to edit. So the precedence law that file states is restated in
// the guard below rather than relied on — the door (ctrl+c), the question the
// SESSION is blocked on, the three modal overlays and the two typed lists all
// outrank a room, in exactly the order they outrank the draft. Everything this
// does not take falls through untouched, which is what keeps the box a box: a
// person types into it in here the same way they type into it out there.
func (a *app) roomKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	// A JOB'S PAGE IS A CARD, not a room, but it sits at this rung because the
	// update loop already asks here for the full-frame overlay a person is
	// standing in (app.go). The page has no box, so every key is a verb or a
	// scroll, and ctrl+c stays the door.
	if cmd, taken := a.jobPageKeyPress(msg); taken {
		return cmd, true
	}
	if a.room == nil {
		return nil, false
	}
	// THE STATUS SHEET IS ON THIS LIST FOR THE REASON THE SETTINGS PANEL IS, and
	// it was missing from it: it is a FULLSCREEN overlay at the same rung
	// (input.go reads the two one after the other), so while it is up there is
	// nothing of the room on screen for a key to mean anything to — and the two
	// keys this function takes are the two the sheet needs most. Without it, esc
	// closed the room out from under a sheet the person was reading and enter
	// steered the node instead of opening the line under the cursor, which is the
	// door a phone-tier press on the task's model chip now arrives through
	// (statusdeck.go's [app.deckModelRow]).
	//
	// AND HOME IS ON IT FOR THE SAME REASON, and was missing from it. It takes
	// the whole frame (view.go's [app.frame]), so while it is up there is nothing
	// of the room on screen — and every key this function takes is a key home's
	// own foot advertises: esc closed the room out from under the screen a person
	// was reading instead of closing home, enter STEERED THE NODE with the
	// sentence they had typed into home's box, and ↑↓ scrolled a page nobody
	// could see rather than walking the column.
	//
	// AND THE THINKING CHOOSER IS ON IT for the reason the roster puts it on its
	// own list (task.go's [app.railKey]): while five rungs are drawn over the box,
	// the thing a person is standing on is the CONVERSATION'S ladder, so
	// [effortKey] belongs to it and not to the node behind it. One chord, one
	// scope, and the nearest surface is the one on top.
	switch key := msg.String(); {
	case key == "ctrl+c", a.asking(), a.awaitingTask(),
		a.at(pageSettings), a.at(pageTasks), a.at(pageHome), a.deckShowing(), a.pick.open,
		a.roster.open, a.copy.on, a.welcome.open, a.menu.open, a.comp.open,
		a.effPick.open:
		return nil, false
	}
	// THE GUARD IS READ BEFORE THE ROOM, and it is the same rung: it is a
	// question raised by the room's own enter, so the keys it answers with have
	// to outrank the key that raised it. Everything above still outranks both.
	if cmd, taken := a.guardKey(msg); taken {
		return cmd, true
	}
	// AND THE DESIGN'S APPROVAL CHORDS ARE READ UNDER THE GUARD, which is the
	// right order for the one moment both are on screen: the guard is a question
	// raised by a sentence the person just tried to send, and it has to be
	// answered before anything else in the room means anything. These two take
	// only ctrl+k and ctrl+x and let every other key past — a row that swallowed
	// keys would make the box it points at unusable (roomapproval.go).
	if a.roomApprovalKey(msg) {
		return nil, true
	}
	// AND THE FOUR LETTERS THAT DECIDE ABOUT A NODE THAT NEEDS A LOOK, under the
	// guard for the same reason, and only ever over an EMPTY box: the box in here
	// steers the worker, and a letter that decided somebody's work was finished
	// on the first keystroke of a sentence would be unforgivable
	// (tasksettle.go's [app.roomSettleKey] holds every guard `x` has).
	if a.roomSettleKey(msg) {
		return nil, true
	}
	// AND A RUN'S PAGE IS READ BEFORE THE ROOM'S OWN TWO KEYS (roomorch.go),
	// because it has more levels than a room does: esc walks out of a chip's card
	// and out of a nested run before it walks out of the page at all, and enter
	// over an empty box opens the chip under the cursor rather than steering
	// nothing. The recall walk is excluded here for the same reason esc excludes
	// it below — a state the dismiss key cannot dismiss is a trap — and every key
	// the page does not take falls through to the two below.
	if a.room.orch != nil && !a.recalling() {
		if cmd, taken := a.orchKey(msg); taken {
			return cmd, true
		}
	}
	switch msg.String() {
	case "esc":
		// ESC IN HERE IS THE DOOR AND IT IS NEVER A STOP — stop.go's standing law,
		// restated at the keystroke it is about. Out in the conversation esc
		// interrupts the running turn; the analogous act in a room is ending the
		// node, which is not reversible and is therefore always asked first (`x`,
		// and the card). So the two surfaces do NOT converge on this key, and the
		// legend says which of the two meanings is live: while a room is open the
		// hint slot never reads "esc interrupt" (render.go's [app.hintWord]).
		//
		// A recall walk is left first, for the reason input.go leaves it first: a
		// state that could not be dismissed by the dismiss key is a trap, and the
		// room is still one keystroke behind it.
		if a.recalling() {
			return nil, false
		}
		a.closeRoom()
		return nil, true

	// ← IS NOT TAKEN HERE, and that is a resolved collision rather than an
	// omission. The room briefly counted two consecutive ← presses of its own to
	// leave; the arrow grammar that landed beside it says the same thing with one
	// more level in it — one ← steps back a level (the room, then the selection),
	// two inside [navDoubleTap] go home to the live edge — and two definitions of
	// one keypress is the one thing a keyboard cannot have. So ← falls through to
	// input.go, which spends it on [app.navBack] over an empty box and on the
	// caret over a sentence.

	case "enter":
		return a.steer(), true

	case "ctrl+v":
		// HOW HARD THIS NODE THINKS, one step up the ladder — the same chord that
		// moves the install's rung on home at rest and a standing item's on its
		// own card, bound here to the node whose page this is (taskeffort.go).
		// Everything that outranks the room outranks it, because it is read from
		// inside the room's own switch and never above it.
		a.cycleTaskEffort()
		return nil, true

	case "ctrl+b":
		// FREEZE THE ROOM, not the conversation. copymode.go snapshots the
		// transcript's rows, which while a room is open are not the rows on
		// screen — so the snapshot is taken here, from what is actually drawn.
		a.freezeRoom()
		return nil, true

	case "pgup":
		a.roomScroll(-a.scrollPage())
		return nil, true
	case "pgdown":
		a.roomScroll(a.scrollPage())
		return nil, true

	case "up":
		// ↑ MEANS IN HERE WHAT IT MEANS OUT THERE, in the same order (input.go's
		// four meanings): inside a multi-line draft it moves the caret, at the top
		// of the draft it walks the history of what you have typed, and only when
		// there is no history to walk does it scroll the page.
		//
		// IT USED TO SCROLL FIRST, and that made the room's box a different editor
		// from the conversation's — the one place on this surface where a sentence
		// somebody had already typed once could not be brought back and edited. A
		// steered line is now remembered ([app.steer]), so the walk in here reaches
		// exactly the words the walk out there reaches.
		//
		// THE PAGE DID NOT LOSE A SCROLL GESTURE THAT MATTERS. pgup/pgdown page it,
		// the wheel scrolls it (app.go's pointer routing), and ↑ still scrolls on a
		// session with no history at all — which is the same bargain the
		// conversation struck, where ↑ has not been a one-row scroll since the day
		// there was anything to recall.
		if !a.input.onFirstLine() {
			return nil, false // the caret's, in input.go
		}
		if !a.recallBack() {
			a.roomScroll(-1)
		}
		return nil, true

	case "down":
		if !a.input.onLastLine() {
			return nil, false
		}
		if !a.recallForward() {
			a.roomScroll(1)
		}
		return nil, true
	}
	return nil, false
}

// roomHint is the hint slot while a room is open (render.go's [app.hintWord]),
// and it exists because that slot used to LIE in here: with a turn running out
// in the conversation it drew "esc interrupt" over a page where esc leaves the
// room and interrupts nothing. A hint naming a key that does something else is
// the one failure the slot exists to prevent.
//
// IT NEVER NAMES esc. The legend's left end is already carrying that key for as
// long as a room is open ([app.legendLeft]), and a row that said it at both ends
// would be the surface repeating itself in the one place a person reads for the
// next keystroke.
func (a *app) roomHint() string {
	switch {
	case a.guarding() || a.stopping():
		// Both draw their own answers on their own row, directly above the box
		// (see [app.guardRows]). This is the rewind mode's rule: a slot repeating
		// keys that are already on screen is a slot nobody reads twice.
		return ""
	case a.recalling():
		return roomRecallHint
	case a.stopOffered():
		// THE ROOM'S ANSWER TO "HOW DO I STOP THIS". It is the honest counterpart
		// to the conversation's "esc interrupt": the work in here ends through a
		// card and never through the dismiss key (stop.go), so this is the key a
		// person reaching for esc actually wants. It is drawn only while there is
		// something to stop, which is the emptiness law applied to a hint.
		return roomStopHint
	case a.roomSettleAsking():
		// THE ROOM'S ANSWER TO "IT SAYS LOOK IT OVER, NOW WHAT". The node has
		// landed, so nothing above this is live, and the three answers are
		// printed on the foot as well — but the foot is at the far end of a page
		// somebody is reading, and this slot is the one place on the frame a
		// person looks for the next keystroke (tasksettle.go).
		//
		// AND IT IS SPELLED TO THE FRAME. The slot takes a line whole or not at
		// all ([app.legend]), so the full sentence — four cells too long at sixty
		// columns — left the narrowest terminal naming none of the keys that
		// answer the question it was standing on. [roomSettleHintAt] is the
		// ranked prefix of it that fits.
		return a.settleHintAt(a.width, "")
	}
	return ""
}

// freezeRoom hands copy mode the room's own rows. It is the same frozen viewport
// [app.enterCopy] builds — same struct, same cursor, same yank — over a
// different list, because what a person freezes must be what a person is
// reading.
//
// ONE WRINKLE, KNOWN AND SMALL: leaving copy mode rejoins the CONVERSATION's
// live edge ([app.exitCopy] sets stick), so a person who froze a room while the
// transcript was scrolled up loses that scroll. It is the one seam where the
// room does not leave the conversation untouched, and it is left alone because
// the alternative — a room-shaped exception inside copy mode — would put a
// second definition of "what is frozen" in a file whose whole point is that
// there is one.
func (a *app) freezeRoom() {
	if a.copy.on || a.room == nil {
		return
	}
	rows := a.roomRows(a.bodyWidth())
	if len(rows) == 0 {
		return
	}
	height := a.viewHeight()
	snapshot := make([]string, 0, len(rows))
	stripped := make([]string, 0, len(rows))
	owner := make([]int, 0, len(rows))
	for _, r := range rows {
		snapshot = append(snapshot, r.text)
		stripped = append(stripped, ansi.Strip(r.text))
		// The index is into the ROOM's own list, which is the list this snapshot
		// was taken from — that is all "a" needs it to be, since it only ever
		// compares two rows of the same freeze (copymode.go's [app.copyBlock]).
		owner = append(owner, r.entry)
	}
	top := a.roomOffsetFor(len(rows), height)
	a.copy = copyMode{
		on: true, rows: snapshot, text: stripped, owner: owner,
		at: min(top+height-1, len(rows)-1), top: top, mark: -1,
	}
	a.touch()
}

// ── moving between the conversation and the work ────────────────────────────
//
// THE ARROWS ARE THE OTHER DOOR. The rail's click opens a room and esc leaves
// it, which is a complete pair for a person with a mouse and an incomplete one
// for everybody else: the keyboard could get OUT of a room and could only get
// into one through a proposal row that had scrolled away hours ago.
//
// So, over an EMPTY box — the same tier the proposal's options are read at, and
// for the same reason, that there is no caret to move:
//
//	→   forward, into the work: the next running node, or the first one
//	←   back one level: out of the room, into the conversation
//	←←  home: out of everything, at the live edge
//
// The tree is one level deep today. v1's graph has no edges (task.go says so
// where the rail is built), so "back one level" and "home" land in the same
// place from inside a room — and they are still two gestures rather than one,
// because the day a node opens a node the ← that steps out of the child must not
// also be the ← that abandons the parent. What changes when edges arrive is how
// far apart the two answers are, not what either of them means.

// navDoubleTap is how long the second ← has to arrive in. Six hundred
// milliseconds is a deliberate double-tap and not a fast walk backwards: a
// person stepping out of two levels at speed presses the key twice in about a
// quarter of a second, which is the case this window is set wide enough to
// catch and the reason it is not set wider.
const navDoubleTap = 600 * time.Millisecond

// navForward is → over an empty box: into the next running node's room.
//
// It walks the RAIL's order, which is the order the nodes are on screen, so the
// key moves down the column a person is already looking at. From the
// conversation it takes the first running node; from inside a room it takes the
// one after this one and wraps at the end — and does nothing at all when the
// wrap lands back where it started, because a door that shuts on the second
// press of a key whose whole meaning is "forward" would be a door that answers
// the gesture with its opposite.
//
// A session with nothing running does nothing, silently. The alternative is a
// note in the transcript every time somebody taps an arrow key, which is a
// permanent line in the record about a keystroke that meant nothing.
func (a *app) navForward() tea.Cmd {
	// AN ADAPTIVE RUN IS THE FIRST STOP (roomorch.go): → from the conversation
	// opens the run this session has heard from, when there is one and nothing
	// else is open. It is no longer the ONLY door — a run's root and node rows on
	// the roster resolve to the same page (task.go) — but it is the one that
	// needs no aim, which is what makes it the first thing the key does. From
	// inside any room the key goes back to walking the roster, which is what it
	// has always done.
	if a.room == nil && a.orchLive != "" {
		a.openOrchRoom(a.orchLive, "")
		return a.takeRoomPump()
	}
	running := a.runningNodes()
	if len(running) == 0 {
		return nil
	}
	at := -1
	if a.room != nil {
		for i, node := range running {
			if node.id == a.room.id {
				at = i
				break
			}
		}
	}
	next := running[(at+1)%len(running)]
	if a.room != nil && a.room.id == next.id {
		return nil
	}
	a.openRoom(next.id, next.title)
	return a.takeRoomPump()
}

// runningNodes are the roster's nodes that have somebody in them, in THE
// ROSTER'S OWN ORDER — which is the `running` group, newest first, whether or
// not the column is folded to hide it (task.go's [app.railMembers]). The
// forward key walks what the column would show, not what it happens to be
// showing: a fold is about reading, and this is about going somewhere.
//
// A QUEUED NODE IS NOT ONE. It is on the roster — it is going to run — but it
// has no worker yet, so its room is a page with nothing on it and nothing
// coming, and steering it gets the engine's "no worker to talk to yet". A
// forward key that landed there would be a key that mostly opens empty pages.
func (a *app) runningNodes() []*taskNode {
	members := a.railMembers()
	out := make([]*taskNode, 0, len(members[railRunning]))
	for _, node := range members[railRunning] {
		if node.state == session.TaskRunning {
			out = append(out, node)
		}
	}
	return out
}

// navBack is ← over an empty box, and it is where the double-tap is resolved.
func (a *app) navBack() {
	now := a.now()
	double := !a.leftTap.IsZero() && now.Sub(a.leftTap) <= navDoubleTap
	a.leftTap = now
	if double {
		// The pair is spent. A third tap starts a new one rather than counting
		// as the second of another, which is what keeps a held-down arrow from
		// reading as three separate double-taps.
		a.leftTap = time.Time{}
		a.goHome()
		return
	}
	a.stepBack()
}

// stepBack leaves one level: the room first, and the selection after it.
//
// The selection is a level. ↑/↓ over an empty box pick a tool call out of the
// transcript (input.go), and a highlight left behind is a row that enter would
// open — so a person walking backwards out of what they were reading gets the
// highlight taken off before nothing happens at all.
func (a *app) stepBack() {
	if a.room != nil {
		a.closeRoom()
		return
	}
	if a.sel >= 0 {
		a.sel = -1
		a.touch()
	}
}

// goHome is ←← and it is the one gesture that does not care where you are: the
// conversation, no room over it, nothing selected in it, and at the live edge.
//
// It rejoins the BOTTOM, which is the one thing esc out of a room deliberately
// does not do (room.go's header: the transcript is never touched while a room is
// open, so leaving restores the scroll exactly). Home is the gesture for the
// other intention — not "back to what I was reading" but "back to now".
func (a *app) goHome() {
	a.closeRoom()
	if a.sel >= 0 {
		a.sel = -1
	}
	a.stick = true
	a.follow()
	a.touch()
}

// ── the pointer ─────────────────────────────────────────────────────────────

// railPress is the rail's own click, and it is the ONE gesture on this surface
// that needs the column as well as the row: the rail is drawn beside the
// conversation, so a press is the rail's or the body's depending on where it
// landed horizontally.
//
// It reports whether it took the click. A press in the rail's columns that hit
// no node is still the RAIL's — the alternative is a click in empty rail space
// closing the room, which would make the column a person aims at to switch rooms
// the column that throws them out.
//
// A ROW IS A DOOR AND ITS GLYPH CELL IS A FOLD. Anywhere on a node's row opens
// that node's room, which is what every row of this column has always done; the
// one cell that means something else is the STATE CELL of a family root, where a
// press folds the family instead — and that is the same cell the pointer reveals
// a ▾ or ▸ in, so the affordance and the target are the same two columns
// (task.go's [app.railEntryRows]). A folded root's ▸ +N is the other half of it:
// the count is what says there is something hidden, so pressing the count opens
// it.
//
// The press moves the roster's cursor to what was pressed but does NOT take the
// keyboard: clicks focus what was clicked, and the draft is where this surface
// types.
//
// OVER THE BODY IT IS THE OTHER WAY ROUND: the roster has the whole width, so
// the question stops being about x and becomes about y — a press inside the body
// region is the roster's, and the pinned rows above it (the header, the strip)
// are not, because those are drawn by somebody else and answer for themselves
// (view.go's [app.topHeight]).
func (a *app) railPress(x, y int) (tea.Cmd, bool) {
	// THE CLOSED COLUMN'S EDGE IS THE FIRST THING ASKED, and it is the whole of
	// what that strip does: a press anywhere on it brings the roster back, which
	// is exactly ctrl+g (task.go's [railGripCols] says why the strip is there).
	// It is asked before [app.railAt] because that question is about a column
	// which, in this state, is not on the frame at all.
	if a.railGripAt(x, y) {
		a.railStow(false)
		return nil, true
	}
	if !a.railAt(x, y) {
		return nil, false
	}
	// THE SEAM IS THE COLUMN'S HANDLE. It answers before rows do because the
	// same two cells run through node rows and the footer alike: grabbing the
	// handle changes the column, never opens whatever happens to sit behind it.
	if a.railSeamAt(x, y) {
		a.railWiden(!a.railWide)
		return nil, true
	}
	line, ok := a.railLineAt(y)
	if !ok {
		return nil, true
	}
	// THE FOOTER'S ONE OFFER IS PRESSABLE, because a hint that names a key and
	// cannot be pressed is a hint that is only for one of the two hands
	// (task.go's [app.railFootRows]).
	// AND THE LAST LINE IS THE COLUMN'S DOOR, for the same reason one rung up: a
	// line that names ctrl+g and cannot be clicked is an affordance for one of the
	// two hands (task.go's [railStowHint]).
	if line.stow {
		a.railStow(true)
		return nil, true
	}
	// AND THE DOOR ONTO THE TASK PAGE IS THE THIRD OF THEM, on the same terms: it
	// names a chord, so it has to answer to the hand that does not type chords
	// ([taskSheetPastHint], taskview.go). It leaves the column exactly as it is —
	// the page is a place you go and come back from, not a state the column
	// enters.
	if line.more {
		return a.showPage(pageTasks), true
	}
	if line.hint {
		a.railWiden(!a.railWide)
		return nil, true
	}
	// AND THE MARGIN'S OWN LINES ARE ITS OWN: the `+` row at the foot of each
	// section, which types its slash word into the draft, and a standing order's
	// row, which opens the page on that order (margin.go). They are asked before
	// the entries below for the reason the footer's lines are — they belong to no
	// node, and a question about which node is under the pointer would answer
	// about the space beside them.
	if cmd, took := a.marginPress(line); took {
		return cmd, true
	}
	e, ok := a.railEntryAt(y)
	if !ok || e.node == nil {
		return nil, true
	}
	a.railWhere = railSpotOf(e)
	at := x - a.railLeft() - ansi.StringWidth(railSeam)
	switch {
	case e.root && line.badge.holds(at):
		a.railSetOpen(e.node, true)
	case (e.root || a.railTucks(e)) && line.glyph.holds(at):
		// THE GLYPH CELL IS THE DISCLOSURE ON BOTH KINDS OF ROW: a family root
		// folds its subtree, and a row that has landed folds its own block back
		// under itself (task.go's [app.railSaysMore]). The set that LIGHTS under
		// the pointer is the set that acts, which is hover.go's own law — both
		// halves ask [app.railTucks].
		a.railToggle(e.node)
	default:
		if e.node.run != "" {
			a.openOrchRoom(e.node.run, e.node.node)
		} else {
			a.openRoomFor(e.node.id, e.node.title)
		}
	}
	return a.takeRoomPump(), true
}

// railSeamAt reports whether a pointer is on the visible two-cell handle.
func (a *app) railSeamAt(x, y int) bool {
	if !a.railAt(x, y) || a.railFull() {
		return false
	}
	left := a.railLeft()
	return x >= left && x < left+ansi.StringWidth(railSeam)
}

// railAt reports whether a pointer at these coordinates is over the roster. It
// is the press's guard and the hover's alike (hover.go), because a column that
// answered a click it would not light under the pointer is a column that
// disagrees with itself about what it is.
func (a *app) railAt(x, y int) bool {
	switch {
	case a.railFull():
		top := a.bodyTop()
		return top >= 0 && y >= top && y < top+a.viewHeight()
	case !a.railShowing() || x < a.bodyWidth():
		return false
	}
	return true
}

// railLeft is the screen column the roster's own lines start at: the frame's
// left edge where it is drawn over the body, and the far side of the
// conversation where it is a column.
func (a *app) railLeft() int {
	if a.railFull() {
		return 0
	}
	return a.bodyWidth()
}

// railHoverNode is which node the pointer is over, or nil (hover.go).
func (a *app) railHoverNode(x, y int) *taskNode {
	if !a.railAt(x, y) {
		return nil
	}
	return a.railNodeAt(y)
}

// ── THE FOCUS HEADER ────────────────────────────────────────────────────────
//
//	─ ⠙ main ▸ Fix the nil-map crash · running · 2m12s · $0.04 ──── esc/←← main ─
//
// A ROOM USED TO LOOK LIKE THE CONVERSATION. Same rows, same hues, same box
// underneath, and the only two things saying otherwise were a word in the legend
// and a placeholder in the box — both of which are read once and then stop being
// read. A person who walked into a node, scrolled, and looked up two minutes
// later had nothing on screen telling them that the sentence they were about to
// type was going to a worktree somewhere else.
//
// So the room pins ONE line at the top of the body region, and it is the only
// thing on this surface drawn in the accent that is not the person's own words:
// WHERE YOU ARE (the trail), WHAT IT IS DOING (the state, its clock, its spend),
// and HOW YOU LEAVE. It is pinned rather than scrolled for the reason a status
// line is pinned — a fact that scrolls away is a fact that is only true at the
// top of the page — and it is one line because a room is a place you are looking
// THROUGH, not a page about a node.
//
// THE TRAIL IS A BREADCRUMB and it always names the root: "main ▸ <node>" one
// level down, "main ▸ parent ▸ child" when a node's page grows a door into the
// node it spawned. The root is on it even at one level deep because the trail's
// job is to say what this page hangs off, and "main" is the one name this
// surface has for the conversation itself.

// roomHead is the pinned line, or "" when there is no room open and nothing to
// pin. It is drawn by the frame (view.go), which is the only thing that knows
// where the top of the body region is.
// THE ✕ RIDES THE RIGHT END, AFTER THE WAY OUT (stop.go). The header already
// ends in the two things a person needs from a page they are standing in — how
// to leave it, and, now, how to stop what is in it — and they are in that order
// because leaving is free and stopping is not.
//
// IT DEGRADES BEFORE THE BACK WORD DOES. The right label is tried at three
// strengths, and the middle one keeps the ✕ alone: the key that leaves is
// printed on the legend at the bottom of the frame and known by everybody who
// has ever used a terminal, while the button is the only thing anywhere on the
// surface that ends work with a pointer. So the mark outlives the microcopy —
// the same ladder [app.legend] walks, spending the recoverable thing first.
// roomHeadFloor is the narrowest frame that gets a pinned header at all: under
// it there is not a trail and a way out's worth of room, and the row would be an
// ellipsis. It is stated once because the kin rows under the header stand on it
// too — a header region whose two halves disagreed about the floor would be rows
// the geometry counted and the frame did not draw ([app.roomKinRows]).
const roomHeadFloor = 12

func (a *app) roomHead(width int) string {
	// [app.headHeight] is what the geometry budgeted for this row, and it is
	// asked rather than second-guessed: a header the frame drew on a short
	// terminal that the scrolling had not subtracted would push the room's last
	// row under the input box.
	a.roomStop = hudSpan{}
	if a.headHeight() == 0 || width < roomHeadFloor {
		return ""
	}
	left := a.roomHeadWord(width)
	mark := a.roomStopWord()
	// THE ✕ BRIGHTENS UNDER THE POINTER, and it is brightened HERE rather than
	// spliced into the finished line: [app.legendLine] paints the right label as
	// one piece and the mark is the last thing in it, so ink written into the label
	// lands on the mark's own cells and the piece after it is painted separately
	// anyway. It is a step up from the dim the label rests in, which is the model
	// segment's own answer to a label that is also a control (render.go's
	// [app.paintIdentity]) — the row is one line at the top of the frame, not a row
	// of a list, and a highlighted rectangle round one glyph would be the one boxed
	// thing on a surface with no boxes. The width is unchanged, so every attempt
	// below still fits exactly as it did.
	shown := mark
	if mark != "" && a.hoveringRoomStop() {
		shown = a.pal.ink(mark)
	}
	attempts := []string{roomBackWord, ""}
	if mark != "" {
		attempts = []string{roomBackWord + roomStopSep + shown, shown, roomBackWord, ""}
	}
	for _, right := range attempts {
		line, ok := a.legendLine(left, right, width, a.pal.accent)
		if !ok {
			continue
		}
		// The mark is the LAST thing in the right label, and [app.legendLine]
		// closes with one space and one rule cell after it — so its columns are
		// arithmetic rather than a second layout, whichever attempt fitted.
		if mark != "" && strings.HasSuffix(right, shown) {
			cols := ansi.StringWidth(mark)
			a.roomStop = hudSpan{from: width - 2 - cols, to: width - 2}
		}
		// AND THE ROW ITSELF TAKES THE BACKGROUND STEP, because the row itself is
		// the control: everything on it is about leaving, and [app.roomBackPress]
		// takes a press anywhere along it. The ✕ never lights with it — the two are
		// different hovers and the pointer can only be on one of them — so the band
		// is never the surface offering "leave" over cells that end work.
		if a.hoveringRoomBack() {
			line = a.pal.cursor(line, width)
		}
		return line
	}
	return a.pal.accent(fit(left, width))
}

// roomBackPress answers a press on the pinned header, and reports whether it
// took it. The header IS the way out for the pointer.
//
// THE WHOLE ROW IS THE TARGET, not just the "esc/← main" at its right end. The
// row is one line tall and about nine cells of it are the microcopy; asking a
// person to land a pointer on those nine is asking them to aim at a label, and
// the two things that share this row — the trail and the way out — are both
// about leaving. The ✕ is the exception and it is claimed one rung earlier
// (stop.go's [app.stopMarkPress]), because ending work and leaving the page you
// were watching it on are opposite gestures and the expensive one wins the cells
// it is drawn on.
//
// THE KIN ROWS UNDER IT ARE NOT PART OF THIS. They are dim telemetry about the
// node's family ([app.roomKinRows]), and a press on a fact is not a press on a
// door — it does nothing, exactly as a press on any other row that answers to
// nothing does ([app.press]).
//
// It is read from the frame's OWN row numbering — the header is the first row of
// a room's frame, always, because [app.view] draws it first and the geometry
// charges [app.headHeight] for it — rather than through [app.chromeAt], which
// resolves the block at the BOTTOM of the window and has never had a row up here
// to answer for.
func (a *app) roomBackPress(y int) bool {
	if !a.roomBackAt(y) {
		return false
	}
	a.closeRoom()
	return true
}

// roomBackAt is that same test with nothing done about it, so the pointer can ask
// what the press asks and the row can light on exactly the cells a click acts on
// (hover.go's law). The ✕ is claimed one rung earlier and never reaches here
// (stop.go's [app.stopMarkAt]).
func (a *app) roomBackAt(y int) bool {
	return a.roomOpen() && a.headHeight() != 0 && y == 0
}

// ── THE HEADER IS THE INSTRUMENT ────────────────────────────────────────────
//
// roomHeadWord is the header's left, and it carries THE JUDGMENT AND
// ACCOUNTABILITY ACTS WHOLE so that the transcript below it does not have to:
// which node this is, what it is doing, how long it has been at it, what it has
// cost, how many calls it has made, and — where it is running — the vaguest
// true sentence about what is happening right now.
//
// WHY THEY ARE HERE AND NOT DOWN THE PAGE. A person comes to a task to steer
// and to check. The check is one glance, and a glance is a fixed number of
// cells at the top of the frame — so a room's numbers gather in one line
// instead of dribbling down a scroll that has to be read to be summed
// (lens.go's [receiptsHeader]; the conversation keeps its per-turn receipts,
// which is the opposite posture and the right one out there).
//
// EVERY SEGMENT IS DROPPED WHEN NOBODY HAS PUBLISHED IT — THE EMPTINESS LAW,
// PER SEGMENT. A queued node has no clock, an unpriced one no cost, a node that
// has called nothing no count, and a node nobody has dialled no rung. That is
// the reason the turn footer drops its own fields (timestamps.go): a figure
// that is zero is a figure nobody measured, and `$0.00 · 0 tool calls` is the
// row spending its scarce cells saying nothing twice.
//
// AND IT DEGRADES BY WHAT IT IS FOR, on the fitter every list on this surface
// already uses (rowfit.go). THE IDENTITY IS WHOLE OR THE ROW IS POINTLESS: the
// mark and the trail take every cell they ask for before a fact gets one,
// because a person on a narrow terminal is first of all working out which page
// they are on. The facts behind it are a RANKED PREFIX — the first one that
// will not fit ends the line and nothing later is skipped forward into the gap
// — so a narrow header says the same ranked things a wide one does, with the
// tail missing rather than a different tail.
//
// It is built PLAIN, without paint, because the whole line is painted once by
// [app.legendLine]: a hue nested inside a hue ends at the inner one's reset, and
// the rest of the line would fall back to the terminal's default mid-sentence.
func (a *app) roomHeadWord(width int) string {
	// A RUN'S PAGE ANSWERS FOR ITS OWN HEADER (roomorch.go): the facts under it
	// are a node's — a state, a clock, a spend — and a run has none of them.
	// What it has instead is a tank, and the tank is the fact that cannot be left
	// off this line.
	if a.orchOpen() {
		return a.orchHeadWord(width)
	}
	node := a.roomNode()
	name := a.roomMark(node) + " " + a.roomTrail()
	room := max(width-roomHeadFurniture, 0)
	if node == nil {
		// A room on a node this surface has had no update for. The trail is still
		// true and nothing else is, which is exactly what gets said.
		return fit(name, room)
	}
	// LAW 1, AND IT NOW HAS SOMETHING TO SPEND. The name arrives WHOLE — the
	// engine's own title for the work, uncut ([taskTitleOf]; until this wave it
	// was cut to three words before any width was known, so this header at a
	// hundred and sixty columns named the work no better than a twenty-four-cell
	// rail row did) — and the identity takes every cell it asks for before a fact
	// gets one. A name that had to be CUT takes the whole line: an ellipsis in it
	// has already spent the one thing the row was drawn to say, and a spend
	// figure beside it would be a second loss.
	if ansi.StringWidth(name) > room {
		return fit(name, room)
	}
	tail := rowTail(a.roomHeadFacts(node), room-ansi.StringWidth(name)-len(rowSep))
	if tail == "" {
		return name
	}
	return name + rowSep + tail
}

// roomHeadFurniture is what [app.legendLine] spends on the header's own rule
// before the words start: the lead `─ `, the space after the left label, and the
// one fill cell that line refuses to draw without. It is subtracted here so the
// fitter is measuring the cells the words actually have, rather than fitting to
// the frame and letting the legend fall back to a cut.
const roomHeadFurniture = 4

// roomHeadFacts is the instrument's ranked prefix: what a person checking on
// work reads, in the order they read it.
//
// THE ORDER IS THE ARGUMENT. The verb is first because "is it going right" is
// the question the visit is for. Elapsed and spend are next because they are
// the two figures nobody can recover by looking at the page. The call count is
// the size of what happened, and the live line is the one thing on the row that
// moves. The model and the rung come last together: they are settings somebody
// made before the work started, not news, and they are the first thing a narrow
// frame can afford to lose.
func (a *app) roomHeadFacts(node *taskNode) []rowField {
	work := roomWorkOf(a.roomEntries())
	return []rowField{
		rowSay(a.roomStateWord(node)),
		rowSay(a.roomClock(node)),
		rowSay(a.roomSpend(node)),
		roomCallField(work.calls),
		rowSay(a.roomLiveWord(node, work)),
		rowSay(strings.TrimSpace(node.model), modelBase(strings.TrimSpace(node.model))),
		rowSay(a.taskEffortClause(node)),
	}
}

// roomCallField is HOW MUCH WORK THIS IS, counted. It is the chip's own grammar
// (workfold.go's [app.workfoldLabel]) so that the header and the chips under it
// count in one vocabulary, with a shorter spelling for a narrow frame.
//
// A NODE THAT HAS CALLED NOTHING SAYS NOTHING. The emptiness law, per segment:
// `0 tool calls` is a measurement of nothing dressed as a measurement.
func roomCallField(calls int) rowField {
	if calls <= 0 {
		return rowSay()
	}
	if calls == 1 {
		return rowSay("1 tool call", "1 call")
	}
	return rowSay(itoa(calls)+" tool calls", itoa(calls)+" calls")
}

// roomLiveWord is THE LIVE LINE: what is happening on this node right now, and
// nothing when the honest answer is that nobody knows.
//
// THE VAGUEST TRUE SENTENCE IS WHAT IS LEFT WHEN NO BETTER ONE IS KNOWN, which
// is the conversation's own ladder read downwards (render.go's [stillWorking]).
// A call in flight is the specific answer and it wins; past that, a page that
// has not moved for ten seconds says so, because from the outside a retry and a
// long tool-free think are indistinguishable from a hang and "still working" is
// true of both.
//
// IT NEVER REPEATS THE VERB BESIDE IT. The state word already says `designing`
// or `awaiting your look` where the engine published one ([app.roomStateWord]),
// and one line saying the same thing twice is a line read twice to learn once.
//
// A NODE THAT IS NOT RUNNING HAS NO LIVE LINE AT ALL. What a finished node is
// doing is nothing, and the surface does not spend a segment saying so.
func (a *app) roomLiveWord(node *taskNode, work roomWork) string {
	if node.state != session.TaskRunning {
		return ""
	}
	word := work.running
	if word == "" && !work.last.IsZero() && a.now().Sub(work.last) >= stillWorking {
		word = strings.TrimPrefix(stillWorkingWord, " · ")
	}
	if word == a.roomStateWord(node) {
		return ""
	}
	return word
}

// roomWork is what the page's own rows say about the work: how many calls have
// CLOSED, which one is in flight, and when anything last moved.
//
// It is one walk answering three questions because all three are read on the
// same frame by the same line, and three walks over the same slice is the same
// arithmetic done three times. It reads the ENTRIES — the rows a person can see
// — so the header cannot claim a call the page does not show.
type roomWork struct {
	calls   int
	running string
	last    time.Time
}

func roomWorkOf(es []entry) roomWork {
	var out roomWork
	for i := range es {
		e := &es[i]
		if !e.ended.IsZero() && e.ended.After(out.last) {
			out.last = e.ended
		}
		if !e.began.IsZero() && e.began.After(out.last) {
			out.last = e.began
		}
		if callClosed(e) {
			// THE SAME "FINISHED" THE AMBIENT COUNTS USE (app.go's [callClosed]),
			// so the figure in the header and the figures in the Σ segment cannot
			// be counting two different things.
			out.calls++
			continue
		}
		if e.kind == entryTool && e.status == toolRunning {
			// THE NEWEST ONE IN FLIGHT, because a row that named the oldest would
			// go stale while the work moved on under it.
			out.running = e.tool
		}
	}
	return out
}

// roomEntries is the open page's blocks, or none. It is a door rather than a
// field read because the header is drawn for a run's page too, and a run has a
// graph where a node has a transcript.
func (a *app) roomEntries() []entry {
	if a.room == nil || a.room.orch != nil {
		return nil
	}
	return a.room.entries
}

// roomKinRowCap is how many rows the kin block may take under the header. Three
// is the whole of the family a room can have something to say about — who asked
// for this work, and the five pieces it handed out (session's taskFanLimit) laid
// along one wrapped sentence — and it is a CAP rather than a budget because
// these rows are charged to the page under them: a header that grew with the
// family would take the transcript a person opened the room to read.
const roomKinRowCap = 3

// roomKinNameFloor is the least of a relative's name worth drawing. Under it
// the name is an ellipsis with a letter in front of it, so the row keeps that
// many cells and lets [railWrap] take the overflow onto the next row rather
// than drawing a fragment.
const roomKinNameFloor = 8

// roomKinName is one relative's name in the cells this row can spare it.
func roomKinName(name string, room int) string {
	if room < roomKinNameFloor {
		room = roomKinNameFloor
	}
	return fit(name, room)
}

// roomKinRows is the pinned header's second region: WHERE THIS NODE SITS IN ITS
// FAMILY, in at most [roomKinRowCap] dim rows under the accent line.
//
// THE ENGINE HAS ALWAYS MODELLED THIS AND THE PAGE NEVER SAID IT. A node carries
// who spawned it and what it spawned (session's TaskNotice.Parent, and the
// buckets task.go's [app.railKin] pours them into), and the roster draws its
// whole tree from exactly that — so a person who walked INTO a piece of a
// recursive task could not see, from inside it, that it was a piece of anything
// or that anything was running underneath it. These rows are the tree's own
// data said in words, on the one page where the tree shape is not on screen.
//
// NO NEW ENGINE STATE AND NO SECOND SOURCE: it reads [app.railKin], which is the
// same function the column's forest is grown from, so a family that draws one
// way on the rail cannot read another way here.
//
// WHAT IT WAITS ON IS NOT ON THESE ROWS, AND THAT IS NOT AN OMISSION. The accent
// line above already spends its state word on "waits: <title>" for a node held
// behind a prerequisite ([app.roomStateWord]), and the same sentence twice in
// one header is a header read twice to learn one thing.
//
// It is DIM, INDENTED, AND UNLABELLED, which is the whole of its styling: this
// is telemetry about the page rather than a second header, and v1's column is
// the reference — restrained, no border, no frame of its own (internal/tui).
func (a *app) roomKinRows(width int) []string {
	// A RUN'S PAGE IS ALREADY ITS OWN FAMILY TREE (roomorch.go): the graph is
	// drawn there, node by node, with every prerequisite an edge — so a sentence
	// about kin would be the picture read out loud beside the picture.
	if a.room == nil || a.room.orch != nil || width < roomHeadFloor {
		return nil
	}
	// THE SAME LADDER THE BREATHING ROOM STANDS ON (view.go's
	// [app.breathingRows]). These rows cost the body its rows, so they are spent
	// only where there is body to spend them from: the window tall enough to
	// afford a second blank above the draft is the window tall enough to be told
	// where this work sits. It is asked as a question of the existing ladder
	// rather than written as a second height, because a floor stated twice drifts.
	if a.breathingRows() < 2 {
		return nil
	}
	node := a.roomNode()
	if node == nil {
		return nil
	}
	kids, byKey := a.railKin()
	// THE ROWS ARE A BUDGET AND THE NAMES ARE FITTED TO IT. These lines are dim
	// telemetry with a hard cap of [roomKinRowCap] rows, so a relative's name
	// that arrives whole (taskident.go's [taskTitleOf]) is cut HERE, where the
	// width is known — the alternative is what a name spilled over the cap
	// actually looks like: the block wrapping to three rows and the last one
	// ending on a bare `—` with the state word cut off the bottom of it.
	inner := width - ansi.StringWidth(roomKinIndent)
	var lines []string
	// WHO ASKED FOR THE WORK, AND IT IS NOT A DEPENDENCY — session's
	// task_contract.go states that difference in those words, and this line is
	// the only place on the surface that says the parent out loud rather than
	// drawing it as an elbow. A parent this surface has had no update for is left
	// UNSAID rather than named as an id, which is the rule [app.railWaits]
	// already applies at the other end of the family: "part of: 7" has told a
	// person nothing.
	if up := byKey[node.ParentID()]; up != nil && up != node {
		lines = append(lines, roomKinUnderWord+roomKinName(up.title, inner-ansi.StringWidth(roomKinUnderWord)))
	}
	// AND WHAT THIS WORK HANDED OUT, each piece with the state word it wears
	// everywhere else on the surface. The order is [app.railKin]'s, which is the
	// order the session met them — the one order a family is allowed to use,
	// because any other moves a row a person is watching for a reason they
	// cannot see.
	var spawned []string
	// Each piece gets an EQUAL SHARE of what is left after the lead and the
	// separators between them, less its own state word: the names are the
	// identities on this line and the state words the facts, and a share is what
	// keeps one long name from spending the row a sibling was going to use.
	if pieces := kids[stripKey(node)]; len(pieces) > 0 {
		room := inner - ansi.StringWidth(roomKinSpawnedWord) -
			(len(pieces)-1)*ansi.StringWidth(railSep)
		for _, kid := range pieces {
			word := a.roomKinWord(kid)
			share := room/len(pieces) - ansi.StringWidth(roomKinStateSep) - ansi.StringWidth(word)
			spawned = append(spawned, roomKinName(kid.title, share)+roomKinStateSep+word)
		}
	}
	if len(spawned) > 0 {
		lines = append(lines, roomKinSpawnedWord+strings.Join(spawned, railSep))
	}
	// NOTHING TO SAY IS NOTHING DRAWN. A task with no parent has no parent line
	// and a task that spawned nothing has no spawned line — a room on a flat task
	// is the one pinned row this surface has always drawn, unchanged, and the
	// header does not grow an empty shelf to hold a fact nobody has (the
	// emptiness law).
	if len(lines) == 0 {
		return nil
	}
	out := make([]string, 0, roomKinRowCap)
	for _, line := range lines {
		// THE SENTENCE WRAPS ON ITS SPACES and is cut at the cap, exactly as the
		// rail's under-block is (task.go's [railWrap] and railUnderRows): a title
		// broken mid-word is a title nobody can match against the roster.
		for _, part := range railWrap(line, inner) {
			if len(out) == roomKinRowCap {
				return out
			}
			out = append(out, a.pal.dim(roomKinIndent+part))
		}
	}
	return out
}

// roomKinWord is a CHILD's state on the handed-out line: [app.roomStateWord]'s
// answer about that child, except that one held behind a prerequisite says only
// the one word the column already says about it.
//
// THE DEPENDENCY SENTENCE BELONGS TO THE PAGE YOU WOULD OPEN TO ACT ON IT. A row
// reading "handed out: draft — waits: fetch the RFCs · review — running" is one
// line carrying three tasks' business, and the task it is actually about is the
// one it says least about. What this line owes a person is which pieces exist
// and which of them are still moving; what a piece is behind is on its own row
// in the roster and in its own header the moment they walk in.
//
// AND THAT ONE WORD IS `parked` AND NOT `queued`. "queued" says a scheduler will
// get to this child, and nothing is coming: its prerequisite is the piece of work
// whose room this is, sitting there waiting on the person reading this very page.
// The column has called these children `parked` the whole time
// ([railGroupWords], [railParked] — "admitted and BLOCKED"), so the word is read
// out of the column's own table rather than spelled a second time here.
func (a *app) roomKinWord(node *taskNode) string {
	if node.state == session.TaskQueued && !node.stopped && a.railWaits(node) != "" {
		return railGroupWords[railParked]
	}
	return a.roomStateWord(node)
}

// roomNode is the node the open room is about, or nil when this surface has
// never had an update for it.
func (a *app) roomNode() *taskNode {
	if a.room == nil {
		return nil
	}
	return a.tasks[a.room.id]
}

// roomMark is the node's state in one cell, UNPAINTED — [app.railGlyph]'s glyph
// without its hue, because the header wears one hue for its whole length.
//
// It is the roster's own cell ([app.taskStateMark]) and not a second table. The
// copy that used to live here had drifted: it knew the refusal mark but neither
// the stop's ⊘ nor the halt's !, so a node a person stopped wore a failure's
// cross on its own page and the roster's ⊘ one keypress away.
func (a *app) roomMark(node *taskNode) string {
	if node == nil {
		return a.linearMark(glyphQueued, glyphQueuedASCII)
	}
	return a.taskStateMark(node)
}

// roomTrail is the breadcrumb: the root, then one step per room walked into
// without coming back out.
//
// The path is one deep today, because the only door into a room is the rail and
// the rail is a flat list of the session's nodes — walking from one row to
// another is a step SIDEWAYS, and [app.openRoomFor] treats it as one. The trail
// is written over a path rather than over the open room so that the day a node's
// own page grows a door into the node it spawned, the breadcrumb is already the
// thing on screen.
func (a *app) roomTrail() string {
	trail := roomCrumbRoot
	for _, step := range a.roomPath() {
		trail += roomCrumbSep + step
	}
	return trail
}

// roomPath is the titles of the rooms between the conversation and the page on
// screen, outermost first.
func (a *app) roomPath() []string {
	if a.room == nil {
		return nil
	}
	return []string{a.room.title}
}

// roomStateWord is what the node is doing, in the engine's own vocabulary where
// it has one (task.go's merge words).
func (a *app) roomStateWord(node *taskNode) string {
	switch node.state {
	case session.TaskRunning:
		// A NODE A PERSON HAS ENDED IS STOPPING, AND IT OUTRANKS EVERY PHASE
		// BELOW. This is the room's copy of the conversation's own law (app.go's
		// [stoppingWord]): between the card's "stop it" and the engine moving the
		// node there is a real window — the child's context is cut and the child
		// is winding up — and for the whole of it this line read "working" about
		// work the person had just ended, which is the one word on the page they
		// know to be wrong. The word is the same word for the same reason the
		// working word is shared: a person who has learned what it means out in
		// the conversation has learned it here too.
		//
		// The SPINNER beside it deliberately keeps turning, which is not a
		// contradiction but the other half of the honesty ([session.ProjectTask]
		// keeps that law now: a stop still going through is still running): out in
		// the conversation the person
		// is sitting in front of the turn and nothing should move once they have
		// stopped it, while a node is work going on somewhere else that really is
		// still going on — and the mark that says "landed" is owed to the landing
		// and to nothing earlier.
		if node.stopped {
			return stoppingWord
		}
		// A NODE IN A NAMED PHASE SAYS THE PHASE, and it outranks both of the
		// clauses below. "designing" and "awaiting your look" are what this work
		// IS at this moment (session's TaskNotice.Doing) — a header that said
		// "working" over a card waiting on the person would be the one line on the
		// page spent on the least specific thing on it.
		if node.doing != "" {
			return node.doing
		}
		// AND SO DOES A NODE THAT IS NOT ITS OWN WORKER RIGHT NOW. A check
		// reading what the work left, and a round closing what the check found,
		// are minutes each and the header said "working" through all of them —
		// which is the one line on the page a person watching a run that has gone
		// quiet actually reads (taskphase.go says what the silence cost). It
		// outranks the gap clause below because it says the gap's own round and
		// how far through it is; the finding itself is on the rail's row under the
		// node, and the header has one line and spends it on the state.
		if word := taskPhaseLine(node); word != "" {
			return word
		}
		// A NODE CLOSING A GAP IS FINISHING, AND THE HEADER SAYS SO. It is still
		// running — the engine has not moved it and neither does this — but a
		// person standing in the room of work that is nearly home is owed the
		// difference between "this is under way" and "this is being tied off",
		// and it is one word (task.go's [taskFinishingWord]). What is being tied
		// off is on the rail's own row under the node; the header has one line and
		// spends it on the state.
		if node.mending != "" {
			return taskFinishingWord
		}
		// AND A NODE WHOSE CALLS ARE BEING PACED IS WAITING, in the same one word
		// the rail spends on it (task.go's [taskHeldWord]). The header is the
		// line a person reads to find out why nothing has moved for a minute, and
		// "working" is the answer that sends them looking for a fault that is not
		// there: the node is running and the wire is full. The reason itself is on
		// the rail's own row under the node; the header has one line and spends it
		// on the state.
		if node.waiting != "" {
			return taskHeldWord
		}
		// The word the status line uses for a session that is working, said about
		// a node for the same reason: a person who has learned what "working"
		// means on this surface has learned it here too.
		return stateWorking.String()
	case session.TaskQueued:
		if node.stopped {
			// Stopped before it started, and still queued for the instant between
			// the key and the landing.
			return taskStoppedByPerson
		}
		// THE DEPENDENCY OUTRANKS THE HOLD HERE TOO, for the reason the rail
		// states in full ([app.railUnder]): a named prerequisite is work a person
		// can act on and a full cap is a queue that clears itself.
		if waits := a.railWaits(node); waits != "" {
			return "waits: " + waits
		}
		if node.waiting != "" {
			return taskHeldWord
		}
		return roomQueuedWord
	case session.TaskFailed:
		// A PERSON ENDING WORK IS NOT A FAILURE, and the header is where that
		// difference is read: "failed" sends somebody looking for a fault, and
		// the fault is that they pressed stop (session's TaskNotice.Stopped).
		if node.stopped {
			return taskStoppedByPerson
		}
		if refused(node.ending) {
			return taskRecordStoppedWord
		}
		return roomFailedWord
	case session.TaskUnverified:
		// NOT THE MERGE SENTENCE, for the reason the rail states in the same words
		// (task.go's [app.railUnder]): an unverified node wears session's
		// "aborted" merge exactly as a stopped one does, so the switch below said
		// "stopped" about work that ran to the end. What it is waiting for is a
		// person, and the header says what the card and the rail already say
		// (task.go's [taskUnverifiedWord]).
		return taskUnverifiedWord
	}
	// WHERE THE WORK LANDED, IN A PERSON'S WORDS. It is one table lookup and not
	// a switch with a fall-through, because the fall-through was drawing the
	// engine's own token: a task in a folder with no repository publishes
	// "inplace", and this line said `inplace` to somebody who has never read
	// internal/session (task.go's [mergeScreenWords] holds the table and the
	// argument). A node the engine published no merge word for is simply done.
	if word := mergeScreenWord(node.merge); word != "" {
		return word
	}
	return roomDoneWord
}

// The three words the header has that nothing else on this surface says.
const (
	roomQueuedWord = "queued"
	roomDoneWord   = "done"
	roomFailedWord = "failed"
)

// roomClock is the node's age: counting up while it runs, frozen at what the
// update that ended it reported.
func (a *app) roomClock(node *taskNode) string {
	if node.state == session.TaskRunning && !node.began.IsZero() {
		return countUpWord(a.now().Sub(node.began))
	}
	return countUpWord(node.elapsed)
}

// roomSpend is what this node has cost, or "" when nobody has published a price
// (session's TaskNotice.CostUSD says why zero is not an answer).
func (a *app) roomSpend(node *taskNode) string {
	if node.cost <= 0 {
		return ""
	}
	return dollars(node.cost)
}

// roomChip is the identity cluster while a room is open: the node's mark and its
// title, in the accent, and the telemetry beside it still the SESSION's — the
// room is a view over one body region, not a second session, and a status line
// that re-pointed the cost and the context at a node would be quoting figures
// nobody is measuring.
//
// It replaces a cluster that read "task · <title>", which on a node this surface
// had no title for read "task · task 7" — a place named after its own id twice.
// The chip is the same object the header pins at the top of the page, said once
// more at the bottom, so the two ends of the frame agree about where you are.
// room is the cells the cluster may spend, and a room of zero or less is NO
// BOUND AT ALL — the reading every caller that is not laying the status row out
// wants ([app.identity], and the deck, which fits row 1 to its own width after
// it has the telemetry beside it).
func (a *app) roomChip(room int) string {
	if a.room == nil {
		return ""
	}
	// A RUN'S PAGE WEARS THE RUN'S MARK (roomorch.go). Without this the cluster
	// took [app.roomMark]'s answer for a node this surface has never seen — the
	// queued glyph — which would draw a run that is spending money as work that
	// has not started.
	mark := a.roomMark(a.roomNode())
	if a.room.orch != nil {
		mark = a.orchHeadMark()
	}
	title := strings.TrimSpace(a.room.title)
	if room > 0 {
		title = fit(title, room-ansi.StringWidth(mark)-1)
	}
	if title == "" {
		return mark
	}
	return mark + " " + title
}

// roomChipFloor is the fewest cells the name may be cut to before the row has
// stopped saying where you are. It is [doneTitleFloor]'s figure — the tool
// line's own — because it is the same question asked about the same kind of
// string, and a second number here would be a second answer to drift from.
const roomChipFloor = doneTitleFloor

// roomModelLead is the word in front of a node's model wherever the status line
// says one, and the space after it is part of it. See [app.roomModelWord].
const roomModelLead = "task "

// roomModelWord is WHAT IS ANSWERING while a room is open: the model the ROOM's
// node runs on, said the way the status row says a model — its BASENAME, because
// that law is about the row's scarce width and not about whose model it is
// (render.go's [app.identity]).
//
// THE LEAD WORD IS PART OF THE FACT. The conversation's cluster reads
// "<name> · <model>", and a room's reading "<chip> · <model>" would put a second
// model id in the one place on this surface that has only ever held one: a person
// who has learned to read that spot would read the swap as a switch of the
// CONVERSATION's model, which is the misreading this whole change exists to
// prevent. So the node's id is led by a word saying whose it is, the way the
// served rider is led by "via" (render.go's [app.servedRider]) — a lead in the
// vocabulary the line already speaks, and no new colour: the cluster is painted
// once, in the accent, because a room is open.
//
// AN UNPUBLISHED MODEL SAYS NOTHING, AND MUST NOT FALL BACK TO THE SESSION'S.
// The engine reads a node's empty model as "the conversation's own" at the moment
// the node's agent is MINTED (internal/session's task_run.go, newTaskAgent) — and
// the person can move the conversation's dial afterwards, which they do from this
// very line. So "nobody published a model" and "this ran on something the session
// is no longer on" are the same thing seen from here, and the session's current id
// is a guess this row is not entitled to make. It draws nothing, which is what
// every other unpublished figure in a room draws ([app.roomSpend]).
//
// It wears NO REASONING SUFFIX AND NO SERVED RIDER either, for one reason said
// twice: THE DIAL IS THE CONVERSATION'S. The ":high" is spliced on by lending
// a.model its suffixed form for the length of one call (view.go's
// [app.statusRow]) and the rider is keyed on a.model's own sighting, so both are
// facts about the model the SESSION is running — and a task model wearing the
// session's knob would be the same lie in smaller print. Reading the node's model
// from the node keeps it out of the splice by construction.
func (a *app) roomModelWord() string {
	node := a.roomNode()
	if node == nil {
		return ""
	}
	model := modelBase(strings.TrimSpace(node.model))
	if model == "" {
		return ""
	}
	return roomModelLead + model
}

// ── the room, drawn ─────────────────────────────────────────────────────────
//
//	› check the config under etc/  [screenshot.png]
//	⠿ thought for 4s · 96 tok · ctrl+e
//	  I'll look at the loader first.
//
//	├─▶ read internal/config/load.go        · 189 lines
//	╰─▶ edit internal/config/load.go        +3 −1
//	task finished — esc to return
//
// There is NO PAINTING IN THIS FUNCTION, and that is the whole of the parity
// this file's header promises: the blocks are the conversation's blocks and the
// pass is the conversation's pass (render.go's [app.deckRows]), so the spacing
// law, the tool cluster, the fold, the markdown, the reasoning window, the
// hover and the expansions are the same code and cannot drift into two shapes.

// roomRows builds the room's row list, cached on its own content and width.
func (a *app) roomRows(width int) []row {
	room := a.room
	if room == nil || width < 4 {
		return nil
	}
	// THE HEIGHT IS PART OF THE KEY. It is read here rather than passed in so
	// that every caller — the frame, the wheel, the click, the freeze — lays the
	// page out against the one view the frame is drawing, and a resize, a rail
	// tier change or a draft growing a line all re-derive the fold's tail
	// without any of them having to remember to.
	height := a.viewHeight()
	if room.rows != nil && room.width == width && room.height == height && !room.dirty {
		return room.rows
	}
	// A RUN'S PAGE IS A GRAPH AND NOT A TRANSCRIPT (roomorch.go). It is branched
	// here — inside the room's own cache, above the conversation's renderers —
	// because everything around this line is about a room and nothing about a
	// list of blocks: the width, the cache, the hover pass and the offset are the
	// same questions for both pages, and only what fills them differs.
	if room.orch != nil {
		out := a.orchRows(width)
		a.hoverPass(out, width)
		room.rows, room.width, room.height, room.dirty = out, width, height, false
		return out
	}
	out, closed := a.deckRows(room.deck(), width)
	if room.harnessProgress != "" && !room.done {
		out = append(out, row{text: a.pal.dim(fit(room.harnessProgress, width)), entry: -1})
		closed = false
	}
	// AN EMPTY ROOM SAYS WHAT IT KNOWS AND WHY IT KNOWS NO MORE, above whatever
	// foot it is owed. It is drawn only when the page has no blocks at all: a room
	// with even one of them is a room with a transcript, and the foot alone is the
	// whole of what it adds.
	//
	// IT IS ASKED OF EVERY ROOM AND NOT ONLY OF A LANDED ONE, which is the whole
	// of [app.roomRecordRows]'s law rather than half of it. The branch used to sit
	// inside the `done` block below, so a node that was QUEUED or STILL WORKING
	// with nothing journaled yet drew a correct header — its name, its state, its
	// elapsed, all of it true — over a body with NOTHING in it. That is the page
	// that reads as the program having lost the work, and it is reachable on a
	// perfectly healthy session: a task opened the moment it is started has
	// journaled nothing yet, and one that is queued has not begun. The words
	// differ by what is true (roomYetWord above), the rule does not.
	if room.readFailed {
		out = append(out, row{text: a.pal.dim(fit(roomReadFailedWord, width)), entry: -1})
	} else if len(out) == 0 && room.harnessProgress == "" {
		out = a.roomRecordRows(out, width)
	}
	if room.done {
		// THE FOOT. A room on a node that has landed says so once, at the bottom,
		// where the next thing would have appeared — which is the place a person
		// is already looking when they wonder why nothing is. It takes the blank a
		// closed block above it asks for, which is what [app.deckRows] reports —
		// the same rule the conversation's ellipsis is drawn under.
		if closed && len(out) > 0 {
			out = append(out, row{entry: -1})
		}
		// A NODE THAT NEEDS A LOOK ASKS HERE, in the place the foot would have
		// said "finished": the same two rows its landed card draws, answering to
		// the same keys and the same pointer, and the same receipt once answered
		// (tasksettle.go's [app.roomSettleRows]). Every other landing keeps the
		// foot it has.
		var asked bool
		if out, asked = a.roomSettleRows(out, width); !asked {
			// AND IT NAMES A DOOR (roomrefusal.go). `task finished — esc to
			// return` was the whole of what this row said for a year, and esc is
			// already on the legend and on the focus header above it; where the
			// words in the box can go was on neither.
			out = append(out, row{text: a.pal.dim(a.roomFinishedRefusal().fit(width)), entry: -1})
		}
	}
	// THE POINTER, LAST, exactly as in the conversation (render.go's layout).
	a.hoverPass(out, width)
	room.rows, room.width, room.height, room.dirty = out, width, height, false
	return out
}

// roomRecordRows is what a landed room draws when it has NO TRANSCRIPT TO DRAW:
// the facts this surface already holds about the row, and then one honest line
// about why there is nothing else on the page.
//
// A ROOM IS NEVER AN EMPTY BODY UNDER A CORRECT HEADER. That combination is the
// worst page this surface can draw, because everything about it says the program
// has lost the work: the header names the task, gives its state and its elapsed —
// all of it read off the row the roster is still showing ([app.roomNode]) — and
// then the space where the work should be is blank. A person cannot tell that
// from a task whose output vanished, and there is nothing on the page to act on.
// So whatever else is true, the room spends these rows on what it can stand
// behind.
//
// THE ROW IS THE SOURCE, NOT THE ENGINE, and that is the point: this branch is
// reached precisely when a door refused the id or the journal was not found, so
// asking the engine again would answer nothing twice. The roster's record was
// published by the engine on a [session.TaskNotice] and is as true as the header
// drawn from it.
//
// AND A NODE THAT HAS NOT LANDED IS THE OTHER CASE, reached by opening a task
// that is queued or that has only just started: the file it will fill in exists
// and is empty, so there is nothing to replay and nothing has been lost either.
// It takes [roomYetWord], which is the same shape of answer said about a page
// that is not finished being written.
func (a *app) roomRecordRows(out []row, width int) []row {
	if a.room != nil && a.room.loading {
		return append(out, row{text: a.pal.dim(fit(roomLoadingWord, width)), entry: -1})
	}
	node := a.roomNode()
	word := roomGoneWord
	if a.room != nil && !a.room.done {
		word = roomYetWord
	}
	if node == nil {
		// A page this surface never had a row for. There is nothing to add to the
		// blank except the reason it is blank.
		return append(out, row{text: a.pal.dim(fit(word, width)), entry: -1})
	}
	// WHAT THE ROW SAYS THE WORK CAME TO, first, because it is the only thing here
	// a person came for. The report is prose somebody wrote, so it wraps.
	if report := strings.TrimSpace(node.report); report != "" {
		for _, line := range wrap(report, width) {
			out = append(out, row{text: a.pal.dim(line), entry: -1})
		}
	}
	// THEN WHY THERE IS NO TRANSCRIPT UNDER IT, chosen above.
	return append(out, row{text: a.pal.dim(fit(word, width)), entry: -1})
}

// roomRowDone answers the one question every foot, legend and refusal on a room
// hangs off: IS THE WORK OVER — from the roster's row, which is the record the
// engine publishes on every notice and the same record the header above the page
// is drawn from.
//
// A ROW THIS SURFACE DOES NOT HOLD IS OVER, and that is the honest reading rather
// than a fallback: nothing is coming for an id no notice ever named, and a page
// that waited for it would wait for ever.
func roomRowDone(node *taskNode) bool {
	if node == nil {
		return true
	}
	return node.state != session.TaskQueued && node.state != session.TaskRunning
}

// roomToolTail is how many of a folded turn's calls a room keeps on screen:
// as many as the view is tall, and never fewer than the conversation keeps.
//
// It is DERIVED FROM THE VIEW AT RENDER TIME and is not a second constant,
// because a constant is a number that drifts from the frame it was chosen for.
// The rule is "fold only the overflow": a call is at least one row, so a tail
// this long cannot leave the frame short by itself — the fold line and the
// oldest calls behind it are the only rows that start above the view, and
// scrolling up reaches them ([app.roomScroll]). The conversation is untouched:
// [deck.window] falls back to [toolWindow] wherever no tail is set.
func (a *app) roomToolTail() int {
	return max(toolWindow, a.viewHeight())
}

// roomWindow is the room's visible slice and the padding under it. It is
// [app.window]'s shape over the room's own rows and the room's own offset —
// which is the whole mechanism behind "esc restores the scroll exactly".
//
// THE PADDING FALLS BELOW A SHORT PAGE, EXACTLY AS IT DOES UNDER A SHORT
// CONVERSATION (view.go's frame states the law). Do not bottom-anchor a room:
// a young room growing from the top is the conversation's own behaviour, and
// the void that once sat between four folded rows and the box was never a
// gravity problem — it was the fold starving the page, which [app.roomToolTail]
// ended. A room with material now fills its frame, and a room without it reads
// from the top like everything else on this surface.
func (a *app) roomWindow(width, height int) ([]row, int) {
	if height <= 0 {
		return nil, 0
	}
	rows := a.roomRows(width)
	offset := a.roomOffsetFor(len(rows), height)
	end := min(offset+height, len(rows))
	visible := rows[offset:end]
	if pad := height - len(visible); pad > 0 {
		return visible, pad
	}
	return visible, 0
}

// roomOffsetFor resolves the room's scroll position, sticking to the live edge
// the way the conversation's does ([app.offsetFor]).
func (a *app) roomOffsetFor(total, height int) int {
	room := a.room
	bottom := total - height
	if bottom < 0 {
		bottom = 0
	}
	if room == nil || room.stick || room.offset > bottom {
		return bottom
	}
	if room.offset < 0 {
		return 0
	}
	return room.offset
}

// roomScroll moves the room's window and re-decides whether the reader is
// following the node.
//
// SCROLLING UP AT THE TOP OPENS THE FOLD. Scroll is the universal read-history
// gesture, and a fold is exactly the history a person came into a room to
// read; a wheel that stopped dead against a line saying "N earlier tool calls"
// was the gesture unwired from the one thing it is for. So a scroll up that
// arrives with the view already at the top, and a fold line on screen, unfolds
// that turn instead of going nowhere — and it is ANCHORED: the rows the person
// was looking at stay on the same screen lines, the offset advanced by exactly
// the rows the unfold put above them. Nothing jumps, and the next tick walks up
// into the calls that just appeared. The gesture is spent on the unfold; it does
// not also move. ctrl+o and a click on the line still toggle it either way, and
// scrolling back down to the live edge re-sticks without folding anything.
func (a *app) roomScroll(delta int) {
	room := a.room
	if room == nil {
		return
	}
	height := a.viewHeight()
	total := len(a.roomRows(a.bodyWidth()))
	bottom := total - height
	if bottom < 0 {
		bottom = 0
	}
	at := a.roomOffsetFor(total, height)
	if delta < 0 && at == 0 && a.roomUnfoldAtTop(total, height) {
		return
	}
	at += delta
	switch {
	case at >= bottom:
		room.offset, room.stick = bottom, true
	case at <= 0:
		room.offset, room.stick = 0, false
	default:
		room.offset, room.stick = at, false
	}
	a.touch()
}

// roomUnfoldAtTop is the anchored unfold [app.roomScroll] describes: the topmost
// fold line inside the view is opened, and the offset advances by exactly the
// rows the unfold added. It reports false, having done nothing, when no fold is
// on screen — which hands the gesture back to the ordinary clamp.
//
// THE ANCHOR IS THE CALLS UNDER THE FOLD, not whatever sat above it. Every row
// after the fold line moves down the list by the growth and nothing else, so an
// offset moved by the same growth keeps each of them on the screen line it was
// on; the calls that just opened take the lines the fold and anything above it
// held, and the next tick up walks into them. The instruction at the top of a
// young page scrolls off upward in that motion, which is right: the person is
// reading history upward, and it is the first thing they reach when they run
// out of calls.
//
// The offset is set directly rather than through the clamp because the clamp
// reads the OLD row count: the rows have just grown by the unfold, and the
// anchor is an index into the new list. `stick` goes false in the same motion,
// since a person opening history has left the live edge on purpose — a sticky
// reader would be dragged straight back down by the next event
// ([app.roomTouched]).
func (a *app) roomUnfoldAtTop(total, height int) bool {
	room := a.room
	rows := a.roomRows(a.bodyWidth())
	end := min(height, total)
	var open func()
	for i := 0; i < end && open == nil; i++ {
		open = a.roomFoldDoor(rows[i])
	}
	if open == nil {
		return false
	}
	open()
	grown := len(a.roomRows(a.bodyWidth())) - total
	room.offset = max(grown, 0)
	room.stick = false
	a.touch()
	return true
}

// roomFoldDoor is what a row on a room's page OPENS, or nil for a row that
// opens nothing. It is the whole of what [app.roomUnfoldAtTop] knows about the
// two kinds of fold a room draws, said once.
//
// TWO KINDS, ONE GESTURE. A cluster's `↳ N earlier tool calls` line and a
// phase's `▸ worked …` chip are both history a person came into a room to read,
// and scroll is the universal read-history gesture — so the wheel opens
// whichever of them it reaches first. A ladder that opened one and stopped dead
// at the other would dead-end exactly where the ruling that put the chips there
// promised it would not (workfold.go's [app.deckFolds]).
//
// AND IT ONLY EVER OPENS. A cluster's fold line exists only while it is folded,
// so toggling it was the same as opening it; a chip is drawn open OR shut, so a
// toggle would have made the wheel close the phase it had just opened and the
// next tick open it again. The chip already showing its work is not a door, and
// the scan walks past it to the next one that is.
func (a *app) roomFoldDoor(r row) func() {
	switch r.hit {
	case hitFold:
		return func() { a.unfold(r.turn) }
	case hitCaption:
		if a.room == nil || r.entry < 0 || r.entry >= len(a.room.entries) {
			return nil
		}
		turn := a.room.entries[r.entry].turn
		if a.room.unfolded[turn] {
			return nil
		}
		return func() { a.unfold(turn) }
	case hitWorkFold:
		// A CHIP ALREADY SHOWING ITS WORK IS NOT A DOOR — whether the reader
		// opened it or `ui.work = open` did (render.go's [app.deckRows]).
		if a.room == nil || a.workFoldOpen(a.room.deck(), r.turn) {
			return nil
		}
		return func() { a.openWorkfold(r.turn) }
	}
	return nil
}

// roomSteerLaneRows is the box's placeholder while a room is open: who the
// sentence is going to.
//
// It is applied to the block the input already rendered, for the reason
// [app.redirectLane] is (task.go): the hint slot inside the box belongs to the
// picker's filter, and an empty draft renders as the bare prompt, which is
// exactly the row a placeholder goes on.
func (a *app) roomSteerLaneRows(rows []string, width int) []string {
	if a.room == nil || len(rows) == 0 || !a.input.empty() || a.pick.open || a.awaitingTask() {
		return rows
	}
	// THE SEGMENT IN FRONT OF THE PROMPT ALREADY NAMES THE NODE ([app.roomLead]),
	// so the placeholder stops naming it: `⠙ Ship the port › Steer Ship the port…`
	// is one name read twice on one line. Where the frame is too narrow for the
	// segment the placeholder goes back to carrying the name itself, because
	// something on the row has to.
	lead := a.roomLead(width)
	// The cells the placeholder actually has. It is measured before the lane is
	// chosen because two of the choices below are REFUSALS, and a refusal is
	// fitted by its own law — the door survives and the fact degrades
	// (roomrefusal.go) — rather than by a cut from the right.
	room := width - ansi.StringWidth(lead) - ansi.StringWidth(prompt)
	lane := roomSteerLane + a.room.title + roomSteerBack
	if lead != "" {
		lane = roomSteerHere + roomSteerBack
	}
	if a.room.orch != nil {
		// A RUN HAS NO WORKER TO TALK TO, so the box does not offer to steer one:
		// the sentence goes to the PLANNER, which reads it on its next call
		// (roomorch.go). The placeholder says whose ear it is for the reason it
		// names a node out here — the box is the same box either way, and "who is
		// listening" is the question it exists to answer.
		lane = orchSteerLane + roomSteerBack
	}
	if a.room.done {
		lane = a.roomFinishedRefusal().fit(room)
	}
	out := append([]string(nil), rows...)
	out[0] = lead + a.pal.dim(prompt) + a.pal.dim(fit(lane, room))
	return out
}

// ── THE COMPOSER SAYS WHERE THE WORDS GO ────────────────────────────────────
//
//	⠙ Ship the port › fix the flake in the loader
//
// A ROOM USED TO STOP SAYING ANYTHING THE MOMENT YOU TYPED. The placeholder
// named the node — and a placeholder is the one thing in a box that disappears
// the instant somebody uses it, so the row that said "these words are going to a
// worktree somewhere else" said it only to people who had not started. Everything
// else on the frame that knew was somewhere the eye was not: the header at the
// top, the legend under the transcript, the status line at the bottom.
//
// So the box itself carries the room, as one segment in front of its prompt: the
// node's state cell and the node's name, lifted off the page on the selection
// tint and painted in the state's own hue ([app.taskStateInk] — the hue the
// roster paints the same node's glyph with, so the row that is banded in the
// column and the name in front of the caret are the same colour for the same
// reason). It is where a person's eye already is, it is there whether the box is
// empty or full, and it is gone the moment there is no room — the emptiness law:
// there is no segment in the conversation, not a dim one and not an empty one.
//
// IT IS A SEGMENT AND NOT A ROW. A row above the box would be a row taken off
// the transcript on every frame of every room, for a fact three cells can carry.
const (
	// roomLeadCap is the most of a node's name the segment spends, and it is the
	// strip's cap said again for the same reason: past about three words a title
	// stops identifying the work and starts being a sentence.
	roomLeadCap = stripTitleCap
	// roomLeadWordFloor is the least of a name worth drawing. Under this the
	// segment is dropped whole rather than shown as an ellipsis with a letter in
	// front of it, and the placeholder goes back to naming the node.
	roomLeadWordFloor = 6
	// roomLeadTyping is how much of the box the segment may never take: what is
	// left has to be a box somebody can see a sentence in.
	roomLeadTyping = 24
	// roomLeadPad is the air inside the segment, one cell each side, so the tint
	// reads as a chip rather than as a highlighted word.
	roomLeadPad = " "
	// roomSteerHere is the placeholder where the segment is already carrying the
	// name: what the box does, without saying the node twice.
	roomSteerHere = "Steer this task"
)

// roomLead is the composer's room segment, painted, or "" when there is no room
// open or no width to spend on one. width is the box's own width — what
// [app.inputBlock] is laying out into — because the segment is charged to the
// box and to nothing else.
func (a *app) roomLead(width int) string {
	if a.room == nil {
		return ""
	}
	node := a.roomNode()
	glyph := a.roomMark(node)
	// The box as it would be without a segment, less what the segment's own
	// furniture costs: two pads, the glyph, and the space after it.
	space := width - ansi.StringWidth(prompt) - roomLeadTyping -
		ansi.StringWidth(glyph) - 1 - 2*ansi.StringWidth(roomLeadPad)
	if space > roomLeadCap {
		space = roomLeadCap
	}
	if space < roomLeadWordFloor {
		return ""
	}
	title := fit(strings.TrimSpace(a.room.title), space)
	if title == "" {
		return ""
	}
	// A run's page has no node and so no state to be in; its segment is dim,
	// which is what this surface says with when nobody has published anything.
	ink := a.pal.dim
	if node != nil {
		ink = a.taskStateInk(node)
	}
	return a.pal.tint(roomLeadPad+glyph+" "+title+roomLeadPad, ink)
}
