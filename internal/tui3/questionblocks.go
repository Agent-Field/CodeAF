package tui3

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

// THE EVIDENCE AN ASKER ATTACHES, DRAWN AT THE SIZE IT NEEDS.
//
// docs/design/questions/DESIGN.md, rung five of the ladder: "show outcomes —
// attach what each answer produces (a diff, a rendered row, a layout)". A
// question with a diff under each answer is a question a person answers in four
// seconds; the same question with the diffs described in prose is one they
// answer by asking to see the diffs.
//
// SIX KINDS AND NO SEVENTH ([session.BlockKind]). Each one is drawn by the rule
// its content already carries and never by a rule invented here:
//
//   - text — prose, wrapped to the block's own indent.
//   - diagram — PRE-FORMATTED and drawn as-is. Its lines mean what they are; a
//     wrap would destroy it, so a diagram wider than the page is cut at the edge
//     with the ellipsis every other cut line on this surface wears.
//   - table — [session.Block.Rows], first row the header, columns sized from the
//     content and cut evenly when the page is narrow.
//   - diff — the diff glyphs and tokens ([tokens.GDiffAdd]/[tokens.GDiffDel],
//     [palette.add]/[palette.del]), which is toolview.go's own painting said
//     again here because a diff must look the same wherever this program draws
//     one.
//   - image — the surface's existing picture path ([app.pictureRowsFor]), which
//     is the same renderer, cache and half-cell painting a picture in the
//     conversation gets. A terminal that cannot paint gets the path and `o open`.
//   - layout — two pre-formatted panes side by side above 100 columns, stacked
//     below, which is DESIGN.md's own number.
//
// ── NO BOX DRAWING ──
//
// DESIGN.md's hue law: "no box drawing except the room's rule lines and blocks
// the asker drew". So a block is a dim title and its content indented under it,
// and the only lines on the page are the ones inside a diagram the asker drew
// themselves. A frame around every block would put four rows of furniture around
// three rows of evidence on a question with four answers.

const (
	// questionBlockRows is how tall one block may be before it is cut. It is the
	// same instinct the tool expansion's budget is: evidence is worth a screenful
	// and no more, because past that it is the page rather than the evidence.
	questionBlockRowCap = 24
	// questionImageRows is the picture budget, and it is smaller for the same
	// reason: several answers may each carry one.
	questionImageRows = 10
	// questionOpenWord is what a terminal that cannot paint a picture is offered
	// instead. The path is never cut — a path with an ellipsis in it is a path
	// nobody can open, which is imagepreview.go's own stated rule.
	questionOpenWord = " · o open"
	// questionMoreWord is the foot on a block that was cut.
	questionMoreWord = " more lines"
)

// questionBlockRows draws one block at an indent.
func (a *app) questionBlockRows(block session.Block, indent, width int) []string {
	pad := strings.Repeat(" ", indent)
	inner := max(1, width-indent)
	out := make([]string, 0, 8)
	if title := strings.TrimSpace(block.Title); title != "" {
		out = append(out, pad+a.pal.dim(fit(title, inner)))
	}
	var body []string
	switch block.Kind {
	case session.BlockDiagram:
		body = a.questionDiagramLines(block, inner)
	case session.BlockTable:
		body = a.questionTableLines(block, inner)
	case session.BlockDiff:
		body = a.questionDiffLines(block, inner)
	case session.BlockImage:
		body = a.questionImageLines(block, inner)
	case session.BlockLayout:
		body = a.questionLayoutLines(block, inner, width)
	default:
		for _, line := range wrap(strings.TrimSpace(block.Body), inner) {
			body = append(body, a.pal.ink(line))
		}
	}
	cut := 0
	if len(body) > questionBlockRowCap {
		cut, body = len(body)-questionBlockRowCap, body[:questionBlockRowCap]
	}
	for _, line := range body {
		out = append(out, pad+line)
	}
	if cut > 0 {
		out = append(out, pad+a.pal.dim(a.icon(tokens.GEllipsis)+" "+itoa(cut)+questionMoreWord))
	}
	return out
}

