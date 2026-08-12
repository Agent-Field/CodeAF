package chat

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	// Aliased: this package already has a `prose` type of its own
	// (markdown.go), and the record needs the markdown renderer's highlighter
	richtext "github.com/Agent-Field/aforge-v2/internal/tui2/prose"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The execution trace: what a worker actually DID, which is the one thing every
// task room on this machine could not say.
//
// THE REPORT, twice, in the reader's own words: "we need to be very clear what
// the task is and what tools are used — very similar to how it was in v1", and
// before it "the task page has no tool use or conversation or anything".
//
// H13 answered the second one by measuring the JOURNAL and concluding the data
// does not exist: not one tool call is journaled anywhere, no `PartKind` for one
// exists, and therefore "a room can show a RESULT and a RECEIPT and can never
// show a TRACE". Every measurement in that finding is correct and its conclusion
// was wrong, because it asked the wrong table. **V1 NEVER READ THE JOURNAL FOR
// THIS.** Its node page reads a FILE the executor writes beside the work —
// `<job>/.aforge/trace/<seq>.trace.log`, one line per model turn, per tool call
// and per tool result — and that file is on disk right now for every job this
// profile has ever run, including the deep-research job the report was written
// about. The trace was never missing. It was in a place no v2 surface had asked.
//
// So this is the lens law (Decision 7) and nothing more inventive than that: the
// SAME source v1 reads, read through v2's grammar. What is new here is the
// grammar, not the fact.
//
// THREE THINGS THIS DELIBERATELY DOES NOT DO.
//
//   - It does not re-spell the path. internal/exec owns where a recorder lives
//     and exports [exec.TracePath] to say so; the read goes through
//     `command.Commander.NodeTraceTail`, which is v1's own reader with the tui
//     types taken out of its signature. Two spellings of one path is how the
//     node page came to be drawing an empty feed in the first place — see that
//     function's comment, which is a repair as much as a refactor.
//   - It does not invent a glyph HERE. §5 fixes the four voices and names their
//     marks — `✳ $ ⌕ ✎ ›` — so the three the 5.17 table was short are declared
//     where every other glyph in this tree is declared, with a plain-tier twin,
//     a measured width and a nerd-font icon (internal/tui2/tokens). A renderer
//     that spelled a byte itself would be a second vocabulary. A call whose kind
//     cannot be read wears the fold mark alone, and 4.3's own example of that
//     row — `▸ control: cancelled wisp-nav2` — is exactly what it draws.
//   - It does not parse harder than the writer wrote. internal/exec/trace.go
//     emits five shapes and `snip` guarantees every record is exactly one line
//     (newlines become ⏎ on the way out). Anything this reader does not
//     recognise is drawn as the dim line it is rather than dropped, because a
//     recorder that gains a sixth shape tomorrow must not make the room go
//     quiet — which is precisely the failure mode this whole file is fixing.

// Trace is the optional worker-recorder read.
//
// It is asked of the COMMANDER and not of the Backend, for the same reason
// [Graph] is asked of the Backend: it is a capability some hosts have and some
// do not, and a window whose engine cannot answer draws a room with no execution
// rows rather than failing. *command.Commander satisfies it structurally, so the
// v2 entry point wires nothing new — the object was already being handed over.
type Trace interface {
	// NodeTraceTail returns the tail of one node's recorder, the file's stamp,
	// and whether the answer is fresh. A caller that hands back the stamp it was
	// given last gets ("", stamp, false) when nothing has been appended, which is
	// what makes this affordable on every cycle.
	NodeTraceTail(nodeID string, maxBytes int, sinceSize int64, sinceMod time.Time) (string, int64, time.Time, bool)
}

const (
	// traceMaxBytes is how much of one recorder's tail a room reads. v1 reads
	// 64KB; this reads half, because a v2 room draws the tail of up to
	// traceMaxNodes recorders at once where v1 drew exactly one.
	traceMaxBytes = 32 << 10
	// traceMaxNodes bounds the fan-out. A room reads the same subtree its trail
	// read (readNodeCmd), and the deep end of that is capped at maxSubtreeRows;
	// this is the tighter cap, because a trace is a file per node and a trail is
	// a query per node.
	traceMaxNodes = 12
	// traceMaxEvents bounds one node's drawn rows. A long-running worker's
	// recorder is a document; a room is a room.
	traceMaxEvents = 60
	// traceGistCap is how much of a call's argument or a thought's text stands
	// above its own fold.
	traceGistCap = 120
	// traceReasonCap is how much of a failure's why fits beside the mark. A
	// sentence, not a stack.
	traceReasonCap = 48
	// traceBatchShow is how many inputs a batched run names before it counts
	// the rest. Three is what fits beside a verb at an ordinary width and is
	// enough to recognise the run without reading it.
	traceBatchShow = 3
	// traceInputCap is how much of one input a batch shows. A batch is a
	// glance; the rows inside it are the reading.
	traceInputCap = 28
	// traceBoxRows is the bounded output box: what a call's result shows when
	// it is opened, before the rest goes behind one more door. A result is a
	// document and a record is a record — 4.3's collapsed-row law does not stop
	// applying because a row was opened.
	traceBoxRows = 12
)

// The recorder's OWN BYTES, named so no renderer mistakes them for chrome.
//
// These two are the writer's grammar (internal/exec/trace.go) being matched on
// the way IN, not glyphs being drawn on the way out, so they are deliberately
// not tokens slots: pinning them to the display vocabulary would mean a change
// to how the room draws could stop the room from parsing. The glyph audit asked
// for constants rather than literals, and that is exactly the right shape for
// them — one name, one place, and a comment saying which side of the seam they
// live on.
const (
	// traceRulePrefix opens the round rule the parse spends as a boundary.
	traceRulePrefix = "── turn "
	// traceEnterMark is what `snip` writes in place of a newline so one record
	// is one line. The composer lane owns choosing the ONE enter glyph and its
	// table slot for anything DRAWN; this is the byte on disk and follows the
	// writer, not the vocabulary.
	traceEnterMark = "⏎"
)

// nodeTrace is one recorder as this window last read it: the text, and the
// stamp that says whether it is worth reading again.
type nodeTrace struct {
	text string
	size int64
	mod  time.Time
}

// traceReadMsg is one pass over a room's recorders.
type traceReadMsg struct {
	// node is the ROOM's root, so a read that lands after the reader has walked
	// somewhere else is dropped rather than drawn into the wrong room.
	node   string
	traces map[string]nodeTrace
	moved  bool
	// cards says this pass was taken for the CONVERSATION's job cards rather
	// than for an open room, which is a different acceptance test on the way
	// back in: the reader has to still be looking at the thread.
	cards bool
}

// readTraceCmd tails every recorder under an open task room, off the render
// goroutine.
//
// IT RUNS ON QUIET CYCLES TOO, and that is the one scheduling fact worth its
// line. A worker appending to its recorder journals NOTHING — that is the whole
// of why the journal could not answer this question — so `result.quiet`, which
// is the cheap proof that the thread has not moved, is no proof at all that the
// work has not. v1's poll makes the same exception in the same words
// (internal/tui/model.go), and a room that only re-read on journal moves would
// freeze mid-run and look exactly like a worker that had stopped.
//
// What keeps that affordable is the stamp. A recorder that has not grown costs
// one open and one stat; only a file that actually moved is read, allocated and
// re-parsed.
// traceNodes is which nodes under a room have a recorder worth asking for, and
// it is ONE PER RECORDER rather than one per node.
//
// The executor names a recorder by the node's CREATED SEQUENCE
// (`internal/exec/linear.go` hands `task.NodeID` to `newTracer`, and
// `exec.TraceFile` spells it), and a planner splices every part of a job in ONE
// transaction — so every part of a four-part job shares one created seq and
// therefore ONE recorder, which all four workers append to. Measured on the
// reporter's own profile: `task-1961` has five nodes, all at seq 1974, and one
// 120KB `1974.trace.log` between them.
//
// A room that asked per node would draw that same file five times. Deduplicating
// on the seq is not a workaround for that; it is the recorder's own identity
// read correctly. The FIRST node holding a seq keeps it, and workRecord puts the
// surface row first, so a shared recorder lands under the job it belongs to
// rather than under whichever part happened to be walked first.
//
// (That several workers interleave into one file is a producer fact this surface
// does not get to fix and must not hide. The rows come out in APPEND ORDER,
// which is the order the work happened in. 13.17 filed the consequence as a
// numbering problem — the rules restart at `turn 1` partway down a document —
// and that half of it is now moot, because there is no number on screen to
// restart: the boundary is a blank line, and one splice reads as one more
// boundary. What a reader separates the workers by is the rhythm, which is what
// they were separating them by in the first place.)
func (a *App) traceNodes(root string) []string {
	nodes := a.source.subtreeNodes(root)
	out := make([]string, 0, len(nodes))
	seen := make(map[int64]bool, len(nodes))
	for _, id := range nodes {
		node, known := a.source.nodes[id]
		if !known {
			continue
		}
		if seen[node.CreatedSeq] {
			continue
		}
		seen[node.CreatedSeq] = true
		out = append(out, id)
		if len(out) == traceMaxNodes {
			break
		}
	}
	return out
}

