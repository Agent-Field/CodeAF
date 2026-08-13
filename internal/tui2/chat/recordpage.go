package chat

import (
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE RECORD IS CHRONOLOGICAL (user-amended 2026-08-11).
//
// §5 used to open the record with its delivery card — answer first, then the
// `── execution ──` seam, then the work. The reader's own sketch replaces that
// order with the one the work actually happened in:
//
//	  the prompt card      what was asked, and what the head read into it
//	  ── execution ──
//	  the work             the trace, or the living tree
//	  the result card      what came of it
//
// and pays for the lost "answer first" with a SCROLL POSITION rather than with a
// reordering: a settled task opens scrolled to its result card, so the answer is
// still the first thing on screen, and a running one opens at the live tail,
// where the thing worth watching is. The document reads top to bottom as a
// record of one job; the viewport lands where the reader was going anyway.
//
// THE RECORD'S TOP IS A RULE AND NOT A CARD (amends §5's "commitment ground").
// The commitment ground means "made and still running", which is a claim a
// settled record may not make, and a page that wore it also wore §4's one ground
// twice while saying the job's name and its whole receipt in both. §16's
// word-in-line seam says the same three facts in ONE row with no ground at all:
// the label IS the rule. EXACTLY THREE DRESSED ELEMENTS still holds; the first
// of the three changed dress.
//
// TWO VARIANTS, AND THE JOB'S OWN SHAPE PICKS. A job with no parts is an ATOMIC
// record: its middle is the execution trace, dressed exactly as it always was
// (trace.go — four voices, batched tool rows, bounded output boxes). A job with
// parts is a GRAPH record: its middle is the living tree (recordtree.go), and
// the trace of any one part is one click down. §14 forbids either variant from
// announcing which one it is; the shape says it.

// recordInputs is everything the page is built from, gathered once per rebuild.
//
// It is a struct rather than nine parameters because the page now has two
// variants and four seams, and a positional call that long is a call whose
// arguments get swapped in review. Every field is a read that already happened:
// the scope walk, the ledger, the recorder tails, the reader's own fold
// answers.
type recordInputs struct {
	// record is the subtree as the board holds it, surface row first.
	record []workRow
	// messages is the node-anchored trail, kept whole.
	messages []store.Message
	style    *tokens.Styler
	board    jobSource
	// traces is what each worker's recorder said, keyed by node id.
	traces map[string]nodeTrace
	// open is the reader's fold answer for a trace row or a message. Its
	// default is the room-wide receipts flag, which is SHUT.
	open func(id string) bool
	// treeOpen is the reader's fold answer for a tree branch. Its default is
	// OPEN, because the tree is the page's middle rather than a receipt on it —
	// see [App.foldOpenDefault].
	treeOpen func(id string) bool
	// money is the entered job's ledger.
	money spend
	// models is the deduped list of models that billed the ROOT, for its card.
	models []string
	// nodeModels answers the same question per part, for the tree's rows. Nil
	// leaves each row on the binding the board resolved.
	nodeModels func(node string) []string
	// forThread builds the attribution row (chat-simplify.md 5.3): `for  <thread
	// name>`, and a door onto that conversation. It is a SEAM rather than a
	// string because only the window knows what a session is called and whether
	// naming it here would be a door back to the room the row is drawn in — see
	// [App.forThreadRow]. Nil, or a nil block, draws nothing.
	forThread func(session string) blocks.Block
	// now is the instant the snapshot was folded in, which is what a running
	// row's start is counted back from ([startedAt]).
	now time.Time
	// clock is the transcript's shared animation clock (8.1.3).
	clock *blocks.Clock
}

// roomBlocks is the whole record page, in chronological order.
func roomBlocks(in recordInputs) []blocks.Block {
	if !hasRecord(in.record, in.messages) && !hasTrace(in.record, in.traces) {
		return nil
	}
	if len(in.record) == 0 {
		return nil
	}
	root := in.record[0]

	// WHICH ROW IS THE RESULT is decided before anything else is laid out,
	// because the answer is a row that must appear at the BOTTOM and nowhere
	// else. A job whose ending was announced has a delivery message carrying it;
	// a sub-job, or one whose ending has not been announced yet, keeps its whole
	// result in the node's own columns (`announceNode` only posts for a node
	// parented on the spine). Both are the same card and it is drawn once.
	result, consumed := resultCard(root, in)

	out := make([]blocks.Block, 0, len(in.record)+len(in.messages)+4)
	out = append(out, chargeRule(root, in.style, in.money, in.models))
	// THE ATTRIBUTION, immediately under the rule and above the ask (5.3's
	// `attribution` row). It answers "who is this for", which is the question a
	// reader asks between "what is this" (the rule) and "what was asked" (the
	// card) — and it is a door back to the conversation that commissioned the
	// work, which is J2's whole shape: one thread, N tasks, and a way home from
	// each of them.
	if in.forThread != nil {
		if row := in.forThread(strings.TrimSpace(root.node.Provenance.SessionID)); row != nil {
			out = append(out, row)
		}
	}
	out = append(out, chargeBlock(root, in.style))

	// THE PROGRESS LINE, AND THERE IS EXACTLY ONE (user review, 2026-08-11).
	// See [recordStatusBlock] for the defect it kills.
	if status := recordStatus(in.messages, consumed); status != "" {
		out = append(out, &recordStatusBlock{
			style: in.style, clock: in.clock, text: status,
			live: !settledLife(root.row.Life),
		})
	}

	// THE SEAM GOES WHERE THE EXECUTION ROWS BEGIN, and nowhere else. It carries
	// the legend that teaches the four VOICES (trace.go's seamBlock), so it
	// belongs immediately above the rows those voices are drawn on — over a tree
	// it would be teaching a vocabulary the rows beneath it do not use. A record
	// whose engine has no recorder door therefore draws no seam at all, which is
	// the difference between a surface that is quiet and one that is broken
	// (12.10). It remains the record's only hairline (§5, §16's RULED LINES).
	seam := false
	for _, block := range recordMiddle(root, consumed, in) {
		if !seam && isTraceBlock(block) {
			out = append(out, &seamBlock{style: in.style})
			seam = true
		}
		out = append(out, block)
	}
	if result != nil {
		out = append(out, result)
	}
	return out
}

// recordMiddle is the work half of the page: the living tree for a job with
// parts, the execution rows for one without, and in both cases the journaled
// conversation that happened inside the job — a steer the reader aimed at it, a
// narrator's line — at its own place in the sequence.
func recordMiddle(root workRow, consumed string, in recordInputs) []blocks.Block {
	type entry struct {
		seq   int64
		order int
		block blocks.Block
	}
	entries := make([]entry, 0, len(in.record)+len(in.messages))

	rows := buildTree(in.record, in.money, in.nodeModels,
		recordPreview(in.record, in.traces), in.now, in.treeOpen)

	if len(rows) > 0 {
		// THE GRAPH VARIANT. The tree stands for every PART at once, so the
		// per-part work rows are not laid out beside it — a part's own words,
		// its recorder and its result are what its own page holds, one click
		// down. The tree sits at the root's birth, which is where the plan came
		// into being.
		entries = append(entries, entry{
			seq:   root.node.CreatedSeq,
			order: root.node.CreatedOrder,
			block: &recordTreeBlock{
				id: recordTreeID, style: in.style, clock: in.clock,
				rows: rows, live: treeIsLive(rows),
			},
		})
	}
	// THE ROOT'S OWN EXECUTION, in both variants and for the same reason: it is
	// what THIS record's subject did, as opposed to what its parts did. On an
	// atomic job that is the whole middle. On a planned one it is the row-0
	// worker's own recorder — the turns that read the ask, wrote the plan and
	// spliced it — and hiding it would make a record go quiet about the only
	// work it directly owns, which is the defect trace.go exists to fix.
	for _, block := range nodeTraceBlocks(root, in.traces, in.style, in.open, in.clock) {
		entries = append(entries, entry{
			seq: root.node.CreatedSeq, order: root.node.CreatedOrder, block: block,
		})
	}

	// The trail, minus the row that became the result card and minus every
	// MACHINERY row, which the page's one progress line carries instead
	// ([recordStatusBlock]). What is left is what somebody said: the reader's
	// own steers, the head's questions, the learning moments — three sentence
	// classes and no fourth (§1) — each at its own sequence, so a steer reads
	// back where it was aimed.
	for i := range in.messages {
		message := in.messages[i]
		if messageID(message.Seq) == consumed || isMachineryRow(message) {
			continue
		}
		entries = append(entries, entry{
			seq:   message.Seq,
			block: newMessageBlock(message, in.style, in.board),
		})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].seq != entries[j].seq {
			return entries[i].seq < entries[j].seq
		}
		return entries[i].order < entries[j].order
	})
	out := make([]blocks.Block, 0, len(entries))
	for i := range entries {
		out = append(out, entries[i].block)
	}
	return out
}

