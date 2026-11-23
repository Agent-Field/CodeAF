//go:build linux || darwin

package taskdoor_test

import (
	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type storageStatsTransport func(*http.Request) (*http.Response, error)

func (f storageStatsTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// This acceptance test uses the existing independent-copy and child fixtures,
// including their disposable Git history, isolated from the caller's checkout.
func TestStorageOptInServingTaskResultAfterApproval(t *testing.T) {
	celltest.Available(t)
	c := newChildRig(t)
	first, fresh := t.TempDir(), t.TempDir()
	id, dev := c.copies.Identity, c.copies.Device
	if err := identity.Install(first, id, dev); err != nil {
		t.Fatal(err)
	}
	if err := identity.Install(fresh, id, dev); err != nil {
		t.Fatal(err)
	}
	t.Setenv(syncsetup.URLVar, syncsetup.ProductionRelayURL)
	if err := syncsetup.SaveStorage(first, syncsetup.ProductionRelayURL, id.ID(), "on"); err != nil {
		t.Fatal(err)
	}
	if syncsetup.StorageForHome(fresh) != "off" {
		t.Fatal("fresh home reused first home permission")
	}
	var stats, writes atomic.Int32
	original := http.DefaultTransport
	underlying := blobstore.NewHTTP(c.copies.Service, func(*http.Request, []byte) {}, &http.Client{Transport: storageStatsTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/v1/store/frames") {
			writes.Add(1)
		}
		return original.RoundTrip(r)
	})})
	// The production HTTP gate used by serving copies shares the fresh home's
	// reading; the frame wire still reaches the existing real loopback store.
	c.copies.Store = syncsetup.GateStore(underlying, fresh, syncsetup.ProductionRelayURL, id.ID())
	http.DefaultTransport = storageStatsTransport(func(r *http.Request) (*http.Response, error) {
		if syncsetup.IsHostedRelay(r.URL.Scheme + "://" + r.URL.Host) {
			if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/v1/store/stats") {
				t.Fatalf("unexpected hosted request %s", r.URL)
			}
			if _, _, err := reqsign.AuthenticateAt(time.Now)(r, nil); err != nil {
				t.Errorf("stats signature: %v", err)
			}
			stats.Add(1)
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"storage":"on"}`))}, nil
		}
		return original.RoundTrip(r)
	})
	defer func() { http.DefaultTransport = original }()
	// This is the same process-wide lifetime serve and --take-tasks start.
	stopRefresh := syncsetup.StartStorageRefresh(t.Context(), fresh)
	defer stopRefresh()
	deadline := time.After(5 * time.Second)
	for syncsetup.StorageForHome(fresh) != "on" {
		select {
		case <-deadline:
			t.Fatal("serving process did not discover approval")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	stop := c.r.serve(c.config(t, false))
	defer stop()
	conn := c.start(t)
	defer conn.Close()
	if err := os.WriteFile(filepath.Join(c.dir, "finish"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	_, _, done := childFinish(t, c.r.dial(), c.id)
	if done.Outcome != "done" || done.Result == nil || writes.Load() == 0 || stats.Load() != 1 {
		t.Fatalf("result=%+v stats=%d writes=%d", done, stats.Load(), writes.Load())
	}
}
