package store

import (
	"path/filepath"
	"reflect"
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
	// One message may carry both lanes' metadata: model attribution from the
	// boost lane and structured progress from the compile journal. Replay must
	// keep both.
	wantProgress := &MessageProgress{Phase: "designing the approach", Done: 1, Total: 4, Latest: "Outline the fix"}
	if _, err := graph.PostMessage(Message{
		SessionID: "boost", Role: RoleSystem, Body: "designing the approach (1 of 4)",
		Model: "anthropic/claude-opus-5", Progress: wantProgress,
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
	if err := reopened.Rebuild(); err != nil {
		t.Fatal(err)
	}
	messages, err := reopened.Messages("boost", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 3 || messages[0].Model != "anthropic/claude-opus-5" ||
		messages[1].Model != "anthropic/claude-opus-5-2026-08-01" {
		t.Fatalf("reopened model metadata = %+v", messages)
	}
	if messages[2].Model != "anthropic/claude-opus-5" ||
		!reflect.DeepEqual(messages[2].Progress, wantProgress) {
		t.Fatalf("rebuilt model+progress message = %+v", messages[2])
	}
}
