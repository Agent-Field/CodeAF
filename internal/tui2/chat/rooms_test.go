package chat

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/homes"
	"github.com/Agent-Field/aforge-v2/internal/tui2/palette"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The rooms lane's laws, driven through the real app.
//
// Every test here presses keys and reads frames, because the thing Wave 3 has
// to be right about is not a data structure — it is whether the surface says
// true things while a person walks around it. The rail, the main pane and the
// composer are three renderings of one cursor position (5.15), and a test that
// checked only one of them would pass through exactly the state where they
// disagree.

// -- a store with a board in it ----------------------------------------------

// boardBackend is a fakeBackend that also answers the graph and the room list,
// which is what makes it a [Graph] and a [Rooms] as well as a [Backend].
type boardBackend struct {
	fakeBackend
	nodes     []store.Node
	edges     []store.Edge
	usage     map[string]store.JobUsage
	questions []store.AgentQuestion
	sessions  []store.Session
	node      map[string][]store.Message
	opened    []string
}

func (b *boardBackend) ActiveSnapshot() (store.Snapshot, error) {
	return store.Snapshot{Nodes: b.nodes, Edges: b.edges}, nil
}

func (b *boardBackend) TopLevelJobUsage() (map[string]store.JobUsage, error) {
	return b.usage, nil
}

func (b *boardBackend) OpenQuestions(string, int) ([]store.AgentQuestion, error) {
	return b.questions, nil
}

func (b *boardBackend) NodeMessages(node string, after int64, _ int) ([]store.Message, error) {
	var out []store.Message
	for _, message := range b.node[node] {
		if message.Seq > after {
			out = append(out, message)
		}
	}
	return out, nil
}

func (b *boardBackend) Sessions() ([]store.Session, error) { return b.sessions, nil }

func (b *boardBackend) OpenSession(id, title, surface string) (store.Session, error) {
	b.opened = append(b.opened, id)
	opened := store.Session{ID: id, Title: title, Surface: surface}
	b.sessions = append([]store.Session{opened}, b.sessions...)
	b.journal++
	return opened, nil
}

func (b *boardBackend) RenameSession(id, title string) (store.Session, error) {
	for i := range b.sessions {
		if b.sessions[i].ID == id {
			b.sessions[i].Title = title
			return b.sessions[i], nil
		}
	}
	return store.Session{}, store.ErrNotFound
}

// board is one window's worth of work: a planned job with two workers, one of
// them queued behind the other, plus a settled job and a second room.
func board() *boardBackend {
	backend := &boardBackend{
		nodes: []store.Node{
			{ID: "job-1", Title: "wisp-parity", Status: store.Running, CreatedSeq: 10,
				Summary: "reworking NavCtx after the worker died"},
			{ID: "job-1/h2", Parent: "job-1", Title: "H2", Status: store.Running, CreatedSeq: 11},
			{ID: "job-1/keycutter", Parent: "job-1", Title: "KeyCutter", Status: store.Pending, CreatedSeq: 12},
			{ID: "job-2", Title: "perf-audit", Status: store.Done, CreatedSeq: 5},
		},
		edges: []store.Edge{{From: "job-1/h2", To: "job-1/keycutter", Kind: store.Blocks}},
		usage: map[string]store.JobUsage{"job-1": {Cost: 8.65}},
		sessions: []store.Session{
			{ID: testSession, Title: "the wisp parity push"},
			{ID: "session-two", Title: "importer rewrite"},
		},
		node: map[string][]store.Message{
			"job-1": {{Seq: 90, SessionID: testSession, Role: store.RoleAgent,
				NodeID: "job-1", Body: "picked up NavCtx again"}},
		},
	}
	return backend
}

func boardApp(t *testing.T) (*App, *boardBackend) {
	t.Helper()
	backend := board()
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	return app, backend
}

// press drives one keystroke through the real ladder, exactly as a terminal
// would, and runs whatever command it produced.
func press(app *App, key string) tea.Msg {
	var msg tea.KeyPressMsg
	switch key {
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		msg = tea.KeyPressMsg{Code: tea.KeyEscape}
	case "ctrl+o":
		msg = tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}
	case "ctrl+k":
		msg = tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}
	case "ctrl+u":
		msg = tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	case "up":
		msg = tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		msg = tea.KeyPressMsg{Code: tea.KeyDown}
	default:
		msg = tea.KeyPressMsg{Code: rune(key[0]), Text: key}
	}
	cmd := app.drain(app.key(msg))
	if cmd == nil {
		return nil
	}
	return cmd()
}

// rowNames is what the rail is showing, in order, with the ids it is never
// allowed to draw kept out of the comparison.
func rowNames(app *App) []string {
	rows := app.railModel.Rows()
	out := make([]string, len(rows))
	for i := range rows {
		out[i] = rows[i].Name
	}
	return out
}

// -- the scope adapter -------------------------------------------------------

