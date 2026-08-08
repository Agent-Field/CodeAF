// Package tui renders the durable graph store as an interactive conversation.
// It is deliberately only a lens: messages are appended to the store and all
// graph changes arrive asynchronously from another process.
package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/voice"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// composerPlaceholder is the first sentence the product says to anyone, and it
// is the highest-traffic string in the whole surface. It used to read "Ask the
// graph…", which taught a word we invented before the user had typed anything
// — the design filter's own failure case, at the one place it costs the most.
// The resting state names no machinery at all: there is one place to speak and
// nothing to learn before speaking in it.
const composerPlaceholder = "Ask for anything…"

const (
	pollInterval      = 400 * time.Millisecond
	animationInterval = 120 * time.Millisecond
	pollLimit         = 200
	railAtWidth       = 100
	statusTTL         = 3 * time.Second
	nodeTraceMaxBytes = 64 << 10
	// pollIdleInterval is the cadence a quiet store falls back to. The graph is
	// permanent, so every snapshot read costs more as history grows; a store
	// nobody is writing has nothing to report, and two seconds is still well
	// inside the delay a person reads as immediate.
	pollIdleInterval = 2 * time.Second
	// pollQuietAfter is how long the journal must sit unchanged before the tick
	// decays. It outlasts the gap between a send and the head's first event, so
	// an ordinary turn never drops out of the hot cadence mid-answer.
	pollQuietAfter = 10 * time.Second
	// pollActiveAfter keeps the hot cadence for a moment after the user acts:
	// they just asked for something and its first event is imminent.
	pollActiveAfter = 3 * time.Second
	// quietRepaintInterval refreshes the cached panes from state already in
	// hand, with no store read at all. Relative timestamps are minute-grained,
	// so that is exactly how often "now" can go stale while nothing happens.
	quietRepaintInterval = time.Minute
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

// journalReader is the cheapest possible proof that nothing happened. Every
// projection this lens reads — thread, graph, commands, usage, questions,
// charters, receipts — is written in the same transaction as an events row, so
// an unchanged watermark means an unchanged answer for all of them. It stays
// an optional capability: a backend without it is simply always read in full.
type journalReader interface {
	LatestEventSeq() (int64, error)
}

// selfActivityReader is the resident-life slice of the store. It stays an
// optional backend capability so lightweight TUI embedders do not have to
// implement the resident, while the real store supplies every value.
type selfActivityReader interface {
	SelfSpendToday() (float64, error)
	SelfReceipts(time.Time) ([]store.SelfReceipt, error)
	FactBySeq(int64) (store.Fact, bool, error)
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
	// quiet says the journal watermark had not moved, so no other field was
	// read and none carries meaning. It is the whole point of the poll: the
	// answer "nothing changed" costs one indexed row instead of ten scans.
	quiet             bool
	journalSeq        int64
	journalRead       bool
	sessionID         string
	messages          []store.Message
	snapshot          store.Snapshot
	cardSnapshot      store.Snapshot
	pending           []store.Command
	usage             store.TotalUsage
	jobUsage          map[string]store.JobUsage
	commands          []store.Command
	agentQuestions    []store.AgentQuestion
	selfSpendToday    float64
	selfReceipts      []store.SelfReceipt
	selfLearning      string
	messagesErr       error
	snapshotErr       error
	cardSnapshotErr   error
	pendingErr        error
	usageErr          error
	jobUsageErr       error
	commandsErr       error
	agentQuestionsErr error
	selfCharters      []store.Charter
	chartersRead      bool
	selfChartersErr   error

	selfDataRead      bool
	selfSpend         float64
	selfCompetence    store.CompetenceMap
	selfFacts         []store.Fact
	selfSkills        []store.Fact
	selfCrafts        []CraftSummary
	selfCraftsRead    bool
	selfReceiptsErr   error
	selfSpendErr      error
	selfCompetenceErr error
	selfFactsErr      error
	selfSkillsErr     error
	selfCraftsErr     error

	nodeID          string
	node            store.Node
	nodeFound       bool
	nodeMessages    []store.Message
	nodeTrace       string
	nodeErr         error
	nodeMessagesErr error

	// residency rides every cycle, quiet ones included: which process runs the
	// brain can change without a single row moving in the journal, so the
	// watermark cannot speak for it. residencyRead separates "the surface said
	// this window is the resident" from "no surface was asked".
	residency     Residency
	residencyRead bool
	// adopt is the replacement commander the surface handed back when the role
	// moved. Nil on every ordinary cycle.
	adopt Commander
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
	self  viewport.Model

	nodeTrace viewport.Model

	messages             []store.Message
	snapshot             store.Snapshot
	cardSnapshot         store.Snapshot
	pending              []store.Command
	usage                store.TotalUsage
	jobUsage             map[string]store.JobUsage
	commands             map[int64]store.Command
	agentQuestions       []store.AgentQuestion
	selfSpendToday       float64
	selfReceiptSeq       int64
	selfReceiptAt        time.Time
	selfLearning         string
	lastSeq              int64
	answeringQuestionSeq int64

	// journalSeq is the store's event watermark as of the last full read, and
	// the only thing an idle poll looks at. pollForce covers what the journal
	// cannot see: the first read and any key or click that may have changed
	// which surface is displayed. A node view is deliberately not in that set:
	// its trace is a file, read on every cycle regardless, while everything it
	// reads from SQL is journal-backed like the rest.
	journalSeq    int64
	journalPrimed bool
	pollForce     bool
	lastChangeAt  time.Time
	lastActionAt  time.Time
	lastRepaintAt time.Time

	// The rail's two store-backed sections are read once per poll, not once
	// per accessor: six render and layout sites ask for them, so an uncached
	// read put six synchronous SQLite queries on the UI goroutine per frame —
	// around three hundred a second during a stream. Both tables are written
	// in the same transaction as the journal row the poll watermark already
	// gates, so the poll is exactly the right refresh point. Only the read is
	// cached: the presentation projection still runs against the live
	// snapshot, so nothing the graph knows can go stale behind it.
	railServices      []store.Service
	railServicesValid bool
	railCharters      []store.Charter
	railChartersErr   error
	railChartersValid bool

	// Self is a read-only employee file assembled by the ordinary store poll:
	// a root list of what aforge does unattended, and one drill-in at a time
	// behind it. Every list is windowed and filterable, so the place opens
	// just as fast on a six-month-old brain as on an empty one.
	selfOpen        bool
	selfReceipts    []store.SelfReceipt
	selfSpend       float64
	selfCompetence  store.CompetenceMap
	selfFacts       []store.Fact
	selfSkills      []store.Fact
	selfCrafts      []CraftSummary
	selfBeliefHits  []store.Fact
	selfCharters    []store.Charter
	selfRoute       selfRoute
	selfCraftName   string
	selfCraftDetail CraftDetail
	selfPracticeKey string
	selfQuery       string
	selfShown       int
	selfShowOlder   bool
	selfSelection   int
	selfRows        []selfRow

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
	// A cadence is edited where it is read: the action row becomes a field.
	charterEditing    bool
	charterCadence    textinput.Model
	serviceCardID     string
	serviceFocusIndex int
	serviceRows       []serviceRow
	serviceCardRows   []serviceCardRow

	// The dock is measured before it is placed and drawn after, and the layout
	// measures it again on every relayout. It is rendered once per frame and
	// held here with its height; the frame and the relayout each drop it.
	dockContent string
	dockHeight  int
	dockValid   bool

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

	// threadQuestion is the open askback the thread itself owns: a question
	// the head asked with no job behind it, so nothing in the card derivation
	// can speak for it. It is card-shaped because every answer path — a digit,
	// the arrows, enter, a click, the input placeholder — resolves one target,
	// and it lives outside m.cards so a question never becomes a job row.
	threadQuestion *jobCard

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
	streamEvents <-chan StreamEvent
	streamMode   streamMode
	// residency is the window's own account of which process runs the brain.
	// The zero value is "this one", so a single-window session carries none of
	// this and renders as it always has.
	residency       Residency
	residencySource residentSurface
	streamRearm     bool
	// streamRaw accumulates the provider's raw structured response. It is a
	// builder, not a string: a token-by-token `+=` re-allocates the whole reply
	// per token, which is quadratic over a long answer.
	streamRaw          strings.Builder
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
	// nodeDetailsClipped says the header could not hold the whole outcome, so
	// the feed carries it in full rather than the reader losing the rest.
	nodeDetailsClipped bool
	nodeTraceHeight    int

	inputFocused     bool
	focus            paneFocus
	graphOpen        bool
	autoScroll       bool
	newMessages      int
	spinnerFrame     int
	animationPending bool
	graphAnimating   bool
	shimmerFrame     int
	// shimmerSeen dates each live card's status line so a wedged worker stops
	// breathing; sweepCache holds this frame's swept lines, which every line on
	// screen shares a phase with.
	shimmerSeen map[string]shimmerStamp
	sweepFrame  int
	sweepCache  map[string]string
	// awaitingSeq is the user turn this window posted and has not been answered
	// for yet, and awaitingSince is when it went out. Together they are the
	// whole presence indicator: a window only ever waits on its own words.
	awaitingSeq      int64
	awaitingSince    time.Time
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
	helpReturnFocus       paneFocus
	helpReturnInput       bool
	settingsRegistry      *config.Settings
	settingsGroups        []config.SettingGroup
	settingsIndex         int
	settingsOffset        int
	settingsEditing       bool
	settingsEditor        textinput.Model
	settingsError         string
	settingsReturnFocus   paneFocus
	settingsReturnInput   bool
	modelPickerReturn     paletteKind
	historyEntries        []historyEntry
	historyTerms          string
	historyVisible        bool
	historyLoading        bool
	historyErr            error
	historySelection      int
	historyOpen           int
	historyGeneration     int
	status                string
	statusUntil           time.Time
	headerStatusShown     bool
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

	chatBounds           paneBounds
	headerTasksBounds    paneBounds
	headerThreadBounds   paneBounds
	headerBoardBounds    paneBounds
	headerSelfBounds     paneBounds
	headerQuestionBounds paneBounds
	headerModelsBounds   paneBounds
	headerHelpBounds     paneBounds
	headerFocusIndex     int
	// headerPlacesShown records whether the last frame drew the place labels:
	// a narrow header folds them into the wordmark, and a focus ring around
	// something that is not on screen is worse than not reaching it.
	headerPlacesShown         bool
	graphBounds               paneBounds
	graphRowsBounds           paneBounds
	standingRowsBounds        paneBounds
	serviceRowsBounds         paneBounds
	graphToggleBounds         paneBounds
	selfBounds                paneBounds
	inputBounds               paneBounds
	boostBounds               paneBounds
	micBounds                 paneBounds
	voiceCancelBounds         paneBounds
	nodeBounds                paneBounds
	nodeTraceBounds           paneBounds
	nodeBackBounds            paneBounds
	paletteCloseBounds        paneBounds
	helpBounds                paneBounds
	settingsBounds            paneBounds
	headerSettingsBounds      paneBounds
	settingsRowHits           []settingsRowBounds
	modelPickerBounds         paneBounds
	modelSlotRows             []modelSlotRow
	modelPickerRows           []modelPickerRow
	activityBarBounds         paneBounds
	textQuestionDismissBounds paneBounds
	cardDockRows              []cardRow
	chatCardRows              []cardRow
	cardPartRows              []cardPartRow
	cardCloseRows             []cardCloseRow

	// blockCache holds already-rendered settled message groups. A settled
	// message is immutable and the thread is re-rendered many times between
	// two of them — every animation tick, every relayout — so the whole
	// conversation was being rebuilt to draw one moving tail. The cache is
	// dropped whole when the pane's width changes or the journal moves, which
	// is the only way the parts of a block outside its key can change.
	blockCache map[string]threadBlock
	blockWidth int
	blockGen   uint64
	threadGen  uint64

	// chatMessageRows maps rendered chat lines to the message seq they
	// belong to, so clicking a collapsed deliverable opens it in place.
	chatMessageRows []chatMessageRow

	// chatExpandRows maps the visible disclosure affordances in the thread
	// to the state they toggle. The whole rendered line is a click target.
	chatExpandRows []chatExpandRow

	// chatChipRows maps rendered provenance-chip lines to the task they
	// point at, so clicking `↳ title` opens that task's activity view.
	chatChipRows []chatChipRow
	historyRows  []historyRow
}

type paneFocus int

const (
	focusInput paneFocus = iota
	focusQuestions
	focusChat
	focusCards
	focusGraph
	focusSelf
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
	input.Placeholder = composerPlaceholder
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
		settingsEditor:        newSettingsEditor(),
		chat:                  viewport.New(1, 1),
		graph:                 viewport.New(1, 1),
		self:                  viewport.New(1, 1),
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
		historyOpen:           -1,
		voiceChunkText:        map[int]string{},
		tipCurrent:            -1,
		tipSeen:               map[int]bool{},
		selfShown:             selfWindow,
	}
	m.adoptCommander(commander)
	if saved, ok := commander.(splitStore); ok {
		m.splitPct = clampSplitPct(saved.SplitPct())
	}
	m.setSize(100, 30)
	return m
}

