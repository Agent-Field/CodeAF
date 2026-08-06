// Package tui renders the durable graph store as an interactive conversation.
// It is deliberately only a lens: messages are appended to the store and all
// graph changes arrive asynchronously from another process.
package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	pollInterval = 400 * time.Millisecond
	pollLimit    = 200
	railAtWidth  = 100
	statusTTL    = 3 * time.Second
)

// Backend is the small part of the durable store the terminal lens needs.
// Keeping it local makes the Elm update loop straightforward to exercise with
// an in-memory fake.
type Backend interface {
	Messages(sessionID string, afterSeq int64, limit int) ([]store.Message, error)
	PostMessage(store.Message) (store.Message, error)
	ActiveSnapshot() (store.Snapshot, error)
}

// Commander owns the live operations that do not belong to the thread lens.
// A nil Commander keeps ordinary chat fully functional and reports command
// capabilities honestly when they are requested.
type Commander interface {
	Models() []string
	CurrentModel(role string) string
	SetModel(role, slug string) error
	NewSession() (string, error)
	Cancel(nodeID string) error
}

var _ Backend = (*store.Store)(nil)

type tickMsg time.Time

type pollResultMsg struct {
	sessionID   string
	messages    []store.Message
	snapshot    store.Snapshot
	messagesErr error
	snapshotErr error
}

type postResultMsg struct {
	err error
}

// Model is the Bubble Tea model for an aforge chat session.
type Model struct {
	backend   Backend
	sessionID string
	commander Commander

	input textinput.Model
	chat  viewport.Model
	graph viewport.Model

	messages []store.Message
	snapshot store.Snapshot
	lastSeq  int64

	width  int
	height int

	horizontal  bool
	chatWidth   int
	chatHeight  int
	graphWidth  int
	graphHeight int

	inputFocused     bool
	focus            paneFocus
	autoScroll       bool
	newMessages      int
	spinnerFrame     int
	receiptsExpanded bool
	err              error

	palette          paletteKind
	paletteSelected  int
	paletteDismissed bool
	modelRole        string
	status           string
	statusUntil      time.Time
}

type paneFocus int

const (
	focusInput paneFocus = iota
	focusChat
	focusGraph
)

// New returns a ready-to-run chat model. The default dimensions make View
// useful in tests before Bubble Tea sends its first WindowSizeMsg.
func New(backend Backend, sessionID string) *Model {
	return newModel(backend, sessionID, nil)
}

// NewWithCommander returns a model with live command capabilities. It is
// exported so embedders can exercise or compose the surface without running a
// terminal program.
func NewWithCommander(backend Backend, sessionID string, commander Commander) *Model {
	return newModel(backend, sessionID, commander)
}

func newModel(backend Backend, sessionID string, commander Commander) *Model {
	input := textinput.New()
	input.Prompt = "› "
	input.Placeholder = "Ask the graph…"
	input.CharLimit = store.MaxMessageBytes
	input.PromptStyle = promptStyle
	input.TextStyle = inputTextStyle
	input.PlaceholderStyle = placeholderStyle
	input.Cursor.Style = cursorStyle
	input.MaxLines = 3
	_ = input.Focus()

	m := &Model{
		backend:      backend,
		sessionID:    sessionID,
		commander:    commander,
		input:        input,
		chat:         viewport.New(1, 1),
		graph:        viewport.New(1, 1),
		inputFocused: true,
		focus:        focusInput,
		autoScroll:   true,
		modelRole:    "talk",
	}
	m.setSize(100, 30)
	return m
}

// Run starts a full-screen terminal session and restores the caller's screen
// when the user exits.
func Run(backend Backend, sessionID string) error {
	return RunWithCommander(backend, sessionID, nil)
}

// RunWithCommander starts a full-screen terminal session with live slash
// commands enabled.
func RunWithCommander(backend Backend, sessionID string, commander Commander) error {
	_, err := tea.NewProgram(
		NewWithCommander(backend, sessionID, commander),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	).Run()
	return err
}

// Init starts cursor blinking and performs the first read immediately.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.poll())
}

