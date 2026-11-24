package knowledge_test

import (
	"context"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/knowledge"
)

// Owner decision 5 (2026-11-17): Add to Library is a deliberate act and never
// depends on "Learn from chats and jobs". E3 F4 found the verb gone with
// learning off and, once learning was on, the Library filled from a chat. With
// learning off a person's add must be kept and written, and nothing a person
// did not choose may ride along with it.
func TestADeliberateAddIsWrittenWithLearningOffAndNothingElseIs(t *testing.T) {
	dir := t.TempDir()
	v := knowledge.Open(dir)
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	// A chat source left pending from when learning was on.
	if err := v.Capture(knowledge.Source{ID: "chat-1", Session: "chat", Role: "user", TS: "2026-11-16T12:00:00Z", Text: "Roast the turkey to 165 F."}); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteMemoryEnabled(dir, false); err != nil {
		t.Fatal(err)
	}
	// Learning off: an ordinary capture keeps nothing, and no door other than a
	// deliberate one gets round that.
	if err := v.Capture(knowledge.Source{ID: "chat-2", Session: "chat", Role: "user", TS: "2026-11-16T12:01:00Z", Text: "Pies on Wednesday."}); err != nil {
		t.Fatal(err)
	}
	if err := v.Admit(knowledge.Source{ID: "chat-3", Session: "chat", Role: "user", TS: "2026-11-16T12:02:00Z", Text: "Sneaking in."}); err == nil {
		t.Fatal("Admit took a source that is not a person's deliberate add")
	}
	before, _ := v.Status()
	file := knowledge.Source{ID: knowledge.WorkstreamFileSourcePrefix + "ws-1/f-brief@00aa", Session: "workstream:ws-1", Role: "user",
		Kind: knowledge.KindWorkstreamFile, TS: "2026-11-17T09:00:00Z", Text: "Brief\n\nCedar queues jobs."}
	if err := v.Admit(file); err != nil {
		t.Fatalf("a deliberate add was refused with learning off: %v", err)
	}
	after, _ := v.Status()
	if after.Pending != before.Pending+1 {
		t.Fatalf("pending %d -> %d: the add was not kept", before.Pending, after.Pending)
	}

	var mu sync.Mutex
	sent := map[string]bool{}
	withKnowledgeModel(t, func(payload map[string]any) map[string]any {
		messages, _ := payload["messages"].([]any)
		if messages == nil {
			return map[string]any{"subjects": []any{}}
		}
		claims := []any{}
		for _, raw := range messages {
			m, _ := raw.(map[string]any)
			id, _ := m["id"].(string)
			mu.Lock()
			sent[id] = true
			mu.Unlock()
			if id == file.ID {
				claims = append(claims, map[string]any{"local_id": "c", "text": "Cedar queues jobs.", "topic": "Queues", "scope": "Cedar", "kind": "fact",
					"evidence": []any{map[string]any{"source": id, "quote": "Cedar queues jobs."}}})
			}
		}
		return map[string]any{"claims": claims}
	})
	if err := v.Write(context.Background(), "fixture", file.Session); err != nil {
		t.Fatalf("writing the add with learning off: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !sent[file.ID] {
		t.Fatalf("the add never reached the Library's writer; sent %v", sent)
	}
	if sent["chat-1"] || sent["chat-2"] {
		t.Fatalf("a chat the person did not choose rode along with their add: %v", sent)
	}
	hits, err := v.Search("queues", 5)
	if err != nil || len(hits) != 1 {
		t.Fatalf("the add is not in the Library: %+v %v", hits, err)
	}
	if knowledge.LearnedFromChats(hits[0].Page) {
		t.Fatal("a page made only from a person's add is marked as learned from chats")
	}
}

func TestLearnedFromChatsReadsEveryPiece(t *testing.T) {
	kept := knowledge.Page{Evidence: []knowledge.Evidence{{Source: knowledge.KeptPassagePrefix + "ab"}, {Source: knowledge.WorkstreamFileSourcePrefix + "w/f@1"}}}
	if knowledge.LearnedFromChats(kept) {
		t.Fatal("deliberate adds read as learned")
	}
	learned := knowledge.Page{Sections: []knowledge.Section{{Paragraphs: []knowledge.Paragraph{{Evidence: []knowledge.Evidence{{Source: "chat-turn-7"}}}}}}}
	if !knowledge.LearnedFromChats(learned) {
		t.Fatal("a paragraph learned from a chat is not marked")
	}
}
