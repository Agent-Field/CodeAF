package tui3

import (
	"bufio"
	"bytes"
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

// taskRoom is one node's page: what it has said, the lane carrying what it says
// next, and where the reader is in it.
type taskRoom struct {
	id    uint64
	title string
	// entries is the page, in the conversation's own blocks (app.go's [entry]).
	// It is the same list a transcript is, built from the node's journal and
	// then grown by its lane, and it is drawn by the conversation's renderers
	// through [taskRoom.deck].
	entries []entry
	// unfolded is the page's OWN fold state, keyed by the page's own turns. It is
	// not the conversation's map for the reason the entries are not the
	// conversation's list: a turn number means nothing outside the list it counts
	// (render.go's [deck]).
	unfolded map[int]bool
	// turn counts the page's turns — the node's first instruction, then every
	// line steered into it. It is what groups a tool cluster and what ctrl+o
	// folds, exactly as [app.turn] is out in the conversation.
	turn int
	lane <-chan session.Event
	// gen is the generation device the two other lanes on this surface use
	// (app.go's stream, task.go's standing subscription): a room that was closed
	// while its channel still had events in flight must not paint into the room
	// that replaced it.
	gen int
	// live is the index of the node's growing assistant block, or -1, and think
	// the index of its reasoning block, or -1. Both are [app.live] and
	// [app.think] applied to this list, and the folders below are those two
	// files' rules restated over it.
	live  int
	think int
	// mdAt is the markdown promotion clock for the live block (app.go's
	// [app.promoteMarkdown], which runs over this list too).
	mdAt time.Time
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

// deck is the page as the renderers take it (render.go). It is a view over the
// live fields rather than a copy: the row cache each entry carries is written
// through it.
func (r *taskRoom) deck() deck {
	return deck{entries: r.entries, unfolded: r.unfolded}
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
	// roomFinishedRefusal is what a line typed at a landed node gets. It is a
	// refusal rather than a silent drop because the person pressed enter and is
	// owed an answer about where their sentence went.
	roomFinishedRefusal = "the task has finished — nothing is listening"
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
	room := &taskRoom{
		id: id, title: title, gen: a.roomGen,
		unfolded: map[int]bool{},
		live:     -1,
		think:    -1,
		mdAt:     a.now(),
		stick:    true,
		dirty:    true,
	}
	room.entries, room.turn = readRoomJournal(doors.TaskJournal(id), a.pal)
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
// below are the ones a page draws and nothing else — an unknown line kind, an
// unknown role and a field this build does not know are all skipped rather than
// guessed at, which is what makes an older or a newer file readable.
type journalLine struct {
	Type       string        `json:"type"`
	Role       string        `json:"role"`
	Content    string        `json:"content"`
	ToolCalls  []journalCall `json:"toolCalls"`
	ToolCallID string        `json:"toolCallId"`
	// Parts are the message's non-text content parts as the journal kept them —
	// WHERE the bytes were, never the bytes (session's sessionfile.go). The path
	// is the only field a page needs: a picture is drawn as its name here for the
	// reason it is drawn as its name in the conversation (attach.go's
	// [chipMarkers]).
	Parts []journalPart `json:"parts"`
}

type journalCall struct {
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type journalPart struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

// readRoomJournal turns a node's session file into the page's blocks, oldest
// last, and reports how many turns it counted.
//
// It is [app.replay] over a file instead of over an open agent (replay.go), and
// it is deliberately the same shaping internal/session does for a resumed
// conversation (agent.go's shapeEntries, which this mirrors): the results are
// INDEXED FIRST and then the messages are walked, because a call's arguments
// ride the assistant message that made it and its result is a separate message
// keyed by the call's id. One pass to index, one to shape.
//
// TOOL RESULTS ARE KEPT, which is the whole of what makes a call on this page
// expandable. They were skipped when a room row was one dim line — there was
// nothing for a payload to open into — and skipping them now would leave a page
// of rows that hover, click, and open onto nothing (replay.go's [replayInert]
// says why that is the one thing a row must never do).
//
// A path that is empty, missing or unreadable is not an error and draws nothing:
// the journal is EVIDENCE, not a prerequisite (internal/session says so where it
// mints the path), and a room that refused to open because a file was not there
// would be refusing to show the live work as well.
func readRoomJournal(path string, pal palette) ([]entry, int) {
	lines := readJournalLines(path)
	// The results, indexed by the call each one answered. A result with no id is
	// skipped rather than kept under "", for the reason session's own index skips
	// it: it is a message no call can claim.
	results := make(map[string]string, 8)
	for _, line := range lines {
		if line.Role == "tool" && line.ToolCallID != "" {
			results[line.ToolCallID] = line.Content
		}
	}

	var out []entry
	turn := 0
	for _, line := range lines {
		text := strings.TrimSpace(line.Content)
		switch line.Role {
		case "user":
			// The pictures are part of what was said, so a message that was only a
			// picture is still a message: the markers alone are the line
			// (replay.go's [replayUserLine], whose rule this is).
			said := journalUserText(text, line.Parts, pal)
			if said == "" {
				continue
			}
			// The turn counter moves with the person's messages, exactly as it does
			// in the conversation: it is what groups a cluster and what ctrl+o folds.
			// A node's first "message" is the instruction it was given, so a page
			// opens on turn one the way a conversation does.
			turn++
			out = append(out, entry{kind: entryUser, text: said, turn: turn})

		case "assistant":
			if text != "" {
				out = append(out, entry{
					kind: entryAssistant, text: text, turn: turn, settled: true,
				})
			}
			for _, call := range line.ToolCalls {
				name := strings.TrimSpace(call.Function.Name)
				if name == "" {
					continue
				}
				out = append(out, entry{
					kind: entryTool, tool: name, turn: turn, status: toolOK,
					// UNPARSED, exactly as a replayed call carries it: everything the
					// expansion shows is derived from these two at render time
					// (toolview.go), so a call on a page and a call in the conversation
					// go through one renderer and cannot disagree.
					detail: toolDetail{
						Args:   journalArgs(call.Function.Arguments),
						Output: journalOutput(results[call.ID]),
					},
				})
			}
		}
	}
	if len(out) > roomTail {
		out = out[len(out)-roomTail:]
	}
	return out, turn
}

// readJournalLines is the file, parsed. An unknown line kind, an unknown role
// and a field this build does not know are all skipped rather than guessed at,
// which is what makes an older or a newer file readable.
func readJournalLines(path string) []journalLine {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	var out []journalLine
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
		out = append(out, line)
	}
	return out
}

// journalUserText is one journaled message as the person sent it: their words,
// and the names of the pictures that went with them. It is [replayUserLine]'s
// rule applied to what a FILE kept rather than to what an open agent answered —
// same markers, same separator — because a message drawn one way in the
// conversation and another way on a page is two records of one thing.
func journalUserText(text string, parts []journalPart, pal palette) string {
	pictures := make([]chip, 0, len(parts))
	for _, part := range parts {
		if part.Type != journalPartImage {
			continue
		}
		if path := strings.TrimSpace(part.Path); path != "" {
			pictures = append(pictures, chip{path: path})
		}
	}
	return userLine(text, pictures, pal)
}

// journalPartImage is the one non-text part a person's message can carry today,
// spelled as session's journal spells it (sessionfile.go's journalPartImage).
const journalPartImage = "image"

// journalArgs and journalOutput are session's two display renderings, applied to
// what the file kept (loop.go's argsText and capOutput). They are restated here
// rather than imported because they are unexported there — and they are restated
// EXACTLY, because the caps are what every width and every "… N more bytes" on
// an expanded row is measured against.
func journalArgs(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, []byte(raw)); err == nil {
		raw = compacted.String()
	}
	return clipBytes(raw, journalArgsLimit)
}

func journalOutput(text string) string {
	if len(text) <= journalOutputLimit {
		return text
	}
	cut := journalOutputLimit
	for cut > 0 && !runeStart(text[cut]) {
		cut--
	}
	return text[:cut] + "… (" + itoa(len(text)-cut) + " more bytes)"
}

// The two display caps, from internal/session's loop.go: 8k of arguments
// (because an edit's diff is computed from them and a shorter cap produced the
// wrong number) and 4k of result (a screen or two, which is what an expanded row
// is for).
const (
	journalArgsLimit   = 8192
	journalOutputLimit = 4000
)

// clipBytes cuts at a byte budget without splitting a rune.
func clipBytes(text string, n int) string {
	if len(text) <= n {
		return text
	}
	cut := n - len("…")
	for cut > 0 && !runeStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
}

func runeStart(b byte) bool { return b&0xC0 != 0x80 }

// ── the live lane ───────────────────────────────────────────────────────────

// roomEvent folds one of the child's events in and re-arms the pump.
//
// IT IS [app.event]'s SWITCH, over the page's list. The kinds it takes are the
// kinds a node produces that a person in here is watching for — what it is
// saying, what it is THINKING, what it is doing, what it cost itself in
// compaction, and what went wrong. A node's usage and its title are the child
// agent's accounting, and nothing in a task can ask this keyboard a question
// (its policy allows everything but the floor, and the floor refuses rather than
// prompts), so there is no question in here to draw.
func (a *app) roomEvent(ev session.Event) tea.Cmd {
	room := a.room
	if room == nil {
		return nil
	}
	// THE COLLAPSE RULE, quoted from the conversation's pump (app.go's [app.event],
	// thinking.go): the first thing a turn says that is not reasoning ends the
	// reasoning block, and EventThinking is exempt because it is the marker that
	// opened the run.
	if ev.Kind != session.EventReasoning && ev.Kind != session.EventThinking {
		a.roomCollapseThought()
	}
	switch ev.Kind {
	case session.EventTextDelta:
		a.roomSay(ev.Text)

	case session.EventReasoning:
		a.roomThink(ev.Text)

	case session.EventToolAnnounced:
		a.roomAnnounceTool(ev)

	case session.EventToolBegin:
		a.roomBeginTool(ev)

	case session.EventToolEnd:
		a.roomCloseTool(ev, toolOK, "")

	case session.EventToolFailed:
		a.roomCloseTool(ev, toolFailed, firstNonEmpty(ev.Hint, errText(ev.Err)))

	case session.EventCompacting:
		a.roomCloseLive()
		a.roomAppend(entry{
			kind:  entryCompact,
			text:  firstNonEmpty(ev.Hint, "compacting"),
			turn:  room.turn,
			began: a.now(),
		})

	case session.EventCompacted:
		a.roomCloseLive()
		a.roomSettleCompaction(firstNonEmpty(ev.Hint, "compacted"))

	case session.EventError:
		a.roomNote("error: " + errText(ev.Err))
	}
	a.touch()
	return tea.Batch(waitRoom(room.lane, room.gen), a.wake())
}

// roomSay grows the node's live block, opening one when the last thing on the
// page was anything else. It is [app.appendText] over the room's list.
func (a *app) roomSay(text string) {
	room := a.room
	if text == "" || room == nil {
		return
	}
	if room.live < 0 || room.live >= len(room.entries) ||
		room.entries[room.live].kind != entryAssistant {
		room.entries = append(room.entries, entry{kind: entryAssistant, turn: room.turn})
		room.live = len(room.entries) - 1
		room.mdAt = a.now()
	}
	e := &room.entries[room.live]
	e.text += text
	e.stale = true
	a.roomTouched()
}

// roomThink grows the node's reasoning block. It is [app.appendThought] over the
// room's list, down to the rule that the reply in progress is closed first so
// the block lands above the answer rather than splitting a paragraph that is
// still being written (thinking.go).
func (a *app) roomThink(text string) {
	room := a.room
	if text == "" || room == nil {
		return
	}
	if room.think < 0 || room.think >= len(room.entries) ||
		room.entries[room.think].kind != entryThinking {
		a.roomCloseLive()
		now := a.now()
		room.entries = append(room.entries, entry{
			kind: entryThinking, turn: room.turn, began: now, ended: now,
		})
		room.think = len(room.entries) - 1
	}
	e := &room.entries[room.think]
	e.text += text
	e.ended = a.now()
	e.stale = true
	a.roomTouched()
}

// roomCollapseThought settles the streaming reasoning block ([app.collapseThought]).
func (a *app) roomCollapseThought() {
	room := a.room
	if room == nil || room.think < 0 {
		return
	}
	if room.think < len(room.entries) && room.entries[room.think].kind == entryThinking {
		e := &room.entries[room.think]
		e.settled, e.stale = true, true
	}
	room.think = -1
	a.roomTouched()
}

// roomCloseLive ends the assistant block being streamed into ([app.closeLive]).
func (a *app) roomCloseLive() {
	room := a.room
	if room == nil {
		return
	}
	if room.live >= 0 && room.live < len(room.entries) {
		e := &room.entries[room.live]
		e.settled, e.stale = true, true
	}
	room.live = -1
}

// roomAnnounceTool draws the row for a call the node has finished asking for.
// It is [app.announceTool] over the room's list, and it exists for the same
// reason: the change an edit is ABOUT to make is previewed from the arguments,
// and the moment that preview is worth anything is the moment before it happens.
func (a *app) roomAnnounceTool(ev session.Event) {
	room := a.room
	if room == nil {
		return
	}
	a.roomCloseLive()
	a.roomAppend(entry{
		kind: entryTool, tool: ev.Tool, text: ev.Hint, turn: room.turn,
		status: toolQueued, detail: toolDetail{Args: ev.Args},
	})
}

// roomBeginTool is EXECUTION STARTED, and it adopts the row the announcement
// drew ([app.beginTool], whose pairing rule [roomClaimAnnounced] restates).
func (a *app) roomBeginTool(ev session.Event) {
	room := a.room
	if room == nil {
		return
	}
	if at := roomClaimAnnounced(room.entries, ev); at >= 0 {
		e := &room.entries[at]
		e.status = toolRunning
		e.began = a.now()
		e.detail.Args = firstNonEmpty(ev.Args, e.detail.Args)
		e.text = firstNonEmpty(ev.Hint, e.text)
		a.roomTouched()
		return
	}
	a.roomCloseLive()
	a.roomAppend(entry{
		kind: entryTool, tool: ev.Tool, text: ev.Hint, turn: room.turn,
		status: toolRunning, began: a.now(), detail: toolDetail{Args: ev.Args},
	})
}

// roomClaimAnnounced finds the queued row this begin belongs to, or -1. The
// payload is matched first and the name only after, for the reason
// [app.claimAnnounced] states: a batch of three edits announces three rows, and
// pairing by name alone would start the clock on whichever was drawn first.
func roomClaimAnnounced(es []entry, ev session.Event) int {
	fallback := -1
	for i := range es {
		e := &es[i]
		if e.kind != entryTool || e.status != toolQueued || e.tool != ev.Tool {
			continue
		}
		if ev.Args != "" && e.detail.Args == ev.Args {
			return i
		}
		if fallback < 0 {
			fallback = i
		}
	}
	return fallback
}

// roomCloseTool resolves the oldest still-running line for that tool. It is
// [app.closeTool] over the room's list, failure-opens-itself included: a call
// that failed is the one row whose detail is the reason the person came in here.
func (a *app) roomCloseTool(ev session.Event, status toolState, why string) {
	room := a.room
	if room == nil {
		return
	}
	for i := range room.entries {
		e := &room.entries[i]
		if e.kind != entryTool || !e.status.live() || e.tool != ev.Tool {
			continue
		}
		e.status = status
		e.ended = a.now()
		e.detail.Args = firstNonEmpty(ev.Args, e.detail.Args)
		e.detail.Output = firstNonEmpty(ev.Output, why)
		if why != "" && status == toolFailed {
			e.text = strings.TrimSpace(e.text + " — " + why)
		}
		if status == toolFailed {
			e.open = true
		}
		a.roomTouched()
		return
	}
	// A close with no open line still deserves to be seen rather than silently
	// dropped: the node said something happened.
	if status == toolFailed {
		a.roomAppend(entry{
			kind: entryTool, tool: ev.Tool, text: why, turn: room.turn, status: toolFailed,
			open:   true,
			detail: toolDetail{Args: ev.Args, Output: firstNonEmpty(ev.Output, why)},
		})
	}
}

// roomSettleCompaction stops the newest compaction row's clock, or draws one
// born finished when this page never saw the pass start ([app.settleCompaction]).
func (a *app) roomSettleCompaction(text string) {
	room := a.room
	if room == nil {
		return
	}
	for i := len(room.entries) - 1; i >= 0; i-- {
		e := &room.entries[i]
		if e.kind != entryCompact || !e.ended.IsZero() {
			continue
		}
		e.text, e.ended = text, a.now()
		e.stale = true
		a.roomTouched()
		return
	}
	now := a.now()
	a.roomAppend(entry{
		kind: entryCompact, text: text, turn: room.turn, began: now, ended: now,
	})
}

// roomNote is the surface's own line inside the room. It is [app.note] over the
// room's list, and it draws the same dim "· " block.
func (a *app) roomNote(text string) {
	room := a.room
	if room == nil {
		return
	}
	if text = strings.TrimSpace(text); text != "" {
		a.roomCloseLive()
		a.roomAppend(entry{kind: entryNote, text: text, turn: room.turn})
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
// A node that has landed REFUSES rather than swallowing the line. The box is not
// cleared in that case: the sentence is still the person's, and taking it away
// after telling them it went nowhere would be the surface losing their words
// twice.
func (a *app) steer() tea.Cmd {
	room := a.room
	line := strings.TrimSpace(a.input.String())
	if room == nil || line == "" {
		return nil
	}
	if room.done {
		a.roomNote(roomFinishedRefusal)
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
		// away the half that says what to do about it.
		a.roomNote(err.Error())
		return nil
	}
	a.input.reset()
	a.endRecall()
	a.closeLists()
	// A STEERED LINE OPENS A TURN, the way a person's message opens one in the
	// conversation (attach.go's [app.submit] path, render.go's turn counter): it
	// is what groups the calls that answer it into one cluster and what ctrl+o
	// folds. The chips are not spent here — a room's box sends words, and the tray
	// belongs to the conversation.
	a.roomCollapseThought()
	a.roomCloseLive()
	room.turn++
	a.roomAppend(entry{kind: entryUser, text: line, turn: room.turn})
	return a.edited()
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
	if room.rows != nil && room.width == width && !room.dirty {
		return room.rows
	}
	out, closed := a.deckRows(room.deck(), width)
	if room.done {
		// THE FOOT. A room on a node that has landed says so once, at the bottom,
		// where the next thing would have appeared — which is the place a person
		// is already looking when they wonder why nothing is. It takes the blank a
		// closed block above it asks for, which is what [app.deckRows] reports —
		// the same rule the conversation's ellipsis is drawn under.
		if closed && len(out) > 0 {
			out = append(out, row{entry: -1})
		}
		out = append(out, row{text: a.pal.dim(fit(roomFinishedWord, width)), entry: -1})
	}
	// THE POINTER, LAST, exactly as in the conversation (render.go's layout).
	a.hoverPass(out, width)
	room.rows, room.width, room.dirty = out, width, false
	return out
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
