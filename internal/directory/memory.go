package directory

import (
	"context"
	"maps"
	"sync"
	"time"
)

// Memory is a small real directory held in memory. It applies the pure rules
// under one lock per write and stamps every time from its own clock. Many
// devices share one Memory, each through the Client that For returns.
type Memory struct {
	clock func() time.Time

	mu       sync.Mutex
	identity IdentityRec
	devices  map[string]Device
	cells    map[string]Cell
}

// NewMemory returns an empty directory that reads time from clock.
func NewMemory(clock func() time.Time) *Memory {
	return &Memory{
		clock:    clock,
		identity: IdentityRec{V: 1},
		devices:  map[string]Device{},
		cells:    map[string]Cell{},
	}
}

// For returns the Client that device would hold: every write it makes is made
// as that device.
func (m *Memory) For(device string) Client { return &memoryClient{m: m, device: device} }

type memoryClient struct {
	m      *Memory
	device string
}

func (m *Memory) now() int64 { return m.clock().UnixMilli() }

// change reads cell id, applies fn and stores the result when fn accepts it.
// The caller holds the lock.
func (m *Memory) change(id string, fn func(Cell, int64) (Cell, error)) (CellView, error) {
	c, ok := m.cells[id]
	if !ok {
		return CellView{}, ErrNotFound
	}
	now := m.now()
	next, err := fn(c, now)
	if err != nil {
		return CellView{}, err
	}
	m.cells[id] = next
	return CellView{Now: now, Cell: copyCell(next)}, nil
}

// locked runs fn holding the directory lock, unless the calling device has
// been revoked: then fn never runs and the caller is told so. Every verb goes
// through here, which makes "refused on every verb" a fact of one line.
func (c *memoryClient) locked(fn func() error) error {
	c.m.mu.Lock()
	defer c.m.mu.Unlock()
	if c.m.devices[c.device].Revoked {
		return ErrRevoked
	}
	return fn()
}

// changed is change under the lock, for the calling device.
func (c *memoryClient) changed(id string, fn func(Cell, int64) (Cell, error)) (v CellView, err error) {
	err = c.locked(func() (err error) {
		v, err = c.m.change(id, fn)
		return err
	})
	return v, err
}

func (c *memoryClient) List(context.Context) (l Listing, err error) {
	err = c.locked(func() error {
		l = Listing{
			Now: c.m.now(), Identity: c.m.identity,
			Devices: maps.Clone(c.m.devices), Cells: map[string]Cell{},
		}
		for id, cell := range c.m.cells {
			l.Cells[id] = copyCell(cell)
		}
		return nil
	})
	return l, err
}

func (c *memoryClient) Cell(_ context.Context, id string) (CellView, error) {
	return c.changed(id, func(cell Cell, _ int64) (Cell, error) { return cell, nil })
}

// PutDevice keeps the stored Revoked flag whatever the record says, so the
// only way to stop a device is Revoke and a device cannot clear its own stop.
func (c *memoryClient) PutDevice(_ context.Context, id string, d Device) error {
	return c.locked(func() error {
		d.Revoked = c.m.devices[id].Revoked
		c.m.devices[id] = d
		return nil
	})
}

func (c *memoryClient) Revoke(_ context.Context, id string) error {
	return c.locked(func() error {
		d, ok := c.m.devices[id]
		if !ok {
			return ErrNotFound
		}
		d, err := RevokeOf(d, c.device, id)
		if err == nil {
			c.m.devices[id] = d
		}
		return err
	})
}

func (c *memoryClient) SetVault(_ context.Context, old, next string) error {
	return c.locked(func() error {
		if c.m.identity.Vault != old {
			return ErrCAS
		}
		c.m.identity.Vault = next
		return nil
	})
}

func (c *memoryClient) Create(_ context.Context, id string, in CellInit) (v CellView, err error) {
	err = c.locked(func() error {
		if _, taken := c.m.cells[id]; taken {
			return ErrExists
		}
		now := c.m.now()
		cell := Created(in, c.device, now)
		c.m.cells[id] = cell
		v = CellView{Now: now, Cell: copyCell(cell)}
		return nil
	})
	return v, err
}

func (c *memoryClient) Acquire(_ context.Context, id string, o AcquireOpts) (CellView, error) {
	return c.changed(id, func(cell Cell, now int64) (Cell, error) { return Acquire(cell, c.device, now, o.Force) })
}

func (c *memoryClient) Heartbeat(_ context.Context, id string, b Beat) (CellView, error) {
	return c.changed(id, func(cell Cell, now int64) (Cell, error) { return Heartbeat(cell, c.device, b, now) })
}

func (c *memoryClient) Publish(_ context.Context, id string, p Publish) (CellView, error) {
	return c.changed(id, func(cell Cell, now int64) (Cell, error) { return PublishTo(cell, c.device, p, now) })
}

func (c *memoryClient) Release(_ context.Context, id string, fence uint64) error {
	_, err := c.changed(id, func(cell Cell, _ int64) (Cell, error) { return ReleaseOf(cell, c.device, fence) })
	return err
}

func (c *memoryClient) Archive(_ context.Context, id string) error {
	_, err := c.changed(id, func(cell Cell, _ int64) (Cell, error) {
		cell.Archived = true
		return cell, nil
	})
	return err
}

// copyCell detaches the key map so a caller cannot edit the stored record.
func copyCell(c Cell) Cell {
	keys := make(map[string]map[string]string, len(c.Keys))
	for id, wrapped := range c.Keys {
		keys[id] = maps.Clone(wrapped)
	}
	c.Keys = keys
	return c
}
