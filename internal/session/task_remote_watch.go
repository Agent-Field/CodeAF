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
	last := clock.Now()
	delay := time.Second
	unreachable := false
	notice := func() {
		if !unreachable && clock.Now().Sub(last) >= TaskComputerSilentAfter {
			unreachable = true
			reachability(true)
		}
	}
	for {
		child, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		activity := make(chan struct{}, 1)
		deliveries := make(chan taskWatchDelivery)
		alive := func() {
			select {
			case activity <- struct{}{}:
			default:
			}
		}
		go func(cursor uint64) {
			done <- watch(child, cursor, alive, func(seq uint64) error {
				delivery := taskWatchDelivery{seq: seq, done: make(chan error, 1)}
				select {
				case deliveries <- delivery:
				case <-child.Done():
					return child.Err()
				}
				select {
				case err := <-delivery.done:
					return err
				case <-child.Done():
					return child.Err()
				}
			})
		}(from)
		tick := clock.After(time.Second)
		var err error
	watching:
		for {
			select {
			case <-activity:
				last = clock.Now()
				delay = time.Second
				if unreachable {
					unreachable = false
					reachability(false)
				}
			case delivery := <-deliveries:
				var consumeErr error
				if delivery.seq > from {
					consumeErr = consume(delivery.seq)
					if consumeErr == nil {
						from = delivery.seq
					}
				}
				delivery.done <- consumeErr
				if consumeErr != nil {
					cancel()
					<-done
					return consumeErr
				}
			case err = <-done:
				break watching
			case <-ctx.Done():
				cancel()
				<-done
				return ctx.Err()
			case <-tick:
				notice()
				tick = clock.After(time.Second)
			}
		}
		cancel()
		if err == nil || errors.Is(err, ErrTaskWatchEnded) {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		wait := delay
		if unreachable {
			wait = TaskComputerRetryEvery
		}
		retry := clock.After(wait)
		tick = clock.After(time.Second)
	waiting:
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-retry:
				break waiting
			case <-tick:
				notice()
				tick = clock.After(time.Second)
			}
		}
		if delay < TaskComputerRetryEvery {
			delay *= 2
			if delay > TaskComputerRetryEvery {
				delay = TaskComputerRetryEvery
			}
		}
	}
}

// ErrTaskWatchEnded distinguishes a final frame from a transport drop.
var ErrTaskWatchEnded = errors.New("the task watch ended")
