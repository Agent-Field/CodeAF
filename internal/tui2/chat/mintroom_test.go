package chat

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/composer"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
)

// 13.19: the `+ new` card, and the trap it used to be.
//
// THE INCIDENT, in the owner's own screenshot. A reader selected `+ new` in the
// rail. The rail law is "selection previews; enter opens", so the main pane drew
// a card and the composer deliberately stayed bound to the room the reader was
// standing in — which is right, and was invisible. Believing they were in the new
// room, they typed a sentence and sent it. It landed in the OLD room, where the
// pane was not looking, and the task it opened inherited that room's whole
// history and went wrong.
//
// The three answers are tested here as three laws: typing in front of the card
// mints the room and keeps the words, a send that beats the store still lands in
// the new room, and the composer names its room whenever the pane is showing
// something else.

// freshRoomPlace is the digit that reaches the `+ new` door in the fixture's
// rail: `aforge`, two rooms, then the door. It is a named place for the reason
// [settledTaskPlace] is one — the digits count rows a cursor may rest on, and the
// fixture's list is free to grow.
const freshRoomPlace = "4"

// previewFreshRoom puts the reader where the incident started: the map has the
// keyboard and the main pane is the `+ new` card.
func previewFreshRoom(t *testing.T, app *App) {
	t.Helper()
	press(app, "ctrl+o")
	press(app, freshRoomPlace)
	if name := app.railModel.Selected().Name; name != newRoomDoor {
		t.Fatalf("place %s is %q, not the fresh-room door", freshRoomPlace, name)
	}
	if !app.previewingNewRoom() {
		t.Fatal("selecting the door did not draw its card")
	}
}

// previewRow is [previewFreshRoom] for a fixture whose list has grown past the
// digit: it aims the cursor by row id and produces the same preview the digit
// would have.
func previewRow(t *testing.T, app *App, id string) {
	t.Helper()
	press(app, "ctrl+o")
	for i, row := range app.railModel.Rows() {
		if row.ID == id {
			app.applyScope(app.railModel.Select(i))
			return
		}
	}
	t.Fatalf("the rail has no row %q", id)
}

// typeRune is one printable keystroke through the real ladder, with whatever it
// produced folded back in the way the runtime would.
func typeRune(t *testing.T, app *App, r rune) {
	t.Helper()
	runCmd(t, app, app.drain(app.key(tea.KeyPressMsg{Code: r, Text: string(r)})), 0)
}

func typeText(t *testing.T, app *App, text string) {
	t.Helper()
	for _, r := range text {
		typeRune(t, app, r)
	}
}

// A printable key in front of the card is the reader saying they meant the new
// room. It is taken as the commitment it obviously is: the room is made, the
// window moves into it, and the letters are in that room's composer.
func TestTypingAtTheFreshRoomCardMintsTheRoomAndCarriesTheDraft(t *testing.T) {
	app, backend := boardApp(t)
	previewFreshRoom(t, app)
	before := app.session

	typeText(t, app, "hi")

	if len(backend.opened) != 1 {
		t.Fatalf("typing at the card minted %d rooms, want exactly one", len(backend.opened))
	}
	if app.session == before {
		t.Fatalf("the window is still standing in %q", app.session)
	}
	if app.session != backend.opened[0] {
		t.Fatalf("the window is in %q, not the room it minted (%q)", app.session, backend.opened[0])
	}
	if draft := app.composer.Draft(); draft != "hi" {
		t.Fatalf("the draft reads %q: the typed letters did not survive the mint", draft)
	}
	if app.railFocus {
		t.Fatal("the map kept the keyboard, so the rest of the sentence would be eaten by it")
	}
	if bind := app.composerMode(); bind.mode != rail.ComposerChat {
		t.Fatalf("the fresh room's composer binds %+v", bind)
	}
	if app.view != nil {
		t.Fatal("the main pane is still a card: the reader cannot see the room they are typing into")
	}
}

