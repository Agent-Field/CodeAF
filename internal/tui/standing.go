package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// charterGroupMarker is the temporary read seam for the standing-engine
// branch. The durable charter API is intentionally not duplicated here: this
// adapter projects marked graph nodes into display-only values, and every
// renderer below consumes only those values.
const charterGroupMarker = "charter"

const (
	charterFieldPrefix       = charterGroupMarker + ":"
	charterFiringGroupPrefix = charterGroupMarker + ":firing:"
	standingGraphRowPrefix   = "\x00standing:"
	charterHistoryLimit      = 5
)

// standingReader is the narrow compatibility seam for the charter store work
// landing on another branch. A future store-native query only needs to return
// this presentation projection; rendering and interaction stay unchanged.
type standingReader interface {
	Charters(store.Snapshot, map[string]store.JobUsage, time.Time) []standingCharter
}

type snapshotStandingReader struct{}

type standingCharter struct {
	ID             string
	Name           string
	Invariant      string
	Watch          string
	Quote          string
	Cap            string
	Expiry         string
	State          string
	ProposalReason string
	Proposed       bool
	Breathing      bool
	LastFired      time.Time
	Today          int
	Firings        []standingFiring
}

type standingFiring struct {
	JobID   string
	At      time.Time
	Cost    float64
	Outcome string
	Status  store.Status
	Active  bool
}

// Charter roots use Group="charter". Until the store-native charter view is
// merged, optional child nodes expose watch/rails/state fields with groups
// charter:watch, charter:quote, charter:cap, charter:expiry, charter:state,
// and charter:proposal. Trigger jobs point back through
// Group="charter:firing:<charter-id>". No renderer knows this encoding.
func (snapshotStandingReader) Charters(
	snapshot store.Snapshot,
	usage map[string]store.JobUsage,
	now time.Time,
) []standingCharter {
	children := make(map[string][]store.Node)
	for _, node := range snapshot.Nodes {
		children[node.Parent] = append(children[node.Parent], node)
	}

	charters := make([]standingCharter, 0)
	byID := make(map[string]int)
	for _, node := range snapshot.Nodes {
		if node.Parent != store.RootID || node.Group != charterGroupMarker {
			continue
		}
		charter := standingCharter{
			ID:        node.ID,
			Name:      strings.TrimSpace(node.Title),
			Invariant: strings.TrimSpace(node.Provenance.Intent),
			Watch:     strings.TrimSpace(node.Brief),
			State:     "active",
			Proposed:  node.Provenance.Origin == store.OriginSelf,
			Breathing: node.Status == store.Claimed || node.Status == store.Running,
		}
		if charter.Name == "" {
			charter.Name = nodeLabel(node)
		}
		if node.Status == store.Cancelled {
			charter.State = "paused"
		}
		for _, child := range children[node.ID] {
			value := charterFieldValue(child)
			switch child.Group {
			case charterFieldPrefix + "watch":
				charter.Watch = value
			case charterFieldPrefix + "quote":
				charter.Quote = value
			case charterFieldPrefix + "cap":
				charter.Cap = value
			case charterFieldPrefix + "expiry":
				charter.Expiry = value
			case charterFieldPrefix + "state":
				if value != "" {
					charter.State = strings.ToLower(value)
				}
			case charterFieldPrefix + "proposal":
				charter.ProposalReason = value
				charter.Proposed = true
			}
		}
		charters = append(charters, charter)
		byID[node.ID] = len(charters) - 1
	}

	for _, node := range snapshot.Nodes {
		charterID, ok := firingCharterID(node, byID)
		if !ok {
			continue
		}
		at := node.FinishedAt
		if at.IsZero() {
			at = node.StartedAt
		}
		outcome := strings.TrimSpace(node.Summary)
		if node.Status == store.Failed || node.Status == store.Cancelled {
			outcome = strings.TrimSpace(node.Error)
		}
		if outcome == "" {
			outcome = string(node.Status)
		}
		active := node.Status == store.Claimed || node.Status == store.Running
		firing := standingFiring{
			JobID: node.ID, At: at, Cost: usage[node.ID].Cost,
			Outcome: firstLine(outcome), Status: node.Status, Active: active,
		}
		index := byID[charterID]
		charters[index].Firings = append(charters[index].Firings, firing)
		charters[index].Breathing = charters[index].Breathing || active
		if !at.IsZero() && (charters[index].LastFired.IsZero() || at.After(charters[index].LastFired)) {
			charters[index].LastFired = at
		}
		if sameLocalDay(at, now) {
			charters[index].Today++
		}
	}

	for index := range charters {
		sort.SliceStable(charters[index].Firings, func(i, j int) bool {
			return charters[index].Firings[i].At.After(charters[index].Firings[j].At)
		})
		if len(charters[index].Firings) > charterHistoryLimit {
			charters[index].Firings = charters[index].Firings[:charterHistoryLimit]
		}
	}
	return charters
}

