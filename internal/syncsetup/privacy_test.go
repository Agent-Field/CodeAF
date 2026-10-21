package syncsetup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
)

// TestC10OffBlocksEvenPreviouslyOpenedClients proves C10 after pairing: all
// wire doors remain quiet, including clients constructed before the switch.
func TestC10OffBlocksEvenPreviouslyOpenedClients(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer srv.Close()
	home := machine(t, srv.URL)
	if err := identity.EndSolo(home); err != nil {
		t.Fatal(err)
	}
	s := openSync(t, home)
	t.Setenv(URLVar, "off")
	if s, ok, err := OpenFirst(home); s != nil || ok || err != nil {
		t.Fatalf("off opened a sync client: %v, %v, %v", s, ok, err)
	}
	ctx := context.Background()
	_, _ = s.Dir.List(ctx)
	_, _ = s.Store.Has(ctx, []string{strings.Repeat("a", 64)})
	_, _ = s.Dir.(directory.Watcher).Watch(ctx)
	if hits.Load() != 0 {
		t.Fatalf("off sent %d requests", hits.Load())
	}
}

// TestC10ExplicitRelayReceivesTrafficInsteadOfHosted proves C10 with two local
// recorders; the hosted URL is replaced before any client is constructed.
func TestC10ExplicitRelayReceivesTrafficInsteadOfHosted(t *testing.T) {
	var defaultHits, targetHits atomic.Int64
	defaultRelay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defaultHits.Add(1)
		http.NotFound(w, r)
	}))
	defer defaultRelay.Close()
	hosted(t, defaultRelay.URL)
	target := relay(t)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits.Add(1)
		target.Config.Handler.ServeHTTP(w, r)
	}))
	defer proxy.Close()
	home := machine(t, proxy.URL)
	s := openSync(t, home)
	if err := s.putDevice(context.Background(), "paired computer"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Dir.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.Has(context.Background(), []string{strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if targetHits.Load() == 0 || defaultHits.Load() != 0 || s.Relay != proxy.URL {
		t.Fatalf("target requests %d, hosted requests %d, chosen relay %q", targetHits.Load(), defaultHits.Load(), s.Relay)
	}
}
