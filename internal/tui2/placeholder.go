package tui2

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// What the skeleton draws where the real surface will be.
//
// A placeholder that says "TODO" teaches nothing and hides everything. These
// say what region they are, how many cells they were given, and which package
// is going to fill them — so booting the v2 shell at a strange width is a
// diagnostic rather than a guess, and so a golden frame taken this wave still
// means something when the panes underneath it are real.

// placeholderNote names the sibling that will own each region.
var placeholderNote = [numLayers]string{
	LayerTranscript: "blocks: committed-prefix message parts",
	LayerRail:       "blocks: scope map — one scope, one cursor",
	LayerComposer:   "blocks: draft, attachments, meta strip",
	LayerStatus:     "columns drop by priority, never wrap",
	LayerOverlay:    "wave 3: dialogs, palette, ? surfaces",
}

// placeholder renders one empty region.
func (s *Shell) placeholder(id LayerID, w, h int) string {
	if id == LayerStatus {
		return s.statusLine(w)
	}
	body := []string{
		strconv.Itoa(w) + "×" + strconv.Itoa(h) + " cells",
		placeholderNote[id],
	}
	if id == s.focus {
		body = append(body, "focused")
	}
	if id == LayerTranscript {
		// The plumbing this surface was handed, said plainly. It is not opened
		// yet, and a shell that displayed a session it had not read would be
		// the first lie in a package built to stop telling them.
		body = append(body, "", "db "+dash(s.db), "session "+dash(s.session), "(not opened this wave)")
	}
	if s.linear {
		return plain(id.String(), body, w, h)
	}
	return box(id.String(), body, w, h)
}

// plain is the linear rendering of a region (10.1.5): a heading and its lines,
// with no drawn frame at all. A box is a picture of a boundary, and a screen
// reader reads it as a hundred and forty punctuation marks before the first
// word — so the accessible mode does not draw one. Same content, same order,
// same width discipline; less to listen to.
func plain(title string, body []string, w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	lines := make([]string, 0, len(body)+1)
	lines = append(lines, title)
	lines = append(lines, body...)
	if len(lines) > h {
		lines = lines[:h]
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], w, "")
	}
	return strings.Join(lines, "\n")
}

// box draws a titled frame filled to exactly w×h cells. Below the size a frame
// needs it degrades to bare clipped text rather than drawing a corner with
// nothing inside it — at one column by one row the honest picture is a single
// character, and the surface still has to come up.
func box(title string, body []string, w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	if w < 4 || h < 3 {
		lines := make([]string, 0, h)
		lines = append(lines, title)
		lines = append(lines, body...)
		for i := range lines {
			lines[i] = ansi.Truncate(lines[i], w, "")
		}
		if len(lines) > h {
			lines = lines[:h]
		}
		return strings.Join(lines, "\n")
	}

	inner := w - 2
	var out strings.Builder
	out.Grow(w * h)

	head := "─ " + title + " "
	if ansi.StringWidth(head) > inner {
		head = ansi.Truncate(head, inner, "")
	}
	out.WriteString("┌")
	out.WriteString(head)
	out.WriteString(strings.Repeat("─", inner-ansi.StringWidth(head)))
	out.WriteString("┐")

	for row := 0; row < h-2; row++ {
		out.WriteString("\n│")
		line := ""
		if row < len(body) {
			line = " " + body[row]
		}
		line = ansi.Truncate(line, inner, "")
		out.WriteString(line)
		out.WriteString(strings.Repeat(" ", inner-ansi.StringWidth(line)))
		out.WriteString("│")
	}

	out.WriteString("\n└")
	out.WriteString(strings.Repeat("─", inner))
	out.WriteString("┘")
	return out.String()
}

// column is one field of the status line. Rank is a drop order, not an
// importance score: the highest rank still shown is the next one to go.
type column struct {
	text string
	rank int
}

// composeStatus composes the footer and shortens it by dropping columns, never by
// wrapping (10.5.22). The wordmark is rank 0 and therefore the last thing left
// — a single truncated word is still a surface saying which surface it is.
func composeStatus(cols []column, w int) string {
	if w <= 0 || len(cols) == 0 {
		return ""
	}
	shown := make([]bool, len(cols))
	for i := range shown {
		shown[i] = true
	}
	for {
		line := joinColumns(cols, shown)
		if ansi.StringWidth(line) <= w {
			return line
		}
		worst := -1
		for i := range cols {
			if shown[i] && (worst < 0 || cols[i].rank > cols[worst].rank) {
				worst = i
			}
		}
		if worst < 0 || cols[worst].rank == 0 {
			// Everything droppable is gone and it still does not fit. Clip
			// what remains: a shortened truth beats an empty line.
			return ansi.Truncate(line, w, "")
		}
		shown[worst] = false
	}
}

func joinColumns(cols []column, shown []bool) string {
	var out strings.Builder
	for i := range cols {
		if !shown[i] || cols[i].text == "" {
			continue
		}
		if out.Len() > 0 {
			out.WriteString(" · ")
		}
		out.WriteString(cols[i].text)
	}
	return out.String()
}

// statusLine for this shell: the facts a Wave 2 operator needs and no others.
// Health lives here; this-turn cost and context belong on the composer's meta
// strip when it exists (10.5.23), and the two homes never mix.
func (s *Shell) statusLine(w int) string {
	shape := "wide"
	switch {
	case s.linear:
		shape = "linear"
	case s.layout.Narrow:
		shape = "narrow"
	}

	hit := "hit —"
	if s.hitOK {
		hit = "hit " + s.hitID.String() + " " + strconv.Itoa(s.hitAt.X) + "," + strconv.Itoa(s.hitAt.Y)
	}

	caps := "kbd " + boolWord(s.caps.Disambiguation) +
		" · sync " + s.caps.SyncOutput.String() +
		" · color " + s.caps.Color.String()
	if s.caps.Verified {
		caps += "!"
	}

	cols := []column{
		{text: "aforge v2", rank: 0},
		{text: shape, rank: 1},
		{text: strconv.Itoa(s.width) + "×" + strconv.Itoa(s.height), rank: 2},
		{text: "focus " + s.focus.String(), rank: 3},
		{text: "newline " + s.caps.NewlineKey(), rank: 5},
		{text: caps, rank: 4},
		{text: hit, rank: 6},
	}
	return composeStatus(cols, w)
}

func boolWord(v bool) string {
	if v {
		return "yes"
	}
	return "?"
}

// dash renders missing data as an em dash rather than as an empty space or an
// invented default (10.2, honesty marks).
func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
