package tui3

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// frameInterval is the repaint ceiling: at most one frame is BUILT per 33ms,
// no matter how many deltas arrive inside it. A provider that streams token by
// token would otherwise re-wrap the live paragraph a hundred times a second to
// show the reader thirty of them, and the thirty it showed would be the same
// thirty.
//
// It is also the only clock on this surface. The spinner and the ellipsis step
// on multiples of it (see spinnerStep, pulseStep) rather than on schedules of
// their own: two animation clocks means two wakeups per second per animation
// and rows that drift out of phase with each other.
const frameInterval = 33 * time.Millisecond

// markdownThrottle is how often a streaming reply's settled prefix is promoted
// from plain wrapped text to rendered markdown. See [app.assistantRows].
const markdownThrottle = 1500 * time.Millisecond

// usageEvery is how many frames pass between asks for the session's running
// cost. The agent answers under a lock, and a lock taken thirty times a second
// to move a figure that changes once a turn is a lock taken for nothing.
const usageEvery = 10

// quietBeforeEllipsis is how long the stream has to be silent before the
// ellipsis appears under a reply that is already streaming. Text arriving in
// chunks a few hundred milliseconds apart is a working model, not a stalled
// one, and a dot that blinks in between every chunk is noise.
const quietBeforeEllipsis = 700 * time.Millisecond

// runState is the word in the status line.
type runState int

const (
	stateIdle runState = iota
	stateWorking
	stateInterrupted
)

func (s runState) String() string {
	switch s {
	case stateWorking:
		return "working"
	case stateInterrupted:
		return "interrupted"
	default:
		return "idle"
	}
}

// entryKind is what one block of the conversation is.
type entryKind int

const (
	entryUser entryKind = iota
	entryAssistant
	entryTool
	entryDivider
	// entryCompact is ONE compaction pass, from the moment it starts to the
	// moment it stops — a row with a clock rather than a mark left behind.
	//
	// It is a kind of its own rather than a divider written twice because a
	// compaction is the one thing this surface waits on that is neither the
	// model speaking nor a tool running: a paid summarizer call that can hold a
	// turn for ten seconds while the screen says nothing. It opens as a spinning
	// line ("compacting ~84k tokens · 6s") and SETTLES IN PLACE into the rule the
	// divider always drew, so the finished conversation reads exactly as it did
	// before this wave and the running one stops being a silence.
	entryCompact
	entryNote
	// entryThinking is one turn's reasoning (thinking.go): streamed while it
	// arrives, collapsed to a single row the moment the turn says anything else.
	entryThinking
	// entryTask is ONE TASK PROPOSAL — the decision moment, drawn where it
	// happened (task.go). It keeps its verdict afterwards, the way a consent
	// row does, because "the model asked to go and do this and you said yes" is
	// the only record of where a running node came from.
	entryTask
	// entryDone is ONE TASK THAT CAME HOME (taskdone.go): the card that lands
	// when a node reaches its final state, minutes after the turn that proposed
	// it and usually with no turn open at all.
	//
	// It is a kind of its own rather than the note it used to be for the reason
	// entryCompact is not a divider: a note is the surface muttering about its
	// own housekeeping, and this is the only statement of an outcome a person
	// delegated ten minutes of work to get.
	entryDone
)

// toolState is where one call is in its life, and it is the whole of what the
// left of a tool line says (toolview.go draws it):
//
//	◌  toolQueued    the model asked for it; NOTHING has started
//	?  toolConsent   it is blocked on a person — the question hue, and the row
//	                 with it
//	⠋  toolRunning   it is executing: the braille spinner, and only here
//	   toolOK        it finished, quietly, with its elapsed time
//	✗  toolFailed    it failed, loudly, and its detail is already open
//
// The first two states are the fix for one defect: a mutating call sat spinning
// while the RESPONSE streamed (nothing had started), and a call waiting for an
// answer spun identically to one doing work. A spinner is a claim about
// motion, so it is now spent on motion alone.
//
// The zero value is toolQueued and that is deliberate: an entry built from an
// announcement carries no status field, and the only honest default for a call
// nobody has said anything else about is "asked for".
type toolState int

const (
	toolQueued toolState = iota
	toolConsent
	toolRunning
	toolOK
	toolFailed
)

// live reports whether a call has not resolved yet — queued, waiting on a
// person, or executing. Every "find the row this event is about" walk asks
// this rather than comparing against toolRunning, which was the whole test back
// when running was the only unresolved state there was.
func (s toolState) live() bool { return s == toolQueued || s == toolConsent || s == toolRunning }

// entry is one block of the conversation, and its rendered rows.
//
// Tool activity is an entry rather than a decoration on the assistant's block
// because it lands in place, between two paragraphs of a reply, and reads in
// the order it happened.
//
// The cached rows are the second half of the snappiness contract: an entry
// renders when its own content or the width changes, and a frame joins what is
// already there. Nothing in here holds a blank row — spacing is [app.layout]'s
// and only [app.layout]'s.
type entry struct {
	kind entryKind
	text string
	turn int

	// Tool fields.
	tool   string
	status toolState
	detail toolDetail
	open   bool // this call's expansion is showing inline
	// full lifts the expansion's per-tool cap: it is set by a click on the
	// "… N more lines" foot, which is the person saying they want the rest.
	full bool
	// decision is what the person answered when this call was asked about —
	// "allowed" or "denied", dim, beside the row's stat (consent.go). It is
	// empty for every call the policy did not stop.
	decision string

	// began and ended are the block's clock, and the two entry kinds that have
	// one read it the same way — the moment the thing started and the moment it
	// stopped:
	//
	//	entryThinking  the first and last reasoning delta (thinking.go)
	//	entryTool      EXECUTION started (EventToolBegin) and ended, which is
	//	               what the line's trailing "1.2s" measures. A call's
	//	               announcement is deliberately not in it: the time a
	//	               response spent streaming is not time the tool ran.
	//
	// settled collapses a thinking block and open expands it again.
	began, ended time.Time

	// Assistant fields. settled means the block is finished and renders
	// through renderMarkdown; mdCut is how many bytes of a still-streaming
	// block have already been promoted to markdown.
	settled bool
	mdCut   int

	// card is the proposal this entry draws, for kind entryTask and for nothing
	// else (task.go). It is a POINTER because the answer lane holds the same
	// card: a row and the verdict on it must not be able to disagree.
	card *taskCard
	// done is the card a landed node writes, for kind entryDone and for nothing
	// else (taskdone.go). Like [entry.card] it is a POINTER, because the card
	// carries the one piece of state a person can change about it — whether its
	// full context is showing — and the row and that state must never disagree.
	done *taskDone

	// The row cache. built distinguishes "no rows yet" from "renders to no
	// rows", which an empty slice cannot.
	rows  []string
	width int
	built bool
	stale bool
}

// The messages the surface moves on. Every stream message carries the
// generation of the stream it came from: a turn that was interrupted while its
// channel still had events in flight must not paint over the turn that
// replaced it.
type (
	submittedMsg struct {
		ch  <-chan session.Event
		err error
	}
	streamEventMsg struct {
		gen int
		ev  session.Event
	}
	streamClosedMsg struct{ gen int }
	compactedMsg    struct{ err error }
	// frameMsg is the paint clock: it promotes whatever streamed since the
	// last one into a frame, and steps the animations.
	frameMsg struct{}
	// gitMsg is what the workspace's repository answered (see [gitHead]). It is
	// a message rather than a call because `git status` on a large tree is tens
	// of milliseconds and the model loop is not a place to wait.
	gitMsg struct {
		branch string
		dirty  bool
		ok     bool
	}
	// taskEventMsg is one event off the STANDING task subscription (task.go),
	// which is a second stream and not the turn's: a node's "done" lands
	// minutes after the turn that proposed it, when there is no stream left to
	// land on. Its generation is the same device the turn stream's is — a lane
	// belonging to an agent that has been replaced must not paint into the one
	// that replaced it.
	taskEventMsg struct {
		gen int
		ev  session.Event
	}
	taskLaneClosedMsg struct{ gen int }
	// The THIRD lane on this surface (room.go): one node's own events, while a
	// person is standing in its room. It is neither the turn's stream nor the
	// standing task subscription — those carry what the CONVERSATION is doing and
	// what a node's life has come to; this carries what one node is doing right
	// now, and it exists only for as long as somebody is watching.
	roomEventMsg struct {
		gen int
		ev  session.Event
	}
	roomClosedMsg struct{ gen int }
	// The FOURTH lane, and the only one nobody asked for: one watcher per
	// RUNNING node, held for as long as the node runs, so the rail can say what
	// a node is doing and for how long without a person having to walk into its
	// room to find out (task.go's [taskPilot]). Its generation is per-pilot,
	// because pilots come and go with the nodes and the surface holds several.
	taskPilotMsg struct {
		gen int
		id  uint64
		ev  session.Event
	}
	taskPilotClosedMsg struct {
		gen int
		id  uint64
	}
	// hudFadeMsg is a BOUNDED catch-up tick: the two wakeups a settled turn
	// schedules so its fresh numbers can go quiet on time (render.go's fade).
	// There is deliberately no idle ticker behind it — a surface with nothing
	// happening on it wakes twice and then stops.
	hudFadeMsg struct{}
)

