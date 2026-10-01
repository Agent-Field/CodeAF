// Package chatlist turns the directory's listing into the rows a person sees
// and the sentences that go with them. It is pure: no clock, no network, no
// terminal. The home screen and `codeaf cell list --all` both draw from it, so
// the copy in copy.go is the one source of truth for both.
package chatlist

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// Status is where a chat is running, from one device's point of view.
type Status string

const (
	Here    Status = "here"    // this device holds the live lease
	Running Status = "running" // another device holds a live lease
	Off     Status = "off"     // expired and never released: the holder died or slept
	Idle    Status = "idle"    // released
	Branch  Status = "branch"  // OrphanTurns > 0, not archived
)

// Row is one chat as the list shows it.
type Row struct {
	Cell, Title, Device, DeviceID, Parent string
	Status                                Status
	DurableAgo                            time.Duration // Listing.Now - DurableAt, directory clock only
	DurableAt                             int64         // directory ms of the last durable turn: the fact DurableAgo is read from
	Pending, OrphanTurns                  uint32
	// Mine says this device was the chat's last holder, so a lapsed lease is
	// this device's own to pick up and not another device's work gone quiet.
	Mine bool
}

// Opener opens a sealed name with the metadata key.
type Opener func(sealed string) (string, error)

// Source is anything that can list chats across machines.
type Source interface {
	Rows(ctx context.Context) ([]Row, error)
}

// Versioned is a Source that can also say which directory version its answer
// was read at, so a screen following the change feed can tell a frame it has
// already read past from one that is news.
type Versioned interface {
	Source
	RowsAt(ctx context.Context) ([]Row, uint64, error)
}

// RowsAt reads src and reports the version it was read at, or 0 when the
// source does not keep versions.
func RowsAt(ctx context.Context, src Source) ([]Row, uint64, error) {
	if v, ok := src.(Versioned); ok {
		return v.RowsAt(ctx)
	}
	rows, err := src.Rows(ctx)
	return rows, 0, err
}

// Static is the fake Source: it always answers with itself.
type Static []Row

// Rows implements Source.
func (s Static) Rows(context.Context) ([]Row, error) { return []Row(s), nil }

const (
	untitled  = "untitled"
	devPrefix = "dev_"
	idPrefix  = 8
)

// Rows builds the list from a directory listing. self is this device's id.
// Archived cells are hidden; the newest durable turn comes first and ties
// break by cell id so the order never flickers.
func Rows(l directory.Listing, self string, open Opener) []Row {
	rows := make([]Row, 0, len(l.Cells))
	for id, c := range l.Cells {
		if !c.Archived {
			rows = append(rows, rowOf(id, c, l, self, open))
		}
	}
	sort.Slice(rows, func(i, j int) bool { return newer(rows[i], rows[j]) })
	return rows
}

// newer orders by smaller DurableAgo (a newer turn), then by cell id.
func newer(a, b Row) bool {
	if a.DurableAgo != b.DurableAgo {
		return a.DurableAgo < b.DurableAgo
	}
	return a.Cell < b.Cell
}

func rowOf(id string, c directory.Cell, l directory.Listing, self string, open Opener) Row {
	return Row{
		Cell:        id,
		Title:       nameOr(open, c.Title, untitled),
		Device:      deviceName(l.Devices, c.Lease.Device, open),
		DeviceID:    c.Lease.Device,
		Parent:      c.ParentCell,
		Status:      heldStatus(c, l, self),
		Mine:        c.Lease.Device == self,
		DurableAgo:  time.Duration(l.Now-c.DurableAt) * time.Millisecond,
		DurableAt:   c.DurableAt,
		Pending:     c.Lease.Pending,
		OrphanTurns: c.OrphanTurns,
	}
}

// heldStatus is the chat's status, except that a lease held by a revoked device
// is no lease: that device can no longer release it or be reached, so the chat
// reads as released and is never offered as running or lapsed elsewhere.
func heldStatus(c directory.Cell, l directory.Listing, self string) Status {
	st := statusOf(c, l.Now, self)
	if holder, ok := l.Devices[c.Lease.Device]; ok && holder.Revoked && (st == Running || st == Off) {
		return Idle
	}
	return st
}

// statusOf applies the precedence Branch, Here, Running, Off, Idle.
func statusOf(c directory.Cell, now int64, self string) Status {
	switch {
	case c.OrphanTurns > 0:
		return Branch
	case c.Lease.Expires > now && c.Lease.Device == self:
		return Here
	case c.Lease.Expires > now:
		return Running
	case c.Lease.Expires != 0:
		return Off
	}
	return Idle
}

// DeviceName is the name of device id as the list shows it, for the surfaces
// that name a device outside a row.
func DeviceName(devs map[string]directory.Device, id string, open Opener) string {
	return deviceName(devs, id, open)
}

// deviceName opens the holder's sealed name; a name that will not open, or a
// device the listing does not know, shows the first hex digits of its id.
func deviceName(devs map[string]directory.Device, id string, open Opener) string {
	return nameOr(open, devs[id].Name, shortID(id))
}

func shortID(id string) string {
	hex := strings.TrimPrefix(id, devPrefix)
	if len(hex) > idPrefix {
		hex = hex[:idPrefix]
	}
	return hex
}

// nameOr opens a sealed name and falls back when it is empty or will not open.
func nameOr(open Opener, sealed, fallback string) string {
	if sealed == "" {
		return fallback
	}
	if s, err := open(sealed); err == nil && s != "" {
		return s
	}
	return fallback
}
