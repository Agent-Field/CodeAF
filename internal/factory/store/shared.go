package store

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── a floor that has not changed is listed without a read or a copy ───────
//
// THE SETTLE WAS THE LAST FULL READING OF AN IDLE FLOOR. The factory settles
// the floor every five seconds while a lease is held (the runner's beats) and
// before every person's request, and each settle listed it through
// [Store.List]: per job a stat for the read, a second stat and a second deep
// copy for the hydrated read, and a stat and a read of the runner settings to
// trim the job's output. The eighth idle measurement on macOS, after #2517,
// still spent a ninth of its samples there, with 86 jobs on a floor on which
// nothing had changed (PERF.md, "Factory floor reads").
//
// So the store keeps the listing it made last, with the file each job was read
// from, as the lease desk's queue does ([lease.Desk] floorQueue). A listing
// reads the folder once, and a job whose file is the one it read before is
// answered from memory: no second stat, no decode and no copy. A job edited on
// disk by anything, another codeaf or a person's editor, is another file or
// the same file with another size or time, and is read again. The job's
// hydration (its author's access, the retention of its output) is applied to
// every listing afresh, because neither is in the file.
//
// THE ANSWER IS SHARED AND READ-ONLY. Its items are the items the next listing
// is handed, so a caller may replace an element of the slice it was given but
// never write through one (a field of a pointer, a slice or a map it holds).
// A caller that acts reads the job again under its own lock, as every settle
// already does.

// sharedReads counts the job documents a shared listing read again, so a test
// can say a listing of an unchanged floor read none and an edit read one.
var sharedReads atomic.Int64

// SharedReads is how many job documents shared listings have read again,
// for tests outside the package.
func SharedReads() int64 { return sharedReads.Load() }

// sharedList is a store's last shared listing. Its zero value is empty.
type sharedList struct {
	mu sync.Mutex
	// files is each job file the listing was made from, by name.
	files map[string]sharedFile
	// around is the identity of the documents a job's reading depends on
	// beside its own file: the watched repositories and the checkouts, which
	// name a legacy job's repository ([Store.migrateRepository]).
	around string
}

type sharedFile struct {
	info os.FileInfo
	item factory.Item
}

// sharedAround is the documents beside the jobs that a reading of one depends on.
var sharedAround = []string{"repos.json", "checkouts.json"}

