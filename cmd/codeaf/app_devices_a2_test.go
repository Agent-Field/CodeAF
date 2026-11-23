package main

import (
	"net/http"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/identity"
)

func TestA2RelayChangeAndMissingSelfRetainReachableRoster(t *testing.T) {
	celltest.Available(t)
	appDevicesRelay(t)
	home := t.TempDir()
	if _, err := identity.Ensure(home); err != nil {
		t.Fatal(err)
	}
	old := appDeviceSync(t, home)
	if err := old.Rename(t.Context(), "old relay name"); err != nil {
		t.Fatal(err)
	}
	// The new relay has no self record. Its roster is readable even while
	// the independent presence door is temporarily unavailable.
	appDevicesRelay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/dir/presence" || r.Method == http.MethodPut {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"err":"unreachable"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	b, _, wire := appDeviceBackend(t, home)
	view := appDeviceList(t, wire)
	if len(view.Devices) != 1 || !view.Devices[0].Self || view.Devices[0].ID != old.Device.ID() || view.Devices[0].Online != nil {
		t.Fatalf("A2: readable directory without self: %+v", view)
	}
	_ = b.Close()
}