// -- the top rule ----------------------------------------------------------------

// chargeRule is the page's first row: the job's name set into a hairline, with
// its whole receipt behind it.
//
//	─ wisp-parity-2 · swe · 28m · $8.65 · 41k tok · claude-k3 ────────────────
//
// THREE FACTS, ONE ROW, NO GROUND. It says which job this page is about, how it
// went, and — by being a rule — that everything below it belongs to that job.
// The card it replaces said the first two of those in a raised plane and spent
// six rows doing it, and the plane itself was a claim: §4's commitment ground
// means "made and still running", which a settled record may not say.
//
// THERE IS NO STATE GLYPH, and its absence is §15's delete test rather than an
// oversight. Take the glyph away and nothing is lost: the tree below says what
// every part is doing, and the result card at the foot says how the whole thing
// ended. Leave it in and the page carries a lifecycle in three places, two of
// which are derived and one of which is a rule — a boundary claiming a state is
// a boundary claiming it for both halves of what it separates.
//
// The title is SETTLED and not chrome, which is the one way it differs from the
// execution seam (trace.go): `execution` is a word for the eye that looks for
// the boundary, and this is the name of the thing the page is about.
func chargeRule(root workRow, style *tokens.Styler, money spend, models []string) blocks.Block {
	return &ruleBlock{
		id:    chargeBlockID,
		style: style,
		rule: blocks.Ruled{
			Title: root.row.Name,
			Meta:  chargeCells(root, money, models),
			State: blocks.StateSettled,
		},
	}
}

