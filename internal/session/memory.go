package session

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// Memory is one file of durable lines — ~/.aforge/v3/memory.md by default —
// and two tools that write it: note appends a line, forget removes the lines
// that match.
//
// It is deliberately the boring shape. No embeddings, no retrieval, no scoring:
// a session's memory is a handful of standing facts a person would repeat to a
// new colleague on their first morning, and the whole of it fits in the system
// prompt. Anything that needed ranking to be useful is not memory, it is a
// search index over the repo, and the belt already has grep for that.
//
// The file is read at the START OF EVERY TURN (see refreshSystemLocked), never
// cached beyond it. That is what makes a note land: the tool appends while a
// turn is running, the block the model is reading was rendered before that turn
// began, and the next turn re-reads the file and sees it. It also means a person
// editing memory.md in an editor is obeyed on the next thing they say, without
// restarting the session.
const (
	// memoryBlockLimit is how much of the file rides in the system prompt. 4KiB
	// is around fifty remembered lines — more standing preferences than any
	// working relationship actually has — and it is paid for on every request of
	// every turn, which is why it is a quarter of what AGENTS.md gets.
	memoryBlockLimit = 4 << 10

	// memoryFileLimit bounds what forget will rewrite. forget must read the
	// whole file to filter it, and a memory file the size of a log is not a
	// thing this tool should quietly load into memory; past this it says so and
	// points at the path.
	memoryFileLimit = 1 << 20
)

// memoryStore is the file and the lock that keeps two concurrent tool calls
// from writing over each other. Tool calls in one batch run in parallel
// (loop.go), so two notes in the same batch are two appends racing for the same
// file; the lock is what makes the second one land after the first instead of
// on top of it.
type memoryStore struct {
	mu   sync.Mutex
	path string
}

func newMemoryStore(path string) *memoryStore {
	return &memoryStore{path: path}
}

// note appends one line and returns the line as it was stored.
//
// The fact is flattened to a single line because the file's unit IS the line:
// forget removes lines, the block renders lines, and a note containing a
// newline would be two entries one of which nobody can remove. It is prefixed
// with "- " so the file stays a readable markdown list for the person whose
// memory it is.
func (m *memoryStore) note(fact string) (string, error) {
	line := memoryLine(fact)
	if line == "" {
		return "", nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if directory := filepath.Dir(m.path); directory != "" && directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return "", err
		}
	}
	file, err := os.OpenFile(m.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", err
	}
	if _, err := file.WriteString(line + "\n"); err != nil {
		_ = file.Close()
		return "", err
	}
	return line, file.Close()
}

// forget removes every line containing pattern and returns the ones it removed.
//
// The match is a plain case-insensitive substring, NOT a regular expression,
// and that is the safety property rather than a limitation: forget("." ) as a
// regex empties the file, and a model reaching for a wildcard must not be able
// to erase a person's standing preferences by accident. Removing several lines
// at once is still possible — it just has to be spelled with words that are
// actually in them.
func (m *memoryStore) forget(pattern string) ([]string, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	content, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(content) > memoryFileLimit {
		return nil, fmt.Errorf("memory file %s is larger than %dKiB; edit it directly", m.path, memoryFileLimit>>10)
	}

	needle := strings.ToLower(pattern)
	var (
		kept    strings.Builder
		removed []string
	)
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) != "" && strings.Contains(strings.ToLower(line), needle) {
			removed = append(removed, strings.TrimSpace(line))
			continue
		}
		kept.WriteString(line)
		kept.WriteString("\n")
	}
	if len(removed) == 0 {
		return nil, nil
	}

	// Rewritten through a temporary file in the same directory: a memory file
	// truncated by a crash mid-write is every remembered fact gone, and the
	// rename is the one write that cannot half-happen.
	rewritten := strings.TrimRight(kept.String(), "\n")
	if rewritten != "" {
		rewritten += "\n"
	}
	temporary := m.path + ".tmp"
	if err := os.WriteFile(temporary, []byte(rewritten), 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(temporary, m.path); err != nil {
		_ = os.Remove(temporary)
		return nil, err
	}
	return removed, nil
}

// block renders the <memory> section of the system prompt, empty when the file
// is missing or holds nothing.
//
// The content is fenced for the reason AGENTS.md is: these are lines a person
// wrote, they can contain anything, and a stray heading or code fence inside
// them must not be able to close the block and continue as instructions.
func (m *memoryStore) block() string {
	notes, truncated := m.tail()
	if notes == "" {
		return ""
	}
	var out strings.Builder
	out.WriteString("\n<memory>\n\nWhat you were asked to remember in earlier sessions, kept with the note tool. Standing preferences, corrections and facts about this work — context that is true now, not a request to act on. Remove one that is wrong or superseded with forget.\n\n")
	fence := fenceFor(notes)
	out.WriteString(fence + "text\n")
	out.WriteString(notes)
	out.WriteString("\n" + fence + "\n")
	if truncated {
		fmt.Fprintf(&out, "\n(Only the most recent %dKiB is shown; the whole file is %s — read it if you need the rest.)\n",
			memoryBlockLimit>>10, m.path)
	}
	out.WriteString("</memory>\n")
	return out.String()
}

