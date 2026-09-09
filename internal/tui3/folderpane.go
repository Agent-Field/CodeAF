package tui3

// THE PREVIEW PANE, AND HOW THE SHEET'S WIDTH IS DIVIDED.
//
// The owner's reference is Yazi's arrangement (../reference-yazi.png): a
// readable path along the top, a NARROW ancestry column on the left, a GENEROUS
// list of what you are standing in, and a LARGE preview on the right — with the
// sizes right-aligned, one unmistakable selection, and compact controls at the
// foot. This file is the arithmetic of that, adapted to this surface's own
// tokens: no borders, no rules between the columns, no colour behind anything,
// and the columns told apart by the gaps and by the ink the way every other
// block down here is.
//
// THE PREVIEW TAKES THE THIRD COLUMN'S PLACE AND IT IS AN IMPROVEMENT ON IT
// rather than an addition. The browse used to draw the CHILDREN of the row
// under the cursor over there, which meant one readdir per cursor move on top
// of the two already in flight, and it could only ever show a folder. The
// preview pane shows the same folder's contents — the same readdir, done by
// contextpreview.go, cached and cancellable — AND a file's source with syntax
// colour, AND a picture, AND a PDF's text. So the region is stable: it is
// always "what is the thing under the cursor", whichever kind of thing that is.
//
// ── THREE STATES, TWO KEYS, AND THE RULE THAT MAKES SCROLLING OBVIOUS ───────
//
// The pane is BESIDE the list, ALONE on the sheet, or OFF. alt+p turns it off
// and back on; alt+o gives it the whole sheet and gives the sheet back.
//
// And the pane the arrow keys act on is THE PANE THAT IS DRAWN — there is no
// focus to lose track of. With a list on screen `↑↓` walk the list and
// `shift+↑↓` scroll the preview beside it; with the preview alone there is no
// list to walk, so `↑↓` scroll it and `←→` slide it sideways. A modal focus
// would have needed a fourth thing on the foot row to say where the keyboard
// currently was.

