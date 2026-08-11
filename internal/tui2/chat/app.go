package chat

import (
	"context"
	"io"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/composer"
	"github.com/Agent-Field/aforge-v2/internal/tui2/footer"
	"github.com/Agent-Field/aforge-v2/internal/tui2/homes"
	"github.com/Agent-Field/aforge-v2/internal/tui2/modelui"
	"github.com/Agent-Field/aforge-v2/internal/tui2/palette"
	"github.com/Agent-Field/aforge-v2/internal/tui2/placeline"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/settings"
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
	// Database is the path this window's journal lives at.
	Database string
	// Root is the material ground this room's work lands on — the resident
	// root of 5.19, and the only leg the place line has while one room exists.
	// Empty asks the process for its working directory, which is the same
	// answer a shell prompt would have given for free.
	Root string
	// Home folds the root into its "~" form before the place line abbreviates
	// it. Empty asks the process.
	Home string
	// Events is the keyed stream feed. Nil means no live tokens: replies land
	// whole, at the poll, which is exactly what a visitor window sees.
	Events <-chan StreamEvent
	// Residents is how this window finds out what it is. Nil means the caller
	// is not playing the residency game at all, and the window renders as the
	// resident it must then be. See [Residents].
	Residents Residents
	// Profile is the terminal's colour vocabulary, already decided by the entry
	// point (tokens.DetectProfile or an explicit ParseProfile).
	Profile tokens.Profile
	// Linear selects the accessible rendering (10.1.5): one column, no motion.
	Linear bool
	// GlyphSet is the glyph repertoire tier (12.7), decided by the entry point.
	// The zero value is tokens.Plain, so an unset field renders the 5.17 floor.
	GlyphSet tokens.GlyphSet

	// Now and PollEvery exist so the app can be driven without a wall clock or
	// a real cadence. Tests set both; nothing else does.
	Now       func() time.Time
	PollEvery time.Duration

	// Input and Output let the surface boot without a terminal.
	Input  io.Reader
	Output io.Writer
}

// Residency is what this window currently IS: the process running the head, or
// a surface attached to a head that another process is running.
//
// The lease is one file per store directory (internal/lease), so a second
// aforge on the same journal — a second chat window, or a headless `aforge do`
// holding the role for the length of a job — makes this window a visitor. That
// is an ordinary state and a legitimate one; what is not legitimate is the
// window not saying so. A visitor cannot start a turn and cannot stop one, and
// a surface that kept advertising "esc interrupt" over a head it does not run
// would be failing 5.20 rule 3 in the most expensive way there is: the reader
// presses the key, nothing happens, and the affordance has lied.
type Residency struct {
	// Visitor says the head is somewhere else. The zero value is the ordinary
	// single-window journey.
	Visitor bool
	// PID names the process that does hold the role, when it is known.
	PID int
	// Note replaces the label while something is in motion — a promotion under
	// way, a handover asked for. A short phrase, never a sentence.
	Note string
}

// Residents is the door the entry point wires so a window can find out what it
// is, and can stop being wrong about it.
//
// It is asked on every poll cycle, quiet ones included, for the reason v1
// states at internal/tui/model.go's own Residency: the resident can die while
// nothing at all changes in the store, so the journal watermark cannot speak
// for the role. The implementation is expected to throttle its own probing —
// the surface asks often because the surface cannot know when to ask.
type Residents interface {
	// Residency reports the window's role right now, and hands back a
	// replacement engine EXACTLY ONCE, on the cycle the role moves. Returning
	// the same commander twice would make a window rebuild itself for nothing.
	Residency() (Residency, Commander)
}

