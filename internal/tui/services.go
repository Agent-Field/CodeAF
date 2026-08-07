package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const (
	serviceGraphRowPrefix = "\x00service:"
	serviceLogTailLines   = 10
	serviceLogMaxBytes    = 32 << 10
)

type serviceLister interface {
	ActiveServices() ([]store.Service, error)
}

var _ serviceLister = (*store.Store)(nil)

type serviceRow struct {
	line      int
	serviceID string
}

type serviceCardRow struct {
	line   int
	action string
}

type serviceCommandResultMsg struct {
	action string
	name   string
	err    error
}

func serviceGraphRowID(id string) string { return serviceGraphRowPrefix + id }

func serviceIDFromGraphRow(id string) (string, bool) {
	if !strings.HasPrefix(id, serviceGraphRowPrefix) {
		return "", false
	}
	return strings.TrimPrefix(id, serviceGraphRowPrefix), true
}

// activeServices answers from the poll-scoped cache. Four layout and render
// sites ask per frame; the store is read once per poll behind them.
func (m *Model) activeServices() []store.Service {
	if m.railServicesValid {
		return m.railServices
	}
	m.railServicesValid = true
	m.railServices = nil
	lister, ok := m.backend.(serviceLister)
	if !ok {
		return nil
	}
	services, err := lister.ActiveServices()
	if err != nil {
		return nil
	}
	m.railServices = services
	return services
}

func (m *Model) activeService(id string) (store.Service, bool) {
	for _, service := range m.activeServices() {
		if service.ID == id {
			return service, true
		}
	}
	return store.Service{}, false
}

func (m *Model) servicesSectionHeight() int {
	if !m.graphVisible() || m.graphScopeID != "" || m.charterCardID != "" || m.serviceCardID != "" {
		return 0
	}
	count := len(m.activeServices())
	if count == 0 {
		return 0
	}
	return count + 2 // header + rows + separating blank
}

func (m *Model) renderServicesSection(width int) string {
	m.serviceRows = m.serviceRows[:0]
	services := m.activeServices()
	if len(services) == 0 {
		return ""
	}
	lines := []string{mutedStyle.Faint(true).Render("services")}
	for _, service := range services {
		row := len(lines)
		selected := m.focus == focusGraph && m.selectedNodeID == serviceGraphRowID(service.ID)
		marker := mutedStyle.Faint(true).Render("▸ ")
		if selected {
			marker = powderStyle.Bold(true).Render("▸ ")
		}
		state := "up " + serviceAge(service.StartedAt, m.standingTime())
		if service.Status != store.ServiceRunning {
			state = string(service.Status)
		}
		body := mutedStyle.Faint(true).Render(fmt.Sprintf("%s · %s · %s", service.Name, state, service.Health.Suffix()))
		line := truncate(marker+body, width)
		if selected {
			line = bandStyle.Width(width).Render(line)
		}
		lines = append(lines, line)
		m.serviceRows = append(m.serviceRows, serviceRow{line: row, serviceID: service.ID})
	}
	lines = append(lines, "")
	return strings.Join(lines, "\n")
}

func serviceAge(started, now time.Time) string {
	if started.IsZero() || !now.After(started) {
		return "0s"
	}
	age := now.Sub(started)
	switch {
	case age < time.Minute:
		return fmt.Sprintf("%ds", int(age/time.Second))
	case age < time.Hour:
		return fmt.Sprintf("%dm", int(age/time.Minute))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh", int(age/time.Hour))
	default:
		return fmt.Sprintf("%dd", int(age/(24*time.Hour)))
	}
}

