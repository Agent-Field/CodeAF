package consentui

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The drawing.
//
// A dialog is a calm block of rows separated by whitespace: no border, no box
// inside a box, no second header grammar (5.13). The only colour is amber on
// the `?` and the waiting count, because amber means a human is needed and this
// is the surface that means that (5.16). Everything else is the three-tier grey
// ramp, and the selection is a background band rather than a foreground colour.
//
// Height is a budget, not an assumption. Every section declares how far it may
// be trimmed and in what order, so a dialog in a six-row frame still shows the
// question and its answers instead of panicking or spilling. Trimming a section
// always leaves a visible mark: a reader must never be shown a silently
// shortened consent question (12.5).

// gutter is the one column of air down each side. 5.13: cards are separated by
// whitespace, not boxes, and that applies to a dialog's edges too.
const gutter = 1

// Render implements tui2.Pane.
func (m *Model) Render(width, height int) string {
	if width < 1 || height < 1 {
		return ""
	}
	rows := m.compose(width, height)
	if len(rows) > height {
		rows = rows[:height]
	}
	return strings.Join(rows, "\n")
}

// section is one run of rows with its own survival rules.
type section struct {
	rows []string
	// min is how many rows survive the tightest frame. Zero means the section
	// may vanish entirely.
	min int
	// order is the trim order: lower goes first.
	order int
}

// trim orders, low goes first. Air before detail, detail before the key strip,
// the strip before the words, and the answers last — a dialog reduced to two
// rows should be the question and what you can do about it.
const (
	trimAir = iota
	trimDetail
	trimStrip
	trimConsequence
	trimPrompt
	trimOptions
)

func (m *Model) compose(width, height int) []string {
	inner := width - 2*gutter
	pad := strings.Repeat(" ", gutter)
	if inner < 1 {
		inner, pad = width, ""
	}
	forced := ForcedFullscreen(width, height)
	full := m.full || forced

	q, ok := m.Current()
	if !ok {
		return nil
	}

	// The selected answer's hint is the first thing a tight frame gives up, and
	// it is given up WHOLE rather than trimmed to its first line: half a
	// sentence explaining what "hold" means is worse than no sentence, and the
	// hint is the only part of this dialog whose absence costs nothing legible.
	// So the frame is assembled with hints and, if that does not fit, once more
	// without them — which is cheaper than teaching the fitter to reach inside a
	// section and pull out alternating rows.
	sections := m.assemble(q, inner, full, forced, true)
	if rowCount(sections) > height {
		sections = m.assemble(q, inner, full, forced, false)
	}
	return indent(fit(sections, height), pad, width)
}

func (m *Model) assemble(q Question, inner int, full, forced, hints bool) []section {
	sections := []section{{rows: []string{m.titleRow(q, inner)}, min: 1, order: trimOptions}}
	air := func() { sections = append(sections, section{rows: []string{""}, order: trimAir}) }

	switch m.mode {
	case modeScope, modeScopeEdit:
		air()
		sections = append(sections, m.scopeSections(inner, full)...)
	case modeSteer:
		air()
		sections = append(sections, m.steerSections(inner)...)
	default:
		air()
		sections = append(sections, section{
			rows:  wrapRows(m.tintAll(q.Prompt, inner, tokens.TextPrimary)),
			min:   1,
			order: trimPrompt,
		})
		if q.Consequence != "" {
			air()
			sections = append(sections, section{
				rows:  wrapRows(m.tintAll(q.Consequence, inner, tokens.TextSecondary)),
				min:   1,
				order: trimConsequence,
			})
		}
		if m.detail && q.HasDetail() {
			air()
			sections = append(sections, section{rows: m.detailRows(q, inner, full), order: trimDetail})
		}
		air()
		sections = append(sections, section{rows: m.optionRows(q, inner, hints), min: 1, order: trimOptions})
	}
	air()
	return append(sections, section{rows: []string{m.stripRow(q, inner, full, forced)}, order: trimStrip})
}

func rowCount(sections []section) int {
	total := 0
	for i := range sections {
		total += len(sections[i].rows)
	}
	return total
}

// -- rows ---------------------------------------------------------------------

