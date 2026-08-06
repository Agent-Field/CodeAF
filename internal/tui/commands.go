package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

type paletteKind int

const (
	paletteNone paletteKind = iota
	paletteCommands
	paletteModelCompletion
	paletteCancelCompletion
	paletteModel
	paletteMemory
	paletteHelp
)

type commandSpec struct {
	name        string
	description string
	takesArg    bool
}

// keyBindings is the single source for routed chords and every help label
// that names them. Bubble Tea reports option-G as the literal "alt+g".
var keyBindings = struct {
	graph string
}{
	graph: "alt+g",
}

var slashCommands = []commandSpec{
	{name: "graph", description: "toggle the task rail"},
	{name: "tasks", description: "focus and expand the active-task dock"},
	{name: "node", description: "open a node by id prefix or current selection", takesArg: true},
	{name: "notebook", description: "browse the scoped notebook"},
	{name: "help", description: "show commands and keyboard shortcuts"},
	{name: "model", description: "choose the talk or work model", takesArg: true},
	{name: "memory", description: "alias for /notebook"},
	{name: "session", description: "show the current session and database"},
	{name: "new", description: "start a fresh chat session"},
	{name: "cancel", description: "cancel a non-terminal graph node", takesArg: true},
	{name: "quit", description: "exit aforge cleanly"},
}

type paletteEntry struct {
	value       string
	description string
	score       int
}

func (m *Model) paletteOpen() bool { return m.palette != paletteNone }

func (m *Model) closePalette() {
	if m.palette == paletteModel {
		m.input.Reset()
	}
	m.palette = paletteNone
	m.paletteSelected = 0
	m.setSize(m.width, m.height)
}

func (m *Model) syncPalette() {
	if m.palette == paletteModel || m.palette == paletteMemory || m.palette == paletteHelp {
		return
	}
	value := m.input.Value()
	if m.paletteDismissed || !strings.HasPrefix(value, "/") {
		m.palette = paletteNone
		return
	}
	command, _, hasArgument := strings.Cut(value, " ")
	switch {
	case command == "/model" && hasArgument:
		m.palette = paletteModelCompletion
	case command == "/cancel" && hasArgument:
		m.palette = paletteCancelCompletion
	default:
		m.palette = paletteCommands
	}
	m.clampPaletteSelection()
}

func (m *Model) updatePaletteKey(key string) (tea.Cmd, bool) {
	switch m.palette {
	case paletteModel:
		choices := m.filteredModelChoices()
		switch key {
		case "tab", "left", "right":
			m.switchModelRole()
			return nil, true
		case "up":
			m.cyclePalette(-1, len(choices))
			return nil, true
		case "down":
			m.cyclePalette(1, len(choices))
			return nil, true
		case "enter":
			return m.applySelectedModel(choices), true
		}
	case paletteMemory:
		switch key {
		case "up":
			m.scrollMemory(-1)
			return nil, true
		case "down":
			m.scrollMemory(1)
			return nil, true
		case "pgup":
			m.scrollMemory(-max(1, m.paletteLineLimit()-1))
			return nil, true
		case "pgdown":
			m.scrollMemory(max(1, m.paletteLineLimit()-1))
			return nil, true
		case "home":
			m.paletteSelected = 0
			return nil, true
		case "end":
			m.paletteSelected = max(0, m.memoryLineCount()-m.paletteLineLimit())
			return nil, true
		case "enter", "tab":
			return nil, true
		}
	case paletteHelp:
		if key == "enter" {
			m.closePalette()
			return nil, true
		}
		if key == "tab" || key == "up" || key == "down" {
			return nil, true
		}
	case paletteCommands:
		entries := m.commandEntries()
		switch key {
		case "tab", "down":
			m.cyclePalette(1, len(entries))
			return nil, true
		case "up":
			m.cyclePalette(-1, len(entries))
			return nil, true
		case "enter":
			return m.acceptCommand(entries), true
		}
	case paletteModelCompletion:
		entries := m.modelEntries()
		switch key {
		case "down":
			m.cyclePalette(1, len(entries))
			return nil, true
		case "up":
			m.cyclePalette(-1, len(entries))
			return nil, true
		case "tab":
			m.completeModel(entries)
			return nil, true
		case "enter":
			role, partial := parseModelArgument(m.input.Value())
			if partial == "" {
				return m.openModelPicker(role), true
			}
			return m.applySelectedModelEntries(entries), true
		}
	case paletteCancelCompletion:
		entries := m.cancelEntries()
		switch key {
		case "down":
			m.cyclePalette(1, len(entries))
			return nil, true
		case "up":
			m.cyclePalette(-1, len(entries))
			return nil, true
		case "tab":
			m.completeCancel(entries)
			return nil, true
		case "enter":
			return m.cancelSelected(entries), true
		}
	}
	return nil, false
}

