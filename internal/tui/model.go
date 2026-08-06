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
	pollInterval      = 400 * time.Millisecond
	animationInterval = 120 * time.Millisecond
	pollLimit         = 200
	railAtWidth       = 100
	statusTTL         = 3 * time.Second
	nodeTraceMaxBytes = 64 << 10
)

// ModelChoice is one model advertised by the live provider catalog. Price is
// display-ready because providers own the units and parsing rules.
type ModelChoice struct {
	Slug  string `json:"slug"`
	Name  string `json:"name,omitempty"`
	Price string `json:"price,omitempty"`
}

// Backend is the small part of the durable store the terminal lens needs.
// Keeping it local makes the Elm update loop straightforward to exercise with
// an in-memory fake.
type Backend interface {
	Messages(sessionID string, afterSeq int64, limit int) ([]store.Message, error)
	PostMessage(store.Message) (store.Message, error)
	ActiveSnapshot() (store.Snapshot, error)
	Snapshot() (store.Snapshot, error)
	Node(id string) (store.Node, bool, error)
	NodeMessages(nodeID string, afterSeq int64, limit int) ([]store.Message, error)
	PendingCommands(limit int) ([]store.Command, error)
	CommandBySeq(seq int64) (store.Command, bool, error)
	Usage() (store.TotalUsage, error)
	TopLevelJobUsage() (map[string]store.JobUsage, error)
}

// Commander owns the live operations that do not belong to the thread lens.
// A nil Commander keeps ordinary chat fully functional and reports command
// capabilities honestly when they are requested.
type Commander interface {
	Models() []string
	Catalog() []ModelChoice
	CurrentModel(role string) string
	SetModel(role, slug string) error
	Notebook(limit int) []store.Fact
	NewSession() (string, error)
	Cancel(nodeID string) error
	NodeTrace(nodeID string, maxBytes int) string
}

var _ Backend = (*store.Store)(nil)

type pollTickMsg time.Time

type animationTickMsg time.Time

type catalogResultMsg struct {
	choices []ModelChoice
}

type pollResultMsg struct {
	sessionID       string
	messages        []store.Message
	snapshot        store.Snapshot
	cardSnapshot    store.Snapshot
	pending         []store.Command
	usage           store.TotalUsage
	jobUsage        map[string]store.JobUsage
	commands        []store.Command
	messagesErr     error
	snapshotErr     error
	cardSnapshotErr error
	pendingErr      error
	usageErr        error
	jobUsageErr     error
	commandsErr     error

	nodeID          string
	node            store.Node
	nodeFound       bool
	nodeMessages    []store.Message
	nodeTrace       string
	nodeErr         error
	nodeMessagesErr error
}

type postResultMsg struct {
	message store.Message
	nodeID  string
	err     error
}

// Model is the Bubble Tea model for an aforge chat session.
type Model struct {
	backend   Backend
	sessionID string
	commander Commander

	input textinput.Model
	chat  viewport.Model
	graph viewport.Model

	nodeTrace viewport.Model

	messages     []store.Message
	snapshot     store.Snapshot
	cardSnapshot store.Snapshot
	pending      []store.Command
	usage        store.TotalUsage
	jobUsage     map[string]store.JobUsage
	commands     map[int64]store.Command
	lastSeq      int64

	cards           []jobCard
	cardExpanded    map[string]bool
	selectedCardID  string
	graphScopeID    string
	cardReturnFocus paneFocus

	selectedNodeID string
	graphRows      []graphRow
	nodeViewID     string
	inspectedNode  store.Node
	nodeMessages   []store.Message
	nodeLastSeq    int64
	nodeTraceText  string
	chatDraft      string
	returnFocus    paneFocus

	feedRows     []feedRow
	feedBlocks   []feedBlock
	feedExpanded map[int]bool

	// expandedMessages holds the seqs of long chat deliverables opened in
	// place; everything else shows its lead and a ⋯.
	expandedMessages map[int64]bool

	// revealSeq/revealShown drive the arrival animation: the newest agent
	// message unrolls a few lines per tick instead of appearing whole.
	revealSeq   int64
	revealShown int

	width  int
	height int

	horizontal  bool
	chatWidth   int
	chatHeight  int
	graphWidth  int
	graphHeight int

	nodeDetailsText string
	nodeTraceHeight int

	inputFocused     bool
	focus            paneFocus
	graphOpen        bool
	autoScroll       bool
	newMessages      int
	spinnerFrame     int
	animationPending bool
	graphAnimating   bool
	receiptsExpanded bool
	err              error

	palette          paletteKind
	paletteSelected  int
	paletteDismissed bool
	modelRole        string
	modelCatalog     []ModelChoice
	catalogRequested bool
	catalogLoading   bool
	memoryFacts      []store.Fact
	status           string
	statusUntil      time.Time

	// splitPct is the chat pane's share of the width in percent; zero means
	// the default. draggingSplit is true while the divider is held.
	splitPct      int
	draggingSplit bool

	chatBounds        paneBounds
	graphBounds       paneBounds
	graphRowsBounds   paneBounds
	inputBounds       paneBounds
	nodeBounds        paneBounds
	nodeTraceBounds   paneBounds
	nodeBackBounds    paneBounds
	activityBarBounds paneBounds
	cardDockRows      []cardRow
	chatCardRows      []cardRow
	cardPartRows      []cardPartRow

	// chatMessageRows maps rendered chat lines to the message seq they
	// belong to, so clicking a collapsed deliverable opens it in place.
	chatMessageRows []chatMessageRow

	// chatChipRows maps rendered provenance-chip lines to the task they
	// point at, so clicking `↳ title` opens that task's activity view.
	chatChipRows []chatChipRow
}

