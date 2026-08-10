package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// Self is one calm column, the way a settings app is: a root list of what
// aforge does when nobody is watching, each row saying in its own voice what
// it is, and enter drilling into the list behind it. Nothing is hidden and
// nothing is a dashboard — a row is a count, a sentence, and a way in.
//
// Every list here is windowed and filterable rather than complete. After six
// months the belief notebook is thousands of lines and the receipt log is
// longer; a surface that loads all of it is a surface that stops opening. The
// window plus the count plus the filter is the honest shape: it says how much
// there is, shows the part that matters now, and takes a query for the rest.

type place uint8

const (
	placeThread place = iota
	placeBoard
	placeSelf
)

type selfRoute uint8

const (
	selfRouteRoot selfRoute = iota
	selfRouteCrafts
	selfRouteCompetence
	selfRouteBeliefs
	selfRouteSkills
	selfRouteWatches
	selfRouteServices
	selfRoutePractice
	selfRouteDials
)

const (
	// selfTenureAfter mirrors the store's default promotion ladder: three
	// consecutive green firings turn probation into tenure.
	selfTenureAfter = 3
	// selfWindow is how many rows a drill-in shows before it says how many
	// more there are; selfWindowStep is what one "show more" adds. Twenty is
	// about a screen: enough to read without scrolling into a wall.
	selfWindow     = 20
	selfWindowStep = 20
	// selfBeliefScan bounds the belief read itself. The count line says
	// "500+" past it rather than pretending to have counted a notebook that
	// has been filling for months.
	selfBeliefScan = 500
	// selfSkillScan is the matching bound for forged skills. Skills are rarer
	// than beliefs by construction — a procedure has to work twice — so a
	// smaller ceiling still shows every one anybody has.
	selfSkillScan = 200
	// selfFoldAge is where a list stops being "now" and becomes history: a
	// week of receipts and beliefs reads as this week's work, and everything
	// older folds behind one line until it is asked for or searched.
	selfFoldAge = 7 * 24 * time.Hour
	// selfReceiptReach is how far back the practice log is read: a month, so
	// the week fold has a genuine "older" behind it without the read growing
	// with the life of the brain. Today's totals are a projection of the same
	// rows rather than a second query.
	selfReceiptReach = 30 * 24 * time.Hour
	// selfDetailTitleWords is how many words of a long goal name its detail
	// view. Enough to recognize, short enough that the header stays a header.
	selfDetailTitleWords = 5
	// selfLabelWidth is the root list's title column. selfNarrowWidth is
	// where two columns stop fitting and the explainer moves to its own line
	// rather than being truncated into nonsense.
	selfLabelWidth  = 17
	selfNarrowWidth = 62
)

// selfSection is one root row. The explainer is not decoration: it is the only
// place a user learns what aforge was doing while they were away, so it lives
// beside the title rather than at the render site, and the empty hint teaches
// the same thing when there is nothing to count yet.
type selfSection struct {
	route   selfRoute
	title   string
	explain string
	empty   string
}

var selfSections = []selfSection{
	{
		route: selfRouteCrafts, title: "Know-how",
		explain: "what I've learned to repeat — versioned, measured, reused",
		empty:   "none yet; I keep one when a job's shape looks worth repeating",
	},
	{
		route: selfRouteCompetence, title: "Competence",
		explain: "where I'm strong and where I'm at my frontier — measured, not guessed",
		empty:   "nothing measured yet; a scope needs runs behind it before I'll claim anything",
	},
	{
		route: selfRouteBeliefs, title: "Beliefs",
		explain: "what I hold true about you and this machine — corrections welcome",
		empty:   "nothing yet; I write one down when work teaches me something durable",
	},
	{
		route: selfRouteSkills, title: "Skills",
		explain: "tools I built and checked; everything I run for you can reach them",
		empty:   "none yet; a procedure has to run and pass twice before I keep it",
	},
	{
		route: selfRouteWatches, title: "Watches",
		explain: "standing goals checking on their own schedule",
		empty:   "none yet; say \"whenever…\" or \"remind me…\" and I'll stand one up",
	},
	{
		route: selfRouteServices, title: "Services",
		explain: "processes I keep alive for you",
		empty:   "none running; I start one when work needs something to stay up",
	},
	{
		route: selfRoutePractice, title: "Practice",
		explain: "what I did with idle time, and what it taught me",
		empty:   "nothing yet; I practice in the quiet, inside the carve-out you set",
	},
	{
		route: selfRouteDials, title: "Dials",
		explain: "how I balance demand against curiosity, and what I may propose",
		empty:   "",
	},
}

