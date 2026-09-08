package tui3

// A PREVIEW, DRAWN — AND NOTHING ELSE IN THIS FILE TOUCHES A DISK.
//
// contextpreview.go reads a file into a [filePreview], which carries no palette,
// no width and no escape sequence. This file turns one of those into rows. The
// split is what makes the pane cheap and what makes it testable: a resize, a
// scroll, a theme change and a re-measured terminal all redraw from a value that
// is already in memory, and a test can pin exactly what a person sees without a
// fixture tree.
//
// EVERY FUNCTION HERE IS PURE. No app, no state, no clock. Give it a palette, a
// preview and a box and it answers the same rows every time.
//
// THE PANE IS A RECTANGLE AND NOTHING LEAVES IT. Every row is fitted to
// box.Width with [fit], which measures the way the renderer underneath composes
// its grid, and every line of somebody else's text was already stripped of
// escapes and control bytes on the way in ([previewLines]). A file cannot repaint
// the frame it is being previewed in.
//
// THE LAYOUT IS THE SCREENSHOT'S, ADAPTED. A body that fills the region, and one
// dim foot line stating what the thing is and what is not being shown — the
// limitation on the left, the facts on the right. Nothing else: no border, no
// title bar, no colour behind the pane [design-law: internal/tui is the visual
// north star].

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/prose"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// previewBox is where a preview is drawn and how much of it is showing.
type previewBox struct {
	// Width and Height are the rectangle, in cells and rows. A preview never
	// draws more than Height rows and never a row wider than Width.
	Width, Height int
	// Top is the first body row shown, counting from zero: the scroll offset.
	// It is clamped by [previewClampTop] rather than trusted, so a pane that
	// scrolled and then shrank cannot ask for a row that is not there.
	Top int
	// Left is how many cells are clipped off the left of every text row: the
	// horizontal scroll. It is what lets a person read past the right edge of a
	// long line without the pane wrapping, which would destroy the indentation
	// that source code is read by.
	Left int
	// Numbers draws the subdued line-number gutter. It is a caller's choice
	// because a narrow pane is better off spending those cells on the text.
	Numbers bool
}

// previewGutterMin is the narrowest line-number gutter, so a file's first
// hundred lines do not shift sideways as the number gains a digit.
const previewGutterMin = 2

// previewWidthMin is the narrowest pane worth drawing text into. Under this
// there is room for a gutter and an ellipsis and nothing a person could read.
const previewWidthMin = 12

// previewRows is THE ONE DOOR: a preview and a box, and the rows to paint.
//
// The last row of the box is the foot, when there is something to say on it and
// the box is at least two rows tall. The body gets everything else. A caller
// that wants the region filled to its full height pads with [previewPad] — the
// rows come back as long as they are, because a caller composing columns beside
// each other needs to know which of them ended.
func previewRows(pal palette, st *tokens.Styler, pv filePreview, box previewBox) []string {
	if box.Width < 1 || box.Height < 1 || pv.empty() {
		return nil
	}
	foot := previewFoot(pal, pv, box.Width)
	body := previewBodyBox(pal, pv, box)
	rows := previewBodyRows(pal, st, pv, body)
	if foot == "" || box.Height <= 1 {
		return rows
	}
	// The foot sits at the FOOT, so it does not move as a body of two lines is
	// drawn into a pane of thirty. A pane whose contents jump when the cursor
	// moves is a pane a person stops reading.
	for len(rows) < body.Height {
		rows = append(rows, "")
	}
	return append(rows[:body.Height], foot)
}

// previewBodyBox is the region the BODY actually gets, once the foot has taken
// its row.
//
// It is exported to the rest of the package rather than kept inside
// [previewRows] because a pane's scrolling arithmetic has to agree with its
// drawing: a caller working out how far a wheel or a page key may move must
// clamp against the same height the rows were drawn into, or the last line of a
// file becomes unreachable by exactly one row. The recipe is
// `previewClampTop(pv, previewBodyBox(pal, pv, box))`.
func previewBodyBox(pal palette, pv filePreview, box previewBox) previewBox {
	body := box
	if box.Height > 1 && previewFoot(pal, pv, box.Width) != "" {
		body.Height = box.Height - 1
	}
	return body
}

// previewBodyRows is the pane's contents, without the foot.
func previewBodyRows(pal palette, st *tokens.Styler, pv filePreview, box previewBox) []string {
	if box.Height < 1 || box.Width < 1 {
		return nil
	}
	switch pv.Kind {
	case previewFolder:
		if len(pv.Entries) == 0 {
			return previewNoteRows(pal, pv, box)
		}
		return previewFolderRows(pal, pv, box)
	case previewPicture:
		if pv.Thumb == nil || !pal.paintsPictures() {
			return previewNoteRows(pal, pv, box)
		}
		return previewPictureRows(pal, pv, box)
	case previewSource, previewProse, previewDocument:
		if len(pv.Lines) == 0 {
			return previewNoteRows(pal, pv, box)
		}
		return previewTextRows(pal, st, pv, box)
	}
	return previewNoteRows(pal, pv, box)
}

