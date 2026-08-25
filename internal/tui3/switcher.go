package tui3

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
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
	a.memPanel.close()
	a.permPanel.close()
	// AND THE STANDING PAGE, which is a door onto what stands over the
	// conversation this window was holding: the shelves are read per
	// conversation, so one left open across a switch would be three headings
	// about somewhere else (standingpage.go).
	a.standPage.close()
	// AND /subharness, for the reason above and one of its own: a card is an
	// answer half typed, and carrying one across a switch would leave a person
	// about to start work in a conversation they are no longer in
	// (subharness.go).
	a.subPage.close()
	a.roster.close()
	a.shelf.close()
	a.closeLists()
	a.closeExpand()
	if a.sheet.open {
		a.closeSettings()
	}
	if a.deck.open {
		a.closeStatusSheet()
	}
	if a.taskSheet.open {
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

const switcherShown = 8

// switcherVerb is one thing the strip can offer for a row: the letter, the word
// it is spelled with, and — for the two verbs that ANSWER a question — the
// option key that answer has to be sent under.
//
// THE ANSWER KEY IS CARRIED AND NEVER DERIVED. A question's options are the ones
// that session offered ("1", "3", sometimes "2"), and a strip that recomputed
// them from the letter it drew would be answering a different question than the
// one on the row (homeband_answer.go's [app.answerKey] holds the same law for
// the digits).
type switcherVerb struct {
	key    rune
	word   string
	answer string
}

// switcherView is HOW this reading is shown, as opposed to what is in it: the
// three things `alt+g`, `alt+q` and the fold line change about one list of
// facts. They travel together because they are one question — what shape is this
// list in — and a reader that took three bools in a row would be a reader whose
// call sites are three unlabelled trues.
type switcherView struct {
	// grouped is `alt+g`: the flat ranked list becomes one block per project.
	grouped bool
	// hideQuiet is `alt+q`: nothing that is neither asking nor moving is drawn,
	// and the fold at the foot says so in one word.
	hideQuiet bool
	// all is the fold standing open — every row drawn, with no cap at all. It is
	// a door and not a setting ([homeQuiet] states the rule this inherits): a
	// line that says rows are being hidden and cannot be asked to stop hiding
	// them is a dead end somebody hits and gives up at.
	all bool
}

type switcherLedgerInput struct {
	learned int
	letGo   int
}

type switcherKind uint8

const (
	switcherConversation switcherKind = iota
	switcherStanding
	switcherLedger
	switcherFold
)

// switcherRow holds every kind of door the router can open. Zero fields are
// deliberately meaningful: a row never fabricates an address it was not given.
type switcherRow struct {
	kind     switcherKind
	session  session.SessionRow
	item     StandingItemView
	place    string
	project  string
	title    string
	note     string
	age      string
	at       time.Time
	needs    bool
	moving   bool
	paused   bool
	here     bool
	fold     bool
	foldWord string
	options  []session.AnswerOption
}

type switcherLine struct {
	row     *switcherRow
	heading string
	section bool
	blank   bool
}

type switcherReading struct {
	lines        []switcherLine
	chatCount    int
	hasAttention bool
	view         switcherView
	now          time.Time
	// hidden is how many rows the fold at the foot is standing for, and zero
	// when there is no fold. It is what the door needs to know whether opening
	// it would show anything.
	hidden int
}

// switcherHere is WHERE THIS WINDOW IS STANDING, and it is two addresses because
// two different questions are asked of it: `session` is the conversation on
// screen — the one row that wears `here` instead of an age — and `project` is
// the bucket it lives in, which is what puts a person's own project first when
// `alt+g` groups the list.
//
// THE CONVERSATION IS THE EXACT ANSWER AND THE PROJECT IS THE BROAD ONE. A
// window standing in a project with no conversation of its own has the second
// and not the first, and a reading that only had the project would have to guess
// which of its rows was `here` (it used to, and it guessed the first open one).
type switcherHere struct {
	session string
	project string
}

// readSwitcher uses the same attention rules as homeattention.go: NeedsPerson
// outranks everything; moving is Tasks.Running or a fresh PresenceWorking
// conversation, and a standing item moves only while view.Running. An item's
// own NeedsPerson likewise outranks its running marker.
func readSwitcher(world session.World, items map[string][]StandingItemView, here switcherHere, seen time.Time, now time.Time, view switcherView, ledger switcherLedgerInput) switcherReading {
	r := switcherReading{view: view, now: now}
	projectByDir := make(map[string]session.Project, len(world.Projects))
	var all []switcherRow
	for _, project := range world.Projects {
		projectByDir[filepath.Clean(project.Dir)] = project
		for _, row := range project.Sessions {
			if row.Archived {
				continue
			}
			r.chatCount++
			needs := row.NeedsPerson()
			moving := !needs && (row.Tasks.Running > 0 || row.Live && row.Presence.State == session.PresenceWorking)
			// EXACTLY THE ONE CONVERSATION THIS WINDOW IS HOLDING. A broader test
			// would put `here` on a row somebody would then press enter on and go
			// nowhere, which is the worst thing a word on a door can do.
			atHere := here.session != "" && ((row.Dir != "" && filepath.Clean(row.Dir) == filepath.Clean(here.session)) ||
				(row.ID != "" && row.ID == here.session))
			options := []session.AnswerOption(nil)
			if row.NeedsPerson() && strings.TrimSpace(row.Presence.Question.Text) != "" {
				options = append(options, row.Presence.Question.Options...)
			}
			all = append(all, switcherRow{
				kind: switcherConversation, session: row, project: project.Name,
				title: homeName(row), note: switcherConversationNote(row, seen), age: sinceAt(row.At, now),
				at: switcherSortAt(row), needs: needs, moving: moving, here: atHere,
				options: options,
			})
		}
		for _, view := range items[project.Dir] {
			if strings.TrimSpace(view.Item.NeedsPerson) == "" && !view.Running {
				continue
			}
			needs := strings.TrimSpace(view.Item.NeedsPerson) != ""
			all = append(all, switcherRow{
				kind: switcherStanding, item: view, project: project.Name,
				title: strings.TrimSpace(view.Item.Words), note: switcherStandingNote(view),
				age: sinceAt(switcherItemAt(view), now), at: switcherItemAt(view),
				needs: needs, moving: !needs && view.Running, paused: view.Item.Status == standing.StatusPaused,
			})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return switcherLess(all[i], all[j]) })
	for _, row := range all {
		if row.needs || row.moving {
			r.hasAttention = true
			break
		}
	}

	r.addLedger(items, world, seen, ledger)
	if view.grouped {
		r.addGrouped(all, here.project, projectByDir)
	} else {
		r.addFlat(all)
	}
	return r
}

// switcherCap is how many rows this reading draws before the rest go behind one
// door. It is [switcherShown] at rest and NO CAP AT ALL once the fold has been
// opened, which is the whole of what opening it means.
func (r switcherReading) cap(n int) int {
	if r.view.all {
		return n
	}
	return min(switcherShown, n)
}

func switcherSortAt(row session.SessionRow) time.Time {
	if row.NeedsPerson() {
		return attentionWaitedSince(row)
	}
	if row.Tasks.Running > 0 || row.Live && row.Presence.State == session.PresenceWorking {
		return attentionMovingSince(row)
	}
	return row.At
}

func switcherItemAt(view StandingItemView) time.Time {
	if strings.TrimSpace(view.Item.NeedsPerson) != "" {
		return view.Item.Updated
	}
	if view.Running {
		return view.Mark.Since
	}
	return view.Item.LastFired
}

func switcherLess(a, b switcherRow) bool {
	ra, rb := switcherRank(a), switcherRank(b)
	if ra != rb {
		return ra > rb
	}
	if ra == 3 { // A longer wait belongs first.
		return attentionOlder(a.at, b.at)
	}
	if ra == 2 { // More live work is the useful tie-break before recency.
		ba, bb := switcherBusy(a), switcherBusy(b)
		if ba != bb {
			return ba > bb
		}
	}
	return a.at.After(b.at)
}

func switcherRank(row switcherRow) int {
	if row.needs {
		return 3
	}
	if row.moving {
		return 2
	}
	return 1
}

func switcherBusy(row switcherRow) int {
	if row.kind == switcherConversation && row.session.Tasks.Running > 0 {
		return row.session.Tasks.Running
	}
	if row.moving {
		return 1
	}
	return 0
}

func switcherConversationNote(row session.SessionRow, seen time.Time) string {
	if row.NeedsPerson() {
		line := switcherFirstLine(row.Presence.Question.Text)
		if line == "" {
			line = switcherFirstLine(row.Reason())
		}
		if line == "" {
			return ""
		}
		if row.Presence.Question.Kind == session.QuestionConsent {
			return "wants to " + strings.TrimSpace(strings.TrimSuffix(line, "?"))
		}
		return "asks: " + line
	}
	if row.Tasks.Running > 0 {
		note := fmt.Sprintf("%d %s running", row.Tasks.Running, switcherPlural(row.Tasks.Running, "task", "tasks"))
		for _, entry := range row.Tasks.Rows {
			if row.Runs(entry) && strings.TrimSpace(entry.Activity) != "" {
				return note + " · " + switcherFirstLine(entry.Activity)
			}
		}
		return note
	}
	files := 0
	saved := false
	for _, entry := range row.Tasks.Rows {
		if !entry.EndedAt.After(seen) {
			continue
		}
		files += entry.FilesChanged
		if session.TaskKindWord(entry.Kind) == "saved shape" {
			saved = true
		}
	}
	if files > 0 {
		return fmt.Sprintf("%d files made", files)
	}
	if saved {
		return "ran a saved shape"
	}
	return ""
}

func switcherStandingNote(view StandingItemView) string {
	if need := switcherFirstLine(view.Item.NeedsPerson); need != "" {
		return "asks: " + need
	}
	if view.Running && strings.TrimSpace(view.Mark.What) != "" {
		return switcherFirstLine(view.Mark.What)
	}
	return ""
}

func switcherFirstLine(s string) string {
	s = strings.TrimSpace(s)
	if at := strings.IndexByte(s, '\n'); at >= 0 {
		s = s[:at]
	}
	return strings.TrimSpace(s)
}

func switcherPlural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func (r *switcherReading) addLedger(items map[string][]StandingItemView, world session.World, seen time.Time, input switcherLedgerInput) {
	var events []switcherRow
	for _, views := range items {
		for _, view := range views {
			if !view.Item.LastFired.After(seen) {
				continue
			}
			line := standing.LastLookLine(view.Item, r.now)
			if line == "" {
				line = switcherFirstLine(view.Item.LastCheckLine)
			}
			if line != "" {
				events = append(events, switcherRow{kind: switcherLedger, item: view, title: line, place: "standing", at: view.Item.LastFired})
			}
		}
	}
	landed := 0
	for _, row := range world.Sessions() {
		for _, entry := range row.Tasks.Rows {
			if entry.EndedAt.After(seen) && entry.Status != string(session.TaskRunning) && entry.Status != string(session.TaskQueued) {
				landed++
			}
		}
	}
	if input.learned > 0 || input.letGo > 0 {
		parts := []string{}
		if input.learned > 0 {
			parts = append(parts, fmt.Sprintf("learned %d %s", input.learned, switcherPlural(input.learned, "thing", "things")))
		}
		if input.letGo > 0 {
			parts = append(parts, fmt.Sprintf("let go of %d", input.letGo))
		}
		events = append(events, switcherRow{kind: switcherLedger, title: strings.Join(parts, ", "), place: "memory", at: r.now})
	}
	if landed > 0 {
		events = append(events, switcherRow{kind: switcherLedger, title: fmt.Sprintf("%d tasks landed", landed), place: "tasks", at: r.now})
	}
	if len(events) == 0 {
		return
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].at.After(events[j].at) })
	age := sinceAt(seen, r.now)
	head := "since you left"
	if age != "" {
		head += " · " + age
	}
	r.addSectionLine(switcherLine{heading: head})
	for i := range events {
		row := events[i]
		r.lines = append(r.lines, switcherLine{row: &row})
	}
}

