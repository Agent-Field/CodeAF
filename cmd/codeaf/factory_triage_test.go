package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/triage"
	"github.com/Agent-Field/codeaf/internal/home"
)

// A server has no journal from which to recover a pin. Its launch must supply
// it to the same call that reads size and estimate, ahead of the profile's crew.
func TestFactoryServerTriageRequestsItsOneModelPin(t *testing.T) {
	const pinned = "deepseek/deepseek-v4.1-flash"
	t.Setenv(home.EnvVar, t.TempDir())
	previous := factoryWorkerParent()
	t.Cleanup(func() { setFactoryProcess(func() { factoryProcess.parent = previous }) })
	requested := make(chan string, 4)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		requested <- req.Model
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"{\"read\":\"Fix the launch pin\",\"size\":\"S\",\"est_usd\":1}"},"finish_reason":"stop"}]}`)
	}))
	defer endpoint.Close()
	settings := config.Config{APIKey: "sk-test", BaseURL: endpoint.URL, Model: "profile/default", ProfileDir: oneModelProfile(t)}
	call, ok := factoryTriageCall(func() (config.Config, error) { return settings, nil })
	if !ok {
		t.Fatal("no triage call")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready, done := make(factoryReady, 1), make(chan error, 1)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	go func() {
		done <- serveFactory(ctx, configPath, "127.0.0.1:0", ready, pinned, true)
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("server did not start: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("server did not start")
	}
	st, err := store.Open(v3FactoryRoot())
	if err != nil {
		t.Fatal(err)
	}
	// The call was built before the server launch; it must read the current
	// pin on each request, just as it reads a changed key from the profile.
	for _, one := range []bool{true, false} {
		setFactoryProcess(func() { factoryProcess.parent.OneModel = one })
		it, err := st.Create(factory.Item{Repo: "owner/repo", Title: "Fix the launch pin", Kind: factory.KindIssue, State: factory.StateNew})
		if err != nil {
			t.Fatal(err)
		}
		triage.ReadOne(ctx, st, call, triage.Options{})
		want := pinned
		if !one {
			want = "tier/low"
		}
		select {
		case got := <-requested:
			if got != want {
				t.Errorf("one-model=%v requested %q; want %q", one, got, want)
			}
		default:
			t.Fatal("triage made no request")
		}
		read, err := st.Get(it.ID)
		if err != nil || read.Triage.Read != "Fix the launch pin" || read.Triage.Size != "S" || read.Triage.Est != 1 {
			t.Fatalf("triage did not record its read, size and estimate: %+v, %v", read.Triage, err)
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not stop")
	}
}

// TestFactoryTriageStartsNothingWithoutAKey pins the start predicate: with no
// key resolving, no call is built, the triage lock is never taken, and the
// wait ends with its context having started nothing.
func TestFactoryTriageStartsNothingWithoutAKey(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	noKey := func() (config.Config, error) { return config.Config{}, config.ErrNoAPIKey }
	if call, ok := factoryTriageCall(noKey); ok || call != nil {
		t.Fatal("a call was built with no key")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	call, release := waitForFactoryTriage(ctx, st, noKey, 5*time.Millisecond)
	if call != nil || release != nil {
		t.Fatal("the worker started with no key")
	}
	held, ok := st.TryTriager()
	if !ok {
		t.Fatal("the triage lock was left taken by a worker that never started")
	}
	held()
}

// TestFactoryTriageStartsOnceAKeyResolvesAndTakesTheLock is the other side:
// a key that resolves builds the call, and the worker holds the floor's one
// triage lock, so a second window waits.
func TestFactoryTriageStartsOnceAKeyResolvesAndTakesTheLock(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	keyed := func() (config.Config, error) { return config.Config{APIKey: "sk-test"}, nil }
	call, release := waitForFactoryTriage(context.Background(), st, keyed, time.Millisecond)
	if call == nil || release == nil {
		t.Fatal("no worker with a key")
	}
	defer release()
	if other, err := store.Open(st.Root()); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		if c, r := waitForFactoryTriage(ctx, other, keyed, 5*time.Millisecond); c != nil {
			r()
			t.Skip("this platform's lock does not exclude within one process")
		}
	}
}
