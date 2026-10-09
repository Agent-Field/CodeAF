package placegraph

// THE UNTOUCHED-PLACE SUGGESTION (design 6d "Too many places", 6e "Keeping places
// few"): a place nobody has touched for sixty days gets one quiet suggestion to
// merge or archive it, on Home and in ⌘P, and "Not now" hides it for thirty days.
//
// BOTH FIGURES ARE THE DESIGN'S OWN AND ARE CRITICAL CLEANUP POLICY, NOT
// SETTINGS. They are constants on purpose: no field of RecommendPolicy, no row on
// the settings page. docs/AI-ROLES-AND-PLACES-POLICY.md ("Critical cleanup policy") records them.
// Changing either changes that page in the same commit.
//
// NO MODEL IS CALLED, EVER. This is date arithmetic over the graph and the clock
// the caller injects, so a test pins the boundary to the nanosecond and two
// windows given the same files give the same answer.
//
// NOTHING HERE ARCHIVES, MERGES OR DELETES. It names candidates; a person's click
// on Merge or Archive is the existing write, with its existing receipt and Undo.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

const (
	// StaleAfterDays is how long a place must go untouched (design 6d: "untouched
	// for 60 days"). Exactly this long counts: the boundary is inclusive.
	StaleAfterDays = 60
	// StaleSnoozeDays is how long "Not now" hides the suggestion for a place
	// (Interactions: "Not now hides it for 30 days"). Exactly this long later it
	// is offered again: the snooze ends at its boundary.
	StaleSnoozeDays = 30
	// MaxStaleListed bounds one answer. The screens draw one line at a time; the
	// rest are here so a snooze reveals the next without another round trip.
	MaxStaleListed = 20
	// MaxStaleSnoozes bounds the snooze file. Expired snoozes are dropped on every
	// write, so reaching it takes more live snoozes than there can be places.
	MaxStaleSnoozes = MaxPlaces

	staleVersion = 1
	day          = 24 * time.Hour
)

// StalePlace is one place offered for tidying.
type StalePlace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// TouchedAt is the newest sign of life: it was opened, created, or someone
	// spoke in a chat inside it or inside a place under it.
	TouchedAt time.Time `json:"touchedAt"`
	// DaysUntouched is whole days (24 hours each) from TouchedAt to now.
	DaysUntouched int `json:"daysUntouched"`
}

// StaleInput is everything the rule reads. Nothing in it is fetched.
type StaleInput struct {
	Snap *Snapshot
	Now  time.Time
	// Activity is the newest time the person spoke in any chat filed in a place or
	// below it, by place id. A place missing from it has no chat activity.
	Activity map[string]time.Time
	// Busy reports work running in a place or below it, or a chat in it waiting on
	// the person. A busy place is never offered, whatever its dates say.
	Busy func(placeID string) bool
	// Snoozed maps a place id to the instant its snooze ends.
	Snoozed map[string]time.Time
}

