package session

// One door for every message handed to a conversation, and one answer about
// what happened to it.
//
// A task is an addressable conversation: an assignment, an owner, and a queue
// somebody drains. The main chat is the same shape with different
// responsibilities. Four callers already put words on one of those queues — a
// person's line steered into a node's room ([Agent.SteerTask]), a landed node's
// report ([Agent.deliverTaskNote]), a background job's ending, a watch's tick —
// and each picked its own reader, its own idea of "was it taken", and its own
// answer when nobody was there. Two of them were wrong in the same way: the
// reader was chosen at one instant and handed the message at another, so a
// worker that stopped reading in between swallowed the message while the caller
// wrote down that it had been delivered.
//
// What lives here is the delivery and nothing else: who is addressed, what kind
// of message it is, who is speaking, and one hand-over that resolves the live
// reader and appends UNDER THE SAME LOCK — so "accepted" means a reader that
// was still listening took it. Scheduling, admission, assignment state, source
// control and everything a surface draws stay where they are.
//
// IT IS LOCAL AND DELIBERATELY SMALL. [conversationID] carries a session
// because a task number is minted per session and repeats across them, so a
// number alone stops meaning anything the moment two transcripts are read
// together. A later cross-session router would resolve one to a mailbox the
// same way this file resolves the local ones, and would additionally need
// authenticated authority, admission policy and replay protection. None of that
// is here, implied, or usable yet: there is no discovery, no registry, no bus.
//
// WHAT IT DOES NOT CARRY YET, stated so nobody reads more into a receipt than
// it says: an assignment revision. A person's correction and a worker's
// finalization remain two independent races on one node — the correction is
// accepted here and read at the worker's next step boundary, and a worker that
// lands in between finishes against the older request. A receipt says the words
// reached a live reader. It does not say the work has been re-aimed.

import "fmt"

// conversationID addresses one conversation this session can deliver to: the
// main chat, which is task 0, or a task room.
type conversationID struct {
	session string
	task    uint64
}

func (c conversationID) String() string {
	if c.task == 0 {
		return "session " + c.sessionName() + " main"
	}
	return fmt.Sprintf("session %s task %d", c.sessionName(), c.task)
}

func (c conversationID) sessionName() string {
	if c.session == "" {
		return "unfiled"
	}
	return c.session
}

// messageOrigin is WHO IS SPEAKING, and it is the fact a recipient cannot infer
// from the words. A person at this keyboard, another agent in this session, and
// the runtime's own account of a state change all arrive on one queue as
// user-role text, and only the origin tells them apart again where it matters:
// whether the line is journaled as the person's own correction, whether the
// session's folder learns the person was here, and whether the recipient may
// read it as authority over its own assignment.
//
// NO ORIGIN GRANTS ANYTHING BY ITSELF. It is the honest label on a message, and
// the policies that read it live with those policies. A descendant cannot raise
// its own authority by writing a sentence that sounds like the person.
type messageOrigin uint8

const (
	// fromRuntime is the program's own account of something that happened: a
	// job's ending, a node landing, a watch's tick. Nobody typed it.
	fromRuntime messageOrigin = iota
	// fromPerson is the authenticated person at this keyboard, reaching a
	// conversation through a surface door.
	fromPerson
	// fromAgent is another conversation in this session speaking through a
	// tool — the model in the main chat, or a parent task, talking to a worker.
	// It is coordination, and it is not the person however it is phrased.
	fromAgent
)

// messageKind is the small set of real differences between messages, and it
// decides which queue a message lands on and whether it may start a turn.
//
// The kinds absent from it are absent on purpose. A work request goes through
// admission (task.go) and is not a message; a question and its answer ride the
// result road today, correlated by the node they are about.
type messageKind uint8

const (
	// msgNotice is a typed state change: real, carried, read by whatever the
	// recipient does next, and never a person speaking.
	msgNotice messageKind = iota
	// msgDirection is words aimed at a running conversation — a correction, an
	// instruction, coordination. Its authority is its origin's, not its kind's.
	msgDirection
	// msgResult is a durable answer owed to whoever asked for the work.
	msgResult
	// msgProgress is a replaceable observation for people. It is coalesced, it
	// never wakes a model, and it never becomes an established fact.
	msgProgress
)

// delivery is one message on its way to a conversation. The body is already the
// queue's own shape, because the queue is where it is going and a second
// envelope around it would be one more thing to keep in step.
type delivery struct {
	origin messageOrigin
	kind   messageKind
	note   userMessage
}

// deliveryState is what became of a delivery, and the three answers are
// genuinely different things to tell a caller.
type deliveryState uint8

const (
	// deliveryNobody: there is no reader in that conversation right now. A node
	// mid-check, a room whose worker has finished reading, a settled node.
	deliveryNobody deliveryState = iota
	// deliveryClosed: a reader was addressed and cannot take anything any more.
	// Nothing drains a closed agent's queue, so this is a refusal and never a
	// silent drop.
	deliveryClosed
	// deliveryAccepted: a live reader has the message on its queue. It does not
	// say the message has been read.
	deliveryAccepted
)

