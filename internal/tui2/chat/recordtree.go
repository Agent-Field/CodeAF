package chat

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The living tree: what the middle of a GRAPH record is.
//
// §5b calls it by name — "tree as progress, not as a toggle" — and the record
// page is where that sentence gets its full measure. On the board a job's
// subtree is a two-level preview inside a two-line row; here it is the whole
// document: one row per part, on §20's ladder, with v1's connector guides
// saying who belongs to whom, and each row carrying its own receipt.
//
// WHY IT IS ONE BLOCK AND NOT ONE BLOCK PER ROW. §20: "a parent and its children
// are one tight unit — zero blank lines inside a block". A tree assembled from N
// transcript blocks would have to suppress the blank line each of them leaves,
// and would pay N cache entries to draw what is arithmetically one shape. More
// importantly it would be N live blocks: the transcript rebuilds every
// unfinalized entry on every frame (blocks/cache.go's refresh), so a spinner
// anywhere in the tree would make the whole list live one row at a time. One
// block is live exactly when something under it is running, and settles into the
// finalized cache the moment the last part stops.
//
// WHAT MOVES, AND ONLY WHAT §11 ALLOWS. A running row's state glyph is the
// braille spinner off the transcript's own clock; its elapsed cell counts. A
// queued row is dim and still, a settled row wears the dim `✓`, a failed row the
// coral `✕`. The one addition this wave makes is the PREVIEW LINE under a
// running row — the latest human line from that part's flight recorder, dim, one
// line, at the child indent — and it is deliberately NOT a fourth motion: the
// text changes because the recorder grew, which is the work itself moving, and
// the spinner on the row above it is what carries the motion. §11 stays at three.

// treeRow is one part as the record draws it.
//
// Everything on it is a fact the board already resolved (the row) or the ledger
// already answered (the receipt cells). Nothing here reads the store, for the
// reason record.go's whole header gives: the record and the rail are two
// renderings of one snapshot and must not be two opinions.
type treeRow struct {
	// node is the graph id. NEVER RENDERED (§14, 5.14) — it is the key the fold
	// map, the hit test and the drill door are all spelled in.
	node string
	name string
	life rail.Lifecycle
	// depth is the level under the record's own root: 1 for a part of the job,
	// 2 for a part of a part. The root itself is the page's top card and is not
	// a row here.
	depth int
	// guides says, for each ancestor level 1..depth-1, whether that ancestor
	// has a sibling still below it — which is the whole of what a `│` means.
	guides []bool
	// last marks the last child of its own parent, which is what turns `├─`
	// into `└─`.
	last bool
	// branch says this row has parts of its own, and is therefore a fold door
	// (§10) rather than a drill door.
	branch bool
	// hidden is how many rows a collapsed branch is holding, for its `▸ N`.
	hidden int
	// open is whether the branch is currently expanded.
	open bool

	// receipt is the static half of the row's telemetry — model word, burn,
	// money — composed once at build time, `·`-separated, dim.
	receipt string
	// since is when a RUNNING row's work started, so its elapsed cell can be
	// recomputed from the frame's own latched instant rather than from whenever
	// the last snapshot happened. Zero means the clock is unknowable and the
	// cell is absent (§16's EMPTINESS), never zero and never a guess.
	since time.Time
	// elapsed is a SETTLED row's measured wall, and hasElapsed says it could be
	// measured at all.
	elapsed    time.Duration
	hasElapsed bool
	// waits is what a queued row is sitting behind — the same `waits: <deps>`
	// spelling the rail and the work row use, one column over.
	waits []string
	// preview is the latest human line from this part's recorder, or "". It is
	// only ever filled for a running LEAF: a settled part's words are its
	// result, a queued part has said nothing, and a branch's work is its parts'
	// — so a preview on any of the three would be the surface narrating where
	// there is nothing of its own to narrate.
	preview string
}

