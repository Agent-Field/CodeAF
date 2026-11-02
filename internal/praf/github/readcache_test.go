package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// countingForge is a GitHub that counts what it is asked, by path, and how
// many of those asks it answered in full (GitHub counts those against the
// hourly limit; a 304 it does not).
type countingForge struct {
	mu      sync.Mutex
	asked   map[string]int
	counted int
	answer  func(w http.ResponseWriter, r *http.Request)
}

func newCountingForge(t *testing.T, answer func(w http.ResponseWriter, r *http.Request)) (*countingForge, *httptest.Server) {
	f := &countingForge{asked: map[string]int{}, answer: answer}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.asked[r.Method+" "+r.URL.Path]++
		f.mu.Unlock()
		rec := httptest.NewRecorder()
		answer(rec, r)
		if rec.Code != http.StatusNotModified {
			f.mu.Lock()
			f.counted++
			f.mu.Unlock()
		}
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *countingForge) calls(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.asked[path]
}

func (f *countingForge) countedCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counted
}

// holdReadCacheClock pins the read cache's clock for one test.
func holdReadCacheClock(t *testing.T) *time.Time {
	now := time.Unix(1_800_000_000, 0)
	old := readCacheNow
	readCacheNow = func() time.Time { return now }
	t.Cleanup(func() { readCacheNow = old })
	return &now
}

func idleClient(t *testing.T, srv *httptest.Server) *client {
	// Each test has its own token, so the process-wide cache never carries an
	// answer from one test into another.
	return NewClient("token-" + t.Name()).WithBaseURL(srv.URL).WithSleep(func(context.Context, time.Duration) error { return nil })
}

// WHO A TOKEN BELONGS TO IS ASKED ONCE A WHILE, not on every request: the
// factory asked GET /user for nearly every request it served.
func TestMeIsAskedOnceWhileFreshAndAgainAfterARefusal(t *testing.T) {
	now := holdReadCacheClock(t)
	refuse := false
	f, srv := newCountingForge(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/user":
			_, _ = w.Write([]byte(`{"login":"octocat"}`))
		case refuse:
			w.WriteHeader(http.StatusUnauthorized)
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	})
	c := idleClient(t, srv)
	for range 60 {
		if login, err := c.Me(context.Background()); err != nil || login != "octocat" {
			t.Fatalf("Me = %q, %v", login, err)
		}
	}
	if n := f.calls("GET /user"); n != 1 {
		t.Fatalf("sixty asks of who the token is reached GitHub %d times", n)
	}
	*now = now.Add(identityFresh + time.Second)
	_, _ = c.Me(context.Background())
	if n := f.calls("GET /user"); n != 2 {
		t.Fatalf("a stale answer was not asked again: %d asks", n)
	}
	// A refusal of the token, on any read, forgets who it belonged to.
	refuse = true
	_, _, _ = c.read(context.Background(), srv.URL+"/repos/o/r", "")
	refuse = false
	_, _ = c.Me(context.Background())
	if n := f.calls("GET /user"); n != 3 {
		t.Fatalf("a refused token's login was kept: %d asks", n)
	}
}

// A read GitHub tagged is asked again conditionally, and its 304 answers the
// kept body: GitHub does not count a 304 against the hourly limit.
func TestAnUntaggedReadIsAskedAgainConditionally(t *testing.T) {
	holdReadCacheClock(t)
	f, srv := newCountingForge(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(`[{"number":1}]`))
	})
	c := idleClient(t, srv)
	for range 60 {
		body, _, err := c.read(context.Background(), srv.URL+"/repos/o/r/pulls/2/comments", "")
		if err != nil || string(body) != `[{"number":1}]` {
			t.Fatalf("read = %q, %v", body, err)
		}
	}
	if n := f.countedCalls(); n != 1 {
		t.Fatalf("sixty reads of an unchanged list cost %d counted calls", n)
	}
}

// A branch protection this token may not read is not asked about every
// minute: the answer (none required) stands like who the token is.
func TestAnUnreadableBranchProtectionIsAskedOnceAWhile(t *testing.T) {
	now := holdReadCacheClock(t)
	f, srv := newCountingForge(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Upgrade to GitHub Pro or make this repository public to enable this feature.","documentation_url":"https://docs.github.com/rest/branches/branch-protection#get-branch-protection"}`))
	})
	c := idleClient(t, srv)
	for range 60 {
		if n, err := c.RequiredReviews(context.Background(), "o", "r", "main"); err != nil || n != 0 {
			t.Fatalf("RequiredReviews = %d, %v", n, err)
		}
	}
	if n := f.calls("GET /repos/o/r/branches/main/protection"); n != 1 {
		t.Fatalf("sixty reads of an unreadable protection asked GitHub %d times", n)
	}
	*now = now.Add(identityFresh + time.Second)
	_, _ = c.RequiredReviews(context.Background(), "o", "r", "main")
	if n := f.calls("GET /repos/o/r/branches/main/protection"); n != 2 {
		t.Fatalf("a stale refusal was not asked again: %d", n)
	}
}
