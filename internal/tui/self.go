package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type place uint8

const (
	placeThread place = iota
	placeBoard
	placeSelf
)

type selfSection uint8

const (
	selfToday selfSection = iota
	selfCompetenceSection
	selfBeliefs
	selfStanding
)

var selfSectionNames = []string{"today", "competence", "beliefs", "standing"}

const (
	// selfTenureAfter mirrors the store's default promotion ladder: three
	// consecutive green firings turn probation into tenure.
	selfTenureAfter = 3
	// selfReceiptCap is how many of today's receipts the file shows before it
	// admits the rest with a single "…N more"; selfBeliefLimit is the matching
	// budget for the newest notebook facts. The file is a glance, not a log.
	selfReceiptCap  = 10
	selfBeliefLimit = 10
)

type selfRow struct {
	line    int
	section selfSection
	header  bool
}

// selfCharterLister matches the store's variadic status filter exactly. The
// older rail compatibility seam intentionally keeps its no-argument shape.
type selfCharterLister interface {
	Charters(...store.CharterStatus) ([]store.Charter, error)
}

// selfDataReader is the whole read surface the employee file needs. Binding it
// as one interface — rather than five anonymous assertions — makes the
// compile-time assertion below the guarantee that the real store still answers
// every question Self asks.
type selfDataReader interface {
	SelfReceipts(since time.Time) ([]store.SelfReceipt, error)
	SelfSpendToday() (float64, error)
	CompetenceMap(...store.CompetenceOptions) (store.CompetenceMap, error)
	RecentFacts(limit int) ([]store.Fact, error)
}

var (
	_ selfCharterLister = (*store.Store)(nil)
	_ selfDataReader    = (*store.Store)(nil)
)

func (m *Model) activePlace() place {
	switch {
	case m.selfVisible():
		return placeSelf
	case m.graphOpen || m.nodeViewID != "":
		return placeBoard
	default:
		return placeThread
	}
}

func (m *Model) selfVisible() bool { return m.selfOpen && m.nodeViewID == "" }

// selectPlace is the single spatial transition used by header clicks,
// option-number chords, slash commands, and the esc ladder. Leaving a place
// closes what was open on top of it — a node view, a palette, the notebook —
// so no surface can survive invisibly behind another place and steal the next
// esc.
func (m *Model) selectPlace(next place) tea.Cmd {
	if m.nodeViewID != "" {
		m.closeNodeView()
	}
	if m.notebookOpen {
		m.closeNotebook()
	}
	m.palette = paletteNone
	m.paletteDismissed = false
	switch next {
	case placeBoard:
		m.selfOpen = false
		m.graphOpen = true
		m.graphScopeID = ""
		m.charterCardID = ""
		m.charterFocusIndex = 0
		m.serviceCardID = ""
		m.serviceFocusIndex = 0
		m.focus = focusGraph
		m.inputFocused = false
		m.input.Blur()
		m.ensureGraphSelection()
	case placeSelf:
		m.graphOpen = false
		m.selfOpen = true
		m.focus = focusSelf
		m.inputFocused = false
		m.input.Blur()
		m.ensureSelfSelectionVisible()
	default:
		m.graphOpen = false
		m.selfOpen = false
		m.graphScopeID = ""
		m.charterCardID = ""
		m.charterFocusIndex = 0
		m.focus = focusInput
		m.inputFocused = true
		_ = m.input.Focus()
	}
	m.setSize(m.width, m.height)
	if next == placeSelf {
		return m.poll()
	}
	return nil
}

// boardNeedsAttention lights the board dot for the two states the rail
// exists to surface: a failed job, or a job whose question has sat unanswered
// long enough to count as stuck (the same questionIsStuck the dock uses).
func (m *Model) boardNeedsAttention() bool {
	_, _, failed := m.taskCounts()
	if failed > 0 {
		return true
	}
	now := m.standingTime()
	for _, card := range m.cards {
		if questionIsStuck(card, now) {
			return true
		}
	}
	return false
}

