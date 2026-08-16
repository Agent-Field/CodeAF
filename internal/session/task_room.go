package session

// The room: a task node as a PLACE the person can enter.
//
// Everything else in this slice treats a node as a thing that is handed off and
// reported on — the proposal, the countdown, the worktree, the report that
// arrives minutes later (task.go, task_run.go). That is the right shape for
// work you delegate and stop thinking about, and it is the wrong shape for the
// moment a person changes their mind: the node is running, they can see it is
// going the wrong way, and until now the only thing they could do about it was
// kill it and propose the corrected task again.
//
// A room is three doors on one running node, and no more:
//
//   - [Agent.WatchTask] — the LIVE stream of the child agent's own events, as
//     they happen: its deltas, its tool calls beginning and ending, its errors.
//   - [Agent.SteerTask] — the person's words into the child's steering lane,
//     the same lane a background job's exit note rides (agent.go).
//   - [Agent.TaskJournal] — the path to the node's whole transcript on disk.
//
// ── LIVE AND HISTORY ARE TWO LANES, FOR DECISION 19's OWN REASON ──
//
// The journal is the node's history and the room's stream is its present, and
// they are deliberately not the same door. A watcher subscribing to a running
// node gets what happens FROM NOW — nothing is replayed, exactly as
// [eventHub.subscribe] replays nothing, because a stream that re-narrated a
// half-hour of somebody else's greps before reaching the live edge would make
// "watch this node" mean "read this node's history slowly". A person who wants
// the history opens the journal, which is a real session file and has been
// since the first node ran. It is the same split as the two update lanes: the
// turn's hub carries what is happening, [Agent.TaskUpdates] carries what
// landed, and neither is asked to be the other.
//
// ── AND STEERING IS NOT A REDIRECT ──
//
// [TaskNode]'s goal contract is untouched by this file. `spec.brief` and
// `spec.acceptance` are frozen at admission and nothing here writes them: what
// the auditor grades the work against cannot move while the work runs, or the
// verification verifies nothing. Steering is a line of TALK to the worker —
// "the config lives under etc/, not conf/" — arriving in the transcript as the
// person's own user-role message, which is exactly what it looks like from the
// child's side. If the objective itself was wrong, the answer is still a new
// proposal, and that is the person exercising authority rather than editing a
// target mid-flight.

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

// SteerTask injects the person's words into a running node's loop — the same
// steering lane a job's exit note rides ([Agent.enqueueSteering]). Unknown id
// or a node that is not running is an error naming which.
//
// The note carries NO DECORATION. A job's exit is framed ("task 3 finished: …")
// because the model has to be told what kind of news it is; a person's line
// needs no frame, because from the child's side it is what it looks like — the
// person talking. Wrapping it would teach the node to read the person's words
// as a system event, which is the one thing they are not.
func (a *Agent) SteerTask(id uint64, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("nothing to say")
	}
	node := a.taskNode(id)
	if node == nil {
		return fmt.Errorf("no task %d in this session", id)
	}
	if state := node.stateNow(); state != TaskRunning {
		return fmt.Errorf("task %d is %s, not running", id, state)
	}
	child := node.openRoom().speaker()
	if child == nil {
		// Running, but the child is not up yet (its worktree is still being
		// prepared) or is already shutting down. Both are "there is nobody in
		// there to talk to", and both are worth saying rather than silently
		// dropping the person's line into a queue nothing will drain.
		return fmt.Errorf("task %d has no worker to talk to yet", id)
	}
	child.enqueueSteering(text)
	return nil
}

// WatchTask subscribes to one node's LIVE event stream: the child agent's own
// events — text deltas, tool begins and ends, errors — as they happen. The
// channel closes at the node's final state and on [Agent.Close]. Unknown id is
// an error.
//
// A FINISHED NODE ANSWERS WITH A CLOSED CHANNEL rather than an error: the id is
// real, the work is over, and a channel that closes immediately is how every
// other stream in this package says "that is the whole of it" ([eventHub.
// subscribe] does exactly this for a turn that has ended). The history is the
// journal; this door is only ever the present.
func (a *Agent) WatchTask(id uint64) (<-chan Event, error) {
	node := a.taskNode(id)
	if node == nil {
		return nil, fmt.Errorf("no task %d in this session", id)
	}
	a.mu.Lock()
	closed := a.closed
	a.mu.Unlock()
	if closed {
		return closedEventStream(), nil
	}
	return node.openRoom().join(), nil
}

