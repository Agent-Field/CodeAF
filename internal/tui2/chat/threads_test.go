package chat

import (
	"image"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/palette"
)

// The chats lane's laws, driven through the real app.
//
// The thing this wave has to be right about is not a data structure: it is
// whether a person can tell which conversation they are in, get to another one
// in one gesture, and be moved by the head without the two rooms' rows ever
// mixing. So every test here presses a key or clicks a cell and reads a frame.

// -- the fixture ---------------------------------------------------------------

// threadsBackend is [boardBackend] plus the one read the switcher's fallback
// needs and the message log cannot answer: where each room's last non-user row
// sits. *store.Store answers it with one indexed aggregate; this answers it by
// walking the fixture, which is the same fact by a slower route.
type threadsBackend struct {
	*boardBackend
}

func (b *threadsBackend) SessionLastNonUserMessageSeq(session string) (int64, error) {
	var seq int64
	for _, message := range b.messages {
		if message.SessionID == session && message.Role != store.RoleUser && message.Seq > seq {
			seq = message.Seq
		}
	}
	return seq, nil
}

// threadsApp is a window over two named threads, each with a line in it.
func threadsApp(t *testing.T) (*App, *threadsBackend) {
	t.Helper()
	board := board()
	board.sessions = []store.Session{
		{ID: testSession, Title: "the wisp parity push",
			LastActive: fixedNow().Add(-2 * time.Hour)},
		{ID: "session-two", Title: "importer rewrite",
			LastActive: fixedNow().Add(-30 * time.Hour)},
	}
	backend := &threadsBackend{boardBackend: board}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleUser,
		Body: "how is the parity push going"})
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent,
		Body: "the diff is ready when you are"})
	backend.add(store.Message{SessionID: "session-two", Role: store.RoleAgent,
		Body: "parked on the schema question"})
	app := newTestApp(backend, &fakeCommander{model: "anthropic/claude-k3"}, nil)
	poll(t, app)
	return app, backend
}

// -- the title chip ------------------------------------------------------------

// The chip is always on the bar row and it is the thread's NAME. A reader who
// looks down should be able to say which conversation they are in without
// navigating anywhere.
func TestTheTitleChipNamesTheThread(t *testing.T) {
	app, _ := threadsApp(t)
	row := ansi.Strip(app.status.Render(120, 1))
	if !strings.Contains(row, "the wisp parity push") {
		t.Fatalf("the bar row does not name the thread:\n%q", row)
	}
}

// SILENCE OVER MACHINERY. A thread the scribe has not named yet shows nothing
// rather than an id (13.3.4) — and nothing rather than a placeholder either.
func TestAnUnnamedThreadDrawsNoChipAtAll(t *testing.T) {
	backend := &threadsBackend{boardBackend: board()}
	backend.sessions = []store.Session{{ID: testSession}}
	app := newTestApp(backend, &fakeCommander{}, nil)
	poll(t, app)
	row := ansi.Strip(app.status.Render(120, 1))
	if strings.Contains(row, testSession) {
		t.Fatalf("the session id reached the bar row:\n%q", row)
	}
	if got := app.status.thread; got != "" {
		t.Fatalf("an unnamed thread produced the chip text %q", got)
	}
}

// The chip is a DOOR, and it is the same door the key opens. 5.22's parity
// clause, at the one word on the bar row that opens a list.
func TestClickingTheTitleChipOpensTheSwitcher(t *testing.T) {
	app, _ := threadsApp(t)
	const width = 120
	_ = app.Frame(width, 30)

	targets := app.status.bar.Targets(app.status.focusContext(width), width)
	at := -1
	for _, target := range targets {
		if target.ID == "footer:thread" {
			at = target.From
		}
	}
	if at < 0 {
		t.Fatalf("the title chip is not a click target: %+v", targets)
	}
	app.status.Mouse(clickAt(at, 0), image.Point{X: at})
	if app.overlay != overlayThreads {
		t.Fatalf("clicking the chip raised overlay %d, want the switcher", app.overlay)
	}
}

// -- opening the switcher ------------------------------------------------------

