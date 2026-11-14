package main

import (
	"context"
	"errors"
	"log"
	"sync"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// driveCloseWithin bounds the quit path's last publish and lease release, so a
// silent relay cannot hold the terminal.
const driveCloseWithin = cellstore.SealWithin

// syncDrives is the drive side of every conversation the chat door opened in
// this process. It exists at all only for a chat: a headless run never syncs.
var syncDrives = &driveBook{}

// driveBook keeps one drive side per cell, so a conversation opened again in the
// same process (a resume, a switch back) goes on with the drive it had instead
// of taking a second lease.
type driveBook struct {
	mu     sync.Mutex
	drive  map[string]*liveDrive
	claims map[*liveDrive]int
	// notices is installed by an app assembly before any drive starts. Nil
	// retains the terminal's desk and its marking first-run behaviour.
	notices *appDriveNotices
	// idleCheck includes app work waiting outside the session's own queue.
	idleCheck func(string) bool
}

type appDriveNotices struct {
	firstRun func(*syncsetup.Sync)
	say      func(cell.Cell, string)
	viewer   func(cell.Cell, syncsetup.Viewer)
	driving  func(cell.Cell)
}

// driveOf is the drive side of c, made on first use. It answers nil, and the
// chat runs exactly as it did before sync existed, when sync is off, or when it
// cannot start: a relay setting must never stop a conversation from opening.
// A computer with no identity yet gets a drive side that starts by itself the
// moment one is made (see liveDrive). A nil book answers nil, which is how a
// door that never syncs asks.
func (b *driveBook) driveOf(c cell.Cell, engine cellstore.Engine, report func(error), watches ...*cellstore.SealWatch) *liveDrive {
	d, claim := b.pendingDriveOf(c, engine, report, watches...)
	claim(true)
	return d
}

// pendingDriveOf reserves a drive until one opener owns the journal.
func (b *driveBook) pendingDriveOf(c cell.Cell, engine cellstore.Engine, report func(error), watches ...*cellstore.SealWatch) (*liveDrive, func(bool)) {
	noop := func(bool) {}
	if b == nil {
		return nil, noop
	}
	if !syncsetup.Resolve(home.Dir()).On() || !cell.Available() {
		return nil, noop
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if d := b.drive[c.ID]; d != nil {
		if !d.superseded() {
			if d.notices != nil {
				if syncsetup.StorageForHome(home.Dir()) == "on" {
					d.notices.clearStorageOff()
				}
				d.notices.register(watches...)
			}
			return d, b.claim(d, c.ID)
		}
		// Settle the displaced writer before the replacement reads its branch map
		// or acquires a lease, so late uploads cannot change the new drive's target.
		ctx, cancel := context.WithTimeout(context.Background(), driveCloseWithin)
		err := d.Close(ctx)
		cancel()
		if err != nil {
			report(err)
			// Close stops the writer even when its last save fails. Retain that
			// failure for the final receipt, then give the reopened chat a writer.
			recordFinalSaveFailure(session.SavingError(d.transcript, err))
		}
	}
	// A drive side that no longer holds the chat's lease (another machine took it, or it was moved
	// back here) never drives again: the chat opened here needs a drive side of its own, or every
	// tool call in it would be refused as superseded.
	workAdmissions.Delete(b.drive[c.ID])
	delete(b.drive, c.ID)
	notices := &driveNotices{}
	notices.register(watches...)
	appNotices := b.notices
	sayNotice := notices.say
	if appNotices != nil {
		sayNotice = func(line string) { appNotices.say(c, line) }
	}
	d := newAdmissionDrive(func(changed func()) (*syncsetup.Drive, error) {
		return startLiveDrive(c, engine, report, appNotices, sayNotice, changed)
	}, notices)
	if check := b.idleCheck; check != nil {
		d.surfaceIdle = func() bool { return check(c.ID) }
	}
	if appNotices != nil {
		d.say = sayNotice
	}
	d.transcript = (session.Place{Dir: c.Root}).Transcript()
	d.localGate = &localToolGate{cell: c, say: sayNotice}
	if d != nil {
		if b.drive == nil {
			b.drive = map[string]*liveDrive{}
		}
		b.drive[c.ID] = d
	}
	return d, b.claim(d, c.ID)
}

// claim is made under the book lock; no drive path takes that lock in return.
func (b *driveBook) claim(d *liveDrive, id string) func(bool) {
	if !d.isParked() {
		return func(bool) {}
	}
	if b.claims == nil {
		b.claims = make(map[*liveDrive]int)
	}
	b.claims[d]++
	var once sync.Once
	return func(opened bool) {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.claims[d]--
			if opened {
				d.begin()
			}
			if b.claims[d] <= 0 {
				delete(b.claims, d)
				if d.abandon() && b.drive[id] == d {
					delete(b.drive, id)
					workAdmissions.Delete(d)
				}
			}
		})
	}
}

