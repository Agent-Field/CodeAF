package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE NEW-CHAT START PAGE: `+`, AND THE CONVERSATION THAT IS NOT MADE YET ──
//
// The tab strip grew a `+` and the question that comes with one is what it is
// allowed to DO. A browser's plus opens an empty tab and costs nothing; this
// surface's cheapest equivalent — /new — is a synchronous conversation swap that
// asks the door for an agent, puts the one on screen into the keeper, and over a
// shared engine handle ENDS it, because the far side holds one conversation at a
// time (keeper.go's [app.stow] says so in those words). A `+` wired to that would
// be a control that closes somebody's work by being pressed, and pressing it
// twice by accident would close two.
//
// So `+` OPENS A PAGE AND NOTHING ELSE. No agent, no session file, no wire, no
// keeper entry, nothing on the recency stack. What it opens is the greeting's own
// unit — the wordmark, the model line, a real message box, this directory's last
// conversations — standing over the conversation the person was in, which is
// still exactly where they left it one `esc` away. The conversation is made by
// the FIRST MESSAGE, through the same door /new and home already use, and not a
// keystroke earlier.
//
// ── THE THREE THINGS THAT MUST NOT LEAK ─────────────────────────────────────
//
// 1. THE OLD SENTENCE STAYS IN THE OLD CHAT. The page is a blank box, and the
//    half-written message behind it — its caret, its tray, the documents behind
//    its compact tokens — belongs to the conversation it was typed at and comes
//    back untouched on `esc`. This is not written here: the box is a view onto
//    one recipient's composer and the start page is a recipient of its own
//    (recipient.go's [startRecipient]), so the swap in and the swap out are the
//    same two calls every task page already makes.
//
// 2. NOTHING THE OLD CHAT WAS HOLDING RIDES OUT ON THE FIRST MESSAGE. /new's own
//    law is that the draft goes with the PERSON — it copies the leaving box, its
//    chips and its pastes forward into the new conversation (app.go's
//    [app.renew]) — and that law is right for /new and wrong here, because `+`
//    promises the conversation you were in keeps its draft. So the create is
//    asked for and then the start page's OWN composer is laid out over what the
//    renew carried, which is the one line of policy this file adds.
//
// 3. A DOOR THAT REFUSES LOSES NOTHING. The refusal is said on the page rather
//    than noted into a transcript that is not on the frame, the words stay in
//    the box, and the conversation behind is untouched — [app.renew] asks the
//    door before it puts anything down, so a failure costs exactly nothing.
//
// ── WHAT IT IS NOT ──────────────────────────────────────────────────────────
//
// It is not home. Home is a dashboard over the whole machine and takes the frame
// whole — no tab strip, no room header — and raising it takes six disk readings
// and spends the greeting for good (home.go's [app.raiseHome] calls
// [app.dismissWelcome]). It is not a new AGENT either: nothing is instantiated,
// and a person who opens the page three times and escapes three times has made
// nothing and closed nothing.

// An unnamed conversation cannot be kept by the existing session keeper. Refuse
// creation before detaching it when doing so would strand its unsent words.
const startDraftUnownedWord = "finish or clear the draft in the current chat before starting another"

// startKeepsLeaving is `+`'s one amendment to what a create may CLOSE.
//
// A FRESH CONVERSATION HOLDING AN UNSENT SENTENCE IS KEPT. /new's exception ends
// a fresh and empty conversation because nothing is in it and the draft goes with
// the PERSON — same box, same person, one step later. `+` promises the opposite
// in as many words: the conversation you were in keeps its draft. Replacing it
// ends the only owner that sentence has, and then the create is holding somebody's
// paragraph with nowhere to put it but the new conversation's box — a draft in
// front of a model it was never addressed to, which is the one leak this page
// exists to prevent. Kept, the words stay in their own conversation and come back
// by pressing its tab.
//
// IT IS ASKED OF THE START PAGE ONLY. Every other caller of the door is standing
// in the conversation it is about to leave, where carrying the box forward is the
// promise rather than the leak.
func (a *app) startKeepsLeaving() bool {
	if !a.startingChat() || a.mainComposer().empty() {
		return false
	}
	return a.agent != nil && a.convKey(a.file) != ""
}

