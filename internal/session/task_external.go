package session

// WAITING ON A TASK OF ANOTHER CONVERSATION.
//
// The task graph's frontier has always honoured `depends_on`, but only between
// the tasks of ONE conversation: a node waits, queued, until every id it names
// is done, and fails with the cascade when one fails. A graph that spans
// conversations (a crew whose members hand work to each other) had no such
// command, so nothing could be said to wait on another member's task except in
// prose. This file adds the command, at the seam the frontier already is:
//
//   - [Agent.WaitOnTask] records, on a QUEUED node, that it waits on one task of
//     another conversation. The link is persisted in the node's own record
//     (taskRecord.WaitsExternal), so it survives a reopen.
//   - [TaskGraph.readinessLocked] enforces it exactly as it enforces depends_on:
//     the prerequisite done lets the node start, failed fails the dependent with
//     a plain reason, anything else (running, queued, unverified, unreadable)
//     keeps it waiting. A node never starts on a guess.
//
// ── NO IO UNDER A LOCK, EVER ──
//
// Another conversation's tasks are read through [Config.ExternalTasks], which
// opens files. [TaskGraph.readinessLocked] runs with the graph's lock held, and
// the graph's lock is what stop, steer and every other control takes, so a
// reader that blocked there would freeze the controls of a conversation over a
// disk it does not own. So the frontier never calls the reader. It consults an
// OBSERVATION — what the reader last said about each target, held in memory —
// and a target nobody has observed yet is simply "not done". After the lock is
// released the pass hands the targets that are still pending to ONE refresh,
// which asks the reader on its own goroutine under its own deadline, stores the
// answers under the lock for an instant, and turns the frontier again.
//
// THE REFRESH IS BOUNDED. At most one is in flight; it asks for at most
// [externalRefreshMax] targets, each under [externalAskTimeout]; and it
// re-arms itself only while some node is still pending on an unfinished target,
// so a graph with no external waits does no reading at all and a graph whose
// waits have resolved stops polling.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
	"unicode/utf8"
)

// TaskRef names one task of one conversation. Session is the conversation's id
// (its folder's name) and ID the conversation's own number for the task.
type TaskRef struct {
	Session string
	ID      uint64
}

// String is the ref's wire spelling, "<session>:<id>", the same as the app's
// task ids.
func (r TaskRef) String() string { return fmt.Sprintf("%s:%d", r.Session, r.ID) }

// maxSessionIDBytes bounds a conversation id; today's are 16 hex digits.
const maxSessionIDBytes = 128

// validSessionID is the law the app applies before it joins an id to a folder:
// ONE path component, bounded, printable, with no separator, colon or NUL.
func validSessionID(s string) bool {
	if s == "" || len(s) > maxSessionIDBytes || s[0] == '.' || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || r == '/' || r == '\\' || r == ':' || r == utf8.RuneError {
			return false
		}
	}
	return true
}

// Valid reports whether the ref names something safe to look up.
func (r TaskRef) Valid() bool { return validSessionID(r.Session) && r.ID != 0 }

// ExternalTask is what the reader says about one task of another conversation.
// Exists false with a nil error means the conversation was read and has no such
// task; an error means it could not be read, which says nothing about the task.
type ExternalTask struct {
	Exists bool
	State  TaskState
	// Waits are the tasks this one waits on, in any conversation, so that a new
	// link can be refused when it would close a loop. Same-conversation
	// prerequisites are included, spelled with this task's own session.
	Waits []TaskRef
}

// ExternalTasks reads one task of another conversation. It may do IO and may
// block; the engine only ever calls it from a goroutine holding no lock, under
// a deadline, and treats an error as "not known yet".
type ExternalTasks func(ctx context.Context, ref TaskRef) (ExternalTask, error)

