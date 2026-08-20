package tui3

import (
	"reflect"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// AN ADAPTIVE RUN IS A PLACE TOO, AND THE PLACE IS A GRAPH.
//
// room.go says a task is a place you can go, and what you find when you get
// there is a TRANSCRIPT: one worker, saying things in order. An adaptive run
// (internal/orchestrate) is not one worker and does not happen in order. A
// planner amends the frontier on every completion while the scheduler launches
// whatever became free — so the only honest picture of it is the one the
// package's own doc comment describes: the graph is never designed, it
// CRYSTALLIZES, and a person watching it is watching a shape appear.
//
// So a run's page is not a transcript with a graph drawn on it. It is the
// graph: nodes as chips in topological layers, the planner's notes as thin
// lines between them, and one fuel gauge pinned at the top. Nothing on it
// scrolls past — a node that finished is still a chip, in the layer it was
// always in, wearing a different glyph.
//
// ── IT IS THE SAME ROOM, POINTED AT A RUN ──
//
// This file adds no second page mechanism. A run's page IS a [taskRoom] whose
// [taskRoom.orch] is set, which means every promise room.go makes holds here
// with no restatement: esc leaves and the conversation is untouched, the rail
// stays on screen, the scroll is the room's own, the box under it is the
// room's box, copy mode freezes what is drawn, and the frame's geometry
// ([app.roomWindow], [app.bodyRows], [app.headHeight]) never learns there are
// two kinds of page. What this file owns is what a run's page IS: the layers,
// the chips, the card behind a chip, and the gate.
//
// ── THE POLL IS THE LANE ──
//
// A task's page is fed by a channel of events. A run has no such channel for
// its shape — the orchestrator publishes a [orchestrate.Snapshot] and the room
// reads it ([session.Agent.OrchestrateSnapshot]) — so the page POLLS, four
// times a second, and redraws only when what it read differs from what it drew.
// Four times a second is the rate at which a graph appearing reads as a graph
// appearing rather than as a page flickering, and it is cheap: a snapshot is a
// struct copy, not a render.
//
// The three EVENT kinds are the rest of it, and they carry what a snapshot
// cannot: the planner's note as it lands, the fuel warning at the 80% mark, and
// the PAUSE, which is a question. They arrive on the session's STANDING run lane
// (app.go's [app.watchRuns]) rather than on a turn's stream, because a run
// outlives the turn that asked for it and its gate has no turn to arrive on;
// [app.orchestrateEvent] folds them in through [app.orchNoteEvent],
// [app.orchFuelEvent] and [app.orchPauseEvent].
//
// ── THE DOORS ARE ASSERTED, NEVER REQUIRED ──
//
// [orchAgent] is a fourth optional interface on this surface, asserted at the
// moment a run's page opens, for [taskRoomAgent]'s reason exactly: a session
// with no orchestrator under it (session.Config.OrchestrateRunner is nil, which
// is orchestration off) keeps everything else it had and says so in one note.

// orchAgent is the three doors onto one adaptive run (internal/session's
// orchestrate.go).
type orchAgent interface {
	// OrchestrateSnapshot is the page's poll: the run's latest published shape,
	// or false when this session knows no such run.
	OrchestrateSnapshot(id string) (orchestrate.Snapshot, bool)
	// OrchestrateNodeJournal resolves a published node's session journal when it
	// exists. A node that has not started has no journal and answers false.
	OrchestrateNodeJournal(runID, nodeID string) (string, bool)
	// SteerOrchestrate appends one steering note. The planner sees it on its
	// next call, and steering outranks the plan.
	SteerOrchestrate(id, text string) error
	// ResolveOrchestrate answers one paused run: "topup:<dollars>", "finish" or
	// "stop".
	ResolveOrchestrate(id, answer string) (string, error)
}

// orchDoors is the run half of the agent under this surface, when it has one.
func (a *app) orchDoors() (orchAgent, bool) {
	doors, ok := a.agent.(orchAgent)
	return doors, ok
}

// ── the page's state ────────────────────────────────────────────────────────

// orchRun is one adaptive run as a page shows it: what the last poll read, what
// the lane has said since, where the reader's cursor is, and the trail of outer
// runs they walked in from.
type orchRun struct {
	// id is the run, as the session's doors name it, and goal is its sentence.
	// The goal is taken from the snapshot when there is one and from whoever
	// opened the page when there is not.
	id   string
	goal string

	// snap is the last shape read, and known says a read has ever succeeded. The
	// two are separate because an empty snapshot and no snapshot are different
	// facts: the first is a run whose planner has not laid a node yet, the second
	// is a run this session cannot see.
	snap  orchestrate.Snapshot
	known bool

	// seen is every node id this page has ever drawn, and fresh the ones that
	// arrived on the LAST poll — the one-interval "new" marker. The marker is one
	// interval by design: the graph growing is the signal, and the word is only
	// the punctuation under it, so it expires on the next read rather than
	// lingering as a badge on work that is already old.
	seen  map[string]bool
	fresh map[string]bool

	// pick is what the cursor is on: a chip, or one of the gate's answers. It is
	// the TARGET ITSELF and not an index into the ring, because the ring GROWS
	// UNDER IT — a run that added three nodes between two keypresses would
	// otherwise move the cursor without anybody pressing anything.
	pick orchTarget

	// card is the node whose detail is open, or "" for the graph, and link the
	// cursor over that card's navigable needs.
	card string
	link int

	// transcript is the node journal descended into from its card. It is another
	// level of this page, not another room: esc returns to the card and the room's
	// own scroll remains private from the main conversation.
	transcript string
	journal    []entry
	journalSet bool

	// following says the last poll found the open transcript's node still
	// running, which is what buys it ONE more read after the node stops — see
	// [app.orchRead], where the reason is stated.
	following bool

	// crumbs are the runs walked OUT of on the way in here, outermost first: a
	// node that is itself a nested run expands into its own snapshot, and esc
	// comes back out one level at a time.
	crumbs []orchCrumb

	// notes are the planner's lines as the LANE delivered them, and fuel its
	// last spend sentence. Both are what a page has before its first poll lands
	// and after a poll that could not resolve the run at all.
	notes []string
	fuel  string

	// steered is what the person typed into this page, echoed the moment it was
	// accepted rather than when a snapshot admits it, and acts what they answered
	// the gate with. Both are the page's own record of the person's half.
	steered []string
	acts    []string

	// gate is the unanswered pause, or nil.
	gate *orchGate
	// answered says the person has given the pause an answer the RUN has not
	// caught up with yet. It is what keeps the snapshot from raising the question
	// again in the gap between the answer and the resumption — the tank is topped
	// up on the far side of a poll, so for one interval the shape still says
	// paused and the page must not re-ask something already answered.
	answered bool

	// spots is what each drawn row answers to, by column — the chips on it, the
	// links inside a card, the gate's three answers. It is written by the layout
	// for the reason every other target on this surface is written there
	// (render.go's [hudSpan] callers): a hit-test that recomputed the geometry
	// would be measuring a row the frame has not drawn.
	spots [][]orchSpot
}

// orchTarget is one thing the cursor can be on. Exactly one field is set: a
// chip, or one of the gate's three answers. The gate's answers are IN THE SAME
// RING as the chips because they are the same gesture — a thing on the page that
// enter acts on — and a question with a keyboard of its own would be a second
// answer to "what does enter do in here".
type orchTarget struct {
	node   string
	answer string
}

func (t orchTarget) empty() bool { return t.node == "" && t.answer == "" }

// orchCrumb is one run left behind on the way into a nested one.
type orchCrumb struct {
	id   string
	goal string
	card string
	pick orchTarget
}

// orchGate is one unanswered [session.EventOrchestratePause]: the run has spent
// its tank, its in-flight nodes have landed, and nothing new will launch until
// a person says which of the three things happens next.
type orchGate struct {
	id   uint64
	text string // the session's own spend sentence
}

// orchSpot is one pressable span of one drawn row. Exactly one of node and
// answer is set: a chip or a link goes somewhere, an answer resolves the gate.
type orchSpot struct {
	span       hudSpan
	node       string
	run        string // set when the spot descends INTO a nested run
	transcript string
	answer     string
}

// key is this spot's identity for the pointer, and it is a STRING because an
// orchestrate node is named "n3" rather than numbered (taskstrip.go says the same
// about the parent seam). The pointer is held by key rather than by row for the
// reason the roster's is held by id: this page is re-laid every poll, so a hover
// stored as "row nine" would follow the redraw instead of following the chip
// (hover.go's [hoverOrch]).
//
// The kind is spelled into it because one node's page carries several spots ABOUT
// THAT NODE — its own chip, the run under it, its transcript — and three targets
// sharing a key would light together on a press that opens one of them.
func (s orchSpot) key() string {
	switch {
	case s.answer != "":
		return "answer:" + s.answer
	case s.run != "":
		return "run:" + s.run
	case s.transcript != "":
		return "transcript:" + s.transcript
	}
	return "node:" + s.node
}

// orchPage is a page under construction: the rows, and what each row answers
// to. The two are appended together so that a row and its targets cannot be
// built by two different passes over the same list.
type orchPage struct {
	rows  []row
	spots [][]orchSpot
}

func (p *orchPage) put(text string, spots ...orchSpot) {
	p.rows = append(p.rows, row{text: text, entry: -1})
	p.spots = append(p.spots, spots)
}

// orchOf is the run the open page is about, or nil when the body is anything
// else.
func (a *app) orchOf() *orchRun {
	if a.room == nil {
		return nil
	}
	return a.room.orch
}

// orchOpen reports whether the body region is a run's graph right now.
func (a *app) orchOpen() bool { return a.orchOf() != nil }

// The words a run's page says of itself.
const (
	// orchUnavailableWord is the degraded case: a session with no orchestrator
	// under it at all (session.Config.OrchestrateRunner nil is orchestration off).
	orchUnavailableWord = "adaptive runs unavailable — this session has no orchestrator"
	// orchEmptyWord is a run whose planner has not laid a node yet. It is not an
	// error and not a spinner: the first call is thinking, and the page says so.
	orchEmptyWord = "the planner has not laid a node yet"
	// orchUnknownWord is a run this session cannot see — the id names nothing
	// here — which is a different fact from an empty one and gets a different line.
	orchUnknownWord = "no shape published yet"
	// orchNewWord rides one poll on a node that has just appeared.
	orchNewWord = "new"
	// The four state words the header spends its last segment on.
	orchPausedWord  = "paused"
	orchDoneWord    = "done"
	orchOpeningWord = "opening"
	// orchStoppedWord is a run a PERSON ended (stop.go). It outranks "done" on
	// the header even though a stopped run IS done, because "done" reads as a
	// run that answered the goal and this one did not — it was cut, and the
	// chips underneath say where it got to.
	orchStoppedWord = "stopped"
	// orchNeedsLead opens a chip's dependency line, at the tiers where a chip is
	// a row rather than a cell.
	orchNeedsLead = "↳ needs: "
	// The two echoes the person's own half of the page leaves behind.
	orchSteerLead = "you steered · "
	orchActLead   = "you answered · "
	// The card's own words.
	orchNeedsHead  = "needs"
	orchDigestHead = "digest"
	orchErrHead    = "error"
	orchRunHead    = "nested run"
	orchAnswerHead = "answer"
	orchCardBack   = "esc · back to the graph"
	// orchGateLead opens the gate's question.
	orchGateLead = "out of fuel"
	// orchPlannerLead names the model the run is thinking with, beside the
	// gauge. It is spelled out rather than left as a bare model name because a
	// run's page already has two other models on it in a person's mind — the one
	// they are talking to and the one the nodes run on — and an unlabelled word
	// beside a dollar figure reads as a third mystery.
	orchPlannerLead = "planner: "
	// orchSteerLane is what the box says it is talking to while a run's page is
	// open (room.go's [app.roomSteerLaneRows] splices the way out onto it).
	orchSteerLane = "Steer the planner"
)

// The graph's own glyph alphabet, and it is NOT the roster's.
//
// The rail spends its cells on a VERDICT — ✓ landed, ✗ failed, ? nobody judged
// (task.go's [app.railGlyph]) — because a rail row is a presence that goes away
// when the work comes home. A chip never goes away: it is a node in a shape, and
// what a person reads off it fifty times a minute is how FULL it is. So the
// three ordinary states are one circle at three fills — empty, half, solid —
// which reads as a ramp at a glance and needs no colour to do it. Only failure
// keeps the surface's own mark, because failure is the one state that is not a
// point on that ramp.
const (
	orchGlyphQueued  = "○"
	orchGlyphRunning = "◐"
	orchGlyphDone    = "●"
	orchGlyphPaused  = "⏸"
	// The linear tier's stand-ins, on [glyphQueuedASCII]'s terms: a shape a
	// screen reader cannot name is replaced by a character it can.
	orchGlyphQueuedASCII  = "o"
	orchGlyphRunningASCII = "*"
	orchGlyphDoneASCII    = "#"
	orchGlyphPausedASCII  = "="
)

// orchChipGap is what separates two chips on one line. Three cells, because two
// is the gap inside a chip between its glyph and its name and a separator has to
// be wider than the thing it separates.
const orchChipGap = "   "

// orchLead is the cursor's two cells in front of every chip, picked or not. It
// is constant width so that moving the cursor never reflows the line, and it is
// the ROSTER's own marker (task.go's [railMark]) because it is the same gesture
// one surface over: this is where enter goes.
func (a *app) orchLead(picked bool) string {
	if !picked {
		return strings.Repeat(" ", ansi.StringWidth(railMark))
	}
	return a.pal.accent(a.linearMark(railMark, railMarkASCII))
}

// orchPollEvery is how often an open page re-reads its run. Four times a second
// is where a graph appearing reads as an appearance rather than as a flicker,
// and a poll is a struct copy — the redraw behind it is what costs, and it is
// paid only when the shape actually changed.
const orchPollEvery = 250 * time.Millisecond

// ── opening, closing, polling ───────────────────────────────────────────────

// openOrchRoom enters a run's page. The first snapshot is read HERE rather than
// waited for, so the page opens with whatever shape exists instead of a quarter
// second of nothing.
//
// The command that starts the poll is PARKED in [app.roomPump], the way
// [app.openRoom] parks its lane: this is called from event folds and from key
// paths that return no command of their own, and one drain point is worth more
// than four return values.
func (a *app) openOrchRoom(id, goal string) {
	if _, ok := a.orchDoors(); !ok {
		// THE BUILD GUARD, room.go's exactly: the doors are an assertion and not a
		// compile-time requirement, so a surface driven by an agent that has never
		// heard of an adaptive run says so and stays in the conversation.
		a.note(orchUnavailableWord)
		return
	}
	a.roomGen++
	run := &orchRun{
		id: id, goal: goal,
		seen:  map[string]bool{},
		fresh: map[string]bool{},
	}
	a.room = &taskRoom{
		// A RUN IS NOT A NODE, so the page carries no node id: the room's id is
		// the tasker's counter and this page belongs to no row of it. Everything
		// keyed on that id — the frozen clock, the model word, the rail's
		// highlight — reads zero and draws nothing, which is the honest answer.
		id: 0, title: firstNonEmpty(goal, id), gen: a.roomGen,
		unfolded: map[int]bool{},
		live:     -1,
		think:    -1,
		mdAt:     a.now(),
		stick:    true,
		dirty:    true,
		orch:     run,
	}
	a.orchLive = id
	a.sel = -1
	a.dropHover()
	a.orchRead()
	// A SECOND ARGUMENT THAT NAMES A NODE IS A DOOR TO THAT CARD. Existing
	// callers also pass the run's goal here, so only a node present in the first
	// snapshot changes the page's focus; everything else remains its title.
	if run.known {
		if _, ok := orchNodeOf(run.snap, goal); ok {
			a.orchCardOpen(goal)
		}
	}
	a.touch()
	a.roomPump = tea.Batch(orchTick(a.room.gen), a.wake())
}

// orchShowing reports whether this run's page is the one on screen.
func (a *app) orchShowing(id string) bool {
	run := a.orchOf()
	return run != nil && id != "" && run.id == id
}

// orchTick asks for the next poll.
func orchTick(gen int) tea.Cmd {
	return tea.Tick(orchPollEvery, func(time.Time) tea.Msg { return orchPollMsg{gen: gen} })
}

// orchPoll re-reads the run and re-arms. A generation that no longer matches is
// a page that was closed or replaced while its tick was in flight, and it stops
// the clock rather than restarting it — the room's own generation device
// (room.go), applied to a lane that ticks instead of one that streams.
func (a *app) orchPoll(gen int) tea.Cmd {
	if a.room == nil || a.room.orch == nil || a.room.gen != gen {
		return nil
	}
	a.orchRead()
	return tea.Batch(orchTick(gen), a.wake())
}

// orchRead takes one snapshot and marks what is new in it.
//
// A READ THAT RESOLVES NOTHING CHANGES NOTHING. The session answers false for a
// run it does not know, and a page that blanked itself on that answer would
// throw away the notes and the gate the LANE delivered — which are the only
// things it has while the core lands behind the contract.
func (a *app) orchRead() {
	run := a.orchOf()
	doors, ok := a.orchDoors()
	if run == nil || !ok {
		return
	}
	snap, ok := doors.OrchestrateSnapshot(run.id)
	if !ok {
		return
	}
	// THE TRANSCRIPT FOLLOWS A LIVE NODE, AND ONE READ PAST ITS LAST. The journal
	// is written by the worker and the state is published by the scheduler, so the
	// two land in either order — a page that stopped re-reading the instant the
	// shape said done would keep whichever lines lost that race off the screen
	// until somebody walked out of the transcript and back into it.
	if run.transcript != "" {
		if node, found := orchNodeOf(snap, run.transcript); found {
			live := node.State == orchestrate.Running
			if live || run.following {
				a.orchReadTranscript()
			}
			run.following = live
		}
	}
	// THE FIRST READ MARKS NOTHING. "New" means "this arrived while you were
	// watching", and a page that opened with every chip wearing the word would be
	// telling a person that a run they walked in on has just been invented.
	fresh := make(map[string]bool)
	for _, node := range snap.Nodes {
		if run.seen[node.ID] {
			continue
		}
		run.seen[node.ID] = true
		if run.known {
			fresh[node.ID] = true
		}
	}
	changed := run.changedBy(snap) || len(fresh) > 0 || len(run.fresh) > 0
	run.fresh, run.snap, run.known = fresh, snap, true
	if goal := strings.TrimSpace(snap.Goal); goal != "" {
		run.goal = goal
		a.room.title = goal
	}
	// A RUN THAT ANSWERED ITS OWN GATE TAKES THE QUESTION DOWN. The person can
	// top up from another surface, and a gate still on screen over a run that is
	// spending again is a question about a moment that has passed.
	// A RUN THAT IS OVER TAKES THE QUESTION DOWN TOO, and it has to be said
	// separately: a stopped run is un-paused with its tank still empty, so the
	// spend test alone would leave three answers on screen under a run that
	// cannot act on any of them.
	if run.gate != nil && (snap.Stopped || snap.Done ||
		(!snap.Paused && snap.Fuel.Spent < snap.Fuel.Cap)) {
		run.gate = nil
		changed = true
	}
	// AND A PAUSE OUTLIVES THE EVENT THAT ANNOUNCED IT.
	//
	// The gate is raised by the LANE ([app.orchPauseEvent]), and a lane is a thing
	// that happens once: a page walked out of and re-opened builds a fresh
	// [orchRun], so a run still sitting on its cap came back with "paused" in its
	// header and no question anywhere under it — a run nobody could answer from
	// the surface they were standing on. The snapshot is the durable half of the
	// same fact, so the page raises the question from it.
	//
	// The three answers only ever needed the run's own id ([app.orchAnswer]), so
	// a gate rebuilt here is answerable exactly as the lane's is; what it does not
	// have is the pause's own event id, which nothing reads.
	if !snap.Paused {
		// The run is spending again, so the answer has landed and the next pause
		// is a new question.
		run.answered = false
	}
	if run.gate == nil && !run.answered && snap.Paused && !snap.Done && !snap.Stopped {
		// THE SPEND SENTENCE IS THE RUN'S OWN ([orchestrate.Fuel.Gauge]), which is
		// what the lane's event carries too: the gate says "$2.00 of $2.00"
		// whichever half raised it, rather than the header's "/" form. One
		// question, one wording.
		run.gate = &orchGate{text: snap.Fuel.Gauge()}
		run.card = ""
		run.pick = orchTarget{answer: orchTopUp}
		changed = true
	}
	if !changed {
		return
	}
	a.room.dirty = true
	a.touch()
}

// changedBy reports whether a snapshot says anything the page is not already
// drawing. It is a FIELD-BY-FIELD comparison of what the page renders and
// nothing else, because the alternative — redrawing on every poll — is four
// layouts a second for a run that is thinking.
func (r *orchRun) changedBy(snap orchestrate.Snapshot) bool {
	old := r.snap
	if !r.known || old.Goal != snap.Goal || old.Planner != snap.Planner || old.Fuel != snap.Fuel ||
		old.Paused != snap.Paused || old.Done != snap.Done || old.Stopped != snap.Stopped ||
		old.Answer != snap.Answer ||
		len(old.Nodes) != len(snap.Nodes) || len(old.Notes) != len(snap.Notes) ||
		len(old.Steer) != len(snap.Steer) {
		return true
	}
	for i := range snap.Nodes {
		a, b := old.Nodes[i], snap.Nodes[i]
		if a.ID != b.ID || a.State != b.State || a.Digest != b.Digest ||
			a.Err != b.Err || a.Cost != b.Cost || a.Goal != b.Goal ||
			strings.Join(a.Needs, ",") != strings.Join(b.Needs, ",") {
			return true
		}
	}
	for i := range snap.Notes {
		if old.Notes[i] != snap.Notes[i] {
			return true
		}
	}
	for i := range snap.Steer {
		if old.Steer[i] != snap.Steer[i] {
			return true
		}
	}
	return false
}

// ── the three event kinds ───────────────────────────────────────────────────
//
// WHICH RUN AN EVENT IS ABOUT is the one seam this file cannot answer for
// itself. The session's doors name a run with a STRING ([session.Agent]'s three
// methods) and the lane names it with the NUMBER in [session.Event.ID] — the
// same run spelled twice, because the event carries no string field for it.
// [orchRunOf] is the one place the two spellings meet, and it is the one thing
// that changes on the day the contract gives the event an id of its own.

// orchRunOf is the run an event names, or "" when it names none.
func orchRunOf(ev session.Event) string {
	if ev.ID == 0 {
		return ""
	}
	return itoa(int(ev.ID))
}

// orchRunFor is which run an event belongs to ON THIS SURFACE.
//
// TWO SPELLINGS OF ONE RUN IS THE COMMON CASE, not the exception, for as long as
// the lane counts runs and the doors name them. So an id that this session
// cannot resolve to a snapshot is not treated as a second run while a page is
// open: it is THIS run, named by the other spelling, and the event lands where
// the person is looking. An id that DOES resolve is taken at its word, because
// then it is a run the session can actually show — which is what a second run
// looks like.
func (a *app) orchRunFor(ev session.Event) string {
	id := orchRunOf(ev)
	if run := a.orchOf(); run != nil {
		if id == "" || id == run.id {
			return run.id
		}
		if doors, ok := a.orchDoors(); ok {
			if _, known := doors.OrchestrateSnapshot(id); !known {
				return run.id
			}
		}
	}
	return firstNonEmpty(id, a.orchLive)
}

// orchNoteEvent takes one [session.EventOrchestrateNote]: one line of what the
// planner is thinking, between two completions.
//
// It lands on the run's PAGE when the page is open and in the CONVERSATION when
// it is not — the same arrangement a task node's landing has (task.go): a
// person watching the graph reads the note where the graph is, and a person who
// has walked away still has it in the record when they come back.
func (a *app) orchNoteEvent(ev session.Event) {
	id := a.orchRunFor(ev)
	if id != "" {
		a.orchLive = id
	}
	note := strings.TrimSpace(ev.Text)
	if note == "" {
		return
	}
	if run := a.orchOf(); run != nil && run.id == id {
		run.notes = append(run.notes, note)
		a.roomTouched()
		return
	}
	a.note(orchNotePrefix + note)
}

// orchNotePrefix names the lane a dim line in the transcript came off, the way
// the harness lane's own notes do (harness.go's [app.noteHarness]).
const orchNotePrefix = "run · "

// orchFuelEvent takes one [session.EventOrchestrateFuel]: the gauge, and its
// warning at the 80% mark.
//
// The page keeps the sentence for the header to fall back on; the conversation
// gets it only when no page is open, because a gauge is already pinned at the
// top of one that is.
func (a *app) orchFuelEvent(ev session.Event) {
	id := a.orchRunFor(ev)
	if id != "" {
		a.orchLive = id
	}
	word := strings.TrimSpace(ev.Text)
	if word == "" {
		return
	}
	if run := a.orchOf(); run != nil && run.id == id {
		run.fuel = word
		a.roomTouched()
		return
	}
	a.note(orchNotePrefix + word)
}

// orchPauseEvent takes one [session.EventOrchestratePause] — the run has spent
// its tank — and it is the one orchestrate kind that OPENS A PAGE.
//
// A question outranks a panel, on the terms every other question on this surface
// states (harness.go, connect.go): the gate is drawn on the run's own page, so a
// gate raised while somebody is reading their settings has to bring the page it
// is drawn on with it. Nothing is answered here and nothing is decided: the run
// sits paused, resumable, until a person presses one of three keys.
func (a *app) orchPauseEvent(ev session.Event) tea.Cmd {
	id := a.orchRunFor(ev)
	if id != "" {
		a.orchLive = id
	}
	if id == "" {
		// A PAUSE THAT NAMES NO RUN OPENS NO PAGE. The gate is answered per run
		// ([session.Agent.ResolveOrchestrate] takes the id), so a page opened on a
		// run nobody named would draw three answers that cannot be given.
		a.note(orchNotePrefix + firstNonEmpty(ev.Text, orchGateLead))
		return nil
	}
	a.closeSettings()
	a.closeExpand()
	if !a.orchShowing(id) {
		a.note(orchNotePrefix + firstNonEmpty(ev.Text, orchGateLead))
		a.openOrchRoom(id, "")
	}
	run := a.orchOf()
	if run == nil {
		// No doors, so no page opened. The question is still a fact and the
		// transcript is where facts go when there is nowhere else to put them.
		return nil
	}
	run.gate = &orchGate{id: ev.ID, text: strings.TrimSpace(ev.Text)}
	// The gate is about the RUN, so the page shows the run: a card open over one
	// chip is a page whose question is off screen.
	run.card = ""
	// AND THE CURSOR MOVES TO THE QUESTION. It is the only thing on the page that
	// somebody has to answer, so it is what enter should mean the moment it is
	// raised — a person who presses enter on a paused run means the gate.
	run.pick = orchTarget{answer: orchTopUp}
	a.room.stick = true
	a.roomTouched()
	return a.takeRoomPump()
}

// The three answers, spelled as [session.Agent.ResolveOrchestrate] takes them.
const (
	orchTopUp  = "topup:1"
	orchFinish = "finish"
	orchStop   = "stop"
)

// orchAnswer resolves the gate. The run's own answer is what changes the run;
// this page only says what was asked for, and lets the next poll show what came
// of it.
func (a *app) orchAnswer(answer string) {
	run := a.orchOf()
	doors, ok := a.orchDoors()
	if run == nil || run.gate == nil || !ok {
		return
	}
	if _, err := doors.ResolveOrchestrate(run.id, answer); err != nil {
		// The engine's own sentence, kept: a gate that could not be answered is a
		// run still sitting on its cap, and a page that swallowed the reason would
		// leave a person pressing the same key again.
		run.notes = append(run.notes, err.Error())
		a.roomTouched()
		return
	}
	run.gate, run.answered = nil, true
	run.acts = append(run.acts, orchAnswerWord(answer))
	a.room.stick = true
	a.roomTouched()
}

// orchAnswerWord is the answer as the echo says it back.
func orchAnswerWord(answer string) string {
	switch answer {
	case orchTopUp:
		return "add $1"
	case orchFinish:
		return "finish with what we have"
	case orchStop:
		return "stop"
	}
	return answer
}

// ── steering ────────────────────────────────────────────────────────────────

// orchSteer is enter with a sentence in the box, while a run's page is open:
// the words go to the PLANNER, which sees them on its next call.
//
// The echo is immediate and it is the page's own record ([orchRun.steered]),
// not a guess about the snapshot: the planner may be mid-call, and a person who
// typed a sentence is owed the evidence that it was taken now rather than
// whenever the next shape is published.
//
// A REFUSAL RAISES THE ROOM'S OWN GUARD (room.go's [app.raiseGuard]) rather
// than inventing a second one. "This run is finished" and "there is nobody to
// talk to" are the same shape of fact a parked node produces, and the answer a
// person needs is the same one: send it to the head instead, or keep it.
func (a *app) orchSteer() tea.Cmd {
	run := a.orchOf()
	line := strings.TrimSpace(a.input.String())
	if run == nil || line == "" {
		return nil
	}
	doors, ok := a.orchDoors()
	if !ok {
		a.roomNote(orchUnavailableWord)
		return nil
	}
	if err := doors.SteerOrchestrate(run.id, line); err != nil {
		a.raiseGuard(line, err.Error())
		return nil
	}
	// A LINE STEERED AT THE PLANNER IS REMEMBERED TOO, on room.go's argument
	// exactly: the box on this page is the box in the conversation, and ↑ has to
	// bring back what you typed wherever you typed it (recall.go).
	a.remember(line)
	a.input.reset()
	a.endRecall()
	a.closeLists()
	run.steered = append(run.steered, line)
	a.room.stick = true
	a.roomTouched()
	return a.edited()
}

// ── the keyboard ────────────────────────────────────────────────────────────

// orchKey is the graph's claim on the keyboard, read from inside [app.roomKey]
// so that everything which outranks a room still outranks this.
//
// EVERY KEY IT TAKES IS TAKEN OVER AN EMPTY BOX, which is the rule the room and
// the conversation already share (room.go, input.go): a page whose keys ate the
// letters somebody was typing would be a page you cannot steer, and steering is
// half of what a run's page is for. So a sentence in the box means the box wins,
// every time, and the three gate keys are no exception.
func (a *app) orchKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	run := a.orchOf()
	if run == nil {
		return nil, false
	}
	empty := a.input.empty()
	switch msg.String() {
	case "esc":
		// ESC IS THE BREADCRUMB, WALKED BACKWARDS: the card first, then the nested
		// run it was opened from, and only when there is neither does the room's
		// own esc get the key and close the page.
		switch {
		case run.transcript != "":
			run.transcript, run.journal, run.journalSet, run.following = "", nil, false, false
			a.room.offset = 0
			a.roomTouched()
			return nil, true
		case run.card != "":
			run.card, run.link = "", 0
			a.roomTouched()
			return nil, true
		case len(run.crumbs) > 0:
			a.orchAscend()
			return nil, true
		}
		return nil, false

	case "enter":
		if !empty {
			return nil, false // the box is talking; [app.steer] takes it
		}
		return a.orchOpenPick(), true

	case "up":
		if !empty {
			return nil, false
		}
		a.orchMove(-1)
		return nil, true

	case "down":
		if !empty {
			return nil, false
		}
		a.orchMove(1)
		return nil, true
	}
	return nil, false
}

