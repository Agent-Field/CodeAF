package main

// The chat's door onto answering new devices and keeping the device list: what
// the approve screen and `/devices` reach through tui3.Approvals. It is the
// same Approver and signed directory `codeaf pair approve` uses, pointed at
// this computer's home, so the terminal and the app cannot disagree.

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// approvalsDoor is the surface's tui3.Approvals.
type approvalsDoor struct {
	dir      string
	mailbox  func(relayFlag string) (pair.Mailbox, error)
	approver func(pair.Mailbox) (pair.Approver, error)

	mu     sync.Mutex
	asking map[string]pair.Asking // by code: what Pending read, for the answer
}

var _ tui3.Approvals = (*approvalsDoor)(nil)

func newApprovalsDoor(dir string) *approvalsDoor {
	return &approvalsDoor{dir: dir, mailbox: pairMailbox, approver: linkApprover(dir), asking: map[string]pair.Asking{}}
}

func (d *approvalsDoor) who() (pair.Approver, error) {
	route, err := d.mailbox("")
	if err != nil {
		return pair.Approver{}, err
	}
	return d.approver(route)
}

func (d *approvalsDoor) Pending(ctx context.Context, typed string) (tui3.PendingDevice, error) {
	ref, err := pair.ReadLink(typed)
	if err != nil {
		return tui3.PendingDevice{}, err
	}
	who, err := d.who()
	if err != nil {
		return tui3.PendingDevice{}, err
	}
	as, err := who.Look(ctx, ref)
	if err != nil {
		return tui3.PendingDevice{}, err
	}
	d.remember(as)
	return pendingOf(as), nil
}

func pendingOf(as pair.Asking) tui3.PendingDevice {
	return tui3.PendingDevice{
		Code: as.Ref.Code, Device: as.DeviceID(), Name: as.Name, Platform: as.Platform,
		Check: as.Check, RequestedAt: as.RequestedAt, ExpiresAt: as.ExpiresAt,
	}
}

func (d *approvalsDoor) remember(as pair.Asking) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.asking[as.Ref.Code] = as
}

func (d *approvalsDoor) recall(p tui3.PendingDevice) (pair.Asking, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	as, ok := d.asking[p.Code]
	if !ok {
		return pair.Asking{}, pair.ErrLinkGone
	}
	return as, nil
}

func (d *approvalsDoor) Approve(ctx context.Context, p tui3.PendingDevice) error {
	return d.answer(p, func(who pair.Approver, as pair.Asking) error { return who.Approve(ctx, as) })
}

func (d *approvalsDoor) Deny(ctx context.Context, p tui3.PendingDevice) error {
	return d.answer(p, func(who pair.Approver, as pair.Asking) error { return who.Deny(ctx, as) })
}

// answer runs one decision on the request Pending read, and forgets it after.
func (d *approvalsDoor) answer(p tui3.PendingDevice, decide func(pair.Approver, pair.Asking) error) error {
	as, err := d.recall(p)
	if err != nil {
		return err
	}
	who, err := d.who()
	if err != nil {
		return err
	}
	if err := decide(who, as); err != nil {
		return err
	}
	d.forget(p.Code)
	return nil
}

func (d *approvalsDoor) forget(code string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.asking, code)
}

// home is this computer's signed directory, its identity and its device id.
func (d *approvalsDoor) home() (directory.Client, identity.Identity, string, error) {
	route, err := d.mailbox("")
	if err != nil {
		return nil, identity.Identity{}, "", err
	}
	client, id, err := homeDirectory(d.dir, route)
	if err != nil {
		return nil, id, "", err
	}
	dev, err := identity.Device(d.dir)
	return client, id, dev.ID(), err
}

func (d *approvalsDoor) Devices(ctx context.Context) ([]tui3.DeviceRow, error) {
	client, id, self, err := d.home()
	if err != nil {
		return nil, err
	}
	listing, err := client.List(ctx)
	if err != nil {
		return nil, err
	}
	return rowsOf(listing, directory.MetadataKey(id.CellKey()), self), nil
}

func rowsOf(l directory.Listing, key []byte, self string) []tui3.DeviceRow {
	rows := make([]tui3.DeviceRow, 0, len(l.Devices))
	for id, dev := range l.Devices {
		rows = append(rows, rowOf(id, dev, key, self))
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Name < rows[j].Name || (rows[i].Name == rows[j].Name && rows[i].ID < rows[j].ID)
	})
	return rows
}

func rowOf(id string, dev directory.Device, key []byte, self string) tui3.DeviceRow {
	row := tui3.DeviceRow{ID: id, Name: openName(key, dev.Name), Platform: dev.Platform, Self: id == self, Revoked: dev.Revoked}
	if dev.LastSeen > 0 {
		row.LastSeen = time.UnixMilli(dev.LastSeen)
	}
	return row
}

// openName is a sealed name in words, and "a device" when it cannot be opened.
func openName(key []byte, sealed string) string {
	if name, err := directory.OpenName(key, sealed); err == nil && strings.TrimSpace(name) != "" {
		return name
	}
	return "a device"
}

func (d *approvalsDoor) Revoke(ctx context.Context, id string) error {
	client, _, _, err := d.home()
	if err != nil {
		return err
	}
	return client.Revoke(ctx, id)
}

func (d *approvalsDoor) DeviceName(sealed string) string {
	id, err := identity.Load(d.dir)
	if err != nil {
		return "a new device"
	}
	return openName(directory.MetadataKey(id.CellKey()), sealed)
}
