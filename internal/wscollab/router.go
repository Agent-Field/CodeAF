package wscollab

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Router is the one outbox. Direct, fan-out, and joint discussion all call
// [Router.Deliver]. The store is required; a missing host finder is pending,
// not a silent in-process success.
type Router struct {
	store Store

	mu    sync.Mutex
	seams map[string]Seam
}

// New builds a router over a durable store. Nil store is a missing capability
// and is refused — there is no production memory fallback.
func New(store Store) (*Router, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: collaboration store is required", ErrInvalid)
	}
	return &Router{store: store, seams: map[string]Seam{}}, nil
}

// Bind attaches the recipient's single-writer seam and flushes pending
// envelopes once. Session never calls this; the host does.
func (r *Router) Bind(ctx context.Context, conversationID string, seam Seam) ([]Receipt, error) {
	if strings.TrimSpace(conversationID) == "" || seam == nil {
		return nil, fmt.Errorf("%w: bind requires a conversation and a seam", ErrInvalid)
	}
	r.mu.Lock()
	r.seams[conversationID] = seam
	r.mu.Unlock()
	return r.Resume(ctx, conversationID)
}

// Unbind drops a live seam because the host retired. Pending envelopes stay.
func (r *Router) Unbind(conversationID string) {
	r.mu.Lock()
	delete(r.seams, conversationID)
	r.mu.Unlock()
}

func (r *Router) bound(conversationID string) Seam {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seams[conversationID]
}

// Deliver hands one message to one or many recipients on the same path.
// Origin is stamped here. One recipient is direct; several share a cause and
// split delivery ids (fan-out); a Discussion field makes the pattern joint.
func (r *Router) Deliver(ctx context.Context, origin string, msg Message, to []string) ([]Receipt, error) {
	origin, err := inspectOrigin(origin)
	if err != nil {
		return nil, err
	}
	if err := msg.validate(); err != nil {
		return nil, err
	}
	if len(to) == 0 {
		return nil, fmt.Errorf("%w: no recipients", ErrInvalid)
	}
	cause, err := causeOf(msg.CauseID)
	if err != nil {
		return nil, err
	}
	actor, err := actorOf(msg.ActorID)
	if err != nil {
		return nil, err
	}
	pattern := patternOf(len(to), msg.Discussion)
	out := make([]Receipt, 0, len(to))
	for _, dest := range to {
		if strings.TrimSpace(dest) == "" {
			return nil, fmt.Errorf("%w: recipient is empty", ErrInvalid)
		}
		rec, err := r.route(ctx, msg.envelope(origin, cause, pattern, dest, actor))
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

func causeOf(cause string) (string, error) {
	if cause != "" {
		return cause, nil
	}
	return mintID()
}

func actorOf(actor string) (string, error) {
	if actor != "" {
		return actor, nil
	}
	return mintID()
}

func patternOf(n int, discussion string) string {
	if discussion != "" {
		return PatternDiscussion
	}
	if n > 1 {
		return PatternFanout
	}
	return PatternDirect
}

// Cite names a chat as evidence and does not wake it, enqueue it, or bind it.
func (r *Router) Cite(conversationID string) Citation {
	return Citation{SessionID: conversationID, Woke: false}
}

// Resume delivers every still-pending line for a conversation once. A journal
// that already holds the id is marked recorded and not appended again.
func (r *Router) Resume(ctx context.Context, conversationID string) ([]Receipt, error) {
	pending, err := r.store.Pending(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	out := make([]Receipt, 0, len(pending))
	for _, env := range pending {
		rec, err := r.route(ctx, env)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

// MarkProcessed is the third ack. The journal must already hold the line;
// Deliver never takes this shortcut.
func (r *Router) MarkProcessed(ctx context.Context, id DeliveryID) error {
	got, err := r.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if !got.Recorded {
		return fmt.Errorf("%w: cannot process a delivery that is not recorded", ErrInvalid)
	}
	return r.store.SetProcessed(ctx, id)
}

func (r *Router) route(ctx context.Context, env Envelope) (Receipt, error) {
	if err := r.store.Put(ctx, env); err != nil {
		return Receipt{}, err
	}
	if seam := r.bound(env.To); seam != nil {
		return r.flush(ctx, seam, env)
	}
	return r.wakeOrPending(ctx, env)
}

func (r *Router) flush(ctx context.Context, seam Seam, env Envelope) (Receipt, error) {
	held, err := r.store.Get(ctx, env.ID)
	if err != nil && err != ErrNotFound {
		return Receipt{}, err
	}
	if seam.Recorded(env.ID) || held.Recorded {
		return r.alreadyRecorded(ctx, env, held)
	}
	if err := seam.Append(ctx, env); err != nil {
		return Receipt{}, err
	}
	if err := r.store.SetRecorded(ctx, env.ID); err != nil {
		return Receipt{}, err
	}
	if env.settled != nil {
		env.settled()
	}
	return r.afterRecord(ctx, seam, env)
}

func (r *Router) alreadyRecorded(ctx context.Context, env Envelope, held Record) (Receipt, error) {
	if !held.Recorded {
		if err := r.store.SetRecorded(ctx, env.ID); err != nil {
			return Receipt{}, err
		}
	}
	queue := held.Queue
	if queue == "" || queue == QueuePending {
		queue = QueueNobody
	}
	return recordedReceipt(env, queue, held.Processed), nil
}

func (r *Router) afterRecord(ctx context.Context, seam Seam, env Envelope) (Receipt, error) {
	if !env.Wake {
		return recordedReceipt(env, QueueNobody, false), nil
	}
	queue := seam.Accept(ctx, env)
	if err := r.store.SetQueue(ctx, env.ID, queue); err != nil {
		return Receipt{}, err
	}
	return recordedReceipt(env, queue, false), nil
}

func (r *Router) wakeOrPending(ctx context.Context, env Envelope) (Receipt, error) {
	rec := pendingReceipt(env)
	if !env.Wake {
		return rec, nil
	}
	finder := registeredHosts()
	if finder == nil {
		return rec, nil
	}
	host, err := finder.Find(ctx, env.To)
	if err != nil || host == nil || !host.Alive() {
		return rec, nil
	}
	if err := host.Wake(ctx, env.To); err != nil {
		return rec, nil
	}
	if seam := r.bound(env.To); seam != nil {
		return r.flush(ctx, seam, env)
	}
	return rec, nil
}