func (r *switcherReading) addFlat(all []switcherRow) {
	if r.hasAttention {
		r.addSectionLine(switcherLine{section: true})
	}
	r.addRowsAndFold(all)
}

func (r *switcherReading) addGrouped(all []switcherRow, bucket string, projects map[string]session.Project) {
	var active, quiet []switcherRow
	for _, row := range all {
		if row.needs || row.moving {
			active = append(active, row)
		} else {
			quiet = append(quiet, row)
		}
	}
	selected := append([]switcherRow(nil), active...)
	if !r.view.hideQuiet {
		selected = append(selected, quiet...)
	}
	capped := min(switcherShown, len(selected))
	selected = selected[:r.cap(len(selected))]
	byProject := map[string][]switcherRow{}
	for _, row := range selected {
		byProject[row.project] = append(byProject[row.project], row)
	}
	type group struct {
		name string
		at   time.Time
		here bool
	}
	var groups []group
	for name, rows := range byProject {
		g := group{name: name}
		for _, row := range rows {
			if row.at.After(g.at) {
				g.at = row.at
			}
			g.here = g.here || row.here
		}
		for dir, project := range projects {
			if bucket != "" && dir != "" && project.Name == name && filepath.Clean(dir) == filepath.Clean(bucket) {
				g.here = true
			}
		}
		groups = append(groups, g)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].here != groups[j].here {
			return groups[i].here
		}
		return groups[i].at.After(groups[j].at)
	})
	if r.hasAttention {
		r.addSectionLine(switcherLine{section: true})
	}
	for _, group := range groups {
		r.addSectionLine(switcherLine{heading: group.name})
		for _, row := range byProject[group.name] {
			copy := row
			r.lines = append(r.lines, switcherLine{row: &copy})
		}
	}
	hidden := len(active) + len(quiet) - capped
	if hidden > 0 {
		clause := ""
		if capped >= len(active) {
			quietAt := capped - len(active)
			if r.view.hideQuiet {
				clause = "quiet"
			} else if quietAt < len(quiet) && !quiet[quietAt].at.IsZero() {
				clause = "quiet since " + strings.ToLower(quiet[quietAt].at.Format("Jan 2"))
			}
		}
		r.hidden = hidden
		r.addFold(foldLine(hidden, clause))
	}
}

