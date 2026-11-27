//go:build linux

package sessionhost

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"unicode"
)

// titleMost is the most of a chat's title that is kept, in characters. A title
// is a few words the model chose; this is the ceiling for one that is not, and
// it is applied here so that a chat's list row is small whatever the engine said.
const titleMost = 200

// titleLineMost is the longest engine line the tap will hold to read a title
// from. A line past it is passed along untouched and read for nothing: the
// welcome of a chat with a very long history is the only frame that can come
// near it, and the next ordinary status frame carries the same title.
const titleLineMost = 2 << 20

// titleKinds are the three frame kinds that carry a whole fact set, and so the
// chat's title: the welcome a window is given on arriving (inside it), the
// status frame the engine pushes whenever a fact moves, and the naming frame a
// window that asked to hear about names is sent. Each is matched by the start
// of its line because the engine writes a frame's kind first, so a line that is
// none of these is dropped after a few bytes and never parsed.
var titleKinds = [][]byte{[]byte(`{"kind":"welcome"`), []byte(`{"kind":"facts"`), []byte(`{"kind":"title"`)}

// titleHead is the number of bytes of a line that settle whether it is one of titleKinds.
const titleHead = 17

// titleTap reads the lines a chat's engine sends to one window and reports the
// chat's title as the engine states it. IT ONLY LOOKS: it is the second half of
// an io.TeeReader, its Write never fails and never waits, and the bytes it sees
// reach the window exactly as the engine wrote them.
//
// WHY THE RUNNER READS THE TITLE AT ALL. A chat that is not running has no
// engine to ask, and opening a wire to ask would start it, which is the one
// thing a list must not do. Every window of every chat, the owner's byte-for-byte
// one included, reaches its engine through this bridge, so this is the one place
// that sees the title each time it is said, and the chat's row keeps what was
// last said for the list to show with no process behind it.
type titleTap struct {
	// note receives each title that differs from the one before it on this wire. It must not wait.
	note func(title string)

	line    []byte
	skip    bool
	decided bool
	last    string
	// rev is the highest revision of a fact set this wire has read, the way a window's own copy of
	// the facts keeps it (internal/remote's replica).
	rev uint64
}

func (t *titleTap) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		end := bytes.IndexByte(p, '\n')
		if end < 0 {
			t.take(p)
			break
		}
		t.take(p[:end])
		t.finish()
		p = p[end+1:]
	}
	return n, nil
}

func (t *titleTap) take(chunk []byte) {
	if t.skip {
		return
	}
	if len(t.line)+len(chunk) > titleLineMost {
		t.skip, t.line = true, nil
		return
	}
	t.line = append(t.line, chunk...)
	if t.decided || len(t.line) < titleHead {
		return
	}
	t.decided = true
	if !titleCandidate(t.line) {
		t.skip, t.line = true, nil
	}
}

func titleCandidate(line []byte) bool {
	for _, kind := range titleKinds {
		if bytes.HasPrefix(line, kind) {
			return true
		}
	}
	return false
}

// finish ends one line. A candidate shorter than the head was never decided, so
// it is decided here, and the buffer is emptied whatever the outcome.
func (t *titleTap) finish() {
	line, skipped := t.line, t.skip
	t.line, t.skip, t.decided = t.line[:0], false, false
	if cap(t.line) > 64<<10 {
		// One long welcome must not leave its buffer held for the life of the wire.
		t.line = nil
	}
	if skipped || !titleCandidate(line) {
		return
	}
	t.read(line)
}

// titleFrame is the part of a frame the tap reads. Encoding and Data are how
// the engine sends a payload of 32 KiB or more when the window offered gzip.
type titleFrame struct {
	Kind     string          `json:"kind"`
	Payload  json.RawMessage `json:"payload"`
	Encoding string          `json:"encoding"`
	Data     []byte          `json:"data"`
}

// titleFacts is the engine's statement of its facts, reduced to the two fields
// the tap uses: the revision that orders one statement against another, and the
// title. internal/remote's FactsPush is the type that writes it, and the
// sessionhost tests marshal that very type through the tap, so a rename there
// fails here rather than leaving every list untitled.
type titleFacts struct {
	Rev   uint64 `json:"rev"`
	Facts struct {
		Title string `json:"title"`
	} `json:"facts"`
}

