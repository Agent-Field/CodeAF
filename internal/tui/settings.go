package tui

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The settings sheet is one calm column over the frame, built from the same
// overlay geometry and selection band as help and the models door. Categories
// are typography, not boxes; a row shows its label and its current reading, and
// only the focused row explains itself. Nothing here posts to the thread — the
// row showing its new value is the whole receipt.
//
// Model rows open the existing capability-filtered picker rather than a second
// one. Environment-pinned rows read dim and refuse edits, because a surface
// that silently fights the shell it was launched from cannot be trusted.

const (
	settingsMaxWidth   = 76
	settingsMinWidth   = 36
	settingsLabelWidth = 22
	settingsIndent     = "    "
)

type settingsRowBounds struct {
	bounds paneBounds
	index  int
}

func newSettingsEditor() textinput.Model {
	editor := textinput.New()
	editor.Prompt = ""
	editor.CharLimit = 120
	editor.TextStyle = inputTextStyle
	editor.PlaceholderStyle = placeholderStyle
	editor.Cursor.Style = cursorStyle
	return editor
}

func (m *Model) settingsAvailable() bool { return m.settingsRegistry != nil }

func (m *Model) openSettings() tea.Cmd { return m.showSettings(true) }

// restoreSettings brings the sheet back under a surface it opened — the model
// picker — without moving the selection the user left there.
func (m *Model) restoreSettings() { _ = m.showSettings(false) }

func (m *Model) showSettings(fresh bool) tea.Cmd {
	if !m.settingsAvailable() {
		// Nothing opened, so nothing borrowed the composer: only the command
		// that asked for the sheet goes.
		m.dropSlashCommand()
		return m.showStatus("settings aren't available in this window")
	}
	if fresh && m.palette != paletteSettings {
		m.settingsReturnFocus = m.focus
		m.settingsReturnInput = m.inputFocused
		m.settingsIndex = 0
		m.settingsOffset = 0
	}
	// The sheet takes the composer's focus, so it takes the composer's words
	// with it and gives them back on the way out. Help and voice have always
	// done this; settings resetting the line was a draft destroyed by a chord.
	m.borrowDraft()
	m.palette = paletteSettings
	m.settingsEditing = false
	m.settingsError = ""
	m.inputFocused = false
	m.input.Blur()
	m.refreshSettings()
	m.setSize(m.width, m.height)
	return nil
}

func (m *Model) closeSettings() {
	m.palette = paletteNone
	m.settingsEditing = false
	m.settingsError = ""
	m.focus = m.settingsReturnFocus
	m.inputFocused = m.settingsReturnInput
	if m.inputFocused {
		_ = m.input.Focus()
	} else {
		m.input.Blur()
	}
	m.returnDraft()
	m.setSize(m.width, m.height)
}

// refreshSettings re-reads every row, so an applied change is visible in the
// same frame that accepted it.
func (m *Model) refreshSettings() {
	if !m.settingsAvailable() {
		m.settingsGroups = nil
		return
	}
	m.settingsGroups = m.settingsRegistry.Groups()
	if count := len(m.settingsRows()); count > 0 {
		m.settingsIndex = max(0, min(count-1, m.settingsIndex))
	} else {
		m.settingsIndex = 0
	}
}

func (m *Model) settingsRows() []config.Setting {
	rows := make([]config.Setting, 0, 20)
	for _, group := range m.settingsGroups {
		rows = append(rows, group.Rows...)
	}
	return rows
}

func (m *Model) selectedSetting() (config.Setting, bool) {
	rows := m.settingsRows()
	if len(rows) == 0 {
		return config.Setting{}, false
	}
	index := max(0, min(len(rows)-1, m.settingsIndex))
	return rows[index], true
}

// settingLabel names a row; the boost slot borrows the models door's habit of
// saying out loud when it is following work.
func (m *Model) settingLabel(row config.Setting) string {
	if row.Kind == config.SettingModel && row.Slot == "boost" && m.modelFollowsWork() {
		return row.Label + " (work)"
	}
	return row.Label
}

// settingValue prefers the live surface reading for model slots so an
// optimistic switch shows immediately, and the registry for everything else.
func (m *Model) settingValue(row config.Setting) string {
	if row.Kind == config.SettingModel {
		if current := strings.TrimSpace(m.currentModel(row.Slot)); current != "" {
			return modelShort(current)
		}
		return "–"
	}
	return row.Value()
}

// editableValue is the reading with its unit removed, so the editor opens on
// the number the user would type rather than on its decoration.
func editableValue(row config.Setting) string {
	value := row.Value()
	switch row.Kind {
	case config.SettingDollars:
		return strings.TrimPrefix(value, "$")
	case config.SettingPercent:
		return strings.TrimSuffix(value, "%")
	case config.SettingText:
		if value == row.EmptyLabel {
			return ""
		}
	}
	return value
}