func (r *switcherReading) addRowsAndFold(all []switcherRow) {
	eligible := all
	if r.view.hideQuiet {
		eligible = nil
		for _, row := range all {
			if row.needs || row.moving {
				eligible = append(eligible, row)
			}
		}
	}
	// WHAT THE FOLD STANDS FOR IS COUNTED AT THE CAP AND NEVER AT WHAT IS DRAWN.
	// An opened fold draws every row and still says how many rows it is the door
	// over, because it is the way back — a fold that vanished when it was opened
	// would leave the list with no way to become a summary again.
	capped := min(switcherShown, len(eligible))
	shown := r.cap(len(eligible))
	for _, row := range eligible[:shown] {
		copy := row
		r.lines = append(r.lines, switcherLine{row: &copy})
	}
	if more := len(all) - capped; more > 0 {
		clause := ""
		if capped < len(all) && !all[capped].needs && !all[capped].moving {
			if r.view.hideQuiet {
				clause = "quiet"
			} else if !all[capped].at.IsZero() {
				clause = "quiet since " + strings.ToLower(all[capped].at.Format("Jan 2"))
			}
		}
		r.hidden = more
		r.addFold(foldLine(more, clause))
	}
}

// addFold puts the one door over everything this reading is not drawing, and
// wears the mark that says which way it goes — `▸` while it is hiding rows,
// `▾` once it has been opened, the same two marks every other fold on this
// surface uses.
func (r *switcherReading) addFold(word string) {
	mark := tokens.GlyphCollapsed
	if r.view.all {
		mark = tokens.GlyphExpanded
	}
	row := switcherRow{kind: switcherFold, fold: true, foldWord: mark + strings.TrimPrefix(word, tokens.GlyphCollapsed)}
	r.lines = append(r.lines, switcherLine{row: &row})
}