func charterFieldValue(node store.Node) string {
	for _, value := range []string{node.Brief, node.Summary, node.Title} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func firingCharterID(node store.Node, charters map[string]int) (string, bool) {
	if node.Provenance.Origin != store.OriginTrigger {
		return "", false
	}
	if strings.HasPrefix(node.Group, charterFiringGroupPrefix) {
		id := strings.TrimPrefix(node.Group, charterFiringGroupPrefix)
		_, ok := charters[id]
		return id, ok
	}
	// A firing may also be nested directly under its charter in a compatibility
	// snapshot. Store-native firing jobs remain ordinary top-level jobs.
	_, ok := charters[node.Parent]
	return node.Parent, ok
}

func sameLocalDay(left, right time.Time) bool {
	if left.IsZero() || right.IsZero() {
		return false
	}
	left = left.In(right.Location())
	ly, lm, ld := left.Date()
	ry, rm, rd := right.Date()
	return ly == ry && lm == rm && ld == rd
}

func (m *Model) standingSnapshot() store.Snapshot {
	if len(m.cardSnapshot.Nodes) > 0 {
		return m.cardSnapshot
	}
	return m.snapshot
}

func (m *Model) standingTime() time.Time {
	if m.standingNow != nil {
		return m.standingNow()
	}
	return time.Now()
}

func (m *Model) standingCharters() []standingCharter {
	reader := m.standingReader
	if reader == nil {
		reader = snapshotStandingReader{}
	}
	return reader.Charters(m.standingSnapshot(), m.jobUsage, m.standingTime())
}

func (m *Model) standingCharter(charterID string) (standingCharter, bool) {
	for _, charter := range m.standingCharters() {
		if charter.ID == charterID {
			return charter, true
		}
	}
	return standingCharter{}, false
}

func standingGraphRowID(charterID string) string { return standingGraphRowPrefix + charterID }

func charterIDFromGraphRow(rowID string) (string, bool) {
	if !strings.HasPrefix(rowID, standingGraphRowPrefix) {
		return "", false
	}
	return strings.TrimPrefix(rowID, standingGraphRowPrefix), true
}

func (m *Model) standingSectionHeight() int {
	if !m.graphVisible() || m.graphScopeID != "" || m.charterCardID != "" {
		return 0
	}
	count := len(m.standingCharters())
	if count == 0 {
		return 0
	}
	// header + one row per charter + the separating blank before tasks.
	return count + 2
}

type standingRow struct {
	line      int
	charterID string
}

func (m *Model) renderStandingSection(width int) string {
	m.standingRows = m.standingRows[:0]
	charters := m.standingCharters()
	if len(charters) == 0 {
		return ""
	}
	lines := []string{mutedStyle.Faint(true).Render("standing")}
	for _, charter := range charters {
		row := len(lines)
		marker := "  "
		if m.focus == focusGraph && m.selectedNodeID == standingGraphRowID(charter.ID) {
			marker = lipgloss.NewStyle().Foreground(powder).Bold(true).Render("▸ ")
		}
		line := fmt.Sprintf("⏱ %s · last fired %s · %d today",
			charter.Name, standingAge(charter.LastFired, m.standingTime()), charter.Today)
		if charter.Proposed {
			line += " · proposed"
		}
		body := mutedStyle.Faint(true).Render(line)
		if charter.Breathing {
			body = m.matteSweep(line)
		}
		line = truncate(marker+body, width)
		if m.focus == focusGraph && m.selectedNodeID == standingGraphRowID(charter.ID) {
			line = lipgloss.NewStyle().Background(selectionBand).Width(width).Render(line)
		}
		lines = append(lines, line)
		m.standingRows = append(m.standingRows, standingRow{line: row, charterID: charter.ID})
	}
	lines = append(lines, "")
	return strings.Join(lines, "\n")
}

func standingAge(at, now time.Time) string {
	if at.IsZero() {
		return "never"
	}
	if !now.After(at) {
		return "now"
	}
	age := now.Sub(at)
	switch {
	case age < time.Minute:
		return "now"
	case age < time.Hour:
		return fmt.Sprintf("%dm", int(age/time.Minute))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh", int(age/time.Hour))
	default:
		return fmt.Sprintf("%dd", int(age/(24*time.Hour)))
	}
}

func (m *Model) standingBreathing() bool {
	if !m.graphVisible() || m.graphScopeID != "" || m.charterCardID != "" {
		return false
	}
	for _, charter := range m.standingCharters() {
		if charter.Breathing {
			return true
		}
	}
	return false
}

type charterCardRowKind uint8

const (
	charterHistoryRow charterCardRowKind = iota
	charterActionRow
)

type charterCardRow struct {
	line   int
	kind   charterCardRowKind
	jobID  string
	action string
}

func (m *Model) renderCharterCardBody(width int) string {
	m.charterRows = m.charterRows[:0]
	charter, ok := m.standingCharter(m.charterCardID)
	if !ok {
		return mutedStyle.Render("charter is no longer available")
	}
	width = max(12, width)
	state := charter.State
	if state == "" {
		state = "active"
	}
	if charter.Proposed {
		state = "proposed"
	}
	lines := []string{mutedStyle.Faint(true).Render("╭─ ") + mutedStyle.Render(state)}
	appendLabel := func(label string) {
		lines = append(lines, mutedStyle.Faint(true).Render("│   "+label))
	}
	appendText := func(value string, style lipgloss.Style) {
		value = strings.TrimSpace(value)
		if value == "" {
			value = "—"
		}
		for _, line := range strings.Split(wrapText(value, max(1, width-4)), "\n") {
			lines = append(lines, mutedStyle.Faint(true).Render("│ ")+style.Render(line))
		}
	}
	appendLabel("invariant")
	appendText(charter.Invariant, youTextStyle)
	appendLabel("watch")
	appendText(charter.Watch, mutedStyle)
	appendLabel("rails")
	appendText("per firing · "+displayOrDash(charter.Quote), mutedStyle)
	appendText("cap · "+displayOrDash(charter.Cap), mutedStyle)
	appendText("expiry · "+displayOrDash(charter.Expiry), mutedStyle)
	if charter.ProposalReason != "" {
		appendText("proposed · "+charter.ProposalReason, mutedStyle)
	}
	appendLabel("firing history")
	if len(charter.Firings) == 0 {
		appendText("no firings yet", mutedStyle)
	}
	for _, firing := range charter.Firings {
		outcome := strings.TrimSpace(firing.Outcome)
		if outcome == "" {
			outcome = string(firing.Status)
		}
		text := fmt.Sprintf("▸ %s · %s · %s",
			relativeTime(firing.At, m.standingTime()), formatCardCost(firing.Cost), outcome)
		m.appendCharterRow(&lines, width, text, charterCardRow{
			kind: charterHistoryRow, jobID: firing.JobID,
		})
	}
	appendLabel("actions")
	for _, action := range []string{"pause", "resume", "retire", "edit cadence"} {
		m.appendCharterRow(&lines, width, "▸ "+action, charterCardRow{
			kind: charterActionRow, action: action,
		})
	}
	lines = append(lines, mutedStyle.Faint(true).Render("╰─"))
	return strings.Join(lines, "\n")
}

func displayOrDash(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return "—"
}

func (m *Model) appendCharterRow(lines *[]string, width int, text string, row charterCardRow) {
	row.line = len(*lines)
	selected := len(m.charterRows) == m.charterFocusIndex && m.focus == focusGraph
	prefix := mutedStyle.Faint(true).Render("│   ")
	view := prefix + mutedStyle.Render(truncate(text, max(1, width-lipgloss.Width(prefix))))
	if selected {
		marker, body := "", text
		if strings.HasPrefix(text, "▸ ") {
			marker, body = "▸ ", strings.TrimPrefix(text, "▸ ")
		}
		view = lipgloss.NewStyle().Background(selectionBand).Width(width).Render(
			prefix + lipgloss.NewStyle().Foreground(powder).Render(marker) + mutedStyle.Render(body),
		)
	}
	*lines = append(*lines, truncate(view, width))
	m.charterRows = append(m.charterRows, row)
}

func (m *Model) openStandingCharter(charterID string) {
	if _, ok := m.standingCharter(charterID); !ok {
		return
	}
	m.charterCardID = charterID
	m.charterFocusIndex = 0
	m.selectedNodeID = standingGraphRowID(charterID)
	m.graph.SetYOffset(0)
	m.setSize(m.width, m.height)
}

func (m *Model) closeCharterCard() bool {
	if m.charterCardID == "" || m.graphScopeID != "" {
		return false
	}
	id := m.charterCardID
	m.charterCardID = ""
	m.charterFocusIndex = 0
	m.selectedNodeID = standingGraphRowID(id)
	m.graph.SetYOffset(0)
	m.setSize(m.width, m.height)
	return true
}

func (m *Model) ensureCharterSelection() {
	if m.charterCardID == "" {
		return
	}
	if len(m.charterRows) == 0 {
		m.refreshGraph()
	}
	if len(m.charterRows) == 0 {
		m.charterFocusIndex = 0
		return
	}
	m.charterFocusIndex = max(0, min(m.charterFocusIndex, len(m.charterRows)-1))
	m.ensureCharterSelectionVisible()
}

func (m *Model) moveCharterSelection(delta int) {
	m.ensureCharterSelection()
	if len(m.charterRows) == 0 {
		return
	}
	m.charterFocusIndex = max(0, min(len(m.charterRows)-1, m.charterFocusIndex+delta))
	m.refreshGraph()
	m.ensureCharterSelectionVisible()
}

func (m *Model) ensureCharterSelectionVisible() {
	if len(m.charterRows) == 0 {
		return
	}
	line := m.charterRows[max(0, min(m.charterFocusIndex, len(m.charterRows)-1))].line
	if line < m.graph.YOffset {
		m.graph.SetYOffset(line)
	} else if line >= m.graph.YOffset+max(1, m.graph.Height) {
		m.graph.SetYOffset(line - max(1, m.graph.Height) + 1)
	}
}

func (m *Model) activateCharterSelection() tea.Cmd {
	m.ensureCharterSelection()
	if len(m.charterRows) == 0 {
		return nil
	}
	row := m.charterRows[m.charterFocusIndex]
	if row.kind == charterHistoryRow {
		m.graphScopeID = row.jobID
		m.cardReturnFocus = focusGraph
		m.selectedNodeID = row.jobID
		m.graph.SetYOffset(0)
		m.setSize(m.width, m.height)
		return nil
	}
	return m.requestCharterAction(row.action)
}

func (m *Model) activateCharterLine(line int) (tea.Cmd, bool) {
	for index, row := range m.charterRows {
		if row.line != line {
			continue
		}
		m.charterFocusIndex = index
		m.refreshGraph()
		return m.activateCharterSelection(), true
	}
	return nil, false
}

type commandRequester interface {
	RequestCommand(store.Command) (store.Command, error)
}

type charterCommandResultMsg struct {
	action string
	name   string
	err    error
}

func (m *Model) requestCharterAction(action string) tea.Cmd {
	charter, ok := m.standingCharter(m.charterCardID)
	if !ok {
		return m.showStatus("charter is no longer available")
	}
	requester, ok := m.backend.(commandRequester)
	if !ok {
		return m.showStatus("charter actions unavailable — command requests unsupported")
	}
	m.status = action + " requested → " + charter.Name
	m.statusUntil = time.Now().Add(statusTTL)
	request := store.Command{
		SessionID:   m.sessionID,
		Kind:        store.CommandAmend,
		Target:      charter.ID,
		Instruction: action,
	}
	return func() tea.Msg {
		_, err := requester.RequestCommand(request)
		return charterCommandResultMsg{action: action, name: charter.Name, err: err}
	}
}

// charterDefinitionIDs identifies the non-task definition subtree so counts,
// cards, and the task tree can exclude it while trigger-born firing jobs keep
// behaving as ordinary jobs.
func charterDefinitionIDs(snapshot store.Snapshot) map[string]bool {
	children := make(map[string][]string)
	marked := make(map[string]bool)
	for _, node := range snapshot.Nodes {
		children[node.Parent] = append(children[node.Parent], node.ID)
		if node.Parent == store.RootID && node.Group == charterGroupMarker {
			marked[node.ID] = true
		}
	}
	var markChildren func(string)
	markChildren = func(parent string) {
		for _, child := range children[parent] {
			if marked[child] {
				continue
			}
			marked[child] = true
			markChildren(child)
		}
	}
	for id := range marked {
		markChildren(id)
	}
	return marked
}