// questionDiagramLines is a diagram exactly as the asker drew it. Nothing is
// wrapped, nothing is re-spaced, and a line wider than the page is cut at the
// edge — a diagram whose lines were reflowed is not a diagram.
func (a *app) questionDiagramLines(block session.Block, width int) []string {
	lines := strings.Split(strings.TrimRight(block.Body, "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, a.pal.ink(fit(expandTabs(line), width)))
	}
	return out
}

// questionTableLines lays [session.Block.Rows] out in columns, the first row
// being the header.
//
// THE COLUMNS ARE SIZED FROM THE CONTENT and then cut evenly, which is the one
// rule that keeps a table readable at any width: a column sized from its longest
// cell alone lets one runaway value eat the table, and a table of equal columns
// wastes half the page on a column of yeses and noes.
func (a *app) questionTableLines(block session.Block, width int) []string {
	if len(block.Rows) == 0 {
		return nil
	}
	columns := 0
	for _, row := range block.Rows {
		columns = max(columns, len(row))
	}
	if columns == 0 {
		return nil
	}
	widths := make([]int, columns)
	for _, row := range block.Rows {
		for i, cell := range row {
			widths[i] = max(widths[i], ansi.StringWidth(strings.TrimSpace(cell))+2)
		}
	}
	total := 0
	for _, w := range widths {
		total += w
	}
	if total > width {
		// Every column gives up the same SHARE, so the column that was widest is
		// still the widest afterwards and the shape of the table survives.
		for i := range widths {
			widths[i] = max(questionCellFloor, widths[i]*width/total)
		}
	}
	out := make([]string, 0, len(block.Rows))
	for at, row := range block.Rows {
		line := ""
		for i := range columns {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			text := questionCell(cell, widths[i])
			if at == 0 {
				line += a.pal.dim(text)
				continue
			}
			line += a.pal.ink(text)
		}
		out = append(out, fit(line, width))
	}
	return out
}

// questionDiffLines paints a pre-formatted diff with the diff glyphs and the
// diff tokens, which is what "diff glyphs and tokens" means: the MARK comes from
// the vocabulary and the hue from the ramp, so a diff in a question looks like a
// diff in a tool call because both are drawn from one table.
func (a *app) questionDiffLines(block session.Block, width int) []string {
	lines := strings.Split(strings.TrimRight(block.Body, "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = expandTabs(line)
		switch {
		case strings.HasPrefix(line, "+"):
			body := strings.TrimPrefix(line, "+")
			out = append(out, a.pal.add(fit(a.icon(tokens.GDiffAdd)+body, width)))
		case strings.HasPrefix(line, "-"), strings.HasPrefix(line, "−"):
			body := strings.TrimPrefix(strings.TrimPrefix(line, "-"), "−")
			out = append(out, a.pal.del(fit(a.icon(tokens.GDiffDel)+body, width)))
		default:
			out = append(out, a.pal.dim(fit(line, width)))
		}
	}
	return out
}

// questionImageLines draws the picture through the surface's own picture path,
// and says where it is when it cannot.
//
// IT IS THE SAME RENDERER AND NOT A SECOND ONE (imagepreview.go). A picture in a
// question and a picture in the conversation are one thing drawn twice, and the
// day this file grew its own decoder is the day the two would start disagreeing
// about which terminals can paint.
func (a *app) questionImageLines(block session.Block, width int) []string {
	path := strings.TrimSpace(block.Path)
	if path == "" {
		return nil
	}
	if rows, ok := a.pictureRowsFor(path, true, width, questionImageRows); ok {
		return rows
	}
	// A TERMINAL THAT CANNOT PAINT GETS THE PATH WHOLE AND A WAY IN. The path is
	// never cut for imagepreview.go's stated reason — a path with an ellipsis in
	// it is a path nobody can open — so it wraps rather than truncating, and the
	// key that opens it is named on the last row.
	out := make([]string, 0, 3)
	for _, segment := range wrap(path, width) {
		out = append(out, a.pal.dim(a.pathLink(path, segment)))
	}
	if len(out) > 0 {
		out[len(out)-1] += a.pal.ask(fit(questionOpenWord, max(0, width-ansi.StringWidth(path))))
	}
	return out
}

// questionPane fits one pre-formatted line to a column, padding on the right and
// trimming nothing: the spaces inside a drawing are the drawing.
func questionPane(text string, width int) string {
	if width <= 1 {
		return ""
	}
	text = fit(text, width-1)
	if pad := width - ansi.StringWidth(text); pad > 0 {
		return text + strings.Repeat(" ", pad)
	}
	return text
}

// questionLayoutLines draws two pre-formatted panes beside each other, or under
// each other on a page too narrow to hold both.
//
// THE PANES ARE [session.Block.Rows], ONE ENTRY PER PANE. It is the field the
// object already has for structured content, and a layout IS two lists of lines;
// carving them out of one Body with a separator would mean inventing a separator
// that a pane could contain.
func (a *app) questionLayoutLines(block session.Block, width, full int) []string {
	panes := block.Rows
	if len(panes) == 0 {
		return a.questionDiagramLines(block, width)
	}
	if len(panes) == 1 || full < questionLayoutFloor {
		// STACKED, WITH A BLANK BETWEEN THEM. DESIGN.md's own number: two panes
		// side by side above 100 columns and stacked below, because a pane cut to
		// thirty characters is a pane that has stopped being pre-formatted.
		out := make([]string, 0, 16)
		for i, pane := range panes {
			if i > 0 {
				out = append(out, "")
			}
			for _, line := range pane {
				out = append(out, a.pal.ink(fit(expandTabs(line), width)))
			}
		}
		return out
	}
	left, right := panes[0], panes[1]
	half := (width - 2) / 2
	tall := max(len(left), len(right))
	out := make([]string, 0, tall)
	for i := range tall {
		l, r := "", ""
		if i < len(left) {
			l = expandTabs(left[i])
		}
		if i < len(right) {
			r = expandTabs(right[i])
		}
		// THE PANES ARE PRE-FORMATTED, SO THE LEFT ONE IS PADDED AND NEVER
		// TRIMMED. [questionCell] trims — it is a table cell, where a stray space
		// is noise — and running a pane through it flattened every indent inside
		// the drawing the asker made.
		out = append(out, a.pal.ink(questionPane(l, half+1))+a.pal.ink(fit(r, half)))
	}
	return out
}
