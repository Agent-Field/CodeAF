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

// locked runs fn holding the directory lock.
func (m *Memory) locked(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn()
}

func (c *memoryClient) List(context.Context) (l Listing, err error) {
	c.m.locked(func() {
		l = Listing{
			Now: c.m.now(), Identity: c.m.identity,
			Devices: maps.Clone(c.m.devices), Cells: map[string]Cell{},
		}
		for id, cell := range c.m.cells {
			l.Cells[id] = copyCell(cell)
		}
	})
	return l, nil
}

func (c *memoryClient) Cell(_ context.Context, id string) (v CellView, err error) {
	c.m.locked(func() {
		v, err = c.m.change(id, func(cell Cell, _ int64) (Cell, error) { return cell, nil })
	})
	return v, err
}

func (c *memoryClient) PutDevice(_ context.Context, id string, d Device) error {
	c.m.locked(func() { c.m.devices[id] = d })
	return nil
}

func (c *memoryClient) SetVault(_ context.Context, old, next string) (err error) {
	c.m.locked(func() {
		if c.m.identity.Vault != old {
			err = ErrCAS
			return
		}
		c.m.identity.Vault = next
	})
	return err
}

func (c *memoryClient) Create(_ context.Context, id string, in CellInit) (v CellView, err error) {
	c.m.locked(func() {
		if _, taken := c.m.cells[id]; taken {
			err = ErrExists
			return
		}
		now := c.m.now()
		cell := Created(in, c.device, now)
		c.m.cells[id] = cell
		v = CellView{Now: now, Cell: copyCell(cell)}
	})
	return v, err
}

func (c *memoryClient) Acquire(_ context.Context, id string) (v CellView, err error) {
	c.m.locked(func() {
		v, err = c.m.change(id, func(cell Cell, now int64) (Cell, error) { return Acquire(cell, c.device, now) })
	})
	return v, err
}

func (c *memoryClient) Heartbeat(_ context.Context, id string, b Beat) (v CellView, err error) {
	c.m.locked(func() {
		v, err = c.m.change(id, func(cell Cell, now int64) (Cell, error) { return Heartbeat(cell, c.device, b, now) })
	})
	return v, err
}

func (c *memoryClient) Publish(_ context.Context, id string, p Publish) (v CellView, err error) {
	c.m.locked(func() {
		v, err = c.m.change(id, func(cell Cell, now int64) (Cell, error) { return PublishTo(cell, c.device, p, now) })
	})
	return v, err
}

func (c *memoryClient) Release(_ context.Context, id string, fence uint64) (err error) {
	c.m.locked(func() {
		_, err = c.m.change(id, func(cell Cell, _ int64) (Cell, error) { return ReleaseOf(cell, c.device, fence) })
	})
	return err
}

func (c *memoryClient) Archive(_ context.Context, id string) (err error) {
	c.m.locked(func() {
		_, err = c.m.change(id, func(cell Cell, _ int64) (Cell, error) {
			cell.Archived = true
			return cell, nil
		})
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