func (m *Model) updateSettingsKey(message tea.KeyMsg) (tea.Cmd, bool) {
	key := message.String()
	if m.settingsEditing {
		switch key {
		case "esc":
			m.settingsEditing = false
			m.settingsError = ""
			return nil, true
		case "enter":
			return m.commitSettingsEdit(), true
		}
		if message.Alt {
			// An option chord is an instruction the sheet is refusing, not a
			// character: it must not land in the field as its bare letter.
			return nil, true
		}
		// Everything else is typing. j and k are letters while an editor is
		// open; they must never move the selection under the hand.
		var command tea.Cmd
		m.settingsEditor, command = m.settingsEditor.Update(message)
		return command, true
	}
	switch key {
	case "esc":
		m.closeSettings()
		return nil, true
	case "up", "k":
		m.moveSettingsSelection(-1)
		return nil, true
	case "down", "j":
		m.moveSettingsSelection(1)
		return nil, true
	case "pgup":
		m.moveSettingsSelection(-max(1, m.settingsLineLimit()/2))
		return nil, true
	case "pgdown":
		m.moveSettingsSelection(max(1, m.settingsLineLimit()/2))
		return nil, true
	case "home":
		m.settingsIndex = 0
		return nil, true
	case "end":
		m.settingsIndex = max(0, len(m.settingsRows())-1)
		return nil, true
	case "enter", " ":
		return m.activateSetting(), true
	}
	// The sheet is modal: an unrecognized key must not operate the surface
	// underneath it.
	return nil, true
}

func (m *Model) moveSettingsSelection(delta int) {
	count := len(m.settingsRows())
	if count == 0 {
		return
	}
	m.settingsIndex = max(0, min(count-1, m.settingsIndex+delta))
	m.settingsError = ""
}

func (m *Model) activateSetting() tea.Cmd {
	row, ok := m.selectedSetting()
	if !ok {
		return nil
	}
	if name, pinned := row.PinnedBy(); pinned {
		m.settingsError = "unset " + name + " in your shell to change this here"
		return nil
	}
	switch row.Kind {
	case config.SettingModel:
		m.modelPickerReturn = paletteSettings
		return m.openModelPicker(row.Slot)
	case config.SettingBool:
		next := "on"
		if row.Value() == "on" {
			next = "off"
		}
		return m.applySetting(row, next)
	case config.SettingChoice:
		return m.applySetting(row, nextChoice(row))
	}
	m.settingsEditing = true
	m.settingsError = ""
	m.settingsEditor = newSettingsEditor()
	m.settingsEditor.SetValue(editableValue(row))
	_ = m.settingsEditor.Focus()
	return nil
}

func nextChoice(row config.Setting) string {
	current := row.Value()
	for index, choice := range row.Choices {
		if choice == current {
			return row.Choices[(index+1)%len(row.Choices)]
		}
	}
	if len(row.Choices) > 0 {
		return row.Choices[0]
	}
	return current
}

func (m *Model) commitSettingsEdit() tea.Cmd {
	row, ok := m.selectedSetting()
	if !ok {
		m.settingsEditing = false
		return nil
	}
	return m.applySetting(row, m.settingsEditor.Value())
}

// applySetting persists and lands the live effect. The divider is the one row
// the surface itself owns, so it takes hold in this frame.
func (m *Model) applySetting(row config.Setting, value string) tea.Cmd {
	if err := row.Apply(value); err != nil {
		m.settingsError = err.Error()
		return nil
	}
	m.settingsEditing = false
	m.settingsError = ""
	if row.Key == config.KeySplitPct {
		if pct, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(value), "%")); err == nil {
			m.splitPct = clampSplitPct(pct)
		}
	}
	m.refreshSettings()
	m.setSize(m.width, m.height)
	return nil
}

// Geometry mirrors the help overlay: a floating panel under the header, right
// aligned with the header words that open it, scrolling when it outgrows the
// frame.

// The sheet floats in a wide frame and fills a narrow one, and it stops at the
// bottom of the reading pane rather than at the bottom of the terminal — the
// same two rules the help overlay follows, for the same reason. A modal drawn
// over the composer in a 60-column dock leaves the input's own corners showing
// on either side of it, and takes the composer away while it does.
func (m *Model) settingsOverlayWidth() int {
	if m.width < railAtWidth {
		return m.width
	}
	return min(settingsMaxWidth, max(settingsMinWidth, m.width-4))
}

func (m *Model) settingsLineLimit() int { return max(1, min(m.height-2, m.chatHeight)) }