// THE GATE HAS NO LETTER KEYS OF ITS OWN, and that is a decision rather than an
// omission.
//
// The three offers this surface stacks above the draft answer to bare letters —
// y, n, r, m — and they can, because every one of them SUSPENDS THE BOX: while
// one is up there is no sentence being typed for a letter to belong to
// (consent.go states the modal rule in full). This question does not suspend
// anything. The conversation is free, the run's page still steers, and a person
// looking at a paused run may perfectly well answer it by typing at the planner
// instead — so a gate that claimed "f" would eat the first letter of "finish
// the client one first" and answer a question with it.
//
// So the answers are TARGETS on the page, walked with ↑/↓ and taken with enter
// exactly as the chips are, and pressed with a finger exactly as the chips are.
// The keys a person already has in a room are the keys that answer it, which is
// the parity this page is built on.

// orchRing is everything the cursor walks, in the order it walks it: the gate's
// answers first — a question outranks a picture — then the chips, layer by
// layer, in the order they are drawn.
//
// IT IS DERIVED FROM THE RUN AND NOT FROM THE LAST LAYOUT. A cursor that could
// only move over a page that had already been drawn would be a keyboard that
// does nothing until the frame catches up, which is exactly what a person gets
// on the first keypress after a page opens.
func (a *app) orchRing() []orchTarget {
	run := a.orchOf()
	if run == nil {
		return nil
	}
	var ring []orchTarget
	if run.gate != nil {
		for _, answer := range []string{orchTopUp, orchFinish, orchStop} {
			ring = append(ring, orchTarget{answer: answer})
		}
	}
	for _, layer := range orchLayers(run.snap.Nodes) {
		for _, node := range layer {
			ring = append(ring, orchTarget{node: node.ID})
		}
	}
	return ring
}

