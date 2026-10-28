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
	"github.com/Agent-Field/codeaf/internal/session"
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
	// OnRenewalChange wakes local readers after a renewal or ownership change.
	OnRenewalChange func()
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
	// Ownership stays on the record this window opened. Preserving displaced
	// work in a branch does not transfer this window's permission to run tools.
	directoryID   string
	recordedFence uint64

	cancel          context.CancelFunc
	done            chan struct{}
	once            sync.Once
	closeErr        error
	publicationDone func()
	ownership       chan struct{}

	sent string // fingerprint of the vault as last sent; touched by the flush loop and Close only

	spoken  bool
	sealMu  sync.Mutex
	stopped bool
	mu      sync.Mutex
	viewer  Viewer // the superseded sentence and device, empty while this device drives
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
	d := &Drive{sync: s, files: files, cell: c, opt: opt, done: make(chan struct{}), ownership: make(chan struct{}, 1)}
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
	d.directoryID = drv.ID()
	if d.batcher, err = s.batcher(eng, c, drv, d); err != nil {
		return nil, err
	}
	if err := d.recordSendLease(); err != nil {
		return nil, errors.Join(err, s.Dir.Release(ctx, drv.ID(), drv.Fence))
	}
	d.publicationDone = cellstore.RequirePublication(c, func(end int64) bool {
		spoken, known := session.ConversationIn(session.Place{Dir: c.Root}.Transcript(), end)
		return spoken || !known
	})
	d.catchUp(drv)
	if opt.OnDriving != nil {
		opt.OnDriving()
	}
	run, cancel := context.WithCancel(context.WithoutCancel(ctx))
	d.cancel = cancel
	go func() {
		defer close(d.done)
		var work sync.WaitGroup
		work.Go(func() { d.followOwnership(run) })
		_ = d.batcher.Run(run)
		work.Wait()
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
	d.note(*head)
}

// CatchUp notes the newest startup seal once an asynchronous drive is ready.
// An adopted head is already durable; a viewer never publishes anything.
func (d *Drive) CatchUp() {
	if d.batcher == nil {
		return
	}
	if head, err := cellstore.Head(d.cell); err == nil && head != nil {
		d.note(*head)
	}
}

