package session

// A RECORD READ TWICE UNCHANGED IS PARSED ONCE, AND NO READING IS UNBOUNDED.
//
// A page asking for the older half of a long conversation used to pay for the
// whole journal every time: [ReadTranscript] has to walk the file from its
// first line, because a compaction places everything above it, an entry's id
// counts the person's messages from the first, and an update is known only by a
// call that comes after it. None of that can be read off a tail. So the cache
// below does not shorten the reading; it keeps the finished [Record] of a file
// that has not changed since, and answers the next page from it.
//
// THE FIRST OPEN IS STILL THE WHOLE FILE. Nothing here knows how to resume a
// parse from a checkpoint, and nothing pretends to: a conversation opened for
// the first time, or after any write to its journal, is read whole by the one
// parser this package has.
//
// ── EVERY READING PASSES ONE GATE ──────────────────────────────────────────
//
// A reading is a call to [RecordCache.WithRecord], and the record it hands its
// callback lives exactly as long as the callback. Every reading — one answered
// from a kept record, one the cache parses to keep, one too large to keep —
// first takes one of [RecordCacheLimits.Operations] slots and a reservation of
// bytes, and holds both until the callback returns. A reading that finds every
// slot taken, or the budget spoken for by readings in progress, does not wait
// and does not read anyway: it returns [ErrRecordReadBusy], and the caller says
// so. Two readings of the same unchanged file share one parse: the second waits
// for the first, and is then answered from what was kept. Only one reader waits
// on any parse; the slots bound the rest.
//
// THE BUDGET IS AN ACCOUNT, NOT A CEILING ON THE PROCESS. It counts the records
// kept, the copy each callback is handed, and an estimate for each parse in
// progress, which is replaced by the record's measured size once it is read.
// What it cannot count is the parser's own working memory — the messages and
// indexes a journal is rebuilt through, several times the file at its peak and
// gone when the parse ends. That is bounded only by the slots: at most
// [RecordCacheLimits.Operations] journals are parsed at once. A journal whose
// estimate exceeds the whole budget is still read, whole and correct, but only
// when nothing else holds a reservation, and is never kept; its record can be
// larger than the budget for as long as its callback runs. Nothing is cut short
// to fit: a record is whole or it is not read.
//
// ── WHAT MAKES A KEPT RECORD TRUE ──────────────────────────────────────────
//
// A record is answered from the cache only when the file's stamp — device,
// inode, size, modification time and change time, all to the nanosecond — is
// the stamp the record was read under ([recordStamp]). The change time is the
// one a writer cannot put back: rewriting a journal in place to the same size
// and restoring its modification time still moves it. Where the platform does
// not hand over such a time ([recordStampOf]), nothing is kept at all, and every
// reading is a parse that still passes the gate.
//
// A STAMP IS ONLY AS FINE AS THE CLOCK THAT WROTE IT. A filesystem stamps change
// times off a coarse tick, so two writes inside one tick can leave one stamp. A
// file is therefore kept only once its change time is at least
// [RecordCacheLimits.Settle] old: any write after that lands on a later tick and
// a different stamp. This is the racy-clean rule git applies to its index.
//
// THE FILE MUST STAND STILL WHILE IT IS READ. It is stamped again through the
// open handle before and after the parse, and a reading across a write is
// handed to its callback, exactly as the plain read would have been, and kept
// by nobody.
//
// A READING THAT SAID SOMETHING WAS WRONG IS NEVER KEPT. A record with
// [Record.Unreadable] set, a file that could not be opened, and anything that is
// not a regular file are answered afresh every time, so a fault is reported by
// the reading that met it and never by a memory of one. A path that is not a
// regular file is never opened as one: a named pipe would block the reader on a
// writer that may never come.
//
// ── WHO OWNS WHAT ──────────────────────────────────────────────────────────
//
// A kept record is the cache's and is never handed out. A callback is given its
// own copy ([cloneRecord]) — the entry slices and every slice or pointer inside
// an entry — so a callback that edits what it was given cannot reach another
// reader's page. Strings are shared, because Go does not let anybody write one;
// a kept record evicted while a copy of it is out stays counted until the last
// such callback returns, because the copy still holds its strings.