// orchMove walks the cursor: the chips on the graph, the links inside a card.
func (a *app) orchMove(delta int) {
	run := a.orchOf()
	if run == nil {
		return
	}
	if run.card != "" {
		links := a.orchCardLinks()
		if len(links) == 0 {
			return
		}
		run.link = (run.link + delta + len(links)) % len(links)
		a.roomTouched()
		return
	}
	ring := a.orchRing()
	if len(ring) == 0 {
		return
	}
	at := -1
	for i, target := range ring {
		if target == run.pick {
			at = i
			break
		}
	}
	switch {
	case at < 0 && delta > 0:
		at = 0
	case at < 0:
		at = len(ring) - 1
	default:
		at = (at + delta + len(ring)) % len(ring)
	}
	run.pick = ring[at]
	a.roomTouched()
}

// orchOpenPick is enter over an empty box: the chip under the cursor opens its
// card, and a link inside a card is followed.
func (a *app) orchOpenPick() tea.Cmd {
	run := a.orchOf()
	if run == nil {
		return nil
	}
	if run.card == "" {
		if run.pick.empty() {
			a.orchMove(1)
		}
		switch pick := run.pick; {
		case pick.answer != "":
			a.orchAnswer(pick.answer)
		case pick.node != "":
			a.orchCardOpen(pick.node)
		}
		return nil
	}
	links := a.orchCardLinks()
	if run.link < 0 || run.link >= len(links) {
		return nil
	}
	link := links[run.link]
	if link.transcript != "" {
		a.orchOpenTranscript(link.transcript)
		return nil
	}
	if link.run != "" {
		a.orchDescend(link.run)
		return a.takeRoomPump()
	}
	a.orchCardOpen(link.node)
	return nil
}

