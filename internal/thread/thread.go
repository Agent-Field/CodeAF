// Package thread owns the single door for durable conversation writes.
package thread

import "github.com/Agent-Field/aforge-v2/internal/store"

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
