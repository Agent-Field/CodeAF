package desktopbridge

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// attentionFixture is the rows fixture with the attention producer derived from it.
type attentionFixture struct {
	*rowsFixture
	mu  sync.Mutex
	log []inboxDelta
}

func newAttentionFixture(t *testing.T) *attentionFixture {
	f := &attentionFixture{rowsFixture: newRowsFixture(t)}
	newAttention(f.rows, func(kind string, payload any) {
		if kind != attentionKind {
			t.Errorf("kind = %q, want %q", kind, attentionKind)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.log = append(f.log, payload.(inboxDelta))
	})
	return f
}

func (f *attentionFixture) records() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.log)
}

func itemByID(items []InboxItem, id string) (InboxItem, bool) {
	for _, it := range items {
		if it.ID == id {
			return it, true
		}
	}
	return InboxItem{}, false
}

func TestAttentionListsQuestionsOfAttachedChatsWithTheirHead(t *testing.T) {
	f := newAttentionFixture(t)
	file := f.chat("aaaa", "Lexer", string(session.PresenceWaiting))
	f.rows.Scan()
	asks := []session.Question{
		{ID: 7, Kind: session.QuestionKind("task"), Ask: session.AskChoice, Head: " Which parser? "},
		{ID: 9, Ask: session.AskPermission, Head: "Run the migration?"},
	}
	f.rows.Attached("aaaa", attachedChat{SessionFile: file, Questions: len(asks), Open: asks}, true)
	items := f.rows.attentionSnapshot()
	q, ok := itemByID(items, "aaaa:7")
	if !ok || q.Head != "Which parser?" || q.Kind != attentionQuestion || q.SessionID != "aaaa" || q.SessionFile != file || q.Title != "Lexer" {
		t.Fatalf("question item = %+v (found %v)", q, ok)
	}
	if a, ok := itemByID(items, "aaaa:9"); !ok || a.Kind != attentionApproval || a.Head != "Run the migration?" {
		t.Fatalf("approval item = %+v (found %v)", a, ok)
	}
	if len(items) != 2 {
		t.Fatalf("items = %+v, want the two open questions and no row-level duplicate", items)
	}
}

func TestAnUnattachedChatNeedingYouIsListedWithoutInventedText(t *testing.T) {
	f := newAttentionFixture(t)
	file := f.chat("bbbb", "Parser", string(session.PresenceWaiting))
	f.chat("cccc", "Quiet", string(session.PresenceWorking))
	f.rows.Scan()
	items := f.rows.attentionSnapshot()
	if len(items) != 1 {
		t.Fatalf("items = %+v, want only the asking chat", items)
	}
	it := items[0]
	if it.ID != "bbbb:question" || it.Title != "Parser" || it.Kind != attentionQuestion || it.SessionFile != file {
		t.Fatalf("item = %+v", it)
	}
	if it.Head != "" || it.SessionID != "" {
		t.Fatalf("unattached item invented head %q or session %q", it.Head, it.SessionID)
	}
}

func TestAnsweringRemovesTheItemInOneRecord(t *testing.T) {
	f := newAttentionFixture(t)
	file := f.chat("aaaa", "Lexer", "")
	f.rows.Scan()
	before := f.records()
	ask := session.Question{ID: 7, Ask: session.AskChoice, Head: "Which parser?"}
	f.rows.Attached("aaaa", attachedChat{SessionFile: file, Questions: 1, Open: []session.Question{ask}}, true)
	if f.records() != before+1 {
		t.Fatalf("asking published %d records, want one", f.records()-before)
	}
	f.rows.Attached("aaaa", attachedChat{SessionFile: file}, true)
	if f.records() != before+2 {
		t.Fatalf("answering published %d records, want one", f.records()-before-1)
	}
	f.mu.Lock()
	last := f.log[len(f.log)-1]
	f.mu.Unlock()
	if len(last.Items) != 0 {
		t.Fatalf("record after the answer = %+v, want the item gone", last.Items)
	}
	// A pass that changes nothing publishes nothing.
	f.rows.Scan()
	if f.records() != before+2 {
		t.Fatalf("an unchanged scan published a record")
	}
}

func TestFailedChatsAreListedOnce(t *testing.T) {
	f := newAttentionFixture(t)
	f.chat("aaaa", "Lexer", "")
	index := `{"id":"1","title":"a","label":"a","status":"failed","sessionId":"aaaa","endedAt":"2026-10-01T10:00:00Z"}` + "\n" +
		`{"id":"2","title":"b","label":"b","status":"failed","sessionId":"aaaa","endedAt":"2026-10-02T10:00:00Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(f.root, "bucket", "tasks.jsonl"), []byte(index), 0o600); err != nil {
		t.Fatal(err)
	}
	f.rows.Scan()
	items := f.rows.attentionSnapshot()
	if len(items) != 1 || items[0].ID != "failed:aaaa" || items[0].Kind != attentionFailed || items[0].Head != "" {
		t.Fatalf("items = %+v, want one failed item for two failed tasks", items)
	}
}
