package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/lease"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
	"github.com/Agent-Field/aforge-v2/internal/tui"
)

// chatWindow is everything one aforge window owns regardless of which process
// is running the brain: the durable store it reads, the conversation it is
// looking at, and where both live on disk. Two windows on the same store are
// two chatWindows over one file, which is the whole arrangement.
type chatWindow struct {
	path     string
	database string
	dir      string
	graph    *store.Store
	session  string
}

func openChatWindow(path, database, requestedSession string) (*chatWindow, error) {
	graph, err := store.Open(path)
	if err != nil {
		return nil, err
	}
	// The session is resolved here rather than at flag-definition time because
	// only the journal knows which conversation this is. Everything downstream
	// reads the resolved id; nothing reads the flag again.
	session, err := resolveChatSession(graph, requestedSession)
	if err != nil {
		_ = graph.Close()
		return nil, err
	}
	// One tidying pass per launch, after the room is chosen so the chosen room
	// is never among the ones taken back.
	groomChatRooms(graph, session)
	return &chatWindow{
		path: path, database: database, dir: filepath.Dir(path),
		graph: graph, session: session,
	}, nil
}

func (w *chatWindow) close() {
	if w != nil && w.graph != nil {
		_ = w.graph.Close()
	}
}

const (
	// residencyProbeEvery bounds how often a visitor asks the lock who holds
	// it. The surface polls faster than this when the thread is busy, and the
	// question "is the brain still there" does not get a more useful answer for
	// being asked three times a second — but it must be asked often enough that
	// a resident which exits is noticed within a breath of the user wondering.
	residencyProbeEvery = time.Second
	// handoverPatience is how long a request waits before the silence is read
	// as an answer. A resident that knows the verb settles the command on its
	// very next pass; one that does not know it leaves the command pending
	// forever, and after this the note stops claiming a handover is coming.
	handoverPatience = 30 * time.Second
)

// chatResidency is the one thing in a window that knows whether this process is
// the brain, and the only thing allowed to change the answer.
//
// A window starts as whatever the lock lets it be and may move in either
// direction afterwards: a visitor promotes when the resident exits or its
// heartbeat goes stale, and a resident stands down when a newer build asks for
// the role. Both directions swap the window's commander rather than mutating
// one in place, because the two shapes genuinely differ — a resident's
// commander holds model clients and a head, a visitor's holds neither — and a
// swap is one assignment on the surface's own goroutine instead of a lock
// around every capability the TUI can reach.
type chatResidency struct {
	window *chatWindow
	stamp  lease.Build
	// build is the brain constructor. It is a field so a test can promote a
	// window without a provider key and a network — the mechanism being proved
	// is which process runs a brain, not what the brain says.
	build func(*chatWindow, string, resident.HandoverFunc) (*chatBrain, error)
	// probe is the floor between two reads of the lock, held as a field for the
	// same reason: a test should not have to spend a second of wall clock to
	// watch a role move.
	probe time.Duration

	mu        sync.Mutex
	resident  bool
	brain     *chatBrain
	release   func() error
	surface   *resident.Reconciler
	current   *chatCommander
	handed    tui.Commander
	holder    *lease.Resident
	promoting bool
	closed    bool
	askedPID  int
	note      string
	// notePID is the process the note is about, so a note left over from one
	// holder is not still on screen describing the next one.
	notePID   int
	nextProbe time.Time
}

func newChatResidency(window *chatWindow) *chatResidency {
	return &chatResidency{
		window: window, stamp: lease.LocalBuild(),
		build: buildChatBrain, probe: residencyProbeEvery,
	}
}