func (m *Model) settingsMaxOffset() int {
	lines, _ := m.settingsContentLines(m.settingsContentWidth())
	return max(0, len(lines)-m.settingsLineLimit())
}

// settingsContentWidth is the column the rows live in: the panel less its
// border-free one-cell padding on each side.
func (m *Model) settingsContentWidth() int {
	return max(1, m.settingsOverlayWidth()-4)
}

func (m *Model) scrollSettings(delta int) {
	m.settingsOffset = max(0, min(m.settingsMaxOffset(), m.settingsOffset+delta))
}

// settingsContentLines renders the whole sheet and reports which line each
// selectable row landed on, so scrolling can keep the selection in view and a
// click can find the row under the pointer.
//
// The sheet is laid out once per event rather than once per reader of it: the
// wheel asks how far down the sheet goes and then the frame draws it, and both
// were rendering every row of it. Everything the layout reads — the selection,
// the row being edited, the error under it, the values themselves — changes
// only in response to a message, and the arrival of a message is what drops
// this. An open editor is never kept: its cursor is part of the bytes.
func (m *Model) settingsContentLines(width int) ([]string, []int) {
	if m.settingsLinesOK && m.settingsLinesFor == width {
		return m.settingsLines, m.settingsRowLines
	}
	lines, rowLines := m.layOutSettings(width)
	if !m.settingsEditing {
		m.settingsLines, m.settingsRowLines = lines, rowLines
		m.settingsLinesOK, m.settingsLinesFor = true, width
	}
	return lines, rowLines
}

func (m *Model) layOutSettings(width int) ([]string, []int) {
	lines := make([]string, 0, 48)
	rowLines := make([]int, 0, 24)
	labelWidth := min(settingsLabelWidth, max(12, width/3))
	index := 0
	for groupIndex, group := range m.settingsGroups {
		if groupIndex > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(ink).Bold(true).Render(group.Title))
		for _, row := range group.Rows {
			selected := index == m.settingsIndex
			rowLines = append(rowLines, len(lines))
			lines = append(lines, m.settingsRowLine(row, selected, labelWidth, width))
			if selected {
				lines = append(lines, m.settingsDetailLines(row, width)...)
			}
			index++
		}
	}
	if footer := m.settingsEnvironmentLines(width); len(footer) > 0 {
		lines = append(lines, "")
		lines = append(lines, footer...)
	}
	return lines, rowLines
}

func (m *Model) settingsRowLine(row config.Setting, selected bool, labelWidth, width int) string {
	marker := "  "
	markerStyle := mutedStyle
	if selected {
		marker = "› "
		markerStyle = lipgloss.NewStyle().Foreground(powder).Bold(true)
	}
	name, pinned := row.PinnedBy()
	trailing := ""
	if row.Kind == config.SettingModel || row.Kind == config.SettingChoice {
		trailing = "  ⌄"
	}
	if pinned {
		trailing = "  pinned by " + name
	}
	valueWidth := max(1, width-lipgloss.Width(marker)-labelWidth-lipgloss.Width(trailing))

	value := m.settingValue(row)
	valueStyle := inputTextStyle
	if pinned {
		valueStyle = mutedStyle
	}
	if selected && m.settingsEditing {
		m.settingsEditor.Width = max(4, valueWidth-1)
		return markerStyle.Render(marker) +
			mutedStyle.Faint(true).Render(settingsLabelCell(m.settingLabel(row), labelWidth)) +
			m.settingsEditor.View()
	}
	return truncate(markerStyle.Render(marker)+
		mutedStyle.Faint(true).Render(settingsLabelCell(m.settingLabel(row), labelWidth))+
		valueStyle.Render(padANSI(truncate(value, valueWidth), valueWidth))+
		mutedStyle.Faint(true).Render(trailing), width)
}

// settingsLabelCell always leaves one column of air before the value, so a
// long label narrows itself rather than colliding with what it names.
func settingsLabelCell(label string, width int) string {
	return padANSI(truncate(label, max(1, width-1)), width)
}

// settingsDetailLines is the progressive disclosure: one hint at a time, under
// the row it explains, plus whatever the last edit could not accept.
func (m *Model) settingsDetailLines(row config.Setting, width int) []string {
	detail := make([]string, 0, 3)
	if m.settingsError != "" {
		for _, line := range strings.Split(wrapText(m.settingsError, max(8, width-len(settingsIndent))), "\n") {
			detail = append(detail, settingsIndent+lipgloss.NewStyle().Foreground(rose).Render(line))
		}
		return detail
	}
	hint := row.Hint
	if m.settingsEditing {
		hint = "enter saves · ctrl+u clears · esc leaves it as it was"
	}
	if strings.TrimSpace(hint) == "" {
		return detail
	}
	for _, line := range strings.Split(wrapText(hint, max(8, width-len(settingsIndent))), "\n") {
		detail = append(detail, settingsIndent+mutedStyle.Faint(true).Render(line))
	}
	return detail
}