// addSectionLine keeps headings on the shared one-blank rhythm while leaving
// the first block flush with the top of its reading.
func (r *switcherReading) addSectionLine(line switcherLine) {
	for len(r.lines) > 0 && r.lines[len(r.lines)-1].blank {
		r.lines = r.lines[:len(r.lines)-1]
	}
	if len(r.lines) > 0 {
		r.lines = append(r.lines, switcherLine{blank: true})
	}
	r.lines = append(r.lines, line)
}

func (r switcherReading) rows(width int, pal palette) []string {
	if width < 1 {
		return nil
	}
	out := make([]string, 0, len(r.lines))
	for _, line := range r.lines {
		out = append(out, r.paint(line, width, pal, false, false))
	}
	return out
}

// paint is one line of the reading, with the band on the row the keyboard or the
// pointer is standing on.
//
// THE BAND IS THE WHOLE OF THE SELECTION AND THERE IS NO LEAD. This list has its
// state mark in the first cell of every row (SCREEN 1a), so two more cells spent
// on a `›` would push every name two columns right for a fact the ground already
// carries — which is the one device SCREEN 2a names for the cursor: "the band —
// where the cursor is — selection, and the subject goes bold inside it".
func (r switcherReading) paint(line switcherLine, width int, pal palette, sel, hover bool) string {
	if width < 1 {
		return ""
	}
	switch {
	case line.section:
		left := "what wants you first"
		if r.chatCount > 0 {
			left = fmt.Sprintf("%d chats · %s", r.chatCount, left)
		}
		right := switcherGroupWord
		if ansi.StringWidth(left)+ansi.StringWidth(right)+3 <= width && ansi.StringWidth(left)+ansi.StringWidth(right)+ansi.StringWidth(" · "+switcherQuietWord)+3 <= width {
			right += " · " + switcherQuietWord
		}
		return switcherSides(width, left, right, pal.dim, pal.dim)
	case line.heading != "":
		return pal.dim(fit(line.heading, width))
	case line.row != nil:
		return switcherPaintRow(*line.row, width, pal, r.view.grouped, sel, hover)
	}
	return ""
}