// ruleBlock is one [blocks.Ruled] as a transcript block, and it is deliberately
// the smallest block in this package: a rule has no state, no fold, no pointer
// target and nothing that moves.
//
// It is NEVER finalized-with-a-cached-height in the way a settled card is,
// because its receipt is live — a call settling anywhere under the job changes
// the cells without changing a row of the tree — so the page rebuilds it on the
// same journal move that rebuilt everything else and this type keeps no state to
// go stale. What it does cache is the one string per width, which is the whole
// of its work.
type ruleBlock struct {
	id    string
	style *tokens.Styler
	rule  blocks.Ruled

	rows     []string
	width    int
	measured bool
}

var _ blocks.Block = (*ruleBlock)(nil)

func (b *ruleBlock) ID() string                { return b.id }
func (b *ruleBlock) IsFinalized() bool         { return true }
func (b *ruleBlock) SettledRows(width int) int { return len(b.Rows(width)) }
func (b *ruleBlock) Version() uint64           { return 0 }
func (b *ruleBlock) End() blocks.EndState      { return blocks.EndCompleted }

// Rows draws the rule and the one blank line §20 puts between blocks.
func (b *ruleBlock) Rows(width int) []string {
	if width < 1 {
		width = 1
	}
	if b.measured && b.width == width {
		return b.rows
	}
	b.rows = append(b.rows[:0], b.rule.Render(width, b.styler()), "")
	b.width, b.measured = width, true
	return b.rows
}

func (b *ruleBlock) styler() blocks.Styler {
	if b.style == nil {
		return blocks.Plain
	}
	return b.style
}

// -- the one progress line ------------------------------------------------------

