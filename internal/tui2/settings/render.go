package settings

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The skin (5.16, 5.13, 5.17, 7.1).
//
// No boxes. There is no border, no frame and no panel inside a panel — a
// settings sheet is the surface most likely to grow one, and the whole
// hierarchy here is carried by the three grey tiers, one blank line, and the
// selection band. Colour says five things and nothing else: coral when a write
// was refused, amber on the row an environment variable has taken away from
// the user, and greys everywhere else. There is no theme picker on this sheet
// and there will not be one (10.6).
//
// The shape, top to bottom:
//
//	‹ settings                                        22 rows · 7 groups
//	models  money & limits  rhythm  learning  documents & vision  …
//
//	  daily budget          $25                                    saved
//	▎ ask before spending   $3                                   default
//	    when a planned job is estimated to cost more than this, aforge
//	    quotes the step count and the price and waits for your go-ahead.
//	    built-in default · enter edit
//	  practice budget       $0.50                  AFORGE_PRACTICE_BUDGET
//
//	type to search · ↑↓ move · ←→ tabs · enter edit · esc close
//
// The selected row carries the accent rail glyph AS WELL AS the band, because
// under a NoColor profile the band is not drawn at all and a selection nobody
// can see is not a selection (the same reasoning that makes a dimmed token
// never the only carrier of a fact).

// Render implements tui2.Pane. It returns at most height lines, each at most
// width cells, and it draws less rather than failing at every degenerate size.
func (m *Model) Render(width, height int) string {
	m.rowAtLine = m.rowAtLine[:0]
	if width < 1 || height < 1 {
		return ""
	}

	lines := make([]string, 0, height)
	rows := make([]int, 0, height)

	push := func(text string, position int) {
		if len(lines) >= height {
			return
		}
		lines = append(lines, text)
		rows = append(rows, position)
	}

	push(m.header(width), -1)
	if height >= 3 {
		push(m.tabBar(width), -1)
	}
	if height >= 8 {
		push("", -1)
	}

	hint := ""
	if height >= 5 {
		hint = m.hintLine(width)
	}
	body := height - len(lines)
	if hint != "" {
		body--
	}
	for _, line := range m.bodyLines(width, body, &rows) {
		lines = append(lines, line)
	}
	if hint != "" {
		for len(lines) < height-1 {
			lines = append(lines, "")
			rows = append(rows, -1)
		}
		lines = append(lines, hint)
		rows = append(rows, -1)
	}

	m.rowAtLine = append(m.rowAtLine, rows...)
	return strings.Join(lines, "\n")
}

// header is the scope line: where you are, and how much is here.
func (m *Model) header(width int) string {
	line := newLine(width)
	line.add(tokens.GlyphScopeUp+" ", tokens.TextTertiary)
	line.add("settings", tokens.TextPrimary)
	if m.query != "" {
		line.add(" "+tokens.GlyphSeparator+" ", tokens.TextTertiary)
		line.add(m.query, tokens.TextPrimary)
	}

	right := strconv.Itoa(len(m.rows)) + " rows " + tokens.GlyphSeparator + " " +
		strconv.Itoa(len(m.tabs)) + " groups"
	if m.query != "" {
		right = strconv.Itoa(len(m.visible)) + " of " + strconv.Itoa(len(m.rows))
	}
	line.right(right, tokens.TextTertiary)
	return line.render(m.styler, false)
}

