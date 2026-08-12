package chat

import (
	"image"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/modelui"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/reltime"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The work page: everything this resident is DOING.
//
// Three bands, one grammar. `working` / `recent` / `history` are the jobs — §6's
// sidebar read at page altitude, and the SAME rows the rail draws, from the same
// scope source. `watching` is the standing charters and `services` is the
// promoted processes; both moved here from the notebook the day the split was
// approved (notebook-split.md §2: doing vs knowing), because a charter is a
// thing that fires and a service is a thing that runs, and neither is something
// the resident has learned.
//
// THE ROW IS TWO LINES, and that is the whole of this file's shape (§5b, user
// feedback on the single-line form). A name on the left and `1m · $0.09` flush
// against column eighty is a matching exercise the eye performs on every row:
// the receipt is the row's own fact, so it is drawn where the row's own name is,
// one line down and one tier quieter. What was the right-edge column is now the
// INDENT column, and it is still shared — the glyph column, the name column and
// the receipt column sit at one x across the jobs, the charters and the services
// alike, so scanning down still reads like a table without being one (§16's
// ALIGNMENT).
//
// The other properties are the ones that keep it a page rather than a second
// rail, and they are unchanged:
//
//   - THE SIDEBAR IS NOT DRAWN BESIDE IT. Two copies of one list on one screen
//     is §15's same-fact-twice, and the board is the fuller copy — so the work
//     page hides the rail outright (tui2.Shell.SetRailHidden) and takes the
//     columns.
//   - IT HOLDS NO STATE THE STORE DOES NOT. What lives here is a cursor, the
//     history fold, whichever detail page is open, and the rectangle the pane was
//     last given. Every cell it draws came from a row the source built or from
//     the reads boardreads.go takes once per journal move.
//   - IT HAS NO SCROLL on the list. A wheel moves the SELECTION, exactly as it
//     does over the rail, and the window of lines on screen is derived from where
//     the cursor is. A DETAIL PAGE does scroll, because it is a document.
//   - EVERY DOOR IS THE ONE THE SIDEBAR AND THE PALETTE ALREADY USE. Opening a
//     job is [App.jumpTo] on the rail row's own id — the JumpToRoom path — so a
//     job entered from the board lands exactly where walking there with j and k
//     would have landed.
//
// THE CLICK LAW DIFFERS PER BAND, deliberately and visibly. A JOB opens its
// ROOM, because a job has a conversation and a record and the room is where both
// live. A CHARTER or a SERVICE opens a DETAIL PAGE inside this pane, because
// neither has a room to open — a charter's history is five judgments and a
// service's is ten lines of log, and inventing a transcript for them would be
// inventing a conversation nobody had. [boardLine.target] carries which, one
// value per row, so no reader of this file has to infer it from the kind.

// Board layout constants, with the arithmetic that produced them.
const (
	// THE GUTTER IS TWO CELLS AND BOTH ARE CHROME (§20). Column 0 carries the
	// row's own marker — its state glyph, a fold's chevron — and column 1
	// carries the ▎ accent rail when the row is selected and a space when it is
	// not. Names begin at column 2: [blocks.ContentEdge], the one edge every
	// surface in this window hangs from.
	//
	// WHAT THIS FIXED. The board used to spend the whole gutter on the rail
	// alone, carry its glyph as the first SPAN — so the glyph landed at the
	// content edge and pushed the name a step past it to column 4. The work page
	// and the transcript, one keypress apart in the same window, drew a job's
	// name at two different columns, which is the contradiction §20 exists to
	// end. The glyph moved into the gutter where every other surface puts a row's
	// marker, and the names came back to column 2.
	//
	// NOTHING SHIFTS WHEN THE CURSOR MOVES, which is the property the old
	// arrangement bought with that extra step and this one keeps for free: the
	// marker is always at column 0 and the words always at column 2, so the rail
	// arriving at column 1 inks a cell rather than moving a row. The marker
	// column is not a named constant because it is not a choice: it is column
	// zero, and [App.boardRow] is the one place that knows it.
	boardNameCol = blocks.ContentEdge
	boardMetaCol = boardNameCol
	// boardStep is one level of descent inside a job's live subtree:
	// [blocks.IndentStep], the house two-space rhythm. A child's GLYPH lands in
	// the two marker cells before its own edge, which is what makes indent read
	// as descent without a connector.
	boardStep = blocks.IndentStep
	// boardRecentCap is how many settled jobs the `recent` section carries
	// before the rest becomes history — §6's "recent (settled, dim, ~5)".
	boardRecentCap = 5
	// boardHistoryCap bounds the history section. It is not a policy about what
	// exists: the fold row's count is read off the whole answer, so a store with
	// four hundred jobs still SAYS four hundred and lists the newest of them.
	boardHistoryCap = 200
	// boardTreeDepth is how far a live subtree is drawn under its job before the
	// rest becomes one `…` (§5b). Two levels is the depth at which a tree is
	// still a shape and not a document: the parts, and what each part is doing.
	boardTreeDepth = 2
)

// The section words. §15 admits at most one faint lowercase word per section,
// and only where position alone is ambiguous — which is exactly the case here:
// a settled job and a running one are the same shape, and so are a charter and
// a service, so nothing but the word says which band a reader is in.
const (
	boardWorkingWord  = "working"
	boardRecentWord   = "recent"
	boardHistoryWord  = "history"
	boardWatchingWord = "watching"
	boardServicesWord = "services"
)

// boardEmptyNote is what a board with no work at all says. An unwired surface
// and an empty one must never look alike (12.10), and a blank page is what an
// unwired one looks like.
const boardEmptyNote = "no work yet — ask for something and it shows up here"

// boardNeedsYou is the attention cell's word. It is the census's slot spent on
// the one fact that outranks a census: a blocked human is the most expensive
// state this product has (5.9), and `1?` in a chrome-tier column would say it
// at the volume of a count.
const boardNeedsYou = "needs you"

// boardSeparator is the one separator every receipt on this page uses.
const boardSeparator = " " + tokens.GlyphSeparator + " "

// boardKind is what one line of the board IS. It decides the anatomy; what the
// line OPENS is [boardLine.target], which is a separate axis on purpose — a
// charter's name line and a job's name line are the same shape and different
// doors.
type boardKind uint8

const (
	// boardBlank is a spacing row (§16's padding rhythm: a blank above a group
	// word, none below; one blank between blocks).
	boardBlank boardKind = iota
	// boardWord is a section's faint lowercase word.
	boardWord
	// boardEntry is the first line of a two-line row: gutter, glyph, name.
	boardEntry
	// boardMeta is the second line: the dim receipt under the name.
	boardMeta
	// boardTwig is one member of a live job's subtree.
	boardTwig
	// boardFold is the history fold — a door onto the rest of the list (§10).
	boardFold
	// boardNote is the empty state's one sentence.
	boardNote
)

// boardTarget is what enter or a click on this line does. It is the click law
// written down rather than inferred.
type boardTarget uint8

const (
	// boardOpensNothing is a line a cursor never rests on.
	boardOpensNothing boardTarget = iota
	// boardOpensRoom is a job: [App.jumpTo] on the rail row's id.
	boardOpensRoom
	// boardOpensCharter opens the charter detail page inside this pane.
	boardOpensCharter
	// boardOpensService opens the service detail page inside this pane.
	boardOpensService
	// boardOpensFold performs in place: the history fold (§10).
	boardOpensFold
)

// boardSpan is one painted run of a line: text, and the tier it is drawn at. A
// line is assembled as spans and painted span by span, which is the ordering
// every fitted surface in this tree uses — measuring a painted string is
// measuring its escape sequences.
type boardSpan struct {
	text string
	tier tokens.Token
}

// boardLine is one drawn row, derived on demand and never recorded during a
// render (Part 2's anti-pattern 14). The paint and the pointer both call
// [App.boardLines], so a click cannot land on a row the paint dropped.
type boardLine struct {
	kind boardKind
	// target is what this line opens, and door() is true exactly when it is not
	// [boardOpensNothing].
	target boardTarget
	// id is the handle the target takes — the rail row id for a job, the charter
	// or service id for a detail. It is the one thing on the line that is never
	// drawn (5.14).
	id string
	// block is the door this line BELONGS to, one-based, or 0 for a line that
	// belongs to no door. A receipt line and a subtree twig are not doors of
	// their own, but a pointer landing on one has landed on its job — §3's
	// "the whole block is a click target".
	block int
	// row is the job this line was built from, kept so a test and the history
	// read can ask a line what job it draws. Empty for charters and services.
	row rail.Row
	// indent is where the line's first span begins — its content edge.
	indent int
	// mark is the row's own marker: the state glyph of an entry, the chevron of
	// a fold. It hangs in the GUTTER at column 0 and is not a span, because it
	// is not content — §20 puts a row's marker in cols 0–1 and the row's words
	// at column 2, and a marker carried as span zero is a marker that pushes the
	// words along in front of it, which is how this page came to draw its names
	// at column 4.
	//
	// Empty is a row with no marker of its own: a receipt line, a section word,
	// a subtree twig (whose glyph sits in ITS OWN two marker cells, inside the
	// spans, one rung in).
	mark     string
	markTier tokens.Token
	// spans are the line's painted runs, left to right.
	spans []boardSpan
	// hits are the click targets INSIDE this line — the detail pages' verb chips
	// and the trail's back door. A list row needs none: the whole line (indeed
	// the whole block) is one target there, and [boardLine.block] carries it.
	hits []boardHit
	// spin marks the line whose marker is the live spinner cell (§11's one
	// moving glyph). The painter replaces it per frame; nothing else on the page
	// moves except numbers, which §11 permits outright.
	spin bool
}

// door reports whether this line is a target: something a cursor rests on and
// enter opens.
func (l boardLine) door() bool { return l.target != boardOpensNothing }

// boardState is the work page's own state: where the cursor is, whether history
// is open, which detail page is open, and the rectangle the pane was last drawn
// at.
//
// Every read on it is a CACHE and not state: each is stamped with the journal
// position it was taken at, so it ages with the journal like everything else on
// this surface and never with the frame.
type boardState struct {
	// cursor is which door is selected, counted over the doors alone — a
	// spacing row and a section word are not places a cursor can be.
	cursor int
	// open is the history fold (§10). It is the reader's answer and it is the
	// only thing on this page that survives a rebuild.
	open bool
	// hover is the one-based door under the pointer, or 0 for none.
	hover int

	// width and height are the rectangle the pane was last told about — the one
	// number a pane may keep, because it IS what the pane was given and nothing
	// else can know it (panes.go's statusPane says the same of its own width).
	width, height int

	// The reads, all stamped together. See boardreads.go.
	history  []rail.Row
	usage    map[string]jobSpend
	models   map[string][]string
	charters []boardCharter
	services []boardService
	stamp    int64
	read     bool

	// detail is the open detail page, or the zero value for the list. cursorWas
	// is the door the list was on when it opened, so esc restores the reader to
	// the row they left (notebook-split.md §3: "scrolled to the row it left").
	detail    boardDetail
	cursorWas int
}

// -- the projection ----------------------------------------------------------

// boardLines is the whole page as drawable rows, in order.
//
// It reads the SOURCE and never the store for the job half. [scopeSource.home]
// is the rail's own home scope, rebuilt once per journal move, and the job rows
// on it are exactly the rows the sidebar draws — so the two surfaces cannot
// disagree about a job's name, glyph, census or receipt, because there is one
// row and both of them draw it.
func (a *App) boardLines() []boardLine {
	working, recent := a.boardJobs()
	// History is everything the ledger can still name that the two live sections
	// are not already showing — measured against what the board DRAWS and not
	// against what the rail holds. The distinction is the whole of a real gap:
	// `recent` is capped, so a settled job past the cap is on the rail's home
	// scope and on no section of this page, and a history list that excluded it
	// for being on the rail would have made it appear nowhere at all.
	shown := make(map[string]bool, len(working)+len(recent))
	for _, row := range append(append([]rail.Row(nil), working...), recent...) {
		shown[row.ID] = true
	}
	history := a.boardHistoryRows(shown)

	page := boardPage{lines: make([]boardLine, 0, 32)}
	page.jobs(a, boardWorkingWord, working, true)
	page.jobs(a, boardRecentWord, recent, false)

	if len(history) > 0 {
		page.gap()
		page.push(a.boardFoldLine(history))
		if a.board.open {
			for _, row := range history {
				page.job(a, row, false)
			}
		}
	}
	page.watching(a)
	page.services(a)

	// The note appears only when the WHOLE page is empty. A window with no jobs
	// but with a charter and a service is visibly wired, and 12.10's rule is
	// about telling an empty surface apart from a broken one — not about
	// announcing every band that happens to have nothing in it.
	if len(page.lines) == 0 {
		page.push(boardLine{kind: boardNote, indent: boardNameCol,
			spans: []boardSpan{{text: boardEmptyNote, tier: tokens.TextTertiary}}})
	}
	return page.lines
}

// boardPage is the line list under construction. It exists so the blank-line
// rhythm (§16's PADDING RHYTHM) is stated once instead of at every append: a
// blank above a group word, none below it, and one between blocks.
type boardPage struct {
	lines []boardLine
	// open is how many doors have been pushed so far, which is also the block
	// number the next entry takes. It is counted rather than re-derived because
	// every entry would otherwise walk the whole list to find out where it is.
	open int
}

func (p *boardPage) push(line boardLine) {
	if line.door() {
		p.open++
	}
	p.lines = append(p.lines, line)
}

// gap opens one blank line, and only where there is something above to separate
// from. A page that began with an empty row would have spent its first line on
// nothing.
func (p *boardPage) gap() {
	if len(p.lines) > 0 {
		p.push(boardLine{kind: boardBlank})
	}
}

// word writes a section's faint lowercase word with its blank above.
func (p *boardPage) word(text string) {
	p.gap()
	// The word titles the NAMES under it, so it hangs at their edge (§20). It
	// used to sit at the glyph column, which put a section's word one step left
	// of every row it announced.
	p.push(boardLine{kind: boardWord, indent: boardNameCol,
		spans: []boardSpan{{text: text, tier: tokens.TextTertiary}}})
}

// next is the block number the door about to be pushed will take, one-based.
// The block number and the cursor's door index are the same ordinal, which is
// what lets a receipt line and a subtree twig name their job with one small int.
func (p *boardPage) next() int { return p.open + 1 }

// jobs writes one job section: its word, then each job as a block.
func (p *boardPage) jobs(a *App, word string, rows []rail.Row, live bool) {
	if len(rows) == 0 {
		return
	}
	p.word(word)
	for i, row := range rows {
		if i > 0 {
			// §5b: a blank line between jobs. The two-line row needs the blank
			// to be a row rather than a pair of rows, and it is the same blank
			// §16 puts between blocks everywhere else.
			p.push(boardLine{kind: boardBlank})
		}
		p.job(a, row, live)
	}
}

// job writes one job's block: the name line, its receipt, and — while it is
// working — its live subtree.
func (p *boardPage) job(a *App, row rail.Row, live bool) {
	block := p.next()
	glyph, tier := boardGlyph(row)
	spinning := live && row.Attention() == rail.AttnWorking
	p.push(boardLine{
		kind: boardEntry, target: boardOpensRoom, id: row.ID, block: block, row: row,
		indent: boardNameCol, spin: spinning, mark: glyph, markTier: tier,
		spans: []boardSpan{
			{text: row.Name, tier: tokens.ResolveToken(tokens.HueNone, row.State())},
		},
	})
	if spans := a.boardReceipt(row); len(spans) > 0 {
		p.push(boardLine{kind: boardMeta, block: block, row: row, indent: boardMetaCol, spans: spans})
	}
	if !live {
		// Settled jobs stay collapsed to their two-line row (§5b). A finished
		// job's parts are a document, and the document is its record.
		return
	}
	for _, twig := range a.boardTwigs(row) {
		twig.block = block
		p.push(twig)
	}
}

// watching writes the standing charters band.
func (p *boardPage) watching(a *App) {
	charters := a.boardCharters()
	if len(charters) == 0 {
		return
	}
	p.word(boardWatchingWord)
	for i, charter := range charters {
		if i > 0 {
			p.push(boardLine{kind: boardBlank})
		}
		block := p.next()
		glyph, tier := boardCharterGlyph(charter)
		p.push(boardLine{
			kind: boardEntry, target: boardOpensCharter, id: charter.ID, block: block,
			indent: boardNameCol, mark: glyph, markTier: tier,
			spans: []boardSpan{
				{text: charter.Invariant, tier: tokens.TextPrimary},
			},
		})
		if spans := charter.receipt(); len(spans) > 0 {
			p.push(boardLine{kind: boardMeta, block: block, indent: boardMetaCol, spans: spans})
		}
	}
}

// services writes the promoted-process band.
func (p *boardPage) services(a *App) {
	services := a.boardServices()
	if len(services) == 0 {
		return
	}
	p.word(boardServicesWord)
	for i, service := range services {
		if i > 0 {
			p.push(boardLine{kind: boardBlank})
		}
		block := p.next()
		glyph, tier := boardServiceGlyph(service)
		p.push(boardLine{
			kind: boardEntry, target: boardOpensService, id: service.ID, block: block,
			indent: boardNameCol, mark: glyph, markTier: tier,
			spans: []boardSpan{
				{text: service.Name, tier: tokens.TextPrimary},
			},
		})
		if spans := service.receipt(); len(spans) > 0 {
			p.push(boardLine{kind: boardMeta, block: block, indent: boardMetaCol, spans: spans})
		}
	}
}

// boardJobs splits the rail's job cards into the two live sections: what is
// moving, and what has settled recently.
//
// The order inside each half is the source's own — newest first, held stable by
// the rail's merge (7.2) — and the split is by lifecycle rather than by any
// second opinion this file forms about a job.
func (a *App) boardJobs() (working, recent []rail.Row) {
	if a.source == nil {
		return nil, nil
	}
	rows := a.source.home.Rows
	for i := range rows {
		row := rows[i]
		if !strings.HasPrefix(row.ID, rowTaskPrefix) {
			// Rooms, the `+ new` door and 5.24's group lid are navigation and
			// not work. The board is the WORK view; the rooms live in the hug's
			// own places and in the palette.
			continue
		}
		// A QUESTION OUTRANKS A LIFECYCLE. A job that has stopped and is still
		// holding a question open is not "recent" — a blocked human is the most
		// expensive state this product has (5.9), and burying it under the
		// settled word is the one place this page could hide the thing it exists
		// to surface.
		if row.Life.Terminal() && row.Questions == 0 {
			if len(recent) < boardRecentCap {
				recent = append(recent, row)
			}
			continue
		}
		working = append(working, row)
	}
	return working, recent
}

// boardTwigs is a live job's subtree as board lines: TREE AS PROGRESS (§5b).
//
// It is the scope the rail ALREADY BUILT for this job (scope.go's taskScope),
// not a second read and not a second opinion — the same rows, the same glyphs,
// the same depths the room would draw. Row 0 of that scope is the job's own
// surface and is skipped: it is the line above.
//
// The depth cap is what keeps it a shape rather than a document. Past
// [boardTreeDepth] the tree becomes one `…` at the cap's own indent, which is
// §16's one ellipsis grammar answering "there is more of this, and it is
// downward" without pretending to say how much.
func (a *App) boardTwigs(row rail.Row) []boardLine {
	if a.source == nil {
		return nil
	}
	scope, ok := a.source.Scope(row.ID)
	if !ok || len(scope.Rows) < 2 {
		return nil
	}
	out := make([]boardLine, 0, len(scope.Rows)-1)
	deeper := false
	for _, member := range scope.Rows[1:] {
		// The scope's own depth is zero-based from the job's parts; the board
		// reads it one-based, because on this page the parts are already one
		// level under the name above them.
		depth := member.Depth + 1
		if depth > boardTreeDepth {
			deeper = true
			continue
		}
		glyph, tier := boardGlyph(member)
		// §20's ladder: depth-n content hangs at 2+2n and its glyph occupies
		// the two marker cells before that edge. So a first-level twig marks at
		// col 2 and reads at col 4, under the job name at col 2.
		indent := blocks.MarkerCol(depth)
		out = append(out, boardLine{
			kind: boardTwig, indent: indent,
			spin: member.Attention() == rail.AttnWorking,
			spans: []boardSpan{
				{text: glyph, tier: tier},
				{text: " ", tier: tokens.TextTertiary},
				// A twig is DIM: it is the shape of the work, not the work's
				// name, and §16 allows three tiers on a surface. The job's name
				// above it is the one that gets the ink.
				{text: member.Name, tier: tokens.TextTertiary},
			},
		})
	}
	if deeper {
		out = append(out, boardLine{
			// The tail sits at the deepest DRAWN twig's own marker column — it
			// summarizes those rows, so it stands with them rather than one
			// rung below anything on screen.
			kind: boardTwig, indent: blocks.MarkerCol(boardTreeDepth),
			spans: []boardSpan{{text: tokens.GlyphEllipsis, tier: tokens.TextTertiary}},
		})
	}
	return out
}

// boardReceipt is §5b's receipt line, in the order it fixed and with every part
// present only when it is known (§16's EMPTINESS: absent renders as absence,
// never as zero and never as a guess).
//
//	live count or census · wall-clock · money · model words · ~NK tok
//
// `needs you` in amber REPLACES the census when a question is open, because the
// slot is the row's one summary cell and a blocked human outranks a count.
//
// ONE COLUMN, ONE MEANING. The clock is always how long the work OCCUPIED —
// never "how long ago it was" for the settled rows and "how long it has been at
// it" for the live ones, which is two questions answered in one unlabelled cell.
// It is spelled by [reltime.Elapsed] on every row, live or settled: one
// formatter, one reading. A live row's number therefore TICKS, which §11 permits
// in as many words ("numbers tick"), and the cells after it move by at most one
// cell at a rung boundary — that is a number rolling over, not the every-frame
// width instability §18 bans, which is why the glyph beside it had to be braille
// and this cell did not.
func (a *App) boardReceipt(row rail.Row) []boardSpan {
	out := make([]boardSpan, 0, 9)
	add := func(text string, tier tokens.Token) {
		if text == "" {
			return
		}
		if len(out) > 0 {
			out = append(out, boardSpan{text: boardSeparator, tier: tokens.TextTertiary})
		}
		out = append(out, boardSpan{text: text, tier: tier})
	}
	if row.Questions > 0 {
		add(boardNeedsYou, tokens.Amber)
	} else {
		add(boardCensus(row), tokens.TextTertiary)
	}
	if elapsed, ok := a.boardElapsed(row); ok {
		add(reltime.Elapsed(elapsed), tokens.TextTertiary)
	}
	if row.Meta.HasCost {
		add(tokens.Money(row.Meta.Cost), tokens.TextTertiary)
	}
	for _, word := range a.boardModels(row) {
		add(word, tokens.TextTertiary)
	}
	if total, ok := a.boardTokens(row); ok {
		add(tokens.GlyphEstimate+tokens.Count(total)+" tok", tokens.TextTertiary)
	}
	return out
}

// boardCensus is the receipt's first cell: the LIVE COUNT while anything is
// moving, and the census otherwise.
//
// Two readings of one slot, and the reason they are not one reading is that
// they answer at different volumes. `2 running` is what a person asks of work
// that is happening; `2◐ 1✓` is the shape of work that has stopped happening,
// and it is the rail's own spelling of it (rail.StateCounts.Cell), so the board
// and the sidebar spell the census once. An empty census draws nothing, which is
// the whole of what a single-part job has to say about its shape (§14).
func boardCensus(row rail.Row) string {
	if running := row.Meta.Counts.Running; running > 0 {
		return strconv.Itoa(running) + " running"
	}
	return row.Meta.Counts.Cell()
}

// boardFoldLine is the history section's one row: the expandable word, its
// count, and the ROLLUP of everything behind it.
//
// The rollup is money and only money, deliberately. §13 says a collapsed parent
// carries the rollup and that the clock is wall-clock rather than a sum; a
// hundred jobs that ran over three weeks have no wall between them to report,
// and adding their durations would be the one arithmetic that section forbids.
// Money does add up, so money is what the fold row says.
func (a *App) boardFoldLine(history []rail.Row) boardLine {
	// ONE DISCLOSURE GRAMMAR ([blocks.DiscloseSection]). A section's door is the
	// section's own heading, so the word is the subject and the count is the
	// parenthetical about it — the same row the record's tree branches and the
	// transcript's folded turns wear, one shape further out.
	//
	// The row is composed by the grammar and PLACED by this surface, which is
	// why it is cut back apart: the chevron rides in the gutter's glyph cell and
	// the word starts at the content edge, two columns rather than one span. The
	// alternative was spelling the chevron here as well, and a second spelling is
	// the thing the grammar exists to remove.
	door := blocks.DiscloseSection(a.board.open, boardHistoryWord, len(history))
	glyph, word, _ := strings.Cut(door, " ")
	// The chevron is the state's witness and the whole line is the door (§10),
	// so both are drawn at the tier an affordance is allowed to live at (5.22's
	// amendment: never permanently in the dimmest one).
	// The chevron rides in the gutter's glyph cell and the word starts at the
	// content edge, so `history` lines up with `working` and `recent` instead of
	// standing one step in behind its own affordance.
	spans := []boardSpan{{text: word, tier: tokens.TextSecondary}}
	total, billed := 0.0, false
	for i := range history {
		if history[i].Meta.HasCost {
			total, billed = total+history[i].Meta.Cost, true
		}
	}
	if billed {
		spans = append(spans,
			boardSpan{text: boardSeparator, tier: tokens.TextTertiary},
			boardSpan{text: tokens.Money(total), tier: tokens.TextTertiary})
	}
	return boardLine{kind: boardFold, target: boardOpensFold, indent: boardNameCol,
		mark: glyph, markTier: tokens.TextSecondary, spans: spans}
}

// -- the cursor --------------------------------------------------------------

// boardDoors is the lines a cursor may rest on, as indices into the line list.
func boardDoors(lines []boardLine) []int {
	out := make([]int, 0, len(lines))
	for i := range lines {
		if lines[i].door() {
			out = append(out, i)
		}
	}
	return out
}

// boardClamp keeps a cursor inside a list of doors. It CLAMPS rather than
// wrapping for the reason [rail.Model.Move] does: a list that wraps teleports
// the eye from the bottom to the top, and the gesture the reader meant was
// "further down".
//
// It is a function of its arguments rather than a method, so the PAINT can ask
// it what the cursor means on a list that has shrunk without the paint writing
// anything back — a render that repaired the model would be exactly the
// render-mutates-model habit pane.go's contract forbids.
func boardClamp(cursor, doors int) int {
	if doors <= 0 {
		return 0
	}
	if cursor < 0 {
		return 0
	}
	if cursor >= doors {
		return doors - 1
	}
	return cursor
}

// boardSelect moves the cursor to a door, clamped against the list as it stands
// right now.
func (a *App) boardSelect(index int) {
	a.board.cursor = boardClamp(index, len(boardDoors(a.boardLines())))
}

// boardKey is the work page's keyboard, and it is the rail's grammar reached on
// a page (5.15: "the same rows, with the same keys and the same selection
// semantics, as a full-pane list").
//
// The guards that decide whether it is asked at all live one level up, in
// [App.pageKey]: no overlay, no draft in progress, and the page holding the
// keyboard. This function answers only what the vocabulary is.
//
// ESC IS CLAIMED ONLY WHILE A DETAIL PAGE IS OPEN, and never otherwise. The page
// is the outermost thing esc undoes (§7, App.navigate) and that must keep being
// true; but a detail page is a lens INSIDE the board, so the innermost thing
// closes first. Returning false when no detail is open is what leaves today's
// behaviour exactly as it was.
func (a *App) boardKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if a.page != pageBoard {
		return nil, false
	}
	key := msg.String()
	if a.board.detail.open() {
		return a.boardDetailKey(key)
	}
	switch key {
	case "j", "down":
		a.boardMove(1)
		return nil, true
	case "k", "up":
		a.boardMove(-1)
		return nil, true
	case "home", "g":
		a.boardMove(-len(boardDoors(a.boardLines())))
		return nil, true
	case "end", "G":
		a.boardMove(len(boardDoors(a.boardLines())))
		return nil, true
	case "enter":
		return a.boardEnter(a.board.cursor), true
	}
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		// A board is a numbered list to the eye whether or not it draws the
		// numbers, and reaching row seven by pressing j six times is the
		// interaction a list is supposed to save you (scopeKey's own note).
		a.boardMove(int(key[0]-'1') - a.board.cursor)
		return nil, true
	}
	return nil, false
}

