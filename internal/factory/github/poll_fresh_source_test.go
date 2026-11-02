package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// A team factory builds its read source again on every tick, so a replaced
// grant takes effect, and a fresh source holds no list tags. Its unchanged
// issue list must still cost a 304, which GitHub does not count against the
// hourly limit, and not a full answer every minute.
func TestAFreshSourceEachTickAsksForAnUnchangedListConditionally(t *testing.T) {
	f := newFixture()
	var full atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := httptest.NewRecorder()
		f.handle(rec, r)
		if r.URL.Path == "/repos/acme/api/issues" && rec.Code == http.StatusOK {
			full.Add(1)
		}
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
	}))
	t.Cleanup(srv.Close)
	// The token is this test's own, so no other test's answers are kept for it.
	api := forge.NewClient("test-token").WithBaseURL(srv.URL).WithSleep(func(context.Context, time.Duration) error { return nil })
	for range 10 {
		if _, _, err := New(api, []string{"acme/api"}, nil).Read(context.Background(), ""); err != nil {
			t.Fatal(err)
		}
	}
	if n := full.Load(); n != 1 {
		t.Fatalf("ten ticks of an unchanged issue list cost %d full answers", n)
	}
}
