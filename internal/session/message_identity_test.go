package session

import (
	"path/filepath"
	"testing"
)

func TestOrdinaryNewUserMessagesHaveDistinctIdsWithoutCopyingContent(t *testing.T) {
	original := userMessage{message: textMessage("user", "repeat the same words")}
	a, b := ensureUserIdentity(original), ensureUserIdentity(original)
	if a.person == nil || a.person.Composed == nil || a.person.Composed.MessageID == "" || a.person.Composed.MessageID == b.person.Composed.MessageID {
		t.Fatalf("new equal-word messages lost distinct identity: %+v %+v", a.person, b.person)
	}
	if &a.message.Content[0] != &original.message.Content[0] || a.person.Composed.Delivery != nil || original.person != nil {
		t.Fatal("identity copied content, invented delivery or changed its caller")
	}
	if got := ensureUserIdentity(a); got.person.Composed.MessageID != a.person.Composed.MessageID {
		t.Fatal("existing accepted identity changed")
	}
	if got := ensureUserIdentity(userMessage{message: original.message, authored: true}); got.person != nil {
		t.Fatal("an authored note became a user request")
	}
}

func TestOrdinaryMessageIdentitySurvivesRealJournalReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	first, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = path })
	// This is the real new-message record path, with no model/network request.
	first.mu.Lock()
	first.recordUserLocked(userMessage{message: textMessage("user", "ordinary words")})
	first.mu.Unlock()
	entries := first.Transcript()
	last := entries[len(entries)-1]
	if last.Person == nil || last.Person.Composed == nil || last.Person.Composed.MessageID == "" {
		t.Fatalf("live identity missing: %+v", last)
	}
	id := last.Person.Composed.MessageID
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := newAgent(Config{Workspace: first.config.Workspace, Model: "test/model", System: "SYSTEM", SessionFile: path}, &scriptedCompleter{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	entries = reopened.Transcript()
	last = entries[len(entries)-1]
	if last.Person == nil || last.Person.Composed == nil || last.Person.Composed.MessageID != id {
		t.Fatalf("reopen changed identity: %+v", last)
	}
}
