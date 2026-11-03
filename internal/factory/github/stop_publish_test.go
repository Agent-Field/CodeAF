package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
 "github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestStopPublishAfterForgePreparation(t *testing.T) {
	f := newFixture()
	stopped := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/repos/acme/api" {
			stopped = true
		}
		f.handle(w, r)
	}))
	defer srv.Close()
	api := forge.NewClient("test-token").WithBaseURL(srv.URL).WithSleep(func(context.Context, time.Duration) error { return nil })
	src := New(api, []string{"acme/api"}, nil)
	stopErr := errors.New("stopped during forge preparation")
	_, err := src.Write(context.Background(), factory.Action{Verb: "pr", Item: factory.Item{Repo: "acme/api", Num: 12, Title: "Stop publication"}, Branch: "feat", Draft: true, BeforeWrite: func(context.Context) error {
		if !stopped {
			t.Error("check ran before forge preparation")
		}
		return stopErr
	}})
	if !errors.Is(err, stopErr) {
		t.Errorf("Stop did not prevent PR request: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if got := f.hits["POST /repos/acme/api/pulls"]; got != 0 {
		t.Errorf("stopped job sent %d PR requests", got)
	}
}

func TestStopMergeAfterForgePreparation(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(fmt.Sprintf("stop=%v", stop), func(t *testing.T) {
			var prepared atomic.Bool
			var merges atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/repos/acme/api" {
					prepared.Store(true)
					_, _ = w.Write([]byte(`{"allow_squash_merge":true}`))
					return
				}
				if r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/merge") {
					merges.Add(1)
					_, _ = w.Write([]byte(`{"merged":true,"sha":"merged"}`))
					return
				}
				http.NotFound(w, r)
			}))
			defer srv.Close()
			api := forge.NewClient("test-token").WithBaseURL(srv.URL)
			_, err := New(api, []string{"acme/api"}, nil).Write(context.Background(), factory.Action{Verb: "merge", Item: mergeItem(), Commit: "approved", BeforeWrite: func(context.Context) error {
				if !prepared.Load() {
					t.Error("merge check ran before method lookup")
				}
				if stop {
					return factory.ErrPersonStopped
				}
				return nil
			}})
			if stop {
				if !errors.Is(err, factory.ErrPersonStopped) || merges.Load() != 0 {
					t.Fatalf("Stop during method lookup: writes=%d error=%v", merges.Load(), err)
				}
			} else if err != nil || merges.Load() != 1 {
				t.Fatalf("healthy merge: writes=%d error=%v", merges.Load(), err)
			}
		})
	}
}

func TestStopWhileSourceWaitsPreventsSimpleWrites(t *testing.T) {
 for _, verb := range []string{"comment", "label", "close"} {
  t.Run(verb, func(t *testing.T) {
   f := newFixture()
   srv := httptest.NewServer(http.HandlerFunc(f.handle)); defer srv.Close()
   src := New(forge.NewClient("test-token").WithBaseURL(srv.URL), []string{"acme/api"}, nil)
   authority, err := store.Open(t.TempDir()); if err != nil { t.Fatal(err) }
   it, err := authority.Create(factory.Item{Repo: "acme/api", Num: 12, Title: "held write"}); if err != nil { t.Fatal(err) }
   src.mu.Lock()
   entered, result := make(chan struct{}), make(chan error, 1)
   go func() {
    close(entered)
    _, err := src.Write(context.Background(), factory.Action{Verb: verb, Item: it, Body: "proof", Labels: []string{"verified"}, BeforeWrite: func(context.Context) error {
     cur, err := authority.Get(it.ID); if err != nil { return err }; return factory.CheckPersonStop(cur)
    }})
    result <- err
   }()
   <-entered
   // Write must pass repository selection after the person's durable Stop.
   if err := authority.Update(it.ID, func(cur *factory.Item) error { factory.MarkPersonStopped(cur); return nil }); err != nil { src.mu.Unlock(); t.Fatal(err) }
   src.mu.Unlock()
   if err := <-result; !errors.Is(err, factory.ErrPersonStopped) { t.Errorf("write ignored Stop: %v", err) }
   f.mu.Lock(); defer f.mu.Unlock()
   for path, hits := range f.hits { if !strings.HasPrefix(path, "GET ") && hits != 0 { t.Errorf("stopped job wrote %s %d times", path, hits) } }
  })
 }
}
