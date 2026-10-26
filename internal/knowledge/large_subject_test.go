package knowledge_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func TestKnowledgeLargeConceptKeepsOneUsableGuideAndReadsEveryClaim(t *testing.T) {
	for _, test := range []struct {
		name           string
		initial, added int
		twoDrafts      bool
	}{
		{"initial 161 claims", 161, 0, false},
		{"consolidated 161 claims", 161, 0, true},
		{"existing 100 grows to 170", 100, 70, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := knowledge.Open(t.TempDir())
			if err := v.StartBuild(); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			initialSeen, refreshedSeen := map[string]bool{}, map[string]bool{}
			requests := 0
			retiredID, retiredSource, retiredText := "", "", ""
			withConcurrentKnowledgeModel(t, func(p map[string]any) map[string]any {
				if messages, ok := p["messages"].([]any); ok {
					claims := []any{}
					relations := []any{}
					for _, raw := range messages {
						s := raw.(map[string]any)
						topic, key := "Queues", s["id"]
						if test.twoDrafts && s["id"].(string) >= "rule-080" {
							topic = "Retries"
						}
						if s["id"] == "correction" {
							key = retiredSource
							relations = append(relations, map[string]any{"from": "new:correction", "to": retiredID, "kind": "supersedes"})
						}
						claims = append(claims, map[string]any{"local_id": s["id"], "text": s["text"], "scope": "Cedar", "topic": topic, "key": key, "kind": "fact", "evidence": []any{map[string]any{"source": s["id"], "quote": s["text"]}}})
					}
					return map[string]any{"claims": claims, "relations": relations}
				}
				if catalog, ok := p["catalog"].([]any); ok {
					subjects := []any{}
					for _, raw := range catalog {
						n := raw.(map[string]any)
						subjects = append(subjects, map[string]any{"title": n["title"], "scope": "Cedar", "notes": []any{n["id"]}})
					}
					return map[string]any{"subjects": subjects}
				}
				if paragraphs, ok := p["paragraphs"].([]any); ok {
					checks := []any{}
					for _, raw := range paragraphs {
						checks = append(checks, map[string]any{"id": raw.(map[string]any)["id"], "supported": true, "useful": true})
					}
					return map[string]any{"checks": checks}
				}
				claims := p["claims"].([]any)
				limit, seen := 160, initialSeen
				if p["remaining_steps"] != nil {
					limit, seen = 80, refreshedSeen
				}
				if len(claims) > limit {
					t.Errorf("one evidence part exceeded its %d-claim limit: %d", limit, len(claims))
				}
				refs := []any{}
				mu.Lock()
				requests++
				if p["remaining_steps"] != nil && len(claims) == 80 && retiredID == "" {
					claim := claims[0].(map[string]any)
					retiredID, retiredText = claim["id"].(string), claim["text"].(string)
					var number int
					fmt.Sscanf(retiredText, "Cedar queue rule %d is active.", &number)
					retiredSource = fmt.Sprintf("rule-%03d", number)
				}
				for _, raw := range claims {
					id := raw.(map[string]any)["id"].(string)
					seen[id] = true
					refs = append(refs, id)
				}
				mu.Unlock()
				return map[string]any{"action": "write", "sections": []any{map[string]any{"heading": "Queue operations", "paragraphs": []any{map[string]any{"text": fmt.Sprintf("Queue operations include the sourced rules beginning with %v.", refs[0]), "claims": refs}}}}}
			}, func(p map[string]any) map[string]any {
				members := []any{}
				for _, raw := range p["consolidation_catalog"].([]any) {
					members = append(members, raw.(map[string]any)["id"])
				}
				return map[string]any{"groups": []any{map[string]any{"members": members, "title": "Queue operations", "scope": "Cedar", "purpose": "Operate queues and retries", "folder": []any{"Cedar"}}}}
			})
			capture := func(start, count int) {
				t.Helper()
				for i := start; i < start+count; i++ {
					if err := v.Capture(knowledge.Source{ID: fmt.Sprintf("rule-%03d", i), Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: fmt.Sprintf("Cedar queue rule %03d is active.", i)}); err != nil {
						t.Fatal(err)
					}
				}
				for i := 0; i < (count+63)/64; i++ {
					if err := v.Flush(context.Background(), "fixture", true); err != nil {
						t.Fatal(err)
					}
				}
			}
			readGuide := func(count int) knowledge.Page {
				t.Helper()
				state, err := v.Snapshot()
				if err != nil {
					t.Fatal(err)
				}
				guides := []knowledge.Page{}
				for _, page := range state.Pages {
					if page.Kind == "subject" {
						guides = append(guides, page)
					}
				}
				if len(guides) != 1 {
					t.Fatalf("large concept fragmented into %d guides", len(guides))
				}
				page := guides[0]
				if page.LearningState != "explained" || !strings.Contains(knowledge.PageMarkdown(page), "Queue operations include") || len(page.Evidence) != count {
					t.Fatalf("large concept lacks a current readable guide: state=%s sources=%d want=%d", page.LearningState, len(page.Evidence), count)
				}
				return page
			}
			capture(0, test.initial)
			original := readGuide(test.initial)
			if len(initialSeen) != test.initial {
				t.Fatalf("initial writing omitted claims: saw %d want %d", len(initialSeen), test.initial)
			}
			if test.added > 0 {
				capture(test.initial, test.added)
				updated := readGuide(test.initial + test.added)
				if updated.ID != original.ID || len(refreshedSeen) != test.initial+test.added {
					t.Fatalf("growing the concept lost identity or skipped evidence: id=%s seen=%d", updated.ID, len(refreshedSeen))
				}
				beforeAppend := requests
				capture(test.initial+test.added, 1)
				appended := readGuide(test.initial + test.added + 1)
				if appended.ID != original.ID || requests-beforeAppend != 1 {
					t.Fatalf("adding one rule invalidated unchanged full evidence parts: id=%s requests=%d", appended.ID, requests-beforeAppend)
				}
				beforeCorrection := requests
				if err := v.Capture(knowledge.Source{ID: "correction", Session: "correction", Role: "user", TS: "2026-10-26T12:00:00Z", Text: strings.Replace(retiredText, " is active.", " is inactive.", 1)}); err != nil {
					t.Fatal(err)
				}
				if err := v.Flush(context.Background(), "fixture", true); err != nil {
					t.Fatal(err)
				}
				corrected := readGuide(test.initial + test.added + 1)
				if corrected.ID != original.ID || requests-beforeCorrection < 1 || requests-beforeCorrection > 2 {
					t.Fatalf("retiring a cached claim failed to refresh affected evidence parts: requests=%d", requests-beforeCorrection)
				}
				for _, section := range corrected.Sections {
					for _, paragraph := range section.Paragraphs {
						for _, id := range paragraph.Claims {
							if id == retiredID {
								t.Fatal("retired claim survived in a cached guide part")
							}
						}
					}
				}
			}
			completed := requests
			if err := v.Flush(context.Background(), "fixture", true); err != nil {
				t.Fatal(err)
			}
			if requests != completed {
				t.Fatal("unchanged large concept repeated evidence-part requests")
			}
		})
	}
}