import (
	"container/list"
	"errors"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/filememo"
)

// ErrRecordReadBusy is a reading refused because as many as may run at once
// already are. Nothing was read; asking again later is the remedy.
var ErrRecordReadBusy = errors.New("session: too many transcript readings are in progress")

// recordStamp is everything about a file a kept record was read under.
type recordStamp struct {
	dev, ino     uint64
	size         int64
	mtime, ctime int64
}

// RecordCacheLimits bound a [RecordCache]. A zero field takes its default.
type RecordCacheLimits struct {
	// Operations is the most readings in progress at once, from the moment
	// one is asked for until its callback returns.
	Operations int
	// Bytes is the budget the kept records, the copies handed out and the
	// parses in progress are counted against.
	Bytes int64
	// Entries is the most records kept.
	Entries int
	// FileBytes is the largest journal read for keeping.
	FileBytes int64
	// Settle is how old a file's change time must be before its reading is
	// kept.
	Settle time.Duration
	// Idle is how long a kept record may go unread before it is dropped.
	Idle time.Duration
	// Now is the clock the change time is measured against.
	Now func() time.Time
}

// The defaults: two readings at once; the largest journal kept, 16MiB, reserves
// 64MiB, so it and its copy fit beside a second reading in progress.
const (
	recordCacheOperations = 2
	recordCacheBytes      = 256 << 20
	recordCacheEntries    = 8
	recordCacheFileBytes  = 16 << 20
	recordCacheSettle     = filememo.TimestampSettle
	// A page asked for again comes within seconds; a journal nobody has read
	// for this long is read again for the price of one parse.
	recordCacheIdle = 30 * time.Second
)

// RecordCacheStats is what a cache holds and has done, for a test or a bench.
type RecordCacheStats struct {
	Operations int
	Entries    int
	Retained   int64
	Reserved   int64
	// Parses are journals read; Hits were answered from a kept record; Shared
	// waited on another reading's parse; Busy were refused at the gate.
	Parses, Hits, Shared, Busy int64
}

// RecordCache keeps the readings of unchanged journals and gates every reading;
// see the top of this file.
type RecordCache struct {
	limits RecordCacheLimits

	mu         sync.Mutex
	operations int
	order      *list.List // of *recordSlot, most recently used at the front
	kept       map[string]*list.Element
	flights    map[string]*recordFlight
	retained   int64
	reserved   int64
	stats      RecordCacheStats
	// The timer always targets the earliest unleased record's deadline.
	sweepAt         time.Time
	stopSweep       func()
	sweepGeneration uint64
	after           func(time.Duration, func()) func()
}

// recordSlot is one kept reading. A slot evicted while copies of it are out is
// dropped from the map and still counted until the last one is returned.
type recordSlot struct {
	path    string
	stamp   recordStamp
	record  Record
	cost    int64
	leases  int
	dropped bool
	// used is when the record was kept or last handed out.
	used time.Time
}

// recordFlight is one parse that may be kept, and whether a reader waits on it.
type recordFlight struct {
	stamp  recordStamp
	done   chan struct{}
	waiter bool
}

// NewRecordCache is an empty cache within limits.
func NewRecordCache(limits RecordCacheLimits) *RecordCache {
	if limits.Operations <= 0 {
		limits.Operations = recordCacheOperations
	}
	if limits.Bytes <= 0 {
		limits.Bytes = recordCacheBytes
	}
	if limits.Entries <= 0 {
		limits.Entries = recordCacheEntries
	}
	if limits.FileBytes <= 0 {
		limits.FileBytes = recordCacheFileBytes
	}
	if limits.Settle <= 0 {
		limits.Settle = recordCacheSettle
	}
	if limits.Idle <= 0 {
		limits.Idle = recordCacheIdle
	}
	if limits.Now == nil {
		limits.Now = time.Now
	}
	return &RecordCache{
		limits: limits,
		after: func(delay time.Duration, f func()) func() {
			timer := time.AfterFunc(delay, f)
			return func() { timer.Stop() }
		},
		order:   list.New(),
		kept:    map[string]*list.Element{},
		flights: map[string]*recordFlight{},
	}
}