// EXACTLY THREE DRESSED ELEMENTS PER RECORD PAGE (user review, 2026-08-11): one
// prompt card at the top, one living tree (or one trace) in the body, one result
// card at the bottom. Nothing else on the page wears a card.
//
// THE DEFECT THIS KILLS, from the reader's own screenshot: a planner's status
// progression — "reading the request", "exploring approaches · 1 of 3",
// "3 of 3", "setting working standards · 3 of 4", "4 of 4" — arrived as five
// journaled rows, and message.go's [messageBlock.dressWork] dresses each of them
// as a COMMITMENT CARD (§3's "one block per job, evolving in place"). In the
// conversation that is right, because the poll coalesces every one of them into
// the job's single block; on a record page there is no coalescer, so the page
// drew five stacked cards, each re-printing the same title, the same receipt and
// the same subtree. In the reader's words: "our goal is not to have multiple
// cards for the task — the tree is the main thing, and below each item in the
// tree we have some text or progress".
//
// So the record page coalesces them ITSELF, and to one LINE rather than to one
// card: the newest status wins and replaces in place, sitting between the prompt
// card and the body, at the dimmest tier with the §18.2 breathe in its marker
// while the job is still running. It is the same shape the chat card's phase row
// draws (message.go's phaseRows), which is the point — one progress grammar,
// two surfaces.

// recordStatusID is the progress line's block identity. It is STABLE across the
// rebuild that follows every journal move, which is what makes "replacing in
// place" true: the transcript's anchor and cache both key on it, so a status
// arriving does not move a row the reader is looking at.
const recordStatusID = "room-status"

// isMachineryRow says a journaled row is the work narrating itself.
//
// It is read off COLUMNS and one producer mark, never off a phrase (13.3.1). A
// row anchored to a node that the reader did not type, that carries no question
// and is not one of the three learning moments, is §1's "worker lifecycle,
// progress narration … the work record's business" — which on the record page
// means the progress LINE and not a block.
//
// The three exclusions are the three things that are somebody SPEAKING. A user's
// row is a steer. A row with options is a question, and amber means a human is
// needed — dropping one into a status line would hide the one row a reader must
// act on. A learning moment ([speaks]) is the machine telling the reader
// something about itself that it will act on next time, which §1 keeps in the
// conversation on purpose.
func isMachineryRow(message store.Message) bool {
	if strings.TrimSpace(message.NodeID) == "" || message.Role == store.RoleUser {
		return false
	}
	if message.QuestionSeq != 0 || len(message.Options) > 0 {
		return false
	}
	if isDelivery(message) && !speaks(message.Body) {
		// A delivery-shaped row from a job still running is that job's progress
		// (message.go's landed) and belongs here; one from a job that finished
		// is its answer and belongs on the result card, which took it already.
		return true
	}
	return !speaks(message.Body)
}

// recordStatus is the NEWEST machinery row's own words, or none.
//
// The row the RESULT CARD took is not a status and never was: it is the job's
// answer, dressed at the bottom of the page, and saying it twice on one screen
// is §19's own named defect.
func recordStatus(messages []store.Message, consumed string) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if !isMachineryRow(messages[i]) || messageID(messages[i].Seq) == consumed {
			continue
		}
		if body := flattenLine(messages[i].Body); body != "" {
			return body
		}
	}
	return ""
}

// recordStatusBlock is the page's one progress line.
type recordStatusBlock struct {
	style *tokens.Styler
	clock *blocks.Clock
	text  string
	// live says the job is still running, which is the only condition §18.2
	// allows the breathe under: "the thinking line while a model is working".
	live bool

	rows []string
}

var _ blocks.Block = (*recordStatusBlock)(nil)

func (b *recordStatusBlock) ID() string        { return recordStatusID }
func (b *recordStatusBlock) IsFinalized() bool { return !b.live }
func (b *recordStatusBlock) Version() uint64   { return 0 }
func (b *recordStatusBlock) SettledRows(width int) int {
	if b.live {
		return 0
	}
	return len(b.Rows(width))
}
func (b *recordStatusBlock) End() blocks.EndState { return blocks.EndCompleted }

