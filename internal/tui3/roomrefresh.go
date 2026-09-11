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
	record := session.ReadTranscriptBytes(journal)
	entries, turn := a.roomRecord(record, roomTail)
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
	r.takeRequests(record.Requests)
}

// takeRequests feeds a page with no lane its live token column, from the one
// place a hosted node's figures are written down: the request lines in the
// journal tail this page was just handed ([session.RequestBooks]).
//
// THIS IS THE DEFECT THE COLUMN HAD ON EVERY HOSTED ROOM. The column's books
// were set by the room's event lane and nothing else ([app.roomEvent]); a room
// on another machine is rebuilt from a bounded journal read instead
// ([app.openFarRoom]) and never hears an event, so ↑ was never known and ↓ fell
// back to the prose on the page — which a journal holds only once a message is
// sealed, so a step that was all tool calls drew no column at all.
//
//   - ↑ is the NEWEST REQUEST as it was sent. It is always inside the tail,
//     because the newest line is the last one written, and it moves once per
//     request — the journal records requests, not the tool results joining
//     between them.
//   - ↓ is the output of every request this page has READ, as the books. The
//     first reading counts what its tail holds, which is a floor where the
//     node's run began above the cut (session.TaskJournalTail); every reading
//     after it adds only the requests that are NEW since the one before
//     ([freshRequests]). A sum over each tail instead would SHRINK whenever an
//     old request slid off the top of the window, and a ↓ walking backwards is
//     the one thing this column must never draw.
func (r *taskRoom) takeRequests(books session.RequestBooks) {
	if books.Latest > 0 {
		r.col.weight = books.Latest
	}
	// A reading with no requests in it teaches nothing, and forgetting what the
	// last one held would count all of it again on the next.
	if len(books.Recent) == 0 {
		return
	}
	added := 0
	for _, call := range freshRequests(r.requests, books.Recent) {
		added += call.Output
	}
	r.requests = append(r.requests[:0], books.Recent...)
	// The mark is this page's writing at the moment of reading, which matters
	// less here than anywhere: the page grows only when the next read lands, and
	// the read that grows it brings its bill with it (tokencol.go).
	r.col.bill(r.col.down+added, r.turnWritten(), r.turn)
}

// freshRequests is the part of `now` that `seen` did not already hold: the two
// are successive readings of one growing file, oldest first, so the new
// requests are whatever follows the newest request `seen` ends on.
//
// THE NEWEST SEEN REQUEST IS FOUND BY ITS SHAPE AND ITS NEIGHBOUR. A line has no
// id, but a conversation's prompt only grows between compactions, so two
// requests with the same prompt and the same answer are rare — and requiring
// the one before it to match too makes a false match need two coincidences in a
// row. Where nothing matches, the window has moved past everything this page
// had counted, and every request in it is new.
func freshRequests(seen, now []session.RequestLine) []session.RequestLine {
	if len(seen) == 0 {
		return now
	}
	last := seen[len(seen)-1]
	for at := len(now) - 1; at >= 0; at-- {
		if now[at] != last {
			continue
		}
		if len(seen) > 1 && at > 0 && now[at-1] != seen[len(seen)-2] {
			continue
		}
		return now[at+1:]
	}
	return now
}
