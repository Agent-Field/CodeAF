package tui3

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// The render core is two passes and one cache.
//
// PASS 1 — an entry renders ITSELF into rows and remembers them. It re-renders
// when its own text changes or the width does, and at no other time; a frame
// that touches a settled paragraph is a frame that wrapped text nobody
// re-typed.
//
// PASS 2 — [app.layout] joins those rows into the screen list, and it is the
// ONLY place a blank row is ever emitted. That is the whole fix for the gaps
// this surface used to grow: every entry politely left a line above itself, two
// polite entries left two, and nobody owned the result. Entries no longer get a
// vote.
//
// The frame joins [app.window]'s slice of that list and nothing else — no
// transcript rebuild, no re-wrap per keystroke.

// hitKind is what a visible row answers to a click.
type hitKind uint8

const (
	hitNone     hitKind = iota
	hitTool             // a tool call: click expands that call inline
	hitFold             // the "N earlier tool calls" line: click expands the turn
	hitCaption          // one step heading: click opens that step's calls
	hitWorkFold         // one completed turn's folded machinery
	hitMore             // the "… N more lines" foot of a capped expansion: click lifts the cap
	// hitBrief is the door under a node's folded instruction (brieffold.go):
	// click opens the rest of it, click again folds it back. It is a hit of its
	// own rather than another hitMore because the two answer the same gesture
	// with different things — hitMore lifts a cap and can never put it back,
	// while a fold is a thing a person opens AND shuts.
	hitBrief
	// hitPictures expands the attached pictures without folding the message.
	hitPictures
	// hitPictureOriginal makes the preview itself a full-quality file action.
	hitPictureOriginal
	hitTask // a task proposal (task.go): click opens its brief
	// hitDone is a landed task's card (taskdone.go): click opens its full
	// context, enter opens the node's room, ctrl+o is the key the card itself
	// names. It is a hit of its own rather than another hitTask because the two
	// blocks answer the same gestures with different things — one is a question
	// that can still be answered, the other is a record that cannot.
	hitDone
	// hitSettle is a landed card's ANSWERS row: the choices a task that finished
	// with nobody able to check it offers (tasksettle.go). It is a hit of its own
	// rather than another hitDone because the two rows answer the same gesture
	// with different things — one expands a record, the other decides about work
	// — and it needs the COLUMN as well as the row, the way hitChoice does.
	hitSettle
	hitHarness
	// hitRewind is the rewind mode's cut line (rewind.go): a click on it commits
	// the cut it is drawn at. It is the one hit on this surface that belongs to a
	// row nothing in the conversation produced — the line is drawn between two
	// blocks, and it exists only while the mode is up.
	hitRewind
	// hitThread is a line of a thread card (teamthreadcard.go): the words of a
	// manager's message or of a member's answer, which a press lays out in
	// full and a second press folds again. The row's open field says whose.
	hitThread
)

// row is one visible screen row and what it points at. It is the single
// mapping from screen geometry to the conversation: the frame joins row.text,
// the mouse hit-tests row.entry, and ↑/↓ walk the same list. Two sources of
// truth for "which entry is this row" is how a click lands on the wrong call.
type row struct {
	text  string
	entry int // index into app.entries; -1 for a blank or the fold line
	hit   hitKind
	turn  int // the turn a fold line folds
	// activity says this row already carries the running turn's sign of life.
	// The footer reads the actual drawing so an opened or absent compact block
	// cannot suppress the only remaining indication of work.
	activity bool
	// Inline waiting owns only a sign of life; detailed phase information stays
	// with the footer until the reader opens the full transcript.
	inlineWait bool
	// links are the task references drawn in this row's own columns
	// (markdown.go). They are the one thing on the transcript a click resolves
	// by COLUMN rather than by row, and they are recorded here for the reason
	// the strip's chips and the status row's model segment record theirs: the
	// geometry is written where it is decided, because a hit-test that
	// recomputed it would be measuring a row the frame has not drawn.
	links []taskLink
	// foot is the affordance under a markdown table that was cut, on the one row
	// that carries it (mdtable.go). It is the second target this surface resolves
	// by column, and it is recorded here for the reason the links above it are.
	foot tableFoot
	// keep is where a running bash row's `click to background` clause is, or will
	// be on the hover that reveals it (toolview.go). It is the third transcript
	// target resolved by column; the one-frame-ahead geometry is what lets a
	// pointer land straight on the hidden clause and light only those words.
	keep hudSpan
	// Picture controls retain their index and original-file action through gutter layout.
	pictureIndex int
	pictureOpen  hudSpan
	// open is a [hitThread] row's message, by team and entry id.
	open string
}

// toolWindow is how many of a turn's tool calls stay on screen. Three is the
// number a person can hold without reading: the call that is running and the
// two it followed.
const toolWindow = 3

// deck is ONE LIST OF BLOCKS BEING LAID OUT, and the fold state that belongs to
// it. There are two of them on this surface and there is not going to be a
// third: the conversation ([app.conversation]) and a task's page, which is the
// same list built from a node's journal and its live lane (room.go).
//
// It exists because a room MUST render exactly like the conversation, and the
// only way to guarantee that is for both to go through one renderer. A room
// that drew its own lines was a second rendering of the same four kinds of
// block, and it diverged the way a second rendering always does: no pictures on
// a person's message, no reasoning, no expansion on a call, no markdown on an
// answer. Everything below takes the deck rather than reaching for [app.entries]
// so that a gap between the two cannot be reintroduced by a renderer that
// forgot which list it was drawing.
//
// The entries are a SLICE and the fold map is a POINTER, which is what makes a
// deck a view rather than a copy: [app.entryRows] writes each entry's row cache
// through it, and [app.unfold] writes the map.
//
// EVERY DIFFERENCE BETWEEN TWO PAGES IS ONE FIELD, and it is [deck.lens]
// (lens.go). It used to be three bools and an int added one at a time — a
// clock, a shows-work, a tool tail — so "how does a room differ from the
// conversation" was a question with no answer short of grepping the files that
// read them. A posture is now declared in one literal and the renderers read
// the posture.
type deck struct {
	entries  []entry
	unfolded map[int]bool
	workOpen map[int]bool
	captions []caption
	capOpen  map[int]bool
	// lens is the page's posture: what folds, where the receipts land, whether
	// the session's clock runs over this list (lens.go).
	lens        lens
	runningTurn int
	// col is the live token column this page carries, or nil for a page
	// with none to carry — a run's read-only transcript (roomorch.go). It is a
	// pointer to the feed's own so the drawing reads the pair in motion, and it
	// is on the deck rather than found through the lens because WHICH page is
	// being drawn is the deck's whole job (tokencol.go).
	col *tokenCol
}

// foldWindow is how many of a folded cluster's calls this deck keeps on screen: the
// lens's own tail where it declares one, [toolWindow] otherwise. It is the ONE
// place the two are reconciled, so a renderer never has to know which list it
// is drawing.
//
// It is the app's method rather than the deck's because a room's tail is its
// VIEW'S HEIGHT and the view is the app's (lens.go's [lens.toolTail]): a number
// frozen into the deck at build time would be the height of the frame before
// the resize that is being drawn.
func (a *app) foldWindow(d deck) int {
	if d.lens.toolTail == nil {
		return toolWindow
	}
	return d.lens.toolTail(a)
}

// conversation is the deck the transcript draws.
func (a *app) conversation() deck {
	running := 0
	if a.state == stateWorking {
		running = a.turn
	}
	return deck{entries: a.entries, unfolded: a.unfolded, workOpen: a.workOpen, capOpen: a.capOpen,
		lens: participantLens, runningTurn: running, col: &a.col}
}

// bodyDeck is the deck the BODY REGION is drawing right now — the room's page
// while one is open, the conversation otherwise (view.go's [app.bodyRows] makes
// the same choice about the frame).
//
// Every gesture that names a row by index resolves through here: a click, the
// hover, the fold key, the thinking key. An index is only meaningful against the
// list it was taken from, and the list a person is pointing at is the one on
// screen.
func (a *app) bodyDeck() deck {
	// A NODE'S TRANSCRIPT INSIDE A RUN'S PAGE IS ITS OWN DECK: the rows on
	// screen are that journal's, so the doors that act on "the entry under the
	// click" — open a call, toggle a thought — must act on those entries and
	// not on the room's or the conversation's (roomorch.go's
	// [app.orchTranscriptDeck]).
	if run := a.orchOf(); run != nil && run.transcript != "" {
		return a.orchTranscriptDeck()
	}
	if a.room != nil {
		return a.room.deck()
	}
	return a.conversation()
}

// visible returns the row list, rebuilding it only when something changed.
//
// The dirty flag is the whole of the repaint discipline. A streamed delta
// mutates the entry's text but does NOT set it — the frame clock does, once per
// [frameInterval] — so a hundred deltas in a second cost one hundred string
// appends and thirty layouts, not a hundred layouts.
func (a *app) visible(width int) []row {
	if a.rows == nil || a.rowsWidth != width || a.dirty {
		a.rows = a.layout(width)
		a.rowsWidth = width
		a.dirty = false
		a.builds++
	}
	return a.rows
}

// layout is THE SPACING LAW (docs/CHAT-V3.md D11), and it is a law because it
// is enforced in exactly one place. Every blank row on this surface is emitted
// by the four rules below and by nothing else — no entry appends one, no
// renderer leaves one behind (see [trimBlanks]):
//
//   - ONE blank before a tool cluster that directly follows text. A cluster
//     that answers the person's own message gets none: the calls ARE the reply
//     starting, and a gap there would read as a pause that did not happen.
//   - ZERO between the lines of a cluster — a cluster is one thing.
//   - ONE blank after a cluster, before the text that follows it.
//   - ONE blank before each user message: the turn boundary, the only
//     structural silence this surface has.
//   - ONE blank above THE ANSWER of a turn that did work (hierarchy.go's
//     [answerBreath]). The rule above it covers the commonest shape and only
//     that shape — an answer after a cluster — while a turn that thought and
//     then answered, and a turn whose whole machinery collapsed into one chip,
//     both put the answer hard against the row above it. The silence belongs to
//     the ANSWER rather than to whatever preceded it. A turn with no work in it
//     is given nothing and renders exactly as it did before the rule existed.
//
// Two rules can ask for the same gap — a cluster ending a turn, then the next
// user message — and a gap asked for twice is still one gap, which is why each
// block asks once, before it draws. Nothing is ever emitted at the top of the
// transcript.
func (a *app) layout(width int) []row {
	// THE READING GUTTER IS TAKEN OUT FIRST AND GIVEN BACK LAST (gutter.go).
	// Everything between these two lines lays out against the narrower column,
	// so a block built to the frame's whole width is never shoved past the edge
	// it was measured for; the pass below moves the finished rows into the air.
	inner := gutterInner(width)
	out, closed := a.deckRows(a.conversation(), inner)
	// THE ONE THING EVER EMITTED AT THE TOP OF THE TRANSCRIPT, and it is emitted
	// here rather than by any block because it is not one: it says that the
	// conversation on screen starts part-way through and that scrolling reaches
	// the rest (replay.go's [app.earlierRow]). It goes while the reader is still
	// above it — the moment the real beginning is drawn, there is nothing left
	// to promise and the marker is not laid out at all.
	if line := a.earlierRow(inner); line != "" && len(out) > 0 {
		out = append([]row{{text: line, entry: -1}, {entry: -1}}, out...)
	} else if len(out) > 0 {
		// AND ONE ROW OF AIR WHERE THE CONVERSATION TRULY BEGINS. When the
		// marker is up, its own trailing blank is this row; when the first
		// block drawn IS the first thing ever said, the conversation used to
		// open hard against the top of the frame — the person's `›` on the
		// frame's first row, with less silence above the question than any
		// block below it gets. It is a row of the SCROLLBACK rather than of the
		// frame, so a long conversation carries it away with the history it
		// belongs to and the reading view is unchanged.
		out = append([]row{{entry: -1}}, out...)
	}
	// A FORMING TASK'S BLOCK LIVES AT THE TRANSCRIPT TAIL, outside the
	// notes deck it is deliberately not part of. It takes the ordinary block gap
	// and no border of its own beyond the one named hairline on each live row.
	if forming := a.preflightRows(inner); len(forming) > 0 {
		if len(out) > 0 {
			out = append(out, row{entry: -1})
		}
		out = append(out, forming...)
		closed = true
	}
	line, ok := a.harnessStepRow(inner)
	if !ok && !hasCompactActivity(out) {
		line, ok = a.ellipsis()
		if ok && !a.workFoldOpen(a.conversation(), a.turn) && !a.unfolded[a.turn] {
			line = a.activityLine("  " + a.shimmer("Working"))
		}
	}
	if ok {
		if closed && len(out) > 0 {
			out = append(out, row{entry: -1})
		}
		out = append(out, row{text: line, entry: -1})
	}
	// THE GUTTER, BEFORE THE TWO PASSES THAT PAINT THE WHOLE ROW (gutter.go). It
	// goes here rather than one line later because the wash and the ground below
	// are statements about the row a person is on, and a band that stopped two
	// cells short of the frame's edge would say the gutter was not part of it.
	gutterPass(out, width)
	// THE CUT, SECOND TO LAST. A rewind being chosen is a property of the screen
	// too — the line between two blocks, and the wash over everything under it —
	// so it is applied to finished rows here for [app.hoverPass]'s reason, one
	// pass above it (rewind.go).
	out = a.rewindPass(out, width)
	// THE POINTER, LAST. Hover is a property of the screen and not of the
	// conversation, so it is applied to finished rows in one pass here rather
	// than threaded through six renderers (hover.go).
	a.hoverPass(out, width)
	return out
}

