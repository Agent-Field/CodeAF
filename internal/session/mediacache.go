package session

// mediacache.go keeps the media records a listing or a lookup has just read, so
// the lookup of a source the person is already looking at does not read the
// whole index again.
//
// WHY. The index is global, append-only and unbounded in age; the only way to
// find a record by id without a side index is to scan it, and a screen of two
// hundred cards would ask for two hundred scans. A listing has the records in
// hand a moment before the cards ask for them, so it leaves them here.
//
// THE BOUNDS ARE HARD, with no exception for the newest or the most active:
// at most mediaCacheRecords records AND at most mediaCacheBytes of their index
// lines, least recently used first out. A cached record is a COPY — the
// references slice and the cost are cloned in and out, so no caller can change
// what the next one reads, and the cache holds no chain back to the history it
// came from.
//
// A HIT IS VALIDATED, never trusted. The entry remembers where its line sat in
// the index and a hash of it; a hit opens the index, checks it is the same file
// (device and inode) and long enough, and reads those bytes back to compare.
// A replaced, shortened or rewritten index therefore turns a hit into a miss.
// That costs one small read, not a scan.
//
// THE MISS IS NOT HIDDEN. Finding an uncached record still reads the index, at
// cost proportional to its size; what is bounded is how many of those run at
// once (mediaScanSlots), and a request that finds the slots full is told busy
// at once rather than queued behind a hundred others. Callers asking for the
// same record share one scan.

import (
	"container/list"
	"crypto/sha256"
	"sync"
	"sync/atomic"
	"time"
)

const (
	mediaCacheRecords = 512
	mediaCacheBytes   = 4 << 20
	// mediaScanSlots is how many full-index lookups may run at once.
	mediaScanSlots = 2
)

type mediaCacheKey struct {
	index, session, id string
	dev, ino           uint64
}

type mediaCacheEntry struct {
	key  mediaCacheKey
	row  MediaRow
	sum  [sha256.Size]byte
	cost int
}

var mediaCache = struct {
	mu    sync.Mutex
	items map[mediaCacheKey]*list.Element
	order *list.List // front = most recently used
	bytes int
}{items: map[mediaCacheKey]*list.Element{}, order: list.New()}

// mediaIndexScans counts the full-index lookups that actually ran. A test, or
// a person reading the benchmark, can see that a screen of cards cost none.
var mediaIndexScans atomic.Uint64

// mediaScansActive and mediaScansPeak are how many lookups run now and the most
// that ever ran together, so the slot bound is something a test can read.
var mediaScansActive, mediaScansPeak atomic.Int64

// MediaIndexScans is how many full-index lookups this process has run.
func MediaIndexScans() uint64 { return mediaIndexScans.Load() }

func cloneMediaRow(row MediaRow) MediaRow {
	row.Fact.References = append([]string(nil), row.Fact.References...)
	if row.Fact.TextLayer != nil {
		layer := *row.Fact.TextLayer
		row.Fact.TextLayer = &layer
	}
	if row.Fact.CallCostUSD != nil {
		cost := *row.Fact.CallCostUSD
		row.Fact.CallCostUSD = &cost
	}
	if row.Fact.CostUSD != nil {
		cost := *row.Fact.CostUSD
		row.Fact.CostUSD = &cost
	}
	// The fact is carried in Fact; the artifact's pointer to the same data is
	// not kept, so a clone shares nothing with the row it was made from.
	row.Artifact.Media = nil
	return row
}

