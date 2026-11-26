package relay

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// Both rate limits' refusals say how long is left in the minute they are
// counted in, so a client waits for its window and not for a guess. The wait is
// never under a second, because a client told to come back at once is refused
// again, and the refusal for a full machine, which has no window, says nothing.
func TestARefusedCallerIsToldWhenToComeBack(t *testing.T) {
	zero := time.Unix(100_000, 0) // 40 s into a minute, so 20 s of it are left
	for _, door := range []struct {
		name  string
		path  string
		limit int
	}{{"registrations", EnginePath, RegistrationsPerMinute}, {"dials", DialPrefix + "otter-lamp-42", DialsPerMinute}} {
		for _, at := range []struct {
			into time.Duration
			want int
		}{{0, 20}, {19*time.Second + 500*time.Millisecond, 1}, {-40 * time.Second, 60}, {-39*time.Second - 500*time.Millisecond, 60}} {
			t.Run(door.name+"/"+at.into.String(), func(t *testing.T) {
				now := zero.Add(at.into)
				server := &Server{Now: func() time.Time { return now }}
				for i := 0; i <= door.limit; i++ {
					req := httptest.NewRequest(http.MethodGet, door.path, nil)
					req.RemoteAddr = "192.0.2.1:1234"
					req.Header.Set("Upgrade", Protocol)
					out := httptest.NewRecorder()
					server.ServeHTTP(out, req)
					if i < door.limit {
						if out.Code == http.StatusTooManyRequests {
							t.Fatalf("request %d was refused inside its allowance", i+1)
						}
						if out.Header().Get("Retry-After") != "" {
							t.Fatalf("request %d, which was not refused, carried a Retry-After", i+1)
						}
						continue
					}
					if out.Code != http.StatusTooManyRequests {
						t.Fatalf("request %d status=%d, want 429", i+1, out.Code)
					}
					got, err := strconv.Atoi(out.Header().Get("Retry-After"))
					if err != nil || got != at.want {
						t.Fatalf("Retry-After %q, want %d", out.Header().Get("Retry-After"), at.want)
					}
				}
			})
		}
	}
}
