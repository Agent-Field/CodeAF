package catalog

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// A lazily loaded catalog has to answer exactly what an eagerly loaded one
// answers. The only difference the caller may observe is when it waits.

func TestLoadLazyDoesNotWaitForTheFetchAndAnswersTheSame(t *testing.T) {
	release := make(chan struct{})
	served := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case served <- struct{}{}:
		default:
		}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"vendor/seer","name":"Seer",
			 "architecture":{"input_modalities":["text","image"],"output_modalities":["text"]},
			 "pricing":{"prompt":"0.001","completion":"0.002","request":"0.25"}}]}`))
	}))
	defer server.Close()

	options := Options{BaseURL: server.URL, Dir: t.TempDir()}

	start := time.Now()
	lazy := LoadLazy(context.Background(), options)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("LoadLazy blocked for %s; it must hand back a value immediately", elapsed)
	}

	// The fetch is already in flight before any question is asked.
	select {
	case <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("LoadLazy never started the fetch")
	}
	close(release)

	if !lazy.Supports("vendor/seer", "input", "image") {
		t.Fatal("lazy catalog lost a modality the fetch advertised")
	}
	model, ok := lazy.Model("vendor/seer")
	if !ok || model.RequestPrice != 0.25 {
		t.Fatalf("lazy catalog returned %+v (found %v)", model, ok)
	}
	if got := lazy.ModelsWithOutput("text"); len(got) != 1 || got[0].ID != "vendor/seer" {
		t.Fatalf("lazy listing returned %+v", got)
	}
	if lazy.Supports("vendor/absent", "input", "image") {
		t.Fatal("an unknown model must answer false")
	}
	if lazy.Supports("vendor/seer", "sideways", "image") {
		t.Fatal("an unknown direction must answer false")
	}
}

// A warm from a disk cache is a local read, and a caller that waits for it
// within a bound gets the rows rather than an empty answer.
func TestWarmedWaitsForACachedCatalogWithinTheBound(t *testing.T) {
	dir := t.TempDir()
	if err := Remember(Options{Dir: dir}, []Model{
		{ID: "vendor/seer", Name: "Seer",
			InputModalities: []string{"text"}, OutputModalities: []string{"text"}},
	}); err != nil {
		t.Fatal(err)
	}
	lazy := LoadLazy(context.Background(), Options{Dir: dir})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if !lazy.Warmed(ctx) {
		t.Fatal("the warm from a fresh disk cache did not land within the bound")
	}
	if got := lazy.ModelsNow(); len(got) != 1 || got[0].ID != "vendor/seer" {
		t.Fatalf("the warmed catalog answers %v, want the cached row", got)
	}
}

// A fetch that never returns never lands, and Warmed says so at the bound —
// the caller's cue to fall back — and then says the opposite once the warm
// finally lands. The bound is the caller's, honoured here and nowhere else.
func TestWarmedAnswersFalseAtTheBoundAndTrueWhenTheWarmLands(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	letGo := func() { once.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"vendor/seer","name":"Seer",
			 "pricing":{"prompt":"0.001","completion":"0.002"}}]}`))
	}))
	defer server.Close()
	defer letGo()

	lazy := LoadLazy(context.Background(), Options{BaseURL: server.URL, Dir: t.TempDir()})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if lazy.Warmed(ctx) {
		t.Fatal("a fetch that never returned answered warmed")
	}
	letGo()
	landed, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !lazy.Warmed(landed) {
		t.Fatal("the warm landed and Warmed still said no")
	}
	if got := lazy.ModelsNow(); len(got) != 1 || got[0].ID != "vendor/seer" {
		t.Fatalf("the warmed catalog answers %v, want the fetched row", got)
	}
}

// A catalog that resolved eagerly has nothing to wait for, and no catalog at
// all has nothing to say: both answer at once, in that order.
func TestWarmedAnswersAtOnceForAnEagerCatalogAndNeverForNil(t *testing.T) {
	eager := &Catalog{ready: newRows([]Model{{ID: "vendor/seer", OutputModalities: []string{"text"}}})}
	if !eager.Warmed(context.Background()) {
		t.Fatal("an already resolved catalog answered not warmed")
	}
	var absent *Catalog
	if absent.Warmed(context.Background()) {
		t.Fatal("a nil catalog answered warmed")
	}
}

// Cleaning dedupes on the literal id, so a slug and its "~" pinned variant can
// both survive into the list. A scan answered with the first of them; the index
// has to agree.
func TestIndexKeepsTheFirstRowForANormalizedID(t *testing.T) {
	indexed := &Catalog{ready: newRows([]Model{
		{ID: "~vendor/pinned", OutputModalities: []string{"text"}},
		{ID: "vendor/pinned", OutputModalities: []string{"image"}},
	})}
	model, ok := indexed.Model("vendor/pinned")
	if !ok {
		t.Fatal("the pinned variant should resolve by its bare slug")
	}
	if model.ID != "~vendor/pinned" {
		t.Fatalf("index returned %q, want the first matching row", model.ID)
	}
	if !indexed.Supports("vendor/pinned", "output", "text") {
		t.Fatal("Supports must read the same row Model does")
	}
}

func TestCloseJoinsLateSuccessfulWarmWithoutWritingEitherHome(t *testing.T) {
	oldHome := t.TempDir()
	newHome := t.TempDir()
	t.Setenv("CODEAF_HOME", oldHome)

	started := make(chan struct{})
	release := make(chan struct{})
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		close(started)
		<-release
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     http.StatusText(http.StatusOK),
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(catalogPayload)),
		}, nil
	})}
	first := LoadLazy(context.Background(), Options{
		BaseURL: "https://catalog.example/v1", HTTPClient: client,
	})
	<-started
	t.Setenv("CODEAF_HOME", newHome)

	closed := make(chan struct{})
	go func() {
		first.Close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("Close returned while the warm fetch was still running")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not join the completed warm fetch")
	}
	assertEmptyTree(t, oldHome)
	assertEmptyTree(t, newHome)

	second := LoadLazy(context.Background(), Options{
		BaseURL:    "https://catalog.example/v1",
		HTTPClient: catalogClient(t, http.StatusOK, catalogPayload, nil),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !second.Warmed(ctx) {
		t.Fatal("a second launch did not warm")
	}
	second.Close()
	entries, err := os.ReadDir(newHome)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("the second launch did not write its cache")
	}
}

func assertEmptyTree(t *testing.T, root string) {
	t.Helper()
	var found string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != root {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found != "" {
		t.Fatalf("late warm wrote %s", found)
	}
}