// The environment footer is read-only on purpose: these are the operator's
// plumbing, not the user's settings, and naming them is more honest than
// pretending the sheet owns them.
func (m *Model) settingsEnvironmentLines(width int) []string {
	if !m.settingsAvailable() {
		return nil
	}
	pins := m.settingsRegistry.EnvironmentPins()
	lines := []string{lipgloss.NewStyle().Foreground(ink).Bold(true).Render("environment")}
	body := "nothing pinned · aforge is running on its own defaults"
	if len(pins) > 0 {
		body = strings.Join(pins, " · ")
	}
	for _, line := range strings.Split(wrapText(body, max(8, width-len(settingsIndent))), "\n") {
		lines = append(lines, settingsIndent+mutedStyle.Faint(true).Render(line))
	}
	return lines
}

func (m *Model) overlaySettings(frame string) string {
	width := m.settingsOverlayWidth()
	x := max(0, m.width-width-2)
	y := 1
	innerWidth := max(1, width-2)
	contentWidth := m.settingsContentWidth()
	all, rowLines := m.settingsContentLines(contentWidth)
	limit := m.settingsLineLimit()

	// Keep the selected row and its hint on screen without ever scrolling past
	// the end of the sheet.
	if m.settingsIndex < len(rowLines) {
		selectedLine := rowLines[m.settingsIndex]
		selectedBottom := selectedLine
		if next := m.settingsIndex + 1; next < len(rowLines) {
			// Everything up to the line before the next row: the hint, an
			// editor refusal, every wrapped line — and, for the last row of a
			// group, the blank line and heading that precede the next one. A
			// line or two of slack there is harmless; keeping only the row's
			// first line visible would put the very sentence somebody needs
			// just below the frame. The clamp below keeps the row itself as
			// the floor, so a hint taller than the frame is cut, never the row.
			selectedBottom = rowLines[next] - 1
		}
		if selectedLine < m.settingsOffset {
			// One line of context above keeps the group's name on screen.
			m.settingsOffset = max(0, selectedLine-1)
		} else if selectedBottom >= m.settingsOffset+limit {
			m.settingsOffset = max(0, min(selectedLine, selectedBottom-limit+1))
		}
	}
	m.settingsOffset = max(0, min(max(0, len(all)-limit), m.settingsOffset))
	end := min(len(all), m.settingsOffset+limit)
	visible := all[m.settingsOffset:end]

	title := "⟨×⟩ settings"
	if contentWidth >= 58 {
		title = overlayRight(title, "↑/↓ move · enter change · esc close", contentWidth)
	}
	lines := append([]string{title}, visible...)
	panelStyle := lipgloss.NewStyle().Foreground(ink).Background(selectionBand).Padding(0, 1).Width(innerWidth)
	for index := range lines {
		lines[index] = panelStyle.Render(truncate(lines[index], contentWidth))
	}
	panel := strings.Join(lines, "\n")

	m.settingsBounds = paneBounds{x: x, y: y, width: width, height: len(lines)}
	m.paletteCloseBounds = paneBounds{x: x + 1, y: y, width: lipgloss.Width("⟨×⟩"), height: 1}
	m.settingsRowHits = m.settingsRowHits[:0]
	for index, line := range rowLines {
		if line < m.settingsOffset || line >= end {
			continue
		}
		m.settingsRowHits = append(m.settingsRowHits, settingsRowBounds{
			bounds: paneBounds{x: x, y: y + 1 + line - m.settingsOffset, width: width, height: 1},
			index:  index,
		})
	}
	return overlayBlock(frame, panel, x, y, m.width)
}

// clickSettings is click-equals-enter: the pointed row is selected and opened
// exactly as the keyboard would open it.
func (m *Model) clickSettings(x, y int) (tea.Cmd, bool) {
	if m.paletteCloseBounds.contains(x, y) {
		m.closeSettings()
		return nil, true
	}
	for _, row := range m.settingsRowHits {
		if !row.bounds.contains(x, y) {
			continue
		}
		if m.settingsEditing && row.index != m.settingsIndex {
			m.settingsEditing = false
		}
		m.settingsIndex = row.index
		m.settingsError = ""
		return m.activateSetting(), true
	}
	if m.settingsBounds.contains(x, y) {
		return nil, true
	}
	m.closeSettings()
	return nil, true
}
