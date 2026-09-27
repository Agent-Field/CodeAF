package tui3

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE KEEPER ──────────────────────────────────────────────────────────────
//
// THE ASSUMPTION BEING REMOVED IS THAT A TERMINAL HOLDS ONE LIVE CONVERSATION,
// and this map is the whole of the new state it costs. The surface still draws
// exactly one conversation, still has one of every field and one of every
// overlay; what changed is that the conversations it is NOT drawing are still
// alive rather than closed.
//
// A conversation in here is fully alive. Its turn streams, its tasks run, its
// jobs run, its presence file heartbeats, its flock is held. It is not paused
// and not suspended — the surface simply is not looking at it.
//
// THE PERSON-FACING WORD FOR THIS IS `open`, NEVER `behind`. The status line
// reads `2 open · 1 waiting` and home's rung reads `open`, which is the word
// [session.SessionRow] already carries for the same fact seen from another
// window. `behind` describes a position on a screen nobody can see, which makes
// it furniture; `open` describes what is true.

// NO DOOR ONTO A CONVERSATION IS EVER REFUSED FOR HOW MANY ARE ALREADY OPEN.
// There was a cap — eight — and its own comment said that a cap hit in practice
// by somebody who was not testing it is evidence the number is wrong. It was hit
// in a day of ordinary use, and the owner's ruling on 2026-08-31 was to remove
// the limit rather than to raise it: "that is pointless". That ruling stands, and
// nothing below is a limit: the fiftieth `/new` opens exactly like the first.
//
// WHAT ONE OPEN CONVERSATION COSTS is what it always was — a few goroutines, its
// transcript, one file descriptor, a five-second presence tick and its
// transcript's lock. What used to be true of that cost is that NOTHING GAVE IT
// BACK except a person's own `/quit` or `ctrl+w`, so a window left running for a
// day grew with every conversation somebody had opened in it and never shrank.
//
// SO THE KEEPER SWEEPS ITS COLDEST QUIET CONVERSATION INSTEAD OF REFUSING A NEW
// ONE ([app.sweepKept]). Past [keptCeiling] open, the conversation that has been
// left the longest is let go of — but ONLY if letting go of it costs the person
// nothing: nothing turning in it, nothing waiting on them, no news they have not
// seen, no words of theirs still in its box ([app.keptQuiet] is the whole list).
// Where no held conversation answers that, the window goes on holding more than
// the ceiling. A REFUSAL AND A CLOSE ARE BOTH WORSE THAN A NUMBER BEING EXCEEDED,
// which is the trade this design makes and the reason the ceiling is soft.
//
// AND LETTING GO IS NOT ENDING SOMEBODY'S WORK. It goes through [leaveAgent], the
// same judgement `quit` makes: a hosted conversation is detached and goes on
// running on its engine, and an in-process one — which nothing else could run —
// is closed. Either way its transcript is on disk and every door reopens it.
// The keeper forgets the conversation on the frame; the agent is taken off
// afterwards ([leaveOffFrame]), so Interrupt and Close cannot stall a keystroke.

// WorkspaceGoneWord is what any door says about a workspace that is not there.
// It names the path the caller gave and nothing beyond it, because the caller is
// home and home already prints that path on the row the person pressed.
//
// IT IS EXPORTED SO THERE IS ONE OF IT. The surface says it on the keystroke —
// one os.Stat, before an agent is built — and cmd/codeaf says it again when a
// directory disappears between that stat and the open. Two spellings of one
// refusal would drift, and this is the sentence the manual quotes.
const WorkspaceGoneWord = "that folder is gone"

// kept is one conversation this process holds that is not on screen: the bundle
// the door built around its agent, the readings the person left in it, and the
// small goroutine that keeps it from starving.
type kept struct {
	// conv is everything the door resolved around this agent — the workspace,
	// the draft file, the recall list, the three approval closures. It goes back
	// through [app.takeUp] on the way in, which is what stops a keystroke in one
	// conversation reaching a closure minted around another.
	conv Conversation
	// side is what the person left: the box, the messages waiting behind its
	// running turn, their pictures and documents, where they were reading, what
	// was left of an approval countdown, and the room they had open
	// (switcher.go's [aside]).
	side *aside
	// watch drains this agent's lanes and turns the two interesting edges into
	// one contentless stir. See [behindWatch].
	watch *behindWatch
}

// behindStirMsg is a conversation this process holds asking to be looked at
// again. IT CARRIES NO CONTENT — only which conversation — because the surface
// holds that agent's pointer and can simply read it.
//
// THAT EMPTINESS IS WHAT KEEPS THE GENERATION LAW TRUE. Every other message on
// this surface belongs to a lane of the conversation in front, and a stale one
// discards itself by generation. A stir belongs to no lane and to no turn: it
// names a key, the surface looks it up, and a stir for a conversation that has
// since been closed finds nothing in the map and does nothing at all — the same
// shape a stale generation has, needing no new rule.
type behindStirMsg struct {
	key string
	// quiet is a stir that is ONLY a redraw: the conversation said something
	// that changes what it is CALLED and nothing about what it is doing. It is a
	// separate bit rather than a second lane because the banner rules
	// ([app.behindStir]) are the thing it has to skip, and skipping them is one
	// branch there.
	//
	// WITHOUT IT A LATE NAME RAISES AN ATTENTION BANNER. behindStir re-reads the
	// agent and announces `waiting on you` whenever the conversation needs the
	// person — which is right for a stir that means "something happened" and
	// wrong for one that means "it is called this now": a held conversation
	// sitting on a question would raise that banner again every time it named
	// itself, and consume the landed flag that the finished banner is counted
	// from.
	quiet bool
}

// behindParkedMsg is the answer to sending one held conversation's oldest
// waiting message off the update loop. The key is the generation law in this
// lane: if the conversation moved, closed or came forward while the call was
// crossing, the fold finds that fact by identity rather than touching whatever
// conversation happens to be in front.
type behindParkedMsg struct {
	key   string
	park  parked
	shown string
	ch    <-chan session.Event
	err   error
}

// behindTurn is one stream handed to a watcher after that watcher was already
// running. A session follow-up and a parked message both begin between the
// original turn's close and the next pass through the watcher's select, so the
// watcher needs a lane rather than a second goroutine reading beside it.
type behindTurn struct {
	events <-chan session.Event
	stop   func()
}