// Rows draws the marker and the line, at the frame's own instant.
func (b *recordStatusBlock) Rows(width int) []string {
	if width < 1 {
		width = 1
	}
	mark := blocks.DefaultPulse.Frames[0]
	if b.live && b.clock != nil && !b.clock.Calm {
		mark = blocks.DefaultPulse.Frames[blocks.DefaultPulse.Index(b.clock)]
	}
	room := width - blocks.ContentEdge
	if room < 1 {
		return append(b.rows[:0], "")
	}
	text := blocks.Truncate(b.text, room)
	if b.style != nil {
		mark = b.style.Paint(mark, blocks.StateLive, blocks.HueAlive)
		text = b.style.PaintToken(text, tokens.TextTertiary)
	}
	// §20's gutter: the marker in cols 0–1, the words at the content edge.
	return append(b.rows[:0], mark+" "+text, "")
}

// -- the result card ------------------------------------------------------------

// resultBlockID is the bottom card's identity when the record had to build one.
// A card built from a journaled delivery keeps that message's own id, because
// the fold the reader opened on it must survive the next repaint (disclose.go).
const resultBlockID = "room-result"

// resultCard is the FINAL RESULT on its own grounded card (§4), and it exists
// only once the whole task has settled.
//
// A RUNNING JOB HAS NO RESULT AND MUST NOT WEAR ONE. §4's ground plus `▎` edge
// "mean a finished answer and nothing else may wear it", and message.go's
// `landed` is the same gate one surface over — a delivery-shaped row from a job
// still in flight is that job's progress, not its answer. So the bottom of a
// running page is the live tail of its work, which is exactly where the page
// opens.
//
// TWO SOURCES, ONE CARD. A job whose ending was announced has the delivery
// message, and that message is dressed by the one renderer that knows the card's
// anatomy ([messageBlock.dressDelivery]): the answer's lead line bright, the
// body one tier down, the artifact rows clickable, the receipt whole. A job
// whose ending was never announced keeps it in `nodes.summary` — and its error
// in `nodes.error` — so the same renderer is handed the same columns in a
// message shape it never journaled. That is not an invented row: every field of
// it is read off the node, and the alternative is the record hiding the one
// thing it was opened for.
func resultCard(root workRow, in recordInputs) (*messageBlock, string) {
	if !settledLife(root.row.Life) {
		return nil, ""
	}
	// THE NEWEST ONE, walked from the end. Every progress line a running job
	// posts is delivery-SHAPED by columns (isDelivery reads four of them and a
	// status row answers all four), so the first match down the list is the
	// job's first breath rather than its answer — and on a job that has now
	// settled, the last such row is the one that carries the result.
	for i := len(in.messages) - 1; i >= 0; i-- {
		message := in.messages[i]
		if !isDelivery(message) || strings.TrimSpace(message.NodeID) != root.node.ID {
			continue
		}
		return newMessageBlock(message, in.style, in.board), messageID(message.Seq)
	}
	body := workBody(root.node)
	if strings.TrimSpace(body) == "" {
		return nil, ""
	}
	// The synthesized carrier. Its columns are the node's, its role is the one
	// `announceNode` would have written, and it is never journaled — it exists
	// so the ONE delivery renderer can be asked the question rather than a
	// second one growing here (12.14: two renderings of one fact are two facts
	// waiting to disagree).
	message := store.Message{
		Role:   store.RoleSystem,
		NodeID: root.node.ID,
		Body:   body,
	}
	block := &messageBlock{id: resultBlockID, style: in.style, source: &message}
	block.dressDelivery(message, in.board)
	block.markLead()
	return block, ""
}

// settledLife says a job has stopped, whichever way it stopped. A failure is a
// result — §4's own "failed variant: coral edge + why" — and a page that showed
// a result card only for success would hide the ending a reader most needs.
func settledLife(life rail.Lifecycle) bool {
	switch life {
	case rail.LifeSettled, rail.LifeFailed, rail.LifeCancelled:
		return true
	}
	return false
}

// -- where the page opens ------------------------------------------------------

