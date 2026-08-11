package chat

import (
	"context"
	"io"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/composer"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The assembly: the shell, the block engine, the token layer and the real
// engine, wired into one chat.
//
// Everything below is plumbing between seams that already exist. Nothing here
// re-implements a renderer, a store read or a head loop — the app is a lens
// (Decision 7), and a lens that grew logic of its own would be a second engine
// with a second set of bugs.
//
// Four facts about the shape, because they are the ones that keep it honest:
//
//   - The transcript is the durable thread plus at most one live turn at its
//     tail. Every settled row came out of the journal; nothing on screen above
//     the live seam was invented by this file.
//   - The poll asks the cheapest question first. LatestEventSeq is one indexed
//     row, and an unchanged answer ends the cycle without reading a message —
//     so an idle window costs one row per tick and no allocation.
//   - The animation clock ticks only while a turn is live. A settled window
//     draws nothing, wakes for nothing, and sends no bytes.
//   - Stream events are keyed by session and filtered here. A delta for another
//     room is dropped, not drawn; the day a second room exists, its tokens
//     render there and not into whichever window happens to be open.

const (
	// messagePage bounds one poll's thread read. It is generous because the
	// first poll of a resumed session reads the whole conversation and a reader
	// scrolling back expects to find it.
	messagePage = 500

	// defaultPollEvery is the floor between two store reads. The watermark
	// question is cheap, but it is not free, and nothing a human does needs an
	// answer faster than this.
	defaultPollEvery = 300 * time.Millisecond
)

// Options is everything the app needs from the process that built the engine.
type Options struct {
	// Backend is the durable store. Nil is a surface with nothing to read,
	// which is a legitimate state and draws an empty thread rather than failing.
	Backend Backend
	// Commander is the live engine. Nil degrades: the awaiting line stops
	// advertising an interrupt that would do nothing.
	Commander Commander
	// Session is the room this window is looking at. Every read is scoped to
	// it and every stream event is filtered by it.
	Session string
	// Database is the path the status line reports.
	Database string
	// Events is the keyed stream feed. Nil means no live tokens: replies land
	// whole, at the poll, which is exactly what a visitor window sees.
	Events <-chan StreamEvent
	// Profile is the terminal's colour vocabulary, already decided by the entry
	// point (tokens.DetectProfile or an explicit ParseProfile).
	Profile tokens.Profile
	// Linear selects the accessible rendering (10.1.5): one column, no motion.
	Linear bool

	// Now and PollEvery exist so the app can be driven without a wall clock or
	// a real cadence. Tests set both; nothing else does.
	Now       func() time.Time
	PollEvery time.Duration

	// Input and Output let the surface boot without a terminal.
	Input  io.Reader
	Output io.Writer
}

// liveTurn is the one turn this room is watching being answered. It is a value
// on the app rather than a set of parallel fields because "is a turn live" is
// one question, and answering it from four booleans is how the old surface grew
// a state machine nobody could hold in their head.
type liveTurn struct {
	// active says a turn is being answered in THIS room. It is the whole of
	// what esc consults (8.2.21).
	active bool
	// reply is the streamed text, created on the first delta that decodes to
	// something a reader can be shown.
	reply *blocks.TextBlock
	// await is the awaiting line, present for as long as the turn is.
	await *awaitingBlock
	// raw is the provider's bytes as they arrive; shown is what has been drawn.
	raw   strings.Builder
	shown string
	// since is the journal position the turn started from. The durable reply
	// that ends it is the first agent line above this.
	since int64
}

// App is the v2 chat: one Bubble Tea v2 program wrapping the shell.
//
// It wraps rather than extends because the two have different jobs. The shell
// owns geometry, focus, capabilities and the frame; the app owns the
// conversation — what is in the transcript, when the store is read, what a key
// means. Every message the app does not claim is handed down unchanged.
type App struct {
	shell     *tui2.Shell
	backend   Backend
	commander Commander
	session   string
	events    <-chan StreamEvent
	now       func() time.Time
	pollEvery time.Duration
	linear    bool

	style      *tokens.Styler
	transcript *blocks.Transcript
	pane       *transcriptPane
	status     *statusPane
	rail       *railPane
	composer   composerPane

	// The poll chain. There is exactly one: a read in flight sets polling, a
	// request that arrives while it is set becomes a poke the result honours,
	// and the result arms the next tick. Two chains is how a window ends up
	// reading the store several times per cadence for nothing.
	journal   int64
	watermark int64
	polling   bool
	poke      bool

	// The animation chain, armed only while a turn is live.
	ticking bool

	turn liveTurn

	// drafts submitted by the composer during a Key call, drained into commands
	// once it returns. A callback cannot return a tea.Cmd, so it queues one.
	pending []string
}

var _ tea.Model = (*App)(nil)

// Metrics is the responsive numbers table (10.5.24), filled from the token
// layer's breakpoints rather than from the shell's provisional defaults. This
// is the seam tui2.Options.Metrics was declared for.
func Metrics() tui2.Metrics {
	return tui2.Metrics{
		RailBreakpoint:         tokens.RailAtWidth,
		RailWidth:              tokens.RailWidth,
		MinMainWidth:           tokens.RailTranscriptFloor,
		ComposerHeight:         3,
		StatusHeight:           1,
		DialogFullscreenBelow:  tokens.DialogFullscreenBelowWidth,
		SplitDiffBreakpoint:    tokens.SplitDiffAtWidth,
		PasteToAttachmentLines: tokens.PasteAttachRows,
	}
}

// New builds the app and binds every pane into the shell.
func New(opts Options) *App {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	every := opts.PollEvery
	if every <= 0 {
		every = defaultPollEvery
	}

	app := &App{
		backend:   opts.Backend,
		commander: opts.Commander,
		session:   strings.TrimSpace(opts.Session),
		events:    opts.Events,
		now:       now,
		pollEvery: every,
		linear:    opts.Linear,
		// The token layer implements both of blocks' seams directly, so the
		// transcript is styled by handing it one of these and nothing else.
		style:      tokens.NewStyler(opts.Profile, tokens.FocusNormal),
		transcript: blocks.New(80, 24),
	}

	metrics := Metrics()
	app.shell = tui2.NewShell(tui2.Options{
		Metrics: &metrics,
		Linear:  opts.Linear,
		DB:      opts.Database,
		Session: app.session,
	})

	app.pane = &transcriptPane{
		transcript: app.transcript,
		now:        now,
		invalidate: app.shell.Invalidate,
	}
	app.status = &statusPane{style: app.style, session: app.session}
	app.rail = &railPane{style: app.style, session: app.session}
	app.composer = newComposer(composerOptions{
		OnSubmit:    func(text string) { app.pending = append(app.pending, text) },
		Styler:      app.style,
		SendKey:     tui2.SendKey,
		NewlineKeys: app.shell.Capabilities().NewlineKeys(),
	})

	app.shell.SetPane(tui2.LayerTranscript, app.pane)
	app.shell.SetPane(tui2.LayerComposer, app.composer)
	app.shell.SetPane(tui2.LayerStatus, app.status)
	app.shell.SetPane(tui2.LayerRail, app.rail)
	app.composer.Focus(true)

	if app.commander != nil {
		app.status.model = strings.TrimSpace(app.commander.CurrentModel("chat"))
	}
	return app
}

// Run opens the surface and blocks until it closes.
func Run(ctx context.Context, opts Options) error {
	program := []tea.ProgramOption{}
	if ctx != nil {
		program = append(program, tea.WithContext(ctx))
	}
	if opts.Input != nil {
		program = append(program, tea.WithInput(opts.Input))
	}
	if opts.Output != nil {
		program = append(program, tea.WithOutput(opts.Output))
	}
	_, err := tea.NewProgram(New(opts), program...).Run()
	return err
}

// Init negotiates with the terminal, reads the thread once, and starts
// listening for tokens.
func (a *App) Init() tea.Cmd {
	return tea.Batch(a.shell.Init(), a.startPoll(), a.waitStream())
}

// View is the shell's. The app never draws.
func (a *App) View() tea.View { return a.shell.View() }

// Frame renders one frame at a size with no terminal involved — the door the
// golden harness and the tests drive.
func (a *App) Frame(width, height int) string { return a.shell.Frame(width, height) }

// Update folds a message into the app, handing down everything it does not
// claim.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return a, a.drain(a.key(msg))

	case pollTickMsg:
		return a, a.startPoll()

	case pollResultMsg:
		a.applyPoll(msg)
		return a, a.afterPoll()

	case postResultMsg:
		a.applyPost(msg)
		return a, tea.Batch(a.startPoll(), a.startTick())

	case streamBatchMsg:
		changed := false
		for _, event := range msg.events {
			changed = a.applyStream(event) || changed
		}
		if changed {
			a.refresh()
		}
		return a, tea.Batch(a.waitStream(), a.startTick())

	case streamClosedMsg:
		// The feed is gone: the resident stood down, or the window was never a
		// resident. Replies still land at the poll — they are simply no longer
		// drawn a token at a time.
		a.events = nil
		if a.turn.active {
			a.turn.await.interruptible = false
			a.refresh()
		}
		return a, nil

	case animTickMsg:
		a.ticking = false
		if a.turn.active {
			a.shell.Invalidate()
		}
		return a, a.startTick()

	case composer.EscMsg:
		// The composer had nothing left to protect, so it handed the key back
		// rather than swallowing it. This side is the only one that knows what
		// it means (8.2.21).
		return a, a.navigate()
	}

	if handled, cmd := a.paste(msg); handled {
		return a, cmd
	}
	model, cmd := a.shell.Update(msg)
	_ = model
	return a, cmd
}