// orchOpenTranscript descends from a node card into that node's work. The card
// remains named so esc reveals precisely the place the reader came from.
func (a *app) orchOpenTranscript(node string) {
	run := a.orchOf()
	if run == nil || node == "" {
		return
	}
	run.transcript, run.journalSet, run.following = node, false, true
	a.orchReadTranscript()
	a.room.stick, a.room.offset = true, 0
	a.roomTouched()
}

// orchReadTranscript reads evidence, never an error state. A node which has not
// started simply has no file yet and its page says so.
func (a *app) orchReadTranscript() {
	run := a.orchOf()
	doors, ok := a.orchDoors()
	if run == nil || run.transcript == "" || !ok {
		return
	}
	var next []entry
	if path, found := doors.OrchestrateNodeJournal(run.id, run.transcript); found {
		next, _ = readRoomJournalTail(path, a.pal, 0)
	}
	if run.journalSet && reflect.DeepEqual(run.journal, next) {
		return
	}
	run.journal, run.journalSet = next, true
	if a.room != nil {
		a.room.dirty, a.room.stick = true, true
	}
}

const orchTranscriptTail = 400

func (a *app) orchTranscriptRows(page *orchPage, width int) {
	run := a.orchOf()
	if run == nil || !run.journalSet || len(run.journal) == 0 {
		page.put(a.pal.dim(fit("no transcript yet", width)))
		return
	}
	// showsWork for the reason a room sets it (workfold.go's [app.deckFolds]):
	// somebody descended from a chip into a node's transcript to read what that
	// node did, and a page that collapsed it into "▸ worked · 6 tool calls"
	// would answer that gesture with the one line they already had.
	rows, _ := a.deckRows(deck{
		entries: run.journal, unfolded: map[int]bool{}, workOpen: map[int]bool{}, showsWork: true,
	}, width)
	if omitted := len(rows) - orchTranscriptTail; omitted > 0 {
		page.put(a.pal.dim(fit("… "+itoa(omitted)+" earlier lines", width)))
		rows = rows[omitted:]
	}
	for _, line := range rows {
		page.put(line.text)
	}
}

// orchCardOpen puts one node's card up, with the cursor at the top of its links.
func (a *app) orchCardOpen(id string) {
	run := a.orchOf()
	if run == nil || id == "" {
		return
	}
	run.card, run.link, run.pick = id, 0, orchTarget{node: id}
	a.room.stick = false
	a.room.offset = 0
	a.roomTouched()
}

