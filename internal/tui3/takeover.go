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
// So the conversation MOVES, and there are TWO ROADS to that because there are
// two kinds of holder.
//
// ── THE ENGINE ROAD, WHICH IS THE ORDINARY ONE ──────────────────────────────
//
// A plain `aforge chat` runs its conversation in the workspace's ENGINE
// (internal/enginehost), and the engine is what holds the journal. So the second
// terminal does not have to ask anybody for anything: it opens the conversation
// through the same engine, which hands back the session it is already running —
// mid-turn, in well under a second, with the work still moving. The window it
// left is told by the engine ([session.EventMoved]) and STEPS BACK: it detaches,
// says where the conversation went, and lands on home with that row under the
// cursor, so one enter brings it back. Nothing is interrupted, nothing pauses,
// and no keystroke is spent on a confirmation — the whole of what a move costs
// is one enter to undo it. [app.movedAway] is that half.
//
// ── THE IN-PROCESS ROAD, WHICH IS WHAT IS LEFT ──────────────────────────────
//
// A window with no engine behind it (`--no-host`, `--debug`, a test) holds the
// journal in its own process, and nothing outside that process can join it. That
// is what the rest of this file is: a request on the disk, a holder that lets go,
// and a window waiting on the flock. It ASKS FIRST because it really does END the
// other window, and the work there pauses for a moment and resumes here.
//
// The in-process road has three sides:
//
//	the asking side   home. Enter on a held row raises a confirmation on home's
//	                  own card (homeconfirm.go) — `Move this conversation here?`,
//	                  cursor on `leave it there` — and answering `move it here`
//	                  writes the request ([session.AskTakeover]) and waits on the
//	                  flock. When the lock frees, the row opens by the ordinary
//	                  door.
//	the holder        the window that has it. It hears [session.EventTakeover]
//	                  on the standing lane — the one subscription that outlives
//	                  every turn — and lets go the way /new lets go: interrupt,
//	                  close, land on whatever else this window was holding.
//	the launch        `aforge chat` in a folder whose conversation is open
//	                  somewhere else comes up on home with that row's question
//	                  already asked ([Options.TakeOver]).
//
// ── THE FOUR THINGS THIS DESIGN PROMISES ───────────────────────────────────
//
//   - IT IS ANSWERED AT THE NEXT HEARTBEAT, mid-reply or not. It used to wait
//     for the holder's running turn to end, and that wait was the defect this
//     road was reported for — `coming here · 4m50s` while a long reply finished
//     out of sight. The holder interrupts and closes the way /new does, which
//     leaves its work `paused — it resumes` and its partial reply in the
//     journal, and the window that asked picks both up.
//   - NOTHING IS DECIDED BY ONE KEYSTROKE. Enter raises the question; the answer
//     under the cursor is `leave it there`, so a person walking a list with enter
//     cannot end another window with it. Moving it is `1` or `→` — which move the
//     cursor and answer nothing — and then enter. Anything that moves the cursor
//     off the row takes the question down.
//   - THE WORK COMES WITH IT. Tasks land `paused — it resumes` when the holder
//     closes, and the window that takes the conversation resumes them from the
//     checkpoint. The unsent sentence comes too, through the draft file.
//   - AND IT IS A LOCAL FACT. Over --host the holder is a window on this laptop
//     and the journal is on the far machine; there is nobody to ask, so home
//     keeps [sessionBusyWord] there and says nothing about moving anything.

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── the words ───────────────────────────────────────────────────────────────

// takeoverAskWord is the question itself, and it is a QUESTION rather than an
// instruction: what the first enter raises is a confirmation on home's own card
// (homeconfirm.go), answered the way every confirmation on this surface is.
//
// It used to be a sentence on the foot line — `enter again to move it here (that
// window's reply stops there; its tasks resume here)` — thirty rows from the row
// it was about, and the second enter was the answer. A person leaning on enter
// down a list of conversations ended another window with a key they press to
// make things go away; the card puts the cursor on the answer that loses
// nothing instead, which is stop.go's law and now this door's.
const takeoverAskWord = "Move this conversation here?"

