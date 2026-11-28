// Package clientrun discovers identity-enrolled phones from the relay, keeps
// per-computer rollback and revocation safety state, publishes signed executor
// cards and authenticated-box snapshots, and executes queued calls.
package clientrun

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/nacl/box"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

// DefaultCardName is used when Config.Name is left empty so every runner
// publishes a valid display name on its card.
const DefaultCardName = "computer"

const (
	// defaultHTTPTimeout bounds individual relay HTTP requests when the caller
	// does not supply a custom client.
	defaultHTTPTimeout = 30 * time.Second
	// refreshEvery paces fallback directory and snapshot polling rounds while a
	// phone is active or no watch socket is available.
	refreshEvery = 5 * time.Second
	// initialBackoff is the first retry delay after a failed round or slot write.
	initialBackoff = 10 * time.Second
	// maxBackoff caps exponential retry delays so a runner recovers within five
	// minutes after a relay or slot error clears.
	maxBackoff = 5 * time.Minute
	// boxKeyBytes is the Curve25519 key length required by NaCl authenticated boxes.
	boxKeyBytes = 32
)

// PausedOlderListReason is the user-facing sentence surfaced when a relay
// serves a directory version lower than the pin for that identity and relay URL
// (design 5.3 and 9.2, contract V3).
const PausedOlderListReason = "phone serving is paused: the device list on this sync address is older than one this computer saw. If that relay was reset, run codeaf phone serve --accept-older-list."

// ErrEmptyRelay reports that Config.Relay was left empty when opening a runner.
var ErrEmptyRelay = errors.New("client runner relay URL is required")

// EmptyRelayError is the typed error returned by Open when Config.Relay is
// empty, preventing the per-relay directory version pin from being disabled.
type EmptyRelayError struct{}

func (e *EmptyRelayError) Error() string {
	return ErrEmptyRelay.Error()
}

func (e *EmptyRelayError) Is(target error) bool {
	return target == ErrEmptyRelay
}

// ErrLocked reports that another runner process already holds the home lock.
var ErrLocked = errors.New("client runner state already open")

// LockedError is the typed error returned when another runner process holds
// runner.json.lock for the same home directory.
type LockedError struct {
	Path string
}

func (e *LockedError) Error() string {
	return "client runner state already open: " + e.Path
}

func (e *LockedError) Is(target error) bool {
	return target == ErrLocked
}

// ErrDirectoryVersionRollback reports that the directory listing version is
// lower than the highest version pinned for the same identity and relay URL.
var ErrDirectoryVersionRollback = errors.New(PausedOlderListReason)

// DirectoryRollbackError is the typed error returned when a directory listing
// rolls back below the pinned version for the same identity and relay URL.
type DirectoryRollbackError struct {
	Identity string
	Relay    string
	Pinned   uint64
	Seen     uint64
}

func (e *DirectoryRollbackError) Error() string {
	return PausedOlderListReason
}

func (e *DirectoryRollbackError) Is(target error) bool {
	return target == ErrDirectoryVersionRollback
}