// recordTreeID is the tree's block identity, and treeRowID is one row's — the
// key [App.folds] remembers a collapsed branch by, exactly as the trace rows are
// keyed (trace.go): the page rebuilds its whole block list on every journal
// move, so a flag on the row would be thrown away several times a second.
const recordTreeID = "room-tree"

func treeRowID(node string) string { return "room-tree-" + node }

// recordTreeBlock draws [treeRow]s as one tight unit.
//
//	├─ ✓ XhrSyn                              k3 · ~2.1k tok · $0.08 · 12s
//	├─ ⠸ H2                                  k3 · ~0.4k tok · $0.02 · 1m
//	│    reading internal/http2/frame.go
//	└─ ○ KeyCutter                           waits: H2
type recordTreeBlock struct {
	id    string
	style *tokens.Styler
	clock *blocks.Clock
	rows  []treeRow
	// live says something in the tree is still moving, which is what decides
	// whether this block is rebuilt every frame — and, through
	// [blocks.Transcript.LiveSeam], whether the window arms its animation clock
	// at all (poll.go's roomIsLive, the battery law).
	live bool

	// lines maps each rendered row back to the [treeRow] it belongs to, so a
	// click resolves against the layout the last frame actually produced rather
	// than against a map recorded during Render.
	lines    []int
	out      []string
	width    int
	measured bool
}

var _ blocks.Block = (*recordTreeBlock)(nil)

func (b *recordTreeBlock) ID() string { return b.id }

// IsFinalized is false exactly while something under the tree runs.
func (b *recordTreeBlock) IsFinalized() bool { return !b.live }

// SettledRows is zero while the tree is live and every row once it is not: a
// settled tree is bytes that will not move again.
func (b *recordTreeBlock) SettledRows(width int) int {
	if b.live {
		return 0
	}
	return len(b.Rows(width))
}

// Version never moves. The tree is rebuilt from the snapshot by paintRoom, not
// mutated in place, so a counter here would be a second story about the same
// bytes (8.1.1).
func (b *recordTreeBlock) Version() uint64 { return 0 }

func (b *recordTreeBlock) End() blocks.EndState { return blocks.EndCompleted }

// Rows draws the whole tree at this frame's instant.
func (b *recordTreeBlock) Rows(width int) []string {
	if width < 1 {
		width = 1
	}
	// A settled tree is measured once per width; a live one is redrawn every
	// frame, because that is what live means.
	if b.measured && b.width == width && !b.live {
		return b.out
	}
	b.out, b.lines = b.out[:0], b.lines[:0]
	for i := range b.rows {
		b.out = append(b.out, b.rowLine(b.rows[i], width))
		b.lines = append(b.lines, i)
		if preview := b.previewLine(b.rows[i], width); preview != "" {
			b.out = append(b.out, preview)
			b.lines = append(b.lines, i)
		}
	}
	// One trailing blank, which is §20's rhythm between top-level blocks said
	// from the side that owns it: the tree is one block and leaves one hole
	// after itself, never one between its own rows.
	b.out = append(b.out, "")
	b.lines = append(b.lines, -1)
	b.width, b.measured = width, true
	return b.out
}

// rowLine is one part: its guides, its state glyph, its name, its fold witness
// and its receipt.
func (b *recordTreeBlock) rowLine(row treeRow, width int) string {
	prefix := b.guides(row)
	room := width - blocks.Width(prefix)
	if room < 1 {
		return blocks.Truncate(prefix, width)
	}
	head := blocks.Header{
		Glyph: b.glyph(row),
		Title: row.name,
		State: blocks.StateSettled,
	}
	switch row.life {
	case rail.LifeWorking:
		head.GlyphHue, head.State = blocks.HueAlive, blocks.StateLive
	case rail.LifeSettled:
		head.GlyphHue = blocks.HueMoney
	case rail.LifeFailed, rail.LifeCancelled:
		head.GlyphHue = blocks.HueBroken
	}
	// §10's expand law wants a WITNESS and not a second target: the whole line
	// is the door, and the chevron only says which way it is currently facing.
	// The count is spelled in PARTS rather than in lines — a fold over a subtree
	// is holding work, and "lines" is the transcript's word for prose (§14).
	if row.branch {
		head.Hint = blocks.Disclose(row.open, row.hidden, "part", "parts")
	}
	head.Receipt = b.receiptOf(row)
	return prefix + head.Render(room, b.styler())
}