// tabBar is the groups — or, while a search is running, the filter breadcrumb
// naming which groups the results came from (8.2.19).
func (m *Model) tabBar(width int) string {
	line := newLine(width)
	if m.query != "" {
		crumbs := m.filterBreadcrumb()
		if len(crumbs) == 0 {
			line.add("no group matches", tokens.TextTertiary)
			return line.render(m.styler, false)
		}
		const lead = "across "
		line.add(lead, tokens.TextTertiary)
		text := strings.Join(crumbs, " "+tokens.GlyphSeparator+" ")
		line.add(ansi.Truncate(text, max(1, width-len(lead)), tokens.GlyphTruncated), tokens.TextSecondary)
		return line.render(m.styler, false)
	}

	first, last := m.tabWindow(width)
	if first > 0 {
		line.add(tokens.GlyphTruncated+" ", tokens.TextTertiary)
	}
	for index := first; index <= last && index < len(m.tabs); index++ {
		if index > first {
			line.add("  ", tokens.TextTertiary)
		}
		if index == m.tab {
			if m.linear {
				line.add(tokens.GlyphAccentRail+m.tabs[index], tokens.TextPrimary)
				continue
			}
			line.addOn(m.tabs[index], tokens.TextPrimary, tokens.Band)
			continue
		}
		line.add(m.tabs[index], tokens.TextTertiary)
	}
	if last < len(m.tabs)-1 {
		line.right(tokens.GlyphTruncated, tokens.TextTertiary)
	}
	return line.render(m.styler, false)
}

// tabWindow picks the run of tabs to show. A tab bar too wide for the frame
// scrolls around the selected group rather than clipping mid-word, because a
// tab you cannot read is a tab you cannot navigate to — and the ⋯ at either
// end is 5.17's clickable-overflow mark saying there is more that way.
func (m *Model) tabWindow(width int) (int, int) {
	if len(m.tabs) == 0 {
		return 0, -1
	}
	const gap = 2
	budget := width
	total := 0
	for index, title := range m.tabs {
		if index > 0 {
			total += gap
		}
		total += ansi.StringWidth(title)
	}
	if total <= budget {
		return 0, len(m.tabs) - 1
	}
	// Room for the two overflow marks, which are what says the bar scrolls.
	budget = max(1, budget-4)

	first, last := m.tab, m.tab
	used := ansi.StringWidth(m.tabs[m.tab])
	for {
		grew := false
		if last+1 < len(m.tabs) {
			if cost := gap + ansi.StringWidth(m.tabs[last+1]); used+cost <= budget {
				used += cost
				last++
				grew = true
			}
		}
		if first > 0 {
			if cost := gap + ansi.StringWidth(m.tabs[first-1]); used+cost <= budget {
				used += cost
				first--
				grew = true
			}
		}
		if !grew {
			return first, last
		}
	}
}

// bodyLines lays the rows out and windows them onto the selection, so the band
// and as much of its detail block as fits are always on screen.
func (m *Model) bodyLines(width, height int, rows *[]int) []string {
	if height < 1 {
		return nil
	}
	if m.registry == nil {
		*rows = append(*rows, -1)
		return []string{m.notice("no settings registry here — nothing to change", width)}
	}
	if len(m.visible) == 0 {
		*rows = append(*rows, -1)
		if m.query != "" {
			return []string{m.notice("nothing matches — esc clears the search", width)}
		}
		return []string{m.notice("this group is empty", width)}
	}

	type vline struct {
		text     string
		position int
	}
	lines := make([]vline, 0, len(m.visible)+8)
	selectStart, selectEnd := 0, 0

	labels := m.labelWidth(width)
	for position, index := range m.visible {
		r := m.rows[index]
		selected := position == m.selected
		if selected {
			selectStart = len(lines)
		}
		lines = append(lines, vline{m.rowLine(r, position, selected, labels, width), position})
		if !selected {
			continue
		}
		for _, detail := range m.detailLines(r, width) {
			lines = append(lines, vline{detail, position})
		}
		selectEnd = len(lines) - 1
	}

	top := 0
	if selectEnd >= height {
		top = selectEnd - height + 1
	}
	if selectStart < top {
		top = selectStart
	}
	top = max(0, min(top, max(0, len(lines)-height)))

	out := make([]string, 0, height)
	for index := top; index < len(lines) && len(out) < height; index++ {
		out = append(out, lines[index].text)
		*rows = append(*rows, lines[index].position)
	}
	return out
}