func (m *Model) cyclePalette(delta, count int) {
	if count == 0 {
		m.paletteSelected = 0
		return
	}
	m.paletteSelected = (m.paletteSelected + delta + count) % count
}

func (m *Model) clampPaletteSelection() {
	count := len(m.paletteEntries())
	if count == 0 {
		m.paletteSelected = 0
	} else if m.paletteSelected >= count {
		m.paletteSelected = count - 1
	}
}

func (m *Model) paletteEntries() []paletteEntry {
	switch m.palette {
	case paletteCommands:
		return m.commandEntries()
	case paletteModelCompletion:
		return m.modelEntries()
	case paletteCancelCompletion:
		return m.cancelEntries()
	}
	return nil
}

func (m *Model) commandEntries() []paletteEntry {
	query := strings.TrimPrefix(strings.Fields(m.input.Value())[0], "/")
	entries := make([]paletteEntry, 0, len(slashCommands))
	for _, command := range slashCommands {
		if score, ok := fuzzyScore(command.name, query); ok {
			entries = append(entries, paletteEntry{
				value: command.name, description: command.description, score: score,
			})
		}
	}
	sortEntries(entries)
	return entries
}

func (m *Model) modelEntries() []paletteEntry {
	_, partial := parseModelArgument(m.input.Value())
	return filterEntries(m.models(), partial)
}

func (m *Model) cancelEntries() []paletteEntry {
	_, partial, _ := strings.Cut(m.input.Value(), " ")
	return filterEntries(m.cancellableNodeIDs(), strings.TrimSpace(partial))
}

func filterEntries(values []string, query string) []paletteEntry {
	entries := make([]paletteEntry, 0, len(values))
	for _, value := range values {
		if score, ok := fuzzyScore(value, query); ok {
			entries = append(entries, paletteEntry{value: value, score: score})
		}
	}
	sortEntries(entries)
	return entries
}

func sortEntries(entries []paletteEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].score < entries[j].score
	})
}

func fuzzyScore(value, query string) (int, bool) {
	haystack := []rune(strings.ToLower(value))
	needle := []rune(strings.ToLower(strings.TrimSpace(query)))
	if len(needle) == 0 {
		return 0, true
	}
	position, score := 0, 0
	for _, wanted := range needle {
		found := false
		for position < len(haystack) {
			if haystack[position] == wanted {
				score += position
				position++
				found = true
				break
			}
			position++
		}
		if !found {
			return 0, false
		}
	}
	if strings.HasPrefix(strings.ToLower(value), strings.ToLower(strings.TrimSpace(query))) {
		score -= 100
	}
	return score, true
}

func (m *Model) acceptCommand(entries []paletteEntry) tea.Cmd {
	if len(entries) == 0 {
		m.input.Reset()
		return m.showStatus("no matching command")
	}
	selected := entries[min(m.paletteSelected, len(entries)-1)].value
	typed := strings.TrimPrefix(strings.Fields(m.input.Value())[0], "/")
	command := commandByName(selected)
	if typed != selected {
		value := "/" + selected
		if command.takesArg {
			value += " "
		}
		m.input.SetValue(value)
		m.paletteSelected = 0
		m.syncPalette()
		m.setSize(m.width, m.height)
		return nil
	}
	return m.executeSlash(m.input.Value())
}

func commandByName(name string) commandSpec {
	for _, command := range slashCommands {
		if command.name == name {
			return command
		}
	}
	return commandSpec{}
}

func (m *Model) completeModel(entries []paletteEntry) {
	if len(entries) == 0 {
		return
	}
	role, _ := parseModelArgument(m.input.Value())
	slug := entries[min(m.paletteSelected, len(entries)-1)].value
	value := "/model "
	if role == "work" {
		value += "work "
	}
	m.input.SetValue(value + slug)
	m.paletteSelected = 0
	m.syncPalette()
	m.setSize(m.width, m.height)
}

func (m *Model) applySelectedModelEntries(entries []paletteEntry) tea.Cmd {
	if len(entries) == 0 {
		return m.showStatus("no matching model")
	}
	role, _ := parseModelArgument(m.input.Value())
	return m.applyModel(role, entries[min(m.paletteSelected, len(entries)-1)].value)
}