// deliveryReceipt is the one answer a caller gets. Accepted is not read, and no
// state here claims an assignment was changed.
type deliveryReceipt struct {
	to    conversationID
	state deliveryState
	// reader is the agent that actually took the message, and it is nil for
	// every other outcome. It is a pointer because a local delivery owes the
	// SAME agent two further writes — the news count and the wake
	// ([Agent.handOverTaskNews]) — and handing the caller an address it would
	// have to resolve again is the race this file exists to close. A
	// cross-session delivery would settle those on the recipient's own side.
	reader *Agent
}

func (r deliveryReceipt) accepted() bool { return r.state == deliveryAccepted }

// mailbox is a conversation that can be handed a message. Both implementations
// are local: an agent, and the seat inside a task room.
type mailbox interface {
	address() conversationID
	accept(delivery) deliveryReceipt
}

// deliverTo hands one message to the first mailbox that takes it, and answers
// with what actually happened rather than with what was attempted.
//
// THE ORDER IS THE CALLER'S POLICY. A landed node's report tries the parent's
// worker and then the person's conversation, because news with nowhere to go
// belongs in front of a person; a person's line into a room has ONE mailbox and
// falls back to nobody, because redirecting somebody's words to a reader they
// did not address is worse than telling them it could not be said.
func deliverTo(message delivery, boxes ...mailbox) deliveryReceipt {
	answer := deliveryReceipt{state: deliveryNobody}
	for _, box := range boxes {
		if box == nil {
			continue
		}
		answer = box.accept(message)
		if answer.accepted() {
			return answer
		}
	}
	return answer
}

// address is this agent's own conversation identity. The session is read from
// the journal the same way [TaskGraph.sessionName] reads it — empty when there
// is no file rather than the word "unfiled", which is a rendering and belongs
// with the rendering ([conversationID.sessionName]) — so an agent and the graph
// it belongs to cannot disagree about which session this is.
func (a *Agent) address() conversationID {
	a.mu.Lock()
	defer a.mu.Unlock()
	id := ""
	if a.file != nil {
		id = a.file.ID()
	}
	return conversationID{session: id, task: a.config.taskID}
}

// accept puts one message on this agent's queue and answers whether a reader
// that can still drain it took it.
//
// THE KIND CHOOSES THE QUEUE AND NOTHING ELSE DOES. Progress goes on the
// ambient queue, which no step drain takes and no wake reads, so telemetry
// cannot start a turn however it was addressed ([Agent.enqueueAmbient]).
// Everything else goes on the steering queue with the marks its own constructor
// put on it: the wake, the release of a parked runner and the journal lane are
// facts about the message, decided where the message was built.
func (a *Agent) accept(message delivery) deliveryReceipt {
	at := a.address()
	taken := false
	if message.kind == msgProgress {
		taken = a.enqueueAmbient(message.note)
	} else {
		taken = a.enqueueNote(message.note)
	}
	if !taken {
		return deliveryReceipt{to: at, state: deliveryClosed}
	}
	return deliveryReceipt{to: at, state: deliveryAccepted, reader: a}
}

// roomSeat is whoever is standing in a task room right now. It is a mailbox
// rather than an agent because the reader inside a room is not a fixed thing:
// the runner withdraws it the moment its reading is over (task_child_run.go),
// and a caller holding a pointer it read a moment ago is holding a reader that
// may already have stopped listening.
type roomSeat struct {
	at   conversationID
	room *taskRoom
}

func (s roomSeat) address() conversationID { return s.at }

func (s roomSeat) accept(message delivery) deliveryReceipt {
	return s.room.handIn(s.at, message)
}

// handIn resolves the live reader and appends UNDER ONE HOLD of the room's
// lock. The runner's withdrawal takes the same lock, so the two cannot
// interleave into a swallow: either the message lands while the seat is still
// filled — and then it lands before the runner's final queue check, which
// answers it with one more turn — or the withdrawal won and this refuses, words
// kept and the caller free to put them somewhere they will be read.
//
// Split across two locks it was a race with a narrower window, not a fix: that
// is #273's finding for a person's steered line, and a landed sub-task's report
// took the same road with no room lock at all.
func (r *taskRoom) handIn(at conversationID, message delivery) deliveryReceipt {
	if r == nil {
		return deliveryReceipt{to: at, state: deliveryNobody}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.child == nil {
		return deliveryReceipt{to: at, state: deliveryNobody}
	}
	// The child's own address is taken from the child, so a receipt names the
	// conversation that really took the message rather than the one this seat
	// was built for.
	return r.child.accept(message)
}

// conversationOf is a node's address, session and all.
func conversationOf(node *TaskNode) conversationID {
	if node == nil {
		return conversationID{}
	}
	return conversationID{session: node.graph.sessionName(), task: node.id}
}

// sessionName is the id of the session this graph belongs to, and empty for a
// graph built with no conversation behind it — every test that assembles one by
// hand. It is read without the graph's lock because it asks the home agent, and
// a delivery must never take an agent's lock under the graph's.
func (g *TaskGraph) sessionName() string {
	if g == nil || g.home == nil {
		return ""
	}
	return g.home.journalID()
}