// Config supplies the engine, directory client, signer, and clock for one
// computer's phone runner. Home holds safety state and durable receipts under
// <home>/clients/ and belongs to one running process at a time.
type Config struct {
	Home      string
	Relay     string
	Name      string
	Identity  ed25519.PublicKey
	DeviceKey ed25519.PrivateKey
	Signer    reqsign.Signer
	Directory directory.Client
	Engine    appwire.Engine
	HTTP      *http.Client
	Now       func() time.Time
	// After supplies the refresh clock; nil uses time.After.
	After func(time.Duration) <-chan time.Time
	// AfterFunc schedules a callback on the runner's clock; nil uses
	// time.AfterFunc or wraps After when After is provided.
	AfterFunc func(time.Duration, func()) func()
	// WatchClock drives the directory watch socket opened by Serve; nil uses
	// the real clock.
	WatchClock dirwatch.Clock
	Log        func(Receipt) error
	Logf       func(string, ...any)
	// LeaseCreated publishes a new chat's real initial head and holds it on
	// this executor before its first turn. Nil is for engines without sync.
	LeaseCreated func(context.Context, appwire.Agent) error
	// LocalChats reports the chats this computer holds locally when it is the
	// only computer of its identity (ok == true). An ok == false result, or a
	// nil func, means the directory's cell leases decide which chats this
	// computer holds. The runner calls LocalChats at the moment of each
	// decision and never caches the answer across rounds, because approving a
	// second computer ends solo mode immediately and the runner must switch to
	// directory arbitration without a restart (contract P13).
	LocalChats func() ([]string, bool)
	// OnPausedChange, when set, is told the new pause reason (empty when serving
	// resumed) each time it changes, after the runner's own lock is released. The
	// host prints the sentence once from it, so a reason is never missed by a read
	// that races the round that sets it.
	OnPausedChange func(reason string)
}

// Runner serializes discovery and publication rounds while keeping derived
// client settings and safety state readable between rounds.
type Runner struct {
	lock    *os.File
	cfg     Config
	run     sync.Mutex
	mu      sync.Mutex
	state   state
	pins    pinsState
	tombs   map[string]Tombstone
	changed chan struct{}
	hashes  map[string]publication
	stream  *Stream

	enrolled       map[string]clientproto.Enrollment
	watching       map[string]watchState
	coalesce       map[string]func()
	slotBackoffs   map[string]slotBackoff
	pausedReason   string
	lastDirVersion uint64
}

type publication struct {
	hash [32]byte
	at   time.Time
}

type watchState struct {
	eventOn     bool
	headerUntil time.Time
}

type slotBackoff struct {
	delay time.Duration
	until time.Time
}

