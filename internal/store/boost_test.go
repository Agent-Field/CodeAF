package store

import (
	"path/filepath"
	"testing"
)

func TestReplyModelAttributionSurvivesStoreReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "boost.db")
	graph, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.PostMessage(Message{
		SessionID: "boost", Role: RoleUser, Body: "hard question", Model: "anthropic/claude-opus-5",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.PostMessage(Message{
		SessionID: "boost", Role: RoleAgent, Body: "careful answer", Model: "anthropic/claude-opus-5-2026-08-01",
	}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	messages, err := reopened.Messages("boost", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].Model != "anthropic/claude-opus-5" ||
		messages[1].Model != "anthropic/claude-opus-5-2026-08-01" {
		t.Fatalf("reopened model metadata = %+v", messages)
	}
}