// previewSaysInBody answers whether the preview's one sentence is the whole of
// what the body has to show. When it is, the foot keeps only the facts, so a
// person is not told the same thing twice on two rows.
func previewSaysInBody(pal palette, pv filePreview) bool {
	switch pv.Kind {
	case previewOpaque, previewRefused, previewNothing:
		return true
	case previewFolder:
		return len(pv.Entries) == 0
	case previewPicture:
		return pv.Thumb == nil || !pal.paintsPictures()
	}
	return len(pv.Lines) == 0
}

// previewNoteRows is the body of a preview that has no contents to show: the
// one sentence, dim, at the top of the region.
//
// It is at the TOP rather than centred because it is a caption on an empty
// space, and a caption that moves with the height of the pane is a caption a
// person's eye has to find again on every resize.
func previewNoteRows(pal palette, pv filePreview, box previewBox) []string {
	if pv.Note == "" {
		return nil
	}
	return []string{pal.dim(fit(pv.Note, box.Width))}
}

// ── text ────────────────────────────────────────────────────────────────────

// previewTextRows draws source, prose or a document's text: one row per line,
// FITTED and never wrapped, with an optional subdued number gutter.
//
// NEVER WRAPPED is the same judgement internal/tui2/prose makes about a fenced
// block, and it matters more here: indentation is how source is read, and a
// continuation at column zero puts a lie where the eye is counting levels. The
// answer to a long line is [previewBox.Left], which slides the whole pane
// sideways and keeps every row's shape.
//
// THE LINE IS FITTED BEFORE IT IS PAINTED, which is [app.codeRowsWith]'s rule
// and is the only order that can be measured: a string with escape sequences
// already in it measures wrong, so the truncation happens on plain text and the
// highlighter is handed something that already fits.
func previewTextRows(pal palette, st *tokens.Styler, pv filePreview, box previewBox) []string {
	if box.Height < 1 || box.Width < 1 || len(pv.Lines) == 0 {
		return nil
	}
	top := previewClampTop(pv, box)
	last := top + box.Height
	if last > len(pv.Lines) {
		last = len(pv.Lines)
	}
	gutter := 0
	if box.Numbers && box.Width >= previewWidthMin {
		gutter = previewGutterWidth(last) + 1
	}
	text := box.Width - gutter
	if text < 1 {
		gutter, text = 0, box.Width
	}
	// Highlighting is refused on the screen-reader tier for [codeLang]'s stated
	// reason: syntax colour is a claim carried by hue alone, which a person
	// listening does not receive, and lexing a file to say nothing is a whole
	// pass spent on an audience that cannot hear it.
	lang := pv.Lang
	if pal.linear {
		lang = ""
	}
	out := make([]string, 0, last-top)
	for at := top; at < last; at++ {
		// SCRUBBED AGAIN, AND ON PURPOSE. [previewLines] already did this and is
		// the only producer today, so this pass finds nothing — which is exactly
		// why it is here: the law is that no file's control bytes reach the
		// frame, and a law that holds only while one function upstream keeps
		// holding it is a law with a caller-shaped hole in it. It costs a string
		// walk against the highlighter's sixty microseconds a row.
		line := drawableLine(pv.Lines[at])
		if box.Left > 0 {
			// The clip is by CELLS, not by bytes: a pane scrolled four cells past
			// a line of Japanese must land between characters and not inside one.
			line = ansi.TruncateLeft(line, box.Left, "")
		}
		fitted := fit(line, text)
		painted := ""
		switch {
		case strings.TrimSpace(fitted) == "":
			// A blank line is still a row — dropping them would close the gaps a
			// person reads a file's structure by — and it is a row with nothing
			// in it to highlight.
			painted = pal.dim(fitted)
		case lang == "":
			painted = pal.dim(fitted)
		default:
			// [codeTier] is the same quiet grey an opened `read` wears, so a
			// preview and a tool expansion read as the same kind of block, and
			// [prose.HighlightLine] lights up only where the lexer actually found
			// something.
			painted = prose.HighlightLine(st, fitted, lang, codeTier)
		}
		if gutter > 0 {
			painted = previewNumber(pal, at+1, gutter) + painted
		}
		out = append(out, painted)
	}
	return out
}

