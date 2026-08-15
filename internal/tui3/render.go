package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
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
	hitNone hitKind = iota
	hitTool         // a tool call: click expands that call inline
	hitFold         // the "N earlier tool calls" line: click expands the turn
	hitMore         // the "… N more lines" foot of a capped expansion: click lifts the cap
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
}

// toolWindow is how many of a turn's tool calls stay on screen. Three is the
// number a person can hold without reading: the call that is running and the
// two it followed.
const toolWindow = 3

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
//
// Two rules can ask for the same gap — a cluster ending a turn, then the next
// user message — and a gap asked for twice is still one gap, which is why each
// block asks once, before it draws. Nothing is ever emitted at the top of the
// transcript.
func (a *app) layout(width int) []row {
	out := make([]row, 0, len(a.entries)+8)
	// wasCluster says the block that just drew was a tool cluster, and is the
	// whole of the state this pass carries.
	wasCluster := false
	gap := func() {
		if len(out) > 0 {
			out = append(out, row{entry: -1})
		}
	}
	for i := 0; i < len(a.entries); i++ {
		e := &a.entries[i]

		// A run of tool entries from one turn is a cluster, and a cluster is
		// laid out as a unit: it is the thing that folds.
		if e.kind == entryTool {
			end := i + 1
			for end < len(a.entries) &&
				a.entries[end].kind == entryTool &&
				a.entries[end].turn == e.turn {
				end++
			}
			if !wasCluster && !a.opensTurn(i) {
				gap()
			}
			out = a.clusterRows(out, i, end, width)
			wasCluster = true
			i = end - 1
			continue
		}

		rows := a.entryRows(i, width)
		if len(rows) == 0 {
			continue
		}
		if wasCluster || e.kind == entryUser {
			gap()
		}
		for _, text := range rows {
			out = append(out, row{text: text, entry: i})
		}
		wasCluster = false
	}
	if line, ok := a.ellipsis(); ok {
		if wasCluster {
			gap()
		}
		out = append(out, row{text: line, entry: -1})
	}
	return out
}

// opensTurn reports whether the entry at i is the first thing its turn drew.
// A cluster that opens a turn follows the person's own message and takes no
// blank of its own — the user message already brought one.
func (a *app) opensTurn(i int) bool {
	for at := i - 1; at >= 0; at-- {
		if a.entries[at].turn != a.entries[i].turn {
			return true
		}
		if a.entries[at].kind != entryUser {
			return false
		}
	}
	return true
}

// entryRows is the per-entry cache for everything that is not a tool call.
//
// Tool lines never come through here — [app.clusterRows] draws them, because
// their marker depends on their position in the cluster — and they are
// deliberately NOT cached: one of them is animating, all of them are one line
// plus a bounded expansion, and a cache with an animation in it is a cache that
// has to be invalidated thirty times a second, which is not a cache but a bug
// with a field.
func (a *app) entryRows(i, width int) []string {
	e := &a.entries[i]
	if e.built && e.width == width && !e.stale {
		return e.rows
	}
	e.rows = a.renderEntry(e, width)
	e.width, e.built, e.stale = width, true, false
	return e.rows
}

// renderEntry paints one block. Nothing here appends a blank row — see
// [app.layout].
func (a *app) renderEntry(e *entry, width int) []string {
	switch e.kind {
	case entryUser:
		// The person's own words: the glyph, then the text in bold, and every
		// continuation line aligned under the TEXT rather than under the
		// glyph. The glyph marks the turn; the column belongs to the sentence.
		body := wrap(e.text, width-2)
		out := make([]string, 0, len(body))
		for i, line := range body {
			lead := "  "
			if i == 0 {
				lead = a.pal.accent(glyphYou)
			}
			out = append(out, lead+a.pal.bold(a.pal.ink(line)))
		}
		return out

	case entryAssistant:
		return a.assistantRows(e, width)

	case entryThinking:
		return a.thoughtRows(e, width)

	case entryDivider:
		return []string{a.divider(e.text, width)}

	case entryNote:
		body := wrap(e.text, width-2)
		out := make([]string, 0, len(body))
		for i, line := range body {
			lead := "· "
			if i > 0 {
				lead = "  "
			}
			out = append(out, a.pal.dim(lead+line))
		}
		return out
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
func (a *app) assistantRows(e *entry, width int) []string {
	if e.settled {
		return trimBlanks(renderMarkdown(e.text, width))
	}
	var out []string
	if e.mdCut > 0 {
		out = append(out, renderMarkdown(e.text[:e.mdCut], width)...)
	}
	out = append(out, wrap(e.text[e.mdCut:], width)...)
	return trimBlanks(out)
}

// ellipsis is the sign of life while a turn runs and nothing else on screen is
// moving. It is suppressed while text is actively streaming, and suppressed
// while any call is spinning: the text and the spinner each already answer "is
// this alive?", and two answers to one question is one too many.
func (a *app) ellipsis() (string, bool) {
	if a.state != stateWorking || a.running() {
		return "", false
	}
	if a.live >= 0 && a.live < len(a.entries) && a.entries[a.live].text != "" && !a.quiet() {
		return "", false
	}
	return a.pal.accent("  " + ellipsisFrames[(a.paints/pulseStep)%len(ellipsisFrames)]), true
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

// status is the one line above everything: who we are, what model, where, what
// it has cost, and what is happening right now.
func (a *app) status(width int) string {
	state := a.state.String()
	painted := a.pal.dim(state)
	switch a.state {
	case stateWorking:
		painted = a.pal.accent(state)
	case stateInterrupted:
		painted = a.pal.bad(state)
	}
	sep := a.pal.dim(" · ")
	line := a.pal.dim("aforge")
	// The session's own name for this conversation, left of the model: it is
	// the most specific thing on the line — which conversation this is, rather
	// than what is answering it — and it is dim like everything else the surface
	// says about itself. Absent until the session has named itself (title.go).
	if a.title != "" {
		line += sep + a.pal.dim(a.title)
	}
	line += sep + a.pal.dim(a.model) + sep + a.pal.dim(a.place) + sep +
		a.pal.dim(dollars(a.cost))
	// The meter sits beside the cost because they are the same kind of fact —
	// what this conversation has spent, and what it is carrying. Dim, like
	// every other thing the surface says about itself.
	if pct, ok := a.ctxPercent(); ok {
		line += sep + a.pal.dim(itoa(pct)+"% ctx")
	}
	line += sep + painted
	if ansi.StringWidth(line) > width {
		line = ansi.Truncate(line, width, "")
	}
	return line
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

// fit truncates to a printable width, or returns nothing at all when there is
// no room — a one-cell ellipsis in a one-cell gap says less than a space.
func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, glyphMore)
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
