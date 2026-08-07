package tui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// ── The design system ────────────────────────────────────────────────────────
//
// Voice hierarchy (four typographic levels, one accent, no new colors):
//  1. aforge speaks: the speaker label in lavender — the single conversational
//     accent — with its body in primary ink, markdown-rendered. The answer is
//     the product; it gets the brightest ink.
//  2. you speak: the label dim (muted, faint) and the body in softened ink
//     (ink, faint). The reader knows their own words; they recede slightly so
//     the answers carry the page.
//  3. machine status is ambient: cards, shimmer, receipts, timestamps, and
//     meta all live in muted ink. Status never borrows the conversational
//     accent — attention stays budgeted.
//  4. structure is faint: frames (╭ │ ╰), rules, gutters, and hints render
//     muted+faint. They shape the page without competing with words.
//
// Node-view tool blocks share the same system: a call line is its kind glyph +
// tool name in the working accent (peach, semibold) followed by the command in
// primary ink; output sits indented behind a faint "│" gutter in dim ink;
// failure is a rose ✗ on the status position only — never a whole red block.
//
// Affordance grammar (terminals have no hover, so every clickable element
// declares its action at rest, in muted ink, never the accent):
//
//	▸  expandable — click or enter opens it (also the focus/selection marker,
//	   which renders in powder so target and affordance stay distinguishable)
//	▾  expanded — click or enter collapses it
//	⋯  truncated content — click reveals the rest
//	⟨×⟩ dismiss/close a surface
//	↳  jump to the task an answer came from
//	‹  go back one surface
//	⌄  open a dropdown
//
// Hints stay dim and short, and appear only when glyph + noun cannot carry the
// action alone; global keys live in the one footer line and are not repeated
// per element. Plain "…" marks static overflow that is not clickable.
//
// Interaction zones and the back-out ladder: tab cycles input → dock → thread
// (→ rail while it is open). Within a zone ↑/↓ move the focus marker across
// that zone's interactive elements and enter activates exactly what a click
// would; pgup/pgdn and the wheel scroll. esc climbs one rung at a time:
// expanded element → collapsed element → zone → input → quit.
var (
	lavender = lipgloss.AdaptiveColor{Light: "#6D4BC3", Dark: "#C6B4F5"}
	powder   = lipgloss.AdaptiveColor{Light: "#256B8C", Dark: "#AEDFF7"}
	mint     = lipgloss.AdaptiveColor{Light: "#277A62", Dark: "#A8E6CF"}
	peach    = lipgloss.AdaptiveColor{Light: "#A65B2A", Dark: "#FFD3B6"}
	butter   = lipgloss.AdaptiveColor{Light: "#826614", Dark: "#FBE7A1"}
	rose     = lipgloss.AdaptiveColor{Light: "#B23A57", Dark: "#F5A9B8"}
	muted    = lipgloss.AdaptiveColor{Light: "#686A78", Dark: "#6C7086"}
	ink      = lipgloss.AdaptiveColor{Light: "#2E3038", Dark: "#E8E7EE"}

	promptStyle      = lipgloss.NewStyle().Foreground(powder).Bold(true)
	inputTextStyle   = lipgloss.NewStyle().Foreground(ink)
	placeholderStyle = lipgloss.NewStyle().Foreground(muted)
	cursorStyle      = lipgloss.NewStyle().Foreground(powder)
	mutedStyle       = lipgloss.NewStyle().Foreground(muted)
	questionStyle    = lipgloss.NewStyle().Foreground(lavender)
	selectedInk      = lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#24202E"}
	selectionBand    = lipgloss.AdaptiveColor{Light: "#E8E7EE", Dark: "#343442"}
	selectedStyle    = lipgloss.NewStyle().Foreground(ink).Background(selectionBand)
	pillStyle        = lipgloss.NewStyle().Foreground(selectedInk).Background(peach).Padding(0, 1)

	// The two conversational voices (level 1 and 2 above).
	aforgeLabelStyle = lipgloss.NewStyle().Foreground(lavender)
	youLabelStyle    = lipgloss.NewStyle().Foreground(muted).Faint(true)
	youTextStyle     = lipgloss.NewStyle().Foreground(ink).Faint(true)

	spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
)

// View composes the complete frame once, avoiding terminal-clearing redraws.
// The frame is open text — hierarchy comes from ink and whitespace, not boxes.
func (m *Model) View() string {
	m.trackPaneBounds()
	top := m.renderTopBar()

	var main string
	switch {
	case m.nodeViewID != "":
		main = m.renderNodePane()
	case m.graphVisible() && m.horizontal:
		main = lipgloss.JoinHorizontal(lipgloss.Top, m.renderChatPane(), "  ", m.renderGraphPane())
	case m.graphVisible():
		main = m.renderGraphPane()
	default:
		main = m.renderChatPane()
	}

	parts := []string{top, "", main, ""}
	if m.paletteOpen() && m.palette != paletteModel {
		parts = append(parts, m.renderPalette())
	}
	if m.activityBarVisible() {
		if bar := m.renderActivityBar(); bar != "" {
			parts = append(parts, bar)
		}
	}
	parts = append(parts, m.renderInput())
	if !m.paletteOpen() {
		hint := "/ commands · tab focus · " + keyBindings.graph + " tasks · v receipts · ? help"
		switch {
		case m.voiceHint != "" && time.Now().Before(m.voiceHintUntil):
			hint = m.voiceHint
		case m.voiceState == voiceRecording:
			hint = keyBindings.voice + " finish · esc discard · keep typing to preserve your draft"
		case m.voiceState == voiceStarting || m.voiceState == voiceFinalizing:
			hint = "voice working · esc discard"
		case m.focus == focusCards:
			hint = "↑/↓ select card · enter details/graph · esc back · " + keyBindings.graph + " all tasks"
		case m.nodeViewID != "":
			hint = "type to steer · enter send · c cancel · esc back"
		case m.focus == focusGraph:
			hint = "↑/↓ select · enter inspect · esc close · " + keyBindings.graph + " hide"
		case m.focus == focusHeader:
			hint = "←/→ choose header control · enter open · esc back"
		case !m.voiceHintShown:
			hint = "/ commands · tab focus · " + keyBindings.voice + " voice · " + keyBindings.graph + " tasks · v receipts · ? help"
		}
		parts = append(parts, mutedStyle.Faint(true).Render(truncate(hint, m.width)))
	}
	frame := lipgloss.JoinVertical(lipgloss.Left, parts...)
	if m.palette == paletteModel {
		frame = m.overlayModelDropdown(frame)
	}
	return frame
}

func (m *Model) trackPaneBounds() {
	m.chatBounds = paneBounds{}
	m.headerTasksBounds = paneBounds{}
	m.headerQuestionBounds = paneBounds{}
	m.headerTalkBounds = paneBounds{}
	m.headerWorkBounds = paneBounds{}
	m.headerVoiceBounds = paneBounds{}
	m.graphBounds = paneBounds{}
	m.graphRowsBounds = paneBounds{}
	m.standingRowsBounds = paneBounds{}
	m.graphToggleBounds = paneBounds{}
	m.paletteCloseBounds = paneBounds{}
	m.modelPickerBounds = paneBounds{}
	m.modelTalkBounds = paneBounds{}
	m.modelWorkBounds = paneBounds{}
	m.modelVoiceBounds = paneBounds{}
	m.modelPickerRows = m.modelPickerRows[:0]
	m.inputBounds = paneBounds{}
	m.textQuestionDismissBounds = paneBounds{}
	m.micBounds = paneBounds{}
	m.voiceCancelBounds = paneBounds{}
	m.nodeBounds = paneBounds{}
	m.nodeTraceBounds = paneBounds{}
	m.activityBarBounds = paneBounds{}

	const mainY = 2
	switch {
	case m.nodeViewID != "":
		m.nodeBounds = paneBounds{x: 0, y: mainY, width: m.width, height: m.chatHeight}
	case m.graphVisible() && m.horizontal:
		m.chatBounds = paneBounds{x: 0, y: mainY, width: m.chatWidth, height: m.chatHeight}
		m.graphBounds = paneBounds{x: m.chatWidth + 2, y: mainY, width: m.graphWidth, height: m.graphHeight}
	case m.graphVisible():
		m.graphBounds = paneBounds{x: 0, y: mainY, width: m.graphWidth, height: m.graphHeight}
	default:
		m.chatBounds = paneBounds{x: 0, y: mainY, width: m.chatWidth, height: m.chatHeight}
	}
	if m.graphBounds.width > 0 {
		standingHeight := m.standingSectionHeight()
		if standingHeight > 0 {
			m.standingRowsBounds = paneBounds{
				x: m.graphBounds.x, y: m.graphBounds.y + 1,
				width: m.graph.Width, height: standingHeight - 2,
			}
		}
		m.graphRowsBounds = paneBounds{
			x: m.graphBounds.x, y: m.graphBounds.y + standingHeight + 2,
			width: m.graph.Width, height: m.graph.Height,
		}
		m.graphToggleBounds = paneBounds{
			x: m.graphBounds.x, y: m.graphBounds.y + standingHeight,
			width: m.graphBounds.width, height: 1,
		}
	}
	paletteY := mainY + m.chatHeight + 1
	if m.paletteHasClose() {
		m.paletteCloseBounds = paneBounds{x: 0, y: paletteY, width: lipgloss.Width("⟨×⟩"), height: 1}
	}

	barY := mainY + m.chatHeight + 1 + m.layoutPaletteHeight()
	if m.activityBarVisible() {
		height := m.cardDockHeight()
		m.activityBarBounds = paneBounds{x: 0, y: barY, width: m.width, height: height}
		barY += height
	}
	m.inputBounds = paneBounds{x: 0, y: barY, width: m.width, height: m.inputSurfaceHeight()}
}

