// Package thread owns the single door for durable conversation writes.
package thread

import "github.com/Agent-Field/aforge-v2/internal/store"

type messageStore interface {
	PostMessage(store.Message) (store.Message, error)
}

// Post is the one door for message writes outside internal/store. Every
// attributed speaker uses this door; attribution stays exactly as its caller
// set it in the message fields.
func Post(graph messageStore, message store.Message) (store.Message, error) {
	return graph.PostMessage(message)
}