// behindWatch is one conversation's stir watcher, and it exists for a reason
// that must not be deleted by a future lane trying to save memory.
//
// THREE THINGS IT DOES, IN ORDER OF WHY IT IS HERE:
//
//  1. IT DRAINS. A detached conversation has no reader, and an eventStream is an
//     unbounded producer queue in front of an unbuffered channel: the producer
//     never blocks and never drops, so nothing stalls — but the pump parks
//     forever on its last send and the queue grows for the rest of the turn
//     (session's agent.go says both, on [eventStream.pump] and on the queue).
//     So every lane this agent offers is subscribed here and every event is
//     thrown away.
//  2. IT COUNTS TWO EDGES: a turn finishing, and the agent starting or stopping
//     needing a person ([session.Agent.NeedsPerson]).
//  3. IT STIRS, at most one outstanding at a time, so the surface can refresh
//     the count on the status line and raise the desktop banner. The surface has
//     no idle ticker and is not getting one (render.go), so without this a
//     conversation could finish its work in silence.
//
// The alternative — keep every lane subscribed to the program loop and discard
// the messages on arrival — was rejected: it wakes the frame for events nobody
// is watching, which on a window full of them is the exact cost this design
// exists to avoid, and it needs the discard to be correct, which is a
// conversation id on every message type.
type behindWatch struct {
	key    string
	agent  Agent
	out    chan<- behindStirMsg
	quit   chan struct{}
	adopts chan behindTurn
	once   sync.Once
	// armed is the "at most one outstanding" rule. A watcher that sent a stir
	// nobody has folded in yet sends no more of them: the surface reads the
	// agent when it wakes, so a second nudge to read the same pointer buys
	// nothing and costs a frame.
	armed atomic.Bool
	// landed says a turn ended in here since the surface last looked. It is the
	// one fact a re-read of the agent cannot recover — a finished turn leaves no
	// trace on the agent that says "and it finished just now" — so it is carried
	// on the watcher and taken by the surface rather than put on the message.
	landed atomic.Bool
	// finished counts turns that have ended in here since the person left, and
	// it is a COUNTER BESIDE [behindWatch.landed] rather than a second reader of
	// it: landed is consumed on every stir ([behindWatch.took]), so by the time
	// the switcher asks, the fact that a turn landed has usually already been
	// spent on a banner. This is the same edge, kept for the card to read.
	//
	// NOTHING RESETS IT, because nothing has to: the watcher dies when the
	// conversation comes forward ([app.bringForward] stops it), so the count is
	// always "since you left", by construction rather than by bookkeeping.
	finished atomic.Int64
	// takeover says another window has asked for this conversation
	// ([session.EventTakeover], takeover.go). It is the ONE thing this watcher
	// reads the content of a lane for: every other event here is a nudge, and
	// this one is a conversation that is about to end.
	takeover atomic.Bool
	// moved says another window has OPENED this conversation through the engine
	// that holds it ([session.EventMoved], takeover.go).
	//
	// IT IS A SECOND FLAG AND NOT THE ONE ABOVE, because the two endings are not
	// the same ending: a takeover ends the conversation in this process and a
	// move detaches from one that goes on running elsewhere. A single flag would
	// make the surface guess which, and the guess it would make is the one that
	// stops somebody's work.
	moved atomic.Bool
	// waits and turning are WHAT THIS CONVERSATION IS DOING, cached here so that
	// a surface drawing a mark on its tab does not have to ask the agent
	// (tabsignal.go).
	//
	// THEY COST NOTHING BECAUSE THE LOOP ALREADY COMPUTES THEM. `waits` is set at
	// the edge [behindWatch.run] already finds — it compares [needsPerson]
	// against the last answer on every pass to decide whether to stir — and
	// `turning` is set at the three places a turn's stream is taken up or given
	// back. Neither adds a call, a lock or an allocation.
	//
	// AND THAT IS THE WHOLE POINT OF THEM BEING HERE. The alternative is asking
	// each agent on each frame: [session.Agent.NeedsPerson] takes the agent's
	// mutex and allocates a map (session's taskpresence.go), and the running
	// count comes from [session.Agent.TaskIndex], which READS A FILE. The tab
	// strip is laid out on every frame and states its own law in as many words —
	// it opens no file and crosses no wire (chattabs.go) — so a status read from
	// either of those would be a world scan thirty times a second, or a call to
	// another machine over `--host`.
	//
	// AN UNSET PAIR IS "NOTHING KNOWN", which the strip draws as nothing at all.
	// A conversation with no watcher — one over a shared handle, one this window
	// only remembers — never claims to be running, which is the tab strip's own
	// law about what a tab is allowed to claim.
	waits   atomic.Bool
	turning atomic.Bool
	// tasking is WORK THIS CONVERSATION STARTED THAT OUTLIVES THE TURN THAT
	// STARTED IT. A task node runs in its own worktree under its own agent: the
	// turn that proposed it ends, [behindWatch.turning] goes false, and the node
	// keeps working for minutes afterwards. A strip that read `turning` alone
	// drew that conversation at rest while it was the busiest one in the window,
	// which is the defect this field closes (tabsignal.go).
	//
	// IT IS FOLDED FROM THE LANE THIS LOOP ALREADY DRAINS. Every node's state
	// arrives here as an [session.EventTaskUpdate], and the lane replays the
	// whole roster to a watcher the moment it subscribes (session's
	// WatchTaskUpdates), so a conversation left with work already running is
	// known without asking anything: no [session.Agent.TaskIndex], no file, no
	// call to another machine over `--host`.
	tasking atomic.Bool
	// live is the set of nodes last heard claiming to be running or queued, and
	// it is the bookkeeping behind the atomic above — a count would be wrong,
	// because a node publishes `running` many times and settles once.
	//
	// The watcher writes it through noteTask; workMu also protects the surface’s
	// cancellation snapshot. Painting reads only the atomic working flag.
	live map[uint64]struct{}
	// The surface snapshots only this conversation’s replayed cancellation IDs.
	workMu  sync.Mutex
	jobs    map[int]struct{}
	jobbing atomic.Bool
}

// noteTask folds one task notice into [behindWatch.tasking], and reports whether
// the answer CHANGED — which is the only moment worth a stir, the same shape the
// needs-a-person edge at the bottom of [behindWatch.run] already uses.
//
// THE TWO LIVE STATES AND THE THREE SETTLED ONES ARE NAMED EXPLICITLY, and a
// state that is neither leaves the reading alone. A proposal arrives on this
// lane before its node exists and carries no state at all; a word this surface
// has not heard of is news it cannot interpret. Neither is evidence that work
// stopped, and treating "not a state I know" as "settled" is how a strip goes
// dark on a conversation that is still working.
//
// A BACKGROUND JOB IS NOT A TASK, and is left out here for the reason the
// surface leaves it out of its own roster (task.go's [app.taskUpdate]): jobs
// arrive on their own notice and are counted in their own place.
func (w *behindWatch) noteTask(notice *session.TaskNotice) bool {
	if notice == nil || notice.Kind == session.TaskKindJob {
		return false
	}
	w.workMu.Lock()
	defer w.workMu.Unlock()
	switch notice.State {
	case session.TaskRunning, session.TaskQueued:
		if w.live == nil {
			w.live = map[uint64]struct{}{}
		}
		w.live[notice.ID] = struct{}{}
	case session.TaskDone, session.TaskFailed, session.TaskUnverified:
		delete(w.live, notice.ID)
	default:
		return false
	}
	return w.tasking.Swap(len(w.live) > 0) != (len(w.live) > 0)
}

// stir asks the surface to look, unless it has already been asked.
func (w *behindWatch) stir() {
	if !w.armed.CompareAndSwap(false, true) {
		return
	}
	select {
	case w.out <- behindStirMsg{key: w.key}:
	default:
		// The stir lane is full, which means the surface is already owed more
		// wakeups than it has folded in. Dropping this one is right: what it
		// would have said is "read the agent", and the wakeups already queued
		// will say it.
		w.armed.Store(false)
	}
}

// stirName asks the surface to redraw a conversation that has just been NAMED,
// and asks for nothing else.
//
// IT DOES NOT TOUCH THE ARM. The arm is [behindWatch.stir]'s dedup — one
// outstanding "read the agent" per conversation — and a name is not that
// question: taking the arm here would swallow a real stir queued behind it, and
// clearing it would let two through. A session names itself once
// (session's [Agent.titleTried]), so there is nothing here to dedupe.
func (w *behindWatch) stirName() {
	select {
	case w.out <- behindStirMsg{key: w.key, quiet: true}:
	default:
		// The lane is full, and a redraw is the one stir worth losing: the next
		// wakeup for any reason reads the agent's name off the agent
		// (chattabs.go's [hopRawTitle]), so the tab catches up on the frame
		// after that.
	}
}

// took clears the arm and reports whether a turn landed since the last look.
func (w *behindWatch) took() bool {
	w.armed.Store(false)
	return w.landed.Swap(false)
}

// landedSince is how many turns have ended in here since the person walked away,
// read WITHOUT consuming anything — the switcher draws its card on a keystroke
// and may draw it many times before anybody switches (hop.go).
func (w *behindWatch) landedSince() int {
	if w == nil {
		return 0
	}
	return int(w.finished.Load())
}

// stop ends the watcher and gives every lane back. Calling it twice is calling
// it once. A watcher that was never started — a test double, or a nil one —
// is already stopped.
func (w *behindWatch) stop() {
	if w == nil {
		return
	}
	w.once.Do(func() {
		if w.quit != nil {
			close(w.quit)
		}
	})
}