// Stats is the cache as it stands.
func (c *RecordCache) Stats() RecordCacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	stats := c.stats
	stats.Operations, stats.Entries = c.operations, len(c.kept)
	stats.Retained, stats.Reserved = c.retained, c.reserved
	return stats
}

// WithRecord hands use the record [ReadTranscript] would read at path, and
// returns what use returns. The record is the callback's own and lives until
// it returns; it is answered from a kept reading when the file has not changed
// since. When as many readings are in progress as the limits allow, nothing is
// read and the answer is [ErrRecordReadBusy].
func (c *RecordCache) WithRecord(path string, use func(Record) error) error {
	if strings.TrimSpace(path) == "" {
		return use(Record{})
	}
	c.mu.Lock()
	if c.operations >= c.limits.Operations {
		c.stats.Busy++
		c.mu.Unlock()
		return ErrRecordReadBusy
	}
	c.operations++
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.operations--
		c.mu.Unlock()
	}()
	record, release, err := c.acquire(path)
	if err != nil {
		return err
	}
	defer release()
	return use(record)
}

// acquire is the record for one reading that holds an operation slot, and
// what returns its reservation.
func (c *RecordCache) acquire(path string) (Record, func(), error) {
	waited := false
	for {
		info, err := os.Stat(path)
		if err != nil {
			// Missing, or out of reach: the plain read answers nothing.
			return Record{}, func() {}, nil
		}
		if !info.Mode().IsRegular() {
			return Record{Unreadable: unreadRefusedWord}, func() {}, nil
		}
		stamp, stamped := recordStampOf(info)

		c.mu.Lock()
		if stamped {
			if element := c.kept[path]; element != nil {
				slot := element.Value.(*recordSlot)
				if slot.stamp == stamp {
					return c.leaseLocked(element)
				}
				// The file moved on; what was kept for it is no longer the file.
				c.dropLocked(element)
			}
			if flight := c.flights[path]; flight != nil && flight.stamp == stamp && !flight.waiter && !waited {
				flight.waiter = true
				c.stats.Shared++
				c.mu.Unlock()
				<-flight.done
				// Asked again: answered from what the parse kept, or read
				// afresh if it kept nothing.
				waited = true
				continue
			}
		}
		return c.parseLocked(path, info.Size(), stamp, stamped)
	}
}

// leaseLocked hands out a copy of a kept record, reserving its own slices.
// It is called with the lock held and returns with it released.
func (c *RecordCache) leaseLocked(element *list.Element) (Record, func(), error) {
	slot := element.Value.(*recordSlot)
	slot.leases++ // pinned, so making room cannot count it as freed
	vectors := recordVectors(slot.record)
	if !c.reserveLocked(vectors) {
		c.unleaseLocked(slot)
		c.stats.Busy++
		c.mu.Unlock()
		return Record{}, nil, ErrRecordReadBusy
	}
	c.order.MoveToFront(element)
	slot.used = c.limits.Now()
	c.watchIdleLocked()
	c.stats.Hits++
	c.mu.Unlock()
	record := cloneRecord(slot.record)
	return record, func() {
		c.mu.Lock()
		c.reserved -= vectors
		c.unleaseLocked(slot)
		c.mu.Unlock()
	}, nil
}