// deckRows is the pass itself, over whichever list is being drawn. It reports
// whether the LAST block it laid out closed something — a cluster, a proposal,
// a landed note — which is what a caller needs to decide the gap before a foot
// of its own: the conversation's ellipsis above, and the room's finished line
// (room.go). A foot wedged against the block above it is the same defect the
// four rules exist to prevent.
func (a *app) deckRows(d deck, width int) ([]row, bool) {
	es := d.entries
	activity, activityAt, showActivity := a.questionActivity(d)
	folds := a.deckFolds(d)
	// THE ANSWER HIERARCHY IS DECIDED BEFORE A SINGLE BLOCK DRAWS (hierarchy.go).
	// Which prose was narration and which was the answer is a fact about this
	// LIST, and it is settled here — over the deck, so the conversation, a room
	// and a node's transcript inside a run's page all get it from one pass — so
	// that [app.renderEntry] can paint one block at a time without ever asking
	// what surrounds it.
	stampHierarchy(es, folds)
	// Captions read the hierarchy rather than restating it, so their derivation
	// follows the stamp and their lifted heads are stamped before any block draws.
	d.captions = deriveCaptions(es, d.runningTurn)
	stampCaptions(es, d.captions)
	out := make([]row, 0, len(es)+8)
	// wasCluster says the block that just drew was a tool cluster, and wasBlock
	// that it was a CLOSED block — a proposal, or the note a node writes when it
	// lands (task.go). wasUser says it was THE PERSON'S OWN MESSAGE, and it buys
	// the one blank this pass long owed: a turn is a change of speaker, and the
	// reply — narration, a thinking block, a cluster, the chip of a folded turn —
	// used to open on the very next row, wedged against the question the way no
	// answer wedges against the block above it. Together the three are the whole
	// of the state this pass carries.
	wasCluster, wasBlock, wasUser, wasNote := false, false, false, false
	gap := func() {
		if len(out) > 0 {
			for range spacingBlockRows {
				out = append(out, row{entry: -1})
			}
		}
	}
	// THE CLOCK'S OWN ROWS (timestamps.go) are laid out from here for the reason
	// every blank on this surface is: they are spacing-bearing blocks, and a
	// block that emitted its own gap would be a second spacing law. `walk` is the
	// turn this pass is inside and the last moment it knows about — the two facts
	// a receipt and a seam mark are drawn from. It runs over the CONVERSATION and
	// over nothing else (see [deck.clock]).
	walk := stampWalk{}
	clock := func(i int) {
		if !d.lens.clock {
			return
		}
		drew := false
		out, drew = a.stampBlock(d, out, &walk, i, width, gap)
		if drew {
			wasCluster, wasBlock = false, false
		}
	}
	// THE RUNNING TURN'S OWN WINDOWS (livesteps.go). They are derived here, beside
	// the folds and off the same captions, because they are the same object one
	// tense earlier. The lens chooses live compactness independently of its
	// settled fold policy, and each page owns its disclosure key.
	lives := deriveLiveWork(d)
	for i := 0; i < len(es); i++ {
		e := &es[i]
		if f, ok := folds[i]; ok {
			// THE CHIP IS NAMED BY ITS OWN KEY AND NOT BY THE TURN
			// (workfold.go's [workfold.key]): a room's page is one turn holding a
			// chip per phase, and a row that carried the turn would make every
			// chip on it one control.
			open := a.workFoldOpen(d, f.key)
			// The chip stands where the turn's work stood, so it takes the same
			// blank the work's first block would have taken — which after the
			// person's message is the change-of-speaker gap wasUser buys.
			if wasUser || wasBlock {
				gap()
			}
			// The chip is laid flush and takes its two cells from THE INDENT LAW's
			// pass below like every other work row, rather than baking them in:
			// the pass used to sniff for a row that already began with two spaces
			// so as not to move this one twice, and the sniff caught every other
			// row that happened to open on two spaces as well (see the pass).
			out = append(out, row{text: a.pal.dim(a.workfoldLabel(d, f)), entry: -1, hit: hitWorkFold, turn: f.key})
			if !open {
				i = f.answer - 1
				wasCluster, wasBlock, wasUser, wasNote = false, false, false, false
				continue
			}
			// OPEN IS THE OUTLINE: every finished step as a caption line. A click
			// (or enter) on a caption opens only that step's calls — so the page
			// stays a stack of what happened, not a dump of every tool again.
			drewCaption := false
			for _, c := range d.captions {
				toolsFrom, toolsTo := captionTools(c, es)
				if toolsFrom < f.start || toolsFrom >= f.answer {
					continue
				}
				drewCaption = true
				capOpen := a.captionCallsOpen(d, c)
				out = append(out, a.captionRows(c, false, capOpen, width, d)...)
				if capOpen {
					out = append(out, a.captionBody(d, c, width)...)
					for at := toolsFrom; at < toolsTo; at++ {
						out = append(out, a.toolRows(d, at, at == toolsTo-1, width)...)
					}
				}
			}
			if drewCaption {
				i = f.answer - 1
				wasCluster, wasBlock, wasUser, wasNote = true, false, false, false
				continue
			}
			// A fold with no captions keeps the old expansion so history is never
			// behind an empty outline.
			wasUser = false
		}
		if e.turn != walk.turn {
			// The turn before this one is over: its receipt, and then the mark
			// that says how long ago that was. Both are drawn HERE — at the seam
			// between two turns — because that is where a person reads them.
			clock(i)
		}

		// THE RUNNING TURN'S MACHINERY IS THREE LINES UNTIL SOMEBODY OPENS IT
		// (livesteps.go). It is read after the clock and before the cluster for the
		// chip's reason: the block stands exactly where the work stands, so it takes
		// the blank the work's first block would have taken.
		//
		// AND THERE IS ONE WORKING DOOR PER TURN. A turn split by a kept row can
		// leave a run above the split holding nothing but settled reasoning — no
		// step of its own — and a window with no steps in it draws itself as a
		// `▸ Work · ctrl+e` door (livesteps.go's [app.liveStepBlock]). Two doors
		// with the same key on one page read as the same turn running twice, which
		// is the exact thing [liveWork.last] took the token column off every run
		// but the frontier to stop saying. So a run that is neither the frontier
		// nor holding a step of its own is drawn through the ordinary path below,
		// where its reasoning becomes the `thought for …` row a finished turn
		// draws — the chip a reader already knows, with its own door onto the
		// working. The frontier keeps everything it has, and a non-frontier run
		// that DOES hold steps still draws its block: those steps are work with
		// nothing else on the page to say it.
		if w, ok := lives[i]; ok && (w.last || len(w.steps) > 0) {
			if !a.workFoldOpen(d, w.key) {
				// The block owns its activity door before the first caption,
				// and spends the ordinary gap only when it actually draws.
				rows := a.liveStepBlock(w, width, d)
				if len(rows) > 0 {
					if wasUser || wasBlock {
						gap()
					}
					out = append(out, rows...)
					wasCluster, wasBlock, wasUser, wasNote = true, false, false, false
				}
				i = w.end - 1
				continue
			}
			if wasUser || wasBlock {
				gap()
			}
			// OPEN IS THE OUTLINE, and it is the outline an open chip draws, one
			// tense earlier: every step as a caption row with its own door onto its
			// own calls. The reasoning blocks inside the window are drawn where they
			// happened — a person who opened the work asked for the machinery, and
			// the model's working is machinery this surface has always shown.
			out = append(out, a.liveWorkDoor(w, width, d))
			// THE STEPS ARE WALKED WITH A CURSOR AND THE SPANS ARE STEPPED OVER.
			// The window already knows its own steps in order ([liveWork.steps]),
			// so a block between two of them is one that belongs to no step and is
			// drawn as itself; asking every row which step holds it would be the
			// same page at the cost of rows times steps.
			step := 0
			for at := w.start; at < w.end; at++ {
				if step >= len(w.steps) || at != w.steps[step].start {
					for _, text := range a.entryRows(d, at, width) {
						out = append(out, row{text: text, entry: at})
					}
					continue
				}
				c := w.steps[step]
				step++
				// The rest of the step's own span — its narration and its calls —
				// is the caption's to draw, so the walk resumes past it.
				at = c.end - 1
				capOpen := a.captionCallsOpen(d, c)
				out = append(out, a.captionRows(c, c.ended.IsZero(), capOpen, width, d)...)
				if !capOpen {
					continue
				}
				out = append(out, a.captionBody(d, c, width)...)
				toolsFrom, toolsTo := captionTools(c, es)
				// AND AN OPEN STEP KEEPS THE CALL WINDOW IT ALREADY HAD. This is
				// the same batch the cluster below draws with the same budget
				// ([app.foldWindow] — the running call and the two it followed);
				// opening the work asks to see the steps, not to be handed a turn's
				// whole call log, and a second answer here would make one batch two
				// lengths depending on which door reached it.
				if window := a.foldWindow(d); toolsTo-toolsFrom > window && !d.unfolded[w.turn] {
					toolsFrom = toolsTo - window
				}
				for call := toolsFrom; call < toolsTo; call++ {
					out = append(out, a.toolRows(d, call, call == toolsTo-1, width)...)
				}
			}
			wasCluster, wasBlock, wasUser, wasNote = true, false, false, false
			i = w.end - 1
			continue
		}

		// A run of tool entries from one turn is a cluster, and a cluster is
		// laid out as a unit: it is the thing that folds.
		if e.kind == entryTool {
			end := i + 1
			for end < len(es) &&
				es[end].kind == entryTool &&
				es[end].turn == e.turn {
				end++
			}
			if wasBlock || wasUser || (!wasCluster && !opensTurn(es, i)) {
				gap()
			}
			if c, ok := captionAt(d.captions, i); ok {
				open := a.captionCallsOpen(d, c)
				out = append(out, a.captionRows(c, c.ended.IsZero(), open, width, d)...)
				toolsFrom, toolsTo := captionTools(c, es)
				if open {
					out = append(out, a.captionBody(d, c, width)...)
					start := toolsFrom
					if window := a.foldWindow(d); toolsTo-start > window && !d.unfolded[e.turn] {
						start = toolsTo - window
					}
					for at := start; at < toolsTo; at++ {
						out = append(out, a.toolRows(d, at, at == toolsTo-1, width)...)
					}
				}
				// Advance only past THIS step. Using the whole consecutive tool
				// run would let a shut past caption swallow the live frontier.
				wasCluster, wasBlock, wasUser, wasNote = true, false, false, false
				i = toolsTo - 1
				continue
			} else {
				out = a.clusterRows(d, out, i, end, width)
			}
			wasCluster, wasBlock, wasUser, wasNote = true, false, false, false
			i = end - 1
			continue
		}

		// A run of landed tasks is a BATCH, and a batch is laid out as a unit for
		// the reason a cluster is: whether it rolls up into one object is a
		// property of the run and not of any card in it (taskdone.go). It reads
		// the DECK's entries, like every other block in this pass: a room is a
		// list of its own, and an index is only meaningful against the list it
		// was taken from.
		if e.kind == entryDone {
			end := i + 1
			for end < len(es) && es[end].kind == entryDone {
				end++
			}
			gap()
			out = a.doneCluster(d, out, i, end, width)
			wasCluster, wasBlock, wasUser, wasNote = false, true, false, false
			i = end - 1
			continue
		}
		if e.kind == entryHarness {
			gap()
			for _, text := range a.harnessFeedRows(e.harness, width, a.selected(i)) {
				hit := hitNone
				if strings.Contains(text, "[enter] save") {
					hit = hitHarness
				}
				out = append(out, row{text: text, entry: i, hit: hit})
			}
			wasCluster, wasBlock, wasUser, wasNote = false, true, false, false
			continue
		}

		// A CAPTION HEAD IS DRAWN BY THE CAPTION, not here. Its first line is the
		// heading; the remainder rides under an open caption via [captionBody].
		// Drawing it again would put the body above the heading.
		if e.kind == entryAssistant && e.capHead {
			continue
		}

		rows := a.entryRows(d, i, width)
		if len(rows) == 0 {
			continue
		}
		// AND A CORRECTION TAKES THE SAME BLANK THE QUESTION TAKES. It is a change
		// of speaker in the middle of a turn, and one wedged against the paragraph
		// above it would read as part of that paragraph — which is exactly the
		// defect park.go's block was built to end, arriving through the other door
		// (steerelbow.go).
		// AND THE SURFACE'S OWN VOICE OPENS ON A BLANK ROW. A note leads with `· `
		// at the conversation's indent, which is exactly the glyph and the column
		// a markdown bullet lands on — so under an answer that ends in a list,
		// `· 3 standing orders here — /standing` read as the model's fourth
		// bullet. The one boundary a transcript must draw is WHO IS TALKING, and
		// a blank row is what this surface already spends on every other change
		// of speaker (the rules around it). A RUN OF NOTES IS ONE BLOCK: the
		// blank is bought where the voice changes, not between two lines of the
		// same voice, or the opening frame's three notes would arrive as three
		// paragraphs.
		if (e.kind == entryNote && !wasNote) ||
			wasCluster || wasBlock || wasUser || e.kind == entryUser || e.kind == entrySteer || e.kind == entryTask ||
			(e.kind == entryStanding && e.stand != nil && !e.stand.news()) ||
			// AND THE BREATH ABOVE A PROMOTED ANSWER (hierarchy.go's
			// [answerBreath]). It is asked HERE, inside the same condition as the
			// four rules above it, because [gap] is not idempotent: two calls are
			// two blank rows, and the commonest promoted answer follows a cluster
			// that has already asked for the same silence.
			answerBreath(es, i) {
			// A PROPOSAL TAKES A BLANK OF ITS OWN. It is the one block on this
			// surface that interrupts a reply to ask something, and a question
			// wedged against the sentence above it reads as part of that sentence.
			// AND ONE AFTER IT, which is what wasBlock buys: the block has a foot,
			// and a paragraph that started on the row under it would be a paragraph
			// inside the question.
			gap()
		}
		// The proposal is the only non-tool block a click acts on, so it is the
		// only one that carries a hit (task.go) — and its choices row carries a
		// different one, because that row answers the question rather than opening
		// the brief.
		hit := hitNone
		if e.kind == entryTask {
			hit = hitTask
		}
		// THE BLOCK'S REFERENCES ARE NUMBERED ACROSS ITS ROWS, and the count is
		// reset here because the pointer holds a link as (block, ordinal): a
		// paragraph re-wraps when the frame is dragged, so an ordinal counted per
		// screen row would name a different phrase after a resize (markdown.go's
		// [taskLink]).
		links := 0
		for n, text := range rows {
			// A STANDING CARD HAS NO PRESSABLE ROW ANY MORE. Its answers are the
			// question, and the question is drawn — and pressed — above the box
			// like every other one (standing.go, question.go's [app.questionPress]).
			drawn := row{text: text, entry: i, hit: hit}
			// THE LINK PASS RUNS ON THE MODEL'S OWN ROWS AND ON NOTHING ELSE
			// (markdown.go). It is applied HERE — after the block was rendered and
			// wrapped, at the moment its rows become screen geometry — because a
			// link's columns are a fact about the row it landed on, and the row it
			// lands on is decided by a wrap this pass must not have an opinion
			// about.
			if e.kind == entryAssistant {
				// AND THE ONE THE POINTER IS ON IS INKED BY THE SAME PASS. It cannot be
				// done afterwards: the row that comes back is styled text, and a hue
				// spliced into it by column would have to redo the escape bookkeeping
				// [paintLinks] is already doing (markdown.go).
				hot := -1
				if at := a.hoveringLink(i); at >= 0 {
					hot = at - links
				}
				drawn.text, drawn.links = a.linkTasks(text, hot)
				for j := range drawn.links {
					drawn.links[j].ord = links + j
				}
				links += len(drawn.links)
				// AND THE TABLE FEET ARE READ BACK OFF THE BLOCK, keyed by the row
				// they landed on. They are not derived here for the reason the links
				// above them are derived here: a foot's columns are decided by the
				// render that drew it, and this pass is the one that turns that
				// block's rows into screen geometry (mdtable.go).
				drawn.foot = e.feet[n]
			}
			out = append(out, drawn)
		}
		// AND THE INSTRUCTION'S DOOR, under the lines it is holding back
		// (brieffold.go). It is emitted HERE, and not by the block, for the reason
		// the worked chip above is: a row a click acts on is screen geometry, and
		// this pass is where blocks become screen geometry. It carries the block's
		// index so the press knows which instruction it opened, and it is drawn
		// whether the fold is shut or open — an opened fold keeps its door, which
		// is how it is shut again.
		if hidden := briefFoldHidden(e, width); hidden > 0 {
			out = append(out, row{
				text:  a.briefFoldLine(hidden, !e.full, width),
				entry: i, hit: hitBrief,
			})
		}
		out = append(out, a.mediaRows(e, i, width, userLead)...)
		if showActivity && i == activityAt {
			gap()
			out = append(out, a.activityRows(activity, "", width)...)
		}
		wasCluster = false
		wasNote = e.kind == entryNote
		wasBlock = e.kind == entryTask || (e.kind == entryStanding && e.stand != nil && !e.stand.news())
		// The change-of-speaker gap belongs to the person's message and not to a
		// kind of block: a divider between the question and the reply carries the
		// mark forward, because the reply still opens under their words.
		if e.kind != entryDivider {
			// A CORRECTION IS ONE OF THE PERSON'S MESSAGES, so the reply that
			// follows it opens under the same change-of-speaker blank the reply to
			// a question opens under (steerelbow.go).
			wasUser = e.kind == entryUser || e.kind == entrySteer
		}
	}
	// THE LAST TURN'S RECEIPT, which has no next turn to be drawn at the seam
	// with. A turn still running has no stamp yet, so this draws nothing until
	// the moment it settles — which is exactly when the figures become true.
	clock(-1)
	// THE INDENT LAW is applied after layout so every kind of machinery,
	// including expanded details and synthetic fold rows, obeys one rule.
	//
	// IT COSTS THE ROWS IT MOVES TWO CELLS, and the block that lays itself out
	// flush to the right edge is the one that has to know: a tool line built to
	// the frame's whole width and then shoved two columns right is two columns
	// wider than the column it is drawn in, and [app.railJoin] cuts the overhang
	// back with an ellipsis — which is where a running call's spinner and its
	// clock went, leaving `0…` at the frame's edge. [app.toolLine] and
	// [app.formingLine] subtract [workIndentCols] for exactly that reason, AFTER
	// they have chosen their tier from the frame's own width.
	//
	// EVERY WORK ROW MOVES, WITHOUT LOOKING AT WHAT IT BEGINS WITH. This pass
	// used to leave alone any row whose text already opened on two spaces, so
	// that the fold chip above — which once carried its own indent — would not
	// be moved twice. But two leading spaces are not a mark of this pass: they
	// are a note's continuation lead (render.go's entryNote, "· " on the first
	// row and two blanks under it), a live window's, an indented line inside a
	// tool's result, a list inside streaming prose. Every one of those was
	// skipped while the row above it was moved, so /cost's table came out with
	// `· spend` two cells right of `tokens`, `model calls` and `time` under it —
	// the first row alone wearing the gutter, and its columns broken against
	// the rest. The chip now lays itself flush and takes its cells here, and
	// nothing else in the deck bakes the indent in, so there is nothing left to
	// guard against.
	//
	// AND EVERY SPAN A CLICK IS RESOLVED BY MOVES WITH THE WORDS, as gutter.go's
	// [gutterPass] moves them. The link pass has already measured a task link's
	// columns on the row as it was built, so a pass that moved the text and not
	// the span left `task 8` in the work's own prose two cells to the right of
	// the cells that open it — a press on its number landed in the sentence
	// beside it. That was true of every moved row with a link in it before this
	// pass moved every row; it is not true of any row now.
	// THE TEAM HALF OF THE LINK PASS, over the rows as they were laid out and
	// before the indent law moves them with their spans (teamlink.go).
	a.teamLinkPass(out, es)
	a.mentionLinkPass(out, es)
	if workIndent(width) != "" {
		cols := workIndentCols(width)
		for i := range out {
			if rowIsWork(out[i], es, folds) {
				out[i].text = "  " + out[i].text
				out[i].pictureOpen = out[i].pictureOpen.shift(cols)
				if out[i].keep.pressable() {
					out[i].keep.from += cols
					out[i].keep.to += cols
				}
				for j := range out[i].links {
					out[i].links[j].span = out[i].links[j].span.shift(cols)
				}
				out[i].foot.span = out[i].foot.span.shift(cols)
			}
		}
	}
	return out, wasCluster || wasBlock
}

// hoverPass paints the row a person is on — under the pointer or under the
// keyboard cursor — and it is the last thing done to any row list on this
// surface, the conversation's and the room's alike.
//
// CURSOR AND HOVER ARE ONE STEP, NOT TWO, so they are one pass. The pointer's
// row has come up onto THE GROUND LADDER's cursor step here since hover was
// built; the keyboard's row said its position with an accent on the rail glyph
// and no ground at all, so ↑ and ↓ moved something a pointer crossing the same
// rows would have lit. That is the ladder's own refusal — the row a person is on
// does not change appearance depending on which hand they used — and mending it
// here rather than at the four renderers means the tool row, the phone row, the
// forming row, the done card, the rollup and the harness card all learn it at
// once, and none of them has to remember the pass exists.
func (a *app) hoverPass(out []row, width int) {
	for i := range out {
		if a.isHot(out[i]) || a.onCursorRow(out[i]) {
			out[i].text = a.hoverRow(out[i].text, width)
		}
	}
}

// onCursorRow reports whether the KEYBOARD cursor is on this row: ↑/↓ walk
// [app.sel] over the turn's calls and enter opens whichever one it stopped on.
//
// It is not folded into [app.isHot] because that predicate answers "is the
// POINTER here", and the linear tier has no pointer — see the note there. A
// keyboard cursor is a position in a list and a position is a fact for every
// reader, so the two questions stay two questions even though today's answer
// takes them to the same rung.
func (a *app) onCursorRow(r row) bool {
	if r.hit == hitCaption {
		key, ok := selectedCaption(a.sel)
		return ok && key == r.turn
	}
	return r.entry >= 0 && a.selected(r.entry)
}

// isHot reports whether the pointer is on this row. The linear tier has no
// pointer at all (Options.Linear), so it has no hot row.
func (a *app) isHot(r row) bool {
	if a.linear {
		return false
	}
	switch a.hot.kind {
	case hoverEntry:
		return r.entry >= 0 && r.entry == a.hot.entry
	case hoverFold:
		return r.hit == hitFold && r.turn == a.hot.turn
	case hoverCaption:
		return r.hit == hitCaption && r.turn == a.hot.turn
	case hoverWorkFold:
		return r.hit == hitWorkFold && r.turn == a.hot.turn
	case hoverPictures:
		return r.hit == hitPictures && r.entry == a.hot.entry && r.pictureIndex == a.hot.index
	case hoverBrief:
		// THE DOOR AND NOT THE BLOCK (brieffold.go): the lines above it are the
		// person's own words, and nothing happens when they are pressed.
		return r.hit == hitBrief && r.entry == a.hot.entry
	}
	return false
}

// captionBody is the narration under an open step heading: the remainder of a
// demoted prose head after its first line was lifted into the caption. THE
// OUTLINE HIDES IT; expanding the caption puts those words back above the calls.
func (a *app) captionBody(d deck, c caption, width int) []row {
	if c.source != captionSaid || c.head < 0 || c.head >= len(d.entries) {
		return nil
	}
	e := &d.entries[c.head]
	text := e.text
	if e.capCut > 0 && e.capCut <= len(text) {
		text = text[e.capCut:]
	}
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var out []row
	for _, line := range a.workingProse(text, width) {
		out = append(out, row{text: line, entry: c.head, hit: hitNone, turn: e.turn})
	}
	return out
}

// opensTurn reports whether the entry at i is the first thing its turn drew.
// A cluster that opens a turn follows the person's own message, whose
// change-of-speaker gap (wasUser, in [app.deckRows]) is the one blank that
// boundary takes — this test is what keeps the cluster from asking for a
// second one of its own.
func opensTurn(es []entry, i int) bool {
	for at := i - 1; at >= 0; at-- {
		if es[at].turn != es[i].turn {
			return true
		}
		if es[at].kind != entryUser {
			return false
		}
	}
	return true
}

// entryRows is the per-entry cache for everything that is not a tool call.
//
// Tool lines never come through here — [app.clusterRows] draws them, because
// their marker depends on their position in the cluster — and the LINE is
// deliberately not cached: one of them is animating, and a cache with an
// animation in it is a cache that has to be invalidated thirty times a second,
// which is not a cache but a bug with a field.
//
// THE BLOCK UNDER THE LINE IS A DIFFERENT QUESTION and has a memo of its own
// ([toolBlock]): what moves is the spinner, the clock and the pointer's
// brightness, all of which live on the line, while the diff, the source and the
// output hanging under it are facts about a payload that arrived once.
func (a *app) entryRows(d deck, i, width int) []string {
	e := &d.entries[i]
	// A RUNNING COMPACTION IS NOT CACHED, for the reason the tool lines are not:
	// its spinner and its count-up are functions of the frame, so a cached row
	// would be a still photograph of an animation. It rejoins the cache the
	// moment it settles, which is the moment it stops moving.
	if e.kind == entryCompact && e.ended.IsZero() {
		return a.renderEntry(i, e, width)
	}
	// A PROPOSAL STILL ARRIVING IS NOT CACHED EITHER, and for the same reason:
	// its count-up and its spinner are functions of the frame (task.go). It
	// rejoins the cache the moment the call lands, which is the moment the row
	// stops moving — the countdown that used to keep the answered card out of
	// the cache as well is the question block's now, and the block is chrome.
	if e.kind == entryTask && e.card != nil && e.card.forming && !e.card.settled() {
		return a.renderEntry(i, e, width)
	}
	// AND A SIGN-IN THAT IS STILL WAITING, for the reason both of those are not:
	// its spinner is a function of the frame (connect.go). It rejoins the cache
	// the moment it settles, which is the moment it stops moving.
	if e.kind == entryConnect && e.conn != nil && e.conn.state == connectWaiting {
		return a.renderEntry(i, e, width)
	}
	// AND AN OPEN STANDING CARD, for the reason the proposal above is not: its
	// meter drains toward the moment the engine declines it (standing.go). It
	// rejoins the cache the moment it is answered.
	if e.kind == entryStanding && e.stand != nil && !e.stand.settled() && !e.stand.news() {
		return a.renderEntry(i, e, width)
	}
	// AND A CORRECTION THAT IS STILL MOVING, for the reason all four of those are
	// not: an elbow that has not landed turns a spinner and one that just did is
	// on its way down the fade, both of which are functions of the frame
	// (steerelbow.go). It rejoins the cache the moment it settles, which is the
	// moment the block stops moving.
	if e.kind == entrySteer && a.elbowMoving(e.steer) {
		return a.renderEntry(i, e, width)
	}
	if e.identity == 0 {
		a.renderIdentity++
		e.identity = a.renderIdentity
	}
	// A PICTURE-BEARING USER ENTRY KEEPS THIS ORDINARY ROW KEY. The picture
	// cache below it keys the file's own mtime and size, while this row memo may
	// keep painted cells until the next palette repaint or remote-file restyle.
	// That bounded staleness is the trade for keeping settled transcript blocks
	// out of the per-frame path; tool rows make the opposite trade because their
	// lines already bypass this cache.
	key := renderedEntryKey{identity: e.identity, width: width, ink: a.inkState}
	// A TEAM NOTE DRAWS ANSWERS OUT OF THE TRAFFIC CACHE (teamthreadcard.go), so
	// its rows go stale when that cache moves, and only then.
	if a.teamNoteStale(e, width) {
		e.stale = true
	}
	if e.built && e.rowKey == key && !e.stale {
		return e.rows
	}
	e.rows = a.renderEntry(i, e, width)
	e.rowKey, e.width, e.built, e.stale = key, width, true, false
	return e.rows
}

// userLead is the column the person's own words are drawn in: the turn glyph on
// the first row and this much alignment under it, so every continuation line
// sits under the TEXT rather than under the mark ([app.renderEntry]'s entryUser
// case). It is named because the fold that counts those lines has to wrap at
// exactly the width the paint wraps at (brieffold.go's [briefFoldHidden]), and a
// literal 2 in two files is a literal 2 that will drift.
const userLead = "  "

// userLeadCols is what that lead costs, in cells.
const userLeadCols = len(userLead)

// userRailGutter is the RAIL'S OWN CELL, kept clear of the person's words.
//
// [app.railJoin] pads a conversation row out to [app.bodyWidth] and then writes
// the seam in the very next column, so a body row that measures the column
// exactly ends one cell from the divider — and a person's own message is the one
// block on this surface that regularly fills its column, because it is wrapped
// to the column and not to a reading measure the way the model's prose is
// (markdown.go hands prose [prose.DefaultMeasure]). At 120 columns a wrapped
// question read `…and tell me│`, with the words touching the rule while the rows
// above and below it stood clear.
//
// ONE CELL IS THE WHOLE FIX and it is deliberately not two: the gutter belongs
// to the divider, not to the paragraph, and every cell taken here is a cell off
// the measure at sixty columns, where the same block is the thing that has least
// room to give. It is charged at every width because the frame is the column
// either way — under [railSlimFloor] the cell kept clear is the terminal's own
// edge, which is the same collision with a different rule drawn through it.
const userRailGutter = 1

// userBodyCols is the column a person's own words are wrapped into: the lead
// they are drawn under and the divider's cell, both taken off.
//
// IT IS A FUNCTION BECAUSE TWO PLACES ASK IT AND THEY MAY NEVER DISAGREE — the
// paint here, and the fold that counts the lines the paint would make
// (brieffold.go's [briefFoldHidden]). A literal in two files is the drift
// [userLead]'s own note was written about.
func userBodyCols(width int) int {
	if room := width - userLeadCols - userRailGutter; room > 0 {
		return room
	}
	return 1
}

// pictureMarkerMask holds one basename out of the generic path pass. A marker
// deliberately shows only the basename, while its door must retain the full
// attachment path; letting the generic pass resolve the visible name loses
// that identity whenever the picture lived below another directory.
type pictureMarkerMask struct {
	tokens []rune
	label  []rune
	path   string
}

// maskPictureMarkers substitutes equal-width private glyphs before wrapping.
// The generic path linker cannot mistake those glyphs for a workspace-relative
// basename, and [app.restorePictureMarkers] puts the visible name and its exact
// door back after that pass. Equal width keeps every wrap boundary unchanged.
func (a *app) maskPictureMarkers(text string, e *entry, doors bool) (string, []pictureMarkerMask) {
	if e == nil || len(e.pictures) == 0 || !doors {
		return text, nil
	}
	masks := make([]pictureMarkerMask, 0, len(e.pictures))
	for i, picture := range e.pictures {
		label := filepath.Base(strings.TrimSpace(picture))
		if label == "" || label == "." {
			continue
		}
		marker := "[#" + itoa(i+1) + " " + label + "]"
		labelRunes := []rune(label)
		if len(labelRunes) == 0 || !strings.Contains(text, marker) {
			continue
		}
		// One private rune stands in for one single-cell filename rune. Distinct
		// runes let restoration survive a hard wrap or a brief-fold cut: each
		// visible fragment can be put back and linked without leaking a sentinel.
		tokens := make([]rune, len(labelRunes))
		maskable := true
		for j, r := range labelRunes {
			if ansi.StringWidth(string(r)) != 1 {
				maskable = false
				break
			}
			tokens[j] = rune(0xE000 + i*512 + j)
		}
		if !maskable || len(tokens) > 512 || tokens[len(tokens)-1] > 0xF8FF {
			continue
		}
		token := string(tokens)
		text = strings.Replace(text, marker, "[#"+itoa(i+1)+" "+token+"]", 1)
		masks = append(masks, pictureMarkerMask{tokens: tokens, label: labelRunes, path: picture})
	}
	return text, masks
}

func (a *app) restorePictureMarkers(rows []string, masks []pictureMarkerMask, here bool) []string {
	for _, mask := range masks {
		l := a.linker()
		// A live hosted attachment is on the laptop in front of the person, not
		// on the hosted engine. Its marker therefore uses the local linker even
		// though model-written paths elsewhere in the same row use the far one.
		if here {
			l.far = nil
			l.root, l.home, l.seen = a.workspace, a.tilde, a.pathSeen
		}
		// An exact attachment path stays honest inside a task room too. The room
		// disables generic relative links because they could name another
		// worktree; this path came from the journal and is not a guess.
		l.on = a.pathLinks
		target, linked := "", false
		if l.on {
			target, linked = l.resolve(strings.TrimSpace(mask.path))
		}
		for i := range rows {
			for j := 0; j < len(mask.tokens); {
				at := strings.IndexRune(rows[i], mask.tokens[j])
				if at < 0 {
					j++
					continue
				}
				end, k := at, j
				for k < len(mask.tokens) && strings.HasPrefix(rows[i][end:], string(mask.tokens[k])) {
					end += len(string(mask.tokens[k]))
					k++
				}
				label := string(mask.label[j:k])
				if linked {
					label = l.anchor(label, target)
				}
				rows[i] = rows[i][:at] + label + rows[i][end:]
				j = k
			}
		}
	}
	return rows
}

