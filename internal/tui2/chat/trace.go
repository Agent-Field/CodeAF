package chat

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
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
//   - It does not invent a glyph. 5.17's vocabulary has no tool marks and this
//     is not the lane to add eight of them; 4.3's own example of the row it asks
//     for is `▸ control: cancelled wisp-nav2` — the fold mark and the verb. A
//     call that returned wears ✓ and one that failed wears ✕, which is 5.17's
//     state vocabulary asked of the smallest unit of work there is.
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
func (a *App) readTraceCmd(root string) tea.Cmd {
	reader, ok := a.commander.(Trace)
	if !ok || a.source == nil || root == "" {
		return nil
	}
	nodes := a.source.subtreeNodes(root)
	if len(nodes) > traceMaxNodes {
		nodes = nodes[:traceMaxNodes]
	}
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
func (a *App) applyTraceRead(msg traceReadMsg) {
	if a.view == nil || a.view.kind != viewNode || a.view.node != msg.node {
		return
	}
	if !msg.moved || len(msg.traces) == 0 {
		return
	}
	if a.traces == nil {
		a.traces = make(map[string]nodeTrace, len(msg.traces))
	}
	for id, held := range msg.traces {
		a.traces[id] = held
	}
	// The room's stamp is what decides whether paintRoom rebuilds, and it is a
	// fingerprint of the JOURNAL. A recorder that grew moved nothing the journal
	// can see, so the stamp is cleared rather than recomputed — the next paint is
	// unconditional and the one after it is cheap again.
	a.view.stamp = ""
	a.paintRoom()
}

// -- the parse -----------------------------------------------------------------

// traceKind is what one line of a recorder turned out to be.
type traceKind uint8

const (
	// traceTurnRule is a model round: `── turn 2  finish=…  in=… out=… ──`.
	traceTurnRule traceKind = iota
	// traceThought is what the model said between calls.
	traceThought
	// traceCall is one tool call, carrying its own result once that arrives.
	traceCall
	// traceSteer is a sentence the reader aimed at this worker mid-run.
	traceSteer
	// traceNote is a run-level fact the recorder wrote free-form.
	traceNote
)

// traceEvent is one drawable row of a recorder.
type traceEvent struct {
	kind traceKind
	// title is the row's name: the turn, the tool, or the speaker.
	title string
	// meta is the row's telemetry cells, already worded.
	meta []string
	// gist is the line that stands above the fold; detail is what folds under
	// it. The fold KEEPS EVERY WORD it moved (13.1 item 3).
	gist, detail string
	// failed marks a call whose result came back an error, which is the one
	// thing on this row that may wear a hue (8.2.20).
	failed bool
}

// parseTrace turns one recorder's tail into rows.
//
// The grammar is internal/exec/trace.go's, read straight off its five Fprintf
// calls, and the reason it can be line-based at all is that writer's `snip`:
// every record has its newlines replaced by ⏎ before it is written, so one
// record is one line by construction and no state machine is needed to find the
// end of one.
func parseTrace(text string) []traceEvent {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var out []traceEvent
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "── turn "):
			out = append(out, parseTurnRule(line))
		case strings.HasPrefix(line, "text: "):
			gist, detail := splitGist(unsnip(strings.TrimPrefix(line, "text: ")), traceGistCap)
			out = append(out, traceEvent{kind: traceThought, title: "thinking",
				gist: gist, detail: detail})
		case strings.HasPrefix(line, "call "):
			out = append(out, parseCall(strings.TrimPrefix(line, "call ")))
		case strings.HasPrefix(line, "  → "):
			// A result belongs to the call above it. A result with no call above
			// it is a tail cut mid-turn, and it becomes a row of its own rather
			// than being dropped — the reader is looking at the middle of a
			// document and should be told so by what is on screen.
			if n := len(out); n > 0 && out[n-1].kind == traceCall {
				absorbResult(&out[n-1], strings.TrimPrefix(line, "  → "))
				continue
			}
			out = append(out, traceEvent{kind: traceNote, title: "result",
				gist: unsnip(strings.TrimPrefix(line, "  → "))})
		case strings.HasPrefix(line, "steered: "):
			gist, detail := splitGist(unsnip(strings.TrimPrefix(line, "steered: ")), traceGistCap)
			out = append(out, traceEvent{kind: traceSteer, title: "you",
				gist: gist, detail: detail})
		default:
			// The recorder's own free-form notes — the contract in force, a
			// stop, a nudge. An unrecognised line lands here too, ON PURPOSE:
			// see this file's header.
			out = append(out, traceEvent{kind: traceNote, gist: unsnip(line)})
		}
	}
	if len(out) > traceMaxEvents {
		out = out[len(out)-traceMaxEvents:]
	}
	return out
}

