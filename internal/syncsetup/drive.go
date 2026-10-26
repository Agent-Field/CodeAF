package syncsetup

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/vaultsync"
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
	// OnViewer carries the same sentence with the device that continued it.
	// It runs as soon as new work is refused, before pending saves settle.
	OnViewer func(Viewer)
	// OnDriving says startup acquired a writable conversation, before its
	// background loop can announce a later move.
	OnDriving func()
	// Sleep replaces real time between flushes; tests inject a fake.
	Sleep func(ctx context.Context, d time.Duration) error
}

// Drive is the drive side of one chat: it publishes each sealed turn, holds
// the lease with heartbeats, and gives it back when the chat closes. When
// another device takes the chat over it becomes a viewer (L7).
type Drive struct {
	sync    *Sync
	files   vaultsync.Files // the chat's withheld files, carried beside the secrets
	cell    cell.Cell
	opt     DriveOptions
	batcher *cellsync.Batcher
	scope   *Scope // what this chat's publishes are counted under

	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once

	sent string // fingerprint of the vault as last sent; touched by the flush loop and Close only

	mu     sync.Mutex
	viewer Viewer // the superseded sentence and device, empty while this device drives
}

// Viewer names the computer that continued a chat and the engine's sentence.
type Viewer struct {
	ID   string
	Name string
	Line string
}

// Drive starts the drive side of c. The lease is acquired when the chat has a
// directory record already and created by its first publish otherwise. A chat
// another device is driving right now starts as a viewer. Any directory error
// is returned before anything runs, so the caller can carry on unsynced.
func (s *Sync) Drive(ctx context.Context, eng cellstore.Engine, c cell.Cell, opt DriveOptions) (*Drive, error) {
	if Quiet(s.Home) {
		return nil, ErrQuiet
	}
	files, err := s.files(eng, c)
	if err != nil {
		return nil, err
	}
	d := &Drive{sync: s, files: files, cell: c, opt: opt, done: make(chan struct{})}
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
	d.catchUp(drv)
	if opt.OnDriving != nil {
		opt.OnDriving()
	}
	run, cancel := context.WithCancel(context.WithoutCancel(ctx))
	d.cancel = cancel
	go func() {
		defer close(d.done)
		_ = d.batcher.Run(run)
	}()
	return d, nil
}

// catchUp notes the newest turn sealed on this machine when the relay does not
// hold it yet. The Batcher's list of turns not yet durable lived in the process
// that sealed them, so a run that died (a kill, a dead battery) before its last
// upload left turns that nobody would send until the next seal. A relay head that
// is not one of this machine's own turns means another machine has moved the chat
// on, and nothing is noted: sending an older head would take it back.
func (d *Drive) catchUp(drv cellsync.Driving) {
	head, err := cellstore.Head(d.cell)
	if err != nil || head == nil {
		return
	}
	known, err := cellstore.KnowsHead(d.cell, drv.Head)
	if err != nil || head.Turn.ID == drv.Head || !known {
		return
	}
	d.batcher.Note(head.Turn)
}

// CatchUp notes the newest startup seal once an asynchronous drive is ready.
// An adopted head is already durable; a viewer never publishes anything.
func (d *Drive) CatchUp() {
	if d.batcher == nil {
		return
	}
	if head, err := cellstore.Head(d.cell); err == nil && head != nil {
		d.batcher.Note(head.Turn)
	}
}

// syncVault sends this machine's secrets along with the chat, so a machine that
// continues the chat can put its withheld files and its credentials back. The vault
// changes while a chat runs (a seal may withhold a file, and a
// connection may be saved), so it is sent whenever its content differs from what
// was last sent, not once at the start. The withheld files and credentials.json are captured into the
// vault first, since a save to it is a change of the vault the fingerprint must
// see. It is best effort and says nothing when it fails: the fingerprint is then
// not recorded, so the next flush tries again, and a seal never waits on it.
func (d *Drive) syncVault(ctx context.Context) {
	err := d.sync.withVault(func(v vaultsync.Syncer) error {
		if err := v.Capture(); err != nil {
			return err
		}
		return d.pushChanged(ctx, v)
	}, d.files)
	if err != nil {
		return
	}
	// The push merges what the directory holds, which may rewrite the file.
	d.sent, _ = keys.Fingerprint(d.sync.Home)
}

