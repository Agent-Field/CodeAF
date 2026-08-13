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
	// something a reader can be shown. It is ONE block: the answer is the
	// unmarked voice (§3b), so there is no header row above it to keep at a
	// different depth. See newReplyBlock.
	reply *blocks.TextBlock
	// await is the awaiting line, present for as long as the turn is.
	await *awaitingBlock
	// activity is what the turn is DOING while it is not talking: one row per
	// belt call, pinned between the streamed text and the awaiting line. It is
	// nil until the first call, so an ordinary conversational turn allocates
	// nothing for it — see activity.go.
	activity *activityBlock
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
	// place is no longer a ROW of this surface — §7 folded the directory into
	// the bar's right zone — but it is still the component that knows how to
	// abbreviate a path, so the app keeps a model and asks it for words.
	place    *placeline.Model
	composer composerPane
	verbs    []registry.Entry

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

	railFocus bool
	scopeOpen bool
	// railShown is §6's `sidebar: hidden`, remembered for this session.
	//
	// IT IS FALSE AT LAUNCH, on every page, and that is the wave's decision
	// rather than a default nobody chose. A reader said it twice — "still in
	// chat I see side rail" — and the reason it reads as clutter now is that
	// everything the column was carrying got a better home in the meantime: work
	// and notebook became full pages, the palette searches every room, and the
	// hug's own tabs are the door to all three. What is left of an always-open
	// sidebar is a second copy of a list one keystroke away, which is §15's
	// same-fact-twice standing permanently in a quarter of the window.
	//
	// So the rail is a DRAWER: shut by default, compressed to the dock on the
	// bar row (footer/dock.go), opened by the same chord that has always meant
	// "let me talk to the map" and by a click on the dock itself. It is
	// remembered for the session and not persisted — a preference file would
	// make this a setting, and §6 already has one (`sidebar: right|left|hidden`)
	// for the reader who wants to decide once.
	railShown  bool
	termWidth  int
	termHeight int

	// The overlay plane (5.22): one door at a time, each built on first use so
	// a window that never summons the palette never pays for one.
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
	// switcher is the chats switcher (threads.go). It is built on first use like
	// every other door on this plane, so a window whose reader never presses `t`
	// never pays for one.
	switcher *palette.Switcher

	// roomSwitch is a split the head has settled in the journal and this window
	// has read but not yet performed (5.4). See [App.drainRoomSwitch].
	roomSwitch roomSwitchIntent
	// roomSwitched is the set of settlement rows this window has already acted
	// on, by journal sequence. It is what makes "exactly once" survive the reset
	// a switch performs — see [App.noteRoomSwitchSeen].
	roomSwitched map[int64]bool

	// threadSeen is when this window last had each thread's state on screen,
	// keyed by session id. It is what the switcher's unseen-delivery `●` is
	// derived from until the engine's own [ThreadReader] answers, and it is
	// deliberately a claim about THIS WINDOW rather than about the person: a
	// thread nobody here has visited carries no entry and therefore no dot.
	threadSeen map[string]time.Time

	// view is the main pane's current lens: nil is the room's own conversation,
	// anything else is a task room or the card a cursor move previewed.
	view *mainView

	// The pages (§7: the places tabs swap the LENS, not a mode inside it). page
	// is which one is on screen; the two beside it are the lenses that are not
	// the thread. Both are built once, because a page a reader keeps coming back
	// to must not forget where its cursor was.
	page page
	// verbArmed is a destructive item verb waiting for its second yes
	// (itemverb.go). Nil is the ordinary state.
	verbArmed *armedVerb

	// pageFocus says the PAGE holds the keyboard rather than the composer. It
	// is [App.railFocus] for a lens that has no rail beside it, and it is a
	// second flag rather than a reuse because the two answer about different
	// objects: the map can hold the keyboard on the thread page while the board
	// does not exist, and the board can hold it while the map is off the frame.
	pageFocus    bool
	board        boardState
	boardPane    *pagePane
	notebook     notebookPage
	notebookPane *pagePane
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
	// folds is the reader's own answer for individual rows, keyed by block id,
	// and it OUTRANKS receiptsOpen for as long as it holds one. It is what makes
	// 7.2's "state that survives re-render" true across 13.15's whole-list
	// rebuild — the flag cannot live on a block that is thrown away and rebuilt
	// on every journal move. See disclose.go.
	folds map[string]bool
	// traces is what each worker's recorder said, as this window last read it,
	// keyed by node id. It is the source the journal does not have (trace.go):
	// the tool calls, the results and the model's own words between them. It is
	// kept on the APP rather than on the view so a reader who leaves a room and
	// comes back does not pay for the whole tail again — the stamp beside the
	// text is what makes the re-read a stat.
	traces map[string]nodeTrace
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

	// sendErr is why the LAST send did not land, or "" when the last one did.
	// It is the composer's failed state (§7): a coral `✕` at the prompt and a
	// coral sentence in the bar's middle zone, with the words themselves put
	// back in the draft. See [App.failSend].
	sendErr string

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
		// THREE rows: two of writing area and one blank under them (§7's hug,
		// as amended by a reader who found the one-row version cramped). The bar
		// is not in this number — StatusHeight reserves it — and neither is the
		// place line or the meta strip, which used to bring the total to four
		// and are gone.
		//
		// The floor and the padding are the composer region's own
		// ([draftFloor], [draftPad]); this table only has to reserve enough rows
		// for them, and the region grows upward past it on demand
		// ([composerPane.HintRows] into tui2.Shell.GrowComposer).
		ComposerHeight:              draftFloor + draftPad,
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
		fold:       app.toggleFold,
		record:     app.recordPointer,
		copy:       app.copyMessage,
		focus:      app.focusConversation,
		// The taught empty state's rows (5.22 rule 6) run through the SAME
		// executor the footer's words do — one door, a fifth hand on it.
		run:   app.runFooterVerb,
		style: app.style,
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
		// interrupt is the `interrupt esc` chip's act — esc's own, reached by
		// a pointer. The row offers it only while EscInterrupts says it works.
		interrupt: app.interrupt,
		// openRail is the dock's act: the shut drawer, clicked. It is the very
		// function the rail chord runs (§6, [App.toggleRail]).
		openRail: app.toggleRail,
		// openThreads is the title chip's act: the switcher, clicked. It is the
		// very function the `t` key runs (5.3, [App.openSwitcher]).
		openThreads: app.openSwitcher,
		// The places tabs (§7's left zone) are filled by refresh from the page
		// enum — see [App.places]. They are deliberately NOT written out here as
		// well: the words and which of them is bright are one fact, and a copy
		// of them at construction would be a second answer to "where is the
		// reader" that nothing keeps in step.
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
		app.status.model = strings.TrimSpace(app.commander.CurrentModel("chat"))
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
	}, !app.linear)
	stack.mode = app.composerMode
	stack.hud = app.hudRows
	// The region's whole rectangle is a door onto the keyboard, which is the
	// half of 13.14's pointer law that only ran in one direction (rooms.go's
	// focusConversation).
	stack.focus = app.focusConversation
	app.composer = stack
	// §7: the middle zone holds only what is live RIGHT NOW. The standing
	// legends boundVerbs used to project (quit, newline, receipts) belong to
	// the ? sheet and the palette, which read the registry directly; Verbs is
	// reserved for genuinely live verbs (a task pane's cancel/restart while it
	// holds focus). boundVerbs stays for the ? sheet's key-correction path.
	app.verbs = nil

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
	// The two lenses that are not the thread (§7). They are built here rather
	// than on first use because a tab is a place and not a dialog: the reader
	// expects the work they left on the board to be where they left it, and a
	// page minted on arrival would have no cursor and no fold to come back to.
	app.boardPane = &pagePane{
		render: app.renderBoard,
		point:  app.boardPoint,
		hover:  app.boardHover,
	}
	// The notebook page's RENDERER belongs to internal/tui2/homes; this side
	// wires it. See [notebookPage] for the contract and for what happens on a
	// build where the page has not landed yet.
	app.notebook = newNotebookPage(app.style)
	app.notebookPane = &pagePane{
		render: app.renderNotebook,
		point:  app.notebookPoint,
	}

	app.shell.SetPane(tui2.LayerTranscript, app.pane)
	app.shell.SetPane(tui2.LayerComposer, app.composer)
	app.shell.SetPane(tui2.LayerStatus, app.status)
	app.shell.SetPane(tui2.LayerRail, app.scope)
	// §6's drawer, shut. The rail's PANE is bound either way — the shell is what
	// decides whether the frame has a column for it ([App.applyRail]) — because a
	// window that opened with the sidebar unbound would have to rebuild it the
	// first time the chord was pressed, and a pane built on a keystroke draws
	// nothing for one frame. This is asked here rather than left to the first
	// page swap because the FIRST FRAME is the one a reader judges, and
	// [App.setPage] is a no-op on the page the window already starts on.
	app.applyRail()
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

	case traceReadMsg:
		a.applyTraceRead(msg)
		return a, nil

	case subtreeReadMsg:
		a.applySubtreeRead(msg)
		// The plan may name parts whose recorders were never asked for, because
		// the board did not know they existed a moment ago.
		return a, a.readTraceCmd(msg.node)

	case roomOpenedMsg:
		return a, a.applyRoomOpened(msg)

	case steerResultMsg:
		a.applySteer(msg)
		return a, a.startPoll()

	case pollTickMsg:
		return a, tea.Batch(a.startPoll(), a.probeResidency())

	case pollResultMsg:
		a.applyPoll(msg)
		// The read chain is taken down BEFORE a settled split is performed, so
		// the switch arms the poll for the thread it is arriving in rather than
		// colliding with the one it is leaving. See [App.drainRoomSwitch].
		chain := tea.Batch(a.afterPoll(), a.probeResidency(), a.followNode(msg))
		return a, tea.Batch(chain, a.drainRoomSwitch())

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
		} else if a.roomIsLive() {
			// An open room watching a tool call run. Nothing is rebuilt here —
			// the transcript re-renders its own live region on the frame, which
			// is one row — so this is the invalidate and nothing more.
			a.shell.Invalidate()
		}
		return a, a.startTick()

	case copiedMsg:
		// The copy chip's one frame of proof, expiring (copychip.go).
		a.applyCopied(msg)
		return a, nil

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
	case summonKey, summonKeyNUL, summonKeyLegacy:
		// The summon key, and the cross-scope jump it grew out of (8.3: "ctrl+k
		// covers cross-scope jumps", which is why this surface has no fullscreen
		// roster). Three spellings, one door — see [summonKey].
		return a.openPalette()

	case "alt+,":
		return a.openSettings()

	case threadsChord, threadsCtrl:
		// The switcher's chorded spellings, bound unconditionally. The bare `t`
		// below is the key the doc names; these are the ones a reader can press
		// mid-sentence, which is the same pair every other bare-key row on this
		// surface carries.
		//
		// TWO SPELLINGS BECAUSE ONE OF THEM DOES NOT ARRIVE. See [threadsCtrl]:
		// a macOS terminal composes Option+t into `†` and the alt chord never
		// reaches this switch at all. ctrl+t is the spelling that survives every
		// terminal, and it is the one the registry teaches.
		return a.openSwitcher()

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
		//
		// ON A PAGE IT MEANS THE SAME SENTENCE ABOUT A DIFFERENT LIST. The map
		// is not on screen there — the board IS the list, and the notebook is
		// its own — so the chord toggles between the page and the mouth. One
		// chord, one meaning, whichever lens is up.
		//
		// AND IT IS NOW ALSO THE DOOR, because §6's rail starts hidden: no new
		// key was invented for the drawer, since a second chord for "the map"
		// would be two answers to one question. See [App.toggleRail] for the
		// three rungs and for why the middle one had to stay.
		if a.page != pageThread {
			return a.setPageFocus(!a.pageFocus)
		}
		return a.toggleRail()

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

	// The page's own keyboard, and only on a page that is not the thread. Like
	// the answer digits above it, it claims a key only where that key cannot
	// mean anything else — no overlay, the map not holding the keyboard, and an
	// empty draft — because the mouth is global and a sentence in progress
	// outranks every accelerator on the screen (§8: typing is the rest state).
	if cmd, claimed := a.pageKey(msg); claimed {
		a.shell.Invalidate()
		return cmd
	}

	// The map holds the keyboard only while it has focus. That is the whole of
	// 5.15's answer to "how does a chat surface have a navigable list in it":
	// not a mode, not a focus carousel, one chord in and one chord (or esc at
	// home) out — and while the composer has focus, j and k are letters.
	if a.railFocus {
		// THE BARE `t` LIVES HERE AND NOWHERE ELSE (5.2's J3, and the reason is
		// worth stating because the doc writes the key as a bare `t`).
		//
		// A composer-first room hands every printable character to the draft, and
		// `t` is not `?`: it opens a large fraction of English sentences. Claiming
		// it on an empty draft — the rule `?` keeps, and the rule this lane tried
		// first — meant that typing "the diff looks right" opened a thread list
		// on the first keystroke and filtered it with the rest. That is the same
		// trap the switcher's own `new thread` key was moved off, one surface
		// over, and it fails harder here because the composer is where a person
		// spends their whole day.
		//
		// So the bare key is bound where a bare letter is already navigation
		// rather than text: while the MAP holds the keyboard. Everywhere else the
		// door is alt+t, which is exactly what the registry's ChordKey means and
		// what every surface that reads the catalog will teach in a
		// composer-first room ([registry.SurfaceComposerFirst]).
		if msg.String() == threadsKey {
			return a.openSwitcher()
		}
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

// dockCounts is §6's one-line dock: what the shut drawer has inside it.
//
// It draws only while the rail is actually off the frame, and it asks the SHELL
// rather than [App.railShown] — the shell is the thing that solved the frame,
// and on the work page the rail is off it for a reason this flag knows nothing
// about. One question, asked of whatever answered it.
//
// The counts come from the same split the board page reads (board.go's
// boardJobs), so the dock and the page it is a door to can never disagree about
// what "working" means. Nothing is counted twice: a job holding a question is
// working AND is a question, which is two facts about one row and exactly what
// §6's dock line says.
func (a *App) dockCounts() footer.Dock {
	if a.shell == nil || !a.shell.RailHidden() {
		return footer.Dock{}
	}
	if a.page == pageBoard {
		// The dock says "there is a list you cannot see". On the work page the
		// list is the whole lens, so a collapsed copy of it in the corner is
		// §15's same-fact-twice again — the very thing hiding the rail here was
		// for.
		return footer.Dock{}
	}
	dock := footer.Dock{Shown: true, Questions: a.openQuestions()}
	working, _ := a.boardJobs()
	dock.Working = len(working)
	return dock
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

// The three spellings that summon the palette. One door, and the count is a
// property of terminals rather than a decision this file made.
//
// summonKey is the one the registry records and every surface therefore teaches
// (registry's key.palette row). It was chosen because a terminal NEVER delivers
// a cmd- chord, and because ctrl+space is the one chord almost nothing squats
// on: it is the NUL byte, which is also why it survives a composer-first room
// where every printable key belongs to the draft.
//
// summonKeyNUL is the same byte under a different name. The decoder in this
// stack resolves NUL to {Code: KeySpace, Mod: ModCtrl} and spells that
// "ctrl+space", but a terminal speaking the kitty protocol can report the same
// keystroke as ctrl+@ — the caret notation for the same control code — and a
// door that opened under one spelling and not the other would be a door that
// depends on the reader's terminal emulator. Binding what arrives is the whole
// rule: this side does not get to say which name the wire uses.
//
// summonKeyLegacy is ctrl+k, which opened this palette before the summon key
// existed. It stays bound and is deliberately NOT a second registry row — the
// registry's own rule is that where a surface takes two spellings of one chord,
// Key names the one the help screen leads with and the synonym is not a second
// registration. Muscle memory is a real user of a keybinding.
const (
	summonKey       = "ctrl+space"
	summonKeyNUL    = "ctrl+@"
	summonKeyLegacy = "ctrl+k"
)

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
			a.status.model = model
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
	// A PAGE IS THE OUTERMOST THING ESC UNDOES. The hug persists on every page,
	// so the reader who pressed esc on the board or the notebook has no scope to
	// pop and no transcript to return to — what they are watching is a lens they
	// swapped in, and esc puts it back (§7).
	if a.page != pageThread {
		return a.showPage(pageThread)
	}
	// A nested drill is the innermost thing on the thread page: esc walks back
	// up one worker before anything else moves (recordpage.go).
	if a.drillBack() {
		return nil
	}
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
	// The room-wide key is what makes the room-wide state true again. A reader
	// who opened three rows by hand and then asked for all of them has asked for
	// ALL of them, and an override surviving that would be three rows quietly
	// disagreeing with the key that was just pressed (disclose.go).
	a.clearFolds()
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
	// The title chip (chat-simplify.md 5.3): the scribe's name for the thread
	// this window is in, and NOTHING while it has none. It is asked through
	// [App.threadName] rather than through [scopeSource.RoomTitle] because the
	// two answer different questions — the rail needs a word for every row it
	// draws and falls back to "untitled room", and the chip would rather say
	// nothing than say that.
	a.status.thread = a.threadName(a.session)
	a.status.live = a.turn.active
	a.status.escInterrupts = a.canInterrupt()
	a.status.verbs = a.verbs
	// The model word rides the bar's right zone now, and it is the picker's
	// door there (§7). It used to be kept OFF this row on the grounds that the
	// composer's meta strip was the model's one home and two adjacent rows
	// saying it would be §15's same-fact-twice — which was true, and was an
	// argument for one row rather than for which one. The strip is gone; the
	// word stayed.
	//
	// The ground goes with it, in placeline's own abbreviated form. It is asked
	// at a fixed budget rather than at the terminal's width because the footer
	// fits its own columns: what this side owes the row is the SHORTEST honest
	// spelling of where the work lands, and the row decides whether it has the
	// cells for it.
	if a.place != nil {
		a.status.dir = a.place.Text(dirBudget)
	}
	a.status.foldable = a.foldable
	// §7's left zone. The tabs are drawn FROM the page enum rather than beside
	// it, so the bright word and the lens on screen are one fact: a Current the
	// swap forgot to move would be the footer naming a place the reader is not
	// in, which is 5.20's affordance lying about where you are.
	a.status.places = a.places()
	// The work page's own wider read, at most once per journal move and only
	// while that page is the lens (see [App.syncBoard]).
	a.syncBoard()
	a.status.input, a.status.hint = a.inputState()
	a.status.attention = a.openQuestions()
	a.status.dock = a.dockCounts()
	// The terminal's title carries the same count the footer paints (10.5.27).
	a.noticeAttention(a.status.attention)
	a.status.keyMode, a.status.keyCount = a.keyMode()

	if c, ok := a.composer.(interface {
		Streaming(on bool, frame int)
		SetHint(state composer.Hint, detail string)
		SetState(state composer.State)
	}); ok {
		// §8: one live signal, at the prompt, on the same clock every other
		// live glyph ticks with — and the empty line says what the room is
		// doing. The working state is not chosen here: the composer asserts
		// it off Streaming, so the words and the spinner cannot disagree.
		c.Streaming(a.turn.active, a.transcript.Clock().Frame(len(tokens.SpinnerFrames)))
		c.SetHint(a.composerHint())
		c.SetState(a.composerState())
	}
	if a.turn.active {
		if a.turn.started.IsZero() {
			a.turn.started = a.now()
		}
		a.status.elapsed = a.now().Sub(a.turn.started)
	} else {
		a.status.elapsed = 0
	}
	a.shell.Invalidate()
}

// dirBudget is the cells the place line is asked to fit its abbreviated ground
// into before the bar row is even consulted. It is generous on purpose: the
// abbreviation is already the short form (`~/a/aforge-v2`), and a budget tight
// enough to make placeline drop legs would be this side second-guessing a fit
// the footer is about to do properly.
const dirBudget = 48

// composerState is what the prompt cell and the edge beside it say about this
// room right now (§7's state-reactive prompt). The ladder is the same one the
// composer paints in, minus the streaming rung — [composer.Model.Streaming]
// owns that one, because it also drives the spinner's frame and one fact with
// two doors is one fact that can be told two different things.
//
// A FAILED SEND outranks an open question because it is the more recent event
// and the one the reader's next keystroke is about: the words that did not go
// are still in the draft, and enter sends them again.
func (a *App) composerState() composer.State {
	if a.sendErr != "" {
		return composer.StateFailed
	}
	if a.openQuestions() > 0 {
		return composer.StateQuestion
	}
	return composer.StateIdle
}

// inputState reads the composer's own state for the footer's hint column
// (10.5.26, the Warp pattern). The words are composed here because only this
// side knows what this surface's accelerators actually are; the footer decides
// where they go and what colour a failure is.
func (a *App) inputState() (footer.InputState, string) {
	// A SEND THAT DID NOT LAND outranks everything else this axis can say. It is
	// the one input state §7 lets onto the bar row at all, as a coral sentence
	// in the middle zone (§12 spends coral on broken), and it is paired with the
	// coral `✕` at the prompt two cells away — one event, said once in each of
	// the two places a person is looking when it happens.
	if a.sendErr != "" {
		return footer.InputFailed, a.sendErr
	}
	if a.composer == nil || strings.TrimSpace(a.composer.Draft()) == "" {
		return footer.InputEmpty, ""
	}
	return footer.InputTyped, tui2.SendKey + " send"
}

// failSend records that a post did not land, and gives the words back.
//
// The two halves are one act. A failure the reader can SEE but whose sentence
// has been destroyed is worse than no failure notice at all — it tells them
// something went wrong and leaves them retyping it — so the message goes back
// into the draft on the same call that lights the prompt coral. The composer
// refuses the restore if the reader has already started typing something else
// ([composer.Model.Restore]), and the notice stands either way.
func (a *App) failSend(text string, err error) {
	if err == nil {
		return
	}
	a.sendErr = err.Error()
	if r, ok := a.composer.(interface{ Restore(string) bool }); ok {
		r.Restore(strings.TrimSpace(text))
	}
	a.shell.Invalidate()
}

// clearSendFailure ends the failed state, and is called at the head of every
// new attempt rather than on a timer: the state is about the LAST send, so it
// lasts exactly until there is another one.
func (a *App) clearSendFailure() { a.sendErr = "" }

// openQuestions counts the questions blocking on a human right now, which is
// the one thing 5.16 lets the footer paint amber.
//
// A question is open until the reader speaks again, so the count is read off
// the tail of the transcript backwards, stopping at the reader's own last turn.
// That is bounded work per frame — a handful of blocks — where counting the
// whole thread would make every repaint proportional to the length of the
// conversation, which is exactly what the block engine exists to avoid.
// composerHint is §8's state for the empty line, in priority order. The
// working state is NOT here: the composer asserts it off Streaming, so the
// words and the spinner cannot disagree. The delivered state waits on the
// job-block lane's transcript-tail read; until then a landed delivery is
// announced by its card and the idle words stand.
func (a *App) composerHint() (composer.Hint, string) {
	if a.openQuestions() > 0 {
		return composer.HintQuestion, ""
	}
	return composer.HintIdle, ""
}

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

// The streamed preview used to need a pass of its own here — voiceLivePreview,
// which re-tiered the `aforge` label so it would not brighten under the reader
// at the instant the turn settled. §3b took the label out: the answer is the
// unmarked voice, the preview and its journaled twin are both plain prose at the
// shared left edge, and there is no longer anything about the live region that
// changes when it becomes a record. The pass went with the row it was fixing.

// -- the pages (§7: tabs are pages) ------------------------------------------
//
// THE TABS SWAP THE LENS. `chat` is the thread — today's transcript, unchanged;
// `work` is the board (board.go); `notebook` is the homes page. The HUG does not
// move: the composer and the contextual line are the same two rows on every
// page, because the mouth is global (§2's one mouth) and a page that took the
// composer with it would be a second surface wearing the first one's chrome.
//
// Three properties hold the mechanism together, and they are the reason it is
// one enum rather than a flag per lens:
//
//   - A PAGE IS A PLACE, NOT A MODE. Arriving at one leaves whatever room the
//     reader had descended into, so the footer's trail and its tabs are never
//     both true — §7 gives the left zone to one or the other, and the trail
//     replaces the tabs while the reader is inside something.
//   - ENTERING A JOB IS ALWAYS THE SAME DOOR. From the board, from the sidebar,
//     from the palette: [App.jumpTo] on the row's own id, which lands the reader
//     in the room on the CHAT page with the trail where the tabs were.
//   - A PAGE HOLDS NO STATE THE STORE DOES NOT. What the board and the notebook
//     keep is a cursor, a fold and a scroll — presentation, all of it, and all
//     of it belonging to the surface a person is looking at rather than to the
//     facts underneath.

// page is which lens the places tabs have swapped in.
type page uint8

const (
	// pageThread is the room's own conversation, and the zero value: a window
	// that has never touched a tab is in the chat, which is where a chat surface
	// starts.
	pageThread page = iota
	// pageBoard is the work board — §6's sidebar at page altitude (board.go).
	pageBoard
	// pageNotebook is the homes page, drawn by internal/tui2/homes.
	pageNotebook
)

// String names the page for a test failure.
func (p page) String() string {
	switch p {
	case pageThread:
		return "chat"
	case pageBoard:
		return "work"
	case pageNotebook:
		return "notebook"
	}
	return "invalid"
}

// places is §7's left zone: the three tabs, with the current one read off the
// page enum.
//
// The ids are the registry's own, so a click on a tab and the chord that reaches
// the same place run the ONE executor ([App.runEntry]) — a fourth hand on one
// door, never a fourth door.
func (a *App) places() []footer.Place {
	return []footer.Place{
		{ID: placeThreadID, Word: "chat", Current: a.page == pageThread},
		{ID: placeBoardID, Word: "work", Current: a.page == pageBoard},
		{ID: placeNotebookID, Word: "notebook", Current: a.page == pageNotebook},
	}
}

// The three registry rows the tabs are drawn from and routed by. They are named
// once so the word on the strip, the row that performs it and the page it swaps
// in cannot drift apart.
const (
	placeThreadID   = "key.place-thread"
	placeBoardID    = "key.place-board"
	placeNotebookID = "slash.notebook"
)

// showPage is the tab's act: swap the lens, and leave whatever room the reader
// was in.
//
// The pop is the whole difference between a page and a preview. A reader who
// asks for the board while standing inside a job would otherwise be looking at
// the board under that job's breadcrumb — the trail saying they are inside
// something the lens is not showing — which is the one thing §7's cohabitation
// rule cannot survive.
func (a *App) showPage(target page) tea.Cmd {
	if a.railModel != nil {
		a.railModel.Home()
	}
	a.showThread()
	a.composerBind = composerBind{mode: rail.ComposerChat}
	a.setPage(target)
	// ARRIVING AT A PAGE HANDS IT THE KEYBOARD, and returning to the thread
	// hands it back. It is [App.handOverTheKeyboard] read on a lens instead of
	// on a room: you talk to what you are looking at, and what a reader is
	// looking at on the board is a list they walk. The thread's lens is the one
	// you talk INTO, so the mouth takes the keys back the moment it is up.
	a.setPageFocus(target != pageThread)
	a.status.breadcrumb = a.breadcrumb()
	a.refresh()
	return nil
}

// setPageFocus moves the keyboard between the page and the composer, and tells
// the composer so it paints the state it is actually in.
//
// It is [App.focusScope] for a lens rather than for the map, and it keeps the
// same promise: the composer drawn unfocused is the picture telling the truth
// about where a keystroke will go.
func (a *App) setPageFocus(on bool) tea.Cmd {
	on = on && a.page != pageThread
	if a.pageFocus == on {
		return nil
	}
	a.pageFocus = on
	if on {
		// ONE CURSOR (5.14). The map and the page are two lists, and a window
		// where both held the keyboard would be two cursors answering one j.
		a.focusScope(false)
	}
	if a.composer != nil {
		a.composer.Focus(!on && !a.railFocus)
	}
	a.refresh()
	return nil
}

// setPage moves the lens without touching the rail or the room.
//
// It is the half [App.bind] calls: a rail gesture binds the thread's lens, and a
// reader who walked into a room from the sidebar while standing on the board has
// asked for the room, not for the board with a room's breadcrumb over it. It is
// a no-op when the page has not moved, which is what lets bind call it on every
// preview.
func (a *App) setPage(target page) {
	if a.page == target {
		return
	}
	a.page = target
	if target == pageThread {
		a.setPageFocus(false)
	}
	a.applyLens()
}

// applyLens binds the pane the current page draws into, and settles the two
// things a page swap decides besides: whether the sidebar is on the frame, and
// where the keyboard is.
//
// THE SIDEBAR IS SHUT BY DEFAULT ON EVERY PAGE, and on the work page it cannot
// be opened at all. The two halves are different rules with different reasons.
// The general one is [App.railShown]: the column reads as clutter now that its
// contents have better homes, so it is a drawer. The work page's is older and
// harder — the board IS the sidebar's list, fuller, and two copies of one list
// side by side is §15's same-fact-twice, so there is nothing there to open.
func (a *App) applyLens() {
	switch a.page {
	case pageBoard:
		// The wider read happens before the pane is bound, so the first frame of
		// the page is the whole page — a board that drew its live rows and
		// filled in its history one frame later would be the attach-time lie the
		// residency probe exists to avoid, one surface over.
		a.syncBoard()
		a.shell.SetPane(tui2.LayerTranscript, a.boardPane)
		// The map is not on screen, so it may not hold the keyboard: focusing
		// something that is not drawn is a keystroke with no visible effect
		// (5.20), and j and k belong to the board now.
		a.focusScope(false)
	case pageNotebook:
		a.shell.SetPane(tui2.LayerTranscript, a.notebookPane)
	default:
		a.shell.SetPane(tui2.LayerTranscript, a.pane)
	}
	a.applyRail()
	a.shell.Invalidate()
}

// applyRail is the ONE place the rail's presence on the frame is decided, so
// the page swap, the chord and the dock's click cannot each hold a different
// opinion about whether the column is there.
//
// It is stated as a question about the frame rather than as three assignments
// because [tui2.Shell.SetRailHidden] is idempotent and the shell re-solves only
// when the answer moved — so a caller may ask on every page swap and every
// toggle without thinking about which of them changed what.
func (a *App) applyRail() {
	a.shell.SetRailHidden(a.page == pageBoard || !a.railShown)
}

// setRailShown opens or shuts the drawer and puts the keyboard where the answer
// implies: on the map when it arrives, back on the mouth when it leaves.
//
// The two are one act rather than two calls a caller has to remember, because
// every way this is reached wants both — a rail that opened without the
// keyboard would be a column the reader then has to find a second chord for,
// and a rail that closed while still holding it would leave j and k typing into
// nothing (5.20: focusing what is not drawn is a keystroke with no effect).
func (a *App) setRailShown(on bool) tea.Cmd {
	if a.railShown != on {
		a.railShown = on
		a.applyRail()
	}
	return a.setScope(on)
}

// toggleRail is what the rail chord and the dock's click both perform: one
// three-rung ladder, walked toward the map and then away from it.
//
// The rungs are in the order a reader's intent arrives in, and each is a thing
// that is TRUE right now rather than a mode someone selected:
//
//  1. the drawer is shut — open it, and hand it the keyboard;
//  2. it is open but the keyboard is elsewhere (entering a room hands the
//     keyboard to that room's composer — rooms.go's handOverTheKeyboard) — this
//     is the chord's oldest meaning, "let me talk to the map", and it survives
//     the drawer intact;
//  3. it is open and you are standing in it — put it away.
//
// Collapsing 2 into 3 was the tempting simplification and it is the wrong one:
// it would answer "let me go back to the map" by closing the map, which is the
// exact defect the ctrl+o comment in [App.key] already describes one wave back.
func (a *App) toggleRail() tea.Cmd {
	switch {
	case a.page == pageBoard:
		// There is no drawer on the work page: the board IS the list, so there
		// is nothing to open and focusing a rail the frame has no column for
		// would be a keystroke with no visible effect (5.20). The chord never
		// arrives here — [App.key] gives it the page's own meaning — and the
		// dock is not drawn here either ([App.dockCounts]); this rung exists so
		// that neither of those two facts is the only thing holding the promise.
		return nil
	case !a.railShown:
		return a.setRailShown(true)
	case !a.railFocus:
		return a.setScope(true)
	default:
		return a.setRailShown(false)
	}
}

// pageKey offers a keystroke to the page on screen.
//
// THE PAGE KEEPS WHAT IT CLAIMS, and that is the half of this that is not
// obvious. A key the page does not recognise is NOT a letter for the composer to
// take: 13.8's first gap was the map claiming j, k, g and the digits while
// everything else fell through to the draft, so a sentence typed at the map
// arrived with its navigation keys missing — "jack knife kayak" as "ac nife
// aya", a steer that silently said something other than what was typed. One
// cursor means one keyboard. ctrl+o hands it back, and so does a click on the
// composer.
//
// The two exceptions are the two keys that mean the same thing everywhere: esc
// leaves the page (§7), and a draft that survived the swap keeps the keyboard so
// the reader can finish the sentence they had already started.
func (a *App) pageKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if a.page == pageThread || !a.pageFocus || a.overlay != overlayNone || a.drafting() {
		return nil, false
	}
	// An armed destructive verb answers first: `y` is its second yes, esc
	// stands it down, and any other key stands it down on the way to its
	// ordinary meaning (itemverb.go).
	if cmd, claimed := a.verbKey(msg); claimed {
		return cmd, true
	}
	var claimed bool
	var cmd tea.Cmd
	switch a.page {
	case pageBoard:
		cmd, claimed = a.boardKey(msg)
	case pageNotebook:
		cmd, claimed = a.notebookKey(msg)
	}
	if claimed {
		return cmd, true
	}
	// A page may hold a lens of its own — a detail page opened from a row — and
	// esc closes the innermost thing first. Only an esc the page had nothing to
	// do with leaves the page itself.
	if msg.String() == "esc" {
		return a.showPage(pageThread), true
	}
	return nil, true
}
