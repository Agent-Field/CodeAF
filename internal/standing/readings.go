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

// keepReading writes this reading's manifest and removes every other manifest
// except the ones named in keep — the reading the document still names, and
// the one a run that did not finish was measured from ([Store.unreportedSince]).
// The pass treats it as best effort: a manifest that could not be written
// costs the next firing its change list ([Occurrence.ChangesUnknown]) and never
// the firing itself. The error is for [Store.baseline], which names no reading
// it could not keep.
//
// A MANIFEST ALREADY ON DISK IS REWRITTEN WHEN refresh SAYS ITS TIMES MOVED.
// The digest names contents, so a touched file leaves it the same; rewriting
// the manifest under it with the new times is what lets the next pass carry
// the file's hash instead of reading the file again on every pass.
func (s *Store) keepReading(id, digest string, files map[string]fileEntry, refresh bool, keep ...string) error {
	if err := checkID(id); err != nil {
		return err
	}
	if digest == "" {
		return errors.New("a reading with no digest")
	}
	dir := s.readingsDir(id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(s.readingPath(id, digest)); err != nil || refresh {
		data, err := json.Marshal(files)
		if err != nil {
			return err
		}
		if err := writeAtomic(s.readingPath(id, digest), data); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name(), ".json")
		if name == digest || slices.Contains(keep, name) || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		_ = os.Remove(filepath.Join(dir, entry.Name()))
	}
	return nil
}

// baseline takes a file watch's first reading AT THE YES — [Store.Create], and
// [Store.Revise] when what it watches changes — and names it on the item.
//
// A BASELINE TAKEN AT THE FIRST PASS SWALLOWED THE GAP (wave 5, ruling R10). The
// first pass came up to five minutes after the yes, or whenever a window next
// opened, and read everything it found as the baseline — so the ticket that
// landed a minute after the person said "tell me when a ticket comes in" was
// never told. Taken here, anything that changes after the yes is a change.
//
// IT STATS AND NEVER READS. The walk is the pass's own ([watched]), bounded by
// [WatchLimit] entries and already made once by [Item.CheckWatch] a moment
// before; no file's contents are read, so a yes costs at most ten thousand
// stats however large the files are. The first pass fills in the hashes, and
// the reading's times stand for contents until then — so a file saved again
// unchanged in that gap is the one touch that counts as a change.
//
// A reading that cannot be taken or kept leaves the item with none, and its
// first pass takes the baseline as it always did. keep names a reading the
// item still names, so a revision that fails after this leaves it on disk.
func (s *Store) baseline(item *Item, keep ...string) {
	if item.When.Kind != WhenFile {
		return
	}
	item.Fingerprint = ""
	digest, _, files, err := fingerprint(item.Workspace, item.When.Glob, nil, false)
	if err != nil || s.keepReading(item.ID, digest, files, true, keep...) != nil {
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