// Open acquires the exclusive process lock under <home>/clients/runner.json.lock
// and loads persisted safety state without contacting the relay.
func Open(cfg Config) (*Runner, error) {
	if strings.TrimRight(strings.TrimSpace(cfg.Relay), "/") == "" {
		return nil, &EmptyRelayError{}
	}
	if cfg.Name != "" && !clientproto.ValidCardName(cfg.Name) {
		return nil, fmt.Errorf("invalid executor card name %q", cfg.Name)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.After == nil && cfg.AfterFunc == nil {
		cfg.AfterFunc = func(d time.Duration, fn func()) func() {
			timer := time.AfterFunc(d, fn)
			return func() { timer.Stop() }
		}
	}
	if cfg.After == nil && cfg.AfterFunc != nil {
		afterFunc := cfg.AfterFunc
		nowFn := cfg.Now
		cfg.After = func(d time.Duration) <-chan time.Time {
			ch := make(chan time.Time, 1)
			afterFunc(d, func() { ch <- nowFn() })
			return ch
		}
	}
	if cfg.After == nil {
		cfg.After = time.After
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: defaultHTTPTimeout}
	}
	r := &Runner{
		cfg:          cfg,
		changed:      make(chan struct{}, 1),
		hashes:       map[string]publication{},
		tombs:        map[string]Tombstone{},
		enrolled:     map[string]clientproto.Enrollment{},
		watching:     map[string]watchState{},
		coalesce:     map[string]func(){},
		slotBackoffs: map[string]slotBackoff{},
	}
	r.stream = NewStream(cfg.Now, r.afterFunc(), r.Changed)
	if err := os.MkdirAll(filepath.Dir(StatePath(cfg.Home)), 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(StatePath(cfg.Home)+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := filelock.Lock(lock, true, true); err != nil {
		lock.Close()
		if filelock.IsBusy(err) {
			return nil, &LockedError{Path: StatePath(cfg.Home)}
		}
		return nil, err
	}
	// The separate lock inode survives atomic replacement of runner.json.
	r.lock = lock
	opened := false
	defer func() {
		if !opened {
			_ = r.Close()
		}
	}()
	r.state.LastAt = map[string]int64{}
	r.state.Paused = map[string]bool{}
	r.state.Outcomes = map[string][]clientproto.Outcome{}
	r.state.Receipts = map[string]Receipt{}
	r.state.Watermarks = map[string]int64{}
	r.state.Creates = map[string][]createdChat{}
	r.pins.Enrollments = map[string]enrollmentPin{}
	r.pins.Directories = map[string]uint64{}
	if err := r.load(); err != nil {
		return nil, err
	}
	if err := r.ensureBoxKey(); err != nil {
		return nil, err
	}
	opened = true
	return r, nil
}

func (r *Runner) afterFunc() func(time.Duration, func()) func() {
	if r.cfg.AfterFunc != nil {
		return r.cfg.AfterFunc
	}
	after := r.cfg.After
	return func(d time.Duration, fn func()) func() {
		ch := after(d)
		stop := make(chan struct{})
		var once sync.Once
		go func() {
			select {
			case <-ch:
				fn()
			case <-stop:
			}
		}()
		return func() { once.Do(func() { close(stop) }) }
	}
}

// Close stops stream coalescing, waits for any round in flight, and releases
// the exclusive process lock.
func (r *Runner) Close() error {
	if r.stream != nil {
		r.stream.Stop()
	}
	r.run.Lock()
	defer r.run.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, cancel := range r.coalesce {
		delete(r.coalesce, key)
		cancel()
	}
	if r.lock == nil {
		return nil
	}
	err := r.lock.Close()
	r.lock = nil
	return err
}

// StatePath names the executor's local key, monotonic timestamp, outcome, and
// receipt state file.
func StatePath(home string) string { return filepath.Join(home, "clients", "runner.json") }

// PinsPath names the executor's enrollment version pin and per-relay directory
// version pin file.
func PinsPath(home string) string { return filepath.Join(home, "clients", "pins.json") }

// TombstonesPath names the executor's permanent revoked-client tombstone file.
func TombstonesPath(home string) string { return filepath.Join(home, "clients", "tombstones.json") }

// Paused returns the user-facing pause sentence when phone serving is paused by
// a directory version rollback (contract V3), or the empty string otherwise.
func (r *Runner) Paused() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pausedReason
}

// AcceptOlderList lowers the directory version pin for the current identity and
// relay URL to the version the relay now serves, keeps every tombstone and
// conflict latch, and discards cached non-latched enrollments so they are
// fetched and checked again (contract V5).
func (r *Runner) AcceptOlderList(ctx context.Context) error {
	r.run.Lock()
	defer r.run.Unlock()
	r.mu.Lock()
	if r.lock == nil {
		r.mu.Unlock()
		return errors.New("client runner is closed")
	}
	r.mu.Unlock()
	if r.cfg.Directory == nil {
		return errors.New("client runner has no directory client")
	}
	listing, err := r.cfg.Directory.List(ctx)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if key := r.dirPinKeyLocked(); key != "" {
		r.pins.Directories[key] = listing.Version
	}
	for id, pin := range r.pins.Enrollments {
		if !pin.Conflict {
			delete(r.pins.Enrollments, id)
		}
	}
	r.enrolled = map[string]clientproto.Enrollment{}
	r.hashes = map[string]publication{}
	r.slotBackoffs = map[string]slotBackoff{}
	r.pausedReason = ""
	r.lastDirVersion = listing.Version
	for id, d := range listing.Devices {
		if d.Revoked && (d.Kind == "client" || r.hasClientStateLocked(id)) {
			if err := r.tombstoneClientLocked(id, listing.Version); err != nil {
				return err
			}
		}
	}
	if err := r.savePinsLocked(); err != nil {
		return err
	}
	r.notifyChanged()
	return nil
}

// Changed requests a coalesced round after a sync publish or engine event. It
// does no work when no active phone is known.
func (r *Runner) Changed() {
	if len(r.active()) == 0 {
		return
	}
	r.notifyChanged()
}

func (r *Runner) notifyChanged() {
	select {
	case r.changed <- struct{}{}:
	default:
	}
}

// Delta feeds a streaming chat text delta into the runner's 250 ms coalescer
// (contract L1).
func (r *Runner) Delta(d appwire.TurnDelta) {
	if r.stream != nil {
		r.stream.Delta(d)
	}
}

// ClearStream discards any buffered streaming partial for agent once its
// durable turn has landed.
func (r *Runner) ClearStream(agent string) {
	if r.stream != nil {
		r.stream.Clear(agent)
	}
}

// Run runs the serving loop; it is an alias for Serve so callers using the
// earlier entry point get the serving watch socket and watching guard.
func (r *Runner) Run(ctx context.Context) {
	r.Serve(ctx)
}

// Serve holds the directory watch socket open with serving=1 and events=1,
// reacts to directory version bumps, watching transitions, and revocations, and
// falls back to the refresh timer when the socket is down (contract L4, L7, L8, P8).
func (r *Runner) Serve(ctx context.Context) {
	var follower dirwatch.Follower
	if sw, ok := r.cfg.Directory.(directory.ServingWatcher); ok {
		follower = dirwatch.OpenClock(sw.WatchServing, r.cfg.WatchClock)
	} else if w, ok := r.cfg.Directory.(directory.Watcher); ok {
		follower = dirwatch.OpenClock(w.Watch, r.cfg.WatchClock)
	}
	if follower != nil {
		defer follower.Close()
	}

	var lastWatchVersion uint64
	var lastWatchedSeq uint64
	var lastRevokedSeq uint64
	var watchWasUp bool

	applyWatch := func() (needStep bool) {
		if follower == nil {
			return false
		}
		st := follower.State()
		for _, rev := range st.Revoked {
			if rev.Seq > lastRevokedSeq {
				lastRevokedSeq = rev.Seq
				_ = r.handleRevoked(rev.Device)
			}
		}
		watchingOn := false
		for _, w := range st.Watched {
			if w.Seq > lastWatchedSeq {
				lastWatchedSeq = w.Seq
				if st.Up {
					r.setWatchingFromEvent(w.Device, w.On)
					if w.On {
						watchingOn = true
					}
				}
			}
		}
		if st.Up {
			for _, dev := range st.Watching {
				if !r.isEventWatching(dev) {
					r.setWatchingFromEvent(dev, true)
					watchingOn = true
				}
			}
		} else if watchWasUp {
			r.clearEventWatching()
		}
		watchWasUp = st.Up
		r.mu.Lock()
		knownVer := r.lastDirVersion
		r.mu.Unlock()
		verChanged := st.Version > 0 && st.Version != knownVer && st.Version != lastWatchVersion
		if st.Version > 0 {
			lastWatchVersion = st.Version
		}
		return watchingOn || verChanged
	}

	delay := refreshEvery
	runStep := true
	for {
		if ctx.Err() != nil {
			return
		}
		var roundErr error
		if runStep {
			roundErr, _ = r.stepRound(ctx)
			if applyWatch() && roundErr == nil {
				roundErr, _ = r.stepRound(ctx)
			}
		}
		runStep = false

		if roundErr != nil {
			delay = min(delay*2, maxBackoff)
			var watchCh <-chan struct{}
			if follower != nil {
				watchCh = follower.Changes()
			}
			timerCh := r.cfg.After(delay)
			// Change notifications cannot defeat network backoff during an outage,
			// while watch-socket revocation events still tombstone immediately.
			for !runStep {
				select {
				case <-ctx.Done():
					return
				case <-watchCh:
					applyWatch()
				case <-timerCh:
					runStep = true
				}
			}
			continue
		}
		delay = refreshEvery

		var watchCh <-chan struct{}
		if follower != nil {
			watchCh = follower.Changes()
		}
		var timerCh <-chan time.Time
		if len(r.active()) > 0 || follower == nil {
			timerCh = r.cfg.After(delay)
		}
		select {
		case <-ctx.Done():
			return
		case <-r.changed:
			runStep = true
		case <-watchCh:
			if applyWatch() {
				runStep = true
			}
		case <-timerCh:
			runStep = true
		}
	}
}

// Step performs one deterministic round: reads the directory (failing closed),
// checks the per-relay directory version pin, tombstones any revoked clients,
// fetches and checks enrollments only when a non-revoked client is present in
// the directory, and publishes changed card, snapshot, and outcome slots.
func (r *Runner) Step(ctx context.Context) error {
	roundErr, slotErr := r.stepRound(ctx)
	return errors.Join(roundErr, slotErr)
}

func (r *Runner) stepRound(ctx context.Context) (roundErr error, slotErr error) {
	r.run.Lock()
	defer r.run.Unlock()
	r.mu.Lock()
	if r.lock == nil {
		r.mu.Unlock()
		return errors.New("client runner is closed"), nil
	}
	r.mu.Unlock()
	if r.cfg.Directory == nil {
		return nil, nil
	}
	listing, err := r.cfg.Directory.List(ctx)
	if err != nil {
		return err, nil
	}
	if err := r.checkDirectoryListing(listing); err != nil {
		return err, nil
	}
	if !r.hasNonRevokedClientInListing(listing) {
		r.mu.Lock()
		r.enrolled = map[string]clientproto.Enrollment{}
		r.mu.Unlock()
		return nil, nil
	}
	rows, err := r.fetchEnrollments(ctx)
	if err != nil {
		return err, nil
	}
	served, err := r.admitEnrollments(listing, rows)
	if err != nil {
		return err, nil
	}
	if len(served) == 0 {
		return nil, nil
	}
	if err := r.ensureBoxKey(); err != nil {
		return err, nil
	}
	var drainErrs []error
	var slotErrs []error
	for _, c := range served {
		if !r.clientStillServed(listing, c.ID) {
			continue
		}
		if err := r.drain(ctx, c, &listing); err != nil {
			drainErrs = append(drainErrs, err)
		}
		if !r.clientStillServed(listing, c.ID) {
			continue
		}
		if err := r.publish(ctx, c, listing); err != nil {
			slotErrs = append(slotErrs, err)
		}
	}
	return errors.Join(drainErrs...), errors.Join(slotErrs...)
}

func (r *Runner) checkDirectoryListing(listing directory.Listing) error {
	r.mu.Lock()
	before := r.pausedReason
	err := r.checkDirectoryListingLocked(listing)
	after := r.pausedReason
	r.mu.Unlock()
	if after != before && r.cfg.OnPausedChange != nil {
		r.cfg.OnPausedChange(after)
	}
	return err
}

func (r *Runner) checkDirectoryListingLocked(listing directory.Listing) error {
	if key := r.dirPinKeyLocked(); key != "" {
		pinned := r.pins.Directories[key]
		if listing.Version < pinned {
			r.pausedReason = PausedOlderListReason
			return &DirectoryRollbackError{
				Identity: reqsign.IDOf(r.identityKeyLocked()),
				Relay:    r.relayKeyLocked(),
				Pinned:   pinned,
				Seen:     listing.Version,
			}
		}
		if listing.Version > pinned {
			r.pins.Directories[key] = listing.Version
			if err := r.savePinsLocked(); err != nil {
				return err
			}
		}
	}
	r.pausedReason = ""
	r.lastDirVersion = listing.Version
	for id, d := range listing.Devices {
		if d.Revoked && (d.Kind == "client" || r.hasClientStateLocked(id)) {
			if err := r.tombstoneClientLocked(id, listing.Version); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Runner) hasNonRevokedClientInListing(listing directory.Listing) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, d := range listing.Devices {
		if d.Kind == "client" && !d.Revoked && !r.isTombstonedLocked(id) {
			return true
		}
	}
	return false
}

func (r *Runner) admitEnrollments(listing directory.Listing, rows []enrolledClientRow) ([]Client, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	identityPub := r.identityKeyLocked()
	derived := map[string]clientproto.Enrollment{}
	// Keep serving from the pinned statement when the relay returns a rolled-back
	// or badly signed row; only a same-ver-different-bytes conflict stops serving.
	for id, pin := range r.pins.Enrollments {
		if pin.Conflict || r.isTombstonedLocked(id) {
			continue
		}
		d, ok := listing.Devices[id]
		if !ok || d.Revoked || d.Kind != "client" {
			continue
		}
		if pin.Enroll.Device == id && pin.Enroll.Verify(identityPub) == nil {
			derived[id] = pin.Enroll
		}
	}
	pinsDirty := false
	for _, row := range rows {
		id := row.Device
		if r.isTombstonedLocked(id) {
			continue
		}
		d, ok := listing.Devices[id]
		if !ok {
			continue
		}
		if d.Revoked {
			delete(derived, id)
			if err := r.tombstoneClientLocked(id, listing.Version); err != nil {
				return nil, err
			}
			continue
		}
		if d.Kind != "client" {
			continue
		}
		if row.Enroll.Device != id || row.Enroll.Verify(identityPub) != nil {
			r.logf("enrollment signature did not check out for %s", id)
			continue
		}
		pin, hasPin := r.pins.Enrollments[id]
		if hasPin && pin.Conflict {
			delete(derived, id)
			continue
		}
		if hasPin && row.Enroll.Ver < pin.Enroll.Ver {
			r.logf("ignoring rolled-back enrollment ver %d < %d for %s", row.Enroll.Ver, pin.Enroll.Ver, id)
			continue
		}
		if hasPin && row.Enroll.Ver == pin.Enroll.Ver {
			if !row.Enroll.Same(pin.Enroll) {
				r.logf("conflicting enrollment at ver %d for %s; refusing client until re-paired", row.Enroll.Ver, id)
				pin.Conflict = true
				r.pins.Enrollments[id] = pin
				pinsDirty = true
				delete(derived, id)
				continue
			}
		} else {
			r.pins.Enrollments[id] = enrollmentPin{Enroll: row.Enroll}
			pinsDirty = true
		}
		derived[id] = row.Enroll
	}
	if pinsDirty {
		if err := r.savePinsLocked(); err != nil {
			return nil, err
		}
	}
	r.enrolled = derived
	return r.activeLocked(), nil
}

func (r *Runner) active() []Client {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.activeLocked()
}

func (r *Runner) activeLocked() []Client {
	ids := slices.Sorted(maps.Keys(r.enrolled))
	out := make([]Client, 0, len(ids))
	for _, id := range ids {
		if r.isTombstonedLocked(id) || r.state.Paused[id] {
			continue
		}
		out = append(out, r.clientOfLocked(id, r.enrolled[id]))
	}
	return out
}

func (r *Runner) clientOfLocked(id string, e clientproto.Enrollment) Client {
	return Client{
		ID:      id,
		SignKey: slices.Clone(e.SignPub),
		BoxKey:  slices.Clone(e.BoxPub),
		Methods: slices.Clone(e.Methods),
		Reads:   slices.Clone(e.Reads),
		Ver:     e.Ver,
		Paused:  r.state.Paused[id],
	}
}

func (r *Runner) ensureBoxKey() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.state.Secret) == boxKeyBytes {
		return nil
	}
	_, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	r.state.Secret = priv[:]
	return r.save()
}

