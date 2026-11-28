package relayserve

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// Slot limits are the same for every client and implementation.
const (
	MaxSlotBytes   = clientproto.MaxSlotBytes
	MaxClientSlots = clientproto.MaxClientSlots
	FabricMaxAge   = clientproto.MaxAge
)

type fabricSlot struct {
	Sealed  []byte `json:"sealed"`
	Writer  string `json:"writer"`
	At      int64  `json:"at"`
	Version uint64 `json:"version"`
}

// clientState is replaced durably as one file; readers never see half a write.
// Slots and queue share the same lifetime and revocation boundary.
type clientState struct {
	Version    uint64                `json:"version"`
	Slots      map[string]fabricSlot `json:"slots"`
	Queue      []queueEntry          `json:"queue"`
	LastNumber int64                 `json:"last_number"`
}

func (c fabricCall) statePath() string {
	return filepath.Join(c.f.ns.root, "fabric", c.id, c.r.PathValue("device")+".json")
}

func (c fabricCall) load() (clientState, error) {
	s := clientState{Slots: map[string]fabricSlot{}}
	raw, err := os.ReadFile(c.statePath())
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return s, err
	}
	now := c.f.ns.now().UnixMilli()
	for name, slot := range s.Slots {
		if now-slot.At >= FabricMaxAge.Milliseconds() {
			delete(s.Slots, name)
		}
	}
	s.Queue = slices.DeleteFunc(s.Queue, func(e queueEntry) bool { return e.At <= now-FabricMaxAge.Milliseconds() })
	return s, nil
}