// titleRow is the chrome above the question: the amber `?` that means a human
// is needed, the word for what kind of decision this is, and the waiting count.
//
// The count is amber too, and it is the same number the footer's attention
// column carries (10.5.22) — one fact, two places, never two arithmetics.
func (m *Model) titleRow(q Question, width int) string {
	segments := []segment{
		{tokens.GlyphNeedsHuman + " ", tokens.Amber},
		{kindWord(q), tokens.TextSecondary},
	}
	if n := m.Pending(); n > 1 {
		segments = append(segments, segment{
			" " + tokens.GlyphSeparator + " " + strconv.Itoa(n) + " waiting", tokens.Amber})
	}
	if word := modeWord(m.mode); word != "" {
		segments = append(segments, segment{" " + tokens.GlyphSeparator + " " + word, tokens.TextTertiary})
	}
	return m.paintSegments(segments, width)
}

// segment is one painted run of a composed row.
type segment struct {
	text  string
	token tokens.Token
}

// paintSegments lays runs out left to right inside a width budget, truncating
// the run that crosses the edge and dropping the rest. Painting happens AFTER
// the measurement, so no escape sequence is ever counted as a cell and no row
// can leave the frame it was given.
func (m *Model) paintSegments(segments []segment, width int) string {
	if width < 1 {
		return ""
	}
	var b strings.Builder
	left := width
	for _, seg := range segments {
		if left <= 0 || seg.text == "" {
			break
		}
		text := seg.text
		if blocks.Width(text) > left {
			text = blocks.Truncate(text, left)
		}
		if text == "" {
			break
		}
		b.WriteString(m.tint(text, seg.token))
		left -= blocks.Width(text)
	}
	return b.String()
}

// kindWord names the decision. The conservative default is the store's (9.4,
// 12.1.4): anything that is not explicitly informational reads as consent, so
// an unlabeled question is never drawn as the lighter thing.
func kindWord(q Question) string {
	if q.Class == store.QuestionInformational {
		return "question"
	}
	return "consent"
}

func modeWord(m mode) string {
	switch m {
	case modeScope:
		return "scope"
	case modeScopeEdit:
		return "editing scope"
	case modeSteer:
		return "redirect"
	default:
		return ""
	}
}

// optionRows draws the answers: a letter, the durable label, and the durable
// hint under the selected one.
//
// The label is never rewritten. What a question offered is what the dialog
// offers, and the letter in front of it came out of that same label (10.4.17),
// so the strip and the keyboard cannot disagree.
func (m *Model) optionRows(q Question, width int, hints bool) []string {
	rows := make([]string, 0, len(q.Options)*2)
	for i, option := range q.Options {
		selected := i == m.sel
		key := option.Key
		if key == "" {
			key = strconv.Itoa(option.Index)
		}
		body := key + "  " + option.Label
		if selected && m.bandable() {
			rows = append(rows, m.band(blocks.Pad(blocks.Truncate("  "+body, width), width)))
		} else if selected {
			rows = append(rows, m.tint(blocks.Truncate(
				tokens.GlyphAccentRail+" "+body, width), tokens.TextPrimary))
		} else {
			// The letter is an interactive chip at rest, which the palette puts
			// on tier 3 — NOT amber. Amber means a human is needed, and a column
			// of amber letters would spend the one word this surface most needs
			// to keep sharp (5.16).
			lead := "  " + key
			rows = append(rows, m.tint(blocks.Truncate(lead, width), tokens.TextTertiary)+
				m.tint(blocks.Truncate("  "+option.Label, width-blocks.Width(lead)), tokens.TextPrimary))
		}
		if !hints || !selected || option.Hint == "" {
			continue
		}
		for _, line := range wrapPlain(option.Hint, width-5) {
			rows = append(rows, m.tint(blocks.Truncate("     "+line, width), tokens.TextTertiary))
		}
	}
	return rows
}

// detailRows is the `t` view (10.4.17): the diff-shaped thing being consented
// to, in the add/remove vocabulary of 5.17 and the two hues those mean.
func (m *Model) detailRows(q Question, width int, full bool) []string {
	// Eight rows is the preview the breakpoints table sized the floating dialog
	// for; the fullscreen escalation is where a long diff is actually read.
	limit := 8
	if full {
		limit = 40
	}
	rows := make([]string, 0, limit+1)
	if q.DetailTitle != "" {
		rows = append(rows, m.tint(blocks.Truncate(q.DetailTitle, width), tokens.TextSecondary))
	}
	for i, line := range q.Detail {
		if i >= limit {
			rows = append(rows, m.tint(blocks.Truncate(
				tokens.GlyphTruncated+" +"+strconv.Itoa(len(q.Detail)-limit)+" more", width), tokens.TextTertiary))
			break
		}
		glyph, tone := " ", tokens.TextSecondary
		switch line.Kind {
		case DetailAdd:
			glyph, tone = tokens.GlyphDiffAdd, tokens.Green
		case DetailDel:
			glyph, tone = tokens.GlyphDiffDel, tokens.Coral
		}
		rows = append(rows, m.tint(blocks.Truncate(glyph+" "+line.Text, width), tone))
	}
	return rows
}