// THE SHAPE IT WAS REPORTED IN. The keyboard was on the COMPOSER, not on the map:
// the reader selected the door, clicked back into the writing area — which takes
// the keyboard and leaves the pane on the card — and typed. The door has to exist
// on both sides of that flag, or the fix is one a mouse walks around.
func TestTypingAtTheFreshRoomCardMintsWithTheKeyboardOnTheComposer(t *testing.T) {
	app, backend := boardApp(t)
	previewFreshRoom(t, app)
	if !app.focusConversation() {
		t.Fatal("clicking the writing area did not take the keyboard")
	}
	if !app.previewingNewRoom() {
		t.Fatal("taking the keyboard cleared the card, so this is no longer the reported state")
	}

	typeText(t, app, "go")

	if len(backend.opened) != 1 {
		t.Fatalf("typing minted %d rooms", len(backend.opened))
	}
	if app.session != backend.opened[0] {
		t.Fatalf("the window is in %q, not the minted room", app.session)
	}
	if draft := app.composer.Draft(); draft != "go" {
		t.Fatalf("the draft reads %q", draft)
	}
}

// THE INCIDENT PINNED. An old room with history behind it, `+ new` selected, and
// a sentence typed and SENT before the store has answered — the one ordering that
// can still put a person's words in the room they were leaving. The journal row's
// session is the fresh room's, and the old room takes nothing.
func TestADraftSentBeforeTheMintLandsPostsIntoTheFreshRoom(t *testing.T) {
	app, backend := boardApp(t)
	backend.add(store.Message{SessionID: testSession, Role: store.RoleUser,
		Body: "here is a long argument about the importer"})
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent,
		Body: "understood — I will keep that in mind"})
	poll(t, app)
	old := app.session
	previewFreshRoom(t, app)

	// The mint is issued and DELIBERATELY not delivered yet: this is the gap.
	var mint tea.Cmd
	for i, r := range "new task" {
		cmd := app.drain(app.key(tea.KeyPressMsg{Code: r, Text: string(r)}))
		if i == 0 {
			if cmd == nil {
				t.Fatal("the first letter produced no command, so no room was minted")
			}
			mint = cmd
			continue
		}
		if cmd != nil {
			t.Fatalf("letter %d produced a command of its own: %T", i, cmd())
		}
	}
	// Enter, while the store is still working.
	if cmd := app.drain(app.key(tea.KeyPressMsg{Code: tea.KeyEnter})); cmd != nil {
		t.Fatalf("the send went out during the mint: %T", cmd())
	}
	if len(backend.posted) != 0 {
		t.Fatalf("a draft reached the store mid-mint: %+v", backend.posted)
	}

	// Now the room lands, and the held words go with it.
	runCmd(t, app, mint, 0)

	if len(backend.posted) != 1 {
		t.Fatalf("the store took %d writes, want the one held draft: %+v",
			len(backend.posted), backend.posted)
	}
	posted := backend.posted[0]
	if posted.Body != "new task" {
		t.Fatalf("the held draft posted as %q", posted.Body)
	}
	if posted.SessionID == old {
		t.Fatalf("the words landed in the room the reader was leaving (%q)", old)
	}
	if posted.SessionID != app.session || posted.SessionID != backend.opened[0] {
		t.Fatalf("the words landed in %q; the window is in %q and minted %q",
			posted.SessionID, app.session, backend.opened[0])
	}
	if app.mint.active {
		t.Fatal("the mint gap never closed: every later send would be held forever")
	}
}

// A mint that FAILS leaves the reader in the old room, so the held sentence may
// not be performed there — it comes back into the draft instead, beside the
// reason.
func TestAFailedMintGivesTheHeldWordsBack(t *testing.T) {
	app, _ := boardApp(t)
	previewFreshRoom(t, app)
	typeRune(t, app, 'x')
	// A second mint, whose answer is a refusal.
	app.mint.open()
	app.mint.take(composer.Send{Text: "words nobody sent"})
	app.composer.(interface{ KillToStart() }).KillToStart()

	app.applyRoomOpened(roomOpenedMsg{err: store.ErrNotFound})

	if app.mint.active {
		t.Fatal("a failed mint left the gap open")
	}
	if draft := app.composer.Draft(); !strings.Contains(draft, "words nobody sent") {
		t.Fatalf("the draft reads %q: the words the store refused were destroyed", draft)
	}
	if app.status.err == "" {
		t.Fatal("the failure is not on screen anywhere")
	}
}