// orchDescend expands a node that is itself a run INTO its own snapshot: the
// outer run goes on the breadcrumb, and this page becomes the inner one.
//
// It reuses the page rather than opening a second one, because a nested run is
// not a second place — it is the same place one level down, and the trail across
// the top is what says so (room.go's [app.roomTrail] states the rule for a
// task's page).
func (a *app) orchDescend(id string) {
	run := a.orchOf()
	if run == nil || id == "" || id == run.id {
		return
	}
	run.crumbs = append(run.crumbs, orchCrumb{
		id: run.id, goal: run.goal, card: run.card, pick: run.pick,
	})
	run.id, run.goal = id, ""
	run.snap, run.known = orchestrate.Snapshot{}, false
	run.seen, run.fresh = map[string]bool{}, map[string]bool{}
	run.pick, run.card, run.link = orchTarget{}, "", 0
	run.transcript, run.journal, run.journalSet, run.following = "", nil, false, false
	run.notes, run.fuel, run.steered, run.acts, run.gate = nil, "", nil, nil, nil
	a.room.stick, a.room.offset = true, 0
	a.orchRead()
	a.roomTouched()
	// THE POLL IS NOT RE-ARMED HERE, and that is the bug this line would be. The
	// clock is keyed to the ROOM's generation and descending does not open a new
	// room — so the tick already in flight is still the page's own, and a second
	// one started beside it would poll twice as often, and four times after the
	// next level down. What the inner run needed was one read, and it has had it.
}

// orchAscend is esc out of a nested run, back to the one it hung off.
func (a *app) orchAscend() {
	run := a.orchOf()
	if run == nil || len(run.crumbs) == 0 {
		return
	}
	back := run.crumbs[len(run.crumbs)-1]
	run.crumbs = run.crumbs[:len(run.crumbs)-1]
	run.id, run.goal = back.id, back.goal
	run.snap, run.known = orchestrate.Snapshot{}, false
	run.seen, run.fresh = map[string]bool{}, map[string]bool{}
	// The card comes back too. A person who walked into a nested run from a chip's
	// card came from a place, and esc is "back", not "back to the top".
	run.card, run.pick, run.link = back.card, back.pick, 0
	run.transcript, run.journal, run.journalSet, run.following = "", nil, false, false
	run.notes, run.fuel, run.steered, run.acts, run.gate = nil, "", nil, nil, nil
	a.room.stick, a.room.offset = true, 0
	a.orchRead()
	a.roomTouched()
}

// ── the pointer ─────────────────────────────────────────────────────────────

// orchPress resolves a click on a run's page and reports whether it took it.
//
// A chip and a link are targets by ROW AND COLUMN, because a layer is several
// chips on one line, so this is the same kind of hit-test the strip and the
// offer rows do (taskstrip.go, harness.go). A press that lands on no target
// falls through untouched — the empty parts of this page are still the way out
// of the room (app.go's [app.press]).
func (a *app) orchPress(x, y int) bool {
	spot, ok := a.orchSpotAt(x, y)
	if !ok {
		return false
	}
	switch {
	case spot.answer != "":
		a.orchAnswer(spot.answer)
	case spot.run != "":
		a.orchDescend(spot.run)
	case spot.transcript != "":
		a.orchOpenTranscript(spot.transcript)
	case spot.node != "":
		a.orchCardOpen(spot.node)
	}
	return true
}

// orchSpotAt is that hit-test on its own: which target of this page the pointer
// is over. It is one function so the press and the pointer cannot disagree about
// which chip a cell belongs to — the set that lights is the set the press acts on
// (hover.go).
func (a *app) orchSpotAt(x, y int) (orchSpot, bool) {
	run := a.orchOf()
	if run == nil {
		return orchSpot{}, false
	}
	at, ok := a.roomRowAt(y)
	if !ok || at >= len(run.spots) {
		return orchSpot{}, false
	}
	for _, spot := range run.spots[at] {
		if spot.span.holds(x) {
			return spot, true
		}
	}
	return orchSpot{}, false
}

// orchHoverAt is what the pointer is over on a run's page, by key, and false
// where it is over nothing this page answers to.
func (a *app) orchHoverAt(x, y int) (string, bool) {
	if !a.orchOpen() {
		return "", false
	}
	spot, ok := a.orchSpotAt(x, y)
	if !ok {
		return "", false
	}
	return spot.key(), true
}

// roomRowAt resolves a screen row to an index into the room's OWN row list, or
// false when the pointer is not over one.
//
// It is [app.rowAt]'s arithmetic, answered in the room's coordinates: the frame
// pins rows above the body region, a short page is padded down to sit on the
// input, and the window starts wherever the reader scrolled to. A page that
// hit-tested without all three would open the card of the chip two rows from the
// one under the finger.
func (a *app) roomRowAt(y int) (int, bool) {
	top := a.bodyTop()
	if a.room == nil || top < 0 {
		return 0, false
	}
	height := a.viewHeight()
	rows := a.roomRows(a.bodyWidth())
	offset := a.roomOffsetFor(len(rows), height)
	end := min(offset+height, len(rows))
	pad := 0
	if shown := end - offset; shown < height {
		pad = height - shown
	}
	at := y - top - pad + offset
	if at < offset || at >= end {
		return 0, false
	}
	return at, true
}

// ── THE GRAPH, DRAWN ────────────────────────────────────────────────────────
//
//	● plan      ◐ fetch-rfcs   ○ write-up
//	            │
//	· the retry section is the only one that matters — narrowing
//	  ○ compare
//
// The layers are the run's SHAPE and they are computed from the needs alone: a
// node sits one layer below the deepest thing it needs, which is exactly the
// order the scheduler is allowed to launch in. Nothing about the drawing decides
// anything about the run — this is a picture of a fact, and when the planner
// amends the frontier the picture grows a chip where the fact grew a node.

// orchRows is a run's page: the graph or a card, then the person's own echoes,
// then the gate. It is called by [app.roomRows], which caches it on the room's
// own dirty flag and width, so this runs when the shape changed and not once per
// frame.
func (a *app) orchRows(width int) []row {
	run := a.orchOf()
	if run == nil || width < 4 {
		return nil
	}
	page := &orchPage{}
	if run.transcript != "" {
		a.orchTranscriptRows(page, width)
		run.spots = page.spots
		a.orchHoverPass(page, width)
		return page.rows
	}
	a.orchPlannerRow(page, width)
	if run.card != "" {
		a.orchCardRows(page, width)
	} else {
		a.orchGraphRows(page, width)
	}
	a.orchSynthesisRows(page, width)
	a.orchEchoRows(page, width)
	a.orchGateRows(page, width)
	run.spots = page.spots
	a.orchHoverPass(page, width)
	return page.rows
}

// orchHoverPass lights whatever the pointer is over on this page, and it is the
// last thing done to it — the same order the conversation's own pass keeps
// (render.go's [app.hoverPass]).
//
// IT ANSWERS FOR THE FULL-WIDTH TARGETS AND NOTHING ELSE. A link in a card, a
// chip drawn as a stack of rows, an answer on the gate: all of them are spans
// from the first cell to the last, so what lights is the row and the pass can
// paint it after the fact. The chips of a WIDE layer are narrower than the line
// they share — several of them sit on it — so those light themselves where they
// are painted, on exactly their own cells ([app.orchLayerFlow]). A pass that
// banded their row would say "press here" about three nodes the pointer is not
// on, which is the law this file's hover is written under (hover.go).
//
// One key can cover three consecutive rows and all three light, which is right:
// a chip that a phone draws as a stack of rows is one object, and one press
// opens it from any of them ([app.orchLayerList]).
func (a *app) orchHoverPass(page *orchPage, width int) {
	if a.linear || a.hot.kind != hoverOrch {
		return
	}
	for i, spots := range page.spots {
		for _, spot := range spots {
			if !a.hoveringOrch(spot.key()) || spot.span.from != 0 || spot.span.to < width {
				continue
			}
			text := page.rows[i].text
			if text == "" {
				// A phone's third row is empty and still part of the target, so it is
				// handed the one cell [palette.background]'s padding grows out from.
				text = " "
			}
			page.rows[i].text = a.hoverRow(text, width)
			break
		}
	}
}

// orchGraphRows draws the layers.
func (a *app) orchGraphRows(page *orchPage, width int) {
	run := a.orchOf()
	notes := run.noteLines()
	if len(run.snap.Nodes) == 0 {
		word := orchEmptyWord
		if !run.known {
			word = orchUnknownWord
		}
		page.put(a.pal.dim(fit(word, width)))
		for _, note := range notes {
			a.orchNoteRows(page, note, width)
		}
		return
	}
	layers := orchLayers(run.snap.Nodes)
	tier := layoutTier(width)
	for i, layer := range layers {
		if i > 0 {
			// THE BOUNDARY BETWEEN TWO LAYERS IS WHERE A NOTE GOES, and the pairing
			// is by ORDER rather than by claim: the planner speaks at completions
			// and the graph grows at completions, so the nth note and the nth
			// boundary are the same moment seen twice. A boundary with no note left
			// to put on it is a blank row, which is the spacing the layers want
			// anyway.
			a.orchNoteBoundary(page, notes, i-1, width)
		}
		var next []orchestrate.NodeStatus
		if i+1 < len(layers) {
			next = layers[i+1]
		}
		if tier >= tierNarrow {
			a.orchLayerList(page, layer, width, tier == tierPhone)
		} else {
			a.orchLayerFlow(page, layer, next, width, tier == tierWide)
		}
	}
	// Notes the boundaries had no room for: the planner has said more than the
	// graph has grown, which is what a run that is nearly done looks like.
	for i := len(layers) - 1; i < len(notes); i++ {
		if i < 0 {
			continue
		}
		a.orchNoteRows(page, notes[i], width)
	}
	// A CURSOR ON A CANCELLED NODE IS A CURSOR ON NOTHING. The planner cancels
	// pending nodes (the amendment's own vocabulary), and a chip that went away
	// under the cursor would leave enter pointing at a card that cannot be drawn.
	if run.pick.node != "" && !run.seen[run.pick.node] {
		run.pick = orchTarget{}
	}
}