func (c fabricCall) save(s clientState) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	path := c.statePath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".client-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, werr := file.Write(raw)
	err = errors.Join(werr, file.Sync(), file.Close())
	if err != nil {
		return err
	}
	if err := fsreplace.Rename(file.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

func (c fabricCall) target(client bool) error {
	callerClient := c.listing.Devices[c.dev].Kind == "client"
	if callerClient != client || (client && c.dev != c.r.PathValue("device")) {
		return wireauth.ErrUnauthorized
	}
	target, ok := c.listing.Devices[c.r.PathValue("device")]
	if !ok || target.Kind != "client" || target.Revoked {
		return badFabric(404, "not_found")
	}
	if c.r.Method != "GET" && c.listing.Identity.Rotation != nil {
		return wireauth.ErrRotated
	}
	return nil
}

func putSlot(c fabricCall) (any, error) {
	if err := c.target(false); err != nil {
		return nil, err
	}
	name := c.r.PathValue("name")
	if !fabricName.MatchString(name) {
		return nil, badFabric(400, "bad_request")
	}
	var in struct {
		Sealed []byte `json:"sealed"`
	}
	if err := decodeFabric(c.body, &in); err != nil {
		return nil, err
	}
	if len(in.Sealed) > MaxSlotBytes {
		return nil, badFabric(413, "too_large")
	}
	if len(in.Sealed) == 0 {
		return nil, badFabric(400, "bad_request")
	}
	s, err := c.load()
	if err != nil {
		return nil, err
	}
	if _, exists := s.Slots[name]; !exists && len(s.Slots) >= MaxClientSlots {
		return nil, badFabric(429, "full")
	}
	s.Version++
	s.Slots[name] = fabricSlot{Sealed: in.Sealed, Writer: c.dev, At: c.f.ns.now().UnixMilli(), Version: s.Version}
	if err := c.save(s); err != nil {
		return nil, err
	}
	if c.f.isWatchingHeader(c.id, c.r.PathValue("device")) && c.header != nil {
		c.header.Set(clientproto.WatchingHeader, "1")
	}
	c.f.wakeSlots(c.statePath())
	return nil, nil
}

func getSlot(c fabricCall) (any, error) {
	if err := c.target(true); err != nil {
		return nil, err
	}
	if len(c.body) != 0 || !fabricName.MatchString(c.r.PathValue("name")) {
		return nil, badFabric(400, "bad_request")
	}
	c.f.touchWatching(c.ns, c.id, c.dev, 0)
	s, err := c.load()
	if err != nil {
		return nil, err
	}
	slot, ok := s.Slots[c.r.PathValue("name")]
	if !ok {
		return nil, badFabric(404, "not_found")
	}
	return slot, nil
}

type clientWatch struct {
	id, dev string
	parked  int
	lastAt  int64
	hasLast bool
	eventOn bool
	offStop func() bool
	gen     uint64
}

func (f *fabric) after(d time.Duration, fn func()) func() bool {
	if f.ns != nil && f.ns.after != nil {
		return f.ns.after(d, fn)
	}
	return time.AfterFunc(d, fn).Stop
}

// isWatchingHeader reports whether the client is watching in the 30-second header sense (contract L4).
// The caller holds f.mu.
func (f *fabric) isWatchingHeader(id, dev string) bool {
	w := f.clientWatches[id+"/"+dev]
	if w == nil {
		return false
	}
	if w.parked > 0 {
		return true
	}
	return w.hasLast && f.ns.now().UnixMilli()-w.lastAt < clientproto.WatchingHeaderFor.Milliseconds()
}

// touchWatching records slot read or wait activity and transitions the client into the watching state.
// The caller holds f.mu.
func (f *fabric) touchWatching(ns *nativeNamespace, id, dev string, deltaParked int) {
	if f.clientWatches == nil {
		f.clientWatches = make(map[string]*clientWatch)
	}
	key := id + "/" + dev
	w := f.clientWatches[key]
	if w == nil {
		w = &clientWatch{id: id, dev: dev}
		f.clientWatches[key] = w
	}
	now := f.ns.now().UnixMilli()
	w.parked += deltaParked
	if w.parked < 0 {
		w.parked = 0
	}
	w.lastAt = now
	w.hasLast = true
	if w.offStop != nil {
		w.offStop()
		w.offStop = nil
	}
	w.gen++
	if w.parked == 0 {
		gen := w.gen
		w.offStop = f.after(clientproto.WatchingEventFor, func() {
			f.expireWatching(key, gen)
		})
	}
	if !w.eventOn {
		w.eventOn = true
		if ns != nil && ns.feed != nil {
			ns.feed.SetWatching(dev, true)
		}
	}
}

func (f *fabric) expireWatching(key string, gen uint64) {
	f.mu.Lock()
	w := f.clientWatches[key]
	if w == nil || w.gen != gen || w.parked > 0 {
		f.mu.Unlock()
		return
	}
	now := f.ns.now().UnixMilli()
	if w.hasLast && now-w.lastAt < clientproto.WatchingEventFor.Milliseconds() {
		rem := time.Duration(w.lastAt+clientproto.WatchingEventFor.Milliseconds()-now) * time.Millisecond
		w.offStop = f.after(rem, func() {
			f.expireWatching(key, gen)
		})
		f.mu.Unlock()
		return
	}
	w.offStop = nil
	w.eventOn = false
	id, dev := w.id, w.dev
	f.mu.Unlock()
	if d := f.ns.group(id); d != nil && d.feed != nil {
		d.feed.SetWatching(dev, false)
	}
}

// clearWatching removes a revoked client's watching state. The caller holds f.mu.
func (f *fabric) clearWatching(ns *nativeNamespace, id, dev string) {
	key := id + "/" + dev
	w := f.clientWatches[key]
	if w == nil {
		return
	}
	if w.offStop != nil {
		w.offStop()
		w.offStop = nil
	}
	wasOn := w.eventOn
	delete(f.clientWatches, key)
	if wasOn && ns != nil && ns.feed != nil {
		ns.feed.SetWatching(dev, false)
	}
}

type slotWaiters struct {
	changed chan struct{}
	count   int
}

type slotChanges struct {
	Version uint64   `json:"version"`
	Slots   []string `json:"slots"`
}

// wakeSlots runs under the fabric lock, after a durable PUT or revocation succeeds.
func (f *fabric) wakeSlots(key string) {
	if w := f.slotWaiters[key]; w != nil {
		close(w.changed)
		w.changed = make(chan struct{})
	}
}

func (f *fabric) watchSlots(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxFabricBody))
	if err != nil {
		fabricReply(w, nil, badFabric(413, "too_large"))
		return
	}
	since, sinceErr := strconv.ParseUint(r.URL.Query().Get("since"), 10, 64)
	wait, waitErr := strconv.ParseFloat(r.URL.Query().Get("wait"), 64)
	bad := sinceErr != nil || waitErr != nil || math.IsNaN(wait) || wait < 0 || wait > clientproto.MaxSlotWait || len(body) != 0
	var timer *time.Timer
	for {
		out, changed, release, err := f.slotWatch(r, body, since, !bad && wait > 0, bad)
		if err != nil || out != nil || changed == nil {
			fabricReply(w, out, err)
			return
		}
		if timer == nil {
			timer = time.NewTimer(time.Duration(wait * float64(time.Second)))
			defer timer.Stop()
		}
		select {
		case <-changed:
			release()
		case <-timer.C:
			release()
			// Revalidate revocation even when the wait ended without a PUT.
			out, _, _, err = f.slotWatch(r, body, since, false, false)
			fabricReply(w, out, err)
			return
		case <-r.Context().Done():
			release()
			return
		case <-f.stop:
			release()
			fabricReply(w, nil, badFabric(503, "unavailable"))
			return
		case <-f.drain:
			release()
			fabricReply(w, nil, badFabric(503, "unavailable"))
			return
		}
	}
}