type paneFocus int

const (
	focusInput paneFocus = iota
	focusChat
	focusCards
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
		backend:          backend,
		sessionID:        sessionID,
		commander:        commander,
		input:            input,
		chat:             viewport.New(1, 1),
		graph:            viewport.New(1, 1),
		nodeTrace:        viewport.New(1, 1),
		inputFocused:     true,
		focus:            focusInput,
		autoScroll:       true,
		modelRole:        "talk",
		feedExpanded:     map[int]bool{},
		expandedMessages: map[int64]bool{},
		jobUsage:         map[string]store.JobUsage{},
		commands:         map[int64]store.Command{},
		cardExpanded:     map[string]bool{},
	}
	if saved, ok := commander.(splitStore); ok {
		m.splitPct = clampSplitPct(saved.SplitPct())
	}
	m.setSize(100, 30)
	return m
}

// splitStore is the optional Commander capability of remembering the divider
// position across launches. A Commander without it still resizes live; the
// position just resets next launch.
type splitStore interface {
	SplitPct() int
	SaveSplitPct(pct int)
}

// The divider clamps so neither pane can be dragged into uselessness.
const (
	defaultSplitPct = 80
	minSplitPct     = 25
	maxSplitPct     = 85
)

func clampSplitPct(pct int) int {
	if pct == 0 {
		return 0 // zero stays "unset" and resolves to the default at layout time
	}
	return max(minSplitPct, min(maxSplitPct, pct))
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
		return m, m.scheduleAnimation()

	case pollTickMsg:
		return m, m.poll()

	case pollResultMsg:
		m.applyPoll(message)
		return m, tea.Batch(nextPollTick(), m.scheduleAnimation())

	case animationTickMsg:
		m.animationPending = false
		if m.revealSeq != 0 {
			// The reveal: a landed message unrolls a few lines per tick, so
			// an answer reads as arriving rather than materialising.
			m.revealShown += 3
			m.refreshChat()
			if m.autoScroll {
				m.chat.GotoBottom()
			}
		}
		if !m.graphAnimating && m.revealSeq == 0 {
			return m, nil
		}
		m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
		m.refreshGraph()
		return m, m.scheduleAnimation()

	case catalogResultMsg:
		m.applyCatalog(message.choices)
		return m, nil

	case postResultMsg:
		if message.err != nil {
			m.err = fmt.Errorf("send message: %w", message.err)
			return m, nil
		}
		if message.nodeID != "" {
			m.landOptimisticNodeMessage(message.nodeID, message.message)
		}
		m.err = nil
		return m, nil

	case tea.KeyMsg:
		if command, handled := m.updateKey(message); handled {
			if command == nil {
				return m, m.scheduleAnimation()
			}
			return m, command
		}

	case tea.MouseMsg:
		if m.updateMouse(message) {
			return m, m.scheduleAnimation()
		}
	}

	if m.inputFocused {
		before := m.input.Value()
		var command tea.Cmd
		m.input, command = m.input.Update(message)
		if m.input.Value() != before && m.nodeViewID == "" {
			m.paletteSelected = 0
			m.paletteDismissed = false
			m.syncPalette()
			m.setSize(m.width, m.height)
		}
		return m, command
	}

	var command tea.Cmd
	if m.nodeViewID != "" {
		m.updateNodeViewport(message)
		return m, nil
	}
	if m.focus == focusGraph {
		m.graph, command = m.graph.Update(message)
		m.refreshGraph()
		return m, tea.Batch(command, m.scheduleAnimation())
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
	if m.nodeViewID != "" {
		switch {
		case key == "esc":
			m.closeNodeView()
			return nil, true
		case key == "c" && m.input.Value() == "":
			return m.cancelInspectedNode(), true
		case key == "enter" && m.inputFocused:
			return m.submitSteer(), true
		case key == "pgup" || key == "pgdown":
			m.pageNodeViewport(key == "pgdown")
			return nil, true
		case (key == "up" || key == "down") && m.input.Value() == "":
			m.scrollNodeFeed(key == "down")
			return nil, true
		case key == "end":
			m.nodeTrace.GotoBottom()
			return nil, true
		}
		return nil, false
	}
	if key == "ctrl+g" {
		m.toggleGraph()
		return nil, true
	}
	if key == "esc" {
		switch {
		case m.palette == paletteModel || m.palette == paletteMemory || m.palette == paletteHelp:
			m.closePalette()
		case m.paletteOpen():
			m.palette = paletteNone
			m.paletteDismissed = true
			m.setSize(m.width, m.height)
		case m.graphVisible() && m.focus == focusGraph && m.closeScopedGraph():
		case m.focus == focusCards && m.collapseSelectedCard():
		case m.focus == focusChat && m.collapseSelectedCard():
		case m.focus == focusCards:
			m.focus = focusInput
			m.inputFocused = true
			_ = m.input.Focus()
			m.setSize(m.width, m.height)
		case m.graphVisible() && m.focus == focusGraph:
			m.toggleGraph()
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
	if m.focus == focusCards && (key == "up" || key == "k" || key == "down" || key == "j") {
		delta := -1
		if key == "down" || key == "j" {
			delta = 1
		}
		m.moveCardSelection(delta)
		return nil, true
	}
	if key == "enter" && m.focus == focusCards {
		return m.advanceCard(m.selectedCardID, focusCards), true
	}
	if key == "enter" && m.focus == focusChat && m.selectedCardID != "" {
		if card := m.cardByID(m.selectedCardID); card != nil && card.State == cardSettled {
			return m.advanceCard(card.ID, focusChat), true
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
	if !m.inputFocused && (key == "[" || key == "]") {
		delta := -5
		if key == "]" {
			delta = 5
		}
		m.nudgeSplit(delta)
		return nil, true
	}
	if m.focus == focusGraph && (key == "up" || key == "k" || key == "down" || key == "j") {
		delta := -1
		if key == "down" || key == "j" {
			delta = 1
		}
		m.moveGraphSelection(delta)
		return nil, true
	}
	if key == "enter" && m.focus == focusGraph {
		return m.openSelectedNode(), true
	}
	if key == "enter" && m.inputFocused {
		return m.submit(), true
	}
	// An empty input has nothing for the arrows to do, so they read backwards
	// through the conversation instead of dying silently: up scrolls history
	// into view, down walks back toward now, End (or sending) re-pins.
	if m.inputFocused && m.input.Value() == "" && (key == "up" || key == "down") {
		if key == "up" {
			m.chat.SetYOffset(m.chat.YOffset - 3)
		} else {
			m.chat.SetYOffset(m.chat.YOffset + 3)
		}
		m.syncChatScroll()
		return nil, true
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
	nodeID := m.nodeViewID
	nodeAfterSeq := m.nodeLastSeq
	commander := m.commander
	return func() tea.Msg {
		messages, messagesErr := backend.Messages(sessionID, afterSeq, pollLimit)
		snapshot, snapshotErr := backend.ActiveSnapshot()
		cardSnapshot, cardSnapshotErr := backend.Snapshot()
		pending, pendingErr := backend.PendingCommands(pollLimit)
		usage, usageErr := backend.Usage()
		jobUsage, jobUsageErr := backend.TopLevelJobUsage()
		result := pollResultMsg{
			sessionID:       sessionID,
			messages:        messages,
			snapshot:        snapshot,
			cardSnapshot:    cardSnapshot,
			pending:         pending,
			usage:           usage,
			jobUsage:        jobUsage,
			messagesErr:     messagesErr,
			snapshotErr:     snapshotErr,
			cardSnapshotErr: cardSnapshotErr,
			pendingErr:      pendingErr,
			usageErr:        usageErr,
			jobUsageErr:     jobUsageErr,
		}
		seenCommands := make(map[int64]bool)
		for _, message := range messages {
			if message.CommandSeq == 0 || seenCommands[message.CommandSeq] {
				continue
			}
			seenCommands[message.CommandSeq] = true
			command, found, err := backend.CommandBySeq(message.CommandSeq)
			if err != nil {
				result.commandsErr = errors.Join(result.commandsErr, err)
			} else if found {
				result.commands = append(result.commands, command)
			}
		}
		if nodeID != "" {
			result.nodeID = nodeID
			result.node, result.nodeFound, result.nodeErr = backend.Node(nodeID)
			result.nodeMessages, result.nodeMessagesErr = backend.NodeMessages(nodeID, nodeAfterSeq, pollLimit)
			if commander != nil {
				result.nodeTrace = commander.NodeTrace(nodeID, nodeTraceMaxBytes)
			}
		}
		return result
	}
}

func nextPollTick() tea.Cmd {
	return tea.Tick(pollInterval, func(at time.Time) tea.Msg {
		return pollTickMsg(at)
	})
}

func nextAnimationTick() tea.Cmd {
	return tea.Tick(animationInterval, func(at time.Time) tea.Msg {
		return animationTickMsg(at)
	})
}

func (m *Model) scheduleAnimation() tea.Cmd {
	if m.animationPending || (!m.graphAnimating && m.revealSeq == 0) {
		return nil
	}
	m.animationPending = true
	return nextAnimationTick()
}

func (m *Model) applyPoll(result pollResultMsg) {
	if result.snapshotErr == nil {
		m.snapshot = result.snapshot
	}
	if result.cardSnapshotErr == nil {
		m.cardSnapshot = result.cardSnapshot
	}
	if result.pendingErr == nil {
		m.pending = result.pending
	}
	if result.usageErr == nil {
		m.usage = result.usage
	}
	if result.jobUsageErr == nil && result.jobUsage != nil {
		m.jobUsage = result.jobUsage
	}
	if result.commandsErr == nil {
		for _, command := range result.commands {
			m.commands[command.Seq] = command
		}
	}
	if result.nodeID != "" && result.nodeID == m.nodeViewID {
		if result.nodeErr == nil && result.nodeFound {
			m.inspectedNode = result.node
		}
		if result.nodeMessagesErr == nil {
			m.appendNodeMessages(result.nodeMessages)
		}
		m.nodeTraceText = result.nodeTrace
		m.refreshNodeView(false)
	}

	var accepted []store.Message
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
			accepted = append(accepted, message)
		}
	}

	m.rebuildCards()
	added := 0
	questionArrived := false
	for _, message := range accepted {
		if isQuestionMessage(message) {
			questionArrived = true
		}
		if !m.attentionMessage(message) {
			continue
		}
		added++
		// Only attention events animate. Ambient narration mutates its docked
		// card without manufacturing a new arrival in the conversation.
		if message.Role != store.RoleUser && m.autoScroll {
			m.revealSeq = message.Seq
			m.revealShown = 1
		}
	}

	// A state-only poll can move a card between the dock and its birth place,
	// so the footer and conversation both reflow even without a new message.
	if questionArrived {
		m.focus = focusInput
		m.inputFocused = true
		_ = m.input.Focus()
	}
	m.setSize(m.width, m.height)
	if added > 0 {
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
	if result.cardSnapshotErr != nil {
		problems = append(problems, fmt.Errorf("read card graph: %w", result.cardSnapshotErr))
	}
	if result.pendingErr != nil {
		problems = append(problems, fmt.Errorf("read pending commands: %w", result.pendingErr))
	}
	if result.usageErr != nil {
		problems = append(problems, fmt.Errorf("read usage: %w", result.usageErr))
	}
	if result.jobUsageErr != nil {
		problems = append(problems, fmt.Errorf("read job usage: %w", result.jobUsageErr))
	}
	if result.commandsErr != nil {
		problems = append(problems, fmt.Errorf("read card commands: %w", result.commandsErr))
	}
	if result.nodeID != "" && result.nodeID == m.nodeViewID {
		if result.nodeErr != nil {
			problems = append(problems, fmt.Errorf("read node: %w", result.nodeErr))
		} else if !result.nodeFound {
			problems = append(problems, fmt.Errorf("read node: %s not found", result.nodeID))
		}
		if result.nodeMessagesErr != nil {
			problems = append(problems, fmt.Errorf("read node messages: %w", result.nodeMessagesErr))
		}
	}
	m.err = errors.Join(problems...)
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
		posted, err := backend.PostMessage(message)
		return postResultMsg{message: posted, err: err}
	}
}

// toggleFocus cycles through the panes that are actually on screen: input and
// chat always, the task rail only while it is open.
func (m *Model) toggleFocus() tea.Cmd {
	order := []paneFocus{focusInput, focusChat}
	if m.activityBarVisible() && m.activeCardCount() > 0 {
		order = append(order, focusCards)
	}
	if m.graphVisible() {
		if m.horizontal {
			order = []paneFocus{focusInput, focusChat, focusGraph}
		} else {
			order = []paneFocus{focusInput, focusGraph}
		}
	}
	at := 0
	for index, pane := range order {
		if pane == m.focus {
			at = index
			break
		}
	}
	m.focus = order[(at+1)%len(order)]
	m.inputFocused = m.focus == focusInput
	if m.focus == focusGraph {
		m.ensureGraphSelection()
	}
	if m.focus == focusCards {
		m.ensureCardSelection()
	}
	if m.inputFocused {
		m.setSize(m.width, m.height)
		return m.input.Focus()
	}
	m.input.Blur()
	m.setSize(m.width, m.height)
	return nil
}

// toggleGraph opens or closes the task rail. Opening moves focus into it so
// the arrows work immediately; closing hands focus back to the input.
func (m *Model) toggleGraph() {
	m.graphOpen = !m.graphOpen
	m.graphScopeID = ""
	if m.graphOpen {
		m.focus = focusGraph
		m.inputFocused = false
		m.input.Blur()
		m.ensureGraphSelection()
	} else {
		m.focus = focusInput
		m.inputFocused = true
		_ = m.input.Focus()
	}
	m.setSize(m.width, m.height)
}

// graphVisible reports whether the task rail (or full task pane, when the
// terminal is narrow) is on screen.
func (m *Model) graphVisible() bool { return m.graphOpen && m.nodeViewID == "" }

// activityBarVisible reports whether the active-card dock sits above the
// input. Its quiet fallback still opens the rail for graph-only stores.
func (m *Model) activityBarVisible() bool {
	return !m.graphVisible() && m.nodeViewID == "" && !m.paletteOpen()
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
	barHeight := 0
	if m.activityBarVisible() {
		barHeight = m.cardDockHeight()
	}
	// top bar + blank + main + blank + palette + activity bar + input + hint
	mainHeight := max(3, m.height-3-paletteHeight-barHeight-m.input.LineCount()-footerHeight)
	if m.graphVisible() && m.horizontal {
		const gap = 2
		pct := m.splitPct
		if pct == 0 {
			pct = defaultSplitPct
		}
		m.chatWidth = max(20, (m.width-gap)*pct/100)
		m.graphWidth = max(12, m.width-gap-m.chatWidth)
	} else {
		m.chatWidth = m.width
		m.graphWidth = m.width
	}
	m.chatHeight = mainHeight
	m.graphHeight = mainHeight

	m.chat.Width = max(1, m.chatWidth-2) // breathing room on the right
	m.chat.Height = max(1, m.chatHeight)
	m.graph.Width = max(1, m.graphWidth-2)
	m.graph.Height = max(1, m.graphHeight-2) // header + blank
	m.input.Width = max(1, m.width-4)
	m.sizeNodeViewports()
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
	// The collapsed rail still shows a spinner in the activity bar, so live
	// work keeps the animation ticking even with the tree off screen.
	if !m.graphVisible() && m.nodeViewID == "" && m.liveWorkCount() > 0 {
		m.graphAnimating = true
	}
	if m.nodeViewID != "" &&
		(m.inspectedNode.Status == store.Claimed || m.inspectedNode.Status == store.Running ||
			m.completionFlashing(m.inspectedNode, time.Now())) {
		m.graphAnimating = true
	}
}

func (m *Model) pageFocused(down bool) {
	if m.nodeViewID != "" {
		m.pageNodeViewport(down)
		return
	}
	if m.focus == focusGraph {
		if down {
			m.graph.PageDown()
		} else {
			m.graph.PageUp()
		}
		m.refreshGraph()
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
	if m.draggingSplit {
		switch event.Action {
		case tea.MouseActionMotion:
			m.dragSplitTo(event.X)
			return true
		case tea.MouseActionRelease:
			m.draggingSplit = false
			m.saveSplit()
			return true
		}
	}
	if event.Button == tea.MouseButtonLeft && event.Action == tea.MouseActionPress && m.splitDividerHit(event.X, event.Y) {
		m.draggingSplit = true
		return true
	}
	if event.Button == tea.MouseButtonLeft && event.Action == tea.MouseActionPress && m.newMessagePillHit(event.X, event.Y) {
		m.pinChat()
		return true
	}
	if event.Button == tea.MouseButtonLeft && event.Action == tea.MouseActionPress {
		return m.updateMouseClick(event.X, event.Y)
	}
	if event.Button != tea.MouseButtonWheelUp && event.Button != tea.MouseButtonWheelDown {
		return false
	}
	down := event.Button == tea.MouseButtonWheelDown
	if m.nodeViewID != "" {
		return m.scrollNodeAt(event.X, event.Y, down)
	}
	if m.graphBounds.contains(event.X, event.Y) {
		if down {
			m.graph.SetYOffset(m.graph.YOffset + 3)
		} else {
			m.graph.SetYOffset(m.graph.YOffset - 3)
		}
		m.refreshGraph()
		return true
	}
	if m.chatBounds.contains(event.X, event.Y) {
		if down {
			m.chat.SetYOffset(m.chat.YOffset + 3)
		} else {
			m.chat.SetYOffset(m.chat.YOffset - 3)
		}
		m.syncChatScroll()
		return true
	}
	return false
}

// splitDividerHit reports whether (x, y) lands on the gap between the chat
// and graph panes — the grab zone for htop-style drag-to-resize.
func (m *Model) splitDividerHit(x, y int) bool {
	if !m.horizontal || m.nodeViewID != "" || m.graphBounds.width == 0 {
		return false
	}
	if y < m.chatBounds.y || y >= m.chatBounds.bottom() {
		return false
	}
	// Only the gap columns between the panes grab — the rail's first column
	// stays clickable for selecting nodes.
	return x >= m.chatBounds.right() && x < m.graphBounds.x
}

func (m *Model) dragSplitTo(x int) {
	total := m.width - 2
	if total <= 0 {
		return
	}
	pct := clampSplitPct(x * 100 / total)
	if pct == 0 || pct == m.splitPct {
		return
	}
	m.splitPct = pct
	m.setSize(m.width, m.height)
}

func (m *Model) nudgeSplit(delta int) {
	pct := m.splitPct
	if pct == 0 {
		pct = defaultSplitPct
	}
	pct = clampSplitPct(pct + delta)
	if pct == m.splitPct {
		return
	}
	m.splitPct = pct
	m.setSize(m.width, m.height)
	m.saveSplit()
}

func (m *Model) saveSplit() {
	if saved, ok := m.commander.(splitStore); ok && m.splitPct != 0 {
		saved.SaveSplitPct(m.splitPct)
	}
}

func (m *Model) newMessagePillHit(x, y int) bool {
	if m.newMessages == 0 || m.chatBounds.width == 0 {
		return false
	}
	pillWidth := lipgloss.Width(m.newMessageLabel()) + 2
	return x >= m.chatBounds.right()-pillWidth-2 && x < m.chatBounds.right() &&
		y == m.chatBounds.bottom()-1
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
