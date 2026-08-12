// Package thread owns the single door for durable conversation writes.
package thread

import (
	"fmt"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

type messageStore interface {
	PostMessage(store.Message) (store.Message, error)
}

// Post is the one door for message writes outside internal/store. Every
// attributed speaker uses this door; attribution stays exactly as its caller
// set it in the message fields.
//
// Typed message parts ride here too, on store.Message.Parts, and the signature
// is unchanged on purpose: what a message carries belongs to the message, so
// the door widens by the message widening. A second entry point taking parts
// would be a second door, and the whole value of this one is that there is only
// one place where prose, attribution and structure are all checked together.
func Post(graph messageStore, message store.Message) (store.Message, error) {
	return graph.PostMessage(message)
}

// Record is the other half of the same door: one message written to the WORK
// RECORD and not to the conversation.
//
// THE THREE-CLASS LAW (13.18). A thread message is a COMMITMENT ("on it —
// splitting this four ways"), a DELIVERY (the result), or a QUESTION. There is
// no fourth class. A compile phase, a stage advance, a lifecycle receipt whose
// sentence the head already spoke in its own voice, a mailbox copy of words the
// person typed a second ago — every one of those is the record's business, and
// the room is where a reader goes to read the record.
//
// The mechanism is one field, and it was already there. A room's transcript is
// store.NodeMessages, keyed by NODE and never by session; every conversation
// read — the chat's poll, the head's own thread window, the v1 lens — is keyed
// by SESSION. So a message with a node and no session is written once, kept
// forever, drawn in the room it belongs to, and heard nowhere. Nothing is lost;
// the thread simply stops carrying it.
//
// The node is required, because a record with nowhere to be read is not a
// record. A producer that wants to say something with no work behind it is
// making one of the three classes and belongs at Post.
func Record(graph messageStore, message store.Message) (store.Message, error) {
	if message.NodeID == "" {
		return store.Message{}, fmt.Errorf("record message: %w: the work record is anchored to a node", store.ErrInvalid)
	}
	message.SessionID = ""
	return graph.PostMessage(message)
}