// orchNoteBoundary is one boundary between two layers: the planner's line, or
// the blank row a boundary with no note left is worth anyway.
func (a *app) orchNoteBoundary(page *orchPage, notes []string, at, width int) {
	note := ""
	if at >= 0 && at < len(notes) {
		note = strings.TrimSpace(notes[at])
	}
	if note == "" {
		page.put("")
		return
	}
	a.orchNoteRows(page, note, width)
}

// orchNoteRows puts one planner note on the page as however many rows it takes.
//
// A NOTE IS WRAPPED AND NEVER CLIPPED. Everything else on this page is a PICTURE
// of the run — a chip, a glyph, a gauge — and those are cut at the frame's edge
// without losing anything, because the shape is the information and the shape is
// still there. A note is the one thing here that is the planner's own SENTENCE
// about what it just decided, and half a sentence is the half that says least:
// "the retry section is the only one that matters — narrowing" clipped after
// eight words is a person watching a run and being told nothing.
//
// The lead is on the first row only and the rest hang under it, so a long note
// reads as one paragraph off one bullet rather than as a list of fragments.
func (a *app) orchNoteRows(page *orchPage, note string, width int) {
	note = strings.TrimSpace(note)
	if note == "" {
		return
	}
	lead := orchNoteLead
	for _, line := range wrap(note, width-ansi.StringWidth(orchNoteLead)) {
		page.put(a.pal.dim(fit(lead+line, width)))
		lead = strings.Repeat(" ", ansi.StringWidth(orchNoteLead))
	}
}

// orchNoteLead opens a planner's note, and the continuation rows are indented to
// exactly its width — one constant, so the hang cannot drift from the bullet.
const orchNoteLead = "· "

// orchLayerFlow draws one layer as chips across the line, wrapping when the
// width runs out, with the connector row under it at the wide tier.
func (a *app) orchLayerFlow(page *orchPage, layer, next []orchestrate.NodeStatus, width int, connect bool) {
	run := a.orchOf()
	line, at := "", 0
	var spots []orchSpot
	flush := func() {
		if line == "" {
			return
		}
		page.put(line, spots...)
		if connect {
			if row := orchConnectorRow(spots, next, width); row != "" {
				page.put(a.pal.dim(row))
			}
		}
		line, at, spots = "", 0, nil
	}
	for _, node := range layer {
		word := a.orchLead(run.pick == orchTarget{node: node.ID}) + a.orchChipWord(node)
		wide := ansi.StringWidth(word)
		if at > 0 && at+len(orchChipGap)+wide > width {
			flush()
		}
		if at > 0 {
			line += orchChipGap
			at += len(orchChipGap)
		}
		spot := orchSpot{span: hudSpan{from: at, to: at + wide}, node: node.ID}
		spots = append(spots, spot)
		chip := a.orchPaintChip(node, word, width-at)
		// THE CHIP LIGHTS ITSELF, because it is narrower than the line it is on:
		// several nodes share this row, so a band across it would offer every one of
		// them under a pointer that is on one ([app.orchHoverPass] states the split).
		// The band goes round exactly the chip's cells, which is what
		// [palette.hover] does when it is given no width to pad to.
		if a.hoveringOrch(spot.key()) {
			chip = a.pal.hover(chip, 0)
		}
		line += chip
		at += wide
	}
	flush()
}

// orchConnectorRow is the thin line between two layers: one stroke under every
// chip the next layer needs.
//
// It is drawn at the WIDE tier alone, and that is a width decision rather than a
// taste one — under a hundred and twenty columns the chips wrap, and a connector
// under a wrapped layer points at a chip that is no longer above it. The edges
// are still readable at every tier: the narrow and phone layouts write them out
// in words, one "↳ needs:" per chip.
func orchConnectorRow(spots []orchSpot, next []orchestrate.NodeStatus, width int) string {
	if len(next) == 0 || width <= 0 {
		return ""
	}
	needed := make(map[string]bool, len(next))
	for _, node := range next {
		for _, need := range node.Needs {
			needed[need] = true
		}
	}
	line, any := []rune(strings.Repeat(" ", width)), false
	for _, spot := range spots {
		// The stroke lands on the chip's GLYPH, which is two cells into the chip:
		// the first two are the cursor's lead, and a line under those would be a
		// line under the cursor rather than under the node.
		at := spot.span.from + ansi.StringWidth(railMark)
		if !needed[spot.node] || at < 0 || at >= width {
			continue
		}
		line[at], any = '│', true
	}
	if !any {
		return ""
	}
	return strings.TrimRight(string(line), " ")
}

// orchLayerList draws one layer as a stack of rows: the tiers where a chip
// cannot be a cell.
//
// AT THE PHONE TIER EVERY CHIP IS THREE ROWS, whether or not it has three rows'
// worth to say. A finger covers about three rows of a terminal, and a target
// that is one row tall is a target that opens the node above or below the one
// somebody meant — so the goal takes a row of its own and the needs line is
// drawn even when it is empty. The width is not what is being spent there; the
// certainty is.
func (a *app) orchLayerList(page *orchPage, layer []orchestrate.NodeStatus, width int, phone bool) {
	run := a.orchOf()
	for _, node := range layer {
		lead := a.orchLead(run.pick == orchTarget{node: node.ID})
		word := lead + a.orchChipWord(node)
		if !phone {
			// The narrow tier keeps the name and the goal on one line: two lines per
			// node at sixty columns is a page you scroll to see four nodes.
			if goal := strings.TrimSpace(node.Goal); goal != "" {
				word += " · " + goal
			}
		}
		// THE WHOLE ROW IS THE TARGET at these tiers, and so is every row under it:
		// a chip that is a stack of rows is one object, and a finger that landed on
		// its goal meant the chip.
		spot := orchSpot{span: hudSpan{from: 0, to: width}, node: node.ID}
		page.put(a.orchPaintChip(node, word, width), spot)
		if phone {
			page.put(a.pal.dim(fit("    "+strings.TrimSpace(node.Goal), width)), spot)
		}
		page.put(a.pal.dim(fit("    "+orchNeedsWord(node), width)), spot)
	}
}

// orchNeedsWord is a chip's dependency line, in words. An independent node says
// so rather than saying nothing: a blank where every other row has a sentence
// reads as a row that failed to draw.
func orchNeedsWord(node orchestrate.NodeStatus) string {
	if len(node.Needs) == 0 {
		return orchNeedsLead + "—"
	}
	return orchNeedsLead + strings.Join(node.Needs, ", ")
}

// orchChipWord is one chip, plain: its state in a cell, its id, and the "new"
// marker for the one interval after it appeared.
func (a *app) orchChipWord(node orchestrate.NodeStatus) string {
	run := a.orchOf()
	word := a.orchGlyph(node) + " " + node.ID
	if run != nil && run.fresh[node.ID] {
		word += " · " + orchNewWord
	}
	return word
}

// orchGlyph is a node's state in one cell.
//
// A PAUSED RUN'S UNSTARTED NODES WEAR THE PAUSE, and that is a fact about the
// run said in the place a person is looking. Nothing new launches while the gate
// is up (internal/orchestrate's fuel law), so a queued node is not waiting for
// its needs any more — it is waiting for an answer, and the empty circle would
// say the first thing about a node in the second state.
func (a *app) orchGlyph(node orchestrate.NodeStatus) string {
	run := a.orchOf()
	paused := run != nil && (run.snap.Paused || run.gate != nil)
	switch node.State {
	case orchestrate.Done:
		return a.linearMark(orchGlyphDone, orchGlyphDoneASCII)
	case orchestrate.Failed:
		return a.linearMark(glyphBad, glyphBadASCII)
	case orchestrate.Cancelled:
		// NOT THE CROSS. A node somebody stopped did not fail and nobody found
		// anything wrong with it — it is the one state on this ramp that is not a
		// point on it, and it wears the roster's own stop mark (stop.go).
		return a.linearMark(glyphStopped, glyphStoppedASCII)
	case orchestrate.Running:
		return a.linearMark(orchGlyphRunning, orchGlyphRunningASCII)
	default:
		if paused {
			return a.linearMark(orchGlyphPaused, orchGlyphPausedASCII)
		}
		return a.linearMark(orchGlyphQueued, orchGlyphQueuedASCII)
	}
}

// orchPaintChip is the chip's hue: the accent while it runs, the surface's own
// bad hue when it failed, muted once it has landed, dim while it waits or once
// somebody stopped it. It is one paint over the whole chip rather than one per
// part, because a chip is one object and a glyph in a different hue from its
// name is two.
//
// A CANCELLED CHIP GOES GREY AND STAYS ON THE PAGE. It is not painted away and
// it is not removed: the shape a person is looking at is the shape the run
// crystallized into, and a chip that vanished when they pressed stop would be
// the page denying the work was ever asked for. Dim is what this surface already
// spends on work that is not happening.
func (a *app) orchPaintChip(node orchestrate.NodeStatus, word string, width int) string {
	text := fit(word, width)
	switch node.State {
	case orchestrate.Running:
		return a.pal.accent(text)
	case orchestrate.Failed:
		return a.pal.bad(text)
	case orchestrate.Done:
		return a.pal.muted(text)
	}
	return a.pal.dim(text)
}

// noteLines is the planner's notes as the page has them: what the snapshot
// published, and anything the lane delivered that the snapshot has not caught up
// with. The two are merged rather than chosen between, because the lane is
// always ahead of the poll and the poll is the only one that survives a reopen.
func (r *orchRun) noteLines() []string {
	out := append([]string(nil), r.snap.Notes...)
	for _, note := range r.notes {
		if !orchHas(out, note) {
			out = append(out, note)
		}
	}
	return out
}