// startTinyWord is the whole start page on a window too small to draw the unit
// in ([welcomeMinRows], [welcomeMinCols]). The composer falls back to the foot of
// the frame where it always is, the recent list is not drawn, and this row is
// what says which page the box belongs to. A page that drew nothing at all would
// be a blank screen with a working caret in it.
const startTinyWord = "new chat · esc keeps the chat you were in"

// startPage names WHICH PAGE was standing when `+` was pressed, and it exists
// because AN ID CANNOT SAY.
//
// A RUN'S PAGE CARRIES THE NODE ID ZERO ON PURPOSE (roomorch.go's
// [app.openOrchRoom]): it belongs to no row of the tasker's counter, so an `esc`
// that read the id back saw the same zero a window with no page open has, and
// put the person in the conversation with their graph gone.
//
// AND A TASK READ OUT OF ANOTHER CONVERSATION CARRIES AN ID THIS WINDOW ALSO
// USES. Task numbers restart with every conversation (taskowner.go says it of
// every field on [taskGuest]), so id 4 next door is very probably id 4 in here —
// and reopening by number alone drew THE WRONG PIECE OF WORK under the right
// name, through this window's own door, on a page the person had been reading
// somebody else's work on.
//
// So the page is remembered by KIND and reopened through the door that kind has,
// or not at all.
type startPage uint8

const (
	// startPageNone is the conversation itself: nothing to put back.
	startPageNone startPage = iota
	// startPageNode is one of this conversation's own tasks (room.go).
	startPageNode
	// startPageRun is an adaptive run's graph (roomorch.go).
	startPageRun
	// startPageGuest is a task inside another conversation, which this file
	// reacquires through the original owner rather than the local task graph.
	startPageGuest
)

// startBack is everything the surface was holding when `+` was pressed, kept so
// that `esc` is exact rather than approximate.
//
// THE COMPOSER IS NOT IN HERE, and that absence is the design: it is stashed
// under [mainRecipient] by [app.retargetComposer] with its caret, its tray, its
// pastes and its uncertain sends, which is the same road a task page takes and
// the only one that has ever been right about a draft (recipient.go).
type startBack struct {
	// page is which door reopens what was put down, and [startPageNone] is a
	// window that was in the conversation.
	page  startPage
	guest taskOwnerAsk
	// room is the node whose page was open, and title is the name it was drawn
	// under. THE ID AND THE NAME ONLY, for [aside.room]'s reason exactly: the
	// page is rebuilt on the way back through the door a rail click uses, and
	// the name is carried so a node the engine has not published yet comes back
	// under the word it was already wearing rather than under its number.
	room  uint64
	title string
	// run is the adaptive run whose graph was open and card the node inside it
	// whose detail was, which are the same two the rail opens a run's page with
	// (task.go's [app.railPress]).
	run  string
	card string
	// welcome is the greeting's own state, restored whole. A person who pressed
	// `+` on the launch screen gets the launch screen back — with its animation
	// already spent, because [welcome.step] rides along and a greeting that
	// swept its wordmark a second time would be the surface repeating itself.
	welcome welcome
	// offset and stick are where they were reading in the transcript that is
	// about to be hidden, and whether they were pinned to its foot.
	offset int
	stick  bool
}

// startingChat reports whether the new-chat start page is the unit on screen.
// It is the one question the rest of the surface asks about this page.
func (a *app) startingChat() bool { return a.welcome.open && a.welcome.start }

// startSay puts one sentence on the start page. It is the page's own [app.note]:
// while the page is up the transcript is not drawn at all, so a refusal noted
// into it would be said where nobody is looking.
func (a *app) startSay(word string) {
	a.welcome.msg = strings.TrimSpace(word)
	a.touch()
}

