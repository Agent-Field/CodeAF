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
//
// They say it on bare ground. This file used to draw a titled box-drawing
// rectangle around every region, which §16 BORDERS bans outright: the delivery
// card's ground plus its `▎` edge is the only "border" in the product, dialogs
// get the chrome ring (dialog.go), and everything else separates by whitespace
// and faint words. A skeleton is not exempt — a placeholder is the first thing
// anyone sees of a surface, and a frame drawn in the anti-catalog's own
// characters teaches the wrong grammar to every pane that lands after it. So
// the region's sentence floats in the middle of the space it was given and
// nothing is drawn around it: the whitespace IS the boundary, which is the
// same claim the real panes will make.
//
// SEAM — internal/tui2/tokens: the two vocabulary marks below are restated as
// literals rather than read off tokens, for the reason dialog.go's own SEAM
// note gives at length — this root package cannot import the tokens sibling
// without inverting the tokens → tui2 dependency. placeholder_tokens_test.go
// pins each restatement to its constant from outside the package, so a drift
// fails a test rather than shipping two marks for one meaning.
const (
	// separatorMark is tokens.GlyphSeparator: the one telemetry separator, so
	// the skeleton's status line is punctuated the way every real surface is.
	separatorMark = "·"
	// statusSep is the separator with its air, which is how it is always drawn.
	statusSep = " " + separatorMark + " "
	// missingMark is tokens.GlyphMissing: absent data renders as absence and
	// never as an invented default (§16 EMPTINESS, 10.2.8).
	missingMark = "—"
	// sizeJoin writes a rectangle as "120x30". It is ASCII on purpose: the
	// obvious `×` (U+00D7) is East_Asian_Width=Ambiguous, so a diagnostic about
	// how many cells a region has would itself measure two cells wider under a
	// CJK locale, and the vocabulary's only near-twin is tokens.GlyphFailed
	// (`✕`, U+2715), which means a broken row and nothing else.
	sizeJoin = "x"
)

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
		strconv.Itoa(w) + sizeJoin + strconv.Itoa(h) + " cells",
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
	return centred(id.String(), body, w, h)
}

// plain is the linear rendering of a region (10.1.5): a heading and its lines
// at the left edge, with no padding around them at all. Neither mode draws a
// frame any more — [centred] floats the same words on bare ground — so what
// this mode still drops is the CENTRING: leading spaces are a picture of a
// position, and a screen reader reads them as nothing useful before the first
// word. Same content, same order, same width discipline; less to listen to.
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

// centred is the ordinary rendering of an empty region: the region's own word
// and the lines under it, floated in the middle of the rectangle on nothing but
// ground. It fills the whole w×h allotment with spaces, because a pane owns the
// rectangle it was given and a sparse one would let the layer underneath show
// through its gaps — but every one of those cells is blank. Whitespace is the
// boundary (§16 BORDERS), and the empty-state idiom the real panes use is the
// same one: a quiet sentence in the middle of a room with nothing in it.
//
// The word comes first and it is lowercase, which is the single faint word §15
// allows a section when position alone is ambiguous — and a region that has not
// been built yet is exactly that case. Everything else is the diagnostic.
//
// A rectangle too short for the whole block keeps the top of it, so the word
// survives to the last row: at one column by one row the honest picture is a
// single character, and the surface still has to come up.
func centred(title string, body []string, w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	lines := make([]string, 0, len(body)+1)
	lines = append(lines, title)
	lines = append(lines, body...)
	if len(lines) > h {
		lines = lines[:h]
	}

	top := (h - len(lines)) / 2
	var out strings.Builder
	out.Grow((w + 1) * h)
	blank := strings.Repeat(" ", w)
	for row := 0; row < h; row++ {
		if row > 0 {
			out.WriteByte('\n')
		}
		i := row - top
		if i < 0 || i >= len(lines) {
			out.WriteString(blank)
			continue
		}
		line := ansi.Truncate(lines[i], w, "")
		lead := (w - ansi.StringWidth(line)) / 2
		out.WriteString(blank[:lead])
		out.WriteString(line)
		out.WriteString(blank[:w-lead-ansi.StringWidth(line)])
	}
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
			out.WriteString(statusSep)
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

	hit := "hit " + missingMark
	if s.hitOK {
		hit = "hit " + s.hitID.String() + " " + strconv.Itoa(s.hitAt.X) + "," + strconv.Itoa(s.hitAt.Y)
	}

	caps := "kbd " + boolWord(s.caps.Disambiguation) +
		statusSep + "sync " + s.caps.SyncOutput.String() +
		statusSep + "color " + s.caps.Color.String()
	if s.caps.Verified {
		caps += "!"
	}

	cols := []column{
		{text: "aforge v2", rank: 0},
		{text: shape, rank: 1},
		{text: strconv.Itoa(s.width) + sizeJoin + strconv.Itoa(s.height), rank: 2},
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

// dash renders missing data as [missingMark] rather than as an empty space or
// an invented default (10.2, honesty marks).
func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return missingMark
	}
	return s
}