func (a *App) readTraceCmd(root string) tea.Cmd {
	reader, ok := a.commander.(Trace)
	if !ok || a.source == nil || root == "" {
		return nil
	}
	nodes := a.traceNodes(root)
	if len(nodes) == 0 {
		return nil
	}
	// The stamps are copied out here, on the update goroutine, because the
	// command runs on another one and the map is the app's.
	since := make(map[string]nodeTrace, len(nodes))
	for _, id := range nodes {
		if held, seen := a.traces[id]; seen {
			since[id] = held
		}
	}
	return func() tea.Msg {
		out := make(map[string]nodeTrace, len(nodes))
		moved := false
		for _, id := range nodes {
			held := since[id]
			text, size, mod, fresh := reader.NodeTraceTail(id, traceMaxBytes, held.size, held.mod)
			if !fresh {
				continue
			}
			next := nodeTrace{text: text, size: size, mod: mod}
			if next == held {
				continue
			}
			out[id] = next
			moved = true
		}
		return traceReadMsg{node: root, traces: out, moved: moved}
	}
}

// applyTraceRead folds one pass into the app and repaints when it changed
// anything. A room the reader has left takes nothing.
//
// THIS IS THE SANITIZE CHOKEPOINT FOR THE RECORDER, and it was missing. A
// recorder file is MODEL- AND TOOL-AUTHORED TEXT — the model's own sentences,
// the arguments it composed, and verbatim bytes some program wrote to a pipe —
// arriving from disk rather than from the store, which is exactly why it slipped
// past the seam every other such string passes ([sanitizeText], 10.2.6/7). Left
// raw, a tool that printed an OSC 8 hyperlink, a cursor-position query or a
// scroll-region escape would have had it replayed into the terminal by a room
// that only meant to quote it, and a bare `\x1b[31m` in a build log would have
// outranked this surface's own tiers.
//
// It is one call at the READ seam and not a rule the renderer has to remember:
// [parseTrace] and every block under it see cleaned text by construction, so no
// row of the record can be the one that forgot. The cost is one pass over a tail
// that just changed, which is the only time this runs at all.
func (a *App) applyTraceRead(msg traceReadMsg) {
	switch {
	case msg.cards:
		// A card read is only worth folding in while the CARDS are on screen.
		if a.view != nil || a.page != pageThread {
			return
		}
	case a.view == nil || a.view.kind != viewNode || a.view.node != msg.node:
		return
	}
	if !msg.moved || len(msg.traces) == 0 {
		return
	}
	if a.traces == nil {
		a.traces = make(map[string]nodeTrace, len(msg.traces))
	}
	for id, held := range msg.traces {
		held.text = sanitizeText(held.text)
		a.traces[id] = held
	}
	if msg.cards {
		// The conversation's cards, not a room's: the recorder moved, so the
		// one-line preview under each running part has something new to say.
		a.refreshCardPreviews()
		return
	}
	// The room's stamp is what decides whether paintRoom rebuilds, and it is a
	// fingerprint of the JOURNAL. A recorder that grew moved nothing the journal
	// can see, so the stamp is cleared rather than recomputed — the next paint is
	// unconditional and the one after it is cheap again.
	a.view.stamp = ""
	a.paintRoom()
}

// -- the conversation's own recorder read --------------------------------------

// readCardTracesCmd tails the recorders under every LIVE job card in the
// conversation, so each running part's row can carry the one line its worker
// last wrote.
//
// THE READER ASKED FOR IT BY ITS ABSENCE: "no 1 line update as its running". A
// job card in the thread showed a growing list of part names and nothing about
// what any of them was doing, while the bytes that say so were on disk the whole
// time — the same file the record page reads (recordtree.go), a second consumer
// of one read.
//
// THE BATTERY LAW IS THE WHOLE OF THE SCHEDULING. It runs only while the THREAD
// is the visible lens and only for cards that are actually live, so a reader on
// the board, in a room or looking at a settled conversation pays nothing. On top
// of that sits the stamp every trace read already carries: a recorder that has
// not grown costs one open and one stat.
func (a *App) readCardTracesCmd() tea.Cmd {
	if a.view != nil || a.page != pageThread {
		return nil
	}
	reader, ok := a.commander.(Trace)
	if !ok || a.source == nil {
		return nil
	}
	roots := a.liveCardJobs()
	if len(roots) == 0 {
		return nil
	}
	nodes := make([]string, 0, traceMaxNodes)
	for _, root := range roots {
		for _, id := range a.traceNodes(root) {
			nodes = append(nodes, id)
			if len(nodes) == traceMaxNodes {
				break
			}
		}
		if len(nodes) == traceMaxNodes {
			break
		}
	}
	if len(nodes) == 0 {
		return nil
	}
	since := make(map[string]nodeTrace, len(nodes))
	for _, id := range nodes {
		if held, seen := a.traces[id]; seen {
			since[id] = held
		}
	}
	return func() tea.Msg {
		out := make(map[string]nodeTrace, len(nodes))
		moved := false
		for _, id := range nodes {
			held := since[id]
			text, size, mod, fresh := reader.NodeTraceTail(id, traceMaxBytes, held.size, held.mod)
			if !fresh {
				continue
			}
			next := nodeTrace{text: text, size: size, mod: mod}
			if next == held {
				continue
			}
			out[id] = next
			moved = true
		}
		return traceReadMsg{traces: out, moved: moved, cards: true}
	}
}

// liveCardJobs is which jobs the conversation is currently watching: the roots
// of the job cards in the transcript's tail that are still running.
//
// It walks the TAIL for [App.threadIsLive]'s reason and with its window: a job
// block evolves in place at the position of its ask, and a thread with a
// thousand settled rows must not pay for them on a cycle.
func (a *App) liveCardJobs() []string {
	if a.transcript == nil {
		return nil
	}
	lo := a.transcript.Len() - liveTailWindow
	if lo < 0 {
		lo = 0
	}
	var out []string
	seen := make(map[string]bool, 4)
	for i := lo; i < a.transcript.Len(); i++ {
		block, ok := a.transcript.Block(i).(*messageBlock)
		if !ok || block.card == dressNone || !block.working || block.job == "" {
			continue
		}
		if seen[block.job] || !block.partsAlive() {
			continue
		}
		seen[block.job] = true
		out = append(out, block.job)
	}
	return out
}

// -- the parse -----------------------------------------------------------------

// traceKind is what one line of a recorder turned out to be.
//
// There are four, and they are 5.5's four VOICES rather than the recorder's
// five shapes: the model thinking, a tool being reached for, the reader
// steering, and the recorder's own free-form note. The fifth shape — the
// `── turn N ──` rule — is not a voice and no longer a row. It is a BOUNDARY,
// and §15 is unambiguous about what a boundary is drawn as: "blank lines are
// boundaries; adjacency is belonging". A header that spelled the number would
// be a label doing spacing's job, and "turn" is machinery vocabulary (§14).
type traceKind uint8

const (
	// traceThought is what the model said between calls.
	traceThought traceKind = iota
	// traceCall is one tool call, carrying its own result once that arrives.
	traceCall
	// traceSteer is a sentence the reader aimed at this worker mid-run.
	traceSteer
	// traceNote is a run-level fact the recorder wrote free-form.
	traceNote
	// traceMachine is a run of MACHINE-AUTHORED lines: an event feed, a dumped
	// log, anything whose shape says no person composed it. It is one row with a
	// bounded box behind it and never prose — see [machineShaped].
	traceMachine
)

// traceEvent is one drawable row of a recorder.
type traceEvent struct {
	kind traceKind
	// breaks marks the first row of a new round. The recorder writes the
	// boundary as a rule with a number in it; the room draws it as the one
	// blank line between two adjacent runs of rows, which is the whole of §5's
	// "a blank line IS the turn boundary".
	breaks bool
	// tool is the call's own name, and family is what [toolFamily] made of it.
	// The name is NOT on the collapsed row of a call whose family is known —
	// the glyph says that, and the row's words are what the call was FOR.
	tool   string
	family toolFamily
	// gist is the one line the collapsed row shows: the call's salient INPUT,
	// the first line of a thought, the reader's own sentence. For a call it is
	// never JSON — see [callInput].
	gist string
	// detail is what the fold holds for a voice that SPOKE: the rest of a
	// thought or a steer. A call has none — its fold holds what came back and
	// nothing else, because the collapsed row already names what went in.
	detail string
	// output is what a call RETURNED, drawn dim behind the `│` gutter when the
	// row is open, bounded to a box of [traceBoxRows] and continued behind one
	// more door.
	output string
	// bytes is the result's size as the recorder measured it, and zero means it
	// did not say. It is a number rather than a cell because a batch sums it.
	bytes int
	// reason is the short why of a failure, which is the only receipt a row
	// keeps now that success is silent.
	reason string
	// returned marks a call whose result has arrived; failed marks one that
	// arrived as an error. SUCCESS IS THE DEFAULT AND WEARS NOTHING: a ✓ on
	// every row of a nine-call run is noise that buries the one row that is
	// not fine.
	returned, failed bool
}