// adoptCommander wires the capabilities a commander optionally offers.
//
// It runs at construction and again whenever the window's commander is
// replaced, which is what taking or giving up the resident role looks like from
// in here. Voice and the provider stream are set or cleared rather than merely
// set: they are the two capabilities whose absence the user can feel, and a
// window that has just lost its head must stop offering a microphone it can no
// longer transcribe with. The saved divider position is deliberately not
// re-read — the person may have dragged it since, and a role change is not a
// reason to move their pane.
func (m *Model) adoptCommander(commander Commander) {
	m.commander = commander
	m.residencySource, _ = commander.(residentSurface)
	if services, ok := commander.(voiceServices); ok {
		m.voiceRecorder = services.VoiceRecorder()
		m.voiceTranscriber = services.VoiceTranscriber()
	} else {
		m.voiceRecorder, m.voiceTranscriber = nil, nil
	}
	if source, ok := commander.(streamSource); ok {
		m.streamEvents = source.StreamEvents()
	} else {
		m.streamEvents = nil
	}
	if source, ok := commander.(settingsSource); ok {
		m.settingsRegistry = source.Settings()
	}
	// Every model name on screen was answered by the commander that just left,
	// so none of it speaks for the one that arrived.
	clear(m.mediaModelCatalogs)
	clear(m.mediaCatalogRequested)
	clear(m.mediaCatalogLoading)
	clear(m.optimisticModels)
}