// glyph is the row's state mark — the spinner on a running row, the vocabulary's
// own glyph on every other.
//
// CALM WINS, exactly as the running trace row decides it (trace.go): a window
// asked to stop moving draws the still `◐` and keeps counting the elapsed cell
// beside it, because a still glyph next to a moving number is a row visibly
// alive without anything animating.
func (b *recordTreeBlock) glyph(row treeRow) string {
	if row.life != rail.LifeWorking || b.clock == nil || b.clock.Calm {
		return lifeGlyph(row.life)
	}
	return b.clock.Glyph()
}

// receiptOf is the row's dim telemetry: the cells composed at build time, plus
// the clock — which for a running row is recomputed from the frame's own instant
// so it TICKS between journal moves (§13: numbers tick).
func (b *recordTreeBlock) receiptOf(row treeRow) string {
	cells := make([]string, 0, 3)
	// A queued row says what it is behind instead of what it spent, because for
	// a row that has not run that is the whole of its receipt (§5b, and the
	// rail's own `waits:` one column over).
	if len(row.waits) > 0 {
		cells = append(cells, waitsWord+strings.Join(row.waits, ", "))
	}
	if row.receipt != "" {
		cells = append(cells, row.receipt)
	}
	if clock := b.clockCell(row); clock != "" {
		cells = append(cells, clock)
	}
	return strings.Join(cells, " "+tokens.GlyphSeparator+" ")
}

// clockCell is how long this row has been at it: live off the animation clock
// while it runs, measured once it has stopped, absent when neither is knowable.
func (b *recordTreeBlock) clockCell(row treeRow) string {
	if row.life == rail.LifeWorking && b.clock != nil && !row.since.IsZero() {
		since := b.clock.Now().Sub(row.since)
		if since < 0 {
			// A clock ahead of ours. Absent beats negative.
			return ""
		}
		return tokens.Elapsed(since)
	}
	if row.hasElapsed {
		return tokens.Elapsed(row.elapsed)
	}
	return ""
}

// previewLine is the running row's one-line window onto its own recorder.
//
// It is ellipsized to ONE LINE at the row's measure (§19: "sentence-length rows
// ellipsize at the row's measure — the full text lives in the detail page"),
// dim, at the child indent, and it exists only while the part is running.
func (b *recordTreeBlock) previewLine(row treeRow, width int) string {
	if row.life != rail.LifeWorking || strings.TrimSpace(row.preview) == "" {
		return ""
	}
	prefix := b.previewGuides(row)
	room := width - blocks.Width(prefix)
	if room < 1 {
		return ""
	}
	text := blocks.Truncate(flattenLine(row.preview), room)
	if b.style != nil {
		text = b.style.PaintToken(text, tokens.TextTertiary)
	}
	return prefix + text
}

// guides is a row's connector run: two cells per level, `│ ` for an ancestor
// that has more children below, two spaces for one that does not, and the row's
// own `├─` or `└─` last.
//
// TWO CELLS PER LEVEL IS §20 AND NOT A STYLE. The grid's indent step is 2 and
// its example puts a child's elbow in "the two cells before its edge"; §3 spells
// the connectors themselves. This is both read together: the elbow IS the two
// cells, so the state glyph lands on the ladder and the name lands at 2+2n.
func (b *recordTreeBlock) guides(row treeRow) string {
	var out strings.Builder
	out.Grow(2 * (row.depth + 1))
	for _, more := range row.guides {
		out.WriteString(b.chrome(guideCell(more)))
	}
	if row.last {
		out.WriteString(b.chrome(tokens.GlyphTreeLast + tokens.GlyphTreeDash))
	} else {
		out.WriteString(b.chrome(tokens.GlyphTreeBranch + tokens.GlyphTreeDash))
	}
	return out.String()
}

