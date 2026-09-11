package standing

// readings.go is what lets a file watch say WHICH files changed, not just that
// something did.
//
// A watch's document keeps one digest ([Item.Fingerprint]) of everything its
// glob matched, and a digest can only answer "same or different". A firing
// woken by a difference was then handed the whole listing and left to guess
// which lines were new — which is exactly the question the person asked the
// watch to answer. So every reading's per-file manifest is kept beside the
// item, CONTENT-ADDRESSED BY ITS DIGEST: the document names a digest, and the
// file of that name is what the files looked like then.
//
// CONTENT ADDRESSING IS WHAT MAKES A CRASH HARMLESS. A reading is written
// before the document ever names it, and the document still names the previous
// digest until a firing is recorded. A pass that dies half way leaves the
// previous reading exactly where the retry will look for it, so the retry
// reports the same changes rather than none. The only readings ever removed are
// ones the document no longer names and the current look did not just write.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// fileEntry is one file as a reading saw it: its name relative to the
// workspace, its size, its modification time, and the hash of its contents
// where it has one ([contentHash]). A reading written before contents were
// hashed has none, and is compared by size and time.
type fileEntry struct {
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime"`
	Hash  string `json:"hash,omitempty"`
}

// differs answers whether a file changed between two readings. Where both
// readings hashed it, its contents decide and its time does not, so a file
// touched or rewritten with the same text is the same file.
func (e fileEntry) differs(later fileEntry) bool {
	if e.Size != later.Size {
		return true
	}
	if e.Hash != "" && later.Hash != "" {
		return e.Hash != later.Hash
	}
	return e.MTime != later.MTime
}

// readingsDir is where an item's manifests live, inside its own folder.
func (s *Store) readingsDir(id string) string { return filepath.Join(s.ItemDir(id), "readings") }

func (s *Store) readingPath(id, digest string) string {
	return filepath.Join(s.readingsDir(id), digest+".json")
}

// keepReading writes this reading's manifest and tidies away every other
// manifest except the ones named in keep — the reading the pass started from,
// and the one a run that did not finish was measured from
// ([Store.unreportedSince]) — and the one the item's document names at the
// moment of tidying ([Store.tidyReadings]). The pass treats it as best effort:
// a manifest that could not be written costs the next firing its change list
// ([Occurrence.ChangesUnknown]) and never the firing itself.
func (s *Store) keepReading(id, digest string, files map[string]fileEntry, refresh bool, keep ...string) error {
	if err := s.writeReading(id, digest, files, refresh); err != nil {
		return err
	}
	s.tidyReadings(id, append(keep, digest)...)
	return nil
}

