package session

// Reading a conversation back WITHOUT opening it.
//
// History lists conversations nobody has open, and a person reading one back
// wants its words — theirs and the assistant's — not its tool traffic, its
// bookkeeping lines or the notes the session wrote to itself. [Peek] answers the
// picker's three questions and stops; this answers the fourth, "show me what was
// said", with the same discipline: open, scan forward, close, never lock, never
// write.
//
// TWO LINES OF THE JOURNAL CHANGE WHAT WAS SAID AND ARE APPLIED. A `rewind`
// takes the last N messages back, and a `compaction` marker is followed by the
// pass's own rewritten copy of the window it replaced ([sessionEntry.Window]) —
// the same conversation journaled twice. Reading both would show a person their
// conversation twice, so the copy is skipped; a marker that does not say how
// long the copy is leaves the lines as they are, which can repeat a stretch and
// never loses one.

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"time"
)

// ConversationMessage is one thing said in a conversation.
type ConversationMessage struct {
	// Role is "user" or "assistant".
	Role string
	Text string
	// At is the journal line's own timestamp, zero for a line that carries none.
	At time.Time
}

// ReadConversation returns the words of one transcript, oldest first: the
// person's messages and the assistant's text, nothing else. A file that is
// missing or unreadable answers nothing.
func ReadConversation(path string) []ConversationMessage {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	// every message line is kept, shown or not, because a rewind counts them all.
	type line struct {
		message ConversationMessage
		shown   bool
	}
	var lines []line
	skipCopy := 0
	scanner := bufio.NewScanner(file)
	// The buffer [Peek] takes, for its reason: one enormous tool result would
	// otherwise end the scan before the words after it.
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for scanner.Scan() {
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		var entry sessionEntry
		if json.Unmarshal([]byte(raw), &entry) != nil {
			continue
		}
		switch entry.Type {
		case "message":
			if entry.Role == "" {
				continue
			}
			if skipCopy > 0 {
				skipCopy--
				continue
			}
			at, _ := time.Parse(time.RFC3339Nano, entry.Timestamp)
			text := strings.TrimSpace(entry.Content)
			shown := text != "" && !entry.Note && !isCodeafNote(text) &&
				(entry.Role == "user" || (entry.Role == "assistant"))
			lines = append(lines, line{ConversationMessage{Role: entry.Role, Text: text, At: at}, shown})
		case "rewind":
			if entry.Dropped >= len(lines) {
				lines = lines[:0]
			} else if entry.Dropped > 0 {
				lines = lines[:len(lines)-entry.Dropped]
			}
		case "compaction":
			skipCopy = compactionOverlap(entry)
			if skipCopy < 0 {
				skipCopy = 0
			}
		}
	}
	var out []ConversationMessage
	for _, l := range lines {
		if l.shown {
			out = append(out, l.message)
		}
	}
	return out
}