// TaskJournal is the node's journal path — its whole transcript on disk — or ""
// for an unknown id.
//
// The path is recorded when the node's child agent is built, because that is
// where it is minted: [taskJournalPath] stamps the current time into the name,
// so recomputing it later would name a file nobody ever wrote. A node from a
// checkpoint of an earlier life answers "" — its journal is on disk under the
// same session directory, but this process never learned which file it is, and
// a guessed path is worse than none.
func (a *Agent) TaskJournal(id uint64) string {
	node := a.taskNode(id)
	if node == nil {
		return ""
	}
	return node.journalPath()
}

// taskNode finds one admitted node, without BUILDING a graph that a question
// about tasks does not need: a session that never groomed one answers every
// door in this file with "no such task", which is the truth.
func (a *Agent) taskNode(id uint64) *TaskNode {
	a.mu.Lock()
	graph := a.tasks
	a.mu.Unlock()
	if graph == nil {
		return nil
	}
	return graph.node(id)
}

// ── the room itself ─────────────────────────────────────────────────────────

// taskRoom is one running node's presence: the child agent that IS the node,
// and the live subscribers watching it work.
//
// It is opened by whoever arrives first — the runner attaching its child, or a
// person watching a node that has not started yet — and it closes exactly once,
// when the node reaches its final state ([TaskNode.openRoom] hangs that on the
// node's own done channel, so every path to a final state closes it: the
// runner's, the frontier's cascade, and a recovery's).
//
// The subscribers are [eventStream]s, the same unbounded queue every other
// fan-out in this package uses (agent.go's eventHub, [Agent.TaskUpdates]'s
// watchers): the producer here is the child's event loop, which must never wait
// on a surface that is redrawing, and must never drop a text delta, which is the
// one event whose loss reads as corruption rather than as lag.
type taskRoom struct {
	mu       sync.Mutex
	child    *Agent
	closed   bool
	watchers map[*eventStream]struct{}
	// live is the recorder every published event also goes through
	// (task_live.go): what the node is doing and the tail of what it has said,
	// kept for the reader who was NOT subscribed while it happened — the model,
	// asking after the fact. It is set once here and never reassigned, so it is
	// read without this lock, and it OUTLIVES the close: a node that landed a
	// second ago still answers with the last thing it was doing.
	live *taskLive
}

func newTaskRoom() *taskRoom {
	return &taskRoom{watchers: make(map[*eventStream]struct{}, 1), live: &taskLive{}}
}

// join returns a fresh channel carrying this node's events from now on. A room
// that has already closed answers with a channel that closes immediately,
// exactly as a subscription to a finished node does — the two are the same
// event arriving on either side of the close, and they must not be two
// behaviours.
//
// A nil room is a node with no room to enter: the same answer, so no caller has
// to check.
func (r *taskRoom) join() <-chan Event {
	stream := newEventStream()
	if r == nil {
		stream.close()
		return stream.out
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		stream.close()
		return stream.out
	}
	r.watchers[stream] = struct{}{}
	r.mu.Unlock()
	return stream.out
}

// speaking hands the room the agent the person will be talking to.
func (r *taskRoom) speaking(child *Agent) {
	if r == nil {
		return
	}
	r.mu.Lock()
	if !r.closed {
		r.child = child
	}
	r.mu.Unlock()
}

// speaker is who is in the room, or nil once the node is over. It is cleared at
// close so a line typed into a landed node's page is refused rather than queued
// onto an agent nothing will drain.
func (r *taskRoom) speaker() *Agent {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.child
}

// publish fans one of the child's events out to everyone watching. The lock is
// held across the fan-out for [eventHub.send]'s reason: a send is an append and
// a signal, never a wait, so every watcher sees the same events in the same
// order and one arriving mid-fan-out lands cleanly before or after this event
// rather than inside it.
//
// IT IS ALSO WHERE THE EVENT IS RECORDED (task_live.go). This is the one funnel
// the child's whole narrative passes through, so it is the one place a
// recording can be in the order the work happened; a second tap on the stream
// would be a second ordering of the same events, disagreeing exactly under the
// load that makes the question worth asking.
func (r *taskRoom) publish(event Event) {
	if r == nil {
		return
	}
	r.live.record(event)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	for stream := range r.watchers {
		stream.send(event)
	}
}

// close ends every watcher's channel and empties the room. It is idempotent —
// the node's done channel is closed once, but a room may also be closed by a
// caller that got there first — and every subscriber added after it gets a
// closed channel from join.
func (r *taskRoom) close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	watchers := r.watchers
	r.watchers = nil
	r.child = nil
	r.mu.Unlock()
	for stream := range watchers {
		stream.close()
	}
}

// closedEventStream is an already-ended channel: what a door answers when the
// thing behind it is over.
func closedEventStream() <-chan Event {
	stream := newEventStream()
	stream.close()
	return stream.out
}

// settled says a node's life is over — nothing more will happen in its room.
func (s TaskState) settled() bool {
	return s == TaskDone || s == TaskFailed
}
