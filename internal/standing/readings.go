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
// except the one the document still names. Best effort: a manifest that could
// not be written costs the next firing its change list ([Occurrence.ChangesUnknown])
// and never the firing itself.
func (s *Store) keepReading(id, digest, previous string, files map[string]fileEntry) {
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
		if name == digest || name == previous || !strings.HasSuffix(entry.Name(), ".json") {
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

// changesText is the change list as a firing reads it, ahead of the listing.
func changesText(changes []Change, unknown bool) string {
	if unknown {
		return "WHAT CHANGED: unknown — the previous reading is not available, so compare against the listing below.\n"
	}
	var out strings.Builder
	out.WriteString("WHAT CHANGED SINCE THE LAST READING (size or modification time):\n")
	for _, change := range changes {
		out.WriteString(change.Kind + "  " + change.Path + "\n")
	}
	return out.String()
}