// parseLocked reads the journal whole, under a reservation, and keeps the
// reading when it is true to keep. It is called with the lock held and
// returns with it released.
func (c *RecordCache) parseLocked(path string, size int64, stamp recordStamp, stamped bool) (Record, func(), error) {
	estimate := recordReserve(size)
	// A journal estimated past the whole budget is read alone, never kept.
	reserve := min(estimate, c.limits.Bytes)
	if !c.reserveLocked(reserve) {
		c.stats.Busy++
		c.mu.Unlock()
		return Record{}, nil, ErrRecordReadBusy
	}
	var flight *recordFlight
	if stamped && c.flights[path] == nil && size <= c.limits.FileBytes && estimate <= c.limits.Bytes &&
		filememo.TimestampsSettled(c.limits.Now(), time.Unix(0, stamp.mtime), time.Unix(0, stamp.ctime), c.limits.Settle) {
		flight = &recordFlight{stamp: stamp, done: make(chan struct{})}
		c.flights[path] = flight
	}
	c.stats.Parses++
	c.mu.Unlock()

	record, settled := readSettled(path, stamp, stamped)
	cost := recordCost(record)

	c.mu.Lock()
	defer c.mu.Unlock()
	if flight != nil {
		delete(c.flights, path)
		defer close(flight.done)
	}
	// The estimate gives way to what was read.
	c.reserved -= reserve
	if flight != nil && settled && record.Unreadable == "" && cost <= reserve {
		// Kept, the record is the cache's and the reader is handed a copy.
		vectors := recordVectors(record)
		for c.order.Len() >= c.limits.Entries {
			c.dropLocked(c.order.Back())
		}
		if c.reserveLocked(cost + vectors) {
			c.reserved -= cost
			slot := &recordSlot{path: path, stamp: stamp, record: record, cost: cost, leases: 1, used: c.limits.Now()}
			c.kept[path] = c.order.PushFront(slot)
			c.retained += cost
			c.watchIdleLocked()
			copied := cloneRecord(record)
			return copied, func() {
				c.mu.Lock()
				c.reserved -= vectors
				c.unleaseLocked(slot)
				c.mu.Unlock()
			}, nil
		}
	}
	// Not kept: the reader owns what it read, and it is counted at its size
	// until the callback returns, over the budget if it must be.
	c.reserved += cost
	return record, func() {
		c.mu.Lock()
		c.reserved -= cost
		c.mu.Unlock()
		releaseAfterLargeRead(size)
	}, nil
}

// reserveLocked counts n bytes against the budget, evicting kept records that
// no callback holds, least recently used first. It reserves nothing and evicts
// nothing when even an empty cache would not make room.
func (c *RecordCache) reserveLocked(n int64) bool {
	var evictable int64
	for element := c.order.Back(); element != nil; element = element.Prev() {
		if slot := element.Value.(*recordSlot); slot.leases == 0 {
			evictable += slot.cost
		}
	}
	if c.retained-evictable+c.reserved+n > c.limits.Bytes {
		return false
	}
	for element := c.order.Back(); element != nil && c.retained+c.reserved+n > c.limits.Bytes; {
		prev := element.Prev()
		if element.Value.(*recordSlot).leases == 0 {
			c.dropLocked(element)
		}
		element = prev
	}
	c.reserved += n
	return true
}

// Expire drops the kept records nobody has read for the idle time. A record a
// callback still holds stays until the next look. It runs by itself while
// anything is kept; calling it is only for a test.
func (c *RecordCache) Expire() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expireLocked()
}

func (c *RecordCache) expireLocked() {
	c.cancelSweepLocked()
	now := c.limits.Now()
	for element := c.order.Back(); element != nil; {
		prev := element.Prev()
		if slot := element.Value.(*recordSlot); slot.leases == 0 && now.Sub(slot.used) >= c.limits.Idle {
			c.dropLocked(element)
		}
		element = prev
	}
	c.watchIdleLocked()
}

func (c *RecordCache) cancelSweepLocked() {
	if c.stopSweep != nil {
		c.stopSweep()
		c.stopSweep = nil
	}
	c.sweepAt = time.Time{}
	c.sweepGeneration++
}