// startDrive opens the drive side of c. Local failures are reported here;
// liveDrive owns refusal notices and waits across startup attempts.
func startDrive(c cell.Cell, engine cellstore.Engine, report func(error), changed ...func()) (*syncsetup.Drive, error) {
	return startDriveWith(c, engine, report, nil, changed...)
}

func startDriveWith(c cell.Cell, engine cellstore.Engine, report func(error), notices *appDriveNotices, changed ...func()) (*syncsetup.Drive, error) {
	say := surfaceNotices.Say
	refuse := surfaceNotices.SayHomeOnce
	if notices != nil {
		say = func(line string) { notices.say(c, line) }
		refuse = say
	}
	d, err := startLiveDrive(c, engine, report, notices, say, changed...)
	sayRefusalTo(refuse, err)
	return d, err
}

// startLiveDrive keeps every drive sentence on the conversation's own notice road,
// leaving startup refusal announcements to liveDrive so retry waits say each once.
func startLiveDrive(c cell.Cell, engine cellstore.Engine, report func(error), notices *appDriveNotices, say func(string), changed ...func()) (*syncsetup.Drive, error) {
	s, ok, err := syncsetup.Open(home.Dir())
	if errors.Is(err, syncsetup.ErrNoIdentity) {
		// Waiting for an identity is not a seal failing: the live drive starts by itself once one
		// is made, and until then nothing is owed to the relay. With the hosted default every
		// fresh computer is in this state, so reporting it would mark every first launch as
		// failing to seal.
		return nil, err
	}
	if err != nil || !ok {
		if err != nil {
			report(err)
		}
		return nil, err
	}
	if syncsetup.Quiet(home.Dir()) {
		return nil, syncsetup.ErrQuiet // a fleet of one has nothing to sync with yet
	}
	if !s.StorageAllowed() {
		return nil, wireauth.ErrStorageOff
	}
	var viewer func(syncsetup.Viewer)
	var driving func()
	if notices != nil {
		viewer = func(v syncsetup.Viewer) { notices.viewer(c, v) }
		driving = func() { notices.driving(c) }
		notices.firstRun(s)
	} else if line, first := s.FirstRun(); first {
		say(line)
	}
	var onChange func()
	if len(changed) > 0 {
		onChange = changed[0]
	}
	d, err := s.Drive(context.Background(), engine, c, syncsetup.DriveOptions{
		DeviceName:      deviceName(),
		OnRenewalChange: onChange,
		// The title the chat has now, sealed on its way out: the other machine's
		// list names the chat by it.
		Title: func() string { return chatTitle(c) },
		// A line is said by the surface the moment it happens (tui3/notices.go).
		OnNotice:  say,
		OnViewer:  viewer,
		OnDriving: driving,
	})
	if _, refused := cellsync.RefusalOf(err); refused {
		return nil, err
	}
	report(err)
	return d, err
}

// renewalsOf reads the writer already installed for a chat without starting one.
func (b *driveBook) renewalsOf(id string) *liveDrive {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.drive[id]
}