type app struct {
	ctx   context.Context
	agent Agent
	fresh func() (Agent, string, error)
	// workspace is the directory this conversation is about, whole; place is
	// its base name, which is what the status line has room for. The whole path
	// is what history is keyed by and what the @ completion walks.
	workspace string
	place     string
	file      string
	resumed   bool

	entries []entry
	// live is the assistant entry currently being streamed into, or -1.
	live int
	// turn counts the person's messages. It groups tool calls into clusters
	// and is what ctrl+o folds and unfolds.
	turn int
	// unfolded holds the turns whose tool cluster is showing every call.
	unfolded map[int]bool
	// sel is the selected tool entry, or -1. ↑/↓ move it; enter opens it.
	sel int
	// hot is what the pointer is over (hover.go). The zero value is nothing.
	hot hoverAt

	state runState
	model string
	// title is the name the session gave itself, shown left of the model. Empty
	// until the session has one (session's title.go names it after the first
	// completed turn); a resumed session opens with the name it already had.
	title  string
	cost   float64
	tokens int
	// ctxWindow is the model's context in tokens as this surface last set it,
	// and ctxTokens what the conversation currently weighs. The pair is the
	// meter in the status line. The window is TRACKED rather than asked for
	// because session exposes no getter — the surface is the one that tells the
	// agent (see [app.switchModel]), so the surface is the one that knows.
	ctxWindow int
	ctxTokens int
	// inputTokens is the session's prompt-token total, and cacheRead/cacheWrite
	// its prompt-cache totals. The first two together are the status line's warm
	// share — the fraction of everything this session has sent that came off a
	// cache — and they are held apart rather than as a ratio because the two
	// provider dialects disagree about whether one contains the other
	// (session.Usage.CachedShare owns that reconciliation).
	//
	// cacheWrite is not drawn. It is kept beside its pair because "is the cache
	// working" is one question with two halves, and a surface holding only the
	// half it currently draws is one that has to go back to the session to
	// answer the obvious follow-up.
	inputTokens int
	cacheRead   int
	cacheWrite  int
	// cacheSaved is what those reads have been WORTH, in dollars, summed over
	// every turn that had a published price pair to compute it from (see
	// [app.cacheNote], which is the one place it grows). It is what turns the
	// warm share from a statistic into a fact about the bill: the percentage is
	// the hit RATE, this is what the rate MEANT.
	//
	// It is deliberately NOT derivable from the totals beside it. The price is a
	// property of the model that was answering at the time, and a session that
	// switched models halfway cannot be re-priced afterwards from one number —
	// so it is accumulated per turn, at the moment the price is known.
	cacheSaved float64

	// outputTokens is what the session has WRITTEN, and it is held apart from
	// [app.tokens] (which is input+output) for one reason: the burn rate. Tokens
	// per second is a claim about generation, and a figure with the prompt in it
	// would climb by twenty thousand the moment a large file was read.
	outputTokens int
	// turnBegan is when the turn now running started, and turnOutStart what
	// [app.outputTokens] read at that moment. The pair is the burn segment's
	// whole arithmetic: what this turn has written, over how long it has been
	// writing. Both are cleared when the turn settles — a rate quoted over a
	// finished turn is a rate nobody is watching.
	turnBegan    time.Time
	turnOutStart int
	// ctxRing is the last [ctxRingSize] TURN-END context readings, oldest first.
	// It is the sparkline's data and the compaction ETA's, and it is sampled at
	// turn end rather than on the frame clock because that is the only moment
	// the figure means the same thing twice: mid-turn it climbs with every tool
	// result and falls back when the batch resolves.
	ctxRing []int
	// ringTurn is the turn the last reading was taken for — see
	// [app.sampleContext] for why a turn can end more than once.
	ringTurn int

	// branch is the git branch the workspace is on and branchDirty whether it
	// has uncommitted work — the right half of the input's legend (render.go).
	// Empty branch means "no answer", which is what a directory that is not a
	// repository, a git that is not installed and a probe that timed out all
	// look like from here.
	branch      string
	branchDirty bool
	// gitProbe is the seam onto that answer, so a test can pin a branch without
	// a repository and the surface can be driven with no git at all. Nil is
	// [gitHead].
	gitProbe func(dir string) (string, bool, bool)
	// home is what "~" abbreviates in the legend's path, read once at boot.
	home string
	// approval is the tool gate's blanket posture — "prompt", "allow", "deny" —
	// as the profile last said. It is on this surface for exactly one reason:
	// "allow" means nothing will ever be asked, and that is the one posture a
	// person must not be able to forget they are in (render.go's YOLO segment).
	approval string
	// mouse is whether the surface reports the mouse at all (config's ui.mouse
	// row): on buys hover and click, off hands every drag back to the
	// terminal, whose native selection is the more fundamental act. Read where
	// approval is read — boot and each turn end.
	mouse bool

	// The HUD's per-segment change clocks (render.go's [app.freshen]). segText
	// is what each segment last read and segAt when it last CHANGED, which is
	// the whole of the age fade: paint follows recency.
	segText [segCount]string
	segAt   [segCount]time.Time

	// hud is the cached answer to the two questions the telemetry asks of the
	// whole conversation — how much background work is alive, and what the
	// session has written — and hudStale says the entries have moved since it
	// was computed. Both walks read every tool call's arguments, which is a JSON
	// parse per row: at thirty frames a second, on a session with four hundred
	// rows, that is the status line costing more than the conversation.
	hud      hudStats
	hudStale bool

	// stream is the channel being pumped and gen its generation. gen is
	// bumped by every Submit so that a late event from an abandoned stream can
	// be recognized and dropped.
	stream <-chan session.Event
	gen    int

	// lastDelta is when text last arrived, and mdAt when the live reply's
	// prefix was last promoted to markdown.
	lastDelta time.Time
	mdAt      time.Time

	// The paint clock. dirty says the row list no longer matches the entries;
	// painting says a frameMsg is already on its way, so a burst of deltas
	// schedules one tick and not one each. paints counts frames and drives
	// every animation on this surface; builds counts layouts and exists so a
	// test can assert the coalescing without sleeping.
	dirty    bool
	painting bool
	paints   int
	builds   int

	// rows is the last laid-out screen list, and rowsWidth the width it was
	// laid out for.
	rows      []row
	rowsWidth int

	width, height int
	offset        int
	stick         bool

	pal   palette
	input editor
	// pick is the model overlay (palette.go). Closed, it costs the frame
	// nothing; open, it owns the keyboard and the bottom of the screen.
	pick picker
	// asks are the approval questions waiting for an answer, oldest first
	// (consent.go). While one is up it owns the keyboard: the draft below is
	// suspended untouched, exactly as the model picker suspends it.
	asks []ask
	// follows are the messages typed with ctrl+q while a turn ran, each holding
	// the stream the turn it starts will speak on (followup.go).
	follows []queued

	// THE TASK SIDE (task.go). task is the proposal that owns the answer lane,
	// or nil; tasks and taskOrder are the rail's nodes, keyed by id and kept in
	// admission order; taskSeen is the (id, state) de-dup, because an in-turn
	// update arrives on both the turn's stream and the standing one; taskLane is
	// that standing subscription and taskGen the generation it belongs to.
	task      *taskCard
	tasks     map[uint64]*taskNode
	taskOrder []uint64
	taskSeen  map[uint64]session.TaskState
	taskLane  <-chan session.Event
	taskGen   int
	// pilots are the watchers on the nodes that are running right now, keyed by
	// id, and pilotGen the counter each one takes its generation from (task.go).
	// Empty is the ordinary state: nothing is running, so nothing is watched.
	pilots   map[uint64]*taskPilot
	pilotGen int
	// room is the node's page, when a person has walked into one (room.go), and
	// roomGen the generation of the lane feeding it. Nil is the ordinary state:
	// the body region is the conversation, and every geometric question about it
	// resolves through the transcript.
	room    *taskRoom
	roomGen int
	// roomPump is the command a freshly opened room's lane needs, PARKED rather
	// than returned.
	//
	// The reason is one call path this file cannot hand a command back through:
	// enter on a selected proposal row runs input.go's [app.enter] into
	// [app.openTool], which returns nothing at all. A door that worked when it
	// was clicked and did nothing when it was pressed would be the worse of the
	// two defects, so every door parks here and the program loop drains it.
	roomPump tea.Cmd
	// think is the reasoning block currently streaming, or -1 (thinking.go).
	think int

	// chips are the pictures attached to the message being written, drawn as a
	// tray above the box (attach.go). sent holds the ones a message in flight
	// took, so a refusal can put them back where the person left them.
	chips []chip
	sent  []chip

	// menu is the command list and comp the @ file completion — the two
	// overlays that open by TYPING rather than by a key (commands.go,
	// files.go). They are not modal: the draft under them keeps the keyboard.
	menu menu
	comp completion

	// history is the recall list, and hist the walk currently in it (recall.go).
	// Nil history is a surface with no ↑, which is what --no-history is.
	history History
	hist    recall

	// draftFile is where the unsent sentence is kept between sessions
	// (draft.go); empty means it is not kept at all.
	draftFile    string
	draftPending bool
	// models is the door's model list, asked for at the moment the picker
	// opens rather than at boot — a lazily warmed catalog may have arrived in
	// between, and it must never be waited for. Nil falls through to the cache
	// and the built-ins (see [app.modelList]).
	models func() []Model

	// sheet is the settings panel (settings.go): the one FULLSCREEN thing this
	// surface draws, and the only overlay that is modal for the pointer as well
	// as for the keyboard. Closed, it costs the frame nothing.
	sheet sheet
	// profileDir is where the panel's writes land, and settings the registry it
	// edits. The registry is built at the first /settings rather than at boot —
	// it is a door onto a file, and a surface that may never be asked about
	// settings should not open one.
	profileDir string
	settings   *config.Settings

	// copy is the frozen viewport a person reads and yanks out of (copymode.go).
	// Closed, it costs the frame nothing.
	copy copyMode
	// tmux says this surface is inside a multiplexer, so a clipboard write has
	// to be wrapped in its passthrough (copymode.go). It is read once, from
	// TERM, because a terminal does not change what it is mid-session.
	tmux bool

	// focused is whether the terminal window has the keyboard, and seenFocus
	// whether it has ever told us (notify.go). The pair is what decides whether
	// a finished turn is worth a notification: a person watching the screen does
	// not need to be told what they are looking at.
	focused   bool
	seenFocus bool

	// linear is the screen-reader tier (Options.Linear): one column, no
	// animation, no hover, ASCII markers. It is read by the rendering branches
	// that draw motion or shape, and by nothing else.
	linear bool

	// clock is where this surface reads the time, and nil is [time.Now].
	//
	// It exists for ONE fact that is now on screen: a running call's age
	// (toolview.go's count-up), which is the first thing this surface draws that
	// is a function of the wall clock rather than of what arrived. A test cannot
	// wait a minute to see "1m 5s", and a render that slept to be tested would
	// be a render tuned to a test.
	clock func() time.Time

	// welcome is the box an empty session opens with (welcome.go). It is the
	// only animation on this surface that is not a spinner, and it runs once.
	welcome welcome
	// recentSessions answers the box's right column, and resume opens one of
	// them. Both are nil on a surface the door did not wire, and then the box
	// says it has no sessions rather than pretending to have lost them.
	recentSessions func() []Session
	resume         func(file string) (Agent, error)
}

