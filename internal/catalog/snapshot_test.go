package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCapabilitySnapshotUsesCachedRowsWhileRefreshWaits(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"new/image","architecture":{"input_modalities":["text"],"output_modalities":["image"]}}]}`))
	}))
	defer server.Close()
	opts := Options{BaseURL: server.URL, Dir: t.TempDir(), Refresh: true}
	if err := Remember(opts, []Model{{ID: "cached/image", InputModalities: []string{"text"}, OutputModalities: []string{"image"}}}); err != nil {
		t.Fatal(err)
	}
	c := LoadLazy(context.Background(), opts)
	defer c.Close()
	defer close(release)
	<-entered
	snapshot := c.SnapshotNow()
	if !snapshot.Supports("cached/image", "output", "image") {
		t.Fatal("a pending refresh hid cached media capability")
	}
	if c.BlockingReads() != 0 {
		t.Fatal("snapshot entered the waiting catalog path")
	}
	if len(c.ModelsNow()) != 0 {
		t.Fatal("the fresh-only listing changed its contract")
	}
}

func TestCapabilitySnapshotNeverBorrowsDefaultRowsForACustomService(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := LoadLazy(ctx, Options{BaseURL: "https://custom.invalid/v1", Dir: t.TempDir()})
	defer c.Close()
	if len(c.SnapshotNow().ModelsWithOutput("image")) != 0 {
		t.Fatal("custom service inherited default media")
	}
	defaults := LoadLazy(ctx, Options{Dir: t.TempDir()})
	defer defaults.Close()
	if len(defaults.SnapshotNow().ModelsWithOutput("image")) == 0 {
		t.Fatal("default service lost its curated offline capabilities")
	}
}
