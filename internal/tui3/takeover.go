package tui3

// ── CONTINUING A CONVERSATION IN ANOTHER TERMINAL ───────────────────────────
//
// A conversation lives inside the process of the terminal that opened it, under
// a flock nothing else can take. A second terminal used to meet that lock and be
// told to go away — `open in another window — go there, or start a new
// conversation here` — which is a true sentence and the wrong answer to what a
// person actually wants: they are AT the second terminal, and the first one is
// upstairs, or on a laptop that is shut, or simply behind eleven other windows.
//
// So the conversation MOVES. This file is the surface's whole half of that, and
// it has three sides:
//
//	the asking side   home. One enter on a held row ARMS it and says what the
//	                  next one will do and what it costs; the second writes the
//	                  request ([session.AskTakeover]) and waits on the flock.
//	                  When the lock frees, the row opens by the ordinary door.
//	the holder        the window that has it. It hears [session.EventTakeover]
//	                  on the standing lane — the one subscription that outlives
//	                  every turn — and lets go the way /new lets go: interrupt,
//	                  close, land on whatever else this window was holding.
//	the launch        `aforge chat` in a folder whose conversation is open
//	                  somewhere else comes up on home with that row already
//	                  armed ([Options.TakeOver]), so one enter continues it.
//
// ── THE FOUR THINGS THIS DESIGN PROMISES ───────────────────────────────────
//
//   - A REPLY IS NEVER CUT. The holder answers at the first heartbeat AFTER its
//     running turn ends (internal/session's takeover.go), so the wait here has
//     no deadline of its own: a long reply is minutes, and the person who walked
//     to this terminal was not typing in the other one.
//   - NOTHING IS DECIDED BY ONE KEYSTROKE. The first enter is the question and
//     the second is the answer, because moving a conversation ends the window it
//     was in, and a person pressing enter down a list must not do that by
//     accident. Anything but a second enter disarms.
//   - THE WORK COMES WITH IT. Tasks land `paused — it resumes` when the holder
//     closes, and the window that takes the conversation resumes them from the
//     checkpoint. The unsent sentence comes too, through the draft file.
//   - AND IT IS A LOCAL FACT. Over --host the holder is a window on this laptop
//     and the journal is on the far machine; there is nobody to ask, so home
//     keeps [sessionBusyWord] there and says nothing about moving anything.

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── the words ───────────────────────────────────────────────────────────────

// takeoverArmedWord is the foot line after the FIRST enter, and it is built onto
// what home already says about the row ([app.homeHolding] — `open in another
// window · working`), because the state of the other window is half of what a
// person is deciding with.
//
// IT STATES THE COST BEFORE THE KEY IS PRESSED. Two things are surprising about
// this door and both are in the sentence: it does not happen at once when the
// other window is mid-reply, and the work running over there comes here rather
// than stopping.
func takeoverArmedWord(holding string) string {
	if holding == "" {
		holding = homeHeldWord
	}
	return holding + " — enter again to move it here (it moves when that window's reply ends; its tasks resume here)"
}

// takeoverWaitWord is the line while the request is on disk and the flock is
// still held.
const takeoverWaitWord = "moving it here — waiting for the other window…"

// takeoverStillWord replaces it once the wait has gone on long enough to want
// explaining. The first sentence is not wrong for four seconds and is worrying
// for thirty, so the line grows a reason and the key that ends it.
const takeoverStillWord = "still waiting — the other window finishes its reply first · esc stops waiting"

// takeoverPatience is when the line above takes over. It is not a timeout: this
// wait has no deadline at all, because the thing it is waiting for is somebody
// else's reply finishing and that is allowed to take minutes.
const takeoverPatience = 15 * time.Second

// takeoverBeatEvery is how often the flock is asked. One open-and-flock on a file
// five times a second is nothing beside the fact that a person is sitting
// watching a line that says "waiting", and a slower beat is a door that opens
// noticeably after it could have.
const takeoverBeatEvery = 200 * time.Millisecond