// claim decides what this window is when it opens. The flock is the arbiter:
// the first aforge on a store runs the brain, and every later one attaches to
// the journal as a surface and starts watching for the role to come free.
func (r *chatResidency) claim() error {
	release, heldBy, err := lease.AcquireResident(r.window.path, "chat")
	if err != nil {
		return err
	}
	if release == nil {
		host := strings.TrimSpace(heldBy.Host)
		if host == "" {
			host = "unknown host"
		}
		// The stderr line is kept for anyone running with the TUI disabled, but
		// it is no longer the only notice: bubbletea's alt screen swallows it
		// whole, which is precisely how a visitor came to look like a dead app.
		// The header says it too, and says it for as long as it is true.
		fmt.Fprintf(os.Stderr, "another aforge is resident (pid %d on %s); attaching as a visitor\n",
			heldBy.PID, host)
		return r.becomeVisitor(heldBy, true)
	}
	brain, err := r.build(r.window, r.window.session, r.handover)
	if err != nil {
		_ = release()
		return err
	}
	brain.start()
	r.installBrain(brain, release, false)
	return nil
}

// becomeVisitor builds the surface half of a window. open is true only the
// first time: a window that opened as a visitor announces its arrival and gets
// whatever the brief has for it, while one that has just stood down from the
// resident role has been present all along and has nothing to arrive at.
func (r *chatResidency) becomeVisitor(holder *lease.Resident, open bool) error {
	surface := resident.New(r.window.graph, nil, nil)
	session := r.currentSession()
	if err := surface.AttachSession(session); err != nil {
		return err
	}
	if open {
		if err := surface.SessionOpened(context.Background(), session, "tui", 0); err != nil {
			return err
		}
	}
	commander := newVisitorCommander(r.window.path, session, r.window.graph, surface.AttachSession)
	commander.SetResidency(r)
	r.installVisitor(surface, commander, holder, !open)
	return nil
}

// commander is the surface the TUI is started with. Everything after that
// arrives through Residency.
func (r *chatResidency) commander() tui.Commander {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.current
}

func (r *chatResidency) currentSession() string {
	if current := r.currentCommander(); current != nil {
		if session := strings.TrimSpace(current.Session()); session != "" {
			return session
		}
	}
	return r.window.session
}

func (r *chatResidency) currentCommander() *chatCommander {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.current
}

// Poll is what the surface asks on every cycle. It is a probe and a decision,
// never the work itself: acquiring a lease and building a brain both happen on
// goroutines this starts, so the poll that asked returns in the time one
// non-blocking flock takes.
func (r *chatResidency) Poll() (tui.Residency, tui.Commander) {
	handed := r.takeHanded()
	state, due := r.probeDue()
	if !due {
		return state, handed
	}
	holder, err := lease.ProbeResident(r.window.path)
	if err != nil {
		// A lock we cannot read is not a lock we may take. The window stays a
		// visitor and says so with whatever it last knew.
		log.Printf("note: could not read the resident lock: %v", err)
		return r.state(), handed
	}
	state, ask, promote, reclaim := r.observe(holder)
	if ask {
		asked := holder.PID
		guard.Go("chat/handover-request", func() { r.requestHandover(asked) })
	}
	if promote {
		guard.Go("chat/promote", r.promote)
	}
	if reclaim {
		guard.Go("chat/reclaim-lease", r.reclaimLease)
	}
	return state, handed
}

// takeHanded yields the replacement commander exactly once. The surface adopts
// what it is given, so handing the same one twice would rebuild every model
// cache in the window for nothing.
func (r *chatResidency) takeHanded() tui.Commander {
	r.mu.Lock()
	defer r.mu.Unlock()
	handed := r.handed
	r.handed = nil
	return handed
}

// probeDue reports what the window currently is and whether this cycle should
// go and ask the lock.
//
// A resident holding its own lease never asks — it is the answer. A resident
// serving beside a wedged holder keeps asking, because the flock is still
// naming a process that stopped working and every other window on the store
// reads that name; the moment the wedged process lets go, this one takes the
// lock so its own heartbeat is what they see.
func (r *chatResidency) probeDue() (tui.Residency, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || (r.resident && r.release != nil) {
		return tui.Residency{}, false
	}
	if r.promoting || time.Now().Before(r.nextProbe) {
		return r.stateLocked(), false
	}
	r.nextProbe = time.Now().Add(r.probe)
	return r.stateLocked(), true
}