// parseTrace turns one recorder's tail into rows.
//
// The grammar is internal/exec/trace.go's, read straight off its five Fprintf
// calls, and the reason it can be line-based at all is that writer's `snip`:
// every record has its newlines replaced by ⏎ before it is written, so one
// record is one line by construction and no state machine is needed to find the
// end of one.
//
// The turn rule is READ AND SPENT rather than drawn: it arms a boundary, the
// next row of any kind takes it, and what reaches the screen is one blank line.
// Its numbers do not survive the crossing — see [traceBlock] — and its
// bracketed note does, as the note it always was.
func parseTrace(text string) []traceEvent {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var out []traceEvent
	// pending is a boundary the recorder has spelled and no row has spent yet.
	// It is never spent on the FIRST row: a document that opens on a boundary
	// would open on a blank line.
	pending := false
	push := func(event traceEvent) {
		event.breaks = pending && len(out) > 0
		pending = false
		out = append(out, event)
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, traceRulePrefix):
			pending = true
			// `finish=`, `in=`, `out=` and `cached=` end here. They are the
			// round's machinery telemetry, §5 refuses token counts on rows, and
			// with the header gone there is no row that owns them. What the
			// rule can carry that a reader acts on is the recorder's own note.
			body := strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(line, traceRulePrefix)),
				strings.Repeat(tokens.GlyphTreeDash, 2))
			if note := bracketed(body); note != "" {
				push(traceEvent{kind: traceNote, gist: note})
			}
		case strings.HasPrefix(line, "text: "):
			gist, detail := splitGist(unsnip(strings.TrimPrefix(line, "text: ")), traceGistCap)
			push(traceEvent{kind: traceThought, gist: gist, detail: detail})
		case strings.HasPrefix(line, "call "):
			push(parseCall(strings.TrimPrefix(line, "call ")))
		case strings.HasPrefix(line, "  → "):
			// A result belongs to the call above it. A result with no call above
			// it is a tail cut mid-round, and it becomes a row of its own rather
			// than being dropped — the reader is looking at the middle of a
			// document and should be told so by what is on screen.
			if n := len(out); n > 0 && out[n-1].kind == traceCall {
				absorbResult(&out[n-1], strings.TrimPrefix(line, "  → "))
				continue
			}
			push(traceEvent{kind: traceNote, gist: unsnip(strings.TrimPrefix(line, "  → "))})
		case strings.HasPrefix(line, "steered: "):
			gist, detail := splitGist(unsnip(strings.TrimPrefix(line, "steered: ")), traceGistCap)
			push(traceEvent{kind: traceSteer, gist: gist, detail: detail})
		default:
			// The recorder's own free-form notes — the contract in force, a
			// stop, a nudge. An unrecognised line lands here too, ON PURPOSE:
			// see this file's header.
			//
			// A note is ONE LINE. `unsnip` puts the writer's newlines back, and
			// a header title holding three of them would draw three rows out of
			// a cell that measured one; what a note has beyond its first line
			// goes behind its own fold like every other voice's does.
			gist, detail := splitGist(unsnip(line), traceGistCap)
			push(traceEvent{kind: traceNote, gist: gist, detail: detail})
		}
	}
	out = clampStreams(out)
	if len(out) > traceMaxEvents {
		out = out[len(out)-traceMaxEvents:]
	}
	return out
}

// clampStreams folds runs of machine-authored notes into one bounded row each.
//
// THIS IS THE RENDERER'S OWN DEFENCE AND IT IS DELIBERATELY NOT A FIX FOR ONE
// PRODUCER. The reported failure was a coding subharness `note`ing its child's
// NDJSON straight into the recorder, and that seam is closed where it belongs
// (internal/exec/swe.go's consume, which now spills the raw feed to a sidecar).
// But the recorder is an APPEND-ONLY FILE ANY WORKER CAN WRITE TO, this reader
// draws whatever it cannot parse rather than dropping it (see this file's
// header, third bullet), and every recorder already on this disk still holds
// the old shape. A room must be unable to draw a machine stream as content no
// matter who wrote it or when, so the clamp is here as well: §5's bounded box
// is a law about OUTPUT, and a run of unreadable lines is output whatever door
// it arrived through.
//
// Two runs qualify and they are the same row. A run of JSON-shaped lines is
// machine-authored by its shape, at ANY length — one such line is already not a
// sentence. A run of ordinary notes qualifies at [traceDumpRun], because past
// that many consecutive free-form lines with no voice between them, a reader is
// being shown a log rather than a record.
func clampStreams(events []traceEvent) []traceEvent {
	// The common case first, and it must cost one pass and no allocation: an
	// ordinary recorder has a handful of notes in it and nothing to fold.
	if !needsClamp(events) {
		return events
	}
	out := make([]traceEvent, 0, len(events))
	for i := 0; i < len(events); {
		if events[i].kind != traceNote {
			out = append(out, events[i])
			i++
			continue
		}
		end, seen := i, 0
		for end < len(events) && events[end].kind == traceNote {
			if machineShaped(events[end].gist) {
				seen++
			}
			end++
		}
		if seen == 0 && end-i < traceDumpRun {
			out = append(out, events[i:end]...)
			i = end
			continue
		}
		out = append(out, streamEvent(events[i:end], seen > 0))
		i = end
	}
	return out
}

// needsClamp is the cheap question asked before anything is rebuilt: does this
// document hold a machine run at all.
func needsClamp(events []traceEvent) bool {
	run := 0
	for i := range events {
		if events[i].kind != traceNote {
			run = 0
			continue
		}
		if machineShaped(events[i].gist) {
			return true
		}
		run++
		if run >= traceDumpRun {
			return true
		}
	}
	return false
}

// streamEvent is the one row a run of machine lines becomes: a title that names
// what it is, the whole run behind the bounded box, and the size as the token
// estimate every other result on this surface wears.
func streamEvent(run []traceEvent, machine bool) traceEvent {
	body := make([]string, 0, len(run))
	bytes := 0
	for i := range run {
		line := run[i].gist
		if run[i].detail != "" {
			line = join2(line, run[i].detail)
		}
		bytes += len(line) + 1
		body = append(body, line)
	}
	word := traceDumpWord
	if machine {
		word = traceStreamWord
	}
	return traceEvent{
		kind:   traceMachine,
		breaks: run[0].breaks,
		gist:   word,
		output: strings.Join(body, "\n"),
		bytes:  bytes,
	}
}

// machineShaped says a line was composed by a program and not by a person.
//
// The test is the honest one and not a heuristic about length: a whole JSON
// value on a line of its own. That is exactly what an event feed writes
// (`{"id":"evt_…","type":"message.part.delta",…}`) and exactly what no voice in
// §5 writes — a model's sentence, a shell command, a path and a reader's steer
// are none of them a JSON document. The parse is bounded by the tail this
// reader keeps (traceMaxBytes), so the cost is one validation pass over at most
// 32KB, on the only cycle where the tail actually changed.
func machineShaped(line string) bool {
	line = strings.TrimSpace(line)
	if len(line) < 2 {
		return false
	}
	switch line[0] {
	case '{', '[':
	default:
		return false
	}
	return json.Valid([]byte(line))
}

const (
	// traceDumpRun is how many consecutive free-form lines stop being notes and
	// start being a log. Eight is more than any recorder's own preamble
	// (`workspace:`, `engine:`, a contract line) and fewer than any dump.
	traceDumpRun = 8
	// traceStreamWord names a machine feed in the two plain lowercase words the
	// recorder itself uses for it (internal/exec/trace.go's streamNote).
	traceStreamWord = "engine stream"
	// traceDumpWord names a long run of lines that is not JSON but is not
	// speech either.
	traceDumpWord = "output"
)

// bracketed pulls the recorder's note out of `[…]`.
func bracketed(body string) string {
	open := strings.LastIndexByte(body, '[')
	if open < 0 {
		return ""
	}
	shut := strings.IndexByte(body[open:], ']')
	if shut < 0 {
		return ""
	}
	return strings.TrimSpace(body[open+1 : open+shut])
}

// parseCall reads `call web {"q":"Bitcoin price today live"}`.
//
// THE ROW NEVER SHOWS JSON. A call is drawn as what it was FOR — a command, a
// query, a path, a place on the web — and the whole argument object is not a
// fact a reader can do anything with. The measured failure this replaces is on
// screen in the reporter's own screenshot: a row titled with the query, then
// `web {"q":"…"}` under it, then `search: …` under that — three spellings of
// one fact, two of them the transport.
//
// The one input is found through [toolFamilies], a table of FAMILIES and not of
// tools: one row per kind of thing a tool does, matched on the words in the
// tool's own name, with the argument names that kind's subject goes by. A tool
// nobody has written yet lands in a family if it is named like one, falls back
// to the generic argument names if it is not, and shows its own name alone if
// even those say nothing — never `{…}`.
func parseCall(rest string) traceEvent {
	name, args, _ := strings.Cut(rest, " ")
	event := traceEvent{kind: traceCall, tool: strings.TrimSpace(name)}
	if event.tool == "" {
		event.tool = "call"
	}
	event.family = familyOf(event.tool)
	args = strings.TrimSpace(args)
	if args == "" {
		return event
	}
	event.gist, _ = splitGist(callInput(event.family, args), traceGistCap)
	return event
}