// 5.24: row 0 is `aforge`, the home thread list carries a `+ new` row at its
// foot, the task cards follow, and the collapsed dim group is last.
func TestHomeScopeCarriesRoomsTasksAndTheCollapsedGroup(t *testing.T) {
	app, _ := boardApp(t)
	names := rowNames(app)
	want := []string{
		"aforge",
		"the wisp parity push", "importer rewrite", "+ new room",
		"wisp-parity", "perf-audit",
		// 5.24's collapsed group, now the real one (12.10) rather than the
		// placeholder row that cited the section and opened nothing.
		homes.GroupWord,
	}
	if len(names) != len(want) {
		t.Fatalf("home scope rows = %q, want %q", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("home scope rows = %q, want %q", names, want)
		}
	}
	if kind := app.railModel.Rows()[0].Kind; kind != rail.RowSurface {
		t.Fatalf("row 0 is %v, not the conversational surface", kind)
	}
}

// 13.3.4 / 5.14: a session id is in the never-shown tier. It may not reach the
// rail, the footer or any other cell — and a room nobody has named says so
// rather than falling back to its id.
func TestNoSessionIdReachesTheScreen(t *testing.T) {
	app, backend := boardApp(t)
	backend.sessions = append(backend.sessions, store.Session{ID: "session-nameless"})
	app.source.refresh(0, true)
	app.railModel.Refresh()
	app.refresh()

	frame := ansi.Strip(app.Frame(120, 30))
	for _, id := range []string{testSession, "session-two", "session-nameless", "job-1", "job-2"} {
		if strings.Contains(frame, id) {
			t.Fatalf("the frame drew the id %q (5.14, 13.3.4):\n%s", id, frame)
		}
	}
	if !strings.Contains(frame, untitledRoom) {
		t.Fatalf("an unnamed room did not say it was unnamed:\n%s", frame)
	}
}

// 13.3.3: the rail said "no live work" beside a reply that had just queued a
// job. A board reading that only sees `running` calls an admitted, unstarted
// job nothing at all.
func TestQueuedWorkCountsAsWork(t *testing.T) {
	backend := board()
	// Nothing is running: one job admitted, not started.
	backend.nodes = []store.Node{
		{ID: "job-3", Title: "data-clean", Status: store.Pending, CreatedSeq: 20},
	}
	backend.usage = nil
	app := newTestApp(backend, &fakeCommander{}, nil)
	poll(t, app)

	status := app.railModel.Rows()[0].Status
	if !strings.Contains(status, "queued") {
		t.Fatalf("a queued job left the head saying %q", status)
	}
	if strings.Contains(status, "nothing running") {
		t.Fatalf("the rail called a queued job nothing at all: %q", status)
	}
	frame := ansi.Strip(app.Frame(120, 30))
	if !strings.Contains(frame, "data-clean") {
		t.Fatalf("the queued job has no card:\n%s", frame)
	}
}

// The waits-on structure is what a task scope exists to make visible (5.15),
// and it is carried in NAMES, never ids.
func TestTaskScopeShowsThePlanAndItsWaits(t *testing.T) {
	app, _ := boardApp(t)
	scope, ok := app.source.Scope(rowTaskPrefix + "job-1")
	if !ok {
		t.Fatal("the planned job has no scope to descend into")
	}
	if got := rowNamesOf(scope.Rows); len(got) != 3 {
		t.Fatalf("task scope rows = %q", got)
	}
	var blocked rail.Row
	for _, row := range scope.Rows {
		if row.Name == "KeyCutter" {
			blocked = row
		}
	}
	if len(blocked.WaitsOn) != 1 || blocked.WaitsOn[0] != "H2" {
		t.Fatalf("the blocked worker does not name what it waits on: %+v", blocked.WaitsOn)
	}
	if blocked.Attention() != rail.AttnWaitsOn {
		t.Fatalf("a blocked worker reads as %v", blocked.Attention())
	}
}

// A leaf has no room to descend into, and the rail is told so by the source
// answering false rather than by a flag on the row.
func TestALeafWorkerOffersNoScope(t *testing.T) {
	app, _ := boardApp(t)
	if _, ok := app.source.Scope(rowTaskPrefix + "job-1/h2"); ok {
		t.Fatal("a leaf worker offered a scope to descend into")
	}
}

func rowNamesOf(rows []rail.Row) []string {
	out := make([]string, len(rows))
	for i := range rows {
		out[i] = rows[i].Name
	}
	return out
}

// The production bar: no per-frame source rebuild. A quiet poll must not walk
// the graph, and neither must a repaint.
func TestTheScopeIsNotRebuiltPerFrame(t *testing.T) {
	app, backend := boardApp(t)
	before := backend.reads
	for i := 0; i < 20; i++ {
		app.Frame(120, 30)
		press(app, "ctrl+o")
		press(app, "j")
		press(app, "k")
	}
	poll(t, app)
	if backend.reads > before+2 {
		t.Fatalf("frames and keystrokes cost %d store reads", backend.reads-before)
	}
}

// -- navigation --------------------------------------------------------------

// 5.15: one cursor, and ctrl+o is the one chord in and out of the map. While
// the composer has focus, j and k are letters.
func TestTheMapTakesTheKeyboardOnlyWhenItHasFocus(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "j")
	if got := app.composer.Draft(); got != "j" {
		t.Fatalf("j did not reach the composer: draft = %q", got)
	}
	if app.railModel.Cursor() != 0 {
		t.Fatal("a letter moved the rail cursor while the composer had focus")
	}

	press(app, "ctrl+o")
	press(app, "j")
	if app.railModel.Cursor() != 1 {
		t.Fatalf("j did not move the focused rail: cursor = %d", app.railModel.Cursor())
	}
	if got := app.composer.Draft(); got != "j" {
		t.Fatalf("the map's j reached the draft too: %q", got)
	}
}

