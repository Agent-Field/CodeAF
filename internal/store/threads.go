package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Threads are sessions, admitted (chat-simplify Part 5). The primitive has been
// here all along — a room row, per-room head cursors, delivery routed to the
// room that commissioned the work — and what was missing was the product's
// admission that a room is a WORKING CONVERSATION: alive for days, commissioning
// tasks along the way, taking their results back, resumable after weeks.
//
// This file is the store's half of that admission, and it is deliberately three
// small things rather than a new table:
//
//   - NewSessionID, so there is exactly one generator for a room's name and the
//     head can mint one without importing the surface that used to own it.
//   - SessionNodes, the read that answers "what did this conversation
//     commission", which nothing could answer before: nodes.session_id has
//     carried the commissioning room since splices were journaled, and every
//     caller filtered it in Go after reading the whole graph.
//   - OpenThreads, the one reader behind both the head's alive glance and the
//     surface's board home. Per the one-renderer law the QUERY lives here and
//     the wording lives in whoever is speaking, so the two halves of the product
//     cannot come to disagree about which conversations are still alive.
//
// Nothing here is a lifecycle. A thread never closes: it goes quiet, sinks out
// of the open list, and stays reachable forever by recall.

// NewSessionID mints a fresh room id. It is random rather than sequential
// because a room id is a name, not an order: two windows opened in the same
// second must not collide, and nothing downstream reads it as a number.
//
// It lives in the store because a room is a store concept and both the surface
// that opens one by key press and the head that opens one by splitting a
// conversation need the same generator. Two spellings of "a new room's name"
// is how two builds come to disagree about what a room id looks like.
func NewSessionID() string {
	var random [4]byte
	if _, err := rand.Read(random[:]); err == nil {
		return hex.EncodeToString(random[:])
	}
	return fmt.Sprintf("%08x", time.Now().UnixNano())
}

// SessionNodes returns the job roots one conversation commissioned, newest
// first. Roots only: a splice stamps every node it admits with the room, so the
// unfiltered answer would be one row per step and a conversation that
// commissioned three jobs would read as forty.
//
// Folded history is included on purpose. This read answers "what did we set
// going in here, and what came of it", and what came of it is only knowable once
// the job has settled — which is also when the fold packs it away.
func (s *Store) SessionNodes(sessionID string, limit int) ([]Node, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil
	}
	return s.queryNodesLimitOrdered(`WHERE session_id = ? AND parent_id = ?`,
		[]any{sessionID, RootID}, limit, `created_seq DESC, created_order DESC, id`)
}

// ThreadOpenKind names the one unresolved shape that keeps a thread alive. There
// are exactly three, and each is a different party owing the other something.
type ThreadOpenKind string

const (
	// ThreadOpenQuestion is the head waiting on the person: a question was put
	// and nothing came back.
	ThreadOpenQuestion ThreadOpenKind = "question"
	// ThreadOpenDelivery is work that landed in a room the person has not
	// opened since. Nobody is blocked; something is simply unread.
	ThreadOpenDelivery ThreadOpenKind = "delivery"
	// ThreadOpenUnanswered is the person waiting on the head: they spoke last
	// and no reply followed.
	ThreadOpenUnanswered ThreadOpenKind = "unanswered"
)

// ThreadArc is one conversation with something still open in it.
//
// Left is the one line the thread was left at — the words of whichever message
// is the open thing — because a switcher row that says only a name and a time
// cannot tell a reader which conversation they want.
type ThreadArc struct {
	SessionID string
	Title     string
	// LastActive is the room's own activity mark, which is what the list is
	// ordered by; Since is when the OPEN thing started, which is what "parked
	// for two days" measures.
	LastActive time.Time
	Since      time.Time
	Open       ThreadOpenKind
	Left       string
	// UnseenDelivery is the one ornament the product allows: work landed here
	// and no lens has been in the room since. It is separate from Open because
	// a thread can be waiting on an answer AND holding an unread delivery, and
	// the dot is drawn for the second regardless of the first.
	UnseenDelivery bool
}

const (
	// openThreadTailDepth is how far back one room is read to find its open
	// thing. The unresolved end of a conversation is at its end; a thread whose
	// last two dozen rows are all settled is a quiet thread, and reading its
	// whole history to say so would make this query cost the journal.
	openThreadTailDepth = 24
	// openThreadScanCap bounds how many rooms are examined. Rooms are read
	// newest-active first, so this is a bound on cost and not on truth: a
	// conversation nobody has touched in months has sunk, which is exactly what
	// a thread with no lifecycle does instead of closing.
	openThreadScanCap = 60
	// openThreadLeftBytes keeps the left-at line to a line.
	openThreadLeftBytes = 160
	// OpenThreadsDefaultLimit is what a caller that does not care asks for.
	OpenThreadsDefaultLimit = 8
)