// adopt replaces the turn this watcher is draining. It is the held
// conversation's [app.adoptTurn]: a follow-up the session started itself and a
// parked message the keeper just submitted both become the one stream whose
// close supplies the next landing edge.
func (w *behindWatch) adopt(events <-chan session.Event, stop func()) {
	if events == nil {
		if stop != nil {
			stop()
		}
		return
	}
	if w == nil || w.adopts == nil || w.quit == nil {
		if stop != nil {
			stop()
		}
		return
	}
	select {
	case <-w.quit:
		if stop != nil {
			stop()
		}
		return
	default:
	}
	select {
	case w.adopts <- behindTurn{events: events, stop: stop}:
	case <-w.quit:
		if stop != nil {
			stop()
		}
	}
}

// startBehindWatch subscribes to everything this agent has and drains it.
func startBehindWatch(key string, agent Agent, out chan<- behindStirMsg) *behindWatch {
	w := &behindWatch{key: key, agent: agent, out: out, quit: make(chan struct{}), adopts: make(chan behindTurn, 1)}
	go w.run()
	return w
}

// needsPerson is [session.Agent.NeedsPerson] asked of whatever agent this is,
// and false for one that has never heard of the question — which is the honest
// answer for a scripted agent that cannot want anything.
func needsPerson(agent Agent) bool {
	door, ok := agent.(interface{ NeedsPerson() bool })
	return ok && door.NeedsPerson()
}

func (w *behindWatch) run() {
	var stops []func()
	defer func() {
		for _, stop := range stops {
			stop()
		}
	}()
	keep := func(stop func()) {
		if stop != nil {
			stops = append(stops, stop)
		}
	}

	var tasks, designs, runs <-chan session.Event
	var wakes <-chan (<-chan session.Event)
	if door, ok := w.agent.(leavableTasker); ok {
		lane, stop := door.WatchTaskUpdates()
		tasks = lane
		keep(stop)
	}
	if door, ok := w.agent.(leavableWaker); ok {
		lane, stop := door.WatchWakes()
		wakes = lane
		keep(stop)
	}
	if door, ok := w.agent.(leavableDesigner); ok {
		lane, stop := door.WatchHarnessDesigns()
		designs = lane
		keep(stop)
	}
	if door, ok := w.agent.(leavableRunner); ok {
		lane, stop := door.WatchOrchestrations()
		runs = lane
		keep(stop)
	}
	// AND THE NAME, WHICH A HELD CONVERSATION EARNS WHILE NOBODY IS LOOKING AT
	// IT. The tab is drawn from the agent ([hopRawTitle]) and the agent knows the
	// name the moment it lands, so all this lane buys is the frame that redraws
	// the tab — which is why it stirs quietly (names.go, session's title.go).
	var titles <-chan session.Event
	if door, ok := w.agent.(leavableNamer); ok {
		lane, stop := door.WatchTitle()
		titles = lane
		keep(stop)
	}

	// THE TURN IN FLIGHT AT THE MOMENT OF THE DETACH is joined here, and it is
	// joined through the same door a person coming back would use: the surface
	// walked away from the stream the Submit handed it, and this is the reader
	// that stops that stream's pump parking for the rest of the turn.
	var turn <-chan session.Event
	var turnStop func()
	if door, ok := w.agent.(attachable); ok {
		events, running, stop := door.Attach()
		if running {
			turn, turnStop = events, stop
			// A TURN WAS ALREADY IN FLIGHT AT THE DETACH, and that is the one
			// moment this fact cannot be recovered from anywhere else later
			// (tabsignal.go).
			w.turning.Store(true)
		} else {
			stop()
		}
	}
	defer func() {
		if turnStop != nil {
			turnStop()
		}
		// A stop can win the select while an adoption is buffered. Give that
		// subscription back too; otherwise the held agent retains a reader after
		// the conversation has already left this watcher.
		select {
		case left := <-w.adopts:
			if left.stop != nil {
				left.stop()
			}
		default:
		}
	}()
	replaceTurn := func(next <-chan session.Event, stop func()) {
		if turnStop != nil {
			turnStop()
		}
		turn, turnStop = next, stop
		w.turning.Store(next != nil)
	}

	waiting := needsPerson(w.agent)
	w.waits.Store(waiting)
	for {
		select {
		case <-w.quit:
			return
		case next := <-w.adopts:
			// THIS REPLACES RATHER THAN JOINS THE OLD STREAM. The adoption is
			// offered only after an Attach says which turn is current or after a
			// Submit starts the next one, so keeping both readers would count one
			// turn twice and raise two landing edges.
			replaceTurn(next.events, next.stop)
		case ev, ok := <-tasks:
			if !ok {
				tasks = nil
				break
			}
			// THE ONE EVENT ON THIS LANE THAT IS NOT A NUDGE. Another window has
			// asked for this conversation and the engine has agreed, so the stir
			// this raises is not "look at the count" — it is "let go of it"
			// (takeover.go's [app.takeOverKept]).
			if ev.Kind == session.EventTakeover {
				w.takeover.Store(true)
			}
			// AND THE OTHER ONE: the engine holding this conversation has told
			// this window that another one is in it now. The stir is the same
			// "let go of it", and the letting go is a detach
			// (takeover.go's [app.movedKept]).
			if ev.Kind == session.EventMoved {
				w.moved.Store(true)
			}
			// AND EVERY OTHER EVENT ON THIS LANE IS A NODE SAYING WHERE IT IS.
			// Folding it costs a map write; the stir is raised only when the
			// conversation as a whole starts or stops having work in flight, so a
			// graph publishing a node a second does not wake the surface a second.
			if w.noteTask(ev.Task) || w.noteJob(ev.Job) {
				w.stir()
			}
		case _, ok := <-titles:
			if !ok {
				titles = nil
				break
			}
			w.stirName()
		case _, ok := <-designs:
			if !ok {
				designs = nil
			}
		case _, ok := <-runs:
			if !ok {
				runs = nil
			}
		case stream, ok := <-wakes:
			if !ok {
				wakes = nil
				break
			}
			// A TURN THIS CONVERSATION STARTED BY ITSELF — a node landing, a job
			// exiting, a watch firing. It arrives as the stream it will speak on,
			// and draining it is what keeps its pump from parking. A wake while
			// another turn is still being drained replaces it, which is the
			// session's own arrangement: a wake is handed over between turns.
			replaceTurn(stream, nil)
		case _, ok := <-turn:
			if !ok {
				turn = nil
				if turnStop != nil {
					turnStop()
					turnStop = nil
				}
				// A TURN FINISHED IN A CONVERSATION NOBODY IS LOOKING AT. It
				// landed in its own journal and moved nothing on screen; the
				// banner is the whole of what tells the person.
				w.landed.Store(true)
				w.turning.Store(false)
				w.finished.Add(1)
				w.stir()
			}
		}
		if now := needsPerson(w.agent); now != waiting {
			waiting = now
			w.waits.Store(now)
			w.stir()
		}
	}
}

// stirDepth is how deep the shared stir lane is, and it is A BUFFER RATHER THAN
// A LIMIT: nothing refuses a conversation because this number is small. A stir
// says "read the agent" and nothing else, at most one is outstanding per
// conversation ([behindWatch.stir]), and one that finds the lane full is dropped
// because the wakeups already queued will say the same sentence. Eight is enough
// that a person switching between a handful of conversations never loses a
// frame, and a window holding thirty loses nothing a later wakeup does not
// carry.
const stirDepth = 8

// waitStir takes one conversation's stir off the shared lane and asks for the
// next. It is the pump every other standing lane on this surface uses, in the
// one shape that belongs to no conversation at all.
func waitStir(lane <-chan behindStirMsg) tea.Cmd {
	return func() tea.Msg {
		note, ok := <-lane
		if !ok {
			return nil
		}
		return note
	}
}