// boardMove slides the cursor and repaints.
func (a *App) boardMove(delta int) {
	a.boardSelect(a.board.cursor + delta)
	a.shell.Invalidate()
}

// boardEnter opens the door the cursor is on, and WHICH door it is decides what
// opening means — the click law, in one switch.
//
// A JOB goes through [App.jumpTo] — the rail's own SelectID, which is the path
// the sidebar's enter, the palette's JumpToRoom and a click on a card all take —
// so entering a job from the board lands in exactly the room walking there would
// have opened, with the same breadcrumb and the same composer binding. A CHARTER
// or a SERVICE opens its detail page inside this pane and never a room: neither
// has a conversation, and taking a reader to the chat page for one would be the
// surface inventing a place. The fold row performs instead of navigating, and
// performing is all it does: §10's whole line is the door, and the door opens
// and shuts in place.
func (a *App) boardEnter(index int) tea.Cmd {
	lines := a.boardLines()
	doors := boardDoors(lines)
	if index < 0 || index >= len(doors) {
		return nil
	}
	line := lines[doors[index]]
	switch line.target {
	case boardOpensFold:
		a.board.open = !a.board.open
		a.boardSelect(index)
		a.refresh()
		return nil
	case boardOpensCharter, boardOpensService:
		a.openBoardDetail(line.target, line.id, index)
		return nil
	case boardOpensRoom:
		if line.id == "" {
			return nil
		}
		// The room is a CHAT-page thing: entering a job from any page opens its
		// room as today, and the trail replaces the tabs while the reader is in
		// it.
		a.showPage(pageThread)
		return a.jumpTo(line.id)
	}
	return nil
}