func (f *fabric) slotWatch(r *http.Request, body []byte, since uint64, wait, bad bool) (any, <-chan struct{}, func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, err := f.admit(r, body)
	if err != nil {
		return nil, nil, nil, err
	}
	if err = c.target(true); err != nil {
		return nil, nil, nil, err
	}
	if bad {
		return nil, nil, nil, badFabric(400, "bad_request")
	}
	s, err := c.load()
	if err != nil {
		return nil, nil, nil, err
	}
	if s.Version > since {
		c.f.touchWatching(c.ns, c.id, c.dev, 0)
		out := slotChanges{Version: s.Version, Slots: []string{}}
		for name, slot := range s.Slots {
			if slot.Version > since {
				out.Slots = append(out.Slots, name)
			}
		}
		sort.Strings(out.Slots)
		return out, nil, nil, nil
	}
	if !wait {
		c.f.touchWatching(c.ns, c.id, c.dev, 0)
		return nil, nil, nil, nil
	}
	key := c.statePath()
	if f.slotWaiters == nil {
		f.slotWaiters = make(map[string]*slotWaiters)
	}
	entry := f.slotWaiters[key]
	if f.waiting >= clientproto.SlotWaitersPerRelay || entry != nil && entry.count >= clientproto.SlotWaitersPerClient {
		return nil, nil, nil, badFabric(429, "full")
	}
	if entry == nil {
		entry = &slotWaiters{changed: make(chan struct{})}
		f.slotWaiters[key] = entry
	}
	entry.count++
	f.waiting++
	c.f.touchWatching(c.ns, c.id, c.dev, 1)
	ns, id, dev := c.ns, c.id, c.dev
	release := func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		entry.count--
		f.waiting--
		if entry.count == 0 {
			delete(f.slotWaiters, key)
		}
		if f.clientWatches[id+"/"+dev] != nil {
			f.touchWatching(ns, id, dev, -1)
		}
	}
	return nil, entry.changed, release, nil
}

func revokeClient(c fabricCall) (any, error) {
	if c.listing.Devices[c.dev].Kind == "client" {
		return nil, wireauth.ErrUnauthorized
	}
	if len(c.body) != 0 {
		return nil, badFabric(400, "bad_request")
	}
	if err := c.ns.commitFabricV1(c.r.Context(), c.dev, c.r, c.body); err != nil {
		return nil, err
	}
	c.f.clearWatching(c.ns, c.id, c.r.PathValue("device"))
	c.f.wakeSlots(c.statePath())
	err := os.Remove(c.statePath())
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	return nil, err
}

// cleanRevoked completes deletion if the process stopped between committing a
// revoke and removing its client file. The sweep already holds the fabric lock.
func (f *fabric) cleanRevoked() error {
	ids, err := f.ns.known()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if f.ns.gone(id) {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(f.ns.root, "fabric", id))
		if errors.Is(err, os.ErrNotExist) || len(entries) == 0 {
			continue
		}
		if err != nil {
			return err
		}
		d, err := f.ns.activate(context.Background(), id, nil)
		if err != nil {
			return err
		}
		l, err := d.fabricListing(context.Background())
		if err != nil {
			return err
		}
		for _, entry := range entries {
			dev, ok := strings.CutSuffix(entry.Name(), ".json")
			if ok && fabricDevice.MatchString(dev) && l.Devices[dev].Revoked {
				if err := os.Remove(filepath.Join(f.ns.root, "fabric", id, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
		}
	}
	return nil
}