func TestArrowsAndDigitsMoveTheCursor(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "down")
	press(app, "down")
	if app.railModel.Cursor() != 2 {
		t.Fatalf("arrows moved to %d", app.railModel.Cursor())
	}
	press(app, "up")
	if app.railModel.Cursor() != 1 {
		t.Fatalf("up moved to %d", app.railModel.Cursor())
	}
	press(app, "5")
	if app.railModel.Cursor() != 4 {
		t.Fatalf("the digit 5 jumped to row %d, not the fifth", app.railModel.Cursor())
	}
	if name := app.railModel.Selected().Name; name != "wisp-parity" {
		t.Fatalf("row 5 is %q", name)
	}
}

// Enter on a task re-scopes the rail to that task's DAG and swaps the main pane
// to its room; esc pops the scope, never just the selection.
func TestEnterDescendsAndEscPops(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "5")
	press(app, "enter")

	if app.railModel.Depth() != 1 {
		t.Fatalf("enter did not re-scope: depth = %d", app.railModel.Depth())
	}
	if crumbs := app.railModel.Breadcrumb(); len(crumbs) != 2 || crumbs[1] != "wisp-parity" {
		t.Fatalf("breadcrumb = %q", crumbs)
	}
	if app.view == nil || app.view.kind != viewNode || app.view.node != "job-1" {
		t.Fatalf("the main pane is not the task's room: %+v", app.view)
	}
	if names := rowNames(app); len(names) != 3 || names[0] != "wisp-parity" {
		t.Fatalf("the task scope rows are %q", names)
	}

	press(app, "esc")
	if app.railModel.Depth() != 0 {
		t.Fatalf("esc did not pop the scope: depth = %d", app.railModel.Depth())
	}
	press(app, "esc")
	if app.railFocus {
		t.Fatal("esc at home did not hand the keyboard back to the conversation")
	}
}

// A task room's transcript is the node-anchored trail, dressed by the same
// renderers the conversation uses (4.6: a view over the same journal).
func TestATaskRoomShowsTheNodeTrail(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "5")
	msg := press(app, "enter")
	trail, ok := msg.(nodeMessagesMsg)
	if !ok {
		t.Fatalf("enter did not read the node trail: %T", msg)
	}
	app.applyNodeMessages(trail)

	frame := ansi.Strip(app.Frame(120, 30))
	if !strings.Contains(frame, "picked up NavCtx again") {
		t.Fatalf("the task room does not show its trail:\n%s", frame)
	}
}

// -- the composer binding ----------------------------------------------------

// 5.15's one rule, all three cases: the head chats, a worker takes steering
// mail, and settled work refuses the draft and says why.
func TestTheComposerBindsWhatTheCursorRestsOn(t *testing.T) {
	app, _ := boardApp(t)
	if got := app.composerMode().mode; got != rail.ComposerChat {
		t.Fatalf("home binds %v", got)
	}

	press(app, "ctrl+o")
	press(app, "5")
	press(app, "enter")
	press(app, "j")
	if bind := app.composerMode(); bind.mode != rail.ComposerSteer || bind.node != "job-1/h2" {
		t.Fatalf("a worker row binds %+v", bind)
	}

	press(app, "esc")
	press(app, "6")
	if bind := app.composerMode(); bind.mode != rail.ComposerDisabled ||
		!strings.Contains(bind.note, "settled") {
		t.Fatalf("a settled task binds %+v", bind)
	}
}

// The prompt glyph previews the composer the row bound, and the card's mark
// promised the same glyph — so the affordance cannot lie (5.11).
func TestTheSteerLineDrawsItsOwnPromptAndHint(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "5")
	press(app, "enter")
	press(app, "j")

	frame := ansi.Strip(app.Frame(120, 30))
	if !strings.Contains(frame, tokens.GlyphPromptSteer) {
		t.Fatalf("the steer line has no steer prompt:\n%s", frame)
	}
	if !strings.Contains(frame, steerPlaceholder) {
		t.Fatalf("the steer line kept the chat placeholder:\n%s", frame)
	}
}

// A disabled composer takes nothing at all. A draft nobody could send is worse
// than no draft: the reader finds out after typing.
func TestADisabledComposerRefusesTheKeyboardAndSaysWhy(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "6")
	press(app, "esc")
	press(app, "h")
	press(app, "i")
	if got := app.composer.Draft(); got != "" {
		t.Fatalf("a disabled composer took a draft: %q", got)
	}
	frame := ansi.Strip(app.Frame(120, 30))
	if !strings.Contains(frame, "ask aforge") {
		t.Fatalf("a disabled composer did not say why:\n%s", frame)
	}
}

// A steered draft becomes a node-anchored user message through the one door
// every writer uses — the same verb the old window's steer line posts.
func TestASteeredDraftIsJournaledAgainstItsNode(t *testing.T) {
	app, backend := boardApp(t)
	press(app, "ctrl+o")
	press(app, "5")
	press(app, "enter")
	press(app, "j")
	// ctrl+o and not esc: esc pops the SCOPE (5.15), which would take the
	// cursor off the worker the draft is aimed at. Leaving the map is the other
	// chord, and the binding survives it — that is the point.
	press(app, "ctrl+o")

	for _, key := range strings.Split("skip", "") {
		press(app, key)
	}
	msg := press(app, "enter")
	result, ok := msg.(steerResultMsg)
	if !ok {
		t.Fatalf("the steer did not reach the journal: %T", msg)
	}
	if result.message.NodeID != "job-1/h2" || result.message.Body != "skip" {
		t.Fatalf("the steer landed as %+v", result.message)
	}
	if len(backend.posted) != 1 {
		t.Fatalf("the journal took %d writes", len(backend.posted))
	}
}

