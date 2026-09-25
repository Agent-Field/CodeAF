package tui3

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
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
// instant it stopped running. Current work uses the conversation's compact
// step display; opening it restores its calls and reasoning. Expanded calls
// retain the room's screenful budget. workfold.go owns settled phase folding,
// while livesteps.go owns the running work's disclosure.
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

// taskWeightDoor is what one node's worker weighs — the size of the request it
// has in flight, the figure a room's ↑ draws (tokencol.go). It is a door of its
// own because only an engine that holds the worker can answer it: a hosted room
// has no lane and reads the same fact off the node's journal instead
// (roomrefresh.go). Zero is "nobody is working that node right now".
type taskWeightDoor interface {
	TaskContextTokens(id uint64) int
}

func (a *app) taskSteerDoors() (taskSteerDoor, bool) {
	door, ok := a.agent.(taskSteerDoor)
	return door, ok
}

// roomGuest is the view the page on screen is being read through, and nil on an
// ordinary room — which is every room but one (taskowner.go).
func (a *app) roomGuest() *taskGuest {
	if a.room == nil {
		return nil
	}
	return a.room.guest
}

// roomIsGuest is THE ONE QUESTION EVERY ACTION ON A ROOM ASKS, and it is spelled
// once because the answer is the same for all of them: this page is a READING of
// another conversation's work, so nothing on it may reach into this window's
// graph.
//
// THE IDS COLLIDE BY DESIGN, which is what makes this dangerous rather than
// merely wrong. Task ids restart with every conversation, so a page onto another
// conversation's task 7 usually sits beside a perfectly real local task 7 — and a
// door that fell through to [app.tasks] would not fail. It would stop healthy
// local work, or move it onto another model, silently, while the person was
// looking at somebody else's task. So every one of those doors is ABSENT here
// rather than aimed somewhere safer.
func (a *app) roomIsGuest() bool { return a.roomGuest() != nil }

// roomSteerDoors is the door THE PAGE ON SCREEN types into: this conversation's
// own, and NONE on a page being read through somebody else's.
//
// IT IS ASKED IN PLACE OF [app.taskSteerDoors] AT EXACTLY ONE CALL SITE, which
// is what keeps this narrow: everything else on the surface that steers is
// steering its own conversation's work, and the one thing that might not be is
// the box under a room.
func (a *app) roomSteerDoors() (taskSteerDoor, bool) {
	if a.roomIsGuest() {
		return nil, false
	}
	return a.taskSteerDoors()
}

// taskModelDoor is the fourth door onto a node (internal/session's
// [Agent.RetargetTask]): the person's explicit pick of another model for THIS
// node, taking effect on its next request.
//
// IT ANSWERS WHEN, and that is the whole of why the answer is not a bare error:
// a step whose request has produced nothing the person could use lets go of it
// and asks again on the new model at once, and a step whose answer is already
// arriving finishes it first. Those are different things to wait for, so the
// room says which one it got.
//
// IT IS ITS OWN INTERFACE for the reason [taskRoomAgent] is: a capability is
// asserted, never required. An engine that can stream a node and be steered but
// has never heard of retargeting keeps its rooms, and the model word in there is
// simply a fact with no door on it — which is the honest degraded state and the
// same one every node that is not running is in.
type taskModelDoor interface {
	RetargetTask(id uint64, model string) (session.ModelLanding, error)
}

// taskModelDoors is that door under this surface, when it has one.
func (a *app) taskModelDoors() (taskModelDoor, bool) {
	door, ok := a.agent.(taskModelDoor)
	if host, hosted := a.agent.(interface{ TaskSetupSupported() bool }); hosted {
		ok = ok && host.TaskSetupSupported()
	}
	return door, ok
}

