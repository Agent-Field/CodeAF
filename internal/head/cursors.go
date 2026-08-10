package head

import "github.com/Agent-Field/aforge-v2/internal/store"

// sessionCursors is the head's resume state: one watermark per room — the
// newest row of that room the head owes nothing for — beside the journal
// position the poll reads forward from.
//
// The split is a correctness fix, not tidying. The head used to hold a single
// journal-wide number for both jobs, and a number that says "everything below
// here is handled" is only true of the room whose reply last moved it: with two
// rooms open, an answer in one carries the mark past the other's unanswered
// message and that message is never read again. The rows are per room because
// the question they answer — has this been answered — is per room. The read
// position stays journal-wide because the journal is one sequence and paging it
// once is cheaper than paging it per room.
type sessionCursors struct {
	// scanned is the newest row the poll has read, in any room. It only ever
	// rises, which is what makes the poll's inner loop terminate.
	scanned int64
	// answered maps session id to the newest row of that session that needs no
	// answer. It is keyed by raw id, the empty one included — messages posted
	// to no room in particular are still a run of rows with a resume point.
	// Its size is the number of rooms ever spoken in, not the number of rows.
	answered map[string]int64
}

func newSessionCursors(scanned int64) *sessionCursors {
	return &sessionCursors{scanned: scanned, answered: make(map[string]int64)}
}

// answeredThrough reports the watermark of one room. A room the head has never
// heard of is answered through nothing, which is the right answer for a room
// whose first message is the one being asked about.
func (c *sessionCursors) answeredThrough(sessionID string) int64 {
	if c == nil {
		return 0
	}
	return c.answered[sessionID]
}

// mark records that one room needs no answer through seq, and never lowers what
// is already recorded.
//
// It deliberately leaves the read position alone. A turn can absorb rows the
// poll has not reached yet — the mid-turn watch folds arrivals in — so its
// answer covers a row above the page it came from. Letting that carry the read
// position with it is the old bug in miniature: the rows it would skip over
// belong to other rooms, and they would go unread. The next page reads them
// again and this room's own watermark skips its own.
func (c *sessionCursors) mark(sessionID string, seq int64) {
	if c == nil || seq <= 0 {
		return
	}
	if c.answered == nil {
		c.answered = make(map[string]int64)
	}
	if seq > c.answered[sessionID] {
		c.answered[sessionID] = seq
	}
}

// read records that the poll has walked past one row. This is the only thing
// that moves the journal position, and it moves it one read row at a time.
func (c *sessionCursors) read(seq int64) {
	if c == nil {
		return
	}
	if seq > c.scanned {
		c.scanned = seq
	}
}

// resumeCursors turns the store's per-room resume points into the head's
// starting state. The read position is the oldest of them, because a room that
// was left mid-sentence is resumed from where it was left even when louder
// rooms have written since; every row above it that some room has already
// answered is skipped by that room's own watermark on the way past.
func resumeCursors(answered map[string]int64) *sessionCursors {
	cursors := newSessionCursors(0)
	if len(answered) == 0 {
		return cursors
	}
	scanned := int64(-1)
	for sessionID, seq := range answered {
		if seq > 0 {
			cursors.answered[sessionID] = seq
		}
		if scanned < 0 || seq < scanned {
			scanned = seq
		}
	}
	if scanned > 0 {
		cursors.scanned = scanned
	}
	return cursors
}

// handled reports whether one row is below its own room's watermark. Rows the
// head never answers — a job's narration, a worker's mail — are still walked
// past and still raise the read position; this is only about what it owes.
func (c *sessionCursors) handled(message store.Message) bool {
	return message.Seq <= c.answeredThrough(message.SessionID)
}
