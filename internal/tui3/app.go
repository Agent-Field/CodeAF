package tui3

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

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
	entryNote
	// entryThinking is one turn's reasoning (thinking.go): streamed while it
	// arrives, collapsed to a single row the moment the turn says anything else.
	entryThinking
)

// toolState is the mark on a tool line.
type toolState int

const (
	toolRunning toolState = iota
	toolOK
	toolFailed
)

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

	// Thinking fields (thinking.go): when the first and last reasoning delta of
	// this block arrived. settled collapses the block and open expands it again.
	began, ended time.Time

	// Assistant fields. settled means the block is finished and renders
	// through renderMarkdown; mdCut is how many bytes of a still-streaming
	// block have already been promoted to markdown.
	settled bool
	mdCut   int

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

	state runState
	model string
	// title is the name the session gave itself, shown left of the model. Empty
	// until the session has one (session's title.go names it after the first
	// completed turn); a resumed session opens with the name it already had.
	title  string
	cost   float64
	tokens int
	// ctxWindow is the model's context in tokens as this surface last set it,
	// and ctxBytes what the conversation currently weighs. The pair is the
	// meter in the status line. The window is TRACKED rather than asked for
	// because session exposes no getter — the surface is the one that tells the
	// agent (see [app.switchModel]), so the surface is the one that knows.
	ctxWindow int
	ctxBytes  int

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
	// think is the reasoning block currently streaming, or -1 (thinking.go).
	think int

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
}

func newApp(ctx context.Context, opts Options) *app {
	place := strings.TrimSpace(opts.Workspace)
	if place == "" {
		if cwd, err := os.Getwd(); err == nil {
			place = cwd
		}
	}
	a := &app{
		ctx:       ctx,
		agent:     opts.Agent,
		fresh:     opts.Fresh,
		workspace: place,
		place:     filepath.Base(place),
		file:      opts.SessionFile,
		resumed:   opts.Resumed,
		models:    opts.Models,
		history:   opts.History,
		draftFile: opts.DraftFile,
		ctxWindow: opts.ContextWindow,
		live:      -1,
		sel:       -1,
		think:     -1,
		unfolded:  map[int]bool{},
		stick:     true,
		width:     80,
		height:    24,
		pal:       detectPalette(),
	}
	if a.agent != nil {
		a.model = a.agent.Model()
		// A resumed session is already named, and the name is a fact about the
		// conversation on screen: it belongs in the first frame, not after the
		// next turn (session's title.go re-names nothing).
		a.title = strings.TrimSpace(a.agent.Title())
	}
	// The conversation that already happened is drawn BEFORE the surface says
	// anything of its own, so the notices below land where a person's eye
	// already is: at the bottom, next to the box.
	a.replay()
	a.measureContext()
	if notice := strings.TrimSpace(opts.Notice); notice != "" {
		a.note(notice)
	}
	if a.resumed && a.file != "" {
		a.note("resumed " + a.file)
	}
	a.note("/help for commands · esc or ctrl+c interrupts · ctrl+o expands tool calls")
	a.restoreDraft()
	return a
}

var _ tea.Model = (*app)(nil)

func (a *app) Init() tea.Cmd { return nil }

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
		return a, a.key(msg)

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
		switch msg.Mouse().Button {
		case tea.MouseWheelUp:
			a.scroll(-3)
		case tea.MouseWheelDown:
			a.scroll(3)
		}
		return a, nil

	case tea.MouseClickMsg:
		if msg.Mouse().Button == tea.MouseLeft {
			a.press(msg.Mouse().Y)
		}
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
		a.settle()
		// The turn is over, so a message that was waiting for it starts now
		// (followup.go). Nil when nothing is queued.
		return a, a.startFollow()

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
	if a.state == stateWorking {
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
	if msg.err != nil {
		a.note("submit failed: " + msg.err.Error())
		a.settle()
		return nil
	}
	if msg.ch == nil || a.stream != nil {
		return nil
	}
	a.gen++
	a.stream = msg.ch
	a.state = stateWorking
	a.lastDelta = time.Now()
	return tea.Batch(waitEvent(msg.ch, a.gen), a.wake())
}

// event folds one session event into the conversation.
//
// Text deltas mark the live entry stale and stop there: they are the flood, and
// the clock decides when a flood becomes a frame. Everything else is discrete
// and paints at once — a tool beginning is a fact a person is waiting for.
func (a *app) event(ev session.Event) tea.Cmd {
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
		a.askConsent(ev)

	case session.EventTitleChanged:
		a.setTitle(ev.Text)

	case session.EventToolBegin:
		a.closeLive()
		a.entries = append(a.entries, entry{
			kind: entryTool, tool: ev.Tool, text: ev.Hint, turn: a.turn,
			status: toolRunning, detail: toolDetail{Args: ev.Args},
		})
		a.follow()
		a.touch()

	case session.EventToolEnd:
		a.closeTool(ev, toolOK, "")

	case session.EventToolFailed:
		a.closeTool(ev, toolFailed, firstNonEmpty(ev.Hint, errText(ev.Err)))

	case session.EventCompacted:
		a.closeLive()
		a.entries = append(a.entries, entry{
			kind: entryDivider, text: firstNonEmpty(ev.Hint, "compacted"), turn: a.turn,
		})
		a.follow()
		a.touch()

	case session.EventTurnDone:
		a.take(ev.Usage)
		a.settle()

	case session.EventError:
		a.note("error: " + errText(ev.Err))
		a.take(ev.Usage)
		a.settle()
	}
	if a.stream == nil {
		return nil
	}
	// The clock is normally already running (Submit started it), but a stream
	// that outlives its turn's state would otherwise stream into a frame
	// nobody built. Batch drops a nil cmd, so this costs nothing when the
	// clock is up.
	return tea.Batch(a.wake(), waitEvent(a.stream, a.gen))
}

