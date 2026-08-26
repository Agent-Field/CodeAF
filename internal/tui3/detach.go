package tui3

import (
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── DETACH AND ATTACH ───────────────────────────────────────────────────────
//
// THE SURFACE DRAWS ONE CONVERSATION AND POINTS AT A DIFFERENT ONE BY MOVING,
// NOT BY CLOSING. That is the whole of this file, and it is [app.openSession]
// split along the one line that used to make the conversation being left stop
// existing — `a.agent.Close()`.
//
// Detach takes the surface off an agent: it stops every lane this surface holds
// on it, folds the box and the readings that are the PERSON's rather than the
// agent's into a sidecar, and clears the per-conversation fields. The agent
// itself is untouched — its turn goes on streaming into its own journal, its
// tasks go on running, its presence file goes on heartbeating.
//
// Attach points the surface at an agent: it takes up the conversation bundle,
// bumps every lane generation, rebuilds the screen from the agent's own record
// ([app.replay], [app.measureContext]), re-subscribes the standing lanes, joins
// an in-flight turn from its first token ([session.Agent.Attach]) and puts the
// sidecar's readings back.
//
// EVERYTHING NOT IN THE SIDECAR IS REBUILT FROM THE AGENT OR FORGOTTEN. That is
// the bargain that makes this a third of the size of a surface that held N
// conversations: the transcript, the rail, the meters, the pending cards, the
// model and the title are all facts about the agent, and the agent still has
// them.

// aside is a detached conversation's PERSON-SIDE readings: the few things on
// screen that are not facts about the agent and would therefore be lost when
// the surface stops drawing it.
//
// EVERYTHING NOT IN THIS STRUCT IS FORGOTTEN BY A SWITCH AND REBUILT FROM THE
// AGENT ON THE WAY BACK. Adding a field here is adding a thing the surface must
// keep correct while it is not drawing it, which is the expensive kind of
// state; the test is "would a person notice it was gone", not "could we".
type aside struct {
	// draft is the unsent sentence with every parked message folded in after it,
	// and chips are the pictures attached to it.
	//
	// THE PARKS GO INTO THE BOX RATHER THAN BEING DROPPED. [app.dropParked] is
	// what /new and /resume do — they say "2 waiting messages dropped" because
	// the turn those messages were queued behind is about to stop existing. A
	// switch is not a close: the turn is still running and the words are still
	// the person's, so they go back where they can see them (quitarm.go's
	// [app.leavingDraft] assembles exactly this string for the same reason).
	draft string
	chips []chip
	// offset is where they were reading and stick whether they were pinned to
	// the foot of the transcript.
	offset int
	stick  bool
	// askLeft is what was LEFT of the approval countdown, and zero is no
	// question or no clock.
	//
	// IT IS THE REMAINDER AND NOT THE STAMP. Storing [app.askAt] would mean the
	// countdown ran while the person was in another project, which is precisely
	// what the focus gate already refuses to do (consent.go's [app.tickAsk]:
	// ten seconds is "long enough to read a command and a rule", and there is
	// nobody reading a conversation that is not on screen). Attach rebases it so
	// the card comes back with the reading time it had.
	askLeft time.Duration
	// askPaused rides along unchanged, keeping the one exception [app.pauseAsk]
	// makes intact: a question a person has already touched stays paused.
	askPaused bool
	// room is the node whose page was open, and zero is none. THE ID ONLY: the
	// page itself is rebuilt on the way back from the agent's own journal, which
	// is what opening a room from the rail does anyway.
	room uint64
	// since is when this conversation was detached.
	since time.Time
}

// laneStops are the standing subscriptions this surface holds on the agent in
// front, each with the function that LEAVES it (K1's Watch… doors).
//
// THEY EXIST BECAUSE A SUBSCRIBER THAT WALKS AWAY WITHOUT SAYING SO IS A PARKED
// GOROUTINE AND A QUEUE THAT GROWS FOREVER (session's agent.go states this on
// [eventStream.pump]). While nothing ever detached, dropping the channel was
// free — the agent was being closed in the same breath. Under a switch the
// agent goes on living, so every lane has to be given back.
//
// A nil stop is a door that does not offer one: an older seam, or a scripted
// agent in a test that never heard of the Watch… variants. Calling through this
// struct is always safe.
type laneStops struct {
	tasks   func()
	wakes   func()
	designs func()
	runs    func()
}

// leave gives every standing lane back and forgets the stops.
func (l *laneStops) leave() {
	for _, stop := range []func(){l.tasks, l.wakes, l.designs, l.runs} {
		if stop != nil {
			stop()
		}
	}
	*l = laneStops{}
}

// attachable is the agent door onto an in-flight turn: the whole of what the
// turn has said so far, then its live tail, on one channel (session's
// [Agent.Attach]).
//
// It is asserted rather than added to [Agent] for [taskAgent]'s reason: a
// scripted agent in this package's tests has never heard of one, and a surface
// driven by one must stay representable.
type attachable interface {
	Attach() (<-chan session.Event, bool, func())
}

// attachReplayer is the same door taken TOGETHER WITH THE REPLAY, in one atomic
// reading (session's [Agent.AttachReplay]). It exists because the two taken
// separately drew the running turn twice: the journal already holds a turn's
// completed steps mid-turn, and the attach backlog replays those same steps in
// their live form — so a surface that replayed and then attached showed the
// turn's first half in both renderings, stacked. events is nil when no turn is
// in flight, and the entries are then the whole record.
type attachReplayer interface {
	AttachReplay() ([]session.DisplayEntry, <-chan session.Event, func())
}

// convKey is a transcript's IDENTITY, and it is computed here and nowhere else.
//
// THE SYMLINKS ARE RESOLVED BECAUSE `/tmp` AND `/private/tmp` ARE ONE FILE ON
// THIS PLATFORM AND TWO STRINGS. Two spellings of one transcript would be two
// keys in the keeper, which is two agents on one journal — a flock conflict with
// ourselves and a conversation this process could open twice. A link that cannot
// be resolved (a transcript just minted, a filesystem that will not answer)
// falls back to the cleaned path, because a name we cannot canonicalise is still
// a name, and refusing to key it would be worse than keying it twice.
func convKey(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(real)
	}
	return filepath.Clean(path)
}

