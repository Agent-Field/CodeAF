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
	old := poolJudgeSweepRun
	poolJudgeSweepRun = func(ctx context.Context, _ config.Config, profileDir, _ string, _ func() []catalog.Model, _ func(string) judge.Ask, _ func() time.Time) {
		started <- struct{}{}
		<-ctx.Done()
		pool := config.ProfilePath(profileDir, "pool")
		if err := os.MkdirAll(pool, 0o700); err != nil {
			t.Error(err)
			return
		}
		if err := os.WriteFile(filepath.Join(pool, "joined"), nil, 0o600); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { poolJudgeSweepRun = old })

	proc := v3TestProcess(t)
	if _, err := openV3Launch(proc, v3Options{Model: "test/model", Workspace: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	<-started
	pool := config.ProfilePath(proc.ProfileDir, "pool")
	proc.closeAll()
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
	old := poolJudgeSweepRun
	poolJudgeSweepRun = func(ctx context.Context, _ config.Config, profileDir, _ string, _ func() []catalog.Model, _ func(string) judge.Ask, _ func() time.Time) {
		mu.Lock()
		starts++
		mu.Unlock()
		started <- struct{}{}
		<-ctx.Done()
		pool := config.ProfilePath(profileDir, "pool")
		if err := os.MkdirAll(pool, 0o700); err != nil {
			t.Error(err)
			return
		}
		if err := os.WriteFile(filepath.Join(pool, "joined"), nil, 0o600); err != nil {
			t.Error(err)
		}
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
	t.Setenv("CODEAF_HOME", homeB)
	first.closeAll()
	poolB := filepath.Join(homeB, "pool")
	if err := os.RemoveAll(poolB); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(poolB); !os.IsNotExist(err) {
		t.Fatalf("survivor recreated the second home pool: %v", err)
	}

	second := open()
	second.closeAll()
	mu.Lock()
	got := starts
	mu.Unlock()
	if got != 2 {
		t.Fatalf("sweep starts = %d, want one per launch across two processes", got)
	}
}