// toolKind is what a family of tools DOES, which is what its glyph says.
type toolKind uint8

const (
	// kindUnknown is a tool this reader cannot name. It wears the fold mark
	// alone and says its own name, which is honestly all that is known.
	kindUnknown toolKind = iota
	kindShell
	kindWeb
	kindFile
)

// toolFamily is one row of the table: what a kind of tool is called, what its
// subject is called, and the word a RUN of them says.
type toolFamily struct {
	// words are the name-tokens that put a tool in this family. They are
	// matched as whole tokens of the tool's own name split on `_-.:` — never as
	// substrings, or `publish` would be a shell.
	words []string
	kind  toolKind
	// keys are the argument names this family's subject goes by, most specific
	// first, ahead of the generic list.
	keys []string
	// verb is what a batch of these calls says it did: `searched 9`. It is a
	// plain past-tense word a person would use, never the tool's own name.
	verb string
	// place says the input is a URL and should be drawn as the place it names
	// rather than as the whole address.
	place bool
}

// toolFamilies is the salient-input table, and it is deliberately SHORT and
// EXTENSIBLE: one row per kind of work, first match wins. It is the one place a
// new family is added, and adding one is a row here and nothing else.
var toolFamilies = []toolFamily{
	{words: []string{"sh", "bash", "zsh", "shell", "exec", "run", "terminal", "cmd", "command"},
		kind: kindShell, keys: []string{"cmd", "command", "script"}, verb: "ran"},
	{words: []string{"fetch", "browse", "open", "curl", "get", "http", "url", "visit"},
		kind: kindWeb, keys: []string{"url", "urls"}, verb: "fetched", place: true},
	{words: []string{"web", "search", "exa", "google", "grep", "find", "lookup"},
		kind: kindWeb, keys: []string{"q", "query", "pattern", "term"}, verb: "searched"},
	{words: []string{"write", "edit", "read", "file", "patch", "create", "append", "cat", "view"},
		kind: kindFile, keys: []string{"path", "file", "filename", "target"}, verb: "wrote"},
}

// familyOf puts one tool in its family, by the WORDS in its name.
func familyOf(tool string) toolFamily {
	name := strings.ToLower(strings.TrimSpace(tool))
	for _, family := range toolFamilies {
		for _, token := range strings.FieldsFunc(name, isNameBreak) {
			for _, word := range family.words {
				if token == word {
					return family
				}
			}
		}
	}
	return toolFamily{kind: kindUnknown, verb: "called"}
}

// isNameBreak splits a tool's name into the words it is made of, which is how
// `web_search`, `web-search` and `mcp.web.search` are one family and `publish`
// is not a shell.
func isNameBreak(r rune) bool {
	return r == '_' || r == '-' || r == '.' || r == ':' || r == ' ' || r == '/'
}

// salientKeys are the argument names a call's SUBJECT goes by when its family's
// own names do not answer, most specific first. Every one of them is a name some
// tool in this codebase already uses.
var salientKeys = []string{"cmd", "q", "query", "path", "file", "prompt", "url", "urls", "pattern", "text"}

// callInput is the ONE thing the row says a call was for: the family's own
// argument first, then the generic names, and an empty string when neither
// answers — never the object.
func callInput(family toolFamily, args string) string {
	key, value := scanArgs(args, family.keys)
	if value == "" {
		key, value = salientArg(args)
	}
	if value == "" {
		return ""
	}
	if family.place || key == "url" || key == "urls" {
		return trimScheme(value)
	}
	return value
}

// trimScheme draws a URL as the place it names — `docs.rs/tokio/latest` — since
// the scheme is the same six characters on every row and the query string is a
// tracking parameter more often than it is information.
func trimScheme(url string) string {
	for _, scheme := range []string{"https://", "http://"} {
		url = strings.TrimPrefix(url, scheme)
	}
	if cut := strings.IndexAny(url, "?#"); cut > 0 {
		url = url[:cut]
	}
	return strings.TrimSuffix(url, "/")
}

// salientArg finds the one argument worth putting on the row, and reports the
// KEY it was found under.
//
// The key is not a byproduct: it is what the row's kind glyph is read off
// ([callGlyph]), which is how this surface names `$`, `⌕` and `✎` without ever
// holding a list of tool names. A call that says `cmd` is a shell call whatever
// the tool is called this month.
//
// TWO PASSES, AND THE SECOND IS NOT A FALLBACK SO MUCH AS THE COMMON CASE.
// internal/exec/trace.go's `snip` truncates an argument object at 300
// characters, so ANY call whose arguments are longer than a tweet reaches this
// function as INVALID JSON with its closing brace cut off — a heredoc handed to
// `sh`, a file handed to `write`. Measured on the reporter's own recorder: the
// strict parse fails on exactly the calls a reader most wants named, and the row
// then falls back to drawing raw `{"cmd":"cd … << 'EOF'\n\nprint(\"…` at them.
// The scan reads the one key it wants and stops, so a cut object answers as well
// as a whole one.
func salientArg(args string) (key, value string) { return scanArgs(args, salientKeys) }

// scanArgs is [salientArg] aimed at one family's own argument names.
func scanArgs(args string, keys []string) (key, value string) {
	if len(keys) == 0 {
		return "", ""
	}
	restored := strings.ReplaceAll(args, traceEnterMark, "\\n")
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(restored), &object); err == nil {
		for _, name := range keys {
			raw, ok := object[name]
			if !ok {
				continue
			}
			var one string
			if err := json.Unmarshal(raw, &one); err == nil {
				return name, strings.TrimSpace(one)
			}
			var many []string
			if err := json.Unmarshal(raw, &many); err == nil && len(many) > 0 {
				return name, strings.Join(many, "  ")
			}
		}
		return "", ""
	}
	return salientScan(restored, keys)
}

// salientScan reads one string-valued key out of an object that may end
// mid-token. It is deliberately not a JSON parser: it looks for the key it
// already wants, walks its value honouring escapes, and stops at the closing
// quote or at the end of what survived the truncation.
func salientScan(args string, keys []string) (key, value string) {
	for _, name := range keys {
		needle := `"` + name + `":`
		at := strings.Index(args, needle)
		if at < 0 {
			continue
		}
		rest := strings.TrimLeft(args[at+len(needle):], " ")
		if !strings.HasPrefix(rest, `"`) {
			continue
		}
		return name, strings.TrimSpace(unquoteRun(rest[1:]))
	}
	return "", ""
}

// unquoteRun walks a JSON string body to its closing quote, or to the end of a
// value the recorder cut short.
func unquoteRun(rest string) string {
	var out strings.Builder
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case '"':
			return out.String()
		case '\\':
			if i+1 >= len(rest) {
				return out.String()
			}
			switch rest[i+1] {
			case 'n':
				out.WriteByte('\n')
			case 't':
				out.WriteByte('\t')
			case 'r':
				out.WriteByte('\r')
			default:
				out.WriteByte(rest[i+1])
			}
			i++
		default:
			out.WriteByte(rest[i])
		}
	}
	return out.String()
}

// absorbResult folds `  → 1652B: search: …` onto the call above it.
//
// The SIZE goes on the right of the header as telemetry and the content goes
// under the fold, which is the whole difference between this row and v1's:
// there, a result was three lines of body under every call and a five-call round
// filled a screen with output nobody had asked to read. Here it is one cell and
// a `▸`.
//
// A FAILURE ALSO KEEPS ITS REASON. Success is silent now, so the failure mark
// is the only receipt of state left on a row, and a mark with no reason next to
// it makes the reader open the row to learn a sentence that fits beside it.
func absorbResult(event *traceEvent, rest string) {
	head, content, found := strings.Cut(rest, ": ")
	if !found {
		head, content = rest, ""
	}
	event.returned = true
	if strings.HasSuffix(head, " ERROR") {
		event.failed = true
		head = strings.TrimSuffix(head, " ERROR")
	}
	if bytes := strings.TrimSuffix(strings.TrimSpace(head), "B"); bytes != "" {
		if n, err := strconv.Atoi(bytes); err == nil {
			event.bytes += n
		}
	}
	if content = strings.TrimSpace(unsnip(content)); content != "" {
		event.output = join2(event.output, content)
		if event.failed && event.reason == "" {
			event.reason = clipCell(firstLine(content), traceReasonCap)
		}
	}
}

// clipCell cuts a cell to a width it can wear, on §16's one ellipsis grammar.
func clipCell(text string, limit int) string {
	if limit <= 0 || len(text) <= limit {
		return text
	}
	if cut := strings.LastIndexByte(text[:limit], ' '); cut > limit/2 {
		return strings.TrimSpace(text[:cut]) + tokens.GlyphEllipsis
	}
	return strings.TrimSpace(text[:limit]) + tokens.GlyphEllipsis
}

