package knowledge_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func TestKnowledgeNoteRenamePreservesIdentityWithoutCollidingWithNewOriginalTitle(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	oldClaim := ""
	withKnowledgeModel(t, func(p map[string]any) map[string]any {
		messages, extracting := p["messages"].([]any)
		if !extracting {
			return map[string]any{"subjects": []any{}}
		}
		source := messages[0].(map[string]any)
		result := map[string]any{"claims": []any{map[string]any{"local_id": "new", "text": source["text"], "scope": "Cedar", "topic": "Queue policy", "kind": "fact", "evidence": []any{map[string]any{"source": source["id"], "quote": source["text"]}}}}}
		if oldClaim != "" {
			result["notes"] = []any{map[string]any{"scope": "Cedar", "title": "Retry policy", "claims": []any{oldClaim}}}
		}
		return result
	})
	if err := v.Capture(knowledge.Source{ID: "old", Session: "chat", Role: "user", TS: "2026-10-24T12:00:00Z", Text: "Cedar retries twice."}); err != nil {
		t.Fatal(err)
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	before, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Pages) != 1 {
		t.Fatalf("expected one evidence note, got %d", len(before.Pages))
	}
	oldID := before.Pages[0].ID
	oldClaim = before.Pages[0].Claims[0]
	if err := v.Capture(knowledge.Source{ID: "new", Session: "chat2", Role: "user", TS: "2026-10-25T12:00:00Z", Text: "Cedar queues durable jobs."}); err != nil {
		t.Fatal(err)
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatalf("renaming an evidence note blocked publication: %v", err)
	}
	after, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Pages) != 2 {
		t.Fatalf("rename lost independent note: %+v", after.Pages)
	}
	seen := map[string]bool{}
	for _, p := range after.Pages {
		if seen[p.ID] {
			t.Fatalf("duplicate knowledge identity %s", p.ID)
		}
		seen[p.ID] = true
		if p.Title == "Retry policy" && (p.ID != oldID || len(p.Claims) != 1 || p.Claims[0] != oldClaim) {
			t.Fatalf("renamed note lost stable identity/membership: %+v", p)
		}
		if len(p.Evidence) != 1 {
			t.Fatalf("note lost its evidence: %+v", p)
		}
	}
}

func TestKnowledgeRepairsPersistedDuplicateNoteIdentitiesWithoutReextracting(t *testing.T) {
	profile := t.TempDir()
	v := knowledge.Open(profile)
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	extractingCalls, discoveryCalls := 0, 0
	withKnowledgeModel(t, func(p map[string]any) map[string]any {
		messages, ok := p["messages"].([]any)
		if !ok {
			if catalog, discovering := p["catalog"].([]any); discovering {
				discoveryCalls++
				if len(catalog) != 2 || catalog[0].(map[string]any)["id"] == catalog[1].(map[string]any)["id"] {
					t.Errorf("organization did not see both distinct evidence notes: %d", len(catalog))
				}
			}
			return map[string]any{"subjects": []any{}}
		}
		extractingCalls++
		claims := []any{}
		for _, raw := range messages {
			source := raw.(map[string]any)
			claims = append(claims, map[string]any{"local_id": source["id"], "text": source["text"], "scope": "Cedar", "topic": source["id"], "kind": "fact", "evidence": []any{map[string]any{"source": source["id"], "quote": source["text"]}}})
		}
		return map[string]any{"claims": claims}
	})
	for _, id := range []string{"Retries", "Queues"} {
		if err := v.Capture(knowledge.Source{ID: id, Session: id, Role: "user", TS: "2026-10-25T12:00:00Z", Text: "Cedar policy for " + id + "."}); err != nil {
			t.Fatal(err)
		}
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	before, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(profile, "knowledge", "knowledge.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var raw string
	var revision int
	if err = db.QueryRow("SELECT payload,revision FROM publications WHERE partition='profile'").Scan(&raw, &revision); err != nil {
		t.Fatal(err)
	}
	var persisted map[string]any
	if err = json.Unmarshal([]byte(raw), &persisted); err != nil {
		t.Fatal(err)
	}
	notes := persisted["notes"].([]any)
	retainedID := notes[len(notes)-1].(map[string]any)["id"].(string)
	original, err := v.Read(retainedID)
	if err != nil {
		t.Fatal(err)
	}
	manual := "## My policy\n\nI check queued work before retrying."
	if err = v.EditPage(retainedID, original.Revision, manual); err != nil {
		t.Fatal(err)
	}
	notes[0].(map[string]any)["id"] = retainedID
	encoded, _ := json.Marshal(persisted)
	if _, err = db.Exec("UPDATE publications SET payload=? WHERE partition='profile'", string(encoded)); err != nil {
		t.Fatal(err)
	}
	if err = v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatalf("saved duplicate identities blocked repair: %v", err)
	}
	after, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Pages) != len(before.Pages) || extractingCalls != 1 {
		t.Fatalf("repair lost notes or re-extracted sources: pages=%d extraction=%d discovery=%d", len(after.Pages), extractingCalls, discoveryCalls)
	}
	for _, prior := range before.Pages {
		page, err := v.Read(prior.ID)
		if err != nil || (page.ID != retainedID && len(page.Claims) != 1) || len(page.Evidence) != 1 || page.Evidence[0].Source != prior.Evidence[0].Source {
			t.Fatalf("repair lost existing membership/evidence: %+v %v", page, err)
		}
	}
	edited, err := v.Read(retainedID)
	if err != nil || knowledge.PageMarkdown(edited) != manual || !edited.HumanEdited {
		t.Fatalf("repair changed retained human edit: %+v %v", edited, err)
	}
	var repairedRevision int
	if err = db.QueryRow("SELECT revision FROM publications WHERE partition='profile'").Scan(&repairedRevision); err != nil {
		t.Fatal(err)
	}
	if repairedRevision != revision+1 {
		t.Fatalf("repair must persist once: before=%d after=%d", revision, repairedRevision)
	}
	if err = v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	var unchangedRevision int
	if err = db.QueryRow("SELECT revision FROM publications WHERE partition='profile'").Scan(&unchangedRevision); err != nil {
		t.Fatal(err)
	}
	if unchangedRevision != repairedRevision {
		t.Fatal("unchanged repair repeatedly bumped the raw publication")
	}
}
