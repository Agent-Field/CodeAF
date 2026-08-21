package tui3

import (
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE WELCOME BOX: the first thing an empty session shows, and the last time it
// shows it.
//
// A terminal that opens on a bare prompt is a terminal that tells a first-time
// reader nothing and a returning one less: which model is answering, which
// directory this is, and — the fact a chat surface is worst at — WHAT WAS I
// DOING YESTERDAY. So an empty conversation opens with one box above the input:
// the wordmark and where you are on the left, the four sessions you were last
// in on the right, and enter or a click on one of them opens it.
//
// Three rules, and the first two are the whole of why this is not chrome:
//
//   - IT SHOWS ONCE. The first submit, the first key, the first click — any of
//     them and the box is gone for the life of the surface. Nothing brings it
//     back, because a box that returns is a box a person has to dismiss twice.
//   - IT NEVER SHOWS OVER A CONVERSATION. A resumed session has a transcript,
//     and the transcript is the answer to "where was I" — a welcome box above it
//     would be the surface answering a question the screen already answered.
//   - IT ANIMATES IN ONCE AND THEN IS STILL. A slow matte sweep across the
//     letters, easing out to static over about a second and a quarter, and then
//     nothing moves on this surface again until the person types. No loop, no
//     flash, no second run: motion that repeats is motion a reader has to learn
//     to ignore, which is the definition of noise.
//
// The animation is counted in FRAME SLOTS rather than measured against the
// clock (app.go's [frameInterval] is the only clock here), so what it looks
// like does not depend on how busy the machine was — and so a test can assert
// the settled state without sleeping through it. A slot is 33ms of wall time
// whether or not a frame was drawn in it (link.go), so the box takes the same
// second and a quarter to arrive over a connection as it does here.

// The animation's three lengths, in slots of the 33ms paint clock.
const (
	// welcomeSlide is the box arriving: about 300ms of a one-cell slide and a
	// fade up from dim.
	welcomeSlide = 9
	// welcomeSweep is the wordmark's breath: about 1.2s for the pastel to cross
	// the letters, easing out.
	welcomeSweep = 36
	// welcomeFrames is when everything is still. The paint clock stops asking
	// for ticks here (see [app.paint]) — nothing on this surface animates by
	// itself afterwards.
	welcomeFrames = welcomeSweep + 4
)

// welcomeSlots is how many recent sessions the right column holds. It is FIXED:
// the box is the same height with four sessions, with one, and with none, so
// the input line under it does not move while a person is reading the box above
// it.
const welcomeSlots = 4

// Session is one conversation this directory has had before: a row of the
// welcome box's right column, and a row of the resume picker (resume.go).
type Session struct {
	// Title is the name the session gave itself, empty for one that was never
	// named. A row with no words is a row nobody can choose between, so both
	// surfaces fall back rather than draw one: the box to the file's own name
	// (below), the picker down the whole ladder in [humanName].
	Title string
	// Opening is the first thing the person said in it, one line. It is what
	// lets an unnamed session still be CALLED something in the picker: a name
	// derived at DISPLAY time costs nothing and rewrites no transcript, which
	// is what keeps a session written by an older build listable by a newer one.
	Opening string
	// Last is the last thing that happened in it, one line — what the person
	// said last, or what the agent answered when they said it with a picture.
	// It is the row's description, and it is the field that answers the
	// question a person actually opens this list with.
	Last string
	// File is the transcript, and it is what [Options.Resume] is handed.
	File string
	// At is when it was last written.
	At time.Time
}

// welcome is the box's whole state. The zero value is a surface that never had
// one, which is what a resumed session is.
type welcome struct {
	open bool
	// spent says the box has already been dismissed. It is separate from open
	// so that nothing — a resize, a /new, a stray frame — can put it back.
	spent bool
	// step counts frames since it opened, and stops at [welcomeFrames].
	step int
	// sel is the recent row under the cursor, or -1. Only ↑/↓ on an empty draft
	// move it (see [app.welcomeKey]).
	sel    int
	recent []Session
}

func (w *welcome) animating() bool { return w.open && w.step < welcomeFrames }

// tick advances the animation by one frame's worth of slots and clamps at the
// end. The stride is the caller's (link.go's [app.frameStride]), so the sweep
// takes its second and a quarter whether that was forty frames or fourteen —
// an arrival animation that ran three times as long because the terminal is on
// a wire would be a box that has to be waited out.
func (w *welcome) tick(slots int) {
	if !w.open || w.step >= welcomeFrames {
		return
	}
	w.step = min(w.step+max(slots, 1), welcomeFrames)
}

// openWelcome decides, once, whether this surface gets a box. It is called
// from [newApp] after the replay, so "empty" means what a reader means by it:
// there is nothing on screen.
func (a *app) openWelcome() {
	if a.resumed || len(a.entries) > 0 {
		return
	}
	a.welcome = welcome{open: true, sel: -1}
	if a.recentSessions != nil {
		list := a.recentSessions()
		if len(list) > welcomeSlots {
			list = list[:welcomeSlots]
		}
		a.welcome.recent = list
	}
}

// dismissWelcome puts the box away for good.
func (a *app) dismissWelcome() {
	if !a.welcome.open {
		return
	}
	a.welcome = welcome{spent: true}
	a.touch()
}

// welcomeKey is the box's claim on the keyboard, and it is deliberately two
// keys wide.
//
// Everything dismisses the box — that is the contract — EXCEPT the walk through
// the recent list, which would otherwise be unreachable: a person cannot select
// a row with an arrow key if the arrow key closes the thing the row is in. So
// ↑/↓ over an empty draft move the selection, enter on a selected row opens it,
// and every other key is the person starting work, which is what dismissal
// means.
//
// It reports whether it took the key, and hands back whatever work the key
// started — which for enter on a recent session is the two standing lanes the
// conversation it just opened owes itself ([app.resumeSession]). A key it did
// not take still dismisses, and then goes on to do whatever it always does.
func (a *app) welcomeKey(name string) (tea.Cmd, bool) {
	if !a.welcome.open {
		return nil, false
	}
	switch name {
	case "up", "down":
		if !a.input.empty() || len(a.welcome.recent) == 0 {
			return nil, false
		}
		delta := 1
		if name == "up" {
			delta = -1
		}
		if a.welcome.sel < 0 {
			// From nowhere, either arrow takes the most recent session: it is
			// the top of the list and it is the row a person reaching for this
			// list means nine times in ten.
			a.welcome.sel = 0
		} else {
			a.welcome.sel = moveCursor(a.welcome.sel, delta, len(a.welcome.recent))
		}
		a.touch()
		return nil, true

	case "enter":
		if a.welcome.sel < 0 || a.welcome.sel >= len(a.welcome.recent) {
			return nil, false
		}
		chosen := a.welcome.recent[a.welcome.sel]
		a.dismissWelcome()
		return a.resumeSession(chosen), true
	}
	return nil, false
}

// welcomeKeeps is the one key that goes past the box WITHOUT putting it away.
//
// Everything else dismisses, and that is the box's contract: any key but the
// walk through the recent list is the person starting work here, which is what
// dismissal means ([app.welcomeKey]). tab over an empty box is the opposite of
// starting work here — it is leaving for the conversation you were in before
// (keeper.go) — and dismissal is irreversible ([app.dismissWelcome] sets spent),
// so one keystroke would do two unrelated things and only one of them could be
// undone. The box is still standing when they come back, because the sidecar
// kept it.
func welcomeKeeps(name string) bool { return name == "tab" }

// resumeSession swaps this surface onto an earlier conversation.
//
// It is [app.renew] with the sign flipped: the same close, the same wholesale
// reset of everything that belonged to the session being left, and then a
// replay instead of an empty screen. The two share no code because they share
// no seam — one asks the door for a NEW agent, the other for a named one — and
// the reset is written out here rather than factored so that a field added to
// the surface is a compile error in both places rather than a stale value in
// one.
//
// THE DUPLICATION IS NOT SAFE FOR WORK THAT IS RETURNED, and that is the whole
// reason this has a result at all. The line above is true of FIELDS: add one to
// the surface and both places stop compiling. A COMMAND is not a field, and
// [app.renew] ends with two of them — the standing task lane and the wake lane
// (task.go, followup.go) — which this function simply did not return. The cost
// was the whole point of the wake lane: a node landing on a resumed session
// starts a real turn, the model answers, and the events reach the journal and
// nothing else, because the closed agent took the lane with it and no pump was
// armed on the new one. Both lanes are re-opened here for the same reason /new
// re-opens them: they belong to the agent that handed them over.
func (a *app) resumeSession(chosen Session) tea.Cmd {
	cmd, refusal := a.openSession(chosen)
	if refusal != "" {
		a.note(refusal)
	}
	return cmd
}

// sessionBusyWord is what this surface says about a conversation another window
// is holding, and it is ONE SENTENCE IN ONE PLACE.
//
// It replaces the engine's own error, which reads
// `session file: /Users/…/b9c0d3ad…/transcript.jsonl is open in another aforge`
// — a full path, wrapped across two lines of somebody's conversation, naming a
// directory they have never had a reason to look at and a fact they cannot act
// on. The path is not the news. The news is that the conversation is open
// somewhere and what to do about it, and neither of those needs sixty
// characters of bookkeeping to say.
const sessionBusyWord = "open in another window — go there, or start a new conversation here"

// openSession swaps this surface onto an earlier conversation, and answers the
// command it owes plus a SENTENCE FOR A PERSON rather than an error — "" when
// it worked.
//
// THE NEW CONVERSATION IS OPENED BEFORE THE OLD ONE IS CLOSED, and that order is
// the whole repair. It used to be the other way round, so a resume that failed
// — the ordinary case of a second window on a session somebody already has open
// — closed this window's agent, failed to open the other, and left the surface
// holding a closed session with nothing to fall back to. Opening first means a
// refusal costs nothing at all: the conversation on screen is still the live one
// and still writable, and the person is exactly where they were.
//
// Two agents are briefly alive, which is fine and is not a lock conflict: they
// hold different files by construction, because every caller answers a request
// for the conversation already open by staying in it rather than reopening it.
func (a *app) openSession(chosen Session) (tea.Cmd, string) {
	if !a.canOpen() {
		// The picker's sentence, said once (resume.go): the box and the list are
		// two doors onto the same missing seam, and a surface that explained it
		// twice in two different words would read as two different faults.
		return nil, resumeUnavailableWord
	}
	// IDENTITY IS ASKED BEFORE THE LOCK IS. A transcript this process already
	// holds — on screen or open behind the screen — answers [session.InUse] TRUE
	// about itself, because a flock rides the open file description rather than
	// the process. Asking the door for it would meet our own lock and refuse
	// `open in another window` about a conversation one keystroke away, so the
	// keeper is consulted first and a hit is a switch rather than an open
	// (keeper.go's [app.bringForward]).
	if cmd, ours := a.bringForward(chosen.File); ours {
		return cmd, ""
	}
	conv, whole, err := a.openConversation(chosen.File)
	if err != nil {
		if errors.Is(err, session.ErrSessionLocked) {
			return nil, sessionBusyWord
		}
		return nil, "resume failed: " + err.Error()
	}
	// THE OLD CONVERSATION IS DETACHED AND THEN CLOSED, IN THAT ORDER, and the
	// two halves are separate for the whole of this wave's reason: detaching is
	// what a switch does and closing is what /resume does, and there is exactly
	// one implementation of "make this conversation the front one"
	// (switcher.go). Everything between the two lines below is what /resume
	// means that a switch does not.
	// A MESSAGE STILL WAITING FOR AN ANSWER GOES WITH THE CONVERSATION IT WAS
	// TYPED AT (park.go), and it is dropped BEFORE the detach so that the note
	// lands rather than the words being folded silently into the box. It was
	// parked against a reply that is about to stop existing, and there is no
	// turn end coming to send it — but the person typed those words, so this
	// says that it went. A SWITCH does the other thing, because there the turn
	// is still running (switcher.go's [aside]).
	a.dropParked()
	leaving := a.agent
	side := a.detachConversation()
	if leaving != nil {
		leaving.Interrupt()
		if err := leaving.Close(); err != nil {
			a.note("close failed: " + err.Error())
		}
	}
	if !whole {
		// The older seam hands back an agent alone, and a bundle with nine zero
		// fields would clear the recent list, the draft and the approval trio
		// ([app.takeUp] states this). The surface keeps what it was holding.
		conv = Conversation{Agent: conv.Agent, SessionFile: conv.SessionFile,
			Workspace: a.workspace, Place: a.place, Owned: a.owned,
			ContextWindow: a.ctxWindow, DraftFile: a.draftFile, History: a.history,
			RecentSessions: a.recentSessions, SaveApproval: a.saveApproval,
			SaveBashApproval: a.saveBashApproval, ApplyApprovals: a.applyApprovals}
	}
	cmd := a.attachConversation(conv, nil)
	// THE DRAFT GOES WITH THE PERSON RATHER THAN WITH THE CONVERSATION, which is
	// the promise /new already makes in those words ([app.renew]: "the sentence
	// in the box is the person's next one"). /resume closed a session; the
	// sentence somebody was part way through typing is still theirs.
	if side.draft != "" {
		a.input.setText(side.draft)
	}
	a.chips = side.chips
	a.resumed = true
	a.note("resumed " + a.hostedPath(a.file))
	if conv.Notice != "" {
		// The door had something to say about how this conversation came to be
		// open, and the entry line is where the first one's notice lands too
		// ([Options.Notice]).
		a.note(conv.Notice)
	}
	return cmd, ""
}

// welcomePress is a click inside the box: on a recent row it opens that
// session, anywhere else it is the person reaching past the box, which
// dismisses it.
func (a *app) welcomePress(slot int) tea.Cmd {
	if !a.welcome.open {
		return nil
	}
	if slot < 0 || slot >= len(a.welcome.recent) {
		a.dismissWelcome()
		return nil
	}
	chosen := a.welcome.recent[slot]
	a.dismissWelcome()
	if chosen.File != "" && convKey(chosen.File) == convKey(a.file) {
		// The conversation this window is already in. It is the picker's rule
		// (resume.go), and here it is also what keeps [app.openSession]'s
		// open-before-close safe: asking the door for our own journal would meet
		// our own flock.
		return nil
	}
	return a.resumeSession(chosen)
}

// ── the drawing ─────────────────────────────────────────────────────────────

// The wordmark, three rows of it, one entry per letter of [product]. It is
// drawn from box-drawing characters rather than from a figlet font because a
// figlet 'openaf' is nine rows of hash marks and this surface owns two: the
// letterform here is the same vocabulary the rail and the rules are drawn in,
// which is the whole reason it reads as part of the surface rather than as
// something pasted onto it.
var wordmarkGlyphs = map[rune][3]string{
	'o': {"┌─┐", "│ │", "└─┘"},
	'p': {"┌─┐", "├─┘", "│  "},
	'e': {"┌─┐", "├─ ", "└─┘"},
	'n': {"┌─┐", "│ │", "│ │"},
	'a': {"┌─┐", "├─┤", "└─┘"},
	'f': {"┌─ ", "├─ ", "│  "},
}

// wordmarkRows is the wordmark as three unpainted rows, and the column each
// letter starts on. A terminal that cannot draw the box characters gets the
// word itself — the same information, one row instead of three, and no
// mojibake (styles.go's [detectASCII] is the same veto the rail obeys).
func wordmarkRows(ascii bool) []string {
	if ascii {
		return []string{product}
	}
	rows := [3]string{}
	for i, letter := range product {
		glyph, ok := wordmarkGlyphs[letter]
		if !ok {
			continue
		}
		for r := 0; r < 3; r++ {
			if i > 0 {
				rows[r] += " "
			}
			rows[r] += glyph[r]
		}
	}
	return rows[:]
}

// sweepAt is where the breath has reached, as a column, easing out.
//
// The easing is 1-(1-t)² — fast at the start, almost stopped at the end — which
// is the difference between a sweep that arrives and one that simply travels.
// Past [welcomeSweep] it is off the end of the word and every letter is at rest.
func sweepAt(step, span int) int {
	if step >= welcomeSweep {
		return span + welcomeHead
	}
	if step <= 0 {
		return -welcomeHead
	}
	// Fixed point: the fractions here are small and integers are exact.
	t := step * 1000 / welcomeSweep
	eased := 1000 - (1000-t)*(1000-t)/1000
	return (span+2*welcomeHead)*eased/1000 - welcomeHead
}

// welcomeHead is how many cells of the sweep are lit at once. Three is a soft
// edge on a terminal that only has three tiers of ink to spend.
const welcomeHead = 3

// paintWordmark paints one row of the wordmark for this frame: accent where the
// sweep has been and where it is going, ink under its head. At rest — and on a
// terminal that has no hues — the whole word is the accent, which is the state
// this animation exists to arrive at rather than to decorate.
func (w *welcome) paintWordmark(row string, pal palette) string {
	head := sweepAt(w.step, ansi.StringWidth(row))
	if w.step >= welcomeSweep {
		return pal.accent(row)
	}
	out := ""
	for i, cell := range []rune(row) {
		text := string(cell)
		if i >= head-welcomeHead && i <= head {
			out += pal.ink(text)
			continue
		}
		out += pal.accent(text)
	}
	return out
}

// welcomeHeight is how many rows the box takes. It is a constant of the frame:
// the border, the wordmark or its ascii stand-in, the place line, and the four
// fixed slots beside them.
func (a *app) welcomeHeight() int {
	if !a.welcome.open {
		return 0
	}
	rows := a.welcomeRows(a.widthOr())
	return len(rows)
}

func (a *app) widthOr() int {
	width, _ := a.size()
	return width
}

// welcomeRows draws the box. The slot each row belongs to is answered by
// [app.welcomeSlotAt], from the same geometry, so a click cannot land on a
// session the frame drew somewhere else.
func (a *app) welcomeRows(width int) []string {
	if !a.welcome.open {
		return nil
	}
	// A frame this small has no room to be greeted in. The box would take the
	// conversation, the rule and half the input line with it, and a welcome
	// that leaves nowhere to type is not a welcome — so a small window simply
	// opens on the prompt, which is what it would have done anyway.
	if _, height := a.size(); height < 12 || width < 40 {
		return nil
	}
	w := &a.welcome
	pal := a.pal

	left := wordmarkRows(pal.ascii)
	body := make([]string, 0, len(left)+2)
	for _, row := range left {
		body = append(body, w.paintWordmark(row, pal))
	}
	body = append(body, "")
	// The place line is capped: a long model slug is a fact about the model and
	// not a reason for the wordmark's column to eat the sessions beside it.
	body = append(body, pal.dim(fit(a.placeLine(), 34)))

	right := w.recentRows(pal, a.hoveredSlot())
	for len(body) < len(right) {
		body = append(body, "")
	}
	for len(right) < len(body) {
		right = append(right, "")
	}

	// The box is inset by one cell, and by one more while it is arriving: the
	// whole slide is a single column, which is a movement a person notices
	// without watching.
	inset := 1
	if w.step < welcomeSlide {
		inset = 2
	}
	// The left column is as wide as what it holds — the wordmark, or the place
	// line under it, whichever is longer — plus a gutter. It is measured rather
	// than chosen so that the right column starts where the left one ACTUALLY
	// ends: a fixed split puts the sessions through the middle of the wordmark
	// on the day somebody's model slug is long.
	inner := width - inset - 2
	leftWidth := 0
	for _, line := range body {
		if w := ansi.StringWidth(ansi.Strip(line)); w > leftWidth {
			leftWidth = w
		}
	}
	leftWidth += 2
	rightWidth := inner - leftWidth - 2
	if rightWidth < 22 {
		// Too narrow for two columns. The sessions go rather than being wrapped
		// into stubs nobody could choose between — the box still says what this
		// is and what is answering, which is the half that fits.
		right = make([]string, len(body))
		rightWidth = 0
	}

	// THE BLOCK IS CENTRED IN THE BOX, not pushed against its left edge. The
	// right column is given the room it ASKS for rather than everything that is
	// left, because a sessions column stretched to the far wall is a column with
	// its content at one end and a hand's width of nothing after it — which is
	// what made a wide window look like a form somebody had abandoned halfway.
	//
	// Measured, never chosen: the wordmark decides the left column and the
	// longest session name decides the right, so the pair sits in the middle of
	// whatever window it is drawn in and nothing has to be re-tuned when either
	// one changes.
	used := 0
	for _, line := range right {
		if w := ansi.StringWidth(ansi.Strip(line)); w > used {
			used = w
		}
	}
	if used > rightWidth {
		used = rightWidth
	}
	lead := (inner - 2 - leftWidth - used) / 2
	if lead < 0 {
		lead = 0
	}
	middle := strings.Repeat(" ", lead)

	pad := strings.Repeat(" ", inset)
	out := make([]string, 0, len(body)+2)
	out = append(out, pad+pal.dim(boxTop(inner, pal.ascii)))
	for i, line := range body {
		cell := line
		if gap := leftWidth - ansi.StringWidth(ansi.Strip(line)); gap > 0 {
			cell += strings.Repeat(" ", gap)
		}
		row := middle + cell + fitPainted(right[i], used)
		if gap := inner - 2 - ansi.StringWidth(ansi.Strip(row)); gap > 0 {
			row += strings.Repeat(" ", gap)
		}
		out = append(out, pad+pal.dim(boxSide(pal.ascii))+" "+row+" "+pal.dim(boxSide(pal.ascii)))
	}
	out = append(out, pad+pal.dim(boxBottom(inner, pal.ascii)))
	return out
}

// fitPainted truncates a row that is already painted. It measures the plain
// text and gives up rather than cutting an escape sequence in half.
func fitPainted(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(ansi.Strip(text)) <= width {
		return text
	}
	return ansi.Truncate(text, width, glyphMore)
}

// placeLine is the left column's second half: what is answering, and where.
func (a *app) placeLine() string {
	model := a.model
	if model == "" {
		model = "no model"
	}
	place := a.place
	if place == "" {
		place = "here"
	}
	return model + " · " + place
}

// hoveredSlot is the recent session the pointer is over, or -1.
func (a *app) hoveredSlot() int {
	if a.hot.kind == hoverWelcome {
		return a.hot.index
	}
	return -1
}

// recentRows is the right column: the heading, then [welcomeSlots] rows whether
// or not there is anything to put in them.
func (w *welcome) recentRows(pal palette, hover int) []string {
	out := make([]string, 0, welcomeSlots+2)
	out = append(out, pal.dim("recent sessions"))
	out = append(out, "")
	if len(w.recent) == 0 {
		out = append(out, pal.dim("no recent sessions"))
		for len(out) < welcomeSlots+2 {
			out = append(out, "")
		}
		return out
	}
	for i := 0; i < welcomeSlots; i++ {
		if i >= len(w.recent) {
			out = append(out, "")
			continue
		}
		out = append(out, w.recentRow(i, pal, i == hover))
	}
	return out
}

func (w *welcome) recentRow(i int, pal palette, hovered bool) string {
	session := w.recent[i]
	// The name is read back as words when it arrived as ONE TOKEN, and left alone
	// otherwise (names.go's [readableName]). That is the whole difference between
	// this row and the resume picker's: the picker climbs a ladder and title-cases
	// what it finds, because it is a page a person went to on purpose and can
	// spend the width; this column is 24 cells wide and its whole voice is this
	// surface's lowercase — the heading above these rows is "recent sessions" — so
	// a sentence that already reads as one is not touched, and the only name that
	// changes here is the machine token nobody could read either way.
	//
	// The row still OPENS the file it was read from — [app.welcomePress] resumes
	// Session.File — so nothing that identifies the session is touched here.
	name := readableName(session.Title)
	if name == "" {
		name = sessionStem(session.File)
	}
	when := since(session.At)
	line := fit(name, 24)
	if when != "" {
		line += "  " + when
	}
	switch {
	case i == w.sel:
		return pal.accent(glyphYou) + pal.bold(pal.ink(line))
	case hovered:
		// The pointer's own lead, the same one every list on this surface draws
		// under a pointer (palette.go's overlayRow).
		return pal.accent("· ") + pal.ink(line)
	}
	return "  " + pal.dim(line)
}

// welcomeSlotAt resolves one row of the box to the recent session drawn on it,
// or -1. It counts from the SAME layout [app.welcomeRows] draws: the border,
// the heading, the blank, then one row per slot.
func (a *app) welcomeSlotAt(row int) int {
	if !a.welcome.open || len(a.welcome.recent) == 0 {
		return -1
	}
	// border(1) + heading(1) + blank(1)
	slot := row - 3
	if slot < 0 || slot >= len(a.welcome.recent) {
		return -1
	}
	return slot
}

func baseName(path string) string {
	if at := strings.LastIndexByte(path, '/'); at >= 0 {
		return path[at+1:]
	}
	if path == "" {
		return "session"
	}
	return path
}

// since is the relative time in the right column. It is coarse on purpose: the
// question a person asks of this list is "which one was I in", and "3d" answers
// it where a timestamp would have to be read.
func since(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	d := time.Since(at)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return itoa(int(d/time.Minute)) + "m"
	case d < 24*time.Hour:
		return itoa(int(d/time.Hour)) + "h"
	case d < 30*24*time.Hour:
		return itoa(int(d/(24*time.Hour))) + "d"
	default:
		return at.Format("2 Jan")
	}
}

// The box's own furniture. It is the only border this surface draws, and it is
// drawn for exactly one reason: the welcome box is an OBJECT that goes away,
// and the rule above the input — this surface's one line — is a seam that
// stays. Two different things, two different marks.
func boxTop(inner int, ascii bool) string {
	if ascii {
		return "+" + strings.Repeat("-", inner) + "+"
	}
	return "╭" + strings.Repeat("─", inner) + "╮"
}

func boxBottom(inner int, ascii bool) string {
	if ascii {
		return "+" + strings.Repeat("-", inner) + "+"
	}
	return "╰" + strings.Repeat("─", inner) + "╯"
}

func boxSide(ascii bool) string {
	if ascii {
		return "|"
	}
	return "│"
}
