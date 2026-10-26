package knowledge_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func TestKnowledgeSelectsCompactInitialAndRefreshedGuidesWithoutChangingEvidence(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	reviewed := map[string][]string{}
	selections := 0
	selectionSizes := []int{}
	emptyChoice := false
	withConcurrentKnowledgeModel(t, func(p map[string]any) map[string]any {
		if messages, ok := p["messages"].([]any); ok {
			claims := []any{}
			for _, raw := range messages {
				s := raw.(map[string]any)
				claims = append(claims, map[string]any{"local_id": s["id"], "text": s["text"], "scope": "Cedar", "topic": "Queues", "kind": "fact", "evidence": []any{map[string]any{"source": s["id"], "quote": s["text"]}}})
			}
			return map[string]any{"claims": claims}
		}
		if catalog, ok := p["catalog"].([]any); ok {
			return map[string]any{"subjects": []any{map[string]any{"title": "Queue operations", "scope": "Cedar", "notes": []any{catalog[0].(map[string]any)["id"]}}}}
		}
		if paragraphs, ok := p["paragraphs"].([]any); ok {
			if _, curation := paragraphs[0].(map[string]any)["heading"]; curation {
				if emptyChoice {
					return map[string]any{"sections": []any{}}
				}
				if len(paragraphs) > 48 {
					t.Errorf("curation request contains %d candidates", len(paragraphs))
				}
				mu.Lock()
				selections++
				selectionSizes = append(selectionSizes, len(paragraphs))
				mu.Unlock()
				ids := []any{}
				for _, raw := range paragraphs[:min(8, len(paragraphs))] {
					ids = append(ids, raw.(map[string]any)["id"])
				}
				return map[string]any{"sections": []any{map[string]any{"heading": "Operating rules", "paragraphs": ids}}}
			}
			checks := []any{}
			mu.Lock()
			for _, raw := range paragraphs {
				paragraph := raw.(map[string]any)
				refs := []string{}
				for _, id := range paragraph["claims"].([]any) {
					refs = append(refs, id.(string))
				}
				reviewed[paragraph["text"].(string)] = refs
				checks = append(checks, map[string]any{"id": paragraph["id"], "supported": true, "useful": true})
			}
			mu.Unlock()
			return map[string]any{"checks": checks}
		}
		claims := p["claims"].([]any)
		count, perSection := 16, 1
		if p["remaining_steps"] != nil {
			count, perSection = 8, 3
		}
		sections := []any{}
		for i := 0; i < count; i++ {
			paragraphs := []any{}
			for j := 0; j < perSection; j++ {
				id := claims[(i*perSection+j)%len(claims)].(map[string]any)["id"]
				paragraphs = append(paragraphs, map[string]any{"text": fmt.Sprintf("Queue procedure %v section %d paragraph %d preserves this reviewed rule.", id, i, j), "claims": []any{id}})
			}
			sections = append(sections, map[string]any{"heading": fmt.Sprintf("Procedure %d", i), "paragraphs": paragraphs})
		}
		return map[string]any{"action": "write", "sections": sections}
	})
	check := func(sources int) string {
		t.Helper()
		state, err := v.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		for _, page := range state.Pages {
			if page.Kind != "subject" {
				continue
			}
			paragraphs := 0
			for _, section := range page.Sections {
				if section.Heading == "Recent knowledge" {
					t.Fatal("omitted reviewed details reappeared as raw Recent knowledge")
				}
				for _, paragraph := range section.Paragraphs {
					paragraphs++
					original := reviewed[paragraph.Text]
					if fmt.Sprint(paragraph.Claims) != fmt.Sprint(original) || len(paragraph.Evidence) == 0 {
						t.Fatalf("selection changed reviewed prose/support: %+v", paragraph)
					}
				}
			}
			if len(page.Sections) > 8 || paragraphs > 12 || len(strings.Fields(knowledge.PageMarkdown(page))) > 900 {
				t.Fatalf("guide was not compact: sections=%d paragraphs=%d", len(page.Sections), paragraphs)
			}
			if len(page.Evidence) != sources {
				t.Fatalf("guide selection lost canonical evidence: got %d want %d", len(page.Evidence), sources)
			}
			return page.ID
		}
		t.Fatal("guide disappeared")
		return ""
	}
	for i := 0; i < 161; i++ {
		if err := v.Capture(knowledge.Source{ID: fmt.Sprintf("rule-%03d", i), Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: fmt.Sprintf("Cedar queue policy %d is documented.", i)}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		if err := v.Flush(context.Background(), "fixture", true); err != nil {
			t.Fatal(err)
		}
	}
	original := check(161)
	beforeRefresh := selections
	if err := v.Capture(knowledge.Source{ID: "new-policy", Session: "chat", Role: "user", TS: "2026-10-26T12:00:00Z", Text: "Cedar queues now show a policy review reminder."}); err != nil {
		t.Fatal(err)
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	if check(162) != original || selections <= beforeRefresh || beforeRefresh == 0 {
		t.Fatal("initial and refreshed guides were not both curated under their stable identity")
	}
	sawFullBatch := false
	for _, size := range selectionSizes {
		if size == 48 {
			sawFullBatch = true
		}
	}
	if !sawFullBatch || selections-beforeRefresh < 3 {
		t.Fatalf("refresh did not exercise hierarchical selection over72 candidates: sizes=%v", selectionSizes)
	}
	emptyChoice = true
	if err := v.Capture(knowledge.Source{ID: "final-policy", Session: "chat", Role: "user", TS: "2026-10-26T13:00:00Z", Text: "Cedar queues require a final policy check."}); err != nil {
		t.Fatal(err)
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	state, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range state.Pages {
		if page.Kind == "subject" && page.ID == original {
			if len(page.Sections) != 0 || page.LearningState != "needs_evidence" || len(page.Evidence) != 163 {
				t.Fatalf("valid empty final selection retained old prose or lost evidence: sections=%d state=%s sources=%d", len(page.Sections), page.LearningState, len(page.Evidence))
			}
			return
		}
	}
	t.Fatal("empty final selection lost canonical guide identity")
}

func TestKnowledgeRejectsUnsafeGuideSelectionAndRetainsRetryProgress(t *testing.T) {
	for _, mistake := range []string{"unknown paragraph", "repeated paragraph", "oversized selection"} {
		t.Run(mistake, func(t *testing.T) {
			v := knowledge.Open(t.TempDir())
			if err := v.StartBuild(); err != nil {
				t.Fatal(err)
			}
			invalid := true
			selectionCalls := 0
			withConcurrentKnowledgeModel(t, func(p map[string]any) map[string]any {
				if messages, ok := p["messages"].([]any); ok {
					s := messages[0].(map[string]any)
					return map[string]any{"claims": []any{map[string]any{"local_id": "c", "text": s["text"], "scope": "Cedar", "topic": "Queues", "kind": "fact", "evidence": []any{map[string]any{"source": s["id"], "quote": s["text"]}}}}}
				}
				if catalog, ok := p["catalog"].([]any); ok {
					return map[string]any{"subjects": []any{map[string]any{"title": "Queues", "scope": "Cedar", "notes": []any{catalog[0].(map[string]any)["id"]}}}}
				}
				if paragraphs, ok := p["paragraphs"].([]any); ok {
					if _, curation := paragraphs[0].(map[string]any)["heading"]; curation {
						selectionCalls++
						if mistake == "oversized selection" {
							sections := []any{}
							for i, raw := range paragraphs {
								candidate := raw.(map[string]any)
								if candidate["word_count"] == nil {
									t.Error("selector candidate lacks word count")
								}
								sections = append(sections, map[string]any{"heading": fmt.Sprintf("Rule %d", i), "paragraphs": []any{candidate["id"]}})
							}
							return map[string]any{"sections": sections}
						}
						id := paragraphs[0].(map[string]any)["id"]
						ids := []any{id}
						if invalid && mistake == "unknown paragraph" {
							ids = []any{"uninspected-paragraph"}
						} else if invalid && mistake == "repeated paragraph" {
							ids = []any{id, id}
						}
						return map[string]any{"sections": []any{map[string]any{"heading": "Current guide", "paragraphs": ids}}}
					}
					checks := []any{}
					for _, raw := range paragraphs {
						checks = append(checks, map[string]any{"id": raw.(map[string]any)["id"], "supported": true, "useful": true})
					}
					return map[string]any{"checks": checks}
				}
				claim := p["claims"].([]any)[0].(map[string]any)["id"]
				paragraphs := []any{}
				for i := 0; i < 13; i++ {
					text := fmt.Sprintf("Reviewed queue explanation %d.", i)
					if mistake == "oversized selection" && i == 0 {
						text = strings.Repeat("durable ", 901)
					}
					paragraphs = append(paragraphs, map[string]any{"text": text, "claims": []any{claim}})
				}
				return map[string]any{"sections": []any{map[string]any{"heading": "Queues", "paragraphs": paragraphs}}}
			})
			if err := v.Capture(knowledge.Source{ID: "s", Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: "Cedar queues durable jobs."}); err != nil {
				t.Fatal(err)
			}
			if mistake == "oversized selection" {
				if err := v.Flush(context.Background(), "fixture", true); err != nil {
					t.Fatalf("reference-valid overlarge proposal required retry: %v", err)
				}
				state, err := v.Snapshot()
				if err != nil {
					t.Fatal(err)
				}
				for _, page := range state.Pages {
					if page.Kind == "subject" {
						if selectionCalls != 1 || len(page.Sections) != 8 || page.Sections[0].Paragraphs[0].Text != "Reviewed queue explanation 1." || len(page.Evidence) != 1 {
							t.Fatalf("host bounding changed prose, lost evidence or retried: calls=%d page=%+v", selectionCalls, page)
						}
						return
					}
				}
				t.Fatal("bounded guide missing")
			}
			if err := v.Flush(context.Background(), "fixture", true); err == nil {
				t.Fatal("unsafe guide selection was accepted")
			}
			state, err := v.Snapshot()
			if err != nil || len(state.Pages) != 0 {
				t.Fatalf("unsafe guide selection replaced published state: %+v %v", state.Pages, err)
			}
			invalid = false
			if err := v.Flush(context.Background(), "fixture", true); err != nil {
				t.Fatalf("safe selection could not resume retained evidence: %v", err)
			}
			state, err = v.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			for _, page := range state.Pages {
				if page.Kind == "subject" {
					expected := "Reviewed queue explanation 0."
					if len(page.Sections) != 1 || len(page.Sections[0].Paragraphs) != 1 || page.Sections[0].Paragraphs[0].Text != expected || len(page.Evidence) != 1 {
						t.Fatalf("safe retry lost the original reviewed paragraph/evidence: %+v", page)
					}
					return
				}
			}
			t.Fatal("safe guide selection did not publish")
		})
	}
}

func TestKnowledgeGuideSelectionBoundsNonreducingContextAndCachedRetry(t *testing.T) {
	for _, test := range []struct {
		name       string
		characters int
	}{
		{"two candidate context batches", 9000},
		{"oversized singleton context", 25000},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := knowledge.Open(t.TempDir())
			if err := v.StartBuild(); err != nil {
				t.Fatal(err)
			}
			large, failSelection := false, true
			selectionCalls := 0
			firstRequests := 0
			withConcurrentKnowledgeModel(t, func(p map[string]any) map[string]any {
				if messages, ok := p["messages"].([]any); ok {
					s := messages[0].(map[string]any)
					return map[string]any{"claims": []any{map[string]any{"local_id": "c", "text": s["text"], "scope": "Cedar", "topic": "Queues", "kind": "fact", "evidence": []any{map[string]any{"source": s["id"], "quote": s["text"]}}}}}
				}
				if catalog, ok := p["catalog"].([]any); ok {
					return map[string]any{"subjects": []any{map[string]any{"title": "Queues", "scope": "Cedar", "notes": []any{catalog[0].(map[string]any)["id"]}}}}
				}
				if paragraphs, ok := p["paragraphs"].([]any); ok {
					if _, curation := paragraphs[0].(map[string]any)["heading"]; curation {
						selectionCalls++
						if paragraphs[0].(map[string]any)["id"] == "P0" && paragraphs[0].(map[string]any)["heading"] == "Procedure 0" {
							firstRequests++
						}
						if failSelection && selectionCalls == 2 {
							return map[string]any{"sections": []any{map[string]any{"heading": "Selected", "paragraphs": []any{"unknown"}}}}
						}
						count := len(paragraphs)
						ids := []any{}
						for _, raw := range paragraphs[:count] {
							ids = append(ids, raw.(map[string]any)["id"])
						}
						sections := []any{}
						if count > 0 {
							sections = append(sections, map[string]any{"heading": "Selected", "paragraphs": ids})
						}
						return map[string]any{"sections": sections}
					}
					checks := []any{}
					for _, raw := range paragraphs {
						checks = append(checks, map[string]any{"id": raw.(map[string]any)["id"], "supported": true, "useful": true})
					}
					return map[string]any{"checks": checks}
				}
				refs := []any{}
				for _, raw := range p["claims"].([]any) {
					refs = append(refs, raw.(map[string]any)["id"])
				}
				sections := []any{}
				if large {
					for i := 0; i < 8; i++ {
						paragraphs := []any{}
						for j := 0; j < 2; j++ {
							paragraphs = append(paragraphs, map[string]any{"text": fmt.Sprintf("Reviewed queue %d %d %s", i, j, strings.Repeat("x", test.characters)), "claims": refs})
						}
						sections = append(sections, map[string]any{"heading": fmt.Sprintf("Procedure %d", i), "paragraphs": paragraphs})
					}
				} else {
					sections = append(sections, map[string]any{"heading": "Queues", "paragraphs": []any{map[string]any{"text": "Cedar queues jobs.", "claims": refs}}})
				}
				return map[string]any{"action": "write", "sections": sections}
			})
			captureRefinementRound(t, v, "baseline", []string{"Cedar"})
			if err := v.Flush(context.Background(), "fixture", true); err != nil {
				t.Fatal(err)
			}
			before, err := v.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			large = true
			captureRefinementRound(t, v, "added", []string{"Cedar"})
			if err := v.Flush(context.Background(), "fixture", true); err == nil {
				t.Fatal("unknown paragraph selection was accepted")
			}
			failed, err := v.Snapshot()
			if err != nil || failed.Revision != before.Revision {
				t.Fatalf("failed selection changed the readable publication: %v", err)
			}
			failSelection = false
			if err := v.Flush(context.Background(), "fixture", true); err != nil {
				t.Fatalf("cached overlarge responses stranded a safe retry: %v", err)
			}
			state, err := v.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			for _, page := range state.Pages {
				if page.Kind == "subject" {
					paragraphs, words := 0, 0
					for _, section := range page.Sections {
						words += len(strings.Fields(section.Heading))
						for _, paragraph := range section.Paragraphs {
							paragraphs++
							words += len(strings.Fields(paragraph.Text))
						}
					}
					if len(page.Evidence) != 2 || page.NeedsEditorialUpdate || len(page.Sections) > 8 || paragraphs > 12 || words > 900 {
						t.Fatalf("safe reduction lost evidence or resurrected pending prose: %+v", page)
					}
					if firstRequests != 1 {
						t.Fatalf("valid first proposal was not reused from cache: %d requests", firstRequests)
					}
					return
				}
			}
			t.Fatal("safe reduction lost the readable guide")

		})
	}
}

func TestKnowledgeFinalGuideBudgetIncludesReviewedQuestionAnswers(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	answerSelected := false
	answer := "Cedar queues retry failed jobs."
	withConcurrentKnowledgeModel(t, func(p map[string]any) map[string]any {
		if messages, ok := p["messages"].([]any); ok {
			s := messages[0].(map[string]any)
			return map[string]any{"claims": []any{map[string]any{"local_id": "c", "text": s["text"], "scope": "Cedar", "topic": "Queues", "kind": "fact", "evidence": []any{map[string]any{"source": s["id"], "quote": s["text"]}}}}}
		}
		if catalog, ok := p["catalog"].([]any); ok {
			return map[string]any{"subjects": []any{map[string]any{"title": "Queue operations", "scope": "Cedar", "notes": []any{catalog[0].(map[string]any)["id"]}}}}
		}
		if paragraphs, ok := p["paragraphs"].([]any); ok {
			if _, curation := paragraphs[0].(map[string]any)["heading"]; curation {
				ids := []any{}
				for _, raw := range paragraphs {
					paragraph := raw.(map[string]any)
					if paragraph["text"] == answer {
						ids = append(ids, paragraph["id"])
						answerSelected = true
					}
				}
				for _, raw := range paragraphs {
					paragraph := raw.(map[string]any)
					if paragraph["text"] != answer && len(ids) < 8 {
						ids = append(ids, paragraph["id"])
					}
				}
				return map[string]any{"sections": []any{map[string]any{"heading": "Current queue guide", "paragraphs": ids}}}
			}
			checks := []any{}
			for _, raw := range paragraphs {
				checks = append(checks, map[string]any{"id": raw.(map[string]any)["id"], "supported": true, "useful": true})
			}
			return map[string]any{"checks": checks, "questions": []any{map[string]any{"id": "Q0", "grounded": true}}}
		}
		if answers, ok := p["answers"].([]any); ok {
			return map[string]any{"checks": []any{map[string]any{"question": answers[0].(map[string]any)["question"], "supported": true, "complete": true}}}
		}
		if questions, ok := p["questions"].([]any); ok {
			question := questions[0].(map[string]any)
			if p["per_subject"] != nil {
				return map[string]any{"keep": []any{map[string]any{"id": question["id"], "reason": "Useful current behavior"}}}
			}
			if p["budget"] != nil {
				return map[string]any{"selected": []any{map[string]any{"id": question["id"], "reason": "Useful current behavior"}}}
			}
			if p["sources"] != nil {
				refs := question["claims"].([]any)
				return map[string]any{"answers": []any{map[string]any{"question": question["id"], "heading": "Retry behavior", "text": answer, "claims": []any{refs[0]}, "evidence": []any{}}}}
			}
			if p["claims"] != nil {
				return map[string]any{"matches": []any{}}
			}
			return map[string]any{"keep": []any{map[string]any{"id": question["id"], "reason": "Useful current behavior"}}}
		}
		refs := []any{}
		for _, raw := range p["claims"].([]any) {
			refs = append(refs, raw.(map[string]any)["id"])
		}
		sections := []any{}
		for i := 0; i < 8; i++ {
			sections = append(sections, map[string]any{"heading": fmt.Sprintf("Queue practice %d", i), "paragraphs": []any{map[string]any{"text": fmt.Sprintf("Reviewed queue practice %d.", i), "claims": refs}}})
		}
		return map[string]any{"action": "write", "sections": sections, "questions": []any{map[string]any{"question": "How do queues retry failed jobs?", "why": "Understand retry behavior", "claims": []any{refs[0]}, "search_terms": []any{"queues", "retry", "failed"}}}}
	})
	for i, text := range []string{answer, "Cedar queues expose retry counters."} {
		if err := v.Capture(knowledge.Source{ID: fmt.Sprintf("s%d", i), Session: "chat", Role: "user", TS: "2026-10-25T12:00:00Z", Text: text}); err != nil {
			t.Fatal(err)
		}
		if err := v.Flush(context.Background(), "fixture", true); err != nil {
			t.Fatal(err)
		}
	}
	if !answerSelected {
		t.Fatal("final selector did not inspect the reviewed answer appended to the full guide")
	}
	state, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range state.Pages {
		if page.Kind == "subject" {
			paragraphs := 0
			retainedAnswer := false
			for _, section := range page.Sections {
				paragraphs += len(section.Paragraphs)
				for _, paragraph := range section.Paragraphs {
					retainedAnswer = retainedAnswer || paragraph.Text == answer
				}
			}
			if len(page.Sections) > 8 || paragraphs > 12 || len(page.Evidence) != 2 || !retainedAnswer {
				t.Fatalf("reviewed answers bypassed the final reader budget or lost citations: %+v", page)
			}
			return
		}
	}
	t.Fatal("final guide disappeared")
}
