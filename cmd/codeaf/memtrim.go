package main

import (
	"context"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
)

// heapTrim* decide when a surface hands idle heap back to the system.
//
// The surface runs with the collector waiting for five times the live heap
// (tuneForTheSurface), which is right for a busy turn and wrong for the minutes
// after it: a burst of reads fills the heap with garbage, the collector runs
// once, and the runtime then returns the freed pages only a little at a time.
// The process sits at several times its live heap for as long as nothing
// allocates. The trimmer closes that gap with one forced collection and a full
// return, and only when what is held and not live is a large share.
const (
	// heapTrimFloor is the smallest excess worth a collection.
	heapTrimFloor = 48 << 20
	// heapTrimEvery is how often the heap is looked at, and heapTrimCooldown
	// the least time between two trims, so a heap that fills again at once is
	// not collected over and over.
	heapTrimEvery    = 5 * time.Second
	heapTrimCooldown = time.Minute

	// heapHeadroom is the garbage the collector may let build up, and
	// heapGCMin and heapGCMax bound the percent it is turned into. The
	// maximum is the surface's own generous setting, kept for a heap small
	// enough that a multiple of it is little memory.
	heapHeadroom = 32 << 20
	heapGCMin    = 10
	heapGCMax    = 400
)

// heapGCPercent is the collector percent that lets a heap of live bytes build
// about heapHeadroom of garbage before it is collected.
func heapGCPercent(live uint64) int {
	if live == 0 {
		return heapGCMax
	}
	return int(min(max(uint64(heapHeadroom)*100/live, heapGCMin), heapGCMax))
}

// heapReading is what the runtime says about its heap: Live is what the last
// collection found reachable, Held is the memory the runtime has mapped for
// itself and not returned.
type heapReading struct {
	Live, Held uint64
}

// excess is what is held beyond what is live.
func (r heapReading) excess() uint64 {
	if r.Held <= r.Live {
		return 0
	}
	return r.Held - r.Live
}

// worthTrimming is true when the excess is over the floor and over half of the
// live heap, so a large heap is not collected for a share it needs.
func (r heapReading) worthTrimming() bool {
	excess := r.excess()
	return excess > heapTrimFloor && excess > r.Live/2
}

type heapTrimmer struct {
	read func() heapReading
	free func()
	now  func() time.Time
	last time.Time
	// percent, when set, is given the collector percent the live heap calls
	// for each time it moves a fifth from the last one given.
	percent func(int)
	mu      sync.Mutex
	given   int
}

// tune gives the collector the percent the live heap calls for.
func (t *heapTrimmer) tune(reading heapReading) {
	if t.percent == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	want := heapGCPercent(reading.Live)
	if t.given == 0 || want*5 < t.given*4 || want*4 > t.given*5 {
		t.given = want
		t.percent(want)
	}
}

// step is one look at the heap.
func (t *heapTrimmer) step() {
	reading := t.read()
	t.tune(reading)
	now := t.now()
	if !t.last.IsZero() && now.Sub(t.last) < heapTrimCooldown {
		return
	}
	if !reading.worthTrimming() {
		return
	}
	t.last = now
	t.free()
}

// readHeap is the runtime's own account, read without stopping the world.
func readHeap() heapReading {
	samples := []metrics.Sample{
		{Name: "/gc/heap/live:bytes"},
		{Name: "/memory/classes/total:bytes"},
		{Name: "/memory/classes/heap/released:bytes"},
	}
	metrics.Read(samples)
	for _, sample := range samples {
		if sample.Value.Kind() != metrics.KindUint64 {
			return heapReading{}
		}
	}
	live, total, released := samples[0].Value.Uint64(), samples[1].Value.Uint64(), samples[2].Value.Uint64()
	if total < released {
		return heapReading{}
	}
	return heapReading{Live: live, Held: total - released}
}

// startHeapTrimmer looks at the heap until ctx ends.
func startHeapTrimmer(ctx context.Context) {
	t := &heapTrimmer{read: readHeap, free: debug.FreeOSMemory, now: time.Now}
	// An explicit GOGC decides, as it does in tuneForTheSurface.
	if os.Getenv("GOGC") == "" {
		t.percent = func(p int) { debug.SetGCPercent(p) }
	}
	if t.percent != nil {
		watchCollections(func() { t.tune(t.read()) })
	}
	guard.Go("heap/trim", func() {
		t.step()
		ticker := time.NewTicker(heapTrimEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				t.step()
			}
		}
	})
}

// watchCollections calls after once for every completed collection, so the
// percent follows the heap through a burst instead of lagging a tick behind it.
// A finalizer on an unreachable sentinel runs after the cycle that found it
// unreachable; it plants the next sentinel before it returns.
func watchCollections(after func()) {
	type sentinel struct{ _ [16]byte }
	var plant func()
	plant = func() {
		runtime.SetFinalizer(&sentinel{}, func(*sentinel) {
			plant()
			after()
		})
	}
	plant()
}