// -- narrow and wide ---------------------------------------------------------

// Part 9.12: scope is not a rail feature. The same rows, the same keys and the
// same selection render as a column at 120 and as the main pane at 80.
func TestNarrowAndWideShowTheSameScope(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "3")

	wide := ansi.Strip(app.Frame(120, 30))
	narrow := ansi.Strip(app.Frame(80, 30))
	for _, name := range []string{"aforge", "the wisp parity push", "+ new room", "wisp-parity"} {
		if !strings.Contains(wide, name) {
			t.Fatalf("the wide rail is missing %q:\n%s", name, wide)
		}
		if !strings.Contains(narrow, name) {
			t.Fatalf("the narrow scope pane is missing %q:\n%s", name, narrow)
		}
	}
	if app.railModel.Cursor() != 2 {
		t.Fatalf("the cursor moved across the breakpoint: %d", app.railModel.Cursor())
	}
}

// The bounded HUD (8.2.8) is the NARROW fallback and never a second rail. It
// carries work and never the navigation rows.
func TestTheHudIsBoundedNarrowAndCarriesOnlyWork(t *testing.T) {
	app, _ := boardApp(t)
	app.termWidth = 80
	rows := app.hudRows(80, 8)
	if len(rows) == 0 {
		t.Fatal("a window with running work drew no bounded summary")
	}
	if len(rows) > tokens.HUDRowCap {
		t.Fatalf("the HUD exceeded its cap: %d rows", len(rows))
	}
	joined := ansi.Strip(strings.Join(rows, "\n"))
	if !strings.Contains(joined, "wisp-parity") {
		t.Fatalf("the HUD does not carry the live work:\n%s", joined)
	}
	for _, navigation := range []string{"+ new room", "more", "importer rewrite"} {
		if strings.Contains(joined, navigation) {
			t.Fatalf("the HUD counted the navigation row %q as work:\n%s", navigation, joined)
		}
	}

	app.termWidth = 120
	if rows := app.hudRows(120, 8); len(rows) != 0 {
		t.Fatalf("the HUD drew beside a rail:\n%s", strings.Join(rows, "\n"))
	}
}

// -- the room switcher -------------------------------------------------------

// 12.1.3 / 5.24: `+ new` is the visible door, and it mints a room through the
// one store call that makes an empty room exist.
func TestTheNewRoomRowMintsARoomAndSwitchesToIt(t *testing.T) {
	app, backend := boardApp(t)
	press(app, "ctrl+o")
	press(app, "4")
	if name := app.railModel.Selected().Name; name != "+ new room" {
		t.Fatalf("row 4 is %q", name)
	}
	msg := press(app, "enter")
	opened, ok := msg.(roomOpenedMsg)
	if !ok {
		t.Fatalf("enter on the new-room row produced %T", msg)
	}
	app.applyRoomOpened(opened)

	if len(backend.opened) != 1 {
		t.Fatalf("the store minted %d rooms", len(backend.opened))
	}
	if app.session != backend.opened[0] {
		t.Fatalf("the window is in %q, not the room it minted", app.session)
	}
	if app.transcript.Len() != 0 {
		t.Fatal("a fresh room inherited the old room's transcript")
	}
	if app.railFocus {
		t.Fatal("a minted room did not hand the keyboard back to the composer")
	}
}

// Switching rooms switches the conversation: the transcript, the watermark and
// the journal claim all belong to the room they were read for.
func TestSwitchingRoomsResetsTheConversation(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "3")
	press(app, "enter")

	if app.session != "session-two" {
		t.Fatalf("the window is in %q", app.session)
	}
	if app.watermark != 0 || app.journal != 0 {
		t.Fatalf("the switch carried the old room's watermarks: %d/%d", app.watermark, app.journal)
	}
	if bind := app.composerMode(); bind.mode != rail.ComposerChat {
		t.Fatalf("the new room's composer binds %+v", bind)
	}
	if app.view != nil {
		t.Fatal("the main pane is not the new room's conversation")
	}
}

// A room the cursor is only PREVIEWING is not the room the composer talks to.
// 5.15's one rule has no exceptions, and this is the case it is easiest to get
// wrong: the row looks like a chat and is not one yet.
func TestPreviewingAnotherRoomDoesNotBindItsComposer(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "3")
	if bind := app.composerMode(); bind.mode != rail.ComposerDisabled ||
		!strings.Contains(bind.note, "enter") {
		t.Fatalf("a previewed room binds %+v", bind)
	}
	if app.session != testSession {
		t.Fatal("a preview moved the window into another room")
	}
}

// -- the overlay plane -------------------------------------------------------