// openChatStart is `+`.
//
// IT IS A NAVIGATION ACTION AND IT IS REVERSIBLE. Nothing here asks a door for
// anything, closes anything or writes anything: it stands the conversation's
// unit down, points the box at a composer of its own, and asks the recent list
// off the loop. Pressing it again while it is up reuses the page rather than
// building a second one — a person leaning on a control is not asking for two of
// what it makes.
func (a *app) openChatStart() tea.Cmd {
	if a.startingChat() {
		// The page is already up. It keeps its words and its selection.
		a.touch()
		return nil
	}
	if !a.canStart() {
		// NO SEAM, NO PAGE. A start page that could collect a sentence and then
		// had nowhere to send it would be worse than the refusal: the words would
		// be typed, and the door would be missing at the one moment they matter.
		// The refusal is /new's own, in /new's own words (app.go).
		a.note(newUnavailableWord)
		return nil
	}
	if a.viewHeight() <= 0 {
		// There is no body region at all on this frame — a terminal two rows tall
		// — so there is nowhere to put the page and nothing it could say. A frame
		// that is merely too small for the UNIT is fine: the box falls back to the
		// foot of the frame where it lives on every other screen, and
		// [startTinyWord] says which page it belongs to (view.go's [app.bodyRows]).
		return nil
	}
	if a.boxTaken() {
		// Something else is standing in the message box's position — a picker's
		// filter, the rewind bar, the sessions roster (welcome.go's
		// [app.boxTaken]). A page whose whole content is a composer may not open
		// over a keyboard that is pointed somewhere else.
		return nil
	}
	back := startBack{welcome: a.welcome, offset: a.offset, stick: a.stick}
	if a.roomOpen() {
		// A PAGE IS PUT DOWN AND REMEMBERED BY ITS KIND. The start page needs the
		// body region, and a room drawn under it would be two pages claiming the
		// same rows; closing it through its own door is what stashes that page's
		// steering line under its own recipient, and `esc` opens it again through
		// the door a rail click uses (detach.go's [app.restoreAside] does the
		// same two lines for a conversation coming back).
		//
		// THE GUEST IS ASKED FIRST AND THE RUN SECOND, because both of them look
		// like an ordinary node from the id alone and neither of them is one
		// ([startPage]).
		switch run := a.orchOf(); {
		case a.roomIsGuest():
			back.page = startPageGuest
			back.guest = a.room.guest.ask
			back.guest.trail = append([]string(nil), a.room.guest.trail...)
		case run != nil:
			back.page, back.run, back.card = startPageRun, run.id, firstNonEmpty(run.card, run.goal)
		default:
			back.page, back.room, back.title = startPageNode, a.room.id, a.room.title
		}
		a.closeRoom()
	}
	a.startBack = back
	// AND THE WORDS FROM A PAGE THAT WAS PARKED COME BACK TO IT. A start page put
	// away with `esc` keeps its sentence on the SURFACE rather than in the
	// composer map, because the map goes down with the conversation and these
	// words were never addressed to one ([app.cancelChatStart]).
	if !a.startKept.empty() {
		a.keepComposer(startRecipient, a.startKept)
		a.startKept = composerState{}
	}
	a.retargetComposer(startRecipient)
	// THE UNIT OPENS SETTLED. The sweep across the wordmark is an ARRIVAL, and
	// this unit arrives every time somebody presses `+`; a greeting that animated
	// on every press would be the one piece of motion on this surface a person
	// has to learn to ignore (welcome.go's third rule, read the other way).
	a.welcome = welcome{open: true, start: true, sel: -1, step: welcomeFrames}
	a.stick = true
	a.touch()
	return a.loadStartRecents()
}

// newChatChord is `ctrl+t`, and what it opens is the start page — the SAME door
// the `+` at the end of the tab strip presses ([app.openChatStart]).
//
// IT IS THE BROWSER'S OWN READING OF THE KEY, on a row that is drawn as tabs: a
// new tab, beside the ones already there, with everything they were holding left
// exactly where it was. Nothing is created by the chord — the page is where a
// first message creates a conversation ([app.startChatEnter]) — so a person who
// presses it and changes their mind leaves with `esc` and finds their draft,
// their attachments, their open task page and their reading position untouched
// ([app.cancelChatStart]).
//
// THE CHORD USED TO BE THE ROSTER'S and is not any more: that hand-off is
// [railHoldChord], `alt+t`, one modifier away. Nothing else on this surface
// loses the key — the model picker's `ctrl+t` walks a row's thinking effort and
// is modal above this rung (palette.go), and home's `ctrl+t` starts a
// conversation in the FOLDER under the cursor, which is a place and modal above
// this rung too (home.go).
const newChatChord = "ctrl+t"

