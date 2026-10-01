package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A DAY-OLD CACHE IS ANSWERED AT ONCE AND REFRESHED BEHIND THE ANSWER.
//
// These are the launch's terms (LoadLazy's doc): the first launch of a day used
// to fetch in front of yesterday's cache, and every question the conversation's
// opening asked waited on that fetch — ten seconds on an ordinary connection,
// most of a minute on a failing one, all of it a dark terminal.

// staleServer is a listing that holds every request until release is called,
// counts what it was asked, and then answers with one row named fresh.
type staleServer struct {
	*httptest.Server
	asked   atomic.Int32
	arrived chan struct{}
	release func()
}

func newStaleServer(t *testing.T, status int, fresh string) *staleServer {
	t.Helper()
	gate := make(chan struct{})
	var once sync.Once
	s := &staleServer{arrived: make(chan struct{}, 8), release: func() { once.Do(func() { close(gate) }) }}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.asked.Add(1)
		s.arrived <- struct{}{}
		<-gate
		if status != http.StatusOK {
			http.Error(w, "no listing today", status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"` + fresh + `","name":"Fresh","context_length":200000,
			"architecture":{"input_modalities":["text"],"output_modalities":["text","image"]},
			"pricing":{"prompt":"0.001","completion":"0.002"}}]}`))
	}))
	t.Cleanup(s.Server.Close)
	t.Cleanup(s.release)
	return s
}

// waitAsked waits for the refresh to reach the listing, and fails the test when
// it never does: a day-old cache that is answered and never refreshed would
// leave every later launch answering from it too.
func (s *staleServer) waitAsked(t *testing.T) {
	t.Helper()
	select {
	case <-s.arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("a day-old cache was answered and never refreshed")
	}
}

// seedStaleCache writes yesterday's listing — one row, older than the TTL —
// where a catalog over server would look for it, and answers when it was dated.
func seedStaleCache(t *testing.T, dir, base string) time.Time {
	t.Helper()
	fetched := time.Now().Add(-TTL - time.Hour).UTC()
	base = normalizeBase(base)
	if err := writeCache(cachePath(dir, "", base), cache{
		FetchedAt: fetched, Base: base,
		Models: []Model{{ID: "vendor/yesterday", Name: "Yesterday", ContextLength: 128000,
			InputModalities: []string{"text"}, OutputModalities: []string{"text", "image"}}},
	}); err != nil {
		t.Fatal(err)
	}
	return fetched
}

// answerWithin runs a question that may wait and fails the test when it is
// still waiting at the bound. The bound is generous on purpose: the fetch it
// would be waiting on never returns at all, so any finite wait proves the point.
func answerWithin[T any](t *testing.T, what string, ask func() T) T {
	t.Helper()
	answered := make(chan T, 1)
	go func() { answered <- ask() }()
	select {
	case got := <-answered:
		return got
	case <-time.After(5 * time.Second):
		t.Fatalf("%s waited on a fetch that never returns; a day-old cache must answer at once", what)
	}
	var zero T
	return zero
}

func TestALazyCatalogAnswersADayOldCacheAtOnceAndRefreshesItBehindTheAnswer(t *testing.T) {
	dir := t.TempDir()
	server := newStaleServer(t, http.StatusOK, "vendor/today")
	fetched := seedStaleCache(t, dir, server.URL)
	options := Options{BaseURL: server.URL, Dir: dir}

	lazy := LoadLazy(context.Background(), options)
	t.Cleanup(lazy.Close)

	// Every door that waits, asked while the listing is still being held.
	if window := answerWithin(t, "ContextLength", func() int { return lazy.ContextLength("vendor/yesterday") }); window != 128000 {
		t.Fatalf("the day-old cache answered a window of %d, want its own 128000", window)
	}
	if got := answerWithin(t, "ModelsWithOutput", func() []Model { return lazy.ModelsWithOutput("image") }); len(got) != 1 || got[0].ID != "vendor/yesterday" {
		t.Fatalf("the day-old cache answered %v for image output, want its one row", got)
	}
	if at := lazy.FetchedAt(); !at.Equal(fetched) {
		t.Fatalf("the rows are dated %s, want the cache's own %s — a stale answer carries its date", at, fetched)
	}

	// The refresh was not skipped, only moved behind the answer.
	server.waitAsked(t)
	server.release()
	var landed cache
	deadline := time.Now().Add(5 * time.Second)
	for {
		cached, ok := readCache(cachePath(dir, "", normalizeBase(server.URL)), "", server.URL)
		if ok && cached.FetchedAt.After(fetched) {
			landed = cached
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the refresh behind the answer never reached the cache file")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(landed.Models) != 1 || landed.Models[0].ID != "vendor/today" {
		t.Fatalf("the refreshed cache holds %v, want today's listing", landed.Models)
	}

	// This catalog keeps the answer it gave; the next reader gets today's, and
	// pays no fetch for it.
	if _, ok := lazy.Model("vendor/today"); ok {
		t.Fatal("the listing changed under a catalog that had already answered")
	}
	before := server.asked.Load()
	next := LoadLazy(context.Background(), options)
	t.Cleanup(next.Close)
	if window := next.ContextLength("vendor/today"); window != 200000 {
		t.Fatalf("the next reader answered a window of %d, want today's 200000", window)
	}
	if after := server.asked.Load(); after != before {
		t.Fatalf("a fresh cache was fetched again (%d requests, want %d)", after, before)
	}
}

func TestAFailedRefreshLeavesTheDayOldCacheWhereItWas(t *testing.T) {
	dir := t.TempDir()
	server := newStaleServer(t, http.StatusServiceUnavailable, "")
	fetched := seedStaleCache(t, dir, server.URL)

	lazy := LoadLazy(context.Background(), Options{BaseURL: server.URL, Dir: dir})
	if !answerWithin(t, "Model", func() bool { _, ok := lazy.Model("vendor/yesterday"); return ok }) {
		t.Fatal("the day-old cache lost its row")
	}
	server.waitAsked(t)
	server.release()
	lazy.Close()

	cached, ok := readCache(cachePath(dir, "", normalizeBase(server.URL)), "", server.URL)
	if !ok || !cached.FetchedAt.Equal(fetched) || len(cached.Models) != 1 || cached.Models[0].ID != "vendor/yesterday" {
		t.Fatalf("a failed refresh changed the cache to %+v (ok %v); it must leave yesterday's where it was", cached, ok)
	}
}

func TestClosingTheCatalogStopsTheRefreshBeforeItCanWrite(t *testing.T) {
	dir := t.TempDir()
	server := newStaleServer(t, http.StatusOK, "vendor/today")
	fetched := seedStaleCache(t, dir, server.URL)

	lazy := LoadLazy(context.Background(), Options{BaseURL: server.URL, Dir: dir})
	answerWithin(t, "ContextLength", func() int { return lazy.ContextLength("vendor/yesterday") })
	server.waitAsked(t)
	lazy.Close()
	server.release()

	cached, ok := readCache(cachePath(dir, "", normalizeBase(server.URL)), "", server.URL)
	if !ok || !cached.FetchedAt.Equal(fetched) {
		t.Fatalf("a refresh outlived the catalog that owned it and wrote %+v", cached)
	}
}

// The other two shapes keep the terms they had: a cache inside the TTL is never
// fetched over, and a machine with no cache at all still fetches before it
// answers — there is nothing older to tell it.
func TestAFreshCacheIsNotFetchedAndNoCacheStillWaitsForTheListing(t *testing.T) {
	server := newStaleServer(t, http.StatusOK, "vendor/today")
	server.release()

	fresh := t.TempDir()
	base := normalizeBase(server.URL)
	if err := writeCache(cachePath(fresh, "", base), cache{FetchedAt: time.Now().UTC(), Base: base,
		Models: []Model{{ID: "vendor/cached", OutputModalities: []string{"text"}}}}); err != nil {
		t.Fatal(err)
	}
	inside := LoadLazy(context.Background(), Options{BaseURL: server.URL, Dir: fresh})
	if _, ok := inside.Model("vendor/cached"); !ok {
		t.Fatal("a fresh cache did not answer")
	}
	inside.Close()
	if asked := server.asked.Load(); asked != 0 {
		t.Fatalf("a cache inside the TTL was fetched over %d times", asked)
	}

	cold := LoadLazy(context.Background(), Options{BaseURL: server.URL, Dir: t.TempDir()})
	t.Cleanup(cold.Close)
	if _, ok := cold.Model("vendor/today"); !ok {
		t.Fatal("with no cache the first question did not get the fetched listing")
	}
}

// The engine host builds its catalog with no Dir at all, and the cache path
// falls back to the home directory. That catalog is the one every conversation
// asks, so it is the one whose day-old cache most needs refreshing — and the
// first cut of this change skipped it, because it read an empty Dir as "no
// cache". Answered at once from the stale rows, and never refreshed, the model
// list would have stayed yesterday's for good.
func TestACatalogWithNoDirRefreshesTheDayOldCacheItFoundAtHome(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	server := newStaleServer(t, http.StatusOK, "vendor/today")
	fetched := seedStaleCache(t, "", server.URL)

	lazy := LoadLazy(context.Background(), Options{BaseURL: server.URL})
	t.Cleanup(lazy.Close)
	if window := answerWithin(t, "ContextLength", func() int { return lazy.ContextLength("vendor/yesterday") }); window != 128000 {
		t.Fatalf("the day-old cache at home answered a window of %d, want its own 128000", window)
	}
	server.waitAsked(t)
	server.release()
	deadline := time.Now().Add(5 * time.Second)
	for {
		cached, ok := readCache(cachePath("", "", normalizeBase(server.URL)), "", server.URL)
		if ok && cached.FetchedAt.After(fetched) && len(cached.Models) == 1 && cached.Models[0].ID == "vendor/today" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the day-old cache found at home was answered and never refreshed")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