// previewNumber is one subdued line number, right-aligned in its gutter with the
// separating space already on it.
//
// SUBDUED IS THE WHOLE POINT [steering-02 §3: "line numbers that do not
// dominate"]. It is drawn at [palette.dim], the quietest ink on this surface, so
// the column reads as a ruler beside the text rather than as a second column of
// content competing with it.
func previewNumber(pal palette, line, gutter int) string {
	if gutter < 2 {
		return ""
	}
	number := itoa(line)
	if len(number) > gutter-1 {
		number = number[len(number)-(gutter-1):]
	}
	return pal.dim(strings.Repeat(" ", gutter-1-len(number)) + number + " ")
}

// previewGutterWidth is how many cells the numbers need for the last line that
// will be drawn, never under [previewGutterMin].
func previewGutterWidth(last int) int {
	width := len(itoa(last))
	if width < previewGutterMin {
		width = previewGutterMin
	}
	return width
}

// previewClampTop is the one law for how far a preview may be scrolled, so the
// pane, the wheel and the page keys cannot disagree about it.
//
// The floor is zero and the ceiling leaves the LAST line on screen rather than
// scrolling into empty space below it — a person who has reached the end of what
// was read should see it, not a blank pane with a foot under it.
func previewClampTop(pv filePreview, box previewBox) int {
	span := previewSpan(pv)
	last := span - box.Height
	if last < 0 {
		last = 0
	}
	switch {
	case box.Top < 0:
		return 0
	case box.Top > last:
		return last
	}
	return box.Top
}

// previewSpan is how many body rows this preview has to scroll through — the
// text's lines, or a folder's entries. A picture has none: it is drawn to fit
// whatever region it is given.
func previewSpan(pv filePreview) int {
	switch pv.Kind {
	case previewFolder:
		return len(pv.Entries)
	case previewSource, previewProse, previewDocument:
		return len(pv.Lines)
	}
	return 0
}

// ── a folder ────────────────────────────────────────────────────────────────

// previewFolderRows draws a directory's contents: the name on the left and the
// size right-aligned against the edge, which is the screenshot's own alignment
// and the one that makes a column of sizes readable at a glance.
//
// A DIRECTORY CARRIES A TRAILING SLASH AND NOT A GLYPH. Icons are an optional
// enhancement with a plain-font fallback [steering-02 §5], and `src/` is the
// fallback that has been legible on every terminal since 1978 — no Nerd Font,
// no width surprise, and it is already how a person writes the name.
//
// A DIRECTORY HAS NO SIZE DRAWN. The number a filesystem gives for a directory
// is the size of its own bookkeeping, not of what is inside it, and drawing it
// beside file sizes would be a column of two different meanings
// [design-law §EMPTINESS].
func previewFolderRows(pal palette, pv filePreview, box previewBox) []string {
	if box.Height < 1 || box.Width < 1 || len(pv.Entries) == 0 {
		return nil
	}
	top := previewClampTop(pv, box)
	last := top + box.Height
	if last > len(pv.Entries) {
		last = len(pv.Entries)
	}
	out := make([]string, 0, last-top)
	for _, entry := range pv.Entries[top:last] {
		// A FILENAME IS SOMEBODY ELSE'S BYTES TOO ([previewEntry.Name]), and the
		// same defence applies here as to a line of text.
		name := drawableLine(entry.Name)
		if entry.Dir {
			name += "/"
		}
		size := ""
		if !entry.Dir {
			size = byteWord(int(entry.Bytes))
		}
		out = append(out, previewNameAndSize(pal, name, size, entry.Dir, box.Width))
	}
	return out
}

// previewNameAndSize lays one folder row out: the name, then whatever space is
// left, then the size against the right edge.
//
// The size is given up before the name is, because the name is what a person
// came to read. Under [previewWidthMin] the size is not drawn at all.
func previewNameAndSize(pal palette, name, size string, dir bool, width int) string {
	ink := pal.muted
	if dir {
		// A directory is the row a person can go INTO, so it wears the body ink
		// and a file wears the quieter one. Two tiers is the whole colour scheme
		// here: a filename coloured by its type is a legend nobody was given
		// [steering-02 §5].
		ink = pal.ink
	}
	if size == "" || width < previewWidthMin {
		return ink(fit(name, width))
	}
	room := width - ansi.StringWidth(size) - 1
	if room < 4 {
		return ink(fit(name, width))
	}
	fitted, used := fitWidth(name, room)
	return ink(fitted) + strings.Repeat(" ", width-used-ansi.StringWidth(size)) + pal.dim(size)
}

// ── a picture ───────────────────────────────────────────────────────────────

