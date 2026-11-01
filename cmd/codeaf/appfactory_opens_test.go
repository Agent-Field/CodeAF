package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// openWithin is how long opening the app may take to list the factory on this
// Mac and read its jobs, from the floor on disk.
const openWithin = time.Second

// hangingGH puts a gh first on PATH that never answers in time, the way a gh
// waiting on a keychain or a dead network behaves.
func hangingGH(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// THE OWNER'S MAC: the factory never came up because the clean-up of old PR
// jobs, run before the factory served, failed on one job the same way at every
// launch. The row kept the last launch's dead port, and every read went there.
// Housekeeping that stalls and then fails, a gh that hangs, and the dead port
// in the row: the list and the jobs still answer at once, from the floor.
func TestTheFactoryOpensAtOnceWhatEverHousekeepingAndGitHubDo(t *testing.T) {
	s := newLocalSetup(t)
	st := s.start()
	floor, err := store.Open(v3FactoryRoot())
	if err != nil {
		t.Fatal(err)
	}
	issue, err := floor.Create(factory.Item{Kind: factory.KindIssue, Repo: "acme/api", Num: 7, Title: "keep me", State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	pr, err := floor.Create(factory.Item{Kind: factory.KindPR, Repo: "acme/api", Num: 65, Title: "old PR job", State: factory.StateQueued})
	if err != nil {
		t.Fatal(err)
	}
	// A live process (this one) holds the PR job in its worker mirror, so the
	// clean-up waits for it to let go and then gives up with an error.
	mirror := filepath.Join(floor.Root(), "workers", strconv.Itoa(os.Getpid()), "test")
	if err := os.MkdirAll(mirror, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mirror, strconv.Itoa(pr.ID)+".json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := s.backend.local.origin
	s.backend.Close()

	hangingGH(t)
	w, _ := reopened(t, s.backend.factoryHub())
	began := time.Now()
	var list appwire.FactoryList
	w.must("factory.list", struct{}{}, &list)
	var q api.Queue
	w.must("factory.queue", map[string]any{"factory": st.Factory}, &q)
	took := time.Since(began)

	var own appwire.FactoryConnection
	for _, c := range list.Factories {
		if c.ID == appLocalID {
			own = c
		}
	}
	if own.State != appwire.FactoryOK {
		t.Fatalf("the factory on this Mac is %q: %+v", own.State, own)
	}
	if own.URL == stale {
		t.Fatalf("the row still names the last launch's address %s", stale)
	}
	found := false
	for _, j := range q.Jobs {
		found = found || j.ID == issue.ID
	}
	if !found {
		t.Fatalf("the queue lost the issue: %v", jobIDs(q))
	}
	if took > openWithin {
		t.Fatalf("opening took %v, want under %v", took, openWithin)
	}
}

// GitHub that does not answer (a hung gh, no network) never holds the factory
// up: the person the factory knows is the one GitHub last named, and GitHub is
// asked again behind it.
func TestTheFactoryOpensAtOnceWhenGitHubDoesNotAnswer(t *testing.T) {
	s := newLocalSetup(t)
	st := s.start()
	var me appwire.FactoryList
	s.must("factory.list", struct{}{}, &me)
	s.backend.Close()

	hangingGH(t)
	w, _ := reopened(t, s.backend.factoryHub())
	asked := make(chan struct{}, 16)
	w.backend.local.seamHook = func(seam factory.Seam) factory.Seam {
		seam.GitHub = func(ctx context.Context) (factory.GitHubLink, error) {
			asked <- struct{}{}
			<-ctx.Done()
			return factory.GitHubLink{}, ctx.Err()
		}
		return seam
	}
	began := time.Now()
	var list appwire.FactoryList
	w.must("factory.list", struct{}{}, &list)
	var q api.Queue
	w.must("factory.queue", map[string]any{"factory": st.Factory}, &q)
	took := time.Since(began)
	var own appwire.FactoryConnection
	for _, c := range list.Factories {
		if c.ID == appLocalID {
			own = c
		}
	}
	if own.State != appwire.FactoryOK || own.Name != localFactoryName("owner") {
		t.Fatalf("the factory on this Mac: %+v", own)
	}
	if took > openWithin {
		t.Fatalf("opening took %v with GitHub not answering, want under %v", took, openWithin)
	}
	select {
	case <-asked:
	case <-time.After(5 * time.Second):
		t.Fatal("GitHub was never asked again behind the open")
	}
}