// residencyMsg is one answer from the residency probe, folded in on the render
// goroutine like every other fact.
type residencyMsg struct {
	state Residency
	adopt Commander
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
	// something a reader can be shown. label is the header row above it: the
	// two are one preview and are split only so the header can sit flush left
	// while the prose sits at the depth its journaled twin will have. See
	// newReplyBlock.
	reply *blocks.TextBlock
	label *blocks.TextBlock
	// await is the awaiting line, present for as long as the turn is.
	await *awaitingBlock
	// raw is the provider's bytes as they arrive; shown is what has been drawn.
	raw   strings.Builder
	shown string
	// since is the journal position the turn started from. The durable reply
	// that ends it is the first agent line above this.
	since int64
	// stopped says the person ended this turn. It outranks whatever the
	// provider says next, because a call cancelled on purpose reports itself as
	// a failure and it is not one: an interrupt is chrome, never coral (5.16),
	// and a surface that answered esc with "stream lost" would be blaming the
	// reader for pressing the key it advertised.
	stopped bool
	// started is when this room began waiting, for the composer meta strip's
	// elapsed cell. It is stamped by refresh rather than by the turn's opening
	// so the one clock the app was given is the only clock it reads.
	started time.Time
	// voiced records that the streamed preview has been given the same header
	// voice the durable reply will wear, so the label does not change tier
	// under the reader at the moment the turn settles.
	voiced bool
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
	place      *placeline.Model
	meta       *metaStrip
	composer   composerPane
	verbs      []registry.Entry

	// The scope map (5.15). source is the adapter onto the store, railModel is
	// the one scope model, railView paints it, and scope is its place in the
	// shell. railFocus says the map holds the keyboard; termWidth is the
	// terminal's own width, which is what decides the rendering — a pane is only
	// ever told its own rectangle, and the rail's column is not the terminal.
	source    *scopeSource
	railModel *rail.Model
	railView  *rail.View
	scope     *scopePane
	// hudModel and hudView are the bounded summary's own pair (8.2.8). They are
	// separate from the rail's because the HUD shows a different scope — the
	// work only, never the navigation — and because a shared View's line buffer
	// is valid only until its next render, and both draw in the same frame.
	hudModel *rail.Model
	hudView  *rail.View

	railFocus  bool
	scopeOpen  bool
	termWidth  int
	termHeight int

	// The overlay plane (5.22): one door at a time, each built on first use so
	// a window that never presses ctrl+k never pays for a palette.
	// The four homes of 5.24, as rail scopes with one detail renderer. The lid's
	// own state lives on the scope source beside the rows it decides.
	homesView  *homes.View
	homesSel   homes.Selection
	homesPane  func(width, height int) []string
	overlay    overlayKind
	palette    *palette.Palette
	capability *palette.Capability
	settings   *settings.Model
	models     *modelui.Picker

	// view is the main pane's current lens: nil is the room's own conversation,
	// anything else is a task room or the card a cursor move previewed.
	view *mainView
	// composerBind is what the composer is talking to (5.15's one rule).
	composerBind composerBind

	// The residency chain. residents is the door, residency is the last answer
	// it gave, and probing keeps exactly one question in flight — the same
	// discipline the poll chain keeps, for the same reason.
	residents Residents
	residency Residency
	probing   bool

	// receiptsOpen is the fold state every collapsible row shares. Receipts are
	// a class of row, not a set of independent widgets: a reader who wants to
	// see what the system has been doing wants to see all of it, and a per-row
	// fold would make that N keystrokes.
	receiptsOpen bool
	// foldable says the transcript holds at least one row the fold can act on.
	// The footer offers the accelerator only while that is true: 5.20 rule 3
	// forbids advertising a door that opens nothing, and the cells it saves are
	// cells the health column keeps — 10.5.22's drop order puts verbs above
	// health, so an idle verb costs a real notice at 80 columns.
	foldable bool

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
	// dispatches is the same queue for the `@` grammar's addressed sends (5.18),
	// kept apart because the two carry different facts and merging them would
	// mean re-deriving the target from the text the composer already parsed.
	// sends is the third queue: a draft that captured files (5.11, attach.go).
	// It is kept apart for the reason dispatches are — the composer already
	// resolved which tokens were paths and took them out of the text, and
	// re-deriving that here from a string would be this side parsing the draft a
	// second time and getting a different answer.
	pending    []string
	dispatches []composer.Dispatch
	sends      []composer.Send

	// hooks is the terminal-protocol queue (10.5.27, shell.go's contract). The
	// shell's hooks return bytes as commands and nothing writes them but the
	// runtime, so a hook raised inside the poll chain waits here until Update
	// hands it back. See Update.
	hooks []tea.Cmd
	// notify is the shell's Notify behind a field, so the interruption budget's
	// three occasions can be asserted. See raiseNotice.
	notify func(tui2.AttentionKind, string, string) tea.Cmd
	// lives is the last lifecycle this window saw for each task, so a delivery
	// and a failure can be noticed as the TRANSITIONS they are. A notification
	// keyed off a state rather than off a change would fire on every poll for as
	// long as the state lasted, which is the interruption budget spent on one
	// event forever.
	lives map[string]rail.Lifecycle
}

var _ tea.Model = (*App)(nil)