// THE BARE `t` IS A LETTER WHILE THE COMPOSER HOLDS THE KEYBOARD, and this is
// the test that keeps it one. `t` opens roughly a third of English sentences;
// a chat surface that answered the first keystroke of "the diff looks right"
// with a thread list would be unusable, which is the whole reason the bare key
// lives on the map instead.
func TestTheBareTIsALetterWhileTheComposerHoldsTheKeyboard(t *testing.T) {
	app, _ := threadsApp(t)
	_ = app.Frame(120, 30)

	typeInto(app, "the diff looks right")
	if app.overlay != overlayNone {
		t.Fatalf("typing a sentence raised overlay %d", app.overlay)
	}
	if got := app.composer.Draft(); got != "the diff looks right" {
		t.Fatalf("the draft took %q — the switcher ate the sentence", got)
	}
}

// And it IS the door while the map holds the keyboard, where a bare letter is
// navigation rather than text.
func TestTheBareTOpensTheSwitcherFromTheMap(t *testing.T) {
	app, _ := threadsApp(t)
	_ = app.Frame(120, 30)
	press(app, "ctrl+o")
	if !app.railFocus {
		t.Fatal("the fixture did not put the keyboard on the map")
	}
	press(app, threadsKey)
	if app.overlay != overlayThreads {
		t.Fatalf("`t` from the map raised overlay %d", app.overlay)
	}
	press(app, "esc")
	if app.overlay != overlayNone {
		t.Fatal("esc did not close the switcher")
	}
}

// And the chord means the same thing mid-sentence, which is what a chord is for.
func TestTheChordOpensTheSwitcherMidSentence(t *testing.T) {
	app, _ := threadsApp(t)
	_ = app.Frame(120, 30)
	typeInto(app, "half a thought")
	press(app, threadsChord)
	if app.overlay != overlayThreads {
		t.Fatalf("%s raised overlay %d", threadsChord, app.overlay)
	}
	if got := app.composer.Draft(); got != "half a thought" {
		t.Fatalf("the chord disturbed the draft: %q", got)
	}
}

// The list the switcher opens on is this window's threads, with the line each
// was left on — the fallback read, since the fixture is not the engine.
func TestTheSwitcherListsTheThreadsAndWhereTheyWereLeft(t *testing.T) {
	app, _ := threadsApp(t)
	_ = app.Frame(120, 30)
	press(app, threadsChord)
	frame := ansi.Strip(app.switcher.Render(90, 12))
	for _, want := range []string{
		"the wisp parity push", "importer rewrite", "parked on the schema question",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the switcher is missing %q:\n%s", want, frame)
		}
	}
}

// -- switching -----------------------------------------------------------------

// THE SWITCH RE-POINTS EVERYTHING, ATOMICALLY. The transcript, the read
// watermark and the journal claim all belong to the room they were read for,
// and this is the test that none of them is carried across.
func TestSwitchingRepointsTheWindowWithNoGhostRows(t *testing.T) {
	app, _ := threadsApp(t)
	_ = app.Frame(120, 30)
	if !strings.Contains(frame(app), "the diff is ready when you are") {
		t.Fatal("the fixture did not render the first thread")
	}
	before := app.watermark
	if before == 0 {
		t.Fatal("the fixture never raised a watermark")
	}

	if cmd := app.switchThread("session-two"); cmd != nil {
		_ = cmd()
	}
	if app.session != "session-two" {
		t.Fatalf("the window is in %q", app.session)
	}
	if app.watermark != 0 || app.journal != 0 {
		t.Fatalf("the switch carried the old room's read position: watermark %d journal %d",
			app.watermark, app.journal)
	}
	out := frame(app)
	if strings.Contains(out, "the diff is ready when you are") {
		t.Fatalf("a row from the thread the reader left is still on screen:\n%s", out)
	}

	poll(t, app)
	out = frame(app)
	if !strings.Contains(out, "parked on the schema question") {
		t.Fatalf("the arrived thread's own journal did not repaint:\n%s", out)
	}
	if strings.Contains(out, "the diff is ready when you are") {
		t.Fatalf("the old thread's rows came back after a poll:\n%s", out)
	}
	if app.status.thread != "importer rewrite" {
		t.Fatalf("the chip still says %q", app.status.thread)
	}
}