// parseTurnRule reads `── turn 2  finish=tool_calls  in=2007 out=58 cached=0  [nudge] ──`.
//
// Only two of those numbers reach a cell. `out=` is what the turn actually
// produced and is the one number a reader can do anything with; `finish=` is a
// provider's own word and belongs to 5.14's never-shown tier unless it says
// something went wrong, which the note in brackets already does more plainly.
func parseTurnRule(line string) traceEvent {
	body := strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(line, "── turn ")), "──")
	event := traceEvent{kind: traceTurnRule, title: "turn"}
	if note := bracketed(body); note != "" {
		event.meta = append(event.meta, note)
	}
	for i, field := range strings.Fields(body) {
		switch {
		case i == 0:
			event.title = "turn " + field
		case strings.HasPrefix(field, "out="):
			if n, err := strconv.Atoi(strings.TrimPrefix(field, "out=")); err == nil && n > 0 {
				event.meta = append([]string{strconv.Itoa(n) + " tok"}, event.meta...)
			}
		}
	}
	return event
}

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
// The row shows the tool's name and the ONE argument that says what the call was
// for; the whole argument object folds under it. The salient key is found by
// LOOKING, against a short list of the names an argument that describes a call
// actually goes by — not by a per-tool table, because a table is a list of the
// tools that existed on the day it was written and a room that met a new one
// would show it as `{…}`.
func parseCall(rest string) traceEvent {
	name, args, _ := strings.Cut(rest, " ")
	event := traceEvent{kind: traceCall, title: strings.TrimSpace(name)}
	if event.title == "" {
		event.title = "call"
	}
	args = strings.TrimSpace(args)
	if args == "" {
		return event
	}
	gist := salientArg(args)
	if gist == "" {
		gist = unsnip(args)
	}
	event.gist, event.detail = splitGist(gist, traceGistCap)
	if full := unsnip(args); full != gist {
		event.detail = join2(event.detail, full)
	}
	return event
}

// salientKeys are the argument names a call's SUBJECT goes by, most specific
// first. Every one of them is a name some tool in this codebase already uses;
// what makes the mechanism general is that it is a search over the object rather
// than a switch on the tool.
var salientKeys = []string{"cmd", "q", "query", "path", "file", "prompt", "url", "urls", "pattern", "text"}

// salientArg finds the one argument worth putting on the row.
func salientArg(args string) string {
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.ReplaceAll(args, "⏎", "\\n")), &object); err != nil {
		return ""
	}
	for _, key := range salientKeys {
		raw, ok := object[key]
		if !ok {
			continue
		}
		var one string
		if err := json.Unmarshal(raw, &one); err == nil {
			return strings.TrimSpace(one)
		}
		var many []string
		if err := json.Unmarshal(raw, &many); err == nil && len(many) > 0 {
			return strings.Join(many, "  ")
		}
	}
	return ""
}

