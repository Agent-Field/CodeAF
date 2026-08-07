package tui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
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

	// One style per ink, built once. A style carries its color in an interface,
	// so building one inside a render heap-allocates per glyph, per row, per
	// frame. Variants (bold, faint, a width) copy from these — a Style is a
	// value, so a copy costs nothing and shares nothing.
	inkStyle      = lipgloss.NewStyle().Foreground(ink)
	powderStyle   = lipgloss.NewStyle().Foreground(powder)
	peachStyle    = lipgloss.NewStyle().Foreground(peach)
	mintStyle     = lipgloss.NewStyle().Foreground(mint)
	butterStyle   = lipgloss.NewStyle().Foreground(butter)
	roseStyle     = lipgloss.NewStyle().Foreground(rose)
	lavenderStyle = lipgloss.NewStyle().Foreground(lavender)
	mutedStyle    = lipgloss.NewStyle().Foreground(muted)

	selectedInk   = lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#24202E"}
	selectionBand = lipgloss.AdaptiveColor{Light: "#E8E7EE", Dark: "#343442"}
	bandStyle     = lipgloss.NewStyle().Background(selectionBand)

	promptStyle      = powderStyle.Bold(true)
	inputTextStyle   = inkStyle
	placeholderStyle = mutedStyle
	cursorStyle      = powderStyle
	questionStyle    = lavenderStyle
	selectedStyle    = inkStyle.Background(selectionBand)
	pillStyle        = lipgloss.NewStyle().Foreground(selectedInk).Background(peach).Padding(0, 1)

	// The two conversational voices (level 1 and 2 above).
	aforgeLabelStyle = lavenderStyle
	youLabelStyle    = mutedStyle.Faint(true)
	youTextStyle     = inkStyle.Faint(true)

	spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
)

// View composes the complete frame once, avoiding terminal-clearing redraws.
// The frame is open text — hierarchy comes from ink and whitespace, not boxes.
func (m *Model) View() string {
	// One frame, one dock. Bounds tracking and the bar itself both need it, and
	// rendering it twice to throw one away is a card render per frame.
	m.invalidateDock()
	m.trackPaneBounds()
	top := m.renderTopBar()

	var main string
	switch {
	case m.nodeViewID != "":
		main = m.renderNodePane()
	case m.selfVisible():
		main = m.renderSelfPane()
	case m.graphVisible() && m.horizontal:
		main = lipgloss.JoinHorizontal(lipgloss.Top, m.renderChatPane(), "  ", m.renderGraphPane())
	case m.graphVisible():
		main = m.renderGraphPane()
	default:
		main = m.renderChatPane()
	}

	parts := []string{top, "", main, ""}
	if m.paletteOpen() && m.palette != paletteModels && m.palette != paletteModel &&
		m.palette != paletteHelp && m.palette != paletteSettings {
		parts = append(parts, m.renderPalette())
	}
	if m.activityBarVisible() {
		if bar := m.renderActivityBar(); bar != "" {
			parts = append(parts, bar)
		}
	}
	parts = append(parts, m.renderInput())
	if !m.paletteOpen() {
		// One footer line, one explicit priority: transient voice status →
		// ambient status the top bar could not fit → active boost indicator →
		// pending-question context → idle tip → focus-zone help. Both status
		// lines are short-lived so boost only yields momentarily; tips are
		// idle-only and can never displace an armed or pinned boost.
		hint := m.contextHelpLine()
		boostShown := false
		switch {
		case m.voiceHint != "" && time.Now().Before(m.voiceHintUntil):
			hint = m.voiceHint
		case m.voiceState == voiceRecording:
			hint = "ctrl+v finish · esc discard · keep typing to preserve your draft"
		case m.voiceState == voiceStarting || m.voiceState == voiceFinalizing:
			hint = "voice working · esc discard"
		case m.status != "" && time.Now().Before(m.statusUntil) && !m.headerStatusShown:
			hint = m.status
		case m.boost != boostOff:
			hint = m.boostLabel()
			boostShown = true
		case m.hasPendingQuestion() && m.focus != focusQuestions:
			hint = "press a question's number — or type your own answer"
		default:
			if tip := m.idleTipLine(m.standingTime()); tip != "" {
				hint = tip
			}
		}
		renderedHint := mutedStyle.Faint(true).Render(truncate(hint, m.width))
		if boostShown {
			m.boostBounds = paneBounds{x: 0, y: m.inputBounds.bottom(), width: lipgloss.Width(renderedHint), height: 1}
		}
		parts = append(parts, renderedHint)
	}
	frame := lipgloss.JoinVertical(lipgloss.Left, parts...)
	if m.palette == paletteModels || m.palette == paletteModel {
		frame = m.overlayModels(frame)
	} else if m.palette == paletteHelp {
		frame = m.overlayHelp(frame)
	} else if m.palette == paletteSettings {
		frame = m.overlaySettings(frame)
	}
	return frame
}