// Enter is untouched: the key the card names still opens the room, and it opens
// it empty.
func TestEnterStillOpensTheFreshRoomFromItsCard(t *testing.T) {
	app, backend := boardApp(t)
	previewFreshRoom(t, app)

	runCmd(t, app, app.drain(app.key(tea.KeyPressMsg{Code: tea.KeyEnter})), 0)

	if len(backend.opened) != 1 {
		t.Fatalf("enter minted %d rooms", len(backend.opened))
	}
	if app.session != backend.opened[0] {
		t.Fatalf("the window is in %q", app.session)
	}
	if draft := app.composer.Draft(); draft != "" {
		t.Fatalf("enter put %q in the draft", draft)
	}
}

// Esc pops the preview and mints nothing: a person backing out of a door has not
// walked through it.
func TestEscAtTheFreshRoomCardMintsNothing(t *testing.T) {
	app, backend := boardApp(t)
	previewFreshRoom(t, app)

	pressThrough(app, "esc")

	if len(backend.opened) != 0 {
		t.Fatalf("esc minted %v", backend.opened)
	}
	if app.mint.active {
		t.Fatal("esc opened a mint gap")
	}
}

// Walking the map is still walking. j, k and the digits are how a reader moves
// past this row, and a door that took them would make the map usable for exactly
// one row.
func TestNavigatingPastTheFreshRoomCardMintsNothing(t *testing.T) {
	app, backend := boardApp(t)
	previewFreshRoom(t, app)

	press(app, "k")
	if len(backend.opened) != 0 {
		t.Fatalf("a navigation key minted %v", backend.opened)
	}
	if app.previewingNewRoom() {
		t.Fatal("k did not move the cursor off the door")
	}
	press(app, "j")
	if !app.previewingNewRoom() {
		t.Fatal("j did not walk back onto the door")
	}
	if len(backend.opened) != 0 {
		t.Fatalf("walking minted %v", backend.opened)
	}
}

// A chord is an instruction and not a letter. No accelerator anywhere in the
// product may mint a room, and the test is on the predicate because that is where
// the rule is: the ladder above it is a list of keys, and this is the law.
func TestOnlyAWrittenCharacterMintsARoom(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.KeyPressMsg
		want bool
	}{
		{"a letter", tea.KeyPressMsg{Code: 'a', Text: "a"}, true},
		{"a capital", tea.KeyPressMsg{Code: 'A', Text: "A", Mod: tea.ModShift}, true},
		{"a space", tea.KeyPressMsg{Code: ' ', Text: " "}, true},
		{"a digit typed at a composer", tea.KeyPressMsg{Code: '7', Text: "7"}, true},
		{"an accented rune", tea.KeyPressMsg{Code: 'é', Text: "é"}, true},
		{"enter", tea.KeyPressMsg{Code: tea.KeyEnter}, false},
		{"esc", tea.KeyPressMsg{Code: tea.KeyEscape}, false},
		{"an arrow", tea.KeyPressMsg{Code: tea.KeyUp}, false},
		{"a ctrl chord", tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}, false},
		{"an alt chord", tea.KeyPressMsg{Code: 't', Mod: tea.ModAlt}, false},
		{"a carriage return wearing text", tea.KeyPressMsg{Code: tea.KeyEnter, Text: "\r"}, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := typedRune(testCase.msg); got != testCase.want {
				t.Fatalf("typedRune = %v, want %v", got, testCase.want)
			}
		})
	}
}

// -- the composer names its room ---------------------------------------------

