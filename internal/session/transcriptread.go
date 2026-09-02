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
// THEY DO NOT FAIL. A path that is empty or missing is not an error and answers
// nothing — the record is EVIDENCE, not a prerequisite, and a page that refused
// to open because a file was not there yet would refuse to show the live work
// beside it as well. A file with a line this build cannot read answers with
// EVERYTHING ABOVE THAT LINE and says which line it was ([Record.UnreadFrom]),
// which is the same answer a resume gets from the same file.
//
// THEY DO NOT OPEN A PICTURE'S FILE. The parts come back as references to where
// the bytes were ([journalPart.reference]) and never as the bytes: these
// messages are shaped for display and thrown away, a room re-reads on every
// open and a run page four times a second, and on a hosted record the paths
// belong to another machine entirely.

import (
	"bytes"
	"os"
	"strings"
)

// Record is one session file read for display: the conversation the model still
// carries, and the region a compaction pass edited away.
//
// IT IS THE SHAPE THE CONVERSATION ALREADY HAS, said once for a file instead of
// for an open agent: `Entries` is [Agent.Transcript] and `Earlier`/`Floor` are
// [EarlierHistory], with the same contract between them — THE RECORD, TOLD ONCE
// AND WHOLE, IS `Earlier` FOLLOWED BY `Entries[Floor:]`.
//
// The region is here rather than left out because a page that dropped it would
// be showing a person the pass's own shortened copy of their work as though it
// were the work: the calls above the marker come back as stubs, and the prose
// comes back as one folded line. THE LENS MAY LOWER SALIENCE; IT MAY NOT DROP A
// FACT (docs/design/lens/DESIGN.md).
//
// Earlier is empty, and Floor zero, for every record that was never compacted —
// which is nearly all of them — and for one compacted by a build that did not
// write the window's length, where the region cannot be placed and is dropped
// rather than drawn twice ([replayedSession.earlier]).
type Record struct {
	Entries []DisplayEntry
	Earlier []DisplayEntry
	Floor   int
	// UnreadFrom is the 1-based line of the file this reading could not get past,
	// and zero for a file read to its end. Everything above it is in Entries.
	UnreadFrom int
}

// ReadTranscript is one session file, at rest, as display entries — the shape
// [Agent.Transcript] and [Agent.EarlierHistory] answer with, for a record this
// process does not hold open.
func ReadTranscript(path string) Record {
	if strings.TrimSpace(path) == "" {
		return Record{}
	}
	file, err := os.Open(path)
	if err != nil {
		return Record{}
	}
	defer file.Close()
	return transcriptFrom(readJournal(file, path, false))
}

// ReadTranscriptBytes is the same reading of a record that arrived as BYTES
// rather than as a path: the tail a hosted page is handed over the wire
// ([TaskRecord.Journal]).
//
// A TAIL OPENS MID-FILE, and every part of the reading already survives that: a
// half line is skipped, a result whose call is above the cut is a message no
// entry claims, and a file with no header is a file whose format version nobody
// declared.
func ReadTranscriptBytes(data []byte) Record {
	if len(data) == 0 {
		return Record{}
	}
	return transcriptFrom(readJournal(bytes.NewReader(data), "", false))
}

// transcriptFrom is the shaping, over what the scan rebuilt.
//
// The indexes go into a DETACHED journal — a [sessionFile] with no file behind
// it — because that is what the shaping asks for a message's pictures, its
// session-authored mark and its steer mark, and the marks are on the LINES the
// scan just consumed. Nothing here can write: the door was never given a handle.
//
// An error is not a reason to draw nothing. The only one this can carry is a
// file written by a newer aforge, and what came back with it is every line this
// build did understand — which is the record as far as it is legible, and that
// is what a page shows.
//
// THE FLOOR IS COUNTED BY DOING THE SHAPING, exactly as the resume path counts
// it (agent.go): the journal counts MESSAGES and a surface indexes ENTRIES, and
// a second rule for how many entries a message makes is a rule that can disagree
// with [shapeEntries].
func transcriptFrom(replayed replayedSession, _ error) Record {
	journal := &sessionFile{
		images:    replayed.images,
		notes:     replayed.notes,
		replyTags: replayed.replyTags,
		steers:    replayed.steers,
	}
	overlap := replayed.overlap
	if overlap > len(replayed.messages) {
		overlap = len(replayed.messages)
	}
	return Record{
		Entries:    shapeEntries(replayed.messages, journal),
		Earlier:    shapeEntries(replayed.earlier, journal),
		Floor:      len(shapeEntries(replayed.messages[:overlap], journal)),
		UnreadFrom: replayed.unread,
	}
}
