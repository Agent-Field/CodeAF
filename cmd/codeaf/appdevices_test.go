package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// The same signed in-process directory as syncsetup's relay fixture, without
// a chat engine or a provider. Every request, including watch upgrades, counts.
func appDevicesRelay(t *testing.T, wrap ...func(http.Handler) http.Handler) (*httptest.Server, *atomic.Int64, *atomic.Bool) {
	t.Helper()
	var mu sync.Mutex
	dirs := map[string]directory.Directory{}
	open := func(id string) (directory.Directory, error) {
		mu.Lock()
		defer mu.Unlock()
		if dirs[id] == nil {
			dirs[id] = directory.NewMemory(time.Now)
		}
		return dirs[id], nil
	}
	hits, down := &atomic.Int64{}, &atomic.Bool{}
	door := directory.Handler(reqsign.AuthenticateAt(time.Now), open)
	for _, one := range wrap {
		door = one(door)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if down.Load() {
			http.Error(w, "offline", http.StatusServiceUnavailable)
			return
		}
		door.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(syncsetup.URLVar, srv.URL)
	t.Setenv(syncsetup.IntervalVar, "")
	return srv, hits, down
}

func appDeviceSync(t *testing.T, dir string) *syncsetup.Sync {
	t.Helper()
	s, ok, err := syncsetup.Open(dir)
	if err != nil || !ok {
		t.Fatalf("open sync: %v, %v", ok, err)
	}
	return s
}

func appDeviceBackend(t *testing.T, dir string) (*appBackend, *appRecorder, *actionWire) {
	t.Helper()
	t.Setenv(home.EnvVar, dir)
	rec := newAppRecorder()
	b := newAppBackend(rec, nil)
	// This fixture exercises device management only; suppress the unrelated
	// process warmer that normally follows hello.
	b.warmed = make(chan struct{})
	close(b.warmed)
	t.Cleanup(func() { _ = b.Close() })
	w := newActionWire(t, b)
	if !slices.Contains(w.hello, appwire.FeatureDevicesManage) {
		t.Fatal("hello is missing devices.manage")
	}
	return b, rec, w
}

func appDeviceList(t *testing.T, w *actionWire) appwire.Devices {
	t.Helper()
	return machineCall[appwire.DevicesResult](t, w, "devices.list", struct{}{}).Devices
}

func appDeviceEvent(t *testing.T, rec *appRecorder, match func(appwire.Devices) bool) int {
	t.Helper()
	return rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		return event == appwire.EventDevicesChanged && match(data.(appwire.DevicesResult).Devices)
	})
}

// V1, V5: real watch traffic updates presence without any roster/presence read.
func TestAppDevicesV1V5ExistingWatchSuppliesPresence(t *testing.T) {
	celltest.Available(t)
	_, hits, _ := appDevicesRelay(t)
	a, b := t.TempDir(), t.TempDir()
	id, err := identity.Ensure(a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(b, id, false); err != nil {
		t.Fatal(err)
	}
	sa, sb := appDeviceSync(t, a), appDeviceSync(t, b)
	for _, s := range []*syncsetup.Sync{sa, sb} {
		if err := s.Rename(context.Background(), "computer"); err != nil {
			t.Fatal(err)
		}
	}
	_, rec, w := appDeviceBackend(t, a)
	view := appDeviceList(t, w)
	if view.Devices[1].Online == nil || *view.Devices[1].Online {
		t.Fatalf("known offline presence: %+v", view)
	}
	before := hits.Load()
	// These sockets belong to other engine work, not to the Devices adapter.
	follow := sa.Follow()
	if follow == nil {
		t.Fatal("the real directory has no follower")
	}
	t.Cleanup(follow.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for !follow.State().Up {
		select {
		case <-follow.Changes():
		case <-ctx.Done():
			t.Fatal("the existing follower did not open")
		}
	}
	remote, err := sb.Dir.(directory.Watcher).Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(remote.Close)
	appDeviceEvent(t, rec, func(v appwire.Devices) bool {
		return len(v.Devices) == 2 && v.Devices[1].Online != nil && *v.Devices[1].Online
	})
	remote.Close()
	appDeviceEvent(t, rec, func(v appwire.Devices) bool {
		return len(v.Devices) == 2 && v.Devices[1].Online != nil && !*v.Devices[1].Online && !v.Devices[1].LastSeen.IsZero()
	})
	rec.mu.Lock()
	after := len(rec.events)
	rec.mu.Unlock()
	follow.Close()
	seen := 0
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		i := seen
		seen++
		if i < after || event != appwire.EventDevicesChanged {
			return false
		}
		v := data.(appwire.DevicesResult).Devices
		return len(v.Devices) == 2 && v.Devices[1].Online == nil
	})
	if hits.Load()-before != 2 {
		t.Fatalf("watch events caused %d requests beyond the two upgrades", hits.Load()-before)
	}
}