// -- the pane ----------------------------------------------------------------

// pagePane is a swapped-in lens's place in the shell — the board's, and the
// notebook's. It is thin for the reason [scopePane] is: it resolves a cell to a
// row and decides nothing about what pointing at one MEANS.
//
// One type for both pages rather than two, because a page is not a kind of
// thing here: it is a rectangle that draws lines and answers a pointer, and the
// difference between the two lives entirely in the closures.
type pagePane struct {
	// render paints the page. It is a function so the pane never learns what a
	// job or a belief is.
	render func(width, height int) string
	// point is a click or a wheel notch, already resolved to a pane-local point.
	point func(msg tea.MouseMsg, local image.Point) tea.Cmd
	// hover marks the row under the pointer. It returns whether the frame moved
	// and never a command — the hover door stays side-effect free (13.14).
	hover func(local image.Point, inside bool) bool
}

var (
	_ tui2.Pane      = (*pagePane)(nil)
	_ tui2.PaneMouse = (*pagePane)(nil)
	_ tui2.PaneHover = (*pagePane)(nil)
)

// Render draws the page at the rectangle it was given.
func (p *pagePane) Render(width, height int) string {
	if p == nil || p.render == nil || width <= 0 || height <= 0 {
		return ""
	}
	return p.render(width, height)
}

