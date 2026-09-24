package tui3

import (
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE WELCOME: the first thing an empty session shows, and the last time it
// shows it.
//
// A terminal that opens on a bare prompt is a terminal that tells a first-time
// reader nothing and a returning one less: which model is answering, what it
// will cost to talk to, and — the fact a chat surface is worst at — WHAT WAS I
// DOING YESTERDAY. So an empty conversation opens as ONE CENTRED UNIT and
// nothing else: the wordmark, the model and the crew under it, the message box
// itself with the caret in it, one dim line of things to try, and — only where
// there are any — the sessions this directory was last in, which enter or a
// click opens.
//
// THE BOX IS IN THE MIDDLE OF THE SCREEN BECAUSE THAT IS WHERE THE EYE IS. It
// used to be a bordered box at the top with the caret alone forty rows below it,
// a full-height column of `+ /task` doors beside nothing, and a status line
// billing `$0.00` for a conversation that had not happened. Every one of those
// was furniture drawn to mark an absence. Now the first keystroke lands where
// a person is already looking, and the frame's ordinary furniture — the input at
// the foot, the legend, the column on the right, the telemetry — arrives with
// the conversation rather than before it (view.go, task.go, render.go each gate
// their piece on this state).
//
// Three rules, and the first two are the whole of why this is not chrome:
//
//   - IT SHOWS ONCE. The first submit, the first key, the first click — any of
//     them and the unit is gone for the life of the surface, and the box is back
//     at the foot of the frame with the keystroke in it. Nothing brings it back,
//     because a greeting that returns is a greeting a person has to dismiss
//     twice.
//   - IT NEVER SHOWS OVER A CONVERSATION. A resumed session has a transcript,
//     and the transcript is the answer to "where was I" — a greeting above it
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
// whether or not a frame was drawn in it (link.go), so the unit takes the same
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

// welcomeSlots is the most recent sessions the unit lists. It is a CAP and not
// a reservation: four rows with four sessions, one with one, and nothing at all
// with none — an empty list is the emptiness law's plainest case, and a heading
// over four blank slots was a row spent announcing it.
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
	// start says this unit is the NEW-CHAT START PAGE rather than the greeting
	// (chatstart.go). It is the same drawing and the same rows, and it changes
	// three things about the unit's behaviour, each stated where it acts:
	//
	//   - IT DOES NOT DISMISS. The greeting's contract is that any key puts it
	//     away, because any key is the person starting work here; the start page
	//     IS the place they are starting work, so typing into it keeps it up and
	//     its only exits are esc and the first message ([app.dismissWelcome]).
	//   - IT DOES NOT ANIMATE. The sweep is an arrival, and this unit arrives
	//     every time somebody presses `+`. Motion that repeats is motion a reader
	//     learns to ignore, which is the greeting's own law read the other way,
	//     so the page opens settled ([app.openChatStart] sets step).
	//   - IT CAN SPEAK. A door that refused has to say so where the person is
	//     standing, and while this page is up the transcript is not on the frame
	//     at all (msg below).
	start bool
	// first says this is the FIRST CONVERSATION on this machine — the greeting
	// that opens the moment the setup screen goes — and it changes three things
	// about the unit, each stated where it acts (onboarding.go holds the screen
	// before it, and docs/design/onboarding/DESIGN.md the whole journey):
	//
	//   - IT SAYS WHAT THIS BOX IS FOR. A heading, one instruction and the folder
	//     the conversation is standing in, above three starting points. Every
	//     later greeting is the model line and the box, because by then the person
	//     knows what they are looking at.
	//   - THE COMPOSER DOES NOT MOVE WHEN TYPING BEGINS. The ordinary greeting is
	//     dismissed by the first keystroke and the box drops to the foot of the
	//     frame, which on a first conversation moves the one thing the person was
	//     aiming at, mid-word. This unit stands until the message is SENT
	//     ([app.spendWelcome]).
	//   - ITS STARTING POINTS FILL THE BOX AND NEVER SEND IT.
	first bool
	// starter is the starting point under the cursor, or -1. Only ↑/↓ over an
	// EMPTY draft move it, which is the same rule the recent list keeps — and it
	// is what makes it impossible for a starting point to overwrite something
	// somebody had already typed.
	starter int
	// msg is the one line this page has for the person — a door that could not
	// open a conversation, said where they are looking rather than into a
	// transcript underneath the page (chatstart.go's [app.startSay]). It is empty
	// on every ordinary frame and the emptiness law applies: no line is drawn.
	msg string
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
	a.welcome = welcome{open: true, sel: -1, starter: -1}
	if a.recentSessions != nil {
		list := a.recentSessions()
		if len(list) > welcomeSlots {
			list = list[:welcomeSlots]
		}
		a.welcome.recent = list
	}
	// A FIRST CONVERSATION IS ONE THIS PROFILE HAS NOT HAD, and it takes BOTH
	// halves. The marker the setup writes is the one honest record that the setup
	// has not been met ([config.SetupSeenAt]), read here before [app.openSetup]
	// stamps it, because this greeting is what stands behind that screen. And a
	// directory with conversations already in it is not having its first one
	// whatever the marker says — a profile restored from a backup, a marker
	// written by an older build — so the recent list has a veto.
	a.welcome.first = len(a.welcome.recent) == 0 && config.SetupSeenAt(a.profileDir).IsZero()
}