// anchorRecord puts the viewport where the page's own state says the reader is
// going: on the result card when the work has settled, at the live tail while it
// runs.
//
// IT HAPPENS ONCE PER ENTRY AND NEVER AGAIN. Manual scrolling is never fought
// (§5's "nothing sticky", 8.1.6's "a row moving under the eye that is reading
// it"), so this runs on the first paint of a room and is silent on every repaint
// after it — a job that settles while the reader is halfway up its trace does
// not yank them to the bottom.
func (a *App) anchorRecord(view *mainView) {
	if view == nil || view.transcript == nil || view.anchored {
		return
	}
	transcript := view.transcript
	id := resultAnchorID(transcript)
	if id == "" {
		// Nothing to land ON. A running record's resting place is its live tail
		// — which is where the transcript already is, because following is its
		// zero state — and that is the end of it. A SETTLED record whose result
		// has not been read back yet keeps following until it arrives, so the
		// answer is on screen the moment the trail lands rather than one scroll
		// below the fold forever.
		transcript.GotoBottom()
		view.anchored = !settledLife(view.card.Life)
		return
	}
	if !transcript.Following() {
		// The reader has already scrolled. Their place outranks ours, always
		// (§5's nothing-sticky, 8.1.6's row-under-the-eye).
		view.anchored = true
		return
	}
	index, ok := transcript.IndexOf(id)
	if !ok {
		return
	}
	view.anchored = true
	// A settled page opens ON the result card. The offset is the card's own
	// start, so the card's first row is the viewport's first row and the work
	// above it is one scroll away — which is the reordering the chronological
	// page traded for.
	transcript.Frame(a.now())
	transcript.ScrollTo(rowOfBlock(transcript, index))
}

// rowOfBlock is which document row one block starts on.
//
// The transcript keeps this arithmetic privately for its own layout and does not
// hand it out, so it is recomputed here from the same [blocks.Block.Rows] the
// layout measured — at the same width, so the two cannot disagree. It runs once
// per room entry and walks a list bounded by maxSubtreeRows.
func rowOfBlock(transcript *blocks.Transcript, index int) int {
	width := transcript.Width()
	if width < 1 {
		width = 1
	}
	row := 0
	for i := 0; i < index && i < transcript.Len(); i++ {
		row += len(transcript.Block(i).Rows(width))
	}
	return row
}

// resultAnchorID is the id of the block a settled page opens on, or "" when
// there is none.
func resultAnchorID(transcript *blocks.Transcript) string {
	for _, id := range []string{resultBlockID} {
		if _, ok := transcript.IndexOf(id); ok {
			return id
		}
	}
	// A journaled delivery keeps its message id, so the anchor is found by
	// walking the tail rather than by guessing the sequence: the result card is
	// the last block of a settled page by construction.
	if n := transcript.Len(); n > 0 {
		block, ok := transcript.Block(n - 1).(*messageBlock)
		if ok && block.card == dressDelivery {
			return block.ID()
		}
	}
	return ""
}

// -- drilling into one part ----------------------------------------------------

// drillInto opens one part's own record page, nested inside the page it was
// clicked from.
//
// A LEAF IS A RECORD LIKE ANY OTHER. The page a part opens is variant A of the
// same page — its charge, its execution, its result — and the reason it is worth
// a page rather than a fold is that a worker's trace is a document: §5's bounded
// boxes, batched tool rows and four voices need the whole lens, and 13.10's
// finding was precisely that a card which grew with its content stopped being a
// card.
//
// The page it came from is kept WHOLE, transcript and scroll offset and all, so
// walking back costs nothing and lands the reader exactly where they left. That
// is 8.1.6's rule read for navigation: the surface may not move what the reader
// was reading.
func (a *App) drillInto(node, name string) tea.Cmd {
	node = strings.TrimSpace(node)
	if node == "" || a.view == nil || a.view.kind != viewNode {
		return nil
	}
	parent := a.view
	// The offset the reader was at is latched HERE and not on the way back,
	// because by then the transcript has been rebuilt under a different page.
	if parent.transcript != nil {
		parent.offset = parent.transcript.YOffset()
	}
	transcript := blocks.New(80, 24)
	if parent.transcript != nil {
		transcript.SetSize(parent.transcript.Width(), parent.transcript.Height())
	}
	row := rail.Row{ID: node, Name: name, Life: a.source.rowLife(node)}
	a.view = &mainView{
		kind: viewNode, node: node, title: name,
		transcript: transcript, card: row, parent: parent,
	}
	a.pane.homes = nil
	a.pane.transcript = transcript
	a.paintRoom()
	a.shell.Invalidate()
	return tea.Batch(a.readNodeCmd(node, 0), a.readTraceCmd(node), a.readSubtreeCmd(node))
}

