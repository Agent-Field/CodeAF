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
)

const (
	pollInterval = 400 * time.Millisecond
	pollLimit    = 200
	stackAtWidth = 90
)

// Backend is the small part of the durable store the terminal lens needs.
// Keeping it local makes the Elm update loop straightforward to exercise with
// an in-memory fake.
type Backend interface {
	Messages(sessionID string, afterSeq int64, limit int) ([]store.Message, error)
	PostMessage(store.Message) (store.Message, error)
	ActiveSnapshot() (store.Snapshot, error)
}

var _ Backend = (*store.Store)(nil)

type tickMsg time.Time

type pollResultMsg struct {
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

	input textinput.Model
	chat  viewport.Model

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

	inputFocused bool
	autoScroll   bool
	newMessages  int
	spinnerFrame int
	err          error
}

// New returns a ready-to-run chat model. The default dimensions make View
// useful in tests before Bubble Tea sends its first WindowSizeMsg.
func New(backend Backend, sessionID string) *Model {
	input := textinput.New()
	input.Prompt = "› "
	input.Placeholder = "Ask the graph…"
	input.CharLimit = store.MaxMessageBytes
	input.PromptStyle = promptStyle
	input.TextStyle = inputTextStyle
	input.PlaceholderStyle = placeholderStyle
	input.Cursor.Style = cursorStyle
	_ = input.Focus()

	m := &Model{
		backend:      backend,
		sessionID:    sessionID,
		input:        input,
		chat:         viewport.New(1, 1),
		inputFocused: true,
		autoScroll:   true,
	}
	m.setSize(100, 30)
	return m
}

// Run starts a full-screen terminal session and restores the caller's screen
// when the user exits.
func Run(backend Backend, sessionID string) error {
	_, err := tea.NewProgram(New(backend, sessionID), tea.WithAltScreen()).Run()
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
		switch message.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "tab":
			return m, m.toggleFocus()
		case "enter":
			return m, m.submit()
		case "pgup":
			m.chat.PageUp()
			if m.chat.TotalLineCount() > m.chat.Height {
				m.autoScroll = false
			}
			return m, nil
		case "pgdown":
			m.chat.PageDown()
			if m.chat.AtBottom() {
				m.autoScroll = true
				m.newMessages = 0
			}
			return m, nil
		}
	}

	if m.inputFocused {
		var command tea.Cmd
		m.input, command = m.input.Update(message)
		return m, command
	}

	var command tea.Cmd
	m.chat, command = m.chat.Update(message)
	if m.chat.AtBottom() {
		m.autoScroll = true
		m.newMessages = 0
	} else if m.chat.TotalLineCount() > m.chat.Height {
		m.autoScroll = false
	}
	return m, command
}

func (m *Model) poll() tea.Cmd {
	backend := m.backend
	sessionID := m.sessionID
	afterSeq := m.lastSeq
	return func() tea.Msg {
		messages, messagesErr := backend.Messages(sessionID, afterSeq, pollLimit)
		snapshot, snapshotErr := backend.ActiveSnapshot()
		return pollResultMsg{
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
	if result.messagesErr == nil {
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
}

func (m *Model) submit() tea.Cmd {
	body := strings.TrimSpace(m.input.Value())
	if body == "" {
		return nil
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
	m.inputFocused = !m.inputFocused
	if m.inputFocused {
		return m.input.Focus()
	}
	m.input.Blur()
	return nil
}

func (m *Model) setSize(width, height int) {
	m.width = max(20, width)
	m.height = max(8, height)
	m.horizontal = m.width >= stackAtWidth

	mainHeight := max(4, m.height-6) // top bar, two gaps, three-line input
	if m.horizontal {
		const gap = 2
		m.chatWidth = max(20, (m.width-gap)*62/100)
		m.graphWidth = max(12, m.width-gap-m.chatWidth)
		m.chatHeight = mainHeight
		m.graphHeight = mainHeight
	} else {
		const gap = 1
		m.chatWidth = m.width
		m.graphWidth = m.width
		m.chatHeight = max(4, (mainHeight-gap)*62/100)
		m.graphHeight = max(3, mainHeight-gap-m.chatHeight)
	}

	m.chat.Width = max(1, m.chatWidth-4)   // border and horizontal padding
	m.chat.Height = max(1, m.chatHeight-4) // title, breathing room, border
	m.input.Width = max(1, m.width-6)
	m.refreshChat()
	if m.autoScroll {
		m.chat.GotoBottom()
	}
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