// front is the conversation on screen, assembled as the bundle a door would
// have handed over.
//
// IT IS READ OFF THE SURFACE RATHER THAN REMEMBERED FROM THE DOOR, and that is
// deliberate: the older seam hands back an agent alone ([Options.Fresh]), and a
// remembered bundle would then be nine zero fields where the surface is holding
// nine live ones. What is true is what the surface has.
func (a *app) front() Conversation {
	return Conversation{
		Agent:            a.agent,
		SessionFile:      a.file,
		Workspace:        a.workspace,
		Place:            a.place,
		Owned:            a.owned,
		ContextWindow:    a.ctxWindow,
		DraftFile:        a.draftFile,
		History:          a.history,
		RecentSessions:   a.recentSessions,
		SaveApproval:     a.saveApproval,
		SaveBashApproval: a.saveBashApproval,
		ApplyApprovals:   a.applyApprovals,
	}
}

// detachConversation takes the surface off the conversation in front and hands
// back the readings that would otherwise be lost.
//
// IT DOES NOT CLOSE THE AGENT AND IT DOES NOT INTERRUPT THE TURN. Those are the
// two lines [app.openSession] used to run that this deliberately does not, and
// they are the whole feature: the conversation being left goes on working.
//
// The caller is what decides where the agent goes — the keeper, or
// [app.closeFront], which closes it for real.
func (a *app) detachConversation() *aside {
	side := &aside{
		// The box and the parked messages, in the order they would have been
		// sent (quitarm.go's [app.leavingDraft] is the same assembly the door
		// out of the program makes, and for the same reason).
		draft:  a.leavingDraft(),
		chips:  a.chips,
		offset: a.offset,
		stick:  a.stick,
		since:  a.now(),
	}
	if left, ok := a.askLeft(); ok {
		side.askLeft, side.askPaused = left, a.askPaused
	}
	if a.room != nil {
		side.room = a.room.id
	}
	// THE DEBOUNCE IS DISARMED HERE AND THE FILE IS THE CALLER'S BUSINESS. A
	// save armed by this conversation must not fire after the switch and write
	// this box under the NEXT conversation's name (draft.go's [draftSaveMsg]
	// carries the file it was armed for, and this is the other half of that
	// guard). What happens to the file itself differs by caller: a conversation
	// going into the keeper keeps its crash insurance, and one being closed has
	// no crash to insure against.
	a.draftPending = false
	// THE OVERLAYS CLOSE BEFORE ANYTHING MOVES. Every one of them is a door onto
	// something process-wide or a mode the person is in the middle of, and
	// neither survives arriving somewhere else.
	a.closeForSwitch()
	// AND EVERY STANDING LANE IS GIVEN BACK. This is the inversion of
	// [app.openSession]'s last line: it re-subscribed because the agent was
	// being replaced, and this unsubscribes because the agent is being left
	// alive with nobody reading it.
	a.stops.leave()
	a.clearConversation()
	return side
}

