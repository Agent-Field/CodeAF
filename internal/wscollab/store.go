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

	PutDiscussion(ctx context.Context, d Discussion) error
	GetDiscussion(ctx context.Context, id string) (Discussion, error)

	PutInvocation(ctx context.Context, inv Invocation) error
	GetInvocation(ctx context.Context, id string) (Invocation, error)
}
