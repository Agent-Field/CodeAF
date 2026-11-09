package syncsetup

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestAClosingChatDoesNotReadItsHoldWhenWatchNewsArrives(t *testing.T) {
	var releasing atomic.Bool
	var lateReads atomic.Int32
	r := newDriveRig(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Method == http.MethodGet && strings.Contains(req.URL.Path, "/cells/") && releasing.Load() {
				lateReads.Add(1)
			}
			if req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/release") {
				releasing.Store(true)
			}
			next.ServeHTTP(w, req)
		})
	})
	_, err := r.a.Dir.Create(t.Context(), r.cell.ID, directory.CellInit{})
	mustNil(t, err)
	drive, err := r.a.Drive(t.Context(), r.engine, r.cell, DriveOptions{DeviceName: nameA})
	mustNil(t, err)
	t.Cleanup(func() { _ = drive.Close(t.Context()) })
	mustNil(t, drive.Close(t.Context()))
	// Watch news queued during the release no longer needs an ownership reading.
	mustNil(t, drive.CheckLease(t.Context()))
	if lateReads.Load() != 0 {
		t.Fatalf("the closing chat read its hold %d times", lateReads.Load())
	}
}