// previewGuides is the run under a row: the row's own level continues as a
// vertical when it has siblings below it, and the preview then hangs at the
// child edge (name column + one step).
func (b *recordTreeBlock) previewGuides(row treeRow) string {
	var out strings.Builder
	for _, more := range row.guides {
		out.WriteString(b.chrome(guideCell(more)))
	}
	out.WriteString(b.chrome(guideCell(!row.last)))
	// The two cells the state glyph occupied, and the two the name hangs from:
	// the preview sits one step INSIDE its subject, which is what "at the child
	// indent" means on this ladder.
	out.WriteString("    ")
	return out.String()
}

// guideCell is one ancestor level's two cells.
func guideCell(more bool) string {
	if more {
		return tokens.GlyphTreeVert + " "
	}
	return "  "
}

// chrome paints a connector run at the dimmest tier. Connectors are structure,
// which §12 gives to chrome and never to content.
func (b *recordTreeBlock) chrome(text string) string {
	if b.style == nil || strings.TrimSpace(text) == "" {
		return text
	}
	return b.style.PaintToken(text, tokens.TextTertiary)
}

func (b *recordTreeBlock) styler() blocks.Styler {
	if b.style == nil {
		return blocks.Plain
	}
	return b.style
}

// rowAt resolves a line INSIDE this block to the part drawn on it, which is the
// whole of the tree's contribution to the click layer. A preview line answers
// with its own row: §10's door is the whole line, and the line a part's own
// words are on is that part's line.
func (b *recordTreeBlock) rowAt(line int) (treeRow, bool) {
	if line < 0 || line >= len(b.lines) {
		return treeRow{}, false
	}
	i := b.lines[line]
	if i < 0 || i >= len(b.rows) {
		return treeRow{}, false
	}
	return b.rows[i], true
}

// flattenLine makes one line out of whatever the recorder wrote. A preview that
// could smuggle a second row into a block whose height the cache has already
// recorded would be a defect in the transcript, not in the text.
func flattenLine(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		text = strings.TrimSpace(text[:i])
	}
	return text
}

// -- building the tree ---------------------------------------------------------

