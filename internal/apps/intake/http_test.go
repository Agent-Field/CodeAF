package intake

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIntakeHTTPRejectsRequestsUntilRateLimitResets(t *testing.T) {
	// Exhaust the limit at a fixed point so CPU load cannot reset the window
	// while the test exercises the actual HTTP refusal.
	now := time.Now().Add(time.Hour)
	l := NewLocal(Service{Load: func(string) (Definition, error) {
		t.Fatal("a rate-limited request reached the app loader")
		return Definition{}, nil
	}})
	l.host = "127.0.0.1:1234"
	for range 20 {
		if !l.limit.allow(now) {
			t.Fatal("rejected a request inside the initial allowance")
		}
	}
	w := httptest.NewRecorder()
	l.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://"+l.host+"/intake/01ARZ3NDEKTSV4RRFFQ69G5FAV/form", nil))
	if w.Code != http.StatusTooManyRequests || w.Body.String() != "too many deliveries. Try again shortly\n" {
		t.Fatalf("rate limit = %d %q", w.Code, w.Body.String())
	}
	if !l.limit.allow(now.Add(10 * time.Second)) {
		t.Fatal("the allowance did not reset")
	}
}
