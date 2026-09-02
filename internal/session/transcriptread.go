package session

// THE SESSION OWNS THE READING OF ITS OWN RECORD.
//
// A conversation's own surface never touches the file: it asks the agent holding
// it for [Agent.Transcript] and draws what comes back. A page watching ANOTHER
// agent's work had no such door — the record it wants belongs to a node whose
// agent is somebody else's, and there was nothing to ask — so the view grew a
// second reader of its own: its own line struct, its own scanner, its own idea
// of which roles exist, and hand-copied duplicates of two display caps this
// package does not export.
//
// That is one record with two readings, and the two drifted exactly where a
// second reading always drifts: the view's knew `user`, `assistant` and two
// spellings of thinking and nothing else, so a line the session had MARKED — a
// correction typed into running work, a sentence the session itself wrote —
// reopened as an ordinary question from the person. Yesterday's correction read
// as a second brief.
//
// So the doors below. They are the same rebuild a resume does and the same
// shaping [Agent.Transcript] answers with, pointed at a file nobody has open:
//
//	ONE RECORD, ONE READING. A page and a conversation shape the same journal
//	through the same code, so a mark this package writes cannot be a mark a
//	surface fails to know about.
//
// ── WHAT THEY DELIBERATELY DO NOT DO ───────────────────────────────────────
//
// THEY DO NOT REPAIR. [replaySessionFile] mends a transcript that is about to be
// SENT — an unanswered trailing batch is a 400 on every request after it — and
// that repair would delete the one shape a page opened on live work exists to
// draw: the call the record names with no result under it yet. A reader that
// only draws wants the record as written ([DisplayEntry.Answered]).
//
// THEY DO NOT CLAIM THE FILE. No lock is taken and nothing is written: the
// journal belongs to whoever is filling it in, and a page is a spectator.
//
// THEY DO NOT FAIL. A path that is empty, missing, unreadable or half-written is
// not an error and answers nothing — the record is EVIDENCE, not a prerequisite,
// and a page that refused to open because a file was not there yet would refuse
// to show the live work beside it as well.

import (
	"bytes"
	"os"
	"strings"
)

// ReadTranscript is one session file, at rest, as display entries — the shape
// [Agent.Transcript] answers with, for a record this process does not hold open.
func ReadTranscript(path string) []DisplayEntry {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	return transcriptFrom(readJournal(file, path))
}

// ReadTranscriptBytes is the same reading of a record that arrived as BYTES
// rather than as a path: the tail a hosted page is handed over the wire
// ([TaskRecord.Journal]).
//
// A TAIL OPENS MID-FILE, and every part of the reading already survives that: a
// half line is skipped, a result whose call is above the cut is a message no
// entry claims, and a file with no header is a file whose format version nobody
// declared.
func ReadTranscriptBytes(data []byte) []DisplayEntry {
	if len(data) == 0 {
		return nil
	}
	return transcriptFrom(readJournal(bytes.NewReader(data), ""))
}

// transcriptFrom is the shaping, over what the scan rebuilt.
//
// The indexes go into a DETACHED journal — a [sessionFile] with no file behind
// it — because that is what the shaping asks for a message's pictures, its
// session-authored mark and its steer mark, and the marks are on the LINES the
// scan just consumed. Nothing here can write: the door was never given a handle.
//
// An error is not a reason to draw nothing. The scan stops at the first
// unreadable line and hands back everything above it, which is the record as far
// as it is legible, and that is what a page shows.
func transcriptFrom(replayed replayedSession, _ error) []DisplayEntry {
	return shapeEntries(replayed.messages, &sessionFile{
		images:    replayed.images,
		notes:     replayed.notes,
		replyTags: replayed.replyTags,
		steers:    replayed.steers,
	})
}