// takeoverMoveWord and takeoverStayWord are the two answers, and
// [takeoverStayWord] is the one marked [session.AnswerOption.Safe] — the cursor
// starts on it, `esc` answers with it, and the offer row calls `esc` by its name.
const (
	takeoverMoveWord = "move it here"
	takeoverStayWord = "leave it there"
)

// takeoverCostWord is what moving it costs, and it is the card's REASON rather
// than a consequence beside an answer.
//
// IT STATES THE COST BEFORE THE KEY IS PRESSED. Two things are surprising about
// this door and both are here: the other window's reply stops where it is, and
// the work running over there comes here rather than stopping.
//
// IT IS THE REASON ROW BECAUSE HOME'S CARD IS FIFTY COLUMNS. A consequence
// stands in a column beside its answer and is cut at the card's right edge —
// which on this card left `its reply stops there; i…` — and the reason has the
// row to itself. The other thing that row could have said is where the
// conversation is, and that is already on the line above it and in the row's own
// margin: the same sentence twice is what this block exists to end.
const takeoverCostWord = "its reply stops there; its tasks come here"

// takeoverQuestionKind is the lane this file's question travels under, and it is
// NOT one of internal/session's for stop.go's reason: nothing in the engine
// raises it, nothing in the engine answers it, and it is answered by the closure
// the card carries ([questionShown.local]).
const takeoverQuestionKind session.QuestionKind = "surface-takeover"

// takeoverShown is that question, and the closure that acts on it.
//
// IT BLOCKS NOTHING and carries no clock: the person raised it with their own
// hand, nothing is waiting behind it, and a countdown on the end of its row
// would be a promise that something is about to happen by itself.
func (a *app) takeoverShown(line homeLine) questionShown {
	return questionShown{
		question: session.Question{
			Kind:    takeoverQuestionKind,
			Ask:     session.AskConfirmation,
			Form:    session.FormCard,
			Asker:   session.Asker{Kind: session.AskerSurface},
			Head:    takeoverAskWord,
			Reason:  takeoverCostWord,
			Subject: session.SubjectRef{Name: strings.TrimSpace(line.row.Transcript)},
			Options: []session.AnswerOption{
				{Key: "1", Label: takeoverMoveWord},
				{Key: "2", Label: takeoverStayWord, Safe: true},
			},
			// MOVING A CONVERSATION ENDS THE WINDOW IT WAS IN, which is the one
			// fact that makes this a confirmation at all: the reply over there
			// stops where it is and cannot be put back.
			Stakes: session.StakesIrreversible,
			Asked:  a.now(),
		},
		pick: takeoverStayAt,
		local: func(answer session.Answer) tea.Cmd {
			if answer.FirstKey() != "1" {
				// `leave it there`, and `esc`, which is the same answer. The row
				// goes back to rest and nothing has happened.
				a.home.armed = ""
				return nil
			}
			return a.askTakeover(line)
		},
	}
}

// takeoverStayAt is which of the two answers is the one that loses nothing, and
// it is the cursor's home.
const takeoverStayAt = 1

// takeoverWaitWord is the foot's ECHO while the claim is out, and it is short
// on purpose.
//
// THE FOOT IS NO LONGER WHERE THIS DOOR LIVES (takeovervoice.go). The row under
// the cursor and the card beside it carry the state, the reason and the clock;
// what is left down here is for the one case those cannot cover — the cursor
// has walked off the claimed row, and the card is about something else. So it
// says the two things that are true wherever the cursor went: it is moving, and
// esc stops it.
const takeoverWaitWord = "moving it here — esc stops waiting"