func TestTheJumpPaletteOpensAndCloses(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+k")
	if app.overlay != overlayPalette {
		t.Fatalf("ctrl+k raised %v", app.overlay)
	}
	if !app.shell.OverlayOpen() {
		t.Fatal("the overlay plane is not raised")
	}
	frame := ansi.Strip(app.Frame(120, 30))
	if !strings.Contains(frame, "wisp-parity") {
		t.Fatalf("the palette lists no rooms:\n%s", frame)
	}
	press(app, "esc")
	if app.overlay != overlayNone || app.shell.OverlayOpen() {
		t.Fatalf("esc left the plane at %v", app.overlay)
	}
}

func TestThePaletteJumpsToARoom(t *testing.T) {
	app, _ := boardApp(t)
	if cmd := app.choose(palette.JumpToRoom{ID: rowTaskPrefix + "job-1"}); cmd != nil {
		cmd()
	}
	if name := app.railModel.Selected().Name; name != "wisp-parity" {
		t.Fatalf("the jump landed on %q", name)
	}
	if app.view == nil || app.view.node != "job-1" {
		t.Fatalf("the jump did not open the room: %+v", app.view)
	}
}

// 5.20 rule 3: `?` lists what THIS room can do, and a disabled row says why.
func TestTheCapabilitySheetNamesTheRoomAndItsRefusals(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "5")
	press(app, "enter")
	press(app, "?")
	if app.overlay != overlayCapability {
		t.Fatalf("? raised %v", app.overlay)
	}
	frame := ansi.Strip(app.Frame(120, 30))
	if !strings.Contains(frame, "wisp-parity") {
		t.Fatalf("the sheet does not name the room it describes:\n%s", frame)
	}
	if reason := app.entryReason("key.thread.receipts"); reason == "" {
		t.Fatal("an unavailable action offered no reason (5.20 rule 3)")
	}
}

// 13.5 finding 2: the footer draws `? help` on every frame, so the key has to
// fire from the state a reader is actually in — the composer, with nothing
// typed. 5.20 rule 3 writes the door as a bare `?` "in any room"; 12.12.6 says
// where a bare key may be claimed: "empty draft, no overlay, rail unfocused".
func TestTheHelpDoorOpensFromTheComposerOnAnEmptyDraft(t *testing.T) {
	app, _ := boardApp(t)
	if app.railFocus {
		t.Fatal("the window opened with the map holding the keyboard")
	}
	press(app, "?")
	if app.overlay != overlayCapability {
		t.Fatalf("? from the composer raised %v, want the capability sheet", app.overlay)
	}
	if app.composer.Draft() != "" {
		t.Fatalf("the help key also landed in the draft: %q", app.composer.Draft())
	}
}

// The other half of the same law: a sentence in progress owns the rune. `?` is
// a character before it is a door, and the composer already distinguishes an
// empty draft from a written one for esc.
func TestTheHelpKeyIsATypedCharacterInsideASentence(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "w")
	press(app, "h")
	press(app, "y")
	press(app, "?")
	if app.overlay != overlayNone {
		t.Fatalf("? inside a sentence raised %v", app.overlay)
	}
	if got := app.composer.Draft(); got != "why?" {
		t.Fatalf("draft = %q, want %q", got, "why?")
	}
}

// 13.5 finding 4 / 5.15: closing an overlay puts the keyboard back where it
// was. Opened from the composer, esc must leave the reader talking to the
// conversation — Enter sends the draft, it does not open a rail row.
func TestClosingAnOverlayLeavesTheKeyboardOnTheComposer(t *testing.T) {
	for _, door := range []string{"?", "ctrl+k"} {
		t.Run(door, func(t *testing.T) {
			app, backend := boardApp(t)
			press(app, door)
			if app.overlay == overlayNone {
				t.Fatalf("%q opened nothing", door)
			}
			press(app, "esc")
			if app.overlay != overlayNone {
				t.Fatalf("esc left the plane at %v", app.overlay)
			}
			if app.railFocus {
				t.Fatal("closing the sheet handed the keyboard to the scope map")
			}
			press(app, "h")
			press(app, "i")
			if got := app.composer.Draft(); got != "hi" {
				t.Fatalf("typing after the sheet closed gave %q", got)
			}
			press(app, "enter")
			if len(backend.posted) != 1 || backend.posted[0].Body != "hi" {
				t.Fatalf("enter posted %+v, want the draft", backend.posted)
			}
		})
	}
}

// And the same law read from the other side: opened from the map, closing it
// gives the map the keyboard back. Where it was is where it goes.
func TestClosingAnOverlayLeavesTheKeyboardOnTheMapItWasOpenedFrom(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "?")
	if app.overlay != overlayCapability {
		t.Fatalf("? from the map raised %v", app.overlay)
	}
	press(app, "esc")
	if !app.railFocus {
		t.Fatal("closing the sheet took the keyboard off the map that opened it")
	}
}