// absorbResult folds `  → 1652B: search: …` onto the call above it.
//
// The SIZE goes on the header as telemetry and the content goes under the fold,
// which is the whole difference between this row and v1's: there, a result was
// three lines of body under every call and a five-call turn filled a screen with
// output nobody had asked to read. Here it is one cell and a `▸`.
func absorbResult(event *traceEvent, rest string) {
	head, content, found := strings.Cut(rest, ": ")
	if !found {
		head, content = rest, ""
	}
	if strings.HasSuffix(head, " ERROR") {
		event.failed = true
		head = strings.TrimSuffix(head, " ERROR")
	}
	if bytes := strings.TrimSuffix(strings.TrimSpace(head), "B"); bytes != "" {
		if n, err := strconv.Atoi(bytes); err == nil {
			event.meta = append(event.meta, humanBytes(n))
		}
	}
	if content = strings.TrimSpace(unsnip(content)); content != "" {
		event.detail = join2(event.detail, content)
	}
}

// humanBytes is a size a reader can hold, at the granularity 5.14 allows a
// telemetry cell: never more than three significant characters.
func humanBytes(n int) string {
	switch {
	case n < 1024:
		return strconv.Itoa(n) + "B"
	case n < 1024*1024:
		return strconv.Itoa((n+512)/1024) + "KB"
	default:
		return strconv.Itoa((n+512*1024)/(1024*1024)) + "MB"
	}
}

// unsnip puts the writer's line breaks back. internal/exec/trace.go replaces
// every newline with ⏎ so one record is one line; a renderer that left them as
// ⏎ would be showing the reader the transport.
func unsnip(text string) string {
	return strings.TrimSpace(strings.ReplaceAll(text, "⏎", "\n"))
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
	return "room-trace-" + node + "-" + strconv.Itoa(index)
}

// traceBlocks draws one node's recorder as transcript rows.
//
// Every row is COLLAPSED at rest — 4.3's "inline collapsed tool-call rows … the
// head's actions visible in-thread, expandable, exactly like a coding-agent
// harness renders tool use", which is the one bullet of that section's
// transcript anatomy that has never had a renderer. Two rows per call: what was
// called and what it was called with. Everything else is behind the `▸` that
// 13.16 made a door.
func traceBlocks(node string, events []traceEvent, style *tokens.Styler) []blocks.Block {
	if len(events) == 0 {
		return nil
	}
	out := make([]blocks.Block, 0, len(events))
	for i := range events {
		out = append(out, traceBlock(traceBlockID(node, i), events[i], style))
	}
	return out
}

// traceBlock is one execution row.
func traceBlock(id string, event traceEvent, style *tokens.Styler) *messageBlock {
	block := &messageBlock{id: id, style: style}
	block.head = blocks.Header{Title: event.title, State: blocks.StateChrome}
	block.head.Meta = append(block.head.Meta, event.meta...)

	switch event.kind {
	case traceCall:
		// 5.17's state vocabulary, asked of the smallest unit of work there is:
		// a call that came back, and one that came back an error. Nothing else
		// on the row carries a hue, because a hue that meant two things would
		// mean neither (5.16).
		block.head.Glyph = tokens.GlyphSettled
		block.head.GlyphHue = blocks.HueMoney
		if event.failed {
			block.head.Glyph = tokens.GlyphFailed
			block.head.GlyphHue = blocks.HueBroken
		}
	case traceSteer:
		// The steer prompt, which is what the reader typed this with (5.11).
		block.head.Glyph = tokens.GlyphPromptSteer
	case traceTurnRule:
		// A turn is a seam and not an event: it has a name, a token count and
		// nothing under it.
		return block
	}

	if event.gist != "" {
		block.segs = append(block.segs, segment{
			kind: segProse, text: event.gist, tier: tokens.TextSecondary, indent: bodyIndent,
		})
	}
	if strings.TrimSpace(event.detail) == "" {
		return block
	}
	block.collapsible = true
	block.hidden = strings.Count(event.detail, "\n") + 1
	block.head.Hint = blocks.ExpandHint(false, block.hidden)
	block.segs = append(block.segs, segment{
		kind: segProse, text: event.detail, tier: tokens.TextTertiary,
		indent: bodyIndent, folded: true,
	})
	return block
}