func (m *Model) selfNeedsAttention() bool {
	for _, charter := range m.selfCharters {
		if charter.Status == store.CharterProposed {
			return true
		}
	}
	return false
}

func startOfLocalDay(now time.Time) time.Time {
	local := now.In(time.Local)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
}

func (m *Model) refreshSelf() {
	offset := m.self.YOffset
	m.self.SetContent(m.renderSelfContent(max(1, m.self.Width)))
	m.self.SetYOffset(offset)
	if len(m.selfRows) == 0 {
		m.selfSelection = 0
		return
	}
	m.selfSelection = max(0, min(m.selfSelection, len(m.selfRows)-1))
}

func (m *Model) renderSelfContent(width int) string {
	m.selfRows = m.selfRows[:0]
	lines := make([]string, 0, 32)
	for index, name := range selfSectionNames {
		if index > 0 {
			lines = append(lines, "")
		}
		section := selfSection(index)
		expanded := m.selfExpanded == index
		disclosure := "▸"
		if expanded {
			disclosure = "▾"
		}
		selected := m.focus == focusSelf && len(m.selfRows) == m.selfSelection
		style := mutedStyle.Faint(true)
		if selected {
			style = powderStyle.Bold(true)
		}
		m.selfRows = append(m.selfRows, selfRow{line: len(lines), section: section, header: true})
		line := truncate(style.Render(disclosure+" ")+inputTextStyle.Render(name), width)
		if selected {
			line = bandStyle.Width(width).Render(line)
		}
		lines = append(lines, line)
		if !expanded {
			continue
		}
		switch section {
		case selfToday:
			m.appendSelfToday(&lines, width)
		case selfCompetenceSection:
			m.appendSelfCompetence(&lines, width)
		case selfBeliefs:
			m.appendSelfBeliefs(&lines, width)
		case selfStanding:
			m.appendSelfStanding(&lines, width)
		}
	}
	return strings.Join(lines, "\n")
}

func (m *Model) appendSelfRow(lines *[]string, section selfSection, body string, width int) {
	selected := m.focus == focusSelf && len(m.selfRows) == m.selfSelection
	marker := mutedStyle.Faint(true).Render("  ")
	if selected {
		marker = powderStyle.Bold(true).Render("▸ ")
	}
	m.selfRows = append(m.selfRows, selfRow{line: len(*lines), section: section})
	line := truncate(marker+body, width)
	if selected {
		line = bandStyle.Width(width).Render(line)
	}
	*lines = append(*lines, line)
}

func (m *Model) appendSelfToday(lines *[]string, width int) {
	live := make([]store.Node, 0)
	for _, node := range m.snapshot.Nodes {
		if node.ID != store.RootID && node.Provenance.Origin == store.OriginSelf &&
			(node.Status == store.Claimed || node.Status == store.Running) {
			live = append(live, node)
		}
	}
	sort.SliceStable(live, func(i, j int) bool { return live[i].CreatedSeq < live[j].CreatedSeq })
	if len(live) == 0 {
		m.appendSelfRow(lines, selfToday, mutedStyle.Faint(true).Render("presence · quiet"), width)
	} else {
		for _, node := range live {
			kind := "self work"
			if node.Group == store.PracticeGroup {
				kind = "practice"
			}
			glyph := peachStyle.Render(spinnerFrames[m.spinnerFrame%len(spinnerFrames)])
			label := nodeLabelInSnapshot(node, m.snapshot)
			body := glyph + " " + inputTextStyle.Render(label) + mutedStyle.Faint(true).Render(" · "+kind)
			m.appendSelfRow(lines, selfToday, body, width)
		}
	}
	m.appendSelfRow(lines, selfToday,
		mutedStyle.Faint(true).Render("self-spend today · ")+inputTextStyle.Render(formatSelfDollars(m.selfSpend)), width)

	if len(m.selfReceipts) == 0 {
		m.appendSelfRow(lines, selfToday, mutedStyle.Faint(true).Render("no receipts today"), width)
		return
	}
	*lines = append(*lines, mutedStyle.Faint(true).Render(truncate("  TRIED   COST   LEARNED", width)))
	shown := min(selfReceiptCap, len(m.selfReceipts))
	for index := 0; index < shown; index++ {
		receipt := m.selfReceipts[len(m.selfReceipts)-1-index]
		tried := oneLineSelfReceipt(receipt.Origin)
		if tried == "" {
			tried = oneLineSelfReceipt(receipt.Scope)
		}
		body := inputTextStyle.Render(tried) + mutedStyle.Faint(true).Render("   "+formatSelfDollars(receipt.Cost)+"   ") +
			inputTextStyle.Render(selfReceiptLearning(receipt))
		m.appendSelfRow(lines, selfToday, body, width)
	}
	if more := len(m.selfReceipts) - shown; more > 0 {
		m.appendSelfRow(lines, selfToday, mutedStyle.Faint(true).Render(fmt.Sprintf("…%d more", more)), width)
	}
}

