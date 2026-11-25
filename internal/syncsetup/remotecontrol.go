package syncsetup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/filesdoor"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/livedoor"
	"github.com/Agent-Field/codeaf/internal/pair"
	relaypkg "github.com/Agent-Field/codeaf/internal/relay"
	"github.com/Agent-Field/codeaf/internal/session"
)

// livePermitWatchEvery is how often the door's permit watcher stats permits.json
// for edits made by another process; zero uses PermitBook.Watch's one-second
// default and tests shrink it with t.Cleanup.
var livePermitWatchEvery time.Duration

// legacySnapshots remembers which (home, identity) pairs have already taken
// their one-time legacy permit snapshot in this process so later roster reads
// never touch the permit file again.
var legacySnapshots sync.Map

// revokedSubscribers holds the per-home callbacks registered by the files door
// so a revocation event arriving on a directory watch stream reaches open live
// views without waiting for the roster cache TTL.
var revokedSubscribers = struct {
	sync.Mutex
	next int
	by   map[string]map[int]func([]string)
}{by: map[string]map[int]func([]string){}}

// remoteControlWanted reports whether this computer should hold its files door
// open for remote control: the person turned remote control on and the cached
// roster shows at least one other non-revoked device on this identity.
func (s *Sync) remoteControlWanted() bool {
	return config.RemoteControlOn() && s.hasOtherActiveDevice()
}

// hasOtherActiveDevice answers from the already-read roster snapshot or the
// persisted others marker whether another non-revoked device belongs to this
// identity, making no relay request on the hot path.
func (s *Sync) hasOtherActiveDevice() bool {
	if s == nil {
		return false
	}
	binding := deviceBindingFor(s.Relay, s.Identity, s.Device)
	deviceObservers.Lock()
	snap, ok := deviceObservers.snapshots[binding]
	deviceObservers.Unlock()
	if ok && snap.Complete {
		for id, dev := range snap.Devices {
			if id != s.Device.ID() && !dev.Revoked {
				return true
			}
		}
		if len(snap.Devices) > 1 {
			return false
		}
	}
	return s.OthersPaired()
}

// snapshotLegacyOnce records the one-time legacy device list in the permit book
// the first time this process reads a full roster for an identity that is not
// solo. Because the permit book's SnapshotLegacy is idempotent once written and
// legacySnapshots guards the call per process and identity, newly paired
// devices that appear in later reads stay ClassNew.
func snapshotLegacyOnce(homeDir string, b deviceBinding, devices map[string]directory.Device) {
	if b.identity == "" || len(devices) < 2 {
		return
	}
	if homeDir != "" && identity.Solo(homeDir) {
		return
	}
	hasOther := false
	roster := make([]string, 0, len(devices))
	for id, dev := range devices {
		if dev.Revoked {
			continue
		}
		if id != b.self {
			hasOther = true
		}
		roster = append(roster, id)
	}
	if !hasOther || len(roster) < 2 {
		return
	}
	slices.Sort(roster)
	key := filepath.Clean(home.Dir()) + "\x00" + b.identity
	if _, loaded := legacySnapshots.LoadOrStore(key, true); loaded {
		return
	}
	_ = pair.Permits().SnapshotLegacy(b.identity, roster)
}

func (s *Sync) budget() *livedoor.Budget {
	s.liveBudgetOnce.Do(func() {
		s.liveBudget = livedoor.OpenBudget(s.Home, livedoor.RealClock(), 0)
	})
	return s.liveBudget
}

func (s *Sync) peerLabel(deviceID string) string {
	if name, ok := s.cachedDeviceName(deviceID); ok && name != "" {
		return name
	}
	return chatlist.DeviceName(nil, deviceID, nil)
}

func (s *Sync) hostLabel() string {
	if name, ok := s.cachedDeviceName(s.Device.ID()); ok && name != "" {
		return name
	}
	return devname.Name(s.Home)
}

func (s *Sync) livePresence(exclude string) []session.SessionPresence {
	rows := session.ReadAllPresence(filepath.Join(s.Home, "v3", "projects"), time.Now(), exclude)
	if len(rows) == 0 && filepath.Clean(s.Home) != filepath.Clean(home.Dir()) {
		rows = session.HomePresence(exclude)
	}
	return rows
}