func selfSectionFor(route selfRoute) selfSection {
	for _, section := range selfSections {
		if section.route == route {
			return section
		}
	}
	return selfSection{route: selfRouteRoot, title: "self"}
}

type selfRowAction uint8

const (
	selfRowInert selfRowAction = iota
	selfRowOpenSection
	selfRowOpenCraft
	selfRowOpenPractice
	selfRowOpenCharter
	selfRowOpenService
	selfRowShowMore
	selfRowUnfold
)

type selfRow struct {
	line   int
	action selfRowAction
	route  selfRoute
	key    string
}

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
	m.selfPaneStale = false
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
	if m.selfRoute == selfRouteRoot {
		return m.renderSelfRoot(width)
	}
	return m.renderSelfDrill(width)
}

// appendSelfRow lays one selectable row. Selection is a band and a marker,
// the same grammar the rail and the settings sheet use, so the hand never has
// to learn a second one.
func (m *Model) appendSelfRow(lines *[]string, width int, body string, row selfRow) {
	selected := m.focus == focusSelf && len(m.selfRows) == m.selfSelection
	marker := mutedStyle.Faint(true).Render("  ")
	if selected {
		marker = powderStyle.Bold(true).Render("▸ ")
	}
	row.line = len(*lines)
	m.selfRows = append(m.selfRows, row)
	line := truncate(marker+body, width)
	if selected {
		line = bandStyle.Width(width).Render(line)
	}
	*lines = append(*lines, line)
}

func appendSelfPlain(lines *[]string, width int, body string) {
	*lines = append(*lines, truncate(body, width))
}

// selfTodayLine is the one line that is always true: what today cost, what it
// taught, and how much of it was practice. It sits above every route so
// walking into a drill-in never loses the day.
func (m *Model) selfTodayLine(width int) string {
	parts := []string{"today: " + formatSelfDollars(m.selfSpend)}
	if learned := m.selfLearnedToday(); learned > 0 {
		parts = append(parts, fmt.Sprintf("%d learned", learned))
	}
	if practiced := m.selfPracticedToday(); practiced > 0 {
		parts = append(parts, "practiced "+shortDuration(practiced))
	}
	summary := mutedStyle.Faint(true).Render("· " + strings.Join(parts, " · "))
	return overlayRight(inputTextStyle.Render("self"), summary, width)
}

// selfLearnedToday counts distinct beliefs and skills named by today's
// receipts. A receipt may name the same fact twice; the user is being told how
// much aforge learned, not how many rows were written.
func (m *Model) selfLearnedToday() int {
	start := startOfLocalDay(m.standingTime())
	seen := make(map[int64]bool)
	for _, receipt := range m.selfReceipts {
		if !receipt.Time.IsZero() && receipt.Time.Before(start) {
			continue
		}
		for _, id := range receipt.FactIDs {
			seen[id] = true
		}
		for _, id := range receipt.SkillIDs {
			seen[id] = true
		}
	}
	return len(seen)
}

// selfPracticedToday sums the practice roots that started today, counting a
// live one up to now. Practice is the only self-directed work with a clock the
// user did not start, so the day's total is the honest report of it.
func (m *Model) selfPracticedToday() time.Duration {
	now := m.standingTime()
	start := startOfLocalDay(now)
	total := time.Duration(0)
	for _, node := range m.practiceRoots() {
		began := node.StartedAt
		if began.IsZero() || began.Before(start) {
			continue
		}
		ended := node.FinishedAt
		if ended.IsZero() {
			ended = now
		}
		if ended.After(began) {
			total += ended.Sub(began)
		}
	}
	return total
}

func (m *Model) practiceRoots() []store.Node {
	roots := make([]store.Node, 0, 4)
	seen := make(map[string]bool)
	for _, snapshot := range []store.Snapshot{m.cardSnapshot, m.snapshot} {
		for _, node := range snapshot.Nodes {
			if node.Parent != store.RootID || node.Group != store.PracticeGroup || seen[node.ID] {
				continue
			}
			seen[node.ID] = true
			roots = append(roots, node)
		}
	}
	sort.SliceStable(roots, func(i, j int) bool { return roots[i].CreatedSeq > roots[j].CreatedSeq })
	return roots
}

func shortDuration(value time.Duration) string {
	switch {
	case value < time.Minute:
		return fmt.Sprintf("%ds", int(value/time.Second))
	case value < time.Hour:
		return fmt.Sprintf("%dm", int(value/time.Minute))
	default:
		return fmt.Sprintf("%dh%02dm", int(value/time.Hour), int(value%time.Hour/time.Minute))
	}
}