// dismissWelcome puts the unit away for good, and says the two keys that leave.
//
// THE EXIT IS TAUGHT AFTER THE ENTRANCE. `esc interrupts · ctrl+c quits`
// used to be the first line of every session, drawn above a greeting whose whole
// job was to get somebody to type their first sentence — a way out, offered
// before the way in. So while the unit is up the transcript carries nothing,
// and the line lands here, at the moment the conversation begins, where it is
// the first thing above the box a person has just started typing into. A
// session that never had a unit — a resumed one — gets the line on its first
// frame as it always did ([newApp]).
func (a *app) dismissWelcome() {
	if !a.welcome.open {
		return
	}
	// AND THE START PAGE IS NOT DISMISSED, BY ANYTHING (chatstart.go). Dismissal
	// is irreversible — it sets spent — and the start page is a navigation action
	// that must come back to exactly where it was pressed, with the sentence that
	// was in the box. Every road into this function is a keystroke or a click
	// that means "the person is starting work here", which on the start page is
	// what they are already doing: the page's own two endings are `esc` and the
	// first message, and both go through [app.cancelChatStart] or
	// [app.startChatEnter] instead.
	if a.welcome.start {
		return
	}
	a.welcome = welcome{spent: true}
	a.noteLandingKeys()
	a.touch()
}

// welcomeStandsThroughTyping reports that this unit is NOT to be dismissed by a
// keystroke or a paste.
//
// IT IS ASKED AT THE TYPING DOORS AND NOT INSIDE [app.dismissWelcome], and the
// difference matters: home landing, a page opening, a session resuming all
// dismiss the greeting for reasons that have nothing to do with the keyboard, and
// a refusal buried in the dismissal itself left the box standing underneath home.
// What the first conversation changes is one thing — that typing does not move
// the composer out from under the person — so that is the one place it is said.
func (a *app) welcomeStandsThroughTyping() bool {
	return a.welcome.open && a.welcome.first
}

