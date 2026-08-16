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

func (e *editor) empty() bool { return len(strings.TrimSpace(string(e.value))) == 0 }

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
	// An approval question outranks even the model overlay: it is the one state
	// where the SESSION is blocked on this keyboard — a tool call is parked
	// mid-batch waiting for the answer — and everything else on this surface can
	// wait for one keystroke. ctrl+c is the exception it makes for itself
	// (consent.go).
	if cmd, taken := a.consentKey(msg); taken {
		return cmd
	}

	// The settings panel is the fullscreen overlay, and it is modal for the same
	// reason the picker is and one more: there is nothing else on the screen to
	// send a key to (settings.go).
	if a.sheet.open && msg.String() != "ctrl+c" {
		a.sheetKey(msg)
		return nil
	}

	// The model overlay is modal: while it is up every key belongs to it and
	// the draft below is suspended untouched. ctrl+c is the one exception, for
	// the same reason it is read first below — leaving is never modal.
	if a.pick.open && msg.String() != "ctrl+c" {
		a.pickerKey(msg)
		return nil
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

	// COPY MODE is modal, and it is modal one rung below ctrl+c for the same
	// reason everything else here is: leaving is never modal. While it is up the
	// surface is a reader, and a key that fell through to the draft would type
	// into a box whose effect is off screen (copymode.go).
	if cmd, taken := a.copyKey(msg); taken {
		return cmd
	}

	// The welcome box reads two keys and gives every other one back (welcome.go):
	// ↑/↓ walk the recent sessions, enter opens the one they picked, and
	// anything else is the person starting work, which puts the box away for
	// good before the key does whatever it always does.
	if a.welcome.open {
		if a.welcomeKey(msg.String()) {
			return nil
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
		a.interrupt()
		return nil

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
		if a.openDone(a.sel) {
			return nil
		}
		a.unfold(a.bodyTurn())
		return nil

	case "ctrl+b":
		// FREEZE AND READ (copymode.go). ctrl+b used to be the emacs `left` here,
		// alongside the arrow key that everybody actually presses, and it is spent
		// on this instead: the alt screen took the terminal's own selection away,
		// and getting text out of the conversation is a thing this surface could
		// not do at all. ← is untouched.
		a.enterCopy()
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
		if len(a.input.value) == 0 && a.dropChip() {
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
		if msg.String() == "ctrl+e" && a.input.empty() && a.toggleLatestThought() {
			return nil
		}
		a.input.end()
		a.touch()
		return nil
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
		// picture rather than sending the first (attach.go).
		return a.slash(line)
	}
	// EVERY "@task" IN THE SENTENCE GROWS ITS FOOTNOTE HERE, and here is after
	// the line has been remembered: what ↑ brings back is what the person typed,
	// and what goes to the model — and into the transcript, so they are the same
	// thing — is the sentence with its pointer blocks under it (taskmention.go).
	// A line with no mentions in it comes back untouched.
	line = a.expandTaskMentions(line)
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
	if a.pick.open {
		return draftBlock(&a.pick.filter, a.pal, width, 1, pickerHint)
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
	block, caretX, caretRow := draftBlock(&a.input, a.pal, width, rows, "")
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
	rows, _, _ := a.inputBlock(width)
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
// hint is the placeholder shown while the editor is empty, and the picker's
// filter box is why it exists — the overlay explains itself in the box a person
// is already looking at instead of spending a row on a legend.
func draftBlock(e *editor, pal palette, width, maxRows int, hint string) ([]string, int, int) {
	room := width - ansi.StringWidth(prompt)
	if room < 4 {
		room = 4
	}
	if maxRows < 1 {
		maxRows = 1
	}
	if len(e.value) == 0 && hint != "" {
		return []string{pal.dim(prompt) + pal.dim(fit(hint, room))}, ansi.StringWidth(prompt), 0
	}

	segments := wrapRunes(e.value, room)
	caretRow, caretColumn := caretAt(e, segments, room)

	top := 0
	if len(segments) > maxRows {
		top = len(segments) - maxRows
		if caretRow < top {
			top = caretRow
		}
	}
	end := min(top+maxRows, len(segments))

	out := make([]string, 0, end-top)
	for i := top; i < end; i++ {
		lead := "  "
		switch {
		case i == 0:
			lead = pal.dim(prompt)
		case i == top:
			// The block is scrolled: say so where the prompt would be, in the
			// same two cells, so the rows do not shift under the caret.
			lead = pal.dim(glyphMore + " ")
		}
		out = append(out, lead+pal.ink(string(e.value[segments[i].from:segments[i].to])))
	}
	return out, ansi.StringWidth(prompt) + caretColumn, caretRow - top
}

// segment is one soft-wrapped display row of the draft, as rune offsets into
// the editor's value.
type segment struct{ from, to int }

// wrapRunes breaks the draft into display rows: its own newlines always break,
// and a logical line longer than room breaks at the last space that fits, or
// mid-word when there is no space to break at. An empty logical line still
// produces a row — the caret has to be able to stand on it.
func wrapRunes(value []rune, room int) []segment {
	var out []segment
	line := 0
	flush := func(end int) {
		for line < end {
			if end-line <= room {
				out = append(out, segment{from: line, to: end})
				line = end
				return
			}
			cut := line + room
			for at := cut; at > line; at-- {
				if value[at-1] == ' ' {
					cut = at
					break
				}
			}
			out = append(out, segment{from: line, to: cut})
			line = cut
		}
		out = append(out, segment{from: end, to: end})
	}
	for at := 0; at < len(value); at++ {
		if value[at] == '\n' {
			flush(at)
			line = at + 1
		}
	}
	flush(len(value))
	if len(out) == 0 {
		out = append(out, segment{})
	}
	return out
}

// caretAt resolves the caret's row and column within the wrapped rows. A caret
// sitting exactly on a soft break belongs to the row that follows it, which is
// where the next character it types will appear.
func caretAt(e *editor, segments []segment, room int) (int, int) {
	row := 0
	for i, s := range segments {
		if e.cursor >= s.from && e.cursor <= s.to {
			row = i
			if e.cursor == s.to && e.cursor-s.from >= room && i+1 < len(segments) {
				continue
			}
			break
		}
	}
	s := segments[row]
	from := s.from
	if e.cursor < from {
		from = e.cursor
	}
	return row, ansi.StringWidth(string(e.value[from:e.cursor]))
}
