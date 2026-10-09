package desktopbridge

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// markFailureSeen is POST /world/failures/seen: the person looked at a failed
// task, so it stops lighting the Inbox in EVERY window and device. The mark is
// the conversation's own (session.MarkFailureSeen); this door only authenticates
// (the caller already passed the token check), validates, and tells the feed to
// read again so other windows learn from their stream rather than from a poll.
//
// IT MAKES NO MODEL CALL AND ATTACHES NOTHING: a conversation nobody has open is
// exactly the one whose failure is waiting in the Inbox.
func (b *Bridge) markFailureSeen(w http.ResponseWriter, r *http.Request) {
	var ask struct {
		Session string `json:"session"`
		At      string `json:"at"`
		// Task is optional corroboration: when given it must be the failure landed at At.
		Task string `json:"task"`
	}
	if !decodeMax(w, r, &ask, 4<<10) {
		return
	}
	id := strings.TrimSpace(ask.Session)
	at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(ask.At))
	if id == "" || err != nil {
		fail(w, 400, "session and at (the failure's landing time) are required")
		return
	}
	b.mu.Lock()
	history := b.history
	b.mu.Unlock()
	if history == nil {
		history = &History{}
	}
	dir, ok := history.folder(id)
	if !ok {
		fail(w, 404, "no such conversation")
		return
	}
	result, err := session.MarkFailureSeen(dir, id, ask.Task, at)
	switch {
	case errors.Is(err, session.ErrNoSuchFailure):
		fail(w, 409, err.Error())
		return
	case err != nil:
		fail(w, 500, "could not record that the failure was seen")
		return
	}
	if result.Changed {
		b.worldFeed().refresh()
	}
	write(w, map[string]any{"session": id, "through": result.Through.UTC().Format(time.RFC3339Nano), "changed": result.Changed, "unseenFailed": result.Unseen})
}