// rowLine is one row: marker, label, value, and the right-hand chip — which is
// the row's provenance normally and its group while a search is running, so a
// result always says which tab it came from.
func (m *Model) rowLine(r row, position int, selected bool, labels, width int) string {
	line := newLine(width)
	if selected {
		line.add(tokens.GlyphAccentRail+" ", tokens.TextSecondary)
	} else {
		line.add("  ", tokens.TextTertiary)
	}

	label := r.setting.Label
	if m.query != "" {
		line.addHighlighted(label, m.hitsFor(position), labels)
	} else {
		line.add(pad(label, labels), tokens.TextPrimary)
	}
	line.add("  ", tokens.TextPrimary)

	value := m.value(r)
	if value == "" {
		value = tokens.GlyphMissing
	}
	valueToken := tokens.TextPrimary
	if !m.editable(r) {
		// An environment pin is the one thing that takes a row away from the
		// user. Amber is the word for "a human is implicated" (5.16), and this
		// is a value only a human at a shell prompt can move.
		valueToken = tokens.Amber
	}
	line.add(value, valueToken)

	chip, token := m.chip(r)
	if chip != "" {
		line.right(chip, token)
	}
	// The band is a background fill, and linear mode (10.1.5) is exactly where
	// a background fill is least trustworthy — a screen reader gets nothing
	// from it and a high-contrast terminal may render it as a block. The
	// accent rail above already carries the same fact in a printable cell, so
	// linear mode keeps the marker and drops the fill rather than replacing
	// one with the other.
	return line.render(m.styler, selected && !m.linear)
}

// chip is the right-hand column of a row line.
func (m *Model) chip(r row) (string, tokens.Token) {
	if m.query != "" {
		return r.group, tokens.TextTertiary
	}
	kind, text := m.provenance(r)
	switch kind {
	case sourcePinned:
		return text, tokens.Amber
	case sourceUnknown:
		return tokens.GlyphMissing, tokens.TextTertiary
	default:
		return text, tokens.TextTertiary
	}
}

// detailLines are the focused row's own area: what it does, what it will look
// like, what it refused, and the verbs that apply here (5.22 rule 1).
func (m *Model) detailLines(r row, width int) []string {
	indent := "    "
	body := max(8, width-len(indent)-1)
	out := make([]string, 0, 6)

	if hint := strings.TrimSpace(r.setting.Hint); hint != "" {
		wrapped, _ := blocks.Wrap(nil, hint, body)
		for _, text := range wrapped {
			line := newLine(width)
			line.add(indent, tokens.TextSecondary)
			line.add(text, tokens.TextSecondary)
			out = append(out, line.render(m.styler, false))
		}
	}

	if sample := m.preview(r, body); sample != "" {
		line := newLine(width)
		line.add(indent, tokens.TextTertiary)
		line.add(sample, tokens.TextSecondary)
		out = append(out, line.render(m.styler, false))
	}

	switch {
	case m.editing:
		line := newLine(width)
		line.add(indent, tokens.TextTertiary)
		line.add(tokens.GlyphPromptSteer+" ", tokens.Cyan)
		line.addCursor(m.editor.text(), m.editor.cursor)
		out = append(out, line.render(m.styler, false))

	case m.picking:
		line := newLine(width)
		line.add(indent, tokens.TextTertiary)
		for index, choice := range r.setting.Choices {
			if index > 0 {
				line.add("  ", tokens.TextTertiary)
			}
			if index == m.pick {
				line.addOn(choice, tokens.TextPrimary, tokens.Band)
				continue
			}
			line.add(choice, tokens.TextTertiary)
		}
		out = append(out, line.render(m.styler, false))
	}

	if failure := m.failed[r.setting.Key]; failure != "" {
		wrapped, _ := blocks.Wrap(nil, failure, body)
		for _, text := range wrapped {
			line := newLine(width)
			line.add(indent, tokens.Coral)
			line.add(text, tokens.Coral)
			out = append(out, line.render(m.styler, false))
		}
	}

	line := newLine(width)
	line.add(indent, tokens.TextTertiary)
	line.add(m.metaLine(r), tokens.TextTertiary)
	out = append(out, line.render(m.styler, false))
	return out
}