// roomModelMovable reports whether the room's model word is a DOOR as well as a
// fact — which is exactly the set of moments a press on it would do something.
//
// Ordinary settled tasks save continuation settings through the same picker.
// Adaptive runs and guest pages remain outside this task model door.
func (a *app) roomModelMovable() bool {
	if a.room == nil || a.room.orch != nil || a.room.plan != nil {
		return false
	}
	// AND A PAGE READ THROUGH SOMEBODY ELSE'S CONVERSATION MOVES NOTHING. The door
	// behind this ([app.taskModelDoors]) is THIS window's engine, so it would
	// retarget whatever this conversation calls by the same number
	// ([app.roomIsGuest]). The word is still drawn — a person is entitled to read
	// what the work ran on — and it does not light.
	if a.roomIsGuest() {
		return false
	}
	node := a.roomNode()
	if node == nil || node.run != "" {
		return false
	}
	if !taskSetupAvailable(node) {
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
	landing, err := door.RetargetTask(id, model)
	if err != nil {
		a.note(err.Error())
		if a.room != nil && a.room.id == id {
			a.roomNote(err.Error())
		}
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
	if a.room != nil && a.room.id == id {
		// AND THE TIMING IS WHAT IS TRUE, WHICH IS NOW TWO SENTENCES AND NOT ONE.
		// "Its next turn takes it" was true when the engine latched the model once
		// per turn, and a turn is a whole step, so a person who spoke while a step
		// was stuck waited for a boundary twenty minutes off. "The next turn takes
		// it; a rescue goes to it first" was the half-fix: the pick could only ride
		// a move the step was already making. Neither is true here any more. The
		// engine now answers which of the two things happened to the request that
		// is out RIGHT NOW, and the room says that back — so the sentence is a
		// report and not a promise, and there is nothing left for it to hedge with.
		//
		// A rescue is no longer worth a clause of its own. It was worth one while
		// the pick had to wait for a move somebody else made; now the next request
		// carries the pick whether it is a rescue, a retry or the step's own next
		// step, so naming the rescue would name one road out of three.
		timing := roomModelTiming(landing)
		if taskSetupLater(a.roomNode()) {
			timing = roomModelSavedWord
		}
		a.roomNote("model · " + model + " · " + timing)
	}
}

// The three things a room can truthfully say about when a pick lands, and the
// whole of what the engine's answer is turned into
// (internal/session's [session.ModelLanding]).
//
// `switching now` is the step having let go of a request nothing had come back
// from; `the next request takes it` is an answer already arriving being allowed
// to finish, and is also what a live node with nothing on the wire gets, which
// is true of it. NEITHER EVER SAYS "TURN": a step is one turn and can run for
// twenty minutes, and a person who read that about a step waiting on a pace they
// could not see was told their pick would do nothing today.
const (
	roomModelNowWord   = "switching now"
	roomModelNextWord  = "the next request takes it"
	roomModelSavedWord = "saved for when you continue"
)

func roomModelTiming(landing session.ModelLanding) string {
	if landing == session.ModelLandsNow {
		return roomModelNowWord
	}
	return roomModelNextWord
}

// taskModelUnavailableWord is the degraded case, in the vocabulary the other
// unavailable doors on this surface use (roomrefusal.go's [roomUnavailableRefusal],
// stop.go's [stopUnavailableWord]).
const taskModelUnavailableWord = "changing a task's model is unavailable — this session has no door onto it"

// ── the room's state ────────────────────────────────────────────────────────

// taskRoom is one node's page: what it has said, the lane carrying what it says
// next, and where the reader is in it.
type taskRoom struct {
	workActivity tokens.WorkActivity
	// detailsTop belongs to this page so scrolling its facts never moves the tree.
	detailsTop int

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
	// requests is the newest request lines the last journal reading held, for a
	// page with no lane: what lets the next reading count only what is new
	// (roomrefresh.go's [taskRoom.takeRequests]).
	requests []session.RequestLine
	// unfolded is the page's OWN fold state, keyed by the page's own turns. It is
	// not the conversation's map for the reason the entries are not the
	// conversation's list: a turn number means nothing outside the list it counts
	// (render.go's [deck]).
	unfolded        map[int]bool
	workOpen        map[int]bool
	capOpen         map[int]bool
	readingRestored bool
	lane            <-chan session.Event
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

	// plan is set when this page is A RUN'S TASK, read from the run's plan store
	// rather than from a node's journal and lane (planroom.go). It is a field on
	// this struct for [taskRoom.orch]'s reason: there is one page type, and
	// everything a room promises has to hold for a run's task too.
	plan *planRoom

	// tab is which of the page's two tabs is on screen: the transcript, or the
	// work it changed (roomtabs.go).
	tab roomTab

	// harnessProgress is the design lane's one evolving thought inside the
	// design node's room. It is display-only: no journal line is minted for live
	// telemetry, and the next event replaces this string in place.
	harnessProgress string

	// guest is set when this page is a task in ANOTHER CONVERSATION, read
	// through a second view onto the engine that is already running it
	// (taskowner.go). It is a field on this struct for [taskRoom.orch]'s reason:
	// everything a room promises — esc restores the transcript, the rail stays,
	// the scroll is the room's own — has to hold here too, and the only way to
	// guarantee that is for this to BE a room.
	//
	// IT REPLACES THE PAGE'S TWO SEAMS AND NOTHING ELSE. Where it is set, the
	// journal is read through it ([app.readRoomRecord]) and a line is steered
	// through it ([app.steer]); every other field means exactly what it means on
	// an ordinary room. The surface's own agent, conversation and drafts are
	// untouched, which is what makes this a view rather than a switch.
	guest *taskGuest

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
	rows       []row
	width      int
	height     int
	dirty      bool
	loading    bool
	readFailed bool
	journal    []byte
	// beatPath is where this node's pulse lives, AS THE RECORD ITSELF NAMED IT
	// (session.TaskRecord.Beat, from the checkpoint row's own field), and "" for a
	// node that is not writing one. IT IS CARRIED AND NEVER BUILT: a page that
	// recomputed it from the id would be a second spelling of where the pulse
	// lives, which is the exact bargain the record's field exists to end.
	beatPath string
	// beat is the LAST READING of the pulse, taken on the refresh tick itself so
	// the display keeps moving and never freezes on one take — and false for a
	// node with no pulse, which renders nothing, the same emptiness as any other
	// unknown ([roomFactsOf]'s live segment is the consumer).
	beat          session.TaskBeatRow
	beatRead      bool
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
	if r.running() {
		running = r.turn
	}
	// THE LENS IS THE WHOLE OF WHAT MAKES THIS PAGE A ROOM (lens.go): settled
	// phases fold to chips, the numbers gather in the header rather than under
	// each turn, the session's clock does not run over a node's turns, and a
	// expanded cluster keeps a screenful of calls instead of three.
	return deck{
		entries: r.entries, unfolded: r.unfolded, workOpen: r.workOpen, capOpen: r.capOpen,
		lens: r.readingLens(), runningTurn: running, col: &r.col,
	}
}

// running is whether the page's work is still going, and it is the ONE answer
// the deck's running turn, the live token column and every other sign of life
// on the page read. Hosted and guest pages read bounded journals without a
// local event lane. The task's reported state owns activity; a failed or lost
// reader cannot claim that its retained transcript is still making progress.
func (r *taskRoom) running() bool {
	return !r.done && !r.readFailed && (r.guest == nil || !r.guest.lost)
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
	roomStopHint   = "/stop · x with empty input · esc main"
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
	// The sentence the kin block says, in the alphabet the rail already spells a
	// family relation in — "waits: <title>", so "handed out: <title>" (task.go's
	// [app.railUnder]). A person who has read the roster's rows has already
	// learned this punctuation.
	//
	// IT USED TO HAVE A SIBLING, `part of: <title>`, and the breadcrumb took its
	// job (roomcrumbs.go): the parent is a PLACE and is on the trail with the rest
	// of the chain, where it can be pressed, rather than a fact one row under a
	// trail that was saying something else.
	//
	// AND `spawned:` WAS THE MACHINERY'S OWN WORD. It is what a process does to
	// another process, and this house bans it in anything a person reads — the
	// same rule that took `worktree` off the completion card. What actually
	// happened is that this piece of work handed some of itself out, which is the
	// verb the rest of the surface already uses for it.
	roomKinSpawnedWord = "handed out: "
	// roomKinStateSep joins a child to its state word on the spawned line. It is
	// the em dash the surface already uses to hang a condition off a name
	// (task.go's [taskBranchKept], "branch kept"), so the two levels
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
	// Replacing a view must release its subscription just as Escape does.
	// Leaving the old lane open does not keep useful work running; it leaks a reader.
	a.closeRoom()
	a.roomGen++
	r := &taskRoom{
		id: id, title: title, gen: a.roomGen,
		unfolded: map[int]bool{},
		capOpen:  map[int]bool{},
		stick:    true,
		dirty:    true,
	}
	r.workActivity.Start(a.now(), tokens.WorkLogoRandom)
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
	// A task destination takes the body and composer together. Park an open
	// start page before retargeting either of them.
	if a.startingChat() {
		a.parkChatStart()
	}
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
	record := session.ReadTranscript(doors.TaskJournal(id))
	room.entries, room.turn = a.roomRecord(record, roomTail)
	// ↑ FROM THE FIRST FRAME, where the journal can say it: the newest request
	// the node banked is the weight of what it last sent, and the worker's own
	// answer replaces it on the next beat ([app.usageBack]). ↓ is deliberately
	// NOT seeded from the same lines — the lane's step totals add the running
	// step's whole bill when it ends, and a seed would count its earlier
	// requests twice (tokencol.go).
	room.col.weight = record.Requests.Latest
	// AND WHAT THIS NODE HAS ALREADY STARTED JOINS THE SESSION'S COUNTS. Opening
	// the page is the moment this surface first READS a node's history, and a
	// background job it started an hour ago is as alive as one it starts while
	// somebody is watching ([app.tallyNode], Decision 4).
	a.tallyNode(id, room.entries)
	a.room = room
	// AND THE BOX STARTS TALKING TO THIS NODE (recipient.go). Whatever was being
	// written for the conversation — or for the node whose page this one replaced
	// — is stashed under its own reader, and this node's own unsent line, caret,
	// paste chips and tray are laid back out. Nothing is carried across: an
	// unsent draft has not changed its mind about who it is for.
	//
	// IT IS FIRST, AND THE SEND BELOW DEPENDS ON IT. The unresolved sends this
	// page draws are read off THIS recipient's composer (steersend.go's
	// [app.adoptUnsentSteer]), and the question it raises is drawn over this
	// page's own box — both of which are the wrong ones until the box has been
	// pointed at this node.
	a.retargetComposer(taskRecipient(id))
	// AND A CORRECTION THIS WINDOW NEVER LEARNED THE FATE OF COMES BACK ONTO THE
	// PAGE IT WAS TYPED INTO (steersend.go). It is drawn under the history rather
	// than lost with the process that was holding it, and it is still a send: the
	// question it raises asks again under the name it already had.
	a.adoptUnsentSteer(room)
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
	room.done = roomRowDone(node)
	a.room = room
	// The hosted door owes the composer exactly what the local one owes it
	// ([app.openRoom]): this page's own words, and nobody else's. It is still THIS
	// conversation's node, on another machine — a page reading ANOTHER
	// conversation's journal is a different reader, and this one line is all that
	// changes for it (recipient.go's [guestRecipient]).
	a.retargetComposer(taskRecipient(node.id))
	// The same unresolved send a local page adopts, held in hand so the bounded
	// journal read that is about to replace these rows keeps it (steersend.go).
	a.adoptUnsentSteer(room)
	a.sel = -1
	a.dropHover()
	a.touch()
	a.armRoomRecord()
}

// armRoomRecord parks the journal read a freshly opened page owes itself AND
// sets [taskRoom.loading] from whether there is actually a read on the way.
//
// THE FLAG IS A CLAIM ABOUT THE WIRE, NOT A MOOD, and the two doors that raise
// it had it the other way round: the flag was set first, unconditionally, and
// the read was asked for afterwards with nothing checking that it existed. Every
// door into [app.readRoomRecord] can answer nil — a hosted page whose row has no
// transcript URI and no by-id reader, a guest view whose owner never handed a
// room reader back — and where it did, `loading` stayed true with NO command
// parked, so no answer and no beat were ever coming. That is the page in the
// defect report: a huge empty body promising `loading this task's conversation…`
// under a header whose clock was counting up, for ever.
//
// So the read is asked for first and the flag is its answer. A page with nothing
// on the way is not loading; it falls through to the honest line about what it
// does know ([app.roomRecordRows]).
// IT PARKS THE READ ALONE AND BATCHES NOTHING WITH IT. Every caller of
// [app.takeRoomPump] on this road hands the one command straight back to the
// program loop and reads its answer as a [roomRecordMsg]; wrapping it in a batch
// here would make the far door's pump a different shape from the guest door's,
// which batches its own notice lane on top afterwards.
func (a *app) armRoomRecord() {
	a.roomPump = a.readRoomRecord()
	if a.room != nil {
		a.room.loading = a.roomPump != nil
	}
}

// Capture the reader and identity before leaving the program loop, for both
// the first read and refreshes. A window switch cannot change an in-flight read.
//
// A GUEST PAGE READS THROUGH ITS OWN VIEW. The journal belongs to another
// conversation and this window's own reader would ask its own engine session
// about an id that means something else there — the same wrong-owner failure as
// opening the room, one layer down. The view is captured here with everything
// else, so a page closed mid-read cannot be answered by a connection that has
// since been given back ([taskRoom.gen] discards it either way).
func (a *app) readRoomRecord() tea.Cmd {
	if a.room == nil {
		return nil
	}
	if guest := a.room.guest; guest != nil {
		read, id, gen := guest.room, a.room.id, a.room.gen
		if read == nil || guest.lost {
			return nil
		}
		return func() tea.Msg {
			record, err := read(id, session.TaskJournalTail)
			return roomRecordMsg{gen: gen, record: record, err: err}
		}
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
	if guest := a.roomGuest(); guest != nil && guest.lost {
		return nil
	}
	a.room.loading = false
	a.room.readFailed = msg.err != nil
	if msg.err == nil {
		a.refreshRoomRecord(msg.record.Journal, msg.record.Beat)
	}
	// A GUEST PAGE TAKES ONE THING FROM THE READING AND ONE ONLY: whether the
	// conversation it joined is still the conversation it joined. What the WORK is
	// doing is not in a journal at all and is never guessed from one — that comes
	// from the owner's own lane (taskowner.go's [app.tookGuestNotice]).
	a.tookGuestRecord(msg)
	a.room.resolveUnfinished()
	a.roomTouched()
	prefetch := a.prefetchRoomPictures()
	// AND A GUEST WHOSE CONVERSATION IS GONE STOPS ASKING. Retrying would be a
	// beat forever against an engine that has already given its final answer
	// ([taskGuest.lost]); the page keeps what it last read and says why.
	if a.roomIsGuest() {
		if a.room.guest.lost {
			return prefetch
		}
		return tea.Batch(prefetch, farRoomTick(a.room.gen))
	}
	// A GUEST PAGE KEEPS THE ANSWER IT OPENED WITH. `is the work over` is read
	// off THIS session's roster, and a task in another conversation has no row
	// there — [roomRowDone] answers true for a node it has never seen, which
	// would put `task finished` under work that is running in the window next
	// door. What the row on the tasks place said is the only reading this window
	// has, and [app.openOwnerRoom] set it on the way in.
	if a.room.guest == nil {
		a.room.setDone(roomRowDone(a.tasks[a.room.id]))
	}
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
	return surfaceTick(farRoomEvery, func(time.Time) tea.Msg { return farRoomTickMsg{gen: gen} })
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
	if a.room.done && !a.room.readFailed && !a.roomIsGuest() {
		return nil
	}
	cmd := a.readRoomRecord()
	if cmd == nil {
		// THE BEAT STOPS AND THE PAGE STOPS SAYING IT IS WAITING, together. A
		// reader can go away between one beat and the next — a guest view given
		// back, a row whose URI never arrived — and a page that kept the word
		// while nothing was coming is the stuck sentence [app.armRoomRecord]
		// exists to prevent, reached one tick later instead of at the door.
		if a.room.loading {
			a.room.loading = false
			a.roomTouched()
		}
		return nil
	}
	return cmd
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
	a.rememberRoomReading()
	a.roomGen++
	// AND THE LANE IS GIVEN BACK. A room closes while its conversation goes on
	// running, so there is nobody to close the channel for us the way an agent
	// being closed would (switcher.go's [laneStops]).
	if a.room.stop != nil {
		a.room.stop()
	}
	// AND A GUEST VIEW IS GIVEN BACK WITH THE PAGE, which is the whole of what
	// makes looking into somebody else's conversation free. It is THIS VIEW'S
	// connection and nothing else: the conversation goes on running, the window
	// that owns it keeps its keyboard, and the engine is untouched
	// (tui3.go's [TaskOwnerView.Close]).
	guest := a.room.guest
	if guest != nil {
		guest.release()
		a.room.guest = nil
	}
	// The clock thaws where it was frozen, at the value it would have had all
	// along: nothing was stopped, only unreported (task.go's [app.taskNow]).
	//
	// A GUEST PAGE FROZE NOTHING AND THAWS NOTHING. Its id belongs to another
	// conversation's graph and very probably also to a node of this one; thawing
	// on the way out would report a local node's clock as having run through a
	// page that was never about it.
	if guest == nil {
		a.thawNode(a.room.id)
	}
	a.room = nil
	// AND THE BOX GOES BACK TO THE CONVERSATION, HOLDING THE CONVERSATION'S OWN
	// WORDS (recipient.go). The line typed at the node stays with the node — esc
	// is a way out of a page and never a decision to throw a sentence away — and
	// the half-written message this window had for the model is exactly where it
	// was, caret included. It is done through the OWNER rather than through the
	// room that was just put down, so a page whose kind is not a node's (a run's,
	// roomorch.go) is stashed under its own name.
	a.retargetComposer(mainRecipient)
	// The guard is a question about a line typed at THIS node. Leaving the room
	// takes it down: the two answers it offers are both about a page that is no
	// longer on screen, and the words are not lost either way — they stay with
	// the page they were typed at, and come back with it (recipient.go).
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
	if a.room == nil || node == nil || a.roomIsGuest() {
		return false
	}
	// A RUN'S TASK IS MATCHED BY ITS STORE ID, which the run's own row and every
	// row lent to one of its parts carry alike ([taskNode.planTask]).
	if plan := a.room.plan; plan != nil {
		return strings.TrimSpace(node.planTask) != "" && strings.TrimSpace(node.planTask) == plan.id
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

// openRoomFor toggles compact task controls and transcript links within the same
// conversation. Sidebar rows use openRailRoom so a repeated click stays inside.
// A guest with the same task number belongs to a different conversation.
func (a *app) openRoomFor(id uint64, title string) {
	if a.room != nil && !a.roomIsGuest() && a.room.id == id {
		a.closeRoom()
		return
	}
	a.openRoom(id, title)
}

// openRailRoom makes list selection idempotent. Repeated clicks must not close
// the page or replace its draft, scroll position and live subscription.
//
// EVERY TASK OPENS THE TASK ROOM. A row whose task has a stored page opens the
// room over that store task (planroom.go), and the question is asked of the
// store at the gesture, off the loop ([app.openRailPlan]). What the row opens
// when the store has no page for it is fixed HERE, from the row as it was
// pressed: the read may come back after the rail has been redrawn, and an
// absent page still opens exactly what this gesture chose.
func (a *app) openRailRoom(node *taskNode) tea.Cmd {
	if node == nil || a.roomStandingOn(node) {
		return nil
	}
	id, title, run, part := node.id, node.title, node.run, node.node
	// THE STORE TASK IS THE ONE THE ROW NAMES, and a row that names none is asked
	// for under its own number, which is the number a run's door gives the
	// store task it seeds ([session.TaskNotice.PlanTask]).
	store := strings.TrimSpace(node.planTask)
	if store == "" {
		store = strconv.FormatUint(id, 10)
	}
	// A ROW LENT TO A STORE TASK HAS NO NODE BEHIND IT, so a store with no page
	// for it opens nothing rather than a room onto an id the engine never gave.
	if node.planRow != nil {
		return a.openRailPlan(store, nil)
	}
	_, hasPlan := a.planReader()
	room := func() tea.Cmd {
		if run != "" && !hasPlan {
			a.openOrchRoom(run, part)
		} else {
			a.openRoom(id, title)
		}
		return a.takeRoomPump()
	}
	return a.openRailPlan(store, room)
}

// openRailPlan opens the task room over one store task FROM THE CHAT, for a
// press on a rail row, on one of a run's own rows under it, on a crumb, on the
// tasks place or on the run's tab. `missing` is what the gesture does when the
// store has no such page.
//
// THE KEYS TYPED WHILE THE READ IS OUT ARE THE ROOM'S ([railPlanPending]): they
// go into the room's box once it is up, and nowhere else.
func (a *app) openRailPlan(id string, missing func() tea.Cmd) tea.Cmd {
	if a.railPlanPending.id == id {
		return nil
	}
	agent, ok := a.planReader()
	if !ok {
		if missing != nil {
			return missing()
		}
		return nil
	}
	a.beginRailPlan(id)
	return a.offLoop(func() func(bool) tea.Cmd {
		page, found := agent.PlanTaskPage(id)
		return func(here bool) tea.Cmd {
			// EVERY ENDING OF THE READ ENDS THE HOLD IT WAS MADE FOR. A second press
			// replaced this one, or `esc` withdrew it, and then the answer opens
			// nothing.
			if a.railPlanPending.id != id {
				return nil
			}
			keys := a.railPlanPending.keys
			a.railPlanPending = railPlanPending{}
			if !here {
				return nil
			}
			if !found {
				if missing != nil {
					return missing()
				}
				return nil
			}
			cmd := a.openPlanRoom(page)
			for _, key := range keys {
				a.railPlanReplay(key)
			}
			a.touch()
			return cmd
		}
	})
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
		// And the node's weight has just moved — its step is billed and its
		// result is joining what it sends next — so the beat asks now
		// ([app.usageOwed], [taskWeightDoor]).
		a.usageOwed = true

	case session.EventToolFailed:
		a.usageOwed = true

	case session.EventTurnDone:
		// THE STEP IS FINISHED AND ON DISK — the same event internal/session's
		// [taskCatchup] takes as the signal to drop what it was holding. The
		// block the node was writing is therefore over, and settling it here is
		// what stops the NEXT step's first word from being appended to the last
		// step's last paragraph. A design felt this hardest: its draft and the
		// revision the review pass writes are two replies on one lane, and
		// without this they arrived as one unbroken wall of JSON.
		room.closeLive()
		// AND ONE STEP'S OUTPUT IS ADDED TO THE PAGE'S ↓, the way the pilot adds
		// it to the node's bill (task.go's [taskNode.liveCost]): a node is driven
		// through many Submits, this is the end of one, and what came back over
		// its live work is the sum of them. The column is not opened afresh per
		// step because the work the block stands for is the node's whole run, and
		// the block leaves when the run does. The mark goes down with it, so ↓
		// goes on moving with what arrives after (tokencol.go's [tokenCol.bill]).
		//
		// ↑ IS NOT SUMMED, and that is the point of the change that put this
		// sentence here. The step's Input is every request of that step added
		// together; ↑ is ONE request — the node's newest — and it comes from the
		// worker itself on the usage beat ([taskWeightDoor]).
		room.col.bill(room.col.down+ev.Usage.Output, room.turnWritten(), room.turn)
		a.usageOwed = true

	case session.EventError:
		// THE NODE'S OWN FAILURES ARE ROWS ON ITS PAGE, in the conversation's
		// words for the same thing: a request that was asked again draws its
		// reason as it happens ([feed.retry], which this page's reducer takes
		// through [feed.ingest] above), and a step that ran out of tries ends on
		// `gave up after 2 tries · …` rather than on a page that goes on saying
		// nothing has arrived yet ([roomYetWord]).
		//
		// AND IT CANNOT SAY THE SAME THING TWICE AS THE PHASE LINE. The line
		// under the phase word (taskphase.go) is an ERRAND's ladder — a sizing
		// reading, a review — which asks a DIFFERENT model at each rung and
		// reports on the node's phase notice, never on its event stream
		// (internal/session's auxiliary.go sends no event at all). These rows are
		// the node's OWN turn being asked again. The two vocabularies are one
		// vocabulary read against that difference: an errand says `asking
		// <model>` because the model changes, and a turn says `asking again`
		// because it does not.
		a.roomNote(a.room.failureNote(ev.Err, a.serviceWordFor(a.roomNodeModel())))
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

// roomTraySteerWord is what a page says when there are files on its tray as a
// correction leaves it. A correction carries WORDS — the engine's steer door
// takes a sentence and nothing else — so the tray is left exactly as it was
// rather than quietly spent, and the line says where those files CAN be sent
// from, because this page can never send them.
const roomTraySteerWord = "attached files do not go with a correction · they stay on this page · esc, then attach them in the conversation to send them"

// steer is enter, while a room is open: the sentence in the box goes to the
// NODE, and lands in the room as the person's own line.
//
// It is the person's voice under the person's law — the accent hue, the same
// glyph the conversation's user block wears — because from the node's side it is
// exactly what it looks like: somebody talking. The engine wraps it in nothing
// (internal/session's SteerTask), and neither does this.
//
// A NODE THAT IS NOT LISTENING RAISES THE GUARD instead of swallowing the line
// (see [steerGuard]). The person's sentence comes back to them in that case:
// their words are still theirs, and taking them away after telling them they
// went nowhere would be the surface losing them twice (steersend.go's
// [app.restoreSteerDraft]).
//
// AND THE CROSSING ITSELF IS NOT DONE HERE. The engine is asked from a command,
// off the event loop, and its answer settles the row when it arrives —
// steersend.go states that law in full and this function's job is the two ends
// of it: what the page says the instant enter is pressed, and what it says when
// the answer comes back.
func (a *app) steer() tea.Cmd {
	room := a.room
	line := strings.TrimSpace(a.input.String())
	if room == nil || line == "" {
		return nil
	}
	// A COMPACT TAG WITH NOTHING BEHIND IT STOPS THE SEND HERE TOO (draftkeep.go's
	// [app.missingPaste]). [app.pastesUnfolded] below would hand the worker the tag
	// as though it were the document, and a correction is the last message that can
	// afford to be half of itself.
	if tag := a.missingPaste(line); tag != "" {
		a.roomNote(draftOrphanSendWord + " · " + tag)
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
	// A RUN'S TASK TAKES THE LINE AS A NOTE ON ITS STORE TASK (planroom.go): the
	// same box and the same enter, through the plan's own note door.
	if room.plan != nil {
		return a.planRoomSteer(line)
	}
	if a.roomIsGuest() {
		a.roomNote(roomGuestReadingWord)
		return nil
	}
	if room.done {
		a.raiseGuard(line, "")
		return nil
	}
	if !a.canSteerTask() {
		// AND IT SAYS WHICH REFUSAL THIS IS. An engine with no ear, a page belonging
		// to another conversation, and a conversation with no transcript to keep a
		// correction in are three different facts (steersend.go's [app.steerRefusal]).
		a.roomNote(a.steerRefusal())
		return nil
	}
	// The worker reads the paste and the room's row keeps the tag (pastechip.go).
	// The chips are spent as the words leave: a send that fails brings the
	// sentence back with its tags in it, and unfolding it twice would put the
	// document in the box.
	words := a.pastesUnfolded(line)
	// AND THE WHOLE COMPOSER IS PHOTOGRAPHED BEFORE IT IS EMPTIED. What a refused
	// send gives back is what the person actually had — their spelling, their
	// caret, and the compact chips their text stands on — and none of that can be
	// reconstructed from the sentence afterwards (steersend.go's [steerKeep]).
	keep := a.steerComposerNow()
	// A STEERED LINE IS A LINE YOU TYPED, so ↑ brings it back — through the same
	// door [app.enter] remembers a message through (recall.go), because the box in
	// here is the box out there.
	//
	// IT IS REMEMBERED WHEN THE SEND SETTLES AND NOT AT THIS KEYPRESS
	// ([app.rememberSteer]). A sentence the engine handed straight back is in the
	// box again, and a recall list holding it as well would be a history of
	// something that did not happen.
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
	// IT IS NOT CONSUMED YET, and that is the change this row went through. The
	// engine has not been asked at the instant this block is drawn — the crossing
	// is a command, and it may take as long as a slow link takes — so the row
	// wears the working clause the whole way over ([steerSendingWord]) and takes
	// the engine's own sentence when the answer arrives (steersend.go's
	// [app.steerSent]). A block that claimed delivery here would be claiming it
	// before anybody had said so.
	now := a.now()
	elbow := &steerElbow{words: line, at: now, landing: steerSendingWord}
	a.roomSaid(entry{kind: entrySteer, turn: room.turn, context: a.turnContext(), steer: elbow})
	if a.farRoomRecord != nil {
		room.keepSteerEcho(words, room.entries[len(room.entries)-1])
	}
	// AND FILES IN THE TRAY ARE SAID RATHER THAN PRETENDED. A correction is a
	// sentence — the engine's steer door takes words and nothing else — so the
	// tray is left exactly as it is, and a person who dropped a file in expecting
	// it to go with these words is told where it CAN go instead. The compact
	// pastes above did travel: they are in the sentence.
	if len(a.chips) > 0 {
		a.roomNote(roomTraySteerWord)
	}
	// The crossing, the draft's own save, and the two wakeups the clause needs —
	// which is [fadeTicks]' whole bargain, and here it is what turns the spinner
	// on a page where nothing else is arriving (steerelbow.go takes the same two
	// for the same reason).
	return tea.Batch(a.sendSteer(room, line, words, elbow, keep), a.edited(), fadeTicks())
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
	// lost is the THIRD question, and it is not about the node at all: nobody
	// answered the crossing, so what is unknown is whether the words arrived
	// (steersend.go). Neither of the other two keys is honest here — reviving the
	// work or sending the line to the model would both duplicate a correction the
	// task may already hold — so the only offer is to ASK AGAIN about the send
	// that is already out there, under the name it already has.
	lost *steerSend
	// risky says asking again may deliver twice, because the engine at the other
	// end does not recognise a repeat. The offer says so in as many words: a
	// person choosing it is deciding, and a surface doing it quietly is a defect.
	risky bool
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

// raiseLostGuard is the question a crossing NOBODY ANSWERED raises. The words
// are on the page and still held as a send; this is the offer to ask again
// about that same send rather than to type it a second time.
func (a *app) raiseLostGuard(send *steerSend) {
	if a.room == nil || send == nil {
		return
	}
	// THE REASON IS NOT REPEATED HERE. The row the words are on already carries
	// it ([steerLostWord]), and one sentence said twice on two adjacent rows is
	// the surface talking over itself.
	a.guard = &steerGuard{
		title: a.room.title, text: send.line, lost: send,
		// Asking again can only deliver twice where a crossing was LOST and the
		// engine keeps no message names. A refusal delivered nothing.
		risky: send.lost && !send.door.repeat,
	}
	a.closeLists()
	a.touch()
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
	lost := a.guard.lost
	switch msg.String() {
	case "ctrl+c":
		return nil, false
	case "r":
		// A LOST GUARD'S `r` ASKS AGAIN ABOUT THE SEND ALREADY OUT THERE, under
		// the name it already had, which is what keeps one correction one
		// correction (steersend.go's [app.retrySteer]).
		if lost != nil {
			a.dropGuard()
			return a.retrySteer(lost.at), true
		}
		// A busy guard has no revive (see [steerGuard.busy]): the key does
		// nothing rather than restarting work that is still running.
		if a.guard.busy {
			return nil, true
		}
		return a.guardSend(true), true
	case "m":
		// AND IT HAS NO `m` EITHER. Sending the line to the model while the task
		// may already hold it is the duplicate this question exists to avoid, and
		// there is nothing to send to main anyway — the words are on the page.
		if lost != nil {
			return nil, true
		}
		return a.guardSend(false), true
	case "esc":
		// The words stay in the box. esc here is not "leave the room" — the room
		// is still open under the question — it is "I did not mean to send that
		// yet", and the sentence is exactly where it was.
		//
		// AND ON A LOST GUARD IT LEAVES THE SEND WHERE IT IS: still held, still on
		// the page wearing its own clause, and still one `r` from being asked
		// about again. Nothing is dropped by declining to decide.
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
	// THE ACCEPTED DRAFT IS THE ROOM'S, AND IT IS SPENT WHILE THE ROOM STILL HOLDS
	// THE BOX (recipient.go). These words were typed at the node and are going to
	// the model instead because the person just said so — so the page's own line
	// is cleared here, and the conversation's unsent sentence, which nobody has
	// decided anything about, is still waiting under [app.closeRoom] below.
	//
	// AND THE MODEL READS THE DOCUMENTS AND NOT THE TAGS. A compact paste in the
	// room's box carries `[paste 1 · 42 lines]` on screen and forty-two lines
	// underneath (pastechip.go's law: every door that carries the box's words to a
	// model hands over the second). This road handed over the first, so a sentence
	// re-pointed at the conversation arrived as a tag the model could only guess
	// about — and the chips are the page's, so they are unfolded and spent before
	// the box changes hands.
	shown := line
	line = a.pastesUnfolded(line)
	a.pastes = nil
	a.input.reset()
	a.endRecall()
	a.closeLists()
	a.closeRoom()
	a.stick = true
	// The person's own sentence is what goes in the recall list, not the
	// wrapper this surface put around it: ↑ is for getting back what you typed.
	a.remember(guard.text)
	// AND THE FILE FOLLOWS THE CONVERSATION'S BOX RATHER THAN BEING DROPPED
	// (draft.go's [app.keepMainDraft]). What was spent here was the page's line,
	// not main's, so a remove would delete the crash insurance for a sentence
	// still sitting in the box this keystroke just came back to.
	a.keepMainDraft()
	return a.submitShown(line, shown)
}

// ── the guard, drawn ────────────────────────────────────────────────────────

// ONE SLOT, ONE QUESTION NOW. The stop confirmation (stop.go) and the tab-close
// card (tabclose.go) were drawn in exactly these rows, and both are the question
// block's (question.go) — so what is left here is the steer guard alone, which
// is not a question the block draws: it is the surface holding a SENTENCE back
// until it is told where to send it, and what it offers are two destinations
// rather than two answers.

// guardHeight is how many rows the question takes: the offer, and the engine's
// reason under it when there is one.
func (a *app) guardHeight() int {
	if !a.guarding() {
		return 0
	}
	if a.guard.why == "" {
		return 1
	}
	return 2
}

// guardMark says what one row of the slot is for the pointer, and the answer is
// NOTHING: the steer guard is three keys and nothing else. It is a function
// rather than a bare zero at the call site because the frame asks every block on
// the chrome the same question, and a slot that answered it differently would be
// the one place a reader has to check.
func (a *app) guardMark(int) chromeRow { return chromeRow{} }

// guardRows draws it, in the question hue the approval block wears and for the
// same reason: this is the surface blocked on a keyboard, and the one thing on
// screen that is blocked on you must not look like the things that are not.
func (a *app) guardRows(width int) []string {
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
	// THE LOST QUESTION OFFERS ONE ACTION AND SAYS WHAT IT COSTS. Where the
	// engine keeps message names, asking again cannot deliver twice and the line
	// says so; where it does not, the line says that instead, because the person
	// is the one deciding (steersend.go).
	if a.guard.lost != nil {
		retry := steerLostRetry
		if a.guard.risky {
			retry = steerLostRisk
		}
		// A send the engine REFUSED as belonging to another conversation was not
		// delivered at all, so the question is not about a missing answer.
		ask := steerLostAsk
		if !a.guard.lost.lost {
			ask = steerElsewhereAsk
		}
		parts = []string{
			a.guard.title + ask, "[r]", retry, "[esc]", steerLostLeave,
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
	// THE LETTERS THAT DECIDE ABOUT A NODE THAT NEEDS A LOOK ARE NOT TAKEN HERE.
	// A landed `your call` is a question, and the question block above the box
	// answers it on every page with the one key grammar — which is read before
	// this file is reached (input.go's key order, tasksettle.go says why).
	//
	// AND NEITHER IS A DESIGN WAITING TO BE JUDGED. It had two chords of its own
	// pinned above the box in here (roomapproval.go, deleted) — one decision with
	// two drawings and two grammars. It is the same question on the same block,
	// answered by the same digits, and the two keys this file owns are untouched:
	// esc leaves and enter steers.
	// AND A RUN'S PAGE IS READ BEFORE THE ROOM'S OWN TWO KEYS (roomorch.go),
	// because it has more levels than a room does: esc walks out of a chip's card
	// and out of a nested run before it walks out of the page at all, and enter
	// over an empty box opens the chip under the cursor rather than steering
	// nothing. The recall walk is excluded here for the same reason esc excludes
	// it below — a state the dismiss key cannot dismiss is a trap — and every key
	// the page does not take falls through to the two below.
	// Scoped controls remain commands after their argument closes the slash list.
	// Otherwise Enter would send "/model <slug>" to the worker as an instruction.
	if msg.String() == "enter" {
		line := strings.TrimSpace(a.input.String())
		name, _, _ := strings.Cut(line, " ")
		if name == "/model" || name == "/stop" {
			a.input.setText("")
			a.closeLists()
			return a.slash(line), true
		}
	}
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

	case roomTabKey:
		// THE OTHER TAB, over an empty box (roomtabs.go). A box with words in it
		// keeps the key for what it does there.
		if a.roomHasTabs() && a.input.empty() && !a.comp.open {
			return a.roomTabNext(), true
		}
		return nil, false

	case "enter":
		if !a.roomIsGuest() && a.room.plan == nil && strings.TrimSpace(a.input.String()) == "" {
			entry := a.roomRetryEntry()
			if a.taskCanRetry(entry) {
				return a.retryTask(entry), true
			}
		}
		return a.steer(), true

	case "alt+pgup":
		a.roomDetailsScroll(-a.scrollPage())
		return nil, true
	case "alt+pgdown":
		a.roomDetailsScroll(a.scrollPage())
		return nil, true

	case effortKey:
		// HOW HARD THIS NODE THINKS, one step up the ladder — the same chord that
		// moves the install's rung on home at rest and a standing item's on its
		// own card, bound here to the node whose page this is (taskeffort.go).
		// Everything that outranks the room outranks it, because it is read from
		// inside the room's own switch and never above it.
		if a.room.plan == nil {
			a.cycleTaskEffort()
		}
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
	if a.room != nil && !a.roomIsGuest() && !a.guarding() && !a.asking() && !a.stopping() {
		entry := a.roomRetryEntry()
		if hint := a.taskRetryHint(entry); hint != "" {
			return hint
		}
		if a.taskCanRetry(entry) && strings.TrimSpace(a.input.String()) == "" {
			return taskRetryWord
		}
	}
	switch {
	case a.guarding() || a.stopping():
		// Both draw their own answers on their own row, directly above the box
		// (see [app.guardRows]). This is the rewind mode's rule: a slot repeating
		// keys that are already on screen is a slot nobody reads twice.
		return ""
	case a.recalling():
		return roomRecallHint
	case a.stopOffered():
		if a.roomOrganized() {
			return "/model · /stop · esc main"
		}
		// THE ROOM'S ANSWER TO "HOW DO I STOP THIS". It is the honest counterpart
		// to the conversation's "esc interrupt": the work in here ends through a
		// card and never through the dismiss key (stop.go), so this is the key a
		// person reaching for esc actually wants. It is drawn only while there is
		// something to stop, which is the emptiness law applied to a hint.
		return roomStopHint
	case a.roomLandingAsking():
		// THE ROOM'S ANSWER TO "IT SAYS LOOK IT OVER, NOW WHAT". The node has
		// landed, so nothing above this is live, and the answers are on the
		// question block above the box — but that block is at the other end of a
		// page somebody is reading, and this slot is the one place on the frame a
		// person looks for the next keystroke.
		//
		// AND IT IS SPELLED FROM THE QUESTION'S OWN ANSWERS, so the block, this
		// slot and the roster's cannot name three different letters for one
		// question ([app.landingHintAt]).
		return a.landingHintAt(a.room.id, a.width, "")
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
	width := a.bodyWidth()
	height := a.viewHeight()
	// COPY OWNS THE PAGE BEFORE IT IS LAID OUT, so the room's transient
	// activity and the blank belonging only to it never enter the snapshot
	// (worklogo.go, #1384).
	a.copy.on = true
	a.room.dirty = true
	rows := a.roomRows(width)
	if len(rows) == 0 {
		a.copy.on = false
		a.room.dirty = true
		return
	}
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
	if a.room != nil && !a.roomIsGuest() {
		for i, node := range running {
			if node.id == a.room.id {
				at = i
				break
			}
		}
	}
	next := running[(at+1)%len(running)]
	if a.roomStandingOn(next) {
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
// A ROW IS A DOOR AND ONLY A DRAWN CONTROL IS ANYTHING ELSE. Anywhere on a
// node's row opens that node's room, which is what every row of this column has
// always done; a group's heading opens or folds its group; and the side
// column's own rows answer through the doors their layout recorded
// (sidecol.go), so the target is always exactly what is on screen.
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
	// THE SEAM IS THE COLUMN'S HANDLE WHERE THERE IS A TIER TO PULL IT TO. It
	// answers before rows do because the same two cells run through node rows and
	// the footer alike: grabbing the handle changes the column, never opens
	// whatever happens to sit behind it. Where the frame lends no second tier
	// there is no handle, so those two cells are the row's like every other cell
	// on it ([app.railSeamAt], and task.go's [app.railCanWiden] for the whole of
	// why).
	if a.railSeamAt(x, y) {
		a.railWiden(!a.railWide)
		return nil, true
	}
	line, ok := a.railLineAt(y)
	if !ok {
		return nil, true
	}
	if line.roomAction != "" {
		a.roomPanelTake(line.roomAction)
		return nil, true
	}
	// THE SIDE COLUMN'S OWN ROWS ANSWER FOR THEMSELVES: the header's words and
	// its key, a band item, a row of the Traffic and the doors on each
	// (sidecol.go). A row with nothing to press takes the press to do nothing.
	if line.side != nil {
		if !line.side.pressable() && line.side.doorAt(a.sideLineCol(x)) < 0 {
			return nil, true
		}
		return a.sidePress(line.side, a.sideLineCol(x))
	}
	// THE FOOTER'S ONE OFFER IS PRESSABLE, because a hint that names a key and
	// cannot be pressed is a hint that is only for one of the two hands
	// (task.go's [app.railFootRows]).
	// AND THE DOOR ONTO THE TASK PAGE IS THE THIRD OF THEM, on the same terms: it
	// names a chord, so it has to answer to the hand that does not type chords
	// ([taskSheetPastHint], taskview.go). It leaves the column exactly as it is —
	// the page is a place you go and come back from, not a state the column
	// enters.
	if line.more {
		return a.showPage(pageTasks), true
	}
	// AND THE STANDING COUNT IS THE FOURTH, on the same terms: it is a line of
	// the footer, it belongs to no node, and it opens the page a person reading
	// that number is trying to find (standdoor.go). It is refused only where the
	// page is already what they are looking at.
	if line.keeping {
		if a.at(pageStanding) {
			return nil, true
		}
		return a.openStanding(), true
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
	// A ROW OF A RUN IS A DOOR ONTO THAT TASK'S PAGE, the run's own row and every
	// part under it alike. It is a line of the store's tree and no node of this
	// window's graph, so it is asked before the entries below.
	if line.plan != "" {
		return a.openRailPlan(line.plan, nil), true
	}
	e, ok := a.railEntryAt(y)
	if !ok {
		return nil, true
	}
	a.railWhere = railSpotOf(e)
	// A GROUP'S HEADING OPENS THE GROUP OR FOLDS IT, anywhere on the row, and
	// the running work's heading, which does not fold, takes the press to do
	// nothing. EVERY OTHER ROW IS A NODE, and the whole row is its door.
	if e.head {
		a.sideToggleGroup(e.group)
		return nil, true
	}
	if e.node == nil {
		return nil, true
	}
	a.sideAck(e.node)
	return a.openRailRoom(e.node), true
}

// railSeamAt reports whether a pointer is on the visible two-cell handle — which
// is a handle only where there is a tier to pull it to (task.go's
// [app.railCanWiden] says what it cost when it was not).
func (a *app) railSeamAt(x, y int) bool {
	if !a.railAt(x, y) || !a.railCanWiden() {
		return false
	}
	left := a.railLeft()
	return x >= left && x < left+ansi.StringWidth(railSeam)
}

// railAt reports whether a pointer at these coordinates is over the roster. It
// is the press's guard and the hover's alike (hover.go), because a column that
// answered a click it would not light under the pointer is a column that
// disagrees with itself about what it is.
//
// IT IS BOUNDED BOTH WAYS, and the vertical bound is the same one the closed
// column's edge already keeps (task.go's [app.railGripAt]): the roster is drawn
// into the BODY REGION and nowhere else — [app.railRows] returns exactly
// [app.viewHeight] lines, footer included — so a column that answered for every
// row of the frame was answering for the composer, the legend and the status
// line under it. [app.railPress] takes what [app.railAt] gives it and returns
// "taken" whether or not a line resolves, so those rows lost every press landing
// in the roster's last thirty columns: the box did not focus, the hint did not
// act, and nothing at all happened. Below the region a press is somebody else's.
func (a *app) railAt(x, y int) bool {
	if !a.railFull() && (!a.railShowing() || x < a.bodyWidth() || x >= a.bodyWidth()+a.railWidth()) {
		return false
	}
	top := a.bodyTop()
	return top >= 0 && y >= top && y < top+a.viewHeight()
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
//	─ main ▸ Ship the port ▸ Fix the nil-map crash ───────────── esc/← main ─
//	─ ⠙ working · 2m12s · $0.04 · 6 tool calls ───────────────────── Stop ───
//
// A ROOM USED TO LOOK LIKE THE CONVERSATION. Same rows, same hues, same box
// underneath, and the only two things saying otherwise were a word in the legend
// and a placeholder in the box — both of which are read once and then stop being
// read. A person who walked into a node, scrolled, and looked up two minutes
// later had nothing on screen telling them that the sentence they were about to
// type was going to a worktree somewhere else.
//
// So the room pins its own rows at the top of the body region, under the tab
// strip (chattabs.go). They are pinned rather than scrolled for the reason a
// status line is pinned — a fact that scrolls away is a fact that is only true
// at the top of the page.
//
// ── AND THEY ARE TWO ROWS, WHICH THEY WERE NOT ──────────────────────────────
//
// One row carried all of it: the state glyph, then the trail, then the state
// word, the clock, the spend, the call count and the model, joined with the same
// `·` the trail's own steps were joined with and painted in the one accent the
// whole line wore. So the ancestry a person came to read ran into telemetry with
// no seam between them, the loudest ink on the page was spent on figures, and
// the path — the thing the row exists for — was the part that got cut first when
// the frame narrowed.
//
// THE TRAIL ROW IS NOW ANCESTRY AND NOTHING ELSE: "main ▸ parent ▸ child", the
// way out at its right end, and no glyph, no money, no model. THE FACTS ROW
// UNDER IT is what the work is doing — the state in its own semantic ink, the
// rest in the dim every other piece of telemetry on this surface wears — and it
// ends in `Stop`, spelled out, which is the one control anywhere on the surface
// that ends work with a pointer.
//
// NAVIGATION OUTRANKS TELEMETRY WHEN THE FRAME IS NARROW, and the split is what
// makes that true by construction rather than by negotiation: the trail gets its
// whole row, and the facts degrade on their own row by the ranked prefix every
// list on this surface already uses (rowfit.go's law 3).
//
// THE TRAIL IS A BREADCRUMB and it always names the root: "main ▸ <node>" one
// level down, "main ▸ parent ▸ child" when a node's page grows a door into the
// node it spawned. The root is on it even at one level deep because the trail's
// job is to say what this page hangs off, and "main" is the one name this
// surface has for the conversation itself.

// roomHeadFloor is the narrowest frame that gets a pinned header at all: under
// it there is not a trail and a way out's worth of room, and the row would be an
// ellipsis. It is stated once because the kin rows under the header stand on it
// too — a header region whose two halves disagreed about the floor would be rows
// the geometry counted and the frame did not draw ([app.roomKinRows]).
const roomHeadFloor = 12

// roomHeadRows is the room's pinned rows, or none when there is no room open and
// nothing to pin. They are drawn by the frame (view.go), which is the only thing
// that knows where the top of the body region is, and there are always exactly
// [roomHeadRowCount] of them when there are any — the geometry charges for that
// number and a frame that drew a different one would put the room's last row
// under the input box.
func (a *app) roomHeadRows(width int) []string {
	a.roomStop, a.roomBackSpan, a.crumbs = hudSpan{}, hudSpan{}, nil
	// THE CONVERSATION HAS NO HEADER OF ITS OWN. It used to draw one crumb here —
	// its own name, which is `main ▸` with nothing after it — and the tab strip
	// above now says that and more (chattabs.go). A row whose whole content is a
	// fact the row above it already carries is a row this surface does not draw.
	rows := a.roomHeadHeight(width)
	if rows == 0 {
		return nil
	}
	if rows == 1 {
		return []string{a.roomTrailRow(width)}
	}
	head := []string{a.roomTrailRow(width), a.roomFactsLine(width)}
	if a.roomOrganized() {
		head = []string{a.roomTrailRow(width), a.roomTitleRow(width)}
	}
	if rows > a.roomHeadCount() {
		if a.roomOrganized() {
			head = append(head, a.rule(width))
			return head
		}
		head = append(head, "")
	}
	return head
}

// roomHeadRowCount counts the semantic rows before optional trailing air on a frame with the
// height to spare: the trail and the title with its facts. Compact frames
// keep the title within the trail. The tab strip above and
// the kin rows below are counted separately.
const roomHeadRowCount = 2

// Compact frames already name the task in their navigation row.
func (a *app) roomHeadCount() int {
	if a.roomOrganized() {
		return roomHeadRowCount
	}
	return 2
}

// roomHeadHeight is how many of those rows this frame can actually afford.
//
// THE FACTS ROW IS THE SECONDARY CHROME AND IT STANDS DOWN FIRST, on the ladder
// the kin rows and the breathing room already stand on (view.go's
// [app.breathingRows]). A person on a six-row terminal has one row of page and a
// box; what they cannot do without up here is WHICH PAGE THIS IS, so the trail
// keeps the row and the telemetry — which is still on the status line — gives it
// up. Navigation first when the frame is short is the same law as navigation
// first when it is narrow.
//
// It is asked rather than assumed by [app.headHeight] and by every pointer
// target on these rows: a row the frame drew and the scrolling did not subtract
// puts the page's last line under the input box.
func (a *app) roomHeadHeight(width int) int {
	if a.room == nil || width < roomHeadFloor || a.breathingRows() == 0 {
		return 0
	}
	if a.breathingRows() < 2 {
		return 1
	}
	return a.roomHeadCount() + a.roomHeaderPad()
}

// roomTrailRow is the ancestry, the way out, and nothing else.
func (a *app) roomTrailRow(width int) string {
	left, hits := a.roomHeadParts(width)
	if a.roomOrganized() {
		left, hits = a.roomAncestorParts(width)
	}
	a.crumbs, a.roomBackSpan, a.roomTabSpans = hits, hudSpan{}, nil
	line := strings.Repeat(" ", headLabelAt) + a.paintCrumbs(left, headLabelAt, a.pal.accent)
	leftWidth := headLabelAt + ansi.StringWidth(left)
	back := " " + roomBackWord + " "
	backWidth := ansi.StringWidth(back)
	if leftWidth+2+backWidth+1 <= width {
		from := width - backWidth - 1
		a.roomBackSpan = hudSpan{from: from, to: from + backWidth}
		shown := a.pal.dim(back)
		if a.hoveringRoomBack() {
			shown = a.pal.cursor(shown, 0)
		}
		// THE TABS STAND BESIDE THE WAY OUT, where they cost no row of the page,
		// and only where the whole of both names fits (roomtabs.go). A frame too
		// narrow for them keeps the trail and the way out, and `tab` still moves.
		if tabs, cols, spans := a.roomTabsLabel(); tabs != "" && leftWidth+2+cols+2 <= from {
			at := from - 2 - cols
			for _, span := range spans {
				a.roomTabSpans = append(a.roomTabSpans, hudSpan{from: at + span.from, to: at + span.to})
			}
			return line + strings.Repeat(" ", at-leftWidth) + tabs + "  " + shown + " "
		}
		return line + strings.Repeat(" ", from-leftWidth) + shown + " "
	}
	return line + strings.Repeat(" ", max(0, width-leftWidth))
}

// roomFactsLine is the quiet row under the trail: what this work is doing, what
// it has taken, and the one control that ends it.
//
// EVERY SEGMENT IS DROPPED WHEN NOBODY HAS PUBLISHED IT — THE EMPTINESS LAW,
// PER SEGMENT. A queued node has no clock, an unpriced one no cost, a node that
// has called nothing no count, and a node nobody has dialled no rung. That is
// the reason the turn footer drops its own fields (timestamps.go): a figure
// that is zero is a figure nobody measured, and `$0.00 · 0 tool calls` is the
// row spending its scarce cells saying nothing twice.
//
// THE STATE WEARS THE NODE'S OWN INK and everything after it is dim. The hue is
// [app.taskStateInk] — the same one the roster paints that node's glyph with —
// so "your call" reads as warning here exactly as it does in the column,
// and the figures beside it read as figures. The row is painted in pieces rather
// than nested for [app.roomTrailRow]'s reason: a hue inside a hue ends at the
// inner one's reset.
//
// Roomy frames group outcome and activity apart from model, effort and cost.
// When those complete groups cannot fit, the shared ranked fitter (rowfit.go)
// retains a prefix of the compact priority order, without skipping facts.
func (a *app) roomFactsLine(width int) string {
	a.roomStop = hudSpan{}
	node := a.roomNode()
	mark := a.roomStopWord()
	shown := mark
	// THE `Stop` BRIGHTENS UNDER THE POINTER, and it is brightened HERE rather
	// than spliced into the finished line: [app.legendLine] paints the right label
	// as one piece and the word is the last thing in it, so ink written into the
	// label lands on the word's own cells. It is a step up from the dim the label
	// rests in, which is this surface's answer to a label that is also a control
	// (render.go's [app.paintIdentity]) — the row is one line at the top of the
	// frame, and a highlighted rectangle round one word would be the one boxed
	// thing on a surface with no boxes.
	if mark != "" && a.hoveringRoomStop() {
		shown = a.pal.ink(mark)
	}
	if node != nil && !a.orchOpen() {
		if line, ok := a.roomGroupedFacts(node, width, shown); ok {
			if mark != "" {
				cols := ansi.StringWidth(mark)
				a.roomStop = hudSpan{from: width - 2 - cols, to: width - 2}
			}
			return line
		}
	}
	left, lead := a.roomFactsWord(node, width)
	ink := a.pal.dim
	if node != nil && lead > 0 {
		ink = a.taskStateInk(node)
	}
	paint := func(label string) string {
		cols := ansi.StringWidth(label)
		if lead <= 0 || lead > cols {
			return a.pal.dim(label)
		}
		return ink(ansi.Cut(label, 0, lead)) + a.roomSetupInk(ansi.Cut(label, lead, cols), node)
	}
	for _, right := range []string{shown, ""} {
		line, ok := a.legendLine(left, right, width, paint)
		if !ok {
			continue
		}
		// The word is the LAST thing in the right label, and [app.legendLine]
		// closes with one space and one rule cell after it — so its columns are
		// arithmetic rather than a second layout.
		if mark != "" && right == shown {
			cols := ansi.StringWidth(mark)
			a.roomStop = hudSpan{from: width - 2 - cols, to: width - 2}
		}
		return line
	}
	return a.pal.dim(fit(left, width))
}

// roomFactsWord is that row's left label, plain, and how many of its leading
// cells are the state word — which is the one segment painted in the node's own
// ink rather than in the row's dim.
//
// A RUN'S PAGE ANSWERS FOR ITS OWN (roomorch.go): the facts under a node are a
// state, a clock and a spend, and a run has none of them. What it has instead is
// a tank, and the tank is the fact that cannot be left off this row.
func (a *app) roomFactsWord(node *taskNode, width int) (string, int) {
	// Keep a visible seam even when a dependency or phase has a long name.
	room := max(width-roomHeadFurniture-12, 0)
	if a.orchOpen() {
		return a.orchHeadWord(room), 0
	}
	if node == nil {
		return "", 0
	}
	facts := a.roomHeadFacts(node)
	// THE STATE IS THE ROW'S OWN LEAD AND IT KEEPS ITS GLYPH. The mark is the
	// roster's cell for this node ([app.roomMark]) and it belongs beside the word
	// it illustrates rather than in front of a path — which is where it used to
	// sit, one cell into a breadcrumb it had nothing to do with.
	lead := ""
	if facts[0].known() {
		lead = a.roomMark(node) + " " + facts[0].full
		facts = facts[1:]
	}
	if lead == "" {
		return rowTail(facts, room), 0
	}
	lead = fit(lead, room)
	cols := ansi.StringWidth(lead)
	tail := rowTail(facts, room-cols-len(rowSep))
	if tail == "" {
		return lead, cols
	}
	return lead + rowSep + tail, cols
}

// roomBackPress uses the rendered Back label's padded target. Whitespace and
// breadcrumb separators are not navigation controls.
func (a *app) roomBackPress(x, y int) bool {
	if !a.roomBackAt(x, y) {
		return false
	}
	a.closeRoom()
	return true
}

func (a *app) roomBackAt(x, y int) bool {
	return a.roomOpen() && a.headHeight() != 0 && y == a.roomHeadRow() && a.roomBackSpan.holds(x)
}

// ── THE HEADER IS THE INSTRUMENT ────────────────────────────────────────────
//
// roomHeadWord is the trail row's left: WHICH PAGE THIS IS, the whole way down.
// It is ancestry alone now. The judgment and accountability facts it used to
// carry beside the path — the state, the clock, the spend, the call count, the
// model — are the row underneath ([app.roomFactsLine]), which is where they can
// be painted as figures instead of as a continuation of a place.
//
// WHY THEY ARE AT THE TOP OF THE FRAME AT ALL, on either row. A person comes to
// a task to steer and to check. The check is one glance, and a glance is a fixed
// number of cells at the top of the frame — so a room's numbers gather there
// instead of dribbling down a scroll that has to be read to be summed (lens.go's
// [receiptsHeader]; the conversation keeps its per-turn receipts, which is the
// opposite posture and the right one out there).
//
// It is built PLAIN, without paint, because the whole line is painted once by
// [app.legendLine]: a hue nested inside a hue ends at the inner one's reset, and
// the rest of the line would fall back to the terminal's default mid-sentence.
func (a *app) roomHeadWord(width int) string {
	word, _ := a.roomHeadParts(width)
	return word
}

// roomHeadParts is that line and where its crumbs landed, in the label's own
// columns. The two are built together for the reason every hit map on this
// surface is written by the render that drew it: a trail laid out twice is a
// trail a click can miss by exactly the difference between the two layouts.
//
// The current task keeps its identity. The way back gets a reserved target
// when the middle of the path can fold enough to keep both; otherwise the path
// uses the whole row and its root remains a way out.
func (a *app) roomHeadParts(width int) (string, []crumbHit) {
	room := max(width-roomHeadFurniture, 0)
	// Reserve the padded Back target before fitting the middle of the path.
	// If that would cut the current task's identity, the path keeps priority.
	withBack := width - headLabelAt - ansi.StringWidth(" "+roomBackWord+" ") - 3
	line, hits, whole := a.roomCrumbLine(max(withBack, 0))
	if !whole {
		line, hits, _ = a.roomCrumbLine(room)
	}
	if a.orchOpen() {
		// A RUN'S PAGE HAS A TRAIL AND NO CRUMB THIS WINDOW CAN OPEN (roomorch.go):
		// its steps are the run's own goals and none of them is a node in this
		// conversation's graph, so the row is drawn and records nothing. Its facts
		// are the tank, and the tank is on the row below with everything else that
		// is a figure.
		return line, nil
	}
	return line, crumbsAt(hits, headLabelAt)
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
	return a.roomFactsOf(node).ranked()
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
	// THE OPEN CALL'S OWN SENTENCE, WHEN THE PULSE SAYS ONE IS IN FLIGHT. The
	// pulse is the one source that knows a request went out and has not come
	// back — the page's own rows cannot, because a model call that is still
	// streaming has written no entry yet, which is the exact defect #837 was
	// filed over: one call ran twelve minutes with the header saying nothing
	// about it but the task's own age and a tool count. While a call is open the
	// live segment is THE CALL'S clock and its rate, and when it comes back the
	// segment falls through to the ladder below — reverted, not replaced.
	if open := a.roomOpenCallWord(node); open != "" {
		return open
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

// roomOpenCallWord is the in-flight call's own sentence, in the exact words the
// chat foot spells the live rate — or "" when no call is open.
//
// IT IS THE FOOT'S VOCABULARY AND NEVER A SECOND ONE. The rate is the same
// [PhaseNews.Rate] the status row's right edge draws ([app.liveRiderAt]), spelled
// by the same " tok/s" that line builds, so the two rows can never disagree on
// what "how fast" looks like. The clock is the call's own elapsed in the live
// spelling every moving figure on this surface already uses ([countUpWord]), and
// it needs no wakeup of its own: the frame already repaints on the tick while a
// room is open, so a ten-minute call ticks.
//
// THE PREDICATE IS THE PULSE'S AND NOT A GUESS. [session.TaskBeatRow.Working] is
// "the last start has not been answered by a finish", and a node with no pulse
// at all — an unknown id, a landed node whose pulse was taken away with the
// landing, a build that cannot read the file — answers false and draws nothing,
// which is the emptiness law and not a failure to draw.
func (a *app) roomOpenCallWord(node *taskNode) string {
	if node.state != session.TaskRunning {
		return ""
	}
	if !a.room.beatRead || !a.room.beat.Working() {
		return ""
	}
	var fields []rowField
	if clock := countUpWord(a.now().Sub(a.room.beat.RequestStarted)); clock != "" {
		fields = append(fields, rowSay(clock))
	}
	if news, ok := a.roomPhase(); ok && a.windowWorking() && news.Rate > 0 {
		// The rate rides only while it is being measured, which is the foot's own
		// law ([app.liveRiderAt]): a rate nobody is producing is nothing, never
		// `0 tok/s`.
		fields = append(fields, rowSay(tokenWord(int(news.Rate))+" tok/s"))
	}
	return rowLed(fields, rowUnbounded)
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
	// The expanded page already has the family tree beside its transcript.
	if a.roomOrganized() {
		return nil
	}
	// FAMILY DETAILS BELONG TO THE TASK COLUMN. The header spans the window,
	// but its child summary must stop where the adjacent roster begins. Both
	// frame drawing and height accounting use this same width decision.
	width = min(width, a.bodyWidth())
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
	kids, _ := a.railKin()
	// THE ROWS ARE A BUDGET AND THE NAMES ARE FITTED TO IT. These lines are dim
	// telemetry with a hard cap of [roomKinRowCap] rows, so a relative's name
	// that arrives whole (taskident.go's [taskTitleOf]) is cut HERE, where the
	// width is known — the alternative is what a name spilled over the cap
	// actually looks like: the block wrapping to three rows and the last one
	// ending on a bare `—` with the state word cut off the bottom of it.
	inner := width - ansi.StringWidth(roomKinIndent)
	var lines []string
	// WHO ASKED FOR THE WORK IS ON THE TRAIL NOW AND NOT ON A LINE DOWN HERE.
	// This block used to open with `part of: <the parent's title>` — one level of
	// ancestry, said as a fact you could not press, one row under a trail that was
	// claiming in the same breath that this work hung off the conversation. The
	// breadcrumb says the whole chain and every step of it is a door
	// (roomcrumbs.go), so the sentence is gone rather than drawn twice: the same
	// rule the state word already holds these rows to — a header that said the
	// same thing twice would be a header read twice to learn one thing.
	//
	// WHAT THIS WORK HANDED OUT STAYS, because a trail goes UP. Children are not
	// on the way to anywhere, they are what is happening underneath, and the roster
	// is the only other place they are drawn. Each piece keeps the state word it
	// wears everywhere else on the surface.
	// everywhere else on the surface. The order is [app.railKin]'s, which is the
	// column's own: what needs a person first, then what is running, then what is
	// waiting, then what is over — with arrival order keeping the peace inside
	// each of those. It used to be arrival order alone, and on the block this line
	// exists for — a run that hands four pieces out and finishes them one by one —
	// that put every settled piece in front of the ones still going.
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
// AND THAT ONE WORD IS `waiting` AND NOT `queued`. `queued`
// ([railGroupWords], [railIdle]) says nothing is in this child's way but a slot,
// and no slot is coming: its prerequisite is the piece of work whose room this
// is, sitting there waiting on the person reading this very page. The column's
// `waiting` group ([railParked] — admitted and BLOCKED) is the honest answer, so
// the word is read out of that table rather than spelled a second time here.
func (a *app) roomKinWord(node *taskNode) string {
	if node.state == session.TaskQueued && !node.stopped && a.railWaits(node) != "" {
		return railGroupWords[railParked]
	}
	return a.roomStateWord(node)
}

// roomNode is the node the open room is about, or nil when this surface has
// never had an update for it.
//
// A GUEST PAGE ANSWERS WITH ITS OWN NODE AND NEVER WITH THE LOCAL GRAPH'S, and
// this is the single most important line in the guest lane. Task ids restart with
// every conversation ([session.TaskIndexEntry.ID] states it), so a page opened
// onto another conversation's task 7 is very often standing beside THIS
// conversation's task 7 — a different piece of work, with a different title, a
// different model and a different state. Every reader of this function is a
// header, a state word, a clock, a model, a kin row or an action; answering any
// of them out of [app.tasks] would describe the wrong work and then let a key act
// on it. The guest's node is built once from the row that was pressed and is
// never in that map ([taskGuest.node]).
// roomNodeModel is the id the OPEN ROOM'S NODE is answering on, for the seams
// that need the service behind it rather than the name on screen. Empty when no
// node is open, which every caller reads as "no service to name".
func (a *app) roomNodeModel() string {
	node := a.roomNode()
	if node == nil {
		return ""
	}
	return strings.TrimSpace(node.model)
}

func (a *app) roomNode() *taskNode {
	if a.room == nil {
		return nil
	}
	if guest := a.room.guest; guest != nil {
		return guest.node
	}
	if plan := a.room.plan; plan != nil {
		return plan.node
	}
	return a.tasks[a.room.id]
}

// roomMark is the node's state in one cell, UNPAINTED — [app.railGlyph]'s glyph
// without its hue, because the header wears one hue for its whole length.
//
// It is the roster's own cell ([app.taskStateMark]) and not a second table. The
// copy that used to live here had drifted: it knew the refusal mark but neither
// the stop's mark nor the halt's `!`, so a node a person stopped wore a
// failure's cross on its own page and the stop's mark one keypress away.
func (a *app) roomMark(node *taskNode) string {
	if node == nil {
		return a.icon(tokens.GQueued)
	}
	return a.taskStateMark(node)
}

// The trail itself is roomcrumbs.go's — [app.roomCrumbs] is the model and
// [app.roomTrail] reads it out as this sentence. It used to be built here, over
// [app.roomPath], and it named the root and the open room and nothing between
// them: the ancestry the engine had modelled all along was one dim line further
// down, spelled as a fact rather than as a place, and only ever one level of it.

// guestOwnerName is what to call the conversation a page is being read through,
// and [taskAwayWord] for one nothing has named — which is the same word the row
// and the recovery card use for a window with no title, so the three places a
// person meets this fact sound like one program.
func guestOwnerName(owner string) string {
	if owner = strings.TrimSpace(owner); owner == "" {
		return taskAwayWord
	}
	return owner
}

// roomStateWord is what the node is doing, in the engine's own vocabulary where
// it has one (task.go's merge words).
func (a *app) roomStateWord(node *taskNode) string {
	// A QUESTION SOMEBODY ELSE IS ANSWERING IS STILL THAT QUESTION. This line
	// used to read `awaiting review` for a card the settle policy had handed to
	// the model, which was a fourth word for one reading and said nothing about
	// what the question actually was. The header reads the reading's own sentence
	// — `your call · nobody could check it` — and WHO is holding it is the card's
	// to say, one keypress away (tasksettle.go, docs/design/task-states/DESIGN.md).
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
		// A ROW NEVER READS A BARE `waiting`. The word is half the news and the
		// reason is the half a person can act on, so the two travel together
		// wherever either is drawn ([session.TaskStatus.RowWord]).
		if node.waiting != "" {
			return a.taskStatus(node).RowWord()
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
			return a.taskStatus(node).RowWord()
		}
		return roomQueuedWord
	case session.TaskFailed:
		// A PERSON ENDING WORK IS NOT A FAILURE, and the header is where that
		// difference is read: "failed" sends somebody looking for a fault, and
		// the fault is that they pressed stop (session's TaskNotice.Stopped).
		if node.stopped {
			return taskStoppedByPerson
		}
		// AND `failed` IS GONE. The engine keeps the state's name and a person
		// reads the reading's own word — `incomplete` — because "failed" sends
		// somebody looking for a fault and four of the five ways a run ends this
		// way are not one (docs/design/task-states/DESIGN.md). The reason itself is
		// on the rail's row under the node; the header has one line and spends it
		// on the state.
		return a.taskStatus(node).Word
	case session.TaskUnverified:
		// NOT THE MERGE SENTENCE, for the reason the rail states in the same words
		// (task.go's [app.railUnder]): a node whose landing is somebody's call
		// wears session's "aborted" merge exactly as a stopped one does, so the
		// switch below said "stopped" about work that ran to the end. What it is
		// waiting for is a person, and the header says the word the card and the
		// rail already say (tasktier.go).
		return a.taskStatus(node).Word
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

// The two words the header has that nothing else on this surface says.
//
// A THIRD ONE, `failed`, IS GONE. It was the header's private name for a landing
// the reading calls `incomplete`, and it reported a finding nobody made about
// the four endings in five that are not faults.
const (
	roomQueuedWord = "queued"
	roomDoneWord   = "done"
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
// It wears NO REASONING SUFFIX, because THE DIAL IS THE CONVERSATION'S: the
// ":high" is spliced on by lending a.model its suffixed form for the length of
// one call (view.go's [app.statusRow]), so a task model wearing it would be a
// fact about the model the SESSION is running told in smaller print. Reading the
// node's model from the node keeps it out of the splice by construction.
//
// IT DOES WEAR A SERVED RIDER, and it is the NODE'S (lanes.go's
// [app.roomLaneRider]). It did not until the news started saying which piece of
// work it was about: the sighting desk was keyed by model, so the only machine
// this row could have named was whichever answer on that model id landed last —
// the conversation's, most often — and drawing that here would have been the
// same misreading as the reasoning splice. The rider is appended by the caller
// ([app.identityParts]), which is where the row's width ladder lives.
func (a *app) roomModelWord() string {
	node := a.roomNode()
	if node == nil {
		return ""
	}
	if node.nextModel != "" {
		return "next model " + modelBase(node.nextModel)
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
	reading := a.restoreRoomReading()
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
	// THE READING GUTTER IS TAKEN OUT FIRST AND GIVEN BACK LAST, exactly as in
	// the conversation (gutter.go, and render.go's [app.layout] states the law).
	// A task's page is a transcript and is read as one; it stood flush against
	// the terminal's left edge for the same reason the conversation did, and at
	// the same cost. A RUN'S PAGE IS NOT — a graph of cards is not a paragraph —
	// which is why the branch that returns one does so above this line.
	inner := gutterInner(width)
	// THE WORK TAB IS WHAT THE TASK CHANGED, and it is the same page with a
	// different body: the head, the box and the keys are the room's (roomtabs.go).
	if room.tab == roomTabWork {
		out := a.roomWorkRows(inner)
		gutterPass(out, width)
		a.hoverPass(out, width)
		room.rows, room.width, room.height, room.dirty = out, width, height, false
		return out
	}
	out, closed := a.deckRows(room.deck(), inner)
	// A RUN'S TASK SHOWS ITS PARTS UNDER ITS TRANSCRIPT, each drawn as the rail
	// draws a task (planroom.go).
	if parts := a.planRoomPartRows(inner); len(parts) > 0 {
		out = append(out, parts...)
		closed = false
	}
	if room.harnessProgress != "" && !room.done {
		out = append(out, row{text: a.pal.dim(fit(room.harnessProgress, inner)), entry: -1})
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
	switch {
	case room.guest != nil && room.guest.lost:
		// THE CONVERSATION UNDER THIS PAGE WAS REPLACED, and there is nothing to
		// retry. Everything above stays — it is what this window did read, and it
		// was true when it read it — and this is the last line of it.
		out = append(out, row{text: a.pal.dim(fit(taskGuestGoneWord, inner)), entry: -1})
	case room.readFailed && len(out) > 0:
		// A PAGE THAT DID READ SOMETHING SAYS THE FAILURE UNDER IT. An EMPTY one
		// falls through instead, because a lone `couldn't read…` over a blank body
		// is the very page this branch was written to prevent — everything the
		// surface already knows about the work goes on first, and the failure is
		// the last line of it ([app.roomRecordRows] draws both).
		out = append(out, row{text: a.pal.dim(fit(roomReadFailedWord, inner)), entry: -1})
	case len(out) == 0:
		out = a.roomRecordRows(out, inner)
	}
	// AND A LIFE WAITING ON A REQUEST SAYS WHAT THE REQUEST IS DOING, under
	// whatever the page already holds — the transcript of a worker that asked for
	// its work to be divided, or, on a page nothing has been written to yet, the
	// brief. It is the next thing that will happen to this work, so it stands
	// where the next thing would appear ([app.roomCallRow]).
	if call, ok := a.roomCallRow(inner); ok {
		out = append(out, call)
	}
	// AND A READING PAGE WITH NO WAY TO ASK ITS OWNER SAYS SO, once, under
	// whatever it did read. It is not a refusal and not an error — the transcript
	// above it is real — it is the one thing the page cannot know, said rather
	// than papered over with a state word that stopped being true (taskowner.go's
	// [app.roomGuestStale]).
	if a.roomGuestStale() {
		out = append(out, row{text: a.pal.dim(fit(roomGuestStaleWord, inner)), entry: -1})
	}
	// AND A CONVERSATION THAT HAS STOPPED AND IS WAITING ON SOMEBODY SAYS SO,
	// under what it has done so far. The roster cannot say it — a node sitting on
	// a question is still `running` — so a page reading somebody else's work drew
	// a clock over work that had not moved since somebody was asked something
	// (taskowner.go's questions lane).
	//
	// IT IS DIM AND NOT AMBER, AND THAT IS THE HUE LAW RATHER THAN AN OVERSIGHT.
	// Amber is waiting on YOU and nothing else (docs/design/questions/DESIGN.md);
	// this question is waiting on the window that owns the work, this page has no
	// key that would answer it, and a row here in the colour that means "press
	// something" would be asking a person for a keystroke that does not exist.
	if asked, waiting := a.roomGuest().waiting(); waiting {
		if head := strings.TrimSpace(asked.Head); head != "" {
			line := a.icon(tokens.GNeedsHuman) + " " + head + railSep + roomGuestAskedWord
			out = append(out, row{text: a.pal.dim(fit(line, inner)), entry: -1})
		}
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
		// A NODE THAT NEEDS A LOOK DOES NOT SAY `finished` HERE. Its question is
		// standing on the block above the box, in this room as on every other
		// page (tasksettle.go), and a foot that read `this task has finished —
		// say it to main` over it told a person the one thing that was not true
		// about the page they were looking at (#767). Every other landing keeps
		// the foot it has.
		if !a.roomLandingAsking() {
			// AND IT NAMES A DOOR (roomrefusal.go). `task finished — esc to
			// return` was the whole of what this row said for a year, and esc is
			// already on the legend and on the focus header above it; where the
			// words in the box can go was on neither. On a page read through
			// somebody else's conversation the door is the OWNER'S
			// ([app.roomDoneRefusal]): `say it to main` there would aim the words
			// at this window's conversation, which is the wrong one.
			out = append(out, row{text: a.pal.dim(a.roomDoneRefusal().fit(inner)), entry: -1})
		}
	}
	// THE GUTTER, BEFORE THE PASS THAT PAINTS THE WHOLE ROW (gutter.go). The
	// room's own foot is asked for by name because a node that needs a look draws
	// its answers with no entry to hang them on, so the deck walk cannot reach it.
	gutterPass(out, width)
	// THE POINTER, LAST, exactly as in the conversation (render.go's layout).
	a.hoverPass(out, width)
	a.restoreRoomAnchor(reading, out)
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
// AND A PAGE THAT HAS NOT LANDED SPENDS THEM ON THE INSTRUCTION. A task opened
// the second it is started is the commonest way to reach these rows and it was
// the worst of them: the only thing under the header was `loading this task's
// conversation…` — a sentence about the surface's own plumbing, on a page opened
// by somebody who wanted to see the work. What that person is owed is already in
// hand before any journal is read, because the contract is frozen at admission
// (task.go's [taskNode.brief]): THE WORDS THEY ASKED FOR. So the brief goes on
// first, then what the engine last said the work is doing, then the one line
// about why there is nothing else — and every one of them comes off together the
// moment a block arrives, because this whole function is drawn only for a page
// with no blocks at all.
func (a *app) roomRecordRows(out []row, width int) []row {
	node := a.roomNode()
	if node == nil {
		// A page this surface never had a row for. There is nothing to add to the
		// blank except the reason it is blank.
		return append(out, row{text: a.pal.dim(fit(a.roomBlankWord(), width)), entry: -1})
	}
	// WHAT THE ROW SAYS THE WORK CAME TO, first, because it is the only thing here
	// a person came for. The report is prose somebody wrote, so it wraps.
	//
	// THE BRIEF STANDS IN ITS PLACE AND NEVER BESIDE IT, and WHICH of them is
	// drawn is decided by whether the work is over. A landing's own sentences are
	// what a finished page is for, and the instruction under them would push them
	// off a short frame. Work that is still going is the other way round: what a
	// person opening it wants is what they asked for, and a report on a running
	// row is a sentence about some EARLIER state of it — a far roster carries the
	// last outcome it heard beside a row that has since started again, and drawing
	// that over live work is the page claiming an ending that has not happened.
	// Either one falls back to the other, because a page with one of them and
	// neither drawn is the blank this whole function exists to prevent.
	instruction := firstNonEmpty(strings.TrimSpace(node.brief), strings.TrimSpace(node.assignment))
	said := instruction
	if a.room != nil && a.room.done {
		said = firstNonEmpty(strings.TrimSpace(node.report), instruction)
	} else if said == "" {
		said = strings.TrimSpace(node.report)
	}
	if said != "" {
		ink := a.pal.narr
		if !a.room.done {
			ink = a.pal.accent
		}
		for _, line := range wrap(said, width) {
			out = append(out, row{text: ink(line), entry: -1})
		}
		out = append(out, row{entry: -1})
	}
	// THEN WHAT THE WORK IS DOING RIGHT NOW, where the engine has said and the
	// header has not already spent its one word on it ([app.roomStartingSay]).
	if say := a.roomStartingSay(node); say != "" {
		for _, line := range wrap(say, width) {
			out = append(out, row{text: a.pal.dim(line), entry: -1})
		}
	}
	// AND LAST, WHY THERE IS NO TRANSCRIPT UNDER ANY OF IT — unless a request is
	// out on the work's behalf, which is the better answer to that question and
	// is drawn under these rows by the caller ([app.roomCallRow]). A request
	// thinking for three minutes under `nothing on this page yet` was the page
	// telling a person nothing was happening while something was.
	if a.roomCall() != nil {
		return out
	}
	return append(out, row{text: a.pal.dim(fit(a.roomBlankWord(), width)), entry: -1})
}

// roomCall is the request the open room's node is waiting on, and nil when there
// is none or when the page is over ([session.TaskPhaseNotice.Call]).
func (a *app) roomCall() *session.TaskCall {
	if a.room == nil || a.room.done {
		return nil
	}
	if node := a.roomNode(); node != nil {
		return node.phaseCall
	}
	return nil
}

// roomCallRow is that request drawn as one row: the thinking mark while the
// model thinks, the ladder's own sentence — which model, which of how many — and
// the request's facts after it ([app.callFields]).
//
//	✳ asking deepseek/deepseek-v4.1-flash · 1 of 2 · thinking 41s · ↓ 4,465 · deepinfra
//
// THE MARK IS THE THOUGHT'S OWN SLOT while reasoning is what is arriving, which
// is the one sign on this surface that means "the model is thinking", and it
// comes through the vocabulary's door ([app.icon]) so each terminal draws its own
// tier's spelling of it. Any other moment of the request — out with nothing
// back, parked, writing — wears the wait mark the room's live row wears between
// calls ([app.compactWaitMark]).
//
// THE SENTENCE LEADS AND IS KEPT WHOLE, and the facts after it shed from the
// right (rowfit.go's first law): which model is being asked is what the row is
// ABOUT, and a clock beside half a model's name is a clock about nobody.
func (a *app) roomCallRow(width int) (row, bool) {
	call := a.roomCall()
	if call == nil {
		return row{}, false
	}
	mark := a.compactWaitMark()
	if call.Phase == provider.CallThinking {
		mark = a.pal.dim(a.icon(tokens.GThought))
	}
	gutter := ansi.StringWidth(mark) + 1
	fields := append([]rowField{rowSay(a.roomNode().phaseFinding)}, a.callFields(call, a.now())...)
	said := rowLed(fields, width-gutter)
	if said == "" {
		return row{}, false
	}
	return row{text: mark + " " + a.pal.dim(said), entry: -1}, true
}

// roomBlankWord is the one line an empty page ends on: WHY there is nothing
// here, which is a different fact in each of four cases and was collapsed into
// two of them for as long as the loading branch returned early.
//
// The order is the order of certainty. A read on the wire has not answered yet
// and nothing else is known; a read that FAILED is not a page that is loading
// and never says so, which is the distinction the retry line carries; and under
// both of them sit the two honest endings a page with no read pending has —
// nothing has arrived yet, or nothing is left.
func (a *app) roomBlankWord() string {
	switch {
	case a.room == nil:
		return roomGoneWord
	case a.room.readFailed:
		return roomReadFailedWord
	case a.room.loading:
		return roomLoadingWord
	case !a.room.done:
		return roomYetWord
	}
	return roomGoneWord
}

// roomStartingSay is what the engine last said this work is DOING, for a page
// that has nothing of its own to draw yet: the difference between a task that
// has not started writing and a task nothing is happening to.
//
// A WAIT USES THE ROSTER'S COMPLETE EXPLANATION. Reasons can be fragments
// such as "its parts", so dropping the state leaves a sentence without a verb.
// Named phases already printed verbatim by the header remain absent here.
//
// A LANDED PAGE SAYS NONE OF IT. These three fields are reports of RIGHT NOW and
// the engine clears them at the landing (task.go); drawing a stale one over
// finished work would be the page claiming live activity that ended.
func (a *app) roomStartingSay(node *taskNode) string {
	if node == nil || a.room == nil || a.room.done {
		return ""
	}
	switch {
	case strings.TrimSpace(node.waiting) != "":
		return a.taskStatus(node).RowWord()
	case strings.TrimSpace(node.mending) != "":
		return strings.TrimSpace(node.mending)
	case strings.TrimSpace(node.tool) != "":
		return strings.TrimSpace(node.tool)
	}
	return ""
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
// SCROLLING UP AT THE TOP OF A RUNNING ROOM OPENS THE FOLD. Scroll is the universal read-history
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
	// FINISHED WORK OPENS ONLY BY DISCLOSURE. Scrolling back to the request,
	// including trackpad momentum at the top, must not expose the tool log.
	if room == nil || room.done {
		return false
	}
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
	if a.roomOrganized() {
		lane = "Give an instruction or ask about this task"
	}
	if a.room.orch != nil {
		// A RUN HAS NO WORKER TO TALK TO, so the box does not offer to steer one:
		// the sentence goes to the PLANNER, which reads it on its next call
		// (roomorch.go). The placeholder says whose ear it is for the reason it
		// names a node out here — the box is the same box either way, and "who is
		// listening" is the question it exists to answer.
		lane = orchSteerLane + roomSteerBack
	}
	if a.room.plan != nil {
		// A RUN'S TASK TAKES THE WORDS AS A NOTE (planroom.go), and the box says
		// so: the worker reads a note at its next step, not the instant it is sent.
		lane = taskPlanNoteWord + roomSteerBack
	}
	if a.roomIsGuest() {
		// A BORROWED PAGE DOES NOT OFFER A KEYBOARD IT DOES NOT HAVE. This window is
		// a second view onto a conversation another window is driving, and the
		// steering door is genuinely absent here ([app.roomSteerDoors]) — so a
		// placeholder reading `Steer <task>…` would be the box promising a delivery
		// nothing behind it can make, which is the one thing a placeholder must
		// never do. It says what the page IS instead, and keeps the way out.
		lane = roomGuestLane + roomSteerBack
	}
	// A GUEST PAGE KEEPS THE READING WORD WHEN THE WORK LANDS. The finished
	// refusal below is about a box that could once steer and now cannot; this
	// one never could, and the reading word is exactly as true of a landed task
	// as of a running one. The foot beside it carries the owner-aware finished
	// sentence ([app.roomDoneRefusal]), so nothing here has to.
	// AND A NODE THAT IS STILL SOMEBODY'S CALL KEEPS ITS STEER LANE. The work has
	// stopped, but the box has not: `[s] tell it` is one of the three answers
	// standing on the block above it, and what it does is point this box at this
	// task (tasksettle.go's [app.landingTell]). A placeholder reading `this task
	// has finished — say it to main` over a question the person is being asked
	// here would send them somewhere else to answer it, which is the room half of
	// the screen #767 was filed about.
	if a.room.done && !a.roomIsGuest() && !a.roomLandingAsking() {
		lane = a.roomFinishedRefusal().fit(room)
	}
	// The attachment/effort tray can precede the draft. Put the placeholder
	// on the actual prompt row so it never paints a second composer above it.
	prefix := ansi.Strip(lead) + prompt
	for i, line := range rows {
		if strings.HasPrefix(ansi.Strip(line), prefix) {
			out := append([]string(nil), rows...)
			out[i] = lead + a.pal.dim(prompt) + a.pal.dim(fit(lane, room))
			return out
		}
	}
	return rows
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
	if a.room == nil || a.roomOrganized() {
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