// sayRefusal tells the person what the relay refused when it refused this
// device outright (removed from the fleet, replaced, over its limit) and
// answers true, so the chat runs unsynced with the reason on screen instead
// of opening as if nothing were wrong. Any other failure is not its business.
func sayRefusal(err error) bool { return sayRefusalTo(surfaceNotices.SayHomeOnce, err) }

func sayRefusalTo(say func(string), err error) bool {
	r, ok := cellsync.RefusalOf(err)
	if ok {
		say(r.Say(err))
	}
	return ok
}

// chatTitle is the chat's title as its session record has it now: the name it
// earned, or the person's opening words until it has one.
func chatTitle(c cell.Cell) string {
	m, _ := session.LoadMeta(c.Root)
	return m.Title
}

// closeAll ends every drive side: what is sealed is published and each lease is
// given back, so another machine can take the chat at once. It runs on the
// door's way out, after the conversations closed, so their last seal is in.
func (b *driveBook) closeAll() {
	drives := b.take()
	ctx, cancel := context.WithTimeout(context.Background(), driveCloseWithin)
	defer cancel()
	for _, d := range drives {
		defer func() { recordFinalSaveNotices(d.TakeSavingNotices()) }()
		if err := d.Close(ctx); err != nil {
			log.Print("sync: closing a chat: ", err)
			recordFinalSaveFailure(session.SavingError(d.transcript, err))
		}
	}
}

// take hands over every drive side and forgets them.
func (b *driveBook) take() map[string]*liveDrive {
	b.mu.Lock()
	defer b.mu.Unlock()
	drives := b.drive
	for _, d := range drives {
		d.abandon()
		workAdmissions.Delete(d)
	}
	b.drive = nil
	return drives
}

// driveGate is what a tool call meets before it runs: the superseded line once
// another machine has taken the chat, and nothing while this one drives it.
func driveGate(d *liveDrive) func() error {
	if d == nil {
		return nil
	}
	return d.Gate
}

// driveWorkGate shares a fresh ownership confirmation across the app and
// session doors. Startup and captures never hold admission; an idle drive's
// directory read has its own short budget and degrades to local work on error.
func driveWorkGate(d *liveDrive) func() error {
	if d == nil {
		return nil
	}
	value, ok := workAdmissions.Load(d)
	if !ok {
		value, _ = workAdmissions.LoadOrStore(d, &workAdmission{})
	}
	check := value.(*workAdmission)
	return func() error {
		if started := d.started(); started != nil {
			return check.admit(started)
		}
		return nil
	}
}

func driveSettingsGate(d *liveDrive) func() error {
	if d == nil {
		return nil
	}
	return func() error {
		if started := d.published.Load(); started != nil {
			ctx, cancel := context.WithTimeout(context.Background(), executor.TakeBackWithin)
			defer cancel()
			return started.ConfirmSettingsWork(ctx)
		}
		return nil
	}
}

func drivePreparedGate(d *liveDrive) func() error {
	if d == nil {
		return nil
	}
	return func() error {
		if started := d.started(); started != nil {
			within := driveWorkWithin
			if started.IdleReleased() {
				within = executor.TakeBackWithin
			}
			ctx, cancel := context.WithTimeout(context.Background(), within)
			defer cancel()
			return started.ConfirmPreparedWork(ctx)
		}
		return nil
	}
}

// driveStore is how the seat's seals reach the drive side; nil seals on the
// engine itself.
func driveStore(d *liveDrive) func(cellstore.Engine) cellstore.Store {
	if d == nil {
		return nil
	}
	return d.Store
}

// closeCell is a conversation's final release after its agent has closed and
// its last seal is durable, even when the local engine process stays alive.
func (b *driveBook) closeCell(id string) error {
	d := b.takeCell(id)
	if d == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), driveCloseWithin)
	defer cancel()
	return session.SavingError(d.transcript, d.Close(ctx))
}

