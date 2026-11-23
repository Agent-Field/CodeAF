package main

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// B8b: a hosted relay stamps the asking computer's last seen on every request.
// That stamp, and an online computer's, must not count as a roster change, or
// each roster read publishes devices.changed and the app reads again.
func TestAppDevicesB8LastSeenStampIsNotAChange(t *testing.T) {
	on, off := true, false
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	roster := func(later time.Duration) appwire.Devices {
		return appwire.Devices{Mode: appwire.DevicesOn, Devices: []appwire.Device{
			{ID: "self", Name: "Desk", Self: true, Online: &on, LastSeen: at.Add(later)},
			{ID: "online", Name: "Home", Online: &on, LastSeen: at.Add(later)},
			{ID: "offline", Name: "Office", Online: &off, LastSeen: at},
		}}
	}
	first, read := devicesCompared(roster(0)), devicesCompared(roster(5*time.Second))
	if !devicesSame(t, first, read) {
		t.Fatal("B8b: a read that only moved this computer's and an online computer's last seen counts as a change")
	}
	if roster(5 * time.Second).Devices[0].LastSeen.IsZero() {
		t.Fatal("devicesCompared changed the published view")
	}
	moved := roster(0)
	moved.Devices[2].LastSeen = at.Add(time.Minute)
	if devicesSame(t, first, devicesCompared(moved)) {
		t.Fatal("an offline computer's new last seen is shown, so it must still count as a change")
	}
	flipped := roster(0)
	flipped.Devices[1].Online = &off
	if devicesSame(t, first, devicesCompared(flipped)) {
		t.Fatal("a computer going offline must still count as a change")
	}
}

func devicesSame(t *testing.T, a, b appwire.Devices) bool {
	t.Helper()
	return string(mustJSON(t, a)) == string(mustJSON(t, b))
}