// stirLane opens the shared stir channel once and starts pumping it.
func (a *app) stirLane() tea.Cmd {
	if a.stirs != nil {
		return nil
	}
	a.stirs = make(chan behindStirMsg, stirDepth)
	return waitStir(a.stirs)
}

// behindStir is one conversation asking to be looked at.
//
// IT RE-READS THE AGENT rather than believing anything the message said, which
// is what makes the message able to say nothing. A key that is no longer in the
// keeper is a conversation that has since been closed, and the honest answer to
// a stir about it is to do nothing.
func (a *app) behindStir(note behindStirMsg) tea.Cmd {
	next := waitStir(a.stirs)
	held := a.behind[note.key]
	if held == nil {
		return next
	}
	// A CONVERSATION THAT HAS JUST NAMED ITSELF IS A REDRAW AND NOTHING ELSE.
	// The tab reads the name off the agent on the frame ([hopRawTitle]), so the
	// frame IS the whole of the refresh; every line below is about work landing,
	// and running them for a name would announce `waiting on you` again for a
	// question the person has already been told about, and spend the landed flag
	// the finished banner is counted from.
	if note.quiet {
		a.touch()
		return next
	}
	// LETTING GO COMES BEFORE ANYTHING ELSE IS READ. A conversation another
	// window has asked for is one this process is about to stop holding, and a
	// banner about a turn that landed in it would be news about a conversation
	// that is leaving (takeover.go). The agent is asked as well as the flag, for
	// the surface that woke on a stir raised by something else.
	if held.watch.moved.Load() {
		// A MOVE IS ASKED FIRST BECAUSE IT IS THE GENTLER ANSWER. The two flags
		// cannot both be true in any road this build has — a conversation is
		// either held in this process or held by an engine — and if a future one
		// ever raises both, detaching from work that is still running is the
		// ending that loses nothing.
		return tea.Batch(next, a.movedKept(note.key, held))
	}
	if held.watch.takeover.Load() || takenOver(held.conv.Agent) {
		return tea.Batch(next, a.takeOverKept(note.key, held))
	}
	landed := held.watch.took()
	var parked tea.Cmd
	continued := false
	if landed && held.side != nil && len(held.side.parks) > 0 {
		// FOLLOW-UPS GO FIRST. The session may have started one as the turn
		// ended; Attach is the authoritative answer. The check runs off the
		// update loop because this agent may be another process or machine.
		parked = a.checkBehindParked(note.key, held.conv.Agent)
		continued = parked != nil || held.side.parkSending
	}
	// The count on the status line and home's own rows are both read from the
	// agent on the frame, so waking the frame is the whole of the refresh.
	a.touch()
	var banner tea.Cmd
	switch {
	case needsPerson(held.conv.Agent):
		// A QUESTION IN A CONVERSATION NOBODY IS LOOKING AT. The presence file
		// says `waiting on you` to every other window on the machine and to
		// home; this is what says it to the person sitting here, who is looking
		// at a different conversation in the same terminal.
		banner = a.notifyBehind(held, notifyAskWord)
	case landed && !continued:
		banner = a.notifyBehind(held, notifyDoneWord)
	}
	// NO FINISHED BANNER FOR AN ANSWER THAT IMMEDIATELY CONTINUES. The finished
	// counter still advances because the tab truthfully reports how many turns
	// landed while the person was away, but a banner saying "finished" beside a
	// conversation already answering their next message would be stale on the
	// frame it appeared.
	// A CONVERSATION THAT HAS JUST STOPPED WORKING IS THE OTHER MOMENT THE SWEEP
	// CAN ACT, and without it a window left holding cold conversations only ever
	// collects one when somebody opens another (this surface has no idle ticker).
	// The conversation this stir came from cannot be what it takes: a stir that
	// raised either banner above raised it because there is a question in there or
	// a turn landed in it, and [app.keptQuiet] refuses both.
	a.sweepKept()
	if a.homeAnimating() {
		return tea.Batch(next, banner, parked, a.wake())
	}
	return tea.Batch(next, banner, parked)
}

// checkBehindParked asks which turn is current without blocking the update
// loop, then either adopts the session's own continuation or starts the oldest
// parked message through the held agent.
//
// THE FOLD LOOKS THE CONVERSATION UP AGAIN. A person can bring it forward or
// close it while Attach is crossing a connection; using the captured sidecar
// afterwards would send into state this window no longer owns.
func (a *app) checkBehindParked(key string, agent Agent) tea.Cmd {
	door, ok := agent.(attachable)
	if !ok {
		return nil
	}
	return a.offLoop(func() func(bool) tea.Cmd {
		events, running, stop := door.Attach()
		return func(bool) tea.Cmd {
			held := a.behind[key]
			if held == nil || held.side == nil || len(held.side.parks) == 0 {
				stop()
				// An attachable conversation can still have come forward after
				// its idle reading. If it did, this fold is the only remaining
				// edge that knows the parked queue is ready to go.
				if !running && key == a.convKey(a.file) && len(a.parks) > 0 {
					return a.sendParked()
				}
				return nil
			}
			if running {
				held.watch.adopt(events, stop)
				return nil
			}
			stop()
			return a.sendBehindParked(key, held)
		}
	})
}

// sendBehindParked starts one held message and leaves it at the front of the
// queue until the call succeeds. That is what lets a read error or a closed
// agent return the complete message — pictures and paste bodies included —
// instead of turning a failed send into lost input.
func (a *app) sendBehindParked(key string, held *kept) tea.Cmd {
	if held == nil || held.side == nil || held.side.parkSending || len(held.side.parks) == 0 {
		return nil
	}
	held.side.parkSending = true
	held.side.parks[0].sending = true
	p := held.side.parks[0]
	_, shown, start := parkedStart(held.conv.Agent, a.ctx, a.hosted(), p)
	return func() tea.Msg {
		ch, err := start()
		return behindParkedMsg{key: key, park: p, shown: shown, ch: ch, err: err}
	}
}

// tookBehindParked folds the off-loop send into whichever place now owns the
// conversation. Success spends the marked head exactly once and hands its
// stream to that conversation's watcher; failure clears only the crossing mark
// and leaves the full message waiting.
func (a *app) tookBehindParked(msg behindParkedMsg) tea.Cmd {
	failure := func(err error) string { return "submit failed: " + err.Error() }
	if held := a.behind[msg.key]; held != nil && held.side != nil {
		held.side.parkSending = false
		if len(held.side.parks) > 0 && held.side.parks[0].sending {
			if msg.err != nil {
				held.side.parks[0].sending = false
				held.side.parkNotes = append(held.side.parkNotes, failure(msg.err))
				a.stowDrafts(held.conv, held.side)
				return nil
			}
			held.side.parks = held.side.parks[1:]
		}
		a.stowDrafts(held.conv, held.side)
		held.watch.adopt(msg.ch, nil)
		return nil
	}

	// The conversation may have come forward while Submit was crossing. The
	// sidecar moved its queue and crossing mark onto the app, so identity still
	// decides whether this answer belongs here.
	if msg.key == a.convKey(a.file) {
		a.parkSending = false
		if len(a.parks) > 0 && a.parks[0].sending {
			if msg.err != nil {
				a.parks[0].sending = false
			}
			if msg.err == nil {
				a.parks = a.parks[1:]
			}
		}
		if msg.err != nil {
			a.note(failure(msg.err))
			return tea.Batch(a.edited(), a.settle())
		}
		// If Attach already found this turn, its atomic replay supplied the user
		// line and its stream is the one to keep. Otherwise the send crossed
		// after that idle reading, so draw the ordinary line here and adopt the
		// channel Submit returned.
		if a.stream == nil {
			a.drawBehindParked(msg.park, msg.shown)
			return a.takeStream(msg.ch)
		}
		if msg.ch != nil {
			// Attach already owns a separate subscription to this turn. Drain
			// Submit's original subscription as well: dropping it would leave the
			// agent's pump parked on a reader that came forward during the call.
			return func() tea.Msg {
				for range msg.ch {
				}
				return nil
			}
		}
		return nil
	}

	// The conversation was closed or moved while the call crossed. Nobody owns
	// this returned stream now, but it still must be drained so the agent's event
	// pump cannot park on a reader that disappeared.
	if msg.ch != nil {
		return func() tea.Msg {
			for range msg.ch {
			}
			return nil
		}
	}
	return nil
}