// The two views this list offers and the keys that reach them. They are quoted
// on the section line and in the manual from this one spelling.
const (
	switcherGroupWord = "alt+g group by project"
	switcherQuietWord = "alt+q hide the quiet ones"
)

func switcherPaintRow(row switcherRow, width int, pal palette, grouped, sel, hover bool) string {
	if row.fold {
		return switcherBand(pal.dim(fit(row.foldWord, width)), width, pal, sel, hover)
	}
	if row.kind == switcherLedger {
		// A LEDGER LINE IS A DOOR, so it takes the band like any other stop, and
		// the place it names sits out at the right margin where every row's tail
		// sits.
		return switcherBand(switcherSides(width, row.title, row.place, pal.ink, pal.dim), width, pal, sel, hover)
	}
	glyph, glyphInk := tokens.GlyphQueued, pal.dim
	if row.paused {
		glyph = tokens.GlyphPaused
	}
	if row.moving {
		glyph, glyphInk = tokens.GlyphWorking, pal.accent
	}
	if row.needs {
		glyph, glyphInk = tokens.GlyphNeedsHuman, pal.warn
	}
	age := row.age
	project, note := row.project, row.note
	if grouped {
		project = ""
	}
	if row.here {
		age = homeHereWord
	}
	if width < 80 {
		note = ""
	}
	parts := []string{project, note, age}
	for switcherTailWidth(parts)+ansi.StringWidth(glyph)+2+8 > width {
		if parts[1] != "" {
			parts[1] = ""
			continue
		}
		if parts[0] != "" {
			parts[0] = ""
			continue
		}
		if parts[2] != "" {
			parts[2] = ""
			continue
		}
		break
	}
	tail := switcherTailWidth(parts)
	room := max(0, width-ansi.StringWidth(glyph)-1-tail)
	// THE SUBJECT GOES BOLD INSIDE THE BAND and the tail steps up with it: dim
	// grey on a raised ground is grey on grey, which is the rule every row on
	// this surface is painted under (palette.go's [overlayRowTinted]).
	name, tailInk := pal.ink(fit(row.title, room)), pal.dim
	if sel || hover {
		name, tailInk = pal.bold(name), pal.ink
	}
	line := glyphInk(glyph) + " " + name
	used := ansi.StringWidth(line)
	if pad := width - used - tail; pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	for _, part := range parts {
		if part != "" {
			line += " " + tailInk(part)
		}
	}
	return switcherBand(fit(line, width), width, pal, sel, hover)
}