// newChatKey is that chord's whole claim on the keyboard, and it reports whether
// it took the key.
//
// IT ANSWERS FROM A HELD ROSTER AS WELL, which is the one state that could have
// been read the other way. The roster is read before this router (app.go's
// [app.route]) and no longer claims ctrl+t, so a person standing on a running
// task presses it and gets a fresh conversation rather than a column folding
// away under them — and the hold is given back on the way, because the page
// about to be drawn has a box in it and a keyboard pointed at a list nobody can
// see is the bug chordfocus.go states.
//
// AND PRESSING IT AGAIN ON THE PAGE KEEPS WHAT IS TYPED. [app.openChatStart] is
// idempotent by design — a person leaning on a control is not asking for two of
// what it makes — so the second press is the page keeping its words, its caret
// and its selection rather than a fresh one thrown over the top of them.
func (a *app) newChatKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if msg.String() != newChatChord {
		return nil, false
	}
	if a.railHold {
		a.railTake(false)
	}
	return a.openChatStart(), true
}

// cancelChatStart is `esc`: the page comes down and the conversation comes back
// exactly as it was — its sentence, its caret, its tray, the documents behind
// its compact tokens, the task page that was open, and where they were reading.
//
// NOTHING IS ENDED AND NOTHING IS STARTED. This is the half of `+`'s promise
// that is easiest to break and cheapest to keep: no agent was made on the way
// in, so there is none to close on the way out.
func (a *app) cancelChatStart() tea.Cmd {
	if !a.startingChat() {
		return nil
	}
	a.retargetComposer(mainRecipient)
	// THE PAGE'S OWN SENTENCE IS PARKED ON THE WINDOW AND NOT IN THE MAP. The
	// composer map goes down with the conversation — into its aside, into its
	// crash record — and a sentence addressed to no conversation may not ride
	// there (draftkeep.go's [app.everyComposer] states the same law from the
	// other end). Kept here it survives a switch, a resume and a close, which is
	// what a person who parked a half-written first message expects of it.
	a.startKept = a.composers[startRecipient]
	delete(a.composers, startRecipient)
	back := a.startBack
	a.startBack = startBack{}
	a.welcome = back.welcome
	a.offset, a.stick = back.offset, back.stick
	a.touch()
	// AND THE PAGE GOES BACK UP THROUGH ITS OWN DOOR. Each of the three is opened
	// exactly as the rail opens it, so a page that came back is the page a click
	// would have made and not an approximation of one ([startPage] says what the
	// id alone got wrong).
	switch back.page {
	case startPageNode:
		a.openRoom(back.room, back.title)
		return a.takeRoomPump()
	case startPageRun:
		a.openOrchRoom(back.run, back.card)
		return a.takeRoomPump()
	case startPageGuest:
		ask := back.guest
		owner := taskOwner{row: session.SessionRow{Transcript: ask.file, ProjectDir: ask.dir, Title: ask.name}}
		if cmd, ok := a.openOwnerRoom(owner, ask.item); ok {
			a.taskOwnerAt.trail = ask.trail
			a.taskOwnerAt.fromStart, a.taskOwnerAt.front = true, a.file
			return cmd
		}
		a.note(startGuestBackWord)
	}
	return nil
}

// A missing guest-view capability must never fall back to this chat's task id.
const startGuestBackWord = "that task view is unavailable here — ctrl+. opens the task list"

// Navigation away parks the draft without reopening a task that is immediately
// being left. Escape and the start tab's own close use the full restoration.
func (a *app) parkChatStart() tea.Cmd {
	if !a.startingChat() {
		return nil
	}
	a.startBack.page = startPageNone
	return a.cancelChatStart()
}

// startChatOpen is a recent session chosen off the start page.
//
// THE PAGE IS CANCELLED FIRST AND THE RESUME HAPPENS SECOND, in that order and
// never merged. Cancelling is what puts the conversation being left back
// together — its draft in its box, its task page open — and the resume then
// leaves THAT, which is what every other door on this surface leaves: a
// conversation whose unsent sentence and whose open page are its own
// (welcome.go's [app.openSession]). A resume run from underneath the start page
// would put down a conversation whose composer was still pointed at a box the
// person had been typing a different message into.
//
// AND WHAT WAS TYPED ON THE PAGE IS NOT SENT. Choosing a row is navigation; the
// sentence stays parked and is in the box the next time `+` is pressed.
func (a *app) startChatOpen(slot int) tea.Cmd {
	if !a.startingChat() || slot < 0 || slot >= len(a.welcome.recent) {
		return nil
	}
	chosen := a.welcome.recent[slot]
	back := a.parkChatStart()
	if chosen.File != "" && a.convKey(chosen.File) == a.convKey(a.file) {
		// The conversation this window is already in — the row is the one it is
		// standing on. It is the picker's own rule, and here it is also what keeps
		// [app.openSession]'s open-before-close safe (welcome.go).
		return back
	}
	return tea.Batch(back, a.resumeSession(chosen))
}