// drawBehindParked is the front half of an already-started background send. It
// is reached only when the conversation came forward before Attach could see
// the new turn; ordinary background completion is drawn later from the journal.
func (a *app) drawBehindParked(p parked, shown string) {
	a.turn++
	a.sel = -1
	if a.openingPrompt == "" {
		a.openingPrompt = shown
	}
	line := shown
	pictures := chipPaths(pictureChips(p.chips))
	if len(p.chips) > 0 {
		line = userLine(shown, p.chips, a.pal)
	}
	a.said(entry{kind: entryUser, text: line, turn: a.turn, began: a.now(), context: a.turnContext(), pictures: pictures, picturesHere: len(pictures) > 0})
	for _, picture := range pictures {
		a.learnPicture(picture, true)
	}
	a.state = stateWorking
	a.lastDelta = time.Now()
	a.awaited = time.Now()
	a.startClock()
	a.follow()
	a.touch()
}

// ── holding a conversation, and taking one back ─────────────────────────────

// stow puts the conversation that was just detached into the keeper and starts
// its watcher.
//
// THE CRASH INSURANCE IS WRITTEN HERE and not in the detach, because this is the
// branch where the conversation goes on existing: a machine that loses power
// with three conversations open should give all three boxes back, and the
// sidecar is memory (draft.go).
func (a *app) stow(conv Conversation, side *aside) tea.Cmd {
	key := a.convKey(conv.SessionFile)
	if key == "" || conv.Agent == nil {
		return nil
	}
	// A DOOR WHOSE CONVERSATIONS SHARE ONE HANDLE KEEPS NOTHING HERE, and this is
	// the one guard rather than a branch at each of the four callers — /new, the
	// two beside-doors and a switch all end up on this line.
	//
	// WHAT WOULD HAPPEN OTHERWISE is not a missing feature, it is a lie on the
	// screen. Over an engine door [Options.Resume] hands back the SAME agent it
	// was given, now pointing at the session the engine has just swapped to
	// (cmd/codeaf's chatv3_host.go). Putting that pointer in the map records the
	// conversation now IN FRONT under the key of the one being left: the switcher
	// then draws it twice, one of them under the old name, and pressing the held
	// row opens the body of the conversation already on screen. It would also arm
	// a [behindWatch] on the agent the surface is itself reading, so one handle's
	// lanes would have two readers and the drain would eat events the frame is
	// waiting for.
	//
	// AND THE CONVERSATION BEING LEFT IS NOT LOST BY SKIPPING THIS — it is already
	// gone: the engine interrupts and closes the previous conversation as part of
	// the swap (internal/remote's Session.swap). There is nothing running to hold.
	//
	// WHAT IS KEPT ANYWAY IS THE UNSENT SENTENCE. The agent is not this window's
	// to hold, but the words in the box were never the agent's — they are the
	// person's, they are not on the wire, and a switch that dropped them would
	// lose a paragraph somebody was in the middle of writing every time they
	// pressed a tab. So the composer goes down under THIS conversation's own
	// identity exactly as it does below, and comes back through the same reunion
	// when the conversation is opened again (draftkeep.go's [app.stowDrafts] and
	// [app.layKeptDrafts]).
	if a.shared {
		a.stowDrafts(conv, side)
		// Retain the outgoing navigation identity even though its agent ended.
		a.rememberOpen(key)
		return nil
	}
	// AND IT IS THE WHOLE COMPOSER, not only the box: every page's own unsent line
	// goes down under THIS conversation's identity (draftkeep.go's
	// [app.stowDrafts]), because the conversation now in front is about to write
	// its own record under a different name.
	a.stowDrafts(conv, side)
	if a.behind == nil {
		a.behind = map[string]*kept{}
	}
	held := &kept{
		conv:  conv,
		side:  side,
		watch: startBehindWatch(key, conv.Agent, a.stirs),
	}
	a.behind[key] = held
	a.rememberOpen(key)
	var parked tea.Cmd
	if side != nil && len(side.parks) > 0 {
		// The turn may have ended between the park and the watcher joining. Ask
		// once after the watcher exists: an idle answer means there is no future
		// close edge to wake it, so the oldest message goes now. The helper keeps
		// this potentially remote door off the update loop.
		parked = a.checkBehindParked(key, conv.Agent)
	}
	// AND THE KEEPER GIVES BACK WHAT IT CAN, on the one keystroke that grew it. The
	// conversation just stowed is the youngest thing in there and can never be
	// what the sweep takes ([keptIdleGrace]), so this collects a conversation
	// somebody stopped thinking about rather than the one they just left.
	a.sweepKept()
	return parked
}

// rememberOpen puts a key on top of the previous-stack, which is the order `tab`
// walks and the order [app.closeFront] brings a conversation forward in.
//
// EVERY EARLIER MENTION OF THE KEY IS REMOVED FIRST, written as a filter rather
// than as a search: a conversation switched to four times is one conversation,
// and a stack that grew an entry per visit would send `tab` somewhere it has
// already been.
func (a *app) rememberOpen(key string) {
	a.forget(key)
	a.prev = append(a.prev, key)
	// AND A CONVERSATION COMING FORWARD GETS ITS TAB BACK. This is the one door
	// every road to the front goes through — a switch, a resume, an open beside,
	// a close bringing the next one up — so a dismissal lifted here cannot be
	// missed by a road somebody adds later (chattabs.go's [app.tabDismiss]).
	if a.tabShut[key] {
		delete(a.tabShut, key)
		a.chatTabBar = tabBar{}
	}
	if a.at(pageHome) && a.home.tabs != nil {
		a.home.build()
	}
}

// forget takes a key off the previous-stack, every occurrence of it.
func (a *app) forget(key string) {
	out := a.prev[:0]
	for _, held := range a.prev {
		if held != key {
			out = append(out, held)
		}
	}
	a.prev = out
}

// lastBehind is the conversation `tab` goes to: the most recently in front of
// the ones this process still holds. Keys the keeper no longer has are stepped
// over rather than cleaned up, because a close already filters them.
func (a *app) lastBehind() (string, bool) {
	for at := len(a.prev) - 1; at >= 0; at-- {
		if a.behind[a.prev[at]] != nil {
			return a.prev[at], true
		}
	}
	return "", false
}

// holding reports whether THIS PROCESS has this transcript open — in front or in
// the keeper.
//
// IT IS ASKED BEFORE THE FLOCK IS, EVERYWHERE, and that is a correctness rule
// rather than a nicety. [session.InUse] answers by taking a flock on a fresh
// descriptor, and a flock rides the OPEN FILE DESCRIPTION rather than the
// process — so a transcript this process already holds conflicts with itself and
// reports `open in another window` about a conversation one keystroke away.
func (a *app) holding(file string) bool {
	key := a.convKey(file)
	if key == "" {
		return false
	}
	return key == a.convKey(a.file) || a.behind[key] != nil
}