// takeoverStillWord is that echo with the reason in it, and it is said only
// where there is NO CARD to say it better — below [homeCardMin] the frame is one
// column and this line is the whole of what this window can tell somebody.
//
// IT IS TRIGGERED BY THE FAR WINDOW BEING MID-REPLY AND NOT BY A CLOCK. The old
// line grew this reason after fifteen seconds whether or not it was true, which
// made it a guess that happened to be right most of the time; the presence file
// beside the conversation says which it is (takeovervoice.go's
// [app.takeoverHeldUp]).
const takeoverStillWord = "moving it here — that window is stopping its reply · esc stops waiting"

// takeoverPatience is when a wait stops being ordinary. It is not a timeout:
// this wait has no deadline of its own, because the thing it is waiting for is
// somebody else's reply finishing and that is allowed to take minutes. What it
// changes is what the card says — past it, a move with nothing to wait for owes
// a person the fact that nothing has answered ([takeoverQuietWord]).
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
	// since is when the ask went out. The card reads it for the clock it draws
	// and for whether a quiet wait has gone on long enough to say so.
	since time.Time
	// outcome and about are HOW THE LAST CLAIM ENDED and which conversation it
	// was for, and they outlive the wait itself.
	//
	// A WAIT THAT ENDS IN NOTHING BEING SAID IS THE DEFECT THIS FIELD EXISTS
	// FOR. Two of the three endings put the person in the conversation and say
	// so by being there; the other two — a request that died of old age, and a
	// conversation that came free while somebody was on another page — used to
	// clear this struct and leave the screen exactly as it was before the key
	// was pressed. The card reads these and says what happened
	// (takeovervoice.go).
	outcome takeoverOutcome
	about   string
}

// takeoverOutcome is how a claim ended, for the two endings that are not simply
// "and then the conversation opened".
type takeoverOutcome int

const (
	takeoverEndedNothing takeoverOutcome = iota
	// takeoverEndedUnanswered is a request that reached [session.TakeoverStale]
	// with nobody answering it. Past that age the holder deletes it unread, so
	// waiting on the flock any longer is waiting for something that cannot now
	// happen — which is what this window used to do, silently, for ever.
	takeoverEndedUnanswered
	// takeoverEndedFree is the conversation letting go while this window was
	// not on home. Nothing is opened under somebody who walked away, so the
	// news keeps until they come back.
	takeoverEndedFree
)

// takeoverTickMsg is one look at the flock, on its way back to the loop.
type takeoverTickMsg struct{ gen int }

// waiting reports that this window has a request out.
func (a *app) waitingToTakeOver() bool { return a.takeover.file != "" }

// takeoverLine is the foot's echo while the wait is on, and "" when it is not.
// Home clears its own line on every keystroke ([app.homeKey]), which is right
// for a refusal and wrong for a condition that is still true, so this is re-said
// rather than remembered.
func (a *app) takeoverLine() string {
	if !a.waitingToTakeOver() || a.takeoverCarded(a.takeover.file) {
		return ""
	}
	if a.takeoverHeldUp(a.takeoverSubject()) {
		return takeoverStillWord
	}
	return takeoverWaitWord
}

// takeoverSubject is the claimed row as the LAST reading of the world saw it,
// and the row the ask was made against when the list no longer carries one.
//
// THE FROZEN ROW IS THE WRONG THING TO READ A LIVE STATE OFF. [takeoverWait]
// keeps the line it was asked for on purpose — the conversation stops being
// `open in another window` at the exact moment the wait succeeds, so a second
// lookup would race the change it is waiting for — but what the far window is
// DOING changes underneath that, every three seconds, and a sentence about a
// reply that ended a minute ago is a sentence that is simply wrong.
func (a *app) takeoverSubject() session.SessionRow {
	for _, line := range a.home.lines {
		if line.kind == homeSession && line.row.Transcript == a.takeover.file {
			return line.row
		}
	}
	return a.takeover.line.row
}