// startChatEnter is the first message, and it is the only thing on this page
// that creates anything.
//
// THE ORDER IS THE WHOLE FUNCTION:
//
//  1. nothing to send, nothing happens — no door is asked for an empty box;
//  2. the page's composer is read off the screen while it is still the screen;
//  3. the door is asked, through [app.renew], which reads the OLD conversation's
//     composer out of the stash for its aside and refuses without putting
//     anything down;
//  4. a refusal leaves the page exactly as it was, with the words in it and the
//     sentence on it;
//  5. and only then the page's composer is laid out in the conversation that now
//     exists, and the ordinary enter road runs in it.
//
// STEP FIVE IS THE ONE PIECE OF POLICY. [app.renew] carries the leaving box
// forward on the law that a draft goes with the PERSON, which is right for /new
// and wrong for `+`: this control promises the conversation you were in keeps
// its draft, so what it carried is overwritten here and the words it carried are
// still on that conversation's own aside, one press of its tab away.
//
// AND THAT PROMISE REACHES BACK INTO THE DOOR ITSELF, which is the second piece.
// A conversation holding an unsent sentence is not one the create may close
// ([app.startKeepsLeaving]) — because the words a `+` promised to leave where
// they were cannot be left anywhere if their owner is ended.
func (a *app) startChatEnter(marked bool) tea.Cmd {
	if !a.startingChat() {
		return nil
	}
	// A FULL TRAY IS A MESSAGE, which is input.go's law about enter: a picture
	// with no words is not an empty message. Anything else is a person pressing
	// enter at a blank page, and a conversation made for that would be a session
	// file nobody typed in.
	if strings.TrimSpace(a.input.String()) == "" && len(a.chips) == 0 {
		return nil
	}
	// Enter on a selected paste edits it; that is not a first message.
	if a.openSelectedPaste() {
		return nil
	}
	if !a.mainComposer().empty() && (a.agent == nil || a.convKey(a.file) == "") {
		a.startSay(startDraftUnownedWord)
		return nil
	}
	page := a.liveComposer()
	cmd, started := a.renewRefusing(a.startSay)
	if !started {
		// The door refused and said so on the page. Nothing was put down —
		// [app.renew] asks before it detaches — so the words are still in the box,
		// the conversation behind is still running, and the person can press enter
		// again or escape back to it.
		return nil
	}
	a.startBack = startBack{}
	a.startKept = composerState{}
	// The surface has had its greeting: this page WAS one, and a conversation
	// opening under it must not raise a second.
	a.welcome = welcome{spent: true}
	a.putComposer(page)
	send := a.enterLine(marked)
	return tea.Batch(cmd, send)
}

// startChatKey is the page's claim on the keyboard, and it is four keys wide.
//
// EVERYTHING ELSE IS TYPING. That is the whole difference between this unit and
// the greeting it is drawn as: the greeting hands every key back and puts itself
// away, because any key there means the person is starting work somewhere else;
// here the page IS where they are starting work, so the box keeps the key and
// the page keeps standing (welcome.go's [app.dismissWelcome] refuses for this
// reason).
func (a *app) startChatKey(name string) (tea.Cmd, bool) {
	if !a.startingChat() {
		return nil, false
	}
	switch name {
	case "esc":
		if a.typedListOpen() {
			// The command list or the file completion is up over the box, and esc
			// belongs to whatever is nearest the person. Closing the page under an
			// open list would take two things away with one key.
			return nil, false
		}
		return a.cancelChatStart(), true
	case "up", "down":
		// The greeting's own walk, on the greeting's own terms: only over an empty
		// box, because a person editing a sentence is moving a caret.
		return a.welcomeKey(name)
	case "enter":
		if a.typedListOpen() {
			return nil, false
		}
		if a.welcome.sel >= 0 && a.welcome.sel < len(a.welcome.recent) && a.input.empty() {
			return a.startChatOpen(a.welcome.sel), true
		}
		return a.startChatEnter(false), true
	}
	return nil, false
}

