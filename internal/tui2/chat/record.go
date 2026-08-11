package chat

import (
	"sort"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The work record: what a task room is a transcript OF.
//
// 4.6 says a v1 task room is "a view over the same journal, filtered to one
// node", and until this file existed that filter was one table: the messages
// anchored to the subtree. A resident-run task barely writes any. Measured on a
// live run (2026-08-11, `room-homes/room-repro/graph.db`), a commissioned job
// left EIGHT events under its node — `subtree_spliced`, `node_claimed`,
// `node_started`, two `usage_recorded`, `delivery_gate`, `node_completed`,
// `message_posted` — and exactly ONE of them was a message. Enter that room
// while it runs and the filter answers with nothing; enter it after it settles
// and the filter answers with one collapsed card. The reader who commissioned
// real work, watched a card move on the rail and then opened the room got
// 12.14's teaching line, forever, over work that demonstrably happened.
//
// The record was never missing. It was in the columns beside the ones the room
// read: a node's BRIEF is what it was asked to do, its SUMMARY is what it says
// it did, its ERROR is how it failed, and its STATUS, clock and waits-on edges
// are its lifecycle. The rail has been drawing all of it as a tree since 13.11.
// This file draws the same reading as a transcript, so the room says what the
// rail says — and neither one invents a row (5.20 rule 1, 8.2.20).
//
// THREE KINDS OF ROW, ONE ORDER.
//
//   - THE CHARGE, first and always: the job's own name, telemetry and brief.
//     It is the plan for an atomic job, which has no parts to be a plan, and it
//     is the reason a room that has not yet produced anything is still worth
//     entering — it says what is being worked on while it is being worked on.
//   - ONE ROW PER PART, at its birth position, collapsed: the state glyph 5.17
//     assigns, the part's name, its clock, what it waits on, and its own words
//     folded behind the `▸` grammar 4.3 asks for ("inline collapsed tool-call
//     rows … the head's actions visible in-thread, expandable"). A part with
//     nothing to say yet is still a row, because the fact that it exists and is
//     queued IS the record at that moment.
//   - THE JOURNALED MESSAGES, dressed exactly as they always were: a steer, a
//     narrator line, and the settled deliverable card 13.10 built. Nothing
//     about their dressing changes here.
//
// WHAT THIS FILE DOES NOT DO. It writes nothing. Every cell comes from the
// snapshot the rail was already built from, at the watermark the poll already
// paid for, so a room costs no store read of its own beyond the message trail
// it always read. Where the record is genuinely absent it stays absent: there
// is no per-part money cell, because per-node usage is the read gap 13.11
// filed on [Graph] and an invented number is worse than a missing one.

// workRow is one node's own account of itself: the row the rail drew for it,
// paired with the node the row was drawn from.
//
// Both halves are needed and neither is redundant. The ROW carries the name,
// the lifecycle and the clock the rail already resolved — so the tree and the
// transcript cannot disagree about the same part, which is 12.14's law ("a
// preview that outranks the thing it previews is the affordance lying") asked
// of two renderings of one node. The NODE carries the prose a 28-column rail
// had no room for: the whole brief, the whole summary, the whole error.
type workRow struct {
	row  rail.Row
	node store.Node
}

// workRecord is one task's subtree as the room reads it: the surface row first,
// then every part in splice order.
//
// It is a map lookup and a walk over rows already built. The scope was compiled
// in the same pass that built the card (scope.go's taskRows), so this cannot
// issue a read and cannot see a different subtree than the rail is showing.
func (s *scopeSource) workRecord(root string) []workRow {
	root = strings.TrimSpace(root)
	if s == nil || root == "" {
		return nil
	}
	scope, ok := s.tasks[rowTaskPrefix+root]
	if !ok {
		// No scope means the board has never mentioned this node. A room over
		// it has no record to draw, which is a different thing from a record
		// that is empty, and both are handled by the caller finding nothing
		// here.
		return nil
	}
	out := make([]workRow, 0, len(scope.Rows))
	for _, row := range scope.Rows {
		id := strings.TrimPrefix(row.ID, rowTaskPrefix)
		node, known := s.nodes[id]
		if !known {
			continue
		}
		out = append(out, workRow{row: row, node: node})
	}
	return out
}

// recordStamp is what the room's transcript was last built from.
//
// A task room repaints on the cycles the journal moved, and most of those moves
// are not about this task. The stamp is every number that can change a row —
// each node's own update sequence and status, and the trail's length and tail —
// so an unrelated move costs one comparison instead of a rebuild. It is a
// fingerprint and not a hash: collisions would have to agree on every node's
// update sequence at once, which is the same thing as nothing having changed.
func recordStamp(record []workRow, messages []store.Message) string {
	var b strings.Builder
	for i := range record {
		b.WriteString(record[i].node.ID)
		b.WriteByte(':')
		b.WriteString(strconv.FormatInt(record[i].node.UpdatedSeq, 10))
		b.WriteByte(':')
		b.WriteString(string(record[i].node.Status))
		b.WriteByte('|')
	}
	b.WriteByte('m')
	b.WriteString(strconv.Itoa(len(messages)))
	if n := len(messages); n > 0 {
		b.WriteByte(':')
		b.WriteString(strconv.FormatInt(messages[n-1].Seq, 10))
	}
	return b.String()
}

// -- the blocks --------------------------------------------------------------

// chargeBlockID and workBlockID are the room's own block identities. They are
// not message sequences — nothing in the record is a message — so they carry
// their own prefixes and can never collide with [messageID].
const chargeBlockID = "room-charge"

func workBlockID(node string) string { return "room-work-" + node }

// chargeBlock is the room's first row: what this task was asked to do.
//
// It is the answer to the half of the report the parts cannot answer. "No plan"
// is a fair complaint about a room over an ATOMIC job, which has no parts for a
// plan to be made of — and the plan was journaled all along, as the node's
// brief, the same sentence the head's commissioning row shows in the room that
// commissioned it (13.3.1's columns, never prose). The owner thread has always
// had it; the room it is about did not.
//
// The header is the card the reader entered from, by construction: 12.14's
// finding 1 is that an entered room must never know less about a task than the
// card above it, so the name, the glyph and the telemetry are the row's own and
// are not derived a second time here.
func chargeBlock(item workRow, style *tokens.Styler) *messageBlock {
	block := &messageBlock{id: chargeBlockID, style: style}
	block.head = blocks.Header{
		Glyph: item.row.Attention().Glyph(),
		Title: item.row.Name,
		State: blocks.StateChrome,
	}
	if cells := cardTelemetry(item.row); cells != "" {
		block.head.Meta = append(block.head.Meta, cells)
	}
	brief, rest := splitGist(item.node.Brief, gistCap)
	if brief != "" {
		block.segs = append(block.segs, segment{
			kind: segProse, text: brief, tier: tokens.TextSecondary, indent: bodyIndent,
		})
	}
	if rest == "" {
		return block
	}
	block.collapsible = true
	block.hidden = strings.Count(rest, "\n") + 1
	block.head.Hint = blocks.ExpandHint(false, block.hidden)
	block.segs = append(block.segs, segment{
		kind: segProse, text: rest, tier: tokens.TextSecondary,
		indent: bodyIndent, folded: true,
	})
	return block
}

// workBlock is one part, at 4.3's collapsed-row grammar.
//
//	✓ XhrSyn · 12s                                             ▸ 4 lines
//	  Synchronous XHR blocks the event loop, so browsers deprecated it.
//
// Line 1 is the state glyph, the part's name and its clock; line 2 is the first
// line of what the part said, and the rest of it folds. A part that has said
// nothing is line 1 alone, plus `waits on H2` when that is why — which is the
// honest record of a queued part and is exactly what the rail draws for the
// same row one column over.
//
// THE HUE IS ONLY EVER A CLAIM THE BOARD MADE (8.2.20, 13.10's own rule). Green
// for a part that finished, coral for one that failed, and no hue at all for
// one still in flight — because a failure drawn green is the single mistake
// this row can make.
//
// THERE IS NO MONEY CELL, and that is the read gap 13.11 filed rather than a
// forgotten field: [Graph.TopLevelJobUsage] answers per JOB ROOT, so a per-part
// dollar figure would have to be invented. 8.2.20 prefers the absent cell.
func workBlock(item workRow, style *tokens.Styler) *messageBlock {
	block := &messageBlock{id: workBlockID(item.node.ID), style: style}
	block.head = blocks.Header{
		Glyph: item.row.Attention().Glyph(),
		Title: item.row.Name,
		State: blocks.StateChrome,
	}
	switch item.row.Life {
	case rail.LifeSettled:
		block.head.GlyphHue = blocks.HueMoney
	case rail.LifeFailed, rail.LifeCancelled:
		block.head.GlyphHue = blocks.HueBroken
	}
	if cells := workCells(item.row); cells != "" {
		block.head.Meta = append(block.head.Meta, cells)
	}
	// What the part waits on is drawn before what it said, because for a row
	// that has not spoken it is the whole explanation of the silence (5.15's
	// wireframe puts `waits on H2` directly under the row for the same reason).
	if len(item.row.WaitsOn) > 0 {
		block.segs = append(block.segs, segment{
			kind: segProse, text: "waits on " + strings.Join(item.row.WaitsOn, ", "),
			tier: tokens.TextTertiary, indent: bodyIndent,
		})
	}
	said, rest := splitGist(workBody(item.node), gistCap)
	if said != "" {
		block.segs = append(block.segs, segment{
			kind: segProse, text: said, tier: tokens.TextSecondary, indent: bodyIndent,
		})
	}
	if rest == "" {
		return block
	}
	block.collapsible = true
	block.hidden = strings.Count(rest, "\n") + 1
	block.head.Hint = blocks.ExpandHint(false, block.hidden)
	block.segs = append(block.segs, segment{
		kind: segProse, text: rest, tier: tokens.TextSecondary,
		indent: bodyIndent, folded: true,
	})
	return block
}

// workBody is what a part actually said about itself.
//
// A FAILURE SPEAKS ITS ERROR AND NOT ITS SUMMARY. A node that failed may carry
// both — the summary is how far it got, the error is why it stopped — and the
// second one is the fact the reader opened the room for. They are joined rather
// than chosen between, error first, because 13.1 item 3's rule is that the
// dressing presents the record whole and never edits it.
func workBody(node store.Node) string {
	summary := strings.TrimSpace(node.Summary)
	reason := strings.TrimSpace(node.Error)
	switch {
	case reason == "":
		return summary
	case summary == "":
		return reason
	default:
		return reason + "\n\n" + summary
	}
}

// gistCap is how much of its own words a collapsed row shows before the rest
// folds — about two rows at an ordinary transcript width.
//
// A cap is needed and a newline is not enough. [splitHeadline] folds at the
// first line break, which is the right boundary for a worker that wrote a
// headline and then its detail, and NO boundary at all for one that wrote a
// single four-hundred-character paragraph. Measured live at 120 columns: one
// part's unbroken result took six rows and pushed the job's own charge, and the
// three parts under it, off the screen — which is 13.10's finding ("a job that
// wrote a long answer pushed the conversation off the screen") arriving one
// surface later, and 5.9's progressive disclosure is the law it breaks.
const gistCap = 160

// splitGist is [splitHeadline] with a length of its own.
//
// It cuts at the first line break, and then, only if what it kept is still too
// long to be a status line, at the last sentence end before the cap — falling
// back to the last word boundary when the paragraph has no sentence in it. The
// FOLD KEEPS EVERY WORD IT MOVED: this splits the record, it never shortens it,
// so an expanded row still reads exactly what the worker wrote (13.1 item 3).
func splitGist(body string, limit int) (gist, rest string) {
	head, tail := splitHeadline(body)
	join := func(a, b string) string {
		a, b = strings.TrimSpace(a), strings.TrimSpace(b)
		switch {
		case a == "":
			return b
		case b == "":
			return a
		default:
			return a + "\n" + b
		}
	}
	if len(head) <= limit {
		return head, tail
	}
	cut := sentenceCut(head, limit)
	if cut <= 0 {
		cut = strings.LastIndexByte(head[:limit], ' ')
	}
	if cut <= 0 {
		return head, tail
	}
	return strings.TrimSpace(head[:cut]), join(head[cut:], tail)
}

// sentenceCut is the byte after the last sentence that ends inside limit, or
// zero when there is none. A terminator only counts when a space follows it, so
// a version number or an ellipsis is not mistaken for the end of a thought.
func sentenceCut(text string, limit int) int {
	if limit > len(text) {
		limit = len(text)
	}
	for i := limit - 1; i > 0; i-- {
		switch text[i] {
		case '.', '!', '?':
			if i+1 < len(text) && text[i+1] == ' ' {
				return i + 1
			}
		}
	}
	return 0
}

// workCells is a part's telemetry line: its clock, and the size of what is
// under it. It deliberately does not spell money — see [workBlock].
func workCells(row rail.Row) string {
	cells := make([]string, 0, 2)
	if row.Meta.HasElapsed {
		cells = append(cells, tokens.Elapsed(row.Meta.Elapsed))
	}
	if row.Meta.HasWorkers {
		cells = append(cells, plural(row.Meta.Workers, "part", "parts"))
	}
	return strings.Join(cells, " "+tokens.GlyphSeparator+" ")
}

// -- assembling the room -----------------------------------------------------

// hasRecord is the emptiness question, and it is the one 12.14's teaching line
// is the answer to.
//
// A ROOM IS EMPTY WHEN THE JOURNAL HAS NOTHING TO SAY ABOUT IT — not when its
// message trail is empty, which is the reading this wave replaces. A task's name
// and its status are not a record: they are the CARD, and the card is what the
// empty room draws beside the line explaining why there is nothing under it. So
// the question is asked of the four things that would be worth reading — a part,
// a message, the job's brief, or the job's own account of how it went — and the
// teaching line survives exactly as long as all four are absent.
// nodeTraceBlocks is one part's execution rows, or none when its worker has not
// written a recorder — which is the ordinary case for a node that plans rather
// than runs, and is an ABSENT row and never an empty one.
func nodeTraceBlocks(item workRow, traces map[string]nodeTrace,
	style *tokens.Styler) []blocks.Block {

	held, ok := traces[item.node.ID]
	if !ok {
		return nil
	}
	return traceBlocks(item.node.ID, parseTrace(held.text), style)
}

// hasTrace says a room has execution rows to draw even if the journal has
// nothing to say about it.
//
// It exists because of the exact shape of the bug this lane fixes: a worker can
// be fifteen seconds into a run, have made four tool calls, and have journaled
// NOTHING but `node_started` — H13 measured precisely that. Without this clause
// such a room would draw 12.14's teaching line ("nothing journaled here yet")
// over a trace sitting on disk beside it, which is the same class of lie one
// layer further in.
func hasTrace(record []workRow, traces map[string]nodeTrace) bool {
	for i := range record {
		if held, ok := traces[record[i].node.ID]; ok && strings.TrimSpace(held.text) != "" {
			return true
		}
	}
	return false
}

func hasRecord(record []workRow, messages []store.Message) bool {
	if len(messages) > 0 || len(record) > 1 {
		return true
	}
	if len(record) == 0 {
		return false
	}
	return strings.TrimSpace(record[0].node.Brief) != "" || workBody(record[0].node) != ""
}

// roomBlocks is the whole transcript of a task room, in one order.
//
// The order is the journal's, and it is stable under everything that moves. A
// part sits at its BIRTH — the sequence it was spliced at — and not at its last
// update, so a room does not re-shuffle itself as its parts finish; that is
// 4.3's "settled deliverable cards stay inline at birth position" applied to the
// rows a deliverable is made of, and it is the difference between a record and a
// leaderboard. Messages sit at their own sequence, which is the same counter
// (the `messages` table's seq IS the event seq), so the two interleave without
// anything having to be reconciled.
//
// THE EXECUTION ROWS SIT UNDER THE PART THAT PRODUCED THEM, and that placement
// is decided by the sort rather than by a second list. A node's trace entries
// carry the node's own seq and order — the same pair its work row carries — and
// [sort.SliceStable] keeps insertion order among equals, so "what this part did"
// lands directly beneath "this part", wherever the part itself lands. The root's
// trace carries the root's birth, which puts it directly under the charge, which
// is v1's own document order (brief, `── execution ──`, then the thread).
func roomBlocks(record []workRow, messages []store.Message, style *tokens.Styler,
	board jobSource, traces map[string]nodeTrace) []blocks.Block {

	if !hasRecord(record, messages) && !hasTrace(record, traces) {
		return nil
	}

	type entry struct {
		seq   int64
		order int
		block blocks.Block
	}
	entries := make([]entry, 0, len(record)+len(messages)+1)

	// A node whose ending the trail already carries does not get a second row
	// for it. 13.10's delivery card is this same fact dressed better — it has
	// the money, the lifecycle hue and the artifact rows a work row cannot read
	// — so where both exist the card wins and the work row stands down. This is
	// the whole of the de-duplication, and it is decided on COLUMNS (isDelivery)
	// rather than by comparing prose.
	delivered := make(map[string]bool, len(messages))
	for i := range messages {
		if isDelivery(messages[i]) {
			delivered[strings.TrimSpace(messages[i].NodeID)] = true
		}
	}

	// The charge is record[0] — taskScope puts the surface row first — and it
	// leads the room outright rather than taking its place in the sort. It is
	// the room's subject, not an event in it.
	var lead blocks.Block
	if len(record) > 0 {
		lead = chargeBlock(record[0], style)
		// THE JOB'S OWN ENDING, when nobody else is carrying it. `announceNode`
		// posts a delivery message only for a node parented on the spine, so a
		// sub-job, and any job whose ending has not been announced yet, keeps
		// its whole result in `nodes.summary` where no message can be read from.
		// That row is the answer to "maybe it shows the final answer — it
		// doesn't show that either", and it is journaled: it is the same field
		// the rail's own status line is a first line of.
		//
		// It sits at the sequence the ending was journaled at, not at the job's
		// birth, because an ending belongs where it happened. The parts below
		// keep their birth positions (4.3), so the room reads charge, plan,
		// ending, in the order those three things occurred.
		if !delivered[record[0].node.ID] && workBody(record[0].node) != "" {
			entries = append(entries, entry{
				seq:   record[0].node.UpdatedSeq,
				block: workBlock(record[0], style),
			})
		}
		for _, block := range nodeTraceBlocks(record[0], traces, style) {
			entries = append(entries, entry{
				seq: record[0].node.CreatedSeq, order: record[0].node.CreatedOrder, block: block,
			})
		}
	}
	for i := 1; i < len(record); i++ {
		if !delivered[record[i].node.ID] {
			entries = append(entries, entry{
				seq:   record[i].node.CreatedSeq,
				order: record[i].node.CreatedOrder,
				block: workBlock(record[i], style),
			})
		}
		for _, block := range nodeTraceBlocks(record[i], traces, style) {
			entries = append(entries, entry{
				seq: record[i].node.CreatedSeq, order: record[i].node.CreatedOrder, block: block,
			})
		}
	}
	for i := range messages {
		message := messages[i]
		entries = append(entries, entry{
			seq:   message.Seq,
			block: newMessageBlock(message, style, board),
		})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].seq != entries[j].seq {
			return entries[i].seq < entries[j].seq
		}
		return entries[i].order < entries[j].order
	})

	out := make([]blocks.Block, 0, len(entries)+1)
	if lead != nil {
		out = append(out, lead)
	}
	for i := range entries {
		out = append(out, entries[i].block)
	}
	return out
}