// Mouse hands the gesture on.
func (p *pagePane) Mouse(msg tea.MouseMsg, local image.Point) tea.Cmd {
	if p == nil || p.point == nil {
		return nil
	}
	return p.point(msg, local)
}

// Hover marks the row under the pointer.
func (p *pagePane) Hover(local image.Point, inside bool) bool {
	if p == nil || p.hover == nil {
		return false
	}
	return p.hover(local, inside)
}

// boardPoint is 13.18's law on a page: clicks go where they point.
//
// One click on a row opens it, because a hand that put the pointer on a row and
// pressed has already looked — the select-then-enter split is the KEYBOARD's,
// and it earns its second key there because an arrow is how a keyboard looks
// around. A wheel moves the SELECTION rather than a viewport, because the list
// has no scroll of its own to move; on a DETAIL page the wheel scrolls, because
// there the page is a document.
func (a *App) boardPoint(msg tea.MouseMsg, local image.Point) tea.Cmd {
	if a.board.detail.open() {
		return a.boardDetailPoint(msg, local)
	}
	switch event := msg.(type) {
	case tea.MouseWheelMsg:
		switch event.Button {
		case tea.MouseWheelUp:
			a.boardMove(-1)
		case tea.MouseWheelDown:
			a.boardMove(1)
		}
		return nil
	case tea.MouseClickMsg:
		if event.Button != tea.MouseLeft {
			return nil
		}
		door, ok := a.boardDoorAt(local.Y)
		if !ok {
			// The empty space under a short board is not the job at the top of
			// it. A click there is a click on nothing, and answering it with a
			// navigation would be the surface guessing.
			return nil
		}
		a.boardSelect(door)
		return a.boardEnter(door)
	}
	return nil
}