// The typed refusals of [Agent.WaitOnTask].
var (
	ErrWaitNotQueued   = errors.New("that task has already started, so it can no longer be told to wait")
	ErrWaitSameSession = errors.New("that task is in the same conversation: use depends_on")
	ErrWaitSelf        = errors.New("a task cannot wait on itself")
	ErrWaitMissing     = errors.New("the task to wait on does not exist")
	ErrWaitDuplicate   = errors.New("that wait is already recorded")
	ErrWaitCycle       = errors.New("that wait would close a loop of tasks waiting on each other")
	ErrWaitAbsent      = errors.New("this process cannot read other conversations' tasks, so cross-conversation waits are not available")
	ErrWaitUnreadable  = errors.New("the task to wait on could not be read right now")
	ErrWaitInvalid     = errors.New("that is not a valid task reference")
	ErrWaitNoTask      = errors.New("this conversation has no such task")
)

// taskWaitRecord is one external wait on disk.
type taskWaitRecord struct {
	Session string `json:"session"`
	Task    uint64 `json:"task"`
}

func waitRecords(refs []TaskRef) []taskWaitRecord {
	if len(refs) == 0 {
		return nil
	}
	out := make([]taskWaitRecord, len(refs))
	for i, r := range refs {
		out[i] = taskWaitRecord{Session: r.Session, Task: r.ID}
	}
	return out
}

// waitRefs reads the records back, dropping any that could not name a task
// safely: a hand-edited file never makes the frontier open an unsafe path.
func waitRefs(records []taskWaitRecord) []TaskRef {
	var out []TaskRef
	for _, r := range records {
		ref := TaskRef{Session: r.Session, ID: r.Task}
		if ref.Valid() {
			out = append(out, ref)
		}
	}
	return out
}

// setStateLocked moves the node to a state and stamps the instant it entered
// it. The graph's lock is held.
func (n *TaskNode) setStateLocked(state TaskState) {
	n.state = state
	n.stateAt = time.Now()
}

// The refresh's bounds.
const (
	// externalRefreshMax is the most targets one refresh asks about.
	externalRefreshMax = 64
	// externalAskTimeout is how long the reader gets for one target.
	externalAskTimeout = 3 * time.Second
	// externalPoll is how often a still-pending wait is looked at again.
	externalPoll = 2 * time.Second
	// externalWalkMax bounds the loop check's walk, in tasks read.
	externalWalkMax = 256
)

// externalLinks serialises the WRITING of cross-conversation links within this
// process, so that two conversations cannot each pass the loop check and then
// both commit the two halves of a loop. It guards no state the frontier reads.
var externalLinks sync.Mutex

// externalState is a graph's view of the tasks its nodes wait on elsewhere. All
// of it is guarded by the graph's lock.
type externalState struct {
	// seen is the reader's last answer per target.
	seen map[TaskRef]ExternalTask
	// refreshing says one refresh is in flight; armed that a timer for the next
	// is set. Either keeps a second from starting.
	refreshing bool
	armed      bool
	// every is the poll period (0 is [externalPoll]); a field for the tests.
	every time.Duration
}

// externalVerdict is what the frontier makes of one node's external waits:
// whether any is unfinished and, if one failed, the reason. It reads the
// observation only.
func (g *TaskGraph) externalVerdict(node *TaskNode) (ready bool, blocked string) {
	ready = true
	for _, ref := range node.waitsExt {
		seen, ok := g.ext.seen[ref]
		switch {
		case !ok:
			ready = false
		case !seen.Exists:
			return false, fmt.Sprintf("it waits on task %s, which is not in that conversation's work", ref)
		case seen.State == TaskDone:
		case seen.State == TaskFailed:
			return false, fmt.Sprintf("it waits on task %s, which did not finish", ref)
		default:
			ready = false
		}
	}
	return ready, ""
}

// pendingExternalLocked lists the targets some queued node still waits on that
// are not known to be done, nearest-first and bounded.
func (g *TaskGraph) pendingExternalLocked() []TaskRef {
	var out []TaskRef
	seen := map[TaskRef]bool{}
	for _, id := range g.order {
		node := g.nodes[id]
		if node == nil || node.state != TaskQueued {
			continue
		}
		for _, ref := range node.waitsExt {
			if seen[ref] || (g.ext.seen[ref].Exists && g.ext.seen[ref].State == TaskDone) {
				continue
			}
			seen[ref] = true
			out = append(out, ref)
			if len(out) == externalRefreshMax {
				return out
			}
		}
	}
	return out
}