func (m *Model) appendSelfCompetence(lines *[]string, width int) {
	classes := []store.CompetenceClass{store.CompetenceStrong, store.CompetenceFrontier, store.CompetenceWeak}
	wrote := false
	for _, class := range classes {
		rows := make([]store.ScopeCompetence, 0)
		for _, scope := range m.selfCompetence.Scopes {
			if scope.Class == class {
				rows = append(rows, scope)
			}
		}
		if len(rows) == 0 {
			continue
		}
		wrote = true
		*lines = append(*lines, mutedStyle.Faint(true).Render("  "+string(class)))
		for _, scope := range rows {
			glyphStyle := mutedStyle.Faint(true)
			glyph := "·"
			if class == store.CompetenceFrontier {
				glyphStyle = peachStyle
				glyph = "◆"
			}
			name := strings.TrimPrefix(scope.Scope, "profile:")
			if scope.Kind == store.CompetenceProfile {
				name += " work"
			}
			number := fmt.Sprintf("%d%% failed", int(scope.FailureRate*100+0.5))
			if scope.Samples == 0 {
				number = "0 runs"
			}
			body := glyphStyle.Render(glyph) + " " + inputTextStyle.Render(name) +
				mutedStyle.Faint(true).Render(" · "+number)
			m.appendSelfRow(lines, selfCompetenceSection, body, width)
		}
	}
	if !wrote {
		m.appendSelfRow(lines, selfCompetenceSection, mutedStyle.Faint(true).Render("no measured scopes yet"), width)
	}
}

func (m *Model) appendSelfBeliefs(lines *[]string, width int) {
	if len(m.selfFacts) == 0 {
		m.appendSelfRow(lines, selfBeliefs, mutedStyle.Faint(true).Render("notebook is empty"), width)
		return
	}
	for _, fact := range m.selfFacts {
		m.appendSelfRow(lines, selfBeliefs, memoryFactRow(fact, max(1, width-2)), width)
	}
}

// appendSelfStanding reads tenure from the store's first-class charters — the
// only place autonomy lives — and takes last-fired and today's count from the
// same projection the rail renders, so the two places can never disagree about
// when a charter last woke.
func (m *Model) appendSelfStanding(lines *[]string, width int) {
	fired := make(map[string]standingCharter)
	for _, charter := range m.standingCharters() {
		fired[charter.ID] = charter
	}
	count := 0
	for _, charter := range m.selfCharters {
		if charter.Status == store.CharterRetired {
			continue
		}
		count++
		grade := "tenured"
		if charter.Autonomy != store.CharterTenured {
			grade = fmt.Sprintf("probation %d/%d",
				min(selfTenureAfter, max(0, charter.GreenFirings)), selfTenureAfter)
		}
		history := fired[charter.ID]
		last := "never"
		if !history.LastFired.IsZero() {
			last = history.LastFired.Local().Format("Jan 2 15:04")
		}
		name := charterShortName(charter.Invariant, charter.ID)
		body := inputTextStyle.Render(name) + mutedStyle.Faint(true).Render(
			fmt.Sprintf(" · %s · last %s · %d today", grade, last, history.Today))
		m.appendSelfRow(lines, selfStanding, body, width)
	}
	if count == 0 {
		m.appendSelfRow(lines, selfStanding, mutedStyle.Faint(true).Render("no standing charters"), width)
	}
}