// steerLines is the same merge for the person's own words.
func (r *orchRun) steerLines() []string {
	out := append([]string(nil), r.snap.Steer...)
	for _, line := range r.steered {
		if !orchHas(out, line) {
			out = append(out, line)
		}
	}
	return out
}

func orchHas(list []string, text string) bool {
	for _, one := range list {
		if one == text {
			return true
		}
	}
	return false
}

// ── the chip's card ─────────────────────────────────────────────────────────
//
//	● fetch-rfcs · done · $0.12
//	  pull the three RFCs and quote what they say about retries
//
//	  needs
//	  ▌ ● plan
//	  digest
//	    RFC 7231 §6.5.1 …
//	  esc · back to the graph
//
// The needs are LINKS and not text, which is the whole reason the card is a page
// of its own rather than a tooltip: "what did this depend on" is answered by
// GOING there, and a person who walked from a chip to its parent to its parent's
// digest has read the run's reasoning backwards, which is how anybody reads a
// result they did not expect.

// orchLink is one navigable thing on a card: another node, or the run this node
// is.
type orchLink struct {
	node       string
	run        string
	transcript string
}

// orchCardLinks is what the open card's cursor walks: its needs that this
// snapshot carries, and the nested run behind it when there is one.
//
// A NEED THIS SNAPSHOT DOES NOT CARRY IS NOT A LINK. The id is still printed on
// the needs line — it is what the planner wrote — but a link that opened a card
// for a node nobody has published would be a door onto an empty page (task.go's
// [app.railWaits] states the same rule about the same kind of gap).
func (a *app) orchCardLinks() []orchLink {
	run := a.orchOf()
	if run == nil || run.card == "" {
		return nil
	}
	node, ok := orchNodeOf(run.snap, run.card)
	if !ok {
		return nil
	}
	var out []orchLink
	for _, need := range node.Needs {
		if _, ok := orchNodeOf(run.snap, need); ok {
			out = append(out, orchLink{node: need})
		}
	}
	if a.orchNested(node.ID) {
		out = append(out, orchLink{run: node.ID})
	}
	out = append(out, orchLink{transcript: node.ID})
	return out
}

// orchNested reports whether a node is itself a run this session can show. The
// id is the run's id — a nested run is a node whose work IS a run, and the
// session names both the same way.
func (a *app) orchNested(id string) bool {
	run := a.orchOf()
	doors, ok := a.orchDoors()
	if run == nil || !ok || id == "" || id == run.id {
		return false
	}
	_, found := doors.OrchestrateSnapshot(id)
	return found
}

// orchNodeOf finds one node in a snapshot.
func orchNodeOf(snap orchestrate.Snapshot, id string) (orchestrate.NodeStatus, bool) {
	for _, node := range snap.Nodes {
		if node.ID == id {
			return node, true
		}
	}
	return orchestrate.NodeStatus{}, false
}

// orchCardRows draws the open chip's card.
func (a *app) orchCardRows(page *orchPage, width int) {
	run := a.orchOf()
	node, ok := orchNodeOf(run.snap, run.card)
	if !ok {
		// The node went away under the card — a cancel, or a snapshot from another
		// run. The card says so rather than drawing an empty page, and esc is on the
		// line under it.
		page.put(a.pal.dim(fit(run.card+" is not in this run", width)))
		page.put(a.pal.dim(fit(orchCardBack, width)))
		return
	}
	head := a.orchGlyph(node) + " " + node.ID
	for _, part := range []string{orchNodeWord(node), a.orchCost(node)} {
		if part != "" {
			head += " · " + part
		}
	}
	page.put(a.orchPaintChip(node, head, width))
	for _, line := range wrap(strings.TrimSpace(node.Goal), width-2) {
		page.put(a.pal.ink(fit("  "+line, width)))
	}

	links := a.orchCardLinks()
	at := 0
	page.put("")
	page.put(a.pal.dim(fit(orchNeedsHead, width)))
	if len(node.Needs) == 0 {
		page.put(a.pal.dim(fit("  —", width)))
	}
	for _, need := range node.Needs {
		dep, known := orchNodeOf(run.snap, need)
		if !known {
			// Named, not linked: the planner asked for it and this snapshot has not
			// published it.
			page.put(a.pal.dim(fit("  "+need, width)))
			continue
		}
		picked := at < len(links) && links[at].node == need && at == run.link
		word := a.orchLead(picked) + a.orchGlyph(dep) + " " + dep.ID
		spot := orchSpot{span: hudSpan{from: 0, to: width}, node: need}
		page.put(a.orchPaintChip(dep, word, width), spot)
		if layoutTier(width) == tierPhone {
			// THE PHONE'S THREE ROWS, on a link as much as on a chip: a door a thumb
			// misses is a door that opened the wrong node (see [app.orchLayerList]).
			page.put(a.pal.dim(fit("    "+strings.TrimSpace(dep.Goal), width)), spot)
			page.put("", spot)
		}
		at++
	}
	if digest := strings.TrimSpace(node.Digest); digest != "" {
		page.put("")
		page.put(a.pal.dim(fit(orchDigestHead, width)))
		for _, line := range wrap(digest, width-2) {
			page.put(a.pal.ink(fit("  "+line, width)))
		}
	}
	if err := strings.TrimSpace(node.Err); err != "" {
		page.put("")
		page.put(a.pal.bad(fit(orchErrHead, width)))
		for _, line := range wrap(err, width-2) {
			page.put(a.pal.bad(fit("  "+line, width)))
		}
	}
	if a.orchNested(node.ID) {
		picked := len(links) > 0 && links[len(links)-1].run == node.ID && run.link == len(links)-1
		word := a.orchLead(picked) + orchRunHead + " · enter opens it"
		spot := orchSpot{span: hudSpan{from: 0, to: width}, run: node.ID}
		page.put(a.pal.accent(fit(word, width)), spot)
		if layoutTier(width) == tierPhone {
			page.put("", spot)
			page.put("", spot)
		}
	}
	picked := len(links) > 0 && links[len(links)-1].transcript == node.ID && run.link == len(links)-1
	word := a.orchLead(picked) + "transcript · enter opens it"
	spot := orchSpot{span: hudSpan{from: 0, to: width}, transcript: node.ID}
	page.put(a.pal.accent(fit(word, width)), spot)
	if layoutTier(width) == tierPhone {
		page.put("", spot)
		page.put("", spot)
	}
	page.put("")
	page.put(a.pal.dim(fit(orchCardBack, width)))
}

// orchNodeWord is one node's state in the run's own vocabulary.
func orchNodeWord(node orchestrate.NodeStatus) string {
	switch node.State {
	case orchestrate.Queued:
		return "queued"
	case orchestrate.Ready:
		return "ready"
	case orchestrate.Running:
		return "running"
	case orchestrate.Done:
		return orchDoneWord
	case orchestrate.Failed:
		return "failed"
	case orchestrate.Cancelled:
		return orchStoppedWord
	}
	return ""
}

// orchCost is what one node has burned, or "" when nobody has priced it — the
// room's own rule about a figure nobody measured (room.go's [app.roomSpend]).
func (a *app) orchCost(node orchestrate.NodeStatus) string {
	if node.Cost <= 0 {
		return ""
	}
	return dollars(node.Cost)
}

// ── the person's own half, and the gate ─────────────────────────────────────

// orchSynthesisRows draws the run's answer, once it has one.
//
// IT IS THE POINT OF THE PAGE AND IT COMES LAST, which is not a contradiction:
// the graph above it is how the answer was reached, and a person reading a
// finished run reads the claim and then looks up at the chips it cites (the
// synthesis grounds every claim in node ids — internal/orchestrate's law). It is
// drawn in the ink hue and nothing else on this page is, so the one thing here
// that is a RESULT rather than a picture of work is the one thing that reads as
// text.
func (a *app) orchSynthesisRows(page *orchPage, width int) {
	run := a.orchOf()
	answer := strings.TrimSpace(run.snap.Answer)
	if answer == "" {
		return
	}
	page.put("")
	page.put(a.pal.dim(fit(orchAnswerHead, width)))
	for _, line := range wrap(answer, width-2) {
		page.put(a.pal.ink(fit("  "+line, width)))
	}
}

// orchEchoRows draws what the person has said to this run: the lines they
// steered, and the answers they gave the gate. They sit under the graph because
// that is where they happened — the graph is the run's half of the page and this
// is theirs.
func (a *app) orchEchoRows(page *orchPage, width int) {
	run := a.orchOf()
	steers, acts := run.steerLines(), run.acts
	if len(steers) == 0 && len(acts) == 0 {
		return
	}
	page.put("")
	for _, line := range steers {
		page.put(a.pal.dim(fit(orchSteerLead+line, width)))
	}
	for _, act := range acts {
		page.put(a.pal.dim(fit(orchActLead+act, width)))
	}
}

