package provider

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// rateLimitedUntil answers 429 for the first `limit` requests and then answers
// a real completion. It is the fake provider these tests pace against.
func rateLimitedUntil(limit int, served *int, mu *sync.Mutex) http.HandlerFunc {
	return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		*served++
		paced := *served <= limit
		mu.Unlock()
		if paced {
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = writer.Write([]byte(`{"error":{"message":"slow down"}}`))
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"model":"sim/model","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`))
	})
}

func pacedClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	client, err := NewClient(Config{APIKey: "k", BaseURL: "http://provider.test", Model: "sim/model",
		HTTPClient: handlerClient(handler)})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// A CONVERSATION GIVES UP. A person is watching the cursor, and an error they
// can act on beats a silence they cannot.
func TestAWatchedCallGivesUpOnPacingAfterTheBoundedPatience(t *testing.T) {
	var mu sync.Mutex
	served := 0
	client := pacedClient(t, rateLimitedUntil(1000, &served, &mu))
	client.wait = func(context.Context, time.Duration) error { return nil }

	if _, err := client.CompleteWithMessages(context.Background(), userMessages("a")); err == nil {
		t.Fatal("every attempt was rate limited; the call should have failed")
	}
	mu.Lock()
	attempts := served
	mu.Unlock()
	if attempts != rateLimitAttempts {
		t.Fatalf("a watched call made %d attempts, want the bounded %d", attempts, rateLimitAttempts)
	}
}

// A TASK CHILD KEEPS GOING. Nobody is watching it and a worktree of real work
// is behind it, so it waits the pacing out well past the number of attempts
// that ends a conversation's call — as far as its own budget, which the test
// below this one spends.
func TestATaskChildWaitsPacingOutPastTheBoundedPatience(t *testing.T) {
	var mu sync.Mutex
	served := 0
	const paced = rateLimitAttempts * 4
	client := pacedClient(t, rateLimitedUntil(paced, &served, &mu))
	var waits []time.Duration
	client.wait = func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		return nil
	}

	ctx := WithPatientRateLimits(context.Background())
	if _, err := client.CompleteWithMessages(ctx, userMessages("a")); err != nil {
		t.Fatalf("a patient call should have landed once the pacing lifted: %v", err)
	}
	mu.Lock()
	attempts := served
	mu.Unlock()
	if attempts != paced+1 {
		t.Fatalf("a patient call made %d attempts, want it to keep going to %d", attempts, paced+1)
	}
	// NO WAIT IS EVER LONGER THAN maxProviderWait, and an unbounded loop is
	// where that stops being decoration: the exponent would otherwise overflow
	// into a negative duration somewhere past the sixtieth attempt.
	for index, delay := range waits {
		if delay <= 0 || delay > maxProviderWait {
			t.Fatalf("wait %d = %s, want a positive wait no longer than %s", index, delay, maxProviderWait)
		}
	}
}

// BUT IT STILL ENDS. "Waits it out however long that takes" was written as a
// loop with no exit but the context's, so an account saturated by something
// else — a sibling process on the same key, a neighbour's burst that never
// clears — held the node forever and said nothing an operator could act on.
func TestAPatientCallGivesUpOnceItsPatienceIsSpent(t *testing.T) {
	var mu sync.Mutex
	served := 0
	client := pacedClient(t, rateLimitedUntil(1<<20, &served, &mu))
	// Instant waits: this is the degenerate case the attempt ceiling exists for,
	// where the wall clock never moves and only a count is finite.
	client.wait = func(context.Context, time.Duration) error { return nil }

	ctx := WithPatientRateLimits(context.Background())
	_, err := client.CompleteWithMessages(ctx, userMessages("a"))
	if err == nil {
		t.Fatal("a provider that never lets up should still end the call")
	}
	mu.Lock()
	attempts := served
	mu.Unlock()
	if attempts != patientAttempts {
		t.Fatalf("a patient call made %d attempts, want the capped %d", attempts, patientAttempts)
	}
	// It comes out of the SAME DOOR every other provider failure comes out of:
	// the provider's own words, wrapped in the attempt count. internal/session
	// reads exactly this text to decide whether to retry the turn.
	if !strings.Contains(err.Error(), "API error (429)") || !strings.Contains(err.Error(), "slow down") {
		t.Fatalf("the spent call said %q, want the provider's own 429 through the usual path", err)
	}
}