func (r *Runner) executorPublicKey() ([boxKeyBytes]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out [boxKeyBytes]byte
	if len(r.state.Secret) != boxKeyBytes {
		return out, errors.New("executor encryption key is missing")
	}
	pub, err := curve25519.X25519(r.state.Secret, curve25519.Basepoint)
	if err != nil {
		return out, err
	}
	copy(out[:], pub)
	return out, nil
}

func (r *Runner) secretKey() [boxKeyBytes]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out [boxKeyBytes]byte
	copy(out[:], r.state.Secret)
	return out
}

func (r *Runner) identityKeyLocked() ed25519.PublicKey {
	if len(r.cfg.Identity) == ed25519.PublicKeySize {
		return r.cfg.Identity
	}
	if r.cfg.Signer != nil {
		return r.cfg.Signer.IdentityKey()
	}
	return nil
}

func (r *Runner) relayKeyLocked() string {
	return strings.TrimRight(strings.TrimSpace(r.cfg.Relay), "/")
}

func (r *Runner) dirPinKeyLocked() string {
	pub := r.identityKeyLocked()
	relay := r.relayKeyLocked()
	if len(pub) != ed25519.PublicKeySize || relay == "" {
		return ""
	}
	return reqsign.IDOf(pub) + "\n" + relay
}

