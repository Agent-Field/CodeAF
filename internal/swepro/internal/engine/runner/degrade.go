package runner

// `degrade` — the shared collapse helper of ENGINE-DESIGN §4.5, standing in
// for the 115 `Effect.catchCause` sites (27 of them behind an
// `Effect.timeout`) that the TS uses to make a step un-failable.
//
// The TS idiom, and the three shapes it appears in:
//
//	(a) effect.pipe(Effect.timeout(MS), Effect.catchCause(() => Effect.succeed(fallback)))
//	(b) effect.pipe(Effect.timeout("3 seconds"), Effect.catchCause(() => Effect.void))
//	(c) effect.pipe(Effect.catchCause(() => Effect.succeed(fallback)))
//
// plandb-scheduler.ts:2119-2121 states the contract: "Effect.timeout itself
// wraps in a Cause when the deadline trips, so a single catchCause swallows
// both failures and timeouts. Defects also flow through cause-handling, so
// this is a complete catch-all."
//
// It is complete for failures, defects, timeouts, and interruption. In
// particular, an already-cancelled outer context still takes the one fallback
// branch. This is intentionally uncomfortable, but it is the explicit
// ENGINE-DESIGN.md:1174-1182 contract.

import (
	"context"
	"errors"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/logshim"
)

// PrettyLimit is the `.slice(0, 300)` used at prompt.ts:1528, agent-json.ts:531
// and review-gate.ts:808/:1093. Those slices are written into gate results, so
// they are model-visible, not log-only.
const PrettyLimit = 300

// Value runs fn with an optional deadline and collapses timeout, failure,
// defect (panic), and interruption into fallback. d <= 0 disables the
// deadline.
//
// The deadline interrupts fn's context and then WAITS for fn to return, which
// is what Effect.timeout does with the inner fiber's finalizers. An fn that
// ignores ctx therefore blocks past its deadline, exactly like an
// uninterruptible Effect.
func Value[T any](
	ctx context.Context,
	d time.Duration,
	fallback T,
	fn func(context.Context) (T, error),
) T {
	if ctx == nil {
		ctx = context.Background()
	}
	inner := ctx
	if d > 0 {
		var cancel context.CancelFunc
		inner, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}

	v, err := runWork(inner, fn)

	// The deadline tripping beats a late success, the way Effect.timeout fails
	// with TimeoutError even if the uninterruptible inner effect went on to
	// produce a value.
	if d > 0 && ctx.Err() == nil && errors.Is(inner.Err(), context.DeadlineExceeded) {
		return fallback
	}
	if err == nil {
		return v
	}
	return fallback
}

// Void is shape (b): T = void.
func Void(ctx context.Context, d time.Duration, fn func(context.Context) error) {
	Value(ctx, d, struct{}{}, func(c context.Context) (struct{}, error) {
		return struct{}{}, fn(c)
	})
}

// Log is Value with the log line the noisier sites carry, e.g.
// prompt.ts:1528 `slog.error("scheduler cycle errored", {cause:
// Cause.pretty(cause).slice(0, 300)})`.
func Log[T any](
	ctx context.Context,
	d time.Duration,
	fallback T,
	msg string,
	fn func(context.Context) (T, error),
) T {
	if ctx == nil {
		ctx = context.Background()
	}
	inner := ctx
	if d > 0 {
		var cancel context.CancelFunc
		inner, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}

	v, err := runWork(inner, fn)

	timedOut := d > 0 && ctx.Err() == nil && errors.Is(inner.Err(), context.DeadlineExceeded)
	if !timedOut && err == nil {
		return v
	}
	cause := err
	if timedOut && cause == nil {
		cause = context.DeadlineExceeded
	}
	logshim.Default.Error(msg, map[string]any{"cause": PrettySlice(cause, PrettyLimit)})
	return fallback
}