// A READ BELONGS TO THE THREAD IT WAS ISSUED FOR. A poll in flight when the
// window switches must not land in the room the reader arrived in.
func TestAReadIssuedForTheOldThreadIsRefused(t *testing.T) {
	app, _ := threadsApp(t)
	_ = app.Frame(120, 30)

	// One read, taken for the thread the window is in and held.
	app.polling = true
	stale, _ := app.pollCmd()().(pollResultMsg)
	app.polling = false
	if stale.session != testSession {
		t.Fatalf("the read did not name its own room: %q", stale.session)
	}

	if cmd := app.switchThread("session-two"); cmd != nil {
		_ = cmd()
	}
	app.applyPoll(stale)
	if out := frame(app); strings.Contains(out, "the diff is ready when you are") {
		t.Fatalf("a read for the thread the reader left landed in the new one:\n%s", out)
	}
	if app.watermark != 0 {
		t.Fatalf("the stale read moved the new room's watermark to %d", app.watermark)
	}
}

// THE JOIN IS ONE RULED LINE AND THE THREAD'S TITLE (5.3's `thread break`), and
// it breathes: a blank above and a blank below.
func TestTheThreadBreakIsOneRuledLineWithTheTitle(t *testing.T) {
	app, _ := threadsApp(t)
	_ = app.Frame(120, 30)
	if cmd := app.switchThread("session-two"); cmd != nil {
		_ = cmd()
	}

	index, found := app.transcript.IndexOf(threadBreakID)
	if !found {
		t.Fatal("the arriving thread drew no join at all")
	}
	block, ok := app.transcript.Block(index).(*threadBreakBlock)
	if !ok {
		t.Fatalf("the join is a %T", app.transcript.Block(index))
	}
	rows := block.Rows(80)
	if len(rows) != 3 {
		t.Fatalf("the join is %d rows, want a blank, the rule and a blank: %q", len(rows), rows)
	}
	if strings.TrimSpace(rows[0]) != "" || strings.TrimSpace(rows[2]) != "" {
		t.Fatalf("the join does not breathe: %q", rows)
	}
	if !strings.Contains(rows[1], "importer rewrite") {
		t.Fatalf("the rule does not name the thread: %q", rows[1])
	}
	// One ruled line, and no box: the product's one disclosure grammar.
	if strings.ContainsAny(rows[1], "│┌┐└┘├┤") {
		t.Fatalf("the join drew a box: %q", rows[1])
	}
}

// -- the room-switch contract (5.4) --------------------------------------------

// roomSwitchRow is the settlement row the head journals: a message carrying the
// typed part whose payload is the new session id.
func roomSwitchRow(session, target, body string) store.Message {
	return store.Message{
		SessionID: session, Role: store.RoleSystem, Body: body,
		Parts: []store.MessagePart{{Kind: roomSwitchKind, Text: target}},
	}
}

// The contract, end to end: a row lands in this thread, and the window is in
// the other one afterwards.
func TestARoomSwitchRowMovesTheWindow(t *testing.T) {
	app, backend := threadsApp(t)
	_ = app.Frame(120, 30)
	backend.add(roomSwitchRow(testSession, "session-two",
		"this deserves its own thread — continuing in importer rewrite"))

	poll(t, app)
	if cmd := app.drainRoomSwitch(); cmd != nil {
		_ = cmd()
	}
	if app.session != "session-two" {
		t.Fatalf("the settled split left the window in %q", app.session)
	}
	if app.watermark != 0 {
		t.Fatalf("the split carried the old thread's watermark: %d", app.watermark)
	}
}

// ONLY ONCE. The row is acted on by the pass that first absorbs it, and a
// second poll over the same journal cannot fire it again.
func TestARoomSwitchRowFiresExactlyOnce(t *testing.T) {
	app, backend := threadsApp(t)
	_ = app.Frame(120, 30)
	backend.add(roomSwitchRow(testSession, "session-two", "continuing over there"))

	poll(t, app)
	if app.roomSwitch.target != "session-two" {
		t.Fatalf("the first pass did not record the split: %+v", app.roomSwitch)
	}
	if cmd := app.drainRoomSwitch(); cmd != nil {
		_ = cmd()
	}
	if app.roomSwitch.target != "" {
		t.Fatalf("the intent survived being performed: %+v", app.roomSwitch)
	}

	// Back to the thread it came from, and read it again. The row is already
	// absorbed there, so nothing re-fires.
	if cmd := app.switchThread(testSession); cmd != nil {
		_ = cmd()
	}
	poll(t, app)
	poll(t, app)
	if app.roomSwitch.target != "" {
		t.Fatalf("a re-read of an absorbed row armed the split again: %+v", app.roomSwitch)
	}
	if app.session != testSession {
		t.Fatalf("the window moved a second time, to %q", app.session)
	}
}