// spendWelcome is the send putting the first conversation's greeting away, at the
// moment the person's own words go to a model — which is the moment it has done
// its job. Every later greeting has been gone since the first keystroke and this
// finds nothing to do.
func (a *app) spendWelcome() {
	if !a.welcome.open || a.welcome.start {
		return
	}
	a.dismissWelcome()
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
	// THE FIRST CONVERSATION'S STARTING POINTS TAKE ↑/↓ AND ENTER, and they take
	// them only over an empty box — which is the recent list's own rule, kept for
	// the reason it was written: a person who has typed something has said what
	// they want, and a list that could still be walked would be a list that can
	// overwrite it.
	if a.welcome.first {
		return a.welcomeStarterKey(name)
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

// welcomeStarter is one of the three starting points the first conversation
// offers. It is a thing to SAY and not a thing to run: `fills` lands in the box
// with the caret after it, and the person edits it, adds to it, or deletes it.
type welcomeStarter struct {
	// word is the row, in the fewest words that still name a kind of work.
	word string
	// fills is the sentence it puts in the box. Two of the three are deliberately
	// UNFINISHED — a starting point that filled the box with a complete request
	// about somebody else's project would be the surface guessing at the work.
	fills string
	// helper is the one line the SELECTED row gets, and it says what actually
	// happens next. It may not promise anything the program does not do: reading
	// files is allowed without asking and says so, longer work is proposed as a
	// task with a countdown that can be stopped, and neither sentence claims a
	// result.
	helper string
}

// welcomeStarters are the three, in the order they are offered: the one nearly
// everybody wants first, then making a change, then weighing two options.
var welcomeStarters = []welcomeStarter{
	{
		word:   "Understand this folder",
		fills:  "What is in this folder, and where would I start?",
		helper: "It reads what is here and answers in the conversation.",
	},
	{
		word:   "Make or fix something",
		fills:  "Fix this for me: ",
		helper: "Longer work is proposed as a task first, with a countdown you can stop.",
	},
	{
		word:   "Compare two options",
		fills:  "Compare these two options: ",
		helper: "It weighs each one and says which it would pick, with its reasons.",
	},
}

// The first conversation's own two lines. The heading is the SHORT one — "What
// would you like to work on?" — because the box under it is the answer, and the
// instruction says the two things a person can do with it.
const (
	welcomeFirstTitle = "What would you like to work on?"
	welcomeFirstWord  = "Choose a starting point or type your request."
)

// welcomeStarterKey is the first conversation's claim on ↑/↓ and enter.
//
// ENTER ON A STARTING POINT FILLS THE BOX AND SENDS NOTHING. That is the whole
// contract: the sentence lands in the composer, the caret goes after it, and the
// next thing that happens is whatever the person types. An enter with no row
// selected is an ordinary send and is not taken here.
func (a *app) welcomeStarterKey(name string) (tea.Cmd, bool) {
	w := &a.welcome
	switch name {
	case "up", "down":
		if !a.input.empty() {
			return nil, false
		}
		delta := 1
		if name == "up" {
			delta = -1
		}
		if w.starter < 0 {
			// From nowhere, either arrow takes the first row: it is the top of the
			// list and it is the one somebody reaching for this list means.
			w.starter = 0
		} else {
			w.starter = moveCursor(w.starter, delta, len(welcomeStarters))
		}
		a.touch()
		return nil, true
	case "enter":
		if w.starter < 0 || w.starter >= len(welcomeStarters) {
			return nil, false
		}
		a.takeStarter(w.starter)
		return nil, true
	}
	// Every other key is the person typing, and the unit stays standing for it.
	return nil, false
}

// takeStarter puts a starting point's sentence in the box.
//
// IT REFUSES TO OVERWRITE. The selection can only be moved over an empty box, so
// in practice nothing is ever there — and the guard is here anyway, because a
// click can reach a row the keyboard could not and a draft is somebody's own
// words.
func (a *app) takeStarter(at int) {
	if at < 0 || at >= len(welcomeStarters) || !a.input.empty() {
		return
	}
	a.welcome.starter = at
	a.input.setText(welcomeStarters[at].fills)
	a.touch()
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
// `session file: /Users/…/b9c0d3ad…/transcript.jsonl is open in another codeaf`
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
	// AND ON A SHARED HANDLE THERE IS NOTHING HERE TO CLOSE, which is the one
	// thing the open-before-close repair above could not have known about.
	// [Options.SharedAgent] doors hand the SAME agent back out of the door called
	// four lines up, now naming the conversation the engine has just swapped to —
	// so `leaving` is not the conversation being left, and interrupting and
	// closing it would stop and flush the session that was just opened, in front
	// of the person who asked for it. The previous conversation is already ended,
	// by the engine, as part of the swap (internal/remote's Session.swap).
	if leaving != nil && !a.shared {
		leaving.InterruptFor(session.StopByLeaving)
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
	// And the documents its compact tokens stand for, for [app.renew]'s reason
	// exactly (recipient.go). The conversation being opened brings its own task
	// pages, so nothing typed at the old one's pages comes with it.
	a.pastes = append([]pasteChip(nil), side.pastes...)
	a.resumed = true
	// THE OTHER DOOR ONTO THE SAME LINE, and it says the same thing now: WHICH
	// CONVERSATION this is, and the path only where there is no name to give
	// ([app.resumedNote]). This road — the greeting's recent list and /resume —
	// still spelled the absolute transcript out, so opening a conversation from
	// the picker put four to six wrapped rows of `.codeaf/v3/projects/…` above
	// the person's own first message while opening the very same conversation
	// from the launch line said its name. One sentence, one door.
	a.note(a.resumedNote())
	if conv.Notice != "" {
		// The door had something to say about how this conversation came to be
		// open, and the entry line is where the first one's notice lands too
		// ([Options.Notice]).
		a.note(conv.Notice)
	}
	return cmd, ""
}

// welcomeRowPress is a click on one row of the unit: on a recent session it
// opens that session, on the message box it does nothing — the keyboard is
// already there, and a click that yanked the box to the foot of the frame would
// be the surface moving the thing a person just aimed at — and anywhere else it
// is the person reaching past the greeting, which dismisses it.
func (a *app) welcomeRowPress(row int) tea.Cmd {
	if !a.welcome.open {
		return nil
	}
	if a.welcomeInputRow(row) {
		return nil
	}
	// A STARTING POINT PRESSED DOES WHAT ENTER ON IT DOES, and the greeting stays
	// standing: it filled the box, which is where the person is now looking.
	if mark, ok := a.welcomeMarkAt(row); ok && mark.kind == welcomeRowStarter {
		a.takeStarter(mark.slot)
		return nil
	}
	return a.welcomePress(a.welcomeSlotAt(row))
}

// welcomePress opens the recent session in this slot, and dismisses the
// greeting for a slot that names none. It is the door's own seam — every way a
// person opens a session comes back through it with that conversation's lanes
// (resumelanes_test.go) — which is why the row's press above resolves to a slot
// before it gets here.
func (a *app) welcomePress(slot int) tea.Cmd {
	if !a.welcome.open {
		return nil
	}
	// THE START PAGE'S ROWS ARE DOORS AND ITS BLANK CELLS ARE NOT (chatstart.go).
	// A press that is not on a recent session is a press on the page, and the
	// page stays: dismissal is the greeting's answer to being reached past, and
	// this unit has nothing behind it to reach.
	if a.welcome.start {
		if slot < 0 || slot >= len(a.welcome.recent) {
			return nil
		}
		return a.startChatOpen(slot)
	}
	if slot < 0 || slot >= len(a.welcome.recent) {
		a.dismissWelcome()
		return nil
	}
	chosen := a.welcome.recent[slot]
	a.dismissWelcome()
	if chosen.File != "" && a.convKey(chosen.File) == a.convKey(a.file) {
		// The conversation this window is already in. It is the picker's rule
		// (resume.go), and here it is also what keeps [app.openSession]'s
		// open-before-close safe: asking the door for our own journal would meet
		// our own flock.
		return nil
	}
	return a.resumeSession(chosen)
}

// ── the drawing ─────────────────────────────────────────────────────────────

// The wordmark, four rows of it, and it MUST HOLD A LETTERFORM FOR EVERY LETTER
// OF [product] — [wordmarkRows] skips a letter it has never heard of, so a
// missing glyph is not a build error, it is a word with a hole in it on the
// first screen of a fresh install (TestTheWordmarkCanSpellTheProductsWholeName
// is what holds the two together).
//
// THE WORD IS SPELLED `codeaf` AND DRAWN `CodeAF`. The table is keyed by the
// letters of [product], which is the one spelling every sentence uses, but the
// forms under `a` and `f` are capitals: the owner wants the two letters at the
// end to stand up out of the word (2026-09-17), and a letterform table is the
// one place a drawing may differ from the text without the text changing. The
// terminal that gets the word instead of the drawing still gets `codeaf`.
//
// THE TOP ROW IS THE CAP LINE AND THE BASELINE IS ONE LINE, and both come from
// one fact about box-drawing: a bar sits at the MIDDLE of its cell and only a
// vertical reaches a cell's edge. So a letter whose top is a bar (`c`, `o`,
// `e`, the capitals) tops out half a row below a letter whose top is a stem,
// and a letter whose foot is a stem hangs half a row under one whose foot is
// a bowl. With three rows that put the `d`'s ascender half a row over the
// capitals' tops, which is the one thing a capital is not allowed to be. Now
// the x-height letters live in the lower three rows and leave the top row
// blank; the capitals and the ascender start in the top row, the ascender as
// the half-stroke `╷` so its top is the capitals' bar and not the edge above
// it; and every stem that ends on the baseline ends as the half-stroke `╵`,
// which stops where a bowl's `└─┘` does. A `│` in the bottom row is a
// DESCENDER and nothing else.
//
// It is drawn from box-drawing characters rather than from a figlet font because
// a figlet wordmark is nine rows of hash marks and this surface owns two: the
// letterform here is the same vocabulary the rail and the rules are drawn in,
// which is the whole reason it reads as part of the surface rather than as
// something pasted onto it.
var wordmarkGlyphs = map[rune][4]string{
	// The bowl open on the right, on the stem `r` is drawn with.
	'c': {"   ", "┌─ ", "│  ", "└─ "},
	'o': {"   ", "┌─┐", "│ │", "└─┘"},
	// The bowl of an `o` with the ascender on its right: the one lowercase
	// letter of this name that rises to the cap line, and it rises to it and
	// not past it, because `╷` starts at the middle of its cell where the
	// capitals' top bars are, and `│` would start at the edge half a row over
	// them — which is the discrepancy the fourth row exists to remove.
	'd': {"  ╷", "┌─┤", "│ │", "└─┘"},
	'p': {"   ", "┌─┐", "├─┘", "│  "},
	// THE CROSSBAR ENDS IN A TERMINAL AND NOT IN A BLANK. `e` sits between two
	// closed letterforms, and its right column used to be `├─ `, a blank cell
	// with the bowl's `┐` directly above it and its `┘` directly below. A hole
	// punched into a block of box-drawing between two inked cells does not read
	// as an open letterform; it reads as a word the terminal cut off, which is
	// what the wave that found this filed it as. The half-stroke closes that
	// cell while keeping the aperture a lowercase `e` has and `a` (`├─┤`) has
	// not — the one cell that tells those two letters apart here. `c` needs no
	// such closing: its right column is blank on all three rows, so the eye
	// reads a letter that ends rather than a stroke that is missing, and `f`
	// has ink in that column on its top row alone, with nothing under it to
	// make a hole of the two blanks below.
	'e': {"   ", "┌─┐", "├─╴", "└─┘"},
	'n': {"   ", "┌─┐", "│ │", "╵ ╵"},
	// A CAPITAL: the apex on the cap line, the crossbar a third of the way up,
	// and two legs that reach the baseline apart. It used to be `┌─┐ / ├─┤ /
	// └─┘`, which is not an `a` but two closed bowls of equal size stacked on
	// each other, and two equal closed bowls are an `8` — which is what the
	// owner read on the first screen. Opening the bottom is what makes it a
	// letter, and the crossbar is what keeps it from being the `n` above.
	'a': {"┌─┐", "│ │", "├─┤", "╵ ╵"},
	// A CAPITAL, AND THE TOP ARM IS THE LONGER ONE. Both arms of the old `f`
	// were two cells, which is a lowercase `f` drawn without its hook and reads
	// as neither case in particular; a capital `F` is the one letter of the Latin
	// alphabet whose case is told by its arms alone, the upper reaching past the
	// lower, and its middle arm sits above the centre as the letter's does. That
	// third cell is the only ink in the word's right-hand column, so the edge
	// below it is a letter stopping and not a hole between strokes
	// (TestTheWordmarksRightEdgeIsNeverAHoleBetweenTwoStrokes is the line).
	'f': {"┌──", "├─ ", "│  ", "╵  "},
	// The shoulder alone, on a stem that stops at the baseline.
	'r': {"   ", "┌─┐", "│  ", "╵  "},
	// The bowl of an `o` with the tail under it, which is the one letter of this
	// table that hangs below the line.
	'g': {"   ", "┌─┐", "└─┤", "└─┘"},
}

// wordmarkRows is the wordmark as four unpainted rows, and the column each
// letter starts on. A terminal that cannot draw the box characters gets the
// word itself — the same information, one row instead of four, and no
// mojibake (styles.go's [detectASCII] is the same veto the rail obeys).
func wordmarkRows(ascii bool) []string {
	if ascii {
		return []string{product}
	}
	rows := [4]string{}
	for i, letter := range product {
		glyph, ok := wordmarkGlyphs[letter]
		if !ok {
			continue
		}
		for r := range rows {
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

// paintWordmark paints one row of the wordmark for this frame: muted where the
// sweep has been and where it is going, ink under its head. At rest — and on a
// terminal that has no hues — the whole word is the muted tier, which is the
// state this animation exists to arrive at rather than to decorate.
//
// THE WORDMARK IS THE BOX'S HEADING AND HEADINGS ARE STRUCTURE. The accent on
// this screen belongs to the recent session the cursor is standing on
// ([welcome.recentRow]) — the one thing here anybody is about to act on — and a
// three-row wordmark in the same hue was the louder of the two by area alone.
// [hueMuted] keeps the letterforms a clear rung above the dim place line under
// them while leaving the eye somewhere to land.
func (w *welcome) paintWordmark(row string, pal palette) string {
	head := sweepAt(w.step, ansi.StringWidth(row))
	if w.step >= welcomeSweep {
		return pal.muted(row)
	}
	out := ""
	for i, cell := range []rune(row) {
		text := string(cell)
		if i >= head-welcomeHead && i <= head {
			out += pal.ink(text)
			continue
		}
		out += pal.muted(text)
	}
	return out
}

// welcomeUnitWidth is the widest the unit is drawn. It is exactly the starter
// line's three clauses with a cell to spare, and no wider: a two-hundred-column
// window centres a small object rather than stretching one across it, and the
// message box inside the unit wraps where the eye already is.
const welcomeUnitWidth = 76

// The frame the unit will not draw into. Under these there is no room to be
// greeted in — the unit would take the whole window with it and leave nowhere
// to type — so a small window simply opens on the prompt, which is what it
// would have done anyway.
const (
	welcomeMinRows = 12
	welcomeMinCols = 40
)

// The starter line's three clauses, widest first. Each is true in ANY directory:
// the example asks about the folder rather than "this repo", because a person
// who opened codeaf in ~/notes must not be promised a repository it cannot see.
// The narrow ladder drops from the left — the example first, then the task door
// — and the last thing standing is the slash, which is the one door onto
// everything else.
const (
	starterTryWord   = `try "what is in this folder"`
	starterTaskWord  = "/task <brief> starts work"
	starterSlashWord = "/ shows commands"
)

// starterLine is the dim line under the message box, cut to the clauses that
// fit in room cells, and "" where not even the slash fits.
func starterLine(room int) string {
	for _, line := range []string{
		starterTryWord + legendJoin + starterTaskWord + legendJoin + starterSlashWord,
		starterTaskWord + legendJoin + starterSlashWord,
		starterSlashWord,
	} {
		if ansi.StringWidth(line) <= room {
			return line
		}
	}
	return ""
}

// startPageKeysWord is the start page's line in the greeting's starter slot, and
// it says the two things that are true of THIS unit and not of that one: the way
// back, and what enter does. The greeting's own three clauses are things to try
// in a conversation that has not begun; a person who pressed `+` has a
// conversation behind them and needs to know it is still there.
const startPageKeysWord = "esc keeps the chat you were in · enter starts a new one"

// unitStarterLine is the dim line under the message box, and it is the one row
// of the unit that differs between the greeting and the start page. It cuts to
// what fits exactly as [starterLine] does, and answers "" for a frame that has
// room for neither clause.
func (a *app) unitStarterLine(unit int) string {
	if !a.welcome.start {
		return starterLine(unit)
	}
	for _, line := range []string{startPageKeysWord, startPageEscWord} {
		if ansi.StringWidth(line) <= unit {
			return line
		}
	}
	return ""
}

// startPageEscWord is the narrow frame's half of the line above: the way back is
// the clause that survives, because it is the one a person cannot guess.
const startPageEscWord = "esc keeps the chat you were in"

// welcomeRowKind says what one drawn row of the unit is, for the pointer: most
// rows are statements, the message box's rows are the keyboard's, and a recent
// session's row is a door.
type welcomeRowKind uint8

const (
	welcomeRowPlain welcomeRowKind = iota
	welcomeRowInput
	welcomeRowRecent
	// welcomeRowStarter is one of the first conversation's starting points, and
	// pressing it does what enter on it does: fills the box, sends nothing.
	welcomeRowStarter
)

// welcomeMark is one row's kind and, for a recent session, which one.
type welcomeMark struct {
	kind welcomeRowKind
	slot int
}

// welcomeFits reports whether the unit is on the frame at all: it is open, and
// the window has the room stated above.
func (a *app) welcomeFits() bool {
	// Browsing commands lends the greeting's space to the list and restores
	// the ordinary seam and draft. The start page's ownership stays intact.
	if !a.welcome.open || a.menu.open {
		return false
	}
	// AND NEVER UNDER A QUESTION SOMEBODY OPENED OUT (questionroom.go). The
	// greeting is a unit drawn in the MIDDLE of the frame and the question's page
	// is the body region, so the two would be drawn through each other — which is
	// exactly the reason the start page is answered in [app.bodyRows] rather than
	// in the draw alone. A question raised on the first turn of a session is not
	// rare: it is the ladder working.
	if a.questionRoomOpen() {
		return false
	}
	return a.welcomeRoom()
}

// welcomeRoom is the size half of [app.welcomeFits], asked on its own by the
// door that OPENS the start page: a `+` that drew a page the frame has no room
// for would put a person in front of a hidden unit with the conversation behind
// it already put away (chatstart.go).
func (a *app) welcomeRoom() bool {
	width, _ := a.size()
	return a.welcomeRowsLeft() >= welcomeMinRows && width >= welcomeMinCols
}

// welcomeRowsLeft is the height the greeting may spend: the frame LESS THE HEAD
// over it (head.go).
//
// THE UNIT IS DRAWN UNDER THE HEAD, SO IT IS MEASURED UNDER IT. Its floors and its
// recent list were sized against the whole terminal while the head over it was
// one row or none; the head is the places' four rows now, and a greeting that
// did not know would be three rows taller than the frame on a sixteen-row
// terminal — cut off at the bottom, with the strip above it answering a click
// on a row that was no longer the strip.
func (a *app) welcomeRowsLeft() int {
	_, height := a.size()
	return height - a.topHeight()
}

// welcomeHolds reports whether the message box is drawn INSIDE the unit this
// frame rather than at the foot of the frame.
//
// It is the unit's whole reason and it is still conditional, because the box at
// the foot is not always the draft: a picker's filter, the sessions roster's,
// the rewind bar and the rest all stand in its position while they hold the
// keyboard (input.go's [app.inputBlock]), and `codeaf resume` opens the roster
// over the greeting. A filter box drawn in the middle of the screen beside a
// list at the bottom would be a box a person cannot find the list for — so
// while anything else has the box, the unit draws without it and the foot of
// the frame keeps whatever is standing there.
func (a *app) welcomeHolds() bool {
	if !a.welcomeFits() {
		return false
	}
	if a.boxTaken() {
		return false
	}
	return true
}

// boxTaken reports whether something other than the draft is standing in the
// message box's position.
//
// IT IS ONE READING BECAUSE TWO DOORS ASK IT. The greeting draws without the box
// while anything else holds it ([app.welcomeHolds] above), and the new-chat start
// page REFUSES TO OPEN at all in the same state (chatstart.go): a page whose whole
// content is a composer, drawn while a filter box has the keyboard, would be a
// page a person cannot type into. A list that grew in one place and not the other
// would be exactly that page.
func (a *app) boxTaken() bool {
	if a.rew.on || a.pick.open || a.at(pageMemory) || a.roster.open || a.subPage.open ||
		a.shelf.open || a.connPanel.open {
		return true
	}
	// AND A WATCHER HAS NO BOX AT ALL (watching.go): the line that stands in its
	// place belongs at the foot of the frame with the rest of what has taken that
	// position.
	return a.watching()
}

// welcomeHeight is how many rows the unit takes, which changes with the draft
// inside it and the sessions under it. Nothing here is reserved: the frame
// charges the conversation exactly what is drawn (view.go's [app.chromeHeight]).
func (a *app) welcomeHeight() int {
	return len(a.welcomeRows(a.widthOr()))
}

func (a *app) widthOr() int {
	width, _ := a.size()
	return width
}

// welcomeRows draws the unit. The pointer's half of the same geometry is
// [app.welcomeUnit]'s marks, so a click cannot land on a session the frame drew
// somewhere else.
func (a *app) welcomeRows(width int) []string {
	rows, _, _, _ := a.welcomeUnit(width)
	return rows
}

// welcomeUnit is the unit as rows, one mark per row, and where the caret sits
// in it — a column from the frame's left edge, and a row from the unit's top —
// while the unit holds the message box.
//
// THE ROWS SHARE ONE LEFT EDGE and the edge is centred. Each row is as wide as
// what it says; the block they make is [welcomeUnitWidth] or the window less
// four, whichever is less, and it sits in the middle of the frame. A left edge is
// what makes a stack of different things read as one object, which is the whole
// difference between this and the four separate pieces of furniture it
// replaced.
func (a *app) welcomeUnit(width int) ([]string, []welcomeMark, int, int) {
	if !a.welcomeFits() {
		return nil, nil, 0, 0
	}
	w := &a.welcome
	pal := a.pal
	unit := min(width-4, welcomeUnitWidth)
	lead := (width - unit) / 2
	// One cell further in while the unit is arriving: the whole slide is a
	// single column, which is a movement a person notices without watching.
	if w.step < welcomeSlide {
		lead++
	}
	pad := strings.Repeat(" ", lead)

	rows := make([]string, 0, 16)
	marks := make([]welcomeMark, 0, 16)
	add := func(text string, mark welcomeMark) {
		rows = append(rows, pad+text)
		marks = append(marks, mark)
	}
	// THE FIRST CONVERSATION LEADS WITH THE QUESTION AND NOT WITH THE LOGO.
	//
	// Every later greeting opens on the three-row wordmark and a line naming the
	// model and the crew, and that is right for them: a person returning to a
	// conversation is being told which machine they are back in, and the ordinary
	// screen has no heading competing for the top of the frame.
	//
	// It is wrong here, and it was wrong in a way worth writing down. The first
	// screen after the setup carried FOUR things above `What would you like to
	// work on?` — a wordmark three rows tall, the raw `~deepseek/… · max crew`
	// line, and a blank — and then said the same model and the same crew again in
	// the status row at the foot. The heading a person has never seen before was
	// the fourth thing on the frame, under a model id they had chosen ninety
	// seconds earlier on the screen behind this one. So the wordmark shrinks to a
	// signature and the model line goes: they are both said elsewhere, and this
	// screen has exactly one job.
	if w.first {
		add(pal.dim(product), welcomeMark{})
		add("", welcomeMark{})
		add(pal.bold(pal.ink(fit(welcomeFirstTitle, unit))), welcomeMark{})
		add(pal.muted(fit(welcomeFirstWord, unit)), welcomeMark{})
		if where := a.welcomeWhereLine(); where != "" {
			add(pal.dim(fit(where, unit)), welcomeMark{})
		}
		add("", welcomeMark{})
	} else {
		for _, row := range wordmarkRows(pal.ascii) {
			add(w.paintWordmark(row, pal), welcomeMark{})
		}
		add(pal.dim(fit(a.welcomeModelLine(), unit)), welcomeMark{})
		add("", welcomeMark{})
	}

	caretX, caretRow := 0, 0
	if a.welcomeHolds() {
		// THE DRAFT IS THE REAL DRAFT, laid out by the same function the foot of
		// the frame uses, at the unit's width. It may not take the window: a
		// restored draft six rows tall in a twelve-row window would push the
		// wordmark off the top, so the box gets what is left after the rest of
		// the unit and the status row have taken theirs.
		box := max(1, min(draftRows, a.welcomeRowsLeft()-a.statusHeight(width)-8))
		// AND A SECRET IS MASKED WHEREVER THE BOX IS DRAWN. The greeting lifts
		// the real draft into the middle of the frame, and a question asking for
		// a credential is answered in exactly that box — so the one rule about
		// never drawing a key back has to be read here too (input.go's
		// [app.secretDraftBlock]). It was not, and a key typed on a conversation
		// nobody had spoken in yet went onto the screen in the clear.
		block, x, row := a.secretDraftBlock(unit)
		if block == nil {
			block, x, row = draftBlockWithTags(&a.input, pal, unit, box, "", a.roomLead(unit), a.input.demotedTags, a.draftInk())
		}
		caretX, caretRow = lead+x, len(rows)+row
		for _, line := range block {
			add(line, welcomeMark{kind: welcomeRowInput})
		}
	}
	// A DOOR THAT REFUSED SAYS SO HERE. While the start page is up the transcript
	// is not on the frame (view.go's [app.bodyRows]), so a refusal noted into it
	// would be a sentence written where nobody is looking (chatstart.go).
	if w.msg != "" {
		add(pal.warn(fit(w.msg, unit)), welcomeMark{})
	}
	// THE THREE STARTING POINTS STAND WHERE THE DIM CLAUSE LINE USUALLY IS, and
	// they replace it rather than joining it: `try "what is in this folder"` and a
	// row that says the same thing and can be pressed are one idea drawn twice.
	// A profile with conversations behind it is not having its first one, whatever
	// the marker says, and keeps the ordinary line (see [welcome.first]).
	if w.first {
		add("", welcomeMark{})
		for i, starter := range welcomeStarters {
			add(w.starterRow(i, pal, unit), welcomeMark{kind: welcomeRowStarter, slot: i})
			// ONLY THE SELECTED ROW IS EXPLAINED. Three helper lines under three
			// rows is a paragraph nobody asked for; one under the row somebody has
			// stopped on is an answer to the question they are asking.
			if i != w.starter {
				continue
			}
			for _, line := range wrap(starter.helper, unit-4) {
				add(strings.Repeat(" ", 4)+pal.dim(line), welcomeMark{})
			}
		}
		add(pal.dim(fit(welcomeStarterKeysWord, unit)), welcomeMark{})
	} else if line := a.unitStarterLine(unit); line != "" {
		add(pal.dim(line), welcomeMark{})
	}

	// THE SESSIONS TAKE ONLY THE ROOM THE WINDOW HAS LEFT. A twelve-row window
	// with four sessions to list would draw the last of them over the status row;
	// the list is cut to what fits with a row of slack to spare, and a list that
	// would fit no row at all is not drawn — its heading is a label earned by a
	// real row, never a row spent on absence.
	spare := a.welcomeRowsLeft() - a.statusHeight(width) - len(rows) - 1
	shown := min(len(w.recent), spare-2)
	if shown > 0 {
		add("", welcomeMark{})
		add(pal.dim("recent sessions"), welcomeMark{})
		hover := a.hoveredSlot()
		names := w.recentNameWidth(shown, unit)
		for i := 0; i < shown; i++ {
			add(w.recentRow(i, pal, names, i == hover), welcomeMark{kind: welcomeRowRecent, slot: i})
		}
	}
	return rows, marks, caretX, caretRow
}

// welcomeStarterKeysWord is the line under the three starting points: the two
// keys that work on them, and the fact that typing is always an option. It says
// `fills the box` on purpose — a person choosing off a list in a terminal expects
// enter to RUN the thing, and this one does not.
const welcomeStarterKeysWord = "↑↓ choose · enter fills the box · or just type"

// starterRow is one starting point as a row of the unit, wearing the same
// cursor and ground the recent list's rows wear.
func (w *welcome) starterRow(i int, pal palette, unit int) string {
	word := fit(welcomeStarters[i].word, unit-2)
	if i == w.starter {
		return pal.accent(setupLead) + pal.bold(pal.ink(word))
	}
	return "  " + pal.dim(word)
}

// welcomeWhereLine is the folder this conversation is standing in, drawn on the
// first conversation and nowhere else — the design's own point that the working
// folder belongs beside the first message rather than in a setup field.
//
// IT IS THE REAL PLACE AND ITS REAL SEMANTICS. A session that owns its workspace
// says so in the word the whole surface uses for that ([app.placeWord]), and one
// standing in a directory shows the directory, shortened the way every other path
// on this surface is shortened. It promises nothing about what is in there:
// nothing has been read, and the greeting does not pretend it has.
func (a *app) welcomeWhereLine() string {
	if a.owned {
		// AN OWNED SESSION HAS NO FOLDER OF YOURS TO NAME, and saying `in codeaf`
		// would be this line answering with a product name where a person is
		// looking for a path. [ownedWord] is right for the status line, which has
		// one word to spend; here there is room to say what it means and to name
		// the door onto choosing otherwise.
		return welcomeOwnedWhereWord
	}
	if a.workspace == "" {
		return ""
	}
	if where := shortPath(a.workspace, a.tilde, 0); where != "" {
		return "in " + where
	}
	return ""
}

// welcomeOwnedWhereWord is the folder line for a conversation codeaf opened a
// workspace for, which is what bare `codeaf` outside a project does. It says the
// fact and the door, and it promises nothing about what is in there.
const welcomeOwnedWhereWord = "in a folder " + product + " keeps for this conversation · /workspace picks another"

// recentNameCap is the most cells a session's name may take on its row. Past
// it the name is cut, because the age beside it is the half of the row a person
// scans down, and a column of ages that wanders is a column that cannot be
// scanned.
const recentNameCap = 40

// recentNameWidth is the name column's width for the rows that are shown: the
// longest name among them, cut to [recentNameCap] and to what the unit has left
// beside an age — measured rather than chosen, so a list of short names does
// not hold its ages a hand's width off to the right.
func (w *welcome) recentNameWidth(shown, unit int) int {
	widest := 0
	for i := 0; i < shown; i++ {
		if n := ansi.StringWidth(w.recentName(i)); n > widest {
			widest = n
		}
	}
	return min(min(widest, recentNameCap), unit-10)
}

// recentName is what a session's row calls it.
//
// The name is read back as words when it arrived as ONE TOKEN, and left alone
// otherwise (names.go's [readableName]). That is the whole difference between
// this row and the resume picker's: the picker climbs a ladder and title-cases
// what it finds, because it is a page a person went to on purpose and can
// spend the width; this row's whole voice is this surface's lowercase — the
// heading above these rows is "recent sessions" — so a sentence that already
// reads as one is not touched, and the only name that changes here is the
// machine token nobody could read either way.
//
// The row still OPENS the file it was read from — [app.welcomePress] resumes
// Session.File — so nothing that identifies the session is touched here.
func (w *welcome) recentName(i int) string {
	session := w.recent[i]
	if name := readableName(session.Title); name != "" {
		return name
	}
	return sessionStem(session.File)
}

// welcomeModelLine is the line under the wordmark: what is answering, and which
// crew stands behind it — `anthropic/claude-sonnet-4.5 · balanced crew`.
//
// The model is its whole routing address and not the basename the status row
// keeps, because this line is where a person who is about to spend their own
// money reads what they are paying for, and it has the width. The crew clause is
// absent on a window with no crew of its own ([app.crewReading]'s law — the
// hosted one, and not the ordinary launch this line used to drop it on), and the
// directory that used to stand here is on the legend and the status sheet the
// moment the conversation begins.
func (a *app) welcomeModelLine() string {
	model := a.model
	if model == "" {
		model = "no model"
	}
	// The clause is taken whole from the reading rather than joined here: under
	// `--one-model` the preset word is not what stands behind the model — the
	// flag is — and the reading is the one place that knows which (#444).
	crew := ""
	if reading, ok := a.crewReading(); ok {
		crew = reading.clause
	}
	return dotted(model, crew)
}

// hoveredSlot is the recent session the pointer is over, or -1.
func (a *app) hoveredSlot() int {
	if a.hot.kind == hoverWelcome {
		return a.hot.index
	}
	return -1
}

// recentRow is one session as a row of the unit: its name in a column names
// cells wide, and a coarse age after it.
func (w *welcome) recentRow(i int, pal palette, names int, hovered bool) string {
	line := fit(w.recentName(i), names)
	if gap := names - ansi.StringWidth(line); gap > 0 {
		line += strings.Repeat(" ", gap)
	}
	if when := since(w.recent[i].At); when != "" {
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

// welcomeMarkAt is the mark of one row of the unit, from the SAME layout the
// frame drew, so the pointer and the paint cannot disagree.
func (a *app) welcomeMarkAt(row int) (welcomeMark, bool) {
	_, marks, _, _ := a.welcomeUnit(a.widthOr())
	if row < 0 || row >= len(marks) {
		return welcomeMark{}, false
	}
	return marks[row], true
}

// welcomeSlotAt resolves one row of the unit to the recent session drawn on it,
// or -1.
func (a *app) welcomeSlotAt(row int) int {
	if mark, ok := a.welcomeMarkAt(row); ok && mark.kind == welcomeRowRecent {
		return mark.slot
	}
	return -1
}

// welcomeInputRow reports whether one row of the unit is the message box.
func (a *app) welcomeInputRow(row int) bool {
	mark, ok := a.welcomeMarkAt(row)
	return ok && mark.kind == welcomeRowInput
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

func baseName(path string) string {
	if at := strings.LastIndexByte(path, '/'); at >= 0 {
		return path[at+1:]
	}
	if path == "" {
		return "session"
	}
	return path
}

// since is the relative age beside a recent session. It is coarse on purpose:
// the question a person asks of this list is "which one was I in", and "3d"
// answers it where a timestamp would have to be read.
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
