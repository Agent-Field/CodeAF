package syncsetup

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstats"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// unknownDevice is said for the machine that took a chat when the directory
// could not name it; the line still has to be a sentence.
const unknownDevice = "another device"

// DriveOptions is what a chat tells its drive side about itself.
type DriveOptions struct {
	// DeviceName is this machine's name, sealed into its directory record.
	DeviceName string
	// Title is the chat's title as it is now; nil publishes none.
	Title func() string
	// OnNotice hears a person-facing line: the superseded line, the skew line.
	OnNotice func(string)
	// Sleep replaces real time between flushes; tests inject a fake.
	Sleep func(ctx context.Context, d time.Duration) error
}

// Drive is the drive side of one chat: it publishes each sealed turn, holds
// the lease with heartbeats, and gives it back when the chat closes. When
// another device takes the chat over it becomes a viewer (L7).
type Drive struct {
	sync    *Sync
	cell    cell.Cell
	opt     DriveOptions
	batcher *cellsync.Batcher

	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once

	sent string // fingerprint of the vault as last sent; touched by the flush loop and Close only

	mu   sync.Mutex
	line string // the superseded line, empty while this device drives
}

// Drive starts the drive side of c. The lease is acquired when the chat has a
// directory record already and created by its first publish otherwise. A chat
// another device is driving right now starts as a viewer. Any directory error
// is returned before anything runs, so the caller can carry on unsynced.
func (s *Sync) Drive(ctx context.Context, eng cellstore.Engine, c cell.Cell, opt DriveOptions) (*Drive, error) {
	d := &Drive{sync: s, cell: c, opt: opt, done: make(chan struct{})}
	if err := s.putDevice(ctx, opt.DeviceName); err != nil {
		return nil, err
	}
	d.syncVault(ctx)
	drv, held, err := s.driving(ctx, c)
	if err != nil {
		return nil, err
	}
	if held != "" {
		d.becomeViewer(cellsync.Superseded{By: held})
		close(d.done)
		return d, nil
	}
	if d.batcher, err = s.batcher(eng, c, drv, d); err != nil {
		return nil, err
	}
	run, cancel := context.WithCancel(context.WithoutCancel(ctx))
	d.cancel = cancel
	go func() {
		defer close(d.done)
		_ = d.batcher.Run(run)
	}()
	return d, nil
}

// syncVault sends this machine's secrets along with the chat, so a machine that
// continues the chat can put its .env back. The vault changes while a chat
// runs (the first seal imports the project's .env), so it is sent whenever its
// content differs from what was last sent, not once at the start. It is best
// effort and says nothing when it fails: the fingerprint is then not recorded,
// so the next flush tries again, and a seal never waits on it.
func (d *Drive) syncVault(ctx context.Context) {
	fp, ok := keys.Fingerprint(d.sync.Home)
	if !ok || fp == d.sent {
		return
	}
	syncer, err := d.sync.vaultSyncer(nil)
	if err != nil || syncer.Push(ctx) != nil {
		return
	}
	// The push merges what the directory holds, which may rewrite the file.
	d.sent, _ = keys.Fingerprint(d.sync.Home)
}

// flushed is what runs after each publish: the telemetry, then the vault.
func (d *Drive) flushed(rec *cellstats.Recorder) func(cellsync.Flush) {
	return func(f cellsync.Flush) {
		rec.OnFlush(f)
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		d.syncVault(ctx)
	}
}

// putDevice upserts this device's record with its sealed name and what it can do.
func (s *Sync) putDevice(ctx context.Context, name string) error {
	sealed, err := directory.SealName(directory.MetadataKey(s.Identity.CellKey()), name)
	if err != nil {
		return err
	}
	return s.Dir.PutDevice(ctx, s.Device.ID(), directory.Device{
		V: 1, Name: sealed, AddedBy: s.Identity.ID(),
		Caps: directory.Caps{OS: runtime.GOOS, Arch: runtime.GOARCH, Cow: "none"},
	})
}

// driving finds where this chat stands in the directory: no record yet (fence
// 0), a record this device now holds, or a record another device holds, in
// which case held names that device. A chat that was branched drives its branch.
func (s *Sync) driving(ctx context.Context, c cell.Cell) (cellsync.Driving, string, error) {
	branches, err := cellsync.OpenBranchMap(s.Home)
	if err != nil {
		return cellsync.Driving{}, "", err
	}
	drv := cellsync.Driving{Cell: c}
	if id := branches.Resolve(c.ID); id != c.ID {
		drv.Remote = id
	}
	v, err := s.Dir.Acquire(ctx, drv.ID())
	switch {
	case errors.Is(err, directory.ErrNotFound):
		return drv, "", nil
	case errors.Is(err, directory.ErrLeaseHeld):
		return drv, s.holder(ctx, drv.ID()), nil
	case err != nil:
		return drv, "", err
	}
	drv.Fence, drv.Head = v.Cell.Lease.Fence, v.Cell.Head
	return drv, "", nil
}