// renderEntry paints one block. Nothing here appends a blank row — see
// [app.layout].
//
// The index is carried in for one reason: hover is a fact about a POSITION in
// the conversation, and the only block that draws its own hover state — the
// thinking block, whose marker brightens — is also the only one whose rows are
// cached (hover.go marks it stale in exchange).
func (a *app) renderEntry(i int, e *entry, width int) []string {
	a.renders++
	switch e.kind {
	case entryUser:
		// THE PERSON'S OWN WORDS, IN A QUIET BLUE OF THEIR OWN — the accent on
		// the `›` and the body in MUTED, one full tier calmer, with every
		// continuation line aligned under the TEXT rather than under the
		// glyph. The glyph marks the turn; the column belongs to the sentence.
		//
		// The whole body wore the accent for a wave, and a wave was long enough
		// to read the cost: a question is often the longest paragraph on the
		// screen, and painting all of it in the identity hue spent THE ACCENT
		// BUDGET (styles.go) on prose — over a working turn whose narration was
		// also blue, the page read as one blue field. It then wore plain ink
		// for a day, and that failed the other way: the question and the answer
		// were the same colour, and the one distinction a transcript must draw
		// had nothing carrying it but a glyph. So the body settles one tier
		// down-and-across, on MUTED — a soft blue a full step calmer than the
		// accent, which still says "a different voice" without spending the
		// budget. Nothing the model's prose wears is muted (its narration is
		// the neutral narr tier, its answer is ink), so the hue stays an
		// identity. The body was bold ink once before, and bold stays wrong
		// for the stated reason: MARKDOWN OWNS WEIGHT.
		//
		// AND A LINE THE FAR END HAS NOT AGREED TO YET IS ONE STEP QUIETER
		// (echo.go). It is the same glyph in the same column with the same wrap
		// — nothing moves when the engine confirms — and the only difference is
		// the tier its words are painted in: [palette.narr], which is where this
		// surface already puts words that sit one reading step under the
		// question's own (steerelbow.go). NO NEW COLOUR IS INVENTED FOR IT,
		// because the mark's whole job is to be quiet enough that the ordinary
		// case reads as a line settling rather than as a warning.
		words := a.pal.muted
		if e.pending {
			words = a.pal.narr
		}
		// The original-file link is independent of the terminal's ability to paint pixels.
		pictureDoors := a.pathLinks
		marked, pictureMasks := a.maskPictureMarkers(requestDisplayText(e), e, pictureDoors)
		body := wrap(marked, userBodyCols(width))
		if strings.TrimSpace(e.text) == "" {
			body = nil
		}
		// AND A NODE'S INSTRUCTION SHOWS ITS OPENING AND NOT ALL OF ITSELF
		// (brieffold.go). The cut is made on the WRAPPED lines, so it lands where
		// a reader's eye would land rather than at some count of bytes; the door
		// under it is drawn by the pass that turns these rows into screen
		// geometry ([app.deckRows]), because a row a click acts on is that pass's
		// business and never a block's.
		body = briefFoldCut(e, body)
		out := make([]string, 0, len(body))
		for i, line := range body {
			lead := userLead
			if i == 0 {
				lead = a.pal.accent(a.pal.youGlyph())
			}
			// AND A RECOGNIZED SLASH COMMAND KEEPS ITS CHIP AFTER IT IS SENT
			// (slashchip.go). The box is where a person learns that this surface
			// knows the word, and a message that dropped the mark on its way into
			// the transcript would take the fact back the moment it mattered — a
			// conversation scrolled back through is the only record of what was
			// asked for. Every row of a wrapped message opens at a boundary: the
			// wrap breaks on spaces, and a word too long to break on one is not a
			// command either.
			// Sent tag ranges are stored on the unwrapped message. Wrapped rows
			// cannot reuse those offsets, so a line away from the head may chip a
			// send door only when this entry records that one acted.
			spans := transcriptCommandSpans([]rune(line), e.actedTags)
			if len(e.actedTags) > 0 && i > 0 {
				// Wrapping changes offsets; routed messages are ordinarily one line,
				// while the scanner still safely recognizes their door on this row.
				spans = commandSpans([]rune(line), true)
			}
			out = append(out, lead+paintCommandSpans(line, spans, a.pal, words))
		}
		// AND A PATH THE PERSON TYPED IS A DOOR TOO (pathlink.go). The commonest
		// one here is not typed at all: an `@task` mention leaves a footnote
		// block naming the node's transcript, and that block is the fastest way
		// into what a task actually did.
		//
		// AND THEN THE TURN SAYS WHAT IT WAS PART OF, when it was part of anything
		// (turncontext.go). It is added AFTER the paths are linked, and that order
		// is the point: the mark is this surface talking about the message, not a
		// word of the message, so nothing in it may become a door. On an ordinary
		// turn it adds nothing at all — which is nearly every turn, and is why this
		// line changes no frame most people will ever look at.
		//
		out = a.linkPaths(out)
		out = a.restorePictureMarkers(out, pictureMasks, e.picturesHere)
		out = a.turnContextRows(out, e.context, width)
		return out

	case entrySteer:
		// ONE CORRECTION, WHERE IT WAS SAID (steerelbow.go). It is a block of its
		// own rather than a tail on the question's block so that it lands between
		// the tool rows it interrupted — and it is drawn from here, like every
		// other kind, so that the row cache, the indent law and the fold all meet
		// it as an ordinary entry.
		//
		// AND IT SAYS WHAT IT WAS PART OF, on the person's own block's terms
		// (turncontext.go, applied inside [app.steerBlockRows] so that the mark sits
		// with the sentence and the passing clause stays last): a correction typed
		// into a NAMED working context is a thing said into that context exactly as
		// the question was, and it would otherwise be the only sentence of theirs on
		// the page that did not name where it went. An ordinary correction — every
		// one typed in this conversation — carries no context and adds nothing.
		return a.steerBlockRows(e, width)

	case entryAssistant:
		return a.linkPaths(a.assistantRows(i, e, width))

	case entryThinking:
		return a.thoughtRows(e, width, a.hoveringEntry(i))

	case entryDivider:
		return []string{a.divider(e.text, width)}

	case entrySeam:
		// THE DIM "· " LANE, like every other line this surface says on its own
		// account, and WRAPPED rather than cut — the same two leads a note uses
		// above. The sentence states a limit, and a limit truncated at column 58
		// on a narrow frame is the half-sentence that invites exactly the wrong
		// conclusion: "above here the model keeps a shortened rec…".
		out := make([]string, 0, 2)
		for i, line := range wrap(e.text, width-2) {
			lead := "· "
			if i > 0 {
				lead = "  "
			}
			out = append(out, a.pal.dim(lead+line))
		}
		return out

	case entryCompact:
		return []string{a.compactRow(e, width)}

	case entryConnect:
		return a.connectRows(e, width)

	case entryTask:
		return a.taskCardRows(e.card, width, a.sel == i)

	case entryStanding:
		return StandingCardRows(a, e.stand, width, a.sel == i)

	case entryHarness:
		return a.harnessFeedRows(e.harness, width, a.sel == i)

	case entryTeam:
		return a.teamCardRows(*e, width)

	case entryNote:
		// A LINE MAY BE QUIET; THE FACT IT CARRIES MAY NOT BE (payload.go). The
		// lane keeps its dim prose and its dim lead — a note is still the surface
		// talking about itself — while the words the person typed the command to
		// READ step up to ink, and any door the sentence names wears the chip it
		// wears in the box and in the message (slashchip.go). The walk is built
		// once for the whole note and spent across its wrapped rows, because the
		// data are in the order of the sentence and not of the rows the frame
		// happened to break it into.
		//
		// A NOTE WHOSE INDENTATION IS ITS MEANING IS CUT RATHER THAN RE-FLOWED
		// ([app.noteBlock]). The one shape that asks for it is a subharness card,
		// where the indent under a lane is what says the step belongs to that lane
		// — and [wrap] laid every one of those flat against the margin.
		// AND IT IS WRAPPED TO THE ROOM THE ROW ACTUALLY HAS, which is the frame
		// less BOTH of the leads it wears: the note's own "· " marker
		// ([noteLead]) and THE INDENT LAW's gutter, which this pass gives every
		// note afterwards ([app.deckRows] — a note is work by [workEntry]). It
		// was wrapped at the marker alone, so every row came out two cells wider
		// than the column it is drawn in, and [app.railJoin] cut the overhang
		// back with an ellipsis — which ate three characters out of the MIDDLE of
		// a path, silently, with the row below carrying on from after the gap. A
		// note is where this surface names its own files (below); a path that
		// comes out wrong with nothing saying so is worse than one not shown.
		room := width - noteLead - workIndentCols(width)
		body := wrap(e.text, room)
		if e.block {
			body = noteBlockLines(e.text, room)
		} else if e.sheet {
			// /help is a column. A wrap that starts the next row at the margin
			// makes the sentence look like a new key. Continuations keep the
			// column the first row's sentence already sits in.
			body = wrapSheet(e.text, room)
		}
		out := make([]string, 0, len(body))
		walk := factWalk{words: e.facts}
		for i, line := range body {
			lead := "· "
			if i > 0 {
				lead = "  "
			}
			// The lead is painted WITH the line rather than beside it, so a note
			// with nothing to lift comes back as the one dim run it has always been
			// — [paintPayload] hands an unmarked line straight back to the prose
			// role. The spans are shifted by the lead's own two cells, which is what
			// [noteLead] is for.
			out = append(out, paintPayload(lead+line,
				shifted(walk.take([]rune(line)), noteLead), a.pal, a.pal.dim))
		}
		// A NOTE IS WHERE THIS SURFACE NAMES ITS OWN FILES, and it names them in
		// full on purpose (statusnote.go): /status's `file`, /help's `session ·`,
		// `exported · …`, `resumed …`, `new session · …`. Every one of them is a
		// path somebody was going to select and paste somewhere, which is the
		// same sentence as "somebody was going to open it".
		return a.linkPaths(out)
	}
	return nil
}

// assistantRows is the markdown swap.
//
// While a turn streams, the live tail is PLAIN wrapped text: markdown of a
// half-written sentence costs a parse per frame and re-flows under the
// reader's eye. Every [markdownThrottle] the settled PREFIX — everything up to
// the last newline — is promoted to rendered rows and remembered as promoted,
// so the formatting catches up without the tail flickering between two
// renderings. On EventTurnDone the whole block is rendered at once.
//
// A FOOT UNDER A TABLE IS DRAWN ON THE SETTLED RENDER AND ONLY THERE
// (mdtable.go). A table half-arrived has columns that will move when the rest of
// it lands, and an offer to open something that is still being written is a
// promise the surface cannot keep — so the streaming path below leaves the
// block's feet where it finds them, which is nowhere.
//
// The index is carried in for one reason, and it is [app.renderEntry]'s: a
// foot brightens under the pointer, and the pointer is a fact about a POSITION
// in the list being drawn.
//
// ── INK SETTLES WHEN THE TURN ENDS ──────────────────────────────────────────
//
// The plain tail below — and ONLY the plain tail — is painted one lightness
// step above the body, in [palette.live]. It is THE GROWING EDGE: the bytes
// that arrived since the last promotion, the one thing on this screen that is
// actually moving, and therefore the one thing THE ACCENT BUDGET (styles.go)
// says may lead. When the turn settles, [entry.settled] flips, the row cache is
// dropped with it (see [feed.closeLive] and [app.entryRows]) and the same words
// come back through [app.settledMarkdown] at the calm body tier. The eye is
// drawn to the edge while it grows and history recedes on its own — no spinner
// is added and no checkmark, so the emptiness law is kept THROUGH the
// transition rather than around it.
//
// WHY ONLY THE TAIL, AND NOT THE PROMOTED HEAD. The head is
// [app.renderMarkdown]'s output: already-styled spans, already through prose's
// sanitizer, carrying headings, code chroma and emphasis of its own. Repainting
// it would mean either wrapping styled runs in a second foreground — which the
// inner SGR 39s would tear open halfway down the row — or reaching into the
// renderer, and the tail is where the honest answer already is. THE HEAD IS THE
// PART THAT HAS ALREADY STOPPED MOVING: every promotion is the surface deciding
// those bytes are final enough to format, so the head being calm and the tail
// being lit is the same fact the promotion itself states, said in ink.
func (a *app) assistantRows(at int, e *entry, width int) []string {
	tags := a.taskReplyTagRows(e.replyTags, width)
	// ── AND PROSE THAT TURNED OUT NOT TO BE THE ANSWER ──────────────────────
	//
	// A block that more work opened under is narration, and it is drawn as what
	// it is: the model's own words in the work column, one lightness step BELOW
	// the body, with no markdown on them at all (hierarchy.go states the law and
	// [app.workingProse] does the paint).
	//
	// It is asked BEFORE [entry.settled] because a demoted block has no live tail
	// worth keeping. [entry.mdCut] — the promotion boundary a still-streaming
	// block was cut at — stops mattering the instant the block stops being the
	// answer: it is a bookkeeping mark about how much of a growing edge had been
	// formatted, and there is no growing edge here. The whole text is wrapped at
	// one tier, so a block demoted mid-sentence cannot come back as half rendered
	// markdown and half plain.
	//
	// AND ITS TABLE FEET GO WITH IT. A foot is an offer to open something, and
	// the affordances belong to the answer: [entry.feet] is keyed by row, these
	// are different rows, and a stale foot would put a door on somebody else's
	// line (mdtable.go).
	if e.demoted {
		e.feet = nil
		text := e.text
		if e.capHead && e.capCut > 0 && e.capCut <= len(text) {
			text = text[e.capCut:]
		}
		return append(tags, a.workingProse(text, width)...)
	}
	if e.settled {
		return append(tags, a.settledMarkdown(at, e, width)...)
	}
	e.feet = nil
	var out []string
	drawn := e.revealed()
	if e.mdCut > 0 && e.mdCut <= len(drawn) {
		out = append(out, a.promotedRows(e, width)...)
		out = append(out, a.liveTail(drawn[e.mdCut:], width)...)
	} else {
		out = append(out, a.liveTail(drawn, width)...)
	}
	return append(tags, trimBlanks(out)...)
}

// promotedRows is the head of a streaming reply — the bytes the throttle has
// already declared final — off the memo beside the block, or rendered into it.
//
// THE PROMOTED HEAD IS THE PART THAT HAS STOPPED MOVING, which is the fact
// [app.assistantRows]'s own note above states about the ink and this states
// about the work: bytes that are not going to change do not need rendering
// twice. The rows are re-made when the cut moves (once every
// [markdownThrottle]) and when the frame is dragged to another width, and on
// every other frame of the turn — which at thirty frames a second is forty-four
// out of forty-five of them — the block's whole cost is wrapping the plain tail.
//
// See [entry.mdHead] for why the key is only those two facts, and for the one
// thing it deliberately cannot see.
func (a *app) promotedRows(e *entry, width int) []string {
	if h := e.mdHead; h != nil && h.cut == e.mdCut && h.width == width {
		return h.rows
	}
	rows := a.renderMarkdown(e.text[:e.mdCut], width)
	e.mdHead = &promotedHead{rows: rows, cut: e.mdCut, width: width}
	return rows
}

// promotedHead is that memo: the rendered rows, and the two facts that decided
// them. See [entry.mdHead].
type promotedHead struct {
	rows  []string
	cut   int
	width int
}

// liveTail is the growing edge of a streaming reply: the bytes since the last
// markdown promotion, wrapped plain and painted at the live tier.
//
// It is a method of its own rather than four lines inside [app.assistantRows]
// so that a test can ask for the same rows the renderer builds instead of
// spelling the paint out a second time — a want that reimplements its subject
// is a want that agrees with the bug.
func (a *app) liveTail(text string, width int) []string {
	rows := wrap(text, width)
	for i, line := range rows {
		// A ROW WITH NOTHING ON IT IS LEFT ALONE. Painting whitespace paints
		// nothing a person can see and costs something a person can: [trimBlanks]
		// decides what to drop by asking whether a row is blank, and a run of
		// spaces wrapped in an escape stops answering yes — which would leave the
		// gap at the foot of every reply that ends on a newline.
		if strings.TrimSpace(line) == "" {
			continue
		}
		rows[i] = a.pal.live(line)
	}
	return rows
}

// taskReplyTagRows puts the cause immediately above the answer it prompted.
// The request is copied as-is from the task record and omitted when empty.
func (a *app) taskReplyTagRows(tags []session.TaskReplyTag, width int) []string {
	var out []string
	for _, tag := range tags {
		title := taskTitleOf(tag.Title, tag.Request, tag.ID)
		body := title
		if tag.Request != "" {
			body += " · \"" + tag.Request + "\""
		}
		ident := identFor(tag.ID)
		glyph := ident.glyph
		if a.pal.ascii || a.linear {
			glyph = ident.ascii
		}
		rows := wrap(glyph+" "+body, width)
		for i, line := range rows {
			if i == 0 && strings.HasPrefix(line, glyph) {
				out = append(out, a.taskMark(ident)+a.pal.dim(strings.TrimPrefix(line, glyph)))
				continue
			}
			out = append(out, a.pal.dim(line))
		}
	}
	return out
}

// stillWorking is how long a turn has to be silent before the indicator says
// so out loud. IT IS THE LAST FALLBACK AND NOT THE FIRST ANSWER: where the
// layer holding the stream says what it is doing, the phase clock says it
// (phase.go's [phaseWords]), and where it does not the wait says what is being
// waited on ([app.waitingWords]). This word survives only where neither knows
// anything — a build with nobody posting, a stall past the freshness window, a
// turn whose request has already returned — which is the emptiness law read
// downwards: the vaguest true sentence is what is left when no better one is
// known, and it is still better than three dots.
//
// THE DEFECT THIS FIXES: the session's loop retries a failed request silently,
// with a backoff — a rate limit, a 529, a connection reset — and it says nothing
// to the surface while it does, because a retry that succeeds is not news. From
// the outside that is indistinguishable from a hang: three dots, pulsing, for
// forty seconds. The dots are the only thing on screen and they claim exactly
// as much at second one as at second forty.
//
// Ten seconds is chosen against the thing being waited on rather than against a
// person's patience: a first token from a large model on a cold cache can take
// six or seven, so below ten this would fire on ordinary turns and mean nothing.
// Past it, silence is either a retry or a very long tool-free think, and "still
// working" is true of both — which is why it says that and not "retrying". The
// surface does not know that it is retrying. It knows the stream has said
// nothing for ten seconds, and that is exactly what it claims.
const stillWorking = 10 * time.Second

// stillWorkingWord is the suffix, and the least specific thing this surface can
// truthfully say about a running turn — see [stillWorking] for why it is now
// reached last of three.
const stillWorkingWord = " · still working"

// ── THE WAIT FOR THE FIRST BYTE ─────────────────────────────────────────────
//
//	···                                                    under four seconds
//	··· waiting for kimi-k3 · 12s                          past them
//	··· waiting for kimi-k3 · 47s · nothing has come back yet
//	··· trying again · 12s                                 after a cut stream
//
// THE DEFECT THIS FIXES: [stillWorking] above is measured from [app.lastDelta],
// which is the last thing the stream SAID — so it describes a turn that spoke
// and then stopped. The state a person actually complains about is the other
// one: a request went out and the provider has not yet produced a first byte,
// for twenty seconds, for a minute. Nothing is streaming, no call is spinning,
// and the pulse claims exactly as much at second one as at second fifty. From
// where the person sits that is indistinguishable from a hung program.
//
// WHAT THE LINE IS ALLOWED TO CLAIM is bounded by what the surface can see, and
// the surface cannot see the wire. It knows a request went out ([app.awaited]),
// it knows the stream has said nothing since, and it knows which model the
// conversation is pointed at. So it says exactly that and no more: not "the
// network is slow", not "the model is thinking" — the surface does not know
// either of those and both are frequently false.
//
// "TRYING AGAIN" IS THE ONE EXCEPTION, and only because it stopped being a
// guess. The engine cuts a request that has gone quiet or come apart and says so
// (session.EventRetrying, internal/provider's streamguard.go), so on that one
// event — and never by inference from a long wait — the words change. A wait
// nobody reported a retry for still reads "waiting for", however long it runs.
//
// IT NEVER RUNS UNDER A CALL. A tool that is executing has its own spinner and
// its own count-up (toolview.go), and [app.ellipsis] has already stood the
// pulse down for it — a second clock on the same wait would be the surface
// timing a `go test` and calling it a provider.
const (
	// waitingGrace is how long a request may be outstanding before the surface
	// puts a clock on it. A FAST PROVIDER MUST NEVER SHOW ONE: a first token
	// commonly lands in one or two seconds, and a figure that appears and
	// vanishes on every ordinary turn is chrome that trains the eye to ignore
	// it — so the grace sits above the ordinary case rather than at it.
	waitingGrace = 4 * time.Second
	// waitingLong is when the wait stops being ordinary. Past thirty seconds a
	// person is no longer waiting, they are deciding whether to interrupt, and
	// the honest thing to give them for that decision is the plain fact.
	waitingLong = 30 * time.Second
)

const (
	// waitForWord opens the line when the model is known, waitBareWord when it is
	// not — the emptiness law: an unknown name renders as nothing rather than as
	// an empty slot after "waiting for".
	waitForWord  = " waiting for "
	waitBareWord = " waiting"
	// waitLongWord is the escalation, and it is a statement of fact rather
	// than an alarm: the request is out, and the answer is that nothing has
	// arrived. It deliberately does not say whose fault that is.
	waitLongWord = " · nothing has come back yet"
	// retryWord replaces the whole "waiting for <model>" half while the request
	// on the wire is a SECOND attempt. The model is not named on it: the name was
	// on the line that was just cut, and naming it again would suggest the retry
	// went somewhere else. It waits out no grace, because the person has just
	// watched an answer disappear and is owed the reason immediately.
	retryWord = " trying again"
)

// awaitingReply is THE ONE READING two rows of this surface take of the same
// moment: a request is out and nothing has come back from it yet ([app.awaited],
// app.go, which the event loop anchors on both edges).
//
// IT IS ONE FUNCTION BECAUSE TWO ROWS ASK IT. The pulse says "waiting for
// kimi-k3 · 12s" from it ([app.waitingWords]); the status line's right edge
// keeps its live rate silent through the same moment ([app.liveRiderAt]), and
// so do the per-turn burn on the sheet ([app.burnSegment]) and the served
// rider. Those were two readings of one moment, taken from different signals —
// the rate counts a whole turn's output tokens over the whole turn's wall time,
// so a turn that wrote a paragraph and then went quiet kept drawing `30 tok/s`
// two rows under this surface saying nothing had come back. A person watching a
// stalled turn read two of this program's own sentences saying opposite things,
// at the exact moment they were deciding whether to interrupt. SILENCE IS
// SILENCE ON BOTH ROWS, and it is one predicate so the two cannot drift again.
func (a *app) awaitingReply() bool {
	return a.state == stateWorking && !a.awaited.IsZero() && !a.running()
}

// waitingWords is the dim tail on the pulse while a model request is
// outstanding and the stream has said nothing at all against it, or "" when
// there is nothing to say — no turn running, a call spinning, the stream
// already speaking, or a wait still inside its grace.
//
// NOTHING NEW TICKS FOR IT. The frame clock already redraws while a turn runs —
// it is what turns the pulse — so the count-up is a function of the time at
// paint, in the spelling every other live clock on this surface uses
// ([countUpWord], toolview.go), and costs the surface no wakeup of its own.
func (a *app) waitingWords() string {
	if !a.awaitingReply() {
		return ""
	}
	waited := time.Since(a.awaited)
	if waited < waitingGrace && !a.retrying {
		return ""
	}
	word := waitBareWord
	switch {
	case a.retrying:
		// AND IT SAYS WHICH TRY, out of the struct the feed's own row was
		// composed from (failurerow.go's [failureDetail]). It is empty until the
		// engine's retry event carries the arithmetic, and an empty detail draws
		// nothing rather than an empty slot — the emptiness law.
		detail := failureDetail(a.lastAsk)
		switch {
		case modelBase(a.lastAsk.next) != "":
			// A MOVE SAYS THE MOVE AND NOTHING ELSE. The row this was composed from
			// says `moving to kimi-k3` and does not say "trying again" beside it —
			// the two readings of one moment may not disagree, which is the law this
			// line is written to. And the difference is not cosmetic once a person's
			// own word is one of the things that moves a step: somebody who picked a
			// model and read "trying again" would be told their choice was a
			// failure being recovered from (internal/session's errPersonCut).
			word = " " + detail
		case detail != "":
			word = retryWord + partDot + detail
		default:
			word = retryWord
		}
	case modelBase(a.model) != "":
		word = waitForWord + modelBase(a.model)
	}
	if clock := countUpWord(waited); clock != "" {
		word += " · " + clock
	}
	if waited >= waitingLong {
		word += waitLongWord
	}
	return word
}