// writeReading writes one reading's manifest under its digest.
//
// A MANIFEST ALREADY ON DISK IS REWRITTEN WHEN refresh SAYS ITS TIMES MOVED.
// The digest names contents, so a touched file leaves it the same; rewriting
// the manifest under it with the new times is what lets the next pass carry
// the file's hash instead of reading the file again on every pass.
func (s *Store) writeReading(id, digest string, files map[string]fileEntry, refresh bool) error {
	if err := checkID(id); err != nil {
		return err
	}
	if digest == "" {
		return errors.New("a reading with no digest")
	}
	if err := os.MkdirAll(s.readingsDir(id), 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(s.readingPath(id, digest)); err == nil && !refresh {
		return nil
	}
	data, err := json.Marshal(files)
	if err != nil {
		return err
	}
	return writeAtomic(s.readingPath(id, digest), data)
}

// tidyReadings removes every manifest but the ones named in keep and the one
// the item's document names NOW.
//
// THE DOCUMENT IS READ UNDER ITS OWN LOCK, AT THE MOMENT OF DELETING (L2). A
// pass decides what to keep when it starts; an edit that lands while it walks
// takes a new baseline and names it ([Store.Revise]), under this same lock. A
// tidy that trusted the pass's list deleted that baseline, and the next pass —
// unable to load the reading its item named — read the folder as changed in
// unknown ways and fired, billed, on a folder where nothing had changed (wave
// 5 review). Read here, the reading the item names is never the one removed.
func (s *Store) tidyReadings(id string, keep ...string) {
	_ = s.underItemLock(id, func() error {
		current, err := s.read(s.ItemPath(id))
		if err != nil {
			return err
		}
		keep = append(keep, current.Fingerprint)
		entries, err := os.ReadDir(s.readingsDir(id))
		if err != nil {
			return err
		}
		for _, entry := range entries {
			name := strings.TrimSuffix(entry.Name(), ".json")
			if slices.Contains(keep, name) || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			_ = os.Remove(filepath.Join(s.readingsDir(id), entry.Name()))
		}
		return nil
	})
}

// baseline takes a file watch's first reading AT THE YES — [Store.Create], and
// [Store.Revise] when the pattern it watches changes — and names it on the
// item. Anything else leaves it naming no reading.
//
// A BASELINE TAKEN AT THE FIRST PASS SWALLOWED THE GAP (wave 5, ruling R10). The
// first pass came up to five minutes after the yes, or whenever a window next
// opened, and read everything it found as the baseline — so the ticket that
// landed a minute after the person said "tell me when a ticket comes in" was
// never told. Taken here, anything that changes after the yes is a change.
//
// IT STATS AND NEVER READS. The walk is the pass's own ([watched]), bounded by
// [WatchLimit] entries and made once already by [Item.CheckWatch] a moment
// before; no file's contents are read, so a yes costs at most ten thousand
// stats however large the files are. The first pass fills in the hashes, and
// the reading's times stand for contents until then — so a file saved again
// unchanged in that gap counts as a change. Hashing at the yes would close
// that, at the price of reading up to ten thousand files while a person waits
// on a card.
//
// IT WRITES AND NEVER TIDIES: it runs under the item's lock, and the next pass
// tidies what the item no longer names. A reading that cannot be taken or
// written leaves the item with none, and its first pass takes the baseline as
// it always did.
func (s *Store) baseline(item *Item) {
	item.Fingerprint = ""
	if item.When.Kind != WhenFile {
		return
	}
	digest, _, files, err := fingerprint(item.Workspace, item.When.Glob, nil, false)
	if err != nil || s.writeReading(item.ID, digest, files, true) != nil {
		return
	}
	item.Fingerprint = digest
}

// reading is the manifest kept under a digest. The error is the honest answer
// when that reading is not on disk — an item written before readings were
// kept, or a manifest that could not be written — and a caller comparing
// against it says the changes are unknown rather than "none".
func (s *Store) reading(id, digest string) (map[string]fileEntry, error) {
	if digest == "" {
		return nil, errors.New("no previous reading")
	}
	raw, err := os.ReadFile(s.readingPath(id, digest))
	if err != nil {
		return nil, err
	}
	var files map[string]fileEntry
	if err := json.Unmarshal(raw, &files); err != nil {
		return nil, err
	}
	return files, nil
}

// changesBetween lists what was added, modified or removed between two
// readings, by name.
func changesBetween(before, now map[string]fileEntry) []Change {
	var changes []Change
	for name, entry := range now {
		old, had := before[name]
		switch {
		case !had:
			changes = append(changes, Change{Path: name, Kind: "added"})
		case old.differs(entry):
			changes = append(changes, Change{Path: name, Kind: "modified"})
		}
	}
	for name := range before {
		if _, has := now[name]; !has {
			changes = append(changes, Change{Path: name, Kind: "removed"})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes
}

// unreportedSince answers the reading a watch's change list is measured from.
// It is the reading before this one — unless the last run DID NOT FINISH its
// work over the changes it was woken for, in which case it is the reading that
// run was measured from, so the next run is told about those changes too.
//
// A FAILED RUN USED UP ITS CHANGES. The watch moves on at every look, so the
// occurrence after a failed one listed only what had changed since the failure
// — and the files the failed run never reported were in no change list again.
// The chain holds only while the last run's own reading is the one the item
// still names; anything else is measured from the reading before, as always.
func (s *Store) unreportedSince(id, previous string) string {
	if previous == "" {
		return previous
	}
	records, err := s.Occurrences(id, 1)
	if err != nil || len(records) == 0 {
		return previous
	}
	last := records[0]
	unfinished := last.Outcome == OutcomeFailed || last.Outcome == OutcomeNeedsYou
	if last.Phase != PhaseFinished || last.Published != nil || !unfinished || last.Since == "" || last.Reading != previous {
		return previous
	}
	return last.Since
}

// changesText is the change list as a firing reads it, ahead of the listing.
// carried says the list reaches back past a run that did not finish.
func changesText(changes []Change, unknown, carried bool) string {
	if unknown {
		return "WHAT CHANGED: unknown — the previous reading is not available, so compare against the listing below.\n"
	}
	var out strings.Builder
	if carried {
		out.WriteString("WHAT CHANGED SINCE THE LAST RUN THAT FINISHED — the run after it did not, so its changes are listed again:\n")
	} else {
		out.WriteString("WHAT CHANGED SINCE THE LAST READING:\n")
	}
	for _, change := range changes {
		out.WriteString(change.Kind + "  " + change.Path + "\n")
	}
	return out.String()
}
