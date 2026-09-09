package tui3

import (
	"bytes"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// A task accepts steering before its worker journals it. Keep that receipt in
// this view until the corresponding occurrence appears in the journal. Counting
// occurrences matters: saying the same thing twice really sends it twice.
type roomSteerEcho struct {
	words      string
	after      time.Time
	occurrence int
	entry      entry
}

func (r *taskRoom) keepSteerEcho(words string, e entry) {
	n := 1
	for _, pending := range r.pendingSteers {
		if pending.words == words {
			n++
		}
	}
	r.pendingSteers = append(r.pendingSteers, roomSteerEcho{words, r.lastSteerAt, n, e})
}

func (a *app) refreshRoomRecord(journal []byte) {
	r := a.room
	if bytes.Equal(r.journal, journal) {
		return
	}
	entries, turn := a.roomRecord(session.ReadTranscriptBytes(journal), roomTail)
	// Tool ids survive growing output. Text identifies the instruction and
	// other settled blocks. Queue equal keys so repeated prose stays distinct.
	type key struct {
		kind     entryKind
		id, text string
	}
	identity := func(e entry) key {
		if e.callID != "" {
			return key{kind: e.kind, id: e.callID}
		}
		return key{kind: e.kind, text: e.text}
	}
	previous := make(map[key][]entry)
	for _, e := range r.entries {
		k := identity(e)
		previous[k] = append(previous[k], e)
	}
	steers := make(map[string][]time.Time)
	for i := range entries {
		e := &entries[i]
		k := identity(*e)
		if old := previous[k]; len(old) > 0 {
			e.open, e.full = old[0].open, old[0].full
			previous[k] = old[1:]
		}
		if e.steer != nil {
			steers[e.steer.words] = append(steers[e.steer.words], e.steer.at)
			if e.steer.at.After(r.lastSteerAt) {
				r.lastSteerAt = e.steer.at
			}
		}
	}
	pending := r.pendingSteers[:0]
	for _, echo := range r.pendingSteers {
		seen := 0
		for _, at := range steers[echo.words] {
			if at.After(echo.after) {
				seen++
			}
		}
		if seen < echo.occurrence {
			entries = append(entries, echo.entry)
			pending = append(pending, echo)
		}
	}
	r.pendingSteers = pending
	r.entries, r.turn = entries, turn
	r.journal = bytes.Clone(journal)
}