// scopeSections is 10.4.18's first half: before an "always" grant is taken, the
// exact patterns it would whitelist are listed. Not summarized, not counted —
// listed, because a grant nobody read is a grant nobody gave.
func (m *Model) scopeSections(width int, full bool) []section {
	label := m.pending.Label
	lead := "\"" + label + "\" would whitelist:"
	if len(m.scope) == 0 {
		lead = "\"" + label + "\" would whitelist nothing."
	}
	out := []section{{
		rows:  wrapRows(m.tintAll(lead, width, tokens.TextSecondary)),
		min:   1,
		order: trimPrompt,
	}, {rows: []string{""}, order: trimAir}}

	rows := make([]string, 0, len(m.scope)+2)
	for i, pattern := range m.scope {
		selected := i == m.scopeSel
		if m.mode == modeScopeEdit && selected {
			rows = append(rows, m.tint(blocks.Truncate("  "+m.scopeEdit.caretRow(width-2), width), tokens.TextPrimary))
			continue
		}
		switch {
		case selected && m.bandable():
			rows = append(rows, m.band(blocks.Pad(blocks.Truncate("  "+pattern, width), width)))
		case selected:
			rows = append(rows, m.tint(blocks.Truncate(
				tokens.GlyphAccentRail+" "+pattern, width), tokens.TextPrimary))
		default:
			rows = append(rows, m.tint(blocks.Truncate("  "+pattern, width), tokens.TextSecondary))
		}
	}
	out = append(out, section{rows: rows, min: 1, order: trimOptions})
	if m.mode == modeScope && !full {
		out = append(out,
			section{rows: []string{""}, order: trimAir},
			section{rows: []string{m.tint(blocks.Truncate(
				"editing a pattern opens the full frame", width), tokens.TextTertiary)}, order: trimStrip})
	}
	return out
}

// steerSections is 10.4.16: a rejection is a redirect. What is typed here is
// what the work does next, so the row that says which answer is being given
// stays on screen above it — a redirect typed against the wrong "no" would be
// the worst possible outcome of a consent dialog.
func (m *Model) steerSections(width int) []section {
	lead := "declining: " + m.pending.Label
	prompt := m.tint(tokens.GlyphPromptSteer+" ", tokens.Amber) +
		m.tint(m.steer.caretRow(width-2), tokens.TextPrimary)
	return []section{
		{rows: wrapRows(m.tintAll(lead, width, tokens.TextSecondary)), min: 1, order: trimPrompt},
		{rows: []string{""}, order: trimAir},
		{rows: wrapRows(m.tintAll("tell it what to do differently", width, tokens.TextTertiary)), order: trimConsequence},
		{rows: []string{""}, order: trimAir},
		{rows: []string{prompt}, min: 1, order: trimOptions},
	}
}

// stripRow is the contextual key line. It advertises exactly what the current
// mode does, esc included — 5.20 rule 6: an escape that is not advertised does
// not exist, and 5.22: no action of this dialog is typed-only.
func (m *Model) stripRow(q Question, width int, full, forced bool) string {
	var cells []string
	switch m.mode {
	case modeScope:
		cells = []string{"enter allow", "e edit", "esc back"}
	case modeScopeEdit:
		cells = []string{"enter keep", "esc discard"}
	case modeSteer:
		cells = []string{"enter send", "esc back"}
	default:
		// "tab move" rather than an arrow pair: 5.17 bans width-unstable chrome
		// and ↑↓ are East-Asian-Ambiguous, so they are exactly the ghosting the
		// glyph table exists to avoid. The arrow keys are bound all the same.
		cells = []string{"enter answer", "tab move"}
		if q.HasDetail() {
			word := "t detail"
			if m.detail {
				word = "t hide"
			}
			cells = append(cells, word)
		}
		// `f` is offered only where it is a real choice. Below the threshold the
		// frame is already the dialog, and advertising a key that cannot change
		// anything is the capability dishonesty of 5.20 rule 3.
		if !forced {
			word := "f full"
			if full {
				word = "f panel"
			}
			cells = append(cells, word)
		}
		cells = append(cells, "esc later")
	}
	line := strings.Join(cells, " "+tokens.GlyphSeparator+" ")
	for len(cells) > 1 && blocks.Width(line) > width {
		cells = cells[:len(cells)-1]
		line = strings.Join(cells, " "+tokens.GlyphSeparator+" ")
	}
	return m.tint(blocks.Truncate(line, width), tokens.TextTertiary)
}

