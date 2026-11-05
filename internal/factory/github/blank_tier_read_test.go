package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

func TestReadFillsLegacyBlankTierAfterUnchangedPoll(t *testing.T) {
	f := newFixture()
	_, api := f.serve(t)
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	src := New(api, nil, nil)
	ctx := context.Background()
	cursor, err := PollOnce(ctx, src, st, "", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	items, err := st.List()
	if err != nil || len(items) != 1 {
		t.Fatalf("initial import: items=%d, err=%v", len(items), err)
	}
	id := items[0].ID
	// Reproduce a document saved before author tiers were populated.
	if err := st.Update(id, func(it *factory.Item) error {
		it.Tier = ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if cursor == "" {
		t.Fatal("initial import did not establish a changed-issues cursor")
	}
	if _, err := PollOnce(ctx, src, st, cursor, time.Now); err != nil {
		t.Fatal(err)
	}
	// Inspect the fixture's saved document without triggering the read under test.
	body, err := os.ReadFile(st.ItemPath(id))
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Item factory.Item `json:"item"`
	}
	if err := json.Unmarshal(body, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Item.Tier != "" {
		t.Fatalf("unchanged poll unexpectedly filled tier: %q", saved.Item.Tier)
	}
	got, err := st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture answers 404 for bob's collaborator permission: a known stranger.
	if got.Tier != factory.TierStranger {
		t.Fatalf("read left legacy author tier %q; want %q after unchanged poll", got.Tier, factory.TierStranger)
	}
}

func blankTierStore(t *testing.T, status int, tier factory.Tier, entered, release chan struct{}) (*store.Store, int, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer read-fixture" {
			t.Error("lookup did not use read credential")
		}
		switch r.URL.Path {
		case "/user":
			_, _ = w.Write([]byte(`{"login":"reader"}`))
		case "/repos/acme/api/collaborators/bob/permission":
			if hits.Add(1) == 1 && entered != nil {
				close(entered)
				<-release
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"permission":"write","message":"denied"}`))
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(srv.Close)
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	src := New(forge.NewClient("read-fixture").WithBaseURL(srv.URL), nil, nil)
	// Registration must happen even when no watched repository can be polled.
	if _, err := PollOnce(context.Background(), src, st, "", time.Now); err != nil {
		t.Fatal(err)
	}
	it, err := st.Create(factory.Item{Origin: factory.OriginForge, Product: "acme", Repo: "api", Author: "bob", Num: 7, Tier: tier, Title: "legacy job"})
	if err != nil {
		t.Fatal(err)
	}
	return st, it.ID, &hits
}

func TestBlankTierForbiddenRefusesSignOff(t *testing.T) {
	st, id, hits := blankTierStore(t, http.StatusForbidden, "", nil, nil)
	desk, err := lease.Open(st, lease.Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = lease.NewDoors(desk).SignOff(id, false)
	if err == nil || !strings.Contains(err.Error(), "Author access unknown:") || !strings.Contains(err.Error(), "; retry after ") {
		t.Fatalf("signoff refusal = %v; want unknown access and retry deadline", err)
	}
	got, err := st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tier != "" || got.Failure == nil || !strings.Contains(got.Failure.Detail, "Author access unknown:") {
		t.Fatalf("forbidden lookup: tier=%q failure=%+v", got.Tier, got.Failure)
	}
	if hits.Load() != 1 {
		t.Fatalf("permission lookups=%d; want 1 during backoff", hits.Load())
	}
	body, err := os.ReadFile(st.ItemPath(id))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "retry after") {
		t.Fatal("retry state leaked to saved job")
	}
}

func TestBlankTierNotFoundPersistsAcrossReopen(t *testing.T) {
	st, id, hits := blankTierStore(t, http.StatusNotFound, "", nil, nil)
	before, err := st.GetSaved(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get(id); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(st.Root())
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tier != factory.TierStranger || hits.Load() != 1 {
		t.Fatalf("reopened tier=%q lookups=%d", got.Tier, hits.Load())
	}
	if !got.Changed.Equal(before.Changed) {
		t.Fatal("tier fill changed job age")
	}
}

func TestReadNeverOverwritesSetTier(t *testing.T) {
	for _, tier := range []factory.Tier{factory.TierStranger, factory.TierCollab, factory.TierOwner} {
		t.Run(string(tier), func(t *testing.T) {
			st, id, hits := blankTierStore(t, http.StatusOK, tier, nil, nil)
			got, err := st.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			listed, err := st.List()
			if err != nil {
				t.Fatal(err)
			}
			if got.Tier != tier || len(listed) != 1 || listed[0].Tier != tier || hits.Load() != 0 {
				t.Fatalf("tier=%q listed=%+v lookups=%d", got.Tier, listed, hits.Load())
			}
		})
	}
}

func TestConcurrentBlankTierReadersShareLookup(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	st, id, hits := blankTierStore(t, http.StatusOK, "", entered, release)
	var wg sync.WaitGroup
	results := make(chan factory.Item, 2)
	for range 2 {
		wg.Go(func() {
			got, err := st.Get(id)
			if err != nil {
				t.Error(err)
			}
			results <- got
		})
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		wg.Wait()
		t.Fatal("read never looked up author permission")
	}
	close(release)
	wg.Wait()
	close(results)
	for got := range results {
		if got.Tier != factory.TierCollab {
			t.Errorf("tier=%q; want collaborator", got.Tier)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("lookups=%d; want 1", hits.Load())
	}
}

func TestTierLookupLeavesDeskAndItemUnlocked(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	finishLookup := func() { unblock.Do(func() { close(release) }) }
	st, id, hits := blankTierStore(t, http.StatusOK, "", entered, release)
	defer finishLookup()
	desk, err := lease.Open(st, lease.Options{})
	if err != nil {
		t.Fatal(err)
	}
	signoff := make(chan error, 1)
	go func() { _, err := lease.NewDoors(desk).SignOff(id, false); signoff <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("signoff never looked up author permission")
	}
	bookkeeping := make(chan error, 1)
	go func() {
		_, err := desk.Live()
		if err == nil {
			err = st.Annotate(id, func(it *factory.Item) error { it.Tier = factory.TierStranger; return nil })
		}
		bookkeeping <- err
	}()
	select {
	case err := <-bookkeeping:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("permission lookup held the desk or item lock")
	}
	finishLookup()
	if err := <-signoff; err == nil {
		t.Fatal("signoff overwrote a concurrently recorded stranger tier")
	}
	got, err := st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tier != factory.TierStranger || hits.Load() != 1 {
		t.Fatalf("tier=%q lookups=%d", got.Tier, hits.Load())
	}
}