func (s *Sync) attachRemoteControl(cfg *filesdoor.ServeConfig) {
	if !config.RemoteControlOn() {
		return
	}
	identityID := s.Identity.ID()
	resolver := livedoor.NewResolver(s.Home, livedoor.ResolverOptions{
		Presence: s.livePresence,
	})
	registry := livedoor.DefaultRegistry(s.Home)
	// THE SWITCH IS READ AGAIN FOR EVERY PERMIT, never remembered from when this
	// door started. Remote control turned off while codeaf keeps running must
	// stop handing chats out at once, as the desktop's door does (its lookup
	// asks the setting on every question): a permit with no watch is already
	// what the router answers with a refusal before any chat byte, for a new
	// tunnel, for a new channel on an open one and for a direct offer.
	readPermit := func(device string) pair.Permit {
		if !config.RemoteControlOn() {
			return pair.Permit{}
		}
		p, _ := pair.Permits().For(identityID, device)
		return p
	}
	// serve is the one place a live tunnel becomes a livedoor.Serve call, whichever
	// road carried it. direct is the host half a RELAY tunnel advertises, and is
	// nil for every tunnel that must not offer one.
	serve := func(ctx context.Context, tun io.ReadWriteCloser, device, road string, direct livedoor.DirectHost) error {
		permit, class := pair.Permits().For(identityID, device)
		if !config.RemoteControlOn() {
			permit = pair.Permit{}
		}
		peer := livedoor.Peer{
			Device: device,
			Label:  s.peerLabel(device),
			Permit: permit,
			Class:  class,
		}
		return livedoor.Serve(ctx, tun, peer, livedoor.ServeConfig{
			Resolver: resolver,
			Registry: registry,
			Budget:   s.budget(),
			Permit:   readPermit,
			Host:     s.hostLabel(),
			Road:     road,
			Direct:   direct,
			Clock:    livedoor.RealClock(),
			// A VIEW THAT ENDS BECAUSE THE SWITCH IS OFF SAYS SO. The permit read
			// above answers "no watching" for it, and a viewer told that its permit
			// changed would read a computer that changed what it allows, not one
			// that stopped serving.
			Off: func() bool { return !config.RemoteControlOn() },
		})
	}
	// THE DIRECT ROAD EXISTS ONLY WHEN ALL THREE HOLD: remote control is on (the
	// check above), the person did not pin the road to the relay, and the program
	// installed an implementation. Otherwise nothing here constructs a peer
	// connection, no STUN request can be made, the door advertises no direct road
	// and a direct intent is refused before any Noise work.
	if road, ok := InstalledDirectRoad(); ok && !config.RemoteRoadRelayOnly() {
		host := road.NewHost(config.RemoteSTUN())
		cfg.Direct = func(ctx context.Context, accept filesdoor.StreamAcceptor) {
			host.SetAcceptor(ctx, accept)
			guard.Go("syncsetup.directroad.close", func() {
				<-ctx.Done()
				host.Close()
			})
		}
		cfg.LiveRoad = func(ctx context.Context, tun io.ReadWriteCloser, device, carried string) error {
			if carried == livedoor.RoadDirect {
				return serve(ctx, tun, device, livedoor.RoadDirect, nil)
			}
			return serve(ctx, tun, device, livedoor.RoadRelay, host)
		}
	} else {
		cfg.Live = func(ctx context.Context, tun io.ReadWriteCloser, device string) error {
			return serve(ctx, tun, device, livedoor.RoadRelay, nil)
		}
	}
	cfg.Revoked = func(ctx context.Context, gone func(devices []string)) func() {
		return subscribeRevoked(s.Home, func(ids []string) {
			registry.Revoke(ids...)
			if gone != nil {
				gone(ids)
			}
		})
	}
}