// Metrics is the responsive numbers table (10.5.24), filled from the token
// layer's breakpoints rather than from the shell's provisional defaults. This
// is the seam tui2.Options.Metrics was declared for.
func Metrics() tui2.Metrics {
	return tui2.Metrics{
		RailBreakpoint: tokens.RailAtWidth,
		RailWidth:      tokens.RailWidth,
		MinMainWidth:   tokens.RailTranscriptFloor,
		// Four rows: the place line on the top edge (5.19), two rows of draft,
		// and the meta strip on the bottom (10.5.23). The draft gets two
		// because one is a text field and two is a composer — a pasted command
		// or a second sentence has somewhere to be without the region moving.
		ComposerHeight:              4,
		StatusHeight:                1,
		DialogFullscreenBelowWidth:  tokens.DialogFullscreenBelowWidth,
		DialogFullscreenBelowHeight: tokens.DialogFullscreenBelowHeight,
		SplitDiffBreakpoint:         tokens.SplitDiffAtWidth,
		PasteToAttachmentLines:      tokens.PasteAttachRows,
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
		residents: opts.Residents,
		now:       now,
		pollEvery: every,
		linear:    opts.Linear,
		// The token layer implements both of blocks' seams directly, so the
		// transcript is styled by handing it one of these and nothing else.
		style:      tokens.NewStylerIn(opts.Profile, tokens.FocusNormal, opts.GlyphSet),
		transcript: blocks.New(80, 24),
	}

	metrics := Metrics()
	app.shell = tui2.NewShell(tui2.Options{
		Metrics: &metrics,
		Linear:  opts.Linear,
		DB:      opts.Database,
		Session: app.session,
	})

	app.notify = app.shell.Notify
	app.pane = &transcriptPane{
		transcript: app.transcript,
		now:        now,
		invalidate: app.shell.Invalidate,
		answer:     app.answerByPointer,
	}
	app.status = &statusPane{
		style:   app.style,
		bar:     footer.New(footer.Options{Styler: app.style}),
		session: app.session,
		// The footer's words become live here and nowhere else. run is the ONE
		// executor the palette, the `?` sheet and the `/` line already reach —
		// a fourth hand on the same door, not a fourth door — and pop is the
		// breadcrumb's way out, which is esc and the rail's ‹ said a third way.
		run: app.runFooterVerb,
		pop: app.popScope,
	}

	// The place line's ground. One room exists, so the ground is one leg: the
	// resident root this process is standing in (5.19). The day a task room
	// opens, its workspace leg is appended here and nothing else changes —
	// which is why the ground is set through SetGround rather than built into
	// the model.
	app.place = placeline.New(placeline.Options{Styler: app.style, Home: homeDir(opts.Home)})
	if root := rootDir(opts.Root); root != "" {
		app.place.SetGround(placeline.Segment{Path: root, Kind: placeline.SegmentRoot})
	}
	app.meta = &metaStrip{style: app.style}
	// The role is asked once here, before the first frame, rather than left to
	// the first poll a third of a second later. A window that opened as a
	// visitor would otherwise spend that third of a second drawn as the
	// resident it is not, and the one frame a reader sees at attach is the
	// worst possible frame to be wrong in.
	if app.residents != nil {
		state, adopt := app.residents.Residency()
		app.residency = state
		if adopt != nil {
			app.commander = adopt
		}
	}
	if app.commander != nil {
		app.meta.model = strings.TrimSpace(app.commander.CurrentModel("chat"))
	}

	newline := app.shell.Capabilities().NewlineKeys()
	stack := newComposer(composerOptions{
		OnSubmit: func(text string) { app.pending = append(app.pending, text) },
		// The `@` grammar (5.18). Targets alone is a legal adoption — the filter
		// opens, the token completes, and an addressed send goes to OnSubmit like
		// any other — so the two are wired together here only because both halves
		// are ready, not because the composer requires it.
		Targets:    app.mentionTargets,
		OnDispatch: func(d composer.Dispatch) { app.dispatches = append(app.dispatches, d) },
		// The `/` grammar (5.22 rule 3), adopted the same way and just as
		// separably: Commands is the catalog and OnCommand is the executor the
		// palette already uses, so the slash line is a third door onto one room
		// rather than a second room.
		Commands:  app.slashCommands,
		OnCommand: app.runSlash,
		// The attachment grammar (5.11, 7.2, JOURNEY 17), adopted the same way
		// again: Attach is the only door through which a path in the draft
		// becomes a file, and OnSend is where a send that captured one lands.
		// Both are this side's because both need the filesystem and the engine,
		// and the composer is allowed to know about neither (attach.go).
		Attach:      attachmentCandidate,
		OnSend:      func(s composer.Send) { app.sends = append(app.sends, s) },
		Styler:      app.style,
		SendKey:     tui2.SendKey,
		NewlineKeys: newline,
	}, app.place, app.meta)
	stack.mode = app.composerMode
	stack.hud = app.hudRows
	// The region's two chips (5.22 rule 5). Both are doors that already exist:
	// the model palette the registry's own /model row opens, and the clipboard
	// door JOURNEY 18 opened for `y`.
	stack.openModels = app.openModelPicker
	stack.copy = app.copyPath
	app.composer = stack
	app.verbs = boundVerbs(app.shell.Capabilities().NewlineKey())

	// The scope map is built before the panes are bound, because binding the
	// rail pane means handing the shell an object that already knows what it is
	// showing — a rail that drew nothing for one frame and then filled in would
	// be the same attach-time lie the residency probe exists to avoid.
	app.buildScope()
	// The homes' detail renderer. It is handed to the pane as a closure so the
	// pane never learns what a home is, and the View's own line buffer is
	// copied out because it is valid only until the next Render.
	app.homesView = homes.NewView(app.style)
	app.homesPane = app.renderHomes

	app.shell.SetPane(tui2.LayerTranscript, app.pane)
	app.shell.SetPane(tui2.LayerComposer, app.composer)
	app.shell.SetPane(tui2.LayerStatus, app.status)
	app.shell.SetPane(tui2.LayerRail, app.scope)
	app.composer.Focus(true)
	app.refresh()
	return app
}

