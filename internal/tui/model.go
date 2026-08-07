// Package tui renders the durable graph store as an interactive conversation.
// It is deliberately only a lens: messages are appended to the store and all
// graph changes arrive asynchronously from another process.
package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/voice"
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

type boostMode uint8

const (
	boostOff boostMode = iota
	boostArmed
	boostPinned
)

func (mode boostMode) next() boostMode {
	return (mode + 1) % 3
}

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
	role    string
	choices []ModelChoice
}

type pollResultMsg struct {
	sessionID         string
	messages          []store.Message
	snapshot          store.Snapshot
	cardSnapshot      store.Snapshot
	pending           []store.Command
	usage             store.TotalUsage
	jobUsage          map[string]store.JobUsage
	commands          []store.Command
	agentQuestions    []store.AgentQuestion
	messagesErr       error
	snapshotErr       error
	cardSnapshotErr   error
	pendingErr        error
	usageErr          error
	jobUsageErr       error
	commandsErr       error
	agentQuestionsErr error

	nodeID          string
	node            store.Node
	nodeFound       bool
	nodeMessages    []store.Message
	nodeTrace       string
	nodeErr         error
	nodeMessagesErr error
}

type postResultMsg struct {
	message     store.Message
	nodeID      string
	questionSeq int64
	err         error
}

type questionSurfaceResultMsg struct {
	questionSeq int64
	message     store.Message
	err         error
}