// previewPictureRows paints the picture into the region with its ASPECT RATIO
// PRESERVED, through the half-cell renderer this surface already has
// (imagepreview.go): one cell is two stacked pixels, U+2580 over two colours,
// and nothing but SGR reaches the frame.
//
// IT IS NOT KITTY OR ITERM GRAPHICS AND MUST NOT BE DESCRIBED AS THOUGH IT WERE.
// This surface composes every frame as rows of text and hands it over whole, so
// an inline-image escape sequence written into one frame is painted over by the
// next. What comes back here is a real colour picture at cell resolution —
// coarser than a graphics protocol, and the only kind that survives a repaint.
//
// The picture is CENTRED horizontally. A preview pane is wider than most
// pictures are once their height is bounded, and a portrait pinned to the left
// edge of an empty region reads as a layout mistake rather than as a photograph.
func previewPictureRows(pal palette, pv filePreview, box previewBox) []string {
	if pv.Thumb == nil || box.Height < 1 || box.Width < pictureColsMin || !pal.paintsPictures() {
		return nil
	}
	cols, rows := pictureGrid(pv.Wide, pv.High, box.Width, box.Height)
	if cols < 1 || rows < 1 {
		return nil
	}
	painted := pictureCells(pal, pv.Thumb, cols, rows)
	pad := (box.Width - cols) / 2
	if pad <= 0 {
		return painted
	}
	margin := strings.Repeat(" ", pad)
	out := make([]string, 0, len(painted))
	for _, row := range painted {
		out = append(out, margin+row)
	}
	return out
}

// ── the foot ────────────────────────────────────────────────────────────────

// previewFoot is the one dim line under the body: what is not being shown on the
// left, and what the thing is on the right.
//
// The two halves are drawn on one row because they answer one question between
// them — "what am I looking at, and how much of it" — and two rows of quiet
// telemetry under a pane is one row more chrome than the answer is worth.
func previewFoot(pal palette, pv filePreview, width int) string {
	if width < 1 {
		return ""
	}
	note := pv.Note
	if previewSaysInBody(pal, pv) {
		// Already said above, in the space where the contents would have been.
		note = ""
	}
	facts := previewFacts(pv)
	switch {
	case note == "" && facts == "":
		return ""
	case note == "":
		return pal.dim(fit(facts, width))
	case facts == "":
		return pal.dim(fit(note, width))
	}
	gap := width - ansi.StringWidth(note) - ansi.StringWidth(facts)
	if gap < 2 {
		// No room for both. The note is the part a person cannot work out for
		// themselves, so it is the part that stays.
		return pal.dim(fit(note, width))
	}
	return pal.dim(note + strings.Repeat(" ", gap) + facts)
}

// previewFacts is what the thing IS, in the fewest true words: a picture's
// shape and format, a file's language and length, a folder's counts.
//
// NOTHING UNKNOWN IS DRAWN. A file whose line count nobody counted says nothing
// about lines; a picture that would not decode states no dimensions
// [design-law §EMPTINESS]. That is why this is built by appending rather than by
// a format string with holes in it.
func previewFacts(pv filePreview) string {
	var parts []string
	switch pv.Kind {
	case previewFolder:
		folders, files := 0, 0
		for _, entry := range pv.Entries {
			if entry.Dir {
				folders++
			} else {
				files++
			}
		}
		if folders > 0 {
			parts = append(parts, itoa(folders)+" "+plural("folder", folders))
		}
		if files > 0 {
			parts = append(parts, itoa(files)+" "+plural("file", files))
		}
		if extra := pv.Shown - len(pv.Entries); extra > 0 {
			parts = append(parts, itoa(extra)+" more not shown")
		}
	case previewPicture:
		if pv.Wide > 0 && pv.High > 0 {
			parts = append(parts, itoa(pv.Wide)+"×"+itoa(pv.High))
		}
		if pv.Format != "" {
			parts = append(parts, pv.Format)
		}
		if size := byteWord(int(pv.Key.Bytes)); size != "" {
			parts = append(parts, size)
		}
	default:
		if pv.Lang != "" {
			parts = append(parts, pv.Lang)
		}
		if pv.Total > 0 {
			parts = append(parts, itoa(pv.Total)+" "+plural("line", pv.Total))
		}
		if size := byteWord(int(pv.Key.Bytes)); size != "" {
			parts = append(parts, size)
		}
	}
	return strings.Join(parts, " · ")
}

// ── filling the region ──────────────────────────────────────────────────────

// previewPad fills the box out to its full height with blank rows, for a caller
// composing the preview beside columns that must stay the same height.
//
// It is separate from [previewRows] because the two callers genuinely differ: a
// sheet drawing one pane wants the region held open, and a caller stacking rows
// under each other wants to know where the preview ended.
func previewPad(rows []string, box previewBox) []string {
	if box.Height < 1 {
		return rows
	}
	if len(rows) > box.Height {
		return rows[:box.Height]
	}
	out := make([]string, 0, box.Height)
	out = append(out, rows...)
	for len(out) < box.Height {
		out = append(out, "")
	}
	return out
}