// cacheMediaRow remembers a record read from the index at row.Start.
func cacheMediaRow(index string, id mediaCacheKey, row MediaRow, line []byte) {
	cost := len(line) + 256
	if cost > mediaCacheBytes || row.Len != len(line) {
		return
	}
	id.index, id.session = index, row.Artifact.Session
	id.id = row.Fact.ID
	entry := &mediaCacheEntry{key: id, row: cloneMediaRow(row), sum: sha256.Sum256(line), cost: cost}
	mediaCache.mu.Lock()
	defer mediaCache.mu.Unlock()
	if old, ok := mediaCache.items[id]; ok {
		mediaCache.bytes -= old.Value.(*mediaCacheEntry).cost
		mediaCache.order.Remove(old)
	}
	mediaCache.items[id] = mediaCache.order.PushFront(entry)
	mediaCache.bytes += cost
	for mediaCache.order.Len() > mediaCacheRecords || mediaCache.bytes > mediaCacheBytes {
		oldest := mediaCache.order.Back()
		dropped := oldest.Value.(*mediaCacheEntry)
		mediaCache.order.Remove(oldest)
		delete(mediaCache.items, dropped.key)
		mediaCache.bytes -= dropped.cost
	}
}

// dropMediaCacheEntry forgets an entry that failed validation.
func dropMediaCacheEntry(key mediaCacheKey) {
	mediaCache.mu.Lock()
	defer mediaCache.mu.Unlock()
	if element, ok := mediaCache.items[key]; ok {
		mediaCache.bytes -= element.Value.(*mediaCacheEntry).cost
		mediaCache.order.Remove(element)
		delete(mediaCache.items, key)
	}
}

// cachedMediaRow answers a validated copy of the record, or false.
func cachedMediaRow(index, sessionID, id string) (MediaRow, bool) {
	file, now, err := openIndex(index)
	if err != nil {
		return MediaRow{}, false
	}
	defer file.Close()
	key := mediaCacheKey{index: index, session: sessionID, id: id, dev: now.Dev, ino: now.Ino}
	mediaCache.mu.Lock()
	element, ok := mediaCache.items[key]
	if !ok {
		mediaCache.mu.Unlock()
		return MediaRow{}, false
	}
	mediaCache.order.MoveToFront(element)
	entry := *element.Value.(*mediaCacheEntry)
	mediaCache.mu.Unlock()

	row := entry.row
	if row.Start < 0 || row.Start+int64(row.Len) > now.End {
		dropMediaCacheEntry(key)
		return MediaRow{}, false
	}
	line := make([]byte, row.Len)
	if _, err := file.ReadAt(line, row.Start); err != nil || sha256.Sum256(line) != entry.sum {
		dropMediaCacheEntry(key)
		return MediaRow{}, false
	}
	return cloneMediaRow(row), true
}

// A flight is one running lookup that other askers of the same record join.
type mediaFlight struct {
	done chan struct{}
	row  MediaRow
	err  error
}

var mediaFlights = struct {
	mu      sync.Mutex
	running map[mediaCacheKey]*mediaFlight
}{running: map[mediaCacheKey]*mediaFlight{}}

// joinMediaFlight returns the flight for key, and whether the caller must run
// it. A caller that must run it and finds every scan slot taken is refused as
// busy straight away: the slots bound the work, they do not make a queue.
func joinMediaFlight(key mediaCacheKey) (flight *mediaFlight, run bool, err error) {
	mediaFlights.mu.Lock()
	defer mediaFlights.mu.Unlock()
	if flight, ok := mediaFlights.running[key]; ok {
		return flight, false, nil
	}
	if len(mediaFlights.running) >= mediaScanSlots {
		return nil, false, ErrMediaBusy
	}
	flight = &mediaFlight{done: make(chan struct{})}
	mediaFlights.running[key] = flight
	return flight, true, nil
}

func finishMediaFlight(key mediaCacheKey, flight *mediaFlight, row MediaRow, err error) {
	flight.row, flight.err = row, err
	mediaFlights.mu.Lock()
	delete(mediaFlights.running, key)
	mediaFlights.mu.Unlock()
	close(flight.done)
}

func waitMediaFlight(flight *mediaFlight, deadline time.Time) (MediaRow, error) {
	if deadline.IsZero() {
		<-flight.done
	} else {
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		select {
		case <-flight.done:
		case <-timer.C:
			return MediaRow{}, ErrMediaBusy
		}
	}
	if flight.err != nil {
		return MediaRow{}, flight.err
	}
	return cloneMediaRow(flight.row), nil
}
