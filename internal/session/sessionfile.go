package session

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"golang.org/x/sys/unix"
)

// The session file is JSONL: one header line, then one line per COMPLETED
// message and per compaction pass. Append-only and line-oriented, so a crash
// mid-write costs the last line and nothing before it, and a resume is a
// forward read with no rewrite.
//
// Content is flattened to text. A session's messages are text — the tools
// return text and the person types text — and keeping the part array would
// buy fidelity for a shape that does not occur while making every line
// unreadable to the person the transcript is for.
//
// The one part that is not text is an image, and it is journaled as a REFERENCE
// rather than as content: path, digest, media type. A 4MB photo base64'd into a
// JSONL line is how a session file dies — it becomes unreadable to a person, it
// is re-read into memory on every resume, and it grows the file by more than the
// whole conversation around it. The bytes are already on disk at a path this
// machine can read, so the journal writes where they are and what they were, and
// the replay checks the second before trusting the first (see [journalPart]).
//
// The file is locked while it is open. Two aforge processes resuming the same
// path would both replay it and both append, and their lines interleave into
// one transcript that belongs to neither — the last writer's resume reads the
// other's messages as its own. A resume picks the newest file by mtime, so the
// two windows converge on the same path by default rather than by accident.
// openSessionFile therefore takes a non-blocking exclusive flock and the loser
// gets ErrSessionLocked, which names the file so the surface can offer "open it
// where it is" or "start a new one".

const sessionFileVersion = 1

// ErrSessionLocked is what a second open of a live session file returns. Match
// it with errors.Is; the *SessionLockedError it wraps carries the path.
var ErrSessionLocked = errors.New("session file is open in another aforge")

// SessionLockedError names the file another process holds.
type SessionLockedError struct{ Path string }

func (e *SessionLockedError) Error() string {
	return fmt.Sprintf("session file: %s is open in another aforge", e.Path)
}

func (e *SessionLockedError) Unwrap() error { return ErrSessionLocked }

type sessionHeader struct {
	Type      string `json:"type"`
	Version   int    `json:"version"`
	ID        string `json:"id"`
	Cwd       string `json:"cwd"`
	Model     string `json:"model"`
	Timestamp string `json:"timestamp"`
}

type sessionEntry struct {
	Type       string        `json:"type"`
	Role       string        `json:"role,omitempty"`
	Content    string        `json:"content,omitempty"`
	ToolCalls  []ai.ToolCall `json:"toolCalls,omitempty"`
	ToolCallID string        `json:"toolCallId,omitempty"`

	// Parts are the message's non-text content parts as durable references, in
	// the order they sit in the message AFTER its text. Absent on every message
	// that is only words, which is nearly all of them — a reader of an old file
	// and a reader of a new one see the same lines for the same conversation.
	Parts []journalPart `json:"parts,omitempty"`

	// Note marks a user-role line the SESSION wrote rather than the person: a
	// task's completion note and the other news that rides the steering queue
	// (agent.go's [Agent.enqueueNote]). The message is user-role because that is
	// what the model must read it as, and this is the one bit that says who
	// actually said it.
	//
	// It exists for the replay. Without it a resumed conversation draws the
	// harness's own line with the person's "›" in front of it — words in their
	// mouth they never typed, and the opposite of what the live surface does with
	// the same note ([Agent.wakeLocked], tui3's startFollow). It is absent from
	// every file written before it existed, and those lines replay exactly as
	// they always did.
	Note      bool           `json:"note,omitempty"`
	ReplyTags []TaskReplyTag `json:"replyTags,omitempty"`

	// Compaction fields.
	//
	// Summary is LEGACY ONLY: it is the prose a summarizer wrote for every
	// marker up to the pass that stopped calling one, and it is still read so a
	// session compacted last week resumes as itself. Nothing writes it now.
	Summary      string `json:"summary,omitempty"`
	TokensBefore int    `json:"tokensBefore,omitempty"`

	// Stubbed and Folded are what the current pass did (loop.go): how many tool
	// results became pointers to their own bytes, and how many assistant
	// messages went into one marker line.
	//
	// They are the RECORD and never the instruction. A modern marker rebuilds
	// nothing by itself — the pass re-journals the whole rebuilt window behind
	// it, so replay reads the stubs and the fold marker as ordinary message
	// lines and reconstructs the transcript verbatim rather than from counts.
	Stubbed int `json:"stubbed,omitempty"`
	Folded  int `json:"folded,omitempty"`

	// Window is HOW MANY MESSAGE LINES THE PASS RE-JOURNALED BEHIND THIS MARKER
	// — the length of the rebuilt window [sessionFile.appendCompaction] writes
	// out after it.
	//
	// It is the one thing a reader cannot work out for itself, and it is what
	// makes the conversation above a marker readable. The lines above are the
	// original ones and the lines below are the pass's rewritten copy OF THE SAME
	// CONVERSATION, so a reader that showed both would draw the whole session
	// twice; it needs to know where the copy ends and the conversation carries on.
	// Nothing in the file says that but this number.
	//
	// ABSENT MEANS UNKNOWN, not zero. Every marker written before this field
	// existed — and every legacy marker, whose kept tail was re-journaled with no
	// count either — leaves the region above it unplaceable, and a reader must
	// then decline to offer it rather than guess (see [compactionOverlap]). It is
	// deliberately NOT derived from Stubbed and Folded: those are the RECORD of
	// what the pass did, nothing rebuilds from them, and a length derived from a
	// count is a length that drifts the day the pass changes.
	Window int `json:"window,omitempty"`

	// Dropped is how many messages a rewind removed (rewind.go). It is a COUNT
	// rather than a cut position because the file is append-only and positions
	// in it are not positions in the replayed transcript: a compaction marker
	// earlier in the file collapses everything before it into one message. A
	// count is applied to whatever the replay is holding when it reaches the
	// line, which is exactly the list the rewind was taken against.
	Dropped int `json:"dropped,omitempty"`

	// Title is the session's name (title.go). It is its own line rather than a
	// header field because the header is written ONCE, when the file is
	// created, and the name is not known until the first turn has been
	// answered. A line is also how a name can be rewritten later without any
	// reader having to rewrite the file: the replay takes the LAST title line.
	Title string `json:"title,omitempty"`

	// Usage is what one COMPLETED turn — or one auxiliary call beside it — cost,
	// and it is on its own line rather than on the assistant message that ended
	// the turn: a turn is several requests and several messages, and hanging the
	// bill on one of them would be a number that is true of the line above it and
	// of nothing else. Absent from every line that is not a seal, and from every
	// file written before it existed.
	Usage *journalUsage `json:"usage,omitempty"`

	// Call is ONE request's accounting, beside the seal rather than inside it.
	// Absent from every line that is not a call line, and from every file
	// written before it existed.
	Call *journalCall `json:"call,omitempty"`

	// Mark is ONE reading taken at a checkpoint mark, and Ceiling is what the
	// last mark then did with the turn (checkpoint.go). Absent from every line
	// that is not one of those, and from every file written before they existed.
	Mark    *journalMark    `json:"mark,omitempty"`
	Ceiling *journalCeiling `json:"ceiling,omitempty"`

	// Division is ONE division put to the road, whoever asked for it
	// (task_divide.go). Absent from every line that is not one, and from every
	// file written before it existed.
	Division *journalDivision `json:"division,omitempty"`

	Timestamp string `json:"timestamp"`
}