func (m *Model) renderSelfPane() string {
	lines := strings.Split(m.self.View(), "\n")
	for len(lines) < m.chatHeight {
		lines = append(lines, "")
	}
	if len(lines) > m.chatHeight {
		lines = lines[:m.chatHeight]
	}
	clampLines(lines, m.width)
	return lipgloss.NewStyle().Width(m.width).Render(strings.Join(lines, "\n"))
}

func (m *Model) moveSelfSelection(delta int) {
	if len(m.selfRows) == 0 {
		return
	}
	m.selfSelection = max(0, min(len(m.selfRows)-1, m.selfSelection+delta))
	m.refreshSelf()
	m.ensureSelfSelectionVisible()
}

func (m *Model) activateSelfSelection() {
	if m.selfSelection < 0 || m.selfSelection >= len(m.selfRows) {
		return
	}
	row := m.selfRows[m.selfSelection]
	if !row.header {
		return
	}
	section := int(row.section)
	if m.selfExpanded == section {
		m.selfExpanded = -1
	} else {
		m.selfExpanded = section
	}
	m.refreshSelf()
	m.ensureSelfSelectionVisible()
}

func (m *Model) ensureSelfSelectionVisible() {
	if len(m.selfRows) == 0 || m.selfSelection < 0 || m.selfSelection >= len(m.selfRows) {
		return
	}
	line := m.selfRows[m.selfSelection].line
	if line < m.self.YOffset {
		m.self.SetYOffset(line)
	} else if line >= m.self.YOffset+m.self.Height {
		m.self.SetYOffset(max(0, line-m.self.Height+1))
	}
}

func (m *Model) activateSelfAt(x, y int) bool {
	if !m.selfBounds.contains(x, y) {
		return false
	}
	line := y - m.selfBounds.y + m.self.YOffset
	for index, row := range m.selfRows {
		if row.line != line {
			continue
		}
		m.focus = focusSelf
		m.inputFocused = false
		m.input.Blur()
		m.selfSelection = index
		if row.header {
			m.activateSelfSelection()
		} else {
			m.refreshSelf()
		}
		return true
	}
	return true
}

func formatSelfDollars(cost float64) string {
	raw := strconv.FormatFloat(cost, 'f', 4, 64)
	raw = strings.TrimRight(raw, "0")
	if strings.HasSuffix(raw, ".") {
		raw += "00"
	} else if !strings.Contains(raw, ".") {
		raw += ".00"
	} else if len(raw)-strings.IndexByte(raw, '.') == 2 {
		raw += "0"
	}
	return "$" + raw
}

func selfReceiptLearning(receipt store.SelfReceipt) string {
	parts := make([]string, 0, 3)
	if len(receipt.FactIDs) > 0 {
		parts = append(parts, "facts "+selfReceiptIDs(receipt.FactIDs))
	}
	if len(receipt.SkillIDs) > 0 {
		parts = append(parts, "skills "+selfReceiptIDs(receipt.SkillIDs))
	}
	if receipt.SurpriseDelta != nil && *receipt.SurpriseDelta > 0 {
		parts = append(parts, fmt.Sprintf("surprise down %.0f%%", 100**receipt.SurpriseDelta))
	}
	if len(parts) == 0 {
		parts = append(parts, "nothing")
	}
	if receipt.SurpriseDelta != nil && *receipt.SurpriseDelta < 0 {
		parts = append(parts, fmt.Sprintf("surprise up %.0f%%", -100**receipt.SurpriseDelta))
	}
	return strings.Join(parts, "; ")
}

func selfReceiptIDs(ids []int64) string {
	values := make([]string, 0, len(ids))
	for _, id := range ids {
		values = append(values, "#"+strconv.FormatInt(id, 10))
	}
	return strings.Join(values, ",")
}

func oneLineSelfReceipt(value string) string { return strings.Join(strings.Fields(value), " ") }