// boundVerbs is the footer's verb column, read out of the command registry and
// never hand-rolled (5.22 rule 4).
//
// An entry is offered only when this surface really binds the key the entry
// names, which is what keeps the strip from advertising the old window's
// vocabulary. The one substitution is the newline chord: the registry still
// records v1's ctrl+j, and v2 binds whatever the terminal could actually
// negotiate (10.1.2), so the entry's own verb and description are kept and only
// its accelerator is corrected to the truth. Correcting a key is more honest
// than dropping the row; inventing the words would be less.
func boundVerbs(newlineKey string) []registry.Entry {
	out := make([]registry.Entry, 0, 3)
	if quit, ok := registry.ByID("key.quit"); ok && quit.Key == "ctrl+c" {
		out = append(out, quit)
	}
	if newline, ok := registry.ByID("key.thread.newline"); ok && newlineKey != "" {
		newline.Key = newlineKey
		out = append(out, newline)
	}
	// The receipts fold now has a registry-recorded accelerator for THIS kind of
	// surface: the catalog carries v1's bare "v" and a ChordKey for a surface
	// where the composer holds every printable key, and Entry.On projects the
	// row onto the one this surface actually binds. The hand-corrected key this
	// file used to need is gone — 5.22 rule 4 wants the strip read out of the
	// registry, not written beside it.
	if receipts, ok := registry.ByID("key.thread.receipts"); ok {
		if projected, bound := receipts.On(registry.SurfaceComposerFirst); bound &&
			projected.Key == receiptsKey {
			out = append(out, projected)
		}
	}
	// The two copy doors are deliberately NOT here. They are bound (see
	// copy.go) and they are listed by the `?` sheet and the palette, which read
	// the registry directly — but the footer's verb column is scarce and drops
	// lowest-priority-first (10.5.22), so two more permanent rows would push a
	// health notice off an 80-column strip to advertise a key nobody presses
	// twice a session. 5.22's rule is that nothing is typed-only; it is not that
	// everything is on the footer.
	return out
}

// receiptsKey is the accelerator this surface actually binds for the receipts
// fold. It is named once so the ladder in App.key and the footer's own claim
// about it cannot drift apart.
const receiptsKey = "ctrl+r"

