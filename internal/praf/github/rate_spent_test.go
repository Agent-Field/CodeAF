package github

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// A SPENT LIMIT WHOSE RESET OUTLASTS ONE PAUSE IS ANSWERED AT ONCE: the read
// is refused the same way until the reset, so it is tried once and not slept on.
func TestAReadMeetingALongSpentLimitAnswersAtOnce(t *testing.T) {
	var calls atomic.Int32
	c := mergeServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
		http.Error(w, `{"message":"API rate limit exceeded"}`, http.StatusForbidden)
	})
	var slept []time.Duration
	c.WithSleep(func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil })
	_, err := c.Me(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.RetryAfter <= maxRateSleep {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != 1 || len(slept) != 0 {
		t.Fatalf("calls %d, slept %v", calls.Load(), slept)
	}
}