// boardHover lights the row under the pointer one tier (13.14: never a band,
// and nothing that hovers is inert).
func (a *App) boardHover(local image.Point, inside bool) bool {
	if a.board.detail.open() {
		return false
	}
	door := 0
	if inside {
		if at, ok := a.boardDoorAt(local.Y); ok {
			door = at + 1
		}
	}
	if a.board.hover == door {
		return false
	}
	a.board.hover = door
	return true
}

// boardDoorAt resolves a pane-local row to the door drawn on it, against the
// window the last frame actually produced.
//
// THE WHOLE BLOCK IS THE DOOR (§3). A pointer that landed on a job's receipt
// line or on one of its subtree twigs landed on the job: those lines carry the
// job's block number precisely so a hand does not have to hit the one line in
// three that happens to be the name.
func (a *App) boardDoorAt(y int) (int, bool) {
	lines := a.boardLines()
	top := boardTop(lines, a.board.cursor, a.board.height)
	index := top + y
	if y < 0 || index < 0 || index >= len(lines) {
		return 0, false
	}
	if block := lines[index].block; block > 0 {
		return block - 1, true
	}
	if !lines[index].door() {
		return 0, false
	}
	for door, at := range boardDoors(lines) {
		if at == index {
			return door, true
		}
	}
	return 0, false
}

