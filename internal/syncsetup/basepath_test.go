package syncsetup_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/pairbox"
)

// A relay address with a path of its own (the hosted relay lives at
// https://host/fabric) must reach every wire beneath that path, whether or not
// the person typed a trailing slash. The server answers nothing useful; the test
// reads only which paths arrived.
func TestEveryClientKeepsTheRelayBasePath(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.Path)
		mu.Unlock()
		http.NotFound(w, r)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	nosign := func(*http.Request, []byte) {}
	for _, base := range []string{srv.URL + "/fabric", srv.URL + "/fabric/"} {
		seen = nil
		dir := directory.NewHTTP(base, nosign, http.DefaultClient)
		_, _ = dir.List(ctx)
		_, _ = dir.Watch(ctx)
		_, _ = blobstore.NewHTTP(base, nosign, nil).Stats(ctx)
		_, _ = pairbox.NewHTTP(base, http.DefaultClient).Limits(ctx)
		for _, path := range seen {
			if !strings.HasPrefix(path, "/fabric/v1/") || strings.Contains(path, "//") {
				t.Errorf("base %q sent %q", base, path)
			}
		}
		if len(seen) != 4 {
			t.Errorf("base %q: %d requests arrived, want 4: %v", base, len(seen), seen)
		}
	}
}