// watchIdleLocked schedules the earliest unleased deadline, including the
// remainder after a sweep. Leased records are reconsidered when returned.
func (c *RecordCache) watchIdleLocked() {
	var deadline time.Time
	for element := c.order.Back(); element != nil; element = element.Prev() {
		slot := element.Value.(*recordSlot)
		due := slot.used.Add(c.limits.Idle)
		if slot.leases == 0 && (deadline.IsZero() || due.Before(deadline)) {
			deadline = due
		}
	}
	if deadline.Equal(c.sweepAt) {
		return
	}
	c.cancelSweepLocked()
	if deadline.IsZero() {
		return
	}
	c.sweepAt = deadline
	generation := c.sweepGeneration
	c.stopSweep = c.after(max(0, deadline.Sub(c.limits.Now())), func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.sweepGeneration == generation {
			c.expireLocked()
		}
	})
}

// dropLocked forgets one kept reading; its bytes stay counted while a copy of
// it is out.
func (c *RecordCache) dropLocked(element *list.Element) {
	slot := element.Value.(*recordSlot)
	c.order.Remove(element)
	delete(c.kept, slot.path)
	slot.dropped = true
	if slot.leases == 0 {
		c.retained -= slot.cost
	}
}

// unleaseLocked returns one copy of a kept reading.
func (c *RecordCache) unleaseLocked(slot *recordSlot) {
	slot.leases--
	if slot.dropped && slot.leases == 0 {
		c.retained -= slot.cost
	}
	c.watchIdleLocked()
}

// readSettled is the plain reading of the journal at path, through the one
// parser, and whether the file it read is the one stamped and stood still
// through the reading.
func readSettled(path string, stamp recordStamp, stamped bool) (Record, bool) {
	file, err := openRecordFile(path)
	if err != nil {
		return Record{}, false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return Record{Unreadable: unreadRefusedWord}, false
	}
	before := stamped && sameStamp(info, stamp)
	record := transcriptFrom(readJournal(file, path, false))
	if !before {
		return record, false
	}
	after, err := file.Stat()
	return record, err == nil && sameStamp(after, stamp)
}

// sameStamp is whether a file's stat still carries the stamp.
func sameStamp(info os.FileInfo, stamp recordStamp) bool {
	now, ok := recordStampOf(info)
	return ok && now == stamp
}

// recordReserve is what a journal of size bytes is expected to cost once read.
// The record keeps the display text of each line, capped, plus a fixed shape
// of 328 bytes per entry on 64-bit hosts. Short journal lines can expand
// beyond four times the file, before their strings and nested metadata are
// counted. Reserve five times the file plus room for small records. This is
// still an estimate: a record that comes out larger is read but not kept.
func recordReserve(size int64) int64 {
	return 5*size + 64<<10
}

// recordVectors is what a copy of a record costs: its own slices, with the
// strings still shared.
func recordVectors(record Record) int64 {
	cost := int64(len(record.Entries)+len(record.Earlier)) * int64(unsafe.Sizeof(DisplayEntry{}))
	cost += int64(len(record.ModelSwitches)) * int64(unsafe.Sizeof(ModelSwitch{}))
	cost += int64(len(record.Requests.Recent)) * int64(unsafe.Sizeof(RequestLine{}))
	cost += int64(len(record.Passes)) * int64(unsafe.Sizeof(CompactionFact{}))
	cost += progressCost(record.Progress)
	for _, entries := range [][]DisplayEntry{record.Entries, record.Earlier} {
		for i := range entries {
			entry := &entries[i]
			cost += int64(len(entry.ImageRefs))*int64(unsafe.Sizeof("")) +
				int64(len(entry.ReplyTags))*int64(unsafe.Sizeof(TaskReplyTag{})) +
				int64(len(entry.CompletionTasks))*int64(unsafe.Sizeof(TaskReplyTag{})) +
				int64(len(entry.Team))*int64(unsafe.Sizeof(TeamLine{}))
			if entry.Person != nil {
				cost += composedSizes(entry.Person.Composed)
				cost += int64(unsafe.Sizeof(PersonMark{})) +
					int64(len(entry.Person.Attachments))*int64(unsafe.Sizeof(MessageAttachment{}))
				for _, attachment := range entry.Person.Attachments {
					cost += attachmentSizes(attachment)
				}
				cost += int64(len(entry.Person.Reads)) * int64(unsafe.Sizeof(MessageRead{}))
				for _, read := range entry.Person.Reads {
					cost += int64(len(read.Items))*int64(unsafe.Sizeof(MessageReference{})) +
						int64(len(read.Warnings))*int64(unsafe.Sizeof(""))
				}
			}
			if entry.Steer != nil {
				cost += int64(unsafe.Sizeof(SteerMark{}))
			}
			if entry.ToolProcess != nil {
				cost += int64(unsafe.Sizeof(ToolProcess{}))
			}
		}
	}
	return cost
}