func newApp(ctx context.Context, opts Options) *app {
	place := strings.TrimSpace(opts.Workspace)
	if place == "" {
		if cwd, err := os.Getwd(); err == nil {
			place = cwd
		}
	}
	a := &app{
		ctx:            ctx,
		agent:          opts.Agent,
		fresh:          opts.Fresh,
		workspace:      place,
		place:          filepath.Base(place),
		file:           opts.SessionFile,
		resumed:        opts.Resumed,
		models:         opts.Models,
		history:        opts.History,
		draftFile:      opts.DraftFile,
		ctxWindow:      opts.ContextWindow,
		profileDir:     opts.ProfileDir,
		settings:       opts.Settings,
		recentSessions: opts.RecentSessions,
		resume:         opts.Resume,
		live:           -1,
		sel:            -1,
		think:          -1,
		unfolded:       map[int]bool{},
		stick:          true,
		width:          80,
		height:         24,
		pal:            detectPalette(),
		linear:         opts.Linear,
		tmux:           tmuxTerm(os.Getenv),
		// A terminal that has said nothing is assumed to HAVE the keyboard, which
		// is the quiet assumption: the cost of getting it wrong is a notification
		// nobody got, and the cost of the other default is a notification every
		// turn on a screen somebody is watching (notify.go).
		focused: true,
	}
	a.copy.mark = -1
	a.gitProbe = gitHead
	if home, err := os.UserHomeDir(); err == nil {
		a.home = home
	}
	// The gate's posture is read at boot and re-read at every turn end
	// ([app.settle]): a person who opens the settings panel and turns the asking
	// off sees the YOLO segment appear one turn later, which is soon enough for
	// a fact that only ever changes by hand.
	a.approval = readApproval(a.profileDir)
	a.mouse = config.MouseEnabledAt(a.profileDir)
	if a.linear {
		// The linear tier is a palette question as well as an app one: the two
		// paints that mean motion and pointer stop, and the rail drops to the
		// ASCII it already had a spelling for.
		a.pal.linear, a.pal.ascii = true, true
	}
	if a.agent != nil {
		a.model = a.agent.Model()
		// A resumed session is already named, and the name is a fact about the
		// conversation on screen: it belongs in the first frame, not after the
		// next turn (session's title.go re-names nothing).
		a.title = strings.TrimSpace(a.agent.Title())
	}
	a.hudStale = true
	// The conversation that already happened is drawn BEFORE the surface says
	// anything of its own, so the notices below land where a person's eye
	// already is: at the bottom, next to the box.
	a.replay()
	// The box is decided HERE, between the replay and the first thing the
	// surface says of its own: "empty" has to mean "the conversation is empty",
	// and every line below this one is the surface talking (welcome.go).
	a.openWelcome()
	if a.linear {
		// The box still opens; it just opens FINISHED. Its arrival animation is
		// the one piece of motion on this surface that is not a spinner, and
		// linear mode's rule is the same for both.
		a.welcome.step = welcomeFrames
	}
	a.measureContext()
	if notice := strings.TrimSpace(opts.Notice); notice != "" {
		a.note(notice)
	}
	if a.resumed && a.file != "" {
		a.note("resumed " + a.file)
	}
	// The opening line says the two keys the status line has no room for. The
	// other two — /help and ctrl+o — moved to that line's right end this wave
	// and are on screen permanently, so repeating them here would be the surface
	// saying the same thing twice on the first frame of every session.
	a.note("esc or ctrl+c interrupts")
	a.restoreDraft()
	return a
}

var _ tea.Model = (*app)(nil)

// Init starts the paint clock when — and only when — the first frame has
// something to animate. That is the welcome box's arrival and nothing else: an
// idle surface with no box is a surface with no wakeups at all.
func (a *app) Init() tea.Cmd {
	// The repository is asked ONCE here and then only at turn ends. A branch is
	// a fact that changes when a person changes it, and a person who checks out
	// a branch mid-turn is between two turns by the time it matters.
	// THE STANDING TASK SUBSCRIPTION IS OPENED ONCE, HERE (task.go). It is not
	// the turn's stream and it never closes with one: a node proposed in this
	// turn reports minutes later, with no turn open, and the rail is the only
	// thing on screen that knows it is still alive.
	if a.welcome.animating() {
		return tea.Batch(a.wake(), a.probeGit(), a.watchTasks())
	}
	return tea.Batch(a.probeGit(), a.watchTasks())
}

func (a *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// A zero size is a terminal that could not say — a headless boot, a
		// window being born. Keeping the last known size draws something;
		// taking the zero draws nothing.
		if msg.Width > 0 && msg.Height > 0 {
			a.width, a.height = msg.Width, msg.Height
			a.touch()
			a.clampScroll()
		}
		return a, nil

	case tea.KeyPressMsg:
		// THE ROOM READS FIRST, and only ever while one is open (room.go). It has
		// to be read here rather than inside [app.key] because the two keys it
		// takes — esc to leave, enter to steer — belong to input.go, and it
		// restates that file's precedence law rather than jumping it: everything
		// that outranks the draft there outranks the room here.
		if cmd, taken := a.roomKey(msg); taken {
			return a, cmd
		}
		// A key can OPEN a room too — enter, on a selected proposal — down a path
		// that returns no command, so whatever that door parked is drained here.
		return a, tea.Batch(a.key(msg), a.takeRoomPump())

	case tea.FocusMsg:
		// The terminal reports focus (View asks for it in view.go), so the
		// notification has something honest to gate on — see notify.go.
		a.focused, a.seenFocus = true, true
		return a, nil

	case tea.BlurMsg:
		a.focused, a.seenFocus = false, true
		return a, nil

	case tea.PasteMsg:
		// Bracketed paste, whole, in one message — the terminal told us where
		// it started and where it ended, so the newlines inside it are text and
		// not a stack of enters. It goes in as typed; nothing here submits.
		return a, a.paste(msg.Content)

	case filesLoadedMsg:
		a.comp.all, a.comp.loaded, a.comp.loading = msg.paths, true, false
		a.comp.rank()
		a.touch()
		return a, nil

	case draftSaveMsg:
		return a, a.saveDraft()

	case tea.MouseWheelMsg:
		// COPY MODE OWNS THE WHEEL while it is up, because the viewport it froze
		// is the thing the wheel would otherwise move (copymode.go).
		if a.copy.on {
			switch msg.Mouse().Button {
			case tea.MouseWheelUp:
				a.copyScroll(-3)
			case tea.MouseWheelDown:
				a.copyScroll(3)
			}
			return a, nil
		}
		// The settings panel is modal for the pointer too: it is the whole
		// screen, so there is no conversation under it for a wheel to reach.
		if a.sheet.open {
			switch msg.Mouse().Button {
			case tea.MouseWheelUp:
				a.sheet.move(-3)
			case tea.MouseWheelDown:
				a.sheet.move(3)
			}
			a.touch()
			return a, nil
		}
		// The room is the body region while it is up, so the wheel is the room's:
		// a wheel that moved the transcript under it would scroll a list that is
		// not on screen (room.go).
		if a.roomOpen() {
			switch msg.Mouse().Button {
			case tea.MouseWheelUp:
				a.roomScroll(-3)
			case tea.MouseWheelDown:
				a.roomScroll(3)
			}
			return a, nil
		}
		switch msg.Mouse().Button {
		case tea.MouseWheelUp:
			a.scroll(-3)
		case tea.MouseWheelDown:
			a.scroll(3)
		}
		return a, nil

	case tea.MouseClickMsg:
		if a.copy.on {
			// A click in copy mode acts on nothing: the rows under the pointer are
			// a FROZEN snapshot, and expanding a call in it would be expanding a
			// row that is no longer where the conversation says it is.
			return a, nil
		}
		if msg.Mouse().Button == tea.MouseLeft {
			if a.sheet.open {
				a.sheetPress(msg.Mouse().X, msg.Mouse().Y)
				return a, nil
			}
			// A chip is the one thing below the conversation a click can take
			// off, and it is the one thing down there that needs the COLUMN as
			// well as the row (attach.go).
			if a.chipPress(msg.Mouse().X, msg.Mouse().Y) {
				return a, nil
			}
			// THE RAIL IS THE OTHER COLUMN-AWARE TARGET, and it is read before
			// the body for the same reason: the two are drawn side by side, so
			// which one was pressed is a question about x (room.go). A rail row
			// is a door into that node's room.
			if cmd, took := a.railPress(msg.Mouse().X, msg.Mouse().Y); took {
				return a, cmd
			}
			// AND THE PROPOSAL'S CHOICES ROW IS THE THIRD, for the same reason
			// again: three answers share one line, so which was pressed is a
			// question about x (task.go). It is read before the body because a
			// click on that row answers the question rather than opening the
			// brief — which is what the rest of the card does with a press.
			if cmd, took := a.choicePress(msg.Mouse().X, msg.Mouse().Y); took {
				return a, cmd
			}
			a.press(msg.Mouse().Y)
		}
		return a, nil

	case tea.MouseMotionMsg:
		// Motion is the cheapest and commonest message this surface gets — a
		// pointer crossing the window sends one per cell — so [app.setHover]
		// repaints only when the row under it actually changed (hover.go).
		//
		// Two surfaces have no hover at all and drop it here rather than paying
		// for a hit-test per cell: the frozen viewport (nothing under the pointer
		// is actionable) and the linear tier (there is no pointer).
		if a.copy.on || a.linear {
			return a, nil
		}
		if a.sheet.open {
			a.sheetHover(msg.Mouse().Y)
			return a, nil
		}
		a.setHover(msg.Mouse().Y)
		return a, nil

	case submittedMsg:
		return a, a.adopt(msg)

	case streamEventMsg:
		if msg.gen != a.gen {
			return a, nil
		}
		return a, a.event(msg.ev)

	case streamClosedMsg:
		if msg.gen != a.gen {
			return a, nil
		}
		a.stream = nil
		// The turn is over, so a message that was waiting for it starts now
		// (followup.go). Nil when nothing is queued.
		return a, tea.Batch(a.settle(), a.startFollow())

	case taskEventMsg:
		if msg.gen != a.taskGen {
			return a, nil
		}
		return a, a.taskEvent(msg.ev)

	case roomEventMsg:
		if a.room == nil || msg.gen != a.room.gen {
			return a, nil
		}
		return a, a.roomEvent(msg.ev)

	case roomClosedMsg:
		// The node reached its final state, so the lane ended. The room stays
		// open — a person reading what a task did is not finished reading because
		// the task is finished doing — and says so at its foot (room.go).
		if a.room == nil || msg.gen != a.room.gen {
			return a, nil
		}
		a.room.done, a.room.lane = true, nil
		a.roomTouched()
		return a, a.wake()

	case taskPilotMsg:
		return a, a.pilotEvent(msg)

	case taskPilotClosedMsg:
		// The node reached its final state, so its lane ended. The update that
		// said so has usually landed the pilot already ([app.landPilot]); this is
		// the other order, and the generation is what keeps it from forgetting a
		// watcher on a node that has since started again.
		if pilot := a.pilots[msg.id]; pilot != nil && pilot.gen == msg.gen {
			a.landPilot(msg.id)
			a.touch()
		}
		return a, nil

	case taskLaneClosedMsg:
		// The agent this lane belonged to is gone. A lane from an agent that was
		// replaced is already forgotten by its generation, so only the current
		// one is dropped.
		if msg.gen == a.taskGen {
			a.taskLane = nil
		}
		return a, nil

	case gitMsg:
		if msg.ok {
			a.branch, a.branchDirty = msg.branch, msg.dirty
		} else {
			a.branch, a.branchDirty = "", false
		}
		a.touch()
		return a, nil

	case hudFadeMsg:
		// One of the two catch-up ticks: nothing changed, but a number that was
		// news four seconds ago has stopped being news, and the frame has to be
		// drawn again to say so.
		a.touch()
		return a, nil

	case followMsg:
		return a, a.queueFollow(msg)

	case compactedMsg:
		if msg.err != nil {
			a.note("compact failed: " + msg.err.Error())
		}
		return a, nil

	case frameMsg:
		return a, a.paint()
	}
	return a, nil
}