// -- the paint ---------------------------------------------------------------

// boardTop is the first line on screen, derived from the cursor rather than
// stored.
//
// THE LIST HAS NO SCROLL STATE, and this is what stands in for one. The window
// is a PAGE of lines — the door under the cursor decides which page, and the
// lines inside it do not move while the cursor walks them. A rule that kept the
// cursor a fixed distance from the edge would move every row on every keystroke,
// which is the one motion 8.1.6 forbids: a row moving under the eye that is
// reading it.
func boardTop(lines []boardLine, cursor, height int) int {
	if height <= 0 || len(lines) <= height {
		return 0
	}
	doors := boardDoors(lines)
	if len(doors) == 0 {
		return 0
	}
	top := (doors[boardClamp(cursor, len(doors))] / height) * height
	if max := len(lines) - height; top > max {
		top = max
	}
	return top
}

// renderBoard paints the page.
func (a *App) renderBoard(width, height int) string {
	a.board.width, a.board.height = width, height
	// THE ANIMATION CLOCK IS LATCHED HERE, because while the board is the lens
	// the transcript is not being drawn and nothing else latches it. One clock
	// for the window (blocks.Transcript.SetClock says so in as many words), so
	// two live surfaces are phase-locked and a repaint inside one step is
	// byte-identical (8.1.3).
	clock := a.transcript.Clock()
	clock.Latch(a.now())
	if a.board.detail.open() {
		return a.renderBoardDetail(width, height)
	}

	lines := a.boardLines()
	top := boardTop(lines, a.board.cursor, height)
	doors := boardDoors(lines)
	// The selection is marked on the DOOR'S OWN LINE and nowhere else. A rail
	// down the whole block would be four marks for one selection, and 5.21 gives
	// the gutter one job: saying which row enter would open.
	selected := -1
	if len(doors) > 0 {
		selected = doors[boardClamp(a.board.cursor, len(doors))]
	}
	spinner := boardSpinner(clock)

	out := make([]string, 0, height)
	for i := top; i < len(lines) && len(out) < height; i++ {
		line := lines[i]
		// HOVER PROMOTES THE WHOLE BLOCK, because the whole block is one target
		// (§3) and a pointer that lit one line of four would be an affordance
		// disagreeing with its own hit test.
		lit := line.block > 0 && line.block == a.board.hover
		out = append(out, a.boardRow(line, width, i == selected, lit, spinner))
	}
	return strings.Join(out, "\n")
}