// ONLY ROWS OF THE CURRENT SESSION. A settlement journaled in another thread is
// that thread's business, and this window may not act on it.
func TestARoomSwitchRowInAnotherThreadIsNotActedOn(t *testing.T) {
	app, backend := threadsApp(t)
	_ = app.Frame(120, 30)
	backend.add(roomSwitchRow("session-two", testSession, "continuing elsewhere"))

	poll(t, app)
	if cmd := app.drainRoomSwitch(); cmd != nil {
		_ = cmd()
	}
	if app.session != testSession {
		t.Fatalf("another thread's settlement moved this window to %q", app.session)
	}
}

// ONLY FORWARD. A part naming the room the window is already in is a no-op, not
// a reset — a replayed row may not throw a transcript away to arrive where it
// already is.
func TestARoomSwitchRowNamingThisThreadDoesNothing(t *testing.T) {
	app, backend := threadsApp(t)
	_ = app.Frame(120, 30)
	backend.add(roomSwitchRow(testSession, testSession, "staying put"))

	poll(t, app)
	before := app.watermark
	if cmd := app.drainRoomSwitch(); cmd != nil {
		_ = cmd()
	}
	if app.session != testSession || app.watermark != before {
		t.Fatalf("a self-addressed split reset the window: session %q watermark %d",
			app.session, app.watermark)
	}
	if !strings.Contains(frame(app), "the diff is ready when you are") {
		t.Fatal("a self-addressed split threw the transcript away")
	}
}

// The closing row is drawn only when the head did not say it itself. Two
// sentences saying one thing at the last moment a reader sees a thread is §19
// at the worst possible place.
func TestTheClosingRowIsOnlyDrawnWhenNobodySaidIt(t *testing.T) {
	t.Run("the head spoke", func(t *testing.T) {
		app, backend := threadsApp(t)
		_ = app.Frame(120, 30)
		backend.add(roomSwitchRow(testSession, "session-two", "moving this to its own thread"))
		poll(t, app)
		if !app.roomSwitch.spoken {
			t.Fatal("a row with a body was recorded as silent")
		}
		if cmd := app.drainRoomSwitch(); cmd != nil {
			_ = cmd()
		}
		if _, found := app.transcript.IndexOf(roomSwitchNoteID); found {
			t.Fatal("the surface said it again under the head's own sentence")
		}
	})

	t.Run("the row was silent", func(t *testing.T) {
		app, backend := threadsApp(t)
		_ = app.Frame(120, 30)
		backend.add(roomSwitchRow(testSession, "session-two", ""))
		poll(t, app)
		if app.roomSwitch.spoken {
			t.Fatal("a part-only row was recorded as spoken")
		}
		// The receipt is written into the thread being LEFT, so it is read
		// before the switch throws that transcript away.
		if cmd := app.applyRoomSwitch(app.roomSwitch); cmd != nil {
			_ = cmd()
		}
		if app.session != "session-two" {
			t.Fatalf("the silent split did not move the window: %q", app.session)
		}
	})
}

// -- the alive glance (J5) -----------------------------------------------------

// The board home lists the open threads beside the running work, name and
// left-at line, and NOT the thread the reader is already in.
func TestTheBoardHomeListsOpenThreads(t *testing.T) {
	app, _ := threadsApp(t)
	app.showPage(pageBoard)
	_ = app.Frame(120, 40)

	found, current := false, false
	for _, line := range app.boardLines() {
		if line.target == boardOpensThread {
			found = true
			if line.id == testSession {
				current = true
			}
		}
	}
	if !found {
		t.Fatal("the board home lists no threads at all")
	}
	if current {
		t.Fatal("the board offered a door back to the room it is drawn in")
	}

	page := ansi.Strip(app.renderBoard(120, 40))
	for _, want := range []string{boardThreadsWord, "importer rewrite", "parked on the schema question"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the board home is missing %q:\n%s", want, page)
		}
	}
	// One ornament, and it is the switcher's. A dot on a page the reader is
	// already looking at is decoration.
	if strings.Contains(page, "●") {
		t.Fatalf("the board grew the switcher's ornament:\n%s", page)
	}
}

