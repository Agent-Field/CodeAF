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
	paletteModels
	paletteModel
	paletteMemory
	paletteHelp
)

var modelSlots = []string{"talk", "work", "voice", "image", "speech", "music", "video", "boost"}

const followWorkModel = "follow work"

type commandSpec struct {
	name        string
	description string
	takesArg    bool
}

// keyBindings is the single source for routed chords and every help label
// that names them. Bubble Tea reports option-G as the literal "alt+g".
var keyBindings = struct {
	graph string
	voice string
	boost string
}{
	graph: "alt+g",
	voice: "alt+v",
	boost: "alt+b",
}

var slashCommands = []commandSpec{
	{name: "graph", description: "toggle the task rail"},
	{name: "tasks", description: "focus and expand the active-task dock"},
	{name: "node", description: "open a node by id prefix or current selection", takesArg: true},
	{name: "notebook", description: "browse the scoped notebook"},
	{name: "budget", description: "show or change today's dollar rail"},
	{name: "standing", description: "list active standing charters"},
	{name: "help", description: "show commands and keyboard shortcuts"},
	{name: "model", description: "choose any model slot", takesArg: true},
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

type modelCommandResultMsg struct {
	role     string
	slug     string
	previous string
	err      error
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
	if m.palette == paletteModels || m.palette == paletteModel || m.palette == paletteMemory || m.palette == paletteHelp {
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
	case paletteModels:
		switch {
		case key == "tab" || key == "left" || key == "right":
			return nil, true
		case key == "up" || key == "k":
			m.modelSlotIndex = (m.modelSlotIndex + len(modelSlots) - 1) % len(modelSlots)
			return nil, true
		case key == "down" || key == "j":
			m.modelSlotIndex = (m.modelSlotIndex + 1) % len(modelSlots)
			return nil, true
		case key == "enter":
			return m.openModelPicker(modelSlots[m.modelSlotIndex]), true
		case len(key) == 1 && key[0] >= '1' && key[0] <= '8':
			m.modelSlotIndex = int(key[0] - '1')
			return m.openModelPicker(modelSlots[m.modelSlotIndex]), true
		}
	case paletteModel:
		choices := m.filteredModelChoices()
		switch key {
		case "tab", "left", "right":
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
				if len(strings.Fields(m.input.Value())) == 1 {
					return m.openModelsPalette(), true
				}
				if role == "boost" {
					m.input.Reset()
					m.toggleBoost()
					return nil, true
				}
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
	role, partial := parseModelArgument(m.input.Value())
	return filterEntries(m.modelsForRole(role), partial)
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
	if role != "talk" {
		value += role + " "
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
	return m.modelPickerSeam().selectChoice(choices[min(m.paletteSelected, len(choices)-1)])
}

func (m *Model) applyModel(role, slug string) tea.Cmd {
	if !isModelSlot(role) {
		return m.showStatus(fmt.Sprintf("unknown model role %q", role))
	}
	previous := m.currentModel(role)
	display := slug
	if role == "boost" && slug == followWorkModel {
		slug = ""
		display = followWorkModel
	}
	if m.commander != nil {
		if err := m.commander.SetModel(role, slug); err != nil {
			return m.showStatus(fmt.Sprintf("could not set %s model: %v", role, err))
		}
		delete(m.optimisticModels, role)
		m.input.Reset()
		m.paletteDismissed = false
		return m.showStatus(fmt.Sprintf("%s model → %s", role, display))
	}
	if role != "talk" && role != "work" && role != "voice" {
		return m.showStatus(fmt.Sprintf("%s model switching unavailable — no Commander", role))
	}
	requester, ok := m.backend.(commandRequester)
	if !ok {
		return m.showStatus("model switching unavailable — command requests unsupported")
	}
	m.optimisticModels[role] = slug
	m.input.Reset()
	m.paletteDismissed = false
	m.showStatus(fmt.Sprintf("%s model requested → %s", role, slug))
	request := store.Command{
		SessionID: m.sessionID, Kind: store.CommandAmend, Target: store.RootID,
		Instruction: "use " + slug + " for " + role,
	}
	return func() tea.Msg {
		_, err := requester.RequestCommand(request)
		return modelCommandResultMsg{role: role, slug: slug, previous: previous, err: err}
	}
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
		return m.showStatus("/graph · /tasks · /budget · /standing · /notebook · /help")
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
	case "/budget":
		m.budgetUsed = true
		handler, ok := m.commander.(interface {
			Budget(arguments []string) (string, error)
		})
		if !ok {
			m.input.Reset()
			return m.showStatus("budget unavailable — no Commander")
		}
		result, err := handler.Budget(fields[1:])
		m.input.Reset()
		if err != nil {
			return m.showStatus("could not change budget: " + err.Error())
		}
		return m.showStatus(result)
	case "/standing":
		handler, ok := m.commander.(interface {
			Standing() (string, error)
		})
		if !ok {
			m.input.Reset()
			return m.showStatus("standing unavailable — no Commander")
		}
		result, err := handler.Standing()
		m.input.Reset()
		if err != nil {
			return m.showStatus("could not list standing charters: " + err.Error())
		}
		return m.showStatus(result)
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
		if len(arguments) == 0 {
			return m.openModelsPalette()
		}
		if len(arguments) == 1 && arguments[0] == "boost" {
			m.input.Reset()
			m.toggleBoost()
			return nil
		}
		if len(arguments) > 0 && isModelSlot(arguments[0]) {
			role = arguments[0]
			arguments = arguments[1:]
		}
		if len(arguments) == 0 {
			return m.openModelPicker(role)
		}
		entries := filterEntries(m.modelsForRole(role), strings.Join(arguments, " "))
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
	if !isModelSlot(role) {
		role = "talk"
	}
	fallback := m.fallbackModelChoices(role)
	if len(fallback) == 0 {
		fallback = normalizeModelChoices(m.modelCatalogForRole(role))
	}
	if len(fallback) == 0 {
		return m.showStatus("model switching unavailable — no models configured")
	}
	m.input.Reset()
	m.modelRole = role
	m.palette = paletteModel
	m.focus = focusInput
	m.inputFocused = true
	_ = m.input.Focus()
	if len(m.modelCatalogForRole(role)) == 0 {
		m.setModelCatalogForRole(role, fallback)
	}
	if role == "boost" && m.modelFollowsWork() {
		m.paletteSelected = 0
	} else {
		m.paletteSelected = indexModelChoice(m.modelPickerSeam().choices, m.currentModel(role))
	}
	if !m.modelCatalogWasRequested(role) && m.commander != nil {
		m.setModelCatalogLoading(role, true)
	}
	m.setSize(m.width, m.height)
	if m.modelCatalogWasRequested(role) || m.commander == nil {
		return nil
	}
	m.setModelCatalogRequested(role)
	commander := m.commander
	return func() tea.Msg {
		choices := commander.Catalog()
		if cataloger, ok := commander.(interface{ CatalogFor(string) []ModelChoice }); ok {
			choices = cataloger.CatalogFor(role)
		}
		return catalogResultMsg{role: role, choices: choices}
	}
}

func (m *Model) openModelsPalette() tea.Cmd {
	m.input.Reset()
	m.palette = paletteModels
	m.modelSlotIndex = max(0, min(len(modelSlots)-1, m.modelSlotIndex))
	m.focus = focusHeader
	m.headerFocusIndex = 0
	m.inputFocused = false
	m.input.Blur()
	m.setSize(m.width, m.height)
	return nil
}

func (m *Model) returnToModelsPalette() {
	m.input.Reset()
	m.palette = paletteModels
	m.modelSlotIndex = indexModelSlot(m.modelRole)
	m.paletteSelected = 0
	m.focus = focusHeader
	m.headerFocusIndex = 0
	m.inputFocused = false
	m.input.Blur()
	m.setSize(m.width, m.height)
}

func (m *Model) applyCatalog(role string, choices []ModelChoice) {
	if role == "" {
		role = m.modelRole
	}
	m.setModelCatalogLoading(role, false)
	if normalized := normalizeModelChoices(choices); len(normalized) > 0 {
		// A remote catalog enriches the configured/current choices; it must not
		// erase a pinned model merely because the provider omitted an alias.
		m.setModelCatalogForRole(role, normalizeModelChoices(append(normalized, m.fallbackModelChoices(role)...)))
	}
	if m.palette != paletteModel || role != m.modelRole {
		return
	}
	filtered := m.filteredModelChoices()
	if strings.TrimSpace(m.input.Value()) == "" {
		if m.modelRole == "boost" && m.modelFollowsWork() {
			m.paletteSelected = 0
		} else {
			m.paletteSelected = indexModelChoice(filtered, m.currentModel(m.modelRole))
		}
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
	m.questionSelection = map[string]int{}
	m.questionDismissed = map[string]bool{}
	m.agentQuestions = nil
	m.answeringQuestionSeq = 0
	m.questionDockExpanded = false
	m.questionDockSelection = 0
	m.questionDockRows = nil
	m.selectedCardID = ""
	m.graphScopeID = ""
	m.charterCardID = ""
	m.charterFocusIndex = 0
	m.standingRows = nil
	m.charterRows = nil
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

func (m *Model) modelsForRole(role string) []string {
	choices := m.modelCatalogForRole(role)
	if len(choices) == 0 {
		choices = m.fallbackModelChoices(role)
	}
	models := make([]string, 0, len(choices)+1)
	if role == "boost" {
		models = append(models, followWorkModel)
	}
	for _, choice := range choices {
		models = append(models, choice.Slug)
	}
	return models
}

func (m *Model) fallbackModelChoices(role string) []ModelChoice {
	if m.commander == nil {
		return normalizeModelChoices(m.modelCatalogForRole(role))
	}
	if role != "talk" && role != "work" && role != "boost" {
		current := strings.TrimSpace(m.commander.CurrentModel(role))
		if current == "" {
			return nil
		}
		return []ModelChoice{{Slug: current}}
	}
	models := m.commander.Models()
	choices := make([]ModelChoice, 0, len(models))
	for _, model := range models {
		choices = append(choices, ModelChoice{Slug: model})
	}
	return normalizeModelChoices(choices)
}

func (m *Model) activateHeaderFocus() tea.Cmd {
	switch m.headerFocusIndex {
	case 0:
		return m.openModelsPalette()
	default:
		if m.hasPendingQuestion() {
			m.focusPendingQuestion()
			return nil
		}
		if m.nodeViewID != "" {
			m.closeNodeView()
		}
		m.toggleGraph()
		return nil
	}
}

func (m *Model) filteredModelChoices() []ModelChoice {
	return m.modelPickerSeam().filtered(m.input.Value())
}

// searchableModelList is the dropdown's intentionally narrow merge seam: a
// caller supplies a list, a filter callback, and a selection callback. Voice
// and conversational models share it today; other header pickers can do so
// without taking a dependency on model state.
type searchableModelList struct {
	choices      []ModelChoice
	filter       func(ModelChoice, string) (int, bool)
	selectChoice func(ModelChoice) tea.Cmd
}

func (m *Model) modelPickerSeam() searchableModelList {
	role := m.modelRole
	choices := m.modelCatalogForRole(role)
	if role == "boost" {
		choices = append([]ModelChoice{{Slug: followWorkModel, Name: "use work model"}}, choices...)
	}
	return searchableModelList{
		choices:      choices,
		filter:       modelChoiceScore,
		selectChoice: func(choice ModelChoice) tea.Cmd { return m.applyModel(role, choice.Slug) },
	}
}

func (list searchableModelList) filtered(query string) []ModelChoice {
	query = strings.TrimSpace(query)
	type scoredChoice struct {
		choice ModelChoice
		score  int
	}
	scored := make([]scoredChoice, 0, len(list.choices))
	for _, choice := range list.choices {
		score, ok := list.filter(choice, query)
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

func (m *Model) modelCatalogForRole(role string) []ModelChoice {
	if role == "voice" {
		return m.voiceModelCatalog
	}
	if role != "talk" && role != "work" && role != "boost" {
		return m.mediaModelCatalogs[role]
	}
	return m.modelCatalog
}

func (m *Model) setModelCatalogForRole(role string, choices []ModelChoice) {
	if role == "voice" {
		m.voiceModelCatalog = choices
		return
	}
	if role != "talk" && role != "work" && role != "boost" {
		m.mediaModelCatalogs[role] = choices
		return
	}
	m.modelCatalog = choices
}

func (m *Model) modelCatalogWasRequested(role string) bool {
	if role == "voice" {
		return m.voiceCatalogRequested
	}
	if role != "talk" && role != "work" && role != "boost" {
		return m.mediaCatalogRequested[role]
	}
	return m.catalogRequested
}

func (m *Model) setModelCatalogRequested(role string) {
	if role == "voice" {
		m.voiceCatalogRequested = true
		return
	}
	if role != "talk" && role != "work" && role != "boost" {
		m.mediaCatalogRequested[role] = true
		return
	}
	m.catalogRequested = true
}

func (m *Model) modelCatalogIsLoading(role string) bool {
	if role == "voice" {
		return m.voiceCatalogLoading
	}
	if role != "talk" && role != "work" && role != "boost" {
		return m.mediaCatalogLoading[role]
	}
	return m.catalogLoading
}

func (m *Model) setModelCatalogLoading(role string, loading bool) {
	if role == "voice" {
		m.voiceCatalogLoading = loading
		return
	}
	if role != "talk" && role != "work" && role != "boost" {
		m.mediaCatalogLoading[role] = loading
		return
	}
	m.catalogLoading = loading
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
	for _, candidate := range modelSlots {
		if arguments == candidate {
			return candidate, ""
		}
		if strings.HasPrefix(arguments, candidate+" ") {
			role = candidate
			arguments = strings.TrimSpace(strings.TrimPrefix(arguments, candidate+" "))
			break
		}
	}
	return role, arguments
}

func isModelSlot(role string) bool { return indexModelSlot(role) >= 0 }

func indexModelSlot(role string) int {
	for index, candidate := range modelSlots {
		if candidate == role {
			return index
		}
	}
	return -1
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