// buildTree turns a record into rows: one per part, in the order the board holds
// them, with the guides and the fold state resolved.
//
// THE ORDER IS THE BOARD'S AND THEREFORE THE SPLICE'S. scope.go walks the DAG
// once per journal move and hands back rows in birth order with their depth
// already resolved; re-deriving either here would be a second opinion about one
// subtree read at a different time, which is the fault record.go's header names.
//
// open reports whether a branch the reader collapsed is collapsed. Nil means
// everything is open, which is what a test that renders a tree without an app
// gets — and it is the right default: a record is a document, and a document
// does not arrive folded shut.
func buildTree(record []workRow, money spend, models func(string) []string,
	preview func(workRow) string, now time.Time, open func(id string) bool) []treeRow {

	if len(record) < 2 {
		return nil
	}
	if open == nil {
		open = func(string) bool { return true }
	}
	// The parts, in board order, minus the root — which is the page's own top
	// card and is not a row in its own tree.
	parts := record[1:]
	depths := make([]int, len(parts))
	base := parts[0].row.Depth
	for i := range parts {
		depth := parts[i].row.Depth - base + 1
		if depth < 1 {
			depth = 1
		}
		depths[i] = depth
	}
	// A row is LAST at its level when no later row is at the same depth before
	// the tree climbs back out above it. That is the one fact a connector needs
	// and it is a property of the flattened list, so it is read off the list.
	last := make([]bool, len(parts))
	for i := range parts {
		last[i] = true
		for j := i + 1; j < len(parts); j++ {
			if depths[j] < depths[i] {
				break
			}
			if depths[j] == depths[i] {
				last[i] = false
				break
			}
		}
	}
	rows := make([]treeRow, 0, len(parts))
	// carry is the vertical-guide state of each ancestor level, indexed by
	// depth-1. It is walked forward with the list rather than recursed, because
	// the list is already flat and a second traversal would be a second shape.
	carry := make([]bool, 0, 8)
	for i := range parts {
		depth := depths[i]
		for len(carry) < depth {
			carry = append(carry, false)
		}
		carry = carry[:depth]
		carry[depth-1] = !last[i]
		guides := make([]bool, depth-1)
		copy(guides, carry[:depth-1])

		item := parts[i]
		row := treeRow{
			node:   item.node.ID,
			name:   item.row.Name,
			life:   item.row.Life,
			depth:  depth,
			guides: guides,
			last:   last[i],
			branch: i+1 < len(parts) && depths[i+1] > depth,
			waits:  item.row.WaitsOn,
		}
		row.open = !row.branch || open(treeRowID(row.node))
		row.receipt = treeCells(item, money, models)
		if item.row.Meta.HasElapsed {
			row.elapsed, row.hasElapsed = item.row.Meta.Elapsed, true
		}
		if row.life == rail.LifeWorking {
			row.since = startedAt(item, money, now)
			// THE PREVIEW BELONGS TO A LEAF. A branch is not doing work itself
			// — its parts are, and their own rows say so one indent in — and on
			// the shared recorder a splice writes (trace.go) a branch and its
			// running child would answer with the SAME line, printed twice, one
			// under the other. §15's rule against saying a thing twice on one
			// screen decides it.
			if preview != nil && !row.branch {
				row.preview = preview(item)
			}
		}
		rows = append(rows, row)
	}
	// A collapsed branch takes its whole subtree off the page and says how many
	// rows it is holding (§10's witness, §20's "never a silent cut").
	return foldSubtrees(rows)
}

// foldSubtrees drops the descendants of every collapsed branch and records the
// count on the branch that swallowed them.
func foldSubtrees(rows []treeRow) []treeRow {
	out := make([]treeRow, 0, len(rows))
	for i := 0; i < len(rows); {
		row := rows[i]
		if row.open {
			out = append(out, row)
			i++
			continue
		}
		end := i + 1
		for end < len(rows) && rows[end].depth > row.depth {
			end++
		}
		row.hidden = end - i - 1
		out = append(out, row)
		i = end
	}
	return out
}

// treeCells is a part's own dim receipt, in §5b's order and vocabulary: the
// model that did it in HUMANE WORDS (never a slug), what it burned marked as the
// estimate it is, and what it cost.
//
// The clock is deliberately not here — it is the one cell that moves, so it is
// composed per frame ([recordTreeBlock.clockCell]) and appended last, which also
// puts it at the end of the receipt where §16 puts the figure a reader scans.
//
// EVERY CELL IS ABSENT WHEN IT IS UNKNOWABLE (§16). A part that has not been
// billed carries no burn and no money, and that is a different and truer picture
// than `$0.00`.
func treeCells(item workRow, money spend, models func(string) []string) string {
	cells := make([]string, 0, 3)
	if models != nil {
		cells = append(cells, modelWords(models(item.node.ID))...)
	} else if word := modelWord(item.row.Meta.Model); word != "" {
		cells = append(cells, word)
	}
	if rollup, ok := money.rollup(item.node.ID); ok && rollup.Runs > 0 {
		if burned := rollup.PromptTokens + rollup.CompletionTokens; burned > 0 {
			cells = append(cells, tokens.GlyphEstimate+tokens.Count(int64(burned))+" tok")
		}
		if rollup.Cost > 0 {
			cells = append(cells, tokens.Money(rollup.Cost))
		}
	} else if item.row.Meta.HasCost {
		cells = append(cells, tokens.Money(item.row.Meta.Cost))
	}
	return strings.Join(cells, " "+tokens.GlyphSeparator+" ")
}

