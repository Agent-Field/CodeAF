package session

import (
	"context"
	"errors"
	"testing"
)

// The CI watcher split must retain the fatal boundary for a failed durable consumer.
func TestAFailedRemoteRecordWriteStopsWatchingInsteadOfRetrying(t *testing.T) {
	want := errors.New("the task record could not be written")
	attempts := 0
	watch := func(ctx context.Context, from uint64, alive func(), deliver func(uint64) error) error {
		attempts++
		if attempts > 1 {
			return ErrTaskWatchEnded
		}
		return deliver(1)
	}
	err := FollowTaskWatch(t.Context(), nil, 0, watch, func(uint64) error { return want }, func(bool) {})
	if !errors.Is(err, want) || attempts != 1 {
		t.Fatalf("failed record write returned %v after %d connections", err, attempts)
	}
}
