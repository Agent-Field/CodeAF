package runs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/realtime"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestCloseUnsubscribesFromHub(t *testing.T) {
	root := t.TempDir()
	hub := &shutdownHub{Hub: realtime.New(svc.Config{}, nil), canceled: make(chan struct{})}
	s := New(svc.Config{Root: root}, &svc.Services{Hub: hub})
	t.Cleanup(s.Close)
	s.Close()
	select {
	case <-hub.canceled:
	default:
		t.Fatal("Close left the trigger subscription open")
	}
}

type shutdownHub struct {
	svc.Hub
	canceled chan struct{}
}

func (h *shutdownHub) Subscribe(topic string) (<-chan svc.Event, func()) {
	events, cancel := h.Hub.Subscribe(topic)
	return events, func() {
		cancel()
		close(h.canceled)
	}
}

func TestCloseCancelsAndWaitsForRuns(t *testing.T) {
	for _, source := range []string{"manual", "record"} {
		t.Run(source, func(t *testing.T) {
			h := newHarness(t)
			model := &shutdownModel{
				entered: make(chan struct{}), canceled: make(chan struct{}), release: make(chan struct{}),
			}
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(model.release) }) }
			t.Cleanup(release)
			h.all.Model = model
			if source == "manual" {
				go func() {
					_, _ = h.runs.Start(context.Background(), h.scope(), bridge.StartRunParams{Run: "follow-up"})
				}()
			} else {
				_, err := h.recs.Update(h.ctx, h.scope(), bridge.UpdateRecordParams{
					Type: "deal", ID: "dl_hooli", Set: map[string]any{"stage": "Proposal"},
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			awaitShutdown(t, model.entered, "model call")
			closed := make(chan struct{})
			go func() { h.runs.Close(); close(closed) }()
			awaitShutdown(t, model.canceled, "run cancellation")
			select {
			case <-closed:
				t.Fatal("Close returned before the model call exited")
			default:
			}
			release()
			awaitShutdown(t, closed, "Close")
			h.runs.Close()
			if len(h.eng.dbs) != 0 || len(h.eng.busy) != 0 {
				t.Fatal("Close left journals or runs open")
			}
			enginesMu.Lock()
			_, retained := engines[h.root]
			enginesMu.Unlock()
			if retained {
				t.Fatal("Close retained the engine")
			}
		})
	}
}

func TestClosePreventsJournalWrites(t *testing.T) {
	h := newHarness(t)
	h.runs.Close()
	scope := h.scope()
	scope.App = "after-close"
	for _, tt := range []struct {
		name string
		call func() error
	}{
		{"start", func() error {
			_, err := h.runs.Start(context.Background(), scope, bridge.StartRunParams{Run: "follow-up"})
			return err
		}},
		{"get", func() error {
			_, err := h.runs.Get(context.Background(), scope, bridge.GetRunParams{RunID: "missing"})
			return err
		}},
		{"approval", func() error {
			_, err := h.inbox.Open(context.Background(), scope, bridge.Approval{Title: "Send"})
			return err
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); err == nil {
				t.Fatal("closed service accepted a journal operation")
			}
		})
	}
	if _, err := os.Stat(filepath.Join(h.root, scope.App)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("closed service created an app directory: %v", err)
	}
}

type shutdownModel struct {
	entered  chan struct{}
	canceled chan struct{}
	release  chan struct{}
}

func (m *shutdownModel) Complete(ctx context.Context, _ svc.ModelCall) (svc.ModelReply, error) {
	close(m.entered)
	<-ctx.Done()
	close(m.canceled)
	<-m.release
	return svc.ModelReply{}, ctx.Err()
}

func awaitShutdown(t *testing.T, ch <-chan struct{}, action string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", action)
	}
}