func (r *chatResidency) state() tui.Residency {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stateLocked()
}

// observe records what the lock said and decides what follows from it: nothing,
// an ask, a promotion, or — for a resident serving without a lease — taking the
// lock now that it is finally free.
func (r *chatResidency) observe(holder *lease.Resident) (state tui.Residency, ask, promote, reclaim bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.resident {
		return tui.Residency{}, false, false, r.release == nil && holder == nil
	}
	if r.promoting {
		return r.stateLocked(), false, false, false
	}
	r.holder = holder
	// A note is about one process. When the lock changes hands — or comes free
	// — whatever it said stops being true of whoever is there now.
	if holder == nil || holder.PID != r.notePID {
		r.note, r.notePID = "", 0
	}
	// Nobody holds the flock, or the holder is alive and has stopped stamping
	// passes. Both mean the same thing to a surface waiting to be answered:
	// there is no brain, and this window can be one.
	free := holder == nil || holder.Stuck
	if !free && holder.PID != r.askedPID && r.stamp.NewerThan(holder.Build) {
		r.askedPID = holder.PID
		ask = true
		r.note, r.notePID = fmt.Sprintf("asking pid %d to hand over", holder.PID), holder.PID
	}
	if free {
		r.promoting, promote = true, true
		r.note, r.notePID = "taking over as resident", 0
	}
	return r.stateLocked(), ask, promote, false
}

func (r *chatResidency) stateLocked() tui.Residency {
	if r.resident {
		return tui.Residency{}
	}
	state := tui.Residency{Visitor: true, Note: r.note}
	if r.holder != nil {
		state.PID = r.holder.PID
	}
	return state
}

// reclaimLease takes the lock for a window that is already serving without it.
// Failing is ordinary — somebody else got there first, or the wedged holder is
// still wedged — and it fails silently, because nothing about the window the
// user is looking at changes either way.
func (r *chatResidency) reclaimLease() {
	release, _, err := lease.AcquireResident(r.window.path, "chat")
	if err != nil || release == nil {
		return
	}
	if !r.adoptLease(release) {
		_ = release()
	}
}

func (r *chatResidency) adoptLease(release func() error) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.resident || r.release != nil {
		return false
	}
	r.release = release
	return true
}

// requestHandover journals the ask. Whether it is honoured is entirely the
// resident's business: a build that knows the verb stands down, and one that
// predates it rejects the command in words — which is the honest outcome for
// exactly the case that produced this whole mechanism, a resident running a
// binary from before handover existed. That window is reclaimed the slow way,
// when its heartbeat goes stale, and the note stops promising otherwise.
func (r *chatResidency) requestHandover(pid int) {
	reason := fmt.Sprintf("pid %d opened this store with a newer build and is asking for the resident role",
		os.Getpid())
	command, err := r.window.graph.RequestCommand(store.Command{
		SessionID:   r.currentSession(),
		Kind:        store.CommandHandover,
		Instruction: reason,
	})
	if err != nil {
		log.Printf("note: could not ask pid %d to hand over: %v", pid, err)
		r.setNote("", 0)
		return
	}
	r.awaitHandover(command.Seq, pid)
}

// awaitHandover watches the one command it asked for and says what became of
// it. Refusal and silence are the same news to the person: the role is not
// moving on request, and the only thing that will move it now is the old window
// closing or its heartbeat going stale. That is worth one quiet phrase in the
// header, because it is also the one thing they can act on.
func (r *chatResidency) awaitHandover(seq int64, pid int) {
	refused := fmt.Sprintf("pid %d would not hand over", pid)
	deadline := time.Now().Add(handoverPatience)
	for time.Now().Before(deadline) {
		time.Sleep(residencyProbeEvery)
		if r.settled() {
			return
		}
		command, ok, err := r.window.graph.CommandBySeq(seq)
		if err != nil || !ok || command.Status == store.CommandPending {
			continue
		}
		if command.Status == store.CommandRejected {
			log.Printf("note: pid %d would not hand over the resident role: %s", pid, command.Result)
			r.setNote(refused, pid)
			return
		}
		// Applied: the old window is standing down and the lock is about to
		// come free. The next probe is what actually takes it.
		r.setNote("", 0)
		return
	}
	// Silence for the whole of the patience is the old-binary case: the command
	// sits pending because nothing in that process knows how to read it.
	log.Printf("note: pid %d never answered the handover request", pid)
	r.setNote(refused, pid)
}