// pulse is the ellipsis frame — or the still one, in the linear tier, where an
// animation is a word repeated forever.
func (a *app) pulse() string {
	if a.linear {
		return ellipsisFrames[len(ellipsisFrames)-1]
	}
	return ellipsisFrames[(a.paints/pulseStep)%len(ellipsisFrames)]
}

// ellipsis is the sign of life while a turn runs and nothing else on screen is
// moving. It is suppressed while text is actively streaming, and suppressed
// while any call is spinning: the text and the spinner each already answer "is
// this alive?", and two answers to one question is one too many.
func (a *app) ellipsis() (string, bool) {
	if !a.ellipsisShowing() {
		return "", false
	}
	return a.activityLine(a.pal.accent("  " + a.pulse())), true
}

// activityLine keeps the same truthful wait information behind either sign of
// life: the compact text sweep or the detailed transcript's existing pulse.
func (a *app) activityLine(line string) string {
	// THREE ANSWERS TO ONE QUESTION, AND THE MOST SPECIFIC ONE WINS. All three
	// say "nothing is arriving"; they differ in how much they know about why.
	//
	//	··· thinking · 12s · friendli 38 t/s   the layer holding the stream said
	//	··· waiting for kimi-k3 · 12s          a request is out, and that is all
	//	··· still working                      the stream has simply gone quiet
	//
	// The phase clock outranks both because it is the only one of the three that
	// is not an inference: it is the wire's own account of itself (phase.go),
	// and where it exists the other two are guesses about a thing somebody has
	// already told us.
	if news, ok := a.livePhase(); ok {
		if words := phaseWords(news, a.now()); words != "" {
			return line + a.pal.dim(" "+words)
		}
	}
	// THE WAIT OUTRANKS THE SILENCE, and only one of the two is ever on the
	// line. They are two ways of saying the same thing — nothing is arriving —
	// and the wait is the more specific of them: it names what is being waited
	// on and how long for, where "still working" only says that something is.
	if tail := a.waitingWords(); tail != "" {
		return line + a.pal.dim(tail)
	}
	if a.silentFor() >= stillWorking && strings.TrimSpace(ansi.Strip(line)) != "Working" {
		line += a.pal.dim(stillWorkingWord)
	}
	return line
}

// ellipsisShowing reports whether the pulse row is on the frame at all — which
// is [app.ellipsis]'s own three refusals asked as a question, so that a second
// row can find out whether the pulse is already speaking without building it.
//
// IT EXISTS FOR THE PHASE, and phase.go's words are the only thing that reads
// it. See [app.pulseHoldsThePhase].
func (a *app) ellipsisShowing() bool {
	if a.state != stateWorking || a.running() {
		return false
	}
	if a.live >= 0 && a.live < len(a.entries) && a.entries[a.live].text != "" && !a.quiet() {
		return false
	}
	return true
}

// pulseHoldsThePhase reports whether the pulse is already saying this phase, and
// it is the answer to WHICH ROW OWNS THE PHASE WORDS.
//
// THE DEFECT THIS FIXES, measured: the same sentence was drawn twice on one
// frame, verbatim, two rows apart — `·· paced · retry in 2s` on the pulse and
// `… · paced · retry in 2s` on the status line — two live things moving in
// lockstep saying one fact. A reader given the same words twice does not read
// them twice; they check whether they are the same words, which is a cost paid
// on every frame of every wait.
//
// THE PULSE WINS WHILE IT IS ON THE FRAME, for two reasons that point the same
// way. It is where the answer is about to appear, so it is where the eye
// already is; and the status line has a whole cluster of telemetry behind the
// phase on its own drop ladder — the bill, the context meter, the watch count —
// which the phase words were spending. The moment the pulse stops drawing (an
// answer is streaming, a call is spinning, the turn is over) the rider takes the
// phase up, so no state of a turn is without it. That is the whole rule: ONE
// HOME AT A TIME, and never the same words on two rows.
//
// AND A ROOM HAS NO PULSE TO HOLD IT. The pulse is a row of the CONVERSATION's
// transcript (this file's [app.deckRows]), and while a room is open the body is
// the room's own page — so there is no second row for the words to appear
// twice on, and a conversation that happens to be working behind the room must
// not silence the node's clock in front of it.
func (a *app) pulseHoldsThePhase(news PhaseNews) bool {
	if a.inlineWaitShowing || a.roomOpen() {
		return false
	}
	return a.ellipsisShowing() && phaseWords(news, a.now()) != ""
}

// hasCompactActivity asks the rows that actually drew, rather than re-deriving
// their visibility from engine state. Expanding a block returns its activity
// budget to the ordinary tool rows and footer on the very same frame.
func hasCompactActivity(rows []row) bool {
	for _, r := range rows {
		if r.activity {
			return true
		}
	}
	return false
}

// harnessStepRow is the live row under a running sub-harness's announcement:
// the step it just finished (harness.go's [app.stepHarness]).
//
// IT TAKES THE ELLIPSIS'S PLACE RATHER THAN SITTING BESIDE IT. The pulse means
// "this is alive" and so does a step landing every few seconds — and two answers
// to one question is one too many, which is the rule [app.ellipsis] is already
// written to about streaming text and spinning calls.
//
// It is drawn only while the turn is working. A step that arrived on a run whose
// turn has since ended is a row about work that is over, and the report is on
// screen by then saying what all of it did.
func (a *app) harnessStepRow(width int) (string, bool) {
	if a.state != stateWorking || a.harnessStep == "" {
		return "", false
	}
	return a.pal.dim(fit("  "+a.harnessStep, width)), true
}

// silentFor is how long the stream has said nothing. Zero when nothing has ever
// arrived, which is a turn that has not started rather than one that has stopped.
func (a *app) silentFor() time.Duration {
	if a.lastDelta.IsZero() {
		return 0
	}
	return time.Since(a.lastDelta)
}

// divider is the compaction mark: a rule with the fact in it, because a
// conversation that silently lost its middle is a conversation the person
// cannot reason about.
func (a *app) divider(hint string, width int) string {
	label := " ⚭ " + hint + " "
	rest := width - ansi.StringWidth(label) - 2
	if rest < 0 {
		return a.pal.dim(ansi.Truncate("──"+label, width, glyphMore))
	}
	left := rest / 2
	return a.pal.dim(strings.Repeat("─", left+2) + label + strings.Repeat("─", rest-left))
}

// compactRow draws one compaction pass, in the two shapes it has.
//
// RUNNING, it is the braille spinner, the session's own hint and the clock:
//
//	⠙ compacting ~84k tokens · 6s
//
// It borrows the spinner and the count-up from the tool lines rather than
// inventing an animation, because a person who has learned that a spinner means
// "this is executing right now" has learned it here too — and it turns on the
// same [spinnerStep] grid, so two moving rows on one screen never beat against
// each other. The hue is the DIM the divider wears: this is the surface talking
// about its own housekeeping, and housekeeping is never the subject. (The ask
// violet is not available to it — that hue means a person is being asked
// something, and nobody is being asked anything here.)
//
// SETTLED, it is the rule it always was, with what it cost in time:
//
//	───── ⚭ compacted from ~84k tokens · took 6s ─────
//
// The duration is dropped under a second, by the same law the tool clock uses
// ([countUpWord]'s floor): "took 0s" is a column read for nothing.
func (a *app) compactRow(e *entry, width int) string {
	if !e.ended.IsZero() {
		label := e.text
		if !e.began.IsZero() {
			if word := countUpWord(e.ended.Sub(e.began)); word != "" {
				label += " · took " + word
			}
		}
		return a.divider(label, width)
	}
	// The linear tier's objection to a spinner is the one it makes on a tool
	// line: a claim repeated thirty times a second is heard thirty times a
	// second by a surface being read aloud. A still `*` makes it once.
	mark := tokens.Spinner(a.paints / spinnerStep)
	if a.linear {
		mark = glyphRunASCII
	}
	line := mark + " " + e.text
	if word := countUpWord(a.now().Sub(e.began)); word != "" {
		line += " · " + word
	}
	return a.pal.dim(ansi.Truncate(line, width, glyphMore))
}

// ── THE BOTTOM HUD ──────────────────────────────────────────────────────────
//
// The bottom of this surface is TWO ROWS, and every element on them has exactly
// one job (foot.go states the whole law):
//
//	─ porting the parser · gpt-4.1-mini · ⠿ high · ◇ asks · via deepinfra ──── space space home · / commands ─
//	$0.14 · ⟲ saved $0.02 · 89% cached   12.4k/128k · 10%   2 jobs      92 tok/s · ⠹ working · 4s
//
// THE SEAM IS IDENTITY — which conversation, what is answering it, and the keys
// that work now. THE STATUS ROW IS NUMBERS AND ALIVENESS — the ledger on the
// left, grouped by the question each group answers, and what it is DOING on the
// right. Nothing crosses: a name never appears on the status row (a room's chip
// is the one exception, because a room renames where you are), and a number
// never appears on the seam.
//
// THE PAINT LAW: dim by default. Paint is spent on three things and nothing
// else — ALIVENESS (the spinner and its clock), DECISIONS (a question waiting,
// a context meter about to compact, a gate left open), and RECENCY (a number
// that just moved, fading back to furniture over ten seconds). A surface where
// everything is painted has told you nothing about what to look at.
//
// THE PRODUCT NAME IS GONE from this line. It was here for the screenshot —
// a terminal photograph that names everything except the program — and the
// conversation's own name on the seam identifies a pane far better than a word
// that is the same in every one of them.
//
// The two rows are drawn by [app.legend] (the input's top border, with the
// numbers on its right since 2026-09-17 — footswap.go) and [app.statusRows]
// (the last row, which is the keys).

const (
	// hudWide is where the frame is comfortable: everything is on it,
	// the session delta included.
	hudWide = 120
	// hudTight is the narrow floor: the legend loses its branch and hints while
	// the status line keeps the conversation identity.
	hudTight = 70
)

// ── AGE-FADED TELEMETRY: PAINT FOLLOWS RECENCY ──────────────────────────────
//
// A number that changed a second ago and a number that has not moved in ten
// minutes are two different facts, and they were drawn identically. So every
// telemetry segment carries the moment it last CHANGED, and it is painted from
// that:
//
//	< 4s   ink     it just moved — this is the news on the line
//	< 10s  muted   it moved recently
//	else   dim     furniture, which is what a settled number is
//
// STALE NUMBERS STOP COMPETING. That is the whole point: at rest the entire
// right cluster is one quiet grey, so the one segment that is moving is the
// only thing on the line with any weight at all.
//
// A segment's FIRST appearance is not a change — there was nothing there to
// have changed, and a line that opens entirely in ink is a line with no
// hierarchy — so it starts at the bottom of the ramp and climbs only when it
// moves again.
//
// The clock behind it costs nothing while nothing is happening: during a turn
// the frame tick already runs at [frameInterval], and at turn settle exactly
// TWO one-shot ticks are scheduled ([fadeTicks]) so the fresh tier can expire
// on time. There is no idle ticker on this surface and there is not going to
// be one.
const (
	hudFresh = 4 * time.Second
	hudWarm  = 10 * time.Second
)

// ctxRingSize is how many turn-end context readings the sparkline holds, and
// how many the ETA estimator averages over. Six is the width a sparkline can
// carry without becoming a chart.
const ctxRingSize = 6

// hudSeg names one telemetry segment. The kind is what the fade clock is keyed
// by — the segment's POSITION is not, because a segment that disappears must
// not hand its age to the one that took its place.
type hudSeg uint8

const (
	// segCrew is the crew's preset word — `crew max` — the OTHER model dial,
	// drawn at the head of the telemetry so it stands beside the conversation's
	// model across the gap (crew.go's [app.crewSegment] says why it is one word).
	segCrew hudSeg = iota
	// segOpen is how many conversations this terminal is holding, and how many
	// of them want a person (keeper.go). It is OFF THE LINE (foot.go's
	// [groupOff]): the tab strip above the transcript draws every open
	// conversation by name, so the count was the same fact said twice and the
	// weaker of the two. The phone sheet and /status still carry it.
	segOpen
	segAmbient
	// segKeeping is the standing side's own presence: how many things are
	// keeping an eye on this project (homestanding.go). It is OFF THE LINE as
	// well, and unlike the open count it went somewhere: it is a line at the
	// foot of the task column now (task.go's [app.railFootRows]), beside the
	// counts of what that column is holding, which is where the rest of this
	// project's live work is already written down.
	segKeeping
	segDelta
	segCost
	segCtx
	segCache
	segBurn
	segETA
	segYolo
	// segRate is the live rate — `38 tok/s`, what the stream is producing at
	// this moment — or the phase's own words while the turn is in one of the
	// phases that is not writing (`connecting · 1.2s`, `paced · retry in 6s`).
	// It stands beside the state word because it is a claim about NOW, and it is
	// the rider the seam's model segment used to carry ([app.liveRiderAt]).
	segRate
	// segLink is the connection under a --host session: `devbox · 3ms` after
	// its first measured round trip, or `reconnecting to devbox — trying for up
	// to 5 minutes` when that condition wins (hostlink.go). It sits immediately
	// before the state word because the two are the only segments on the line
	// that are true of the WHOLE of it — one says what the conversation is
	// doing, and this one says how the machine it is doing it on answers.
	segLink
	// segQuestions is how many decisions are waiting on this person, and the
	// chord that raises the newest one: `? 3 questions · alt+y` (question.go).
	// It sits beside [segLink] and is not in [dropOrder] for the same reason:
	// a narrow frame gives up a number rather than the one segment saying that
	// the session has stopped and is waiting for them.
	segQuestions
	segState
	segCount
)

// hudPart is one assembled segment: what it says, which clock it is on, and —
// for the segments whose painted form is not simply their text in a hue — the
// painting itself, TAKEN AT THE SAME INSTANT AS THE TEXT.
//
// THE PAINT IS CARRIED RATHER THAN ASKED FOR AGAIN, AND THAT IS THE WHOLE OF
// WHY THIS FIELD EXISTS. Every width decision this row makes is made from the
// PLAIN cluster ([app.paintParts] says so out loud): the gap between the two
// clusters, the right-aligned row's indent, and the columns the keeping and
// money doors are pressed on (standdoor.go, moneydoor.go) are all arithmetic on
// what the plain string measured. So a painted segment that measures one cell
// more than the text it was measured as makes a row a cell wider than the
// frame — and the renderer under us composes into a cell grid exactly the
// terminal's width, so what happens is not a wrap but a QUIET CUT: the last
// cell of the row is dropped, and `⠋ working · 10s` is drawn `⠋ working · 10`.
// The two doors are meanwhile one column left of where they are drawn.
//
// The state word is where that happened. It carries the turn's count-up clock,
// [app.stateSegment] reads [app.now] to build it, and `now` is `time.Now()` in
// the running program — so the layout's reading and the paint's reading are two
// reads of a moving clock a few microseconds apart, and a turn that crosses
// `9s` → `10s` between them is drawn one cell wider than it was measured.
type hudPart struct {
	kind hudSeg
	text string
	// paint is the segment as it will be DRAWN, or "" for the segments whose
	// painting is decided at paint time from state that cannot change the width
	// (the pointer, the fade ramp, the meter's heat).
	paint string
}

// status is the HUD's status row, or both of its rows joined, which is what a
// caller that wants "the line" means. The frame draws the rows themselves —
// see [app.statusRows].
func (a *app) status(width int) string {
	return strings.Join(a.statusRows(width), "\n")
}

// statusRows is the last row of the frame: THE KEYS (footswap.go's
// [app.hintRow]) — the state's own keys, the chord diagnosis, the earned tip or
// the four idle doors, one cell in, as a place's foot draws its own hint.
//
// The numbers this row carried until 2026-09-17 are on the seam over the box
// ([app.legend]); on a frame with no seam the right edge's aliveness rides
// this row's right instead. At phone width the row is still the two-row deck,
// because the seam there has no room for a number (statusdeck.go).
func (a *app) statusRows(width int) []string {
	// THE DOORS ON THIS ROW ARE CLEARED BEFORE IT IS LAID OUT and written only
	// where they landed (foot.go). The ledger's doors are the seam's now and
	// are cleared there; the deck records its own.
	a.modelSpan = hudSpan{}
	a.dockClear()
	if a.startingChat() {
		return []string{a.pal.dim(fit("New chat · first message starts the conversation", width))}
	}
	if width < 1 {
		return []string{""}
	}
	// AND AT PHONE WIDTH IT IS A DECK, deterministically two rows, because the
	// seam has nothing left to give: at forty-four columns the ledger alone is
	// wider than the frame and every number would be dropped before the first
	// segment is drawn. The deck keeps the two facts a phone can answer at a
	// glance and moves the rest into a sheet one tap away (statusdeck.go).
	if layoutTier(width) == tierPhone {
		a.doors = a.doors[:0]
		a.moneySpan, a.moneyRow = hudSpan{}, 0
		return a.statusDeck(width)
	}
	return []string{a.hintRow(width)}
}

// hudGap is the smallest barrier the two clusters will stand next to each
// other across. Below it they are not two clusters, they are one line with a
// number in the middle of it.
const hudGap = 3

// statusHeight is how many rows the HUD's status takes: the frame, the chrome
// height and the pointer's hit-testing all have to agree about it (view.go).
func (a *app) statusHeight(width int) int {
	if a.startingChat() {
		return 1
	}
	if width < 1 {
		return 1
	}
	// THE PHONE TIER ANSWERS FROM THE TIER, without laying anything out: the
	// deck is two rows at every phone-width frame, in every state
	// (statusdeck.go). Every other tier is one row — the keys — because the
	// numbers that used to wrap onto a second row are on the seam now
	// (footswap.go).
	if layoutTier(width) == tierPhone {
		return deckHeight
	}
	return 1
}

// identity is the left cluster: WHICH conversation, and WHAT is answering it.
//
//	porting the parser · gpt-4.1-mini:high
//
// The model is its BASENAME. "deepseek/deepseek-v4-flash" is a routing address
// and its first half is the same for every model a person is choosing between —
// nine cells that never vary, on the row where width is scarcest. The whole id
// stays wherever it is being CHOSEN or RECORDED: the picker's rows, the /model
// note, the session file. The reasoning rider is kept because it is not part of
// the address — it is how this model is being run (view.go's [app.statusRow]).
//
// The name falls back to the workspace's base name until the session has named
// itself (session's title.go), so the cluster is never empty.
func (a *app) identity() string {
	text, _ := a.identityParts(0)
	return text
}

// identityParts is that cluster and the COLUMNS ITS MODEL SEGMENT OCCUPIES on
// the row, because the model segment is a thing you can press: the whole point
// of a name on screen is that it is where a person already looks when they want
// to change it, and until this wave the only door was typing /model.
//
// The span is [from, to) in cells from the row's left edge, which is where this
// cluster is drawn. An empty span (to == 0) means there is nothing to press — a
// session with no model yet, or a room whose node is past being moved: the press
// always acts on WHAT THE ROW NAMES, so in a room it is the node's model and out
// here it is the conversation's, and neither can ever be mistaken for the other.
// roomTotalsWord labels the numbers as the conversation's while the organized
// room's header is carrying the node's own identity.
const roomTotalsWord = "Conversation totals"

func (a *app) identityParts(width int) (string, hudSpan) {
	if a.roomOrganized() {
		return roomTotalsWord, hudSpan{}
	}
	// A ROOM RENAMES THIS CLUSTER AND NOTHING ELSE ON THE LINE. The identity is
	// WHERE YOU ARE, and while a room is open where you are is a task — but the
	// telemetry beside it is still the session's, because a room is a view over
	// one body region and not a second session (room.go). A status line that
	// re-pointed the cost and the context meter at a node would be quoting
	// figures nobody is measuring.
	//
	// AND THE MODEL SEGMENT NAMES THE ROOM'S NODE. The status row is ABOUT THE
	// WINDOW, and while a room is open the window IS that task — so the law above
	// argues FOR this and not against it. What the law forbids is re-pointing the
	// TELEMETRY, which measures the session and would be quoting figures nobody
	// took; the node's model is not a measurement, it is a fact the node
	// published. It is said here because this is the only ALWAYS-VISIBLE model
	// name on the screen, and a person who launched a task on one model, opened
	// its room, and read the conversation's model at the foot of the frame was
	// told the wrong thing by the one line they could not look away from.
	//
	// AND WHILE A ROOM IS OPEN THE SEGMENT IS A DOOR ONTO THAT NODE'S OWN MODEL —
	// never onto the conversation's. The two are one gesture over two subjects,
	// which is the only reading of "press the name to change it" that stays true
	// wherever the name is: what the row names is what the press moves. A picker
	// opened from in here retargets THIS node from its next turn on and touches
	// neither the conversation nor any other task (room.go's [app.retargetTask],
	// internal/session's [Agent.RetargetTask]).
	//
	// THE SPAN IS EMPTY WHENEVER THE PICK COULD NOT LAND, which is the design law
	// rather than a special case: a capability that cannot work is absent, not
	// broken. A node that has finished, failed, been stopped or needs a look has a
	// model that is a FACT about what happened — nothing can move it and the
	// engine refuses to try — so the name is still drawn and simply cannot be
	// pressed ([app.roomModelMovable] holds the whole of that list). An
	// affordance that lit up and then apologised would be worse than none.
	if a.roomOpen() {
		// THE ROW NAMES THE TASK AND NOTHING ELSE. The node's model, its
		// machine, its rung and its gate are on the SEAM over the box now —
		// the same four cells the conversation's seam carries, with the same
		// doors (roomseam.go) — so the row that used to carry `· task glm-5.2 ·
		// via deepinfra` beside the name, and the ladder that gave those up
		// under width, carries the name alone. One fact, one place.
		//
		// THE NAME IS WHOLE UNTIL THE LINE CANNOT HOLD IT, and then it is cut
		// to what the row has, never below [roomChipFloor] — under that the
		// cluster has stopped saying where you are and the ladder above has to
		// find its cells somewhere else.
		cluster := a.roomChip(0)
		if width > 0 && ansi.StringWidth(cluster) > width {
			room := width
			if room < roomChipFloor {
				room = roomChipFloor
			}
			cluster = a.roomChip(room)
		}
		return cluster, hudSpan{}
	}
	name := a.sessionName()
	if name == "" {
		name = a.place
	}
	// THE MODEL IS THE CHROME'S WORD FOR IT, pin and all ([app.modelWord]).
	model := a.modelWord()
	if model == "" {
		return name, hudSpan{}
	}
	// The RIDER IS PART OF THE TARGET. "via deepinfra · 92 tok/s" is a fact
	// about the model that is answering, so a person pressing it means the same
	// thing they mean by pressing the id.
	//
	// AND THE RIDER IS THE HALF THE WIDTH IS TAKEN OUT OF. The conversation's
	// name and the model's are what the cluster IS; the rider is what is
	// happening to it, and it is the only part of the line that has a shorter
	// true spelling to fall back on. So the columns left after the two names are
	// the rider's budget, and a width of zero or less is no budget at all — the
	// reading every caller that is not laying out the status row wants
	// ([app.identity]).
	segment := model
	switch {
	case width <= 0:
		segment += a.servedRiderAt(-1)
	default:
		if room := width - ansi.StringWidth(name+" · "+model); room > 0 {
			segment += a.servedRiderAt(room)
		}
	}
	from := ansi.StringWidth(name + " · ")
	return name + " · " + segment, hudSpan{from: from, to: from + ansi.StringWidth(segment)}
}