// -- fitting ------------------------------------------------------------------

// fit spends the height budget. Sections are trimmed in declared order, and a
// trimmed section always says so on its last row, so nothing is quietly cut.
func fit(sections []section, height int) []string {
	total := 0
	for i := range sections {
		total += len(sections[i].rows)
	}
	for order := trimAir; total > height && order <= trimOptions; order++ {
		for i := range sections {
			if sections[i].order != order || total <= height {
				continue
			}
			over := total - height
			keep := len(sections[i].rows) - over
			if keep < sections[i].min {
				keep = sections[i].min
			}
			if keep >= len(sections[i].rows) {
				continue
			}
			total -= len(sections[i].rows) - keep
			sections[i].rows = truncateRows(sections[i].rows, keep)
		}
	}
	out := make([]string, 0, max(0, min(total, height)))
	for i := range sections {
		out = append(out, sections[i].rows...)
	}
	if len(out) > height {
		out = out[:height]
	}
	return out
}

// truncateRows keeps n rows and marks the cut, because a consent question that
// was shortened must say so (12.5) — the reader has to know there is more of the
// thing they are about to agree to.
//
// The mark costs the last kept row rather than an extra one: there is by
// definition no extra row here. The exception is n == 1, where spending the only
// row on a marker would leave a dialog that shows a count instead of a question;
// there the mark rides on the end of the single surviving row.
func truncateRows(rows []string, n int) []string {
	if n <= 0 {
		return rows[:0]
	}
	if n >= len(rows) {
		return rows
	}
	if n == 1 {
		return []string{rows[0] + " " + tokens.GlyphTruncated}
	}
	cut := rows[:n]
	cut[n-1] = tokens.GlyphTruncated + " +" + strconv.Itoa(len(rows)-n+1)
	return cut
}

// indent pads each row into the gutter and clips it to the frame.
//
// The clip is the last line of defence and it is unconditional: every row above
// measures its own content, but the pane contract says a render returns rows of
// at most w cells, and "at most" is a promise this function keeps rather than an
// invariant scattered across a dozen builders. The compositor truncates too; a
// pane that leaned on that would be a pane whose arithmetic is never tested.
func indent(rows []string, pad string, width int) []string {
	for i := range rows {
		if rows[i] == "" {
			continue
		}
		row := pad + rows[i]
		if blocks.Width(row) > width {
			row = blocks.Truncate(row, width)
		}
		rows[i] = row
	}
	return rows
}

// -- painting -----------------------------------------------------------------

func (m *Model) tint(text string, token tokens.Token) string {
	if m.style == nil || text == "" {
		return text
	}
	return m.style.PaintToken(text, token)
}

// bandable reports whether the selection may be drawn as a background band
// (5.16) rather than as the accent-rail marker of 5.21.
//
// Four things forbid the band, and each is a real state rather than a
// defensive check:
//
//   - linear mode (10.1.5), where the selection must be a character a reader
//     can hear, not a colour it must see;
//   - an unfocused pane, because tokens.Legal forbids a dimmed foreground on a
//     raised ground and names the accent rail as the substitute;
//   - a NoColor profile, where PaintOn is the identity function — a band drawn
//     there would be a selection made of invisible spaces, which is the exact
//     failure a fallback exists to prevent;
//   - no styler at all, which is the same case one step earlier.
func (m *Model) bandable() bool {
	return !m.linear && m.focused && m.style != nil && m.style.Profile() != tokens.NoColor
}

// band is the selection idiom of 5.16: a raised ground, the text keeping its
// own tier colour. Callers gate on [Model.bandable] first.
func (m *Model) band(text string) string {
	if m.style == nil {
		return text
	}
	return m.style.PaintOn(text, tokens.TextPrimary, tokens.Band)
}

// tintAll wraps plain text and paints each row, which keeps every escape
// sequence on the row it belongs to — a painted string spanning a line break
// would leak its colour into the next row on a terminal that clips.
func (m *Model) tintAll(text string, width int, token tokens.Token) []string {
	lines := wrapPlain(text, width)
	for i := range lines {
		lines[i] = m.tint(lines[i], token)
	}
	return lines
}

func wrapPlain(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	rows, _ := blocks.Wrap(nil, text, width)
	return rows
}

// wrapRows guarantees at least one row, so a section declaring min 1 can always
// honour it.
func wrapRows(rows []string) []string {
	if len(rows) == 0 {
		return []string{""}
	}
	return rows
}
