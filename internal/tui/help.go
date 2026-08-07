package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type helpRow struct {
	key     string
	meaning string
}

type helpCategory struct {
	title string
	rows  []helpRow
}

// helpCategories is deliberately generated from the same slashCommands table
// executeSlash dispatches. Adding a routed slash command therefore adds it to
// help in the same edit; there is no second command list to remember.
func helpCategories() []helpCategory {
	slashRows := make([]helpRow, 0, len(slashCommands))
	for _, command := range slashCommands {
		slashRows = append(slashRows, helpRow{key: "/" + command.name, meaning: command.description})
	}
	return []helpCategory{
		{title: "talking", rows: []helpRow{
			{key: "enter", meaning: "send the draft or selected answer"},
			{key: keyBindings.newline, meaning: "insert a newline without sending"},
			{key: "ctrl+b/" + keyBindings.boost, meaning: "cycle boosted next answer · pinned · off; click the active footer too"},
			{key: "ctrl+v/" + keyBindings.voice, meaning: "start or finish voice input; click the mic too; esc discards it"},
			{key: "v", meaning: "expand or collapse reading receipts outside the input"},
			{key: "↑/↓ · 1–9", meaning: "with an empty draft, choose and send a pending answer"},
			{key: "esc", meaning: "dismiss question, clear draft/attachments, then quit when input is empty"},
			{key: "backspace", meaning: "with an empty draft, remove the last attachment"},
			{key: "drag a file in", meaning: "attach an existing image path; unsupported models send it as text"},
			{key: "editing", meaning: "←/→ · home/end · ctrl+a/e/b/f · ctrl+u/k/w · delete"},
		}},
		{title: "moving around", rows: []helpRow{
			{key: keyBindings.thread + " / " + keyBindings.board + " / " + keyBindings.self, meaning: "open thread / board / self"},
			{key: "tab", meaning: "cycle input, questions/tasks, thread, rail, and header zones"},
			{key: "?", meaning: "open this guide only when the current draft is empty"},
			{key: "ctrl+c", meaning: "quit immediately"},
			{key: "↑/↓ · j/k", meaning: "move the selected row in the focused zone"},
			{key: "enter", meaning: "activate the selected row exactly like a click"},
			{key: "pgup/pgdn · wheel", meaning: "scroll the pointed or focused surface"},
			{key: "end", meaning: "return to now; in task activity, jump to the bottom"},
			{key: "esc", meaning: "expanded item → zone → input → quit; modal overlays close first"},
			{key: "ctrl+t/" + keyBindings.graph, meaning: "toggle the task rail"},
			{key: "[ / ]", meaning: "nudge the chat/task split while outside the input"},
			{key: "c", meaning: "cancel the inspected worker when its steer input is empty"},
			{key: keyBindings.settings + " / ,", meaning: "open settings; the bare comma works outside the input"},
			{key: "header", meaning: "thread · board · self · models ⌄ · ⚙ settings · tasks ▸/▾ · ? help"},
			{key: "mouse", meaning: "click focus/select/open; drag the pane divider"},
			{key: "/", meaning: "type to filter commands; tab/↑/↓ choose; enter accepts"},
		}},
		{title: "self", rows: []helpRow{
			{key: keyBindings.self, meaning: "open the list of what aforge does unattended"},
			{key: "enter · click", meaning: "drill into crafts, beliefs, skills, watches, services, practice, dials"},
			{key: "type", meaning: "inside a drill-in, the letters filter the list"},
			{key: "show N more", meaning: "widen the window; older than a week folds until asked for"},
			{key: "practice rows", meaning: "repeated attempts at one goal group into one row; open it for each attempt"},
			{key: "esc", meaning: "filter → item → list → back to the thread"},
		}},
		{title: "models", rows: []helpRow{
			{key: "models ⌄", meaning: "open all nine model slots from the header"},
			{key: "1–9", meaning: "jump to a slot inside the models palette"},
			{key: "↑/↓ · enter", meaning: "choose a slot or a model; type to filter the picker"},
			{key: "/model", meaning: "open the palette, or use /model [slot] [model]"},
			{key: "plan slot", meaning: "the model that plans and reviews jobs; empty follows the work model"},
		}},
		{title: "asking for work", rows: []helpRow{
			{key: "task", meaning: "“Draft the launch note and save it as launch.md.”"},
			{key: "standing", meaning: "“Whenever the report changes, summarize it.” · “Remind me every Friday.”"},
			{key: "surgery", meaning: "“Cancel/pause/restart that job.” · “Do the API audit first.”"},
			{key: "boost once", meaning: "press ctrl+b (or alt+b) once, then ask the one question that needs a heavier model"},
		}},
		{title: "money", rows: []helpRow{
			{key: "/budget", meaning: "show today's spend and ceiling"},
			{key: "/budget 25", meaning: "raise today's ceiling to $25"},
			{key: "/budget default 25", meaning: "set the default daily ceiling to $25"},
			{key: "/budget unlimited today", meaning: "remove today's ceiling until midnight"},
			{key: "header meter", meaning: "tokens and measured model/tool cost used by this session"},
		}},
		{title: "memory", rows: []helpRow{
			{key: "/notebook", meaning: "browse durable scoped facts and lessons"},
			{key: "notebook keys", meaning: "↑/↓ · pgup/pgdn · home/end scroll; esc closes"},
			{key: "/history", meaning: "show the most recent settled jobs"},
			{key: "/history terms", meaning: "search intents, summaries, and fold digests"},
			{key: "history rows", meaning: "↑/↓ · 1–8 · click choose; enter expands the digest"},
			{key: "ask", meaning: "“What have you learned about this repository?”"},
		}},
		{title: "slash commands", rows: slashRows},
		{title: "glyphs", rows: []helpRow{
			{key: "▸", meaning: "expand or open"},
			{key: "▾", meaning: "collapse"},
			{key: "⋯", meaning: "more content"},
			{key: "⟨×⟩", meaning: "dismiss or close"},
			{key: "⌄", meaning: "open a picker"},
			{key: "⚙", meaning: "open settings"},
			{key: "»", meaning: "boosted answer"},
			{key: "⏱", meaning: "standing work"},
			{key: "⚒", meaning: "skill"},
			{key: "⚖", meaning: "experiment"},
			{key: "⌾", meaning: "image"},
			{key: "♪", meaning: "audio"},
			{key: "▶", meaning: "video"},
			{key: "↳", meaning: "jump to the task that produced an answer"},
			{key: "‹", meaning: "go back one surface"},
		}},
	}
}