// bringForward points the surface at a conversation this process is already
// holding, and reports whether it did.
//
// It is the first thing every door onto a transcript asks — home's enter, the
// resume picker, the welcome box's rows, /resume by argument — because a
// conversation in the keeper is not something to open. It is something to look
// at again.
func (a *app) bringForward(file string) (cmd tea.Cmd, owned bool) {
	if a.startingChat() {
		back := a.parkChatStart()
		defer func() { cmd = tea.Batch(back, cmd) }()
	}
	key := a.convKey(file)
	if key == "" {
		return nil, false
	}
	if key == a.convKey(a.file) {
		// Already the one on screen. Every door answers this by staying where it
		// is rather than reopening, which would drop the lock, replay the journal
		// and land exactly here.
		return nil, true
	}
	held := a.behind[key]
	if held == nil {
		return nil, false
	}
	delete(a.behind, key)
	held.watch.stop()
	leaving, side := a.front(), a.detachConversation()
	parked := a.stow(leaving, side)
	cmd = tea.Batch(parked, a.attachConversation(held.conv, held.side))
	a.rememberOpen(key)
	return cmd, true
}

// openBeside opens a transcript in ITS OWN workspace and puts the conversation
// that was in front into the keeper, still running.
//
// IT IS THE OTHER HALF OF [app.openSession], and the difference between them is
// the whole feature: one closes what it leaves, this one keeps it. A refusal
// costs nothing at all — the new conversation is opened BEFORE the old one is
// detached, so a door that says no leaves the person exactly where they were,
// with a live and writable conversation on screen.
func (a *app) openBeside(workspace, transcript string) (tea.Cmd, string) {
	if !a.canOpen() {
		return nil, resumeUnavailableWord
	}
	// IDENTITY IS ASKED BEFORE THE DOOR IS, which is [app.openSession]'s own rule
	// and was missing here. A row naming the conversation ALREADY IN FRONT is
	// answered by staying in it: asking the door for it would meet this process's
	// own flock and refuse `open in another window` about the session on screen,
	// and over a shared handle it would ask the engine to swap onto the
	// conversation it already has open. A row the keeper holds is a switch.
	if cmd, ours := a.bringForward(transcript); ours {
		return cmd, ""
	}
	if a.open == nil {
		if a.resume == nil {
			return nil, resumeUnavailableWord
		}
		agent, err := a.resume(transcript)
		if err != nil {
			return nil, err.Error()
		}
		return a.takeBeside(Conversation{Agent: agent, Workspace: workspace, SessionFile: transcript}), ""
	}
	conv, err := a.open(workspace, transcript)
	if err != nil {
		if errors.Is(err, session.ErrSessionLocked) {
			return nil, sessionBusyWord
		}
		return nil, err.Error()
	}
	return a.takeBeside(conv), ""
}

// startBeside mints a FRESH conversation in a workspace and puts the one in
// front into the keeper. It is what a path typed on home opens.
func (a *app) startBeside(workspace string) (tea.Cmd, string) {
	if !a.canStart() || a.start == nil {
		return nil, newUnavailableWord
	}
	conv, err := a.start(workspace)
	if err != nil {
		return nil, err.Error()
	}
	cmd := a.takeBeside(conv)
	// A conversation started while a team is shown is one of that team
	// (teams.go's [app.teamJoinFront]).
	a.teamJoinFront()
	return cmd, ""
}

// takeBeside is the two lines both doors above end in: the conversation on
// screen steps aside and goes on running, and the new one takes the surface.
func (a *app) takeBeside(conv Conversation) tea.Cmd {
	// The name of what is being left, read while it is still in front, and said
	// afterwards on a door that could not keep it (below). Notes are cleared by
	// the detach, so this is remembered rather than written now.
	closed := a.sessionName()
	if closed == "" {
		closed = a.place
	}
	leaving, side := a.front(), a.detachConversation()
	parked := a.stow(leaving, side)
	if a.shared {
		// The legacy wire seam returns only an Agent. The local draft store
		// belongs to this window, with separate owner-scoped slots inside it.
		if conv.DraftFile == "" {
			conv.DraftFile = leaving.DraftFile
		}
		if conv.History == nil {
			conv.History = leaving.History
		}
	}
	cmd := tea.Batch(parked, a.attachConversation(conv, nil))
	if a.shared {
		a.restoreDraft()
	}
	if key := a.convKey(conv.SessionFile); key != "" {
		a.rememberOpen(key)
	}
	if conv.Notice != "" {
		a.note(conv.Notice)
	}
	// AND A DOOR THAT DID NOT ACTUALLY OPEN ONE BESIDE SAYS SO. Every caller of
	// this function promises the conversation on screen goes on running; over a
	// shared handle it does not, because the engine ended it in the swap
	// ([Options.SharedAgent]). A person who watched their work vanish off the
	// switcher is owed the sentence rather than the discovery.
	if a.shared {
		if closed != "" {
			a.note("closed · " + closed + " — " + oneConversationWord)
		} else {
			a.note(oneConversationWord)
		}
	}
	return cmd
}

// oneConversationWord is what a door says when it swapped a conversation in
// place because this window's engine holds one at a time
// ([Options.SharedAgent]). It names the connection and not the machine: the
// limit belongs to the wire's one open session, and the same sentence is true
// over `--host`, over `--at` and over the socket an ordinary `codeaf chat` opens
// onto this machine's own engine.
const oneConversationWord = "a connection holds one conversation at a time"

// lastConversation is `tab`: the way back to the conversation that was in front
// before this one.
//
// IT DOES NOTHING WHEN THERE IS NOWHERE TO GO, and says nothing about it. One
// conversation open, or none this terminal has been in before, is a key that
// cannot act — and a key that cannot act says so by not being advertised
// (home.go's legend slot carries `tab last` only while there is a last one).
func (a *app) lastConversation() tea.Cmd {
	key, ok := a.lastBehind()
	if !ok {
		return nil
	}
	cmd, _ := a.bringForward(a.behind[key].conv.SessionFile)
	return cmd
}

// closeFront ends the conversation on screen for real and brings the most
// recently open one forward, or reports that there was nothing to come forward.
//
// THIS IS THE ONE PLACE AN AGENT IS CLOSED BY A PERSON'S KEYSTROKE, and the
// close is the whole difference between it and a switch.
func (a *app) closeFront() (tea.Cmd, bool) {
	return a.closeFrontFor(session.StopByLeaving)
}

// closeFrontFor is the same close with the DOOR it came through on it. The
// engine writes that word down and says one sentence about a reply that never
// arrived, and "you closed this conversation" and "another window took it" are
// two different sentences to be owed (internal/session's stopcause.go).
func (a *app) closeFrontFor(door session.StopDoor) (tea.Cmd, bool) {
	return a.leaveFront(func(agent Agent) { a.endAgentFor(agent, door) }, true)
}

// stepBackFront is the same act for a window that is NOT ending anything: another
// window has opened this conversation through the engine that holds it, and this
// one is getting out of the seat ([app.movedAway]).
//
// IT DETACHES WHERE [app.closeFront] CLOSES, and that one line is the whole
// difference. The engine is still running the turn and still holding the tasks;
// a close would tell it the conversation is over and undo the very thing the
// engine road exists for. [leaveAgent] is the same judgement `quit` makes, so a
// conversation with no engine behind it still ends here, because there would be
// nothing left to run it.
//
// AND IT SAYS NOTHING ABOUT WHAT IT LEFT. `closed · <name>` would be a false
// sentence — nothing closed — and the true one is said by the caller in the
// conversation's own vocabulary ([session.MovedWord]).
func (a *app) stepBackFront() (tea.Cmd, bool) {
	return a.leaveFront(leaveAgent, false)
}

// endAgent is a person ending a conversation: interrupt whatever is running and
// close it, with the failure said where they can see it.
func (a *app) endAgent(agent Agent) { a.endAgentFor(agent, session.StopByLeaving) }

// endAgentFor is the same ending, naming the door. A turn still in flight is
// stopped by machinery here whatever the key was — the person asked for the
// CONVERSATION to go, not for the reply to be thrown away — so the engine owes
// them a sentence about the answer that never came.
func (a *app) endAgentFor(agent Agent, door session.StopDoor) {
	agent.InterruptFor(door)
	if err := agent.Close(); err != nil {
		a.note("close failed: " + err.Error())
	}
}

