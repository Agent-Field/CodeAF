package remote

import "sync"

// ── ONE MECHANISM: WORK THAT KEEPS ITS ORDER AND LEAVES THE READER ──────────
//
// orderedLane runs handed-in work ON ONE GOROUTINE, IN THE ORDER IT ARRIVED,
// and never makes the hand-in wait. Those three properties together are the
// whole of what the engine's reader goroutine was providing by running calls
// itself — and the fourth thing it was providing, accidentally, was a socket
// that stopped being read for as long as the work took.
//
// THE HAND-IN NEVER BLOCKS AND THE QUEUE IS NOT CAPPED, which is a deliberate
// choice and not an oversight. Every frame on this queue is a call the far end
// is waiting for synchronously, under its own [callDeadline]; a surface that is
// waiting for an answer does not send another. So the depth of this queue is
// the number of calls ONE surface has outstanding — a handful — and it is
// bounded by that surface's own patience rather than by a number invented here.
// A cap would have to be such a number, and the first thing it would do on the
// day it was reached is stop reading the socket, which is the defect this type
// exists to remove.
//
// THAT INVARIANT IS A LAW AND NOT A HOPE. [Client.notify] is the one mechanism
// on this wire that writes a frame nobody waits for, and one line of it naming
// an ordered method would turn the bound above into nothing;
// [TestEveryNotifiedMethodOwesNobodyAnOrder] refuses exactly that. Two smaller
// things the depth argument rests on, said out loud because a reader will ask:
// a surface whose call reaches [callDeadline] is free to send another, so the
// true bound is its outstanding calls plus one per deadline per goroutine — which
// is what "bounded by that surface's own patience" means; and after a long
// [MethodCompact] the lane replays ordered calls nobody is waiting for any more,
// which is what the socket buffer did before this type existed and is therefore
// the same behaviour rather than a new one.
//
// It is a type rather than a channel and a goroutine written inline because
// "run these in the order they came, off the goroutine that received them" is a
// shape, and a shape spelled out twice is two shapes that will disagree.
type orderedLane struct {
	mu     sync.Mutex
	wake   chan struct{}
	queue  []Frame
	closed bool
	done   chan struct{}
}

func newOrderedLane() *orderedLane {
	return &orderedLane{wake: make(chan struct{}, 1), done: make(chan struct{})}
}

// hand puts one frame at the back of the queue and returns at once. A frame
// handed to a lane that has already been closed is dropped: the connection it
// belonged to is over, and running it would write into a session that has been
// told nobody is there.
func (l *orderedLane) hand(frame Frame) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	l.queue = append(l.queue, frame)
	l.mu.Unlock()
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

// run drains the lane until it is closed and everything handed in before the
// close has been run. It is the goroutine the work happens on, and there is
// exactly one of it per lane — which is what makes "in order" and "one at a
// time" the same statement.
func (l *orderedLane) run(work func(Frame)) {
	defer close(l.done)
	for {
		l.mu.Lock()
		if len(l.queue) == 0 {
			if l.closed {
				l.mu.Unlock()
				return
			}
			l.mu.Unlock()
			<-l.wake
			continue
		}
		next := l.queue[0]
		l.queue = l.queue[1:]
		l.mu.Unlock()
		work(next)
	}
}

// close says there is no more work. Everything already handed in still runs, in
// order, because a call whose frame crossed the wire is a call the far end is
// waiting for an answer to.
func (l *orderedLane) close() {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	l.closed = true
	l.mu.Unlock()
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

// wait blocks until the lane has run everything it was given. A lane that was
// never started returns at once, because nothing was ever taken from it.
func (l *orderedLane) wait() { <-l.done }