// clearConversation empties every surface field that belongs to the
// conversation being left.
//
// THE LIST IS [app.openSession]'S PLUS THE NINE ROWS IT USED TO LEAVE OVER.
// /resume never called [app.dropTasks], never reset copy mode, never reset the
// rewind marks, never cleared the per-turn fold state and never bumped the lane
// generations other than [app.gen] — so a resumed session carried the previous
// conversation's rail, its rooms, its pilots, a frozen viewport of a transcript
// that was gone and a cut line through it. That was latent while switching was
// rare. It is not latent here.
func (a *app) clearConversation() {
	a.entries = nil
	a.live, a.sel, a.think = -1, -1, -1
	// AND THE ECHO GOES WITH THE CONVERSATION IT WAS TYPED INTO (echo.go): an
	// index into a transcript that has been replaced points at somebody else's
	// row, and a confirmation arriving after the swap would take the mark off it.
	a.echoAt = -1
	a.asks, a.follows = nil, nil
	// A warm ctrl+c names what a second press would stop IN THIS CONVERSATION,
	// and after this line that is a different one (quitarm.go).
	a.disarmQuit()
	// The offers and the sign-ins belong to the conversation that raised them
	// (connect.go), and a browser standing open on one is a browser nobody is
	// coming back to.
	a.connAsks, a.connPanel = nil, connectPanel{}
	a.harnessAsks, a.harnPanel = nil, harnessPanel{}
	a.harnessStep = ""
	// And the picked harness with them: a chip is a choice about the NEXT
	// message of this conversation (harnesspick.go).
	a.harnPick, a.harnChip = harnessPick{}, ""
	a.abandonConnects()
	a.turn = 0
	// The scrollback's mark and the compacted region both belong to the
	// transcript being put down. [app.replay] rebuilds both on the way back —
	// clearing them here is what makes that the design rather than luck.
	a.replayFrom, a.replayFloor = 0, 0
	a.earlier, a.earlierFloor, a.earlierFrom, a.earlierSeam = nil, 0, 0, false
	a.transcript = nil
	a.historyGen++
	a.historyLoading = false
	// A page or conversation replacing this one also owns the wheel reports that
	// have not reached their frame yet. Letting one land afterwards would move a
	// transcript the gesture was never made over.
	a.unfolded = map[int]bool{}
	// Per-turn fold state carried across would be applied to another
	// conversation's turn NUMBERS, which is the same index meaning something
	// else.
	a.workOpen = map[int]bool{}
	a.dropHover()
	// A frozen viewport and a cut line are modes a person is in the middle of,
	// and there is no honest way to be in the middle of one in a conversation
	// nobody is looking at (copymode.go, rewind.go).
	a.copy = copyMode{mark: -1}
	a.rew = rewindMode{}
	a.rewSay, a.rewSayAt = "", time.Time{}
	// The rail goes with its nodes, its rooms and its pilots (task.go).
	a.dropTasks()
	// Which paths in THIS transcript were linkable. It is cleared at every turn
	// end anyway (app.go's [app.settle]); leaving it would be a memo about a
	// conversation that is no longer on screen.
	a.pathSeen = nil
	a.stream = nil
	// EVERY LANE GENERATION, not only the turn's. An event already in flight on
	// any of them must discard itself rather than land in the conversation that
	// took this one's place — which is the law app.go's generation comment
	// states and which /resume only ever kept for one of the eight.
	a.gen++
	a.taskGen++
	a.designGen++
	a.orchGen++
	a.roomGen++
	a.pilotGen++
	a.wakeGen++
	a.state = stateIdle
	a.resetMeters()
	a.endRecall()
	a.offset, a.stick = 0, true
	// The box goes with the conversation it was typed at: the sidecar is holding
	// it, and the arriving conversation has its own.
	a.input.setText("")
	a.chips = nil
	a.parks = nil
	a.touch()
}

