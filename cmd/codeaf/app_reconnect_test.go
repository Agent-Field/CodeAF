package main

import (
	"bufio"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/identity"
)

type appReconnectWriter struct {
	http.ResponseWriter
	connections chan net.Conn
}

func (w appReconnectWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c, rw, err := w.ResponseWriter.(http.Hijacker).Hijack()
	if err == nil {
		w.connections <- c
	}
	return c, rw, err
}

func TestAppPresenceP1ReconnectKeepsFreshPeerAndMonotonicLastSeen(t *testing.T) {
	appPresenceReconnect(t, "fresh")
}

func TestAppPresenceP1ReconnectReconcilesMissedOffline(t *testing.T) {
	appPresenceReconnect(t, "missed offline")
}

func TestAppPresenceP1OutageExpiresToUnknown(t *testing.T) {
	appPresenceReconnect(t, "outage")
}

func appPresenceReconnect(t *testing.T, scenario string) {
	missedOffline := scenario == "missed offline"
	celltest.Available(t)
	connections := make(chan net.Conn, 8)
	var stale, blocked atomic.Bool
	appDevicesRelay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/dir/watch" {
				if blocked.Load() {
					http.Error(w, "reconnect paused", http.StatusServiceUnavailable)
					return
				}
				next.ServeHTTP(appReconnectWriter{w, connections}, r)
				return
			}
			if r.URL.Path == "/v1/dir/list" && stale.Load() {
				rec := httptest.NewRecorder()
				next.ServeHTTP(rec, r)
				var listing directory.Listing
				if err := json.Unmarshal(rec.Body.Bytes(), &listing); err != nil {
					t.Error(err)
					return
				}
				for id, d := range listing.Devices {
					d.LastSeen = 1
					listing.Devices[id] = d
				}
				for k, v := range rec.Header() {
					w.Header()[k] = v
				}
				w.WriteHeader(rec.Code)
				_ = json.NewEncoder(w).Encode(listing)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	a, b := t.TempDir(), t.TempDir()
	id, err := identity.Ensure(a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = identity.Adopt(b, id, false); err != nil {
		t.Fatal(err)
	}
	sa, sb := appDeviceSync(t, a), appDeviceSync(t, b)
	if err = sa.Rename(t.Context(), "A"); err != nil {
		t.Fatal(err)
	}
	if err = sb.Rename(t.Context(), "B"); err != nil {
		t.Fatal(err)
	}
	peer := sb.Follow()
	defer peer.Close()
	select {
	case <-connections:
	case <-time.After(10 * time.Second):
		t.Fatal("P1 B did not connect")
	}
	appSyncWait(t, "P1 B's live watch", func() bool { return peer.State().Up })
	backend, rec, wire := appDeviceBackend(t, a)
	machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{})
	var own net.Conn
	select {
	case own = <-connections:
	case <-time.After(10 * time.Second):
		t.Fatal("P1 A did not connect")
	}
	appDeviceList(t, wire)
	appDeviceEvent(t, rec, func(v appwire.Devices) bool {
		for _, d := range v.Devices {
			if d.ID == sb.Device.ID() {
				return d.Online != nil && *d.Online
			}
		}
		return false
	})
	rec.mu.Lock()
	before := len(rec.events)
	rec.mu.Unlock()
	if scenario != "fresh" {
		blocked.Store(true)
	}
	if err = own.Close(); err != nil {
		t.Fatal(err)
	}
	if scenario == "outage" {
		seen := 0
		rec.waitFor(t, dirwatch.DeadAfter+5*time.Second, func(event string, data any) bool {
			i := seen
			seen++
			if i < before {
				return false
			}
			if event != appwire.EventDevicesChanged {
				return false
			}
			for _, d := range data.(appwire.DevicesResult).Devices.Devices {
				if d.ID == sb.Device.ID() {
					return d.Online == nil
				}
			}
			return false
		})
		return
	}
	if missedOffline {
		peer.Close()
		appSyncWait(t, "P1 B's presence actually lapses", func() bool {
			p, err := sb.Dir.Presence(t.Context())
			return err == nil && !p.Devices[sb.Device.ID()].Online
		})
		blocked.Store(false)
	} else {
		peer.Probe()
	}
	select {
	case <-connections:
	case <-time.After(30 * time.Second):
		t.Fatal("P1 A did not reconnect")
	}
	if missedOffline {
		// No devices.list call repairs A: the watch reconnect owns reconciliation.
		appDeviceEvent(t, rec, func(v appwire.Devices) bool {
			for _, d := range v.Devices {
				if d.ID == sb.Device.ID() {
					return d.Online != nil && !*d.Online
				}
			}
			return false
		})
		return
	}
	// A fresh read during the reconnect must agree with B's uninterrupted pings.
	fresh := appDeviceList(t, wire)
	stale.Store(true)
	older := appDeviceList(t, wire)
	for i, d := range older.Devices {
		if d.LastSeen.Before(fresh.Devices[i].LastSeen) {
			t.Fatalf("P1 last_seen moved backwards: %v -> %v", fresh.Devices[i].LastSeen, d.LastSeen)
		}
		if d.ID == sb.Device.ID() && (d.Online == nil || !*d.Online) {
			t.Fatal("P1 reconnect lost fresh peer presence")
		}
	}
	// Serialize the final observer publication before inspecting the wire.
	backend.devices.op.Lock()
	backend.devices.publish(backend.emit)
	backend.devices.op.Unlock()
	rec.mu.Lock()
	defer rec.mu.Unlock()
	last := map[string]time.Time{}
	for i, event := range rec.events {
		if event != appwire.EventDevicesChanged {
			continue
		}
		for _, d := range rec.data[i].(appwire.DevicesResult).Devices.Devices {
			if d.LastSeen.Before(last[d.ID]) {
				t.Fatal("P1 emitted last_seen backwards")
			}
			last[d.ID] = d.LastSeen
			if i >= before && d.ID == sb.Device.ID() && d.Online != nil && !*d.Online {
				t.Fatal("P1 reconnect emitted fresh B offline")
			}
		}
	}
}