// pushChanged sends the vault only when its content differs from what was last
// sent, so an unchanged vault costs no request.
func (d *Drive) pushChanged(ctx context.Context, v vaultsync.Syncer) error {
	if fp, ok := keys.Fingerprint(d.sync.Home); !ok || fp == d.sent {
		return nil
	}
	return v.Push(ctx)
}

// flushed is what runs after each publish: the telemetry, then the vault.
func (d *Drive) flushed() func(cellsync.Flush) {
	return func(f cellsync.Flush) {
		if err := cellstore.NotePublished(d.cell, f.Head); err != nil {
			d.notice("could not record which turns reached your other devices: " + err.Error())
		}
		d.scope.Flushed(f)
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		d.syncVault(ctx)
	}
}

// putDevice upserts this device's record with its sealed name and what it can do.
func (s *Sync) putDevice(ctx context.Context, name string) error {
	return PutOwnDevice(ctx, s.Dir, s.Identity, s.Device, name)
}

// PutOwnDevice upserts a device's own directory record at dir: the same sealed
// name and capability row every chat start makes ([Sync.putDevice]), exposed so
// the doors that finish a pairing can make it too. A device may only ever write
// its own record, the name travels sealed under the identity's key, and the
// relay never learns it.
func PutOwnDevice(ctx context.Context, dir directory.Client, id identity.Identity, dev identity.Dev, name string) error {
	rec, err := deviceRecord(id, name)
	if err != nil {
		return err
	}
	return dir.PutDevice(ctx, dev.ID(), rec)
}

// Rename tells the identity's other devices what this one is now called. It is
// the same upsert every chat start makes ([Sync.putDevice]): a device may only
// ever write its own record, the name travels sealed under the identity's key,
// and the relay never learns it.
func (s *Sync) Rename(ctx context.Context, name string) error { return s.putDevice(ctx, name) }

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
	view, err := s.Dir.Cell(ctx, drv.ID())
	if errors.Is(err, directory.ErrNotFound) {
		return drv, "", nil
	}
	if err != nil {
		return drv, "", err
	}
	known, err := cellstore.KnowsHead(c, view.Cell.Head)
	if err != nil {
		return drv, "", err
	}
	// A released lease is not permission to publish an older tree over it.
	// The ordinary continuation restores the newer head and keeps local work;
	// until the person takes that door this window only shows its local copy.
	if !known {
		return drv, view.Cell.Lease.Device, nil
	}
	v, err := s.Dir.Acquire(ctx, drv.ID(), directory.AcquireOpts{Expect: &directory.LeaseExpectation{
		Device: view.Cell.Lease.Device, Fence: view.Cell.Lease.Fence,
	}})
	switch {
	case errors.Is(err, directory.ErrNotFound):
		return drv, "", nil
	case errors.Is(err, directory.ErrLeaseHeld):
		return drv, s.holder(ctx, drv.ID()), nil
	case err != nil:
		return drv, "", err
	}
	known, err = cellstore.KnowsHead(c, v.Cell.Head)
	if err != nil || !known {
		_ = s.Dir.Release(ctx, drv.ID(), v.Cell.Lease.Fence)
		return drv, view.Cell.Lease.Device, err
	}
	drv.Fence, drv.Head = v.Cell.Lease.Fence, v.Cell.Head
	// The plan comes with the record: the frames the last publisher named for
	// this head, which this device's publishes now extend (contract §22.2).
	drv.Frames = v.Cell.Frames
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
	engine.MaxFrame = blobstore.BulkFrame // a flush's objects share frames up to the bulk size (contract §22.6); a small save still writes one small frame
	d.scope = s.scope(c.ID)
	pub := &cellsync.Publisher{Engine: engine, Store: d.scope.Store, Dir: s.Dir}
	return &cellsync.Batcher{
		Publisher: pub,
		Brancher: cellsync.Brancher{
			Dir: s.Dir, Publisher: pub, Map: branches,
			NewID: func() (string, error) { return cell.NewID(time.Now()) },
		},
		Driving:      &drv,
		Interval:     s.Interval,
		Info:         d.info,
		OnFlush:      d.flushed(),
		OnSuperseded: d.becomeViewer,
		OnError:      d.refusal,
		Sleep:        d.opt.Sleep,
		Liveness:     s.Liveness(),
	}, nil
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
	name := d.sync.deviceName(s.By)
	line := chatlist.Superseded(name)
	viewer := Viewer{ID: s.By, Name: name, Line: line}
	d.mu.Lock()
	d.viewer = viewer
	d.mu.Unlock()
	if d.opt.OnViewer != nil {
		d.opt.OnViewer(viewer)
	}
	// Refuse new work immediately, then preserve the pending save before
	// announcing displacement. A routine ownership check does no fsync (C28).
	ctx, cancel := context.WithTimeout(context.Background(), cellstore.SealWithin)
	defer cancel()
	if err := cellstore.Await(ctx, d.cell); err != nil {
		d.notice(err.Error())
	}
	if s.Branch != "" {
		d.notice(chatlist.KeptEdits(s.Turns, d.opt.DeviceName))
	}
	d.notice(line)
}