// rootDir resolves the place line's root leg: what the caller named, or the
// directory this process is standing in. A working directory that cannot be
// read leaves the line empty, which draws nothing — better than a root that is
// a guess (5.19 exists to stop the chrome being silent about the ground, not to
// start it being wrong about it).
func rootDir(named string) string {
	if named = strings.TrimSpace(named); named != "" {
		return named
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

// homeDir resolves the tilde-folding home. An unknown home only costs the fold.
func homeDir(named string) string {
	if named = strings.TrimSpace(named); named != "" {
		return named
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
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
	return tea.Batch(a.shell.Init(), a.startPoll(), a.waitStream(), a.probeResidency())
}

// View is the shell's. The app never draws.
func (a *App) View() tea.View { return a.shell.View() }

// Frame renders one frame at a size with no terminal involved — the door the
// golden harness and the tests drive.
func (a *App) Frame(width, height int) string {
	a.termWidth, a.termHeight = width, height
	return a.shell.Frame(width, height)
}

// composerMode is what the composer region asks to know: what the selected row
// bound it to (5.15). It is a function rather than a copied field so the region
// and the rail can never hold two answers.
func (a *App) composerMode() composerBind {
	if a.composerBind.mode == rail.ComposerNone {
		return composerBind{mode: rail.ComposerChat}
	}
	return a.composerBind
}

// hudRows is the bounded sticky summary above the composer (8.2.8).
//
// It draws only where the rail does not: 8.3 adopts the HUD-only layout as the
// NARROW fallback and refuses it beside a rail, because two renderings of the
// same live work on one screen is the control room the scoped master-detail
// replaced. It draws nothing at all when nothing is live, so a settled window
// spends no rows on saying so.
func (a *App) hudRows(width, height int) []string {
	if a.wide() || a.hudView == nil || a.hudModel == nil || height <= 0 {
		return nil
	}
	if a.railFocus {
		// The map is already the main pane; a summary of it above the composer
		// would be the same rows twice.
		return nil
	}
	rows := a.hudView.HUD(a.hudModel, width, height)
	if len(rows) == 0 {
		return nil
	}
	// The view's slice aliases its own buffer and is valid only until the next
	// render. Copying is the whole price of holding lines across two paints.
	out := make([]string, len(rows))
	copy(out, rows)
	return out
}

// Update folds a message into the app and returns whatever the fold produced,
// plus whatever the terminal hooks queued while it ran.
//
// The wrapper exists because of the hook contract's one hard rule (shell.go):
// a returned command must be returned onwards, and there is no background
// writer — the command IS the write. The facts that raise a hook are noticed
// deep inside the poll chain, in functions that were built to return nothing,
// and threading a tea.Cmd back out of every one of them would put the contract
// at the mercy of the next branch somebody adds. So the hooks queue and this
// one door drains, which makes "no hook command is ever dropped" a property of
// the shape rather than a rule each caller has to remember.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := a.update(msg)
	if hooks := a.drainHooks(); hooks != nil {
		cmd = tea.Batch(cmd, hooks)
	}
	return model, cmd
}

// hook queues one terminal-protocol write. A nil command is the common case —
// most notifications are suppressed, and an unchanged busy state costs nothing
// — so it is dropped here rather than at four call sites.
func (a *App) hook(cmd tea.Cmd) {
	if cmd != nil {
		a.hooks = append(a.hooks, cmd)
	}
}

// drainHooks hands the queue over as one command.
func (a *App) drainHooks() tea.Cmd {
	if len(a.hooks) == 0 {
		return nil
	}
	batch := tea.Batch(a.hooks...)
	a.hooks = a.hooks[:0]
	return batch
}

func (a *App) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return a, a.drain(a.key(msg))

	case tea.WindowSizeMsg:
		// The terminal's own width decides which of the scope map's three
		// renderings this frame wants (5.15, Part 9.12). A pane is told its
		// rectangle and nothing else, so the number is recorded here — the one
		// place it arrives — rather than guessed from a column's width.
		a.termWidth, a.termHeight = msg.Width, msg.Height
		model, cmd := a.shell.Update(msg)
		_ = model
		return a, cmd

	case nodeMessagesMsg:
		a.applyNodeMessages(msg)
		return a, nil

	case roomOpenedMsg:
		return a, a.applyRoomOpened(msg)

	case steerResultMsg:
		a.applySteer(msg)
		return a, a.startPoll()

	case pollTickMsg:
		return a, tea.Batch(a.startPoll(), a.probeResidency())

	case pollResultMsg:
		a.applyPoll(msg)
		return a, tea.Batch(a.afterPoll(), a.probeResidency(), a.followNode(msg))

	case residencyMsg:
		a.applyResidency(msg)
		return a, nil

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
			// The tick is what ages the meta strip's elapsed cell as well as
			// what turns the awaiting glyph, so it goes through refresh rather
			// than straight to Invalidate: one clock, one place that reads it.
			a.refresh()
		}
		return a, a.startTick()

	case settings.ModelMsg:
		// The settings sheet asked for the models door. It is the one row kind
		// that surface deliberately does not edit itself (8.2.16: one home), so
		// the host opens the palette and the sheet stands down.
		return a, a.openModelSlot(msg)

	case modelResultMsg:
		return a, a.applyModelResult(msg)

	case receiptPostedMsg:
		if msg.err != nil {
			a.status.err = msg.err.Error()
			a.shell.Invalidate()
			return a, nil
		}
		// The row is in the store. The poll draws it there, so nothing is
		// appended here — a receipt drawn twice would be a surface keeping its
		// own copy of the journal.
		return a, a.startPoll()

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
	if len(a.pending) == 0 && len(a.dispatches) == 0 && len(a.sends) == 0 {
		return cmd
	}
	cmds := make([]tea.Cmd, 0, len(a.pending)+len(a.dispatches)+len(a.sends)+1)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}
	for _, text := range a.pending {
		cmds = append(cmds, a.submit(text))
	}
	for _, dispatch := range a.dispatches {
		cmds = append(cmds, a.dispatchCmd(dispatch))
	}
	for _, send := range a.sends {
		cmds = append(cmds, a.submitSend(send))
	}
	a.pending = a.pending[:0]
	a.dispatches = a.dispatches[:0]
	a.sends = a.sends[:0]
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
	key := msg.String()
	if key == "ctrl+c" {
		return tea.Quit
	}
	// A raised overlay owns the keyboard (5.22). The compositor already gives it
	// the cells and the clicks; anything less here would let a keystroke reach a
	// draft the reader cannot see.
	if cmd, taken := a.overlayKey(msg); taken {
		a.shell.Invalidate()
		return cmd
	}

	switch key {
	case "ctrl+k":
		// The cross-scope jump (8.3: "ctrl+k covers cross-scope jumps", which is
		// why this surface has no fullscreen roster).
		return a.openPalette()

	case "alt+,":
		return a.openSettings()

	case "ctrl+o":
		// The chord toggles WHERE THE KEYBOARD IS, not whether a flag is set.
		//
		// The two used to be the same question, and they stopped being it the
		// moment entering a room started handing the keyboard to that room's
		// composer (rooms.go's handOverTheKeyboard): the map is still open, the
		// reader is just no longer typing into it. Toggling scopeOpen there
		// answered ctrl+o by CLOSING the map the reader was asking to go back
		// to. Asking about focus instead makes the chord mean one thing in
		// every state — "let me talk to the map" and, from the map, "let me
		// talk to the room" — which is the round trip that makes the hand-off
		// learnable rather than a rule to remember.
		return a.setScope(!a.railFocus)

	case "esc":
		// 8.2.21, the reconciling rule: esc acts on what you are watching. A
		// turn streaming in THIS room is interrupted; anything else is the
		// composer's to protect and then the app's to interpret. The draft is
		// untouched either way — interrupting a turn is not an edit.
		if a.canInterrupt() {
			return a.interrupt()
		}

	case copyAnswerKey:
		// JOURNEY 18: the registry has named this door since before this surface
		// existed and nothing here opened it.
		return a.copyAnswer()

	case copyFileKey:
		return a.copyFile()

	case "ctrl+r":
		// The receipts fold (5.20 rule 5: receipts are law, and the UI duty is
		// rendering them compactly in the room the user is looking at). The
		// registry records this action under v1's bare "v", which cannot be
		// bound here — in a chat surface the composer holds every printable
		// key — so the accelerator differs and the row's own visible expand
		// hint carries the affordance. See the report: the registry needs a
		// v2-honest key for this row before the footer can name it.
		return a.toggleReceipts()

	case "pgup", "pgdown", "shift+up", "shift+down", "ctrl+home", "ctrl+end":
		return a.pane.Key(msg)
	}

	// The capability door 5.20 rule 3 promises in EVERY room, reachable from the
	// state a reader is actually in. See [App.helpKeyLive].
	if key == helpKey && a.helpKeyLive() {
		return a.openCapability()
	}

	// An open question owns the bare answer keys, and owns them BEFORE the rail
	// and the composer see them (JOURNEY 6). It claims nothing while a draft is
	// in progress, while an overlay is raised, or while the map holds the
	// keyboard — question.go's answerKey states each of those refusals — so the
	// only keystrokes it takes are the ones the row on screen just named.
	if cmd, claimed := a.answerKey(key); claimed {
		a.shell.Invalidate()
		return cmd
	}

	// The map holds the keyboard only while it has focus. That is the whole of
	// 5.15's answer to "how does a chat surface have a navigable list in it":
	// not a mode, not a focus carousel, one chord in and one chord (or esc at
	// home) out — and while the composer has focus, j and k are letters.
	if a.railFocus {
		if cmd, claimed := a.scopeKey(msg); claimed {
			a.shell.Invalidate()
			return cmd
		}
		// AND THE MAP KEEPS IT. A key the map does not claim is not a letter
		// for the composer to take, and letting it fall through was the second
		// half of 13.8's first gap: the map claims j, k, g, G and the digits,
		// everything else reached the draft, and a sentence typed at the map
		// arrived with the navigation keys missing from it. "jack knife kayak"
		// became "ac nife aya" — a steer that silently said something other
		// than what was typed, which is the worst shape a surface can fail in.
		//
		// One cursor (5.14) has to mean one keyboard, or the composer is a
		// second cursor that only accepts some of the alphabet. The composer is
		// already drawn unfocused here; refusing the key is that picture told
		// the truth. ctrl+o hands the keyboard back, and the footer says so.
		return nil
	}

	if a.composer == nil {
		return nil
	}
	// A keystroke the composer takes is a fact that moved: the draft grew, the
	// caret walked, the ring turned. The shell cannot see any of that from the
	// outside, so this is where it is told — and a frame that turns out
	// identical still costs nothing, because Bubble Tea diffs it to no bytes.
	cmd := a.composer.Key(msg)
	// The keystroke may have opened, narrowed or closed an inline completion,
	// and that list lives inside the composer's rectangle. Asking here rather
	// than through refresh keeps the per-keystroke path what it was — refresh
	// walks the transcript to count open questions, which is not a price a
	// letter should pay.
	a.sizeComposer()
	a.shell.Invalidate()
	return cmd
}

