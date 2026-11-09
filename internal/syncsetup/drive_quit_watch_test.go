package syncsetup

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/guard"
)

func TestAClosingChatDoesNotReadItsHoldWhenWatchNewsArrives(t *testing.T) {
	var releasing atomic.Bool
	var lateReads atomic.Int32
	releaseEntered, finishRelease := make(chan struct{}), make(chan struct{})
	r := newDriveRig(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Method == http.MethodGet && strings.Contains(req.URL.Path, "/cells/") && releasing.Load() {
				lateReads.Add(1)
			}
			if req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/release") {
				releasing.Store(true)
				close(releaseEntered)
				select {
				case <-finishRelease:
				case <-req.Context().Done():
					return
				}
			}
			next.ServeHTTP(w, req)
		})
	})
	_, err := r.a.Dir.Create(t.Context(), r.cell.ID, directory.CellInit{})
	mustNil(t, err)
	drive, err := r.a.Drive(t.Context(), r.engine, r.cell, DriveOptions{DeviceName: nameA})
	mustNil(t, err)
	t.Cleanup(func() { _ = drive.Close(t.Context()) })
	feed := r.a.Follow()
	defer feed.Close()
	before := c31AwaitFeed(t, feed, requestTimeout, func(st dirwatch.State) bool { return st.Up && st.Version > 0 })
	closed := make(chan error, 1)
	guard.Go("closing watch fixture", func() { closed <- drive.Close(t.Context()) })
	defer close(finishRelease)
	select {
	case <-releaseEntered:
	case <-time.After(requestTimeout):
		t.Fatal("close never started releasing its hold")
	}
	_, err = r.b.Dir.Create(t.Context(), "closing-watch-news", directory.CellInit{})
	mustNil(t, err)
	c31AwaitFeed(t, feed, requestTimeout, func(st dirwatch.State) bool { return st.Version > before.Version })
	// Consume the announced state before cancellation can mask an ownership read.
	drive.followOwnership(t.Context())
	finishRelease <- struct{}{}
	select {
	case err := <-closed:
		mustNil(t, err)
	case <-time.After(requestTimeout):
		t.Fatal("close did not finish after its release")
	}
	// A later direct check must retain the same post-close refusal to read.
	mustNil(t, drive.CheckLease(t.Context()))
	if lateReads.Load() != 0 {
		t.Fatalf("the closing chat read its hold %d times", lateReads.Load())
	}
}