// refusal shows the person-facing line of a refused request, which the
// refusal table in cellsync holds beside how the request is retried.
func (d *Drive) refusal(err error) {
	if r, ok := cellsync.RefusalOf(err); ok {
		d.notice(r.Say(err))
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
	return d.viewer.Line, d.viewer.Line != ""
}

// HoldsLease says whether this drive side still drives the chat: it is not a
// viewer, and the directory's lease is this device's at the fence this drive
// side publishes under. A lease taken by another device since (a take by force
// moves it without telling the drive side that was idle) answers false. A
// directory that cannot be asked answers true, because a drive side that cannot
// tell must not be thrown away for it.
func (d *Drive) HoldsLease(ctx context.Context) bool {
	if _, viewer := d.Viewer(); viewer || d.batcher == nil {
		return false
	}
	v, err := d.sync.Dir.Cell(ctx, d.cell.ID)
	if err != nil {
		return true
	}
	lease := v.Cell.Lease
	return lease.Device == d.sync.Device.ID() && lease.Fence == d.batcher.Fence()
}

// CheckLease closes the stale-window gap before a tool runs, even when no
// heartbeat or directory change has reached this process since the move.
func (d *Drive) CheckLease(ctx context.Context) error {
	if err := cellstore.Captured(ctx, d.cell); err != nil {
		return err
	}
	if _, viewer := d.Viewer(); viewer || d.batcher == nil {
		return nil
	}
	v, err := d.sync.Dir.Cell(ctx, d.cell.ID)
	if err == nil && (v.Cell.Lease.Device != d.sync.Device.ID() || v.Cell.Lease.Fence != d.batcher.Fence()) {
		d.becomeViewer(cellsync.Superseded{By: v.Cell.Lease.Device})
	}
	return nil
}

// Gate refuses with the superseded line once the chat is a viewer, and answers
// nil while this device drives it. It is what a viewer's tool calls meet.
func (d *Drive) Gate() error {
	d.mu.Lock()
	viewer := d.viewer
	d.mu.Unlock()
	if viewer.Line != "" {
		return &executor.WorkRefusal{Line: viewer.Line, HolderID: viewer.ID, HolderName: viewer.Name}
	}
	return nil
}

// Idle says the agent stopped to wait for the person: what is sealed and not yet
// uploaded goes up now. A chat that only views has nothing to upload.
func (d *Drive) Idle() {
	if d.batcher != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), cellstore.SealWithin)
			defer cancel()
			if err := cellstore.Await(ctx, d.cell); err != nil {
				d.notice(err.Error())
				return
			}
			d.batcher.Idle()
		}()
	}
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
		pending := cellstore.Await(ctx, d.cell)
		if d.batcher == nil {
			err = pending
			return
		}
		d.cancel()
		<-d.done
		// A failed drain must still stop heartbeats. Its local head remains
		// available for recovery; it is never uploaded as a successful close.
		if pending != nil {
			d.scope.Settle()
			err = pending
			return
		}
		d.syncVault(ctx)
		err = d.batcher.Close(ctx)
		d.scope.Settle() // a publish that failed after the last flush line still counts
	})
	return err
}