// 13.5 finding 3: ctrl+u clears the draft, and the `?` sheet lists it — a key
// that is not in the one catalog is a typed-only action (5.22).
func TestCtrlUClearsTheDraftAndIsInTheCatalog(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "d")
	press(app, "o")
	press(app, "h")
	if app.composer.Draft() != "doh" {
		t.Fatalf("draft = %q", app.composer.Draft())
	}
	press(app, "ctrl+u")
	if got := app.composer.Draft(); got != "" {
		t.Fatalf("ctrl+u left %q in the draft", got)
	}
	entry, ok := registry.ByID("key.thread.clear-draft")
	if !ok {
		t.Fatal("the clear-draft verb is in no catalog")
	}
	if bound, ok := entry.On(registry.SurfaceComposerFirst); !ok || bound.Key != "ctrl+u" {
		t.Fatalf("the catalog offers %q on a composer-first surface, want ctrl+u", bound.Key)
	}
	// It is in the sheet the reader can actually open, with its accelerator.
	press(app, "?")
	sheet := ansi.Strip(app.Frame(120, 60))
	if !strings.Contains(sheet, "clear draft") || !strings.Contains(sheet, "ctrl+u") {
		t.Fatalf("the `?` sheet does not teach the clear-draft key:\n%s", sheet)
	}
}

// The palette row is a door, not a label: choosing it performs the same edit
// the chord does (5.22 rule 5 — an affordance never lies).
func TestTheClearDraftRowPerformsTheEdit(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "n")
	press(app, "o")
	if cmd := app.choose(palette.RunEntry{ID: "key.thread.clear-draft"}); cmd != nil {
		cmd()
	}
	if got := app.composer.Draft(); got != "" {
		t.Fatalf("the palette row left %q in the draft", got)
	}
	// The row is never dimmed for an empty draft: a disabled row renders its
	// reason instead of its key, and `?` from the composer only opens on an
	// empty draft — so a reason here would hide the chord from every reader who
	// could have read it (see entryReason's own note).
	if reason := app.entryReason("key.thread.clear-draft"); reason != "" {
		t.Fatalf("the clear-draft row went dim and took its key with it: %q", reason)
	}
}

// -- honesty under a backend that cannot answer ------------------------------

// A backend that is only a Backend — no graph, no room list — still gets a
// working rail. The doors that are not built yet render; they simply open
// nothing (5.24).
func TestAPlainBackendStillGetsAnHonestRail(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	poll(t, app)
	names := rowNames(app)
	if len(names) < 3 || names[0] != "aforge" {
		t.Fatalf("a graphless window has rail %q", names)
	}
	if got := app.Frame(120, 30); strings.Contains(got, "no live work") {
		t.Fatal("the stub rail survived the wave that replaced it")
	}
	press(app, "ctrl+o")
	press(app, "j")
	press(app, "enter")
	press(app, "esc")
	if app.railModel.Depth() != 0 {
		t.Fatal("a graphless rail descended into a room that does not exist")
	}
}

// Every width and every depth, without a panic and without an over-wide row.
func TestTheRoomsSurfaceSurvivesEveryWidth(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "5")
	press(app, "enter")
	for width := 1; width <= 130; width++ {
		frame := app.Frame(width, 12)
		for _, row := range strings.Split(frame, "\n") {
			if got := ansi.StringWidth(row); got > width {
				t.Fatalf("width %d produced a %d-cell row: %q", width, got, row)
			}
		}
	}
}

// The clock the rail reads is the app's, so an elapsed cell in a card is a
// function of the journal and the injected time alone.
func TestTheCardClockIsTheAppsClock(t *testing.T) {
	backend := board()
	backend.nodes[1].StartedAt = fixedNow().Add(-30 * time.Minute)
	app := newTestApp(backend, &fakeCommander{}, nil)
	poll(t, app)
	for _, row := range app.railModel.Rows() {
		if row.Name != "wisp-parity" {
			continue
		}
		if !row.Meta.HasElapsed || row.Meta.Elapsed != 30*time.Minute {
			t.Fatalf("the card's elapsed is %v (has=%v)", row.Meta.Elapsed, row.Meta.HasElapsed)
		}
		return
	}
	t.Fatal("no card to read a clock off")
}

// A task room is a view over the SUBTREE, not over the root node's own trail.
// A planned job's root usually says nothing — its parts do the work and its
// workers do the talking — so a room that read only the root drew a title and
// a status line and looked like a job with nothing in it.
func TestATaskRoomShowsTheWholeSubtreesTrail(t *testing.T) {
	app, backend := boardApp(t)
	backend.node["job-1/h2"] = []store.Message{{Seq: 91, SessionID: testSession,
		Role: store.RoleAgent, NodeID: "job-1/h2", Body: "H2 is green"}}
	backend.node["job-1/keycutter"] = []store.Message{{Seq: 92, SessionID: testSession,
		Role: store.RoleAgent, NodeID: "job-1/keycutter", Body: "KeyCutter waiting on H2"}}
	backend.journal++
	poll(t, app)

	press(app, "ctrl+o")
	for range 10 {
		if app.view != nil && app.view.kind == viewNode {
			break
		}
		press(app, "down")
		press(app, "enter")
	}
	if app.view == nil || app.view.kind != viewNode {
		t.Fatalf("never reached a task room: %#v", app.view)
	}
	if msg, ok := run(t, app.readNodeCmd(app.view.node, 0)).(nodeMessagesMsg); ok {
		app.applyNodeMessages(msg)
	} else {
		t.Fatal("the task room read did not answer with messages")
	}
	out := shown(t, app, 80)
	for _, want := range []string{"picked up NavCtx again", "H2 is green", "KeyCutter waiting on H2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the task room is missing %q:\n%s", want, out)
		}
	}
}

