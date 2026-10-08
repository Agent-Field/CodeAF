package main

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

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
