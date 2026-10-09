package desktopbridge

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
)

// ── A SAVED CHAT'S OPENING MESSAGE ──────────────────────────────────────────
//
// An open conversation is weighed on its title, its opening message and its
// recap; a saved one used to be weighed on its title and recap alone, because
// the session world's row carries no message text. That left every chat
// written before recaps existed — which, on the two real machines this was
// measured on, was every chat there was — with a title of four to eight words
// as its whole evidence. Six conversations implementing one desktop app, each
// in its own git worktree, shared one word across their titles; their opening
// messages shared the app's name, its toolkit and its build system. Grouping
// cannot find what it is not shown.
//
// SO THE OPENING MESSAGE IS READ FROM THE TRANSCRIPT, CHEAPLY AND ONCE. The
// person's first message is the second line of a transcript (after the
// session header), so the read stops at the first user message and never
// walks a long conversation; it is bounded in lines and bytes besides, and a
// chat's opening never changes, so it is remembered by chat id. A transcript
// that cannot be read, or has no message from the person in its first
// lines, has no opening — never an error. NOTHING HERE IS WRITTEN, AND NO
// MODEL IS ASKED.

const (
	// openingLines and openingBytes bound one read: the opening is normally
	// line two, and a session's header and first records are small.
	openingLines = 64
	openingBytes = 1 << 20
	// openingMemory bounds the cache; past it, it starts again.
	openingMemory = 2 * maxLibrary
)

type openingCache struct {
	mu   sync.Mutex
	seen map[string]string
}

// of is the chat's opening message, clipped to firstMessageRunes.
func (c *openingCache) of(chatID, transcript string) string {
	if chatID == "" || strings.TrimSpace(transcript) == "" {
		return ""
	}
	c.mu.Lock()
	if text, ok := c.seen[chatID]; ok {
		c.mu.Unlock()
		return text
	}
	c.mu.Unlock()
	text := savedOpening(transcript)
	// A freshly created chat may not have its first message yet. Missing
	// evidence is not permanent, so do not cache absence across its first turn.
	if text == "" {
		return ""
	}
	c.mu.Lock()
	if c.seen == nil || len(c.seen) >= openingMemory {
		c.seen = map[string]string{}
	}
	c.seen[chatID] = text
	c.mu.Unlock()
	return text
}

// savedOpening reads the first message the person wrote in a transcript.
func savedOpening(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	lines := bufio.NewScanner(io.LimitReader(f, openingBytes))
	lines.Buffer(make([]byte, 0, 64<<10), openingBytes)
	for n := 0; n < openingLines && lines.Scan(); n++ {
		line := lines.Bytes()
		// Most records are not a person's message; the cheap test spares
		// decoding them.
		if !strings.Contains(string(line), `"user"`) {
			continue
		}
		var record struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if json.Unmarshal(line, &record) != nil || record.Type != "message" || record.Role != "user" {
			continue
		}
		if text := strings.TrimSpace(record.Content); text != "" {
			return clipRunes(text, firstMessageRunes)
		}
	}
	return ""
}