// settled reports that the question the ask was about has already been answered
// some other way — this window took the role, or is taking it.
func (r *chatResidency) settled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.resident || r.promoting
}

func (r *chatResidency) setNote(note string, about int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.note, r.notePID = note, about
}

// promote takes the role. The flock is the arbiter and nothing else is: two
// visitors that both saw the role free both arrive here, one acquires and the
// other is handed the winner's identity and stays a surface pointed at it.
func (r *chatResidency) promote() {
	defer r.donePromoting()
	release, heldBy, err := lease.AcquireResident(r.window.path, "chat")
	if err != nil {
		log.Printf("note: could not take the resident role: %v", err)
		r.setNote("", 0)
		return
	}
	wedged := ""
	if release == nil {
		if heldBy == nil || !heldBy.Stuck {
			r.pointAt(heldBy)
			return
		}
		// The flock cannot be taken from a live process. A holder that has
		// stopped completing passes is not serving the role, only occupying
		// it, so the brain starts beside it and says so — the same judgement,
		// and the same sentence, a wake pass already makes about the same lock.
		wedged = fmt.Sprintf("pid %d still holds the resident lock but has not completed a pass since %s",
			heldBy.PID, heldBy.LastTick.Format(time.RFC3339))
	}
	session := r.currentSession()
	brain, err := r.build(r.window, session, r.handover)
	if err != nil {
		if release != nil {
			_ = release()
		}
		log.Printf("note: could not start the resident half after taking the role: %v", err)
		r.setNote("", 0)
		return
	}
	brain.start()
	if !r.installBrain(brain, release, true) {
		// The window closed while this was being built. Give back what was
		// taken rather than leaving a brain running over a store nobody is
		// looking at any more.
		brain.stop()
		if release != nil {
			_ = release()
		}
		return
	}
	line := "Taking over as resident — this window is running the brain now."
	if wedged != "" {
		line += " (" + wedged + ")"
	}
	r.say(session, line)
}

// promotingNow reports a promotion still running on its own goroutine.
func (r *chatResidency) promotingNow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.promoting
}

func (r *chatResidency) donePromoting() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.promoting = false
}

// pointAt is what losing the race looks like: another window acquired the lock
// first, so this one goes on being a surface and names the winner.
func (r *chatResidency) pointAt(holder *lease.Resident) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.holder, r.note, r.notePID, r.askedPID = holder, "", 0, 0
}

// installBrain makes this window the resident. hand is false only at launch,
// where the surface has not been built yet and will be handed this commander
// directly.
func (r *chatResidency) installBrain(brain *chatBrain, release func() error, hand bool) bool {
	// The commander the brain built has to be able to answer the surface's next
	// residency question, or a window that promotes can never hear that it has
	// been asked to stand down again.
	brain.commander.SetResidency(r)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	r.resident, r.brain, r.release = true, brain, release
	r.surface = nil
	r.current = brain.commander
	if hand {
		r.handed = brain.commander
	}
	r.holder, r.note, r.notePID, r.askedPID = nil, "", 0, 0
	return true
}

func (r *chatResidency) installVisitor(surface *resident.Reconciler, commander *chatCommander,
	holder *lease.Resident, hand bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.surface, r.current, r.holder = surface, commander, holder
	r.resident, r.brain, r.release = false, nil, nil
	r.askedPID, r.nextProbe = 0, time.Time{}
	r.note, r.notePID = "", 0
	if hand {
		r.handed = commander
	}
}