// startedAt is when a running part's work began, in the order the record can
// know it: the ledger's own wall if it has one, and otherwise the instant the
// board's elapsed reading counts back to.
//
// The second form is not an approximation dressed as a fact. The board measured
// the elapsed at the snapshot, and `now` is the instant that snapshot was folded
// in, so counting back from it recovers the start the board was measuring from —
// and pinning the START rather than the DURATION is what lets the cell tick on
// the animation clock without a journal move (§13).
func startedAt(item workRow, money spend, now time.Time) time.Time {
	if rollup, ok := money.rollup(item.node.ID); ok && !rollup.Started.IsZero() {
		return rollup.Started
	}
	if item.row.Meta.HasElapsed && !now.IsZero() {
		return now.Add(-item.row.Meta.Elapsed)
	}
	return time.Time{}
}

// treeIsLive says something in the tree is still moving, which decides whether
// the block is rebuilt per frame and whether the window arms its clock at all.
func treeIsLive(rows []treeRow) bool {
	for i := range rows {
		if rows[i].life == rail.LifeWorking {
			return true
		}
	}
	return false
}

// -- the recorder preview ------------------------------------------------------

// recorderLine is the latest HUMAN line one part's recorder holds.
//
// It does not fork a parser and could not afford to: [parseTrace] is the one
// reading of the recorder's grammar in this window, it already classifies every
// line into 5.5's four voices, and it already folds machine-authored runs into
// the bounded rows they are ([clampStreams]). So the preview is the tail of that
// same parse walked backwards to the first thing a person could read — a
// thought, a steer, the recorder's own note, or the salient INPUT of the call
// that is in flight right now.
//
// A MACHINE ROW IS NOT A PREVIEW. clampStreams already collapses an NDJSON feed
// or a dumped log into one traceMachine row; drawing that as "what this worker
// is doing" would put a line nobody wrote where the reader is looking for the
// one line somebody did.
func recorderLine(text string) string {
	events := parseTrace(text)
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].kind == traceMachine {
			continue
		}
		if gist := strings.TrimSpace(events[i].gist); gist != "" {
			return gist
		}
	}
	return ""
}

// recordPreview is [recorderLine] asked of the recorder a part actually writes
// to.
//
// THE RECORDER IS KEYED BY THE SPLICE AND NOT BY THE NODE, which trace.go's
// [App.traceNodes] documents at length: a planner splices every part of a job in
// one transaction, so every part shares one created sequence and therefore ONE
// recorder, held under the first node that claimed the sequence. A preview that
// asked for its own node id would come back empty for every part but one — which
// is the same class of silence this whole record page exists to end.
func recordPreview(record []workRow, traces map[string]nodeTrace) func(workRow) string {
	if len(traces) == 0 {
		return nil
	}
	// Which node holds the recorder for each created sequence, in board order,
	// so a shared file lands under the job it belongs to.
	holder := make(map[int64]string, len(record))
	for i := range record {
		seq := record[i].node.CreatedSeq
		if _, taken := holder[seq]; !taken {
			holder[seq] = record[i].node.ID
		}
	}
	return func(item workRow) string {
		id := item.node.ID
		if held, ok := holder[item.node.CreatedSeq]; ok {
			id = held
		}
		trace, ok := traces[id]
		if !ok {
			return ""
		}
		return recorderLine(trace.text)
	}
}

// -- the page's own hit test ---------------------------------------------------