// journalCall is what ONE provider response reported, on its own line.
//
// IT IS EVIDENCE AND NEVER SPEND. The seal above already carries every one of
// these numbers, summed; a replay that added these lines too would bill the
// session twice for the same calls. Nothing reads them back into the session's
// totals, and [replaySessionFile] says so where it drops them.
//
// It exists because a turn is sixty-odd requests with wildly different shapes —
// a cold first call, then fifty that are almost all cache read — and the sum of
// them cannot answer what a call with THIS many cached tokens actually cost.
// That question had to be reconstructed from transcript byte counts once, in a
// cost autopsy that found this surface paying 3.5× its models' list prices; the
// line is so the next one is a read rather than a reconstruction.
//
// Endpoint is who served it, exactly as the router spelled it, and it is the
// field the summed seal could never carry: a turn routed across three endpoints
// has one bill and three tariffs.
//
// EVERY REQUEST THIS SESSION MAKES WRITES ONE, the errands included
// (auxiliary.go's [Agent.callRole]). It did not always: the line was written
// from the turn's own accounting alone, so a measured run's call lines summed to
// $0.123 while the real bill was $0.739 — the difference being three side-calls
// to a mastermind that left `usage` lines and no shape at all. A record that
// covers most of the money is a record that answers cost questions wrongly, so
// the sum of these lines IS the bill.
//
// Role is which errand made the call, spelled as the role registry spells it
// (internal/roles). It is ABSENT on the conversation's own requests rather than
// spelled "chat", because absent is what the whole file means by "this is the
// session itself" — [journalUsage] already writes its own Role the same way —
// and a name invented for the default case is a name that has to be kept in step
// with a registry it is not in.
type journalCall struct {
	Model      string  `json:"model,omitempty"`
	Endpoint   string  `json:"endpoint,omitempty"`
	Role       string  `json:"role,omitempty"`
	Input      int     `json:"input,omitempty"`
	CacheRead  int     `json:"cacheRead,omitempty"`
	CacheWrite int     `json:"cacheWrite,omitempty"`
	Output     int     `json:"output,omitempty"`
	CostUSD    float64 `json:"costUsd,omitempty"`
}

// journalMark is ONE reading taken at a checkpoint mark: what the sidecar was
// asked to draw mid-turn, what it drew, what the harness did about it, and what
// the call itself cost (checkpoint.go's [Agent.readMark]).
//
// IT EXISTS BECAUSE A DECISION NOBODY WROTE DOWN CANNOT BE MEASURED. Three of
// these reads were made on one measured run and cost sixty-two cents between
// them — five times the whole of what the work they were judging cost — and
// every one of them answered "carry on". None of that was in the file: the
// spend showed up as three anonymous auxiliary lines, and what was asked, what
// came back and what it decided existed nowhere at all. So the reading is
// journaled where the money already is, and a bench can join the two.
//
// N is which rung of the ladder this was and Rounds is where the turn stood when
// it fired, which together say whether the ladder is landing where the policy
// says it does. Sketch is THE SHAPE LINE ALONE — the legend is a sentence for a
// worker and not evidence for a reader of the file — and Decision is what the
// harness took off it: `split` when the turn was handed over on account of the
// parts, `continue` when nothing happened, `failed` when no reading came back at
// all. The ceiling's own read is a `continue` too: it decides nothing, and the
// ceiling line that follows it says what actually happened.
type journalMark struct {
	N          int     `json:"n,omitempty"`
	Rounds     int     `json:"rounds,omitempty"`
	Model      string  `json:"model,omitempty"`
	CostUSD    float64 `json:"costUsd,omitempty"`
	Sketch     string  `json:"sketch,omitempty"`
	Decision   string  `json:"decision,omitempty"`
	DurationMS int64   `json:"durationMs,omitempty"`
}

// journalCeiling is what the LAST mark did with the turn: moved the remaining
// work onto the one road, or dropped the handover and left the turn to finish.
//
// It is a line of its own rather than a field on the mark above it because the
// two are different facts about different moments — the mark is a reading and
// this is an act — and because the ceiling can fire with no reading behind it at
// all (a sidecar nobody could reach still meets the ceiling).
//
// Decision is `moved`, `dropped:nothing-left` — the running model declared the
// work finished AND the mark's own reader agreed nothing remained — or
// `dropped:no-brief`, which is the one other way a ceiling ends with no task:
// nothing could be written down for anybody. TaskID names the node when one was
// admitted, and is absent otherwise by the emptiness law the rest of the line
// keeps.
type journalCeiling struct {
	Rounds   int    `json:"rounds,omitempty"`
	Decision string `json:"decision,omitempty"`
	TaskID   uint64 `json:"taskId,omitempty"`
}

// journalDivision is ONE piece of work being put to the division road: who asked,
// how many parts they asked for, how many exist afterwards, and what answered
// (task_divide.go).
//
// IT EXISTS BECAUSE THREE COMPLETELY DIFFERENT OUTCOMES USED TO READ THE SAME.
// A task that ran with one worker had NEVER ASKED to divide, had asked and been
// refused by a free gate, or had asked and been refused by the reviewer — and the
// only trace of any of it was the absence of child nodes. Over three measured
// cells whose work a mastermind had already read as four jobs, every one landed
// `parts=0`, and nothing in any file said which of the three had happened. So one
// line, written wherever the road is asked.
//
// Source is `worker` for a division a worker reached for with the verb and
// `sketch` for one the harness submitted on its behalf out of a mark's drawing
// (task_divide_sketch.go). Requested is what was put; Admitted is how many parts
// exist, which differs when the reviewer merges. Decision is `admitted` or
// `refused:` and the gate that said no, so a bench can tell a floor refusal from
// a busy machine from a reviewer that read the parts as one job.
type journalDivision struct {
	TaskID    uint64 `json:"taskId,omitempty"`
	Source    string `json:"source,omitempty"`
	Requested int    `json:"requested,omitempty"`
	Admitted  int    `json:"admitted,omitempty"`
	Decision  string `json:"decision,omitempty"`
	// Error is why a review came to nothing, on the one decision where that is
	// not the same fact as the counter's refusal ([divisionRefusedUnreviewed]).
	Error string `json:"error,omitempty"`
}

// journalUsage is one turn's accounting as the journal holds it.
//
// Duration is milliseconds and not a time.Duration because a time.Duration
// marshals as bare nanoseconds, and this is a file a person reads.
//
// Aux marks a line that was NOT a step of the conversation: the title call, a
// memory reflex, a rendered picture, a folded task node. The distinction is
// what lets a replay rebuild both counters the live session keeps — Calls
// counts every request to the provider, Turns only the ones a turn of the
// person's made (see [Agent.addAuxiliaryUsage]).
type journalUsage struct {
	Model      string  `json:"model,omitempty"`
	Input      int     `json:"input,omitempty"`
	Output     int     `json:"output,omitempty"`
	CacheRead  int     `json:"cacheRead,omitempty"`
	CacheWrite int     `json:"cacheWrite,omitempty"`
	CostUSD    float64 `json:"costUsd,omitempty"`
	Calls      int     `json:"calls,omitempty"`
	DurationMS int64   `json:"durationMs,omitempty"`
	Aux        bool    `json:"aux,omitempty"`

	// Role names WHAT the auxiliary call was for — "title", "taskname" — on the
	// lines where knowing it changes what a person can do with the record. Aux
	// says a turn did not ask for the call and Model says which model answered
	// it; neither says what was being asked, so a name that came back wrong
	// could not be traced to the model that gave it. Absent from a turn's own
	// seal and from every auxiliary call that does not name itself, by the same
	// emptiness law the rest of the line keeps.
	Role string `json:"role,omitempty"`
}

// journalPartImage names the one non-text part a person's message can carry
// today. It is a field rather than an implied shape so a file written now stays
// readable when there is a second kind.
const journalPartImage = "image"

