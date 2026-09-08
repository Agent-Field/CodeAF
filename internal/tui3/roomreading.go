package tui3

import (
	"crypto/sha256"
	"path/filepath"
)

// Reading positions belong to a task in its owning conversation, never to a
// task number alone. Closing a page still releases its reader; this cache keeps
// only fingerprints and display choices, not transcripts or subscriptions.
const roomReadingLimit = 64
const roomReadingEntryLimit = 2 * roomTail

type roomReadingKey struct {
	host, file string
	task       uint64
}

type roomReadingEntry struct {
	kind       entryKind
	hash       [32]byte
	occurrence int
}

type roomReadingFlags struct {
	open, full, unfolded, work, caption, hasCaption bool
}

type roomReading struct {
	workStyle foldStyle
	follow    bool
	anchor    roomReadingEntry
	hasAnchor bool
	rowDelta  int
	flags     map[roomReadingEntry]roomReadingFlags
}

func (a *app) roomReadingKey() (roomReadingKey, bool) {
	r := a.room
	if r == nil || r.orch != nil {
		return roomReadingKey{}, false
	}
	file := a.file
	if r.guest != nil {
		file = r.guest.session
	}
	if file == "" {
		return roomReadingKey{}, false
	}
	return roomReadingKey{a.host, filepath.Clean(file), r.id}, true
}

// Tool ids survive output growing while a page is closed. Other blocks use
// their settled words. Occurrences keep repeated identical prose distinct.
func readingEntries(entries []entry) []roomReadingEntry {
	keys := make([]roomReadingEntry, len(entries))
	seen := make(map[roomReadingEntry]int)
	for i, e := range entries {
		text := e.text
		if e.callID != "" {
			text = e.callID
		}
		k := roomReadingEntry{kind: e.kind, hash: sha256.Sum256([]byte(text))}
		n := seen[k]
		seen[k]++
		k.occurrence = n
		keys[i] = k
	}
	return keys
}

func (a *app) rememberRoomReading() {
	key, ok := a.roomReadingKey()
	r := a.room
	// Closing a loading or failed view must not replace its previous bookmark
	// with the loading line. The next successful read gets another chance.
	if !ok || r.loading || r.readFailed {
		return
	}
	rows := a.roomRows(a.bodyWidth())
	keys := readingEntries(r.entries)
	d := r.deck()
	folds := a.deckFolds(d)
	saved := roomReading{workStyle: d.lens.foldPast, follow: r.stick, flags: map[roomReadingEntry]roomReadingFlags{}}
	for i := len(r.entries) - 1; i >= 0; i-- {
		e := r.entries[i]
		caption, hasCaption := r.capOpen[i]
		// Fold keys are phase ordinals or turn numbers, never entry indexes.
		// Persist the start block so a shifted journal can derive its new key.
		fold, startsFold := folds[i]
		work := startsFold && r.workOpen[fold.key]
		flags := roomReadingFlags{e.open, e.full, r.unfolded[e.turn], work, caption, hasCaption}
		if flags == (roomReadingFlags{}) {
			continue
		}
		// The most recent expanded blocks are the useful ones when a very long
		// live task exceeds the journal tail a reopened page can read.
		if len(saved.flags) == roomReadingEntryLimit {
			break
		}
		saved.flags[keys[i]] = flags
	}
	if !r.stick {
		top := a.roomOffsetFor(len(rows), a.viewHeight())
		for i := top; i < len(rows); i++ {
			index := rows[i].entry
			if index < 0 || index >= len(keys) {
				continue
			}
			start := i
			for start > 0 && rows[start-1].entry == index {
				start--
			}
			saved.anchor, saved.hasAnchor, saved.rowDelta = keys[index], true, top-start
			break
		}
	}
	if a.roomReadings == nil {
		a.roomReadings = map[roomReadingKey]roomReading{}
	}
	for i, old := range a.roomReadingOrder {
		if old == key {
			a.roomReadingOrder = append(a.roomReadingOrder[:i], a.roomReadingOrder[i+1:]...)
			break
		}
	}
	if len(a.roomReadingOrder) == roomReadingLimit {
		delete(a.roomReadings, a.roomReadingOrder[0])
		a.roomReadingOrder = a.roomReadingOrder[1:]
	}
	a.roomReadings[key] = saved
	a.roomReadingOrder = append(a.roomReadingOrder, key)
}

// Restoration waits until the owner and transcript are known. Hosted pages
// initially paint a loading line; consuming the bookmark there would lose it.
func (a *app) restoreRoomReading() *roomReading {
	r := a.room
	if r.readingRestored || r.loading || r.readFailed || r.orch != nil {
		return nil
	}
	r.readingRestored = true
	key, ok := a.roomReadingKey()
	if !ok {
		return nil
	}
	saved, ok := a.roomReadings[key]
	if !ok {
		return nil
	}
	if r.workOpen == nil {
		r.workOpen = map[int]bool{}
	}
	d := r.deck()
	folds := a.deckFolds(d)
	for i, k := range readingEntries(r.entries) {
		flags, found := saved.flags[k]
		if !found {
			continue
		}
		r.entries[i].open, r.entries[i].full = flags.open, flags.full
		if flags.unfolded {
			r.unfolded[r.entries[i].turn] = true
		}
		// A live phase and a completed turn can share a start block while
		// hiding different work. Only the same fold policy inherits expansion.
		if flags.work && saved.workStyle == d.lens.foldPast {
			if fold, startsFold := folds[i]; startsFold {
				r.workOpen[fold.key] = true
			}
		}
		if flags.hasCaption {
			r.capOpen[i] = flags.caption
		}
	}
	r.stick, r.dirty = saved.follow, true
	return &saved
}

func (a *app) restoreRoomAnchor(saved *roomReading, rows []row) {
	if saved == nil || saved.follow {
		return
	}
	r := a.room
	// If the journal tail no longer contains the anchor, start at the oldest
	// retained row. Losing history must never masquerade as choosing live output.
	r.offset = 0
	if !saved.hasAnchor {
		return
	}
	keys := readingEntries(r.entries)
	for i, row := range rows {
		if row.entry < 0 || row.entry >= len(keys) || keys[row.entry] != saved.anchor {
			continue
		}
		end := i + 1
		for end < len(rows) && rows[end].entry == row.entry {
			end++
		}
		r.offset = max(0, min(i+saved.rowDelta, end-1))
		return
	}
}
