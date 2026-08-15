package session

import (
	"bufio"
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

	// Compaction fields.
	Summary      string `json:"summary,omitempty"`
	TokensBefore int    `json:"tokensBefore,omitempty"`

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

	Timestamp string `json:"timestamp"`
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
// path, because the person can often put the file back.
func (p journalPart) placeholder() ai.ContentPart {
	return ai.ContentPart{Type: "text", Text: "[image " + p.Path + " — file changed or gone]"}
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
// The claim comes BEFORE the replay, not after. A lock taken at the end would
// leave two processes reading the same file concurrently and only then finding
// out one of them must back off, and the loser would have paid for a replay it
// cannot use. Locked first, the loser fails at the door.
func openSessionFile(path, cwd, model string) (*sessionFile, []ai.Message, error) {
	if directory := filepath.Dir(path); directory != "" && directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return nil, nil, fmt.Errorf("session file: %w", err)
		}
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("session file: %w", err)
	}
	locked, err := lockSessionFile(file, path)
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	journal := &sessionFile{file: file, locked: locked}

	// Creating the file above does not make it an existing session: existed is
	// "this file has lines in it", and a file this call just created has none.
	replayed, err := replaySessionFile(path)
	if err != nil {
		// The claim is released here rather than left to the caller: this
		// returns no journal, so nobody else has a handle to close.
		_ = journal.Close()
		return nil, nil, err
	}
	journal.title = replayed.title
	journal.id = replayed.id
	journal.images = replayed.images

	if !replayed.existed {
		// The header names the session once. A resumed file keeps its
		// original: the id is what a second window looks a session up by.
		journal.id = newSessionID()
		journal.writeLine(sessionHeader{
			Type:      "session",
			Version:   sessionFileVersion,
			ID:        journal.id,
			Cwd:       cwd,
			Model:     model,
			Timestamp: stamp(),
		})
	}
	return journal, replayed.messages, nil
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
		title    string
		id       string
		lines    int
	)
	// The picture index is built as the messages are, because this is the one
	// pass that holds both halves at once: the reference the journal wrote and
	// the part it was rebuilt into (see [sessionFile.images]). It survives a
	// compaction marker for the same reason the title does — where a picture came
	// from is a fact about the file, not about the tail of the transcript.
	images := make(map[string]string)
	scanner := bufio.NewScanner(file)
	// A tool result can be tens of kilobytes; the default 64KiB token limit
	// would end the replay at the first big one.
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lines++
		var entry sessionEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		switch entry.Type {
		case "session":
			// The header names the format. A file written by a newer aforge can
			// hold entry types and fields this build does not know, and every
			// one of them would be dropped in silence — a session that resumes
			// looking complete and is not. Say so instead.
			var header sessionHeader
			if err := json.Unmarshal([]byte(line), &header); err != nil {
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
			messages = append(messages, message)
		case "compaction":
			// Everything before this marker is what the summary replaces. The
			// name is not a message and survives the cut: a compacted session
			// is the same session, still called what it was called.
			messages = append(messages[:0], textMessage("user", compactionNote(entry.Summary)))
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
		case "title":
			// LAST one wins. A name written twice is a name that was changed,
			// and the file's order is the order it was changed in.
			if named := strings.TrimSpace(entry.Title); named != "" {
				title = named
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return replayedSession{title: title, id: id, images: images, existed: lines > 0}, fmt.Errorf("session file: %w", err)
	}
	return replayedSession{
		messages: repairTranscript(messages),
		title:    title,
		id:       id,
		images:   images,
		existed:  lines > 0,
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

// replayedSession is what one pass over the journal recovered: the live
// transcript, the session's name, and whether the file had any lines at all.
//
// It is a struct rather than three returns because the three grow together —
// the name arrived here after the other two — and a reader of a call site
// should not have to count positions to know which bool is which.
type replayedSession struct {
	messages []ai.Message
	title    string
	// id is the header's session id, empty for a file that has no header yet.
	id string
	// images is where this file's pictures came from, keyed by [partKey] — the
	// index [sessionFile.images] is opened holding.
	images  map[string]string
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
	// Indexed as it is written, not only as it is replayed: a picture attached
	// an hour ago is one a rewind or a /compact can put back through the display
	// shaping in THIS process, long before anybody resumes the file.
	s.rememberParts(message, refs)
	var text strings.Builder
	for _, part := range message.Content {
		if part.Type == "text" {
			text.WriteString(part.Text)
		}
	}
	s.writeLine(sessionEntry{
		Type:       "message",
		Role:       message.Role,
		Content:    text.String(),
		ToolCalls:  message.ToolCalls,
		ToolCallID: message.ToolCallID,
		Parts:      refs,
		Timestamp:  stamp(),
	})
}

// appendCompaction journals one pass: the marker, then the kept tail again.
//
// The re-journal is what makes a compacted session resumable as itself. Replay
// discards everything before the marker — that is the marker's meaning — so
// the verbatim tail the pass deliberately kept has to sit on the far side of
// it, or a resume comes back holding the summary alone and the live session's
// last few exchanges are gone. Re-writing it costs one pass over at most
// keepRecentTokens; the alternative, a line count inside the marker, makes the
// file's meaning depend on arithmetic no reader of the file can check.
func (s *sessionFile) appendCompaction(summary string, tokensBefore int, kept []ai.Message) {
	s.writeLine(sessionEntry{
		Type:         "compaction",
		Summary:      summary,
		TokensBefore: tokensBefore,
		Timestamp:    stamp(),
	})
	for _, message := range kept {
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

// newSessionID is 16 random hex characters: enough to name every session a
// machine will ever hold without a coordinator.
func newSessionID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// crypto/rand failing is not a reason to refuse to open a session.
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw[:])
}