// settle ends a turn: the stream is done or abandoned, nothing is live, and the
// state word goes back to idle unless the person interrupted it — an interrupt
// is a fact about the turn that ended and stays on screen until the next one
// starts. The reply it was writing becomes markdown here.
func (a *app) settle() {
	a.closeLive()
	// A turn that streamed nothing but reasoning still ends with a block, and a
	// block left open would keep a finished thought expanded over the next turn.
	a.collapseThought()
	// Questions the turn was blocked on died with it. The session already
	// released those calls; a prompt left on screen would be asking about work
	// that is over (consent.go).
	a.dropAsks()
	if a.state == stateWorking {
		a.state = stateIdle
	}
	a.refreshUsage()
	a.measureContext()
	a.follow()
	a.touch()
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

func (a *app) take(u session.Usage) {
	if u.CostUSD > a.cost {
		a.cost = u.CostUSD
	}
	if n := u.Input + u.Output; n > a.tokens {
		a.tokens = n
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
		if e.kind != entryTool || e.status != toolRunning || e.tool != ev.Tool {
			continue
		}
		e.status = status
		e.detail.Args = firstNonEmpty(ev.Args, e.detail.Args)
		e.detail.Output = firstNonEmpty(ev.Output, why)
		if why != "" && status == toolFailed {
			e.text = strings.TrimSpace(e.text + " — " + why)
		}
		a.follow()
		a.touch()
		return
	}
	// A close with no open line still deserves to be seen rather than
	// silently dropped: the session said something happened.
	if status == toolFailed {
		a.entries = append(a.entries, entry{
			kind: entryTool, tool: ev.Tool, text: why, turn: a.turn, status: toolFailed,
			detail: toolDetail{Args: ev.Args, Output: firstNonEmpty(ev.Output, why)},
		})
		a.follow()
		a.touch()
	}
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
	a.follow()
	a.touch()
	return tea.Batch(func() tea.Msg {
		ch, err := agent.Submit(ctx, text)
		return submittedMsg{ch: ch, err: err}
	}, a.wake())
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

// running reports whether any call of the current turn is still spinning.
func (a *app) running() bool {
	for i := range a.entries {
		e := &a.entries[i]
		if e.kind == entryTool && e.status == toolRunning && e.turn == a.turn {
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
func (a *app) openTool(i int) {
	if i < 0 || i >= len(a.entries) || a.entries[i].kind != entryTool {
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
	}
}

// selectTool moves the selection through the tool calls that are actually on
// the row list — the folded ones are not selectable, because selecting a row
// nobody can see is a cursor that has vanished. Walking off either end returns
// false, and the key that asked falls through to scrolling.
func (a *app) selectTool(delta int) bool {
	rows := a.visible(a.width)
	var calls []int
	for _, r := range rows {
		if r.hit == hitTool && (len(calls) == 0 || calls[len(calls)-1] != r.entry) {
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
	if at < 0 || at >= len(calls) {
		a.sel = -1
		a.touch()
		return false
	}
	a.sel = calls[at]
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

	case "compact":
		agent, ctx := a.agent, a.ctx
		a.note("compacting…")
		return func() tea.Msg { return compactedMsg{err: agent.Compact(ctx)} }

	case "new":
		a.renew()
		return nil

	default:
		a.note("unknown command: /" + name + " · try /help")
		return nil
	}
}

// renew closes this conversation and opens the next one on the same config.
// The transcript is cleared because it belongs to the agent that just closed:
// a fresh session file with the old conversation still on screen would be the
// surface claiming context the model does not have.
func (a *app) renew() {
	if a.fresh == nil {
		a.note("/new is unavailable here")
		return
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
		return
	}
	a.agent, a.file = agent, file
	a.entries = nil
	a.live, a.sel, a.think = -1, -1, -1
	a.asks, a.follows = nil, nil
	a.title = strings.TrimSpace(agent.Title())
	a.turn = 0
	a.unfolded = map[int]bool{}
	a.stream = nil
	a.gen++
	a.state = stateIdle
	a.cost, a.tokens, a.ctxBytes = 0, 0, 0
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
	// The model overlay is modal for the keyboard, so it is modal for the
	// clipboard: a paste while it is up is a filter somebody copied.
	if a.pick.open {
		a.pick.filter.insert(strings.ReplaceAll(text, "\n", " "))
		a.pick.rank()
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

// bytesPerToken is the estimator behind the meter: ~4 bytes to the token for
// code and English prose. It is INTERIM — the honest figure is the provider's
// own prompt-token count, which arrives with a turn's usage and not before it —
// and it is the same figure internal/session sizes compaction with, so the
// meter and the compaction it predicts cannot disagree with each other.
const bytesPerToken = 4

// measureContext asks the agent what the conversation now weighs. It is called
// where the answer CHANGES — a turn ending, a session opening or being replaced
// — and never on the frame clock: Transcript takes the session's lock and
// copies the whole conversation, and doing that thirty times a second to move a
// figure that changes once a turn is a lock taken for nothing.
func (a *app) measureContext() {
	if a.agent == nil {
		return
	}
	a.ctxBytes = contextBytes(a.agent.Transcript())
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
// estimated to be using. False when nobody has said what the window is, because
// a percentage of an unknown is a number that means nothing.
func (a *app) ctxPercent() (int, bool) {
	if a.ctxWindow <= 0 || a.ctxBytes <= 0 {
		return 0, false
	}
	return a.ctxBytes / bytesPerToken * 100 / a.ctxWindow, true
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