// leaveFront is the act both doors above are: the conversation in front is let
// go of, and the most recently open one comes forward.
//
// THE TWO CALLERS DIFFER IN ONE THING AND IT IS THE ONE THAT MATTERS — what
// letting go MEANS. Everything else here is the same act, and a second copy of
// it would be the second answer to whether a steer still waiting on a write may
// cross into the conversation that replaced this one.
func (a *app) leaveFront(let func(Agent), say bool) (tea.Cmd, bool) {
	next, ok := a.lastBehind()
	if !ok {
		return nil, false
	}
	held := a.behind[next]
	delete(a.behind, next)
	held.watch.stop()
	leaving, file := a.agent, a.file
	a.forget(a.convKey(file))
	// AND THE CORRECTIONS IT WAS STILL WAITING ON GO WITH IT, BEFORE ITS RECORD IS
	// CLEARED BELOW. A send still waiting on a write must not cross into a
	// conversation that has been closed, and the cleared record must not be read as
	// an answer about it either (steersend.go's [app.forgetSteerOwner]).
	a.forgetSteerOwner(draftOwnerOf(a.host, a.workspace, file))
	a.detachConversation()
	if leaving != nil {
		let(leaving)
	}
	// THE DRAFT FILE OF A CLOSED CONVERSATION GOES WITH IT. It is crash
	// insurance for a conversation that is no longer at risk, and leaving it
	// would hand somebody's finished sentence to the next window that opens on
	// that directory as an orphan (draft.go's [adoptDraft]).
	if a.draftFile != "" {
		dropDraftFile(a.draftFile)
	}
	closed := a.sessionName()
	if closed == "" {
		closed = a.place
	}
	cmd := a.attachConversation(held.conv, held.side)
	a.rememberOpen(next)
	if say && closed != "" {
		a.note("closed · " + closed)
	}
	return cmd, true
}

// leaveEverything is what the PROGRAM leaving does to the conversations this
// terminal holds — the one in front and every one in the keeper — IN PARALLEL,
// and it is safe to call twice.
//
// LEAVING A WINDOW IS NOT ENDING SOMEBODY'S WORK. This used to interrupt and
// close every agent, which is right for a conversation whose engine is this
// process and wrong for one that is hosted: a hosted agent's Close is a message
// to the far side saying the conversation is over ([remote.Agent.Close] sends
// it), so closing a terminal on a running task paused the task and restarted its
// worker on the way back. The window going away is a view leaving; the engine
// keeps the turn, the tasks, the questions and the journal.
//
// So each agent is asked which it is ([detachable]) and answers for itself. An
// in-process conversation still ends here, because there is nothing left to run
// it once this process is gone.
//
// PARALLEL BECAUSE THE GRACES OVERLAP RATHER THAN SUM. [session.Agent.Close] is
// bounded on every axis and its phases are sequential, so a row of closes is
// that many times the wait — and every one of those clocks exists precisely so a
// quit never waits on somebody else's courtesy. The keeper's ceiling is soft
// and a window can hold more conversations than the switcher draws, so a
// serial quit would get slower the more of them somebody had open; this one
// does not.
func (a *app) leaveEverything() {
	agents := make([]Agent, 0, len(a.behind)+1)
	if a.agent != nil {
		agents = append(agents, a.agent)
	}
	for key, held := range a.behind {
		held.watch.stop()
		if held.conv.Agent != nil {
			agents = append(agents, held.conv.Agent)
		}
		delete(a.behind, key)
	}
	a.prev = nil
	var wg sync.WaitGroup
	for _, agent := range agents {
		wg.Add(1)
		go func(agent Agent) {
			defer wg.Done()
			leaveAgent(agent)
		}(agent)
	}
	wg.Wait()
}

// leaveAgent takes this terminal off one conversation: a detach where the work
// outlives the window, and the ordinary interrupt-and-close where it does not.
func leaveAgent(agent Agent) {
	if hosted, ok := agent.(detachable); ok {
		_ = hosted.Detach()
		return
	}
	agent.InterruptFor(session.StopByLeaving)
	_ = agent.Close()
}

// leaveOffFrame is [leaveAgent] started away from the caller. Detach and
// Interrupt-and-Close can wait; the update path cannot. [app.leaveEverything]
// starts the same work in a goroutine and waits, because the process is going
// away. The sweep only starts it — the keeper has already forgotten the
// conversation, and a keystroke is not charged for the close.
func leaveOffFrame(agent Agent) {
	go leaveAgent(agent)
}

// workOutlivesExit reports whether this conversation's work would keep going
// after the window closed. It is what the switcher's rows and the close-a-tab
// card are written from, so the sentence and the act cannot disagree.
func workOutlivesExit(agent Agent) bool {
	hosted, ok := agent.(detachable)
	return ok && hosted.WorkOutlivesExit()
}

// ── what the keeper is asked on a frame ─────────────────────────────────────

// openCount is how many conversations this process holds: the keeper's size
// plus the one on screen.
func (a *app) openCount() int { return len(a.behind) + 1 }

// waitingCount is how many of the ones in the keeper want a person.
//
// IT ASKS THE AGENTS AND NOT THE PRESENCE FILE. The file is written on a
// five-second heartbeat and believed for fifteen, which is right for another
// window and wrong for an agent whose pointer is in this process's own map: a
// person who switches away from a question and looks at the count would watch it
// say the wrong thing for up to five seconds. One predicate, three readers —
// this, home's rung, and the banner — and they cannot disagree.
func (a *app) waitingCount() int {
	n := 0
	for _, held := range a.behind {
		if needsPerson(held.conv.Agent) {
			n++
		}
	}
	return n
}

// behindTasks is how many nodes are running in the conversations this process
// holds but is not drawing, counted ON THE KEYSTROKE and never on a frame.
//
// IT IS TASKS AND NOT JOBS, and that is a limit rather than an oversight: a
// promoted shell is counted out of the transcript on screen (render.go's
// [app.computeStats]), and there is no door on the engine that answers "what
// background jobs is this agent running". So a second ctrl+c names every
// running task in every open conversation and names background jobs only in the
// one on screen. A count that guessed at the rest would be a warning nobody
// could trust; the sentence is short of a fact rather than wrong about one, and
// the manual says so.
func (a *app) behindTasks() int {
	tasks := 0
	for _, held := range a.behind {
		// THE COUNT GOES THROUGH ONE FUNCTION, which the switcher's rows also
		// call (hop.go's [runningTasks]): the assertion, the status string and
		// the walk were about to exist twice, and two spellings of one count is
		// how a status line and a card come to disagree about the same session.
		tasks += runningTasks(held.conv.Agent)
	}
	return tasks
}

// behindSince is when a conversation was detached, which is what home's row
// measures "since you last looked" from. Zero for one this process does not
// hold.
func (a *app) behindSince(file string) time.Time {
	held := a.behind[a.convKey(file)]
	if held == nil || held.side == nil {
		return time.Time{}
	}
	return held.side.since
}

// closeKept ends one conversation the keeper is holding, for real.
//
// IT IS [app.closeFront] WITHOUT THE HALF THAT MOVES THE SURFACE. That function
// closes the conversation ON SCREEN and has to bring another forward in the same
// breath; this one closes a conversation nobody is looking at, so there is
// nothing to attach, no sidecar to restore and no draft to hand over — only the
// agent to end, the watcher to stop, and the two places the key was remembered.
//
// THE DRAFT FILE GOES WITH IT, on [app.closeFront]'s own reasoning: it is crash
// insurance for a conversation that is no longer at risk, and left behind it is
// somebody's finished sentence orphaned in a directory (draft.go's [adoptDraft]).
func (a *app) closeKept(file string) bool {
	key := a.convKey(file)
	held := a.behind[key]
	if key == "" || held == nil {
		return false
	}
	a.letGoKept(key, held, func(agent Agent) {
		agent.InterruptFor(session.StopByLeaving)
		if err := agent.Close(); err != nil {
			a.note("close failed: " + err.Error())
		}
	})
	return true
}

