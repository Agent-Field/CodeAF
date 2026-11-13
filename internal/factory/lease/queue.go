package lease

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// ── the queue a pull reads is built once per change of the floor ───────────
//
// EVERY RUNNER SLOT LISTED AND DEEP-COPIED THE WHOLE FLOOR ON EVERY LOOK. A
// pull is one slot; a machine runs fifteen of them, each holding a pull open,
// and every one of them woke on the same change and each made its own
// [store.Store.ListSaved] (a deep copy of every job document) and its own sort
// of the result. The seventh live A+B run idled at a fifth of a core with 86
// jobs on the floor, and the biggest named path in its profile was exactly
// that: Pull → ListSaved → deepCopy (docs/changes, r7).
//
// So the desk keeps the queue it read last, in [runsBefore]'s order, with the
// file each job was read from. A look stats the floor (one directory read)
// and reuses every job whose file is the one it read before; only a job whose
// file changed is read again, and the queue is sorted again only when one did.
// Fifteen slots woken by one change pay one read of that job and one sort
// between them. THE QUEUE IS SHARED AND READ-ONLY: a look only asks of each job
// whether it may be taken, and the reservation re-reads the job under its own
// lock ([Desk.reserve]), so nothing a look is handed is ever written.

// queueBuilds counts the queues sorted afresh, and queueReads the job
// documents a queue read again, so a test can say fifteen looks at one
// unchanged floor built nothing and one change read one job.
var queueBuilds, queueReads atomic.Int64

// QueueCounts is how many queues were built and job documents read for one,
// for tests outside the package.
func QueueCounts() (builds, reads int64) { return queueBuilds.Load(), queueReads.Load() }

// queueCache is the desk's last queue. Its zero value is empty.
type queueCache struct {
	mu sync.Mutex
	// files is each job file the queue was built from, by name.
	files map[string]queuedFile
	// around is the identity of the documents a job's reading depends on
	// beside its own file: the watched repositories and the checkouts, which
	// name a legacy job's repository ([store.Store.ListSaved]).
	around string
	sorted []factory.Item
}

type queuedFile struct {
	info os.FileInfo
	item factory.Item
	ok   bool
}

// queueAround is the documents beside the jobs a listing reads.
var queueAround = []string{"repos.json", "checkouts.json", "runner-settings.json"}

// floorQueue is the floor in [runsBefore]'s order. The answer is shared with every
// other look until the floor changes: a caller reads it and never writes it.
func (d *Desk) floorQueue() ([]factory.Item, error) {
	root := d.store.Root()
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var around strings.Builder
	for _, name := range queueAround {
		if info, err := os.Stat(filepath.Join(root, name)); err == nil {
			around.WriteString(name + " " + strconv.FormatInt(info.Size(), 10) + " " + strconv.FormatInt(info.ModTime().UnixNano(), 10) + "\n")
		}
	}
	q := &d.queued
	q.mu.Lock()
	defer q.mu.Unlock()
	fresh := q.files == nil || q.around != around.String()
	next := make(map[string]queuedFile, len(entries))
	changed := fresh
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		id, err := strconv.Atoi(strings.TrimSuffix(name, ".json"))
		if err != nil || id <= 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if last, ok := q.files[name]; ok && !fresh && sameFile(last.info, info) {
			next[name] = last
			continue
		}
		changed = true
		queueReads.Add(1)
		it, err := d.store.GetSaved(id)
		// AN UNREADABLE DOCUMENT IS SKIPPED, as a listing skips it, and is
		// read again on the next look rather than remembered as nothing.
		next[name] = queuedFile{info: info, item: it, ok: err == nil}
	}
	if len(next) != len(q.files) {
		changed = true
	}
	if !changed {
		return q.sorted, nil
	}
	queueBuilds.Add(1)
	type ranked struct {
		item factory.Item
		rank int
	}
	rows := make([]ranked, 0, len(next))
	for _, f := range next {
		if f.ok {
			rows = append(rows, ranked{f.item, factory.PriorityRank(f.item)})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return runsBeforeRanked(rows[i].item, rows[j].item, rows[i].rank, rows[j].rank) })
	sorted := make([]factory.Item, len(rows))
	for i, r := range rows {
		sorted[i] = r.item
	}
	for name, f := range next {
		if !f.ok {
			// Forget it, so the next look reads it again.
			delete(next, name)
		}
	}
	q.files, q.around, q.sorted = next, around.String(), sorted
	return sorted, nil
}

// sameFile says two looks saw one unchanged file: every store write renames a
// fresh file into place, so a changed document is another file.
func sameFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && store.FileStamp(a) == store.FileStamp(b) && a.Mode() == b.Mode()
}