// note publishes only chats that have conversation work. A window's startup
// head still saves its bookkeeping locally without creating an untitled chat
// on every other computer. Once used, the chat keeps its ordinary save path.
func (d *Drive) note(sealed cellstore.Sealed) {
	if d.sync.isRemoved() {
		return
	}
	d.mu.Lock()
	if !d.spoken {
		d.spoken, _ = session.ConversationIn(session.Place{Dir: d.cell.Root}.Transcript(), sealed.Receipt.Transcript.End)
	}
	spoken := d.spoken
	d.mu.Unlock()
	if spoken {
		d.batcher.Note(sealed.Turn)
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
		if err := d.recordSendLease(); err != nil {
			d.becomeViewer(cellsync.Superseded{})
		}
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
	if err := s.reconcileSendLease(c.ID, drv.ID(), view); err != nil {
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
		return drv, nonemptyHolder(view.Cell.Lease.Device), nil
	}
	var localHead string
	wasFree := view.Cell.Lease.Expires <= view.Now
	// An outage can leave our own saved head ahead of the relay. The
	// unchanged, unreleased hold can renew without discarding that work.
	resumeOwn := view.Cell.Lease.Device == s.Device.ID() && view.Cell.Lease.Expires != 0
	if wasFree && !resumeOwn {
		head, err := cellstore.Head(c)
		if err != nil {
			return drv, "", err
		}
		if head != nil {
			localHead = head.Turn.ID
		}
		if localHead != view.Cell.Head {
			return drv, nonemptyHolder(view.Cell.Lease.Device), nil
		}
	}
	var v directory.CellView
	if resumeOwn {
		// Opening an installed copy, including just after a take or a process
		// restart, resumes the same hold. Heartbeat checks its fence atomically
		// and can renew an expired lease that nobody else acquired.
		v, err = s.Dir.Heartbeat(ctx, drv.ID(), directory.Beat{Fence: view.Cell.Lease.Fence, Pending: view.Cell.Lease.Pending})
	} else {
		v, err = s.Dir.Acquire(ctx, drv.ID(), directory.AcquireOpts{RequireConditional: wasFree, Expect: &directory.LeaseExpectation{
			Device: view.Cell.Lease.Device, Fence: view.Cell.Lease.Fence,
		}})
	}
	switch {
	case errors.Is(err, directory.ErrNotFound):
		return drv, "", nil
	case errors.Is(err, directory.ErrLeaseHeld), errors.Is(err, directory.ErrFenceStale):
		return drv, s.holder(ctx, drv.ID()), nil
	case err != nil:
		return drv, "", err
	}
	known, err = cellstore.KnowsHead(c, v.Cell.Head)
	if err != nil || !known || (wasFree && !resumeOwn && localHead != v.Cell.Head) {
		_ = s.Dir.Release(ctx, drv.ID(), v.Cell.Lease.Fence)
		return drv, nonemptyHolder(view.Cell.Lease.Device), err
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
		Driving:         &drv,
		Interval:        s.Interval,
		Info:            d.info,
		OnFlush:         d.flushed(),
		OnSuperseded:    d.becomeViewer,
		OnLost:          func() { d.refusal(directory.ErrFenceStale) },
		OnError:         d.refusal,
		Sleep:           d.opt.Sleep,
		Liveness:        s.Liveness(),
		OnRenewalChange: d.renewalChanged,
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
	if s.Branch == "" && d.directoryID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		v, err := d.sync.Dir.Cell(ctx, d.directoryID)
		cancel()
		if err == nil && v.Cell.Lease.Expires <= v.Now {
			d.available()
			return
		}
		if err == nil && s.By == "" {
			s.By = v.Cell.Lease.Device
		}
	}
	if s.By != "" && s.By == d.sync.Device.ID() {
		d.available()
		return
	}
	d.mu.Lock()
	previous := d.viewer
	if s.By == "" {
		s.By = previous.ID
	}
	changed := d.viewer.Line == ""
	d.viewer = Viewer{ID: s.By, Line: chatlist.Superseded(unknownDevice)}
	// Repeated ownership checks must not replace a known holder with the
	// temporary unnamed refusal while its display name is read again.
	if previous.ID == s.By && previous.Name != "" {
		d.viewer = previous
	}
	expected := sendLease{Cell: d.directoryID, Fence: d.recordedFence}
	d.mu.Unlock()
	if err := d.sync.forgetSendLease(d.cell.ID, expected); err != nil {
		d.notice(err.Error())
	}
	if changed && d.opt.OnRenewalChange != nil {
		d.opt.OnRenewalChange()
	}
	device := unknownDevice
	if s.By != "" && s.By != unknownDevice {
		device = d.sync.deviceName(s.By)
	}
	line := chatlist.Superseded(device)
	viewer := Viewer{ID: s.By, Name: device, Line: line}
	d.mu.Lock()
	d.viewer = viewer
	d.mu.Unlock()
	if d.opt.OnViewer != nil {
		d.opt.OnViewer(viewer)
	}
	// Refuse new work immediately, then preserve the pending save before
	// announcing displacement. A routine ownership check does no fsync (C28).
	if d.batcher != nil {
		ctx, cancel := context.WithTimeout(context.Background(), cellstore.SealWithin)
		defer cancel()
		if err := cellstore.Await(ctx, d.cell); err != nil {
			d.notice(err.Error())
		}
	}
	if s.Branch != "" {
		d.notice(chatlist.KeptEdits(s.Turns, d.opt.DeviceName))
	}
	d.notice(line)
}

// refusal shows the person-facing line of a refused request, which the
// refusal table in cellsync holds beside how the request is retried.
func (d *Drive) refusal(err error) {
	if errors.Is(err, directory.ErrFenceStale) || errors.Is(err, cellsync.ErrSuperseded) {
		d.becomeViewer(cellsync.Superseded{})
		return
	}
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

// RenewalStatus reads this writer's cached renewal health without network work.
func (d *Drive) RenewalStatus() (time.Time, time.Duration, bool) {
	if _, viewer := d.Viewer(); viewer || d.batcher == nil || d.sync.removed.Load() {
		return time.Time{}, 0, false
	}
	return d.batcher.RenewalStatus()
}

// HoldsLease says whether this drive side still drives the chat: it is not a
// viewer, and the directory's lease is this device's at the fence this drive
// side publishes under. A lease taken by another device since (a take by force
// moves it without telling the drive side that was idle) answers false. A
// directory that cannot be asked answers true, because a drive side that cannot
// tell must not be thrown away for it.
func (d *Drive) HoldsLease(ctx context.Context) bool {
	if line, viewer := d.Viewer(); viewer && line != chatlist.Available || d.batcher == nil {
		return false
	}
	v, err := d.sync.Dir.Cell(ctx, d.directoryID)
	expired := err == nil && v.Cell.Lease.Expires <= v.Now
	if err == nil {
		v, err = d.renewIdleLease(ctx, v, d.batcher.Fence())
	}
	if err != nil {
		if errors.Is(err, directory.ErrFenceStale) || errors.Is(err, cellstore.ErrReadOnly) || expired && !cellsync.Unreachable(err) {
			return false
		}
		return true
	}
	lease := v.Cell.Lease
	// The first publish may establish the fence while the read is in flight.
	held := lease.Expires > v.Now && lease.Device == d.sync.Device.ID() && lease.Fence == d.batcher.Fence()
	if held {
		d.resumeViewer()
	}
	return held
}

// CheckLease closes the stale-window gap before a tool runs, even when no
// heartbeat or directory change has reached this process since the move.
func (d *Drive) CheckLease(ctx context.Context) error {
	if err := cellstore.Captured(ctx, d.cell); err != nil {
		return err
	}
	if d.batcher == nil {
		return nil
	}
	id, fence := d.batcher.Lease()
	v, err := d.sync.Dir.Cell(ctx, d.directoryID)
	if currentID, currentFence := d.batcher.Lease(); currentID != id || currentFence != fence {
		return nil
	}
	if err == nil {
		v, err = d.renewIdleLease(ctx, v, fence)
	}
	if err == nil && v.Cell.Lease.Expires <= v.Now {
		d.available()
		return nil
	}
	if err == nil && (v.Cell.Lease.Expires <= v.Now || v.Cell.Lease.Device != d.sync.Device.ID() || v.Cell.Lease.Fence != d.batcher.Fence()) {
		d.becomeViewer(cellsync.Superseded{By: v.Cell.Lease.Device})
	} else if err == nil {
		d.resumeViewer()
	}
	return nil
}

// renewIdleLease covers the gap between a watch reconnect and its ordinary
// heartbeat. Expiry alone never retires an unchanged owner that can still
// renew; an explicit release or a different fence still needs continuation.
func (d *Drive) renewIdleLease(ctx context.Context, v directory.CellView, fence uint64) (directory.CellView, error) {
	lease := v.Cell.Lease
	if lease.Expires != 0 && lease.Expires <= v.Now && lease.Device == d.sync.Device.ID() && lease.Fence == fence {
		// Close stops writers under the same lock before its release. A late
		// ownership read must never resurrect that released fence.
		d.sealMu.Lock()
		d.mu.Lock()
		stopped := d.stopped
		d.mu.Unlock()
		if stopped {
			d.sealMu.Unlock()
			return v, cellstore.ErrReadOnly
		}
		renewed, err := d.sync.Dir.Heartbeat(ctx, d.directoryID, directory.Beat{Fence: fence, Pending: d.batcher.Pending()})
		d.sealMu.Unlock()
		if errors.Is(err, directory.ErrFenceStale) {
			d.becomeViewer(cellsync.Superseded{})
		} else if err != nil && !cellsync.Unreachable(err) {
			d.available()
		}
		if err != nil {
			return v, err
		}
		return renewed, err
	}
	return v, nil
}

func nonemptyHolder(id string) string {
	if id == "" {
		return unknownDevice
	}
	return id
}

// Gate refuses with the superseded line once the chat is a viewer, and answers
// nil while this device drives it. It is what a viewer's tool calls meet.
func (d *Drive) Gate() error {
	d.mu.Lock()
	viewer, stopped := d.viewer, d.stopped
	d.mu.Unlock()
	if stopped {
		return cellstore.ErrReadOnly
	}
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
// viewer leaves the existing history alone.
func (d *Drive) Store(e cellstore.Engine) cellstore.Store {
	return stamping{engine: e, drive: d}
}

// stamping writes the device key and the current fence into each turn it seals.
type stamping struct {
	engine cellstore.Engine
	drive  *Drive
}

var _ cellstore.Store = stamping{}

// Seal implements cellstore.Store.
func (s stamping) Seal(ctx context.Context, c cell.Cell, info cellstore.TurnInfo) (cellstore.Sealed, error) {
	d := s.drive
	d.sealMu.Lock()
	defer d.sealMu.Unlock()
	if d.Gate() != nil {
		return cellstore.Sealed{}, cellstore.ErrReadOnly
	}
	if d.sync.isRemoved() {
		return s.engine.Seal(ctx, c, info)
	}
	if d.batcher == nil {
		return cellstore.Sealed{}, cellstore.ErrReadOnly
	}
	e := s.engine
	e.Identity = cellstore.Identity{Device: d.sync.Device.Cert.Device, Fence: d.batcher.Fence()}
	sealed, err := e.Seal(ctx, c, info)
	if err == nil {
		d.note(sealed)
	}
	return sealed, err
}

// Prepare screens only the current writer's files before its first call.
func (s stamping) Prepare(ctx context.Context, c cell.Cell) error {
	d := s.drive
	d.sealMu.Lock()
	defer d.sealMu.Unlock()
	if d.Gate() != nil || d.batcher == nil && !d.sync.isRemoved() {
		return nil
	}
	return s.engine.Prepare(ctx, c)
}

// Close ends the drive side: what is sealed is published, and the lease is
// given back so another device can take the chat at once. It is safe to call
// on every exit path, and more than once. A failed closing boundary stays
// failed for every caller, so reopening cannot mistake it for a clean release.
func (d *Drive) Close(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, cellstore.SealWithin)
	defer cancel()
	d.once.Do(func() {
		pending := cellstore.Await(ctx, d.cell)
		// A completed capture may race the first wait. Stop new seals under
		// the writer's lock, then keep every seal that crossed before it closed.
		d.sealMu.Lock()
		d.mu.Lock()
		d.stopped = true
		d.mu.Unlock()
		d.sealMu.Unlock()
		if pending == nil {
			pending = cellstore.Await(ctx, d.cell)
		}
		if d.batcher == nil {
			d.closeErr = pending
			return
		}
		stop := context.AfterFunc(ctx, d.cancel)
		if pending == nil && !d.sync.isRemoved() {
			// Join the publish before stopping its request. Canceling a committed
			// request can lose its reply and leave the next publish using an old head.
			d.closeErr = d.batcher.Close(ctx)
		} else {
			d.closeErr = pending
		}
		d.cancel()
		<-d.done
		stop()
		removed := d.sync.isRemoved()
		if removed {
			// Removal keeps this chat local. Only its local durability can fail.
			d.closeErr = pending
		}
		if pending == nil && !removed {
			d.syncVault(ctx)
		}
		d.scope.Settle() // a publish that failed after the last flush line still counts
		if d.closeErr == nil {
			d.mu.Lock()
			expected := sendLease{Cell: d.directoryID, Fence: d.recordedFence}
			d.mu.Unlock()
			d.closeErr = d.sync.forgetSendLease(d.cell.ID, expected)
		}
		if d.closeErr == nil && d.publicationDone != nil {
			d.publicationDone()
		}
	})
	return d.closeErr
}