// drillBack walks one level out of a nested record and puts the reader back
// where they were: the same transcript, at the same offset.
//
// It answers whether it did anything, so the key ladder can fall through to
// whatever esc means when there is no level to leave (app.go's navigate).
func (a *App) drillBack() bool {
	if a.view == nil || a.view.parent == nil {
		return false
	}
	parent := a.view.parent
	a.view = parent
	a.pane.homes = nil
	a.pane.transcript = parent.transcript
	if parent.transcript != nil {
		// The page is repainted before the offset is restored, because the
		// record moved while the reader was one level down and the rows they
		// left may be a different height now. Restoring first would put them at
		// an offset measured against a document that no longer exists.
		a.paintRoom()
		parent.transcript.Frame(a.now())
		parent.transcript.ScrollTo(parent.offset)
	}
	a.shell.Invalidate()
	return true
}

// rowLife is the lifecycle the board recorded for one node, or queued when it
// has never heard of it. A drill door carries the row it was drawn from so the
// nested page's own card opens holding what the row above it knew (12.14).
func (s *scopeSource) rowLife(node string) rail.Lifecycle {
	if s == nil {
		return rail.LifeQueued
	}
	stored, ok := s.nodes[strings.TrimSpace(node)]
	if !ok {
		return rail.LifeQueued
	}
	switch stored.Status {
	case store.Done:
		return rail.LifeSettled
	case store.Failed:
		return rail.LifeFailed
	case store.Cancelled:
		return rail.LifeCancelled
	case store.Running:
		return rail.LifeWorking
	}
	return rail.LifeQueued
}

// -- one part's own record -----------------------------------------------------

// workRecordAt is [scopeSource.workRecord] for a node that is not a top-level
// task.
//
// A drilled-into part has no scope of its own — the board builds one scope per
// job, and a part is a ROW inside its job's — so the plain lookup answers
// nothing and the nested page would draw the empty-room card over work that
// demonstrably happened. What it does have is the row the tree drew it from,
// which is in exactly one of the scopes already built, so the fallback is a
// lookup and not a second walk.
func (s *scopeSource) workRecordAt(root string) []workRow {
	if record := s.workRecord(root); len(record) > 0 {
		return record
	}
	root = strings.TrimSpace(root)
	if s == nil || root == "" {
		return nil
	}
	node, known := s.nodes[root]
	if !known {
		return nil
	}
	for _, scope := range s.tasks {
		for _, row := range scope.Rows {
			if strings.TrimPrefix(row.ID, rowTaskPrefix) != root {
				continue
			}
			// The row is re-based to depth zero: it is the surface of its own
			// page now, and every renderer under it measures depth from the
			// record's first row.
			row.Depth = 0
			return []workRow{{row: row, node: node}}
		}
	}
	return nil
}

// traceFor is the recorder one node's page reads, following the SPLICE and not
// the node.
//
// trace.go documents why they differ: a planner splices every part of a job in
// one transaction, so every part shares one created sequence and therefore one
// recorder, held under the first node that claimed it. A nested page that asked
// for its own id would find nothing, which is the same silence the record page
// exists to end — so it asks for the file its work actually went into, and the
// interleaving of its siblings inside that file is a producer fact this surface
// does not get to hide.
func (s *scopeSource) traceFor(node string, traces map[string]nodeTrace) map[string]nodeTrace {
	node = strings.TrimSpace(node)
	if len(traces) == 0 || node == "" {
		return traces
	}
	if _, ok := traces[node]; ok {
		return traces
	}
	stored, known := s.nodes[node]
	if !known {
		return traces
	}
	for id, held := range traces {
		other, ok := s.nodes[id]
		if !ok || other.CreatedSeq != stored.CreatedSeq {
			continue
		}
		return map[string]nodeTrace{node: held}
	}
	return traces
}