// paint is one turn of the clock: everything that streamed since the last frame
// becomes a frame, the animations step, and — while the turn is still running —
// the next tick is scheduled. It is the ONLY place a streamed delta becomes
// visible, which is what caps the repaint rate.
func (a *app) paint() tea.Cmd {
	a.paints++
	a.dirty = true
	if a.paints%usageEvery == 0 {
		a.refreshUsage()
	}
	a.promoteMarkdown()
	// The welcome box's one-shot animation is the second and last reason this
	// clock runs while nothing is being asked of the model. It steps here and
	// stops itself (welcome.go), which is what makes it one-shot rather than a
	// loop with a condition somebody has to remember to write.
	a.welcome.tick()
	// The countdown on an open proposal runs down here, on the clock that is
	// already turning — no ticker of its own (task.go).
	a.tickTasks()
	// AND THE CLOCK OUTLIVES THE TURN when a node does. A task runs for minutes
	// with no stream open: its spinner, its count-up and the countdown above are
	// the third reason this surface asks for a frame while the model is idle.
	if a.state == stateWorking || a.welcome.animating() || a.tasksAnimating() {
		return frameTick()
	}
	a.painting = false
	return nil
}

// promoteMarkdown is the 1.5s throttle: the settled prefix of a streaming reply
// — everything up to its last newline — is rendered as markdown, and the tail
// keeps streaming plain underneath it.
func (a *app) promoteMarkdown() {
	if a.live < 0 || a.live >= len(a.entries) {
		return
	}
	if time.Since(a.mdAt) < markdownThrottle {
		return
	}
	a.mdAt = time.Now()
	e := &a.entries[a.live]
	cut := strings.LastIndexByte(e.text, '\n') + 1
	if cut <= e.mdCut {
		return
	}
	e.mdCut, e.stale = cut, true
}

// adopt takes the channel a Submit returned. Three shapes are possible and all
// three are ordinary: an error (the turn never started), a steering submit
// while a turn is already being pumped (the session agent's event hub already
// broadcasts that turn to the stream we hold, so the second channel is dropped
// and the message rides the first), or a new stream to follow.
//
// The generation is assigned HERE and never at submit time, because a steering
// submit must not invalidate the stream it is steering.
func (a *app) adopt(msg submittedMsg) tea.Cmd {
	// The attachment tray settles on the same answer: a refused message keeps
	// its pictures, an accepted one has spent them (attach.go).
	a.chipsSettled(msg.err)
	if msg.err != nil {
		a.note("submit failed: " + msg.err.Error())
		return a.settle()
	}
	if msg.ch == nil || a.stream != nil {
		return nil
	}
	a.gen++
	a.stream = msg.ch
	a.state = stateWorking
	a.lastDelta = time.Now()
	a.startClock()
	return tea.Batch(waitEvent(msg.ch, a.gen), a.wake())
}

// event folds one session event into the conversation.
//
// Text deltas mark the live entry stale and stop there: they are the flood, and
// the clock decides when a flood becomes a frame. Everything else is discrete
// and paints at once — a tool beginning is a fact a person is waiting for.
func (a *app) event(ev session.Event) tea.Cmd {
	// after is what this event asks the program loop to DO, as opposed to what
	// it asks the screen to say. Two events produce one — a turn ending, which
	// may ring a terminal nobody is looking at (notify.go), and a task node
	// starting, which opens a watcher on it (task.go) — and both are carried out
	// to the batch below rather than returned early, because the stream still
	// has to be waited on afterwards.
	var after tea.Cmd
	// THE BURN WINDOW OPENS ON THE FIRST EVENT of any turn that did not open one
	// itself. A turn starts in three places — a submit, an attached message, a
	// queued follow-up — and only the first of them runs through [app.submit];
	// the other two are a turn like any other and owe the person the same clock.
	// [app.startClock] keeps whichever window is already open.
	if a.state == stateWorking {
		a.startClock()
	}
	// THE COLLAPSE RULE (thinking.go): the first thing a turn says that is not
	// reasoning ends the reasoning block. EventThinking is exempt because it is
	// the marker that OPENED the run — collapsing on it would close the block
	// before its first word arrived.
	if ev.Kind != session.EventReasoning && ev.Kind != session.EventThinking {
		a.collapseThought()
	}

	switch ev.Kind {
	case session.EventTextDelta:
		a.appendText(ev.Text)
		a.lastDelta = time.Now()

	case session.EventThinking:
		a.lastDelta = time.Now()

	case session.EventReasoning:
		a.appendThought(ev.Text)
		a.lastDelta = time.Now()

	case session.EventConsentRequest:
		// A question outranks a panel. The consent block is drawn above the
		// input, and the settings sheet is the whole screen, so a question that
		// arrived while somebody was reading their settings would be a session
		// blocked on a keyboard behind a fullscreen overlay.
		a.closeSettings()
		a.askConsent(ev)

	case session.EventTaskProposal:
		// A DECISION OUTRANKS A PANEL, for the reason a consent question does:
		// the proposal takes the keyboard's answer lane, and a lane behind a
		// fullscreen sheet is a turn blocked on keys nobody can reach.
		a.closeSettings()
		a.proposeTask(ev)

	case session.EventTaskUpdate:
		// The same event also arrives on the standing lane; [app.taskUpdate]'s
		// (id, state) de-dup is what makes taking both harmless (task.go).
		//
		// The command it hands back is the WATCHER on a node that has just started
		// (task.go's [taskPilot]), and it is carried out through `after` because
		// the de-dup means either lane can be the one that sees "running" first:
		// a surface that armed the pilot only on the standing lane would leave
		// every in-turn node unwatched.
		after = a.taskUpdate(ev)

	case session.EventTitleChanged:
		a.setTitle(ev.Text)

	case session.EventToolAnnounced:
		a.announceTool(ev)

	case session.EventToolBegin:
		a.beginTool(ev)

	case session.EventToolEnd:
		a.closeTool(ev, toolOK, "")

	case session.EventToolFailed:
		a.closeTool(ev, toolFailed, firstNonEmpty(ev.Hint, errText(ev.Err)))

	case session.EventCompacting:
		// THE PASS IS NOW VISIBLE WHILE IT RUNS. The session sends this the
		// moment the cut is made and before the summarizer is called, and that
		// call is the slowest thing on this surface that draws nothing: a turn
		// that stops for eight seconds with no spinner, no text and no tool row
		// is indistinguishable from a hang.
		a.closeLive()
		a.entries = append(a.entries, entry{
			kind:  entryCompact,
			text:  firstNonEmpty(ev.Hint, "compacting"),
			turn:  a.turn,
			began: a.now(),
		})
		a.follow()
		a.touch()

	case session.EventCompacted:
		// ALWAYS the other half of the pair, success or failure — a failed pass
		// says so in its hint and settles the same row, because a row left
		// spinning over a turn that moved on is the defect the pair exists to
		// close.
		a.closeLive()
		a.settleCompaction(firstNonEmpty(ev.Hint, "compacted"))
		// AND RE-READ THE METER HERE. The pass just changed what the
		// conversation weighs by an order of magnitude, and the status line's
		// only other reader is the end of the turn — which is a long way off
		// when compaction fires mid-batch. A meter that keeps showing 168k for
		// another two minutes of tool calls is a meter reporting a conversation
		// that no longer exists.
		a.measureContext()
		a.follow()
		a.touch()

	case session.EventNudge:
		// The loop caught itself repeating a call: a dim one-liner, never an
		// interruption — the model is already being told, the person only
		// needs to see that it was.
		a.note(firstNonEmpty(ev.Hint, "stuck? nudged · "+ev.Tool))

	case session.EventGuardianAllowed:
		// The guardian answered for the person: quiet proof on the row's
		// decision slot, the same place a person's answer would sit.
		a.note("guardian allowed · " + ev.Tool)

	case session.EventTurnDone:
		// Both notes go in BEFORE the turn settles, so they land under the reply
		// they are about rather than above whatever is said next. What was
		// CHANGED comes first and what it COST second: the files are the work,
		// and the money is the surface talking about itself.
		a.changedNote()
		a.cacheNote(ev.Usage)
		a.take(ev.Usage)
		after = tea.Batch(a.settle(), a.notifyDone())

	case session.EventError:
		a.note("error: " + errText(ev.Err))
		// A turn that failed still paid for the steps it took, and its cache
		// reads are as real as a completed turn's.
		a.cacheNote(ev.Usage)
		a.take(ev.Usage)
		after = a.settle()
	}
	if a.stream == nil {
		return after
	}
	// The clock is normally already running (Submit started it), but a stream
	// that outlives its turn's state would otherwise stream into a frame
	// nobody built. Batch drops a nil cmd, so this costs nothing when the
	// clock is up.
	return tea.Batch(after, a.wake(), waitEvent(a.stream, a.gen))
}

// settle ends a turn: the stream is done or abandoned, nothing is live, and the
// state word goes back to idle unless the person interrupted it — an interrupt
// is a fact about the turn that ended and stays on screen until the next one
// starts. The reply it was writing becomes markdown here.
//
// It returns the two commands a settled turn owes the HUD: the repository probe
// (a turn may have committed, branched or dirtied the tree) and the BOUNDED
// fade ticks. Both are the whole of this surface's idle wakeup budget — two
// timers per turn, and nothing at all while nothing is happening.
func (a *app) settle() tea.Cmd {
	a.closeLive()
	// A turn that streamed nothing but reasoning still ends with a block, and a
	// block left open would keep a finished thought expanded over the next turn.
	a.collapseThought()
	// Questions the turn was blocked on died with it. The session already
	// released those calls; a prompt left on screen would be asking about work
	// that is over (consent.go).
	a.dropAsks()
	// A proposal the engine is no longer holding stops asking, for the reason
	// the questions above are dropped — except that this one is CHECKED rather
	// than assumed, because the clock may have answered it (task.go).
	a.syncTaskAsk()
	if a.state == stateWorking {
		a.state = stateIdle
	}
	a.refreshUsage()
	a.measureContext()
	// THE RING IS SAMPLED HERE AND NOWHERE ELSE: one reading per turn, taken at
	// the only moment the figure is comparable with the reading before it.
	a.sampleContext()
	a.turnBegan, a.turnOutStart = time.Time{}, 0
	a.approval = readApproval(a.profileDir)
	a.mouse = config.MouseEnabledAt(a.profileDir)
	a.follow()
	a.touch()
	return tea.Batch(a.probeGit(), fadeTicks())
}

// fadeTicks are the two catch-up wakeups a settled turn schedules: one where
// the fresh tier ends and one where the warm tier does. They are tea.Ticks and
// not a ticker on purpose — see [hudFadeMsg].
func fadeTicks() tea.Cmd {
	return tea.Batch(
		tea.Tick(hudFresh, func(time.Time) tea.Msg { return hudFadeMsg{} }),
		tea.Tick(hudWarm, func(time.Time) tea.Msg { return hudFadeMsg{} }),
	)
}

