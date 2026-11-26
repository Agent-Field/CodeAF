package syncsetup

// LIVE TRANSFER: A CHAT MOVES BETWEEN YOUR COMPUTERS WITHOUT THE RELAY KEEPING IT.
//
// THE PERSON-FACING NAME IS "BLIND PASS-THROUGH". The identifiers here keep the
// word live (it never shipped under that name, and renaming every one would
// only add churn), but nothing a person reads may say "live transfer": the
// manual, the help text, the refusal sentences and the change entry all say
// blind pass-through, and the switch is CODEAF_PASSTHROUGH.
//
// The hosted relay keeps no content until an account is approved for sync
// storage, so a chat cannot be parked there for another computer to collect.
// Live transfer is the other road. The computer that has the chat keeps the
// chat's already-sealed frames in a store of its own, and the computer that
// takes it asks for them through the relay's blind pipe while both are running
// codeaf (internal/filesdoor). The relay forwards ciphertext and writes nothing.
//
// EVERY PLACE THAT ASKS "IS STORAGE OFF?" ABOUT A CHAT ASKS HERE. Before this
// there were a dozen sites that each decided for themselves what a closed
// account meant for a chat, and a site that was missed would have left one door
// refusing what the rest allowed. They all go through [Sync.ChatStorageOff]
// now. The things that really need relay storage (the vault of saved keys, the
// home state, an idle handoff, the copies a task makes) keep asking
// [Sync.StorageAllowed], because live transfer does not carry them.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/filesdoor"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/livedoor"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/syncwords"
)

// LiveTransferVar turns blind pass-through off when it says off (also 0, false
// and no). Unset, or any other value, it is on: it only ever acts where sync
// storage is closed and another computer is paired, which is exactly where a
// chat could not move at all.
const LiveTransferVar = "CODEAF_PASSTHROUGH"

// liveRecordsCarryTitle is the one choice the owners may flip: whether the
// record a live-only chat leaves in the hosted directory carries the chat's
// sealed title and folder name. It is FALSE because until now a computer with
// storage off wrote no record of a new chat at all, so these two sealed
// strings would be the first thing about a person's chats the hosted relay
// holds, and the terms for that are not finished. The record then holds only
// ids, the holder, times, lease state, frame hints and wrapped keys, the kind
// of thing pairing already stores. Home shows such a chat as untitled until it
// has been moved, when the title comes from the chat itself.
const liveRecordsCarryTitle = false

// recordsWithholdNames is the ONE answer to "may this chat's title or folder go
// to the hosted relay?". It is no while blind pass-through is the road, whatever
// the chat, and it is asked in two places that must agree: where a record is
// built ([Sync.publishInfo]) and at the directory client every record leaves
// through ([deviceDirectory]), so a caller that builds a record some other way
// is still kept from sending a name. Approved storage and self-hosted relays
// answer false and send names as they always have.
func (s *Sync) recordsWithholdNames() bool { return !liveRecordsCarryTitle && s.LiveTransfer() }

// The timings below are variables only so a test can shorten them; the doors
// are built from these, never from a literal.
var (
	liveRetryWithin = filesdoor.DefaultRetryWithin
	// liveNoMachineWithin is how long a take redials a computer the relay says
	// nobody is registered for, before it says that computer is off.
	liveNoMachineWithin = filesdoor.DefaultNoMachineWithin
	liveDoorRosterFor   = filesdoor.DefaultRosterFor
	liveDoorLockPoll    = filesdoor.DefaultLockPoll
	// liveSourceHook, when set, is told of every door a take dials: from is
	// this computer, to the one it asks. It is how a test counts dials and
	// request sizes without reaching into a take that is already running.
	liveSourceHook func(from, to string, source *filesdoor.Source)
	// liveDoorStore, when set, wraps the store the files door serves from, so a
	// test can make a computer go quiet at an exact point of a transfer.
	liveDoorStore func(home string, store blobstore.Store) blobstore.Store
)

// liveTransferEnabled is false only when the person switched blind pass-through
// off. The person-facing name of this mode is blind pass-through; "live" is the
// name the code kept.
func liveTransferEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(env.Get(LiveTransferVar))) {
	case "off", "0", "false", "no":
		return false
	}
	return true
}

