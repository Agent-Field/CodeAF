package tui3

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// prompt is the input line's mark. Two cells, and the only furniture below the
// conversation: a box around the input would be a box the reader has to look
// past on every frame.
const prompt = "› "

// draftRows is how many rows of a draft the box shows at once. Six is where a
// paste stops being a message and starts being a document: past it the block
// scrolls under the caret rather than eating the conversation it is about.
const draftRows = 6

// editor is the draft, and it holds newlines.
//
// It was a single line for one wave, on the argument that a conversation is
// typed a sentence at a time. That is true of what people TYPE and false of
// what they PASTE — a stack trace, a diff, a paragraph out of a file — and a
// box that silently flattened a paste into one run-on line was answering the
// commonest input on this surface by destroying it. So: alt+enter and ctrl+j
// open a line, a bracketed paste arrives whole, and enter still submits. The
// value is a rune slice with '\n' in it and nothing else is special about it.
type editor struct {
	value  []rune
	cursor int
}

func (e *editor) String() string { return string(e.value) }

// empty reports whether the draft holds anything a person would call text. It
// walks the runes rather than trimming a copy of them, because the frame asks
// this question half a dozen times a paint — the welcome box, the placeholder,
// the proposal's hint and the room's all read it — and building a string of a
// four-thousand-line paste six times a frame is a hundred kilobytes of garbage
// per frame to learn one bit.
func (e *editor) empty() bool {
	for _, r := range e.value {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func (e *editor) reset() { e.value, e.cursor = e.value[:0], 0 }

// setText replaces the whole draft and parks the caret at its end. It is what
// history recall and the command list write through.
func (e *editor) setText(text string) {
	e.value = append(e.value[:0], []rune(text)...)
	e.cursor = len(e.value)
}

func (e *editor) insert(text string) {
	runes := []rune(text)
	e.value = append(e.value[:e.cursor], append(runes, e.value[e.cursor:]...)...)
	e.cursor += len(runes)
}

func (e *editor) deleteBackward() {
	if e.cursor == 0 {
		return
	}
	e.value = append(e.value[:e.cursor-1], e.value[e.cursor:]...)
	e.cursor--
}

func (e *editor) deleteForward() {
	if e.cursor >= len(e.value) {
		return
	}
	e.value = append(e.value[:e.cursor], e.value[e.cursor+1:]...)
}

// deleteWord is ctrl+w: back over any spaces, then back over the word.
func (e *editor) deleteWord() {
	at := e.cursor
	for at > 0 && unicode.IsSpace(e.value[at-1]) {
		at--
	}
	for at > 0 && !unicode.IsSpace(e.value[at-1]) {
		at--
	}
	e.value = append(e.value[:at], e.value[e.cursor:]...)
	e.cursor = at
}

// killToStart is ctrl+u, and it kills to the start of THIS line rather than of
// the draft: on a one-line draft the two are the same, and on a six-line paste
// only one of them is a gesture anybody wants.
func (e *editor) killToStart() {
	at := e.lineStart()
	e.value = append(e.value[:at], e.value[e.cursor:]...)
	e.cursor = at
}

func (e *editor) left() {
	if e.cursor > 0 {
		e.cursor--
	}
}

func (e *editor) right() {
	if e.cursor < len(e.value) {
		e.cursor++
	}
}

func (e *editor) home() { e.cursor = e.lineStart() }

func (e *editor) end() { e.cursor = e.lineEnd() }

// lineStart and lineEnd bound the LOGICAL line the caret is on — the run
// between two newlines, not the soft-wrapped row the box happens to draw.
func (e *editor) lineStart() int {
	for at := e.cursor; at > 0; at-- {
		if e.value[at-1] == '\n' {
			return at
		}
	}
	return 0
}

func (e *editor) lineEnd() int {
	for at := e.cursor; at < len(e.value); at++ {
		if e.value[at] == '\n' {
			return at
		}
	}
	return len(e.value)
}

// multiline reports whether the draft has more than one logical line.
func (e *editor) multiline() bool {
	for _, r := range e.value {
		if r == '\n' {
			return true
		}
	}
	return false
}

// onFirstLine reports whether the caret is on the draft's first logical line.
// It is the whole test for whether ↑ belongs to the draft or to history.
func (e *editor) onFirstLine() bool { return e.lineStart() == 0 }

func (e *editor) onLastLine() bool { return e.lineEnd() == len(e.value) }

// up and down move the caret between logical lines, keeping the column.
func (e *editor) up() {
	start := e.lineStart()
	if start == 0 {
		return
	}
	column := e.cursor - start
	previous := start - 1
	for previous > 0 && e.value[previous-1] != '\n' {
		previous--
	}
	if length := start - 1 - previous; column > length {
		column = length
	}
	e.cursor = previous + column
}

func (e *editor) down() {
	end := e.lineEnd()
	if end >= len(e.value) {
		return
	}
	column := e.cursor - e.lineStart()
	next := end + 1
	length := 0
	for next+length < len(e.value) && e.value[next+length] != '\n' {
		length++
	}
	if column > length {
		column = length
	}
	e.cursor = next + column
}

// key routes one keypress. The order is the surface's law: the key that acts on
// the SESSION (ctrl+c) is read before any key that acts on the draft, and the
// two typed overlays are read before the editor, because while a list is up the
// four keys that move and commit it are the list's.
func (a *app) key(msg tea.KeyPressMsg) tea.Cmd {
	// THE POINTER COMES BACK FIRST, ABOVE EVERYTHING, and then the key does
	// whatever it was always going to do. A hand back on the keyboard is a hand
	// that has finished selecting (copymode.go), so this is the whole of the exit
	// — no key to learn, no state to be stuck in, and nothing swallowed. ctrl+s
	// itself is excepted so it can act as the plain toggle it looks like: pressing
	// it twice must undo it, not re-arm it.
	if msg.String() != selectKey {
		a.takeMouseBack()
	}

	// An approval question outranks even the model overlay: it is the one state
	// where the SESSION is blocked on this keyboard — a tool call is parked
	// mid-batch waiting for the answer — and everything else on this surface can
	// wait for one keystroke. ctrl+c is the exception it makes for itself
	// (consent.go).
	if cmd, taken := a.consentKey(msg); taken {
		return cmd
	}

	// The connect offer is the next rung down, and it is modal for the same
	// reason at a lower urgency: the session is waiting on this answer too, and
	// a key that is not one of the two answers is a key that does nothing rather
	// than a key that types into a conversation that cannot move (connect.go).
	if cmd, taken := a.connectAskKey(msg); taken {
		return cmd
	}

	// And the harness offer under that, modal for the same reason at the lowest
	// urgency of the three: the session is holding a turn — before its first
	// request — on this one answer (harness.go).
	if cmd, taken := a.harnessAskKey(msg); taken {
		return cmd
	}
	if a.harnessCardKey(msg) {
		// `e` on that card walks into the design's room now (harnesscard.go), so
		// whatever door it parked is handed on here: a room whose lane was never
		// started is a page that never updates.
		return a.takeRoomPump()
	}

	// The settings panel is the fullscreen overlay, and it is modal for the same
	// reason the picker is and one more: there is nothing else on the screen to
	// send a key to (settings.go).
	if a.sheet.open && msg.String() != "ctrl+c" {
		cmd, _ := a.sheetKey(msg)
		return cmd
	}

	// And home is modal at the same rung and for the same reason: it takes the
	// whole frame, so there is nothing under it a key could mean anything to.
	// Every printable key belongs to it — typing on home is how a conversation
	// starts (home.go).
	if a.home.open && msg.String() != "ctrl+c" {
		return a.homeKey(msg)
	}

	// And the phone tier's status sheet is modal at the same rung and for the
	// same reason: it is the whole screen, so there is nothing under it to send a
	// key to (statusdeck.go).
	if a.deckShowing() && msg.String() != "ctrl+c" {
		a.deckSheetKey(msg)
		return nil
	}

	// The phone's tool detail is the other fullscreen overlay, and it is modal
	// at the same rung for the same two reasons: it is the whole screen, so
	// there is nothing under it a key could mean anything to, and esc is how a
	// person leaves it (expand.go). It is read AFTER the status sheet because
	// the deck is raised over whatever the body was drawing, this one included.
	if a.expandShowing() && msg.String() != "ctrl+c" {
		a.expandKey(msg)
		return nil
	}

	// The model overlay is modal: while it is up every key belongs to it and
	// the draft below is suspended untouched. ctrl+c is the one exception, for
	// the same reason it is read first below — leaving is never modal.
	if a.pick.open && msg.String() != "ctrl+c" {
		a.pickerKey(msg)
		return nil
	}
	if a.crewPick.open && msg.String() != "ctrl+c" {
		a.crewPickerKey(msg)
		return nil
	}
	if a.taskPick.open && msg.String() != "ctrl+c" {
		return a.taskChooserKey(msg)
	}
	if a.memPanel.open && msg.String() != "ctrl+c" {
		a.memoryKey(msg)
		return nil
	}

	// And the session picker is modal at the same rung, for the same reasons: it
	// takes the input line's place, it holds its own filter, and esc leaves the
	// conversation exactly as it was (resume.go). The two are never up together —
	// each is opened by a command typed into a box neither of them leaves open.
	if a.roster.open && msg.String() != "ctrl+c" {
		return a.resumeKey(msg)
	}

	// And the deliverables picker at the same rung, for the same reasons again:
	// it takes the input line's place, it holds its own filter — and its own
	// destination box over that — and esc leaves the conversation exactly as it
	// was (deliverables.go).
	if a.shelf.open && msg.String() != "ctrl+c" {
		return a.filesKey(msg)
	}

	// And the connections panel at the same rung again, for the same reasons:
	// it opens on a COMMAND rather than by typing, so nothing is being written
	// under it, and esc leaves everything exactly as it was (connectpanel.go).
	if a.connPanel.open && msg.String() != "ctrl+c" {
		return a.connectPanelKey(msg)
	}

	// And the harness panel, which is that panel's twin in every respect that
	// matters here: opened by a command, nothing being typed under it, and esc
	// leaving the conversation exactly as it was (harnesspanel.go).
	if a.harnPanel.open && msg.String() != "ctrl+c" {
		return a.harnessPanelKey(msg)
	}

	// And the permissions panel, which is those two panels' twin in every
	// respect that matters here: opened by a command, nothing being typed under
	// it, and esc leaving the conversation exactly as it was (permissions.go).
	// Being modal is also what frees a bare d to mean "drop this line" — no
	// draft is under this one for a letter to fall through into.
	if a.permPanel.open && msg.String() != "ctrl+c" {
		return a.permPanelKey(msg)
	}

	if msg.String() == "ctrl+c" {
		// INTERRUPT FIRST. While a turn runs ctrl+c is the same key esc is —
		// a person hitting it mid-turn is reaching for the model, not for the
		// door, and every terminal habit in the world says that keystroke stops
		// the RUNNING thing. Idle, there is nothing to stop and it leaves.
		if a.state == stateWorking {
			a.interrupt()
			return nil
		}
		return a.quit()
	}

	// THE TASK PAGE IS MODAL AT THIS RUNG AND FOR THE SETTINGS PANEL'S REASON: it
	// is the whole screen, so there is nothing under it a key could mean anything
	// to (taskview.go). It is read HERE rather than up beside the panel because
	// this one handler carries both halves of the page — every key while it is up,
	// and the single chord that OPENS it while it is down — and the opening half
	// must not outrank ctrl+c, which is read directly above and stays the door.
	if cmd, taken := a.taskSheetKeyPress(msg); taken {
		return cmd
	}

	// COPY MODE is modal, and it is modal one rung below ctrl+c for the same
	// reason everything else here is: leaving is never modal. While it is up the
	// surface is a reader, and a key that fell through to the draft would type
	// into a box whose effect is off screen (copymode.go).
	if cmd, taken := a.copyKey(msg); taken {
		return cmd
	}

	// REWIND MODE IS MODAL AT THE SAME RUNG AND FOR THE SAME REASON (rewind.go):
	// while it is up the draft box is not on the frame at all — a mode bar stands
	// in its position — so a key that fell through to the editor would type into a
	// box nobody can see, and the arrows it takes are the arrows that move the cut.
	// ctrl+c is read above it and stays the door.
	if cmd, taken := a.rewindKey(msg); taken {
		return cmd
	}

	// The welcome box reads two keys and gives every other one back (welcome.go):
	// ↑/↓ walk the recent sessions, enter opens the one they picked, and
	// anything else is the person starting work, which puts the box away for
	// good before the key does whatever it always does.
	if a.welcome.open {
		if cmd, taken := a.welcomeKey(msg.String()); taken {
			// enter on a recent session opens it, and what comes back is that
			// conversation's standing lanes (welcome.go's [app.resumeSession]).
			return cmd
		}
		a.dismissWelcome()
	}

	// tab is the path completion's key, and its only one: "/image " with tab
	// after it offers this directory's files, and tab again takes the one under
	// the cursor (files.go). It is read before the lists below because on this
	// surface tab means nothing else at all.
	if msg.String() == "tab" {
		return a.completePath()
	}
	// And enter belongs to the LINE under that list, not to the list. A person
	// who typed a path out in full would otherwise have it swapped for whatever
	// the ranking put first, by the key they pressed to run the command.
	if a.comp.open && a.comp.arg && msg.String() == "enter" {
		a.comp.close()
		return a.enter()
	}

	// The typed overlays — the command list, the @ file completion — hang UNDER
	// the draft rather than over it. They are not modal: the person keeps typing
	// into the same box and the list follows what they type. Only the keys that
	// move and commit a list are taken from the editor.
	if cmd, taken := a.listKey(msg); taken {
		return cmd
	}

	switch msg.String() {
	case "esc":
		// esc during a recall is the recall's: it puts the person's own draft
		// back. A modal-ish state that could not be left by the dismiss key
		// would be a trap, and the turn is still interruptible the moment the
		// walk ends.
		if a.recalling() {
			a.recallCancel()
			return nil
		}
		// THE DOUBLE ESC IS THE REWIND'S DOOR, and it is read here rather than
		// above the interrupt because the interrupt is not for sale (rewind.go):
		// the first esc means exactly what it always meant and ARMS the mode on its
		// way past, and only a second one inside the window is taken. A stray esc
		// after the window has lapsed changes nothing.
		cmd, taken := a.escRewind()
		if taken {
			return cmd
		}
		a.interrupt()
		// AND ESC WITH A MESSAGE WAITING SENDS IT NOW (park.go). Stopping the
		// answer is nearly all of it: the interrupt above closes the stream, and
		// the close is exactly where a parked message goes (app.go's
		// streamClosedMsg). This is the other case — a message parked against a
		// turn that has ALREADY ended, which has no close coming for it and would
		// otherwise sit above the box until the person typed something else.
		if a.state != stateWorking {
			return tea.Batch(cmd, a.sendParked())
		}
		return cmd

	case "enter":
		return a.enter()

	case "alt+enter", "ctrl+j":
		// Open a line. Two spellings because terminals disagree about which one
		// they can even send: alt+enter is the one people reach for, ctrl+j is
		// the one that survives every terminal that swallows it.
		a.input.insert("\n")
		return a.edited()

	case "ctrl+q":
		// The other way to say something to a working session: after the work,
		// not into it (followup.go). Enter stays steering.
		return a.followUp()

	case "ctrl+o":
		// A SELECTED COMPLETION CARD OWNS THIS KEY, because that card is the one
		// place on the surface that prints a key and says what it does with it —
		// "ctrl+o output", on its own second row (taskdone.go). With nothing
		// selected, which is every other moment, it is the tool cluster's fold it
		// has always been — of whichever list the body is drawing (render.go's
		// [app.bodyDeck]).
		// A SELECTED PROPOSAL OWNS IT TOO, for the other half of the same reason:
		// a click on that card opens the node's room now, so the brief keeps the
		// key rather than losing both gestures (task.go's [app.openCard]).
		if a.openDone(a.sel) || a.openCard(a.sel) {
			return nil
		}
		a.unfold(a.bodyTurn())
		return nil

	case "ctrl+g":
		// SEND THE RUNNING COMMAND TO THE BACKGROUND (background.go). It is
		// bound here, in the plain switch, and it takes the key ONLY while there
		// is a foreground bash call to promote — with nothing running it falls
		// through to the bottom of this router, where a key that carries no text
		// does nothing, which is what ctrl+g has always done.
		if a.backgroundRunning() {
			return nil
		}

	case "ctrl+b":
		// FREEZE AND READ (copymode.go). ctrl+b used to be the emacs `left` here,
		// alongside the arrow key that everybody actually presses, and it is spent
		// on this instead: the alt screen took the terminal's own selection away,
		// and getting text out of the conversation is a thing this surface could
		// not do at all. ← is untouched.
		a.enterCopy()
		return nil

	case selectKey:
		// DRAG THE WAY YOU DRAG EVERYWHERE ELSE (copymode.go). It is the mouse's
		// half of ctrl+b and it sits beside it for that reason. With the pointer
		// already the terminal's it does nothing, because the drag it offers is
		// one the person can already make.
		a.releaseMouse()
		return nil

	case "ctrl+,":
		// The settings key every application on this machine already has. The
		// slash is the other door onto the same panel (settings.go).
		a.openSettings()
		return nil

	case "pgup":
		a.scroll(-a.page())
		return nil
	case "pgdown":
		a.scroll(a.page())
		return nil

	case jumpKey:
		// BACK TO THE LIVE EDGE IN ONE KEY, from anywhere in the transcript and
		// with anything in the box (jumpchip.go). It is bound here, in the plain
		// switch, rather than beside a chord that means something else with an
		// empty draft: every other route to the bottom of the conversation is
		// spoken for the moment there is a sentence to move a caret through, and a
		// chip that prints a key has to be able to promise the key does this.
		a.toLatest()
		return nil

	case "up":
		// ↑ has four meanings and they are read in the order a person's hand
		// means them: inside a multi-line draft it moves the caret; at the top
		// of the draft it walks history; with nothing typed and calls on screen
		// it selects one; and past all of those it scrolls.
		if !a.input.onFirstLine() {
			a.input.up()
			a.touch()
			return nil
		}
		// A MESSAGE WAITING FOR THE ANSWER IS READ BEFORE THE HISTORY, and it has
		// to be: enter remembers everything it parks, so the newest history line
		// and the newest parked message are the same words — and a ↑ that walked
		// the history would put those words in the box while ALSO leaving them
		// parked, which is one message on screen twice and two turns spent on it.
		// Read here, the block is taken back and the sentence is yours again
		// (park.go).
		if a.input.empty() && !a.recalling() && a.recallParked() {
			return a.edited()
		}
		if a.recallBack() {
			return nil
		}
		if a.input.empty() && a.selectTool(-1) {
			return nil
		}
		a.scroll(-1)
		return nil

	case "down":
		if !a.input.onLastLine() {
			a.input.down()
			a.touch()
			return nil
		}
		if a.recallForward() {
			return nil
		}
		if a.input.empty() && a.selectTool(1) {
			return nil
		}
		a.scroll(1)
		return nil

	case "backspace":
		// With nothing typed, the thing behind the caret is the attachment tray:
		// backspace takes the last picture off it (attach.go). It is the same
		// gesture as deleting a character, applied to the only thing left to
		// delete, so nothing new has to be learned to undo an attachment.
		// A PICKED HARNESS IS THE LAST THING BEHIND THE CARET, after the
		// pictures: the tray is read right to left, which is the direction this
		// key deletes in and the order the row is drawn in (harnesspick.go).
		if len(a.input.value) == 0 && (a.dropChip() || a.dropHarnessChip()) {
			return a.edited()
		}
		a.input.deleteBackward()
		return a.edited()
	case "delete":
		a.input.deleteForward()
		return a.edited()
	case "ctrl+u", "super+backspace":
		// KILL TO THE START OF THE LINE, under both of its names. ctrl+u is the
		// readline one every shell on this machine has; super+backspace is the
		// same gesture on a Mac keyboard, where cmd+delete is what a person's
		// hand does without being told. It reaches this switch only on a
		// terminal that reports the super modifier at all (kitty's protocol,
		// win32-input) — everywhere else it is simply never sent, which costs
		// nothing and is why it is bound rather than detected.
		a.input.killToStart()
		return a.edited()
	case "ctrl+w", "alt+backspace", "ctrl+backspace":
		// DELETE THE WORD BEHIND THE CARET, under all three of its names.
		// ctrl+w is readline's; alt+backspace is the one both macOS and every
		// GTK/Qt text field agree on, and it is the one people actually press;
		// ctrl+backspace is Windows' and the terminals that speak the kitty
		// protocol send it faithfully.
		//
		// ctrl+h is deliberately NOT here. A terminal in backspace-sends-BS mode
		// delivers a plain backspace as ctrl+h (ultraviolet's key table maps
		// 0x08 that way), so binding it to a word kill would make one keyboard's
		// ordinary backspace eat a word at a time.
		a.input.deleteWord()
		return a.edited()
	case "left":
		// ← ON AN EMPTY BOX IS NAVIGATION. There is no caret to move in an empty
		// draft, which is the same argument the proposal's row makes for taking
		// ←/→ over its options (task.go) — and it is the only argument that
		// matters, because the key keeps its ordinary meaning the instant there
		// is a sentence to move through. See [app.navBack].
		if a.input.empty() {
			a.navBack()
			return nil
		}
		a.input.left()
		a.touch()
		return nil
	case "right":
		// → is the other half of it: forward, into the work (room.go).
		if a.input.empty() {
			return a.navForward()
		}
		a.input.right()
		a.touch()
		return nil
	case "ctrl+f":
		// The emacs forward-char keeps its plain meaning at both ends. It is the
		// caret key and nothing else, so nothing about the navigation above can
		// be reached by a chord somebody pressed to move one character.
		a.input.right()
		a.touch()
		return nil
	case "home", "ctrl+a":
		a.input.home()
		a.touch()
		return nil
	case "end", "ctrl+e":
		// ctrl+e has two meanings and they are read the way ↑'s four are: with
		// nothing typed it opens the model's thinking (thinking.go), and with a
		// sentence in the box it is end-of-line, where the caret is what the hand
		// meant. `end` is always end-of-line, so nothing is unreachable.
		if msg.String() == "ctrl+e" && a.input.empty() {
			if a.toggleLatestWorkfold() || a.toggleLatestThought() {
				return nil
			}
		}
		a.input.end()
		a.touch()
		return nil
	}

	// TWO SPACES IN AN EMPTY BOX ARE THE DOOR HOME (home.go). It is read here,
	// at the very bottom of the router, because it must lose to every other
	// meaning a space could have on this surface — inside a paste bracket, in a
	// filter box, in copy mode, in any overlay — and because the first of the
	// two spaces has already typed itself perfectly ordinarily one keystroke
	// ago, through the line below.
	if a.homeGesture(msg) {
		a.input.reset()
		return tea.Batch(a.edited(), a.openHome())
	}
	if text := msg.Key().Text; text != "" {
		// The ordinary case: a key that carries text types it.
		//
		// This line used to claim it also caught a paste that arrived as
		// keystrokes, and it could not: a pasted newline arrives as a key named
		// "enter", which the switch above matches and SUBMITS on, so a paste on a
		// terminal whose brackets leaked was sent to the model a line at a time.
		// The bracket is what catches that now, before this router is reached at
		// all (app.go's [app.pasteKey]).
		a.input.insert(text)
		return a.edited()
	}
	return nil
}

// enter is the submit key, and it has one first meaning: send the draft.
func (a *app) enter() tea.Cmd {
	line := strings.TrimSpace(a.input.String())
	// A FULL TRAY IS A MESSAGE. An empty box with a picture attached is not an
	// empty message — "what is this?" is often the picture itself — so the two
	// tests below both ask about the tray as well as about the words.
	held := len(a.chips) > 0
	// An empty draft with a call selected is a reader, not a typist: enter
	// opens what ↑/↓ picked out. A draft of any length is a sentence, and a
	// sentence wins.
	if line == "" && !held && a.sel >= 0 {
		a.openTool(a.sel)
		return nil
	}
	a.input.reset()
	a.endRecall()
	a.closeLists()
	if line == "" && !held {
		return a.edited()
	}
	a.stick = true
	// Everything the person pressed enter on is remembered, commands included:
	// "/model anthropic/…" is exactly the kind of line nobody wants to type
	// twice, and a recall list that held only the sentences would be a shell
	// history that dropped the commands.
	if line != "" {
		a.remember(line)
	}
	a.dropDraft()
	if strings.HasPrefix(line, "/") {
		// A command with a tray full is still a command: /image adds a second
		// picture rather than sending the first (attach.go). A picked harness
		// waits through it for the same reason — a slash is a thing said to this
		// surface, and the request is a thing said to the harness.
		return a.slash(line)
	}
	// A PICKED HARNESS TAKES THE SENTENCE, and it takes it whole: the person
	// chose the shape of the work off a list and then said what the work is, so
	// this runs that harness on exactly those words and nothing detects anything
	// (harnesspick.go). An empty box with a chip in the tray never reaches here —
	// the tray's own hint says to type the request, and enter on nothing is the
	// no-op it always was.
	if a.harnChip != "" {
		return a.runPickedHarness(line)
	}
	// EVERY "@task" IN THE SENTENCE GROWS ITS FOOTNOTE HERE, and here is after
	// the line has been remembered: what ↑ brings back is what the person typed,
	// and what goes to the model — and into the transcript, so they are the same
	// thing — is the sentence with its pointer blocks under it (taskmention.go).
	// A line with no mentions in it comes back untouched.
	line = a.expandTaskMentions(line)
	// AND A MESSAGE TYPED WHILE AN ANSWER IS STILL COMING WAITS FOR IT (park.go).
	// It is not sent, it is not spliced into the reply that is streaming, and it
	// is not lost: it is held in its own block above the box until the answer is
	// finished, where esc can send it early and ↑ or a click can pull it back to
	// be edited. Everything above this line — a slash command, a picked harness —
	// still happens at once, because those are things said to THIS SURFACE rather
	// than to the model.
	if a.parking() {
		return a.park(line)
	}
	if held {
		return a.submitImages(line)
	}
	return a.submit(line)
}

// completePath is tab: the file list over a command's path argument, opened if
// it is not up and committed if it is (files.go).
func (a *app) completePath() tea.Cmd {
	if a.comp.open && a.comp.arg {
		if _, ok := a.comp.choice(); ok {
			a.completeFile()
			return a.edited()
		}
	}
	if !a.comp.openArg(&a.input) {
		return nil
	}
	a.touch()
	return a.loadFiles()
}

// inputBlock renders the draft — or the picker's filter box in its place — and
// says where the caret sits inside it.
func (a *app) inputBlock(width int) ([]string, int, int) {
	// THE REWIND'S MODE BAR STANDS IN THE BOX'S OWN POSITION (rewind.go), for the
	// reason the two filter boxes below take it: the keyboard is pointed somewhere
	// else, and a draft drawn under a mode that has taken its keys is a box that
	// cannot be typed into. The caret rests at the bar's first cell — the mode has
	// no sentence to put one inside of.
	if a.rew.on {
		return a.rewindBar(width), 0, 0
	}
	if a.pick.open {
		return draftBlock(&a.pick.filter, a.pal, width, 1, pickerHint, "")
	}
	if a.memPanel.open {
		if a.memPanel.edit != nil {
			return draftBlock(a.memPanel.edit, a.pal, width, 1, memoryEditHint, "")
		}
		return draftBlock(&a.memPanel.filter, a.pal, width, 1, memoryFilterHint, "")
	}
	if a.roster.open {
		return draftBlock(&a.roster.filter, a.pal, width, 1, resumeHint, "")
	}
	// AND THE DELIVERABLES PICKER TAKES IT ON THE SAME TERMS, for whichever of
	// its two boxes is open: the filter, and the destination box over a row that
	// is being copied out (deliverables.go). Neither is a widget of its own —
	// both are this surface's one-line box, in the position it gives every box
	// that has taken the keyboard.
	if a.shelf.open {
		if dest := a.shelf.dest; dest != nil {
			return draftBlock(&dest.box, a.pal, width, 1, filesCopyHint, "")
		}
		return draftBlock(&a.shelf.filter, a.pal, width, 1, filesHint, "")
	}
	// AND THE CONNECTIONS PANEL TAKES IT ON THE SAME TERMS, for whichever of its
	// two boxes is open: the filter, once the catalog is long enough to be
	// searched rather than read, and the key box over a row that wants one
	// (connectpanel.go). Neither is a widget of its own — the filter is the
	// picker's box and the key box is the offer's row, in the position this
	// surface gives every box that has taken the keyboard.
	if a.connPanel.open {
		if entry := a.connPanel.entry; entry != nil {
			return keyBoxLines(entry, a.pal, width, 0)
		}
		if a.connPanel.filtering {
			return draftBlock(&a.connPanel.filter, a.pal, width, 1, connectFilterHint, "")
		}
	}
	// The box may not take the frame. Two rows are spoken for whatever happens
	// — the status line and the blank under it — and what is left over, up to
	// the ceiling, is the box's: a six-line paste into a four-line window shows
	// two rows and scrolls, rather than pushing off the line that says where
	// you are.
	_, height := a.size()
	rows := min(draftRows, height-2)
	if rows < 1 {
		rows = 1
	}
	// AND THE BOX SAYS WHICH ROOM IT IS TYPING INTO, as a segment in front of its
	// own prompt (room.go's [app.roomLead]). It is the main draft's alone: the
	// filter boxes above stand in this position while an overlay has the keyboard,
	// and none of them sends a word anywhere.
	block, caretX, caretRow := draftBlock(&a.input, a.pal, width, rows, "", a.roomLead(width))
	// THE TRAY IS PART OF THE BOX, not a fifth thing the frame has to know about
	// (attach.go). It is one row above the draft, so it is one row of this
	// block: every geometric question below the conversation already goes
	// through here, and a strip drawn anywhere else would be a row the
	// hit-testing and the height had to be told about separately.
	if strip := a.chipStrip(width); strip != "" {
		block = append([]string{strip}, block...)
		caretRow++
	}
	return block, caretX, caretRow
}

// inputHeight is how many rows the input block is taking. Every geometric
// question below the conversation goes through it, so a draft that grew to six
// rows takes those rows from the transcript and from nothing else.
func (a *app) inputHeight() int {
	width, _ := a.size()
	// THE BOX IS ASKED AT THE WIDTH THE FRAME LAYS IT OUT AT, which is the frame
	// less the one cell it is inset by (view.go's [inputPad]). Measured a cell
	// wider, a draft that wraps to three rows on screen could be counted as two —
	// and rows the geometry did not subtract are rows [app.frameOut] then loses
	// off the TOP of the window, which is where the room's header is.
	rows, _, _ := a.inputBlock(width - len(inputPad))
	return len(rows)
}

// draftBlock lays one editor out as a block: the prompt on its first row, every
// continuation aligned under the TEXT, soft-wrapped to the box's width, and at
// most maxRows of it on screen. It returns the rows, the caret's column, and
// the row the caret is on.
//
// A draft taller than maxRows scrolls INSIDE the box, keeping the caret in
// view, and marks the rows above with the same ellipsis the rest of the surface
// truncates with. Nothing about a long paste is allowed to move the
// conversation: the box grows to six rows and stops.
//
// The window is TOP-ANCHORED — the first line of the draft is the first row of
// the block until the caret walks past the cap — and the law is stated at the
// arithmetic below.
//
// hint is the placeholder shown while the editor is empty, and the picker's
// filter box is why it exists — the overlay explains itself in the box a person
// is already looking at instead of spending a row on a legend.
//
// lead is an already-painted segment drawn in FRONT of the prompt, and "" for
// every box but the main draft standing in a room (room.go's [app.roomLead]).
// Its width is charged to the box the way the prompt's is — the wrap, the
// continuation indent and the caret's column all count through the same number —
// because a lead the layout drew and the caret arithmetic did not know about
// would put the terminal's cursor several cells left of the letter it is on.
func draftBlock(e *editor, pal palette, width, maxRows int, hint, lead string) ([]string, int, int) {
	head := ansi.StringWidth(lead) + ansi.StringWidth(prompt)
	room := width - head
	if room < 4 {
		room = 4
	}
	if maxRows < 1 {
		maxRows = 1
	}
	if len(e.value) == 0 && hint != "" {
		return []string{lead + pal.dim(prompt) + pal.dim(fit(hint, room))}, head, 0
	}

	// THE BLOCK IS ANCHORED AT THE TOP AND TEXT FLOWS DOWN. The first row of the
	// draft is the first row of the box — prompt and all — and a second line
	// appears UNDER it, which is what every text field a person has ever typed
	// into does and what this one did not: the window used to be pinned to the
	// BOTTOM of the draft the moment it outgrew the cap, so a long paste showed
	// its tail and the "›" was the first thing to go.
	//
	// Scrolling starts only when the caret leaves the cap, and it follows the
	// caret by exactly as much as it must. The offset is DERIVED from the caret
	// rather than remembered, which is what makes it agree with itself: the
	// caret's row is returned to [app.View] as the terminal's cursor position
	// (view.go), and a stored top would be one frame's answer applied to another
	// frame's draft. [draftWindow] is where that arithmetic now lives.
	segments, caretRow, top, opening := draftWindow(e.value, e.cursor, room, maxRows)
	caretColumn := caretColumnIn(e, segments[caretRow])
	end := min(top+maxRows, len(segments))

	out := make([]string, 0, end-top)
	// Every row after the first is indented to where the text starts, segment
	// included: a continuation that began under the segment would be a wrapped
	// sentence with a step in its left margin.
	under := strings.Repeat(" ", head)
	for i := top; i < end; i++ {
		row0 := under
		switch {
		case i == 0 && opening:
			row0 = lead + pal.dim(prompt)
		case i == top:
			// The block is scrolled: say so where the prompt would be, in the
			// same two cells, so the rows do not shift under the caret.
			row0 = under[:head-ansi.StringWidth(prompt)] + pal.dim(glyphMore+" ")
		}
		// A RECOGNIZED SLASH COMMAND IS CHIPPED AS IT IS TYPED (slashchip.go).
		// The boundary question is asked of the DRAFT and not of the row,
		// because a row is a soft-wrapped slice of it: [wrapLine] breaks after a
		// space where it can, but a word longer than the box breaks mid-word, and
		// a row that opens in the middle of "cmd/aforge" does not open a token.
		at := segments[i].from
		boundary := at == 0 || e.value[at-1] == ' ' || e.value[at-1] == '\n'
		row := string(e.value[at:segments[i].to])
		out = append(out, row0+paintCommands(row, pal, pal.ink, boundary))
	}
	return out, head + caretColumn, caretRow - top
}

// segment is one soft-wrapped display row of the draft, as rune offsets into
// the editor's value.
type segment struct{ from, to int }

// draftWindow soft-wraps the rows the box can SHOW rather than the whole draft,
// and it is the reason a four-thousand-line paste does not make this surface
// crawl.
//
// THE WRAP OF A DRAFT IS THE WRAP OF ITS LOGICAL LINES, ONE AT A TIME. Every
// newline breaks a row unconditionally, so how a line wraps depends on that line
// and on nothing before it — which means the six rows around the caret can be
// laid out without touching the four thousand that are not on screen. The old
// arithmetic wrapped the entire value on the way to picking six of it, and it
// did so from [app.inputHeight] as well as from the render, so a big paste in
// the box cost a full re-wrap several times per FRAME and once per pointer cell.
// A person who pasted a stack trace and then moved the mouse was paying half a
// millisecond a step for rows nobody was going to see.
//
// It returns the rows it laid out, the caret's row within them, the first row
// the box draws, and whether that list OPENS the draft — the last one is what
// tells the caller to draw the "›" rather than the scrolled-past ellipsis, and
// it is a fact the caller can no longer get from an index, because index zero of
// a window is only row zero of the draft when the walk reached the top.
//
// The one draft this does not help is a single logical line with no newline in
// it at all — a minified blob out of a browser — where finding the caret means
// wrapping the line it is in. That is the same work the whole draft used to
// cost, on the one shape where it cannot be avoided.
func draftWindow(value []rune, cursor, room, maxRows int) ([]segment, int, int, bool) {
	cursor = min(max(cursor, 0), len(value))
	head, tail := lineHead(value, cursor), lineTail(value, cursor)
	// The caret's own line first: it is the only one that has to be wrapped to
	// answer where the caret is.
	segments := wrapLine(value, head, tail, room)
	caretRow := caretIn(segments, cursor, room, tail < len(value))
	// THEN BACKWARD, a line at a time, until the rows above the caret could fill
	// the box. A line is wrapped whole because that is the unit the wrap is
	// defined on — one of them can be worth twenty rows, and taking twenty is
	// cheaper than deciding not to.
	for caretRow < maxRows-1 && head > 0 {
		start := lineHead(value, head-1)
		above := wrapLine(value, start, head-1, room)
		segments = append(above, segments...)
		caretRow += len(above)
		head = start
	}
	top := max(0, caretRow-maxRows+1)
	// AND FORWARD ONLY WHERE THE BOX HAS ROOM LEFT. A caret at the end of a long
	// paste leaves none — the window ends on the caret's row — so this walks
	// nothing at all in the case that used to be the expensive one.
	for len(segments) < top+maxRows && tail < len(value) {
		start := tail + 1
		tail = lineTail(value, start)
		segments = append(segments, wrapLine(value, start, tail, room)...)
	}
	return segments, caretRow, top, head == 0
}

// lineHead and lineTail bound the LOGICAL line a rune offset sits on — the run
// between two newlines, which is [editor.lineStart] and [editor.lineEnd] asked
// about a position rather than about the caret.
func lineHead(value []rune, at int) int {
	for ; at > 0; at-- {
		if value[at-1] == '\n' {
			return at
		}
	}
	return 0
}

func lineTail(value []rune, at int) int {
	for ; at < len(value); at++ {
		if value[at] == '\n' {
			return at
		}
	}
	return len(value)
}

// wrapLine breaks ONE logical line into display rows: it fits what it can, and
// breaks at the last space before the edge or mid-word when the line offers no
// space to break at. An empty line still produces a row — the caret has to be
// able to stand on it.
func wrapLine(value []rune, from, to, room int) []segment {
	var out []segment
	for from < to {
		if to-from <= room {
			return append(out, segment{from: from, to: to})
		}
		cut := from + room
		for at := cut; at > from; at-- {
			if value[at-1] == ' ' {
				cut = at
				break
			}
		}
		out = append(out, segment{from: from, to: cut})
		from = cut
	}
	return append(out, segment{from: to, to: to})
}

// caretIn resolves the caret's row within a laid-out run of rows. A caret
// sitting exactly on a soft break belongs to the row that FOLLOWS it, which is
// where the next character it types will appear — and "follows" is asked of the
// draft rather than of the run, which is what more says: there are rows after
// this window that were not laid out because nobody is going to see them.
func caretIn(segments []segment, cursor, room int, more bool) int {
	row := 0
	for i, s := range segments {
		if cursor >= s.from && cursor <= s.to {
			row = i
			if cursor == s.to && cursor-s.from >= room && (i+1 < len(segments) || more) {
				continue
			}
			break
		}
	}
	return row
}

// caretColumnIn is how far into its row the caret sits, in cells rather than in
// runes: a draft holds whatever a person pasted into it, and a caret placed by
// counting runes would stand in the wrong column the moment one of them is wide.
func caretColumnIn(e *editor, s segment) int {
	from := s.from
	if e.cursor < from {
		from = e.cursor
	}
	return ansi.StringWidth(string(e.value[from:e.cursor]))
}
