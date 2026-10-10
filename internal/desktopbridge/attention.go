package desktopbridge

import (
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/session"
)

// The `attention` producer: everything across every conversation that is waiting on
// the person, as one flat list the Inbox draws. It is derived from the `world` rows
// producer and publishes nothing the rows do not already know.
//
// NO TEXT IS INVENTED. A conversation a window holds knows its open questions, so its
// items carry the question's own head. One nobody holds is known only by its row, so
// its item is the title and the kind and nothing else. The Inbox answers either the
// same way: POST /sessions {sessionFile} attaches (and dedups against a window already
// holding the file), then /answer names the question. An answered question leaves the
// attached conversation's open set, so the next record omits the item in one step.

// attentionKind is the record type this producer publishes.
const attentionKind = "attention"

// Item kinds. An unattached chat can only say "question": its row counts questions
// but cannot say whether one is an approval, so the kind is not guessed.
const (
	attentionQuestion = "question"
	attentionApproval = "approval"
	attentionFailed   = "failed"
)

// InboxItem is one thing waiting on the person. Head is omitted unless a window holds
// the conversation, and SessionID unless it is attached.
type InboxItem struct {
	ID          string `json:"id"`
	ChatID      string `json:"chatId"`
	SessionFile string `json:"sessionFile,omitempty"`
	SessionID   string `json:"sessionId,omitempty"`
	Title       string `json:"title,omitempty"`
	Kind        string `json:"kind"`
	Head        string `json:"head,omitempty"`
	At          string `json:"at,omitempty"`
}

// inboxDelta is the payload of an `attention` record: the whole current list, because
// the list is small and a delta of questions invites a reader to keep one the engine
// withdrew.
type inboxDelta struct {
	Items []InboxItem `json:"items"`
}

type attention struct {
	rows *worldRows
	// publish receives each changed list. It runs under the rows producer's lock, so
	// records leave in order; it must not call back into either producer.
	publish func(kind string, payload any)

	mu   sync.Mutex
	last string // the list as last published, serialised by key
}

// newAttention derives the list from rows and publishes it whenever it changes.
func newAttention(rows *worldRows, publish func(string, any)) *attention {
	if publish == nil {
		publish = func(string, any) {}
	}
	a := &attention{rows: rows, publish: publish}
	rows.mu.Lock()
	rows.changed = a.refreshLocked
	rows.mu.Unlock()
	return a
}

// Full is the current list, reading the disk once if nothing has yet.
func (a *attention) Full() []InboxItem {
	a.rows.Full()
	return a.rows.attentionSnapshot()
}

// attentionSnapshot is the list as the rows stand now.
func (w *worldRows) attentionSnapshot() []InboxItem {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.attentionLocked()
}

// refreshLocked runs with the rows lock held and publishes the list if it moved.
func (a *attention) refreshLocked() {
	items := a.rows.attentionLocked()
	var sig strings.Builder
	for _, it := range items {
		sig.WriteString(it.ID + "|" + it.Title + "|" + it.Head + "|" + it.At + "|" + it.SessionFile + "\n")
	}
	a.mu.Lock()
	same := a.last == sig.String()
	a.last = sig.String()
	a.mu.Unlock()
	if !same {
		a.publish(attentionKind, inboxDelta{Items: items})
	}
}

// attentionLocked builds the list from the settled rows. Archived chats ask nothing.
func (w *worldRows) attentionLocked() []InboxItem {
	items := []InboxItem{}
	for id, row := range w.rows {
		if row.Archived {
			continue
		}
		if chat, ok := w.attached[id]; ok && len(chat.Open) > 0 {
			for _, q := range chat.Open {
				kind := attentionQuestion
				if q.Ask == session.AskPermission {
					kind = attentionApproval
				}
				token := strconv.FormatUint(q.ID, 10)
				if q.Ref != "" {
					token = q.Ref
				}
				items = append(items, InboxItem{
					ID: id + ":" + token, ChatID: id, SessionFile: row.SessionFile, SessionID: id,
					Title: row.Title, Kind: kind, Head: strings.TrimSpace(q.Head), At: row.UpdatedAt,
				})
			}
		} else if row.NeedsYou > 0 {
			items = append(items, InboxItem{
				ID: id + ":question", ChatID: id, SessionFile: row.SessionFile,
				Title: row.Title, Kind: attentionQuestion, At: row.UpdatedAt,
			})
		}
		// One failed item per chat however many tasks failed: the Inbox opens the chat.
		if row.Failed > 0 {
			items = append(items, InboxItem{
				ID: "failed:" + id, ChatID: id, SessionFile: row.SessionFile,
				Title: row.Title, Kind: attentionFailed, At: row.UpdatedAt,
			})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}