func (m *Model) renderServiceCardBody(width int) string {
	m.serviceCardRows = m.serviceCardRows[:0]
	service, ok := m.activeService(m.serviceCardID)
	if !ok {
		return mutedStyle.Render("service is no longer available")
	}
	lines := []string{mutedStyle.Faint(true).Render("╭─ ") + mutedStyle.Render(string(service.Status))}
	lines = append(lines, mutedStyle.Faint(true).Render("│   log · "+service.LogPath))
	logTail := readServiceLogTail(service.LogPath)
	if len(logTail) == 0 {
		logTail = []string{"(no output)"}
	}
	for _, line := range logTail {
		lines = append(lines, mutedStyle.Faint(true).Render("│ ")+mutedStyle.Render(truncate(line, max(1, width-2))))
	}
	lines = append(lines, mutedStyle.Faint(true).Render("│   actions"))
	actions := []string{"stop", "restart"}
	if !service.AutoRestart {
		actions = append(actions, "enable auto-restart")
	}
	for _, action := range actions {
		row := serviceCardRow{line: len(lines), action: action}
		selected := m.focus == focusGraph && len(m.serviceCardRows) == m.serviceFocusIndex
		line := mutedStyle.Faint(true).Render("│   ▸ ") + mutedStyle.Render(action)
		if selected {
			line = bandStyle.Width(width).Render(line)
		}
		lines = append(lines, truncate(line, width))
		m.serviceCardRows = append(m.serviceCardRows, row)
	}
	lines = append(lines, mutedStyle.Faint(true).Render("╰─"))
	return strings.Join(lines, "\n")
}

func readServiceLogTail(path string) []string {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() == 0 {
		return nil
	}
	count := int64(serviceLogMaxBytes)
	if info.Size() < count {
		count = info.Size()
	}
	buffer := make([]byte, count)
	_, _ = file.ReadAt(buffer, info.Size()-count)
	raw := strings.Split(ansi.Strip(string(buffer)), "\n")
	if len(raw) > 0 && raw[len(raw)-1] == "" {
		raw = raw[:len(raw)-1]
	}
	if len(raw) > serviceLogTailLines {
		raw = raw[len(raw)-serviceLogTailLines:]
	}
	return raw
}

func (m *Model) openServiceCard(id string) {
	if _, ok := m.activeService(id); !ok {
		return
	}
	m.serviceCardID = id
	m.serviceFocusIndex = 0
	m.selectedNodeID = serviceGraphRowID(id)
	m.graph.SetYOffset(0)
	m.setSize(m.width, m.height)
}

func (m *Model) closeServiceCard() bool {
	if m.serviceCardID == "" {
		return false
	}
	id := m.serviceCardID
	m.serviceCardID = ""
	m.serviceFocusIndex = 0
	m.selectedNodeID = serviceGraphRowID(id)
	m.graph.SetYOffset(0)
	m.setSize(m.width, m.height)
	return true
}

func (m *Model) moveServiceSelection(delta int) {
	if len(m.serviceCardRows) == 0 {
		m.refreshGraph()
	}
	if len(m.serviceCardRows) == 0 {
		return
	}
	m.serviceFocusIndex = max(0, min(len(m.serviceCardRows)-1, m.serviceFocusIndex+delta))
	m.refreshGraph()
}

func (m *Model) activateServiceSelection() tea.Cmd {
	if len(m.serviceCardRows) == 0 {
		m.refreshGraph()
	}
	if len(m.serviceCardRows) == 0 {
		return nil
	}
	return m.requestServiceAction(m.serviceCardRows[m.serviceFocusIndex].action)
}

func (m *Model) activateServiceLine(line int) (tea.Cmd, bool) {
	for index, row := range m.serviceCardRows {
		if row.line != line {
			continue
		}
		m.serviceFocusIndex = index
		m.refreshGraph()
		return m.activateServiceSelection(), true
	}
	return nil, false
}

func (m *Model) requestServiceAction(action string) tea.Cmd {
	service, ok := m.activeService(m.serviceCardID)
	if !ok {
		return m.showStatus("service is no longer available")
	}
	requester, ok := m.backend.(commandRequester)
	if !ok {
		return m.showStatus("service actions unavailable — command requests unsupported")
	}
	kind := store.CommandServiceStop
	if action == "restart" {
		kind = store.CommandServiceRestart
	} else if action == "enable auto-restart" {
		kind = store.CommandServiceAutoRestart
	}
	request := store.Command{SessionID: m.sessionID, Kind: kind, Target: service.ID, Instruction: action}
	return func() tea.Msg {
		_, err := requester.RequestCommand(request)
		return serviceCommandResultMsg{action: action, name: service.Name, err: err}
	}
}