// sampleContext appends this turn's context reading to the ring, keeping the
// last [ctxRingSize]. A reading identical to the one before it is still kept: a
// flat run is exactly what the ETA estimator needs to see to say nothing.
//
// ONE READING PER TURN, and the turn counter is what enforces it: a turn ends
// TWICE on this surface — the session's own EventTurnDone, and then the stream
// closing behind it — and a ring that took both would report half the growth
// per turn and forecast a compaction that is twice as far away as it is.
func (a *app) sampleContext() {
	if a.ctxTokens <= 0 || a.ringTurn == a.turn {
		return
	}
	a.ringTurn = a.turn
	a.ctxRing = append(a.ctxRing, a.ctxTokens)
	if len(a.ctxRing) > ctxRingSize {
		a.ctxRing = a.ctxRing[len(a.ctxRing)-ctxRingSize:]
	}
}

// closeLive ends the assistant block being streamed into. A block nobody is
// writing any more is a finished document, so it renders as one.
func (a *app) closeLive() {
	if a.live >= 0 && a.live < len(a.entries) {
		e := &a.entries[a.live]
		e.settled, e.stale = true, true
	}
	a.live = -1
}

func (a *app) refreshUsage() {
	if a.agent == nil {
		return
	}
	a.take(a.agent.Usage())
}

// take folds one usage report into the status line's figures. Every field takes
// the LARGER of what it holds and what arrived, because a turn's usage and the
// session's total both come through here and only the session's total is
// monotonic — a per-turn event must never shrink a running total.
func (a *app) take(u session.Usage) {
	if u.CostUSD > a.cost {
		a.cost = u.CostUSD
	}
	if n := u.Input + u.Output; n > a.tokens {
		a.tokens = n
	}
	if u.Output > a.outputTokens {
		a.outputTokens = u.Output
	}
	if u.Input > a.inputTokens {
		a.inputTokens = u.Input
	}
	if u.CacheRead > a.cacheRead {
		a.cacheRead = u.CacheRead
	}
	if u.CacheWrite > a.cacheWrite {
		a.cacheWrite = u.CacheWrite
	}
}

// appendText grows the live assistant block, opening one if the last thing on
// screen was a tool line or a user message.
func (a *app) appendText(text string) {
	if text == "" {
		return
	}
	if a.live < 0 || a.live >= len(a.entries) || a.entries[a.live].kind != entryAssistant {
		a.entries = append(a.entries, entry{kind: entryAssistant, turn: a.turn})
		a.live = len(a.entries) - 1
		a.mdAt = time.Now()
	}
	e := &a.entries[a.live]
	e.text += text
	e.stale = true
	a.follow()
}

// announceTool draws the row for a call the model has finished asking for
// (session.EventToolAnnounced). Nothing has started, so the row is queued: a
// dim ◌, no spinner, and — for an edit or a write — the change it is ABOUT to
// make, previewed underneath from the arguments (toolview.go).
//
// A row is only ever announced once, but a surface that attached mid-batch may
// see a begin with no announcement and must not draw a second line for it, so
// the pairing rule lives in [app.claimAnnounced] and both events use it.
func (a *app) announceTool(ev session.Event) {
	a.closeLive()
	a.entries = append(a.entries, entry{
		kind: entryTool, tool: ev.Tool, text: ev.Hint, turn: a.turn,
		status: toolQueued, detail: toolDetail{Args: ev.Args},
	})
	a.follow()
	a.touch()
}

// beginTool is EXECUTION STARTED. It adopts the row the announcement drew —
// the same call, one line, which is the whole point of announcing it — and
// starts that row's clock. A begin nobody announced draws its own row, which is
// every provider that does not stream tool calls and every surface that
// attached late.
func (a *app) beginTool(ev session.Event) {
	if at := a.claimAnnounced(ev); at >= 0 {
		e := &a.entries[at]
		e.status = toolRunning
		e.began = a.now()
		e.detail.Args = firstNonEmpty(ev.Args, e.detail.Args)
		e.text = firstNonEmpty(ev.Hint, e.text)
		a.follow()
		a.touch()
		return
	}
	a.closeLive()
	a.entries = append(a.entries, entry{
		kind: entryTool, tool: ev.Tool, text: ev.Hint, turn: a.turn,
		status: toolRunning, began: a.now(), detail: toolDetail{Args: ev.Args},
	})
	a.follow()
	a.touch()
}

// claimAnnounced finds the queued row this begin belongs to, or -1.
//
// The payload is matched FIRST and the tool name only after: a batch of three
// edits to three files announces three rows, and pairing by name alone would
// start the clock on whichever of them was drawn first. Identical arguments are
// the one case where the two rules disagree and it does not matter — two calls
// with the same name and the same payload are the same work, in either order.
func (a *app) claimAnnounced(ev session.Event) int {
	fallback := -1
	for i := range a.entries {
		e := &a.entries[i]
		if e.kind != entryTool || e.status != toolQueued || e.tool != ev.Tool {
			continue
		}
		if ev.Args != "" && e.detail.Args == ev.Args {
			return i
		}
		if fallback < 0 {
			fallback = i
		}
	}
	return fallback
}

// closeTool marks the oldest still-running line for that tool. Oldest rather
// than newest because tools run in parallel and finish in any order, and the
// first one begun is the first one a person watching the column expects to
// resolve.
//
// The end event carries the call's Args as well as its Output — session sends
// a self-contained end — so both are taken from it here rather than kept from
// the begin: a row rebuilt from one event is a row that cannot disagree with
// itself. The failure text is a fallback for the Output, because a tool that
// failed before it ran has a reason and no result.
func (a *app) closeTool(ev session.Event, status toolState, why string) {
	for i := range a.entries {
		e := &a.entries[i]
		if e.kind != entryTool || !e.status.live() || e.tool != ev.Tool {
			continue
		}
		e.status = status
		e.ended = a.now()
		e.detail.Args = firstNonEmpty(ev.Args, e.detail.Args)
		e.detail.Output = firstNonEmpty(ev.Output, why)
		if why != "" && status == toolFailed {
			e.text = strings.TrimSpace(e.text + " — " + why)
		}
		// A FAILURE OPENS ITSELF. Everything else on this surface waits to be
		// asked, because a quiet line is a success and success has nothing to
		// read; a call that failed is the one row whose detail is the reason the
		// person is looking at the screen, and making them click for it is
		// making them click for the only thing that happened.
		if status == toolFailed {
			e.open = true
		}
		// A CALL THAT CLOSED IS THE ONLY THING THAT MOVES THE AMBIENT COUNTS OR
		// THE SESSION DELTA: both are sums over finished calls, so this is the
		// one place their cache has to be dropped (see [app.hudStats]).
		a.hudStale = true
		a.follow()
		a.touch()
		return
	}
	// A close with no open line still deserves to be seen rather than
	// silently dropped: the session said something happened.
	if status == toolFailed {
		a.entries = append(a.entries, entry{
			kind: entryTool, tool: ev.Tool, text: why, turn: a.turn, status: toolFailed,
			open:   true,
			detail: toolDetail{Args: ev.Args, Output: firstNonEmpty(ev.Output, why)},
		})
		a.follow()
		a.touch()
	}
}

// settleCompaction stops the compaction row's clock: the LAST one still running
// takes the finished hint and the end time, and turns into the rule.
//
// Last rather than first, which is what [app.closeTool] does and for the
// opposite reason: tool calls overlap and resolve in any order, while a session
// compacts one pass at a time (session holds a compacting flag across it), so
// the only unfinished row there can be is the newest one — and walking backwards
// finds it without reading the whole conversation.
//
// A settle with NO row to settle is not an error and is not dropped: a resumed
// session replays a transcript that already contains passes nobody watched, and
// an older session predates the start event entirely. Those get a row born
// finished — the divider they always drew, with no duration claimed, because a
// pass this surface did not see the start of has no honest elapsed time.
func (a *app) settleCompaction(text string) {
	for i := len(a.entries) - 1; i >= 0; i-- {
		e := &a.entries[i]
		if e.kind != entryCompact || !e.ended.IsZero() {
			continue
		}
		e.text, e.ended = text, a.now()
		e.stale = true
		return
	}
	now := a.now()
	a.entries = append(a.entries, entry{
		kind: entryCompact, text: text, turn: a.turn, began: now, ended: now,
	})
}

// note appends a surface-side line — a slash command's answer, an error, the
// opening hint. It is never sent anywhere.
func (a *app) note(text string) {
	a.closeLive()
	a.entries = append(a.entries, entry{kind: entryNote, text: text, turn: a.turn})
	a.follow()
	a.touch()
}

// setTitle takes the name the session gave itself (session.EventTitleChanged).
// It is a status-line fact and nothing more: no note, no line in the
// transcript. The session named itself, which is not news the conversation
// needs — it is a label, and a label belongs where the labels are.
func (a *app) setTitle(title string) {
	title = strings.TrimSpace(title)
	if title == "" || title == a.title {
		return
	}
	a.title = title
	a.touch()
}

// submit sends one message. It always goes through a command: Submit talks to
// a lock and possibly a provider, and the Update loop is not a place to wait.
func (a *app) submit(text string) tea.Cmd {
	agent, ctx := a.agent, a.ctx
	a.closeLive()
	a.turn++
	// A new turn drops the selection: the calls it was pointing into belong to
	// the turn before this one, and a cursor left on them would answer enter
	// with somebody else's history.
	a.sel = -1
	a.entries = append(a.entries, entry{kind: entryUser, text: text, turn: a.turn})
	a.state = stateWorking
	a.lastDelta = time.Now()
	a.startClock()
	a.follow()
	a.touch()
	return tea.Batch(func() tea.Msg {
		ch, err := agent.Submit(ctx, text)
		return submittedMsg{ch: ch, err: err}
	}, a.wake())
}

// startClock opens the burn window: this turn's start, and the output total it
// started from. A turn that is already running keeps the clock it has —
// steering a turn mid-flight (a plain enter) is not a second turn, and
// restarting the window there would report the rate of the last four seconds as
// the rate of the turn.
func (a *app) startClock() {
	if !a.turnBegan.IsZero() {
		return
	}
	a.turnBegan, a.turnOutStart = a.now(), a.outputTokens
}

// now is the time, from the seam rather than from the package: see [app.clock].
func (a *app) now() time.Time {
	if a.clock != nil {
		return a.clock()
	}
	return time.Now()
}

// touch says the rows no longer match the entries, and the next frame rebuilds
// them. Deltas deliberately do NOT call it — see [app.paint].
func (a *app) touch() { a.dirty = true }

// wake starts the paint clock if it is not already running.
func (a *app) wake() tea.Cmd {
	if a.painting {
		return nil
	}
	a.painting = true
	return frameTick()
}

func frameTick() tea.Cmd {
	return tea.Tick(frameInterval, func(time.Time) tea.Msg { return frameMsg{} })
}