// hudSpan is a pressable stretch of the status row: [from, to) cells on it.
// It is the same shape a proposal's choices row carries for its three options
// (task.go's [choiceSpan]) and it is written and read the same way — by the
// render, then by the hit-testing — so a click can never land on a segment the
// frame drew somewhere else.
type hudSpan struct{ from, to int }

// pressable reports whether this span has any columns in it.
func (s hudSpan) pressable() bool { return s.to > s.from }

// holds reports whether a column is inside the span.
func (s hudSpan) holds(x int) bool { return s.pressable() && x >= s.from && x < s.to }

// servedSighting is the HUD's window onto the adapter's velocity ledger. It is
// a var so a test can state one sighting without a live endpoint; nothing else
// ever reassigns it.
var servedSighting = provider.LastServed

// servedWindow is how long a sighting still describes the present. Past it the
// rider goes quiet rather than keeping a rate from a conversation that has
// since gone to sleep on the line.
const servedWindow = 10 * time.Minute

// servedRider is the other half of the model's name: WHO ACTUALLY ANSWERED, and
// how fast they were writing.
//
//	deepseek-v4-flash · via deepinfra · 92 tok/s
//
// A model id is an address, not a machine. One id is fanned over many endpoints
// that answer at very different speeds for the same price, and which of them
// answered is decided by the router unless somebody's routing row says otherwise
// (internal/provider's velocity.go). The id alone therefore names a decision the
// session did not make — this is the part of it that is a fact.
//
// It is drawn only when the server's name is not already the model's own: an
// endpoint that IS the vendor adds nothing to "deepseek-v4-flash", and a line
// that reads "gpt-4.1 · via openai" is a cell of chrome per frame for a word
// the reader already has.
//
// THE LIVE PHASE OUTRANKS BOTH OF THE READINGS UNDER IT (phase.go's
// [app.livePhase]), and the ranking is by TENSE. A phase is what this request is
// doing right now; the lane news is what the last answer did; the sighting is
// what some answer did within the last ten minutes. Drawing the older one beside
// the newer is how the row came to say a finished answer's lane and throughput
// under a request that had been stalled for a minute, which is the defect the
// phase clock was built for.
//
// THE LANE LAYER SPEAKS SECOND WHEN IT HAS SPOKEN AT ALL (lanes.go's
// [app.laneRider]). It knows three things the sighting cannot — the first-token
// wait, that a rescue is in flight, and that one landed — and the two must not
// both draw, or the row would say `via` twice about one answer. Where nothing
// has posted, this is exactly the rider it has always been.
func (a *app) servedRider() string { return a.servedRiderAt(-1) }

// riderLead is the separator the rider hangs off the model's name by, and the
// cells a bounded rider has to pay for before it may say anything at all.
const riderLead = " · "

// servedRiderAt is that rider in the widest spelling that fits in the columns
// the row has left for it. A width below zero is no bound, which is what
// [app.servedRider] asks for and what every reading that is not on the status
// row wants.
//
// THE SEGMENT DEGRADES BY WHAT ITS PARTS ARE WORTH, NEVER BY WHERE THE ROW
// ENDS. A clip takes the tail, and the tail of this segment is the rate — which
// is right, once — and then it takes the machine's name, the phase and the
// clock together, in one step, and leaves an ellipsis standing where a shorter
// true sentence would have fitted. So a narrow row is handed the segment as
// DATA (phase.go's [phaseSegment]) and given the best rung of it that fits.
func (a *app) servedRiderAt(width int) string {
	room := width
	if room >= 0 {
		room -= ansi.StringWidth(riderLead)
		if room < 0 {
			room = 0
		}
	}
	if news, ok := a.livePhase(); ok && !a.pulseHoldsThePhase(news) {
		// THE RATE RIDES ONLY WHILE A TURN IS RUNNING, which is the rule both
		// readings below already keep: what a phase IS remains attribution, and
		// how fast it was writing is a claim about now. Zeroing it here rather
		// than inside the words keeps that rule in one place per rider.
		if a.state != stateWorking {
			news.Rate = 0
		}
		if words := rowLed(phaseFields(news, a.now()), roomFor(room)); words != "" {
			return riderLead + words
		}
	}
	// THE LANE LAYER'S OWN RIDER IS NOT ON THE LADDER YET, and it is the one
	// segment on this row that a narrow frame can still clip. Its three states
	// are assembled in lanes.go, which this wave does not own; the rebase that
	// lands internal/tui3/rowfit.go is where it becomes a [phaseSegment] like
	// the two around it.
	if rider := a.laneRider(true); rider != "" {
		return rider
	}
	sighting, ok := servedSighting(a.model)
	if !ok || sighting.Provider == "" {
		return ""
	}
	if a.now().Sub(sighting.At) > servedWindow {
		return ""
	}
	served := strings.ToLower(sighting.Provider)
	if strings.Contains(strings.ToLower(a.model), served) {
		return ""
	}
	// The name leads and the rate is the field after it, which is this segment's
	// whole hierarchy: who answered is the fact, how fast is the measurement.
	fields := []rowField{rowSay("via "+served, served)}
	// THE RATE RIDES ONLY WHILE A TURN IS RUNNING. Who served is attribution
	// and stays; how fast they were writing is a claim about NOW, and a rate
	// from the last turn standing on an idle status line read as a live figure
	// nobody was producing — a person sat looking at "92 tok/s" over a chat
	// that was doing nothing.
	// AND IT DOES NOT RIDE WHILE THIS SURFACE IS SAYING NOTHING HAS COME BACK,
	// for [app.burnSegment]'s reason and out of the same reading: the served
	// rate is the layer's figure for a stretch that is over, and drawn beside
	// the pulse's "nothing has come back yet" it is the same contradiction said
	// by a second row.
	if sighting.Rate > 0 && a.state == stateWorking && !a.awaitingReply() {
		fields = append(fields, rowSay(tokenWord(int(sighting.Rate))+" tok/s"))
	}
	if words := rowLed(fields, roomFor(room)); words != "" {
		return riderLead + words
	}
	return ""
}

// modelRiderAt is the half of the rider that is ATTRIBUTION — who is answering
// for this model — and it rides the model's name on the seam, inside the
// model's own cell:
//
//	glm-5.3-flash (deepinfra)
//	glm-5.3-flash (z-ai)
//	glm-5.3-flash · slow · trying coreweave…
//
// It never carries a rate. How fast the machine is writing is a claim about now
// and stands beside the state word instead ([app.liveRiderAt]).
//
// IT IS WHOLE OR NOTHING, AND IT DOES NOT AGE OUT. Until 2026-09-17 it was
// ` · via deepinfra`, said as `· deepinfra` on a tight line and gone ten
// minutes after the last answer ([servedWindow]). The owner ruled that the
// machine is written beside the model as one word — `deepseek-v4.1-flash
// (baidu)` — and that the last machine to answer stays named until another
// does: a conversation read half an hour later still ran on that machine.
// The sheet's `served` row keeps the window, because the figures beside its
// name are about one answer ([app.servedRiderAt]).
//
// IT IS DRAWN WHOEVER SERVED, AND THAT IS THE OWNER'S OWN RULING (2026-09-09).
// It used to go silent when the server's name was already inside the model id —
// `z-ai/glm-5.3-flash` answered by z-ai — on the argument that the reader
// already had the word. On this line they do not: the seam spells the model as
// its BASENAME ([modelBase]), so the vendor half of the id is not on the screen
// at all, and a rider that appeared for one endpoint and vanished for another
// read as the sighting having been lost rather than as the vendor having served
// its own model. The sheet's `served` row keeps the old rule, because the row
// above it there is the model's WHOLE routing address (statusdeck.go).
//
// THE RULING WAS KEPT HERE AND BROKEN ONE CALL DOWN until 2026-09-10: the lane
// layer's rider, which speaks first, still went silent for a vendor serving its
// own model, and the ledger fallback below it is in-process only — so over an
// engine host, where the ledger is filled in the engine's process and never in
// this one, `deepseek/…` served by DeepSeek drew no `via` at all. The seam now
// asks [app.talkLaneRider], which never suppresses and which also names the
// machine the request in flight is on, so `via` is there from the first answer.
func (a *app) modelRiderAt(width int) string {
	rider := a.talkLaneRider()
	if rider == "" {
		sighting, ok := servedSighting(a.model)
		if !ok || sighting.Provider == "" {
			return ""
		}
		rider = riderWords(riderBeside, strings.ToLower(sighting.Provider), "")
	}
	if width < 0 || ansi.StringWidth(rider) <= width {
		return rider
	}
	return ""
}

// liveRiderAt is the half of the rider that is ABOUT NOW, drawn beside the
// state word while a turn is running:
//
//	38 tok/s                     while the answer is being thought or written
//	connecting · 1.2s            while the turn is in a phase that is neither
//	first word · 3.1s → parasail at 4.4s
//	paced · retry in 6s
//
// THE RATE IS WHAT THE STREAM IS PRODUCING RIGHT NOW, and nothing else is
// allowed on the right edge under that name. It is [PhaseNews.Rate] — tokens
// over elapsed, measured on the live stream by the layer holding it
// (internal/provider's phase.go) and posted again every second while it lasts —
// so it is drawn ONLY while it is being measured, and the line is empty the
// moment it is not.
//
// TWO OTHER FIGURES WERE HERE AND BOTH WERE ABOUT THE PAST. The served
// sighting's rate is what SOME answer within the last ten minutes averaged, and
// it sat on an idle line as a live figure nobody was producing; the per-turn
// burn ([app.burnSegment]) is a turn's whole output over a turn's whole wall
// time, waits and tool calls included, which is a different quantity from "how
// fast is this writing" and reads low by a factor of several on any turn that
// ran a tool. The owner rejected both on 2026-09-09. The burn is still on
// /status and the phone sheet under its own label, where it is not claiming to
// be now.
//
// WHILE IT WRITES, THE RATE IS THE WHOLE SEGMENT. `writing · 4s · friendli 61
// t/s` said three things a person already had: the state word two runs to the
// right says `⠹ working · 4s`, and who is serving is on the seam. What the row
// has that nothing else does is the speed. Every OTHER phase keeps its words,
// because in those the turn is not producing anything and the phase is the only
// thing on the frame saying it is alive at all.
//
// AND IT IS THE WINDOW'S OWN WORK, NEVER THE SESSION'S (phase.go's
// [app.windowPhase] and the law above it). While a room is open the work in
// front of the person is that node, so this edge is the node's rate — which is
// the whole of why a room used to show none: the figure was the conversation's,
// and a conversation that has handed a task out is idle.
func (a *app) liveRiderAt(width int) string {
	news, ok := a.windowPhase()
	if !ok || a.pulseHoldsThePhase(news) {
		return ""
	}
	switch news.Phase {
	case provider.PhaseThinking, provider.PhaseWriting:
		// AND NOT WHILE THIS SURFACE IS SAYING NOTHING HAS COME BACK
		// ([app.awaitingReply] states the whole of why): one moment, one reading,
		// and a rate quoted beside the pulse's own silence is this program
		// contradicting itself out loud. The emptiness law does the rest — a rate
		// nobody has measured yet is nothing, never `0 tok/s`.
		if !a.windowWorking() || news.Rate <= 0 {
			return ""
		}
		return rowLed([]rowField{rowSay(tokenWord(int(news.Rate)) + " tok/s")}, roomFor(width))
	}
	return rowLed(phaseFields(news, a.now()), roomFor(width))
}

// roomFor turns this file's "below zero is no bound" into rowfit.go's own
// spelling of the same thing, which is a very large number rather than a
// special case ([rowUnbounded]). One code path fits every row.
func roomFor(width int) int {
	if width < 0 {
		return rowUnbounded
	}
	return width
}

// modelBase strips the vendor from a model id, and nothing else: everything
// after the last slash, which leaves a bare id alone and keeps a ":level" rider
// (the rider is appended after the id, and the slash is before it).
func modelBase(id string) string {
	if at := strings.LastIndexByte(id, '/'); at >= 0 {
		return id[at+1:]
	}
	return id
}

// costFloorCells is the widest the bill can be written while it is still under
// a cent: settingspend.go's floor spelling, `<$0.0001`, asked of the function
// that prints it rather than counted here — one source of truth for a width two
// files would otherwise both know.
var costFloorCells = ansi.StringWidth(subCent(0))

// costCell is the bill with the room it is going to need already under it.
//
// THE LAW THIS EXTENDS IS THE ONE THIS LINE ALREADY HAS. The live status row
// keeps `$0.00` rather than drawing nothing — the emptiness law's one sanctioned
// exception (app.go's [dollars]) — and the whole reason is that the segments to
// the right of the bill must not jump sideways while a person is reading them.
// The exception held the segment's PRESENCE still and let its WIDTH move, so one
// turn walked `$0.00` (five cells) → `<$0.0001` (eight) → `$0.0052` (seven) →
// `$0.01` (five) and shoved the context meter, the watch count and the state
// word two and three columns each way inside a few seconds.
//
// SO THE FIGURE IS RIGHT-ALIGNED IN THE ROOM ITS OWN SPELLINGS NEED, and no
// second exception is invented for it: nothing is drawn that was not drawn
// before, no zero stands in for an unknown, and `$0.00` is still the only zero
// on the line. The reservation is a pure function of the amount and it cannot
// shrink as a session spends, because the two things it is the larger of never
// shrink either — the sub-cent floor is a constant, and the two-place spelling
// only widens as the bill climbs a decade. A bill that reaches $9,999.99 takes
// its ninth cell once and keeps it.
func costCell(usd float64) string {
	word := dollars(usd)
	room := costFloorCells
	if cells := ansi.StringWidth(word); cells > room {
		room = cells
	}
	if pad := room - ansi.StringWidth(word); pad > 0 {
		return strings.Repeat(" ", pad) + word
	}
	return word
}

// splitReserve takes [costCell]'s reservation off the front of a segment: the
// room, which is space, and the figure, which is the only part of it anything
// paints. The width arithmetic above still reads the whole string — the
// reservation is real cells and every measurement of the row has to see them —
// so the split happens at the last possible moment, in the paint.
func splitReserve(text string) (room, figure string) {
	at := 0
	for at < len(text) && text[at] == ' ' {
		at++
	}
	return text[:at], text[at:]
}

// telemetry assembles the right cluster IN ORDER, and the order is the question
// each segment answers about the run:
//
//	2 jobs · 1 watch     what is still alive out there
//	Σ +128 −14           what this session has written
//	$0.14                what it has cost
//	12.4k/128k · 10% ▁▂▃ what it is carrying, and where that has been going
//	⟲ saved $0.02 · 89%  what the cache gave back
//	1.2k tok/s avg       output over this turn's elapsed time
//	compaction in ~3     what is about to happen to it
//	YOLO                 the gate is open — only on the frames whose seam is
//	                     not carrying the approvals chip (approvalchip.go);
//	                     the phone sheet says the posture at every posture
//	38 tok/s             what the stream is producing right now
//	⠹ working · 4s       what it is DOING — always last, because it is the one
//	                     segment that is true of the whole line
func (a *app) telemetry(width int) []hudPart {
	// THE ROW'S OWN CAPACITY, TAKEN ONCE. There are [segCount] kinds and never
	// more, and growing this slice a segment at a time is three allocations on
	// a line rebuilt every frame — which the scroll's law counts
	// (inputsmooth_test.go's
	// [TestOneScreenScrollOfFourThousandLinesStaysInsideTheAllocationLaw]).
	parts := make([]hudPart, 0, segCount)
	add := func(kind hudSeg, text string) {
		if text != "" {
			parts = append(parts, hudPart{kind: kind, text: text})
		}
	}
	// THE BILL IS THE WHOLE TREE'S and not the conversation's own half of it: the
	// work this conversation started is spending its money, and a segment that
	// waited for each task to close said `$2.53` for two hours over a family
	// burning $51.05 (treespend.go's [app.spendShown]).
	add(segCost, costCell(a.spendDrawn()))
	add(segCache, a.warmSegment())
	if context, _ := a.contextSegment(); context != "" {
		add(segCtx, context)
	}
	add(segETA, a.etaSegment())
	add(segAmbient, a.ambientSegment())
	add(segYolo, a.approvalSegment())
	// THE FACTS OFF THE LINE. The crew word, the session delta, the per-turn
	// burn, the open count and the standing count are not drawn on the status
	// row (foot.go's [groupOff]) — the phone sheet and /status still say all
	// five, and this list is where both read.
	add(segCrew, a.crewSegment())
	add(segDelta, a.deltaSegment())
	add(segBurn, a.burnSegment())
	add(segOpen, a.openSegment())
	add(segKeeping, a.keepingSegment())
	add(segRate, a.liveRiderAt(-1))
	// A LINK THAT HAS STOPPED WORKING OUTRANKS EVERY NUMBER ON THIS LINE, and
	// says so by never being dropped: it is not in [dropOrder], so a narrow
	// frame gives up the ledger around it rather than the one segment that
	// explains why none of those numbers are moving (hostlink.go).
	add(segLink, a.linkSegment())
	// AND A DECISION WAITING ON A PERSON OUTRANKS EVERY NUMBER ON IT for the
	// link's own reason said one rung louder: the numbers are not moving, and
	// this is the segment that says whose move it is (question.go).
	add(segQuestions, a.questionSegment())
	if word, painted := a.stateSegment(); word != "" {
		parts = append(parts, hudPart{kind: segState, text: word, paint: painted})
	}
	return parts
}

// statusQuiet reports whether the status row is the untouched screen's: the
// greeting is up, so nothing has been said, sent, spent or produced here yet.
//
// THE BILL AND THE METER ARRIVE WITH THE CONVERSATION. On the empty screen there
// is nothing to bill and nothing but the prompt to meter, and `$0.00 · 9.4k/1.3M
// · 1%` under a greeting asking for a first sentence was the first thing a
// person on their own card read. The `$0.00` exception ([app.statusRows]'s law)
// is untouched: it is about a session IN USE not having its segments jump
// sideways, and from the first keystroke on — the frame on which the greeting
// dissolves and the box moves anyway — every row is drawn exactly as it always
// was.
//
// IT IS THE GREETING'S OWN STATE AND NOT A COUNT OF ANYTHING, for the reason the
// column and the legend read the same state (task.go's [app.railQuiet],
// view.go's [app.chrome]): the empty screen is one condition, and four pieces
// of furniture that each derived "empty" their own way would come and go on
// four different frames. And it governs THE ROW AND THE DECK AND NOTHING ELSE
// — the status sheet and /status read the full segment set (statusdeck.go's
// [app.deckItems], statusnote.go), because a person who asked what the context
// holds is owed the prompt's size; only the row nobody asked is kept quiet.
func (a *app) statusQuiet() bool { return a.welcome.open }

// quietParts is the segment set with the bill and the meter taken out, for a
// row that is [app.statusQuiet].
func quietParts(parts []hudPart) []hudPart {
	kept := parts[:0]
	for _, part := range parts {
		if part.kind == segCost || part.kind == segCtx {
			continue
		}
		kept = append(kept, part)
	}
	return kept
}

// openSegment is how many conversations this terminal holds and how many of
// them want somebody:
//
//	2 open · 1 waiting
//
// IT IS ABSENT WHENEVER ONLY ONE IS OPEN, which is the ordinary case and the
// emptiness law's plainest application — a permanent `1 open` would be a
// permanent reminder of the absence of a feature. The `· N waiting` clause is
// absent when nothing is waiting, on the same terms.
//
// The deliberate $0.00 exception on this line is NOT extended here. That
// exception exists so a cost segment does not jump sideways as its width
// changes; this one appears and disappears with a real change in what is true,
// and a placeholder would be a lie about how many conversations are open.
//
// THE COUNT IS ASKED OF THE AGENTS AND NOT OF THE PRESENCE FILE, for the reason
// home's own rows are ([app.homeTrue]): the file lags by up to five seconds, and
// this is a pointer we are holding.
func (a *app) openSegment() string {
	open := a.openCount()
	if open < 2 {
		return ""
	}
	word := itoa(open) + " " + homeOpenWord
	if waiting := a.waitingCount(); waiting > 0 {
		word += " · " + itoa(waiting) + " waiting"
	}
	return word
}

// paintPart is where the hue budget is spent, and the order of these branches
// IS the priority of the three things paint is allowed to mean.
func (a *app) paintPart(part hudPart) string {
	// THE PAINTING THAT CAME WITH THE TEXT IS THE PAINTING THAT IS DRAWN. A
	// segment whose painted form was decided when its text was — the state word
	// and its clock — hands it over here rather than being asked to build it a
	// second time from a clock that has moved on ([hudPart]).
	if part.paint != "" {
		return part.paint
	}
	// The standing count is not here any more: it is a line at the foot of the
	// task column, and it is painted where it is drawn (task.go's
	// [app.railStandingLine]).
	switch part.kind {
	case segCost:
		// MONEY IS A DOOR AND A BOUND, and this is the only segment on the line
		// that can be both (moneydoor.go).
		//
		// It brightens under the pointer for the keeping segment's reason — a
		// label that is also a control has to say so — and it takes the WARM ink
		// at four fifths of this conversation's own ceiling, which is a glance
		// and not an alarm: a bound about to be reached is not a failure and must
		// not wear the failure hue. The pointer outranks the warning, because
		// while somebody is about to press it the fact worth saying is that it
		// opens.
		//
		// AND THE ROOM THE SEGMENT RESERVES IS NOT PAINTED WITH THE FIGURE.
		// [costCell] holds this segment's width still by putting the room it will
		// need in front of the figure, and the room is SPACE — it has no ink and
		// it is not part of what the age ramp, the pointer or the bound are
		// talking about. Painting it into the same span would make the segment's
		// paint depend on how much money had been spent, which is what the ramp
		// exists to say something else about (bundle_test.go's fade and hue tests
		// read exactly this span, and they are the ones that named it).
		room, figure := splitReserve(part.text)
		switch {
		case a.hoveringMoney():
			return room + a.pal.accent(figure)
		case a.moneyNearRail():
			return room + a.pal.warn(figure)
		}
		return room + a.fadeSeg(part.kind, figure)
	case segQuestions:
		// AMBER, AND THE THIRD SEGMENT THE AGE RAMP HAS NOTHING TO SAY ABOUT.
		// It is true while it is drawn and gone the instant it is not, so "this
		// changed four seconds ago" is not a fact about it — and it is loud for
		// what it MEANS rather than for when it changed, which is [segYolo]'s
		// own argument at the one hue this surface reserves for a person being
		// waited on (question.go, styles.go's [hueWarn]).
		return a.pal.warn(part.text)
	case segYolo:
		// The one segment that is loud because of what it MEANS rather than
		// because of when it changed. It is a reading here and not a door: the
		// control is the chip on the seam (approvalchip.go).
		return a.pal.bad(part.text)
	case segCache:
		// THE CACHE IS THE OTHER HALF OF THE MONEY DOOR: it brightens with the
		// bill beside it, because pressing either opens the same tab.
		if a.hoveringMoney() {
			return a.pal.accent(part.text)
		}
	case segETA:
		if a.hoveringMeter() {
			return a.pal.accent(part.text)
		}
	case segLink:
		// THE SECOND SEGMENT THE AGE RAMP HAS NOTHING TO SAY ABOUT. It is true
		// for as long as it is drawn and false the instant it is not, so "this
		// changed four seconds ago" is not a fact about it — and the ramp would
		// paint it dim forever anyway, because a segment's FIRST appearance
		// never stamps a clock ([app.freshen] says why).
		//
		// ACCENT AND NOT [palette.bad] WHILE REDIALLING: it is expected, bounded
		// and usually resolves itself. A healthy round-trip reading is ordinary
		// telemetry and stays dim; the words, not paint alone, distinguish them.
		if a.linkNote() != "" {
			return a.pal.accent(part.text)
		}
		return a.pal.dim(part.text)
	case segCtx:
		// The meter's three-rung ramp outranks its age: a conversation about to
		// compact is a decision a person can still act on, and "this number is
		// four seconds old" is not. The pointer outranks both, because while
		// somebody is about to press it the fact worth saying is that it opens.
		if a.hoveringMeter() {
			return a.pal.accent(part.text)
		}
		switch a.ctxHeat() {
		case ctxDue:
			return a.pal.bad(part.text)
		case ctxNear:
			return a.pal.accent(part.text)
		}
	}
	return a.fadeSeg(part.kind, part.text)
}

