package session

// One door for every message handed to a conversation, and one answer about
// what happened to it.
//
// A task is an addressable conversation: an assignment, an owner, and a queue
// somebody drains; the main chat is the same shape with different
// responsibilities. The callers that already shared that path — a person's line
// into a room ([Agent.SteerTask]), the model's ([Agent.relayToTask]), a landed
// node's report ([Agent.deliverTaskNote]), a watch's tick — each used to pick
// its own reader, judge for itself whether it had been taken, and invent its own
// answer when nobody was there.
//
// What lives here is the delivery: who is addressed, what kind of message it is,
// who is speaking, and one hand-over that resolves the live reader and appends
// under the same lock. Scheduling, admission, assignment state and everything a
// surface draws stay where they are.
//
// Two invariants the rest of the package relies on:
//
//   - ACCEPTED IS NOT READ. A receipt says a reader that was still listening has
//     the message on its queue, and nothing more.
//   - THE ORIGIN IS NOT THE KIND. Delivery mechanics are shared; authorship is
//     not. Whether the words are journaled as the person's correction, whether
//     the folder records that the person spoke, and whether the recipient may
//     read the message as authority all follow [messageOrigin].
//
// It is local. [conversationID] carries a session because a task number is
// minted per session and repeats across them; a later cross-session router would
// resolve one to a mailbox the same way this file resolves the local ones, and
// would additionally need authenticated authority, admission policy and replay
// protection. None of that is here or implied: no discovery, no registry, no bus.
//
// AND NO ASSIGNMENT REVISION TRAVELS WITH A DELIVERY YET. A correction and a
// worker's finalization remain two independent races on one node: a worker that
// lands before reading an accepted correction finishes against the older
// request. Accepted means a live reader has the words, not that the work has
// been re-aimed.

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

// sessionName is the id as a person reads it. The empty session is a
// conversation with no journal, and "unfiled" is this file's word for it.
func (c conversationID) sessionName() string {
	if c.session == "" {
		return "unfiled"
	}
	return c.session
}

// messageOrigin is who is speaking, which a recipient cannot infer from the
// words: a person, another conversation in this session, and the runtime all
// arrive on one queue as user-role text. No origin grants anything by itself —
// it is the honest label, and the policies that read it live with those
// policies. A descendant cannot raise its own authority by writing a sentence
// that sounds like the person.
type messageOrigin uint8

const (
	// fromRuntime is the program's own account of something that happened: a
	// job's ending, a node landing, a watch's tick. Nobody typed it.
	fromRuntime messageOrigin = iota
	// fromPerson is the authenticated person at this keyboard, through a surface
	// door.
	fromPerson
	// fromAgent is another conversation in this session speaking through a tool.
	// It is coordination, and it is not the person however it is phrased.
	fromAgent
)

// messageKind is the small set of real differences between messages, and it
// decides which queue a message lands on and whether it may start a turn. The
// kinds absent from it are absent on purpose: a work request goes through
// admission (task.go) and is not a message, and a question and its answer ride
// the result road today, correlated by the node they are about.
type messageKind uint8

const (
	// msgNotice is a typed state change: real, carried, read by whatever the
	// recipient does next, and never a person speaking.
	msgNotice messageKind = iota
	// msgDirection is words aimed at a running conversation. Its authority is its
	// origin's, not its kind's.
	msgDirection
	// msgResult is a durable answer owed to whoever asked for the work.
	msgResult
	// msgProgress is a replaceable observation for people: coalesced, never a
	// wake, never an established fact.
	msgProgress
)

// delivery is one message on its way to a conversation. The body is already the
// queue's own shape, because the queue is where it is going.
type delivery struct {
	origin messageOrigin
	kind   messageKind
	note   userMessage
}

// deliveryState is what became of a delivery. The three answers are different
// things to tell a caller, and a caller that treats any of them as "sent" loses
// messages.
type deliveryState uint8

const (
	// deliveryNobody: no reader in that conversation right now — a node
	// mid-check, a room whose worker has finished reading, a settled node.
	deliveryNobody deliveryState = iota
	// deliveryClosed: a reader was addressed and can take nothing more. Nothing
	// drains a closed agent's queue, so this is a refusal and never a drop.
	deliveryClosed
	// deliveryAccepted: a live reader has it on its queue. Not read.
	deliveryAccepted
)

// deliveryReceipt is the one answer a caller gets.
type deliveryReceipt struct {
	to    conversationID
	state deliveryState
	// reader is the agent that took the message, and nil for every other
	// outcome. It is a pointer because a local delivery owes the SAME agent two
	// further writes — the news count and the wake ([Agent.handOverTaskNews]) —
	// and resolving an address a second time is the race this file closes. A
	// cross-session delivery would settle those on the recipient's own side.
	reader *Agent
}

func (r deliveryReceipt) accepted() bool { return r.state == deliveryAccepted }