func (m *Model) renderTopBar() string {
	wordmark := lipgloss.NewStyle().Foreground(ink).Bold(true).Render("aforge")
	left := wordmark + mutedStyle.Faint(true).Render("  "+m.sessionID)
	talk := m.renderHeaderModel("talk", 0)
	work := m.renderHeaderModel("work", 1)
	voiceControl := m.renderHeaderModel("voice", 2)
	separator := mutedStyle.Faint(true).Render("  ·  ")
	models := talk + separator + work + separator + voiceControl

	rightMeta := m.renderSpend()
	if m.status != "" && time.Now().Before(m.statusUntil) {
		rightMeta = mutedStyle.Render(truncate(m.status, max(8, m.width/2)))
	}
	if m.err != nil {
		rightMeta = lipgloss.NewStyle().Foreground(rose).Render(truncate(m.err.Error(), max(8, m.width/2)))
	}
	// The rail toggle is a real button: alt+g and /graph are accelerators, the
	// click path is always visible. It follows the affordance grammar (▸ when
	// the rail would open, ▾ while it is on screen).
	button := m.renderTasksButton()
	right := models
	if rightMeta != "" {
		right += "  " + rightMeta
	}
	right += "  " + button

	space := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if space < 1 {
		// The controls outlive the session label when width runs out.
		left = wordmark
		space = m.width - lipgloss.Width(left) - lipgloss.Width(right)
	}
	if space < 1 && rightMeta != "" {
		// …and outlive the transient meta: status and spend are readable
		// elsewhere, the three role controls are the only door to the picker.
		right = models + "  " + button
		space = m.width - lipgloss.Width(left) - lipgloss.Width(right)
	}
	if space < 1 {
		// On a narrow terminal the task button remains the last header action.
		right = button
		space = m.width - lipgloss.Width(left) - lipgloss.Width(right)
	}
	if space < 1 {
		return truncate(left+" "+right, m.width)
	}
	rightX := lipgloss.Width(left) + space
	if strings.Contains(right, "talk") {
		m.headerTalkBounds = paneBounds{x: rightX, y: 0, width: lipgloss.Width(talk), height: 1}
		workX := rightX + lipgloss.Width(talk) + lipgloss.Width(separator)
		m.headerWorkBounds = paneBounds{x: workX, y: 0, width: lipgloss.Width(work), height: 1}
		m.headerVoiceBounds = paneBounds{
			x: workX + lipgloss.Width(work) + lipgloss.Width(separator),
			y: 0, width: lipgloss.Width(voiceControl), height: 1,
		}
	}
	buttonWidth := lipgloss.Width(button)
	m.headerTasksBounds = paneBounds{x: m.width - buttonWidth, y: 0, width: buttonWidth, height: 1}
	if m.hasPendingQuestion() {
		offset := lipgloss.Width("⟨tasks ")
		m.headerQuestionBounds = paneBounds{x: m.headerTasksBounds.x + offset, y: 0, width: 1, height: 1}
	}
	return left + strings.Repeat(" ", space) + right
}

func (m *Model) renderHeaderModel(role string, focusIndex int) string {
	label := mutedStyle.Faint(true).Render(role + " ⌄ ")
	valueStyle := inputTextStyle
	if m.focus == focusHeader && m.headerFocusIndex == focusIndex {
		valueStyle = lipgloss.NewStyle().Foreground(powder).Bold(true)
	}
	return label + valueStyle.Render(truncate(modelShort(m.currentModel(role)), 18))
}

func (m *Model) renderTasksButton() string {
	disclosure := "▸"
	if m.graphVisible() {
		disclosure = "▾"
	}
	dot := ""
	if m.hasPendingQuestion() {
		dot = questionStyle.Bold(true).Render("●") + " "
	}
	style := mutedStyle.Faint(true)
	if m.focus == focusHeader && m.headerFocusIndex == 3 {
		style = lipgloss.NewStyle().Foreground(powder)
	}
	return style.Render("⟨tasks ") + dot + style.Render(disclosure+"⟩")
}

// renderSpend is the one number that is always worth the top-right corner:
// what this session has consumed. Tokens and cost are quiet metadata.
func (m *Model) renderSpend() string {
	if m.usage.Nodes == 0 {
		return ""
	}
	tokens := humanizeTokens(m.usage.PromptTokens + m.usage.CompletionTokens)
	return mutedStyle.Render(tokens + " tok · " + fmt.Sprintf("$%.2f", m.usage.Cost))
}

// taskCounts sweeps the snapshot once for the activity bar and the rail
// header: work in flight, work queued, and anything that failed.
func (m *Model) taskCounts() (running, queued, failed int) {
	definitions := charterDefinitionIDs(m.snapshot)
	for _, node := range m.snapshot.Nodes {
		if node.ID == store.RootID || definitions[node.ID] {
			continue
		}
		switch node.Status {
		case store.Claimed, store.Running:
			running++
		case store.Pending:
			queued++
		case store.Failed:
			failed++
		}
	}
	return running, queued, failed
}

// liveWorkCount is everything still moving: planning placeholders plus
// running workers. It drives the collapsed-rail spinner.
func (m *Model) liveWorkCount() int {
	running, _, _ := m.taskCounts()
	return running + m.planningCount()
}

// renderActivityBar hosts the active-card dock. The legacy aggregate remains
// its quiet empty-state and covers graph-only stores without thread provenance.
func (m *Model) renderActivityBar() string {
	return m.renderCardDock(true)
}

// renderLegacyActivityBar is the graph-only fallback: a spinner when work
// moves, counts only when they are non-zero, and failures in rose. Clicking it
// opens the unscoped rail.
func (m *Model) renderLegacyActivityBar() string {
	running, queued, failed := m.taskCounts()
	planning := m.planningCount()
	segments := make([]string, 0, 4)
	if planning > 0 {
		frame := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
		segments = append(segments, lipgloss.NewStyle().Foreground(peach).Render(
			fmt.Sprintf("%s planning", frame)))
	}
	if running > 0 {
		frame := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
		segments = append(segments, lipgloss.NewStyle().Foreground(peach).Render(
			fmt.Sprintf("%s %d working", frame, running)))
	}
	if queued > 0 {
		segments = append(segments, mutedStyle.Render(fmt.Sprintf("○ %d queued", queued)))
	}
	if failed > 0 {
		segments = append(segments, lipgloss.NewStyle().Foreground(rose).Render(fmt.Sprintf("%d failed", failed)))
	}
	bar := strings.Join(segments, mutedStyle.Render(" · "))
	if bar == "" {
		// Nothing in flight: no dock at all — a quiet screen owes no chrome.
		return ""
	}
	bar += mutedStyle.Faint(true).Render(" — " + keyBindings.graph + " tasks")
	return truncate(bar, m.width)
}

// renderChatPane is the conversation itself, full-bleed: no border, no title,
// just the thread. The unread pill floats over the bottom line when scrolled.
func (m *Model) renderChatPane() string {
	lines := strings.Split(m.chat.View(), "\n")
	for len(lines) < m.chatHeight {
		lines = append(lines, "")
	}
	if len(lines) > m.chatHeight {
		lines = lines[:m.chatHeight]
	}
	// Hard clamp: any line wider than the pane would be soft-wrapped by the
	// Width style below, growing the frame taller than the terminal and
	// letting ghost frames overlap. After the clamp the style only pads.
	clampLines(lines, m.chatWidth)
	if m.newMessages > 0 {
		pill := pillStyle.Render(m.newMessageLabel())
		index := len(lines) - 1
		lines[index] = overlayRight(lines[index], pill, max(1, m.chatWidth-2))
	}
	return lipgloss.NewStyle().Width(m.chatWidth).Render(strings.Join(lines, "\n"))
}

// clampLines truncates, ANSI-aware, every line that exceeds the pane width.
func clampLines(lines []string, width int) {
	for index, line := range lines {
		if lipgloss.Width(line) > width {
			lines[index] = truncate(line, width)
		}
	}
}

// renderGraphPane is the task rail: a faint header naming it, then the tree.
// The header brightens while the rail holds focus.
func (m *Model) renderGraphPane() string {
	label := "tasks"
	if m.graphScopeID != "" {
		label = m.graphScopeID
		if node, ok := m.snapshotNode(m.graphScopeID); ok {
			label = nodeLabelInSnapshot(node, m.cardSnapshot)
		}
		label = "‹ card · " + label
	} else if charter, ok := m.standingCharter(m.charterCardID); ok {
		label = "‹ card · " + charter.Name
	}
	title := mutedStyle.Faint(true).Render(label)
	if m.focus == focusGraph {
		title = lipgloss.NewStyle().Foreground(powder).Render(label)
	}
	title += mutedStyle.Faint(true).Render("  ⟨×⟩")
	lines := make([]string, 0, m.graphHeight)
	if m.graphScopeID == "" && m.charterCardID == "" {
		if section := m.renderStandingSection(max(1, m.graphWidth)); section != "" {
			lines = append(lines, strings.Split(section, "\n")...)
		}
	}
	lines = append(lines, title, "")
	lines = append(lines, strings.Split(m.graph.View(), "\n")...)
	for len(lines) < m.graphHeight {
		lines = append(lines, "")
	}
	if len(lines) > m.graphHeight {
		lines = lines[:m.graphHeight]
	}
	clampLines(lines, m.graphWidth)
	return lipgloss.NewStyle().Width(m.graphWidth).Render(strings.Join(lines, "\n"))
}