func (t *titleTap) read(line []byte) {
	var frame titleFrame
	if json.Unmarshal(line, &frame) != nil {
		return
	}
	payload := frame.Payload
	if frame.Encoding == "gzip" && len(frame.Data) > 0 {
		reader, err := gzip.NewReader(bytes.NewReader(frame.Data))
		if err != nil {
			return
		}
		defer reader.Close()
		if payload, err = io.ReadAll(io.LimitReader(reader, titleLineMost)); err != nil {
			return
		}
	}
	var facts titleFacts
	switch frame.Kind {
	case "welcome":
		var welcome struct {
			Facts *titleFacts `json:"facts"`
		}
		// A welcome with no facts is an engine that states none; it says nothing about the title.
		if json.Unmarshal(payload, &welcome) != nil || welcome.Facts == nil {
			return
		}
		facts = *welcome.Facts
		// A welcome is the first statement about whichever conversation the engine has open now, so it
		// replaces the baseline instead of being compared with it: after a swap its revision may be lower.
		t.rev = facts.Rev
	case "facts", "title":
		if json.Unmarshal(payload, &facts) != nil {
			return
		}
		// A status frame can overtake a naming frame (the engine writes them from two places), and each
		// carries the whole fact set as of its revision, so the highest revision seen is the truth and
		// an older frame arriving late must not put an old title back.
		if facts.Rev <= t.rev {
			return
		}
		t.rev = facts.Rev
	default:
		return
	}
	// A statement with no title says nothing: a chat's title never goes back to nothing, because a list
	// row that lost its name between two windows would be worse than one that is a conversation late.
	title := cleanTitle(facts.Facts.Title)
	if title == "" || title == t.last {
		return
	}
	t.last = title
	t.note(title)
}

// cleanTitle makes a title the engine sent safe to keep and to draw on other
// people's screens: control characters go, whitespace collapses to single spaces
// and the length is capped. An empty result is a chat with no title.
func cleanTitle(said string) string {
	said = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r) && !unicode.IsSpace(r):
			return -1
		// The characters that reverse the direction of the text after them, which would let one title be
		// drawn as another. Joiners and the rest of the format characters stay, because emoji need them.
		case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
			return -1
		}
		return r
	}, said)
	said = strings.Join(strings.Fields(said), " ")
	if runes := []rune(said); len(runes) > titleMost {
		said = strings.TrimSpace(string(runes[:titleMost]))
	}
	return said
}

// titleFeed carries the titles one wire reads to the chat's row, on a goroutine of its own that the supervisor's
// shutdown waits for. The tap runs in the stream's copy loop, so it must never wait on the registry's lock or its disk;
// a title that arrives while the last one is still waiting replaces it, because only the newest is of any use.
type titleFeed struct {
	mu     sync.Mutex
	ch     chan string
	closed bool
}

func (s *supervisor) feedTitles(id string) *titleFeed {
	f := &titleFeed{ch: make(chan string, 1)}
	// The wire that asks is itself in the group, so the count cannot be at zero while this is added.
	s.wireWG.Add(1)
	go func() {
		defer s.wireWG.Done()
		for title := range f.ch {
			s.noteTitle(id, title)
		}
	}()
	return f
}

// note never waits. The channel holds one title and is emptied here first, under the lock that every send takes, so
// the send cannot find it full.
func (f *titleFeed) note(title string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	select {
	case <-f.ch:
	default:
	}
	f.ch <- title
}

func (f *titleFeed) close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.ch)
	}
}

// noteTitle keeps the title the engine last stated for a chat. The row is read again under the lock, so
// a chat that was stopped, deleted or doomed while the title was on its way is left as it is, and a title
// that has not changed writes nothing.
func (s *supervisor) noteTitle(id, title string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, exists := s.registry.sessions[id]
	if !exists || row.Doomed || row.Title == title {
		return
	}
	row.Title = title
	if s.registry.put(row) != nil {
		s.log.Warn("could not save a chat's title", "id", id)
	}
}
