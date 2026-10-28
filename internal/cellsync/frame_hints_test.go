package cellsync

import (
	"context"
	"errors"
	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestWarmHintsUseProductionAdapterChainAndFallback(t *testing.T) {
	for _, mode := range []string{"valid", "stale", "missing", "overflow", "unsupported"} {
		t.Run(mode, func(t *testing.T) {
			r := newRig(t)
			r.pub.WarmFrameHints = true
			first := r.publishFirst(map[string]string{"a": "original"})
			server := httptest.NewServer(blobstore.Handler(func(*http.Request, []byte) (string, string, error) { return "fixture", "fixture", nil }, func(string) (blobstore.Store, error) { return r.mem, nil }))
			defer server.Close()
			counters := &blobstore.Counters{}
			store := ObserveStore(&blobstore.Resuming{Inner: blobstore.Counting{Inner: blobstore.NewHTTP(server.URL, func(*http.Request, []byte) {}, server.Client()), C: counters}}, func(TraceEvent) {})
			f, c := fetcherFor(t, store)
			f.Dir = r.dir.For(devB)
			f.WarmFrameHints = true
			if err := f.Fetch(context.Background(), c, first); err != nil {
				t.Fatal(err)
			}
			second := r.seal(map[string]string{"a": "updated", "b": "new output"})
			if err := r.pub.Publish(context.Background(), r.drv, second, r.info()); err != nil {
				t.Fatal(err)
			}
			view := r.head(cellID)
			if len(view.FrameHints) == 0 {
				t.Fatal("opt-in publisher omitted latest small frames")
			}
			for _, hint := range view.FrameHints {
				raw, err := r.mem.GetFrame(context.Background(), hint.ID)
				if err != nil || int64(len(raw)) != hint.Bytes {
					t.Fatalf("descriptor not exact uploaded frame: %v", err)
				}
			}
			before := counters.Gets.Load()
			switch mode {
			case "stale":
				second = first
			case "missing":
				_, err := r.dir.For(devA).Publish(context.Background(), cellID, directory.Publish{Fence: r.drv.Fence, OldHead: second, Head: second})
				if err != nil {
					t.Fatal(err)
				}
			case "overflow":
				_, err := r.dir.For(devA).Publish(context.Background(), cellID, directory.Publish{Fence: r.drv.Fence, OldHead: second, Head: second, FrameHints: []directory.FrameHint{{ID: view.FrameHints[0].ID, Bytes: math.MaxInt64}}})
				if err != nil {
					t.Fatal(err)
				}
			case "unsupported":
				f.Store = r.mem
			}
			if err := f.Fetch(context.Background(), c, second); err != nil {
				t.Fatal(err)
			}
			want := "updated"
			if mode == "stale" {
				want = "original"
			}
			got, err := os.ReadFile(filepath.Join(c.Root, "a"))
			if err != nil || string(got) != want {
				t.Fatalf("fallback materialization %q: %v", got, err)
			}
			if mode == "valid" && counters.Gets.Load()-before != 1 {
				t.Fatalf("warm delta expected one bounded frame fetch, got %d", counters.Gets.Load()-before)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := f.Fetch(ctx, c, second); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel lost: %v", err)
			}
		})
	}
}

type cancelHintStore struct {
	blobstore.Store
	cancel             context.CancelFunc
	bounded, unbounded int
}

func (s *cancelHintStore) GetFrame(context.Context, string) ([]byte, error) {
	s.unbounded++
	return nil, errors.New("must not fall back")
}
func (s *cancelHintStore) GetFrameBounded(ctx context.Context, id string, _ int64) ([]byte, error) {
	s.bounded++
	raw, err := s.Store.GetFrame(ctx, id)
	s.cancel()
	return raw, err
}
func TestWarmHintsCancellationRemovesOwnedAttempt(t *testing.T) {
	r := newRig(t)
	r.pub.WarmFrameHints = true
	first := r.publishFirst(map[string]string{"a": "one"})
	f, c := fetcherFor(t, r.mem)
	f.Dir = r.dir.For(devB)
	if err := f.Fetch(context.Background(), c, first); err != nil {
		t.Fatal(err)
	}
	next := r.seal(map[string]string{"a": "two"})
	if err := r.pub.Publish(context.Background(), r.drv, next, r.info()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &cancelHintStore{Store: r.mem, cancel: cancel}
	f.Store = store
	f.WarmFrameHints = true
	if err := f.Fetch(ctx, c, next); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel lost: %v", err)
	}
	if store.bounded != 1 || store.unbounded != 0 {
		t.Fatal("bounded capability not preserved")
	}
	entries, err := os.ReadDir(filepath.Dir(f.Inbox(c)))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != c.ID {
			t.Fatalf("retained attempt inbox %s", entry.Name())
		}
	}
}