// deliveryID names one durable delivery: which conversation the news is about,
// which life of that work, and which ending. Every part of it is on the task
// checkpoint, so the same landing composed again after a restart carries the
// same id and a replay can be told from a second event.
type deliveryID string

// durableDelivery is a message whose sender is owed an answer about the
// RECIPIENT'S RECORD rather than about its queue.
//
// THE QUEUE IS NOT THE RECORD, and the difference is a lost report. A note
// accepted onto an idle conversation's queue is read at that conversation's next
// step boundary, which on an unattended session may never come: the wake
// declines with nobody there ([Agent.wakeLocked]), the reaper closes the session
// half an hour after the terminal detached, and the queue goes with the process.
// Marked delivered at the queue, that landing was also marked announced on the
// checkpoint, so the next life of the session did not re-tell it either.
//
// So the sender is told twice, and the two facts are different: `accepted` when
// a live reader took it ([deliveryReceipt]), and `settled` when the recipient's
// own record holds it ([Agent.recordUserLocked]). Only the second is written
// down as announced.
//
// THE ID IS WRITTEN INTO THE RECORD, so a replay is checked against the record
// rather than against a checkpoint that may not have been written: a resume
// asks the recipient's journal whether it already holds this delivery before
// telling the landing again (task_store.go, [sessionFile.recorded]).
//
// IT IS STILL NOT EXACTLY ONCE AND DOES NOT CLAIM TO BE. The dedupe covers what
// the journal holds; a note recorded only in memory — a session with no journal
// — or a journal line that never reached the disk is told again on resume, which
// is the direction this must fail in. And nothing here makes an external effect
// idempotent: a task that already sent an email has sent it.
type durableDelivery struct {
	id      deliveryID
	settled func()
}

// deliveryIDs is the ids of a message's durable deliveries, for the record that
// is about to hold them.
func deliveryIDs(carried []durableDelivery) []deliveryID {
	var ids []deliveryID
	for _, delivery := range carried {
		if delivery.id != "" {
			ids = append(ids, delivery.id)
		}
	}
	return ids
}

// hasRecorded answers whether this conversation's own journal already holds the
// line that carried one delivery. It is what a resume asks before re-telling a
// landing: the checkpoint may not have been written, and the journal is the
// record that was ([sessionFile.recorded]).
func (a *Agent) hasRecorded(id deliveryID) bool {
	a.mu.Lock()
	file := a.file
	a.mu.Unlock()
	return file.recorded(id)
}

// mailbox is a conversation that can be handed a message. Both implementations
// are local: an agent, and the seat inside a task room.
type mailbox interface {
	address() conversationID
	accept(delivery) deliveryReceipt
}

// deliverTo hands one message to the first mailbox that takes it and answers
// what happened rather than what was attempted.
//
// The order is the caller's policy: a landed node's report tries the parent's
// worker and then the person's conversation, because news with nowhere to go
// still belongs to somebody, while a line said INTO a room has one mailbox and
// no fallback — redirecting somebody's words to a reader they did not address is
// worse than saying nobody was there.
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

// address is this agent's own conversation identity. The session is read the
// way [TaskGraph.sessionName] reads it — empty for a conversation with no
// journal — so an agent and its graph cannot disagree about which session this
// is.
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
// THE KIND CHOOSES THE QUEUE. Progress goes on the ambient queue, which no step
// drain takes and no wake reads, so telemetry cannot start a turn however it was
// addressed. Everything else goes on the steering queue wearing the marks its
// own constructor put on it: the wake, the release of a parked runner and the
// journal lane are facts about the message, decided where it was built.
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
// rather than an agent because that reader is not fixed: the runner withdraws it
// the moment its reading is over (task_child_run.go), so a pointer read a moment
// ago may already have stopped listening.
type roomSeat struct {
	at   conversationID
	room *taskRoom
}

func (s roomSeat) address() conversationID { return s.at }

func (s roomSeat) accept(message delivery) deliveryReceipt {
	return s.room.handIn(s.at, message)
}

// handIn resolves the live reader and appends under ONE hold of the room's lock.
// The runner's withdrawal takes the same lock, so the two cannot interleave into
// a swallow: either the message lands while the seat is filled — and then it
// lands before the runner's final queue check, which answers it with one more
// turn — or the withdrawal won and this refuses, words kept. Split across two
// locks it was #273's race with a narrower window, not a fix.
func (r *taskRoom) handIn(at conversationID, message delivery) deliveryReceipt {
	if r == nil {
		return deliveryReceipt{to: at, state: deliveryNobody}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.child == nil {
		return deliveryReceipt{to: at, state: deliveryNobody}
	}
	// The address comes from the child, so a receipt names the conversation that
	// really took the message rather than the one this seat was built for.
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
// graph with no conversation behind it. It takes no graph lock because it asks
// the home agent, and a delivery must never take an agent's lock under the
// graph's.
func (g *TaskGraph) sessionName() string {
	if g == nil || g.home == nil {
		return ""
	}
	return g.home.journalID()
}