// running reports whether any call of the current turn is still unresolved —
// queued, asked about, or spinning. All three are something on screen for the
// person to watch, which is the question the ellipsis is asking.
func (a *app) running() bool {
	for i := range a.entries {
		e := &a.entries[i]
		if e.kind == entryTool && e.status.live() && e.turn == a.turn {
			return true
		}
	}
	return false
}

// quiet reports whether the stream has been silent long enough to say so.
func (a *app) quiet() bool {
	return !a.lastDelta.IsZero() && time.Since(a.lastDelta) >= quietBeforeEllipsis
}

// waitEvent takes one event from the stream. Re-issued after each one, this is
// the whole bridge between the session's goroutine and the program loop.
func waitEvent(ch <-chan session.Event, gen int) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return streamClosedMsg{gen: gen}
		}
		return streamEventMsg{gen: gen, ev: ev}
	}
}

// unfold is ctrl+o: every call of the current turn on its own line, or back to
// the last [toolWindow] of them.
func (a *app) unfold(turn int) {
	a.unfolded[turn] = !a.unfolded[turn]
	a.touch()
}

// openTool expands one call inline — its tool-shaped expansion, under the rail.
// Closing it also drops a lifted cap: the next opening starts at the window
// again, because "show me everything" was said about a block that is no longer
// on screen.
//
// It is also the KEYBOARD DOOR INTO A ROOM, because it is the one thing enter
// does with a selected row and a proposal is now selectable: a card whose node
// has started opens that node's page instead of an expansion (room.go). The
// branch is here rather than in [app.enter] so that "enter opens what ↑/↓
// picked" stays one sentence with one implementation.
func (a *app) openTool(i int) {
	if a.openRoomAt(i) {
		return
	}
	if i < 0 || i >= len(a.entries) || a.entries[i].kind != entryTool || replayInert(&a.entries[i]) {
		return
	}
	e := &a.entries[i]
	e.open = !e.open
	if !e.open {
		e.full = false
	}
	a.sel = i
	a.touch()
}

// showAll lifts one expansion's cap — the click on "… N more lines".
func (a *app) showAll(i int) {
	if i < 0 || i >= len(a.entries) || a.entries[i].kind != entryTool {
		return
	}
	a.entries[i].full = true
	a.touch()
}

// press resolves a click to the row it landed on. A click that lands on
// nothing does nothing: this surface has no empty-space gesture.
func (a *app) press(y int) {
	// A CLICK IN THE BODY IS THE WAY OUT OF A ROOM. While one is open the region
	// under this pointer is the node's page, not the conversation, and the
	// gesture that returns to the conversation is pressing it (room.go). The rail
	// was offered this click first and did not want it, so anything landing in
	// the body region here is the person reaching back past the room.
	if a.roomOpen() {
		if top := a.bodyTop(); top >= 0 && y >= top && y < top+a.viewHeight() {
			a.closeRoom()
		}
		return
	}
	// The welcome box gets the click first, because while it is up it is the
	// thing between the pointer and everything else: a recent session opens,
	// and anywhere else is the person reaching past the box, which is what
	// dismissal means (welcome.go).
	if a.welcome.open {
		if mark, ok := a.chromeAt(y); ok && mark.kind == chromeWelcome {
			a.welcomePress(a.welcomeSlotAt(mark.index))
			return
		}
		a.dismissWelcome()
	}
	r, ok := a.rowAt(y)
	if !ok {
		return
	}
	// A click anywhere on a thinking block toggles it — the whole block is the
	// target, because a collapsed one is a single row and asking somebody to hit
	// a five-cell label is asking them to aim (thinking.go).
	if r.entry >= 0 && r.entry < len(a.entries) && a.entries[r.entry].kind == entryThinking {
		a.toggleThought(r.entry)
		return
	}
	switch r.hit {
	case hitTool:
		a.openTool(r.entry)
	case hitFold:
		a.unfold(r.turn)
	case hitMore:
		a.showAll(r.entry)
	case hitTask:
		// A click anywhere on a proposal opens its brief, for the reason a click
		// anywhere on a thinking block opens that (task.go).
		a.toggleCardAt(r.entry)
	case hitDone:
		// And a click anywhere on a landed card opens its full context, which is
		// the same gesture answering the same question about the same object one
		// state later (taskdone.go).
		a.toggleDoneAt(r.entry)
	case hitChoice:
		// The choices row was offered this click before the body and took it
		// (see [app.choicePress]); reaching here means the pointer was in a
		// column no option occupies, and empty space on this surface does
		// nothing.
	}
}

// choicePress resolves a click on a proposal's choices row to the option under
// the pointer, and reports whether it took the click.
//
// A press anywhere on that ROW is the row's, whether or not it landed on an
// option: the alternative is a click in the gap between two answers falling
// through to the card and collapsing the brief, which would make the row a place
// where missing costs you the thing you were reading.
func (a *app) choicePress(x, y int) (tea.Cmd, bool) {
	if a.roomOpen() || a.welcome.open {
		return nil, false
	}
	r, ok := a.rowAt(y)
	if !ok || r.hit != hitChoice || r.entry < 0 || r.entry >= len(a.entries) {
		return nil, false
	}
	card := a.entries[r.entry].card
	// The open question is the only one that can be answered, and it is the one
	// the lane holds: an older card still on screen has already settled.
	if card == nil || card != a.task || card.settled() {
		return nil, true
	}
	for _, span := range card.spans {
		if x >= span.from && x < span.to {
			return a.takeChoice(span.at), true
		}
	}
	return nil, true
}

// selectTool moves the selection through the tool calls that are actually on
// the row list — the folded ones are not selectable, because selecting a row
// nobody can see is a cursor that has vanished. Walking off either end returns
// false, and the key that asked falls through to scrolling.
func (a *app) selectTool(delta int) bool {
	rows := a.visible(a.bodyWidth())
	var calls []int
	for _, r := range rows {
		// A PROPOSAL WALKS WITH THE CALLS, AND SO DOES A LANDED CARD. They are the
		// other blocks on this surface that enter opens into something — a call
		// opens its expansion, a proposal and a finished node open that node's room
		// (room.go) — and a keyboard that could reach one and not the others would
		// make the room a mouse-only place. The walk is also what gives ctrl+o
		// something to act on: the key a card names is spent on the SELECTED card
		// (taskdone.go's [app.openDone]).
		if r.hit != hitTool && r.hit != hitTask && r.hit != hitDone {
			continue
		}
		if len(calls) == 0 || calls[len(calls)-1] != r.entry {
			calls = append(calls, r.entry)
		}
	}
	if len(calls) == 0 {
		return false
	}
	at := -1
	for i, e := range calls {
		if e == a.sel {
			at = i
			break
		}
	}
	switch {
	case at < 0 && delta < 0:
		at = len(calls) - 1 // ↑ from nowhere takes the most recent call
	case at < 0:
		at = 0
	default:
		at += delta
	}
	// The two entries that gain or lose the cursor are marked stale by hand, for
	// the reason [app.setHover] marks its two: a block whose rows are CACHED —
	// which a settled proposal's are (render.go) — would otherwise keep the
	// selection it was drawn with when the cursor was on it.
	a.markStale(a.sel)
	if at < 0 || at >= len(calls) {
		a.sel = -1
		a.touch()
		return false
	}
	a.sel = calls[at]
	a.markStale(a.sel)
	a.touch()
	a.reveal(a.sel)
	return true
}

// slash consumes a command line. Everything starting with "/" is answered
// here and nothing starting with "/" is ever sent to the model — including a
// command nobody defined, which gets a hint instead of a turn.
func (a *app) slash(line string) tea.Cmd {
	name, rest, _ := strings.Cut(strings.TrimPrefix(line, "/"), " ")
	rest = strings.TrimSpace(rest)
	switch strings.ToLower(name) {
	case "quit", "exit", "q":
		return a.quit()

	case "help", "?":
		a.note(helpText(a.file))
		return nil

	case "model":
		// Bare /model is a question — "which ones are there" — and the picker
		// is the answer. A slug is an instruction, and an instruction that
		// opened a list to confirm itself would be the surface asking a person
		// to say something twice.
		if rest == "" {
			a.openPicker()
			return nil
		}
		a.switchModel(rest, 0)
		return nil

	case "image":
		// The other door onto the tray, for a picture that is not under this
		// directory or not in the walk: a path, attached (attach.go).
		a.attachPath(rest)
		return nil

	case "settings", "set", "config":
		a.openSettings()
		return nil

	case "compact":
		agent, ctx := a.agent, a.ctx
		a.note("compacting…")
		return func() tea.Msg { return compactedMsg{err: agent.Compact(ctx)} }

	case "new":
		// The command that replaces the agent is the one command here that
		// returns work: the standing task lane belongs to the agent that handed
		// it over, so the next conversation subscribes to its own (task.go).
		return a.renew()

	default:
		a.note("unknown command: /" + name + " · try /help")
		return nil
	}
}

// renew closes this conversation and opens the next one on the same config.
// The transcript is cleared because it belongs to the agent that just closed:
// a fresh session file with the old conversation still on screen would be the
// surface claiming context the model does not have.
// It returns the one command the next conversation owes itself: its own
// standing task subscription (task.go).
func (a *app) renew() tea.Cmd {
	if a.fresh == nil {
		a.note("/new is unavailable here")
		return nil
	}
	if a.state == stateWorking {
		a.agent.Interrupt()
	}
	if err := a.agent.Close(); err != nil {
		a.note("close failed: " + err.Error())
	}
	agent, file, err := a.fresh()
	if err != nil {
		a.note("new session failed: " + err.Error())
		return nil
	}
	a.agent, a.file = agent, file
	a.entries = nil
	a.live, a.sel, a.think = -1, -1, -1
	a.asks, a.follows = nil, nil
	a.title = strings.TrimSpace(agent.Title())
	a.turn = 0
	a.unfolded = map[int]bool{}
	a.dropHover()
	// A frozen viewport is a snapshot of a conversation that no longer exists
	// (copymode.go), for the same reason the hover is dropped one line above.
	a.copy = copyMode{mark: -1}
	// The rail goes with the conversation: its nodes died with the agent that
	// started them, and a row left standing would be presence claimed for work
	// nobody is doing (task.go).
	a.dropTasks()
	a.stream = nil
	a.gen++
	a.state = stateIdle
	a.resetMeters()
	a.model = agent.Model()
	// The draft is NOT cleared: /new closes a conversation, and the sentence in
	// the box is the person's next one (draft.go).
	a.endRecall()
	a.offset, a.stick = 0, true
	a.touch()
	if file != "" {
		a.note("new session · " + file)
	} else {
		a.note("new session")
	}
	return a.watchTasks()
}

func (a *app) quit() tea.Cmd {
	// The draft goes to disk on the way out, synchronously and before anything
	// else: the debounce may be mid-window, and a sentence typed in the last
	// three hundred milliseconds of a session is exactly the one a person would
	// be most surprised to lose (draft.go).
	if a.draftFile != "" {
		writeDraft(a.draftFile, a.input.String())
	}
	if a.agent != nil {
		a.agent.Interrupt()
		_ = a.agent.Close()
	}
	return tea.Quit
}

