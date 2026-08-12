package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE GRID (§20), as a test every surface in this window is run through.
//
// §20 is one geometry for the whole product, stated in numbers: a 2-cell gutter
// that holds markers and chrome and never content, a content edge at column 2
// that every top-level title and sentence hangs from, an indent step of 2 so
// depth-n content sits at 2+2n, and exactly one blank line between blocks. The
// law was written because four surfaces had each picked their own answer — the
// board drew job names at column 4, the trace double-stepped an expanded result
// to columns 4 and 6, and a job card hung its status text under whatever column
// the job's NAME happened to end at.
//
// A checker is the only way that stays fixed. Each of these facts is a property
// of a rendered frame rather than of a constant, so a renderer can satisfy every
// constant in the tree and still draw off the grid — which is exactly what the
// three defects above did.

// TestThisSurfaceSpellsNoGridNumberOfItsOwn is the checker's other half, and it
// is a CONSTANT test rather than a frame test on purpose.
//
// assertGrid catches a renderer that drew off the ladder. This catches the thing
// that put it there: a file that spelled its own `2`. §20 was written because
// four surfaces had each answered the same question privately, and every one of
// those answers was right on the day it was written — the drift came later, when
// one of them moved. A number aliased to the grid cannot drift, and a number
// spelled again can, so the alias is the law and this is where it is pinned.
func TestThisSurfaceSpellsNoGridNumberOfItsOwn(t *testing.T) {
	for _, pin := range []struct {
		what string
		got  int
		want int
	}{
		{"a message body's indent (markdown.go)", bodyIndent, blocks.ContentEdge},
		{"a part row's marker cells (message.go)", partIndent, blocks.IndentStep},
		{"a question's options (question.go)", optionIndent, blocks.IndentStep},
	} {
		if pin.got != pin.want {
			t.Errorf("%s is %d where §20's grid says %d", pin.what, pin.got, pin.want)
		}
	}
}

// gridChrome is what may stand in the gutter (cols 0–1) or in a child's two
// marker cells: markers, rails, connectors, chevrons and the spinner's frames.
//
// It is a SET OF RUNES rather than a set of strings because the checker's
// question is about one cell: what is the first inked thing on this row, and is
// it a mark or is it a word. Everything here means state, kind, navigation or
// selection — §12's three glyph jobs — and nothing here is content.
func gridChrome() map[rune]bool {
	marks := []string{
		// State (§12), including the plain twins.
		tokens.GlyphQueued, tokens.GlyphWorking, tokens.GlyphSettled,
		tokens.GlyphFailed, tokens.GlyphPaused, tokens.GlyphNeedsHuman,
		tokens.GlyphWaitsOn,
		// Kind: the voices of the record (§5).
		tokens.GlyphThought, tokens.GlyphShell, tokens.GlyphSearch,
		tokens.GlyphWrite, tokens.GlyphPromptChat, tokens.GlyphPromptSteer,
		tokens.GlyphModel,
		// Navigation and disclosure (§10).
		tokens.GlyphCollapsed, tokens.GlyphExpanded, tokens.GlyphScopeUp,
		tokens.GlyphTruncated, tokens.GlyphEllipsis,
		// Structure: rails, connectors, the seam's rule, the cut rule.
		tokens.GlyphAccentRail, tokens.GlyphTreeBranch, tokens.GlyphTreeLast,
		tokens.GlyphTreeVert, tokens.GlyphTreeDash, tokens.GlyphCut,
		// Steps, and the one number-shaped marker a question's options wear:
		// an option's key IS its glyph (question.go), so a digit in a child's
		// marker cells is a mark and not a word.
		tokens.GlyphStepDone, tokens.GlyphStepRunning, tokens.GlyphStepPending,
		tokens.GlyphStepBlocked,
		"0123456789yn",
	}
	out := map[rune]bool{}
	for _, mark := range marks {
		for _, r := range mark {
			out[r] = true
		}
	}
	for _, frame := range blocks.Spinner {
		for _, r := range frame {
			out[r] = true
		}
	}
	return out
}

// gridRow is one row's geometry: where its ink starts and what stands there.
type gridRow struct {
	line  string
	blank bool
	// at is the column of the first inked cell, measured in CELLS and not in
	// bytes — an indent is a distance on screen.
	at int
	// first is the rune standing there.
	first rune
}

// gridScan strips the paint and measures each row. Painting is measured away
// rather than disabled, because a surface that only lines up when it is not
// coloured is a surface that does not line up.
func gridScan(frame string) []gridRow {
	lines := strings.Split(frame, "\n")
	out := make([]gridRow, 0, len(lines))
	for _, line := range lines {
		plain := ansi.Strip(line)
		trimmed := strings.TrimLeft(plain, " ")
		row := gridRow{line: plain}
		if strings.TrimSpace(plain) == "" {
			row.blank = true
			out = append(out, row)
			continue
		}
		row.at = blocks.Width(plain[:len(plain)-len(trimmed)])
		row.first = []rune(trimmed)[0]
		out = append(out, row)
	}
	return out
}