// recordCost is what a record holds: every entry's shape and every byte its
// strings and slices point at. Strings that share memory are counted once each
// time they appear, which can only overstate it.
func recordCost(record Record) int64 {
	cost := int64(unsafe.Sizeof(record))
	cost += entriesCost(record.Entries) + entriesCost(record.Earlier)
	// The private origin vector is immutable and shared by leases, but its
	// retained allocation is still charged here, including spare capacity.
	cost += int64(cap(record.userLines)) * int64(unsafe.Sizeof(recordUserLine{}))
	cost += int64(len(record.Unreadable))
	cost += int64(cap(record.ModelSwitches)) * int64(unsafe.Sizeof(ModelSwitch{}))
	for _, n := range record.ModelSwitches {
		cost += int64(len(n.ID) + len(n.Text))
	}
	cost += int64(cap(record.Requests.Recent)) * int64(unsafe.Sizeof(RequestLine{}))
	// A pass's summary is the one long string it carries; the journal holds
	// it on the marker line, so this is bounded by the file like the rest.
	cost += int64(cap(record.Passes)) * int64(unsafe.Sizeof(CompactionFact{}))
	for i := range record.Passes {
		cost += int64(len(record.Passes[i].Summary) + len(record.Passes[i].Model) + len(record.Passes[i].Failed))
	}
	cost += progressCost(record.Progress)
	for _, step := range record.Progress {
		cost += int64(len(step.Step) + len(step.Event))
	}
	return cost
}

// progressCost is what a record's step marks hold beside its entries: a key
// and a mark each, the strings still shared.
func progressCost(progress map[int]factory.StepProgress) int64 {
	return int64(len(progress)) * int64(unsafe.Sizeof(int(0))+unsafe.Sizeof(factory.StepProgress{}))
}

func entriesCost(entries []DisplayEntry) int64 {
	cost := int64(cap(entries)) * int64(unsafe.Sizeof(DisplayEntry{}))
	for i := range entries {
		entry := &entries[i]
		cost += int64(len(entry.RunID) + len(entry.Role) + len(entry.Text) + len(entry.Tool) + len(entry.Hint) +
			len(entry.CallID) + len(entry.Args) + len(entry.Output) + len(entry.Caption) + len(entry.CaptionCategory))
		cost += int64(cap(entry.ImageRefs)) * int64(unsafe.Sizeof(""))
		for _, ref := range entry.ImageRefs {
			cost += int64(len(ref))
		}
		cost += int64(cap(entry.ReplyTags)) * int64(unsafe.Sizeof(TaskReplyTag{}))
		for _, tag := range entry.ReplyTags {
			cost += int64(len(tag.Title) + len(tag.Request) + len(tag.Obligation) + len(tag.State))
		}
		cost += int64(cap(entry.CompletionTasks)) * int64(unsafe.Sizeof(TaskReplyTag{}))
		for _, tag := range entry.CompletionTasks {
			cost += int64(len(tag.Title) + len(tag.Request) + len(tag.Obligation) + len(tag.State))
		}
		if entry.Steer != nil {
			cost += int64(unsafe.Sizeof(SteerMark{})) + int64(len(entry.Steer.Landing))
		}
		if entry.Pass != nil {
			cost += int64(unsafe.Sizeof(CompactionFact{})) + int64(len(entry.Pass.Summary)+len(entry.Pass.Model)+len(entry.Pass.Failed))
		}
		if entry.ToolProcess != nil {
			cost += int64(unsafe.Sizeof(ToolProcess{})) + int64(len(entry.ToolProcess.Service))
		}
		cost += int64(cap(entry.Team)) * int64(unsafe.Sizeof(TeamLine{}))
		for _, line := range entry.Team {
			cost += int64(len(line.Team) + len(line.From) + len(line.To) + len(line.Kind) + len(line.Text) + len(line.Thread))
		}
		if entry.Person != nil {
			cost += composedSizes(entry.Person.Composed)
			cost += int64(unsafe.Sizeof(PersonMark{})) + int64(len(entry.Person.ClientID)) + int64(cap(entry.Person.Attachments))*int64(unsafe.Sizeof(MessageAttachment{}))
			for _, attachment := range entry.Person.Attachments {
				cost += int64(len(attachment.ID)+len(attachment.Kind)+len(attachment.Name)+len(attachment.Path)+len(attachment.URL)) +
					attachmentSizes(attachment)
			}
			cost += int64(cap(entry.Person.Reads)) * int64(unsafe.Sizeof(MessageRead{}))
			for _, read := range entry.Person.Reads {
				cost += referenceCost(read.Source)
				cost += int64(cap(read.Items)) * int64(unsafe.Sizeof(MessageReference{}))
				for _, item := range read.Items {
					cost += referenceCost(item)
				}
				cost += int64(cap(read.Warnings)) * int64(unsafe.Sizeof(""))
				for _, warning := range read.Warnings {
					cost += int64(len(warning))
				}
			}
		}
	}
	return cost
}