// answerByPointer answers an option row a click landed on. It is [App.answer]
// with a repaint: the digits that reach the same call come through the key
// ladder, which invalidates on its own way out, and a click has no such path.
func (a *App) answerByPointer(block *messageBlock, number int) tea.Cmd {
	cmd := a.answer(block, number)
	a.refresh()
	return cmd
}

// copyPath puts a path on the clipboard from a pointer. It is the same door
// copyFile opens and says the same thing when there is nothing to say — 5.19's
// "click/`y` copies" is one act with two hands, not two acts.
func (a *App) copyPath(text string) tea.Cmd {
	text = strings.TrimSpace(text)
	if text == "" {
		a.status.err = "nothing to copy — this room has no ground"
		a.shell.Invalidate()
		return nil
	}
	a.status.err = ""
	a.refresh()
	return tea.SetClipboard(text)
}

// runFooterVerb performs a word on the contextual footer.
//
// It routes through runEntry unchanged, and the invalidate is here rather than
// inside runEntry because the keyboard paths that also call it already repaint
// on their own way out. A click has no such path — the shell repaints what the
// pointer changed, and what this changed is somewhere else entirely.
func (a *App) runFooterVerb(id string) tea.Cmd {
	cmd := a.runEntry(id)
	a.refresh()
	return cmd
}

// popScope is the breadcrumb's click: one step out, the same step esc takes
// (App.navigate) and the same step the rail's ‹ takes (scopePoint). A
// breadcrumb at home pops nothing and says nothing, because there is nowhere
// above home to go.
func (a *App) popScope() tea.Cmd {
	if a.railModel == nil || a.railModel.Depth() == 0 {
		return nil
	}
	return a.applyScope(a.railModel.Escape())
}

// sizeComposer asks the region for however many rows its open inline completion
// wants, and for none when nothing is open. GrowComposer re-solves the layout
// only when the answer moved, so a draft nobody is completing costs nothing.
func (a *App) sizeComposer() {
	if a.composer == nil {
		return
	}
	a.shell.GrowComposer(a.composer.HintRows())
}

// helpKey is the bare rune the footer has been advertising on every frame since
// this surface existed. It is named once so the ladder that binds it and the
// door that draws it cannot drift apart.
const helpKey = "?"