// OpenThreads returns the conversations with an unresolved arc, newest active
// first. It is the one reader behind the head's alive glance and the surface's
// board home, so the two can never disagree about which threads are alive.
//
// A thread is alive when one of three things is true of its end: the head asked
// and nothing came back, the person spoke and nothing answered, or work landed
// and nobody has been in the room since. Everything else has sunk.
func (s *Store) OpenThreads(limit int) ([]ThreadArc, error) {
	if limit <= 0 {
		limit = OpenThreadsDefaultLimit
	}
	sessions, err := s.Sessions()
	if err != nil {
		return nil, fmt.Errorf("read open threads: %w", err)
	}
	seen, err := s.sessionSeenSeqs()
	if err != nil {
		return nil, fmt.Errorf("read open threads: %w", err)
	}
	arcs := make([]ThreadArc, 0, limit)
	for index, session := range sessions {
		if index >= openThreadScanCap || len(arcs) >= limit {
			break
		}
		tail, err := s.MessageTail(session.ID, openThreadTailDepth)
		if err != nil {
			return nil, fmt.Errorf("read open threads: %w", err)
		}
		arc, open := threadArcOf(session, tail, seen[session.ID])
		if !open {
			continue
		}
		arcs = append(arcs, arc)
	}
	return arcs, nil
}

// ThreadIndex returns EVERY conversation, newest active first, with its open
// arc filled in when it has one.
//
// IT IS OpenThreads WITHOUT THE FILTER, AND THE DIFFERENCE IS A PRODUCT BUG THAT
// SHIPPED. [Store.OpenThreads] answers "what is still alive", which is the right
// question for the head's alive glance and for the nudge — and the WRONG one for
// a switcher. A switcher is an index: it exists so a person can walk back into a
// conversation, and the conversations a person most wants to walk back into are
// usually the ones that were finished properly. Driving the switcher from the
// open-loops query meant a store holding three real sessions drew a list of
// none — every answered conversation was invisible, the only row was the
// `new thread` door, and enter on it abandoned the thread the reader was
// standing in. That is the whole of "I cannot reach the chats feature".
//
// So OPEN IS A DECORATION HERE, NEVER A FILTER. A row's [ThreadArc.Open] and
// [ThreadArc.UnseenDelivery] still say what is unresolved in it; nothing is
// dropped for being settled. Callers that genuinely want the live set keep
// asking OpenThreads, which is unchanged.
//
// Cost is OpenThreads': one tail read per room, bounded by the same scan cap,
// paid when a door opens rather than on a cadence.
func (s *Store) ThreadIndex(limit int) ([]ThreadArc, error) {
	if limit <= 0 {
		limit = OpenThreadsDefaultLimit
	}
	sessions, err := s.Sessions()
	if err != nil {
		return nil, fmt.Errorf("read thread index: %w", err)
	}
	seen, err := s.sessionSeenSeqs()
	if err != nil {
		return nil, fmt.Errorf("read thread index: %w", err)
	}
	arcs := make([]ThreadArc, 0, limit)
	for index, session := range sessions {
		if index >= openThreadScanCap || len(arcs) >= limit {
			break
		}
		tail, err := s.MessageTail(session.ID, openThreadTailDepth)
		if err != nil {
			return nil, fmt.Errorf("read thread index: %w", err)
		}
		// The second return is deliberately discarded: it says whether the arc
		// is OPEN, and this reader lists a room whatever the answer is. The arc
		// itself is fully populated either way — title, activity, left-at line
		// and the unseen mark — because threadArcOf fills those before it
		// decides anything about liveness.
		arc, _ := threadArcOf(session, tail, seen[session.ID])
		arcs = append(arcs, arc)
	}
	return arcs, nil
}

// ParkedThreads is OpenThreads narrowed to the ones that have been sitting on
// their open thing longer than idle. It is the nudge's query: a thread parked
// since this morning is an ordinary working conversation, and one parked since
// last week is something the person has genuinely lost track of.
func (s *Store) ParkedThreads(now time.Time, idle time.Duration, limit int) ([]ThreadArc, error) {
	arcs, err := s.OpenThreads(openThreadScanCap)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = OpenThreadsDefaultLimit
	}
	parked := make([]ThreadArc, 0, limit)
	for _, arc := range arcs {
		if len(parked) >= limit {
			break
		}
		if arc.Since.IsZero() || now.Sub(arc.Since) < idle {
			continue
		}
		parked = append(parked, arc)
	}
	return parked, nil
}

// SessionSeenSeq is one room's attention watermark: the newest moment a lens was
// in it. Zero means nobody ever has been, which is the honest answer and the one
// that makes everything in the room unread.
func (s *Store) SessionSeenSeq(sessionID string) (int64, error) {
	watermarks, err := s.sessionSeenSeqs()
	if err != nil {
		return 0, err
	}
	return watermarks[strings.TrimSpace(sessionID)], nil
}