// Model is the Bubble Tea model for an aforge chat session.
type Model struct {
	backend        Backend
	sessionID      string
	commander      Commander
	standingReader standingReader
	standingNow    func() time.Time

	input textinput.Model
	chat  viewport.Model
	graph viewport.Model

	nodeTrace viewport.Model

	messages             []store.Message
	snapshot             store.Snapshot
	cardSnapshot         store.Snapshot
	pending              []store.Command
	usage                store.TotalUsage
	jobUsage             map[string]store.JobUsage
	commands             map[int64]store.Command
	agentQuestions       []store.AgentQuestion
	lastSeq              int64
	answeringQuestionSeq int64

	cards           []jobCard
	cardExpanded    map[string]bool
	selectedCardID  string
	graphScopeID    string
	cardReturnFocus paneFocus

	// A charter card is a rail sub-surface. Its firing history can open a
	// scoped job graph while the card id remains as the next back-out rung.
	charterCardID     string
	charterFocusIndex int
	standingRows      []standingRow
	charterRows       []charterCardRow

	// dockExpanded holds the overflow dock open without card focus; the
	// dockSummaryLine is the rendered ▸/▾ summary row, -1 when absent.
	dockExpanded          bool
	dockSummaryLine       int
	questionDockExpanded  bool
	questionDockSelection int
	questionDockRows      []questionDockRow

	// chatFocusIndex walks the thread zone's interactive lines (folds,
	// receipts, chips, cards) under keyboard traversal; enter activates
	// exactly what a click on that line would.
	chatFocusIndex int

	// Question options remain selected while the ordinary input keeps focus.
	questionSelection  map[string]int
	questionDismissed  map[string]bool
	cardOptionRows     []cardOptionRow
	notebookOptionRows []cardOptionRow

	selectedNodeID   string
	graphRows        []graphRow
	nodeViewID       string
	inspectedNode    store.Node
	nodeMessages     []store.Message
	nodeLastSeq      int64
	nodeTraceText    string
	chatDraft        string
	chatAttachments  []string
	returnFocus      paneFocus
	attachments      []string
	attachmentBounds []paneBounds

	feedRows   []feedRow
	feedBlocks []feedBlock
	// feedKeys are content-derived identities for feedBlocks, index-aligned.
	// feedExpanded is keyed by them, never by block position: the trace is
	// tail-truncated at its byte budget, so positions shift as it grows.
	feedKeys     []string
	feedExpanded map[string]bool

	// expandedMessages holds the seqs of long chat deliverables opened in
	// place; everything else shows its lead and a ⋯.
	expandedMessages map[int64]bool
	learningExpanded map[int64]bool
	briefExpanded    map[int64]bool
	selectedBriefSeq int64
	// The provider stream is optional Commander input. Real head deltas and
	// simulated landed answers share one paced renderer so neither path pops.
	streamEvents       <-chan StreamEvent
	streamMode         streamMode
	streamRaw          string
	streamTarget       string
	streamShown        string
	streamSeq          int64
	streamProviderDone bool
	streamQueue        []store.Message

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
	shimmerFrame     int
	receiptsExpanded bool
	historyExpanded  bool
	err              error

	palette               paletteKind
	paletteSelected       int
	paletteDismissed      bool
	modelRole             string
	modelSlotIndex        int
	modelCatalog          []ModelChoice
	mediaModelCatalogs    map[string][]ModelChoice
	mediaCatalogRequested map[string]bool
	mediaCatalogLoading   map[string]bool
	optimisticModels      map[string]string
	catalogRequested      bool
	catalogLoading        bool
	memoryFacts           []store.Fact
	notebookOpen          bool
	notebookQuery         string
	notebookFacts         []store.Fact
	notebookExpandedSeq   int64
	notebookOption        int
	status                string
	statusUntil           time.Time
	boost                 boostMode

	voiceRecorder         voice.Recorder
	voiceTranscriber      voice.Transcriber
	voiceState            voiceState
	voiceGeneration       int
	voiceStartedAt        time.Time
	voicePending          string
	voiceChunkText        map[int]string
	voiceNextChunk        int
	voiceInFlight         int
	voiceChunksClosed     bool
	voiceFinalReady       bool
	voiceFinalText        string
	voiceFinalErr         error
	voiceLevels           []float64
	voiceHint             string
	voiceHintUntil        time.Time
	voiceHintShown        bool
	voiceModelCatalog     []ModelChoice
	voiceCatalogRequested bool
	voiceCatalogLoading   bool
	voiceContext          context.Context
	voiceCancel           context.CancelFunc

	// Idle tips are session-local and share standingNow, the TUI's existing
	// injected time seam, so timing behavior stays deterministic in tests.
	tipActivityAt  time.Time
	tipLastShownAt time.Time
	tipCurrent     int
	tipSeen        map[int]bool
	voiceUsed      bool
	budgetUsed     bool

	// splitPct is the chat pane's share of the width in percent; zero means
	// the default. draggingSplit is true while the divider is held.
	splitPct      int
	draggingSplit bool

	chatBounds                paneBounds
	headerTasksBounds         paneBounds
	headerQuestionBounds      paneBounds
	headerModelsBounds        paneBounds
	headerFocusIndex          int
	graphBounds               paneBounds
	graphRowsBounds           paneBounds
	standingRowsBounds        paneBounds
	graphToggleBounds         paneBounds
	inputBounds               paneBounds
	boostBounds               paneBounds
	micBounds                 paneBounds
	voiceCancelBounds         paneBounds
	nodeBounds                paneBounds
	nodeTraceBounds           paneBounds
	nodeBackBounds            paneBounds
	paletteCloseBounds        paneBounds
	modelPickerBounds         paneBounds
	modelSlotRows             []modelSlotRow
	modelPickerRows           []modelPickerRow
	activityBarBounds         paneBounds
	textQuestionDismissBounds paneBounds
	cardDockRows              []cardRow
	chatCardRows              []cardRow
	cardPartRows              []cardPartRow
	cardCloseRows             []cardCloseRow

	// chatMessageRows maps rendered chat lines to the message seq they
	// belong to, so clicking a collapsed deliverable opens it in place.
	chatMessageRows []chatMessageRow

	// chatExpandRows maps the visible disclosure affordances in the thread
	// to the state they toggle. The whole rendered line is a click target.
	chatExpandRows []chatExpandRow

	// chatChipRows maps rendered provenance-chip lines to the task they
	// point at, so clicking `↳ title` opens that task's activity view.
	chatChipRows []chatChipRow
}

type paneFocus int

const (
	focusInput paneFocus = iota
	focusQuestions
	focusChat
	focusCards
	focusGraph
	focusHeader
)

type modelPickerRow struct {
	bounds paneBounds
	index  int
}

type modelSlotRow struct {
	bounds paneBounds
	index  int
}

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