// metaLine says where the value came from and what the keyboard can do to it.
func (m *Model) metaLine(r row) string {
	kind, text := m.provenance(r)
	var parts []string
	switch kind {
	case sourcePinned:
		parts = append(parts, "pinned by "+text+" · read-only here")
	case sourceSaved:
		parts = append(parts, "saved in this profile")
	case sourceUnknown:
		parts = append(parts, "kept beside the graph "+tokens.GlyphSeparator+" origin "+tokens.GlyphMissing)
	default:
		parts = append(parts, "built-in default")
	}
	if m.editable(r) {
		switch {
		case m.editing:
			parts = append(parts, "enter save", "esc cancel")
		case m.picking:
			parts = append(parts, "←→ choose", "enter save", "esc cancel")
		default:
			parts = append(parts, kindVerb(r.setting))
		}
	}
	return strings.Join(parts, " "+tokens.GlyphSeparator+" ")
}

// hintLine is the contextual footer of 5.22 rule 4, scoped to this surface,
// dropped lowest-priority-first through the same fitting pass the real footer
// uses — the priority-drop mechanic lives in tokens and is not restated here.
func (m *Model) hintLine(width int) string {
	type part struct {
		text     string
		priority int
	}
	var parts []part
	switch {
	case m.editing:
		parts = []part{{"enter save", 100}, {"esc cancel", 95}, {"ctrl+u clear", 40}}
	case m.picking:
		parts = []part{{"←→ choose", 100}, {"enter save", 95}, {"esc cancel", 90}}
	case m.query != "":
		parts = []part{{"esc clears", 100}, {"↑↓ move", 90}, {"enter edit", 80}, {"backspace", 30}}
	default:
		parts = []part{{"type to search", 100}, {"↑↓ move", 90}, {"←→ tabs", 80},
			{"enter edit", 70}, {"esc close", 60}}
	}

	columns := make([]tokens.FooterColumn, 0, len(parts))
	for index, p := range parts {
		minWidth := ansi.StringWidth(p.text)
		if index > 0 {
			minWidth += 3
		}
		columns = append(columns, tokens.FooterColumn{ID: p.text, MinWidth: minWidth, Priority: p.priority})
	}

	line := newLine(width)
	line.add("  ", tokens.TextTertiary)
	for index, column := range tokens.FitFooter(columns, max(0, width-2)) {
		if index > 0 {
			line.add(" "+tokens.GlyphSeparator+" ", tokens.TextTertiary)
		}
		line.add(column.ID, tokens.TextTertiary)
	}
	return line.render(m.styler, false)
}

func (m *Model) notice(text string, width int) string {
	line := newLine(width)
	line.add("  ", tokens.TextSecondary)
	line.add(text, tokens.TextSecondary)
	return line.render(m.styler, false)
}

// labelWidth is the label column: wide enough for the widest label on screen,
// and never more than a third of the frame, so a long label never pushes the
// value off a narrow terminal.
func (m *Model) labelWidth(width int) int {
	widest := 0
	for _, index := range m.visible {
		widest = max(widest, ansi.StringWidth(m.rows[index].setting.Label))
	}
	return max(6, min(widest, max(6, width/3)))
}

// hitsFor returns the label highlight offsets for one visible position, from
// the list the last rebuild computed.
func (m *Model) hitsFor(position int) []int {
	if position < 0 || position >= len(m.hits) {
		return nil
	}
	return m.hits[position]
}

func pad(text string, width int) string {
	gap := width - ansi.StringWidth(text)
	if gap <= 0 {
		return text
	}
	return text + strings.Repeat(" ", gap)
}
