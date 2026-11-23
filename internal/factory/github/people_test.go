package github_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

func TestPeopleProfileTTLAndETagAndOutsideAuthor(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	reads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/outside" {
			t.Errorf("path = %s", r.URL.Path)
		}
		reads++
		if reads > 1 {
			if r.Header.Get("If-None-Match") != `"user-v1"` {
				t.Error("missing ETag")
			}
			w.WriteHeader(304)
			return
		}
		w.Header().Set("ETag", `"user-v1"`)
		fmt.Fprint(w, `{"name":"Outside Author","avatar_url":"https://avatar/outside","html_url":"https://github.com/outside"}`)
	}))
	defer srv.Close()
	loader := factorygithub.NewPeopleLoader(func(context.Context) factorygithub.PeopleAPI { return forge.NewClient("").WithBaseURL(srv.URL) }, func() time.Time { return now })
	first, err := loader.Profile(context.Background(), "@Outside")
	if err != nil || first.Name != "Outside Author" || first.HTMLURL == "" {
		t.Fatalf("profile = %+v %v", first, err)
	}
	now = now.Add(factorygithub.ProfileTTL - time.Second)
	if got, err := loader.Profile(context.Background(), "outside"); err != nil || got != first || reads != 1 {
		t.Fatalf("cached profile = %+v %v reads %d", got, err, reads)
	}
	now = now.Add(time.Second)
	if got, err := loader.Profile(context.Background(), "outside"); err != nil || got != first || reads != 2 {
		t.Fatalf("304 profile = %+v %v reads %d", got, err, reads)
	}
}

func TestCodeOwnersDefaultBranchPrecedenceAndNextPoll(t *testing.T) {
	path, text := "CODEOWNERS", "/api/ @outside\n"
	reads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads++
		if r.URL.Query().Get("ref") != "" {
			t.Error("ownership read from job branch")
		}
		if r.URL.Path != "/repos/acme/api/contents/"+path {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"type": "file", "encoding": "base64", "sha": "owners", "content": base64.StdEncoding.EncodeToString([]byte(text))})
	}))
	defer srv.Close()
	clock := time.Unix(1_800_000_000, 0)
	loader := factorygithub.NewPeopleLoader(func(context.Context) factorygithub.PeopleAPI { return forge.NewClient("stub").WithBaseURL(srv.URL) }, func() time.Time { return clock })
	ctx := context.Background()
	rules, err := loader.Owners(ctx, "acme/api")
	if err != nil || len(rules) != 1 || rules[0].Handles[0] != "outside" || reads != 2 {
		t.Fatalf("rules = %+v %v reads=%d", rules, err, reads)
	}
	rules[0].Handles[0] = "mutated"
	if rules, _ := loader.Owners(ctx, "acme/api"); rules[0].Handles[0] != "outside" || reads != 2 {
		t.Fatal("cache mutated or re-read")
	}
	// A file added where the forge last said there was none is read once
	// that answer is stale.
	path, text = ".github/CODEOWNERS", "* @mei\n"
	clock = clock.Add(factorygithub.MissingFresh)
	loader.RefreshOwners(ctx, "acme/api")
	if rules, _ := loader.Owners(ctx, "acme/api"); len(rules) != 1 || rules[0].Handles[0] != "mei" || reads != 3 {
		t.Fatalf("next poll = %+v reads %d", rules, reads)
	}
	path = "missing"
	loader.RefreshOwners(ctx, "acme/api")
	if rules, err := loader.Owners(ctx, "acme/api"); err != nil || len(rules) != 0 {
		t.Fatalf("deleted file = %+v %v", rules, err)
	}
}

// TestKnownNeverWaitsOnGitHubAndRemembersAFailure pins the law a list of
// people reads by: Known answers at once with what is already read, asks for
// the rest in the background, and a handle the forge would not answer is not
// asked for again on every read until ProfileRetry has passed.
func TestKnownNeverWaitsOnGitHubAndRemembersAFailure(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	var clock sync.Mutex
	at := func() time.Time { clock.Lock(); defer clock.Unlock(); return now }
	release := make(chan struct{})
	served := make(chan string, 16)
	var reads atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		<-release
		defer func() { served <- r.URL.Path }()
		if r.URL.Path == "/users/gone" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"name":"Known Person","avatar_url":"https://avatar/known"}`)
	}))
	defer srv.Close()
	defer close(release)
	loader := factorygithub.NewPeopleLoader(func(context.Context) factorygithub.PeopleAPI { return forge.NewClient("").WithBaseURL(srv.URL) }, at)

	// The forge is holding every answer, and still nothing waits on it.
	start := time.Now()
	if _, ok := loader.Known("known"); ok {
		t.Fatal("a profile nobody read is known")
	}
	if _, ok := loader.Known("gone"); ok {
		t.Fatal("a profile nobody read is known")
	}
	if waited := time.Since(start); waited > time.Second {
		t.Fatalf("Known waited %v on GitHub", waited)
	}
	release <- struct{}{}
	release <- struct{}{}
	for range 2 {
		select {
		case <-served:
		case <-time.After(10 * time.Second):
			t.Fatal("the background read never ran")
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, ok := loader.Known("known")
		if ok && got.Name == "Known Person" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the read profile never became known: %+v %v", got, ok)
		}
		runtime.Gosched()
	}

	// A handle the forge refused is asked for again only after the wait.
	// Until the reader has recorded the refusal the handle is still queued,
	// and after it the refusal holds it back, so no moment between the two
	// lets a list ask again.
	before := reads.Load()
	for range 5 {
		loader.Known("gone")
	}
	if got := reads.Load(); got != before {
		t.Fatalf("a refused profile was asked for again at once: %d reads, want %d", got, before)
	}
	clock.Lock()
	now = now.Add(factorygithub.ProfileRetry)
	clock.Unlock()
	// The reader may still be between the refusal and recording it, when the
	// handle counts as queued, so the list keeps asking as a list would.
	for deadline := time.Now().Add(10 * time.Second); reads.Load() == before; runtime.Gosched() {
		if time.Now().After(deadline) {
			t.Fatal("a refused profile was never asked for again")
		}
		loader.Known("gone")
	}
	release <- struct{}{}
	select {
	case path := <-served:
		if path != "/users/gone" {
			t.Fatalf("asked for %s, want the refused handle again", path)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a refused profile was never asked for again")
	}
}