func (m *Model) renderSelfRoot(width int) string {
	lines := []string{m.selfTodayLine(width), ""}
	narrow := width < selfNarrowWidth
	for _, section := range selfSections {
		count, counted := m.selfSectionCount(section.route)
		title := section.title
		if counted {
			title += " (" + strconv.Itoa(count) + ")"
		}
		explain := section.explain
		if counted && count == 0 && section.empty != "" {
			explain = section.empty
		}
		row := selfRow{action: selfRowOpenSection, route: section.route}
		if narrow {
			// A truncated explainer teaches nothing, so the narrow frame gives
			// the sentence its own lines rather than its first half.
			m.appendSelfRow(&lines, width, inputTextStyle.Render(title), row)
			for _, wrapped := range strings.Split(wrapText(explain, max(8, width-6)), "\n") {
				appendSelfPlain(&lines, width, mutedStyle.Faint(true).Render("      "+wrapped))
			}
			continue
		}
		body := inputTextStyle.Render(padANSI(truncate(title, selfLabelWidth-1), selfLabelWidth)) +
			mutedStyle.Faint(true).Render(explain)
		m.appendSelfRow(&lines, width, body, row)
	}
	lines = append(lines, "")
	appendSelfPlain(&lines, width, mutedStyle.Faint(true).Render("  enter opens · esc goes back"))
	return strings.Join(lines, "\n")
}

// selfSectionCount reports a row's count, and whether the row has one at all.
// Practice and Dials are states rather than collections: a number in front of
// them would be a number about nothing.
func (m *Model) selfSectionCount(route selfRoute) (int, bool) {
	switch route {
	case selfRouteCrafts:
		return len(m.selfCrafts), true
	case selfRouteCompetence:
		return len(m.selfCompetence.Scopes), true
	case selfRouteBeliefs:
		return len(m.selfFacts), true
	case selfRouteSkills:
		return len(m.selfSkills), true
	case selfRouteWatches:
		return len(m.selfLiveCharters()), true
	case selfRouteServices:
		return len(m.activeServices()), true
	}
	return 0, false
}

func (m *Model) selfLiveCharters() []store.Charter {
	live := make([]store.Charter, 0, len(m.selfCharters))
	for _, charter := range m.selfCharters {
		if charter.Status == store.CharterRetired {
			continue
		}
		live = append(live, charter)
	}
	return live
}

// Navigation. The root list is a list of places; a drill-in is a list of
// things; a craft is one thing. esc walks that ladder back out, and only when
// it is fully out does it hand the key on to the place ladder.

func (m *Model) openSelfRoute(route selfRoute) {
	m.selfRoute = route
	m.selfQuery = ""
	m.selfCraftName = ""
	m.selfCraftDetail = CraftDetail{}
	m.selfPracticeKey = ""
	m.selfShowOlder = false
	m.selfShown = selfWindow
	m.selfSelection = 0
	m.self.SetYOffset(0)
	if route == selfRouteBeliefs {
		m.loadSelfBeliefs()
	}
	m.refreshSelf()
}

// selfBack pops exactly one rung — a filter, then a craft, then a drill-in —
// and reports whether it had one to pop. The esc ladder in updateKey asks it
// before it considers leaving the place.
func (m *Model) selfBack() bool {
	switch {
	case m.selfQuery != "":
		m.selfQuery = ""
		if m.selfRoute == selfRouteBeliefs {
			m.loadSelfBeliefs()
		}
		m.selfSelection = 0
		m.refreshSelf()
		return true
	case m.selfCraftName != "":
		name := m.selfCraftName
		m.selfCraftName = ""
		m.selfCraftDetail = CraftDetail{}
		m.selfSelection = m.selfCraftIndex(name)
		m.refreshSelf()
		m.ensureSelfSelectionVisible()
		return true
	case m.selfPracticeKey != "":
		m.selfPracticeKey = ""
		m.selfSelection = 0
		m.refreshSelf()
		m.ensureSelfSelectionVisible()
		return true
	case m.selfRoute != selfRouteRoot:
		route := m.selfRoute
		m.selfRoute = selfRouteRoot
		m.selfShowOlder = false
		m.selfShown = selfWindow
		m.selfSelection = selfRootIndex(route)
		m.self.SetYOffset(0)
		m.refreshSelf()
		return true
	}
	return false
}

func selfRootIndex(route selfRoute) int {
	for index, section := range selfSections {
		if section.route == route {
			return index
		}
	}
	return 0
}