// orchGateRows draws the pause as a card at the foot of the page:
//
//	? out of fuel · $2.00 of $2.00
//	▌ add $1
//	  finish with what we have
//	  stop
//
// IT IS DRAWN ON THE PAGE AND NOT IN THE CHROME, unlike the three offers this
// surface stacks above the draft (harness.go, connect.go, consent.go), and the
// difference is what is blocked. Those three hold a TURN: the session cannot go
// on until they are answered, so they belong over the box the person is typing
// in and they take the keyboard whole. This one holds a RUN. The conversation is
// free, the box still steers, and a person may quite reasonably walk out of the
// page and come back — so the question lives where the thing it is about lives,
// wearing the same question hue that every other question on this surface does.
//
// THE THREE ANSWERS ARE ROWS AND NOT A KEY LEGEND, which is what the offers up
// in the chrome use. A key chip promises a keystroke, and this question has no
// keystrokes of its own to promise: it cannot suspend the box the way a modal
// offer does, so the keys it could claim are letters somebody is in the middle
// of typing (see [app.orchKey]). A row is answered by the two gestures the page
// already has — the cursor and a finger — and a row is a target a thumb can hit
// at the phone tier, which a chip five cells wide is not.
func (a *app) orchGateRows(page *orchPage, width int) {
	run := a.orchOf()
	if run.gate == nil {
		return
	}
	page.put("")
	question := glyphAsk + " " + orchGateLead
	if spend := strings.TrimSpace(run.gate.text); spend != "" {
		question += " · " + spend
	}
	page.put(a.pal.askBold(glyphAsk) + a.pal.ask(fit(question[len(glyphAsk):], width-len(glyphAsk))))
	for _, answer := range []string{orchTopUp, orchFinish, orchStop} {
		lead := a.orchLead(run.pick == orchTarget{answer: answer})
		spot := orchSpot{span: hudSpan{from: 0, to: width}, answer: answer}
		page.put(lead+a.pal.ask(fit(orchAnswerWord(answer), width-ansi.StringWidth(lead))), spot)
		if layoutTier(width) == tierPhone {
			// The phone's three rows again, and here it matters most: this is the one
			// row on the page that spends money.
			page.put("", spot)
			page.put("", spot)
		}
	}
}

// ── the header ──────────────────────────────────────────────────────────────

// orchHeadWord is the pinned line's left end while a run's page is open: where
// you are, what the tank says, and what the run is doing.
//
//	◐ main ▸ ship the parser fix · $0.87 / $2.00 · working
//
// THE GAUGE IS THE MIDDLE SEGMENT AND IT IS NEVER DROPPED. A run spends money
// on its own initiative — that is what an adaptive run IS — and the one number
// that says how much of the person's decision is left has to be on screen at
// every width the header is drawn at. What gets cut is the GOAL, which is a
// sentence the person wrote and can still read at the top of the conversation.
func (a *app) orchHeadWord(width int) string {
	run := a.orchOf()
	if run == nil {
		return ""
	}
	// THE PLANNER IS NAMED IN FRONT OF THE GAUGE, because the two are one fact:
	// the tank is being spent by a judgement, and with tiers configured that
	// judgement is a model nobody in this conversation is talking to. At the
	// PHONE tier it comes off the line first — not dropped, moved — and the page
	// draws it as a dim row of its own ([app.orchPlannerRow]).
	parts := []string{run.fuelWord(), a.orchStateWord()}
	if layoutTier(width) != tierPhone {
		parts = append([]string{run.plannerWord()}, parts...)
	}
	tail := ""
	for _, part := range parts {
		if part != "" {
			tail += " · " + part
		}
	}
	mark := a.orchHeadMark()
	trail := roomCrumbRoot
	for _, crumb := range run.crumbs {
		trail += roomCrumbSep + orchCrumbWord(crumb.goal, crumb.id)
	}
	trail += roomCrumbSep + orchCrumbWord(run.goal, run.id)
	if run.card != "" {
		trail += roomCrumbSep + run.card
	}
	if run.transcript != "" {
		trail += roomCrumbSep + "transcript"
	}
	// The trail is what gives way, from its own end: the room a person is in is
	// the one they can least afford to lose off the line.
	space := width - ansi.StringWidth(mark+" "+tail)
	return fit(mark+" "+fit(trail, space)+tail, width)
}

// orchCrumbWord is one step of the trail: the goal when there is one, and the
// run's id when there is not — a crumb that said nothing would be a step nobody
// can count.
func orchCrumbWord(goal, id string) string {
	if word := strings.TrimSpace(goal); word != "" {
		return word
	}
	return id
}

// orchHeadMark is the run's state in the header's one cell, unpainted for
// [app.roomHeadWord]'s reason: the whole line is painted once, and a hue nested
// inside a hue ends at the inner one's reset.
func (a *app) orchHeadMark() string {
	run := a.orchOf()
	switch {
	case run.snap.Stopped:
		return a.linearMark(glyphStopped, glyphStoppedASCII)
	case run.gate != nil || run.snap.Paused:
		return a.linearMark(orchGlyphPaused, orchGlyphPausedASCII)
	case run.snap.Done:
		return a.linearMark(orchGlyphDone, orchGlyphDoneASCII)
	case !run.known:
		return a.linearMark(orchGlyphQueued, orchGlyphQueuedASCII)
	}
	if a.linear {
		return orchGlyphRunningASCII
	}
	return tokens.Spinner(a.paints / spinnerStep)
}

// orchStateWord is what the run is doing, in one word.
func (a *app) orchStateWord() string {
	run := a.orchOf()
	switch {
	case run.snap.Stopped:
		return orchStoppedWord
	case run.gate != nil || run.snap.Paused:
		return orchPausedWord
	case run.snap.Done:
		return orchDoneWord
	case !run.known:
		return orchOpeningWord
	}
	return stateWorking.String()
}

// fuelWord is the gauge: what has been spent of what was approved.
//
// It falls back to the LANE's own sentence when no snapshot has resolved,
// because the fuel event carries a spend summary in words and a page with a
// number it cannot compute is better off quoting the one it was given than
// printing "$0.00 / $0.00" — which is a gauge saying a run has no tank.
func (r *orchRun) fuelWord() string {
	if r.snap.Fuel.Cap > 0 || r.snap.Fuel.Spent > 0 {
		return dollars(r.snap.Fuel.Spent) + " / " + dollars(r.snap.Fuel.Cap)
	}
	return strings.TrimSpace(r.fuel)
}

// plannerWord names the model this run is thinking with, or "" when no snapshot
// has said one. It is the RUN's fact and not the session's: the planner is
// resolved through internal/roles when the run is built, so an install with a
// high tier set plans on a model that appears nowhere else on this surface.
func (r *orchRun) plannerWord() string {
	model := strings.TrimSpace(r.snap.Planner)
	if model == "" {
		return ""
	}
	return orchPlannerLead + model
}

// orchPlannerRow is the header's planner segment, moved onto the page at the
// phone tier.
//
// AT FORTY COLUMNS THE HEADER CANNOT HOLD IT — the trail gives way from its own
// end there and a fourth segment would take the goal with it — and dropping it
// is not the alternative: the model spending somebody's money is a fact this
// page says at every width. So it becomes one dim row at the top, above the
// graph and above a card alike, because it is true of the whole run rather than
// of anything drawn under it.
//
// The tier is read off the WINDOW and not off the page's own width, because the
// header spans the window (view.go) and this row exists only to catch what the
// header let go: two widths asking the question separately is how a fact ends up
// drawn twice on a screen with a roster open, or nowhere on one without.
func (a *app) orchPlannerRow(page *orchPage, width int) {
	run := a.orchOf()
	frame, _ := a.size()
	if run == nil || layoutTier(frame) != tierPhone {
		return
	}
	if word := run.plannerWord(); word != "" {
		page.put(a.pal.dim(fit(word, width)))
	}
}

// ── the layers ──────────────────────────────────────────────────────────────

// orchLayers groups a run's nodes into topological layers: a node sits one below
// the deepest thing it needs.
//
// THE LAYOUT IS DERIVED AND NEVER STORED, which is what makes the picture
// crystallize instead of being redrawn: the same nodes always land in the same
// layers, so a node arriving adds a chip and moves nothing that was already
// there unless the planner made it depend on something deeper.
//
// Two shapes the planner should never produce are tolerated rather than
// rejected, because a page that refused to draw a graph would be the surface
// deciding the run is invalid: a need naming a node this snapshot does not carry
// is skipped (it may simply not be published yet), and a cycle is broken at the
// edge that closes it. Neither can make this loop forever, which is the only
// property that actually matters here.
func orchLayers(nodes []orchestrate.NodeStatus) [][]orchestrate.NodeStatus {
	by := make(map[string]orchestrate.NodeStatus, len(nodes))
	for _, node := range nodes {
		by[node.ID] = node
	}
	depth := make(map[string]int, len(nodes))
	open := make(map[string]bool, len(nodes))
	var walk func(id string) int
	walk = func(id string) int {
		if at, done := depth[id]; done {
			return at
		}
		if open[id] {
			return 0 // the edge that closes a cycle
		}
		node, known := by[id]
		if !known {
			return 0
		}
		open[id] = true
		at := 0
		for _, need := range node.Needs {
			if _, carried := by[need]; !carried {
				continue
			}
			if below := walk(need) + 1; below > at {
				at = below
			}
		}
		delete(open, id)
		depth[id] = at
		return at
	}
	deepest := 0
	for _, node := range nodes {
		if at := walk(node.ID); at > deepest {
			deepest = at
		}
	}
	layers := make([][]orchestrate.NodeStatus, deepest+1)
	for _, node := range nodes {
		at := depth[node.ID]
		layers[at] = append(layers[at], node)
	}
	// A layer with nothing in it cannot happen — every depth on the way to the
	// deepest is occupied by whatever made it deep — but an empty one drawn would
	// be a blank band in the middle of the graph, so it is dropped rather than
	// trusted.
	out := make([][]orchestrate.NodeStatus, 0, len(layers))
	for _, layer := range layers {
		if len(layer) > 0 {
			out = append(out, layer)
		}
	}
	return out
}