// drain turns any drafts the composer submitted during a keystroke into
// commands. The callback could not return one, so it queued the text instead.
func (a *App) drain(cmd tea.Cmd) tea.Cmd {
	if len(a.pending) == 0 {
		return cmd
	}
	cmds := make([]tea.Cmd, 0, len(a.pending)+1)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}
	for _, text := range a.pending {
		cmds = append(cmds, a.postCmd(text))
	}
	a.pending = a.pending[:0]
	a.shell.Invalidate()
	return tea.Batch(cmds...)
}

// key is the keyboard ladder.
//
// The composer holds the keyboard in a chat surface — you talk to what you are
// looking at, and what you are looking at is the conversation — so everything
// that is not a scroll gesture, an interrupt or a quit goes to it. Routing is
// decided here rather than by the shell's focus because focus follows clicks,
// and a chat where clicking the transcript stopped you being able to type would
// be a chat nobody could use.
func (a *App) key(msg tea.KeyPressMsg) tea.Cmd {
	switch key := msg.String(); key {
	case "ctrl+c":
		return tea.Quit

	case "ctrl+o":
		// The shell owns the scope gesture; Wave 3 gives it something to show.
		_, cmd := a.shell.Update(msg)
		return cmd

	case "esc":
		// 8.2.21, the reconciling rule: esc acts on what you are watching. A
		// turn streaming in THIS room is interrupted; anything else is the
		// composer's to protect and then the app's to interpret. The draft is
		// untouched either way — interrupting a turn is not an edit.
		if a.canInterrupt() {
			return a.interrupt()
		}

	case "pgup", "pgdown", "shift+up", "shift+down", "ctrl+home", "ctrl+end":
		return a.pane.Key(msg)
	}

	if a.composer == nil {
		return nil
	}
	return a.composer.Key(msg)
}