// NewWithVoice injects deterministic audio services. It is primarily useful
// to embedders and tests; the resident chat commander supplies these services
// automatically in the normal application.
func NewWithVoice(backend Backend, sessionID string, commander Commander, recorder voice.Recorder, transcriber voice.Transcriber) *Model {
	m := newModel(backend, sessionID, commander)
	m.voiceRecorder = recorder
	m.voiceTranscriber = transcriber
	return m
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
		backend:               backend,
		sessionID:             sessionID,
		commander:             commander,
		input:                 input,
		chat:                  viewport.New(1, 1),
		graph:                 viewport.New(1, 1),
		nodeTrace:             viewport.New(1, 1),
		inputFocused:          true,
		focus:                 focusInput,
		autoScroll:            true,
		modelRole:             "talk",
		feedExpanded:          map[string]bool{},
		expandedMessages:      map[int64]bool{},
		learningExpanded:      map[int64]bool{},
		briefExpanded:         map[int64]bool{},
		jobUsage:              map[string]store.JobUsage{},
		commands:              map[int64]store.Command{},
		cardExpanded:          map[string]bool{},
		questionSelection:     map[string]int{},
		questionDismissed:     map[string]bool{},
		optimisticModels:      map[string]string{},
		mediaModelCatalogs:    map[string][]ModelChoice{},
		mediaCatalogRequested: map[string]bool{},
		mediaCatalogLoading:   map[string]bool{},
		dockSummaryLine:       -1,
		voiceChunkText:        map[int]string{},
		tipCurrent:            -1,
		tipSeen:               map[int]bool{},
	}
	if services, ok := commander.(voiceServices); ok {
		m.voiceRecorder = services.VoiceRecorder()
		m.voiceTranscriber = services.VoiceTranscriber()
	}
	if source, ok := commander.(streamSource); ok {
		m.streamEvents = source.StreamEvents()
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
	return tea.Batch(textinput.Blink, m.poll(), waitForStream(m.streamEvents))
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

	case StreamEvent:
		m.applyStreamEvent(message)
		return m, tea.Batch(waitForStream(m.streamEvents), m.scheduleAnimation())

	case streamClosedMsg:
		m.streamEvents = nil
		return m, nil

	case animationTickMsg:
		m.animationPending = false
		if m.streamMode != streamNone {
			m.advanceStream()
			m.refreshChat()
			if m.autoScroll {
				m.chat.GotoBottom()
			}
		}
		m.shimmerFrame++
		m.sampleVoiceLevel()
		if !m.graphAnimating && !m.streamAnimating() && !m.shimmerVisible() && !m.voiceAnimating() {
			return m, nil
		}
		m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
		m.refreshChat()
		m.refreshGraph()
		return m, m.scheduleAnimation()

	case catalogResultMsg:
		m.applyCatalog(message.role, message.choices)
		return m, nil

	case voiceStartedMsg:
		return m, m.applyVoiceStarted(message)

	case voiceChunkMsg:
		return m, m.applyVoiceChunk(message)

	case voiceChunkResultMsg:
		return m, m.applyVoiceChunkResult(message)

	case voiceFinalResultMsg:
		return m, m.applyVoiceFinal(message)

	case voiceStoppedMsg:
		return m, nil

	case voiceUsageRecordedMsg:
		return m, nil

	case postResultMsg:
		if message.err != nil {
			if message.questionSeq != 0 {
				m.answeringQuestionSeq = message.questionSeq
			}
			m.err = fmt.Errorf("send message: %w", message.err)
			return m, nil
		}
		if message.nodeID != "" {
			m.landOptimisticNodeMessage(message.nodeID, message.message)
		}
		m.err = nil
		return m, nil

	case questionSurfaceResultMsg:
		if message.err != nil {
			m.err = fmt.Errorf("surface question: %w", message.err)
			return m, nil
		}
		m.answeringQuestionSeq = message.questionSeq
		m.agentQuestions = removeAgentQuestion(m.agentQuestions, message.questionSeq)
		m.questionDockExpanded = false
		m.focus = focusInput
		m.inputFocused = true
		_ = m.input.Focus()
		m.setSize(m.width, m.height)
		return m, m.poll()

	case charterCommandResultMsg:
		if message.err != nil {
			m.err = fmt.Errorf("%s charter: %w", message.action, message.err)
			return m, nil
		}
		m.err = nil
		m.status = message.action + " requested → " + message.name
		m.statusUntil = time.Now().Add(statusTTL)
		return m, nil

	case modelCommandResultMsg:
		if message.err != nil {
			if message.previous == "" || message.previous == "–" {
				delete(m.optimisticModels, message.role)
			} else {
				m.optimisticModels[message.role] = message.previous
			}
			m.err = fmt.Errorf("request %s model: %w", message.role, message.err)
			return m, nil
		}
		m.err = nil
		m.status = fmt.Sprintf("%s model → %s", message.role, message.slug)
		m.statusUntil = time.Now().Add(statusTTL)
		return m, nil

	case tea.KeyMsg:
		m.noteKeypress()
		if command, handled := m.updateKey(message); handled {
			if command == nil {
				return m, m.scheduleAnimation()
			}
			return m, command
		}

	case tea.MouseMsg:
		if command, handled := m.updateMouse(message); handled {
			return m, tea.Batch(command, m.scheduleAnimation())
		}
	}

	if m.inputFocused {
		before := m.input.Value()
		var command tea.Cmd
		m.input, command = m.input.Update(message)
		if m.input.Value() != before && m.nodeViewID == "" {
			m.captureImageAttachments()
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
	if m.nodeViewID == "" && m.inputFocused && (key == "backspace" || key == "ctrl+h") &&
		m.input.Value() == "" && len(m.attachments) > 0 {
		m.removeAttachment(len(m.attachments) - 1)
		return nil, true
	}
	if key == keyBindings.graph {
		if m.nodeViewID != "" {
			m.closeNodeView()
		}
		m.toggleGraph()
		return nil, true
	}
	if key == keyBindings.voice {
		return m.toggleVoice(), true
	}
	if key == keyBindings.boost {
		m.toggleBoost()
		return nil, true
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
	if key == "esc" {
		if m.voiceState != voiceIdle {
			return m.cancelVoice(), true
		}
		// The back-out ladder (see the design-system comment in view.go):
		// expanded element → collapsed element → zone → input → quit.
		switch {
		case m.palette == paletteModel:
			m.returnToModelsPalette()
		case m.palette == paletteModels:
			m.palette = paletteNone
			m.paletteSelected = 0
			m.focus = focusHeader
			m.headerFocusIndex = 0
			m.setSize(m.width, m.height)
		case m.palette == paletteMemory || m.palette == paletteHelp:
			m.closePalette()
		case m.paletteOpen():
			m.palette = paletteNone
			m.paletteDismissed = true
			m.setSize(m.width, m.height)
		case m.graphVisible() && m.focus == focusGraph && m.closeScopedGraph():
		case m.graphVisible() && m.focus == focusGraph && m.closeCharterCard():
		case m.focus == focusCards && m.collapseSelectedCard():
		case m.focus == focusChat && m.collapseSelectedCard():
		case m.focus == focusChat && m.collapseSelectedBrief():
		case m.notebookOpen && m.collapseNotebook():
		case m.notebookOpen:
			m.closeNotebook()
		case m.focus == focusCards:
			m.dockExpanded = false
			m.focus = focusInput
			m.inputFocused = true
			_ = m.input.Focus()
			m.setSize(m.width, m.height)
		case m.focus == focusQuestions && m.questionDockExpanded:
			m.questionDockExpanded = false
			m.setSize(m.width, m.height)
		case m.focus == focusQuestions:
			m.focus = focusInput
			m.inputFocused = true
			_ = m.input.Focus()
			m.setSize(m.width, m.height)
		case m.focus == focusChat:
			m.focus = focusInput
			m.inputFocused = true
			_ = m.input.Focus()
			m.setSize(m.width, m.height)
		case m.graphVisible() && m.focus == focusGraph:
			m.toggleGraph()
		case m.focus == focusHeader:
			m.focus = focusInput
			m.inputFocused = true
			_ = m.input.Focus()
			m.setSize(m.width, m.height)
		case m.focus == focusInput && m.dismissTextQuestion():
		case m.input.Value() != "" || len(m.attachments) > 0:
			m.input.Reset()
			m.attachments = nil
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
	if m.notebookOpen && len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		m.activateNotebookNumber(int(key[0] - '0'))
		return nil, true
	}
	if m.focus == focusHeader {
		switch key {
		case "left", "up", "k":
			m.headerFocusIndex = (m.headerFocusIndex + 1) % 2
			return nil, true
		case "right", "down", "j":
			m.headerFocusIndex = (m.headerFocusIndex + 1) % 2
			return nil, true
		case "enter":
			return m.activateHeaderFocus(), true
		}
	}
	if m.focus == focusQuestions {
		switch key {
		case "up", "k":
			m.moveAgentQuestionSelection(-1)
			return nil, true
		case "down", "j":
			m.moveAgentQuestionSelection(1)
			return nil, true
		case "enter":
			if !m.questionDockExpanded {
				m.questionDockExpanded = true
				m.setSize(m.width, m.height)
				return nil, true
			}
			return m.surfaceSelectedAgentQuestion(), true
		}
	}
	// Numbered question options are a layer over the normal live input. An
	// empty input enables shortcuts; once text exists, every key and enter
	// continue through the ordinary free-text path.
	if m.inputFocused && m.input.Value() == "" {
		if card := m.questionCardWithOptions(); card != nil {
			switch {
			case key == "up" || key == "k":
				m.moveQuestionSelection(card.ID, -1)
				return nil, true
			case key == "down" || key == "j":
				m.moveQuestionSelection(card.ID, 1)
				return nil, true
			case key == "enter":
				return m.submitSelectedQuestionOption(card.ID), true
			case len(key) == 1 && key[0] >= '1' && key[0] <= '9':
				if command, ok := m.submitQuestionOptionNumber(card.ID, int(key[0]-'0')); ok {
					return command, true
				}
			}
		}
	}
	if m.focus == focusCards {
		if card := m.selectedQuestionCardWithOptions(); card != nil {
			if key == "up" || key == "k" || key == "down" || key == "j" {
				delta := -1
				if key == "down" || key == "j" {
					delta = 1
				}
				m.moveQuestionSelection(card.ID, delta)
				return nil, true
			}
			if key == "enter" {
				return m.submitSelectedQuestionOption(card.ID), true
			}
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
	// Thread-zone traversal: ↑/↓ walk the interactive lines, enter activates
	// exactly what a click on the focused line would; pgup/pgdn and the wheel
	// keep scrolling.
	if m.notebookOpen && m.focus == focusChat && (key == "left" || key == "right") &&
		m.notebookExpandedSeq != 0 {
		if key == "left" {
			m.notebookOption = 0
		} else {
			m.notebookOption = 1
		}
		m.refreshChat()
		m.focusNotebookOptionRow()
		return nil, true
	}
	if m.focus == focusChat && (key == "up" || key == "k" || key == "down" || key == "j") {
		delta := -1
		if key == "down" || key == "j" {
			delta = 1
		}
		m.moveChatFocus(delta)
		return nil, true
	}
	if key == "enter" && m.focus == focusChat {
		if m.activateChatFocus() {
			return nil, true
		}
		if card := m.cardByID(m.selectedCardID); card != nil && card.State == cardSettled {
			return m.advanceCard(card.ID, focusChat), true
		}
		return nil, true
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
	if m.focus == focusGraph && m.charterCardID != "" && m.graphScopeID == "" &&
		(key == "up" || key == "k" || key == "down" || key == "j") {
		delta := -1
		if key == "down" || key == "j" {
			delta = 1
		}
		m.moveCharterSelection(delta)
		return nil, true
	}
	if key == "enter" && m.focus == focusGraph && m.charterCardID != "" && m.graphScopeID == "" {
		return m.activateCharterSelection(), true
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
		if m.voiceState != voiceIdle {
			return m.flashVoiceHint("finish voice input first · " + keyBindings.voice), true
		}
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
		var agentQuestions []store.AgentQuestion
		var agentQuestionsErr error
		if reader, ok := backend.(interface {
			PendingQuestions(string, int) ([]store.AgentQuestion, error)
		}); ok {
			agentQuestions, agentQuestionsErr = reader.PendingQuestions(sessionID, pollLimit)
		}
		result := pollResultMsg{
			sessionID:         sessionID,
			messages:          messages,
			snapshot:          snapshot,
			cardSnapshot:      cardSnapshot,
			pending:           pending,
			usage:             usage,
			jobUsage:          jobUsage,
			messagesErr:       messagesErr,
			snapshotErr:       snapshotErr,
			cardSnapshotErr:   cardSnapshotErr,
			pendingErr:        pendingErr,
			usageErr:          usageErr,
			jobUsageErr:       jobUsageErr,
			agentQuestions:    agentQuestions,
			agentQuestionsErr: agentQuestionsErr,
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
	if m.animationPending || (!m.graphAnimating && !m.streamAnimating() && !m.shimmerVisible() && !m.voiceAnimating()) {
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
	if result.agentQuestionsErr == nil {
		m.agentQuestions = m.agentQuestions[:0]
		for _, question := range result.agentQuestions {
			if question.Urgency != store.QuestionBlocking && question.Status == store.QuestionPending {
				m.agentQuestions = append(m.agentQuestions, question)
			}
		}
		if len(m.agentQuestions) == 0 && m.focus == focusQuestions {
			m.focus = focusInput
			m.inputFocused = true
			_ = m.input.Focus()
		}
		m.questionDockSelection = max(0, min(m.questionDockSelection, len(m.agentQuestions)-1))
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
			if card := m.cardForMessage(message); card != nil {
				delete(m.questionDismissed, card.ID)
			}
		}
		if !m.attentionMessage(message) {
			continue
		}
		added++
		// Real head deltas keep their durable landing; provider paths that did
		// not stream, plus settled deliverables, enter the same paced renderer.
		// Ambient narration only mutates its card and shimmer line.
		if message.Role != store.RoleUser && !m.matchRealStream(message) &&
			m.shouldSimulateStream(message) {
			m.queueSimulatedStream(message)
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
	if result.agentQuestionsErr != nil {
		problems = append(problems, fmt.Errorf("read agent questions: %w", result.agentQuestionsErr))
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
	if body == "" && len(m.attachments) == 0 {
		return nil
	}
	if strings.HasPrefix(body, "/") {
		return m.executeSlash(body)
	}
	if m.notebookOpen {
		m.closeNotebook()
	}
	attachments := append([]string(nil), m.attachments...)
	if len(attachments) > 0 {
		model, supported := m.imageInputSupport()
		if !supported {
			body = strings.TrimSpace(strings.Join(append([]string{body}, attachments...), " "))
			attachments = nil
			_ = model
		} else if body == "" {
			body = "Image attached."
		}
	}
	m.input.Reset()
	m.attachments = nil
	return m.postUserMessage(body, attachments...)
}

func (m *Model) postUserMessage(body string, attachments ...string) tea.Cmd {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	m.err = nil
	backend := m.backend
	messageModel := ""
	if m.boost != boostOff {
		messageModel = strings.TrimSpace(m.currentModel("boost"))
		if messageModel == "–" {
			messageModel = ""
		}
		if m.boost == boostArmed {
			// The one-shot is consumed by submission, not by a successful post or
			// reply. A failed provider call must never leave an expensive surprise
			// armed for the following message.
			m.boost = boostOff
			m.setSize(m.width, m.height)
		}
	}
	message := store.Message{
		SessionID:   m.sessionID,
		Role:        store.RoleUser,
		Body:        body,
		Attachments: attachments,
		Model:       messageModel,
		QuestionSeq: m.answeringQuestionSeq,
	}
	m.answeringQuestionSeq = 0
	return func() tea.Msg {
		posted, err := backend.PostMessage(message)
		return postResultMsg{message: posted, questionSeq: message.QuestionSeq, err: err}
	}
}

func (m *Model) toggleBoost() {
	m.boost = m.boost.next()
	m.setSize(m.width, m.height)
}

// toggleFocus cycles the zones actually on screen, in the documented order:
// input → dock → thread → rail (rail only while open; a narrow rail takes the
// whole main area, so the thread zone yields to it).
func (m *Model) toggleFocus() tea.Cmd {
	order := []paneFocus{focusInput}
	if m.activityBarVisible() && len(m.agentQuestions) > 0 {
		order = append(order, focusQuestions)
	}
	if m.activityBarVisible() && m.activeCardCount() > 0 {
		order = append(order, focusCards)
	}
	order = append(order, focusChat)
	if m.graphVisible() {
		if m.horizontal {
			order = []paneFocus{focusInput, focusChat, focusGraph, focusHeader}
		} else {
			order = []paneFocus{focusInput, focusGraph, focusHeader}
		}
	} else {
		order = append(order, focusHeader)
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
	if m.focus == focusQuestions {
		m.questionDockSelection = max(0, min(m.questionDockSelection, len(m.agentQuestions)-1))
	}
	if m.focus == focusChat {
		// Enter the thread at its newest interactive line — context lives at
		// the bottom of a conversation.
		m.chatFocusIndex = 1 << 30
	}
	if m.focus == focusHeader {
		m.headerFocusIndex = max(0, min(1, m.headerFocusIndex))
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
	m.charterCardID = ""
	m.charterFocusIndex = 0
	m.palette = paletteNone
	m.paletteDismissed = false
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
	// The input's width determines how many rows it wraps to, and every height
	// below is measured against that row count — so the width must land first
	// or a resize computes the frame against the stale wrap.
	pendingReserve := 0
	if strings.TrimSpace(m.voicePending) != "" {
		pendingReserve = min(32, max(12, m.width/3))
	}
	m.input.Width = max(1, m.width-4-m.voiceControlWidth()-pendingReserve)

	paletteHeight := m.layoutPaletteHeight()
	footerHeight := 1
	if m.paletteOpen() {
		footerHeight = 0
	}
	barHeight := 0
	if m.activityBarVisible() {
		barHeight = m.cardDockHeight()
	}
	// top bar + blank + main + blank + palette + activity bar + input + hint
	mainHeight := max(3, m.height-3-paletteHeight-barHeight-m.inputSurfaceHeight()-footerHeight)
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
	// Header + blank + the exact static standing-section budget.
	m.graph.Height = max(1, m.graphHeight-2-m.standingSectionHeight())
	m.sizeNodeViewports()
	m.refreshChat()
	m.refreshGraph()
	if m.autoScroll {
		m.chat.GotoBottom()
	}
}

func (m *Model) refreshGraph() {
	offset := m.graph.YOffset
	if m.charterCardID != "" && m.graphScopeID == "" {
		m.graphRows = nil
		m.standingRows = nil
		m.graphAnimating = false
		m.graph.SetContent(m.renderCharterCardBody(max(1, m.graph.Width)))
	} else {
		m.graph.SetContent(m.renderTree(max(1, m.graph.Width), 0))
	}
	m.graph.SetYOffset(offset)
	if m.standingBreathing() {
		m.graphAnimating = true
	}
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

// moveChatFocus walks the thread's interactive lines with the arrows. A
// thread with nothing interactive keeps the arrows useful by scrolling.
func (m *Model) moveChatFocus(delta int) {
	targets := m.chatFocusLines()
	if len(targets) == 0 {
		m.chat.SetYOffset(m.chat.YOffset + 3*delta)
		m.syncChatScroll()
		return
	}
	m.chatFocusIndex = max(0, min(len(targets)-1, m.chatFocusIndex+delta))
	m.refreshChat()
	line := targets[m.chatFocusIndex]
	if line < m.chat.YOffset {
		m.chat.SetYOffset(line)
	} else if line >= m.chat.YOffset+max(1, m.chat.Height) {
		m.chat.SetYOffset(line - max(1, m.chat.Height) + 1)
	}
	m.syncChatScroll()
}

// activateChatFocus is enter-equals-click for the thread zone: it triggers
// whatever a click on the focused interactive line would.
func (m *Model) activateChatFocus() bool {
	targets := m.chatFocusLines()
	if len(targets) == 0 {
		return false
	}
	m.chatFocusIndex = max(0, min(m.chatFocusIndex, len(targets)-1))
	return m.activateChatLine(targets[m.chatFocusIndex])
}

func (m *Model) updateMouse(message tea.MouseMsg) (tea.Cmd, bool) {
	event := tea.MouseEvent(message)
	if m.draggingSplit {
		switch event.Action {
		case tea.MouseActionMotion:
			m.dragSplitTo(event.X)
			return nil, true
		case tea.MouseActionRelease:
			m.draggingSplit = false
			m.saveSplit()
			return nil, true
		}
	}
	if event.Button == tea.MouseButtonLeft && event.Action == tea.MouseActionPress && m.splitDividerHit(event.X, event.Y) {
		m.draggingSplit = true
		return nil, true
	}
	if event.Button == tea.MouseButtonLeft && event.Action == tea.MouseActionPress && m.newMessagePillHit(event.X, event.Y) {
		m.pinChat()
		return nil, true
	}
	if event.Button == tea.MouseButtonLeft && event.Action == tea.MouseActionPress {
		return m.updateMouseClick(event.X, event.Y)
	}
	if event.Button != tea.MouseButtonWheelUp && event.Button != tea.MouseButtonWheelDown {
		return nil, false
	}
	down := event.Button == tea.MouseButtonWheelDown
	if m.nodeViewID != "" {
		return nil, m.scrollNodeAt(event.X, event.Y, down)
	}
	if m.graphBounds.contains(event.X, event.Y) {
		if down {
			m.graph.SetYOffset(m.graph.YOffset + 3)
		} else {
			m.graph.SetYOffset(m.graph.YOffset - 3)
		}
		m.refreshGraph()
		return nil, true
	}
	if m.chatBounds.contains(event.X, event.Y) {
		if down {
			m.chat.SetYOffset(m.chat.YOffset + 3)
		} else {
			m.chat.SetYOffset(m.chat.YOffset - 3)
		}
		m.syncChatScroll()
		return nil, true
	}
	return nil, false
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

// refreshChat re-renders the thread. Pinned-to-bottom stays pinned; a reader
// scrolled up keeps the same content on screen even when a card lands at its
// birth position above them or an earlier block changes height — the offset is
// re-derived from a stable anchor (message seq or card id), not reused raw.
func (m *Model) refreshChat() {
	if m.autoScroll {
		m.chat.SetContent(m.renderMessages())
		m.chat.GotoBottom()
		return
	}
	offset := m.chat.YOffset
	anchor := m.captureChatAnchor(offset)
	m.chat.SetContent(m.renderMessages())
	m.chat.SetYOffset(m.resolveChatAnchor(anchor, offset))
}

// chatAnchor names the stable thing rendered at the top of the viewport: a
// message by journal seq or a card by id, plus how far into it the reader was.
type chatAnchor struct {
	ok     bool
	seq    int64
	cardID string
	delta  int
}

// captureChatAnchor reads the current row maps (built by the previous render)
// and picks the last row starting at or above the offset.
func (m *Model) captureChatAnchor(offset int) chatAnchor {
	anchor := chatAnchor{}
	best := -1
	for _, row := range m.chatCardRows {
		if row.start <= offset && row.start > best {
			best = row.start
			anchor = chatAnchor{ok: true, cardID: row.cardID, delta: offset - row.start}
		}
	}
	// A message row wins ties: it is the finer anchor (a settled card's
	// deliverable body has its own message row inside the card's span).
	for _, row := range m.chatMessageRows {
		if row.start <= offset && row.start >= best {
			best = row.start
			anchor = chatAnchor{ok: true, seq: row.seq, delta: offset - row.start}
		}
	}
	return anchor
}

// resolveChatAnchor maps an anchor back to an offset against the freshly
// rendered row maps. A message that was absorbed into a card since the last
// render resolves to that card; anything unresolvable keeps the raw offset.
func (m *Model) resolveChatAnchor(anchor chatAnchor, fallback int) int {
	if !anchor.ok {
		return fallback
	}
	if anchor.seq != 0 {
		for _, row := range m.chatMessageRows {
			if row.seq == anchor.seq {
				return row.start + anchor.delta
			}
		}
		if card := m.cardForMessageSeq(anchor.seq); card != nil {
			for _, row := range m.chatCardRows {
				if row.cardID == card.ID {
					return row.start
				}
			}
		}
		return fallback
	}
	for _, row := range m.chatCardRows {
		if row.cardID == anchor.cardID {
			return row.start + anchor.delta
		}
	}
	return fallback
}

// cardForMessageSeq finds the card that owns a thread message, if any.
func (m *Model) cardForMessageSeq(seq int64) *jobCard {
	for index := range m.cards {
		card := &m.cards[index]
		if card.Deliverable != nil && card.Deliverable.Seq == seq {
			return card
		}
		for _, message := range card.Messages {
			if message.Seq == seq {
				return card
			}
		}
	}
	return nil
}
