package livedoor

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/home"
)

// Closer is the lifecycle handle a live tunnel registers with a Registry so
// device removal and permit edits reach open views immediately.
type Closer interface {
	CloseWithReason(reason string)
	PermitsChanged()
}

type channelResetter interface {
	resetAllChannels(reason string)
}

type maxTunnelsProvider interface {
	maxTunnelsPerDevice() int
}

// roadProvider is implemented by a Closer that knows which road it arrived on.
// A Closer that does not is a relay tunnel, which is every tunnel that existed
// before the direct road did.
type roadProvider interface {
	roadName() string
}

type regEntry struct {
	id     uint64
	device string
	road   string
	closer Closer
}

func roadOf(c Closer) string {
	if p, ok := c.(roadProvider); ok {
		if road := p.roadName(); road != "" {
			return road
		}
	}
	return RoadRelay
}

// Registry tracks active live-door tunnels by authenticated viewer device ID so
// device revocation, permit changes, per-device tunnel limits, and daily budget
// exhaustion reach every open tunnel without holding a lock across network writes.
type Registry struct {
	mu       sync.Mutex
	nextID   uint64
	byDevice map[string][]*regEntry
}

// NewRegistry returns an empty tunnel registry.
func NewRegistry() *Registry {
	return &Registry{
		byDevice: make(map[string][]*regEntry),
	}
}

// Add registers c under device and returns an idempotent removal callback. When
// device already holds MaxTunnelsPerDevice active tunnels ON THE SAME ROAD, the
// oldest of that road is closed with ReasonHostClosing outside the registry lock
// before Add returns.
//
// THE CAP IS PER ROAD, because a reconnecting relay link briefly overlaps its
// dying predecessor and must be allowed to, and a cap shared with the direct road
// would let that reconnect evict a healthy direct tunnel (or the other way
// round). Each road evicts only its own.
func (r *Registry) Add(device string, c Closer) (remove func()) {
	if r == nil || c == nil {
		return func() {}
	}
	maxTunnels := MaxTunnelsPerDevice
	if p, ok := c.(maxTunnelsProvider); ok {
		if m := p.maxTunnelsPerDevice(); m > 0 {
			maxTunnels = m
		}
	}
	road := roadOf(c)

	var evicted []Closer
	r.mu.Lock()
	r.nextID++
	id := r.nextID
	list := r.byDevice[device]
	for sameRoad(list, road) >= maxTunnels {
		for i, entry := range list {
			if entry.road == road {
				evicted = append(evicted, entry.closer)
				list = append(append([]*regEntry(nil), list[:i]...), list[i+1:]...)
				break
			}
		}
	}
	list = append(list, &regEntry{
		id:     id,
		device: device,
		road:   road,
		closer: c,
	})
	r.byDevice[device] = list
	r.mu.Unlock()

	for _, old := range evicted {
		old.CloseWithReason(ReasonHostClosing)
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			cur := r.byDevice[device]
			for i, entry := range cur {
				if entry.id == id {
					cur = append(cur[:i], cur[i+1:]...)
					break
				}
			}
			if len(cur) == 0 {
				delete(r.byDevice, device)
			} else {
				r.byDevice[device] = cur
			}
			r.mu.Unlock()
		})
	}
}

// sameRoad counts the entries in list that arrived on road.
func sameRoad(list []*regEntry, road string) int {
	n := 0
	for _, entry := range list {
		if entry.road == road {
			n++
		}
	}
	return n
}

// Revoke removes and closes every registered tunnel belonging to any of devices
// with ReasonRemoved.
func (r *Registry) Revoke(devices ...string) {
	if r == nil || len(devices) == 0 {
		return
	}
	var targets []Closer
	r.mu.Lock()
	for _, dev := range devices {
		if dev == "" {
			continue
		}
		for _, entry := range r.byDevice[dev] {
			targets = append(targets, entry.closer)
		}
		delete(r.byDevice, dev)
	}
	r.mu.Unlock()

	for _, c := range targets {
		c.CloseWithReason(ReasonRemoved)
	}
}

// PermitsChanged notifies every registered tunnel for device that its host
// permit may have changed. When device is empty, all registered tunnels across
// all devices are notified.
func (r *Registry) PermitsChanged(device string) {
	if r == nil {
		return
	}
	var targets []Closer
	r.mu.Lock()
	if device == "" {
		for _, list := range r.byDevice {
			for _, entry := range list {
				targets = append(targets, entry.closer)
			}
		}
	} else {
		for _, entry := range r.byDevice[device] {
			targets = append(targets, entry.closer)
		}
	}
	r.mu.Unlock()

	for _, c := range targets {
		c.PermitsChanged()
	}
}

// Count reports how many active tunnels are currently registered for device.
func (r *Registry) Count(device string) int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.byDevice[device])
}

// HasRoad reports whether device has at least one tunnel registered on road. The
// host uses it to excuse a silent relay standby while a direct tunnel of the same
// device is open.
func (r *Registry) HasRoad(device, road string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return sameRoad(r.byDevice[device], road) > 0
}

// countRoad reports how many tunnels are registered on road across every
// device: what the host's own direct-tunnel cap is measured against.
func (r *Registry) countRoad(road string) int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, list := range r.byDevice {
		n += sameRoad(list, road)
	}
	return n
}

// onBudgetExhausted resets the channels of every RELAY tunnel. A direct tunnel
// is not counted by the daily budget and a spent budget never touches it: the
// budget exists for the relay's cost.
func (r *Registry) onBudgetExhausted() {
	if r == nil {
		return
	}
	var targets []channelResetter
	r.mu.Lock()
	for _, list := range r.byDevice {
		for _, entry := range list {
			if entry.road == RoadDirect {
				continue
			}
			if cr, ok := entry.closer.(channelResetter); ok {
				targets = append(targets, cr)
			}
		}
	}
	r.mu.Unlock()

	for _, cr := range targets {
		cr.resetAllChannels(ReasonBudget)
	}
}

var (
	defaultRegistriesMu sync.Mutex
	defaultRegistries   = make(map[string]*Registry)
)

// DefaultRegistry returns the process-wide Registry for homeDir, creating it on
// first use so in-process revocation callers and the live door share one table.
func DefaultRegistry(homeDir string) *Registry {
	homeDir = strings.TrimSpace(homeDir)
	if homeDir == "" {
		homeDir = home.Dir()
	}
	key := filepath.Clean(homeDir)
	defaultRegistriesMu.Lock()
	defer defaultRegistriesMu.Unlock()
	reg, ok := defaultRegistries[key]
	if !ok {
		reg = NewRegistry()
		defaultRegistries[key] = reg
	}
	return reg
}

// NotifyRemoved closes any live tunnel for device under homeDir immediately.
func NotifyRemoved(homeDir, device string) {
	if strings.TrimSpace(device) == "" {
		return
	}
	DefaultRegistry(homeDir).Revoke(device)
}