// paste routes bracketed paste to a composer that accepts it. The shell has no
// paste seam yet, so the capability is asked for rather than assumed: a
// composer without the method simply never sees a paste, and a terminal without
// bracketed paste delivers the same text as keystrokes.
func (a *App) paste(msg tea.Msg) (bool, tea.Cmd) {
	pasted, ok := msg.(tea.PasteMsg)
	if !ok || a.composer == nil {
		return false, nil
	}
	sink, ok := a.composer.(interface {
		Paste(tea.PasteMsg) tea.Cmd
	})
	if !ok {
		return false, nil
	}
	cmd := sink.Paste(pasted)
	a.shell.Invalidate()
	return true, cmd
}

// canInterrupt reports whether esc would in fact stop a turn — which is the
// exact condition under which the awaiting line is allowed to say so.
func (a *App) canInterrupt() bool {
	return a.turn.active && a.commander != nil && a.turn.await != nil &&
		a.turn.await.interruptible
}

// interrupt stops the turn being watched, carrying in the words already on
// screen so the durable line that ends it is the same reply, marked where it
// stopped. The mark itself is the engine's to journal (store.InterruptedEnd)
// and arrives at the next poll as a message part; this surface does not invent
// it, which is what keeps the record and the screen the same thing.
func (a *App) interrupt() tea.Cmd {
	a.commander.Interrupt(a.turn.shown)
	a.turn.await.phase = "stopping"
	a.turn.await.interruptible = false
	a.refresh()
	return a.startPoll()
}

// navigate is what esc means when there is no turn to stop and no draft to
// protect: return to the live edge. It never destroys anything.
func (a *App) navigate() tea.Cmd {
	if a.transcript.AtBottom() {
		return nil
	}
	a.transcript.GotoBottom()
	a.shell.Invalidate()
	return nil
}

// refresh repaints and keeps the status line's summary current.
func (a *App) refresh() {
	a.status.live = a.turn.active
	a.shell.Invalidate()
}