// interrupt is esc: stop the turn, keep what it said.
func (a *app) interrupt() {
	if a.state != stateWorking {
		return
	}
	a.agent.Interrupt()
	a.state = stateInterrupted
	a.note("interrupted")
	// The session drops its follow-up queue on an interrupt — a stop that was
	// followed by the session working again is not a stop — so the surface says
	// so rather than leaving a count above the box for turns that will never run.
	a.dropFollows()
}

// paste inserts pasted text into the draft. It is a method rather than an
// inline insert because a paste is an edit like any other: the overlays follow
// it, and the draft debounce is armed by it.
func (a *app) paste(text string) tea.Cmd {
	if text == "" {
		return nil
	}
	// Bracketed paste arrives with the SENDER's line endings, and tmux sends
	// CR: an editor that breaks rows on LF alone would hold one "line" whose
	// carriage returns paint each logical line over the last. Normalize once,
	// at the door — CRLF first, then bare CR.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	// The model overlay is modal for the keyboard, so it is modal for the
	// clipboard: a paste while it is up is a filter somebody copied.
	if a.pick.open {
		a.pick.filter.insert(strings.ReplaceAll(text, "\n", " "))
		a.pick.rank()
		a.touch()
		return nil
	}
	// The settings sheet owns the clipboard while it is up, the same way it
	// owns the keyboard: into the text row being answered first (an API key
	// is the paste this path exists for), into the select row's filter next,
	// into the search box otherwise. All three are one-line boxes — newlines
	// flatten to spaces.
	if a.sheet.open {
		flat := strings.ReplaceAll(text, "\n", " ")
		switch {
		case a.sheet.edit != nil:
			a.sheet.edit.box.insert(flat)
		case a.sheet.sel != nil:
			a.sheet.sel.pick.filter.insert(flat)
			a.sheet.sel.pick.rank()
		default:
			a.sheet.query.insert(flat)
		}
		a.touch()
		return nil
	}
	a.input.insert(text)
	return a.edited()
}

// ── the two typed overlays ──────────────────────────────────────────────────

// listKey routes the keys that belong to an open command list or file
// completion, and reports whether it took the key. Everything it does not take
// falls through to the editor, which is the whole difference between these two
// overlays and the modal model picker: the person is still typing a sentence.
func (a *app) listKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.menu.open && !a.comp.open {
		return nil, false
	}
	switch msg.String() {
	case "up", "ctrl+p":
		if a.menu.open {
			a.menu.move(-1)
		} else {
			a.comp.move(-1)
		}
		a.touch()
		return nil, true

	case "down", "ctrl+n":
		if a.menu.open {
			a.menu.move(1)
		} else {
			a.comp.move(1)
		}
		a.touch()
		return nil, true

	case "esc":
		a.closeLists()
		a.touch()
		return nil, true

	case "enter":
		if a.menu.open {
			if _, ok := a.menu.choice(); !ok {
				// Nothing matched what was typed. The line is still a line, and
				// enter is still submit — /nonsense gets its answer.
				a.menu.close()
				return nil, false
			}
			return a.runMenu(), true
		}
		if _, ok := a.comp.choice(); !ok {
			a.comp.close()
			return nil, false
		}
		a.completeFile()
		return a.edited(), true
	}
	return nil, false
}

// syncLists is what every edit runs: the overlays follow the draft, and never
// the other way round. At most one is open — a line that starts with "/" is a
// command being chosen, and an @ inside it would be an argument to a command
// this surface does not have.
func (a *app) syncLists() tea.Cmd {
	a.menu.sync(a.input.String())
	if a.menu.open {
		a.comp.close()
		return nil
	}
	was := a.comp.open
	a.comp.sync(&a.input)
	if a.comp.open && !was {
		return a.loadFiles()
	}
	return nil
}

func (a *app) closeLists() {
	a.menu.close()
	a.comp.close()
}

// ── the context meter ───────────────────────────────────────────────────────

// accentAtThresholdPercent is when the meter stops being dim: at 80% of the
// COMPACTION THRESHOLD, not of the window.
//
// The window is not the thing that happens to you. Compaction is — the summary
// call, the paid-for pass, the tail that survives and the middle that does not —
// and it fires well below the window (session.CompactThreshold). A meter that
// waited for 80% of the window would go quiet through the whole approach and
// then light up after the event it was warning about.
const accentAtThresholdPercent = 80

// resetMeters zeroes everything the status line counts. It is called wherever
// the agent underneath this surface is REPLACED — /new, and opening a recent
// session from the welcome box — because every one of these figures is a fact
// about one conversation and carrying any of them into the next one would be a
// bill for somebody else's work.
//
// It is a method rather than the tuple assignment it replaced so that a figure
// added here is reset in both places rather than in whichever one was edited.
func (a *app) resetMeters() {
	a.cost = 0
	a.tokens, a.inputTokens, a.outputTokens = 0, 0, 0
	a.cacheRead, a.cacheWrite = 0, 0
	a.cacheSaved = 0
	a.ctxTokens = 0
	// The HUD's own state is a fact about one conversation too: a sparkline
	// carried across /new would be a graph of somebody else's context, and an
	// ambient count would be claiming jobs that died with the agent.
	a.ctxRing, a.ringTurn = nil, 0
	a.turnBegan, a.turnOutStart = time.Time{}, 0
	a.hud, a.hudStale = hudStats{}, true
	a.segText, a.segAt = [segCount]string{}, [segCount]time.Time{}
}

// measureContext asks the agent what the conversation now weighs. It is called
// where the answer CHANGES — a turn ending, a session opening or being replaced
// — and never on the frame clock: the agent takes its own lock to answer, and
// doing that thirty times a second to move a figure that changes once a turn is
// a lock taken for nothing.
func (a *app) measureContext() {
	if a.agent == nil {
		return
	}
	a.ctxTokens = a.agent.ContextTokens()
	if a.ctxWindow <= 0 {
		// The door may not have known the window at boot: a cold catalog
		// resolves in the background AFTER this surface is already up, and it
		// tells the agent directly (cmd/aforge's warmV3Models) where there is
		// no seam back to here. Asking the model list again is that seam, and
		// it cannot block — [app.modelList] falls through to the disk cache and
		// then to the built-ins. Once it answers, it is never asked again.
		a.ctxWindow = a.windowFor(a.model)
	}
}

// ctxPercent is the meter: how much of the model's window the conversation is
// using, to the NEAREST whole percent. False when nobody has said what the
// window is, because a percentage of an unknown is a number that means nothing.
//
// Nearest rather than floored, which is what this did while it was a percentage
// of a byte estimate. Flooring a figure that is already an estimate rounds the
// same direction every time, and the direction it rounds is the reassuring one:
// a conversation at 9.7% of its window reads as 9%, and one at 79.9% of the
// threshold reads as under it.
func (a *app) ctxPercent() (int, bool) {
	if a.ctxWindow <= 0 || a.ctxTokens <= 0 {
		return 0, false
	}
	return (a.ctxTokens*200/a.ctxWindow + 1) / 2, true
}

// ctxHeat is the meter's THREE-RUNG RAMP, and every rung is measured against
// the compaction threshold rather than against the window (see
// [accentAtThresholdPercent] for why):
//
//	ctxCalm  dim       nothing is approaching
//	ctxNear  accent    past 80% of the threshold — compaction is coming
//	ctxDue   pal.bad   past the threshold ITSELF — it is due or overdue, and the
//	                   next turn will pay for a summarizer call
//
// The third rung exists because the second one used to be the end of the ramp:
// a conversation at 81% of the threshold and one 40k past it were painted
// identically, and the second is the only one where the person can still act —
// finish the thought, /new, split the work — before a pass takes the middle of
// the conversation away.
//
// Every rung is ctxCalm whenever the window is unknown: a surface that does not
// know the threshold must not guess that one has been crossed.
type ctxHeat int

const (
	ctxCalm ctxHeat = iota
	ctxNear
	ctxDue
)

func (a *app) ctxHeat() ctxHeat {
	threshold := session.CompactThreshold(a.ctxWindow)
	if threshold <= 0 || a.ctxTokens <= 0 {
		return ctxCalm
	}
	switch {
	case a.ctxTokens >= threshold:
		return ctxDue
	case a.ctxTokens*100 >= threshold*accentAtThresholdPercent:
		return ctxNear
	}
	return ctxCalm
}

// ctxCrowded says the conversation is close enough to compaction that the meter
// should stop being furniture — the bottom of the ramp, kept as the one-bit
// question the rest of the surface asks.
func (a *app) ctxCrowded() bool { return a.ctxHeat() >= ctxNear }

// priceFor is what this surface knows a model's tokens cost, from the same list
// the picker draws (models.go's three rungs). The bool is false when nobody has
// published a prompt price — an id off the built-ins, a cache written before the
// prices were kept, one of OpenRouter's own routers — and a caller must then
// show the tokens alone rather than a saving computed from zero.
func (a *app) priceFor(id string) (Model, bool) {
	id = strings.TrimSpace(id)
	for _, model := range a.modelList() {
		if strings.EqualFold(model.ID, id) && model.PromptPrice > 0 {
			return model, true
		}
	}
	return Model{}, false
}

// cacheNote is the per-turn savings line: what this turn read off a warm prefix
// and what that was worth.
//
//	⟲ 9.8k cached · saved .0041
//
// It is written only when there were cache reads, so a session on a provider
// that caches nothing — or a first turn, which can only write — says nothing at
// all rather than reporting a zero every turn.
//
// The saving is cached tokens × (prompt price − cache-read price), which is the
// honest figure: a cache read is CHEAPER, never free, and the difference is what
// the cache actually bought. Without a published price pair the line degrades to
// the token count, because "9.8k cached" is a true thing this surface knows and
// "saved $0.0000" is not.
func (a *app) cacheNote(u session.Usage) {
	if u.CacheRead <= 0 {
		return
	}
	line := "⟲ " + tokenWord(u.CacheRead) + " cached"
	if model, known := a.priceFor(a.model); known {
		if saved := float64(u.CacheRead) * (model.PromptPrice - model.CacheReadPrice); saved > 0 {
			// The same figure, twice: once for this turn, and once into the
			// session's running total behind the status line's warm share. It is
			// summed HERE — under the same price guard — so the total can never
			// contain a turn the note itself could not price.
			a.cacheSaved += saved
			line += " · saved " + savedWord(saved)
		}
	}
	a.note(line)
}

