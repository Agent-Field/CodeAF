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
	paletteHelp
)

type commandSpec struct {
	name        string
	description string
	takesArg    bool
}

var slashCommands = []commandSpec{
	{name: "model", description: "choose the talk or work model", takesArg: true},
	{name: "session", description: "show the current session and database"},
	{name: "new", description: "start a fresh chat session"},
	{name: "cancel", description: "cancel a non-terminal graph node", takesArg: true},
	{name: "help", description: "show commands and keyboard shortcuts"},
	{name: "quit", description: "exit aforge cleanly"},
}

type paletteEntry struct {
	value       string
	description string
	score       int
}

func (m *Model) paletteOpen() bool { return m.palette != paletteNone }

func (m *Model) closePalette() {
	m.palette = paletteNone
	m.paletteSelected = 0
	m.setSize(m.width, m.height)
}

func (m *Model) syncPalette() {
	if m.palette == paletteModel || m.palette == paletteHelp {
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
		switch key {
		case "tab", "left", "right":
			m.switchModelRole()
			return nil, true
		case "up":
			m.cyclePalette(-1, len(m.models()))
			return nil, true
		case "down":
			m.cyclePalette(1, len(m.models()))
			return nil, true
		case "enter":
			return m.applySelectedModel(m.models()), true
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
	return m.executeSlash("/" + selected)
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

func (m *Model) applySelectedModel(models []string) tea.Cmd {
	if len(models) == 0 {
		return m.showStatus("model switching unavailable — no models configured")
	}
	return m.applyModel(m.modelRole, models[min(m.paletteSelected, len(models)-1)])
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
	m.setSize(m.width, m.height)
	backend := m.backend
	message := store.Message{SessionID: m.sessionID, Role: store.RoleUser, Body: "cancel " + nodeID}
	return func() tea.Msg {
		_, err := backend.PostMessage(message)
		return postResultMsg{err: err}
	}
}

func (m *Model) executeSlash(body string) tea.Cmd {
	fields := strings.Fields(body)
	if len(fields) == 0 {
		return nil
	}
	switch fields[0] {
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
		return m.showStatus("unknown command · /help lists available commands")
	}
}

func (m *Model) openModelPicker(role string) tea.Cmd {
	if m.commander == nil {
		return m.showStatus("model switching unavailable — no Commander")
	}
	models := m.models()
	if len(models) == 0 {
		return m.showStatus("model switching unavailable — no models configured")
	}
	m.input.Reset()
	m.modelRole = role
	m.palette = paletteModel
	m.paletteSelected = indexString(models, m.commander.CurrentModel(role))
	m.setSize(m.width, m.height)
	return nil
}

func (m *Model) switchModelRole() {
	if m.modelRole == "talk" {
		m.modelRole = "work"
	} else {
		m.modelRole = "talk"
	}
	models := m.models()
	if m.commander != nil {
		m.paletteSelected = indexString(models, m.commander.CurrentModel(m.modelRole))
	}
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
	m.autoScroll = true
	m.newMessages = 0
	m.input.Reset()
	m.chat.SetContent(m.renderMessages())
	m.chat.GotoBottom()
	m.showStatus("new session → " + sessionID)
	return m.poll()
}

func (m *Model) showStatus(status string) tea.Cmd {
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
	seen := make(map[string]bool)
	models := make([]string, 0)
	for _, model := range m.commander.Models() {
		model = strings.TrimSpace(model)
		if model != "" && !seen[model] {
			seen[model] = true
			models = append(models, model)
		}
	}
	return models
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