// takeoverCarded reports that the card beside the list is already telling this
// conversation's story, which is the whole of when the foot stays quiet.
//
// ONE FACT IS SAID IN ONE PLACE. The card says the state, the reason, the clock
// and the key, three cells from the row it is about; a foot line repeating any
// of that while the card is up is the same sentence twice on one screen, and the
// second copy is the one nobody was looking at. The echo is for the case the
// card genuinely cannot cover — the cursor walked off the claimed row, or the
// frame is too narrow for a card at all ([homeColumns]).
func (a *app) takeoverCarded(file string) bool {
	if file == "" || !a.at(pageHome) || a.home.phone {
		// The phone tier draws one column and no card at all (homephone.go), so
		// there is nothing up there for the foot to be an echo of.
		return false
	}
	if _, card := homeColumns(a.width); card <= 0 {
		return false
	}
	line, ok := a.home.previewLine()
	return ok && line.kind == homeSession && line.row.Transcript == file
}

// takeoverSpins is the row the ONE SPINNER belongs to while a claim is out, and
// "" when there is none.
//
// IT IS KEPT ON THE VIEW RATHER THAN ASKED OF THE APP, because the choice of
// which row moves is settled once with the lines themselves and read thirty
// times a second after that (homespinner.go's [homeView.spinAt]). The three
// places that change the wait call this, and it is the whole of the bookkeeping.
func (a *app) syncHomeClaim() {
	if a.home.claim == a.takeover.file {
		return
	}
	a.home.claim = a.takeover.file
	// AND THE COLUMN IS BUILT AGAIN, because the claim is one of the facts the
	// reading paints a row from ([switcherHere.coming]) and the reading is
	// settled with the lines rather than at the draw. It opens nothing and stats
	// nothing (place_home.go's [homeView.buildSwitch]), which is what makes it
	// safe on a keystroke.
	a.home.build()
}

// homeTakeoverEnter is enter on a row another window is holding, and it RAISES
// THE QUESTION rather than answering it.
//
// NOTHING IS DECIDED BY ONE KEYSTROKE, and this is where that is kept. Moving a
// conversation ends the window it was in, so the press puts a confirmation on
// home's own card (homeconfirm.go) with the cursor on `leave it there`: the
// answer somebody gets by pressing enter again, or esc, is the one that loses
// nothing, and moving it costs a deliberate `1` or `→` first. That is stop.go's
// law, kept whole, on the one door on this screen that ends another window.
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
	if h.armed != line.row.Transcript || line.row.Transcript == "" || h.ask == nil {
		h.armed = line.row.Transcript
		a.raiseHomeAsk(a.takeoverShown(line))
		a.sayHomeAsk()
		return nil
	}
	// The card is already up and this is enter landing on it, which the card's
	// own router answers ([app.homeAskKey]). Getting here at all means the key
	// reached the row before the card, so it is handed on rather than acted on.
	cmd, _ := a.homeAskKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	return cmd
}