func TestKnowledgeLargeGuidePreservesContradictionAcrossEvidenceParts(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	lines := []string{}
	for i := 0; i < 161; i++ {
		lines = append(lines, fmt.Sprintf("Cedar queue rule %03d is documented.", i))
	}
	lines[0], lines[160] = "Cedar queues accept duplicate jobs.", "Cedar queues reject duplicate jobs."
	var mu sync.Mutex
	endpoints, parts := map[string]string{}, map[string]int{}
	writes := 0
	reviewSawConflict := false
	withConcurrentKnowledgeModel(t, func(p map[string]any) map[string]any {
		if messages, ok := p["messages"].([]any); ok {
			claims := []any{}
			message := messages[0].(map[string]any)
			source := message["id"]
			if source == "correction" {
				return map[string]any{"claims": []any{map[string]any{"local_id": "corrected", "text": message["text"], "scope": "Cedar", "topic": "Queues", "key": "duplicate policy", "kind": "fact", "evidence": []any{map[string]any{"source": source, "quote": message["text"]}}}}, "relations": []any{map[string]any{"from": "new:corrected", "to": endpoints[lines[0]], "kind": "supersedes"}}}
			}
			for i, text := range lines {
				key := fmt.Sprintf("rule %d", i)
				if i == 0 || i == 160 {
					key = "duplicate policy"
				}
				claims = append(claims, map[string]any{"local_id": fmt.Sprintf("c%d", i), "text": text, "scope": "Cedar", "topic": "Queues", "key": key, "kind": "fact", "evidence": []any{map[string]any{"source": source, "quote": text}}})
			}
			return map[string]any{"claims": claims, "relations": []any{map[string]any{"from": "new:c160", "to": "new:c0", "kind": "contradicts", "reason": "Both acceptance and rejection of duplicate jobs are recorded."}}}
		}
		if catalog, ok := p["catalog"].([]any); ok {
			return map[string]any{"subjects": []any{map[string]any{"title": "Queue operations", "scope": "Cedar", "notes": []any{catalog[0].(map[string]any)["id"]}}}}
		}
		if paragraphs, ok := p["paragraphs"].([]any); ok {
			mu.Lock()
			if conflicts, ok := p["conflicts"].([]any); ok && len(conflicts) > 0 {
				reviewSawConflict = true
			}
			mu.Unlock()
			checks := []any{}
			for _, raw := range paragraphs {
				checks = append(checks, map[string]any{"id": raw.(map[string]any)["id"], "supported": true, "useful": true})
			}
			return map[string]any{"checks": checks}
		}
		claims := p["claims"].([]any)
		if len(claims) > 160 {
			t.Errorf("initial evidence part exceeded its claim budget: %d", len(claims))
		}
		refs := []any{}
		mu.Lock()
		writes++
		for _, raw := range claims {
			claim := raw.(map[string]any)
			id := claim["id"].(string)
			refs = append(refs, id)
			if claim["text"] == lines[0] || claim["text"] == lines[160] {
				endpoints[claim["text"].(string)], parts[id] = id, writes
			}
		}
		mu.Unlock()
		return map[string]any{"action": "write", "sections": []any{map[string]any{"heading": "Queue rules", "paragraphs": []any{map[string]any{"text": fmt.Sprintf("Queue rules in this evidence segment begin with %v.", refs[0]), "claims": refs}}}}}
	})
	if err := v.Capture(knowledge.Source{ID: "contradictory-rules", Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: strings.Join(lines, "\n")}); err != nil {
		t.Fatal(err)
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	a, b := endpoints[lines[0]], endpoints[lines[160]]
	if a == "" || b == "" || parts[a] == parts[b] {
		t.Fatalf("fixture did not exercise separate evidence parts: %v", parts)
	}
	state, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range state.Pages {
		if page.Kind != "subject" {
			continue
		}
		for _, conflict := range page.Conflicts {
			if len(conflict.Claims) == 2 && ((conflict.Claims[0] == a && conflict.Claims[1] == b) || (conflict.Claims[0] == b && conflict.Claims[1] == a)) && conflict.State == "unresolved" {
				if !reviewSawConflict {
					t.Fatal("guide reviewer did not receive the known contradiction")
				}
				if err := v.Capture(knowledge.Source{ID: "correction", Session: "chat", Role: "user", TS: "2026-10-26T12:00:00Z", Text: "Cedar duplicate-job policy requires manual review."}); err != nil {
					t.Fatal(err)
				}
				if err := v.Flush(context.Background(), "fixture", true); err != nil {
					t.Fatal(err)
				}
				corrected, err := v.Read(page.ID)
				if err != nil || len(corrected.Conflicts) != 0 {
					t.Fatalf("retired contradiction survived in the current guide: %+v %v", corrected.Conflicts, err)
				}
				return
			}
		}
	}
	t.Fatal("known contradiction disappeared across initial evidence parts")
}
