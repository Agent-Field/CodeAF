package serve

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// A SPENT GITHUB LIMIT DECIDES NOTHING ABOUT A REPOSITORY. A rate limit on the
// visibility check (403 with no requests remaining, or a 429 secondary limit)
// was read as "not allowed": the outcome was passed as hidden, the hidden
// answer was kept for the visibility TTL, and a signed-in caller whose token
// was rate-limited saved a cursor past an outcome it could see, losing it for
// good. A limit is undecided, as a 5xx is: the page ends before it, a read
// that would start at it answers try again, and once GitHub answers again
// the outcome is shown.
func TestJobEventsWaitOutAGitHubRateLimit(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		header func(now time.Time) http.Header
	}{
		{"a spent primary limit", http.StatusForbidden, func(now time.Time) http.Header {
			return http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {strconv.FormatInt(now.Add(2*time.Minute).Unix(), 10)}}
		}},
		{"a secondary limit", http.StatusTooManyRequests, func(time.Time) http.Header {
			return http.Header{"Retry-After": {"60"}}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h, clock, github := visibilityFloor(t, map[string]map[string]bool{"priya": {"acme/api": true, "acme/web": true}})
			token := signViewer(t, h, github, clock, "priya")
			st, err := store.Open(h.eventRoot())
			if err != nil {
				t.Fatal(err)
			}
			snap, err := h.seam.Load()
			if err != nil {
				t.Fatal(err)
			}
			for _, it := range snap.Items {
				it.ID, it.Num = 0, 1
				if _, err := st.Create(it); err != nil {
					t.Fatal(err)
				}
			}
			h.config.Store = st
			for _, id := range []int{1, 2, 3} {
				if err := live.Append(st.Root(), live.New(live.OutcomeKind, id, live.Outcome{Job: id, Kind: "finished"})); err != nil {
					t.Fatal(err)
				}
			}
			<-github.mu
			github.limited = map[string]limitAnswer{"acme/web": {tt.status, tt.header(*clock)}}
			github.mu <- struct{}{}
			read := func(after int) *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/job-events?after=%d", after), nil)
				r.RemoteAddr = "192.0.2.1:9"
				r.Header.Set("Authorization", "Bearer "+token)
				w := httptest.NewRecorder()
				h.jobEvents(w, authRequest(r))
				return w
			}
			// Job 2's repository check meets the spent limit: the page stops before it.
			got := decodeJobEvents(t, read(0))
			if got.Reset || got.Cursor != 1 || !got.More || len(got.Events) != 1 || got.Events[0].Job != 1 {
				t.Fatalf("a page across a rate-limited repository: %+v", got)
			}
			// A read that starts at it answers try again, and moves no cursor,
			// while GitHub's limit is still spent.
			if w := read(1); w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "try again") {
				t.Fatalf("a read at a rate-limited repository: %d %s", w.Code, w.Body.String())
			}
			// The limit resets: the outcome is shown, then the rest follow.
			<-github.mu
			github.limited = nil
			github.mu <- struct{}{}
			*clock = clock.Add(repoVisibilityTTL + time.Second)
			got = decodeJobEvents(t, read(1))
			if got.Reset || got.Cursor != 3 || got.More || len(got.Events) != 2 || got.Events[0].Job != 2 || got.Events[1].Job != 3 {
				t.Fatalf("after the limit reset: %+v", got)
			}
		})
	}
}