func (r *Runner) cardName() string {
	if r.cfg.Name != "" {
		return r.cfg.Name
	}
	return DefaultCardName
}

func (r *Runner) logf(format string, args ...any) {
	if r.cfg.Logf != nil {
		r.cfg.Logf(format, args...)
	}
}

// handleRevoked records a permanent tombstone for device on a watch-socket
// revoked event, deletes its local keys, receipts, and outcomes, and stops
// publishing to it (contract V1, V5).
func (r *Runner) handleRevoked(device string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lock == nil {
		return errors.New("client runner is closed")
	}
	return r.tombstoneClientLocked(device, r.lastDirVersion)
}

// setWatching updates a phone's watching state from a watch-socket watching
// event and wakes the runner when watching turns on (contract L4, L7).
func (r *Runner) setWatching(device string, on bool) {
	r.setWatchingFromEvent(device, on)
	if on {
		r.notifyChanged()
	}
}

func (r *Runner) setWatchingFromEvent(device string, on bool) {
	r.mu.Lock()
	if r.isTombstonedLocked(device) {
		r.mu.Unlock()
		return
	}
	if !on {
		delete(r.watching, device)
		r.mu.Unlock()
		return
	}
	w := r.watching[device]
	w.eventOn = true
	r.watching[device] = w
	r.mu.Unlock()
	if r.stream != nil {
		r.stream.Flush()
	}
}