// referenceCost is the bytes one reference's strings hold.
func referenceCost(ref MessageReference) int64 {
	return int64(len(ref.Kind) + len(ref.ID) + len(ref.Name) + len(ref.Owner) + len(ref.Path) + len(ref.URL))
}

// attachmentSizes is what one descriptor's measured sizes hold beside it.
func attachmentSizes(attachment MessageAttachment) int64 {
	var cost int64
	if attachment.Bytes != nil {
		cost += int64(unsafe.Sizeof(*attachment.Bytes))
	}
	if attachment.Lines != nil {
		cost += int64(unsafe.Sizeof(*attachment.Lines))
	}
	cost += sourceSizes(attachment.Source)
	return cost
}

// cloneRecord is a record no other reader can reach through: its own entry
// slices, and its own copy of every slice and pointer inside an entry.
func cloneRecord(record Record) Record {
	record.ModelSwitches = append([]ModelSwitch(nil), record.ModelSwitches...)
	record.Entries = cloneEntries(record.Entries)
	record.Earlier = cloneEntries(record.Earlier)
	record.Requests.Recent = slices.Clone(record.Requests.Recent)
	record.Passes = slices.Clone(record.Passes)
	record.Progress = maps.Clone(record.Progress)
	return record
}

func cloneEntries(entries []DisplayEntry) []DisplayEntry {
	if entries == nil {
		return nil
	}
	out := slices.Clone(entries)
	for i := range out {
		entry := &out[i]
		entry.ImageRefs = slices.Clone(entry.ImageRefs)
		entry.ReplyTags = slices.Clone(entry.ReplyTags)
		entry.CompletionTasks = slices.Clone(entry.CompletionTasks)
		entry.Team = slices.Clone(entry.Team)
		if entry.Person != nil {
			person := PersonMark{Authored: entry.Person.Authored, Attachments: cloneAttachments(entry.Person.Attachments), Reads: cloneReads(entry.Person.Reads), Composed: cloneComposed(entry.Person.Composed), Queued: entry.Person.Queued, ClientID: entry.Person.ClientID}
			entry.Person = &person
		}
		if entry.Steer != nil {
			steer := *entry.Steer
			entry.Steer = &steer
		}
		if entry.Pass != nil {
			pass := *entry.Pass
			entry.Pass = &pass
		}
		if entry.ToolProcess != nil {
			process := *entry.ToolProcess
			entry.ToolProcess = &process
		}
	}
	return out
}