// boardSpinner is this frame's live glyph, or "" when nothing may move.
//
// §11 permits exactly three motions and this is the first of them: the braille
// spinner on a RUNNING ROW. Under Calm it answers "", and the caller draws the
// static state glyph instead — a frozen braille frame would be a spinner that
// had stopped rather than a row that never spins, and the state glyph is what
// the row means when nothing is allowed to move. The clock keeps ticking either
// way, so the elapsed reading in the receipt still ages: reduced motion is a
// promise about MOVEMENT, not about staleness.
func boardSpinner(clock *blocks.Clock) string {
	if clock == nil || clock.Calm {
		return ""
	}
	return blocks.Spinner[clock.Frame(len(blocks.Spinner))]
}

// boardRow paints one line: the gutter, the indent, and the line's spans.
//
// The row is assembled PLAIN and painted span by span, which is the ordering
// every fitted surface in this tree uses — measuring a painted string is
// measuring its escape sequences.
func (a *App) boardRow(line boardLine, width int, selected, hovered bool, spinner string) string {
	if line.kind == boardBlank || width <= 0 {
		return ""
	}
	var out strings.Builder
	x := 0
	add := func(text string, tier tokens.Token) {
		if text == "" || x >= width {
			return
		}
		if room := width - x; blocks.Width(text) > room {
			text = blocks.Truncate(text, room)
			if text == "" {
				return
			}
		}
		if hovered {
			// 13.14's hover law: one tier brighter under the pointer, never a
			// band. The whole block rises together because the whole block is
			// one target.
			tier = tokens.Promote(tier)
		}
		out.WriteString(a.boardPaint(text, tier))
		x += blocks.Width(text)
	}

	// THE GUTTER IS TWO CELLS AND BOTH ARE CHROME (§20).
	//
	// Column 0 is the row's own marker. Column 1 is the accent rail when the row
	// is selected and a space when it is not — the selection is marked with the
	// rail rather than with a band, the same mark at the same tier the rail
	// package draws when it cannot raise one, for the same reason: a page whose
	// only statement of "you are here" was a background would have none at
	// `--color none`.
	//
	// The rail sits at column 1 rather than column 0 because that is what an
	// ACCENT EDGE is — a bar hard against the content it marks (§4, and
	// blocks.CardBlock draws its own the same way) — and because column 0 is
	// already spoken for by the state glyph, which is where every other surface
	// in this window puts a row's marker. Nothing moves when the cursor moves:
	// the marker is always at 0 and the words are always at 2, selected or not.
	if line.mark != "" {
		mark := line.mark
		if line.spin && spinner != "" {
			// §18's width law is why this substitution is legal at all: every
			// braille frame is one cell under both shipping rulers, so the name
			// beside it does not dance while the glyph turns.
			mark = spinner
		}
		add(mark, line.markTier)
	}
	if gap := blocks.ContentEdge - 1 - x; gap > 0 {
		add(strings.Repeat(" ", gap), tokens.TextTertiary)
	}
	if selected && x == blocks.ContentEdge-1 {
		add(tokens.GlyphAccentRail, tokens.TextSecondary)
	}
	if gap := line.indent - x; gap > 0 {
		add(strings.Repeat(" ", gap), tokens.TextTertiary)
	}

	for _, span := range line.spans {
		add(span.text, span.tier)
	}
	return out.String()
}

