package blobstore

import (
	"context"
	"time"
)

// A request's deadline is a fixed allowance plus the time its body needs at the
// slowest link the store still supports. A fixed limit is wrong both ways: it
// kills a large frame on a slow link, and it waits far too long for a small
// request to a dead relay.
const (
	// BaseTimeout is the allowance every request gets, however small.
	BaseTimeout = 30 * time.Second
	// MinThroughput is the slowest sustained rate, in bytes per second, at
	// which a body is still expected to arrive.
	MinThroughput = 32 << 10
)

// Deadline is how long a request carrying n body bytes may take.
func Deadline(n int) time.Duration {
	return BaseTimeout + time.Duration(n)*time.Second/MinThroughput
}

// budget is the time limit of one request. It opens as the allowance for the
// request body, and once the answer's headers say how long the answer is it is
// set again to the allowance for that many bytes: a get sends almost nothing
// and receives a whole frame, so sizing it by the request alone gives a large
// answer the allowance of an empty one and cuts it off on any slow link.
type budget struct {
	timer  *time.Timer
	cancel context.CancelCauseFunc
	sized  func(bodyBytes int) time.Duration
}

// startBudget returns ctx limited to sized(requestBytes) and the budget that
// resizes it.
func startBudget(ctx context.Context, sized func(int) time.Duration, requestBytes int) (context.Context, *budget) {
	ctx, cancel := context.WithCancelCause(ctx)
	b := &budget{cancel: cancel, sized: sized}
	b.timer = time.AfterFunc(sized(requestBytes), func() { cancel(context.DeadlineExceeded) })
	return ctx, b
}

// answering resets the limit to the time an answer of n bytes needs. A length
// the answer does not state (n < 0) is allowed the largest answer there is.
func (b *budget) answering(n int64) {
	if n < 0 {
		n = MaxFrame
	}
	b.timer.Reset(b.sized(int(n)))
}

// done releases the budget's timer and context.
func (b *budget) done() {
	b.timer.Stop()
	b.cancel(nil)
}