func (m *Model) openHelp() {
	if m.palette != paletteHelp {
		m.helpReturnFocus = m.focus
		m.helpReturnInput = m.inputFocused
	}
	m.palette = paletteHelp
	m.paletteSelected = 0
	m.inputFocused = false
	m.input.Blur()
	m.setSize(m.width, m.height)
}

func (m *Model) closeHelp() {
	m.palette = paletteNone
	m.paletteSelected = 0
	m.focus = m.helpReturnFocus
	m.inputFocused = m.helpReturnInput
	if m.inputFocused {
		_ = m.input.Focus()
	} else {
		m.input.Blur()
	}
	m.setSize(m.width, m.height)
}

func (m *Model) helpOverlayWidth() int {
	return min(112, max(36, m.width-4))
}

func (m *Model) helpLineLimit() int { return max(1, m.height-2) }

func (m *Model) helpMaxOffset() int {
	innerWidth := max(1, m.helpOverlayWidth()-2)
	return max(0, len(m.helpContentLines(innerWidth))-m.helpLineLimit())
}

func (m *Model) scrollHelp(delta int) {
	m.paletteSelected = max(0, min(m.helpMaxOffset(), m.paletteSelected+delta))
}

// Every fragment inside the modal is painted on the panel background. lipgloss
// closes each styled run with a full reset, so a fragment left unpainted — a
// separator space, a padded column, a foreground-only word — falls back to the
// terminal background and the panel reads as mottled bands.
var (
	helpBandStyle    = lipgloss.NewStyle().Foreground(ink).Background(selectionBand)
	helpTitleStyle   = lipgloss.NewStyle().Foreground(ink).Bold(true).Background(selectionBand)
	helpKeyStyle     = lipgloss.NewStyle().Foreground(muted).Background(selectionBand)
	helpMeaningStyle = lipgloss.NewStyle().Foreground(ink).Background(selectionBand)
)

// helpBlank is a run of painted spaces, used wherever a column has no content
// on this row.
func helpBlank(width int) string {
	if width <= 0 {
		return ""
	}
	return helpBandStyle.Render(strings.Repeat(" ", width))
}

