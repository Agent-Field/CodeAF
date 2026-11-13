package knowledge

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

// countingHistory is saved chats that count how often they were looked at.
type countingHistory struct {
	inventories, reads atomic.Int64
	chats              atomic.Int64
}

func (h *countingHistory) Inventory(context.Context) ([]Journal, error) {
	h.inventories.Add(1)
	out := []Journal{}
	for i := range h.chats.Load() {
		out = append(out, Journal{ID: string(rune('a' + i)), Size: 1})
	}
	return out, nil
}

func (h *countingHistory) Read(_ context.Context, j Journal, _ JournalCursor) (JournalBatch, error) {
	h.reads.Add(1)
	return JournalBatch{Sources: []Source{{ID: j.ID}}, Cursor: JournalCursor{Offset: j.Size}}, nil
}

func waitChats(t *testing.T, v *Vault, want int) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		s, err := v.Status()
		if err != nil {
			t.Fatal(err)
		}
		if s.Build.Chats == want && s.Build.Phase == BuildUnbuilt {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	s, _ := v.Status()
	t.Fatalf("the count never reached %d: %+v", want, s.Build)
}

// With Memory off nothing reads saved chats in the background. The count is
// taken when the Memory page asks, once, and it stays for the next visit.
func TestMemoryOffReadsNoChatsUntilThePageAsks(t *testing.T) {
	profile := t.TempDir()
	// Unset profiles enable Memory; this fixture exercises an explicit off choice.
	if err := config.WriteMemoryEnabled(profile, false); err != nil {
		t.Fatal(err)
	}
	v := Open(profile)
	if v.Enabled() {
		t.Fatal("this profile should start with Memory off")
	}
	history := &countingHistory{}
	history.chats.Store(2)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = v.Run(ctx, history, func() string { return "" }) }()
	t.Cleanup(func() { cancel(); <-done })

	// Two passes run two seconds apart.
	time.Sleep(2300 * time.Millisecond)
	if n, r := history.inventories.Load(), history.reads.Load(); n != 0 || r != 0 {
		t.Fatalf("with Memory off and nobody asking, the worker took %d inventories and %d reads", n, r)
	}

	if err := v.AskDiscovery(); err != nil {
		t.Fatal(err)
	}
	waitChats(t, v, 2)
	time.Sleep(2300 * time.Millisecond)
	if n := history.inventories.Load(); n != 1 {
		t.Fatalf("one ask took %d inventories, want 1", n)
	}
	waitChats(t, v, 2) // the last count stays

	history.chats.Store(3)
	if err := v.AskDiscovery(); err != nil {
		t.Fatal(err)
	}
	waitChats(t, v, 3)
}
