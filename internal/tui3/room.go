package tui3

import (
	"bufio"
	"encoding/json"
	"os"
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
// ── A ROOM ROW IS A READING ──
//
// The node's tool calls are drawn as ONE DIM LINE each ("· read config.go") and
// they do not expand. The full tool view — the diff, the output, the capped
// preview, the click target, the hover — is the conversation's machine for work
// a person is supervising call by call, and a node is precisely the work they
// delegated so they would not have to. What a person needs in here is the shape
// of what is happening ("it is reading the wrong directory"), and the shape is
// one line. Everything else is in the journal, which is a real session file.
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
	SteerTask(id uint64, text string) error
	// WatchTask subscribes to the node's live events, FROM NOW: nothing is
	// replayed, and the channel closes at the node's final state. A finished node
	// answers with an already-closed channel rather than an error.
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

// ── the room's state ────────────────────────────────────────────────────────

// roomKind is what one line in a room IS. There are four and there is not going
// to be a fifth: the person, the node, what the node did, and the surface saying
// something about the room itself.
type roomKind uint8

const (
	// roomSaid is the node's own words — the assistant block, coalesced.
	roomSaid roomKind = iota
	// roomAsked is the person's line, in the person's hue, under the person's
	// glyph. It is the same law the main transcript's user block follows
	// (render.go): identity takes hue, and nothing the model writes is ever
	// painted in the accent.
	roomAsked
	// roomCalled is one tool call, one dim line.
	roomCalled
	// roomNoted is the surface talking inside the room — a refusal, an error.
	roomNoted
)

// roomLine is one block of a node's transcript.
type roomLine struct {
	kind roomKind
	text string
	// call identifies the CALL a roomCalled line is about — its tool and its
	// target, together. It is what makes a begin and its end one line rather
	// than two: the second event replaces the first, and a different call
	// starts a line of its own (see [app.roomCall]).
	call string
}

// taskRoom is one node's page: what it has said, the lane carrying what it says
// next, and where the reader is in it.
type taskRoom struct {
	id    uint64
	title string
	lines []roomLine
	lane  <-chan session.Event
	// gen is the generation device the two other lanes on this surface use
	// (app.go's stream, task.go's standing subscription): a room that was closed
	// while its channel still had events in flight must not paint into the room
	// that replaced it.
	gen int
	// live is the index of the node's growing block, or -1. Text deltas coalesce
	// into it exactly as they do in the conversation.
	live int
	// done says the lane closed — the node reached its final state — which is
	// the one fact the room adds to what it is showing: a foot line, and a
	// refusal for anything typed after it.
	done bool

	// The reader's own position. It is HERE and not on the app because that is
	// the whole promise of esc: the conversation's scroll is not touched while a
	// room is open, so returning to it restores nothing because nothing moved.
	offset int
	stick  bool

	// The row cache, on the same terms every other cached block on this surface
	// has one (render.go): rebuilt when the content or the width changes and at
	// no other time.
	rows  []row
	width int
	dirty bool
}

// The words the room says of itself.
const (
	// roomPlaceWord is the identity cluster while a room is open. The telemetry
	// beside it is still the SESSION's — the room is a view over one body region,
	// not a second session, and a status line that re-pointed the cost and the
	// context at a node would be claiming figures nobody is measuring.
	roomPlaceWord = "task"
	// roomLegendWord replaces the path in the legend while a room is open: where
	// you are, and the one key that leaves.
	roomLegendWord = "room · esc to return"
	// roomFinishedWord is the foot under a node that has landed.
	roomFinishedWord = "task finished — esc to return"
	// roomParkedWord opens the guard's line, after the node's title: what is
	// wrong, in three words, before the three keys that answer it.
	roomParkedWord = " is parked — "
	// roomUnavailableWord is the degraded case: an agent under this surface with
	// no room doors on it at all.
	roomUnavailableWord = "room unavailable — this session has no task rooms"
	// roomSteerLane is the input's placeholder while a room is open, with the
	// node's title spliced in: the box says who it is talking to, because it is
	// the same box that talks to the model.
	roomSteerLane = "steer "
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
// why): a watcher gets what happens FROM NOW and nothing is re-narrated, so the
// history has to come off disk or not at all. A node with no journal path opens
// on its live edge, which is honest — this process never learned which file that
// node wrote.
// The command that starts the lane is PARKED rather than returned, in
// [app.roomPump]. See that field for the call path that forces it.
func (a *app) openRoom(id uint64, title string) {
	doors, ok := a.roomDoors()
	if !ok {
		// THE BUILD GUARD. The doors are an assertion and not a compile-time
		// requirement, so a surface driven by an agent that has never heard of a
		// room says so and stays in the conversation.
		a.note(roomUnavailableWord)
		return
	}
	if title == "" {
		title = "task " + itoa(int(id))
	}
	a.roomGen++
	room := &taskRoom{id: id, title: title, gen: a.roomGen, live: -1, stick: true, dirty: true}
	room.lines = readRoomJournal(doors.TaskJournal(id))
	a.room = room
	// The selection and the pointer belong to the conversation, which is no
	// longer the thing on screen: a highlight under a room is a highlight on a
	// row nobody can see.
	a.sel = -1
	a.dropHover()
	a.touch()

	lane, err := doors.WatchTask(id)
	if err != nil {
		// An unknown id. The room still opens — the journal is worth reading —
		// and it opens finished, because there is nothing to listen to.
		room.done = true
		a.roomNote(err.Error())
		a.roomPump = a.wake()
		return
	}
	room.lane = lane
	a.roomPump = tea.Batch(waitRoom(lane, room.gen), a.wake())
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
func (a *app) openRoomAt(i int) bool {
	if i < 0 || i >= len(a.entries) || a.entries[i].kind != entryTask {
		return false
	}
	card := a.entries[i].card
	if card == nil {
		return false
	}
	node := a.tasks[card.id]
	if node == nil {
		return false
	}
	a.openRoomFor(node.id, firstNonEmpty(node.title, card.title))
	return true
}

// ── the journal, replayed ───────────────────────────────────────────────────

// journalLine is the slice of internal/session's session file this room reads.
//
// It is parsed HERE rather than asked for because there is no door for it: the
// journal is a real session file, the package that writes it exposes its shape
// only through an agent that has the file OPEN, and a room must be able to read
// the transcript of a node whose agent belongs to somebody else. The fields
// below are the four the room draws and nothing else — an unknown line kind, an
// unknown role and a field this build does not know are all skipped rather than
// guessed at, which is what makes an older or a newer file readable.
type journalLine struct {
	Type      string        `json:"type"`
	Role      string        `json:"role"`
	Content   string        `json:"content"`
	ToolCalls []journalCall `json:"toolCalls"`
}

type journalCall struct {
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// readRoomJournal turns a node's session file into room lines, oldest last.
//
// A path that is empty, missing or unreadable is not an error and draws nothing:
// the journal is EVIDENCE, not a prerequisite (internal/session says so where it
// mints the path), and a room that refused to open because a file was not there
// would be refusing to show the live work as well.
//
// Tool RESULTS are skipped. A tool message is the payload the node read, and a
// room row is a reading — the line already says what was called and on what,
// which is the shape a person came in here for.
func readRoomJournal(path string) []roomLine {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	var out []roomLine
	scan := bufio.NewScanner(file)
	// A journaled message can be a whole file's content, and the default token
	// is 64k. The cap is what one line may weigh, not what the file may.
	scan.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scan.Scan() {
		var line journalLine
		if err := json.Unmarshal(scan.Bytes(), &line); err != nil {
			continue // a torn last line, or a kind this build does not know
		}
		if line.Type != "message" {
			continue
		}
		switch line.Role {
		case "user":
			if text := strings.TrimSpace(line.Content); text != "" {
				out = append(out, roomLine{kind: roomAsked, text: text})
			}
		case "assistant":
			if text := strings.TrimSpace(line.Content); text != "" {
				out = append(out, roomLine{kind: roomSaid, text: text})
			}
			for _, call := range line.ToolCalls {
				name := strings.TrimSpace(call.Function.Name)
				if name == "" {
					continue
				}
				out = append(out, roomLine{
					kind: roomCalled,
					call: roomCallKey(name, call.Function.Arguments, ""),
					text: roomCallWord(name, call.Function.Arguments, ""),
				})
			}
		}
	}
	if len(out) > roomTail {
		out = out[len(out)-roomTail:]
	}
	return out
}

// ── the live lane ───────────────────────────────────────────────────────────

// roomEvent folds one of the child's events in and re-arms the pump.
//
// The set it draws is the set a person in here is watching for: what the node is
// SAYING, what it is DOING, and what went wrong. A node's usage, its title, its
// consent questions and its own compaction are the child agent's business —
// nothing in a task can ask this keyboard a question (its policy allows
// everything but the floor, and the floor refuses rather than prompts), so there
// is no question in here to draw.
func (a *app) roomEvent(ev session.Event) tea.Cmd {
	room := a.room
	if room == nil {
		return nil
	}
	switch ev.Kind {
	case session.EventTextDelta:
		a.roomSay(ev.Text)
	case session.EventToolAnnounced, session.EventToolBegin,
		session.EventToolEnd, session.EventToolFailed:
		a.roomCall(ev)
	case session.EventError:
		a.roomNote("error: " + errText(ev.Err))
	}
	a.touch()
	return tea.Batch(waitRoom(room.lane, room.gen), a.wake())
}

// roomSay grows the node's live block, opening one when the last thing in the
// room was anything else. It is [app.appendText]'s pattern with the cache
// bookkeeping and nothing else in it.
func (a *app) roomSay(text string) {
	room := a.room
	if text == "" || room == nil {
		return
	}
	if room.live < 0 || room.live >= len(room.lines) || room.lines[room.live].kind != roomSaid {
		room.lines = append(room.lines, roomLine{kind: roomSaid})
		room.live = len(room.lines) - 1
	}
	room.lines[room.live].text += text
	a.roomTouched()
}

// roomCall draws one call, and REPLACES the line when the event is about the
// call that line already holds.
//
// A call arrives up to three times — announced, begun, ended — and three lines
// saying "read config.go" is a room that reports its own event plumbing. The
// identity is the tool AND its target together rather than the tool alone: a
// batch of four reads is four different calls, and collapsing them by name would
// leave one line where four files were read.
func (a *app) roomCall(ev session.Event) {
	room := a.room
	if room == nil {
		return
	}
	line := roomLine{
		kind: roomCalled,
		call: roomCallKey(ev.Tool, ev.Args, ev.Hint),
		text: roomCallWord(ev.Tool, ev.Args, ev.Hint),
	}
	if last := len(room.lines) - 1; last >= 0 &&
		room.lines[last].kind == roomCalled && room.lines[last].call == line.call {
		room.lines[last] = line
		a.roomTouched()
		return
	}
	a.roomAppend(line)
}

// roomNote is the surface's own line inside the room.
func (a *app) roomNote(text string) {
	if text = strings.TrimSpace(text); text != "" {
		a.roomAppend(roomLine{kind: roomNoted, text: text})
	}
}

// roomAppend adds one block and closes whatever was streaming: a block that
// follows the node's words is the node having stopped saying them.
func (a *app) roomAppend(line roomLine) {
	room := a.room
	if room == nil {
		return
	}
	room.lines = append(room.lines, line)
	room.live = -1
	a.roomTouched()
}

// roomTouched drops the room's cached rows and keeps a reader at the live edge
// where they were already at it. It is [app.follow]'s law, applied to the room's
// own offset.
func (a *app) roomTouched() {
	if a.room == nil {
		return
	}
	a.room.dirty = true
	if a.room.stick {
		a.room.offset = 0 // resolved from the bottom by roomOffsetFor
	}
	a.touch()
}

// roomCallKey is what makes two events one line: the tool, and the thing it is
// pointed at.
func roomCallKey(tool, args, hint string) string {
	return strings.TrimSpace(tool) + "\x00" + toolTarget(tool, args, hint)
}

// roomCallWord is one call in one sentence, from the same two renderers the
// conversation's tool line is built from (toolview.go, toolstat.go): the tool's
// name with the verb said once, and the payload's own target ahead of the
// session's gloss.
func roomCallWord(tool, args, hint string) string {
	name, _ := toolWords(tool, hint)
	if target := toolTarget(tool, args, hint); target != "" {
		return name + " " + target
	}
	return name
}

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
	if room.done {
		a.raiseGuard(line, "")
		return nil
	}
	doors, ok := a.roomDoors()
	if !ok {
		a.roomNote(roomUnavailableWord)
		return nil
	}
	if err := doors.SteerTask(room.id, line); err != nil {
		// The engine's own sentence, kept: "task 3 is done, not running" and
		// "task 3 has no worker to talk to yet" are different facts, and a
		// surface that flattened them to "could not steer" would be throwing
		// away the half that says what to do about it. It goes on the guard's
		// second row rather than into the room, because it is the reason the
		// question below is being asked.
		a.raiseGuard(line, err.Error())
		return nil
	}
	a.input.reset()
	a.endRecall()
	a.closeLists()
	a.roomAppend(roomLine{kind: roomAsked, text: line})
	return a.edited()
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
}

// raiseGuard puts the question up. The room stays open underneath it: the page
// is what the person was reading, and the question is about what to do with a
// line they typed into it.
func (a *app) raiseGuard(line, why string) {
	if a.room == nil {
		return
	}
	a.guard = &steerGuard{title: a.room.title, text: line, why: why}
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
	if a.room == nil {
		return nil, false
	}
	switch key := msg.String(); {
	case key == "ctrl+c", a.asking(), a.awaitingTask(),
		a.sheet.open, a.pick.open, a.copy.on, a.welcome.open,
		a.menu.open, a.comp.open:
		return nil, false
	}
	// THE GUARD IS READ BEFORE THE ROOM, and it is the same rung: it is a
	// question raised by the room's own enter, so the keys it answers with have
	// to outrank the key that raised it. Everything above still outranks both.
	if cmd, taken := a.guardKey(msg); taken {
		return cmd, true
	}
	switch msg.String() {
	case "esc":
		// A recall walk is left first, for the reason input.go leaves it first: a
		// state that could not be dismissed by the dismiss key is a trap, and the
		// room is still one keystroke behind it.
		if a.recalling() {
			return nil, false
		}
		a.closeRoom()
		return nil, true

	case "enter":
		return a.steer(), true

	case "ctrl+b":
		// FREEZE THE ROOM, not the conversation. copymode.go snapshots the
		// transcript's rows, which while a room is open are not the rows on
		// screen — so the snapshot is taken here, from what is actually drawn.
		a.freezeRoom()
		return nil, true

	case "pgup":
		a.roomScroll(-a.page())
		return nil, true
	case "pgdown":
		a.roomScroll(a.page())
		return nil, true

	case "up":
		// ↑/↓ read the room only when there is no sentence in the box, which is
		// the same rule that decides whether they walk history or move the caret
		// out in the conversation.
		if a.input.empty() {
			a.roomScroll(-1)
			return nil, true
		}
	case "down":
		if a.input.empty() {
			a.roomScroll(1)
			return nil, true
		}
	}
	return nil, false
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
	for _, r := range rows {
		snapshot = append(snapshot, r.text)
		stripped = append(stripped, ansi.Strip(r.text))
	}
	top := a.roomOffsetFor(len(rows), height)
	a.copy = copyMode{
		on: true, rows: snapshot, text: stripped,
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

// runningNodes are the rail's nodes that have somebody in them, in the rail's
// own order.
//
// A QUEUED NODE IS NOT ONE. It is on the rail — it is going to run — but it has
// no worker yet, so its room is a page with nothing on it and nothing coming,
// and steering it gets the engine's "no worker to talk to yet". A forward key
// that landed there would be a key that mostly opens empty pages.
func (a *app) runningNodes() []*taskNode {
	nodes := a.railNodes()
	out := make([]*taskNode, 0, len(nodes))
	for _, node := range nodes {
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
func (a *app) railPress(x, y int) (tea.Cmd, bool) {
	if !a.railShowing() || x < a.bodyWidth() {
		return nil, false
	}
	if node := a.railNodeAt(y); node != nil {
		a.openRoomFor(node.id, node.title)
	}
	return a.takeRoomPump(), true
}

// ── the room, drawn ─────────────────────────────────────────────────────────
//
//	› check the config under etc/
//	  I'll look at the loader first.
//	· read internal/config/load.go
//	· grep etc/
//	task finished — esc to return
//
// The spacing is the transcript's law in miniature: one blank between blocks,
// and none between the lines of a run of calls, because a run of calls is one
// thing (render.go's [app.layout] states the same rule for the same reason).

// roomRows builds the room's row list, cached on its own content and width.
func (a *app) roomRows(width int) []row {
	room := a.room
	if room == nil || width < 4 {
		return nil
	}
	if room.rows != nil && room.width == width && !room.dirty {
		return room.rows
	}
	out := make([]row, 0, len(room.lines)*2+2)
	gap := func() {
		if len(out) > 0 {
			out = append(out, row{entry: -1})
		}
	}
	wasCall := false
	for i := range room.lines {
		line := &room.lines[i]
		painted := a.roomLineRows(line, width)
		if len(painted) == 0 {
			continue
		}
		if !(line.kind == roomCalled && wasCall) {
			gap()
		}
		for _, text := range painted {
			out = append(out, row{text: text, entry: -1})
		}
		wasCall = line.kind == roomCalled
	}
	if room.done {
		// THE FOOT. A room on a node that has landed says so once, at the bottom,
		// where the next thing would have appeared — which is the place a person
		// is already looking when they wonder why nothing is.
		gap()
		out = append(out, row{text: a.pal.dim(fit(roomFinishedWord, width)), entry: -1})
	}
	room.rows, room.width, room.dirty = out, width, false
	return out
}

// roomLineRows paints one block.
func (a *app) roomLineRows(line *roomLine, width int) []string {
	switch line.kind {
	case roomAsked:
		// The person's own words, in the person's own hue, with every
		// continuation aligned under the TEXT: render.go's user block, exactly.
		body := wrap(line.text, width-2)
		out := make([]string, 0, len(body))
		for i, text := range body {
			lead := "  "
			if i == 0 {
				lead = a.pal.accent(a.pal.youGlyph())
			}
			out = append(out, lead+a.pal.accent(text))
		}
		return out

	case roomCalled:
		// ONE DIM LINE, and it does not expand. See this file's header for why a
		// room does not rebuild the tool view.
		return []string{a.pal.dim("· " + fit(line.text, width-2))}

	case roomNoted:
		out := make([]string, 0, 2)
		for _, text := range wrap(line.text, width) {
			out = append(out, a.pal.dim(text))
		}
		return out

	default:
		// The node's words are drawn PLAIN — wrapped, not rendered. Markdown is
		// the conversation's shape for an answer somebody is reading; a room is a
		// window onto work in progress, and a heading rule drawn across a
		// half-finished sentence is furniture on a thing that is still moving.
		out := make([]string, 0, 4)
		for _, text := range wrap(line.text, width-2) {
			out = append(out, "  "+a.pal.ink(text))
		}
		return out
	}
}

// roomWindow is the room's visible slice and the padding above it. It is
// [app.window]'s shape over the room's own rows and the room's own offset —
// which is the whole mechanism behind "esc restores the scroll exactly".
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
	at := a.roomOffsetFor(total, height) + delta
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
	lane := roomSteerLane + a.room.title + "…"
	if a.room.done {
		lane = roomFinishedWord
	}
	room := width - ansi.StringWidth(prompt)
	out := append([]string(nil), rows...)
	out[0] = a.pal.dim(prompt) + a.pal.dim(fit(lane, room))
	return out
}