// liveTransferActive is the ONE predicate. Live transfer carries a chat when
// all four hold: the relay gates storage, the account's storage is off (or has
// not been answered on a hosted relay), the switch is not off, and the roster
// has another computer to send to or take from. Storage approved, or a relay
// that does not gate storage, answers false, so everything there is exactly
// what it was.
func liveTransferActive(home, relay, identity string, othersPaired bool) bool {
	state, gating, _ := storagePolicy(home, relay, identity)
	return gating && state == "off" && liveTransferEnabled() && othersPaired
}

// liveReadyAt answers [liveTransferActive] from local files alone, the way
// [StorageState] does, so a process that waits to share a chat can notice
// without a request that live transfer would carry it now.
func liveReadyAt(home string) bool {
	relay := Resolve(home)
	if !relay.On() || identity.Solo(home) {
		return false
	}
	id, err := identity.Load(home)
	if err != nil {
		return false
	}
	dev, err := identity.LoadDevice(home)
	if err != nil {
		return false
	}
	_, statErr := os.Stat(othersPath(home, deviceBindingFor(relay.URL, id, dev)))
	return liveTransferActive(home, relay.URL, id.ID(), statErr == nil)
}

// LiveTransferForHome is [Sync.LiveTransfer] for a caller that has only the home,
// answered from local files and without a request.
func LiveTransferForHome(home string) bool { return liveReadyAt(home) }

// liveReady is [liveTransferActive] for the storage waiter, answered from what
// its check has just read. A state of "off" is only ever reported for a relay
// that gates storage, so the identity and the saved answer are not read a second
// time; this computer's own device id never changes, so it is read once; and
// "another computer is paired" is a stat of the file [rememberOthers] keeps,
// which is not a read of anything in the home's top folder.
func (p *storageProcess) liveReady(relay, identityID, state string) bool {
	if state != "off" || identityID == "" || !liveTransferEnabled() {
		return false
	}
	if p.self == "" {
		dev, err := identity.LoadDevice(p.home)
		if err != nil {
			return false
		}
		p.self = dev.ID()
	}
	base := relay
	if b, err := relayBase(relay); err == nil {
		base = b
	}
	_, err := os.Stat(othersPath(p.home, deviceBinding{relay: base, identity: identityID, self: p.self}))
	return err == nil
}

// ChatStorageOffForHome is [Sync.ChatStorageOff] for a caller that has only the
// home, answered from local files and without a request.
func ChatStorageOffForHome(home string) bool {
	return StorageForHome(home) == "off" && !liveReadyAt(home)
}

// LiveTransfer reports whether this computer's chats move live right now.
func (s *Sync) LiveTransfer() bool {
	return liveTransferActive(s.Home, s.Relay, s.Identity.ID(), s.OthersPaired())
}

// ChatStorageOff reports that a chat must stay on this computer for want of
// storage: the account's storage is closed and live transfer is not carrying
// chats instead. It is the question every chat-sync site asks in place of
// "!StorageAllowed()", and it is false whenever a chat can be shared, whether
// through the relay's storage or live.
func (s *Sync) ChatStorageOff() bool {
	return !s.StorageAllowed() && !s.LiveTransfer()
}

// liveTransferRefusal turns "the frames are not there" into a sentence when the
// reason is that this computer has live transfer switched off while the account
// has no storage: the chat's record is listed, and nothing on this road can
// fetch what it names. Every other error passes through untouched.
func (s *Sync) liveTransferRefusal(err error) error {
	if err == nil || !errors.Is(err, blobstore.ErrNotFound) || liveTransferEnabled() {
		return err
	}
	if state, gating, _ := storagePolicy(s.Home, s.Relay, s.Identity.ID()); !gating || state != "off" {
		return err
	}
	return fmt.Errorf("%w: %w", syncwords.ErrLiveTransferOff, err)
}

// liveStoreDir is where a computer keeps the frames it shares live.
func liveStoreDir(home string) string { return filepath.Join(home, "v3", "sync", "live-store") }

