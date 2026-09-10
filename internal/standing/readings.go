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
// workspace, its size and its modification time. Contents are not hashed, so a
// file touched without being changed reads as modified — the change list says
// "size or time moved", never "the text changed".
type fileEntry struct {
	Size  int64 `json:"size"`
	MTime int64 `json:"mtime"`
}

// readingsDir is where an item's manifests live, inside its own folder.
func (s *Store) readingsDir(id string) string { return filepath.Join(s.ItemDir(id), "readings") }

func (s *Store) readingPath(id, digest string) string {
	return filepath.Join(s.readingsDir(id), digest+".json")
}

// keepReading writes this reading's manifest and removes every other manifest
// except the ones named in keep — the reading the document still names, and
// the one a run that did not finish was measured from ([Store.unreportedSince]).
// Best effort: a manifest that could not be written costs the next firing its
// change list ([Occurrence.ChangesUnknown]) and never the firing itself.
func (s *Store) keepReading(id, digest string, files map[string]fileEntry, keep ...string) {
	if checkID(id) != nil || digest == "" {
		return
	}
	dir := s.readingsDir(id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	if _, err := os.Stat(s.readingPath(id, digest)); err != nil {
		data, err := json.Marshal(files)
		if err != nil {
			return
		}
		if err := writeAtomic(s.readingPath(id, digest), data); err != nil {
			return
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name(), ".json")
		if name == digest || slices.Contains(keep, name) || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		_ = os.Remove(filepath.Join(dir, entry.Name()))
	}
}

// changesSince compares the reading named previous with the files seen now.
// The error is the honest answer when the previous reading is not on disk — an
// item written before readings were kept, or a manifest that could not be
// written — and the caller says the changes are unknown rather than "none".
func (s *Store) changesSince(id, previous string, now map[string]fileEntry) ([]Change, error) {
	if previous == "" {
		return nil, errors.New("no previous reading")
	}
	raw, err := os.ReadFile(s.readingPath(id, previous))
	if err != nil {
		return nil, err
	}
	var before map[string]fileEntry
	if err := json.Unmarshal(raw, &before); err != nil {
		return nil, err
	}
	var changes []Change
	for name, entry := range now {
		old, had := before[name]
		switch {
		case !had:
			changes = append(changes, Change{Path: name, Kind: "added"})
		case old != entry:
			changes = append(changes, Change{Path: name, Kind: "modified"})
		}
	}
	for name := range before {
		if _, has := now[name]; !has {
			changes = append(changes, Change{Path: name, Kind: "removed"})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, nil
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
		out.WriteString("WHAT CHANGED SINCE THE LAST RUN THAT FINISHED — the run after it did not, so its changes are listed again (size or modification time):\n")
	} else {
		out.WriteString("WHAT CHANGED SINCE THE LAST READING (size or modification time):\n")
	}
	for _, change := range changes {
		out.WriteString(change.Kind + "  " + change.Path + "\n")
	}
	return out.String()
}