// The subtree read is derived from the scope the rail already built, so it can
// never disagree with what the room shows, and it is bounded by that scope.
func TestTheSubtreeReadFollowsTheRailsOwnScope(t *testing.T) {
	app, _ := boardApp(t)
	nodes := app.source.subtreeNodes("job-1")
	want := map[string]bool{"job-1": true, "job-1/h2": true, "job-1/keycutter": true}
	if len(nodes) != len(want) {
		t.Fatalf("subtree = %q, want the three of job-1", nodes)
	}
	for _, id := range nodes {
		if !want[id] {
			t.Fatalf("subtree carries %q, which is not job-1's", id)
		}
	}
	if nodes[0] != "job-1" {
		t.Fatalf("the root is not first: %q", nodes)
	}
	// A leaf answers with itself: that is exactly its own trail.
	if leaf := app.source.subtreeNodes("job-1/h2"); len(leaf) != 1 || leaf[0] != "job-1/h2" {
		t.Fatalf("a leaf's subtree = %q", leaf)
	}
}

// 5.20 rule 5's UI duty is rendering receipts in the room the reader is looking
// at, and the fold is the same rule as a keystroke. It used to walk the
// conversation's block list from inside another room, so it opened rows nobody
// could see and left the ones on screen shut.
func TestTheReceiptsFoldActsOnTheRoomOnScreen(t *testing.T) {
	app, _ := boardApp(t)
	receipt := store.Message{Seq: 500, SessionID: testSession, Role: store.RoleSystem,
		Body: "reflected\nthe long half nobody reads until they do"}
	// One receipt in the room's own conversation, one in the room on screen.
	app.transcript.Append(newMessageBlock(receipt, app.style))
	view := blocks.New(80, 24)
	other := receipt
	other.Seq = 501
	view.Append(newMessageBlock(other, app.style))
	app.view = &mainView{kind: viewNode, node: "job-1", title: "wisp-parity", transcript: view}
	app.pane.transcript = view

	if before := shown(t, app, 80); strings.Contains(before, "nobody reads") {
		t.Fatalf("the receipt was already open:\n%s", before)
	}
	app.toggleReceipts()
	if after := shown(t, app, 80); !strings.Contains(after, "nobody reads") {
		t.Fatalf("ctrl+r did not open the room on screen:\n%s", after)
	}
}

