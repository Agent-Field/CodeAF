package worker_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/worker"
)

func TestTwoWorkersDeliverRepliesAndReportTheirManagerLines(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seam := factory.LocalSeam(st, time.Now())
	h := serve.New(seam, serve.Config{Mode: api.FactoryTeam, Store: st, PullWait: time.Millisecond})
	s := httptest.NewServer(h)
	t.Cleanup(func() { s.Close(); h.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan int, 2)
	release := make(chan struct{})
	done := make(chan struct{}, 2)
	var calls atomic.Int32
	var ids []int
	for i, person := range []string{"sam", "raj"} {
		mirror, err := store.Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		chat := filepath.Join(t.TempDir(), "manager.jsonl")
		it, err := st.Create(factory.Item{Title: person + "'s job", State: factory.StateLanded, Assignee: person, Talk: chat, WorktreeMachine: person + "-machine"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, it.ID)
		words := fmt.Sprintf("Explain job %d", it.ID)
		if err := client.Open(s.URL, "").Seam().Say(ctx, it.ID, words); err != nil {
			t.Fatal(err)
		}
		w := client.Open(s.URL, "").Worker("")
		opts := worker.Options{Slots: 1,
			Dial:   func(int) *client.WorkerClient { return w },
			Attach: api.WorkerAttach{Name: person, Machine: person + "-machine", Handle: person},
			Mirror: mirror, Runner: run.New(run.Options{Store: mirror}),
			ReportEvery: time.Millisecond, BeatEvery: 5 * time.Millisecond,
			Reply: func(ctx context.Context, got factory.Item, text string) error {
				if got.ID != it.ID || text != words {
					return fmt.Errorf("wrong worker received job %d: %s", got.ID, text)
				}
				calls.Add(1)
				started <- i
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-release:
				}
				data, err := json.Marshal(map[string]string{"type": "message", "role": "assistant", "content": "Proof explained for " + person, "timestamp": "2026-10-11T12:00:00Z"})
				if err != nil {
					return err
				}
				return os.WriteFile(chat, append(data, '\n'), 0600)
			},
		}
		go func() { defer func() { done <- struct{}{} }(); worker.Run(ctx, opts) }()
	}
	t.Cleanup(func() {
		cancel()
		for range 2 {
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("worker did not stop")
			}
		}
	})
	for range 2 {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("both managers did not receive their replies")
		}
	}
	for _, id := range ids {
		it, _ := st.Get(id)
		if len(it.Thread) != 1 || it.Thread[0].State != "pending" {
			t.Fatalf("reply did not stay pending during delivery: %+v", it.Thread)
		}
	}
	close(release)
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticks := time.NewTicker(5 * time.Millisecond)
	defer ticks.Stop()
	for {
		complete := true
		for _, id := range ids {
			it, err := st.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			if it.State != factory.StateLanded || it.Stream != nil {
				t.Fatalf("reply started job steps: %+v", it)
			}
			complete = complete && len(it.Thread) == 2 && it.Thread[0].State == "delivered" && it.Thread[1].From == "manager"
		}
		if complete {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("reply acknowledgment or manager report missing")
		case <-ticks.C:
		}
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("manager turns = %d; want one per reply", got)
	}
}