// liveDoorLockPath is the file whose lock says which codeaf process of a home
// holds its files door.
func liveDoorLockPath(home string) string {
	return filepath.Join(home, "v3", "sync", "files-door.lock")
}

// liveMarkerPath is a file that exists while this home has frames in its live
// store that the relay may not hold, which is what the backfill pass clears.
func liveMarkerPath(home string) string { return filepath.Join(liveStoreDir(home), "pending-backfill") }

// liveDisks hands every user of a home's live store in one process the same
// Disk. Several processes may open one too: its writes are a temporary file and
// a rename, and a frame's name is the hash of its contents, so two writers of
// the same frame cannot disagree.
var liveDisks sync.Map // directory -> *blobstore.Disk

func (s *Sync) liveDisk() (*blobstore.Disk, error) {
	dir := liveStoreDir(s.Home)
	if d, ok := liveDisks.Load(dir); ok {
		return d.(*blobstore.Disk), nil
	}
	disk, err := blobstore.NewDisk(dir)
	if err != nil {
		return nil, err
	}
	actual, _ := liveDisks.LoadOrStore(dir, disk)
	return actual.(*blobstore.Disk), nil
}

// markLivePending records that this home now holds frames the relay may lack.
func markLivePending(home string) {
	path := liveMarkerPath(home)
	if _, err := os.Stat(path); err == nil {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0o700) == nil {
		_ = os.WriteFile(path, nil, 0o600)
	}
}

func livePending(home string) bool {
	_, err := os.Stat(liveMarkerPath(home))
	return err == nil
}

// liveCarries says this computer's chat sync goes through the live composite and
// its files door: live transfer is carrying chats now, or frames it shared live
// are still waiting for a relay that has since begun to store them.
//
// THE OFF SWITCH WINS OVER THE MARKER. The marker outlives the account's
// storage being closed, so a computer that shared a chat once keeps it for as
// long as the backfill has not finished; reading the marker alone kept the
// composite and the door alive on a computer whose person had said off, and it
// fetched a chat through another computer's door with the switch off. The
// backfill itself only uploads to the relay and needs neither.
func (s *Sync) liveCarries() bool {
	return s.LiveTransfer() || liveTransferEnabled() && livePending(s.Home)
}

// chatScope is scope for the one chat sync every door shares. When live
// transfer carries chats, or frames this computer shared live are still
// waiting to reach a relay that now stores, the store under it is the live
// composite; in every other case it is exactly the plain scope.
func (s *Sync) chatScope(name string) *Scope {
	if !s.liveCarries() {
		return s.scope(name)
	}
	c := &blobstore.Counters{}
	inner, err := s.newLiveStore(name, blobstore.Counting{Inner: s.Store, C: c})
	if err != nil {
		// A store that cannot be opened here leaves chats on the road they were
		// on, which for storage off is that they stay put.
		log.Printf("blind pass-through: opening the live store: %v", err)
		return s.scope(name)
	}
	return s.scopeOver(name, inner, c)
}

// ---- the files door of this computer ----

// filesDoors are the doors this process serves, one per home.
var filesDoors = struct {
	sync.Mutex
	runs map[string]*doorRun
}{runs: map[string]*doorRun{}}

type doorRun struct {
	stop          context.CancelFunc
	done          chan struct{}
	remoteControl bool
}