// THE COMPOSER ALWAYS SAYS WHERE WORDS GO when the eyes and the mouth are in
// different places. A card in the main pane is a look at something the composer
// is not bound to, and that is the frame the incident happened in.
func TestTheComposerNamesItsRoomWhileThePaneShowsSomethingElse(t *testing.T) {
	app, _ := boardApp(t)
	if target := app.composerTarget(); target != "" {
		t.Fatalf("the composer names %q while the pane IS its room: that is chrome announcing itself", target)
	}

	// Previewing another room: the words still go to this one, and it says so.
	press(app, "ctrl+o")
	press(app, "3")
	app.focusConversation()
	app.refresh()
	target := app.composerTarget()
	if !strings.Contains(target, "the wisp parity push") {
		t.Fatalf("the composer's line reads %q and does not name the room it is bound to", target)
	}
	if out := ansi.Strip(app.Frame(100, 24)); !strings.Contains(out, target) {
		t.Fatalf("the line never reaches the frame:\n%s", out)
	}

	// And back in the room, it goes quiet again.
	pressThrough(app, "esc")
	press(app, "ctrl+o")
	press(app, "2")
	press(app, "enter")
	if target := app.composerTarget(); target != "" {
		t.Fatalf("an entered room still names itself: %q", target)
	}
}

// A room being minted has no name yet, and answering with the OLD room's name
// here would be the exact lie the line exists to prevent — the words are going to
// the new room, and the card's own word is what they are going to.
func TestTheComposerNamesTheFreshRoomWhileItIsBeingMinted(t *testing.T) {
	app, _ := boardApp(t)
	previewFreshRoom(t, app)
	app.drain(app.key(tea.KeyPressMsg{Code: 'q', Text: "q"}))

	if !app.mint.active {
		t.Fatal("typing did not open the mint")
	}
	if target := app.composerTarget(); target != newRoomCardTitle {
		t.Fatalf("mid-mint the composer names %q, want %q", target, newRoomCardTitle)
	}
}

// An untitled room is named by the only honest thing there is to say about it,
// never by an id (13.3.4).
func TestTheComposerNamesAnUntitledRoomHonestly(t *testing.T) {
	app, backend := boardApp(t)
	backend.sessions = append(backend.sessions, store.Session{ID: "chat-nameless"})
	backend.journal++
	poll(t, app)
	app.switchRoom("chat-nameless")
	previewRow(t, app, rowNewRoomID)
	app.focusConversation()

	target := app.composerTarget()
	if !strings.Contains(target, untitledRoom) {
		t.Fatalf("the composer's line reads %q", target)
	}
	if strings.Contains(target, "chat-nameless") {
		t.Fatalf("the line drew a session id: %q", target)
	}
}

// -- the card itself ----------------------------------------------------------

// The card is an EMPTY STATE now and not a rail row printed sideways. What it
// used to draw — a queued glyph over a button caption over an honest `$—` about a
// room that does not exist — said nothing true, and §15's delete test takes all
// three away without a loss.
func TestTheFreshRoomCardIsAnEmptyStateAndNotARowDump(t *testing.T) {
	app, _ := boardApp(t)
	previewFreshRoom(t, app)
	out := ansi.Strip(app.Frame(120, 30))

	for _, want := range []string{
		newRoomCardTitle,
		// The promise the rail's `untitled room` rows depend on.
		"takes its name",
		newRoomTypeVerb, newRoomTypeNote,
		newRoomOpenVerb + " " + newRoomOpenKey, newRoomOpenNote,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the card is missing %q:\n%s", want, out)
		}
	}
	// The pane, not the rail: the door's own row still reads `+ new`, and the
	// card must not repeat it.
	pane := strings.Join(paneRows(out), "\n")
	if strings.Contains(pane, "$"+"—") {
		t.Fatalf("the card still carries a cost for a room that does not exist:\n%s", pane)
	}
	if strings.Contains(pane, newRoomDoor) {
		t.Fatalf("the card is still drawing the door's own label:\n%s", pane)
	}
	if strings.Contains(pane, "nothing to report yet") {
		t.Fatalf("the card fell back to the row renderer's empty line:\n%s", pane)
	}
}

// paneRows is the left of a wide frame — the main pane, without the rail beside
// it — so an assertion about what the CARD says cannot be answered by the rail
// row it is a card for.
func paneRows(frame string) []string {
	rows := strings.Split(frame, "\n")
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		if len(row) > 92 {
			row = row[:92]
		}
		out = append(out, row)
	}
	return out
}