// journalPart is one non-text content part as the journal holds it: WHERE the
// bytes are and WHAT they were, never the bytes themselves.
//
// The digest is what makes the reference honest. A path alone says where a
// picture used to be; a build that trusted it would happily send a resumed
// session whatever now sits at that path — a different screenshot, a file the
// person overwrote an hour later — as the image they attached, and the model
// would answer about it as if the conversation had always been about that. So a
// replay re-reads the file ONLY when its digest still matches, and otherwise
// puts a placeholder in the transcript saying so. A transcript that admits it
// lost a picture is worth more than one that quietly substitutes another.
type journalPart struct {
	Type   string `json:"type"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	MIME   string `json:"mime,omitempty"`
}

// contentPart turns one reference back into content for the live transcript.
//
// This is the ONE rule for rebuilding a journaled part, and every rebuild goes
// through it: the resume replay below, and any later pass that rebuilds context
// from the file. Unchanged file, matching digest — the real image, byte-identical
// to what was sent the first time, so a resumed turn and the original turn put
// the same bytes on the wire. Anything else — moved, deleted, edited, unreadable,
// grown past the limit — is a text part that says which picture is missing.
func (p journalPart) contentPart() ai.ContentPart {
	if p.Type != journalPartImage {
		return p.placeholder()
	}
	info, err := os.Stat(p.Path)
	if err != nil || info.IsDir() || info.Size() > maxImageBytes {
		return p.placeholder()
	}
	data, err := os.ReadFile(p.Path)
	if err != nil || len(data) > maxImageBytes {
		return p.placeholder()
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != p.SHA256 {
		return p.placeholder()
	}
	mediaType := strings.TrimSpace(p.MIME)
	if mediaType == "" {
		mediaType = imageMediaTypes[strings.ToLower(filepath.Ext(p.Path))]
	}
	if mediaType == "" {
		return p.placeholder()
	}
	return ai.ContentPart{Type: "image_url", ImageURL: &ai.ImageURLData{
		URL: "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data),
	}}
}

// placeholder is what the model reads where a picture used to be. It names the
// path, because the person can often put the file back. The reason clause is
// the caller's, because the two callers know two different truths: a replay
// whose file will not open says so, and the transcript guard swapping for a
// blind model says THAT ([placeholderBecause]) — one sentence shape, the
// honest reason in it, never "changed or gone" about a file sitting untouched
// on disk.
func (p journalPart) placeholder() ai.ContentPart {
	return p.placeholderBecause("file changed or gone")
}

func (p journalPart) placeholderBecause(reason string) ai.ContentPart {
	if path := strings.TrimSpace(p.Path); path != "" {
		return ai.ContentPart{Type: "text", Text: "[image " + path + " — " + reason + "]"}
	}
	return ai.ContentPart{Type: "text", Text: "[image — " + reason + "]"}
}

// sessionFile is the open journal. Its own mutex keeps a line whole: the agent
// lock orders the writes, this one keeps a write from being interleaved by
// anything that reaches the file another way.
type sessionFile struct {
	mu     sync.Mutex
	file   *os.File
	locked bool
	closed bool
	// title is the name replayed from the file at open, so a resumed session
	// keeps the one it was given instead of paying to be named again.
	title string
	// id is the header's session id — generated when the file is created and
	// replayed unchanged on every resume after it. It is what makes a session
	// one identity across days rather than one per process, which is what the
	// prompt-cache lineage is keyed on (see [Agent.cacheKey]).
	id string

	// images is WHERE the pictures in this conversation came from: one journaled
	// path per non-text content part, under a fingerprint of the part itself
	// (see [partKey]).
	//
	// It lives on the file because the file is the only thing that knows. A
	// rebuilt image part is a data URL — bytes with no provenance, which is the
	// same reason the journal had to write a reference in the first place — so by
	// the time a surface asks "what was this a picture of", the answer exists
	// nowhere in the transcript. Both places a picture enters the file put it here
	// too: the replay at open, and every appended message that carries refs.
	//
	// A fingerprint rather than the URL itself, because the URL is the whole
	// base64'd photo: an index keyed on it would hold every image of the session
	// alive for as long as the file is open, including the ones a compaction
	// already dropped.
	images map[string]string

	// notes is WHICH user-role messages the session wrote itself, under the same
	// kind of fingerprint the pictures use ([noteKey]).
	//
	// It lives here for the same reason images does: the transcript cannot answer
	// the question. A wake note is user-role text and nothing about the message
	// distinguishes it from a line somebody typed — the mark is on the journal's
	// line, so the journal is what a surface asks (see [sessionEntry.Note] and
	// [shapeEntries]).
	notes     map[string]bool
	replyTags map[string][]TaskReplyTag

	// restored is what this conversation had already spent when the file was
	// opened: the SUM of its usage lines, replayed once and never updated after.
	// It is the file's answer to "what did this cost before today", and the agent
	// seeds its own running total from it at construction (agent.go). Nothing
	// stores a second copy of the total — the lines are the record, and this is
	// the one pass that adds them up.
	restored Usage
}

// RestoredUsage is what the conversation in this file had spent before it was
// opened, and the zero Usage for a file that is new or holds no usage lines.
func (s *sessionFile) RestoredUsage() Usage {
	if s == nil {
		return Usage{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restored
}

// Title is the name this file was opened holding, empty when it has none.
func (s *sessionFile) Title() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.title
}

// ID is the session id this file was opened holding, empty when the header
// carried none (a file written before the id was recorded, or a replay that
// stopped before reaching the header).
func (s *sessionFile) ID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.id
}

// imageRefs is the journaled path of every picture in one message, in the order
// the parts sit in it, and nil for the messages — nearly all of them — that
// carry none.
//
// The NIL RECEIVER answers nil, which is not defensiveness: a session with no
// file has no journal to have written a path, and making that caller test for a
// file before asking a question about pictures would put the same nil check at
// every call site instead of at the one place that can answer it.
func (s *sessionFile) imageRefs(message ai.Message) []string {
	if s == nil || len(message.Content) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.images) == 0 {
		return nil
	}
	var refs []string
	for _, part := range message.Content {
		if path, known := s.images[partKey(part)]; known {
			refs = append(refs, path)
		}
	}
	return refs
}

// imagePath is where ONE part's picture came from, "" when the journal never
// recorded it — a memory-only session, or a part this file did not write.
//
// It is separate from [sessionFile.imageRefs] rather than its inner half because
// the two ask different questions: that one walks a whole message under one lock
// and skips what it does not know, and this one asks about a single part and has
// to be able to say "not known" for it. The transcript guard is the caller, and
// it must replace EVERY image part whether or not the journal can name it.
//
// The NIL RECEIVER answers "", for the reason [sessionFile.imageRefs] answers
// nil: a session with no file wrote no path down.
func (s *sessionFile) imagePath(part ai.ContentPart) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.images[partKey(part)]
}

// rememberImagePath indexes one part under a path this file already knows, which
// is what keeps a picture's NAME on a message whose bytes have been replaced by a
// placeholder ([Agent.scrubBlindImagePartsLocked]). A replay does the same thing
// by accident and for the same reason — [rememberParts] indexes whatever part
// was rebuilt, placeholder or picture — so a scrubbed message and a replayed one
// draw alike.
//
// An empty path records nothing: an index entry pointing nowhere would make
// [sessionFile.imageRefs] claim a picture it cannot name.
func (s *sessionFile) rememberImagePath(part ai.ContentPart, path string) {
	if s == nil {
		return
	}
	if path = strings.TrimSpace(path); path == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.images == nil {
		s.images = make(map[string]string)
	}
	s.images[partKey(part)] = path
}

// isNote reports whether one message is a line the SESSION wrote — the answer
// [shapeEntries] turns into the "note" role a surface draws in its own lane
// rather than in the person's.
//
// The NIL RECEIVER answers false, for the reason [sessionFile.imageRefs] answers
// nil: a session with no file wrote no journal, so there is no mark to have
// read, and the caller should not have to check for a file first.
func (s *sessionFile) isNote(message ai.Message) bool {
	if s == nil {
		return false
	}
	key := noteKey(message)
	if key == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.notes[key]
}

// taskReplyTags returns the typed identities stored beside a completion note.
func (s *sessionFile) taskReplyTags(message ai.Message) []TaskReplyTag {
	if s == nil {
		return nil
	}
	key := noteKey(message)
	if key == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]TaskReplyTag(nil), s.replyTags[key]...)
}

func rememberReplyTags(index map[string][]TaskReplyTag, message ai.Message, tags []TaskReplyTag) {
	if len(tags) == 0 {
		return
	}
	if key := noteKey(message); key != "" {
		index[key] = append([]TaskReplyTag(nil), tags...)
	}
}

// rememberNote marks one message as the session's own, in a map the caller owns
// — the file's, under its lock, and the one a replay is still building.
func rememberNote(notes map[string]bool, message ai.Message) {
	if notes == nil {
		return
	}
	if key := noteKey(message); key != "" {
		notes[key] = true
	}
}

// noteKey fingerprints a session-authored line: its role and its text, through
// the same [partKey] the pictures are indexed by.
//
// ONE TEXT PART IS THE WHOLE SHAPE of these messages — [Agent.enqueueNote]
// builds them from a string — so anything else is not one and is left alone. Two
// notes with the same words share a key, which is the right answer: they are the
// same line and both are the session's.
func noteKey(message ai.Message) string {
	if len(message.Content) != 1 || message.Content[0].Type != "text" {
		return ""
	}
	return message.Role + "|" + partKey(message.Content[0])
}

// rememberParts records where one message's non-text parts came from.
//
// The refs are the message's LAST parts, and that is a fact both builders of
// such a message state: [imageUserMessage] appends the pictures after the
// optional text, and [replayedMessage] rebuilds them in the same order. Anything
// shorter than its own references is left alone rather than guessed at.
func (s *sessionFile) rememberParts(message ai.Message, refs []journalPart) {
	if s == nil || len(refs) == 0 || len(message.Content) < len(refs) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rememberParts(s.images, message, refs)
}

// rememberParts is the indexing itself, for a map the caller owns — the file's,
// under its lock, and the one a replay is still building.
func rememberParts(images map[string]string, message ai.Message, refs []journalPart) {
	if images == nil || len(refs) == 0 || len(message.Content) < len(refs) {
		return
	}
	parts := message.Content[len(message.Content)-len(refs):]
	for index, part := range parts {
		if path := strings.TrimSpace(refs[index].Path); path != "" {
			images[partKey(part)] = path
		}
	}
}

// partKey fingerprints one content part: its kind, its length, and its two
// ends.
//
// It is a fingerprint and not the content because the content is a multi-megabyte
// data URL, and it is BOTH ends because base64 of the same media type opens with
// the same handful of bytes for every picture — a key made of the head alone
// would collide across photos of the same size. Two parts that match this and
// are different bytes would have to agree on all three, which within one
// conversation is a photo attached twice.
func partKey(part ai.ContentPart) string {
	body := part.Text
	if part.ImageURL != nil {
		body = part.ImageURL.URL
	}
	const ends = 48
	size := len(body)
	if size > 2*ends {
		body = body[:ends] + body[size-ends:]
	}
	return part.Type + ":" + strconv.Itoa(size) + ":" + body
}

// openSessionFile opens (or creates) the journal, claims it, and replays it
// into the live transcript. The replay starts AFTER the latest compaction
// marker, with that marker's summary as the context prefix — resuming means
// resuming the conversation the model last had, not the one that was already
// summarized away.
//
// IT ALSO HANDS BACK WHAT IS ABOVE THAT MARKER ([replayedSession.earlier]).
// That region is not part of the transcript and is never sent; it is what a
// surface scrolls back into, so that the boundary reads as the seam it is
// rather than as the beginning of the conversation.
//
// The claim comes BEFORE the replay, not after. A lock taken at the end would
// leave two processes reading the same file concurrently and only then finding
// out one of them must back off, and the loser would have paid for a replay it
// cannot use. Locked first, the loser fails at the door.
// id is the name the header of a FRESHLY CREATED file carries, and it is the
// caller's rather than this function's because a session is a folder named by
// its id (place.go): the folder has to be minted before the transcript inside
// it can be, so by the time the journal is opened the id already exists. Empty
// is the legacy flat layout, where nobody outside had an opinion and the file
// names itself. A resumed file keeps the id it was written with either way.
func openSessionFile(path, cwd, model, id string) (*sessionFile, replayedSession, error) {
	if directory := filepath.Dir(path); directory != "" && directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return nil, replayedSession{}, fmt.Errorf("session file: %w", err)
		}
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, replayedSession{}, fmt.Errorf("session file: %w", err)
	}
	locked, err := lockSessionFile(file, path)
	if err != nil {
		_ = file.Close()
		return nil, replayedSession{}, err
	}
	journal := &sessionFile{file: file, locked: locked}

	// Creating the file above does not make it an existing session: existed is
	// "this file has lines in it", and a file this call just created has none.
	replayed, err := replaySessionFile(path)
	if err != nil {
		// The claim is released here rather than left to the caller: this
		// returns no journal, so nobody else has a handle to close.
		_ = journal.Close()
		return nil, replayedSession{}, err
	}
	journal.title = replayed.title
	journal.id = replayed.id
	journal.images = replayed.images
	journal.notes = replayed.notes
	journal.replyTags = replayed.replyTags
	journal.restored = replayed.usage

	if !replayed.existed {
		// The header names the session once. A resumed file keeps its
		// original: the id is what a second window looks a session up by.
		journal.id = strings.TrimSpace(id)
		if journal.id == "" {
			journal.id = NewSessionID()
		}
		journal.writeLine(sessionHeader{
			Type:      "session",
			Version:   sessionFileVersion,
			ID:        journal.id,
			Cwd:       cwd,
			Model:     model,
			Timestamp: stamp(),
		})
	}
	return journal, replayed, nil
}

// lockSessionFile claims the journal for this process with a non-blocking
// exclusive flock, and reports whether the claim was actually taken.
//
// flock is the right primitive here because the kernel releases it when the
// holding process dies, however it dies. That is the whole reason there is no
// pid file and no staleness check: a held flock IS a live writer, so a crashed
// aforge leaves nothing behind to clean up or to second-guess. The lock rides
// the open file description, so it lives exactly as long as the descriptor the
// journal holds.
//
// A filesystem that cannot flock at all (some network mounts answer EINVAL or
// ENOLCK) is not a reason to refuse the session. The interleave this guards
// against is possible but rare; being unable to open your own transcript is
// certain. So an unsupported lock opens unlocked, and the caller carries
// locked=false so Close does not unlock what it never took.
func lockSessionFile(file *os.File, path string) (bool, error) {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, unix.EWOULDBLOCK):
		// EAGAIN on Linux, EWOULDBLOCK on darwin — the same value, and the one
		// answer that means "somebody else holds this".
		return false, &SessionLockedError{Path: path}
	default:
		return false, nil
	}
}

// replaySessionFile reads a journal into messages. It reports whether the file
// had any content — an empty or missing file is a new session, not an error.
//
// A line that does not parse is skipped rather than fatal: the one line a
// crash can corrupt is the last one, and losing a session because its tail was
// half-written is the wrong trade against losing that tail. The replayed
// transcript is then repaired (see repairTranscript) — a half-written tail can
// be a half-written tool batch, which is not a lost line but an illegal
// transcript.
func replaySessionFile(path string) (replayedSession, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return replayedSession{}, nil
		}
		return replayedSession{}, fmt.Errorf("session file: %w", err)
	}
	defer file.Close()

	var (
		messages []ai.Message
		earlier  []ai.Message
		overlap  int
		title    string
		id       string
		lines    int
		spent    Usage
	)
	// The picture index is built as the messages are, because this is the one
	// pass that holds both halves at once: the reference the journal wrote and
	// the part it was rebuilt into (see [sessionFile.images]). It survives a
	// compaction marker for the same reason the title does — where a picture came
	// from is a fact about the file, not about the tail of the transcript.
	images := make(map[string]string)
	// And the note index with it, for the same reason and in the same pass: the
	// mark is on the LINE, and once the line has been rebuilt into a message
	// there is nothing left to read it off (see [sessionFile.notes]).
	notes := make(map[string]bool)
	replyTags := make(map[string][]TaskReplyTag)
	scanner := bufio.NewScanner(file)
	// A tool result can be tens of kilobytes; the default 64KiB token limit
	// would end the replay at the first big one.
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for scanner.Scan() {
		// scanner.Bytes() is the scanner's own buffer and is only valid until the
		// next Scan. Every reader of it in this loop is one of the two unmarshals
		// below, both of which consume it before the loop turns over and copy
		// every string they keep out of it. Text() would instead allocate a copy
		// of the line, and []byte(...) of that copy a second one — two copies of
		// every line of the journal, and a tool result is tens of kilobytes.
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		lines++
		var entry sessionEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		switch entry.Type {
		case "session":
			// The header names the format. A file written by a newer aforge can
			// hold entry types and fields this build does not know, and every
			// one of them would be dropped in silence — a session that resumes
			// looking complete and is not. Say so instead.
			var header sessionHeader
			if err := json.Unmarshal(line, &header); err != nil {
				continue
			}
			if header.Version > sessionFileVersion {
				return replayedSession{existed: true}, fmt.Errorf(
					"session file: %s was written by a newer aforge (format version %d; this build reads %d)",
					path, header.Version, sessionFileVersion)
			}
			// FIRST one wins, unlike the title: the header is written once, at
			// creation, and a second one in the same file would be a file two
			// processes wrote — in which case the older identity is the one the
			// conversation actually has.
			if id == "" {
				id = strings.TrimSpace(header.ID)
			}
		case "message":
			if entry.Role == "" {
				continue
			}
			message := replayedMessage(entry)
			rememberParts(images, message, entry.Parts)
			if entry.Note {
				rememberNote(notes, message)
				rememberReplyTags(replyTags, message, entry.ReplyTags)
			}
			messages = append(messages, message)
		case "compaction":
			// THE REGION THIS MARKER REPLACES IS KEPT BEFORE IT IS THROWN AWAY,
			// which is the one thing this pass does that the live transcript has
			// no use for. It is what a surface scrolls back into: the journal
			// holds every line of the conversation above the marker, and without
			// this the boundary would masquerade as the beginning of the chat.
			//
			// It is taken with the SAME reducer state that is about to be
			// discarded — rewind cuts already applied, an older marker's window
			// already in place — so the region is the transcript exactly as it
			// stood one instant before this pass edited it, and not a naive
			// re-read of the lines above the marker (which would resurrect turns
			// a rewind took back).
			//
			// The LAST marker wins because each one overwrites the snapshot the
			// one before it took. See [replayedSession.earlier] for why the
			// nested regions are not stacked.
			earlier = append(earlier[:0], messages...)
			// Everything before this marker is what the pass replaces. The
			// name is not a message and survives the cut: a compacted session
			// is the same session, still called what it was called.
			rebuilt := compactionMessages(entry)
			// AND THE REGION IS ONLY KEPT IF IT CAN BE PLACED. The pass wrote its
			// whole rebuilt window back below the marker, so the lines above it
			// and the first Window lines below it are two renderings of ONE
			// conversation — a reader that could not say where the copy ends has
			// nothing it can honestly draw, and drops the region rather than
			// showing the session to itself twice.
			if overlap = compactionOverlap(entry); overlap < 0 {
				earlier, overlap = earlier[:0], 0
			}
			messages = append(messages[:0], rebuilt...)
			// The frames message is the FIRST of them when an old marker carried
			// pages, which is the order [compactionMessages] builds and the only
			// place those references belong: the summary beside it is words.
			if len(entry.Parts) > 0 && len(rebuilt) > 0 {
				rememberParts(images, rebuilt[0], entry.Parts)
			}
		case "rewind":
			// The turn this line took back. Everything after it in the file is
			// ordinary conversation again — a rewind is followed by the person
			// saying the thing better — so the replay drops N and keeps reading
			// rather than stopping here.
			if entry.Dropped <= 0 {
				continue
			}
			if entry.Dropped >= len(messages) {
				messages = messages[:0]
				continue
			}
			messages = messages[:len(messages)-entry.Dropped]
		case "usage":
			// EVERY line is added, and none is ever taken back. This is the one
			// arm that accumulates rather than rebuilds: a compaction below
			// replaces the message window, and money spent before it stays spent.
			if entry.Usage == nil {
				continue
			}
			used := entry.Usage
			spent.Input += used.Input
			spent.Output += used.Output
			spent.CacheRead += used.CacheRead
			spent.CacheWrite += used.CacheWrite
			spent.CostUSD += used.CostUSD
			spent.Duration += time.Duration(used.DurationMS) * time.Millisecond
			spent.Calls += used.Calls
			// Turns counts the conversation's own steps and nothing else, which
			// is the law the live counters keep ([Agent.addUsage] bumps it,
			// [Agent.addAuxiliaryUsage] deliberately does not). The aux mark on
			// the line is what lets a replay keep the same distinction.
			if !used.Aux {
				spent.Turns += used.Calls
			}
		case "call":
			// DROPPED ON PURPOSE, and this arm exists to say so rather than to
			// leave it to the switch falling off the end. A call line is the
			// SHAPE of one request — who served it, how much of its prompt was
			// warm — and every dollar on it is already counted in the seal that
			// closed its turn. Folding it in here would bill the session twice
			// for the same money.
		case "mark", "ceiling", "division":
			// DROPPED ON PURPOSE, for the reason a call line is: these are the
			// RECORD of a decision the harness took mid-turn, and a decision is
			// not a message and not money. Whatever the mark's reader cost is
			// already on the usage line beside it and on its own call line, and
			// what the ceiling did to the turn is already in the transcript —
			// the line the person read, and the task the graph admitted. A
			// division's parts are nodes in the graph's own checkpoint and its
			// receipt is already in the worker's transcript.
			// Replaying them would put machinery into somebody's conversation.
		case "title":
			// LAST one wins. A name written twice is a name that was changed,
			// and the file's order is the order it was changed in. A name that
			// is the namer's own instruction is read as NO name ([healedTitle],
			// title.go), which is what heals the sessions that were already
			// written down under one.
			if named := healedTitle(entry.Title); named != "" {
				title = named
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return replayedSession{title: title, id: id, images: images, notes: notes, replyTags: replyTags, usage: spent, existed: lines > 0}, fmt.Errorf("session file: %w", err)
	}
	repaired := repairTranscript(messages)
	// The overlap was counted against the lines the file holds and is applied to
	// the transcript the repair left behind, so it is clamped to it. The repair
	// only ever drops an unanswered trailing batch and orphaned results — the tail
	// and the rare stray — so the two agree in every session that was not killed
	// mid-batch, and a message of drift at the seam is a row nobody can see.
	if overlap > len(repaired) {
		overlap = len(repaired)
	}
	return replayedSession{
		messages: repaired,
		// The earlier region goes through the SAME repair as the live one. It is
		// never sent, so the 400 the repair exists to prevent cannot happen to
		// it — but a tool result whose call is missing is a row a surface would
		// draw with nothing above it either way, and two shapings of one journal
		// that disagreed about which lines are real would be the seam lying in a
		// second way.
		earlier:   repairTranscript(earlier),
		overlap:   overlap,
		title:     title,
		id:        id,
		images:    images,
		notes:     notes,
		replyTags: replyTags,
		usage:     spent,
		existed:   lines > 0,
	}, nil
}

// replayedMessage rebuilds one journaled message into the live transcript.
//
// A line with no references is rebuilt exactly as it always was: one text part,
// even when the text is empty — an assistant message that was pure tool calls
// journals empty content, and giving it no content at all would change a shape
// the provider has been accepting all along.
//
// A line WITH references is text-then-parts, which is the order [imageUserMessage]
// assembled and therefore the order the model read the first time. The text part
// is dropped when there was no text, for the same reason it was never added.
func replayedMessage(entry sessionEntry) ai.Message {
	message := ai.Message{
		Role:       entry.Role,
		Content:    []ai.ContentPart{{Type: "text", Text: entry.Content}},
		ToolCalls:  entry.ToolCalls,
		ToolCallID: entry.ToolCallID,
	}
	if len(entry.Parts) == 0 {
		return message
	}
	content := make([]ai.ContentPart, 0, len(entry.Parts)+1)
	if entry.Content != "" {
		content = append(content, ai.ContentPart{Type: "text", Text: entry.Content})
	}
	for _, part := range entry.Parts {
		content = append(content, part.contentPart())
	}
	message.Content = content
	return message
}

// compactionMessages rebuilds one compaction marker into the context prefix it
// left behind.
//
// A MARKER WRITTEN BY THE CURRENT PASS LEAVES NOTHING BEHIND, and answers nil.
// Its pass rearranged the transcript rather than replacing it — stubs, a fold
// marker, the verbatim tail — and re-journaled the whole rebuilt window on the
// far side of the line, so the window comes back as ordinary message lines and
// this has nothing to add (loop.go's [Agent.compact]).
//
// THE OTHER TWO SHAPES ARE LEGACY and are read for one reason: a session
// compacted by an older aforge has to still resume as itself.
//
//   - SUMMARY — the prose a summarizer wrote. One note, exactly as it was.
//   - PAGES — the frames rung's images, each re-read only while its digest still
//     matches ([journalPart]), with the summary of any overflow after them.
//
// Neither is written any more.
func compactionMessages(entry sessionEntry) []ai.Message {
	summary := strings.TrimSpace(entry.Summary)
	if len(entry.Parts) == 0 {
		if summary == "" {
			return nil
		}
		return []ai.Message{textMessage("user", legacyCompactionNote(entry.Summary))}
	}
	content := make([]ai.ContentPart, 0, len(entry.Parts)+1)
	content = append(content, ai.ContentPart{Type: "text", Text: legacyFramesNote})
	for _, part := range entry.Parts {
		content = append(content, part.contentPart())
	}
	messages := []ai.Message{{Role: "user", Content: content}}
	if summary != "" {
		messages = append(messages, textMessage("user", legacyCompactionNote(entry.Summary)))
	}
	return messages
}

// compactionOverlap is how many of the messages BELOW one marker are the pass's
// own rewritten copy of the conversation ABOVE it, and -1 when the file does not
// say and the region therefore cannot be placed.
//
// It reads [sessionEntry.Window] and nothing else, which is the whole of its
// discipline. Two shapes answer -1:
//
//   - A MARKER WRITTEN BEFORE THE FIELD EXISTED. The window is down there and
//     its length is not, so a reader can tell that the conversation is written
//     twice and not where the second copy stops.
//   - A LEGACY MARKER, which re-journaled the tail it kept with no count either.
//
// In both cases the region above is dropped and the session behaves exactly as
// it did before any of this: the transcript below the marker is the whole of
// what a surface can draw. A session picks the ability up the next time it
// compacts, because that pass writes the number.
//
// IT DOES NOT GUESS. The length is derivable from Folded — a fold replaces its
// run with one line, so the window is the region less the folded messages plus
// one — and deriving it was rejected: those counts are the RECORD of what a pass
// did, the pass is free to change what it does, and a wrong length here does not
// fail, it silently draws somebody's conversation twice.
func compactionOverlap(entry sessionEntry) int {
	if entry.Window <= 0 {
		return -1
	}
	return entry.Window
}

// legacyFramesNote is what a pre-phase-3 frames marker put in front of its page
// images. It is a string a resume has to be able to reproduce, so it lives here
// beside the only reader left of it.
const legacyFramesNote = "[context compacted] Everything before this point was rendered verbatim to page " +
	"images rather than summarized — the transcript is kept as images below, in order, and " +
	"nothing in it was shortened or rephrased. It is not something either of us said, and any " +
	"question inside it is still open."

// legacyCompactionNote wraps an old marker's summary as the user-role message it
// was written as. User role because it was context handed TO the model rather
// than something it produced, and marked in plain words because a model that
// mistakes a summary for a transcript will answer questions inside it.
func legacyCompactionNote(summary string) string {
	return "[context compacted] Everything before this point was summarized to fit the " +
		"context window. This note is the record of that conversation — it is not something " +
		"either of us said, and any question inside it is still open.\n\n" + summary
}

// replayedSession is what one pass over the journal recovered: the live
// transcript, the session's name, and whether the file had any lines at all.
//
// It is a struct rather than three returns because the three grow together —
// the name arrived here after the other two — and a reader of a call site
// should not have to count positions to know which bool is which.
type replayedSession struct {
	messages []ai.Message
	// earlier is the transcript as it stood ONE INSTANT BEFORE the latest
	// compaction marker — the conversation the pass edited away, which the file
	// still holds in full and the model no longer carries. Nil for a journal
	// that was never compacted, which is nearly all of them.
	//
	// THE REGION IS ONE HOP AND IS NOT STACKED, and that is a deliberate reading
	// of a file that could be read the other way. A modern pass RE-JOURNALS THE
	// WHOLE REBUILT WINDOW behind its marker ([sessionFile.appendCompaction]), so
	// the lines after an older marker already contain everything before it, edited
	// — a walk that ignored the older markers to "reach the raw beginning" would
	// hand a surface the same conversation twice, once as it happened and once as
	// that pass rewrote it. Applying every marker but the last one instead costs
	// nothing a person can see: a pass never folds a USER message ([Agent.foldLocked]),
	// so the region still opens on the conversation's very first words, with the
	// older passes' stubs and fold lines in it exactly where the file puts them.
	earlier []ai.Message
	// overlap is how many messages at the START of messages are the pass's own
	// rewritten copy of earlier — the rebuilt window it journaled behind its
	// marker (see [compactionOverlap]). The conversation, told once and whole, is
	// therefore `earlier` followed by `messages[overlap:]`.
	//
	// Zero whenever earlier is empty, and never anything else: a region that
	// cannot be placed is not kept.
	overlap int
	title   string
	// id is the header's session id, empty for a file that has no header yet.
	id string
	// images is where this file's pictures came from, keyed by [partKey] — the
	// index [sessionFile.images] is opened holding.
	images map[string]string
	// notes is which of those messages the session wrote itself, keyed by
	// [noteKey] — the index [sessionFile.notes] is opened holding.
	notes map[string]bool
	// replyTags is the typed identity stored on task completion notes.
	replyTags map[string][]TaskReplyTag
	// usage is the SUM of the file's usage lines — what this conversation has
	// spent across every process that ever held it. Summed rather than stored,
	// so the total cannot drift from the lines it is made of.
	usage   Usage
	existed bool
}

// repairTranscript makes a replayed transcript legal to send.
//
// A batch's results are journaled only after the whole batch has run
// (loop.go), so a session killed mid-batch leaves an assistant message whose
// tool_calls nobody answered. That is not a cosmetic gap: every provider
// rejects the shape with a 400, and because the transcript is append-only the
// session would be rejected on this request and on every request after it,
// forever — a file that can never be resumed. The mirror shape, a tool result
// whose call was never journaled, is rejected the same way.
//
// Two rules, in order:
//
//  1. an unanswered trailing batch is dropped, together with whatever partial
//     results it did journal — the tools ran, but the model never saw them, so
//     the honest resume is the one where the call was never made;
//  2. a tool message with no call above it is dropped.
func repairTranscript(messages []ai.Message) []ai.Message {
	for index := len(messages) - 1; index >= 0; index-- {
		if len(messages[index].ToolCalls) == 0 {
			continue
		}
		// Only the LAST batch can be the interrupted one, and only if nothing
		// but its results follows: an assistant or user message after it is
		// proof the conversation moved on, which it could not have done
		// through a transcript the provider was refusing.
		answered := make(map[string]bool)
		trailing := true
		for _, later := range messages[index+1:] {
			if later.Role != "tool" {
				trailing = false
				break
			}
			answered[later.ToolCallID] = true
		}
		if trailing {
			for _, call := range messages[index].ToolCalls {
				if !answered[call.ID] {
					messages = messages[:index]
					break
				}
			}
		}
		break
	}

	seen := make(map[string]bool)
	repaired := messages[:0]
	for _, message := range messages {
		if message.Role == "tool" {
			if message.ToolCallID == "" || !seen[message.ToolCallID] {
				continue
			}
			repaired = append(repaired, message)
			continue
		}
		for _, call := range message.ToolCalls {
			seen[call.ID] = true
		}
		repaired = append(repaired, message)
	}
	if len(repaired) == 0 {
		// nil rather than an empty slice: a fresh session's transcript is nil,
		// and a resume that repaired away to nothing is exactly that.
		return nil
	}
	return repaired
}

// appendMessage journals one message: its text flattened, and the durable
// references for whatever else it carried.
//
// refs are variadic because almost nothing has any — the model's replies, the
// tool results, the compaction tail — and a call site that passes none writes
// exactly the line it wrote before this existed.
//
// The references are the CALLER's, not derived from the content here: a data URL
// in a part is bytes with no provenance, and by the time a message reaches the
// journal there is no way to recover the path it was read from (see
// [userMessage]).
func (s *sessionFile) appendMessage(message ai.Message, refs ...journalPart) {
	s.append(message, false, refs)
}

// messageRef names the most recent journal line carrying message. Compaction
// uses it when the store is off, so a fold marker points at an exact range in
// the append-only record instead of merely saying that a record exists.
func (s *sessionFile) messageRef(message ai.Message) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.file == nil {
		return ""
	}
	content, err := os.ReadFile(s.file.Name())
	if err != nil {
		return ""
	}
	want := chatRefKey(message)
	lineNumber := 0
	found := 0
	for _, line := range bytes.Split(content, []byte{'\n'}) {
		lineNumber++
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var entry sessionEntry
		if json.Unmarshal(line, &entry) != nil || entry.Type != "message" {
			continue
		}
		candidate := ai.Message{Role: entry.Role, ToolCallID: entry.ToolCallID,
			Content: []ai.ContentPart{{Type: "text", Text: entry.Content}}, ToolCalls: entry.ToolCalls}
		if chatRefKey(candidate) == want {
			found = lineNumber
		}
	}
	if found == 0 {
		return ""
	}
	return fmt.Sprintf("journal:line-%d", found)
}

// appendNote is appendMessage for a line the SESSION wrote (see
// [sessionEntry.Note]). It is a separate door rather than a flag on the common
// one because exactly one caller has the answer — [Agent.recordUserLocked],
// which is holding the [userMessage] the mark comes off — and every other call
// site should stay the call it was.
func (s *sessionFile) appendNote(message ai.Message, tags ...[]TaskReplyTag) {
	s.append(message, true, nil, tags...)
}

func (s *sessionFile) append(message ai.Message, note bool, refs []journalPart, tagSets ...[]TaskReplyTag) {
	// Indexed as it is written, not only as it is replayed: a picture attached
	// an hour ago is one a rewind or a /compact can put back through the display
	// shaping in THIS process, long before anybody resumes the file. The same is
	// true of a note: the woken turn it belongs to is drawn in THIS process, and
	// a /compact or a rewind can put its line back through the shaping.
	s.rememberParts(message, refs)
	if note {
		s.mu.Lock()
		if s.notes == nil {
			s.notes = make(map[string]bool, 4)
		}
		rememberNote(s.notes, message)
		if len(tagSets) > 0 && len(tagSets[0]) > 0 {
			if s.replyTags == nil {
				s.replyTags = make(map[string][]TaskReplyTag)
			}
			rememberReplyTags(s.replyTags, message, tagSets[0])
		}
		s.mu.Unlock()
	}
	// The single text part is what nearly every message is, and its text is
	// already the string the line wants; a Builder would copy a whole tool
	// result to arrive back at it.
	var text string
	if len(message.Content) == 1 && message.Content[0].Type == "text" {
		text = message.Content[0].Text
	} else {
		var flattened strings.Builder
		for _, part := range message.Content {
			if part.Type == "text" {
				flattened.WriteString(part.Text)
			}
		}
		text = flattened.String()
	}
	s.writeLine(sessionEntry{
		Type:       "message",
		Role:       message.Role,
		Content:    text,
		ToolCalls:  message.ToolCalls,
		ToolCallID: message.ToolCallID,
		Parts:      refs,
		Note:       note,
		ReplyTags:  firstReplyTags(tagSets),
		Timestamp:  stamp(),
	})
}

func firstReplyTags(tagSets [][]TaskReplyTag) []TaskReplyTag {
	if len(tagSets) == 0 {
		return nil
	}
	return tagSets[0]
}

// appendCompaction journals one pass: the marker, then the whole rebuilt window
// again.
//
// The re-journal is what makes a compacted session resumable as itself. Replay
// discards everything before the marker — that is the marker's meaning — so
// whatever the pass decided the window should be has to sit on the far side of
// it, or a resume comes back holding a prefix the pass deliberately edited.
//
// IT IS THE WHOLE WINDOW AND NOT THE TAIL, which is the change this pass makes
// to the file's meaning. The old pass only ever edited by DELETION: everything
// above the cut became one summary, so the tail was the only thing whose text
// the marker had to carry forward. The new pass edits in place — a tool result
// becomes a stub, a run of assistant work becomes one line — and those edits sit
// above the marker among lines the file already holds in their original form. So
// the window is written out entire, and the lines above the marker stay exactly
// what they were: the RECORD of what happened, which is what the journal is for.
//
// The counts ride the marker so a surface reading the file back can say what the
// pass did without re-deriving it. Nothing rebuilds from them.
func (s *sessionFile) appendCompaction(pass compactionPass, tokensBefore int, window []ai.Message) {
	s.writeLine(sessionEntry{
		Type:         "compaction",
		TokensBefore: tokensBefore,
		Stubbed:      pass.stubbed,
		Folded:       pass.folded,
		// The length is written BEFORE the window it describes, which is the only
		// order that survives a crash halfway through: a reader that finds fewer
		// lines than the number promised has a truncated file and can say so,
		// where a count written afterwards would simply never arrive.
		Window:    len(window),
		Timestamp: stamp(),
	})
	for _, message := range window {
		// A KEPT LINE IS RE-JOURNALED AS WHAT IT WAS. The window is written again
		// on the far side of the marker (above), and a note re-written without its
		// mark would come back from the next resume as the person's words — this
		// pass is the one place a message is journaled twice.
		if s.isNote(message) {
			s.appendNote(message, s.taskReplyTags(message))
			continue
		}
		s.appendMessage(message)
	}
}

// appendRewind journals one rewind: the count of messages it removed from the
// live transcript. Nothing in the file is rewritten — the dropped lines stay
// where they are, and the marker is what a replay reads them against. That is
// what keeps the journal a record of what happened rather than of what is
// currently believed: a rewound turn really did run, and its tool calls really
// did touch the workspace.
func (s *sessionFile) appendRewind(dropped int) {
	if dropped <= 0 {
		return
	}
	s.writeLine(sessionEntry{Type: "rewind", Dropped: dropped, Timestamp: stamp()})
}

// appendTitle journals the session's name. It is one line, appended like any
// other: a later name simply lands after this one, and the replay takes the
// last. Nothing rewrites the file.
func (s *sessionFile) appendTitle(title string) {
	title = strings.TrimSpace(title)
	if title == "" {
		return
	}
	s.mu.Lock()
	s.title = title
	s.mu.Unlock()
	s.writeLine(sessionEntry{Type: "title", Title: title, Timestamp: stamp()})
}

// appendUsage journals what one seal cost: the turn's own figures, or one
// auxiliary call's beside it.
//
// It is the file's half of the one-source-of-truth law. The session total is
// the SUM of these lines and is stored nowhere else, so a resumed conversation
// knows what it spent by adding them up (see [replaySessionFile]) rather than
// by trusting a number some earlier process wrote down.
//
// A SEAL THAT SPENT NOTHING WRITES NOTHING. An instantly-cancelled turn, and
// the zero-token seals the harness, an orchestrated run and the image turn send
// because their spend went through the auxiliary door, all leave no row — the
// emptiness law applied to the file.
//
// The NIL RECEIVER writes nothing, for the reason [sessionFile.isNote] answers
// false: a memory-only session has no journal, and the caller should not have
// to test for a file before sealing a turn.
func (s *sessionFile) appendUsage(used Usage, model string, aux bool, role string) {
	if s == nil {
		return
	}
	if used.Input == 0 && used.Output == 0 && used.CostUSD == 0 {
		return
	}
	s.writeLine(sessionEntry{
		Type: "usage",
		Usage: &journalUsage{
			Model:      strings.TrimSpace(model),
			Input:      used.Input,
			Output:     used.Output,
			CacheRead:  used.CacheRead,
			CacheWrite: used.CacheWrite,
			CostUSD:    used.CostUSD,
			Calls:      used.Calls,
			DurationMS: used.Duration.Milliseconds(),
			Aux:        aux,
			Role:       strings.TrimSpace(role),
		},
		Timestamp: stamp(),
	})
}

// appendCall writes ONE response's own accounting down, beside the seal that
// will sum it.
//
// A RESPONSE THAT REPORTED NO USAGE WRITES NOTHING. The emptiness law, and the
// same test the seal keeps: a call with no tokens and no cost is a call the
// provider said nothing about, and a row of zeroes would read as a fact. A
// stream that was cut before its final chunk is exactly that case.
//
// The nil receiver writes nothing, as everywhere in this file: a memory-only
// session has no journal and no caller should have to know it.
func (s *sessionFile) appendCall(call journalCall) {
	if s == nil {
		return
	}
	if call.Input == 0 && call.Output == 0 && call.CacheRead == 0 && call.CacheWrite == 0 && call.CostUSD == 0 {
		return
	}
	s.writeLine(sessionEntry{Type: "call", Call: &call, Timestamp: stamp()})
}

// appendMark writes ONE mark's reading down (see [journalMark]).
//
// A MARK THAT NEVER HAPPENED WRITES NOTHING, which is the emptiness law applied
// to a file a person reads: a turn that crossed no mark, and a session that
// cannot reach a reader at all, leave the journal exactly as it was before any
// of this existed. The caller's own guard is the one that knows — a read that
// was never attempted is not a read — and this repeats it on the decision,
// because a line with no decision on it says nothing about anything.
//
// The nil receiver writes nothing, as everywhere in this file.
func (s *sessionFile) appendMark(mark journalMark) {
	if s == nil || strings.TrimSpace(mark.Decision) == "" {
		return
	}
	s.writeLine(sessionEntry{Type: "mark", Mark: &mark, Timestamp: stamp()})
}

// appendCeiling writes down what the last mark did with the turn (see
// [journalCeiling]). A ceiling that did not fire writes nothing, for
// [sessionFile.appendMark]'s reason.
func (s *sessionFile) appendCeiling(ceiling journalCeiling) {
	if s == nil || strings.TrimSpace(ceiling.Decision) == "" {
		return
	}
	s.writeLine(sessionEntry{Type: "ceiling", Ceiling: &ceiling, Timestamp: stamp()})
}

// appendDivision writes down one division put to the road (see
// [journalDivision]). A division nobody asked for writes nothing, for
// [sessionFile.appendMark]'s reason: the whole value of the line is telling
// never-asked from refused, and a line with no decision on it says neither.
func (s *sessionFile) appendDivision(division journalDivision) {
	if s == nil || strings.TrimSpace(division.Decision) == "" {
		return
	}
	s.writeLine(sessionEntry{Type: "division", Division: &division, Timestamp: stamp()})
}

// writeLine marshals one entry and appends it. A failed write is dropped
// rather than raised: the journal is a record of the conversation, and a
// person mid-turn cannot act on "the transcript did not save" — the next
// Close reports the state of the file.
func (s *sessionFile) writeLine(entry any) {
	payload, err := json.Marshal(entry)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	payload = append(payload, '\n')
	_, _ = s.file.Write(payload)
}

// Close flushes the file, releases the claim, and closes the descriptor.
// Writes are unbuffered appends, so the flush is the kernel's; Close is what
// makes the file safe for another aforge to open. Calling it twice is safe.
//
// The explicit unlock is belt-and-braces — closing the descriptor drops the
// flock on its own — but it states the release at the place a reader looks for
// it, and it puts the release before the close rather than as a side effect of
// it. Nothing can slip into the gap: closed is already set, so no write reaches
// the file after this point.
func (s *sessionFile) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if err := s.file.Sync(); err != nil {
		// Sync failing on a tmpfs or a pipe is not a lost transcript; the
		// close below is the one that matters.
		_ = err
	}
	if s.locked {
		s.locked = false
		_ = unix.Flock(int(s.file.Fd()), unix.LOCK_UN)
	}
	return s.file.Close()
}

func stamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// InUse reports whether another aforge is holding this transcript open.
//
// It is the same flock [lockSessionFile] takes, asked as a question rather than
// as a claim: the lock is tried and released at once, so the answer is "somebody
// else has it right now" and nothing is left behind. A migration and a launch
// groom both need it — moving or removing a session another window is writing
// is the one way either of them could cost somebody a live conversation — and
// both would rather skip a folder than take one.
//
// A file that is not there, cannot be opened, or sits on a filesystem with no
// locking answers FALSE, for the reason [lockSessionFile] opens unlocked on such
// a filesystem: the guard is worth having where it works and is never worth
// refusing the work over.
func InUse(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return errors.Is(err, unix.EWOULDBLOCK)
	}
	_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
	return false
}

// NewSessionID is 16 random hex characters: enough to name every session a
// machine will ever hold without a coordinator.
//
// It is EXPORTED because the folder is named by it (place.go): the surface
// mints the id, makes the directory, and hands the same id back here for the
// header — one law for what a session id is, applied at both ends.
func NewSessionID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// crypto/rand failing is not a reason to refuse to open a session.
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw[:])
}
