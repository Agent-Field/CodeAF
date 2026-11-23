package taskdoor

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/relay"
)

// V22 at the task door: a relay that turns this machine's registrations away is
// retried after a spread wait, not after exactly one second, two, four. The
// first wait is one second plus a draw from the next five, and each later one is
// the doubling schedule scaled by 0.75 to 1.25, so a fleet dropped by one relay
// restart does not come back in step.
func TestTheRegistrationLoopSpreadsItsRetries(t *testing.T) {
	throttled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "too many registrations from here — wait a minute", http.StatusTooManyRequests)
	}))
	t.Cleanup(throttled.Close)
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	var waits []time.Duration
	loop := &registrationLoop{
		Service: throttled.URL,
		Key:     key,
		Say:     func(string) {},
		Hold:    func(context.Context, *relay.Registration) {},
		Wait: func(_ context.Context, d time.Duration) error {
			waits = append(waits, d)
			if len(waits) == 6 {
				stop()
				return context.Canceled
			}
			return nil
		},
	}
	if err := loop.Run(ctx); err != nil {
		t.Fatalf("a loop cancelled mid-wait returned %v, want nil", err)
	}
	if len(waits) != 6 {
		t.Fatalf("%d waits, want 6: %v", len(waits), waits)
	}
	if waits[0] < time.Second || waits[0] >= 6*time.Second {
		t.Errorf("the first wait was %v, want it in [1s, 6s)", waits[0])
	}
	for i, step := range []time.Duration{0, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 32 * time.Second}[1:] {
		got := waits[i+1]
		if got < step*3/4 || got >= step*5/4 {
			t.Errorf("wait %d was %v, want it in [%v, %v)", i+2, got, step*3/4, step*5/4)
		}
	}
}
