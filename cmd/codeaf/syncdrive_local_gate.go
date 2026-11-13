package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

const localOwnershipFreshFor = 2 * time.Second
const localOwnershipFeedFreshFor = 15 * time.Second

// localToolGate checks shared local writers without acquiring or renewing a hold.
// The feed invalidates cached readings; the bound covers missing or disconnected updates.
type localToolGate struct {
	mu        sync.Mutex
	cell      cell.Cell
	say       func(string)
	sync      *syncsetup.Sync
	feed      dirwatch.Follower
	checked   time.Time
	version   uint64
	available bool
	line      string
	closed    bool
}

func (g *localToolGate) Gate() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.line != "" {
		return errors.New(g.line)
	}
	if g.closed || syncsetup.Quiet(home.Dir()) {
		return nil
	}
	if g.sync == nil {
		s, ok, err := syncsetup.Open(home.Dir())
		if err != nil || !ok || s.StorageAllowed() {
			return nil
		}
		g.sync = s
	}
	remote, fence, receiptErr := g.sync.SendLease(g.cell.ID)
	if receiptErr != nil || fence == 0 {
		shared, err := cellstore.Shared(g.cell)
		if err != nil || !shared {
			return nil
		}
		remote = g.cell.ID
	}
	if g.feed == nil {
		g.feed = g.sync.Follow()
	}
	var state dirwatch.State
	if g.feed != nil {
		state = g.feed.State()
	}
	version := state.Version
	freshFor := localOwnershipFreshFor
	if state.Up && g.available {
		freshFor = localOwnershipFeedFreshFor
	}
	if !g.checked.IsZero() && version <= g.version && time.Since(g.checked) < freshFor {
		return nil
	}
	g.version = version
	ctx, cancel := context.WithTimeout(context.Background(), holdsLeaseWithin)
	defer cancel()
	started := time.Now()
	view, err := g.sync.Dir.Cell(ctx, remote)
	// Queued tools share the completed attempt even when a failed request was slow.
	g.checked = started
	if err != nil {
		g.checked = time.Now()
	}
	g.available = err == nil
	// An unavailable reading cannot establish another holder, so offline work continues.
	if err != nil || view.Cell.Lease.Device == "" || view.Cell.Lease.Device == g.sync.Device.ID() || view.Cell.Lease.Expires <= view.Now {
		return nil
	}
	name := "another device"
	if listing, err := g.sync.Dir.List(ctx); err == nil {
		key := directory.MetadataKey(g.sync.Identity.CellKey())
		name = chatlist.DeviceName(listing.Devices, view.Cell.Lease.Device, func(sealed string) (string, error) { return directory.OpenName(key, sealed) })
	}
	g.line = chatlist.Superseded(name)
	if g.say != nil {
		g.say(g.line)
	}
	return errors.New(g.line)
}

func (g *localToolGate) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
	if g.feed != nil {
		g.feed.Close()
		g.feed = nil
	}
}

func (g *localToolGate) superseded() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.line != ""
}