// ListShared answers what [Store.List] answers, in its order, from memory for
// every job whose file has not changed since the last shared listing. The
// items are shared with every other caller: see the law above.
func (st *Store) ListShared() ([]factory.Item, error) {
	entries, err := os.ReadDir(st.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var around strings.Builder
	for _, name := range sharedAround {
		if info, err := os.Stat(filepath.Join(st.root, name)); err == nil {
			around.WriteString(name + " " + strconv.FormatInt(info.Size(), 10) + " " + strconv.FormatInt(info.ModTime().UnixNano(), 10) + "\n")
		}
	}
	keep := st.outputKeep()
	now := st.now()
	q := &st.shared
	q.mu.Lock()
	fresh := q.files == nil || q.around != around.String()
	next := make(map[string]sharedFile, len(entries))
	var ids repositoryIDs
	loadedIDs := false
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		// ONLY A NUMBER NAMES AN ITEM, as in [Store.list].
		if _, err := strconv.Atoi(strings.TrimSuffix(name, ".json")); err != nil {
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
		sharedReads.Add(1)
		d, err := st.read(filepath.Join(st.root, name))
		if err != nil {
			// AN UNREADABLE DOCUMENT IS SKIPPED, as a listing skips it, and is
			// read again on the next listing rather than remembered as nothing.
			continue
		}
		if !loadedIDs && needsRepositoryLookup(d.Item) {
			if ids, err = st.repositoryIdentities(); err != nil {
				q.mu.Unlock()
				return nil, err
			}
			loadedIDs = true
		}
		if d, err = st.migrateRepository(d, ids); err != nil {
			q.mu.Unlock()
			return nil, err
		}
		next[name] = sharedFile{info: info, item: d.Item}
	}
	q.files, q.around = next, around.String()
	saved := make([]factory.Item, 0, len(next))
	for _, f := range next {
		saved = append(saved, f.item)
	}
	q.mu.Unlock()
	items := make([]factory.Item, 0, len(saved))
	for _, it := range saved {
		if needsTier(it) {
			// A job whose author's access is still to be looked up goes the
			// way [Store.List] takes it, lookup and fresh read included: the
			// lookup may write the job, and few jobs are ever waiting on one.
			if it, err = st.readHydrated(it); err != nil {
				return nil, err
			}
			items = append(items, it)
			continue
		}
		st.tierDetail(&it)
		keep.apply(&it, now)
		items = append(items, it)
	}
	sort.SliceStable(items, func(a, b int) bool {
		if items[a].Created.Equal(items[b].Created) {
			return items[a].ID > items[b].ID
		}
		return items[a].Created.After(items[b].Created)
	})
	return items, nil
}

// needsTier says whether a read of it may look up its author's access
// ([Store.hydrateTier] does nothing for any other job).
func needsTier(it factory.Item) bool {
	return it.Tier == "" && it.RefusesWrite() && it.Author != "" && factory.RepoOf(it) != ""
}

// Stamp is the identity of one job's file as it was looked at: two stamps
// that are [Stamp.Same] saw one unchanged document. The zero Stamp is the same
// as nothing.
type Stamp struct {
	info os.FileInfo
	// projected is set when the job's reading carries something that is not
	// in its file (its author's access still being looked up), so the
	// document read is not answered by its file alone.
	projected bool
}

// Stamp looks at the file of job id without reading it.
func (st *Store) Stamp(id int) (Stamp, error) {
	if err := checkID(id); err != nil {
		return Stamp{}, ErrNotFound
	}
	info, err := os.Stat(st.ItemPath(id))
	if err != nil {
		return Stamp{}, err
	}
	st.tierMu.Lock()
	projected := st.tierReads[id] != nil
	st.tierMu.Unlock()
	return Stamp{info: info, projected: projected}, nil
}

// Same says the two looks saw one unchanged document, and that a read of it
// answers what its file holds.
func (s Stamp) Same(o Stamp) bool {
	return !s.projected && !o.projected && sameFile(s.info, o.info)
}

// outputKeep is the output retention a read applies: how many days and bytes
// of a job's log are kept. Nothing is trimmed when either is below one.
type outputKeep struct{ days, bytes int }

// outputKeep reads the retention once, for every job a listing reads.
func (st *Store) outputKeep() outputKeep {
	if st.outputPolicy != nil {
		days, bytes := st.outputPolicy()
		return outputKeep{days, bytes}
	}
	if _, err := os.Stat(filepath.Join(st.root, "runner-settings.json")); err != nil {
		return outputKeep{}
	}
	settings, err := st.RunnerSettings()
	if err != nil {
		return outputKeep{}
	}
	return outputKeep{settings.KeepOutputDays, settings.KeepOutputMB * (1 << 20)}
}

// apply trims it's log to the retention, newest lines first. It writes a
// copy of the job's stream, never the stream it was handed, so a job shared
// by a listing is trimmed without changing what the next listing is given.
func (k outputKeep) apply(it *factory.Item, now time.Time) {
	if it.Stream == nil || k.days < 1 || k.bytes < 1 {
		return
	}
	cutoff := now.Add(-time.Duration(k.days) * 24 * time.Hour)
	budget := k.bytes
	kept := []factory.LogLine{}
	for i := len(it.Stream.Log) - 1; i >= 0; i-- {
		line := it.Stream.Log[i]
		if !line.At.IsZero() && line.At.Before(cutoff) {
			continue
		}
		if len(line.Text) > budget {
			break
		}
		budget -= len(line.Text)
		kept = append(kept, line)
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	stream := *it.Stream
	stream.Log = kept
	it.Stream = &stream
}