// StalePlaces returns the places to suggest tidying, the longest untouched first.
//
// A place is left out when it is archived, pinned (the person put it on the rail
// on purpose), busy, snoozed, or has no date to judge by. An unknown age is never
// guessed at: a place with neither a creation nor an opened time is not stale.
// A place is only as old as the newest thing under it, so a quiet parent of a
// busy place is not offered.
func StalePlaces(in StaleInput) []StalePlace {
	if in.Snap == nil || in.Now.IsZero() {
		return []StalePlace{}
	}
	pinned := make(map[string]bool, len(in.Snap.Pinned))
	for _, id := range in.Snap.Pinned {
		pinned[id] = true
	}
	own := func(p Place) time.Time {
		return latest(p.CreatedAt, p.LastOpenedAt, in.Activity[p.ID])
	}
	out := []StalePlace{}
	for _, p := range in.Snap.Places {
		if p.Archived || pinned[p.ID] {
			continue
		}
		if in.Busy != nil && in.Busy(p.ID) {
			continue
		}
		if until, ok := in.Snoozed[p.ID]; ok && in.Now.Before(until) {
			continue
		}
		touched := own(p)
		for _, d := range in.Snap.Descendants(p.ID, false) {
			touched = latest(touched, own(d))
		}
		if touched.IsZero() {
			continue
		}
		age := in.Now.Sub(touched)
		if age < StaleAfterDays*day {
			continue
		}
		out = append(out, StalePlace{ID: p.ID, Name: p.Name, TouchedAt: touched.UTC(), DaysUntouched: int(age / day)})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].TouchedAt.Equal(out[j].TouchedAt) {
			return out[i].TouchedAt.Before(out[j].TouchedAt)
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > MaxStaleListed {
		out = out[:MaxStaleListed]
	}
	return out
}

func latest(times ...time.Time) time.Time {
	var best time.Time
	for _, t := range times {
		if t.After(best) {
			best = t
		}
	}
	return best
}

// ---- the snooze file ---------------------------------------------------------

// StaleSnooze is one "Not now".
type StaleSnooze struct {
	PlaceID string    `json:"placeId"`
	At      time.Time `json:"at"`
	Until   time.Time `json:"until"`
}

type staleDoc struct {
	Version int           `json:"version"`
	Snoozes []StaleSnooze `json:"snoozes"`
}

// StaleBook is the handle on the snooze file. Safe for concurrent use and for
// several processes (windows) on one path.
//
// IT IS A SEPARATE FILE FROM THE GRAPH, like the ledger and the choices: a
// snooze is not a change to the person's organisation, so it must not move the
// graph's revision or break an undo. It is also not a window-local guess: every
// window reads the same file, so "Not now" in one window hides the line in all.
// A DAMAGED FILE COSTS ONE REPEATED SUGGESTION AND NOTHING ELSE: reads treat it
// as empty, and the next write sets it aside and starts fresh.
type StaleBook struct {
	path string
	mu   sync.Mutex
}

// OpenStale prepares the snooze file at path (created on the first Snooze). The
// conventional place is beside the graph: <dir of places.json>/places-stale.json.
func OpenStale(path string) (*StaleBook, error) {
	if path == "" {
		return nil, fmt.Errorf("%w: empty path", ErrInvalid)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return &StaleBook{path: path}, nil
}

// Active maps each place with a snooze still running at now to when it ends. The
// file is only ever replaced whole, so the read takes no lock.
func (b *StaleBook) Active(now time.Time) map[string]time.Time {
	doc, _ := readStaleDoc(b.path)
	out := map[string]time.Time{}
	for _, s := range doc.Snoozes {
		if now.Before(s.Until) {
			out[s.PlaceID] = s.Until
		}
	}
	return out
}

// Snooze hides the suggestion for placeID until StaleSnoozeDays after now. A
// second "Not now" for the same place replaces the first. Snoozes that have
// ended are dropped while the file is open for writing.
func (b *StaleBook) Snooze(placeID string, now time.Time) (StaleSnooze, error) {
	if err := validID(placeID); err != nil {
		return StaleSnooze{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	lock, err := os.OpenFile(b.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return StaleSnooze{}, err
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, false); err != nil {
		return StaleSnooze{}, err
	}
	defer filelock.Unlock(lock)
	doc, err := readStaleDoc(b.path)
	switch {
	case errors.Is(err, ErrUnsupportedVersion):
		return StaleSnooze{}, err
	case err != nil && !errors.Is(err, os.ErrNotExist):
		// Unreadable: keep the bytes for a person to look at, then start empty.
		_ = os.Rename(b.path, fmt.Sprintf("%s.damaged-%d", b.path, now.Unix()))
		doc = staleDoc{}
	}
	at := now.UTC()
	pick := StaleSnooze{PlaceID: placeID, At: at, Until: at.Add(StaleSnoozeDays * day)}
	kept := make([]StaleSnooze, 0, len(doc.Snoozes)+1)
	for _, s := range doc.Snoozes {
		if s.PlaceID != placeID && at.Before(s.Until) {
			kept = append(kept, s)
		}
	}
	kept = append(kept, pick)
	if over := len(kept) - MaxStaleSnoozes; over > 0 {
		kept = kept[over:]
	}
	if err := writeStaleDoc(b.path, staleDoc{Version: staleVersion, Snoozes: kept}); err != nil {
		return StaleSnooze{}, err
	}
	return pick, nil
}

func readStaleDoc(path string) (staleDoc, error) {
	data, err := readCapped(path)
	if err != nil {
		if errors.Is(err, errOversize) {
			return staleDoc{}, fmt.Errorf("%w: snooze file over %d bytes", ErrTooLarge, MaxFileBytes)
		}
		return staleDoc{}, err
	}
	var doc staleDoc
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&doc); err != nil {
		return staleDoc{}, fmt.Errorf("%w: snooze file is not valid JSON: %v", ErrInvalid, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return staleDoc{}, fmt.Errorf("%w: unexpected data after the snooze document", ErrInvalid)
	}
	if doc.Version > staleVersion {
		return staleDoc{}, fmt.Errorf("%w (snooze version %d)", ErrUnsupportedVersion, doc.Version)
	}
	return doc, nil
}

func writeStaleDoc(path string, doc staleDoc) error {
	data, err := json.MarshalIndent(doc, "", " ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".places-stale-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
	}
	return err
}