func (r *Runner) isEventWatching(device string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.watching[device].eventOn
}

func (r *Runner) clearEventWatching() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, w := range r.watching {
		w.eventOn = false
		if w.headerUntil.IsZero() {
			delete(r.watching, id)
		} else {
			r.watching[id] = w
		}
	}
}

func (r *Runner) markWatchingFromHeader(device string) (becameWatching bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.isTombstonedLocked(device) {
		return false
	}
	now := r.cfg.Now()
	prev := r.watching[device]
	wasWatching := prev.eventOn || now.Before(prev.headerUntil)
	prev.headerUntil = now.Add(clientproto.WatchingHeaderFor)
	r.watching[device] = prev
	return !wasWatching
}

func (r *Runner) isWatching(device string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.watching[device]
	if !ok {
		return false
	}
	if w.eventOn {
		return true
	}
	if !r.cfg.Now().Before(w.headerUntil) {
		delete(r.watching, device)
		return false
	}
	return true
}

// RecordOutcome appends an execution outcome for client, keeps at most the
// newest MaxOutcomes (32) entries, persists runner.json, and wakes the runner.
func (r *Runner) RecordOutcome(client string, o clientproto.Outcome) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lock == nil {
		return errors.New("client runner is closed")
	}
	if r.isTombstonedLocked(client) {
		return errors.New("client is revoked")
	}
	if !o.Valid() {
		return clientproto.ErrOutcome
	}
	list := append(slices.Clone(r.state.Outcomes[client]), o)
	if len(list) > clientproto.MaxOutcomes {
		list = list[len(list)-clientproto.MaxOutcomes:]
	}
	prev := r.state.Outcomes[client]
	r.state.Outcomes[client] = list
	if err := r.save(); err != nil {
		r.state.Outcomes[client] = prev
		return err
	}
	r.notifyChanged()
	return nil
}