// letGoKept is the bookkeeping every road out of the keeper shares, with WHAT
// LETTING GO MEANS left to the caller — the one thing a person's `ctrl+w` and the
// sweep below do not agree about. It is [app.leaveFront]'s shape said for a
// conversation nobody is looking at, and for its reason: a second copy of these
// five lines would be the second answer to whether a steer still waiting on a
// write may cross into the conversation that replaced this one.
func (a *app) letGoKept(key string, held *kept, let func(Agent)) {
	delete(a.behind, key)
	a.forget(key)
	// Its unsettled corrections go before its record is cleared, for
	// [app.closeFront]'s reason.
	a.forgetSteerOwner(draftOwnerOf(a.host, held.conv.Workspace, held.conv.SessionFile))
	if held.watch != nil {
		held.watch.stop()
	}
	if held.conv.Agent != nil {
		let(held.conv.Agent)
	}
	if held.conv.DraftFile != "" {
		dropDraftFile(held.conv.DraftFile)
	}
}

// ── the sweep: what gives a conversation's cost back ────────────────────────

// keptCeiling is how many conversations this window holds — the one on screen and
// every one in the keeper — before the sweep starts looking for one to let go of.
//
// IT IS A CEILING AND NOT A CAP, and the difference is the whole of the design
// above: nothing is ever refused for this number, and a window whose held
// conversations are all busy sails past it. The number is the switcher card's
// own row count ([hopShown]): a person who can see every conversation they have
// open on one card has not lost track of any of them, and the first one this
// sweep can take is by definition one they have not looked at in a quarter of
// an hour.
const keptCeiling = hopShown

// keptIdleGrace is how long a conversation has to have been left alone before the
// sweep will consider it, measured from the moment it was detached ([aside.since])
// — which is the same figure the switcher's row draws as "since you last looked".
//
// FIFTEEN MINUTES IS LONG ENOUGH THAT SWITCHING AROUND COSTS NOTHING. Somebody
// working across four projects is in and out of each of them inside a minute, and
// every one of those conversations is younger than this grace, so the sweep never
// reaches them however many are open. What it reaches is the conversation opened
// before lunch and not thought about since.
const keptIdleGrace = 15 * time.Minute

// The two sentences the sweep says, and they differ in the only fact a person
// would act on: whether the work in there went with it.
const (
	keptSweptWord     = "quiet a while — open it again from home"
	keptSweptOnWord   = "quiet a while — its work keeps running"
	keptSweptNameWord = "let go · "
)

// sweepKept lets go of the coldest quiet conversations this window holds until it
// is back under [keptCeiling], and does nothing at all when none of them can be
// let go of without costing the person something.
//
// THE ORDER IS COLDEST FIRST, and it is [app.prev]'s own order read from the
// bottom: that stack is the conversations this window has been in, most recent
// last ([app.rememberOpen]), so walking it forwards is walking from the one left
// longest ago. Keys it holds that the keeper does not are stepped over — the
// conversation in front is on that stack too, and it is never a candidate.
//
// IT IS CALLED WHERE THE TWO FACTS IT READS CHANGE, and nowhere else: [app.stow],
// which is the only place the keeper grows, and [app.behindStir], which is where
// a conversation says it has stopped working. There is no idle ticker on this
// surface and this is not the lane that gets one (render.go) — so a window left
// completely alone keeps what it holds, and the first keystroke that opens
// another conversation is what collects it.
//
// THE AGENT LEAVES OFF THIS FRAME. Both callers sit on Bubble Tea's update
// path, and [leaveAgent] is the same wait `quit` makes — a Detach on a hosted
// conversation, Interrupt and Close on an in-process one. The keeper forgets
// the conversation here so the ceiling and the switcher move on this keystroke;
// the agent is taken off afterwards ([leaveOffFrame]), the way
// [app.leaveEverything] already starts that work in a goroutine.
func (a *app) sweepKept() {
	if len(a.behind) == 0 {
		return
	}
	now := a.now()
	for a.openCount() > keptCeiling {
		key, held := a.coldestQuiet(now)
		if held == nil {
			return
		}
		// Both read while the conversation is still held, and said after it is
		// gone: the name comes off its own agent, and whether its work outlives
		// this window is what picks the sentence ([workOutlivesExit]).
		name, running := hopTitle(held.conv.Agent, held.side), workOutlivesExit(held.conv.Agent)
		a.letGoKept(key, held, leaveOffFrame)
		said := keptSweptWord
		if running {
			said = keptSweptOnWord
		}
		a.note(keptSweptNameWord + name + " — " + said)
		a.touch()
	}
}

// coldestQuiet is the held conversation that has been left the longest and can be
// let go of for nothing, or no conversation at all.
func (a *app) coldestQuiet(now time.Time) (string, *kept) {
	for _, key := range a.prev {
		held := a.behind[key]
		if held != nil && a.keptQuiet(held, now) {
			return key, held
		}
	}
	return "", nil
}

// keptQuiet is the whole list of what makes a held conversation free to let go
// of, and EVERY LINE OF IT IS A REASON NOT TO. A conversation that fails one of
// them is one whose person would come back to something missing, so the sweep
// leaves it alone and the window holds more than the ceiling instead.
//
// NOTHING HERE OPENS A FILE OR CROSSES A WIRE. The work a conversation is doing is
// read off its watcher's cached flags, which the drain already computes for the
// tab strip (tabsignal.go's law), and never from [session.Agent.TaskIndex], which
// reads a file, or from a presence file written on a five-second heartbeat. The
// one door it does knock on is [session.Agent.NeedsPerson], because a question is
// the fact this must not be wrong about and the watcher's copy of it is only as
// fresh as the last edge it saw.
//
// A CONVERSATION WITH NO WATCHER IS NEVER SWEPT. That is a conversation over a
// shared handle, or one this window only remembers, and an unset flag on it is
// "nothing known" rather than "nothing running" — the tab strip's own law about
// what an absent watcher is allowed to claim.
func (a *app) keptQuiet(held *kept, now time.Time) bool {
	if held == nil || held.watch == nil || held.conv.Agent == nil {
		return false
	}
	w := held.watch
	// A conversation another window has asked for or has already opened is
	// leaving by its own road, with its own sentence; the sweep does not race
	// those two endings (takeover.go).
	if w.takeover.Load() || w.moved.Load() {
		return false
	}
	// Work in flight, of any of the four kinds this surface knows about.
	if w.turning.Load() || w.tasking.Load() || w.jobbing.Load() {
		return false
	}
	// A question, and news of a turn that ended in there that the person has not
	// been shown yet. Letting either go would be throwing away the very thing
	// they left the conversation open for.
	if w.waits.Load() || needsPerson(held.conv.Agent) {
		return false
	}
	if w.landed.Load() || w.landedSince() > 0 {
		return false
	}
	// AND ALL OF THE PERSON'S UNSENT WORDS ARE THEIR OWN. A draft, a waiting
	// message, a picture on either, a document behind a paste tag, a message that
	// has left the box and not settled, a line typed at one of its task pages:
	// any of them and this conversation is somebody's unfinished sentence rather
	// than a cost. Parks used to be folded into draft, so separating them without
	// naming them here would make the sweep able to discard the new sidecar state.
	if held.side == nil {
		return false
	}
	side := held.side
	if strings.TrimSpace(side.draft) != "" || len(side.parks) > 0 || side.parkSending || len(side.chips) > 0 || len(side.pastes) > 0 || len(side.sends) > 0 || len(side.composers) > 0 {
		return false
	}
	// And it has to have been left alone for long enough that letting go of it is
	// not a surprise. An undated sidecar is not evidence of age, so it waits.
	return !side.since.IsZero() && now.Sub(side.since) >= keptIdleGrace
}
