package tui3

// readingLens keeps the room's receipts, clock and tool tail while using the
// conversation's completed-turn fold once work ends. Running rooms still fold
// settled phases and keep their live frontier visible. The shared turn fold
// preserves person corrections and replies, and leaves unfinished work visible.
func (r *taskRoom) readingLens() lens {
	l := overseerLens
	if r.done {
		l.foldPast = foldTurns
	}
	return l
}

// Phase and turn fold keys have different meanings. Never carry a live phase
// expansion into a completed answer, or vice versa.
func (r *taskRoom) setDone(done bool) {
	if r.done != done {
		r.workOpen = nil
		r.dirty = true
	}
	r.done = done
}