// renderNodePane is one task's flight recorder, full-bleed: a header line,
// a faint hairline, the brief, then the scrolling activity feed.
func (m *Model) graphToggleHit(x, y int) bool {
	if m.graphToggleBounds.contains(x, y) {
		return true
	}
	return m.graphBounds.contains(x, y) && x == m.graphBounds.right()-1
}

func (m *Model) renderNodePane() string {
	innerWidth := max(1, m.width-2)
	now := time.Now()
	back := lipgloss.NewStyle().Foreground(powder).Render("‹ back")
	m.nodeBackBounds = paneBounds{x: m.nodeBounds.x, y: m.nodeBounds.y, width: lipgloss.Width(back), height: 1}
	glyph, _ := m.nodeGlyphStyled(m.inspectedNode, now, false)
	title := nodeLabelInSnapshot(m.inspectedNode, m.snapshot)
	timing := m.nodeTiming(now)
	if timing != "" {
		timing = truncate(timing, max(1, innerWidth-lipgloss.Width(glyph)-4))
	}
	timingWidth := 0
	if timing != "" {
		timingWidth = lipgloss.Width("  ·  " + timing)
	}
	title = truncate(title, max(1, innerWidth-lipgloss.Width(glyph)-10-timingWidth))
	header := back + "  " + glyph + " " + lipgloss.NewStyle().Foreground(ink).Bold(true).Render(title)
	if timing != "" {
		header += mutedStyle.Render("  ·  " + timing)
	}

	hairline := mutedStyle.Faint(true).Render(strings.Repeat("─", innerWidth))
	lines := []string{header, hairline}
	contentX := m.nodeBounds.x
	contentY := m.nodeBounds.y + len(lines)
	appendSection := func(label string, view string, height int, bounds *paneBounds) {
		lines = append(lines, mutedStyle.Faint(true).Render(label))
		contentY++
		if height <= 0 {
			return
		}
		*bounds = paneBounds{x: contentX, y: contentY, width: innerWidth, height: height}
		visible := strings.Split(view, "\n")
		if view == "" {
			visible = nil
		}
		for len(visible) < height {
			visible = append(visible, "")
		}
		if len(visible) > height {
			visible = visible[:height]
		}
		lines = append(lines, visible...)
		contentY += height
	}
	appendSection("BRIEF", m.nodeDetailsText, strings.Count(m.nodeDetailsText, "\n")+1, new(paneBounds))
	lines = append(lines, "")
	contentY++
	appendSection("ACTIVITY   ✳ model · $ shell · ✎ file · ⌕ web · › you · ⋯ expands",
		m.nodeTrace.View(), m.nodeTraceHeight, &m.nodeTraceBounds)

	for len(lines) < m.chatHeight {
		lines = append(lines, "")
	}
	if len(lines) > m.chatHeight {
		lines = lines[:m.chatHeight]
	}
	clampLines(lines, m.width)
	return lipgloss.NewStyle().Width(m.width).Render(strings.Join(lines, "\n"))
}

// renderInput keeps editable text owned by textinput while laying the pending
// transcript beside it as non-editable, visibly provisional ink. The mic is
// overlaid at the right edge after ANSI-aware clipping, so neither a long
// draft nor a live transcript can widen the pane. A text question adds one
// quiet context line above all of that without changing what enter submits —
// the provisional transcript and the "answering:" line never share a row.
func (m *Model) renderInput() string {
	input := m.input
	if strings.TrimSpace(m.voicePending) != "" {
		input.Placeholder = ""
	}
	lines := strings.Split(input.View(), "\n")
	last := len(lines) - 1
	if strings.TrimSpace(m.voicePending) != "" {
		lines[last] += mutedStyle.Faint(true).Italic(true).Render(" " + m.voicePending)
	}
	control := m.voiceControl()
	for index := range lines {
		if index == last {
			lines[index] = overlayRight(lines[index], control, m.width)
		} else {
			lines[index] = truncate(lines[index], m.width)
		}
	}
	card := m.activeTextQuestion()
	inputY := m.inputBounds.y
	if card != nil {
		inputY++ // the question context line owns the input surface's first row
	}
	// Attachment chips sit between the question line and the editable input,
	// each with its own dismiss target; an extra hint row warns when the talk
	// model cannot see images.
	m.attachmentBounds = m.attachmentBounds[:0]
	chipLines := make([]string, 0, len(m.attachments)+1)
	for index, path := range m.attachments {
		prefix := "⌾ " + truncate(filepath.Base(path), max(1, m.width-lipgloss.Width("⌾  ⟨×⟩"))) + " "
		line := mutedStyle.Faint(true).Render(prefix + "⟨×⟩")
		chipLines = append(chipLines, line)
		m.attachmentBounds = append(m.attachmentBounds, paneBounds{
			x: lipgloss.Width(prefix), y: inputY + index, width: lipgloss.Width("⟨×⟩"), height: 1,
		})
	}
	if len(m.attachments) > 0 {
		if model, supported := m.imageInputSupport(); !supported {
			hint := truncate(model+" can't see images — try a vision model", m.width)
			chipLines = append(chipLines, mutedStyle.Faint(true).Render(hint))
		}
	}
	inputY += len(chipLines)
	controlWidth := lipgloss.Width(control)
	controlX := max(0, m.width-controlWidth)
	micWidth := lipgloss.Width(m.voiceMicGlyph())
	m.micBounds = paneBounds{
		x: controlX, y: inputY + last,
		width: micWidth, height: 1,
	}
	if m.voiceState != voiceIdle {
		cancelWidth := lipgloss.Width("⟨×⟩")
		m.voiceCancelBounds = paneBounds{
			x: m.width - cancelWidth, y: inputY + last,
			width: cancelWidth, height: 1,
		}
	}
	rendered := lipgloss.NewStyle().PaddingLeft(0).Width(m.width).Render(
		strings.Join(append(chipLines, lines...), "\n"))
	if card == nil {
		return rendered
	}
	close := "⟨×⟩"
	prefix := "answering: "
	available := max(1, m.width-lipgloss.Width(prefix)-lipgloss.Width(close)-3)
	line := mutedStyle.Faint(true).Render(prefix+truncate(card.Question, available)+" · ") +
		mutedStyle.Render(close)
	m.textQuestionDismissBounds = paneBounds{
		x: lipgloss.Width(line) - lipgloss.Width(close), y: m.inputBounds.y,
		width: lipgloss.Width(close), height: 1,
	}
	return line + "\n" + rendered
}

// overlayModelDropdown paints a small matte menu over the first rows beneath
// the header, anchored under whichever role control opened it. The frame keeps
// its exact height; the dropdown does not steal conversation space or move the
// input while the user searches.
func (m *Model) overlayModelDropdown(frame string) string {
	width := min(72, max(36, m.width*2/3))
	x := m.headerTalkBounds.x
	if m.modelRole == "work" && m.headerWorkBounds.width > 0 {
		x = m.headerWorkBounds.x
	}
	if m.modelRole == "voice" && m.headerVoiceBounds.width > 0 {
		x = m.headerVoiceBounds.x
	}
	if x == 0 && m.headerTalkBounds.width == 0 {
		x = max(0, m.width-width)
	}
	x = max(0, min(x, m.width-width))
	y := 1
	innerWidth := max(1, width-2)
	body := m.modelPickerLines(innerWidth)
	lines := append([]string{"⟨×⟩ esc · models"}, body...)
	panelStyle := lipgloss.NewStyle().Foreground(ink).Background(selectionBand).Padding(0, 1).Width(width)
	for index := range lines {
		lines[index] = panelStyle.Render(truncate(lines[index], innerWidth))
	}
	panel := strings.Join(lines, "\n")
	m.modelPickerBounds = paneBounds{x: x, y: y, width: width, height: len(lines)}
	m.paletteCloseBounds = paneBounds{x: x, y: y, width: width, height: 1}
	m.modelTalkBounds = paneBounds{x: x + 1, y: y + 1, width: lipgloss.Width("◉ talk"), height: 1}
	m.modelWorkBounds = paneBounds{x: x + 1 + lipgloss.Width("◉ talk    "), y: y + 1, width: lipgloss.Width("◉ work"), height: 1}
	m.modelVoiceBounds = paneBounds{x: x + 1 + lipgloss.Width("◉ talk    ◉ work    "), y: y + 1, width: lipgloss.Width("◉ voice"), height: 1}

	choiceLine := y + 1 + 2
	prefixLines := 2
	if m.modelCatalogIsLoading(m.modelRole) {
		choiceLine++
		prefixLines++
	}
	choices := m.filteredModelChoices()
	rowLimit := max(1, min(8, m.paletteLineLimit()-prefixLines))
	start, end := visiblePaletteWindow(m.paletteSelected, len(choices), rowLimit)
	for index := start; index < end; index++ {
		m.modelPickerRows = append(m.modelPickerRows, modelPickerRow{
			bounds: paneBounds{x: x, y: choiceLine + index - start, width: width, height: 1},
			index:  index,
		})
	}
	return overlayBlock(frame, panel, x, y, m.width)
}

