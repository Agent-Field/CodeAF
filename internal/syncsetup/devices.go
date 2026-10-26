package syncsetup

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
)

// DeviceRequestWithin bounds a person's device request, including rename.
const DeviceRequestWithin = 20 * time.Second

// DeviceSnapshot contains only facts already read by this process. A missing
// Online entry means unknown, including after a presence socket goes away.
type DeviceSnapshot struct {
	Devices  map[string]directory.Device
	Online   map[string]bool
	Version  uint64
	Refused  error
	mutation uint64
}

// A removal's sequence orders it against reads from every client in this
// process, including sync work outside the app's device-operation mutex.
var deviceMutations atomic.Uint64

// DeviceObserver listens locally. It never opens a client, socket or timer.
type DeviceObserver struct {
	home    string
	binding deviceBinding
	state   DeviceSnapshot
	changes chan struct{}
}

type deviceBinding struct {
	relay, identity, self string
}

func deviceBindingFor(relay string, id identity.Identity, dev identity.Dev) deviceBinding {
	if base, err := relayBase(relay); err == nil {
		relay = base
	}
	return deviceBinding{relay: relay, identity: id.ID(), self: dev.ID()}
}

var deviceObservers = struct {
	sync.Mutex
	all map[*DeviceObserver]struct{}
}{all: map[*DeviceObserver]struct{}{}}

func ObserveDevices(home string) *DeviceObserver {
	o := &DeviceObserver{home: home, changes: make(chan struct{}, 1)}
	if id, err := identity.Load(home); err == nil {
		if dev, err := identity.Device(home); err == nil {
			o.binding = deviceBindingFor(Resolve(home).URL, id, dev)
		}
	}
	deviceObservers.Lock()
	deviceObservers.all[o] = struct{}{}
	deviceObservers.Unlock()
	return o
}

func (o *DeviceObserver) Changes() <-chan struct{} { return o.changes }

// Snapshot scopes remembered facts to the pairing files the caller just read.
// Clients of a previous pairing cannot restore its roster or refusal later.
func (o *DeviceObserver) Snapshot(id identity.Identity, dev identity.Dev) DeviceSnapshot {
	binding := deviceBindingFor(Resolve(o.home).URL, id, dev)
	deviceObservers.Lock()
	defer deviceObservers.Unlock()
	if o.binding != binding {
		o.binding, o.state = binding, DeviceSnapshot{}
	}
	s := o.state
	s.Devices, s.Online = maps.Clone(s.Devices), maps.Clone(s.Online)
	return s
}

func (o *DeviceObserver) Close() {
	deviceObservers.Lock()
	defer deviceObservers.Unlock()
	if _, ok := deviceObservers.all[o]; ok {
		delete(deviceObservers.all, o)
		close(o.changes)
	}
}

func rememberDevices(home string, binding deviceBinding, edit func(*DeviceSnapshot)) {
	deviceObservers.Lock()
	defer deviceObservers.Unlock()
	for o := range deviceObservers.all {
		if o.home != home || o.binding != binding {
			continue
		}
		next := o.state
		next.Devices, next.Online = maps.Clone(next.Devices), maps.Clone(next.Online)
		if next.Devices == nil {
			next.Devices = map[string]directory.Device{}
		}
		edit(&next)
		if reflect.DeepEqual(next, o.state) {
			continue
		}
		o.state = next
		select {
		case o.changes <- struct{}{}:
		default:
		}
	}
}

// deviceDirectory keeps facts from requests the engine was making anyway.
// Embedding the HTTP client preserves its holding and conditional-take doors.
type deviceDirectory struct {
	*directory.HTTP
	home    string
	binding deviceBinding
}

func (d *deviceDirectory) List(ctx context.Context) (directory.Listing, error) {
	read := deviceMutations.Load()
	l, err := d.HTTP.List(ctx)
	d.refusal(err)
	if err == nil {
		rememberDevices(d.home, d.binding, func(s *DeviceSnapshot) {
			if read < s.mutation || l.Version != 0 && l.Version < s.Version {
				return
			}
			devices := maps.Clone(l.Devices)
			if devices == nil {
				devices = map[string]directory.Device{}
			}
			// Equal or unversioned reads cannot retract a removal. A read
			// begun before a completed removal cannot replace any facts.
			if l.Version == 0 || l.Version == s.Version {
				for id, old := range s.Devices {
					if old.Revoked {
						rec, ok := devices[id]
						if !ok {
							rec = old
						}
						rec.Revoked = true
						devices[id] = rec
					}
				}
			}
			s.Devices, s.Version = devices, max(s.Version, l.Version)
		})
	}
	return l, err
}

func (d *deviceDirectory) PutDevice(ctx context.Context, id string, rec directory.Device) error {
	err := d.HTTP.PutDevice(ctx, id, rec)
	d.refusal(err)
	if err == nil {
		rememberDevices(d.home, d.binding, func(s *DeviceSnapshot) {
			old := s.Devices[id]
			rec.Revoked, rec.Created, rec.LastSeen = old.Revoked, old.Created, old.LastSeen
			s.Devices[id] = rec
		})
	}
	return err
}