// resultCost is what a call's result cost the model to read, in the only unit
// that means anything to a person deciding whether a run is worth what it is
// doing: `~1.5k tok`.
//
// BYTES WERE THE WRONG UNIT. `47KB` is a fact about a file and not about this
// work — nobody has an intuition for what 47KB of search results does to a
// context window, and the number a reader is actually trying to form is "how
// much of the window did that just eat". Four bytes to the token is the
// industry rule of thumb, and it is close enough to carry the judgement it is
// there to support.
//
// THE `~` IS THE WHOLE OF ITS HONESTY. 8.2.20 bans invented numbers, and an
// estimate presented as a measurement is exactly that; an estimate MARKED as
// one is not, because the reader is told what they are holding. That is the
// sanctioned exception and it is narrow: the tilde is not decoration and may
// not be dropped to save a cell.
//
// AND THERE IS NO DOLLAR FIGURE HERE, deliberately. A round's calls share one
// bill — the model is charged for the turn, not per tool — so a per-call
// dollar cell would have to invent a split. The record's own header carries the
// money for the whole job, where it is a measurement.
func resultCost(bytes int) string {
	if bytes <= 0 {
		return ""
	}
	return tokenMark + tokens.Count(int64(bytes)/bytesPerToken) + " tok"
}

const (
	// tokenMark says the figure beside it is an estimate and not a reading.
	tokenMark = "~"
	// bytesPerToken is the rule of thumb the estimate rests on.
	bytesPerToken = 4
)

// unsnip puts the writer's line breaks back. internal/exec/trace.go replaces
// every newline with ⏎ so one record is one line; a renderer that left them as
// ⏎ would be showing the reader the transport.
func unsnip(text string) string {
	return strings.TrimSpace(strings.ReplaceAll(text, traceEnterMark, "\n"))
}

// join2 concatenates two folded runs, keeping the blank line between them that
// makes them read as two things.
func join2(a, b string) string {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + "\n\n" + b
	}
}

// -- the blocks ----------------------------------------------------------------

// traceBlockID is one execution row's identity. It is keyed by NODE and by
// position, which is what makes 13.16's fold law hold here: the room rebuilds
// its whole block list on every move, so a fold flag living on the block would
// be thrown away several times a second, and [App.folds] survives only because
// the id does.
//
// Position is counted from the START of the parsed tail, which is stable for as
// long as the recorder fits inside traceMaxBytes — which is every trace this
// profile has ever written. A run long enough to overflow it re-keys its rows
// once, and the cost of that is that folds a reader opened near the top come
// back shut. That is a real, bounded edge and it is recorded rather than papered
// over: keying by turn number instead would trade it for a worse one, because a
// turn number is not unique across a tail that begins mid-document.
func traceBlockID(node string, index int) string {
	return traceBlockPrefix + node + "-" + strconv.Itoa(index)
}

// traceBlockPrefix is how the record recognizes its own execution rows when it
// decides where the seam goes (record.go). It is the id's prefix rather than a
// second flag, because the id is already the thing that must be stable.
const traceBlockPrefix = "room-trace-"

// isTraceBlock reports that a block is one of this file's execution rows.
func isTraceBlock(block blocks.Block) bool {
	return block != nil && isTraceBlockID(block.ID())
}

// isTraceBlockID is the same question asked of an id alone, which is what the
// fold door has in hand (disclose.go).
func isTraceBlockID(id string) bool { return strings.HasPrefix(id, traceBlockPrefix) }

// traceBlocks draws one node's recorder as transcript rows.
//
// ONE ROW PER EVENT, and the rows of one round are ADJACENT LINES. That is the
// whole shape change of this pass, and it is §5 and §15 read together: the
// record used to open every event with a two-line block and close it with a
// blank, so a five-call round drew fifteen lines with six holes in it and then
// wore a `turn 3 · 58 tok` header to explain the rhythm the spacing had
// destroyed. Delete the header and fix the spacing, in that order — the rhythm
// then says what the label was spelling, which is §15's own test.
//
// The blank line is placed by [traceEvent.breaks] and nothing else: a row is
// TIGHT (no trailing blank) unless the row after it opens a new round, and the
// last row of a recorder is never tight, so the next thing in the record — the
// next part, the next recorder's rows spliced in behind it, a journaled message
// — is separated by exactly one blank line as well. A splice boundary is a
// boundary like any other, which is precisely why it needs no special case.
// A RUN OF THE SAME TOOL IS ONE ROW. This is the second shape change, and it
// comes from the reporter's own screenshot: nine consecutive searches drew nine
// near-identical rows and drowned the two sentences of narration between them,
// which are the thing a person opens a record to read. Consecutive calls of the
// same tool — with nothing said between them — collapse into
// `⌕ searched 9 · first · second · third +6 · 47KB ▸`, and opening it lays out
// the calls themselves, each of which opens onto what it returned.
//
// The rows that are NOT batched are the ones that were always the point: a
// thought, a steer. They end up the loudest thing in the record by arithmetic
// rather than by emphasis.
//
// open reports whether a row the reader has opened is open, and it is what makes
// this a rebuild rather than a re-render: which rows EXIST depends on it, so
// [App.toggleFold] repaints the room instead of only re-rendering one block. A
// nil predicate means everything is shut, which is what a test that renders the
// record without an app gets.
func traceBlocks(node string, events []traceEvent, style *tokens.Styler,
	open func(id string) bool, live traceLive) []blocks.Block {

	if len(events) == 0 {
		return nil
	}
	if open == nil {
		open = func(string) bool { return false }
	}
	out := make([]blocks.Block, 0, len(events)+2)
	for i := 0; i < len(events); {
		run := runLength(events, i)
		// THE TAIL OF A RECORDER MAY STILL BE HAPPENING. Calls whose result has
		// not been written yet, at the very end of the file, are calls that are
		// running right now — they are peeled off the run and drawn moving
		// ([runningBlock]), and what came before them is drawn as the quiet
		// record it is.
		if pending := runningTail(events, i, run); pending > 0 {
			if settled := run - pending; settled > 0 {
				out = append(out, traceRun(node, events, i, settled, false, style, open)...)
			}
			out = append(out, newRunningBlock(traceBlockID(node, i+run-pending),
				events[i+run-pending:i+run], style, live))
			i += run
			continue
		}
		// tight is decided by the event AFTER the run, so a batch that spans
		// three rounds is one row with no holes in it and the boundary that
		// matters — the one back to a voice that speaks — still lands.
		tight := i+run < len(events) && !events[i+run].breaks && !opensCluster(events, i, i+run)
		out = append(out, traceRun(node, events, i, run, tight, style, open)...)
		i += run
	}
	return out
}

// opensCluster says the event at next begins a new narration cluster and must
// therefore be preceded by air.
//
// §5's rhythm, in the shape the reference reads at: NARRATION, its tool rows
// TIGHT BENEATH IT, then a blank line, then the next narration. The recorder's
// own `── turn N ──` rule is not enough on its own, because one round routinely
// alternates — a sentence, three calls, another sentence, two more calls — and
// without this the second sentence would butt against the last tool row and the
// whole round would read as one undifferentiated block. A voice that SPEAKS
// after machinery has run is starting something; machinery that runs after a
// voice spoke belongs to what was said.
func opensCluster(events []traceEvent, at, next int) bool {
	if events[at].kind != traceCall {
		return false
	}
	switch events[next].kind {
	case traceThought, traceSteer:
		return true
	}
	return false
}

// traceRun draws one settled run: a single row, or the batch a run of same-tool
// calls collapses into.
func traceRun(node string, events []traceEvent, i, run int, tight bool,
	style *tokens.Styler, open func(id string) bool) []blocks.Block {

	if run == 1 {
		return callRowBlocks(node, i, events[i], tight, 0, style, open)
	}
	out := make([]blocks.Block, 0, run+1)
	{
		batchID := traceBlockID(node, i)
		batch := batchBlock(batchID, events[i:i+run], style)
		if !open(batchID) {
			batch.tight = tight
			return append(out, batch)
		}
		// An open batch is ADJACENT to what it opened onto: the calls belong to
		// it, and a blank line between a row and its own contents would say they
		// were two things (§15).
		batch.expanded, batch.tight = true, true
		batch.head.Hint = blocks.Disclose(true, batch.hidden, "line", "lines")
		out = append(out, batch)
		for n := 0; n < run; n++ {
			last := n == run-1
			inner := tight || !last
			out = append(out, callRowBlocks(node, i+n, events[i+n], inner, bodyIndent, style, open)...)
		}
	}
	return out
}

// runningTail is how many calls at the end of a run are STILL IN FLIGHT.
//
// It is only ever asked of a run that reaches the end of the parsed tail, and
// that bound is the whole of the honesty here. internal/exec/trace.go writes a
// call line when the call is issued and its `  → ` result line when the result
// comes back, so an unreturned call in the MIDDLE of a document is not a call
// that is running — it is a call whose result was lost to a crash or to the
// 32KB window this reader keeps. Drawing that one with a spinner would be the
// surface claiming liveness it cannot see (8.1.6, 8.2.20), which is the one
// mistake a moving glyph can make.
//
// More than one can be in flight at once, and that is not a special case: a
// model that issues three tool calls in one turn has all three written before
// any result is, so the tail is `call call call` and the room is watching three
// things happen.
func runningTail(events []traceEvent, i, run int) int {
	if i+run != len(events) || events[i].kind != traceCall {
		return 0
	}
	pending := 0
	for n := i + run - 1; n >= i && !events[n].returned; n-- {
		pending++
	}
	return pending
}

