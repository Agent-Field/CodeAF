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
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
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

	// orch is set when this page is an ADAPTIVE RUN rather than a node
	// (roomorch.go): the same room, the same doors, the same geometry, drawing a
	// graph instead of a transcript. It is a field on this struct rather than a
	// second kind of page for the reason the page is built out of the
	// conversation's blocks — everything room.go promises (esc restores the
	// transcript, the rail stays, the scroll is the room's own, copy mode freezes
	// what is drawn) has to hold for both, and the only way to guarantee that is
	// for both to BE a room.
	orch *orchRun

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
	// roomLegendWord replaces the path in the legend while a room is open: where
	// you are, and the keys that leave. It names both of them for the reason the
	// header does — a person's hand is either on esc or on the arrows.
	roomLegendWord = "room · esc/←← main"
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

	lane, err := doors.WatchTask(id)
	if err != nil {
		// An unknown id. The room still opens — the journal is worth reading —
		// and it opens finished, because there is nothing to listen to. A call the
		// file left running is resolved on the way in for that same reason:
		// nothing is coming for it here either.
		room.done = true
		a.roomResolveUnfinished()
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
// A MISSING RESULT IS A CALL THAT HAS NOT COME BACK, and it is drawn as one. It
// is the only thing on this page derived from an ABSENCE, and the absence is
// load-bearing: internal/session writes the assistant message before the tool
// batch runs, so a node caught mid-call journals the asking and nothing else.
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
				// A CALL WITH NO RESULT UNDER IT HAS NOT COME BACK, and the file is
				// the only thing that can say so. internal/session writes the
				// assistant message BEFORE the tool batch runs (its loop.go), so a
				// node caught mid-call journals the asking and nothing else — and a
				// page that drew every journaled call as finished told a person the
				// work was further along than it is, then left the row inert when the
				// real end arrived on the live lane with nothing to land on.
				//
				// The clock is NOT invented to go with it: began stays zero, so the
				// row shows no age (toolview.go). Nobody measured when it started.
				output, answered := results[call.ID]
				status := toolRunning
				if answered {
					status = toolOK
				}
				out = append(out, entry{
					kind: entryTool, tool: name, turn: turn, status: status,
					// THE PROVIDER'S ID IS KEPT because it is the call's identity in the
					// file, and it is what any pairing by id has to pair on. Nothing
					// pairs on it today: the end events this row is waiting for carry no
					// id (session's loop.go), so [roomClaimRunning] matches on the
					// payload and then on the name.
					callID: call.ID,
					// UNPARSED, exactly as a replayed call carries it: everything the
					// expansion shows is derived from these two at render time
					// (toolview.go), so a call on a page and a call in the conversation
					// go through one renderer and cannot disagree.
					detail: toolDetail{
						Args:   journalArgs(call.Function.Arguments),
						Output: journalOutput(output),
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
//
// THE SPEND IS NOT COUNTED HERE, AND THE OMISSION IS THE DESIGN. A node's turn
// totals are folded by its PILOT (task.go's [app.pilotEvent]), which keeps flying
// for as long as the node runs whether or not anybody is standing in its room —
// [app.openRoom] opens a watch of its own beside it rather than taking the
// pilot's over, because internal/session's door is a fan-out (task_room.go:
// openRoom().join()) and hands each caller its own copy of every event. A fold
// on this lane as well would therefore bill every turn twice for exactly as long
// as a person had the page open.
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

	case session.EventToolForming:
		// THE NODE'S CALL IS ARRIVING, drawn while it arrives — the same event
		// the conversation draws from (app.go's [app.formTool]). A room without
		// this said nothing at all while a node streamed a file out, which is
		// the exact gap the forming row was built to close, left open in the one
		// place a person goes BECAUSE they want to watch.
		a.roomFormTool(ev)

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

// roomFormTool draws — and keeps redrawing — the row for a call the node is
// STILL SPELLING OUT. It is [app.formTool] over the room's list, down to the
// rule that nothing is parsed: the row holds how much has arrived and the gloss
// session built from the fields that have closed, never the half-sent JSON.
//
// There is no spawn card half here, unlike out in the conversation: a node does
// not propose tasks to the person standing in its room.
func (a *app) roomFormTool(ev session.Event) {
	room := a.room
	if room == nil {
		return
	}
	at := claimForming(room.entries, ev)
	if at < 0 {
		a.roomCloseLive()
		a.roomAppend(entry{
			kind: entryTool, tool: ev.Tool, text: ev.Hint, turn: room.turn,
			status: toolForming, callID: ev.CallID, bytes: ev.Bytes,
		})
		return
	}
	// Every field is taken FORWARD only, for [app.formTool]'s reason: a later
	// fragment that carried less than the one before it must not un-say what the
	// row already knows.
	e := &room.entries[at]
	e.callID = firstNonEmpty(ev.CallID, e.callID)
	e.tool = firstNonEmpty(ev.Tool, e.tool)
	e.text = firstNonEmpty(ev.Hint, e.text)
	if ev.Bytes > e.bytes {
		e.bytes = ev.Bytes
	}
	a.roomTouched()
}

// roomResolveUnfinished resolves every call that was still in the air when the
// node's lane ended. It is [app.dropForming] over the room's list, widened by
// one state, and it exists for that function's reason: a lane that has closed is
// the last word there will ever be about the calls on this page — no
// announcement, no begin, no end is coming — so a row left in a live state is
// the page animating work that is over.
//
// THE WIDENING IS THE JOURNAL'S. A call the file names with no result under it
// is drawn as RUNNING ([readRoomJournal]), which is true of a node still working
// and false the moment its lane closes; before that state existed, the only row
// that could be caught out this way was a forming one.
//
// Every row is RESOLVED, never removed: the node started asking for something
// and stopped, which is a fact about what happened, and a row that vanished
// would take it with it. The status is left alone for the same reason — this
// surface does not know whether the call ran, and "failed" is a claim about
// something nobody watched.
func (a *app) roomResolveUnfinished() {
	room := a.room
	if room == nil {
		return
	}
	now := a.now()
	for i := range room.entries {
		e := &room.entries[i]
		if e.kind != entryTool || !e.ended.IsZero() {
			continue
		}
		if e.forming() || e.status.live() {
			e.ended = now
		}
	}
	a.roomTouched()
}

// roomAnnounceTool draws the row for a call the node has finished asking for.
// It is [app.announceTool] over the room's list, and it exists for the same
// reason: the change an edit is ABOUT to make is previewed from the arguments,
// and the moment that preview is worth anything is the moment before it happens.
//
// IT ADOPTS THE FORMING ROW rather than drawing a second one — one call, one
// line, from the first fragment to the last — and it pairs by the call's id,
// which session's announcement carries (its loop.go).
func (a *app) roomAnnounceTool(ev session.Event) {
	room := a.room
	if room == nil {
		return
	}
	if at := claimFormed(room.entries, ev); at >= 0 {
		e := &room.entries[at]
		e.status = toolQueued
		e.tool = firstNonEmpty(ev.Tool, e.tool)
		e.text = firstNonEmpty(ev.Hint, e.text)
		e.detail.Args = firstNonEmpty(ev.Args, e.detail.Args)
		a.roomTouched()
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
	at := roomClaimAnnounced(room.entries, ev)
	if at < 0 {
		// A call that formed and then began with no announcement between them.
		// The ordering law says that cannot happen, and a row left pulsing at a
		// call that is already running would be the page believing the law over
		// the event in its hand ([app.beginTool] says the same).
		at = claimFormed(room.entries, ev)
	}
	if at >= 0 {
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

// roomClaimRunning finds the live row this END belongs to, or -1. It is
// [roomClaimAnnounced]'s rule one state later, and it earned its own function
// when the journal began drawing calls it left unanswered as running
// ([readRoomJournal]): oldest-of-that-tool was a guess that was right by
// convention, and a page can now hold two `bash` rows at once — one off the file
// and one off the lane — where the guess is wrong half the time.
//
// The payload is matched first for that reason, and it is a PREFERENCE rather
// than a key: session renders a call's arguments for the end event and this
// surface renders them again off the journal, which agree for an ordinary call
// and can differ on one long enough to be clipped. So the walk by name is what
// is left, exactly as it is for an announcement.
func roomClaimRunning(es []entry, ev session.Event) int {
	fallback := -1
	for i := range es {
		e := &es[i]
		if e.kind != entryTool || !e.status.live() || e.tool != ev.Tool {
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

// roomCloseTool resolves the live line this end belongs to. It is
// [app.closeTool] over the room's list, failure-opens-itself included: a call
// that failed is the one row whose detail is the reason the person came in here.
func (a *app) roomCloseTool(ev session.Event, status toolState, why string) {
	room := a.room
	if room == nil {
		return
	}
	if at := roomClaimRunning(room.entries, ev); at >= 0 {
		e := &room.entries[at]
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
	// THE STATUS SHEET IS ON THIS LIST FOR THE REASON THE SETTINGS PANEL IS, and
	// it was missing from it: it is a FULLSCREEN overlay at the same rung
	// (input.go reads the two one after the other), so while it is up there is
	// nothing of the room on screen for a key to mean anything to — and the two
	// keys this function takes are the two the sheet needs most. Without it, esc
	// closed the room out from under a sheet the person was reading and enter
	// steered the node instead of opening the line under the cursor, which is the
	// door a phone-tier press on the task's model chip now arrives through
	// (statusdeck.go's [app.deckModelRow]).
	switch key := msg.String(); {
	case key == "ctrl+c", a.asking(), a.awaitingTask(),
		a.sheet.open, a.deckShowing(), a.pick.open, a.roster.open, a.copy.on,
		a.welcome.open, a.menu.open, a.comp.open:
		return nil, false
	}
	// THE GUARD IS READ BEFORE THE ROOM, and it is the same rung: it is a
	// question raised by the room's own enter, so the keys it answers with have
	// to outrank the key that raised it. Everything above still outranks both.
	if cmd, taken := a.guardKey(msg); taken {
		return cmd, true
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
	// AN ADAPTIVE RUN IS THE FIRST STOP, and it is the only keyboard door onto
	// one (roomorch.go): a run is not on the roster — it is not a node, it has no
	// row — so → from the conversation opens the run this session has heard from,
	// when there is one and nothing else is open. From inside any room the key
	// goes back to walking the roster, which is what it has always done.
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
// A HEADING IS THE OTHER TARGET, and it is the fold rather than a door: the
// pointer gets the same two acts the keyboard has (task.go's [app.railKey]), so
// a hundred and forty-eight parked nodes are one click away whichever hand a
// person reached with. The press moves the roster's cursor to what was pressed
// but does NOT take the keyboard: clicks focus what was clicked, and the draft
// is where this surface types.
// OVER THE BODY IT IS THE OTHER WAY ROUND: the roster has the whole width, so
// the question stops being about x and becomes about y — a press inside the body
// region is the roster's, and the pinned rows above it (the header, the strip)
// are not, because those are drawn by somebody else and answer for themselves
// (view.go's [app.topHeight]).
func (a *app) railPress(x, y int) (tea.Cmd, bool) {
	switch {
	case a.railFull():
		top := a.bodyTop()
		if top < 0 || y < top || y >= top+a.viewHeight() {
			return nil, false
		}
	case !a.railShowing() || x < a.bodyWidth():
		return nil, false
	}
	if e, ok := a.railEntryAt(y); ok {
		a.railWhere = railSpotOf(e)
		if e.node == nil {
			a.railToggle(e.group)
			return nil, true
		}
		a.openRoomFor(e.node.id, e.node.title)
	}
	return a.takeRoomPump(), true
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
func (a *app) roomHead(width int) string {
	// [app.headHeight] is what the geometry budgeted for this row, and it is
	// asked rather than second-guessed: a header the frame drew on a short
	// terminal that the scrolling had not subtracted would push the room's last
	// row under the input box.
	a.roomStop = hudSpan{}
	if a.headHeight() == 0 || width < 12 {
		return ""
	}
	left := a.roomHeadWord(width)
	mark := a.roomStopWord()
	attempts := []string{roomBackWord, ""}
	if mark != "" {
		attempts = []string{roomBackWord + roomStopSep + mark, mark, roomBackWord, ""}
	}
	for _, right := range attempts {
		line, ok := a.legendLine(left, right, width, a.pal.accent)
		if !ok {
			continue
		}
		// The mark is the LAST thing in the right label, and [app.legendLine]
		// closes with one space and one rule cell after it — so its columns are
		// arithmetic rather than a second layout, whichever attempt fitted.
		if mark != "" && strings.HasSuffix(right, mark) {
			cols := ansi.StringWidth(mark)
			a.roomStop = hudSpan{from: width - 2 - cols, to: width - 2}
		}
		return line
	}
	return a.pal.accent(fit(left, width))
}

// roomHeadWord is the header's left: the node's mark, the trail, and the three
// facts about the work. Every one of the three is DROPPED when nobody has
// published it — a queued node has no clock, an unpriced one has no cost — for
// the reason the turn footer drops its own fields (timestamps.go): a figure that
// is zero is a figure nobody measured.
//
// It is built PLAIN, without paint, because the whole line is painted once by
// [app.legendLine]: a hue nested inside a hue ends at the inner one's reset, and
// the rest of the line would fall back to the terminal's default mid-sentence.
func (a *app) roomHeadWord(width int) string {
	// A RUN'S PAGE ANSWERS FOR ITS OWN HEADER (roomorch.go): the three facts under
	// it are a node's — a state, a clock, a spend — and a run has none of them.
	// What it has instead is a tank, and the tank is the fact that cannot be left
	// off this line.
	if a.orchOpen() {
		return a.orchHeadWord(width)
	}
	node := a.roomNode()
	word := a.roomMark(node) + " " + a.roomTrail()
	if node == nil {
		// A room on a node this surface has had no update for. The trail is still
		// true and nothing else is, which is exactly what gets said.
		return fit(word, width)
	}
	// The model joins the three because it answers the same kind of question
	// they do — what is true of this work right now — and it is dropped by the
	// same rule when nobody published one. It goes last: the state and the clock
	// change while you watch, and whose hands the work is in was settled before
	// it started.
	for _, part := range []string{a.roomStateWord(node), a.roomClock(node), a.roomSpend(node), strings.TrimSpace(node.model)} {
		if part != "" {
			word += " · " + part
		}
	}
	return fit(word, width)
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
func (a *app) roomMark(node *taskNode) string {
	if node == nil {
		return a.linearMark(glyphQueued, glyphQueuedASCII)
	}
	switch node.state {
	case session.TaskDone:
		return a.linearMark(glyphDone, glyphDoneASCII)
	case session.TaskFailed:
		return a.linearMark(glyphBad, glyphBadASCII)
	case session.TaskUnverified:
		// THE THIRD SETTLED STATE WEARS THE RAIL'S THIRD MARK (task.go's
		// [glyphUnverified]), and without this case it wore the QUEUED glyph: a
		// node that ran to the end, drawn on its own page as though it had not
		// started. It is the same cell in both glyph tiers, so there is nothing
		// for [app.linearMark] to stand in for.
		return glyphUnverified
	case session.TaskRunning:
		if a.linear {
			return glyphRunASCII
		}
		return tokens.Spinner(a.paints / spinnerStep)
	default:
		return a.linearMark(glyphQueued, glyphQueuedASCII)
	}
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
	switch node.merge {
	case mergeWordConflicted:
		return mergeWordConflicted
	case mergeWordAborted:
		return taskStoppedWord
	case "":
		return roomDoneWord
	default:
		return node.merge
	}
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
func (a *app) roomChip() string {
	if a.room == nil {
		return ""
	}
	// A RUN'S PAGE WEARS THE RUN'S MARK (roomorch.go). Without this the cluster
	// took [app.roomMark]'s answer for a node this surface has never seen — the
	// queued glyph — which would draw a run that is spending money as work that
	// has not started.
	if a.room.orch != nil {
		return a.orchHeadMark() + " " + a.room.title
	}
	return a.roomMark(a.roomNode()) + " " + a.room.title
}

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
	if room.rows != nil && room.width == width && !room.dirty {
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
		room.rows, room.width, room.dirty = out, width, false
		return out
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
	lane := roomSteerLane + a.room.title + roomSteerBack
	if a.room.orch != nil {
		// A RUN HAS NO WORKER TO TALK TO, so the box does not offer to steer one:
		// the sentence goes to the PLANNER, which reads it on its next call
		// (roomorch.go). The placeholder says whose ear it is for the reason it
		// names a node out here — the box is the same box either way, and "who is
		// listening" is the question it exists to answer.
		lane = orchSteerLane + roomSteerBack
	}
	if a.room.done {
		lane = roomFinishedWord
	}
	room := width - ansi.StringWidth(prompt)
	out := append([]string(nil), rows...)
	out[0] = a.pal.dim(prompt) + a.pal.dim(fit(lane, room))
	return out
}