// switcherBand is the one ground this list paints: the row the keyboard is on,
// and the row the pointer is over, at the same rung — whether a person arrived
// with `↓` or with the mouse, the row they are on is the row they are on
// (palette.go's ladder).
func switcherBand(line string, width int, pal palette, sel, hover bool) string {
	if !sel && !hover {
		return line
	}
	return pal.cursor(line, width)
}

func switcherTailWidth(parts []string) int {
	n := 0
	for _, p := range parts {
		if p != "" {
			n += 1 + ansi.StringWidth(p)
		}
	}
	return n
}

func switcherSides(width int, left, right string, leftInk, rightInk func(string) string) string {
	if right == "" {
		return leftInk(fit(left, width))
	}
	if ansi.StringWidth(right) >= width {
		return rightInk(fit(right, width))
	}
	room := width - ansi.StringWidth(right) - 1
	l, lw := fitWidth(left, room)
	return leftInk(l) + strings.Repeat(" ", max(1, width-lw-ansi.StringWidth(right))) + rightInk(right)
}

func (r switcherReading) at(i int) (switcherRow, bool) {
	if i < 0 || i >= len(r.lines) || r.lines[i].row == nil {
		return switcherRow{}, false
	}
	return *r.lines[i].row, true
}

func (r switcherReading) verbs(i int) []switcherVerb {
	row, ok := r.at(i)
	if !ok {
		return nil
	}
	return switcherVerbsFor(row)
}

// switcherVerbsFor is the same answer taken from a row rather than from its
// position, which is what the surface holding these rows as lines of its own
// column needs (homeswitch.go).
func switcherVerbsFor(row switcherRow) []switcherVerb {
	if row.kind == switcherStanding {
		verbs := switcherQuestionVerbs(row.options)
		if row.paused {
			return append(verbs, switcherVerb{key: 'r', word: "resume it"})
		}
		return append(verbs, switcherVerb{key: 'p', word: "pause it"})
	}
	if row.kind != switcherConversation {
		return nil
	}
	verbs := switcherQuestionVerbs(row.options)
	verbs = append(verbs, switcherVerb{key: 'a', word: "put it away"})
	if strings.TrimSpace(row.session.Workspace) != "" || strings.TrimSpace(row.session.ProjectDir) != "" {
		verbs = append(verbs, switcherVerb{key: 't', word: "new chat here"}, switcherVerb{key: 'o', word: "open folder"}, switcherVerb{key: 'c', word: "copy path"})
	}
	return verbs
}

// switcherQuestionVerbs is 1b's answer-in-place: the question's OWN option
// words, on the two letters a hand already knows, carrying the option key the
// answer has to be sent under.
//
// TWO, AND NEVER THE WHOLE LIST. A strip is one row of the frame and a question
// with five options would push the list down by two more; the digits still
// answer every one of them, on the row, because they are drawn there
// (homeband_answer.go).
func switcherQuestionVerbs(options []session.AnswerOption) []switcherVerb {
	var verbs []switcherVerb
	for i, option := range options {
		if i > 1 {
			break
		}
		key := 'y'
		if i == 1 {
			key = 'n'
		}
		if word := strings.TrimSpace(option.Label); word != "" {
			verbs = append(verbs, switcherVerb{key: key, word: word, answer: option.Key})
		}
	}
	return verbs
}
