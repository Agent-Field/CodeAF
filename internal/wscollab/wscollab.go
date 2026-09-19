// Package wscollab is the one durable envelope and outbox for cross-chat
// collaboration.
//
// Direct request/reply, fan-out to several recipients, and a contribution to a
// shared discussion are the same path: one envelope, one router, one receipt
// shape. Destinations and participant records express the difference. There is
// no manager subclass, no second bus, and no planner or critic type — those
// labels are configurable roles on a participant.
//
// THREE ACKS ARE THREE FACTS, and a caller that treats any of them as "sent"
// loses messages (A12):
//
//   - accepted: a live queue took the line. The queue dies with the process.
//   - recorded: the recipient's own journal holds it. Resume asks the journal.
//   - processed: the recipient acted. Deliver never sets this.
//
// THE QUEUE IS NOT THE RECORD. The router writes the outbox, routes to the
// owning engine host, and appends through that conversation's single-writer
// journal seam before it offers the line to a live queue. An offline or retired
// host leaves the envelope pending; Bind plus Resume delivers it once, except
// an archived conversation, which stays pending on Bind, Resume, tick, and
// host spawn so putting it away is not an automatic wakeup.
//
// Citing a chat as evidence does not wake it.
//
// Session must not import this package. The host binds a conversation's journal
// and queue through [Router.Bind] and installs a [HostFinder] through
// [RegisterHostFinder], the same cycle-safe door [session.RegisterRunEngine]
// is for the run engine. Delivery identifiers compose the local mailbox scheme
// (conversation / task @ life : ending) rather than minting a second one.
package wscollab

import "errors"

const (
	// OriginPerson is the authenticated person at a trusted surface door.
	OriginPerson = "person"
	// OriginAgent is a representative or another conversation. Assignment
	// overlays that read this origin grant nothing (A11).
	OriginAgent = "agent"
	// OriginRuntime is the program's own account of something that happened.
	OriginRuntime = "runtime"

	// PatternDirect is one recipient. PatternFanout is several, each with its
	// own delivery id and the same cause. PatternDiscussion is a contribution
	// into a shared conversation. They are not three routers.
	PatternDirect     = "direct"
	PatternFanout     = "fan-out"
	PatternDiscussion = "discussion"

	// QueuePending is an offline or retired recipient. The outbox holds the
	// line; nobody has taken it and the journal has not either.
	QueuePending = "pending"
	// QueueAccepted is a live reader with the line on its queue. Not recorded.
	QueueAccepted = "accepted"
	// QueueNobody is a bound conversation whose reader is not listening, or a
	// recorded line that is not being offered to a queue (a cite, a replay).
	QueueNobody = "nobody"
	// QueueClosed is a reader that can take nothing more. A refusal, not a drop.
	QueueClosed = "closed"

	// ScopeSelected is a frozen snapshot of marked conversation ids. Adding a
	// sibling elsewhere does not enlarge it (A16).
	ScopeSelected = "selected"
	// ScopeFolder follows current descendants, including chats filed later.
	ScopeFolder = "folder"

	// RootID is the virtual Root representative. Root is not a collections row;
	// this identity exists so a conflict discussion can name it once (A17).
	RootID = "root"
)

var (
	ErrInvalid  = errors.New("invalid collaboration record")
	ErrNotFound = errors.New("collaboration record not found")
	ErrRetired  = errors.New("engine host has retired")
)