// ── the asking side: home ───────────────────────────────────────────────────

// takeoverWait is the request this window has out, and the row it is for.
//
// THE ROW IS KEPT RATHER THAN LOOKED UP AGAIN, because home rebuilds its list on
// its own clock and the conversation being waited for stops being `open in
// another window` at the exact moment this succeeds — so a second lookup would
// be racing the very change it is waiting for.
type takeoverWait struct {
	// gen makes a stale beat harmless. Every ask bumps it, and a tick carrying
	// an older one is a wait that was cancelled or replaced while it was in the
	// air — the same law every other lane on this surface keeps ([app.gen]).
	gen  int
	dir  string
	file string
	line homeLine
	// since is when the ask went out, and the only thing the line reads it for
	// is whether to grow its explanation ([takeoverPatience]).
	since time.Time
}

// takeoverTickMsg is one look at the flock, on its way back to the loop.
type takeoverTickMsg struct{ gen int }

// waiting reports that this window has a request out.
func (a *app) waitingToTakeOver() bool { return a.takeover.file != "" }

// takeoverLine is what the foot says while the wait is on, and "" when it is
// not. Home clears its own line on every keystroke ([app.homeKey]), which is
// right for a refusal and wrong for a condition that is still true, so this is
// re-said rather than remembered.
func (a *app) takeoverLine() string {
	if !a.waitingToTakeOver() {
		return ""
	}
	if a.now().Sub(a.takeover.since) >= takeoverPatience {
		return takeoverStillWord
	}
	return takeoverWaitWord
}

// homeTakeoverEnter is enter on a row another window is holding, and it is the
// two-step door.
//
// THE FIRST PRESS ARMS AND THE SECOND ASKS. What makes that worth a second
// keystroke rather than a confirmation card is that the sentence it puts on the
// foot is the card: it names the state of the other window, what enter will do,
// and the two things about it that surprise people.
func (a *app) homeTakeoverEnter(line homeLine) tea.Cmd {
	h := &a.home
	// A ROW ALREADY ASKED FOR IS NOT ASKED FOR AGAIN. The request is one file and
	// the answer is somebody else's reply ending, so a second press has nothing
	// to add — it repeats the line, which is what a person leaning on enter is
	// looking for anyway.
	if a.waitingToTakeOver() && a.takeover.file == line.row.Transcript {
		h.say(a.takeoverLine(), "")
		return nil
	}
	if h.armed != line.row.Transcript || line.row.Transcript == "" {
		h.armed = line.row.Transcript
		h.say(takeoverArmedWord(a.homeHolding(line.row)), "")
		return nil
	}
	h.armed = ""
	if line.row.Dir == "" {
		// A conversation with no folder of its own — the flat layout that
		// predates session directories — has nowhere to leave a request, so this
		// is exactly the row the old refusal was written for.
		h.say(sessionBusyWord, "")
		return nil
	}
	// ONE WINDOW WAITS FOR ONE CONVERSATION. Asking for a second while the first
	// is still out would leave a request nobody is listening for on the disk, and
	// the holder answering it would close a window for no reason.
	a.cancelTakeover()
	if err := session.AskTakeover(line.row.Dir); err != nil {
		h.say(err.Error(), "")
		return nil
	}
	a.takeover = takeoverWait{
		gen:   a.takeover.gen + 1,
		dir:   line.row.Dir,
		file:  line.row.Transcript,
		line:  line,
		since: a.now(),
	}
	h.say(takeoverWaitWord, "")
	return a.takeoverBeat()
}

// takeoverBeat schedules the next look at the flock.
func (a *app) takeoverBeat() tea.Cmd {
	gen := a.takeover.gen
	return tea.Tick(takeoverBeatEvery, func(time.Time) tea.Msg { return takeoverTickMsg{gen: gen} })
}