// boardGlyph is a row's state glyph and its colour.
//
// It is [rail.Attention] verbatim — the same shape for the same state on every
// surface in the product (§12) — with one difference from the rail's own
// glyphToken, and it is a difference of PURPOSE rather than of vocabulary: the
// identity pastel a 28-column card wears answers "which job is this" for a
// reader who cannot see the whole name (5.16). A page shows every name in full,
// so the pastel would be an answer to a question the page has already answered,
// and §12's "glyphs carry only state" is what is left.
func boardGlyph(row rail.Row) (string, tokens.Token) {
	att := row.Attention()
	if hue := att.Hue(); hue != tokens.HueNone {
		return att.Glyph(), tokens.ResolveToken(hue, row.State())
	}
	// Queued and paused: not moving and not done. Chrome tier, because a bright
	// ○ on twenty pending rows is twenty claims on the eye that nothing has
	// earned (render.go's own words for the same cell).
	return att.Glyph(), tokens.TextTertiary
}

// boardModels is the deduped humane model words for a job — WHO DID THE WORK.
//
// The words are [modelui.ModelWord]'s, never provider slugs (5.10, and §5b says
// so again for this row): `sonnet`, not `anthropic/claude-sonnet-4.6`. The
// dedupe happens AFTER the shortening, because two slugs from one family shorten
// to one word and a receipt that said `sonnet · sonnet` would be counting
// bindings out loud.
func (a *App) boardModels(row rail.Row) []string {
	slugs := a.board.models[strings.TrimPrefix(row.ID, rowTaskPrefix)]
	if len(slugs) == 0 {
		return nil
	}
	out := make([]string, 0, len(slugs))
	seen := make(map[string]bool, len(slugs))
	for _, slug := range slugs {
		word := strings.TrimSpace(modelui.ModelWord(slug))
		if word == "" || seen[word] {
			continue
		}
		seen[word] = true
		out = append(out, word)
	}
	return out
}

// boardPaint draws one span, or returns it unchanged for a surface built
// without a profile (the golden harness and every headless test).
func (a *App) boardPaint(text string, tier tokens.Token) string {
	if a.style == nil || text == "" {
		return text
	}
	return a.style.PaintToken(text, tier)
}