import (
	"path/filepath"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// folderPane is where the preview is, if anywhere.
type folderPane uint8

const (
	// folderPaneBeside is the default and the screenshot's: the preview holds
	// the right of the sheet with the list beside it.
	folderPaneBeside folderPane = iota
	// folderPaneWide is the preview with the whole sheet — for reading a file
	// rather than for choosing between files.
	folderPaneWide
	// folderPaneOff is no preview at all, which is what a person browsing a deep
	// tree on a small terminal wants: every cell spent on names.
	folderPaneOff
)

// The two keys that move the pane between those states. They are constants
// because the manual quotes both of them exactly as they are spelled here, and
// because the foot row's own legend is built from the same strings.
const (
	folderPaneKey = "alt+p"
	folderWideKey = "alt+o"
	folderMarkKey = "alt+m"
)

// The widths the division turns on.
//
// THE MIDDLE COLUMN IS STILL THE ONE THAT SURVIVES, which is folderpick.go's
// own law and the reason the order below is preview, then ancestry, then names:
// a column that would leave the names unreadable is a column not worth drawing,
// and the names are where a person's hands are.
const (
	// folderPaneAt is the narrowest sheet that gets a preview beside the list.
	// Under it the preview is available ALONE (alt+o) rather than squeezed in
	// beside a column of eight-character names [steering-02 §2: "Narrow
	// terminals retain legible filenames ... with an optional full preview view
	// rather than squeezing everything"].
	folderPaneAt = 64
	// folderPaneFloor is the narrowest preview worth drawing. Under thirty cells
	// a line of source is an ellipsis and a picture is a smudge.
	folderPaneFloor = 30
	// folderPaneCap stops the preview from eating a very wide terminal: past
	// this the extra cells are worth more to the list, which is where the
	// choosing happens.
	folderPaneCap = 78
	// folderNumbersAt is the narrowest preview that gets the line-number gutter.
	// It is the figure reports/preview.md suggests and the gutter is three to
	// seven dim cells, so it is worth having only where the text can spare them.
	folderNumbersAt = 44
)

// folderLayout is the sheet's width, divided. A zero column is a column that is
// not drawn at all — never a column drawn one cell wide.
type folderLayout struct {
	up, here, pane int
}

// folderDivide is how the frame's width is divided between the ancestry column,
// the names and the preview.
//
// It replaces [folderColumns] and keeps its two floors, so a narrowing frame
// gives up the preview before the ancestry and the ancestry before the names.
func folderDivide(width int, pane folderPane) folderLayout {
	room := width - folderPadCells - 1
	if room < 1 {
		return folderLayout{here: max(width, 1)}
	}
	if pane == folderPaneWide {
		// THE PREVIEW ALONE IS THE WHOLE SHEET, ancestry included: somebody who
		// asked to read the file has stopped choosing between files.
		return folderLayout{pane: room}
	}
	out := folderLayout{}
	// THE ANCESTRY COLUMN IS THE NARROWEST THING ON THE SHEET, which is the
	// reference screenshot's own proportion: about an eighth of the room, capped,
	// because it is CONTEXT — "where am I standing" — and not a list anybody
	// reads down. Its own floor is the give-way ladder below rather than a second
	// threshold up here: a sheet wide enough for a preview and a readable middle
	// column is wide enough for it, and a sheet that is not gives it up first.
	if room >= folderWideAt {
		out.up = min(room/8, 20)
	}
	// THE NAMES ARE GIVEN WHAT THEY WANT AND THE PREVIEW TAKES THE REST, which is
	// the right way round and was not the first way this was written. Handing the
	// preview a fixed FRACTION left the names sixty cells of empty middle with the
	// size stranded against a far edge — a column of two things a person's eye
	// cannot associate. A name and its size want about [folderHereWant] cells
	// between them, and every cell past that is worth more to the preview.
	if pane == folderPaneBeside && room >= folderPaneAt {
		rest := room - out.up - folderGapFor(out.up) - 1
		out.pane = min(max(rest-folderHereWant, folderPaneFloor), max(folderPaneCap, rest/2))
		out.here = rest - out.pane
		// A column that would leave the names unreadable is a column not worth
		// drawing, and the preview goes before the ancestry does.
		if out.here < folderNameFloor {
			out.pane = 0
		}
	}
	if out.pane == 0 {
		out.here = room - out.up - folderGapFor(out.up)
	}
	if out.here < folderNameFloor && out.up > 0 {
		out.here, out.up = room, 0
	}
	out.here = max(out.here, 1)
	return out
}

// folderHereWant is how many cells a name and its size want between them. A
// directory name a person recognizes plus a right-aligned `12.4 KB` reads well
// in this much and reads worse in three times it, so the surplus goes to the
// preview instead.
const folderHereWant = 56

// paneWide reports whether the preview is DRAWN BESIDE THE LIST at this width.
// It is the question the foot row asks before it decides whether to name the
// preview's own door ([folderPick.controlLegend]), and it takes the legend's own
// room rather than the sheet's — one cell of arithmetic in one place.
func (f *folderPick) paneWide(room int) bool {
	return folderDivide(room+folderPadCells, f.pane).pane > 0
}

// paneShown reports whether the preview has any cells on this frame, which is
// what decides whether the sheet asks for a preview at all.
func (f *folderPick) paneShown(width int) bool {
	return f.browsing && folderDivide(width, f.pane).pane > 0
}

// paneBox is the region the preview is drawn into: the layout's cells, the body
// rows the columns were given, and this browser's own two scroll offsets.
func (f *folderPick) paneBox(width, rows int) previewBox {
	pane := folderDivide(width, f.pane).pane
	return previewBox{
		Width: pane, Height: rows,
		Top: f.paneTop, Left: f.paneLeft,
		Numbers: pane >= folderNumbersAt,
	}
}

// paneRows draws the preview into that region, filled out to its full height so
// the columns beside it stay the same length.
//
// IT GOES THROUGH THE ONE-ENTRY MEMO. [previewCanvas] holds the last answer and
// its key changes exactly when something visible does, so a sheet redrawn on a
// tick does not lex the same forty rows of source again (reports/preview.md's
// stated cost: about sixty microseconds a row).
// THE SCROLL IS CLAMPED HERE AND NOWHERE ELSE, against the BODY box rather than
// the outer one — the foot has taken its row, and getting this wrong makes the
// last line of a file unreachable by exactly one row (reports/preview.md §5).
//
// It is clamped on the DRAW rather than on the key because only the draw knows
// the box: how wide the pane is and how many rows it was given are answers about
// the frame, and a key handler that had to be told the frame's size to move a
// pane one row would be a second layout that could disagree with this one.
func (f *folderPick) paneRows(pal palette, st *tokens.Styler, width, rows, hot int) []string {
	box := f.paneBox(width, rows)
	if box.Width < 1 || box.Height < 1 {
		return nil
	}
	body := previewBodyBox(pal, f.preview, box)
	body.Top = f.paneTop
	f.paneTop = previewClampTop(f.preview, body)
	box.Top = f.paneTop
	out := previewPad(f.canvas.rows(pal, st, f.preview, box), box)
	// THE MAP FROM A ROW TO THE ENTRY ON IT IS WRITTEN BY THE FUNCTION THAT DRAWS
	// THE ROWS, which is the whole of why it is here rather than beside the press:
	// the scroll offset and the height the foot left over are answers about THIS
	// frame, and a press resolved against a second copy of that arithmetic is a
	// press on the row above the one somebody aimed at.
	//
	// It is filled ONLY for a folder preview. A file's source and a picture are
	// read-only over there and stay that way — the pane is not a fake list of
	// things to click (steering-02 §5, and the brief this wave answers).
	f.geom.paneDir, f.geom.paneFrom, f.geom.paneBody = "", 0, 0
	if f.preview.Kind == previewFolder && len(f.preview.Entries) > 0 {
		f.geom.paneDir = f.preview.Key.Path
		f.geom.paneFrom = box.Top
		f.geom.paneBody = min(body.Height, len(f.preview.Entries)-box.Top)
	}
	// AND ONLY AN ENTRY ROW LIGHTS. The foot under the listing states a fact and
	// is not a target, so a band across it would be the sheet offering a press
	// that does nothing (hover.go's law).
	if hot < 0 || hot >= f.geom.paneBody || hot >= len(out) {
		return out
	}
	// THE HOVERED ROW IS PAINTED HERE AND NOT IN THE PREVIEW'S OWN DRAW, because
	// the draw goes through a one-entry memo keyed on what is VISIBLE and the
	// pointer is not part of that key — painting inside it would either poison the
	// cache or add the pointer to a key that changes on every mouse motion. The
	// slice is copied for the same reason: the rows it holds belong to the memo.
	lit := make([]string, len(out))
	copy(lit, out)
	lit[hot] = pal.cursor(lit[hot], 0)
	return lit
}

// paneEntry is the RAW name of the folder-preview entry drawn on one body row
// of the pane, and false where that row holds no entry — a file's source, a
// picture, a refusal, the foot, or a row past the end of a short listing.
//
// IT ANSWERS THE RAW NAME AND NEVER THE DRAWN ONE. [previewEntry.Name] has been
// through [drawableLine] on the way in, which is what makes it safe to paint and
// exactly what makes it unsafe to navigate with: a file called `ok\e[2Jgone` is
// a legal name on every filesystem this program runs on, and joining the
// scrubbed label back onto a directory would open a path nobody has.
func (f *folderPick) paneEntry(row int) (string, bool) {
	if f.geom.paneDir == "" || row < 0 || row >= f.geom.paneBody {
		return "", false
	}
	// AND THE PREVIEW BEING HELD NOW MUST BE THE ONE THAT WAS DRAWN. A preview
	// arrives off the loop and the row map is written by the paint, so there is a
	// frame in which a new folder's entries sit behind the last folder's
	// geometry — and a press landing in it would join one directory's row number
	// onto another directory's path. The identity is what tells them apart, and it
	// is the same identity every cache on this path is keyed by
	// (contextpreview.go's [previewKey]).
	if f.preview.Kind != previewFolder || f.preview.Key.Path != f.geom.paneDir {
		return "", false
	}
	at := f.geom.paneFrom + row
	if at < 0 || at >= len(f.preview.Entries) {
		return "", false
	}
	name := f.preview.Entries[at].Raw
	if name == "" {
		return "", false
	}
	return name, true
}

// paneStep moves the preview by whole rows. The ceiling belongs to the draw
// ([folderPick.paneRows]); the floor is here because zero is the top of every
// file whatever size the pane is.
func (f *folderPick) paneStep(delta int) { f.paneTop = max(f.paneTop+delta, 0) }

// folderSlideStep is how many cells one sideways press moves. Eight is about an
// indentation level of source, which is the unit a person sliding a code preview
// is actually thinking in.
const folderSlideStep = 8

// paneSlide moves the preview sideways, in CELLS, which is what lets a person
// read past the right edge of a long line without the pane wrapping and
// destroying the indentation source code is read by.
//
// The floor is zero and there is no ceiling, because the length of the longest
// line in the part of the file that was read is not a bound worth computing on
// a keystroke: a slide past the end shows an empty pane and one press of the
// other arrow brings the text back.
func (f *folderPick) paneSlide(delta int) {
	f.paneLeft = max(f.paneLeft+delta, 0)
}

// paneRest puts both offsets back, which is what moving onto another file
// means: the top of the new file, and its left margin.
func (f *folderPick) paneRest() { f.paneTop, f.paneLeft = 0, 0 }

// ── the marks, as a row a person can read and press ─────────────────────────

// folderMarkGlyph is the cell a marked row wears, in the list and on the tray.
// The ascii floor gets an asterisk rather than a box approximation, for the
// reason styles.go states — a terminal that cannot draw U+25AA should be given
// something it can, not something close.
func folderMarkGlyph(pal palette) string {
	if pal.ascii {
		return "*"
	}
	return "▪"
}

// folderTrayGap is the space between two cells of the mark tray, and it is
// attach.go's own gap: two cells, because one reads as a single wrapped label
// and three reads as a column.
const folderTrayGap = "  "

// folderTrayCells is the marks as cells, in the order they were marked, each
// naming the thing rather than its whole path — a tray is a reminder and not a
// location (attach.go's [chip.name] makes the same choice).
//
// A DIRECTORY CARRIES A TRAILING SLASH AND NOT A GLYPH, which is the same
// spelling the columns and the preview use for the same fact: no Nerd Font, no
// width surprise, and it is already how a person writes the name.
func folderTrayCells(marks []folderMark, pal palette) []string {
	if len(marks) == 0 {
		return nil
	}
	mark := folderMarkGlyph(pal)
	out := make([]string, 0, len(marks))
	for _, held := range marks {
		name := filepath.Base(held.path)
		if held.dir {
			name += "/"
		}
		out = append(out, mark+" "+name)
	}
	return out
}

// folderTrayRow draws the marks as one row above the action row, with the
// count first so a tray that has overflowed the frame still says how many
// things the confirm is carrying.
//
// THE COUNT IS THE PART THAT CANNOT BE GIVEN UP. Cells are dropped from the
// right, whole rather than truncated (pickrow.go's law about an answer): half a
// filename is a filename somebody reads as another file. The count stays,
// because "3" is the fact that makes the action row's sentence make sense.
func (f *folderPick) trayRow(width int, pal palette, hot int) string {
	cells := folderTrayCells(f.marks, pal)
	if len(cells) == 0 {
		return ""
	}
	room := width - folderPadCells
	lead := itoa(len(f.marks)) + folderMarkedWord
	line, used := pal.ink(lead), ansi.StringWidth(lead)
	f.geom.trayCells = f.geom.trayCells[:0]
	for at, cell := range cells {
		step := len(folderTrayGap) + ansi.StringWidth(cell)
		if used+step > room {
			break
		}
		span := hudSpan{from: folderPadCells + used + len(folderTrayGap)}
		span.to = span.from + ansi.StringWidth(cell)
		painted := pal.dim(cell)
		if at == hot {
			painted = pal.cursor(painted, 0)
		}
		line += folderTrayGap + painted
		used += step
		f.geom.trayCells = append(f.geom.trayCells, span)
	}
	if rest := len(cells) - len(f.geom.trayCells); rest > 0 {
		if more := " +" + itoa(rest); used+ansi.StringWidth(more) <= room {
			line += pal.dim(more)
		}
	}
	return folderPad + line
}

// folderMarkedWord follows the count on the tray row. It is a constant because
// the manual quotes it exactly as it is spelled here.
const folderMarkedWord = " chosen ·"

// trayPress resolves a click on the mark tray, and reports whether it took one.
// A mark comes OFF when its cell is pressed, which is the same gesture the
// message tray above the box gives a picture (attach.go's [app.chipPress]).
func (f *folderPick) trayPress(x int) bool {
	for at, span := range f.geom.trayCells {
		if span.holds(x) && at < len(f.marks) {
			f.marks = append(f.marks[:at], f.marks[at+1:]...)
			return true
		}
	}
	return false
}