// Update applies terminal events and store results. All store I/O is returned
// as a command, so keystrokes and rendering never wait on SQLite or a reply.
func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.setSize(message.Width, message.Height)
		return m, nil

	case tickMsg:
		m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
		m.refreshGraph()
		return m, m.poll()

	case pollResultMsg:
		m.applyPoll(message)
		return m, nextTick()

	case postResultMsg:
		if message.err != nil {
			m.err = fmt.Errorf("send message: %w", message.err)
			return m, nil
		}
		m.err = nil
		return m, nil

	case tea.KeyMsg:
		if command, handled := m.updateKey(message); handled {
			return m, command
		}

	case tea.MouseMsg:
		if m.updateMouse(message) {
			return m, nil
		}
	}

	if m.inputFocused {
		before := m.input.Value()
		var command tea.Cmd
		m.input, command = m.input.Update(message)
		if m.input.Value() != before {
			m.paletteSelected = 0
			m.paletteDismissed = false
			m.syncPalette()
			m.setSize(m.width, m.height)
		}
		return m, command
	}

	var command tea.Cmd
	if m.focus == focusGraph {
		m.graph, command = m.graph.Update(message)
		return m, command
	}
	m.chat, command = m.chat.Update(message)
	m.syncChatScroll()
	return m, command
}

func (m *Model) updateKey(message tea.KeyMsg) (tea.Cmd, bool) {
	key := message.String()
	if key == "ctrl+c" {
		return tea.Quit, true
	}
	if key == "esc" {
		switch {
		case m.palette == paletteModel || m.palette == paletteHelp:
			m.closePalette()
		case m.paletteOpen():
			m.palette = paletteNone
			m.paletteDismissed = true
			m.setSize(m.width, m.height)
		case m.input.Value() != "":
			m.input.Reset()
			m.paletteDismissed = false
			m.setSize(m.width, m.height)
		default:
			return tea.Quit, true
		}
		return nil, true
	}
	if key == "?" && m.input.Value() == "" {
		m.palette = paletteHelp
		m.paletteSelected = 0
		m.setSize(m.width, m.height)
		return nil, true
	}
	if m.paletteOpen() {
		if command, handled := m.updatePaletteKey(key); handled {
			return command, true
		}
	}
	if key == "v" && !m.inputFocused {
		m.receiptsExpanded = !m.receiptsExpanded
		m.refreshChat()
		return nil, true
	}
	if key == "tab" {
		return m.toggleFocus(), true
	}
	if key == "enter" && m.inputFocused {
		return m.submit(), true
	}
	if key == "pgup" || key == "pgdown" {
		m.pageFocused(key == "pgdown")
		return nil, true
	}
	if key == "end" && (!m.autoScroll || m.newMessages > 0) {
		m.pinChat()
		return nil, true
	}
	return nil, false
}

func (m *Model) poll() tea.Cmd {
	backend := m.backend
	sessionID := m.sessionID
	afterSeq := m.lastSeq
	return func() tea.Msg {
		messages, messagesErr := backend.Messages(sessionID, afterSeq, pollLimit)
		snapshot, snapshotErr := backend.ActiveSnapshot()
		return pollResultMsg{
			sessionID:   sessionID,
			messages:    messages,
			snapshot:    snapshot,
			messagesErr: messagesErr,
			snapshotErr: snapshotErr,
		}
	}
}

func nextTick() tea.Cmd {
	return tea.Tick(pollInterval, func(at time.Time) tea.Msg {
		return tickMsg(at)
	})
}

func (m *Model) applyPoll(result pollResultMsg) {
	if result.snapshotErr == nil {
		m.snapshot = result.snapshot
	}

	added := 0
	if result.messagesErr == nil && (result.sessionID == "" || result.sessionID == m.sessionID) {
		for _, message := range result.messages {
			// Polls can briefly overlap after a post. Journal sequence numbers
			// make accepting both results safe without a separate seen map.
			if message.Seq != 0 && message.Seq <= m.lastSeq {
				continue
			}
			m.messages = append(m.messages, message)
			if message.Seq > m.lastSeq {
				m.lastSeq = message.Seq
			}
			added++
		}
	}

	if added > 0 {
		m.refreshChat()
		if m.autoScroll {
			m.chat.GotoBottom()
			m.newMessages = 0
		} else {
			m.newMessages += added
		}
	}

	var problems []error
	if result.messagesErr != nil {
		problems = append(problems, fmt.Errorf("read thread: %w", result.messagesErr))
	}
	if result.snapshotErr != nil {
		problems = append(problems, fmt.Errorf("read graph: %w", result.snapshotErr))
	}
	m.err = errors.Join(problems...)
	m.refreshGraph()
}

func (m *Model) submit() tea.Cmd {
	body := strings.TrimSpace(m.input.Value())
	if body == "" {
		return nil
	}
	if strings.HasPrefix(body, "/") {
		return m.executeSlash(body)
	}
	m.input.Reset()
	m.err = nil

	backend := m.backend
	message := store.Message{
		SessionID: m.sessionID,
		Role:      store.RoleUser,
		Body:      body,
	}
	return func() tea.Msg {
		_, err := backend.PostMessage(message)
		return postResultMsg{err: err}
	}
}