// Activating a thread row jumps to that thread, by key and by pointer alike.
func TestActivatingABoardThreadRowSwitchesToIt(t *testing.T) {
	for _, hand := range []string{"keyboard", "pointer"} {
		t.Run(hand, func(t *testing.T) {
			app, _ := threadsApp(t)
			app.showPage(pageBoard)
			_ = app.Frame(120, 40)

			door := -1
			lines := app.boardLines()
			for i, at := range boardDoors(lines) {
				if lines[at].target == boardOpensThread {
					door = i
					break
				}
			}
			if door < 0 {
				t.Fatal("no thread door on the board")
			}

			var cmd tea.Cmd
			if hand == "keyboard" {
				app.boardSelect(door)
				cmd = app.boardEnter(door)
			} else {
				y := boardRowOf(t, app, lines, door)
				cmd = app.boardPoint(clickAt(4, y), image.Point{X: 4, Y: y})
			}
			if cmd != nil {
				_ = cmd()
			}
			if app.session != "session-two" {
				t.Fatalf("the %s left the window in %q", hand, app.session)
			}
			if app.page != pageThread {
				t.Fatalf("the %s did not take the reader to the conversation", hand)
			}
		})
	}
}

// boardRowOf finds the viewport row one door is drawn on, by asking the page's
// own hit test rather than by counting lines.
func boardRowOf(t *testing.T, app *App, lines []boardLine, door int) int {
	t.Helper()
	for y := range 40 {
		if at, ok := app.boardDoorAt(y); ok && at == door {
			return y
		}
	}
	t.Fatalf("door %d is not on the frame", door)
	return 0
}

// -- the attribution row (5.3) -------------------------------------------------

// attributedApp is a window standing on a task page whose work was commissioned
// from the OTHER thread.
func attributedApp(t *testing.T) *App {
	t.Helper()
	app, backend := threadsApp(t)
	for i := range backend.nodes {
		if backend.nodes[i].ID == "job-1" {
			backend.nodes[i].Provenance = store.Provenance{
				Origin: store.OriginUser, SessionID: "session-two",
				Intent: "rework NavCtx",
			}
		}
	}
	backend.journal++
	poll(t, app)
	_ = app.Frame(120, 30)
	press(app, "ctrl+o")
	for i, row := range app.railModel.Rows() {
		if row.Name == "wisp-parity" {
			app.scopePoint(railPoint{row: i})
			break
		}
	}
	if app.view == nil || app.view.kind != viewNode {
		t.Fatal("the fixture did not enter a task room")
	}
	_ = app.Frame(120, 30)
	return app
}

// The task page names the conversation it was run for, in words and never in an
// id.
func TestTheTaskPageNamesTheThreadItWasRunFor(t *testing.T) {
	app := attributedApp(t)
	index, found := app.view.transcript.IndexOf(forThreadID)
	if !found {
		t.Fatalf("the record page carries no attribution row:\n%s", ansi.Strip(app.Frame(120, 30)))
	}
	row := ansi.Strip(strings.Join(app.view.transcript.Block(index).Rows(80), "\n"))
	if !strings.Contains(row, "importer rewrite") {
		t.Fatalf("the attribution row does not name the thread: %q", row)
	}
	if strings.Contains(row, "session-two") {
		t.Fatalf("the attribution row printed a session id: %q", row)
	}
}

// A job commissioned from the thread the reader is already in gets NO row: a
// door back to the room it is drawn in is not a door.
func TestATaskRunFromThisThreadGetsNoAttributionRow(t *testing.T) {
	app, _ := threadsApp(t)
	if row := app.forThreadRow(testSession); row != nil {
		t.Fatal("the page offered a door back to the room it is drawn in")
	}
	if row := app.forThreadRow(""); row != nil {
		t.Fatal("a job with no provenance session drew an attribution row anyway")
	}
}

