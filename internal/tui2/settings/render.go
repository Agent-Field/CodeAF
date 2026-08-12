package settings

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/registry"
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
// The shape, top to bottom (15: structure is never labeled — the groups are
// four faint lowercase words and the blank lines between them, never a tab bar
// that hid four fifths of the sheet behind a keystroke nobody found):
//
//	‹ settings
//
//	  models
//	  conversation          deepseek-v4-flash          $0.4/M out  default
//	▎ execution             deepseek-v4-flash          $0.4/M out  default
//
//	  spending
//	  daily budget          $25                         $3.12 today  saved
//	  ask before spending   $3                                     default
//	    when a planned job is estimated to cost more than this, aforge
//	    quotes the step count and the price and waits for your go-ahead.
//	    built-in default · edit enter
//
//	type to search · move ↑↓ · groups ←→ · edit enter · close esc
//
// The action words are verb·key chips ([registry.Chip]): the VERB first, at the
// brighter of the two dim tiers, and the key after it one tier down. This line
// used to read `esc close` — two greys, two words, and nothing in the row
// saying which one names the act and which one is the thing to press.
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
	if height >= 8 {
		push(blank(m.styler, width, m.ground()), -1)
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
			lines = append(lines, blank(m.styler, width, m.ground()))
			rows = append(rows, -1)
		}
		lines = append(lines, hint)
		rows = append(rows, -1)
	}
	// The sheet stands on its whole rectangle. A pane is given a box and told to
	// fill it (pane.go); one that returns fewer lines leaves the room it floats
	// over showing through the bottom of it, which is 12.13's ghost one plane up.
	for len(lines) < height {
		lines = append(lines, blank(m.styler, width, m.ground()))
		rows = append(rows, -1)
	}

	m.rowAtLine = append(m.rowAtLine, rows...)
	return strings.Join(lines, "\n")
}

// ground is the floor this surface paints under everything that is not banded.
//
// Linear mode (10.1.5) gets none, and for the same reason it gets no selection
// band: a background fill is what a screen reader cannot see and what a
// high-contrast terminal may render as a solid block. Nothing is lost by
// dropping it — the ground carries no information, only elevation — which is
// exactly why it is the part that yields.
func (m *Model) ground() tokens.Token {
	if m.linear {
		return tokens.Ground
	}
	return tokens.Sheet
}

// header is the scope line: where you are, and — only while a search is
// running — how much of the sheet answered.
//
// Browsing carries no count. 15 forbids counting things whose multiplicity is
// already visible, and "22 rows · 7 groups" was exactly that: the rows are on
// the screen and the groups are their own words. A search is the one case where
// the number is a fact the reader cannot see, because what it counts is what
// the query took away.
func (m *Model) header(width int) string {
	line := newLine(width)
	// Where you are, and — when something drilled into this sheet — the way
	// back out of it, on the line that already names the surface (trail.go).
	m.addTrail(line)
	if m.query == "" {
		return line.render(m.styler, false, m.ground())
	}
	line.add(" "+tokens.GlyphSeparator+" ", tokens.TextTertiary)
	line.add(m.query, tokens.TextPrimary)
	line.right(strconv.Itoa(len(m.visible))+" of "+strconv.Itoa(len(m.rows)), tokens.TextTertiary)
	return line.render(m.styler, false, m.ground())
}