// takeCell forgets one drive under the book lock before any closing I/O begins.
func (b *driveBook) takeCell(id string) *liveDrive {
	b.mu.Lock()
	defer b.mu.Unlock()
	d := b.drive[id]
	if d != nil {
		d.abandon()
	}
	workAdmissions.Delete(d)
	delete(b.drive, id)
	return d
}

// chatSyncService keeps sync presentation quiet until this computer has paired.
// A remote chat can still supply its own engine's service explicitly.
func chatSyncService() string {
	if syncsetup.Quiet(home.Dir()) {
		return ""
	}
	return syncsetup.Resolve(home.Dir()).URL
}

// chatSyncStats uses the sync client's signed store wire without starting a driver.
// A quiet or disabled identity must not make a request just to open /status.
func chatSyncStats(ctx context.Context) (blobstore.Counts, error) {
	if chatSyncService() == "" {
		return blobstore.Counts{}, nil
	}
	s, err := syncOf()
	if err != nil {
		return blobstore.Counts{}, err
	}
	if !syncsetup.StorageNeedsRefresh(s.Home, s.Relay, s.Identity.ID()) {
		return syncsetup.StorageCounts(s.Home, s.Relay, s.Identity.ID()), nil
	}
	before := s.StorageAllowed()
	counts, err := s.RefreshStorage(ctx)
	if err == nil && !before && s.StorageAllowed() {
		syncDrives.storageOpened()
	}
	if syncsetup.StorageGated(s.Home, s.Relay, s.Identity.ID()) && counts.Storage != "on" {
		counts.Storage = "off"
	}
	return counts, err
}

// storageOpened wakes both running batchers and local startup waits for open chats.
func (b *driveBook) storageOpened() {
	if b == nil {
		return
	}
	for _, d := range b.openDrives() {
		d.storageOpened()
	}
}

// openDrives copies the open drives under the lock, so waking them happens
// outside it.
func (b *driveBook) openDrives() []*liveDrive {
	b.mu.Lock()
	defer b.mu.Unlock()
	drives := make([]*liveDrive, 0, len(b.drive))
	for _, d := range b.drive {
		drives = append(drives, d)
	}
	return drives
}

// driveNotices fans out to each seat sharing a writer, including an idle engine seat.
// Only a current startup refusal is replayed while no drive is running.
type driveNotices struct {
	mu      sync.Mutex
	last    string
	watches map[*cellstore.SealWatch]bool
	closing bool
	final   []string
}

func (n *driveNotices) register(watches ...*cellstore.SealWatch) {
	for _, watch := range watches {
		if watch != nil {
			watch.OnRetire = n.add(watch)
		}
	}
}

func (n *driveNotices) add(watches ...*cellstore.SealWatch) func() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.watches == nil {
		n.watches = make(map[*cellstore.SealWatch]bool)
	}
	for _, watch := range watches {
		if watch == nil || n.watches[watch] {
			continue
		}
		n.watches[watch] = true
		if n.last != "" {
			watch.Note(n.last)
		}
	}
	return func() {
		n.mu.Lock()
		defer n.mu.Unlock()
		for _, watch := range watches {
			delete(n.watches, watch)
		}
	}
}

// refuse caches only startup conditions, never first-run or running-drive announcements.
func (n *driveNotices) refuse(line string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.last = line
	for watch := range n.watches {
		watch.Note(line)
	}
}

func (n *driveNotices) clear() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.last = ""
}

func (n *driveNotices) say(line string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	// The last watch retires during agent close, before the drive's Close.
	// Keep news from that gap for the closing receipt instead of dropping it.
	if n.closing || n.watches != nil && len(n.watches) == 0 {
		n.final = append(n.final, line)
		return
	}
	for watch := range n.watches {
		watch.Note(line)
	}
}

// clearStorageOff keeps a later seat from replaying a refusal that approval ended.
func (n *driveNotices) clearStorageOff() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.last == chatlist.RelayStorageOff {
		n.last = ""
	}
}