// ensureFilesDoor starts this home's files door in this process if it is not
// running. Which process of the home actually registers is decided by the lock
// inside the door, so every codeaf process may call this. It is also what ends
// the remote-control part of a door: the person turning remote control off is
// seen at the next call, which the watch loop of [StartLiveTransfer] makes at
// least every [liveWatchEvery] in every process that opens chats, with or
// without live transfer.
func (s *Sync) ensureFilesDoor() {
	wantRC := config.RemoteControlOn()
	if !s.liveCarries() && !s.remoteControlWanted() {
		// THE DOOR HAS NO REASON TO RUN, and the one case that needs ending is a
		// run that is serving remote control the person has just turned off:
		// without this, the early return left it up, and every view already open
		// on it stayed connected until the process quit. A run that was never
		// serving remote control, and a computer that has no other computer yet,
		// are left exactly as they were.
		if !wantRC {
			s.endRemoteControlRun()
		}
		return
	}
	filesDoors.Lock()
	if existing, ok := filesDoors.runs[s.Home]; ok {
		if existing.remoteControl == wantRC {
			filesDoors.Unlock()
			return
		}
		delete(filesDoors.runs, s.Home)
		filesDoors.Unlock()
		if existing.remoteControl && !wantRC {
			// The door goes on for live transfer, so the views end with a reason
			// they can say, and the run restarts below without the live handlers.
			s.endRemoteControlViews()
		}
		existing.stop()
		<-existing.done
		filesDoors.Lock()
		if current, ok := filesDoors.runs[s.Home]; ok && current.remoteControl == wantRC {
			filesDoors.Unlock()
			return
		}
	}
	defer filesDoors.Unlock()
	cfg, err := s.filesDoorConfig()
	if err != nil {
		log.Printf("blind pass-through: opening the live store: %v", err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	// Remote control is carried by whichever of the two live handlers is set: Live
	// alone, or LiveRoad when the direct road is on.
	servesLive := cfg.Live != nil || cfg.LiveRoad != nil
	run := &doorRun{stop: cancel, done: make(chan struct{}), remoteControl: servesLive}
	filesDoors.runs[s.Home] = run
	homeDir := s.Home
	var watchWG sync.WaitGroup
	if servesLive {
		watchWG.Add(1)
		guard.Go("syncsetup.permitsWatch", func() {
			defer watchWG.Done()
			pair.Permits().Watch(ctx, livePermitWatchEvery, func() {
				livedoor.DefaultRegistry(homeDir).PermitsChanged("")
			})
		})
	}
	guard.Go("syncsetup.filesdoor", func() {
		defer func() {
			cancel()
			watchWG.Wait()
			close(run.done)
		}()
		if err := filesdoor.Serve(ctx, cfg); err != nil && ctx.Err() == nil {
			log.Printf("blind pass-through: files door: %v", err)
		}
	})
}

// endRemoteControlViews closes every live view open on this home's door. It
// leans on the door's own permit read, which answers "no watching" while remote
// control is off, so each open tunnel closes the way it does for a permit that
// took watching away, and the viewer's link then finds a refusal and not a door
// that is merely quiet. It holds no lock of ours.
func (s *Sync) endRemoteControlViews() {
	livedoor.DefaultRegistry(s.Home).PermitsChanged("")
}

// endRemoteControlRun ends this home's door when it is running to serve remote
// control and nothing else needs it any more, and waits for it to let go of its
// lock, which is what a restart onto a door without the live handlers does too.
// A door that never served remote control is left alone.
func (s *Sync) endRemoteControlRun() {
	filesDoors.Lock()
	run, ok := filesDoors.runs[s.Home]
	if !ok || !run.remoteControl {
		filesDoors.Unlock()
		return
	}
	delete(filesDoors.runs, s.Home)
	filesDoors.Unlock()
	s.endRemoteControlViews()
	run.stop()
	<-run.done
}

// filesDoorConfig is everything one process needs to serve this home's files
// door from its live store.
func (s *Sync) filesDoorConfig() (filesdoor.ServeConfig, error) {
	disk, err := s.liveDisk()
	if err != nil {
		return filesdoor.ServeConfig{}, err
	}
	var store blobstore.Store = disk
	if liveDoorStore != nil {
		store = liveDoorStore(s.Home, store)
	}
	cfg := filesdoor.ServeConfig{
		Service:   s.Relay,
		Identity:  s.Identity,
		Device:    s.Device,
		Store:     store,
		LockPath:  liveDoorLockPath(s.Home),
		RosterFor: liveDoorRosterFor,
		LockPoll:  liveDoorLockPoll,
		Roster:    s.activeDevices,
		ChatTitle: s.serveChatTitle,
		// The chat's secret files are read from the chat's own folder and never
		// from the live store.
		Secrets:     s.serveSecrets,
		SecretsPage: liveSecretsPage,
	}
	s.attachRemoteControl(&cfg)
	return cfg, nil
}

// stopFilesDoor ends this process's door for one home and waits for it to let
// go of its lock, which is what a process that ends does for every other
// process of the home.
func stopFilesDoor(home string) {
	filesDoors.Lock()
	run := filesDoors.runs[home]
	delete(filesDoors.runs, home)
	filesDoors.Unlock()
	if run != nil {
		run.stop()
		<-run.done
	}
}

// activeDevices answers, for every computer of this identity, whether it is
// still allowed: listed and not removed. The files door asks it for each
// connection and before each response.
func (s *Sync) activeDevices(ctx context.Context) (map[string]bool, error) {
	l, err := s.Dir.List(ctx)
	if err != nil {
		return nil, err
	}
	s.RememberDevices(l.Devices)
	active := make(map[string]bool, len(l.Devices))
	var revoked []string
	for id, d := range l.Devices {
		active[id] = !d.Revoked
		if d.Revoked {
			revoked = append(revoked, id)
		}
	}
	if len(revoked) > 0 {
		livedoor.DefaultRegistry(s.Home).Revoke(revoked...)
	}
	return active, nil
}

// stopFilesDoors ends every door this process serves and waits for it to let
// go of its lock. A test ends its doors this way, and so does a quit.
func stopFilesDoors() {
	filesDoors.Lock()
	runs := filesDoors.runs
	filesDoors.runs = map[string]*doorRun{}
	filesDoors.Unlock()
	for _, run := range runs {
		run.stop()
	}
	for _, run := range runs {
		<-run.done
	}
}

// liveWatchEvery is how often a process that has no reason to run the door
// looks again whether it has one now.
const liveWatchEvery = 30 * time.Second

// liveWatchers are the loops of this process that want to hear that another
// computer became known, which is what turns live transfer on for a home that
// was paired a moment ago.
var liveWatchers = struct {
	sync.Mutex
	next int
	by   map[string]map[int]func()
}{by: map[string]map[int]func(){}}

func watchLive(home string, wake func()) func() {
	liveWatchers.Lock()
	defer liveWatchers.Unlock()
	liveWatchers.next++
	id := liveWatchers.next
	if liveWatchers.by[home] == nil {
		liveWatchers.by[home] = map[int]func(){}
	}
	liveWatchers.by[home][id] = wake
	return func() {
		liveWatchers.Lock()
		defer liveWatchers.Unlock()
		delete(liveWatchers.by[home], id)
	}
}

// notifyLive wakes the loops of home: a roster read has just found another computer.
func notifyLive(home string) {
	liveWatchers.Lock()
	var wakes []func()
	for _, wake := range liveWatchers.by[home] {
		wakes = append(wakes, wake)
	}
	liveWatchers.Unlock()
	for _, wake := range wakes {
		wake()
	}
	// A chat that waits to be shared may now be shared.
	storageProcesses.Lock()
	p := storageProcesses.homes[home]
	storageProcesses.Unlock()
	if p != nil {
		go p.check(false)
	}
}

// StartLiveTransfer keeps this home's files door and its backfill pass in step
// with the account until stop is called: the door runs while live transfer is
// carrying chats (or frames are still waiting for the relay), and the backfill
// runs once storage is approved. It makes no request while neither applies.
// Every process that opens chats calls it at startup, so a computer keeps
// serving the chats it shared after the window that started them has closed.
func StartLiveTransfer(ctx context.Context, home string) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	woke := make(chan struct{}, 1)
	wake := func() {
		select {
		case woke <- struct{}{}:
		default:
		}
	}
	unwatch := watchLive(home, wake)
	unopened := OnStorageOpened(home, wake)
	go func() {
		defer close(done)
		defer unwatch()
		defer unopened()
		timer := time.NewTimer(0)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-woke:
			case <-timer.C:
			}
			timer.Reset(liveWatchEvery)
			s, ok, err := Open(home)
			if err != nil || !ok {
				continue
			}
			s.ensureFilesDoor()
			if s.StorageAllowed() && livePending(home) {
				if err := s.BackfillLive(ctx); err != nil && ctx.Err() == nil {
					log.Printf("blind pass-through: sending shared chats to the relay: %v", err)
				}
			}
		}
	}()
	return func() { cancel(); <-done }
}