func (m *Model) applySelectedModel(choices []ModelChoice) tea.Cmd {
	if len(choices) == 0 {
		return m.showStatus("model switching unavailable — no models configured")
	}
	return m.applyModel(m.modelRole, choices[min(m.paletteSelected, len(choices)-1)].Slug)
}

func (m *Model) applyModel(role, slug string) tea.Cmd {
	if m.commander == nil {
		return m.showStatus("model switching unavailable — no Commander")
	}
	if err := m.commander.SetModel(role, slug); err != nil {
		return m.showStatus(fmt.Sprintf("could not set %s model: %v", role, err))
	}
	m.input.Reset()
	m.paletteDismissed = false
	return m.showStatus(fmt.Sprintf("%s model → %s", role, slug))
}

func (m *Model) completeCancel(entries []paletteEntry) {
	if len(entries) == 0 {
		return
	}
	nodeID := entries[min(m.paletteSelected, len(entries)-1)].value
	m.input.SetValue("/cancel " + nodeID)
	m.paletteSelected = 0
	m.syncPalette()
	m.setSize(m.width, m.height)
}

func (m *Model) cancelSelected(entries []paletteEntry) tea.Cmd {
	if len(entries) == 0 {
		return m.showStatus("no matching non-terminal node")
	}
	return m.cancelNode(entries[min(m.paletteSelected, len(entries)-1)].value)
}

func (m *Model) cancelNode(nodeID string) tea.Cmd {
	if m.commander == nil {
		return m.showStatus("cancel unavailable — no Commander")
	}
	if err := m.commander.Cancel(nodeID); err != nil {
		return m.showStatus(fmt.Sprintf("could not cancel %s: %v", nodeID, err))
	}
	m.input.Reset()
	m.paletteDismissed = false
	m.palette = paletteNone
	return m.showStatus("cancel requested → " + nodeID)
}

func (m *Model) executeSlash(body string) tea.Cmd {
	fields := strings.Fields(body)
	if len(fields) == 0 {
		return nil
	}
	switch fields[0] {
	case "/":
		m.input.Reset()
		return m.showStatus("/graph · /tasks · /node · /notebook · /help")
	case "/graph":
		m.input.Reset()
		if m.nodeViewID != "" {
			m.closeNodeView()
		}
		m.toggleGraph()
		return nil
	case "/tasks":
		m.input.Reset()
		return m.openTasksDock()
	case "/node":
		prefix := ""
		if len(fields) > 1 {
			prefix = fields[1]
		}
		return m.openNodePrefix(prefix)
	case "/notebook", "/memory":
		return m.openMemory()
	case "/model":
		role := "talk"
		arguments := fields[1:]
		if len(arguments) > 0 && (arguments[0] == "talk" || arguments[0] == "work") {
			role = arguments[0]
			arguments = arguments[1:]
		}
		if len(arguments) == 0 {
			return m.openModelPicker(role)
		}
		entries := filterEntries(m.models(), strings.Join(arguments, " "))
		if len(entries) == 0 {
			return m.showStatus("no matching model")
		}
		return m.applyModel(role, entries[0].value)
	case "/session":
		detail := "session " + m.sessionID
		if source, ok := m.commander.(interface{ DatabasePath() string }); ok && source.DatabasePath() != "" {
			detail += " · " + source.DatabasePath()
		} else {
			detail += " · database unavailable"
		}
		m.input.Reset()
		return m.showStatus(detail)
	case "/new":
		return m.newSession()
	case "/cancel":
		if len(fields) < 2 {
			m.input.SetValue("/cancel ")
			m.palette = paletteCancelCompletion
			m.setSize(m.width, m.height)
			return nil
		}
		return m.cancelNode(fields[1])
	case "/help":
		m.input.Reset()
		m.palette = paletteHelp
		m.paletteSelected = 0
		m.setSize(m.width, m.height)
		return nil
	case "/quit":
		return tea.Quit
	default:
		m.input.Reset()
		return m.showStatus(fields[0] + " is not a command · /help lists them")
	}
}

func (m *Model) openTasksDock() tea.Cmd {
	if m.nodeViewID != "" {
		m.closeNodeView()
	}
	if m.graphVisible() {
		m.toggleGraph()
	}
	if m.activeCardCount() == 0 {
		return m.showStatus("no active tasks to expand")
	}
	m.focusCardDock()
	return nil
}