// takeoverTick is one look. The lock is the ONLY thing consulted: the request
// file is taken off disk by the holder before it announces anything, so its
// absence means "seen", not "done", and the door is open exactly when the
// journal can be locked by somebody else.
func (a *app) takeoverTick(msg takeoverTickMsg) tea.Cmd {
	if !a.waitingToTakeOver() || msg.gen != a.takeover.gen {
		return nil
	}
	if session.InUse(a.takeover.file) {
		if a.at(pageHome) {
			a.home.say(a.takeoverLine(), "")
		}
		return a.takeoverBeat()
	}
	line := a.takeover.line
	a.takeover = takeoverWait{gen: a.takeover.gen}
	if !a.at(pageHome) {
		// The person walked away from home while this was in the air. The
		// conversation is free and nothing is holding it; nothing is opened
		// under them, and home's row will simply be openable when they go back.
		return nil
	}
	a.home.say("", "")
	// THE ORDINARY DOOR, and deliberately the SAME one enter on a free row goes
	// through (home.go's [app.homeOpenDoor]). The folder may have gone in the
	// minutes this waited, this window may have filled up with conversations,
	// and the lock may have been taken again by somebody else in the instant
	// since the check above — all three of those are already answered there, in
	// this screen's own words.
	return a.homeOpenDoor(line)
}

// cancelTakeover is esc while waiting: the request comes off the disk so the
// holder never answers it, and the foot line goes.
//
// IT REPORTS WHETHER IT DID ANYTHING, because esc on home already means "clear
// the box" and then "leave", and a key that means three things has to be read
// in order — the innermost thing first.
func (a *app) cancelTakeover() bool {
	if !a.waitingToTakeOver() {
		return false
	}
	session.CancelTakeover(a.takeover.dir)
	a.takeover = takeoverWait{gen: a.takeover.gen + 1}
	a.home.say("", "")
	return true
}

// landTakeover is [Options.TakeOver]: a launch that met a lock lands on home
// with that row pointed and already armed, so one enter continues the
// conversation rather than leaving the person to find the row themselves.
//
// IT RUNS INSIDE [newApp], after [app.landHome], and builds the screen the same
// way that function does when it greets somebody — the same constructor, the
// same four lines — because two spellings of "raise home before bubbletea
// exists" is two screens that drift.
func (a *app) landTakeover(transcript string) {
	if transcript == "" || a.hosted() || !a.canOpen() {
		return
	}
	if a.holding(transcript) {
		// This window turns out to be the one holding it. There is nothing to
		// ask for and nobody to ask.
		return
	}
	if !a.at(pageHome) {
		world, known := a.readWorldKnown()
		a.home = a.newHomeView(world, known)
		a.raisePlace(pageHome)
		a.readStandBands()
		a.home.readGone()
		a.home.build()
		a.dismissWelcome()
	}
	a.home.point(transcript)
	line, ok := a.home.focusedLine()
	if !ok || line.kind != homeSession || line.row.Transcript != transcript {
		// The row is not on the screen — a conversation in a project home does
		// not list, or a transcript that has since gone. Home is still the right
		// place to have landed; there is simply nothing to arm.
		return
	}
	a.home.armed = transcript
	a.home.say(takeoverArmedWord(a.homeHolding(line.row)), "")
}

// ── the holder: the conversation in front ───────────────────────────────────