// freshen stamps the change clocks. A segment that vanished loses its clock
// rather than keeping it: the next thing to appear under that kind is new, and
// new is not the same as recently changed.
func (a *app) freshen(parts []hudPart) {
	var seen [segCount]bool
	for _, part := range parts {
		if part.kind >= segCount {
			continue
		}
		seen[part.kind] = true
		if a.segText[part.kind] == part.text {
			continue
		}
		if a.segText[part.kind] != "" {
			a.segAt[part.kind] = a.now()
		}
		a.segText[part.kind] = part.text
	}
	for kind := hudSeg(0); kind < segCount; kind++ {
		if !seen[kind] {
			a.segText[kind], a.segAt[kind] = "", time.Time{}
		}
	}
}

// fadeSeg paints one segment at its age (see the ramp above).
//
// WHILE A PERSON IS BEING ASKED SOMETHING, the whole ramp collapses to dim. The
// consent question owns the screen's attention for as long as it is up, and a
// cost figure glowing beside it is a number competing with a decision.
func (a *app) fadeSeg(kind hudSeg, text string) string {
	if a.asking() || kind >= segCount {
		return a.pal.dim(text)
	}
	at := a.segAt[kind]
	if at.IsZero() {
		return a.pal.dim(text)
	}
	switch age := a.now().Sub(at); {
	case age < hudFresh:
		return a.pal.ink(text)
	case age < hudWarm:
		return a.pal.muted(text)
	default:
		return a.pal.dim(text)
	}
}

// rightAlign pushes a painted cluster to the right edge of the frame.
func rightAlign(painted, plain string, width int) string {
	if gap := width - ansi.StringWidth(plain); gap > 0 {
		return strings.Repeat(" ", gap) + painted
	}
	return fit(painted, width)
}

// ── THE TELEMETRY SEGMENTS ──────────────────────────────────────────────────

// ambientSegment is what is still alive that nobody is watching:
//
//	2 jobs · 1 watch
//
// It exists because background work is the one thing on this surface that
// happens OFF the transcript. A server started twenty turns ago is not on
// screen, is not in the reply, and is still holding a port; a watch is still
// firing into the conversation. Zero of both is the ordinary case and it draws
// NOTHING — an ambient count that reads "0 jobs" is a permanent reminder of the
// absence of a thing.
func (a *app) ambientSegment() string {
	stats := a.hudStats()
	var parts []string
	if stats.jobs > 0 {
		parts = append(parts, itoa(stats.jobs)+plural(" job", stats.jobs))
	}
	if stats.watches > 0 {
		parts = append(parts, itoa(stats.watches)+plural(" watch", stats.watches, "es"))
	}
	return strings.Join(parts, " · ")
}

// deltaSegment is what this SESSION has written, summed from the same tool
// arguments the per-turn "what changed" line is derived from (app.go's
// [app.computeStats]):
//
//	Σ +128 −14
//
// The per-turn line answers "what did that do"; this answers "what has this
// conversation done", which is the question a person asks before they decide
// whether to keep it. It is the first segment sacrificed to width because it is
// the only one on the line that is about the PAST rather than about now.
func (a *app) deltaSegment() string {
	stats := a.hudStats()
	if stats.adds == 0 && stats.dels == 0 {
		return ""
	}
	return "Σ " + glyphAdd + itoa(stats.adds) + " " + glyphDel + itoa(stats.dels)
}

// ctxSpark is the last few turn-end context readings, as one glyph each:
//
//	context   12.4k/128k · 10% ▁▂▂▃▅▆
//
// IT RIDES THE METER ON THE SHEET AND ON /status, NOT ON THE ROW. It was beside
// the meter on the status line until 2026-09-09, where it was six cells of
// trend on a line that is read at a glance and acted on segment by segment —
// nothing on the row does anything with a shape. It is on the one page a person
// opens BECAUSE they want the shape (statusdeck.go's [app.deckItems]).
//
// It answers the question the number cannot: a conversation at 60% that has sat
// at 60% for six turns and one that arrived there from 20% are the same figure
// and completely different situations. The bars are measured against the
// COMPACTION THRESHOLD rather than the window, for the reason the heat ramp is
// ([accentAtThresholdPercent]): the threshold is the thing that actually
// happens to you.
//
// Fewer than two readings draws nothing. One bar is not a trend, it is a bar.
func (a *app) ctxSpark() string {
	// A SPARKLINE IS SHAPE, and the two tiers that cannot read shape do not get
	// one: a terminal that cannot be trusted with box drawing would render six
	// replacement characters, and a surface being read aloud would announce
	// them one by one. Both keep the number, which is the fact.
	if a.pal.ascii || a.linear {
		return ""
	}
	threshold := session.CompactThresholdFor(a.model, a.ctxWindow)
	if threshold <= 0 || len(a.ctxRing) < 2 {
		return ""
	}
	// THE SHAPE IS THE SPARK MACHINERY'S AND NOT THIS FUNCTION'S (spark.go), so
	// that one rounding rule answers "how tall is this sample" for every spark on
	// this surface. The ceiling is STATED here, because a context bar has to mean
	// the same thing from one turn to the next.
	return barSpark(a.ctxRing, threshold, len(a.ctxRing))
}

// burnSegment is the output rate averaged over this turn, including tool and
// model waiting time rather than only time spent generating tokens:
//
//	1.2k tok/s avg
//
// It is output tokens over the wall time of THIS turn, and it exists because
// "working" is a boolean and a person watching a long turn wants a rate. It is
// drawn only while a turn is running — a rate over a finished turn is a fact
// about the past wearing the clothes of a live one — and only after a second,
// because a rate computed over 200ms is a rate computed over the first packet.
func (a *app) burnSegment() string {
	if a.state != stateWorking || a.turnBegan.IsZero() {
		return a.holdBurn("")
	}
	elapsed := a.now().Sub(a.turnBegan)
	if elapsed < time.Second {
		return a.holdBurn("")
	}
	// AND A RATE IS NOT DRAWN WHILE THIS SURFACE IS SAYING NOTHING HAS COME BACK
	// ([app.awaitingReply] states the whole of why). The figure below is the
	// turn's output over the turn's wall time, which is a fact about a stretch
	// that has already ended once the stream has gone quiet — and the pulse two
	// rows up is meanwhile naming what is being waited on.
	if a.awaitingReply() {
		return a.holdBurn("")
	}
	written := a.outputTokens - a.turnOutStart
	if written <= 0 {
		return a.holdBurn("")
	}
	// THE EMPTINESS LAW IS ASKED OF THE FIGURE THAT IS DRAWN, not of the count
	// behind it. One output token over a sixty-second turn is a positive count
	// and a rate that rounds to nothing, and `0 tok/s` is the least informative
	// cell on the frame at the moment a person is deciding whether to interrupt.
	// This line's one sanctioned exception to the law is `$0.00`, whose width
	// keeps the segments beside it from jumping sideways; a rate has no such
	// claim, so a zero one draws NOTHING, like every other zero on this surface.
	rate := burnStep(int(float64(written) / elapsed.Seconds()))
	if rate <= 0 {
		return a.holdBurn("")
	}
	return a.holdBurn(tokenWord(rate) + " tok/s avg")
}

// ── THE STEADY FIGURE ───────────────────────────────────────────────────────
//
// A NUMBER THAT MOVES FASTER THAN IT CAN BE READ IS NOT INFORMATION, IT IS
// MOTION. The burn rate is recomputed every frame, and unheld it lands on a
// different figure nearly every one of them: thirty times a second the status
// line becomes a line the terminal has to be told about again, to show a person
// digits their eye never resolved. The second and third digits of a token rate
// are not a fact anybody acts on — "about ninety" is the whole of what the
// figure says, and it says it whether it was drawn from 88 or from 91.
//
// So the DISPLAY is damped and the accounting is not. Every meter behind this
// line keeps its exact figure; what is held is the string, and it is held in
// two ways at once: the rate is rounded to a step worth reading, and the shown
// value is only replaced twice a second. Between replacements the segment is
// character for character the line it already was, which costs the wire
// nothing, and the person still watches a live rate — just one that stands
// still long enough to be read.

// burnHoldFor is how long a shown rate stands before it is allowed to move.
// Half a second is two updates a second: fast enough that a turn slowing down
// says so while it is still happening, slow enough that the figure is legible.
const burnHoldFor = 500 * time.Millisecond

// burnStep rounds a rate to a step a person reads as one number.
//
// The two tiers are the same rule stated for two magnitudes: keep about two
// digits of it. Under a hundred that is steps of five — the difference between
// 62 and 64 tok/s is a difference nobody is deciding anything on — and above it
// two significant figures, which is what [tokenWord] would draw anyway by the
// time the figure reaches thousands.
func burnStep(rate int) int {
	if rate <= 0 {
		return 0
	}
	if rate < 100 {
		return (rate + 2) / 5 * 5
	}
	step := 1
	for left := rate; left >= 100; left /= 10 {
		step *= 10
	}
	return (rate + step/2) / step * step
}

// holdBurn is the half-second hold: it returns the figure currently on the
// line, and adopts the new one only when the hold has run out. An empty
// candidate — the turn ended, or has not earned a rate yet — takes effect at
// once and clears the hold with it: a rate held past the turn it describes
// would be the surface reporting on work that has stopped.
func (a *app) holdBurn(text string) string {
	if text == "" {
		a.burnShown, a.burnAt = "", time.Time{}
		return ""
	}
	now := a.now()
	if a.burnShown == "" || now.Sub(a.burnAt) >= burnHoldFor {
		a.burnShown, a.burnAt = text, now
	}
	return a.burnShown
}

// etaSegment is the compaction forecast, and it only ever speaks when the
// answer is SOON:
//
//	compaction in ~3 turns
//
// The estimate is the average growth over the turn-end ring, against what is
// left before the threshold. It is deliberately silent in three cases: when the
// conversation is not growing (a session of reads and replies can sit flat for
// twenty turns, and "compaction in ~400 turns" is a number nobody will ever
// use), when the answer is more than [etaHorizon] turns away, and when
// compaction is already due — the meter is painted the bad hue by then, and a
// forecast of a thing that is happening is not a forecast.
func (a *app) etaSegment() string {
	turns, ok := a.compactionETA()
	if !ok {
		return ""
	}
	return "compaction in ~" + itoa(turns) + plural(" turn", turns)
}

// etaHorizon is how far ahead the forecast is worth making.
const etaHorizon = 5

func (a *app) compactionETA() (int, bool) {
	threshold := session.CompactThresholdFor(a.model, a.ctxWindow)
	if threshold <= 0 || len(a.ctxRing) < 2 || a.ctxTokens <= 0 {
		return 0, false
	}
	growth := (a.ctxRing[len(a.ctxRing)-1] - a.ctxRing[0]) / (len(a.ctxRing) - 1)
	if growth <= 0 {
		return 0, false
	}
	remaining := threshold - a.ctxTokens
	if remaining <= 0 {
		return 0, false
	}
	turns := (remaining + growth - 1) / growth
	if turns > etaHorizon {
		return 0, false
	}
	return turns, true
}

// ── NEGATIVE-SPACE SAFETY ───────────────────────────────────────────────────
//
// The gate's posture on this row is [app.approvalSegment] (approvalchip.go):
// `YOLO`, only when the gate is open, and only on the frames whose seam is not
// carrying the chip that says it at every posture.

// stateSegment is the last segment: what this surface is DOING, plain and
// painted.
//
//	idle                      dim       nothing is happening
//	⠹ working · 1m 4s         accent    the model has the turn, and for how long
//	waiting · your call       violet    IT HAS THE TURN AND IT IS YOURS
//	interrupted               soft red  the last turn was stopped by hand
//
// THE CLOCK IS THE ALIVENESS. A spinner says "something is happening" and says
// exactly as much at second one as at second ninety; the count-up is the only
// thing on the line that answers "should I still be waiting for this?". It is
// the tool rows' own count-up ([countUpWord]) and it turns on the tool rows'
// own grid ([spinnerStep]), so nothing on this screen beats against anything
// else.
func (a *app) stateSegment() (string, string) {
	word, painted := a.stateWord()
	// A task page reports its own state. The main conversation's spinner and
	// clock do not describe the work being read here.
	if a.room != nil && !a.orchOpen() {
		return word, painted
	}
	if a.state != stateWorking || a.asking() || a.copy.on {
		return word, painted
	}
	mark := tokens.Spinner(a.paints / spinnerStep)
	if a.linear {
		// A spinner read aloud is a word repeated forever (styles.go's linear
		// tier); the clock beside it is the fact it was standing in for.
		mark = glyphRunASCII
	}
	plain, line := mark+" "+word, a.pal.accent(mark)+" "+painted
	if clock := countUpWord(a.now().Sub(a.turnBegan)); clock != "" && !a.turnBegan.IsZero() {
		plain += " · " + clock
		line += a.pal.dim(" · ") + a.pal.accent(clock)
	}
	return plain, line
}

// plural spells a count's unit. The plural form is "s" unless a caller says
// otherwise, which "watch" does.
func plural(unit string, n int, form ...string) string {
	if n == 1 {
		return unit
	}
	if len(form) > 0 {
		return unit + form[0]
	}
	return unit + "s"
}

// contextSegment is what the conversation is CARRYING, and it reports whether
// that has got close enough to compaction to be painted.
//
//	12.4k/128k · 10%      the ordinary reading
//	842/128k              under one percent: the figure without a percentage
//	                      (empty)  nobody has said what the window is
//
// It leads with the tokens rather than the percentage because the two answer
// different questions and only one of them is answerable without the other. "How
// much am I carrying" is a fact about the conversation; "how much of the window
// is that" is a fact about the model, and it changes under a person's feet when
// they switch models without a single word being added. Both are on the line, in
// that order.
//
// The percentage is dropped entirely below 1% rather than shown as "0%" or "1%".
// A meter that reads 1% for the first twenty turns of a session is not a meter —
// it is what the byte-counting estimator this replaced actually did, and the
// figure it parked at was the only thing anybody ever read off it.
func (a *app) contextSegment() (string, bool) {
	if a.ctxTokens <= 0 || a.ctxWindow <= 0 {
		return "", false
	}
	segment := tokenWord(a.ctxDrawn()) + "/" + tokenWord(a.ctxWindow)
	if pct, ok := a.ctxPercent(); ok && pct >= 1 {
		segment += " · " + itoa(pct) + "%"
	}
	return segment, a.ctxCrowded()
}

// warmSegment is the session's cached share of everything it has sent, and what
// that share was WORTH — and it is empty until there is one.
//
//	⟲ saved $0.02 · 89% cached    a priced session: the cash, then the hit rate
//	⟲ 89% cached                  nobody published a price: the rate alone
//
// THE LAW IN ONE LINE: the percentage is the hit RATE, and the cash is what it
// MEANT. "⟲ 89%" alone was a number nobody could act on — a person reading it
// could not tell whether it was a good thing that had happened to them or a
// statistic about a mechanism they never asked about. The dollars are the
// answer, and they lead because money is the part a person recognizes on sight.
//
// The cash appears only when it is real (app.go's cacheSaved, which is derived
// from the session's cache reads only where a prompt price AND a cache-read
// price are both published): a session on a model that publishes neither, or
// publishes only the first, keeps exactly the segment it had, rather than
// learning to say "saved $0.00" — or, worse, to count the whole prompt price as
// a saving the cache never made.
//
// It is a share rather than a count because a count of cached tokens says
// nothing on its own: 40k cached is excellent against 50k sent and a rounding
// error against 4M. The glyph is the same one the per-turn savings note opens
// with, so the running total and the line that explains one turn of it are
// visibly the same subject.
//
// Rounding is toward the honest side: 0% is shown when the share is real but
// tiny, because "there is a cache and it is barely hitting" is a different fact
// from the empty segment's "there is no cache accounting here at all".
func (a *app) warmSegment() string {
	share, ok := session.Usage{Input: a.inputTokens, CacheRead: a.cacheRead}.CachedShare()
	if !ok {
		return ""
	}
	// The word rides with the rate because the rate alone was the owner's own
	// stumble: two percentages share this line, and the one that means "of the
	// window" and the one that means "served from the cache" are told apart by
	// a word, not by position. It is the same word the per-turn note spends on
	// the same subject ("⟲ 9.8k cached"), so the running share and the turn
	// that explains it stay one vocabulary.
	rate := itoa(int(share*100)) + "% cached"
	// THE CASH IS SAID ONLY WHEN THE WORD FOR IT IS NOT ZERO. A saving that is
	// real but under a hundredth of a cent — a few thousand cached tokens on a
	// model priced in millionths — is `$0.0000` once spelled, which reads as
	// "there is no saving" and is exactly the sentence THE EMPTINESS LAW keeps
	// off the screen. The share alone is still true of it.
	if word := savedWord(a.cacheSaved); a.cacheSaved > 0 && word != savedNothing {
		return "⟲ saved " + word + " · " + rate
	}
	return "⟲ " + rate
}

// savedNothing is what [savedWord] spells for a saving too small to have a
// figure, and the one spelling the cache segment refuses to draw.
const savedNothing = "$0.0000"

// warmSegmentShort is the cache segment's shorter true spelling — the hit rate
// without the cash — which is the rung a narrow row takes before giving the
// segment up (foot.go's [dropOrder]).
func (a *app) warmSegmentShort() string {
	share, ok := session.Usage{Input: a.inputTokens, CacheRead: a.cacheRead}.CachedShare()
	if !ok {
		return ""
	}
	return "⟲ " + itoa(int(share*100)) + "% cached"
}

// stateWord is the state itself, plain and painted — what [app.stateSegment]
// wraps with the spinner and the clock.
//
// A pending question OUTRANKS the run state, and says so in words as well as in
// colour: the turn is technically still working — the tool call is parked
// inside the batch — but what is true about it that a person can act on is that
// it is waiting for them. "your call" rather than "your answer" because it is
// shorter and because it is what it is.
func (a *app) stateWord() (string, string) {
	// COPY OUTRANKS EVERYTHING, because it is the only state on this line that is
	// about the KEYBOARD rather than about the turn. While the viewport is frozen
	// the keys do something else entirely (copymode.go), and a status line that
	// said "idle" would be describing the session correctly and the screen
	// wrongly. The turn underneath keeps running; the row it would have claimed
	// is back the moment esc is pressed.
	if a.copy.on {
		word := a.copyWord()
		return word, a.pal.accent(word)
	}
	// A sweep's receipt outranks the run state for the seconds it stands: the
	// person's eye is on the status line asking exactly one question — did the
	// copy land — and the turn's own word is back the moment it expires
	// (dragselect.go).
	if word := a.dragWord(); word != "" {
		return word, a.pal.accent(word)
	}
	if a.room != nil && !a.orchOpen() {
		if guest := a.roomGuest(); guest != nil && (guest.lost || a.roomGuestStale()) {
			return "reading", a.pal.dim("reading")
		}
		if node := a.roomNode(); node != nil {
			word := a.roomStateWord(node)
			return word, a.taskStateInk(node)(word)
		}
		return "reading", a.pal.dim("reading")
	}
	// AND THE STOP OUTRANKS THE QUESTION, on that same reading turned around. A
	// card still standing between the esc and the stream's close is asking about
	// a call the cancellation has already released (session's consent.go), so
	// "waiting · your call" would be this line naming the person as the thing
	// holding up a turn they themselves stopped. What is true and actionable at
	// that moment is neither — it is that the work is being let go.
	if a.windingDown() {
		// AND THE BOUND ON IT IS ON THE LINE BESIDE THE WORD (app.go's
		// [app.stoppingSegment]). The composition lives there because the
		// deadline, the clock it runs against and the door behind it all do.
		return a.stoppingSegment()
	}
	if run := a.orchOf(); run != nil && run.gate != nil {
		return waitingWord, a.pal.askBold(waitingWord)
	}
	// Required input outranks work. A proposal with a deadline starts on its
	// own; it offers an intervention, not a question that blocks progress.
	if a.asking() || a.awaitingStanding() || a.awaitingSubharness() ||
		(a.awaitingTask() && a.task.deadline.IsZero()) {
		return waitingWord, a.pal.askBold(waitingWord)
	}
	if a.awaitingTask() {
		return taskStartingWord, a.pal.accent(taskStartingWord)
	}
	// A DOOR AT REST WHOSE WORK OUTLIVED ITS TURN IS NOT IDLE. Handing a task out
	// ends the turn — `a.state` goes back to [stateIdle] — and the node it started
	// works on for minutes with nothing happening in the conversation, which the
	// tab strip already draws as `working` ([tabWorkingWord], via
	// [app.frontSignal]). The row said `idle` under a tab wearing `◐`: one
	// conversation described two ways on one screen. The word is the tab's own,
	// and the reading is the surface's frame-safe one — [app.tasksInFlight] walks
	// a map this surface keeps and [app.jobsRunning] walks the job list, so
	// nothing here opens [session.Agent.TaskIndex], which reads a file
	// (tabsignal.go's header states the law). It says `working` and the word is
	// NOT set on `a.state`: that field is a behavioural predicate (the spinner,
	// the clock, ticking, barge-in and the background-work question all read it)
	// and the door is genuinely at rest. There is no spinner and no clock here
	// either — both belong to a turn, and [app.stateSegment] draws them only in
	// [stateWorking].
	if a.state == stateIdle && a.frontSignal() == tabWorking {
		return tabWorkingWord, a.pal.accent(tabWorkingWord)
	}
	word := a.state.String()
	switch a.state {
	case stateWorking:
		return word, a.pal.accent(word)
	case stateInterrupted:
		return word, a.pal.bad(word)
	default:
		return word, a.pal.dim(word)
	}
}

// waitingWord is the state a person has to answer.
const waitingWord = "waiting · your call"

const taskStartingWord = "starting task"

