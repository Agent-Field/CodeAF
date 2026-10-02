package directorytest

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
)

// The numbers the presence cases run on. A device under test declares a beat of
// two seconds, so its window is five and a case that waits lapseWait (the window
// and a second of slack) sees it lapsed. The viewer is a device that must stay
// live for the whole case, so it declares the longest beat there is.
const (
	deviceBeatQuery = "beat=2"
	viewerQuery     = "beat=60&events=1"
	lapseWait       = 6 * time.Second
)

// presenceCases are the named rows of section 21.12 of the contract that a relay
// proves; the client's rows are proved where the client lives.
var presenceCases = map[string]func(watchEnv){
	"FrozenDeviceGoesOffline":   frozenDeviceGoesOffline,
	"LapsedSocketClosesSilent":  lapsedSocketClosesSilent,
	"PingingDeviceStaysOnline":  pingingDeviceStaysOnline,
	"ReturnAnnouncesOnline":     returnAnnouncesOnline,
	"PresenceReadAppliesWindow": presenceReadAppliesWindow,
	"OneLiveSocketKeepsOnline":  oneLiveSocketKeepsOnline,
	"LegacyWindowIs75s":         legacyWindowIs75s,
	"BadBeatIsRefused":          badBeatIsRefused,
	"LapsedDeviceAnnouncedOnce": lapsedDeviceAnnouncedOnce,
	"LapseStampsLastSeenOnly":   lapseStampsLastSeenOnly,
	"UpgradeAnnouncesPresence":  upgradeAnnouncesPresence,
}

// RunPresence runs every presence case of the contract (section 21.12) against
// a fresh rig each. They wait on the rig's clock, so a fake clock makes them
// instant and a live relay takes the five-second window of the beat they declare.
func RunPresence(t *testing.T, factory WatchRigFactory) {
	for name, fn := range presenceCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fn(newWatchEnv(t, factory(t)))
		})
	}
}

// dialQuery dials the watch route as device with a raw query, unopened.
func (w watchEnv) dialQuery(device, query string) *sock {
	w.t.Helper()
	dialCtx, cancel := context.WithTimeout(context.Background(), dialWait)
	defer cancel()
	conn, resp, err := dialWatchQuery(dialCtx, w.rig.Base, w.rig.Sign(device), query)
	if err != nil {
		w.t.Fatalf("watch dial %q: %v (%s)", query, err, describe(resp))
	}
	s := startSock(w.t, conn)
	s.resp = resp
	return s
}

// viewer opens the events socket of a device that stays live, as the home screen does.
func (w watchEnv) viewer() *sock { return w.dialQuery(devB, viewerQuery).opened() }

// device opens a socket of device A that has promised to ping every two seconds.
func (w watchEnv) device(query string) *sock { return w.dialQuery(devA, query).opened() }

// watched is a viewer and a device A that will go silent, with the viewer
// having heard A arrive.
func (w watchEnv) watched() (viewer, frozen *sock) {
	w.t.Helper()
	w.enrol()
	viewer = w.viewer()
	frozen = w.device(deviceBeatQuery)
	viewer.wantPresence(w.env.id(devA), true)
	return viewer, frozen
}

// enrol records device A, which the presence route lists only once the
// directory knows it, and device B, which reads it.
func (w watchEnv) enrol() {
	w.t.Helper()
	for _, d := range []string{devA, devB} {
		must(w.t, w.dirAs(d).PutDevice(ctx, w.env.id(d), directory.Device{V: 1, Name: d[:5]}))
	}
}

// wantPresence asserts the next delivery is the presence event of device.
func (s *sock) wantPresence(device string, online bool) {
	s.t.Helper()
	text := s.text()
	var e dirwatch.Event
	if err := json.Unmarshal([]byte(text), &e); err != nil || e.T != dirwatch.EventPresence ||
		e.Device != device || e.Online == nil || *e.Online != online {
		s.t.Fatalf("frame %q, want presence online=%v of %s", text, online, device)
	}
}

// wantOnline asserts what the presence route says about device A, read by device B.
func (w watchEnv) wantOnline(online bool) {
	w.t.Helper()
	v, err := w.dirAs(devB).Presence(ctx)
	must(w.t, err)
	if got := v.Devices[w.env.id(devA)].Online; got != online {
		w.t.Fatalf("presence lists device A online=%v, want %v", got, online)
	}
}

// A device that stops pinging is announced offline once its window passes.
func frozenDeviceGoesOffline(w watchEnv) {
	viewer, _ := w.watched()
	w.wantOnline(true)
	w.env.clock.Wait(lapseWait)
	viewer.wantPresence(w.env.id(devA), false)
	w.wantOnline(false)
}

// The socket of the lapsed device is told to redial.
func lapsedSocketClosesSilent(w watchEnv) {
	_, frozen := w.watched()
	w.env.clock.Wait(lapseWait)
	frozen.wantClosed(directory.CloseSilent, "silent")
}