// groupLine is 15's one allowance: a single faint lowercase word announcing a
// section, and only because a sheet with four unrelated runs of rows is exactly
// where position alone is genuinely ambiguous. It is never selectable and never
// wears the accent — nothing inert does (12).
func (m *Model) groupLine(title string, width int) string {
	line := newLine(width)
	line.add("  ", tokens.TextTertiary)
	// A group word cut short is a static tail cut, not an overflow anyone can
	// click — ⋯ means "there is more that way" everywhere else on this surface,
	// and a heading has no that-way. [tokens.GlyphEllipsis] is the mark for
	// text that simply ran out of frame.
	line.add(ansi.Truncate(title, max(1, width-2), tokens.GlyphEllipsis), tokens.TextTertiary)
	return line.render(m.styler, false, m.ground())
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
	group := ""
	for position, index := range m.visible {
		r := m.rows[index]
		// Group words only when the page is the page. A search result already
		// carries its group on the right of its own line, and a heading over a
		// run of one would be the label doing structure's job again.
		if m.query == "" && r.group != group {
			group = r.group
			if len(lines) > 0 {
				lines = append(lines, vline{blank(m.styler, width, m.ground()), -1})
			}
			lines = append(lines, vline{m.groupLine(group, width), -1})
		}
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
	if values := m.valueWidth(width); values > 0 {
		value = pad(value, values)
	}
	line.add(value, valueToken)

	chip, token := m.chip(r)
	// The receipt is dim and it yields first. It is the third thing on the line
	// and the least of them — the value is the state, the chip is where the
	// value came from, and the receipt is a fact standing beside both — so a
	// frame with room for two of the three drops this one rather than crushing
	// the column that says whether a value is even yours to change.
	if receipt := m.receipt(r); receipt != "" {
		want := ansi.StringWidth(receipt) + 2
		if chip != "" {
			want += ansi.StringWidth(chip) + 2
		}
		if line.max-line.used >= want {
			line.add("  ", tokens.TextTertiary)
			line.add(receipt, tokens.TextTertiary)
		}
	}
	if chip != "" {
		line.right(chip, token)
	}
	// The band is a background fill, and linear mode (10.1.5) is exactly where
	// a background fill is least trustworthy — a screen reader gets nothing
	// from it and a high-contrast terminal may render it as a block. The
	// accent rail above already carries the same fact in a printable cell, so
	// linear mode keeps the marker and drops the fill rather than replacing
	// one with the other.
	return line.render(m.styler, selected && !m.linear, m.ground())
}

// receipt is the dim fact that stands beside a row's value: today's spend
// beside the day's ceiling, a price beside a model. It comes from the registry
// (config.Setting.Receipt) and never from a read this package makes — a pane's
// Render is a pure function of its state, and a receipt this file computed
// would be a second reader of the same fact.
//
// A row being edited shows none: the value on screen is not yet the value the
// receipt is about, and a receipt that describes the previous number while the
// user types the next one is worse than no receipt.
func (m *Model) receipt(r row) string {
	if _, pending := m.pending[r.setting.Key]; pending {
		return ""
	}
	return r.setting.Receipt()
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
			out = append(out, line.render(m.styler, false, m.ground()))
		}
	}

	if sample := m.preview(r, body); sample != "" {
		line := newLine(width)
		line.add(indent, tokens.TextTertiary)
		line.add(sample, tokens.TextSecondary)
		out = append(out, line.render(m.styler, false, m.ground()))
	}

	switch {
	case m.editing:
		line := newLine(width)
		line.add(indent, tokens.TextTertiary)
		line.add(tokens.GlyphPromptSteer+" ", tokens.Cyan)
		line.addCursor(m.editor.text(), m.editor.cursor)
		out = append(out, line.render(m.styler, false, m.ground()))

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
		out = append(out, line.render(m.styler, false, m.ground()))
	}

	if failure := m.failed[r.setting.Key]; failure != "" {
		wrapped, _ := blocks.Wrap(nil, failure, body)
		for _, text := range wrapped {
			line := newLine(width)
			line.add(indent, tokens.Coral)
			line.add(text, tokens.Coral)
			out = append(out, line.render(m.styler, false, m.ground()))
		}
	}

	line := newLine(width)
	line.add(indent, tokens.TextTertiary)
	lead, chips := m.metaParts(r)
	line.add(lead, tokens.TextTertiary)
	for _, chip := range chips {
		line.add(" "+tokens.GlyphSeparator+" ", tokens.TextTertiary)
		addChip(line, chip)
	}
	out = append(out, line.render(m.styler, false, m.ground()))
	return out
}

// metaParts says where the value came from, and what the keyboard can do to it.
//
// The provenance half is a sentence and the action half is verb·key chips
// (registry.Chip): the verb first at the brighter tier, the key after it one
// tier down. They are returned apart because they are painted apart — a
// sentence is all one tier and a chip is two, and joining them into one string
// is what made `esc save` read as two interchangeable grey words.
func (m *Model) metaParts(r row) (string, []registry.Chip) {
	kind, text := m.provenance(r)
	lead := "built-in default"
	switch kind {
	case sourcePinned:
		lead = "pinned by " + text + " " + tokens.GlyphSeparator + " read-only here"
	case sourceSaved:
		lead = "saved in this profile"
	case sourceUnknown:
		lead = "kept beside the graph " + tokens.GlyphSeparator + " origin " + tokens.GlyphMissing
	}
	if !m.editable(r) {
		return lead, nil
	}
	switch {
	case m.editing:
		return lead, []registry.Chip{
			registry.ChipFor("save", "enter"),
			registry.ChipFor("cancel", "esc"),
		}
	case m.picking:
		return lead, []registry.Chip{
			registry.ChipFor("choose", "←→"),
			registry.ChipFor("save", "enter"),
			registry.ChipFor("cancel", "esc"),
		}
	}
	return lead, kindChips(r.setting)
}