// externalReader is the reader this graph asks, or nil.
func (g *TaskGraph) externalReader() ExternalTasks {
	if g.home == nil {
		return nil
	}
	return g.home.config.ExternalTasks
}

// armExternal starts the one refresh the pass asked for, if there is something
// to refresh and none is running. It takes the lock only to decide and never
// calls the reader there.
func (g *TaskGraph) armExternal() {
	reader := g.externalReader()
	g.mu.Lock()
	if g.quitting || g.ext.refreshing || g.ext.armed {
		g.mu.Unlock()
		return
	}
	pending := g.pendingExternalLocked()
	if len(pending) == 0 || reader == nil {
		g.mu.Unlock()
		return
	}
	g.ext.refreshing = true
	g.mu.Unlock()
	go g.refreshExternal(reader, pending)
}

// refreshExternal asks the reader about each pending target with NO LOCK HELD,
// publishes the answers, turns the frontier, and re-arms only while something
// is still waiting.
func (g *TaskGraph) refreshExternal(reader ExternalTasks, pending []TaskRef) {
	answers := make(map[TaskRef]ExternalTask, len(pending))
	for _, ref := range pending {
		ctx, cancel := context.WithTimeout(g.externalContext(), externalAskTimeout)
		got, err := reader(ctx, ref)
		cancel()
		if err != nil {
			// Unreadable says nothing about the task: keep the old answer.
			continue
		}
		answers[ref] = got
	}
	g.mu.Lock()
	if g.ext.seen == nil {
		g.ext.seen = map[TaskRef]ExternalTask{}
	}
	for ref, got := range answers {
		g.ext.seen[ref] = got
	}
	g.ext.refreshing = false
	again := !g.quitting && len(g.pendingExternalLocked()) > 0
	every := g.ext.every
	if again {
		g.ext.armed = true
	}
	g.mu.Unlock()
	g.runFrontier()
	if !again {
		return
	}
	if every <= 0 {
		every = externalPoll
	}
	time.AfterFunc(every, func() {
		g.mu.Lock()
		g.ext.armed = false
		g.mu.Unlock()
		g.armExternal()
	})
}

// externalContext is the context a refresh's reads are cancelled by when the
// session quits.
func (g *TaskGraph) externalContext() context.Context {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.extCtx == nil {
		g.extCtx, g.extCancel = context.WithCancel(context.Background())
		if g.quitting {
			g.extCancel()
		}
	}
	return g.extCtx
}

// ownWaitsLocked is this conversation's own waits, same-conversation edges
// included, for the loop check's walk.
func (g *TaskGraph) ownWaitsLocked(session string) map[uint64][]TaskRef {
	out := make(map[uint64][]TaskRef, len(g.nodes))
	for id, node := range g.nodes {
		var waits []TaskRef
		for _, dep := range node.dependsOn {
			waits = append(waits, TaskRef{Session: session, ID: dep})
		}
		waits = append(waits, node.waitsExt...)
		if len(waits) > 0 {
			out[id] = waits
		}
	}
	return out
}

// WaitOnTask records that this conversation's queued task id waits on `on`, a
// task of ANOTHER conversation, and persists the link so it survives a reopen.
// The frontier then enforces it: the task does not start until `on` is done,
// and fails if `on` fails.
//
// It refuses, with a typed error, when the task is not queued (a started task
// cannot be re-scoped), when `on` is the task itself or in this conversation
// (use depends_on), when `on` does not exist, when the link already exists,
// when it would close a loop across conversations, and when this process
// cannot read other conversations' tasks — the capability is absent, not
// broken. Every read happens with no lock held.
func (a *Agent) WaitOnTask(ctx context.Context, id uint64, on TaskRef) error {
	self := a.config.Place.ID()
	reader, err := a.refuseWaitTarget(self, on, id)
	if err != nil {
		return err
	}
	g := a.tasker()
	if g == nil {
		return ErrWaitNoTask
	}
	if err := g.checkWaitable(id, on); err != nil {
		return err
	}
	// ALL IO FIRST, with no lock of the conversation's held: does the target
	// exist, and would the link close a loop.
	externalLinks.Lock()
	defer externalLinks.Unlock()
	target, err := reader(ctx, on)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWaitUnreadable, err)
	}
	if !target.Exists {
		return ErrWaitMissing
	}
	g.mu.Lock()
	own := g.ownWaitsLocked(self)
	g.mu.Unlock()
	if err := walkForLoop(ctx, reader, TaskRef{Session: self, ID: id}, on, own, self); err != nil {
		return err
	}
	return g.commitWait(id, on, target)
}

