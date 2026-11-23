package syncsetup

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
)

func TestQAStorageOffBackgroundPublicationStaysQuietAndResumes(t *testing.T) {
	var requests atomic.Int32
	r := newDriveRig(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			requests.Add(1)
			next.ServeHTTP(w, req)
		})
	})
	c := savedHomeChat(t, r, projectsOf(r.a), "saved while storage is off", 128)
	mustNil(t, r.b.Rename(t.Context(), "other computer"))
	_, err := r.a.Dir.List(t.Context())
	mustNil(t, err)
	mustNil(t, SaveStorage(r.a.Home, r.a.Relay, r.a.Identity.ID(), "off"))
	before := requests.Load()
	var notices []string
	for range 3 {
		mustNil(t, r.a.PublishLocalChats(t.Context(), time.Now, func(_ cell.Cell, line string) { notices = append(notices, line) }))
	}
	if got := requests.Load() - before; got != 0 {
		t.Fatalf("storage-off background publication made %d requests, want zero", got)
	}
	if len(notices) != 0 {
		t.Fatalf("storage-off background publication promised retries: %q", notices)
	}
	mustNil(t, SaveStorage(r.a.Home, r.a.Relay, r.a.Identity.ID(), "on"))
	mustNil(t, r.a.PublishLocalChats(t.Context(), time.Now, nil))
	if !listed(t, r.a, c.ID) {
		t.Fatal("approval did not publish the preserved chat")
	}
}