// handover is the seam the reconciler calls when another window asks for the
// role. It answers immediately and does the standing down on its own
// goroutine, so the pass that is still holding the command queue open finishes
// and settles this very command before anything is torn down.
func (r *chatResidency) handover(request resident.Handover) (bool, string) {
	if !r.serving() {
		return false, "this window is not the resident"
	}
	reason := firstLine(request.Reason)
	guard.Go("chat/stand-down", func() { r.demote(reason) })
	return true, "standing down as resident: " + reason
}

func (r *chatResidency) serving() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.resident
}

// demote gives the role up without taking the work down with it. The lease goes
// first so the asking window can promote immediately; the runner then stops
// claiming new leaves but keeps the ones it is already running, because the
// store owns their claims and a leaf killed mid-turn is work already paid for
// and thrown away.
func (r *chatResidency) demote(reason string) {
	brain, session, ok := r.giveUpRole()
	if !ok {
		return
	}
	if brain != nil {
		brain.standDown()
	}
	if err := r.becomeVisitor(nil, false); err != nil {
		log.Printf("note: could not settle into a visitor surface after handing over: %v", err)
	}
	line := "Handing the resident role to the newer window — this one is a second window now."
	if reason != "" {
		line = "Handing the resident role over: " + reason + ". This window is a second window now."
	}
	r.say(session, line)
}

// giveUpRole lets go of the lease inside the same critical section that stops
// this window being the resident. The two have to be one step: anything that
// reads "not the resident any more" and then reaches for the lock must find it
// free, or the window that was asked to take over is told the role is still
// taken by a process that has already given it up.
func (r *chatResidency) giveUpRole() (*chatBrain, string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.resident {
		return nil, "", false
	}
	brain := r.brain
	if r.release != nil {
		_ = r.release()
	}
	r.resident, r.brain, r.release = false, nil, nil
	session := r.window.session
	if r.current != nil {
		if current := strings.TrimSpace(r.current.Session()); current != "" {
			session = current
		}
	}
	return brain, session, true
}

// say posts one quiet line into the thread. A role change is a fact about the
// window rather than an answer to anything, so it is a system message and it is
// one sentence.
func (r *chatResidency) say(session, body string) {
	if _, err := thread.Post(r.window.graph, store.Message{
		SessionID: session, Role: store.RoleSystem, Body: body,
	}); err != nil {
		log.Printf("note: could not say the resident role moved: %v", err)
	}
}

func (r *chatResidency) sessionClosed() error {
	brain, surface := r.halves()
	if brain != nil {
		return brain.reconciler.SessionClosed(r.window.session, "tui")
	}
	if surface != nil {
		return surface.SessionClosed(r.window.session, "tui")
	}
	return nil
}

func (r *chatResidency) halves() (*chatBrain, *resident.Reconciler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.brain, r.surface
}

func (r *chatResidency) shutdown() (*chatBrain, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	if !r.resident {
		return nil, false
	}
	brain := r.brain
	if r.release != nil {
		_ = r.release()
	}
	r.resident, r.brain, r.release = false, nil, nil
	return brain, true
}

// stop is the ordinary shutdown, and it is idempotent: the window may already
// have handed the role away, in which case there is nothing left to release.
// It also closes the window to any promotion still in flight — a lease acquired
// a moment ago must not become a brain running over a store that is about to
// be closed.
func (r *chatResidency) stop() {
	brain, ok := r.shutdown()
	// A promotion may be halfway through building a brain over the store this
	// window is about to close. shutdown has already closed the window to it —
	// it will give back whatever it took — and this gives it a bounded moment
	// to notice, because the alternative is a goroutine still reading a
	// database on its way out.
	for giveUpAt := time.Now().Add(5 * time.Second); r.promotingNow() && time.Now().Before(giveUpAt); {
		time.Sleep(10 * time.Millisecond)
	}
	if !ok {
		return
	}
	if brain != nil {
		brain.stop()
	}
}