// askTakeover is `move it here` taken: the request onto the disk, and the wait
// on the flock.
func (a *app) askTakeover(line homeLine) tea.Cmd {
	h := &a.home
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
	// THE LAST CLAIM'S ENDING IS DROPPED HERE AND NOWHERE ELSE. `that window
	// did not answer` is true until somebody asks again, and asking again is
	// exactly this keystroke.
	a.syncHomeClaim()
	h.say(a.takeoverLine(), "")
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
		// A REQUEST NOBODY CAN ANSWER ANY MORE ENDS THE WAIT. Past
		// [session.TakeoverStale] the holder deletes the request unread, so
		// every beat after that is this window watching a lock that will never
		// free for a reason it asked for. It used to beat for ever and say
		// nothing; now it stops and the card says what is true — the
		// conversation is still in the other window.
		if a.now().Sub(a.takeover.since) >= session.TakeoverStale {
			return a.giveUpTakeover()
		}
		if a.at(pageHome) {
			a.home.say(a.takeoverLine(), "")
		}
		return a.takeoverBeat()
	}
	// THE REQUEST COMES OFF THE DISK ON THE WAY IN, and this is not tidying.
	// The lock frees because the holder let go — and the holder only removes the
	// request when it is the one that answered it ([session.drainTakeover]
	// returns without taking it when the session was already closing, or had
	// been asked once before). So a claim can succeed against a lock that freed
	// for its own reasons with this window's question still lying in the folder
	// it is about to open — and the session opened on it would find that
	// question on its own beat and let go of a conversation nobody asked it to.
	// A window that has what it asked for has no question left to leave behind.
	session.CancelTakeover(a.takeover.dir)
	line, about := a.takeover.line, a.takeover.file
	a.takeover = takeoverWait{gen: a.takeover.gen}
	a.syncHomeClaim()
	if !a.at(pageHome) {
		// The person walked away from home while this was in the air. Nothing
		// is opened under them — but the ending is REMEMBERED rather than
		// dropped, so home says the conversation came free when they come back
		// to it ([takeoverFreeWord]) instead of looking as though the key they
		// pressed did nothing at all.
		a.takeover.outcome, a.takeover.about = takeoverEndedFree, about
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

// giveUpTakeover ends a wait for a request that has aged out, and leaves the
// state on the card rather than reverting in silence.
//
// IT WITHDRAWS THE REQUEST ON THE WAY OUT even though the holder would ignore
// it: a stale file in a session folder is a question nobody asked lying where
// the next window to open that conversation will find it, and the one thing
// this window still knows is that nobody is listening for the answer.
func (a *app) giveUpTakeover() tea.Cmd {
	about := a.takeover.file
	session.CancelTakeover(a.takeover.dir)
	a.takeover = takeoverWait{gen: a.takeover.gen + 1, outcome: takeoverEndedUnanswered, about: about}
	a.syncHomeClaim()
	if a.at(pageHome) && !a.takeoverCarded(about) {
		a.home.say(takeoverUnansweredWord, "")
	}
	return nil
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
	// ESC LEAVES NO STATE BEHIND, and that is the one ending that is right to
	// say nothing about: the person withdrew the question themselves, so the
	// screen going back to how it was IS the answer.
	a.takeover = takeoverWait{gen: a.takeover.gen + 1}
	a.syncHomeClaim()
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
	a.raiseHomeAsk(a.takeoverShown(line))
	a.sayHomeAsk()
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
		// The whole composer, so the window taking this conversation over takes
		// its task pages' unsent lines with it and not only the sentence in the
		// box (draftkeep.go).
		a.writeDraftsNow(a.leavingDraft())
		// Cleared so the close below leaves the files where they are; the
		// conversation arriving brings its own draft file with it.
		a.draftFile = ""
	}
	cmd, moved := a.closeFrontFor(session.StopByTakeover)
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
func (a *app) takeOverFresh() tea.Cmd { return a.freshAfterLeaving(a.endAgent) }

// ── the arriving window: a question nobody answered ─────────────────────────

// turnResumer is the agent door onto A QUESTION THIS CONVERSATION WAS NEVER
// ANSWERED (session's [Agent.ResumeStoppedTurn]). It asks the person's last
// message again — once, through the ordinary turn door — when the journal this
// conversation was opened on ends in their words and then this machine stopping
// the turn that was answering them, which is the shape a takeover mid-thought
// leaves behind (session's resume.go).
//
// It is asserted rather than added to [Agent] for [attachable]'s reason: a
// scripted agent in this package's tests has never heard of one, and a surface
// driven by one must stay representable. A hosted conversation answers no such
// door either, which is honest — over a connection the engine is running the
// turn and nothing stopped it.
type turnResumer interface {
	ResumeStoppedTurn(ctx context.Context) (<-chan session.Event, bool)
}

// resumedTurnMsg is that door's answer on its way back to the loop, and it is
// sent only when a turn really started.
type resumedTurnMsg struct {
	gen int
	ch  <-chan session.Event
}

// resumeStoppedTurn is the one line [app.attachConversation] spends on this: ask
// the conversation being taken up whether it is sitting on a question nobody
// answered, and if it is, let it ask again.
//
// IT GOES THROUGH A COMMAND because the door takes the agent's lock and may
// reach a provider, and the Update loop is not a place to wait. Nothing on the
// screen moves until the answer lands, so a conversation that is NOT owed one —
// which is nearly every conversation ever opened — costs one lock read and draws
// nothing at all.
//
// A TURN ALREADY ON SCREEN IS NEVER ASKED. The atomic replay above this may have
// handed the surface a turn that is still running; the engine would refuse
// anyway (a resume needs an idle session), and asking would spend a lock to be
// told so.
func (a *app) resumeStoppedTurn() tea.Cmd {
	door, ok := a.agent.(turnResumer)
	if !ok || a.stream != nil {
		return nil
	}
	gen, ctx := a.gen, a.ctx
	return func() tea.Msg {
		events, resumed := door.ResumeStoppedTurn(ctx)
		if !resumed {
			return nil
		}
		return resumedTurnMsg{gen: gen, ch: events}
	}
}

// tookResumedTurn is the surface taking up that turn.
//
// THE SENTENCE IS SAID HERE AND THE TURN IS TAKEN THE ORDINARY WAY. What the
// person sees is their own question — already on the page, put there by the
// replay a moment ago — one dim line saying the reply is being asked for again
// ([session.ResumedWord]), and then the answer streaming under it. There is no
// second copy of their words, because nothing wrote one.
//
// A WINDOW THAT MOVED ON IN THE MEANTIME LETS IT GO. The generation is the one
// the ask was made under, and a conversation switched away from between the two
// is a turn this surface is no longer drawing; the engine goes on running it and
// the journal keeps its answer, which is what every other detached turn does.
func (a *app) tookResumedTurn(msg resumedTurnMsg) tea.Cmd {
	if msg.ch == nil || msg.gen != a.gen || a.stream != nil {
		return nil
	}
	a.note(session.ResumedWord)
	return a.takeStream(msg.ch)
}

// movedFresh is the same landing on the ENGINE ROAD, and it differs in the one
// line [app.stepBackFront] differs in: the conversation is detached rather than
// closed, because the engine is still running it for the window that took it.
func (a *app) movedFresh() tea.Cmd { return a.freshAfterLeaving(leaveAgent) }

func (a *app) freshAfterLeaving(let func(Agent)) tea.Cmd {
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
		let(leaving)
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
	// And the documents its compact tokens stand for, for [app.renew]'s reason
	// exactly (recipient.go): the sentence travels whole or it is not the
	// sentence. What was typed at this conversation's task pages does not travel
	// — those pages belong to the conversation this window just let go of.
	a.pastes = append([]pasteChip(nil), side.pastes...)
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
	return a.letGoOfKept(key, held, a.endAgent, session.TakeoverWord)
}

// movedKept is the ENGINE ROAD's version of the same: a conversation this window
// is keeping has been opened in another terminal, so this window detaches from
// it and says where it went. The work is untouched — it never left the engine.
func (a *app) movedKept(key string, held *kept) tea.Cmd {
	return a.letGoOfKept(key, held, leaveAgent, session.MovedWord)
}

func (a *app) letGoOfKept(key string, held *kept, let func(Agent), word string) tea.Cmd {
	delete(a.behind, key)
	held.watch.stop()
	a.forget(key)
	if held.conv.Agent != nil {
		let(held.conv.Agent)
	}
	// The draft file is left for [app.takeOver]'s reason: the window taking this
	// conversation adopts it.
	a.touch()
	return a.notifyBehind(held, word)
}

// ── the engine road: this window stepping back ──────────────────────────────

// movedAway is this window learning that ANOTHER WINDOW HAS OPENED THE
// CONVERSATION IN FRONT OF IT, through the engine that holds them both
// ([session.EventMoved], and internal/remote's tellMoved on the other side).
//
// IT IS [app.takeOver]'s ROAD MINUS THE INTERRUPT, and that subtraction is the
// whole ruling. On this road the conversation does not live in this process: the
// engine is running the turn, holding the tasks and writing the journal, and it
// goes on doing all three while the surfaces around it change. So this window
// DETACHES — it lets go of a view, not of anybody's work — and nothing pauses,
// nothing resumes, and nothing is lost.
//
// THE DRAFT FILE IS LEFT ON DISK for [app.takeOver]'s reason exactly: the window
// that just opened this conversation adopts it (draft.go's [adoptDraft]), so the
// half-typed sentence walks to the other terminal with the conversation.
//
// AND IT LANDS ON HOME WITH THAT ROW UNDER THE CURSOR, which is what makes the
// move symmetrical: the way back is the same single keystroke that brought it
// there, so neither side owes a confirmation for something one enter undoes.
func (a *app) movedAway(word string) tea.Cmd {
	if strings.TrimSpace(word) == "" {
		word = session.MovedWord
	}
	moved := a.file
	a.dropParked()
	if a.draftFile != "" {
		// The whole composer, exactly as the takeover road hands it over.
		a.writeDraftsNow(a.leavingDraft())
		a.draftFile = ""
	}
	cmd, landed := a.stepBackFront()
	if !landed {
		cmd = a.movedFresh()
	}
	a.note(word)
	return tea.Batch(cmd, a.landMoved(moved, word))
}

// landMoved raises home over whatever this window came to rest on, points the
// cursor at the conversation that left, and says on the foot where it went.
//
// A WINDOW ALREADY ON HOME IS LEFT WHERE IT IS, apart from the cursor. Raising
// home again would rebuild the screen under somebody who is reading it — losing
// the fold they opened and the row they were on — to arrive at the screen they
// are already looking at.
//
// THE SENTENCE IS SAID HERE AS WELL AS IN THE CONVERSATION. [app.note] puts it
// in the transcript this window came to rest on, which is the right record and
// the wrong place to READ it: the person is looking at home. One line on the
// foot, beside the row it is about.
//
// AND THE ROW IS REMEMBERED, because it is very often not on the list yet. Home
// reads the disk on its own three-second beat and the conversation only stops
// being this window's the instant this runs, so the cursor is aimed again on the
// first beat that has the row ([app.pointMovedRow]).
func (a *app) landMoved(transcript, word string) tea.Cmd {
	if strings.TrimSpace(transcript) == "" || a.hosted() {
		return nil
	}
	var cmd tea.Cmd
	if !a.at(pageHome) {
		cmd = a.openHome()
	}
	a.movedFrom = transcript
	a.pointMovedRow()
	a.home.say(word, "")
	return cmd
}

// pointMovedRow aims the cursor at the conversation that just left. It runs on
// the event and again on EVERY home beat until the person touches a key.
//
// AIMING ONCE IS NOT ENOUGH AND THAT IS NOT A RACE, it is what the beat is for.
// The row is not on the list at the instant the conversation goes — home reads
// the disk on its own three-second clock — and each of those readings rebuilds
// the list with the cursor following THIS window's own conversation
// ([homeView.build]). So a cursor put on the moved row once is moved off it a
// beat later, which is the state a person actually met: the row they were told
// to press enter on, with the cursor somewhere else.
//
// IT IS THE KEYBOARD THAT ENDS IT ([app.homeKey]), because a person who has
// pressed anything at all has taken the cursor back and a screen that kept
// dragging it away would be the surface arguing with them.
func (a *app) pointMovedRow() {
	if a.movedFrom == "" || !a.at(pageHome) {
		return
	}
	a.home.point(a.movedFrom)
}