// V1: an older relay that cannot answer presence leaves it unknown.
func TestAppDevicesV1OldRelayPresenceIsUnknown(t *testing.T) {
	celltest.Available(t)
	_, _, _ = appDevicesRelay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/dir/presence" {
				http.NotFound(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	dir := t.TempDir()
	if _, err := identity.Ensure(dir); err != nil {
		t.Fatal(err)
	}
	s := appDeviceSync(t, dir)
	if err := s.Rename(context.Background(), "desk"); err != nil {
		t.Fatal(err)
	}
	_, _, w := appDeviceBackend(t, dir)
	view := appDeviceList(t, w)
	if view.Mode != appwire.DevicesOn || len(view.Devices) != 1 || view.Devices[0].Online != nil {
		t.Fatalf("old relay presence was invented: %+v", view)
	}
}

// V1-V3: two paired homes use the real backend and registered wire methods.
func TestAppDevicesV1V2V3PairedHomes(t *testing.T) {
	celltest.Available(t)
	srv, hits, _ := appDevicesRelay(t)
	a, b := t.TempDir(), t.TempDir()
	id, err := identity.Ensure(a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(b, id, false); err != nil {
		t.Fatal(err)
	}
	sa, sb := appDeviceSync(t, a), appDeviceSync(t, b)
	for dir, name := range map[string]string{a: "desk", b: "laptop"} {
		if _, err := devname.Set(dir, name); err != nil {
			t.Fatal(err)
		}
		if err := appDeviceSync(t, dir).Rename(context.Background(), name); err != nil {
			t.Fatal(err)
		}
	}
	backend, rec, wa := appDeviceBackend(t, a)
	_, _, wb := appDeviceBackend(t, b)
	view := appDeviceList(t, wa)
	if view.Mode != appwire.DevicesOn || len(view.Devices) != 2 || view.Devices[0].ID != sa.Device.ID() || !view.Devices[0].Self || view.Devices[0].Name != "desk" || view.Devices[1].Name != "laptop" || view.Devices[1].Self || view.Devices[1].Platform != runtime.GOOS || view.Devices[1].Removed {
		t.Fatalf("paired list: %+v", view)
	}
	for _, params := range []map[string]any{{"name": ""}, {"name": strings.Repeat("x", devname.Most+1)}, {"name": "a\nb"}, {"name": "other", "id": sb.Device.ID()}} {
		if _, err := wa.call("devices.rename", params); err == nil || err.Code != appwire.CodeBadParams {
			t.Fatalf("rename %v: %v", params, err)
		}
	}
	renamed := machineCall[appwire.DeviceRenameResult](t, wa, "devices.rename", appwire.DeviceRenameParams{Name: "  studio  "})
	if renamed.Device.ID != sa.Device.ID() || renamed.Device.Name != "studio" || renamed.Line != pair.RenamedLine("studio") {
		t.Fatalf("rename receipt: %+v", renamed)
	}
	appDeviceEvent(t, rec, func(v appwire.Devices) bool { return v.Devices[0].Name == "studio" })
	other := appDeviceList(t, wb)
	if len(other.Devices) != 2 || other.Devices[0].ID != sb.Device.ID() || other.Devices[1].Name != "studio" {
		t.Fatalf("the other home sees: %+v", other)
	}
	// The CLI's reader sees the same shared name, independently of app state.
	rows, err := chatsKindOf(sb.Dir, id.CellKey(), sb.Device.ID(), sb.Relay).rows()
	if err != nil || len(rows) != 2 || rows[1].Name != "studio" {
		t.Fatalf("CLI roster: %+v, %v", rows, err)
	}
	before := hits.Load()
	if _, err := wa.call("devices.remove", appwire.IDParams{ID: sa.Device.ID()}); err == nil || err.Code != appwire.CodeBadParams {
		t.Fatalf("remove self: %v", err)
	}
	if hits.Load() != before {
		t.Fatal("self removal reached the relay")
	}
	if _, err := wa.call("devices.remove", appwire.IDParams{ID: "unknown"}); err == nil || err.Code != appwire.CodeNotFound {
		t.Fatalf("remove unknown: %v", err)
	}
	removed := machineCall[appwire.DeviceRemoveResult](t, wa, "devices.remove", appwire.IDParams{ID: sb.Device.ID()})
	if removed.ID != sb.Device.ID() || removed.Line != pair.ChatsRevokedLine("laptop") {
		t.Fatalf("remove receipt: %+v", removed)
	}
	appDeviceEvent(t, rec, func(v appwire.Devices) bool { return len(v.Devices) == 2 && v.Devices[1].Removed })
	view = appDeviceList(t, wa)
	if len(view.Devices) != 2 || !view.Devices[1].Removed {
		t.Fatalf("removed device disappeared: %+v", view)
	}
	if _, err := wb.call("devices.list", struct{}{}); err == nil || err.Code != appwire.CodeEngine || err.Message != chatlist.Removed {
		t.Fatalf("the removed computer's refusal: %v", err)
	}
	before = hits.Load()
	unavailable := appDeviceList(t, wb)
	if unavailable.Mode != appwire.DevicesUnavailable || unavailable.Line != chatlist.Removed || len(unavailable.Devices) != 1 || !unavailable.Devices[0].Removed || hits.Load() != before {
		t.Fatalf("known removal was not answered locally: %+v", unavailable)
	}
	srv.Close()
	if _, err := wa.call("devices.list", struct{}{}); err == nil || err.Code != appwire.CodeUnreachable {
		t.Fatalf("refresh after removal: %v", err)
	}
	retained, err := backend.devices.snapshot()
	if err != nil || len(retained.Devices) != 2 || !retained.Devices[1].Removed {
		t.Fatalf("failed refresh undid removal: %+v, %v", retained, err)
	}
}

// V4-V5: local reads, local rename and self refusal leave quiet/off homes quiet.
func TestAppDevicesV4V5LocalModesMakeNoRelayRequests(t *testing.T) {
	for _, mode := range []string{appwire.DevicesQuiet, appwire.DevicesOff, appwire.DevicesUnavailable} {
		t.Run(mode, func(t *testing.T) {
			celltest.Available(t)
			srv, hits, _ := appDevicesRelay(t)
			dir := t.TempDir()
			if mode == appwire.DevicesOff {
				// An off home that had paired must also stay local.
				if _, err := identity.Ensure(dir); err != nil {
					t.Fatal(err)
				}
				if err := syncsetup.SaveRelayURL(dir, srv.URL); err != nil {
					t.Fatal(err)
				}
				t.Setenv(syncsetup.URLVar, "off")
			}
			if mode == appwire.DevicesUnavailable {
				if _, err := identity.Ensure(dir); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(cell.UseAvailabilityInTests(false))
			}
			backend, rec, w := appDeviceBackend(t, dir)
			view := appDeviceList(t, w)
			if view.Mode != mode || len(view.Devices) != 1 || !view.Devices[0].Self || view.Devices[0].Online != nil || !view.Devices[0].LastSeen.IsZero() || view.Line == "" {
				t.Fatalf("local list: %+v", view)
			}
			if mode == appwire.DevicesQuiet && (view.Line != chatlist.DevicesQuiet || !strings.Contains(view.Line, "codeaf pair") || !identity.Solo(dir)) {
				t.Fatalf("fresh home is not quiet with terminal instructions: %+v", view)
			}
			if mode == appwire.DevicesOff && view.Line != chatlist.SyncOff {
				t.Fatalf("off copy: %q", view.Line)
			}
			if mode != appwire.DevicesUnavailable {
				renamed := machineCall[appwire.DeviceRenameResult](t, w, "devices.rename", appwire.DeviceRenameParams{Name: "kitchen"})
				if renamed.Line != pair.RenamedAloneLine("kitchen") || renamed.Device.ID != view.Devices[0].ID {
					t.Fatalf("local rename: %+v", renamed)
				}
				appDeviceEvent(t, rec, func(v appwire.Devices) bool { return v.Devices[0].Name == "kitchen" })
			}
			if _, err := w.call("devices.remove", appwire.IDParams{ID: view.Devices[0].ID}); err == nil || err.Code != appwire.CodeBadParams {
				t.Fatalf("local self refusal: %v", err)
			}
			_ = appDeviceList(t, w)
			if err := backend.Close(); err != nil {
				t.Fatal(err)
			}
			if hits.Load() != 0 {
				t.Fatalf("local home sent %d relay requests", hits.Load())
			}
		})
	}
}

// V5: while Devices was never opened, engine-owned writes emit without a read.
func TestAppDevicesV5PassiveEventsDoNotReadRelay(t *testing.T) {
	celltest.Available(t)
	_, hits, _ := appDevicesRelay(t)
	dir := t.TempDir()
	if _, err := identity.Ensure(dir); err != nil {
		t.Fatal(err)
	}
	s := appDeviceSync(t, dir)
	before := hits.Load()
	_, rec, _ := appDeviceBackend(t, dir)
	if err := s.Rename(context.Background(), "elsewhere in the engine"); err != nil {
		t.Fatal(err)
	}
	// Sync.Rename publishes a directory record. A CLI rename also saves the
	// chosen local name, as it does here before a second engine-owned write.
	if _, err := devname.Set(dir, "window name"); err != nil {
		t.Fatal(err)
	}
	if err := s.Rename(context.Background(), "window name"); err != nil {
		t.Fatal(err)
	}
	appDeviceEvent(t, rec, func(v appwire.Devices) bool { return v.Devices[0].Name == "window name" })
	if hits.Load()-before != 2 {
		t.Fatalf("passive events sent %d requests beyond the two writes", hits.Load()-before)
	}
}

// V2 and the wire error contract: an unreachable relay keeps the local name.
func TestAppDevicesV2OfflineRenameAndUnreachableList(t *testing.T) {
	celltest.Available(t)
	srv, _, _ := appDevicesRelay(t)
	dir := t.TempDir()
	if _, err := identity.Ensure(dir); err != nil {
		t.Fatal(err)
	}
	_, rec, w := appDeviceBackend(t, dir)
	srv.Close()
	if _, err := w.call("devices.list", struct{}{}); err == nil || err.Code != appwire.CodeUnreachable || err.Message != chatlist.Unreachable {
		t.Fatalf("unreachable list: %v", err)
	}
	raw, err := w.call("devices.rename", appwire.DeviceRenameParams{Name: "offline desk"})
	if err != nil {
		t.Fatal(err)
	}
	var got appwire.DeviceRenameResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Device.Name != "offline desk" || got.Line != pair.RenamedHereLine("offline desk") || devname.Name(dir) != "offline desk" {
		t.Fatalf("offline rename: %+v", got)
	}
	appDeviceEvent(t, rec, func(v appwire.Devices) bool { return v.Devices[0].Name == "offline desk" })
}
