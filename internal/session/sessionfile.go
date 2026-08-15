package session

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
}

// Title is the name this file was opened holding, empty when it has none.
func (s *sessionFile) Title() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.title
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

	if !replayed.existed {
		// The header names the session once. A resumed file keeps its
		// original: the id is what a second window looks a session up by.
		journal.writeLine(sessionHeader{
			Type:      "session",
			Version:   sessionFileVersion,
			ID:        newSessionID(),
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
		lines    int
	)
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
		case "message":
			if entry.Role == "" {
				continue
			}
			messages = append(messages, ai.Message{
				Role:       entry.Role,
				Content:    []ai.ContentPart{{Type: "text", Text: entry.Content}},
				ToolCalls:  entry.ToolCalls,
				ToolCallID: entry.ToolCallID,
			})
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
		return replayedSession{title: title, existed: lines > 0}, fmt.Errorf("session file: %w", err)
	}
	return replayedSession{
		messages: repairTranscript(messages),
		title:    title,
		existed:  lines > 0,
	}, nil
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
	existed  bool
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

func (s *sessionFile) appendMessage(message ai.Message) {
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