func (m *Model) openNodePrefix(prefix string) tea.Cmd {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = m.selectedNodeID
	}
	if prefix == "" && m.nodeViewID != "" {
		prefix = m.nodeViewID
	}
	if prefix == "" {
		m.input.Reset()
		return m.showStatus("/node needs an id prefix or selected node")
	}

	seen := make(map[string]bool)
	var matches []string
	for _, snapshot := range []store.Snapshot{m.snapshot, m.cardSnapshot} {
		for _, node := range snapshot.Nodes {
			if node.ID == store.RootID || seen[node.ID] || !strings.HasPrefix(node.ID, prefix) {
				continue
			}
			seen[node.ID] = true
			if node.ID == prefix {
				matches = []string{node.ID}
				break
			}
			matches = append(matches, node.ID)
		}
		if len(matches) == 1 && matches[0] == prefix {
			break
		}
	}
	m.input.Reset()
	switch len(matches) {
	case 0:
		return m.showStatus("no node starts with " + prefix)
	case 1:
		if m.nodeViewID != "" {
			m.closeNodeView()
		}
		return m.openNodeByID(matches[0])
	default:
		return m.showStatus(fmt.Sprintf("%s matches %d nodes · type more of the id", prefix, len(matches)))
	}
}

func (m *Model) openMemory() tea.Cmd {
	if m.commander == nil {
		return m.showStatus("memory unavailable — no Commander")
	}
	m.input.Reset()
	m.memoryFacts = m.commander.Notebook(100)
	m.palette = paletteMemory
	m.paletteSelected = 0
	m.setSize(m.width, m.height)
	return nil
}

func (m *Model) scrollMemory(delta int) {
	maximum := max(0, m.memoryLineCount()-m.paletteLineLimit())
	m.paletteSelected = max(0, min(maximum, m.paletteSelected+delta))
}

func (m *Model) memoryLineCount() int {
	if len(m.memoryFacts) == 0 {
		return 1
	}
	count := len(m.memoryFacts)
	seen := make(map[string]bool)
	for _, fact := range m.memoryFacts {
		scope := strings.TrimSpace(fact.Scope)
		if !seen[scope] {
			seen[scope] = true
			count++
		}
	}
	return count
}

func (m *Model) openModelPicker(role string) tea.Cmd {
	if m.commander == nil {
		return m.showStatus("model switching unavailable — no Commander")
	}
	fallback := m.fallbackModelChoices()
	if len(fallback) == 0 {
		return m.showStatus("model switching unavailable — no models configured")
	}
	m.input.Reset()
	m.modelRole = role
	m.palette = paletteModel
	if len(m.modelCatalog) == 0 {
		m.modelCatalog = fallback
	}
	m.paletteSelected = indexModelChoice(m.modelCatalog, m.commander.CurrentModel(role))
	if !m.catalogRequested {
		m.catalogLoading = true
	}
	m.setSize(m.width, m.height)
	if m.catalogRequested {
		return nil
	}
	m.catalogRequested = true
	commander := m.commander
	return func() tea.Msg {
		return catalogResultMsg{choices: commander.Catalog()}
	}
}

func (m *Model) switchModelRole() {
	if m.modelRole == "talk" {
		m.modelRole = "work"
	} else {
		m.modelRole = "talk"
	}
	choices := m.filteredModelChoices()
	if m.commander != nil && strings.TrimSpace(m.input.Value()) == "" {
		m.paletteSelected = indexModelChoice(choices, m.commander.CurrentModel(m.modelRole))
	} else {
		m.paletteSelected = min(m.paletteSelected, max(0, len(choices)-1))
	}
}

func (m *Model) applyCatalog(choices []ModelChoice) {
	m.catalogLoading = false
	if normalized := normalizeModelChoices(choices); len(normalized) > 0 {
		m.modelCatalog = normalized
	}
	if m.palette != paletteModel {
		return
	}
	filtered := m.filteredModelChoices()
	if strings.TrimSpace(m.input.Value()) == "" && m.commander != nil {
		m.paletteSelected = indexModelChoice(filtered, m.commander.CurrentModel(m.modelRole))
	} else if len(filtered) == 0 {
		m.paletteSelected = 0
	} else {
		m.paletteSelected = min(m.paletteSelected, len(filtered)-1)
	}
	m.setSize(m.width, m.height)
}