// Outcomes returns a copy of the persisted outcomes for client.
func (r *Runner) Outcomes(client string) []clientproto.Outcome {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := slices.Clone(r.state.Outcomes[client])
	if out == nil {
		return []clientproto.Outcome{}
	}
	return out
}

// Tombstones returns the sorted list of revoked-phone tombstones persisted on
// this computer.
func (r *Runner) Tombstones() []Tombstone {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Tombstone, 0, len(r.tombs))
	for _, t := range r.tombs {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Client < out[j].Client })
	return out
}

// ErrNotReady reports that a live runner holds runner.json.lock on home before
// any executor encryption key has been written to runner.json.
var ErrNotReady = errors.New("this computer is not ready to serve a phone yet")

// ExecutorCard returns this computer's signed executor card for a phone's
// pairing grant without fighting a running runner (Step 3). When the runner
// lock is free, it takes the lock, creates the executor X25519 key in
// <home>/clients/runner.json using the same atomic writer as Open when the file
// has none, and releases the lock; when the lock is held by a live runner, it
// only reads the key from runner.json and writes nothing.
func ExecutorCard(home string, deviceKey ed25519.PrivateKey, cert identity.Cert, name string, now time.Time) (clientproto.Card, error) {
	if !clientproto.ValidCardName(name) {
		return clientproto.Card{}, fmt.Errorf("invalid executor card name %q", name)
	}
	if len(deviceKey) != ed25519.PrivateKeySize {
		return clientproto.Card{}, errors.New("invalid device signing key")
	}
	writer := cert.DeviceID()
	if writer == "" {
		return clientproto.Card{}, errors.New("invalid device certificate")
	}
	secret, err := executorSecret(home)
	if err != nil {
		return clientproto.Card{}, err
	}
	pub, err := curve25519.X25519(secret, curve25519.Basepoint)
	if err != nil {
		return clientproto.Card{}, err
	}
	card := clientproto.Card{
		V:      clientproto.SlotVersion,
		Writer: writer,
		Cert:   cert,
		BoxPub: pub,
		Name:   name,
		At:     now.UnixMilli(),
	}
	return card.Sign(deviceKey), nil
}