func overlayBlock(base, overlay string, x, y, width int) string {
	baseLines := strings.Split(base, "\n")
	for index, overlayLine := range strings.Split(overlay, "\n") {
		at := y + index
		if at < 0 || at >= len(baseLines) {
			continue
		}
		left := ansi.Cut(baseLines[at], 0, x)
		if gap := x - lipgloss.Width(left); gap > 0 {
			left += strings.Repeat(" ", gap)
		}
		overlayWidth := min(lipgloss.Width(overlayLine), max(0, width-x))
		right := ansi.Cut(baseLines[at], x+overlayWidth, width)
		baseLines[at] = truncate(left+truncate(overlayLine, overlayWidth)+right, width)
	}
	return strings.Join(baseLines, "\n")
}

func (m *Model) renderPalette() string {
	innerWidth := max(1, m.width-2)
	lines := m.paletteLines(innerWidth)
	if m.paletteHasClose() {
		header := truncate("⟨×⟩ esc · "+m.paletteTitle(), innerWidth)
		lines = append([]string{mutedStyle.Faint(true).Render(header)}, lines...)
	}
	return strings.Join(lines, "\n")
}

func (m *Model) paletteHasClose() bool {
	return m.palette == paletteModel || m.palette == paletteMemory || m.palette == paletteHelp
}

func (m *Model) paletteTitle() string {
	switch m.palette {
	case paletteModel:
		return "models"
	case paletteMemory:
		return "notebook"
	case paletteHelp:
		return "help"
	default:
		return ""
	}
}

func (m *Model) paletteHeight() int {
	if !m.paletteOpen() {
		return 0
	}
	return lipgloss.Height(m.renderPalette())
}

func (m *Model) layoutPaletteHeight() int {
	if m.palette == paletteModel {
		return 0
	}
	return m.paletteHeight()
}

func (m *Model) paletteLines(width int) []string {
	switch m.palette {
	case paletteCommands:
		entries := m.commandEntries()
		start, end := visiblePaletteWindow(m.paletteSelected, len(entries), m.paletteLineLimit())
		lines := make([]string, 0, end-start)
		for index := start; index < end; index++ {
			entry := entries[index]
			lines = append(lines, m.paletteRow(
				fmt.Sprintf("「/%s」 %s", entry.value, entry.description), index == m.paletteSelected, width,
			))
		}
		if strings.TrimSpace(m.input.Value()) == "/" {
			hint := truncate("/graph · /tasks · /node · /notebook · /help · type to filter", width)
			return []string{mutedStyle.Faint(true).Render(hint)}
		}
		if len(lines) == 0 {
			return []string{mutedStyle.Render("no matching command")}
		}
		return lines
	case paletteModelCompletion:
		return m.completionLines(m.modelEntries(), "models", width)
	case paletteCancelCompletion:
		return m.completionLines(m.cancelEntries(), "non-terminal nodes", width)
	case paletteModel:
		return m.modelPickerLines(width)
	case paletteMemory:
		return m.memoryPanelLines(width)
	case paletteHelp:
		lines := make([]string, 0, len(slashCommands)+5)
		for _, command := range slashCommands {
			lines = append(lines, truncate(fmt.Sprintf("「/%s」 %s", command.name, command.description), width))
		}
		// Every action lists its key and its click path: chords are
		// accelerators, never the only door in.
		lines = append(lines,
			mutedStyle.Render(truncate("voice  you ask · aforge answers · v toggles receipts (or click their ▸ line)", width)),
			mutedStyle.Render(truncate("mic    "+keyBindings.voice+" starts/stops voice · click ◌ at the input edge · esc discards", width)),
			mutedStyle.Render(truncate("tasks  "+keyBindings.graph+" toggles the rail · or click ⟨tasks ▸⟩ in the header · or /graph", width)),
			mutedStyle.Render(truncate("rail   ↑/↓ select · enter inspect (or click a row twice) · esc closes", width)),
			mutedStyle.Render(truncate("cards  tab or click the dock · enter expands then opens its job · esc climbs back", width)),
			mutedStyle.Render(truncate("chat   ↳ chips jump to the task · tab focuses the thread · ↑/↓ walk lines · enter = click", width)),
			mutedStyle.Render(truncate("node   type guidance + enter to steer · c cancels worker · ‹ back or esc returns", width)),
			mutedStyle.Render(truncate("mouse  click focus/select/open · wheel scrolls pointed pane", width)),
			mutedStyle.Render(truncate("menus  tab/↑/↓ choose · enter accept · esc close · ctrl+c quit", width)),
		)
		if limit := m.paletteLineLimit(); len(lines) > limit {
			lines = lines[:limit]
		}
		return lines
	default:
		return nil
	}
}

func (m *Model) memoryPanelLines(width int) []string {
	if len(m.memoryFacts) == 0 {
		return []string{mutedStyle.Render("notebook is empty")}
	}

	order := make([]string, 0)
	groups := make(map[string][]store.Fact)
	for _, fact := range m.memoryFacts {
		scope := strings.TrimSpace(fact.Scope)
		if _, ok := groups[scope]; !ok {
			order = append(order, scope)
		}
		groups[scope] = append(groups[scope], fact)
	}
	lines := make([]string, 0, m.memoryLineCount())
	for _, scope := range order {
		lines = append(lines, mutedStyle.Faint(true).Render(truncate(scope, width)))
		for _, fact := range groups[scope] {
			lines = append(lines, memoryFactRow(fact, width))
		}
	}

	limit := m.paletteLineLimit()
	start := min(m.paletteSelected, max(0, len(lines)-limit))
	end := min(len(lines), start+limit)
	return lines[start:end]
}

func memoryFactRow(fact store.Fact, width int) string {
	glyph := "·"
	style := mutedStyle
	switch fact.Kind {
	case store.FactPreference:
		glyph = "◆"
		style = lipgloss.NewStyle().Foreground(powder)
	case store.FactQuirk:
		glyph = "▲"
		style = lipgloss.NewStyle().Foreground(peach)
	case store.FactLesson:
		glyph = "●"
		style = lipgloss.NewStyle().Foreground(mint)
	}
	body := truncate(strings.TrimSpace(fact.Body), max(1, width-lipgloss.Width(glyph)-1))
	return style.Render(glyph) + " " + inputTextStyle.Render(body)
}

func (m *Model) completionLines(entries []paletteEntry, label string, width int) []string {
	if len(entries) == 0 {
		return []string{mutedStyle.Render("no matching " + label)}
	}
	start, end := visiblePaletteWindow(m.paletteSelected, len(entries), min(6, m.paletteLineLimit()))
	lines := make([]string, 0, end-start)
	for index := start; index < end; index++ {
		lines = append(lines, m.paletteRow(entries[index].value, index == m.paletteSelected, width))
	}
	return lines
}

func (m *Model) modelPickerLines(width int) []string {
	talk, work, voiceRole := "○ talk", "○ work", "○ voice"
	if m.modelRole == "talk" {
		talk = "◉ talk"
	} else if m.modelRole == "work" {
		work = "◉ work"
	} else {
		voiceRole = "◉ voice"
	}
	talkView := mutedStyle.Render(talk)
	workView := mutedStyle.Render(work)
	voiceView := mutedStyle.Render(voiceRole)
	if m.modelRole == "talk" {
		talkView = lipgloss.NewStyle().Foreground(powder).Bold(true).Render(talk)
	} else if m.modelRole == "work" {
		workView = lipgloss.NewStyle().Foreground(powder).Bold(true).Render(work)
	} else {
		voiceView = lipgloss.NewStyle().Foreground(mint).Bold(true).Render(voiceRole)
	}
	lines := []string{
		talkView + mutedStyle.Render("    ") + workView + mutedStyle.Render("    ") + voiceView,
		mutedStyle.Render("filter: ") + inputTextStyle.Render(m.input.Value()),
	}
	if m.modelCatalogIsLoading(m.modelRole) {
		lines = append(lines, mutedStyle.Render("fetching full catalog…"))
	}

	choices := m.filteredModelChoices()
	if len(choices) == 0 {
		empty := "no models configured"
		if strings.TrimSpace(m.input.Value()) != "" {
			empty = "no matching models"
		}
		return append(lines, mutedStyle.Render(empty))
	}
	rowLimit := max(1, min(8, m.paletteLineLimit()-len(lines)))
	start, end := visiblePaletteWindow(m.paletteSelected, len(choices), rowLimit)
	for index := start; index < end; index++ {
		lines = append(lines, m.modelChoiceRow(choices[index], index == m.paletteSelected, width))
	}
	return lines
}