// stoppingWord is what the status line says between a person's esc and the
// engine letting go of the turn ([app.windingDown]).
//
// IT IS THE PRESENT TENSE, AND THAT IS THE WHOLE OF WHAT IT ADDS. The line has
// always gone straight to "interrupted" on the key, which is the truth about the
// turn and reads, for the three or four seconds a real teardown can take, as a
// claim that everything is over — so a person watching a tool that has not quite
// let go presses the key again, harder, on a surface that already heard them.
// "stopping" says the stop landed AND that the letting go is still happening,
// and "interrupted" arrives behind it the moment it has.
//
// IT IS DIM, where "interrupted" is the soft red and "working" the accent. The
// hues on this line are its loudness, and winding down is the quietest thing the
// surface ever does: nothing is wrong, nothing is wanted, nothing is being
// waited on by anybody but the machine. It carries NO SPINNER for the reason it
// carries no colour — [app.stateSegment] draws the mark only in stateWorking, so
// the line stills on the key and stays stilled, which is the whole point.
//
// IT NAMES NO KEY, and there is no line about it in the hint slot, because THE
// SECOND STAGE IS A CLOCK AND NOT A KEY. There is nothing for a person to press:
// the esc they already pressed started a bounded window, and past that bound the
// surface lets go of the turn on its own — the waits are ended, the request is
// aborted, and the turn is marked abandoned in the journal with what it spent
// (app.go's block below [app.windingDown], and session's abandon.go).
//
// WHAT IT DOES NAME IS THE BOUND. [app.stoppingSegment] draws this word and,
// while there is a door behind the deadline, how long until the surface detaches
// — because a countdown a person cannot see is a stop they cannot trust.
const stoppingWord = "stopping"

// ── THE LEGEND: THE INPUT'S TOP BORDER, WITH THE CONVERSATION IN IT ─────────
//
// The seam above the input carries the model, effort and approvals at the left,
// with the numbers at the right. The conversation's name belongs to its tab and
// breadcrumb; the branch and workspace remain in /status. Keeping them off this
// line leaves room for the model and its serving machine (foot.go).
//
// The keys have their own row under the box, except on the phone deck where
// they occupy the seam's right end. A state with its own keys replaces the idle
// controls, so a live prompt never competes with a second set of instructions.

// microcopy is the input's own affordance and the idle hint's narrowest form.
//
// IT NAMES ONE KEY AND IT USED TO NAME TWO. "@ files" was true — the completion
// still opens on "@" and always will (files.go, taskmention.go) — and it was
// still the wrong half to print, because the two keys are not the same KIND of
// thing. "/" opens a list of everything this surface can be told to do, which is
// the door a person who does not know what to press is looking for; "@" is a
// shortcut inside a sentence somebody is already writing, and a person writing a
// sentence about a file discovers it by typing the character that is already in
// their head. Printing both made the slot a two-item menu, and a two-item menu
// beside a live prompt is read once and then never again.
const microcopy = "/ commands"

// legend draws that border. It replaces the plain rule at every width, and
// degrades back into it when there is no room for anything else.
func (a *app) legend(width int) string {
	if width < 1 {
		return ""
	}
	// THE ONE MOMENT THE LEGEND MAY SHOUT: while a person is being asked
	// something, the place takes the waiting hue along with the state word — the
	// amber [palette.ask] has painted with since the question violet was retired
	// (styles.go). The question is bottom-anchored and so is this border — the two
	// of them framing the question is the surface pointing at it with both hands.
	paint := a.pal.dim
	if a.asking() || a.awaitingTask() || a.awaitingStanding() {
		paint = a.pal.ask
	}
	// THE LADDER, in the order of what a person can recover elsewhere. Each rung
	// is told its own room, because the left label is now BUILT to fit rather
	// than measured and rejected: the name is cut to whatever the frame leaves
	// it, and only when there is not even [legendNameFloor] worth of cells for it
	// is the hint slot spent instead — the keys it names keep working unprinted,
	// and which conversation this is is not written anywhere else on a frame
	// this narrow. A rung whose name did not survive is skipped rather than
	// drawn, which is what puts the hints on the block before the name.
	// THE DOOR'S SPAN IS THIS FUNCTION'S TO CLEAR, because this is the ladder that
	// decides whether the door is drawn at all ([app.legendLine] says why it is
	// not cleared down there).
	a.homeDoor = hudSpan{}
	a.seamProjectSpan = hudSpan{}
	a.seamModelSpan, a.seamEffortSpan, a.seamApprovalSpan = hudSpan{}, hudSpan{}, hudSpan{}
	a.doors = a.doors[:0]
	a.moneySpan, a.moneyRow = hudSpan{}, 0
	// THE RIGHT-HAND LADDER IS THE NUMBERS' (footswap.go): the bill, the meter
	// and the state word, said shorter one rung of [dropOrder] at a time. At
	// phone width it is still the keys' — the running slot's clauses, dropped
	// from the right ([app.hintShorter]) — because the deck under the box is
	// where a phone reads its numbers.
	var rungs []seamRung
	var parts []hudPart
	telemetry := a.seamCarriesTelemetry()
	if telemetry {
		rungs, parts = a.seamTelemetryRungs(width)
	} else {
		for rung := a.footHint(width); rung != ""; rung = a.hintShorter(rung) {
			rungs = append(rungs, seamRung{plain: rung, width: ansi.StringWidth(rung)})
		}
	}
	// THE PIECES ARE GATHERED ONCE and every rung of every tier is laid from
	// them: gathering them is a handful of strings, and this ladder is climbed
	// on every frame of a scroll, which the allocation law counts
	// (inputsmooth_test.go's
	// [TestOneScreenScrollOfFourThousandLinesStaysInsideTheAllocationLaw]).
	pieces := a.legendPieces(width)
	// THREE CELLS ON THIS LINE ARE DOORS. Their columns come from the chosen
	// layout, offset by the border's two cells. The model always wears bold
	// data ink, while the rung and gate keep their hover, flash and state cues.
	// THE LIFTS ARE PAINTED SIDE BY SIDE AND NEVER NESTED, because raw SGR
	// resets would end an outer hue at the inner reset ([paintSpans]).
	//
	// IN A ROOM THE THREE SPANS ARE THE NODE'S DOORS (roomseam.go), recorded
	// on the same bargain and pressed through the same three functions.
	seam, dial, gate := hudSpan{}, hudSpan{}, hudSpan{}
	lift := func(text string) string {
		return paintSpans(text, paint,
			spanLift{span: seam, lift: func(text string) string {
				return seamModelPaint(a.pal, text, a.hoveringStatusModel())
			}, on: true},
			spanLift{span: dial, lift: a.paintEffortChip, on: a.effortSeamLit()},
			spanLift{span: gate, lift: a.paintApprovalChip, on: a.approvalSeamLit()})
	}
	// seat is one attempt: FIT IS DECIDED ON THE PLAIN WIDTHS BEFORE ANYTHING
	// IS PAINTED, so the attempts that fail cost nothing but arithmetic —
	// painting a rung is two builders and the doors' lifts, and only the one
	// rung drawn pays for it (footswap.go's [seamRung]). The numbers' doors are
	// recorded where the right label landed, on the seam's own row
	// ([legendDoorRow]), by the same walk the status row used ([app.markDoors]).
	seat := func(left string, span, dialSpan, gateSpan hudSpan, rung seamRung) (string, bool) {
		if !legendFits(left, rung.width, width) {
			return "", false
		}
		seam, dial, gate = span, dialSpan, gateSpan
		a.seamModelSpan, a.seamEffortSpan, a.seamApprovalSpan = shiftIntoBorder(span), shiftIntoBorder(dialSpan), shiftIntoBorder(gateSpan)
		right, painted := rung.plain, ""
		var ledger []hudPart
		if telemetry && rung.width > 0 {
			var alive []hudPart
			ledger, alive = a.seamRungParts(parts, rung.steps)
			painted, right = a.seamTelemetryLabel(ledger, alive)
		}
		// THE PROJECT IS THE KEYS ROW'S NOW (footswap.go's [app.hintRow]), and
		// it stays on the seam only at the phone tier, where the seam IS the
		// keys row and the last row is the deck. Its extra columns never move
		// the ledger's doors relative to that label.
		projectSpan := hudSpan{}
		if !a.roomOpen() && !telemetry {
			original := right
			right, projectSpan = seamProjectRight(left, right, pieces.project, width)
			if projectSpan.pressable() {
				if painted == "" && original != "" {
					painted = paintHint(original, a.pal, a.pal.dim)
				}
				start := ansi.StringWidth(original)
				tail := ansi.Cut(right, start, ansi.StringWidth(right))
				span := hudSpan{from: projectSpan.from - start, to: projectSpan.to - start}
				painted += a.paintSeamProject(tail, span, a.hot.kind == hoverSeamProject)
			}
		}
		line, at, ok := a.legendLinePainted(left, right, painted, width, lift)
		if !ok {
			a.seamModelSpan, a.seamEffortSpan, a.seamApprovalSpan = hudSpan{}, hudSpan{}, hudSpan{}
			return "", false
		}
		if projectSpan.pressable() {
			a.seamProjectSpan = hudSpan{from: at + projectSpan.from, to: at + projectSpan.to}
		}
		if len(ledger) > 0 {
			a.markDoors(ledger, at, legendDoorRow)
		}
		return line, true
	}
	// THREE TIERS, AND THE NUMBERS GIVE WAY BEFORE THE NAME DOES. Each tier
	// tries every right-hand rung, fullest first, against a left held to that
	// tier of its own ladder ([seamTier]): first with the rider whole, then
	// with the name and the model whole, and only when not even the state
	// word fits beside those does the last tier let the left cut and drop
	// against each rung in turn — and there an empty left is refused too,
	// because a rule with numbers and no name would be a rule about nobody.
	// The first attempt that fits is drawn. At phone width, where the right is
	// the keys, the keys are the cheaper thing and the left's whole ladder
	// runs at once.
	tiers := [...]seamTier{seamTierRider, seamTierWhole, seamTierAny}
	from := 0
	if !telemetry {
		from = 2
	}
	for _, tier := range tiers[from:] {
		// A RIDER TIER WITH NO RIDER TO SEAT is every rung tried for nothing.
		if tier == seamTierRider && pieces.rider == "" {
			continue
		}
		for _, rung := range rungs {
			// THE RIDER IS WORTH THE CHEAP NUMBERS AND NOT THE DEAR ONES
			// ([seamRung.cheap]): past the last cheap rung the rider tier
			// stands down and the whole tier, rider and all, takes over.
			if tier == seamTierRider && !rung.cheap {
				continue
			}
			left, span, dialSpan, gateSpan, named := a.legendLeftSpanFrom(&pieces, legendRoomFor(width, rung.width), tier)
			if !named || (left == "" && telemetry && !a.roomOrganized()) {
				continue
			}
			if line, ok := seat(left, span, dialSpan, gateSpan, rung); ok {
				return line
			}
		}
	}
	// THE BARE RULE LAST: the left alone, at whatever its ladder can make of
	// the whole width.
	if left, span, dialSpan, gateSpan, _ := a.legendLeftSpanFrom(&pieces, legendRoomFor(width, 0), seamTierAny); left != "" {
		if line, ok := seat(left, span, dialSpan, gateSpan, seamRung{}); ok {
			return line
		}
	}
	return a.rule(width)
}

// legendFits is [app.legendLinePainted]'s own arithmetic asked ahead of the
// paint: whether the two labels leave a cell of rule between them.
func legendFits(left string, rightWidth, width int) bool {
	head := 1
	if left != "" {
		head = 3 + ansi.StringWidth(left)
	}
	tail := 0
	if rightWidth > 0 {
		tail = 3 + rightWidth
	}
	return (left != "" || rightWidth > 0) && width-head-tail >= 1
}

// legendPieces is the cluster's pieces for this frame: the node's inside a
// room, the conversation's otherwise, and none on the organized room's page,
// whose header carries the identity ([app.legendLeftSpanFrom]).
func (a *app) legendPieces(width int) seamPieces {
	if a.roomOpen() {
		if a.roomOrganized() {
			return seamPieces{}
		}
		return a.roomSeamPieces()
	}
	return a.seamPieces(width)
}

// shiftIntoBorder moves a span from the label's own columns to the frame's: the
// legend sets its labels into the rule two cells in, and a door is pressed at
// the column it was DRAWN at.
func shiftIntoBorder(span hudSpan) hudSpan {
	if !span.pressable() {
		return span
	}
	span.from += 2
	span.to += 2
	return span
}

// legendGap is the shortest run of rule the two labels will leave between them.
//
// It is three rather than one because a name can now fill this line on its own:
// a label cut to the last cell leaves `… · chat-v3-task* ─ / commands ─`, where
// the single dash reads as two labels that collided rather than as a border
// with two labels set into it. Three cells is the least that still reads as a
// rule, and it is bought from the name, which is the thing that had too much to
// say in the first place.
const legendGap = 3

// legendRoom is how many cells one attempt leaves for its left label: the
// width, less the border's own two cells at the head, the space that separates
// the label from the fill, the gap, and the right label's tail.
//
// It is the arithmetic of [app.legendLine] read forwards instead of backwards,
// and it lives here because the left label now has to be BUILT to a budget
// rather than merely measured against one. Two functions computing the same
// number would drift; this is the one that computes it.
func legendRoom(width int, right string) int {
	return legendRoomFor(width, ansi.StringWidth(right))
}

// legendRoomFor is [legendRoom] given the right label's width alone, which is
// all the seam's ladder keeps of a rung it has not drawn (footswap.go).
func legendRoomFor(width, rightWidth int) int {
	tail := 0
	if rightWidth > 0 {
		tail = rightWidth + 3
	}
	return width - 3 - legendGap - tail
}

// legendLine lays one attempt out, and reports whether it fitted. The label
// sits one cell inside the border on each side, which is what makes it read as
// a legend rather than as text that collided with a rule.
//
// AN EMPTY LEFT IS A LINE, NOT A FAILURE. A session that has not named itself
// in a directory that is not a repository has nothing true to put at that end
// (the emptiness law), and the hint slot at the other end is the newcomer's
// only pointer at "/" — so the border draws from the frame's edge and the keys
// keep their place. Only an attempt with nothing at EITHER end is refused, and
// what answers that is the plain rule.
func (a *app) legendLine(left, right string, width int, paint func(string) string) (string, bool) {
	line, _, ok := a.legendLinePainted(left, right, "", width, paint)
	return line, ok
}

// legendLinePainted is [app.legendLine] with the right label optionally
// supplied already painted (footswap.go's telemetry), and it also reports the
// column the right label starts at, for the doors recorded along it.
func (a *app) legendLinePainted(left, right, rightPainted string, width int, paint func(string) string) (string, int, bool) {
	head := "─"
	if left != "" {
		head = "─ " + left + " "
	}
	tail := ""
	if right != "" {
		tail = " " + right + " ─"
	}
	fill := width - ansi.StringWidth(head) - ansi.StringWidth(tail)
	if (left == "" && right == "") || fill < 1 {
		return "", 0, false
	}
	at := ansi.StringWidth(head) + fill + 1
	// WHERE THE DOOR LANDED, for the press that may follow. It is written HERE,
	// as the line is laid out, for the reason [app.statusPress] gives about the
	// model segment: a column read from anywhere else is a column from the
	// frame before this one.
	//
	// IT IS CLEARED BY THE LEGEND AND NOT BY THIS FUNCTION, which is the fix for
	// a bug the breadcrumb bar exposed: this line is laid out by the pinned
	// header as well as by the legend (room.go, roomcrumbs.go), and the header is
	// drawn AFTER the chrome — so a header clearing the span erased a door the
	// legend had just recorded, and `space space home` became a label nothing
	// answered for. What makes the span its own answer to "was it drawn" is
	// [app.legend] clearing it before its own ladder starts.
	if offset := strings.Index(right, homeDoorWord); offset >= 0 {
		from := at + ansi.StringWidth(right[:offset])
		a.homeDoor = hudSpan{from: from, to: from + ansi.StringWidth(homeDoorWord)}
	}
	line := a.pal.dim("─")
	if left != "" {
		line = a.pal.dim("─ ") + paint(left) + a.pal.dim(" ")
	}
	line += a.pal.dim(strings.Repeat("─", fill))
	if tail != "" {
		// THE KEY IS THE PAYLOAD AND THE VERB IS THE PROSE (payload.go). This slot
		// is written in one grammar at every call site that fills it — `esc
		// interrupt`, `y allow · n deny · a always` — and until this wave both
		// halves were drawn at the dim value the border itself wears, so the chord
		// a person had to press was exactly as loud as the word explaining it. The
		// chord steps to ink; nothing else on the line moves, and the line is the
		// same number of cells it was.
		if rightPainted == "" {
			rightPainted = paintHint(right, a.pal, a.pal.dim)
		}
		line += a.pal.dim(" ") + rightPainted + a.pal.dim(" ─")
	}
	return line, at, true
}

// legendNameFloor is the fewest cells worth spending on a room's cut way out.
// Below it, an ellipsis wearing a few letters says too little to be useful.
const legendNameFloor = 12

// branchWord is the branch as every surface writes it: its name, and a "*" when
// the tree has uncommitted work. An unknown branch is the empty string, which
// the emptiness law then draws as nothing wherever this is spent.
//
// The status sheet and /status share this spelling so the dirty mark agrees
// wherever a person asks which repository they are working in.
func (a *app) branchWord() string {
	if a.branch == "" {
		return ""
	}
	if a.branchDirty {
		return a.branch + "*"
	}
	return a.branch
}

// legendLeft is what is answering this conversation and where it is running:
// the model with its machine, the rung, the gate and, on a remote
// session, the host. The tab strip and the breadcrumb bar own the
// conversation's name, so the legend does not repeat it (foot.go).
//
// THE MACHINE KEEPS ITS PLACE NOW THAT THE PATH HAS LOST ITS OWN. host.go's law
// is that a connection is shown as the place and nowhere else, and this end of
// the legend is that place: `devbox · glm-5.3-flash`. It is written as a
// SEGMENT rather than with the path's colon, because `devbox:` in front of a
// sentence of English is scp syntax pointed at something nobody can copy. It is
// never cut, for the reason the path never cut it either — which machine is the
// half of the answer a person cannot reconstruct from anything else on screen.
func (a *app) legendLeft(width, room int) (string, bool) {
	left, _, _, _, named := a.legendLeftSpan(width, room, seamTierAny)
	return left, named
}

// legendLeftSpan is that label AND the columns its three doors occupy within it
// — the model's name, the thinking rung, then the approvals chip — which is
// what [app.legend] needs to make all three pressable without building the
// cluster a second time. The tier says how much of the left's ladder may be
// walked ([seamTier]); on the two upper tiers an empty cluster is a refusal.
func (a *app) legendLeftSpan(width, room int, tier seamTier) (string, hudSpan, hudSpan, hudSpan, bool) {
	pieces := a.legendPieces(width)
	return a.legendLeftSpanFrom(&pieces, room, tier)
}

// legendLeftSpanFrom is [app.legendLeftSpan] laid from pieces already
// gathered — [app.legend] gathers them once per frame and lays every rung of
// every tier from the same set.
func (a *app) legendLeftSpanFrom(pieces *seamPieces, room int, tier seamTier) (string, hudSpan, hudSpan, hudSpan, bool) {
	// THE PLACE IS THE ROOM while one is open, and the name and branch go with
	// the path: none of them is a fact about the page on screen, and the one
	// thing a person in here needs from this slot is the key that gets them out
	// (room.go). The task's own title is on the breadcrumb bar at the top of
	// the page (roomcrumbs.go).
	if a.roomOpen() {
		if a.roomOrganized() {
			// THE ORGANIZED ROOM'S HEADER CARRIES THE IDENTITY, so the seam's
			// left says only whose numbers ride its right — the conversation's,
			// never the node's (room.go's law about the telemetry). At phone
			// width the right is the keys and the left stays empty.
			if a.seamCarriesTelemetry() {
				return roomTotalsWord, hudSpan{}, hudSpan{}, hudSpan{}, true
			}
			return "", hudSpan{}, hudSpan{}, hudSpan{}, true
		}
		if room < 1 {
			return "", hudSpan{}, hudSpan{}, hudSpan{}, tier == seamTierAny
		}
		// THE WAY OUT LEADS, AND THE NODE'S OWN CELLS FOLLOW IT — its model,
		// its rung and its gate, on the seam's own ladder (roomseam.go). While
		// a history walk is on the lead says what esc actually does, which for
		// those few keystrokes is not "main" (recall.go); the slot promises the
		// NEXT keystroke, so it moves with it. Only the doors the node still
		// has are recorded ([app.roomSeamDoors]).
		cluster, model, rung, gate := seamLay(pieces, room, tier)
		if tier != seamTierAny && cluster == "" {
			return "", hudSpan{}, hudSpan{}, hudSpan{}, false
		}
		model, rung, gate = a.roomSeamDoors(model, rung)
		return cluster, model, rung, gate, true
	}
	if room < 1 {
		return "", hudSpan{}, hudSpan{}, hudSpan{}, tier == seamTierAny
	}
	// The model, thinking rung and approvals share the ladder in foot.go.
	cluster, span, dial, gate := seamLay(pieces, room, tier)
	if tier != seamTierAny && cluster == "" {
		return "", hudSpan{}, hudSpan{}, hudSpan{}, false
	}
	return cluster, span, dial, gate, true
}

// legendJoin is the separator between the legend's facts, and dotted threads any
// number of them onto it while skipping the ones that are not there — which is
// the emptiness law spelled as a function, since a missing branch must leave no
// dangling "·" behind it.
const legendJoin = " · "

func dotted(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, legendJoin)
}

// placePath is the workspace, abbreviated at one of three strengths — and, on
// a session that is running on another machine, the machine's name in front of
// it: `devbox:~/code/app`.
//
// IT IS NO LONGER ON THE LEGEND. The path is on the status sheet's
// "place" row and in /status (statusdeck.go, statusnote.go), which is where a
// path a person copies belongs. The three strengths are kept because the sheet
// is a forty-four-column page.
//
// THE HOST IS NOT CUT WITH THE PATH. On a remote session WHICH MACHINE is the
// half of the answer a person cannot reconstruct from anything else — the path
// they might recognize, the host they would have to remember. So the
// abbreviation eats the path and leaves the name. The home abbreviation is still
// this machine's home, which is why a remote path rarely collapses to `~`: it is
// the far machine's home and nobody here knows it.
func (a *app) placePath(hard int) string {
	return a.hostedPath(a.placeWord(shortPath(a.workspace, a.tilde, hard)))
}

// footHint is the keys slot: the state's own keys when it has any, and the
// input's own affordances when it does not. It is the whole of the last row
// now (footswap.go's [app.hintRow]), and the seam's right at phone width.
//
// The narrowest usable frame keeps / commands: it is how someone who does not
// know the keys discovers what the surface can do (steer.go's [app.hintShorter]).
func (a *app) footHint(width int) string {
	// A QUESTION'S PAGE BRINGS ITS OWN FOOT (questionroom.go's
	// [app.questionFootRows]) and names the keys it takes there; a second line
	// under it naming the conversation's doors would be naming keys the page
	// does not take.
	if a.questionRoomOpen() {
		return ""
	}
	if hint := a.hintWord(); hint != "" {
		return hint
	}
	// A WORD OF THE HEAD UNDER THE POINTER says what it opens and its key
	// (topnav.go's [app.headHint]), under a state's own keys and over every
	// resting sentence.
	if hint := a.headHint(); hint != "" {
		return hint
	}
	// AND UNDER THE STATES, BUT OVER EVERY TIP AND DOOR: THE CHORD THAT DID NOT
	// ARRIVE. A Mac whose Option key is composing accents answers the switcher's
	// chord with the character `˚`, and the legend's own door would go on naming
	// a key that is not reaching this program — which is the phantom this
	// surface's whole key law exists to forbid. It outranks the earned tips
	// because it is a diagnosis and they are suggestions, and it says nothing at
	// all on every other terminal (chords.go's [app.chordWatch] arms it only on a
	// Mac, and only after a chord was actually aimed and missed).
	if a.chordLost && a.chords.meta == chordMetaWord {
		return a.chords.chordShortWords()
	}
	return a.idleHint()
}