func (m *Model) selfCraftIndex(name string) int {
	for index, entry := range m.selfCrafts {
		if entry.Name == name {
			return index
		}
	}
	return 0
}

func (m *Model) moveSelfSelection(delta int) {
	if len(m.selfRows) == 0 {
		return
	}
	m.selfSelection = max(0, min(len(m.selfRows)-1, m.selfSelection+delta))
	m.refreshSelf()
	m.ensureSelfSelectionVisible()
}

func (m *Model) activateSelfSelection() tea.Cmd {
	if m.selfSelection < 0 || m.selfSelection >= len(m.selfRows) {
		return nil
	}
	return m.activateSelfRow(m.selfRows[m.selfSelection])
}

func (m *Model) activateSelfRow(row selfRow) tea.Cmd {
	switch row.action {
	case selfRowOpenSection:
		m.openSelfRoute(row.route)
	case selfRowOpenCraft:
		m.openSelfCraft(row.key)
	case selfRowOpenPractice:
		m.openSelfPractice(row.key)
	case selfRowShowMore:
		m.selfShown += selfWindowStep
		m.refreshSelf()
	case selfRowUnfold:
		m.selfShowOlder = true
		m.refreshSelf()
	case selfRowOpenCharter:
		// A charter's card, its firing history, and its actions already live
		// on the board. Self says which watches exist and takes the user to
		// the one surface that can act on them rather than growing a second.
		command := m.selectPlace(placeBoard)
		m.openStandingCharter(row.key)
		return command
	case selfRowOpenService:
		command := m.selectPlace(placeBoard)
		m.openServiceCard(row.key)
		return command
	}
	return nil
}

// updateSelfKey is the whole keyboard of the place. The root list takes j/k
// because there is nothing to type into it; a drill-in gives the letters back
// to the filter, because a list that has grown for six months is reached by
// typing, not by scrolling.
func (m *Model) updateSelfKey(message tea.KeyMsg) (tea.Cmd, bool) {
	key := message.String()
	switch key {
	case "up":
		m.moveSelfSelection(-1)
		return nil, true
	case "down":
		m.moveSelfSelection(1)
		return nil, true
	case "enter":
		return m.activateSelfSelection(), true
	case "home":
		m.selfSelection = 0
		m.refreshSelf()
		m.ensureSelfSelectionVisible()
		return nil, true
	case "end":
		m.selfSelection = max(0, len(m.selfRows)-1)
		m.refreshSelf()
		m.ensureSelfSelectionVisible()
		return nil, true
	}
	if m.selfRoute == selfRouteRoot {
		switch key {
		case "k":
			m.moveSelfSelection(-1)
			return nil, true
		case "j":
			m.moveSelfSelection(1)
			return nil, true
		case " ":
			return m.activateSelfSelection(), true
		}
		return nil, false
	}
	if !m.selfFilterable() {
		return nil, false
	}
	switch {
	case key == "backspace" || key == "ctrl+h":
		if m.selfQuery == "" {
			return nil, true
		}
		runes := []rune(m.selfQuery)
		m.setSelfQuery(string(runes[:len(runes)-1]))
		return nil, true
	case key == "ctrl+u":
		m.setSelfQuery("")
		return nil, true
	case message.Type == tea.KeyRunes && !message.Alt && len(message.Runes) > 0:
		m.setSelfQuery(m.selfQuery + string(message.Runes))
		return nil, true
	}
	return nil, false
}

// selfFilterable says which drill-ins take a query. A craft's steps and the
// dials are short by construction; typing into them would be a prompt with
// nothing to answer.
func (m *Model) selfFilterable() bool {
	if m.selfCraftName != "" || m.selfPracticeKey != "" {
		return false
	}
	switch m.selfRoute {
	case selfRouteCrafts, selfRouteCompetence, selfRouteBeliefs,
		selfRouteSkills, selfRouteWatches, selfRouteServices, selfRoutePractice:
		return true
	}
	return false
}

func (m *Model) setSelfQuery(query string) {
	m.selfQuery = query
	m.selfShown = selfWindow
	m.selfSelection = 0
	if m.selfRoute == selfRouteBeliefs {
		m.loadSelfBeliefs()
	}
	m.refreshSelf()
	m.self.SetYOffset(0)
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

func (m *Model) activateSelfAt(x, y int) (tea.Cmd, bool) {
	if !m.selfBounds.contains(x, y) {
		return nil, false
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
		m.refreshSelf()
		return m.activateSelfRow(row), true
	}
	return nil, true
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
	return strings.Join(lines, "\n")
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
