package relayserve

import (
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// Two foreground connections allow a reconnect without letting one client
// exhaust the relay. Waiting requests never hold the fabric mutation lock.
const maxSlotWaitersPerClient = 2
const maxSlotWaiters = 1024

type slotWaiters struct {
	changed chan struct{}
	count   int
}

type slotChanges struct {
	Version uint64   `json:"version"`
	Slots   []string `json:"slots"`
}

// wakeSlots runs under the fabric lock, after the durable PUT succeeds.
func (f *fabric) wakeSlots(key string) {
	if w := f.slotWaiters[key]; w != nil {
		close(w.changed)
		w.changed = make(chan struct{})
	}
}

func (f *fabric) watchSlots(w http.ResponseWriter, r *http.Request) {
	since, err := strconv.ParseUint(r.URL.Query().Get("since"), 10, 64)
	wait, waitErr := strconv.ParseFloat(r.URL.Query().Get("wait"), 64)
	if err != nil || waitErr != nil || math.IsNaN(wait) || wait < 0 || wait > 25 || r.ContentLength != 0 {
		fabricReply(w, nil, badFabric(400, "bad_request"))
		return
	}
	timer := time.NewTimer(time.Duration(wait * float64(time.Second)))
	defer timer.Stop()
	for {
		out, changed, release, err := f.slotWatch(r, since, wait > 0)
		if err != nil || out != nil || changed == nil {
			fabricReply(w, out, err)
			return
		}
		select {
		case <-changed:
			release()
		case <-timer.C:
			release()
			// Revalidate revocation even when the wait ended without a PUT.
			out, _, _, err = f.slotWatch(r, since, false)
			fabricReply(w, out, err)
			return
		case <-r.Context().Done():
			release()
			return
		case <-f.stop:
			release()
			fabricReply(w, nil, badFabric(503, "unavailable"))
			return
		}
	}
}

func (f *fabric) slotWatch(r *http.Request, since uint64, wait bool) (any, <-chan struct{}, func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, err := f.admit(r, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	if err = c.target(true); err != nil {
		return nil, nil, nil, err
	}
	s, err := c.load()
	if err != nil {
		return nil, nil, nil, err
	}
	if s.Version > since {
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
		return nil, nil, nil, nil
	}
	key := c.statePath()
	if f.slotWaiters == nil {
		f.slotWaiters = make(map[string]*slotWaiters)
	}
	entry := f.slotWaiters[key]
	if f.waiting >= maxSlotWaiters || entry != nil && entry.count >= maxSlotWaitersPerClient {
		return nil, nil, nil, badFabric(429, "full")
	}
	if entry == nil {
		entry = &slotWaiters{changed: make(chan struct{})}
		f.slotWaiters[key] = entry
	}
	entry.count++
	f.waiting++
	release := func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		entry.count--
		f.waiting--
		if entry.count == 0 {
			delete(f.slotWaiters, key)
		}
	}
	return nil, entry.changed, release, nil
}