// shown is [whole] over the transcript the main pane is actually drawing, which
// is not the room's own once a task room or a preview card is open.
func shown(t *testing.T, app *App, width int) string {
	t.Helper()
	transcript := app.pane.transcript
	if transcript == nil {
		transcript = app.transcript
	}
	var out strings.Builder
	for i := 0; i < transcript.Len(); i++ {
		for _, row := range transcript.Block(i).Rows(width) {
			out.WriteString(ansi.Strip(row))
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// 5.24's four rooms, as rail scopes. The placeholder row that named them and
// opened nothing is gone; the group is a LID, so enter expands it in place
// rather than descending into a fifth surface.
func TestTheHomesGroupIsALidAndItsRoomsAreScopes(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	if _, found := app.railModel.SelectID(homes.GroupRowID); !found {
		t.Fatalf("the rail carries no homes group: %q", rowNames(app))
	}
	before := len(rowNames(app))
	press(app, "enter")
	after := rowNames(app)
	if len(after) <= before {
		t.Fatalf("enter on the lid did not expand it: %q", after)
	}
	for _, word := range []string{"notebook", "self", "standing", "services"} {
		if !contains(after, word) {
			t.Fatalf("the expanded group is missing %q: %q", word, after)
		}
	}
	// The lid closes again in place.
	press(app, "enter")
	if len(rowNames(app)) != before {
		t.Fatalf("enter did not close the lid: %q", rowNames(app))
	}
}

// A service is not a conversation. The row's own ComposerNone cannot say so —
// composerMode coerces None to Chat — so this side must disable it by name.
func TestAServiceRowGetsADisabledComposer(t *testing.T) {
	app, _ := boardApp(t)
	app.bind(rail.Event{RowID: homes.ServiceRowPrefix + "watcher"}, false)
	if app.composerBind.mode != rail.ComposerDisabled {
		t.Fatalf("a service row bound composer mode %v", app.composerBind.mode)
	}
	if app.composerMode().mode != rail.ComposerDisabled {
		t.Fatal("the coercion at composerMode reached a service row")
	}
	if app.composerBind.note == "" {
		t.Fatal("a disabled composer with no words is a dead end (5.20 rule 3)")
	}
}

// A home is a room the palette can jump to by name; the LID is not one.
func TestThePaletteJumpsToAHomeButNotToTheLid(t *testing.T) {
	app, _ := boardApp(t)
	app.source.homes.Expanded = true
	app.source.refresh(app.journal, true)
	app.railModel.Refresh()
	var ids []string
	for _, room := range app.catalogRooms() {
		ids = append(ids, room.ID)
	}
	if !contains(ids, homes.HomeNotebook.ScopeID()) {
		t.Fatalf("the palette cannot reach the notebook: %q", ids)
	}
	if contains(ids, homes.GroupRowID) {
		t.Fatalf("the palette offered the lid as a room: %q", ids)
	}
}

func contains(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}

// THE DEFECT: entering a task knew LESS about it than the card you entered
// from. The card falls back to "1 part running" when the root itself says
// nothing (13.3.3) and carries the cost, the elapsed and the atomic mark;
// taskScope built row 0 from the node a second time and carried none of it. On a
// job root — which "usually carries no status of its own worth showing", as
// taskCard's own comment says — the difference was the whole row.
//
// 5.15 makes the card a PREVIEW of the room, and a preview that outranks the
// thing it previews is the affordance lying in the one direction nobody checks.
func TestAnEnteredRoomKnowsWhatItsCardKnew(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")

	for _, tc := range []struct{ digit, name string }{
		{"5", "wisp-parity"},
		{"6", "perf-audit"}, // atomic, settled: the shape the screenshot caught
	} {
		press(app, tc.digit)
		card := app.railModel.Selected()
		if card.Name != tc.name {
			t.Fatalf("digit %s selected %q, want %q", tc.digit, card.Name, tc.name)
		}
		press(app, "enter")
		surface := app.railModel.Rows()[0]

		if surface.Kind != rail.RowSurface {
			t.Fatalf("%s: row 0 is %v, not the conversational surface", tc.name, surface.Kind)
		}
		if surface.Status != card.Status {
			t.Fatalf("%s: the room says %q and the card said %q", tc.name, surface.Status, card.Status)
		}
		if surface.Meta != card.Meta {
			t.Fatalf("%s: the room's telemetry is %+v and the card's was %+v",
				tc.name, surface.Meta, card.Meta)
		}
		if surface.Life != card.Life || surface.Seed != card.Seed {
			t.Fatalf("%s: the room's lifecycle or identity moved: %+v vs %+v", tc.name, surface, card)
		}
		press(app, "esc")
	}
}

// THE DEFECT: a task whose subtree has journaled nothing rendered a completely
// blank main pane — the same picture an unwired room draws, which is exactly
// what 12.10 warns must never happen. A room that is empty has to SAY it is
// empty (5.20 rule 1).
func TestAnEmptyTaskRoomSaysSoRatherThanDrawingNothing(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "6") // perf-audit: no rows under it in the fixture
	msg := press(app, "enter")

	if app.view == nil || app.view.kind != viewNode {
		t.Fatalf("enter did not open the room: %+v", app.view)
	}
	if trail, ok := msg.(nodeMessagesMsg); ok {
		app.applyNodeMessages(trail)
	}

	// The note wraps at transcript width, so the assertion is on the phrase
	// that carries the meaning rather than on the whole sentence.
	frame := ansi.Strip(app.Frame(120, 30))
	if !strings.Contains(frame, "nothing journaled here yet") {
		t.Fatalf("an empty room drew no teaching line:\n%s", frame)
	}
	// And it is not empty of the task either: the card the reader entered from
	// is still on screen, so the room is never poorer than the preview.
	if !strings.Contains(frame, "perf-audit") {
		t.Fatalf("the empty room forgot which task it is:\n%s", frame)
	}

	// The teaching is only on screen while it is true: the first journaled row
	// retires it.
	app.applyNodeMessages(nodeMessagesMsg{node: "job-2", messages: []store.Message{
		{Seq: 400, SessionID: testSession, Role: store.RoleAgent, NodeID: "job-2",
			Body: "the audit finished"},
	}})
	frame = ansi.Strip(app.Frame(120, 30))
	if strings.Contains(frame, "nothing journaled here yet") {
		t.Fatalf("the teaching line outlived the emptiness it described:\n%s", frame)
	}
	if !strings.Contains(frame, "the audit finished") {
		t.Fatalf("the room did not take its first row:\n%s", frame)
	}
}

// THE DEFECT: esc out of a room re-opened it. A pop commits, the committed row
// is the one just left, and openTaskRoom sees the same node and returns early —
// so the main pane stayed in the room the reader had escaped from, at home, for
// the rest of the session. 5.15: "selection previews; enter opens", and esc is a
// movement of the map.
func TestEscOutOfARoomLeavesIt(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "5")
	press(app, "enter")
	if app.view == nil || app.view.kind != viewNode || app.view.node != "job-1" {
		t.Fatalf("enter did not open the room: %+v", app.view)
	}

	press(app, "esc")
	if app.railModel.Depth() != 0 {
		t.Fatalf("esc did not pop the scope: depth = %d", app.railModel.Depth())
	}
	if app.view != nil && app.view.kind == viewNode {
		t.Fatalf("the main pane stayed in the room esc left: %+v", app.view)
	}
	frame := ansi.Strip(app.Frame(120, 30))
	if !strings.Contains(frame, "wisp-parity") {
		t.Fatalf("popping back lost the card the cursor is on:\n%s", frame)
	}
}

// The same rule, one floor down: esc out of a home must not bounce straight back
// into it. The homes branch of bind calls Enter again on a commit, so a
// committing pop re-entered the room it had just left.
func TestEscOutOfAHomeLeavesIt(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "end")
	if app.railModel.Selected().ID != homes.GroupRowID {
		t.Fatalf("the last row is %q, not the homes group", app.railModel.Selected().ID)
	}
	press(app, "enter") // the group is a lid: it expands in place
	press(app, "down")
	press(app, "enter") // into the first home
	depth := app.railModel.Depth()
	if depth == 0 {
		t.Fatalf("enter did not descend into a home")
	}

	press(app, "esc")
	if got := app.railModel.Depth(); got >= depth {
		t.Fatalf("esc left the depth at %d, want less than %d — the pop re-entered", got, depth)
	}
}