// runLength is how many events starting at i are one batchable run: consecutive
// calls of the SAME tool with nothing said between them. A round boundary does
// not end a run — nine searches over nine rounds are still nine searches — but
// a thought, a steer or a recorder's note does, because those are what the run
// is being separated FROM.
func runLength(events []traceEvent, i int) int {
	if events[i].kind != traceCall {
		return 1
	}
	n := 1
	for i+n < len(events) &&
		events[i+n].kind == traceCall &&
		events[i+n].tool == events[i].tool {
		n++
	}
	return n
}

// callRowBlocks is one event's row plus, when the row is open and what came back
// is longer than the box, the continuation that holds the rest.
//
// THE OUTPUT BOX IS BOUNDED (§5's collapsed-row law, applied to the opened
// state). An opened result shows [traceBoxRows] lines behind the gutter and no
// more; the remainder lives behind one further door, at the same gutter, and is
// whole when it is opened. A record that dumped a 4,000-line result into itself
// because someone tapped a row would be the same failure 13.10 found one surface
// out, and "expand" would become a gesture nobody dares make.
func callRowBlocks(node string, index int, event traceEvent, tight bool, indent int,
	style *tokens.Styler, open func(id string) bool) []blocks.Block {

	id := traceBlockID(node, index)
	box, rest := splitBox(event.output)
	block := traceBlock(id, event, box, tight, indent, style)
	if rest == "" || !open(id) {
		return []blocks.Block{block}
	}
	block.expanded, block.tight = true, true
	block.head.Hint = blocks.Disclose(true, block.hidden, "line", "lines")

	more := &messageBlock{
		id: id + traceMoreSuffix, style: style, tight: tight, indent: indent + bodyIndent,
	}
	lines := strings.Count(rest, "\n") + 1
	// The gutter continues and the fold hint counts what is behind it — and
	// that is the whole row. A title saying "8 more lines" beside a hint saying
	// "▸ 8 lines" would be §15's own failure in four words: the label spelling
	// what the row already says.
	more.head = blocks.Header{
		Glyph: style.Glyph(tokens.GProseQuote),
		State: blocks.StateChrome,
	}
	more.collapsible, more.hidden = true, lines
	more.head.Hint = blocks.Disclose(open(more.id), lines, "line", "lines")
	more.expanded = open(more.id)
	// THE STEP IS TAKEN ONCE, BY THE BLOCK. `more` already sits at
	// `indent + bodyIndent` (line above), which is what puts its `│` in the
	// child's marker cells and its content at the child's edge. A segment indent
	// on top of that would take the SAME step a second time: measured at 70
	// columns, the continuation of a top-level row drew its bar at column 4 and
	// its bytes at column 6, one whole rung below the row it continues, and a
	// batch child's continuation landed at 6/8. §20's ladder is `En = 2 + 2n`
	// with no rung skipped, so the run hangs flush inside the block that already
	// placed it.
	more.segs = append(more.segs, segment{
		kind: segGutter, text: rest, tier: tokens.TextTertiary, folded: true,
	})
	return []blocks.Block{block, more}
}

// traceMoreSuffix keys the continuation onto the row it continues, so the
// reader's answer for one survives a rebuild exactly as the row's own does.
const traceMoreSuffix = "-more"

// splitBox cuts a result into the bounded box and the rest of the document.
// It cuts on SOURCE LINES because [gutterRows] draws one row per line — a
// result is preformatted, so the box is [traceBoxRows] rows by construction and
// not by hope.
func splitBox(output string) (box, rest string) {
	output = strings.TrimSpace(output)
	if output == "" {
		return "", ""
	}
	lines := strings.Split(output, "\n")
	if len(lines) <= traceBoxRows {
		return output, ""
	}
	return strings.Join(lines[:traceBoxRows], "\n"), strings.Join(lines[traceBoxRows:], "\n")
}

// batchBlock is the one row a run of same-tool calls collapses into.
//
//	⌕ searched 9 · rust async trait · tokio spawn cost · pin project +6   47KB  ▸ 9 calls
//
// The verb is the FAMILY's word and never the tool's name (§14: a tool name is
// machinery; "searched" is what happened). The count is information rather than
// a label, and §15 permits it precisely because the multiplicity it counts is
// NOT visible while the row is shut — the moment it opens, the count stops being
// the only witness and the rows themselves are.
//
// THE SIZE IS A COLUMN AND NOT A CELL. It rides [blocks.Header.Receipt], hard
// against the right edge, because §16's first rule is that receipts right-align
// to one shared column per surface — and because as a meta cell it was the
// FIRST thing the header shed: measured at 88 columns, a batch whose three
// named inputs ran to the edge lost its size entirely while the shorter row
// under it kept one, and a column with holes in it is not a column.
func batchBlock(id string, run []traceEvent, style *tokens.Styler) *messageBlock {
	block := &messageBlock{id: id, style: style}
	glyph, _ := callGlyph(run[0], style)
	// NO CYAN ON A SETTLED ROW (§18.3). Cyan means ALIVE, and a batch in a
	// journaled record is a batch that has finished; a hue spent on "this is a
	// door" would be the vocabulary paying for an affordance the fold chevron
	// and the hover promotion already carry. The glyph is dim machinery until
	// something in the run broke, and then it is coral.
	block.head = blocks.Header{
		Glyph: traceInk(style, glyph, tokens.TextTertiary),
		State: blocks.StateSettled,
	}

	shown := make([]string, 0, traceBatchShow+1)
	shown = append(shown, run[0].family.verb+" "+strconv.Itoa(len(run)))
	bytes, failed := 0, 0
	for i := range run {
		bytes += run[i].bytes
		if run[i].failed {
			failed++
		}
		if len(shown) <= traceBatchShow && strings.TrimSpace(run[i].gist) != "" {
			shown = append(shown, clipCell(run[i].gist, traceInputCap))
		}
	}
	if hidden := len(run) - (len(shown) - 1); hidden > 0 && len(shown) > 1 {
		shown[len(shown)-1] += " +" + strconv.Itoa(hidden)
	}
	// Secondary, like every other tool row: a batch is machinery no matter how
	// many calls it stands for.
	block.head.Title = traceInk(style,
		strings.Join(shown, " "+tokens.GlyphSeparator+" "), tokens.TextSecondary)
	if failed > 0 {
		// The one hue, and the one count that has to survive width pressure: a
		// run of nine that broke twice must never read as a run of nine.
		block.head.Glyph, block.head.GlyphHue = glyph, blocks.HueBroken
		badge := blocks.CountBadge(style.Glyph(tokens.GFailed), failed, blocks.HueBroken)
		badge.Sticky = true
		block.head.Badges = append(block.head.Badges, badge)
	}
	if bytes > 0 {
		block.head.Receipt = resultCost(bytes)
	}
	block.collapsible, block.hidden = true, len(run)
	block.head.Hint = blocks.Disclose(false, len(run), "call", "calls")
	return block
}

