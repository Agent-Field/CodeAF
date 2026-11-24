package chatlist

import (
	"fmt"
	"sort"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// The words of the devices row. The verb it offers, MoveHere, is in copy.go with
// the other contract sentences.
const (
	thisMac    = "This Mac"
	thisDevice = "This computer"
	offlineTag = "(offline)"
	verbFrom   = "%s from %s"
	darwin     = "darwin"
)

// Device is one paired device as the devices row shows it.
type Device struct {
	ID, Name string
	// Self marks the device the row is drawn on.
	Self bool
	// LastSeen is when the directory last heard from this device, or zero when unknown.
	LastSeen time.Time
}

// Devices lists the devices that are not revoked, this one first and the rest by
// name, so the row never reshuffles when a device comes and goes.
func Devices(l directory.Listing, self string, open Opener) []Device {
	var out []Device
	for id, d := range l.Devices {
		if !d.Revoked {
			var seen time.Time
			if d.LastSeen > 0 {
				seen = time.UnixMilli(d.LastSeen)
			}
			out = append(out, Device{ID: id, Name: ownName(id == self, d, open, id, l), Self: id == self, LastSeen: seen})
		}
	}
	sort.Slice(out, func(i, j int) bool { return deviceBefore(out[i], out[j]) })
	return out
}

func ownName(self bool, d directory.Device, open Opener, id string, l directory.Listing) string {
	if !self {
		return deviceName(l.Devices, id, open)
	}
	if d.Platform == darwin {
		return thisMac
	}
	return thisDevice
}

func deviceBefore(a, b Device) bool {
	if a.Self != b.Self {
		return a.Self
	}
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.ID < b.ID
}

// Mark is the dot and name of a device, filled when it is online. A device that
// is not online says so in words as well, since a hollow dot alone is easy to
// miss on a plain terminal.
func (d Device) Mark(online bool, mark string) string {
	if online || d.Self {
		return mark + " " + d.Name
	}
	return mark + " " + d.Name + " " + offlineTag
}

// VerbFrom is the verb with the device it brings the chat from. The verb is
// [MoveHere] for every chat on another device, whether that device is running it
// or let it go.
func VerbFrom(r Row) string { return fmt.Sprintf(verbFrom, MoveHere, r.Device) }