// splitStore is the optional Commander capability of remembering the divider
// position across launches. A Commander without it still resizes live; the
// position just resets next launch.
type splitStore interface {
	SplitPct() int
	SaveSplitPct(pct int)
}

// The divider clamps so neither pane can be dragged into uselessness. The band
// lives in the settings registry so the drag, the [ ] nudge, and the settings
// row cannot drift apart.
const (
	defaultSplitPct = config.DefaultSplitPct
	minSplitPct     = config.MinSplitPct
	maxSplitPct     = config.MaxSplitPct
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
		// The renderer defaults to 60 wakeups a second forever. Nothing here
		// moves faster than the 120ms animation cadence, and half the rate
		// leaves the timer coalescing that keeps a laptop cool.
		tea.WithFPS(30),
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
		commands := []tea.Cmd{m.nextPollTick(), m.scheduleAnimation()}
		if m.streamRearm {
			// The window changed what it is. Whatever stream the previous
			// commander offered has been replaced, so the listener is started
			// over the new one — once, here, and never again until the role
			// moves again.
			m.streamRearm = false
			commands = append(commands, waitForStream(m.streamEvents))
		}
		return m, tea.Batch(commands...)

	case StreamEvent:
		m.lastActionAt = m.standingTime()
		m.applyStreamEvent(message)
		return m, tea.Batch(waitForStream(m.streamEvents), m.scheduleAnimation())

	case streamBatchMsg:
		m.lastActionAt = m.standingTime()
		for _, event := range message.events {
			m.applyStreamEvent(event)
		}
		return m, tea.Batch(waitForStream(m.streamEvents), m.scheduleAnimation())

	case streamClosedMsg:
		m.streamEvents = nil
		return m, nil

	case animationTickMsg:
		m.animationPending = false
		streaming := m.streamMode != streamNone
		if streaming {
			m.advanceStream()
		}
		m.shimmerFrame++
		m.sampleVoiceLevel()
		animating := m.graphAnimating || m.streamAnimating() || m.shimmerAnimating() ||
			m.voiceAnimating() || m.awaitingAnimating()
		if animating {
			m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
		}
		// One refresh, not two: no frame is drawn between them, so the thread
		// was rendered twice per tick for one visible result.
		if streaming || animating {
			m.refreshChat()
			if streaming && m.autoScroll {
				m.chat.GotoBottom()
			}
		}
		if !animating {
			return m, nil
		}
		// Only the panes that can reach the screen are rebuilt. The rail's
		// animation bookkeeping still runs with the tree off screen: that is
		// what keeps the collapsed rail's spinner turning.
		if m.graphContentVisible() {
			m.refreshGraph()
		} else {
			m.noteGraphAnimation()
		}
		if m.selfVisible() {
			m.refreshSelf()
		}
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
		m.landPostedMessage(message.message)
		m.noteAwaitingReply(message.message)
		m.err = nil
		return m, m.scheduleAnimation()

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

	case serviceCommandResultMsg:
		if message.err != nil {
			m.err = fmt.Errorf("%s service: %w", message.action, message.err)
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

	case historyResultMsg:
		m.applyHistoryResult(message)
		return m, nil

	case tea.KeyMsg:
		m.noteKeypress()
		m.noteActivity()
		if command, handled := m.updateKey(message); handled {
			if command == nil {
				return m, m.scheduleAnimation()
			}
			return m, command
		}

	case tea.MouseMsg:
		m.noteActivity()
		if command, handled := m.updateMouse(message); handled {
			return m, tea.Batch(command, m.scheduleAnimation())
		}
	}

	if m.inputFocused {
		before := m.input.Value()
		// Typing can only move two heights: the draft's own wrapped rows and the
		// palette below it. Everything else the relayout recomputes — the whole
		// thread, the whole rail, the whole employee file — is identical to what
		// is already on screen, so a character costs a relayout only when the
		// frame it lives in actually changed shape.
		inputHeight := m.inputSurfaceHeight()
		paletteHeight := m.layoutPaletteHeight()
		var command tea.Cmd
		m.input, command = m.input.Update(message)
		if m.input.Value() != before && m.nodeViewID == "" {
			m.captureImageAttachments()
			m.paletteSelected = 0
			m.paletteDismissed = false
			m.syncPalette()
			if m.inputSurfaceHeight() != inputHeight || m.layoutPaletteHeight() != paletteHeight {
				m.setSize(m.width, m.height)
			}
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
	if m.focus == focusSelf {
		m.self, command = m.self.Update(message)
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
	if m.nodeViewID == "" && m.inputFocused && (key == "backspace" || key == "ctrl+h") &&
		m.input.Value() == "" && len(m.attachments) > 0 {
		m.removeAttachment(len(m.attachments) - 1)
		return nil, true
	}
	// The settings sheet is modal above every chord but quit: while an inline
	// editor is open, ctrl+v is a paste the terminal delivers as runes, not a
	// microphone, and a letter is a letter.
	if m.palette == paletteSettings {
		return m.updateSettingsKey(message)
	}
	switch key {
	case keyBindings.thread:
		return m.selectPlace(placeThread), true
	case keyBindings.board:
		return m.selectPlace(placeBoard), true
	case keyBindings.self:
		return m.selectPlace(placeSelf), true
	}
	// A Self drill-in is a filter with a list under it. While one is open the
	// letters belong to that filter rather than to the global chords, the same
	// way an open settings editor claims them — otherwise a list that has been
	// growing for six months could only be reached by scrolling. The root list
	// takes no query, so it keeps falling through to the ordinary ladder.
	if m.palette == paletteNone && m.selfVisible() && m.focus == focusSelf &&
		m.selfRoute != selfRouteRoot && key != "esc" {
		if command, handled := m.updateSelfKey(message); handled {
			return command, true
		}
	}
	// A charter's cadence editor is a text field like any other: while it is
	// open the letters are the cadence being written.
	if m.charterEditing {
		if command, handled := m.updateCharterCadenceKey(message); handled {
			return command, true
		}
	}
	// The one gate that keeps every single-key action out of a sentence being
	// typed (the law and its table live in keys.go). Returning unhandled hands
	// the key to the focused field unchanged, which is exactly what a character
	// is for.
	if !m.commandKey(key) {
		return nil, false
	}
	// Every option-chord has a control synonym: on macOS, Option only reaches
	// the program as alt+<key> when the terminal is configured to send it as
	// Meta (Terminal.app "Use Option as Meta key", iTerm2 "Left Option: Esc+");
	// out of the box it types a glyph (∫, √, ©) and the binding silently never
	// fires. Ctrl arrives everywhere.
	if key == keyBindings.graph || key == "ctrl+t" {
		if m.nodeViewID != "" {
			m.closeNodeView()
		}
		m.toggleGraph()
		return nil, true
	}
	if key == keyBindings.voice || key == "ctrl+v" {
		return m.toggleVoice(), true
	}
	if key == keyBindings.boost || key == "ctrl+b" {
		m.toggleBoost()
		return nil, true
	}
	if m.palette == paletteHelp {
		if key == "esc" {
			m.closeHelp()
			return nil, true
		}
		if command, handled := m.updatePaletteKey(key); handled {
			return command, true
		}
		// Help is modal reading space. Unrecognized keys must not edit or
		// operate the surface beneath it.
		return nil, true
	}
	// Help is global only while the draft is empty. This must precede the node
	// activity branch so an empty steer input gets the same help door; once any
	// draft exists the rune falls through to the text input unchanged.
	if key == "?" && m.input.Value() == "" {
		m.openHelp()
		return nil, true
	}
	// The settings door: the option chord anywhere, and the bare comma only
	// outside the input, where a letter is a command rather than a character.
	if key == keyBindings.settings || (key == "," && m.nodeViewID == "") {
		return m.openSettings(), true
	}
	if m.paletteOpen() {
		if command, handled := m.updatePaletteKey(key); handled {
			return command, true
		}
	}
	// A visible option question owns bare answer keys across focus zones. The
	// modal palettes stay above this layer, and any existing draft keeps the
	// digit on the ordinary text-entry path.
	if m.palette == paletteNone && m.input.Value() == "" {
		if card := m.questionCardWithOptions(); card != nil {
			switch {
			case len(key) == 1 && key[0] >= '1' && key[0] <= '9':
				if command, ok := m.submitQuestionOptionNumber(card.ID, int(key[0]-'0')); ok {
					return command, true
				}
			case key == "enter" && card.QuestionKind == questionConfirm:
				if index, ok := markedDefaultQuestionOption(*card); ok {
					m.questionSelection[card.ID] = index
					return m.submitQuestionOption(card.ID, index), true
				}
			}
		}
	}
	if m.nodeViewID != "" {
		switch {
		case key == "esc" && m.voiceState != voiceIdle:
			// Voice owns esc wherever it is recording. The node view used to
			// close instead, which made help's "esc discards it" false in the
			// one surface where a dictated steer is most likely.
			return m.cancelVoice(), true
		case key == "esc":
			m.closeNodeView()
			return nil, true
		case key == "tab":
			m.toggleNodeSteerFocus()
			return nil, true
		case key == "c":
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
		case m.palette == paletteMemory:
			m.closePalette()
		case m.paletteOpen():
			m.palette = paletteNone
			m.paletteDismissed = true
			m.setSize(m.width, m.height)
		case m.selfVisible() && m.selfBack():
		case m.selfVisible():
			return m.selectPlace(placeThread), true
		case m.graphVisible() && m.focus == focusGraph && m.closeScopedGraph():
		case m.graphVisible() && m.focus == focusGraph && m.closeCharterCard():
		case m.graphVisible() && m.focus == focusGraph && m.closeServiceCard():
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
		case m.graphVisible():
			return m.selectPlace(placeThread), true
		default:
			return tea.Quit, true
		}
		return nil, true
	}
	if m.notebookOpen && len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		m.activateNotebookNumber(int(key[0] - '0'))
		return nil, true
	}
	if m.focus == focusHeader {
		switch key {
		case "left", "up", "k":
			doors := m.headerDoorCount()
			m.headerFocusIndex = (m.headerFocusIndex + doors - 1) % doors
			return nil, true
		case "right", "down", "j":
			m.headerFocusIndex = (m.headerFocusIndex + 1) % m.headerDoorCount()
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
	if m.focus == focusSelf {
		if command, handled := m.updateSelfKey(message); handled {
			return command, true
		}
	}
	// Numbered question options are a layer over the normal live input. An
	// empty input enables shortcuts; once text exists, every key and enter
	// continue through the ordinary free-text path. The vim letters stay out
	// of it: while the input has focus a letter is the first character of an
	// answer, and "kimi 3" must arrive whole.
	if m.inputFocused && m.input.Value() == "" {
		if card := m.questionCardWithOptions(); card != nil {
			switch {
			case key == "up":
				m.moveQuestionSelection(card.ID, -1)
				return nil, true
			case key == "down":
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
	if m.focus == focusChat && m.historyVisible && len(key) == 1 && key[0] >= '1' && key[0] <= '8' {
		index := int(key[0] - '1')
		if index < len(m.historyEntries) {
			return nil, m.activateHistory(index)
		}
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
		if command, ok := m.activateChatFocus(); ok {
			return command, true
		}
		if card := m.cardByID(m.selectedCardID); card != nil && card.State == cardSettled {
			return m.advanceCard(card.ID, focusChat), true
		}
		return nil, true
	}
	if key == "v" {
		m.receiptsExpanded = !m.receiptsExpanded
		m.refreshChat()
		return nil, true
	}
	// Getting work out of here: the answer, or the file it produced.
	if key == "y" {
		return m.copyAnswer(), true
	}
	if key == "Y" {
		return m.copyDeliverablePath(), true
	}
	if key == "tab" {
		return m.toggleFocus(), true
	}
	if key == "[" || key == "]" {
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
	if m.focus == focusGraph && m.serviceCardID != "" &&
		(key == "up" || key == "k" || key == "down" || key == "j") {
		delta := -1
		if key == "down" || key == "j" {
			delta = 1
		}
		m.moveServiceSelection(delta)
		return nil, true
	}
	if key == "enter" && m.focus == focusGraph && m.serviceCardID != "" {
		return m.activateServiceSelection(), true
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
	selfReceiptSeq := m.selfReceiptSeq
	selfReceiptSince := m.selfReceiptAt
	if selfReceiptSince.IsZero() {
		now := m.standingTime()
		selfReceiptSince = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	}
	readSelf := m.selfVisible()
	selfNow := m.standingTime()
	journal, _ := backend.(journalReader)
	residencySource := m.residencySource
	force := m.pollForce || !m.journalPrimed
	knownSeq := m.journalSeq
	return func() tea.Msg {
		// Asked before the quiet short-circuit, because the one thing this
		// answers — is the process that answers me still there — is exactly the
		// thing that can change while the journal does not move.
		var residency Residency
		var residencyRead bool
		var adopt Commander
		if residencySource != nil {
			residency, adopt = residencySource.Residency()
			residencyRead = true
		}
		var journalSeq int64
		var journalRead bool
		if journal != nil {
			seq, err := journal.LatestEventSeq()
			if err == nil {
				journalSeq, journalRead = seq, true
				if !force && seq == knownSeq {
					// The node pane's trace is tailed from the executor's file,
					// not the journal, so it is read even on a quiet cycle. Its
					// SQL — the node row and its messages — is journal-backed
					// like everything else, so the watermark still speaks for it
					// and the quiet path stays quiet with a node view open.
					quiet := pollResultMsg{
						quiet: true, journalSeq: seq, journalRead: true, sessionID: sessionID,
						residency: residency, residencyRead: residencyRead, adopt: adopt,
					}
					if nodeID != "" && commander != nil {
						quiet.nodeID = nodeID
						quiet.nodeTrace = commander.NodeTrace(nodeID, nodeTraceMaxBytes)
					}
					return quiet
				}
			}
		}
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
		var selfSpendToday float64
		var selfReceipts []store.SelfReceipt
		var selfLearning string
		var selfSpendErr, selfReceiptsErr error
		if reader, ok := backend.(selfActivityReader); ok {
			selfSpendToday, selfSpendErr = reader.SelfSpendToday()
			selfReceipts, selfReceiptsErr = reader.SelfReceipts(selfReceiptSince)
			if selfReceiptsErr == nil && len(selfReceipts) > 0 {
				newest := selfReceipts[len(selfReceipts)-1]
				if newest.Seq > selfReceiptSeq && selfReceiptHasLearning(newest) &&
					selfReceiptIsPractice(newest, cardSnapshot, snapshot) {
					selfLearning, selfReceiptsErr = selfReceiptLearningClause(reader, newest)
				}
			}
		}
		result := pollResultMsg{
			journalSeq:        journalSeq,
			journalRead:       journalRead,
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
			selfSpendToday:    selfSpendToday,
			selfReceipts:      selfReceipts,
			selfLearning:      selfLearning,
			selfSpendErr:      selfSpendErr,
			selfReceiptsErr:   selfReceiptsErr,
			residency:         residency,
			residencyRead:     residencyRead,
			adopt:             adopt,
		}
		// Charters are read every cycle because the self place-dot must be
		// able to light while the user is somewhere else. The rest of the
		// employee file is read only while the file is open.
		if reader, ok := backend.(selfCharterLister); ok {
			result.chartersRead = true
			result.selfCharters, result.selfChartersErr = reader.Charters()
		}
		if reader, ok := backend.(selfDataReader); ok && readSelf {
			result.selfDataRead = true
			// Practice folds by the week, so the receipt read reaches back one
			// fold rather than to midnight: the day's totals are a projection
			// of the same rows.
			result.selfReceipts, result.selfReceiptsErr = reader.SelfReceipts(selfNow.Add(-selfReceiptReach))
			result.selfSpend, result.selfSpendErr = reader.SelfSpendToday()
			result.selfCompetence, result.selfCompetenceErr = reader.CompetenceMap(
				store.CompetenceOptions{Now: selfNow},
			)
			result.selfFacts, result.selfFactsErr = reader.RecentFacts(selfBeliefScan)
			result.selfSkills, result.selfSkillsErr = reader.SkillFacts("", selfSkillScan)
		}
		if shelf, ok := commander.(craftShelfReader); ok && readSelf {
			result.selfCraftsRead = true
			result.selfCrafts, result.selfCraftsErr = shelf.Crafts()
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

func selfReceiptHasLearning(receipt store.SelfReceipt) bool {
	return len(receipt.FactIDs) > 0 || len(receipt.SkillIDs) > 0 ||
		(receipt.SurpriseDelta != nil && *receipt.SurpriseDelta > 0)
}

func selfReceiptIsPractice(receipt store.SelfReceipt, snapshots ...store.Snapshot) bool {
	for _, snapshot := range snapshots {
		for _, node := range snapshot.Nodes {
			if node.ID == receipt.NodeID {
				return node.Group == store.PracticeGroup
			}
		}
	}
	return false
}

func selfReceiptLearningClause(reader selfActivityReader, receipt store.SelfReceipt) (string, error) {
	ids := make([]int64, 0, len(receipt.FactIDs)+len(receipt.SkillIDs))
	ids = append(ids, receipt.FactIDs...)
	ids = append(ids, receipt.SkillIDs...)
	for _, seq := range ids {
		fact, found, err := reader.FactBySeq(seq)
		if err != nil {
			return "", err
		}
		if found {
			if clause := oneSentence(firstLine(fact.Body)); clause != "" {
				return clause, nil
			}
		}
	}

	scope := strings.TrimSpace(receipt.Scope)
	if scope == "" {
		scope = strings.TrimSpace(receipt.Origin)
	}
	if receipt.SurpriseDelta != nil && *receipt.SurpriseDelta > 0 {
		clause := fmt.Sprintf("surprise fell %.0f%%", 100**receipt.SurpriseDelta)
		if scope != "" {
			clause += " in " + scope
		}
		return clause, nil
	}
	kind := "a fact"
	if len(receipt.FactIDs) == 0 && len(receipt.SkillIDs) > 0 {
		kind = "a skill"
	}
	if scope == "" {
		return "captured " + kind, nil
	}
	return "captured " + kind + " in " + scope, nil
}

// pollCadence is hot whenever there is a reason to expect a change soon and
// idle only after a stretch in which nothing did. Every snap-back condition is
// a fact already on the model, so the cadence never needs its own bookkeeping.
func (m *Model) pollCadence() time.Duration {
	if !m.journalPrimed || m.pollForce || m.streamMode != streamNone || m.liveWorkCount() > 0 {
		return pollInterval
	}
	now := m.standingTime()
	if now.Sub(m.lastActionAt) < pollActiveAfter || now.Sub(m.lastChangeAt) < pollQuietAfter {
		return pollInterval
	}
	return pollIdleInterval
}

func (m *Model) nextPollTick() tea.Cmd {
	return tea.Tick(m.pollCadence(), func(at time.Time) tea.Msg {
		return pollTickMsg(at)
	})
}

// noteActivity snaps the cadence back to hot and buys exactly one full read.
// A key or a click can open a place whose data the journal watermark cannot
// speak for — the employee file, a node view — so the next poll must not be
// allowed to answer it with "nothing changed".
func (m *Model) noteActivity() {
	m.pollForce = true
	m.lastActionAt = m.standingTime()
}

func nextAnimationTick() tea.Cmd {
	return tea.Tick(animationInterval, func(at time.Time) tea.Msg {
		return animationTickMsg(at)
	})
}

func (m *Model) scheduleAnimation() tea.Cmd {
	if m.animationPending || (!m.graphAnimating && !m.streamAnimating() &&
		!m.shimmerAnimating() && !m.voiceAnimating() && !m.awaitingAnimating()) {
		return nil
	}
	m.animationPending = true
	return nextAnimationTick()
}

// applyResidency records what the surface said about which process runs the
// brain, and adopts a replacement commander when the role has moved. Both
// paths through the poll call it, because a quiet journal is not evidence that
// the resident is still alive.
//
// A commander change raises streamRearm, because a window that has just
// promoted has a head to listen to for the first time and nothing else would
// ever go looking for it.
func (m *Model) applyResidency(result pollResultMsg) {
	if !result.residencyRead {
		return
	}
	m.residency = result.residency
	if result.adopt == nil {
		return
	}
	m.adoptCommander(result.adopt)
	m.streamRearm = true
	m.threadGen++
	m.invalidateRailCaches()
}

// applyQuietPoll is the whole no-op path: the journal did not move, so every
// cached pane is still correct and nothing is rebuilt. The one exception is
// the clock — relative timestamps live inside cached content — and that is
// answered by re-rendering from state already in hand, never by reading again.
func (m *Model) applyQuietPoll(result pollResultMsg) {
	m.journalSeq = result.journalSeq
	m.applyResidency(result)
	// The one read a quiet cycle still makes: the open node's trace is a file
	// the executor appends to outside the journal, so the watermark cannot
	// speak for it. It re-renders only when the text actually moved.
	if result.nodeID != "" && result.nodeID == m.nodeViewID && result.nodeTrace != m.nodeTraceText {
		m.nodeTraceText = result.nodeTrace
		m.refreshNodeView(false)
	}
	now := m.standingTime()
	if now.Sub(m.lastRepaintAt) < quietRepaintInterval {
		return
	}
	m.lastRepaintAt = now
	m.invalidateRailCaches()
	// The minute repaint is the clock's own frame: it re-renders everything
	// from state already in hand. Retiring the rendered blocks with it keeps
	// the cache one thread wide, instead of one entry per timestamp a long
	// idle stretch walks through.
	m.threadGen++
	m.rebuildCards()
	m.setSize(m.width, m.height)
}

func (m *Model) applyPoll(result pollResultMsg) {
	if result.quiet {
		m.applyQuietPoll(result)
		return
	}
	m.pollForce = false
	m.lastRepaintAt = m.standingTime()
	m.applyResidency(result)
	m.invalidateRailCaches()
	// A moved journal is the one thing that can change a settled message's
	// rendering from outside the message itself: the node it names, the files
	// it links, the turn that answers its question. So it retires the thread's
	// rendered blocks, and nothing else has to.
	m.threadGen++
	if result.journalRead {
		if result.journalSeq != m.journalSeq || !m.journalPrimed {
			m.lastChangeAt = m.standingTime()
		}
		m.journalSeq = result.journalSeq
		m.journalPrimed = true
	} else {
		// A backend with no watermark can never be proven quiet, so it keeps
		// the original cadence and the original unconditional read.
		m.lastChangeAt = m.standingTime()
	}
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
	if result.selfSpendErr == nil {
		m.selfSpendToday = result.selfSpendToday
	}
	if result.selfReceiptsErr == nil {
		// A learned line lives for exactly the poll on which its receipt first
		// appears. Re-observing the same newest receipt clears it back to the
		// default silence.
		m.selfLearning = ""
		if len(result.selfReceipts) > 0 {
			newest := result.selfReceipts[len(result.selfReceipts)-1]
			if newest.Seq > m.selfReceiptSeq && result.selfLearning != "" &&
				selfReceiptIsPractice(newest, result.cardSnapshot, result.snapshot) {
				m.selfLearning = result.selfLearning
			}
			if newest.Seq > m.selfReceiptSeq {
				m.selfReceiptSeq = newest.Seq
			}
			if newest.Time.After(m.selfReceiptAt) {
				m.selfReceiptAt = newest.Time
			}
		}
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
	if result.chartersRead && result.selfChartersErr == nil {
		m.selfCharters = append(m.selfCharters[:0], result.selfCharters...)
	}
	if result.selfDataRead {
		if result.selfReceiptsErr == nil && result.selfReceipts != nil {
			m.selfReceipts = append(m.selfReceipts[:0], result.selfReceipts...)
		}
		if result.selfSpendErr == nil {
			m.selfSpend = result.selfSpend
		}
		if result.selfCompetenceErr == nil {
			m.selfCompetence = result.selfCompetence
		}
		if result.selfFactsErr == nil && result.selfFacts != nil {
			m.selfFacts = append(m.selfFacts[:0], result.selfFacts...)
		}
		if result.selfSkillsErr == nil && result.selfSkills != nil {
			m.selfSkills = append(m.selfSkills[:0], result.selfSkills...)
		}
	}
	if result.selfCraftsRead && result.selfCraftsErr == nil {
		m.selfCrafts = append(m.selfCrafts[:0], result.selfCrafts...)
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
			if message.Seq > m.lastSeq {
				m.lastSeq = message.Seq
			}
			// A turn already landed at post time is not news twice.
			if m.hasThreadMessage(message.Seq) {
				continue
			}
			// A pure progress state line (no Latest content) is replaceable,
			// not history: the newest one for a node supersedes its
			// predecessor in place, so a compile narrates in one updating
			// line instead of a barrage. Title-bearing posts are content and
			// always keep their place.
			if message.Progress != nil && message.Progress.Latest == "" {
				if index := m.progressLineIndex(message.NodeID); index >= 0 {
					m.messages[index] = message
					continue
				}
			}
			m.insertThreadMessage(message)
			accepted = append(accepted, message)
		}
	}

	m.rebuildCards()
	added := 0
	questionArrived := false
	for _, message := range accepted {
		m.noteAwaitingAnswered(message)
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
	if result.selfSpendErr != nil {
		problems = append(problems, fmt.Errorf("read self spend: %w", result.selfSpendErr))
	}
	if result.selfReceiptsErr != nil {
		problems = append(problems, fmt.Errorf("read self receipts: %w", result.selfReceiptsErr))
	}
	if result.chartersRead && result.selfChartersErr != nil {
		problems = append(problems, fmt.Errorf("read self standing: %w", result.selfChartersErr))
	}
	if result.selfDataRead {
		if result.selfCompetenceErr != nil {
			problems = append(problems, fmt.Errorf("read competence: %w", result.selfCompetenceErr))
		}
		if result.selfFactsErr != nil {
			problems = append(problems, fmt.Errorf("read beliefs: %w", result.selfFactsErr))
		}
		if result.selfSkillsErr != nil {
			problems = append(problems, fmt.Errorf("read skills: %w", result.selfSkillsErr))
		}
	}
	if result.selfCraftsRead && result.selfCraftsErr != nil {
		problems = append(problems, fmt.Errorf("read crafts: %w", result.selfCraftsErr))
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

// progressLineIndex finds the thread's current progress line for a node, so a
// fresher one can take its place rather than stack beneath it.
func (m *Model) progressLineIndex(nodeID string) int {
	for index := len(m.messages) - 1; index >= 0; index-- {
		if m.messages[index].Progress != nil && m.messages[index].Progress.Latest == "" &&
			m.messages[index].NodeID == nodeID {
			return index
		}
	}
	return -1
}

func (m *Model) submit() tea.Cmd {
	body := strings.TrimSpace(m.input.Value())
	if body == "" && len(m.attachments) == 0 {
		return nil
	}
	// Enter on an exact option number takes the same local selection path as a
	// bare digit. Attachments make the turn richer than a numeric-only answer,
	// so they deliberately retain ordinary message semantics.
	if len(m.attachments) == 0 {
		if number, err := strconv.Atoi(body); err == nil && number > 0 && strconv.Itoa(number) == body {
			if card := m.questionCardWithOptions(); card != nil {
				if command, ok := m.submitQuestionOptionNumber(card.ID, number); ok {
					return command
				}
			}
		}
	}
	if strings.HasPrefix(body, "/") {
		return m.executeSlash(body)
	}
	if m.notebookOpen {
		m.closeNotebook()
	}
	attachments := append([]string(nil), m.attachments...)
	if len(attachments) > 0 {
		kept := make([]string, 0, len(attachments))
		documents, images := 0, 0
		for _, path := range attachments {
			// A screenshot is staged whatever the model in the talk slot can
			// see. Dropping it here was the silence: no copy, no fallback, and
			// nothing said. What can look at it is decided where the work runs.
			switch {
			case isImageExtension(path):
				images++
			case isDocumentAttachment(path):
				documents++
			default:
				continue
			}
			kept = append(kept, m.keepAttachment(path))
		}
		attachments = kept
		if body == "" {
			switch {
			case documents > 0 && images > 0:
				body = "Attachments added."
			case documents > 0:
				body = "Document attached."
			default:
				body = "Image attached."
			}
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

// landPostedMessage puts an accepted turn in the thread the moment the store
// takes it, rather than at the next poll. Answering a question is where the
// wait was loudest: the choice was made, the options stayed up, and nothing on
// screen said whether it had landed. The message carries the sequence the store
// gave it, so the poll behind it recognizes the turn instead of doubling it.
func (m *Model) landPostedMessage(posted store.Message) {
	// Only the newest turn lands early. The watermark deliberately stays where
	// it is: advancing it here would let the poll behind this post skip turns
	// that arrived between the last read and this one.
	if posted.Seq <= m.lastSeq || posted.NodeID != "" || m.hasThreadMessage(posted.Seq) {
		return
	}
	m.insertThreadMessage(posted)
	m.threadGen++
	m.rebuildCards()
	m.setSize(m.width, m.height)
	if m.autoScroll {
		m.chat.GotoBottom()
	}
}

// insertThreadMessage keeps m.messages in sequence order. A turn landed at post
// time can be older than one a poll in flight is about to deliver, and readers
// that walk the thread backwards — which question a reply answered, which
// progress line a node owns — depend on that order.
func (m *Model) insertThreadMessage(message store.Message) {
	at := len(m.messages)
	for at > 0 && message.Seq != 0 && m.messages[at-1].Seq > message.Seq {
		at--
	}
	m.messages = append(m.messages, store.Message{})
	copy(m.messages[at+1:], m.messages[at:])
	m.messages[at] = message
}

func (m *Model) hasThreadMessage(seq int64) bool {
	if seq == 0 {
		return false
	}
	for index := len(m.messages) - 1; index >= 0; index-- {
		switch existing := m.messages[index].Seq; {
		case existing == seq:
			return true
		case existing != 0 && existing < seq:
			return false
		}
	}
	return false
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
	if m.selfVisible() {
		order = []paneFocus{focusInput, focusSelf, focusHeader}
	} else if m.graphVisible() {
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
	if m.focus == focusSelf {
		m.ensureSelfSelectionVisible()
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
		m.headerFocusIndex = max(0, min(2, m.headerFocusIndex))
	}
	if m.inputFocused {
		m.setSize(m.width, m.height)
		return m.input.Focus()
	}
	m.input.Blur()
	m.setSize(m.width, m.height)
	return nil
}

// toggleGraph is the ⟨tasks⟩/alt+g alias for the board place. From the board
// it returns home to the thread; from anywhere else — including Self — it
// opens the board, focus and all, so the arrows work immediately.
func (m *Model) toggleGraph() {
	if m.graphOpen && !m.selfOpen {
		_ = m.selectPlace(placeThread)
		return
	}
	_ = m.selectPlace(placeBoard)
}

// graphVisible reports whether the task rail (or full task pane, when the
// terminal is narrow) is on screen.
func (m *Model) graphVisible() bool { return m.graphOpen && m.nodeViewID == "" }

// activityBarVisible reports whether the active-card dock sits above the
// input. Its quiet fallback still opens the rail for graph-only stores.
func (m *Model) activityBarVisible() bool {
	return !m.selfVisible() && !m.graphVisible() && m.nodeViewID == "" && !m.paletteOpen()
}

func (m *Model) setSize(width, height int) {
	m.invalidateDock()
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
	// The frame takes inputFrameInset columns per side; inside it the prompt
	// and the right-edge voice control share the row with the text.
	m.input.Width = max(1, m.width-4-2*inputFrameInset-m.voiceControlWidth()-pendingReserve)

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
	// Header + blank + the exact static standing, services, and presence budget.
	m.graph.Height = max(1, m.graphHeight-2-m.standingSectionHeight()-m.servicesSectionHeight()-m.residentPresenceHeight())
	m.self.Width = max(1, m.width-2)
	m.self.Height = max(1, mainHeight)
	m.sizeNodeViewports()
	m.refreshChat()
	m.refreshGraph()
	m.refreshSelf()
	if m.autoScroll {
		m.chat.GotoBottom()
	}
}

func (m *Model) refreshGraph() {
	m.refreshGraphContent()
	m.noteGraphAnimation()
}

// graphContentVisible reports whether anything the rail viewport holds can
// reach the screen. When nothing can, the animation tick keeps the bookkeeping
// and skips building a tree no one will read.
func (m *Model) graphContentVisible() bool {
	return m.graphVisible() || m.nodeViewID != "" || m.serviceCardID != "" || m.charterCardID != ""
}

// noteGraphAnimation decides whether anything on screen still moves. It is the
// half of the refresh the collapsed rail still needs: its spinner lives in the
// activity bar, not in the tree.
func (m *Model) noteGraphAnimation() {
	if !m.graphContentVisible() {
		m.graphAnimating = false
	}
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

func (m *Model) refreshGraphContent() {
	offset := m.graph.YOffset
	if m.serviceCardID != "" {
		m.graphRows = nil
		m.standingRows = nil
		m.serviceRows = nil
		m.graphAnimating = false
		m.graph.SetContent(m.renderServiceCardBody(max(1, m.graph.Width)))
	} else if m.charterCardID != "" && m.graphScopeID == "" {
		m.graphRows = nil
		m.standingRows = nil
		m.graphAnimating = false
		m.graph.SetContent(m.renderCharterCardBody(max(1, m.graph.Width)))
	} else {
		m.graph.SetContent(m.renderTree(max(1, m.graph.Width), 0))
	}
	m.graph.SetYOffset(offset)
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
	if m.focus == focusSelf {
		if down {
			m.self.PageDown()
		} else {
			m.self.PageUp()
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
	m.syncHistorySelection(line)
	if line < m.chat.YOffset {
		m.chat.SetYOffset(line)
	} else if line >= m.chat.YOffset+max(1, m.chat.Height) {
		m.chat.SetYOffset(line - max(1, m.chat.Height) + 1)
	}
	m.syncChatScroll()
}

// activateChatFocus is enter-equals-click for the thread zone: it triggers
// whatever a click on the focused interactive line would.
func (m *Model) activateChatFocus() (tea.Cmd, bool) {
	targets := m.chatFocusLines()
	if len(targets) == 0 {
		return nil, false
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
	if m.palette == paletteHelp && m.helpBounds.contains(event.X, event.Y) {
		delta := -3
		if down {
			delta = 3
		}
		m.scrollHelp(delta)
		return nil, true
	}
	if m.palette == paletteSettings && m.settingsBounds.contains(event.X, event.Y) {
		delta := -3
		if down {
			delta = 3
		}
		m.scrollSettings(delta)
		return nil, true
	}
	if m.nodeViewID != "" {
		return nil, m.scrollNodeAt(event.X, event.Y, down)
	}
	if m.selfBounds.contains(event.X, event.Y) {
		if down {
			m.self.SetYOffset(m.self.YOffset + 3)
		} else {
			m.self.SetYOffset(m.self.YOffset - 3)
		}
		return nil, true
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