func (d *deviceDirectory) Revoke(ctx context.Context, id string) error {
	err := d.HTTP.Revoke(ctx, id)
	d.refusal(err)
	if err == nil {
		mutation := deviceMutations.Add(1)
		rememberDevices(d.home, d.binding, func(s *DeviceSnapshot) {
			s.mutation = max(s.mutation, mutation)
			rec := s.Devices[id]
			rec.Revoked = true
			s.Devices[id] = rec
			delete(s.Online, id)
		})
	}
	return err
}

func (d *deviceDirectory) Presence(ctx context.Context) (directory.PresenceView, error) {
	p, err := d.HTTP.Presence(ctx)
	d.refusal(err)
	rememberDevices(d.home, d.binding, func(s *DeviceSnapshot) {
		s.Online = nil
		if err != nil {
			return
		}
		s.Online = map[string]bool{}
		for id, seen := range p.Devices {
			s.Online[id] = seen.Online
			// An online answer stamps now, not a past sighting. Keeping
			// that clock out prevents a refresh/event/refresh loop.
			if rec, ok := s.Devices[id]; ok && !seen.Online {
				rec.LastSeen = seen.LastSeen
				s.Devices[id] = rec
			}
		}
	})
	return p, err
}

func (d *deviceDirectory) refusal(err error) {
	var refused error
	switch {
	case errors.Is(err, directory.ErrRevoked):
		refused = dirwatch.ErrRevoked
	case errors.Is(err, directory.ErrRotated):
		refused = dirwatch.ErrRotated
	default:
		return
	}
	rememberDevices(d.home, d.binding, func(s *DeviceSnapshot) {
		s.Refused, s.Online = refused, nil
		if errors.Is(refused, dirwatch.ErrRevoked) {
			rec := s.Devices[d.binding.self]
			rec.Revoked = true
			s.Devices[d.binding.self] = rec
		}
	})
}

func (d *deviceDirectory) Watch(ctx context.Context) (dirwatch.Stream, error) {
	s, err := d.HTTP.Watch(ctx)
	if err != nil {
		d.refusal(err)
		return nil, err
	}
	return &deviceStream{Stream: s, dir: d, presence: true}, nil
}

func (d *deviceDirectory) WatchHolding(ctx context.Context, holds []directory.Hold) (dirwatch.Stream, error) {
	s, err := d.HTTP.WatchHolding(ctx, holds)
	if err != nil {
		d.refusal(err)
		return nil, err
	}
	return &deviceStream{Stream: s, dir: d}, nil
}

type deviceStream struct {
	dirwatch.Stream
	dir      *deviceDirectory
	presence bool
}

func (s *deviceStream) Next(ctx context.Context) (dirwatch.Frame, error) {
	f, err := s.Stream.Next(ctx)
	if f.Pong && err == nil {
		return f, err
	}
	rememberDevices(s.dir.home, s.dir.binding, func(v *DeviceSnapshot) {
		switch {
		case err != nil:
			delete(v.Online, s.dir.binding.self)
			if s.presence {
				v.Online = nil
			}
			if errors.Is(err, dirwatch.ErrRevoked) || errors.Is(err, dirwatch.ErrRotated) {
				v.Refused = err
				if errors.Is(err, dirwatch.ErrRevoked) {
					rec := v.Devices[s.dir.binding.self]
					rec.Revoked = true
					v.Devices[s.dir.binding.self] = rec
				}
			}
		case f.Event != nil:
			e := f.Event
			rec := v.Devices[e.Device]
			switch e.T {
			case dirwatch.EventJoined:
				rec.Name, rec.Platform = e.Name, e.Platform
				v.Devices[e.Device] = rec
			case dirwatch.EventRevoked:
				rec.Revoked = true
				v.Devices[e.Device] = rec
				delete(v.Online, e.Device)
			case dirwatch.EventPresence:
				if e.Online != nil {
					if v.Online == nil {
						v.Online = map[string]bool{}
					}
					v.Online[e.Device] = *e.Online
					if !*e.Online && e.At > 0 {
						rec.LastSeen = e.At
						v.Devices[e.Device] = rec
					}
				}
			}
		default:
			v.Version = max(v.Version, f.Version)
			if v.Online == nil {
				v.Online = map[string]bool{}
			}
			v.Online[s.dir.binding.self] = true
		}
	})
	return f, err
}

// RenameDevice keeps the name here first, then tells the other computers when
// allowed. A failed relay write keeps the CLI's successful local-only receipt.
func RenameDevice(ctx context.Context, home, typed string) (string, error) {
	name, err := devname.Set(home, typed)
	if err != nil {
		return "", err
	}
	if !MayTalk(home) {
		return pair.RenamedAloneLine(name), nil
	}
	s, ok, err := Open(home)
	if err != nil || !ok {
		return pair.RenamedHereLine(name), nil
	}
	ctx, cancel := context.WithTimeout(ctx, DeviceRequestWithin)
	defer cancel()
	if err := s.Rename(ctx, name); err != nil {
		return pair.RenamedHereLine(name), nil
	}
	return pair.RenamedLine(name), nil
}