// holder is the device that holds the lease of id now, or "" if it cannot be told.
func (s *Sync) holder(ctx context.Context, id string) string {
	v, err := s.Dir.Cell(ctx, id)
	if err != nil {
		return ""
	}
	return v.Cell.Lease.Device
}

// batcher wires the publisher, the brancher and the telemetry to one chat.
func (s *Sync) batcher(eng cellstore.Engine, c cell.Cell, drv cellsync.Driving, d *Drive) (*cellsync.Batcher, error) {
	branches, err := cellsync.OpenBranchMap(s.Home)
	if err != nil {
		return nil, err
	}
	engine := eng.Sync(cellstore.SyncKeys{CellKey: s.Identity.CellKey(), Dedup: s.Identity.DedupSecret()}, s.Ledger)
	rec := cellstats.NewRecorder(s.Home, c.ID, meter(s.Counters))
	pub := &cellsync.Publisher{Engine: engine, Store: s.Store, Dir: s.Dir}
	return &cellsync.Batcher{
		Publisher: pub,
		Brancher: cellsync.Brancher{
			Dir: s.Dir, Publisher: pub, Map: branches,
			NewID: func() (string, error) { return cell.NewID(time.Now()) },
		},
		Driving:      &drv,
		Interval:     s.Interval,
		Info:         d.info,
		OnFlush:      d.flushed(rec),
		OnSuperseded: d.becomeViewer,
		OnError:      d.refusal,
		Sleep:        d.opt.Sleep,
	}, nil
}

// meter reads the store client's counters as the recorder's snapshot.
func meter(c *blobstore.Counters) cellstats.Meter {
	return cellstats.MeterFunc(func() cellstats.Counts {
		return cellstats.Counts{
			Puts: c.Puts.Load(), Gets: c.Gets.Load(), Has: c.Has.Load(),
			BytesUp: c.BytesUp.Load(), BytesDown: c.BytesDown.Load(),
		}
	})
}

// info is what a publish tells the directory about this chat.
func (d *Drive) info() cellsync.PublishInfo {
	title := ""
	if d.opt.Title != nil {
		title = d.opt.Title()
	}
	return d.sync.publishInfo(d.cell, title)
}

// becomeViewer is the Batcher's word that another device took the chat over:
// from now on this window only shows it, and says who has it.
func (d *Drive) becomeViewer(s cellsync.Superseded) {
	line := chatlist.Superseded(d.sync.deviceName(s.By))
	d.mu.Lock()
	d.line = line
	d.mu.Unlock()
	d.notice(line)
}

// refusal shows the person-facing line of a refused request.
func (d *Drive) refusal(err error) {
	if errors.Is(err, wireauth.ErrSkew) {
		d.notice(chatlist.ClockOff)
	}
}

func (d *Drive) notice(line string) {
	if d.opt.OnNotice != nil {
		d.opt.OnNotice(line)
	}
}

// deviceName is the name the person gave a device, or the first digits of its
// id when the directory cannot say.
func (s *Sync) deviceName(id string) string {
	if id == "" {
		return unknownDevice
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	l, err := s.Dir.List(ctx)
	if err != nil {
		return chatlist.DeviceName(nil, id, nil)
	}
	key := directory.MetadataKey(s.Identity.CellKey())
	return chatlist.DeviceName(l.Devices, id, func(sealed string) (string, error) { return directory.OpenName(key, sealed) })
}

// Viewer is the superseded line once another device has taken the chat, and
// false while this device drives it.
func (d *Drive) Viewer() (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.line, d.line != ""
}

// Gate refuses with the superseded line once the chat is a viewer, and answers
// nil while this device drives it. It is what a viewer's tool calls meet.
func (d *Drive) Gate() error {
	if line, viewer := d.Viewer(); viewer {
		return errors.New(line)
	}
	return nil
}

// Store is the store a seal goes through: the engine stamped with this device
// and the fence it holds, then noted for the Batcher. A chat that started as a
// viewer seals on the engine alone.
func (d *Drive) Store(e cellstore.Engine) cellstore.Store {
	if d.batcher == nil {
		return e
	}
	stamped := stamping{engine: e, device: d.sync.Device.Cert.Device, fence: d.batcher.Fence}
	return &cellsync.Publishing{Inner: stamped, Batcher: d.batcher}
}

// stamping writes the device key and the current fence into each turn it seals.
type stamping struct {
	engine cellstore.Engine
	device string
	fence  func() uint64
}

var _ cellstore.Store = stamping{}

// Seal implements cellstore.Store.
func (s stamping) Seal(ctx context.Context, c cell.Cell, info cellstore.TurnInfo) (cellstore.Sealed, error) {
	e := s.engine
	e.Identity = cellstore.Identity{Device: s.device, Fence: s.fence()}
	return e.Seal(ctx, c, info)
}

// Close ends the drive side: what is sealed is published, and the lease is
// given back so another device can take the chat at once. It is safe to call
// on every exit path, and more than once.
func (d *Drive) Close(ctx context.Context) error {
	var err error
	d.once.Do(func() {
		if d.batcher == nil {
			return
		}
		d.cancel()
		<-d.done
		d.syncVault(ctx)
		err = d.batcher.Close(ctx)
	})
	return err
}
