package pair

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/relay"
)

// L4-R7: Repeated struggling registrations share one outage and one recovery notice.
func TestL4FixServeStrugglingOnce(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	var attempts atomic.Int32
	door := &relay.Server{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) <= 2 {
			http.Error(w, "struggling", http.StatusServiceUnavailable)
			return
		}
		door.ServeHTTP(w, r)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	log := &screenLog{}
	host := &Host{Service: server.URL, Device: aDevice(t), Desk: &Desk{}, Devices: BookAt(filepath.Join(t.TempDir(), "devices.json")), Say: func(line string) {
		log.say(line)
		if strings.Contains(line, "reachable as") {
			cancel()
		}
	}}
	host.sleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	if err := host.Run(ctx); err != nil {
		t.Fatalf("serve stopped on recoverable service failure: %v", err)
	}
	lost, back := 0, 0
	for _, line := range log.all() {
		if strings.Contains(line, "cannot agree on changes") {
			lost++
		}
		if strings.Contains(line, "back") {
			back++
		}
	}
	if attempts.Load() != 3 || lost != 1 || back != 1 {
		t.Fatalf("attempts=%d lost=%d back=%d lines=%q", attempts.Load(), lost, back, log.all())
	}
}
