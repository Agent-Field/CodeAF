package directory_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// feedless hides everything a Directory offers beyond the base interface, so
// it stands for a relay built before the watch feed existed.
type feedless struct{ directory.Directory }

// A relay without the feed has no watch route at all, and says so the way any
// unknown route does, which is how a client learns to keep polling.
func TestWatchOldRelayIs404(t *testing.T) {
	open := func(string) (directory.Directory, error) {
		return feedless{directory.NewMemory(time.Now)}, nil
	}
	srv := httptest.NewServer(directory.Handler(fakeAuth, open))
	t.Cleanup(srv.Close)
	req, err := http.NewRequest(http.MethodGet, srv.URL+directory.WatchPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	fakeSign("id_one", devA)(req, nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("watch on a relay without the feed answered %d, want 404", resp.StatusCode)
	}
}
