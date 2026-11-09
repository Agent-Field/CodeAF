package relayserve_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

func TestAnUnfinishedAuthenticationIsRefusedAndAnOutageCanStillBeRetried(t *testing.T) {
	r := newRig(t)
	id, device := newIdentity(t)
	if _, err := r.dir(device).Create(bg, cell, directory.CellInit{Head: head1}); err != nil {
		t.Fatal(err)
	}
	check := func(ctx context.Context, signed bool, want int) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/v1/dir/cells/"+cell, nil).WithContext(ctx)
		if signed {
			reqsign.Sign(req, nil, device, r.clock.Now())
		}
		answer := httptest.NewRecorder()
		r.svc.Handler.ServeHTTP(answer, req)
		if answer.Code != want {
			t.Errorf("signed=%v, context=%v: got %d %s, want %d", signed, ctx.Err(), answer.Code, answer.Body.String(), want)
		}
	}
	check(bg, false, http.StatusUnauthorized)
	cancelled, cancel := context.WithCancel(bg)
	cancel()
	check(cancelled, true, http.StatusUnauthorized)
	r.stop()
	path := filepath.Join(r.store, "raft", id.ID())
	for _, member := range []string{"2", "3"} {
		if err := os.Rename(filepath.Join(path, member+".db"), filepath.Join(path, member+".asleep")); err != nil {
			t.Fatal(err)
		}
	}
	r.start()
	check(bg, false, http.StatusUnauthorized)
	check(bg, true, http.StatusServiceUnavailable)
}