func (m *Model) trackPaneBounds() {
	m.chatBounds = paneBounds{}
	m.headerTasksBounds = paneBounds{}
	m.headerThreadBounds = paneBounds{}
	m.headerBoardBounds = paneBounds{}
	m.headerSelfBounds = paneBounds{}
	m.headerQuestionBounds = paneBounds{}
	m.headerModelsBounds = paneBounds{}
	m.headerHelpBounds = paneBounds{}
	m.graphBounds = paneBounds{}
	m.graphRowsBounds = paneBounds{}
	m.standingRowsBounds = paneBounds{}
	m.serviceRowsBounds = paneBounds{}
	m.graphToggleBounds = paneBounds{}
	m.selfBounds = paneBounds{}
	m.paletteCloseBounds = paneBounds{}
	m.helpBounds = paneBounds{}
	m.settingsBounds = paneBounds{}
	m.headerSettingsBounds = paneBounds{}
	m.modelPickerBounds = paneBounds{}
	m.modelSlotRows = m.modelSlotRows[:0]
	m.modelPickerRows = m.modelPickerRows[:0]
	m.settingsRowHits = m.settingsRowHits[:0]
	m.inputBounds = paneBounds{}
	m.boostBounds = paneBounds{}
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
	case m.selfVisible():
		m.selfBounds = paneBounds{x: 0, y: mainY, width: m.width, height: m.chatHeight}
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
		servicesHeight := m.servicesSectionHeight()
		presenceHeight := m.residentPresenceHeight()
		if standingHeight > 0 {
			m.standingRowsBounds = paneBounds{
				x: m.graphBounds.x, y: m.graphBounds.y + 1,
				width: m.graph.Width, height: standingHeight - 2,
			}
		}
		if servicesHeight > 0 {
			m.serviceRowsBounds = paneBounds{
				x: m.graphBounds.x, y: m.graphBounds.y + standingHeight + 1,
				width: m.graph.Width, height: servicesHeight - 2,
			}
		}
		m.graphRowsBounds = paneBounds{
			x: m.graphBounds.x, y: m.graphBounds.y + standingHeight + servicesHeight + presenceHeight + 2,
			width: m.graph.Width, height: m.graph.Height,
		}
		m.graphToggleBounds = paneBounds{
			x: m.graphBounds.x, y: m.graphBounds.y + standingHeight + servicesHeight + presenceHeight,
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
	m.headerStatusShown = false
	wordmark := inkStyle.Bold(true).Render("aforge")
	thread := m.renderPlaceLabel("thread", placeThread, len(m.agentQuestions) > 0)
	board := m.renderPlaceLabel("board", placeBoard, m.boardNeedsAttention())
	self := m.renderPlaceLabel("self", placeSelf, m.selfNeedsAttention())
	separator := mutedStyle.Faint(true).Render(" · ")
	left := wordmark + "   " + thread + separator + board + separator + self
	showPlaces := true
	talkGlance := "talk " + truncate(modelShort(m.currentModel("talk")), 18)
	if m.boost == boostPinned {
		talkGlance = "talk » " + truncate(modelShort(m.currentModel("boost")), 18)
	}
	glance := mutedStyle.Faint(true).Render(talkGlance +
		" · work " + truncate(modelShort(m.currentModel("work")), 18))
	compactGlance := mutedStyle.Faint(true).Render(talkGlance)
	models := m.renderModelsButton()
	settings := m.renderSettingsButton()

	rightMeta := m.renderSpend()
	statusActive := m.status != "" && time.Now().Before(m.statusUntil)
	if statusActive {
		rightMeta = mutedStyle.Render(truncate(m.status, max(8, m.width/2)))
	}
	if m.err != nil {
		rightMeta = roseStyle.Render(truncate(m.err.Error(), max(8, m.width/2)))
	}
	// The rail toggle is a real button: alt+g and /graph are accelerators, the
	// click path is always visible. It follows the affordance grammar (▸ when
	// the rail would open, ▾ while it is on screen).
	button := m.renderTasksButton()
	help := m.renderHelpButton()

	// The header yields in one fixed order as the frame narrows: the model
	// glance shortens to talk-only, then ambient status leaves (the footer
	// line picks it up), then the glance goes entirely — it is context the
	// model door already owns — then the settings gear, which /settings and
	// alt+, still reach, and only in the last resort do the places collapse to
	// the wordmark. The model door, the rail button, and ? never yield: they
	// are the header's irreplaceable actions.
	shownGlance, shownMeta, shownSettings := glance, rightMeta, settings
	compose := func() string {
		right := ""
		if shownGlance != "" {
			right = shownGlance + "  "
		}
		right += models
		if shownSettings != "" {
			right += "  " + shownSettings
		}
		if shownMeta != "" {
			right += "  " + shownMeta
		}
		return right + "  " + button + "  " + help
	}
	yields := []func(){
		func() { shownMeta = "" },
		func() { shownGlance = "" },
		func() { shownSettings = "" },
		func() { left, showPlaces = wordmark, false },
	}
	if m.width >= railAtWidth {
		// Only a frame wide enough for the side rail shortens the glance
		// before giving anything up. Narrower than that it goes whole: a
		// truncated model name beside the door that opens it is not worth
		// the columns the places need.
		yields = append([]func(){func() { shownGlance = compactGlance }}, yields...)
	}
	right := compose()
	space := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	for _, yield := range yields {
		if space >= 1 {
			break
		}
		yield()
		right = compose()
		space = m.width - lipgloss.Width(left) - lipgloss.Width(right)
	}
	if space < 1 {
		return truncate(left+" "+right, m.width)
	}
	m.headerStatusShown = statusActive && shownMeta != ""
	rightX := lipgloss.Width(left) + space
	modelsOffset := 0
	if shownGlance != "" {
		modelsOffset = lipgloss.Width(shownGlance) + 2
	}
	m.headerModelsBounds = paneBounds{x: rightX + modelsOffset, y: 0, width: lipgloss.Width(models), height: 1}
	if shownSettings != "" {
		m.headerSettingsBounds = paneBounds{
			x: m.headerModelsBounds.right() + 2, y: 0,
			width: lipgloss.Width(shownSettings), height: 1,
		}
	}
	if showPlaces {
		x := lipgloss.Width(wordmark) + 3
		m.headerThreadBounds = paneBounds{x: x, y: 0, width: lipgloss.Width(thread), height: 1}
		x += lipgloss.Width(thread) + lipgloss.Width(separator)
		m.headerBoardBounds = paneBounds{x: x, y: 0, width: lipgloss.Width(board), height: 1}
		x += lipgloss.Width(board) + lipgloss.Width(separator)
		m.headerSelfBounds = paneBounds{x: x, y: 0, width: lipgloss.Width(self), height: 1}
	}
	buttonWidth := lipgloss.Width(button)
	helpWidth := lipgloss.Width(help)
	m.headerHelpBounds = paneBounds{x: m.width - helpWidth, y: 0, width: helpWidth, height: 1}
	m.headerTasksBounds = paneBounds{x: m.headerHelpBounds.x - 2 - buttonWidth, y: 0, width: buttonWidth, height: 1}
	if m.hasPendingQuestion() {
		offset := lipgloss.Width("⟨tasks ")
		m.headerQuestionBounds = paneBounds{x: m.headerTasksBounds.x + offset, y: 0, width: 1, height: 1}
	}
	return left + strings.Repeat(" ", space) + right
}

func (m *Model) renderPlaceLabel(name string, target place, attention bool) string {
	style := mutedStyle.Faint(true)
	if m.activePlace() == target {
		style = inkStyle
	}
	label := style.Render(name)
	if attention {
		label += " " + questionStyle.Bold(true).Render("●")
	}
	return label
}

func (m *Model) renderModelsButton() string {
	style := mutedStyle.Faint(true)
	if m.focus == focusHeader && m.headerFocusIndex == 0 {
		style = powderStyle.Bold(true)
	}
	return style.Render("models ⌄")
}

// The settings door is one glyph, like ? beside it: the header's two doors
// onto whole overlays are the two things it can afford to spell in symbols.
// It reads in the same quiet ink as the models door and brightens under focus.
func (m *Model) renderSettingsButton() string {
	style := mutedStyle.Faint(true)
	if (m.focus == focusHeader && m.headerFocusIndex == 1) || m.palette == paletteSettings {
		style = lipgloss.NewStyle().Foreground(powder)
	}
	return style.Render("⚙")
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
	if m.focus == focusHeader && m.headerFocusIndex == 2 {
		style = powderStyle
	}
	// The button keeps its name in every layout: the header's board label is
	// the place, this is the alias that opens it, and a control that renames
	// itself under the cursor is one the hand stops trusting.
	return style.Render("⟨tasks ") + dot + style.Render(disclosure+"⟩")
}

func (m *Model) renderHelpButton() string {
	style := mutedStyle.Faint(true)
	if (m.focus == focusHeader && m.headerFocusIndex == 3) || m.palette == paletteHelp {
		style = powderStyle
	}
	return style.Render("?")
}

// renderSpend keeps session usage and today's self-spend together in the
// top-right corner. Tokens and cost are quiet metadata.
func (m *Model) renderSpend() string {
	parts := make([]string, 0, 2)
	if m.usage.Nodes > 0 {
		tokens := humanizeTokens(m.usage.PromptTokens + m.usage.CompletionTokens)
		parts = append(parts, tokens+" tok · "+fmt.Sprintf("$%.2f", m.usage.Cost))
	}
	if m.selfSpendToday > 0 {
		parts = append(parts, formatCardCost(m.selfSpendToday)+" self")
	}
	if len(parts) == 0 {
		return ""
	}
	return mutedStyle.Render(strings.Join(parts, " · "))
}

// residentPresenceText is the sole projection of self-directed life. Active
// practice takes precedence over the one-poll learning afterglow; otherwise
// silence is the state.
func (m *Model) residentPresenceText() string {
	if root, ok := activePracticeRoot(m.snapshot); ok {
		return fmt.Sprintf("practicing: %s · $%.2f on myself today",
			practiceScope(root), m.selfSpendToday)
	}
	if clause := strings.TrimSpace(m.selfLearning); clause != "" {
		return "learned: " + clause
	}
	return ""
}

func activePracticeRoot(snapshot store.Snapshot) (store.Node, bool) {
	var newest store.Node
	found := false
	for _, node := range snapshot.Nodes {
		if node.Parent != store.RootID || node.Group != store.PracticeGroup || nodeSettled(node) {
			continue
		}
		if !found || node.CreatedSeq > newest.CreatedSeq {
			newest, found = node, true
		}
	}
	return newest, found
}

func practiceScope(node store.Node) string {
	title := strings.TrimSpace(node.Title)
	const prefix = "practice "
	if len(title) >= len(prefix) && strings.EqualFold(title[:len(prefix)], prefix) {
		if scope := strings.TrimSpace(title[len(prefix):]); scope != "" {
			return scope
		}
	}
	line := firstLine(node.Brief)
	lower := strings.ToLower(line)
	if strings.HasPrefix(lower, "practice question #") {
		if at := strings.Index(lower, " in "); at >= 0 {
			tail := line[at+len(" in "):]
			if colon := strings.IndexByte(tail, ':'); colon > 0 {
				return strings.TrimSpace(tail[:colon])
			}
		}
	}
	if title != "" {
		return title
	}
	if label := nodeLabel(node); label != "" {
		return label
	}
	return node.ID
}

func (m *Model) renderResidentPresence(width int) string {
	text := m.residentPresenceText()
	if text == "" {
		return ""
	}
	return mutedStyle.Faint(true).Render(truncate(text, max(1, width)))
}

func (m *Model) residentPresenceHeight() int {
	if !m.graphVisible() || m.graphScopeID != "" || m.charterCardID != "" ||
		m.residentPresenceText() == "" {
		return 0
	}
	return 1
}

// taskCounts sweeps the snapshot once for the activity bar and the rail
// header: work in flight, work queued, and anything that failed.
func (m *Model) taskCounts() (running, queued, failed int) {
	definitions := charterDefinitionIDs(m.snapshot)
	for _, node := range m.snapshot.Nodes {
		if node.ID == store.RootID || definitions[node.ID] ||
			node.Provenance.Origin == store.OriginSelf {
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
	content, _ := m.activityDock()
	return content
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
		segments = append(segments, peachStyle.Render(
			fmt.Sprintf("%s planning", frame)))
	}
	if running > 0 {
		frame := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
		segments = append(segments, peachStyle.Render(
			fmt.Sprintf("%s %d working", frame, running)))
	}
	if queued > 0 {
		segments = append(segments, mutedStyle.Render(fmt.Sprintf("○ %d queued", queued)))
	}
	if failed > 0 {
		segments = append(segments, roseStyle.Render(fmt.Sprintf("%d failed", failed)))
	}
	bar := strings.Join(segments, mutedStyle.Render(" · "))
	if bar == "" {
		// Nothing in flight: no dock at all — a quiet screen owes no chrome.
		return ""
	}
	bar += mutedStyle.Faint(true).Render(" — ctrl+t tasks")
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
	} else if service, ok := m.activeService(m.serviceCardID); ok {
		label = "‹ service · " + service.Name
	}
	title := mutedStyle.Faint(true).Render(label)
	if m.focus == focusGraph {
		title = powderStyle.Render(label)
	}
	title += mutedStyle.Faint(true).Render("  ⟨×⟩")
	lines := make([]string, 0, m.graphHeight)
	if m.graphScopeID == "" && m.charterCardID == "" && m.serviceCardID == "" {
		if section := m.renderStandingSection(max(1, m.graphWidth)); section != "" {
			lines = append(lines, strings.Split(section, "\n")...)
		}
		if section := m.renderServicesSection(max(1, m.graphWidth)); section != "" {
			lines = append(lines, strings.Split(section, "\n")...)
		}
		if presence := m.renderResidentPresence(max(1, m.graphWidth)); presence != "" {
			lines = append(lines, presence)
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
	back := powderStyle.Render("‹ back")
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
	header := back + "  " + glyph + " " + inkStyle.Bold(true).Render(title)
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
	if input.Value() == "" {
		if card := m.questionCardWithOptions(); card != nil {
			input.Placeholder = fmt.Sprintf("1–%d to answer · or type your own", len(card.Options))
		}
	}
	if m.boost != boostOff {
		input.Prompt = "» "
	}
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
		glyph := attachmentGlyph(path)
		prefix := glyph + " " + truncate(filepath.Base(path), max(1, m.width-lipgloss.Width(glyph+"  ⟨×⟩"))) + " "
		line := mutedStyle.Faint(true).Render(prefix + "⟨×⟩")
		chipLines = append(chipLines, line)
		m.attachmentBounds = append(m.attachmentBounds, paneBounds{
			x: lipgloss.Width(prefix), y: inputY + index, width: lipgloss.Width("⟨×⟩"), height: 1,
		})
	}
	if hasImageAttachments(m.attachments) {
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

// overlayModels paints both rungs of the unified model control over the first
// rows beneath the header. Moving from the eight-slot palette into the reused
// searchable picker replaces the panel in place, so the conversation never
// jumps while the operator drills down.
func (m *Model) overlayModels(frame string) string {
	width := min(72, max(36, m.width*2/3))
	x := m.headerModelsBounds.x
	if x == 0 && m.headerModelsBounds.width == 0 {
		x = max(0, m.width-width)
	}
	x = max(0, min(x, m.width-width))
	y := 1
	innerWidth := max(1, width-2)
	if m.palette == paletteModels {
		return m.overlayModelPalette(frame, x, y, width, innerWidth)
	}
	return m.overlayModelPicker(frame, x, y, width, innerWidth)
}

func (m *Model) overlayModelPalette(frame string, x, y, width, innerWidth int) string {
	title := "⟨×⟩ models"
	if m.width >= 100 {
		title = overlayRight(title, "↑/↓ choose · enter open · 1–8 jump", innerWidth)
	}
	lines := []string{title}
	for index, slot := range modelSlots {
		lines = append(lines, m.modelSlotLine(slot, index == m.modelSlotIndex, innerWidth))
	}
	panelStyle := inkStyle.Background(selectionBand).Padding(0, 1).Width(innerWidth)
	for index := range lines {
		lines[index] = panelStyle.Render(truncate(lines[index], innerWidth))
	}
	panel := strings.Join(lines, "\n")
	m.modelPickerBounds = paneBounds{x: x, y: y, width: width, height: len(lines)}
	m.paletteCloseBounds = paneBounds{x: x + 1, y: y, width: lipgloss.Width("⟨×⟩"), height: 1}
	for index := range modelSlots {
		m.modelSlotRows = append(m.modelSlotRows, modelSlotRow{
			bounds: paneBounds{x: x, y: y + 1 + index, width: width, height: 1},
			index:  index,
		})
	}
	return overlayBlock(frame, panel, x, y, m.width)
}

func (m *Model) modelSlotLine(slot string, selected bool, width int) string {
	marker := "  "
	markerStyle := mutedStyle
	if selected {
		marker = "› "
		markerStyle = powderStyle.Bold(true)
	}
	slotLabel := slot
	if slot == "boost" && m.modelFollowsWork() {
		slotLabel += " (work)"
	}
	slotColumn := 13
	modelWidth := max(1, width-lipgloss.Width(marker)-slotColumn-lipgloss.Width("  ⌄"))
	model := truncate(modelShort(m.currentModel(slot)), modelWidth)
	row := markerStyle.Render(marker) + mutedStyle.Faint(true).Render(padANSI(slotLabel, slotColumn)) +
		inputTextStyle.Render(padANSI(model, modelWidth)) + mutedStyle.Render("  ⌄")
	return truncate(row, width)
}

func (m *Model) overlayModelPicker(frame string, x, y, width, innerWidth int) string {
	body := m.modelPickerLines(innerWidth)
	lines := append([]string{"⟨×⟩ esc · " + m.modelRole + " models"}, body...)
	panelStyle := inkStyle.Background(selectionBand).Padding(0, 1).Width(innerWidth)
	for index := range lines {
		lines[index] = panelStyle.Render(truncate(lines[index], innerWidth))
	}
	panel := strings.Join(lines, "\n")
	m.modelPickerBounds = paneBounds{x: x, y: y, width: width, height: len(lines)}
	m.paletteCloseBounds = paneBounds{x: x + 1, y: y, width: lipgloss.Width("⟨×⟩"), height: 1}

	choiceLine := y + 2
	prefixLines := 1
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

func padANSI(value string, width int) string {
	value = truncate(value, width)
	return value + strings.Repeat(" ", max(0, width-lipgloss.Width(value)))
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
	return m.palette == paletteModels || m.palette == paletteModel || m.palette == paletteMemory
}

func (m *Model) paletteTitle() string {
	switch m.palette {
	case paletteModels:
		return "models"
	case paletteModel:
		return m.modelRole + " models"
	case paletteMemory:
		return "notebook"
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
	if m.palette == paletteModels || m.palette == paletteModel ||
		m.palette == paletteHelp || m.palette == paletteSettings {
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
		style = powderStyle
	case store.FactQuirk:
		glyph = "▲"
		style = peachStyle
	case store.FactLesson:
		glyph = "●"
		style = mintStyle
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
	lines := []string{mutedStyle.Render("filter: ") + inputTextStyle.Render(m.input.Value())}
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
	current := m.currentModel(m.modelRole) == choice.Slug
	if m.modelRole == "boost" && choice.Slug == followWorkModel {
		current = m.modelFollowsWork()
	}
	if current {
		marker = "● "
		markerStyle = mintStyle
	}
	if selected {
		marker = "› "
		markerStyle = powderStyle.Bold(true)
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
	} else if m.modelRole == "voice" || m.modelRole == "speech" || m.modelRole == "music" {
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
	current := m.commander.CurrentModel(role)
	if role == "boost" && strings.TrimSpace(current) == "" {
		return m.currentModel("work")
	}
	return current
}

func (m *Model) modelFollowsWork() bool {
	if model := strings.TrimSpace(m.optimisticModels["boost"]); model != "" {
		return false
	}
	if m.commander == nil {
		return true
	}
	if follower, ok := m.commander.(interface{ ModelFollows(string) bool }); ok {
		return follower.ModelFollows("boost")
	}
	return strings.TrimSpace(m.commander.CurrentModel("boost")) == ""
}

func (m *Model) boostLabel() string {
	state := "next message"
	if m.boost == boostPinned {
		state = "pinned"
	}
	return "boost " + modelShort(m.currentModel("boost")) + " · " + state
}

func modelShort(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return "–"
	}
	if index := strings.LastIndex(model, "/"); index >= 0 && index+1 < len(model) {
		model = model[index+1:]
	}
	model = strings.TrimPrefix(model, "~")
	model = strings.TrimSuffix(model, ":free")
	parts := strings.Split(model, "-")
	if len(parts) >= 4 && len(parts[len(parts)-3]) == 4 && len(parts[len(parts)-2]) == 2 && len(parts[len(parts)-1]) == 2 &&
		allDigits(parts[len(parts)-3]) && allDigits(parts[len(parts)-2]) && allDigits(parts[len(parts)-1]) {
		model = strings.Join(parts[:len(parts)-3], "-")
	}
	return model
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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
	chatExpandBrief
	chatExpandLearning
	chatExpandNotebookFact
	chatNotebookClose
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
	if m.blockWidth != m.chat.Width || m.blockGen != m.threadGen {
		m.blockWidth, m.blockGen = m.chat.Width, m.threadGen
		clear(m.blockCache)
	}
	m.chatMessageRows = m.chatMessageRows[:0]
	m.chatExpandRows = m.chatExpandRows[:0]
	m.notebookOptionRows = m.notebookOptionRows[:0]
	m.chatChipRows = m.chatChipRows[:0]
	m.historyRows = m.historyRows[:0]
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
		if item.message.Brief != nil {
			flushGroup()
			appendBlock(m.renderBrief(item.message, max(8, m.chat.Width-2), line, true))
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
	if notebook := m.renderNotebookSurface(max(8, m.chat.Width-2), line, true); notebook != "" {
		appendBlock(notebook)
	}
	if m.historyVisible {
		appendBlock(m.renderRecallHistory(max(8, m.chat.Width-2), line, true))
	}
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
	for _, row := range m.notebookOptionRows {
		seen[row.line] = true
	}
	for _, row := range m.historyRows {
		seen[row.line] = true
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
	rows[line] = bandStyle.
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

// threadBlock is one rendered message group together with the interactive
// rows it registers, held relative to the block's own first line. A settled
// message never changes, so the same bytes and the same rows can be replayed
// wherever the block lands in the thread.
type threadBlock struct {
	content  string
	chips    []chatChipRow
	expands  []chatExpandRow
	messages []chatMessageRow
}

func (m *Model) renderMessageGroup(group messageGroup, atLine int) string {
	key, cacheable := m.threadBlockKey(group)
	if cacheable {
		if block, found := m.blockCache[key]; found {
			m.replayThreadBlock(block, atLine)
			return block.content
		}
	}
	block := m.buildMessageGroup(group)
	if cacheable {
		if m.blockCache == nil {
			m.blockCache = make(map[string]threadBlock, 64)
		}
		m.blockCache[key] = block
	}
	m.replayThreadBlock(block, atLine)
	return block.content
}

// threadBlockKey names everything a settled group's block depends on that can
// move while the thread stands still: which messages it holds, which of them
// are open, and the one clock-derived string in it. A group the key cannot
// speak for — a message still arriving, a question whose choices move under
// the reader — is not cached at all.
func (m *Model) threadBlockKey(group messageGroup) (string, bool) {
	var key strings.Builder
	key.Grow(16 * len(group.messages))
	for _, message := range group.messages {
		if message.Seq == 0 {
			return "", false
		}
		if _, streaming := m.streamedBody(message); streaming {
			return "", false
		}
		if component, ok := readQuestionComponent(message.Body); ok && len(component.Options) > 0 {
			return "", false
		}
		key.WriteString(strconv.FormatInt(message.Seq, 10))
		if m.expandedMessages[message.Seq] {
			key.WriteByte('o')
		}
		if m.learningExpanded[message.Seq] {
			key.WriteByte('l')
		}
		key.WriteByte(',')
	}
	if m.receiptsExpanded {
		key.WriteByte('r')
	}
	latest := group.messages[len(group.messages)-1]
	key.WriteString(relativeTime(latest.Time, m.standingTime()))
	return key.String(), true
}

// replayThreadBlock lands a block's rows at the position it was drawn.
func (m *Model) replayThreadBlock(block threadBlock, atLine int) {
	for _, row := range block.chips {
		row.line += atLine
		m.chatChipRows = append(m.chatChipRows, row)
	}
	for _, row := range block.expands {
		row.line += atLine
		m.chatExpandRows = append(m.chatExpandRows, row)
	}
	for _, row := range block.messages {
		row.start += atLine
		row.end += atLine
		m.chatMessageRows = append(m.chatMessageRows, row)
	}
}

func (m *Model) buildMessageGroup(group messageGroup) threadBlock {
	block := threadBlock{}
	latest := group.messages[len(group.messages)-1]
	available := max(8, m.chat.Width-2)
	header := speakerHeader(latest, m.standingTime())

	// One voice: every conversational body reads in primary ink with the same
	// markdown treatment. Headers and provenance remain quiet metadata.
	line := 1
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
		item = m.withThreadQuestionOptions(item, message, available)
		// A task-anchored answer names its origin: a small clickable chip that
		// jumps to that task's activity view.
		if message.NodeID != "" && message.Role != store.RoleUser && !secondaryMessage(message) {
			chip := mutedStyle.Faint(true).Render(
				"↳ " + truncate(m.nodeChipLabel(message.NodeID), max(6, min(40, available-2))))
			block.chips = append(block.chips, chatChipRow{line: line, nodeID: message.NodeID})
			item = chip + "\n" + item
		}
		items = append(items, item)
		height := lipgloss.Height(item)
		if receipt {
			if _, details, quiet := quietSystemMessage(message.Body); quiet && len(details) > 0 {
				block.expands = append(block.expands, chatExpandRow{
					line: line, action: chatExpandLearning, seq: message.Seq,
				})
			} else if !quiet {
				block.expands = append(block.expands, chatExpandRow{
					line: line, action: chatExpandReceipts,
				})
			}
		}
		if foldedAnswer {
			block.expands = append(block.expands, chatExpandRow{
				line: line + height - 1, action: chatExpandMessage, seq: message.Seq,
			})
		}
		if message.Seq != 0 {
			block.messages = append(block.messages, chatMessageRow{start: line, end: line + height - 1, seq: message.Seq})
		}
		line += height + 1
	}
	block.content = header
	if len(items) > 0 {
		block.content += "\n" + strings.Join(items, "\n\n")
	}
	return block
}

// threadQuestionIndent is the gutter the thread's option rows hang under. The
// card gutter is a frame the thread does not have.
const threadQuestionIndent = "  "

// withThreadQuestionOptions keeps a still-open question's choices with the
// message that asked it. The thread is the durable surface: the activity dock
// collapses to a one-line summary as soon as work piles up, and the numbers
// the input placeholder promises must exist somewhere that cannot fold away.
// An answered question keeps its prompt and loses its choices — the record
// stays, the affordance does not.
func (m *Model) withThreadQuestionOptions(item string, message store.Message, width int) string {
	if message.Role != store.RoleAgent {
		return item
	}
	component, ok := readQuestionComponent(message.Body)
	if !ok || len(component.Options) == 0 || m.questionAnswered(message) {
		return item
	}
	// Selection state belongs to the card that owns the question, so the band
	// the arrows move is the same band in both places.
	selected := 0
	if card := m.cardForMessage(message); card != nil && card.State == cardQuestion &&
		len(card.Options) == len(component.Options) {
		selected = m.questionOptionIndex(*card)
	}
	var rows []string
	if component.Kind == questionConfirm {
		line, _ := renderConfirmOptions(component, selected, width, threadQuestionIndent)
		rows = []string{line}
	} else {
		rows = renderChooseOptions(component.Options, selected, width, threadQuestionIndent)
	}
	if item == "" {
		return strings.Join(rows, "\n")
	}
	return item + "\n" + strings.Join(rows, "\n")
}

// questionAnswered reads the same no-intervening-user-turn rule the head
// applies when it routes a reply: any later user turn consumes the question.
func (m *Model) questionAnswered(question store.Message) bool {
	if question.Seq == 0 {
		return false
	}
	for index := len(m.messages) - 1; index >= 0; index-- {
		message := m.messages[index]
		if message.Seq <= question.Seq {
			return false
		}
		if message.Role == store.RoleUser && message.NodeID == "" {
			return true
		}
	}
	return false
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
	if message.Role != store.RoleUser {
		body = m.linkWorkspaceReferences(message.NodeID, body)
	}
	rendered := renderMarkdown(body, width)
	if streaming {
		if rendered != "" {
			rendered += "\n"
		}
		return rendered + powderStyle.Render("▌"), false
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
	if headline, details, ok := quietSystemMessage(message.Body); ok {
		if len(details) == 0 {
			return mutedStyle.Faint(true).Render(truncate(headline, width))
		}
		if !m.learningExpanded[message.Seq] {
			return mutedStyle.Faint(true).Render(truncate(headline+" ▸", width))
		}
		lines := []string{mutedStyle.Faint(true).Render(truncate(headline+" ▾", width))}
		for _, detail := range details {
			wrapped := wrapText(detail, max(1, width-2))
			lines = append(lines, mutedStyle.Faint(true).Render(indentLines(wrapped, "  ")))
		}
		return strings.Join(lines, "\n")
	}
	summary := receiptSummary(message)
	if !m.receiptsExpanded {
		return mutedStyle.Render(truncate("▸ "+summary, width))
	}
	label := mutedStyle.Render(truncate("▾ "+summary, width))
	body := wrapText(message.Body, max(1, width-2))
	return label + "\n" + mutedStyle.Render(indentLines(body, "  "))
}

func quietSystemMessage(body string) (string, []string, bool) {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(body), "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		return "", nil, false
	}
	headline := strings.TrimSpace(lines[0])
	if !strings.HasPrefix(headline, "· reflected — ") &&
		!strings.HasPrefix(headline, "· let go — ") {
		return "", nil, false
	}
	details := make([]string, 0, len(lines)-1)
	for _, line := range lines[1:] {
		if line = strings.TrimSpace(line); line != "" {
			details = append(details, line)
		}
	}
	return headline, details, true
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
	metadata := "  " + relativeTime(message.Time, now)
	if label != "you" && strings.TrimSpace(message.Model) != "" {
		metadata += " · " + modelShort(message.Model)
	}
	return style.Render(label) + mutedStyle.Faint(true).Render(metadata)
}

func messageVoice(message store.Message) string {
	_, label := messagePresentation(message)
	if label != "you" && strings.TrimSpace(message.Model) != "" {
		return label + "\x00" + message.Model
	}
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
		prefix := peachStyle.Render(frame) + " "
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
				marker = powderStyle.Bold(true).Render("▸ ")
			}
			prefix := marker + mutedStyle.Render(ancestorGuide+branch) + glyph + " "
			label := nodeLabel(node, jobRoots[node.ID])
			labelStyle := inkStyle
			if dimmed {
				labelStyle = mutedStyle
			}
			line := prefix + labelStyle.Render(
				truncate(label, max(1, width-lipgloss.Width(prefix))),
			)
			if selected {
				line = bandStyle.Width(width).Render(line)
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
			marker = lavenderStyle.Bold(true).Render("▸ ")
		}
		disclosure := "▸"
		if m.historyExpanded {
			disclosure = "▾"
		}
		line := marker + mutedStyle.Faint(true).Render(fmt.Sprintf("%s history (%d)", disclosure, historyCount))
		if selected {
			line = bandStyle.Width(width).Render(line)
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
		return peachStyle.Render("● " + frame), true
	case store.Failed, store.Cancelled:
		return lipgloss.NewStyle().Foreground(tint(rose)).Render("●"), false
	default:
		return butterStyle.Render("○"), false
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
	// One pass for the ordinary case: ansi.Truncate returns a line that already
	// fits untouched, so measuring first pays for the same scan twice. Multiple
	// lines still measure, because their width is the widest of them and
	// truncation would run straight through the newline.
	if strings.IndexByte(text, '\n') < 0 {
		if width == 1 && lipgloss.Width(text) > 1 {
			return "…"
		}
		return ansi.Truncate(text, width, "…")
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
		// The running width is carried, not re-measured: measuring line+word per
		// word makes wrapping one paragraph quadratic in its length, and this is
		// the hottest single function in the thread render.
		line := ""
		lineWidth := 0
		for _, word := range words {
			wordWidth := lipgloss.Width(word)
			for wordWidth > width {
				if line != "" {
					wrapped = append(wrapped, line)
					line, lineWidth = "", 0
				}
				piece, rest := splitWidth(word, width)
				wrapped = append(wrapped, piece)
				word = rest
				wordWidth = lipgloss.Width(word)
			}
			switch {
			case line == "":
				line, lineWidth = word, wordWidth
			case lineWidth+1+wordWidth > width:
				wrapped = append(wrapped, line)
				line, lineWidth = word, wordWidth
			default:
				line += " " + word
				lineWidth += 1 + wordWidth
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
