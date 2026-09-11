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
// beside it as well.
//
// BUT A SHORT READING IS NEVER A SILENT ONE. Two files come back with less than
// they hold: one whose line this build cannot get past, which answers with
// EVERYTHING ABOVE THAT LINE, and one written by a NEWER AFORGE, which a resume
// refuses outright and which answers here with nothing at all. Returning either
// as an ordinary short record would be the page saying, in the only way a page
// can, that this is all the work there ever was. So the reading carries what it
// could not do ([Record.Unreadable]) and the page draws it.
//
// THEY DO NOT OPEN A PICTURE'S FILE. The parts come back as references to where
// the bytes were ([journalPart.reference]) and never as the bytes: these
// messages are shaped for display and thrown away, a room re-reads on every
// open and a run page four times a second, and on a hosted record the paths
// belong to another machine entirely.

import (
	"bytes"
	"errors"
	"os"
	"strconv"
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
	// Unreadable is what a page must SAY about this reading, and "" for a record
	// read whole — which is nearly all of them.
	//
	// IT IS A FINISHED SENTENCE and not a code or a line number, for the reason
	// [SteerMark.Landing] is one: the fact belongs to whoever did the reading,
	// and a surface composing the sentence from parts is a second place the
	// answer can be wrong. There is at most one, because a reading either stopped
	// AT a line or was refused the whole file, never both.
	Unreadable string
	// Requests is what the record's own request lines say about the work it
	// holds — the other half of a page watching a node it has no lane to
	// ([RequestBooks]).
	Requests RequestBooks
}

// RequestBooks is what a journal's `call` lines — ONE PER MODEL REQUEST the
// conversation made, banked beside the seal that sums them ([Agent.addUsage])
// — say about the work in the bytes that were read.
//
// IT EXISTS FOR A PAGE WITH NO LANE. A surface watching a node on this machine
// hears its steps as they end; a surface watching one on ANOTHER machine is
// handed a bounded tail of its journal and nothing else, and every live figure
// it draws has to come out of those bytes. The request lines are the only place
// the provider's own counts are written down per request, so they are read by
// the same scan that rebuilds the transcript — one record, one reading — rather
// than by a second parser of the same file.
//
// ONLY THE CONVERSATION'S OWN REQUESTS ARE COUNTED. An errand's call — a title,
// a memory reflex, a review — is written with its role on it and is not the
// work's writing ([Agent.journalRoleCall]); counting it would make a page's ↓
// jump for a sentence nobody on the page wrote.
//
// THE READING IS BOUNDED BY WHAT WAS READ. A tail opens mid-file, so Recent is
// the requests in the tail and not the whole run above it. Latest is always
// there when any request is: the newest line is the last one written.
type RequestBooks struct {
	// Latest is the prompt the newest request sent, whole — what that request
	// weighed. The providers aforge speaks to count cached prompt tokens INSIDE
	// the prompt figure (an OpenAI-shaped usage block, which is what OpenRouter
	// returns); a line whose cached share is LARGER than its prompt can only
	// have been counted the other way, and has the two added back together.
	Latest int
	// Recent is the newest requests in the reading, oldest first, at most
	// [requestsKept] of them. It is a LIST rather than a sum because a reader
	// of successive tails needs to tell the requests it has already counted
	// from the ones that are new: the window slides as the file grows, and a sum
	// over it would shrink every time an old request fell off the top.
	Recent []RequestLine
}

// RequestLine is one request as its line spells it: what it sent and what came
// back.
type RequestLine struct {
	Input, Output int
}

// requestsKept bounds [RequestBooks.Recent]. A page reads a new tail four times
// a second and a node makes a request every few seconds, so the overlap between
// two readings is never more than a handful; the bound is for the whole-file
// reading a resume makes, which walks every request the conversation ever made
// and needs none of them.
const requestsKept = 64

// observe folds one request line in.
func (b *RequestBooks) observe(call journalCall) {
	if strings.TrimSpace(call.Role) != "" {
		return
	}
	prompt := call.Input
	if call.CacheRead > prompt {
		prompt += call.CacheRead
	}
	if prompt > 0 {
		b.Latest = prompt
	}
	if len(b.Recent) == requestsKept {
		b.Recent = append(b.Recent[:0], b.Recent[1:]...)
	}
	b.Recent = append(b.Recent, RequestLine{Input: prompt, Output: call.Output})
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
// AN ERROR IS A THING TO SAY, NOT A REASON TO SAY NOTHING. The scan refuses a
// file written by a newer aforge — a build reading it anyway would drop every
// entry type and field it does not know, in silence, and show a shortened copy
// of somebody's work as though it were the work — and hands back nothing, so the
// page draws that sentence and no entries, which is the truth about the file.
//
// IT IS MATCHED, NOT ASSUMED. That refusal is the only one the scan has today,
// and reading every error as that one would make the day a second is added the
// day this starts telling people to upgrade a build that is already fine. The
// sentinel says which, and anything else gets the sentence that is true of all
// of them.
//
// THE FLOOR IS COUNTED BY DOING THE SHAPING, exactly as the resume path counts
// it (agent.go): the journal counts MESSAGES and a surface indexes ENTRIES, and
// a second rule for how many entries a message makes is a rule that can disagree
// with [shapeEntries].
func transcriptFrom(replayed replayedSession, err error) Record {
	if err != nil {
		return Record{Unreadable: unreadRefusal(err)}
	}
	journal := &sessionFile{
		images:    replayed.images,
		notes:     replayed.notes,
		replyTags: replayed.replyTags,
		delivered: replayed.delivered,
		steers:    replayed.steers,
		captions:  replayed.captions,
		tooks:     replayed.tooks,
	}
	overlap := replayed.overlap
	if overlap > len(replayed.messages) {
		overlap = len(replayed.messages)
	}
	return Record{
		Entries:    shapeEntries(replayed.messages, journal),
		Earlier:    shapeEntries(replayed.earlier, journal),
		Floor:      len(shapeEntries(replayed.messages[:overlap], journal)),
		Unreadable: unreadPastLine(replayed.unread),
		Requests:   replayed.requests,
	}
}

// ── WHAT A SHORT READING SAYS ───────────────────────────────────────────────
//
// Both sentences STATE BOTH HALVES: what this build could not do, and what is
// on the page in spite of it. "Something went wrong" is not an answer anybody
// can act on, and a row that only named the trouble would leave a person
// guessing whether the work above it is real.

// unreadNewerWord is a file this build has no business reading. It names the
// remedy by naming the cause: the file is not damaged and nothing is lost — a
// newer aforge opens it.
const unreadNewerWord = "this transcript was written by a newer aforge — this build cannot read it"

// unreadRefusedWord is every other way a reading can come back with nothing. It
// promises no cause it has not established: whatever went wrong, what a person
// needs to know first is that the page below is not the work.
const unreadRefusedWord = "this transcript could not be read"

// unreadRefusal is which of the two a refusal earns.
func unreadRefusal(err error) string {
	if errors.Is(err, errNewerFormat) {
		return unreadNewerWord
	}
	return unreadRefusedWord
}

// unreadPastLine is a file that stopped being legible partway down, and "" for
// one read to its end. It names the LINE, which is a place in a file somebody
// can open, rather than a fault somewhere in it.
func unreadPastLine(line int) string {
	if line <= 0 {
		return ""
	}
	return "this transcript could not be read past line " + strconv.Itoa(line) +
		" — everything above it is on this page"
}