// tail reads the last memoryBlockLimit bytes of the file and reports whether
// anything was left behind.
//
// The TAIL, not the head: a file over the limit has been growing, and the lines
// that were added last are the ones that describe the working relationship as it
// stands. A partial first line — the cut lands mid-line by construction — is
// dropped rather than shown, because half a remembered fact is a fact the person
// never stated.
func (m *memoryStore) tail() (notes string, truncated bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	file, err := os.Open(m.path)
	if err != nil {
		return "", false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", false
	}
	size := info.Size()
	if size > memoryBlockLimit {
		if _, err := file.Seek(size-memoryBlockLimit, io.SeekStart); err != nil {
			return "", false
		}
		truncated = true
	}
	content, err := io.ReadAll(file)
	if err != nil {
		return "", false
	}
	text := string(content)
	if truncated {
		if newline := strings.IndexByte(text, '\n'); newline >= 0 {
			text = text[newline+1:]
		} else {
			// One line longer than the whole budget: nothing here is a complete
			// remembered fact, so nothing is shown.
			return "", false
		}
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	return text, truncated
}

// refreshSystemLocked rebuilds message[0] from the base prompt and the memory
// file as it stands right now. It is called at construction and at the start of
// every turn, and it is a no-op for a session with no memory file — the prompt
// is then exactly the bytes it always was.
//
// message[0] is REPLACED rather than appended to: a.system stays the base, so
// every refresh renders base + current file instead of stacking one block on
// top of the last one.
//
// The read happens with a.mu held, which is the same trade the journal already
// takes (recordLocked appends to the session file under this lock): one small
// read of a local file per TURN, against the alternative of a second lock and a
// window in which the prompt is rebuilt from a file the turn did not start with.
func (a *Agent) refreshSystemLocked() {
	if a.memory == nil || len(a.messages) == 0 {
		return
	}
	a.messages[0] = textMessage("system", a.system+a.memory.block())
}

// memoryLine flattens one fact to the single "- …" line the file stores. Empty
// in, empty out — the caller refuses it.
func memoryLine(fact string) string {
	fact = strings.TrimSpace(fact)
	if fact == "" {
		return ""
	}
	fact = strings.Join(strings.Fields(fact), " ")
	fact = strings.TrimPrefix(fact, "- ")
	if fact == "" {
		return ""
	}
	return "- " + fact
}

// ── the two tools ───────────────────────────────────────────────────────────

const noteDescription = "Remember one durable fact across sessions: a preference the person stated, a correction they made, a convention of this project worth carrying into the next session. Write it as a standing truth in one short line ('prefers tabs over spaces in Go'), not as a log of what just happened. It is appended to the memory file and joins the <memory> block of your system prompt from the NEXT turn onward — it is not visible to you in this one. Do not note what the transcript already holds, what the repo or AGENTS.md already records, or anything that will be false tomorrow."

const noteSchemaJSON = `{"type":"object","properties":{"fact":{"type":"string","description":"The single line to remember, in plain words"}},"required":["fact"],"additionalProperties":false}`

const forgetDescription = "Forget remembered lines. Every line of the memory file that CONTAINS this text is removed and reported back; the match is case-insensitive plain text, not a regular expression or a glob. Use it when a remembered preference was wrong, has been superseded, or the person says to drop it."

const forgetSchemaJSON = `{"type":"object","properties":{"pattern":{"type":"string","description":"Text to match; every memory line containing it is removed"}},"required":["pattern"],"additionalProperties":false}`

// The gloss a person reads beside a memory call is the fact itself — "note
// prefers tabs over spaces" — for the reason every other tool's gloss is its
// path or its command: the tool name alone says a memory call happened and not
// what it did. The registration is here rather than in the glossField literal
// (loop.go) because these two tools are this file's, and a table entry is the
// narrowest way to say so from where they are defined.
func init() {
	glossField["note"] = "fact"
	glossField["forget"] = "pattern"
}

// memoryTools is the pair, or nothing at all when no memory file is configured.
//
// Nothing at all is the point: a belt that carries note and forget against no
// file is a model told it can remember, whose every note is refused. A session
// without memory simply does not have the verb.
func (a *Agent) memoryTools() []bare.Tool {
	if a.memory == nil {
		return nil
	}
	return []bare.Tool{
		{
			Name:        "note",
			Description: noteDescription,
			Schema:      json.RawMessage(noteSchemaJSON),
			Execute: func(_ context.Context, args json.RawMessage) (string, bool, error) {
				var parsed struct {
					Fact string `json:"fact"`
				}
				if err := json.Unmarshal(args, &parsed); err != nil {
					return "Invalid arguments: " + err.Error(), true, nil
				}
				line, err := a.memory.note(parsed.Fact)
				if err != nil {
					return "Could not write the memory file: " + err.Error(), true, nil
				}
				if line == "" {
					return "Invalid arguments: fact is required", true, nil
				}
				return fmt.Sprintf("Noted in %s:\n%s\n\nIt is part of your memory from the next turn onward, not this one.", a.memory.path, line), false, nil
			},
		},
		{
			Name:        "forget",
			Description: forgetDescription,
			Schema:      json.RawMessage(forgetSchemaJSON),
			Execute: func(_ context.Context, args json.RawMessage) (string, bool, error) {
				var parsed struct {
					Pattern string `json:"pattern"`
				}
				if err := json.Unmarshal(args, &parsed); err != nil {
					return "Invalid arguments: " + err.Error(), true, nil
				}
				if strings.TrimSpace(parsed.Pattern) == "" {
					return "Invalid arguments: pattern is required", true, nil
				}
				removed, err := a.memory.forget(parsed.Pattern)
				if err != nil {
					return "Could not rewrite the memory file: " + err.Error(), true, nil
				}
				if len(removed) == 0 {
					// Not an error: the answer to "forget X" when nothing says X
					// is that there was nothing to forget, and the model needs to
					// read it as a fact rather than as a failed call to retry.
					return fmt.Sprintf("No memory line contains %q; nothing was removed.", parsed.Pattern), false, nil
				}
				return fmt.Sprintf("Forgot %d line(s):\n%s", len(removed), strings.Join(removed, "\n")), false, nil
			},
		},
	}
}