// closeForSwitch puts away every overlay that must not survive a switch.
//
// TWO KINDS OF THING ARE HERE AND THEY FAIL DIFFERENTLY. A picker, a panel and
// a sheet are doors onto something PROCESS-WIDE — the model catalog, the
// profile, the shared memory store, the machine's deliverables index — and a
// person who left /settings open in one project and found it open in another
// would reasonably believe it was that project's settings. Copy mode, the
// rewind sheet and the expand sheet are modes a person is in the MIDDLE of, and
// there is nothing to be in the middle of in a conversation nobody is drawing.
//
// HOME IS NOT CLOSED HERE. It is usually the thing that caused the switch, and
// it closes itself on the keystroke that did (home.go).
func (a *app) closeForSwitch() {
	a.pick.close()
	a.mem.close()
	a.permPanel.close()
	// AND THE STANDING PAGE, which is a door onto what stands over the
	// conversation this window was holding: the shelves are read per
	// conversation, so one left open across a switch would be three headings
	// about somewhere else (place_standing.go). CLOSING IS THE LOOK, so the
	// place is handed the app to stamp it with, exactly as `esc` does.
	a.orders.close(a)
	// AND /subharness, for the reason above and one of its own: a card is an
	// answer half typed, and carrying one across a switch would leave a person
	// about to start work in a conversation they are no longer in
	// (subharness.go).
	a.subPage.close()
	a.roster.close()
	a.shelf.close()
	a.closeLists()
	a.closeExpand()
	if a.at(pageSettings) {
		a.closeSettings()
	}
	if a.deck.open {
		a.closeStatusSheet()
	}
	if a.at(pageTasks) {
		a.closeTaskSheet()
	}
	if a.rewSheet.open {
		// RESTORING the sentence it is holding: the draft it stashed on the way
		// in belongs to the person and not to the page that took it, and the
		// sidecar is about to pick that box up (rewindsheet.go).
		a.closeRewindSheet(true)
	}
	a.closeTaskRecord()
}

// attachConversation points the surface at a conversation and hands back the
// commands it owes itself.
//
// conv is the bundle — the agent and the seams minted around it — and side is
// the sidecar a detach left, or nil for a conversation that was just opened.
func (a *app) attachConversation(conv Conversation, side *aside) tea.Cmd {
	a.takeUp(conv, true)
	agent := a.agent
	a.state = stateIdle
	a.resetMeters()
	if agent != nil {
		a.model = agent.Model()
		a.title = strings.TrimSpace(agent.Title())
		// THE DIAL BELONGS TO THE AGENT, so what was held about the last one is
		// dropped and this one's current model is asked about directly — the
		// third of the three seeded moments (reasoninglevel.go).
		a.forgetLevels()
		a.learnLevel(a.model)
	}
	a.hudStale = true
	// THE SCREEN IS REBUILT FROM THE AGENT'S OWN RECORD. This is the one moment
	// a person can tell that this is not several terminals, and it is paid on
	// the switch rather than on the frame.
	//
	// THE RECORD AND THE IN-FLIGHT TURN ARE TAKEN AS ONE READING when the agent
	// offers it: mid-turn the journal already holds the turn's completed steps,
	// and the attach backlog replays those same steps — two calls made
	// separately drew them both, and a person resuming into a running turn read
	// its first half twice. The atomic door hands back entries that stop where
	// the turn's work begins and a stream that carries the turn whole, so the
	// split cannot race the turn ending between the two.
	var joined tea.Cmd
	if door, ok := agent.(attachReplayer); ok {
		entries, events, stop := door.AttachReplay()
		a.replayList(entries)
		if events != nil {
			joined = a.adoptTurn(events, stop)
		}
	} else {
		a.replay()
		joined = a.joinTurn()
	}
	a.noteStandingHere()
	a.measureContext()
	// The rail is rebuilt from the engine's own record rather than carried: the
	// task lane opens on a replay of the graph's roster (session's
	// [Agent.WatchTaskUpdates]), so watchTasks re-grows the column row by row
	// through the same taskUpdate door every live event uses. loadTasks
	// refreshes only the "@" completion's snapshot (taskmention.go).
	// AND THE WAITING ROOM IS ASKED ABOUT AGAIN, because the conversation being
	// taken up is a DIFFERENT one: the far machine holds each session's
	// outstanding questions with that session, and the list this surface was
	// handed on the first frame belongs to the one it just left (hostlink.go's
	// [app.askHeld]).
	cmds := []tea.Cmd{a.watchTasks(), a.watchWakes(), a.watchDesigns(), a.watchRuns(), a.loadTasks(), a.askHeld()}
	if side != nil {
		cmds = append(cmds, a.restoreAside(side))
	}
	// THE IN-FLIGHT TURN'S STREAM IS PUMPED LAST, so the replay above has
	// already put the person's own message on the screen: the journal holds it
	// from the moment it was submitted, and the events this stream carries
	// belong to that same turn number rather than to a new one.
	if joined != nil {
		cmds = append(cmds, joined)
	}
	a.touch()
	return tea.Batch(cmds...)
}