// refuseWaitTarget is the refusals that need no reading: a reference that is
// not safe, one that names this conversation or the task itself, and a process
// with no reader. It answers the reader the rest of the call uses.
func (a *Agent) refuseWaitTarget(self string, on TaskRef, id uint64) (ExternalTasks, error) {
	switch {
	case !on.Valid():
		return nil, ErrWaitInvalid
	case self != "" && on.Session == self && on.ID == id:
		return nil, ErrWaitSelf
	case self != "" && on.Session == self:
		return nil, ErrWaitSameSession
	case a.config.ExternalTasks == nil:
		return nil, ErrWaitAbsent
	}
	return a.config.ExternalTasks, nil
}

// commitWait REVALIDATES AND COMMITS under the lock, and writes the file before
// answering: a link acknowledged is a link on the disk. A write that fails
// takes the link back off the record, so the record and the disk agree.
func (g *TaskGraph) commitWait(id uint64, on TaskRef, target ExternalTask) error {
	g.mu.Lock()
	if err := g.checkWaitableLocked(id, on); err != nil {
		g.mu.Unlock()
		return err
	}
	node := g.nodes[id]
	node.waitsExt = append(append([]TaskRef(nil), node.waitsExt...), on)
	node.app.Version++
	if g.ext.seen == nil {
		g.ext.seen = map[TaskRef]ExternalTask{}
	}
	g.ext.seen[on] = target
	store := g.store
	g.mu.Unlock()
	if err := store.write(g); err != nil && !errors.Is(err, errStoreClosed) {
		g.mu.Lock()
		if n := g.nodes[id]; n != nil {
			n.waitsExt = withoutRef(n.waitsExt, on)
		}
		g.mu.Unlock()
		return err
	}
	g.runFrontier()
	return nil
}

func withoutRef(refs []TaskRef, drop TaskRef) []TaskRef {
	out := make([]TaskRef, 0, len(refs))
	for _, r := range refs {
		if r != drop {
			out = append(out, r)
		}
	}
	return out
}

func (g *TaskGraph) checkWaitable(id uint64, on TaskRef) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.checkWaitableLocked(id, on)
}

func (g *TaskGraph) checkWaitableLocked(id uint64, on TaskRef) error {
	node := g.nodes[id]
	if node == nil || g.deleted[id] {
		return ErrWaitNoTask
	}
	if node.state != TaskQueued || !node.started.IsZero() {
		return ErrWaitNotQueued
	}
	for _, ref := range node.waitsExt {
		if ref == on {
			return ErrWaitDuplicate
		}
	}
	return nil
}

// walkForLoop reports ErrWaitCycle when `from` is reachable from `start`
// through stored waits: adding from→start would close a loop. The walk reads
// other conversations through the reader and this conversation's own tasks from
// the snapshot, visits each task once and stops after [externalWalkMax] reads.
func walkForLoop(ctx context.Context, reader ExternalTasks, from, start TaskRef, own map[uint64][]TaskRef, self string) error {
	seen := map[TaskRef]bool{start: true}
	queue := []TaskRef{start}
	reads := 0
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur == from {
			return ErrWaitCycle
		}
		var next []TaskRef
		if cur.Session == self {
			next = own[cur.ID]
		} else {
			if reads++; reads > externalWalkMax {
				return fmt.Errorf("%w: the chain of waits is longer than %d tasks", ErrWaitUnreadable, externalWalkMax)
			}
			got, err := reader(ctx, cur)
			if err != nil {
				return fmt.Errorf("%w: %v", ErrWaitUnreadable, err)
			}
			next = got.Waits
		}
		for _, n := range next {
			if !seen[n] && n.Valid() {
				seen[n] = true
				queue = append(queue, n)
			}
		}
	}
	return nil
}