func (m *Model) newSession() tea.Cmd {
	if m.commander == nil {
		return m.showStatus("new session unavailable — no Commander")
	}
	sessionID, err := m.commander.NewSession()
	if err != nil {
		return m.showStatus(fmt.Sprintf("could not start a new session: %v", err))
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return m.showStatus("could not start a new session: empty id")
	}
	m.sessionID = sessionID
	m.messages = nil
	m.lastSeq = 0
	m.cards = nil
	m.commands = map[int64]store.Command{}
	m.cardExpanded = map[string]bool{}
	m.selectedCardID = ""
	m.graphScopeID = ""
	m.graphOpen = false
	m.focus = focusInput
	m.autoScroll = true
	m.newMessages = 0
	m.input.Reset()
	m.streamQueue = nil
	m.clearStream()
	m.chat.SetContent(m.renderMessages())
	m.chat.GotoBottom()
	m.showStatus("new session → " + sessionID)
	return m.poll()
}

func (m *Model) showStatus(status string) tea.Cmd {
	m.err = nil
	m.status = status
	m.statusUntil = time.Now().Add(statusTTL)
	m.palette = paletteNone
	m.paletteSelected = 0
	m.setSize(m.width, m.height)
	return nil
}

func (m *Model) models() []string {
	if m.commander == nil {
		return nil
	}
	choices := m.modelCatalog
	if len(choices) == 0 {
		choices = m.fallbackModelChoices()
	}
	models := make([]string, 0, len(choices))
	for _, choice := range choices {
		models = append(models, choice.Slug)
	}
	return models
}

func (m *Model) fallbackModelChoices() []ModelChoice {
	if m.commander == nil {
		return nil
	}
	models := m.commander.Models()
	choices := make([]ModelChoice, 0, len(models))
	for _, model := range models {
		choices = append(choices, ModelChoice{Slug: model})
	}
	return normalizeModelChoices(choices)
}

func (m *Model) filteredModelChoices() []ModelChoice {
	query := strings.TrimSpace(m.input.Value())
	type scoredChoice struct {
		choice ModelChoice
		score  int
	}
	scored := make([]scoredChoice, 0, len(m.modelCatalog))
	for _, choice := range m.modelCatalog {
		score, ok := modelChoiceScore(choice, query)
		if ok {
			scored = append(scored, scoredChoice{choice: choice, score: score})
		}
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].score < scored[j].score })
	choices := make([]ModelChoice, 0, len(scored))
	for _, candidate := range scored {
		choices = append(choices, candidate.choice)
	}
	return choices
}

func modelChoiceScore(choice ModelChoice, query string) (int, bool) {
	if strings.TrimSpace(query) == "" {
		return 0, true
	}
	best, matched := 0, false
	for _, value := range []string{choice.Slug, choice.Name, choice.Slug + " " + choice.Name} {
		if score, ok := fuzzyScore(value, query); ok && (!matched || score < best) {
			best, matched = score, true
		}
	}
	return best, matched
}

func normalizeModelChoices(choices []ModelChoice) []ModelChoice {
	seen := make(map[string]bool, len(choices))
	normalized := make([]ModelChoice, 0, len(choices))
	for _, choice := range choices {
		choice.Slug = strings.TrimSpace(choice.Slug)
		choice.Name = strings.TrimSpace(choice.Name)
		choice.Price = strings.TrimSpace(choice.Price)
		if choice.Slug == "" || seen[choice.Slug] {
			continue
		}
		seen[choice.Slug] = true
		normalized = append(normalized, choice)
	}
	return normalized
}

func (m *Model) cancellableNodeIDs() []string {
	ids := make([]string, 0, len(m.snapshot.Nodes))
	for _, node := range m.snapshot.Nodes {
		if node.ID == store.RootID || node.Status == store.Done || node.Status == store.Failed || node.Status == store.Cancelled {
			continue
		}
		ids = append(ids, node.ID)
	}
	return ids
}

func parseModelArgument(value string) (string, string) {
	_, arguments, _ := strings.Cut(value, " ")
	arguments = strings.TrimSpace(arguments)
	role := "talk"
	if arguments == "work" {
		return "work", ""
	}
	if strings.HasPrefix(arguments, "work ") {
		role = "work"
		arguments = strings.TrimSpace(strings.TrimPrefix(arguments, "work "))
	} else if arguments == "talk" {
		arguments = ""
	} else if strings.HasPrefix(arguments, "talk ") {
		arguments = strings.TrimSpace(strings.TrimPrefix(arguments, "talk "))
	}
	return role, arguments
}

func indexString(values []string, value string) int {
	for index, candidate := range values {
		if candidate == value {
			return index
		}
	}
	return 0
}

func indexModelChoice(choices []ModelChoice, slug string) int {
	for index, choice := range choices {
		if choice.Slug == slug {
			return index
		}
	}
	return 0
}
