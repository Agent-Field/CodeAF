// Package chatlist turns the directory's listing into the rows a person sees
// and the sentences that go with them. It is pure: no clock, no network, no
// terminal. The home screen and `codeaf cell list --all` both draw from it, so
// the copy in copy.go is the one source of truth for both.
package chatlist

import (
	"context"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/devname"
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

// BranchCopy is one unsent branch copy set aside for a chat during a handoff.
// Redundant reports that every turn in this copy is already in the main chat,
// so archiving it loses nothing.
type BranchCopy struct {
	Cell        string
	Device      string
	OrphanTurns uint32
	Redundant   bool
}

// Row is one chat as the list shows it.
type Row struct {
	Cell, Title, Device, DeviceID, Parent string
	Head                                  string
	Status                                Status
	DurableAgo                            time.Duration // Listing.Now - DurableAt, directory clock only
	DurableAt                             int64         // directory ms of the last durable turn: the fact DurableAgo is read from
	Pending, OrphanTurns                  uint32
	// Fence binds the continuation card to the holder the person saw.
	Fence uint64
	// Mine says this device was the chat's last holder, so a lapsed lease is
	// this device's own to pick up and not another device's work gone quiet.
	Mine bool
	// Released distinguishes a real release from a revoked or offline holder.
	Released    bool
	ReleasedAgo time.Duration
	// HolderRemoved says the last holder was removed from the person's
	// devices: its lease reads as released and it is never offered (#2072).
	HolderRemoved bool
	// BranchRedundant reports that a branch row's saved head is already in its
	// parent chat's turn history, so archiving the branch loses nothing.
	BranchRedundant bool
	// Copies are branch copies of this chat collapsed onto its single Home row.
	Copies []BranchCopy
}

// Quiet is the row of a chat whose live holder has gone: a lease that still
// runs on a device that is no longer online is a chat that stopped, so it reads
// as one that went off (`desktop offline`), not `running on desktop`.
func (r Row) Quiet() Row {
	if r.Status == Running {
		r.Status = Off
	}
	return r
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

// SendSource reads ownership for a submitted message independently of display
// hints. The context bounds any wait for a first directory reading.
type SendSource interface {
	RowsForSend(context.Context) ([]Row, uint64, error)
}

// RowsForSend uses the source's ownership door when it has one.
func RowsForSend(ctx context.Context, src Source) ([]Row, uint64, error) {
	if fresh, ok := src.(SendSource); ok {
		return fresh.RowsForSend(ctx)
	}
	return RowsAt(ctx, src)
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
		Head:        c.Head,
		Title:       nameOr(open, c.Title, untitled),
		Device:      deviceName(l.Devices, c.Lease.Device, open),
		DeviceID:    c.Lease.Device,
		Fence:       c.Lease.Fence,
		Parent:      c.ParentCell,
		Status:      heldStatus(c, l, self),
		Mine:        c.Lease.Device == self,
		Released:    c.Lease.Expires == 0 && c.Lease.ReleasedAt > 0 && !l.Devices[c.Lease.Device].Revoked,
		ReleasedAgo: time.Duration(l.Now-c.Lease.ReleasedAt) * time.Millisecond,
		DurableAgo:  time.Duration(l.Now-c.DurableAt) * time.Millisecond,
		DurableAt:   c.DurableAt,
		Pending:     c.Lease.Pending,
		OrphanTurns: c.OrphanTurns,

		HolderRemoved: l.Devices[c.Lease.Device].Revoked,
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

// statusOf applies the precedence Branch, Here, Running, Off, Idle. A lapsed
// lease of this device's own is free, not off: nobody else holds the chat, so
// there is nothing to continue from and it opens without a question (#2072).
func statusOf(c directory.Cell, now int64, self string) Status {
	switch {
	case c.OrphanTurns > 0:
		return Branch
	case c.Lease.Expires > now && c.Lease.Device == self:
		return Here
	case c.Lease.Expires > now:
		return Running
	case c.Lease.Expires != 0 && c.Lease.Device != self:
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
	return devname.Shown(nameOr(open, devs[id].Name, shortID(id)))
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

// CollapseCopies folds branch copies with a known parent chat onto that main
// chat's single row (or returns them under byLocal when the main chat is known
// only locally on Home). Standalone branch rows with no parent stay on their
// own row so orphan branches remain visible and actionable.
func CollapseCopies(rows []Row, localIDs map[string]bool) ([]Row, map[string][]BranchCopy) {
	if len(rows) == 0 {
		return nil, nil
	}
	byCell := make(map[string]int, len(rows))
	mainIdx := make(map[string]int, len(rows))
	for i, r := range rows {
		byCell[r.Cell] = i
		if r.Status != Branch {
			mainIdx[r.Cell] = i
		}
	}
	rootOf := func(r Row) string {
		cur := strings.TrimSpace(r.Parent)
		for hops := 0; cur != "" && hops < 16; hops++ {
			idx, ok := byCell[cur]
			if !ok || rows[idx].Status != Branch {
				break
			}
			next := strings.TrimSpace(rows[idx].Parent)
			if next == "" || next == cur {
				break
			}
			cur = next
		}
		return cur
	}
	orphanCounts := map[string]int{}
	firstBranchIdx := map[string]int{}
	for i, r := range rows {
		if r.Status != Branch {
			continue
		}
		root := rootOf(r)
		if root == "" {
			continue
		}
		if _, ok := mainIdx[root]; ok || localIDs[root] {
			continue
		}
		orphanCounts[root]++
		if _, seen := firstBranchIdx[root]; !seen {
			firstBranchIdx[root] = i
		}
	}

	skip := make([]bool, len(rows))
	attached := make(map[int][]BranchCopy)
	var byLocal map[string][]BranchCopy

	for i, r := range rows {
		if r.Status != Branch {
			continue
		}
		root := rootOf(r)
		if root == "" {
			continue
		}
		if idx, ok := mainIdx[root]; ok {
			redundant := r.BranchRedundant || (r.Head != "" && r.Head == rows[idx].Head)
			attached[idx] = append(attached[idx], BranchCopy{
				Cell:        r.Cell,
				Device:      r.Device,
				OrphanTurns: r.OrphanTurns,
				Redundant:   redundant,
			})
			skip[i] = true
			continue
		}
		if localIDs[root] {
			if byLocal == nil {
				byLocal = make(map[string][]BranchCopy)
			}
			byLocal[root] = append(byLocal[root], BranchCopy{
				Cell:        r.Cell,
				Device:      r.Device,
				OrphanTurns: r.OrphanTurns,
				Redundant:   r.BranchRedundant,
			})
			skip[i] = true
			continue
		}
		if orphanCounts[root] > 1 {
			if idx := firstBranchIdx[root]; idx != i {
				redundant := r.BranchRedundant || (r.Head != "" && r.Head == rows[idx].Head)
				attached[idx] = append(attached[idx], BranchCopy{
					Cell:        r.Cell,
					Device:      r.Device,
					OrphanTurns: r.OrphanTurns,
					Redundant:   redundant,
				})
				skip[i] = true
			}
		}
	}

	out := make([]Row, 0, len(rows))
	for i, r := range rows {
		if skip[i] {
			continue
		}
		if more := attached[i]; len(more) > 0 {
			r.Copies = append(slices.Clone(r.Copies), more...)
		}
		out = append(out, r)
	}
	return out, byLocal
}