// helpPanelRow lays one row of the panel by hand: a padding column, the line
// clipped and filled to the panel's inner width, a padding column. Composing it
// here rather than through a styled Width keeps lipgloss from word-wrapping an
// over-wide line into stray fragments hanging off the left margin.
func helpPanelRow(line string, innerWidth int) string {
	line = truncate(line, innerWidth)
	return helpBlank(1) + line + helpBlank(innerWidth-lipgloss.Width(line)) + helpBlank(1)
}

// overlayHelp shares the model palette's floating geometry and muted selection
// band. At 100+ columns categories pair up; at 80 columns they become one calm,
// stacked reading column without changing or hiding any content.
func (m *Model) overlayHelp(frame string) string {
	width := m.helpOverlayWidth()
	x := max(0, m.width-width-2)
	y := 1
	// The panel is `width` columns wide including one column of padding on each
	// side, so its content — and every line generated for it — is width-2.
	innerWidth := max(1, width-2)
	all := m.helpContentLines(innerWidth)
	limit := m.helpLineLimit()
	m.paletteSelected = max(0, min(max(0, len(all)-limit), m.paletteSelected))
	end := min(len(all), m.paletteSelected+limit)
	visible := all[m.paletteSelected:end]

	title := helpTitleStyle.Render("⟨×⟩ help")
	if len(all) > limit {
		hint := "↑/↓ scroll"
		gap := max(1, innerWidth-lipgloss.Width("⟨×⟩ help")-lipgloss.Width(hint))
		title += helpBlank(gap) + helpKeyStyle.Render(hint)
	}
	lines := append([]string{title}, visible...)
	for index := range lines {
		lines[index] = helpPanelRow(lines[index], innerWidth)
	}
	panel := strings.Join(lines, "\n")
	m.helpBounds = paneBounds{x: x, y: y, width: width, height: len(lines)}
	m.paletteCloseBounds = paneBounds{x: x + 1, y: y, width: lipgloss.Width("⟨×⟩"), height: 1}
	return overlayBlock(frame, panel, x, y, m.width)
}

func (m *Model) helpContentLines(width int) []string {
	categories := helpCategories()
	if m.width < 100 {
		keyWidth := helpKeyWidth(categories, width)
		lines := make([]string, 0)
		for index, category := range categories {
			if index > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, renderHelpCategory(category, keyWidth, width)...)
		}
		return lines
	}

	gap := 2
	columnWidth := max(18, (width-gap)/2)
	keyWidth := helpKeyWidth(categories, columnWidth)
	lines := make([]string, 0)
	for index := 0; index < len(categories); index += 2 {
		left := renderHelpCategory(categories[index], keyWidth, columnWidth)
		right := []string(nil)
		if index+1 < len(categories) {
			right = renderHelpCategory(categories[index+1], keyWidth, columnWidth)
		}
		height := max(len(left), len(right))
		for row := 0; row < height; row++ {
			leftLine, rightLine := helpBlank(columnWidth), ""
			if row < len(left) {
				leftLine = left[row]
			}
			if row < len(right) {
				rightLine = right[row]
			}
			lines = append(lines, leftLine+helpBlank(gap)+rightLine)
		}
		if index+2 < len(categories) {
			lines = append(lines, "")
		}
	}
	return lines
}

// helpKeyWidth is measured once across every category so the key column lines
// up down the whole modal instead of stepping in and out per category.
func helpKeyWidth(categories []helpCategory, width int) int {
	widest := 0
	for _, category := range categories {
		for _, row := range category.rows {
			widest = max(widest, lipgloss.Width(row.key))
		}
	}
	return max(1, min(widest, max(8, width/3)))
}

func renderHelpCategory(category helpCategory, keyWidth, width int) []string {
	lines := []string{helpTitleStyle.Render(padANSI(category.title, width))}
	meaningWidth := max(8, width-keyWidth-2)
	for _, row := range category.rows {
		wrapped := strings.Split(wrapText(row.meaning, meaningWidth), "\n")
		if len(wrapped) == 0 {
			wrapped = []string{""}
		}
		for index, meaning := range wrapped {
			key := ""
			if index == 0 {
				key = truncate(row.key, keyWidth)
			}
			lines = append(lines, helpKeyStyle.Render(padANSI(key, keyWidth))+
				helpMeaningStyle.Render("  "+padANSI(meaning, meaningWidth)))
		}
	}
	return lines
}