func (m *Model) toggleFocus() tea.Cmd {
	m.focus = (m.focus + 1) % 3
	m.inputFocused = m.focus == focusInput
	if m.inputFocused {
		return m.input.Focus()
	}
	m.input.Blur()
	return nil
}

func (m *Model) setSize(width, height int) {
	m.width = max(20, width)
	m.height = max(8, height)
	m.horizontal = m.width >= railAtWidth

	paletteHeight := m.paletteHeight()
	footerHeight := 1
	if m.paletteOpen() {
		footerHeight = 0
	}
	stripHeight := 0
	if !m.horizontal {
		stripHeight = 1
	}
	mainHeight := max(3, m.height-3-(m.input.LineCount()+2)-paletteHeight-footerHeight-stripHeight)
	if m.horizontal {
		const gap = 2
		m.chatWidth = max(20, (m.width-gap)*80/100)
		m.graphWidth = max(12, m.width-gap-m.chatWidth)
	} else {
		m.chatWidth = m.width
		m.graphWidth = m.width
	}
	m.chatHeight = mainHeight
	m.graphHeight = mainHeight

	m.chat.Width = max(1, m.chatWidth-4)   // border and horizontal padding
	m.chat.Height = max(1, m.chatHeight-4) // title, breathing room, border
	m.graph.Width = max(1, m.graphWidth-4)
	m.graph.Height = max(1, m.graphHeight-4)
	m.input.Width = max(1, m.width-6)
	m.refreshChat()
	m.refreshGraph()
	if m.autoScroll {
		m.chat.GotoBottom()
	}
}

func (m *Model) refreshGraph() {
	offset := m.graph.YOffset
	m.graph.SetContent(m.renderTree(max(1, m.graph.Width), 0))
	m.graph.SetYOffset(offset)
}

func (m *Model) pageFocused(down bool) {
	if m.focus == focusGraph {
		if down {
			m.graph.PageDown()
		} else {
			m.graph.PageUp()
		}
		return
	}
	if down {
		m.chat.PageDown()
	} else {
		m.chat.PageUp()
	}
	m.syncChatScroll()
}

func (m *Model) syncChatScroll() {
	if m.chat.AtBottom() {
		m.pinChat()
	} else if m.chat.TotalLineCount() > m.chat.Height {
		m.autoScroll = false
	}
}

func (m *Model) pinChat() {
	m.chat.GotoBottom()
	m.autoScroll = true
	m.newMessages = 0
}

func (m *Model) updateMouse(message tea.MouseMsg) bool {
	event := tea.MouseEvent(message)
	if event.Button == tea.MouseButtonLeft && event.Action == tea.MouseActionPress && m.newMessagePillHit(event.X, event.Y) {
		m.pinChat()
		return true
	}
	if event.Button != tea.MouseButtonWheelUp && event.Button != tea.MouseButtonWheelDown {
		return false
	}
	down := event.Button == tea.MouseButtonWheelDown
	if m.mouseInGraph(event.X, event.Y) {
		if down {
			m.graph.SetYOffset(m.graph.YOffset + 3)
		} else {
			m.graph.SetYOffset(m.graph.YOffset - 3)
		}
		return true
	}
	if down {
		m.chat.SetYOffset(m.chat.YOffset + 3)
	} else {
		m.chat.SetYOffset(m.chat.YOffset - 3)
	}
	m.syncChatScroll()
	return true
}

func (m *Model) mouseInGraph(x, y int) bool {
	if m.horizontal {
		return y >= 2 && y < 2+m.graphHeight && x >= m.chatWidth+2
	}
	return m.focus == focusGraph && y >= 2 && y < 2+m.graphHeight
}

func (m *Model) newMessagePillHit(x, y int) bool {
	if m.newMessages == 0 {
		return false
	}
	if !m.horizontal && m.focus == focusGraph {
		return false
	}
	chatX, chatY := 0, 2
	pillWidth := lipgloss.Width(m.newMessageLabel()) + 2
	return x >= chatX+m.chatWidth-pillWidth-1 && x < chatX+m.chatWidth-1 &&
		y >= chatY+m.chatHeight-2 && y < chatY+m.chatHeight-1
}

func (m *Model) refreshChat() {
	offset := m.chat.YOffset
	m.chat.SetContent(m.renderMessages())
	if m.autoScroll {
		m.chat.GotoBottom()
		return
	}
	m.chat.SetYOffset(offset)
}