// ── WHAT CHANGED ────────────────────────────────────────────────────────────
//
// A turn that touched files ends with one dim line saying which:
//
//	· 2 files · loop.go +32 −2 · agent.go +18 −0
//
// It exists because of what a tool cluster looks like AFTER it has scrolled. A
// turn that edits four files across nine calls draws nine rows, three of which
// are visible by then, and the question a person actually has when the turn
// stops — "so what did it change?" — is answered nowhere on the screen. The
// individual +N −M stats are on rows that folded; the reply above says what the
// model meant to do, which is not the same claim.
//
// It is derived from the SAME arguments the expansions are (toolstat.go), so
// the figures cannot disagree with the diffs a click opens: an edit's stat is
// its replacements diffed, a write's is the lines it laid down.
//
// What is excluded, and why: reads and bashes and searches. A read changes
// nothing, and a bash MAY change everything but says so nowhere a surface can
// see — a line that reported four files after a `make` that rewrote two hundred
// would be a lie with a number in it. This line's claim is narrow on purpose:
// these are the files this turn wrote THROUGH THE TOOLS THAT SAY WHAT THEY
// WROTE.

// changedFiles is how many files the line names before it stops naming them.
// Four is the width a dim line can carry at eighty columns; past it the count
// at the front is doing the work anyway.
const changedFiles = 4

// fileStat is one file's share of a turn.
type fileStat struct {
	path       string
	adds, dels int
}

// changedNote appends the line, or nothing at all when the turn wrote nothing.
func (a *app) changedNote() {
	if stats := a.turnStats(a.turn); len(stats) > 0 {
		a.note(changedWord(stats))
	}
}

// turnStats gathers one turn's file writes, in the order they were first
// touched — which is the order they happened, and the only order that does not
// need a rule.
//
// A failed call is skipped: an edit that did not apply changed nothing, and a
// line that counted it would be reporting a file that is on disk as its author
// left it. A call still running is skipped for the same reason — though by the
// time this runs the turn is over, so that is a belt on a done deal.
func (a *app) turnStats(turn int) []fileStat {
	var out []fileStat
	at := map[string]int{}
	add := func(path string, adds, dels int) {
		if path == "" {
			return
		}
		if i, seen := at[path]; seen {
			out[i].adds += adds
			out[i].dels += dels
			return
		}
		at[path] = len(out)
		out = append(out, fileStat{path: path, adds: adds, dels: dels})
	}
	for i := range a.entries {
		e := &a.entries[i]
		if e.kind != entryTool || e.turn != turn || e.status != toolOK {
			continue
		}
		fields := argsOf(e.detail.Args)
		switch e.tool {
		case "edit":
			adds, dels := editStat(e.detail.Args)
			add(argString(fields, "path"), adds, dels)
		case "write":
			add(argString(fields, "path"), lineCount(argString(fields, "content")), 0)
		}
	}
	return out
}

// changedWord spells the line. The files are named by their BASE names
// (welcome.go's [baseName], which is the package's one answer to that question):
// the directory is what the tool rows above already showed, and a line of full
// paths at eighty columns is one file per line.
func changedWord(stats []fileStat) string {
	head := itoa(len(stats)) + " files"
	if len(stats) == 1 {
		head = "1 file"
	}
	parts := []string{head}
	shown := stats
	if len(shown) > changedFiles {
		shown = shown[:changedFiles]
	}
	for _, s := range shown {
		parts = append(parts, baseName(s.path)+" "+
			glyphAdd+itoa(s.adds)+" "+glyphDel+itoa(s.dels))
	}
	if rest := len(stats) - len(shown); rest > 0 {
		parts = append(parts, "+"+itoa(rest)+" more")
	}
	return strings.Join(parts, " · ")
}

// ── WHAT IS ALIVE, AND WHAT WAS WRITTEN ─────────────────────────────────────
//
// hudStats is the pair of sums the right cluster reports about the whole
// conversation: how much background work this session started and has not
// watched stop, and what it has written to disk across every turn.
type hudStats struct {
	// jobs and watches are ALIVE, by the only definition this surface can hold
	// honestly: it saw them start and it has not seen them killed.
	//
	// The session owns the real job table (internal/session's jobs.go) and does
	// not publish it, so this is read off the transcript the surface already
	// drew — a `bash` call that succeeded with background:true is a job, a
	// `watch` call that succeeded is a watch, and a `jobs` call with action
	// "kill" takes one of them away. The id is matched where the output gave one
	// ("job 3 started"), because a batch that starts three jobs and kills the
	// second must not decrement the first.
	//
	// WHAT IT CAN GET WRONG, said out loud: a job that exited on its own is
	// still counted, because nothing on the wire says so. That is the honest
	// direction to be wrong in — the count is "what you started", and a person
	// who sees "1 job" and finds it finished has lost nothing, where a count
	// that silently dropped a live server would have.
	jobs, watches int
	// adds and dels are the session's diffstat, summed from the same arguments
	// the tool rows' own stats are derived from (toolstat.go), so the Σ segment
	// and the per-turn "what changed" lines can never disagree.
	adds, dels int
}

// hudStats answers both, from the cache when the calls have not moved.
func (a *app) hudStats() hudStats {
	if !a.hudStale {
		return a.hud
	}
	a.hud, a.hudStale = a.computeStats(), false
	return a.hud
}

func (a *app) computeStats() hudStats {
	var out hudStats
	// live holds the ids of the background jobs this surface watched start, so a
	// kill can take away the one it names rather than the newest.
	var live []string
	for i := range a.entries {
		e := &a.entries[i]
		if e.kind != entryTool || e.status != toolOK {
			continue
		}
		fields := argsOf(e.detail.Args)
		switch e.tool {
		case "edit":
			adds, dels := editStat(e.detail.Args)
			out.adds, out.dels = out.adds+adds, out.dels+dels
		case "write":
			out.adds += lineCount(argString(fields, "content"))
		case "bash":
			if argString(fields, "background") != "true" {
				continue
			}
			out.jobs++
			live = append(live, jobID(e.detail.Output))
		case "watch":
			out.watches++
		case "jobs":
			if argString(fields, "action") != "kill" {
				continue
			}
			id := argString(fields, "id")
			if at := indexOf(live, id); id != "" && at >= 0 {
				live = append(live[:at], live[at+1:]...)
				out.jobs--
				continue
			}
			// An id this surface never saw start is a watch's — watches are
			// jobs too (kind watch) and their start line publishes no id — and
			// failing that it is a job from before we were looking.
			if out.watches > 0 {
				out.watches--
				continue
			}
			if out.jobs > 0 {
				out.jobs--
			}
		}
	}
	return out
}

// jobID reads the id out of a background bash call's own answer, which session
// spells "job 3 started; log at …". Empty when it said something else.
func jobID(output string) string {
	fields := strings.Fields(strings.TrimSpace(output))
	if len(fields) < 2 || fields[0] != "job" {
		return ""
	}
	return strings.TrimSuffix(fields[1], ";")
}

func indexOf(values []string, want string) int {
	for i, value := range values {
		if value == want {
			return i
		}
	}
	return -1
}

// ── THE REPOSITORY ──────────────────────────────────────────────────────────

// gitTimeout is how long the legend will wait for a repository to answer.
//
// It is short because the answer is FURNITURE: a branch name in a border is
// worth a quarter of a second of a background goroutine and not one frame of
// the surface. A tree so large that `git status` cannot answer in that time
// draws no branch, which is exactly what a directory that is not a repository
// draws — and neither of them makes the person wait.
const gitTimeout = 400 * time.Millisecond

// probeGit asks the workspace what it is, off the model loop.
func (a *app) probeGit() tea.Cmd {
	dir, probe := a.workspace, a.gitProbe
	if dir == "" || probe == nil {
		return nil
	}
	return func() tea.Msg {
		branch, dirty, ok := probe(dir)
		return gitMsg{branch: branch, dirty: dirty, ok: ok}
	}
}

// gitHead is the default probe: the branch, and whether the tree is dirty.
//
// Two commands rather than one because they answer two questions and only the
// first one is cheap. A detached HEAD answers "HEAD", which is reported as it
// is — it is the truth about where the work is going, and inventing a short sha
// here would be this surface deciding what a repository means.
//
// `--no-optional-locks` is the one flag that matters: `git status` normally
// refreshes the index, which takes a write lock, and a status bar must never be
// the reason a person's own `git commit` in the next pane blocks.
func gitHead(dir string) (string, bool, bool) {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", false, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	run := func(args ...string) (string, bool) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			return "", false
		}
		return strings.TrimSpace(string(out)), true
	}
	branch, ok := run("rev-parse", "--abbrev-ref", "HEAD")
	if !ok || branch == "" {
		return "", false, false
	}
	// An unreadable status is reported as CLEAN rather than dirty: the star is a
	// claim, and a claim this surface could not check is one it should not make.
	status, _ := run("--no-optional-locks", "status", "--porcelain", "--untracked-files=no")
	return branch, status != "", true
}

// readApproval is the gate's posture, or "" where there is no profile to ask.
//
// The empty answer is deliberately not [config.DefaultToolApprovalMode]: a
// surface booted without a profile (every test, and any embedding that wires
// its own policy) has not been told the gate is open, and the YOLO segment's
// whole law is that it appears only when somebody said so.
func readApproval(profileDir string) string {
	if strings.TrimSpace(profileDir) == "" {
		return ""
	}
	return config.ToolApprovalModeAt(profileDir)
}

func errText(err error) string {
	if err == nil {
		return "unknown"
	}
	return err.Error()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// dollars formats a running cost the way the status line wants it: cents while
// the session is cheap, so a first turn is not rendered as $0.00.
func dollars(usd float64) string {
	switch {
	case usd <= 0:
		return "$0.00"
	case usd < 0.01:
		return fmt.Sprintf("$%.4f", usd)
	default:
		return fmt.Sprintf("$%.2f", usd)
	}
}

// savedWord formats what a cache read was worth, and it is deliberately NOT
// [dollars]: the savings note is a dim aside, the amount beside it is a fraction
// of a cent for most turns, and "$0.0041" spends three cells on a zero and a
// point that carry nothing. The leading zero goes; the figure does not.
//
// Four places below a dollar and two above, because those are the two scales the
// number actually lives at — a turn saves thousandths, a long session saves
// dollars, and nothing useful sits between them.
func savedWord(usd float64) string {
	if usd >= 1 {
		return fmt.Sprintf("$%.2f", usd)
	}
	return fmt.Sprintf("$%.4f", usd)
}

// tokenWord is a token count at a glance: "842", "12.4k", "1.2M". One
// significant decimal and no more — the meter is read in passing, and a figure
// that changes in its fourth digit every step is a figure nobody can read.
//
// The trailing ".0" is dropped so a round number is round: a 128k window is
// "128k" and never "128.0k".
func tokenWord(tokens int) string {
	switch {
	case tokens <= 0:
		return "0"
	case tokens < 1000:
		return strconv.Itoa(tokens)
	// 999_950 and not 1_000_000: one decimal rounds anything above it to
	// "1000.0k", which is a figure with the wrong unit on it.
	case tokens < 999_950:
		return trimUnit(float64(tokens)/1000, "k")
	default:
		return trimUnit(float64(tokens)/1_000_000, "M")
	}
}

func trimUnit(value float64, unit string) string {
	return strings.TrimSuffix(strconv.FormatFloat(value, 'f', 1, 64), ".0") + unit
}
