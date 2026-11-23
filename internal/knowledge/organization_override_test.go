package knowledge_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func TestKnowledgeOrganizationProtectsHumanEditsAndDeletedConcepts(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	var editedSeen, deletedSeen bool
	withConcurrentKnowledgeModel(t, func(p map[string]any) map[string]any {
		if messages, ok := p["messages"].([]any); ok {
			s := messages[0].(map[string]any)
			return map[string]any{"claims": []any{map[string]any{"local_id": "c", "text": s["text"], "scope": "Cedar", "topic": s["id"], "kind": "fact", "evidence": []any{map[string]any{"source": s["id"], "quote": s["text"]}}}}}
		}
		if catalog, ok := p["catalog"].([]any); ok {
			n := catalog[0].(map[string]any)
			subject := map[string]any{"title": "Job queues", "scope": "Cedar", "notes": []any{n["id"]}}
			if existing, ok := p["existing_subjects"].([]any); ok && len(existing) > 0 {
				prior := existing[0].(map[string]any)
				if prior["human_edited"] == true || prior["deleted"] == true {
					subject["existing_subject_id"] = prior["id"]
					editedSeen = editedSeen || prior["human_edited"] == true
					deletedSeen = deletedSeen || prior["deleted"] == true
				} else {
					subject["title"] = "Queue handling"
				}
			}
			return map[string]any{"subjects": []any{subject}}
		}
		if p["paragraphs"] != nil {
			return map[string]any{"checks": []any{map[string]any{"id": "P0", "supported": true, "useful": true}}}
		}
		if p["remaining_steps"] != nil {
			return refinementDraft(p)
		}
		return synthesisDraft(p)
	})
	capture := func(number int, text string) {
		t.Helper()
		if err := v.Capture(knowledge.Source{ID: fmt.Sprintf("session-%d", number), Session: fmt.Sprintf("chat-%d", number), Role: "user", TS: "2026-10-25T12:00:00Z", Text: text}); err != nil {
			t.Fatal(err)
		}
		if err := v.Flush(context.Background(), "fixture", true); err != nil {
			t.Fatal(err)
		}
	}
	capture(1, "Cedar queues jobs.")
	state, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	var original knowledge.Page
	for _, page := range state.Pages {
		if page.Kind == "subject" {
			original = page
		}
	}
	if original.ID == "" {
		t.Fatal("initial concept was not published")
	}
	markdown := "## My queue guide\n\nI review the queue before changing its settings."
	if err := v.EditPage(original.ID, original.Revision, markdown); err != nil {
		t.Fatal(err)
	}
	edited, err := v.Read(original.ID)
	if err != nil || edited.NeedsEditorialUpdate {
		t.Fatalf("fresh human edit should acknowledge existing evidence: %+v %v", edited, err)
	}
	capture(2, "Cedar retries failed jobs.")
	if !editedSeen {
		t.Fatal("existing concept catalog did not expose human-edit protection")
	}
	edited, err = v.Read(original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if knowledge.PageMarkdown(edited) != markdown || !edited.HumanEdited || edited.LearningState != "human_edited" {
		t.Fatalf("new evidence overwrote the human guide: %+v", edited)
	}
	if !edited.NeedsEditorialUpdate || len(edited.Evidence) != 2 {
		t.Fatalf("new background evidence did not request review: %+v", edited)
	}
	for _, section := range edited.Sections {
		for _, paragraph := range section.Paragraphs {
			if len(paragraph.Claims) != 0 || len(paragraph.Evidence) != 0 {
				t.Fatal("human prose inherited source proof from generated text")
			}
		}
	}
	if err := v.EditPage(edited.ID, edited.Revision, markdown); err != nil {
		t.Fatal(err)
	}
	reviewed, err := v.Read(original.ID)
	if err != nil || reviewed.NeedsEditorialUpdate {
		t.Fatalf("saving reviewed human text did not acknowledge background evidence: %+v %v", reviewed, err)
	}
	capture(3, "Cedar delays retries.")
	reviewed, err = v.Read(original.ID)
	if err != nil || !reviewed.NeedsEditorialUpdate || knowledge.PageMarkdown(reviewed) != markdown {
		t.Fatalf("later evidence did not reopen human review: %+v %v", reviewed, err)
	}
	if err := v.DeletePage(reviewed.ID, reviewed.Revision); err != nil {
		t.Fatal(err)
	}
	capture(4, "Cedar tracks job completion.")
	if !deletedSeen {
		t.Fatal("existing concept catalog did not expose deletion protection")
	}
	state, err = v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range state.Pages {
		if page.Kind == "subject" || page.ID == original.ID || page.Title == "Queue handling" {
			t.Fatalf("deleted concept returned under another page: %+v", page)
		}
	}
}