// joinTurn puts the surface back on a turn that is still running, from its
// first token, and answers nil when nothing is in flight.
//
// A SURFACE THAT SUBSCRIBED WITHOUT THE REPLAY WOULD DRAW HALF A TURN WITH NO
// BEGINNING, which is why this goes through [session.Agent.Attach] and not
// through a plain subscription.
func (a *app) joinTurn() tea.Cmd {
	door, ok := a.agent.(attachable)
	if !ok {
		return nil
	}
	events, running, stop := door.Attach()
	if !running {
		// Nothing to watch. The history is the journal and the replay above has
		// already drawn it, so the surface stays idle rather than waiting on a
		// channel that would never carry anything.
		stop()
		return nil
	}
	return a.adoptTurn(events, stop)
}

// adoptTurn wires an in-flight turn's stream onto the surface — the working
// state, the clock, and the pump — whichever door handed it over.
func (a *app) adoptTurn(events <-chan session.Event, stop func()) tea.Cmd {
	a.stream = events
	a.streamStop = stop
	a.state = stateWorking
	a.lastDelta = time.Now()
	a.startClock()
	return tea.Batch(waitEvent(a.stream, a.gen), a.wake())
}

// restoreAside puts the person's own readings back.
func (a *app) restoreAside(side *aside) tea.Cmd {
	if side.draft != "" {
		a.input.setText(side.draft)
	}
	a.chips = side.chips
	a.offset, a.stick = side.offset, side.stick
	// THE COUNTDOWN IS HANDED BACK RATHER THAN RESTAMPED, and only to a question
	// THE ENGINE STILL HOLDS. It is consumed by [app.startAskClock] when the
	// replayed turn raises the question again — which it does, because the
	// event is in the turn's backlog — and it is dropped if nothing is pending,
	// because a card restored for a question nobody is asking any more is the
	// worst outcome available: a person would answer it.
	if side.askLeft > 0 && a.enginePending() {
		a.askResume, a.askResumePaused = side.askLeft, side.askPaused
	}
	if side.room == 0 {
		return nil
	}
	// A ROOM IS A PLACE RATHER THAN A MODE, which is why it is the one thing on
	// this list that is not a reading. It is cheap to reopen from the node id
	// and expensive to explain the absence of. A node that finished while the
	// conversation was away opens as its finished page, which is what opening it
	// from the rail would do anyway (room.go).
	a.openRoom(side.room, "")
	return a.takeRoomPump()
}

// enginePending reports whether the agent is still holding an approval question.
func (a *app) enginePending() bool {
	door, ok := a.agent.(interface{ PendingConsent() []uint64 })
	if !ok {
		// A door that cannot be asked is not evidence either way, and the
		// alternative — refusing to restore the clock — would silently drop the
		// reading time on every scripted agent in the suite.
		return true
	}
	return len(door.PendingConsent()) > 0
}