// ThreadArcOf is the arc reader with the room's rows passed in. OpenThreads is
// this function over every room; a caller that already holds a BOUNDED window of
// one room — the head building a re-entry brief, which must read the thread as it
// stood before the message it is grounding — asks it directly, so both answers
// come from one piece of judgment about what "still open" means.
//
// tail is oldest-first, which is what MessageTail returns.
func ThreadArcOf(session Session, tail []Message, seenSeq int64) (ThreadArc, bool) {
	return threadArcOf(session, tail, seenSeq)
}

// threadArcOf reads one room's end and says what, if anything, is still open in
// it. tail is oldest-first, which is what MessageTail returns.
func threadArcOf(session Session, tail []Message, seenSeq int64) (ThreadArc, bool) {
	arc := ThreadArc{
		SessionID:  session.ID,
		Title:      strings.TrimSpace(session.Title),
		LastActive: session.LastActive,
	}
	var lastUser, lastAgent, lastQuestion, lastDelivery Message
	for _, message := range tail {
		if strings.TrimSpace(message.Body) == "" {
			continue
		}
		switch message.Role {
		case RoleUser:
			lastUser = message
		case RoleAgent:
			lastAgent = message
			if message.QuestionSeq != 0 || len(message.Options) > 0 {
				lastQuestion = message
			}
		case RoleSystem:
			// A delivery is a system row anchored to the work that produced it,
			// which is the same test the head's own absorb lane uses.
			if strings.TrimSpace(message.NodeID) != "" {
				lastDelivery = message
			}
		}
	}
	arc.UnseenDelivery = lastDelivery.Seq > 0 && lastDelivery.Seq > seenSeq

	switch {
	case lastQuestion.Seq > lastUser.Seq && lastQuestion.Seq == lastAgent.Seq:
		arc.Open, arc.Left, arc.Since = ThreadOpenQuestion, lastQuestion.Body, lastQuestion.Time
	case lastUser.Seq > lastAgent.Seq && lastUser.Seq > 0:
		arc.Open, arc.Left, arc.Since = ThreadOpenUnanswered, lastUser.Body, lastUser.Time
	case arc.UnseenDelivery && lastDelivery.Seq > lastUser.Seq:
		arc.Open, arc.Left, arc.Since = ThreadOpenDelivery, lastDelivery.Body, lastDelivery.Time
	default:
		// SETTLED, AND STILL A REAL THREAD. The arc is filled in and returned
		// with open=false rather than zeroed, because a settled conversation is
		// exactly what [Store.ThreadIndex] exists to list — and a switcher row
		// still needs the title, the activity mark and a line to recognise the
		// conversation by. Callers asking "what is still alive" (OpenThreads,
		// the head's brief) test the boolean and drop it, which is what they
		// already did.
		//
		// The line is the AGENT's last word. "Left at" is what the conversation
		// was saying when you walked away; quoting the reader's own last message
		// back at them would make the switcher a list of things they already
		// know they said. A thread whose agent has never spoken has no line, and
		// absent is honest.
		arc.Left, arc.Since = lastAgent.Body, lastAgent.Time
		if arc.Since.IsZero() {
			arc.Since = session.LastActive
		}
		arc.Left = clipThreadLine(arc.Left)
		return arc, false
	}
	arc.Left = clipThreadLine(arc.Left)
	if arc.Since.IsZero() {
		arc.Since = session.LastActive
	}
	return arc, true
}

// clipThreadLine is the left-at line: one line, bounded, with the newlines that
// would break a list row taken out.
func clipThreadLine(body string) string {
	line := strings.TrimSpace(body)
	if index := strings.IndexByte(line, '\n'); index >= 0 {
		line = strings.TrimSpace(line[:index])
	}
	if len(line) > openThreadLeftBytes {
		line = strings.TrimSpace(line[:openThreadLeftBytes]) + "…"
	}
	return line
}

// sessionSeenSeqs is the newest attention watermark per room, in one query.
//
// Seen is journal-only and global — one person's attention stream across every
// surface — so "has anybody been in this room since" is the newest seen_touched
// event that NAMED the room. A room nobody has ever attached to has no
// watermark, which reads as zero and makes every delivery in it unseen; that is
// the honest answer, because nobody has been there.
func (s *Store) sessionSeenSeqs() (map[string]int64, error) {
	rows, err := s.db.Query(`
		SELECT json_extract(payload, '$.session_id') AS sid, MAX(seq)
		FROM events WHERE kind = ? GROUP BY sid`, EventSeenTouched)
	if err != nil {
		return nil, fmt.Errorf("read seen watermarks: %w", err)
	}
	defer rows.Close()
	watermarks := make(map[string]int64)
	for rows.Next() {
		var sessionID sql.NullString
		var seq int64
		if err := rows.Scan(&sessionID, &seq); err != nil {
			return nil, fmt.Errorf("read seen watermarks: %w", err)
		}
		if id := strings.TrimSpace(sessionID.String); id != "" {
			watermarks[id] = seq
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read seen watermarks: %w", err)
	}
	return watermarks, nil
}