// recordTreeAt resolves a viewport row of the open record to the part drawn on
// it. It is the transcript's own arithmetic asked twice — which block, and which
// line inside it — and it records nothing during a render.
func (a *App) recordTreeAt(y int) (*recordTreeBlock, treeRow, bool) {
	if a.view == nil || a.view.kind != viewNode || a.view.transcript == nil {
		return nil, treeRow{}, false
	}
	index, line, ok := a.view.transcript.BlockAtScreenRow(y)
	if !ok {
		return nil, treeRow{}, false
	}
	tree, ok := a.view.transcript.Block(index).(*recordTreeBlock)
	if !ok {
		return nil, treeRow{}, false
	}
	row, ok := tree.rowAt(line)
	return tree, row, ok
}

// recordForThreadAt resolves a viewport row of the open record to the
// attribution row's thread, when that is what is drawn there.
//
// It is the same arithmetic [App.recordTreeAt] runs, asked about a different
// block type, so the paint and the pointer can never disagree about which row
// is which — the page's own transcript is what answers both.
func (a *App) recordForThreadAt(y int) (string, bool) {
	if a.view == nil || a.view.kind != viewNode || a.view.transcript == nil {
		return "", false
	}
	index, _, ok := a.view.transcript.BlockAtScreenRow(y)
	if !ok {
		return "", false
	}
	row, ok := a.view.transcript.Block(index).(*forThreadBlock)
	if !ok || row.session == "" {
		return "", false
	}
	return row.session, true
}

// recordPointer is what a click on the record's tree means (§10, and the drill
// the reader asked for).
//
// TWO DOORS AND THE ROW ITSELF SAYS WHICH. A BRANCH has parts of its own, so the
// whole line is the fold door and the chevron is its witness — §10 word for
// word. An ATOMIC LEAF has no subtree to open, so the line is the way IN: it
// opens that worker's own record page, which is the same page one level down.
// Nothing on a tree row is inert and nothing needs a second target.
func (a *App) recordPointer(y int) tea.Cmd {
	// THE ATTRIBUTION ROW IS A DOOR, and it is asked first because it is the one
	// row on this page that leaves it (5.3: "activating it jumps to the
	// thread"). It cannot overlap a tree row — the two are different block types
	// at different places in the page — so the order is habit rather than
	// necessity: the row that navigates away is the one worth resolving before
	// the rows that rearrange what is on screen.
	if session, ok := a.recordForThreadAt(y); ok {
		return a.switchThread(session)
	}
	_, row, ok := a.recordTreeAt(y)
	if !ok {
		return nil
	}
	if row.branch {
		a.toggleTreeFold(row)
		return nil
	}
	return a.drillInto(row.node, row.name)
}

// toggleTreeFold flips one branch and remembers that the reader flipped it.
//
// The state lives in [App.folds] beside every other per-row disclosure answer,
// for the reason disclose.go gives: the page rebuilds its whole block list on
// every journal move, so a flag on the row would not survive the next breath of
// the job it describes.
func (a *App) toggleTreeFold(row treeRow) {
	if a.folds == nil {
		a.folds = make(map[string]bool, 4)
	}
	id := treeRowID(row.node)
	a.folds[id] = !a.foldOpenDefault(id)
	// Which rows EXIST changed, so this is a rebuild and not a re-render. The
	// stamp fingerprints the JOURNAL and nothing in the journal moved: the
	// reader did.
	if a.view != nil {
		a.view.stamp = ""
	}
	a.paintRoom()
	a.shell.Invalidate()
}

// foldOpenDefault is [App.foldOpen] with the tree's own default, which is the
// opposite of a receipt's: a record's tree arrives OPEN, because the tree is the
// page's middle and a page that opened folded shut would be the empty room this
// whole record exists to replace.
func (a *App) foldOpenDefault(id string) bool {
	if open, chosen := a.folds[id]; chosen {
		return open
	}
	return true
}

// The word for what a collapsed branch is holding used to live here, as
// [treeHiddenWord]. It is [blocks.Disclose]'s `unit` argument now — §14's
// vocabulary is still this surface's to state (a job has PARTS, never nodes and
// never workers), and the GRAMMAR around it belongs to the one renderer every
// other door in the product goes through.