func subscribeRevoked(homeDir string, fn func([]string)) func() {
	key := filepath.Clean(homeDir)
	revokedSubscribers.Lock()
	defer revokedSubscribers.Unlock()
	revokedSubscribers.next++
	id := revokedSubscribers.next
	if revokedSubscribers.by[key] == nil {
		revokedSubscribers.by[key] = map[int]func([]string){}
	}
	revokedSubscribers.by[key][id] = fn
	return func() {
		revokedSubscribers.Lock()
		defer revokedSubscribers.Unlock()
		delete(revokedSubscribers.by[key], id)
	}
}

func notifyRevoked(homeDir string, ids ...string) {
	if len(ids) == 0 {
		return
	}
	livedoor.DefaultRegistry(homeDir).Revoke(ids...)
	key := filepath.Clean(homeDir)
	revokedSubscribers.Lock()
	var subs []func([]string)
	for _, fn := range revokedSubscribers.by[key] {
		subs = append(subs, fn)
	}
	revokedSubscribers.Unlock()
	for _, fn := range subs {
		fn(ids)
	}
}

// LiveLink constructs a viewer-side multiplexed live link to hostDeviceID over
// the files-door relay tunnel, mapping an unregistered host to
// livedoor.ErrHostOffline so the surface transitions straight to WentOffline.
func (s *Sync) LiveLink(hostDeviceID, hostName string, active func() bool, onState func(livedoor.State)) *livedoor.Link {
	return livedoor.NewLink(s.liveLinkConfig(hostDeviceID, hostName, active, onState))
}

// directSignalBuffer is how many host signals a viewer link holds for the
// attempt that is reading them. An attempt exchanges an answer and a handful of
// candidates, so this is generous; a full buffer drops the newest signal rather
// than ever blocking the relay's read loop, which would stall the whole view.
const directSignalBuffer = 64

// liveLinkConfig is LiveLink's configuration, apart from the link built from it,
// so a test can read what the link was given.
func (s *Sync) liveLinkConfig(hostDeviceID, hostName string, active func() bool, onState func(livedoor.State)) livedoor.LinkConfig {
	cfg := livedoor.LinkConfig{
		Home: s.Home,
		Dial: func(ctx context.Context) (io.ReadWriteCloser, error) {
			tun, err := filesdoor.DialLive(ctx, s.Relay, s.Identity, s.Device, hostDeviceID)
			if err != nil {
				if errors.Is(err, relaypkg.ErrNoMachine) {
					return nil, fmt.Errorf("%w: %w", livedoor.ErrHostOffline, err)
				}
				var refused *filesdoor.RefusedError
				if errors.As(err, &refused) && strings.TrimSpace(refused.Reason) == filesdoor.RemovedLine {
					return nil, &livedoor.ReasonError{Reason: livedoor.ReasonRemoved, Host: hostName, Err: err}
				}
				return nil, err
			}
			return tun, nil
		},
		Identity: s.Identity.ID(),
		Host:     hostDeviceID,
		HostName: hostName,
		Active:   active,
		OnState:  onState,
	}
	// THE VIEWER OFFERS THE DIRECT ROAD UNDER THE SAME THREE CONDITIONS THE DOOR
	// ANSWERS IT: remote control is on, the person did not pin the road to the
	// relay, and the program installed an implementation. Otherwise the link has
	// no direct half at all, so it builds no peer connection, can make no STUN
	// request, and never signals anything.
	road, ok := InstalledDirectRoad()
	if !ok || !config.RemoteControlOn() || config.RemoteRoadRelayOnly() {
		return cfg
	}
	deliver := make(chan []byte, directSignalBuffer)
	cfg.OnSignal = func(payload []byte) {
		select {
		case deliver <- payload:
		default:
		}
	}
	cfg.Direct = &livedoor.DirectConfig{
		Dial: func(ctx context.Context, sig livedoor.Signaller) (io.ReadWriteCloser, error) {
			// Whatever the host said to an attempt that has ended is of no use to
			// this one (the road also checks each signal's attempt id), and it
			// must not sit in the way of the answer.
			for drained := false; !drained; {
				select {
				case <-deliver:
				default:
					drained = true
				}
			}
			stream, err := road.Offer(ctx, config.RemoteSTUN(), sig, deliver)
			if err != nil {
				return nil, err
			}
			return filesdoor.DialDirect(ctx, stream, s.Identity, s.Device, hostDeviceID)
		},
	}
	return cfg
}
