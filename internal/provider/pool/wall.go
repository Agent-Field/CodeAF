package pool

import (
	"context"
	"fmt"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/router"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// DefaultCallWall is how long one structuring completion may take before the
// system stops believing in it.
//
// Four minutes is deliberately far past honest: the slowest structuring call in
// the production usage rows — a contract pass over a wide fanout, on a
// reasoning model, with a cold cache — is well under a minute. This is not a
// latency budget and must never be tuned like one. It is the line past which a
// single completion is no longer slow, it is gone.
//
// The unit matters. This bounds ONE completion, not one command and not one
// agent loop: a head turn that makes nine tool calls gets nine fresh walls, and
// a leaf that thinks for an hour across forty round-trips is never touched by
// it. Bounding the call rather than the caller is what lets the number be small
// enough to catch a hang without ever cutting honest work in half.
//
// The incident it exists for: a contract call on a chat splice never returned.
// There was no deadline anywhere on the path — not on the context, not on the
// client, not on the transport for a streamed request — so the reconciler's
// command queue stopped forever behind one open socket while the process went
// on looking alive.
const DefaultCallWall = 4 * time.Minute

// ErrCallWall is what a call that outlived its wall returns. It is an ordinary
// provider failure by design: every caller on the structuring path already has
// an error branch, and this arrives on it rather than inventing a new one. The
// layer above owns the retry — this layer only refuses to wait forever.
//
// It wraps context.DeadlineExceeded deliberately. A stall has to be legible to
// the reconciler's watchdog, which decides between striking a command and
// failing it, and the alternative was for internal/resident to import this
// package for one sentinel. Wrapping the standard error instead means the
// watchdog asks the only question it actually has — "did this die of time?" —
// with errors.Is and no new dependency.
var ErrCallWall = fmt.Errorf("the model stopped answering: %w", context.DeadlineExceeded)

// walled bounds one completion at a time. It is a decorator over router.Client
// rather than a change inside the provider adapters because the wall is a
// policy about which calls the surface is willing to wait on, and the adapters
// do not know which call they are serving.
//
// It is created fresh from Snapshot on every read, so a model swap underneath
// is picked up without the wrapper ever holding a stale client.
type walled struct {
	inner router.Client
	wall  time.Duration
}

// wallClient wraps only when there is a wall to apply, so an unwalled slot —
// the work client, whose leaves are agent loops with their own governors —
// keeps handing back exactly the object it always did, type identity included.
func wallClient(inner router.Client, wall time.Duration) router.Client {
	if inner == nil || wall <= 0 {
		return inner
	}
	return walled{inner: inner, wall: wall}
}

func (w walled) Model() string { return w.inner.Model() }

// CompleteWithMessages runs the inner call under its own deadline and then
// tells the two expiries apart. A caller who cancelled gets their own context
// error back untouched — an interrupt is not a provider failure and must not be
// retried as one. Only a call that outlived the wall while its caller was still
// waiting becomes ErrCallWall.
func (w walled) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	callCtx, cancel := context.WithTimeout(ctx, w.wall)
	defer cancel()

	response, err := w.inner.CompleteWithMessages(callCtx, messages, options...)
	if err == nil {
		return response, nil
	}
	if ctx.Err() != nil {
		// The caller went away. Whatever the inner call said about it, the
		// honest report is the caller's own cancellation.
		return response, err
	}
	if callCtx.Err() != nil {
		return nil, fmt.Errorf("%w after %s", ErrCallWall, w.wall)
	}
	return response, err
}

// WithCallWall sets the per-completion wall for every call served through this
// slot, including the ones a caller makes on a snapshot it took earlier.
//
// It is opt-in per slot, and the split is the whole safety argument. The
// structuring slots — the one that talks and the one that plans — are single
// completions whose only honest duration is short, so they are walled. The work
// slot is not: a leaf is an agent loop bounded by the executor's own deadline,
// and a wall here would be a second, dumber governor over work that is
// legitimately allowed to take hours.
func (l *Client) WithCallWall(wall time.Duration) *Client {
	if l == nil {
		return l
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.wall = wall
	return l
}

// CallWall reports the wall in force on this slot; zero means unbounded.
func (l *Client) CallWall() time.Duration {
	if l == nil {
		return 0
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.wall
}