func (m *Model) paletteLineLimit() int {
	const minimumMainHeight = 3
	// top bar + blank + main + blank + palette + input; the hint yields while
	// the palette is open.
	available := m.height - 3 - m.inputSurfaceHeight() - minimumMainHeight
	if m.paletteHasClose() {
		available--
	}
	return max(1, available)
}

func (m *Model) modelChoiceRow(choice ModelChoice, selected bool, width int) string {
	marker := "  "
	markerStyle := mutedStyle
	if m.currentModel(m.modelRole) == choice.Slug {
		marker = "● "
		markerStyle = lipgloss.NewStyle().Foreground(mint)
	}
	if selected {
		marker = "› "
		markerStyle = lipgloss.NewStyle().Foreground(powder).Bold(true)
	}

	detail := choice.Name
	if detail == choice.Slug {
		detail = ""
	}
	if choice.Price != "" {
		if detail != "" {
			detail += " · "
		}
		detail += choice.Price
	}

	available := max(1, width-lipgloss.Width(marker))
	slug := truncate(choice.Slug, available)
	detailWidth := available - lipgloss.Width(slug) - 2
	if detailWidth > 0 && detail != "" {
		detail = truncate(detail, detailWidth)
	} else {
		detail = ""
	}
	accent := powder
	if m.modelRole == "work" {
		accent = peach
	} else if m.modelRole == "voice" {
		accent = mint
	}
	slugStyle := lipgloss.NewStyle().Foreground(accent).Bold(selected)
	row := markerStyle.Render(marker) + slugStyle.Render(slug)
	if detail != "" {
		row += mutedStyle.Faint(true).Render("  " + detail)
	}
	return row
}

func (m *Model) paletteRow(text string, selected bool, width int) string {
	text = truncate(text, width)
	if selected {
		return selectedStyle.Width(width).Render(text)
	}
	return text
}

func visiblePaletteWindow(selected, count, limit int) (int, int) {
	if count <= limit {
		return 0, count
	}
	start := max(0, selected-limit/2)
	start = min(start, count-limit)
	return start, start + limit
}

func (m *Model) currentModel(role string) string {
	if model := strings.TrimSpace(m.optimisticModels[role]); model != "" {
		return model
	}
	if m.commander == nil {
		return "–"
	}
	return m.commander.CurrentModel(role)
}

func modelShort(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return "–"
	}
	if index := strings.LastIndex(model, "/"); index >= 0 && index+1 < len(model) {
		return model[index+1:]
	}
	return model
}

func (m *Model) newMessageLabel() string {
	if m.newMessages > 1 {
		return fmt.Sprintf("↓ %d new messages", m.newMessages)
	}
	return "↓ new messages"
}

func overlayRight(line, overlay string, width int) string {
	overlayWidth := lipgloss.Width(overlay)
	if overlayWidth >= width {
		return truncate(overlay, width)
	}
	line = truncate(line, width-overlayWidth-1)
	gap := max(1, width-lipgloss.Width(line)-overlayWidth)
	return line + strings.Repeat(" ", gap) + overlay
}

// chatMessageRow maps a span of rendered chat lines to the message shown
// there, so a click on a collapsed answer opens it in place.
type chatMessageRow struct {
	start int
	end   int
	seq   int64
}

type chatExpandAction uint8

const (
	chatExpandMessage chatExpandAction = iota
	chatExpandReceipts
)

// chatExpandRow is one explicit disclosure line in the thread. Keeping these
// separate from message spans makes receipt labels clickable without changing
// the existing click-anywhere behavior of folded deliverables.
type chatExpandRow struct {
	line   int
	action chatExpandAction
	seq    int64
}

// chatChipRow maps a rendered provenance-chip line (`↳ title`) to the task
// node it came from, so clicking it opens that task's activity view.
type chatChipRow struct {
	line   int
	nodeID string
}

type threadRenderItem struct {
	order   int64
	index   int
	message store.Message
	card    jobCard
	isCard  bool
}

func (m *Model) renderMessages() string {
	m.chatMessageRows = m.chatMessageRows[:0]
	m.chatExpandRows = m.chatExpandRows[:0]
	m.chatChipRows = m.chatChipRows[:0]
	m.chatCardRows = m.chatCardRows[:0]
	kept := m.cardPartRows[:0]
	for _, row := range m.cardPartRows {
		if row.dock {
			kept = append(kept, row)
		}
	}
	m.cardPartRows = kept
	keptOptions := m.cardOptionRows[:0]
	for _, row := range m.cardOptionRows {
		if row.dock {
			keptOptions = append(keptOptions, row)
		}
	}
	m.cardOptionRows = keptOptions
	keptClose := m.cardCloseRows[:0]
	for _, row := range m.cardCloseRows {
		if row.dock {
			keptClose = append(keptClose, row)
		}
	}
	m.cardCloseRows = keptClose

	items := make([]threadRenderItem, 0, len(m.messages)+len(m.cards))
	for index, message := range m.messages {
		if !m.streamMessage(message) {
			continue
		}
		order := message.Seq
		if order == 0 {
			order = int64(index - len(m.messages) - 1)
		}
		items = append(items, threadRenderItem{order: order, index: index, message: message})
	}
	_, settled := placeJobCards(m.cards)
	for index, card := range settled {
		items = append(items, threadRenderItem{
			order: card.BirthSeq, index: len(m.messages) + index, card: card, isCard: true,
		})
	}
	if message, ok := m.streamingMessage(); ok {
		items = append(items, threadRenderItem{
			order: m.lastSeq + 1, index: len(m.messages) + len(m.cards), message: message,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].order == items[j].order {
			return items[i].index < items[j].index
		}
		return items[i].order < items[j].order
	})
	blocks := make([]string, 0, len(items)+1)
	if len(items) == 0 {
		// Wrapped, not raw: a line wider than the pane would be soft-wrapped by
		// the pane style, growing the frame and shifting every row below it.
		blocks = append(blocks, mutedStyle.Render(wrapText("No messages yet. Start with a thought or a task.", max(8, m.chat.Width-2))))
	}
	line := 0
	appendBlock := func(block string) {
		blocks = append(blocks, block)
		line += lipgloss.Height(block) + 1
	}
	var group messageGroup
	flushGroup := func() {
		if len(group.messages) == 0 {
			return
		}
		block := m.renderMessageGroup(group, line)
		appendBlock(block)
		group = messageGroup{}
	}
	for _, item := range items {
		if item.isCard {
			flushGroup()
			block := m.renderJobCard(item.card, max(8, m.chat.Width-2),
				m.cardExpanded[item.card.ID], line, false, true)
			m.chatCardRows = append(m.chatCardRows, cardRow{
				start: line, end: line + lipgloss.Height(block) - 1, cardID: item.card.ID,
			})
			appendBlock(block)
			continue
		}
		voice := messageVoice(item.message)
		if len(group.messages) > 0 &&
			(group.voice != voice || messageGap(group.messages[len(group.messages)-1], item.message) > messageGroupWindow) {
			flushGroup()
		}
		if len(group.messages) == 0 {
			group.voice = voice
		}
		group.messages = append(group.messages, item.message)
	}
	if shimmer := m.renderShimmerLines(max(1, m.chat.Width-2)); shimmer != "" {
		flushGroup()
		appendBlock(shimmer)
	}
	flushGroup()
	return m.applyChatFocus(strings.Join(blocks, "\n\n"))
}

// chatFocusLines lists, in order, every thread line a click would activate:
// receipts, folds, provenance chips, card headers, part rows, and close rows.
func (m *Model) chatFocusLines() []int {
	seen := make(map[int]bool)
	for _, row := range m.chatExpandRows {
		seen[row.line] = true
	}
	for _, row := range m.chatChipRows {
		seen[row.line] = true
	}
	for _, row := range m.chatCardRows {
		seen[row.start] = true
	}
	for _, row := range m.cardPartRows {
		if !row.dock {
			seen[row.line] = true
		}
	}
	for _, row := range m.cardCloseRows {
		if !row.dock {
			seen[row.line] = true
		}
	}
	lines := make([]int, 0, len(seen))
	for line := range seen {
		lines = append(lines, line)
	}
	sort.Ints(lines)
	return lines
}

// applyChatFocus paints the focused interactive line with the same selection
// band the rail uses, so keyboard users see the exact target a click would hit.
func (m *Model) applyChatFocus(content string) string {
	if m.focus != focusChat {
		return content
	}
	targets := m.chatFocusLines()
	if len(targets) == 0 {
		return content
	}
	m.chatFocusIndex = max(0, min(m.chatFocusIndex, len(targets)-1))
	line := targets[m.chatFocusIndex]
	rows := strings.Split(content, "\n")
	if line < 0 || line >= len(rows) {
		return content
	}
	rows[line] = lipgloss.NewStyle().Background(selectionBand).
		Width(max(1, m.chat.Width)).Render(rows[line])
	return strings.Join(rows, "\n")
}

const messageGroupWindow = 3 * time.Minute

type messageGroup struct {
	messages []store.Message
	voice    string
}

func groupMessages(messages []store.Message) []messageGroup {
	groups := make([]messageGroup, 0, len(messages))
	for _, message := range messages {
		if message.Role == store.RoleUser && message.NodeID != "" {
			continue
		}
		voice := messageVoice(message)
		startGroup := len(groups) == 0
		if !startGroup {
			latest := groups[len(groups)-1]
			startGroup = latest.voice != voice ||
				messageGap(latest.messages[len(latest.messages)-1], message) > messageGroupWindow
		}
		if startGroup {
			groups = append(groups, messageGroup{voice: voice})
		}
		groups[len(groups)-1].messages = append(groups[len(groups)-1].messages, message)
	}
	return groups
}

