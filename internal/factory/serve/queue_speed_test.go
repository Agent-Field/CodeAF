package serve_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// bigFloor is a solo factory watching one busy repository: n open issues and
// pull requests read from GitHub, each with its own author and a few
// commenters, served through the same store, seam and handler a solo host
// builds. profiles counts the waiting public-profile reads a queue asks
// for, which on a real host are GitHub calls.
type bigFloor struct {
	h        *serve.Handler
	profiles atomic.Int64
}

func newBigFloor(tb testing.TB, n int, profile func(context.Context, string) (factory.GitHubPerson, error), known func(string) (factory.GitHubPerson, bool)) *bigFloor {
	tb.Helper()
	f := &bigFloor{}
	st, err := store.Open(tb.TempDir())
	if err != nil {
		tb.Fatal(err)
	}
	if err := st.SetRepos([]string{"acme/api"}); err != nil {
		tb.Fatal(err)
	}
	for i := 1; i <= n; i++ {
		kind, url := factory.KindIssue, fmt.Sprintf("https://github.com/acme/api/issues/%d", i)
		if i%3 == 0 {
			kind, url = factory.KindPR, fmt.Sprintf("https://github.com/acme/api/pull/%d", i)
		}
		it := factory.Item{Repo: "acme/api", Num: i, URL: url, Kind: kind, Origin: factory.OriginForge, Synced: true,
			Title: fmt.Sprintf("a reported problem number %d with a title of ordinary length", i),
			Body:  "Steps to reproduce: open the thing, press the key, watch it go wrong. Expected it to go right.",
			// Distinct authors are the ordinary shape of a public repository.
			Author: fmt.Sprintf("person-%d", i), Tier: factory.TierStranger, State: factory.StateNew,
			Created: now.Add(-time.Duration(i) * time.Hour), Changed: now.Add(-time.Duration(i) * time.Minute),
			Stages: factory.DefaultRecipe().Stages,
			Triage: factory.Triage{Type: "bug", Size: "M", Area: "tui", Readiness: 60, Read: "a short read of what it is"},
			Comments: []factory.Comment{
				{Author: fmt.Sprintf("person-%d", (i*7)%n+1), Body: "same here", At: now.Add(-time.Hour)},
				{Author: "maintainer", Body: "thanks, looking", At: now.Add(-time.Hour)},
			}}
		if kind == factory.KindPR {
			it.Files = []factory.FileChange{{Path: fmt.Sprintf("internal/pkg%d/file.go", i%20), Added: 3}}
		}
		if _, err := st.Create(it); err != nil {
			tb.Fatal(err)
		}
	}
	clock := func() time.Time { return now }
	rec, err := record.Open(st, record.Options{Now: clock})
	if err != nil {
		tb.Fatal(err)
	}
	seam := factory.LocalSeam(st, now)
	seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "sam"}, nil }
	f.h = serve.New(seam, serve.Config{Mode: api.FactorySolo, ServedBy: "app", AccessToken: "floor-secret", ProfileDir: tb.TempDir(),
		Address: config.Address, Version: "test", Store: st, Root: st.Root(), Record: rec,
		Now: clock, LeaseOptions: lease.Options{Now: clock, Record: rec}, OperatorLogin: "sam",
		PersonProfile: func(ctx context.Context, handle string) (factory.GitHubPerson, error) {
			f.profiles.Add(1)
			return profile(ctx, handle)
		},
		PersonKnown: known,
		CodeOwners: func(context.Context, string) ([]factory.CodeOwner, error) {
			return []factory.CodeOwner{{Pattern: "*", Handles: []string{"maintainer"}}}, nil
		}})
	tb.Cleanup(f.h.Close)
	return f
}

func (f *bigFloor) queue(tb testing.TB) []byte {
	tb.Helper()
	r := httptest.NewRequest("GET", "/v1/queue", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Authorization", "Bearer floor-secret")
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	if w.Code != 200 {
		tb.Fatalf("queue answered %d: %s", w.Code, w.Body.String())
	}
	return w.Body.Bytes()
}

// TestAQueueOfManyAuthorsNeverWaitsOnTheirProfiles is the regression behind
// a Jobs page that took ten seconds and more to open on a busy public
// repository: the queue asked GitHub for every author's profile, one after
// another, before answering. With PersonKnown the queue carries what is known
// and never calls the waiting door at all.
func TestAQueueOfManyAuthorsNeverWaitsOnTheirProfiles(t *testing.T) {
	f := newBigFloor(t, 120, func(context.Context, string) (factory.GitHubPerson, error) {
		t.Error("the queue waited on a profile read")
		return factory.GitHubPerson{}, nil
	}, func(handle string) (factory.GitHubPerson, bool) {
		if handle == "person-7" {
			return factory.GitHubPerson{PersonSummary: factory.PersonSummary{Name: "Seven"}}, true
		}
		return factory.GitHubPerson{}, false
	})
	var q api.Queue
	if err := json.Unmarshal(f.queue(t), &q); err != nil {
		t.Fatal(err)
	}
	if len(q.Jobs) != 120 {
		t.Fatalf("queue listed %d jobs, want 120", len(q.Jobs))
	}
	if got := q.People["person-7"].Name; got != "Seven" {
		t.Fatalf("a known author reads %q, want Seven", got)
	}
	if _, ok := q.People["person-8"]; !ok {
		t.Fatal("an author not known yet left the people map instead of drawing as a handle")
	}
	if f.profiles.Load() != 0 {
		t.Fatalf("the queue made %d waiting profile reads", f.profiles.Load())
	}
}

// BenchmarkQueue800 is a queue read of 800 open issues and pull requests,
// the size of a busy public repository, through the store, seam, handler and
// codec a solo host serves.
func BenchmarkQueue800(b *testing.B) {
	f := newBigFloor(b, 800, nil, func(string) (factory.GitHubPerson, bool) {
		return factory.GitHubPerson{PersonSummary: factory.PersonSummary{Name: "A Person"}}, true
	})
	b.ResetTimer()
	for b.Loop() {
		f.queue(b)
	}
}