// helpKeyLive reports whether a bare `?` reaches the capability sheet right now.
//
// 13.5's second finding: the footer drew `? help` unconditionally while the only
// way to fire it was ctrl+o first, so the surface's own footer was breaking
// 5.22's discoverability law and 5.20 rule 3's promise that "`?` in any room
// lists what this orchestrator can do". The doc offers two repairs and says to
// take exactly one — "cheap fix in the key ladder or honest fix in the footer,
// but not both ways" — and the ladder is the one the rest of the doc already
// argues for: 5.20 rule 3 and 5.22 rule 4 both write the door as a bare `?`,
// and 5.22's checklist closes by demanding the `?` surface have a permanent
// visible door because "the capability-honesty surface cannot itself be a
// memory test". A footer that withdrew the door in the state a reader is most
// often in would make it exactly that again.
//
// So `?` is claimed the way 12.12.6 claims the question digits, in that
// section's own words: "only where they cannot mean anything else — empty
// draft, no overlay, rail unfocused". A draft in progress keeps `?` as the
// character it is, because a sentence is a sentence; on an empty draft nothing
// is being written and the rune can only have been the door. The map's own
// focus is the third case and the one this surface already had.
func (a *App) helpKeyLive() bool {
	if a.overlay != overlayNone {
		return false
	}
	return a.railFocus || !a.drafting()
}