// PARITY. The row jumps by pointer, and the same jump is one keystroke away for
// the keyboard: `t` opens the switcher already pointing at that thread, so
// enter finishes the gesture.
func TestTheAttributionRowJumpsByPointerAndByKeyboard(t *testing.T) {
	t.Run("pointer", func(t *testing.T) {
		app := attributedApp(t)
		frame := strings.Split(ansi.Strip(app.Frame(120, 30)), "\n")
		y := -1
		for i, line := range frame {
			if strings.Contains(line, strings.TrimSpace(forThreadLead)+" ") &&
				strings.Contains(line, "importer rewrite") {
				y = i
			}
		}
		if y < 0 {
			t.Fatalf("the attribution row is not on the frame:\n%s", strings.Join(frame, "\n"))
		}
		session, ok := app.recordForThreadAt(y)
		if !ok || session != "session-two" {
			t.Fatalf("the hit test answered %q (%v) on the attribution row", session, ok)
		}
		if cmd := app.recordPointer(y); cmd != nil {
			_ = cmd()
		}
		if app.session != "session-two" {
			t.Fatalf("clicking the attribution row left the window in %q", app.session)
		}
	})

	t.Run("keyboard", func(t *testing.T) {
		app := attributedApp(t)
		if got := app.provenanceThread(); got != "session-two" {
			t.Fatalf("the page's provenance thread is %q", got)
		}
		press(app, threadsChord)
		if app.overlay != overlayThreads {
			t.Fatal("the chord did not raise the switcher from a task page")
		}
		result, ok := app.switcher.Selected()
		if !ok {
			t.Fatal("the switcher opened with the cursor on nothing")
		}
		if got, want := result.(palette.SwitchThread).ID, "session-two"; got != want {
			t.Fatalf("the switcher opened pointing at %q, want the thread the job was run for", got)
		}
	})
}

// -- the new-thread door -------------------------------------------------------

// Choosing `new thread` mints one and walks into it, with no naming prompt on
// the way: the scribe names it after the first exchange.
func TestChoosingNewThreadMintsOneAndAsksForNothing(t *testing.T) {
	app, backend := threadsApp(t)
	_ = app.Frame(120, 30)
	press(app, threadsChord)

	// Through the component's own key, so the close and the act happen in the
	// order the surface really performs them.
	cmd := app.switcher.Key(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("the new-thread door produced no command")
	}
	msg, ok := cmd().(roomOpenedMsg)
	if !ok {
		t.Fatalf("the door yielded %T", cmd())
	}
	if msg.err != nil {
		t.Fatalf("minting failed: %v", msg.err)
	}
	if len(backend.opened) != 1 {
		t.Fatalf("the store took %d opens", len(backend.opened))
	}
	if _, _ = app.Update(msg); app.session != msg.session.ID {
		t.Fatalf("the window did not walk into the thread it minted: %q", app.session)
	}
	if app.overlay != overlayNone {
		t.Fatal("the switcher stayed up over the thread it opened")
	}
}

// -- the unseen ornament -------------------------------------------------------

// THE WINDOW MAY ONLY CLAIM WHAT IT HAS SEEN. A thread this window has never
// visited carries no dot; one it has visited and left carries one when
// something has landed since.
func TestTheUnseenDotIsAClaimAboutThisWindow(t *testing.T) {
	app, _ := threadsApp(t)
	_ = app.Frame(120, 30)

	for _, thread := range app.readThreads() {
		if thread.Unseen {
			t.Fatalf("a fresh window dotted %q without ever having visited it", thread.Name)
		}
	}

	// A thread this window HAS visited, last seen before its newest activity:
	// that is the whole of what the dot claims.
	app.noteThreadSeen("session-two", fixedNow().Add(-40*time.Hour))
	dotted := map[string]bool{}
	for _, thread := range app.readThreads() {
		if thread.Unseen {
			dotted[thread.SessionID] = true
		}
	}
	if !dotted["session-two"] {
		t.Fatal("a visited thread with newer activity carries no dot")
	}
	if dotted[testSession] {
		t.Fatal("the thread the reader is standing in wears the dot")
	}

	// And a later visit takes it back off: the dot is about what this window
	// has shown, so showing it is what clears it.
	app.noteThreadSeen("session-two", fixedNow())
	for _, thread := range app.readThreads() {
		if thread.Unseen {
			t.Fatalf("%q still dots after this window read it", thread.Name)
		}
	}
}