// traceBlock is one execution row: one line at rest, and what it holds under
// the `▸` 13.16 made a door.
//
// The four voices of §5, each with a fixed glyph and a fixed tier, and no row
// carries a word that says which voice it is — the glyph is the identification
// and the legend at the top of the record teaches it once ([seamBlock]):
//
//	✳ I'll gather live data first.
//	$ ls -la clips/                                                1KB  ▸ 3 lines
//	⌕ ffmpeg concat mp4                        [✕ exa 503: upstream]  902B  ▸ 2 lines
//	› focus on scene 10 only
//
// THE SIZE AND THE DOOR ARE ONE RIGHT-ALIGNED COLUMN (§16), which is what the
// wireframe above always drew and what [blocks.Header.Receipt] finally makes
// true: as a meta cell the size was the first thing the header shed under width
// pressure, so the column it belongs to had holes in it at exactly the widths a
// reader is most likely to be reading at.
//
// THE KIND GLYPH IS THE ACCENT AND THE INPUT IS THE INK. §12 gives the accent to
// what is clickable and the primary tier to content, and on a call row those are
// two different cells: the glyph is the door (every call row opens), and the
// input is the one thing the reader came to read. A thought is dim because it is
// the model talking to itself; a steer is bright because the reader's own words
// must be findable in a document they did not write.
//
// SUCCESS IS SILENT. The row used to wear `· ✓ ·` whenever a result came back,
// which put a mark on nearly every row in the record and left the one row that
// broke looking like all the others. Only a failure marks now, and it brings its
// reason with it — coral, sticky, on the right.
//
// TOKEN COUNTS ARE GONE FROM EVERY ROW. §5 says so outright, and there is no
// row left that owns them: they described a round, and a round is now a blank
// line.
func traceBlock(id string, event traceEvent, box string, tight bool, indent int,
	style *tokens.Styler) *messageBlock {

	block := &messageBlock{id: id, style: style, tight: tight, indent: indent}
	// Chrome is the dim tier, which is what a recorder's own free-form note is
	// drawn at; every voice that carries content asks for more than that.
	block.head = blocks.Header{Title: event.gist, State: blocks.StateChrome}
	detailTier := tokens.TextTertiary

	switch event.kind {
	case traceThought:
		// THE NARRATION IS THE LOUDEST THING IN THE RECORD, and until this it
		// was the quietest. A thought was drawn dim on the reasoning that the
		// model was talking to itself — but a record is opened to read exactly
		// that, and the machinery around it (a path, a query, a size) was a tier
		// brighter, so nine tool rows out-shouted the two sentences that
		// explained them. §5 says so outright: "the model's narration must read
		// as the loudest voice in the record; tools are the quiet machinery
		// under it." Primary ink here, secondary on the tool rows, dim on their
		// receipts — three tiers, in the order the reader needs them (§16).
		//
		// The ✳ stays dim. The glyph is the identification and not the content,
		// and a mark as bright as the sentence beside it competes with the words
		// for the eye it is supposed to be directing.
		block.head.Glyph = traceInk(style, style.Glyph(tokens.GThought), tokens.TextTertiary)
		block.head.State = blocks.StateSettled
		detailTier = tokens.TextPrimary

	case traceSteer:
		// The chat prompt, which is the mark the reader typed this at (5.11) —
		// the same glyph their own turns wear in the conversation, because this
		// IS one of their turns, aimed at a worker instead of at the room.
		block.head.Glyph = style.Glyph(tokens.GPromptChat)
		block.head.State = blocks.StateSettled
		detailTier = tokens.TextPrimary

	case traceMachine:
		// THE ROW THE BYPASS NEEDED. §5 already required tool output to sit in
		// a bounded box, and this content slipped past that law by never being
		// a tool's output: it arrived as N free-form notes, and a note is a
		// header TITLE, which no box ever bounded. Making it one event with an
		// `output` puts it back inside the law it was outside of — the box, the
		// gutter, the `▸ N lines` door and the `~ tok` receipt are all the
		// existing machinery, because the defect was never that the machinery
		// was missing.
		//
		// It is dim chrome and it wears the fold mark alone. Nobody said it,
		// nothing about it is content, and the one thing worth knowing at a
		// glance is that it is there and how much of it there is.
		block.head.Glyph = traceInk(style, style.Glyph(tokens.GCollapsed), tokens.TextTertiary)
		block.head.State = blocks.StateSettled
		block.head.Title = traceInk(style, event.gist, tokens.TextTertiary)
		if event.bytes > 0 {
			block.head.Receipt = resultCost(event.bytes)
		}

	case traceCall:
		glyph, named := callGlyph(event, style)
		// Dim machinery, not an accent: see [batchBlock] on why a settled tool
		// row may not wear the alive hue (§18.3).
		block.head.Glyph = traceInk(style, glyph, tokens.TextTertiary)
		block.head.State = blocks.StateSettled
		if !named || event.gist == "" {
			// A family this reader cannot name wears the fold mark alone, and
			// then the row owes the reader the tool's own name — the glyph is no
			// longer saying it. It still never says `{…}`.
			block.head.Title, block.head.Desc = event.tool, event.gist
		}
		// ONE TIER UNDER THE NARRATION. The salient input is content and gets
		// the middle grey; the glyph keeps its accent because it is the door;
		// the size and the fold hint are already chrome in the receipt column.
		// The gap between this tier and the sentence above it is the whole of
		// §5's "tools are the quiet machinery under it".
		block.head.Title = traceTitle(style, event, block.head.Title)
		if event.failed {
			block.head.Glyph, block.head.GlyphHue = glyph, blocks.HueBroken
			mark := style.Glyph(tokens.GFailed)
			if event.reason != "" {
				mark += " " + event.reason
			}
			block.head.Badges = append(block.head.Badges, blocks.Badge{
				Text: mark, State: blocks.StateSettled, Hue: blocks.HueBroken, Sticky: true,
			})
		}
		if event.bytes > 0 {
			block.head.Receipt = resultCost(event.bytes)
		}
	}

	detail := strings.TrimSpace(event.detail)
	if detail == "" && box == "" {
		return block
	}
	block.collapsible = true
	if detail != "" {
		block.hidden += strings.Count(detail, "\n") + 1
		block.segs = append(block.segs, segment{
			kind: segProse, text: detail, tier: detailTier,
			indent: bodyIndent, folded: true,
		})
	}
	if box != "" {
		// What came back is QUOTED and never re-flowed as prose: it is bytes a
		// program wrote, and a renderer that read a directory listing as
		// markdown would be inventing structure in somebody else's output.
		block.hidden += strings.Count(box, "\n") + 1
		block.segs = append(block.segs, segment{
			kind: segGutter, text: box, tier: tokens.TextTertiary,
			indent: bodyIndent, folded: true,
		})
	}
	block.head.Hint = blocks.Disclose(false, block.hidden, "line", "lines")
	return block
}

// -- the running row -----------------------------------------------------------

// traceLive is what a row that is STILL HAPPENING needs and a settled row does
// not: the one animation clock, and the instant its work started.
//
// It is a value passed down rather than read from the app, for the reason every
// other input to this file is: the record is rebuilt from a snapshot, and a
// renderer that reached for the current time itself would make two rows of one
// frame disagree about what "now" is.
type traceLive struct {
	// clock is the transcript's shared clock (8.1.3). Every moving glyph in a
	// frame derives from the same latched instant, so a room with three running
	// calls animates as one organism rather than three twitches. Nil means the
	// row is drawn standing still, which is what a headless test gets.
	clock *blocks.Clock
	// since is when the running work started, taken from the RECORDER'S OWN
	// mtime. That is not an approximation dressed as a fact: a call line is
	// written the moment the call is issued, so on a tail whose last line is an
	// unreturned call the file's last write IS that call's start. Zero means the
	// room could not read one, and then no clock is drawn — §16's absent cell,
	// never a guess.
	since time.Time
}

// runningBlock is the record's ONE MOVING ROW (§11's motion allowance).
//
//	⠸ npm test -- --run                                                    25s
//	⠸ running 3                                                            4s
//
// It exists because the record was silent about the only thing a reader watches
// a record FOR. A call whose result has not come back drew exactly like a call
// whose result had — same glyph, same tier, no clock — so the row a person was
// waiting on was the one row on screen with no way to tell it was still going,
// and the room looked frozen while the work was fine.
//
// It is the one block in this file that is NOT finalized, and that is the whole
// mechanism: [blocks.Transcript] rebuilds a live entry every frame and leaves
// every settled one alone, so the spinner costs one row per frame and never the
// document above it. When the result lands the recorder grows, the tail
// re-parses, and this row is replaced by the ordinary quiet one — no transition
// to animate, because the row simply stops being live.
//
// SHAPE NEVER MOVES, ONLY COLOUR AND THE CLOCK. The braille frames are
// width-homogeneous by construction ([blocks.Spinner]) and the elapsed cell
// rides the receipt column, so nothing to the right of a running row dances
// with it (5.17's width stability, §16's column).
type runningBlock struct {
	id     string
	style  *tokens.Styler
	live   traceLive
	glyph  string
	title  string
	tight  bool
	indent int

	rows []string
}

var _ blocks.Block = (*runningBlock)(nil)

func (b *runningBlock) ID() string { return b.id }

// IsFinalized is always false: this row is the live region of a record.
func (b *runningBlock) IsFinalized() bool { return false }

// SettledRows is zero. Nothing about a running row is promised to stay put.
func (b *runningBlock) SettledRows(int) int { return 0 }

// Version never moves. A live block is rebuilt because it is live, not because
// a counter said so (blocks/cache.go's refresh).
func (b *runningBlock) Version() uint64 { return 0 }

func (b *runningBlock) End() blocks.EndState { return blocks.EndCompleted }

// Rows draws the row at this frame's instant.
func (b *runningBlock) Rows(width int) []string {
	if width < 1 {
		width = 1
	}
	pad, inner := "", width
	if b.indent > 0 && b.indent < width {
		pad, inner = strings.Repeat(" ", b.indent), width-b.indent
	}
	head := blocks.Header{
		Glyph:    b.frame(),
		GlyphHue: blocks.HueAlive,
		Title:    b.title,
		State:    blocks.StateSettled,
		Receipt:  b.elapsed(),
	}
	b.rows = append(b.rows[:0], pad+head.Render(inner, b.styler()))
	if !b.tight {
		b.rows = append(b.rows, "")
	}
	return b.rows
}

// frame is the spinner cell, or the family's own glyph standing still.
//
// CALM WINS. 10.1.5's linear mode and 5.13's reduced-motion both say the same
// thing — the surface stops moving and says what it was saying without motion —
// and the elapsed cell beside it is what keeps the row honest when it does: a
// still glyph with a number that changes is still a row visibly alive.
func (b *runningBlock) frame() string {
	if b.live.clock == nil || b.live.clock.Calm {
		return b.glyph
	}
	return b.live.clock.Glyph()
}

// elapsed is how long the running work has been at it, or nothing.
func (b *runningBlock) elapsed() string {
	if b.live.clock == nil || b.live.since.IsZero() {
		return ""
	}
	since := b.live.clock.Now().Sub(b.live.since)
	if since < 0 {
		// A recorder written by a clock ahead of ours. Absent beats negative.
		return ""
	}
	return tokens.Elapsed(since)
}

func (b *runningBlock) styler() blocks.Styler {
	if b.style == nil {
		return blocks.Plain
	}
	return b.style
}