func executorSecret(home string) ([]byte, error) {
	path := StatePath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, true); err != nil {
		if filelock.IsBusy(err) {
			secret, ok, readErr := readExecutorSecret(path)
			if readErr != nil {
				return nil, readErr
			}
			if !ok {
				return nil, ErrNotReady
			}
			return secret, nil
		}
		return nil, err
	}
	defer func() { _ = filelock.Unlock(lock) }()
	secret, ok, err := readExecutorSecret(path)
	if err != nil {
		return nil, err
	}
	if ok {
		return secret, nil
	}
	_, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	doc := map[string]json.RawMessage{}
	if raw, err := os.ReadFile(path); err == nil && len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, err
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	secretJSON, err := json.Marshal(priv[:])
	if err != nil {
		return nil, err
	}
	doc["secret"] = secretJSON
	if err := writeAtomic(path, ".runner-*", doc); err != nil {
		return nil, err
	}
	return priv[:], nil
}

func readExecutorSecret(path string) ([]byte, bool, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var loaded struct {
		Secret []byte `json:"secret"`
	}
	if err := json.Unmarshal(raw, &loaded); err != nil {
		return nil, false, err
	}
	if len(loaded.Secret) != boxKeyBytes {
		return nil, false, nil
	}
	return loaded.Secret, true, nil
}
