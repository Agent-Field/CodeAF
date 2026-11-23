package knowledge

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type countedHistory struct {
	consentEmptyHistory
	looks atomic.Int64
}

func (h *countedHistory) Inventory(ctx context.Context) ([]Journal, error) {
	h.looks.Add(1)
	return h.consentEmptyHistory.Inventory(ctx)
}

// A QUIET VAULT LOOKS LESS AND LESS OFTEN, AND A BUILD WAKES IT AT ONCE. The
// fifth live A+B run's idle engine spent part of its 21% here: every two
// seconds, for as long as the engine lived, the pass listed every saved chat,
// stat'd each one and opened the knowledge database, though nothing had
// changed since the last pass. A pass that finds the same chats as the last
// one now doubles the wait, up to [buildQuietMax]; anything new, or a person
// pressing Build, starts it again from [buildEvery].
func TestQuietVaultBacksOffAndBuildWakesIt(t *testing.T) {
	every, most := buildEvery, buildQuietMax
	buildEvery, buildQuietMax = 10*time.Millisecond, 160*time.Millisecond
	t.Cleanup(func() { buildEvery, buildQuietMax = every, most })
	v := Open(t.TempDir())
	h := &countedHistory{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- v.Run(ctx, h, func() string { return "" }) }()
	time.Sleep(time.Second)
	// At a fixed 10ms a second is about a hundred passes; backing off to
	// 160ms makes it about a dozen.
	if n := h.looks.Load(); n > 25 {
		t.Fatalf("a quiet vault made %d passes in a second, want it to back off", n)
	}
	before := h.looks.Load()
	if err := Open(v.profile).StartBuild(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(80 * time.Millisecond)
	for h.looks.Load() == before && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if h.looks.Load() == before {
		t.Fatal("pressing Build did not wake the quiet vault")
	}
	cancel()
	<-done
}