// newRunningBlock dresses the calls at the tail of a recorder that have not
// come back yet.
//
// ONE ROW FOR THE WHOLE TAIL, counted rather than listed, and that is §15's
// count rule read the same way [batchBlock] reads it: the multiplicity is not
// visible while the calls are in flight — none of them has said anything yet —
// so the number is the only witness there is. A single call keeps its own words
// instead, because there the input IS the information.
func newRunningBlock(id string, run []traceEvent, style *tokens.Styler, live traceLive) *runningBlock {
	block := &runningBlock{id: id, style: style, live: live}
	block.glyph, _ = callGlyph(run[0], style)
	title := runningTitle(run)
	block.title = traceInk(style, title, tokens.TextSecondary)
	return block
}

// runningTitle is what a running row says it is doing.
func runningTitle(run []traceEvent) string {
	if len(run) > 1 {
		return runningWord + strconv.Itoa(len(run))
	}
	if gist := strings.TrimSpace(run[0].gist); gist != "" {
		return gist
	}
	return run[0].tool
}

// runningWord is how a record says work is in flight, ONE spelling. It is a
// plain present-tense word and never a tool name or a count of workers (§14).
const runningWord = "running "

// traceInk paints a header cell at a tier the state axis deliberately refuses.
//
// tokens.ResolveToken's third rule is explicit that the SECONDARY tier is never
// reached from the state axis — "a status line is secondary because of WHAT IT
// IS, not because of how live it is; a renderer names TextSecondary directly" —
// so this is the sanctioned door and not a way around the grammar. The painted
// span goes through [blocks.Header] whole: width is measured ANSI-aware, the cut
// is ANSI-aware, and the header's own paint wraps a span that has already chosen
// its colour, so the inner tier is what reaches the screen.
//
// A nil styler paints nothing, which is exactly what a golden at [blocks.Plain]
// wants.
func traceInk(style *tokens.Styler, text string, tier tokens.Token) string {
	if style == nil || text == "" {
		return text
	}
	return style.PaintToken(text, tier)
}

// traceTitle paints a tool row's own words, and colours a SHELL COMMAND as the
// source it is.
//
// §5 asks for it in as many words — "shell commands syntax-highlighted at the
// dim end of the pastel ramp" — and the reason is that a command is the one
// salient input with internal structure a reader parses rather than reads: the
// program, its flags, its quoted argument. Everything else this table names is
// a query, a path or a domain, which are single words to the eye and gain
// nothing from being taken apart.
//
// THE HIGHLIGHTER IS NOT HERE. internal/tui2/prose owns chroma and the one
// chroma style built from the ramp; this asks it for a painted span
// ([richtext.HighlightLine]) rather than keeping a second copy of a two-hundred
// row table. What comes back goes through [blocks.Header] whole, and the header
// survives it: width is measured ANSI-aware, the cut is ANSI-aware, and a
// pre-painted span passes through the header's own paint with its own colours
// intact — the same property [blocks.CardBlock] relies on for a pre-rendered
// body line. Under a profile with no code ramp the span comes back at the plain
// tool tier and the row reads exactly as it did.
func traceTitle(style *tokens.Styler, event traceEvent, title string) string {
	if title == "" {
		return ""
	}
	// Only the command itself, and only when the row is showing the command. A
	// row that fell back to naming its tool is naming machinery, not source.
	if event.family.kind == kindShell && title == event.gist {
		return richtext.HighlightLine(style, title, shellLexer, tokens.TextSecondary)
	}
	return traceInk(style, title, tokens.TextSecondary)
}

// shellLexer is what a command is lexed as. It is chroma's own name for the
// language and not a display word, so it lives beside the read it feeds.
const shellLexer = "bash"

// callGlyph is the family's mark, and it reports whether the family was named.
//
// A call whose family answers to nothing wears the fold mark alone — which is
// honest, because the only thing this reader knows about it is that it opens.
func callGlyph(event traceEvent, style *tokens.Styler) (glyph string, named bool) {
	switch event.family.kind {
	case kindShell:
		return style.Glyph(tokens.GShell), true
	case kindWeb:
		return style.Glyph(tokens.GSearch), true
	case kindFile:
		return style.Glyph(tokens.GWrite), true
	}
	return style.Glyph(tokens.GCollapsed), false
}

// gutterRows draws returned bytes behind the `│` gutter of §5: dim, quoted, and
// never markdown.
//
// ONE SOURCE LINE IS ONE ROW, cut at the edge on §16's one ellipsis grammar
// rather than wrapped. A tool's output is PREFORMATTED — the same reason prose
// refuses to re-flow a fenced block — so re-flowing a directory listing or a
// diff invents line breaks nobody wrote and turns a column of paths into a
// paragraph. It is also what makes the box bounded by construction: N lines in,
// N rows out, so [splitBox] can promise [traceBoxRows] and be right.
func gutterRows(dst []string, text string, width, indent int, style *tokens.Styler) []string {
	if width < 1 {
		return dst
	}
	if indent >= width {
		indent = 0
	}
	pad := strings.Repeat(" ", indent)
	bar := style.Glyph(tokens.GProseQuote)
	room := width - indent - blocks.Width(bar) - 1
	if room < 1 {
		room = 1
	}
	paint := func(text string, token tokens.Token) string {
		if style == nil {
			return text
		}
		return style.PaintToken(text, token)
	}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = blocks.Truncate(strings.TrimRight(line, " \t"), room)
		if line == "" {
			// A blank line inside the output keeps the gutter and gains no
			// trailing space: a row that ended in whitespace would be a row
			// nobody can select cleanly.
			dst = append(dst, pad+paint(bar, tokens.TextTertiary))
			continue
		}
		dst = append(dst, pad+paint(bar+" ", tokens.TextTertiary)+
			paint(line, tokens.TextTertiary))
	}
	return dst
}

// -- the seam and the legend ---------------------------------------------------

// traceSeamID is the one seam a record draws, and its block identity.
const traceSeamID = "room-execution-seam"

// traceSeamWord is the one faint lowercase word §15 allows a section, at the
// one boundary in the record that is genuinely ambiguous without it: above the
// line is what the job DELIVERED, below it is what it DID.
const traceSeamWord = "execution"

// seamBlock is the `── execution ──` hairline and the legend under it.
//
// THE LEGEND IS TAUGHT ONCE, INSIDE THE SCROLL, and that placement is the whole
// of it: §12's discoverability rule is "ambient — never tutorials", and a line
// that scrolls away with the document it introduces is ambient in a way a
// pinned key hint is not. It is v1's own idiom, and it is drawn at the dimmest
// tier because nobody said it.
//
// It is the record's ONLY hairline (§5). The delivery/execution boundary is the
// one place a rule earns its row, because the two halves of a record are
// different kinds of thing rather than two paragraphs of one thing.
type seamBlock struct {
	style *tokens.Styler

	width    int
	measured bool
	rows     []string
}

var _ blocks.Block = (*seamBlock)(nil)

func (b *seamBlock) ID() string                { return traceSeamID }
func (b *seamBlock) IsFinalized() bool         { return true }
func (b *seamBlock) SettledRows(width int) int { return len(b.Rows(width)) }
func (b *seamBlock) Version() uint64           { return 0 }
func (b *seamBlock) End() blocks.EndState      { return blocks.EndCompleted }

// Rows draws the seam, the legend and the blank line that separates the legend
// from the first execution row.
func (b *seamBlock) Rows(width int) []string {
	if width < 1 {
		width = 1
	}
	if b.measured && b.width == width {
		return b.rows
	}
	paint := func(text string, token tokens.Token) string {
		if b.style == nil {
			return text
		}
		return b.style.PaintToken(text, token)
	}
	// THE SEAM IS §16'S WORD-IN-LINE RULE, drawn by the one renderer that owns
	// that shape ([blocks.Ruled]). It used to be spelled here, with TWO leading
	// marks, which put `execution` at column 3 — the one row on this page that
	// did not hang at the content edge every row above and below it hangs at.
	// The renderer leads with one mark for exactly that reason.
	rows := append(b.rows[:0],
		blocks.Ruled{Title: traceSeamWord, State: blocks.StateChrome}.
			Render(width, b.styler()))
	if legend := traceLegend(b.style); blocks.Width(legend)+bodyIndent <= width {
		rows = append(rows, strings.Repeat(" ", bodyIndent)+paint(legend, tokens.TextTertiary))
	}
	rows = append(rows, "")
	b.rows, b.width, b.measured = rows, width, true
	return b.rows
}

// styler resolves this block's paint for the shared renderers.
func (b *seamBlock) styler() blocks.Styler {
	if b.style == nil {
		return blocks.Plain
	}
	return b.style
}

// traceLegend is the one line that teaches the four voices and the door, in the
// order the reader meets them.
func traceLegend(style *tokens.Styler) string {
	sep := " " + tokens.GlyphSeparator + " "
	return strings.Join([]string{
		style.Glyph(tokens.GThought) + " model",
		style.Glyph(tokens.GShell) + " shell",
		style.Glyph(tokens.GSearch) + " web",
		style.Glyph(tokens.GPromptChat) + " you",
		style.Glyph(tokens.GCollapsed) + " expands",
	}, sep)
}
