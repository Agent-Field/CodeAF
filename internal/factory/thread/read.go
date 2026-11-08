// Package thread reads spoken manager lines on the worker that owns the journal.
package thread

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
)

func Read(path string) []factory.Message {
	path = strings.TrimSpace(path)
	if path == "" {
		return []factory.Message{}
	}
	key, keyed := readKeyOf(path)
	if keyed {
		reads.mu.Lock()
		last, ok := reads.last[path]
		reads.mu.Unlock()
		if ok && last.key.same(key) {
			return append([]factory.Message(nil), last.out...)
		}
	}
	out := read(path)
	if keyed {
		reads.mu.Lock()
		if len(reads.last) >= readsMost {
			reads.last = map[string]readAnswer{}
		}
		reads.last[path] = readAnswer{key: key, out: append([]factory.Message(nil), out...)}
		reads.mu.Unlock()
	}
	return out
}

// ── a journal is parsed once per version of it ─────────────────────────────
//
// A HELD JOB'S RUNNER READ ITS MANAGER'S WHOLE JOURNAL ON EVERY BEAT. The
// worker's flush runs every two seconds for each job it holds and parsed the
// manager's transcript twice (its entries, then its clocks) to find manager
// lines nobody had written; the seventh live A+B run's idle profile on macOS
// put a seventh of the engine there with two jobs held. A journal is appended
// to or replaced, never edited in place, so the file's identity (size, time,
// inode) and its folder's (a segment or sidecar added beside it) say whether
// the answer read last is still the answer.

// readsMost bounds the journals remembered.
const readsMost = 256

type readKey struct {
	file, dir os.FileInfo
}

// same says two looks saw one unchanged journal in one unchanged folder.
func (a readKey) same(b readKey) bool {
	return sameInfo(a.file, b.file) && sameInfo(a.dir, b.dir)
}

func sameInfo(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime()) && a.Mode() == b.Mode()
}

type readAnswer struct {
	key readKey
	out []factory.Message
}

var reads = struct {
	mu   sync.Mutex
	last map[string]readAnswer
}{last: map[string]readAnswer{}}

// readParses counts the journals parsed, so a test can say an unchanged one
// was not parsed again.
var readParses atomic.Int64

func readKeyOf(path string) (readKey, bool) {
	file, err := os.Stat(path)
	if err != nil {
		return readKey{}, false
	}
	dir, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return readKey{}, false
	}
	return readKey{file: file, dir: dir}, true
}

// read is [Read] without the memory: the journal parsed as it is now.
func read(path string) []factory.Message {
	readParses.Add(1)
	entries := spoken(session.ReadTranscript(path))
	clocks := messageClocks(path)
	out := make([]factory.Message, 0, len(entries))
	occurrences := map[string]int{}
	next := 0
	for _, entry := range entries {
		msg := factory.Message{From: speaker(entry.Role), Text: entry.Text}
		if entry.Steer != nil && !entry.Steer.At.IsZero() {
			msg.At = entry.Steer.At
		} else if i := matchClock(clocks, next, entry.Text); i >= 0 {
			msg.At = clocks[i].at
			next = i + 1
		}
		key := fmt.Sprintf("%s:%s:%s", msg.From, msg.At.Format(time.RFC3339Nano), msg.Text)
		occurrences[key]++
		msg.ID = fmt.Sprintf("line_%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%d", key, occurrences[key]))))
		if len(msg.Text) > factory.MessageTextMost {
			msg.Text = strings.ToValidUTF8(msg.Text[:factory.MessageTextMost], "")
		}
		out = append(out, msg)
	}
	if len(out) > factory.ThreadMost {
		out = out[len(out)-factory.ThreadMost:]
	}
	return out
}

// speaker uses the product word for the manager. Every other role keeps the
// transcript's own word; the file does not carry a person's handle.
func speaker(role string) string {
	if role == "assistant" {
		return "manager"
	}
	return role
}

// spoken is the conversation the reader returns, without tool rows. A tool
// row is the call, not a line someone said. The whole record is the reader's
// earlier region followed by the live tail, which is how a compacted file
// is one conversation rather than two copies.
func spoken(rec session.Record) []session.DisplayEntry {
	entries := rec.Entries
	if rec.Floor >= 0 && rec.Floor <= len(rec.Entries) {
		entries = rec.Entries[rec.Floor:]
		if len(rec.Earlier) > 0 {
			all := make([]session.DisplayEntry, 0, len(rec.Earlier)+len(entries))
			all = append(all, rec.Earlier...)
			entries = append(all, entries...)
		}
	}
	out := make([]session.DisplayEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Tool != "" || entry.Role == "tool" || entry.Role == "note" {
			continue
		}
		if strings.TrimSpace(entry.Text) == "" {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// clockLine is one journal message's clock. Display entries carry no time
// except on a steer, so the timestamp stays on the journal line and is
// matched back by the words the reader returned.
type clockLine struct {
	text string
	at   time.Time
}

func messageClocks(path string) []clockLine {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	var out []clockLine
	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var line struct {
			Type      string `json:"type"`
			Role      string `json:"role"`
			Content   string `json:"content"`
			Timestamp string `json:"timestamp"`
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil {
			continue
		}
		if line.Type != "message" || line.Role == "tool" {
			continue
		}
		text := strings.TrimSpace(line.Content)
		if text == "" {
			continue
		}
		out = append(out, clockLine{text: text, at: parseStamp(line.Timestamp)})
	}
	return out
}

func matchClock(lines []clockLine, start int, text string) int {
	want := strings.TrimSpace(text)
	for i := start; i < len(lines); i++ {
		if lines[i].text == want {
			return i
		}
	}
	return -1
}

func parseStamp(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	t, _ := time.Parse(time.RFC3339, s)
	return t
}