// startMenuEnter is enter over the COMMAND LIST while the start page is up.
//
// THE LIST IS A SHORTCUT FOR TYPING AND NOT A SECOND DOOR WITH DIFFERENT RULES,
// which is commands.go's own sentence about it ([chooseCommand]) — so the chosen
// row is written into the box and the PAGE'S enter runs it, exactly as it runs a
// command somebody typed out in full. That road makes the conversation and runs
// the command in IT ([app.startChatEnter] hands the line to [app.enterLine]).
//
// [app.runMenu] cannot be used here because it dispatches to [app.slash]
// immediately, and the conversation that dispatch lands in is the one this page
// is drawn over and hiding.
func (a *app) startMenuEnter() tea.Cmd {
	chosen, ok := a.menu.choice()
	if !ok {
		return nil
	}
	word, ran := chooseCommand(&a.input, &a.menu, chosen)
	if !ran {
		// The row was written into a longer sentence, or the command takes an
		// argument and is waiting for it. Either way the box is the answer and
		// the page keeps standing.
		return a.edited()
	}
	// AND THE WORD GOES BACK IN THE BOX. [chooseCommand] empties it ready for a
	// dispatch that is not made here; the line has to be in the composer for the
	// page to send it, because what the page sends is what is in the composer.
	a.input.setText(word)
	return a.startChatEnter(false)
}

// typedListOpen reports whether one of the lists that hang under the draft has
// the keys the page would otherwise take (input.go). They are not modal — the
// person keeps typing — so the page asks rather than assumes.
func (a *app) typedListOpen() bool { return a.menu.open || a.comp.open }

// ── THE RECENT LIST ─────────────────────────────────────────────────────────

// startRecentsMsg is this directory's last conversations, read off the loop.
type startRecentsMsg struct {
	// gen is which opening of the page asked. A list that arrives after the
	// person escaped, or after they opened the page a second time, is dropped —
	// the same discard-by-generation every other lane on this surface uses.
	gen  int
	list []Session
}

// loadStartRecents asks the door for this project's earlier conversations, OFF
// THE EVENT LOOP.
//
// THE GREETING READS THE SAME LIST SYNCHRONOUSLY AND THAT IS NOT A PRECEDENT TO
// FOLLOW HERE. It reads at construction, once, before there is a frame to hold
// up; this reads on a keystroke, in front of somebody, and the seam behind it
// walks a directory of transcripts (tui3.go's [Options.RecentSessions]). A
// keystroke that waited on a disk is a keystroke that stutters on a cold cache
// and on a network mount.
//
// A DOOR THAT IS NOT WIRED ASKS NOTHING AND THE PAGE DRAWS NO LIST. That is the
// emptiness law's plainest case and it is also the honest one: this surface
// cannot tell "no earlier conversations" from "nobody wired the reading", and a
// heading over four blank slots would claim it could.
func (a *app) loadStartRecents() tea.Cmd {
	read := a.recentSessions
	if read == nil {
		return nil
	}
	a.startGen++
	gen := a.startGen
	return func() tea.Msg {
		list := read()
		if len(list) > welcomeSlots {
			list = list[:welcomeSlots]
		}
		return startRecentsMsg{gen: gen, list: list}
	}
}

// takeStartRecents folds the list into the page.
//
// IT TOUCHES THE COMPOSER, THE SELECTION AND THE PAGE'S STATE NOT AT ALL beyond
// the rows themselves. A list arriving a second after the page opened lands while
// somebody is typing their first sentence into it, and a reading that reset the
// box — or the cursor, or the message on the page — would take a keystroke back
// for a reason that has nothing to do with them.
func (a *app) takeStartRecents(msg startRecentsMsg) {
	if !a.startingChat() || msg.gen != a.startGen {
		return
	}
	a.welcome.recent = msg.list
	if a.welcome.sel >= len(msg.list) {
		a.welcome.sel = -1
	}
	a.touch()
}