// assertGrid runs one frame past every clause of §20.
func assertGrid(t *testing.T, label string, frame string) {
	t.Helper()
	chrome := gridChrome()
	rows := gridScan(frame)

	blanks := 0
	for i, row := range rows {
		if row.blank {
			// (d) BLOCKS BREATHE, LINES DON'T. Exactly one blank line between
			// top-level blocks — so two in a row is a hole, not a rhythm.
			if blanks++; blanks > 1 {
				t.Errorf("%s row %d: two blank lines in a row — §20 separates blocks with exactly one\n%s",
					label, i, gridShow(rows))
			}
			continue
		}
		blanks = 0

		// (a) THE GUTTER IS CHROME ONLY. Ink at column 0 or 1 must be a mark:
		// a state glyph, a voice glyph, a rail, a connector, a chevron.
		if row.at < blocks.ContentEdge && !chrome[row.first] {
			t.Errorf("%s row %d: content %q starts at column %d, inside the gutter (§20: cols 0–1 are chrome only)\n  %q",
				label, i, string(row.first), row.at, row.line)
			continue
		}

		// (b) + (c) THE LADDER. Every content edge is 2+2n: column 2 for a
		// top-level row, and exactly parent+2 for each level of descent. An odd
		// column is a surface that spent one space or three where the grid says
		// two, which is the drift §20 names in as many words ("never three
		// spaces, never one").
		if row.at >= blocks.ContentEdge && (row.at-blocks.ContentEdge)%blocks.IndentStep != 0 {
			t.Errorf("%s row %d: content edge at column %d is off the %d-cell ladder (2, 4, 6, …)\n  %q",
				label, i, row.at, blocks.IndentStep, row.line)
		}
		// A marker in the gutter puts its content at the edge; a marker in a
		// child's own two cells puts it one rung further. Either way the column
		// the marker sits at is even.
		if row.at < blocks.ContentEdge && row.at%blocks.IndentStep != 0 {
			t.Errorf("%s row %d: a marker at column %d does not start a %d-cell marker pair\n  %q",
				label, i, row.at, blocks.IndentStep, row.line)
		}
	}
}

// gridShow renders a scanned frame with a column ruler, so a failure names the
// column rather than making the reader count.
func gridShow(rows []gridRow) string {
	var b strings.Builder
	b.WriteString("    0.........1.........2.........3.........4\n")
	for i, row := range rows {
		b.WriteString("    ")
		b.WriteString(row.line)
		b.WriteString("|\n")
		_ = i
	}
	return b.String()
}

// -- the surfaces --------------------------------------------------------------

// gridWidths are the measures every surface is checked at. A grid that only
// holds at one width is a coincidence: 40 is narrower than most receipts, 70 and
// 100 are ordinary, and 160 is where a right-aligned receipt used to sit a
// hundred cells from its subject (§20's named defect).
var gridWidths = []int{40, 70, 100, 160}

// THE TRANSCRIPT. Every voice — the reader's marked turn, the assistant's
// unmarked one, a job card with its title, prompt, status, receipt and artifact
// rows — lands on one ladder.
func TestTheTranscriptIsOnTheGrid(t *testing.T) {
	app, backend := runningJobApp(t)
	jobStatus(backend, "job-1", "reworking NavCtx after the split")
	backend.add(store.Message{SessionID: testSession, Role: store.RoleUser,
		Body: "can you also bring the history sheet over, and the tab strip with it"})
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent,
		Body: "Yes. The wisp browser is missing both.\n\nI will start with the tab strip."})
	poll(t, app)

	for _, width := range gridWidths {
		for i := 0; i < app.transcript.Len(); i++ {
			assertGrid(t, gridLabel("transcript", width, i),
				blockRows(t, app, i, width))
		}
	}
}

// THE RECORD. The delivery card, the `── execution ──` seam and its legend, the
// tool rows, their batches and their opened output boxes.
func TestTheRecordIsOnTheGrid(t *testing.T) {
	backend := recordBoard()
	commander := &tracingCommander{traces: map[string]string{"job-3": traceFixture}}
	app := newTestApp(backend, commander, nil)
	poll(t, app)
	enterTracedRoom(t, app, "5")

	// The room's OWN blocks, built the way the record page builds them. The
	// entered room does not live in app.transcript, so walking that list would
	// have been a test that checked nothing and said so in green.
	room := roomBlocks(recordInputs{
		record:     app.source.workRecordAt(app.view.node),
		messages:   app.view.messages,
		style:      app.style,
		board:      app.source,
		traces:     app.traces,
		open:       app.foldOpen,
		treeOpen:   app.foldOpenDefault,
		money:      app.source.jobSpend(app.view.node),
		models:     app.source.jobModels(app.view.node),
		nodeModels: app.source.jobModels,
		now:        app.now(),
		clock:      app.view.transcript.Clock(),
	})
	if len(room) == 0 {
		t.Fatal("the record fixture built no blocks, so this test would check nothing")
	}
	for _, width := range gridWidths {
		for i, block := range room {
			assertGrid(t, gridLabel("record", width, i),
				strings.Join(block.Rows(width), "\n"))
		}
	}
}

// THE BOARD, list and detail pages alike — the surface that was a whole step
// off the grid, drawing its glyphs at the content edge and its names past it.
func TestTheBoardIsOnTheGrid(t *testing.T) {
	app, _ := boardApp(t)
	app.runFooterVerb(placeBoardID)
	if app.page != pageBoard {
		t.Fatalf("the work tab left the window on the %s page", app.page)
	}
	for _, width := range gridWidths {
		// The board PANE alone: the footer and the composer belong to another
		// lane, and a law about this page's columns is asserted against this
		// page's rows.
		assertGrid(t, gridLabel("board", width, 0),
			strings.Join(boardPageRows(t, app, width, 40), "\n"))
	}
}

func gridLabel(surface string, width, index int) string {
	return surface + " @" + itoaTest(width) + " block " + itoaTest(index)
}

func itoaTest(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