// takeOver is this window letting go of the conversation on screen, because
// another window asked for it.
//
// IT IS /new's ROAD AND NOT A NEW ONE. Interrupt, close, and land on whatever
// else this window was holding — a kept conversation if there is one, a fresh
// one in the same workspace if there is not. Interrupt-and-close is exactly what
// leaves this session's running work as `paused — it resumes`, which is what the
// window taking the conversation picks up from the checkpoint.
//
// THE DRAFT FILE IS LEFT ON DISK ON PURPOSE, and that is the one line where this
// differs from every other close. [app.closeFront] drops it, because a finished
// sentence orphaned in a directory is somebody else's confusion; here it is the
// opposite — the window about to open this conversation adopts that file
// (draft.go's [adoptDraft]), so the sentence the person was in the middle of
// walks to the other terminal with the conversation.
func (a *app) takeOver() tea.Cmd {
	a.dropParked()
	if a.draftFile != "" {
		writeDraft(a.draftFile, a.leavingDraft())
		// Cleared so the close below leaves the file where it is; the
		// conversation arriving brings its own draft file with it.
		a.draftFile = ""
	}
	cmd, moved := a.closeFront()
	if !moved {
		cmd = a.takeOverFresh()
	}
	a.note(session.TakeoverWord)
	return cmd
}

// takeOverFresh is the other landing: this window was holding nothing else, so
// it comes up on a new conversation in the same workspace.
//
// IT IS [app.renew]'s REPLACING BRANCH and not [app.renew] itself, because that
// door puts the conversation it leaves into the KEEPER — which would keep the
// flock this whole exchange exists to release, and the window waiting for it
// would wait for ever.
func (a *app) takeOverFresh() tea.Cmd {
	if !a.canStart() {
		a.note(newUnavailableWord)
		return nil
	}
	conv, whole, err := a.nextConversation()
	if err != nil {
		a.note("new session failed: " + err.Error())
		return nil
	}
	leaving, side := a.agent, a.detachConversation()
	if leaving != nil {
		leaving.Interrupt()
		if err := leaving.Close(); err != nil {
			a.note("close failed: " + err.Error())
		}
	}
	if !whole {
		conv = Conversation{Agent: conv.Agent, SessionFile: conv.SessionFile,
			Workspace: a.workspace, Place: a.place, Owned: a.owned,
			ContextWindow: a.ctxWindow, History: a.history,
			RecentSessions: a.recentSessions, SaveApproval: a.saveApproval,
			SaveBashApproval: a.saveBashApproval, ApplyApprovals: a.applyApprovals}
	}
	cmd := a.attachConversation(conv, nil)
	a.resumed = false
	// THE SENTENCE IN THE BOX GOES WITH THE PERSON, which is what /new has always
	// promised in those words: they are sitting here, and the words are theirs.
	// The one written to the DRAFT FILE above is a different copy, for the window
	// that is about to open the conversation this one just let go of.
	if side.draft != "" {
		a.input.setText(side.draft)
	}
	a.chips = side.chips
	return cmd
}

// ── the holder: a conversation this window is keeping ───────────────────────

// takenOver is [session.Agent.TakeoverAsked] asked of whatever agent this is,
// and false for one that has never heard of the question — a scripted agent
// cannot be asked for.
//
// IT IS THE SECOND HALF OF A PAIR. The event is the fast road and this is the
// road that still works: a kept conversation's watcher drains its lanes and
// keeps no history, so a surface waking on a stir for another reason asks the
// agent directly rather than relying on having caught the announcement.
func takenOver(agent Agent) bool {
	door, ok := agent.(interface{ TakeoverAsked() bool })
	return ok && door.TakeoverAsked()
}

// takeOverKept lets go of a conversation this window is holding but not drawing.
//
// THE BANNER IS THE WHOLE OF WHAT IS SAID, and it is named with that
// conversation rather than with the one on screen — the person is looking at
// something else, and a note in the transcript in front of them would be a
// sentence about a conversation they cannot see.
func (a *app) takeOverKept(key string, held *kept) tea.Cmd {
	delete(a.behind, key)
	held.watch.stop()
	a.forget(key)
	if held.conv.Agent != nil {
		held.conv.Agent.Interrupt()
		if err := held.conv.Agent.Close(); err != nil {
			a.note("close failed: " + err.Error())
		}
	}
	// The draft file is left for [app.takeOver]'s reason: the window taking this
	// conversation adopts it.
	a.touch()
	return a.notifyBehind(held, session.TakeoverWord)
}
