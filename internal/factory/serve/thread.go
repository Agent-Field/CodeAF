package serve

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/session"
)

// thread is GET /v1/jobs/{id}/thread. The chat id is the one Talk already
// kept on the job; asking Talk when the job has none would make a
// conversation, and this read must not. The transcript reader never writes
// the file. No id, no file, or no spoken lines is an empty list.
func (h *Handler) thread(w http.ResponseWriter, r *http.Request) {
	it, ok := h.findJob(w, r)
	if !ok {
		return
	}
	chat := strings.TrimSpace(it.Talk)
	if chat != "" && h.seam.Talk != nil {
		named, err := h.seam.Talk(r.Context(), it.ID)
		if h.failed(w, err) {
			return
		}
		if named = strings.TrimSpace(named); named != "" {
			chat = named
		}
	}
	h.write(w, http.StatusOK, struct {
		Factory  api.Factory   `json:"factory"`
		Messages []api.Message `json:"messages"`
	}{h.factory, transcriptMessages(chat)})
}

func transcriptMessages(path string) []api.Message {
	path = strings.TrimSpace(path)
	if path == "" {
		return []api.Message{}
	}
	entries := spoken(session.ReadTranscript(path))
	clocks := messageClocks(path)
	out := make([]api.Message, 0, len(entries))
	next := 0
	for _, entry := range entries {
		msg := api.Message{From: speaker(entry.Role), Text: entry.Text}
		if entry.Steer != nil && !entry.Steer.At.IsZero() {
			msg.At = entry.Steer.At
		} else if i := matchClock(clocks, next, entry.Text); i >= 0 {
			msg.At = clocks[i].at
			next = i + 1
		}
		out = append(out, msg)
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