// A device that pings every beat stays online for far longer than the window.
func pingingDeviceStaysOnline(w watchEnv) {
	viewer, device := w.watched()
	for range 3 {
		w.env.clock.Wait(2 * time.Second)
		device.ping()
	}
	viewer.wantSilent()
	w.wantOnline(true)
}

// A device that comes back after it was announced offline is announced online at once.
func returnAnnouncesOnline(w watchEnv) {
	viewer, _ := w.watched()
	w.env.clock.Wait(lapseWait)
	viewer.wantPresence(w.env.id(devA), false)
	w.device(deviceBeatQuery)
	viewer.wantPresence(w.env.id(devA), true)
	w.wantOnline(true)
}

// The presence route applies the window with nobody watching and no alarm set.
func presenceReadAppliesWindow(w watchEnv) {
	w.enrol()
	w.device(deviceBeatQuery)
	w.wantOnline(true)
	w.env.clock.Wait(lapseWait)
	w.wantOnline(false)
}

// One live socket keeps its device online, and a stale sibling announces nothing.
func oneLiveSocketKeepsOnline(w watchEnv) {
	viewer, _ := w.watched()
	live := w.device(deviceBeatQuery)
	for range 2 {
		w.env.clock.Wait(2 * time.Second)
		live.ping()
	}
	w.env.clock.Wait(2 * time.Second)
	viewer.wantSilent()
	w.wantOnline(true)
}

// A socket that declares no beat is a client from before presence: it has 75
// seconds. That is 75 seconds of the rig's clock, so only a fake clock runs it.
func legacyWindowIs75s(w watchEnv) {
	if _, fake := w.rig.Clock.(*FakeClock); !fake {
		w.t.Skip("the legacy window is 75 s of the relay's own clock; the in-process relay proves it on a fake clock")
	}
	w.enrol()
	viewer := w.viewer()
	w.dialQuery(devA, "").opened()
	viewer.wantPresence(w.env.id(devA), true)
	w.env.clock.Wait(60 * time.Second)
	w.wantOnline(true)
	viewer.wantSilent()
	w.env.clock.Wait(16 * time.Second)
	viewer.wantPresence(w.env.id(devA), false)
	w.wantOnline(false)
}

// A beat outside 1..60, one that is not a plain decimal and two different beats
// are refused before the upgrade; the edges and one beat twice are accepted.
func badBeatIsRefused(w watchEnv) {
	for _, q := range []string{"beat=0", "beat=61", "beat=x", "beat=", "beat=-1", "beat=%2B5", "beat=1.5", "beat=5&beat=6"} {
		w.wantHoldQuery(q, http.StatusBadRequest)
	}
	for _, q := range []string{"beat=1", "beat=60", "beat=5&beat=5"} {
		w.wantHoldQuery(q, http.StatusSwitchingProtocols)
	}
}

// A lapsed device is announced once: its stale socket closing starts no gap. A
// fake clock waits out the 15 s gap; a live relay is only given the quiet window.
func lapsedDeviceAnnouncedOnce(w watchEnv) {
	viewer, frozen := w.watched()
	w.env.clock.Wait(lapseWait)
	viewer.wantPresence(w.env.id(devA), false)
	frozen.wantClosed(directory.CloseSilent, "silent")
	if _, fake := w.rig.Clock.(*FakeClock); fake {
		w.env.clock.Wait(directory.OfflineDebounce + time.Second)
	}
	viewer.wantSilent()
}

// The lapse stamps last_seen with the device's last sign of life and moves no version.
func lapseStampsLastSeenOnly(w watchEnv) {
	id := w.env.id(devA)
	w.enrol()
	viewer := w.viewer()
	device := w.device(deviceBeatQuery)
	viewer.wantPresence(id, true)
	w.env.clock.Wait(2 * time.Second)
	before := w.env.clock.Now()
	device.ping()
	after := w.env.clock.Now()
	version := w.listVersion(devB)
	w.env.clock.Wait(lapseWait)
	viewer.wantPresence(id, false)
	l, err := w.dirAs(devB).List(ctx)
	must(w.t, err)
	if got := l.Devices[id].LastSeen; got < before.UnixMilli() || got > after.UnixMilli() {
		w.t.Fatalf("last_seen %d, want the ping's time in [%d, %d]", got, before.UnixMilli(), after.UnixMilli())
	}
	if got := w.listVersion(devB); got != version {
		w.t.Fatalf("list version %d after the lapse, want %d", got, version)
	}
}

// The 101 carries Codeaf-Presence whether or not the socket declared a beat.
func upgradeAnnouncesPresence(w watchEnv) {
	for _, q := range []string{"", deviceBeatQuery} {
		s := w.dialQuery(devA, q)
		if got := s.resp.Header.Get(directory.PresenceHeader); got != "1" {
			w.t.Fatalf("101 answer has %s=%q for query %q, want 1", directory.PresenceHeader, got, q)
		}
	}
}