func messageGap(previous, next store.Message) time.Duration {
	if previous.Time.IsZero() || next.Time.IsZero() || !next.Time.After(previous.Time) {
		return 0
	}
	return next.Time.Sub(previous.Time)
}

// deliverableLead is how much of a long answer shows before the ⋯. The lead
// carries the answer itself — workers are prompted to put the conclusion
// first — and the full detail is one click away.
const deliverableLead = 14

func (m *Model) renderMessageGroup(group messageGroup, atLine int) string {
	latest := group.messages[len(group.messages)-1]
	available := max(8, m.chat.Width-2)
	header := speakerHeader(latest, time.Now())

	// One voice: every conversational body reads in primary ink with the same
	// markdown treatment. Headers and provenance remain quiet metadata.
	line := atLine + 1
	items := make([]string, 0, len(group.messages))
	for _, message := range group.messages {
		var item string
		var foldedAnswer bool
		receipt := false
		switch {
		case secondaryMessage(message):
			item = m.renderReceipt(message, available)
			receipt = true
		case message.Role == store.RoleUser:
			item = youTextStyle.Render(wrapText(message.Body, available))
		default:
			item, foldedAnswer = m.renderAnswerFold(message, available)
		}
		if artifacts := m.renderMediaArtifacts(message, available); artifacts != "" {
			if item != "" {
				item += "\n"
			}
			item += artifacts
		}
		// A task-anchored answer names its origin: a small clickable chip that
		// jumps to that task's activity view.
		if message.NodeID != "" && message.Role != store.RoleUser && !secondaryMessage(message) {
			chip := mutedStyle.Faint(true).Render(
				"↳ " + truncate(m.nodeChipLabel(message.NodeID), max(6, min(40, available-2))))
			m.chatChipRows = append(m.chatChipRows, chatChipRow{line: line, nodeID: message.NodeID})
			item = chip + "\n" + item
		}
		items = append(items, item)
		height := lipgloss.Height(item)
		if receipt {
			m.chatExpandRows = append(m.chatExpandRows, chatExpandRow{
				line: line, action: chatExpandReceipts,
			})
		}
		if foldedAnswer {
			m.chatExpandRows = append(m.chatExpandRows, chatExpandRow{
				line: line + height - 1, action: chatExpandMessage, seq: message.Seq,
			})
		}
		if message.Seq != 0 {
			m.chatMessageRows = append(m.chatMessageRows, chatMessageRow{start: line, end: line + height - 1, seq: message.Seq})
		}
		line += height + 1
	}
	content := header
	if len(items) > 0 {
		content += "\n" + strings.Join(items, "\n\n")
	}
	return content
}

// nodeChipLabel is the short human name for a task referenced from chat: the
// node's title while it is visible in the snapshot, its id once folded away.
func (m *Model) nodeChipLabel(nodeID string) string {
	if node, ok := m.snapshotNode(nodeID); ok {
		return nodeLabelInSnapshot(node, m.snapshot)
	}
	return nodeID
}

// renderAnswer presents an agent-side message the way a person reads one:
// markdown styled, file paths clickable, long answers led by their opening
// with the rest one click away, and arrivals paced token by token.
func (m *Model) renderAnswer(message store.Message, width int) string {
	rendered, _ := m.renderAnswerFold(message, width)
	return rendered
}

// renderAnswerFold reports, alongside the rendered answer, whether its tail is
// folded behind a click target.
func (m *Model) renderAnswerFold(message store.Message, width int) (string, bool) {
	body := message.Body
	if message.Role == store.RoleAgent {
		if component, ok := readQuestionComponent(body); ok {
			body = component.Prompt
		}
	}
	streaming := message.Seq == 0 && m.streamMode == streamReal
	if shown, ok := m.streamedBody(message); ok {
		body, streaming = shown, true
	}
	rendered := renderMarkdown(body, width)
	if streaming {
		if rendered != "" {
			rendered += "\n"
		}
		return rendered + lipgloss.NewStyle().Foreground(powder).Render("▌"), false
	}
	lines := strings.Split(rendered, "\n")
	if len(lines) <= deliverableLead+4 {
		return rendered, false
	}
	if m.expandedMessages[message.Seq] {
		// The affordance flips with state: an opened fold shows how to close.
		return rendered + "\n" + mutedStyle.Faint(true).Render("▾ collapse"), true
	}
	head := strings.Join(lines[:deliverableLead], "\n")
	return head + "\n" + mutedStyle.Faint(true).Render(
		fmt.Sprintf("▸ %d more lines", len(lines)-deliverableLead)), true
}

func (m *Model) renderReceipt(message store.Message, width int) string {
	summary := receiptSummary(message)
	if !m.receiptsExpanded {
		return mutedStyle.Render(truncate("▸ "+summary, width))
	}
	label := mutedStyle.Render(truncate("▾ "+summary, width))
	body := wrapText(message.Body, max(1, width-2))
	return label + "\n" + mutedStyle.Render(indentLines(body, "  "))
}

func messagePresentation(message store.Message) (lipgloss.AdaptiveColor, string) {
	if message.Role == store.RoleUser {
		return muted, "you"
	}
	return lavender, "aforge"
}

// speakerHeader renders the one-line attribution above a message group in the
// voice hierarchy: aforge in the lavender accent, you dim. The timestamp is
// ambient either way.
func speakerHeader(message store.Message, now time.Time) string {
	_, label := messagePresentation(message)
	style := aforgeLabelStyle
	if label == "you" {
		style = youLabelStyle
	}
	return style.Render(label) + mutedStyle.Faint(true).Render("  "+relativeTime(message.Time, now))
}

func messageVoice(message store.Message) string {
	_, label := messagePresentation(message)
	return label
}

func secondaryMessage(message store.Message) bool {
	return message.Role == store.RoleSystem && message.NodeID == ""
}

func receiptSummary(message store.Message) string {
	if message.CommandSeq != 0 {
		assumptions := 0
		for _, line := range strings.Split(strings.ReplaceAll(message.Body, "\r\n", "\n"), "\n") {
			if strings.HasPrefix(line, "Assumed:") {
				assumptions++
			}
		}
		return fmt.Sprintf("reading + %d assumptions", assumptions)
	}
	label := firstLine(message.Body)
	if label == "" {
		label = "update"
	}
	return label
}

func indentLines(text, prefix string) string {
	return prefix + strings.ReplaceAll(text, "\n", "\n"+prefix)
}

const railHistoryLimit = 5

