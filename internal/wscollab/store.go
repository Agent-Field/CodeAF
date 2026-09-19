package wscollab

import "context"

// Store is the durable outbox and the participant/invocation records this
// router owns. The schema lane persists participants and deliveries; this
// package talks only to the interface so a fake in tests is enough until
// integrate. There is no production memory fallback: [New] refuses a nil store.
type Store interface {
	Put(ctx context.Context, env Envelope) error
	Get(ctx context.Context, id DeliveryID) (Record, error)
	SetQueue(ctx context.Context, id DeliveryID, queue string) error
	SetRecorded(ctx context.Context, id DeliveryID) error
	SetProcessed(ctx context.Context, id DeliveryID) error
	Pending(ctx context.Context, conversationID string) ([]Envelope, error)
	// Archived is whether automatic Bind/Resume/host wake are suppressed for
	// this conversation. History stays; pending stays pending until it is
	// brought back. Pause is a different bit and must not report true here.
	Archived(ctx context.Context, conversationID string) (bool, error)

	PutDiscussion(ctx context.Context, d Discussion) error
	GetDiscussion(ctx context.Context, id string) (Discussion, error)

	PutInvocation(ctx context.Context, inv Invocation) error
	GetInvocation(ctx context.Context, id string) (Invocation, error)
}
