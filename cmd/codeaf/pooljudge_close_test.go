package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/pool/judge"
)

func TestJudgeSweepStopsAndJoinsAtRealProcessClose(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	wrote := make(chan struct{})
	old := poolJudgeSweepRun
	poolJudgeSweepRun = func(ctx context.Context, _ config.Config, profileDir, _ string, _ func() []catalog.Model, _ func(string) judge.Ask, _ func() time.Time) {
		started <- struct{}{}
		<-release
		pool := config.ProfilePath(profileDir, "pool")
		if err := os.MkdirAll(pool, 0o700); err != nil {
			t.Error(err)
			return
		}
		if err := os.WriteFile(filepath.Join(pool, "joined"), nil, 0o600); err != nil {
			t.Error(err)
		}
		close(wrote)
	}
	t.Cleanup(func() { poolJudgeSweepRun = old })

	proc := v3TestProcess(t)
	if _, err := openV3Launch(proc, v3Options{Model: "test/model", Workspace: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	<-started
	pool := config.ProfilePath(proc.ProfileDir, "pool")
	closed := make(chan struct{})
	go func() {
		proc.closeAll()
		close(closed)
	}()
	select {
	case <-closed:
		close(release)
		<-wrote
		t.Fatal("Close returned while the judge sweep could still write")
	case <-time.After(1 * time.Second):
	}
	close(release)
	<-wrote
	<-closed
	if err := os.RemoveAll(pool); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pool); !os.IsNotExist(err) {
		t.Fatalf("pool was recreated after close: %v", err)
	}
}

func TestJudgeSweepCannotCrossHomesAndLaterProcessStartsItsOwn(t *testing.T) {
	homeA, homeB := t.TempDir(), t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_HOME", homeA)
	t.Setenv("CODEAF_PROFILE_DIR", "")
	t.Setenv("OPENROUTER_API_KEY", "test-key")

	var mu sync.Mutex
	starts := 0
	started := make(chan struct{}, 2)
	releases := make(chan chan struct{}, 2)
	writes := make(chan string, 2)
	old := poolJudgeSweepRun
	poolJudgeSweepRun = func(ctx context.Context, _ config.Config, profileDir, _ string, _ func() []catalog.Model, _ func(string) judge.Ask, _ func() time.Time) {
		mu.Lock()
		starts++
		mu.Unlock()
		started <- struct{}{}
		release := make(chan struct{})
		releases <- release
		<-release
		pool := config.ProfilePath(profileDir, "pool")
		if err := os.MkdirAll(pool, 0o700); err != nil {
			t.Error(err)
			return
		}
		if err := os.WriteFile(filepath.Join(pool, "joined"), nil, 0o600); err != nil {
			t.Error(err)
		}
		writes <- pool
	}
	t.Cleanup(func() { poolJudgeSweepRun = old })

	open := func() *v3Process {
		p, err := openV3Process("chat")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := openV3Launch(p, v3Options{Model: "test/model", Workspace: t.TempDir()}); err != nil {
			t.Fatal(err)
		}
		<-started
		return p
	}
	first := open()
	firstRelease := <-releases
	t.Setenv("CODEAF_HOME", homeB)
	firstClosed := make(chan struct{})
	go func() {
		first.closeAll()
		close(firstClosed)
	}()
	select {
	case <-firstClosed:
		close(firstRelease)
		if got := <-writes; got != filepath.Join(homeB, "pool") {
			t.Fatalf("survivor wrote under %q, want second home", got)
		}
		t.Fatal("first Close returned while its sweep could write into the second home")
	case <-time.After(1 * time.Second):
	}
	close(firstRelease)
	<-writes
	<-firstClosed
	poolB := filepath.Join(homeB, "pool")
	if err := os.RemoveAll(poolB); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(poolB); !os.IsNotExist(err) {
		t.Fatalf("survivor recreated the second home pool: %v", err)
	}

	second := open()
	secondRelease := <-releases
	secondClosed := make(chan struct{})
	go func() {
		second.closeAll()
		close(secondClosed)
	}()
	select {
	case <-secondClosed:
		close(secondRelease)
		<-writes
		t.Fatal("second Close returned while its sweep could still write")
	case <-time.After(1 * time.Second):
	}
	close(secondRelease)
	<-writes
	<-secondClosed
	mu.Lock()
	got := starts
	mu.Unlock()
	if got != 2 {
		t.Fatalf("sweep starts = %d, want one per launch across two processes", got)
	}
}

// TestJudgeLandingLeftUnjudgedWhenCancelledMidJudge proves the now-cancellable
// sweep does not burn a landing's judgement: when a close cancels the context
// mid-judge every candidate fails with no score, and the landing must be left
// unjudged for the next start rather than marked judged forever.
func TestJudgeLandingLeftUnjudgedWhenCancelledMidJudge(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_MODEL_POOL", "on")
	t.Setenv("CODEAF_MODEL_POOL_SUBMIT_URL", "http://127.0.0.1:1/submit")

	profileDir := t.TempDir()
	poolDir := config.ProfilePath(profileDir, "pool")
	settings := config.Config{APIKey: "k"}
	landing := poolTestLanding()
	landing.ID = 77

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the close has already cancelled the sweep's context
	ask := func(string) judge.Ask {
		return func(actx context.Context, _, _ string) (string, error) {
			<-actx.Done()
			return "", actx.Err()
		}
	}
	poolJudgeLandingContext(ctx, settings, profileDir, poolTestCatalog, ask, time.Now, "do", landing)

	if alreadyJudged(poolDir, landing.ID, landing.Attempt) {
		t.Fatal("a landing cancelled mid-judge was marked judged; it will never be scored or rejudged")
	}
}
