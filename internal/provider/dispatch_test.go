package provider

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestBackoffCapsWhatARetryAfterMayAskFor(t *testing.T) {
	plain := backoffFor(1, 0)
	if plain < baseBackoff || plain > baseBackoff+baseBackoff/2 {
		t.Fatalf("plain backoff = %s, want jitter inside [%s, %s]", plain, baseBackoff, baseBackoff+baseBackoff/2)
	}
	if got := backoffFor(1, 5*time.Second); got != 5*time.Second {
		t.Fatalf("a provider asking for 5s should get 5s, got %s", got)
	}
	// The header may be an HTTP date, and "come back tomorrow" is a wait no
	// interactive run can honour.
	if got := backoffFor(1, 24*time.Hour); got != maxProviderWait {
		t.Fatalf("a day-long Retry-After = %s, want the cap %s", got, maxProviderWait)
	}
	if got := backoffFor(5, 0); got <= backoffFor(1, 0) {
		t.Fatalf("backoff should grow with attempts: %s", got)
	}
}

// A Retry-After describes one moment to come back at. It used to be latched:
// set once on the first 429 and then applied as the floor for every later
// attempt of the same call, whatever those attempts failed on.
func TestRetryAfterIsSpentOnTheAttemptItWasIssuedFor(t *testing.T) {
	var responses sync.Mutex
	served := 0
	handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		responses.Lock()
		served++
		first := served == 1
		responses.Unlock()
		if first {
			writer.Header().Set("Retry-After", "30")
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = writer.Write([]byte(`{"error":{"message":"slow down"}}`))
			return
		}
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte(`{"error":{"message":"broken"}}`))
	})

	client, err := NewClient(Config{APIKey: "k", BaseURL: "http://provider.test", Model: "sim/model",
		HTTPClient: handlerClient(handler)})
	if err != nil {
		t.Fatal(err)
	}
	var waits []time.Duration
	client.wait = func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		return nil
	}

	if _, err := client.CompleteWithMessages(context.Background(), userMessages("a")); err == nil {
		t.Fatal("every attempt failed; the call should have")
	}
	if len(waits) < 2 {
		t.Fatalf("expected several retries, waited %v", waits)
	}
	if waits[0] != 30*time.Second {
		t.Fatalf("the 429's own Retry-After should govern the next attempt, got %s", waits[0])
	}
	for index, delay := range waits[1:] {
		if delay >= 30*time.Second {
			t.Fatalf("wait %d = %s: a spent Retry-After is still setting the floor", index+1, delay)
		}
	}
}