func (m *Model) renderTree(width, height int) string {
	snapshot := m.snapshot
	if m.graphScopeID != "" {
		snapshot = m.cardSnapshot
	}
	now := time.Now()
	m.graphAnimating = false
	m.graphRows = nil
	lines := make([]string, 0, len(snapshot.Nodes)+len(m.pending))
	for _, command := range m.pending {
		if m.graphScopeID != "" {
			continue
		}
		if command.Kind != store.CommandSplice {
			continue
		}
		row := len(lines)
		frame := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
		instruction := truncate(firstLine(command.Instruction), 30)
		elapsed := "0s elapsed"
		if !command.Time.IsZero() {
			elapsed = formatElapsed(now.Sub(command.Time)) + " elapsed"
		}
		label := "planning…"
		if instruction != "" {
			label += " " + instruction
		}
		label += " · " + elapsed
		prefix := lipgloss.NewStyle().Foreground(peach).Render(frame) + " "
		lines = append(lines, prefix+inputTextStyle.Render(truncate(label, max(1, width-lipgloss.Width(prefix)))))
		m.noteAnimatedGraphRow(row)
	}

	definitions := charterDefinitionIDs(snapshot)
	children := make(map[string][]store.Node, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		if node.ID == store.RootID || definitions[node.ID] {
			continue
		}
		children[node.Parent] = append(children[node.Parent], node)
	}
	// Live work reads top-down: jobs still moving sit first, newest first, so
	// the eye lands on what is happening now; everything settled sinks below
	// and renders dimmed.
	var roots []store.Node
	if m.graphScopeID != "" {
		for _, node := range snapshot.Nodes {
			if node.ID == m.graphScopeID {
				roots = []store.Node{node}
				break
			}
		}
	} else {
		roots = orderRoots(children[store.RootID], children)
	}
	historyCount := 0
	if m.graphScopeID == "" {
		roots, historyCount = visibleRootHistory(roots, children, m.historyExpanded)
	}

	// Dependency edges are the pipeline structure the tree cannot draw, so
	// they surface two ways: a hollow dotted glyph for work that is queued
	// but waiting on another node, and a "waits:" line under the selection.
	nodeByID := make(map[string]store.Node, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		if !definitions[node.ID] {
			nodeByID[node.ID] = node
		}
	}
	jobRoots := nodeJobRoots(snapshot.Nodes)
	waitsOn := make(map[string][]string)
	for _, edge := range snapshot.Edges {
		source, ok := nodeByID[edge.From]
		if ok && !nodeSettled(source) {
			waitsOn[edge.To] = append(waitsOn[edge.To], edge.From)
		}
	}
	if len(roots) == 0 && len(lines) == 0 {
		empty := truncate("the graph is quiet — ask for something", max(1, width))
		return lipgloss.Place(
			max(1, width),
			max(1, height),
			lipgloss.Center,
			lipgloss.Center,
			mutedStyle.Render(empty),
		)
	}

	seen := make(map[string]bool, len(snapshot.Nodes))
	var walk func([]store.Node, string)
	walk = func(nodes []store.Node, ancestorGuide string) {
		lastGroup := ""
		groupLabeled := true
		for index, node := range nodes {
			if seen[node.ID] {
				continue
			}
			seen[node.ID] = true
			last := index == len(nodes)-1
			branch := "├─ "
			nextGuide := ancestorGuide + "│  "
			if last {
				branch = "╰─ "
				nextGuide = ancestorGuide + "   "
			}

			// A change of planning container gets a label line: the nesting
			// the planner built survives here even though execution flattened
			// it to edges. The label is scoped to a contiguous run of rows: once
			// another subtree's rows have rendered in between (top-level jobs
			// are siblings, so a whole other job can sit there), the group is
			// announced again rather than left bleeding over foreign rows.
			if node.Group != lastGroup || !groupLabeled {
				lastGroup = node.Group
				groupLabeled = true
				if node.Group != "" {
					header := "  " + ancestorGuide + "┄ " + node.Group
					lines = append(lines, mutedStyle.Faint(true).Render(truncate(header, max(1, width))))
				}
			}

			row := len(lines)
			dimmed := subtreeSettled(node, children) && !m.completionFlashing(node, now)
			glyph, active := m.nodeGlyphStyled(node, now, dimmed)
			waiting := waitsOn[node.ID]
			if node.Status == store.Pending && len(waiting) > 0 && !dimmed {
				glyph = mutedStyle.Render("◌")
			}
			selected := node.ID == m.selectedNodeID
			marker := "  "
			if selected {
				marker = lipgloss.NewStyle().Foreground(powder).Bold(true).Render("▸ ")
			}
			prefix := marker + mutedStyle.Render(ancestorGuide+branch) + glyph + " "
			label := nodeLabel(node, jobRoots[node.ID])
			labelStyle := lipgloss.NewStyle().Foreground(ink)
			if dimmed {
				labelStyle = mutedStyle
			}
			line := prefix + labelStyle.Render(
				truncate(label, max(1, width-lipgloss.Width(prefix))),
			)
			if selected {
				line = lipgloss.NewStyle().Background(selectionBand).Width(width).Render(line)
			}
			lines = append(lines, line)
			m.graphRows = append(m.graphRows, graphRow{line: row, nodeID: node.ID})
			if active || m.completionFlashing(node, now) {
				m.noteAnimatedGraphRow(row)
			}

			if active && !node.StartedAt.IsZero() {
				elapsedPrefix := "  " + nextGuide + "   "
				elapsed := formatElapsed(now.Sub(node.StartedAt)) + " elapsed"
				lines = append(lines, mutedStyle.Render(elapsedPrefix+truncate(elapsed, max(1, width-lipgloss.Width(elapsedPrefix)))))
			}
			if selected && len(waiting) > 0 {
				names := make([]string, 0, len(waiting))
				for _, id := range waiting {
					names = append(names, nodeLabel(nodeByID[id], jobRoots[id]))
				}
				waitPrefix := "  " + nextGuide + "   "
				waits := "waits: " + strings.Join(names, " · ")
				lines = append(lines, mutedStyle.Render(waitPrefix+truncate(waits, max(1, width-lipgloss.Width(waitPrefix)))))
			}
			if descendants := children[node.ID]; len(descendants) > 0 {
				walk(descendants, nextGuide)
				groupLabeled = false
			}
		}
	}
	walk(roots, "")
	if historyCount > 0 {
		row := len(lines)
		selected := m.selectedNodeID == historyGraphRowID
		marker := "  "
		if selected {
			marker = lipgloss.NewStyle().Foreground(lavender).Bold(true).Render("▸ ")
		}
		disclosure := "▸"
		if m.historyExpanded {
			disclosure = "▾"
		}
		line := marker + mutedStyle.Faint(true).Render(fmt.Sprintf("%s history (%d)", disclosure, historyCount))
		if selected {
			line = lipgloss.NewStyle().Background(selectionBand).Width(width).Render(line)
		}
		lines = append(lines, line)
		m.graphRows = append(m.graphRows, graphRow{line: row, nodeID: historyGraphRowID})
	}

	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

// nodeLabel is the display name of a node anywhere space is short. It is a
// pure display projection: durable titles and briefs remain verbatim in the
// store, while prompt-shaped fallbacks are compressed here.
func nodeLabel(node store.Node, jobRoot ...store.Node) string {
	title := strings.TrimSpace(node.Title)
	if strings.EqualFold(title, "synthesis") {
		root := node
		if len(jobRoot) > 0 && jobRoot[0].ID != "" {
			root = jobRoot[0]
		}
		if noun := synthesisJobNoun(root); noun != "" && noun != "synthesis" {
			return "synthesis · " + noun
		}
		return "synthesis"
	}
	if title != "" && !titleMatchesBriefPrefix(title, node.Brief) && !instructionShapedTitle(title) {
		return title
	}
	source := node.Brief
	if strings.TrimSpace(source) == "" {
		source = title
	}
	if label := deriveNodeLabel(source); label != "" {
		return label
	}
	if title != "" {
		return strings.ToLower(title)
	}
	return node.ID
}

func instructionShapedTitle(title string) bool {
	title = strings.ToLower(strings.TrimSpace(title))
	for _, prefix := range []string{
		"you are ", "you're ", "you will receive ", "you'll receive ",
		"write the ", "write a ", "write an ",
	} {
		if strings.HasPrefix(title, prefix) {
			return true
		}
	}
	return false
}

func nodeLabelInSnapshot(node store.Node, snapshot store.Snapshot) string {
	if root, ok := nodeJobRoots(snapshot.Nodes)[node.ID]; ok {
		return nodeLabel(node, root)
	}
	return nodeLabel(node)
}

// nodeJobRoots resolves every visible node to its top-level job. Cycles and
// partial snapshots fail closed: the ordinary node label remains available.
func nodeJobRoots(nodes []store.Node) map[string]store.Node {
	byID := make(map[string]store.Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	roots := make(map[string]store.Node, len(nodes))
	visiting := make(map[string]bool, len(nodes))
	var resolve func(string) (store.Node, bool)
	resolve = func(id string) (store.Node, bool) {
		if root, ok := roots[id]; ok {
			return root, true
		}
		node, ok := byID[id]
		if !ok || node.ID == store.RootID || visiting[id] {
			return store.Node{}, false
		}
		visiting[id] = true
		defer delete(visiting, id)
		if node.Parent == store.RootID {
			roots[id] = node
			return node, true
		}
		root, ok := resolve(node.Parent)
		if ok {
			roots[id] = root
		}
		return root, ok
	}
	for id := range byID {
		_, _ = resolve(id)
	}
	return roots
}

func titleMatchesBriefPrefix(title, brief string) bool {
	clipped := strings.HasSuffix(strings.TrimSpace(title), "…") || strings.HasSuffix(strings.TrimSpace(title), "...")
	normalize := func(value string) string {
		value = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(value, "…"), "..."))
		return strings.ToLower(strings.Join(strings.Fields(value), " "))
	}
	title = normalize(title)
	brief = normalize(brief)
	if title == "" || brief == "" || !strings.HasPrefix(brief, title) {
		return false
	}
	if len(brief) == len(title) || clipped {
		return true
	}
	for _, next := range brief[len(title):] {
		return unicode.IsSpace(next) || strings.ContainsRune(".,:;!?—–-", next)
	}
	return false
}

func deriveNodeLabel(brief string) string {
	brief, imperative := stripInstructionBoilerplate(brief)
	words := meaningfulLabelWords(brief, 6)
	if len(words) == 0 {
		return ""
	}
	if imperative {
		words[0] = imperativeWord(words[0])
	}
	return strings.Join(words, " ")
}

func stripInstructionBoilerplate(text string) (string, bool) {
	text = strings.TrimSpace(text)
	lower := strings.ToLower(text)
	for _, prefix := range []string{"you will receive ", "you'll receive "} {
		if !strings.HasPrefix(lower, prefix) {
			continue
		}
		rest := text[len(prefix):]
		if boundary := strings.IndexAny(rest, ".;\n"); boundary >= 0 && strings.TrimSpace(rest[boundary+1:]) != "" {
			text = strings.TrimSpace(rest[boundary+1:])
		} else {
			text = strings.TrimSpace(rest)
		}
		lower = strings.ToLower(text)
		break
	}
	for _, prefix := range []string{"write the ", "write a ", "write an "} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(text[len(prefix):]), false
		}
	}
	for _, prefix := range []string{"you are ", "you're "} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(text[len(prefix):]), true
		}
	}
	return text, false
}