// drafting reports whether a sentence is in progress. It is the one predicate
// every bare-key guard on this surface consults — the answer ladder, the
// footer's digit column and the help door — so the keys and the rows that
// advertise them cannot disagree about what "empty draft" means.
func (a *App) drafting() bool {
	return a.composer != nil && strings.TrimSpace(a.composer.Draft()) != ""
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

// clearDraft is the registry's `key.thread.clear-draft` performed from a
// palette row rather than from its accelerator (5.22: the key is never the only
// door, and a row the sheet lists must do what it says).
//
// The method is asked for rather than assumed, exactly as [App.paste] asks for
// its own: a composer that cannot kill a draft simply never gets the door, and
// the pane interface stays the three calls it has always been.
func (a *App) clearDraft() tea.Cmd {
	if a.composer == nil {
		return nil
	}
	killer, ok := a.composer.(interface{ KillToStart() })
	if !ok {
		return nil
	}
	killer.KillToStart()
	a.refresh()
	return nil
}

// canInterrupt reports whether esc would in fact stop a turn — which is the
// exact condition under which the awaiting line is allowed to say so.
//
// A visitor is excluded by name. Its commander is not nil — a visitor window
// still has one, for the doors that write to the journal — but the turn being
// answered is running in another process, and stopping it is not something this
// window can do. An interrupt hint over a turn this window cannot reach is the
// worst kind of lie: the reader presses the key, the turn keeps running, and
// the surface has taught them not to believe it.
func (a *App) canInterrupt() bool {
	return a.turn.active && !a.residency.Visitor && a.commander != nil &&
		a.turn.await != nil && a.turn.await.interruptible
}

// -- the residency chain -----------------------------------------------------

// probeResidency asks the door what this window is, off the render goroutine
// and one question at a time.
func (a *App) probeResidency() tea.Cmd {
	if a.residents == nil || a.probing {
		return nil
	}
	a.probing = true
	residents := a.residents
	return func() tea.Msg {
		state, adopt := residents.Residency()
		return residencyMsg{state: state, adopt: adopt}
	}
}

// applyResidency folds one answer in, and adopts the replacement engine when
// the role has moved.
//
// The stream feed is deliberately NOT swapped here. The entry point owns one
// durable channel for the life of the window and re-points its own bridge at
// whatever engine currently holds the role, so a promotion changes who is
// talking without changing what this surface is listening to — and the wait
// already in flight stays valid instead of being orphaned on a dead channel.
func (a *App) applyResidency(msg residencyMsg) {
	a.probing = false
	moved := msg.state != a.residency
	a.residency = msg.state
	if msg.adopt != nil {
		a.commander = msg.adopt
		if model := strings.TrimSpace(msg.adopt.CurrentModel("chat")); model != "" {
			a.meta.model = model
		}
		moved = true
	}
	if moved {
		a.refresh()
	}
}

// interrupt stops the turn being watched, carrying in the words already on
// screen so the durable line that ends it is the same reply, marked where it
// stopped. The mark itself is the engine's to journal (store.InterruptedEnd)
// and arrives at the next poll as a message part; this surface does not invent
// it, which is what keeps the record and the screen the same thing.
func (a *App) interrupt() tea.Cmd {
	a.commander.Interrupt(a.turn.shown)
	a.turn.stopped = true
	a.turn.await.phase = "stopping"
	a.turn.await.interruptible = false
	a.refresh()
	return a.startPoll()
}

// navigate is what esc means when there is no turn to stop and no draft to
// protect. It never destroys anything, and it walks outward one step at a time:
// back to the live edge first, and then out of the scope.
//
// The second step is not new behaviour, it is the same rule reached from the
// other side of the keyboard. 5.15 says esc pops scope, never just selection,
// and the map's own grammar has always done that (scopeKey). Once entering a
// room hands the keyboard to that room's composer (bind's handOverTheKeyboard),
// the map is no longer where esc arrives — so without this the reader who
// entered a task would have had no way out but a chord, and "esc pops scope"
// would have quietly become "esc pops scope if you first press ctrl+o".
func (a *App) navigate() tea.Cmd {
	if !a.transcript.AtBottom() {
		a.transcript.GotoBottom()
		a.shell.Invalidate()
		return nil
	}
	if a.railModel == nil || a.railModel.Depth() == 0 {
		return nil
	}
	return a.applyScope(a.railModel.Escape())
}

// toggleReceipts opens or closes every folded row in the transcript.
//
// It walks the block list rather than keeping an index of receipts, because the
// walk happens once per keystroke and an index would have to be kept correct on
// every append — a cost paid per journal row to save one paid per key. Only the
// blocks that actually moved bump a version, so the transcript's finalized cache
// re-renders exactly the rows that changed and nothing else.
func (a *App) toggleReceipts() tea.Cmd {
	a.receiptsOpen = !a.receiptsOpen
	moved := false
	// The transcript the READER IS LOOKING AT, which is not always the room's
	// own: a task room and a preview card each carry their own block list, and
	// the fold used to walk the conversation's regardless — so ctrl+r inside a
	// task room opened rows nobody could see and left the ones on screen shut.
	// 5.20 rule 5's UI duty is rendering receipts in the room the user is
	// looking at, and the fold is the same rule said as a keystroke.
	transcript := a.pane.transcript
	if transcript == nil {
		transcript = a.transcript
	}
	for i := 0; i < transcript.Len(); i++ {
		if block, ok := transcript.Block(i).(*messageBlock); ok {
			moved = block.SetExpanded(a.receiptsOpen) || moved
		}
	}
	if moved {
		a.shell.Invalidate()
	}
	return nil
}

// refresh repaints and brings every strip of chrome up to date with the state
// that decides it.
//
// It is one function rather than a set of setters because the chrome answers
// one question — what is true right now — and the surface's old failure mode
// was four booleans that could each be right while the row they produced was
// wrong. Everything here is read from state the app already holds; nothing is
// computed twice and nothing is stored that a render could have derived.
func (a *App) refresh() {
	// A visitor cannot stop a turn it is not running, so the awaiting line's own
	// hint is withdrawn at the same moment the footer's is. Both rows answer the
	// same question — what will esc do — and 8.2.21's rule is that whatever the
	// awaiting line says is what esc will do.
	if a.residency.Visitor && a.turn.await != nil {
		a.turn.await.interruptible = false
	}
	a.status.residency = a.residency
	a.sizeComposer()
	if a.source != nil {
		a.status.room = a.source.RoomTitle(a.session)
	}
	a.status.live = a.turn.active
	a.status.escInterrupts = a.canInterrupt()
	a.status.verbs = a.verbs
	a.status.foldable = a.foldable
	a.status.input, a.status.hint = a.inputState()
	a.status.attention = a.openQuestions()
	// The terminal's title carries the same count the footer paints (10.5.27).
	a.noticeAttention(a.status.attention)
	a.status.keyMode, a.status.keyCount = a.keyMode()

	a.meta.live = a.turn.active
	if a.turn.active {
		if a.turn.started.IsZero() {
			a.turn.started = a.now()
		}
		a.meta.elapsed = a.now().Sub(a.turn.started)
	} else {
		a.meta.elapsed = 0
	}
	a.voiceLivePreview()
	a.shell.Invalidate()
}

// inputState reads the composer's own state for the footer's hint column
// (10.5.26, the Warp pattern). The words are composed here because only this
// side knows what this surface's accelerators actually are; the footer decides
// where they go and what colour a failure is.
func (a *App) inputState() (footer.InputState, string) {
	if a.composer == nil || strings.TrimSpace(a.composer.Draft()) == "" {
		return footer.InputEmpty, ""
	}
	return footer.InputTyped, tui2.SendKey + " send"
}

// openQuestions counts the questions blocking on a human right now, which is
// the one thing 5.16 lets the footer paint amber.
//
// A question is open until the reader speaks again, so the count is read off
// the tail of the transcript backwards, stopping at the reader's own last turn.
// That is bounded work per frame — a handful of blocks — where counting the
// whole thread would make every repaint proportional to the length of the
// conversation, which is exactly what the block engine exists to avoid.
func (a *App) openQuestions() int {
	open := 0
	for i := a.transcript.Len() - 1; i >= 0; i-- {
		block, ok := a.transcript.Block(i).(*messageBlock)
		if !ok {
			continue
		}
		if block.user {
			break
		}
		open += block.questions
	}
	return open
}

// voiceLivePreview gives the streamed reply the same header voice its durable
// twin will wear.
//
// The preview's block is built by the poll chain with a chrome-tier header; the
// journaled reply that replaces it is drawn at full contrast (message.go's
// dressSpeech). Left alone, the label would brighten under the reader at the
// exact moment the turn settles — a change that means nothing, on the row a
// reader is watching most closely. The mutation goes through TextBlock.Mutate
// so the version bumps, which is the only coherent way to change bytes a cache
// has already committed (8.1.1), and it happens once per turn.
func (a *App) voiceLivePreview() {
	if a.turn.voiced || a.turn.label == nil {
		return
	}
	a.turn.voiced = true
	a.turn.label.Mutate(func(block *blocks.TextBlock) {
		block.Head.State = blocks.StateSettled
	})
}