// idleHint keeps the shared controls in home's order, followed by the way home.
// Each control is named only where the session can take its key.
func (a *app) idleHint() string {
	doors := make([]string, 0, 5)
	if !a.roomOpen() {
		if _, ok := a.effortDial(); ok {
			doors = append(doors, a.chords.say(targetEffortKeyWord))
		}
		if _, ok := a.approvalDial(); ok {
			doors = append(doors, a.chords.say(targetApprovalKeyWord))
		}
	}
	if a.hopAvailable() {
		doors = append(doors, a.chords.say(hopDoorWord))
	}
	doors = append(doors, microcopy)
	if a.homeDoorShowing() {
		doors = append(doors, homeDoorWord)
	}
	return strings.Join(doors, hintSegment)
}

// hopDoorWord advertises the switcher, and it names the binding rather than the
// `ctrl+tab` alias because this line is drawn on every terminal and the alias is
// only real on some of them (hop.go states the whole argument). A hint may only
// name a key that works.
//
// THE NOUN IS `chats` AND IT USED TO BE THE VERB `switch`. One word for one door
// is the rule the audit-words pass was written to keep (docs/design/polish/
// audit-words.md found `esc` spelled three ways across two panels), and this
// door had picked up two: the tab row's own control said `Chats`, home's rule
// says `alt+k chats`, and this line said `switch`. The noun is the one that
// survives — it is what the thing IS rather than what pressing it does, it is
// the word on the page the card draws, and it is the word a person who has used
// any other program already has for a list of conversations. The control that
// was the third spelling is deleted (chattabs.go).
const hopDoorWord = hopOpenKey + " chats"

// ── CONTEXTUAL KEY HINTS ────────────────────────────────────────────────────
//
// THE HINT SLOT IS STATE-DRIVEN, AND IDLE IT IS EMPTY. This surface used to
// carry "/help · ctrl+o" permanently, which is the definition of a static
// cheatsheet: two keys that were true in every state, drawn in the one place a
// person looks when they do not know what to do, and therefore never read after
// the first session.
//
// What replaces it is a slot that only ever names the keys that WORK RIGHT NOW:
//
//	the pointer is theirs drag to select · any key ends it
//	the picker is open    → lanes · enter switch · esc · crew max
//	  inside a fold       enter choose · ← back · esc · crew max
//	the sessions are up   enter open · esc
//	copy mode is on       v select · a block · y yank · esc
//	rewind is armed       esc again to rewind        (rewind.go's double esc)
//	rewind mode is up     nothing — the mode bar prints its own keys
//	the welcome box is up ↑↓ recent · enter open
//	a path is completing  tab take · enter run · esc
//	a list is open        ↑↓ · enter · esc
//	a proposal is up      enter answer · esc no (task.go's own keys)
//	a question is up      a allow · t always · d deny  (consent.go's own keys)
//	a room is open        x stop, or ↑↓ history mid-walk  (room.go's [app.roomHint])
//	a turn is running     send · stop-and-send · background · stop (when true)
//	the column is away    ctrl+g tasks               (task.go's [railBackHint])
//	idle                  nothing
//
// The keys are quoted from the handlers rather than authored here — a hint that
// disagrees with input.go is worse than no hint, because it is a hint somebody
// will act on.
//
// AND THE ORDER IS input.go's OWN ROUTING ORDER, top to bottom, because that is
// the only thing that makes the slot true: what a key does is decided by which
// handler reads it first, so a hint ranked any other way is a hint that names
// the keys of a state the keyboard has already been taken away from. The
// pointer handover leads, because it is read first; copy mode is the rung under
// the modals; the typed lists come last of those, since they take only the four
// keys that move and commit a list and give every other one back to the draft.
// ctrl+c is on none of these rungs and never was a state this slot could name —
// it is the door, read above every modal, and it acts on the press that lands
// (leaving.go).
//
// COPY MODE IS THE ONE THIS SLOT WAS MOST WRONG ABOUT. While the viewport is
// frozen every key on this surface means something else, and the slot was
// drawing "@ files · / commands" — two affordances of a box the keyboard is not
// currently pointed at. A hint naming keys that do nothing is the failure mode
// this slot exists to prevent, and it was the default state of it.
func (a *app) hintWord() string {
	switch {
	case a.released:
		// IT LEADS BECAUSE THE HANDOVER IS READ FIRST ([app.key] takes the pointer
		// back above every overlay), and because it is the one state on this slot
		// that a person cannot see any other way: the pointer being somewhere else
		// looks exactly like the pointer being broken until a line says otherwise.
		return "drag to select · any key ends it"
	case a.pick.open:
		// AND THE CREW IS NAMED BESIDE THE KEYS, because this list is where a
		// person lands when the crew they just set did not change anything they
		// can see. The status line's model readout is the conversation's model,
		// which /crew never touches by design — so somebody who typed `/crew max`
		// opens /model hunting for the change, and the one word this slot can
		// afford tells them the crew is a separate thing that is already set.
		// The picker's rows are the list itself and are reused whole inside the
		// settings panel ([picker.rowsOwned]), so it has no header or foot of its
		// own to spend on a sentence; this slot is the line that is already there.
		// It says nothing on a door with no profile ([app.crewHint]).
		//
		// AND THE KEYS ARE THE ROW'S, not the list's: `→ lanes` on a model,
		// `← back` inside its fold ([picker.keysHint] says why the slot has to
		// follow the cursor to be true at all).
		keys := a.pick.keysHint()
		if crew := a.crewHint(); crew != "" {
			return keys + " · " + crew
		}
		return keys
	case a.crewPick.open:
		return "↑↓ · ←→ family · enter apply · esc"
	case a.effPick.open:
		// The chord is named beside the keys because this list is the only place
		// on the surface that can teach it: the chip it opens from prints a mark
		// and a word and has no room for a key, so the slot under the ladder is
		// where somebody who arrived by clicking learns how to arrive by typing
		// (effortchip.go).
		return a.chords.say("↑↓ · enter apply · esc · " + effortKey + " next rung")
	case a.roster.open:
		return "enter open · esc"
	case a.shelf.open:
		// The deliverables picker names its verbs here as well as in its filter
		// box, because they are the half of this list nobody can guess: two of
		// the three are chords (deliverables.go).
		if a.shelf.dest != nil {
			return filesCopyVerbs
		}
		return filesVerbs
	case a.at(pageStanding):
		// The standing page names its verbs here because they are the half of it
		// nobody can guess, and it names the ones the ROW UNDER THE CURSOR
		// actually has: one of them stops a thing for good, and an order in
		// another project cannot be excepted from a place it never reached
		// ([standingPlace.hint]).
		return a.orders.hint(a)
	case a.subPage.open:
		// /subharness names its verbs here PER ROW, because enter means two
		// things on the intake card — fill this field in, or start the run — and
		// one line saying "enter" for both would be teaching nobody
		// (subharness.go).
		return a.subVerbs()
	case a.copy.on:
		return copyKeysWord
	case a.rew.on:
		// The rewind mode prints its own keys in the bar that replaced the draft
		// box (rewind.go), and a slot repeating them would be the surface saying
		// the same thing twice on one screen.
		return ""
	case a.rewindArmed() && a.rewindReady():
		// The first esc has landed and the second one means something else for
		// half a second. This outranks "esc interrupt" below for exactly that
		// reason: while the window is open, that is no longer what the key does.
		//
		// It asks [app.rewindReady] as well as the clock, because the two can come
		// apart: a question can be raised in the half second the window is open,
		// and from that moment esc belongs to the question. The slot promises what
		// the NEXT esc does, so it has to ask the same thing that key will.
		return rewindArmWord
	case a.rewindSaying():
		return a.rewSay
	case a.welcome.open:
		// The box reads two keys and hands back the rest (welcome.go), and the
		// arrows only mean the list while the draft is empty — which is exactly
		// when this hint is worth drawing.
		if !a.input.empty() || len(a.welcome.recent) == 0 {
			return ""
		}
		return "↑↓ recent · enter open"
	case a.comp.open && a.comp.arg:
		// A path completing under a command argument: tab takes the row, and
		// enter belongs to the LINE rather than to the list (input.go).
		return "tab take · enter run · esc"
	case a.menu.open || a.comp.open:
		if hint := a.mentionHeadHint(); hint != "" {
			return hint
		}
		return "↑↓ · enter · esc"
	case a.shaping():
		// The widening answer is part-way given and the block is on its second
		// beat (question.go): the numbers bank a shape and esc puts the question
		// back exactly as it was.
		return "1-3 shape · esc never mind"
	case (a.asking() || a.awaitingDecision()) && a.questionHint() != "":
		// IT IS A CASE WITH A CONDITION because a question that draws its own
		// keys says nothing here (hints pick A, 2026-09-11), and a slot that
		// returned an empty string from this rung would have spent the row: the
		// note under it — "this question has been waiting 4 hours" — is the
		// sentence that rung is for.
		//
		// THE KEYS THE BLOCK ACTUALLY DRAWS, read off the question itself. This
		// line said "a allow · t always" for a year after the answers took their
		// own first letters, so the hint under the box named `a` as allow while
		// the block above it named `a` as always — one keystroke, two readings,
		// and the wrong one widens a permission. It is derived now, so it cannot
		// come apart from the row again.
		return a.questionHint()
	case a.railHold:
		// The roster has the keyboard (alt+t, task.go) — the one state on this
		// surface where the arrows have left the box entirely. It ranks HERE, under
		// every overlay and both questions, because that is exactly where
		// [app.railKey]'s guard stands down; and above the running turn, because
		// while it is held esc gives the keyboard back rather than interrupting.
		return a.railHoldHintWord()
	case a.roomOpen():
		// A ROOM IS READ HERE because that is where [app.roomKey] stands in the
		// program loop (app.go): under the stop card and the roster, over
		// everything the draft would have got. And it ranks ABOVE the two lines
		// below for the reason this whole slot is ordered the way it is — while a
		// room is open, esc leaves the page and does not touch the conversation's
		// turn, so "esc interrupt" would be naming a key that is spoken for.
		return a.roomHint()
	case a.state == stateWorking:
		// ONE RUNNING STATE, ONE COMPOSED LINE (steer.go's [app.runHint]). Its
		// clauses ask their keys' own guards and remain in one fixed order, so the
		// slot never teaches a gesture another router has taken on this frame.
		return a.runHint()
	case a.spell.asking:
		// THE EXPANSION IS OUT. The slot the chord was named in is where the
		// spinner for it belongs — the person pressed a key at the end of this
		// line and this is the line answering (spellout.go). It ranks above the two
		// offers below because it is not an offer: it is something happening.
		return a.spellWorkingWord()
	case a.slashTagHint() != "":
		// A LIVE TAG OWNS ENTER, so its line outranks the two optional chords
		// below. A HINT MAY ONLY NAME A KEY THAT WORKS, and exactly one tag is the
		// only state where enter has the promised alternate meaning.
		return a.slashTagHint()
	case a.standMarkOffered():
		// THE DRAFT LOOKS LIKE A CONDITION, so the slot says the chord that makes
		// it one (standmark.go). It ranks HERE — under the running turn, over the
		// column's own line — for this slot's ordering law: the chord is read at
		// the bottom of [app.key]'s plain switch, so every state above has already
		// taken the keyboard, and esc while an answer is streaming is a key the
		// person is far more likely to want next.
		//
		// It costs no rows. The legend is on the frame in every state, and this is
		// the slot it already carries — so a draft that starts looking like a rule
		// changes one word at the end of a line and moves nothing.
		return standMarkHint
	case a.spellOffered():
		// AND THE DRAFT LOOKS LIKE SOMETHING TO BUILD, with room left to say what
		// it means, so the slot offers to spell it out (spellout.go). It ranks
		// directly UNDER the standing hint because these two are the only lines
		// here that are about the sentence being typed rather than about a state
		// the surface is in — but the sharing itself is decided in
		// [app.spellOffered], which answers no while the standing hint is up, so
		// this ordering is a statement of the same rule and never a second one.
		//
		// It costs no rows, for the reason the line above it costs none: this is
		// the legend, which is on the frame in every state.
		return spellOutHint
	case a.railAway && a.railAvail() && a.headHint() == "":
		// THE COLUMN IS AWAY AND THIS SESSION HAS RUN SOMETHING (task.go's
		// [app.railStow]). It ranks LAST, under every state above it, because it is
		// the only line here that is not about the next keystroke — it is where the
		// work went, said in the one slot a person looks at when they cannot see
		// something they know exists.
		//
		// AND IT IS THE HALF THE STRIP CANNOT SAY. The chips above the conversation
		// draw what is RUNNING and nothing else, so a session whose work has all
		// landed has a roster full of results and no sign on the frame that it is
		// there. This is that sign. With nothing run at all it stays quiet — the
		// column a person closed was empty, ctrl+g still brings it back, and a
		// standing hint about a roster of nothing is the emptiness law broken in
		// the one slot a person reads most. A word of the head under the pointer
		// outranks it too ([app.footHint] asks [app.headHint] next): the pointer
		// is on a word, and the line says that word.
		return a.sideBackHint()
	}
	return ""
}

// awaitingDecision reports whether a call is parked on a person. It is the same
// hint as an open question because it is the same moment: the request may not
// have reached this surface yet, and the row is already showing the "?".
func (a *app) awaitingDecision() bool {
	for i := range a.entries {
		if a.entries[i].kind == entryTool && a.entries[i].status == toolConsent {
			return true
		}
	}
	return false
}

// ── THE PATH, FISH-STYLE ────────────────────────────────────────────────────
//
// shortPath abbreviates a directory the way fish's prompt does, at one of three
// strengths:
//
//	hard 0   ~/s/codeaf     home to "~", every parent to its initial
//	hard 1   …/codeaf       the parents dropped entirely
//	hard 2   codeaf         the place, alone
//
// The LAST SEGMENT IS ALWAYS WHOLE, at every strength. It is the only part of
// the path that answers the question the legend is for — which project is this
// pane — and a rule that abbreviated it would be a rule that saved cells by
// deleting the message. A leading dot is kept with the letter after it (".c"
// for ".claude"), because a lone "." is not a name.
func shortPath(dir, home string, hard int) string {
	dir = strings.TrimRight(strings.TrimSpace(dir), "/")
	if dir == "" {
		return ""
	}
	if home = strings.TrimRight(home, "/"); home != "" {
		if dir == home {
			return "~"
		}
		if strings.HasPrefix(dir, home+"/") {
			dir = "~" + strings.TrimPrefix(dir, home)
		}
	}
	parts := strings.Split(dir, "/")
	last := parts[len(parts)-1]
	switch {
	case hard >= 2 || len(parts) == 1:
		return last
	case hard == 1:
		return glyphMore + "/" + last
	}
	for i, part := range parts[:len(parts)-1] {
		parts[i] = initialOf(part)
	}
	return strings.Join(parts, "/")
}

// tildePath is the same path with the person's home written as `~`, and nothing
// else touched.
//
// IT IS [shortPath]'S FIRST STEP ON ITS OWN, and it is separate because the two
// answer different questions. That one is for a LEGEND — a cell of a status row
// where the last segment is the whole message and the parents may be spent down
// to initials. This one is for a path in the transcript, where the surface names
// one of its own files and somebody may be about to select it and paste it into
// a shell: `~` is the one abbreviation that survives that, because the shell
// expands it back. Every other segment is left exactly as it is.
func tildePath(path, home string) string {
	path = strings.TrimSpace(path)
	home = strings.TrimRight(strings.TrimSpace(home), "/")
	if path == "" || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+"/") {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

// initialOf is one path segment, cut to what identifies it: its first rune, or
// the first two when the first is a dot.
func initialOf(segment string) string {
	runes := []rune(segment)
	switch {
	case len(runes) == 0:
		return ""
	case runes[0] == '.' && len(runes) > 1:
		return string(runes[:2])
	default:
		return string(runes[:1])
	}
}

// wrap breaks a block of plain text to width, keeping its own newlines. The
// text is unstyled at this point: styling after wrapping is what keeps every
// width measurement honest.
func wrap(text string, width int) []string {
	if width < 4 {
		width = 4
	}
	text = strings.ReplaceAll(text, "\t", "    ")
	var out []string
	for _, para := range strings.Split(text, "\n") {
		if para == "" {
			out = append(out, "")
			continue
		}
		out = append(out, strings.Split(ansi.Wrap(para, width, ""), "\n")...)
	}
	return out
}

// wrapSheet is [wrap] for a column sheet such as /help. Each source line keeps
// its own column: a row that already starts in spaces stays there, and a row
// whose sentence begins after a gap of spaces continues under that sentence.
// A continuation that fell back to column 0 read as a new key.
func wrapSheet(text string, width int) []string {
	if width < 4 {
		width = 4
	}
	text = strings.ReplaceAll(text, "\t", "    ")
	var out []string
	for _, para := range strings.Split(text, "\n") {
		if para == "" {
			out = append(out, "")
			continue
		}
		out = append(out, wrapSheetLine(para, width)...)
	}
	return out
}

// wrapSheetLine wraps one sheet row so every piece starts at the same column
// as the sentence on the first piece.
func wrapSheetLine(line string, width int) []string {
	at := sheetColumn(line)
	if at <= 0 || at >= width-4 {
		return strings.Split(ansi.Wrap(line, width, ""), "\n")
	}
	head, rest := splitCells(line, at)
	if strings.TrimSpace(rest) == "" {
		return []string{line}
	}
	body := strings.Split(ansi.Wrap(rest, width-at, ""), "\n")
	if len(body) == 0 {
		return []string{line}
	}
	pad := strings.Repeat(" ", at)
	out := make([]string, len(body))
	out[0] = head + body[0]
	for i := 1; i < len(body); i++ {
		out[i] = pad + body[i]
	}
	return out
}

// sheetColumn is where a sheet row's sentence starts, in cells. A row that
// already begins with spaces is hanging there. Otherwise it is the cell after
// the first gap of two or more spaces, which is the column the key's sentence
// is padded to. Zero means the row has no column to keep.
func sheetColumn(line string) int {
	lead := 0
	for _, r := range line {
		if r != ' ' {
			break
		}
		lead++
	}
	if lead > 0 {
		return lead
	}
	gap := 0
	col := 0
	for _, r := range line {
		if r == ' ' {
			gap++
			col++
			continue
		}
		if gap >= 2 {
			return col
		}
		gap = 0
		col += ansi.StringWidth(string(r))
	}
	return 0
}

// noteBlockLines is [wrap]'s opposite number for a block whose own line
// structure is the meaning: the text's own lines, each one FITTED to the width
// and none of them re-flowed.
//
// It is the same shape a fenced block already gets (markdown.go says why in the
// same words: indentation is content, and a paragraph filler does not know it).
// The difference is only which door the text came through — a card printed into
// the conversation by a slash command arrives as a note rather than as prose
// from the model.
func noteBlockLines(text string, width int) []string {
	text = strings.ReplaceAll(text, "\t", "    ")
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, fit(line, width))
	}
	return out
}

// Conversation names use a visible three-dot suffix without changing the stored name.
func fitConversationTitle(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, strings.Repeat(".", min(width, 3)))
}

// fit truncates to a printable width, or returns nothing at all when there is
// no room — a one-cell ellipsis in a one-cell gap says less than a space.
func fit(s string, width int) string {
	fitted, _ := fitWidth(s, width)
	return fitted
}

// fitWidth is fit, and what the result measures.
//
// Callers that lay a row out column by column need both, and measuring a
// string is a grapheme walk — the most expensive thing this surface does per
// character. fit already measures the string to decide whether it fits, so a
// caller that then measures the answer itself pays for the same walk twice.
// Only the truncating branch has to measure again, because ansi.Truncate
// stops at the last cluster that fits and can land under the budget.
func fitWidth(s string, width int) (string, int) {
	if width <= 0 {
		return "", 0
	}
	if measured := ansi.StringWidth(s); measured <= width {
		return s, measured
	}
	cut := ansi.Truncate(s, width, glyphMore)
	return cut, ansi.StringWidth(cut)
}

// The meter's alphabet: what is left, and what has been spent. Both are from
// the block family, so the bar reads as one object rather than as a row of
// glyphs — and both have an ASCII stand-in, because a terminal that cannot draw
// them would otherwise render a countdown as replacement characters.
const (
	meterFull       = "█"
	meterSpent      = "░"
	meterFullASCII  = "#"
	meterSpentASCII = "-"
)

// progress draws a meter of exactly cells glyphs, of which frac is still full.
//
// IT IS A PROPORTION AND NEVER A COUNT: the caller passes what is LEFT, between
// 0 and 1, and the bar says how much of the whole that is. The paint follows the
// same split — the remainder in the question hue, the spent part dim — so what
// is left is the part of the row that is lit.
//
// The last sliver survives rounding: any fraction above zero keeps one cell,
// because an empty bar is the meter saying the thing has already happened, and
// it must not say that a tenth of a second early.
func (a *app) progress(frac float64, cells int) string {
	if cells < 1 {
		return ""
	}
	switch {
	case frac < 0:
		frac = 0
	case frac > 1:
		frac = 1
	}
	full, spent := meterFull, meterSpent
	if a.pal.ascii {
		full, spent = meterFullASCII, meterSpentASCII
	}
	left := int(frac*float64(cells) + 0.5)
	if left > cells {
		left = cells
	}
	if left == 0 && frac > 0 {
		left = 1
	}
	return a.pal.ask(strings.Repeat(full, left)) + a.pal.dim(strings.Repeat(spent, cells-left))
}

// trimBlanks drops leading and trailing empty rows from a block. It is the
// other half of the spacing law: a block that ends in a newline must not hand
// the layout a gap it did not ask for, because the layout would keep it.
func trimBlanks(rows []string) []string {
	for len(rows) > 0 && strings.TrimSpace(rows[0]) == "" {
		rows = rows[1:]
	}
	for len(rows) > 0 && strings.TrimSpace(rows[len(rows)-1]) == "" {
		rows = rows[:len(rows)-1]
	}
	return rows
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// firstProseLine is the first line of a report that says something, and it is
// what the quoted half of a card is built from.
//
// A REPORT IS THE MODEL'S OWN MARKDOWN, and a model that has just run a command
// or produced a diff opens with the fence around it rather than with a sentence
// — the fence is not a defect in the report, it is how the answer is spelled.
// The literal first line of such a report is ``` or ~~~, which is a row of
// punctuation and says nothing at all; quoting it spends the one line the card
// exists to draw on the wrapper around the answer.
//
// SO THE WHOLE LAW IS ONE SENTENCE: a blank line says nothing, a fence marker
// says nothing, and the first line that is neither is what the work came to.
// The markers are read by [mdFenceOpen], the same door the renderer reads them
// through, so a marker with a language word after it — go, sh, diff, json —
// is a marker here for exactly the reason it is one there: a line this surface
// would DRAW as prose is quoted as prose, and there is no second opinion about
// what a fence is.
//
// THAT MEANS A REPORT THAT IS NOTHING BUT A FENCED BLOCK QUOTES THE FIRST LINE
// INSIDE IT, which is the point rather than an exception to it: the work's
// answer IS the block's contents, and a reading that stepped over the whole
// block to look for prose underneath would quote nothing at all — the emptiness
// law doing the opposite of its job, going silent where there was something to
// say.
//
// AND A REPORT WITH NO PROSE AT ALL RETURNS NOTHING, which hands the row to the
// emptiness law where it does belong: the card falls back to its subtitle and,
// failing that, draws the start stamp alone rather than an empty pair of
// quotation marks claiming the work said something ([app.doneUnder] does that
// already, and nothing here has to learn it twice).
func firstProseLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if _, _, fence := mdFenceOpen(trimmed); fence {
			continue
		}
		return trimmed
	}
	return ""
}