func meaningfulLabelWords(text string, limit int) []string {
	stop := map[string]bool{
		"a": true, "an": true, "the": true, "and": true, "or": true,
		"of": true, "for": true, "to": true, "from": true, "with": true,
		"in": true, "on": true, "at": true, "by": true, "into": true,
		"this": true, "that": true, "these": true, "those": true,
		"your": true, "its": true, "their": true, "it": true, "them": true,
	}
	fields := strings.FieldsFunc(strings.ToLower(text), func(char rune) bool {
		return !unicode.IsLetter(char) && !unicode.IsNumber(char) && char != '-'
	})
	words := make([]string, 0, min(limit, len(fields)))
	for _, field := range fields {
		field = strings.Trim(field, "-")
		if field == "" || stop[field] {
			continue
		}
		words = append(words, field)
		if len(words) == limit {
			break
		}
	}
	return words
}

func imperativeWord(word string) string {
	known := map[string]string{
		"analyzing": "analyze", "assembling": "assemble", "creating": "create",
		"designing": "design", "editing": "edit", "generating": "generate",
		"implementing": "implement", "preparing": "prepare", "producing": "produce",
		"recording": "record", "reviewing": "review", "studying": "study",
		"writing": "write",
	}
	if imperative, ok := known[word]; ok {
		return imperative
	}
	if strings.HasSuffix(word, "ing") && len(word) > 5 {
		stem := strings.TrimSuffix(word, "ing")
		if len(stem) > 2 && stem[len(stem)-1] == stem[len(stem)-2] {
			stem = stem[:len(stem)-1]
		}
		return stem
	}
	return word
}

func synthesisJobNoun(root store.Node) string {
	source := strings.TrimSpace(root.Title)
	if source == "" || strings.EqualFold(source, "synthesis") {
		source = strings.TrimSpace(root.Provenance.Intent)
	}
	if source == "" {
		source = root.Brief
	}
	words := strings.Fields(deriveNodeLabel(source))
	if len(words) > 5 {
		words = words[:5]
	}
	if len(words) == 0 {
		return ""
	}
	objectVerbs := map[string]bool{
		"assemble": true, "build": true, "create": true, "deliver": true,
		"generate": true, "make": true, "prepare": true, "produce": true,
		"review": true, "write": true,
	}
	words[0] = imperativeWord(words[0])
	if len(words) > 1 && objectVerbs[words[0]] {
		words = words[1:]
	}
	return strings.Join(words, " ")
}

// nodeSettled reports whether one node has nothing left to do: it landed,
// failed, was cancelled, or is the digest of a folded subtree.
func nodeSettled(node store.Node) bool {
	if node.FoldRoot {
		return true
	}
	switch node.Status {
	case store.Done, store.Failed, store.Cancelled:
		return true
	}
	return false
}

// subtreeSettled reports whether a node and every visible descendant are
// settled — the condition for a job to sink below live work and dim.
func subtreeSettled(node store.Node, children map[string][]store.Node) bool {
	if !nodeSettled(node) {
		return false
	}
	for _, child := range children[node.ID] {
		if !subtreeSettled(child, children) {
			return false
		}
	}
	return true
}

// orderRoots puts jobs that are still moving first, newest first, and sinks
// fully settled jobs below them (also newest first). Children keep creation
// order — inside a job the pipeline shape is the information.
func orderRoots(roots []store.Node, children map[string][]store.Node) []store.Node {
	live := make([]store.Node, 0, len(roots))
	settled := make([]store.Node, 0, len(roots))
	for _, root := range roots {
		if subtreeSettled(root, children) {
			settled = append(settled, root)
		} else {
			live = append(live, root)
		}
	}
	newestFirst := func(nodes []store.Node) {
		sort.SliceStable(nodes, func(i, j int) bool {
			return nodes[i].CreatedSeq > nodes[j].CreatedSeq
		})
	}
	newestFirst(live)
	newestFirst(settled)
	return append(live, settled...)
}

// visibleRootHistory keeps live work and the five freshest settled jobs in
// the primary rail. Older top-level jobs remain reachable through one row.
func visibleRootHistory(roots []store.Node, children map[string][]store.Node, expanded bool) ([]store.Node, int) {
	visible := make([]store.Node, 0, len(roots))
	settled := 0
	history := 0
	for _, root := range roots {
		if !subtreeSettled(root, children) {
			visible = append(visible, root)
			continue
		}
		settled++
		if settled <= railHistoryLimit || expanded {
			visible = append(visible, root)
		}
		if settled > railHistoryLimit {
			history++
		}
	}
	return visible, history
}

func (m *Model) nodeGlyph(node store.Node) (string, bool) {
	return m.nodeGlyphStyled(node, time.Now(), false)
}

func (m *Model) nodeGlyphStyled(node store.Node, now time.Time, dimmed bool) (string, bool) {
	tint := func(color lipgloss.AdaptiveColor) lipgloss.AdaptiveColor {
		if dimmed {
			return muted
		}
		return color
	}
	if node.FoldRoot {
		return lipgloss.NewStyle().Foreground(tint(powder)).Render("◆"), false
	}
	switch node.Status {
	case store.Done:
		return lipgloss.NewStyle().Foreground(tint(mint)).Bold(m.completionFlashing(node, now)).Render("●"), false
	case store.Claimed, store.Running:
		frame := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
		return lipgloss.NewStyle().Foreground(peach).Render("● " + frame), true
	case store.Failed, store.Cancelled:
		return lipgloss.NewStyle().Foreground(tint(rose)).Render("●"), false
	default:
		return lipgloss.NewStyle().Foreground(butter).Render("○"), false
	}
}

func (m *Model) completionFlashing(node store.Node, now time.Time) bool {
	return node.Status == store.Done && !node.FinishedAt.IsZero() &&
		!now.Before(node.FinishedAt) && now.Sub(node.FinishedAt) < 2*time.Second
}

func (m *Model) noteAnimatedGraphRow(row int) {
	if !m.graphVisible() {
		return
	}
	start := m.graph.YOffset
	end := start + max(1, m.graph.Height)
	if row >= start && row < end {
		m.graphAnimating = true
	}
}

func (m *Model) planningCount() int {
	count := 0
	for _, command := range m.pending {
		if command.Kind == store.CommandSplice {
			count++
		}
	}
	return count
}

func humanizeTokens(tokens int) string {
	switch {
	case tokens >= 1_000_000:
		return trimDecimal(float64(tokens)/1_000_000) + "M"
	case tokens >= 1_000:
		return trimDecimal(float64(tokens)/1_000) + "k"
	default:
		return fmt.Sprintf("%d", tokens)
	}
}

func trimDecimal(value float64) string {
	return strings.TrimSuffix(fmt.Sprintf("%.1f", value), ".0")
}

func relativeTime(at, now time.Time) string {
	if at.IsZero() || !now.After(at) {
		return "now"
	}
	age := now.Sub(at)
	switch {
	case age < time.Minute:
		return "now"
	case age < time.Hour:
		return fmt.Sprintf("%dm ago", int(age/time.Minute))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(age/time.Hour))
	default:
		return fmt.Sprintf("%dd ago", int(age/(24*time.Hour)))
	}
}

func formatElapsed(elapsed time.Duration) string {
	if elapsed < 0 {
		elapsed = 0
	}
	elapsed = elapsed.Round(time.Second)
	if elapsed < time.Minute {
		return fmt.Sprintf("%ds", int(elapsed/time.Second))
	}
	if elapsed < time.Hour {
		return fmt.Sprintf("%dm %02ds", int(elapsed/time.Minute), int(elapsed/time.Second)%60)
	}
	return fmt.Sprintf("%dh %02dm", int(elapsed/time.Hour), int(elapsed/time.Minute)%60)
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return strings.TrimSpace(line)
}

// truncate clips text to width terminal cells. It is ANSI-aware: styled input
// is cut between escape sequences, never through them, so a clipped line can
// neither split a style mid-word nor leave a dangling escape that eats the
// leading characters of the next line the terminal draws.
func truncate(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(text) <= width {
		return text
	}
	if width == 1 {
		return "…"
	}
	return ansi.Truncate(text, width, "…")
}

func wrapText(text string, width int) string {
	if width <= 1 {
		return text
	}
	paragraphs := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	wrapped := make([]string, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			wrapped = append(wrapped, "")
			continue
		}
		line := ""
		for _, word := range words {
			for lipgloss.Width(word) > width {
				if line != "" {
					wrapped = append(wrapped, line)
					line = ""
				}
				piece, rest := splitWidth(word, width)
				wrapped = append(wrapped, piece)
				word = rest
			}
			candidate := word
			if line != "" {
				candidate = line + " " + word
			}
			if lipgloss.Width(candidate) > width {
				wrapped = append(wrapped, line)
				line = word
			} else {
				line = candidate
			}
		}
		if line != "" {
			wrapped = append(wrapped, line)
		}
	}
	return strings.Join(wrapped, "\n")
}

// splitWidth cuts one over-wide word at a cell boundary, ANSI-aware, always
// making progress even when the first grapheme alone is wider than the room.
func splitWidth(text string, width int) (string, string) {
	piece := ansi.Truncate(text, width, "")
	if piece == "" || len(piece) >= len(text) {
		_, size := firstRune(text)
		return text[:size], text[size:]
	}
	return piece, ansi.TruncateLeft(text, width, "")
}

func firstRune(text string) (rune, int) {
	for _, char := range text {
		return char, len(string(char))
	}
	return 0, 0
}