// addChip paints one chip in the two tiers the grammar asks for. It is
// internal/tui2/palette's addChip, spent on this surface's own line buffer —
// the order and the fallback ladder live in internal/registry, the paint lives
// wherever the pixels do.
func addChip(line *lineBuf, chip registry.Chip) {
	if chip.Empty() {
		line.add(chip.Key, tokens.TextTertiary)
		return
	}
	// Never the dimmest tier for the verb: 5.22's checklist says an interactive
	// chip may not live there. The key may — it is annotation, not the control.
	line.add(chip.Verb, tokens.TextSecondary)
	if chip.Key != "" {
		line.add(registry.ChipGap+chip.Key, tokens.TextTertiary)
	}
}

// hintLine is the contextual footer of 5.22 rule 4, scoped to this surface,
// dropped lowest-priority-first through the same fitting pass the real footer
// uses — the priority-drop mechanic lives in tokens and is not restated here.
func (m *Model) hintLine(width int) string {
	type part struct {
		chip     registry.Chip
		priority int
	}
	// Verb first, key after (registry.Chip). This line is where the rule was
	// found: it used to read `esc close`, two greys and two words with nothing
	// saying which one is the label and which one is the thing to press.
	var parts []part
	switch {
	case m.editing:
		parts = []part{
			{registry.ChipFor("save", "enter"), 100},
			{registry.ChipFor("cancel", "esc"), 95},
			{registry.ChipFor("clear", "ctrl+u"), 40},
		}
	case m.picking:
		parts = []part{
			{registry.ChipFor("choose", "←→"), 100},
			{registry.ChipFor("save", "enter"), 95},
			{registry.ChipFor("cancel", "esc"), 90},
		}
	case m.query != "":
		parts = []part{
			{registry.ChipFor("clear", "esc"), 100},
			{registry.ChipFor("move", "↑↓"), 90},
			{registry.ChipFor("edit", "enter"), 80},
			{registry.ChipFor("erase", "backspace"), 30},
		}
	default:
		// "type to search" keeps its sentence rather than becoming a chip: it
		// is not a verb·key pair but the one thing this surface has to teach —
		// that letters do not navigate here — and a chip reading `search type`
		// would spend the grammar on a word that is not a key.
		parts = []part{
			{registry.Chip{Key: "type to search"}, 100},
			{registry.ChipFor("move", "↑↓"), 90},
			{registry.ChipFor("groups", "←→"), 80},
			{registry.ChipFor("edit", "enter"), 70},
			{registry.ChipFor("close", "esc"), 60},
		}
	}

	byID := make(map[string]registry.Chip, len(parts))
	columns := make([]tokens.FooterColumn, 0, len(parts))
	for index, p := range parts {
		id := p.chip.String()
		byID[id] = p.chip
		minWidth := ansi.StringWidth(id)
		if index > 0 {
			minWidth += 3
		}
		columns = append(columns, tokens.FooterColumn{ID: id, MinWidth: minWidth, Priority: p.priority})
	}

	line := newLine(width)
	line.add("  ", tokens.TextTertiary)
	for index, column := range tokens.FitFooter(columns, max(0, width-2)) {
		if index > 0 {
			line.add(" "+tokens.GlyphSeparator+" ", tokens.TextTertiary)
		}
		addChip(line, byID[column.ID])
	}
	return line.render(m.styler, false, m.ground())
}

func (m *Model) notice(text string, width int) string {
	line := newLine(width)
	line.add("  ", tokens.TextSecondary)
	line.add(text, tokens.TextSecondary)
	return line.render(m.styler, false, m.ground())
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

// valueWidth is the value column, and it is zero unless something on screen
// carries a receipt. A column exists to line receipts up with each other; with
// nothing to line up it is padding that buys nothing and costs the right-hand
// chip its room on a narrow frame.
func (m *Model) valueWidth(width int) int {
	widest, receipts := 0, false
	for _, index := range m.visible {
		r := m.rows[index]
		if m.receipt(r) != "" {
			receipts = true
		}
		value := m.value(r)
		if value == "" {
			value = tokens.GlyphMissing
		}
		widest = max(widest, ansi.StringWidth(value))
	}
	if !receipts {
		return 0
	}
	return min(widest, max(6, width/3))
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