// The other currency. A count of attempts does not bound TIME when the provider
// names the waits: six attempts each told to come back in a minute is five
// minutes of somebody watching a cursor.
func TestPatienceIsBoundedByTheClockAsWellAsTheCount(t *testing.T) {
	fresh := time.Now()
	long := time.Now().Add(-3 * time.Minute)
	forever := time.Now().Add(-11 * time.Minute)

	if outOfPatience(false, 1, fresh) {
		t.Fatal("a watched call gave up on its first retry")
	}
	if !outOfPatience(false, 1, long) {
		t.Fatalf("a watched call was still paced after 3m, past the %s budget", watchedPacingBudget)
	}
	if outOfPatience(true, 1, long) {
		t.Fatalf("a task node gave up after 3m, inside its %s budget", patientPacingBudget)
	}
	if !outOfPatience(true, 1, forever) {
		t.Fatalf("a task node was still paced after 11m, past the %s budget", patientPacingBudget)
	}
	// A call that has met no 429 at all is not on the clock: pacing is measured
	// from the first one, so real work done before it costs nothing.
	if outOfPatience(true, 1, time.Time{}) {
		t.Fatal("a call that was never paced was judged to have spent its patience")
	}
}

// Patience is about PACING and never about faults. A provider that is broken is
// not a provider asking us to come back.
func TestPatienceDoesNotExtendToFaults(t *testing.T) {
	var mu sync.Mutex
	served := 0
	handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		served++
		mu.Unlock()
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte(`{"error":{"message":"broken"}}`))
	})
	client := pacedClient(t, handler)
	client.wait = func(context.Context, time.Duration) error { return nil }

	ctx := WithPatientRateLimits(context.Background())
	if _, err := client.CompleteWithMessages(ctx, userMessages("a")); err == nil {
		t.Fatal("a broken provider should still end the call")
	}
	mu.Lock()
	attempts := served
	mu.Unlock()
	if attempts != maxAttempts {
		t.Fatalf("a patient call retried a fault %d times, want the fault patience %d", attempts, maxAttempts)
	}
}

// THE CONTEXT IS STILL THE ONLY THING THAT ENDS IT. An interrupt lands on a
// parked call at the next select, not at the end of a backoff and not at the
// end of the pacing.
func TestAnInterruptCutsThroughAParkedCall(t *testing.T) {
	ctx, cancel := context.WithCancel(WithPatientRateLimits(context.Background()))
	defer cancel()

	var mu sync.Mutex
	served := 0
	handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		served++
		stop := served >= 2
		mu.Unlock()
		if stop {
			// The person hit stop while the call was sitting on a 429.
			cancel()
		}
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = writer.Write([]byte(`{"error":{"message":"slow down"}}`))
	})
	// The REAL backoff, because the thing under test is the wait answering the
	// context rather than the timer running out.
	client := pacedClient(t, handler)

	done := make(chan error, 1)
	go func() {
		_, err := client.CompleteWithMessages(ctx, userMessages("a"))
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a cancelled call should not have succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a cancelled call was still parked five seconds later")
	}
}

// The surface hears the park begin and hears it end, once each.
func TestPacingIsAnnouncedWhenItStartsAndTakenBackWhenItEnds(t *testing.T) {
	var mu sync.Mutex
	served := 0
	client := pacedClient(t, rateLimitedUntil(2, &served, &mu))
	client.wait = func(context.Context, time.Duration) error { return nil }

	var said []bool
	var noticed sync.Mutex
	ctx := WithPacingNotice(WithPatientRateLimits(context.Background()), func(parked bool) {
		noticed.Lock()
		said = append(said, parked)
		noticed.Unlock()
	})
	if _, err := client.CompleteWithMessages(ctx, userMessages("a")); err != nil {
		t.Fatal(err)
	}
	noticed.Lock()
	defer noticed.Unlock()
	if len(said) != 2 || !said[0] || said[1] {
		t.Fatalf("the pacing notice said %v, want one park and one release", said)
	}
}

// A call that is never paced says nothing at all. The notice is news, not a
// heartbeat.
func TestAnUnpacedCallSaysNothing(t *testing.T) {
	var mu sync.Mutex
	served := 0
	client := pacedClient(t, rateLimitedUntil(0, &served, &mu))

	spoke := false
	ctx := WithPacingNotice(context.Background(), func(bool) { spoke = true })
	if _, err := client.CompleteWithMessages(ctx, userMessages("a")); err != nil {
		t.Fatal(err)
	}
	if spoke {
		t.Fatal("a call that was never paced told somebody it was waiting")
	}
}

// And a call that gives up still takes its own claim back: a surface left
// holding "still waiting" for a call that has ended is worse than one that was
// never told.
func TestPacingIsTakenBackWhenTheCallGivesUp(t *testing.T) {
	var mu sync.Mutex
	served := 0
	client := pacedClient(t, rateLimitedUntil(1000, &served, &mu))
	client.wait = func(context.Context, time.Duration) error { return nil }

	var said []bool
	ctx := WithPacingNotice(context.Background(), func(parked bool) { said = append(said, parked) })
	if _, err := client.CompleteWithMessages(ctx, userMessages("a")); err == nil {
		t.Fatal("every attempt was rate limited; the call should have failed")
	}
	if len(said) != 2 || !said[0] || said[1] {
		t.Fatalf("the pacing notice said %v, want one park and one release", said)
	}
}
