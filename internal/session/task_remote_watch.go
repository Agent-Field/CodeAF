package session

import (
	"context"
	"errors"
	"time"
)

const (
	TaskComputerSilentAfter = time.Minute
	TaskComputerRetryEvery  = 30 * time.Second
)

// TaskComputerStoppedAnswering is shared with every surface drawing a drop.
func TaskComputerStoppedAnswering(name string) string {
	return name + " stopped answering — it may still be working"
}
func TaskRunningOn(name string) string { return "running on " + name }
func TaskRanOn(name string) string     { return "ran on " + name }

// TaskWatchClock supplies wakeups as well as readings so a network outage can
// be tested without spending a minute on the wall clock.
type TaskWatchClock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}
type taskWatchWall struct{}

func (taskWatchWall) Now() time.Time                         { return time.Now() }
func (taskWatchWall) After(d time.Duration) <-chan time.Time { return time.After(d) }

// TaskWatch retries only a watch. Activity includes pongs, so quiet useful work
// is never called missing. deliver waits until local writes have completed.
type TaskWatch func(context.Context, uint64, func(), func(uint64) error) error

type taskWatchDelivery struct {
	seq  uint64
	done chan error
}

// FollowTaskWatch serializes frames and reachability on its caller's goroutine.
// The network goroutine is joined on every exit and never sends a second start.
func FollowTaskWatch(ctx context.Context, clock TaskWatchClock, from uint64, watch TaskWatch, consume func(uint64) error, reachability func(bool)) error {
	if clock == nil {
		clock = taskWatchWall{}
	}
	state := taskWatchState{clock: clock, last: clock.Now(), delay: time.Second, reachability: reachability}
	for {
		var err error
		var retry bool
		from, err, retry = state.follow(ctx, from, watch, consume)
		if !retry {
			return err
		}
		if err == nil || errors.Is(err, ErrTaskWatchEnded) {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := state.wait(ctx); err != nil {
			return err
		}
	}
}

type taskWatchState struct {
	clock        TaskWatchClock
	last         time.Time
	delay        time.Duration
	unreachable  bool
	reachability func(bool)
}

// notice and alive keep reachability on the caller's goroutine, including retry waits.
func (s *taskWatchState) notice() {
	if !s.unreachable && s.clock.Now().Sub(s.last) >= TaskComputerSilentAfter {
		s.unreachable = true
		s.reachability(true)
	}
}
func (s *taskWatchState) alive() {
	s.last, s.delay = s.clock.Now(), time.Second
	if s.unreachable {
		s.unreachable = false
		s.reachability(false)
	}
}

// follow joins this connection before retrying, so two watches cannot interleave callbacks.
func (s *taskWatchState) follow(ctx context.Context, from uint64, watch TaskWatch, consume func(uint64) error) (uint64, error, bool) {
	child, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	activity := make(chan struct{}, 1)
	deliveries := make(chan taskWatchDelivery)
	joined := false
	defer func() {
		cancel()
		if !joined {
			<-done
		}
	}()
	alive := func() {
		select {
		case activity <- struct{}{}:
		default:
		}
	}
	go func(cursor uint64) {
		done <- watch(child, cursor, alive, func(seq uint64) error { return deliverTaskWatch(child, deliveries, seq) })
	}(from)
	tick := s.clock.After(time.Second)
	for {
		select {
		case <-activity:
			s.alive()
		case delivery := <-deliveries:
			var err error
			if delivery.seq > from {
				err = consume(delivery.seq)
				if err == nil {
					from = delivery.seq
				}
			}
			delivery.done <- err
			if err != nil {
				return from, err, false
			}
		case err := <-done:
			joined = true
			return from, err, true
		case <-ctx.Done():
			return from, ctx.Err(), false
		case <-tick:
			s.notice()
			tick = s.clock.After(time.Second)
		}
	}
}

// deliverTaskWatch waits for the durable consumer before allowing another wire event.
func deliverTaskWatch(ctx context.Context, deliveries chan<- taskWatchDelivery, seq uint64) error {
	delivery := taskWatchDelivery{seq: seq, done: make(chan error, 1)}
	select {
	case deliveries <- delivery:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-delivery.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// wait keeps the same silence notice active while backing off between connections.
func (s *taskWatchState) wait(ctx context.Context) error {
	delay := s.delay
	if s.unreachable {
		delay = TaskComputerRetryEvery
	}
	retry, tick := s.clock.After(delay), s.clock.After(time.Second)
waiting:
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-retry:
			break waiting
		case <-tick:
			s.notice()
			tick = s.clock.After(time.Second)
		}
	}
	if s.delay < TaskComputerRetryEvery {
		s.delay *= 2
		if s.delay > TaskComputerRetryEvery {
			s.delay = TaskComputerRetryEvery
		}
	}
	return nil
}

// ErrTaskWatchEnded distinguishes a final frame from a transport drop.
var ErrTaskWatchEnded = errors.New("the task watch ended")
